// Comando registry-api: API HTTP de consulta de ASNs e prefixos IP do
// BadBlock. Veja registry-api --help.
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
	"strings"
	"syscall"
	"time"

	"github.com/patrickbrandao/badblock/apps/registry-api/internal/buildinfo"
	"github.com/patrickbrandao/badblock/apps/registry-api/internal/cache"
	"github.com/patrickbrandao/badblock/apps/registry-api/internal/config"
	"github.com/patrickbrandao/badblock/apps/registry-api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/registry-api/internal/httpapi"
	"github.com/patrickbrandao/badblock/apps/registry-api/internal/realip"
	"github.com/patrickbrandao/badblock/apps/registry-api/internal/store"
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
		fmt.Println("registry-api", buildinfo.String())
		return 0
	}
	log := newLogger(cfg.LogLevel, cfg.LogFormat)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.PostgresURL, int32(cfg.DBPoolMax))
	if err != nil {
		log.Error("configuração do banco inválida", "err", err)
		return 2
	}
	defer st.Close()

	var c cache.Cache = cache.Noop{}
	if cfg.CacheEnabled && cfg.RedisURL != "" {
		v, err := cache.NewValkey(cache.Options{URL: cfg.RedisURL, TTL: cfg.CacheTTL, Timeout: cfg.CacheTimeout})
		if err != nil {
			log.Error("REDIS_URL inválida", "err", err)
			return 2
		}
		c = v
		log.Info("cache ligado", "url", config.Redact(cfg.RedisURL), "ttl", cfg.CacheTTL.String())
	} else {
		log.Info("cache desligado")
	}

	rip, err := realip.New(cfg.TrustedProxies, cfg.RealIPHeaders)
	if err != nil {
		log.Error("configuração de IP real inválida", "err", err)
		return 2
	}

	watcher := dataset.NewWatcher(st, cfg.DatasetPoll, log)
	// O banco pode subir depois da API: a primeira leitura falha em silêncio
	// e o watcher tenta de novo em segundo plano.
	if err := watcher.Refresh(ctx); err != nil {
		log.Warn("versão do dataset ainda indisponível", "err", err)
	}
	go watcher.Run(ctx)

	api := httpapi.New(st, watcher, c, rip, log, httpapi.Config{
		Version:     buildinfo.Version,
		CORSOrigin:  cfg.CORSOrigin,
		DBTimeout:   cfg.DBTimeout,
		ListTimeout: cfg.ListTimeout,
		AccessLog:   cfg.AccessLog,
	})
	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.HTTPPort),
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      cfg.ListTimeout + 15*time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	errc := make(chan error, 1)
	go func() {
		log.Info("registry-api em escuta", "addr", srv.Addr, "version", buildinfo.String(),
			"dataset", watcher.Current().Version)
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Error("servidor parou", "err", err)
			return 1
		}
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdown); err != nil {
		log.Warn("encerramento forçado", "err", err)
	}
	log.Info("registry-api encerrada")
	return 0
}

// healthcheck é usado pelo HEALTHCHECK do container (imagem sem shell).
func healthcheck() int {
	port := os.Getenv("HTTP_PORT")
	if port == "" {
		port = "8001"
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
