//go:build integration

package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/patrickbrandao/badblock/apps/roothints/api/internal/testdb"
)

// seed tem três servidores reais do named.root de 2026-09-30, fora de ordem,
// e o m sem AAAA e sem comentário (o coletor aceita, com aviso), mais três
// execuções: duas aplicadas e uma recusada, a mais nova.
const seed = `
INSERT INTO roothints_server (name, letter, ipv4, ipv6, ns_ttl, ipv4_ttl, ipv6_ttl, note, created_at, updated_at) VALUES
    ('m.root-servers.net', 'm', '202.12.27.33', NULL, 3600000, 3600000, NULL, NULL, '2026-09-30 03:47:52Z', '2026-09-30 04:00:00Z'),
    ('a.root-servers.net', 'a', '198.41.0.4', '2001:503:ba3e::2:30', 3600000, 3600000, 3600000, 'FORMERLY NS.INTERNIC.NET', '2026-09-30 03:47:52Z', '2026-09-30 03:47:52Z'),
    ('k.root-servers.net', 'k', '193.0.14.129', '2001:7fd::1', 3600000, 3600000, 3600000, 'OPERATED BY RIPE NCC', '2026-09-30 03:47:52Z', '2026-09-30 03:47:52Z');
INSERT INTO roothints_run (status, url, md5, sha256, bytes, last_update, zone_serial, servers, ipv4_addresses, ipv6_addresses, started_at, created_at)
    VALUES (1, 'https://www.internic.net/domain/named.root', repeat('a', 32), repeat('a', 64), 3315, '2026-09-24', 2026092400, 13, 13, 13, NOW() - interval '3 hours', NOW() - interval '3 hours'),
           (1, 'https://www.internic.net/domain/named.root', repeat('b', 32), repeat('b', 64), 3315, '2026-09-24', 2026092401, 13, 13, 13, NOW() - interval '2 hours', NOW() - interval '2 hours'),
           (0, 'https://www.internic.net/domain/named.root', repeat('c', 32), repeat('c', 64), 3315, '2026-09-20', 2026092000, 13, 13, 13, NOW() - interval '1 hour', NOW() - interval '1 hour');
INSERT INTO jobs (app, last_sync_at, last_check_at) VALUES ('collector-roothints', NOW(), NOW());
`

func TestQueries(t *testing.T) {
	url, admin := testdb.New(t)
	ctx := context.Background()
	s, err := Open(ctx, url, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Banco vazio: sem versão, sem jobs, lista vazia.
	if d, err := s.Dataset(ctx); err != nil || d != nil {
		t.Fatalf("dataset vazio = %+v, %v", d, err)
	}
	if j, err := s.Job(ctx); err != nil || j != nil {
		t.Fatalf("jobs vazio = %+v, %v", j, err)
	}
	if list, err := s.Servers(ctx); err != nil || len(list) != 0 {
		t.Fatalf("lista vazia = %+v, %v", list, err)
	}

	if _, err := admin.Exec(ctx, seed); err != nil {
		t.Fatal(err)
	}

	// A versão é a última execução aplicada, não a última linha.
	d, err := s.Dataset(ctx)
	if err != nil || d == nil {
		t.Fatalf("dataset = %+v, %v", d, err)
	}
	var want string
	_ = admin.QueryRow(ctx, `SELECT uuid::text FROM roothints_run WHERE md5 = repeat('b', 32)`).Scan(&want)
	if d.Version != want || d.MD5 != strings.Repeat("b", 32) || d.SHA256 != strings.Repeat("b", 64) ||
		d.URL != "https://www.internic.net/domain/named.root" || d.Servers != 13 ||
		d.LastUpdate == nil || d.LastUpdate.Format(time.DateOnly) != "2026-09-24" ||
		d.ZoneSerial == nil || *d.ZoneSerial != 2026092401 {
		t.Errorf("dataset = %+v", d)
	}
	if j, err := s.Job(ctx); err != nil || j == nil || j.LastSyncAt == nil || j.Consolidated != 0 {
		t.Errorf("jobs = %+v, %v", j, err)
	}

	// Lista em ordem de letra; endereços sem a máscara do inet.
	list, err := s.Servers(ctx)
	if err != nil || len(list) != 3 {
		t.Fatalf("lista = %+v, %v", list, err)
	}
	if list[0].Letter != "a" || list[1].Letter != "k" || list[2].Letter != "m" {
		t.Errorf("ordem = %s %s %s", list[0].Letter, list[1].Letter, list[2].Letter)
	}
	a := list[0]
	if a.Name != "a.root-servers.net" || a.IPv4.String() != "198.41.0.4" || a.IPv6.String() != "2001:503:ba3e::2:30" ||
		a.NSTTL != 3600000 || *a.IPv4TTL != 3600000 || *a.IPv6TTL != 3600000 || *a.Note != "FORMERLY NS.INTERNIC.NET" ||
		!a.CreatedAt.Equal(time.Date(2026, 9, 30, 3, 47, 52, 0, time.UTC)) {
		t.Errorf("a = %+v", a)
	}

	// Pela letra; o m sem AAAA e sem comentário tem os NULLs.
	m, err := s.ServerByLetter(ctx, "m")
	if err != nil || m.IPv4.String() != "202.12.27.33" || m.IPv6 != nil || m.IPv6TTL != nil || m.Note != nil ||
		*m.IPv4TTL != 3600000 || !m.UpdatedAt.Equal(time.Date(2026, 9, 30, 4, 0, 0, 0, time.UTC)) {
		t.Errorf("m = %+v, %v", m, err)
	}
	for _, letter := range []string{"b", "z", "A", ""} {
		if _, err := s.ServerByLetter(ctx, letter); !errors.Is(err, ErrNotFound) {
			t.Errorf("%q: err = %v, quero ErrNotFound", letter, err)
		}
	}

	// Com 13 linhas o planejador prefere ler a página inteira; sem seq scan,
	// a consulta pela letra tem de caber no índice único.
	tx, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
		t.Fatal(err)
	}
	var plan strings.Builder
	rows, err := tx.Query(ctx, `EXPLAIN SELECT `+serverColumns+` FROM roothints_server WHERE letter = 'k'`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var line string
		_ = rows.Scan(&line)
		plan.WriteString(line + "\n")
	}
	rows.Close()
	_ = tx.Rollback(ctx)
	if !strings.Contains(plan.String(), "uq_roothints_server_letter") {
		t.Errorf("plano pela letra sem uq_roothints_server_letter:\n%s", plan.String())
	}

	// Sem execução aplicada, a versão volta a ser nula.
	if _, err := admin.Exec(ctx, `DELETE FROM roothints_run WHERE status = 1`); err != nil {
		t.Fatal(err)
	}
	if d, err := s.Dataset(ctx); err != nil || d != nil {
		t.Errorf("sem aplicada = %+v, %v", d, err)
	}
}
