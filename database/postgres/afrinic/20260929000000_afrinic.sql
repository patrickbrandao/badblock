-- migrate:up

-- =============================================================================
-- afrinic: delegações de ASNs e blocos IP publicadas pela AFRINIC
-- Depende de: central (função set_updated_at)
-- Tabelas: afrinic_run (tabela independente), afrinic_asn (tabela independente),
--          afrinic_prefix (tabela independente)
--
-- Escrita pelo collector-afrinic, lida pela api-afrinic. Fonte (formato
-- "delegated-extended", comum aos cinco RIRs):
--   https://ftp.afrinic.net/pub/stats/afrinic/delegated-afrinic-extended-latest
-- Uma linha por registro: registry|cc|type|start|value|date|status|opaque-id
--
-- Nota: as tabelas espelham o último arquivo aplicado. Um registro que some da
-- fonte é apagado aqui (sem soft delete); histórico e consolidação ficam para
-- as tabelas centrais da fase 2. A aplicação acontece numa transação só, então
-- a api-afrinic nunca vê um arquivo pela metade.
--
-- Nota: ASNs e blocos não têm ligação entre si no arquivo do RIR; o que une os
-- recursos de um mesmo titular é o opaque_id (identificador do titular dentro
-- do RIR), por isso as duas tabelas são independentes e indexam opaque_id.
-- =============================================================================

-- -----------------------------------------------------------------------------
-- Tabela: afrinic_run
-- -----------------------------------------------------------------------------
CREATE TABLE afrinic_run (
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

    CONSTRAINT chk_afrinic_run_status CHECK (status BETWEEN 0 AND 1),
    CONSTRAINT chk_afrinic_run_md5    CHECK (md5    ~ '^[0-9a-f]{32}$'),
    CONSTRAINT chk_afrinic_run_sha256 CHECK (sha256 ~ '^[0-9a-f]{64}$')
);

COMMENT ON TABLE afrinic_run IS
    'Histórico das execuções do collector-afrinic que baixaram um arquivo novo da
     fonte (aplicado ou recusado). Verificações sem mudança (inclusive arquivo
     mais antigo que o aplicado) não geram linha: ficam só em
     jobs.last_check_at. A última linha com status = 1 descreve o dataset que
     está nas tabelas afrinic_asn/afrinic_prefix, e seu uuid é a versão do dataset
     usada pela api-afrinic no cache e no ETag.';
COMMENT ON COLUMN afrinic_run.uuid            IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree. Nas linhas com status = 1 é também a versão do dataset.';
COMMENT ON COLUMN afrinic_run.status          IS 'Resultado (0=falhou | 1=aplicado). Falha: conferência do MD5, parser ou trava de sanidade recusaram o arquivo, e as tabelas ficaram como estavam.';
COMMENT ON COLUMN afrinic_run.forced          IS 'true quando a execução foi forçada (--force): ignora "arquivo igual ao último", "arquivo mais antigo que o aplicado" e a trava de remoção em massa.';
COMMENT ON COLUMN afrinic_run.url             IS 'URL de onde o arquivo foi baixado.';
COMMENT ON COLUMN afrinic_run.http_status     IS 'Status HTTP da resposta do arquivo. NULL = falhou antes de receber resposta.';
COMMENT ON COLUMN afrinic_run.etag            IS 'Cabeçalho ETag da resposta; reenviado em If-None-Match na próxima verificação.';
COMMENT ON COLUMN afrinic_run.last_modified   IS 'Cabeçalho Last-Modified da resposta, como texto HTTP; reenviado em If-Modified-Since.';
COMMENT ON COLUMN afrinic_run.md5             IS 'MD5 (hex minúsculo) do arquivo baixado. Comparado com o .md5 publicado ao lado do arquivo, que é a checagem mais barata de mudança. NULL = falhou antes do download.';
COMMENT ON COLUMN afrinic_run.sha256          IS 'SHA-256 (hex minúsculo) do arquivo baixado; detecta conteúdo igual quando o .md5 está fora do ar. NULL = falhou antes do download.';
COMMENT ON COLUMN afrinic_run.bytes           IS 'Tamanho do arquivo baixado, em bytes.';
COMMENT ON COLUMN afrinic_run.format_version  IS 'Versão do formato no cabeçalho do arquivo (campo version; a AFRINIC publica 2). NULL = cabeçalho não lido.';
COMMENT ON COLUMN afrinic_run.serial          IS 'Número de série do arquivo no cabeçalho (campo serial), como publicado (na AFRINIC, a data AAAAMMDD do arquivo, igual a end_date). Serve, com end_date, para recusar um arquivo mais antigo que o aplicado.';
COMMENT ON COLUMN afrinic_run.header_records  IS 'Quantidade de registros declarada no cabeçalho (campo records), sem cabeçalho, linhas summary e comentários. Um arquivo cujos registros não batem com ela é recusado.';
COMMENT ON COLUMN afrinic_run.start_date      IS 'Início do período coberto pelo arquivo (campo startdate do cabeçalho). NULL = vazio ou 00000000 (a AFRINIC publica 00000000).';
COMMENT ON COLUMN afrinic_run.end_date        IS 'Fim do período coberto pelo arquivo (campo enddate do cabeçalho). Serve, com serial, para recusar um arquivo mais antigo que o aplicado. NULL = vazio ou 00000000.';
COMMENT ON COLUMN afrinic_run.utc_offset      IS 'Fuso das datas do arquivo (campo UTCoffset do cabeçalho), como publicado (a AFRINIC publica 00000).';
COMMENT ON COLUMN afrinic_run.asn_records     IS 'Registros de ASN aceitos (linhas type = asn), isto é, linhas gravadas em afrinic_asn.';
COMMENT ON COLUMN afrinic_run.ipv4_records    IS 'Registros IPv4 aceitos (linhas type = ipv4), antes da divisão em CIDRs.';
COMMENT ON COLUMN afrinic_run.ipv6_records    IS 'Registros IPv6 aceitos (linhas type = ipv6).';
COMMENT ON COLUMN afrinic_run.prefixes_v4     IS 'Blocos IPv4 gravados em afrinic_prefix, depois de dividir os registros que não formam um CIDR.';
COMMENT ON COLUMN afrinic_run.prefixes_v6     IS 'Blocos IPv6 gravados em afrinic_prefix.';
COMMENT ON COLUMN afrinic_run.asn_inserted    IS 'Registros de ASN novos gravados em afrinic_asn. NULL quando status = 0.';
COMMENT ON COLUMN afrinic_run.asn_updated     IS 'Registros de ASN com quantidade, país, data, status ou titular alterados. NULL quando status = 0.';
COMMENT ON COLUMN afrinic_run.asn_deleted     IS 'Registros de ASN que sumiram da fonte e foram apagados. NULL quando status = 0.';
COMMENT ON COLUMN afrinic_run.prefix_inserted IS 'Blocos novos gravados em afrinic_prefix. NULL quando status = 0.';
COMMENT ON COLUMN afrinic_run.prefix_updated  IS 'Blocos com país, data, status, titular ou registro de origem alterados. NULL quando status = 0.';
COMMENT ON COLUMN afrinic_run.prefix_deleted  IS 'Blocos que sumiram da fonte e foram apagados. NULL quando status = 0.';
COMMENT ON COLUMN afrinic_run.warnings        IS 'Lista JSON (array de strings) com os avisos do parser: linhas descartadas, registros duplicados, datas inválidas. Limitada às primeiras ocorrências.';
COMMENT ON COLUMN afrinic_run.error           IS 'Mensagem de erro quando status = 0. NULL quando aplicado.';
COMMENT ON COLUMN afrinic_run.started_at      IS 'Início da execução (antes do download).';
COMMENT ON COLUMN afrinic_run.created_at      IS 'Timestamp de criação do registro, gravado no fim da execução.';

-- -----------------------------------------------------------------------------
-- afrinic_run: versão atual do dataset (última execução aplicada)
-- -----------------------------------------------------------------------------
CREATE INDEX ix_afrinic_run_applied ON afrinic_run(created_at DESC) WHERE status = 1;

-- -----------------------------------------------------------------------------
-- Tabela: afrinic_asn
-- -----------------------------------------------------------------------------
CREATE TABLE afrinic_asn (
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

    CONSTRAINT uq_afrinic_asn_asn_start UNIQUE (asn_start),
    CONSTRAINT chk_afrinic_asn_range    CHECK (asn_start >= 0 AND asn_count >= 1 AND asn_start + asn_count - 1 <= 4294967295),
    CONSTRAINT chk_afrinic_asn_cc       CHECK (cc ~ '^[A-Z]{2}$'),
    CONSTRAINT chk_afrinic_asn_status   CHECK (status IN ('allocated', 'assigned', 'available', 'reserved'))
);

COMMENT ON TABLE afrinic_asn IS
    'Uma linha por registro de ASN (type = asn) do arquivo delegated-extended da
     AFRINIC. Um registro cobre a faixa asn_start..asn_end (na AFRINIC, todos
     têm um ASN só). As faixas não se sobrepõem na fonte, então o registro que contém um
     ASN é o de maior asn_start <= ASN, conferindo asn_end >= ASN. Registro que
     some da fonte é apagado.';
COMMENT ON COLUMN afrinic_asn.uuid       IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree.';
COMMENT ON COLUMN afrinic_asn.asn_start  IS 'Primeiro ASN da faixa (campo start), 0 a 4294967295. Chave natural.';
COMMENT ON COLUMN afrinic_asn.asn_count  IS 'Quantidade de ASNs da faixa (campo value), no mínimo 1.';
COMMENT ON COLUMN afrinic_asn.asn_end    IS 'Último ASN da faixa (asn_start + asn_count - 1), calculado pelo banco.';
COMMENT ON COLUMN afrinic_asn.cc         IS 'País do titular (ISO 3166-1 alfa-2, maiúsculo). ZZ = sem país, como publicado (a AFRINIC usa ZZ em todo available/reserved). NULL = campo vazio na fonte.';
COMMENT ON COLUMN afrinic_asn.reg_date   IS 'Data da alocação/designação (campo date). NULL = vazio ou 00000000 na fonte (na AFRINIC, só available e reserved).';
COMMENT ON COLUMN afrinic_asn.status     IS 'Situação do registro (allocated=alocado a um LIR/ISP | assigned=designado a um usuário final | available=livre no estoque do RIR | reserved=reservado pelo RIR).';
COMMENT ON COLUMN afrinic_asn.opaque_id  IS 'Identificador do titular dentro do RIR (campo opaque-id): liga os ASNs e blocos de um mesmo titular (afrinic_prefix.opaque_id). Não é documento nem nome. NULL = vazio na fonte (available/reserved).';
COMMENT ON COLUMN afrinic_asn.created_at IS 'Timestamp de criação do registro (primeira vez que o registro apareceu na fonte).';
COMMENT ON COLUMN afrinic_asn.updated_at IS 'Timestamp de última alteração, determina a versão do registro; mantido pelo trigger trg_afrinic_asn_updated_at.';

CREATE TRIGGER trg_afrinic_asn_updated_at
    BEFORE UPDATE ON afrinic_asn
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

-- -----------------------------------------------------------------------------
-- afrinic_asn: registro que contém um ASN (api-afrinic /asn). Usa o índice único
-- de uq_afrinic_asn_asn_start, sem índice extra:
--   SELECT ... FROM afrinic_asn WHERE asn_start <= $1
--    ORDER BY asn_start DESC LIMIT 1          -- e conferir asn_end >= $1
-- -----------------------------------------------------------------------------

-- -----------------------------------------------------------------------------
-- afrinic_asn: recursos de um titular (api-afrinic, busca por opaque_id)
-- -----------------------------------------------------------------------------
CREATE INDEX ix_afrinic_asn_opaque_id ON afrinic_asn(opaque_id);

-- -----------------------------------------------------------------------------
-- Tabela: afrinic_prefix
-- -----------------------------------------------------------------------------
CREATE TABLE afrinic_prefix (
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

    CONSTRAINT uq_afrinic_prefix_prefix  UNIQUE (prefix),
    CONSTRAINT chk_afrinic_prefix_cc     CHECK (cc ~ '^[A-Z]{2}$'),
    CONSTRAINT chk_afrinic_prefix_status CHECK (status IN ('allocated', 'assigned', 'available', 'reserved')),
    CONSTRAINT chk_afrinic_prefix_record CHECK (family(record_start) = family(prefix) AND record_value >= 1)
);

COMMENT ON TABLE afrinic_prefix IS
    'Blocos IPv4 e IPv6 do arquivo delegated-extended da AFRINIC, um CIDR por
     linha. Registro IPv6 vira um bloco (value = tamanho do prefixo). Registro
     IPv4 traz a quantidade de endereços: quando ela não forma um CIDR, o
     collector divide a faixa nos CIDRs mínimos (ex.: 196.4.20.0 + 2560 =
     /22 + /22 + /23), e cada pedaço repete os dados do registro. record_start e
     record_value guardam o registro original para quem precisar reconstruí-lo.
     Registro que some da fonte é apagado.';
COMMENT ON COLUMN afrinic_prefix.uuid         IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree.';
COMMENT ON COLUMN afrinic_prefix.prefix       IS 'Bloco em notação CIDR canônica. Chave natural.';
COMMENT ON COLUMN afrinic_prefix.family       IS 'Família do endereço, calculada pelo banco (4=IPv4 | 6=IPv6).';
COMMENT ON COLUMN afrinic_prefix.cc           IS 'País do titular (ISO 3166-1 alfa-2, maiúsculo). ZZ = sem país, como publicado (a AFRINIC usa ZZ em todo available/reserved). NULL = campo vazio na fonte.';
COMMENT ON COLUMN afrinic_prefix.reg_date     IS 'Data da alocação/designação do registro (campo date). NULL = vazio ou 00000000 na fonte (na AFRINIC, só available e reserved).';
COMMENT ON COLUMN afrinic_prefix.status       IS 'Situação do registro (allocated=alocado a um LIR/ISP | assigned=designado a um usuário final | available=livre no estoque do RIR | reserved=reservado pelo RIR).';
COMMENT ON COLUMN afrinic_prefix.opaque_id    IS 'Identificador do titular dentro do RIR (campo opaque-id): liga os blocos e ASNs de um mesmo titular (afrinic_asn.opaque_id). Não é documento nem nome. NULL = vazio na fonte (available/reserved).';
COMMENT ON COLUMN afrinic_prefix.record_start IS 'Endereço inicial do registro de origem (campo start). Igual ao endereço de prefix quando o registro formava um CIDR.';
COMMENT ON COLUMN afrinic_prefix.record_value IS 'Campo value do registro de origem: quantidade de endereços (IPv4) ou tamanho do prefixo (IPv6). Os blocos com o mesmo record_start formam o registro original.';
COMMENT ON COLUMN afrinic_prefix.created_at   IS 'Timestamp de criação do registro (primeira vez que o bloco apareceu na fonte).';
COMMENT ON COLUMN afrinic_prefix.updated_at   IS 'Timestamp de última alteração, determina a versão do registro; mantido pelo trigger trg_afrinic_prefix_updated_at.';

CREATE TRIGGER trg_afrinic_prefix_updated_at
    BEFORE UPDATE ON afrinic_prefix
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

-- -----------------------------------------------------------------------------
-- afrinic_prefix: bloco mais específico que contém um IP ou prefixo
-- (api-afrinic /ip e /prefix), com o operador >>= do inet:
--   SELECT ... FROM afrinic_prefix WHERE prefix >>= $1
--    ORDER BY masklen(prefix) DESC LIMIT 1
-- -----------------------------------------------------------------------------
CREATE INDEX ix_afrinic_prefix_prefix_gist ON afrinic_prefix USING gist (prefix inet_ops);

-- -----------------------------------------------------------------------------
-- afrinic_prefix: recursos de um titular (api-afrinic, busca por opaque_id)
-- -----------------------------------------------------------------------------
CREATE INDEX ix_afrinic_prefix_opaque_id ON afrinic_prefix(opaque_id);

-- migrate:down

DROP TABLE afrinic_prefix;
DROP TABLE afrinic_asn;
DROP TABLE afrinic_run;
