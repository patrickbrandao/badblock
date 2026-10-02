// Package collector orquestra uma verificação do asn.txt do RIPE NCC: decide
// se o arquivo mudou, baixa, valida e aplica.
//
// A fonte não publica hash, então as checagens são, da mais barata para a
// mais cara:
//
//  1. GET condicional com ETag/Last-Modified do último arquivo → 304 = nada mudou.
//  2. Arquivo baixado com o mesmo SHA-256 do último aplicado → nada mudou.
//
// Só depois disso o arquivo é interpretado e aplicado. Um arquivo novo
// recusado (parser, travas, erro no banco) vira uma linha com status = 0 em
// asnames_run e a tabela fica como estava.
package collector

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/patrickbrandao/badblock/apps/asnames/collector/internal/fetch"
	"github.com/patrickbrandao/badblock/apps/asnames/collector/internal/parse"
	"github.com/patrickbrandao/badblock/apps/asnames/collector/internal/store"
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
	MinASNs          int
	RemovalThreshold float64
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
// igual ao último e ignora a trava de remoção em massa.
func (c *Collector) RunOnce(ctx context.Context, force bool) (*Result, error) {
	run := store.Run{StartedAt: c.now().UTC(), Forced: force, URL: c.opt.URL}

	last, err := c.store.LastApplied(ctx)
	if err != nil {
		return nil, fmt.Errorf("lendo o último arquivo aplicado: %w", err)
	}
	var lastSHA, lastVersion string
	var prev fetch.Validators
	if last != nil {
		lastSHA, lastVersion = last.SHA256, last.Version
		// Validadores HTTP só valem para a mesma URL; --force não os envia.
		if last.URL == c.opt.URL && !force {
			prev = fetch.Validators{ETag: last.ETag, LastModified: last.LastModified}
		}
	}

	dl, err := c.src.Download(ctx, c.opt.URL, prev)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	run.HTTPStatus = dl.Status
	if dl.NotModified {
		return c.unchanged(ctx, run, lastVersion, "HTTP 304")
	}
	run.ETag, run.LastModified, run.SHA256, run.Bytes = dl.ETag, dl.LastModified, dl.SHA256, int64(len(dl.Body))
	if !force && dl.SHA256 == lastSHA {
		return c.unchanged(ctx, run, lastVersion, "conteúdo igual ao último aplicado")
	}

	// A partir daqui é um arquivo novo: qualquer recusa fica em asnames_run.
	ds, err := parse.Parse(bytes.NewReader(dl.Body))
	if err != nil {
		return nil, c.fail(ctx, run, fmt.Errorf("parser: %w", err))
	}
	run.ASNs, run.Warnings = len(ds.ASNs), ds.Warnings
	if ds.WarningCount() > len(ds.Warnings) {
		run.Warnings = append(run.Warnings, fmt.Sprintf("... e mais %d avisos", ds.WarningCount()-len(ds.Warnings)))
	}
	if len(ds.ASNs) < c.opt.MinASNs {
		return nil, c.fail(ctx, run, fmt.Errorf("só %d ASNs no arquivo (mínimo %d): arquivo truncado?", len(ds.ASNs), c.opt.MinASNs))
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

// fail grava a recusa em asnames_run e devolve a causa.
func (c *Collector) fail(ctx context.Context, run store.Run, cause error) error {
	if err := c.store.RecordFailure(context.WithoutCancel(ctx), run, cause); err != nil {
		c.log.Error("não consegui gravar a falha em asnames_run", "err", err)
	}
	return cause
}
