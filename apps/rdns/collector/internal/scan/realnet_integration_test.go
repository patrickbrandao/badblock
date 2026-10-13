//go:build integration

package scan

import (
	"context"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/patrickbrandao/badblock/apps/rdns/collector/internal/config"
	"github.com/patrickbrandao/badblock/apps/rdns/collector/internal/limiter"
	"github.com/patrickbrandao/badblock/apps/rdns/collector/internal/resolver"
	"github.com/patrickbrandao/badblock/apps/rdns/collector/internal/store"
	"github.com/patrickbrandao/badblock/apps/rdns/collector/internal/worker"
)

// TestRealNet varre um prefixo de verdade contra resolvedores de verdade, a
// 64 consultas/s, num PG18 descartável, e mostra as contagens e os tempos.
// Nunca roda sozinho: make test-real DNS_SERVERS=1.1.1.1 PREFIX=200.160.2.0/24
// (variáveis RDNS_REAL_DNS_SERVERS e RDNS_REAL_PREFIX).
func TestRealNet(t *testing.T) {
	serversEnv, prefixEnv := os.Getenv("RDNS_REAL_DNS_SERVERS"), os.Getenv("RDNS_REAL_PREFIX")
	if serversEnv == "" || prefixEnv == "" {
		t.Skip("defina RDNS_REAL_DNS_SERVERS e RDNS_REAL_PREFIX (make test-real)")
	}
	servers, err := config.ParseServers(serversEnv)
	if err != nil {
		t.Fatal(err)
	}
	prefix, err := netip.ParsePrefix(prefixEnv)
	if err != nil || !prefix.Addr().Is4() || prefix.Bits() < 22 || prefix.Bits() > 24 {
		t.Fatalf("RDNS_REAL_PREFIX precisa ser um IPv4 de /22 a /24: %q", prefixEnv)
	}
	prefix = prefix.Masked()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pg, err := postgres.Run(ctx, "postgres:18-trixie",
		postgres.WithDatabase("badblock"), postgres.WithUsername("postgres"), postgres.WithPassword("pg"),
		postgres.BasicWaitStrategies())
	testcontainers.CleanupContainer(t, pg)
	if err != nil {
		t.Fatal(err)
	}
	url, _ := pg.ConnectionString(ctx, "sslmode=disable")
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	for _, dir := range []string{"central", "rdns"} {
		files, _ := filepath.Glob(filepath.Join("..", "..", "..", "..", "..", "database", "postgres", dir, "*.sql"))
		for _, f := range files {
			raw, _ := os.ReadFile(f)
			up, _, _ := strings.Cut(string(raw), "-- migrate:down")
			if _, err := admin.Exec(ctx, up); err != nil {
				t.Fatalf("%s: %v", f, err)
			}
		}
	}
	if _, err := admin.Exec(ctx, "INSERT INTO rdns_prefix (prefix) VALUES ($1::cidr)", prefix.String()); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	id := worker.NewUUID()
	if err := st.Heartbeat(ctx, store.Worker{UUID: id, Host: "test-real", Version: "test", RateLimit: 64}); err != nil {
		t.Fatal(err)
	}

	lim := limiter.New(64)
	go lim.Run(ctx)
	res := resolver.New(resolver.Options{Servers: servers, Timeout: 2 * time.Second, Retries: 2, Inflight: 512,
		Limiter: lim, Log: slog.Default()})
	sc := New(st, res, lim, Options{Worker: id, Lease: 2 * time.Minute, Interval: func() time.Duration { return 24 * time.Hour }},
		slog.Default())

	start := time.Now()
	done := make(chan struct{})
	go func() { sc.Run(ctx); close(done) }()
	for sc.Prefixes() == 0 && time.Since(start) < 3*time.Minute {
		time.Sleep(100 * time.Millisecond)
	}
	elapsed := time.Since(start)
	cancel()
	<-done
	if sc.Prefixes() != 1 {
		t.Fatalf("o prefixo não foi varrido em %v", elapsed)
	}

	var ms, ptr, noPTR, failed, changed int
	var size int64
	_ = admin.QueryRow(context.Background(), `SELECT last_scan_ms, last_scan_ptr, last_scan_no_ptr, last_scan_failed, last_scan_changed
		FROM rdns_prefix`).Scan(&ms, &ptr, &noPTR, &failed, &changed)
	_ = admin.QueryRow(context.Background(), "SELECT pg_total_relation_size('rdns_ptr')").Scan(&size)
	addrs := 1 << (32 - prefix.Bits())
	t.Logf("prefixo %s (%d endereços) por %s: %d ms de varredura (%.1f endereços/s), %d consultas (%.2f por endereço)",
		prefix, addrs, serversEnv, ms, float64(addrs)/(float64(ms)/1000), res.Queries(), float64(res.Queries())/float64(addrs))
	t.Logf("ptr %d (%.0f%%), no_ptr %d (%.0f%%), falhas %d, linhas gravadas %d, rdns_ptr %d bytes",
		ptr, 100*float64(ptr)/float64(addrs), noPTR, 100*float64(noPTR)/float64(addrs), failed, changed, size)
}
