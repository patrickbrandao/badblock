//go:build integration

package store

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

const seed = `
INSERT INTO cgibr_asn (asn, name, document) VALUES
    (61613,  'TMSoft Solucoes em Informatica Ltda', '08.030.063/0001-00'),
    (262287, 'Latitude.sh LTDA',                    '06.043.809/0001-87'),
    (275689, 'Internet Systems Consortium',         '10996639'),
    (275690, 'Internet Systems Consortium',         '10996639');
INSERT INTO cgibr_prefix (prefix, asn_uuid)
    SELECT p::cidr, (SELECT uuid FROM cgibr_asn WHERE asn = 61613)
      FROM unnest(ARRAY['45.171.60.0/22', '200.192.152.0/22', '2804:5964::/32']) AS p;
INSERT INTO cgibr_prefix (prefix, asn_uuid)
    SELECT '45.171.62.0/24', uuid FROM cgibr_asn WHERE asn = 262287;
INSERT INTO cgibr_run (status, url, sha256, asns, prefixes_v4, prefixes_v6, started_at)
    VALUES (0, 'https://x/f.txt', NULL, NULL, NULL, NULL, NOW() - interval '2 hours'),
           (1, 'https://x/f.txt', repeat('a', 64), 4, 3, 1, NOW() - interval '1 hour');
INSERT INTO jobs (app, last_sync_at, last_check_at) VALUES ('collector-cgibr', NOW(), NOW());
`

// setup sobe um PG18, aplica as migrations de central/ e cgibr/, carrega um
// recorte de dados e devolve um Store conectado com o usuário postgres.
func setup(t *testing.T) *Store {
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
	defer admin.Close()

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
	if _, err := admin.Exec(ctx, seed); err != nil {
		t.Fatal(err)
	}

	st, err := Open(ctx, superURL, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	return st
}

func TestQueries(t *testing.T) {
	st := setup(t)
	ctx := context.Background()

	d, err := st.Dataset(ctx)
	if err != nil || d == nil || d.SHA256 != strings.Repeat("a", 64) || d.ASNs != 4 || d.PrefixesV6 != 1 {
		t.Fatalf("Dataset = %+v, err = %v", d, err)
	}
	j, err := st.Job(ctx)
	if err != nil || j == nil || j.LastSyncAt == nil || j.Consolidated != 0 {
		t.Errorf("Job = %+v, err = %v", j, err)
	}

	a, err := st.ASN(ctx, 61613)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range a.Prefixes {
		got = append(got, p.String())
	}
	if strings.Join(got, ",") != "45.171.60.0/22,200.192.152.0/22,2804:5964::/32" || a.DocumentDigits != "08030063000100" {
		t.Errorf("ASN = %+v, blocos %v", a, got)
	}
	if _, err := st.ASN(ctx, 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("ASN inexistente: %v", err)
	}

	// O /24 do AS262287 está dentro do /22 do AS61613: vale o mais específico.
	for q, want := range map[string]string{
		"45.171.62.10/32":    "45.171.62.0/24 AS262287",
		"45.171.61.1/32":     "45.171.60.0/22 AS61613",
		"45.171.60.0/23":     "45.171.60.0/22 AS61613",
		"2804:5964:1::1/128": "2804:5964::/32 AS61613",
	} {
		m, err := st.Covering(ctx, netip.MustParsePrefix(q))
		if err != nil {
			t.Errorf("%s: %v", q, err)
			continue
		}
		if s := m.Prefix.String() + " AS" + strconv.FormatInt(m.ASN.ASN, 10); s != want {
			t.Errorf("%s → %s, quero %s", q, s, want)
		}
	}
	if _, err := st.Covering(ctx, netip.MustParsePrefix("8.8.8.8/32")); !errors.Is(err, ErrNotFound) {
		t.Errorf("IP fora: %v", err)
	}

	list, err := st.ByDocument(ctx, "10996639")
	if err != nil || len(list) != 2 || list[0].ASN != 275689 {
		t.Errorf("ByDocument = %+v, err = %v", list, err)
	}
	all, err := st.ListASNs(ctx)
	if err != nil || len(all) != 4 || all[0].ASN != 61613 {
		t.Errorf("ListASNs = %+v, err = %v", all, err)
	}
}
