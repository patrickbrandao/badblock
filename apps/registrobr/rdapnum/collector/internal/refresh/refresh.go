// Package refresh atende os pedidos de atualização que a
// api-registrobr-rdapnum põe na fila do Valkey: tira um pedido (BLPOP),
// confere o alvo e marca rescan = true nele, para ele passar na frente da
// fila (respeitando retry_at), na taxa normal. Ninguém espera a coleta
// (collector.md#atualização-pedida-pela-api).
//
// A chave e o formato da mensagem são o contrato com a API
// (specs/fontes/registrobr/rdapnum/dados.md#fila-de-atualização-valkey):
// mudou aqui, muda lá.
package refresh

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/netip"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/rdap"
)

const (
	// QueueKey é a fila de pedidos (lista sem TTL): a API faz RPUSH, o
	// coletor BLPOP.
	QueueKey = "badblock:registrobr-rdapnum:refresh:queue"

	block      = 5 * time.Second // bloqueio do BLPOP (para notar o encerramento)
	retryEvery = 5 * time.Second // Valkey fora
)

// Request é o pedido na fila.
type Request struct {
	Kind   string    `json:"kind"`
	Target string    `json:"target"`
	At     time.Time `json:"at"`
}

// Store marca o alvo (store.Store).
type Store interface {
	MarkRescan(ctx context.Context, kind, target string) (bool, error)
}

// NewClient cria o cliente do Valkey a partir da REDIS_URL: pool de 2, sem
// novas tentativas, 1 s de prazo fora do BLPOP (que estende o próprio prazo).
func NewClient(url string) (*redis.Client, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	opts.ClientName = "badblock-collector-registrobr-rdapnum"
	opts.MaxRetries = 0
	opts.PoolSize = 2
	opts.DialTimeout = time.Second
	opts.ReadTimeout = time.Second
	opts.WriteTimeout = time.Second
	opts.PoolTimeout = time.Second
	return redis.NewClient(opts), nil
}

// Loop é o laço de atualização de uma instância.
type Loop struct {
	rdb   *redis.Client
	store Store
	log   *slog.Logger

	served atomic.Int64
}

// New cria o laço.
func New(rdb *redis.Client, st Store, log *slog.Logger) *Loop {
	return &Loop{rdb: rdb, store: st, log: log}
}

// Served é quantos pedidos esta instância atendeu (alvo marcado) desde a
// subida.
func (l *Loop) Served() int64 { return l.served.Load() }

// Run atende os pedidos até o contexto acabar.
func (l *Loop) Run(ctx context.Context) {
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
				l.log.Warn("Valkey indisponível; atualização parada", "err", err)
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
		l.Handle(ctx, vals[1])
	}
}

// Parse lê e valida um pedido; devolve o kind e o alvo como vão para o banco.
func Parse(payload string) (kind, target string, ok bool) {
	var req Request
	if err := json.Unmarshal([]byte(payload), &req); err != nil {
		return "", "", false
	}
	t := strings.TrimSpace(req.Target)
	switch req.Kind {
	case rdap.KindASN:
		n, err := strconv.ParseUint(t, 10, 32)
		if err != nil {
			return "", "", false
		}
		return rdap.KindASN, strconv.FormatUint(n, 10), true
	case rdap.KindPrefix:
		p, err := netip.ParsePrefix(t)
		if err != nil || p != p.Masked() || p.Addr().Zone() != "" {
			return "", "", false
		}
		return rdap.KindPrefix, p.String(), true
	}
	return "", "", false
}

// Handle atende um pedido (a mensagem como veio da fila).
func (l *Loop) Handle(ctx context.Context, payload string) {
	kind, target, ok := Parse(payload)
	if !ok {
		l.log.Warn("pedido de atualização inválido", "payload", clip(payload))
		return
	}
	found, err := l.store.MarkRescan(ctx, kind, target)
	if err != nil {
		if ctx.Err() == nil {
			l.log.Warn("não consegui marcar a atualização", "kind", kind, "target", target, "err", err)
		}
		return
	}
	if !found {
		l.log.Debug("pedido de atualização para alvo inexistente ou inativo", "kind", kind, "target", target)
		return
	}
	l.served.Add(1)
	l.log.Debug("atualização pedida", "kind", kind, "target", target)
}

func clip(s string) string {
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}
