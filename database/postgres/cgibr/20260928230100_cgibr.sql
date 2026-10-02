-- migrate:up

-- =============================================================================
-- cgibr: ASNs e blocos IP brasileiros publicados pelo NIC.br (registro.br)
-- Depende de: central (função set_updated_at)
-- Tabelas: cgibr_run (tabela independente), cgibr_asn, cgibr_prefix
--
-- Escrita pelo collector-cgibr, lida pela api-cgibr. Fonte:
--   https://ftp.registro.br/pub/numeracao/origin/nicbr-asn-blk-latest.txt
-- Uma linha por ASN: ASN|nome|documento|bloco|bloco|...
--
-- Nota: as tabelas espelham o último arquivo aplicado. Uma linha que some da
-- fonte é apagada aqui (sem soft delete); histórico e consolidação ficam para
-- as tabelas centrais da fase 2. A aplicação acontece numa transação só, então
-- a api-cgibr nunca vê um arquivo pela metade.
-- =============================================================================

-- -----------------------------------------------------------------------------
-- Tabela: cgibr_run
-- -----------------------------------------------------------------------------
CREATE TABLE cgibr_run (
    uuid               uuid        PRIMARY KEY DEFAULT uuidv7(),
    status             smallint    NOT NULL,
    forced             boolean     NOT NULL DEFAULT false,

    url                text        NOT NULL,
    http_status        smallint,
    etag               text,
    last_modified      text,
    sha256             text,
    bytes              bigint,

    asns               integer,
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

    CONSTRAINT chk_cgibr_run_status CHECK (status BETWEEN 0 AND 1),
    CONSTRAINT chk_cgibr_run_sha256 CHECK (sha256 ~ '^[0-9a-f]{64}$')
);

COMMENT ON TABLE cgibr_run IS
    'Histórico das execuções do collector-cgibr que baixaram um arquivo novo da
     fonte (aplicado ou recusado). Verificações sem mudança não geram linha: ficam
     só em jobs.last_check_at. A última linha com status = 1 descreve o dataset
     que está nas tabelas cgibr_asn/cgibr_prefix, e seu uuid é a versão do
     dataset usada pela api-cgibr no cache e no ETag.';
COMMENT ON COLUMN cgibr_run.uuid            IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree. Nas linhas com status = 1 é também a versão do dataset.';
COMMENT ON COLUMN cgibr_run.status          IS 'Resultado (0=falhou | 1=aplicado). Falha: download, conferência do SHA-256, parser ou trava de sanidade recusaram o arquivo, e as tabelas ficaram como estavam.';
COMMENT ON COLUMN cgibr_run.forced          IS 'true quando a execução foi forçada (--force): ignora "arquivo igual ao último" e a trava de remoção em massa.';
COMMENT ON COLUMN cgibr_run.url             IS 'URL de onde o arquivo foi baixado.';
COMMENT ON COLUMN cgibr_run.http_status     IS 'Status HTTP da resposta do arquivo. NULL = falhou antes de receber resposta.';
COMMENT ON COLUMN cgibr_run.etag            IS 'Cabeçalho ETag da resposta; reenviado em If-None-Match na próxima verificação.';
COMMENT ON COLUMN cgibr_run.last_modified   IS 'Cabeçalho Last-Modified da resposta, como texto HTTP; reenviado em If-Modified-Since.';
COMMENT ON COLUMN cgibr_run.sha256          IS 'SHA-256 (hex minúsculo) do arquivo baixado. Comparado com o .sha256 publicado ao lado do arquivo para decidir se houve mudança. NULL = falhou antes do download.';
COMMENT ON COLUMN cgibr_run.bytes           IS 'Tamanho do arquivo baixado, em bytes.';
COMMENT ON COLUMN cgibr_run.asns            IS 'ASNs lidos do arquivo.';
COMMENT ON COLUMN cgibr_run.prefixes_v4     IS 'Blocos IPv4 lidos do arquivo.';
COMMENT ON COLUMN cgibr_run.prefixes_v6     IS 'Blocos IPv6 lidos do arquivo.';
COMMENT ON COLUMN cgibr_run.asn_inserted    IS 'ASNs novos gravados em cgibr_asn. NULL quando status = 0.';
COMMENT ON COLUMN cgibr_run.asn_updated     IS 'ASNs com nome ou documento alterado. NULL quando status = 0.';
COMMENT ON COLUMN cgibr_run.asn_deleted     IS 'ASNs que sumiram da fonte e foram apagados. NULL quando status = 0.';
COMMENT ON COLUMN cgibr_run.prefix_inserted IS 'Blocos novos gravados em cgibr_prefix. NULL quando status = 0.';
COMMENT ON COLUMN cgibr_run.prefix_updated  IS 'Blocos que mudaram de ASN. NULL quando status = 0.';
COMMENT ON COLUMN cgibr_run.prefix_deleted  IS 'Blocos que sumiram da fonte e foram apagados. NULL quando status = 0.';
COMMENT ON COLUMN cgibr_run.warnings        IS 'Lista JSON (array de strings) com os avisos do parser: linhas descartadas, blocos inválidos, duplicados. Limitada às primeiras ocorrências.';
COMMENT ON COLUMN cgibr_run.error           IS 'Mensagem de erro quando status = 0. NULL quando aplicado.';
COMMENT ON COLUMN cgibr_run.started_at      IS 'Início da execução (antes do download).';
COMMENT ON COLUMN cgibr_run.created_at      IS 'Timestamp de criação do registro, gravado no fim da execução.';

-- -----------------------------------------------------------------------------
-- cgibr_run: versão atual do dataset (última execução aplicada)
-- -----------------------------------------------------------------------------
CREATE INDEX ix_cgibr_run_applied ON cgibr_run(created_at DESC) WHERE status = 1;

-- -----------------------------------------------------------------------------
-- Tabela: cgibr_asn
-- -----------------------------------------------------------------------------
CREATE TABLE cgibr_asn (
    uuid               uuid        PRIMARY KEY DEFAULT uuidv7(),
    asn                bigint      NOT NULL,
    name               text        NOT NULL,
    document           text        NOT NULL,
    document_digits    text        NOT NULL GENERATED ALWAYS AS (regexp_replace(document, '[^0-9]', '', 'g')) STORED,

    created_at         timestamptz NOT NULL DEFAULT NOW(),
    updated_at         timestamptz NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_cgibr_asn_asn   UNIQUE (asn),
    CONSTRAINT chk_cgibr_asn_asn  CHECK (asn BETWEEN 0 AND 4294967295)
);

COMMENT ON TABLE cgibr_asn IS
    'Uma linha por ASN do arquivo do NIC.br. O nome e o documento são do titular
     brasileiro dos blocos, não necessariamente do ASN: algumas linhas usam ASN
     registrado fora do Brasil (ex.: AS174, AS8075) e nesse caso o vínculo vale
     para os blocos. ASN que some da fonte é apagado, com seus blocos (cascata).';
COMMENT ON COLUMN cgibr_asn.uuid            IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree.';
COMMENT ON COLUMN cgibr_asn.asn             IS 'Número do sistema autônomo (0 a 4294967295), sem o prefixo AS. Chave natural.';
COMMENT ON COLUMN cgibr_asn.name            IS 'Nome do titular como publicado pelo NIC.br (segundo campo da linha).';
COMMENT ON COLUMN cgibr_asn.document        IS 'Documento do titular como publicado (terceiro campo): CNPJ formatado 00.000.000/0000-00 ou identificador estrangeiro de 8 dígitos.';
COMMENT ON COLUMN cgibr_asn.document_digits IS 'Só os dígitos de document, calculado pelo banco. 14 dígitos = CNPJ; 8 dígitos = identificador estrangeiro. Usado na busca por documento.';
COMMENT ON COLUMN cgibr_asn.created_at      IS 'Timestamp de criação do registro (primeira vez que o ASN apareceu na fonte).';
COMMENT ON COLUMN cgibr_asn.updated_at      IS 'Timestamp de última alteração, determina a versão do registro; mantido pelo trigger trg_cgibr_asn_updated_at.';

CREATE TRIGGER trg_cgibr_asn_updated_at
    BEFORE UPDATE ON cgibr_asn
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

-- -----------------------------------------------------------------------------
-- cgibr_asn: busca por CNPJ ou identificador estrangeiro (api-cgibr /document)
-- -----------------------------------------------------------------------------
CREATE INDEX ix_cgibr_asn_document_digits ON cgibr_asn(document_digits);

-- -----------------------------------------------------------------------------
-- Tabela: cgibr_prefix
-- -----------------------------------------------------------------------------
CREATE TABLE cgibr_prefix (
    uuid               uuid        PRIMARY KEY DEFAULT uuidv7(),
    prefix             cidr        NOT NULL,
    family             smallint    NOT NULL GENERATED ALWAYS AS (family(prefix)) STORED,
    asn_uuid           uuid        NOT NULL,

    created_at         timestamptz NOT NULL DEFAULT NOW(),
    updated_at         timestamptz NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_cgibr_prefix_prefix  UNIQUE (prefix),

    CONSTRAINT fk_cgibr_prefix_asn     FOREIGN KEY (asn_uuid)
        REFERENCES cgibr_asn(uuid) ON DELETE CASCADE
);

COMMENT ON TABLE cgibr_prefix IS
    'Blocos IPv4 e IPv6 atribuídos a titulares brasileiros, cada um ligado ao ASN
     da linha em que aparece no arquivo do NIC.br. Um bloco pertence a um único
     ASN; se a fonte repetir um bloco em outra linha, vale a primeira ocorrência.';
COMMENT ON COLUMN cgibr_prefix.uuid       IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree.';
COMMENT ON COLUMN cgibr_prefix.prefix     IS 'Bloco em notação CIDR canônica (bits de host zerados pelo collector). Chave natural.';
COMMENT ON COLUMN cgibr_prefix.family     IS 'Família do endereço, calculada pelo banco (4=IPv4 | 6=IPv6).';
COMMENT ON COLUMN cgibr_prefix.asn_uuid   IS 'ASN da linha em que o bloco aparece (FK → cgibr_asn.uuid, removido em cascata).';
COMMENT ON COLUMN cgibr_prefix.created_at IS 'Timestamp de criação do registro (primeira vez que o bloco apareceu na fonte).';
COMMENT ON COLUMN cgibr_prefix.updated_at IS 'Timestamp de última alteração (troca de ASN), determina a versão do registro; mantido pelo trigger trg_cgibr_prefix_updated_at.';

CREATE TRIGGER trg_cgibr_prefix_updated_at
    BEFORE UPDATE ON cgibr_prefix
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

-- -----------------------------------------------------------------------------
-- cgibr_prefix: bloco que contém um IP ou prefixo (api-cgibr /ip e /prefix),
-- com o operador >>= do inet.
-- -----------------------------------------------------------------------------
CREATE INDEX ix_cgibr_prefix_prefix_gist ON cgibr_prefix USING gist (prefix inet_ops);

-- -----------------------------------------------------------------------------
-- cgibr_prefix: blocos de um ASN (api-cgibr /asn) e cascata do DELETE
-- -----------------------------------------------------------------------------
CREATE INDEX ix_cgibr_prefix_asn ON cgibr_prefix(asn_uuid);

-- migrate:down

DROP TABLE cgibr_prefix;
DROP TABLE cgibr_asn;
DROP TABLE cgibr_run;
