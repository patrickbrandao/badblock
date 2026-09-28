// Comando registry-sync: importa e sincroniza os dados públicos de ASNs e
// prefixos IP com o PostgreSQL do BadBlock. Veja registry-sync --help.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/patrickbrandao/badblock/apps/registry-sync/internal/buildinfo"
	"github.com/patrickbrandao/badblock/apps/registry-sync/internal/config"
	"github.com/patrickbrandao/badblock/apps/registry-sync/internal/fetch"
	"github.com/patrickbrandao/badblock/apps/registry-sync/internal/health"
	"github.com/patrickbrandao/badblock/apps/registry-sync/internal/source"
	"github.com/patrickbrandao/badblock/apps/registry-sync/internal/store"
	"github.com/patrickbrandao/badblock/apps/registry-sync/internal/syncer"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) > 0 && args[0] == "healthcheck" {
		return healthcheck()
	}

	cfg, err := config.Load(args, config.Getenv, os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		fmt.Fprintln(os.Stderr, "use --help para ver as opções")
		return 2
	}
	if cfg.ShowVersion {
		fmt.Println("registry-sync", buildinfo.String())
		return 0
	}

	log := newLogger(cfg.LogLevel, cfg.LogFormat)
	sources, err := source.Select(cfg.Sources)
	if err != nil {
		log.Error("configuração inválida", "err", err)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	service := !cfg.Once && !cfg.Rebuild
	st, err := connect(ctx, log, cfg.PostgresURL, service)
	if err != nil {
		log.Error("sem banco", "err", err)
		return 1
	}
	defer st.Close()

	fetcher := &fetch.Fetcher{
		Client:     fetch.NewHTTPClient(),
		UserAgent:  "badblock-registry-sync/" + buildinfo.Version + " (+https://github.com/patrickbrandao/badblock)",
		SourcesDir: cfg.SourcesDir,
		MaxBytes:   cfg.MaxDownloadBytes,
		Retries:    2,
		RetryDelay: 5 * time.Second,
	}
	sy := syncer.New(syncer.Config{
		Sources:          sources,
		DefaultInterval:  cfg.SyncInterval,
		Intervals:        cfg.SourceIntervals,
		RemovalThreshold: cfg.RemovalThreshold,
		RemovalGrace:     cfg.RemovalGrace,
		RawDir:           filepath.Join(cfg.DataDir, "raw"),
		RawKeep:          cfg.RawKeep,
		RunRetention:     cfg.RunRetention,
		Force:            cfg.Force,
		SkipMinRecords:   cfg.SourcesDir != "",
	}, st, fetcher, log)

	switch {
	case cfg.Rebuild:
		bs, err := sy.Rebuild(ctx)
		if err != nil {
			log.Error("reconstrução falhou", "err", err)
			return 1
		}
		log.Info("central reconstruído", "dataset", bs.DatasetID, "changes", bs.Changes,
			"asns", bs.ASNs, "prefixes", bs.Prefixes, "holders", bs.Holders, "duration", bs.Duration.String())
		return 0

	case cfg.Once:
		dctx, cancel := context.WithTimeout(ctx, cfg.HTTPTimeout*time.Duration(len(sources)+1))
		defer cancel()
		res, err := sy.Cycle(dctx, true)
		log.Info("ciclo concluído", "checked", len(res.Checked), "applied", res.Applied,
			"failed", res.Failed, "duration", res.Duration.Round(time.Millisecond).String())
		if err != nil {
			log.Error("ciclo com erro", "err", err)
			return 1
		}
		return 0
	}

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:           health.Handler(st, sy),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		log.Info("health em escuta", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("servidor de health parou", "err", err)
			stop()
		}
	}()

	log.Info("registry-sync iniciado", "version", buildinfo.String(), "sources", len(sources),
		"interval", cfg.SyncInterval.String(), "fixture_dir", cfg.SourcesDir)
	_ = sy.Run(ctx)

	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown)
	log.Info("registry-sync encerrado")
	return 0
}

// connect abre o banco e espera as migrations. Como serviço, insiste até o
// banco ficar pronto: o container pode subir antes do Postgres.
func connect(ctx context.Context, log *slog.Logger, url string, wait bool) (*store.Store, error) {
	st, err := store.Open(ctx, url, 4)
	if err != nil {
		return nil, err
	}
	for attempt := 1; ; attempt++ {
		err = st.Ping(ctx)
		if err == nil {
			err = st.SchemaReady(ctx)
		}
		if err == nil {
			return st, nil
		}
		if !wait && attempt >= 5 {
			st.Close()
			return nil, err
		}
		log.Warn("banco ainda não está pronto", "attempt", attempt, "err", err)
		select {
		case <-ctx.Done():
			st.Close()
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

// healthcheck é usado pelo HEALTHCHECK do container (imagem sem shell).
func healthcheck() int {
	port := os.Getenv("HTTP_PORT")
	if port == "" {
		port = "8002"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + "/ping")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func newLogger(level, format string) *slog.Logger {
	var lv slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lv}
	if strings.ToLower(format) == "text" {
		return slog.New(slog.NewTextHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, opts))
}
