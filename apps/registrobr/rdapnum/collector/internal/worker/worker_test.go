package worker

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/store"
)

type fakeStore struct {
	cfg   map[string]string
	err   error
	beats []store.Worker
}

func (f *fakeStore) Config(context.Context) (map[string]string, error) { return f.cfg, f.err }
func (f *fakeStore) Heartbeat(_ context.Context, w store.Worker) error {
	f.beats = append(f.beats, w)
	return nil
}

type fakeLimiter struct{ rate int }

func (f *fakeLimiter) SetRate(r int) { f.rate = r }

func newWorker(st *fakeStore, lim *fakeLimiter, proxy bool) (*Worker, *bytes.Buffer) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	w := New(st, lim, Options{UUID: "u", Host: "h", Version: "v", RateLimit: 32, ScanInterval: 168 * time.Hour,
		Proxy: proxy, Stats: func(w *store.Worker) { w.Requests = 7 }, Refreshes: func() int64 { return 2 }}, log)
	return w, &buf
}

func TestConfigFromTable(t *testing.T) {
	st, lim := &fakeStore{}, &fakeLimiter{}
	w, logs := newWorker(st, lim, true)
	if lim.rate != 32 || w.ScanInterval() != 168*time.Hour {
		t.Fatalf("reservas: %d %v", lim.rate, w.ScanInterval())
	}
	st.cfg = map[string]string{"rate_limit": "48", "scan_interval": "72h"}
	if err := w.Beat(context.Background()); err != nil {
		t.Fatal(err)
	}
	if lim.rate != 48 || w.Rate() != 48 || w.ScanInterval() != 72*time.Hour {
		t.Errorf("tabela: %d %d %v", lim.rate, w.Rate(), w.ScanInterval())
	}
	if !strings.Contains(logs.String(), "configuração alterada") {
		t.Errorf("log = %s", logs)
	}
	if b := st.beats[len(st.beats)-1]; b.RateLimit != 48 || b.Requests != 7 || b.Refreshes != 2 || !b.Proxy || b.UUID != "u" {
		t.Errorf("batida = %+v", b)
	}

	// Valor inválido: a reserva, com um aviso só por valor.
	st.cfg = map[string]string{"rate_limit": "zero"}
	_ = w.Beat(context.Background())
	_ = w.Beat(context.Background())
	if w.Rate() != 32 || w.ScanInterval() != 168*time.Hour {
		t.Errorf("inválido: %d %v", w.Rate(), w.ScanInterval())
	}
	if n := len(regexp.MustCompile("valor inválido em registrobr_rdapnum_config").FindAllString(logs.String(), -1)); n != 1 {
		t.Errorf("avisos = %d", n)
	}

	// Tabela fora do ar: as reservas, e a batida devolve o erro.
	st.cfg, st.err = map[string]string{"rate_limit": "64"}, errors.New("fora")
	if err := w.Beat(context.Background()); err == nil || w.Rate() != 32 {
		t.Errorf("fora: %v %d", err, w.Rate())
	}
}

func TestRateWithoutProxyWarning(t *testing.T) {
	st, lim := &fakeStore{}, &fakeLimiter{}
	_, logs := newWorker(st, lim, false)
	if !strings.Contains(logs.String(), "taxa acima de 8/min sem proxy") {
		t.Errorf("sem aviso na subida: %s", logs)
	}
	st2 := &fakeStore{cfg: map[string]string{"rate_limit": "8"}}
	w, logs := newWorker(st2, lim, false)
	logs.Reset()
	_ = w.Beat(context.Background())
	if strings.Contains(logs.String(), "taxa acima") {
		t.Errorf("aviso a 8/min: %s", logs)
	}
	_, logs = newWorker(&fakeStore{}, lim, true)
	if strings.Contains(logs.String(), "taxa acima") {
		t.Errorf("aviso com proxy: %s", logs)
	}
}
