// Package httpapi implementa as rotas HTTP da registry-api.
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

	"github.com/patrickbrandao/badblock/apps/registry-api/internal/cache"
	"github.com/patrickbrandao/badblock/apps/registry-api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/registry-api/internal/realip"
	"github.com/patrickbrandao/badblock/apps/registry-api/internal/store"
)

// Store são as consultas que a API usa (implementadas por store.Store).
type Store interface {
	Ping(ctx context.Context) error
	Chain(ctx context.Context, ip netip.Addr) ([]store.Prefix, error)
	Covering(ctx context.Context, p netip.Prefix) ([]store.Prefix, error)
	Children(ctx context.Context, p netip.Prefix, limit int) ([]store.Prefix, error)
	ASN(ctx context.Context, asn int64) (*store.ASN, error)
	ASNBlocks(ctx context.Context, asn int64) ([]store.ASNBlock, error)
	ASNsByNumber(ctx context.Context, asns []int64) ([]store.ASNBrief, error)
	ASNsByHolder(ctx context.Context, holderID int64, limit int) ([]store.ASNBrief, error)
	PrefixesForASN(ctx context.Context, asn int64, holderID *int64, limit int) ([]store.LinkedPrefix, int, error)
	Holder(ctx context.Context, rir, opaqueID string) (*store.Holder, error)
	HolderPrefixes(ctx context.Context, holderID int64, after *netip.Prefix, limit int) ([]store.Prefix, error)
	ListPrefixes(ctx context.Context, f store.ListFilter, after *netip.Prefix, limit int, fn func(store.ListPrefix) error) error
	ListASNs(ctx context.Context, f store.ListFilter, after int64, limit int, fn func(store.ASNBrief) error) error
	History(ctx context.Context, entity, key string, limit int) ([]store.Change, error)
	Sources(ctx context.Context) ([]store.SourceStatus, error)
}

// DatasetView devolve a versão atual do central.
type DatasetView interface {
	Current() dataset.Snapshot
}

// Config são as opções da API.
type Config struct {
	Version     string
	CORSOrigin  string
	DBTimeout   time.Duration
	ListTimeout time.Duration
	AccessLog   bool
	PublicURL   string
}

// API reúne as dependências das rotas.
type API struct {
	store  Store
	data   DatasetView
	cache  cache.Cache
	realip *realip.Resolver
	log    *slog.Logger
	cfg    Config
	sf     singleflight.Group
}

// New cria a API.
func New(st Store, data DatasetView, c cache.Cache, rip *realip.Resolver, log *slog.Logger, cfg Config) *API {
	if cfg.DBTimeout <= 0 {
		cfg.DBTimeout = 5 * time.Second
	}
	if cfg.ListTimeout <= 0 {
		cfg.ListTimeout = 60 * time.Second
	}
	if cfg.CORSOrigin == "" {
		cfg.CORSOrigin = "*"
	}
	return &API{store: st, data: data, cache: c, realip: rip, log: log, cfg: cfg}
}

// Handler monta todas as rotas com os middlewares.
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /v1/ip", a.handleMyIP)
	mux.HandleFunc("GET /v1/ip/{ip}", a.handleIP)
	mux.HandleFunc("GET /v1/asn/{asn}", a.handleASN)
	mux.HandleFunc("GET /v1/asn/{asn}/history", a.handleASNHistory)
	mux.HandleFunc("GET /v1/prefix/{ip}/{len}", a.handlePrefix)
	mux.HandleFunc("GET /v1/prefix/{ip}/{len}/history", a.handlePrefixHistory)
	mux.HandleFunc("GET /v1/holder/{rir}/{id}", a.handleHolder)
	mux.HandleFunc("GET /v1/holder/{rir}/{id}/history", a.handleHolderHistory)
	mux.HandleFunc("GET /v1/country/{cc}/prefixes", a.handleCountryPrefixes)
	mux.HandleFunc("GET /v1/country/{cc}/asns", a.handleCountryASNs)
	mux.HandleFunc("GET /v1/rir/{rir}/prefixes", a.handleRIRPrefixes)
	mux.HandleFunc("GET /v1/rir/{rir}/asns", a.handleRIRASNs)
	mux.HandleFunc("GET /v1/meta/sources", a.handleSources)

	mux.HandleFunc("GET /asn/{asn}", a.handleLegacyASN)

	for _, p := range []string{"/health", "/status"} {
		mux.HandleFunc("GET "+p, a.handleStatus)
		mux.HandleFunc("POST "+p, a.handleStatus)
	}
	mux.HandleFunc("GET /ping", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte("pong"))
	})
	mux.HandleFunc("GET /openapi.yaml", a.handleOpenAPI)
	mux.HandleFunc("GET /docs", a.handleDocs)
	mux.HandleFunc("GET /{$}", a.handleIndex)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "rota inexistente; veja /docs")
	})

	return a.middleware(mux)
}

type ctxKey int

const clientKey ctxKey = 1

type clientInfo struct {
	ip     netip.Addr
	source realip.Source
}

func clientFrom(ctx context.Context) clientInfo {
	c, _ := ctx.Value(clientKey).(clientInfo)
	return c
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

// middleware resolve o IP real, aplica cabeçalhos comuns, CORS e recover, e
// registra o acesso.
func (a *API) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ip, src := a.realip.ClientIP(r)
		sw := &statusWriter{ResponseWriter: w}

		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Access-Control-Allow-Origin", a.cfg.CORSOrigin)
		h.Set("Access-Control-Expose-Headers", "ETag, X-Cache, X-Dataset-Version")
		if a.cfg.Version != "" {
			h.Set("Server", "badblock-registry-api/"+a.cfg.Version)
		}

		defer func() {
			if rec := recover(); rec != nil {
				a.log.Error("panic", "path", r.URL.Path, "panic", fmt.Sprint(rec))
				if sw.status == 0 {
					writeError(sw, http.StatusInternalServerError, "internal_error", "erro interno")
				}
			}
			if a.cfg.AccessLog {
				a.log.Info("http", "method", r.Method, "path", r.URL.Path, "query", r.URL.RawQuery,
					"status", sw.status, "bytes", sw.bytes, "ms", time.Since(start).Milliseconds(),
					"client_ip", ip.String(), "client_ip_source", string(src),
					"cache", sw.Header().Get("X-Cache"))
			}
		}()

		if r.Method == http.MethodOptions {
			h.Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
			h.Set("Access-Control-Allow-Headers", "If-None-Match, Content-Type")
			h.Set("Access-Control-Max-Age", "86400")
			sw.WriteHeader(http.StatusNoContent)
			return
		}
		ctx := context.WithValue(r.Context(), clientKey, clientInfo{ip: ip, source: src})
		next.ServeHTTP(sw, r.WithContext(ctx))
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

// errStatus traduz erros de consulta em respostas.
func (a *API) writeStoreError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
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

// payload é uma resposta pronta, possivelmente vinda do cache.
type payload struct {
	contentType string
	body        []byte
}

// encode/decode do valor guardado no cache: 1 byte de tipo e o corpo.
func (p payload) encode() []byte {
	kind := byte('j')
	if strings.HasPrefix(p.contentType, "text/plain") {
		kind = 't'
	}
	return append([]byte{kind}, p.body...)
}

func decodePayload(b []byte) (payload, bool) {
	if len(b) == 0 {
		return payload{}, false
	}
	switch b[0] {
	case 'j':
		return payload{contentType: "application/json; charset=utf-8", body: b[1:]}, true
	case 't':
		return payload{contentType: "text/plain; charset=utf-8", body: b[1:]}, true
	}
	return payload{}, false
}

// maxCachedBody evita guardar listas enormes no Valkey.
const maxCachedBody = 16 << 20

// serveCached responde uma URL cujo conteúdo depende só dela e da versão do
// dataset: confere o ETag, tenta o cache, calcula uma vez por chave
// (singleflight) e guarda o resultado.
func (a *API) serveCached(w http.ResponseWriter, r *http.Request, snap dataset.Snapshot, key string,
	build func(ctx context.Context) (payload, error)) {
	etag := etagFor(snap.Version, key)
	if match(r.Header.Get("If-None-Match"), etag) {
		setDataHeaders(w, snap, etag, "HIT")
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if raw, ok := a.cache.Get(r.Context(), key); ok {
		if p, ok := decodePayload(raw); ok {
			writePayload(w, snap, etag, "HIT", p)
			return
		}
	}
	v, err, _ := a.sf.Do(key, func() (any, error) {
		p, err := build(context.WithoutCancel(r.Context()))
		if err != nil {
			return nil, err
		}
		if len(p.body) <= maxCachedBody {
			a.cache.Set(context.WithoutCancel(r.Context()), key, p.encode())
		}
		return p, nil
	})
	if err != nil {
		var he *httpError
		if errors.As(err, &he) {
			if he.raw != nil {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(he.status)
				_, _ = w.Write(he.raw)
				return
			}
			writeError(w, he.status, he.code, he.msg)
			return
		}
		a.writeStoreError(w, r, err)
		return
	}
	writePayload(w, snap, etag, a.cacheLabel(), v.(payload))
}

func (a *API) cacheLabel() string {
	if a.cache.Enabled() {
		return "MISS"
	}
	return "BYPASS"
}

// httpError é um erro de validação devolvido de dentro de build. Com raw, o
// corpo é enviado como está (a rota legado responde 404 com {}).
type httpError struct {
	status    int
	code, msg string
	raw       []byte
}

func (e *httpError) Error() string { return e.msg }

func badRequest(code, msg string) error {
	return &httpError{status: http.StatusBadRequest, code: code, msg: msg}
}

func notFound(msg string) error {
	return &httpError{status: http.StatusNotFound, code: "not_found", msg: msg}
}

func writePayload(w http.ResponseWriter, snap dataset.Snapshot, etag, cacheState string, p payload) {
	setDataHeaders(w, snap, etag, cacheState)
	w.Header().Set("Content-Type", p.contentType)
	_, _ = w.Write(p.body)
}

func setDataHeaders(w http.ResponseWriter, snap dataset.Snapshot, etag, cacheState string) {
	h := w.Header()
	h.Set("ETag", etag)
	h.Set("Cache-Control", "public, max-age=300")
	h.Set("X-Cache", cacheState)
	h.Set("X-Dataset-Version", fmt.Sprint(snap.Version))
}

func jsonPayload(v any) (payload, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return payload{}, err
	}
	return payload{contentType: "application/json; charset=utf-8", body: append(b, '\n')}, nil
}

// etagFor é fraco (W/) porque o mesmo conteúdo pode ser serializado de novo.
func etagFor(version int64, key string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	return fmt.Sprintf(`W/"%d-%x"`, version, h.Sum64())
}

func match(ifNoneMatch, etag string) bool {
	if ifNoneMatch == "" {
		return false
	}
	for _, v := range strings.Split(ifNoneMatch, ",") {
		v = strings.TrimSpace(v)
		if v == "*" || v == etag || strings.TrimPrefix(v, "W/") == strings.TrimPrefix(etag, "W/") {
			return true
		}
	}
	return false
}

// ready confere se já existe dataset; se não, responde 503.
func (a *API) ready(w http.ResponseWriter) (dataset.Snapshot, bool) {
	snap := a.data.Current()
	if !snap.Ready() {
		writeError(w, http.StatusServiceUnavailable, "dataset_not_ready",
			"a primeira sincronização ainda não terminou; tente em alguns minutos")
		return snap, false
	}
	return snap, true
}

func datasetInfo(snap dataset.Snapshot) DatasetInfo {
	return DatasetInfo{Version: snap.Version, UpdatedAt: tsStr(snap.BuiltAt)}
}

func (a *API) dbCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, a.cfg.DBTimeout)
}
