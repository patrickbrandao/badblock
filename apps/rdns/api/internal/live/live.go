// Package live faz o pedido de tempo real da api-rdns: põe o IP na fila do
// Valkey que o collector-rdns consome e espera a resposta na lista de retorno
// do IP, por até LIVE_TIMEOUT.
//
// Os pedidos do mesmo IP na instância são coalescidos (um singleflight por
// IP; entre instâncias, a marca inflight com SET NX), a fila tem um teto
// (LIVE_QUEUE_MAX) e há no máximo 128 esperas simultâneas. As chaves e o
// formato das mensagens são o contrato com o collector-rdns
// (specs/fontes/rdns/dados.md#fila-de-tempo-real-valkey): mudou aqui, muda lá.
package live

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"
)

const (
	// QueueKey é a fila de pedidos (lista sem TTL).
	QueueKey = "badblock:rdns:live:queue"
	// InflightPrefix + ip marca o pedido que já está na fila.
	InflightPrefix = "badblock:rdns:live:inflight:"
	// ReplyPrefix + ip é a lista de retorno do IP.
	ReplyPrefix = "badblock:rdns:live:reply:"
	// RecentPrefix + ip guarda a última resolução, por LIVE_MIN_AGE.
	RecentPrefix = "badblock:api-rdns:live:recent:"

	inflightTTL = 30 * time.Second
	// MaxWaits é o teto de esperas simultâneas por instância.
	MaxWaits = 128
)

// Estados do bloco live.
const (
	Resolved    = "resolved"
	Recent      = "recent"
	Timeout     = "timeout"
	Unavailable = "unavailable"
	NotAllowed  = "not_allowed"
)

// ErrQueueFull: a fila passou de LIVE_QUEUE_MAX ou a instância já tem
// MaxWaits esperas (503 live_queue_full).
var ErrQueueFull = errors.New("fila de tempo real cheia")

// Request é o pedido na fila.
type Request struct {
	IP string `json:"ip"`
	At string `json:"at"` // RFC 3339 em UTC, com milissegundos
}

// Reply é a resposta do coletor na lista de retorno.
type Reply struct {
	IP        string   `json:"ip"`
	Result    string   `json:"result"`
	Names     []string `json:"names"`
	CheckedAt string   `json:"checked_at"`
	Worker    string   `json:"worker"`
}

// Mark é a marca recent de um IP.
type Mark struct {
	Result    string `json:"result"`
	CheckedAt string `json:"checked_at"`
}

// Backend são as operações no Valkey (Valkey; um falso nos testes).
type Backend interface {
	Recent(ctx context.Context, ip string) (*Mark, error) // nil = sem marca
	Claim(ctx context.Context, ip string) (bool, error)   // SET inflight NX EX 30
	QueueLen(ctx context.Context) (int64, error)
	Release(ctx context.Context, ip string) error // DEL inflight
	Push(ctx context.Context, req Request) error
	Wait(ctx context.Context, ip string, timeout time.Duration) (*Reply, error) // nil, nil = prazo esgotado
	SetRecent(ctx context.Context, ip string, m Mark, ttl time.Duration) error
	Ping(ctx context.Context) error
}

// Outcome é o resultado de um pedido.
type Outcome struct {
	Status    string // Resolved, Recent, Timeout, Unavailable ou NotAllowed
	Result    string // o que a consulta achou ("" = sem consulta)
	CheckedAt string // quando foi resolvido ("" = sem consulta)
	Body      []byte // Resolved: o corpo frio relido depois da gravação
	Err       error  // Unavailable: o erro do Valkey (para o log)
}

// Requester faz os pedidos de uma instância.
type Requester struct {
	b        Backend
	timeout  time.Duration
	minAge   time.Duration
	queueMax int64
	maxWaits int64
	now      func() time.Time

	waits atomic.Int64
	sf    singleflight.Group
}

// NewRequester cria o Requester.
func NewRequester(b Backend, timeout, minAge time.Duration, queueMax int) *Requester {
	return &Requester{b: b, timeout: timeout, minAge: minAge, queueMax: int64(queueMax), maxWaits: MaxWaits, now: time.Now}
}

// Ping confere o Valkey do tempo real.
func (r *Requester) Ping(ctx context.Context) error { return r.b.Ping(ctx) }

// QueueLen é o tamanho da fila (o /meta).
func (r *Requester) QueueLen(ctx context.Context) (int64, error) { return r.b.QueueLen(ctx) }

// unavailableError é uma falha do Valkey: Resolve a devolve como Unavailable.
type unavailableError struct{ err error }

func (e *unavailableError) Error() string { return e.err.Error() }
func (e *unavailableError) Unwrap() error { return e.err }

func unavailable(err error) error { return &unavailableError{err: err} }

// Resolve pede a resolução de ip e espera. reread relê o IP no banco e
// regrava a chave fria; o líder o chama uma vez, com a resposta, e o corpo
// que ele devolve vai em Outcome.Body. Erros do Valkey viram Unavailable (com
// o erro em Outcome.Err); o erro devolvido é só o de reread (o banco) ou
// ErrQueueFull.
func (r *Requester) Resolve(ctx context.Context, ip string, reread func(ctx context.Context) ([]byte, error)) (Outcome, error) {
	o, err := r.resolve(ctx, ip, reread)
	var u *unavailableError
	if errors.As(err, &u) {
		return Outcome{Status: Unavailable, Err: u.err}, nil
	}
	return o, err
}

func (r *Requester) resolve(ctx context.Context, ip string, reread func(ctx context.Context) ([]byte, error)) (Outcome, error) {
	m, err := r.b.Recent(ctx, ip)
	if err != nil {
		return Outcome{}, unavailable(err)
	}
	if m != nil {
		return Outcome{Status: Recent, Result: m.Result, CheckedAt: m.CheckedAt}, nil
	}
	if r.waits.Add(1) > r.maxWaits {
		r.waits.Add(-1)
		return Outcome{}, ErrQueueFull
	}
	defer r.waits.Add(-1)

	v, err, _ := r.sf.Do(ip, func() (any, error) {
		// O líder segue mesmo se o cliente dele desistir: os outros esperam.
		ctx := context.WithoutCancel(ctx)
		claimed, err := r.b.Claim(ctx, ip)
		if err != nil {
			return nil, unavailable(err)
		}
		if claimed {
			n, err := r.b.QueueLen(ctx)
			if err != nil {
				_ = r.b.Release(ctx, ip)
				return nil, unavailable(err)
			}
			if n >= r.queueMax {
				_ = r.b.Release(ctx, ip)
				return nil, ErrQueueFull
			}
			req := Request{IP: ip, At: r.now().UTC().Format("2006-01-02T15:04:05.000Z07:00")}
			if err := r.b.Push(ctx, req); err != nil {
				_ = r.b.Release(ctx, ip)
				return nil, unavailable(err)
			}
		}
		// Não conseguiu a marca: o pedido já está na fila; só espera.
		reply, err := r.b.Wait(ctx, ip, r.timeout)
		if err != nil {
			return nil, unavailable(err)
		}
		if reply == nil {
			return Outcome{Status: Timeout}, nil
		}
		if reply.Result == NotAllowed {
			return Outcome{Status: NotAllowed}, nil
		}
		body, err := reread(ctx)
		if err != nil {
			return nil, err
		}
		_ = r.b.SetRecent(ctx, ip, Mark{Result: reply.Result, CheckedAt: reply.CheckedAt}, r.minAge)
		return Outcome{Status: Resolved, Result: reply.Result, CheckedAt: reply.CheckedAt, Body: body}, nil
	})
	if err != nil {
		return Outcome{}, err
	}
	return v.(Outcome), nil
}

// Valkey é o Backend real.
type Valkey struct {
	rdb *redis.Client
}

// NewValkey cria o cliente do tempo real a partir da REDIS_URL: separado do
// cache (fora do disjuntor dele), pool de 128, sem novas tentativas e 1 s de
// prazo fora do BLPOP (que estende o próprio prazo).
func NewValkey(url string) (*Valkey, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	opts.ClientName = "badblock-api-rdns-live"
	opts.MaxRetries = 0
	opts.PoolSize = MaxWaits
	opts.DialTimeout = time.Second
	opts.ReadTimeout = time.Second
	opts.WriteTimeout = time.Second
	opts.PoolTimeout = time.Second
	return &Valkey{rdb: redis.NewClient(opts)}, nil
}

// Close fecha o cliente.
func (v *Valkey) Close() error { return v.rdb.Close() }

func (v *Valkey) Recent(ctx context.Context, ip string) (*Mark, error) {
	b, err := v.rdb.Get(ctx, RecentPrefix+ip).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return parseMark(b), nil
}

// parseMark lê a marca recent; ilegível vale como se não houvesse.
func parseMark(b []byte) *Mark {
	var m Mark
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	return &m
}

func (v *Valkey) Claim(ctx context.Context, ip string) (bool, error) {
	return v.rdb.SetNX(ctx, InflightPrefix+ip, "1", inflightTTL).Result()
}

func (v *Valkey) QueueLen(ctx context.Context) (int64, error) {
	return v.rdb.LLen(ctx, QueueKey).Result()
}

func (v *Valkey) Release(ctx context.Context, ip string) error {
	return v.rdb.Del(ctx, InflightPrefix+ip).Err()
}

func (v *Valkey) Push(ctx context.Context, req Request) error {
	b, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return v.rdb.RPush(ctx, QueueKey, b).Err()
}

func (v *Valkey) Wait(ctx context.Context, ip string, timeout time.Duration) (*Reply, error) {
	vals, err := v.rdb.BLPop(ctx, timeout, ReplyPrefix+ip).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var r Reply
	if err := json.Unmarshal([]byte(vals[1]), &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func (v *Valkey) SetRecent(ctx context.Context, ip string, m Mark, ttl time.Duration) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return v.rdb.Set(ctx, RecentPrefix+ip, b, ttl).Err()
}

func (v *Valkey) Ping(ctx context.Context) error {
	return v.rdb.Ping(ctx).Err()
}
