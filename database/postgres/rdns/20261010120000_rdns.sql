-- migrate:up

-- =============================================================================
-- rdns: DNS reverso (PTR) dos IPv4 dos prefixos cadastrados (sonda ativa)
-- Depende de: central (função set_updated_at e tabela jobs)
-- Tabelas: rdns_run (tabela independente), rdns_config (tabela independente),
--          rdns_worker (tabela independente), rdns_ptr (tabela independente),
--          rdns_prefix (FK → rdns_worker)
--
-- Escrita pelo collector-rdns (derivação, varredura, tempo real) e pelo
-- sistema de gestão do administrador (rdns_prefix e rdns_config, direto no
-- banco); lida pela api-rdns. Não há arquivo publicado: o coletor pergunta o
-- PTR de cada endereço a resolvedores recursivos e grava a resposta.
--
-- Os prefixos vêm dos blocos IPv4 do cgibr (cgibr_run, cgibr_prefix,
-- cgibr_asn), quebrados em /24 pelo coletor em tempo de execução — sem chave
-- estrangeira para as tabelas do cgibr —, e das linhas estáticas do
-- administrador.
--
-- Nota: nenhum prefixo sobrepõe outro (ex_rdns_prefix_overlap). O trigger
-- trg_rdns_prefix_split quebra nos /24 que faltam o prefixo novo que contém
-- linhas existentes e descarta o já coberto; trg_rdns_prefix_delete_ptr apaga
-- de rdns_ptr os IPs do prefixo removido. Um IP pertence, por continência
-- (prefix >>= ip), a um prefixo só.
--
-- Nota: o tempo real (api-rdns → collector-rdns) passa por uma fila no
-- Valkey, não por estas tabelas: chaves badblock:rdns:live:* descritas em
-- specs/fontes/rdns/dados.md#fila-de-tempo-real-valkey.
-- =============================================================================

-- -----------------------------------------------------------------------------
-- Tabela: rdns_run
-- -----------------------------------------------------------------------------
CREATE TABLE rdns_run (
    uuid               uuid        PRIMARY KEY DEFAULT uuidv7(),
    status             smallint    NOT NULL,
    forced             boolean     NOT NULL DEFAULT false,

    source             text        NOT NULL,
    source_version     uuid        NOT NULL,
    source_updated_at  timestamptz NOT NULL,

    blocks             integer,
    units              integer,
    inserted           integer,
    asn_updated        integer,
    covered            integer,
    not_in_source      integer,

    warnings           jsonb       NOT NULL DEFAULT '[]'::jsonb,
    error              text,

    started_at         timestamptz NOT NULL,
    created_at         timestamptz NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_rdns_run_status CHECK (status BETWEEN 0 AND 1),
    CONSTRAINT chk_rdns_run_source CHECK (source IN ('cgibr'))
);

COMMENT ON TABLE rdns_run IS
    'Derivações do collector-rdns: uma linha por passada que leu uma versão nova
     de uma fonte de origem (hoje só o cgibr) e derivou dela os prefixos /24 de
     rdns_prefix, aplicada ou falha. Passadas sem versão nova não geram linha
     (ficam em jobs.last_check_at). A linha nunca muda depois de gravada.';
COMMENT ON COLUMN rdns_run.uuid              IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree.';
COMMENT ON COLUMN rdns_run.status            IS 'Resultado (0=falhou | 1=aplicada). Falha depois de ler a versão da origem: o erro fica em error e rdns_prefix fica como estava.';
COMMENT ON COLUMN rdns_run.forced            IS 'true quando a passada veio de --force (deriva mesmo sem versão nova da origem).';
COMMENT ON COLUMN rdns_run.source            IS 'Fonte de origem dos prefixos derivados (cgibr).';
COMMENT ON COLUMN rdns_run.source_version    IS 'Versão do dataset da origem que foi lida: o uuid da última linha status = 1 de <origem>_run (cgibr_run.uuid).';
COMMENT ON COLUMN rdns_run.source_updated_at IS 'Quando a origem aplicou essa versão (cgibr_run.created_at).';
COMMENT ON COLUMN rdns_run.blocks            IS 'Blocos IPv4 lidos da origem. NULL quando status = 0 antes da contagem.';
COMMENT ON COLUMN rdns_run.units             IS 'Prefixos /24 derivados dos blocos (um /20 vira 16). Blocos mais longos que /24 não viram unidade.';
COMMENT ON COLUMN rdns_run.inserted          IS '/24 novos gravados em rdns_prefix (source = cgibr).';
COMMENT ON COLUMN rdns_run.asn_updated       IS 'Linhas source = cgibr cujo ASN foi corrigido. Linhas static nunca são tocadas.';
COMMENT ON COLUMN rdns_run.covered           IS '/24 que já estavam em rdns_prefix ou cobertos por outra linha (units - inserted).';
COMMENT ON COLUMN rdns_run.not_in_source     IS 'Linhas source = cgibr que esta versão da origem não traz mais; continuam em rdns_prefix até o administrador decidir.';
COMMENT ON COLUMN rdns_run.warnings          IS 'Lista JSON (array de strings) de avisos, como blocos mais longos que /24 ignorados: os 50 primeiros e, se houver mais, "... e mais N avisos".';
COMMENT ON COLUMN rdns_run.error             IS 'Motivo da falha quando status = 0. NULL quando aplicada.';
COMMENT ON COLUMN rdns_run.started_at        IS 'Início da passada da derivação.';
COMMENT ON COLUMN rdns_run.created_at        IS 'Timestamp de criação do registro, gravado no fim da passada.';

-- -----------------------------------------------------------------------------
-- rdns_run: a última versão derivada de cada origem (coletor e /rdns/meta)
-- -----------------------------------------------------------------------------
CREATE INDEX ix_rdns_run_applied ON rdns_run(source, created_at DESC) WHERE status = 1;

-- -----------------------------------------------------------------------------
-- Tabela: rdns_config
--
-- Chaves conhecidas (chk_rdns_config_key):
--   rate_limit     consultas DNS por segundo, POR COLETOR (inteiro >= 1)
--   scan_interval  meta por prefixo, em duração Go (ex.: 24h)
-- Sem linha, ou com valor inválido, cada coletor usa a reserva do próprio
-- ambiente (RATE_LIMIT, SCAN_INTERVAL). Os coletores releem a cada 30 s.
-- -----------------------------------------------------------------------------
CREATE TABLE rdns_config (
    uuid               uuid        PRIMARY KEY DEFAULT uuidv7(),
    key                text        NOT NULL,
    value              text        NOT NULL,

    created_at         timestamptz NOT NULL DEFAULT NOW(),
    updated_at         timestamptz NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_rdns_config_key  UNIQUE (key),
    CONSTRAINT chk_rdns_config_key CHECK (key IN ('rate_limit', 'scan_interval'))
);

COMMENT ON TABLE rdns_config IS
    'Parâmetros que valem para todos os collector-rdns, gravados pelo
     administrador (INSERT ... ON CONFLICT (key) DO UPDATE). Cada coletor relê a
     tabela a cada 30 s; a chave que falta, ou com valor inválido, vale a
     variável de reserva do ambiente dele (RATE_LIMIT, SCAN_INTERVAL).';
COMMENT ON COLUMN rdns_config.uuid       IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree.';
COMMENT ON COLUMN rdns_config.key        IS 'Nome do parâmetro (rate_limit = consultas DNS por segundo por coletor, inteiro >= 1 | scan_interval = meta por prefixo, duração Go como 24h).';
COMMENT ON COLUMN rdns_config.value      IS 'Valor do parâmetro, em texto (ex.: 512 para rate_limit, 24h para scan_interval).';
COMMENT ON COLUMN rdns_config.created_at IS 'Timestamp de criação do registro.';
COMMENT ON COLUMN rdns_config.updated_at IS 'Timestamp de última alteração, determina a versão do registro; mantido pelo trigger trg_rdns_config_updated_at.';

CREATE TRIGGER trg_rdns_config_updated_at
    BEFORE UPDATE ON rdns_config
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

-- -----------------------------------------------------------------------------
-- Tabela: rdns_worker
-- -----------------------------------------------------------------------------
CREATE TABLE rdns_worker (
    uuid               uuid        PRIMARY KEY DEFAULT uuidv7(),
    host               text        NOT NULL,
    version            text        NOT NULL,
    rate_limit         integer     NOT NULL,
    queries            bigint      NOT NULL DEFAULT 0,
    live_queries       bigint      NOT NULL DEFAULT 0,
    prefixes           bigint      NOT NULL DEFAULT 0,

    created_at         timestamptz NOT NULL DEFAULT NOW(),
    updated_at         timestamptz NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE rdns_worker IS
    'Uma linha por instância do collector-rdns no ar. A instância grava a linha
     na subida e a renova a cada 30 s (a batida); viva = sinal há menos de 90 s.
     O encerramento limpo apaga a própria linha; a de uma instância morta sai
     na manutenção da derivação, 24 h depois. Apagar a linha devolve à fila os
     prefixos que ela varria (rdns_prefix.scan_worker vira NULL).';
COMMENT ON COLUMN rdns_worker.uuid         IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree. Gerada pela instância na subida e citada em rdns_prefix.scan_worker e nas respostas de tempo real.';
COMMENT ON COLUMN rdns_worker.host         IS 'os.Hostname() da instância (no Docker, o ID curto do container).';
COMMENT ON COLUMN rdns_worker.version      IS 'Versão do collector-rdns.';
COMMENT ON COLUMN rdns_worker.rate_limit   IS 'Taxa em vigor na instância, em consultas DNS por segundo.';
COMMENT ON COLUMN rdns_worker.queries      IS 'Consultas DNS enviadas desde a subida, contando novas tentativas e repetições por TCP.';
COMMENT ON COLUMN rdns_worker.live_queries IS 'Pedidos de tempo real atendidos desde a subida.';
COMMENT ON COLUMN rdns_worker.prefixes     IS 'Prefixos varridos até o fim (resultado gravado) desde a subida.';
COMMENT ON COLUMN rdns_worker.created_at   IS 'Timestamp de criação do registro (subida da instância).';
COMMENT ON COLUMN rdns_worker.updated_at   IS 'Último sinal da instância (a batida a cada 30 s); mantido pelo trigger trg_rdns_worker_updated_at.';

CREATE TRIGGER trg_rdns_worker_updated_at
    BEFORE UPDATE ON rdns_worker
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

-- -----------------------------------------------------------------------------
-- Tabela: rdns_ptr
--
-- PK natural ip (exceção à PK uuidv7): com dezenas de milhões de linhas, uma
-- PK uuid somaria bytes e um segundo índice só para achar o IP. O B-tree da
-- PK serve a busca de um IP (=) e a de um prefixo (ip <<= prefixo).
-- -----------------------------------------------------------------------------
CREATE TABLE rdns_ptr (
    ip                 inet        PRIMARY KEY,
    status             smallint    NOT NULL,
    name               text,
    names              text[],

    created_at         timestamptz NOT NULL DEFAULT NOW(),
    updated_at         timestamptz NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_rdns_ptr_ip     CHECK (family(ip) = 4 AND masklen(ip) = 32),
    CONSTRAINT chk_rdns_ptr_status CHECK (status BETWEEN 1 AND 4),
    CONSTRAINT chk_rdns_ptr_name   CHECK ((status = 1 AND names IS NOT NULL AND cardinality(names) >= 1 AND coalesce(name = names[1], false))
                                       OR (status <> 1 AND name IS NULL AND names IS NULL))
);

COMMENT ON TABLE rdns_ptr IS
    'Resultado da consulta PTR de cada IPv4 que tem nome ou que falhou. Sem PTR
     (NXDOMAIN ou NODATA) = sem linha: num prefixo já varrido, o IP não tem PTR;
     num nunca varrido, não foi consultado. Uma falha nunca apaga um PTR; a
     linha só é regravada quando muda. Sem TTL, sem histórico e sem data por IP:
     a data em que o dado foi conferido é o rdns_prefix.last_scan_at do prefixo
     que contém o IP (prefix >>= ip). Só o collector-rdns escreve aqui.';
COMMENT ON COLUMN rdns_ptr.ip         IS 'O endereço IPv4 (/32). Chave primária natural.';
COMMENT ON COLUMN rdns_ptr.status     IS 'Estado da última consulta (1=ptr | 2=servfail | 3=refused | 4=timeout). servfail: SERVFAIL em dois servidores; refused: REFUSED ou outro RCODE em todas as tentativas; timeout: sem resposta (ou erro de rede) em todas as tentativas.';
COMMENT ON COLUMN rdns_ptr.name       IS 'O primeiro PTR em ordem de bytes (= names[1]); NULL nas falhas.';
COMMENT ON COLUMN rdns_ptr.names      IS 'Todos os PTR da resposta, sem repetição, em ordem de bytes: minúsculas, sem o ponto final, bytes fora do ASCII como \DDD. NULL nas falhas.';
COMMENT ON COLUMN rdns_ptr.created_at IS 'Timestamp de criação do registro (primeira vez que o IP teve linha).';
COMMENT ON COLUMN rdns_ptr.updated_at IS 'Timestamp de última alteração de status, name ou names, determina a versão do registro; mantido pelo trigger trg_rdns_ptr_updated_at.';

CREATE TRIGGER trg_rdns_ptr_updated_at
    BEFORE UPDATE ON rdns_ptr
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

-- -----------------------------------------------------------------------------
-- Tabela: rdns_prefix
--
-- Cadastro (prefix, asn, source, active) e estado da varredura (scan_*,
-- last_scan_*) na mesma linha: a fila é a própria tabela. O administrador
-- inclui (source = static), desativa, remove e pede nova varredura (rescan);
-- nunca troca o prefix de uma linha nem escreve scan_*/last_scan_*. A regra
-- da sonda (só IPv4 de /22 a /24 é varrido) fica no software: o banco aceita
-- qualquer cidr.
-- -----------------------------------------------------------------------------
CREATE TABLE rdns_prefix (
    uuid               uuid        PRIMARY KEY DEFAULT uuidv7(),
    prefix             cidr        NOT NULL,
    asn                bigint      NOT NULL DEFAULT 0,
    source             text        NOT NULL DEFAULT 'static',
    active             boolean     NOT NULL DEFAULT true,
    rescan             boolean     NOT NULL DEFAULT false,

    created_at         timestamptz NOT NULL DEFAULT NOW(),
    updated_at         timestamptz NOT NULL DEFAULT NOW(),

    scan_worker        uuid,
    scan_started_at    timestamptz,
    last_scan_at       timestamptz,
    last_scan_ms       integer,
    last_scan_ptr      integer,
    last_scan_no_ptr   integer,
    last_scan_failed   integer,
    last_scan_changed  integer,

    CONSTRAINT ex_rdns_prefix_overlap EXCLUDE USING gist (prefix inet_ops WITH &&),
    CONSTRAINT chk_rdns_prefix_asn    CHECK (asn BETWEEN 0 AND 4294967295),
    CONSTRAINT chk_rdns_prefix_source CHECK (source IN ('cgibr', 'static')),

    CONSTRAINT fk_rdns_prefix_worker  FOREIGN KEY (scan_worker)
        REFERENCES rdns_worker(uuid) ON DELETE SET NULL
);

COMMENT ON TABLE rdns_prefix IS
    'Prefixos IPv4 cuja zona reversa o collector-rdns varre, com o estado da
     varredura. source = cgibr: /24 derivados dos blocos do cgibr pelo coletor;
     source = static: gravados pelo administrador direto no banco. Prefixos só
     entram sozinhos, nunca saem sozinhos: desativar (active = false), remover
     (DELETE, que apaga também os IPs em rdns_ptr) e pedir nova varredura
     (rescan = true) é do administrador. Nenhum prefixo sobrepõe outro
     (ex_rdns_prefix_overlap); um INSERT que sobrepõe vira os /24 que faltam
     (trg_rdns_prefix_split). A fila só pega IPv4 de /22 a /24 ativos.';
COMMENT ON COLUMN rdns_prefix.uuid              IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree.';
COMMENT ON COLUMN rdns_prefix.prefix            IS 'Prefixo em CIDR. A varredura só pega IPv4 de /22 a /24; linhas fora disso (IPv6, /21, /25...) ficam, mas nunca são varridas. Nunca troque o prefix de uma linha: apague e insira.';
COMMENT ON COLUMN rdns_prefix.asn               IS 'ASN do prefixo (0 a 4294967295); 0 = sem ASN (ex.: bloco privado). Nas linhas cgibr, o ASN do bloco de origem, corrigido pela derivação.';
COMMENT ON COLUMN rdns_prefix.source            IS 'Origem da linha (cgibr = derivada pelo coletor dos blocos do cgibr | static = gravada pelo administrador). A derivação nunca toca uma linha static.';
COMMENT ON COLUMN rdns_prefix.active            IS 'false tira o prefixo da varredura e do tempo real sem apagar os resultados (a api-rdns continua servindo).';
COMMENT ON COLUMN rdns_prefix.rescan            IS 'true fura a fila (o prefixo passa na frente); o coletor zera ao concluir a varredura. Um pedido feito durante a varredura não se perde.';
COMMENT ON COLUMN rdns_prefix.created_at        IS 'Timestamp de criação do registro (entrada do prefixo).';
COMMENT ON COLUMN rdns_prefix.updated_at        IS 'Timestamp da última mudança de cadastro (prefix, asn, source, active); mantido pelo trigger trg_rdns_prefix_updated_at, que não anda com o estado da varredura nem com rescan.';
COMMENT ON COLUMN rdns_prefix.scan_worker       IS 'Instância que está varrendo o prefixo (FK → rdns_worker.uuid, vira NULL quando a instância é apagada); NULL = livre.';
COMMENT ON COLUMN rdns_prefix.scan_started_at   IS 'Quando scan_worker pegou o prefixo; o lease vence em scan_started_at + SCAN_LEASE e outra instância pode retomá-lo.';
COMMENT ON COLUMN rdns_prefix.last_scan_at      IS 'Fim da última varredura completa: a data do dado de todos os IPs do prefixo. NULL = nunca varrido.';
COMMENT ON COLUMN rdns_prefix.last_scan_ms      IS 'Duração da última varredura completa, em milissegundos.';
COMMENT ON COLUMN rdns_prefix.last_scan_ptr     IS 'IPs com PTR na última varredura completa.';
COMMENT ON COLUMN rdns_prefix.last_scan_no_ptr  IS 'IPs sem PTR (NXDOMAIN ou NODATA) na última varredura completa.';
COMMENT ON COLUMN rdns_prefix.last_scan_failed  IS 'IPs com falha (servfail, refused, timeout) na última varredura completa.';
COMMENT ON COLUMN rdns_prefix.last_scan_changed IS 'Linhas de rdns_ptr inseridas, alteradas ou apagadas pela última varredura completa.';

-- -----------------------------------------------------------------------------
-- Função: rdns_prefix_split (trigger BEFORE INSERT de rdns_prefix)
--
-- SET enable_seqscan = off: o plano das consultas da função fica em cache na
-- sessão. Feito com a tabela vazia (a primeira derivação), seria uma leitura
-- sequencial — e cada um dos ~309 mil INSERT da primeira derivação leria a
-- tabela inteira, que cresce a cada linha. O índice GiST da
-- ex_rdns_prefix_overlap serve as três consultas (&& e >>=).
-- -----------------------------------------------------------------------------
CREATE FUNCTION rdns_prefix_split() RETURNS trigger
    LANGUAGE plpgsql
    SET enable_seqscan = off AS
$$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM rdns_prefix p WHERE p.prefix && NEW.prefix) THEN
        RETURN NEW;                                   -- sem sobreposição: entra como veio
    END IF;
    IF EXISTS (SELECT 1 FROM rdns_prefix p WHERE p.prefix >>= NEW.prefix) THEN
        RETURN NULL;                                  -- já coberto inteiro: descartado
    END IF;
    IF family(NEW.prefix) = 4 AND masklen(NEW.prefix) <= 24 THEN
        INSERT INTO rdns_prefix (prefix, asn, source, active, rescan)
        SELECT u.prefix, NEW.asn, NEW.source, NEW.active, NEW.rescan
          FROM (SELECT set_masklen(network(NEW.prefix) + i * 256, 24)::cidr AS prefix
                  FROM generate_series(0::bigint, (1::bigint << (24 - masklen(NEW.prefix))) - 1) AS i) u
         WHERE NOT EXISTS (SELECT 1 FROM rdns_prefix p WHERE p.prefix && u.prefix);
    END IF;
    RETURN NULL;                                      -- o original nunca entra
END;
$$;

COMMENT ON FUNCTION rdns_prefix_split() IS
    'Trigger BEFORE INSERT de rdns_prefix (trg_rdns_prefix_split). Prefixo novo
     sem sobreposição entra como veio. Coberto inteiro por uma linha existente:
     descartado. IPv4 de até /24 que contém linhas existentes: vira os /24 que
     faltam (herdando asn, source, active e rescan) e o original é descartado;
     um /24 só parcialmente coberto (por uma linha menor que /24) não entra.
     IPv6 ou IPv4 mais longo que /24 que sobrepõe alguém: descartado. Um INSERT
     que cai aqui devolve 0 linhas; os /24 aparecem numa consulta depois.';

-- -----------------------------------------------------------------------------
-- Função: rdns_prefix_delete_ptr (trigger AFTER DELETE de rdns_prefix)
--
-- SET enable_seqscan = off pelo mesmo motivo: o B-tree da PK de rdns_ptr
-- serve o ip <<= prefixo, qualquer que fosse o tamanho da tabela quando o
-- plano foi feito.
-- -----------------------------------------------------------------------------
CREATE FUNCTION rdns_prefix_delete_ptr() RETURNS trigger
    LANGUAGE plpgsql
    SET enable_seqscan = off AS
$$
BEGIN
    DELETE FROM rdns_ptr WHERE ip <<= OLD.prefix;
    RETURN NULL;
END;
$$;

COMMENT ON FUNCTION rdns_prefix_delete_ptr() IS
    'Trigger AFTER DELETE de rdns_prefix (trg_rdns_prefix_delete_ptr): apagar um
     prefixo apaga de rdns_ptr os resultados dos IPs dele (ip <<= prefix).';

-- -----------------------------------------------------------------------------
-- rdns_prefix: triggers (cadastro, sobreposição e remoção)
-- -----------------------------------------------------------------------------
CREATE TRIGGER trg_rdns_prefix_updated_at
    BEFORE UPDATE OF prefix, asn, source, active ON rdns_prefix
    FOR EACH ROW
    WHEN ((OLD.prefix, OLD.asn, OLD.source, OLD.active) IS DISTINCT FROM (NEW.prefix, NEW.asn, NEW.source, NEW.active))
    EXECUTE FUNCTION set_updated_at();

CREATE TRIGGER trg_rdns_prefix_split
    BEFORE INSERT ON rdns_prefix
    FOR EACH ROW
    EXECUTE FUNCTION rdns_prefix_split();

CREATE TRIGGER trg_rdns_prefix_delete_ptr
    AFTER DELETE ON rdns_prefix
    FOR EACH ROW
    EXECUTE FUNCTION rdns_prefix_delete_ptr();

-- -----------------------------------------------------------------------------
-- rdns_prefix: a fila de varredura, na ordem dela (rescan → nunca varrido →
-- last_scan_at mais antigo), só com os prefixos que a regra da sonda aceita.
-- -----------------------------------------------------------------------------
CREATE INDEX ix_rdns_prefix_queue ON rdns_prefix(rescan DESC, last_scan_at ASC NULLS FIRST)
    WHERE active AND family(prefix) = 4 AND masklen(prefix) BETWEEN 22 AND 24;

-- -----------------------------------------------------------------------------
-- rdns_prefix: prefixos de uma instância (cascata da FK e devolução no
-- encerramento)
-- -----------------------------------------------------------------------------
CREATE INDEX ix_rdns_prefix_worker ON rdns_prefix(scan_worker) WHERE scan_worker IS NOT NULL;

-- migrate:down

DROP TABLE rdns_prefix;
DROP TABLE rdns_ptr;
DROP TABLE rdns_worker;
DROP TABLE rdns_config;
DROP TABLE rdns_run;
DROP FUNCTION rdns_prefix_split();
DROP FUNCTION rdns_prefix_delete_ptr();
