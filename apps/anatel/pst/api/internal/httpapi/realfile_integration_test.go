//go:build integration

package httpapi

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/patrickbrandao/badblock/apps/anatel/pst/api/internal/cache"
	"github.com/patrickbrandao/badblock/apps/anatel/pst/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/anatel/pst/api/internal/realip"
	"github.com/patrickbrandao/badblock/apps/anatel/pst/api/internal/store"
	"github.com/patrickbrandao/badblock/apps/anatel/pst/api/internal/testdb"
)

// fixtureFile é o recorte real do ZIP de 2026-09-30, na fixture do coletor.
const fixtureFile = "../../../collector/testdata/pst-sample.zip"

// Limites com o arquivo real inteiro (handler e store reais, sem cache);
// medidos bem abaixo disso (api.md, "Medições").
const (
	realMaxProvider = 100 * time.Millisecond
	realMaxServices = 500 * time.Millisecond
	realMaxService  = 1500 * time.Millisecond
	realMaxSearch   = 25 * time.Millisecond
	realMaxBody     = 4 << 20 // metade do limite do cache
)

// TestRealFile carrega o ZIP da Anatel com o binário do collector-anatel-pst
// (compilado aqui, com o arquivo servido por um httptest.Server) num PG18
// descartável e confere /meta, /services, /service de todos os códigos (e de
// todas as UFs no SCM), /provider de todos os CNPJs e /search contra as
// tabelas, com tempos e tamanhos no log. Usa ANATEL_PST_REAL_FILE se definida
// (make test-real); senão, a fixture do coletor, com MIN_PROVIDERS=0 — por
// isso roda em todo make test-int, sem rede.
func TestRealFile(t *testing.T) {
	path := os.Getenv("ANATEL_PST_REAL_FILE")
	full := path != ""
	if !full {
		path = fixtureFile
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sha := sha256.Sum256(raw)
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	pgURL, admin := testdb.New(t)

	// Compila o collector irmão e carrega o arquivo com --once.
	bin := filepath.Join(t.TempDir(), store.CollectorApp)
	build := exec.Command("go", "build", "-o", bin, "./cmd/"+store.CollectorApp)
	build.Dir = filepath.Join("..", "..", "..", "collector")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build do collector: %v\n%s", err, out)
	}
	files := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-zip-compressed")
		_, _ = w.Write(raw)
	}))
	defer files.Close()
	run := exec.Command(bin, "--once")
	run.Env = append(os.Environ(), "POSTGRES_URL="+pgURL, "SOURCE_URL="+files.URL+"/prestadoras_servicos_telecomunicacoes.zip", "LOG_FORMAT=text")
	if !full {
		run.Env = append(run.Env, "MIN_PROVIDERS=0") // o recorte tem poucas prestadoras
	}
	start := time.Now()
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("collector --once: %v\n%s", err, out)
	}
	t.Logf("carga pelo collector: %v (%d bytes)", time.Since(start).Round(time.Millisecond), len(raw))
	if _, err := admin.Exec(ctx, "ANALYZE"); err != nil {
		t.Fatal(err)
	}

	st, err := store.Open(ctx, pgURL, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	w := dataset.NewWatcher(st, time.Minute, log)
	if err := w.Refresh(ctx); err != nil || !w.Current().Ready() {
		t.Fatalf("dataset: %+v, %v", w.Current(), err)
	}
	rip, _ := realip.New(realip.DefaultTrustedProxies, realip.DefaultHeaders)
	h := New(st, w, cache.Noop{}, rip, log, Config{BasePath: base, Version: "real", DBTimeout: 30 * time.Second}).Handler()

	type timing struct {
		path  string
		d     time.Duration
		bytes int
	}
	get := func(path string, verbose bool) (*httptest.ResponseRecorder, timing) {
		t.Helper()
		start := time.Now()
		rec := do(h, "GET", base+path)
		tm := timing{path, time.Since(start), rec.Body.Len()}
		if rec.Code != 200 {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Body)
		}
		if verbose {
			t.Logf("%-46s %3d %9d bytes %v", path, rec.Code, tm.bytes, tm.d.Round(time.Microsecond))
		}
		return rec, tm
	}

	// /meta bate com a execução gravada pelo collector e com o arquivo.
	rec, _ := get("/meta", true)
	meta := decode[MetaResponse](t, rec)
	var providers, services, provRows, servRows int
	if err := admin.QueryRow(ctx, `SELECT providers, services, (SELECT count(*) FROM anatel_pst_provider), (SELECT count(*) FROM anatel_pst_service)
		FROM anatel_pst_run WHERE status = 1`).Scan(&providers, &services, &provRows, &servRows); err != nil {
		t.Fatal(err)
	}
	if meta.Dataset == nil || *meta.Dataset.Providers != providers || *meta.Dataset.Services != services ||
		provRows != providers || servRows != services || meta.Dataset.SHA256 != hex.EncodeToString(sha[:]) ||
		meta.Dataset.CSVSHA256 == nil || meta.Dataset.CSVModifiedAt == nil ||
		meta.Collector == nil || meta.Collector.LastSyncAt == nil {
		t.Errorf("meta = %+v %+v", meta.Dataset, meta.Collector)
	}
	t.Logf("arquivo: %d prestadoras, %d serviços; versão %s", providers, services, meta.Dataset.Version)

	// /services: os códigos da tabela, em ordem, com as contagens.
	rec, tm := get("/services", true)
	if full && tm.d > realMaxServices {
		t.Errorf("/services levou %v (limite %v)", tm.d, realMaxServices)
	}
	cat := decode[ServicesResponse](t, rec)
	var codes int
	if err := admin.QueryRow(ctx, `SELECT count(DISTINCT service_code) FROM anatel_pst_service`).Scan(&codes); err != nil {
		t.Fatal(err)
	}
	sum := 0
	for i, s := range cat.Services {
		sum += s.Services
		if i > 0 && cat.Services[i-1].ServiceCode > s.ServiceCode {
			t.Errorf("/services fora de ordem em %s", s.ServiceCode)
		}
	}
	if cat.Count != codes || len(cat.Services) != codes || sum != services {
		t.Errorf("/services: %d itens, %d códigos na tabela, soma %d de %d serviços", cat.Count, codes, sum, services)
	}

	// /service de todos os códigos: count = providers do catálogo; o maior
	// abaixo de realMaxBody; no maior, a soma das UFs (e das sem UF) bate.
	var slowest, biggest timing
	var sizes []timing
	for _, s := range cat.Services {
		rec, tm := get("/service/"+s.ServiceCode, false)
		sizes = append(sizes, tm)
		got := decode[ServiceResponse](t, rec)
		if got.Count != s.Providers || len(got.Providers) != s.Providers || got.ServiceName != s.ServiceName ||
			!slices.IsSortedFunc(got.Providers, func(a, b ProviderBrief) int { return cmp.Compare(a.Document, b.Document) }) {
			t.Errorf("/service/%s: count %d, catálogo %d", s.ServiceCode, got.Count, s.Providers)
		}
		if tm.d > slowest.d {
			slowest = tm
		}
		if tm.bytes > biggest.bytes {
			biggest = tm
		}
	}
	t.Logf("/service de %d códigos: mais lento %s em %v; maior %s com %d bytes",
		len(cat.Services), slowest.path, slowest.d.Round(time.Microsecond), biggest.path, biggest.bytes)
	slices.SortFunc(sizes, func(a, b timing) int { return cmp.Compare(b.bytes, a.bytes) })
	for _, x := range sizes[:min(3, len(sizes))] {
		t.Logf("  %-14s %9d bytes %v", x.path, x.bytes, x.d.Round(time.Microsecond))
	}
	if biggest.bytes > realMaxBody {
		t.Errorf("%s tem %d bytes (limite do teste %d)", biggest.path, biggest.bytes, realMaxBody)
	}
	if full && slowest.d > realMaxService {
		t.Errorf("%s levou %v (limite %v)", slowest.path, slowest.d, realMaxService)
	}
	top := slices.MaxFunc(cat.Services, func(a, b ServiceSummary) int { return cmp.Compare(a.Providers, b.Providers) })
	get("/service/"+top.ServiceCode, true)
	var states []string
	if err := admin.QueryRow(ctx, `SELECT array_agg(DISTINCT state ORDER BY state) FROM anatel_pst_provider WHERE state IS NOT NULL`).Scan(&states); err != nil {
		t.Fatal(err)
	}
	inState := 0
	for _, uf := range states {
		rec, _ := get("/service/"+top.ServiceCode+"?state="+strings.ToLower(uf), uf == "SP" || uf == "RR")
		got := decode[ServiceResponse](t, rec)
		inState += got.Count
		if got.State == nil || *got.State != uf || slices.ContainsFunc(got.Providers, func(p ProviderBrief) bool { return p.State == nil || *p.State != uf }) {
			t.Errorf("/service/%s?state=%s: %+v", top.ServiceCode, uf, got.State)
		}
	}
	var noState int
	if err := admin.QueryRow(ctx, `SELECT count(*) FROM anatel_pst_provider p WHERE p.state IS NULL AND EXISTS
		(SELECT 1 FROM anatel_pst_service s WHERE s.provider_uuid = p.uuid AND s.service_code = $1)`, top.ServiceCode).Scan(&noState); err != nil {
		t.Fatal(err)
	}
	if inState+noState != top.Providers {
		t.Errorf("/service/%s por UF: %d + %d sem UF, catálogo %d", top.ServiceCode, inState, noState, top.Providers)
	}

	// /provider de todos os CNPJs: a soma dos serviços bate com a tabela.
	var cnpjs []string
	if err := admin.QueryRow(ctx, `SELECT array_agg(document ORDER BY document) FROM anatel_pst_provider`).Scan(&cnpjs); err != nil {
		t.Fatal(err)
	}
	start = time.Now()
	total := 0
	slowest, biggest = timing{}, timing{}
	for _, c := range cnpjs {
		rec, tm := get("/provider/"+c, false)
		got := decode[ProviderResponse](t, rec)
		total += len(got.Services)
		if got.Document != c || len(got.Services) == 0 || len(got.ServiceCodes) == 0 || !slices.IsSorted(got.ServiceCodes) {
			t.Errorf("/provider/%s: %+v", c, got)
		}
		if tm.d > slowest.d {
			slowest = tm
		}
		if tm.bytes > biggest.bytes {
			biggest = tm
		}
	}
	t.Logf("/provider de %d CNPJs: %v no total; mais lento %s em %v; maior %s com %d bytes",
		len(cnpjs), time.Since(start).Round(time.Millisecond), slowest.path, slowest.d.Round(time.Microsecond), biggest.path, biggest.bytes)
	if total != services {
		t.Errorf("/provider: %d serviços somados, tabela %d", total, services)
	}
	if full && slowest.d > realMaxProvider {
		t.Errorf("%s levou %v (limite %v)", slowest.path, slowest.d, realMaxProvider)
	}
	rec, _ = get("/provider/02.558.157%2F0001-62", true)
	if tel := decode[ProviderResponse](t, rec); tel.Name != "TELEFONICA BRASIL S.A." || !slices.Contains(tel.ServiceCodes, "045") {
		t.Errorf("Telefônica = %+v", tel)
	}
	if rec := do(h, "GET", base+"/provider/00000000000000"); rec.Code != 404 {
		t.Errorf("/provider/00000000000000 = %d", rec.Code)
	}

	// /search: termos comuns, raros e com curingas; a ordem é a do banco.
	for _, q := range []string{"telefonica", "telecom", "ltda", "internet", "fibra", "100%", "net_", "são paulo", "zzqqxx", "---"} {
		path := "/search?q=" + url.QueryEscape(q)
		rec, tm := get(path, true)
		got := decode[SearchResponse](t, rec)
		if got.Query != q || got.Count != len(got.Providers) || got.Count > SearchLimit {
			t.Errorf("%s: %+v", path, got)
		}
		for _, p := range got.Providers {
			if !strings.Contains(strings.ToLower(p.Name), q) && (p.TradeName == nil || !strings.Contains(strings.ToLower(*p.TradeName), q)) {
				t.Errorf("%s: %s (%v) não contém o termo", path, p.Name, p.TradeName)
			}
		}
		if full && tm.d > 10*realMaxSearch {
			t.Errorf("%s levou %v", path, tm.d)
		}
	}
	// Busca rara repetida direto no store: o plano não pode virar genérico.
	for i := range 12 {
		start := time.Now()
		if _, err := st.Search(ctx, fmt.Sprintf("zzqq%d", i), SearchLimit+1); err != nil {
			t.Fatal(err)
		}
		if d := time.Since(start); full && d > realMaxSearch {
			t.Errorf("busca rara %d levou %v (limite %v)", i, d, realMaxSearch)
		}
	}
	get("/search?q=telefonica&limit=5", true)

	// Índices (com o arquivo inteiro, o planejador escolhe sozinho; com a
	// fixture, pequena, só sem seq scan).
	tx, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if !full {
		if _, err := tx.Exec(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
			t.Fatal(err)
		}
	}
	for q, idx := range map[string]string{
		`SELECT * FROM anatel_pst_provider WHERE document = '02558157000162'`: "uq_anatel_pst_provider_document",
		`SELECT s.* FROM anatel_pst_service s WHERE s.provider_uuid = (SELECT uuid FROM anatel_pst_provider WHERE document = '02558157000162')
		  ORDER BY service_code, notified_on, notification_fistel`: "uq_anatel_pst_service_key",
		`SELECT min(service_name), min(service_group) FROM anatel_pst_service WHERE service_code = '010'`: "ix_anatel_pst_service_service_code",
		`SELECT service_code, min(service_name), min(service_group), count(DISTINCT provider_uuid), count(*)
		  FROM anatel_pst_service GROUP BY service_code ORDER BY service_code`: "ix_anatel_pst_service_service_code",
		`SELECT p.document FROM anatel_pst_provider p WHERE EXISTS (SELECT 1 FROM anatel_pst_service s
		  WHERE s.provider_uuid = p.uuid AND s.service_code = '010') ORDER BY p.document`: "ix_anatel_pst_service_service_code",
		`SELECT document FROM anatel_pst_provider WHERE name ILIKE '%zzqqxx%' OR trade_name ILIKE '%zzqqxx%'
		  ORDER BY name, document LIMIT 101`: "ix_anatel_pst_provider_name_trgm",
	} {
		rows, err := tx.Query(ctx, "EXPLAIN "+q)
		if err != nil {
			t.Fatal(err)
		}
		var plan []string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatal(err)
			}
			plan = append(plan, line)
		}
		rows.Close()
		text := strings.Join(plan, "\n")
		if !strings.Contains(text, idx) {
			t.Errorf("%s: plano sem %s:\n%s", q, idx, text)
		}
		if full {
			t.Logf("plano de %.60s…:\n%s", strings.Join(strings.Fields(q), " "), text)
		}
	}
}
