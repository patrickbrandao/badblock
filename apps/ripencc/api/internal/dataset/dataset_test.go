package dataset

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/patrickbrandao/badblock/apps/ripencc/api/internal/store"
)

type fakeSource struct {
	d   *store.Dataset
	err error
}

func (f *fakeSource) Dataset(context.Context) (*store.Dataset, error) { return f.d, f.err }

func TestWatcherRefresh(t *testing.T) {
	src := &fakeSource{}
	w := NewWatcher(src, time.Minute, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := context.Background()

	if err := w.Refresh(ctx); err != nil || w.Current().Ready() {
		t.Fatalf("sem carga: ready = %v, err = %v", w.Current().Ready(), err)
	}
	at := time.Unix(100, 0)
	src.d = &store.Dataset{Version: "v1", AppliedAt: at}
	if err := w.Refresh(ctx); err != nil || w.Current() != (Snapshot{Version: "v1", AppliedAt: at}) {
		t.Fatalf("carga: %+v, err = %v", w.Current(), err)
	}
	// Erro de leitura mantém a versão conhecida.
	src.err = errors.New("down")
	if err := w.Refresh(ctx); err == nil || w.Current().Version != "v1" {
		t.Errorf("erro: %+v, err = %v", w.Current(), err)
	}
	src.err, src.d = nil, &store.Dataset{Version: "v2", AppliedAt: at.Add(time.Hour)}
	if err := w.Refresh(ctx); err != nil || w.Current().Version != "v2" {
		t.Errorf("versão nova: %+v, err = %v", w.Current(), err)
	}
}

func TestWatcherRunStopsWithContext(t *testing.T) {
	src := &fakeSource{d: &store.Dataset{Version: "v1"}}
	w := NewWatcher(src, 5*time.Millisecond, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(ctx); close(done) }()
	deadline := time.Now().Add(2 * time.Second)
	for !w.Current().Ready() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if w.Current().Version != "v1" {
		t.Errorf("Run não atualizou: %+v", w.Current())
	}
}
