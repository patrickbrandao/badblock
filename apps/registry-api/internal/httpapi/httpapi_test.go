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

	"github.com/patrickbrandao/badblock/apps/registry-api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/registry-api/internal/realip"
	"github.com/patrickbrandao/badblock/apps/registry-api/internal/store"
)

// --- dublês ------------------------------------------------------------------

func sp(s string) *string { return &s }
func ip64(n int64) *int64 { return &n }
func bp(b bool) *bool     { return &b }

func date(s string) *time.Time {
	t, _ := time.Parse("2006-01-02", s)
	return &t
}

// fakeStore responde com um recorte fixo: o bloco da TMSoft, 10/8 e o ASN 61613.
type fakeStore struct {
	fail    error
	pingErr error
	calls   int
	mu      sync.Mutex
}

var (
	tmsoft = store.Prefix{
		Level: "rir", Prefix: netip.MustParsePrefix("45.171.60.0/22"), RIR: sp("lacnic"), Country: sp("BR"),
		Status: sp("allocated"), Registered: date("2019-02-11"), HolderID: ip64(7), HolderKey: sp("lacnic:258500"),
		HolderName: sp("TMSoft Solucoes em Informatica Ltda"), HolderNameSource: sp("nicbr"),
		HolderDocument: sp("08.030.063/0001-00"), NICBRASNs: []int64{61613},
		RDAPURL: sp("https://rdap.lacnic.net/rdap/ip/45.171.60.0/22"),
	}
	iana45 = store.Prefix{
		Level: "iana", Prefix: netip.MustParsePrefix("45.0.0.0/8"), RIR: sp("arin"), Status: sp("legacy"),
		Designation: sp("Administered by ARIN"),
	}
	special10 = store.Prefix{
		Level: "special", Prefix: netip.MustParsePrefix("10.0.0.0/8"), Designation: sp("Private-Use"),
		GloballyReachable: bp(false),
	}
	iana10 = store.Prefix{
		Level: "iana", Prefix: netip.MustParsePrefix("10.0.0.0/8"), Status: sp("reserved"),
		Designation: sp("IANA - Private Use"),
	}
)

func (f *fakeStore) hit() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.fail
}

func (f *fakeStore) Ping(context.Context) error { return f.pingErr }

func (f *fakeStore) Chain(_ context.Context, ip netip.Addr) ([]store.Prefix, error) {
	if err := f.hit(); err != nil {
		return nil, err
	}
	var out []store.Prefix
	for _, p := range []store.Prefix{tmsoft, special10, iana45, iana10} {
		if p.Prefix.Contains(ip) {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakeStore) Covering(ctx context.Context, p netip.Prefix) ([]store.Prefix, error) {
	return f.Chain(ctx, p.Addr())
}

func (f *fakeStore) Children(context.Context, netip.Prefix, int) ([]store.Prefix, error) {
	return nil, f.hit()
}

func (f *fakeStore) ASN(_ context.Context, asn int64) (*store.ASN, error) {
	if err := f.hit(); err != nil {
		return nil, err
	}
	if asn != 61613 {
		return nil, store.ErrNotFound
	}
	return &store.ASN{
		ASN: 61613, RIR: "lacnic", Country: sp("BR"), Status: "allocated", Registered: date("2023-05-05"),
		Name: sp("TMSoft Solucoes em Informatica Ltda"), NameSource: sp("nicbr"), Handle: sp("AS61613"),
		HolderID: ip64(7), HolderKey: sp("lacnic:258500"), HolderName: sp("TMSoft Solucoes em Informatica Ltda"),
		HolderDocument: sp("08.030.063/0001-00"),
	}, nil
}

func (f *fakeStore) ASNBlocks(_ context.Context, asn int64) ([]store.ASNBlock, error) {
	if asn >= 64512 && asn <= 65534 {
		return []store.ASNBlock{
			{Level: "iana", First: 64512, Last: 65534, Designation: "Reserved for Private Use"},
			{Level: "special", First: 64512, Last: 65534, Designation: "For private use"},
		}, nil
	}
	if asn == 61613 {
		return []store.ASNBlock{{Level: "iana", First: 61440, Last: 61951, RIR: sp("lacnic"), Designation: "Assigned by LACNIC"}}, nil
	}
	return nil, nil
}

func (f *fakeStore) ASNsByNumber(_ context.Context, asns []int64) ([]store.ASNBrief, error) {
	var out []store.ASNBrief
	for _, a := range asns {
		out = append(out, store.ASNBrief{ASN: a, Name: sp("TMSoft Solucoes em Informatica Ltda"), RIR: "lacnic", Status: "allocated"})
	}
	return out, nil
}

func (f *fakeStore) ASNsByHolder(context.Context, int64, int) ([]store.ASNBrief, error) {
	return []store.ASNBrief{{ASN: 61613, Name: sp("TMSoft Solucoes em Informatica Ltda"), RIR: "lacnic", Status: "allocated"}}, nil
}

func (f *fakeStore) PrefixesForASN(context.Context, int64, *int64, int) ([]store.LinkedPrefix, int, error) {
	return []store.LinkedPrefix{{Prefix: tmsoft.Prefix, Link: "nicbr", Status: sp("allocated"), Country: sp("BR")}}, 1, nil
}

func (f *fakeStore) Holder(_ context.Context, rir, id string) (*store.Holder, error) {
	if rir != "lacnic" || id != "258500" {
		return nil, store.ErrNotFound
	}
	return &store.Holder{ID: 7, RIR: "lacnic", OpaqueID: id, Key: "lacnic:258500", Name: sp("TMSoft")}, nil
}

func (f *fakeStore) HolderPrefixes(context.Context, int64, *netip.Prefix, int) ([]store.Prefix, error) {
	return []store.Prefix{tmsoft}, nil
}

var listed = []string{"45.171.60.0/23", "45.171.62.0/23", "200.192.152.0/22"}

func (f *fakeStore) ListPrefixes(_ context.Context, _ store.ListFilter, after *netip.Prefix, limit int, fn func(store.ListPrefix) error) error {
	if err := f.hit(); err != nil {
		return err
	}
	n := 0
	for _, s := range listed {
		p := netip.MustParsePrefix(s)
		if after != nil && p.Addr().Compare(after.Addr()) <= 0 {
			continue
		}
		if limit > 0 && n >= limit {
			break
		}
		n++
		if err := fn(store.ListPrefix{Prefix: p, RIR: sp("lacnic"), Country: sp("BR"), Status: sp("allocated")}); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeStore) ListASNs(_ context.Context, _ store.ListFilter, after int64, limit int, fn func(store.ASNBrief) error) error {
	for _, a := range []int64{1916, 28573, 61613} {
		if a <= after {
			continue
		}
		if err := fn(store.ASNBrief{ASN: a, RIR: "lacnic", Status: "allocated"}); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeStore) History(context.Context, string, string, int) ([]store.Change, error) {
	return []store.Change{{DatasetID: 2, Action: "update", Before: json.RawMessage(`{"name":"A"}`), After: json.RawMessage(`{"name":"B"}`)}}, nil
}

func (f *fakeStore) Sources(context.Context) ([]store.SourceStatus, error) {
	return []store.SourceStatus{{SourceID: "rir-lacnic", URL: sp("https://ftp.lacnic.net/...")}}, nil
}

type fixedData struct{ snap dataset.Snapshot }

func (d fixedData) Current() dataset.Snapshot { return d.snap }

// memCache é um cache em memória com o mesmo contrato do Valkey.
type memCache struct {
	mu      sync.Mutex
	data    map[string][]byte
	pingErr error
}

func (m *memCache) Get(_ context.Context, k string) ([]byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.data[k]
	return v, ok
}
func (m *memCache) Set(_ context.Context, k string, v []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[k] = v
}
func (m *memCache) Ping(context.Context) error { return m.pingErr }
func (m *memCache) Enabled() bool              { return true }

type harness struct {
	api   *API
	store *fakeStore
	cache *memCache
	h     http.Handler
}

func newHarness(t *testing.T, ready bool) *harness {
	t.Helper()
	snap := dataset.Snapshot{Exceptions: map[netip.Prefix]struct{}{}}
	if ready {
		snap = dataset.Snapshot{Version: 42, BuiltAt: time.Date(2026, 9, 28, 13, 0, 0, 0, time.UTC),
			Exceptions: map[netip.Prefix]struct{}{}}
	}
	rip, err := realip.New(realip.DefaultTrustedProxies, realip.DefaultHeaders)
	if err != nil {
		t.Fatal(err)
	}
	fs := &fakeStore{}
	mc := &memCache{data: map[string][]byte{}}
	api := New(fs, fixedData{snap}, mc, rip, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{Version: "test"})
	return &harness{api: api, store: fs, cache: mc, h: api.Handler()}
}

func (h *harness) get(t *testing.T, path string, headers ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	req.RemoteAddr = "172.18.0.2:40000" // o Traefik
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	h.h.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("json inválido (%d): %s", rec.Code, rec.Body.String())
	}
	return m
}

// --- testes ------------------------------------------------------------------

func TestIPLookup(t *testing.T) {
	h := newHarness(t, true)
	rec := h.get(t, "/v1/ip/45.171.60.1")
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var res IPResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.IP != "45.171.60.1" || res.Prefix.CIDR != "45.171.60.0/22" || *res.Prefix.RIR != "LACNIC" {
		t.Errorf("prefixo = %+v", res.Prefix)
	}
	if res.Holder == nil || res.Holder.ID != "lacnic:258500" || *res.Holder.Document != "08.030.063/0001-00" {
		t.Errorf("titular = %+v", res.Holder)
	}
	if len(res.ASNs) != 1 || res.ASNs[0].ASN != 61613 || res.ASNs[0].Link != "nicbr" {
		t.Errorf("ASNs = %+v (o do NIC.br não deveria repetir como holder)", res.ASNs)
	}
	if len(res.Chain) != 2 || res.Chain[0].Level != "iana" || res.Chain[1].Level != "rir" {
		t.Errorf("cadeia deveria ir do menos para o mais específico: %+v", res.Chain)
	}
	if res.Flags.Bogon || res.Flags.Special != nil {
		t.Errorf("flags = %+v", res.Flags)
	}
	if res.Dataset.Version != 42 || rec.Header().Get("X-Dataset-Version") != "42" {
		t.Errorf("dataset = %+v", res.Dataset)
	}
	if !strings.Contains(rec.Body.String(), `"special":null`) {
		t.Error("campos opcionais devem sair como null")
	}
}

func TestIPCacheByBucketAndETag(t *testing.T) {
	h := newHarness(t, true)
	first := h.get(t, "/v1/ip/45.171.60.1")
	if first.Header().Get("X-Cache") != "MISS" {
		t.Errorf("primeira consulta = %s", first.Header().Get("X-Cache"))
	}
	calls := h.store.calls
	second := h.get(t, "/v1/ip/45.171.60.99")
	if second.Header().Get("X-Cache") != "HIT" || h.store.calls != calls {
		t.Errorf("mesmo /24 deveria vir do cache sem consultar o banco")
	}
	if ip := decode(t, second)["ip"]; ip != "45.171.60.99" {
		t.Errorf("a resposta do cache deve trazer o IP consultado, veio %v", ip)
	}
	etag := second.Header().Get("ETag")
	if rec := h.get(t, "/v1/ip/45.171.60.99", "If-None-Match", etag); rec.Code != http.StatusNotModified {
		t.Errorf("If-None-Match = %d", rec.Code)
	}
}

func TestIPBogonAndErrors(t *testing.T) {
	h := newHarness(t, true)
	m := decode(t, h.get(t, "/v1/ip/10.1.2.3"))
	flags := m["flags"].(map[string]any)
	if flags["bogon"] != true || flags["special"] != "Private-Use" {
		t.Errorf("10.1.2.3 flags = %v", flags)
	}
	m = decode(t, h.get(t, "/v1/ip/4000::1"))
	if m["prefix"] != nil || m["flags"].(map[string]any)["bogon"] != true {
		t.Errorf("IP fora de qualquer bloco = %v", m)
	}
	for _, bad := range []string{"/v1/ip/999.1.1.1", "/v1/ip/fe80::1%25eth0", "/v1/ip/abc"} {
		if rec := h.get(t, bad); rec.Code != 400 || decode(t, rec)["error"].(map[string]any)["code"] != "invalid_ip" {
			t.Errorf("%s = %d %s", bad, rec.Code, rec.Body)
		}
	}
	h.store.fail = errors.New("conexão recusada")
	if rec := h.get(t, "/v1/ip/200.1.1.1"); rec.Code != 503 {
		t.Errorf("banco fora = %d", rec.Code)
	}
}

func TestMyIPUsesTrustedHeaders(t *testing.T) {
	h := newHarness(t, true)
	rec := h.get(t, "/v1/ip", "X-Forwarded-For", "8.8.8.8, 45.171.60.7")
	m := decode(t, rec)
	if m["ip"] != "45.171.60.7" {
		t.Errorf("ip = %v (esperado o mais à direita não confiável)", m["ip"])
	}
	if rec.Header().Get("X-Client-IP-Source") != "x-forwarded-for" || !strings.Contains(rec.Header().Get("Cache-Control"), "no-store") {
		t.Errorf("headers = %v", rec.Header())
	}

	// Cliente direto (sem passar pelo Traefik) não consegue forjar o IP.
	req := httptest.NewRequest("GET", "/v1/ip", nil)
	req.RemoteAddr = "200.1.2.3:5555"
	req.Header.Set("X-Forwarded-For", "45.171.60.7")
	rec = httptest.NewRecorder()
	h.h.ServeHTTP(rec, req)
	if ip := decode(t, rec)["ip"]; ip != "200.1.2.3" {
		t.Errorf("IP forjado aceito: %v", ip)
	}
}

func TestDatasetNotReady(t *testing.T) {
	h := newHarness(t, false)
	for _, p := range []string{"/v1/ip/1.1.1.1", "/v1/asn/1", "/v1/country/BR/prefixes"} {
		rec := h.get(t, p)
		if rec.Code != 503 || decode(t, rec)["error"].(map[string]any)["code"] != "dataset_not_ready" {
			t.Errorf("%s = %d %s", p, rec.Code, rec.Body)
		}
	}
}

func TestASN(t *testing.T) {
	h := newHarness(t, true)
	m := decode(t, h.get(t, "/v1/asn/AS61613"))
	if m["name"] != "TMSoft Solucoes em Informatica Ltda" || m["rir"] != "LACNIC" || m["prefixes_total"].(float64) != 1 {
		t.Errorf("AS61613 = %v", m)
	}
	m = decode(t, h.get(t, "/v1/asn/64512"))
	if m["status"] != nil || m["flags"].(map[string]any)["bogon"] != true || len(m["chain"].([]any)) != 2 {
		t.Errorf("AS64512 = %v", m)
	}
	if rec := h.get(t, "/v1/asn/1"); rec.Code != 404 {
		t.Errorf("ASN sem registro nem bloco = %d", rec.Code)
	}
	if rec := h.get(t, "/v1/asn/4294967296"); rec.Code != 400 {
		t.Errorf("ASN fora de 32 bits = %d", rec.Code)
	}
}

func TestLegacyASN(t *testing.T) {
	h := newHarness(t, true)
	rec := h.get(t, "/asn/61613")
	want := `{"asn":61613,"name":"TMSoft Solucoes em Informatica Ltda","cid":"08.030.063/0001-00","country":"BR","rir":"LACNIC","status":"ALLOCATED","score":"0"}`
	if strings.TrimSpace(rec.Body.String()) != want {
		t.Errorf("legado = %s", rec.Body)
	}
	rec = h.get(t, "/asn/1")
	if rec.Code != 404 || strings.TrimSpace(rec.Body.String()) != "{}" {
		t.Errorf("legado não encontrado = %d %s", rec.Code, rec.Body)
	}
}

func TestListsPaginationAndText(t *testing.T) {
	h := newHarness(t, true)
	m := decode(t, h.get(t, "/v1/country/br/prefixes?limit=2"))
	items := m["items"].([]any)
	if len(items) != 2 || m["next_cursor"] == nil {
		t.Fatalf("primeira página = %v", m)
	}
	m2 := decode(t, h.get(t, "/v1/country/BR/prefixes?limit=2&cursor="+m["next_cursor"].(string)))
	if items2 := m2["items"].([]any); len(items2) != 1 || m2["next_cursor"] != nil {
		t.Errorf("segunda página = %v", m2)
	}

	rec := h.get(t, "/v1/country/BR/prefixes?format=txt&aggregate=true")
	if got := rec.Body.String(); got != "45.171.60.0/22\n200.192.152.0/22\n" || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") {
		t.Errorf("txt agregado = %q", got)
	}
	if again := h.get(t, "/v1/country/BR/prefixes?aggregate=true&format=txt"); again.Header().Get("X-Cache") != "HIT" {
		t.Error("mesma consulta com parâmetros em outra ordem deveria vir do cache")
	}
	if rec := h.get(t, "/v1/country/BR/asns?format=txt"); rec.Body.String() != "1916\n28573\n61613\n" {
		t.Errorf("ASNs em txt = %q", rec.Body)
	}
	for _, bad := range []string{
		"/v1/country/BRA/prefixes", "/v1/country/BR/prefixes?status=foo", "/v1/country/BR/prefixes?family=5",
		"/v1/country/BR/prefixes?limit=0", "/v1/country/BR/prefixes?cursor=@@", "/v1/rir/xyz/asns",
		"/v1/country/BR/asns?family=4",
	} {
		if rec := h.get(t, bad); rec.Code != 400 {
			t.Errorf("%s = %d", bad, rec.Code)
		}
	}
}

func TestHolderHistoryAndMeta(t *testing.T) {
	h := newHarness(t, true)
	m := decode(t, h.get(t, "/v1/holder/LACNIC/258500"))
	if m["id"] != "lacnic:258500" || len(m["prefixes"].([]any)) != 1 {
		t.Errorf("titular = %v", m)
	}
	if rec := h.get(t, "/v1/holder/lacnic/999"); rec.Code != 404 {
		t.Errorf("titular inexistente = %d", rec.Code)
	}
	m = decode(t, h.get(t, "/v1/asn/61613/history"))
	if ev := m["events"].([]any); len(ev) != 1 || ev[0].(map[string]any)["action"] != "update" {
		t.Errorf("histórico = %v", m)
	}
	if rec := h.get(t, "/v1/prefix/45.171.61.0/22/history"); rec.Code != 200 || decode(t, rec)["key"] != "45.171.60.0/22" {
		t.Errorf("histórico de prefixo deveria normalizar a chave: %s", rec.Body)
	}
	if rec := h.get(t, "/v1/meta/sources"); rec.Code != 200 {
		t.Errorf("fontes = %d", rec.Code)
	}
	if rec := h.get(t, "/openapi.yaml"); rec.Code != 200 || !strings.Contains(rec.Body.String(), "openapi: 3.1.0") {
		t.Error("openapi.yaml")
	}
	if rec := h.get(t, "/docs"); rec.Code != 200 || !strings.Contains(rec.Body.String(), "/openapi.yaml") {
		t.Error("docs")
	}
	if rec := h.get(t, "/nao-existe"); rec.Code != 404 || decode(t, rec)["error"] == nil {
		t.Error("rota inexistente deveria responder JSON 404")
	}
	if rec := h.get(t, "/ping"); rec.Body.String() != "pong" {
		t.Error("ping")
	}
}

func TestStatus(t *testing.T) {
	h := newHarness(t, true)
	m := decode(t, h.get(t, "/status"))
	if m["status"] != "online" || m["success"].(float64) != 1 || m["message"] != "BadBlock Registry API" {
		t.Errorf("status = %v", m)
	}
	h.cache.pingErr = errors.New("valkey fora")
	rec := h.get(t, "/health")
	if m := decode(t, rec); rec.Code != 200 || m["status"] != "degraded" || m["success"].(float64) != 1 {
		t.Errorf("cache fora deveria ser degraded com 200: %d %v", rec.Code, m)
	}
	h.store.pingErr = errors.New("pg fora")
	rec = h.get(t, "/status")
	if m := decode(t, rec); rec.Code != 503 || m["status"] != "offline" || m["success"].(float64) != 0 {
		t.Errorf("banco fora deveria ser offline com 503: %d %v", rec.Code, m)
	}
	req := httptest.NewRequest("POST", "/status", nil)
	rec = httptest.NewRecorder()
	h.h.ServeHTTP(rec, req)
	if rec.Code != 503 {
		t.Errorf("POST /status = %d", rec.Code)
	}
}

func TestCORSPreflight(t *testing.T) {
	h := newHarness(t, true)
	req := httptest.NewRequest("OPTIONS", "/v1/ip/1.1.1.1", nil)
	rec := httptest.NewRecorder()
	h.h.ServeHTTP(rec, req)
	if rec.Code != 204 || rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("preflight = %d %v", rec.Code, rec.Header())
	}
}
