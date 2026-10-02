-- migrate:up

-- =============================================================================
-- arin: delegações de ASNs e blocos IP publicadas pela ARIN
-- Depende de: central (função set_updated_at)
-- Tabelas: arin_run (tabela independente), arin_asn (tabela independente),
--          arin_prefix (tabela independente)
--
-- Escrita pelo collector-arin, lida pela api-arin. Fonte (formato
-- "delegated-extended", comum aos cinco RIRs):
--   https://ftp.arin.net/pub/stats/arin/delegated-arin-extended-latest
-- Uma linha por registro: registry|cc|type|start|value|date|status|opaque-id
--
-- Nota: as tabelas espelham o último arquivo aplicado. Um registro que some da
-- fonte é apagado aqui (sem soft delete); histórico e consolidação ficam para
-- as tabelas centrais da fase 2. A aplicação acontece numa transação só, então
-- a api-arin nunca vê um arquivo pela metade.
--
-- Nota: ASNs e blocos não têm ligação entre si no arquivo do RIR; o que une os
-- recursos de um mesmo titular é o opaque_id (identificador do titular dentro
-- do RIR), por isso as duas tabelas são independentes e indexam opaque_id.
-- =============================================================================

-- -----------------------------------------------------------------------------
-- Tabela: arin_run
-- -----------------------------------------------------------------------------
CREATE TABLE arin_run (
    uuid               uuid        PRIMARY KEY DEFAULT uuidv7(),
    status             smallint    NOT NULL,
    forced             boolean     NOT NULL DEFAULT false,

    url                text        NOT NULL,
    http_status        smallint,
    etag               text,
    last_modified      text,
    md5                text,
    sha256             text,
    bytes              bigint,

    format_version     text,
    serial             text,
    header_records     integer,
    start_date         date,
    end_date           date,
    utc_offset         text,

    asn_records        integer,
    ipv4_records       integer,
    ipv6_records       integer,
    prefixes_v4        integer,
    prefixes_v6        integer,

    asn_inserted       integer,
    asn_updated        integer,
    asn_deleted        integer,
    prefix_inserted    integer,
    prefix_updated     integer,
    prefix_deleted     integer,

    warnings           jsonb       NOT NULL DEFAULT '[]'::jsonb,
    error              text,

    started_at         timestamptz NOT NULL,
    created_at         timestamptz NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_arin_run_status CHECK (status BETWEEN 0 AND 1),
    CONSTRAINT chk_arin_run_md5    CHECK (md5    ~ '^[0-9a-f]{32}$'),
    CONSTRAINT chk_arin_run_sha256 CHECK (sha256 ~ '^[0-9a-f]{64}$')
);

COMMENT ON TABLE arin_run IS
    'Histórico das execuções do collector-arin que baixaram um arquivo novo da
     fonte (aplicado ou recusado). Verificações sem mudança (inclusive arquivo
     mais antigo que o aplicado) não geram linha: ficam só em
     jobs.last_check_at. A última linha com status = 1 descreve o dataset que
     está nas tabelas arin_asn/arin_prefix, e seu uuid é a versão do dataset
     usada pela api-arin no cache e no ETag.';
COMMENT ON COLUMN arin_run.uuid            IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree. Nas linhas com status = 1 é também a versão do dataset.';
COMMENT ON COLUMN arin_run.status          IS 'Resultado (0=falhou | 1=aplicado). Falha: conferência do MD5, parser ou trava de sanidade recusaram o arquivo, e as tabelas ficaram como estavam.';
COMMENT ON COLUMN arin_run.forced          IS 'true quando a execução foi forçada (--force): ignora "arquivo igual ao último", "arquivo mais antigo que o aplicado" e a trava de remoção em massa.';
COMMENT ON COLUMN arin_run.url             IS 'URL de onde o arquivo foi baixado.';
COMMENT ON COLUMN arin_run.http_status     IS 'Status HTTP da resposta do arquivo. NULL = falhou antes de receber resposta.';
COMMENT ON COLUMN arin_run.etag            IS 'Cabeçalho ETag da resposta; reenviado em If-None-Match na próxima verificação.';
COMMENT ON COLUMN arin_run.last_modified   IS 'Cabeçalho Last-Modified da resposta, como texto HTTP; reenviado em If-Modified-Since.';
COMMENT ON COLUMN arin_run.md5             IS 'MD5 (hex minúsculo) do arquivo baixado. Comparado com o .md5 publicado ao lado do arquivo, que é a checagem mais barata de mudança. NULL = falhou antes do download.';
COMMENT ON COLUMN arin_run.sha256          IS 'SHA-256 (hex minúsculo) do arquivo baixado; detecta conteúdo igual quando o .md5 está fora do ar. NULL = falhou antes do download.';
COMMENT ON COLUMN arin_run.bytes           IS 'Tamanho do arquivo baixado, em bytes.';
COMMENT ON COLUMN arin_run.format_version  IS 'Versão do formato no cabeçalho do arquivo (campo version; 2.3 na ARIN). NULL = cabeçalho não lido.';
COMMENT ON COLUMN arin_run.serial          IS 'Número de série do arquivo no cabeçalho (campo serial), como publicado. Na ARIN não é uma data: é a época Unix em milissegundos da geração do arquivo (ex.: 1790600421096). Serve, com end_date, para recusar um arquivo mais antigo que o aplicado.';
COMMENT ON COLUMN arin_run.header_records  IS 'Quantidade de registros declarada no cabeçalho (campo records), sem cabeçalho, linhas summary e comentários. Um arquivo cujos registros não batem com ela é recusado.';
COMMENT ON COLUMN arin_run.start_date      IS 'Início do período coberto pelo arquivo (campo startdate do cabeçalho; 1970-01-01 na ARIN). NULL = vazio ou 00000000.';
COMMENT ON COLUMN arin_run.end_date        IS 'Fim do período coberto pelo arquivo (campo enddate do cabeçalho; na ARIN, o dia da publicação). Serve, com serial, para recusar um arquivo mais antigo que o aplicado. NULL = vazio ou 00000000.';
COMMENT ON COLUMN arin_run.utc_offset      IS 'Fuso das datas do arquivo (campo UTCoffset do cabeçalho), como publicado (na ARIN, -0400 no horário de verão de Nova York e -0500 no inverno).';
COMMENT ON COLUMN arin_run.asn_records     IS 'Registros de ASN aceitos (linhas type = asn), isto é, linhas gravadas em arin_asn.';
COMMENT ON COLUMN arin_run.ipv4_records    IS 'Registros IPv4 aceitos (linhas type = ipv4), antes da divisão em CIDRs.';
COMMENT ON COLUMN arin_run.ipv6_records    IS 'Registros IPv6 aceitos (linhas type = ipv6).';
COMMENT ON COLUMN arin_run.prefixes_v4     IS 'Blocos IPv4 gravados em arin_prefix, depois de dividir os registros que não formam um CIDR.';
COMMENT ON COLUMN arin_run.prefixes_v6     IS 'Blocos IPv6 gravados em arin_prefix.';
COMMENT ON COLUMN arin_run.asn_inserted    IS 'Registros de ASN novos gravados em arin_asn. NULL quando status = 0.';
COMMENT ON COLUMN arin_run.asn_updated     IS 'Registros de ASN com quantidade, país, data, status ou titular alterados. NULL quando status = 0.';
COMMENT ON COLUMN arin_run.asn_deleted     IS 'Registros de ASN que sumiram da fonte e foram apagados. NULL quando status = 0.';
COMMENT ON COLUMN arin_run.prefix_inserted IS 'Blocos novos gravados em arin_prefix. NULL quando status = 0.';
COMMENT ON COLUMN arin_run.prefix_updated  IS 'Blocos com país, data, status, titular ou registro de origem alterados. NULL quando status = 0.';
COMMENT ON COLUMN arin_run.prefix_deleted  IS 'Blocos que sumiram da fonte e foram apagados. NULL quando status = 0.';
COMMENT ON COLUMN arin_run.warnings        IS 'Lista JSON (array de strings) com os avisos do parser: linhas descartadas, registros duplicados, datas inválidas. Limitada às primeiras ocorrências.';
COMMENT ON COLUMN arin_run.error           IS 'Mensagem de erro quando status = 0. NULL quando aplicado.';
COMMENT ON COLUMN arin_run.started_at      IS 'Início da execução (antes do download).';
COMMENT ON COLUMN arin_run.created_at      IS 'Timestamp de criação do registro, gravado no fim da execução.';

-- -----------------------------------------------------------------------------
-- arin_run: versão atual do dataset (última execução aplicada)
-- -----------------------------------------------------------------------------
CREATE INDEX ix_arin_run_applied ON arin_run(created_at DESC) WHERE status = 1;

-- -----------------------------------------------------------------------------
-- Tabela: arin_asn
-- -----------------------------------------------------------------------------
CREATE TABLE arin_asn (
    uuid               uuid        PRIMARY KEY DEFAULT uuidv7(),
    asn_start          bigint      NOT NULL,
    asn_count          bigint      NOT NULL,
    asn_end            bigint      NOT NULL GENERATED ALWAYS AS (asn_start + asn_count - 1) STORED,
    cc                 text,
    reg_date           date,
    status             text        NOT NULL,
    opaque_id          text,

    created_at         timestamptz NOT NULL DEFAULT NOW(),
    updated_at         timestamptz NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_arin_asn_asn_start UNIQUE (asn_start),
    CONSTRAINT chk_arin_asn_range    CHECK (asn_start >= 0 AND asn_count >= 1 AND asn_start + asn_count - 1 <= 4294967295),
    CONSTRAINT chk_arin_asn_cc       CHECK (cc ~ '^[A-Z]{2}$'),
    CONSTRAINT chk_arin_asn_status   CHECK (status IN ('allocated', 'assigned', 'available', 'reserved'))
);

COMMENT ON TABLE arin_asn IS
    'Uma linha por registro de ASN (type = asn) do arquivo delegated-extended da
     ARIN. Um registro cobre a faixa asn_start..asn_end (a maioria tem um ASN
     só). As faixas não se sobrepõem na fonte, então o registro que contém um
     ASN é o de maior asn_start <= ASN, conferindo asn_end >= ASN. Registro que
     some da fonte é apagado.';
COMMENT ON COLUMN arin_asn.uuid       IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree.';
COMMENT ON COLUMN arin_asn.asn_start  IS 'Primeiro ASN da faixa (campo start), 0 a 4294967295. Chave natural.';
COMMENT ON COLUMN arin_asn.asn_count  IS 'Quantidade de ASNs da faixa (campo value), no mínimo 1.';
COMMENT ON COLUMN arin_asn.asn_end    IS 'Último ASN da faixa (asn_start + asn_count - 1), calculado pelo banco.';
COMMENT ON COLUMN arin_asn.cc         IS 'País do titular (ISO 3166-1 alfa-2, maiúsculo, como publicado). NULL = campo vazio na fonte (na ARIN, todos os available/reserved; ela não usa ZZ).';
COMMENT ON COLUMN arin_asn.reg_date   IS 'Data da designação (campo date). NULL = vazio ou 00000000 na fonte (available, reserved e ~100 ASNs antigos, ex.: AS3).';
COMMENT ON COLUMN arin_asn.status     IS 'Situação do registro (allocated=alocado | assigned=designado | available=livre no estoque do RIR | reserved=reservado pelo RIR). Na ARIN todo ASN delegado vem como assigned.';
COMMENT ON COLUMN arin_asn.opaque_id  IS 'Identificador do titular dentro do RIR (campo opaque-id; na ARIN, hash hex de 32 caracteres): liga os ASNs e blocos de um mesmo titular (arin_prefix.opaque_id). Não é documento nem nome. NULL = vazio na fonte (available/reserved).';
COMMENT ON COLUMN arin_asn.created_at IS 'Timestamp de criação do registro (primeira vez que o registro apareceu na fonte).';
COMMENT ON COLUMN arin_asn.updated_at IS 'Timestamp de última alteração, determina a versão do registro; mantido pelo trigger trg_arin_asn_updated_at.';

CREATE TRIGGER trg_arin_asn_updated_at
    BEFORE UPDATE ON arin_asn
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

-- -----------------------------------------------------------------------------
-- arin_asn: registro que contém um ASN (api-arin /asn). Usa o índice único
-- de uq_arin_asn_asn_start, sem índice extra:
--   SELECT ... FROM arin_asn WHERE asn_start <= $1
--    ORDER BY asn_start DESC LIMIT 1          -- e conferir asn_end >= $1
-- -----------------------------------------------------------------------------

-- -----------------------------------------------------------------------------
-- arin_asn: recursos de um titular (api-arin, busca por opaque_id)
-- -----------------------------------------------------------------------------
CREATE INDEX ix_arin_asn_opaque_id ON arin_asn(opaque_id);

-- -----------------------------------------------------------------------------
-- Tabela: arin_prefix
-- -----------------------------------------------------------------------------
CREATE TABLE arin_prefix (
    uuid               uuid        PRIMARY KEY DEFAULT uuidv7(),
    prefix             cidr        NOT NULL,
    family             smallint    NOT NULL GENERATED ALWAYS AS (family(prefix)) STORED,
    cc                 text,
    reg_date           date,
    status             text        NOT NULL,
    opaque_id          text,
    record_start       inet        NOT NULL,
    record_value       bigint      NOT NULL,

    created_at         timestamptz NOT NULL DEFAULT NOW(),
    updated_at         timestamptz NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_arin_prefix_prefix  UNIQUE (prefix),
    CONSTRAINT chk_arin_prefix_cc     CHECK (cc ~ '^[A-Z]{2}$'),
    CONSTRAINT chk_arin_prefix_status CHECK (status IN ('allocated', 'assigned', 'available', 'reserved')),
    CONSTRAINT chk_arin_prefix_record CHECK (family(record_start) = family(prefix) AND record_value >= 1)
);

COMMENT ON TABLE arin_prefix IS
    'Blocos IPv4 e IPv6 do arquivo delegated-extended da ARIN, um CIDR por
     linha. Registro IPv6 vira um bloco (value = tamanho do prefixo). Registro
     IPv4 traz a quantidade de endereços: quando ela não forma um CIDR (na
     ARIN, só em registros reserved), o collector divide a faixa nos CIDRs
     mínimos (ex.: 23.128.1.0 + 768 = /24 + /23), e cada pedaço repete os
     dados do registro. record_start e record_value guardam o registro
     original para quem precisar reconstruí-lo. Registro que some da fonte é
     apagado.';
COMMENT ON COLUMN arin_prefix.uuid         IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree.';
COMMENT ON COLUMN arin_prefix.prefix       IS 'Bloco em notação CIDR canônica. Chave natural.';
COMMENT ON COLUMN arin_prefix.family       IS 'Família do endereço, calculada pelo banco (4=IPv4 | 6=IPv6).';
COMMENT ON COLUMN arin_prefix.cc           IS 'País do titular (ISO 3166-1 alfa-2, maiúsculo, como publicado). NULL = campo vazio na fonte (na ARIN, todos os available/reserved; ela não usa ZZ).';
COMMENT ON COLUMN arin_prefix.reg_date     IS 'Data da alocação do registro (campo date). NULL = vazio ou 00000000 na fonte (available e reserved).';
COMMENT ON COLUMN arin_prefix.status       IS 'Situação do registro (allocated=alocado | assigned=designado | available=livre no estoque do RIR | reserved=reservado pelo RIR). Na ARIN todo bloco delegado vem como allocated, seja alocação a provedor ou designação a usuário final.';
COMMENT ON COLUMN arin_prefix.opaque_id    IS 'Identificador do titular dentro do RIR (campo opaque-id; na ARIN, hash hex de 32 caracteres): liga os blocos e ASNs de um mesmo titular (arin_asn.opaque_id). Não é documento nem nome. NULL = vazio na fonte (available/reserved).';
COMMENT ON COLUMN arin_prefix.record_start IS 'Endereço inicial do registro de origem (campo start). Igual ao endereço de prefix quando o registro formava um CIDR.';
COMMENT ON COLUMN arin_prefix.record_value IS 'Campo value do registro de origem: quantidade de endereços (IPv4) ou tamanho do prefixo (IPv6). Os blocos com o mesmo record_start formam o registro original.';
COMMENT ON COLUMN arin_prefix.created_at   IS 'Timestamp de criação do registro (primeira vez que o bloco apareceu na fonte).';
COMMENT ON COLUMN arin_prefix.updated_at   IS 'Timestamp de última alteração, determina a versão do registro; mantido pelo trigger trg_arin_prefix_updated_at.';

CREATE TRIGGER trg_arin_prefix_updated_at
    BEFORE UPDATE ON arin_prefix
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

-- -----------------------------------------------------------------------------
-- arin_prefix: bloco mais específico que contém um IP ou prefixo
-- (api-arin /ip e /prefix), com o operador >>= do inet:
--   SELECT ... FROM arin_prefix WHERE prefix >>= $1
--    ORDER BY masklen(prefix) DESC LIMIT 1
-- -----------------------------------------------------------------------------
CREATE INDEX ix_arin_prefix_prefix_gist ON arin_prefix USING gist (prefix inet_ops);

-- -----------------------------------------------------------------------------
-- arin_prefix: recursos de um titular (api-arin, busca por opaque_id)
-- -----------------------------------------------------------------------------
CREATE INDEX ix_arin_prefix_opaque_id ON arin_prefix(opaque_id);

-- migrate:down

DROP TABLE arin_prefix;
DROP TABLE arin_asn;
DROP TABLE arin_run;
