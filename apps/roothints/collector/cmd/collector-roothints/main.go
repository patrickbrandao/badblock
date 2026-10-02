// Comando collector-roothints: importa periodicamente o arquivo named.root da
// InterNIC (root hints: os servidores raiz do DNS) para as tabelas
// roothints_* do BadBlock. Veja collector-roothints --help.
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

	"github.com/patrickbrandao/badblock/apps/roothints/collector/internal/buildinfo"
	"github.com/patrickbrandao/badblock/apps/roothints/collector/internal/collector"
	"github.com/patrickbrandao/badblock/apps/roothints/collector/internal/config"
	"github.com/patrickbrandao/badblock/apps/roothints/collector/internal/fetch"
	"github.com/patrickbrandao/badblock/apps/roothints/collector/internal/store"
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
		fmt.Fprintln(os.Stderr, "collector-roothints:", err)
		return 2
	}
	if cfg.ShowVersion {
		fmt.Println("collector-roothints", buildinfo.String())
		return 0
	}
	log := newLogger(cfg)
	log.Info("iniciando", "version", buildinfo.Version, "commit", buildinfo.Commit,
		"source", cfg.SourceURL, "md5", cfg.MD5URL, "interval", cfg.SyncInterval.String(), "retry", cfg.RetryInterval.String(),
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
		ua = "badblock-collector-roothints/" + buildinfo.Version + " (+https://github.com/patrickbrandao/badblock)"
	}
	src := &fetch.Fetcher{
		Client:     fetch.NewHTTPClient(),
		UserAgent:  ua,
		MaxBytes:   1 << 20, // o arquivo tem ~3,3 KB
		Retries:    2,
		RetryDelay: 10 * time.Second,
	}
	col := collector.New(st, src, collector.Options{
		URL:              cfg.SourceURL,
		MD5URL:           cfg.MD5URL,
		MinServers:       cfg.MinServers,
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
		c, r := res.Changes, res.Run
		log.Info("arquivo novo aplicado", "version", res.Version, "zone_serial", r.Header.ZoneSerial,
			"last_update", r.Header.LastUpdate.Format("2006-01-02"), "md5", r.MD5, "sha256", r.SHA256, "bytes", r.Bytes,
			"servers", r.Servers, "ipv4_addresses", r.IPv4Addresses, "ipv6_addresses", r.IPv6Addresses,
			"server_inserted", c.ServerInserted, "server_updated", c.ServerUpdated, "server_deleted", c.ServerDeleted,
			"warnings", len(r.Warnings), "forced", r.Forced, "ms", time.Since(start).Milliseconds())
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
