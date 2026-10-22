// Package dataset acompanha se a api-registrobr-rdapnum já tem dados: há
// algum alvo em registrobr_rdapnum_target? O rdapnum não tem versão de
// dataset (o dado muda um alvo de cada vez, o dia inteiro — decisão #42), então
// a pergunta se reduz a essa; antes do primeiro alvo as rotas de dados
// respondem 503 dataset_not_ready.
package dataset

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

// Snapshot é o estado visto num dado momento.
type Snapshot struct {
	HasTargets bool
}

// Ready diz se já existe algum alvo cadastrado.
func (s Snapshot) Ready() bool { return s.HasTargets }

// Source é de onde o estado é lido (store.Store).
type Source interface {
	HasTargets(ctx context.Context) (bool, error)
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
	ok, err := w.src.HasTargets(ctx)
	if err != nil {
		return err
	}
	if prev := w.Current(); prev.HasTargets != ok {
		w.log.Info("alvos cadastrados", "ready", ok)
	}
	w.current.Store(&Snapshot{HasTargets: ok})
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
				w.log.Warn("não consegui conferir se há alvos", "err", err)
			}
			cancel()
		}
	}
}
