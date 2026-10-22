// Comando collector-registrobr-rdapnum: consulta o RDAP do registro.br de
// cada ASN e bloco brasileiro para as tabelas registrobr_rdapnum_* do
// BadBlock. Veja collector-registrobr-rdapnum --help.
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

	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/buildinfo"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/config"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/derive"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/limiter"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/parse"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/rdap"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/refresh"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/reprocess"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/scan"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/store"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/worker"
)

const name = "collector-registrobr-rdapnum"

func main() {
	os.Exit(run())
}

func run() int {
	cfg, err := config.Load(os.Args[1:], os.Getenv, os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, name+":", err)
		return 2
	}
	if cfg.ShowVersion {
		fmt.Println(name, buildinfo.String())
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
		"rdap_url", cfg.RDAPURL, "proxy", config.RedactProxy(cfg.ProxyURL), "rate_limit", cfg.RateLimit,
		"scan_interval", cfg.ScanInterval.String(), "scan_lease", cfg.ScanLease.String(), "sources", sources,
		"parser_version", parse.Version, "refresh", cfg.RedisURL != "", "postgres", config.Redact(cfg.PostgresURL))

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
		if rdb, err = refresh.NewClient(cfg.RedisURL); err != nil {
			log.Error("REDIS_URL inválida", "err", err)
			return 2
		}
		defer rdb.Close()
	} else {
		log.Info("atualização desligada (REDIS_URL vazia)")
	}

	lim := limiter.New(cfg.RateLimit)
	client := rdap.New(rdap.Options{BaseURL: cfg.RDAPURL, Proxy: cfg.ProxyURL, Timeout: cfg.HTTPTimeout,
		UserAgent: config.UserAgent(buildinfo.Version)})
	var rf *refresh.Loop
	if rdb != nil {
		rf = refresh.New(rdb, st, log)
	}
	var scanner *scan.Scanner
	w := worker.New(st, lim, worker.Options{
		UUID: id, Host: host, Version: buildinfo.Version,
		RateLimit: cfg.RateLimit, ScanInterval: cfg.ScanInterval, Proxy: cfg.ProxyURL != nil,
		Stats: func(hb *store.Worker) { scanner.Stats(hb) },
		Refreshes: func() int64 {
			if rf == nil {
				return 0
			}
			return rf.Served()
		},
	}, log)
	scanner = scan.New(st, client, lim, scan.Options{
		Worker: id, Lease: cfg.ScanLease, Interval: w.ScanInterval,
		PauseAfter: cfg.TruncatedPauseAfter, Pause: cfg.TruncatedPause,
		Proxy: cfg.ProxyURL != nil, SameIPWarn: cfg.ProxySameIPWarn,
	}, log)

	// A linha em _worker vem antes da fila (scan_worker é FK para ela).
	if err := w.Beat(ctx); err != nil {
		log.Warn("batida falhou", "err", err)
	}

	var wg sync.WaitGroup
	wg.Go(func() { der.Loop(ctx, cfg.DeriveInterval) })
	wg.Go(func() { w.Loop(ctx) })
	wg.Go(func() { reprocess.New(st, log).Run(ctx) })
	var loops sync.WaitGroup
	loops.Go(func() { scanner.Run(ctx) })
	if rf != nil {
		loops.Go(func() { rf.Run(ctx) })
	}

	<-ctx.Done()
	log.Info("encerrando")
	// O scanner devolve o alvo em consulta à fila; apagar a linha da
	// instância devolve o que sobrar.
	done := make(chan struct{})
	go func() { loops.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		log.Warn("encerramento sem esperar a consulta em curso")
	}
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
