package cache

import (
	"context"
	"errors"
	"net/netip"
	"net/url"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestIPKeyBuckets(t *testing.T) {
	exc := map[netip.Prefix]struct{}{
		netip.MustParsePrefix("192.0.0.0/24"): {},
		netip.MustParsePrefix("2001:1::/48"):  {},
	}
	a := IPKey(7, netip.MustParseAddr("45.171.60.1"), exc)
	b := IPKey(7, netip.MustParseAddr("45.171.60.254"), exc)
	if a != b || a != "bb:v1:7:ipb:45.171.60.0/24" {
		t.Errorf("IPs do mesmo /24 deveriam compartilhar a chave: %s %s", a, b)
	}
	if c := IPKey(7, netip.MustParseAddr("45.171.61.1"), exc); c == a {
		t.Error("outro /24 deveria ter outra chave")
	}
	if got := IPKey(7, netip.MustParseAddr("192.0.0.9"), exc); got != "bb:v1:7:ip:192.0.0.9" {
		t.Errorf("bloco fragmentado deveria usar chave por IP: %s", got)
	}
	if got := IPKey(7, netip.MustParseAddr("2804:5964:0:1::1"), exc); got != "bb:v1:7:ipb:2804:5964::/48" {
		t.Errorf("IPv6 deveria usar o /48: %s", got)
	}
	if got := IPKey(7, netip.MustParseAddr("2001:1::1"), exc); got != "bb:v1:7:ip:2001:1::1" {
		t.Errorf("/48 fragmentado deveria usar chave por IP: %s", got)
	}
	if IPKey(8, netip.MustParseAddr("45.171.60.1"), exc) == a {
		t.Error("versão nova do dataset deveria mudar a chave")
	}
}

func TestRequestKeyCanonical(t *testing.T) {
	q1, _ := url.ParseQuery("status=allocated&limit=10&format=txt")
	q2, _ := url.ParseQuery("format=txt&limit=10&status=allocated")
	if RequestKey(1, "/v1/country/BR/prefixes", q1) != RequestKey(1, "/v1/country/BR/prefixes", q2) {
		t.Error("a ordem dos parâmetros não deveria mudar a chave")
	}
	if RequestKey(1, "/v1/asn/1", nil) != "bb:v1:1:req:/v1/asn/1" {
		t.Error("chave sem query")
	}
}

// fakeBackend simula o Valkey.
type fakeBackend struct {
	data  map[string]string
	down  bool
	calls int
}

func (f *fakeBackend) Get(ctx context.Context, key string) *redis.StringCmd {
	f.calls++
	cmd := redis.NewStringCmd(ctx)
	switch {
	case f.down:
		cmd.SetErr(errors.New("connection refused"))
	case f.data[key] == "":
		cmd.SetErr(redis.Nil)
	default:
		cmd.SetVal(f.data[key])
	}
	return cmd
}

func (f *fakeBackend) Set(ctx context.Context, key string, value any, _ time.Duration) *redis.StatusCmd {
	f.calls++
	cmd := redis.NewStatusCmd(ctx)
	if f.down {
		cmd.SetErr(errors.New("connection refused"))
		return cmd
	}
	f.data[key] = string(value.([]byte))
	return cmd
}

func (f *fakeBackend) Ping(ctx context.Context) *redis.StatusCmd {
	cmd := redis.NewStatusCmd(ctx)
	if f.down {
		cmd.SetErr(errors.New("connection refused"))
	}
	return cmd
}

func TestValkeyFailOpenAndBreaker(t *testing.T) {
	fb := &fakeBackend{data: map[string]string{}}
	now := time.Now()
	v := &Valkey{client: fb, ttl: time.Hour, timeout: time.Second,
		breaker: breaker{threshold: 3, cooldown: 5 * time.Second, now: func() time.Time { return now }}}
	ctx := context.Background()

	v.Set(ctx, "k", []byte("v"))
	if got, ok := v.Get(ctx, "k"); !ok || string(got) != "v" {
		t.Fatalf("get = %q %v", got, ok)
	}
	if _, ok := v.Get(ctx, "ausente"); ok {
		t.Error("chave ausente não deveria ser encontrada")
	}

	// Valkey cai: três falhas abrem o disjuntor e as chamadas param.
	fb.down = true
	for i := 0; i < 3; i++ {
		if _, ok := v.Get(ctx, "k"); ok {
			t.Fatal("com o Valkey fora, o get deveria falhar em silêncio")
		}
	}
	before := fb.calls
	v.Get(ctx, "k")
	v.Set(ctx, "k", []byte("x"))
	if fb.calls != before {
		t.Error("com o disjuntor aberto o Valkey não deveria ser chamado")
	}
	if err := v.Ping(ctx); err == nil {
		t.Error("ping deveria reportar a queda mesmo com o disjuntor aberto")
	}

	// Passado o cooldown, uma nova tentativa acontece e o sucesso fecha o disjuntor.
	now = now.Add(6 * time.Second)
	fb.down = false
	if got, ok := v.Get(ctx, "k"); !ok || string(got) != "v" {
		t.Errorf("depois do cooldown o cache deveria voltar: %q %v", got, ok)
	}
}

func TestNoop(t *testing.T) {
	var c Cache = Noop{}
	c.Set(context.Background(), "k", []byte("v"))
	if _, ok := c.Get(context.Background(), "k"); ok || c.Enabled() {
		t.Error("noop não guarda nada")
	}
}
