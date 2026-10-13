//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/patrickbrandao/badblock/apps/rdns/api/internal/cache"
	"github.com/patrickbrandao/badblock/apps/rdns/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/rdns/api/internal/live"
	"github.com/patrickbrandao/badblock/apps/rdns/api/internal/realip"
	"github.com/patrickbrandao/badblock/apps/rdns/api/internal/store"
)

// setupDB sobe um PG18 com as migrations de central/ e rdns/ e devolve o
// Store da API e um pool administrativo (que faz o papel do coletor).
func setupDB(t *testing.T) (*store.Store, *pgxpool.Pool) {
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
	for _, dir := range []string{"central", "rdns"} {
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
	st, err := store.Open(ctx, url, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	return st, admin
}

func setupValkey(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	c, err := testcontainers.Run(ctx, "valkey/valkey:9-alpine",
		testcontainers.WithExposedPorts("6379/tcp"),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("6379/tcp")))
	testcontainers.CleanupContainer(t, c)
	if err != nil {
		t.Fatal(err)
	}
	ep, err := c.PortEndpoint(ctx, "6379/tcp", "")
	if err != nil {
		t.Fatal(err)
	}
	return "redis://" + ep + "/0"
}

func mustExec(t *testing.T, db *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := db.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

// load grava o que o coletor gravaria: 4.096 /24 (um deles o exemplo da
// spec), PTRs em metade dos endereços de 64 deles, uma derivação, duas
// instâncias e a linha de jobs.
//
// As datas são relativas ao NOW() do banco, nunca fixas: overdue compara
// com o relógio, e uma data fixa vira atrasada com o passar dos dias. Os
// 2.048 /24 pares foram varridos há 1 h e o exemplo da spec há 10 min, então
// overdue é 0 com 24 h e 2.048 com 30 min, em qualquer dia.
func load(t *testing.T, admin *pgxpool.Pool) {
	t.Helper()
	mustExec(t, admin, `INSERT INTO rdns_prefix (prefix, asn, source, last_scan_at, last_scan_ptr, last_scan_no_ptr, last_scan_failed)
		SELECT set_masklen('100.64.0.0'::inet + i * 256, 24)::cidr, 64500, 'cgibr',
		       CASE WHEN i % 2 = 0 THEN NOW() - interval '1 hour' END, 128, 127, 1
		  FROM generate_series(0, 4094) i`)
	mustExec(t, admin, `INSERT INTO rdns_prefix (prefix, asn, source, last_scan_at, last_scan_ptr, last_scan_no_ptr)
		VALUES ('200.160.2.0/24', 22548, 'cgibr', NOW() - interval '10 minutes', 2, 254)`)
	mustExec(t, admin, `INSERT INTO rdns_prefix (prefix, active) VALUES ('10.0.0.0/24', false), ('10.1.0.0/26', true)`)
	mustExec(t, admin, `INSERT INTO rdns_ptr (ip, status, name, names)
		SELECT set_masklen('100.64.0.0'::inet + i * 2, 32), 1, 'h' || i || '.example.net', ARRAY['h' || i || '.example.net']
		  FROM generate_series(0, 64 * 128 - 1) i`)
	mustExec(t, admin, `INSERT INTO rdns_ptr (ip, status, name, names) VALUES
		('200.160.2.1', 1, 'ae1-0.ar1.vr.registro.br', '{ae1-0.ar1.vr.registro.br}'),
		('200.160.2.3', 1, 'registro.br', '{registro.br}'),
		('200.160.2.9', 4, NULL, NULL)`)
	mustExec(t, admin, `INSERT INTO rdns_run (status, source, source_version, source_updated_at, blocks, units, inserted,
		asn_updated, covered, not_in_source, started_at)
		VALUES (1, 'cgibr', uuidv7(), NOW() - interval '1 day', 13045, 308529, 308529, 0, 0, 0, NOW())`)
	mustExec(t, admin, `INSERT INTO rdns_worker (host, version, rate_limit, queries, updated_at) VALUES
		('vivo', 'test', 256, 1000, NOW()), ('parado', 'test', 256, 10, NOW() - interval '5 minutes'),
		('morto', 'test', 256, 10, NOW() - interval '2 days')`)
	mustExec(t, admin, `INSERT INTO jobs (app, last_sync_at, last_check_at) VALUES ('collector-rdns', NOW(), NOW())`)
	mustExec(t, admin, "ANALYZE")
}

func TestQueriesAndIndexes(t *testing.T) {
	st, admin := setupDB(t)
	ctx := context.Background()
	if ok, err := st.HasPrefixes(ctx); err != nil || ok {
		t.Fatalf("banco vazio: HasPrefixes = %v, %v", ok, err)
	}
	load(t, admin)
	if ok, _ := st.HasPrefixes(ctx); !ok {
		t.Fatal("HasPrefixes deveria ser true")
	}

	row, err := st.IP(ctx, netip.MustParseAddr("200.160.2.3"))
	if err != nil || row.PTR == nil || *row.PTR.Name != "registro.br" || row.Prefix.ASN != 22548 || row.Prefix.LastScanAt == nil {
		t.Errorf("IP = %+v, %v", row, err)
	}
	if row, err := st.IP(ctx, netip.MustParseAddr("200.160.2.2")); err != nil || row.PTR != nil {
		t.Errorf("sem linha = %+v, %v", row, err)
	}
	if _, err := st.IP(ctx, netip.MustParseAddr("8.8.8.8")); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("fora = %v", err)
	}
	rows, err := st.Prefix(ctx, netip.MustParsePrefix("200.160.2.0/24"))
	if err != nil || len(rows.PTRs) != 3 || rows.PTRs[0].IP.String() != "200.160.2.1" || rows.PTRs[2].Name != nil {
		t.Errorf("Prefix = %+v, %v", rows, err)
	}
	if _, err := st.Prefix(ctx, netip.MustParsePrefix("10.1.0.0/24")); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("/24 sobre um /26 = %v", err)
	}
	if rows, err := st.Prefix(ctx, netip.MustParsePrefix("10.1.0.16/28")); err != nil || rows.Prefix.Prefix.String() != "10.1.0.0/26" {
		t.Errorf("/28 dentro do /26 = %+v, %v", rows, err)
	}

	stats, err := st.ScanStats(ctx, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Prefixes != 4098 || stats.Active != 4097 || stats.Static != 2 || stats.OutOfRule != 1 ||
		stats.Addresses != 4096*256 || stats.NeverScanned != 2047 || stats.Overdue != 0 || stats.OldestScanAt == nil ||
		stats.PTR != 4095*128+2 || stats.Failed != 4095 {
		t.Errorf("ScanStats = %+v", stats)
	}
	if s, _ := st.ScanStats(ctx, 30*time.Minute); s.Overdue != 2048 {
		t.Errorf("overdue com 30m = %d, quero 2048", s.Overdue)
	}
	ders, err := st.Derivations(ctx)
	if err != nil || len(ders) != 1 || ders[0].Units != 308529 || ders[0].Source != "cgibr" {
		t.Errorf("Derivations = %+v, %v", ders, err)
	}
	ws, err := st.Workers(ctx)
	if err != nil || len(ws) != 2 || !ws[0].Alive || ws[1].Alive {
		t.Errorf("Workers = %+v, %v", ws, err)
	}
	if j, err := st.Job(ctx); err != nil || j == nil || j.LastSyncAt == nil {
		t.Errorf("Job = %+v, %v", j, err)
	}
	mustExec(t, admin, "INSERT INTO rdns_config (key, value) VALUES ('scan_interval', '12h')")
	if cfg, err := st.Config(ctx); err != nil || cfg["scan_interval"] != "12h" {
		t.Errorf("Config = %v, %v", cfg, err)
	}

	// Cada consulta usa o índice: o GiST da ex_rdns_prefix_overlap e o
	// B-tree da PK de rdns_ptr (que vira faixa no <<=).
	for q, want := range map[string][]string{
		`SELECT p.prefix, p.asn, p.source, p.active, p.last_scan_at, r.status, r.name, r.names, r.created_at, r.updated_at
		   FROM rdns_prefix p LEFT JOIN rdns_ptr r ON r.ip = '100.64.0.2'::inet WHERE p.prefix >>= '100.64.0.2'::inet`: {"ex_rdns_prefix_overlap", "rdns_ptr_pkey"},
		`SELECT prefix, asn, source, active, last_scan_at FROM rdns_prefix WHERE prefix >>= '100.64.1.0/28'::cidr`: {"ex_rdns_prefix_overlap"},
		`SELECT ip, status, name, names FROM rdns_ptr WHERE ip <<= '100.64.1.0/24'::cidr ORDER BY ip`:              {"rdns_ptr_pkey"},
		`SELECT DISTINCT ON (source) source, uuid FROM rdns_run WHERE status = 1 ORDER BY source, created_at DESC`: {"ix_rdns_run_applied"},
		`SELECT last_sync_at FROM jobs WHERE app = 'collector-rdns'`:                                               {"uq_jobs_app"},
	} {
		plan := explain(t, admin, q)
		for _, idx := range want {
			if !strings.Contains(plan, idx) {
				t.Errorf("sem %s: %s\n→ %s", idx, q, plan)
			}
		}
		t.Logf("%s\n→ %s", strings.Join(strings.Fields(q), " "), plan)
	}
}

// explain devolve o plano numa linha, com seq scan desligado (confere que
// o índice serve a consulta, qualquer que seja o tamanho da tabela de teste).
func explain(t *testing.T, admin *pgxpool.Pool, q string) string {
	t.Helper()
	ctx := context.Background()
	conn, err := admin.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SET enable_seqscan = off"); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = conn.Exec(ctx, "RESET enable_seqscan") }()
	rows, err := conn.Query(ctx, "EXPLAIN "+q)
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

// fakeCollector faz o papel do collector-rdns: tira os pedidos da fila, grava
// em rdns_ptr e responde na lista de retorno com expiração.
func fakeCollector(ctx context.Context, t *testing.T, rdb *redis.Client, admin *pgxpool.Pool) {
	for ctx.Err() == nil {
		vals, err := rdb.BLPop(ctx, time.Second, live.QueueKey).Result()
		if err != nil {
			continue
		}
		var req live.Request
		if err := json.Unmarshal([]byte(vals[1]), &req); err != nil {
			t.Errorf("pedido ilegível: %s", vals[1])
			continue
		}
		if _, err := admin.Exec(ctx, `INSERT INTO rdns_ptr (ip, status, name, names) VALUES ($1::inet, 1, 'novo.registro.br', '{novo.registro.br}')
			ON CONFLICT (ip) DO UPDATE SET status = 1, name = EXCLUDED.name, names = EXCLUDED.names`, req.IP); err != nil {
			t.Error(err)
		}
		body, _ := json.Marshal(live.Reply{IP: req.IP, Result: "ptr", Names: []string{"novo.registro.br"},
			CheckedAt: time.Now().UTC().Format(time.RFC3339), Worker: "fake"})
		key := live.ReplyPrefix + req.IP
		_, _ = rdb.TxPipelined(ctx, func(p redis.Pipeliner) error {
			p.RPush(ctx, key, body)
			p.Expire(ctx, key, 30*time.Second)
			p.Del(ctx, live.InflightPrefix+req.IP)
			return nil
		})
	}
}

func TestLiveEndToEnd(t *testing.T) {
	st, admin := setupDB(t)
	load(t, admin)
	url := setupValkey(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	opts, _ := redis.ParseURL(url)
	rdb := redis.NewClient(opts)
	defer rdb.Close()
	c, err := cache.NewValkey(url, time.Hour, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	v, err := live.NewValkey(url)
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
	watcher := dataset.NewWatcher(st, time.Minute, slog.New(slog.DiscardHandler))
	if err := watcher.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	rip, _ := realip.New(realip.DefaultTrustedProxies, realip.DefaultHeaders)
	h := New(st, watcher, c, live.NewRequester(v, 5*time.Second, time.Minute, 1000), rip,
		slog.New(slog.DiscardHandler), Config{BasePath: "/rdns", Version: "test"}).Handler()

	// Frio primeiro: a chave sem versão vai para o Valkey.
	if rec := do(h, "GET", "/rdns/ip/200.160.2.3"); rec.Code != 200 || rec.Header().Get("X-Cache") != "MISS" {
		t.Fatalf("frio = %d %v", rec.Code, rec.Header())
	}
	if n := rdb.Exists(ctx, "badblock:api-rdns:ip:200.160.2.3").Val(); n != 1 {
		t.Error("chave fria ausente no Valkey")
	}
	if ttl := rdb.TTL(ctx, "badblock:api-rdns:ip:200.160.2.3").Val(); ttl <= 0 {
		t.Errorf("chave fria sem TTL: %v", ttl)
	}

	go fakeCollector(ctx, t, rdb, admin)
	rec := do(h, "GET", "/rdns/ip/200.160.2.3?live=true")
	got := decode[IPResponse](t, rec)
	if rec.Code != 200 || got.Live == nil || got.Live.Status != "resolved" || got.Name == nil || *got.Name != "novo.registro.br" {
		t.Fatalf("live = %d %s", rec.Code, rec.Body)
	}
	if rec.Header().Get("X-Cache") != "LIVE" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("cabeçalhos = %v", rec.Header())
	}
	// A chave fria foi regravada; a marca recent existe com TTL.
	if cold := rdb.Get(ctx, "badblock:api-rdns:ip:200.160.2.3").Val(); !strings.Contains(cold, "novo.registro.br") {
		t.Errorf("chave fria = %s", cold)
	}
	if ttl := rdb.TTL(ctx, live.RecentPrefix+"200.160.2.3").Val(); ttl <= 0 || ttl > time.Minute {
		t.Errorf("TTL da marca recent = %v", ttl)
	}
	if got := decode[IPResponse](t, do(h, "GET", "/rdns/ip/200.160.2.3?live=true")); got.Live.Status != "recent" {
		t.Errorf("segundo live = %+v", got.Live)
	}
	if n := rdb.Exists(ctx, live.InflightPrefix+"200.160.2.3").Val(); n != 0 {
		t.Error("a marca inflight deveria ter sido apagada pelo coletor")
	}

	// Sem coletor: timeout no prazo, com o dado frio, e a fila sem TTL.
	cancel()
	time.Sleep(1200 * time.Millisecond)
	ctx2 := context.Background()
	start := time.Now()
	got = decode[IPResponse](t, do(h, "GET", "/rdns/ip/200.160.2.1?live=true"))
	if got.Live.Status != "timeout" || *got.Name != "ae1-0.ar1.vr.registro.br" || time.Since(start) < 4*time.Second {
		t.Errorf("sem coletor = %+v em %v", got.Live, time.Since(start))
	}
	if n := rdb.LLen(ctx2, live.QueueKey).Val(); n != 1 {
		t.Errorf("fila = %d, quero 1 (o pedido fica para o coletor)", n)
	}
	if ttl := rdb.TTL(ctx2, live.QueueKey).Val(); ttl != -1 {
		t.Errorf("a fila não tem TTL: %v", ttl)
	}
	// /meta mostra a fila.
	m := decode[MetaResponse](t, do(h, "GET", "/rdns/meta"))
	if m.Live.Queue == nil || *m.Live.Queue != 1 || m.Config.ScanInterval != nil {
		t.Errorf("meta live = %+v", m.Live)
	}
}
