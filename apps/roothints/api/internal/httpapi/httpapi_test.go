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
	"net/netip"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/patrickbrandao/badblock/apps/roothints/api/internal/cache"
	"github.com/patrickbrandao/badblock/apps/roothints/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/roothints/api/internal/realip"
	"github.com/patrickbrandao/badblock/apps/roothints/api/internal/store"
)

// base é o caminho de base dos testes (a API recebe "/roothints/" e normaliza).
const base = "/roothints"

// testVersion é a versão do dataset nos testes.
const testVersion = "0192-v1"

// sample são os 13 servidores do named.root de 2026-09-30, como o
// collector-roothints os grava (fonte.md, fatos medidos).
var sample = func() []store.Server {
	rows := []struct{ letter, v4, v6, note string }{
		{"a", "198.41.0.4", "2001:503:ba3e::2:30", "FORMERLY NS.INTERNIC.NET"},
		{"b", "170.247.170.2", "2801:1b8:10::b", "FORMERLY NS1.ISI.EDU"},
		{"c", "192.33.4.12", "2001:500:2::c", "FORMERLY C.PSI.NET"},
		{"d", "199.7.91.13", "2001:500:2d::d", "FORMERLY TERP.UMD.EDU"},
		{"e", "192.203.230.10", "2001:500:a8::e", "FORMERLY NS.NASA.GOV"},
		{"f", "192.5.5.241", "2001:500:2f::f", "FORMERLY NS.ISC.ORG"},
		{"g", "192.112.36.4", "2001:500:12::d0d", "FORMERLY NS.NIC.DDN.MIL"},
		{"h", "198.97.190.53", "2001:500:1::53", "FORMERLY AOS.ARL.ARMY.MIL"},
		{"i", "192.36.148.17", "2001:7fe::53", "FORMERLY NIC.NORDU.NET"},
		{"j", "192.58.128.30", "2001:503:c27::2:30", "OPERATED BY VERISIGN, INC."},
		{"k", "193.0.14.129", "2001:7fd::1", "OPERATED BY RIPE NCC"},
		{"l", "199.7.83.42", "2001:500:9f::42", "OPERATED BY ICANN"},
		{"m", "202.12.27.33", "2001:dc3::35", "OPERATED BY WIDE"},
	}
	var out []store.Server
	for _, r := range rows {
		out = append(out, store.Server{
			Name: r.letter + ".root-servers.net", Letter: r.letter,
			IPv4: new(netip.MustParseAddr(r.v4)), IPv6: new(netip.MustParseAddr(r.v6)),
			NSTTL: 3600000, IPv4TTL: new(3600000), IPv6TTL: new(3600000), Note: new(r.note),
			CreatedAt: time.Unix(20, 0), UpdatedAt: time.Unix(30, 0),
		})
	}
	return out
}()

type fakeStore struct {
	mu       sync.Mutex
	calls    int
	pingErr  error
	err      error  // devolvido pelas consultas de dados
	panicMsg string // faz as consultas de dados entrarem em pânico
	noData   bool   // Dataset e Job devolvem nil (antes da primeira carga)
	metaErr  error  // devolvido por Dataset (o /meta lê direto do banco)
	servers  []store.Server
	lastKey  string // letra recebida por ServerByLetter
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

func (f *fakeStore) rows() []store.Server {
	if f.servers != nil {
		return f.servers
	}
	return sample
}

func (f *fakeStore) Ping(context.Context) error { return f.pingErr }

func (f *fakeStore) Dataset(context.Context) (*store.Dataset, error) {
	if f.metaErr != nil {
		return nil, f.metaErr
	}
	if f.noData {
		return nil, nil
	}
	day := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	return &store.Dataset{Version: "v1", AppliedAt: time.Unix(0, 0), URL: SourceURL, MD5: "d0", SHA256: "18",
		LastUpdate: &day, ZoneSerial: new(int64(2026092401)), Servers: len(f.rows())}, nil
}

func (f *fakeStore) Job(context.Context) (*store.Job, error) {
	if f.noData {
		return nil, nil
	}
	t := time.Unix(10, 0)
	return &store.Job{LastSyncAt: &t, LastCheckAt: &t, Consolidated: 0}, nil
}

func (f *fakeStore) Servers(context.Context) ([]store.Server, error) {
	if err := f.count(); err != nil {
		return nil, err
	}
	return f.rows(), nil
}

func (f *fakeStore) ServerByLetter(_ context.Context, letter string) (*store.Server, error) {
	if err := f.count(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.lastKey = letter
	f.mu.Unlock()
	for _, s := range f.rows() {
		if s.Letter == letter {
			return &s, nil
		}
	}
	return nil, store.ErrNotFound
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

func readySnapshot() dataset.Snapshot {
	return dataset.Snapshot{Version: testVersion, AppliedAt: time.Unix(0, 0), LastUpdate: "2026-09-24",
		ZoneSerial: new(int64(2026092401))}
}

func newTestAPIWithSnap(t *testing.T, st *fakeStore, snap dataset.Snapshot, c cache.Cache) *API {
	t.Helper()
	rip, err := realip.New(realip.DefaultTrustedProxies, realip.DefaultHeaders)
	if err != nil {
		t.Fatal(err)
	}
	return New(st, fixedView{snap}, c, rip, slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config{BasePath: base + "/", Version: "test", DBTimeout: time.Second})
}

func newTestAPI(t *testing.T, st *fakeStore, ready bool, c cache.Cache) *API {
	t.Helper()
	snap := dataset.Snapshot{}
	if ready {
		snap = readySnapshot()
	}
	return newTestAPIWithSnap(t, st, snap, c)
}

func newAPIWith(t *testing.T, st *fakeStore, ready bool, c cache.Cache) http.Handler {
	t.Helper()
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

func TestServers(t *testing.T) {
	st := &fakeStore{}
	h, c := newAPI(t, st, true)
	for _, p := range []string{base + "/servers", base + "/v1/servers"} {
		rec := do(h, "GET", p)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", p, rec.Code, rec.Body)
		}
		got := decode[ServersResponse](t, rec)
		if got.Count != 13 || len(got.Servers) != 13 || got.Dataset.Version != testVersion ||
			got.Dataset.UpdatedAt != "1970-01-01T00:00:00Z" || *got.Source.LastUpdate != "2026-09-24" ||
			*got.Source.ZoneSerial != 2026092401 {
			t.Fatalf("%s: %s", p, rec.Body)
		}
		for i, s := range got.Servers {
			if s.Letter != string(rune('a'+i)) || s.Name != s.Letter+".root-servers.net" {
				t.Errorf("ordem: posição %d = %+v", i, s)
			}
		}
	}
	// Forma exata do primeiro item: endereços sem máscara, TTLs e a nota.
	rec := do(h, "GET", base+"/servers")
	want := `{"source":{"last_update":"2026-09-24","zone_serial":2026092401},"count":13,"servers":[` +
		`{"name":"a.root-servers.net","letter":"a","ipv4":"198.41.0.4","ipv6":"2001:503:ba3e::2:30",` +
		`"ns_ttl":3600000,"ipv4_ttl":3600000,"ipv6_ttl":3600000,"note":"FORMERLY NS.INTERNIC.NET"},`
	if !strings.HasPrefix(rec.Body.String(), want) {
		t.Errorf("corpo = %s", rec.Body)
	}
	if !strings.HasSuffix(rec.Body.String(), `"dataset":{"version":"0192-v1","updated_at":"1970-01-01T00:00:00Z"}}`+"\n") {
		t.Errorf("fim do corpo = %s", rec.Body)
	}
	// Com e sem /v1: uma chave só, uma consulta só.
	if st.Calls() != 1 || !slices.Equal(c.keys(), []string{"badblock:api-roothints:0192-v1:servers"}) {
		t.Errorf("calls = %d, chaves = %v", st.Calls(), c.keys())
	}
}

func TestServer(t *testing.T) {
	st := &fakeStore{}
	h, c := newAPI(t, st, true)
	for _, p := range []string{"/server/k", "/server/K", "/server/k.root-servers.net", "/server/K.ROOT-SERVERS.NET.",
		"/v1/server/K.Root-Servers.Net", "/v1/server/k.root-servers.net."} {
		rec := do(h, "GET", base+p)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", p, rec.Code, rec.Body)
		}
		got := decode[ServerResponse](t, rec)
		if got.Name != "k.root-servers.net" || got.Letter != "k" || *got.IPv4 != "193.0.14.129" ||
			*got.IPv6 != "2001:7fd::1" || got.NSTTL != 3600000 || *got.IPv4TTL != 3600000 || *got.IPv6TTL != 3600000 ||
			*got.Note != "OPERATED BY RIPE NCC" || got.FirstSeen != "1970-01-01T00:00:20Z" ||
			got.UpdatedAt != "1970-01-01T00:00:30Z" || *got.Source.ZoneSerial != 2026092401 ||
			got.Dataset.Version != testVersion {
			t.Errorf("%s: %s", p, rec.Body)
		}
	}
	// Letra e nome, em qualquer caixa e com /v1, caem numa chave só.
	if st.Calls() != 1 || st.lastKey != "k" || !slices.Equal(c.keys(), []string{"badblock:api-roothints:0192-v1:server:k"}) {
		t.Errorf("calls = %d, store recebeu %q, chaves = %v", st.Calls(), st.lastKey, c.keys())
	}
	// Formato: os campos do item da lista, depois as datas, source e dataset.
	rec := do(h, "GET", base+"/server/a")
	want := `{"name":"a.root-servers.net","letter":"a","ipv4":"198.41.0.4","ipv6":"2001:503:ba3e::2:30",` +
		`"ns_ttl":3600000,"ipv4_ttl":3600000,"ipv6_ttl":3600000,"note":"FORMERLY NS.INTERNIC.NET",` +
		`"first_seen":"1970-01-01T00:00:20Z","updated_at":"1970-01-01T00:00:30Z",` +
		`"source":{"last_update":"2026-09-24","zone_serial":2026092401},` +
		`"dataset":{"version":"0192-v1","updated_at":"1970-01-01T00:00:00Z"}}` + "\n"
	if rec.Body.String() != want {
		t.Errorf("corpo = %s", rec.Body)
	}
	// Letra válida que não existe: 404 com o nome, e não entra no cache.
	for _, p := range []string{"/server/n", "/server/Z.ROOT-SERVERS.NET."} {
		rec := do(h, "GET", base+p)
		if rec.Code != 404 || !strings.Contains(rec.Body.String(), ".root-servers.net não consta no named.root da InterNIC") {
			t.Errorf("%s: %d %s", p, rec.Code, rec.Body)
		}
	}
	rec = do(h, "GET", base+"/server/n")
	if msg := decode[errorBody](t, rec).Error.Message; msg != "o servidor raiz n.root-servers.net não consta no named.root da InterNIC" {
		t.Errorf("mensagem do 404 = %q", msg)
	}
	if len(c.keys()) != 2 {
		t.Errorf("404 entrou no cache: %v", c.keys())
	}
}

func TestNormalizeServer(t *testing.T) {
	cases := map[string]string{
		"a": "a", "M": "m", "z": "z",
		"a.root-servers.net": "a", "A.ROOT-SERVERS.NET": "a", "a.root-servers.net.": "a",
		"M.Root-Servers.Net.": "m", "n.root-servers.net": "n",
		// inválidos
		"": "", "a.": "", "ab": "", "1": "", "-": "", ".": "", "é": "",
		"K":                             "", // sinal de kelvin: ToLower daria "k"
		"K.root-servers.net":            "",
		"a.root-servers.net..":          "",
		"a.root-servers.net ":           "",
		" a":                            "",
		"ab.root-servers.net":           "",
		".root-servers.net":             "",
		"..root-servers.net":            "",
		"a.root-servers.org":            "",
		"a.root-servers.net.example":    "",
		"root-servers.net":              "",
		"a.b.root-servers.net":          "",
		"1.root-servers.net":            "",
		"a%2eroot-servers.net":          "",
		"a.root-servers.net\x00":        "",
		strings.Repeat("a", 300):        "",
		"a.root-servers.net." + "extra": "",
	}
	for in, want := range cases {
		got, ok := normalizeServer(in)
		if ok != (want != "") || got != want {
			t.Errorf("%q → %q %v, quero %q", in, got, ok, want)
		}
	}
}

// Servidor sem uma família ou sem comentário (o arquivo atual tem todos): os
// campos saem null, não "" nem 0.
func TestNullFields(t *testing.T) {
	m := sample[12]
	m.IPv6, m.IPv6TTL, m.Note = nil, nil, nil
	st := &fakeStore{servers: append(slices.Clone(sample[:12]), m)}
	h, _ := newAPI(t, st, true)
	want := `"ipv4":"202.12.27.33","ipv6":null,"ns_ttl":3600000,"ipv4_ttl":3600000,"ipv6_ttl":null,"note":null`
	for _, p := range []string{"/server/m", "/servers"} {
		if rec := do(h, "GET", base+p); rec.Code != 200 || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("%s = %d %s", p, rec.Code, rec.Body)
		}
	}
	// Linha aplicada sem cabeçalho (fora do padrão do coletor): source null.
	h = newTestAPIWithSnap(t, &fakeStore{}, dataset.Snapshot{Version: testVersion}, &memCache{m: map[string][]byte{}}).Handler()
	if rec := do(h, "GET", base+"/servers"); !strings.HasPrefix(rec.Body.String(), `{"source":{"last_update":null,"zone_serial":null},`) {
		t.Errorf("source sem cabeçalho = %s", rec.Body)
	}
}

func TestErrors(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, true)
	cases := map[string]int{
		"/server/n":                           404,
		"/server/z.root-servers.net":          404,
		"/server/ab":                          400,
		"/server/1":                           400,
		"/server/a.":                          400,
		"/server/a.root-servers.net..":        400,
		"/server/a.example.com":               400,
		"/server/%E2%84%AA":                   400, // sinal de kelvin
		"/server/a%00":                        400,
		"/server/%FF":                         400,
		"/server/" + strings.Repeat("a", 256): 400,
		"/server/":                            404,
		"/server/a/b":                         404,
		"/servers/a":                          404,
		"/v2/servers":                         404,
		"/nada":                               404,
		"/v1/openapi.yaml":                    404,
	}
	for p, want := range cases {
		rec := do(h, "GET", base+p)
		if rec.Code != want {
			t.Errorf("%s: %d, quero %d (%s)", p, rec.Code, want, rec.Body)
		}
		if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") || rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: erro deveria ser JSON sem cache (%v)", p, rec.Header())
		}
		e := decode[errorBody](t, rec)
		if e.Error.Code == "" || e.Error.Message == "" {
			t.Errorf("%s: corpo de erro = %s", p, rec.Body)
		}
		if want == 400 && e.Error.Message != "servidor inválido: use a letra (ex.: a) ou o nome (ex.: a.root-servers.net), sem diferenciar maiúsculas" {
			t.Errorf("%s: mensagem = %q", p, e.Error.Message)
		}
	}
	for _, p := range []string{"/outra-coisa", "/servers", "/roothintsx/servers"} {
		if rec := do(h, "GET", p); rec.Code != 404 || decode[errorBody](t, rec).Error.Message != "rota inexistente; veja /roothints/" {
			t.Errorf("%s = %d %s", p, rec.Code, rec.Body)
		}
	}
	// Método que a rota não aceita cai no 404 do catch-all.
	if rec := do(h, "DELETE", base+"/servers"); rec.Code != 404 {
		t.Errorf("DELETE = %d", rec.Code)
	}
}

func TestDatabaseErrors(t *testing.T) {
	st := &fakeStore{err: errors.New("conexão recusada")}
	h, c := newAPI(t, st, true)
	for _, p := range []string{"/servers", "/server/a"} {
		rec := do(h, "GET", base+p)
		if rec.Code != 503 || decode[errorBody](t, rec).Error.Message != "banco de dados indisponível" ||
			!strings.Contains(rec.Body.String(), "database_unavailable") {
			t.Errorf("%s: %d %s", p, rec.Code, rec.Body)
		}
	}
	st.err = fmt.Errorf("consulta: %w", context.DeadlineExceeded)
	for _, p := range []string{"/servers", "/server/a"} {
		rec := do(h, "GET", base+p)
		if rec.Code != 504 || !strings.Contains(rec.Body.String(), `"timeout"`) {
			t.Errorf("%s: %d %s", p, rec.Code, rec.Body)
		}
	}
	// Erros não entram no cache: quando o banco volta, a resposta é calculada.
	st.err = nil
	if len(c.keys()) != 0 {
		t.Errorf("erro foi para o cache: %v", c.keys())
	}
	if rec := do(h, "GET", base+"/servers"); rec.Code != 200 || rec.Header().Get("X-Cache") != "MISS" {
		t.Errorf("depois do erro = %d %s", rec.Code, rec.Header().Get("X-Cache"))
	}
}

func TestMetaDatabaseError(t *testing.T) {
	st := &fakeStore{metaErr: errors.New("conexão recusada")}
	h, _ := newAPI(t, st, true)
	rec := do(h, "GET", base+"/meta")
	if rec.Code != 503 || decode[errorBody](t, rec).Error.Code != "database_unavailable" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("meta com banco fora = %d %s", rec.Code, rec.Body)
	}
	st.metaErr = fmt.Errorf("consulta: %w", context.DeadlineExceeded)
	if rec := do(h, "GET", base+"/meta"); rec.Code != 504 {
		t.Errorf("meta lenta = %d %s", rec.Code, rec.Body)
	}
}

func TestPanicIsJSON500(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{panicMsg: "bug"}, true)
	for _, p := range []string{"/servers", "/server/a"} {
		rec := do(h, "GET", base+p)
		if rec.Code != 500 || !strings.Contains(rec.Body.String(), "internal_error") {
			t.Errorf("panic %s = %d %s", p, rec.Code, rec.Body)
		}
	}
}

func TestCacheAndETag(t *testing.T) {
	st := &fakeStore{}
	h, _ := newAPI(t, st, true)

	first := do(h, "GET", base+"/servers")
	if first.Header().Get("X-Cache") != "MISS" {
		t.Errorf("primeira = %s", first.Header().Get("X-Cache"))
	}
	second := do(h, "GET", base+"/v1/servers")
	if second.Header().Get("X-Cache") != "HIT" || st.Calls() != 1 || second.Body.String() != first.Body.String() {
		t.Errorf("segunda = %s, calls = %d", second.Header().Get("X-Cache"), st.Calls())
	}
	etag := first.Header().Get("ETag")
	if etag != etagFor(testVersion, "servers") || first.Header().Get("X-Dataset-Version") != testVersion ||
		first.Header().Get("Cache-Control") != "public, max-age=300" || second.Header().Get("ETag") != etag {
		t.Fatalf("cabeçalhos = %v", first.Header())
	}
	for _, inm := range []string{etag, strings.TrimPrefix(etag, "W/"), `"outro", ` + etag, "*"} {
		rec := do(h, "GET", base+"/v1/servers", "If-None-Match", inm)
		if rec.Code != http.StatusNotModified || rec.Body.Len() != 0 || rec.Header().Get("ETag") != etag ||
			rec.Header().Get("X-Cache") != "HIT" || rec.Header().Get("Content-Type") != "" {
			t.Errorf("If-None-Match %s = %d %v", inm, rec.Code, rec.Header())
		}
	}
	if rec := do(h, "GET", base+"/servers", "If-None-Match", `W/"outro"`); rec.Code != 200 {
		t.Errorf("ETag diferente = %d", rec.Code)
	}
	if st.Calls() != 1 {
		t.Errorf("304 não deveria consultar o banco (calls = %d)", st.Calls())
	}
	// Cada consulta tem o próprio ETag; letra e nome têm o mesmo.
	a := do(h, "GET", base+"/server/a").Header().Get("ETag")
	b := do(h, "GET", base+"/v1/server/A.ROOT-SERVERS.NET.").Header().Get("ETag")
	if a == etag || a != b || a != etagFor(testVersion, "server:a") {
		t.Errorf("ETag do servidor: %q, %q (lista %q)", a, b, etag)
	}
	// Valores reais registrados na spec (dataset de 2026-09-30).
	real := "01a0f06d-08c2-7f4a-924a-b345129d0f5a"
	for key, want := range map[string]string{
		"servers": `W/"fbf484ee604cba0"`, "server:a": `W/"c618b310d98767be"`,
		"server:k": `W/"c618a910d98756c0"`, "server:m": `W/"c618af10d98760f2"`,
	} {
		if got := etagFor(real, key); got != want {
			t.Errorf("etagFor(%s) = %s, quero %s", key, got, want)
		}
	}
}

func TestHEAD(t *testing.T) {
	st := &fakeStore{}
	h, _ := newAPI(t, st, true)
	srv := httptest.NewServer(h)
	defer srv.Close()
	for _, p := range []string{"/servers", "/server/a", "/v1/server/m", "/", "/meta", "/status"} {
		resp, err := http.Head(srv.URL + base + p)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != 200 || len(body) != 0 {
			t.Errorf("HEAD %s = %d (%d bytes)", p, resp.StatusCode, len(body))
		}
	}
	rec := do(h, "HEAD", base+"/servers")
	etag := rec.Header().Get("ETag")
	if etag == "" || rec.Header().Get("X-Cache") != "HIT" {
		t.Errorf("HEAD sem cabeçalhos de dados: %v", rec.Header())
	}
	if rec := do(h, "HEAD", base+"/servers", "If-None-Match", etag); rec.Code != http.StatusNotModified {
		t.Errorf("HEAD com If-None-Match = %d", rec.Code)
	}
	if rec := do(h, "HEAD", base+"/server/n"); rec.Code != 404 {
		t.Errorf("HEAD inexistente = %d", rec.Code)
	}
}

func TestBypassAndBigBody(t *testing.T) {
	h := newAPIWith(t, &fakeStore{}, true, cache.Noop{})
	if rec := do(h, "GET", base+"/servers"); rec.Header().Get("X-Cache") != "BYPASS" {
		t.Errorf("sem cache = %s", rec.Header().Get("X-Cache"))
	}
	// Respostas acima de 8 MiB não vão para o cache.
	big := sample[0]
	big.Note = new(strings.Repeat("x", 9<<20))
	h, c := newAPI(t, &fakeStore{servers: []store.Server{big}}, true)
	rec := do(h, "GET", base+"/servers")
	if rec.Code != 200 || rec.Body.Len() <= maxCachedBody || len(c.keys()) != 0 {
		t.Errorf("corpo grande: %d, %d bytes, chaves %v", rec.Code, rec.Body.Len(), c.keys())
	}
}

func TestNotReady(t *testing.T) {
	st := &fakeStore{noData: true}
	h, _ := newAPI(t, st, false)
	for _, p := range []string{"/servers", "/v1/servers", "/server/a", "/v1/server/a.root-servers.net"} {
		rec := do(h, "GET", base+p)
		if rec.Code != 503 || decode[errorBody](t, rec).Error.Message !=
			"a primeira sincronização do collector-roothints ainda não terminou; tente em alguns minutos" {
			t.Errorf("%s sem dataset = %d %s", p, rec.Code, rec.Body)
		}
	}
	if st.Calls() != 0 {
		t.Errorf("sem dataset não consulta o banco (calls = %d)", st.Calls())
	}
	// Validação vem antes da conferência dos dados.
	if rec := do(h, "GET", base+"/server/ab"); rec.Code != 400 {
		t.Errorf("inválido sem dataset = %d", rec.Code)
	}
	rec := do(h, "GET", base+"/status")
	s := decode[StatusResponse](t, rec)
	if rec.Code != 200 || s.Status != "starting" || !s.Success || s.Checks["dataset"] != "empty" ||
		s.Message != "aguardando a primeira sincronização do collector-roothints" {
		t.Errorf("status sem dataset = %d %+v", rec.Code, s)
	}
	rec = do(h, "GET", base+"/meta")
	if rec.Code != 200 || rec.Body.String() != `{"app":"api-roothints","version":"test","dataset":null,"collector":null}`+"\n" {
		t.Errorf("meta sem dados = %d %s", rec.Code, rec.Body)
	}
}

func TestStatus(t *testing.T) {
	st := &fakeStore{}
	h, c := newAPI(t, st, true)
	for _, m := range []string{"GET", "POST"} {
		for _, p := range []string{"/health", "/status"} {
			rec := do(h, m, base+p)
			s := decode[StatusResponse](t, rec)
			if rec.Code != 200 || !s.Success || s.Status != "ok" || s.Timestamp == "" || s.Message != "api-roothints operacional" ||
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
	if rec.Code != 200 || !s.Success || s.Status != "degraded" || s.Checks["valkey"] != "error" ||
		s.Message != "Valkey indisponível; respondendo sem cache" {
		t.Errorf("valkey fora = %d %s", rec.Code, rec.Body)
	}
	// Valkey fora vence o starting.
	h2, c2 := newAPI(t, &fakeStore{noData: true}, false)
	c2.pingErr = errors.New("valkey fora")
	if s := decode[StatusResponse](t, do(h2, "GET", base+"/status")); s.Status != "degraded" || s.Checks["dataset"] != "empty" {
		t.Errorf("valkey fora sem dataset = %+v", s)
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
	for _, p := range []string{"/servers", "/qualquer/coisa"} {
		rec := do(h, "OPTIONS", base+p, "Origin", "https://exemplo.com", "Access-Control-Request-Method", "GET")
		if rec.Code != http.StatusNoContent || rec.Header().Get("Access-Control-Allow-Origin") != "*" ||
			rec.Header().Get("Access-Control-Allow-Methods") != "GET, HEAD, OPTIONS" ||
			rec.Header().Get("Access-Control-Allow-Headers") != "If-None-Match, Content-Type" ||
			rec.Header().Get("Access-Control-Max-Age") != "86400" {
			t.Errorf("preflight %s = %d %v", p, rec.Code, rec.Header())
		}
	}
	rec := do(h, "GET", base+"/server/a")
	hd := rec.Header()
	if hd.Get("Access-Control-Allow-Origin") != "*" || hd.Get("X-Content-Type-Options") != "nosniff" ||
		hd.Get("Referrer-Policy") != "no-referrer" || hd.Get("Server") != "badblock-api-roothints/test" ||
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
	wantEndpoints := []string{base + "/servers", base + "/server/{server}", base + "/meta", base + "/status", base + "/openapi.yaml"}
	for _, p := range []string{"/", "/v1/"} {
		rec := do(h, "GET", base+p)
		idx := decode[IndexResponse](t, rec)
		if rec.Code != 200 || idx.App != "api-roothints" || idx.Version != "test" || idx.BasePath != base ||
			idx.Source != SourceURL || !slices.Equal(idx.Versions, []string{"v1"}) || !slices.Equal(idx.Endpoints, wantEndpoints) ||
			rec.Header().Get("Cache-Control") != "public, max-age=300" || rec.Header().Get("ETag") != "" {
			t.Errorf("índice %s = %d %s", p, rec.Code, rec.Body)
		}
	}
	for _, p := range []string{base, base + "?x=1"} {
		if rec := do(h, "GET", p); rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != base+"/" {
			t.Errorf("redirect %s = %d %s", p, rec.Code, rec.Header().Get("Location"))
		}
	}
	for _, p := range []string{"/meta", "/v1/meta"} {
		rec := do(h, "GET", base+p)
		m := decode[MetaResponse](t, rec)
		if rec.Code != 200 || m.App != "api-roothints" || m.Dataset == nil || m.Dataset.Servers != 13 ||
			m.Dataset.Source != SourceURL || m.Dataset.MD5 != "d0" || m.Dataset.SHA256 != "18" ||
			*m.Dataset.LastUpdate != "2026-09-24" || *m.Dataset.ZoneSerial != 2026092401 || m.Dataset.Version != "v1" ||
			m.Collector == nil || m.Collector.App != "collector-roothints" || m.Collector.Consolidated ||
			m.Collector.LastSyncAt == nil || rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("meta = %d %s", rec.Code, rec.Body)
		}
		if rec.Header().Get("X-Cache") != "" || rec.Header().Get("ETag") != "" {
			t.Errorf("meta não usa cache: %v", rec.Header())
		}
	}
}
