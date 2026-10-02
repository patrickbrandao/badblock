// Comando collector-iana: importa periodicamente os registros de numeração da
// IANA (10 arquivos tratados como um dataset) para as tabelas iana_* do
// BadBlock. Veja collector-iana --help.
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

	"github.com/patrickbrandao/badblock/apps/iana/collector/internal/buildinfo"
	"github.com/patrickbrandao/badblock/apps/iana/collector/internal/collector"
	"github.com/patrickbrandao/badblock/apps/iana/collector/internal/config"
	"github.com/patrickbrandao/badblock/apps/iana/collector/internal/fetch"
	"github.com/patrickbrandao/badblock/apps/iana/collector/internal/parse"
	"github.com/patrickbrandao/badblock/apps/iana/collector/internal/store"
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
		fmt.Fprintln(os.Stderr, "collector-iana:", err)
		return 2
	}
	if cfg.ShowVersion {
		fmt.Println("collector-iana", buildinfo.String())
		return 0
	}
	log := newLogger(cfg)
	log.Info("iniciando", "version", buildinfo.Version, "commit", buildinfo.Commit,
		"iana", cfg.IANABaseURL, "rdap", cfg.RDAPBaseURL, "interval", cfg.SyncInterval.String(), "retry", cfg.RetryInterval.String(),
		"postgres", config.Redact(cfg.PostgresURL))

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
		ua = "badblock-collector-iana/" + buildinfo.Version + " (+https://github.com/patrickbrandao/badblock)"
	}
	src := &fetch.Fetcher{
		Client:     fetch.NewHTTPClient(),
		UserAgent:  ua,
		MaxBytes:   8 << 20, // os arquivos têm de 0,5 a 25 KB
		Retries:    2,
		RetryDelay: 10 * time.Second,
	}
	col := collector.New(st, src, collector.Options{
		IANABaseURL:      cfg.IANABaseURL,
		RDAPBaseURL:      cfg.RDAPBaseURL,
		RemovalThreshold: cfg.RemovalThreshold,
		Limits:           parse.DefaultLimits(),
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
		files := map[string]int{}
		for _, f := range res.Run.Files {
			if f.Rows != nil {
				files[f.Name] = *f.Rows
			}
		}
		log.Info("dataset novo aplicado", "version", res.Version, "sha256", res.Run.SHA256,
			"changed_files", res.ChangedFiles(), "rows", files, "changes", res.Changes, "total_changes", res.Changes.Total(),
			"warnings", len(res.Run.Warnings), "forced", res.Run.Forced, "ms", time.Since(start).Milliseconds())
		for _, w := range res.Run.Warnings {
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
