-- migrate:up

-- =============================================================================
-- asnames: nome e país de todos os ASNs alocados, publicados pelo RIPE NCC
-- Depende de: central (função set_updated_at); extensão pg_trgm (contrib)
-- Tabelas: asnames_run (tabela independente), asnames_asn (tabela independente)
--
-- Escrita pelo collector-asnames, lida pela api-asnames. Fonte:
--   https://ftp.ripe.net/ripe/asnames/asn.txt
-- Uma linha por ASN, de todos os RIRs: "<asn> <descrição>", com a descrição
-- terminando em ", <CC>". O RIPE monta a descrição de jeitos diferentes
-- conforme o RIR de origem, então handle e name são derivados por regras
-- documentadas em specs/fontes/asnames/fonte.md; o texto original fica
-- intacto em description.
--
-- Nota: as tabelas espelham o último arquivo aplicado. Um ASN que some da
-- fonte é apagado aqui (sem soft delete); histórico e consolidação ficam para
-- as tabelas centrais da fase 2. A aplicação acontece numa transação só, então
-- a api-asnames nunca vê um arquivo pela metade.
--
-- Nota: pg_trgm é criada com IF NOT EXISTS e o migrate:down NÃO a remove: é
-- uma extensão do banco inteiro, que outras pastas podem usar (um DROP
-- EXTENSION quebraria os índices delas ou falharia no rollback).
-- =============================================================================

CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- -----------------------------------------------------------------------------
-- Tabela: asnames_run
-- -----------------------------------------------------------------------------
CREATE TABLE asnames_run (
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
    asn_inserted       integer,
    asn_updated        integer,
    asn_deleted        integer,

    warnings           jsonb       NOT NULL DEFAULT '[]'::jsonb,
    error              text,

    started_at         timestamptz NOT NULL,
    created_at         timestamptz NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_asnames_run_status CHECK (status BETWEEN 0 AND 1),
    CONSTRAINT chk_asnames_run_sha256 CHECK (sha256 ~ '^[0-9a-f]{64}$')
);

COMMENT ON TABLE asnames_run IS
    'Histórico das execuções do collector-asnames que baixaram um arquivo novo da
     fonte (aplicado ou recusado). Verificações sem mudança não geram linha: ficam
     só em jobs.last_check_at. A última linha com status = 1 descreve o dataset
     que está em asnames_asn, e seu uuid é a versão do dataset usada pela
     api-asnames no cache e no ETag.';
COMMENT ON COLUMN asnames_run.uuid          IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree. Nas linhas com status = 1 é também a versão do dataset.';
COMMENT ON COLUMN asnames_run.status        IS 'Resultado (0=falhou | 1=aplicado). Falha: download, parser ou trava de sanidade recusaram o arquivo, e as tabelas ficaram como estavam.';
COMMENT ON COLUMN asnames_run.forced        IS 'true quando a execução foi forçada (--force): ignora "arquivo igual ao último" e a trava de remoção em massa.';
COMMENT ON COLUMN asnames_run.url           IS 'URL de onde o arquivo foi baixado.';
COMMENT ON COLUMN asnames_run.http_status   IS 'Status HTTP da resposta do arquivo. NULL = falhou antes de receber resposta.';
COMMENT ON COLUMN asnames_run.etag          IS 'Cabeçalho ETag da resposta (fraco, W/"...", quando o servidor comprime com gzip); reenviado em If-None-Match na próxima verificação.';
COMMENT ON COLUMN asnames_run.last_modified IS 'Cabeçalho Last-Modified da resposta, como texto HTTP; reenviado em If-Modified-Since.';
COMMENT ON COLUMN asnames_run.sha256        IS 'SHA-256 (hex minúsculo) do arquivo baixado, já descomprimido. A fonte não publica hash: este valor é comparado com o do último arquivo aplicado para decidir se houve mudança. NULL = falhou antes do download.';
COMMENT ON COLUMN asnames_run.bytes         IS 'Tamanho do arquivo baixado (descomprimido), em bytes.';
COMMENT ON COLUMN asnames_run.asns          IS 'ASNs lidos do arquivo (linhas aceitas pelo parser). NULL = o parser não chegou a rodar ou recusou o arquivo.';
COMMENT ON COLUMN asnames_run.asn_inserted  IS 'ASNs novos gravados em asnames_asn. NULL quando status = 0.';
COMMENT ON COLUMN asnames_run.asn_updated   IS 'ASNs com description, handle, name ou country alterado. NULL quando status = 0.';
COMMENT ON COLUMN asnames_run.asn_deleted   IS 'ASNs que sumiram da fonte e foram apagados. NULL quando status = 0.';
COMMENT ON COLUMN asnames_run.warnings      IS 'Lista JSON (array de strings) com os avisos do parser: linhas descartadas, ASNs repetidos. Limitada às primeiras ocorrências.';
COMMENT ON COLUMN asnames_run.error         IS 'Mensagem de erro quando status = 0. NULL quando aplicado.';
COMMENT ON COLUMN asnames_run.started_at    IS 'Início da execução (antes do download).';
COMMENT ON COLUMN asnames_run.created_at    IS 'Timestamp de criação do registro, gravado no fim da execução.';

-- -----------------------------------------------------------------------------
-- asnames_run: versão atual do dataset (última execução aplicada)
-- -----------------------------------------------------------------------------
CREATE INDEX ix_asnames_run_applied ON asnames_run(created_at DESC) WHERE status = 1;

-- -----------------------------------------------------------------------------
-- Tabela: asnames_asn
-- -----------------------------------------------------------------------------
CREATE TABLE asnames_asn (
    uuid               uuid        PRIMARY KEY DEFAULT uuidv7(),
    asn                bigint      NOT NULL,
    description        text        NOT NULL,
    handle             text,
    name               text,
    country            text,

    created_at         timestamptz NOT NULL DEFAULT NOW(),
    updated_at         timestamptz NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_asnames_asn_asn          UNIQUE (asn),
    CONSTRAINT chk_asnames_asn_asn         CHECK (asn BETWEEN 0 AND 4294967295),
    CONSTRAINT chk_asnames_asn_description CHECK (description <> ''),
    CONSTRAINT chk_asnames_asn_handle      CHECK (handle      <> ''),
    CONSTRAINT chk_asnames_asn_name        CHECK (name        <> ''),
    CONSTRAINT chk_asnames_asn_country     CHECK (country     ~ '^[A-Z]{2}$')
);

COMMENT ON TABLE asnames_asn IS
    'Uma linha por ASN do arquivo asn.txt do RIPE NCC, que cobre os ASNs
     alocados por todos os RIRs (AFRINIC, APNIC, ARIN, LACNIC, RIPE NCC).
     description guarda o texto original; handle, name e country são derivados
     dele pelo collector-asnames. ASN que some da fonte é apagado.';
COMMENT ON COLUMN asnames_asn.uuid        IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree.';
COMMENT ON COLUMN asnames_asn.asn         IS 'Número do sistema autônomo (0 a 4294967295), sem o prefixo AS. Chave natural.';
COMMENT ON COLUMN asnames_asn.description IS 'Texto da linha depois do número do ASN, exatamente como publicado (inclui o sufixo ", CC"; espaços extras e mojibake da fonte são preservados). Fonte da verdade: handle, name e country são derivados deste campo.';
COMMENT ON COLUMN asnames_asn.handle      IS 'Nome curto do AS (as-name), derivado de description: o trecho antes de " - " (formato ARIN/APNIC/LACNIC), a metade repetida em "X - X" (formato AFRINIC) ou a primeira palavra (formato RIPE NCC). Não é único. NULL = a linha não traz nome curto.';
COMMENT ON COLUMN asnames_asn.name        IS 'Nome da organização titular, derivado de description (o que vem depois do handle, sem o sufixo de país). NULL = a linha não traz nome além do handle.';
COMMENT ON COLUMN asnames_asn.country     IS 'Código de país de 2 letras maiúsculas do sufixo ", CC" de description (ISO 3166-1 alfa-2, mais os códigos regionais EU e AP usados pelos RIRs). NULL = a linha não termina em ", CC".';
COMMENT ON COLUMN asnames_asn.created_at  IS 'Timestamp de criação do registro (primeira vez que o ASN apareceu na fonte).';
COMMENT ON COLUMN asnames_asn.updated_at  IS 'Timestamp de última alteração, determina a versão do registro; mantido pelo trigger trg_asnames_asn_updated_at.';

CREATE TRIGGER trg_asnames_asn_updated_at
    BEFORE UPDATE ON asnames_asn
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

-- -----------------------------------------------------------------------------
-- asnames_asn: consulta por número (api-asnames /asn/<n>) usa o índice único
-- implícito de uq_asnames_asn_asn; não crie outro índice em asn.
-- -----------------------------------------------------------------------------

-- -----------------------------------------------------------------------------
-- asnames_asn: ASNs de um país, paginados por número
-- (WHERE country = $1 ORDER BY asn)
-- -----------------------------------------------------------------------------
CREATE INDEX ix_asnames_asn_country ON asnames_asn(country, asn);

-- -----------------------------------------------------------------------------
-- asnames_asn: handle exato ou por prefixo, sem diferenciar maiúsculas
-- (WHERE lower(handle) = lower($1) ou lower(handle) LIKE lower($1) || '%')
-- -----------------------------------------------------------------------------
CREATE INDEX ix_asnames_asn_handle ON asnames_asn(lower(handle) text_pattern_ops);

-- -----------------------------------------------------------------------------
-- asnames_asn: busca por trecho de texto no handle ou no nome
-- (WHERE description ILIKE '%' || $1 || '%'). Os trigramas do pg_trgm já
-- ignoram maiúsculas; termos com menos de 3 caracteres não aproveitam o índice.
-- -----------------------------------------------------------------------------
CREATE INDEX ix_asnames_asn_description_trgm ON asnames_asn USING gin (description gin_trgm_ops);

-- migrate:down

-- A extensão pg_trgm fica: é compartilhada pelo banco (ver cabeçalho).
DROP TABLE asnames_asn;
DROP TABLE asnames_run;
