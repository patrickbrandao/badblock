//go:build integration

package scan

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/limiter"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/parse"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/rdap"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/reprocess"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/store"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/worker"
)

// setupPostgres sobe um PG18 com as migrations de dirs.
func setupPostgres(t *testing.T, dirs ...string) (string, *pgxpool.Pool) {
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
	for _, dir := range dirs {
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
	return url, admin
}

// TestEndToEnd: servidor RDAP falso com as fixtures, a fila de verdade, o
// processamento e o reprocessamento sem consultar de novo.
func TestEndToEnd(t *testing.T) {
	url, admin := setupPostgres(t, "central", "registrobr_rdapnum")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		files := map[string]string{
			"/autnum/61610":       "autnum-61610.json",
			"/autnum/22548":       "autnum-22548.json",
			"/ip/200.225.48.0/24": "ip-200.225.48.0-21.json",
			"/ip/2804:8ae0::/32":  "ip-2804-8ae0--32.json",
			"/autnum/64511":       "autnum-61610-truncated.json",
		}
		name, ok := files[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		b, _ := os.ReadFile(filepath.Join("..", "..", "testdata", name))
		w.Header().Set("Content-Type", "application/rdap+json")
		_, _ = w.Write(b)
	}))
	defer srv.Close()

	for _, sql := range []string{
		"INSERT INTO registrobr_rdapnum_target (kind, asn) VALUES ('asn', 61610), ('asn', 22548), ('asn', 64496), ('asn', 64511)",
		"INSERT INTO registrobr_rdapnum_target (kind, prefix) VALUES ('prefix', '200.225.48.0/24'), ('prefix', '2804:8ae0::/32')",
	} {
		if _, err := admin.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	st, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	id := worker.NewUUID()
	if err := st.Heartbeat(ctx, store.Worker{UUID: id, Host: "t", Version: "test", RateLimit: 6000}); err != nil {
		t.Fatal(err)
	}
	lim := limiter.New(6000)
	client := rdap.New(rdap.Options{BaseURL: srv.URL, Timeout: 2 * time.Second, UserAgent: "test"})
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	sc := New(st, client, lim, Options{Worker: id, Lease: time.Minute, Interval: func() time.Duration { return time.Hour },
		Idle: 50 * time.Millisecond}, log)
	done := make(chan struct{})
	go func() { sc.Run(ctx); close(done) }()

	deadline := time.Now().Add(30 * time.Second)
	for {
		var pending int
		_ = admin.QueryRow(ctx, "SELECT count(*) FROM registrobr_rdapnum_target WHERE last_attempt_at IS NULL").Scan(&pending)
		if pending == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("faltam %d alvos", pending)
		}
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	<-done

	check := func(sql string, want int) {
		t.Helper()
		var n int
		if err := admin.QueryRow(context.Background(), sql).Scan(&n); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		if n != want {
			t.Errorf("%s = %d, quero %d", sql, n, want)
		}
	}
	check("SELECT count(*) FROM registrobr_rdapnum_target WHERE last_result = 1", 4)
	check("SELECT count(*) FROM registrobr_rdapnum_target WHERE last_result = 3 AND asn = 64496", 1)
	check("SELECT count(*) FROM registrobr_rdapnum_target WHERE last_result = 2 AND asn = 64511 AND response_uuid IS NULL AND retry_at IS NOT NULL", 1)
	check("SELECT count(*) FROM registrobr_rdapnum_autnum", 2)
	check("SELECT count(*) FROM registrobr_rdapnum_network", 2)
	check("SELECT count(*) FROM registrobr_rdapnum_reverse", 2)
	check("SELECT count(*) FROM registrobr_rdapnum_response WHERE result IN (1, 3) AND parser_version IS NULL", 0)
	check("SELECT count(*) FROM jobs WHERE app = 'collector-registrobr-rdapnum' AND last_sync_at IS NOT NULL", 1)
	var stats store.Worker
	sc.Stats(&stats)
	if stats.Requests != 6 || stats.Complete != 4 || stats.Truncated != 1 || stats.NotFound != 1 || stats.EgressIPs != 1 {
		t.Errorf("contadores = %+v", stats)
	}

	// Subir o PARSER_VERSION refaz tudo a partir das cruas, sem consultar.
	before := requests.Load()
	if _, err := admin.Exec(context.Background(), "UPDATE registrobr_rdapnum_response SET parser_version = $1 - 1", parse.Version); err != nil {
		t.Fatal(err)
	}
	n, err := reprocess.New(st, log).Once(context.Background())
	if err != nil || n != 5 {
		t.Errorf("reprocessadas = %d %v", n, err)
	}
	if requests.Load() != before {
		t.Error("o reprocessamento consultou o servidor")
	}
	check("SELECT count(*) FROM registrobr_rdapnum_response WHERE result IN (1, 3) AND parser_version = "+
		"(SELECT max(parser_version) FROM registrobr_rdapnum_response)", 5)
	if n, _ := reprocess.New(st, log).Once(context.Background()); n != 0 {
		t.Errorf("segunda volta = %d", n)
	}
}
