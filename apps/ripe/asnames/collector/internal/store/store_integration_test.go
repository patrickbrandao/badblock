//go:build integration

package store

import (
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

	"github.com/patrickbrandao/badblock/apps/ripe/asnames/collector/internal/parse"
)

// setup sobe um PG18, aplica as migrations de central/ e asnames/ (seção
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

	for _, dir := range []string{"central", "ripe_asnames"} {
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
	f, err := os.Open("../../testdata/asn-sample.txt")
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

func runFor(sha string, asns int) Run {
	return Run{StartedAt: time.Now(), URL: "https://example.test/asn.txt", HTTPStatus: 200,
		ETag: `W/"e"`, LastModified: "Mon, 28 Sep 2026 10:49:00 GMT", SHA256: strings.Repeat(sha, 64), Bytes: 100, ASNs: asns}
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

type row struct {
	description           string
	handle, name, country *string
	createdAt, updatedAt  time.Time
}

func readASN(t *testing.T, admin *pgxpool.Pool, asn int64) row {
	t.Helper()
	var r row
	err := admin.QueryRow(context.Background(),
		"SELECT description, handle, name, country, created_at, updated_at FROM ripe_asnames_asn WHERE asn = $1", asn).
		Scan(&r.description, &r.handle, &r.name, &r.country, &r.createdAt, &r.updatedAt)
	if err != nil {
		t.Fatalf("AS%d: %v", asn, err)
	}
	return r
}

func str(p *string) string {
	if p == nil {
		return "<NULL>"
	}
	return *p
}

func count(t *testing.T, admin *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := admin.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestApplyLifecycle(t *testing.T) {
	st, admin := setup(t)
	ctx := context.Background()
	opt := ApplyOptions{RemovalThreshold: 0.05}

	if last, err := st.LastApplied(ctx); err != nil || last != nil {
		t.Fatalf("banco vazio: last = %+v, err = %v", last, err)
	}

	// 1) Carga inicial.
	ch, v1, err := st.Apply(ctx, sample(t), runFor("a", 37), opt)
	if err != nil {
		t.Fatal(err)
	}
	if ch != (Changes{ASNInserted: 37}) {
		t.Errorf("carga inicial = %+v", ch)
	}
	if j := readJob(t, admin); j.lastSync == nil || j.lastCheck == nil || j.consolidated != 0 {
		t.Errorf("jobs depois da carga = %+v", j)
	}
	if r := readASN(t, admin, 29571); str(r.handle) != "Orange Côte d'Ivoire" || str(r.name) != "Orange Côte d'Ivoire" || str(r.country) != "CI" {
		t.Errorf("AS29571 = %s / %s / %s", str(r.handle), str(r.name), str(r.country))
	}
	// Campos ausentes viram NULL; o texto original fica, com os controles C1.
	if r := readASN(t, admin, 7901); r.description != "- , NZ" || r.handle != nil || r.name != nil || str(r.country) != "NZ" {
		t.Errorf("AS7901 = %+v", r)
	}
	if r := readASN(t, admin, 59265); !strings.Contains(r.description, "â\u0080\u0093") {
		t.Errorf("AS59265 description = %q", r.description)
	}
	var gotRun struct {
		status, asns, ins, upd, del int
		etag, lm                    string
	}
	_ = admin.QueryRow(ctx, `SELECT status, asns, asn_inserted, asn_updated, asn_deleted, etag, last_modified
		FROM ripe_asnames_run WHERE uuid = $1`, v1).
		Scan(&gotRun.status, &gotRun.asns, &gotRun.ins, &gotRun.upd, &gotRun.del, &gotRun.etag, &gotRun.lm)
	if gotRun.status != 1 || gotRun.asns != 37 || gotRun.ins != 37 || gotRun.etag != `W/"e"` || gotRun.lm == "" {
		t.Errorf("ripe_asnames_run = %+v", gotRun)
	}

	// Índices de busca que a api-ripe-asnames vai usar.
	if n := count(t, admin, "SELECT count(*) FROM ripe_asnames_asn WHERE description ILIKE '%' || $1 || '%'", "côte"); n != 2 {
		t.Errorf("busca por trecho = %d, quero 2", n)
	}
	if n := count(t, admin, "SELECT count(*) FROM ripe_asnames_asn WHERE lower(handle) = lower($1)", "google"); n != 1 {
		t.Errorf("busca por handle = %d", n)
	}
	if n := count(t, admin, "SELECT count(*) FROM ripe_asnames_asn WHERE country = $1", "BR"); n != 3 {
		t.Errorf("ASNs do BR = %d", n)
	}

	// 2) A consolidação (fase 2) marca 1; o mesmo conteúdo não muda nada.
	if _, err := admin.Exec(ctx, "UPDATE jobs SET consolidated = 1"); err != nil {
		t.Fatal(err)
	}
	before := readASN(t, admin, 15169)
	ch, v2, err := st.Apply(ctx, sample(t), runFor("b", 37), opt)
	if err != nil {
		t.Fatal(err)
	}
	if ch.Total() != 0 || v2 == v1 {
		t.Errorf("reaplicação = %+v, versões %s/%s", ch, v1, v2)
	}
	if j := readJob(t, admin); j.consolidated != 1 {
		t.Errorf("sem mudança a flag deve continuar 1: %+v", j)
	}
	if after := readASN(t, admin, 15169); !after.updatedAt.Equal(before.updatedAt) {
		t.Error("reaplicação sem mudança não pode tocar updated_at")
	}

	// 3) Mudanças: descrição nova, ASN removido, ASN novo.
	ds := sample(t)
	var kept []parse.ASN
	for _, a := range ds.ASNs {
		switch a.Number {
		case 403009: // sai da fonte
			continue
		case 15169:
			a.Description = "GOOGLE - Google LLC (novo), US"
			a.Handle, a.Name, a.Country = parse.Derive(a.Description)
		}
		kept = append(kept, a)
	}
	kept = append(kept, parse.ASN{Number: 4200000000, Description: "PRIVADO Teste, ZZ", Handle: "PRIVADO", Name: "Teste", Country: "ZZ"})
	ds.ASNs = kept
	ch, _, err = st.Apply(ctx, ds, runFor("c", len(kept)), opt)
	if err != nil {
		t.Fatal(err)
	}
	if want := (Changes{ASNInserted: 1, ASNUpdated: 1, ASNDeleted: 1}); ch != want {
		t.Errorf("mudanças = %+v, quero %+v", ch, want)
	}
	if j := readJob(t, admin); j.consolidated != 0 {
		t.Errorf("com mudança a flag volta a 0: %+v", j)
	}
	after := readASN(t, admin, 15169)
	if str(after.name) != "Google LLC (novo)" || !after.createdAt.Equal(before.createdAt) || !after.updatedAt.After(before.updatedAt) {
		t.Errorf("AS15169 depois da mudança = %+v (antes %+v)", after, before)
	}
	if n := count(t, admin, "SELECT count(*) FROM ripe_asnames_asn WHERE asn = 403009"); n != 0 {
		t.Error("ASN que sumiu da fonte deveria ser apagado")
	}

	// 4) Trava de remoção em massa, e --force.
	small := &parse.Dataset{ASNs: ds.ASNs[:2]}
	_, _, err = st.Apply(ctx, small, runFor("d", 2), opt)
	var rem *RemovalError
	if !errors.As(err, &rem) || rem.Removed != 35 || rem.Current != 37 {
		t.Fatalf("esperava RemovalError de 35/37, veio %v", err)
	}
	if n := count(t, admin, "SELECT count(*) FROM ripe_asnames_asn"); n != 37 {
		t.Errorf("recusa não pode mexer na tabela: %d ASNs", n)
	}
	if n := count(t, admin, "SELECT count(*) FROM ripe_asnames_run"); n != 3 {
		t.Errorf("recusa dentro do Apply não grava versão: %d execuções", n)
	}
	ch, _, err = st.Apply(ctx, small, runFor("e", 2), ApplyOptions{RemovalThreshold: 0.05, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if ch != (Changes{ASNDeleted: 35}) {
		t.Errorf("com --force = %+v", ch)
	}

	// 5) Falhas ficam em ripe_asnames_run e não viram versão.
	fail := Run{StartedAt: time.Now(), URL: "https://example.test/asn.txt", HTTPStatus: 200,
		SHA256: strings.Repeat("f", 64), Bytes: 10, Warnings: []string{"linha 1: x"}}
	if err := st.RecordFailure(ctx, fail, errors.New("parser: boom")); err != nil {
		t.Fatal(err)
	}
	last, err := st.LastApplied(ctx)
	if err != nil || last == nil || last.SHA256 != strings.Repeat("e", 64) || last.ETag != `W/"e"` || last.URL != fail.URL {
		t.Errorf("LastApplied = %+v, err = %v", last, err)
	}
	if n := count(t, admin, `SELECT count(*) FROM ripe_asnames_run
		WHERE status = 0 AND error = 'parser: boom' AND asns IS NULL AND asn_inserted IS NULL
		  AND warnings = '["linha 1: x"]'::jsonb`); n != 1 {
		t.Errorf("falhas gravadas = %d", n)
	}

	// 6) Verificação sem mudança só toca last_check_at.
	jb := readJob(t, admin)
	if err := st.TouchCheck(ctx); err != nil {
		t.Fatal(err)
	}
	ja := readJob(t, admin)
	if !ja.lastCheck.After(*jb.lastCheck) || !ja.lastSync.Equal(*jb.lastSync) || ja.consolidated != jb.consolidated {
		t.Errorf("TouchCheck: antes %+v, depois %+v", jb, ja)
	}
}

// Primeira verificação sem mudança (banco já com dados de outra instância, por
// exemplo) cria a linha de jobs só com last_check_at.
func TestTouchCheckCreatesJob(t *testing.T) {
	st, admin := setup(t)
	if err := st.TouchCheck(context.Background()); err != nil {
		t.Fatal(err)
	}
	if j := readJob(t, admin); j.lastCheck == nil || j.lastSync != nil || j.consolidated != 0 {
		t.Errorf("jobs = %+v", j)
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
	if _, _, err := st.Apply(ctx, sample(t), runFor("a", 37), ApplyOptions{RemovalThreshold: 0.05}); !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v, quero ErrBusy", err)
	}
	if n := count(t, admin, "SELECT count(*) FROM ripe_asnames_run"); n != 0 {
		t.Errorf("concorrência não grava execução: %d", n)
	}
}

// TestApplyRealFile aplica o arquivo real inteiro, quando RIPE_ASNAMES_REAL_FILE
// aponta para uma cópia de https://ftp.ripe.net/ripe/asnames/asn.txt, e
// confere que a reaplicação é idempotente.
func TestApplyRealFile(t *testing.T) {
	path := os.Getenv("RIPE_ASNAMES_REAL_FILE")
	if path == "" {
		t.Skip("defina RIPE_ASNAMES_REAL_FILE para testar com o arquivo real")
	}
	st, admin := setup(t)
	ctx := context.Background()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ds, err := parse.Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	opt := ApplyOptions{RemovalThreshold: 0.05}

	start := time.Now()
	ch, _, err := st.Apply(ctx, ds, runFor("a", len(ds.ASNs)), opt)
	if err != nil {
		t.Fatal(err)
	}
	first := time.Since(start)
	if ch.ASNInserted != len(ds.ASNs) || ch.ASNUpdated != 0 || ch.ASNDeleted != 0 {
		t.Errorf("carga inicial = %+v", ch)
	}

	start = time.Now()
	ch, _, err = st.Apply(ctx, ds, runFor("b", len(ds.ASNs)), opt)
	if err != nil {
		t.Fatal(err)
	}
	second := time.Since(start)
	if ch.Total() != 0 {
		t.Errorf("reaplicação deveria ser idempotente: %+v", ch)
	}
	if n := count(t, admin, "SELECT count(*) FROM ripe_asnames_asn"); n != len(ds.ASNs) {
		t.Errorf("linhas = %d, quero %d", n, len(ds.ASNs))
	}

	if _, err := admin.Exec(ctx, "ANALYZE ripe_asnames_asn"); err != nil {
		t.Fatal(err)
	}
	start = time.Now()
	n := count(t, admin, "SELECT count(*) FROM ripe_asnames_asn WHERE description ILIKE '%' || $1 || '%'", "google")
	search := time.Since(start)
	if n == 0 || count(t, admin, "SELECT count(*) FROM ripe_asnames_asn WHERE asn = 15169 AND handle = 'GOOGLE'") != 1 {
		t.Errorf("busca por google = %d", n)
	}
	var plan strings.Builder
	rows, err := admin.Query(ctx, "EXPLAIN SELECT asn FROM ripe_asnames_asn WHERE description ILIKE '%google%'")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var line string
		_ = rows.Scan(&line)
		plan.WriteString(line + "\n")
	}
	rows.Close()
	if !strings.Contains(plan.String(), "ix_ripe_asnames_asn_description_trgm") {
		t.Errorf("a busca por trecho deveria usar o índice trigram:\n%s", plan.String())
	}
	t.Logf("%d ASNs: carga inicial %s, reaplicação sem mudança %s, busca ILIKE %s (%d linhas)",
		len(ds.ASNs), first, second, search, n)
}
