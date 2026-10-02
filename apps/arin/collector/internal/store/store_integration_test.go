//go:build integration

package store

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/patrickbrandao/badblock/apps/arin/collector/internal/parse"
	"github.com/patrickbrandao/badblock/apps/arin/collector/internal/rir"
)

// setup sobe um PG18, aplica as migrations de central/ e da fonte (seção
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

	st, err := Open(ctx, superURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	return st, admin
}

func parseFile(t *testing.T, path string) *parse.Dataset {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ds, err := parse.Parse(f, rir.Registry)
	if err != nil {
		t.Fatal(err)
	}
	return ds
}

// holder é um titular da fixture (opaque-id da ARIN, hash hex de 32) com um
// ASN, dois blocos IPv4 e um IPv6.
const holder = "e0082a77a634f2cc5817ffb7de12b38e"

func sample(t *testing.T) *parse.Dataset {
	return parseFile(t, "../../testdata/delegated-extended-sample.txt")
}

// runFor monta a execução de um arquivo cujos hashes são c repetido.
func runFor(c string, ds *parse.Dataset) Run {
	return Run{StartedAt: time.Now(), URL: "https://example.test/delegated", HTTPStatus: 200,
		ETag: `"e"`, MD5: strings.Repeat(c, 32), SHA256: strings.Repeat(c, 64), Bytes: 100,
		Parsed: true, Header: ds.Header, ASNRecords: ds.ASNRecords, IPv4Records: ds.IPv4Records,
		IPv6Records: ds.IPv6Records, PrefixesV4: ds.PrefixesV4, PrefixesV6: ds.PrefixesV6, Warnings: ds.Warnings}
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
	if ch != (Changes{ASNInserted: 7, PrefixInserted: 16}) {
		t.Errorf("carga inicial = %+v", ch)
	}
	if j := readJob(t, admin); j.lastSync == nil || j.lastCheck == nil || j.consolidated != 0 {
		t.Errorf("jobs depois da carga = %+v", j)
	}
	if v4, v6 := count(t, admin, "SELECT count(*) FROM arin_prefix WHERE family = 4"),
		count(t, admin, "SELECT count(*) FROM arin_prefix WHERE family = 6"); v4 != 10 || v6 != 6 {
		t.Errorf("família = %d/%d", v4, v6)
	}
	last, err := st.LastApplied(ctx)
	if err != nil || last == nil || last.Version != v1 || last.Serial != "1790600421096" ||
		!last.EndDate.Equal(time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)) || last.MD5 != strings.Repeat("a", 32) {
		t.Errorf("LastApplied = %+v, err = %v", last, err)
	}
	var fv, utc string
	var hdrRecords, asnRecords, pv4 int
	_ = admin.QueryRow(ctx, `SELECT format_version, utc_offset, header_records, asn_records, prefixes_v4
		FROM arin_run WHERE uuid = $1`, v1).Scan(&fv, &utc, &hdrRecords, &asnRecords, &pv4)
	if fv != "2.3" || utc != "-0400" || hdrRecords != 21 || asnRecords != 7 || pv4 != 10 {
		t.Errorf("arin_run = %s %s %d %d %d", fv, utc, hdrRecords, asnRecords, pv4)
	}

	// As consultas que a api-arin vai fazer.
	var start, end int64
	var status string
	err = admin.QueryRow(ctx, `SELECT asn_start, asn_end, status FROM arin_asn
		WHERE asn_start <= $1 ORDER BY asn_start DESC LIMIT 1`, 404000).Scan(&start, &end, &status)
	if err != nil || start != 403010 || end != 404380 || status != "available" {
		t.Errorf("faixa do AS404000 = %d-%d %s, err = %v", start, end, status, err)
	}
	var pfx netip.Prefix
	var opaque string
	err = admin.QueryRow(ctx, `SELECT prefix, opaque_id FROM arin_prefix
		WHERE prefix >>= $1::inet ORDER BY masklen(prefix) DESC LIMIT 1`, "208.65.33.10").Scan(&pfx, &opaque)
	if err != nil || pfx.String() != "208.65.32.0/22" || opaque != holder {
		t.Errorf("bloco do IP = %s %s, err = %v", pfx, opaque, err)
	}
	if n := count(t, admin, `SELECT (SELECT count(*) FROM arin_asn WHERE opaque_id = $1)
		+ (SELECT count(*) FROM arin_prefix WHERE opaque_id = $1)`, holder); n != 4 {
		t.Errorf("recursos do titular %s = %d, quero 4", holder, n)
	}
	if n := count(t, admin, `SELECT count(*) FROM arin_prefix
		WHERE status = 'reserved' AND cc IS NULL AND reg_date IS NULL AND opaque_id IS NULL`); n != 6 {
		t.Errorf("reservados com NULLs = %d", n)
	}
	if n := count(t, admin, `SELECT count(*) FROM arin_prefix
		WHERE record_start = '23.128.1.0' AND record_value = 768 AND prefix IN ('23.128.1.0/24', '23.128.2.0/23')`); n != 2 {
		t.Errorf("registro não-CIDR da fixture = %d blocos", n)
	}

	// 2) A consolidação (fase 2) marca 1; o mesmo conteúdo não muda nada.
	if _, err := admin.Exec(ctx, "UPDATE jobs SET consolidated = 1"); err != nil {
		t.Fatal(err)
	}
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

	// 3) Mudanças: ASN designado, ASN removido, ASN novo; bloco com titular novo,
	// bloco removido e um registro IPv4 que não forma CIDR (vira 3 blocos).
	ds = sample(t)
	var asns []parse.ASN
	for _, a := range ds.ASNs {
		switch a.Start {
		case 212: // sai da fonte
			continue
		case 403010: // available → assigned
			a.Status, a.CC, a.OpaqueID = "assigned", "US", "999"
		}
		asns = append(asns, a)
	}
	ds.ASNs = append(asns, parse.ASN{Start: 64500, Count: 1, CC: "US", Status: "assigned", OpaqueID: "999"})
	var pfxs []parse.Prefix
	for _, p := range ds.Prefixes {
		switch p.Prefix.String() {
		case "6.0.0.0/8": // sai da fonte
			continue
		case "23.128.16.0/24":
			p.OpaqueID = "999"
		}
		pfxs = append(pfxs, p)
	}
	startAddr := netip.MustParseAddr("23.128.5.0")
	split, _ := parse.SplitIPv4(startAddr, 1792)
	for _, p := range split {
		pfxs = append(pfxs, parse.Prefix{Prefix: p, Status: "reserved", RecordStart: startAddr, RecordValue: 1792})
	}
	ds.Prefixes = pfxs
	ch, _, err = st.Apply(ctx, ds, runFor("c", ds), ApplyOptions{RemovalThreshold: 0.2})
	if err != nil {
		t.Fatal(err)
	}
	want := Changes{ASNInserted: 1, ASNUpdated: 1, ASNDeleted: 1, PrefixInserted: 3, PrefixUpdated: 1, PrefixDeleted: 1}
	if ch != want {
		t.Errorf("mudanças = %+v, quero %+v", ch, want)
	}
	if j := readJob(t, admin); j.consolidated != 0 {
		t.Errorf("com mudança a flag volta a 0: %+v", j)
	}
	if n := count(t, admin, `SELECT count(*) FROM arin_prefix
		WHERE record_start = '23.128.5.0' AND record_value = 1792
		  AND prefix IN ('23.128.5.0/24', '23.128.6.0/23', '23.128.8.0/22')`); n != 3 {
		t.Errorf("registro dividido = %d blocos", n)
	}
	if n := count(t, admin, "SELECT count(*) FROM arin_asn WHERE updated_at > created_at AND asn_start = 403010"); n != 1 {
		t.Errorf("updated_at não mudou no ASN alterado")
	}

	// 4) Trava de remoção em massa, e --force.
	small := &parse.Dataset{ASNs: ds.ASNs[:2], Prefixes: ds.Prefixes[:2]}
	_, _, err = st.Apply(ctx, small, runFor("d", ds), opt)
	if _, ok := errors.AsType[*RemovalError](err); !ok {
		t.Fatalf("esperava RemovalError, veio %v", err)
	}
	if n := count(t, admin, "SELECT count(*) FROM arin_asn"); n != 7 {
		t.Errorf("recusa não pode mexer nas tabelas: %d ASNs", n)
	}
	ch, _, err = st.Apply(ctx, small, runFor("e", ds), ApplyOptions{RemovalThreshold: 0.05, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if ch.ASNDeleted != 5 || ch.PrefixDeleted != 16 {
		t.Errorf("com --force = %+v", ch)
	}

	// 5) Falhas ficam em arin_run e não viram versão.
	if err := st.RecordFailure(ctx, Run{StartedAt: time.Now(), URL: "https://example.test/delegated"}, errors.New("boom")); err != nil {
		t.Fatal(err)
	}
	last, err = st.LastApplied(ctx)
	if err != nil || last == nil || last.MD5 != strings.Repeat("e", 32) || last.ETag != `"e"` {
		t.Errorf("LastApplied = %+v, err = %v", last, err)
	}
	if n := count(t, admin, `SELECT count(*) FROM arin_run
		WHERE status = 0 AND error = 'boom' AND serial IS NULL AND asn_records IS NULL AND asn_inserted IS NULL`); n != 1 {
		t.Errorf("falhas gravadas = %d", n)
	}

	// 6) Verificação sem mudança só toca last_check_at.
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
	if n := count(t, admin, "SELECT count(*) FROM arin_run"); n != 0 {
		t.Errorf("concorrência não grava execução: %d", n)
	}
}

// TestApplyRealFile aplica o arquivo real inteiro do RIR, quando
// ARIN_REAL_FILE aponta para ele (pulado se ausente): carga completa e
// reaplicação idempotente, com os tempos no log.
func TestApplyRealFile(t *testing.T) {
	path := os.Getenv("ARIN_REAL_FILE")
	if path == "" {
		t.Skip("defina ARIN_REAL_FILE com o caminho do arquivo real")
	}
	st, admin := setup(t)
	ctx := context.Background()
	opt := ApplyOptions{RemovalThreshold: 0.05}

	start := time.Now()
	ds := parseFile(t, path)
	t.Logf("parser: %v", time.Since(start))

	start = time.Now()
	ch, _, err := st.Apply(ctx, ds, runFor("a", ds), opt)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("primeira carga: %v, %+v", time.Since(start), ch)
	if ch.ASNInserted != len(ds.ASNs) || ch.PrefixInserted != len(ds.Prefixes) || ch.ASNUpdated+ch.ASNDeleted+ch.PrefixUpdated+ch.PrefixDeleted != 0 {
		t.Errorf("primeira carga = %+v, dataset %d/%d", ch, len(ds.ASNs), len(ds.Prefixes))
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

	asns, pfx := count(t, admin, "SELECT count(*) FROM arin_asn"), count(t, admin, "SELECT count(*) FROM arin_prefix")
	if asns != len(ds.ASNs) || pfx != len(ds.Prefixes) {
		t.Errorf("tabelas = %d/%d, dataset %d/%d", asns, pfx, len(ds.ASNs), len(ds.Prefixes))
	}
	t.Logf("tabelas: %d registros de ASN, %d blocos", asns, pfx)

	// Sobreposição de faixas de ASN (a api-arin supõe faixas disjuntas).
	if n := count(t, admin, `SELECT count(*) FROM (
		SELECT asn_start, lag(asn_end) OVER (ORDER BY asn_start) AS prev_end FROM arin_asn) x
		WHERE asn_start <= prev_end`); n != 0 {
		t.Errorf("%d faixas de ASN sobrepostas", n)
	}
	if _, err := admin.Exec(ctx, "ANALYZE arin_asn; ANALYZE arin_prefix"); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"EXPLAIN SELECT * FROM arin_asn WHERE asn_start <= 11472 ORDER BY asn_start DESC LIMIT 1",
		"EXPLAIN SELECT * FROM arin_prefix WHERE prefix >>= '208.65.33.10'::inet ORDER BY masklen(prefix) DESC LIMIT 1",
		"EXPLAIN SELECT * FROM arin_prefix WHERE opaque_id = '" + holder + "'",
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
		if !strings.Contains(joined, "Index") {
			t.Errorf("sem índice: %s → %s", q, joined)
		}
		t.Logf("%s → %s", q, joined)
	}
}
