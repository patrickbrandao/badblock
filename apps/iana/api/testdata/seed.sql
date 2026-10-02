-- Recorte REAL das tabelas iana_* para o teste de integração do store.
--
-- Gerado em 2026-09-28 a partir de um PostgreSQL 18 carregado pelo binário do
-- collector-iana (--once) com os 10 arquivos da IANA do dia: as linhas abaixo
-- são cópias exatas das que o collector gravou (só um subconjunto delas),
-- escolhidas para cobrir os casos do teste (specs/fontes/iana/api.md, "Regra
-- de bogon").
-- iana_special_asn e iana_special_prefix vão inteiras (9 e 51 linhas): a regra
-- de bogon depende do aninhamento entre elas. As duas execuções sintéticas de
-- iana_run (uma aplicada mais antiga e uma recusada mais nova) e a linha de
-- jobs testam a escolha da versão do dataset.
--
-- Para regerar: carregue um banco com o collector-iana e rode
-- testdata/gen-seed.sql com o psql; as linhas sintéticas do fim (iana_run
-- extras e jobs) são escritas à mão.

INSERT INTO iana_asn_block (asn_start, asn_end, description, registry, whois, rdap_urls, reference, registration_date, source_file) VALUES
    (0, 0, 'Reserved', NULL, NULL, '{}', '[RFC7607]', NULL, 'as-numbers-1'),
    (1, 1876, 'Assigned by ARIN', 'arin', 'whois.arin.net', '{https://rdap.arin.net/registry,http://rdap.arin.net/registry}', NULL, NULL, 'as-numbers-1'),
    (23456, 23456, 'AS_TRANS', NULL, NULL, '{}', '[RFC6793]', NULL, 'as-numbers-1'),
    (61440, 61951, 'Assigned by LACNIC', 'lacnic', 'whois.lacnic.net', '{https://rdap.lacnic.net/rdap/}', NULL, '2013-06-11', 'as-numbers-1'),
    (64496, 64511, 'Reserved for use in documentation and sample code', NULL, NULL, '{}', '[RFC5398]', '2008-12-03', 'as-numbers-1'),
    (64512, 65534, 'Reserved for Private Use', NULL, NULL, '{}', '[RFC6996]', NULL, 'as-numbers-1'),
    (65535, 65535, 'Reserved', NULL, NULL, '{}', '[RFC7300]', NULL, 'as-numbers-1'),
    (65536, 65551, 'Reserved for use in documentation and sample code', NULL, NULL, '{}', '[RFC5398]', '2008-12-03', 'as-numbers-2'),
    (65552, 131071, 'Reserved', NULL, NULL, '{}', NULL, NULL, 'as-numbers-2'),
    (262144, 263167, 'Assigned by LACNIC', 'lacnic', 'whois.lacnic.net', '{https://rdap.lacnic.net/rdap/}', NULL, '2006-11-29', 'as-numbers-2'),
    (275869, 327679, 'Unallocated', NULL, NULL, '{}', NULL, NULL, 'as-numbers-2'),
    (4200000000, 4294967294, 'Reserved for Private Use', NULL, NULL, '{}', '[RFC6996]', NULL, 'as-numbers-2'),
    (4294967295, 4294967295, 'Reserved', NULL, NULL, '{}', '[RFC7300]', NULL, 'as-numbers-2');
INSERT INTO iana_special_asn (asn_start, asn_end, reason, reference) VALUES
    (0, 0, 'Reserved by [RFC7607]', '[RFC7607]'),
    (112, 112, 'Used by the AS112 project to sink misdirected DNS queries; see [RFC7534]', '[RFC7534]'),
    (23456, 23456, 'AS_TRANS; reserved by [RFC6793]', '[RFC6793]'),
    (64496, 64511, 'For documentation and sample code; reserved by [RFC5398]', '[RFC5398]'),
    (64512, 65534, 'For private use; reserved by [RFC6996]', '[RFC6996]'),
    (65535, 65535, 'Reserved by [RFC7300]', '[RFC7300]'),
    (65536, 65551, 'For documentation and sample code; reserved by [RFC5398]', '[RFC5398]'),
    (4200000000, 4294967294, 'For private use; reserved by [RFC6996]', '[RFC6996]'),
    (4294967295, 4294967295, 'Reserved by [RFC7300]', '[RFC7300]');
INSERT INTO iana_prefix_block (prefix, designation, registry, whois, rdap_urls, status, allocation_date, note, source_file) VALUES
    ('0.0.0.0/8', 'IANA - Local Identification', NULL, NULL, '{}', 'RESERVED', '1981-09', '[2][3]', 'ipv4-address-space'),
    ('8.0.0.0/8', 'Administered by ARIN', 'arin', 'whois.arin.net', '{https://rdap.arin.net/registry,http://rdap.arin.net/registry}', 'LEGACY', '1992-12', NULL, 'ipv4-address-space'),
    ('10.0.0.0/8', 'IANA - Private Use', NULL, NULL, '{}', 'RESERVED', '1995-06', '[4]', 'ipv4-address-space'),
    ('100.0.0.0/8', 'ARIN', 'arin', 'whois.arin.net', '{https://rdap.arin.net/registry,http://rdap.arin.net/registry}', 'ALLOCATED', '2010-11', '[6]', 'ipv4-address-space'),
    ('127.0.0.0/8', 'IANA - Loopback', NULL, NULL, '{}', 'RESERVED', '1981-09', '[7]', 'ipv4-address-space'),
    ('187.0.0.0/8', 'LACNIC', 'lacnic', 'whois.lacnic.net', '{https://rdap.lacnic.net/rdap/}', 'ALLOCATED', '2007-09', NULL, 'ipv4-address-space'),
    ('192.0.0.0/8', 'Administered by ARIN', 'arin', 'whois.arin.net', '{https://rdap.arin.net/registry,http://rdap.arin.net/registry}', 'LEGACY', '1993-05', '[10][11]', 'ipv4-address-space'),
    ('224.0.0.0/8', 'Multicast', NULL, NULL, '{}', 'RESERVED', '1981-09', '[14]', 'ipv4-address-space'),
    ('240.0.0.0/8', 'Future use', NULL, NULL, '{}', 'RESERVED', '1981-09', '[17]', 'ipv4-address-space'),
    ('2001::/23', 'IANA', NULL, 'whois.iana.org', '{}', 'ALLOCATED', '1999-07-01', 'This range has been partially allocated. See [IPv6 Special-Purpose Address Space] for details.', 'ipv6-unicast-address-assignments'),
    ('2001:c00::/23', 'APNIC', 'apnic', 'whois.apnic.net', '{https://rdap.apnic.net/}', 'ALLOCATED', '2002-05-02', '2001:db8::/32 is reserved for Documentation [RFC3849]. See [IPv6 Special-Purpose Address Space] for details.', 'ipv6-unicast-address-assignments'),
    ('2002::/16', '6to4', NULL, NULL, '{}', 'ALLOCATED', '2001-02-01', 'See [IPv6 Special-Purpose Address Space] for details.', 'ipv6-unicast-address-assignments'),
    ('2800::/12', 'LACNIC', 'lacnic', 'whois.lacnic.net', '{https://rdap.lacnic.net/rdap/}', 'ALLOCATED', '2006-10-03', '2800::/23 was allocated on 2005-11-17. The more recent allocation (2006-10-03) incorporates the previous allocation.', 'ipv6-unicast-address-assignments'),
    ('3ffe::/16', 'IANA', NULL, NULL, '{}', 'RESERVED', '2008-04', '3ffe:831f::/32 was used for Teredo in some old but widely distributed networking stacks. This usage is deprecated in favor of 2001::/32, which was allocated for the purpose in [RFC4380]. 3ffe::/16 and 5f00::/8 were used for the 6bone, but returned [RFC5156].', 'ipv6-unicast-address-assignments'),
    ('3fff::/20', 'Documentation', NULL, NULL, '{}', 'RESERVED', '2024-07-23', 'See [IPv6 Special-Purpose Address Space] for details.', 'ipv6-unicast-address-assignments');
INSERT INTO iana_special_prefix (prefix, name, rfc, allocation_date, termination_date, source, destination, forwardable, globally_reachable, reserved_by_protocol) VALUES
    ('0.0.0.0/8', '"This network"', '[RFC791], Section 3.2', '1981-09', NULL, 't', 'f', 'f', 'f', 't'),
    ('0.0.0.0/32', '"This host on this network"', '[RFC1122], Section 3.2.1.3', '1981-09', NULL, 't', 'f', 'f', 'f', 't'),
    ('10.0.0.0/8', 'Private-Use', '[RFC1918]', '1996-02', NULL, 't', 't', 't', 'f', 'f'),
    ('100.64.0.0/10', 'Shared Address Space', '[RFC6598]', '2012-04', NULL, 't', 't', 't', 'f', 'f'),
    ('127.0.0.0/8', 'Loopback', '[RFC1122], Section 3.2.1.3', '1981-09', NULL, 'f', 'f', 'f', 'f', 't'),
    ('169.254.0.0/16', 'Link Local', '[RFC3927]', '2005-05', NULL, 't', 't', 'f', 'f', 't'),
    ('172.16.0.0/12', 'Private-Use', '[RFC1918]', '1996-02', NULL, 't', 't', 't', 'f', 'f'),
    ('192.0.0.0/24', 'IETF Protocol Assignments', '[RFC6890], Section 2.1', '2010-01', NULL, 'f', 'f', 'f', 'f', 'f'),
    ('192.0.0.0/29', 'IPv4 Service Continuity Prefix', '[RFC7335]', '2011-06', NULL, 't', 't', 't', 'f', 'f'),
    ('192.0.0.8/32', 'IPv4 dummy address', '[RFC7600]', '2015-03', NULL, 't', 'f', 'f', 'f', 'f'),
    ('192.0.0.9/32', 'Port Control Protocol Anycast', '[RFC7723]', '2015-10', NULL, 't', 't', 't', 't', 'f'),
    ('192.0.0.10/32', 'Traversal Using Relays around NAT Anycast', '[RFC8155]', '2017-02', NULL, 't', 't', 't', 't', 'f'),
    ('192.0.0.170/32', 'NAT64/DNS64 Discovery', '[RFC8880][RFC7050], Section 2.2', '2013-02', NULL, 'f', 'f', 'f', 'f', 't'),
    ('192.0.0.171/32', 'NAT64/DNS64 Discovery', '[RFC8880][RFC7050], Section 2.2', '2013-02', NULL, 'f', 'f', 'f', 'f', 't'),
    ('192.0.2.0/24', 'Documentation (TEST-NET-1)', '[RFC5737]', '2010-01', NULL, 'f', 'f', 'f', 'f', 'f'),
    ('192.31.196.0/24', 'AS112-v4', '[RFC7535]', '2014-12', NULL, 't', 't', 't', 't', 'f'),
    ('192.52.193.0/24', 'AMT', '[RFC7450]', '2014-12', NULL, 't', 't', 't', 't', 'f'),
    ('192.88.99.0/24', 'Deprecated (6to4 Relay Anycast)', '[RFC7526]', '2001-06', '2015-03', NULL, NULL, NULL, NULL, NULL),
    ('192.88.99.2/32', '6a44-relay anycast address', '[RFC6751]', '2012-10', NULL, 't', 't', 't', 'f', 'f'),
    ('192.168.0.0/16', 'Private-Use', '[RFC1918]', '1996-02', NULL, 't', 't', 't', 'f', 'f'),
    ('192.175.48.0/24', 'Direct Delegation AS112 Service', '[RFC7534]', '1996-01', NULL, 't', 't', 't', 't', 'f'),
    ('198.18.0.0/15', 'Benchmarking', '[RFC2544]', '1999-03', NULL, 't', 't', 't', 'f', 'f'),
    ('198.51.100.0/24', 'Documentation (TEST-NET-2)', '[RFC5737]', '2010-01', NULL, 'f', 'f', 'f', 'f', 'f'),
    ('203.0.113.0/24', 'Documentation (TEST-NET-3)', '[RFC5737]', '2010-01', NULL, 'f', 'f', 'f', 'f', 'f'),
    ('240.0.0.0/4', 'Reserved', '[RFC1112], Section 4', '1989-08', NULL, 'f', 'f', 'f', 'f', 't'),
    ('255.255.255.255/32', 'Limited Broadcast', '[RFC8190] [RFC919], Section 7', '1984-10', NULL, 'f', 't', 'f', 'f', 't'),
    ('::/128', 'Unspecified Address', '[RFC4291]', '2006-02', NULL, 't', 'f', 'f', 'f', 't'),
    ('::1/128', 'Loopback Address', '[RFC4291]', '2006-02', NULL, 'f', 'f', 'f', 'f', 't'),
    ('::ffff:0.0.0.0/96', 'IPv4-mapped Address', '[RFC4291]', '2006-02', NULL, 'f', 'f', 'f', 'f', 't'),
    ('64:ff9b::/96', 'IPv4-IPv6 Translat.', '[RFC6052]', '2010-10', NULL, 't', 't', 't', 't', 'f'),
    ('64:ff9b:1::/48', 'IPv4-IPv6 Translat.', '[RFC8215]', '2017-06', NULL, 't', 't', 't', 'f', 'f'),
    ('100::/64', 'Discard-Only Address Block', '[RFC6666]', '2012-06', NULL, 't', 't', 't', 'f', 'f'),
    ('100:0:0:1::/64', 'Dummy IPv6 Prefix', '[RFC9780]', '2025-04', NULL, 't', 'f', 'f', 'f', 'f'),
    ('2001::/23', 'IETF Protocol Assignments', '[RFC2928]', '2000-09', NULL, 'f', 'f', 'f', 'f', 'f'),
    ('2001::/32', 'TEREDO', '[RFC4380] [RFC8190]', '2006-01', NULL, 't', 't', 't', NULL, 'f'),
    ('2001:1::1/128', 'Port Control Protocol Anycast', '[RFC7723]', '2015-10', NULL, 't', 't', 't', 't', 'f'),
    ('2001:1::2/128', 'Traversal Using Relays around NAT Anycast', '[RFC8155]', '2017-02', NULL, 't', 't', 't', 't', 'f'),
    ('2001:1::3/128', 'DNS-SD Service Registration Protocol Anycast', '[RFC9665]', '2024-04', NULL, 't', 't', 't', 't', 'f'),
    ('2001:2::/48', 'Benchmarking', '[RFC5180][RFC Errata 1752]', '2008-04', NULL, 't', 't', 't', 'f', 'f'),
    ('2001:3::/32', 'AMT', '[RFC7450]', '2014-12', NULL, 't', 't', 't', 't', 'f'),
    ('2001:4:112::/48', 'AS112-v6', '[RFC7535]', '2014-12', NULL, 't', 't', 't', 't', 'f'),
    ('2001:10::/28', 'Deprecated (previously ORCHID)', '[RFC4843]', '2007-03', '2014-03', NULL, NULL, NULL, NULL, NULL),
    ('2001:20::/28', 'ORCHIDv2', '[RFC7343]', '2014-07', NULL, 't', 't', 't', 't', 'f'),
    ('2001:30::/28', 'Drone Remote ID Protocol Entity Tags (DETs) Prefix', '[RFC9374]', '2022-12', NULL, 't', 't', 't', 't', 'f'),
    ('2001:db8::/32', 'Documentation', '[RFC3849]', '2004-07', NULL, 'f', 'f', 'f', 'f', 'f'),
    ('2002::/16', '6to4', '[RFC3056]', '2001-02', NULL, 't', 't', 't', NULL, 'f'),
    ('2620:4f:8000::/48', 'Direct Delegation AS112 Service', '[RFC7534]', '2011-05', NULL, 't', 't', 't', 't', 'f'),
    ('3fff::/20', 'Documentation', '[RFC9637]', '2024-07', NULL, 'f', 'f', 'f', 'f', 'f'),
    ('5f00::/16', 'Segment Routing (SRv6) SIDs', '[RFC9602]', '2024-04', NULL, 't', 't', 't', 'f', 'f'),
    ('fc00::/7', 'Unique-Local', '[RFC4193] [RFC8190]', '2005-10', NULL, 't', 't', 't', 'f', 'f'),
    ('fe80::/10', 'Link-Local Unicast', '[RFC4291]', '2006-02', NULL, 't', 't', 'f', 'f', 't');
INSERT INTO iana_rdap_service (kind, resource, asn_start, asn_end, prefix, registry, urls) VALUES
    ('asn', '1-1876', '1', '1876', NULL, 'arin', '{https://rdap.arin.net/registry/,http://rdap.arin.net/registry/}'),
    ('asn', '61440-61951', '61440', '61951', NULL, 'lacnic', '{https://rdap.lacnic.net/rdap/}'),
    ('asn', '262144-263167', '262144', '263167', NULL, 'lacnic', '{https://rdap.lacnic.net/rdap/}'),
    ('ipv4', '8.0.0.0/8', NULL, NULL, '8.0.0.0/8', 'arin', '{https://rdap.arin.net/registry/,http://rdap.arin.net/registry/}'),
    ('ipv4', '100.0.0.0/8', NULL, NULL, '100.0.0.0/8', 'arin', '{https://rdap.arin.net/registry/,http://rdap.arin.net/registry/}'),
    ('ipv4', '187.0.0.0/8', NULL, NULL, '187.0.0.0/8', 'lacnic', '{https://rdap.lacnic.net/rdap/}'),
    ('ipv4', '192.0.0.0/8', NULL, NULL, '192.0.0.0/8', 'arin', '{https://rdap.arin.net/registry/,http://rdap.arin.net/registry/}'),
    ('ipv6', '2001:c00::/23', NULL, NULL, '2001:c00::/23', 'apnic', '{https://rdap.apnic.net/}'),
    ('ipv6', '2800::/12', NULL, NULL, '2800::/12', 'lacnic', '{https://rdap.lacnic.net/rdap/}');
INSERT INTO iana_run (uuid, status, forced, sha256, files, changes, warnings, error, started_at, created_at) VALUES
    ('01a0eaa2-fd5c-7c24-bc6c-4e3c35d61c53', 1, 'f', 'b22ef6b2e76eafd675b5d1a144ad6488720da7c39d56e1d8c09a6cf6401ad06e', E'[{"url": "https://www.iana.org/assignments/as-numbers/as-numbers-1.csv", "name": "as-numbers-1", "rows": 88, "bytes": 7936, "sha256": "47f5fe7842029d8077ef749d6b3590293df3252841f1446a188284c5a1839949", "changed": true, "http_status": 200, "last_modified": "Sat, 19 Sep 2026 00:44:44 GMT"}, {"url": "https://www.iana.org/assignments/as-numbers/as-numbers-2.csv", "name": "as-numbers-2", "rows": 85, "bytes": 7641, "sha256": "50d66bc2884c470438c9c896148713af40692e8139cbe1b74b7364c4223634b7", "changed": true, "http_status": 200, "last_modified": "Sat, 19 Sep 2026 00:44:44 GMT"}, {"url": "https://www.iana.org/assignments/ipv4-address-space/ipv4-address-space.csv", "name": "ipv4-address-space", "rows": 256, "bytes": 22972, "sha256": "b7b28385e8bc8785c117e2ea65974f942cc6f7dd9dce914f2253ff0adfad4b03", "changed": true, "http_status": 200, "last_modified": "Sat, 19 Sep 2026 00:44:20 GMT"}, {"url": "https://www.iana.org/assignments/ipv6-unicast-address-assignments/ipv6-unicast-address-assignments.csv", "name": "ipv6-unicast-address-assignments", "rows": 51, "bytes": 5666, "sha256": "ebff425bb1acbbea29c4f28146930873faddd5ee57260e95b57ed9e04ea21dd8", "changed": true, "http_status": 200, "last_modified": "Sat, 19 Sep 2026 00:44:38 GMT"}, {"url": "https://www.iana.org/assignments/iana-ipv4-special-registry/iana-ipv4-special-registry-1.csv", "name": "iana-ipv4-special-registry-1", "rows": 26, "bytes": 2423, "sha256": "e3e39e76d00b1677335db8e9a805c7b9480ea2f4dc9e33f0b93cd3a905128d73", "changed": true, "http_status": 200, "last_modified": "Sat, 19 Sep 2026 00:44:47 GMT"}, {"url": "https://www.iana.org/assignments/iana-ipv6-special-registry/iana-ipv6-special-registry-1.csv", "name": "iana-ipv6-special-registry-1", "rows": 25, "bytes": 2289, "sha256": "775feea0621dec8735a44fbf30f762e721e8f0a1b3ab7eb341961a88cfce2139", "changed": true, "http_status": 200, "last_modified": "Sat, 19 Sep 2026 00:44:28 GMT"}, {"url": "https://www.iana.org/assignments/iana-as-numbers-special-registry/special-purpose-as-numbers.csv", "name": "special-purpose-as-numbers", "rows": 9, "bytes": 593, "sha256": "9f5eb353031860c4640a527bf0dd713341b5494759952998c8fac40cbbf367ce", "changed": true, "http_status": 200, "last_modified": "Sat, 19 Sep 2026 00:44:45 GMT"}, {"url": "https://data.iana.org/rdap/asn.json", "etag": "W/\\"1138-65336a3cb9688-br\\"", "name": "rdap-asn", "rows": 159, "bytes": 4408, "sha256": "80a0659f933b45130435c0bc7d6143ca2b815b3c5d2ef80c769b985a7821bb65", "changed": true, "http_status": 200, "publication": "2026-06-01T20:00:01Z", "last_modified": "Mon, 01 Jun 2026 20:00:01 GMT"}, {"url": "https://data.iana.org/rdap/ipv4.json", "etag": "W/\\"15fd-58ac0700ff080-br\\"", "name": "rdap-ipv4", "rows": 221, "bytes": 5629, "sha256": "f6ad30d1ebbd16fed6121e1eb54ae7db86edbf5e425345a040aa7893b0447d78", "changed": true, "http_status": 200, "publication": "2019-06-07T19:00:02Z", "last_modified": "Fri, 07 Jun 2019 19:00:02 GMT"}, {"url": "https://data.iana.org/rdap/ipv6.json", "etag": "W/\\"5c4-625e10e87eede-br\\"", "name": "rdap-ipv6", "rows": 34, "bytes": 1476, "sha256": "3292eb71787b903f7a6798ff31f7f2239fb61ce346be142a3719ffa26deaf080", "changed": true, "http_status": 200, "publication": "2024-11-01T22:00:01Z", "last_modified": "Fri, 01 Nov 2024 22:00:01 GMT"}]', '{"iana_asn_block": {"deleted": 0, "updated": 0, "inserted": 173}, "iana_special_asn": {"deleted": 0, "updated": 0, "inserted": 9}, "iana_prefix_block": {"deleted": 0, "updated": 0, "inserted": 307}, "iana_rdap_service": {"deleted": 0, "updated": 0, "inserted": 414}, "iana_special_prefix": {"deleted": 0, "updated": 0, "inserted": 51}}', '[]', NULL, '2026-09-29 00:49:01.613414+00', '2026-09-29 00:49:05.31793+00');
INSERT INTO iana_run (uuid, status, forced, sha256, files, changes, warnings, error, started_at, created_at) VALUES
    ('01a0e000-0000-7000-8000-000000000001', 1, false, repeat('a', 64), '[]', '{}', '[]', NULL,
     '2026-09-01 00:00:00+00', '2026-09-01 00:00:05+00'),
    ('01a0f000-0000-7000-8000-000000000002', 0, false, NULL, '[]', NULL, '[]', 'sanidade: exemplo de recusa',
     '2026-09-29 06:00:00+00', '2026-09-29 06:00:05+00');
INSERT INTO jobs (app, last_sync_at, last_check_at, consolidated) VALUES
    ('collector-iana', '2026-09-29 00:49:05.31793+00', '2026-09-29 06:00:05+00', 0);
