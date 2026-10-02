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

	"github.com/patrickbrandao/badblock/apps/iana/collector/internal/parse"
	"github.com/patrickbrandao/badblock/apps/iana/collector/internal/source"
)

// setup sobe um PG18, aplica as migrations de central/ e iana/ (seção
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

	for _, dir := range []string{"central", "iana"} {
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

func loadDataset(t *testing.T, dir string) *parse.Dataset {
	t.Helper()
	files := map[string][]byte{}
	for _, f := range source.Files {
		b, err := os.ReadFile(filepath.Join(dir, f.Basename()))
		if err != nil {
			t.Fatal(err)
		}
		files[f.Name] = b
	}
	ds, err := parse.Parse(files)
	if err != nil {
		t.Fatal(err)
	}
	return ds
}

func sample(t *testing.T) *parse.Dataset { return loadDataset(t, "../../testdata") }

func runFor(sha string) Run {
	rows := 1
	files := make([]File, len(source.Files))
	for i, f := range source.Files {
		files[i] = File{Name: f.Name, URL: "https://example.test/" + f.Path, HTTPStatus: 200,
			ETag: `"` + sha + `"`, LastModified: "Sat, 19 Sep 2026 00:44:44 GMT",
			SHA256: strings.Repeat(sha, 64), Bytes: 100, Changed: true, Rows: &rows}
	}
	files[7].Publication = "2026-06-01T20:00:01Z"
	return Run{StartedAt: time.Now(), SHA256: strings.Repeat(sha, 64), Files: files}
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

func count(t *testing.T, admin *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := admin.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
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
	ch, v1, err := st.Apply(ctx, sample(t), runFor("a"), opt)
	if err != nil {
		t.Fatal(err)
	}
	want := Changes{
		"iana_asn_block":      {Inserted: 21},
		"iana_prefix_block":   {Inserted: 30},
		"iana_special_prefix": {Inserted: 17},
		"iana_special_asn":    {Inserted: 5},
		"iana_rdap_service":   {Inserted: 43},
	}
	if !equalChanges(ch, want) || ch.Total() != 116 {
		t.Errorf("carga inicial = %+v", ch)
	}
	if j := readJob(t, admin); j.lastSync == nil || j.lastCheck == nil || j.consolidated != 0 {
		t.Errorf("jobs depois da carga = %+v", j)
	}

	// Conteúdo: forma canônica, arrays, NULLs e colunas calculadas.
	var registry, status, note string
	var family int
	var urls []string
	if err := admin.QueryRow(ctx, `SELECT registry, status, family, rdap_urls FROM iana_prefix_block
		WHERE prefix >>= '45.171.60.1'::inet`).Scan(&registry, &status, &family, &urls); err != nil {
		t.Fatal(err)
	}
	if registry != "arin" || status != "LEGACY" || family != 4 || len(urls) != 2 || urls[0] != "https://rdap.arin.net/registry" {
		t.Errorf("45/8 = %s %s %d %v", registry, status, family, urls)
	}
	if err := admin.QueryRow(ctx, `SELECT note FROM iana_prefix_block WHERE prefix = '0.0.0.0/8'`).Scan(&note); err != nil || note != "[2][3]" {
		t.Errorf("000/8 note = %q, %v", note, err)
	}
	if n := count(t, admin, `SELECT count(*) FROM iana_prefix_block WHERE registry IS NULL AND rdap_urls = '{}'`); n != 11 {
		t.Errorf("blocos sem RIR = %d", n)
	}
	if n := count(t, admin, `SELECT count(*) FROM iana_special_prefix
		WHERE prefix >>= '192.0.0.170'::inet AND globally_reachable = false`); n != 2 { // /24 e /32
		t.Errorf("special de 192.0.0.170 = %d", n)
	}
	if n := count(t, admin, `SELECT count(*) FROM iana_special_prefix
		WHERE prefix = '192.88.99.0/24' AND source IS NULL AND termination_date = '2015-03'`); n != 1 {
		t.Error("192.88.99.0/24 deveria ter flags NULL e termination_date")
	}
	if n := count(t, admin, `SELECT count(*) FROM iana_rdap_service
		WHERE kind = 'asn' AND asn_start <= 2000 AND asn_end >= 2000 AND prefix IS NULL AND registry = 'arin'`); n != 1 {
		t.Errorf("RDAP do AS2000 = %d", n)
	}
	if n := count(t, admin, `SELECT count(*) FROM iana_asn_block
		WHERE asn_start <= 23456 AND asn_end >= 23456 AND registry IS NULL AND description = 'AS_TRANS'`); n != 1 {
		t.Errorf("AS_TRANS = %d", n)
	}

	last, err := st.LastApplied(ctx)
	if err != nil || last == nil || last.Version != v1 || last.SHA256 != strings.Repeat("a", 64) || len(last.Files) != 10 {
		t.Fatalf("LastApplied = %+v, err = %v", last, err)
	}
	if f, ok := last.File(source.RDAPASN); !ok || f.ETag != `"a"` || f.Publication != "2026-06-01T20:00:01Z" || f.Rows == nil {
		t.Errorf("files[rdap-asn] = %+v", f)
	}

	// 2) A consolidação (fase 2) marca 1; o mesmo conteúdo não muda nada.
	if _, err := admin.Exec(ctx, "UPDATE jobs SET consolidated = 1"); err != nil {
		t.Fatal(err)
	}
	ch, v2, err := st.Apply(ctx, sample(t), runFor("b"), opt)
	if err != nil {
		t.Fatal(err)
	}
	if ch.Total() != 0 || v2 == v1 || len(ch) != 5 {
		t.Errorf("reaplicação = %+v, versões %s/%s", ch, v1, v2)
	}
	if j := readJob(t, admin); j.consolidated != 1 {
		t.Errorf("sem mudança a flag deve continuar 1: %+v", j)
	}

	// 3) Mudanças: texto alterado, bloco removido, bloco novo, URL RDAP nova.
	ds := sample(t)
	ds.SpecialASNs[0].Reason = "Reservado"
	ds.PrefixBlocks = ds.PrefixBlocks[:len(ds.PrefixBlocks)-1] // 3fff::/20 some
	ds.SpecialPrefixes = append(ds.SpecialPrefixes, parse.SpecialPrefix{
		Prefix: netip.MustParsePrefix("198.18.0.0/15"), Name: "Benchmarking", AllocationDate: "1999-03"})
	ds.RDAPServices[0].URLs = []string{"https://rdap.afrinic.net/rdap/"}
	ch, _, err = st.Apply(ctx, ds, runFor("c"), opt)
	if err != nil {
		t.Fatal(err)
	}
	want = Changes{
		"iana_prefix_block":   {Deleted: 1},
		"iana_special_prefix": {Inserted: 1},
		"iana_special_asn":    {Updated: 1},
		"iana_rdap_service":   {Updated: 1},
	}
	if !equalChanges(ch, want) {
		t.Errorf("mudanças = %+v, quero %+v", ch, want)
	}
	if j := readJob(t, admin); j.consolidated != 0 {
		t.Errorf("com mudança a flag volta a 0: %+v", j)
	}
	if n := count(t, admin, `SELECT count(*) FROM iana_special_asn WHERE reason = 'Reservado' AND updated_at > created_at`); n != 1 {
		t.Error("UPDATE deveria acionar trg_iana_special_asn_updated_at")
	}

	// 4) Trava de remoção: remover até RemovalMinRows linhas sempre passa...
	ds = sample(t)
	ds.SpecialASNs = ds.SpecialASNs[:3] // 2 de 5 (40%)
	if _, _, err := st.Apply(ctx, ds, runFor("d"), opt); err != nil {
		t.Fatalf("remoção de 2 linhas deveria passar: %v", err)
	}
	// ... acima disso vale o limite, e a recusa não mexe em nada.
	ds = sample(t)
	ds.ASNBlocks = ds.ASNBlocks[:10] // 11 de 21
	_, _, err = st.Apply(ctx, ds, runFor("e"), opt)
	var rem *RemovalError
	if !errors.As(err, &rem) || rem.Table != "iana_asn_block" || rem.Removed != 11 || rem.Current != 21 {
		t.Fatalf("esperava RemovalError em iana_asn_block, veio %v", err)
	}
	if n := count(t, admin, "SELECT count(*) FROM iana_asn_block"); n != 21 {
		t.Errorf("recusa não pode mexer nas tabelas: %d faixas", n)
	}
	ch, _, err = st.Apply(ctx, ds, runFor("f"), ApplyOptions{RemovalThreshold: 0.05, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if ch["iana_asn_block"].Deleted != 11 || ch["iana_special_asn"].Inserted != 2 {
		t.Errorf("com --force = %+v", ch)
	}

	// 5) Falhas ficam em iana_run e não viram versão.
	failed := runFor("9")
	failed.Files[0].Rows = nil
	if err := st.RecordFailure(ctx, failed, errors.New("boom")); err != nil {
		t.Fatal(err)
	}
	last, err = st.LastApplied(ctx)
	if err != nil || last == nil || last.SHA256 != strings.Repeat("f", 64) {
		t.Errorf("LastApplied = %+v, err = %v", last, err)
	}
	if n := count(t, admin, `SELECT count(*) FROM iana_run WHERE status = 0 AND error = 'boom' AND changes IS NULL
		AND jsonb_array_length(files) = 10 AND NOT files->0 ? 'rows'`); n != 1 {
		t.Errorf("falhas gravadas = %d", n)
	}
	var changes string
	_ = admin.QueryRow(ctx, `SELECT changes::text FROM iana_run WHERE status = 1 ORDER BY created_at DESC LIMIT 1`).Scan(&changes)
	if !strings.Contains(changes, `"iana_asn_block": {"deleted": 11`) {
		t.Errorf("iana_run.changes = %s", changes)
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
	if _, _, err := st.Apply(ctx, sample(t), runFor("a"), ApplyOptions{RemovalThreshold: 0.05}); !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v, quero ErrBusy", err)
	}
	if n := count(t, admin, "SELECT count(*) FROM iana_run"); n != 0 {
		t.Errorf("iana_run = %d linhas", n)
	}
}

// TestRealFilesApply aplica os arquivos reais inteiros e reaplica (tem que
// dar zero mudanças). Opcional: IANA_REAL_DIR (make test-real).
func TestRealFilesApply(t *testing.T) {
	dir := os.Getenv("IANA_REAL_DIR")
	if dir == "" {
		t.Skip("IANA_REAL_DIR não definida")
	}
	ds := loadDataset(t, dir)
	if err := parse.Check(ds, parse.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	st, admin := setup(t)
	ctx := context.Background()
	opt := ApplyOptions{RemovalThreshold: 0.05}

	ch, _, err := st.Apply(ctx, ds, runFor("a"), opt)
	if err != nil {
		t.Fatal(err)
	}
	wantRows := map[string]int{
		"iana_asn_block": len(ds.ASNBlocks), "iana_prefix_block": len(ds.PrefixBlocks),
		"iana_special_prefix": len(ds.SpecialPrefixes), "iana_special_asn": len(ds.SpecialASNs),
		"iana_rdap_service": len(ds.RDAPServices),
	}
	for table, n := range wantRows {
		if ch[table].Inserted != n || count(t, admin, "SELECT count(*) FROM "+table) != n {
			t.Errorf("%s: inseridas %d, quero %d", table, ch[table].Inserted, n)
		}
	}
	t.Logf("carga real: %+v", ch)

	ch, _, err = st.Apply(ctx, loadDataset(t, dir), runFor("b"), opt)
	if err != nil {
		t.Fatal(err)
	}
	if ch.Total() != 0 {
		t.Errorf("reaplicação deveria ser idempotente: %+v", ch)
	}

	// As consultas que a api-iana vai fazer.
	var registry, desc string
	if err := admin.QueryRow(ctx, `SELECT registry, description FROM iana_asn_block
		WHERE asn_start <= 61613 ORDER BY asn_start DESC LIMIT 1`).Scan(&registry, &desc); err != nil || registry != "lacnic" {
		t.Errorf("AS61613 → %s %q, %v", registry, desc, err)
	}
	if err := admin.QueryRow(ctx, `SELECT registry FROM iana_prefix_block
		WHERE prefix >>= '2804:5964::1'::inet ORDER BY masklen(prefix) DESC LIMIT 1`).Scan(&registry); err != nil || registry != "lacnic" {
		t.Errorf("2804:5964::1 → %s, %v", registry, err)
	}
	if err := admin.QueryRow(ctx, `SELECT registry FROM iana_rdap_service
		WHERE kind = 'ipv4' AND prefix >>= '45.171.60.1'::inet ORDER BY masklen(prefix) DESC LIMIT 1`).Scan(&registry); err != nil || registry != "arin" {
		t.Errorf("RDAP 45.171.60.1 → %s, %v", registry, err)
	}
	if n := count(t, admin, `SELECT count(*) FROM iana_special_prefix WHERE prefix >>= '100.64.1.1'::inet AND globally_reachable = false`); n != 1 {
		t.Errorf("CGNAT como bogon = %d", n)
	}
	if n := count(t, admin, `SELECT count(*) FROM iana_special_asn WHERE asn_start <= 4200000001 AND asn_end >= 4200000001`); n != 1 {
		t.Errorf("ASN privado de 32 bits = %d", n)
	}
}

func equalChanges(got, want Changes) bool {
	for _, t := range Tables {
		if got[t] != want[t] {
			return false
		}
	}
	return len(got) == len(Tables)
}
