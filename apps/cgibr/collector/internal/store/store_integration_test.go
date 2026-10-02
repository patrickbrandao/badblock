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

	"github.com/patrickbrandao/badblock/apps/cgibr/collector/internal/parse"
)

// setup sobe um PG18, aplica as migrations de central/ e cgibr/ (seção
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

	for _, dir := range []string{"central", "cgibr"} {
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
	f, err := os.Open("../../testdata/nicbr-asn-blk-sample.txt")
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

func runFor(sha string) Run {
	return Run{StartedAt: time.Now(), URL: "https://example.test/f.txt", HTTPStatus: 200,
		ETag: `"e"`, SHA256: strings.Repeat(sha, 64), Bytes: 100, ASNs: 1}
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

func TestApplyLifecycle(t *testing.T) {
	st, admin := setup(t)
	ctx := context.Background()
	opt := ApplyOptions{RemovalThreshold: 0.05}

	if last, err := st.LastApplied(ctx); err != nil || last != nil {
		t.Fatalf("banco vazio: last = %+v, err = %v", last, err)
	}

	// 1) Carga inicial.
	ds := sample(t)
	ch, v1, err := st.Apply(ctx, ds, runFor("a"), opt)
	if err != nil {
		t.Fatal(err)
	}
	if ch.ASNInserted != 11 || ch.PrefixInserted != 59 || ch.Total() != 70 {
		t.Errorf("carga inicial = %+v", ch)
	}
	if j := readJob(t, admin); j.lastSync == nil || j.lastCheck == nil || j.consolidated != 0 {
		t.Errorf("jobs depois da carga = %+v", j)
	}
	var v4, v6 int
	_ = admin.QueryRow(ctx, "SELECT count(*) FILTER (WHERE family = 4), count(*) FILTER (WHERE family = 6) FROM cgibr_prefix").Scan(&v4, &v6)
	if v4 != 52 || v6 != 7 {
		t.Errorf("família = %d/%d", v4, v6)
	}
	var digits string
	_ = admin.QueryRow(ctx, "SELECT document_digits FROM cgibr_asn WHERE asn = 61610").Scan(&digits)
	if digits != "35980592000130" {
		t.Errorf("document_digits = %q", digits)
	}

	// 2) A consolidação (fase 2) marca 1; o mesmo conteúdo não muda nada.
	if _, err := admin.Exec(ctx, "UPDATE jobs SET consolidated = 1"); err != nil {
		t.Fatal(err)
	}
	ch, v2, err := st.Apply(ctx, sample(t), runFor("b"), opt)
	if err != nil {
		t.Fatal(err)
	}
	if ch.Total() != 0 || v2 == v1 {
		t.Errorf("reaplicação = %+v, versões %s/%s", ch, v1, v2)
	}
	if j := readJob(t, admin); j.consolidated != 1 {
		t.Errorf("sem mudança a flag deve continuar 1: %+v", j)
	}

	// 3) Mudanças: nome novo, bloco trocando de ASN, ASN removido, ASN novo.
	ds = sample(t)
	var kept []parse.ASN
	moved := netip.MustParsePrefix("187.87.28.0/22")
	for _, a := range ds.ASNs {
		switch a.Number {
		case 6505: // sai da fonte
			continue
		case 61610:
			a.Name = "ELEA Nova"
			a.Prefixes = a.Prefixes[1:] // 187.87.28.0/22 vai para o AS64500
		}
		kept = append(kept, a)
	}
	kept = append(kept, parse.ASN{Number: 64500, Name: "Novo", Document: "11.111.111/0001-11",
		Prefixes: []netip.Prefix{moved, netip.MustParsePrefix("2001:db8::/32")}})
	ds.ASNs = kept
	ch, _, err = st.Apply(ctx, ds, runFor("c"), ApplyOptions{RemovalThreshold: 0.2})
	if err != nil {
		t.Fatal(err)
	}
	want := Changes{ASNInserted: 1, ASNUpdated: 1, ASNDeleted: 1, PrefixInserted: 1, PrefixUpdated: 1}
	if ch != want {
		t.Errorf("mudanças = %+v, quero %+v", ch, want)
	}
	if j := readJob(t, admin); j.consolidated != 0 {
		t.Errorf("com mudança a flag volta a 0: %+v", j)
	}
	var owner int64
	_ = admin.QueryRow(ctx, `SELECT a.asn FROM cgibr_prefix p JOIN cgibr_asn a ON a.uuid = p.asn_uuid
		WHERE p.prefix = '187.87.28.0/22'`).Scan(&owner)
	if owner != 64500 {
		t.Errorf("dono do bloco movido = AS%d", owner)
	}

	// 4) Trava de remoção em massa, e --force.
	small := &parse.Dataset{ASNs: ds.ASNs[:2]}
	_, _, err = st.Apply(ctx, small, runFor("d"), opt)
	if _, ok := errors.AsType[*RemovalError](err); !ok {
		t.Fatalf("esperava RemovalError, veio %v", err)
	}
	var n int
	_ = admin.QueryRow(ctx, "SELECT count(*) FROM cgibr_asn").Scan(&n)
	if n != 11 {
		t.Errorf("recusa não pode mexer nas tabelas: %d ASNs", n)
	}
	ch, _, err = st.Apply(ctx, small, runFor("e"), ApplyOptions{RemovalThreshold: 0.05, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if ch.ASNDeleted != 9 {
		t.Errorf("com --force = %+v", ch)
	}

	// 5) Falhas ficam em cgibr_run e não viram versão.
	if err := st.RecordFailure(ctx, Run{StartedAt: time.Now(), URL: "https://example.test/f.txt"}, errors.New("boom")); err != nil {
		t.Fatal(err)
	}
	last, err := st.LastApplied(ctx)
	if err != nil || last == nil || last.SHA256 != strings.Repeat("e", 64) || last.ETag != `"e"` {
		t.Errorf("LastApplied = %+v, err = %v", last, err)
	}
	var failed int
	_ = admin.QueryRow(ctx, "SELECT count(*) FROM cgibr_run WHERE status = 0 AND error = 'boom'").Scan(&failed)
	if failed != 1 {
		t.Errorf("falhas gravadas = %d", failed)
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
