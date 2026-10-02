//go:build integration

package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/patrickbrandao/badblock/apps/rootanchors/collector/internal/parse"
)

// setup sobe um PG18, aplica as migrations de central/ e rootanchors/ (seção
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

	for _, dir := range []string{"central", "rootanchors"} {
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

func sample(t *testing.T) *parse.Dataset {
	t.Helper()
	f, err := os.Open("../../testdata/root-anchors.xml")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ds, err := parse.Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	return ds
}

func runFor(sha string, ds *parse.Dataset) Run {
	return Run{StartedAt: time.Now(), URL: "https://example.test/root-anchors.xml", HTTPStatus: 200,
		ETag: `W/"e"`, LastModified: "Tue, 05 Nov 2024 19:23:41 GMT", SHA256: strings.Repeat(sha, 64), Bytes: 1861,
		Parsed: true, AnchorID: ds.AnchorID, AnchorSource: ds.Source, Zone: ds.Zone, Keys: len(ds.Keys)}
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

func count(t *testing.T, admin *pgxpool.Pool, sql string) int {
	t.Helper()
	var n int
	if err := admin.QueryRow(context.Background(), sql).Scan(&n); err != nil {
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

	// 1) Carga inicial: as 3 chaves do arquivo real.
	ds := sample(t)
	ch, v1, err := st.Apply(ctx, ds, runFor("a", ds), opt)
	if err != nil {
		t.Fatal(err)
	}
	if ch != (Changes{KeyInserted: 3}) {
		t.Errorf("carga inicial = %+v", ch)
	}
	if j := readJob(t, admin); j.lastSync == nil || j.lastCheck == nil || j.consolidated != 0 {
		t.Errorf("jobs depois da carga = %+v", j)
	}
	// A 19036 só tem o DS; as outras têm o DNSKEY.
	if n := count(t, admin, `SELECT count(*) FROM rootanchors_key WHERE key_id = 'Kjqmt7v' AND key_tag = 19036
		AND public_key IS NULL AND flags IS NULL AND valid_until = '2019-01-11T00:00:00Z' AND valid_from = '2010-07-15T00:00:00Z'`); n != 1 {
		t.Errorf("19036 não bate")
	}
	if n := count(t, admin, `SELECT count(*) FROM rootanchors_key WHERE key_tag IN (20326, 38696)
		AND flags = 257 AND public_key LIKE 'AwEAA%' AND valid_until IS NULL AND algorithm = 8 AND digest_type = 2
		AND length(digest) = 64`); n != 2 {
		t.Errorf("20326/38696: %d linhas", n)
	}
	var anchor, zone string
	var keys int
	_ = admin.QueryRow(ctx, "SELECT anchor_id, zone, keys FROM rootanchors_run WHERE uuid = $1::uuid", v1).Scan(&anchor, &zone, &keys)
	if anchor != "0C05FDD6-422C-4910-8ED6-430ED15E11C2" || zone != "." || keys != 3 {
		t.Errorf("run = %q %q %d", anchor, zone, keys)
	}

	// 2) A consolidação (fase 2) marca 1; o mesmo conteúdo não muda nada.
	if _, err := admin.Exec(ctx, "UPDATE jobs SET consolidated = 1"); err != nil {
		t.Fatal(err)
	}
	var upd1 time.Time
	_ = admin.QueryRow(ctx, "SELECT max(updated_at) FROM rootanchors_key").Scan(&upd1)
	ch, v2, err := st.Apply(ctx, sample(t), runFor("b", ds), opt)
	if err != nil {
		t.Fatal(err)
	}
	if ch.Total() != 0 || v2 == v1 {
		t.Errorf("reaplicação = %+v, versões %s/%s", ch, v1, v2)
	}
	if j := readJob(t, admin); j.consolidated != 1 {
		t.Errorf("sem mudança a flag deve continuar 1: %+v", j)
	}
	var upd2 time.Time
	_ = admin.QueryRow(ctx, "SELECT max(updated_at) FROM rootanchors_key").Scan(&upd2)
	if !upd2.Equal(upd1) {
		t.Errorf("updated_at mudou sem mudança: %v → %v", upd1, upd2)
	}

	// 3) Uma chave nova (rolagem) e a 20326 aposentada: 1 inserida, 1 alterada.
	ds = sample(t)
	until := time.Date(2027, 1, 11, 0, 0, 0, 0, time.UTC)
	ds.Keys[1].ValidUntil = &until
	ds.Keys = append(ds.Keys, parse.Key{ID: "Knovakey", KeyTag: 12345, Algorithm: 13, DigestType: 2,
		Digest: strings.Repeat("AB", 32), ValidFrom: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)})
	ch, _, err = st.Apply(ctx, ds, runFor("c", ds), opt)
	if err != nil {
		t.Fatal(err)
	}
	if ch != (Changes{KeyInserted: 1, KeyUpdated: 1}) {
		t.Errorf("mudanças = %+v", ch)
	}
	if j := readJob(t, admin); j.consolidated != 0 {
		t.Errorf("com mudança a flag volta a 0: %+v", j)
	}
	if n := count(t, admin, "SELECT count(*) FROM rootanchors_key WHERE key_tag = 20326 AND valid_until = '2027-01-11T00:00:00Z' AND updated_at > created_at"); n != 1 {
		t.Errorf("20326 não foi aposentada")
	}

	// 4) Trava de remoção: sumir 1 de 4 chaves (25%) é recusado; com --force, sai.
	small := &parse.Dataset{AnchorID: ds.AnchorID, Source: ds.Source, Zone: ".", Keys: ds.Keys[1:]}
	_, _, err = st.Apply(ctx, small, runFor("d", small), opt)
	var rem *RemovalError
	if !errors.As(err, &rem) || rem.Removed != 1 || rem.Current != 4 {
		t.Fatalf("esperava RemovalError 1 de 4, veio %v", err)
	}
	if n := count(t, admin, "SELECT count(*) FROM rootanchors_key"); n != 4 {
		t.Errorf("recusa não pode mexer nas tabelas: %d chaves", n)
	}
	ch, _, err = st.Apply(ctx, small, runFor("e", small), ApplyOptions{RemovalThreshold: 0.05, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if ch != (Changes{KeyDeleted: 1}) {
		t.Errorf("com --force = %+v", ch)
	}

	// 5) Falhas ficam em rootanchors_run e não viram versão.
	if err := st.RecordFailure(ctx, Run{StartedAt: time.Now(), URL: "https://example.test/root-anchors.xml",
		HTTPStatus: 200, SHA256: strings.Repeat("f", 64), Bytes: 10}, errors.New("parser: boom")); err != nil {
		t.Fatal(err)
	}
	last, err := st.LastApplied(ctx)
	if err != nil || last == nil || last.SHA256 != strings.Repeat("e", 64) || last.ETag != `W/"e"` || last.LastModified == "" {
		t.Errorf("LastApplied = %+v, err = %v", last, err)
	}
	if n := count(t, admin, `SELECT count(*) FROM rootanchors_run WHERE status = 0 AND error = 'parser: boom'
		AND anchor_id IS NULL AND zone IS NULL AND keys IS NULL AND key_inserted IS NULL`); n != 1 {
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
	conn, err := admin.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(hashtext($1))", AppName); err != nil {
		t.Fatal(err)
	}
	ds := sample(t)
	if _, _, err := st.Apply(ctx, ds, runFor("a", ds), ApplyOptions{RemovalThreshold: 0.05}); !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v", err)
	}
	if n := count(t, admin, "SELECT count(*) FROM rootanchors_run"); n != 0 {
		t.Errorf("ErrBusy não grava execução: %d", n)
	}
}

func TestTouchCheckCreatesJob(t *testing.T) {
	st, admin := setup(t)
	if err := st.TouchCheck(context.Background()); err != nil {
		t.Fatal(err)
	}
	if j := readJob(t, admin); j.lastSync != nil || j.lastCheck == nil || j.consolidated != 0 {
		t.Errorf("jobs = %+v", j)
	}
}
