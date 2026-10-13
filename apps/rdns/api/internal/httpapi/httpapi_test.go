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
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/patrickbrandao/badblock/apps/rdns/api/internal/cache"
	"github.com/patrickbrandao/badblock/apps/rdns/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/rdns/api/internal/realip"
	"github.com/patrickbrandao/badblock/apps/rdns/api/internal/store"
)

const base = "/rdns"

var scanned = time.Date(2026, 10, 10, 17, 26, 56, 0, time.UTC)

func ptrRow(ip string, status int16, names ...string) store.PTR {
	p := store.PTR{IP: netip.MustParseAddr(ip), Status: status, CreatedAt: scanned, UpdatedAt: scanned}
	if status == 1 {
		p.Name, p.Names = &names[0], names
	}
	return p
}

// fakeStore guarda prefixos e linhas de rdns_ptr em memória.
type fakeStore struct {
	mu       sync.Mutex
	calls    int
	pingErr  error
	queryErr error
	prefixes []store.Prefix
	ptrs     []store.PTR // em ordem de IP
	cfg      map[string]string
}

func newFakeStore() *fakeStore {
	ts := scanned
	return &fakeStore{
		prefixes: []store.Prefix{
			{Prefix: netip.MustParsePrefix("200.160.2.0/24"), ASN: 22548, Source: "cgibr", Active: true, LastScanAt: &ts},
			{Prefix: netip.MustParsePrefix("200.160.3.0/24"), ASN: 22548, Source: "cgibr", Active: true},
			{Prefix: netip.MustParsePrefix("10.0.0.0/24"), ASN: 0, Source: "static", Active: false, LastScanAt: &ts},
			{Prefix: netip.MustParsePrefix("10.1.0.0/26"), ASN: 0, Source: "static", Active: true},
		},
		ptrs: []store.PTR{
			ptrRow("200.160.2.1", 1, "ae1-0.ar1.vr.registro.br"),
			ptrRow("200.160.2.3", 1, "registro.br"),
			ptrRow("200.160.2.5", 1, "mailexploder.registro.br", "mailx.registro.br"),
			ptrRow("200.160.2.9", 4),
		},
		cfg: map[string]string{"rate_limit": "512"},
	}
}

func (f *fakeStore) count() {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
}

func (f *fakeStore) setPTR(p store.PTR) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.ptrs {
		if f.ptrs[i].IP == p.IP {
			f.ptrs[i] = p
			return
		}
	}
	f.ptrs = append(f.ptrs, p)
	slices.SortFunc(f.ptrs, func(a, b store.PTR) int { return a.IP.Compare(b.IP) })
}

func (f *fakeStore) Ping(context.Context) error { return f.pingErr }

func (f *fakeStore) IP(_ context.Context, ip netip.Addr) (*store.IPRow, error) {
	f.count()
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.prefixes {
		if p.Prefix.Contains(ip) {
			row := &store.IPRow{Prefix: p}
			for _, r := range f.ptrs {
				if r.IP == ip {
					row.PTR = &r
				}
			}
			return row, nil
		}
	}
	return nil, store.ErrNotFound
}

func (f *fakeStore) Prefix(_ context.Context, q netip.Prefix) (*store.PrefixRows, error) {
	f.count()
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.prefixes {
		if p.Prefix.Bits() <= q.Bits() && p.Prefix.Contains(q.Addr()) {
			rows := &store.PrefixRows{Prefix: p}
			for _, r := range f.ptrs {
				if q.Contains(r.IP) {
					rows.PTRs = append(rows.PTRs, r)
				}
			}
			return rows, nil
		}
	}
	return nil, store.ErrNotFound
}

func (f *fakeStore) Config(context.Context) (map[string]string, error) { return f.cfg, nil }

func (f *fakeStore) ScanStats(_ context.Context, interval time.Duration) (*store.ScanStats, error) {
	if interval != 24*time.Hour {
		return nil, errors.New("intervalo inesperado")
	}
	return &store.ScanStats{Prefixes: 4, Active: 3, Static: 2, OutOfRule: 1, Addresses: 512, NeverScanned: 1,
		Overdue: 0, Rescan: 0, Scanning: 1, OldestScanAt: &scanned, PTR: 3, NoPTR: 252, Failed: 1}, nil
}

func (f *fakeStore) Derivations(context.Context) ([]store.Derivation, error) {
	return []store.Derivation{{Source: "cgibr", Version: "01a1", SourceVersion: "01a0", SourceUpdatedAt: scanned,
		CreatedAt: scanned, Blocks: 13045, Units: 308529, Inserted: 308529}}, nil
}

func (f *fakeStore) Workers(context.Context) ([]store.Worker, error) {
	return []store.Worker{{Host: "1f2e3d4c5b6a", Version: "0.0.2", StartedAt: scanned, SeenAt: scanned, Alive: true,
		RateLimit: 256, Queries: 1000, LiveQueries: 2, Prefixes: 3}}, nil
}

func (f *fakeStore) Job(context.Context) (*store.Job, error) {
	return &store.Job{LastSyncAt: &scanned, LastCheckAt: &scanned}, nil
}

type fixedView struct{ snap dataset.Snapshot }

func (v fixedView) Current() dataset.Snapshot { return v.snap }

// memCache é um cache em memória para os testes.
type memCache struct {
	mu   sync.Mutex
	m    map[string][]byte
	down bool
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
func (c *memCache) Ping(context.Context) error {
	if c.down {
		return errors.New("down")
	}
	return nil
}
func (c *memCache) Enabled() bool { return true }

func (c *memCache) get(k string) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.m[k]
}

func newTestAPI(t *testing.T, st *fakeStore, ready bool, c cache.Cache, lv Live) *API {
	t.Helper()
	rip, err := realip.New(realip.DefaultTrustedProxies, realip.DefaultHeaders)
	if err != nil {
		t.Fatal(err)
	}
	return New(st, fixedView{dataset.Snapshot{HasPrefixes: ready}}, c, lv, rip,
		slog.New(slog.NewTextHandler(io.Discard, nil)), Config{BasePath: "/rdns/", Version: "test"})
}

func newAPI(t *testing.T, st *fakeStore, ready bool) (http.Handler, *memCache) {
	t.Helper()
	c := &memCache{m: map[string][]byte{}}
	return newTestAPI(t, st, ready, c, nil).Handler(), c
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
		t.Fatalf("%s: %v", rec.Body, err)
	}
	return v
}

func TestIP(t *testing.T) {
	h, _ := newAPI(t, newFakeStore(), true)
	cases := []struct {
		path, status, name string
		names              int
		active             bool
		checked            bool
		firstSeen          bool
	}{
		{"/rdns/ip/200.160.2.3", "ptr", "registro.br", 1, true, true, true},
		{"/rdns/v1/ip/200.160.2.5", "ptr", "mailexploder.registro.br", 2, true, true, true},
		{"/rdns/ip/::ffff:200.160.2.3", "ptr", "registro.br", 1, true, true, true}, // IPv4 mapeado
		{"/rdns/ip/200.160.2.2", "no_ptr", "", 0, true, true, false},               // sem linha, prefixo varrido
		{"/rdns/ip/200.160.3.1", "not_scanned", "", 0, true, false, false},         // prefixo nunca varrido
		{"/rdns/ip/200.160.2.9", "timeout", "", 0, true, true, true},               // falha
		{"/rdns/ip/10.0.0.5", "no_ptr", "", 0, false, true, false},                 // prefixo inativo
		{"/rdns/ip/200.160.2.3?live=false", "ptr", "registro.br", 1, true, true, true},
	}
	for _, c := range cases {
		rec := do(h, "GET", c.path)
		if rec.Code != 200 {
			t.Errorf("%s: %d %s", c.path, rec.Code, rec.Body)
			continue
		}
		got := decode[IPResponse](t, rec)
		name := ""
		if got.Name != nil {
			name = *got.Name
		}
		if got.Status != c.status || name != c.name || len(got.Names) != c.names || got.Names == nil ||
			got.Scan.Active != c.active || (got.Scan.CheckedAt != nil) != c.checked || (got.FirstSeen != nil) != c.firstSeen {
			t.Errorf("%s = %+v", c.path, got)
		}
		if got.Live != nil || strings.Contains(rec.Body.String(), `"live"`) {
			t.Errorf("%s: bloco live sem live=true", c.path)
		}
	}
	got := decode[IPResponse](t, do(h, "GET", "/rdns/ip/::ffff:200.160.2.3"))
	if got.IP != "200.160.2.3" || got.Scan.Prefix != "200.160.2.0/24" || got.Scan.ASN != 22548 || got.Scan.Source != "cgibr" {
		t.Errorf("normalização = %+v", got)
	}
	// O JSON tem os campos na ordem da spec.
	body := do(h, "GET", "/rdns/ip/200.160.2.3").Body.String()
	want := `{"ip":"200.160.2.3","status":"ptr","name":"registro.br","names":["registro.br"],"first_seen":"2026-10-10T17:26:56Z","updated_at":"2026-10-10T17:26:56Z","scan":{"prefix":"200.160.2.0/24","asn":22548,"source":"cgibr","active":true,"checked_at":"2026-10-10T17:26:56Z"}}` + "\n"
	if body != want {
		t.Errorf("corpo = %s\nquero   %s", body, want)
	}
}

func TestErrors(t *testing.T) {
	h, _ := newAPI(t, newFakeStore(), true)
	cases := map[string]struct {
		code int
		msg  string
	}{
		"/rdns/ip/8.8.8.8":                         {404, "8.8.8.8 não está em nenhum prefixo do rdns"},
		"/rdns/ip/2001:db8::1":                     {400, "o rdns varre só IPv4"},
		"/rdns/ip/abc":                             {400, "endereço IP inválido"},
		"/rdns/ip/999.1.1.1":                       {400, "endereço IP inválido"},
		"/rdns/ip/200.160.2.3?live=1":              {400, "live inválido: use true ou false"},
		"/rdns/ip/200.160.2.3?live=TRUE":           {400, "live inválido: use true ou false"},
		"/rdns/ip/200.160.2.3?live=true&live=true": {400, "live inválido: use true ou false"},
		"/rdns/ip/200.160.2.3?live":                {400, "live inválido: use true ou false"},
		"/rdns/prefix/200.160.2.0/23":              {400, "tamanho de prefixo inválido: use de 24 a 31"},
		"/rdns/prefix/200.160.2.0/32":              {400, "tamanho de prefixo inválido: use de 24 a 31"},
		"/rdns/prefix/200.160.2.0/x":               {400, "tamanho de prefixo inválido: use de 24 a 31"},
		"/rdns/prefix/2001:db8::/64":               {400, "o rdns varre só IPv4"},
		"/rdns/prefix/2001:db8::1/64":              {400, "o rdns varre só IPv4"},
		"/rdns/prefix/200.160.2.0/24?live=true":    {400, "live só vale em /rdns/ip"},
		"/rdns/prefix/8.8.8.0/24":                  {404, "8.8.8.0/24 não está contido em nenhum prefixo do rdns"},
		"/rdns/prefix/10.1.0.0/24":                 {404, "10.1.0.0/24 não está contido em nenhum prefixo do rdns"},
		"/rdns/asn/22548":                          {404, "rota inexistente; veja /rdns/"},
		"/rdns/v2/ip/200.160.2.3":                  {404, "rota inexistente; veja /rdns/"},
		"/outra-coisa":                             {404, "rota inexistente; veja /rdns/"},
	}
	for p, want := range cases {
		rec := do(h, "GET", p)
		if rec.Code != want.code {
			t.Errorf("%s: %d, quero %d (%s)", p, rec.Code, want.code, rec.Body)
			continue
		}
		e := decode[errorBody](t, rec)
		if e.Error.Message != want.msg || rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: %q, quero %q", p, e.Error.Message, want.msg)
		}
	}
	// A validação vem antes de tudo, mesmo sem prefixos.
	h, _ = newAPI(t, newFakeStore(), false)
	if rec := do(h, "GET", "/rdns/ip/2001:db8::1"); rec.Code != 400 {
		t.Errorf("400 antes do 503: %d", rec.Code)
	}
}

func TestPrefix(t *testing.T) {
	h, _ := newAPI(t, newFakeStore(), true)
	rec := do(h, "GET", "/rdns/prefix/200.160.2.77/24")
	if rec.Code != 200 {
		t.Fatalf("/24 = %d %s", rec.Code, rec.Body)
	}
	got := decode[PrefixResponse](t, rec)
	if got.Prefix != "200.160.2.0/24" || len(got.Items) != 256 || got.Scan.Prefix != "200.160.2.0/24" {
		t.Errorf("/24 = %s, %d itens", got.Prefix, len(got.Items))
	}
	if got.Counts != (PrefixCounts{PTR: 3, NoPTR: 252, Timeout: 1}) {
		t.Errorf("counts = %+v", got.Counts)
	}
	if it := got.Items[5]; it.IP != "200.160.2.5" || it.Status != "ptr" || len(it.Names) != 2 {
		t.Errorf("item .5 = %+v", it)
	}
	if it := got.Items[9]; it.Status != "timeout" || it.Name != nil || it.Names == nil || len(it.Names) != 0 {
		t.Errorf("item .9 = %+v", it)
	}
	if got.Items[0].IP != "200.160.2.0" || got.Items[255].IP != "200.160.2.255" {
		t.Error("itens fora de ordem")
	}

	// /31: dois endereços; bits de host zerados.
	got = decode[PrefixResponse](t, do(h, "GET", "/rdns/prefix/200.160.2.5/31"))
	if got.Prefix != "200.160.2.4/31" || len(got.Items) != 2 || got.Items[1].Status != "ptr" || got.Counts.NoPTR != 1 {
		t.Errorf("/31 = %+v", got)
	}
	// Nunca varrido: not_scanned.
	got = decode[PrefixResponse](t, do(h, "GET", "/rdns/prefix/200.160.3.0/30"))
	if got.Counts.NotScanned != 4 || got.Scan.CheckedAt != nil {
		t.Errorf("nunca varrido = %+v", got)
	}
	// Contido num estático menor que /24.
	if rec := do(h, "GET", "/rdns/prefix/10.1.0.16/28"); rec.Code != 200 {
		t.Errorf("/28 dentro do /26 = %d", rec.Code)
	}
	// O exemplo da spec, com o 200.160.2.0/30.
	body := do(h, "GET", "/rdns/prefix/200.160.2.0/30").Body.String()
	want := `{"prefix":"200.160.2.0/30","scan":{"prefix":"200.160.2.0/24","asn":22548,"source":"cgibr","active":true,"checked_at":"2026-10-10T17:26:56Z"},"counts":{"ptr":2,"no_ptr":2,"servfail":0,"refused":0,"timeout":0,"not_scanned":0},"items":[{"ip":"200.160.2.0","status":"no_ptr","name":null,"names":[]},{"ip":"200.160.2.1","status":"ptr","name":"ae1-0.ar1.vr.registro.br","names":["ae1-0.ar1.vr.registro.br"]},{"ip":"200.160.2.2","status":"no_ptr","name":null,"names":[]},{"ip":"200.160.2.3","status":"ptr","name":"registro.br","names":["registro.br"]}]}` + "\n"
	if body != want {
		t.Errorf("corpo = %s\nquero   %s", body, want)
	}
}

func TestCacheAndETag(t *testing.T) {
	st := newFakeStore()
	h, c := newAPI(t, st, true)

	first := do(h, "GET", "/rdns/ip/200.160.2.3")
	if first.Header().Get("X-Cache") != "MISS" {
		t.Errorf("primeira = %s", first.Header().Get("X-Cache"))
	}
	// Versionada e sem versão compartilham o cache e o ETag.
	second := do(h, "GET", "/rdns/v1/ip/200.160.2.3")
	if second.Header().Get("X-Cache") != "HIT" || st.calls != 1 {
		t.Errorf("segunda = %s, calls = %d", second.Header().Get("X-Cache"), st.calls)
	}
	if b := c.get("badblock:api-rdns:ip:200.160.2.3"); b == nil {
		t.Errorf("chave sem versão ausente; chaves: %v", c.m)
	}
	etag := first.Header().Get("ETag")
	if etag == "" || etag != second.Header().Get("ETag") || etag != etagFor(first.Body.Bytes()) {
		t.Fatalf("ETag = %q / %q", etag, second.Header().Get("ETag"))
	}
	if first.Header().Get("X-Dataset-Version") != "" {
		t.Error("o rdns não tem X-Dataset-Version")
	}
	if first.Header().Get("Cache-Control") != "public, max-age=300" {
		t.Errorf("Cache-Control = %q", first.Header().Get("Cache-Control"))
	}
	if got := first.Header().Get("Access-Control-Expose-Headers"); got != "ETag, X-Cache" {
		t.Errorf("Expose-Headers = %q", got)
	}
	rec := do(h, "GET", "/rdns/ip/200.160.2.3", "If-None-Match", etag)
	if rec.Code != http.StatusNotModified || rec.Body.Len() != 0 || rec.Header().Get("ETag") != etag {
		t.Errorf("If-None-Match = %d %q", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", "/rdns/ip/200.160.2.3", "If-None-Match", `W/"0"`); rec.Code != 200 {
		t.Errorf("ETag diferente = %d", rec.Code)
	}
	// Corpo diferente, ETag diferente.
	if other := do(h, "GET", "/rdns/ip/200.160.2.5"); other.Header().Get("ETag") == etag {
		t.Error("corpos diferentes com o mesmo ETag")
	}
	// Prefixo: a chave é o prefixo zerado.
	do(h, "GET", "/rdns/prefix/200.160.2.77/24")
	if c.get("badblock:api-rdns:prefix:200.160.2.0/24") == nil {
		t.Error("chave do prefixo ausente")
	}
}

func TestNotReady(t *testing.T) {
	h, _ := newAPI(t, newFakeStore(), false)
	for _, p := range []string{"/rdns/ip/200.160.2.3", "/rdns/prefix/200.160.2.0/24", "/rdns/ip/200.160.2.3?live=true"} {
		rec := do(h, "GET", p)
		if rec.Code != 503 || decode[errorBody](t, rec).Error.Code != "dataset_not_ready" {
			t.Errorf("%s sem prefixos = %d %s", p, rec.Code, rec.Body)
		}
	}
	s := decode[StatusResponse](t, do(h, "GET", "/rdns/status"))
	if s.Status != "starting" || s.Checks["dataset"] != "empty" || s.Message != "aguardando a primeira derivação do collector-rdns" {
		t.Errorf("status sem prefixos = %+v", s)
	}
	// /meta responde mesmo assim.
	if rec := do(h, "GET", "/rdns/meta"); rec.Code != 200 {
		t.Errorf("meta sem prefixos = %d", rec.Code)
	}
}

func TestDatabaseErrors(t *testing.T) {
	st := newFakeStore()
	st.queryErr = errors.New("conexão recusada")
	h, _ := newAPI(t, st, true)
	if rec := do(h, "GET", "/rdns/ip/200.160.2.3"); rec.Code != 503 || decode[errorBody](t, rec).Error.Code != "database_unavailable" {
		t.Errorf("banco fora = %d %s", rec.Code, rec.Body)
	}
	st.queryErr = context.DeadlineExceeded
	if rec := do(h, "GET", "/rdns/ip/200.160.2.4"); rec.Code != 504 {
		t.Errorf("prazo = %d %s", rec.Code, rec.Body)
	}
}

func TestStatus(t *testing.T) {
	st := newFakeStore()
	c := &memCache{m: map[string][]byte{}}
	h := newTestAPI(t, st, true, c, nil).Handler()
	for _, m := range []string{"GET", "POST"} {
		for _, p := range []string{"/rdns/health", "/rdns/status"} {
			s := decode[StatusResponse](t, do(h, m, p))
			if !s.Success || s.Status != "ok" || s.Message != "api-rdns operacional" || s.Checks["valkey"] != "ok" {
				t.Errorf("%s %s = %+v", m, p, s)
			}
		}
	}
	c.down = true
	s := decode[StatusResponse](t, do(h, "GET", "/rdns/status"))
	if s.Status != "degraded" || s.Message != "Valkey indisponível; respondendo sem cache e sem tempo real" {
		t.Errorf("Valkey fora = %+v", s)
	}
	st.pingErr = errors.New("down")
	rec := do(h, "GET", "/rdns/status")
	if rec.Code != 503 || decode[StatusResponse](t, rec).Status != "error" {
		t.Errorf("Postgres fora = %d %s", rec.Code, rec.Body)
	}
	// Sem cache e sem tempo real: valkey disabled.
	h = newTestAPI(t, newFakeStore(), true, cache.Noop{}, nil).Handler()
	if s := decode[StatusResponse](t, do(h, "GET", "/rdns/status")); s.Checks["valkey"] != "disabled" {
		t.Errorf("sem Valkey = %+v", s)
	}
	if rec := do(h, "GET", "/rdns/ping"); rec.Body.String() != "pong" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("ping = %q", rec.Body)
	}
}

func TestIndexMetaAndRedirect(t *testing.T) {
	h, _ := newAPI(t, newFakeStore(), true)
	idx := decode[IndexResponse](t, do(h, "GET", "/rdns/"))
	if idx.App != "api-rdns" || idx.BasePath != "/rdns" || !slices.Equal(idx.DerivedFrom, []string{"cgibr"}) ||
		idx.Source != SourceURL || !slices.Contains(idx.Endpoints, "/rdns/ip/{ip}") {
		t.Errorf("índice = %+v", idx)
	}
	if rec := do(h, "GET", "/rdns/v1/"); rec.Code != 200 {
		t.Errorf("índice v1 = %d", rec.Code)
	}
	if rec := do(h, "GET", "/rdns"); rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/rdns/" {
		t.Errorf("redirect = %d %s", rec.Code, rec.Header().Get("Location"))
	}
	rec := do(h, "GET", "/rdns/meta")
	m := decode[MetaResponse](t, rec)
	if rec.Header().Get("Cache-Control") != "no-store" || m.App != "api-rdns" || m.Scan.Prefixes != 4 ||
		m.Scan.Addresses != 512 || m.Scan.OldestScanAt == nil || m.Config.RateLimit == nil || *m.Config.RateLimit != 512 ||
		m.Config.ScanInterval != nil || len(m.Derivations) != 1 || m.Derivations[0].Units != 308529 ||
		len(m.Workers) != 1 || !m.Workers[0].Alive || m.Collector == nil || m.Collector.Consolidated || m.Live.Queue != nil {
		t.Errorf("meta = %s", rec.Body)
	}
	for _, k := range []string{`"scan":`, `"config":`, `"derivations":`, `"workers":`, `"collector":`, `"live":{"queue":null}`} {
		if !strings.Contains(rec.Body.String(), k) {
			t.Errorf("meta sem %s: %s", k, rec.Body)
		}
	}
	if strings.Contains(rec.Body.String(), "dns_servers") {
		t.Error("o /meta não mostra os servidores DNS")
	}
}

func TestHeadAndOptions(t *testing.T) {
	h, _ := newAPI(t, newFakeStore(), true)
	srv := httptest.NewServer(h)
	defer srv.Close()
	resp, err := http.Head(srv.URL + "/rdns/ip/200.160.2.3")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || len(body) != 0 || resp.Header.Get("ETag") == "" {
		t.Errorf("HEAD = %d %q %v", resp.StatusCode, body, resp.Header)
	}
	rec := do(h, "OPTIONS", "/rdns/ip/200.160.2.3")
	if rec.Code != 204 || rec.Header().Get("Access-Control-Allow-Methods") != "GET, HEAD, OPTIONS" {
		t.Errorf("OPTIONS = %d %v", rec.Code, rec.Header())
	}
	if rec := do(h, "POST", "/rdns/ip/200.160.2.3"); rec.Code != 404 {
		t.Errorf("POST em rota GET = %d", rec.Code)
	}
}
