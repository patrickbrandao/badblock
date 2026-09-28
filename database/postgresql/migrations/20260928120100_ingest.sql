-- migrate:up

-- Camada ingest: o que cada fonte publicou, já normalizado, mais o controle
-- de download e o histórico de execuções. Toda tabela de dados tem source_id,
-- o id da fonte no catálogo do registry-sync: cada carga substitui só as linhas
-- da própria fonte.

-- Estado de cada fonte (uma linha por fonte do catálogo).
CREATE TABLE ingest.source_state (
    source_id        text PRIMARY KEY,
    url              text,
    etag             text,
    last_modified    text,
    content_sha256   text,
    file_date        date,
    records          bigint,
    last_checked_at  timestamptz,
    last_changed_at  timestamptz,
    last_success_at  timestamptz,
    last_error       text,
    last_error_at    timestamptz,
    updated_at       timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE ingest.source_state IS 'Estado de download e aplicação de cada fonte';
COMMENT ON COLUMN ingest.source_state.url IS 'Última URL baixada com sucesso (primária ou espelho)';
COMMENT ON COLUMN ingest.source_state.etag IS 'ETag da última resposta 200 dessa URL, para GET condicional';
COMMENT ON COLUMN ingest.source_state.content_sha256 IS 'SHA-256 do último conteúdo aplicado';
COMMENT ON COLUMN ingest.source_state.file_date IS 'Data do arquivo segundo o cabeçalho (delegated); impede aplicar arquivo mais antigo';
COMMENT ON COLUMN ingest.source_state.last_changed_at IS 'Última vez que um conteúdo novo foi aplicado';

-- Uma linha por verificação de fonte.
CREATE TABLE ingest.source_run (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    source_id       text        NOT NULL,
    started_at      timestamptz NOT NULL,
    finished_at     timestamptz NOT NULL,
    outcome         text        NOT NULL
                    CHECK (outcome IN ('unchanged', 'applied', 'aborted', 'failed', 'stale')),
    url             text,
    http_status     integer,
    bytes           bigint,
    content_sha256  text,
    file_date       date,
    records         bigint,
    inserted        bigint,
    updated         bigint,
    deleted         bigint,
    message         text
);
CREATE INDEX source_run_source_idx ON ingest.source_run (source_id, started_at DESC);
COMMENT ON TABLE ingest.source_run IS 'Histórico de verificações: unchanged (304 ou mesmo hash), applied, aborted (trava de remoção), failed, stale (arquivo mais antigo que o aplicado)';

-- Registros dos arquivos delegated-extended dos cinco RIRs.
CREATE TABLE ingest.delegation (
    source_id  text   NOT NULL,
    rir        text   NOT NULL CHECK (rir IN ('afrinic', 'apnic', 'arin', 'lacnic', 'ripencc')),
    rtype      text   NOT NULL CHECK (rtype IN ('asn', 'ipv4', 'ipv6')),
    start      text   NOT NULL,
    value      bigint NOT NULL CHECK (value > 0),
    cc         text   NOT NULL DEFAULT '',
    reg_date   date,
    status     text   NOT NULL,
    opaque_id  text   NOT NULL DEFAULT '',
    asn_first  bigint,
    asn_last   bigint,
    cidrs      cidr[],
    PRIMARY KEY (source_id, rtype, start),
    CHECK ((rtype = 'asn') = (asn_first IS NOT NULL AND asn_last IS NOT NULL)),
    CHECK ((rtype = 'asn') = (cidrs IS NULL))
);
CREATE INDEX delegation_holder_idx ON ingest.delegation (rir, opaque_id) WHERE opaque_id <> '';
CREATE INDEX delegation_asn_idx ON ingest.delegation (asn_first, asn_last) WHERE rtype = 'asn';
COMMENT ON TABLE ingest.delegation IS 'Registros delegated-extended (registry|cc|type|start|value|date|status|opaque-id)';
COMMENT ON COLUMN ingest.delegation.start IS 'Início do recurso como publicado: número do ASN ou endereço IP canônico';
COMMENT ON COLUMN ingest.delegation.value IS 'asn: quantidade de ASNs; ipv4: quantidade de endereços; ipv6: tamanho do prefixo';
COMMENT ON COLUMN ingest.delegation.opaque_id IS 'Identificador opaco do titular no RIR (mesmo valor = mesmo titular)';
COMMENT ON COLUMN ingest.delegation.cidrs IS 'CIDRs que cobrem o registro; um registro IPv4 fora de CIDR vira vários';

-- Blocos de ASN da IANA (as-numbers-1.csv e as-numbers-2.csv).
CREATE TABLE ingest.iana_asn_block (
    source_id    text   NOT NULL,
    asn_first    bigint NOT NULL,
    asn_last     bigint NOT NULL,
    description  text   NOT NULL,
    whois        text   NOT NULL DEFAULT '',
    rdap         text   NOT NULL DEFAULT '',
    reference    text   NOT NULL DEFAULT '',
    reg_date     date,
    PRIMARY KEY (source_id, asn_first),
    CHECK (asn_first <= asn_last)
);

-- Blocos IPv4 (/8) e IPv6 unicast da IANA.
CREATE TABLE ingest.iana_ip_block (
    source_id    text NOT NULL,
    prefix       cidr NOT NULL,
    designation  text NOT NULL,
    reg_date     date,
    whois        text NOT NULL DEFAULT '',
    rdap         text NOT NULL DEFAULT '',
    status       text NOT NULL,
    note         text NOT NULL DEFAULT '',
    PRIMARY KEY (source_id, prefix)
);

-- Registros special-purpose da IANA (IPv4 e IPv6).
CREATE TABLE ingest.iana_special_ip (
    source_id             text NOT NULL,
    prefix                cidr NOT NULL,
    name                  text NOT NULL,
    rfc                   text NOT NULL DEFAULT '',
    alloc_date            date,
    termination           text NOT NULL DEFAULT '',
    source_ok             boolean,
    destination_ok        boolean,
    forwardable           boolean,
    globally_reachable    boolean,
    reserved_by_protocol  boolean,
    PRIMARY KEY (source_id, prefix)
);
COMMENT ON COLUMN ingest.iana_special_ip.globally_reachable IS 'NULL quando a IANA publica N/A ou deixa em branco';

-- ASNs de uso especial da IANA.
CREATE TABLE ingest.iana_special_asn (
    source_id  text   NOT NULL,
    asn_first  bigint NOT NULL,
    asn_last   bigint NOT NULL,
    reason     text   NOT NULL,
    reference  text   NOT NULL DEFAULT '',
    PRIMARY KEY (source_id, asn_first),
    CHECK (asn_first <= asn_last)
);

-- Bootstrap RDAP da IANA (RFC 9224): servidor RDAP de cada faixa.
CREATE TABLE ingest.rdap_service (
    source_id  text NOT NULL,
    entry      text NOT NULL,
    asn_first  bigint,
    asn_last   bigint,
    prefix     cidr,
    base_url   text NOT NULL,
    PRIMARY KEY (source_id, entry),
    CHECK ((asn_first IS NULL) <> (prefix IS NULL))
);

-- NIC.br (nicbr-asn-blk): ASN, nome e documento do titular e seus blocos.
CREATE TABLE ingest.nicbr_asn (
    source_id  text   NOT NULL,
    asn        bigint NOT NULL,
    name       text   NOT NULL,
    document   text   NOT NULL DEFAULT '',
    PRIMARY KEY (source_id, asn)
);
CREATE INDEX nicbr_asn_asn_idx ON ingest.nicbr_asn (asn);

CREATE TABLE ingest.nicbr_prefix (
    source_id  text   NOT NULL,
    asn        bigint NOT NULL,
    prefix     cidr   NOT NULL,
    PRIMARY KEY (source_id, asn, prefix)
);
CREATE INDEX nicbr_prefix_prefix_idx ON ingest.nicbr_prefix (prefix);

-- Nomes de AS de todo o mundo (ftp.ripe.net/ripe/asnames/asn.txt).
CREATE TABLE ingest.asname (
    source_id  text   NOT NULL,
    asn        bigint NOT NULL,
    handle     text   NOT NULL,
    name       text   NOT NULL DEFAULT '',
    cc         text   NOT NULL DEFAULT '',
    PRIMARY KEY (source_id, asn)
);
CREATE INDEX asname_asn_idx ON ingest.asname (asn);

-- migrate:down

DROP TABLE ingest.asname;
DROP TABLE ingest.nicbr_prefix;
DROP TABLE ingest.nicbr_asn;
DROP TABLE ingest.rdap_service;
DROP TABLE ingest.iana_special_asn;
DROP TABLE ingest.iana_special_ip;
DROP TABLE ingest.iana_ip_block;
DROP TABLE ingest.iana_asn_block;
DROP TABLE ingest.delegation;
DROP TABLE ingest.source_run;
DROP TABLE ingest.source_state;
