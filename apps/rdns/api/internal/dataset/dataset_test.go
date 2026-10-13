package dataset

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

type fakeSource struct {
	ok  bool
	err error
}

func (f *fakeSource) HasPrefixes(context.Context) (bool, error) { return f.ok, f.err }

func TestWatcher(t *testing.T) {
	src := &fakeSource{}
	w := NewWatcher(src, 0, slog.New(slog.DiscardHandler))
	if w.Current().Ready() {
		t.Fatal("sem leitura não está pronto")
	}
	if err := w.Refresh(context.Background()); err != nil || w.Current().Ready() {
		t.Errorf("sem prefixos: %v %v", w.Current(), err)
	}
	src.ok = true
	if err := w.Refresh(context.Background()); err != nil || !w.Current().Ready() {
		t.Errorf("com prefixos: %v %v", w.Current(), err)
	}
	// Falha na releitura mantém o estado conhecido.
	src.err = errors.New("fora")
	if err := w.Refresh(context.Background()); err == nil || !w.Current().Ready() {
		t.Errorf("falha: %v %v", w.Current(), err)
	}
}
