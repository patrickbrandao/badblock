package httpapi

import (
	"cmp"
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

	"github.com/patrickbrandao/badblock/apps/anatel/pst/api/internal/cache"
	"github.com/patrickbrandao/badblock/apps/anatel/pst/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/anatel/pst/api/internal/realip"
	"github.com/patrickbrandao/badblock/apps/anatel/pst/api/internal/store"
)

const base = "/anatel/pst"

func day(s string) *time.Time {
	t, err := time.Parse("02/01/2006", s)
	if err != nil {
		panic(err)
	}
	return &t
}

// sic monta um serviço de uma outorga SIC, como o coletor grava.
func sic(code, name, group, grantFistel, grantProcess, grantedOn, fistel, process, notifiedOn string) store.Service {
	return store.Service{
		ServiceCode: code, ServiceName: name, ServiceGroup: group,
		NotificationFistel: fistel, NotificationProcess: new(process), NotifiedOn: day(notifiedOn),
		EntityType: "Outorgada", GrantType: "Serviços de Interesse Coletivo e Restrito - SIC",
		GrantFistel: new(grantFistel), GrantProcess: new(grantProcess), GrantedOn: day(grantedOn),
	}
}

// Recorte real do CSV de 2026-09-30, como o collector-anatel-pst grava (na
// ordem do store): a Telefônica (sem nome fantasia, com um trecho dos 42
// serviços), a Copa Energia (nome fantasia, duas outorgas SIR com a mesma
// notificação) e a Transat (uma notificação dispensada de outorga). Telefone
// e e-mail só na Telefônica.
var sampleProviders = []store.ProviderDetail{
	{
		Provider: store.Provider{
			Document: "02558157000162", Name: "TELEFONICA BRASIL S.A.",
			Street: new("Avenida Engenheiro Luiz Carlos Berrini"), Number: new("1376"),
			Complement: new("Telefônica Brasil S/A"), District: new("Cidade Monções"), PostalCode: new("04571936"),
			CityIBGECode: new(3550308), City: new("São Paulo"), State: new("SP"),
			Phone: new("(11) 3430-4532"), Email: new("cadastro.fiscal.br@telefonica.com"),
		},
		Services: []store.Service{
			sic("010", "SERVIÇO MOVEL PESSOAL", "Telefonia Móvel", "50423150120", "53500036134202046", "05/01/2021", "50409146285", "535000247042011", "03/04/2012"),
			sic("045", "Serviço de Comunicação Multimídia", "Banda Larga Fixa", "50423150120", "53500036134202046", "05/01/2021", "50013053736", "535000020652002", "13/02/2003"),
			sic("171", "SERVICO TELEFONICO FIXO COMUTADO", "Telefonia Fixa", "50423150120", "53500036134202046", "05/01/2021", "50001358308", "535000012622003", "19/03/1998"),
			sic("171", "SERVICO TELEFONICO FIXO COMUTADO", "Telefonia Fixa", "50423150120", "53500036134202046", "05/01/2021", "50003954846", "535160014082016", "01/10/1999"),
			sic("750", "Serviço de Acesso Condicionado", "Tv por Assinatura", "50423150120", "53500036134202046", "05/01/2021", "50411491199", "535000240602011", "15/04/2014"),
		},
	},
	{
		Provider: store.Provider{
			Document: "03237583006602", Name: "Copa Energia Distribuidora de Gas S A", TradeName: new("Copa Energia"),
			Street: new("Rua Dalton Lahm dos Reis"), Number: new("260"), Complement: new("Sala A"), District: new("Cidade Nova"),
			PostalCode: new("95112090"), CityIBGECode: new(4305108), City: new("Caxias do Sul"), State: new("RS"),
		},
		Services: func() []store.Service {
			sir := func(grant, grantProcess, grantedOn, code, name, fistel, process, notifiedOn string) store.Service {
				s := sic(code, name, "Limitado Privado", grant, grantProcess, grantedOn, fistel, process, notifiedOn)
				s.GrantType = "Serviços de Interesse Restrito - SIR"
				return s
			}
			return []store.Service{
				sir("50448007169", "53528001299202412", "25/04/2024", "019", "Limitado Privado", "50448153149", "53528001299202412", "13/05/2024"),
				sir("50455705445", "53500036134202046", "15/07/2026", "019", "Limitado Privado", "50448153149", "53528001299202412", "13/05/2024"),
				sir("50448007169", "53528001299202412", "25/04/2024", "028", "Limitado Privado Estações Itinerantes", "50455705607", "535280007212004", "15/07/2026"),
			}
		}(),
	},
	{
		Provider: store.Provider{
			Document: "21557625000129", Name: "TRANSAT TELECOMUNICACOES VIA SATELITE EIRELI",
			Street: new("RUA RIO GRANDE DO NORTE"), Number: new("2668"), Complement: new("SALA  06"), District: new("UMUARAMA"),
			PostalCode: new("38405321"), CityIBGECode: new(3170206), City: new("Uberlândia"), State: new("MG"),
		},
		Services: []store.Service{
			sic("045", "Serviço de Comunicação Multimídia", "Banda Larga Fixa", "50426784685", "53500036134202046", "05/01/2021", "50416620884", "53500018551201892", "29/05/2018"),
			sic("110", "Serviço Móvel Pessoal por Satélite", "Telefonia Móvel por Satélite", "50426784685", "53500036134202046", "05/01/2021", "50453623050", "53500041483202594", "04/12/2025"),
			sic("182", "LIMITADO ESPECIALIZADO POR SATELITE (atual SLP por satélite prestação a terceiros)", "Limitado Privado", "50426784685", "53500036134202046", "05/01/2021", "50412699559", "535000056402015", "02/06/2015"),
			{
				ServiceCode: "190", ServiceName: "Limitado Privado - Dispensa de Autorização", ServiceGroup: "Limitado Privado - Dispensa de Outorga",
				NotificationFistel: "50418027773", NotificationProcess: new("53500018606201945"), NotifiedOn: day("10/05/2019"),
				EntityType: "Dispensada de Outorga", GrantType: "Dispensada de Outorga",
			},
		},
	},
}

type fakeStore struct {
	mu        sync.Mutex
	calls     int
	pingErr   error
	err       error  // devolvido pelas consultas de dados
	panicMsg  string // faz as consultas de dados entrarem em pânico
	noData    bool   // Dataset e Job devolvem nil (antes da primeira carga)
	last      string // último argumento recebido por uma consulta de dados
	extra     []store.ProviderBrief
	manyBrief int // ServiceProviders devolve esta quantidade de prestadoras sintéticas com nomes longos
}

func (f *fakeStore) count(arg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.last = arg
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

func (f *fakeStore) Last() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last
}

func (f *fakeStore) Ping(context.Context) error { return f.pingErr }

func (f *fakeStore) Dataset(context.Context) (*store.Dataset, error) {
	if f.noData {
		return nil, nil
	}
	return &store.Dataset{
		Version: "v1", AppliedAt: time.Unix(0, 0), URL: SourceURL, SHA256: "ab", CSVSHA256: new("cd"),
		CSVModifiedAt: new(time.Unix(5, 0)), Providers: new(len(sampleProviders)), Services: new(12),
	}, nil
}

func (f *fakeStore) Job(context.Context) (*store.Job, error) {
	if f.noData {
		return nil, nil
	}
	t := time.Unix(10, 0)
	return &store.Job{LastSyncAt: &t, LastCheckAt: &t, Consolidated: 0}, nil
}

func (f *fakeStore) Provider(_ context.Context, document string) (*store.ProviderDetail, error) {
	if err := f.count(document); err != nil {
		return nil, err
	}
	for _, p := range sampleProviders {
		if p.Provider.Document == document {
			d := p
			d.Provider.CreatedAt, d.Provider.UpdatedAt = time.Unix(100, 0), time.Unix(200, 0)
			d.Services = slices.Clone(p.Services)
			return &d, nil
		}
	}
	return nil, store.ErrNotFound
}

func (f *fakeStore) Services(context.Context) ([]store.ServiceSummary, error) {
	if err := f.count(""); err != nil {
		return nil, err
	}
	byCode := map[string]*store.ServiceSummary{}
	for _, p := range sampleProviders {
		seen := map[string]bool{}
		for _, s := range p.Services {
			x := byCode[s.ServiceCode]
			if x == nil {
				x = &store.ServiceSummary{ServiceCode: s.ServiceCode, ServiceName: s.ServiceName, ServiceGroup: s.ServiceGroup}
				byCode[s.ServiceCode] = x
			}
			x.Services++
			if !seen[s.ServiceCode] {
				seen[s.ServiceCode] = true
				x.Providers++
			}
		}
	}
	var out []store.ServiceSummary
	for _, x := range byCode {
		out = append(out, *x)
	}
	slices.SortFunc(out, func(a, b store.ServiceSummary) int { return cmp.Compare(a.ServiceCode, b.ServiceCode) })
	return out, nil
}

func brief(p store.Provider) store.ProviderBrief {
	return store.ProviderBrief{Document: p.Document, Name: p.Name, TradeName: p.TradeName, City: p.City, State: p.State}
}

func (f *fakeStore) ServiceProviders(_ context.Context, code, state string) (*store.ServiceInfo, []store.ProviderBrief, error) {
	if err := f.count(code + "|" + state); err != nil {
		return nil, nil, err
	}
	if f.manyBrief > 0 {
		out := make([]store.ProviderBrief, f.manyBrief)
		for i := range out {
			out[i] = store.ProviderBrief{Document: fmt.Sprintf("%014d", i), Name: strings.Repeat("N", 250)}
		}
		return &store.ServiceInfo{ServiceCode: code, ServiceName: "x", ServiceGroup: "y"}, out, nil
	}
	var info *store.ServiceInfo
	out := []store.ProviderBrief{}
	for _, p := range sampleProviders {
		for _, s := range p.Services {
			if s.ServiceCode != code {
				continue
			}
			if info == nil {
				info = &store.ServiceInfo{ServiceCode: s.ServiceCode, ServiceName: s.ServiceName, ServiceGroup: s.ServiceGroup}
			}
			if state == "" || (p.Provider.State != nil && *p.Provider.State == state) {
				out = append(out, brief(p.Provider))
			}
			break
		}
	}
	if info == nil {
		return nil, nil, store.ErrNotFound
	}
	slices.SortFunc(out, func(a, b store.ProviderBrief) int { return cmp.Compare(a.Document, b.Document) })
	return info, out, nil
}

func (f *fakeStore) Search(_ context.Context, term string, limit int) ([]store.ProviderBrief, error) {
	if err := f.count(fmt.Sprintf("%s|%d", term, limit)); err != nil {
		return nil, err
	}
	var out []store.ProviderBrief
	for _, p := range sampleProviders {
		if strings.Contains(strings.ToLower(p.Provider.Name), term) ||
			(p.Provider.TradeName != nil && strings.Contains(strings.ToLower(*p.Provider.TradeName), term)) {
			out = append(out, brief(p.Provider))
		}
	}
	for _, p := range f.extra {
		if strings.Contains(strings.ToLower(p.Name), term) {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b store.ProviderBrief) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.Document, b.Document))
	})
	if len(out) > limit {
		out = out[:limit]
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

const key = "badblock:api-anatel-pst:0192-v1:"

func TestProvider(t *testing.T) {
	st := &fakeStore{}
	h, c := newAPI(t, st, true)
	// Só dígitos, formatado (barra como %2F), lixo em volta e /v1: uma chave
	// só, uma consulta só.
	for _, p := range []string{
		"/provider/02558157000162", "/provider/02.558.157%2F0001-62", "/provider/02.558.157-0001.62",
		"/provider/cnpj02558157000162", "/v1/provider/02558157000162", "/v1/provider/02%20558%20157%200001%2062",
	} {
		rec := do(h, "GET", base+p)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", p, rec.Code, rec.Body)
		}
		got := decode[ProviderResponse](t, rec)
		if got.Document != "02558157000162" || got.Name != "TELEFONICA BRASIL S.A." || got.TradeName != nil ||
			len(got.Services) != 5 || got.Dataset.Version != "0192-v1" {
			t.Errorf("%s: %+v", p, got)
		}
	}
	if st.Calls() != 1 || st.Last() != "02558157000162" || !slices.Equal(c.keys(), []string{key + "provider:02558157000162"}) {
		t.Errorf("calls = %d, último %q, chaves = %v", st.Calls(), st.Last(), c.keys())
	}
	// Formato: os campos na ordem, o endereço agrupado, nulos como null,
	// datas AAAA-MM-DD, códigos distintos e ordenados, serviços na ordem do store.
	body := do(h, "GET", base+"/provider/02558157000162").Body.String()
	for _, want := range []string{
		`{"document":"02558157000162","name":"TELEFONICA BRASIL S.A.","trade_name":null,"address":{"street":"Avenida Engenheiro Luiz Carlos Berrini","number":"1376","complement":"Telefônica Brasil S/A","district":"Cidade Monções","postal_code":"04571936","city_ibge_code":3550308,"city":"São Paulo","state":"SP"},"phone":"(11) 3430-4532","email":"cadastro.fiscal.br@telefonica.com",`,
		`"first_seen":"1970-01-01T00:01:40Z","updated_at":"1970-01-01T00:03:20Z","service_codes":["010","045","171","750"],"services":[{"service_code":"010",`,
		`{"service_code":"045","service_name":"Serviço de Comunicação Multimídia","service_group":"Banda Larga Fixa","notification_fistel":"50013053736","notification_process":"535000020652002","notified_on":"2003-02-13","entity_type":"Outorgada","grant_type":"Serviços de Interesse Coletivo e Restrito - SIC","grant_fistel":"50423150120","grant_process":"53500036134202046","granted_on":"2021-01-05"}`,
		`"dataset":{"version":"0192-v1","updated_at":"1970-01-01T00:00:00Z"}}` + "\n",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("provider sem %s: %s", want, body)
		}
	}

	// Dispensada de outorga: grant_* null; nome fantasia; telefone e e-mail null.
	got := decode[ProviderResponse](t, do(h, "GET", base+"/provider/21557625000129"))
	last := got.Services[len(got.Services)-1]
	if last.EntityType != "Dispensada de Outorga" || last.GrantFistel != nil || last.GrantProcess != nil || last.GrantedOn != nil ||
		*last.NotifiedOn != "2019-05-10" || got.Phone != nil || got.Email != nil ||
		!slices.Equal(got.ServiceCodes, []string{"045", "110", "182", "190"}) {
		t.Errorf("transat = %+v", got)
	}
	body = do(h, "GET", base+"/provider/21557625000129").Body.String()
	if !strings.Contains(body, `"entity_type":"Dispensada de Outorga","grant_type":"Dispensada de Outorga","grant_fistel":null,"grant_process":null,"granted_on":null}`) {
		t.Errorf("dispensada = %s", body)
	}
	// Mesmo código em duas outorgas: dois serviços, um código.
	got = decode[ProviderResponse](t, do(h, "GET", base+"/provider/03237583006602"))
	if *got.TradeName != "Copa Energia" || len(got.Services) != 3 || !slices.Equal(got.ServiceCodes, []string{"019", "028"}) ||
		got.Services[0].NotificationFistel != got.Services[1].NotificationFistel || *got.Services[0].GrantFistel == *got.Services[1].GrantFistel {
		t.Errorf("copa = %+v", got)
	}
}

func TestServices(t *testing.T) {
	st := &fakeStore{}
	h, c := newAPI(t, st, true)
	for _, p := range []string{base + "/services", base + "/v1/services"} {
		rec := do(h, "GET", p)
		got := decode[ServicesResponse](t, rec)
		if rec.Code != 200 || got.Count != 9 || len(got.Services) != 9 || got.Services[0].ServiceCode != "010" {
			t.Errorf("%s: %d %+v", p, rec.Code, got)
		}
	}
	if st.Calls() != 1 || !slices.Equal(c.keys(), []string{key + "services"}) {
		t.Errorf("calls = %d, chaves = %v", st.Calls(), c.keys())
	}
	body := do(h, "GET", base+"/services").Body.String()
	for _, want := range []string{
		`{"count":9,"services":[{"service_code":"010","service_name":"SERVIÇO MOVEL PESSOAL","service_group":"Telefonia Móvel","providers":1,"services":1},`,
		`{"service_code":"019","service_name":"Limitado Privado","service_group":"Limitado Privado","providers":1,"services":2}`,
		`{"service_code":"045","service_name":"Serviço de Comunicação Multimídia","service_group":"Banda Larga Fixa","providers":2,"services":2}`,
		`{"service_code":"171","service_name":"SERVICO TELEFONICO FIXO COMUTADO","service_group":"Telefonia Fixa","providers":1,"services":2}`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("services sem %s: %s", want, body)
		}
	}
}

func TestService(t *testing.T) {
	st := &fakeStore{}
	h, c := newAPI(t, st, true)
	// 45, 045 e /v1: uma chave só; ?state= vazio = sem filtro.
	for _, p := range []string{"/service/045", "/service/45", "/v1/service/045", "/service/045?state=", "/service/045?x=1"} {
		rec := do(h, "GET", base+p)
		got := decode[ServiceResponse](t, rec)
		if rec.Code != 200 || got.ServiceCode != "045" || got.ServiceName != "Serviço de Comunicação Multimídia" ||
			got.ServiceGroup != "Banda Larga Fixa" || got.State != nil || got.Count != 2 || len(got.Providers) != 2 ||
			got.Providers[0].Document != "02558157000162" || got.Providers[1].Document != "21557625000129" {
			t.Errorf("%s: %d %s", p, rec.Code, rec.Body)
		}
	}
	if st.Calls() != 1 || st.Last() != "045|" || !slices.Equal(c.keys(), []string{key + "service:045"}) {
		t.Errorf("calls = %d, último %q, chaves = %v", st.Calls(), st.Last(), c.keys())
	}
	body := do(h, "GET", base+"/service/45").Body.String()
	if !strings.HasPrefix(body, `{"service_code":"045","service_name":"Serviço de Comunicação Multimídia","service_group":"Banda Larga Fixa","state":null,"count":2,"providers":[{"document":"02558157000162","name":"TELEFONICA BRASIL S.A.","trade_name":null,"city":"São Paulo","state":"SP"},`) {
		t.Errorf("service = %s", body)
	}
	// Filtro de UF em qualquer caixa: uma chave por UF.
	for _, p := range []string{"/service/045?state=mg", "/service/45?state=MG", "/v1/service/045?state=Mg"} {
		rec := do(h, "GET", base+p)
		got := decode[ServiceResponse](t, rec)
		if rec.Code != 200 || got.State == nil || *got.State != "MG" || got.Count != 1 || got.Providers[0].Document != "21557625000129" {
			t.Errorf("%s: %d %s", p, rec.Code, rec.Body)
		}
	}
	if st.Calls() != 2 || st.Last() != "045|MG" || !slices.Contains(c.keys(), key+"service:045:MG") {
		t.Errorf("calls = %d, último %q, chaves = %v", st.Calls(), st.Last(), c.keys())
	}
	// UF sem prestadora: 200 com lista vazia (e no cache).
	rec := do(h, "GET", base+"/service/045?state=ZZ")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"state":"ZZ","count":0,"providers":[],`) || !slices.Contains(c.keys(), key+"service:045:ZZ") {
		t.Errorf("UF vazia = %d %s", rec.Code, rec.Body)
	}
	// Código de um dígito.
	if rec := do(h, "GET", base+"/service/0"); rec.Code != 404 || st.Last() != "000|" {
		t.Errorf("/service/0 = %d, último %q", rec.Code, st.Last())
	}
}

func TestSearch(t *testing.T) {
	st := &fakeStore{}
	h, c := newAPI(t, st, true)
	// Espaços, caixa e limit padrão: a mesma chave.
	for _, p := range []string{"?q=telefonica", "?q=%20Telefonica%20", "?q=TELEFONICA&limit=", "?q=telefonica&limit=100", "?q=telefonica&q=outra", "?q=telefonica&limit=0100"} {
		rec := do(h, "GET", base+"/search"+p)
		got := decode[SearchResponse](t, rec)
		if rec.Code != 200 || got.Query != "telefonica" || got.Limit != 100 || got.Count != 1 || got.Truncated ||
			got.Providers[0].Document != "02558157000162" {
			t.Errorf("%s: %d %s", p, rec.Code, rec.Body)
		}
	}
	if st.Calls() != 1 || st.Last() != "telefonica|101" || !slices.Equal(c.keys(), []string{key + "search:100:telefonica"}) {
		t.Errorf("calls = %d, último %q, chaves = %v", st.Calls(), st.Last(), c.keys())
	}
	body := do(h, "GET", base+"/v1/search?q=telefonica").Body.String()
	if !strings.HasPrefix(body, `{"query":"telefonica","limit":100,"count":1,"truncated":false,"providers":[{"document":"02558157000162","name":"TELEFONICA BRASIL S.A.","trade_name":null,"city":"São Paulo","state":"SP"}],"dataset":`) {
		t.Errorf("search = %s", body)
	}
	// Nome fantasia também conta; espaços internos colapsados.
	got := decode[SearchResponse](t, do(h, "GET", base+"/search?q=copa%20%20%09energia"))
	if got.Query != "copa energia" || got.Count != 1 || *got.Providers[0].TradeName != "Copa Energia" {
		t.Errorf("copa = %+v", got)
	}
	// Curingas chegam literais ao store (o escape é do store).
	for q, want := range map[string]string{"100%25": "100%", "a_b": "a_b", `x%5Cy`: `x\y`} {
		rec := do(h, "GET", base+"/search?q="+q)
		if rec.Code != 200 || st.Last() != want+"|101" {
			t.Errorf("q=%s: %d, store recebeu %q", q, rec.Code, st.Last())
		}
	}
	// Sem resultado: 200 com lista vazia.
	rec := do(h, "GET", base+"/search?q=zzqqxx")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"count":0,"truncated":false,"providers":[],`) {
		t.Errorf("sem resultado = %d %s", rec.Code, rec.Body)
	}
	// Corte: o store recebe limit+1; vindo mais, corta e marca truncated.
	st.extra = nil
	for i := range 150 {
		st.extra = append(st.extra, store.ProviderBrief{Document: fmt.Sprintf("%014d", i), Name: fmt.Sprintf("BULK %03d", i)})
	}
	got = decode[SearchResponse](t, do(h, "GET", base+"/search?q=bulk"))
	if got.Count != 100 || !got.Truncated || len(got.Providers) != 100 || got.Providers[99].Name != "BULK 099" {
		t.Errorf("bulk = count %d, truncated %v", got.Count, got.Truncated)
	}
	got = decode[SearchResponse](t, do(h, "GET", base+"/search?q=bulk&limit=5"))
	if got.Count != 5 || !got.Truncated || got.Limit != 5 || st.Last() != "bulk|6" {
		t.Errorf("bulk limit 5 = %+v, último %q", got, st.Last())
	}
	got = decode[SearchResponse](t, do(h, "GET", base+"/search?q=bulk%20149&limit=10"))
	if got.Count != 1 || got.Truncated {
		t.Errorf("bulk 149 = %+v", got)
	}
	if !slices.Contains(c.keys(), key+"search:5:bulk") {
		t.Errorf("chaves = %v", c.keys())
	}
}

func TestNormalizers(t *testing.T) {
	for in, want := range map[string]string{"045": "045", "45": "045", "5": "005", "0": "000", "171": "171"} {
		if got, ok := normalizeServiceCode(in); !ok || got != want {
			t.Errorf("normalizeServiceCode(%q) = %q, %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "0045", "4a", "+45", "-1", " 45", "٤٥"} {
		if _, ok := normalizeServiceCode(in); ok {
			t.Errorf("normalizeServiceCode(%q) deveria falhar", in)
		}
	}
	for in, want := range map[string]string{"": "", "sp": "SP", "Mg": "MG", "ZZ": "ZZ"} {
		if got, ok := normalizeState(in); !ok || got != want {
			t.Errorf("normalizeState(%q) = %q, %v", in, got, ok)
		}
	}
	for _, in := range []string{"S", "SPA", "S1", "ç1", "çé", " S"} {
		if _, ok := normalizeState(in); ok {
			t.Errorf("normalizeState(%q) deveria falhar", in)
		}
	}
	for in, want := range map[string]int{"": 100, "1": 1, "100": 100, "007": 7} {
		if got, ok := parseLimit(in); !ok || got != want {
			t.Errorf("parseLimit(%q) = %d, %v", in, got, ok)
		}
	}
	for _, in := range []string{"0", "101", "-1", "+5", "1.5", "abc", " 5", "99999999999999999999"} {
		if _, ok := parseLimit(in); ok {
			t.Errorf("parseLimit(%q) deveria falhar", in)
		}
	}
	for in, want := range map[string]string{" Telefônica  Brasil ": "telefônica brasil", "abc": "abc", "a\tb\nc": "a b c", strings.Repeat("é", 100): strings.Repeat("é", 100)} {
		if got, ok := normalizeQuery(in); !ok || got != want {
			t.Errorf("normalizeQuery(%q) = %q, %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "   ", "ab", " a ", strings.Repeat("a", 101), "ab\x00c", "ab\u0093c", "ab\xffc"} {
		if _, ok := normalizeQuery(in); ok {
			t.Errorf("normalizeQuery(%q) deveria falhar", in)
		}
	}
	if onlyDigits("02.558.157/0001-62") != "02558157000162" || onlyDigits("١٢٣") != "" {
		t.Error("onlyDigits")
	}
}

func TestErrors(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, true)
	cases := map[string]int{
		base + "/provider/00000000000000":              404,
		base + "/provider/0255815700016":               400, // 13 dígitos
		base + "/provider/025581570001620":             400, // 15 dígitos
		base + "/provider/08030063":                    400, // 8 dígitos: não é CNPJ
		base + "/provider/abc":                         400,
		base + "/provider/02.558.157/0001-62":          404, // a barra sem %2F vira outro caminho
		base + "/service/999":                          404,
		base + "/service/0045":                         400,
		base + "/service/4a":                           400,
		base + "/service/045?state=SPA":                400,
		base + "/service/045?state=1":                  400,
		base + "/service/999?state=SPA":                400,
		base + "/search":                               400,
		base + "/search?q=ab":                          400,
		base + "/search?q=%20%20":                      400,
		base + "/search?q=" + strings.Repeat("a", 101): 400,
		base + "/search?q=ab%00c":                      400,
		base + "/search?q=ab%FFc":                      400,
		base + "/search?q=abc&limit=0":                 400,
		base + "/search?q=abc&limit=101":               400,
		base + "/search?q=abc&limit=-1":                400,
		base + "/search?q=abc&limit=x":                 400,
		base + "/services/045":                         404,
		base + "/provider/02558157000162/services":     404,
		base + "/v2/services":                          404,
		base + "/nada":                                 404,
		"/anatel/outra":                                404,
		"/services":                                    404,
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
	for p, msg := range map[string]string{
		"/provider/00.000.000%2F0000-00": `"code":"not_found","message":"CNPJ 00000000000000 não consta no cadastro de prestadoras da Anatel"`,
		"/provider/abc":                  `"code":"bad_request","message":"CNPJ inválido: use os 14 dígitos, com ou sem pontuação (ex.: 02558157000162 ou 02.558.157%2F0001-62)"`,
		"/service/99":                    `"code":"not_found","message":"serviço 099 não consta no cadastro de prestadoras da Anatel"`,
		"/service/abc":                   `"code":"bad_request","message":"código de serviço inválido: use de 1 a 3 dígitos (ex.: 045 ou 45 para o SCM)"`,
		"/service/045?state=x":           `"code":"bad_request","message":"UF inválida: use a sigla de duas letras (ex.: ?state=SP)"`,
		"/search?q=a":                    `"code":"bad_request","message":"use o parâmetro q com um trecho de 3 a 100 caracteres, sem caracteres de controle (ex.: ?q=telefonica)"`,
		"/search?q=abc&limit=500":        `"code":"bad_request","message":"limit inválido: use um número de 1 a 100 (padrão 100)"`,
		"/nada":                          `"code":"not_found","message":"rota inexistente; veja /anatel/pst/"`,
	} {
		if body := do(h, "GET", base+p).Body.String(); !strings.Contains(body, msg) {
			t.Errorf("%s = %s", p, body)
		}
	}
	// Método não suportado numa rota existente.
	for _, m := range []string{"DELETE", "POST", "PUT"} {
		if rec := do(h, m, base+"/provider/02558157000162"); rec.Code != 404 {
			t.Errorf("%s = %d", m, rec.Code)
		}
	}
	if rec := do(h, "POST", base+"/meta"); rec.Code != 404 {
		t.Errorf("POST /meta = %d", rec.Code)
	}
	// Barra no fim é outra rota.
	if rec := do(h, "GET", base+"/services/"); rec.Code != 404 {
		t.Errorf("/services/ = %d", rec.Code)
	}
}

func TestDatabaseErrors(t *testing.T) {
	st := &fakeStore{err: errors.New("conexão recusada")}
	h, c := newAPI(t, st, true)
	paths := []string{base + "/provider/02558157000162", base + "/services", base + "/service/045", base + "/search?q=telefonica"}
	for _, p := range paths {
		rec := do(h, "GET", p)
		if rec.Code != 503 || !strings.Contains(rec.Body.String(), `"database_unavailable","message":"banco de dados indisponível"`) {
			t.Errorf("%s: %d %s", p, rec.Code, rec.Body)
		}
	}
	st.err = fmt.Errorf("consulta: %w", context.DeadlineExceeded)
	for _, p := range paths {
		rec := do(h, "GET", p)
		if rec.Code != 504 || !strings.Contains(rec.Body.String(), `"timeout","message":"a consulta demorou demais"`) {
			t.Errorf("%s: %d %s", p, rec.Code, rec.Body)
		}
	}
	// Erros não entram no cache: quando o banco volta, a resposta é calculada.
	st.err = nil
	if len(c.keys()) != 0 {
		t.Errorf("erro foi para o cache: %v", c.keys())
	}
	if rec := do(h, "GET", base+"/services"); rec.Code != 200 || rec.Header().Get("X-Cache") != "MISS" {
		t.Errorf("depois do erro = %d %s", rec.Code, rec.Header().Get("X-Cache"))
	}
	// 404 também não entra no cache.
	do(h, "GET", base+"/provider/00000000000000")
	do(h, "GET", base+"/service/999")
	if n := len(c.keys()); n != 1 {
		t.Errorf("chaves = %v", c.keys())
	}
}

func TestPanicIsJSON500(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{panicMsg: "bug"}, true)
	for _, p := range []string{"/provider/02558157000162", "/services", "/service/045", "/search?q=abc"} {
		rec := do(h, "GET", base+p)
		if rec.Code != 500 || !strings.Contains(rec.Body.String(), `"internal_error","message":"erro interno"`) {
			t.Errorf("panic %s = %d %s", p, rec.Code, rec.Body)
		}
	}
}

func TestCacheAndETag(t *testing.T) {
	st := &fakeStore{}
	h, c := newAPI(t, st, true)

	first := do(h, "GET", base+"/provider/02558157000162")
	if first.Header().Get("X-Cache") != "MISS" {
		t.Errorf("primeira = %s", first.Header().Get("X-Cache"))
	}
	// Versionada e sem versão compartilham o cache (mesmo conteúdo).
	second := do(h, "GET", base+"/v1/provider/02.558.157%2F0001-62")
	if second.Header().Get("X-Cache") != "HIT" || st.Calls() != 1 || second.Body.String() != first.Body.String() {
		t.Errorf("segunda = %s, calls = %d", second.Header().Get("X-Cache"), st.Calls())
	}
	if !slices.Equal(c.keys(), []string{key + "provider:02558157000162"}) {
		t.Errorf("chaves = %v", c.keys())
	}
	etag := first.Header().Get("ETag")
	if etag != etagFor("0192-v1", "provider:02558157000162") || first.Header().Get("X-Dataset-Version") != "0192-v1" ||
		first.Header().Get("Cache-Control") != "public, max-age=300" || second.Header().Get("ETag") != etag {
		t.Fatalf("cabeçalhos = %v", first.Header())
	}
	for _, inm := range []string{etag, strings.TrimPrefix(etag, "W/"), `"outro", ` + etag, "*"} {
		rec := do(h, "GET", base+"/v1/provider/02558157000162", "If-None-Match", inm)
		if rec.Code != http.StatusNotModified || rec.Body.Len() != 0 || rec.Header().Get("ETag") != etag ||
			rec.Header().Get("X-Cache") != "HIT" || rec.Header().Get("Content-Type") != "" {
			t.Errorf("If-None-Match %s = %d %v", inm, rec.Code, rec.Header())
		}
	}
	if rec := do(h, "GET", base+"/provider/02558157000162", "If-None-Match", `W/"outro"`); rec.Code != 200 {
		t.Errorf("ETag diferente = %d", rec.Code)
	}
	if st.Calls() != 1 {
		t.Errorf("304 não deveria consultar o banco (calls = %d)", st.Calls())
	}
	// Cada consulta tem o próprio ETag; com e sem /v1, o mesmo.
	if do(h, "GET", base+"/provider/21557625000129").Header().Get("ETag") == etag {
		t.Error("ETag deveria depender da consulta")
	}
	for p, k := range map[string]string{
		"/services":                 "services",
		"/service/45":               "service:045",
		"/service/045?state=sp":     "service:045:SP",
		"/search?q=Telefonica":      "search:100:telefonica",
		"/search?q=copa&limit=10":   "search:10:copa",
		"/provider/21557625000129":  "provider:21557625000129",
		"/provider/21.557.625-0001": "",
	} {
		a := do(h, "GET", base+p).Header().Get("ETag")
		b := do(h, "GET", base+"/v1"+p).Header().Get("ETag")
		if k == "" {
			if a != "" || b != "" {
				t.Errorf("%s inválido não deveria ter ETag", p)
			}
			continue
		}
		if a == "" || a != b || a != etagFor("0192-v1", k) {
			t.Errorf("ETag de %s: %q, %q, quero o de %s", p, a, b, k)
		}
	}
	// Com a versão dos exemplos da spec, os ETags documentados.
	for k, want := range map[string]string{
		"provider:02558157000162": `W/"7b5f3c4da8ce91e8"`,
		"services":                `W/"8e6e540238112467"`,
		"service:045":             `W/"db1be8c5a42ed6df"`,
		"service:045:RR":          `W/"b4897f8aa1a806ff"`,
		"search:100:telefonica":   `W/"c9fcc4ac2ef1ece"`,
		"search:3:telecom":        `W/"5f469ae36664060f"`,
	} {
		if got := etagFor("01a0f518-b4d2-7810-80b5-b58a6c2e4614", k); got != want {
			t.Errorf("etagFor(%s) = %s, quero %s", k, got, want)
		}
	}
}

func TestHEAD(t *testing.T) {
	st := &fakeStore{}
	h, _ := newAPI(t, st, true)
	for _, p := range []string{"/provider/02558157000162", "/services", "/service/045", "/search?q=telefonica", "/", "/meta"} {
		rec := do(h, "HEAD", base+p)
		if rec.Code != 200 {
			t.Errorf("HEAD %s = %d", p, rec.Code)
		}
	}
	rec := do(h, "HEAD", base+"/services")
	etag := rec.Header().Get("ETag")
	if etag == "" || rec.Header().Get("X-Cache") != "HIT" {
		t.Errorf("HEAD sem cabeçalhos de dados: %v", rec.Header())
	}
	if rec := do(h, "HEAD", base+"/services", "If-None-Match", etag); rec.Code != http.StatusNotModified {
		t.Errorf("HEAD com If-None-Match = %d", rec.Code)
	}
	if rec := do(h, "HEAD", base+"/provider/00000000000000"); rec.Code != 404 {
		t.Errorf("HEAD inexistente = %d", rec.Code)
	}
}

func TestBypassAndBigBody(t *testing.T) {
	h := newAPIWith(t, &fakeStore{}, true, cache.Noop{})
	if rec := do(h, "GET", base+"/services"); rec.Header().Get("X-Cache") != "BYPASS" {
		t.Errorf("sem cache = %s", rec.Header().Get("X-Cache"))
	}
	// Respostas acima de 8 MiB não vão para o cache.
	h, c := newAPI(t, &fakeStore{manyBrief: 40000}, true)
	rec := do(h, "GET", base+"/service/045")
	if rec.Code != 200 || rec.Body.Len() <= maxCachedBody || len(c.keys()) != 0 || rec.Header().Get("X-Cache") != "MISS" {
		t.Errorf("corpo grande: %d, %d bytes, chaves %v", rec.Code, rec.Body.Len(), c.keys())
	}
}

func TestNotReady(t *testing.T) {
	st := &fakeStore{noData: true}
	h, _ := newAPI(t, st, false)
	for _, p := range []string{"/provider/02558157000162", "/services", "/v1/service/45", "/search?q=telefonica"} {
		rec := do(h, "GET", base+p)
		if rec.Code != 503 || !strings.Contains(rec.Body.String(), `"dataset_not_ready","message":"a primeira sincronização do collector-anatel-pst ainda não terminou; tente em alguns minutos"`) {
			t.Errorf("%s sem dataset = %d %s", p, rec.Code, rec.Body)
		}
	}
	if st.Calls() != 0 {
		t.Errorf("sem dataset não deveria consultar o banco (calls = %d)", st.Calls())
	}
	// Validação vem antes da conferência dos dados.
	for _, p := range []string{"/provider/123", "/service/abcd", "/service/045?state=1", "/search?q=a", "/search?q=abc&limit=0"} {
		if rec := do(h, "GET", base+p); rec.Code != 400 {
			t.Errorf("%s inválido sem dataset = %d", p, rec.Code)
		}
	}
	rec := do(h, "GET", base+"/status")
	s := decode[StatusResponse](t, rec)
	if rec.Code != 200 || s.Status != "starting" || !s.Success || s.Checks["dataset"] != "empty" ||
		s.Message != "aguardando a primeira sincronização do collector-anatel-pst" {
		t.Errorf("status sem dataset = %d %+v", rec.Code, s)
	}
	rec = do(h, "GET", base+"/meta")
	if rec.Code != 200 || rec.Body.String() != `{"app":"api-anatel-pst","version":"test","dataset":null,"collector":null}`+"\n" {
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
			if rec.Code != 200 || !s.Success || s.Status != "ok" || s.Timestamp == "" || s.Message != "api-anatel-pst operacional" ||
				s.Checks["postgres"] != "ok" || s.Checks["valkey"] != "ok" || s.Checks["dataset"] != "ok" {
				t.Errorf("%s %s = %d %s", m, p, rec.Code, rec.Body)
			}
			if rec.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("%s %s sem no-store", m, p)
			}
		}
	}
	if rec := do(h, "GET", base+"/ping"); rec.Code != 200 || rec.Body.String() != "pong" || rec.Header().Get("Cache-Control") != "no-store" ||
		rec.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
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
	rec := do(h, "OPTIONS", base+"/provider/02558157000162", "Origin", "https://exemplo.com", "Access-Control-Request-Method", "GET")
	if rec.Code != http.StatusNoContent || rec.Header().Get("Access-Control-Allow-Origin") != "*" ||
		rec.Header().Get("Access-Control-Allow-Methods") != "GET, HEAD, OPTIONS" ||
		rec.Header().Get("Access-Control-Allow-Headers") != "If-None-Match, Content-Type" ||
		rec.Header().Get("Access-Control-Max-Age") != "86400" {
		t.Errorf("preflight = %d %v", rec.Code, rec.Header())
	}
	if rec := do(h, "OPTIONS", "/qualquer/coisa"); rec.Code != http.StatusNoContent {
		t.Errorf("preflight fora do caminho de base = %d", rec.Code)
	}
	rec = do(h, "GET", base+"/services")
	hd := rec.Header()
	if hd.Get("Access-Control-Allow-Origin") != "*" || hd.Get("X-Content-Type-Options") != "nosniff" ||
		hd.Get("Referrer-Policy") != "no-referrer" || hd.Get("Server") != "badblock-api-anatel-pst/test" ||
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
	want := []string{
		base + "/provider/{cnpj}", base + "/services", base + "/service/{code}", base + "/search?q={texto}",
		base + "/meta", base + "/status", base + "/openapi.yaml",
	}
	for _, p := range []string{base + "/", base + "/v1/"} {
		rec := do(h, "GET", p)
		idx := decode[IndexResponse](t, rec)
		if rec.Code != 200 || idx.App != "api-anatel-pst" || idx.BasePath != base || idx.Source != SourceURL ||
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
	// /anatel/pst/v1 sem barra: o redirect automático do ServeMux.
	if rec := do(h, "GET", base+"/v1"); rec.Code != http.StatusTemporaryRedirect && rec.Code != http.StatusMovedPermanently {
		t.Errorf("/v1 = %d", rec.Code)
	}
	for _, p := range []string{base + "/meta", base + "/v1/meta"} {
		rec := do(h, "GET", p)
		if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("X-Cache") != "" {
			t.Errorf("meta = %d %v", rec.Code, rec.Header())
		}
		body := rec.Body.String()
		for _, part := range []string{
			`{"app":"api-anatel-pst","version":"test","dataset":{"version":"v1","updated_at":"1970-01-01T00:00:00Z","source":"` + SourceURL + `","sha256":"ab","csv_sha256":"cd","csv_modified_at":"1970-01-01T00:00:05Z","providers":3,"services":12},`,
			`"collector":{"app":"collector-anatel-pst","last_sync_at":"1970-01-01T00:00:10Z","last_check_at":"1970-01-01T00:00:10Z","consolidated":false}}`,
		} {
			if !strings.Contains(body, part) {
				t.Errorf("meta sem %s: %s", part, body)
			}
		}
	}
}
