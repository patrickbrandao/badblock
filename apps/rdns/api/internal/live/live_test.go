package live

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeBackend é o Valkey em memória; Wait bloqueia até release fechar.
type fakeBackend struct {
	mu       sync.Mutex
	recent   map[string]Mark
	inflight map[string]bool
	pushed   []Request
	queueLen int64
	release  chan struct{}
	waiting  atomic.Int64
	timeout  bool
	err      error
	released []string
}

func newBackend() *fakeBackend {
	return &fakeBackend{recent: map[string]Mark{}, inflight: map[string]bool{}, release: make(chan struct{})}
}

func (b *fakeBackend) Recent(_ context.Context, ip string) (*Mark, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.err != nil {
		return nil, b.err
	}
	if m, ok := b.recent[ip]; ok {
		return &m, nil
	}
	return nil, nil
}

func (b *fakeBackend) Claim(_ context.Context, ip string) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.inflight[ip] {
		return false, nil
	}
	b.inflight[ip] = true
	return true, nil
}

func (b *fakeBackend) QueueLen(context.Context) (int64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.queueLen, nil
}

func (b *fakeBackend) Release(_ context.Context, ip string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.inflight, ip)
	b.released = append(b.released, ip)
	return nil
}

func (b *fakeBackend) Push(_ context.Context, req Request) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pushed = append(b.pushed, req)
	return nil
}

func (b *fakeBackend) Wait(_ context.Context, ip string, _ time.Duration) (*Reply, error) {
	b.waiting.Add(1)
	defer b.waiting.Add(-1)
	<-b.release
	if b.timeout {
		return nil, nil
	}
	return &Reply{IP: ip, Result: "ptr", Names: []string{"registro.br"}, CheckedAt: "2026-10-11T03:00:00Z", Worker: "w"}, nil
}

func (b *fakeBackend) SetRecent(_ context.Context, ip string, m Mark, _ time.Duration) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.recent[ip] = m
	return nil
}

func (b *fakeBackend) Ping(context.Context) error { return b.err }

func (b *fakeBackend) pushes() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.pushed)
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("prazo esgotado")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestCoalescing(t *testing.T) {
	b := newBackend()
	r := NewRequester(b, time.Second, time.Minute, 1000)
	var rereads atomic.Int64
	reread := func(context.Context) ([]byte, error) {
		rereads.Add(1)
		return []byte(`{"ip":"200.160.2.3"}`), nil
	}

	var wg sync.WaitGroup
	outs := make([]Outcome, 2)
	wg.Go(func() { outs[0], _ = r.Resolve(context.Background(), "200.160.2.3", reread) })
	waitFor(t, func() bool { return b.waiting.Load() == 1 })
	wg.Go(func() { outs[1], _ = r.Resolve(context.Background(), "200.160.2.3", reread) })
	waitFor(t, func() bool { return r.waits.Load() == 2 })
	time.Sleep(50 * time.Millisecond) // o segundo entra no singleflight do primeiro
	close(b.release)
	wg.Wait()

	if b.pushes() != 1 || rereads.Load() != 1 {
		t.Errorf("dois pedidos: %d RPUSH, %d releituras; quero 1 e 1", b.pushes(), rereads.Load())
	}
	for i, o := range outs {
		if o.Status != Resolved || o.Result != "ptr" || string(o.Body) != `{"ip":"200.160.2.3"}` {
			t.Errorf("pedido %d = %+v", i, o)
		}
	}
	if b.pushed[0].IP != "200.160.2.3" || len(b.pushed[0].At) != len("2026-10-10T14:03:11.123Z") {
		t.Errorf("pedido na fila = %+v", b.pushed[0])
	}
	// A marca recent foi gravada: o próximo não pede de novo.
	o, err := r.Resolve(context.Background(), "200.160.2.3", reread)
	if err != nil || o.Status != Recent || o.CheckedAt != "2026-10-11T03:00:00Z" || b.pushes() != 1 {
		t.Errorf("recent = %+v, %v", o, err)
	}
}

func TestAlreadyQueuedOnlyWaits(t *testing.T) {
	// Outra instância já marcou o IP: esta não enfileira, só espera.
	b := newBackend()
	b.inflight["200.160.2.3"] = true
	close(b.release)
	r := NewRequester(b, time.Second, time.Minute, 1000)
	o, err := r.Resolve(context.Background(), "200.160.2.3", func(context.Context) ([]byte, error) { return []byte("{}"), nil })
	if err != nil || o.Status != Resolved || b.pushes() != 0 {
		t.Errorf("= %+v, %v, %d RPUSH", o, err, b.pushes())
	}
}

func TestQueueFull(t *testing.T) {
	b := newBackend()
	b.queueLen = 1000
	r := NewRequester(b, time.Second, time.Minute, 1000)
	if _, err := r.Resolve(context.Background(), "200.160.2.3", nil); !errors.Is(err, ErrQueueFull) {
		t.Errorf("fila cheia = %v", err)
	}
	if len(b.released) != 1 || b.inflight["200.160.2.3"] {
		t.Error("fila cheia: a marca inflight deveria ser apagada")
	}
}

func TestMaxWaits(t *testing.T) {
	b := newBackend()
	r := NewRequester(b, time.Second, time.Minute, 100000)
	reread := func(context.Context) ([]byte, error) { return []byte("{}"), nil }
	var wg sync.WaitGroup
	for i := range MaxWaits {
		wg.Go(func() { _, _ = r.Resolve(context.Background(), fmt.Sprintf("10.0.%d.%d", i/256, i%256), reread) })
	}
	waitFor(t, func() bool { return b.waiting.Load() == MaxWaits })
	if _, err := r.Resolve(context.Background(), "10.9.9.9", reread); !errors.Is(err, ErrQueueFull) {
		t.Errorf("acima de %d esperas = %v", MaxWaits, err)
	}
	close(b.release)
	wg.Wait()
	if r.waits.Load() != 0 {
		t.Errorf("esperas depois = %d", r.waits.Load())
	}
}

func TestTimeoutAndUnavailable(t *testing.T) {
	b := newBackend()
	b.timeout = true
	close(b.release)
	r := NewRequester(b, time.Second, time.Minute, 1000)
	if o, err := r.Resolve(context.Background(), "200.160.2.3", nil); err != nil || o.Status != Timeout || o.Result != "" {
		t.Errorf("timeout = %+v, %v", o, err)
	}

	b = newBackend()
	b.err = errors.New("down")
	r = NewRequester(b, time.Second, time.Minute, 1000)
	if o, err := r.Resolve(context.Background(), "200.160.2.3", nil); err != nil || o.Status != Unavailable {
		t.Errorf("Valkey fora = %+v, %v", o, err)
	}
}

func TestRereadError(t *testing.T) {
	b := newBackend()
	close(b.release)
	r := NewRequester(b, time.Second, time.Minute, 1000)
	boom := errors.New("banco fora")
	if _, err := r.Resolve(context.Background(), "200.160.2.3", func(context.Context) ([]byte, error) { return nil, boom }); !errors.Is(err, boom) {
		t.Errorf("erro da releitura = %v", err)
	}
	if _, ok := b.recent["200.160.2.3"]; ok {
		t.Error("sem releitura não há marca recent")
	}
}
