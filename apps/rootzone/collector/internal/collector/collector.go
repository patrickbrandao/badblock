// Package collector orquestra uma verificação do root.zone da InterNIC:
// decide se a zona mudou, baixa, valida e aplica.
//
// Ordem das checagens, da mais barata para a mais cara:
//
//  1. MD5 publicado (root.zone.md5, 33 bytes) igual ao do último arquivo
//     aplicado → nada mudou, nem baixa o arquivo.
//  2. GET condicional com ETag/Last-Modified do último arquivo → 304 = nada mudou.
//  3. Arquivo baixado com o mesmo SHA-256 do último aplicado → nada mudou.
//  4. Depois da conferência do MD5 e do parser: serial do SOA menor que o
//     aplicado (RFC 1982) → arquivo mais antigo, nada mudou.
//
// Um arquivo novo recusado (MD5 divergente, parser, mínimo de TLDs, trava de
// remoção, erro no banco) vira uma linha com status = 0 em rootzone_run e as
// tabelas ficam como estavam.
package collector

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/patrickbrandao/badblock/apps/rootzone/collector/internal/fetch"
	"github.com/patrickbrandao/badblock/apps/rootzone/collector/internal/parse"
	"github.com/patrickbrandao/badblock/apps/rootzone/collector/internal/store"
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
	MinTLDs          int
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
		// Validadores HTTP só valem para a mesma URL; --force não os envia.
		if last.URL == c.opt.URL && !force {
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

	// A partir daqui é um arquivo novo: qualquer recusa fica em rootzone_run.
	if published != "" && dl.MD5 != published {
		return nil, c.fail(ctx, run, fmt.Errorf("md5 divergente: publicado %s, baixado %s "+
			"(arquivo e .md5 publicados em momentos diferentes?)", published, dl.MD5))
	}
	ds, err := parse.Parse(bytes.NewReader(dl.Body))
	if err != nil {
		return nil, c.fail(ctx, run, fmt.Errorf("parser: %w", err))
	}
	serial := ds.SOA.Serial
	if !force && last != nil {
		if SerialLess(serial, last.Serial) {
			c.log.Warn("arquivo mais antigo que o aplicado; ignorado (espelho ou servidor com cópia velha?)",
				"serial", serial, "applied_serial", last.Serial, "md5", dl.MD5)
			return c.unchanged(ctx, lastVersion, fmt.Sprintf("arquivo mais antigo que o aplicado (serial %d < %d)", serial, last.Serial))
		}
		if serial == last.Serial {
			// Mesmo serial com conteúdo diferente (o SHA-256 já mostrou): pela
			// RFC 1034 toda mudança na zona incrementa o serial, então é uma
			// republicação (formato, ordem ou assinaturas). Aplica — o MERGE
			// só muda o que de fato mudou — e deixa o aviso registrado.
			ds.Warn("serial %d igual ao aplicado, com conteúdo diferente (sha256 %s); aplicado mesmo assim", serial, dl.SHA256)
		}
	}
	run.Parsed, run.SOA = true, ds.SOA
	run.TLDs, run.Records, run.RRSIGs, run.TypeCounts = len(ds.TLDs), len(ds.Records), ds.RRSIGs, ds.TypeCounts
	run.Warnings = ds.Warnings
	if ds.WarningCount() > len(ds.Warnings) {
		run.Warnings = append(run.Warnings, fmt.Sprintf("... e mais %d avisos", ds.WarningCount()-len(ds.Warnings)))
	}
	if len(ds.TLDs) < c.opt.MinTLDs {
		return nil, c.fail(ctx, run, fmt.Errorf("só %d TLDs na zona (mínimo %d): arquivo truncado?", len(ds.TLDs), c.opt.MinTLDs))
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

// SerialLess diz se o serial a é anterior ao b pela aritmética de seriais da
// RFC 1982 (32 bits): a < b quando são diferentes e b - a (módulo 2³²) é
// menor que 2³¹. Com o formato AAAAMMDDNN da raiz é a comparação numérica
// comum; a RFC só muda o resultado perto da volta de 2³².
func SerialLess(a, b uint32) bool {
	return a != b && b-a < 1<<31
}

func (c *Collector) unchanged(ctx context.Context, version, reason string) (*Result, error) {
	if err := c.store.TouchCheck(ctx); err != nil {
		return nil, fmt.Errorf("jobs: %w", err)
	}
	return &Result{Outcome: Unchanged, Reason: reason, Version: version}, nil
}

// fail grava a recusa em rootzone_run e devolve a causa.
func (c *Collector) fail(ctx context.Context, run store.Run, cause error) error {
	if err := c.store.RecordFailure(context.WithoutCancel(ctx), run, cause); err != nil {
		c.log.Error("não consegui gravar a falha em rootzone_run", "err", err)
	}
	return cause
}
