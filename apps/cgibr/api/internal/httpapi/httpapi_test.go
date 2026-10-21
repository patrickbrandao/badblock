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
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/patrickbrandao/badblock/apps/cgibr/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/cgibr/api/internal/realip"
	"github.com/patrickbrandao/badblock/apps/cgibr/api/internal/store"
)

var elea = store.ASNBrief{ASN: 61610, Name: "ELEA DATA CENTERS",
	Document: "35.980.592/0001-30", DocumentDigits: "35980592000130"}

type fakeStore struct {
	mu      sync.Mutex
	calls   int
	pingErr error
}

func (f *fakeStore) count() {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
}

func (f *fakeStore) Ping(context.Context) error { return f.pingErr }
func (f *fakeStore) Dataset(context.Context) (*store.Dataset, error) {
	return &store.Dataset{Version: "v1", AppliedAt: time.Unix(0, 0), URL: SourceURL, SHA256: "ab", ASNs: 1, PrefixesV4: 2, PrefixesV6: 1}, nil
}
func (f *fakeStore) Job(context.Context) (*store.Job, error) {
	t := time.Unix(10, 0)
	return &store.Job{LastSyncAt: &t, LastCheckAt: &t, Consolidated: 0}, nil
}
func (f *fakeStore) ASN(_ context.Context, asn int64) (*store.ASN, error) {
	f.count()
	if asn != 61610 {
		return nil, store.ErrNotFound
	}
	return &store.ASN{ASNBrief: elea, Prefixes: []netip.Prefix{
		netip.MustParsePrefix("187.87.28.0/22"), netip.MustParsePrefix("200.225.48.0/21"),
		netip.MustParsePrefix("2804:8ae0::/32"),
	}}, nil
}
func (f *fakeStore) Covering(_ context.Context, p netip.Prefix) (*store.Match, error) {
	f.count()
	for _, s := range []string{"187.87.28.0/22", "2804:8ae0::/32"} {
		reg := netip.MustParsePrefix(s)
		if reg.Bits() <= p.Bits() && reg.Contains(p.Addr()) {
			return &store.Match{Prefix: reg, ASN: elea}, nil
		}
	}
	return nil, store.ErrNotFound
}
func (f *fakeStore) ByDocument(_ context.Context, digits string) (*store.Document, error) {
	f.count()
	pfx := func(asn int64, s string) store.ASNPrefix {
		return store.ASNPrefix{ASN: asn, Prefix: netip.MustParsePrefix(s)}
	}
	switch digits {
	case elea.DocumentDigits:
		return &store.Document{ASNs: []store.ASNBrief{elea}, Prefixes: []store.ASNPrefix{
			pfx(61610, "187.87.28.0/22"), pfx(61610, "200.225.48.0/21"), pfx(61610, "2804:8ae0::/32"),
		}}, nil
	case "10996639":
		// Fora de ordem de propósito: a API ordena por ASN, ipv e endereço
		// (numérico, não textual: 9.x antes de 10.x). O AS275691 não tem
		// bloco e só aparece em asns.
		isc := func(asn int64) store.ASNBrief {
			return store.ASNBrief{ASN: asn, Name: "ISC", Document: "10996639", DocumentDigits: "10996639"}
		}
		return &store.Document{ASNs: []store.ASNBrief{isc(275689), isc(275690), isc(275691)}, Prefixes: []store.ASNPrefix{
			pfx(275690, "2001:500:2::/48"), pfx(275690, "10.0.0.0/24"), pfx(275689, "2001:500:3::/48"),
			pfx(275690, "9.0.0.0/24"), pfx(275689, "192.5.5.0/24"), pfx(275690, "10.0.0.0/16"),
		}}, nil
	}
	return &store.Document{ASNs: []store.ASNBrief{}, Prefixes: []store.ASNPrefix{}}, nil
}
func (f *fakeStore) ListASNs(context.Context) ([]store.ASNBrief, error) {
	f.count()
	return []store.ASNBrief{elea, {ASN: 275689, Name: "ISC", Document: "10996639", DocumentDigits: "10996639"}}, nil
}

type fixedView struct{ snap dataset.Snapshot }

func (v fixedView) Current() dataset.Snapshot { return v.snap }

// memCache é um cache em memória para os testes.
type memCache struct {
	mu sync.Mutex
	m  map[string][]byte
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
func (c *memCache) Ping(context.Context) error { return nil }
func (c *memCache) Enabled() bool              { return true }

func newAPI(t *testing.T, st *fakeStore, ready bool) (http.Handler, *memCache) {
	t.Helper()
	rip, err := realip.New(realip.DefaultTrustedProxies, realip.DefaultHeaders)
	if err != nil {
		t.Fatal(err)
	}
	snap := dataset.Snapshot{}
	if ready {
		snap = dataset.Snapshot{Version: "0192-v1", AppliedAt: time.Unix(0, 0)}
	}
	c := &memCache{m: map[string][]byte{}}
	a := New(st, fixedView{snap}, c, rip, slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config{BasePath: "/cgibr/", Version: "test"})
	return a.Handler(), c
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

func TestASN(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, true)
	for _, p := range []string{"/cgibr/asn/61610", "/cgibr/v1/asn/61610", "/cgibr/asn/AS61610", "/cgibr/asn/as61610"} {
		rec := do(h, "GET", p)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", p, rec.Code, rec.Body)
		}
		var got ASNResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got.ASN != 61610 || got.DocumentType != "cnpj" || len(got.Prefixes.IPv4) != 2 || len(got.Prefixes.IPv6) != 1 {
			t.Errorf("%s: %+v", p, got)
		}
		if got.Dataset.Version != "0192-v1" {
			t.Errorf("dataset = %+v", got.Dataset)
		}
	}
}

func TestErrors(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, true)
	cases := map[string]int{
		"/cgibr/asn/1":                 404,
		"/cgibr/asn/abc":               400,
		"/cgibr/asn/4294967296":        400,
		"/cgibr/ip/999.1.1.1":          400,
		"/cgibr/ip/8.8.8.8":            404,
		"/cgibr/prefix/187.87.28.0/33": 400,
		"/cgibr/document/123":          400,
		"/cgibr/document/11111111":     404,
		"/cgibr/v2/asn/61610":          404,
		"/cgibr/nada":                  404,
		"/outra-coisa":                 404,
		"/asn/61610":                   404,
	}
	for p, want := range cases {
		rec := do(h, "GET", p)
		if rec.Code != want {
			t.Errorf("%s: %d, quero %d (%s)", p, rec.Code, want, rec.Body)
		}
		if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
			t.Errorf("%s: erro deveria ser JSON", p)
		}
	}
}

func TestIPAndPrefix(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, true)

	rec := do(h, "GET", "/cgibr/ip/187.87.29.10")
	var ip IPResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &ip)
	if rec.Code != 200 || ip.Prefix != "187.87.28.0/22" || ip.ASN.ASN != 61610 {
		t.Errorf("ip = %d %+v", rec.Code, ip)
	}
	rec = do(h, "GET", "/cgibr/ip/2804:8ae0::1")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "2804:8ae0::/32") {
		t.Errorf("ipv6 = %d %s", rec.Code, rec.Body)
	}

	rec = do(h, "GET", "/cgibr/prefix/187.87.29.1/24")
	var p PrefixResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	if rec.Code != 200 || p.Query != "187.87.29.0/24" || p.Prefix != "187.87.28.0/22" || p.Exact {
		t.Errorf("prefix = %d %+v", rec.Code, p)
	}
	rec = do(h, "GET", "/cgibr/v1/prefix/187.87.28.0/22")
	_ = json.Unmarshal(rec.Body.Bytes(), &p)
	if !p.Exact {
		t.Errorf("prefix exato = %+v", p)
	}
}

func TestDocumentAcceptsFormatted(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, true)
	for _, p := range []string{"/cgibr/document/35980592000130", "/cgibr/document/35.980.592%2F0001-30"} {
		rec := do(h, "GET", p)
		var d DocumentResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &d)
		if rec.Code != 200 || d.Count != 1 || d.ASNs[0].ASN != 61610 || d.DocumentType != "cnpj" {
			t.Errorf("%s: %d %s", p, rec.Code, rec.Body)
		}
	}
}

func TestDocumentPrefixes(t *testing.T) {
	h, c := newAPI(t, &fakeStore{}, true)

	rec := do(h, "GET", "/cgibr/document/35980592000130")
	body := rec.Body.String()
	want := `"asns":[{"asn":61610,"name":"ELEA DATA CENTERS"}],"prefixes":[` +
		`{"asn":61610,"ipv":4,"prefix":"187.87.28.0/22"},{"asn":61610,"ipv":4,"prefix":"200.225.48.0/21"},` +
		`{"asn":61610,"ipv":6,"prefix":"2804:8ae0::/32"}],"dataset":`
	if rec.Code != 200 || !strings.Contains(body, want) {
		t.Errorf("elea: %d %s", rec.Code, body)
	}

	rec = do(h, "GET", "/cgibr/v1/document/10996639")
	var d DocumentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil || rec.Code != 200 {
		t.Fatalf("isc: %d %s", rec.Code, rec.Body)
	}
	var got []string
	for _, p := range d.Prefixes {
		got = append(got, strconv.FormatInt(p.ASN, 10)+" "+strconv.Itoa(p.IPV)+" "+p.Prefix)
	}
	wantOrder := "275689 4 192.5.5.0/24,275689 6 2001:500:3::/48," +
		"275690 4 9.0.0.0/24,275690 4 10.0.0.0/16,275690 4 10.0.0.0/24,275690 6 2001:500:2::/48"
	if strings.Join(got, ",") != wantOrder || d.Count != 3 || len(d.ASNs) != 3 {
		t.Errorf("isc: ordem %v, count %d", got, d.Count)
	}

	// A revisão do corpo entra na chave: nada fica sob a chave antiga "doc:".
	for k := range c.m {
		if strings.Contains(k, ":doc:") || !strings.Contains(k, ":doc2:") {
			t.Errorf("chave de cache de /document: %s", k)
		}
	}
	if e := rec.Header().Get("ETag"); e != etagFor("0192-v1", "doc2:10996639") {
		t.Errorf("ETag = %s", e)
	}
}

func TestASNsList(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, true)
	rec := do(h, "GET", "/cgibr/asns")
	var l ASNListResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &l)
	if rec.Code != 200 || l.Count != 2 || l.ASNs[1].DocumentType != "foreign" {
		t.Errorf("asns = %d %s", rec.Code, rec.Body)
	}
}

func TestCacheAndETag(t *testing.T) {
	st := &fakeStore{}
	h, c := newAPI(t, st, true)

	first := do(h, "GET", "/cgibr/asn/61610")
	if first.Header().Get("X-Cache") != "MISS" {
		t.Errorf("primeira = %s", first.Header().Get("X-Cache"))
	}
	// Versionada e sem versão compartilham o cache (mesmo conteúdo).
	second := do(h, "GET", "/cgibr/v1/asn/61610")
	if second.Header().Get("X-Cache") != "HIT" || st.calls != 1 {
		t.Errorf("segunda = %s, calls = %d", second.Header().Get("X-Cache"), st.calls)
	}
	if len(c.m) != 1 {
		t.Errorf("chaves no cache = %d", len(c.m))
	}
	for k := range c.m {
		if !strings.HasPrefix(k, "badblock:api-cgibr:0192-v1:") {
			t.Errorf("chave sem prefixo/versão: %s", k)
		}
	}
	etag := first.Header().Get("ETag")
	if etag == "" || first.Header().Get("X-Dataset-Version") != "0192-v1" {
		t.Fatalf("cabeçalhos = %v", first.Header())
	}
	if rec := do(h, "GET", "/cgibr/asn/61610", "If-None-Match", etag); rec.Code != http.StatusNotModified {
		t.Errorf("If-None-Match = %d", rec.Code)
	}
}

func TestNotReady(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, false)
	rec := do(h, "GET", "/cgibr/asn/61610")
	if rec.Code != 503 || !strings.Contains(rec.Body.String(), "dataset_not_ready") {
		t.Errorf("sem dataset = %d %s", rec.Code, rec.Body)
	}
	rec = do(h, "GET", "/cgibr/status")
	var s StatusResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &s)
	if rec.Code != 200 || s.Status != "starting" {
		t.Errorf("status sem dataset = %d %+v", rec.Code, s)
	}
}

func TestHealth(t *testing.T) {
	st := &fakeStore{}
	h, _ := newAPI(t, st, true)
	for _, m := range []string{"GET", "POST"} {
		rec := do(h, m, "/cgibr/health")
		var s StatusResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &s)
		if rec.Code != 200 || !s.Success || s.Status != "ok" || s.Timestamp == "" || s.Message == "" {
			t.Errorf("%s /health = %d %s", m, rec.Code, rec.Body)
		}
	}
	if rec := do(h, "GET", "/cgibr/ping"); rec.Body.String() != "pong" {
		t.Errorf("ping = %q", rec.Body)
	}
	st.pingErr = errors.New("down")
	rec := do(h, "GET", "/cgibr/status")
	if rec.Code != 503 || !strings.Contains(rec.Body.String(), `"success":false`) {
		t.Errorf("postgres fora = %d %s", rec.Code, rec.Body)
	}
}

func TestIndexMetaAndRedirect(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, true)
	if rec := do(h, "GET", "/cgibr/"); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"/cgibr/asn/{asn}"`) {
		t.Errorf("índice = %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", "/cgibr/v1/"); rec.Code != 200 {
		t.Errorf("índice v1 = %d", rec.Code)
	}
	if rec := do(h, "GET", "/cgibr"); rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/cgibr/" {
		t.Errorf("redirect = %d %s", rec.Code, rec.Header().Get("Location"))
	}
	rec := do(h, "GET", "/cgibr/meta")
	var m MetaResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	if rec.Code != 200 || m.Dataset == nil || m.Collector == nil || m.Collector.Consolidated || m.Dataset.PrefixesV6 != 1 {
		t.Errorf("meta = %d %s", rec.Code, rec.Body)
	}
}
