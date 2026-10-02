// Comando collector-anatel-pst: importa periodicamente o ZIP de prestadoras
// de serviços de telecomunicações da Anatel para as tabelas anatel_pst_* do
// BadBlock. Veja collector-anatel-pst --help.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/patrickbrandao/badblock/apps/anatel/pst/collector/internal/archive"
	"github.com/patrickbrandao/badblock/apps/anatel/pst/collector/internal/buildinfo"
	"github.com/patrickbrandao/badblock/apps/anatel/pst/collector/internal/collector"
	"github.com/patrickbrandao/badblock/apps/anatel/pst/collector/internal/config"
	"github.com/patrickbrandao/badblock/apps/anatel/pst/collector/internal/fetch"
	"github.com/patrickbrandao/badblock/apps/anatel/pst/collector/internal/store"
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
		fmt.Fprintln(os.Stderr, "collector-anatel-pst:", err)
		return 2
	}
	if cfg.ShowVersion {
		fmt.Println("collector-anatel-pst", buildinfo.String())
		return 0
	}
	log := newLogger(cfg)
	log.Info("iniciando", "version", buildinfo.Version, "commit", buildinfo.Commit,
		"source", cfg.SourceURL, "interval", cfg.SyncInterval.String(), "retry", cfg.RetryInterval.String(),
		"min_providers", cfg.MinProviders, "postgres", config.Redact(cfg.PostgresURL))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := openStore(ctx, cfg, log)
	if err != nil {
		log.Error("sem conexão com o Postgres", "err", err)
		return 1
	}
	defer st.Close()

	ua := cfg.UserAgent
	if ua == "" {
		ua = "badblock-collector-anatel-pst/" + buildinfo.Version + " (+https://github.com/patrickbrandao/badblock)"
	}
	src := &fetch.Fetcher{
		Client:     fetch.NewHTTPClient(),
		UserAgent:  ua,
		MaxBytes:   64 << 20,
		Retries:    2,
		RetryDelay: 10 * time.Second,
	}
	col := collector.New(st, src, collector.Options{
		URL:              cfg.SourceURL,
		MinProviders:     cfg.MinProviders,
		RemovalThreshold: cfg.RemovalThreshold,
		MaxCSVBytes:      archive.MaxCSVBytes,
	}, log)

	if cfg.Once {
		if err := cycle(ctx, col, cfg, log); err != nil {
			return 1
		}
		return 0
	}

	// Verifica já na subida e depois a cada SYNC_INTERVAL. Depois de uma
	// falha (fonte fora do ar, banco ou migrations ainda subindo), tenta de
	// novo em RETRY_INTERVAL.
	for {
		wait := cfg.SyncInterval
		if err := cycle(ctx, col, cfg, log); err != nil {
			wait = min(cfg.RetryInterval, cfg.SyncInterval)
		}
		log.Debug("próxima verificação", "in", wait.String())
		select {
		case <-ctx.Done():
			log.Info("encerrando")
			return 0
		case <-time.After(wait):
		}
	}
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

func cycle(ctx context.Context, col *collector.Collector, cfg *config.Config, log *slog.Logger) error {
	ctx, cancel := context.WithTimeout(ctx, cfg.RunTimeout)
	defer cancel()
	start := time.Now()
	res, err := col.RunOnce(ctx, cfg.Force)
	if err != nil {
		log.Error("verificação falhou", "err", err, "ms", time.Since(start).Milliseconds())
		return err
	}
	switch res.Outcome {
	case collector.Unchanged:
		log.Info("fonte sem mudança", "reason", res.Reason, "version", res.Version,
			"ms", time.Since(start).Milliseconds())
	case collector.Applied:
		c, r := res.Changes, res.Run
		log.Info("arquivo novo aplicado", "version", res.Version, "sha256", r.SHA256, "csv_sha256", r.CSVSHA256,
			"csv_modified_at", r.CSVModifiedAt, "rows", r.Rows, "rows_cpf", r.RowsCPF, "duplicates", r.Duplicates,
			"skipped", r.Skipped, "providers", r.Providers, "services", r.Services,
			"provider_inserted", c.ProviderInserted, "provider_updated", c.ProviderUpdated, "provider_deleted", c.ProviderDeleted,
			"service_inserted", c.ServiceInserted, "service_updated", c.ServiceUpdated, "service_deleted", c.ServiceDeleted,
			"warnings", len(r.Warnings), "forced", r.Forced, "ms", time.Since(start).Milliseconds())
		for _, w := range r.Warnings {
			log.Warn("aviso do parser", "warning", w)
		}
	}
	return nil
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
