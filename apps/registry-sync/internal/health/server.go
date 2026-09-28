// Package health expõe /health, /status e /ping do registry-sync. Não passa
// pelo Traefik: serve ao healthcheck do Docker e a quem opera o serviço.
package health

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/patrickbrandao/badblock/apps/registry-sync/internal/buildinfo"
	"github.com/patrickbrandao/badblock/apps/registry-sync/internal/syncer"
)

const title = "BadBlock Registry Sync"

// Pinger confere a conexão com o banco.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Handler monta as rotas.
func Handler(db Pinger, sync *syncer.Syncer) http.Handler {
	mux := http.NewServeMux()
	status := func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		body := map[string]any{
			"success":   1,
			"status":    "online",
			"timestamp": time.Now().Unix(),
			"message":   title,
			"version":   buildinfo.Version,
			"checks":    map[string]string{"postgres": "online"},
		}
		code := http.StatusOK
		if err := db.Ping(ctx); err != nil {
			body["success"] = 0
			body["status"] = "offline"
			body["checks"] = map[string]string{"postgres": "offline"}
			code = http.StatusServiceUnavailable
		}
		if sync != nil {
			body["sync"] = sync.Status()
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(body)
	}
	for _, p := range []string{"/health", "/status"} {
		mux.HandleFunc("GET "+p, status)
		mux.HandleFunc("POST "+p, status)
	}
	mux.HandleFunc("GET /ping", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("pong"))
	})
	return mux
}
