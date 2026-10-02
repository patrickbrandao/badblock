//go:build integration

package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/patrickbrandao/badblock/apps/rootzone/collector/internal/parse"
)

// Consultas que a api-rootzone usa (specs/fontes/rootzone/dados.md#consultas-da-api).
const (
	qVersion = `SELECT uuid::text, created_at, url, sha256, serial, soa_mname, soa_rname,
	       soa_refresh, soa_retry, soa_expire, soa_minimum, tlds, records, rrsigs
	  FROM rootzone_run WHERE status = 1 ORDER BY created_at DESC LIMIT 1`
	qTLDs = `SELECT tld, tld_unicode, nameservers, nameservers_ipv4, nameservers_ipv6, ds_records
	  FROM rootzone_tld ORDER BY tld`
	qTLD = `SELECT tld, tld_unicode, nameservers, nameservers_ipv4, nameservers_ipv6, ds_records, created_at, updated_at
	  FROM rootzone_tld WHERE tld = $1`
	qDelegation = `SELECT type, rdata, ttl FROM rootzone_record
	 WHERE owner = $1 AND type IN ('NS', 'DS') ORDER BY type, rdata`
	qGlue = `SELECT g.owner, g.type, g.rdata, g.ttl
	  FROM rootzone_record ns
	  JOIN rootzone_record g ON g.owner = ns.rdata AND g.type IN ('A', 'AAAA')
	 WHERE ns.owner = $1 AND ns.type = 'NS'
	 ORDER BY g.owner, g.type, g.rdata`
	qApex      = `SELECT type, rdata, ttl FROM rootzone_record WHERE owner = '.' ORDER BY type, rdata`
	qServedBy  = `SELECT owner FROM rootzone_record WHERE type = 'NS' AND rdata = $1 ORDER BY owner`
	qGlueOwner = `SELECT owner, type FROM rootzone_record WHERE type IN ('A', 'AAAA') AND rdata = $1 ORDER BY owner`
)

// setup sobe um PG18, aplica as migrations de central/ e rootzone/ (seção
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

	for _, dir := range []string{"central", "rootzone"} {
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

	st, err := Open(ctx, superURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	return st, admin
}

func sampleText(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../testdata/root-zone-sample.zone")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func parseText(t *testing.T, s string) *parse.Dataset {
	t.Helper()
	ds, err := parse.Parse(strings.NewReader(s))
	if err != nil {
		t.Fatal(err)
	}
	return ds
}

func runFor(sha string, ds *parse.Dataset) Run {
	return Run{StartedAt: time.Now(), URL: "https://example.test/root.zone", HTTPStatus: 200,
		ETag: `"225757-65ca377d2eb00-gzip"`, LastModified: "Tue, 29 Sep 2026 18:37:00 GMT",
		MD5: strings.Repeat(sha, 32), SHA256: strings.Repeat(sha, 64), Bytes: 100,
		Parsed: true, SOA: ds.SOA, TLDs: len(ds.TLDs), Records: len(ds.Records), RRSIGs: ds.RRSIGs, TypeCounts: ds.TypeCounts}
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
		t.Fatal(err)
	}
	return n
}

// lines roda uma consulta e junta cada linha em "c1 c2 ...".
func lines(t *testing.T, admin *pgxpool.Pool, sql string, args ...any) []string {
	t.Helper()
	rows, err := admin.Query(context.Background(), sql, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			t.Fatal(err)
		}
		parts := make([]string, len(vals))
		for i, v := range vals {
			switch x := v.(type) {
			case string:
				parts[i] = x
			default:
				parts[i] = strings.TrimSpace(strings.ReplaceAll(fmt.Sprint(x), "\n", " "))
			}
		}
		out = append(out, strings.Join(parts, " "))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
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
	ds := parseText(t, sampleText(t))
	ch, v1, err := st.Apply(ctx, ds, runFor("a", ds), opt)
	if err != nil {
		t.Fatal(err)
	}
	if ch != (Changes{TLDInserted: 7, RecordInserted: 195}) {
		t.Errorf("carga inicial = %+v", ch)
	}
	if j := readJob(t, admin); j.lastSync == nil || j.lastCheck == nil || j.consolidated != 0 {
		t.Errorf("jobs depois da carga = %+v", j)
	}
	last, err := st.LastApplied(ctx)
	if err != nil || last == nil || last.Version != v1 || last.Serial != 2026092901 || last.MD5 != strings.Repeat("a", 32) ||
		last.ETag != `"225757-65ca377d2eb00-gzip"` || last.LastModified == "" || last.URL != "https://example.test/root.zone" {
		t.Errorf("LastApplied = %+v, err = %v", last, err)
	}
	var tc string
	if err := admin.QueryRow(ctx, `SELECT type_counts::text FROM rootzone_run WHERE uuid = $1`, v1).Scan(&tc); err != nil ||
		tc != `{"A": 59, "DS": 6, "NS": 61, "SOA": 1, "AAAA": 55, "NSEC": 8, "RRSIG": 17, "DNSKEY": 4, "ZONEMD": 1}` {
		t.Errorf("type_counts = %s, %v", tc, err)
	}
	if n := count(t, admin, "SELECT count(*) FROM rootzone_record WHERE type = 'RRSIG'"); n != 0 {
		t.Errorf("RRSIG não é guardado: %d", n)
	}

	// Consultas da api-rootzone.
	if got := lines(t, admin, qVersion)[0]; !strings.Contains(got, " 2026092901 a.root-servers.net nstld.verisign-grs.com 1800 900 604800 86400 7 195 17") ||
		!strings.HasPrefix(got, v1) {
		t.Errorf("versão = %s", got)
	}
	if got := strings.Join(lines(t, admin, qTLDs), "|"); got !=
		"aaa aaa 6 6 6 1|bo bo 4 4 3 0|br br 6 6 6 1|com com 13 13 13 1|top top 8 6 3 2|xn--p1ai рф 6 6 6 1|zw zw 5 5 5 0" {
		t.Errorf("TLDs = %s", got)
	}
	if got := lines(t, admin, qTLD, "xn--p1ai"); len(got) != 1 || !strings.HasPrefix(got[0], "xn--p1ai рф 6 6 6 1 ") {
		t.Errorf("TLD xn--p1ai = %v", got)
	}
	if got := strings.Join(lines(t, admin, qDelegation, "br"), "|"); got !=
		"DS 38298 13 2 9F2D4993F47B0F2751DE0007D70A2754EE532FE373761154D9EA7A8CB9D8EA18 86400|"+
			"NS a.dns.br 172800|NS b.dns.br 172800|NS c.dns.br 172800|NS d.dns.br 172800|NS e.dns.br 172800|NS f.dns.br 172800" {
		t.Errorf("delegação do br = %s", got)
	}
	glue := lines(t, admin, qGlue, "br")
	if len(glue) != 12 || glue[0] != "a.dns.br A 200.219.148.10 172800" || glue[1] != "a.dns.br AAAA 2001:12f8:6::10 172800" {
		t.Errorf("glue do br = %v", glue)
	}
	if glue := lines(t, admin, qGlue, "top"); len(glue) != 9 {
		t.Errorf("glue do top (6 A + 3 AAAA) = %v", glue)
	}
	apex := lines(t, admin, qApex)
	if len(apex) != 20 || !strings.HasPrefix(apex[0], "DNSKEY 256 3 8 ") || apex[len(apex)-1] != "ZONEMD 2026092901 1 1 E80BFF012C499FB7532E91A1926E336362AE85DFD6715DB7390175BC8A800F10AE18369000A47566523182623B0FFF99 86400" {
		t.Errorf("ápice = %d linhas: %v", len(apex), apex)
	}
	if got := lines(t, admin, qServedBy, "ns.dns.br"); strings.Join(got, ",") != "bo" {
		t.Errorf("TLDs servidos por ns.dns.br = %v", got)
	}
	if got := lines(t, admin, qGlueOwner, "2001:12f8:6::10"); strings.Join(got, ",") != "a.dns.br AAAA" {
		t.Errorf("dono do endereço = %v", got)
	}

	// 2) A consolidação (fase 2) marca 1; o mesmo conteúdo não muda nada.
	if _, err := admin.Exec(ctx, "UPDATE jobs SET consolidated = 1"); err != nil {
		t.Fatal(err)
	}
	var before time.Time
	_ = admin.QueryRow(ctx, "SELECT updated_at FROM rootzone_tld WHERE tld = 'br'").Scan(&before)
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

	// 3) Versão nova: serial novo (SOA e ZONEMD trocam), zw sai da zona, o DS
	// do br muda de TTL e o br ganha um servidor sem glue.
	var b strings.Builder
	for line := range strings.Lines(strings.ReplaceAll(sampleText(t), "2026092901", "2026092902")) {
		switch {
		case strings.HasPrefix(line, "zw.\t"):
			continue
		case strings.HasPrefix(line, "br.\t\t\t86400\tIN\tDS\t"):
			line = strings.Replace(line, "86400", "3600", 1)
		}
		b.WriteString(line)
	}
	b.WriteString("br.\t172800\tIN\tNS\tns.example.net.\n")
	ds3 := parseText(t, b.String())
	ch, v3, err := st.Apply(ctx, ds3, runFor("c", ds3), opt)
	if err != nil {
		t.Fatal(err)
	}
	// Registros: SOA e ZONEMD novos + NS novo; DS com TTL novo; SOA e ZONEMD
	// antigos, 5 NS e o NSEC do zw removidos. TLDs: br com 7 NS; zw removido.
	if want := (Changes{TLDUpdated: 1, TLDDeleted: 1, RecordInserted: 3, RecordUpdated: 1, RecordDeleted: 8}); ch != want {
		t.Errorf("mudanças = %+v, quero %+v", ch, want)
	}
	if j := readJob(t, admin); j.consolidated != 0 {
		t.Errorf("com mudança a flag volta a 0: %+v", j)
	}
	var after, created time.Time
	var ns, v4 int
	_ = admin.QueryRow(ctx, "SELECT updated_at, created_at, nameservers, nameservers_ipv4 FROM rootzone_tld WHERE tld = 'br'").
		Scan(&after, &created, &ns, &v4)
	if ns != 7 || v4 != 6 || !after.After(before) || !created.Before(after) {
		t.Errorf("br depois da mudança: ns %d, v4 %d, updated %v (antes %v)", ns, v4, after, before)
	}
	if n := count(t, admin, "SELECT count(*) FROM rootzone_record WHERE owner = 'zw'") + count(t, admin, "SELECT count(*) FROM rootzone_tld WHERE tld = 'zw'"); n != 0 {
		t.Error("zw sumiu da zona e deveria ser apagado")
	}
	if got := lines(t, admin, qVersion)[0]; !strings.HasPrefix(got, v3) || !strings.Contains(got, " 2026092902 ") {
		t.Errorf("versão = %s", got)
	}

	// 4) Trava de remoção em massa (sobre rootzone_record), e --force.
	var small strings.Builder
	for line := range strings.Lines(sampleText(t)) {
		if strings.HasPrefix(line, ".\t") || strings.Contains(line, "root-servers.net.\t") || strings.HasPrefix(line, "aaa.\t") {
			small.WriteString(line)
		}
	}
	dsSmall := parseText(t, small.String())
	cur := count(t, admin, "SELECT count(*) FROM rootzone_record")
	_, _, err = st.Apply(ctx, dsSmall, runFor("d", dsSmall), opt)
	var rem *RemovalError
	if !errors.As(err, &rem) || rem.Current != cur || rem.Removed < cur/2 {
		t.Fatalf("esperava RemovalError, veio %v", err)
	}
	if n := count(t, admin, "SELECT count(*) FROM rootzone_record"); n != cur {
		t.Errorf("recusa não pode mexer na tabela: %d registros", n)
	}
	if n := count(t, admin, "SELECT count(*) FROM rootzone_run"); n != 3 {
		t.Errorf("recusa dentro do Apply não grava versão: %d execuções", n)
	}
	ch, _, err = st.Apply(ctx, dsSmall, runFor("e", dsSmall), ApplyOptions{RemovalThreshold: 0.05, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if ch.RecordDeleted != rem.Removed || ch.TLDDeleted != 5 || count(t, admin, "SELECT count(*) FROM rootzone_tld") != 1 {
		t.Errorf("com --force = %+v", ch)
	}

	// 5) Falhas ficam em rootzone_run e não viram versão.
	fail := Run{StartedAt: time.Now(), URL: "https://example.test/root.zone", HTTPStatus: 200,
		MD5: strings.Repeat("f", 32), SHA256: strings.Repeat("f", 64), Bytes: 10, Warnings: []string{"linha 1: x"}}
	if err := st.RecordFailure(ctx, fail, errors.New("parser: boom")); err != nil {
		t.Fatal(err)
	}
	last, err = st.LastApplied(ctx)
	if err != nil || last == nil || last.SHA256 != strings.Repeat("e", 64) || last.Serial != 2026092901 {
		t.Errorf("LastApplied = %+v, err = %v", last, err)
	}
	if n := count(t, admin, `SELECT count(*) FROM rootzone_run
		WHERE status = 0 AND error = 'parser: boom' AND serial IS NULL AND soa_mname IS NULL AND tlds IS NULL
		  AND type_counts IS NULL AND record_inserted IS NULL AND tld_inserted IS NULL
		  AND warnings = '["linha 1: x"]'::jsonb`); n != 1 {
		t.Errorf("falhas gravadas = %d", n)
	}
	// Recusa depois do parser (mínimo de TLDs): SOA e contagens gravados.
	failParsed := runFor("9", dsSmall)
	if err := st.RecordFailure(ctx, failParsed, errors.New("só 1 TLDs na zona (mínimo 1000): arquivo truncado?")); err != nil {
		t.Fatal(err)
	}
	if n := count(t, admin, `SELECT count(*) FROM rootzone_run
		WHERE status = 0 AND serial = 2026092901 AND tlds = 1 AND record_inserted IS NULL`); n != 1 {
		t.Errorf("recusa depois do parser = %d", n)
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

// Primeira verificação sem mudança cria a linha de jobs só com last_check_at.
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
	ds := parseText(t, sampleText(t))
	if _, _, err := st.Apply(ctx, ds, runFor("a", ds), ApplyOptions{RemovalThreshold: 0.05}); !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v, quero ErrBusy", err)
	}
	if n := count(t, admin, "SELECT count(*) FROM rootzone_run"); n != 0 {
		t.Errorf("concorrência não grava execução: %d", n)
	}
}

// TestApplyRealFile aplica o root.zone inteiro do dia, quando
// ROOTZONE_REAL_FILE aponta para ele (make test-real): carga completa,
// reaplicação idempotente, as consultas da API com os planos, e os tempos.
func TestApplyRealFile(t *testing.T) {
	path := os.Getenv("ROOTZONE_REAL_FILE")
	if path == "" {
		t.Skip("defina ROOTZONE_REAL_FILE com o caminho do root.zone")
	}
	st, admin := setup(t)
	ctx := context.Background()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	ds := parseText(t, string(raw))
	t.Logf("parser: %v (%d registros, %d TLDs)", time.Since(start), len(ds.Records), len(ds.TLDs))
	opt := ApplyOptions{RemovalThreshold: 0.05}

	start = time.Now()
	ch, _, err := st.Apply(ctx, ds, runFor("a", ds), opt)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("carga inicial: %v, %+v", time.Since(start), ch)
	if ch.RecordInserted != len(ds.Records) || ch.TLDInserted != len(ds.TLDs) || ch.Total() != len(ds.Records)+len(ds.TLDs) {
		t.Errorf("carga inicial = %+v", ch)
	}

	start = time.Now()
	ch, _, err = st.Apply(ctx, ds, runFor("b", ds), opt)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("reaplicação sem mudança: %v, %+v", time.Since(start), ch)
	if ch.Total() != 0 {
		t.Errorf("reaplicação deveria ser idempotente: %+v", ch)
	}
	if n := count(t, admin, "SELECT count(*) FROM rootzone_record"); n != len(ds.Records) {
		t.Errorf("rootzone_record = %d, quero %d", n, len(ds.Records))
	}
	if _, err := admin.Exec(ctx, "ANALYZE rootzone_record; ANALYZE rootzone_tld"); err != nil {
		t.Fatal(err)
	}

	queries := []struct {
		name, sql string
		args      []any
		index     string
	}{
		{"lista de TLDs", qTLDs, nil, ""},
		{"um TLD", qTLD, []any{"br"}, "uq_rootzone_tld_tld"},
		{"NS e DS do br", qDelegation, []any{"br"}, "uq_rootzone_record_owner_type_rdata"},
		{"glue do br", qGlue, []any{"br"}, "uq_rootzone_record_owner_type_rdata"},
		{"glue do com", qGlue, []any{"com"}, "uq_rootzone_record_owner_type_rdata"},
		{"ápice", qApex, nil, "uq_rootzone_record_owner_type_rdata"},
		{"TLDs de um servidor", qServedBy, []any{"a.gtld-servers.net"}, "ix_rootzone_record_type_rdata"},
		{"dono de um endereço", qGlueOwner, []any{"200.219.148.10"}, "ix_rootzone_record_type_rdata"},
	}
	for _, q := range queries {
		start := time.Now()
		n := len(lines(t, admin, q.sql, q.args...))
		elapsed := time.Since(start)
		plan := strings.Join(lines(t, admin, "EXPLAIN "+q.sql, q.args...), "\n")
		if q.index != "" && !strings.Contains(plan, q.index) {
			t.Errorf("%s deveria usar %s:\n%s", q.name, q.index, plan)
		}
		t.Logf("%s: %d linhas em %v", q.name, n, elapsed)
	}
	if got := lines(t, admin, qServedBy, "a.gtld-servers.net"); len(got) < 2 {
		t.Errorf("a.gtld-servers.net serve com e net: %v", got)
	}
}
