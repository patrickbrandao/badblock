// Comando collector-apnic: importa periodicamente o arquivo delegated-extended
// do RIR para as tabelas <fonte>_* do BadBlock. Veja collector-apnic --help.
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

	"github.com/patrickbrandao/badblock/apps/apnic/collector/internal/buildinfo"
	"github.com/patrickbrandao/badblock/apps/apnic/collector/internal/collector"
	"github.com/patrickbrandao/badblock/apps/apnic/collector/internal/config"
	"github.com/patrickbrandao/badblock/apps/apnic/collector/internal/fetch"
	"github.com/patrickbrandao/badblock/apps/apnic/collector/internal/rir"
	"github.com/patrickbrandao/badblock/apps/apnic/collector/internal/store"
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
		fmt.Fprintln(os.Stderr, rir.App+":", err)
		return 2
	}
	if cfg.ShowVersion {
		fmt.Println(rir.App, buildinfo.String())
		return 0
	}
	log := newLogger(cfg)
	log.Info("iniciando", "version", buildinfo.Version, "commit", buildinfo.Commit,
		"source", cfg.SourceURL, "md5", cfg.MD5URL, "interval", cfg.SyncInterval.String(), "retry", cfg.RetryInterval.String(),
		"min_records", cfg.MinRecords, "postgres", config.Redact(cfg.PostgresURL))

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
		ua = "badblock-" + rir.App + "/" + buildinfo.Version + " (+https://github.com/patrickbrandao/badblock)"
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
		MD5URL:           cfg.MD5URL,
		Registry:         rir.Registry,
		MinRecords:       cfg.MinRecords,
		RemovalThreshold: cfg.RemovalThreshold,
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
		r, c := res.Run, res.Changes
		log.Info("arquivo novo aplicado", "version", res.Version, "serial", r.Header.Serial, "md5", r.MD5,
			"asn_records", r.ASNRecords, "ipv4_records", r.IPv4Records, "ipv6_records", r.IPv6Records,
			"prefixes_v4", r.PrefixesV4, "prefixes_v6", r.PrefixesV6,
			"asn_inserted", c.ASNInserted, "asn_updated", c.ASNUpdated, "asn_deleted", c.ASNDeleted,
			"prefix_inserted", c.PrefixInserted, "prefix_updated", c.PrefixUpdated, "prefix_deleted", c.PrefixDeleted,
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
