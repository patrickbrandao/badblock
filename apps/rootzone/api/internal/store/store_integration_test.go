//go:build integration

package store

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/patrickbrandao/badblock/apps/rootzone/api/internal/testdb"
)

// seed é um recorte real da zona de 2026-09-29 (serial 2026092901), como o
// collector-rootzone grava: bo (sem DS, um servidor sem AAAA, ns.dns.br com
// glue sob outro TLD), top (2 DS, servidores só com IPv4 ou só com IPv6) e
// xn--p1ai (рф), com os NSEC deles, o SOA e um NS da raiz com o glue; mais
// três execuções (duas aplicadas e uma recusada, a mais nova) e a linha do
// coletor em jobs.
const seed = `
INSERT INTO rootzone_tld (tld, tld_unicode, nameservers, nameservers_ipv4, nameservers_ipv6, ds_records) VALUES
    ('bo', 'bo', 4, 4, 3, 0),
    ('top', 'top', 8, 6, 3, 2),
    ('xn--p1ai', 'рф', 6, 6, 6, 1);
INSERT INTO rootzone_record (owner, type, rdata, ttl) VALUES
    ('.', 'NS', 'a.root-servers.net', 518400),
    ('.', 'SOA', 'a.root-servers.net nstld.verisign-grs.com 2026092901 1800 900 604800 86400', 86400),
    ('a.dns.ripn.net', 'A', '193.232.128.6', 172800),
    ('a.dns.ripn.net', 'AAAA', '2001:678:17:0:193:232:128:6', 172800),
    ('anycast.ns.nic.bo', 'A', '204.61.216.48', 172800),
    ('anycast.ns.nic.bo', 'AAAA', '2001:500:14:6048:ad::1', 172800),
    ('a.root-servers.net', 'A', '198.41.0.4', 518400),
    ('a.root-servers.net', 'AAAA', '2001:503:ba3e::2:30', 518400),
    ('a.zdnscloud.cn', 'A', '203.99.24.1', 172800),
    ('b.dns.ripn.net', 'A', '194.85.252.62', 172800),
    ('b.dns.ripn.net', 'AAAA', '2001:678:16:0:194:85:252:62', 172800),
    ('bo', 'NS', 'anycast.ns.nic.bo', 172800),
    ('bo', 'NS', 'ns2.nic.fr', 172800),
    ('bo', 'NS', 'ns.dns.br', 172800),
    ('bo', 'NS', 'ns.nic.bo', 172800),
    ('bo', 'NSEC', 'boats NS RRSIG NSEC', 86400),
    ('b.zdnscloud.cn', 'A', '203.99.25.1', 172800),
    ('c.tld-servers.ru', 'A', '194.190.122.17', 172800),
    ('c.tld-servers.ru', 'AAAA', '2a09:bd00:1:0:194:190:122:17', 172800),
    ('c.zdnscloud.com', 'A', '203.99.26.1', 172800),
    ('d.dns.ripn.net', 'A', '194.190.124.17', 172800),
    ('d.dns.ripn.net', 'AAAA', '2001:678:18:0:194:190:124:17', 172800),
    ('d.zdnscloud.com', 'A', '203.99.27.1', 172800),
    ('e.dns.ripn.net', 'A', '193.232.142.17', 172800),
    ('e.dns.ripn.net', 'AAAA', '2001:678:15:0:193:232:142:17', 172800),
    ('e.zdnscloud.cn', 'A', '203.119.82.1', 172800),
    ('e.zdnscloud.cn', 'AAAA', '2401:8d00:15::1', 172800),
    ('f.dns.ripn.net', 'A', '193.232.156.17', 172800),
    ('f.dns.ripn.net', 'AAAA', '2001:678:14:0:193:232:156:17', 172800),
    ('f.zdnscloud.cn', 'A', '116.169.54.111', 172800),
    ('i.zdnscloud.cn', 'AAAA', '2401:8d00:1::1', 172800),
    ('j.zdnscloud.com', 'AAAA', '2401:8d00:2::1', 172800),
    ('ns2.nic.fr', 'A', '192.93.0.4', 172800),
    ('ns2.nic.fr', 'AAAA', '2001:660:3005:1::1:2', 172800),
    ('ns.dns.br', 'A', '200.160.0.5', 172800),
    ('ns.dns.br', 'AAAA', '2001:12ff:0:a20::5', 172800),
    ('ns.nic.bo', 'A', '166.114.1.40', 172800),
    ('top', 'DS', '26780 8 2 5D6E7869EE8E3B536A617DE89482DDD1DCB9DB9DBB1AC33D6ED351E2CA095B1B', 86400),
    ('top', 'DS', '41508 13 2 31422CDF4A9AF99914FF85C97D4FB2291F293C9ADB26011B39E7638A51E1C7DD', 86400),
    ('top', 'NS', 'a.zdnscloud.cn', 172800),
    ('top', 'NS', 'b.zdnscloud.cn', 172800),
    ('top', 'NS', 'c.zdnscloud.com', 172800),
    ('top', 'NS', 'd.zdnscloud.com', 172800),
    ('top', 'NS', 'e.zdnscloud.cn', 172800),
    ('top', 'NS', 'f.zdnscloud.cn', 172800),
    ('top', 'NS', 'i.zdnscloud.cn', 172800),
    ('top', 'NS', 'j.zdnscloud.com', 172800),
    ('top', 'NSEC', 'toray NS DS RRSIG NSEC', 86400),
    ('xn--p1ai', 'DS', '60491 8 2 87F1F8C82EC00047C43AC499A73CC9BEB4FC1503E8558F086DCFB614405F7F21', 86400),
    ('xn--p1ai', 'NS', 'a.dns.ripn.net', 172800),
    ('xn--p1ai', 'NS', 'b.dns.ripn.net', 172800),
    ('xn--p1ai', 'NS', 'c.tld-servers.ru', 172800),
    ('xn--p1ai', 'NS', 'd.dns.ripn.net', 172800),
    ('xn--p1ai', 'NS', 'e.dns.ripn.net', 172800),
    ('xn--p1ai', 'NS', 'f.dns.ripn.net', 172800),
    ('xn--p1ai', 'NSEC', 'xn--pgbs0dh NS DS RRSIG NSEC', 86400);
INSERT INTO rootzone_run (status, url, md5, sha256, bytes, serial, soa_mname, soa_rname, soa_refresh, soa_retry, soa_expire, soa_minimum,
                          tlds, records, rrsigs, type_counts, started_at, created_at)
    VALUES (1, 'https://www.internic.net/domain/root.zone', repeat('a', 32), repeat('a', 64), 2250000, 2026092801,
            'a.root-servers.net', 'nstld.verisign-grs.com', 1800, 900, 604800, 86400, 1437, 22120, 2790, '{}',
            NOW() - interval '3 hours', NOW() - interval '3 hours'),
           (1, 'https://www.internic.net/domain/root.zone', repeat('b', 32), repeat('b', 64), 2250583, 2026092901,
            'a.root-servers.net', 'nstld.verisign-grs.com', 1800, 900, 604800, 86400, 1438, 22131, 2794, '{}',
            NOW() - interval '2 hours', NOW() - interval '2 hours');
INSERT INTO rootzone_run (status, url, md5, sha256, bytes, error, started_at, created_at)
    VALUES (0, 'https://www.internic.net/domain/root.zone', repeat('c', 32), repeat('c', 64), 1000,
            'md5 do arquivo diferente do publicado', NOW() - interval '1 hour', NOW() - interval '1 hour');
INSERT INTO jobs (app, last_sync_at, last_check_at) VALUES ('collector-rootzone', NOW() - interval '2 hours', NOW());
`

func TestQueries(t *testing.T) {
	ctx := context.Background()
	url, admin := testdb.New(t)
	st, err := Open(ctx, url, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// Banco vazio: sem versão, sem jobs, sem TLDs.
	if d, err := st.Dataset(ctx); d != nil || err != nil {
		t.Fatalf("Dataset vazio = %+v, %v", d, err)
	}
	if j, err := st.Job(ctx); j != nil || err != nil {
		t.Fatalf("Job vazio = %+v, %v", j, err)
	}
	if l, err := st.TLDs(ctx); len(l) != 0 || err != nil {
		t.Fatalf("TLDs vazio = %+v, %v", l, err)
	}
	if _, err := st.Delegation(ctx, "bo"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delegation vazio = %v", err)
	}

	if _, err := admin.Exec(ctx, seed); err != nil {
		t.Fatal(err)
	}

	// A versão é a última execução aplicada, não a última linha (recusada).
	d, err := st.Dataset(ctx)
	if err != nil || d == nil {
		t.Fatalf("Dataset = %+v, %v", d, err)
	}
	if d.SHA256 != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" || d.URL != "https://www.internic.net/domain/root.zone" ||
		*d.Serial != 2026092901 || *d.SOAMName != "a.root-servers.net" || *d.SOARName != "nstld.verisign-grs.com" ||
		*d.SOARefresh != 1800 || *d.SOARetry != 900 || *d.SOAExpire != 604800 || *d.SOAMinimum != 86400 ||
		*d.TLDs != 1438 || *d.Records != 22131 || *d.RRSIGs != 2794 || d.AppliedAt.IsZero() || len(d.Version) != 36 {
		t.Errorf("Dataset = %+v", d)
	}
	j, err := st.Job(ctx)
	if err != nil || j == nil || j.LastSyncAt == nil || j.LastCheckAt == nil || j.Consolidated != 0 {
		t.Errorf("Job = %+v, %v", j, err)
	}

	// Todos os TLDs, em ordem de nome, com as contagens.
	list, err := st.TLDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []TLD{
		{TLD: "bo", Unicode: "bo", Nameservers: 4, NameserversV4: 4, NameserversV6: 3, DSRecords: 0},
		{TLD: "top", Unicode: "top", Nameservers: 8, NameserversV4: 6, NameserversV6: 3, DSRecords: 2},
		{TLD: "xn--p1ai", Unicode: "рф", Nameservers: 6, NameserversV4: 6, NameserversV6: 6, DSRecords: 1},
	}
	if !slices.Equal(list, want) {
		t.Errorf("TLDs = %+v", list)
	}

	// bo: 4 NS em ordem de rdata (collation do banco), sem DS e sem o NSEC;
	// glue de todos, inclusive ns.dns.br (sob br) e ns.nic.bo (sem AAAA).
	bo, err := st.Delegation(ctx, "bo")
	if err != nil {
		t.Fatal(err)
	}
	if bo.TLD.TLD != "bo" || bo.TLD.NameserversV6 != 3 || bo.TLD.CreatedAt.IsZero() || bo.TLD.UpdatedAt.IsZero() {
		t.Errorf("bo = %+v", bo.TLD)
	}
	var ns []string
	for _, r := range bo.Records {
		if r.Type != "NS" || r.TTL != 172800 {
			t.Errorf("bo: registro inesperado %+v", r)
		}
		ns = append(ns, r.RData)
	}
	if !slices.Equal(ns, []string{"anycast.ns.nic.bo", "ns2.nic.fr", "ns.dns.br", "ns.nic.bo"}) {
		t.Errorf("bo NS = %v", ns)
	}
	var glue []string
	for _, g := range bo.Glue {
		glue = append(glue, g.Owner+" "+g.Type+" "+g.RData)
	}
	if !slices.Equal(glue, []string{
		"anycast.ns.nic.bo A 204.61.216.48", "anycast.ns.nic.bo AAAA 2001:500:14:6048:ad::1",
		"ns2.nic.fr A 192.93.0.4", "ns2.nic.fr AAAA 2001:660:3005:1::1:2",
		"ns.dns.br A 200.160.0.5", "ns.dns.br AAAA 2001:12ff:0:a20::5",
		"ns.nic.bo A 166.114.1.40",
	}) {
		t.Errorf("bo glue = %v", glue)
	}

	// top: os 2 DS antes dos 8 NS (ordem de tipo); 9 endereços de glue (6 A, 3 AAAA).
	top, err := st.Delegation(ctx, "top")
	if err != nil {
		t.Fatal(err)
	}
	if len(top.Records) != 10 || top.Records[0].Type != "DS" || top.Records[1].Type != "DS" || top.Records[2].Type != "NS" ||
		top.Records[0].RData != "26780 8 2 5D6E7869EE8E3B536A617DE89482DDD1DCB9DB9DBB1AC33D6ED351E2CA095B1B" ||
		top.Records[0].TTL != 86400 || len(top.Glue) != 9 {
		t.Errorf("top = %+v", top)
	}

	// IDN: a chave é a forma ASCII.
	idn, err := st.Delegation(ctx, "xn--p1ai")
	if err != nil || idn.TLD.Unicode != "рф" || len(idn.Records) != 7 || len(idn.Glue) != 12 {
		t.Errorf("xn--p1ai = %+v, %v", idn, err)
	}

	// A raiz não é um TLD; o store não normaliza (quem chama já normalizou).
	for _, name := range []string{".", "", "BO", "bo.", "a.root-servers.net", "nada"} {
		if d, err := st.Delegation(ctx, name); !errors.Is(err, ErrNotFound) {
			t.Errorf("Delegation(%q) = %+v, %v", name, d, err)
		}
	}

	// Sem execução aplicada, a versão volta a ser nula.
	if _, err := admin.Exec(ctx, "DELETE FROM rootzone_run WHERE status = 1"); err != nil {
		t.Fatal(err)
	}
	if d, err := st.Dataset(ctx); d != nil || err != nil {
		t.Errorf("Dataset sem aplicada = %+v, %v", d, err)
	}
}
