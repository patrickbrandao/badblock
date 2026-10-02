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

	"github.com/patrickbrandao/badblock/apps/afrinic/collector/internal/parse"
	"github.com/patrickbrandao/badblock/apps/afrinic/collector/internal/rir"
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
	if ch != (Changes{ASNInserted: 5, PrefixInserted: 13}) {
		t.Errorf("carga inicial = %+v", ch)
	}
	if j := readJob(t, admin); j.lastSync == nil || j.lastCheck == nil || j.consolidated != 0 {
		t.Errorf("jobs depois da carga = %+v", j)
	}
	if v4, v6 := count(t, admin, "SELECT count(*) FROM afrinic_prefix WHERE family = 4"),
		count(t, admin, "SELECT count(*) FROM afrinic_prefix WHERE family = 6"); v4 != 9 || v6 != 4 {
		t.Errorf("família = %d/%d", v4, v6)
	}
	last, err := st.LastApplied(ctx)
	if err != nil || last == nil || last.Version != v1 || last.Serial != "20260928" ||
		!last.EndDate.Equal(time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)) || last.MD5 != strings.Repeat("a", 32) {
		t.Errorf("LastApplied = %+v, err = %v", last, err)
	}
	var fv, utc string
	var startDate *time.Time
	var hdrRecords, asnRecords, pv4 int
	_ = admin.QueryRow(ctx, `SELECT format_version, utc_offset, start_date, header_records, asn_records, prefixes_v4
		FROM afrinic_run WHERE uuid = $1`, v1).Scan(&fv, &utc, &startDate, &hdrRecords, &asnRecords, &pv4)
	if fv != "2" || utc != "00000" || startDate != nil || hdrRecords != 15 || asnRecords != 5 || pv4 != 9 {
		t.Errorf("afrinic_run = %s %s %v %d %d %d", fv, utc, startDate, hdrRecords, asnRecords, pv4)
	}

	// As consultas que a api-afrinic vai fazer.
	var start, end int64
	var status string
	err = admin.QueryRow(ctx, `SELECT asn_start, asn_end, status FROM afrinic_asn
		WHERE asn_start <= $1 ORDER BY asn_start DESC LIMIT 1`, 329814).Scan(&start, &end, &status)
	if err != nil || start != 329814 || end != 329814 || status != "allocated" {
		t.Errorf("registro do AS329814 = %d-%d %s, err = %v", start, end, status, err)
	}
	// ASN fora do arquivo: o registro de maior início (AS8770) termina antes.
	err = admin.QueryRow(ctx, `SELECT asn_start, asn_end FROM afrinic_asn
		WHERE asn_start <= $1 ORDER BY asn_start DESC LIMIT 1`, 10000).Scan(&start, &end)
	if err != nil || start != 8770 || end >= 10000 {
		t.Errorf("AS10000 = %d-%d, err = %v", start, end, err)
	}
	var pfx netip.Prefix
	var opaque string
	err = admin.QueryRow(ctx, `SELECT prefix, opaque_id FROM afrinic_prefix
		WHERE prefix >>= $1::inet ORDER BY masklen(prefix) DESC LIMIT 1`, "102.201.37.10").Scan(&pfx, &opaque)
	if err != nil || pfx.String() != "102.201.36.0/22" || opaque != "F3626E7D" {
		t.Errorf("bloco do IP = %s %s, err = %v", pfx, opaque, err)
	}
	if n := count(t, admin, `SELECT (SELECT count(*) FROM afrinic_asn WHERE opaque_id = $1)
		+ (SELECT count(*) FROM afrinic_prefix WHERE opaque_id = $1)`, "F3626E7D"); n != 3 {
		t.Errorf("recursos do titular F3626E7D = %d, quero 3", n)
	}
	// A AFRINIC publica reserved com cc ZZ (fica ZZ), data e opaque-id vazios.
	if n := count(t, admin, `SELECT count(*) FROM afrinic_prefix
		WHERE status = 'reserved' AND cc = 'ZZ' AND reg_date IS NULL AND opaque_id IS NULL`); n != 2 {
		t.Errorf("reservados com ZZ e NULLs = %d", n)
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

	// 3) Mudanças: ASN alocado, ASN removido, ASN novo; bloco com titular novo,
	// bloco removido e um registro IPv4 que não forma CIDR (vira 2 blocos).
	ds = sample(t)
	var asns []parse.ASN
	for _, a := range ds.ASNs {
		switch a.Start {
		case 8770: // sai da fonte
			continue
		case 10803: // reserved → allocated
			a.Status, a.CC, a.OpaqueID = "allocated", "ZA", "F3699999"
		}
		asns = append(asns, a)
	}
	ds.ASNs = append(asns, parse.ASN{Start: 329999, Count: 1, CC: "ZA", Status: "allocated", OpaqueID: "F3699999"})
	var pfxs []parse.Prefix
	for _, p := range ds.Prefixes {
		switch p.Prefix.String() {
		case "160.115.0.0/16": // sai da fonte
			continue
		case "102.201.36.0/22":
			p.OpaqueID = "F3699999"
		}
		pfxs = append(pfxs, p)
	}
	startAddr := netip.MustParseAddr("196.1.87.0") // registro real da AFRINIC
	split, _ := parse.SplitIPv4(startAddr, 1280)
	for _, p := range split {
		pfxs = append(pfxs, parse.Prefix{Prefix: p, CC: "ZA", Status: "assigned", OpaqueID: "F369C3AE",
			RecordStart: startAddr, RecordValue: 1280})
	}
	ds.Prefixes = pfxs
	ch, _, err = st.Apply(ctx, ds, runFor("c", ds), ApplyOptions{RemovalThreshold: 0.2})
	if err != nil {
		t.Fatal(err)
	}
	want := Changes{ASNInserted: 1, ASNUpdated: 1, ASNDeleted: 1, PrefixInserted: 2, PrefixUpdated: 1, PrefixDeleted: 1}
	if ch != want {
		t.Errorf("mudanças = %+v, quero %+v", ch, want)
	}
	if j := readJob(t, admin); j.consolidated != 0 {
		t.Errorf("com mudança a flag volta a 0: %+v", j)
	}
	if n := count(t, admin, `SELECT count(*) FROM afrinic_prefix
		WHERE record_start = '196.1.87.0' AND record_value = 1280 AND prefix IN ('196.1.87.0/24', '196.1.88.0/22')`); n != 2 {
		t.Errorf("registro dividido = %d blocos", n)
	}
	if n := count(t, admin, "SELECT count(*) FROM afrinic_asn WHERE updated_at > created_at AND asn_start = 10803"); n != 1 {
		t.Errorf("updated_at não mudou no ASN alterado")
	}

	// 4) Trava de remoção em massa, e --force.
	small := &parse.Dataset{ASNs: ds.ASNs[:2], Prefixes: ds.Prefixes[:2]}
	_, _, err = st.Apply(ctx, small, runFor("d", ds), opt)
	if _, ok := errors.AsType[*RemovalError](err); !ok {
		t.Fatalf("esperava RemovalError, veio %v", err)
	}
	if n := count(t, admin, "SELECT count(*) FROM afrinic_asn"); n != 5 {
		t.Errorf("recusa não pode mexer nas tabelas: %d ASNs", n)
	}
	ch, _, err = st.Apply(ctx, small, runFor("e", ds), ApplyOptions{RemovalThreshold: 0.05, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if ch.ASNDeleted != 3 || ch.PrefixDeleted != 12 {
		t.Errorf("com --force = %+v", ch)
	}

	// 5) Falhas ficam em afrinic_run e não viram versão.
	if err := st.RecordFailure(ctx, Run{StartedAt: time.Now(), URL: "https://example.test/delegated"}, errors.New("boom")); err != nil {
		t.Fatal(err)
	}
	last, err = st.LastApplied(ctx)
	if err != nil || last == nil || last.MD5 != strings.Repeat("e", 32) || last.ETag != `"e"` {
		t.Errorf("LastApplied = %+v, err = %v", last, err)
	}
	if n := count(t, admin, `SELECT count(*) FROM afrinic_run
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
	if n := count(t, admin, "SELECT count(*) FROM afrinic_run"); n != 0 {
		t.Errorf("concorrência não grava execução: %d", n)
	}
}

// TestApplyRealFile aplica o arquivo real inteiro do RIR, quando
// AFRINIC_REAL_FILE aponta para ele (pulado se ausente): carga completa e
// reaplicação idempotente, com os tempos no log.
func TestApplyRealFile(t *testing.T) {
	path := os.Getenv("AFRINIC_REAL_FILE")
	if path == "" {
		t.Skip("defina AFRINIC_REAL_FILE com o caminho do arquivo real")
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

	asns, pfx := count(t, admin, "SELECT count(*) FROM afrinic_asn"), count(t, admin, "SELECT count(*) FROM afrinic_prefix")
	if asns != len(ds.ASNs) || pfx != len(ds.Prefixes) {
		t.Errorf("tabelas = %d/%d, dataset %d/%d", asns, pfx, len(ds.ASNs), len(ds.Prefixes))
	}
	t.Logf("tabelas: %d registros de ASN, %d blocos", asns, pfx)

	// Sobreposição de faixas de ASN (a api-afrinic supõe faixas disjuntas).
	if n := count(t, admin, `SELECT count(*) FROM (
		SELECT asn_start, lag(asn_end) OVER (ORDER BY asn_start) AS prev_end FROM afrinic_asn) x
		WHERE asn_start <= prev_end`); n != 0 {
		t.Errorf("%d faixas de ASN sobrepostas", n)
	}
	if _, err := admin.Exec(ctx, "ANALYZE afrinic_asn; ANALYZE afrinic_prefix"); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"EXPLAIN SELECT * FROM afrinic_asn WHERE asn_start <= 37000 ORDER BY asn_start DESC LIMIT 1",
		"EXPLAIN SELECT * FROM afrinic_prefix WHERE prefix >>= '196.4.21.3'::inet ORDER BY masklen(prefix) DESC LIMIT 1",
		"EXPLAIN SELECT * FROM afrinic_prefix WHERE opaque_id = 'F3619C8C'",
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
