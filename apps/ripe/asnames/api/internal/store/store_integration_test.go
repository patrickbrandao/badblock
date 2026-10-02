//go:build integration

package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/patrickbrandao/badblock/apps/ripe/asnames/api/internal/testdb"
)

// seed é um recorte real do asn.txt (com os campos derivados como o
// collector-ripe-asnames grava) mais linhas artificiais com os curingas do LIKE
// (%, _ e \) na description, para provar que a busca os trata como texto.
const seed = `
INSERT INTO ripe_asnames_asn (asn, description, handle, name, country) VALUES
    (513,    'CERN CERN - European Organization for Nuclear Research, CH',     'CERN',        'CERN - European Organization for Nuclear Research', 'CH'),
    (1297,   'CERN1297 CERN - European Organization for Nuclear Research, CH', 'CERN1297',    'CERN - European Organization for Nuclear Research', 'CH'),
    (4745,   'AS4745-138 - , KR',                                              'AS4745-138',  NULL,                                                'KR'),
    (7901,   '- , NZ',                                                         NULL,          NULL,                                                'NZ'),
    (15169,  'GOOGLE - Google LLC, US',                                        'GOOGLE',      'Google LLC',                                        'US'),
    (16509,  'AMAZON-02 - Amazon.com, Inc., US',                               'AMAZON-02',   'Amazon.com, Inc.',                                  'US'),
    (29571,  'Orange Côte d''Ivoire - Orange Côte d''Ivoire, CI',              'Orange Côte d''Ivoire', 'Orange Côte d''Ivoire',                   'CI'),
    (61613,  'AS61613 - TMSoft Solucoes em Informatica Ltda, BR',              'AS61613',     'TMSoft Solucoes em Informatica Ltda',               'BR'),
    (327710, 'Orange Côte d''Ivoire - Orange Côte d''Ivoire, CI',              'ORANGE CÔTE D''IVOIRE', 'Orange Côte d''Ivoire',                   'CI'),
    (403009, 'LOREM-IPSUM - Lorem, US',                                        'LOREM-IPSUM', 'Lorem',                                             'US'),
    (64500,  'TEST-PCT - 100% Fibra, BR',                                      'TEST-PCT',    '100% Fibra',                                        'BR'),
    (64501,  'TEST-1000 - 1000 Fibras, BR',                                    'TEST-1000',   '1000 Fibras',                                       'BR'),
    (64502,  'TEST_UND - Under_score Ltda, BR',                                'TEST_UND',    'Under_score Ltda',                                  'BR'),
    (64503,  'TESTXUND - UnderXscore Ltda, BR',                                'TESTXUND',    'UnderXscore Ltda',                                  'BR'),
    (64504,  'TEST-BS - Back\Slash Ltda, BR',                                  'TEST-BS',     'Back\Slash Ltda',                                   'BR'),
    (64505,  'TEST-BS2 - BackSlash Ltda, BR',                                  'TEST-BS2',    'BackSlash Ltda',                                    'BR');
INSERT INTO ripe_asnames_run (status, url, sha256, bytes, asns, started_at, created_at)
    VALUES (1, 'https://ftp.ripe.net/ripe/asnames/asn.txt', repeat('a', 64), 1000, 16, NOW() - interval '3 hours', NOW() - interval '3 hours'),
           (1, 'https://ftp.ripe.net/ripe/asnames/asn.txt', repeat('b', 64), 1000, 16, NOW() - interval '2 hours', NOW() - interval '2 hours'),
           (0, 'https://ftp.ripe.net/ripe/asnames/asn.txt', NULL,           NULL, NULL, NOW() - interval '1 hour', NOW() - interval '1 hour');
INSERT INTO jobs (app, last_sync_at, last_check_at) VALUES ('collector-ripe-asnames', NOW(), NOW());
`

func asns[T any](list []T, get func(T) int64) []int64 {
	out := make([]int64, 0, len(list))
	for _, x := range list {
		out = append(out, get(x))
	}
	return out
}

func entryASN(e Entry) int64 { return e.ASN }
func briefASN(b Brief) int64 { return b.ASN }

func equal(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestQueries(t *testing.T) {
	url, admin := testdb.New(t)
	ctx := context.Background()

	st, err := Open(ctx, url, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)

	// Banco vazio: sem versão e sem linha em jobs (antes da primeira carga).
	if d, err := st.Dataset(ctx); err != nil || d != nil {
		t.Fatalf("Dataset vazio = %+v, err = %v", d, err)
	}
	if j, err := st.Job(ctx); err != nil || j != nil {
		t.Fatalf("Job vazio = %+v, err = %v", j, err)
	}

	if _, err := admin.Exec(ctx, seed); err != nil {
		t.Fatal(err)
	}

	// A versão é a última linha aplicada (status = 1), não a última linha.
	d, err := st.Dataset(ctx)
	if err != nil || d == nil || d.SHA256 != strings.Repeat("b", 64) || d.ASNs != 16 || d.URL == "" || d.Version == "" {
		t.Fatalf("Dataset = %+v, err = %v", d, err)
	}
	j, err := st.Job(ctx)
	if err != nil || j == nil || j.LastSyncAt == nil || j.LastCheckAt == nil || j.Consolidated != 0 {
		t.Errorf("Job = %+v, err = %v", j, err)
	}
	if err := st.Ping(ctx); err != nil {
		t.Error(err)
	}

	t.Run("ASN", func(t *testing.T) {
		a, err := st.ASN(ctx, 15169)
		if err != nil || *a.Handle != "GOOGLE" || *a.Name != "Google LLC" || *a.Country != "US" ||
			a.Description != "GOOGLE - Google LLC, US" || a.CreatedAt.IsZero() || a.UpdatedAt.IsZero() {
			t.Errorf("ASN = %+v, err = %v", a, err)
		}
		a, err = st.ASN(ctx, 7901)
		if err != nil || a.Handle != nil || a.Name != nil || *a.Country != "NZ" {
			t.Errorf("ASN com nulos = %+v, err = %v", a, err)
		}
		if _, err := st.ASN(ctx, 1); !errors.Is(err, ErrNotFound) {
			t.Errorf("ASN inexistente: %v", err)
		}
	})

	t.Run("ByCountry", func(t *testing.T) {
		list, err := st.ByCountry(ctx, "US")
		if err != nil || !equal(asns(list, briefASN), []int64{15169, 16509, 403009}) || *list[0].Handle != "GOOGLE" {
			t.Errorf("US = %+v, err = %v", list, err)
		}
		list, err = st.ByCountry(ctx, "NZ")
		if err != nil || len(list) != 1 || list[0].Handle != nil || list[0].Name != nil {
			t.Errorf("NZ = %+v, err = %v", list, err)
		}
		if list, err := st.ByCountry(ctx, "ZZ"); err != nil || len(list) != 0 {
			t.Errorf("ZZ = %+v, err = %v", list, err)
		}
	})

	t.Run("ByHandle", func(t *testing.T) {
		for _, h := range []string{"google", "GOOGLE", "GoOgLe"} {
			list, err := st.ByHandle(ctx, h)
			if err != nil || !equal(asns(list, entryASN), []int64{15169}) || list[0].Description == "" || *list[0].Country != "US" {
				t.Errorf("%s = %+v, err = %v", h, list, err)
			}
		}
		// Repetido, com espaço e não-ASCII, em caixas diferentes no banco.
		list, err := st.ByHandle(ctx, "orange côte d'ivoire")
		if err != nil || !equal(asns(list, entryASN), []int64{29571, 327710}) {
			t.Errorf("orange = %+v, err = %v", list, err)
		}
		// Igualdade, não prefixo nem curinga.
		for _, h := range []string{"goog", "cern%", "test_und_", "test%"} {
			if list, err := st.ByHandle(ctx, h); err != nil || len(list) != 0 {
				t.Errorf("%s = %+v, err = %v", h, list, err)
			}
		}
		if list, err := st.ByHandle(ctx, "test_und"); err != nil || !equal(asns(list, entryASN), []int64{64502}) {
			t.Errorf("test_und = %+v, err = %v", list, err)
		}
	})

	t.Run("Search", func(t *testing.T) {
		cases := map[string][]int64{
			"google llc":                {15169},
			"cern":                      {513, 1297},
			"european organization for": {513, 1297},
			"côte d'ivoire":             {29571, 327710},
			"ltda":                      {61613, 64502, 64503, 64504, 64505},
			// Curingas como texto literal.
			"100%":        {64500},
			"% fibra":     {64500},
			"under_score": {64502},
			"r_s":         {64502}, // sem escape, casaria também "UnderXscore"
			`back\slash`:  {64504},
			`\`:           {64504},
			"%%%":         {},
			"___":         {},
			"zzqqxx":      {},
		}
		for term, want := range cases {
			list, err := st.Search(ctx, term, 101)
			if err != nil || !equal(asns(list, entryASN), want) {
				t.Errorf("%q = %v, quero %v, err = %v", term, asns(list, entryASN), want, err)
			}
		}
		// Maiúsculas no termo não importam (ILIKE).
		if list, _ := st.Search(ctx, "GOOGLE LLC", 101); len(list) != 1 {
			t.Errorf("maiúsculas = %+v", list)
		}
		// Limite e ordem.
		list, err := st.Search(ctx, "ltda", 2)
		if err != nil || !equal(asns(list, entryASN), []int64{61613, 64502}) {
			t.Errorf("limite = %v, err = %v", asns(list, entryASN), err)
		}
		if list[0].Handle == nil || *list[0].Name != "TMSoft Solucoes em Informatica Ltda" || *list[0].Country != "BR" {
			t.Errorf("campos = %+v", list[0])
		}
	})

	// Sem nenhuma linha aplicada, a versão volta a ser nil.
	if _, err := admin.Exec(ctx, `DELETE FROM ripe_asnames_run WHERE status = 1; DELETE FROM jobs`); err != nil {
		t.Fatal(err)
	}
	if d, err := st.Dataset(ctx); err != nil || d != nil {
		t.Errorf("Dataset sem aplicação = %+v, err = %v", d, err)
	}
}
