//go:build integration

// Package testdb sobe um PostgreSQL 18 descartável (testcontainers) com o
// mesmo bootstrap e as mesmas migrations de database/postgresql, para os testes
// de integração exercitarem o schema e as permissões reais.
package testdb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// Image é a imagem usada nos testes; a mesma do compose.
const Image = "postgres:18-trixie"

const (
	ownerPassword = "ownerpw"
	syncPassword  = "syncpw"
	apiPassword   = "apipw"
)

// DB são as URLs de conexão de cada role.
type DB struct {
	OwnerURL string
	SyncURL  string
	APIURL   string
}

// databaseDir localiza database/postgresql a partir deste arquivo.
func databaseDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "database", "postgresql")
}

// Start sobe o container, roda o bootstrap e aplica as migrations.
func Start(t *testing.T) DB {
	t.Helper()
	ctx := context.Background()
	dir := databaseDir()

	ctr, err := postgres.Run(ctx, Image,
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		postgres.WithDatabase("postgres"),
		testcontainers.WithEnv(map[string]string{
			"BADBLOCK_OWNER_PASSWORD": ownerPassword,
			"BADBLOCK_SYNC_PASSWORD":  syncPassword,
			"BADBLOCK_API_PASSWORD":   apiPassword,
		}),
		testcontainers.WithFiles(
			testcontainers.ContainerFile{
				HostFilePath:      filepath.Join(dir, "bootstrap", "00-bootstrap.sh"),
				ContainerFilePath: "/docker-entrypoint-initdb.d/00-bootstrap.sh",
				FileMode:          0o755,
			},
			testcontainers.ContainerFile{
				HostFilePath:      filepath.Join(dir, "bootstrap", "bootstrap.psql"),
				ContainerFilePath: "/docker-entrypoint-initdb.d/bootstrap.psql",
				FileMode:          0o644,
			},
		),
		postgres.BasicWaitStrategies(),
	)
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatalf("postgres: %v", err)
	}

	host, err := ctr.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := ctr.MappedPort(ctx, "5432/tcp")
	if err != nil {
		t.Fatal(err)
	}
	url := func(user, pw string) string {
		return fmt.Sprintf("postgres://%s:%s@%s:%s/badblock?sslmode=disable", user, pw, host, port.Port())
	}
	db := DB{
		OwnerURL: url("badblock_owner", ownerPassword),
		SyncURL:  url("badblock_sync", syncPassword),
		APIURL:   url("badblock_api", apiPassword),
	}
	if err := migrate(ctx, db.OwnerURL, filepath.Join(dir, "migrations")); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	return db
}

// migrate aplica a seção "-- migrate:up" de cada migration, em ordem, como o
// dbmate faz em produção.
func migrate(ctx context.Context, url, dir string) error {
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		up, _, _ := strings.Cut(string(raw), "-- migrate:down")
		up = strings.Replace(up, "-- migrate:up", "", 1)
		if _, err := conn.Exec(ctx, up); err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(f), err)
		}
	}
	return nil
}
