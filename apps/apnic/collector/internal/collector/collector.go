// Package collector orquestra uma verificação do arquivo delegated-extended do
// RIR: decide se o arquivo mudou, baixa, valida e aplica.
//
// Ordem das checagens, da mais barata para a mais cara:
//
//  1. MD5 publicado (<url>.md5, ~75 bytes) igual ao do último arquivo
//     aplicado → nada mudou, nem baixa o arquivo.
//  2. GET condicional com ETag/Last-Modified do último arquivo → 304 = nada mudou.
//  3. Arquivo baixado com o mesmo SHA-256 do último aplicado → nada mudou.
//
// Só depois disso o arquivo é conferido contra o MD5 publicado, interpretado e
// aplicado. Um arquivo mais antigo que o aplicado (serial/enddate do cabeçalho
// menores, ex.: espelho atrasado) também conta como "nada mudou". Um arquivo
// novo recusado (MD5 divergente, parser, travas) vira uma linha com
// status = 0 em <fonte>_run e as tabelas ficam como estavam.
package collector

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/patrickbrandao/badblock/apps/apnic/collector/internal/fetch"
	"github.com/patrickbrandao/badblock/apps/apnic/collector/internal/parse"
	"github.com/patrickbrandao/badblock/apps/apnic/collector/internal/store"
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
	PublishedMD5(ctx context.Context, url string) (string, error)
	Download(ctx context.Context, url string, prev fetch.Validators) (*fetch.Download, error)
}

// Options configura o collector.
type Options struct {
	URL              string
	MD5URL           string // vazio desliga a conferência do hash publicado
	Registry         string // registry esperado no arquivo
	MinRecords       int
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
// igual ao último ou mais antigo que ele, e ignora a trava de remoção.
func (c *Collector) RunOnce(ctx context.Context, force bool) (*Result, error) {
	run := store.Run{StartedAt: c.now().UTC(), Forced: force, URL: c.opt.URL}

	last, err := c.store.LastApplied(ctx)
	if err != nil {
		return nil, fmt.Errorf("lendo o último arquivo aplicado: %w", err)
	}
	var lastMD5, lastSHA, lastVersion string
	var prev fetch.Validators
	if last != nil {
		lastMD5, lastSHA, lastVersion = last.MD5, last.SHA256, last.Version
		// Validadores HTTP só valem para a mesma URL.
		if last.URL == c.opt.URL {
			prev = fetch.Validators{ETag: last.ETag, LastModified: last.LastModified}
		}
	}

	var published string
	if c.opt.MD5URL != "" {
		published, err = c.src.PublishedMD5(ctx, c.opt.MD5URL)
		if err != nil {
			// Sem o hash publicado ainda dá para decidir pelo GET condicional;
			// o arquivo só não é conferido.
			c.log.Warn("md5 publicado indisponível; seguindo sem conferência", "url", c.opt.MD5URL, "err", err)
		}
	}
	if !force && published != "" && published == lastMD5 {
		return c.unchanged(ctx, lastVersion, "md5 publicado igual ao último aplicado")
	}

	if force {
		prev = fetch.Validators{}
	}
	dl, err := c.src.Download(ctx, c.opt.URL, prev)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	run.HTTPStatus = dl.Status
	if dl.NotModified {
		return c.unchanged(ctx, lastVersion, "HTTP 304")
	}
	run.ETag, run.LastModified, run.Bytes = dl.ETag, dl.LastModified, int64(len(dl.Body))
	run.MD5, run.SHA256 = dl.MD5, dl.SHA256
	if !force && dl.SHA256 == lastSHA {
		return c.unchanged(ctx, lastVersion, "conteúdo igual ao último aplicado")
	}

	// A partir daqui é um arquivo novo: qualquer recusa fica em <fonte>_run.
	if published != "" && dl.MD5 != published {
		return nil, c.fail(ctx, run, fmt.Errorf("md5 divergente: publicado %s, baixado %s "+
			"(arquivo e .md5 publicados em momentos diferentes?)", published, dl.MD5))
	}
	ds, err := parse.Parse(bytes.NewReader(dl.Body), c.opt.Registry)
	if err != nil {
		return nil, c.fail(ctx, run, fmt.Errorf("parser: %w", err))
	}
	run.Parsed, run.Header, run.Warnings = true, ds.Header, ds.Warnings
	run.ASNRecords, run.IPv4Records, run.IPv6Records = ds.ASNRecords, ds.IPv4Records, ds.IPv6Records
	run.PrefixesV4, run.PrefixesV6 = ds.PrefixesV4, ds.PrefixesV6
	if ds.WarningCount() > len(ds.Warnings) {
		run.Warnings = append(run.Warnings, fmt.Sprintf("... e mais %d avisos", ds.WarningCount()-len(ds.Warnings)))
	}

	if !force && last != nil {
		if why := older(ds.Header, last); why != "" {
			c.log.Warn("arquivo mais antigo que o aplicado; ignorado (espelho ou servidor com cópia velha?)",
				"serial", ds.Header.Serial, "end_date", dateStr(ds.Header.EndDate),
				"applied_serial", last.Serial, "applied_end_date", dateStr(last.EndDate), "md5", dl.MD5)
			return c.unchanged(ctx, lastVersion, "arquivo mais antigo que o aplicado ("+why+")")
		}
	}
	if n := ds.Records(); n < c.opt.MinRecords {
		return nil, c.fail(ctx, run, fmt.Errorf("só %d registros no arquivo (mínimo %d): arquivo truncado?", n, c.opt.MinRecords))
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

// older diz por que o cabeçalho h é mais antigo que o do último arquivo
// aplicado, ou "" se não é. Compara primeiro o enddate; com enddate igual (ou
// desconhecido), o serial, só quando os dois são números do mesmo tamanho
// (um RIR que mude o formato do serial não trava a coleta).
func older(h parse.Header, last *store.Applied) string {
	if !h.EndDate.IsZero() && !last.EndDate.IsZero() && !h.EndDate.Equal(last.EndDate) {
		if h.EndDate.Before(last.EndDate) {
			return fmt.Sprintf("enddate %s < %s", dateStr(h.EndDate), dateStr(last.EndDate))
		}
		return ""
	}
	if len(h.Serial) == len(last.Serial) && digits(h.Serial) && digits(last.Serial) && h.Serial < last.Serial {
		return fmt.Sprintf("serial %s < %s", h.Serial, last.Serial)
	}
	return ""
}

func digits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func dateStr(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
}

func (c *Collector) unchanged(ctx context.Context, version, reason string) (*Result, error) {
	if err := c.store.TouchCheck(ctx); err != nil {
		return nil, fmt.Errorf("jobs: %w", err)
	}
	return &Result{Outcome: Unchanged, Reason: reason, Version: version}, nil
}

// fail grava a recusa em <fonte>_run e devolve a causa.
func (c *Collector) fail(ctx context.Context, run store.Run, cause error) error {
	if err := c.store.RecordFailure(context.WithoutCancel(ctx), run, cause); err != nil {
		c.log.Error("não consegui gravar a falha na tabela de execuções", "err", err)
	}
	return cause
}
