//go:build integration

package dataset_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/patrickbrandao/badblock/apps/registry-api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/registry-api/internal/store"
	"github.com/patrickbrandao/badblock/apps/registry-api/internal/testdb"
)

// Uma versão nova publicada com NOTIFY chega à API sem esperar o poll.
func TestWatcherFollowsNotify(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	db := testdb.Start(t)
	testdb.Seed(t, db, "../../testdata/seed.sql")

	st, err := store.Open(ctx, db.APIURL, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	w := dataset.NewWatcher(st, time.Hour, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := w.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if v := w.Current().Version; v != 1 {
		t.Fatalf("versão inicial = %d", v)
	}
	if _, ok := w.Current().Exceptions[mustPrefix("192.0.0.0/24")]; !ok {
		t.Error("exceções de cache não carregadas")
	}
	go w.Run(ctx)
	time.Sleep(500 * time.Millisecond) // tempo para o LISTEN

	owner, err := pgx.Connect(ctx, db.OwnerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close(ctx)
	var id int64
	if err := owner.QueryRow(ctx, `INSERT INTO registry.dataset (baseline) VALUES (false) RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Exec(ctx, `SELECT pg_notify('badblock_dataset', $1::bigint::text)`, id); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if w.Current().Version == id {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("a versão não mudou para %d (ficou %d)", id, w.Current().Version)
}
