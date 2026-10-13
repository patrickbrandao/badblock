// Package worker mantém a linha da instância em rdns_worker (a batida a cada
// 30 s, com a taxa em vigor e os contadores) e relê rdns_config no mesmo
// ritmo: rate_limit e scan_interval valem para todas as instâncias, e a chave
// que falta, que não pode ser lida ou que tem valor inválido vale a reserva do
// ambiente (RATE_LIMIT, SCAN_INTERVAL).
package worker

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/patrickbrandao/badblock/apps/rdns/collector/internal/store"
)

// Every é o intervalo da batida e da releitura de rdns_config.
const Every = 30 * time.Second

// Store é o que a batida usa (store.Store).
type Store interface {
	Config(ctx context.Context) (map[string]string, error)
	Heartbeat(ctx context.Context, w store.Worker) error
}

// RateSetter recebe a taxa em vigor (limiter.Limiter).
type RateSetter interface {
	SetRate(rate int)
}

// Counters são os contadores da instância desde a subida.
type Counters struct {
	Queries     func() int64
	LiveQueries func() int64
	Prefixes    func() int64
}

// Options configura o Worker.
type Options struct {
	UUID, Host, Version string
	RateLimit           int           // reserva (RATE_LIMIT)
	ScanInterval        time.Duration // reserva (SCAN_INTERVAL)
	ScanLease           time.Duration
	Counters            Counters
}

// Worker é a instância.
type Worker struct {
	store Store
	lim   RateSetter
	opt   Options
	log   *slog.Logger

	rate     atomic.Int64
	interval atomic.Int64

	mu     sync.Mutex
	warned map[string]string // último valor inválido avisado de cada chave
}

// New cria o Worker com os valores de reserva em vigor.
func New(st Store, lim RateSetter, opt Options, log *slog.Logger) *Worker {
	w := &Worker{store: st, lim: lim, opt: opt, log: log, warned: map[string]string{}}
	w.rate.Store(int64(opt.RateLimit))
	w.interval.Store(int64(opt.ScanInterval))
	lim.SetRate(opt.RateLimit)
	w.checkLease(opt.RateLimit)
	return w
}

// UUID é a instância.
func (w *Worker) UUID() string { return w.opt.UUID }

// Rate é a taxa em vigor, em consultas por segundo.
func (w *Worker) Rate() int { return int(w.rate.Load()) }

// ScanInterval é a meta por prefixo em vigor.
func (w *Worker) ScanInterval() time.Duration { return time.Duration(w.interval.Load()) }

// Apply aplica uma leitura de rdns_config (cfg nil = não pôde ser lida: valem
// as reservas).
func (w *Worker) Apply(cfg map[string]string) {
	rate, interval := w.opt.RateLimit, w.opt.ScanInterval
	if v, ok := cfg["rate_limit"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			rate = n
			w.clearWarn("rate_limit")
		} else {
			w.warnInvalid("rate_limit", v)
		}
	}
	if v, ok := cfg["scan_interval"]; ok {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			interval = d
			w.clearWarn("scan_interval")
		} else {
			w.warnInvalid("scan_interval", v)
		}
	}
	oldRate, oldInterval := w.Rate(), w.ScanInterval()
	if rate == oldRate && interval == oldInterval {
		return
	}
	w.rate.Store(int64(rate))
	w.interval.Store(int64(interval))
	w.log.Info("configuração alterada", "rate_limit", rate, "scan_interval", interval.String())
	if rate != oldRate {
		w.lim.SetRate(rate)
		w.checkLease(rate)
	}
}

func (w *Worker) warnInvalid(key, value string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if last, ok := w.warned[key]; ok && last == value {
		return
	}
	w.warned[key] = value
	w.log.Warn("valor inválido em rdns_config; usando a reserva", "key", key, "value", value)
}

func (w *Worker) clearWarn(key string) {
	w.mu.Lock()
	delete(w.warned, key)
	w.mu.Unlock()
}

// checkLease avisa quando um /22 com uma nova tentativa por endereço (2.048
// consultas) não cabe no tempo de consultas do lease.
func (w *Worker) checkLease(rate int) {
	if float64(rate)*(w.opt.ScanLease-5*time.Second).Seconds() < 2048 {
		w.log.Warn("taxa baixa demais para SCAN_LEASE", "rate_limit", rate, "scan_lease", w.opt.ScanLease.String())
	}
}

// Beat relê rdns_config e grava a batida.
func (w *Worker) Beat(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cfg, err := w.store.Config(ctx)
	if err != nil {
		cfg = nil
	}
	w.Apply(cfg)
	c := w.opt.Counters
	hb := store.Worker{UUID: w.opt.UUID, Host: w.opt.Host, Version: w.opt.Version, RateLimit: w.Rate()}
	if c.Queries != nil {
		hb.Queries = c.Queries()
	}
	if c.LiveQueries != nil {
		hb.LiveQueries = c.LiveQueries()
	}
	if c.Prefixes != nil {
		hb.Prefixes = c.Prefixes()
	}
	if herr := w.store.Heartbeat(ctx, hb); herr != nil {
		return herr
	}
	return err
}

// Loop bate a cada 30 s até o contexto acabar (a primeira batida é a do
// main, na subida).
func (w *Worker) Loop(ctx context.Context) {
	t := time.NewTicker(Every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := w.Beat(ctx); err != nil && ctx.Err() == nil {
				w.log.Warn("batida falhou", "err", err)
			}
		}
	}
}
