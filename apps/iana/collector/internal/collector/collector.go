// Package collector orquestra uma verificação dos 10 arquivos da IANA: decide
// se o dataset mudou, baixa, valida e aplica.
//
// Não há hash publicado. Para cada arquivo, um GET condicional com os
// validadores (ETag/Last-Modified) do último dataset aplicado: 304 ou SHA-256
// igual ao aplicado = arquivo sem mudança. Se nenhum mudou, a verificação
// termina aí. Se ao menos um mudou, os que vieram 304 são baixados de novo,
// sem validadores (são pequenos), e o dataset inteiro é interpretado,
// conferido e aplicado numa transação só.
//
// Falha de download de qualquer arquivo = verificação falha, nada aplicado
// (nunca um dataset parcial). Uma recusa depois dos downloads (parser,
// sanidade, trava de remoção, banco) vira uma linha com status = 0 em
// iana_run e as tabelas ficam como estavam.
package collector

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/patrickbrandao/badblock/apps/iana/collector/internal/fetch"
	"github.com/patrickbrandao/badblock/apps/iana/collector/internal/parse"
	"github.com/patrickbrandao/badblock/apps/iana/collector/internal/source"
	"github.com/patrickbrandao/badblock/apps/iana/collector/internal/store"
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
	IANABaseURL      string
	RDAPBaseURL      string
	RemovalThreshold float64
	Limits           parse.Limits // travas de sanidade (produção: parse.DefaultLimits())
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

// ChangedFiles lista os arquivos cujo conteúdo mudou.
func (r *Result) ChangedFiles() []string {
	out := []string{}
	for _, f := range r.Run.Files {
		if f.Changed {
			out = append(out, f.Name)
		}
	}
	return out
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

// RunOnce faz uma verificação completa. Com force, baixa e aplica o dataset
// mesmo sem mudança e ignora a trava de remoção em massa.
func (c *Collector) RunOnce(ctx context.Context, force bool) (*Result, error) {
	run := store.Run{StartedAt: c.now().UTC(), Forced: force}

	last, err := c.store.LastApplied(ctx)
	if err != nil {
		return nil, fmt.Errorf("lendo o último dataset aplicado: %w", err)
	}
	var lastVersion, lastSHA string
	if last != nil {
		lastVersion, lastSHA = last.Version, last.SHA256
	}

	// 1) GET condicional de cada arquivo.
	downloads := make([]*fetch.Download, len(source.Files))
	var notModified, sameContent, changed int
	for i, f := range source.Files {
		url := f.URL(c.opt.IANABaseURL, c.opt.RDAPBaseURL)
		var prev fetch.Validators
		// Validadores HTTP só valem para a mesma URL.
		if lf, ok := last.File(f.Name); ok && !force && lf.URL == url {
			prev = fetch.Validators{ETag: lf.ETag, LastModified: lf.LastModified}
		}
		dl, err := c.src.Download(ctx, url, prev)
		if err != nil {
			return nil, fmt.Errorf("download de %s: %w", f.Name, err)
		}
		downloads[i] = dl
		switch lf, _ := last.File(f.Name); {
		case dl.NotModified:
			notModified++
		case dl.SHA256 == lf.SHA256:
			sameContent++
		default:
			changed++
		}
	}
	if !force && changed == 0 {
		return c.unchanged(ctx, run, lastVersion,
			fmt.Sprintf("nenhum dos %d arquivos mudou (%d com HTTP 304, %d com conteúdo igual)", len(source.Files), notModified, sameContent))
	}

	// 2) Algum mudou: os que vieram 304 são baixados de novo, sem validadores,
	// para o dataset ser aplicado inteiro.
	for i, f := range source.Files {
		if !downloads[i].NotModified {
			continue
		}
		dl, err := c.src.Download(ctx, downloads[i].URL, fetch.Validators{})
		if err != nil {
			return nil, fmt.Errorf("download de %s: %w", f.Name, err)
		}
		if dl.NotModified {
			return nil, fmt.Errorf("download de %s: HTTP 304 sem validadores", f.Name)
		}
		downloads[i] = dl
	}

	bodies := map[string][]byte{}
	run.Files = make([]store.File, len(source.Files))
	for i, f := range source.Files {
		dl := downloads[i]
		lf, _ := last.File(f.Name)
		bodies[f.Name] = dl.Body
		run.Files[i] = store.File{
			Name: f.Name, URL: dl.URL, HTTPStatus: dl.Status, ETag: dl.ETag, LastModified: dl.LastModified,
			SHA256: dl.SHA256, Bytes: int64(len(dl.Body)), Changed: dl.SHA256 != lf.SHA256,
		}
	}
	run.SHA256 = CombinedSHA256(run.Files)
	if !force && run.SHA256 == lastSHA {
		return c.unchanged(ctx, run, lastVersion, "conteúdo igual ao último dataset aplicado")
	}

	// A partir daqui é um dataset novo: qualquer recusa fica em iana_run.
	ds, err := parse.Parse(bodies)
	if err != nil {
		return nil, c.fail(ctx, run, fmt.Errorf("parser: %w", err))
	}
	for i := range run.Files {
		st := ds.Files[run.Files[i].Name]
		rows := st.Rows
		run.Files[i].Rows = &rows
		run.Files[i].Publication = st.Publication
	}
	run.Warnings = ds.Warnings
	if ds.WarningCount() > len(ds.Warnings) {
		run.Warnings = append(run.Warnings, fmt.Sprintf("... e mais %d avisos", ds.WarningCount()-len(ds.Warnings)))
	}
	if err := parse.Check(ds, c.opt.Limits); err != nil {
		return nil, c.fail(ctx, run, fmt.Errorf("sanidade: %w", err))
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

// CombinedSHA256 é o hash do dataset: SHA-256 das linhas
// "<nome>:<sha256 do arquivo>\n" na ordem fixa dos arquivos.
func CombinedSHA256(files []store.File) string {
	var b strings.Builder
	for _, f := range files {
		b.WriteString(f.Name + ":" + f.SHA256 + "\n")
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func (c *Collector) unchanged(ctx context.Context, run store.Run, version, reason string) (*Result, error) {
	if err := c.store.TouchCheck(ctx); err != nil {
		return nil, fmt.Errorf("jobs: %w", err)
	}
	return &Result{Outcome: Unchanged, Reason: reason, Version: version, Run: run}, nil
}

// fail grava a recusa em iana_run e devolve a causa.
func (c *Collector) fail(ctx context.Context, run store.Run, cause error) error {
	if err := c.store.RecordFailure(context.WithoutCancel(ctx), run, cause); err != nil {
		c.log.Error("não consegui gravar a falha em iana_run", "err", err)
	}
	return cause
}
