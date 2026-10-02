-- migrate:up

-- =============================================================================
-- rootzone: a zona raiz do DNS (root.zone), publicada pela InterNIC
-- Depende de: central (função set_updated_at)
-- Tabelas: rootzone_run (tabela independente), rootzone_tld (tabela
--          independente), rootzone_record (tabela independente)
--
-- Escrita pelo collector-rootzone, lida pela api-rootzone. Fonte:
--   https://www.internic.net/domain/root.zone (MD5 em root.zone.md5)
-- Arquivo mestre (RFC 1035) com uma RR por linha:
--   "<dono> <ttl> IN <tipo> <rdata>"
-- Tipos: SOA, NS, A, AAAA, DS, DNSKEY, NSEC, RRSIG e ZONEMD. Os RRSIG NÃO são
-- guardados (só contados em rootzone_run): são reassinados a cada publicação
-- (~2 por dia) e, com o rdata na chave natural, reescreveriam ~2,8 mil linhas
-- a cada versão e disparariam a trava de remoção do coletor.
--
-- Normalização (specs/fontes/rootzone/fonte.md): nomes em minúsculas e sem o
-- ponto final, com a raiz como "."; endereços IPv6 na forma canônica
-- (RFC 5952); hex de DS e ZONEMD em maiúsculas; um espaço entre os campos
-- do rdata.
--
-- Nota: as tabelas espelham a última versão aplicada da zona. Uma RR ou um
-- TLD que some da fonte é apagado aqui (sem soft delete); histórico e
-- consolidação ficam para as tabelas centrais da fase 2. A aplicação acontece
-- numa transação só, então a api-rootzone nunca vê uma zona pela metade.
-- =============================================================================

-- -----------------------------------------------------------------------------
-- Tabela: rootzone_run
-- -----------------------------------------------------------------------------
CREATE TABLE rootzone_run (
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

    serial             bigint,
    soa_mname          text,
    soa_rname          text,
    soa_refresh        bigint,
    soa_retry          bigint,
    soa_expire         bigint,
    soa_minimum        bigint,

    tlds               integer,
    records            integer,
    rrsigs             integer,
    type_counts        jsonb,

    tld_inserted       integer,
    tld_updated        integer,
    tld_deleted        integer,
    record_inserted    integer,
    record_updated     integer,
    record_deleted     integer,

    warnings           jsonb       NOT NULL DEFAULT '[]'::jsonb,
    error              text,

    started_at         timestamptz NOT NULL,
    created_at         timestamptz NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_rootzone_run_status CHECK (status BETWEEN 0 AND 1),
    CONSTRAINT chk_rootzone_run_md5    CHECK (md5    ~ '^[0-9a-f]{32}$'),
    CONSTRAINT chk_rootzone_run_sha256 CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    CONSTRAINT chk_rootzone_run_serial CHECK (serial BETWEEN 0 AND 4294967295)
);

COMMENT ON TABLE rootzone_run IS
    'Histórico das execuções do collector-rootzone que baixaram um arquivo novo
     da fonte (aplicado ou recusado). Verificações sem mudança (MD5 publicado
     igual, HTTP 304, conteúdo igual ou serial mais antigo que o aplicado) não
     geram linha: ficam só em jobs.last_check_at. A última linha com status = 1
     descreve a zona que está em rootzone_tld e rootzone_record, traz o SOA
     dela (serial, mname, rname e temporizadores) e seu uuid é a versão do
     dataset usada pela api-rootzone no cache e no ETag.';
COMMENT ON COLUMN rootzone_run.uuid            IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree. Nas linhas com status = 1 é também a versão do dataset.';
COMMENT ON COLUMN rootzone_run.status          IS 'Resultado (0=recusado | 1=aplicado). Recusado: conferência do MD5, parser, mínimo de TLDs, trava de remoção ou erro do banco recusaram o arquivo, e as tabelas ficaram como estavam.';
COMMENT ON COLUMN rootzone_run.forced          IS 'true quando a execução foi forçada (--force): ignora as checagens de mudança (inclusive o serial mais antigo) e a trava de remoção em massa.';
COMMENT ON COLUMN rootzone_run.url             IS 'URL de onde o arquivo foi baixado.';
COMMENT ON COLUMN rootzone_run.http_status     IS 'Status HTTP da resposta do arquivo.';
COMMENT ON COLUMN rootzone_run.etag            IS 'Cabeçalho ETag da resposta, como veio (com gzip o Apache da InterNIC acrescenta "-gzip"); reenviado em If-None-Match na próxima verificação, junto com a forma sem o sufixo.';
COMMENT ON COLUMN rootzone_run.last_modified   IS 'Cabeçalho Last-Modified da resposta, como texto HTTP; reenviado em If-Modified-Since.';
COMMENT ON COLUMN rootzone_run.md5             IS 'MD5 (hex minúsculo) do arquivo baixado, descomprimido; conferido com o root.zone.md5 publicado e comparado com o publicado na verificação seguinte (checagem mais barata).';
COMMENT ON COLUMN rootzone_run.sha256          IS 'SHA-256 (hex minúsculo) do arquivo baixado, descomprimido; comparado com o do último arquivo aplicado para decidir se houve mudança.';
COMMENT ON COLUMN rootzone_run.bytes           IS 'Tamanho do arquivo baixado (descomprimido), em bytes.';
COMMENT ON COLUMN rootzone_run.serial          IS 'Serial do SOA da zona (formato AAAAMMDDNN, inteiro sem sinal de 32 bits). Um arquivo com serial menor que o aplicado (aritmética de seriais da RFC 1982) não é aplicado. NULL = o parser não rodou ou recusou o arquivo.';
COMMENT ON COLUMN rootzone_run.soa_mname       IS 'MNAME do SOA: servidor primário da zona, normalizado (minúsculas, sem o ponto final), ex.: a.root-servers.net.';
COMMENT ON COLUMN rootzone_run.soa_rname       IS 'RNAME do SOA: caixa de correio do responsável, em forma de nome DNS normalizado, ex.: nstld.verisign-grs.com (= nstld@verisign-grs.com).';
COMMENT ON COLUMN rootzone_run.soa_refresh     IS 'REFRESH do SOA, em segundos.';
COMMENT ON COLUMN rootzone_run.soa_retry       IS 'RETRY do SOA, em segundos.';
COMMENT ON COLUMN rootzone_run.soa_expire      IS 'EXPIRE do SOA, em segundos.';
COMMENT ON COLUMN rootzone_run.soa_minimum     IS 'MINIMUM do SOA (TTL de respostas negativas, RFC 2308), em segundos.';
COMMENT ON COLUMN rootzone_run.tlds            IS 'TLDs delegados no arquivo (donos de NS que não são a raiz). NULL = o parser não rodou ou recusou o arquivo.';
COMMENT ON COLUMN rootzone_run.records         IS 'RRs aceitas e guardáveis do arquivo (todas menos RRSIG), sem repetidas. NULL = o parser não rodou ou recusou o arquivo.';
COMMENT ON COLUMN rootzone_run.rrsigs          IS 'RRSIG aceitos no arquivo: contados, mas não guardados em rootzone_record. NULL = o parser não rodou ou recusou o arquivo.';
COMMENT ON COLUMN rootzone_run.type_counts     IS 'Objeto JSON tipo → linhas aceitas pelo parser, inclusive RRSIG, ex.: {"NS": 7580, "A": 5940, "RRSIG": 2794, ...}. NULL = o parser não rodou ou recusou o arquivo.';
COMMENT ON COLUMN rootzone_run.tld_inserted    IS 'TLDs novos em rootzone_tld. NULL quando status = 0.';
COMMENT ON COLUMN rootzone_run.tld_updated     IS 'TLDs com contagens ou forma Unicode alteradas. NULL quando status = 0.';
COMMENT ON COLUMN rootzone_run.tld_deleted     IS 'TLDs que sumiram da zona e foram apagados. NULL quando status = 0.';
COMMENT ON COLUMN rootzone_run.record_inserted IS 'RRs novas em rootzone_record. NULL quando status = 0.';
COMMENT ON COLUMN rootzone_run.record_updated  IS 'RRs com o TTL alterado (dono, tipo e rdata são a chave). NULL quando status = 0.';
COMMENT ON COLUMN rootzone_run.record_deleted  IS 'RRs que sumiram da zona e foram apagadas (inclui o SOA e o ZONEMD da versão anterior, que mudam a cada publicação). NULL quando status = 0.';
COMMENT ON COLUMN rootzone_run.warnings        IS 'Lista JSON (array de strings) com os avisos do parser (linhas descartadas, RRs repetidas, serial igual com conteúdo diferente); as 50 primeiras e, se houver mais, "... e mais N avisos".';
COMMENT ON COLUMN rootzone_run.error           IS 'Mensagem da recusa quando status = 0. NULL quando aplicado.';
COMMENT ON COLUMN rootzone_run.started_at      IS 'Início da execução (antes do download).';
COMMENT ON COLUMN rootzone_run.created_at      IS 'Timestamp de criação do registro, gravado no fim da execução.';

-- -----------------------------------------------------------------------------
-- rootzone_run: versão atual do dataset (última execução aplicada), com o SOA
-- -----------------------------------------------------------------------------
CREATE INDEX ix_rootzone_run_applied ON rootzone_run(created_at DESC) WHERE status = 1;

-- -----------------------------------------------------------------------------
-- Tabela: rootzone_tld
-- -----------------------------------------------------------------------------
CREATE TABLE rootzone_tld (
    uuid               uuid        PRIMARY KEY DEFAULT uuidv7(),
    tld                text        NOT NULL,
    tld_unicode        text        NOT NULL,
    nameservers        smallint    NOT NULL,
    nameservers_ipv4   smallint    NOT NULL,
    nameservers_ipv6   smallint    NOT NULL,
    ds_records         smallint    NOT NULL,

    created_at         timestamptz NOT NULL DEFAULT NOW(),
    updated_at         timestamptz NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_rootzone_tld_tld         UNIQUE (tld),
    CONSTRAINT chk_rootzone_tld_tld        CHECK (tld ~ '^[a-z0-9_-]{1,63}$'),
    CONSTRAINT chk_rootzone_tld_unicode    CHECK (tld_unicode <> ''),
    CONSTRAINT chk_rootzone_tld_ns         CHECK (nameservers > 0),
    CONSTRAINT chk_rootzone_tld_ns_ipv4    CHECK (nameservers_ipv4 BETWEEN 0 AND nameservers),
    CONSTRAINT chk_rootzone_tld_ns_ipv6    CHECK (nameservers_ipv6 BETWEEN 0 AND nameservers),
    CONSTRAINT chk_rootzone_tld_ds_records CHECK (ds_records >= 0)
);

COMMENT ON TABLE rootzone_tld IS
    'Um TLD delegado na zona raiz por linha (dono de NS que não é a raiz), com
     as contagens da delegação para a listagem da api-rootzone. Os registros
     da delegação (NS, DS, NSEC) e o glue dos servidores ficam em
     rootzone_record. Derivada do mesmo arquivo pelo collector-rootzone; TLD
     que some da zona é apagado.';
COMMENT ON COLUMN rootzone_tld.uuid             IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree.';
COMMENT ON COLUMN rootzone_tld.tld              IS 'Nome do TLD em ASCII, minúsculas, sem pontos (ex.: br, com, xn--p1ai). Chave natural; é o owner das RRs da delegação em rootzone_record.';
COMMENT ON COLUMN rootzone_tld.tld_unicode      IS 'Forma Unicode do TLD: o rótulo xn-- decodificado de punycode (RFC 3492), ex.: xn--p1ai → рф; nos TLDs ASCII é igual a tld.';
COMMENT ON COLUMN rootzone_tld.nameservers      IS 'Quantidade de NS da delegação (servidores de nome do TLD).';
COMMENT ON COLUMN rootzone_tld.nameservers_ipv4 IS 'Quantos desses servidores têm glue A (IPv4) na zona raiz.';
COMMENT ON COLUMN rootzone_tld.nameservers_ipv6 IS 'Quantos desses servidores têm glue AAAA (IPv6) na zona raiz.';
COMMENT ON COLUMN rootzone_tld.ds_records       IS 'Quantidade de registros DS da delegação (0 = TLD sem DNSSEC, sem cadeia de confiança a partir da raiz).';
COMMENT ON COLUMN rootzone_tld.created_at       IS 'Timestamp de criação do registro (primeira vez que este banco viu o TLD na zona).';
COMMENT ON COLUMN rootzone_tld.updated_at       IS 'Timestamp de última alteração, determina a versão do registro; mantido pelo trigger trg_rootzone_tld_updated_at.';

CREATE TRIGGER trg_rootzone_tld_updated_at
    BEFORE UPDATE ON rootzone_tld
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

-- -----------------------------------------------------------------------------
-- rootzone_tld: listagem e consulta de um TLD usam o índice único implícito
-- de uq_rootzone_tld_tld (WHERE tld = $1, ORDER BY tld); não crie outro.
-- -----------------------------------------------------------------------------

-- -----------------------------------------------------------------------------
-- Tabela: rootzone_record
-- -----------------------------------------------------------------------------
CREATE TABLE rootzone_record (
    uuid               uuid        PRIMARY KEY DEFAULT uuidv7(),
    owner              text        NOT NULL,
    type               text        NOT NULL,
    rdata              text        NOT NULL,
    ttl                integer     NOT NULL,

    created_at         timestamptz NOT NULL DEFAULT NOW(),
    updated_at         timestamptz NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_rootzone_record_owner_type_rdata UNIQUE (owner, type, rdata),
    CONSTRAINT chk_rootzone_record_owner           CHECK (owner <> ''),
    CONSTRAINT chk_rootzone_record_type            CHECK (type IN ('SOA', 'NS', 'A', 'AAAA', 'DS', 'DNSKEY', 'NSEC', 'ZONEMD')),
    CONSTRAINT chk_rootzone_record_rdata           CHECK (rdata <> ''),
    CONSTRAINT chk_rootzone_record_ttl             CHECK (ttl >= 0)
);

COMMENT ON TABLE rootzone_record IS
    'Uma RR da zona raiz por linha, já normalizada: todas as do arquivo menos
     os RRSIG (reassinados a cada publicação; só contados em rootzone_run).
     Inclui o ápice (SOA, NS da raiz, DNSKEY, ZONEMD, NSEC da raiz), as
     delegações dos TLDs (NS, DS, NSEC) e o glue A/AAAA dos servidores de
     nome. Chave natural (owner, type, rdata); RR que some da zona é apagada.';
COMMENT ON COLUMN rootzone_record.uuid       IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree.';
COMMENT ON COLUMN rootzone_record.owner      IS 'Dono da RR em minúsculas e sem o ponto final; a raiz é "." (ex.: ".", "br", "a.dns.br"). Nas RRs de um TLD é igual a rootzone_tld.tld.';
COMMENT ON COLUMN rootzone_record.type       IS 'Tipo da RR em maiúsculas: SOA | NS | A | AAAA | DS | DNSKEY | NSEC | ZONEMD (RRSIG não é guardado). A classe é sempre IN.';
COMMENT ON COLUMN rootzone_record.rdata      IS 'Dados da RR normalizados: NS = nome do servidor (sem o ponto final, igual ao owner do glue dele); A/AAAA = endereço canônico (IPv6 pela RFC 5952); DS = "keytag algoritmo tipo DIGEST" com o digest em hex maiúsculo; DNSKEY = "flags protocolo algoritmo chave-base64"; NSEC = "próximo-nome TIPOS..."; SOA = "mname rname serial refresh retry expire minimum"; ZONEMD = "serial esquema algoritmo DIGEST".';
COMMENT ON COLUMN rootzone_record.ttl        IS 'TTL da RR, em segundos (0 a 2147483647).';
COMMENT ON COLUMN rootzone_record.created_at IS 'Timestamp de criação do registro (primeira vez que este banco viu a RR na zona).';
COMMENT ON COLUMN rootzone_record.updated_at IS 'Timestamp de última alteração (só o TTL muda sem mudar a chave), determina a versão do registro; mantido pelo trigger trg_rootzone_record_updated_at.';

CREATE TRIGGER trg_rootzone_record_updated_at
    BEFORE UPDATE ON rootzone_record
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

-- -----------------------------------------------------------------------------
-- rootzone_record: RRs de um dono (a delegação de um TLD, o glue dos seus
-- servidores, o ápice) usam o índice único implícito de
-- uq_rootzone_record_owner_type_rdata (WHERE owner = $1 [AND type = $2], ou
-- owner = ANY($1) AND type IN ('A', 'AAAA')); não crie outro em owner.
-- -----------------------------------------------------------------------------

-- -----------------------------------------------------------------------------
-- rootzone_record: busca reversa pelo rdata — os TLDs servidos por um
-- servidor de nome (type = 'NS' AND rdata = $1) e o dono de um endereço de
-- glue (type IN ('A', 'AAAA') AND rdata = $1)
-- -----------------------------------------------------------------------------
CREATE INDEX ix_rootzone_record_type_rdata ON rootzone_record(type, rdata);

-- migrate:down

DROP TABLE rootzone_record;
DROP TABLE rootzone_tld;
DROP TABLE rootzone_run;
