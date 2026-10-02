//go:build integration

package store

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/patrickbrandao/badblock/apps/anatel/pst/collector/internal/archive"
	"github.com/patrickbrandao/badblock/apps/anatel/pst/collector/internal/parse"
)

// setup sobe um PG18, aplica as migrations de central/ e anatel_pst/ (seção
// migrate:up) e devolve um Store e um pool administrativo no mesmo banco,
// ambos com o usuário postgres, como em produção.
func setup(t *testing.T) (*Store, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	pg, err := postgres.Run(ctx, "postgres:18-trixie",
		postgres.WithDatabase("badblock"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("pg"),
		postgres.BasicWaitStrategies(),
	)
	testcontainers.CleanupContainer(t, pg)
	if err != nil {
		t.Fatal(err)
	}
	superURL, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	admin, err := pgxpool.New(ctx, superURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)

	// O app está em apps/anatel/pst/collector: seis níveis até a raiz.
	for _, dir := range []string{"central", "anatel_pst"} {
		files, _ := filepath.Glob(filepath.Join("..", "..", "..", "..", "..", "..", "database", "postgres", dir, "*.sql"))
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

	st, err := Open(ctx, superURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	return st, admin
}

func sample(t *testing.T) *parse.Dataset {
	t.Helper()
	f, err := os.Open("../../testdata/pst-sample.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ds, err := parse.Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	return ds
}

var modified = time.Date(2026, 9, 30, 9, 15, 8, 0, time.UTC)

func runFor(sha string, ds *parse.Dataset) Run {
	return Run{StartedAt: time.Now(), URL: "https://example.test/pst.zip", HTTPStatus: 200,
		ETag: `"e"`, LastModified: "Wed, 30 Sep 2026 10:56:48 GMT", SHA256: strings.Repeat(sha, 64), Bytes: 100,
		CSVName: "prestadoras_servicos_telecomunicacoes.csv", CSVSHA256: strings.Repeat(sha, 64), CSVBytes: 1000,
		CSVModifiedAt: modified, Parsed: true, Rows: ds.Rows, RowsCNPJ: ds.RowsCNPJ, RowsCPF: ds.RowsCPF,
		Duplicates: ds.Duplicates, Skipped: ds.Skipped, Providers: len(ds.Providers), Services: ds.Services(),
		Warnings: ds.Warnings}
}

type job struct {
	lastSync, lastCheck *time.Time
	consolidated        int
}

func readJob(t *testing.T, admin *pgxpool.Pool) job {
	t.Helper()
	var j job
	err := admin.QueryRow(context.Background(),
		"SELECT last_sync_at, last_check_at, consolidated FROM jobs WHERE app = $1", AppName).
		Scan(&j.lastSync, &j.lastCheck, &j.consolidated)
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func count(t *testing.T, admin *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := admin.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

func without(ds *parse.Dataset, docs ...string) []parse.Provider {
	var out []parse.Provider
	for _, p := range ds.Providers {
		if !strings.Contains(strings.Join(docs, " "), p.Document) {
			out = append(out, p)
		}
	}
	return out
}

func TestApplyLifecycle(t *testing.T) {
	st, admin := setup(t)
	ctx := context.Background()
	opt := ApplyOptions{RemovalThreshold: 0.05}

	if last, err := st.LastApplied(ctx); err != nil || last != nil {
		t.Fatalf("banco vazio: last = %+v, err = %v", last, err)
	}

	// 1) Carga inicial.
	ds := sample(t)
	ch, v1, err := st.Apply(ctx, ds, runFor("a", ds), opt)
	if err != nil {
		t.Fatal(err)
	}
	if ch != (Changes{ProviderInserted: 8, ServiceInserted: 52}) {
		t.Errorf("carga inicial = %+v", ch)
	}
	if j := readJob(t, admin); j.lastSync == nil || j.lastCheck == nil || j.consolidated != 0 {
		t.Errorf("jobs depois da carga = %+v", j)
	}
	if n := count(t, admin, `SELECT count(*) FROM anatel_pst_service s JOIN anatel_pst_provider p ON p.uuid = s.provider_uuid
		WHERE p.document = '02558157000162'`); n != 42 {
		t.Errorf("serviços da telefonica = %d", n)
	}
	if n := count(t, admin, `SELECT count(*) FROM anatel_pst_service
		WHERE grant_fistel IS NULL AND grant_process IS NULL AND granted_on IS NULL AND service_code = '401'`); n != 1 {
		t.Errorf("dispensada com NULLs = %d", n)
	}
	var name, complement string
	var phone, email *string
	_ = admin.QueryRow(ctx, "SELECT name FROM anatel_pst_provider WHERE document = '01763250000146'").Scan(&name)
	_ = admin.QueryRow(ctx, "SELECT complement FROM anatel_pst_provider WHERE document = '01600200001110'").Scan(&complement)
	_ = admin.QueryRow(ctx, "SELECT phone, email FROM anatel_pst_provider WHERE document = '02839640000115'").Scan(&phone, &email)
	if name != `TRANS "S" LTDA` || complement != ": KM 11; GALPAO: 1;" || phone != nil || email != nil {
		t.Errorf("campos: %q, %q, %v, %v", name, complement, phone, email)
	}
	if n := count(t, admin, `SELECT count(*) FROM anatel_pst_run WHERE status = 1 AND rows = 158 AND rows_cnpj = 155
		AND rows_cpf = 3 AND duplicates = 103 AND skipped = 0 AND providers = 8 AND services = 52
		AND csv_name IS NOT NULL AND csv_modified_at = '2026-09-30 06:15:08-03' AND error IS NULL
		AND provider_inserted = 8 AND service_inserted = 52 AND warnings = '[]'::jsonb`); n != 1 {
		t.Errorf("linha aplicada em anatel_pst_run não confere (%d)", n)
	}
	last, err := st.LastApplied(ctx)
	if err != nil || last.Version != v1 || last.CSVSHA256 != strings.Repeat("a", 64) || !last.CSVModifiedAt.Equal(modified) ||
		last.ETag != `"e"` || last.LastModified == "" {
		t.Errorf("LastApplied = %+v, err = %v", last, err)
	}

	// 2) A consolidação (fase 2) marca 1; o mesmo conteúdo não muda nada.
	if _, err := admin.Exec(ctx, "UPDATE jobs SET consolidated = 1"); err != nil {
		t.Fatal(err)
	}
	var uuidBefore string
	_ = admin.QueryRow(ctx, "SELECT uuid::text FROM anatel_pst_provider WHERE document = '02558157000162'").Scan(&uuidBefore)
	ds = sample(t)
	ch, v2, err := st.Apply(ctx, ds, runFor("b", ds), opt)
	if err != nil {
		t.Fatal(err)
	}
	if ch.Total() != 0 || v2 == v1 {
		t.Errorf("reaplicação = %+v, versões %s/%s", ch, v1, v2)
	}
	if j := readJob(t, admin); j.consolidated != 1 {
		t.Errorf("sem mudança a flag deve continuar 1: %+v", j)
	}

	// 3) Mudanças: a Vigillare (4 serviços) sai, a WHIM muda de nome, um
	// serviço da Telefonica muda de data, outro sai e um entra, e chega uma
	// prestadora nova. Limite 0,2 (1 de 8 prestadoras = 12,5%).
	ds = sample(t)
	ds.Providers = without(ds, "02883607000192")
	for i := range ds.Providers {
		p := &ds.Providers[i]
		switch p.Document {
		case "07807833000108":
			p.Name = "WHIM BRAZIL TELECOM LTDA (NOVO NOME)"
		case "02558157000162":
			p.Services[0].NotifiedOn = time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)
			p.Services = p.Services[:len(p.Services)-1]
			p.Services = append(p.Services, parse.Service{EntityType: "Outorgada", GrantType: "Serviços de Interesse Coletivo e Restrito - SIC",
				GrantFistel: "50423150120", ServiceGroup: "Banda Larga Fixa", ServiceCode: "045",
				ServiceName: "Serviço de Comunicação Multimídia", NotificationFistel: "50499999999"})
		}
	}
	ds.Providers = append(ds.Providers, parse.Provider{Document: "11222333000181", Name: "EMPRESA NOVA LTDA",
		Services: []parse.Service{{EntityType: "Dispensada de Outorga", GrantType: "Dispensada de Outorga", ServiceGroup: "Rádio do Cidadão - Dispensa de Outorga",
			ServiceCode: "401", ServiceName: "Rádio do Cidadão - Dispensa de Autorização", NotificationFistel: "50400000001"}}})
	ch, _, err = st.Apply(ctx, ds, runFor("c", ds), ApplyOptions{RemovalThreshold: 0.2})
	if err != nil {
		t.Fatal(err)
	}
	want := Changes{ProviderInserted: 1, ProviderUpdated: 1, ProviderDeleted: 1, ServiceInserted: 2, ServiceUpdated: 1, ServiceDeleted: 5}
	if ch != want {
		t.Errorf("mudanças = %+v, quero %+v", ch, want)
	}
	if j := readJob(t, admin); j.consolidated != 0 {
		t.Errorf("com mudança a flag volta a 0: %+v", j)
	}
	var uuidAfter string
	_ = admin.QueryRow(ctx, "SELECT uuid::text FROM anatel_pst_provider WHERE document = '02558157000162'").Scan(&uuidAfter)
	if uuidAfter != uuidBefore {
		t.Errorf("prestadora que continua mantém o uuid: %s → %s", uuidBefore, uuidAfter)
	}
	if n := count(t, admin, `SELECT count(*) FROM anatel_pst_service s WHERE NOT EXISTS
		(SELECT 1 FROM anatel_pst_provider p WHERE p.uuid = s.provider_uuid)`); n != 0 {
		t.Errorf("%d serviços órfãos", n)
	}
	if n := count(t, admin, "SELECT count(*) FROM anatel_pst_service"); n != 52-5+2 {
		t.Errorf("serviços = %d", n)
	}

	// 4) Cascata: uma prestadora apagada à mão leva os serviços junto.
	if _, err := admin.Exec(ctx, "DELETE FROM anatel_pst_provider WHERE document = '11222333000181'"); err != nil {
		t.Fatal(err)
	}
	if n := count(t, admin, "SELECT count(*) FROM anatel_pst_service WHERE notification_fistel = '50400000001'"); n != 0 {
		t.Errorf("cascata não apagou o serviço")
	}

	// 5) Trava de remoção em massa, e --force.
	small := &parse.Dataset{Providers: ds.Providers[:2]}
	_, _, err = st.Apply(ctx, small, runFor("d", ds), opt)
	if re, ok := errors.AsType[*RemovalError](err); !ok || re.Entity != "prestadoras" {
		t.Fatalf("esperava RemovalError de prestadoras, veio %v", err)
	}
	if n := count(t, admin, "SELECT count(*) FROM anatel_pst_provider"); n != 7 {
		t.Errorf("recusa não pode mexer nas tabelas: %d prestadoras", n)
	}
	// Só serviços: a Telefonica perde 30 serviços (30 de 48 = 62,5%).
	fewer := sample(t)
	fewer.Providers = without(fewer, "02883607000192")
	for i := range fewer.Providers {
		if fewer.Providers[i].Document == "02558157000162" {
			fewer.Providers[i].Services = fewer.Providers[i].Services[:12]
		}
	}
	_, _, err = st.Apply(ctx, fewer, runFor("d", fewer), ApplyOptions{RemovalThreshold: 0.2})
	if re, ok := errors.AsType[*RemovalError](err); !ok || re.Entity != "serviços" {
		t.Fatalf("esperava RemovalError de serviços, veio %v", err)
	}
	ch, _, err = st.Apply(ctx, small, runFor("e", small), ApplyOptions{RemovalThreshold: 0.05, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if ch.ProviderDeleted != 5 || count(t, admin, "SELECT count(*) FROM anatel_pst_provider") != 2 {
		t.Errorf("com --force = %+v", ch)
	}

	// 6) Falhas ficam em anatel_pst_run e não viram versão.
	fail := Run{StartedAt: time.Now(), URL: "https://example.test/pst.zip", HTTPStatus: 200, SHA256: strings.Repeat("f", 64), Bytes: 10}
	if err := st.RecordFailure(ctx, fail, errors.New("ZIP sem CSV")); err != nil {
		t.Fatal(err)
	}
	last, err = st.LastApplied(ctx)
	if err != nil || last == nil || last.SHA256 != strings.Repeat("e", 64) {
		t.Errorf("LastApplied = %+v, err = %v", last, err)
	}
	if n := count(t, admin, `SELECT count(*) FROM anatel_pst_run WHERE status = 0 AND error = 'ZIP sem CSV'
		AND csv_name IS NULL AND rows IS NULL AND provider_inserted IS NULL AND warnings = '[]'::jsonb`); n != 1 {
		t.Errorf("falhas gravadas = %d", n)
	}

	// 7) Verificação sem mudança só toca last_check_at.
	before := readJob(t, admin)
	if err := st.TouchCheck(ctx); err != nil {
		t.Fatal(err)
	}
	after := readJob(t, admin)
	if !after.lastCheck.After(*before.lastCheck) || !after.lastSync.Equal(*before.lastSync) {
		t.Errorf("TouchCheck: antes %+v, depois %+v", before, after)
	}
}

func TestApplyBusy(t *testing.T) {
	st, admin := setup(t)
	ctx := context.Background()
	tx, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", AppName); err != nil {
		t.Fatal(err)
	}
	ds := sample(t)
	if _, _, err := st.Apply(ctx, ds, runFor("a", ds), ApplyOptions{RemovalThreshold: 0.05}); !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v, quero ErrBusy", err)
	}
	if n := count(t, admin, "SELECT count(*) FROM anatel_pst_run"); n != 0 {
		t.Errorf("concorrência não grava execução: %d", n)
	}
}

// TestApplyRealFile aplica o ZIP real inteiro, quando ANATEL_PST_REAL_FILE
// aponta para ele (pulado se ausente): carga completa e reaplicação
// idempotente, com os tempos no log.
func TestApplyRealFile(t *testing.T) {
	path := os.Getenv("ANATEL_PST_REAL_FILE")
	if path == "" {
		t.Skip("defina ANATEL_PST_REAL_FILE com o caminho do ZIP real")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data := raw
	if strings.HasSuffix(strings.ToLower(path), ".zip") {
		start := time.Now()
		c, err := archive.Extract(raw, archive.MaxCSVBytes)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("extração: %v (%d → %d bytes)", time.Since(start), len(raw), len(c.Data))
		data = c.Data
	}
	st, admin := setup(t)
	ctx := context.Background()
	opt := ApplyOptions{RemovalThreshold: 0.05}

	start := time.Now()
	ds, err := parse.Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("parser: %v (%d prestadoras, %d serviços)", time.Since(start), len(ds.Providers), ds.Services())

	start = time.Now()
	ch, _, err := st.Apply(ctx, ds, runFor("a", ds), opt)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("primeira carga: %v, %+v", time.Since(start), ch)
	if ch != (Changes{ProviderInserted: len(ds.Providers), ServiceInserted: ds.Services()}) {
		t.Errorf("primeira carga = %+v", ch)
	}

	start = time.Now()
	ch, _, err = st.Apply(ctx, ds, runFor("b", ds), opt)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("reaplicação: %v, %+v", time.Since(start), ch)
	if ch.Total() != 0 {
		t.Errorf("reaplicação deveria ser idempotente: %+v", ch)
	}

	prov, svc := count(t, admin, "SELECT count(*) FROM anatel_pst_provider"), count(t, admin, "SELECT count(*) FROM anatel_pst_service")
	if prov != len(ds.Providers) || svc != ds.Services() {
		t.Errorf("tabelas = %d/%d, dataset %d/%d", prov, svc, len(ds.Providers), ds.Services())
	}
	var size string
	_ = admin.QueryRow(ctx, `SELECT pg_size_pretty(pg_total_relation_size('anatel_pst_provider') + pg_total_relation_size('anatel_pst_service'))`).Scan(&size)
	t.Logf("tabelas: %d prestadoras, %d serviços, %s com índices", prov, svc, size)

	if _, err := admin.Exec(ctx, "ANALYZE anatel_pst_provider; ANALYZE anatel_pst_service"); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"EXPLAIN SELECT * FROM anatel_pst_provider WHERE document = '02558157000162'",
		"EXPLAIN SELECT * FROM anatel_pst_provider WHERE name ILIKE '%telefonica%' OR trade_name ILIKE '%telefonica%'",
		`EXPLAIN SELECT p.document FROM anatel_pst_provider p WHERE EXISTS
			(SELECT 1 FROM anatel_pst_service s WHERE s.provider_uuid = p.uuid AND s.service_code = '010')`,
	} {
		rows, err := admin.Query(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		var plan []string
		for rows.Next() {
			var line string
			_ = rows.Scan(&line)
			plan = append(plan, strings.TrimSpace(line))
		}
		rows.Close()
		joined := strings.Join(plan, " / ")
		if !strings.Contains(joined, "Index") && !strings.Contains(joined, "Bitmap") {
			t.Errorf("sem índice: %s → %s", q, joined)
		}
		t.Logf("%s → %s", q, joined)
	}
}
