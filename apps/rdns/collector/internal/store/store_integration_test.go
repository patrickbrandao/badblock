//go:build integration

package store

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/patrickbrandao/badblock/apps/rdns/collector/internal/resolver"
)

// setup sobe um PG18, aplica as migrations de central/, cgibr/ e rdns/ (seção
// migrate:up) e devolve um Store e um pool administrativo no mesmo banco.
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
	url, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	for _, dir := range []string{"central", "cgibr", "rdns"} {
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
	st, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	return st, admin
}

func exec(t *testing.T, db *pgxpool.Pool, sql string, args ...any) int64 {
	t.Helper()
	tag, err := db.Exec(context.Background(), sql, args...)
	if err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return tag.RowsAffected()
}

func count(t *testing.T, db *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

func prefixes(t *testing.T, db *pgxpool.Pool, where string) []string {
	t.Helper()
	rows, err := db.Query(context.Background(), "SELECT prefix::text FROM rdns_prefix WHERE "+where+" ORDER BY prefix")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		_ = rows.Scan(&p)
		out = append(out, p)
	}
	return out
}

func TestTriggers(t *testing.T) {
	_, admin := setup(t)
	ctx := context.Background()

	// Um /20 sobre um /24 existente vira os 15 /24 que faltam, herdando asn e
	// source; o original não entra (o INSERT devolve 0 linhas).
	exec(t, admin, "INSERT INTO rdns_prefix (prefix, asn) VALUES ('200.160.2.0/24', 22548)")
	if n := exec(t, admin, "INSERT INTO rdns_prefix (prefix, asn, source) VALUES ('200.160.0.0/20', 22548, 'cgibr')"); n != 0 {
		t.Errorf("INSERT sobre linha existente devolveu %d linhas", n)
	}
	if n := count(t, admin, "SELECT count(*) FROM rdns_prefix WHERE prefix <<= '200.160.0.0/20'"); n != 16 {
		t.Errorf("/24 no /20 = %d, quero 16", n)
	}
	if n := count(t, admin, "SELECT count(*) FROM rdns_prefix WHERE source = 'cgibr' AND asn = 22548 AND masklen(prefix) = 24"); n != 15 {
		t.Errorf("/24 herdados = %d, quero 15", n)
	}
	if n := count(t, admin, "SELECT count(*) FROM rdns_prefix WHERE prefix = '200.160.0.0/20'"); n != 0 {
		t.Error("o /20 original não deveria entrar")
	}
	if p := prefixes(t, admin, "source = 'static'"); !slices.Equal(p, []string{"200.160.2.0/24"}) {
		t.Errorf("estáticos = %v", p)
	}

	// Um /24 dentro de um /22 é descartado; um IPv6 sem sobreposição entra.
	exec(t, admin, "INSERT INTO rdns_prefix (prefix) VALUES ('10.0.0.0/22')")
	exec(t, admin, "INSERT INTO rdns_prefix (prefix) VALUES ('10.0.1.0/24')")
	if p := prefixes(t, admin, "prefix <<= '10.0.0.0/16'"); !slices.Equal(p, []string{"10.0.0.0/22"}) {
		t.Errorf("10.0.0.0/16 = %v", p)
	}
	exec(t, admin, "INSERT INTO rdns_prefix (prefix) VALUES ('2001:db8::/32')")
	// Um /22 que só parcialmente cobre um /26 estático: o /24 parcial não entra.
	exec(t, admin, "INSERT INTO rdns_prefix (prefix) VALUES ('10.1.1.0/26')")
	exec(t, admin, "INSERT INTO rdns_prefix (prefix) VALUES ('10.1.0.0/22')")
	if p := prefixes(t, admin, "prefix <<= '10.1.0.0/16'"); !slices.Equal(p, []string{"10.1.0.0/24", "10.1.1.0/26", "10.1.2.0/24", "10.1.3.0/24"}) {
		t.Errorf("10.1.0.0/16 = %v", p)
	}
	// A restrição de exclusão vale para qualquer escritor (o trigger desligado
	// não a desliga).
	tx, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "ALTER TABLE rdns_prefix DISABLE TRIGGER trg_rdns_prefix_split"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "INSERT INTO rdns_prefix (prefix) VALUES ('10.0.2.0/24')"); err == nil ||
		!strings.Contains(err.Error(), "ex_rdns_prefix_overlap") {
		t.Errorf("sobreposição sem o trigger: %v", err)
	}
	_ = tx.Rollback(ctx)

	// O DELETE de um prefixo apaga os IPs dele em rdns_ptr.
	exec(t, admin, `INSERT INTO rdns_ptr (ip, status, name, names) VALUES
		('200.160.2.3', 1, 'registro.br', '{registro.br}'),
		('200.160.3.1', 4, NULL, NULL)`)
	exec(t, admin, "DELETE FROM rdns_prefix WHERE prefix = '200.160.2.0/24'")
	if n := count(t, admin, "SELECT count(*) FROM rdns_ptr WHERE ip = '200.160.2.3'"); n != 0 {
		t.Error("o IP do prefixo apagado continua em rdns_ptr")
	}
	if n := count(t, admin, "SELECT count(*) FROM rdns_ptr WHERE ip = '200.160.3.1'"); n != 1 {
		t.Error("o IP de outro prefixo foi apagado")
	}

	// rdns_ptr recusa nome sem status ptr e IPv6.
	for _, bad := range []string{
		"INSERT INTO rdns_ptr (ip, status, name, names) VALUES ('200.160.3.2', 1, NULL, NULL)",
		"INSERT INTO rdns_ptr (ip, status, name, names) VALUES ('200.160.3.2', 1, 'b', '{a,b}')",
		"INSERT INTO rdns_ptr (ip, status, name, names) VALUES ('200.160.3.2', 2, 'a', '{a}')",
		"INSERT INTO rdns_ptr (ip, status) VALUES ('2001:db8::1', 4)",
		"INSERT INTO rdns_ptr (ip, status) VALUES ('200.160.3.0/24', 4)",
	} {
		if _, err := admin.Exec(ctx, bad); err == nil {
			t.Errorf("deveria recusar: %s", bad)
		}
	}

	// updated_at só anda com o cadastro.
	exec(t, admin, "UPDATE rdns_prefix SET updated_at = '2000-01-01Z' WHERE prefix = '200.160.3.0/24'")
	stamp := func() time.Time {
		var ts time.Time
		_ = admin.QueryRow(ctx, "SELECT updated_at FROM rdns_prefix WHERE prefix = '200.160.3.0/24'").Scan(&ts)
		return ts
	}
	exec(t, admin, `UPDATE rdns_prefix SET rescan = true, last_scan_at = NOW(), last_scan_ms = 10,
		scan_started_at = NOW() WHERE prefix = '200.160.3.0/24'`)
	exec(t, admin, "UPDATE rdns_prefix SET asn = asn WHERE prefix = '200.160.3.0/24'")
	if ts := stamp(); ts.UTC().Year() != 2000 {
		t.Errorf("o estado da varredura andou o updated_at: %v", ts)
	}
	exec(t, admin, "UPDATE rdns_prefix SET active = false WHERE prefix = '200.160.3.0/24'")
	if ts := stamp(); ts.UTC().Year() == 2000 {
		t.Error("a mudança de cadastro deveria andar o updated_at")
	}
}

// loadCgibr grava uma versão do cgibr: ASNs, blocos e a linha aplicada.
func loadCgibr(t *testing.T, admin *pgxpool.Pool, blocks map[string]int64) string {
	t.Helper()
	ctx := context.Background()
	exec(t, admin, "DELETE FROM cgibr_prefix")
	for p, asn := range blocks {
		exec(t, admin, `INSERT INTO cgibr_asn (asn, name, document) VALUES ($1, 'X', '00.000.000/0001-00')
			ON CONFLICT (asn) DO NOTHING`, asn)
		exec(t, admin, `INSERT INTO cgibr_prefix (prefix, asn_uuid) SELECT $1::cidr, uuid FROM cgibr_asn WHERE asn = $2`, p, asn)
	}
	var v string
	if err := admin.QueryRow(ctx, `INSERT INTO cgibr_run (status, url, sha256, started_at)
		VALUES (1, 'https://example.test/f.txt', repeat('a', 64), NOW()) RETURNING uuid::text`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func asnOf(t *testing.T, admin *pgxpool.Pool, p string) int64 {
	t.Helper()
	var asn int64
	if err := admin.QueryRow(context.Background(), "SELECT asn FROM rdns_prefix WHERE prefix = $1::cidr", p).Scan(&asn); err != nil {
		t.Fatalf("%s: %v", p, err)
	}
	return asn
}

func TestDerive(t *testing.T) {
	st, admin := setup(t)
	ctx := context.Background()

	// Sem dataset do cgibr: sem mudança.
	res, err := st.Derive(ctx, []string{"cgibr"}, false)
	if err != nil || !res.Locked || len(res.Runs) != 1 || res.Runs[0].Applied || res.Runs[0].Reason != "cgibr sem dataset aplicado" {
		t.Fatalf("sem cgibr = %+v, %v", res, err)
	}

	// Uma linha estática dentro de um bloco do cgibr e uma instância morta.
	exec(t, admin, "INSERT INTO rdns_prefix (prefix, asn) VALUES ('187.87.30.0/24', 0)")
	exec(t, admin, `INSERT INTO rdns_worker (host, version, rate_limit, updated_at)
		VALUES ('morto', 'v', 1, NOW() - interval '25 hours')`)

	v1 := loadCgibr(t, admin, map[string]int64{
		"200.160.0.0/20": 22548, // 16 /24
		"187.87.28.0/22": 61610, // 4 /24, um deles estático
		"187.87.29.0/24": 64500, // sobreposto ao /22: vale o mais específico
		"45.6.7.0/25":    1,     // mais longo que /24: aviso
		"2001:12ff::/32": 22548, // IPv6 não entra
	})
	res, err = st.Derive(ctx, []string{"cgibr"}, false)
	if err != nil {
		t.Fatal(err)
	}
	run := res.Runs[0]
	if !run.Applied || run.Version != v1 || run.Blocks != 4 || run.Units != 20 || run.Inserted != 19 ||
		run.Covered != 1 || run.ASNUpdated != 0 || run.NotInSource != 0 {
		t.Errorf("primeira derivação = %+v", run)
	}
	if len(run.Warnings) != 1 || run.Warnings[0] != "bloco 45.6.7.0/25 mais longo que /24 ignorado" {
		t.Errorf("avisos = %v", run.Warnings)
	}
	if res.WorkersRemoved != 1 {
		t.Errorf("instâncias mortas apagadas = %d", res.WorkersRemoved)
	}
	if asnOf(t, admin, "187.87.29.0/24") != 64500 || asnOf(t, admin, "187.87.28.0/24") != 61610 {
		t.Error("o /24 repetido deveria ficar com o ASN do bloco mais específico")
	}
	if asnOf(t, admin, "187.87.30.0/24") != 0 || count(t, admin, "SELECT count(*) FROM rdns_prefix WHERE source = 'static'") != 1 {
		t.Error("a linha estática foi tocada")
	}
	var applied int
	var warnings string
	_ = admin.QueryRow(ctx, "SELECT count(*), max(warnings::text) FROM rdns_run WHERE status = 1").Scan(&applied, &warnings)
	if applied != 1 || !strings.Contains(warnings, "45.6.7.0/25") {
		t.Errorf("rdns_run = %d %s", applied, warnings)
	}
	var lastSync, lastCheck *time.Time
	var consolidated int
	_ = admin.QueryRow(ctx, "SELECT last_sync_at, last_check_at, consolidated FROM jobs WHERE app = $1", AppName).
		Scan(&lastSync, &lastCheck, &consolidated)
	if lastSync == nil || lastCheck == nil || consolidated != 0 {
		t.Errorf("jobs = %v %v %d", lastSync, lastCheck, consolidated)
	}

	// Mesma versão: sem mudança.
	res, err = st.Derive(ctx, []string{"cgibr"}, false)
	if err != nil || res.Runs[0].Applied || res.Runs[0].Reason != "versão do cgibr já derivada" {
		t.Errorf("versão repetida = %+v, %v", res, err)
	}

	// Versão nova: o /20 saiu (as linhas ficam: not_in_source) e o /22 mudou
	// de ASN (a linha estática continua com o dela).
	exec(t, admin, "UPDATE jobs SET consolidated = 1")
	loadCgibr(t, admin, map[string]int64{"187.87.28.0/22": 64500, "187.87.29.0/24": 64500})
	res, err = st.Derive(ctx, []string{"cgibr"}, false)
	if err != nil {
		t.Fatal(err)
	}
	run = res.Runs[0]
	if !run.Applied || run.Inserted != 0 || run.ASNUpdated != 2 || run.NotInSource != 16 || run.Units != 4 {
		t.Errorf("segunda derivação = %+v", run)
	}
	if asnOf(t, admin, "187.87.31.0/24") != 64500 || asnOf(t, admin, "187.87.30.0/24") != 0 {
		t.Error("ASN corrigido errado")
	}
	if n := count(t, admin, "SELECT count(*) FROM rdns_prefix WHERE prefix <<= '200.160.0.0/20'"); n != 16 {
		t.Errorf("prefixos que saíram do cgibr = %d, quero 16 (nunca saem sozinhos)", n)
	}
	if n := count(t, admin, "SELECT consolidated FROM jobs WHERE app = $1", AppName); n != 0 {
		t.Error("ASN corrigido deveria zerar consolidated")
	}

	// --force deriva a mesma versão de novo.
	res, err = st.Derive(ctx, []string{"cgibr"}, true)
	if err != nil || !res.Runs[0].Applied || !res.Runs[0].Forced || res.Runs[0].Inserted != 0 {
		t.Errorf("--force = %+v, %v", res, err)
	}

	// Prefixos fora da regra são contados.
	exec(t, admin, "INSERT INTO rdns_prefix (prefix) VALUES ('2001:db8::/32'), ('10.8.0.0/21'), ('10.9.0.0/25')")
	res, err = st.Derive(ctx, nil, false) // SOURCES=none: só manutenção e jobs
	if err != nil || len(res.Runs) != 0 || res.OutOfRule != 3 {
		t.Errorf("none = %+v, %v", res, err)
	}

	// Falha gravada fora da transação.
	if err := st.RecordDeriveFailure(ctx, DeriveRun{Source: "cgibr", Version: v1, SourceUpdatedAt: time.Now(),
		StartedAt: time.Now()}, errors.New("falhou")); err != nil {
		t.Fatal(err)
	}
	if n := count(t, admin, "SELECT count(*) FROM rdns_run WHERE status = 0 AND error = 'falhou' AND units IS NULL"); n != 1 {
		t.Errorf("linha de falha = %d", n)
	}
}

func TestDeriveLock(t *testing.T) {
	st, admin := setup(t)
	ctx := context.Background()
	loadCgibr(t, admin, map[string]int64{"200.160.0.0/22": 22548})

	// Outra instância segura a trava: esta não deriva.
	tx, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", AppName); err != nil {
		t.Fatal(err)
	}
	res, err := st.Derive(ctx, []string{"cgibr"}, false)
	if err != nil || res.Locked {
		t.Errorf("com a trava ocupada = %+v, %v", res, err)
	}
	_ = tx.Rollback(ctx)

	// Duas ao mesmo tempo: uma deriva, a outra não tem a trava (ou já acha a
	// versão derivada).
	var wg sync.WaitGroup
	results := make([]DeriveResult, 2)
	for i := range results {
		wg.Go(func() {
			r, err := st.Derive(ctx, []string{"cgibr"}, false)
			if err != nil {
				t.Error(err)
			}
			results[i] = r
		})
	}
	wg.Wait()
	derived := 0
	for _, r := range results {
		if r.Locked && len(r.Runs) == 1 && r.Runs[0].Applied {
			derived++
		}
	}
	if derived != 1 || count(t, admin, "SELECT count(*) FROM rdns_run WHERE status = 1") != 1 {
		t.Errorf("derivaram %d vezes: %+v", derived, results)
	}
}

func worker(t *testing.T, st *Store, host string) string {
	t.Helper()
	w := Worker{UUID: newUUID(t, st), Host: host, Version: "test", RateLimit: 256}
	if err := st.Heartbeat(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	return w.UUID
}

func newUUID(t *testing.T, st *Store) string {
	t.Helper()
	var id string
	if err := st.pool.QueryRow(context.Background(), "SELECT uuidv7()::text").Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestQueue(t *testing.T) {
	st, admin := setup(t)
	ctx := context.Background()
	w1, w2 := worker(t, st, "a"), worker(t, st, "b")
	lease, interval := 2*time.Minute, 24*time.Hour

	exec(t, admin, `INSERT INTO rdns_prefix (prefix, last_scan_at, rescan, active) VALUES
		('10.0.1.0/24', NULL, false, true),                            -- nunca varrido
		('10.0.2.0/24', NOW() - interval '2 days', false, true),        -- vencido
		('10.0.3.0/24', NOW() - interval '1 day 1 hour', false, true),  -- vencido, mais novo
		('10.0.4.0/24', NOW() - interval '1 hour', false, true),        -- em dia
		('10.0.5.0/24', NOW() - interval '1 hour', true, true),         -- rescan
		('10.0.6.0/24', NULL, false, false),                            -- inativo
		('10.0.7.0/25', NULL, false, true),                             -- fora da regra
		('10.0.8.0/22', NULL, false, true)                              -- nunca varrido, /22`)
	var got []string
	for i := 0; ; i++ {
		w := w1
		if i%2 == 1 {
			w = w2
		}
		c, err := st.Claim(ctx, w, lease, interval)
		if err != nil {
			t.Fatal(err)
		}
		if c == nil {
			break
		}
		got = append(got, c.Prefix.String())
	}
	// rescan → nunca varridos → mais antigo; cada prefixo uma vez só.
	if len(got) != 5 || got[0] != "10.0.5.0/24" || got[3] != "10.0.2.0/24" || got[4] != "10.0.3.0/24" ||
		!slices.Contains(got[1:3], "10.0.1.0/24") || !slices.Contains(got[1:3], "10.0.8.0/22") {
		t.Errorf("ordem = %v", got)
	}

	// Várias instâncias ao mesmo tempo nunca pegam o mesmo prefixo.
	exec(t, admin, `INSERT INTO rdns_prefix (prefix)
		SELECT set_masklen('172.16.0.0'::inet + i * 256, 24)::cidr FROM generate_series(0, 39) i`)
	var mu sync.Mutex
	seen := map[string]int{}
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			w := w1
			if i%2 == 1 {
				w = w2
			}
			for {
				c, err := st.Claim(ctx, w, lease, interval)
				if err != nil {
					t.Error(err)
					return
				}
				if c == nil {
					return
				}
				mu.Lock()
				seen[c.Prefix.String()]++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if len(seen) != 40 {
		t.Errorf("prefixos pegos = %d, quero 40", len(seen))
	}
	for p, n := range seen {
		if n != 1 {
			t.Errorf("%s pego %d vezes", p, n)
		}
	}
}

func ptrResult(ip string, status resolver.Status, names ...string) resolver.Result {
	return resolver.Result{IP: netip.MustParseAddr(ip), Status: status, Names: names}
}

// results devolve o resultado de todos os endereços de um /24 (no_ptr no que
// não está em set).
func results(prefix string, set ...resolver.Result) []resolver.Result {
	p := netip.MustParsePrefix(prefix)
	var out []resolver.Result
	for a := p.Addr(); p.Contains(a); a = a.Next() {
		r := resolver.Result{IP: a, Status: resolver.NoPTR}
		for _, s := range set {
			if s.IP == a {
				r = s
			}
		}
		out = append(out, r)
	}
	return out
}

func TestScanLifecycle(t *testing.T) {
	st, admin := setup(t)
	ctx := context.Background()
	w1, w2 := worker(t, st, "a"), worker(t, st, "b")
	lease, interval := 2*time.Minute, 24*time.Hour
	exec(t, admin, "INSERT INTO rdns_prefix (prefix, asn) VALUES ('200.160.2.0/24', 22548)")

	// Primeira varredura: grava os PTR e as falhas.
	c, err := st.Claim(ctx, w1, lease, interval)
	if err != nil || c == nil {
		t.Fatalf("claim = %v, %v", c, err)
	}
	first := results("200.160.2.0/24",
		ptrResult("200.160.2.3", resolver.PTR, "registro.br"),
		ptrResult("200.160.2.5", resolver.PTR, "mailexploder.registro.br", "mailx.registro.br"),
		ptrResult("200.160.2.9", resolver.Timeout))
	counts, err := st.SaveScan(ctx, w1, c, first, 1500*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if counts != (Counts{PTR: 2, NoPTR: 253, Failed: 1, Changed: 3}) {
		t.Errorf("counts = %+v", counts)
	}
	var lastScan *time.Time
	var scanWorker *string
	var ms, ptr, changed int
	_ = admin.QueryRow(ctx, `SELECT last_scan_at, scan_worker::text, last_scan_ms, last_scan_ptr, last_scan_changed
		FROM rdns_prefix WHERE uuid = $1::uuid`, c.UUID).Scan(&lastScan, &scanWorker, &ms, &ptr, &changed)
	if lastScan == nil || scanWorker != nil || ms != 1500 || ptr != 2 || changed != 3 {
		t.Errorf("rdns_prefix = %v %v %d %d %d", lastScan, scanWorker, ms, ptr, changed)
	}
	var name string
	var names []string
	_ = admin.QueryRow(ctx, "SELECT name, names FROM rdns_ptr WHERE ip = '200.160.2.5'").Scan(&name, &names)
	if name != "mailexploder.registro.br" || len(names) != 2 {
		t.Errorf(".5 = %q %v", name, names)
	}
	if n := count(t, admin, "SELECT count(*) FROM jobs WHERE app = $1 AND last_sync_at IS NOT NULL AND consolidated = 0", AppName); n != 1 {
		t.Error("jobs deveria marcar a mudança")
	}

	// Em dia: não volta à fila.
	if c, _ := st.Claim(ctx, w1, lease, interval); c != nil {
		t.Errorf("prefixo em dia voltou à fila: %v", c.Prefix)
	}

	// rescan pedido antes de pegar é zerado no fim; pedido durante a varredura
	// sobrevive.
	exec(t, admin, "UPDATE rdns_prefix SET rescan = true")
	c, _ = st.Claim(ctx, w1, lease, interval)
	if c == nil || !c.Rescan {
		t.Fatalf("rescan = %+v", c)
	}
	// Igual à primeira, menos a falha no .3 (não apaga o PTR) e o .9, que
	// passou a ter PTR.
	second := results("200.160.2.0/24",
		ptrResult("200.160.2.3", resolver.Refused),
		ptrResult("200.160.2.5", resolver.PTR, "mailexploder.registro.br", "mailx.registro.br"),
		ptrResult("200.160.2.9", resolver.PTR, "novo.registro.br"))
	counts, err = st.SaveScan(ctx, w1, c, second, time.Second)
	if err != nil || counts.Changed != 1 {
		t.Errorf("segunda = %+v, %v", counts, err)
	}
	if n := count(t, admin, "SELECT count(*) FROM rdns_prefix WHERE rescan"); n != 0 {
		t.Error("rescan pedido antes de pegar deveria ser zerado")
	}
	if n := count(t, admin, "SELECT status FROM rdns_ptr WHERE ip = '200.160.2.3'"); n != 1 {
		t.Error("a falha apagou o PTR do .3")
	}

	exec(t, admin, "UPDATE rdns_prefix SET last_scan_at = NOW() - interval '2 days'")
	exec(t, admin, "UPDATE jobs SET consolidated = 1")
	c, _ = st.Claim(ctx, w1, lease, interval)
	exec(t, admin, "UPDATE rdns_prefix SET rescan = true") // durante a varredura
	if _, err := st.SaveScan(ctx, w1, c, second, time.Second); err != nil {
		t.Fatal(err)
	}
	if n := count(t, admin, "SELECT count(*) FROM rdns_prefix WHERE rescan"); n != 1 {
		t.Error("rescan pedido durante a varredura se perdeu")
	}
	// Nada mudou nessa: jobs não é tocado.
	if n := count(t, admin, "SELECT consolidated FROM jobs WHERE app = $1", AppName); n != 1 {
		t.Error("varredura sem mudança não deveria zerar consolidated")
	}

	// Lease vencido é retomado por outra instância; a primeira perde o
	// resultado.
	exec(t, admin, "UPDATE rdns_prefix SET rescan = false, last_scan_at = NOW() - interval '2 days'")
	c1, _ := st.Claim(ctx, w1, lease, interval)
	if c, _ := st.Claim(ctx, w2, lease, interval); c != nil {
		t.Fatal("prefixo em varredura foi pego por outra instância antes do lease vencer")
	}
	exec(t, admin, "UPDATE rdns_prefix SET scan_started_at = NOW() - interval '3 minutes'")
	c2, _ := st.Claim(ctx, w2, lease, interval)
	if c2 == nil || c2.UUID != c1.UUID {
		t.Fatalf("lease vencido não foi retomado: %+v", c2)
	}
	if _, err := st.SaveScan(ctx, w1, c1, results("200.160.2.0/24"), time.Second); !errors.Is(err, ErrLeaseLost) {
		t.Errorf("lease perdido = %v", err)
	}
	if n := count(t, admin, "SELECT count(*) FROM rdns_ptr"); n != 3 {
		t.Errorf("o resultado do lease perdido foi gravado: %d linhas", n)
	}

	// Abandono devolve à fila; apagar a instância também.
	if err := st.Release(ctx, w2, c2); err != nil {
		t.Fatal(err)
	}
	if n := count(t, admin, "SELECT count(*) FROM rdns_prefix WHERE scan_worker IS NULL AND scan_started_at IS NULL"); n != 1 {
		t.Error("Release não devolveu o prefixo")
	}
	c3, _ := st.Claim(ctx, w2, lease, interval)
	if c3 == nil {
		t.Fatal("prefixo devolvido não voltou à fila")
	}
	if err := st.DeleteWorker(ctx, w2); err != nil {
		t.Fatal(err)
	}
	if n := count(t, admin, "SELECT count(*) FROM rdns_prefix WHERE scan_worker IS NULL"); n != 1 {
		t.Error("apagar a instância deveria devolver os prefixos dela")
	}

	// Tempo real: regra e gravação de um IP.
	ok, err := st.LiveAllowed(ctx, netip.MustParseAddr("200.160.2.77"))
	if err != nil || !ok {
		t.Errorf("LiveAllowed = %v, %v", ok, err)
	}
	if ok, _ := st.LiveAllowed(ctx, netip.MustParseAddr("8.8.8.8")); ok {
		t.Error("IP fora dos prefixos não é permitido")
	}
	changedLive, err := st.SaveLive(ctx, ptrResult("200.160.2.77", resolver.PTR, "live.registro.br"))
	if err != nil || !changedLive {
		t.Errorf("SaveLive = %v, %v", changedLive, err)
	}
	if again, _ := st.SaveLive(ctx, ptrResult("200.160.2.77", resolver.PTR, "live.registro.br")); again {
		t.Error("o mesmo resultado não deveria regravar")
	}
	exec(t, admin, "UPDATE rdns_prefix SET active = false")
	if ok, _ := st.LiveAllowed(ctx, netip.MustParseAddr("200.160.2.77")); ok {
		t.Error("prefixo inativo não é permitido")
	}

	// rdns_config.
	exec(t, admin, "INSERT INTO rdns_config (key, value) VALUES ('rate_limit', '512')")
	cfg, err := st.Config(ctx)
	if err != nil || cfg["rate_limit"] != "512" {
		t.Errorf("Config = %v, %v", cfg, err)
	}
}
