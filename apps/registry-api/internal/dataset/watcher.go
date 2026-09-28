// Package dataset acompanha a versão atual do central. O registry-sync avisa
// cada versão nova com NOTIFY badblock_dataset; a API também consulta a
// versão periodicamente, caso a notificação se perca (reconexão, por exemplo).
package dataset

import (
	"context"
	"errors"
	"log/slog"
	"net/netip"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/patrickbrandao/badblock/apps/registry-api/internal/store"
)

// Snapshot é a versão atual e a lista de blocos com cache por IP.
type Snapshot struct {
	Version    int64
	BuiltAt    time.Time
	Exceptions map[netip.Prefix]struct{}
}

// Ready diz se já existe alguma versão do central.
func (s Snapshot) Ready() bool { return s.Version > 0 }

// Source é o que o Watcher precisa do banco.
type Source interface {
	CurrentDataset(ctx context.Context) (*store.Dataset, error)
	CacheExceptions(ctx context.Context) ([]netip.Prefix, error)
	URL() string
}

// Watcher mantém o Snapshot atualizado.
type Watcher struct {
	src  Source
	poll time.Duration
	log  *slog.Logger

	mu  sync.RWMutex
	cur Snapshot
}

// NewWatcher cria o Watcher; chame Refresh antes de servir e Run em segundo plano.
func NewWatcher(src Source, poll time.Duration, log *slog.Logger) *Watcher {
	return &Watcher{src: src, poll: poll, log: log, cur: Snapshot{Exceptions: map[netip.Prefix]struct{}{}}}
}

// Current devolve a versão em uso.
func (w *Watcher) Current() Snapshot {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.cur
}

// Refresh relê a versão e, se mudou, a lista de exceções.
func (w *Watcher) Refresh(ctx context.Context) error {
	d, err := w.src.CurrentDataset(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if d.Version == w.Current().Version {
		return nil
	}
	list, err := w.src.CacheExceptions(ctx)
	if err != nil {
		return err
	}
	exc := make(map[netip.Prefix]struct{}, len(list))
	for _, p := range list {
		exc[p] = struct{}{}
	}
	w.mu.Lock()
	old := w.cur.Version
	w.cur = Snapshot{Version: d.Version, BuiltAt: d.BuiltAt, Exceptions: exc}
	w.mu.Unlock()
	w.log.Info("versão do dataset", "from", old, "to", d.Version, "cache_exceptions", len(exc))
	return nil
}

// Run escuta o canal badblock_dataset até o contexto acabar, reconectando se
// preciso, e consulta a versão a cada poll mesmo sem notificação.
func (w *Watcher) Run(ctx context.Context) {
	backoff := time.Second
	for ctx.Err() == nil {
		err := w.listen(ctx)
		if ctx.Err() != nil {
			return
		}
		w.log.Warn("LISTEN interrompido; reconectando", "err", err, "in", backoff.String())
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, time.Minute)
		// Enquanto não reconecta, a versão continua sendo consultada.
		if err := w.Refresh(ctx); err != nil {
			w.log.Warn("não foi possível consultar a versão", "err", err)
		}
	}
}

func (w *Watcher) listen(ctx context.Context) error {
	conn, err := pgx.Connect(ctx, w.src.URL())
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())
	if _, err := conn.Exec(ctx, "LISTEN badblock_dataset"); err != nil {
		return err
	}
	// A notificação pode ter passado enquanto a conexão não existia.
	if err := w.Refresh(ctx); err != nil {
		w.log.Warn("não foi possível consultar a versão", "err", err)
	}
	for {
		wctx, cancel := context.WithTimeout(ctx, w.poll)
		_, err := conn.WaitForNotification(wctx)
		cancel()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		// Notificação recebida ou fim do intervalo: confere a versão.
		if err := w.Refresh(ctx); err != nil {
			w.log.Warn("não foi possível consultar a versão", "err", err)
		}
	}
}
