// Comando api-iana: API HTTP dos registros de numeração da IANA (blocos de
// ASN e IP por RIR, special-purpose e RDAP) coletados pelo collector-iana.
// Veja api-iana --help.
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
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/patrickbrandao/badblock/apps/iana/api/internal/buildinfo"
	"github.com/patrickbrandao/badblock/apps/iana/api/internal/cache"
	"github.com/patrickbrandao/badblock/apps/iana/api/internal/config"
	"github.com/patrickbrandao/badblock/apps/iana/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/iana/api/internal/httpapi"
	"github.com/patrickbrandao/badblock/apps/iana/api/internal/realip"
	"github.com/patrickbrandao/badblock/apps/iana/api/internal/store"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	os.Exit(run())
}

// healthcheck é o HEALTHCHECK da imagem distroless (sem shell nem curl).
func healthcheck() int {
	port := os.Getenv("HTTP_PORT")
	if port == "" {
		port = "8080"
	}
	base := "/" + strings.Trim(os.Getenv("BASE_PATH"), "/")
	if base == "/" {
		base = "/iana"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + base + "/ping")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "HTTP", resp.StatusCode)
		return 1
	}
	return 0
}

func run() int {
	cfg, err := config.Load(os.Args[1:], os.Getenv, os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "api-iana:", err)
		return 2
	}
	if cfg.ShowVersion {
		fmt.Println("api-iana", buildinfo.String())
		return 0
	}
	log := newLogger(cfg)
	log.Info("iniciando", "version", buildinfo.Version, "commit", buildinfo.Commit,
		"port", cfg.HTTPPort, "base_path", cfg.BasePath, "postgres", config.Redact(cfg.PostgresURL),
		"cache", cfg.CacheEnabled && cfg.RedisURL != "")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := openStore(ctx, cfg, log)
	if err != nil {
		log.Error("sem conexão com o Postgres", "err", err)
		return 1
	}
	defer st.Close()

	var c cache.Cache = cache.Noop{}
	if cfg.CacheEnabled && cfg.RedisURL != "" {
		v, err := cache.NewValkey(cfg.RedisURL, cfg.CacheTTL, cfg.CacheTimeout)
		if err != nil {
			log.Error("REDIS_URL inválida", "err", err)
			return 2
		}
		if err := v.Ping(ctx); err != nil {
			log.Warn("Valkey indisponível no boot; seguindo sem cache até ele voltar", "err", err)
		}
		c = v
	}

	watcher := dataset.NewWatcher(st, cfg.DatasetPoll, log)
	if err := watcher.Refresh(ctx); err != nil {
		log.Warn("não consegui ler a versão dos dados", "err", err)
	}
	go watcher.Run(ctx)

	rip, _ := realip.New(cfg.TrustedProxies, cfg.RealIPHeaders) // já validado no config
	api := httpapi.New(st, watcher, c, rip, log, httpapi.Config{
		BasePath:   cfg.BasePath,
		Version:    buildinfo.Version,
		CORSOrigin: cfg.CORSOrigin,
		DBTimeout:  cfg.DBTimeout,
		AccessLog:  cfg.AccessLog,
	})

	srv := &http.Server{
		Addr:              ":" + strconv.Itoa(cfg.HTTPPort),
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("ouvindo", "addr", srv.Addr)

	select {
	case err := <-errc:
		log.Error("servidor HTTP parou", "err", err)
		return 1
	case <-ctx.Done():
	}
	log.Info("encerrando")
	shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdown); err != nil {
		log.Error("shutdown", "err", err)
		return 1
	}
	return 0
}

// openStore insiste na conexão por até 2 minutos: no boot do stack o Postgres
// e as migrations podem ainda estar subindo.
func openStore(ctx context.Context, cfg *config.Config, log *slog.Logger) (*store.Store, error) {
	deadline := time.Now().Add(2 * time.Minute)
	for {
		c, cancel := context.WithTimeout(ctx, 10*time.Second)
		st, err := store.Open(c, cfg.PostgresURL, cfg.DBPoolMax)
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
	return slog.New(h).With("app", "api-iana")
}
