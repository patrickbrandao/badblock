//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/patrickbrandao/badblock/apps/apnic/api/internal/cache"
	"github.com/patrickbrandao/badblock/apps/apnic/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/apnic/api/internal/realip"
	"github.com/patrickbrandao/badblock/apps/apnic/api/internal/rir"
	"github.com/patrickbrandao/badblock/apps/apnic/api/internal/store"
)

// TestRealFile carrega o arquivo real inteiro do RIR com o binário do
// collector (apps/<fonte>/collector, compilado aqui e servido o arquivo por
// um httptest.Server) num PG18 descartável e mede as rotas da API sobre esses
// dados: tempos, tamanho da resposta do maior titular e uso de índice.
// Pulado se APNIC_REAL_FILE não aponta para o arquivo (baixe com curl):
//
//	make test-real FILE=/caminho/do/delegated-...-extended-latest
func TestRealFile(t *testing.T) {
	path := os.Getenv("APNIC_REAL_FILE")
	if path == "" {
		t.Skip("defina APNIC_REAL_FILE com o caminho do arquivo real")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	pgURL, admin := realDB(t)

	// Compila o collector do mesmo RIR e carrega o arquivo com --once.
	bin := filepath.Join(t.TempDir(), store.CollectorApp)
	build := exec.Command("go", "build", "-o", bin, "./cmd/"+store.CollectorApp)
	build.Dir = filepath.Join("..", "..", "..", "collector")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build do collector: %v\n%s", err, out)
	}
	files := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, path)
	}))
	defer files.Close()
	run := exec.Command(bin, "--once")
	run.Env = append(os.Environ(), "POSTGRES_URL="+pgURL, "SOURCE_URL="+files.URL+"/delegated-extended",
		"SOURCE_MD5_URL=off", "LOG_FORMAT=text")
	start := time.Now()
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("collector --once: %v\n%s", err, out)
	}
	t.Logf("carga pelo collector: %v", time.Since(start).Round(time.Millisecond))
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
	h := New(st, w, cache.Noop{}, rip, log, Config{BasePath: rir.BasePath, Version: "real"}).Handler()

	get := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		start := time.Now()
		rec := do(h, "GET", rir.BasePath+path)
		if rec.Code != 200 {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Body)
		}
		t.Logf("%-45s %3d %9d bytes %v", path, rec.Code, rec.Body.Len(), time.Since(start).Round(time.Microsecond))
		return rec
	}

	// Meta bate com as tabelas.
	meta := decode[MetaResponse](t, get("/meta"))
	var asnRows, v4, v6 int
	_ = admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM apnic_asn),
		(SELECT count(*) FROM apnic_prefix WHERE family = 4), (SELECT count(*) FROM apnic_prefix WHERE family = 6)`).
		Scan(&asnRows, &v4, &v6)
	if meta.Dataset == nil || *meta.Dataset.ASNRecords != asnRows || *meta.Dataset.PrefixesV4 != v4 ||
		*meta.Dataset.PrefixesV6 != v6 || meta.Collector == nil || meta.Collector.LastSyncAt == nil {
		t.Errorf("meta = %+v, tabelas %d/%d/%d", meta.Dataset, asnRows, v4, v6)
	}
	t.Logf("tabelas: %d registros de ASN, %d blocos IPv4, %d blocos IPv6", asnRows, v4, v6)

	// Amostras tiradas dos próprios dados (o teste vale para qualquer RIR).
	sample := func(q string) string {
		var s string
		if err := admin.QueryRow(ctx, q).Scan(&s); err != nil {
			t.Logf("sem amostra para %q: %v", q, err)
		}
		return s
	}
	asn := sample(`SELECT asn_start::text FROM apnic_asn WHERE opaque_id IS NOT NULL ORDER BY asn_start
		OFFSET (SELECT count(*) / 2 FROM apnic_asn WHERE opaque_id IS NOT NULL) LIMIT 1`)
	get("/asn/" + asn)
	get("/v1/asn/AS" + asn)
	if mid := sample(`SELECT (asn_start + asn_count / 2)::text FROM apnic_asn WHERE asn_count > 1 ORDER BY asn_count DESC LIMIT 1`); mid != "" {
		r := decode[ASNResponse](t, get("/asn/"+mid))
		if r.Range.Count < 2 {
			t.Errorf("faixa = %+v", r.Range)
		}
	}
	ip4 := sample(`SELECT host(prefix + 1) FROM apnic_prefix WHERE family = 4 ORDER BY prefix
		OFFSET (SELECT count(*) / 2 FROM apnic_prefix WHERE family = 4) LIMIT 1`)
	get("/ip/" + ip4)
	ip6 := sample(`SELECT host(prefix + 1) FROM apnic_prefix WHERE family = 6 AND status IN ('allocated', 'assigned') ORDER BY prefix
		OFFSET (SELECT count(*) / 2 FROM apnic_prefix WHERE family = 6 AND status IN ('allocated', 'assigned')) LIMIT 1`)
	get("/ip/" + ip6)
	if pfx := sample(`SELECT prefix::text FROM apnic_prefix WHERE family = 4 ORDER BY prefix LIMIT 1`); pfx != "" {
		p := decode[PrefixResponse](t, get("/prefix/"+pfx))
		if !p.Exact {
			t.Errorf("prefixo exato = %+v", p)
		}
	}
	// Registro IPv4 dividido em vários CIDRs (não existe em todos os RIRs).
	if split := sample(`SELECT host(prefix) FROM apnic_prefix WHERE family = 4 AND host(prefix)::inet <> record_start LIMIT 1`); split != "" {
		r := decode[IPResponse](t, get("/ip/"+split))
		if r.Record.Start == split {
			t.Errorf("bloco dividido deveria apontar para o início do registro: %+v", r)
		}
	}

	// Maior titular (em número de ASNs + blocos): a resposta cabe folgada no
	// limite de 8 MB do cache.
	var holder string
	var items int
	_ = admin.QueryRow(ctx, `SELECT opaque_id, count(*) FROM (
		SELECT opaque_id FROM apnic_asn UNION ALL SELECT opaque_id FROM apnic_prefix) x
		WHERE opaque_id IS NOT NULL GROUP BY opaque_id ORDER BY count(*) DESC LIMIT 1`).Scan(&holder, &items)
	rec := get("/holder/" + holder)
	hr := decode[HolderResponse](t, rec)
	if hr.Counts.ASNs+hr.Counts.IPv4+hr.Counts.IPv6 != items || rec.Body.Len() > maxCachedBody/10 {
		t.Errorf("maior titular %s: %+v, %d itens, %d bytes", holder, hr.Counts, items, rec.Body.Len())
	}
	t.Logf("maior titular: %s, %d itens (%+v), cc %v %v, %d bytes", holder, items, hr.Counts, str(hr.CC), hr.CCs, rec.Body.Len())

	// Latência média de consultas aleatórias (sem cache).
	rows, err := admin.Query(ctx, `SELECT host(prefix + 1) FROM apnic_prefix ORDER BY random() LIMIT 500`)
	if err != nil {
		t.Fatal(err)
	}
	var ips []string
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		ips = append(ips, s)
	}
	rows.Close()
	start = time.Now()
	for _, ip := range ips {
		if rec := do(h, "GET", rir.BasePath+"/ip/"+ip); rec.Code != 200 {
			t.Errorf("/ip/%s: %d", ip, rec.Code)
		}
	}
	t.Logf("%d /ip aleatórios: %v em média", len(ips), (time.Since(start) / time.Duration(max(len(ips), 1))).Round(time.Microsecond))
	start = time.Now()
	for range 500 {
		n := rand.Int64N(400000)
		if rec := do(h, "GET", fmt.Sprintf("%s/asn/%d", rir.BasePath, n)); rec.Code != 200 && rec.Code != 404 {
			t.Errorf("/asn/%d: %d", n, rec.Code)
		}
	}
	t.Logf("500 /asn aleatórios: %v em média", (time.Since(start) / 500).Round(time.Microsecond))

	// As consultas usam índice.
	for _, q := range []string{
		"EXPLAIN SELECT * FROM apnic_asn WHERE asn_start <= 61613 ORDER BY asn_start DESC LIMIT 1",
		"EXPLAIN SELECT * FROM apnic_prefix WHERE prefix >>= '" + ip4 + "'::cidr ORDER BY masklen(prefix) DESC LIMIT 1",
		"EXPLAIN SELECT * FROM apnic_asn WHERE opaque_id = '" + holder + "' ORDER BY asn_start",
		"EXPLAIN SELECT * FROM apnic_prefix WHERE opaque_id = '" + holder + "' ORDER BY family, prefix",
	} {
		plan := explain(t, admin, q)
		if !strings.Contains(plan, "Index") && !strings.Contains(plan, "Bitmap") {
			t.Errorf("sem índice: %s → %s", q, plan)
		}
	}
}

// realDB sobe um PG18 com as migrations de central/ e do RIR.
func realDB(t *testing.T) (string, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	pg, err := postgres.Run(ctx, "postgres:18-trixie",
		postgres.WithDatabase("badblock"), postgres.WithUsername("postgres"), postgres.WithPassword("pg"),
		postgres.BasicWaitStrategies())
	testcontainers.CleanupContainer(t, pg)
	if err != nil {
		t.Fatal(err)
	}
	url, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	for _, dir := range []string{"central", rir.Source} {
		files, _ := filepath.Glob(filepath.Join("..", "..", "..", "..", "..", "database", "postgres", dir, "*.sql"))
		if len(files) == 0 {
			t.Fatalf("nenhuma migration em database/postgres/%s", dir)
		}
		for _, f := range files {
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			up, _, _ := strings.Cut(string(raw), "-- migrate:down")
			if _, err := admin.Exec(ctx, up); err != nil {
				t.Fatalf("%s: %v", f, err)
			}
		}
	}
	return url, admin
}

func explain(t *testing.T, db *pgxpool.Pool, q string) string {
	t.Helper()
	rows, err := db.Query(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var line string
		_ = rows.Scan(&line)
		plan = append(plan, strings.TrimSpace(line))
	}
	return strings.Join(plan, " / ")
}

func str(s *string) string {
	if s == nil {
		return "null"
	}
	b, _ := json.Marshal(*s)
	return string(b)
}
