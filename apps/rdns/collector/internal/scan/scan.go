// Package scan é o laço da fila do collector-rdns: pega um prefixo vencido de
// rdns_prefix (com lease), põe todos os endereços dele no limitador, em
// ordem, pergunta o PTR de cada um e grava o resultado no fim, numa transação
// — ou abandona o prefixo quando o prazo do lease acaba.
//
// Cada instância mantém vários prefixos em varredura: pega o próximo sempre
// que restam menos de LowWater endereços esperando vez no limitador, até
// MaxActive prefixos ao mesmo tempo. Um prefixo por vez deixaria a taxa
// ociosa esperando os últimos prazos de cada um.
package scan

import (
	"context"
	"errors"
	"log/slog"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/patrickbrandao/badblock/apps/rdns/collector/internal/limiter"
	"github.com/patrickbrandao/badblock/apps/rdns/collector/internal/resolver"
	"github.com/patrickbrandao/badblock/apps/rdns/collector/internal/store"
)

// Store é a fila e a gravação (store.Store).
type Store interface {
	Claim(ctx context.Context, worker string, lease, interval time.Duration) (*store.Claim, error)
	SaveScan(ctx context.Context, worker string, c *store.Claim, results []resolver.Result, elapsed time.Duration) (store.Counts, error)
	Release(ctx context.Context, worker string, c *store.Claim) error
}

// Lookuper pergunta o PTR de um endereço (resolver.Resolver).
type Lookuper interface {
	Lookup(ctx context.Context, ip netip.Addr, prio limiter.Priority, first *limiter.Ticket) (resolver.Result, error)
}

// Queue é o limitador (limiter.Limiter).
type Queue interface {
	Reserve(p limiter.Priority) *limiter.Ticket
	Waiting(p limiter.Priority) int
}

// Options configura o Scanner. Os valores zerados valem os padrões da spec.
type Options struct {
	Worker        string               // rdns_worker.uuid desta instância
	Lease         time.Duration        // SCAN_LEASE
	Interval      func() time.Duration // scan_interval em vigor
	MaxActive     int                  // prefixos em varredura ao mesmo tempo (16)
	LowWater      int                  // pega o próximo abaixo disso esperando no limitador (256)
	SaveBudget    time.Duration        // parte final do lease reservada à gravação (5s)
	Idle          time.Duration        // espera sem prefixo vencido (30s)
	RetryAfter    time.Duration        // espera depois de uma falha no Postgres (10s)
	Poll          time.Duration        // releitura das condições para pegar o próximo (100ms)
	ShutdownGrace time.Duration        // prazo para abandonar os prefixos no encerramento (10s)
}

// Scanner é o laço da fila de uma instância.
type Scanner struct {
	store Store
	res   Lookuper
	q     Queue
	opt   Options
	log   *slog.Logger

	prefixes atomic.Int64

	mu         sync.Mutex
	pauseUntil time.Time
}

// New cria o Scanner.
func New(st Store, res Lookuper, q Queue, opt Options, log *slog.Logger) *Scanner {
	def := func(v *time.Duration, d time.Duration) {
		if *v <= 0 {
			*v = d
		}
	}
	def(&opt.SaveBudget, 5*time.Second)
	def(&opt.Idle, 30*time.Second)
	def(&opt.RetryAfter, 10*time.Second)
	def(&opt.Poll, 100*time.Millisecond)
	def(&opt.ShutdownGrace, 10*time.Second)
	if opt.MaxActive <= 0 {
		opt.MaxActive = 16
	}
	if opt.LowWater <= 0 {
		opt.LowWater = 256
	}
	return &Scanner{store: st, res: res, q: q, opt: opt, log: log}
}

// Prefixes é quantos prefixos esta instância varreu até o fim.
func (s *Scanner) Prefixes() int64 { return s.prefixes.Load() }

func (s *Scanner) pause(d time.Duration) {
	s.mu.Lock()
	if until := time.Now().Add(d); until.After(s.pauseUntil) {
		s.pauseUntil = until
	}
	s.mu.Unlock()
}

func (s *Scanner) paused() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Now().Before(s.pauseUntil)
}

// Run pega e varre prefixos até o contexto acabar; então para de pegar,
// abandona os que estão em varredura (até ShutdownGrace) e volta.
func (s *Scanner) Run(ctx context.Context) {
	var wg sync.WaitGroup
	done := make(chan struct{}, s.opt.MaxActive)
	active := 0
	for ctx.Err() == nil {
		if active < s.opt.MaxActive && !s.paused() && s.q.Waiting(limiter.Scan) < s.opt.LowWater {
			c, err := s.store.Claim(ctx, s.opt.Worker, s.opt.Lease, s.opt.Interval())
			switch {
			case err != nil:
				if ctx.Err() == nil {
					s.log.Error("não consegui pegar um prefixo da fila", "err", err)
					s.pause(s.opt.RetryAfter)
				}
			case c == nil:
				s.pause(s.opt.Idle) // sem prefixo vencido
			default:
				// Reserva aqui, antes de olhar o limitador de novo: a vez de cada
				// endereço, em ordem.
				addrs := addresses(c.Prefix)
				tickets := make([]*limiter.Ticket, len(addrs))
				for i := range addrs {
					tickets[i] = s.q.Reserve(limiter.Scan)
				}
				active++
				wg.Go(func() {
					s.scanPrefix(ctx, c, addrs, tickets)
					done <- struct{}{}
				})
				continue
			}
		}
		select {
		case <-ctx.Done():
		case <-done:
			active--
		case <-time.After(s.opt.Poll):
		}
	}
	finished := make(chan struct{})
	go func() {
		wg.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(s.opt.ShutdownGrace):
		s.log.Warn("encerramento sem esperar os prefixos em varredura", "active", active)
	}
}

// addresses devolve todos os endereços do prefixo, em ordem (inclusive o .0
// e o .255, que também podem ter PTR).
func addresses(p netip.Prefix) []netip.Addr {
	p = p.Masked()
	n := 1 << (p.Addr().BitLen() - p.Bits())
	out := make([]netip.Addr, 0, n)
	for a, i := p.Addr(), 0; i < n; a, i = a.Next(), i+1 {
		out = append(out, a)
	}
	return out
}

func (s *Scanner) scanPrefix(ctx context.Context, c *store.Claim, addrs []netip.Addr, tickets []*limiter.Ticket) {
	start := time.Now()
	qctx, cancel := context.WithDeadline(ctx, start.Add(s.opt.Lease-s.opt.SaveBudget))
	defer cancel()

	results := make([]resolver.Result, len(addrs))
	var incomplete atomic.Bool
	var wg sync.WaitGroup
	for i, ip := range addrs {
		if err := tickets[i].Wait(qctx); err != nil {
			// Prazo ou encerramento: as reservas que sobram são canceladas.
			for _, t := range tickets[i:] {
				_ = t.Wait(qctx)
			}
			incomplete.Store(true)
			break
		}
		wg.Go(func() {
			r, err := s.res.Lookup(qctx, ip, limiter.Scan, tickets[i])
			if err != nil {
				incomplete.Store(true)
			}
			results[i] = r
		})
	}
	wg.Wait()
	ms := time.Since(start).Milliseconds()

	if ctx.Err() != nil || incomplete.Load() {
		if ctx.Err() == nil {
			s.log.Warn("varredura abandonada: passou de SCAN_LEASE", "prefix", c.Prefix.String(), "ms", ms)
		}
		rctx, rcancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer rcancel()
		if err := s.store.Release(rctx, s.opt.Worker, c); err != nil {
			s.log.Debug("não consegui devolver o prefixo à fila", "prefix", c.Prefix.String(), "err", err)
		}
		return
	}

	// A gravação tem o resto do lease; no encerramento, termina mesmo assim.
	sctx, scancel := context.WithDeadline(context.WithoutCancel(ctx), start.Add(s.opt.Lease))
	defer scancel()
	counts, err := s.store.SaveScan(sctx, s.opt.Worker, c, results, time.Since(start))
	switch {
	case errors.Is(err, store.ErrLeaseLost):
		s.log.Warn("lease perdido; resultado descartado", "prefix", c.Prefix.String(), "ms", ms)
	case err != nil:
		s.log.Error("não consegui gravar a varredura", "prefix", c.Prefix.String(), "err", err)
		s.pause(s.opt.RetryAfter)
	default:
		s.prefixes.Add(1)
		s.log.Debug("prefixo varrido", "prefix", c.Prefix.String(), "ptr", counts.PTR, "no_ptr", counts.NoPTR,
			"failed", counts.Failed, "changed", counts.Changed, "ms", time.Since(start).Milliseconds())
	}
}
