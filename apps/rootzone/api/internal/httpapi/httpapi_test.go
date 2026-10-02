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

	"github.com/patrickbrandao/badblock/apps/rootzone/api/internal/cache"
	"github.com/patrickbrandao/badblock/apps/rootzone/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/rootzone/api/internal/realip"
	"github.com/patrickbrandao/badblock/apps/rootzone/api/internal/store"
)

const base = "/rootzone"

var testSerial = int64(2026092901)

// Recorte real da zona de 2026-09-29 (serial 2026092901), como o
// collector-rootzone grava: bo (sem DS, um servidor sem AAAA, glue sob outro
// TLD), br, top (2 DS, servidores só com IPv4 ou só com IPv6) e xn--p1ai (рф).
var sampleTLDs = []store.TLD{
	{TLD: "bo", Unicode: "bo", Nameservers: 4, NameserversV4: 4, NameserversV6: 3, DSRecords: 0},
	{TLD: "br", Unicode: "br", Nameservers: 6, NameserversV4: 6, NameserversV6: 6, DSRecords: 1},
	{TLD: "top", Unicode: "top", Nameservers: 8, NameserversV4: 6, NameserversV6: 3, DSRecords: 2},
	{TLD: "xn--p1ai", Unicode: "рф", Nameservers: 6, NameserversV4: 6, NameserversV6: 6, DSRecords: 1},
}

// sampleRecords: dono → RRs de rootzone_record (NS, DS e glue).
var sampleRecords = map[string][]store.Record{
	"bo": {
		{Type: "NS", RData: "anycast.ns.nic.bo", TTL: 172800}, {Type: "NS", RData: "ns2.nic.fr", TTL: 172800},
		{Type: "NS", RData: "ns.dns.br", TTL: 172800}, {Type: "NS", RData: "ns.nic.bo", TTL: 172800},
	},
	"br": {
		{Type: "DS", RData: "38298 13 2 9F2D4993F47B0F2751DE0007D70A2754EE532FE373761154D9EA7A8CB9D8EA18", TTL: 86400},
		{Type: "NS", RData: "a.dns.br", TTL: 172800}, {Type: "NS", RData: "b.dns.br", TTL: 172800},
		{Type: "NS", RData: "c.dns.br", TTL: 172800}, {Type: "NS", RData: "d.dns.br", TTL: 172800},
		{Type: "NS", RData: "e.dns.br", TTL: 172800}, {Type: "NS", RData: "f.dns.br", TTL: 172800},
	},
	"top": {
		{Type: "DS", RData: "26780 8 2 5D6E7869EE8E3B536A617DE89482DDD1DCB9DB9DBB1AC33D6ED351E2CA095B1B", TTL: 86400},
		{Type: "DS", RData: "41508 13 2 31422CDF4A9AF99914FF85C97D4FB2291F293C9ADB26011B39E7638A51E1C7DD", TTL: 86400},
		{Type: "NS", RData: "a.zdnscloud.cn", TTL: 172800}, {Type: "NS", RData: "b.zdnscloud.cn", TTL: 172800},
		{Type: "NS", RData: "c.zdnscloud.com", TTL: 172800}, {Type: "NS", RData: "d.zdnscloud.com", TTL: 172800},
		{Type: "NS", RData: "e.zdnscloud.cn", TTL: 172800}, {Type: "NS", RData: "f.zdnscloud.cn", TTL: 172800},
		{Type: "NS", RData: "i.zdnscloud.cn", TTL: 172800}, {Type: "NS", RData: "j.zdnscloud.com", TTL: 172800},
	},
	"xn--p1ai": {
		{Type: "DS", RData: "60491 8 2 87F1F8C82EC00047C43AC499A73CC9BEB4FC1503E8558F086DCFB614405F7F21", TTL: 86400},
		{Type: "NS", RData: "a.dns.ripn.net", TTL: 172800}, {Type: "NS", RData: "b.dns.ripn.net", TTL: 172800},
		{Type: "NS", RData: "c.tld-servers.ru", TTL: 172800}, {Type: "NS", RData: "d.dns.ripn.net", TTL: 172800},
		{Type: "NS", RData: "e.dns.ripn.net", TTL: 172800}, {Type: "NS", RData: "f.dns.ripn.net", TTL: 172800},
	},
}

// sampleGlue: dono → glue (A e AAAA) de cada servidor dos TLDs acima.
var sampleGlue = func() map[string][]store.Glue {
	g := map[string][]store.Glue{}
	add := func(owner, typ, addr string) {
		g[owner] = append(g[owner], store.Glue{Owner: owner, Type: typ, RData: addr, TTL: 172800})
	}
	for _, x := range [][3]string{
		{"anycast.ns.nic.bo", "A", "204.61.216.48"}, {"anycast.ns.nic.bo", "AAAA", "2001:500:14:6048:ad::1"},
		{"ns2.nic.fr", "A", "192.93.0.4"}, {"ns2.nic.fr", "AAAA", "2001:660:3005:1::1:2"},
		{"ns.dns.br", "A", "200.160.0.5"}, {"ns.dns.br", "AAAA", "2001:12ff:0:a20::5"},
		{"ns.nic.bo", "A", "166.114.1.40"},
		{"a.dns.br", "A", "200.219.148.10"}, {"a.dns.br", "AAAA", "2001:12f8:6::10"},
		{"b.dns.br", "A", "200.189.41.10"}, {"b.dns.br", "AAAA", "2001:12f8:8::10"},
		{"c.dns.br", "A", "200.192.233.10"}, {"c.dns.br", "AAAA", "2001:12f8:a::10"},
		{"d.dns.br", "A", "200.219.154.10"}, {"d.dns.br", "AAAA", "2001:12f8:4::10"},
		{"e.dns.br", "A", "200.229.248.10"}, {"e.dns.br", "AAAA", "2001:12f8:2::10"},
		{"f.dns.br", "A", "200.219.159.10"}, {"f.dns.br", "AAAA", "2001:12f8:c::10"},
		{"a.zdnscloud.cn", "A", "203.99.24.1"}, {"b.zdnscloud.cn", "A", "203.99.25.1"},
		{"c.zdnscloud.com", "A", "203.99.26.1"}, {"d.zdnscloud.com", "A", "203.99.27.1"},
		{"e.zdnscloud.cn", "A", "203.119.82.1"}, {"e.zdnscloud.cn", "AAAA", "2401:8d00:15::1"},
		{"f.zdnscloud.cn", "A", "116.169.54.111"},
		{"i.zdnscloud.cn", "AAAA", "2401:8d00:1::1"}, {"j.zdnscloud.com", "AAAA", "2401:8d00:2::1"},
		{"a.dns.ripn.net", "A", "193.232.128.6"}, {"a.dns.ripn.net", "AAAA", "2001:678:17:0:193:232:128:6"},
		{"b.dns.ripn.net", "A", "194.85.252.62"}, {"b.dns.ripn.net", "AAAA", "2001:678:16:0:194:85:252:62"},
		{"c.tld-servers.ru", "A", "194.190.122.17"}, {"c.tld-servers.ru", "AAAA", "2a09:bd00:1:0:194:190:122:17"},
		{"d.dns.ripn.net", "A", "194.190.124.17"}, {"d.dns.ripn.net", "AAAA", "2001:678:18:0:194:190:124:17"},
		{"e.dns.ripn.net", "A", "193.232.142.17"}, {"e.dns.ripn.net", "AAAA", "2001:678:15:0:193:232:142:17"},
		{"f.dns.ripn.net", "A", "193.232.156.17"}, {"f.dns.ripn.net", "AAAA", "2001:678:14:0:193:232:156:17"},
	} {
		add(x[0], x[1], x[2])
	}
	return g
}()

type fakeStore struct {
	mu       sync.Mutex
	calls    int
	pingErr  error
	err      error  // devolvido pelas consultas de dados
	panicMsg string // faz as consultas de dados entrarem em pânico
	noData   bool   // Dataset e Job devolvem nil (antes da primeira carga)
	lastTLD  string
	badDS    bool // Delegation devolve um DS fora do formato
	manyTLDs int  // TLDs devolve esta quantidade de TLDs sintéticos com nomes longos
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
	return &store.Dataset{
		Version: "v1", AppliedAt: time.Unix(0, 0), URL: SourceURL, SHA256: "ab", Serial: new(testSerial),
		SOAMName: new("a.root-servers.net"), SOARName: new("nstld.verisign-grs.com"),
		SOARefresh: new(int64(1800)), SOARetry: new(int64(900)), SOAExpire: new(int64(604800)), SOAMinimum: new(int64(86400)),
		TLDs: new(len(sampleTLDs)), Records: new(195), RRSIGs: new(17),
	}, nil
}

func (f *fakeStore) Job(context.Context) (*store.Job, error) {
	if f.noData {
		return nil, nil
	}
	t := time.Unix(10, 0)
	return &store.Job{LastSyncAt: &t, LastCheckAt: &t, Consolidated: 0}, nil
}

func (f *fakeStore) TLDs(context.Context) ([]store.TLD, error) {
	if err := f.count(); err != nil {
		return nil, err
	}
	if f.manyTLDs > 0 {
		out := make([]store.TLD, f.manyTLDs)
		for i := range out {
			name := fmt.Sprintf("%063d", i)
			out[i] = store.TLD{TLD: name, Unicode: name, Nameservers: 1}
		}
		return out, nil
	}
	return slices.Clone(sampleTLDs), nil
}

func (f *fakeStore) Delegation(_ context.Context, tld string) (*store.Delegation, error) {
	if err := f.count(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.lastTLD = tld
	f.mu.Unlock()
	i := slices.IndexFunc(sampleTLDs, func(t store.TLD) bool { return t.TLD == tld })
	if i < 0 {
		return nil, store.ErrNotFound
	}
	d := &store.Delegation{TLD: sampleTLDs[i], Records: slices.Clone(sampleRecords[tld])}
	d.TLD.CreatedAt, d.TLD.UpdatedAt = time.Unix(100, 0), time.Unix(200, 0)
	for _, r := range d.Records {
		if r.Type == "NS" {
			d.Glue = append(d.Glue, sampleGlue[r.RData]...)
		}
	}
	if f.badDS {
		d.Records = append(d.Records, store.Record{Type: "DS", RData: "x 8 2 AB", TTL: 1})
	}
	return d, nil
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

func newTestAPI(t *testing.T, st *fakeStore, ready bool, c cache.Cache) *API {
	t.Helper()
	rip, err := realip.New(realip.DefaultTrustedProxies, realip.DefaultHeaders)
	if err != nil {
		t.Fatal(err)
	}
	snap := dataset.Snapshot{}
	if ready {
		snap = dataset.Snapshot{Version: "0192-v1", AppliedAt: time.Unix(0, 0), Serial: new(testSerial)}
	}
	return New(st, fixedView{snap}, c, rip, slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config{BasePath: base + "/", Version: "test", DBTimeout: time.Second})
}

func newAPIWith(t *testing.T, st *fakeStore, ready bool, c cache.Cache) http.Handler {
	return newTestAPI(t, st, ready, c).Handler()
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

func TestTLDs(t *testing.T) {
	st := &fakeStore{}
	h, c := newAPI(t, st, true)
	for _, p := range []string{base + "/tlds", base + "/v1/tlds"} {
		rec := do(h, "GET", p)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", p, rec.Code, rec.Body)
		}
		got := decode[TLDsResponse](t, rec)
		if got.Count != 4 || len(got.TLDs) != 4 || got.TLDs[3].TLD != "xn--p1ai" || got.TLDs[3].TLDUnicode != "рф" ||
			got.Zone.Serial == nil || *got.Zone.Serial != testSerial || got.Dataset.Version != "0192-v1" {
			t.Errorf("%s: %+v", p, got)
		}
	}
	if st.Calls() != 1 || !slices.Equal(c.keys(), []string{"badblock:api-rootzone:0192-v1:tlds"}) {
		t.Errorf("calls = %d, chaves = %v", st.Calls(), c.keys())
	}
	// Formato: zone primeiro, contagens de rootzone_tld em cada item.
	body := do(h, "GET", base+"/tlds").Body.String()
	if !strings.HasPrefix(body, `{"zone":{"serial":2026092901},"count":4,"tlds":[{"tld":"bo","tld_unicode":"bo","nameservers":4,"nameservers_ipv4":4,"nameservers_ipv6":3,"ds_records":0},`) ||
		!strings.HasSuffix(body, `"dataset":{"version":"0192-v1","updated_at":"1970-01-01T00:00:00Z"}}`+"\n") {
		t.Errorf("tlds = %s", body)
	}
}

func TestTLD(t *testing.T) {
	st := &fakeStore{}
	h, c := newAPI(t, st, true)
	// Caixa, ponto final, /v1: uma chave só, uma consulta só.
	for _, p := range []string{"/tld/br", "/tld/BR", "/tld/br.", "/tld/Br.", "/v1/tld/br"} {
		rec := do(h, "GET", base+p)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", p, rec.Code, rec.Body)
		}
		got := decode[TLDResponse](t, rec)
		if got.TLD != "br" || got.TLDUnicode != "br" || len(got.Nameservers) != 6 || len(got.DS) != 1 ||
			got.FirstSeen != "1970-01-01T00:01:40Z" || got.UpdatedAt != "1970-01-01T00:03:20Z" ||
			*got.Zone.Serial != testSerial || got.Dataset.Version != "0192-v1" {
			t.Errorf("%s: %+v", p, got)
		}
	}
	if st.Calls() != 1 || st.lastTLD != "br" || !slices.Equal(c.keys(), []string{"badblock:api-rootzone:0192-v1:tld:br"}) {
		t.Errorf("calls = %d, último %q, chaves = %v", st.Calls(), st.lastTLD, c.keys())
	}
	// Formato de um NS com glue e de um DS.
	body := do(h, "GET", base+"/tld/br").Body.String()
	for _, want := range []string{
		`{"tld":"br","tld_unicode":"br","nameservers":[{"name":"a.dns.br","ttl":172800,"ipv4":[{"address":"200.219.148.10","ttl":172800}],"ipv6":[{"address":"2001:12f8:6::10","ttl":172800}]},`,
		`"ds":[{"key_tag":38298,"algorithm":13,"digest_type":2,"digest":"9F2D4993F47B0F2751DE0007D70A2754EE532FE373761154D9EA7A8CB9D8EA18","ttl":86400}],`,
		`"zone":{"serial":2026092901},"dataset":{"version":"0192-v1",`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("br sem %s: %s", want, body)
		}
	}

	// Sem DS: lista vazia. Servidor sem AAAA: ipv6 vazia. Glue sob outro TLD.
	got := decode[TLDResponse](t, do(h, "GET", base+"/tld/bo"))
	if len(got.DS) != 0 || got.DS == nil || len(got.Nameservers) != 4 {
		t.Fatalf("bo = %+v", got)
	}
	nsByName := map[string]Nameserver{}
	for _, ns := range got.Nameservers {
		nsByName[ns.Name] = ns
	}
	if ns := nsByName["ns.nic.bo"]; len(ns.IPv4) != 1 || ns.IPv6 == nil || len(ns.IPv6) != 0 {
		t.Errorf("ns.nic.bo = %+v", ns)
	}
	if ns := nsByName["ns.dns.br"]; len(ns.IPv4) != 1 || ns.IPv4[0].Address != "200.160.0.5" || len(ns.IPv6) != 1 {
		t.Errorf("ns.dns.br = %+v", ns)
	}
	// A ordem dos NS é a do store (a do banco), não reordenada pela API.
	if got.Nameservers[1].Name != "ns2.nic.fr" {
		t.Errorf("ordem dos NS = %+v", got.Nameservers)
	}

	// 2 DS e servidores só com IPv4 ou só com IPv6.
	got = decode[TLDResponse](t, do(h, "GET", base+"/tld/top"))
	v4, v6 := 0, 0
	for _, ns := range got.Nameservers {
		if len(ns.IPv4) > 0 {
			v4++
		}
		if len(ns.IPv6) > 0 {
			v6++
		}
	}
	if len(got.DS) != 2 || got.DS[1].KeyTag != 41508 || got.DS[1].Algorithm != 13 || v4 != 6 || v6 != 3 {
		t.Errorf("top = %+v", got)
	}
}

func TestTLDUnicode(t *testing.T) {
	st := &fakeStore{}
	h, c := newAPI(t, st, true)
	for _, p := range []string{"/tld/xn--p1ai", "/tld/XN--P1AI.", "/tld/%D1%80%D1%84", "/tld/%D0%A0%D0%A4", "/tld/%D1%80%D1%84.", "/v1/tld/рф"} {
		rec := do(h, "GET", base+p)
		got := decode[TLDResponse](t, rec)
		if rec.Code != 200 || got.TLD != "xn--p1ai" || got.TLDUnicode != "рф" || len(got.Nameservers) != 6 {
			t.Errorf("%s: %d %s", p, rec.Code, rec.Body)
		}
	}
	if st.Calls() != 1 || !slices.Equal(c.keys(), []string{"badblock:api-rootzone:0192-v1:tld:xn--p1ai"}) {
		t.Errorf("calls = %d, chaves = %v", st.Calls(), c.keys())
	}
	// O JSON sai em UTF-8, sem escapar o Unicode.
	if body := do(h, "GET", base+"/tld/xn--p1ai").Body.String(); !strings.HasPrefix(body, `{"tld":"xn--p1ai","tld_unicode":"рф",`) {
		t.Errorf("рф = %s", body)
	}
}

func TestErrors(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, true)
	cases := map[string]int{
		base + "/tld/nada-disso":                           404,
		base + "/tld/xn--zz":                               404,
		base + "/tld/%D0%BE%D0%BD%D0%BB%D0%B0%D0%B9%D0%BD": 404, // онлайн: válido, fora do recorte
		base + "/tld/" + strings.Repeat("a", 63):           404,
		base + "/tld/" + strings.Repeat("a", 64):           400,
		base + "/tld/a.b":                                  400,
		base + "/tld/br..":                                 400,
		base + "/tld/b%20r":                                400,
		base + "/tld/b%00r":                                400,
		base + "/tld/b%FFr":                                400,
		base + "/tld/b*r":                                  400,
		base + "/tld/%E2%80%8B":                            400, // espaço de largura zero
		base + "/tld/%D1%80%D1%84!":                        400,
		base + "/tld/br/ns":                                404,
		base + "/tlds/br":                                  404,
		base + "/v2/tlds":                                  404,
		base + "/nada":                                     404,
		"/outra-coisa":                                     404,
		"/tlds":                                            404,
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
	rec := do(h, "GET", base+"/tld/nada-disso")
	if !strings.Contains(rec.Body.String(), `"message":"o TLD nada-disso não está delegado na zona raiz"`) {
		t.Errorf("404 = %s", rec.Body)
	}
	// A mensagem do 404 traz o nome normalizado, qualquer que seja a forma pedida.
	rec = do(h, "GET", base+"/tld/%D0%9E%D0%9D%D0%9B%D0%90%D0%99%D0%9D.")
	if !strings.Contains(rec.Body.String(), "o TLD xn--80asehdb não está delegado") {
		t.Errorf("404 Unicode = %s", rec.Body)
	}
	rec = do(h, "GET", base+"/tld/a.b")
	if !strings.Contains(rec.Body.String(), `"code":"bad_request","message":"TLD inválido: use um rótulo só`) {
		t.Errorf("400 = %s", rec.Body)
	}
	// Método não suportado numa rota existente.
	if rec := do(h, "DELETE", base+"/tld/br"); rec.Code != 404 {
		t.Errorf("DELETE = %d", rec.Code)
	}
	// "/tld/." é limpo pelo ServeMux (redirect para /tld/, que não existe).
	if rec := do(h, "GET", base+"/tld/."); rec.Code != http.StatusTemporaryRedirect && rec.Code != http.StatusMovedPermanently {
		t.Errorf("/tld/. = %d", rec.Code)
	}
	if rec := do(h, "GET", base+"/tld/"); rec.Code != 404 {
		t.Errorf("/tld/ = %d", rec.Code)
	}
}

func TestDatabaseErrors(t *testing.T) {
	st := &fakeStore{err: errors.New("conexão recusada")}
	h, c := newAPI(t, st, true)
	for _, p := range []string{base + "/tlds", base + "/tld/br"} {
		rec := do(h, "GET", p)
		if rec.Code != 503 || !strings.Contains(rec.Body.String(), "database_unavailable") {
			t.Errorf("%s: %d %s", p, rec.Code, rec.Body)
		}
	}
	st.err = fmt.Errorf("consulta: %w", context.DeadlineExceeded)
	for _, p := range []string{base + "/tlds", base + "/tld/br"} {
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
	if rec := do(h, "GET", base+"/tld/br"); rec.Code != 200 || rec.Header().Get("X-Cache") != "MISS" {
		t.Errorf("depois do erro = %d %s", rec.Code, rec.Header().Get("X-Cache"))
	}
	// 404 também não entra no cache.
	do(h, "GET", base+"/tld/nada-disso")
	if n := len(c.keys()); n != 1 {
		t.Errorf("chaves = %v", c.keys())
	}
	// Registro fora do formato do coletor: 500 JSON, fora do cache.
	st.badDS = true
	rec := do(h, "GET", base+"/tld/top")
	if rec.Code != 500 || !strings.Contains(rec.Body.String(), "internal_error") || len(c.keys()) != 1 {
		t.Errorf("DS inválido = %d %s, chaves %v", rec.Code, rec.Body, c.keys())
	}
}

func TestParseDS(t *testing.T) {
	ds, err := parseDS("38298 13 2 9F2D")
	if err != nil || ds != (DS{KeyTag: 38298, Algorithm: 13, DigestType: 2, Digest: "9F2D"}) {
		t.Errorf("DS = %+v, %v", ds, err)
	}
	for _, bad := range []string{"", "1 2 3", "1 2 3 AB CD", "65536 8 2 AB", "1 256 2 AB", "1 8 -2 AB", "x 8 2 AB"} {
		if _, err := parseDS(bad); err == nil {
			t.Errorf("parseDS(%q) deveria falhar", bad)
		}
	}
}

func TestPanicIsJSON500(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{panicMsg: "bug"}, true)
	rec := do(h, "GET", base+"/tld/br")
	if rec.Code != 500 || !strings.Contains(rec.Body.String(), "internal_error") {
		t.Errorf("panic = %d %s", rec.Code, rec.Body)
	}
}

func TestCacheAndETag(t *testing.T) {
	st := &fakeStore{}
	h, c := newAPI(t, st, true)

	first := do(h, "GET", base+"/tld/br")
	if first.Header().Get("X-Cache") != "MISS" {
		t.Errorf("primeira = %s", first.Header().Get("X-Cache"))
	}
	// Versionada e sem versão compartilham o cache (mesmo conteúdo).
	second := do(h, "GET", base+"/v1/tld/BR.")
	if second.Header().Get("X-Cache") != "HIT" || st.Calls() != 1 || second.Body.String() != first.Body.String() {
		t.Errorf("segunda = %s, calls = %d", second.Header().Get("X-Cache"), st.Calls())
	}
	if !slices.Equal(c.keys(), []string{"badblock:api-rootzone:0192-v1:tld:br"}) {
		t.Errorf("chaves = %v", c.keys())
	}
	etag := first.Header().Get("ETag")
	if etag != etagFor("0192-v1", "tld:br") || first.Header().Get("X-Dataset-Version") != "0192-v1" ||
		first.Header().Get("Cache-Control") != "public, max-age=300" || second.Header().Get("ETag") != etag {
		t.Fatalf("cabeçalhos = %v", first.Header())
	}
	for _, inm := range []string{etag, strings.TrimPrefix(etag, "W/"), `"outro", ` + etag, "*"} {
		rec := do(h, "GET", base+"/v1/tld/br", "If-None-Match", inm)
		if rec.Code != http.StatusNotModified || rec.Body.Len() != 0 || rec.Header().Get("ETag") != etag ||
			rec.Header().Get("X-Cache") != "HIT" || rec.Header().Get("Content-Type") != "" {
			t.Errorf("If-None-Match %s = %d %v", inm, rec.Code, rec.Header())
		}
	}
	if rec := do(h, "GET", base+"/tld/br", "If-None-Match", `W/"outro"`); rec.Code != 200 {
		t.Errorf("ETag diferente = %d", rec.Code)
	}
	if st.Calls() != 1 {
		t.Errorf("304 não deveria consultar o banco (calls = %d)", st.Calls())
	}
	// Cada consulta tem o próprio ETag; /tlds com e sem /v1 têm o mesmo.
	if do(h, "GET", base+"/tld/top").Header().Get("ETag") == etag {
		t.Error("ETag deveria depender da consulta")
	}
	a := do(h, "GET", base+"/tlds").Header().Get("ETag")
	b := do(h, "GET", base+"/v1/tlds").Header().Get("ETag")
	if a == "" || a != b || a != etagFor("0192-v1", "tlds") {
		t.Errorf("ETag de /tlds: %q != %q", a, b)
	}
	// Com a versão dos exemplos da spec, os ETags documentados.
	for key, want := range map[string]string{
		"tlds":         `W/"f60b39e5f95a1753"`,
		"tld:br":       `W/"c378da2e80b8654"`,
		"tld:xn--p1ai": `W/"8c99d0d875f78a75"`,
	} {
		if got := etagFor("01a0f073-f7ab-73d8-bf10-586563066827", key); got != want {
			t.Errorf("etagFor(%s) = %s, quero %s", key, got, want)
		}
	}
}

func TestHEAD(t *testing.T) {
	st := &fakeStore{}
	h, _ := newAPI(t, st, true)
	for _, p := range []string{base + "/tlds", base + "/tld/br", base + "/", base + "/meta"} {
		rec := do(h, "HEAD", p)
		if rec.Code != 200 {
			t.Errorf("HEAD %s = %d", p, rec.Code)
		}
	}
	rec := do(h, "HEAD", base+"/tld/br")
	etag := rec.Header().Get("ETag")
	if etag == "" || rec.Header().Get("X-Cache") != "HIT" {
		t.Errorf("HEAD sem cabeçalhos de dados: %v", rec.Header())
	}
	if rec := do(h, "HEAD", base+"/tld/br", "If-None-Match", etag); rec.Code != http.StatusNotModified {
		t.Errorf("HEAD com If-None-Match = %d", rec.Code)
	}
	if rec := do(h, "HEAD", base+"/tld/nada-disso"); rec.Code != 404 {
		t.Errorf("HEAD inexistente = %d", rec.Code)
	}
}

func TestBypassAndBigBody(t *testing.T) {
	h := newAPIWith(t, &fakeStore{}, true, cache.Noop{})
	if rec := do(h, "GET", base+"/tlds"); rec.Header().Get("X-Cache") != "BYPASS" {
		t.Errorf("sem cache = %s", rec.Header().Get("X-Cache"))
	}
	// Respostas acima de 8 MiB não vão para o cache.
	h, c := newAPI(t, &fakeStore{manyTLDs: 60000}, true)
	rec := do(h, "GET", base+"/tlds")
	if rec.Code != 200 || rec.Body.Len() <= maxCachedBody || len(c.keys()) != 0 || rec.Header().Get("X-Cache") != "MISS" {
		t.Errorf("corpo grande: %d, %d bytes, chaves %v", rec.Code, rec.Body.Len(), c.keys())
	}
}

func TestNotReady(t *testing.T) {
	st := &fakeStore{noData: true}
	h, _ := newAPI(t, st, false)
	for _, p := range []string{base + "/tlds", base + "/tld/br", base + "/v1/tld/рф"} {
		rec := do(h, "GET", p)
		if rec.Code != 503 || !strings.Contains(rec.Body.String(), `"dataset_not_ready","message":"a primeira sincronização do collector-rootzone ainda não terminou; tente em alguns minutos"`) {
			t.Errorf("%s sem dataset = %d %s", p, rec.Code, rec.Body)
		}
	}
	if st.Calls() != 0 {
		t.Errorf("sem dataset não deveria consultar o banco (calls = %d)", st.Calls())
	}
	// Validação vem antes da conferência dos dados.
	if rec := do(h, "GET", base+"/tld/a.b"); rec.Code != 400 {
		t.Errorf("inválido sem dataset = %d", rec.Code)
	}
	rec := do(h, "GET", base+"/status")
	s := decode[StatusResponse](t, rec)
	if rec.Code != 200 || s.Status != "starting" || !s.Success || s.Checks["dataset"] != "empty" {
		t.Errorf("status sem dataset = %d %+v", rec.Code, s)
	}
	rec = do(h, "GET", base+"/meta")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"dataset":null,"collector":null`) {
		t.Errorf("meta sem dados = %d %s", rec.Code, rec.Body)
	}
}

func TestStatus(t *testing.T) {
	st := &fakeStore{}
	h, c := newAPI(t, st, true)
	for _, m := range []string{"GET", "POST"} {
		for _, p := range []string{base + "/health", base + "/status"} {
			rec := do(h, m, p)
			s := decode[StatusResponse](t, rec)
			if rec.Code != 200 || !s.Success || s.Status != "ok" || s.Timestamp == "" || s.Message != "api-rootzone operacional" ||
				s.Checks["postgres"] != "ok" || s.Checks["valkey"] != "ok" || s.Checks["dataset"] != "ok" {
				t.Errorf("%s %s = %d %s", m, p, rec.Code, rec.Body)
			}
			if rec.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("%s %s sem no-store", m, p)
			}
		}
	}
	if rec := do(h, "GET", base+"/ping"); rec.Code != 200 || rec.Body.String() != "pong" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("ping = %d %q", rec.Code, rec.Body)
	}
	for _, p := range []string{"/v1/status", "/v1/health", "/v1/ping"} {
		if rec := do(h, "GET", base+p); rec.Code != 404 {
			t.Errorf("%s fica fora do versionamento: %d", p, rec.Code)
		}
	}

	c.pingErr = errors.New("valkey fora")
	rec := do(h, "GET", base+"/status")
	s := decode[StatusResponse](t, rec)
	if rec.Code != 200 || !s.Success || s.Status != "degraded" || s.Checks["valkey"] != "error" {
		t.Errorf("valkey fora = %d %s", rec.Code, rec.Body)
	}

	st.pingErr = errors.New("down")
	rec = do(h, "POST", base+"/health")
	s = decode[StatusResponse](t, rec)
	if rec.Code != 503 || s.Success || s.Status != "error" || s.Checks["postgres"] != "error" || s.Message != "PostgreSQL indisponível" {
		t.Errorf("postgres fora = %d %s", rec.Code, rec.Body)
	}

	// Cache desligado aparece como disabled.
	h = newAPIWith(t, &fakeStore{}, true, cache.Noop{})
	s = decode[StatusResponse](t, do(h, "GET", base+"/status"))
	if s.Status != "ok" || s.Checks["valkey"] != "disabled" {
		t.Errorf("sem cache = %+v", s)
	}
}

func TestCORSAndHeaders(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, true)
	rec := do(h, "OPTIONS", base+"/tld/br", "Origin", "https://exemplo.com", "Access-Control-Request-Method", "GET")
	if rec.Code != http.StatusNoContent || rec.Header().Get("Access-Control-Allow-Origin") != "*" ||
		rec.Header().Get("Access-Control-Allow-Methods") != "GET, HEAD, OPTIONS" ||
		rec.Header().Get("Access-Control-Allow-Headers") != "If-None-Match, Content-Type" ||
		rec.Header().Get("Access-Control-Max-Age") != "86400" {
		t.Errorf("preflight = %d %v", rec.Code, rec.Header())
	}
	rec = do(h, "GET", base+"/tld/br")
	hd := rec.Header()
	if hd.Get("Access-Control-Allow-Origin") != "*" || hd.Get("X-Content-Type-Options") != "nosniff" ||
		hd.Get("Referrer-Policy") != "no-referrer" || hd.Get("Server") != "badblock-api-rootzone/test" ||
		hd.Get("Access-Control-Expose-Headers") != "ETag, X-Cache, X-Dataset-Version" ||
		hd.Get("Content-Type") != "application/json; charset=utf-8" {
		t.Errorf("cabeçalhos = %v", hd)
	}
	if do(h, "GET", "/nada").Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("404 sem nosniff")
	}
}

func TestIndexMetaAndRedirect(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, true)
	want := []string{base + "/tlds", base + "/tld/{tld}", base + "/meta", base + "/status", base + "/openapi.yaml"}
	for _, p := range []string{base + "/", base + "/v1/"} {
		rec := do(h, "GET", p)
		idx := decode[IndexResponse](t, rec)
		if rec.Code != 200 || idx.App != "api-rootzone" || idx.BasePath != base || idx.Source != SourceURL ||
			!slices.Equal(idx.Versions, []string{"v1"}) || !slices.Equal(idx.Endpoints, want) ||
			rec.Header().Get("Cache-Control") != "public, max-age=300" || rec.Header().Get("ETag") != "" {
			t.Errorf("índice %s = %d %s", p, rec.Code, rec.Body)
		}
	}
	for _, p := range []string{base, base + "?x=1"} {
		if rec := do(h, "GET", p); rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != base+"/" {
			t.Errorf("redirect %s = %d %s", p, rec.Code, rec.Header().Get("Location"))
		}
	}
	for _, p := range []string{base + "/meta", base + "/v1/meta"} {
		rec := do(h, "GET", p)
		if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("X-Cache") != "" {
			t.Errorf("meta = %d %v", rec.Code, rec.Header())
		}
		body := rec.Body.String()
		for _, part := range []string{
			`"serial":2026092901,"soa":{"mname":"a.root-servers.net","rname":"nstld.verisign-grs.com","refresh":1800,"retry":900,"expire":604800,"minimum":86400},"tlds":4,"records":195,"rrsigs":17}`,
			`"collector":{"app":"collector-rootzone","last_sync_at":"1970-01-01T00:00:10Z","last_check_at":"1970-01-01T00:00:10Z","consolidated":false}`,
			`"source":"` + SourceURL + `"`,
		} {
			if !strings.Contains(body, part) {
				t.Errorf("meta sem %s: %s", part, body)
			}
		}
	}
}
