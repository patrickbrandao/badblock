// Package scan é o laço da fila do collector-registrobr-rdapnum: espera a vez
// no limitador, pega um alvo vencido (com lease), consulta o RDAP, grava a
// resposta crua e o estado do alvo numa transação e, se a coleta foi
// conclusiva, processa a resposta noutra (collector.md#coleta-de-um-alvo).
//
// Um alvo por vez por instância: a 32/min sai uma consulta a cada ~1,9 s e
// cada uma leva ~0,1 s. Truncadas seguidas pausam a instância; truncada e
// erro põem o alvo em espera escalonada.
package scan

import (
	"context"
	"errors"
	"log/slog"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/rdap"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/reprocess"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/store"
)

// Store é a fila, a gravação e o processamento (store.Store).
type Store interface {
	reprocess.Processor
	Claim(ctx context.Context, worker string, lease, interval time.Duration) (*store.Claim, error)
	Release(ctx context.Context, worker string, c *store.Claim) error
	SaveAttempt(ctx context.Context, worker string, c *store.Claim, a store.Attempt) (store.Saved, error)
}

// Fetcher consulta o RDAP (rdap.Client).
type Fetcher interface {
	Fetch(ctx context.Context, kind, target string) rdap.Response
}

// Waiter é o limitador (limiter.Limiter).
type Waiter interface {
	Wait(ctx context.Context) error
}

// Options configura o Scanner. Os valores zerados valem os padrões da spec.
type Options struct {
	Worker     string               // registrobr_rdapnum_worker.uuid desta instância
	Lease      time.Duration        // SCAN_LEASE
	Interval   func() time.Duration // scan_interval em vigor
	PauseAfter int                  // TRUNCATED_PAUSE_AFTER (3)
	Pause      time.Duration        // TRUNCATED_PAUSE (2m)
	Proxy      bool                 // há PROXY_URL
	SameIPWarn int                  // PROXY_SAME_IP_WARN (0 desliga)
	Idle       time.Duration        // espera sem alvo vencido (30s)
	RetryAfter time.Duration        // espera depois de uma falha no Postgres (10s)
}

// backoffs é a espera do alvo depois de cada truncada ou erro seguido:
// 1 min, 5 min, 30 min, 2 h e, daí em diante, 6 h.
var backoffs = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour}

// Backoff é a espera do alvo com failures truncadas e erros seguidos
// (contando a desta tentativa, >= 1).
func Backoff(failures int) time.Duration {
	if failures < 1 {
		failures = 1
	}
	if failures > len(backoffs) {
		failures = len(backoffs)
	}
	return backoffs[failures-1]
}

// ringSize é a janela de respostas em que se contam os IPs de saída distintos.
const ringSize = 100

// Scanner é o laço da fila de uma instância.
type Scanner struct {
	store Store
	rdap  Fetcher
	lim   Waiter
	opt   Options
	log   *slog.Logger
	now   func() time.Time

	requests, complete, truncated, notFound, errs atomic.Int64

	mu          sync.Mutex
	pausedUntil time.Time
	truncRun    int // truncadas seguidas
	ring        []netip.Addr
	ringPos     int
	lastEgress  netip.Addr
	sameRun     int
	sameWarned  bool
}

// New cria o Scanner.
func New(st Store, f Fetcher, lim Waiter, opt Options, log *slog.Logger) *Scanner {
	def := func(v *time.Duration, d time.Duration) {
		if *v <= 0 {
			*v = d
		}
	}
	def(&opt.Pause, 2*time.Minute)
	def(&opt.Idle, 30*time.Second)
	def(&opt.RetryAfter, 10*time.Second)
	if opt.PauseAfter <= 0 {
		opt.PauseAfter = 3
	}
	return &Scanner{store: st, rdap: f, lim: lim, opt: opt, log: log, now: time.Now}
}

// Stats preenche os contadores da instância (a batida em _worker).
func (s *Scanner) Stats(w *store.Worker) {
	w.Requests = s.requests.Load()
	w.Complete = s.complete.Load()
	w.Truncated = s.truncated.Load()
	w.NotFound = s.notFound.Load()
	w.Errors = s.errs.Load()
	s.mu.Lock()
	defer s.mu.Unlock()
	w.EgressIP = s.lastEgress
	seen := map[netip.Addr]bool{}
	for _, ip := range s.ring {
		seen[ip] = true
	}
	w.EgressIPs = len(seen)
	if s.now().Before(s.pausedUntil) {
		w.PausedUntil = s.pausedUntil
	}
}

// PausedUntil é o fim da pausa por truncadas (zero = sem pausa).
func (s *Scanner) PausedUntil() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pausedUntil
}

func sleep(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// Run pega e coleta alvos até o contexto acabar.
func (s *Scanner) Run(ctx context.Context) {
	for ctx.Err() == nil {
		if left := s.PausedUntil().Sub(s.now()); left > 0 {
			sleep(ctx, left)
			continue
		}
		if err := s.lim.Wait(ctx); err != nil {
			return
		}
		c, err := s.store.Claim(ctx, s.opt.Worker, s.opt.Lease, s.opt.Interval())
		switch {
		case err != nil:
			if ctx.Err() == nil {
				s.log.Error("não consegui pegar um alvo da fila", "err", err)
				sleep(ctx, s.opt.RetryAfter)
			}
		case c == nil:
			sleep(ctx, s.opt.Idle) // sem alvo vencido
		default:
			if !s.Collect(ctx, c) {
				sleep(ctx, s.opt.RetryAfter)
			}
		}
	}
}

// Collect coleta um alvo pego da fila: consulta, grava e processa. Devolve
// false quando a gravação falhou no Postgres.
func (s *Scanner) Collect(ctx context.Context, c *store.Claim) bool {
	claimed := s.now()
	target := c.Target()
	resp := s.rdap.Fetch(ctx, c.Kind, target)
	if ctx.Err() != nil {
		// Encerramento no meio da consulta: o alvo volta à fila sem gravar nada.
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := s.store.Release(rctx, s.opt.Worker, c); err != nil {
			s.log.Debug("não consegui devolver o alvo à fila", "target", target, "err", err)
		}
		return true
	}
	s.observe(resp)

	a := store.Attempt{Resp: resp}
	if !resp.Result.Conclusive() {
		a.RetryIn = Backoff(c.Failures + 1)
	}
	if resp.Result == rdap.Error {
		a.ErrorMsg = resp.Err.Error()
		if resp.ProxyErr {
			s.log.Warn("falha no proxy", "target", target, "err", a.ErrorMsg)
		} else {
			s.log.Warn("erro na consulta", "target", target, "http_status", resp.HTTPStatus, "err", a.ErrorMsg,
				"retry_at", s.now().Add(a.RetryIn).UTC().Format(time.RFC3339))
		}
	}

	// A gravação tem o resto do lease; no encerramento, termina mesmo assim.
	sctx, cancel := context.WithDeadline(context.WithoutCancel(ctx), claimed.Add(s.opt.Lease))
	defer cancel()
	saved, err := s.store.SaveAttempt(sctx, s.opt.Worker, c, a)
	ms := s.now().Sub(claimed).Milliseconds()
	switch {
	case errors.Is(err, store.ErrLeaseLost):
		s.log.Warn("lease perdido; resposta descartada", "target", target, "ms", ms)
		return true
	case err != nil:
		s.log.Error("não consegui gravar a resposta", "target", target, "err", err)
		return false
	}
	var egress string
	if resp.EgressIP.IsValid() {
		egress = resp.EgressIP.String()
	}
	s.log.Debug("alvo coletado", "kind", c.Kind, "target", target, "result", resp.Result.String(),
		"http_status", resp.HTTPStatus, "egress_ip", egress, "changed", saved.Changed, "ms", ms)
	if resp.Result.Conclusive() {
		reprocess.ProcessOne(sctx, s.store, saved.ResponseUUID, s.log)
	}
	return true
}

// observe conta a consulta, cuida da pausa por truncadas e acompanha o IP de
// saída.
func (s *Scanner) observe(r rdap.Response) {
	s.requests.Add(1)
	switch r.Result {
	case rdap.Complete:
		s.complete.Add(1)
	case rdap.Truncated:
		s.truncated.Add(1)
	case rdap.NotFound:
		s.notFound.Add(1)
	default:
		s.errs.Add(1)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	switch r.Result {
	case rdap.Complete:
		s.truncRun = 0
	case rdap.Truncated:
		s.truncRun++
		if s.truncRun >= s.opt.PauseAfter {
			s.pausedUntil = s.now().Add(s.opt.Pause)
			var egress string
			if r.EgressIP.IsValid() {
				egress = r.EgressIP.String()
			}
			s.log.Warn("respostas truncadas seguidas; pausando", "count", s.truncRun, "egress_ip", egress,
				"until", s.pausedUntil.UTC().Format(time.RFC3339))
		}
	}

	if !r.EgressIP.IsValid() {
		return
	}
	if len(s.ring) < ringSize {
		s.ring = append(s.ring, r.EgressIP)
	} else {
		s.ring[s.ringPos] = r.EgressIP
		s.ringPos = (s.ringPos + 1) % ringSize
	}
	if r.EgressIP == s.lastEgress {
		s.sameRun++
	} else {
		s.sameRun, s.sameWarned = 1, false
	}
	s.lastEgress = r.EgressIP
	if s.opt.Proxy && s.opt.SameIPWarn > 0 && s.sameRun >= s.opt.SameIPWarn && !s.sameWarned {
		s.sameWarned = true
		s.log.Warn("proxy sem rotação de IP", "egress_ip", r.EgressIP.String(), "count", s.sameRun)
	}
}
