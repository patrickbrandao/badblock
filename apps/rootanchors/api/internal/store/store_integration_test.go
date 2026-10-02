//go:build integration

package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/patrickbrandao/badblock/apps/rootanchors/api/internal/testdb"
)

// seed são as três chaves reais do root-anchors.xml de 2026-09-30 (como o
// collector-rootanchors grava) mais uma artificial com o key tag da 20326 (o
// key tag não é único), e três execuções: duas aplicadas e uma recusada, a
// mais nova.
const seed = `
INSERT INTO rootanchors_key (key_id, key_tag, algorithm, digest_type, digest, public_key, flags, valid_from, valid_until) VALUES
    ('Kjqmt7v', 19036, 8, 2, '49AAC11D7B6F6446702E54A1607371607A1A41855200FD2CE1CDDE32F24E8FB5', NULL, NULL,
     '2010-07-15T00:00:00Z', '2019-01-11T00:00:00Z'),
    ('Klajeyz', 20326, 8, 2, 'E06D44B80B8F1D39A95C0B0D7C65D08458E880409BBC683457104237C7F8EC8D',
     'AwEAAaz/tAm8yTn4Mfeh5eyI96WSVexTBAvkMgJzkKTOiW1vkIbzxeF3+/4RgWOq7HrxRixHlFlExOLAJr5emLvN7SWXgnLh4+B5xQlNVz8Og8kvArMtNROxVQuCaSnIDdD5LKyWbRd2n9WGe2R8PzgCmr3EgVLrjyBxWezF0jLHwVN8efS3rCj/EWgvIWgb9tarpVUDK/b58Da+sqqls3eNbuv7pr+eoZG+SrDK6nWeL3c6H5Apxz7LjVc1uTIdsIXxuOLYA4/ilBmSVIzuDWfdRUfhHdY6+cn8HFRm+2hM8AnXGXws9555KrUB5qihylGa8subX2Nn6UwNR1AkUTV74bU=',
     257, '2017-02-02T00:00:00Z', NULL),
    ('Kmyv6jo', 38696, 8, 2, '683D2D0ACB8C9B712A1948B27F741219298D0A450D612C483AF444A4C0FB2B16',
     'AwEAAa96jeuknZlaeSrvyAJj6ZHv28hhOKkx3rLGXVaC6rXTsDc449/cidltpkyGwCJNnOAlFNKF2jBosZBU5eeHspaQWOmOElZsjICMQMC3aeHbGiShvZsx4wMYSjH8e7Vrhbu6irwCzVBApESjbUdpWWmEnhathWu1jo+siFUiRAAxm9qyJNg/wOZqqzL/dL/q8PkcRU5oUKEpUge71M3ej2/7CPqpdVwuMoTvoB+ZOT4YeGyxMvHmbrxlFzGOHOijtzN+u1TQNatX2XBuzZNQ1K+s2CXkPIZo7s6JgZyvaBevYtxPvYLw4z9mR7K2vaF18UYH9Z9GNUUeayffKC73PYc=',
     257, '2024-07-18T00:00:00Z', NULL),
    ('Kzzzzzz', 20326, 13, 4, repeat('AB', 48), NULL, NULL, '2030-01-01T00:00:00Z', NULL);
INSERT INTO rootanchors_run (status, url, http_status, sha256, bytes, anchor_id, anchor_source, zone, keys,
                             key_inserted, key_updated, key_deleted, started_at, created_at)
    VALUES (1, 'https://data.iana.org/root-anchors/root-anchors.xml', 200, repeat('a', 64), 1861,
            'ANCHOR-OLD', 'http://data.iana.org/root-anchors/root-anchors.xml', '.', 3, 3, 0, 0,
            NOW() - interval '3 hours', NOW() - interval '3 hours'),
           (1, 'https://data.iana.org/root-anchors/root-anchors.xml', 200, repeat('b', 64), 1900,
            '0C05FDD6-422C-4910-8ED6-430ED15E11C2', NULL, '.', 4, 1, 0, 0,
            NOW() - interval '2 hours', NOW() - interval '2 hours'),
           (0, 'https://data.iana.org/root-anchors/root-anchors.xml', 200, repeat('c', 64), 10,
            NULL, NULL, NULL, NULL, NULL, NULL, NULL,
            NOW() - interval '1 hour', NOW() - interval '1 hour');
INSERT INTO jobs (app, last_sync_at, last_check_at) VALUES ('collector-rootanchors', NOW(), NOW());
`

func ids(list []Key) string {
	out := make([]string, 0, len(list))
	for _, k := range list {
		out = append(out, k.KeyID)
	}
	return strings.Join(out, ",")
}

func TestQueries(t *testing.T) {
	url, admin := testdb.New(t)
	ctx := context.Background()

	st, err := Open(ctx, url, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)

	// Banco vazio: sem versão, sem jobs e sem chaves (antes da primeira carga).
	if d, err := st.Dataset(ctx); err != nil || d != nil {
		t.Fatalf("Dataset vazio = %+v, err = %v", d, err)
	}
	if j, err := st.Job(ctx); err != nil || j != nil {
		t.Fatalf("Job vazio = %+v, err = %v", j, err)
	}
	if list, err := st.Keys(ctx); err != nil || len(list) != 0 || list == nil {
		t.Fatalf("Keys vazio = %v, err = %v", list, err)
	}

	if _, err := admin.Exec(ctx, seed); err != nil {
		t.Fatal(err)
	}

	// A versão é a última aplicada, não a última linha (a recusada).
	d, err := st.Dataset(ctx)
	if err != nil || d == nil {
		t.Fatalf("Dataset = %+v, err = %v", d, err)
	}
	if d.SHA256 != strings.Repeat("b", 64) || d.AnchorID != "0C05FDD6-422C-4910-8ED6-430ED15E11C2" ||
		d.AnchorSource != nil || d.Zone != "." || d.Keys != 4 || d.URL != "https://data.iana.org/root-anchors/root-anchors.xml" ||
		len(d.Version) != 36 || time.Since(d.AppliedAt) < time.Hour {
		t.Errorf("Dataset = %+v", d)
	}
	j, err := st.Job(ctx)
	if err != nil || j == nil || j.LastSyncAt == nil || j.LastCheckAt == nil || j.Consolidated != 0 {
		t.Errorf("Job = %+v, err = %v", j, err)
	}

	// Todas, em ordem de valid_from e key_id.
	all, err := st.Keys(ctx)
	if err != nil || ids(all) != "Kjqmt7v,Klajeyz,Kmyv6jo,Kzzzzzz" {
		t.Fatalf("Keys = %s, err = %v", ids(all), err)
	}
	k := all[0]
	if k.KeyTag != 19036 || k.Algorithm != 8 || k.DigestType != 2 || k.PublicKey != nil || k.Flags != nil ||
		k.ValidUntil == nil || !k.ValidUntil.Equal(time.Date(2019, 1, 11, 0, 0, 0, 0, time.UTC)) ||
		!k.ValidFrom.Equal(time.Date(2010, 7, 15, 0, 0, 0, 0, time.UTC)) || k.CreatedAt.IsZero() || k.UpdatedAt.IsZero() {
		t.Errorf("19036 = %+v", k)
	}
	k = all[1]
	if k.KeyTag != 20326 || k.PublicKey == nil || !strings.HasPrefix(*k.PublicKey, "AwEAAaz/") || k.Flags == nil ||
		*k.Flags != 257 || k.ValidUntil != nil || k.Digest != "E06D44B80B8F1D39A95C0B0D7C65D08458E880409BBC683457104237C7F8EC8D" {
		t.Errorf("20326 = %+v", k)
	}

	// Por key tag: repetido (duas, em ordem), único e inexistente.
	for tag, want := range map[int]string{20326: "Klajeyz,Kzzzzzz", 38696: "Kmyv6jo", 19036: "Kjqmt7v", 0: "", 65535: "", 12345: ""} {
		list, err := st.KeysByTag(ctx, tag)
		if err != nil || ids(list) != want {
			t.Errorf("KeysByTag(%d) = %q, quero %q, err = %v", tag, ids(list), want, err)
		}
	}
	if list, _ := st.KeysByTag(ctx, 20326); list[1].Algorithm != 13 || list[1].DigestType != 4 || len(list[1].Digest) != 96 {
		t.Errorf("chave artificial = %+v", list[1])
	}

	// Os índices servem às consultas (com seq scan desligado, porque a tabela
	// tem poucas linhas e o planejador preferiria percorrê-la).
	for q, idx := range map[string]string{
		`SELECT uuid FROM rootanchors_run WHERE status = 1 ORDER BY created_at DESC LIMIT 1`:   "ix_rootanchors_run_applied",
		`SELECT key_id FROM rootanchors_key WHERE key_tag = 20326 ORDER BY valid_from, key_id`: "ix_rootanchors_key_key_tag",
		`SELECT app FROM jobs WHERE app = 'collector-rootanchors'`:                             "uq_jobs_app",
	} {
		if plan := explain(t, admin, q); !strings.Contains(plan, idx) {
			t.Errorf("%s não usa %s: %s", q, idx, plan)
		}
	}

	// Sem execução aplicada, a versão volta a ser nula.
	if _, err := admin.Exec(ctx, `DELETE FROM rootanchors_run WHERE status = 1`); err != nil {
		t.Fatal(err)
	}
	if d, err := st.Dataset(ctx); err != nil || d != nil {
		t.Errorf("sem aplicada = %+v, err = %v", d, err)
	}
}

// explain devolve o plano de q numa conexão com enable_seqscan desligado.
func explain(t *testing.T, db *pgxpool.Pool, q string) string {
	t.Helper()
	ctx := context.Background()
	conn, err := db.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SET enable_seqscan = off"); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = conn.Exec(ctx, "RESET enable_seqscan") }()
	rows, err := conn.Query(ctx, "EXPLAIN "+q)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, strings.TrimSpace(line))
	}
	return strings.Join(plan, " / ")
}
