// Package httpapi implementa as rotas HTTP da api-rootzone.
//
// A API é dona de tudo abaixo do caminho de base (BASE_PATH, padrão
// /rootzone): /rootzone/tld/br é a versão atual e /rootzone/v1/tld/br fixa a
// v1. Uma v2 futura ganha /rootzone/v2/... e passa a ser a versão sem prefixo.
//
// Rota de dados nova: o handler (em handlers_<assunto>.go), uma linha em
// dataRoutes (registro nas duas versões e no índice), a consulta em
// internal/store, os tipos em types.go e o path no openapi.yaml.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/patrickbrandao/badblock/apps/rootzone/api/internal/cache"
	"github.com/patrickbrandao/badblock/apps/rootzone/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/rootzone/api/internal/realip"
	"github.com/patrickbrandao/badblock/apps/rootzone/api/internal/store"
)

// Store são as consultas que a API usa (implementadas por store.Store).
type Store interface {
	Ping(ctx context.Context) error
	Dataset(ctx context.Context) (*store.Dataset, error)
	Job(ctx context.Context) (*store.Job, error)
	TLDs(ctx context.Context) ([]store.TLD, error)
	Delegation(ctx context.Context, tld string) (*store.Delegation, error)
}

// DatasetView devolve a versão atual dos dados (dataset.Watcher).
type DatasetView interface {
	Current() dataset.Snapshot
}

// Config são as opções da API.
type Config struct {
	BasePath   string // ex.: /rootzone
	Version    string
	CORSOrigin string
	DBTimeout  time.Duration
	AccessLog  bool
}

// CurrentAPIVersion é a versão servida nas rotas sem /vN.
const CurrentAPIVersion = "v1"

// API reúne as dependências das rotas.
type API struct {
	store  Store
	data   DatasetView
	cache  cache.Cache
	realip *realip.Resolver
	log    *slog.Logger
	cfg    Config
	sf     singleflight.Group
	routes []string // padrões registrados no mux (o openapi_test.go confere com o manifesto)
}

// New cria a API.
func New(st Store, data DatasetView, c cache.Cache, rip *realip.Resolver, log *slog.Logger, cfg Config) *API {
	if cfg.DBTimeout <= 0 {
		cfg.DBTimeout = 5 * time.Second
	}
	if cfg.CORSOrigin == "" {
		cfg.CORSOrigin = "*"
	}
	cfg.BasePath = "/" + strings.Trim(cfg.BasePath, "/")
	return &API{store: st, data: data, cache: c, realip: rip, log: log, cfg: cfg}
}

// Handler monta todas as rotas com os middlewares.
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	b := a.cfg.BasePath
	a.routes = nil
	handle := func(pattern string, h http.HandlerFunc) {
		mux.HandleFunc(pattern, h)
		a.routes = append(a.routes, pattern)
	}

	// Rotas de dados e meta: sem versão (= atual) e com /v1.
	for _, v := range []string{"", "/" + CurrentAPIVersion} {
		handle("GET "+b+v+"/{$}", a.handleIndex)
		for _, rt := range a.dataRoutes() {
			handle("GET "+b+v+rt.pattern, rt.handler)
		}
		handle("GET "+b+v+"/meta", a.handleMeta)
	}
	handle("GET "+b, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, b+"/", http.StatusMovedPermanently)
	})

	// Saúde e manifesto OpenAPI: fora do versionamento.
	for _, p := range []string{"/health", "/status"} {
		handle("GET "+b+p, a.handleStatus)
		handle("POST "+b+p, a.handleStatus)
	}
	handle("GET "+b+"/ping", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte("pong"))
	})
	handle("GET "+b+"/openapi.yaml", a.handleOpenAPI)

	handle("/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "rota inexistente; veja "+b+"/")
	})
	return a.middleware(mux)
}

// statusWriter guarda o status e o tamanho para o log.
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

// middleware aplica cabeçalhos comuns, CORS e recover, e registra o acesso
// com o IP real do cliente.
func (a *API) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Access-Control-Allow-Origin", a.cfg.CORSOrigin)
		h.Set("Access-Control-Expose-Headers", "ETag, X-Cache, X-Dataset-Version")
		if a.cfg.Version != "" {
			h.Set("Server", "badblock-api-rootzone/"+a.cfg.Version)
		}

		defer func() {
			if rec := recover(); rec != nil {
				a.log.Error("panic", "path", r.URL.Path, "panic", fmt.Sprint(rec))
				if sw.status == 0 {
					writeError(sw, http.StatusInternalServerError, "internal_error", "erro interno")
				}
			}
			// O /ping é o healthcheck do Docker, a cada 15 s: fica fora do log.
			if a.cfg.AccessLog && r.URL.Path != a.cfg.BasePath+"/ping" {
				ip, src := a.realip.ClientIP(r)
				a.log.Info("http", "method", r.Method, "path", r.URL.Path, "query", r.URL.RawQuery,
					"status", sw.status, "bytes", sw.bytes, "ms", time.Since(start).Milliseconds(),
					"client_ip", ip.String(), "client_ip_source", src, "cache", sw.Header().Get("X-Cache"))
			}
		}()

		if r.Method == http.MethodOptions {
			h.Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "If-None-Match, Content-Type")
			h.Set("Access-Control-Max-Age", "86400")
			sw.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(sw, r)
	})
}

// --- respostas ------------------------------------------------------------

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	var body errorBody
	body.Error.Code = code
	body.Error.Message = msg
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// httpError é um erro com resposta pronta, devolvido de dentro de build.
type httpError struct {
	status    int
	code, msg string
}

func (e *httpError) Error() string { return e.msg }

func notFound(msg string) error {
	return &httpError{status: http.StatusNotFound, code: "not_found", msg: msg}
}

// writeFailure traduz erros de validação e de consulta em respostas.
func (a *API) writeFailure(w http.ResponseWriter, r *http.Request, err error) {
	var he *httpError
	switch {
	case errors.As(err, &he):
		writeError(w, he.status, he.code, he.msg)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "registro não encontrado")
	case errors.Is(err, context.DeadlineExceeded):
		a.log.Warn("consulta lenta", "path", r.URL.Path, "err", err)
		writeError(w, http.StatusGatewayTimeout, "timeout", "a consulta demorou demais")
	default:
		a.log.Error("falha na consulta", "path", r.URL.Path, "err", err)
		writeError(w, http.StatusServiceUnavailable, "database_unavailable", "banco de dados indisponível")
	}
}

// maxCachedBody evita guardar respostas enormes no Valkey.
const maxCachedBody = 8 << 20

// serveCached responde uma consulta cujo resultado depende só da chave e da
// versão do dataset: confere o ETag, tenta o cache, calcula uma vez por chave
// (singleflight) e guarda o resultado.
func (a *API) serveCached(w http.ResponseWriter, r *http.Request, key string, build func(ctx context.Context, snap dataset.Snapshot) (any, error)) {
	snap := a.data.Current()
	if !snap.Ready() {
		writeError(w, http.StatusServiceUnavailable, "dataset_not_ready",
			"a primeira sincronização do collector-rootzone ainda não terminou; tente em alguns minutos")
		return
	}
	etag := etagFor(snap.Version, key)
	if match(r.Header.Get("If-None-Match"), etag) {
		setDataHeaders(w, snap, etag, "HIT")
		w.WriteHeader(http.StatusNotModified)
		return
	}
	cacheKey := "badblock:api-rootzone:" + snap.Version + ":" + key
	if body, ok := a.cache.Get(r.Context(), cacheKey); ok {
		writeBody(w, snap, etag, "HIT", body)
		return
	}
	v, err, _ := a.sf.Do(cacheKey, func() (any, error) {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), a.cfg.DBTimeout)
		defer cancel()
		obj, err := build(ctx, snap)
		if err != nil {
			return nil, err
		}
		body, err := json.Marshal(obj)
		if err != nil {
			return nil, err
		}
		body = append(body, '\n')
		if len(body) <= maxCachedBody {
			a.cache.Set(context.WithoutCancel(r.Context()), cacheKey, body)
		}
		return body, nil
	})
	if err != nil {
		a.writeFailure(w, r, err)
		return
	}
	state := "BYPASS"
	if a.cache.Enabled() {
		state = "MISS"
	}
	writeBody(w, snap, etag, state, v.([]byte))
}

func writeBody(w http.ResponseWriter, snap dataset.Snapshot, etag, cacheState string, body []byte) {
	setDataHeaders(w, snap, etag, cacheState)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(body)
}

func setDataHeaders(w http.ResponseWriter, snap dataset.Snapshot, etag, cacheState string) {
	h := w.Header()
	h.Set("ETag", etag)
	h.Set("Cache-Control", "public, max-age=300")
	h.Set("X-Cache", cacheState)
	h.Set("X-Dataset-Version", snap.Version)
}

// etagFor é fraco (W/) porque o mesmo conteúdo pode ser serializado de novo.
func etagFor(version, key string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(version))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(key))
	return fmt.Sprintf(`W/"%x"`, h.Sum64())
}

func match(ifNoneMatch, etag string) bool {
	if ifNoneMatch == "" {
		return false
	}
	for v := range strings.SplitSeq(ifNoneMatch, ",") {
		v = strings.TrimSpace(v)
		if v == "*" || strings.TrimPrefix(v, "W/") == strings.TrimPrefix(etag, "W/") {
			return true
		}
	}
	return false
}
