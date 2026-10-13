// Package live atende os pedidos de tempo real que a api-rdns põe na fila do
// Valkey: tira um pedido (BLPOP), confere se o IP está num prefixo ativo na
// regra, pergunta o PTR com prioridade no limitador, grava em rdns_ptr com as
// regras da varredura e só então devolve a resposta na lista de retorno do IP.
//
// As chaves e o formato das mensagens são o contrato com a api-rdns
// (specs/fontes/rdns/dados.md#fila-de-tempo-real-valkey): mudou aqui, muda lá.
package live

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/patrickbrandao/badblock/apps/rdns/collector/internal/limiter"
	"github.com/patrickbrandao/badblock/apps/rdns/collector/internal/resolver"
)

const (
	// QueueKey é a fila de pedidos (lista sem TTL): a API faz RPUSH, o
	// coletor BLPOP.
	QueueKey = "badblock:rdns:live:queue"
	// InflightPrefix + ip marca o pedido na fila (a API grava, o coletor apaga).
	InflightPrefix = "badblock:rdns:live:inflight:"
	// ReplyPrefix + ip é a lista de retorno do IP.
	ReplyPrefix = "badblock:rdns:live:reply:"

	// MaxAge: pedido mais velho que isso ninguém mais espera.
	MaxAge = 30 * time.Second
	// ReplyTTL é a expiração da lista de retorno.
	ReplyTTL = 30 * time.Second

	block      = 5 * time.Second // bloqueio do BLPOP (para notar o encerramento)
	retryEvery = 5 * time.Second // Valkey fora
	handlers   = 32              // pedidos atendidos ao mesmo tempo por instância
)

// Request é o pedido na fila.
type Request struct {
	IP string    `json:"ip"`
	At time.Time `json:"at"`
}

// Reply é a resposta na lista de retorno.
type Reply struct {
	IP        string   `json:"ip"`
	Result    string   `json:"result"` // ptr, no_ptr, servfail, refused, timeout ou not_allowed
	Names     []string `json:"names"`
	CheckedAt string   `json:"checked_at"`
	Worker    string   `json:"worker"`
}

// Store é o que o tempo real usa (store.Store).
type Store interface {
	LiveAllowed(ctx context.Context, ip netip.Addr) (bool, error)
	SaveLive(ctx context.Context, res resolver.Result) (bool, error)
}

// Lookuper pergunta o PTR (resolver.Resolver).
type Lookuper interface {
	Lookup(ctx context.Context, ip netip.Addr, prio limiter.Priority, first *limiter.Ticket) (resolver.Result, error)
}

// NewClient cria o cliente do Valkey a partir da REDIS_URL: pool de 4, sem
// novas tentativas, 1 s de prazo fora do BLPOP (que estende o próprio prazo).
func NewClient(url string) (*redis.Client, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	opts.ClientName = "badblock-collector-rdns"
	opts.MaxRetries = 0
	opts.PoolSize = 4
	opts.DialTimeout = time.Second
	opts.ReadTimeout = time.Second
	opts.WriteTimeout = time.Second
	opts.PoolTimeout = time.Second
	return redis.NewClient(opts), nil
}

// Loop é o laço do tempo real de uma instância.
type Loop struct {
	rdb    *redis.Client
	store  Store
	res    Lookuper
	worker string
	log    *slog.Logger
	now    func() time.Time

	served atomic.Int64
}

// New cria o laço.
func New(rdb *redis.Client, st Store, res Lookuper, worker string, log *slog.Logger) *Loop {
	return &Loop{rdb: rdb, store: st, res: res, worker: worker, log: log, now: time.Now}
}

// Served é quantos pedidos esta instância atendeu desde a subida.
func (l *Loop) Served() int64 { return l.served.Load() }

// Run atende os pedidos até o contexto acabar.
func (l *Loop) Run(ctx context.Context) {
	sem := make(chan struct{}, handlers)
	var wg sync.WaitGroup
	defer wg.Wait()
	down := false
	for ctx.Err() == nil {
		vals, err := l.rdb.BLPop(ctx, block, QueueKey).Result()
		if errors.Is(err, redis.Nil) {
			down = false
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if !down {
				l.log.Warn("Valkey indisponível; tempo real parado", "err", err)
				down = true
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(retryEvery):
			}
			continue
		}
		down = false
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return
		}
		wg.Go(func() {
			defer func() { <-sem }()
			l.Handle(ctx, vals[1])
		})
	}
}

// Handle atende um pedido (a mensagem como veio da fila).
func (l *Loop) Handle(ctx context.Context, payload string) {
	start := l.now()
	var req Request
	if err := json.Unmarshal([]byte(payload), &req); err != nil || req.At.IsZero() {
		l.log.Warn("pedido de tempo real inválido", "payload", clip(payload))
		return
	}
	ip, err := netip.ParseAddr(req.IP)
	if err != nil || ip.Zone() != "" || !ip.Unmap().Is4() {
		l.log.Warn("pedido de tempo real inválido", "payload", clip(payload))
		return
	}
	ip = ip.Unmap()
	if start.Sub(req.At) > MaxAge {
		// Ninguém mais espera por ele.
		l.log.Debug("pedido de tempo real vencido descartado", "ip", ip.String(), "at", req.At)
		l.release(ctx, ip)
		return
	}

	reply := Reply{IP: ip.String(), Names: []string{}, Worker: l.worker}
	allowed, err := l.store.LiveAllowed(ctx, ip)
	if err != nil {
		l.fail(ctx, ip, err)
		return
	}
	if !allowed {
		reply.Result = "not_allowed"
	} else {
		lctx, cancel := context.WithTimeout(ctx, MaxAge)
		res, err := l.res.Lookup(lctx, ip, limiter.Live, nil)
		cancel()
		if err != nil {
			if ctx.Err() == nil {
				l.fail(ctx, ip, err)
			}
			return
		}
		if _, err := l.store.SaveLive(ctx, res); err != nil {
			l.fail(ctx, ip, err)
			return
		}
		reply.Result = res.Status.String()
		if res.Status == resolver.PTR {
			reply.Names = res.Names
		}
	}
	reply.CheckedAt = l.now().UTC().Format(time.RFC3339)
	body, _ := json.Marshal(reply)
	key := ReplyPrefix + ip.String()
	_, err = l.rdb.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.RPush(ctx, key, body)
		p.Expire(ctx, key, ReplyTTL)
		p.Del(ctx, InflightPrefix+ip.String())
		return nil
	})
	if err != nil {
		l.log.Warn("não consegui responder o tempo real", "ip", ip.String(), "err", err)
		return
	}
	l.served.Add(1)
	l.log.Debug("tempo real", "ip", ip.String(), "result", reply.Result, "ms", l.now().Sub(start).Milliseconds())
}

// fail registra a falha e apaga a marca inflight, para o próximo pedido do IP
// poder entrar na fila.
func (l *Loop) fail(ctx context.Context, ip netip.Addr, err error) {
	l.log.Warn("não consegui atender o tempo real", "ip", ip.String(), "err", err)
	l.release(ctx, ip)
}

func (l *Loop) release(ctx context.Context, ip netip.Addr) {
	_ = l.rdb.Del(ctx, InflightPrefix+ip.String()).Err()
}

func clip(s string) string {
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}
