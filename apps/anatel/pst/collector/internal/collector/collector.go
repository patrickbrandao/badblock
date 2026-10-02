// Package collector orquestra uma verificação da fonte da Anatel: decide se o
// arquivo mudou, baixa, extrai, valida e aplica.
//
// Ordem das checagens, da mais barata para a mais cara (a Anatel não publica
// hash do ZIP):
//
//  1. GET condicional com ETag/Last-Modified do último arquivo aplicado → 304.
//  2. ZIP baixado com o mesmo SHA-256 do último aplicado.
//  3. CSV extraído com o mesmo SHA-256 do último aplicado (o ZIP pode ser
//     regerado com o mesmo CSV).
//  4. CSV com data de modificação anterior à do último aplicado (cópia velha
//     num cache): não é aplicado e conta como sem mudança, com aviso no log.
//
// Só depois disso o CSV é interpretado e aplicado. Um arquivo novo recusado
// (ZIP, parser, mínimo, trava de remoção, banco) vira uma linha com status = 0
// em anatel_pst_run e as tabelas ficam como estavam.
package collector

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/patrickbrandao/badblock/apps/anatel/pst/collector/internal/archive"
	"github.com/patrickbrandao/badblock/apps/anatel/pst/collector/internal/fetch"
	"github.com/patrickbrandao/badblock/apps/anatel/pst/collector/internal/parse"
	"github.com/patrickbrandao/badblock/apps/anatel/pst/collector/internal/store"
)

// Store é o que o collector usa do banco (implementado por store.Store).
type Store interface {
	LastApplied(ctx context.Context) (*store.Applied, error)
	TouchCheck(ctx context.Context) error
	Apply(ctx context.Context, ds *parse.Dataset, run store.Run, opt store.ApplyOptions) (store.Changes, string, error)
	RecordFailure(ctx context.Context, run store.Run, cause error) error
}

// Source é o que o collector usa da rede (implementado por fetch.Fetcher).
type Source interface {
	Download(ctx context.Context, url string, prev fetch.Validators) (*fetch.Download, error)
}

// Options configura o collector.
type Options struct {
	URL              string
	MinProviders     int
	RemovalThreshold float64
	MaxCSVBytes      int64 // 0 = archive.MaxCSVBytes
}

// Outcome é o desfecho de uma verificação bem-sucedida.
type Outcome string

const (
	Unchanged Outcome = "unchanged"
	Applied   Outcome = "applied"
)

// Result descreve uma verificação bem-sucedida.
type Result struct {
	Outcome Outcome
	Reason  string // por que foi considerado sem mudança
	Version string // versão do dataset depois da verificação
	Run     store.Run
	Changes store.Changes
}

// Collector junta fonte, banco e opções.
type Collector struct {
	store Store
	src   Source
	opt   Options
	log   *slog.Logger
	now   func() time.Time
}

// New cria o collector.
func New(st Store, src Source, opt Options, log *slog.Logger) *Collector {
	return &Collector{store: st, src: src, opt: opt, log: log, now: time.Now}
}

// RunOnce faz uma verificação completa. Com force, aplica o arquivo mesmo
// igual ou mais antigo que o último e ignora a trava de remoção em massa.
func (c *Collector) RunOnce(ctx context.Context, force bool) (*Result, error) {
	run := store.Run{StartedAt: c.now().UTC(), Forced: force, URL: c.opt.URL}

	last, err := c.store.LastApplied(ctx)
	if err != nil {
		return nil, fmt.Errorf("lendo o último arquivo aplicado: %w", err)
	}
	var lastVersion string
	var prev fetch.Validators
	if last != nil {
		lastVersion = last.Version
		// Validadores HTTP só valem para a mesma URL.
		if last.URL == c.opt.URL && !force {
			prev = fetch.Validators{ETag: last.ETag, LastModified: last.LastModified}
		}
	}

	// 1) GET condicional.
	dl, err := c.src.Download(ctx, c.opt.URL, prev)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	run.HTTPStatus = dl.Status
	if dl.NotModified {
		return c.unchanged(ctx, run, lastVersion, "HTTP 304")
	}
	run.ETag, run.LastModified, run.SHA256, run.Bytes = dl.ETag, dl.LastModified, dl.SHA256, int64(len(dl.Body))

	// 2) Mesmo ZIP.
	if !force && last != nil && dl.SHA256 == last.SHA256 {
		return c.unchanged(ctx, run, lastVersion, "conteúdo igual ao último aplicado")
	}

	// A partir daqui é um arquivo novo: qualquer recusa fica em anatel_pst_run.
	csv, err := archive.Extract(dl.Body, c.opt.MaxCSVBytes)
	if err != nil {
		return nil, c.fail(ctx, run, err)
	}
	for _, name := range csv.Ignored {
		c.log.Warn("entrada do ZIP ignorada", "name", name)
	}
	run.CSVName, run.CSVSHA256, run.CSVBytes, run.CSVModifiedAt = csv.Name, csv.SHA256, int64(len(csv.Data)), csv.ModifiedAt

	// 3) Mesmo CSV num ZIP novo.
	if !force && last != nil && csv.SHA256 == last.CSVSHA256 {
		return c.unchanged(ctx, run, lastVersion, "csv igual ao último aplicado")
	}

	// 4) CSV mais antigo que o aplicado.
	if !force && last != nil && !csv.ModifiedAt.IsZero() && !last.CSVModifiedAt.IsZero() &&
		csv.ModifiedAt.Before(last.CSVModifiedAt) {
		why := fmt.Sprintf("%s < %s", brt(csv.ModifiedAt), brt(last.CSVModifiedAt))
		c.log.Warn("csv mais antigo que o aplicado; ignorado (espelho ou servidor com cópia velha?)",
			"csv_modified_at", brt(csv.ModifiedAt), "applied_csv_modified_at", brt(last.CSVModifiedAt),
			"sha256", dl.SHA256, "csv_sha256", csv.SHA256)
		return c.unchanged(ctx, run, lastVersion, "csv mais antigo que o aplicado ("+why+")")
	}

	ds, err := parse.Parse(bytes.NewReader(csv.Data))
	if err != nil {
		return nil, c.fail(ctx, run, fmt.Errorf("parser: %w", err))
	}
	run.Parsed = true
	run.Rows, run.RowsCNPJ, run.RowsCPF, run.Duplicates, run.Skipped = ds.Rows, ds.RowsCNPJ, ds.RowsCPF, ds.Duplicates, ds.Skipped
	run.Providers, run.Services, run.Warnings = len(ds.Providers), ds.Services(), ds.Warnings
	if ds.WarningCount() > len(ds.Warnings) {
		run.Warnings = append(run.Warnings, fmt.Sprintf("... e mais %d avisos", ds.WarningCount()-len(ds.Warnings)))
	}
	if len(ds.Providers) < c.opt.MinProviders {
		return nil, c.fail(ctx, run, fmt.Errorf("só %d prestadoras no arquivo (mínimo %d): arquivo truncado?",
			len(ds.Providers), c.opt.MinProviders))
	}

	changes, version, err := c.store.Apply(ctx, ds, run, store.ApplyOptions{
		RemovalThreshold: c.opt.RemovalThreshold, Force: force,
	})
	if err != nil {
		if errors.Is(err, store.ErrBusy) {
			return nil, err
		}
		return nil, c.fail(ctx, run, err)
	}
	return &Result{Outcome: Applied, Version: version, Run: run, Changes: changes}, nil
}

func (c *Collector) unchanged(ctx context.Context, run store.Run, version, reason string) (*Result, error) {
	if err := c.store.TouchCheck(ctx); err != nil {
		return nil, fmt.Errorf("jobs: %w", err)
	}
	return &Result{Outcome: Unchanged, Reason: reason, Version: version, Run: run}, nil
}

// fail grava a recusa em anatel_pst_run e devolve a causa.
func (c *Collector) fail(ctx context.Context, run store.Run, cause error) error {
	if err := c.store.RecordFailure(context.WithoutCancel(ctx), run, cause); err != nil {
		c.log.Error("não consegui gravar a falha em anatel_pst_run", "err", err)
	}
	return cause
}

// brt formata um instante na hora de Brasília (a da data do ZIP).
func brt(t time.Time) string {
	return t.In(archive.Brasilia).Format("2006-01-02T15:04:05-07:00")
}
