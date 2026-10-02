-- migrate:up

-- =============================================================================
-- roothints: nomes e endereços dos 13 servidores raiz do DNS (root hints),
-- publicados pela InterNIC no arquivo named.root
-- Depende de: central (função set_updated_at)
-- Tabelas: roothints_run (tabela independente), roothints_server (tabela independente)
--
-- Escrita pelo collector-roothints, lida pela api-roothints. Fonte:
--   https://www.internic.net/domain/named.root  (hash: named.root.md5)
-- Arquivo no formato de zona do BIND: um cabeçalho em comentários (data da
-- última atualização e serial da zona raiz relacionada) e, por servidor, um
-- comentário, uma linha ". NS X.ROOT-SERVERS.NET.", uma A e uma AAAA. As
-- regras do parser estão em specs/fontes/roothints/fonte.md.
--
-- Nota: as tabelas espelham o último arquivo aplicado. Um servidor que some da
-- fonte é apagado aqui (sem soft delete); histórico e consolidação ficam para
-- as tabelas centrais da fase 2. A aplicação acontece numa transação só, então
-- a api-roothints nunca vê um arquivo pela metade.
-- =============================================================================

-- -----------------------------------------------------------------------------
-- Tabela: roothints_run
-- -----------------------------------------------------------------------------
CREATE TABLE roothints_run (
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

    last_update        date,
    zone_serial        bigint,

    servers            integer,
    ipv4_addresses     integer,
    ipv6_addresses     integer,

    server_inserted    integer,
    server_updated     integer,
    server_deleted     integer,

    warnings           jsonb       NOT NULL DEFAULT '[]'::jsonb,
    error              text,

    started_at         timestamptz NOT NULL,
    created_at         timestamptz NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_roothints_run_status      CHECK (status BETWEEN 0 AND 1),
    CONSTRAINT chk_roothints_run_md5         CHECK (md5    ~ '^[0-9a-f]{32}$'),
    CONSTRAINT chk_roothints_run_sha256      CHECK (sha256 ~ '^[0-9a-f]{64}$'),
    CONSTRAINT chk_roothints_run_zone_serial CHECK (zone_serial BETWEEN 0 AND 4294967295)
);

COMMENT ON TABLE roothints_run IS
    'Histórico das execuções do collector-roothints que baixaram um arquivo
     named.root novo da fonte (aplicado ou recusado). Verificações sem mudança
     não geram linha: ficam só em jobs.last_check_at. A última linha com
     status = 1 descreve o dataset que está em roothints_server, e seu uuid é a
     versão do dataset usada pela api-roothints no cache e no ETag.';
COMMENT ON COLUMN roothints_run.uuid            IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree. Nas linhas com status = 1 é também a versão do dataset.';
COMMENT ON COLUMN roothints_run.status          IS 'Resultado (0=falhou | 1=aplicado). Falha: conferência do MD5, parser, arquivo mais antigo que o aplicado, mínimo de servidores ou trava de remoção recusaram o arquivo, e a tabela ficou como estava.';
COMMENT ON COLUMN roothints_run.forced          IS 'true quando a execução foi forçada (--force): ignora "arquivo igual ao último", "arquivo mais antigo que o aplicado" e a trava de remoção em massa.';
COMMENT ON COLUMN roothints_run.url             IS 'URL de onde o arquivo foi baixado.';
COMMENT ON COLUMN roothints_run.http_status     IS 'Status HTTP da resposta do arquivo. NULL = falhou antes de receber resposta.';
COMMENT ON COLUMN roothints_run.etag            IS 'Cabeçalho ETag da resposta; reenviado em If-None-Match na próxima verificação. Na InterNIC muda a cada publicação da zona raiz, mesmo sem o conteúdo mudar.';
COMMENT ON COLUMN roothints_run.last_modified   IS 'Cabeçalho Last-Modified da resposta, como texto HTTP; reenviado em If-Modified-Since.';
COMMENT ON COLUMN roothints_run.md5             IS 'MD5 (hex minúsculo) do arquivo baixado. Comparado com o named.root.md5 publicado ao lado do arquivo, que é a checagem mais barata de mudança. NULL = falhou antes do download.';
COMMENT ON COLUMN roothints_run.sha256          IS 'SHA-256 (hex minúsculo) do arquivo baixado; detecta conteúdo igual quando o .md5 está fora do ar. NULL = falhou antes do download.';
COMMENT ON COLUMN roothints_run.bytes           IS 'Tamanho do arquivo baixado, em bytes.';
COMMENT ON COLUMN roothints_run.last_update     IS 'Data da linha "last update:" do cabeçalho do arquivo (ex.: September 24, 2026). NULL = o parser não leu o cabeçalho.';
COMMENT ON COLUMN roothints_run.zone_serial     IS 'Serial da zona raiz relacionada, da linha "related version of root zone:" do cabeçalho (AAAAMMDDnn, ex.: 2026092401). Um arquivo com serial menor que o da última execução aplicada é recusado sem --force. NULL = o parser não leu o cabeçalho.';
COMMENT ON COLUMN roothints_run.servers         IS 'Servidores raiz lidos do arquivo (linhas NS da raiz), isto é, linhas de roothints_server. NULL = o parser não chegou a rodar ou recusou o arquivo.';
COMMENT ON COLUMN roothints_run.ipv4_addresses  IS 'Servidores com endereço IPv4 (registro A) no arquivo.';
COMMENT ON COLUMN roothints_run.ipv6_addresses  IS 'Servidores com endereço IPv6 (registro AAAA) no arquivo.';
COMMENT ON COLUMN roothints_run.server_inserted IS 'Servidores novos gravados em roothints_server. NULL quando status = 0.';
COMMENT ON COLUMN roothints_run.server_updated  IS 'Servidores com endereço, TTL ou comentário alterado. NULL quando status = 0.';
COMMENT ON COLUMN roothints_run.server_deleted  IS 'Servidores que sumiram da fonte e foram apagados. NULL quando status = 0.';
COMMENT ON COLUMN roothints_run.warnings        IS 'Lista JSON (array de strings) com os avisos do parser (ex.: servidor sem IPv4 ou sem IPv6). Limitada às primeiras ocorrências.';
COMMENT ON COLUMN roothints_run.error           IS 'Mensagem de erro quando status = 0. NULL quando aplicado.';
COMMENT ON COLUMN roothints_run.started_at      IS 'Início da execução (antes do download).';
COMMENT ON COLUMN roothints_run.created_at      IS 'Timestamp de criação do registro, gravado no fim da execução.';

-- -----------------------------------------------------------------------------
-- roothints_run: versão atual do dataset (última execução aplicada)
-- -----------------------------------------------------------------------------
CREATE INDEX ix_roothints_run_applied ON roothints_run(created_at DESC) WHERE status = 1;

-- -----------------------------------------------------------------------------
-- Tabela: roothints_server
-- -----------------------------------------------------------------------------
CREATE TABLE roothints_server (
    uuid               uuid        PRIMARY KEY DEFAULT uuidv7(),
    name               text        NOT NULL,
    letter             text        NOT NULL,
    ipv4               inet,
    ipv6               inet,
    ns_ttl             integer     NOT NULL,
    ipv4_ttl           integer,
    ipv6_ttl           integer,
    note               text,

    created_at         timestamptz NOT NULL DEFAULT NOW(),
    updated_at         timestamptz NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_roothints_server_name     UNIQUE (name),
    CONSTRAINT uq_roothints_server_letter   UNIQUE (letter),
    CONSTRAINT chk_roothints_server_name    CHECK (name   ~ '^[a-z]\.root-servers\.net$'),
    CONSTRAINT chk_roothints_server_letter  CHECK (letter = left(name, 1)),
    CONSTRAINT chk_roothints_server_ipv4    CHECK (family(ipv4) = 4 AND masklen(ipv4) = 32),
    CONSTRAINT chk_roothints_server_ipv6    CHECK (family(ipv6) = 6 AND masklen(ipv6) = 128),
    CONSTRAINT chk_roothints_server_address CHECK (ipv4 IS NOT NULL OR ipv6 IS NOT NULL),
    CONSTRAINT chk_roothints_server_ns_ttl  CHECK (ns_ttl   BETWEEN 0 AND 2147483647),
    CONSTRAINT chk_roothints_server_v4_ttl  CHECK (ipv4_ttl BETWEEN 0 AND 2147483647 AND (ipv4_ttl IS NULL) = (ipv4 IS NULL)),
    CONSTRAINT chk_roothints_server_v6_ttl  CHECK (ipv6_ttl BETWEEN 0 AND 2147483647 AND (ipv6_ttl IS NULL) = (ipv6 IS NULL)),
    CONSTRAINT chk_roothints_server_note    CHECK (note <> '')
);

COMMENT ON TABLE roothints_server IS
    'Uma linha por servidor raiz do DNS listado no named.root da InterNIC (root
     hints: os 13 servidores A a M de root-servers.net, com o endereço IPv4 e o
     IPv6 de cada um). É o que um resolvedor usa para achar a raiz na partida.
     Servidor que some da fonte é apagado.';
COMMENT ON COLUMN roothints_server.uuid       IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree.';
COMMENT ON COLUMN roothints_server.name       IS 'Nome do servidor raiz em minúsculas e sem o ponto final (ex.: a.root-servers.net), vindo do alvo da linha ". NS" do arquivo. Chave natural.';
COMMENT ON COLUMN roothints_server.letter     IS 'Letra do servidor (a a m no arquivo atual), o primeiro rótulo de name. Única; é a chave das consultas por letra da api-roothints.';
COMMENT ON COLUMN roothints_server.ipv4       IS 'Endereço IPv4 do servidor (registro A do arquivo), como inet /32. NULL = o arquivo não traz A para o servidor (o coletor avisa).';
COMMENT ON COLUMN roothints_server.ipv6       IS 'Endereço IPv6 do servidor (registro AAAA do arquivo), como inet /128. NULL = o arquivo não traz AAAA para o servidor (o coletor avisa).';
COMMENT ON COLUMN roothints_server.ns_ttl     IS 'TTL, em segundos, da linha ". NS" do servidor (3600000, cerca de 41,7 dias, no arquivo atual).';
COMMENT ON COLUMN roothints_server.ipv4_ttl   IS 'TTL, em segundos, do registro A. NULL quando ipv4 é NULL.';
COMMENT ON COLUMN roothints_server.ipv6_ttl   IS 'TTL, em segundos, do registro AAAA. NULL quando ipv6 é NULL.';
COMMENT ON COLUMN roothints_server.note       IS 'Comentário do bloco do servidor no arquivo, sem o ";" (ex.: FORMERLY NS.INTERNIC.NET, OPERATED BY VERISIGN, INC.). NULL = o bloco não tem comentário.';
COMMENT ON COLUMN roothints_server.created_at IS 'Timestamp de criação do registro (primeira vez que o servidor apareceu na fonte).';
COMMENT ON COLUMN roothints_server.updated_at IS 'Timestamp de última alteração, determina a versão do registro; mantido pelo trigger trg_roothints_server_updated_at.';

CREATE TRIGGER trg_roothints_server_updated_at
    BEFORE UPDATE ON roothints_server
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

-- -----------------------------------------------------------------------------
-- roothints_server: as consultas da api-roothints por nome (WHERE name = $1) e
-- por letra (WHERE letter = $1), e a lista em ordem (ORDER BY letter), usam os
-- índices únicos implícitos de uq_roothints_server_name e
-- uq_roothints_server_letter; com 13 linhas não há outro índice a criar.
-- -----------------------------------------------------------------------------

-- migrate:down

DROP TABLE roothints_server;
DROP TABLE roothints_run;
