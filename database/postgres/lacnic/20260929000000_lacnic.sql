-- migrate:up

-- =============================================================================
-- lacnic: delegações de ASNs e blocos IP publicadas pela LACNIC
-- Depende de: central (função set_updated_at)
-- Tabelas: lacnic_run (tabela independente), lacnic_asn (tabela independente),
--          lacnic_prefix (tabela independente)
--
-- Escrita pelo collector-lacnic, lida pela api-lacnic. Fonte (formato
-- "delegated-extended", comum aos cinco RIRs):
--   https://ftp.lacnic.net/pub/stats/lacnic/delegated-lacnic-extended-latest
-- Uma linha por registro: registry|cc|type|start|value|date|status|opaque-id
--
-- Nota: as tabelas espelham o último arquivo aplicado. Um registro que some da
-- fonte é apagado aqui (sem soft delete); histórico e consolidação ficam para
-- as tabelas centrais da fase 2. A aplicação acontece numa transação só, então
-- a api-lacnic nunca vê um arquivo pela metade.
--
-- Nota: ASNs e blocos não têm ligação entre si no arquivo do RIR; o que une os
-- recursos de um mesmo titular é o opaque_id (identificador do titular dentro
-- do RIR), por isso as duas tabelas são independentes e indexam opaque_id.
-- =============================================================================

-- -----------------------------------------------------------------------------
-- Tabela: lacnic_run
-- -----------------------------------------------------------------------------
CREATE TABLE lacnic_run (
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

    CONSTRAINT chk_lacnic_run_status CHECK (status BETWEEN 0 AND 1),
    CONSTRAINT chk_lacnic_run_md5    CHECK (md5    ~ '^[0-9a-f]{32}$'),
    CONSTRAINT chk_lacnic_run_sha256 CHECK (sha256 ~ '^[0-9a-f]{64}$')
);

COMMENT ON TABLE lacnic_run IS
    'Histórico das execuções do collector-lacnic que baixaram um arquivo novo da
     fonte (aplicado ou recusado). Verificações sem mudança (inclusive arquivo
     mais antigo que o aplicado) não geram linha: ficam só em
     jobs.last_check_at. A última linha com status = 1 descreve o dataset que
     está nas tabelas lacnic_asn/lacnic_prefix, e seu uuid é a versão do dataset
     usada pela api-lacnic no cache e no ETag.';
COMMENT ON COLUMN lacnic_run.uuid            IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree. Nas linhas com status = 1 é também a versão do dataset.';
COMMENT ON COLUMN lacnic_run.status          IS 'Resultado (0=falhou | 1=aplicado). Falha: conferência do MD5, parser ou trava de sanidade recusaram o arquivo, e as tabelas ficaram como estavam.';
COMMENT ON COLUMN lacnic_run.forced          IS 'true quando a execução foi forçada (--force): ignora "arquivo igual ao último", "arquivo mais antigo que o aplicado" e a trava de remoção em massa.';
COMMENT ON COLUMN lacnic_run.url             IS 'URL de onde o arquivo foi baixado.';
COMMENT ON COLUMN lacnic_run.http_status     IS 'Status HTTP da resposta do arquivo. NULL = falhou antes de receber resposta.';
COMMENT ON COLUMN lacnic_run.etag            IS 'Cabeçalho ETag da resposta; reenviado em If-None-Match na próxima verificação.';
COMMENT ON COLUMN lacnic_run.last_modified   IS 'Cabeçalho Last-Modified da resposta, como texto HTTP; reenviado em If-Modified-Since.';
COMMENT ON COLUMN lacnic_run.md5             IS 'MD5 (hex minúsculo) do arquivo baixado. Comparado com o .md5 publicado ao lado do arquivo, que é a checagem mais barata de mudança. NULL = falhou antes do download.';
COMMENT ON COLUMN lacnic_run.sha256          IS 'SHA-256 (hex minúsculo) do arquivo baixado; detecta conteúdo igual quando o .md5 está fora do ar. NULL = falhou antes do download.';
COMMENT ON COLUMN lacnic_run.bytes           IS 'Tamanho do arquivo baixado, em bytes.';
COMMENT ON COLUMN lacnic_run.format_version  IS 'Versão do formato no cabeçalho do arquivo (campo version: 2 ou 2.3). NULL = cabeçalho não lido.';
COMMENT ON COLUMN lacnic_run.serial          IS 'Número de série do arquivo no cabeçalho (campo serial), como publicado pelo RIR (data AAAAMMDD ou época Unix, conforme o RIR). Serve, com end_date, para recusar um arquivo mais antigo que o aplicado.';
COMMENT ON COLUMN lacnic_run.header_records  IS 'Quantidade de registros declarada no cabeçalho (campo records), sem cabeçalho, linhas summary e comentários. Um arquivo cujos registros não batem com ela é recusado.';
COMMENT ON COLUMN lacnic_run.start_date      IS 'Início do período coberto pelo arquivo (campo startdate do cabeçalho). NULL = vazio ou 00000000.';
COMMENT ON COLUMN lacnic_run.end_date        IS 'Fim do período coberto pelo arquivo (campo enddate do cabeçalho). Serve, com serial, para recusar um arquivo mais antigo que o aplicado. NULL = vazio ou 00000000.';
COMMENT ON COLUMN lacnic_run.utc_offset      IS 'Fuso das datas do arquivo (campo UTCoffset do cabeçalho), como publicado (ex.: -0300).';
COMMENT ON COLUMN lacnic_run.asn_records     IS 'Registros de ASN aceitos (linhas type = asn), isto é, linhas gravadas em lacnic_asn.';
COMMENT ON COLUMN lacnic_run.ipv4_records    IS 'Registros IPv4 aceitos (linhas type = ipv4), antes da divisão em CIDRs.';
COMMENT ON COLUMN lacnic_run.ipv6_records    IS 'Registros IPv6 aceitos (linhas type = ipv6).';
COMMENT ON COLUMN lacnic_run.prefixes_v4     IS 'Blocos IPv4 gravados em lacnic_prefix, depois de dividir os registros que não formam um CIDR.';
COMMENT ON COLUMN lacnic_run.prefixes_v6     IS 'Blocos IPv6 gravados em lacnic_prefix.';
COMMENT ON COLUMN lacnic_run.asn_inserted    IS 'Registros de ASN novos gravados em lacnic_asn. NULL quando status = 0.';
COMMENT ON COLUMN lacnic_run.asn_updated     IS 'Registros de ASN com quantidade, país, data, status ou titular alterados. NULL quando status = 0.';
COMMENT ON COLUMN lacnic_run.asn_deleted     IS 'Registros de ASN que sumiram da fonte e foram apagados. NULL quando status = 0.';
COMMENT ON COLUMN lacnic_run.prefix_inserted IS 'Blocos novos gravados em lacnic_prefix. NULL quando status = 0.';
COMMENT ON COLUMN lacnic_run.prefix_updated  IS 'Blocos com país, data, status, titular ou registro de origem alterados. NULL quando status = 0.';
COMMENT ON COLUMN lacnic_run.prefix_deleted  IS 'Blocos que sumiram da fonte e foram apagados. NULL quando status = 0.';
COMMENT ON COLUMN lacnic_run.warnings        IS 'Lista JSON (array de strings) com os avisos do parser: linhas descartadas, registros duplicados, datas inválidas. Limitada às primeiras ocorrências.';
COMMENT ON COLUMN lacnic_run.error           IS 'Mensagem de erro quando status = 0. NULL quando aplicado.';
COMMENT ON COLUMN lacnic_run.started_at      IS 'Início da execução (antes do download).';
COMMENT ON COLUMN lacnic_run.created_at      IS 'Timestamp de criação do registro, gravado no fim da execução.';

-- -----------------------------------------------------------------------------
-- lacnic_run: versão atual do dataset (última execução aplicada)
-- -----------------------------------------------------------------------------
CREATE INDEX ix_lacnic_run_applied ON lacnic_run(created_at DESC) WHERE status = 1;

-- -----------------------------------------------------------------------------
-- Tabela: lacnic_asn
-- -----------------------------------------------------------------------------
CREATE TABLE lacnic_asn (
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

    CONSTRAINT uq_lacnic_asn_asn_start UNIQUE (asn_start),
    CONSTRAINT chk_lacnic_asn_range    CHECK (asn_start >= 0 AND asn_count >= 1 AND asn_start + asn_count - 1 <= 4294967295),
    CONSTRAINT chk_lacnic_asn_cc       CHECK (cc ~ '^[A-Z]{2}$'),
    CONSTRAINT chk_lacnic_asn_status   CHECK (status IN ('allocated', 'assigned', 'available', 'reserved'))
);

COMMENT ON TABLE lacnic_asn IS
    'Uma linha por registro de ASN (type = asn) do arquivo delegated-extended da
     LACNIC. Um registro cobre a faixa asn_start..asn_end (a maioria tem um ASN
     só). As faixas não se sobrepõem na fonte, então o registro que contém um
     ASN é o de maior asn_start <= ASN, conferindo asn_end >= ASN. Registro que
     some da fonte é apagado.';
COMMENT ON COLUMN lacnic_asn.uuid       IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree.';
COMMENT ON COLUMN lacnic_asn.asn_start  IS 'Primeiro ASN da faixa (campo start), 0 a 4294967295. Chave natural.';
COMMENT ON COLUMN lacnic_asn.asn_count  IS 'Quantidade de ASNs da faixa (campo value), no mínimo 1.';
COMMENT ON COLUMN lacnic_asn.asn_end    IS 'Último ASN da faixa (asn_start + asn_count - 1), calculado pelo banco.';
COMMENT ON COLUMN lacnic_asn.cc         IS 'País do titular (ISO 3166-1 alfa-2, maiúsculo; ZZ = sem país, como publicado). NULL = campo vazio na fonte (comum em available/reserved).';
COMMENT ON COLUMN lacnic_asn.reg_date   IS 'Data da alocação/designação (campo date). NULL = vazio ou 00000000 na fonte (registros antigos, available e reserved).';
COMMENT ON COLUMN lacnic_asn.status     IS 'Situação do registro (allocated=alocado a um LIR/ISP | assigned=designado a um usuário final | available=livre no estoque do RIR | reserved=reservado pelo RIR).';
COMMENT ON COLUMN lacnic_asn.opaque_id  IS 'Identificador do titular dentro do RIR (campo opaque-id): liga os ASNs e blocos de um mesmo titular (lacnic_prefix.opaque_id). Não é documento nem nome. NULL = vazio na fonte (available/reserved).';
COMMENT ON COLUMN lacnic_asn.created_at IS 'Timestamp de criação do registro (primeira vez que o registro apareceu na fonte).';
COMMENT ON COLUMN lacnic_asn.updated_at IS 'Timestamp de última alteração, determina a versão do registro; mantido pelo trigger trg_lacnic_asn_updated_at.';

CREATE TRIGGER trg_lacnic_asn_updated_at
    BEFORE UPDATE ON lacnic_asn
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

-- -----------------------------------------------------------------------------
-- lacnic_asn: registro que contém um ASN (api-lacnic /asn). Usa o índice único
-- de uq_lacnic_asn_asn_start, sem índice extra:
--   SELECT ... FROM lacnic_asn WHERE asn_start <= $1
--    ORDER BY asn_start DESC LIMIT 1          -- e conferir asn_end >= $1
-- -----------------------------------------------------------------------------

-- -----------------------------------------------------------------------------
-- lacnic_asn: recursos de um titular (api-lacnic, busca por opaque_id)
-- -----------------------------------------------------------------------------
CREATE INDEX ix_lacnic_asn_opaque_id ON lacnic_asn(opaque_id);

-- -----------------------------------------------------------------------------
-- Tabela: lacnic_prefix
-- -----------------------------------------------------------------------------
CREATE TABLE lacnic_prefix (
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

    CONSTRAINT uq_lacnic_prefix_prefix  UNIQUE (prefix),
    CONSTRAINT chk_lacnic_prefix_cc     CHECK (cc ~ '^[A-Z]{2}$'),
    CONSTRAINT chk_lacnic_prefix_status CHECK (status IN ('allocated', 'assigned', 'available', 'reserved')),
    CONSTRAINT chk_lacnic_prefix_record CHECK (family(record_start) = family(prefix) AND record_value >= 1)
);

COMMENT ON TABLE lacnic_prefix IS
    'Blocos IPv4 e IPv6 do arquivo delegated-extended da LACNIC, um CIDR por
     linha. Registro IPv6 vira um bloco (value = tamanho do prefixo). Registro
     IPv4 traz a quantidade de endereços: quando ela não forma um CIDR, o
     collector divide a faixa nos CIDRs mínimos (ex.: 62.122.208.0 + 1280 =
     /22 + /24), e cada pedaço repete os dados do registro. record_start e
     record_value guardam o registro original para quem precisar reconstruí-lo.
     Registro que some da fonte é apagado.';
COMMENT ON COLUMN lacnic_prefix.uuid         IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree.';
COMMENT ON COLUMN lacnic_prefix.prefix       IS 'Bloco em notação CIDR canônica. Chave natural.';
COMMENT ON COLUMN lacnic_prefix.family       IS 'Família do endereço, calculada pelo banco (4=IPv4 | 6=IPv6).';
COMMENT ON COLUMN lacnic_prefix.cc           IS 'País do titular (ISO 3166-1 alfa-2, maiúsculo; ZZ = sem país, como publicado). NULL = campo vazio na fonte (comum em available/reserved).';
COMMENT ON COLUMN lacnic_prefix.reg_date     IS 'Data da alocação/designação do registro (campo date). NULL = vazio ou 00000000 na fonte (registros antigos, available e reserved).';
COMMENT ON COLUMN lacnic_prefix.status       IS 'Situação do registro (allocated=alocado a um LIR/ISP | assigned=designado a um usuário final | available=livre no estoque do RIR | reserved=reservado pelo RIR).';
COMMENT ON COLUMN lacnic_prefix.opaque_id    IS 'Identificador do titular dentro do RIR (campo opaque-id): liga os blocos e ASNs de um mesmo titular (lacnic_asn.opaque_id). Não é documento nem nome. NULL = vazio na fonte (available/reserved).';
COMMENT ON COLUMN lacnic_prefix.record_start IS 'Endereço inicial do registro de origem (campo start). Igual ao endereço de prefix quando o registro formava um CIDR.';
COMMENT ON COLUMN lacnic_prefix.record_value IS 'Campo value do registro de origem: quantidade de endereços (IPv4) ou tamanho do prefixo (IPv6). Os blocos com o mesmo record_start formam o registro original.';
COMMENT ON COLUMN lacnic_prefix.created_at   IS 'Timestamp de criação do registro (primeira vez que o bloco apareceu na fonte).';
COMMENT ON COLUMN lacnic_prefix.updated_at   IS 'Timestamp de última alteração, determina a versão do registro; mantido pelo trigger trg_lacnic_prefix_updated_at.';

CREATE TRIGGER trg_lacnic_prefix_updated_at
    BEFORE UPDATE ON lacnic_prefix
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

-- -----------------------------------------------------------------------------
-- lacnic_prefix: bloco mais específico que contém um IP ou prefixo
-- (api-lacnic /ip e /prefix), com o operador >>= do inet:
--   SELECT ... FROM lacnic_prefix WHERE prefix >>= $1
--    ORDER BY masklen(prefix) DESC LIMIT 1
-- -----------------------------------------------------------------------------
CREATE INDEX ix_lacnic_prefix_prefix_gist ON lacnic_prefix USING gist (prefix inet_ops);

-- -----------------------------------------------------------------------------
-- lacnic_prefix: recursos de um titular (api-lacnic, busca por opaque_id)
-- -----------------------------------------------------------------------------
CREATE INDEX ix_lacnic_prefix_opaque_id ON lacnic_prefix(opaque_id);

-- migrate:down

DROP TABLE lacnic_prefix;
DROP TABLE lacnic_asn;
DROP TABLE lacnic_run;
