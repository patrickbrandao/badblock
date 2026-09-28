-- migrate:up

-- Camada registry: as tabelas centrais que o registry-sync reconstrói a partir
-- de ingest e que as APIs leem pelas views do schema api.
--
-- Nada é apagado: uma linha que some de todas as fontes ganha missing_since e,
-- passada a carência (REMOVAL_GRACE do registry-sync), removed_at. Se voltar,
-- os dois campos são limpos. Toda inserção, alteração, remoção e restauração
-- entra em registry.change_log.

-- Cada reconstrução do central é uma versão do dataset.
CREATE TABLE registry.dataset (
    id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    built_at      timestamptz NOT NULL DEFAULT now(),
    baseline      boolean     NOT NULL DEFAULT false,
    asn_count     bigint,
    prefix_count  bigint,
    holder_count  bigint,
    changes       bigint,
    duration_ms   bigint
);
COMMENT ON TABLE registry.dataset IS 'Versões do central; a maior é a atual. baseline = primeira carga, sem eventos por linha no change_log';

-- Titulares: um por (rir, opaque-id).
CREATE TABLE registry.holder (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    rir             text        NOT NULL,
    opaque_id       text        NOT NULL,
    name            text,
    name_source     text        CHECK (name_source IN ('nicbr', 'inferred')),
    document        text,
    country         text,
    asn_count       integer     NOT NULL DEFAULT 0,
    prefix4_count   integer     NOT NULL DEFAULT 0,
    prefix6_count   integer     NOT NULL DEFAULT 0,
    ipv4_addresses  bigint      NOT NULL DEFAULT 0,
    first_seen      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    missing_since   timestamptz,
    removed_at      timestamptz,
    UNIQUE (rir, opaque_id)
);
COMMENT ON TABLE registry.holder IS 'Titulares de recursos, identificados pelo opaque-id dos arquivos delegated';
COMMENT ON COLUMN registry.holder.name_source IS 'nicbr: nome oficial do NIC.br; inferred: nome mais frequente entre os ASNs do titular no asn.txt';
COMMENT ON COLUMN registry.holder.document IS 'CNPJ (ou id estrangeiro) publicado pelo NIC.br';
COMMENT ON COLUMN registry.holder.country IS 'País de registro mais frequente entre os recursos do titular';

-- ASNs: uma linha por número (as faixas dos RIRs são expandidas).
CREATE TABLE registry.asn (
    asn            bigint      PRIMARY KEY CHECK (asn BETWEEN 0 AND 4294967295),
    rir            text        NOT NULL,
    country        text,
    status         text        NOT NULL,
    registered     date,
    holder_id      bigint      REFERENCES registry.holder (id),
    name           text,
    name_source    text        CHECK (name_source IN ('nicbr', 'asnames')),
    handle         text,
    rdap_url       text,
    first_seen     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    missing_since  timestamptz,
    removed_at     timestamptz
);
CREATE INDEX asn_holder_idx  ON registry.asn (holder_id)    WHERE removed_at IS NULL;
CREATE INDEX asn_country_idx ON registry.asn (country, asn) WHERE removed_at IS NULL;
CREATE INDEX asn_rir_idx     ON registry.asn (rir, asn)     WHERE removed_at IS NULL;
COMMENT ON COLUMN registry.asn.country IS 'País de registro no RIR (não é geolocalização); NULL para ZZ';
COMMENT ON COLUMN registry.asn.name_source IS 'nicbr: ASN LACNIC/BR presente no NIC.br; asnames: asn.txt';

-- Blocos de ASN da IANA e de uso especial. Reconstruídos por inteiro a cada
-- versão, sem histórico: mudam raramente e não identificam titulares.
CREATE TABLE registry.asn_block (
    id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    level        text      NOT NULL CHECK (level IN ('iana', 'special')),
    asn_first    bigint    NOT NULL,
    asn_last     bigint    NOT NULL,
    asn_range    int8range GENERATED ALWAYS AS (int8range(asn_first, asn_last, '[]')) STORED,
    rir          text,
    designation  text      NOT NULL,
    reference    text,
    registered   date,
    whois        text,
    rdap_base    text,
    UNIQUE (level, asn_first)
);
CREATE INDEX asn_block_range_idx ON registry.asn_block USING gist (asn_range);

-- Prefixos de todos os níveis: iana (blocos da IANA para os RIRs), special
-- (special-purpose da IANA), rir (delegações) e nicbr (blocos do NIC.br que
-- não coincidem com uma delegação da LACNIC).
CREATE TABLE registry.prefix (
    id                  bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    level               text        NOT NULL CHECK (level IN ('iana', 'special', 'rir', 'nicbr')),
    prefix              cidr        NOT NULL,
    family              smallint    GENERATED ALWAYS AS (family(prefix)) STORED,
    rir                 text,
    country             text,
    status              text,
    registered          date,
    holder_id           bigint      REFERENCES registry.holder (id),
    nicbr_asns          bigint[],
    designation         text,
    special_rfc         text,
    globally_reachable  boolean,
    source_start        text,
    source_value        bigint,
    whois               text,
    rdap_url            text,
    first_seen          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    missing_since       timestamptz,
    removed_at          timestamptz,
    UNIQUE (level, prefix)
);
CREATE INDEX prefix_lookup_idx     ON registry.prefix USING gist (prefix inet_ops) WHERE removed_at IS NULL;
CREATE INDEX prefix_holder_idx     ON registry.prefix (holder_id, prefix) WHERE removed_at IS NULL;
CREATE INDEX prefix_country_idx    ON registry.prefix (country, prefix)
    WHERE removed_at IS NULL AND level IN ('rir', 'nicbr');
CREATE INDEX prefix_rir_idx        ON registry.prefix (rir, prefix)
    WHERE removed_at IS NULL AND level IN ('rir', 'nicbr');
CREATE INDEX prefix_nicbr_asns_idx ON registry.prefix USING gin (nicbr_asns)
    WHERE removed_at IS NULL AND nicbr_asns IS NOT NULL;
COMMENT ON COLUMN registry.prefix.nicbr_asns IS 'ASNs ligados explicitamente pelo NIC.br (vínculo nicbr)';
COMMENT ON COLUMN registry.prefix.source_start IS 'Início do registro delegated de origem, quando o CIDR veio da divisão de um bloco fora de CIDR';
COMMENT ON COLUMN registry.prefix.globally_reachable IS 'Nível special: coluna Globally Reachable da IANA';

-- Histórico de mudanças do central.
CREATE TABLE registry.change_log (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    dataset_id  bigint      NOT NULL REFERENCES registry.dataset (id),
    changed_at  timestamptz NOT NULL DEFAULT now(),
    entity      text        NOT NULL CHECK (entity IN ('asn', 'prefix', 'holder')),
    key         text        NOT NULL,
    level       text,
    action      text        NOT NULL CHECK (action IN ('insert', 'update', 'remove', 'restore')),
    before      jsonb,
    after       jsonb
);
CREATE INDEX change_log_entity_idx  ON registry.change_log (entity, key, changed_at DESC);
CREATE INDEX change_log_dataset_idx ON registry.change_log (dataset_id);
COMMENT ON COLUMN registry.change_log.key IS 'asn: número; prefix: CIDR; holder: rir:opaque-id';

-- Blocos /24 (IPv4) e /48 (IPv6) que contêm prefixos mais específicos. A API
-- usa a lista para decidir quando pode compartilhar o cache de um bloco.
CREATE TABLE registry.cache_bucket_exception (
    bucket cidr PRIMARY KEY
);

-- migrate:down

DROP TABLE registry.cache_bucket_exception;
DROP TABLE registry.change_log;
DROP TABLE registry.prefix;
DROP TABLE registry.asn_block;
DROP TABLE registry.asn;
DROP TABLE registry.holder;
DROP TABLE registry.dataset;
