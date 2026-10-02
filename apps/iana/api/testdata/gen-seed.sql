-- Gera o recorte de testdata/seed.sql a partir de um banco carregado pelo
-- collector-iana: psql -q -f testdata/gen-seed.sql "$POSTGRES_URL" (a parte
-- sintética do fim do seed.sql — iana_run extras e jobs — é escrita à mão).
\pset format unaligned
\pset tuples_only on
SELECT 'INSERT INTO iana_asn_block (asn_start, asn_end, description, registry, whois, rdap_urls, reference, registration_date, source_file) VALUES' || E'\n' ||
  string_agg(format('    (%s, %s, %L, %L, %L, %L, %L, %L, %L)', asn_start, asn_end, description, registry, whois, rdap_urls, reference, registration_date, source_file), E',\n' ORDER BY asn_start) || ';'
  FROM iana_asn_block
 WHERE asn_start IN (0, 1, 23456, 61440, 64496, 64512, 65535, 65536, 65552, 262144, 275869, 4200000000, 4294967295);

SELECT 'INSERT INTO iana_special_asn (asn_start, asn_end, reason, reference) VALUES' || E'\n' ||
  string_agg(format('    (%s, %s, %L, %L)', asn_start, asn_end, reason, reference), E',\n' ORDER BY asn_start) || ';'
  FROM iana_special_asn;

SELECT 'INSERT INTO iana_prefix_block (prefix, designation, registry, whois, rdap_urls, status, allocation_date, note, source_file) VALUES' || E'\n' ||
  string_agg(format('    (%L, %L, %L, %L, %L, %L, %L, %L, %L)', prefix, designation, registry, whois, rdap_urls, status, allocation_date, note, source_file), E',\n' ORDER BY family, prefix) || ';'
  FROM iana_prefix_block
 WHERE prefix = ANY (ARRAY['0.0.0.0/8', '8.0.0.0/8', '10.0.0.0/8', '45.0.0.0/8', '100.0.0.0/8', '127.0.0.0/8',
                           '192.0.0.0/8', '224.0.0.0/8', '240.0.0.0/8',
                           '2001::/23', '2001:c00::/23', '2002::/16', '2800::/12', '3ffe::/16', '3fff::/20']::cidr[]);

SELECT 'INSERT INTO iana_special_prefix (prefix, name, rfc, allocation_date, termination_date, source, destination, forwardable, globally_reachable, reserved_by_protocol) VALUES' || E'\n' ||
  string_agg(format('    (%L, %L, %L, %L, %L, %L, %L, %L, %L, %L)', prefix, name, rfc, allocation_date, termination_date,
                    source, destination, forwardable, globally_reachable, reserved_by_protocol), E',\n' ORDER BY family, prefix) || ';'
  FROM iana_special_prefix;

SELECT 'INSERT INTO iana_rdap_service (kind, resource, asn_start, asn_end, prefix, registry, urls) VALUES' || E'\n' ||
  string_agg(format('    (%L, %L, %L, %L, %L, %L, %L)', kind, resource, asn_start, asn_end, prefix, registry, urls), E',\n'
             ORDER BY kind, asn_start, prefix) || ';'
  FROM iana_rdap_service
 WHERE (kind = 'asn' AND resource IN ('1-1876', '61440-61951', '262144-263167'))
    OR (kind <> 'asn' AND prefix = ANY (ARRAY['8.0.0.0/8', '45.0.0.0/8', '100.0.0.0/8', '192.0.0.0/8', '2001:c00::/23', '2800::/12']::cidr[]));

SELECT format(E'INSERT INTO iana_run (uuid, status, forced, sha256, files, changes, warnings, error, started_at, created_at) VALUES\n    (%L, %s, %L, %L, %L, %L, %L, %L, %L, %L);',
              uuid, status, forced, sha256, files, changes, warnings, error, started_at, created_at)
  FROM iana_run WHERE status = 1;
