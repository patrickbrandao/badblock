//go:build integration

package httpapi

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"

	"github.com/patrickbrandao/badblock/apps/ripe/asnames/api/internal/cache"
	"github.com/patrickbrandao/badblock/apps/ripe/asnames/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/ripe/asnames/api/internal/realip"
	"github.com/patrickbrandao/badblock/apps/ripe/asnames/api/internal/store"
	"github.com/patrickbrandao/badblock/apps/ripe/asnames/api/internal/testdb"
)

// TestRealFile carrega o asn.txt real (RIPE_ASNAMES_REAL_FILE) num PG18
// descartável e passa pela API inteira (handler + store reais, sem cache):
// mede o tempo de cada rota, o tamanho de /country de todos os países (o
// maior precisa caber folgado no limite de 8 MB do cache) e confere que as
// consultas usam os índices.
//
//	curl -o /tmp/asn.txt https://ftp.ripe.net/ripe/asnames/asn.txt
//	RIPE_ASNAMES_REAL_FILE=/tmp/asn.txt make test-int
func TestRealFile(t *testing.T) {
	path := os.Getenv("RIPE_ASNAMES_REAL_FILE")
	if path == "" {
		t.Skip("defina RIPE_ASNAMES_REAL_FILE com o asn.txt real para rodar")
	}
	url, admin := testdb.New(t)
	ctx := context.Background()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rows := parseReal(t, raw)
	start := time.Now()
	n, err := admin.CopyFrom(ctx, pgx.Identifier{"ripe_asnames_asn"},
		[]string{"asn", "description", "handle", "name", "country"}, pgx.CopyFromRows(rows))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if _, err := admin.Exec(ctx, `
		INSERT INTO ripe_asnames_run (status, url, sha256, bytes, asns, started_at) VALUES (1, $1, $2, $3, $4, NOW())`,
		SourceURL, hex.EncodeToString(sum[:]), len(raw), n); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `
		INSERT INTO jobs (app, last_sync_at, last_check_at) VALUES ('collector-ripe-asnames', NOW(), NOW());
		ANALYZE ripe_asnames_asn;`); err != nil {
		t.Fatal(err)
	}
	t.Logf("carga: %d ASNs em %v", n, time.Since(start).Round(time.Millisecond))

	st, err := store.Open(ctx, url, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	w := dataset.NewWatcher(st, time.Minute, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := w.Refresh(ctx); err != nil || !w.Current().Ready() {
		t.Fatalf("dataset: %v", err)
	}
	rip, _ := realip.New(realip.DefaultTrustedProxies, realip.DefaultHeaders)
	h := New(st, w, cache.Noop{}, rip, slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config{BasePath: "/ripe/asnames", Version: "test", DBTimeout: 5 * time.Second}).Handler()

	get := func(p string, want int) *[]byte {
		t.Helper()
		begin := time.Now()
		rec := do(h, "GET", p)
		ms := float64(time.Since(begin).Microseconds()) / 1000
		if rec.Code != want {
			t.Errorf("%s: %d, quero %d: %.300s", p, rec.Code, want, rec.Body)
		}
		t.Logf("%-45s %d %8d bytes %7.1f ms", p, rec.Code, rec.Body.Len(), ms)
		b := rec.Body.Bytes()
		return &b
	}

	// Rotas pontuais.
	var asn ASNResponse
	_ = json.Unmarshal(*get("/ripe/asnames/asn/15169", 200), &asn)
	if asn.Handle == nil || *asn.Handle != "GOOGLE" || *asn.Country != "US" {
		t.Errorf("AS15169 = %+v", asn)
	}
	get("/ripe/asnames/v1/asn/AS61613", 200)
	get("/ripe/asnames/asn/4294967295", 404)
	var hr HandleResponse
	_ = json.Unmarshal(*get("/ripe/asnames/handle/google", 200), &hr)
	if hr.Count < 1 {
		t.Errorf("handle google = %+v", hr)
	}
	get("/ripe/asnames/handle/VRSN-AC50-340", 200)
	get("/ripe/asnames/handle/ICE%2FHT", 200)
	get("/ripe/asnames/handle/orange%20c%C3%B4te%20d'ivoire", 200)

	// Buscas: rara, comum (cortada em 100), sem trigramas (varre a tabela),
	// com curingas e sem resultado.
	for q, truncated := range map[string]bool{
		"google": false, "net": true, "telecom%20ltd": true, "---": false,
		"100%25": false, "c%C3%B4te%20d'ivoire": false, "zzqqxx": false, "a%20b": true,
	} {
		var s SearchResponse
		_ = json.Unmarshal(*get("/ripe/asnames/search?q="+q, 200), &s)
		if s.Truncated != truncated || (truncated && s.Count != SearchLimit) {
			t.Errorf("busca %s: count %d, truncated %v", q, s.Count, s.Truncated)
		}
		if !slices.IsSortedFunc(s.ASNs, func(a, b ASNEntry) int { return int(a.ASN - b.ASN) }) {
			t.Errorf("busca %s fora de ordem", q)
		}
	}

	// Termo raro repetido: o plano tem que continuar usando o índice trigram
	// depois das 5 primeiras execuções da consulta preparada (sem
	// plan_cache_mode = force_custom_plan, o plano genérico varre a tabela em
	// ~65 ms). Direto no store, sem o cache.
	var slowest time.Duration
	for i := range 12 {
		begin := time.Now()
		if _, err := st.Search(ctx, "zzqq"+strconv.Itoa(i), SearchLimit+1); err != nil {
			t.Fatal(err)
		}
		slowest = max(slowest, time.Since(begin))
	}
	t.Logf("busca rara repetida 12 vezes: a mais lenta levou %v", slowest.Round(10*time.Microsecond))
	if slowest > 25*time.Millisecond {
		t.Errorf("busca rara levou %v: o plano genérico voltou?", slowest)
	}

	// /country de todos os países: o maior precisa caber folgado no cache.
	cr, err := admin.Query(ctx, `SELECT DISTINCT country FROM ripe_asnames_asn WHERE country IS NOT NULL ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	countries, err := pgx.CollectRows(cr, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	type size struct {
		cc    string
		bytes int
		count int
		ms    float64
	}
	var sizes []size
	total := 0
	for _, cc := range countries {
		begin := time.Now()
		rec := do(h, "GET", "/ripe/asnames/country/"+strings.ToLower(cc))
		ms := float64(time.Since(begin).Microseconds()) / 1000
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", cc, rec.Code, rec.Body)
		}
		var c CountryResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &c)
		sizes = append(sizes, size{cc, rec.Body.Len(), c.Count, ms})
		total += c.Count
	}
	slices.SortFunc(sizes, func(a, b size) int { return b.bytes - a.bytes })
	for _, s := range sizes[:5] {
		t.Logf("country %s: %d ASNs, %d bytes (%.1f%% do limite), %.1f ms", s.cc, s.count, s.bytes,
			100*float64(s.bytes)/maxCachedBody, s.ms)
	}
	if int64(total) != n {
		t.Errorf("soma dos países = %d, ASNs = %d", total, n)
	}
	if sizes[0].bytes > maxCachedBody/2 {
		t.Errorf("maior /country (%s) tem %d bytes: não cabe folgado no limite de %d do cache", sizes[0].cc, sizes[0].bytes, maxCachedBody)
	}

	var meta MetaResponse
	_ = json.Unmarshal(*get("/ripe/asnames/meta", 200), &meta)
	if meta.Dataset == nil || int64(meta.Dataset.ASNs) != n || meta.Collector == nil {
		t.Errorf("meta = %+v", meta)
	}

	// Planos: cada consulta usa o índice pensado para ela.
	for q, idx := range map[string]string{
		`SELECT * FROM ripe_asnames_asn WHERE asn = 15169`:                                         "uq_ripe_asnames_asn_asn",
		`SELECT * FROM ripe_asnames_asn WHERE lower(handle) = lower('google') ORDER BY asn`:        "ix_ripe_asnames_asn_handle",
		`SELECT * FROM ripe_asnames_asn WHERE description ILIKE '%google%' ORDER BY asn LIMIT 101`: "ix_ripe_asnames_asn_description_trgm",
		`SELECT asn FROM ripe_asnames_asn WHERE country = 'BR' ORDER BY asn`:                       "ripe_asnames_asn",
	} {
		var plan strings.Builder
		pr, err := admin.Query(ctx, "EXPLAIN "+q)
		if err != nil {
			t.Fatal(err)
		}
		lines, err := pgx.CollectRows(pr, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		plan.WriteString(strings.Join(lines, "\n"))
		if !strings.Contains(plan.String(), idx) || strings.Contains(plan.String(), "Seq Scan") {
			t.Errorf("plano de %s não usa %s:\n%s", q, idx, plan.String())
		}
	}
}

// parseReal lê o asn.txt com as regras do collector-ripe-asnames
// (specs/fontes/ripe/asnames/fonte.md), só para carregar o teste.
func parseReal(t *testing.T, raw []byte) [][]any {
	t.Helper()
	var rows [][]any
	sc := bufio.NewScanner(strings.NewReader(string(raw)))
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		num, desc, ok := strings.Cut(line, " ")
		desc = strings.TrimSpace(desc)
		asn, err := strconv.ParseInt(num, 10, 64)
		if !ok || err != nil || desc == "" {
			t.Fatalf("linha fora do formato: %q", line)
		}
		handle, name, country := derive(desc)
		rows = append(rows, []any{asn, desc, null(handle), null(name), null(country)})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

func null(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// derive repete as regras de handle/name/country do collector-ripe-asnames.
func derive(desc string) (handle, name, country string) {
	body := desc
	if n := len(desc); n >= 4 && desc[n-4:n-2] == ", " && isUpper(desc[n-2]) && isUpper(desc[n-1]) {
		country, body = desc[n-2:], desc[:n-4]
	}
	trimmed := strings.TrimSpace(body)
	// "X - X" (AFRINIC), inclusive com " - " dentro de X.
	if k := (len(trimmed) - 3) / 2; len(trimmed) >= 5 && (len(trimmed)-3)%2 == 0 &&
		trimmed[k:k+3] == " - " && trimmed[:k] == trimmed[k+3:] {
		return trimmed[:k], trimmed[:k], country
	}
	padded := " " + body + " "
	if before, after, ok := strings.Cut(padded, " - "); ok {
		if h := strings.TrimSpace(before); !strings.ContainsFunc(h, unicode.IsSpace) {
			return h, strings.TrimSpace(after), country
		}
	}
	if i := strings.IndexFunc(trimmed, unicode.IsSpace); i >= 0 {
		return trimmed[:i], strings.TrimSpace(trimmed[i:]), country
	}
	return trimmed, "", country
}

func isUpper(b byte) bool { return b >= 'A' && b <= 'Z' }
