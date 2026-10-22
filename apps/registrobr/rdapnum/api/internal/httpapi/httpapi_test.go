package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/api/internal/cache"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/api/internal/refresh"
)

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("%s: %v", rec.Body, err)
	}
	return v
}

// compact tira os espaços de um JSON escrito legível no teste.
func compact(t *testing.T, s string) string {
	t.Helper()
	var b bytes.Buffer
	if err := json.Compact(&b, []byte(s)); err != nil {
		t.Fatalf("JSON do teste: %v", err)
	}
	return b.String() + "\n"
}

const eleaJSON = `{"handle": "35980592000130", "kind": "org", "name": "ELEA DATA CENTERS", "email": null, "country": "BR",
  "document": "35.980.592/0001-30", "document_type": "cnpj", "document_digits": "35980592000130",
  "legal_representative": "REPRESENTANTE EXEMPLO",
  "registered_at": "2023-02-06T21:58:30Z", "changed_at": "2026-04-22T15:11:18Z"}`

const robelFields = `"handle": "ROBEL308", "kind": "individual", "name": "CONTATO EXEMPLO", "email": "contato@example.com", "country": "BR",
  "document": null, "document_type": null, "document_digits": null, "legal_representative": null,
  "registered_at": "2023-02-08T17:39:14Z", "changed_at": "2026-07-07T18:30:11Z"`

// O exemplo de api-rotas.md, com o uuid da resposta.
func TestASN(t *testing.T) {
	h, _ := newAPI(t, newFakeStore(), true)
	want := compact(t, `{
  "asn": 61610, "handle": "61610", "name": "ASN61610", "type": "DIRECT ALLOCATION", "country": "BR",
  "registered_at": "2023-02-27T15:58:28Z", "changed_at": "2023-02-27T15:58:28Z",
  "registrant": `+eleaJSON+`,
  "contacts": [
    {`+robelFields+`, "roles": ["abuse", "administrative"], "via": null},
    {`+robelFields+`, "roles": ["administrative"], "via": "35980592000130"}
  ],
  "networks": [
    {"prefix": "187.87.28.0/22", "type": "ALLOCATED", "status": ["active"]},
    {"prefix": "200.225.48.0/21", "type": "ALLOCATED", "status": ["active"]},
    {"prefix": "2804:8ae0::/32", "type": "ALLOCATED", "status": ["active"]}
  ],
  "scan": {"kind": "asn", "target": "61610", "source": "cgibr", "active": true,
    "checked_at": "2026-10-21T20:36:01Z", "attempted_at": "2026-10-21T20:36:01Z",
    "last_result": "complete", "retry_at": null, "response": "`+r61610+`"}
}`)
	for _, p := range []string{"/asn/61610", "/asn/AS61610", "/v1/asn/as61610", "/asn/061610"} {
		rec := do(h, "GET", base+p)
		if rec.Code != 200 || rec.Body.String() != want {
			t.Errorf("%s = %d\n%s\nquero\n%s", p, rec.Code, rec.Body, want)
		}
	}

	// Alvo sem dado processado: 200 com os campos do ASN null e o scan.
	cases := map[string]string{
		"/asn/64500": `{"asn": 64500, "handle": null, "name": null, "type": null, "country": null, "registered_at": null,
  "changed_at": null, "registrant": null, "contacts": [], "networks": [],
  "scan": {"kind": "asn", "target": "64500", "source": "static", "active": true, "checked_at": null,
    "attempted_at": null, "last_result": null, "retry_at": null, "response": null}}`,
		"/asn/64501": `{"asn": 64501, "handle": null, "name": null, "type": null, "country": null, "registered_at": null,
  "changed_at": null, "registrant": null, "contacts": [], "networks": [],
  "scan": {"kind": "asn", "target": "64501", "source": "cgibr", "active": false, "checked_at": "2026-10-20T10:00:00Z",
    "attempted_at": "2026-10-20T10:00:00Z", "last_result": "not_found", "retry_at": null, "response": "` + r64501 + `"}}`,
		"/asn/64502": `{"asn": 64502, "handle": null, "name": null, "type": null, "country": null, "registered_at": null,
  "changed_at": null, "registrant": null, "contacts": [], "networks": [],
  "scan": {"kind": "asn", "target": "64502", "source": "cgibr", "active": true, "checked_at": null,
    "attempted_at": null, "last_result": "error", "retry_at": "2026-10-21T21:00:00Z", "response": null}}`,
	}
	for p, w := range cases {
		if rec := do(h, "GET", base+p); rec.Code != 200 || rec.Body.String() != compact(t, w) {
			t.Errorf("%s = %d %s", p, rec.Code, rec.Body)
		}
	}
}

func TestNetwork(t *testing.T) {
	h, _ := newAPI(t, newFakeStore(), true)
	want := compact(t, `{
  "query": "200.225.48.0/24",
  "prefix": "200.225.48.0/21", "start_address": "200.225.48.0", "end_address": "200.225.55.255", "ip_version": 4,
  "handle": "200.225.48.0/21", "name": "462539", "type": "ALLOCATED", "country": "BR", "status": ["active"],
  "parent_handle": null, "asn": 61610, "registered_at": "1999-11-18T12:00:00Z", "changed_at": "2023-07-19T11:05:57Z",
  "registrant": `+eleaJSON+`,
  "contacts": [{`+robelFields+`, "roles": ["abuse", "technical"], "via": null}],
  "reverse": [{"prefix": "200.225.48.0/24", "start_address": "200.225.48.0", "end_address": "200.225.48.255",
    "nameservers": [
      {"name": "keenan.ns.cloudflare.com", "status": ["ns aa"], "checked_at": "2026-10-21T12:00:00Z", "last_ok_at": "2026-10-21T12:00:00Z"},
      {"name": "sureena.ns.cloudflare.com", "status": ["ns aa"], "checked_at": "2026-10-21T12:00:00Z", "last_ok_at": "2026-10-21T12:00:00Z"}],
    "signed": false, "registered_at": "2024-09-27T15:31:56Z", "changed_at": "2024-09-27T15:31:56Z"}],
  "scan": {"kind": "prefix", "target": "200.225.48.0/21", "source": "cgibr", "active": true,
    "checked_at": "2026-10-21T20:36:01Z", "attempted_at": "2026-10-21T20:36:01Z",
    "last_result": "complete", "retry_at": null, "response": "`+r200225+`"}
}`)
	for _, p := range []string{"/prefix/200.225.48.0/24", "/v1/prefix/200.225.48.77/24"} {
		if rec := do(h, "GET", base+p); rec.Code != 200 || rec.Body.String() != want {
			t.Errorf("%s = %d\n%s\nquero\n%s", p, rec.Code, rec.Body, want)
		}
	}
	for p, q := range map[string]string{
		"/ip/200.225.48.10":              "200.225.48.10",
		"/ip/::ffff:200.225.48.10":       "200.225.48.10",
		"/prefix/200.225.48.0/21":        "200.225.48.0/21",
		"/prefix/200.225.48.0/32":        "200.225.48.0/32",
		"/prefix/::ffff:200.225.48.0/24": "200.225.48.0/24",
	} {
		got := decode[NetworkResponse](t, do(h, "GET", base+p))
		if got.Query != q || got.Prefix != "200.225.48.0/21" {
			t.Errorf("%s = %s → %s", p, got.Query, got.Prefix)
		}
	}
	// IPv6: o bloco do alvo truncado na última tentativa, com o dado da
	// última completa; delegação sem CIDR exato e sem servidores.
	got := decode[NetworkResponse](t, do(h, "GET", base+"/ip/2804:8ae0:4000::1"))
	if got.Prefix != "2804:8ae0::/32" || got.IPVersion != 6 || got.EndAddress != "2804:8ae0:ffff:ffff:ffff:ffff:ffff:ffff" ||
		len(got.Reverse) != 1 || got.Reverse[0].Prefix != nil || got.Reverse[0].Nameservers == nil ||
		got.Scan.LastResult == nil || *got.Scan.LastResult != "truncated" || got.Scan.RetryAt == nil ||
		*got.Scan.CheckedAt != "2026-10-14T08:00:00Z" {
		t.Errorf("IPv6 = %+v", got)
	}
	if rec := do(h, "GET", base+"/ip/2804:8ae0::1"); !strings.Contains(rec.Body.String(), `"nameservers":[]`) {
		t.Errorf("nameservers vazio = %s", rec.Body)
	}
	// Bloco sem delegação reversa: reverse [].
	if rec := do(h, "GET", base+"/ip/187.87.29.10"); !strings.Contains(rec.Body.String(), `"reverse":[]`) {
		t.Errorf("sem reverso = %s", rec.Body)
	}
}

func TestDocumentAndContact(t *testing.T) {
	h, _ := newAPI(t, newFakeStore(), true)
	want := compact(t, `{"document_digits": "35980592000130", "document_type": "cnpj", "entities": [`+eleaJSON+`],
  "asns": [{"asn": 61610, "name": "ASN61610", "scan": {"checked_at": "2026-10-21T20:36:01Z"}}],
  "networks": [
    {"prefix": "187.87.28.0/22", "asn": 61610, "type": "ALLOCATED", "status": ["active"]},
    {"prefix": "200.225.48.0/21", "asn": 61610, "type": "ALLOCATED", "status": ["active"]}]}`)
	for _, p := range []string{"/document/35980592000130", "/document/35.980.592/0001-30", "/v1/document/35.980.592%2F0001-30"} {
		if rec := do(h, "GET", base+p); rec.Code != 200 || rec.Body.String() != want {
			t.Errorf("%s = %d %s", p, rec.Code, rec.Body)
		}
	}
	want = compact(t, `{"entity": {`+robelFields+`}, "objects": [
  {"kind": "asn", "asn": 61610, "prefix": null, "roles": ["abuse", "administrative"], "via": null},
  {"kind": "asn", "asn": 61610, "prefix": null, "roles": ["administrative"], "via": "35980592000130"},
  {"kind": "prefix", "asn": null, "prefix": "200.225.48.0/21", "roles": ["abuse", "technical"], "via": null}]}`)
	if rec := do(h, "GET", base+"/contact/ROBEL308"); rec.Code != 200 || rec.Body.String() != want {
		t.Errorf("contato = %d\n%s\nquero\n%s", rec.Code, rec.Body, want)
	}
	got := decode[ContactResponse](t, do(h, "GET", base+"/contact/35980592000130"))
	if len(got.Objects) != 3 || got.Objects[0].Roles[0] != "registrant" || got.Objects[2].Prefix == nil ||
		*got.Objects[2].Prefix != "200.225.48.0/21" {
		t.Errorf("contato titular = %+v", got)
	}
}

func TestErrors(t *testing.T) {
	h, _ := newAPI(t, newFakeStore(), true)
	cases := map[string]struct {
		code int
		msg  string
	}{
		"/asn/1":                              {404, "AS1 não é alvo do rdapnum"},
		"/asn/4294967296":                     {400, "ASN inválido"},
		"/asn/-1":                             {400, "ASN inválido"},
		"/asn/+5":                             {400, "ASN inválido"},
		"/asn/AS":                             {400, "ASN inválido"},
		"/asn/ASN61610":                       {400, "ASN inválido"},
		"/asn/As61610":                        {400, "ASN inválido"},
		"/ip/8.8.8.8":                         {404, "8.8.8.8 não está em nenhum bloco do rdapnum"},
		"/ip/abc":                             {400, "endereço IP inválido"},
		"/ip/fe80::1%25eth0":                  {400, "endereço IP inválido"},
		"/prefix/8.8.8.0/24":                  {404, "8.8.8.0/24 não está em nenhum bloco do rdapnum"},
		"/prefix/200.225.48.0/33":             {400, "tamanho de prefixo inválido"},
		"/prefix/2804:8ae0::/129":             {400, "tamanho de prefixo inválido"},
		"/prefix/200.225.48.0/x":              {400, "tamanho de prefixo inválido"},
		"/prefix/200.225.48.0/08":             {400, "tamanho de prefixo inválido"},
		"/prefix/999.1.1.1/24":                {400, "endereço IP inválido"},
		"/document/123":                       {400, "documento inválido: use CNPJ (14 dígitos) ou CPF (11)"},
		"/document/3598059200013a":            {400, "documento inválido: use CNPJ (14 dígitos) ou CPF (11)"},
		"/document/11144477735":               {404, "documento 11144477735 não encontrado no rdapnum"},
		"/contact/NINGUEM":                    {404, "contato NINGUEM não encontrado no rdapnum"},
		"/contact/" + strings.Repeat("A", 65): {400, "handle inválido"},
		"/contact/ROB%21":                     {400, "handle inválido"},
		"/response/xyz":                       {400, "uuid inválido"},
		"/response/019a0a4e-0000-7000-8000-000000000000": {404, "resposta 019a0a4e-0000-7000-8000-000000000000 não encontrada"},
		"/asn/1/raw":                 {404, "AS1 não é alvo do rdapnum"},
		"/asn/64500/raw":             {404, "AS64500 sem resposta coletada"},
		"/asn/64501/raw":             {404, "AS64501 sem resposta coletada"},
		"/prefix/187.87.28.0/22/raw": {404, "187.87.28.0/22 sem resposta coletada"},
		"/prefix/8.8.8.0/24/raw":     {404, "8.8.8.0/24 não é alvo do rdapnum"},
		"/asn/1/history":             {404, "AS1 não é alvo do rdapnum"},
		"/prefix/8.8.8.0/24/history": {404, "8.8.8.0/24 não é alvo do rdapnum"},
		"/asns":                      {404, "rota inexistente; veja /registrobr/rdapnum/"},
		"/v2/asn/61610":              {404, "rota inexistente; veja /registrobr/rdapnum/"},
	}
	for p, want := range cases {
		rec := do(h, "GET", base+p)
		if rec.Code != want.code {
			t.Errorf("%s: %d, quero %d (%s)", p, rec.Code, want.code, rec.Body)
			continue
		}
		e := decode[errorBody](t, rec)
		if e.Error.Message != want.msg || rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("ETag") != "" {
			t.Errorf("%s: %q, quero %q", p, e.Error.Message, want.msg)
		}
	}
	// A validação vem antes de tudo, mesmo sem alvos.
	h, _ = newAPI(t, newFakeStore(), false)
	if rec := do(h, "GET", base+"/asn/x"); rec.Code != 400 {
		t.Errorf("400 antes do 503: %d", rec.Code)
	}
}

func TestRaw(t *testing.T) {
	st := newFakeStore()
	h, c := newAPI(t, st, true)
	rec := do(h, "GET", base+"/asn/61610/raw")
	body := rec.Body.String()
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/rdap+json" ||
		rec.Header().Get("X-Response-UUID") != r61610 || rec.Header().Get("Last-Modified") != "Wed, 21 Oct 2026 20:36:01 GMT" {
		t.Fatalf("raw = %d %v %s", rec.Code, rec.Header(), body)
	}
	// Sem a linha "<hora> - <ip>", com o resto do aviso, na ordem em que veio.
	want := `{"name":"ASN61610","type":"DIRECT ALLOCATION","handle":"61610","port43":"whois.nic.br","country":"BR","endAutnum":61610,` +
		`"notices":[{"title":"Use and Privacy Policy","description":["Copyright (c) Nic.br - Use of this data is governed by the Use and Privacy Policy. Distribution, commercialization, reproduction, and use for advertising or similar purposes are expressly prohibited."]}],` +
		`"startAutnum":61610,"objectClassName":"autnum"}` + "\n"
	if body != want {
		t.Errorf("raw =\n%s\nquero\n%s", body, want)
	}
	// Do cache, com os mesmos cabeçalhos.
	rec2 := do(h, "GET", base+"/v1/asn/AS61610/raw")
	if rec2.Header().Get("X-Cache") != "HIT" || rec2.Body.String() != want || rec2.Header().Get("X-Response-UUID") != r61610 ||
		rec2.Header().Get("Content-Type") != "application/rdap+json" || rec2.Header().Get("ETag") != rec.Header().Get("ETag") {
		t.Errorf("raw do cache = %v %s", rec2.Header(), rec2.Body)
	}
	if c.get(keyPrefix+"asn-raw:61610") == nil {
		t.Error("chave asn-raw:61610 ausente")
	}
	// Bloco: sem alvo exato, o alvo do bloco que contém o pedido; IPv6 no aviso.
	rec = do(h, "GET", base+"/prefix/200.225.48.0/24/raw")
	if rec.Code != 200 || rec.Header().Get("X-Response-UUID") != r200225 || strings.Contains(rec.Body.String(), "2001:db8") ||
		!strings.Contains(rec.Body.String(), `"description":["Copyright (c) Nic.br"]`) {
		t.Errorf("prefix raw = %d %s", rec.Code, rec.Body)
	}
	if c.get(keyPrefix+"prefix-raw:200.225.48.0/24") == nil {
		t.Error("chave prefix-raw:200.225.48.0/24 ausente")
	}
}

func TestHistoryAndResponse(t *testing.T) {
	h, _ := newAPI(t, newFakeStore(), true)
	rec := do(h, "GET", base+"/asn/61610/history")
	got := decode[HistoryResponse](t, rec)
	if rec.Code != 200 || got.Target != "61610" || got.Kind != "asn" || got.Current == nil || *got.Current != r61610 ||
		len(got.Responses) != 2 || got.Responses[0].UUID != r61610 || got.Responses[1].Result != "truncated" ||
		got.Responses[1].Processed || !got.Responses[0].Processed || got.Responses[0].SeenCount != 3 {
		t.Errorf("history = %s", rec.Body)
	}
	for _, k := range []string{"egress_ip", "server_time", "192.0.2"} {
		if strings.Contains(rec.Body.String(), k) {
			t.Errorf("history mostra %s: %s", k, rec.Body)
		}
	}
	// Alvo sem resposta: responses [].
	if got := decode[HistoryResponse](t, do(h, "GET", base+"/asn/64500/history")); got.Current != nil || got.Responses == nil || len(got.Responses) != 0 {
		t.Errorf("history vazio = %+v", got)
	}
	got = decode[HistoryResponse](t, do(h, "GET", base+"/prefix/200.225.48.0/24/history"))
	if got.Target != "200.225.48.0/21" || got.Kind != "prefix" || len(got.Responses) != 1 {
		t.Errorf("history do bloco = %+v", got)
	}

	// Uma versão truncada do histórico, sem a linha do IP.
	rec = do(h, "GET", base+"/response/"+strings.ToUpper(r61610old))
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "public, max-age=86400" ||
		rec.Header().Get("X-Response-UUID") != r61610old || strings.Contains(rec.Body.String(), "192.0.2") ||
		!strings.Contains(rec.Body.String(), "object truncated due to server policy") {
		t.Errorf("response truncada = %d %v %s", rec.Code, rec.Header(), rec.Body)
	}
	// Uma not_found.
	rec = do(h, "GET", base+"/v1/response/"+r64501)
	if rec.Code != 200 || rec.Body.String() != `{"result":"not_found","http_status":404}`+"\n" ||
		rec.Header().Get("Content-Type") != "application/json; charset=utf-8" || rec.Header().Get("X-Response-UUID") != r64501 {
		t.Errorf("response not_found = %d %v %s", rec.Code, rec.Header(), rec.Body)
	}
}

func TestRefresh(t *testing.T) {
	st := newFakeStore()
	rf := &fakeRefresher{out: refresh.Outcome{Queued: true}}
	h := newTestAPI(t, st, true, &memCache{m: map[string][]byte{}}, rf).Handler()

	rec := do(h, "POST", base+"/asn/AS61610/refresh")
	if rec.Code != 202 || rec.Body.String() != `{"kind":"asn","target":"61610","queued":true,"reason":null,"checked_at":"2026-10-21T20:36:01Z"}`+"\n" {
		t.Fatalf("refresh = %d %s", rec.Code, rec.Body)
	}
	if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("ETag") != "" || rec.Header().Get("X-Cache") != "BYPASS" {
		t.Errorf("cabeçalhos = %v", rec.Header())
	}
	if len(rf.calls) != 1 || rf.calls[0] != (refresh.Request{Kind: "asn", Target: "61610"}) || rf.scans[0] == nil {
		t.Errorf("pedidos = %+v", rf.calls)
	}

	// Bloco sem alvo exato: cai no alvo do bloco que contém o pedido.
	rf.out = refresh.Outcome{Reason: refresh.Recent}
	rec = do(h, "POST", base+"/v1/prefix/200.225.48.0/24/refresh")
	if rec.Code != 202 || rec.Body.String() != `{"kind":"prefix","target":"200.225.48.0/21","queued":false,"reason":"recent","checked_at":"2026-10-21T20:36:01Z"}`+"\n" {
		t.Errorf("refresh do bloco = %d %s", rec.Code, rec.Body)
	}
	rf.out = refresh.Outcome{Reason: refresh.AlreadyQueued}
	if got := decode[RefreshResponse](t, do(h, "POST", base+"/asn/64500/refresh")); got.Queued || *got.Reason != "already_queued" || got.CheckedAt != nil {
		t.Errorf("already_queued = %+v", got)
	}

	errCases := map[string]struct {
		code       int
		errCode    string
		msg        string
		refreshErr error
	}{
		"/asn/1/refresh":             {404, "not_found", "AS1 não é alvo do rdapnum", nil},
		"/prefix/8.8.8.0/24/refresh": {404, "not_found", "8.8.8.0/24 não é alvo do rdapnum", nil},
		"/asn/64501/refresh":         {409, "target_inactive", "AS64501 está desativado no rdapnum", nil},
		"/asn/x/refresh":             {400, "bad_request", "ASN inválido", nil},
		"/asn/61610/refresh":         {503, "refresh_unavailable", "fila de atualização cheia; tente mais tarde", refresh.ErrQueueFull},
		"/prefix/200.225.48.0/21/refresh": {503, "refresh_unavailable", "atualização indisponível: Valkey fora do ar",
			errors.Join(refresh.ErrUnavailable, errors.New("conexão recusada"))},
	}
	for p, want := range errCases {
		rf.err = want.refreshErr
		rec := do(h, "POST", base+p)
		e := decode[errorBody](t, rec)
		if rec.Code != want.code || e.Error.Code != want.errCode || e.Error.Message != want.msg {
			t.Errorf("%s = %d %s", p, rec.Code, rec.Body)
		}
	}

	// Sem Valkey: 503.
	h = newTestAPI(t, newFakeStore(), true, cache.Noop{}, nil).Handler()
	rec = do(h, "POST", base+"/asn/61610/refresh")
	if e := decode[errorBody](t, rec); rec.Code != 503 || e.Error.Message != "atualização indisponível: Valkey fora do ar" {
		t.Errorf("sem Valkey = %d %s", rec.Code, rec.Body)
	}
	// GET na rota do POST é 404.
	if rec := do(h, "GET", base+"/asn/61610/refresh"); rec.Code != 404 {
		t.Errorf("GET refresh = %d", rec.Code)
	}
}

func TestCacheAndETag(t *testing.T) {
	st := newFakeStore()
	h, c := newAPI(t, st, true)

	first := do(h, "GET", base+"/asn/61610")
	if first.Header().Get("X-Cache") != "MISS" {
		t.Errorf("primeira = %s", first.Header().Get("X-Cache"))
	}
	calls := st.calls
	// Versionada e sem versão compartilham o cache e o ETag.
	second := do(h, "GET", base+"/v1/asn/AS61610")
	if second.Header().Get("X-Cache") != "HIT" || st.calls != calls {
		t.Errorf("segunda = %s, calls = %d", second.Header().Get("X-Cache"), st.calls)
	}
	if c.get(keyPrefix+"asn:61610") == nil {
		t.Errorf("chave sem versão ausente; chaves: %v", c.m)
	}
	etag := first.Header().Get("ETag")
	if etag == "" || etag != second.Header().Get("ETag") || etag != etagFor(first.Body.Bytes()) {
		t.Fatalf("ETag = %q / %q", etag, second.Header().Get("ETag"))
	}
	if first.Header().Get("X-Dataset-Version") != "" || first.Header().Get("Cache-Control") != "public, max-age=300" {
		t.Errorf("cabeçalhos = %v", first.Header())
	}
	if got := first.Header().Get("Access-Control-Expose-Headers"); got != "ETag, X-Cache, X-Response-UUID" {
		t.Errorf("Expose-Headers = %q", got)
	}
	rec := do(h, "GET", base+"/asn/61610", "If-None-Match", etag)
	if rec.Code != http.StatusNotModified || rec.Body.Len() != 0 || rec.Header().Get("ETag") != etag || rec.Header().Get("X-Cache") != "HIT" {
		t.Errorf("If-None-Match = %d %q", rec.Code, rec.Body)
	}
	if rec := do(h, "GET", base+"/asn/61610", "If-None-Match", `W/"0"`); rec.Code != 200 {
		t.Errorf("ETag diferente = %d", rec.Code)
	}
	// Chaves normalizadas.
	for p, k := range map[string]string{
		"/ip/::ffff:200.225.48.10":             "ip:200.225.48.10",
		"/prefix/200.225.48.77/24":             "prefix:200.225.48.0/24",
		"/document/35.980.592/0001-30":         "document:35980592000130",
		"/contact/ROBEL308":                    "contact:ROBEL308",
		"/asn/61610/history":                   "asn-history:61610",
		"/prefix/200.225.48.9/24/history":      "prefix-history:200.225.48.0/24",
		"/response/" + strings.ToUpper(r61610): "response:" + r61610,
	} {
		do(h, "GET", base+p)
		if c.get(keyPrefix+k) == nil {
			t.Errorf("%s: chave %s ausente", p, k)
		}
	}
	// 404 não entra no cache.
	do(h, "GET", base+"/asn/1")
	if c.get(keyPrefix+"asn:1") != nil {
		t.Error("404 no cache")
	}
}

func TestNotReady(t *testing.T) {
	h, _ := newAPI(t, newFakeStore(), false)
	for _, p := range []string{"/asn/61610", "/ip/200.225.48.10", "/prefix/200.225.48.0/24", "/document/35980592000130",
		"/contact/ROBEL308", "/asn/61610/raw", "/asn/61610/history", "/response/" + r61610} {
		rec := do(h, "GET", base+p)
		if rec.Code != 503 || decode[errorBody](t, rec).Error.Code != "dataset_not_ready" {
			t.Errorf("%s sem alvos = %d %s", p, rec.Code, rec.Body)
		}
	}
	s := decode[StatusResponse](t, do(h, "GET", base+"/status"))
	if s.Status != "starting" || s.Checks["dataset"] != "empty" || s.Message != "aguardando a primeira derivação do collector-registrobr-rdapnum" {
		t.Errorf("status sem alvos = %+v", s)
	}
	if rec := do(h, "GET", base+"/meta"); rec.Code != 200 {
		t.Errorf("meta sem alvos = %d", rec.Code)
	}
}

func TestDatabaseErrors(t *testing.T) {
	st := newFakeStore()
	st.queryErr = errors.New("conexão recusada")
	h, _ := newAPI(t, st, true)
	if rec := do(h, "GET", base+"/asn/61610"); rec.Code != 503 || decode[errorBody](t, rec).Error.Code != "database_unavailable" {
		t.Errorf("banco fora = %d %s", rec.Code, rec.Body)
	}
	if rec := do(h, "POST", base+"/asn/61610/refresh"); rec.Code != 503 || decode[errorBody](t, rec).Error.Code != "database_unavailable" {
		t.Errorf("refresh com banco fora = %d %s", rec.Code, rec.Body)
	}
	st.queryErr = context.DeadlineExceeded
	if rec := do(h, "GET", base+"/ip/200.225.48.1"); rec.Code != 504 {
		t.Errorf("prazo = %d %s", rec.Code, rec.Body)
	}
}

func TestStatus(t *testing.T) {
	st := newFakeStore()
	c := &memCache{m: map[string][]byte{}}
	h := newTestAPI(t, st, true, c, nil).Handler()
	for _, m := range []string{"GET", "POST"} {
		for _, p := range []string{"/health", "/status"} {
			s := decode[StatusResponse](t, do(h, m, base+p))
			if !s.Success || s.Status != "ok" || s.Message != "api-registrobr-rdapnum operacional" || s.Checks["valkey"] != "ok" {
				t.Errorf("%s %s = %+v", m, p, s)
			}
		}
	}
	c.down = true
	s := decode[StatusResponse](t, do(h, "GET", base+"/status"))
	if s.Status != "degraded" || s.Message != "Valkey indisponível; respondendo sem cache e sem atualização" {
		t.Errorf("Valkey fora = %+v", s)
	}
	st.pingErr = errors.New("down")
	rec := do(h, "GET", base+"/status")
	if rec.Code != 503 || decode[StatusResponse](t, rec).Status != "error" {
		t.Errorf("Postgres fora = %d %s", rec.Code, rec.Body)
	}
	// Sem cache, só a atualização: o ping é o dela.
	rf := &fakeRefresher{pingErr: errors.New("fora")}
	h = newTestAPI(t, newFakeStore(), true, cache.Noop{}, rf).Handler()
	if s := decode[StatusResponse](t, do(h, "GET", base+"/status")); s.Checks["valkey"] != "error" || s.Status != "degraded" {
		t.Errorf("atualização fora = %+v", s)
	}
	h = newTestAPI(t, newFakeStore(), true, cache.Noop{}, nil).Handler()
	if s := decode[StatusResponse](t, do(h, "GET", base+"/status")); s.Checks["valkey"] != "disabled" {
		t.Errorf("sem Valkey = %+v", s)
	}
	if rec := do(h, "GET", base+"/ping"); rec.Body.String() != "pong" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("ping = %q", rec.Body)
	}
}

func TestIndexMetaAndRedirect(t *testing.T) {
	rf := &fakeRefresher{queue: 3}
	h := newTestAPI(t, newFakeStore(), true, &memCache{m: map[string][]byte{}}, rf).Handler()
	idx := decode[IndexResponse](t, do(h, "GET", base+"/"))
	if idx.App != "api-registrobr-rdapnum" || idx.BasePath != base || !slices.Equal(idx.DerivedFrom, []string{"cgibr"}) ||
		idx.Source != SourceURL || idx.Upstream != "https://rdap.registro.br/" || !slices.Contains(idx.Endpoints, base+"/asn/{asn}") {
		t.Errorf("índice = %+v", idx)
	}
	if rec := do(h, "GET", base+"/v1/"); rec.Code != 200 {
		t.Errorf("índice v1 = %d", rec.Code)
	}
	if rec := do(h, "GET", base); rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != base+"/" {
		t.Errorf("redirect = %d %s", rec.Code, rec.Header().Get("Location"))
	}
	rec := do(h, "GET", base+"/meta")
	m := decode[MetaResponse](t, rec)
	if rec.Header().Get("Cache-Control") != "no-store" || m.App != "api-registrobr-rdapnum" || m.Targets.Total != 7 ||
		m.Targets.Waiting != 2 || m.Targets.OldestScanAt == nil || m.Responses.Truncated != 1 || m.Data.Network != 3 ||
		m.Config.RateLimit == nil || *m.Config.RateLimit != 48 || m.Config.ScanInterval != nil ||
		len(m.Derivations) != 1 || *m.Derivations[0].ASNs != 9134 || len(m.Workers) != 1 || !m.Workers[0].Proxy ||
		m.Workers[0].EgressIPs != 37 || m.Collector == nil || m.Collector.App != "collector-registrobr-rdapnum" ||
		m.Refresh.Queue == nil || *m.Refresh.Queue != 3 {
		t.Errorf("meta = %s", rec.Body)
	}
	for _, k := range []string{"egress_ip\"", "proxy_url", "192.0.2"} {
		if strings.Contains(rec.Body.String(), k) {
			t.Errorf("o /meta mostra %s: %s", k, rec.Body)
		}
	}
	// Sem Valkey: refresh.queue null.
	h, _ = newAPI(t, newFakeStore(), true)
	if rec := do(h, "GET", base+"/meta"); !strings.Contains(rec.Body.String(), `"refresh":{"queue":null}`) {
		t.Errorf("meta sem Valkey = %s", rec.Body)
	}
}

func TestHeadAndOptions(t *testing.T) {
	h, _ := newAPI(t, newFakeStore(), true)
	srv := httptest.NewServer(h)
	defer srv.Close()
	resp, err := http.Head(srv.URL + base + "/asn/61610/raw")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || len(body) != 0 || resp.Header.Get("ETag") == "" || resp.Header.Get("X-Response-UUID") != r61610 {
		t.Errorf("HEAD = %d %q %v", resp.StatusCode, body, resp.Header)
	}
	rec := do(h, "OPTIONS", base+"/asn/61610/refresh")
	if rec.Code != 204 || rec.Header().Get("Access-Control-Allow-Methods") != "GET, HEAD, POST, OPTIONS" {
		t.Errorf("OPTIONS = %d %v", rec.Code, rec.Header())
	}
	if rec := do(h, "POST", base+"/asn/61610"); rec.Code != 404 {
		t.Errorf("POST em rota GET = %d", rec.Code)
	}
}

func TestPublicRDAP(t *testing.T) {
	cases := map[string]string{
		// a linha do aviso, IPv4 e IPv6
		`{"notices":[{"description":["a","2026-10-21T17:36:01-03:00 - 192.0.2.1"]}]}`:   `{"notices":[{"description":["a"]}]}`,
		`{"notices":[{"description":["2026-10-21T17:36:01-03:00 - 2001:db8::1","b"]}]}`: `{"notices":[{"description":["b"]}]}`,
		// formato diferente, mas com IP solto: sai também
		`{"notices":[{"description":["consultado por 192.0.2.1 às 17h"]}]}`: `{"notices":[{"description":[]}]}`,
		// notices fora do formato some inteiro
		`{"notices":"192.0.2.1","x":1}`: `{"notices":[],"x":1}`,
		// nada para tirar; a ordem fica
		`{"z":1,"a":{"b":2},"notices":[{"title":"t","links":[]}]}`: `{"z":1,"a":{"b":2},"notices":[{"title":"t","links":[]}]}`,
	}
	for in, want := range cases {
		got, err := publicRDAP(in)
		if err != nil || string(got) != want+"\n" {
			t.Errorf("%s → %s (%v), quero %s", in, got, err, want)
		}
	}
	if _, err := publicRDAP(`[1]`); err == nil {
		t.Error("lista na raiz deveria falhar")
	}
}

func TestEntryEncoding(t *testing.T) {
	e := &entry{body: []byte("{}\n"), contentType: "application/rdap+json", responseUUID: r61610, lastModified: "Wed, 21 Oct 2026 20:36:01 GMT"}
	got := decodeEntry(e.encode())
	if got == nil || !bytes.Equal(got.body, e.body) || got.contentType != e.contentType || got.responseUUID != e.responseUUID || got.lastModified != e.lastModified {
		t.Errorf("ida e volta = %+v", got)
	}
	plain := &entry{body: []byte(`{"a":1}` + "\n")}
	if !bytes.Equal(plain.encode(), plain.body) || string(decodeEntry(plain.body).body) != string(plain.body) {
		t.Error("JSON puro vai para o cache como está")
	}
	if decodeEntry([]byte(rawEntry+"x")) != nil {
		t.Error("entrada truncada deveria ser ignorada")
	}
}
