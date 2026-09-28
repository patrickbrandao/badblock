-- Recorte pequeno e coerente do central, para os testes de integração da API.
-- Roda como badblock_owner depois das migrations.

INSERT INTO registry.dataset (baseline, asn_count, prefix_count, holder_count, changes) VALUES (true, 2, 8, 2, 0);

INSERT INTO registry.holder (rir, opaque_id, name, name_source, document, country, asn_count, prefix4_count, prefix6_count, ipv4_addresses)
VALUES ('lacnic', '258500', 'TMSoft Solucoes em Informatica Ltda', 'nicbr', '08.030.063/0001-00', 'BR', 1, 2, 1, 2048),
       ('arin', '9d99e3f7d38d1b8026f2ebbea4017c9f', 'Google LLC', 'inferred', NULL, 'US', 1, 1, 0, 256);

INSERT INTO registry.asn (asn, rir, country, status, registered, holder_id, name, name_source, handle, rdap_url)
SELECT 61613, 'lacnic', 'BR', 'allocated', '2023-05-05', id, 'TMSoft Solucoes em Informatica Ltda', 'nicbr', 'AS61613',
       'https://rdap.lacnic.net/rdap/autnum/61613'
  FROM registry.holder WHERE opaque_id = '258500';
INSERT INTO registry.asn (asn, rir, country, status, registered, holder_id, name, name_source, handle, rdap_url)
SELECT 15169, 'arin', 'US', 'assigned', '2000-03-30', id, 'Google LLC', 'asnames', 'GOOGLE',
       'https://rdap.arin.net/registry/autnum/15169'
  FROM registry.holder WHERE rir = 'arin';

INSERT INTO registry.prefix (level, prefix, rir, country, status, registered, holder_id, nicbr_asns, rdap_url)
SELECT 'rir', p::cidr, 'lacnic', 'BR', 'allocated', '2019-02-11', h.id, ARRAY[61613::bigint],
       'https://rdap.lacnic.net/rdap/ip/' || p
  FROM registry.holder h, unnest(ARRAY['45.171.60.0/22', '200.192.152.0/22', '2804:5964::/32']) AS p
 WHERE h.opaque_id = '258500';
INSERT INTO registry.prefix (level, prefix, rir, country, status, registered, holder_id, rdap_url)
SELECT 'rir', '8.8.8.0/24', 'arin', 'US', 'allocated', '2023-12-28', id, 'https://rdap.arin.net/registry/ip/8.8.8.0/24'
  FROM registry.holder WHERE rir = 'arin';
INSERT INTO registry.prefix (level, prefix, status, designation) VALUES
    ('iana', '45.0.0.0/8', 'legacy', 'Administered by ARIN'),
    ('iana', '8.0.0.0/8', 'legacy', 'Administered by ARIN'),
    ('iana', '10.0.0.0/8', 'reserved', 'IANA - Private Use');
UPDATE registry.prefix SET rir = 'arin' WHERE level = 'iana' AND prefix IN ('45.0.0.0/8', '8.0.0.0/8');
INSERT INTO registry.prefix (level, prefix, designation, globally_reachable) VALUES ('special', '10.0.0.0/8', 'Private-Use', false);
-- Removido: não pode aparecer nas consultas.
INSERT INTO registry.prefix (level, prefix, rir, country, status, removed_at)
VALUES ('rir', '200.1.0.0/16', 'lacnic', 'BR', 'allocated', now());

INSERT INTO registry.asn_block (level, asn_first, asn_last, rir, designation) VALUES
    ('iana', 61440, 61951, 'lacnic', 'Assigned by LACNIC'),
    ('iana', 64512, 65534, NULL, 'Reserved for Private Use'),
    ('special', 64512, 65534, NULL, 'For private use; reserved by [RFC6996]');

INSERT INTO registry.change_log (dataset_id, entity, key, action, before, after)
SELECT id, 'asn', '61613', 'update', '{"name": "TMSoft Antiga"}', '{"name": "TMSoft Solucoes em Informatica Ltda"}'
  FROM registry.dataset;

INSERT INTO registry.cache_bucket_exception (bucket) VALUES ('192.0.0.0/24');

INSERT INTO ingest.source_state (source_id, url, records, last_checked_at, last_success_at)
VALUES ('rir-lacnic', 'https://ftp.lacnic.net/pub/stats/lacnic/delegated-lacnic-extended-latest', 97301, now(), now());
