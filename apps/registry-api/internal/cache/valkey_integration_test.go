//go:build integration

package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/valkey"

	"github.com/patrickbrandao/badblock/apps/registry-api/internal/cache"
)

// Contra um Valkey real (a mesma imagem do compose): grava, lê, expira e, com
// o servidor parado, falha rápido e em silêncio.
func TestValkeyReal(t *testing.T) {
	ctx := context.Background()
	ctr, err := valkey.Run(ctx, "valkey/valkey:9-alpine")
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatal(err)
	}
	url, err := ctr.ConnectionString(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c, err := cache.NewValkey(cache.Options{URL: url, TTL: time.Second, Timeout: 200 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	c.Set(ctx, "bb:test", []byte("valor"))
	if v, ok := c.Get(ctx, "bb:test"); !ok || string(v) != "valor" {
		t.Fatalf("get = %q %v", v, ok)
	}
	time.Sleep(1500 * time.Millisecond)
	if _, ok := c.Get(ctx, "bb:test"); ok {
		t.Error("a chave deveria ter expirado pelo TTL")
	}

	if err := ctr.Stop(ctx, nil); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	for i := 0; i < 20; i++ {
		if _, ok := c.Get(ctx, "bb:test"); ok {
			t.Fatal("com o Valkey parado não pode haver acerto")
		}
	}
	// 5 falhas abrem o disjuntor; as outras 15 nem chegam a tentar.
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("fail-open lento demais: %s", elapsed)
	}
	if err := c.Ping(ctx); err == nil {
		t.Error("ping deveria acusar o Valkey parado")
	}
}
