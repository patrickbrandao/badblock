// Package dataset acompanha se a api-rdns já tem dados: há algum prefixo em
// rdns_prefix? O rdns não tem versão de dataset (o dado muda um prefixo de
// cada vez, o dia inteiro), então a pergunta se reduz a essa; antes do
// primeiro prefixo as rotas de dados respondem 503 dataset_not_ready.
package dataset

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

// Snapshot é o estado visto num dado momento.
type Snapshot struct {
	HasPrefixes bool
}

// Ready diz se já existe algum prefixo cadastrado.
func (s Snapshot) Ready() bool { return s.HasPrefixes }

// Source é de onde o estado é lido (store.Store).
type Source interface {
	HasPrefixes(ctx context.Context) (bool, error)
}

// Watcher consulta o estado periodicamente.
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

// Current devolve o estado atual.
func (w *Watcher) Current() Snapshot { return *w.current.Load() }

// Refresh lê o estado agora.
func (w *Watcher) Refresh(ctx context.Context) error {
	ok, err := w.src.HasPrefixes(ctx)
	if err != nil {
		return err
	}
	if prev := w.Current(); prev.HasPrefixes != ok {
		w.log.Info("prefixos cadastrados", "ready", ok)
	}
	w.current.Store(&Snapshot{HasPrefixes: ok})
	return nil
}

// Run relê o estado a cada intervalo até o contexto acabar.
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
				w.log.Warn("não consegui conferir se há prefixos", "err", err)
			}
			cancel()
		}
	}
}
