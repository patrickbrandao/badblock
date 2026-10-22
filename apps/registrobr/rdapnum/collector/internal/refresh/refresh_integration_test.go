//go:build integration

package refresh

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/store"
)

func TestRefreshQueue(t *testing.T) {
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
	for _, dir := range []string{"central", "registrobr_rdapnum"} {
		files, _ := filepath.Glob(filepath.Join("..", "..", "..", "..", "..", "..", "database", "postgres", dir, "*.sql"))
		for _, f := range files {
			raw, _ := os.ReadFile(f)
			up, _, _ := strings.Cut(string(raw), "-- migrate:down")
			if _, err := admin.Exec(ctx, up); err != nil {
				t.Fatalf("%s: %v", f, err)
			}
		}
	}
	if _, err := admin.Exec(ctx, `INSERT INTO registrobr_rdapnum_target (kind, asn) VALUES ('asn', 61610);
		INSERT INTO registrobr_rdapnum_target (kind, prefix) VALUES ('prefix', '200.225.48.0/21')`); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	vk, err := testcontainers.Run(ctx, "valkey/valkey:9-alpine",
		testcontainers.WithExposedPorts("6379/tcp"),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("6379/tcp")))
	testcontainers.CleanupContainer(t, vk)
	if err != nil {
		t.Fatal(err)
	}
	ep, err := vk.PortEndpoint(ctx, "6379/tcp", "")
	if err != nil {
		t.Fatal(err)
	}
	rdb, err := NewClient("redis://" + ep + "/0")
	if err != nil {
		t.Fatal(err)
	}
	defer rdb.Close()

	loop := New(rdb, st, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	done := make(chan struct{})
	go func() { loop.Run(ctx); close(done) }()

	for _, msg := range []string{
		`{"kind":"asn","target":"64496","at":"2026-10-21T21:10:00Z"}`, // inexistente: ignorado
		`lixo`, // inválido: descartado
		`{"kind":"asn","target":"61610","at":"2026-10-21T21:10:00.123Z"}`,
		`{"kind":"prefix","target":"200.225.48.0/21","at":"2026-10-21T21:10:01Z"}`,
	} {
		if err := rdb.RPush(ctx, QueueKey, msg).Err(); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(15 * time.Second)
	for loop.Served() < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
	if loop.Served() != 2 {
		t.Errorf("atendidos = %d", loop.Served())
	}
	var n int
	_ = admin.QueryRow(context.Background(), "SELECT count(*) FROM registrobr_rdapnum_target WHERE rescan").Scan(&n)
	if n != 2 {
		t.Errorf("rescan = %d", n)
	}
	if l, _ := rdb.LLen(context.Background(), QueueKey).Result(); l != 0 {
		t.Errorf("sobraram %d na fila", l)
	}
}
