-- migrate:up

-- =============================================================================
-- rootanchors: âncoras de confiança DNSSEC da zona raiz (KSKs), publicadas pela
-- IANA no arquivo root-anchors.xml
-- Depende de: central (função set_updated_at)
-- Tabelas: rootanchors_run (tabela independente), rootanchors_key (tabela
-- independente)
--
-- Escrita pelo collector-rootanchors, lida pela api-rootanchors. Fonte:
--   https://data.iana.org/root-anchors/root-anchors.xml
-- Um elemento <TrustAnchor> (id, source, <Zone>.</Zone>) com um <KeyDigest>
-- por KSK: id, validFrom, validUntil (opcional), KeyTag, Algorithm,
-- DigestType, Digest e, nas chaves mais novas, PublicKey e Flags. A IANA
-- mantém no arquivo as chaves que já expiraram (com validUntil), então a lista
-- só cresce; as regras de validação estão em specs/fontes/rootanchors/fonte.md.
--
-- Nota: as tabelas espelham o último arquivo aplicado. Uma chave que some da
-- fonte é apagada aqui (sem soft delete); histórico e consolidação ficam para
-- as tabelas centrais da fase 2. A aplicação acontece numa transação só, então
-- a api-rootanchors nunca vê um arquivo pela metade.
-- =============================================================================

-- -----------------------------------------------------------------------------
-- Tabela: rootanchors_run
-- -----------------------------------------------------------------------------
CREATE TABLE rootanchors_run (
    uuid               uuid        PRIMARY KEY DEFAULT uuidv7(),
    status             smallint    NOT NULL,
    forced             boolean     NOT NULL DEFAULT false,

    url                text        NOT NULL,
    http_status        smallint,
    etag               text,
    last_modified      text,
    sha256             text,
    bytes              bigint,

    anchor_id          text,
    anchor_source      text,
    zone               text,
    keys               integer,
    key_inserted       integer,
    key_updated        integer,
    key_deleted        integer,

    warnings           jsonb       NOT NULL DEFAULT '[]'::jsonb,
    error              text,

    started_at         timestamptz NOT NULL,
    created_at         timestamptz NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_rootanchors_run_status CHECK (status BETWEEN 0 AND 1),
    CONSTRAINT chk_rootanchors_run_sha256 CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    CONSTRAINT chk_rootanchors_run_zone   CHECK (zone = '.')
);

COMMENT ON TABLE rootanchors_run IS
    'Histórico das execuções do collector-rootanchors que baixaram um arquivo
     root-anchors.xml novo (aplicado ou recusado). Verificações sem mudança não
     geram linha: ficam só em jobs.last_check_at. A última linha com status = 1
     descreve o que está em rootanchors_key, e seu uuid é a versão do dataset
     usada pela api-rootanchors no cache e no ETag.';
COMMENT ON COLUMN rootanchors_run.uuid          IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree. Nas linhas com status = 1 é também a versão do dataset.';
COMMENT ON COLUMN rootanchors_run.status        IS 'Resultado (0=recusado | 1=aplicado). Recusado: conferência do hash, parser, validação das chaves, mínimo ou trava de remoção barraram o arquivo, e as tabelas ficaram como estavam.';
COMMENT ON COLUMN rootanchors_run.forced        IS 'true quando a execução foi forçada (--force): ignora "arquivo igual ao último" e a trava de remoção em massa.';
COMMENT ON COLUMN rootanchors_run.url           IS 'URL de onde o arquivo foi baixado.';
COMMENT ON COLUMN rootanchors_run.http_status   IS 'Status HTTP da resposta do arquivo. NULL = falhou antes de receber resposta.';
COMMENT ON COLUMN rootanchors_run.etag          IS 'Cabeçalho ETag da resposta (fraco, W/"...", no Cloudflare da IANA); reenviado em If-None-Match na próxima verificação.';
COMMENT ON COLUMN rootanchors_run.last_modified IS 'Cabeçalho Last-Modified da resposta, como texto HTTP; reenviado em If-Modified-Since.';
COMMENT ON COLUMN rootanchors_run.sha256        IS 'SHA-256 (hex minúsculo) do arquivo baixado, já descomprimido; comparado com a linha de root-anchors.xml do checksums-sha256.txt publicado pela IANA para decidir se houve mudança. NULL = falhou antes do download.';
COMMENT ON COLUMN rootanchors_run.bytes         IS 'Tamanho do arquivo baixado (descomprimido), em bytes.';
COMMENT ON COLUMN rootanchors_run.anchor_id     IS 'Atributo id do elemento TrustAnchor (um UUID em maiúsculas, ex.: 0C05FDD6-422C-4910-8ED6-430ED15E11C2). NULL = o parser não chegou a rodar ou recusou o arquivo.';
COMMENT ON COLUMN rootanchors_run.anchor_source IS 'Atributo source do elemento TrustAnchor (a URL que a IANA declara como origem, hoje em http://). NULL = o parser não chegou a rodar, recusou o arquivo ou o atributo veio vazio.';
COMMENT ON COLUMN rootanchors_run.zone          IS 'Conteúdo do elemento Zone; o coletor só aceita "." (a raiz). NULL = o parser não chegou a rodar ou recusou o arquivo.';
COMMENT ON COLUMN rootanchors_run.keys          IS 'Elementos KeyDigest lidos do arquivo (inclusive os de chaves já expiradas). NULL = o parser não chegou a rodar ou recusou o arquivo.';
COMMENT ON COLUMN rootanchors_run.key_inserted  IS 'Chaves novas gravadas em rootanchors_key. NULL quando status = 0.';
COMMENT ON COLUMN rootanchors_run.key_updated   IS 'Chaves com algum dado alterado (key tag, algoritmo, digest, chave pública, flags ou validade). NULL quando status = 0.';
COMMENT ON COLUMN rootanchors_run.key_deleted   IS 'Chaves que sumiram da fonte e foram apagadas. NULL quando status = 0.';
COMMENT ON COLUMN rootanchors_run.warnings      IS 'Lista JSON (array de strings) com os avisos do parser (elementos ou atributos desconhecidos). Limitada às primeiras ocorrências.';
COMMENT ON COLUMN rootanchors_run.error         IS 'Mensagem da recusa quando status = 0. NULL quando aplicado.';
COMMENT ON COLUMN rootanchors_run.started_at    IS 'Início da execução (antes do download).';
COMMENT ON COLUMN rootanchors_run.created_at    IS 'Timestamp de criação do registro, gravado no fim da execução.';

-- -----------------------------------------------------------------------------
-- rootanchors_run: versão atual do dataset (última execução aplicada)
-- -----------------------------------------------------------------------------
CREATE INDEX ix_rootanchors_run_applied ON rootanchors_run(created_at DESC) WHERE status = 1;

-- -----------------------------------------------------------------------------
-- Tabela: rootanchors_key
-- -----------------------------------------------------------------------------
CREATE TABLE rootanchors_key (
    uuid               uuid        PRIMARY KEY DEFAULT uuidv7(),
    key_id             text        NOT NULL,
    key_tag            integer     NOT NULL,
    algorithm          smallint    NOT NULL,
    digest_type        smallint    NOT NULL,
    digest             text        NOT NULL,
    public_key         text,
    flags              integer,
    valid_from         timestamptz NOT NULL,
    valid_until        timestamptz,

    created_at         timestamptz NOT NULL DEFAULT NOW(),
    updated_at         timestamptz NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_rootanchors_key_key_id       UNIQUE (key_id),
    CONSTRAINT chk_rootanchors_key_key_id      CHECK (key_id <> ''),
    CONSTRAINT chk_rootanchors_key_key_tag     CHECK (key_tag     BETWEEN 0 AND 65535),
    CONSTRAINT chk_rootanchors_key_algorithm   CHECK (algorithm   BETWEEN 0 AND 255),
    CONSTRAINT chk_rootanchors_key_digest_type CHECK (digest_type BETWEEN 0 AND 255),
    CONSTRAINT chk_rootanchors_key_digest      CHECK (digest      ~ '^[0-9A-F]+$'),
    CONSTRAINT chk_rootanchors_key_public_key  CHECK (public_key  ~ '^[A-Za-z0-9+/]+={0,2}$'),
    CONSTRAINT chk_rootanchors_key_flags       CHECK (flags       BETWEEN 0 AND 65535),
    CONSTRAINT chk_rootanchors_key_dnskey      CHECK ((public_key IS NULL) = (flags IS NULL)),
    CONSTRAINT chk_rootanchors_key_validity    CHECK (valid_until >= valid_from)
);

COMMENT ON TABLE rootanchors_key IS
    'Uma linha por elemento KeyDigest do root-anchors.xml da IANA: as KSKs
     (chaves de assinatura de chave) da zona raiz do DNS que servem de âncora
     de confiança DNSSEC, cada uma com o registro DS (key tag, algoritmo, tipo
     de digest, digest) e, nas mais novas, o DNSKEY (flags e chave pública).
     Inclui as chaves já expiradas, que a IANA mantém no arquivo com
     validUntil. A chave ativa num instante t é a que tem valid_from <= t e
     (valid_until IS NULL ou valid_until > t). Chave que some da fonte é
     apagada.';
COMMENT ON COLUMN rootanchors_key.uuid        IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree.';
COMMENT ON COLUMN rootanchors_key.key_id      IS 'Atributo id do KeyDigest (ex.: Kmyv6jo), único no arquivo. Chave natural.';
COMMENT ON COLUMN rootanchors_key.key_tag     IS 'Key tag do DNSKEY (0 a 65535, RFC 4034 apêndice B), ex.: 20326, 38696. Não é único: duas chaves podem ter o mesmo key tag. Quando há public_key, o coletor recalcula o key tag e recusa o arquivo se não bater.';
COMMENT ON COLUMN rootanchors_key.algorithm   IS 'Número do algoritmo DNSSEC (registro da IANA): 8=RSA/SHA-256 | 13=ECDSA P-256/SHA-256 | 14=ECDSA P-384/SHA-384 | 15=Ed25519 | 16=Ed448.';
COMMENT ON COLUMN rootanchors_key.digest_type IS 'Tipo do digest do DS (registro da IANA): 1=SHA-1 (40 hex) | 2=SHA-256 (64 hex) | 4=SHA-384 (96 hex). O coletor só aceita estes três.';
COMMENT ON COLUMN rootanchors_key.digest      IS 'Digest do registro DS em hexadecimal maiúsculo, com o tamanho do digest_type. Quando há public_key, o coletor recalcula o digest sobre o nome da raiz em wire format (0x00) e o RDATA do DNSKEY e recusa o arquivo se não bater.';
COMMENT ON COLUMN rootanchors_key.public_key  IS 'Chave pública do DNSKEY em base64, como publicada (sem espaços nem quebras de linha). NULL = o KeyDigest não traz PublicKey (as chaves antigas, ex.: 19036, só têm o DS).';
COMMENT ON COLUMN rootanchors_key.flags       IS 'Campo flags do DNSKEY (257 = zone key + SEP, uma KSK). NULL junto com public_key: as duas colunas vêm juntas ou nenhuma vem.';
COMMENT ON COLUMN rootanchors_key.valid_from  IS 'Atributo validFrom: início da validade da âncora.';
COMMENT ON COLUMN rootanchors_key.valid_until IS 'Atributo validUntil: fim da validade (a chave foi aposentada). NULL = sem data de fim (chave em uso ou prevista para uso).';
COMMENT ON COLUMN rootanchors_key.created_at  IS 'Timestamp de criação do registro (primeira vez que a chave apareceu na fonte).';
COMMENT ON COLUMN rootanchors_key.updated_at  IS 'Timestamp de última alteração, determina a versão do registro; mantido pelo trigger trg_rootanchors_key_updated_at.';

CREATE TRIGGER trg_rootanchors_key_updated_at
    BEFORE UPDATE ON rootanchors_key
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

-- -----------------------------------------------------------------------------
-- rootanchors_key: consulta pelo id do KeyDigest usa o índice único implícito
-- de uq_rootanchors_key_key_id; não crie outro índice em key_id.
-- -----------------------------------------------------------------------------

-- -----------------------------------------------------------------------------
-- rootanchors_key: chaves de um key tag (api-rootanchors /key/<tag>)
-- (WHERE key_tag = $1 ORDER BY valid_from, key_id). A tabela tem poucas
-- linhas; o índice existe para o plano não depender disso.
-- -----------------------------------------------------------------------------
CREATE INDEX ix_rootanchors_key_key_tag ON rootanchors_key(key_tag);

-- migrate:down

DROP TABLE rootanchors_key;
DROP TABLE rootanchors_run;
