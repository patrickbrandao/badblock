// Package httpapi implementa as rotas HTTP da api-registrobr-rdapnum.
//
// A API é dona de tudo abaixo do caminho de base (BASE_PATH, padrão
// /registrobr/rdapnum): /registrobr/rdapnum/asn/61610 é a versão atual e
// /registrobr/rdapnum/v1/asn/61610 fixa a v1. Uma v2 futura ganha
// /registrobr/rdapnum/v2/... e passa a ser a versão sem prefixo.
//
// Como no rdns, não há versão de dataset (decisão #42): a chave de cache não
// leva versão, o ETag é o hash do corpo e o bloco scan substitui o dataset. O
// POST .../refresh só põe o pedido na fila do Valkey e responde 202.
package httpapi

import (
	"bytes"
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

	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/api/internal/cache"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/api/internal/realip"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/api/internal/refresh"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/api/internal/store"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/api/openapi"
)

// Store são as consultas que a API usa (implementadas por store.Store).
type Store interface {
	Ping(ctx context.Context) error
	TargetASN(ctx context.Context, asn int64) (*store.Target, error)
	TargetPrefix(ctx context.Context, p netip.Prefix) (*store.Target, error)
	ASN(ctx context.Context, asn int64) (*store.ASNRow, error)
	Network(ctx context.Context, p netip.Prefix) (*store.NetworkRow, error)
	Document(ctx context.Context, digits string) (*store.DocumentRow, error)
	Contact(ctx context.Context, handle string) (*store.ContactRow, error)
	Response(ctx context.Context, uuid string) (*store.Response, error)
	History(ctx context.Context, targetUUID string) ([]store.ResponseMeta, error)
	Config(ctx context.Context) (map[string]string, error)
	TargetStats(ctx context.Context, interval time.Duration) (*store.TargetStats, error)
	ResponseStats(ctx context.Context) (*store.ResponseStats, error)
	DataStats(ctx context.Context) (*store.DataStats, error)
	Derivations(ctx context.Context) ([]store.Derivation, error)
	Workers(ctx context.Context) ([]store.Worker, error)
	Job(ctx context.Context) (*store.Job, error)
}

// DatasetView diz se já há alvos (dataset.Watcher).
type DatasetView interface {
	Current() dataset.Snapshot
}

// Refresher é o pedido de atualização (refresh.Requester); nil sem REDIS_URL.
type Refresher interface {
	Request(ctx context.Context, kind, target string, lastScanAt *time.Time) (refresh.Outcome, error)
	QueueLen(ctx context.Context) (int64, error)
	Ping(ctx context.Context) error
}

// Config são as opções da API.
type Config struct {
	BasePath   string // ex.: /registrobr/rdapnum
	Version    string
	CORSOrigin string
	DBTimeout  time.Duration
	AccessLog  bool
}

// CurrentAPIVersion é a versão servida nas rotas sem /vN.
const CurrentAPIVersion = "v1"

// keyPrefix é o prefixo das chaves de cache desta API (sem versão).
const keyPrefix = "badblock:api-registrobr-rdapnum:"

// Cache-Control das rotas de dados e de /response/{uuid} (imutável).
const (
	cachePublic    = "public, max-age=300"
	cacheImmutable = "public, max-age=86400"
)

// API reúne as dependências das rotas.
type API struct {
	store   Store
	data    DatasetView
	cache   cache.Cache
	refresh Refresher
	realip  *realip.Resolver
	log     *slog.Logger
	cfg     Config
	sf      singleflight.Group
	routes  []string // padrões registrados em Handler (o teste do manifesto confere)
}

// New cria a API. rf nil desliga a atualização (503 refresh_unavailable).
func New(st Store, data DatasetView, c cache.Cache, rf Refresher, rip *realip.Resolver, log *slog.Logger, cfg Config) *API {
	if cfg.DBTimeout <= 0 {
		cfg.DBTimeout = 5 * time.Second
	}
	if cfg.CORSOrigin == "" {
		cfg.CORSOrigin = "*"
	}
	cfg.BasePath = "/" + strings.Trim(cfg.BasePath, "/")
	return &API{store: st, data: data, cache: c, refresh: rf, realip: rip, log: log, cfg: cfg}
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
		p := b + v
		handle("GET "+p+"/{$}", a.handleIndex)
		handle("GET "+p+"/asn/{asn}", a.handleASN)
		handle("GET "+p+"/asn/{asn}/raw", a.handleASNRaw)
		handle("GET "+p+"/asn/{asn}/history", a.handleASNHistory)
		handle("POST "+p+"/asn/{asn}/refresh", a.handleASNRefresh)
		handle("GET "+p+"/ip/{ip}", a.handleIP)
		handle("GET "+p+"/prefix/{ip}/{len}", a.handlePrefix)
		handle("GET "+p+"/prefix/{ip}/{len}/raw", a.handlePrefixRaw)
		handle("GET "+p+"/prefix/{ip}/{len}/history", a.handlePrefixHistory)
		handle("POST "+p+"/prefix/{ip}/{len}/refresh", a.handlePrefixRefresh)
		handle("GET "+p+"/document/{document...}", a.handleDocument)
		handle("GET "+p+"/contact/{handle}", a.handleContact)
		handle("GET "+p+"/response/{uuid}", a.handleResponse)
		handle("GET "+p+"/meta", a.handleMeta)
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
		h.Set("Access-Control-Expose-Headers", "ETag, X-Cache, X-Response-UUID")
		if a.cfg.Version != "" {
			h.Set("Server", "badblock-api-registrobr-rdapnum/"+a.cfg.Version)
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
			h.Set("Access-Control-Allow-Methods", "GET, HEAD, POST, OPTIONS")
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
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Del("ETag")
	h.Del("X-Response-UUID")
	h.Del("Last-Modified")
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

// writeNotReady é o 503 antes do primeiro alvo.
func writeNotReady(w http.ResponseWriter) {
	writeError(w, http.StatusServiceUnavailable, "dataset_not_ready",
		"nenhum alvo cadastrado ainda: a primeira derivação do collector-registrobr-rdapnum não terminou; tente em alguns minutos")
}

// maxCachedBody evita guardar respostas enormes no Valkey.
const maxCachedBody = 8 << 20

// entry é uma resposta pronta: o corpo e, nas rotas da resposta crua, o tipo
// e os cabeçalhos que vão junto (gravados no cache com o corpo).
type entry struct {
	body         []byte
	contentType  string // "" = JSON
	responseUUID string // X-Response-UUID
	lastModified string // Last-Modified (formato HTTP)
}

// rawEntry marca, no Valkey, uma entrada com cabeçalhos: o corpo JSON das
// outras rotas nunca começa com 0x00.
const rawEntry = "\x00entry\n"

func (e *entry) encode() []byte {
	if e.contentType == "" && e.responseUUID == "" && e.lastModified == "" {
		return e.body
	}
	var b bytes.Buffer
	b.WriteString(rawEntry)
	b.WriteString(e.contentType + "\n" + e.responseUUID + "\n" + e.lastModified + "\n")
	b.Write(e.body)
	return b.Bytes()
}

func decodeEntry(v []byte) *entry {
	rest, ok := bytes.CutPrefix(v, []byte(rawEntry))
	if !ok {
		return &entry{body: v}
	}
	parts := bytes.SplitN(rest, []byte("\n"), 4)
	if len(parts) != 4 {
		return nil
	}
	return &entry{contentType: string(parts[0]), responseUUID: string(parts[1]), lastModified: string(parts[2]), body: parts[3]}
}

// render serializa o corpo de uma resposta de dados (JSON compacto e uma
// quebra de linha no fim, como o json.Encoder).
func render(obj any) (*entry, error) {
	if e, ok := obj.(*entry); ok {
		return e, nil
	}
	body, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return &entry{body: append(body, '\n')}, nil
}

// fetch devolve uma resposta pronta: do cache ou, uma vez por chave
// (singleflight), do banco, com prazo DB_TIMEOUT independente do cliente que
// chegou primeiro. O segundo valor é o X-Cache (HIT, MISS ou BYPASS).
func (a *API) fetch(r *http.Request, key string, build func(ctx context.Context) (any, error)) (*entry, string, error) {
	if v, ok := a.cache.Get(r.Context(), keyPrefix+key); ok {
		if e := decodeEntry(v); e != nil {
			return e, "HIT", nil
		}
	}
	v, err, _ := a.sf.Do(key, func() (any, error) {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), a.cfg.DBTimeout)
		defer cancel()
		obj, err := build(ctx)
		if err != nil {
			return nil, err
		}
		e, err := render(obj)
		if err != nil {
			return nil, err
		}
		if enc := e.encode(); len(enc) <= maxCachedBody {
			a.cache.Set(context.WithoutCancel(r.Context()), keyPrefix+key, enc)
		}
		return e, nil
	})
	if err != nil {
		return nil, "", err
	}
	state := "BYPASS"
	if a.cache.Enabled() {
		state = "MISS"
	}
	return v.(*entry), state, nil
}

// serveCached responde uma rota de dados: sem alvos, 503; o corpo vem do
// cache ou do banco (fetch); o ETag é o hash do corpo, e If-None-Match igual
// responde 304.
func (a *API) serveCached(w http.ResponseWriter, r *http.Request, key, cacheControl string, build func(ctx context.Context) (any, error)) {
	if !a.data.Current().Ready() {
		writeNotReady(w)
		return
	}
	e, state, err := a.fetch(r, key, build)
	if err != nil {
		a.writeFailure(w, r, err)
		return
	}
	etag := etagFor(e.body)
	h := w.Header()
	h.Set("ETag", etag)
	h.Set("Cache-Control", cacheControl)
	h.Set("X-Cache", state)
	if e.responseUUID != "" {
		h.Set("X-Response-UUID", e.responseUUID)
	}
	if e.lastModified != "" {
		h.Set("Last-Modified", e.lastModified)
	}
	if match(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	ct := e.contentType
	if ct == "" {
		ct = "application/json; charset=utf-8"
	}
	h.Set("Content-Type", ct)
	_, _ = w.Write(e.body)
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
