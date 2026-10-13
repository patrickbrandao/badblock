package httpapi

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/patrickbrandao/badblock/apps/rdns/api/internal/live"
)

// fakeBackend é o Valkey do tempo real em memória. reply, quando não nil, é
// a resposta do coletor; antes de devolvê-la, onReply roda (o coletor grava
// em rdns_ptr antes de responder).
type fakeBackend struct {
	mu       sync.Mutex
	recent   map[string]live.Mark
	inflight map[string]bool
	queue    []live.Request
	queueLen int64
	reply    *live.Reply
	onReply  func()
	err      error
	pushes   int
}

func newBackend() *fakeBackend {
	return &fakeBackend{recent: map[string]live.Mark{}, inflight: map[string]bool{}}
}

func (b *fakeBackend) Recent(_ context.Context, ip string) (*live.Mark, error) {
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
	return b.queueLen + int64(len(b.queue)), nil
}

func (b *fakeBackend) Release(_ context.Context, ip string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.inflight, ip)
	return nil
}

func (b *fakeBackend) Push(_ context.Context, req live.Request) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.queue = append(b.queue, req)
	b.pushes++
	return nil
}

func (b *fakeBackend) Wait(_ context.Context, ip string, _ time.Duration) (*live.Reply, error) {
	b.mu.Lock()
	r, hook := b.reply, b.onReply
	b.mu.Unlock()
	if r == nil {
		return nil, nil
	}
	if hook != nil {
		hook()
	}
	b.mu.Lock()
	delete(b.inflight, ip)
	b.mu.Unlock()
	return r, nil
}

func (b *fakeBackend) SetRecent(_ context.Context, ip string, m live.Mark, _ time.Duration) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.recent[ip] = m
	return nil
}

func (b *fakeBackend) Ping(context.Context) error { return b.err }

func liveAPI(t *testing.T, st *fakeStore, b *fakeBackend) (*API, *memCache) {
	t.Helper()
	c := &memCache{m: map[string][]byte{}}
	var lv Live
	if b != nil {
		lv = live.NewRequester(b, time.Second, time.Minute, 1000)
	}
	return newTestAPI(t, st, true, c, lv), c
}

func TestLiveResolved(t *testing.T) {
	st := newFakeStore()
	b := newBackend()
	a, c := liveAPI(t, st, b)
	h := a.Handler()

	// O dado frio já está no cache.
	if rec := do(h, "GET", "/rdns/ip/200.160.2.3"); rec.Code != 200 {
		t.Fatal(rec.Body)
	}
	b.reply = &live.Reply{IP: "200.160.2.3", Result: "ptr", Names: []string{"novo.registro.br"},
		CheckedAt: "2026-10-11T03:00:00Z", Worker: "w"}
	b.onReply = func() { st.setPTR(ptrRow("200.160.2.3", 1, "novo.registro.br")) }

	rec := do(h, "GET", "/rdns/ip/200.160.2.3?live=true", "If-None-Match", "*")
	if rec.Code != 200 {
		t.Fatalf("live = %d %s", rec.Code, rec.Body)
	}
	got := decode[IPResponse](t, rec)
	if got.Live == nil || got.Live.Status != "resolved" || *got.Live.Result != "ptr" || *got.Live.CheckedAt != "2026-10-11T03:00:00Z" {
		t.Errorf("live = %+v", got.Live)
	}
	if got.Name == nil || *got.Name != "novo.registro.br" {
		t.Errorf("o corpo deveria ser o relido do banco: %+v", got)
	}
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("X-Cache") != "LIVE" || rec.Header().Get("ETag") != "" {
		t.Errorf("cabeçalhos = %v", rec.Header())
	}
	if b.pushes != 1 || len(b.queue) != 1 || b.queue[0].IP != "200.160.2.3" || !strings.HasSuffix(b.queue[0].At, "Z") {
		t.Errorf("fila = %+v", b.queue)
	}
	// A chave fria foi regravada com o corpo novo.
	if cold := c.get("badblock:api-rdns:ip:200.160.2.3"); !strings.Contains(string(cold), "novo.registro.br") ||
		strings.Contains(string(cold), `"live"`) {
		t.Errorf("chave fria = %s", cold)
	}
	if rec := do(h, "GET", "/rdns/ip/200.160.2.3"); !strings.Contains(rec.Body.String(), "novo.registro.br") {
		t.Errorf("leitura fria depois do live = %s", rec.Body)
	}

	// De novo, antes de LIVE_MIN_AGE: recent, sem pedido novo.
	got = decode[IPResponse](t, do(h, "GET", "/rdns/ip/200.160.2.3?live=true"))
	if got.Live.Status != "recent" || *got.Live.Result != "ptr" || b.pushes != 1 {
		t.Errorf("recent = %+v, pushes %d", got.Live, b.pushes)
	}
}

func TestLiveFailureKeepsPTR(t *testing.T) {
	// A consulta deu timeout, mas o PTR antigo continua: status ptr com
	// live.result timeout.
	st := newFakeStore()
	b := newBackend()
	b.reply = &live.Reply{IP: "200.160.2.3", Result: "timeout", Names: []string{}, CheckedAt: "2026-10-11T03:00:00Z"}
	a, _ := liveAPI(t, st, b)
	got := decode[IPResponse](t, do(a.Handler(), "GET", "/rdns/ip/200.160.2.3?live=true"))
	if got.Status != "ptr" || got.Live.Status != "resolved" || *got.Live.Result != "timeout" {
		t.Errorf("falha sobre PTR = %+v %+v", got, got.Live)
	}
}

func TestLiveFallbacks(t *testing.T) {
	st := newFakeStore()

	// Sem resposta no prazo: timeout, com o dado frio.
	b := newBackend()
	a, _ := liveAPI(t, st, b)
	got := decode[IPResponse](t, do(a.Handler(), "GET", "/rdns/ip/200.160.2.3?live=true"))
	if got.Live.Status != "timeout" || got.Live.Result != nil || got.Live.CheckedAt != nil || *got.Name != "registro.br" {
		t.Errorf("timeout = %+v", got)
	}
	if !strings.Contains(do(a.Handler(), "GET", "/rdns/ip/200.160.2.3?live=true").Body.String(), `"live":{"status":"timeout","result":null,"checked_at":null}`) {
		t.Error("timeout: result e checked_at deveriam ser null")
	}

	// Sem REDIS_URL: unavailable.
	a, _ = liveAPI(t, st, nil)
	rec := do(a.Handler(), "GET", "/rdns/ip/200.160.2.3?live=true")
	if got := decode[IPResponse](t, rec); got.Live.Status != "unavailable" || rec.Header().Get("X-Cache") != "LIVE" {
		t.Errorf("sem Valkey = %+v", got.Live)
	}

	// Valkey fora: unavailable.
	b = newBackend()
	b.err = errors.New("down")
	a, _ = liveAPI(t, st, b)
	if got := decode[IPResponse](t, do(a.Handler(), "GET", "/rdns/ip/200.160.2.3?live=true")); got.Live.Status != "unavailable" {
		t.Errorf("Valkey fora = %+v", got.Live)
	}

	// Prefixo inativo ou fora da regra: not_allowed, nada é pedido.
	b = newBackend()
	a, _ = liveAPI(t, st, b)
	for _, ip := range []string{"10.0.0.5", "10.1.0.5"} {
		got := decode[IPResponse](t, do(a.Handler(), "GET", "/rdns/ip/"+ip+"?live=true"))
		if got.Live.Status != "not_allowed" || got.IP != ip {
			t.Errorf("%s = %+v", ip, got.Live)
		}
	}
	if b.pushes != 0 {
		t.Errorf("not_allowed pediu %d vezes", b.pushes)
	}

	// O coletor respondeu not_allowed (o prefixo mudou no meio).
	b = newBackend()
	b.reply = &live.Reply{IP: "200.160.2.3", Result: "not_allowed", Names: []string{}}
	a, _ = liveAPI(t, st, b)
	if got := decode[IPResponse](t, do(a.Handler(), "GET", "/rdns/ip/200.160.2.3?live=true")); got.Live.Status != "not_allowed" {
		t.Errorf("coletor not_allowed = %+v", got.Live)
	}

	// 404 vale igual com live=true.
	if rec := do(a.Handler(), "GET", "/rdns/ip/8.8.8.8?live=true"); rec.Code != 404 {
		t.Errorf("fora de prefixo = %d", rec.Code)
	}
}

func TestLiveQueueFull(t *testing.T) {
	b := newBackend()
	b.queueLen = 1000
	a, _ := liveAPI(t, newFakeStore(), b)
	rec := do(a.Handler(), "GET", "/rdns/ip/200.160.2.3?live=true")
	if rec.Code != 503 {
		t.Fatalf("fila cheia = %d %s", rec.Code, rec.Body)
	}
	e := decode[errorBody](t, rec)
	if e.Error.Code != "live_queue_full" || e.Error.Message != "fila de tempo real cheia; tente sem live=true ou mais tarde" {
		t.Errorf("erro = %+v", e)
	}
	if b.inflight["200.160.2.3"] || b.pushes != 0 {
		t.Error("fila cheia: a marca deveria ser apagada e nada enfileirado")
	}
}

func TestLiveHead(t *testing.T) {
	b := newBackend()
	b.reply = &live.Reply{IP: "200.160.2.3", Result: "ptr", Names: []string{"registro.br"}, CheckedAt: "2026-10-11T03:00:00Z"}
	a, _ := liveAPI(t, newFakeStore(), b)
	rec := do(a.Handler(), "HEAD", "/rdns/ip/200.160.2.3?live=true")
	if rec.Code != 200 || b.pushes != 1 || rec.Header().Get("X-Cache") != "LIVE" {
		t.Errorf("HEAD live = %d, pushes %d", rec.Code, b.pushes)
	}
}
