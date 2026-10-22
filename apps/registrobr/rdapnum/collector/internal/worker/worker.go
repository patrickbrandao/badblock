// Package worker mantém a linha da instância em registrobr_rdapnum_worker (a
// batida a cada 30 s, com a taxa em vigor, os contadores, o IP de saída e a
// pausa) e relê registrobr_rdapnum_config no mesmo ritmo: rate_limit e
// scan_interval valem para todas as instâncias, e a chave que falta, que não
// pode ser lida ou que tem valor inválido vale a reserva do ambiente
// (RATE_LIMIT, SCAN_INTERVAL).
package worker

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/config"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/store"
)

// Every é o intervalo da batida e da releitura de _config.
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

// Options configura o Worker.
type Options struct {
	UUID, Host, Version string
	RateLimit           int           // reserva (RATE_LIMIT), por minuto
	ScanInterval        time.Duration // reserva (SCAN_INTERVAL)
	Proxy               bool          // há PROXY_URL
	Stats               func(*store.Worker)
	Refreshes           func() int64
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
	w.checkRate(opt.RateLimit)
	return w
}

// Rate é a taxa em vigor, em consultas por minuto.
func (w *Worker) Rate() int { return int(w.rate.Load()) }

// ScanInterval é a meta por alvo em vigor.
func (w *Worker) ScanInterval() time.Duration { return time.Duration(w.interval.Load()) }

// Apply aplica uma leitura de _config (cfg nil = não pôde ser lida: valem as
// reservas).
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
		w.checkRate(rate)
	}
}

func (w *Worker) warnInvalid(key, value string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if last, ok := w.warned[key]; ok && last == value {
		return
	}
	w.warned[key] = value
	w.log.Warn("valor inválido em registrobr_rdapnum_config; usando a reserva", "key", key, "value", value)
}

func (w *Worker) clearWarn(key string) {
	w.mu.Lock()
	delete(w.warned, key)
	w.mu.Unlock()
}

// checkRate avisa quando, sem proxy, a taxa passa do que o servidor aceita de
// um IP só sem truncar (fonte.md#limite-do-servidor).
func (w *Worker) checkRate(rate int) {
	if config.RateNeedsProxy(rate, w.opt.Proxy) {
		w.log.Warn("taxa acima de 8/min sem proxy: espere respostas truncadas", "rate_limit", rate)
	}
}

// Beat relê _config e grava a batida.
func (w *Worker) Beat(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cfg, err := w.store.Config(ctx)
	if err != nil {
		cfg = nil
	}
	w.Apply(cfg)
	hb := store.Worker{UUID: w.opt.UUID, Host: w.opt.Host, Version: w.opt.Version, RateLimit: w.Rate(), Proxy: w.opt.Proxy}
	if w.opt.Stats != nil {
		w.opt.Stats(&hb)
	}
	if w.opt.Refreshes != nil {
		hb.Refreshes = w.opt.Refreshes()
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
