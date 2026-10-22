-- migrate:up

-- =============================================================================
-- registrobr_rdapnum: RDAP do registro.br por ASN e bloco (sonda ativa)
-- Depende de: central (função set_updated_at e tabela jobs)
-- Tabelas: registrobr_rdapnum_run (tabela independente),
--          registrobr_rdapnum_config (tabela independente),
--          registrobr_rdapnum_worker (tabela independente),
--          registrobr_rdapnum_target (FK → _worker, _response),
--          registrobr_rdapnum_response (FK → _target),
--          registrobr_rdapnum_entity (FK → _response),
--          registrobr_rdapnum_autnum (FK → _target, _response, _entity),
--          registrobr_rdapnum_network (FK → _target, _response, _entity),
--          registrobr_rdapnum_link (FK → _entity, _autnum, _network),
--          registrobr_rdapnum_reverse (FK → _network)
--
-- Escrita pelo collector-registrobr-rdapnum (derivação, coleta,
-- processamento) e pelo sistema de gestão do administrador (_target e
-- _config, direto no banco); lida pela api-registrobr-rdapnum. Não há
-- arquivo publicado: o coletor consulta https://rdap.registro.br/autnum/<asn>
-- e /ip/<bloco> e guarda a resposta crua (_response) antes de processá-la.
--
-- Os alvos vêm dos ASNs e blocos do cgibr (cgibr_run, cgibr_asn,
-- cgibr_prefix), lidos pelo coletor em tempo de execução — sem chave
-- estrangeira para as tabelas do cgibr —, e das linhas estáticas do
-- administrador.
--
-- Nota: o servidor trunca as respostas de quem consulta demais (remarks
-- "object truncated due to server policy": sem e-mails, endereços e CNPJ).
-- A resposta truncada é guardada em _response (result = 2), mas NUNCA é
-- processada e nunca vira _target.response_uuid: o dado bom anterior fica.
--
-- Nota: o pedido de atualização (api-registrobr-rdapnum → coletor) passa por
-- uma fila no Valkey, não por estas tabelas: chaves
-- badblock:registrobr-rdapnum:refresh:* descritas em
-- specs/fontes/registrobr/rdapnum/dados.md#fila-de-atualização-valkey.
-- =============================================================================

-- -----------------------------------------------------------------------------
-- Tabela: registrobr_rdapnum_run
-- -----------------------------------------------------------------------------
CREATE TABLE registrobr_rdapnum_run (
    uuid               uuid        PRIMARY KEY DEFAULT uuidv7(),
    status             smallint    NOT NULL,
    forced             boolean     NOT NULL DEFAULT false,

    source             text        NOT NULL,
    source_version     uuid        NOT NULL,
    source_updated_at  timestamptz NOT NULL,

    asns               integer,
    prefixes           integer,
    inserted           integer,
    not_in_source      integer,

    warnings           jsonb       NOT NULL DEFAULT '[]'::jsonb,
    error              text,

    started_at         timestamptz NOT NULL,
    created_at         timestamptz NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_registrobr_rdapnum_run_status CHECK (status BETWEEN 0 AND 1),
    CONSTRAINT chk_registrobr_rdapnum_run_source CHECK (source IN ('cgibr'))
);

COMMENT ON TABLE registrobr_rdapnum_run IS
    'Derivações do collector-registrobr-rdapnum: uma linha por passada que leu
     uma versão nova de uma fonte de origem (hoje só o cgibr) e derivou dela os
     alvos de registrobr_rdapnum_target, aplicada ou falha. Passadas sem versão
     nova não geram linha (ficam em jobs.last_check_at). A linha nunca muda
     depois de gravada.';
COMMENT ON COLUMN registrobr_rdapnum_run.uuid              IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree.';
COMMENT ON COLUMN registrobr_rdapnum_run.status            IS 'Resultado (0=falhou | 1=aplicada). Falha depois de ler a versão da origem: o erro fica em error e os alvos ficam como estavam.';
COMMENT ON COLUMN registrobr_rdapnum_run.forced            IS 'true quando a passada veio de --force (deriva mesmo sem versão nova da origem).';
COMMENT ON COLUMN registrobr_rdapnum_run.source            IS 'Fonte de origem dos alvos derivados (cgibr).';
COMMENT ON COLUMN registrobr_rdapnum_run.source_version    IS 'Versão do dataset da origem que foi lida: o uuid da última linha status = 1 de cgibr_run.';
COMMENT ON COLUMN registrobr_rdapnum_run.source_updated_at IS 'Quando a origem aplicou essa versão (cgibr_run.created_at).';
COMMENT ON COLUMN registrobr_rdapnum_run.asns              IS 'ASNs lidos de cgibr_asn. NULL quando status = 0 antes da contagem.';
COMMENT ON COLUMN registrobr_rdapnum_run.prefixes          IS 'Blocos lidos de cgibr_prefix (IPv4 e IPv6). NULL quando status = 0 antes da contagem.';
COMMENT ON COLUMN registrobr_rdapnum_run.inserted          IS 'Alvos novos gravados em registrobr_rdapnum_target (source = cgibr). O que já existia, derivado ou estático, fica como estava.';
COMMENT ON COLUMN registrobr_rdapnum_run.not_in_source     IS 'Alvos source = cgibr que esta versão da origem não traz mais; continuam em registrobr_rdapnum_target até o administrador decidir.';
COMMENT ON COLUMN registrobr_rdapnum_run.warnings          IS 'Lista JSON (array de strings) de avisos da passada: os 50 primeiros e, se houver mais, "... e mais N avisos".';
COMMENT ON COLUMN registrobr_rdapnum_run.error             IS 'Motivo da falha quando status = 0. NULL quando aplicada.';
COMMENT ON COLUMN registrobr_rdapnum_run.started_at        IS 'Início da passada da derivação.';
COMMENT ON COLUMN registrobr_rdapnum_run.created_at        IS 'Timestamp de criação do registro, gravado no fim da passada.';

-- -----------------------------------------------------------------------------
-- registrobr_rdapnum_run: a última versão derivada de cada origem (coletor e
-- /registrobr/rdapnum/meta)
-- -----------------------------------------------------------------------------
CREATE INDEX ix_registrobr_rdapnum_run_applied ON registrobr_rdapnum_run(source, created_at DESC) WHERE status = 1;

-- -----------------------------------------------------------------------------
-- Tabela: registrobr_rdapnum_config
--
-- Chaves conhecidas (chk_registrobr_rdapnum_config_key):
--   rate_limit     consultas RDAP POR MINUTO, POR COLETOR (inteiro >= 1)
--   scan_interval  meta por alvo, em duração Go (ex.: 168h)
-- Sem linha, ou com valor inválido, cada coletor usa a reserva do próprio
-- ambiente (RATE_LIMIT, SCAN_INTERVAL). Os coletores releem a cada 30 s.
-- -----------------------------------------------------------------------------
CREATE TABLE registrobr_rdapnum_config (
    uuid               uuid        PRIMARY KEY DEFAULT uuidv7(),
    key                text        NOT NULL,
    value              text        NOT NULL,

    created_at         timestamptz NOT NULL DEFAULT NOW(),
    updated_at         timestamptz NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_registrobr_rdapnum_config_key  UNIQUE (key),
    CONSTRAINT chk_registrobr_rdapnum_config_key CHECK (key IN ('rate_limit', 'scan_interval'))
);

COMMENT ON TABLE registrobr_rdapnum_config IS
    'Parâmetros que valem para todos os collector-registrobr-rdapnum, gravados
     pelo administrador (INSERT ... ON CONFLICT (key) DO UPDATE). Cada coletor
     relê a tabela a cada 30 s; a chave que falta, ou com valor inválido, vale
     a variável de reserva do ambiente dele (RATE_LIMIT, SCAN_INTERVAL).';
COMMENT ON COLUMN registrobr_rdapnum_config.uuid       IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree.';
COMMENT ON COLUMN registrobr_rdapnum_config.key        IS 'Nome do parâmetro (rate_limit = consultas RDAP por minuto por coletor, inteiro >= 1 | scan_interval = meta por alvo, duração Go como 168h).';
COMMENT ON COLUMN registrobr_rdapnum_config.value      IS 'Valor do parâmetro, em texto (ex.: 32 para rate_limit, 168h para scan_interval).';
COMMENT ON COLUMN registrobr_rdapnum_config.created_at IS 'Timestamp de criação do registro.';
COMMENT ON COLUMN registrobr_rdapnum_config.updated_at IS 'Timestamp de última alteração, determina a versão do registro; mantido pelo trigger trg_registrobr_rdapnum_config_updated_at.';

CREATE TRIGGER trg_registrobr_rdapnum_config_updated_at
    BEFORE UPDATE ON registrobr_rdapnum_config
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

-- -----------------------------------------------------------------------------
-- Tabela: registrobr_rdapnum_worker
-- -----------------------------------------------------------------------------
CREATE TABLE registrobr_rdapnum_worker (
    uuid               uuid        PRIMARY KEY DEFAULT uuidv7(),
    host               text        NOT NULL,
    version            text        NOT NULL,
    rate_limit         integer     NOT NULL,
    proxy              boolean     NOT NULL,
    requests           bigint      NOT NULL DEFAULT 0,
    complete           bigint      NOT NULL DEFAULT 0,
    truncated          bigint      NOT NULL DEFAULT 0,
    not_found          bigint      NOT NULL DEFAULT 0,
    errors             bigint      NOT NULL DEFAULT 0,
    refreshes          bigint      NOT NULL DEFAULT 0,
    egress_ip          inet,
    egress_ips         integer     NOT NULL DEFAULT 0,
    paused_until       timestamptz,

    created_at         timestamptz NOT NULL DEFAULT NOW(),
    updated_at         timestamptz NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE registrobr_rdapnum_worker IS
    'Uma linha por instância do collector-registrobr-rdapnum no ar. A instância
     grava a linha na subida e a renova a cada 30 s (a batida); viva = sinal há
     menos de 90 s. O encerramento limpo apaga a própria linha; a de uma
     instância morta sai na manutenção da derivação, 24 h depois. Apagar a
     linha devolve à fila os alvos que ela consultava
     (registrobr_rdapnum_target.scan_worker vira NULL).';
COMMENT ON COLUMN registrobr_rdapnum_worker.uuid         IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree. Gerada pela instância na subida e citada em registrobr_rdapnum_target.scan_worker e registrobr_rdapnum_response.worker.';
COMMENT ON COLUMN registrobr_rdapnum_worker.host         IS 'os.Hostname() da instância (no Docker, o ID curto do container).';
COMMENT ON COLUMN registrobr_rdapnum_worker.version      IS 'Versão do collector-registrobr-rdapnum.';
COMMENT ON COLUMN registrobr_rdapnum_worker.rate_limit   IS 'Taxa em vigor na instância, em consultas RDAP por minuto.';
COMMENT ON COLUMN registrobr_rdapnum_worker.proxy        IS 'true quando a instância sai por um proxy (PROXY_URL definida). A URL do proxy, que leva senha, nunca é gravada no banco.';
COMMENT ON COLUMN registrobr_rdapnum_worker.requests     IS 'Consultas RDAP enviadas desde a subida (todas contam, inclusive as truncadas e as que deram erro).';
COMMENT ON COLUMN registrobr_rdapnum_worker.complete     IS 'Consultas com resposta completa desde a subida.';
COMMENT ON COLUMN registrobr_rdapnum_worker.truncated    IS 'Consultas com resposta truncada pelo servidor desde a subida.';
COMMENT ON COLUMN registrobr_rdapnum_worker.not_found    IS 'Consultas com 404 desde a subida.';
COMMENT ON COLUMN registrobr_rdapnum_worker.errors       IS 'Consultas com erro (rede, prazo, proxy, 429, 5xx, JSON inválido) desde a subida.';
COMMENT ON COLUMN registrobr_rdapnum_worker.refreshes    IS 'Pedidos de atualização da API atendidos (alvo marcado com rescan) desde a subida.';
COMMENT ON COLUMN registrobr_rdapnum_worker.egress_ip    IS 'Último IP de saída visto pelo servidor, lido do aviso da resposta. NULL = nenhum lido ainda.';
COMMENT ON COLUMN registrobr_rdapnum_worker.egress_ips   IS 'Quantidade de IPs de saída distintos nas últimas 100 respostas; mostra se o proxy está trocando de IP.';
COMMENT ON COLUMN registrobr_rdapnum_worker.paused_until IS 'Fim da pausa da instância depois de respostas truncadas seguidas. NULL ou no passado = sem pausa.';
COMMENT ON COLUMN registrobr_rdapnum_worker.created_at   IS 'Timestamp de criação do registro (subida da instância).';
COMMENT ON COLUMN registrobr_rdapnum_worker.updated_at   IS 'Último sinal da instância (a batida a cada 30 s); mantido pelo trigger trg_registrobr_rdapnum_worker_updated_at.';

CREATE TRIGGER trg_registrobr_rdapnum_worker_updated_at
    BEFORE UPDATE ON registrobr_rdapnum_worker
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

-- -----------------------------------------------------------------------------
-- Tabela: registrobr_rdapnum_target
--
-- Cadastro (kind, asn, prefix, source, active) e estado da coleta (scan_*,
-- last_*, failures, retry_at, response_uuid) na mesma linha: a fila é a
-- própria tabela. O administrador inclui (source = static), desativa, remove
-- e pede nova coleta (rescan); nunca troca asn/prefix de uma linha nem
-- escreve as colunas de estado da coleta. A FK de response_uuid é criada
-- depois de registrobr_rdapnum_response (as duas tabelas se apontam).
-- -----------------------------------------------------------------------------
CREATE TABLE registrobr_rdapnum_target (
    uuid               uuid        PRIMARY KEY DEFAULT uuidv7(),
    kind               text        NOT NULL,
    asn                bigint,
    prefix             cidr,
    source             text        NOT NULL DEFAULT 'static',
    active             boolean     NOT NULL DEFAULT true,
    rescan             boolean     NOT NULL DEFAULT false,

    created_at         timestamptz NOT NULL DEFAULT NOW(),
    updated_at         timestamptz NOT NULL DEFAULT NOW(),

    scan_worker        uuid,
    scan_started_at    timestamptz,
    last_scan_at       timestamptz,
    last_attempt_at    timestamptz,
    last_result        smallint,
    last_http_status   smallint,
    last_error         text,
    failures           smallint    NOT NULL DEFAULT 0,
    retry_at           timestamptz,
    response_uuid      uuid,

    CONSTRAINT chk_registrobr_rdapnum_target_kind   CHECK ((kind = 'asn' AND asn IS NOT NULL AND prefix IS NULL)
                                                        OR (kind = 'prefix' AND prefix IS NOT NULL AND asn IS NULL)),
    CONSTRAINT chk_registrobr_rdapnum_target_asn    CHECK (asn BETWEEN 0 AND 4294967295),
    CONSTRAINT chk_registrobr_rdapnum_target_source CHECK (source IN ('cgibr', 'static')),
    CONSTRAINT chk_registrobr_rdapnum_target_result CHECK (last_result BETWEEN 1 AND 4),
    CONSTRAINT uq_registrobr_rdapnum_target_asn     UNIQUE (asn),
    CONSTRAINT uq_registrobr_rdapnum_target_prefix  UNIQUE (prefix),

    CONSTRAINT fk_registrobr_rdapnum_target_worker  FOREIGN KEY (scan_worker)
        REFERENCES registrobr_rdapnum_worker(uuid) ON DELETE SET NULL
);

COMMENT ON TABLE registrobr_rdapnum_target IS
    'ASNs e blocos IP que o collector-registrobr-rdapnum consulta no RDAP do
     registro.br, com o estado da coleta. source = cgibr: derivados dos ASNs e
     blocos do cgibr pelo coletor; source = static: gravados pelo
     administrador direto no banco. Alvos só entram sozinhos, nunca saem
     sozinhos: desativar (active = false), remover (DELETE, que apaga em
     cascata as respostas e o dado processado do alvo) e pedir nova coleta
     (rescan = true) é do administrador. Blocos que se sobrepõem são
     permitidos: cada um é uma consulta.';
COMMENT ON COLUMN registrobr_rdapnum_target.uuid             IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree.';
COMMENT ON COLUMN registrobr_rdapnum_target.kind             IS 'Tipo do alvo (asn = consulta /autnum/<asn> | prefix = consulta /ip/<bloco>).';
COMMENT ON COLUMN registrobr_rdapnum_target.asn              IS 'O ASN (0 a 4294967295) quando kind = asn; NULL quando kind = prefix. Nunca troque o asn de uma linha: apague e insira.';
COMMENT ON COLUMN registrobr_rdapnum_target.prefix           IS 'O bloco consultado (IPv4 ou IPv6) quando kind = prefix; NULL quando kind = asn. O servidor pode devolver um bloco maior, que contém este. Nunca troque o prefix de uma linha: apague e insira.';
COMMENT ON COLUMN registrobr_rdapnum_target.source           IS 'Origem da linha (cgibr = derivada pelo coletor dos ASNs e blocos do cgibr | static = gravada pelo administrador). A derivação só insere; nunca altera uma linha existente.';
COMMENT ON COLUMN registrobr_rdapnum_target.active           IS 'false tira o alvo da fila e da atualização pedida pela API sem apagar nada (a api-registrobr-rdapnum continua servindo o dado).';
COMMENT ON COLUMN registrobr_rdapnum_target.rescan           IS 'true passa o alvo na frente da fila (respeitando retry_at); o coletor zera ao concluir a coleta. Gravado pelo administrador ou pelo coletor ao atender um pedido de atualização da API.';
COMMENT ON COLUMN registrobr_rdapnum_target.created_at       IS 'Timestamp de criação do registro (entrada do alvo).';
COMMENT ON COLUMN registrobr_rdapnum_target.updated_at       IS 'Timestamp da última mudança de cadastro (kind, asn, prefix, source, active); mantido pelo trigger trg_registrobr_rdapnum_target_updated_at, que não anda com o estado da coleta nem com rescan.';
COMMENT ON COLUMN registrobr_rdapnum_target.scan_worker      IS 'Instância que está consultando o alvo (FK → registrobr_rdapnum_worker.uuid, vira NULL quando a instância é apagada); NULL = livre.';
COMMENT ON COLUMN registrobr_rdapnum_target.scan_started_at  IS 'Quando scan_worker pegou o alvo; o lease vence em scan_started_at + SCAN_LEASE e outra instância pode retomá-lo.';
COMMENT ON COLUMN registrobr_rdapnum_target.last_scan_at     IS 'Última coleta conclusiva (resposta completa ou 404): a data do dado. NULL = nunca concluída.';
COMMENT ON COLUMN registrobr_rdapnum_target.last_attempt_at  IS 'Última tentativa de coleta, conclusiva ou não.';
COMMENT ON COLUMN registrobr_rdapnum_target.last_result      IS 'Resultado da última tentativa (1=complete | 2=truncated | 3=not_found | 4=error). NULL = nunca tentado.';
COMMENT ON COLUMN registrobr_rdapnum_target.last_http_status IS 'Status HTTP da última tentativa; NULL em erro de rede, de prazo ou do proxy.';
COMMENT ON COLUMN registrobr_rdapnum_target.last_error       IS 'Motivo do último erro (last_result = 4); NULL depois de uma coleta conclusiva.';
COMMENT ON COLUMN registrobr_rdapnum_target.failures         IS 'Respostas truncadas e erros seguidos desde a última coleta conclusiva; escolhe a espera (1 min, 5 min, 30 min, 2 h, depois 6 h).';
COMMENT ON COLUMN registrobr_rdapnum_target.retry_at         IS 'Antes disso o alvo não volta à fila (espera depois de truncada ou erro), nem com rescan. NULL = sem espera.';
COMMENT ON COLUMN registrobr_rdapnum_target.response_uuid    IS 'A resposta atual: a da última coleta conclusiva (FK → registrobr_rdapnum_response.uuid, vira NULL se ela for apagada). Uma resposta truncada nunca é a atual. NULL = nunca concluída.';

-- -----------------------------------------------------------------------------
-- Tabela: registrobr_rdapnum_response
--
-- Só respostas do servidor (completas, truncadas e 404); erros não têm linha.
-- Uma resposta nova vira linha nova só se mudou: o sha256 do corpo
-- normalizado (sem notices, chaves em ordem) é comparado com a última linha
-- do mesmo result do alvo; igual, só last_seen_at e seen_count andam.
-- Histórico inteiro, sem expurgo: só a cascata do alvo apaga linhas.
-- -----------------------------------------------------------------------------
CREATE TABLE registrobr_rdapnum_response (
    uuid               uuid        PRIMARY KEY DEFAULT uuidv7(),
    target_uuid        uuid        NOT NULL,
    result             smallint    NOT NULL,
    url                text        NOT NULL,
    http_status        smallint    NOT NULL,
    body               jsonb,
    sha256             text        NOT NULL,
    bytes              integer     NOT NULL,
    egress_ip          inet,
    server_time        timestamptz,
    worker             uuid        NOT NULL,

    fetched_at         timestamptz NOT NULL DEFAULT NOW(),
    last_seen_at       timestamptz NOT NULL DEFAULT NOW(),
    seen_count         integer     NOT NULL DEFAULT 1,

    parser_version     smallint,
    processed_at       timestamptz,
    process_error      text,

    CONSTRAINT chk_registrobr_rdapnum_response_result CHECK (result BETWEEN 1 AND 3),
    CONSTRAINT chk_registrobr_rdapnum_response_status CHECK ((result IN (1, 2) AND http_status = 200 AND body IS NOT NULL)
                                                          OR (result = 3 AND http_status = 404)),
    CONSTRAINT chk_registrobr_rdapnum_response_sha256 CHECK (sha256 ~ '^[0-9a-f]{64}$'),

    CONSTRAINT fk_registrobr_rdapnum_response_target  FOREIGN KEY (target_uuid)
        REFERENCES registrobr_rdapnum_target(uuid) ON DELETE CASCADE
);

COMMENT ON TABLE registrobr_rdapnum_response IS
    'Respostas cruas do RDAP do registro.br, uma linha por versão diferente da
     resposta de um alvo, gravadas (e confirmadas) antes de qualquer
     processamento. Só respostas do servidor: completas, truncadas e 404; erros
     (rede, prazo, 5xx, 429, JSON inválido) ficam em target.last_error. A
     truncada é guardada mas nunca processada. Histórico inteiro, sem expurgo:
     nenhuma linha é apagada pelo coletor, só a cascata quando o administrador
     apaga o alvo.';
COMMENT ON COLUMN registrobr_rdapnum_response.uuid           IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree. A api-registrobr-rdapnum a mostra no histórico e em /response/{uuid}.';
COMMENT ON COLUMN registrobr_rdapnum_response.target_uuid    IS 'O alvo consultado (FK → registrobr_rdapnum_target.uuid, ON DELETE CASCADE).';
COMMENT ON COLUMN registrobr_rdapnum_response.result         IS 'Tipo da resposta (1=complete | 2=truncated | 3=not_found). complete = 200 com JSON válido sem a marca de truncagem; truncated = 200 com remarks "object truncated due to server policy"; not_found = 404.';
COMMENT ON COLUMN registrobr_rdapnum_response.url            IS 'A URL consultada (ex.: https://rdap.registro.br/autnum/61610).';
COMMENT ON COLUMN registrobr_rdapnum_response.http_status    IS 'Status HTTP da resposta: 200 (complete, truncated) ou 404 (not_found).';
COMMENT ON COLUMN registrobr_rdapnum_response.body           IS 'O corpo JSON inteiro como veio, inclusive notices (com a hora e o IP de quem consultou, da primeira vez). NULL no 404 sem JSON.';
COMMENT ON COLUMN registrobr_rdapnum_response.sha256         IS 'SHA-256, em hex minúsculo, do corpo normalizado: sem o notices de nível superior e com as chaves dos objetos em ordem; no 404, do texto vazio. Decide se a resposta mudou.';
COMMENT ON COLUMN registrobr_rdapnum_response.bytes          IS 'Tamanho do corpo recebido, em bytes, na primeira vez.';
COMMENT ON COLUMN registrobr_rdapnum_response.egress_ip      IS 'IP de saída que o servidor viu, lido da segunda linha do aviso (notices), na primeira vez. NULL quando o aviso não traz a linha.';
COMMENT ON COLUMN registrobr_rdapnum_response.server_time    IS 'Hora do servidor lida do mesmo aviso, na primeira vez. NULL quando o aviso não traz a linha.';
COMMENT ON COLUMN registrobr_rdapnum_response.worker         IS 'Instância que recebeu a resposta da primeira vez (registrobr_rdapnum_worker.uuid, sem FK: as instâncias morrem, o histórico fica).';
COMMENT ON COLUMN registrobr_rdapnum_response.fetched_at     IS 'Primeira vez que esta versão da resposta chegou.';
COMMENT ON COLUMN registrobr_rdapnum_response.last_seen_at   IS 'Última vez que esta versão chegou igual (mesmo sha256 e result).';
COMMENT ON COLUMN registrobr_rdapnum_response.seen_count     IS 'Quantas vezes esta versão chegou igual.';
COMMENT ON COLUMN registrobr_rdapnum_response.parser_version IS 'Versão do parser (PARSER_VERSION do coletor) que processou a resposta; NULL = não processada (truncadas nunca são).';
COMMENT ON COLUMN registrobr_rdapnum_response.processed_at   IS 'Quando a resposta foi processada (ou recusada pelo processamento).';
COMMENT ON COLUMN registrobr_rdapnum_response.process_error  IS 'Por que o processamento recusou a resposta (ex.: faixa não é CIDR: <início>-<fim>); o dado processado anterior continua. NULL se deu certo ou não foi processada.';

-- -----------------------------------------------------------------------------
-- registrobr_rdapnum_target → registrobr_rdapnum_response (a resposta atual)
-- -----------------------------------------------------------------------------
ALTER TABLE registrobr_rdapnum_target
    ADD CONSTRAINT fk_registrobr_rdapnum_target_response FOREIGN KEY (response_uuid)
        REFERENCES registrobr_rdapnum_response(uuid) ON DELETE SET NULL;

-- -----------------------------------------------------------------------------
-- Tabela: registrobr_rdapnum_entity
--
-- Uma linha por handle (titular ou contato), compartilhada pelos ASNs e
-- blocos que a citam: cada resposta completa processada atualiza a linha
-- (o mais recente vence). A entidade sem nenhuma ligação sai na manutenção.
-- -----------------------------------------------------------------------------
CREATE TABLE registrobr_rdapnum_entity (
    uuid                 uuid        PRIMARY KEY DEFAULT uuidv7(),
    handle               text        NOT NULL,
    kind                 text,
    name                 text,
    email                text,
    country              text,
    document             text,
    document_type        text,
    document_digits      text        GENERATED ALWAYS AS (regexp_replace(document, '[^0-9]', '', 'g')) STORED,
    legal_representative text,
    registered_at        timestamptz,
    changed_at           timestamptz,
    response_uuid        uuid,

    created_at           timestamptz NOT NULL DEFAULT NOW(),
    updated_at           timestamptz NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_registrobr_rdapnum_entity_handle   UNIQUE (handle),

    CONSTRAINT fk_registrobr_rdapnum_entity_response FOREIGN KEY (response_uuid)
        REFERENCES registrobr_rdapnum_response(uuid) ON DELETE SET NULL
);

COMMENT ON TABLE registrobr_rdapnum_entity IS
    'Entidades (titulares e contatos) das respostas completas do RDAP do
     registro.br, uma linha por handle, compartilhada por todos os ASNs e
     blocos que a citam (registrobr_rdapnum_link). Cada resposta completa
     processada atualiza a linha com o que trouxe; como só completas são
     processadas, um e-mail nunca é apagado por truncagem. A entidade que fica
     sem nenhuma ligação sai na manutenção da derivação.';
COMMENT ON COLUMN registrobr_rdapnum_entity.uuid                 IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree.';
COMMENT ON COLUMN registrobr_rdapnum_entity.handle               IS 'Handle da entidade no registro.br: o CNPJ só com dígitos no titular (ex.: 35980592000130) ou o handle do contato (ex.: ROBEL308).';
COMMENT ON COLUMN registrobr_rdapnum_entity.kind                 IS 'kind do vCard (org | individual); NULL sem vCard.';
COMMENT ON COLUMN registrobr_rdapnum_entity.name                 IS 'fn do vCard: razão social do titular ou nome do contato.';
COMMENT ON COLUMN registrobr_rdapnum_entity.email                IS 'O primeiro email do vCard, em minúsculas e sem espaços nas pontas.';
COMMENT ON COLUMN registrobr_rdapnum_entity.country              IS 'País do adr do vCard (o 7º item do valor).';
COMMENT ON COLUMN registrobr_rdapnum_entity.document             IS 'publicIds[0].identifier como publicado (ex.: 35.980.592/0001-30).';
COMMENT ON COLUMN registrobr_rdapnum_entity.document_type        IS 'publicIds[0].type em minúsculas (cnpj, cpf ou o que vier).';
COMMENT ON COLUMN registrobr_rdapnum_entity.document_digits      IS 'document só com os dígitos (coluna gerada); serve a busca por documento.';
COMMENT ON COLUMN registrobr_rdapnum_entity.legal_representative IS 'legalRepresentative do titular.';
COMMENT ON COLUMN registrobr_rdapnum_entity.registered_at        IS 'events registration da entidade; NULL se ausente.';
COMMENT ON COLUMN registrobr_rdapnum_entity.changed_at           IS 'events last changed da entidade; NULL se ausente.';
COMMENT ON COLUMN registrobr_rdapnum_entity.response_uuid        IS 'Última resposta que atualizou a linha (FK → registrobr_rdapnum_response.uuid, ON DELETE SET NULL: a entidade é compartilhada e não cai com o alvo).';
COMMENT ON COLUMN registrobr_rdapnum_entity.created_at           IS 'Timestamp de criação do registro (primeira vez processada).';
COMMENT ON COLUMN registrobr_rdapnum_entity.updated_at           IS 'Timestamp da última mudança de algum valor (não anda com response_uuid), determina a versão do registro; mantido pelo trigger trg_registrobr_rdapnum_entity_updated_at.';

-- updated_at anda só quando um valor muda; response_uuid anda sempre.
CREATE TRIGGER trg_registrobr_rdapnum_entity_updated_at
    BEFORE UPDATE ON registrobr_rdapnum_entity
    FOR EACH ROW
    WHEN ((OLD.kind, OLD.name, OLD.email, OLD.country, OLD.document, OLD.document_type,
           OLD.legal_representative, OLD.registered_at, OLD.changed_at)
          IS DISTINCT FROM
          (NEW.kind, NEW.name, NEW.email, NEW.country, NEW.document, NEW.document_type,
           NEW.legal_representative, NEW.registered_at, NEW.changed_at))
    EXECUTE FUNCTION set_updated_at();

-- -----------------------------------------------------------------------------
-- registrobr_rdapnum_entity: busca por CNPJ/CPF (/registrobr/rdapnum/document)
-- -----------------------------------------------------------------------------
CREATE INDEX ix_registrobr_rdapnum_entity_document_digits ON registrobr_rdapnum_entity(document_digits);

-- -----------------------------------------------------------------------------
-- Tabela: registrobr_rdapnum_autnum
-- -----------------------------------------------------------------------------
CREATE TABLE registrobr_rdapnum_autnum (
    uuid               uuid        PRIMARY KEY DEFAULT uuidv7(),
    asn                bigint      NOT NULL,
    target_uuid        uuid        NOT NULL,
    response_uuid      uuid        NOT NULL,
    handle             text,
    name               text,
    type               text,
    country            text,
    registrant_uuid    uuid,
    registered_at      timestamptz,
    changed_at         timestamptz,

    created_at         timestamptz NOT NULL DEFAULT NOW(),
    updated_at         timestamptz NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_registrobr_rdapnum_autnum_asn        UNIQUE (asn),
    CONSTRAINT uq_registrobr_rdapnum_autnum_target     UNIQUE (target_uuid),
    CONSTRAINT chk_registrobr_rdapnum_autnum_asn       CHECK (asn BETWEEN 0 AND 4294967295),

    CONSTRAINT fk_registrobr_rdapnum_autnum_target     FOREIGN KEY (target_uuid)
        REFERENCES registrobr_rdapnum_target(uuid) ON DELETE CASCADE,
    CONSTRAINT fk_registrobr_rdapnum_autnum_response   FOREIGN KEY (response_uuid)
        REFERENCES registrobr_rdapnum_response(uuid),
    CONSTRAINT fk_registrobr_rdapnum_autnum_registrant FOREIGN KEY (registrant_uuid)
        REFERENCES registrobr_rdapnum_entity(uuid) ON DELETE SET NULL
);

COMMENT ON TABLE registrobr_rdapnum_autnum IS
    'ASNs processados das respostas completas de /autnum/<asn>: uma linha por
     ASN, espelho da resposta atual do alvo (registrobr_rdapnum_target.
     response_uuid). Um 404 apaga a linha (as ligações caem em cascata). Uma
     resposta truncada nunca toca esta tabela.';
COMMENT ON COLUMN registrobr_rdapnum_autnum.uuid            IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree.';
COMMENT ON COLUMN registrobr_rdapnum_autnum.asn             IS 'O ASN (startAutnum, igual a endAutnum no registro.br).';
COMMENT ON COLUMN registrobr_rdapnum_autnum.target_uuid     IS 'Alvo que gerou a linha (FK → registrobr_rdapnum_target.uuid, ON DELETE CASCADE).';
COMMENT ON COLUMN registrobr_rdapnum_autnum.response_uuid   IS 'Resposta processada que gerou a linha (FK → registrobr_rdapnum_response.uuid); anda a cada processamento.';
COMMENT ON COLUMN registrobr_rdapnum_autnum.handle          IS 'handle da resposta (ex.: 61610).';
COMMENT ON COLUMN registrobr_rdapnum_autnum.name            IS 'name da resposta (ex.: ASN61610).';
COMMENT ON COLUMN registrobr_rdapnum_autnum.type            IS 'type da resposta (ex.: DIRECT ALLOCATION).';
COMMENT ON COLUMN registrobr_rdapnum_autnum.country         IS 'country da resposta (ex.: BR).';
COMMENT ON COLUMN registrobr_rdapnum_autnum.registrant_uuid IS 'O titular (entidade com role registrant; FK → registrobr_rdapnum_entity.uuid, ON DELETE SET NULL).';
COMMENT ON COLUMN registrobr_rdapnum_autnum.registered_at   IS 'events registration do ASN; NULL se ausente.';
COMMENT ON COLUMN registrobr_rdapnum_autnum.changed_at      IS 'events last changed do ASN; NULL se ausente.';
COMMENT ON COLUMN registrobr_rdapnum_autnum.created_at      IS 'Timestamp de criação do registro (primeira vez processado).';
COMMENT ON COLUMN registrobr_rdapnum_autnum.updated_at      IS 'Timestamp da última mudança de algum valor (não anda com response_uuid), determina a versão do registro; mantido pelo trigger trg_registrobr_rdapnum_autnum_updated_at.';

-- updated_at anda só quando um valor muda; response_uuid anda sempre.
CREATE TRIGGER trg_registrobr_rdapnum_autnum_updated_at
    BEFORE UPDATE ON registrobr_rdapnum_autnum
    FOR EACH ROW
    WHEN ((OLD.asn, OLD.handle, OLD.name, OLD.type, OLD.country, OLD.registrant_uuid,
           OLD.registered_at, OLD.changed_at)
          IS DISTINCT FROM
          (NEW.asn, NEW.handle, NEW.name, NEW.type, NEW.country, NEW.registrant_uuid,
           NEW.registered_at, NEW.changed_at))
    EXECUTE FUNCTION set_updated_at();

-- -----------------------------------------------------------------------------
-- Tabela: registrobr_rdapnum_network
-- -----------------------------------------------------------------------------
CREATE TABLE registrobr_rdapnum_network (
    uuid               uuid        PRIMARY KEY DEFAULT uuidv7(),
    prefix             cidr        NOT NULL,
    family             smallint    NOT NULL GENERATED ALWAYS AS (family(prefix)) STORED,
    target_uuid        uuid        NOT NULL,
    response_uuid      uuid        NOT NULL,
    handle             text,
    name               text,
    type               text,
    country            text,
    parent_handle      text,
    status             text[]      NOT NULL DEFAULT '{}',
    asn                bigint,
    registrant_uuid    uuid,
    registered_at      timestamptz,
    changed_at         timestamptz,

    created_at         timestamptz NOT NULL DEFAULT NOW(),
    updated_at         timestamptz NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_registrobr_rdapnum_network_prefix     UNIQUE (prefix),

    CONSTRAINT fk_registrobr_rdapnum_network_target     FOREIGN KEY (target_uuid)
        REFERENCES registrobr_rdapnum_target(uuid) ON DELETE CASCADE,
    CONSTRAINT fk_registrobr_rdapnum_network_response   FOREIGN KEY (response_uuid)
        REFERENCES registrobr_rdapnum_response(uuid),
    CONSTRAINT fk_registrobr_rdapnum_network_registrant FOREIGN KEY (registrant_uuid)
        REFERENCES registrobr_rdapnum_entity(uuid) ON DELETE SET NULL
);

COMMENT ON TABLE registrobr_rdapnum_network IS
    'Blocos IP processados das respostas completas de /ip/<bloco>: uma linha
     por bloco registrado que o servidor devolveu (que pode ser maior que o
     consultado). Dois alvos que devolvem o mesmo bloco gravam a mesma linha;
     vale o último processado. Um 404 apaga as linhas do alvo (ligações e
     reversos caem em cascata). Uma resposta truncada nunca toca esta tabela.';
COMMENT ON COLUMN registrobr_rdapnum_network.uuid            IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree.';
COMMENT ON COLUMN registrobr_rdapnum_network.prefix          IS 'O bloco devolvido pelo servidor (o primeiro cidr0_cidrs; sem ele, o handle).';
COMMENT ON COLUMN registrobr_rdapnum_network.family          IS 'Família do bloco (4 | 6), gerada de prefix.';
COMMENT ON COLUMN registrobr_rdapnum_network.target_uuid     IS 'Alvo que gerou a linha por último (FK → registrobr_rdapnum_target.uuid, ON DELETE CASCADE).';
COMMENT ON COLUMN registrobr_rdapnum_network.response_uuid   IS 'Resposta processada que gerou a linha (FK → registrobr_rdapnum_response.uuid); anda a cada processamento.';
COMMENT ON COLUMN registrobr_rdapnum_network.handle          IS 'handle da resposta (ex.: 200.225.48.0/21).';
COMMENT ON COLUMN registrobr_rdapnum_network.name            IS 'name da resposta (no registro.br, um número interno, ex.: 462539).';
COMMENT ON COLUMN registrobr_rdapnum_network.type            IS 'type da resposta (ex.: ALLOCATED).';
COMMENT ON COLUMN registrobr_rdapnum_network.country         IS 'country da resposta (ex.: BR).';
COMMENT ON COLUMN registrobr_rdapnum_network.parent_handle   IS 'parentHandle da resposta, quando há.';
COMMENT ON COLUMN registrobr_rdapnum_network.status          IS 'status da resposta (ex.: {active}); vazio quando ausente.';
COMMENT ON COLUMN registrobr_rdapnum_network.asn             IS 'nicbr_autnum: o ASN do bloco no registro.br; NULL quando ausente.';
COMMENT ON COLUMN registrobr_rdapnum_network.registrant_uuid IS 'O titular (entidade com role registrant; FK → registrobr_rdapnum_entity.uuid, ON DELETE SET NULL).';
COMMENT ON COLUMN registrobr_rdapnum_network.registered_at   IS 'events registration do bloco; NULL se ausente.';
COMMENT ON COLUMN registrobr_rdapnum_network.changed_at      IS 'events last changed do bloco; NULL se ausente.';
COMMENT ON COLUMN registrobr_rdapnum_network.created_at      IS 'Timestamp de criação do registro (primeira vez processado).';
COMMENT ON COLUMN registrobr_rdapnum_network.updated_at      IS 'Timestamp da última mudança de algum valor (não anda com response_uuid nem target_uuid), determina a versão do registro; mantido pelo trigger trg_registrobr_rdapnum_network_updated_at.';

-- updated_at anda só quando um valor muda; response_uuid e target_uuid
-- andam sempre.
CREATE TRIGGER trg_registrobr_rdapnum_network_updated_at
    BEFORE UPDATE ON registrobr_rdapnum_network
    FOR EACH ROW
    WHEN ((OLD.prefix, OLD.handle, OLD.name, OLD.type, OLD.country, OLD.parent_handle,
           OLD.status, OLD.asn, OLD.registrant_uuid, OLD.registered_at, OLD.changed_at)
          IS DISTINCT FROM
          (NEW.prefix, NEW.handle, NEW.name, NEW.type, NEW.country, NEW.parent_handle,
           NEW.status, NEW.asn, NEW.registrant_uuid, NEW.registered_at, NEW.changed_at))
    EXECUTE FUNCTION set_updated_at();

-- -----------------------------------------------------------------------------
-- registrobr_rdapnum_network: o bloco que contém um IP ou prefixo (>>=), os
-- blocos de um ASN e os blocos de um alvo
-- -----------------------------------------------------------------------------
CREATE INDEX ix_registrobr_rdapnum_network_prefix_gist ON registrobr_rdapnum_network USING gist (prefix inet_ops);
CREATE INDEX ix_registrobr_rdapnum_network_asn         ON registrobr_rdapnum_network(asn);
CREATE INDEX ix_registrobr_rdapnum_network_target      ON registrobr_rdapnum_network(target_uuid);

-- -----------------------------------------------------------------------------
-- Tabela: registrobr_rdapnum_link
-- -----------------------------------------------------------------------------
CREATE TABLE registrobr_rdapnum_link (
    uuid               uuid        PRIMARY KEY DEFAULT uuidv7(),
    entity_uuid        uuid        NOT NULL,
    autnum_uuid        uuid,
    network_uuid       uuid,
    role               text        NOT NULL,
    via_uuid           uuid,

    CONSTRAINT chk_registrobr_rdapnum_link_object CHECK (num_nonnulls(autnum_uuid, network_uuid) = 1),
    CONSTRAINT uq_registrobr_rdapnum_link         UNIQUE NULLS NOT DISTINCT (entity_uuid, autnum_uuid, network_uuid, role, via_uuid),

    CONSTRAINT fk_registrobr_rdapnum_link_entity  FOREIGN KEY (entity_uuid)
        REFERENCES registrobr_rdapnum_entity(uuid) ON DELETE CASCADE,
    CONSTRAINT fk_registrobr_rdapnum_link_autnum  FOREIGN KEY (autnum_uuid)
        REFERENCES registrobr_rdapnum_autnum(uuid) ON DELETE CASCADE,
    CONSTRAINT fk_registrobr_rdapnum_link_network FOREIGN KEY (network_uuid)
        REFERENCES registrobr_rdapnum_network(uuid) ON DELETE CASCADE,
    CONSTRAINT fk_registrobr_rdapnum_link_via     FOREIGN KEY (via_uuid)
        REFERENCES registrobr_rdapnum_entity(uuid) ON DELETE CASCADE
);

COMMENT ON TABLE registrobr_rdapnum_link IS
    'Papel de uma entidade num ASN ou num bloco: uma linha por papel
     (registrant, administrative, technical, abuse...). O processamento apaga
     e regrava as ligações do objeto a cada resposta completa; por isso não há
     updated_at.';
COMMENT ON COLUMN registrobr_rdapnum_link.uuid         IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree.';
COMMENT ON COLUMN registrobr_rdapnum_link.entity_uuid  IS 'A entidade (FK → registrobr_rdapnum_entity.uuid, ON DELETE CASCADE).';
COMMENT ON COLUMN registrobr_rdapnum_link.autnum_uuid  IS 'O ASN (FK → registrobr_rdapnum_autnum.uuid, ON DELETE CASCADE); NULL quando a ligação é de um bloco.';
COMMENT ON COLUMN registrobr_rdapnum_link.network_uuid IS 'O bloco (FK → registrobr_rdapnum_network.uuid, ON DELETE CASCADE); NULL quando a ligação é de um ASN.';
COMMENT ON COLUMN registrobr_rdapnum_link.role         IS 'Um papel da entidade no objeto, como veio em roles (registrant, administrative, technical, abuse...).';
COMMENT ON COLUMN registrobr_rdapnum_link.via_uuid     IS 'A entidade dentro da qual o contato veio (o titular), FK → registrobr_rdapnum_entity.uuid, ON DELETE CASCADE; NULL = contato do próprio objeto.';

-- -----------------------------------------------------------------------------
-- registrobr_rdapnum_link: as ligações de uma entidade, de um ASN e de um bloco
-- -----------------------------------------------------------------------------
CREATE INDEX ix_registrobr_rdapnum_link_entity  ON registrobr_rdapnum_link(entity_uuid);
CREATE INDEX ix_registrobr_rdapnum_link_autnum  ON registrobr_rdapnum_link(autnum_uuid) WHERE autnum_uuid IS NOT NULL;
CREATE INDEX ix_registrobr_rdapnum_link_network ON registrobr_rdapnum_link(network_uuid) WHERE network_uuid IS NOT NULL;

-- -----------------------------------------------------------------------------
-- Tabela: registrobr_rdapnum_reverse
-- -----------------------------------------------------------------------------
CREATE TABLE registrobr_rdapnum_reverse (
    uuid               uuid        PRIMARY KEY DEFAULT uuidv7(),
    network_uuid       uuid        NOT NULL,
    start_address      inet        NOT NULL,
    end_address        inet        NOT NULL,
    prefix             cidr,
    nameservers        jsonb       NOT NULL DEFAULT '[]'::jsonb,
    signed             boolean,
    registered_at      timestamptz,
    changed_at         timestamptz,

    CONSTRAINT fk_registrobr_rdapnum_reverse_network FOREIGN KEY (network_uuid)
        REFERENCES registrobr_rdapnum_network(uuid) ON DELETE CASCADE
);

COMMENT ON TABLE registrobr_rdapnum_reverse IS
    'Delegações reversas de um bloco (nicbr_reverseDelegations), uma linha por
     faixa delegada. Apagadas e regravadas com o bloco a cada resposta completa
     processada; por isso não há updated_at.';
COMMENT ON COLUMN registrobr_rdapnum_reverse.uuid          IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree.';
COMMENT ON COLUMN registrobr_rdapnum_reverse.network_uuid  IS 'O bloco (FK → registrobr_rdapnum_network.uuid, ON DELETE CASCADE).';
COMMENT ON COLUMN registrobr_rdapnum_reverse.start_address IS 'Primeiro endereço da faixa delegada (startAddress).';
COMMENT ON COLUMN registrobr_rdapnum_reverse.end_address   IS 'Último endereço da faixa delegada (endAddress).';
COMMENT ON COLUMN registrobr_rdapnum_reverse.prefix        IS 'A faixa como CIDR, quando start_address–end_address é um CIDR exato; senão NULL.';
COMMENT ON COLUMN registrobr_rdapnum_reverse.nameservers   IS 'Lista JSON dos nameservers, em ordem de nome: [{"name":"...","status":["ns aa"],"checked_at":"...","last_ok_at":"..."}] (status e checked_at do evento delegation check, last_ok_at do last correct delegation check).';
COMMENT ON COLUMN registrobr_rdapnum_reverse.signed        IS 'secureDNS.delegationSigned; NULL quando ausente.';
COMMENT ON COLUMN registrobr_rdapnum_reverse.registered_at IS 'events registration da delegação; NULL se ausente.';
COMMENT ON COLUMN registrobr_rdapnum_reverse.changed_at    IS 'events last changed da delegação; NULL se ausente.';

CREATE INDEX ix_registrobr_rdapnum_reverse_network ON registrobr_rdapnum_reverse(network_uuid);

-- -----------------------------------------------------------------------------
-- registrobr_rdapnum_target: triggers e índices (cadastro, fila, instância)
-- -----------------------------------------------------------------------------
CREATE TRIGGER trg_registrobr_rdapnum_target_updated_at
    BEFORE UPDATE OF kind, asn, prefix, source, active ON registrobr_rdapnum_target
    FOR EACH ROW
    WHEN ((OLD.kind, OLD.asn, OLD.prefix, OLD.source, OLD.active) IS DISTINCT FROM (NEW.kind, NEW.asn, NEW.prefix, NEW.source, NEW.active))
    EXECUTE FUNCTION set_updated_at();

-- A fila na ordem dela: rescan → nunca concluído → last_scan_at mais antigo.
CREATE INDEX ix_registrobr_rdapnum_target_queue  ON registrobr_rdapnum_target(rescan DESC, last_scan_at ASC NULLS FIRST) WHERE active;
CREATE INDEX ix_registrobr_rdapnum_target_worker ON registrobr_rdapnum_target(scan_worker) WHERE scan_worker IS NOT NULL;

-- -----------------------------------------------------------------------------
-- registrobr_rdapnum_response: a última versão de cada tipo de um alvo (a
-- comparação) e o histórico; o que falta processar
-- -----------------------------------------------------------------------------
CREATE INDEX ix_registrobr_rdapnum_response_target  ON registrobr_rdapnum_response(target_uuid, result, fetched_at DESC);
CREATE INDEX ix_registrobr_rdapnum_response_pending ON registrobr_rdapnum_response(fetched_at)
    WHERE result IN (1, 3) AND processed_at IS NULL AND process_error IS NULL;

-- migrate:down

DROP TABLE registrobr_rdapnum_reverse;
DROP TABLE registrobr_rdapnum_link;
DROP TABLE registrobr_rdapnum_network;
DROP TABLE registrobr_rdapnum_autnum;
DROP TABLE registrobr_rdapnum_entity;
ALTER TABLE registrobr_rdapnum_target DROP CONSTRAINT fk_registrobr_rdapnum_target_response;
DROP TABLE registrobr_rdapnum_response;
DROP TABLE registrobr_rdapnum_target;
DROP TABLE registrobr_rdapnum_worker;
DROP TABLE registrobr_rdapnum_config;
DROP TABLE registrobr_rdapnum_run;
