package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/patrickbrandao/badblock/apps/apnic/api/internal/cache"
	"github.com/patrickbrandao/badblock/apps/apnic/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/apnic/api/internal/realip"
	"github.com/patrickbrandao/badblock/apps/apnic/api/internal/rir"
	"github.com/patrickbrandao/badblock/apps/apnic/api/internal/store"
)

// base é o caminho de base padrão (/apnic); os testes não citam o RIR.
const base = rir.BasePath

func dp(s string) *time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return &t
}

var (
	created = time.Date(2026, 9, 28, 23, 19, 10, 0, time.UTC)
	elea  = store.Info{CC: new("BR"), RegDate: dp("2019-02-11"), Status: "allocated", OpaqueID: new("258500")}
	empty   = store.Info{Status: "available"}

	asns = []store.ASNRange{
		{Start: 28003, End: 28005, Count: 3, Info: empty, CreatedAt: created, UpdatedAt: created},
		{Start: 61610, End: 61610, Count: 1, Info: store.Info{CC: new("BR"), RegDate: dp("2023-02-27"), Status: "allocated", OpaqueID: new("258500")},
			CreatedAt: created, UpdatedAt: created.Add(time.Hour)},
	}
	blocks = []store.Block{
		block("187.87.28.0/22", elea, "187.87.28.0", 1024),
		block("200.225.48.0/21", elea, "200.225.48.0", 2048),
		block("2804:8ae0::/32", elea, "2804:8ae0::", 32),
		// Registro IPv4 que não forma CIDR (62.122.208.0 + 1280 = /22 + /24).
		block("62.122.208.0/22", store.Info{CC: new("ZZ"), Status: "reserved"}, "62.122.208.0", 1280),
		block("62.122.212.0/24", store.Info{CC: new("ZZ"), Status: "reserved"}, "62.122.208.0", 1280),
		// Titular com recursos em mais de um país.
		block("1.0.0.0/24", store.Info{CC: new("HK"), Status: "allocated", OpaqueID: new("A92E1062")}, "1.0.0.0", 256),
		block("1.0.1.0/24", store.Info{CC: new("CN"), Status: "allocated", OpaqueID: new("A92E1062")}, "1.0.1.0", 256),
		block("1.0.2.0/24", store.Info{CC: new("CN"), Status: "allocated", OpaqueID: new("A92E1062")}, "1.0.2.0", 256),
	}
)

func block(p string, info store.Info, start string, value int64) store.Block {
	return store.Block{Prefix: netip.MustParsePrefix(p), Info: info, RecordStart: netip.MustParseAddr(start),
		RecordValue: value, CreatedAt: created, UpdatedAt: created}
}

type fakeStore struct {
	mu       sync.Mutex
	calls    int
	pingErr  error
	err      error // devolvido pelas consultas de dados e pela meta
	panicMsg string
	noData   bool // Dataset e Job sem linha (antes da primeira carga)
}

func (f *fakeStore) count() error {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.panicMsg != "" {
		panic(f.panicMsg)
	}
	return f.err
}

func (f *fakeStore) Ping(context.Context) error { return f.pingErr }

func (f *fakeStore) Dataset(context.Context) (*store.Dataset, error) {
	if f.err != nil || f.noData {
		return nil, f.err
	}
	n := func(v int) *int { return &v }
	return &store.Dataset{
		Version: "0192-v1", AppliedAt: created, URL: rir.SourceURL, SHA256: new("ab"), MD5: new("cd"), Serial: new("20260927"),
		StartDate: dp("1987-01-01"), EndDate: dp("2026-09-25"),
		ASNRecords: n(2), IPv4Records: n(6), IPv6Records: n(1), PrefixesV4: n(7), PrefixesV6: n(1),
	}, nil
}

func (f *fakeStore) Job(context.Context) (*store.Job, error) {
	if f.err != nil || f.noData {
		return nil, f.err
	}
	t := created
	return &store.Job{LastSyncAt: &t, LastCheckAt: &t, Consolidated: 0}, nil
}

func (f *fakeStore) ASN(_ context.Context, asn int64) (*store.ASNRange, error) {
	if err := f.count(); err != nil {
		return nil, err
	}
	for _, a := range asns {
		if a.Start <= asn && asn <= a.End {
			return &a, nil
		}
	}
	return nil, store.ErrNotFound
}

func (f *fakeStore) Covering(_ context.Context, p netip.Prefix) (*store.Block, error) {
	if err := f.count(); err != nil {
		return nil, err
	}
	var best *store.Block
	for i, b := range blocks {
		if b.Prefix.Bits() <= p.Bits() && b.Prefix.Contains(p.Addr()) && (best == nil || b.Prefix.Bits() > best.Prefix.Bits()) {
			best = &blocks[i]
		}
	}
	if best == nil {
		return nil, store.ErrNotFound
	}
	return best, nil
}

func (f *fakeStore) Holder(_ context.Context, id string) (*store.Holder, error) {
	if err := f.count(); err != nil {
		return nil, err
	}
	h := &store.Holder{OpaqueID: id}
	for _, a := range asns {
		if a.Info.OpaqueID != nil && *a.Info.OpaqueID == id {
			h.ASNs = append(h.ASNs, a)
		}
	}
	for _, b := range blocks {
		if b.Info.OpaqueID != nil && *b.Info.OpaqueID == id {
			h.Blocks = append(h.Blocks, b)
		}
	}
	if len(h.ASNs)+len(h.Blocks) == 0 {
		return nil, store.ErrNotFound
	}
	return h, nil
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

func newTestAPI(t *testing.T, st *fakeStore, ready bool, c cache.Cache) *API {
	t.Helper()
	rip, err := realip.New(realip.DefaultTrustedProxies, realip.DefaultHeaders)
	if err != nil {
		t.Fatal(err)
	}
	snap := dataset.Snapshot{}
	if ready {
		snap = dataset.Snapshot{Version: "0192-v1", AppliedAt: created}
	}
	return New(st, fixedView{snap}, c, rip, slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config{BasePath: base + "/", Version: "test"})
}

func newAPIWith(t *testing.T, st *fakeStore, ready bool, c cache.Cache) http.Handler {
	t.Helper()
	return newTestAPI(t, st, ready, c).Handler()
}

func newAPI(t *testing.T, st *fakeStore, ready bool) (http.Handler, *memCache) {
	t.Helper()
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
		t.Fatalf("JSON inválido (%d): %v\n%s", rec.Code, err, rec.Body)
	}
	return v
}

func TestASN(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, true)
	for _, p := range []string{"/asn/61610", "/v1/asn/61610", "/asn/AS61610", "/asn/as61610"} {
		rec := do(h, "GET", base+p)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", p, rec.Code, rec.Body)
		}
		got := decode[ASNResponse](t, rec)
		if got.ASN != 61610 || got.Range != (Range{61610, 61610, 1}) || *got.CC != "BR" || *got.RegDate != "2023-02-27" ||
			got.Status != "allocated" || *got.OpaqueID != "258500" {
			t.Errorf("%s: %+v", p, got)
		}
		if got.FirstSeen != "2026-09-28T23:19:10Z" || got.UpdatedAt != "2026-09-29T00:19:10Z" || got.Dataset.Version != "0192-v1" {
			t.Errorf("%s: datas %s %s, dataset %+v", p, got.FirstSeen, got.UpdatedAt, got.Dataset)
		}
	}

	// ASN no meio de uma faixa: responde a faixa; campos vazios na fonte são null.
	rec := do(h, "GET", base+"/asn/28004")
	got := decode[ASNResponse](t, rec)
	if rec.Code != 200 || got.ASN != 28004 || got.Range != (Range{28003, 28005, 3}) || got.Status != "available" {
		t.Errorf("faixa = %d %s", rec.Code, rec.Body)
	}
	for _, field := range []string{`"cc":null`, `"reg_date":null`, `"opaque_id":null`} {
		if !strings.Contains(rec.Body.String(), field) {
			t.Errorf("faltou %s em %s", field, rec.Body)
		}
	}
}

func TestErrors(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, true)
	cases := map[string]int{
		"/asn/1":                              404,
		"/asn/28006":                          404,
		"/asn/abc":                            400,
		"/asn/-1":                             400,
		"/asn/+1":                             400,
		"/asn/AS":                             400,
		"/asn/4294967296":                     400,
		"/ip/999.1.1.1":                       400,
		"/ip/fe80::1%25eth0":                  400,
		"/ip/8.8.8.8":                         404,
		"/prefix/187.87.28.0/33":              400,
		"/prefix/187.87.28.0/x":               400,
		"/prefix/2804:8ae0::/129":             400,
		"/prefix/8.8.8.0/24":                  404,
		"/holder/nao%20existe":                400,
		"/holder/" + strings.Repeat("a", 129): 400,
		"/holder/ção":                         400,
		"/holder/999999":                      404,
		"/v2/asn/61610":                       404,
		"/nada":                               404,
		"/v1/nada":                            404,
	}
	for p, want := range cases {
		rec := do(h, "GET", base+p)
		if rec.Code != want {
			t.Errorf("%s: %d, quero %d (%s)", p, rec.Code, want, rec.Body)
		}
		if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") || rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: erro deveria ser JSON sem cache (%v)", p, rec.Header())
		}
		body := decode[errorBody](t, rec)
		if body.Error.Code == "" || body.Error.Message == "" {
			t.Errorf("%s: erro sem code/message: %s", p, rec.Body)
		}
	}
	for _, p := range []string{"/outra-coisa", "/asn/61610", base + "x/asn/61610"} {
		if rec := do(h, "GET", p); rec.Code != 404 {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}
	rec := do(h, "GET", base+"/asn/1")
	if !strings.Contains(rec.Body.String(), rir.Title) {
		t.Errorf("mensagem deveria citar o RIR: %s", rec.Body)
	}
	// No RIR que regenera o opaque-id todo dia, o 404 do titular explica e
	// aponta as rotas que dão o opaque_id atual.
	rec = do(h, "GET", base+"/holder/999999")
	hint := strings.Contains(rec.Body.String(), base+"/ip/{ip}") && strings.Contains(rec.Body.String(), base+"/asn/{asn}")
	if hint != rir.OpaqueIDChangesDaily {
		t.Errorf("404 do titular (OpaqueIDChangesDaily=%v): %s", rir.OpaqueIDChangesDaily, rec.Body)
	}
}

func TestIPAndPrefix(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, true)

	for _, q := range []string{"187.87.29.10", "::ffff:187.87.29.10"} {
		rec := do(h, "GET", base+"/ip/"+q)
		ip := decode[IPResponse](t, rec)
		if rec.Code != 200 || ip.IP != "187.87.29.10" || ip.Prefix != "187.87.28.0/22" || *ip.OpaqueID != "258500" ||
			*ip.CC != "BR" || *ip.RegDate != "2019-02-11" || ip.Record != (Record{"187.87.28.0", 1024}) {
			t.Errorf("%s = %d %s", q, rec.Code, rec.Body)
		}
	}
	rec := do(h, "GET", base+"/ip/2804:8ae0::1")
	ip := decode[IPResponse](t, rec)
	if rec.Code != 200 || ip.Prefix != "2804:8ae0::/32" || ip.Record != (Record{"2804:8ae0::", 32}) {
		t.Errorf("ipv6 = %d %s", rec.Code, rec.Body)
	}
	// Bloco dividido: o pedaço /24 aponta para o registro original.
	rec = do(h, "GET", base+"/ip/62.122.212.9")
	ip = decode[IPResponse](t, rec)
	if rec.Code != 200 || ip.Prefix != "62.122.212.0/24" || ip.Record != (Record{"62.122.208.0", 1280}) || ip.OpaqueID != nil {
		t.Errorf("bloco dividido = %d %s", rec.Code, rec.Body)
	}

	rec = do(h, "GET", base+"/prefix/187.87.29.1/24")
	p := decode[PrefixResponse](t, rec)
	if rec.Code != 200 || p.Query != "187.87.29.0/24" || p.Prefix != "187.87.28.0/22" || p.Exact || p.Status != "allocated" {
		t.Errorf("prefix = %d %s", rec.Code, rec.Body)
	}
	rec = do(h, "GET", base+"/v1/prefix/187.87.28.0/22")
	p = decode[PrefixResponse](t, rec)
	if !p.Exact || p.Record != (Record{"187.87.28.0", 1024}) {
		t.Errorf("prefix exato = %s", rec.Body)
	}
	rec = do(h, "GET", base+"/prefix/2804:8ae0:ffff::/48")
	p = decode[PrefixResponse](t, rec)
	if rec.Code != 200 || p.Prefix != "2804:8ae0::/32" || p.Exact {
		t.Errorf("prefix ipv6 = %d %s", rec.Code, rec.Body)
	}
}

func TestHolder(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, true)
	rec := do(h, "GET", base+"/holder/258500")
	got := decode[HolderResponse](t, rec)
	if rec.Code != 200 || got.OpaqueID != "258500" || *got.CC != "BR" || strings.Join(got.CCs, ",") != "BR" ||
		got.Counts != (HolderCounts{ASNs: 1, IPv4: 2, IPv6: 1}) {
		t.Fatalf("holder = %d %s", rec.Code, rec.Body)
	}
	if got.ASNs[0] != (HolderASN{Start: 61610, End: 61610, Count: 1, CC: got.ASNs[0].CC, Status: "allocated", RegDate: got.ASNs[0].RegDate}) ||
		*got.ASNs[0].RegDate != "2023-02-27" {
		t.Errorf("asns = %+v", got.ASNs)
	}
	if got.Prefixes.IPv4[0].Prefix != "187.87.28.0/22" || got.Prefixes.IPv6[0].Prefix != "2804:8ae0::/32" || *got.Prefixes.IPv4[0].RegDate != "2019-02-11" {
		t.Errorf("prefixes = %+v", got.Prefixes)
	}

	// Titular em dois países: cc é o mais frequente; ccs, todos. Sem ASNs: lista vazia, não null.
	rec = do(h, "GET", base+"/v1/holder/A92E1062")
	got = decode[HolderResponse](t, rec)
	if rec.Code != 200 || *got.CC != "CN" || strings.Join(got.CCs, ",") != "CN,HK" || got.Counts != (HolderCounts{0, 3, 0}) {
		t.Errorf("multi-país = %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"asns":[]`) || !strings.Contains(rec.Body.String(), `"ipv6":[]`) {
		t.Errorf("listas vazias deveriam ser []: %s", rec.Body)
	}
}

func TestHolderCountryTie(t *testing.T) {
	h := &store.Holder{OpaqueID: "x", Blocks: []store.Block{
		block("10.0.0.0/24", store.Info{CC: new("US"), Status: "allocated"}, "10.0.0.0", 256),
		block("10.0.1.0/24", store.Info{CC: new("CA"), Status: "allocated"}, "10.0.1.0", 256),
		block("10.0.2.0/24", store.Info{Status: "allocated"}, "10.0.2.0", 256),
	}}
	got := holderResponse(h, dataset.Snapshot{})
	if *got.CC != "CA" || strings.Join(got.CCs, ",") != "CA,US" {
		t.Errorf("empate = %v %v", *got.CC, got.CCs)
	}
	got = holderResponse(&store.Holder{OpaqueID: "y", ASNs: []store.ASNRange{{Start: 1, End: 1, Count: 1, Info: empty}}}, dataset.Snapshot{})
	if got.CC != nil || len(got.CCs) != 0 {
		t.Errorf("sem país = %v %v", got.CC, got.CCs)
	}
}

// HEAD passa pelo servidor HTTP de verdade: é ele que descarta o corpo.
func TestHead(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, true)
	srv := httptest.NewServer(h)
	defer srv.Close()
	for path, want := range map[string]int{base + "/asn/61610": 200, base + "/v1/holder/258500": 200, base + "/asn/1": 404} {
		resp, err := http.Head(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != want || len(body) != 0 || !strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
			t.Errorf("HEAD %s = %d %q %v", path, resp.StatusCode, body, resp.Header)
		}
		if want == 200 && resp.Header.Get("ETag") == "" {
			t.Errorf("HEAD %s sem ETag", path)
		}
	}
}

func TestCacheAndETag(t *testing.T) {
	st := &fakeStore{}
	h, c := newAPI(t, st, true)

	first := do(h, "GET", base+"/asn/61610")
	if first.Header().Get("X-Cache") != "MISS" {
		t.Errorf("primeira = %s", first.Header().Get("X-Cache"))
	}
	// Versionada e sem versão compartilham o cache (mesmo conteúdo).
	second := do(h, "GET", base+"/v1/asn/AS61610")
	if second.Header().Get("X-Cache") != "HIT" || st.calls != 1 || second.Body.String() != first.Body.String() {
		t.Errorf("segunda = %s, calls = %d", second.Header().Get("X-Cache"), st.calls)
	}
	want := "badblock:" + rir.App + ":0192-v1:asn:61610"
	if _, ok := c.m[want]; !ok || len(c.m) != 1 {
		t.Errorf("chaves no cache = %v, quero %s", c.m, want)
	}
	etag := first.Header().Get("ETag")
	if etag == "" || etag != second.Header().Get("ETag") || first.Header().Get("X-Dataset-Version") != "0192-v1" ||
		first.Header().Get("Cache-Control") != "public, max-age=300" {
		t.Fatalf("cabeçalhos = %v / %v", first.Header(), second.Header())
	}
	for _, inm := range []string{etag, strings.TrimPrefix(etag, "W/"), `"x", ` + etag, "*"} {
		rec := do(h, "GET", base+"/v1/asn/61610", "If-None-Match", inm)
		if rec.Code != http.StatusNotModified || rec.Body.Len() != 0 || rec.Header().Get("ETag") != etag {
			t.Errorf("If-None-Match %s = %d", inm, rec.Code)
		}
	}
	if st.calls != 1 {
		t.Errorf("304 não deveria consultar o banco (calls = %d)", st.calls)
	}
	if rec := do(h, "GET", base+"/asn/61610", "If-None-Match", `W/"outro"`); rec.Code != 200 {
		t.Errorf("ETag diferente = %d", rec.Code)
	}
	// Outra consulta, outro ETag; 404 não entra no cache.
	if rec := do(h, "GET", base+"/ip/187.87.29.10"); rec.Header().Get("ETag") == etag {
		t.Error("ETag deveria depender da consulta")
	}
	do(h, "GET", base+"/asn/1")
	do(h, "GET", base+"/asn/1")
	if len(c.m) != 2 {
		t.Errorf("404 entrou no cache: %v", c.m)
	}
}

func TestCacheDisabledBypass(t *testing.T) {
	h := newAPIWith(t, &fakeStore{}, true, cache.Noop{})
	rec := do(h, "GET", base+"/holder/258500")
	if rec.Code != 200 || rec.Header().Get("X-Cache") != "BYPASS" {
		t.Errorf("sem cache = %d %s", rec.Code, rec.Header().Get("X-Cache"))
	}
}

func TestNotReady(t *testing.T) {
	st := &fakeStore{}
	h, _ := newAPI(t, st, false)
	for _, p := range []string{"/asn/61610", "/ip/187.87.29.10", "/prefix/187.87.28.0/22", "/holder/258500"} {
		rec := do(h, "GET", base+p)
		if rec.Code != 503 || !strings.Contains(rec.Body.String(), "dataset_not_ready") {
			t.Errorf("%s sem dataset = %d %s", p, rec.Code, rec.Body)
		}
	}
	if st.calls != 0 {
		t.Errorf("sem dataset não deveria consultar (calls = %d)", st.calls)
	}
}

func TestStoreFailures(t *testing.T) {
	cases := []struct {
		err  error
		code int
		name string
	}{
		{errors.New("conexão recusada"), 503, "database_unavailable"},
		{context.DeadlineExceeded, 504, "timeout"},
	}
	for _, c := range cases {
		st := &fakeStore{err: c.err}
		h, mc := newAPI(t, st, true)
		for _, p := range []string{"/asn/61610", "/ip/187.87.29.10", "/prefix/187.87.28.0/22", "/holder/258500", "/meta"} {
			rec := do(h, "GET", base+p)
			if rec.Code != c.code || !strings.Contains(rec.Body.String(), c.name) {
				t.Errorf("%v %s = %d %s", c.err, p, rec.Code, rec.Body)
			}
		}
		if len(mc.m) != 0 {
			t.Errorf("erro entrou no cache: %v", mc.m)
		}
	}
}

func TestPanicIsRecovered(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{panicMsg: "bug"}, true)
	rec := do(h, "GET", base+"/asn/61610")
	if rec.Code != 500 || !strings.Contains(rec.Body.String(), "internal_error") {
		t.Errorf("panic = %d %s", rec.Code, rec.Body)
	}
}

func TestStatus(t *testing.T) {
	cases := []struct {
		name            string
		ready           bool
		pgErr, cacheErr error
		noCache         bool
		code            int
		status, pg, vk  string
		dataset         string
	}{
		{"ok", true, nil, nil, false, 200, "ok", "ok", "ok", "ok"},
		{"sem cache", true, nil, nil, true, 200, "ok", "ok", "disabled", "ok"},
		{"starting", false, nil, nil, false, 200, "starting", "ok", "ok", "empty"},
		{"degraded", true, nil, errors.New("down"), false, 200, "degraded", "ok", "error", "ok"},
		{"error", true, errors.New("down"), nil, false, 503, "error", "error", "ok", "ok"},
	}
	for _, c := range cases {
		var ch cache.Cache = &memCache{m: map[string][]byte{}, pingErr: c.cacheErr}
		if c.noCache {
			ch = cache.Noop{}
		}
		h := newAPIWith(t, &fakeStore{pingErr: c.pgErr}, c.ready, ch)
		for _, m := range []string{"GET", "POST"} {
			for _, p := range []string{"/health", "/status"} {
				rec := do(h, m, base+p)
				s := decode[StatusResponse](t, rec)
				if rec.Code != c.code || s.Status != c.status || s.Success != (c.code == 200) || s.Timestamp == "" || s.Message == "" ||
					s.Checks["postgres"] != c.pg || s.Checks["valkey"] != c.vk || s.Checks["dataset"] != c.dataset {
					t.Errorf("%s: %s %s = %d %s", c.name, m, p, rec.Code, rec.Body)
				}
				if rec.Header().Get("Cache-Control") != "no-store" {
					t.Errorf("%s: status com cache", c.name)
				}
			}
		}
	}
	h, _ := newAPI(t, &fakeStore{}, true)
	if rec := do(h, "GET", base+"/ping"); rec.Code != 200 || rec.Body.String() != "pong" {
		t.Errorf("ping = %d %q", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", base+"/v1/ping"); rec.Code != 404 {
		t.Errorf("ping é fora do versionamento: %d", rec.Code)
	}
}

func TestCORSAndHeaders(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, true)
	rec := do(h, "OPTIONS", base+"/asn/61610", "Origin", "https://x.example", "Access-Control-Request-Method", "GET")
	if rec.Code != http.StatusNoContent || rec.Header().Get("Access-Control-Allow-Origin") != "*" ||
		!strings.Contains(rec.Header().Get("Access-Control-Allow-Methods"), "GET") ||
		!strings.Contains(rec.Header().Get("Access-Control-Allow-Headers"), "If-None-Match") {
		t.Errorf("preflight = %d %v", rec.Code, rec.Header())
	}
	rec = do(h, "GET", base+"/asn/61610")
	hd := rec.Header()
	if hd.Get("X-Content-Type-Options") != "nosniff" || hd.Get("Access-Control-Allow-Origin") != "*" ||
		!strings.Contains(hd.Get("Access-Control-Expose-Headers"), "ETag") || hd.Get("Server") != "badblock-"+rir.App+"/test" ||
		hd.Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("cabeçalhos = %v", hd)
	}
	if rec := do(h, "DELETE", base+"/asn/61610"); rec.Code == 200 {
		t.Errorf("DELETE não deveria responder 200")
	}
}

func TestIndexMetaAndRedirect(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, true)
	for _, p := range []string{base + "/", base + "/v1/"} {
		rec := do(h, "GET", p)
		idx := decode[IndexResponse](t, rec)
		if rec.Code != 200 || idx.App != rir.App || idx.Registry != rir.Title || idx.Source != rir.SourceURL ||
			!strings.Contains(rec.Body.String(), `"`+base+`/holder/{opaque_id}"`) {
			t.Errorf("índice %s = %d %s", p, rec.Code, rec.Body)
		}
	}
	if rec := do(h, "GET", base); rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != base+"/" {
		t.Errorf("redirect = %d %s", rec.Code, rec.Header().Get("Location"))
	}
	for _, p := range []string{"/meta", "/v1/meta"} {
		rec := do(h, "GET", base+p)
		m := decode[MetaResponse](t, rec)
		if rec.Code != 200 || m.App != rir.App || m.Dataset == nil || m.Collector == nil || m.Collector.Consolidated ||
			m.Collector.App != "collector-"+rir.Source || *m.Dataset.PrefixesV4 != 7 || *m.Dataset.Serial != "20260927" ||
			*m.Dataset.StartDate != "1987-01-01" || *m.Dataset.EndDate != "2026-09-25" || *m.Dataset.MD5 != "cd" ||
			rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("meta = %d %s", rec.Code, rec.Body)
		}
	}
	// Antes da primeira carga: dataset e collector null (e a meta responde).
	h, _ = newAPI(t, &fakeStore{noData: true}, false)
	rec := do(h, "GET", base+"/meta")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"dataset":null`) || !strings.Contains(rec.Body.String(), `"collector":null`) {
		t.Errorf("meta sem dados = %d %s", rec.Code, rec.Body)
	}
}

// Os formatos de opaque-id dos cinco RIRs (medidos nos arquivos reais) passam
// na validação.
func TestHolderAcceptsEveryRIRFormat(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, true)
	for _, id := range []string{"258500", "12", "F3619C8C", "A9117E4D", "45fe880b68a8f2850ebdcfdc57b4556c",
		"422db66e-88a2-489c-bf20-66c96d820c91"} {
		if rec := do(h, "GET", base+"/holder/"+id); rec.Code != 200 && rec.Code != 404 {
			t.Errorf("%s: %d %s", id, rec.Code, rec.Body)
		}
	}
}
