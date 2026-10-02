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

	"github.com/patrickbrandao/badblock/apps/roothints/collector/internal/parse"
)

// setup sobe um PG18, aplica as migrations de central/ e roothints/ (seção
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

	for _, dir := range []string{"central", "roothints"} {
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

func parseText(t *testing.T, s string) *parse.Dataset {
	t.Helper()
	ds, err := parse.Parse(strings.NewReader(s))
	if err != nil {
		t.Fatal(err)
	}
	return ds
}

func fixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../testdata/named.root")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func runFor(hash string, ds *parse.Dataset) Run {
	return Run{StartedAt: time.Now(), URL: "https://example.test/named.root", HTTPStatus: 200,
		ETag: `"cf3-65ca377d2eb00"`, LastModified: "Tue, 29 Sep 2026 18:37:00 GMT",
		MD5: strings.Repeat(hash, 32), SHA256: strings.Repeat(hash, 64), Bytes: 3315,
		Parsed: true, Header: ds.Header, Servers: len(ds.Servers), IPv4Addresses: ds.IPv4Count(), IPv6Addresses: ds.IPv6Count(),
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

type row struct {
	name, letter         string
	ipv4, ipv6           *netip.Prefix
	nsTTL                int64
	ipv4TTL, ipv6TTL     *int64
	note                 *string
	createdAt, updatedAt time.Time
}

// readServer lê um servidor pela letra, com a consulta que a api-roothints usa.
func readServer(t *testing.T, admin *pgxpool.Pool, letter string) row {
	t.Helper()
	var r row
	err := admin.QueryRow(context.Background(), `
		SELECT name, letter, ipv4, ipv6, ns_ttl, ipv4_ttl, ipv6_ttl, note, created_at, updated_at
		  FROM roothints_server WHERE letter = $1`, letter).
		Scan(&r.name, &r.letter, &r.ipv4, &r.ipv6, &r.nsTTL, &r.ipv4TTL, &r.ipv6TTL, &r.note, &r.createdAt, &r.updatedAt)
	if err != nil {
		t.Fatalf("servidor %s: %v", letter, err)
	}
	return r
}

func addr(p *netip.Prefix) string {
	if p == nil {
		return "<NULL>"
	}
	return p.Addr().String()
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
	ds := parseText(t, fixture(t))
	ch, v1, err := st.Apply(ctx, ds, runFor("a", ds), opt)
	if err != nil {
		t.Fatal(err)
	}
	if ch != (Changes{ServerInserted: 13}) {
		t.Errorf("carga inicial = %+v", ch)
	}
	if j := readJob(t, admin); j.lastSync == nil || j.lastCheck == nil || j.consolidated != 0 {
		t.Errorf("jobs depois da carga = %+v", j)
	}
	a := readServer(t, admin, "a")
	if a.name != "a.root-servers.net" || addr(a.ipv4) != "198.41.0.4" || addr(a.ipv6) != "2001:503:ba3e::2:30" ||
		a.ipv4.Bits() != 32 || a.ipv6.Bits() != 128 || a.nsTTL != 3600000 || *a.ipv4TTL != 3600000 ||
		*a.ipv6TTL != 3600000 || *a.note != "FORMERLY NS.INTERNIC.NET" {
		t.Errorf("a = %+v (%s / %s)", a, addr(a.ipv4), addr(a.ipv6))
	}
	// As consultas da api-roothints: lista em ordem, por nome e por letra.
	var names []string
	rows, err := admin.Query(ctx, "SELECT name FROM roothints_server ORDER BY letter")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		names = append(names, n)
	}
	rows.Close()
	if len(names) != 13 || names[0] != "a.root-servers.net" || names[12] != "m.root-servers.net" {
		t.Errorf("lista = %v", names)
	}
	if n := count(t, admin, "SELECT count(*) FROM roothints_server WHERE name = $1 AND host(ipv4) = '202.12.27.33'", "m.root-servers.net"); n != 1 {
		t.Errorf("consulta por nome = %d", n)
	}
	var gotRun struct {
		status, servers, v4, v6, ins int
		serial                       int64
		lastUpdate                   time.Time
		md5, etag                    string
	}
	_ = admin.QueryRow(ctx, `SELECT status, servers, ipv4_addresses, ipv6_addresses, server_inserted, zone_serial, last_update, md5, etag
		FROM roothints_run WHERE uuid = $1`, v1).
		Scan(&gotRun.status, &gotRun.servers, &gotRun.v4, &gotRun.v6, &gotRun.ins, &gotRun.serial, &gotRun.lastUpdate, &gotRun.md5, &gotRun.etag)
	if gotRun.status != 1 || gotRun.servers != 13 || gotRun.v4 != 13 || gotRun.v6 != 13 || gotRun.ins != 13 ||
		gotRun.serial != 2026092401 || gotRun.lastUpdate.Format("2006-01-02") != "2026-09-24" ||
		gotRun.md5 != strings.Repeat("a", 32) || gotRun.etag != `"cf3-65ca377d2eb00"` {
		t.Errorf("roothints_run = %+v", gotRun)
	}
	last, err := st.LastApplied(ctx)
	if err != nil || last == nil || last.Version != v1 || last.MD5 != strings.Repeat("a", 32) || last.ZoneSerial == nil || *last.ZoneSerial != 2026092401 {
		t.Errorf("LastApplied = %+v, err = %v", last, err)
	}

	// 2) A consolidação (fase 2) marca 1; o mesmo conteúdo não muda nada.
	if _, err := admin.Exec(ctx, "UPDATE jobs SET consolidated = 1"); err != nil {
		t.Fatal(err)
	}
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
	if after := readServer(t, admin, "a"); !after.updatedAt.Equal(a.updatedAt) {
		t.Error("reaplicação sem mudança não pode tocar updated_at")
	}

	// 3) Mudanças: IPv4 novo do B, M sem AAAA e sem comentário (NULLs).
	changed := strings.Replace(fixture(t), "170.247.170.2", "199.9.14.201", 1)
	changed = strings.Replace(changed, "M.ROOT-SERVERS.NET.      3600000      AAAA  2001:dc3::35\n", "", 1)
	changed = strings.Replace(changed, "; OPERATED BY WIDE\n", "", 1)
	ds3 := parseText(t, changed)
	ch, _, err = st.Apply(ctx, ds3, runFor("c", ds3), opt)
	if err != nil {
		t.Fatal(err)
	}
	if want := (Changes{ServerUpdated: 2}); ch != want {
		t.Errorf("mudanças = %+v, quero %+v", ch, want)
	}
	if j := readJob(t, admin); j.consolidated != 0 {
		t.Errorf("com mudança a flag volta a 0: %+v", j)
	}
	if b := readServer(t, admin, "b"); addr(b.ipv4) != "199.9.14.201" || !b.updatedAt.After(b.createdAt) {
		t.Errorf("b = %+v", b)
	}
	if m := readServer(t, admin, "m"); m.ipv6 != nil || m.ipv6TTL != nil || m.note != nil || addr(m.ipv4) != "202.12.27.33" {
		t.Errorf("m = %+v", m)
	}

	// 4) Trava de remoção: 1 de 13 (7,7%) passa do limite de 5%; --force aplica.
	idx := strings.Index(fixture(t), "; \n; OPERATED BY WIDE")
	short := fixture(t)[:idx] + "; End of file"
	ds4 := parseText(t, short)
	_, _, err = st.Apply(ctx, ds4, runFor("d", ds4), opt)
	var rem *RemovalError
	if !errors.As(err, &rem) || rem.Removed != 1 || rem.Current != 13 {
		t.Fatalf("esperava RemovalError de 1/13, veio %v", err)
	}
	if !strings.Contains(err.Error(), "o arquivo removeria 1 de 13 servidores (7.7%, limite 5.0%)") {
		t.Errorf("mensagem = %q", err)
	}
	if n := count(t, admin, "SELECT count(*) FROM roothints_server"); n != 13 {
		t.Errorf("recusa não pode mexer na tabela: %d servidores", n)
	}
	ch, _, err = st.Apply(ctx, ds4, runFor("e", ds4), ApplyOptions{RemovalThreshold: 0.05, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if ch.ServerDeleted != 1 || count(t, admin, "SELECT count(*) FROM roothints_server WHERE letter = 'm'") != 0 {
		t.Errorf("com --force = %+v", ch)
	}

	// 5) Falhas ficam em roothints_run e não viram versão; sem parser, sem
	// cabeçalho nem contagens.
	fail := Run{StartedAt: time.Now(), URL: "https://example.test/named.root", HTTPStatus: 200,
		MD5: strings.Repeat("f", 32), SHA256: strings.Repeat("f", 64), Bytes: 10}
	if err := st.RecordFailure(ctx, fail, errors.New("parser: boom")); err != nil {
		t.Fatal(err)
	}
	few := runFor("0", ds)
	few.Header.ZoneSerial = 2026092400
	if err := st.RecordFailure(ctx, few, errors.New("só 13 servidores no arquivo (mínimo 14): arquivo truncado?")); err != nil {
		t.Fatal(err)
	}
	last, err = st.LastApplied(ctx)
	if err != nil || last == nil || last.SHA256 != strings.Repeat("e", 64) {
		t.Errorf("LastApplied = %+v, err = %v", last, err)
	}
	if n := count(t, admin, `SELECT count(*) FROM roothints_run
		WHERE status = 0 AND error = 'parser: boom' AND servers IS NULL AND zone_serial IS NULL AND last_update IS NULL
		  AND server_inserted IS NULL AND warnings = '[]'::jsonb`); n != 1 {
		t.Errorf("falha do parser gravada = %d", n)
	}
	if n := count(t, admin, `SELECT count(*) FROM roothints_run
		WHERE status = 0 AND zone_serial = 2026092400 AND servers = 13 AND server_inserted IS NULL
		  AND error LIKE 'só 13 servidores%'`); n != 1 {
		t.Errorf("recusa depois do parser (com cabeçalho) gravada = %d", n)
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

// As constraints recusam o que o parser nunca produz.
func TestConstraints(t *testing.T) {
	_, admin := setup(t)
	ctx := context.Background()
	bad := []string{
		`INSERT INTO roothints_server (name, letter, ipv4, ns_ttl, ipv4_ttl) VALUES ('A.ROOT-SERVERS.NET', 'a', '198.41.0.4', 1, 1)`,
		`INSERT INTO roothints_server (name, letter, ipv4, ns_ttl, ipv4_ttl) VALUES ('a.root-servers.net', 'b', '198.41.0.4', 1, 1)`,
		`INSERT INTO roothints_server (name, letter, ipv4, ns_ttl, ipv4_ttl) VALUES ('a.root-servers.net', 'a', '2001:db8::1', 1, 1)`,
		`INSERT INTO roothints_server (name, letter, ipv4, ns_ttl, ipv4_ttl) VALUES ('a.root-servers.net', 'a', '198.41.0.0/24', 1, 1)`,
		`INSERT INTO roothints_server (name, letter, ipv6, ns_ttl, ipv6_ttl) VALUES ('a.root-servers.net', 'a', '198.41.0.4', 1, 1)`,
		`INSERT INTO roothints_server (name, letter, ns_ttl) VALUES ('a.root-servers.net', 'a', 1)`,
		`INSERT INTO roothints_server (name, letter, ipv4, ns_ttl) VALUES ('a.root-servers.net', 'a', '198.41.0.4', 1)`,
		`INSERT INTO roothints_server (name, letter, ipv4, ns_ttl, ipv4_ttl, ipv6_ttl) VALUES ('a.root-servers.net', 'a', '198.41.0.4', 1, 1, 1)`,
		`INSERT INTO roothints_server (name, letter, ipv4, ns_ttl, ipv4_ttl) VALUES ('a.root-servers.net', 'a', '198.41.0.4', -1, 1)`,
		`INSERT INTO roothints_server (name, letter, ipv4, ns_ttl, ipv4_ttl, note) VALUES ('a.root-servers.net', 'a', '198.41.0.4', 1, 1, '')`,
		`INSERT INTO roothints_run (status, url, zone_serial, started_at) VALUES (1, 'x', 4294967296, NOW())`,
		`INSERT INTO roothints_run (status, url, md5, started_at) VALUES (1, 'x', 'ABC', NOW())`,
	}
	for _, sql := range bad {
		if _, err := admin.Exec(ctx, sql); err == nil {
			t.Errorf("esperava recusa: %s", sql)
		}
	}
	if _, err := admin.Exec(ctx, `INSERT INTO roothints_server (name, letter, ipv6, ns_ttl, ipv6_ttl)
		VALUES ('n.root-servers.net', 'n', '2001:db8::1', 0, 0)`); err != nil {
		t.Errorf("servidor só com IPv6 deveria passar: %v", err)
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
	ds := parseText(t, fixture(t))
	if _, _, err := st.Apply(ctx, ds, runFor("a", ds), ApplyOptions{RemovalThreshold: 0.05}); !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v, quero ErrBusy", err)
	}
	if n := count(t, admin, "SELECT count(*) FROM roothints_run"); n != 0 {
		t.Errorf("concorrência não grava execução: %d", n)
	}
}

// TestApplyRealFile aplica o arquivo do dia, quando ROOTHINTS_REAL_FILE aponta
// para uma cópia de https://www.internic.net/domain/named.root, e confere que
// a reaplicação é idempotente.
func TestApplyRealFile(t *testing.T) {
	path := os.Getenv("ROOTHINTS_REAL_FILE")
	if path == "" {
		t.Skip("defina ROOTHINTS_REAL_FILE para testar com o arquivo real")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	st, admin := setup(t)
	ctx := context.Background()
	ds := parseText(t, string(b))
	opt := ApplyOptions{RemovalThreshold: 0.05}

	start := time.Now()
	ch, _, err := st.Apply(ctx, ds, runFor("a", ds), opt)
	if err != nil {
		t.Fatal(err)
	}
	first := time.Since(start)
	if ch.ServerInserted != len(ds.Servers) || ch.ServerUpdated != 0 || ch.ServerDeleted != 0 {
		t.Errorf("carga inicial = %+v", ch)
	}
	start = time.Now()
	ch, _, err = st.Apply(ctx, ds, runFor("b", ds), opt)
	if err != nil {
		t.Fatal(err)
	}
	second := time.Since(start)
	if ch.Total() != 0 {
		t.Errorf("reaplicação deveria ser idempotente: %+v", ch)
	}
	if n := count(t, admin, "SELECT count(*) FROM roothints_server"); n != len(ds.Servers) {
		t.Errorf("linhas = %d, quero %d", n, len(ds.Servers))
	}
	t.Logf("%d servidores (serial %d): carga inicial %s, reaplicação sem mudança %s",
		len(ds.Servers), ds.Header.ZoneSerial, first, second)
}
