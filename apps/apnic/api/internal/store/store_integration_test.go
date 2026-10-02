//go:build integration

package store

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/patrickbrandao/badblock/apps/apnic/api/internal/rir"
)

// seed é um recorte real (titulares 258500 e 130343, faixas de ASN
// available), mais casos que nem todo RIR tem: registro IPv4 dividido em dois
// CIDRs, bloco aninhado, cc ZZ, faixa no fim do espaço de ASNs e opaque_id em
// hex maiúsculo. Os testes não dependem do RIR.
const seed = `
INSERT INTO apnic_asn (asn_start, asn_count, cc, reg_date, status, opaque_id) VALUES
    (1916,       1, 'BR', '1999-11-16', 'allocated', '130343'),
    (6064,       1, NULL, NULL,         'available', NULL),
    (26596,      2, NULL, NULL,         'available', NULL),
    (28003,      3, NULL, NULL,         'available', NULL),
    (61613,      1, 'BR', '2023-05-05', 'allocated', '258500'),
    (327680,     1, 'ZA', '2010-01-01', 'allocated', 'F367B216'),
    (4294967294, 2, 'ZZ', NULL,         'reserved',  NULL);
INSERT INTO apnic_prefix (prefix, cc, reg_date, status, opaque_id, record_start, record_value) VALUES
    ('45.171.60.0/22',    'BR', '2019-02-11', 'allocated', '258500',   '45.171.60.0',    1024),
    ('200.192.152.0/22',  'BR', '2003-11-25', 'allocated', '258500',   '200.192.152.0',  1024),
    ('2804:5964::/32',    'BR', '2019-02-11', 'allocated', '258500',   '2804:5964::',    32),
    ('150.165.0.0/16',    'BR', '1993-06-07', 'assigned',  '130343',   '150.165.0.0',    65536),
    ('150.165.10.0/24',   'BR', '2001-01-01', 'assigned',  '999',      '150.165.10.0',   256),
    ('2001:12f0::/32',    'BR', '2007-12-19', 'assigned',  '130343',   '2001:12f0::',    32),
    ('62.122.208.0/22',   'ZZ', NULL,         'reserved',  NULL,       '62.122.208.0',   1280),
    ('62.122.212.0/24',   'ZZ', NULL,         'reserved',  NULL,       '62.122.208.0',   1280),
    ('2001:1201:20::/43', NULL, NULL,         'available', NULL,       '2001:1201:20::', 43),
    ('41.0.0.0/24',       'ZA', '2010-01-01', 'allocated', 'F367B216', '41.0.0.0',       256);
INSERT INTO apnic_run (status, url, md5, sha256, serial, start_date, end_date,
                        asn_records, ipv4_records, ipv6_records, prefixes_v4, prefixes_v6, started_at, created_at)
    VALUES (1, 'https://x/antigo', repeat('a', 32), repeat('a', 64), '20260926', '1987-01-01', '2026-09-24',
            1, 1, 1, 1, 1, NOW() - interval '3 hours', NOW() - interval '3 hours'),
           (1, 'https://x/atual',  repeat('b', 32), repeat('b', 64), '20260927', '1987-01-01', '2026-09-25',
            7, 7, 3, 8, 3, NOW() - interval '2 hours', NOW() - interval '2 hours'),
           (0, 'https://x/atual',  NULL, NULL, NULL, NULL, NULL,
            NULL, NULL, NULL, NULL, NULL, NOW() - interval '1 hour', NOW() - interval '1 hour');
`

// setup sobe um PG18, aplica as migrations de central/ e do RIR e devolve um
// Store conectado com o usuário postgres (tabelas vazias) e o pool admin.
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

	for _, dir := range []string{"central", rir.Source} {
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

	st, err := Open(ctx, superURL, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	return st, admin
}

func TestQueries(t *testing.T) {
	st, admin := setup(t)
	ctx := context.Background()

	// Antes da primeira carga: sem dataset e sem linha em jobs.
	if d, err := st.Dataset(ctx); d != nil || err != nil {
		t.Fatalf("Dataset vazio = %+v, err = %v", d, err)
	}
	if j, err := st.Job(ctx); j != nil || err != nil {
		t.Fatalf("Job vazio = %+v, err = %v", j, err)
	}

	if _, err := admin.Exec(ctx, seed); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO jobs (app, last_sync_at, last_check_at) VALUES ($1, NOW(), NOW())`, CollectorApp); err != nil {
		t.Fatal(err)
	}

	t.Run("dataset", func(t *testing.T) {
		d, err := st.Dataset(ctx)
		if err != nil || d == nil {
			t.Fatalf("Dataset = %+v, err = %v", d, err)
		}
		var want string
		_ = admin.QueryRow(ctx, `SELECT uuid::text FROM apnic_run WHERE url = 'https://x/atual' AND status = 1`).Scan(&want)
		if d.Version != want || d.URL != "https://x/atual" || *d.SHA256 != strings.Repeat("b", 64) || *d.MD5 != strings.Repeat("b", 32) ||
			*d.Serial != "20260927" || d.StartDate.Format(time.DateOnly) != "1987-01-01" || d.EndDate.Format(time.DateOnly) != "2026-09-25" ||
			*d.ASNRecords != 7 || *d.IPv4Records != 7 || *d.IPv6Records != 3 || *d.PrefixesV4 != 8 || *d.PrefixesV6 != 3 {
			t.Errorf("Dataset = %+v (quero a última aplicada, versão %s)", d, want)
		}
		j, err := st.Job(ctx)
		if err != nil || j == nil || j.LastSyncAt == nil || j.LastCheckAt == nil || j.Consolidated != 0 {
			t.Errorf("Job = %+v, err = %v", j, err)
		}
	})

	t.Run("asn", func(t *testing.T) {
		for asn, want := range map[int64]string{
			61613:      "61613-61613 BR 2023-05-05 allocated 258500",
			28003:      "28003-28005 - - available -", // início da faixa
			28004:      "28003-28005 - - available -", // meio
			28005:      "28003-28005 - - available -", // fim
			26597:      "26596-26597 - - available -",
			1916:       "1916-1916 BR 1999-11-16 allocated 130343",
			4294967295: "4294967294-4294967295 ZZ - reserved -",
		} {
			a, err := st.ASN(ctx, asn)
			if err != nil {
				t.Errorf("AS%d: %v", asn, err)
				continue
			}
			got := fmt.Sprintf("%d-%d %s %s %s %s", a.Start, a.End, str(a.Info.CC), date(a.Info.RegDate), a.Info.Status, str(a.Info.OpaqueID))
			if got != want || a.Count != a.End-a.Start+1 || a.CreatedAt.IsZero() || a.UpdatedAt.IsZero() {
				t.Errorf("AS%d = %s (count %d), quero %s", asn, got, a.Count, want)
			}
		}
		// Fora de qualquer faixa: abaixo da primeira, logo depois de uma faixa, num buraco.
		for _, asn := range []int64{0, 1, 6065, 28006, 61614, 4294967293} {
			if a, err := st.ASN(ctx, asn); !errors.Is(err, ErrNotFound) {
				t.Errorf("AS%d fora: %+v, %v", asn, a, err)
			}
		}
	})

	t.Run("covering", func(t *testing.T) {
		for q, want := range map[string]string{
			"45.171.61.10/32":       "45.171.60.0/22 BR 258500 45.171.60.0+1024",
			"45.171.60.0/22":        "45.171.60.0/22 BR 258500 45.171.60.0+1024", // exato
			"45.171.62.0/23":        "45.171.60.0/22 BR 258500 45.171.60.0+1024",
			"150.165.10.5/32":       "150.165.10.0/24 BR 999 150.165.10.0+256", // aninhado: o mais específico
			"150.165.11.1/32":       "150.165.0.0/16 BR 130343 150.165.0.0+65536",
			"150.165.0.0/16":        "150.165.0.0/16 BR 130343 150.165.0.0+65536",
			"62.122.212.9/32":       "62.122.212.0/24 ZZ - 62.122.208.0+1280", // registro dividido
			"62.122.209.0/24":       "62.122.208.0/22 ZZ - 62.122.208.0+1280",
			"2804:5964:1::1/128":    "2804:5964::/32 BR 258500 2804:5964::+32",
			"2001:1201:3f::/48":     "2001:1201:20::/43 - - 2001:1201:20::+43",
			"2001:12f0:abcd::1/128": "2001:12f0::/32 BR 130343 2001:12f0::+32",
		} {
			b, err := st.Covering(ctx, netip.MustParsePrefix(q))
			if err != nil {
				t.Errorf("%s: %v", q, err)
				continue
			}
			got := fmt.Sprintf("%s %s %s %s+%d", b.Prefix, str(b.Info.CC), str(b.Info.OpaqueID), b.RecordStart, b.RecordValue)
			if got != want || b.Info.Status == "" {
				t.Errorf("%s → %s, quero %s", q, got, want)
			}
		}
		for _, q := range []string{"8.8.8.8/32", "45.171.60.0/21", "2001:db8::1/128", "::/0"} {
			if b, err := st.Covering(ctx, netip.MustParsePrefix(q)); !errors.Is(err, ErrNotFound) {
				t.Errorf("%s fora: %+v, %v", q, b, err)
			}
		}
	})

	t.Run("holder", func(t *testing.T) {
		h, err := st.Holder(ctx, "258500")
		if err != nil {
			t.Fatal(err)
		}
		if h.OpaqueID != "258500" || len(h.ASNs) != 1 || h.ASNs[0].Start != 61613 ||
			prefixes(h.Blocks) != "45.171.60.0/22,200.192.152.0/22,2804:5964::/32" {
			t.Errorf("258500 = %+v, blocos %s", h, prefixes(h.Blocks))
		}
		if b := h.Blocks[1]; date(b.Info.RegDate) != "2003-11-25" || b.RecordValue != 1024 || b.RecordStart.String() != "200.192.152.0" {
			t.Errorf("bloco = %+v", b)
		}

		h, err = st.Holder(ctx, "130343")
		if err != nil || len(h.ASNs) != 1 || h.ASNs[0].Start != 1916 || prefixes(h.Blocks) != "150.165.0.0/16,2001:12f0::/32" {
			t.Errorf("130343 = %+v, %v", h, err)
		}
		// Só blocos, sem ASN.
		h, err = st.Holder(ctx, "999")
		if err != nil || len(h.ASNs) != 0 || prefixes(h.Blocks) != "150.165.10.0/24" {
			t.Errorf("999 = %+v, %v", h, err)
		}
		// Hex em outra caixa: vale o valor gravado.
		for _, id := range []string{"F367B216", "f367b216", "F367b216"} {
			h, err = st.Holder(ctx, id)
			if err != nil || h.OpaqueID != "F367B216" || len(h.ASNs) != 1 || prefixes(h.Blocks) != "41.0.0.0/24" {
				t.Errorf("%s = %+v, %v", id, h, err)
			}
		}
		for _, id := range []string{"nope", "25850", "2585000"} {
			if h, err := st.Holder(ctx, id); !errors.Is(err, ErrNotFound) {
				t.Errorf("%s inexistente: %+v, %v", id, h, err)
			}
		}
	})

	t.Run("canceled", func(t *testing.T) {
		c, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := st.ASN(c, 61613); err == nil || errors.Is(err, ErrNotFound) {
			t.Errorf("contexto cancelado deveria falhar: %v", err)
		}
	})
}

func str(s *string) string {
	if s == nil {
		return "-"
	}
	return *s
}

func date(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return t.Format(time.DateOnly)
}

func prefixes(bs []Block) string {
	var out []string
	for _, b := range bs {
		out = append(out, b.Prefix.String())
	}
	return strings.Join(out, ",")
}
