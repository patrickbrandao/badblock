// Package httpapi implementa as rotas HTTP da api-rdns.
//
// A API é dona de tudo abaixo do caminho de base (BASE_PATH, padrão /rdns):
// /rdns/ip/200.160.2.3 é a versão atual e /rdns/v1/ip/200.160.2.3 fixa a v1.
// Uma v2 futura ganha /rdns/v2/... e passa a ser a versão sem prefixo.
//
// Diferente das outras APIs, não há versão de dataset (decisão #33): a chave
// de cache não leva versão, o ETag é o hash do corpo e o bloco scan substitui
// o dataset.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/patrickbrandao/badblock/apps/rdns/api/internal/cache"
	"github.com/patrickbrandao/badblock/apps/rdns/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/rdns/api/internal/live"
	"github.com/patrickbrandao/badblock/apps/rdns/api/internal/realip"
	"github.com/patrickbrandao/badblock/apps/rdns/api/internal/store"
	"github.com/patrickbrandao/badblock/apps/rdns/api/openapi"
)

// Store são as consultas que a API usa (implementadas por store.Store).
type Store interface {
	Ping(ctx context.Context) error
	IP(ctx context.Context, ip netip.Addr) (*store.IPRow, error)
	Prefix(ctx context.Context, p netip.Prefix) (*store.PrefixRows, error)
	Config(ctx context.Context) (map[string]string, error)
	ScanStats(ctx context.Context, interval time.Duration) (*store.ScanStats, error)
	Derivations(ctx context.Context) ([]store.Derivation, error)
	Workers(ctx context.Context) ([]store.Worker, error)
	Job(ctx context.Context) (*store.Job, error)
}

// DatasetView diz se já há prefixos (dataset.Watcher).
type DatasetView interface {
	Current() dataset.Snapshot
}

// Live é o pedido de tempo real (live.Requester); nil sem REDIS_URL.
type Live interface {
	Resolve(ctx context.Context, ip string, reread func(ctx context.Context) ([]byte, error)) (live.Outcome, error)
	QueueLen(ctx context.Context) (int64, error)
	Ping(ctx context.Context) error
}

// Config são as opções da API.
type Config struct {
	BasePath   string // ex.: /rdns
	Version    string
	CORSOrigin string
	DBTimeout  time.Duration
	AccessLog  bool
}

// CurrentAPIVersion é a versão servida nas rotas sem /vN.
const CurrentAPIVersion = "v1"

// keyPrefix é o prefixo das chaves de cache desta API (sem versão).
const keyPrefix = "badblock:api-rdns:"

// API reúne as dependências das rotas.
type API struct {
	store  Store
	data   DatasetView
	cache  cache.Cache
	live   Live
	realip *realip.Resolver
	log    *slog.Logger
	cfg    Config
	sf     singleflight.Group
	routes []string // padrões registrados em Handler (o teste do manifesto confere)
}

// New cria a API. lv nil desliga o tempo real (live=true responde unavailable).
func New(st Store, data DatasetView, c cache.Cache, lv Live, rip *realip.Resolver, log *slog.Logger, cfg Config) *API {
	if cfg.DBTimeout <= 0 {
		cfg.DBTimeout = 5 * time.Second
	}
	if cfg.CORSOrigin == "" {
		cfg.CORSOrigin = "*"
	}
	cfg.BasePath = "/" + strings.Trim(cfg.BasePath, "/")
	return &API{store: st, data: data, cache: c, live: lv, realip: rip, log: log, cfg: cfg}
}

// Handler monta todas as rotas com os middlewares.
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	b := a.cfg.BasePath
	a.routes = nil
	handle := func(pattern string, h http.HandlerFunc) {
		a.routes = append(a.routes, pattern)
		mux.HandleFunc(pattern, h)
	}

	// Rotas de dados: sem versão (= atual) e com /v1.
	for _, v := range []string{"", "/" + CurrentAPIVersion} {
		handle("GET "+b+v+"/{$}", a.handleIndex)
		handle("GET "+b+v+"/ip/{ip}", a.handleIP)
		handle("GET "+b+v+"/prefix/{ip}/{len}", a.handlePrefix)
		handle("GET "+b+v+"/meta", a.handleMeta)
	}
	handle("GET "+b, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, b+"/", http.StatusMovedPermanently)
	})

	// Saúde e manifesto: fora do versionamento.
	for _, p := range []string{"/health", "/status"} {
		handle("GET "+b+p, a.handleStatus)
		handle("POST "+b+p, a.handleStatus)
	}
	handle("GET "+b+"/ping", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte("pong"))
	})
	handle("GET "+b+"/openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		w.Header().Set("Cache-Control", "public, max-age=300")
		_, _ = w.Write(openapi.Spec)
	})

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
		h.Set("Access-Control-Expose-Headers", "ETag, X-Cache")
		if a.cfg.Version != "" {
			h.Set("Server", "badblock-api-rdns/"+a.cfg.Version)
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
	w.Header().Del("ETag")
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

// writeNotReady é o 503 antes do primeiro prefixo.
func writeNotReady(w http.ResponseWriter) {
	writeError(w, http.StatusServiceUnavailable, "dataset_not_ready",
		"nenhum prefixo cadastrado ainda: a primeira derivação do collector-rdns não terminou; tente em alguns minutos")
}

// maxCachedBody evita guardar respostas enormes no Valkey.
const maxCachedBody = 8 << 20

// render serializa o corpo de uma resposta de dados (JSON compacto e uma
// quebra de linha no fim, como o json.Encoder).
func render(obj any) ([]byte, error) {
	body, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return append(body, '\n'), nil
}

// store grava a chave fria (só 200 e até 8 MiB).
func (a *API) storeBody(ctx context.Context, key string, body []byte) {
	if len(body) <= maxCachedBody {
		a.cache.Set(context.WithoutCancel(ctx), keyPrefix+key, body)
	}
}

// fetch devolve o corpo de uma consulta: do cache ou, uma vez por chave
// (singleflight), do banco, com prazo DB_TIMEOUT independente do cliente que
// chegou primeiro. O segundo valor é o X-Cache (HIT, MISS ou BYPASS).
func (a *API) fetch(r *http.Request, key string, build func(ctx context.Context) (any, error)) ([]byte, string, error) {
	if body, ok := a.cache.Get(r.Context(), keyPrefix+key); ok {
		return body, "HIT", nil
	}
	v, err, _ := a.sf.Do(key, func() (any, error) {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), a.cfg.DBTimeout)
		defer cancel()
		obj, err := build(ctx)
		if err != nil {
			return nil, err
		}
		body, err := render(obj)
		if err != nil {
			return nil, err
		}
		a.storeBody(r.Context(), key, body)
		return body, nil
	})
	if err != nil {
		return nil, "", err
	}
	state := "BYPASS"
	if a.cache.Enabled() {
		state = "MISS"
	}
	return v.([]byte), state, nil
}

// serveCached responde uma rota de dados: sem prefixos, 503; o corpo vem do
// cache ou do banco (fetch); o ETag é o hash do corpo, e If-None-Match igual
// responde 304.
func (a *API) serveCached(w http.ResponseWriter, r *http.Request, key string, build func(ctx context.Context) (any, error)) {
	if !a.data.Current().Ready() {
		writeNotReady(w)
		return
	}
	body, state, err := a.fetch(r, key, build)
	if err != nil {
		a.writeFailure(w, r, err)
		return
	}
	etag := etagFor(body)
	h := w.Header()
	h.Set("ETag", etag)
	h.Set("Cache-Control", "public, max-age=300")
	h.Set("X-Cache", state)
	if match(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(body)
}

// etagFor é o FNV-1a de 64 bits do corpo, fraco (W/) porque o mesmo conteúdo
// pode ser serializado de novo.
func etagFor(body []byte) string {
	h := fnv.New64a()
	_, _ = h.Write(body)
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
