package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/patrickbrandao/badblock/apps/asnames/api/internal/cache"
	"github.com/patrickbrandao/badblock/apps/asnames/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/asnames/api/internal/realip"
	"github.com/patrickbrandao/badblock/apps/asnames/api/internal/store"
)

// sample é um recorte real do asn.txt, já com os campos derivados pelo
// collector-asnames.
var sample = []store.ASN{
	{ASN: 513, Description: "CERN CERN - European Organization for Nuclear Research, CH", Handle: new("CERN"), Name: new("CERN - European Organization for Nuclear Research"), Country: new("CH")},
	{ASN: 1297, Description: "CERN1297 CERN - European Organization for Nuclear Research, CH", Handle: new("CERN1297"), Name: new("CERN - European Organization for Nuclear Research"), Country: new("CH")},
	{ASN: 4745, Description: "AS4745-138 - , KR", Handle: new("AS4745-138"), Country: new("KR")},
	{ASN: 7901, Description: "- , NZ", Country: new("NZ")},
	{ASN: 15169, Description: "GOOGLE - Google LLC, US", Handle: new("GOOGLE"), Name: new("Google LLC"), Country: new("US")},
	{ASN: 16509, Description: "AMAZON-02 - Amazon.com, Inc., US", Handle: new("AMAZON-02"), Name: new("Amazon.com, Inc."), Country: new("US")},
	{ASN: 29571, Description: "Orange Côte d'Ivoire - Orange Côte d'Ivoire, CI", Handle: new("Orange Côte d'Ivoire"), Name: new("Orange Côte d'Ivoire"), Country: new("CI")},
	{ASN: 327710, Description: "Orange Côte d'Ivoire - Orange Côte d'Ivoire, CI", Handle: new("Orange Côte d'Ivoire"), Name: new("Orange Côte d'Ivoire"), Country: new("CI")},
}

type fakeStore struct {
	mu       sync.Mutex
	calls    int
	pingErr  error
	err      error  // devolvido pelas consultas de dados
	panicMsg string // faz as consultas de dados entrarem em pânico
	noData   bool   // Dataset e Job devolvem nil (antes da primeira carga)
	lastTerm string
	lastLim  int
	bigName  bool // ByCountry devolve um nome de 9 MB (acima do limite do cache)
}

func (f *fakeStore) count() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.panicMsg != "" {
		panic(f.panicMsg)
	}
	return f.err
}

func (f *fakeStore) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeStore) Ping(context.Context) error { return f.pingErr }

func (f *fakeStore) Dataset(context.Context) (*store.Dataset, error) {
	if f.noData {
		return nil, nil
	}
	return &store.Dataset{Version: "v1", AppliedAt: time.Unix(0, 0), URL: SourceURL, SHA256: "ab", ASNs: len(sample)}, nil
}

func (f *fakeStore) Job(context.Context) (*store.Job, error) {
	if f.noData {
		return nil, nil
	}
	t := time.Unix(10, 0)
	return &store.Job{LastSyncAt: &t, LastCheckAt: &t, Consolidated: 0}, nil
}

func (f *fakeStore) ASN(_ context.Context, asn int64) (*store.ASN, error) {
	if err := f.count(); err != nil {
		return nil, err
	}
	for _, a := range sample {
		if a.ASN == asn {
			return &a, nil
		}
	}
	return nil, store.ErrNotFound
}

func (f *fakeStore) ByCountry(_ context.Context, cc string) ([]store.Brief, error) {
	if err := f.count(); err != nil {
		return nil, err
	}
	if f.bigName {
		return []store.Brief{{ASN: 1, Name: new(strings.Repeat("x", 9<<20))}}, nil
	}
	var out []store.Brief
	for _, a := range sample {
		if a.Country != nil && *a.Country == cc {
			out = append(out, store.Brief{ASN: a.ASN, Handle: a.Handle, Name: a.Name})
		}
	}
	return out, nil
}

func (f *fakeStore) ByHandle(_ context.Context, h string) ([]store.Entry, error) {
	if err := f.count(); err != nil {
		return nil, err
	}
	var out []store.Entry
	for _, a := range sample {
		if a.Handle != nil && strings.EqualFold(*a.Handle, h) {
			out = append(out, entry(a))
		}
	}
	return out, nil
}

// Search imita o ILIKE com termo literal. "bulk" devolve 150 resultados e
// "cent" exatamente 100, para testar o corte.
func (f *fakeStore) Search(_ context.Context, term string, limit int) ([]store.Entry, error) {
	if err := f.count(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.lastTerm, f.lastLim = term, limit
	f.mu.Unlock()
	synth := map[string]int{"bulk": 150, "cent": 100}
	if n, ok := synth[term]; ok {
		var out []store.Entry
		for i := range min(n, limit) {
			out = append(out, store.Entry{ASN: int64(i + 1), Description: term})
		}
		return out, nil
	}
	var out []store.Entry
	for _, a := range sample {
		if strings.Contains(strings.ToLower(a.Description), term) && len(out) < limit {
			out = append(out, entry(a))
		}
	}
	return out, nil
}

func entry(a store.ASN) store.Entry {
	return store.Entry{ASN: a.ASN, Handle: a.Handle, Name: a.Name, Country: a.Country, Description: a.Description}
}

type fixedView struct{ snap dataset.Snapshot }

func (v fixedView) Current() dataset.Snapshot { return v.snap }

// memCache é um cache em memória para os testes.
type memCache struct {
	mu      sync.Mutex
	m       map[string][]byte
	pingErr error
}

func (c *memCache) Get(_ context.Context, k string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[k]
	return v, ok
}

func (c *memCache) Set(_ context.Context, k string, v []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[k] = v
}

func (c *memCache) Ping(context.Context) error { return c.pingErr }
func (c *memCache) Enabled() bool              { return true }

func (c *memCache) keys() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for k := range c.m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func newAPIWith(t *testing.T, st *fakeStore, ready bool, c cache.Cache) http.Handler {
	t.Helper()
	rip, err := realip.New(realip.DefaultTrustedProxies, realip.DefaultHeaders)
	if err != nil {
		t.Fatal(err)
	}
	snap := dataset.Snapshot{}
	if ready {
		snap = dataset.Snapshot{Version: "0192-v1", AppliedAt: time.Unix(0, 0)}
	}
	a := New(st, fixedView{snap}, c, rip, slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config{BasePath: "/asnames/", Version: "test", DBTimeout: time.Second})
	return a.Handler()
}

func newAPI(t *testing.T, st *fakeStore, ready bool) (http.Handler, *memCache) {
	c := &memCache{m: map[string][]byte{}}
	return newAPIWith(t, st, ready, c), c
}

func do(h http.Handler, method, path string, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("JSON inválido (%d): %v: %s", rec.Code, err, rec.Body)
	}
	return v
}

func TestASN(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, true)
	for _, p := range []string{"/asnames/asn/15169", "/asnames/v1/asn/15169", "/asnames/asn/AS15169", "/asnames/asn/as15169"} {
		rec := do(h, "GET", p)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", p, rec.Code, rec.Body)
		}
		got := decode[ASNResponse](t, rec)
		if got.ASN != 15169 || *got.Handle != "GOOGLE" || *got.Name != "Google LLC" || *got.Country != "US" ||
			got.Description != "GOOGLE - Google LLC, US" || got.FirstSeen == "" || got.UpdatedAt == "" {
			t.Errorf("%s: %+v", p, got)
		}
		if got.Dataset.Version != "0192-v1" || got.Dataset.UpdatedAt != "1970-01-01T00:00:00Z" {
			t.Errorf("dataset = %+v", got.Dataset)
		}
	}
	// Campos ausentes na fonte saem como null, não como "".
	rec := do(h, "GET", "/asnames/asn/7901")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"handle":null,"name":null,"country":"NZ"`) {
		t.Errorf("AS7901 = %d %s", rec.Code, rec.Body)
	}
}

func TestCountry(t *testing.T) {
	st := &fakeStore{}
	h, c := newAPI(t, st, true)
	for _, p := range []string{"/asnames/country/us", "/asnames/country/US", "/asnames/v1/country/Us"} {
		rec := do(h, "GET", p)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", p, rec.Code, rec.Body)
		}
		got := decode[CountryResponse](t, rec)
		if got.Country != "US" || got.Count != 2 || len(got.ASNs) != 2 || got.ASNs[0].ASN != 15169 ||
			*got.ASNs[1].Handle != "AMAZON-02" || got.Dataset.Version != "0192-v1" {
			t.Errorf("%s: %+v", p, got)
		}
	}
	// Maiúsculas e /v1 caem na mesma chave: uma consulta só ao banco.
	if st.Calls() != 1 || !slices.Equal(c.keys(), []string{"badblock:api-asnames:0192-v1:country:US"}) {
		t.Errorf("calls = %d, chaves = %v", st.Calls(), c.keys())
	}
	// Formato da lista: só asn, handle e nome (null quando ausente).
	rec := do(h, "GET", "/asnames/country/nz")
	if !strings.Contains(rec.Body.String(), `"asns":[{"asn":7901,"handle":null,"name":null}]`) {
		t.Errorf("NZ = %s", rec.Body)
	}
}

func TestHandle(t *testing.T) {
	st := &fakeStore{}
	h, c := newAPI(t, st, true)
	for _, p := range []string{"/asnames/handle/google", "/asnames/handle/GOOGLE", "/asnames/v1/handle/Google"} {
		rec := do(h, "GET", p)
		got := decode[HandleResponse](t, rec)
		if rec.Code != 200 || got.Handle != "google" || got.Count != 1 || got.ASNs[0].ASN != 15169 ||
			*got.ASNs[0].Country != "US" || got.ASNs[0].Description == "" {
			t.Errorf("%s: %d %s", p, rec.Code, rec.Body)
		}
	}
	if st.Calls() != 1 || !slices.Equal(c.keys(), []string{"badblock:api-asnames:0192-v1:handle:google"}) {
		t.Errorf("calls = %d, chaves = %v", st.Calls(), c.keys())
	}
	// Handle repetido (AFRINIC "X - X"), com espaço e não-ASCII.
	rec := do(h, "GET", "/asnames/handle/orange%20c%C3%B4te%20d'ivoire")
	got := decode[HandleResponse](t, rec)
	if rec.Code != 200 || got.Count != 2 || got.ASNs[0].ASN != 29571 || got.ASNs[1].ASN != 327710 ||
		got.Handle != "orange côte d'ivoire" {
		t.Errorf("handle repetido = %d %s", rec.Code, rec.Body)
	}
}

func TestSearch(t *testing.T) {
	st := &fakeStore{}
	h, c := newAPI(t, st, true)

	// Normalização: pontas, espaços internos e maiúsculas.
	rec := do(h, "GET", "/asnames/search?q=%20%20Google%20%20%20LLC%20")
	got := decode[SearchResponse](t, rec)
	if rec.Code != 200 || got.Query != "google llc" || got.Count != 1 || got.Truncated || got.ASNs[0].ASN != 15169 {
		t.Fatalf("busca = %d %s", rec.Code, rec.Body)
	}
	if st.lastTerm != "google llc" || st.lastLim != SearchLimit+1 {
		t.Errorf("store recebeu %q limite %d", st.lastTerm, st.lastLim)
	}
	rec = do(h, "GET", "/asnames/v1/search?q=GOOGLE+llc")
	if rec.Header().Get("X-Cache") != "HIT" || st.Calls() != 1 {
		t.Errorf("mesma busca normalizada deveria ser HIT (calls = %d)", st.Calls())
	}
	if !slices.Equal(c.keys(), []string{"badblock:api-asnames:0192-v1:search:google llc"}) {
		t.Errorf("chaves = %v", c.keys())
	}

	// Várias linhas, em ordem de ASN.
	got = decode[SearchResponse](t, do(h, "GET", "/asnames/search?q=cern"))
	if got.Count != 2 || got.ASNs[0].ASN != 513 || got.ASNs[1].ASN != 1297 {
		t.Errorf("cern = %+v", got)
	}

	// Curingas do LIKE chegam ao store como texto literal (o store escapa).
	for q, want := range map[string]string{"50%25_x": "50%_x", `a\b_c`: `a\b_c`, "100%25": "100%"} {
		rec := do(h, "GET", "/asnames/search?q="+strings.ReplaceAll(q, `\`, "%5C"))
		if rec.Code != 200 || st.lastTerm != want {
			t.Errorf("q=%s: %d, store recebeu %q, quero %q", q, rec.Code, st.lastTerm, want)
		}
		if res := decode[SearchResponse](t, rec); res.Count != 0 || res.ASNs == nil {
			t.Errorf("q=%s: sem resultado deveria ser lista vazia: %s", q, rec.Body)
		}
	}

	// Corte em 100.
	got = decode[SearchResponse](t, do(h, "GET", "/asnames/search?q=bulk"))
	if got.Count != SearchLimit || len(got.ASNs) != SearchLimit || !got.Truncated {
		t.Errorf("bulk: count %d, truncated %v", got.Count, got.Truncated)
	}
	got = decode[SearchResponse](t, do(h, "GET", "/asnames/search?q=cent"))
	if got.Count != SearchLimit || got.Truncated {
		t.Errorf("exatamente 100: count %d, truncated %v", got.Count, got.Truncated)
	}

	// Limites de tamanho, contados em caracteres (não bytes), depois de normalizar.
	ok := map[string]bool{
		"":                                    false,
		"q=":                                  false,
		"q=ab":                                false,
		"q=%20ab%20%20":                       false,
		"q=a%20b":                             true,
		"q=abc":                               true,
		"q=%C3%A7%C3%B5%C3%A9":                true, // "çõé": 3 caracteres, 6 bytes
		"q=" + strings.Repeat("a", 100):       true,
		"q=" + strings.Repeat("a", 101):       false,
		"q=" + strings.Repeat("a%20%20", 50):  true, // 50 "a" + 49 espaços depois de colapsar
		"q=abc%00":                            false,
		"q=ab%FF":                             false,
		"q=abc%09def":                         true, // tab vira espaço
		"q=" + strings.Repeat("%C3%A7", 101):  false,
		"x=google":                            false,
		"q=" + strings.Repeat("%C3%A7", 100):  true,
		"q=abc&q=" + strings.Repeat("x", 200): true, // vale o primeiro
	}
	for qs, want := range ok {
		rec := do(h, "GET", "/asnames/search?"+qs)
		if (rec.Code == 200) != want {
			t.Errorf("?%s: %d, quero sucesso=%v (%s)", qs, rec.Code, want, rec.Body)
		}
		if !want && (rec.Code != 400 || !strings.Contains(rec.Body.String(), "bad_request")) {
			t.Errorf("?%s: %d %s", qs, rec.Code, rec.Body)
		}
	}
}

func TestNormalizeQuery(t *testing.T) {
	for in, want := range map[string]string{
		"  Google   LLC ": "google llc",
		"AMAZON\t02":      "amazon 02",
		"Côte D'IVOIRE":   "côte d'ivoire",
		"%_\\":            "%_\\",
	} {
		if got, ok := normalizeQuery(in); !ok || got != want {
			t.Errorf("%q → %q %v, quero %q", in, got, ok, want)
		}
	}
}

func TestErrors(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, true)
	cases := map[string]int{
		"/asnames/asn/1":                                   404,
		"/asnames/asn/abc":                                 400,
		"/asnames/asn/AS":                                  400,
		"/asnames/asn/-1":                                  400,
		"/asnames/asn/4294967296":                          400,
		"/asnames/country/ZZ":                              404,
		"/asnames/country/USA":                             400,
		"/asnames/country/U":                               400,
		"/asnames/country/1A":                              400,
		"/asnames/country/%C3%A7a":                         400,
		"/asnames/handle/nada-disso":                       404,
		"/asnames/handle/%20%20":                           400,
		"/asnames/handle/ab%00c":                           400,
		"/asnames/handle/ab%FF":                            400,
		"/asnames/handle/" + strings.Repeat("x", 256):      400,
		"/asnames/search":                                  400,
		"/asnames/search?q=ab":                             400,
		"/asnames/v2/asn/15169":                            404,
		"/asnames/nada":                                    404,
		"/asnames/search/google":                           404,
		"/outra-coisa":                                     404,
		"/asn/15169":                                       404,
		"/asnames/handle/" + strings.Repeat("x", 255):      404,
		"/asnames/handle/" + strings.Repeat("%C3%A7", 255): 404,
	}
	for p, want := range cases {
		rec := do(h, "GET", p)
		if rec.Code != want {
			t.Errorf("%s: %d, quero %d (%s)", p, rec.Code, want, rec.Body)
		}
		if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") || rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: erro deveria ser JSON sem cache (%v)", p, rec.Header())
		}
		var e errorBody
		if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil || e.Error.Code == "" || e.Error.Message == "" {
			t.Errorf("%s: corpo de erro = %s", p, rec.Body)
		}
	}
	// Método não suportado numa rota existente.
	if rec := do(h, "DELETE", "/asnames/asn/15169"); rec.Code != http.StatusMethodNotAllowed && rec.Code != 404 {
		t.Errorf("DELETE = %d", rec.Code)
	}
}

func TestDatabaseErrors(t *testing.T) {
	st := &fakeStore{err: errors.New("conexão recusada")}
	h, c := newAPI(t, st, true)
	for _, p := range []string{"/asnames/asn/15169", "/asnames/country/US", "/asnames/handle/google", "/asnames/search?q=google"} {
		rec := do(h, "GET", p)
		if rec.Code != 503 || !strings.Contains(rec.Body.String(), "database_unavailable") {
			t.Errorf("%s: %d %s", p, rec.Code, rec.Body)
		}
	}
	st.err = fmt.Errorf("consulta: %w", context.DeadlineExceeded)
	for _, p := range []string{"/asnames/asn/15169", "/asnames/search?q=google"} {
		rec := do(h, "GET", p)
		if rec.Code != 504 || !strings.Contains(rec.Body.String(), `"timeout"`) {
			t.Errorf("%s: %d %s", p, rec.Code, rec.Body)
		}
	}
	// Erros não entram no cache: quando o banco volta, a resposta é calculada.
	st.err = nil
	if len(c.keys()) != 0 {
		t.Errorf("erro foi para o cache: %v", c.keys())
	}
	if rec := do(h, "GET", "/asnames/asn/15169"); rec.Code != 200 || rec.Header().Get("X-Cache") != "MISS" {
		t.Errorf("depois do erro = %d %s", rec.Code, rec.Header().Get("X-Cache"))
	}
	// 404 também não entra no cache.
	do(h, "GET", "/asnames/asn/1")
	if n := len(c.keys()); n != 1 {
		t.Errorf("chaves = %v", c.keys())
	}
}

func TestPanicIsJSON500(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{panicMsg: "bug"}, true)
	rec := do(h, "GET", "/asnames/asn/15169")
	if rec.Code != 500 || !strings.Contains(rec.Body.String(), "internal_error") {
		t.Errorf("panic = %d %s", rec.Code, rec.Body)
	}
}

func TestCacheAndETag(t *testing.T) {
	st := &fakeStore{}
	h, c := newAPI(t, st, true)

	first := do(h, "GET", "/asnames/asn/15169")
	if first.Header().Get("X-Cache") != "MISS" {
		t.Errorf("primeira = %s", first.Header().Get("X-Cache"))
	}
	// Versionada e sem versão compartilham o cache (mesmo conteúdo).
	second := do(h, "GET", "/asnames/v1/asn/AS15169")
	if second.Header().Get("X-Cache") != "HIT" || st.Calls() != 1 || second.Body.String() != first.Body.String() {
		t.Errorf("segunda = %s, calls = %d", second.Header().Get("X-Cache"), st.Calls())
	}
	if !slices.Equal(c.keys(), []string{"badblock:api-asnames:0192-v1:asn:15169"}) {
		t.Errorf("chaves = %v", c.keys())
	}
	etag := first.Header().Get("ETag")
	if !strings.HasPrefix(etag, `W/"`) || first.Header().Get("X-Dataset-Version") != "0192-v1" ||
		first.Header().Get("Cache-Control") != "public, max-age=300" || second.Header().Get("ETag") != etag {
		t.Fatalf("cabeçalhos = %v", first.Header())
	}
	for _, inm := range []string{etag, strings.TrimPrefix(etag, "W/"), `"outro", ` + etag, "*"} {
		rec := do(h, "GET", "/asnames/v1/asn/15169", "If-None-Match", inm)
		if rec.Code != http.StatusNotModified || rec.Body.Len() != 0 || rec.Header().Get("ETag") != etag {
			t.Errorf("If-None-Match %s = %d", inm, rec.Code)
		}
	}
	if rec := do(h, "GET", "/asnames/asn/15169", "If-None-Match", `W/"outro"`); rec.Code != 200 {
		t.Errorf("ETag diferente = %d", rec.Code)
	}
	if st.Calls() != 1 {
		t.Errorf("304 não deveria consultar o banco (calls = %d)", st.Calls())
	}
	// Cada consulta tem o próprio ETag.
	if do(h, "GET", "/asnames/asn/16509").Header().Get("ETag") == etag {
		t.Error("ETag deveria depender da consulta")
	}
	// ETag das rotas de lista também vale entre /v1 e sem versão.
	a := do(h, "GET", "/asnames/search?q=cern").Header().Get("ETag")
	b := do(h, "GET", "/asnames/v1/search?q=CERN").Header().Get("ETag")
	if a == "" || a != b {
		t.Errorf("ETag da busca: %q != %q", a, b)
	}
}

func TestHEAD(t *testing.T) {
	st := &fakeStore{}
	h, _ := newAPI(t, st, true)
	for _, p := range []string{"/asnames/asn/15169", "/asnames/country/us", "/asnames/handle/google", "/asnames/search?q=google", "/asnames/", "/asnames/meta"} {
		rec := do(h, "HEAD", p)
		if rec.Code != 200 {
			t.Errorf("HEAD %s = %d", p, rec.Code)
		}
	}
	rec := do(h, "HEAD", "/asnames/asn/15169")
	etag := rec.Header().Get("ETag")
	if etag == "" || rec.Header().Get("X-Cache") != "HIT" {
		t.Errorf("HEAD sem cabeçalhos de dados: %v", rec.Header())
	}
	if rec := do(h, "HEAD", "/asnames/asn/15169", "If-None-Match", etag); rec.Code != http.StatusNotModified {
		t.Errorf("HEAD com If-None-Match = %d", rec.Code)
	}
	if rec := do(h, "HEAD", "/asnames/asn/1"); rec.Code != 404 {
		t.Errorf("HEAD inexistente = %d", rec.Code)
	}
}

func TestBypassAndBigBody(t *testing.T) {
	st := &fakeStore{}
	h := newAPIWith(t, st, true, cache.Noop{})
	if rec := do(h, "GET", "/asnames/asn/15169"); rec.Header().Get("X-Cache") != "BYPASS" {
		t.Errorf("sem cache = %s", rec.Header().Get("X-Cache"))
	}
	// Respostas acima de 8 MB não vão para o cache.
	h, c := newAPI(t, &fakeStore{bigName: true}, true)
	rec := do(h, "GET", "/asnames/country/US")
	if rec.Code != 200 || rec.Body.Len() <= maxCachedBody || len(c.keys()) != 0 {
		t.Errorf("corpo grande: %d, %d bytes, chaves %v", rec.Code, rec.Body.Len(), c.keys())
	}
}

func TestNotReady(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{noData: true}, false)
	for _, p := range []string{"/asnames/asn/15169", "/asnames/country/US", "/asnames/handle/google", "/asnames/search?q=google", "/asnames/v1/asn/15169"} {
		rec := do(h, "GET", p)
		if rec.Code != 503 || !strings.Contains(rec.Body.String(), "dataset_not_ready") {
			t.Errorf("%s sem dataset = %d %s", p, rec.Code, rec.Body)
		}
	}
	// Validação vem antes da conferência dos dados.
	if rec := do(h, "GET", "/asnames/asn/abc"); rec.Code != 400 {
		t.Errorf("inválido sem dataset = %d", rec.Code)
	}
	rec := do(h, "GET", "/asnames/status")
	s := decode[StatusResponse](t, rec)
	if rec.Code != 200 || s.Status != "starting" || !s.Success || s.Checks["dataset"] != "empty" {
		t.Errorf("status sem dataset = %d %+v", rec.Code, s)
	}
	rec = do(h, "GET", "/asnames/meta")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"dataset":null,"collector":null`) {
		t.Errorf("meta sem dados = %d %s", rec.Code, rec.Body)
	}
}

func TestStatus(t *testing.T) {
	st := &fakeStore{}
	h, c := newAPI(t, st, true)
	for _, m := range []string{"GET", "POST"} {
		for _, p := range []string{"/asnames/health", "/asnames/status"} {
			rec := do(h, m, p)
			s := decode[StatusResponse](t, rec)
			if rec.Code != 200 || !s.Success || s.Status != "ok" || s.Timestamp == "" || s.Message == "" ||
				s.Checks["postgres"] != "ok" || s.Checks["valkey"] != "ok" || s.Checks["dataset"] != "ok" {
				t.Errorf("%s %s = %d %s", m, p, rec.Code, rec.Body)
			}
			if rec.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("%s %s sem no-store", m, p)
			}
		}
	}
	if rec := do(h, "GET", "/asnames/ping"); rec.Code != 200 || rec.Body.String() != "pong" {
		t.Errorf("ping = %d %q", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", "/asnames/v1/status"); rec.Code != 404 {
		t.Errorf("status fica fora do versionamento: %d", rec.Code)
	}

	c.pingErr = errors.New("valkey fora")
	rec := do(h, "GET", "/asnames/status")
	s := decode[StatusResponse](t, rec)
	if rec.Code != 200 || !s.Success || s.Status != "degraded" || s.Checks["valkey"] != "error" {
		t.Errorf("valkey fora = %d %s", rec.Code, rec.Body)
	}

	st.pingErr = errors.New("down")
	rec = do(h, "POST", "/asnames/health")
	s = decode[StatusResponse](t, rec)
	if rec.Code != 503 || s.Success || s.Status != "error" || s.Checks["postgres"] != "error" {
		t.Errorf("postgres fora = %d %s", rec.Code, rec.Body)
	}

	// Cache desligado aparece como disabled.
	h = newAPIWith(t, &fakeStore{}, true, cache.Noop{})
	s = decode[StatusResponse](t, do(h, "GET", "/asnames/status"))
	if s.Status != "ok" || s.Checks["valkey"] != "disabled" {
		t.Errorf("sem cache = %+v", s)
	}
}

func TestCORSAndHeaders(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, true)
	rec := do(h, "OPTIONS", "/asnames/asn/15169", "Origin", "https://exemplo.com", "Access-Control-Request-Method", "GET")
	if rec.Code != http.StatusNoContent || rec.Header().Get("Access-Control-Allow-Origin") != "*" ||
		!strings.Contains(rec.Header().Get("Access-Control-Allow-Methods"), "GET") ||
		!strings.Contains(rec.Header().Get("Access-Control-Allow-Headers"), "If-None-Match") {
		t.Errorf("preflight = %d %v", rec.Code, rec.Header())
	}
	rec = do(h, "GET", "/asnames/asn/15169")
	hd := rec.Header()
	if hd.Get("Access-Control-Allow-Origin") != "*" || hd.Get("X-Content-Type-Options") != "nosniff" ||
		hd.Get("Referrer-Policy") != "no-referrer" || hd.Get("Server") != "badblock-api-asnames/test" ||
		!strings.Contains(hd.Get("Access-Control-Expose-Headers"), "ETag") ||
		hd.Get("Content-Type") != "application/json; charset=utf-8" {
		t.Errorf("cabeçalhos = %v", hd)
	}
	if do(h, "GET", "/nada").Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("404 sem nosniff")
	}
}

func TestIndexMetaAndRedirect(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, true)
	for _, p := range []string{"/asnames/", "/asnames/v1/"} {
		rec := do(h, "GET", p)
		idx := decode[IndexResponse](t, rec)
		if rec.Code != 200 || idx.App != "api-asnames" || idx.BasePath != "/asnames" || idx.Source != SourceURL ||
			!slices.Contains(idx.Endpoints, "/asnames/asn/{asn}") || !slices.Contains(idx.Endpoints, "/asnames/search?q={texto}") {
			t.Errorf("índice %s = %d %s", p, rec.Code, rec.Body)
		}
	}
	for _, p := range []string{"/asnames", "/asnames?x=1"} {
		if rec := do(h, "GET", p); rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/asnames/" {
			t.Errorf("redirect %s = %d %s", p, rec.Code, rec.Header().Get("Location"))
		}
	}
	for _, p := range []string{"/asnames/meta", "/asnames/v1/meta"} {
		rec := do(h, "GET", p)
		m := decode[MetaResponse](t, rec)
		if rec.Code != 200 || m.App != "api-asnames" || m.Dataset == nil || m.Dataset.ASNs != len(sample) ||
			m.Dataset.Source != SourceURL || m.Collector == nil || m.Collector.App != "collector-asnames" ||
			m.Collector.Consolidated || m.Collector.LastSyncAt == nil || rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("meta = %d %s", rec.Code, rec.Body)
		}
		if rec.Header().Get("X-Cache") != "" {
			t.Errorf("meta não usa cache: %v", rec.Header())
		}
	}
}
