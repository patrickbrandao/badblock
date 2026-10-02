// Package dataset acompanha a versão atual dos dados da fonte rootanchors.
//
// A versão é o uuid da última execução aplicada pelo collector-rootanchors
// (rootanchors_run). Ela entra nas chaves do cache e no ETag: quando o
// collector aplica um arquivo novo, as respostas antigas deixam de ser usadas
// sem precisar apagar nada no Valkey.
package dataset

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/patrickbrandao/badblock/apps/rootanchors/api/internal/store"
)

// Snapshot é a versão vista num dado momento.
type Snapshot struct {
	Version   string
	AppliedAt time.Time
}

// Ready diz se já existe algum dado carregado.
func (s Snapshot) Ready() bool { return s.Version != "" }

// Source é de onde a versão é lida (store.Store).
type Source interface {
	Dataset(ctx context.Context) (*store.Dataset, error)
}

// Watcher consulta a versão periodicamente.
type Watcher struct {
	src     Source
	every   time.Duration
	log     *slog.Logger
	current atomic.Pointer[Snapshot]
}

// NewWatcher cria o watcher; chame Refresh uma vez antes de servir.
func NewWatcher(src Source, every time.Duration, log *slog.Logger) *Watcher {
	w := &Watcher{src: src, every: every, log: log}
	w.current.Store(&Snapshot{})
	return w
}

// Current devolve a versão atual.
func (w *Watcher) Current() Snapshot { return *w.current.Load() }

// Refresh lê a versão agora.
func (w *Watcher) Refresh(ctx context.Context) error {
	d, err := w.src.Dataset(ctx)
	if err != nil {
		return err
	}
	next := Snapshot{}
	if d != nil {
		next = Snapshot{Version: d.Version, AppliedAt: d.AppliedAt}
	}
	if prev := w.Current(); prev.Version != next.Version {
		w.log.Info("versão do dataset", "version", next.Version, "previous", prev.Version, "applied_at", next.AppliedAt)
	}
	w.current.Store(&next)
	return nil
}

// Run atualiza a versão a cada intervalo até o contexto acabar.
func (w *Watcher) Run(ctx context.Context) {
	t := time.NewTicker(w.every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c, cancel := context.WithTimeout(ctx, 5*time.Second)
			if err := w.Refresh(c); err != nil {
				w.log.Warn("não consegui ler a versão do dataset", "err", err)
			}
			cancel()
		}
	}
}
