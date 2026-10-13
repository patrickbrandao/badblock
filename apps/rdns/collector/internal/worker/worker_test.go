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

	"github.com/patrickbrandao/badblock/apps/rdns/collector/internal/store"
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

func newWorker(st *fakeStore, lim *fakeLimiter, lease time.Duration) (*Worker, *bytes.Buffer) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	w := New(st, lim, Options{UUID: "u", Host: "h", Version: "v", RateLimit: 256, ScanInterval: 24 * time.Hour,
		ScanLease: lease, Counters: Counters{Queries: func() int64 { return 7 }}}, log)
	return w, &buf
}

func TestConfigFromTable(t *testing.T) {
	st, lim := &fakeStore{}, &fakeLimiter{}
	w, logs := newWorker(st, lim, 2*time.Minute)
	if lim.rate != 256 || w.ScanInterval() != 24*time.Hour {
		t.Fatalf("reservas: %d %v", lim.rate, w.ScanInterval())
	}

	st.cfg = map[string]string{"rate_limit": "512", "scan_interval": "12h"}
	if err := w.Beat(context.Background()); err != nil {
		t.Fatal(err)
	}
	if lim.rate != 512 || w.Rate() != 512 || w.ScanInterval() != 12*time.Hour {
		t.Errorf("tabela: %d %d %v", lim.rate, w.Rate(), w.ScanInterval())
	}
	if !strings.Contains(logs.String(), "configuração alterada") {
		t.Errorf("log = %s", logs)
	}
	if b := st.beats[len(st.beats)-1]; b.RateLimit != 512 || b.Queries != 7 || b.UUID != "u" {
		t.Errorf("batida = %+v", b)
	}

	// Valor inválido: a reserva, com um aviso só (mesmo valor, mesma batida).
	st.cfg = map[string]string{"rate_limit": "muito", "scan_interval": "12h"}
	_ = w.Beat(context.Background())
	_ = w.Beat(context.Background())
	if w.Rate() != 256 {
		t.Errorf("inválido: rate = %d, quero a reserva", w.Rate())
	}
	if n := strings.Count(logs.String(), "valor inválido em rdns_config"); n != 1 {
		t.Errorf("avisos de valor inválido = %d", n)
	}

	// Banco fora: valem as reservas.
	st.cfg, st.err = nil, errors.New("fora")
	if err := w.Beat(context.Background()); err == nil {
		t.Error("Beat deveria devolver o erro da leitura")
	}
	if w.Rate() != 256 || w.ScanInterval() != 24*time.Hour {
		t.Errorf("banco fora: %d %v", w.Rate(), w.ScanInterval())
	}
}

func TestLowRateWarning(t *testing.T) {
	st, lim := &fakeStore{}, &fakeLimiter{}
	_, logs := newWorker(st, lim, 2*time.Minute)
	if strings.Contains(logs.String(), "taxa baixa demais") {
		t.Errorf("256/s com lease de 2m não deveria avisar: %s", logs)
	}
	w, logs := newWorker(st, lim, 2*time.Minute)
	w.Apply(map[string]string{"rate_limit": "10"}) // 10 × 115 s = 1.150 < 2.048
	if !strings.Contains(logs.String(), "taxa baixa demais para SCAN_LEASE") {
		t.Errorf("deveria avisar: %s", logs)
	}
}

func TestNewUUID(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	a, b := NewUUID(), NewUUID()
	if !re.MatchString(a) || a == b {
		t.Errorf("uuids = %s %s", a, b)
	}
}
