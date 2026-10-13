// Package derive faz uma passada da derivação do collector-rdns: a cada
// DERIVE_INTERVAL (e na subida) cada instância tenta derivar; a trava deixa
// só uma fazer o trabalho. Lê a versão do dataset de cada origem, deriva os
// /24 dos blocos novos, corrige o ASN das linhas derivadas, faz a manutenção
// (instâncias mortas) e conta os prefixos fora da regra.
package derive

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/patrickbrandao/badblock/apps/rdns/collector/internal/store"
)

// Store é o que a derivação usa (store.Store).
type Store interface {
	Derive(ctx context.Context, sources []string, force bool) (store.DeriveResult, error)
	RecordDeriveFailure(ctx context.Context, run store.DeriveRun, cause error) error
}

// ErrBusy indica que outra instância estava derivando.
var ErrBusy = errors.New("outra instância está derivando")

// Deriver faz as passadas.
type Deriver struct {
	store   Store
	sources []string
	timeout time.Duration
	log     *slog.Logger

	mu        sync.Mutex
	outOfRule int // última contagem avisada (-1 = nunca)
}

// New cria o Deriver.
func New(st Store, sources []string, timeout time.Duration, log *slog.Logger) *Deriver {
	return &Deriver{store: st, sources: sources, timeout: timeout, log: log, outOfRule: -1}
}

// Once faz uma passada com prazo de RUN_TIMEOUT. Sem a trava devolve
// ErrBusy; as demais falhas já saem no log.
func (d *Deriver) Once(ctx context.Context, force bool) error {
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	start := time.Now()
	res, err := d.store.Derive(ctx, d.sources, force)
	if err != nil {
		var de *store.DeriveError
		if errors.As(err, &de) {
			// Falha depois de ler a versão da origem: linha status = 0, fora da
			// transação (que já foi desfeita).
			c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			if rerr := d.store.RecordDeriveFailure(c, de.Run, de.Err); rerr != nil {
				d.log.Error("não consegui gravar a falha em rdns_run", "err", rerr)
			}
			cancel()
		}
		d.log.Error("derivação falhou", "err", err, "ms", time.Since(start).Milliseconds())
		return err
	}
	if !res.Locked {
		d.log.Debug("outra instância está derivando")
		return ErrBusy
	}
	ms := time.Since(start).Milliseconds()
	for _, run := range res.Runs {
		if !run.Applied {
			d.log.Debug("derivação sem mudança", "source", run.Source, "reason", run.Reason, "version", run.Version)
			continue
		}
		d.log.Info("derivação aplicada", "source", run.Source, "version", run.Version, "blocks", run.Blocks,
			"units", run.Units, "inserted", run.Inserted, "asn_updated", run.ASNUpdated, "covered", run.Covered,
			"not_in_source", run.NotInSource, "warnings", len(run.Warnings), "forced", run.Forced, "ms", ms)
		for _, w := range run.Warnings {
			d.log.Warn("aviso da derivação", "source", run.Source, "warning", w)
		}
	}
	if res.WorkersRemoved > 0 {
		d.log.Info("instâncias sem sinal há mais de 24 h apagadas", "count", res.WorkersRemoved)
	}
	d.mu.Lock()
	changed := res.OutOfRule != d.outOfRule
	d.outOfRule = res.OutOfRule
	d.mu.Unlock()
	if changed && res.OutOfRule > 0 {
		d.log.Warn("prefixos fora da regra ignorados", "count", res.OutOfRule)
	}
	return nil
}

// Loop deriva na subida e a cada every, até o contexto acabar.
func (d *Deriver) Loop(ctx context.Context, every time.Duration) {
	for {
		_ = d.Once(ctx, false)
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}
