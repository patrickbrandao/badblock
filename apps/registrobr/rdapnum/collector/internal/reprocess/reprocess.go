// Package reprocess processa as respostas cruas nas tabelas processadas: a
// de cada coleta conclusiva (ProcessOne, chamado pela fila) e, a cada
// minuto, as respostas atuais que ainda precisam de processamento — parser
// de outra versão, nunca processadas (coleta que caiu entre as duas
// transações) ou recusadas há mais de 1 h (collector.md#reprocessamento).
package reprocess

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/parse"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/store"
)

// Every é o intervalo do laço; Batch, quantas respostas por volta.
const (
	Every = time.Minute
	Batch = 100
)

// Processor processa uma resposta (store.Store).
type Processor interface {
	Process(ctx context.Context, responseUUID string) (store.Processed, error)
	RecordProcessError(ctx context.Context, responseUUID, msg string) error
}

// Store é o que o laço usa (store.Store).
type Store interface {
	Processor
	PendingProcess(ctx context.Context, version, limit int) ([]string, error)
}

// ProcessOne processa uma resposta e registra o resultado no log. Uma falha
// fora do parser (banco) também fica em process_error: o laço tenta de novo
// depois de 1 h. Devolve se o processamento gravou (ou recusou) a resposta.
func ProcessOne(ctx context.Context, st Processor, id string, log *slog.Logger) bool {
	start := time.Now()
	p, err := st.Process(ctx, id)
	switch {
	case err != nil:
		if ctx.Err() != nil {
			return false
		}
		log.Warn("processamento recusado", "response", id, "err", err)
		c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if rerr := st.RecordProcessError(c, id, err.Error()); rerr != nil {
			log.Error("não consegui gravar process_error", "response", id, "err", rerr)
		}
		return false
	case p.Skipped:
		log.Debug("resposta em processamento por outra instância", "response", id)
		return false
	case p.Stale:
		log.Debug("resposta não é mais a atual do alvo", "response", id)
		return true
	case p.Refused != nil:
		log.Warn("processamento recusado", "response", id, "err", parse.Reason(p.Refused))
		return true
	}
	log.Debug("resposta processada", "response", id, "kind", p.Kind, "changed", p.Changed,
		"ms", time.Since(start).Milliseconds())
	return true
}

// Loop é o laço de reprocessamento de uma instância.
type Loop struct {
	store Store
	log   *slog.Logger
	every time.Duration
}

// New cria o laço.
func New(st Store, log *slog.Logger) *Loop {
	return &Loop{store: st, log: log, every: Every}
}

// Once faz uma volta: até Batch respostas, uma a uma. Devolve quantas
// processou.
func (l *Loop) Once(ctx context.Context) (int, error) {
	start := time.Now()
	ids, err := l.store.PendingProcess(ctx, parse.Version, Batch)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		if ProcessOne(ctx, l.store, id, l.log) {
			n++
		}
	}
	if len(ids) > 0 {
		l.log.Info("reprocessamento", "count", n, "ms", time.Since(start).Milliseconds())
	}
	return n, nil
}

// Run faz uma volta a cada minuto até o contexto acabar.
func (l *Loop) Run(ctx context.Context) {
	for {
		if _, err := l.Once(ctx); err != nil && !errors.Is(err, context.Canceled) && ctx.Err() == nil {
			l.log.Error("reprocessamento falhou", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(l.every):
		}
	}
}
