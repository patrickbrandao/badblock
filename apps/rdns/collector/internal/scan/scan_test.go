package scan

import (
	"context"
	"log/slog"
	"net/netip"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/patrickbrandao/badblock/apps/rdns/collector/internal/limiter"
	"github.com/patrickbrandao/badblock/apps/rdns/collector/internal/resolver"
	"github.com/patrickbrandao/badblock/apps/rdns/collector/internal/store"
)

// fakeStore é a fila e rdns_ptr em memória; a gravação usa as mesmas regras
// do store real (store.Decide).
type fakeStore struct {
	mu       sync.Mutex
	queue    []*store.Claim
	ptr      map[netip.Addr]*store.Row
	saved    []string // prefixos gravados
	released []string // prefixos abandonados
	changed  []int
	calls    int  // chamadas de SaveScan
	leaseErr bool // SaveScan devolve ErrLeaseLost
}

func newFakeStore(prefixes ...string) *fakeStore {
	f := &fakeStore{ptr: map[netip.Addr]*store.Row{}}
	for i, p := range prefixes {
		f.queue = append(f.queue, &store.Claim{UUID: string(rune('a' + i)), Prefix: netip.MustParsePrefix(p), StartedAt: time.Now()})
	}
	return f
}

func (f *fakeStore) Claim(context.Context, string, time.Duration, time.Duration) (*store.Claim, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.queue) == 0 {
		return nil, nil
	}
	c := f.queue[0]
	f.queue = f.queue[1:]
	return c, nil
}

func (f *fakeStore) SaveScan(_ context.Context, _ string, c *store.Claim, results []resolver.Result, _ time.Duration) (store.Counts, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	counts := store.Tally(results)
	f.calls++
	if f.leaseErr {
		return counts, store.ErrLeaseLost
	}
	for _, r := range results {
		switch store.Decide(f.ptr[r.IP], r) {
		case store.Insert, store.Update:
			f.ptr[r.IP] = &store.Row{Status: r.Status, Names: r.Names}
			counts.Changed++
		case store.Delete:
			delete(f.ptr, r.IP)
			counts.Changed++
		}
	}
	f.saved = append(f.saved, c.Prefix.String())
	f.changed = append(f.changed, counts.Changed)
	return counts, nil
}

func (f *fakeStore) Release(_ context.Context, _ string, c *store.Claim) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.released = append(f.released, c.Prefix.String())
	return nil
}

func (f *fakeStore) snapshot() (saved, released []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.saved), slices.Clone(f.released)
}

// fakeResolver responde o que answers manda (o resto é no_ptr), depois de
// delay, e conta quantos prefixos têm consultas em andamento ao mesmo tempo.
type fakeResolver struct {
	delay   time.Duration
	answers map[netip.Addr]resolver.Result

	mu       sync.Mutex
	inFlight map[netip.Prefix]int
	maxPfx   int
}

func (r *fakeResolver) Lookup(ctx context.Context, ip netip.Addr, _ limiter.Priority, first *limiter.Ticket) (resolver.Result, error) {
	if err := first.Wait(ctx); err != nil {
		return resolver.Result{}, err
	}
	pfx := netip.PrefixFrom(ip, 24).Masked()
	r.mu.Lock()
	if r.inFlight == nil {
		r.inFlight = map[netip.Prefix]int{}
	}
	r.inFlight[pfx]++
	r.maxPfx = max(r.maxPfx, len(r.inFlight))
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		if r.inFlight[pfx]--; r.inFlight[pfx] == 0 {
			delete(r.inFlight, pfx)
		}
		r.mu.Unlock()
	}()
	select {
	case <-time.After(r.delay):
	case <-ctx.Done():
		return resolver.Result{}, ctx.Err()
	}
	if res, ok := r.answers[ip]; ok {
		res.IP = ip
		return res, nil
	}
	return resolver.Result{IP: ip, Status: resolver.NoPTR}, nil
}

func newScanner(st Store, res Lookuper, q Queue, opt Options) *Scanner {
	opt.Worker = "w1"
	if opt.Lease == 0 {
		opt.Lease = time.Minute
	}
	opt.Interval = func() time.Duration { return 24 * time.Hour }
	opt.Idle = 20 * time.Millisecond
	opt.Poll = 5 * time.Millisecond
	opt.RetryAfter = 20 * time.Millisecond
	return New(st, res, q, opt, slog.New(slog.DiscardHandler))
}

// runUntil roda o Scanner até cond ou o prazo.
func runUntil(t *testing.T, s *Scanner, cond func() bool) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Error("prazo do teste esgotado")
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
}

func startLimiter(t *testing.T, rate int) *limiter.Limiter {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	l := limiter.New(rate)
	go l.Run(ctx)
	return l
}

func TestWriteRules(t *testing.T) {
	ip := func(s string) netip.Addr { return netip.MustParseAddr(s) }
	st := newFakeStore("200.160.2.0/24")
	// Estado anterior: .1 e .3 com PTR, .2 com PTR que deixou de existir, .7
	// com falha antiga.
	st.ptr[ip("200.160.2.1")] = &store.Row{Status: resolver.PTR, Names: []string{"ae1-0.ar1.vr.registro.br"}}
	st.ptr[ip("200.160.2.2")] = &store.Row{Status: resolver.PTR, Names: []string{"velho.registro.br"}}
	st.ptr[ip("200.160.2.3")] = &store.Row{Status: resolver.PTR, Names: []string{"registro.br"}}
	st.ptr[ip("200.160.2.7")] = &store.Row{Status: resolver.Timeout}
	res := &fakeResolver{answers: map[netip.Addr]resolver.Result{
		ip("200.160.2.1"): {Status: resolver.PTR, Names: []string{"ae1-0.ar1.vr.registro.br"}}, // igual: não grava
		ip("200.160.2.3"): {Status: resolver.Timeout},                                          // falha: não apaga o PTR
		ip("200.160.2.5"): {Status: resolver.PTR, Names: []string{"mailexploder.registro.br", "mailx.registro.br"}},
		ip("200.160.2.7"): {Status: resolver.ServFail}, // falha sobre falha: muda o estado
		ip("200.160.2.9"): {Status: resolver.Refused},  // falha nova: grava
	}}
	s := newScanner(st, res, startLimiter(t, 100000), Options{})
	runUntil(t, s, func() bool { saved, _ := st.snapshot(); return len(saved) == 1 })

	if row := st.ptr[ip("200.160.2.1")]; row == nil || row.Names[0] != "ae1-0.ar1.vr.registro.br" {
		t.Errorf(".1 = %+v", row)
	}
	if _, ok := st.ptr[ip("200.160.2.2")]; ok {
		t.Error(".2 sem PTR deveria ter a linha apagada")
	}
	if row := st.ptr[ip("200.160.2.3")]; row == nil || row.Status != resolver.PTR {
		t.Errorf(".3: a falha apagou o PTR: %+v", row)
	}
	if row := st.ptr[ip("200.160.2.5")]; row == nil || len(row.Names) != 2 {
		t.Errorf(".5 = %+v", row)
	}
	if row := st.ptr[ip("200.160.2.7")]; row == nil || row.Status != resolver.ServFail {
		t.Errorf(".7 = %+v", row)
	}
	if row := st.ptr[ip("200.160.2.9")]; row == nil || row.Status != resolver.Refused {
		t.Errorf(".9 = %+v", row)
	}
	// Mudaram: .2 (apagada), .5 e .9 (novas), .7 (alterada).
	if st.changed[0] != 4 {
		t.Errorf("changed = %d, quero 4", st.changed[0])
	}
	if len(st.ptr) != 5 {
		t.Errorf("linhas = %d, quero 5 (.1, .3, .5, .7, .9)", len(st.ptr))
	}
	if s.Prefixes() != 1 {
		t.Errorf("Prefixes = %d", s.Prefixes())
	}

	// Uma segunda varredura com as mesmas respostas não muda nada.
	st.queue = append(st.queue, &store.Claim{UUID: "b", Prefix: netip.MustParsePrefix("200.160.2.0/24")})
	s = newScanner(st, res, startLimiter(t, 100000), Options{})
	runUntil(t, s, func() bool { saved, _ := st.snapshot(); return len(saved) == 2 })
	if st.changed[1] != 0 {
		t.Errorf("segunda varredura: changed = %d, quero 0", st.changed[1])
	}
}

func TestSeveralPrefixesInFlight(t *testing.T) {
	st := newFakeStore("10.0.0.0/24", "10.0.1.0/24", "10.0.2.0/24", "10.0.3.0/24", "10.0.4.0/24")
	// Cada consulta demora: com o limitador rápido, os endereços saem da fila
	// logo e o laço pega o próximo prefixo antes do anterior acabar.
	res := &fakeResolver{delay: 30 * time.Millisecond}
	s := newScanner(st, res, startLimiter(t, 100000), Options{MaxActive: 3})
	runUntil(t, s, func() bool { saved, _ := st.snapshot(); return len(saved) == 5 })
	if res.maxPfx < 2 {
		t.Errorf("no máximo %d prefixo em voo; quero vários", res.maxPfx)
	}
	if res.maxPfx > 3 {
		t.Errorf("%d prefixos em voo, acima de MaxActive (3)", res.maxPfx)
	}
	if s.Prefixes() != 5 {
		t.Errorf("Prefixes = %d", s.Prefixes())
	}
}

func TestLowWater(t *testing.T) {
	// Taxa baixa: os 1.024 endereços do /22 ficam esperando vez e o próximo
	// prefixo não é pego enquanto restam 256 ou mais.
	st := newFakeStore("10.0.0.0/22", "10.0.4.0/24")
	lim := startLimiter(t, 20)
	s := newScanner(st, &fakeResolver{}, lim, Options{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	time.Sleep(150 * time.Millisecond)
	st.mu.Lock()
	left := len(st.queue)
	st.mu.Unlock()
	cancel()
	<-done
	if left != 1 {
		t.Errorf("prefixos ainda na fila = %d, quero 1 (o segundo espera o limitador)", left)
	}
}

func TestAbandonOnDeadline(t *testing.T) {
	st := newFakeStore("10.0.0.0/24")
	res := &fakeResolver{delay: time.Second}
	// 200 ms para as consultas (Lease − SaveBudget).
	s := newScanner(st, res, startLimiter(t, 100000), Options{Lease: 300 * time.Millisecond, SaveBudget: 100 * time.Millisecond})
	start := time.Now()
	runUntil(t, s, func() bool { _, released := st.snapshot(); return len(released) == 1 })
	saved, released := st.snapshot()
	if len(saved) != 0 || len(released) != 1 || released[0] != "10.0.0.0/24" {
		t.Errorf("gravados %v, abandonados %v", saved, released)
	}
	if d := time.Since(start); d > 900*time.Millisecond {
		t.Errorf("o abandono levou %v", d)
	}
	if s.Prefixes() != 0 {
		t.Errorf("Prefixes = %d", s.Prefixes())
	}
}

func TestLeaseLostDiscards(t *testing.T) {
	st := newFakeStore("10.0.0.0/24")
	st.leaseErr = true
	res := &fakeResolver{answers: map[netip.Addr]resolver.Result{
		netip.MustParseAddr("10.0.0.1"): {Status: resolver.PTR, Names: []string{"x.example"}},
	}}
	s := newScanner(st, res, startLimiter(t, 100000), Options{})
	runUntil(t, s, func() bool { st.mu.Lock(); defer st.mu.Unlock(); return st.calls == 1 })
	if len(st.ptr) != 0 || s.Prefixes() != 0 {
		t.Errorf("lease perdido gravou algo: %d linhas, %d prefixos", len(st.ptr), s.Prefixes())
	}
	if _, released := st.snapshot(); len(released) != 0 {
		t.Errorf("lease perdido não devolve o prefixo (outra instância o pegou): %v", released)
	}
}

func TestAddresses(t *testing.T) {
	a := addresses(netip.MustParsePrefix("200.160.2.0/24"))
	if len(a) != 256 || a[0].String() != "200.160.2.0" || a[255].String() != "200.160.2.255" {
		t.Errorf("/24 = %d endereços, %v…%v", len(a), a[0], a[len(a)-1])
	}
	if a := addresses(netip.MustParsePrefix("10.0.4.0/22")); len(a) != 1024 || a[1023].String() != "10.0.7.255" {
		t.Errorf("/22 = %d endereços", len(a))
	}
}
