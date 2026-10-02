// Package cache implementa o cache cache-aside da api-rootzone no Valkey
// (protocolo Redis).
//
// O cache é fail-open: qualquer erro ou lentidão do Valkey vira "não achei" e a
// API segue pelo Postgres. Depois de falhas seguidas, um disjuntor deixa de
// consultar o Valkey por alguns segundos, para que uma queda não some a espera
// do timeout a cada requisição.
package cache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// Cache guarda respostas prontas.
type Cache interface {
	Get(ctx context.Context, key string) ([]byte, bool)
	Set(ctx context.Context, key string, value []byte)
	Ping(ctx context.Context) error
	Enabled() bool
}

// Noop é o cache desligado.
type Noop struct{}

func (Noop) Get(context.Context, string) ([]byte, bool) { return nil, false }
func (Noop) Set(context.Context, string, []byte)        {}
func (Noop) Ping(context.Context) error                 { return nil }
func (Noop) Enabled() bool                              { return false }

// backend é o subconjunto do cliente Redis usado aqui (substituível nos testes).
type backend interface {
	Get(ctx context.Context, key string) *redis.StringCmd
	Set(ctx context.Context, key string, value any, ttl time.Duration) *redis.StatusCmd
	Ping(ctx context.Context) *redis.StatusCmd
}

// Valkey é o cache real.
type Valkey struct {
	client  backend
	ttl     time.Duration
	timeout time.Duration
	breaker breaker
}

// NewValkey cria o cliente a partir da REDIS_URL. A conexão é preguiçosa: um
// Valkey fora do ar no boot não impede a API de subir.
func NewValkey(url string, ttl, timeout time.Duration) (*Valkey, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	opts.DialTimeout = max(timeout*4, 200*time.Millisecond)
	opts.ReadTimeout = timeout
	opts.WriteTimeout = timeout
	opts.PoolTimeout = timeout
	opts.MaxRetries = 0
	opts.ClientName = "badblock-api-rootzone"
	return &Valkey{
		client:  redis.NewClient(opts),
		ttl:     ttl,
		timeout: timeout,
		breaker: breaker{threshold: 5, cooldown: 5 * time.Second},
	}, nil
}

// Enabled diz se o cache está ligado.
func (v *Valkey) Enabled() bool { return true }

// Get busca uma chave. Erro ou disjuntor aberto contam como ausência.
func (v *Valkey) Get(ctx context.Context, key string) ([]byte, bool) {
	if !v.breaker.allow() {
		return nil, false
	}
	ctx, cancel := context.WithTimeout(ctx, v.timeout)
	defer cancel()
	b, err := v.client.Get(ctx, key).Bytes()
	switch {
	case errors.Is(err, redis.Nil):
		v.breaker.success()
		return nil, false
	case err != nil:
		v.breaker.failure()
		return nil, false
	}
	v.breaker.success()
	return b, true
}

// Set grava uma chave com o TTL configurado. Falhas são ignoradas.
func (v *Valkey) Set(ctx context.Context, key string, value []byte) {
	if !v.breaker.allow() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, v.timeout)
	defer cancel()
	if err := v.client.Set(ctx, key, value, v.ttl).Err(); err != nil {
		v.breaker.failure()
		return
	}
	v.breaker.success()
}

// Ping confere o Valkey (usado pelo /status; ignora o disjuntor).
func (v *Valkey) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, max(v.timeout*4, 200*time.Millisecond))
	defer cancel()
	return v.client.Ping(ctx).Err()
}

// breaker abre depois de threshold falhas seguidas e fica aberto por cooldown.
type breaker struct {
	threshold int32
	cooldown  time.Duration
	now       func() time.Time

	failures  atomic.Int32
	mu        sync.Mutex
	openUntil time.Time
}

func (b *breaker) clock() time.Time {
	if b.now != nil {
		return b.now()
	}
	return time.Now()
}

func (b *breaker) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.clock().Before(b.openUntil)
}

func (b *breaker) failure() {
	if b.failures.Add(1) >= b.threshold {
		b.mu.Lock()
		b.openUntil = b.clock().Add(b.cooldown)
		b.mu.Unlock()
		b.failures.Store(0)
	}
}

func (b *breaker) success() { b.failures.Store(0) }
