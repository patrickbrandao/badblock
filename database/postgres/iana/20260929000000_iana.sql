-- migrate:up

-- =============================================================================
-- iana: registros de numeração da IANA (blocos de ASN e de IP por RIR,
-- special-purpose e servidores RDAP)
-- Depende de: central (função set_updated_at)
-- Tabelas: iana_run, iana_asn_block, iana_prefix_block, iana_special_prefix,
--          iana_special_asn, iana_rdap_service (todas independentes, sem FK)
--
-- Escrita pelo collector-iana, lida pela api-iana. Fonte: 10 arquivos pequenos
-- da IANA tratados como UM dataset:
--   https://www.iana.org/assignments/as-numbers/as-numbers-1.csv (e -2.csv)
--   https://www.iana.org/assignments/ipv4-address-space/ipv4-address-space.csv
--   https://www.iana.org/assignments/ipv6-unicast-address-assignments/ipv6-unicast-address-assignments.csv
--   https://www.iana.org/assignments/iana-ipv4-special-registry/iana-ipv4-special-registry-1.csv
--   https://www.iana.org/assignments/iana-ipv6-special-registry/iana-ipv6-special-registry-1.csv
--   https://www.iana.org/assignments/iana-as-numbers-special-registry/special-purpose-as-numbers.csv
--   https://data.iana.org/rdap/asn.json, ipv4.json, ipv6.json (RFC 9224)
--
-- Nota: as tabelas espelham o último dataset aplicado. Uma linha que some da
-- fonte é apagada aqui (sem soft delete). Os 10 arquivos são aplicados juntos,
-- numa transação só: a api-iana nunca vê uma mistura de arquivos novos e velhos.
-- Datas vêm como texto porque a IANA publica ora ano-mês (1981-09), ora
-- ano-mês-dia (2008-12-03); inventar o dia 1 mudaria o dado.
-- =============================================================================

-- -----------------------------------------------------------------------------
-- Tabela: iana_run
-- -----------------------------------------------------------------------------
CREATE TABLE iana_run (
    uuid               uuid        PRIMARY KEY DEFAULT uuidv7(),
    status             smallint    NOT NULL,
    forced             boolean     NOT NULL DEFAULT false,

    sha256             text,
    files              jsonb       NOT NULL DEFAULT '[]'::jsonb,
    changes            jsonb,

    warnings           jsonb       NOT NULL DEFAULT '[]'::jsonb,
    error              text,

    started_at         timestamptz NOT NULL,
    created_at         timestamptz NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_iana_run_status  CHECK (status BETWEEN 0 AND 1),
    CONSTRAINT chk_iana_run_sha256  CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    CONSTRAINT chk_iana_run_files   CHECK (jsonb_typeof(files) = 'array'),
    CONSTRAINT chk_iana_run_changes CHECK (changes IS NULL OR jsonb_typeof(changes) = 'object'),
    CONSTRAINT chk_iana_run_applied CHECK (status = 0 OR (sha256 IS NOT NULL AND changes IS NOT NULL))
);

COMMENT ON TABLE iana_run IS
    'Histórico das execuções do collector-iana em que ao menos um dos 10 arquivos
     da IANA mudou (dataset aplicado ou recusado). Verificações sem mudança não
     geram linha: ficam só em jobs.last_check_at. A última linha com status = 1
     descreve o dataset que está nas tabelas iana_*, e seu uuid é a versão do
     dataset usada pela api-iana no cache e no ETag.';
COMMENT ON COLUMN iana_run.uuid       IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree. Nas linhas com status = 1 é também a versão do dataset.';
COMMENT ON COLUMN iana_run.status     IS 'Resultado (0=recusado | 1=aplicado). Recusado: parser, sanidade, trava de remoção ou erro no banco recusaram o dataset, e as tabelas ficaram como estavam. Falha de download de qualquer arquivo não gera linha (só log).';
COMMENT ON COLUMN iana_run.forced     IS 'true quando a execução foi forçada (--force): ignora "nenhum arquivo mudou" e a trava de remoção em massa.';
COMMENT ON COLUMN iana_run.sha256     IS 'SHA-256 combinado do dataset (hex minúsculo): hash das linhas "<nome>:<sha256 do arquivo>\n" dos 10 arquivos, na ordem fixa de files. Igual ao da última linha aplicada = mesmo dataset.';
COMMENT ON COLUMN iana_run.files      IS
    'Array JSON com um objeto por arquivo, sempre na mesma ordem (as-numbers-1,
     as-numbers-2, ipv4-address-space, ipv6-unicast-address-assignments,
     iana-ipv4-special-registry-1, iana-ipv6-special-registry-1,
     special-purpose-as-numbers, rdap-asn, rdap-ipv4, rdap-ipv6). Chaves: name,
     url, http_status, etag e last_modified (validadores HTTP, ausentes se o
     servidor não mandou; reenviados no GET condicional da próxima verificação),
     sha256 e bytes do conteúdo, changed (true se o conteúdo difere do último
     dataset aplicado), rows (registros gerados, ausente se o parser não chegou
     a rodar) e publication (só nos rdap-*: campo publication do JSON, RFC 3339).';
COMMENT ON COLUMN iana_run.changes    IS
    'Objeto JSON com as alterações por tabela: {"iana_asn_block": {"inserted": n,
     "updated": n, "deleted": n}, ...} para as 5 tabelas de dados. NULL quando
     status = 0.';
COMMENT ON COLUMN iana_run.warnings   IS 'Lista JSON (array de strings) com os avisos do parser: linhas descartadas, campos não reconhecidos, blocos com bits de host. Limitada às primeiras ocorrências.';
COMMENT ON COLUMN iana_run.error      IS 'Mensagem de erro quando status = 0. NULL quando aplicado.';
COMMENT ON COLUMN iana_run.started_at IS 'Início da execução (antes dos downloads).';
COMMENT ON COLUMN iana_run.created_at IS 'Timestamp de criação do registro, gravado no fim da execução.';

-- -----------------------------------------------------------------------------
-- iana_run: versão atual do dataset (última execução aplicada)
-- -----------------------------------------------------------------------------
CREATE INDEX ix_iana_run_applied ON iana_run(created_at DESC) WHERE status = 1;

-- -----------------------------------------------------------------------------
-- Tabela: iana_asn_block
-- -----------------------------------------------------------------------------
CREATE TABLE iana_asn_block (
    uuid               uuid        PRIMARY KEY DEFAULT uuidv7(),
    asn_start          bigint      NOT NULL,
    asn_end            bigint      NOT NULL,
    description        text        NOT NULL,
    registry           text,
    whois              text,
    rdap_urls          text[]      NOT NULL DEFAULT '{}',
    reference          text,
    registration_date  text,
    source_file        text        NOT NULL,

    created_at         timestamptz NOT NULL DEFAULT NOW(),
    updated_at         timestamptz NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_iana_asn_block_asn_start   UNIQUE (asn_start),
    CONSTRAINT chk_iana_asn_block_asn_start  CHECK (asn_start BETWEEN 0 AND 4294967295),
    CONSTRAINT chk_iana_asn_block_asn_end    CHECK (asn_end   BETWEEN asn_start AND 4294967295),
    CONSTRAINT chk_iana_asn_block_registry   CHECK (registry IN ('afrinic', 'apnic', 'arin', 'lacnic', 'ripencc')),
    CONSTRAINT chk_iana_asn_block_date       CHECK (registration_date ~ '^[0-9]{4}-[0-9]{2}(-[0-9]{2})?$'),
    CONSTRAINT chk_iana_asn_block_source     CHECK (source_file IN ('as-numbers-1', 'as-numbers-2'))
);

COMMENT ON TABLE iana_asn_block IS
    'Blocos de ASN do registro da IANA: a quem cada faixa foi entregue (um dos 5
     RIRs) ou por que está reservada. Junta as-numbers-1.csv (16 bits, 0–65535) e
     as-numbers-2.csv (32 bits, 65536–4294967295); a linha "See Sub-registry
     16-bit AS numbers" do segundo arquivo (0–65535) é ignorada. As faixas não se
     sobrepõem e, juntas, cobrem 0–4294967295 sem buracos (conferido pelo
     collector). ASN → bloco: asn_start <= X ORDER BY asn_start DESC LIMIT 1 e
     conferir asn_end >= X.';
COMMENT ON COLUMN iana_asn_block.uuid              IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree.';
COMMENT ON COLUMN iana_asn_block.asn_start         IS 'Primeiro ASN da faixa (coluna Number: "1-1876" → 1; "2043" → 2043). Chave natural.';
COMMENT ON COLUMN iana_asn_block.asn_end           IS 'Último ASN da faixa, inclusive (igual a asn_start quando a faixa tem um ASN só).';
COMMENT ON COLUMN iana_asn_block.description       IS 'Coluna Description como publicada: "Assigned by ARIN", "Reserved", "Unallocated", "AS_TRANS", "Reserved for Private Use"...';
COMMENT ON COLUMN iana_asn_block.registry          IS 'RIR responsável (afrinic | apnic | arin | lacnic | ripencc), tirado do domínio da coluna WHOIS (whois.arin.net → arin) ou, sem WHOIS, da descrição ("Assigned by RIPE NCC" → ripencc). NULL = faixa sem RIR (reservada, não alocada, AS_TRANS...).';
COMMENT ON COLUMN iana_asn_block.whois             IS 'Servidor WHOIS da faixa (coluna WHOIS, ex.: whois.arin.net). NULL quando vazio.';
COMMENT ON COLUMN iana_asn_block.rdap_urls         IS 'URLs RDAP da faixa. A coluna RDAP da IANA às vezes cola duas URLs sem separador (https://rdap.arin.net/registryhttp://rdap.arin.net/registry); o collector as separa. Vazio = sem RDAP.';
COMMENT ON COLUMN iana_asn_block.reference         IS 'Coluna Reference como publicada (ex.: [RFC6996]). NULL quando vazio.';
COMMENT ON COLUMN iana_asn_block.registration_date IS 'Coluna Registration Date como publicada: AAAA-MM ou AAAA-MM-DD (texto, sem inventar o dia). NULL quando vazio ou irreconhecível.';
COMMENT ON COLUMN iana_asn_block.source_file       IS 'Arquivo de origem (as-numbers-1 = ASNs de 16 bits | as-numbers-2 = ASNs de 32 bits).';
COMMENT ON COLUMN iana_asn_block.created_at        IS 'Timestamp de criação do registro (primeira vez que a faixa apareceu na fonte).';
COMMENT ON COLUMN iana_asn_block.updated_at        IS 'Timestamp de última alteração, determina a versão do registro; mantido pelo trigger trg_iana_asn_block_updated_at.';

CREATE TRIGGER trg_iana_asn_block_updated_at
    BEFORE UPDATE ON iana_asn_block
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

-- -----------------------------------------------------------------------------
-- Tabela: iana_prefix_block
-- -----------------------------------------------------------------------------
CREATE TABLE iana_prefix_block (
    uuid               uuid        PRIMARY KEY DEFAULT uuidv7(),
    prefix             cidr        NOT NULL,
    family             smallint    NOT NULL GENERATED ALWAYS AS (family(prefix)) STORED,
    designation        text        NOT NULL,
    registry           text,
    whois              text,
    rdap_urls          text[]      NOT NULL DEFAULT '{}',
    status             text        NOT NULL,
    allocation_date    text,
    note               text,
    source_file        text        NOT NULL,

    created_at         timestamptz NOT NULL DEFAULT NOW(),
    updated_at         timestamptz NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_iana_prefix_block_prefix    UNIQUE (prefix),
    CONSTRAINT chk_iana_prefix_block_registry CHECK (registry IN ('afrinic', 'apnic', 'arin', 'lacnic', 'ripencc')),
    CONSTRAINT chk_iana_prefix_block_status   CHECK (status ~ '^[A-Z][A-Z _-]*$'),
    CONSTRAINT chk_iana_prefix_block_date     CHECK (allocation_date ~ '^[0-9]{4}-[0-9]{2}(-[0-9]{2})?$'),
    CONSTRAINT chk_iana_prefix_block_source   CHECK (
        (source_file = 'ipv4-address-space'               AND family(prefix) = 4) OR
        (source_file = 'ipv6-unicast-address-assignments' AND family(prefix) = 6))
);

COMMENT ON TABLE iana_prefix_block IS
    'Blocos de endereço do registro da IANA: os 256 blocos /8 do IPv4
     (ipv4-address-space.csv, sempre 256 linhas) e os blocos IPv6 unicast
     (ipv6-unicast-address-assignments.csv, dentro de 2000::/3), com o RIR, o
     status (ALLOCATED, LEGACY, RESERVED) e a data. Blocos legados podem ser
     administrados por um RIR e delegados em parte por outro: o RIR daqui é o
     da coluna WHOIS da IANA. IP → bloco: prefix >>= X ORDER BY masklen(prefix)
     DESC LIMIT 1.';
COMMENT ON COLUMN iana_prefix_block.uuid            IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree.';
COMMENT ON COLUMN iana_prefix_block.prefix          IS 'Bloco em notação CIDR canônica. O IPv4 vem da IANA como "000/8", "045/8" e é gravado como 0.0.0.0/8, 45.0.0.0/8. Chave natural.';
COMMENT ON COLUMN iana_prefix_block.family          IS 'Família do endereço, calculada pelo banco (4=IPv4 | 6=IPv6).';
COMMENT ON COLUMN iana_prefix_block.designation     IS 'Coluna Designation como publicada: nome do RIR ("APNIC", "RIPE NCC"), "Administered by ARIN", titular legado ("Apple Computer Inc.", "US-DOD"), "Multicast", "Future use", "IANA - Private Use"...';
COMMENT ON COLUMN iana_prefix_block.registry        IS 'RIR responsável (afrinic | apnic | arin | lacnic | ripencc), tirado do domínio da coluna WHOIS (whois.ripe.net → ripencc) ou, sem WHOIS, da designação. NULL = bloco sem RIR (reservado, multicast, 6to4, IANA).';
COMMENT ON COLUMN iana_prefix_block.whois           IS 'Servidor WHOIS do bloco (coluna WHOIS, ex.: whois.arin.net, whois.iana.org). NULL quando vazio.';
COMMENT ON COLUMN iana_prefix_block.rdap_urls       IS 'URLs RDAP do bloco, separadas pelo collector quando a IANA as cola sem separador. Vazio = sem RDAP.';
COMMENT ON COLUMN iana_prefix_block.status          IS 'Coluna Status como publicada, em maiúsculas: ALLOCATED (entregue a um RIR), LEGACY (alocação anterior aos RIRs, hoje administrada por um), RESERVED (reservado pela IANA/IETF).';
COMMENT ON COLUMN iana_prefix_block.allocation_date IS 'Coluna Date como publicada: AAAA-MM (IPv4) ou AAAA-MM-DD (quase todo o IPv6), texto sem inventar o dia. NULL quando vazio ou irreconhecível.';
COMMENT ON COLUMN iana_prefix_block.note            IS 'Coluna Note com espaços e quebras de linha normalizados. No IPv4 traz só as marcas de nota de rodapé da IANA (ex.: [2][3]); no IPv6, texto (alocações anteriores incorporadas etc.). NULL quando vazio.';
COMMENT ON COLUMN iana_prefix_block.source_file     IS 'Arquivo de origem (ipv4-address-space | ipv6-unicast-address-assignments), coerente com family.';
COMMENT ON COLUMN iana_prefix_block.created_at      IS 'Timestamp de criação do registro (primeira vez que o bloco apareceu na fonte).';
COMMENT ON COLUMN iana_prefix_block.updated_at      IS 'Timestamp de última alteração, determina a versão do registro; mantido pelo trigger trg_iana_prefix_block_updated_at.';

CREATE TRIGGER trg_iana_prefix_block_updated_at
    BEFORE UPDATE ON iana_prefix_block
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

-- -----------------------------------------------------------------------------
-- iana_prefix_block: bloco da IANA que contém um IP ou prefixo (api-iana /ip e
-- /prefix), com os operadores >>= e && do inet.
-- -----------------------------------------------------------------------------
CREATE INDEX ix_iana_prefix_block_prefix_gist ON iana_prefix_block USING gist (prefix inet_ops);

-- -----------------------------------------------------------------------------
-- Tabela: iana_special_prefix
-- -----------------------------------------------------------------------------
CREATE TABLE iana_special_prefix (
    uuid                  uuid        PRIMARY KEY DEFAULT uuidv7(),
    prefix                cidr        NOT NULL,
    family                smallint    NOT NULL GENERATED ALWAYS AS (family(prefix)) STORED,
    name                  text        NOT NULL,
    rfc                   text,
    allocation_date       text,
    termination_date      text,

    source                boolean,
    destination           boolean,
    forwardable           boolean,
    globally_reachable    boolean,
    reserved_by_protocol  boolean,

    created_at            timestamptz NOT NULL DEFAULT NOW(),
    updated_at            timestamptz NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_iana_special_prefix_prefix      UNIQUE (prefix),
    CONSTRAINT chk_iana_special_prefix_allocation  CHECK (allocation_date  ~ '^[0-9]{4}-[0-9]{2}(-[0-9]{2})?$'),
    CONSTRAINT chk_iana_special_prefix_termination CHECK (termination_date ~ '^[0-9]{4}-[0-9]{2}(-[0-9]{2})?$')
);

COMMENT ON TABLE iana_special_prefix IS
    'Blocos IPv4 e IPv6 de uso especial (RFC 6890): privados, loopback,
     documentação, link-local, CGNAT, benchmarking... — a base da marcação de
     bogon. Vem de iana-ipv4-special-registry-1.csv e
     iana-ipv6-special-registry-1.csv, uma linha por bloco: uma célula com
     vários blocos ("192.0.0.170/32, 192.0.0.171/32") vira várias linhas com os
     mesmos atributos, e as marcas de nota de rodapé ("192.0.0.0/24 [2]",
     "False [1]") são retiradas. Blocos aninhados são normais (0.0.0.0/8 e
     0.0.0.0/32). IP → special: prefix >>= X (pode haver mais de um).';
COMMENT ON COLUMN iana_special_prefix.uuid                 IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree.';
COMMENT ON COLUMN iana_special_prefix.prefix               IS 'Bloco em notação CIDR canônica (coluna Address Block, sem a nota de rodapé). Chave natural.';
COMMENT ON COLUMN iana_special_prefix.family               IS 'Família do endereço, calculada pelo banco (4=IPv4 | 6=IPv6); indica também o arquivo de origem.';
COMMENT ON COLUMN iana_special_prefix.name                 IS 'Coluna Name como publicada (ex.: Private-Use, Loopback, "This network" com as aspas da IANA).';
COMMENT ON COLUMN iana_special_prefix.rfc                  IS 'Coluna RFC com as referências, espaços e quebras de linha normalizados (ex.: "[RFC8190] [RFC919], Section 7"). NULL quando vazio.';
COMMENT ON COLUMN iana_special_prefix.allocation_date      IS 'Coluna Allocation Date: AAAA-MM ou AAAA-MM-DD, texto. NULL quando vazio ou irreconhecível.';
COMMENT ON COLUMN iana_special_prefix.termination_date     IS 'Coluna Termination Date: AAAA-MM ou AAAA-MM-DD, texto. NULL = bloco em vigor (a IANA publica N/A); preenchido = registro encerrado (ex.: 192.88.99.0/24, 6to4 relay anycast).';
COMMENT ON COLUMN iana_special_prefix.source               IS 'Coluna Source: o bloco pode ser endereço de origem. NULL quando a IANA publica vazio ou N/A (registros encerrados).';
COMMENT ON COLUMN iana_special_prefix.destination          IS 'Coluna Destination: o bloco pode ser endereço de destino. NULL quando vazio ou N/A.';
COMMENT ON COLUMN iana_special_prefix.forwardable          IS 'Coluna Forwardable: roteadores podem encaminhar pacotes com esses endereços. NULL quando vazio ou N/A.';
COMMENT ON COLUMN iana_special_prefix.globally_reachable   IS 'Coluna Globally Reachable: false = não deve aparecer na Internet pública (bogon). NULL quando vazio ou N/A (ex.: TEREDO e 6to4 publicam "N/A [2]").';
COMMENT ON COLUMN iana_special_prefix.reserved_by_protocol IS 'Coluna Reserved-by-Protocol: reservado pela especificação do protocolo IP. NULL quando vazio ou N/A.';
COMMENT ON COLUMN iana_special_prefix.created_at           IS 'Timestamp de criação do registro (primeira vez que o bloco apareceu na fonte).';
COMMENT ON COLUMN iana_special_prefix.updated_at           IS 'Timestamp de última alteração, determina a versão do registro; mantido pelo trigger trg_iana_special_prefix_updated_at.';

CREATE TRIGGER trg_iana_special_prefix_updated_at
    BEFORE UPDATE ON iana_special_prefix
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

-- -----------------------------------------------------------------------------
-- iana_special_prefix: blocos especiais que contêm ou cruzam um IP ou prefixo
-- (marcação de bogon na api-iana /ip e /prefix), com >>=, <<= e &&.
-- -----------------------------------------------------------------------------
CREATE INDEX ix_iana_special_prefix_prefix_gist ON iana_special_prefix USING gist (prefix inet_ops);

-- -----------------------------------------------------------------------------
-- Tabela: iana_special_asn
-- -----------------------------------------------------------------------------
CREATE TABLE iana_special_asn (
    uuid               uuid        PRIMARY KEY DEFAULT uuidv7(),
    asn_start          bigint      NOT NULL,
    asn_end            bigint      NOT NULL,
    reason             text        NOT NULL,
    reference          text,

    created_at         timestamptz NOT NULL DEFAULT NOW(),
    updated_at         timestamptz NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_iana_special_asn_asn_start  UNIQUE (asn_start),
    CONSTRAINT chk_iana_special_asn_asn_start CHECK (asn_start BETWEEN 0 AND 4294967295),
    CONSTRAINT chk_iana_special_asn_asn_end   CHECK (asn_end   BETWEEN asn_start AND 4294967295)
);

COMMENT ON TABLE iana_special_asn IS
    'ASNs de uso especial (special-purpose-as-numbers.csv): 0, AS112, AS_TRANS
     (23456), documentação, uso privado, 65535 e 4294967295. ASN → special:
     asn_start <= X AND asn_end >= X.';
COMMENT ON COLUMN iana_special_asn.uuid       IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree.';
COMMENT ON COLUMN iana_special_asn.asn_start  IS 'Primeiro ASN da faixa (coluna AS Number: "64512-65534" → 64512). Chave natural.';
COMMENT ON COLUMN iana_special_asn.asn_end    IS 'Último ASN da faixa, inclusive (igual a asn_start para um ASN só).';
COMMENT ON COLUMN iana_special_asn.reason     IS 'Coluna Reason for Reservation como publicada (ex.: "For private use; reserved by [RFC6996]").';
COMMENT ON COLUMN iana_special_asn.reference  IS 'Coluna Reference como publicada (ex.: [RFC6996]). NULL quando vazio.';
COMMENT ON COLUMN iana_special_asn.created_at IS 'Timestamp de criação do registro (primeira vez que a faixa apareceu na fonte).';
COMMENT ON COLUMN iana_special_asn.updated_at IS 'Timestamp de última alteração, determina a versão do registro; mantido pelo trigger trg_iana_special_asn_updated_at.';

CREATE TRIGGER trg_iana_special_asn_updated_at
    BEFORE UPDATE ON iana_special_asn
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

-- -----------------------------------------------------------------------------
-- Tabela: iana_rdap_service
-- -----------------------------------------------------------------------------
CREATE TABLE iana_rdap_service (
    uuid               uuid        PRIMARY KEY DEFAULT uuidv7(),
    kind               text        NOT NULL,
    resource           text        NOT NULL,
    asn_start          bigint,
    asn_end            bigint,
    prefix             cidr,
    registry           text,
    urls               text[]      NOT NULL,

    created_at         timestamptz NOT NULL DEFAULT NOW(),
    updated_at         timestamptz NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_iana_rdap_service_resource UNIQUE (kind, resource),
    CONSTRAINT chk_iana_rdap_service_kind     CHECK (kind IN ('asn', 'ipv4', 'ipv6')),
    CONSTRAINT chk_iana_rdap_service_target   CHECK (
        (kind = 'asn'  AND asn_start IS NOT NULL AND asn_end IS NOT NULL AND prefix IS NULL
                       AND asn_start >= 0 AND asn_end BETWEEN asn_start AND 4294967295) OR
        (kind = 'ipv4' AND prefix IS NOT NULL AND family(prefix) = 4 AND asn_start IS NULL AND asn_end IS NULL) OR
        (kind = 'ipv6' AND prefix IS NOT NULL AND family(prefix) = 6 AND asn_start IS NULL AND asn_end IS NULL)),
    CONSTRAINT chk_iana_rdap_service_registry CHECK (registry IN ('afrinic', 'apnic', 'arin', 'lacnic', 'ripencc')),
    CONSTRAINT chk_iana_rdap_service_urls     CHECK (cardinality(urls) > 0)
);

COMMENT ON TABLE iana_rdap_service IS
    'Bootstrap RDAP da IANA (RFC 9224; data.iana.org/rdap/asn.json, ipv4.json,
     ipv6.json): qual servidor RDAP responde por cada faixa de ASN ou bloco IP.
     Uma linha por entrada de services[][0]. A data de publicação de cada JSON
     fica em iana_run.files (chave publication). ASN → RDAP: kind = ''asn'' AND
     asn_start <= X AND asn_end >= X; IP → RDAP: kind = ''ipv4''/''ipv6'' AND
     prefix >>= X ORDER BY masklen(prefix) DESC.';
COMMENT ON COLUMN iana_rdap_service.uuid       IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree.';
COMMENT ON COLUMN iana_rdap_service.kind       IS 'Arquivo de origem (asn = asn.json | ipv4 = ipv4.json | ipv6 = ipv6.json).';
COMMENT ON COLUMN iana_rdap_service.resource   IS 'Entrada canônica: faixa de ASN "1-1876" (ou "2043" para um só) ou bloco CIDR "41.0.0.0/8". Chave natural junto com kind.';
COMMENT ON COLUMN iana_rdap_service.asn_start  IS 'Primeiro ASN da faixa quando kind = asn; NULL nos blocos IP.';
COMMENT ON COLUMN iana_rdap_service.asn_end    IS 'Último ASN da faixa, inclusive, quando kind = asn; NULL nos blocos IP.';
COMMENT ON COLUMN iana_rdap_service.prefix     IS 'Bloco CIDR canônico quando kind = ipv4/ipv6; NULL nas faixas de ASN.';
COMMENT ON COLUMN iana_rdap_service.registry   IS 'RIR dono do servidor (afrinic | apnic | arin | lacnic | ripencc), tirado do domínio da primeira URL reconhecida. NULL = servidor de outro operador.';
COMMENT ON COLUMN iana_rdap_service.urls       IS 'URLs base do servidor RDAP na ordem publicada (em geral a https primeiro, às vezes também a http).';
COMMENT ON COLUMN iana_rdap_service.created_at IS 'Timestamp de criação do registro (primeira vez que a entrada apareceu na fonte).';
COMMENT ON COLUMN iana_rdap_service.updated_at IS 'Timestamp de última alteração, determina a versão do registro; mantido pelo trigger trg_iana_rdap_service_updated_at.';

CREATE TRIGGER trg_iana_rdap_service_updated_at
    BEFORE UPDATE ON iana_rdap_service
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

-- -----------------------------------------------------------------------------
-- iana_rdap_service: servidor RDAP de um IP ou prefixo (api-iana /ip e /prefix)
-- -----------------------------------------------------------------------------
CREATE INDEX ix_iana_rdap_service_prefix_gist ON iana_rdap_service USING gist (prefix inet_ops);

-- -----------------------------------------------------------------------------
-- iana_rdap_service: servidor RDAP de um ASN (api-iana /asn)
-- -----------------------------------------------------------------------------
CREATE INDEX ix_iana_rdap_service_asn ON iana_rdap_service(asn_start) WHERE kind = 'asn';

-- migrate:down

DROP TABLE iana_rdap_service;
DROP TABLE iana_special_asn;
DROP TABLE iana_special_prefix;
DROP TABLE iana_prefix_block;
DROP TABLE iana_asn_block;
DROP TABLE iana_run;
