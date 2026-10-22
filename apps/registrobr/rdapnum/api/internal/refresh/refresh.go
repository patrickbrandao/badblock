// Package refresh faz o pedido de atualização da api-registrobr-rdapnum
// (POST .../refresh): põe o alvo na fila do Valkey que o
// collector-registrobr-rdapnum consome. Ninguém espera a coleta: o coletor só
// marca rescan e coleta na taxa normal (decisão #42).
//
// Um pedido do mesmo alvo dentro de 60 s não entra de novo (a marca inflight,
// com SET NX), uma coleta mais nova que REFRESH_MIN_AGE não é pedida e a fila
// tem um teto (REFRESH_QUEUE_MAX). As chaves e o formato do pedido são o
// contrato com o coletor
// (specs/fontes/registrobr/rdapnum/dados.md#fila-de-atualização-valkey):
// mudou aqui, muda lá.
package refresh

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// QueueKey é a fila de pedidos (lista sem TTL; o coletor faz BLPOP).
	QueueKey = "badblock:registrobr-rdapnum:refresh:queue"
	// InflightPrefix + "<kind>:<alvo>" marca o pedido que já está na fila.
	InflightPrefix = "badblock:registrobr-rdapnum:refresh:inflight:"

	// InflightTTL é a validade da marca inflight.
	InflightTTL = 60 * time.Second
)

// Motivos de um pedido que não entrou na fila.
const (
	Recent        = "recent"
	AlreadyQueued = "already_queued"
)

// ErrQueueFull: a fila passou de REFRESH_QUEUE_MAX (503 refresh_unavailable).
var ErrQueueFull = errors.New("fila de atualização cheia")

// ErrUnavailable: o Valkey falhou em algum passo (503 refresh_unavailable).
var ErrUnavailable = errors.New("Valkey indisponível")

// Request é o pedido na fila.
type Request struct {
	Kind   string `json:"kind"`   // asn ou prefix
	Target string `json:"target"` // "61610" ou o bloco do alvo em forma canônica
	At     string `json:"at"`     // RFC 3339 em UTC, com milissegundos
}

// Backend são as operações no Valkey (Valkey; um falso nos testes).
type Backend interface {
	Ping(ctx context.Context) error
	Claim(ctx context.Context, mark string) (bool, error) // SET inflight NX EX 60
	Release(ctx context.Context, mark string) error       // DEL inflight
	QueueLen(ctx context.Context) (int64, error)
	Push(ctx context.Context, req Request) error
}

// Outcome é o resultado de um pedido aceito (202).
type Outcome struct {
	Queued bool
	Reason string // "" (entrou na fila), Recent ou AlreadyQueued
}

// Requester faz os pedidos.
type Requester struct {
	b        Backend
	minAge   time.Duration
	queueMax int64
	now      func() time.Time
}

// NewRequester cria o Requester.
func NewRequester(b Backend, minAge time.Duration, queueMax int) *Requester {
	return &Requester{b: b, minAge: minAge, queueMax: int64(queueMax), now: time.Now}
}

// Ping confere o Valkey da atualização.
func (r *Requester) Ping(ctx context.Context) error { return r.b.Ping(ctx) }

// QueueLen é o tamanho da fila (o /meta).
func (r *Requester) QueueLen(ctx context.Context) (int64, error) { return r.b.QueueLen(ctx) }

// unavailable embrulha um erro do Valkey em ErrUnavailable, com a causa.
func unavailable(err error) error { return errors.Join(ErrUnavailable, err) }

// Request pede a atualização de um alvo ativo. lastScanAt é a última coleta
// conclusiva dele (nil = nunca). Devolve ErrUnavailable (com a causa) se o
// Valkey falhou e ErrQueueFull se a fila está cheia; nos dois casos a marca
// inflight que este pedido gravou é apagada.
func (r *Requester) Request(ctx context.Context, kind, target string, lastScanAt *time.Time) (Outcome, error) {
	if err := r.b.Ping(ctx); err != nil {
		return Outcome{}, unavailable(err)
	}
	if lastScanAt != nil && r.now().Sub(*lastScanAt) < r.minAge {
		return Outcome{Reason: Recent}, nil
	}
	mark := kind + ":" + target
	claimed, err := r.b.Claim(ctx, mark)
	if err != nil {
		return Outcome{}, unavailable(err)
	}
	if !claimed {
		return Outcome{Reason: AlreadyQueued}, nil
	}
	// Daqui em diante a marca é deste pedido: falhou, apaga.
	ctx = context.WithoutCancel(ctx)
	n, err := r.b.QueueLen(ctx)
	if err != nil {
		_ = r.b.Release(ctx, mark)
		return Outcome{}, unavailable(err)
	}
	if n >= r.queueMax {
		_ = r.b.Release(ctx, mark)
		return Outcome{}, ErrQueueFull
	}
	req := Request{Kind: kind, Target: target, At: r.now().UTC().Format("2006-01-02T15:04:05.000Z07:00")}
	if err := r.b.Push(ctx, req); err != nil {
		_ = r.b.Release(ctx, mark)
		return Outcome{}, unavailable(err)
	}
	return Outcome{Queued: true}, nil
}

// Valkey é o Backend real.
type Valkey struct {
	rdb *redis.Client
}

// NewValkey cria o cliente da atualização a partir da REDIS_URL: separado do
// cache (fora do disjuntor dele), sem novas tentativas e com 1 s de prazo.
func NewValkey(url string) (*Valkey, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	opts.ClientName = "badblock-api-registrobr-rdapnum-refresh"
	opts.MaxRetries = 0
	opts.PoolSize = 16
	opts.DialTimeout = time.Second
	opts.ReadTimeout = time.Second
	opts.WriteTimeout = time.Second
	opts.PoolTimeout = time.Second
	return &Valkey{rdb: redis.NewClient(opts)}, nil
}

// Close fecha o cliente.
func (v *Valkey) Close() error { return v.rdb.Close() }

func (v *Valkey) Ping(ctx context.Context) error { return v.rdb.Ping(ctx).Err() }

func (v *Valkey) Claim(ctx context.Context, mark string) (bool, error) {
	return v.rdb.SetNX(ctx, InflightPrefix+mark, "1", InflightTTL).Result()
}

func (v *Valkey) Release(ctx context.Context, mark string) error {
	return v.rdb.Del(ctx, InflightPrefix+mark).Err()
}

func (v *Valkey) QueueLen(ctx context.Context) (int64, error) {
	return v.rdb.LLen(ctx, QueueKey).Result()
}

func (v *Valkey) Push(ctx context.Context, req Request) error {
	b, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return v.rdb.RPush(ctx, QueueKey, b).Err()
}
