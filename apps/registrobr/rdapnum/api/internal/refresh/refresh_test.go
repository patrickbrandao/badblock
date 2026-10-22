package refresh

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// FakeBackend é um Valkey em memória.
type fakeBackend struct {
	mu       sync.Mutex
	down     error // falha em tudo
	failAt   string
	marks    map[string]bool
	queue    []Request
	extraLen int64
}

func newFake() *fakeBackend { return &fakeBackend{marks: map[string]bool{}} }

func (f *fakeBackend) fail(op string) error {
	if f.down != nil {
		return f.down
	}
	if f.failAt == op {
		return errors.New(op + " falhou")
	}
	return nil
}

func (f *fakeBackend) Ping(context.Context) error { return f.fail("ping") }

func (f *fakeBackend) Claim(_ context.Context, mark string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail("claim"); err != nil {
		return false, err
	}
	if f.marks[mark] {
		return false, nil
	}
	f.marks[mark] = true
	return true, nil
}

func (f *fakeBackend) Release(_ context.Context, mark string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.marks, mark)
	return nil
}

func (f *fakeBackend) QueueLen(context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail("llen"); err != nil {
		return 0, err
	}
	return int64(len(f.queue)) + f.extraLen, nil
}

func (f *fakeBackend) Push(_ context.Context, r Request) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail("push"); err != nil {
		return err
	}
	f.queue = append(f.queue, r)
	return nil
}

var now = time.Date(2026, 10, 21, 21, 10, 0, 123e6, time.UTC)

func newReq(b Backend) *Requester {
	r := NewRequester(b, 10*time.Minute, 3)
	r.now = func() time.Time { return now }
	return r
}

func TestQueued(t *testing.T) {
	b := newFake()
	r := newReq(b)
	old := now.Add(-time.Hour)
	out, err := r.Request(context.Background(), "asn", "61610", &old)
	if err != nil || !out.Queued || out.Reason != "" {
		t.Fatalf("out = %+v, %v", out, err)
	}
	if len(b.queue) != 1 || b.queue[0] != (Request{Kind: "asn", Target: "61610", At: "2026-10-21T21:10:00.123Z"}) {
		t.Errorf("fila = %+v", b.queue)
	}
	if !b.marks["asn:61610"] {
		t.Error("sem a marca inflight")
	}
	// Nunca coletado também entra.
	if out, err := r.Request(context.Background(), "prefix", "200.225.48.0/21", nil); err != nil || !out.Queued {
		t.Errorf("nunca coletado = %+v, %v", out, err)
	}
}

func TestRecentAndAlreadyQueued(t *testing.T) {
	b := newFake()
	r := newReq(b)
	recent := now.Add(-5 * time.Minute)
	if out, err := r.Request(context.Background(), "asn", "61610", &recent); err != nil || out.Queued || out.Reason != Recent {
		t.Errorf("recent = %+v, %v", out, err)
	}
	if len(b.queue) != 0 || len(b.marks) != 0 {
		t.Error("recent não pede nada")
	}
	b.marks["asn:61610"] = true
	if out, err := r.Request(context.Background(), "asn", "61610", nil); err != nil || out.Queued || out.Reason != AlreadyQueued {
		t.Errorf("already_queued = %+v, %v", out, err)
	}
	if len(b.queue) != 0 || !b.marks["asn:61610"] {
		t.Error("already_queued não mexe na fila nem na marca")
	}
}

func TestQueueFull(t *testing.T) {
	b := newFake()
	b.extraLen = 3
	r := newReq(b)
	if _, err := r.Request(context.Background(), "asn", "61610", nil); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("err = %v", err)
	}
	if len(b.marks) != 0 || len(b.queue) != 0 {
		t.Errorf("a marca tinha de ser apagada: %v", b.marks)
	}
}

func TestUnavailable(t *testing.T) {
	for _, op := range []string{"ping", "claim", "llen", "push"} {
		b := newFake()
		b.failAt = op
		_, err := newReq(b).Request(context.Background(), "asn", "61610", nil)
		if !errors.Is(err, ErrUnavailable) {
			t.Errorf("%s: err = %v", op, err)
		}
		if len(b.marks) != 0 {
			t.Errorf("%s: marca sobrou: %v", op, b.marks)
		}
	}
	// Valkey fora vem antes do recent.
	b := newFake()
	b.down = errors.New("fora")
	recent := now.Add(-time.Minute)
	if _, err := newReq(b).Request(context.Background(), "asn", "61610", &recent); !errors.Is(err, ErrUnavailable) {
		t.Errorf("fora + recent = %v", err)
	}
}
