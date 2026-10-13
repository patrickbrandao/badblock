// Comando collector-rdns: varre o DNS reverso (PTR) dos IPv4 cadastrados em
// rdns_prefix para as tabelas rdns_* do BadBlock. Veja collector-rdns --help.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/patrickbrandao/badblock/apps/rdns/collector/internal/buildinfo"
	"github.com/patrickbrandao/badblock/apps/rdns/collector/internal/config"
	"github.com/patrickbrandao/badblock/apps/rdns/collector/internal/derive"
	"github.com/patrickbrandao/badblock/apps/rdns/collector/internal/limiter"
	"github.com/patrickbrandao/badblock/apps/rdns/collector/internal/live"
	"github.com/patrickbrandao/badblock/apps/rdns/collector/internal/resolver"
	"github.com/patrickbrandao/badblock/apps/rdns/collector/internal/scan"
	"github.com/patrickbrandao/badblock/apps/rdns/collector/internal/store"
	"github.com/patrickbrandao/badblock/apps/rdns/collector/internal/worker"
)

func main() {
	os.Exit(run())
}

func run() int {
	cfg, err := config.Load(os.Args[1:], os.Getenv, os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "collector-rdns:", err)
		return 2
	}
	if cfg.ShowVersion {
		fmt.Println("collector-rdns", buildinfo.String())
		return 0
	}
	log := newLogger(cfg)
	host, _ := os.Hostname()
	id := worker.NewUUID()
	sources := strings.Join(cfg.Sources, ",")
	if sources == "" {
		sources = "none"
	}
	log.Info("iniciando", "version", buildinfo.Version, "commit", buildinfo.Commit, "worker", id, "host", host,
		"dns_servers", strings.Join(cfg.DNSServers, ","), "rate_limit", cfg.RateLimit,
		"scan_interval", cfg.ScanInterval.String(), "scan_lease", cfg.ScanLease.String(), "sources", sources,
		"live", cfg.RedisURL != "", "postgres", config.Redact(cfg.PostgresURL))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := openStore(ctx, cfg, log)
	if err != nil {
		log.Error("sem conexão com o Postgres", "err", err)
		return 1
	}
	defer st.Close()

	der := derive.New(st, cfg.Sources, cfg.RunTimeout, log)
	if cfg.Once {
		if err := der.Once(ctx, cfg.Force); err != nil {
			if errors.Is(err, derive.ErrBusy) {
				log.Warn("outra instância está derivando")
			}
			return 1
		}
		return 0
	}

	var rdb *redis.Client
	if cfg.RedisURL != "" {
		if rdb, err = live.NewClient(cfg.RedisURL); err != nil {
			log.Error("REDIS_URL inválida", "err", err)
			return 2
		}
		defer rdb.Close()
	} else {
		log.Info("tempo real desligado (REDIS_URL vazia)")
	}

	lim := limiter.New(cfg.RateLimit)
	res := resolver.New(resolver.Options{
		Servers: cfg.DNSServers, Timeout: cfg.DNSTimeout, Retries: cfg.DNSRetries,
		Inflight: cfg.DNSInflight, Limiter: lim, Log: log,
	})
	var lv *live.Loop
	if rdb != nil {
		lv = live.New(rdb, st, res, id, log)
	}
	var scanner *scan.Scanner
	w := worker.New(st, lim, worker.Options{
		UUID: id, Host: host, Version: buildinfo.Version,
		RateLimit: cfg.RateLimit, ScanInterval: cfg.ScanInterval, ScanLease: cfg.ScanLease,
		Counters: worker.Counters{
			Queries:  res.Queries,
			Prefixes: func() int64 { return scanner.Prefixes() },
			LiveQueries: func() int64 {
				if lv == nil {
					return 0
				}
				return lv.Served()
			},
		},
	}, log)
	scanner = scan.New(st, res, lim, scan.Options{Worker: id, Lease: cfg.ScanLease, Interval: w.ScanInterval}, log)

	// A linha em rdns_worker vem antes da fila (scan_worker é FK para ela).
	if err := w.Beat(ctx); err != nil {
		log.Warn("batida falhou", "err", err)
	}

	var wg sync.WaitGroup
	wg.Go(func() { lim.Run(ctx) })
	wg.Go(func() { der.Loop(ctx, cfg.DeriveInterval) })
	wg.Go(func() { w.Loop(ctx) })
	var loops sync.WaitGroup
	loops.Go(func() { scanner.Run(ctx) })
	if lv != nil {
		loops.Go(func() { lv.Run(ctx) })
	}

	<-ctx.Done()
	log.Info("encerrando")
	// O scanner abandona os prefixos em varredura (até 10 s); apagar a linha
	// da instância devolve à fila o que sobrar.
	loops.Wait()
	c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := st.DeleteWorker(c, id); err != nil {
		log.Warn("não consegui apagar a linha da instância", "err", err)
	}
	wg.Wait()
	return 0
}

// openStore insiste na conexão por até 2 minutos: no boot do stack o Postgres
// e as migrations podem ainda estar subindo.
func openStore(ctx context.Context, cfg *config.Config, log *slog.Logger) (*store.Store, error) {
	deadline := time.Now().Add(2 * time.Minute)
	for {
		c, cancel := context.WithTimeout(ctx, 10*time.Second)
		st, err := store.Open(c, cfg.PostgresURL)
		cancel()
		if err == nil {
			return st, nil
		}
		if ctx.Err() != nil || time.Now().After(deadline) {
			return nil, err
		}
		log.Warn("Postgres indisponível; tentando de novo", "err", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

func newLogger(cfg *config.Config) *slog.Logger {
	var level slog.Level
	_ = level.UnmarshalText([]byte(cfg.LogLevel))
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler = slog.NewJSONHandler(os.Stdout, opts)
	if cfg.LogFormat == "text" {
		h = slog.NewTextHandler(os.Stdout, opts)
	}
	return slog.New(h).With("app", store.AppName)
}
