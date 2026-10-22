package scan

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/rdap"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/store"
)

type fakeStore struct {
	mu        sync.Mutex
	attempts  []store.Attempt
	processed []string
	leaseLost bool
	n         int
}

func (f *fakeStore) Claim(context.Context, string, time.Duration, time.Duration) (*store.Claim, error) {
	return nil, nil
}
func (f *fakeStore) Release(context.Context, string, *store.Claim) error { return nil }
func (f *fakeStore) SaveAttempt(_ context.Context, _ string, _ *store.Claim, a store.Attempt) (store.Saved, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.leaseLost {
		return store.Saved{}, store.ErrLeaseLost
	}
	f.attempts = append(f.attempts, a)
	f.n++
	if a.Resp.Result == rdap.Error {
		return store.Saved{}, nil
	}
	return store.Saved{ResponseUUID: "resp-" + a.Resp.Result.String(), Changed: true}, nil
}
func (f *fakeStore) Process(_ context.Context, id string) (store.Processed, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.processed = append(f.processed, id)
	return store.Processed{Kind: "autnum"}, nil
}
func (f *fakeStore) RecordProcessError(context.Context, string, string) error { return nil }

// fakeRDAP devolve as respostas em ordem.
type fakeRDAP struct {
	queue []rdap.Response
}

func (f *fakeRDAP) Fetch(context.Context, string, string) rdap.Response {
	r := f.queue[0]
	f.queue = f.queue[1:]
	return r
}

func resp(r rdap.Result, ip string) rdap.Response {
	out := rdap.Response{Result: r, HTTPStatus: 200}
	if ip != "" {
		out.EgressIP = netip.MustParseAddr(ip)
	}
	if r == rdap.Error {
		out.HTTPStatus, out.Err = 503, errors.New("HTTP 503")
	}
	return out
}

func newScanner(st *fakeStore, f *fakeRDAP, opt Options) (*Scanner, *bytes.Buffer) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	opt.Worker, opt.Lease = "w", 2*time.Minute
	opt.Interval = func() time.Duration { return time.Hour }
	return New(st, f, nil, opt, log), &buf
}

func claim(failures int) *store.Claim {
	return &store.Claim{UUID: "t", Kind: rdap.KindASN, ASN: 61610, Failures: failures}
}

func TestBackoff(t *testing.T) {
	want := []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour, 6 * time.Hour, 6 * time.Hour}
	for i, w := range want {
		if got := Backoff(i + 1); got != w {
			t.Errorf("Backoff(%d) = %v, quero %v", i+1, got, w)
		}
	}
	if Backoff(0) != time.Minute {
		t.Error("Backoff(0)")
	}
}

func TestRetryAndProcessing(t *testing.T) {
	st := &fakeStore{}
	f := &fakeRDAP{queue: []rdap.Response{
		resp(rdap.Truncated, ""), resp(rdap.Error, ""), resp(rdap.Complete, ""), resp(rdap.NotFound, ""),
	}}
	s, _ := newScanner(st, f, Options{})
	ctx := context.Background()
	s.Collect(ctx, claim(0)) // truncada: espera de 1 min, sem processar
	s.Collect(ctx, claim(3)) // erro na 4ª seguida: 2 h, com last_error
	s.Collect(ctx, claim(4)) // completa: processa
	s.Collect(ctx, claim(0)) // 404: processa

	if a := st.attempts[0]; a.RetryIn != time.Minute || a.ErrorMsg != "" {
		t.Errorf("truncada = %+v", a)
	}
	if a := st.attempts[1]; a.RetryIn != 2*time.Hour || a.ErrorMsg != "HTTP 503" {
		t.Errorf("erro = %+v", a)
	}
	if a := st.attempts[2]; a.RetryIn != 0 {
		t.Errorf("completa com espera: %+v", a)
	}
	// A truncada nunca é processada.
	if strings.Join(st.processed, ",") != "resp-complete,resp-not_found" {
		t.Errorf("processadas = %v", st.processed)
	}
	var w store.Worker
	s.Stats(&w)
	if w.Requests != 4 || w.Complete != 1 || w.Truncated != 1 || w.Errors != 1 || w.NotFound != 1 {
		t.Errorf("contadores = %+v", w)
	}
}

func TestLeaseLost(t *testing.T) {
	st := &fakeStore{leaseLost: true}
	s, logs := newScanner(st, &fakeRDAP{queue: []rdap.Response{resp(rdap.Complete, "")}}, Options{})
	if !s.Collect(context.Background(), claim(0)) {
		t.Error("lease perdido não é falha do Postgres")
	}
	if len(st.processed) != 0 || !strings.Contains(logs.String(), "lease perdido; resposta descartada") {
		t.Errorf("processadas = %v, log = %s", st.processed, logs)
	}
}

func TestPauseAfterTruncated(t *testing.T) {
	st := &fakeStore{}
	f := &fakeRDAP{queue: []rdap.Response{
		resp(rdap.Truncated, "192.0.2.1"), resp(rdap.Truncated, "192.0.2.1"), resp(rdap.NotFound, ""),
		resp(rdap.Truncated, "192.0.2.1"),
		resp(rdap.Complete, "192.0.2.2"),
		resp(rdap.Truncated, "192.0.2.2"), resp(rdap.Truncated, "192.0.2.2"),
	}}
	s, logs := newScanner(st, f, Options{PauseAfter: 3, Pause: time.Hour})
	ctx := context.Background()
	for range 3 {
		s.Collect(ctx, claim(0))
	}
	if !s.PausedUntil().IsZero() {
		t.Fatal("pausou antes de 3 truncadas (o 404 não zera a conta, mas também não soma)")
	}
	s.Collect(ctx, claim(0)) // 3ª truncada seguida
	until := s.PausedUntil()
	if time.Until(until) < 59*time.Minute {
		t.Fatalf("pausa = %v", until)
	}
	if !strings.Contains(logs.String(), "respostas truncadas seguidas; pausando") {
		t.Errorf("log = %s", logs)
	}
	var w store.Worker
	s.Stats(&w)
	if w.PausedUntil.IsZero() {
		t.Error("paused_until fora da batida")
	}
	// Uma completa zera a conta: duas truncadas depois dela não pausam de novo.
	s.mu.Lock()
	s.pausedUntil = time.Time{}
	s.mu.Unlock()
	for range 3 {
		s.Collect(ctx, claim(0))
	}
	if !s.PausedUntil().IsZero() {
		t.Error("pausou depois da completa com só 2 truncadas")
	}
}

func TestSameEgressWarning(t *testing.T) {
	var queue []rdap.Response
	for range 4 {
		queue = append(queue, resp(rdap.Complete, "192.0.2.1"))
	}
	queue = append(queue, resp(rdap.Complete, "192.0.2.2"))
	for range 3 {
		queue = append(queue, resp(rdap.Complete, "192.0.2.2"))
	}
	st := &fakeStore{}
	s, logs := newScanner(st, &fakeRDAP{queue: queue}, Options{Proxy: true, SameIPWarn: 3})
	for range len(queue) {
		s.Collect(context.Background(), claim(0))
	}
	// Um aviso por sequência: a de 192.0.2.1 (4) e a de 192.0.2.2 (4).
	if n := strings.Count(logs.String(), "proxy sem rotação de IP"); n != 2 {
		t.Errorf("avisos = %d\n%s", n, logs)
	}
	var w store.Worker
	s.Stats(&w)
	if w.EgressIPs != 2 || w.EgressIP != netip.MustParseAddr("192.0.2.2") {
		t.Errorf("egress = %v %d", w.EgressIP, w.EgressIPs)
	}

	// Sem proxy, não há aviso.
	st = &fakeStore{}
	s, logs = newScanner(st, &fakeRDAP{queue: []rdap.Response{resp(rdap.Complete, "192.0.2.1"),
		resp(rdap.Complete, "192.0.2.1"), resp(rdap.Complete, "192.0.2.1")}}, Options{SameIPWarn: 2})
	for range 3 {
		s.Collect(context.Background(), claim(0))
	}
	if strings.Contains(logs.String(), "proxy sem rotação") {
		t.Error("aviso sem proxy")
	}
}

func TestEgressRing(t *testing.T) {
	st := &fakeStore{}
	var queue []rdap.Response
	for i := range 150 {
		queue = append(queue, resp(rdap.Complete, netip.AddrFrom4([4]byte{192, 0, 2, byte(i)}).String()))
	}
	s, _ := newScanner(st, &fakeRDAP{queue: queue}, Options{})
	for range 150 {
		s.Collect(context.Background(), claim(0))
	}
	var w store.Worker
	s.Stats(&w)
	if w.EgressIPs != ringSize {
		t.Errorf("IPs distintos nas últimas 100 = %d", w.EgressIPs)
	}
}
