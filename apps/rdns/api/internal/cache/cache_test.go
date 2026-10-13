package cache

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// flaky é um backend que sempre falha e conta as chamadas.
type flaky struct{ calls int }

func (f *flaky) Get(ctx context.Context, _ string) *redis.StringCmd {
	f.calls++
	c := redis.NewStringCmd(ctx)
	c.SetErr(errors.New("down"))
	return c
}

func (f *flaky) Set(ctx context.Context, _ string, _ any, _ time.Duration) *redis.StatusCmd {
	f.calls++
	c := redis.NewStatusCmd(ctx)
	c.SetErr(errors.New("down"))
	return c
}

func (f *flaky) Ping(ctx context.Context) *redis.StatusCmd {
	c := redis.NewStatusCmd(ctx)
	c.SetErr(errors.New("down"))
	return c
}

func TestBreakerOpensAndCloses(t *testing.T) {
	now := time.Unix(1000, 0)
	be := &flaky{}
	v := &Valkey{client: be, ttl: time.Minute, timeout: 10 * time.Millisecond,
		breaker: breaker{threshold: 3, cooldown: 5 * time.Second, now: func() time.Time { return now }}}
	ctx := context.Background()

	for range 3 {
		if _, ok := v.Get(ctx, "k"); ok {
			t.Fatal("falha deveria ser ausência")
		}
	}
	if be.calls != 3 {
		t.Fatalf("calls = %d", be.calls)
	}
	v.Get(ctx, "k")
	v.Set(ctx, "k", []byte("v"))
	if be.calls != 3 {
		t.Errorf("disjuntor aberto não deveria chamar o Valkey (calls = %d)", be.calls)
	}
	now = now.Add(6 * time.Second)
	v.Get(ctx, "k")
	if be.calls != 4 {
		t.Errorf("depois do cooldown deveria tentar de novo (calls = %d)", be.calls)
	}
}

func TestNoop(t *testing.T) {
	var c Cache = Noop{}
	c.Set(context.Background(), "k", []byte("v"))
	if _, ok := c.Get(context.Background(), "k"); ok || c.Enabled() {
		t.Error("Noop não guarda nada")
	}
}
