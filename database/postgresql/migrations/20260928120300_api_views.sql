-- migrate:up

-- Contrato de leitura das APIs. O registry-api só tem SELECT nestas views;
-- as tabelas por trás podem mudar desde que as views continuem iguais.

CREATE VIEW api.dataset AS
SELECT id AS version, built_at, baseline, asn_count, prefix_count, holder_count, changes
  FROM registry.dataset;

CREATE VIEW api.holder AS
SELECT id,
       rir,
       opaque_id,
       rir || ':' || opaque_id AS holder_key,
       name,
       name_source,
       document,
       country,
       asn_count,
       prefix4_count,
       prefix6_count,
       ipv4_addresses,
       first_seen,
       updated_at,
       removed_at
  FROM registry.holder;

CREATE VIEW api.asn AS
SELECT a.asn,
       a.rir,
       a.country,
       a.status,
       a.registered,
       a.name,
       a.name_source,
       a.handle,
       a.rdap_url,
       a.holder_id,
       h.rir || ':' || h.opaque_id AS holder_key,
       h.name        AS holder_name,
       h.name_source AS holder_name_source,
       h.document    AS holder_document,
       a.first_seen,
       a.updated_at,
       a.removed_at
  FROM registry.asn a
  LEFT JOIN registry.holder h ON h.id = a.holder_id;

CREATE VIEW api.asn_block AS
SELECT level, asn_first, asn_last, asn_range, rir, designation, reference, registered, whois, rdap_base
  FROM registry.asn_block;

CREATE VIEW api.prefix AS
SELECT p.id,
       p.level,
       p.prefix,
       p.family,
       p.rir,
       p.country,
       p.status,
       p.registered,
       p.holder_id,
       h.rir || ':' || h.opaque_id AS holder_key,
       h.name        AS holder_name,
       h.name_source AS holder_name_source,
       h.document    AS holder_document,
       p.nicbr_asns,
       p.designation,
       p.special_rfc,
       p.globally_reachable,
       p.source_start,
       p.source_value,
       p.whois,
       p.rdap_url,
       p.first_seen,
       p.updated_at,
       p.removed_at
  FROM registry.prefix p
  LEFT JOIN registry.holder h ON h.id = p.holder_id;

CREATE VIEW api.change_log AS
SELECT id, dataset_id, changed_at, entity, key, level, action, before, after
  FROM registry.change_log;

CREATE VIEW api.cache_bucket_exception AS
SELECT bucket FROM registry.cache_bucket_exception;

CREATE VIEW api.source_status AS
SELECT source_id,
       url,
       file_date,
       records,
       last_checked_at,
       last_changed_at,
       last_success_at,
       last_error,
       last_error_at
  FROM ingest.source_state;

-- migrate:down

DROP VIEW api.source_status;
DROP VIEW api.cache_bucket_exception;
DROP VIEW api.change_log;
DROP VIEW api.prefix;
DROP VIEW api.asn_block;
DROP VIEW api.asn;
DROP VIEW api.holder;
DROP VIEW api.dataset;
