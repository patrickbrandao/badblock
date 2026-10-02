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

	"github.com/patrickbrandao/badblock/apps/rootanchors/api/internal/cache"
	"github.com/patrickbrandao/badblock/apps/rootanchors/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/rootanchors/api/internal/realip"
	"github.com/patrickbrandao/badblock/apps/rootanchors/api/internal/store"
)

// base é o caminho de base padrão; appName, o nome do app.
const (
	base    = "/rootanchors"
	appName = "api-rootanchors"
)

func tp(s string) *time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return &t
}

// As três chaves reais do root-anchors.xml de 2026-09-30, como o
// collector-rootanchors grava (specs/fontes/rootanchors/fonte.md).
const (
	pub20326 = "AwEAAaz/tAm8yTn4Mfeh5eyI96WSVexTBAvkMgJzkKTOiW1vkIbzxeF3+/4RgWOq7HrxRixHlFlExOLAJr5emLvN7SWXgnLh4+B5xQlNVz8Og8kvArMtNROxVQuCaSnIDdD5LKyWbRd2n9WGe2R8PzgCmr3EgVLrjyBxWezF0jLHwVN8efS3rCj/EWgvIWgb9tarpVUDK/b58Da+sqqls3eNbuv7pr+eoZG+SrDK6nWeL3c6H5Apxz7LjVc1uTIdsIXxuOLYA4/ilBmSVIzuDWfdRUfhHdY6+cn8HFRm+2hM8AnXGXws9555KrUB5qihylGa8subX2Nn6UwNR1AkUTV74bU="
	pub38696 = "AwEAAa96jeuknZlaeSrvyAJj6ZHv28hhOKkx3rLGXVaC6rXTsDc449/cidltpkyGwCJNnOAlFNKF2jBosZBU5eeHspaQWOmOElZsjICMQMC3aeHbGiShvZsx4wMYSjH8e7Vrhbu6irwCzVBApESjbUdpWWmEnhathWu1jo+siFUiRAAxm9qyJNg/wOZqqzL/dL/q8PkcRU5oUKEpUge71M3ej2/7CPqpdVwuMoTvoB+ZOT4YeGyxMvHmbrxlFzGOHOijtzN+u1TQNatX2XBuzZNQ1K+s2CXkPIZo7s6JgZyvaBevYtxPvYLw4z9mR7K2vaF18UYH9Z9GNUUeayffKC73PYc="
)

var (
	seen   = *tp("2026-09-30T03:44:43Z")
	sample = []store.Key{
		{KeyID: "Kjqmt7v", KeyTag: 19036, Algorithm: 8, DigestType: 2,
			Digest:    "49AAC11D7B6F6446702E54A1607371607A1A41855200FD2CE1CDDE32F24E8FB5",
			ValidFrom: *tp("2010-07-15T00:00:00Z"), ValidUntil: tp("2019-01-11T00:00:00Z"), CreatedAt: seen, UpdatedAt: seen},
		{KeyID: "Klajeyz", KeyTag: 20326, Algorithm: 8, DigestType: 2,
			Digest:    "E06D44B80B8F1D39A95C0B0D7C65D08458E880409BBC683457104237C7F8EC8D",
			PublicKey: new(pub20326), Flags: new(257),
			ValidFrom: *tp("2017-02-02T00:00:00Z"), CreatedAt: seen, UpdatedAt: seen},
		{KeyID: "Kmyv6jo", KeyTag: 38696, Algorithm: 8, DigestType: 2,
			Digest:    "683D2D0ACB8C9B712A1948B27F741219298D0A450D612C483AF444A4C0FB2B16",
			PublicKey: new(pub38696), Flags: new(257),
			ValidFrom: *tp("2024-07-18T00:00:00Z"), CreatedAt: seen, UpdatedAt: seen},
	}
	// dupTag é uma chave artificial com o key tag da 20326 (o key tag não é
	// único), com validade posterior: vem depois dela na ordem.
	dupTag = store.Key{KeyID: "Kzzzzzz", KeyTag: 20326, Algorithm: 13, DigestType: 4,
		Digest:    strings.Repeat("AB", 48),
		ValidFrom: *tp("2030-01-01T00:00:00Z"), CreatedAt: seen, UpdatedAt: seen}
)

type fakeStore struct {
	mu        sync.Mutex
	calls     int
	pingErr   error
	err       error  // devolvido pelas consultas de chaves
	dsErr     error  // devolvido por Dataset
	panicMsg  string // faz as consultas de chaves entrarem em pânico
	noData    bool   // Dataset e Job devolvem nil (antes da primeira carga)
	withDup   bool   // acrescenta dupTag
	bigKey    bool   // Keys devolve uma chave pública de 9 MiB (acima do limite do cache)
	emptySrc  bool   // anchor_source NULL
	lastTag   int
	datasetCt int
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
	f.mu.Lock()
	f.datasetCt++
	f.mu.Unlock()
	if f.dsErr != nil {
		return nil, f.dsErr
	}
	if f.noData {
		return nil, nil
	}
	d := &store.Dataset{Version: "v1", AppliedAt: time.Unix(0, 0), URL: SourceURL, SHA256: "ab",
		AnchorID: "0C05FDD6-422C-4910-8ED6-430ED15E11C2", AnchorSource: new("http://data.iana.org/root-anchors/root-anchors.xml"),
		Zone: ".", Keys: len(sample)}
	if f.emptySrc {
		d.AnchorSource = nil
	}
	return d, nil
}

func (f *fakeStore) Job(context.Context) (*store.Job, error) {
	if f.noData {
		return nil, nil
	}
	t := time.Unix(10, 0)
	return &store.Job{LastSyncAt: &t, LastCheckAt: &t, Consolidated: 0}, nil
}

func (f *fakeStore) all() []store.Key {
	out := slices.Clone(sample)
	if f.withDup {
		out = append(out, dupTag)
	}
	if f.bigKey {
		out = append(out, store.Key{KeyID: "Kbig", KeyTag: 1, PublicKey: new(strings.Repeat("A", 9<<20)), Flags: new(257)})
	}
	return out
}

func (f *fakeStore) Keys(context.Context) ([]store.Key, error) {
	if err := f.count(); err != nil {
		return nil, err
	}
	return f.all(), nil
}

func (f *fakeStore) KeysByTag(_ context.Context, tag int) ([]store.Key, error) {
	if err := f.count(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.lastTag = tag
	f.mu.Unlock()
	var out []store.Key
	for _, k := range f.all() {
		if k.KeyTag == tag {
			out = append(out, k)
		}
	}
	return out, nil
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

// newTestAPI monta a API (com BASE_PATH "/rootanchors/", que vira
// "/rootanchors") sobre o store falso e uma versão fixa, "0192-v1".
func newTestAPI(t *testing.T, st *fakeStore, ready bool, c cache.Cache) *API {
	t.Helper()
	rip, err := realip.New(realip.DefaultTrustedProxies, realip.DefaultHeaders)
	if err != nil {
		t.Fatal(err)
	}
	snap := dataset.Snapshot{}
	if ready {
		snap = dataset.Snapshot{Version: "0192-v1", AppliedAt: time.Unix(0, 0)}
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

func TestKeys(t *testing.T) {
	st := &fakeStore{}
	h, c := newAPI(t, st, true)
	for _, p := range []string{base + "/keys", base + "/v1/keys"} {
		rec := do(h, "GET", p)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", p, rec.Code, rec.Body)
		}
		got := decode[KeysResponse](t, rec)
		if got.Count != 3 || len(got.Keys) != 3 || got.Keys[0].KeyTag != 19036 || got.Keys[1].KeyTag != 20326 ||
			got.Keys[2].KeyTag != 38696 || got.Dataset.Version != "0192-v1" || got.Dataset.UpdatedAt != "1970-01-01T00:00:00Z" {
			t.Errorf("%s: %+v", p, got)
		}
		if got.TrustAnchor.ID != "0C05FDD6-422C-4910-8ED6-430ED15E11C2" || got.TrustAnchor.Zone != "." ||
			got.TrustAnchor.Source == nil || *got.TrustAnchor.Source != "http://data.iana.org/root-anchors/root-anchors.xml" {
			t.Errorf("trust_anchor = %+v", got.TrustAnchor)
		}
	}
	// Sem versão e /v1 caem na mesma chave: uma consulta só ao banco.
	if st.Calls() != 1 || !slices.Equal(c.keys(), []string{"badblock:api-rootanchors:0192-v1:keys"}) {
		t.Errorf("calls = %d, chaves = %v", st.Calls(), c.keys())
	}

	// A chave sem PublicKey sai com public_key, flags e dnskey null, e o JSON
	// é exatamente este (ordem dos campos, datas em UTC).
	body := do(h, "GET", base+"/keys").Body.String()
	want19036 := `{"key_id":"Kjqmt7v","key_tag":19036,"algorithm":8,"digest_type":2,` +
		`"digest":"49AAC11D7B6F6446702E54A1607371607A1A41855200FD2CE1CDDE32F24E8FB5",` +
		`"ds":". IN DS 19036 8 2 49AAC11D7B6F6446702E54A1607371607A1A41855200FD2CE1CDDE32F24E8FB5",` +
		`"public_key":null,"flags":null,"dnskey":null,"valid_from":"2010-07-15T00:00:00Z","valid_until":"2019-01-11T00:00:00Z",` +
		`"first_seen":"2026-09-30T03:44:43Z","updated_at":"2026-09-30T03:44:43Z"}`
	if !strings.Contains(body, want19036) {
		t.Errorf("chave 19036 fora do formato:\n%s", body)
	}
	// Nenhum campo que dependa do relógio no corpo em cache.
	if strings.Contains(body, "active") {
		t.Errorf("o corpo não pode ter campo dependente do relógio: %s", body)
	}
	if !strings.HasSuffix(body, "}\n") || strings.Count(body, "\n") != 1 {
		t.Errorf("JSON compacto com uma quebra de linha no fim: %q", body[len(body)-10:])
	}
}

func TestKeyDSAndDNSKEY(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, true)
	got := decode[KeyTagResponse](t, do(h, "GET", base+"/key/20326"))
	if got.KeyTag != 20326 || got.Count != 1 || len(got.Keys) != 1 {
		t.Fatalf("20326 = %+v", got)
	}
	k := got.Keys[0]
	if k.DS != ". IN DS 20326 8 2 E06D44B80B8F1D39A95C0B0D7C65D08458E880409BBC683457104237C7F8EC8D" ||
		k.DNSKEY == nil || *k.DNSKEY != ". IN DNSKEY 257 3 8 "+pub20326 ||
		k.PublicKey == nil || *k.PublicKey != pub20326 || k.Flags == nil || *k.Flags != 257 ||
		k.ValidFrom != "2017-02-02T00:00:00Z" || k.ValidUntil != nil || k.FirstSeen != "2026-09-30T03:44:43Z" {
		t.Errorf("20326 = %+v", k)
	}
	// A zona do TrustAnchor é o dono dos textos.
	if kk := keyOf("example.", sample[1]); !strings.HasPrefix(kk.DS, "example. IN DS ") || !strings.HasPrefix(*kk.DNSKEY, "example. IN DNSKEY ") {
		t.Errorf("owner = %+v", kk)
	}
	// source NULL sai null.
	h, _ = newAPI(t, &fakeStore{emptySrc: true}, true)
	if rec := do(h, "GET", base+"/key/38696"); !strings.Contains(rec.Body.String(), `"trust_anchor":{"id":"0C05FDD6-422C-4910-8ED6-430ED15E11C2","source":null,"zone":"."}`) {
		t.Errorf("source null = %s", rec.Body)
	}
}

func TestKeyTag(t *testing.T) {
	st := &fakeStore{}
	h, c := newAPI(t, st, true)
	for _, p := range []string{base + "/key/20326", base + "/v1/key/20326", base + "/key/020326", base + "/v1/key/0000020326"} {
		rec := do(h, "GET", p)
		got := decode[KeyTagResponse](t, rec)
		if rec.Code != 200 || got.KeyTag != 20326 || got.Count != 1 || got.Keys[0].KeyID != "Klajeyz" {
			t.Errorf("%s: %d %s", p, rec.Code, rec.Body)
		}
	}
	// Zeros à esquerda e /v1 caem numa chave só: uma consulta ao banco.
	if st.Calls() != 1 || st.lastTag != 20326 || !slices.Equal(c.keys(), []string{"badblock:api-rootanchors:0192-v1:key:20326"}) {
		t.Errorf("calls = %d, tag = %d, chaves = %v", st.Calls(), st.lastTag, c.keys())
	}
	// Key tag repetido: lista com as duas, em ordem de valid_from.
	h, _ = newAPI(t, &fakeStore{withDup: true}, true)
	got := decode[KeyTagResponse](t, do(h, "GET", base+"/key/20326"))
	if got.Count != 2 || got.Keys[0].KeyID != "Klajeyz" || got.Keys[1].KeyID != "Kzzzzzz" ||
		got.Keys[1].DNSKEY != nil || got.Keys[1].DS != ". IN DS 20326 13 4 "+strings.Repeat("AB", 48) {
		t.Errorf("repetido = %+v", got)
	}
	// Os limites da faixa são aceitos (0 e 65535 não existem: 404).
	for _, tag := range []string{"0", "65535", "00000"} {
		rec := do(h, "GET", base+"/key/"+tag)
		if rec.Code != 404 {
			t.Errorf("/key/%s = %d %s", tag, rec.Code, rec.Body)
		}
	}
	rec := do(h, "GET", base+"/key/12345")
	if !strings.Contains(rec.Body.String(), `"nenhuma chave com o key tag 12345 no root-anchors.xml da IANA"`) {
		t.Errorf("404 = %s", rec.Body)
	}
	if rec := do(h, "GET", base+"/key/00012345"); !strings.Contains(rec.Body.String(), "key tag 12345 ") {
		t.Errorf("404 com o key tag normalizado = %s", rec.Body)
	}
}

func TestParseKeyTag(t *testing.T) {
	ok := map[string]int{"0": 0, "20326": 20326, "65535": 65535, "020326": 20326, "0000000000000000000038696": 38696}
	for in, want := range ok {
		if got, valid := parseKeyTag(in); !valid || got != want {
			t.Errorf("%q → %d %v, quero %d", in, got, valid, want)
		}
	}
	for _, in := range []string{"", "65536", "-1", "+1", " 1", "1 ", "1e3", "0x10", "abc", "２０３２６", "99999999999999999999"} {
		if _, valid := parseKeyTag(in); valid {
			t.Errorf("%q deveria ser inválido", in)
		}
	}
}

func TestErrors(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, true)
	cases := map[string]int{
		base + "/key/12345":          404,
		base + "/key/abc":            400,
		base + "/key/-1":             400,
		base + "/key/%2B1":           400,
		base + "/key/65536":          400,
		base + "/key/%2020326":       400,
		base + "/key/1e3":            400,
		base + "/v1/key/65536":       400,
		base + "/key/":               404,
		base + "/key":                404,
		base + "/key/20326/":         404,
		base + "/keys/20326":         404,
		base + "/keys/":              404,
		base + "/v2/keys":            404,
		base + "/nada":               404,
		base + "/v1/openapi.yaml":    404,
		base + "/v1/status":          404,
		"/outra-coisa":               404,
		"/keys":                      404,
		base + "/key/20326?x=%00%FF": 200,
	}
	for p, want := range cases {
		rec := do(h, "GET", p)
		if rec.Code != want {
			t.Errorf("%s: %d, quero %d (%s)", p, rec.Code, want, rec.Body)
		}
		if want == 200 {
			continue
		}
		if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") || rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: erro deveria ser JSON sem cache (%v)", p, rec.Header())
		}
		var e errorBody
		if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil || e.Error.Code == "" || e.Error.Message == "" {
			t.Errorf("%s: corpo de erro = %s", p, rec.Body)
		}
	}
	rec := do(h, "GET", base+"/key/abc")
	if !strings.Contains(rec.Body.String(), `{"error":{"code":"bad_request","message":"key tag inválido: use um número de 0 a 65535 (ex.: 20326)"}}`) {
		t.Errorf("400 = %s", rec.Body)
	}
	if rec := do(h, "GET", base+"/nada"); !strings.Contains(rec.Body.String(), "rota inexistente; veja /rootanchors/") {
		t.Errorf("404 genérico = %s", rec.Body)
	}
	// Método não registrado numa rota existente: 404 do catch-all.
	for _, m := range []string{"DELETE", "POST", "PUT"} {
		if rec := do(h, m, base+"/keys"); rec.Code != 404 {
			t.Errorf("%s /keys = %d", m, rec.Code)
		}
	}
	if rec := do(h, "POST", base+"/meta"); rec.Code != 404 {
		t.Errorf("POST /meta = %d", rec.Code)
	}
}

func TestDatabaseErrors(t *testing.T) {
	st := &fakeStore{err: errors.New("conexão recusada")}
	h, c := newAPI(t, st, true)
	for _, p := range []string{base + "/keys", base + "/key/20326"} {
		rec := do(h, "GET", p)
		if rec.Code != 503 || !strings.Contains(rec.Body.String(), "database_unavailable") {
			t.Errorf("%s: %d %s", p, rec.Code, rec.Body)
		}
	}
	st.err = fmt.Errorf("consulta: %w", context.DeadlineExceeded)
	for _, p := range []string{base + "/keys", base + "/key/20326"} {
		rec := do(h, "GET", p)
		if rec.Code != 504 || !strings.Contains(rec.Body.String(), `"timeout"`) {
			t.Errorf("%s: %d %s", p, rec.Code, rec.Body)
		}
	}
	// Falha na leitura do TrustAnchor também é 503.
	st.err, st.dsErr = nil, errors.New("down")
	if rec := do(h, "GET", base+"/keys"); rec.Code != 503 || !strings.Contains(rec.Body.String(), "database_unavailable") {
		t.Errorf("Dataset com erro = %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", base+"/meta"); rec.Code != 503 || rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("/meta com erro = %d %v", rec.Code, rec.Header())
	}
	// Erros não entram no cache: quando o banco volta, a resposta é calculada.
	st.dsErr = nil
	if len(c.keys()) != 0 {
		t.Errorf("erro foi para o cache: %v", c.keys())
	}
	if rec := do(h, "GET", base+"/keys"); rec.Code != 200 || rec.Header().Get("X-Cache") != "MISS" {
		t.Errorf("depois do erro = %d %s", rec.Code, rec.Header().Get("X-Cache"))
	}
	// 404 também não entra no cache.
	do(h, "GET", base+"/key/1")
	if !slices.Equal(c.keys(), []string{"badblock:api-rootanchors:0192-v1:keys"}) {
		t.Errorf("chaves = %v", c.keys())
	}
}

func TestPanicIsJSON500(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{panicMsg: "bug"}, true)
	for _, p := range []string{base + "/keys", base + "/key/20326"} {
		rec := do(h, "GET", p)
		if rec.Code != 500 || !strings.Contains(rec.Body.String(), "internal_error") || rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s panic = %d %s", p, rec.Code, rec.Body)
		}
	}
}

// Os ETags do manifesto e da spec saem do etagFor com a versão real.
func TestETagOfExamples(t *testing.T) {
	const v = "01a0f06a-259b-7abc-a1fd-a7b4bd279125"
	for key, want := range map[string]string{
		"keys":      `W/"c3581f03fc89adf3"`,
		"key:19036": `W/"54e586113b1ebf5f"`,
		"key:20326": `W/"e2045590e46611e7"`,
		"key:38696": `W/"9e9bffe234a5244"`,
	} {
		if got := etagFor(v, key); got != want {
			t.Errorf("etagFor(%s) = %s, quero %s", key, got, want)
		}
	}
}

func TestCacheAndETag(t *testing.T) {
	st := &fakeStore{}
	h, c := newAPI(t, st, true)

	first := do(h, "GET", base+"/key/20326")
	if first.Header().Get("X-Cache") != "MISS" {
		t.Errorf("primeira = %s", first.Header().Get("X-Cache"))
	}
	// Versionada e sem versão compartilham o cache (mesmo conteúdo).
	second := do(h, "GET", base+"/v1/key/020326")
	if second.Header().Get("X-Cache") != "HIT" || st.Calls() != 1 || second.Body.String() != first.Body.String() {
		t.Errorf("segunda = %s, calls = %d", second.Header().Get("X-Cache"), st.Calls())
	}
	etag := first.Header().Get("ETag")
	if etag != etagFor("0192-v1", "key:20326") || first.Header().Get("X-Dataset-Version") != "0192-v1" ||
		first.Header().Get("Cache-Control") != "public, max-age=300" || second.Header().Get("ETag") != etag {
		t.Fatalf("cabeçalhos = %v", first.Header())
	}
	for _, inm := range []string{etag, strings.TrimPrefix(etag, "W/"), `"outro", ` + etag, "*"} {
		rec := do(h, "GET", base+"/v1/key/20326", "If-None-Match", inm)
		if rec.Code != http.StatusNotModified || rec.Body.Len() != 0 || rec.Header().Get("ETag") != etag ||
			rec.Header().Get("X-Cache") != "HIT" || rec.Header().Get("Content-Type") != "" {
			t.Errorf("If-None-Match %s = %d %v", inm, rec.Code, rec.Header())
		}
	}
	if rec := do(h, "GET", base+"/key/20326", "If-None-Match", `W/"outro"`); rec.Code != 200 {
		t.Errorf("ETag diferente = %d", rec.Code)
	}
	if st.Calls() != 1 {
		t.Errorf("304 não deveria consultar o banco (calls = %d)", st.Calls())
	}
	// Cada consulta tem o próprio ETag; /keys vale igual com e sem /v1.
	k1 := do(h, "GET", base+"/keys").Header().Get("ETag")
	k2 := do(h, "GET", base+"/v1/keys").Header().Get("ETag")
	if k1 == "" || k1 != k2 || k1 == etag || k1 != etagFor("0192-v1", "keys") {
		t.Errorf("ETag de /keys: %q, %q", k1, k2)
	}
	if !slices.Equal(c.keys(), []string{"badblock:api-rootanchors:0192-v1:key:20326", "badblock:api-rootanchors:0192-v1:keys"}) {
		t.Errorf("chaves = %v", c.keys())
	}
}

func TestHEAD(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, true)
	for _, p := range []string{base + "/keys", base + "/key/20326", base + "/v1/keys", base + "/", base + "/meta", base + "/status", base + "/ping"} {
		if rec := do(h, "HEAD", p); rec.Code != 200 {
			t.Errorf("HEAD %s = %d", p, rec.Code)
		}
	}
	rec := do(h, "HEAD", base+"/key/20326")
	etag := rec.Header().Get("ETag")
	if etag == "" || rec.Header().Get("X-Cache") != "HIT" {
		t.Errorf("HEAD sem cabeçalhos de dados: %v", rec.Header())
	}
	if rec := do(h, "HEAD", base+"/key/20326", "If-None-Match", etag); rec.Code != http.StatusNotModified {
		t.Errorf("HEAD com If-None-Match = %d", rec.Code)
	}
	if rec := do(h, "HEAD", base+"/key/1"); rec.Code != 404 {
		t.Errorf("HEAD inexistente = %d", rec.Code)
	}
	if rec := do(h, "HEAD", base+"/key/x"); rec.Code != 400 {
		t.Errorf("HEAD inválido = %d", rec.Code)
	}
}

func TestBypassAndBigBody(t *testing.T) {
	h := newAPIWith(t, &fakeStore{}, true, cache.Noop{})
	if rec := do(h, "GET", base+"/keys"); rec.Header().Get("X-Cache") != "BYPASS" {
		t.Errorf("sem cache = %s", rec.Header().Get("X-Cache"))
	}
	// Respostas acima de 8 MiB não vão para o cache.
	h, c := newAPI(t, &fakeStore{bigKey: true}, true)
	rec := do(h, "GET", base+"/keys")
	if rec.Code != 200 || rec.Body.Len() <= maxCachedBody || len(c.keys()) != 0 {
		t.Errorf("corpo grande: %d, %d bytes, chaves %v", rec.Code, rec.Body.Len(), c.keys())
	}
}

func TestNotReady(t *testing.T) {
	st := &fakeStore{noData: true}
	h, _ := newAPI(t, st, false)
	for _, p := range []string{base + "/keys", base + "/key/20326", base + "/v1/keys"} {
		rec := do(h, "GET", p)
		if rec.Code != 503 || !strings.Contains(rec.Body.String(),
			`{"error":{"code":"dataset_not_ready","message":"a primeira sincronização do collector-rootanchors ainda não terminou; tente em alguns minutos"}}`) {
			t.Errorf("%s sem dataset = %d %s", p, rec.Code, rec.Body)
		}
	}
	if st.Calls() != 0 || st.datasetCt != 0 {
		t.Errorf("sem versão não consulta o banco: calls = %d, dataset = %d", st.Calls(), st.datasetCt)
	}
	// Validação vem antes da conferência dos dados.
	if rec := do(h, "GET", base+"/key/abc"); rec.Code != 400 {
		t.Errorf("inválido sem dataset = %d", rec.Code)
	}
	rec := do(h, "GET", base+"/status")
	s := decode[StatusResponse](t, rec)
	if rec.Code != 200 || s.Status != "starting" || !s.Success || s.Checks["dataset"] != "empty" ||
		s.Message != "aguardando a primeira sincronização do collector-rootanchors" {
		t.Errorf("status sem dataset = %d %+v", rec.Code, s)
	}
	rec = do(h, "GET", base+"/meta")
	if rec.Code != 200 || rec.Body.String() != `{"app":"api-rootanchors","version":"test","dataset":null,"collector":null}`+"\n" {
		t.Errorf("meta sem dados = %d %s", rec.Code, rec.Body)
	}

	// Watcher com versão, mas o banco sem execução aplicada (corrida): 503
	// dataset_not_ready, fora do cache.
	h, c := newAPI(t, &fakeStore{noData: true}, true)
	if rec := do(h, "GET", base+"/keys"); rec.Code != 503 || !strings.Contains(rec.Body.String(), "dataset_not_ready") || len(c.keys()) != 0 {
		t.Errorf("corrida = %d %s %v", rec.Code, rec.Body, c.keys())
	}
}

func TestStatus(t *testing.T) {
	st := &fakeStore{}
	h, c := newAPI(t, st, true)
	for _, m := range []string{"GET", "POST"} {
		for _, p := range []string{base + "/health", base + "/status"} {
			rec := do(h, m, p)
			s := decode[StatusResponse](t, rec)
			if rec.Code != 200 || !s.Success || s.Status != "ok" || s.Timestamp == "" || s.Message != "api-rootanchors operacional" ||
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

	// Valkey fora sem dataset: degraded vence starting.
	h, c = newAPI(t, &fakeStore{noData: true}, false)
	c.pingErr = errors.New("valkey fora")
	if s := decode[StatusResponse](t, do(h, "GET", base+"/status")); s.Status != "degraded" || s.Checks["dataset"] != "empty" {
		t.Errorf("degraded sem dataset = %+v", s)
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
	rec := do(h, "OPTIONS", base+"/keys", "Origin", "https://exemplo.com", "Access-Control-Request-Method", "GET")
	if rec.Code != http.StatusNoContent || rec.Header().Get("Access-Control-Allow-Origin") != "*" ||
		rec.Header().Get("Access-Control-Allow-Methods") != "GET, HEAD, OPTIONS" ||
		rec.Header().Get("Access-Control-Allow-Headers") != "If-None-Match, Content-Type" ||
		rec.Header().Get("Access-Control-Max-Age") != "86400" {
		t.Errorf("preflight = %d %v", rec.Code, rec.Header())
	}
	if rec := do(h, "OPTIONS", "/qualquer/coisa"); rec.Code != http.StatusNoContent {
		t.Errorf("preflight fora da base = %d", rec.Code)
	}
	for _, p := range []string{base + "/keys", base + "/nada", base + "/key/x"} {
		hd := do(h, "GET", p).Header()
		if hd.Get("Access-Control-Allow-Origin") != "*" || hd.Get("X-Content-Type-Options") != "nosniff" ||
			hd.Get("Referrer-Policy") != "no-referrer" || hd.Get("Server") != "badblock-api-rootanchors/test" ||
			hd.Get("Access-Control-Expose-Headers") != "ETag, X-Cache, X-Dataset-Version" ||
			hd.Get("Content-Type") != "application/json; charset=utf-8" {
			t.Errorf("%s: cabeçalhos = %v", p, hd)
		}
	}
}

func TestIndexMetaAndRedirect(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, true)
	for _, p := range []string{base + "/", base + "/v1/"} {
		rec := do(h, "GET", p)
		idx := decode[IndexResponse](t, rec)
		if rec.Code != 200 || idx.App != appName || idx.BasePath != base || idx.Source != SourceURL ||
			!slices.Equal(idx.Versions, []string{"v1"}) || rec.Header().Get("Cache-Control") != "public, max-age=300" ||
			!slices.Equal(idx.Endpoints, []string{base + "/keys", base + "/key/{key_tag}", base + "/meta", base + "/status", base + "/openapi.yaml"}) {
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
		m := decode[MetaResponse](t, rec)
		if rec.Code != 200 || m.App != appName || m.Dataset == nil || m.Dataset.Keys != len(sample) ||
			m.Dataset.Source != SourceURL || m.Dataset.TrustAnchor.ID != "0C05FDD6-422C-4910-8ED6-430ED15E11C2" ||
			m.Dataset.TrustAnchor.Zone != "." || m.Collector == nil || m.Collector.App != "collector-rootanchors" ||
			m.Collector.Consolidated || m.Collector.LastSyncAt == nil || rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("meta = %d %s", rec.Code, rec.Body)
		}
		if rec.Header().Get("X-Cache") != "" || rec.Header().Get("ETag") != "" {
			t.Errorf("meta não usa cache: %v", rec.Header())
		}
	}
}
