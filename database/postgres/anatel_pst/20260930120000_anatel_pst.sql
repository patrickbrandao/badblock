-- migrate:up

-- =============================================================================
-- anatel_pst: prestadoras de serviços de telecomunicações publicadas pela Anatel
-- Depende de: central (função set_updated_at); extensão pg_trgm (contrib)
-- Tabelas: anatel_pst_run (tabela independente), anatel_pst_provider,
--          anatel_pst_service
--
-- Escrita pelo collector-anatel-pst, lida pela api-anatel-pst. Fonte (um ZIP
-- com um CSV separado por ';', uma linha por serviço notificado):
--   https://www.anatel.gov.br/dadosabertos/paineis_de_dados/outorga_e_licenciamento/prestadoras_servicos_telecomunicacoes.zip
--
-- Nota: só pessoas jurídicas (CNPJ) são guardadas; as linhas de pessoa física
-- (CPF, mascarado pela Anatel) são ignoradas pelo coletor. As tabelas espelham
-- o último arquivo aplicado: o que some da fonte é apagado aqui (sem soft
-- delete); histórico e consolidação ficam para a fase 2. A aplicação acontece
-- numa transação só, então a api-anatel-pst nunca vê um arquivo pela metade.
--
-- Nota: pg_trgm é criada com IF NOT EXISTS e o migrate:down NÃO a remove: é
-- do banco inteiro e outras pastas (ex.: asnames) também a usam.
-- =============================================================================

CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- -----------------------------------------------------------------------------
-- Tabela: anatel_pst_run
-- -----------------------------------------------------------------------------
CREATE TABLE anatel_pst_run (
    uuid                 uuid        PRIMARY KEY DEFAULT uuidv7(),
    status               smallint    NOT NULL,
    forced               boolean     NOT NULL DEFAULT false,

    url                  text        NOT NULL,
    http_status          smallint,
    etag                 text,
    last_modified        text,
    sha256               text,
    bytes                bigint,

    csv_name             text,
    csv_sha256           text,
    csv_bytes            bigint,
    csv_modified_at      timestamptz,

    rows                 integer,
    rows_cnpj            integer,
    rows_cpf             integer,
    duplicates           integer,
    skipped              integer,
    providers            integer,
    services             integer,

    provider_inserted    integer,
    provider_updated     integer,
    provider_deleted     integer,
    service_inserted     integer,
    service_updated      integer,
    service_deleted      integer,

    warnings             jsonb       NOT NULL DEFAULT '[]'::jsonb,
    error                text,

    started_at           timestamptz NOT NULL,
    created_at           timestamptz NOT NULL DEFAULT NOW(),

    CONSTRAINT chk_anatel_pst_run_status     CHECK (status BETWEEN 0 AND 1),
    CONSTRAINT chk_anatel_pst_run_sha256     CHECK (sha256     ~ '^[0-9a-f]{64}$'),
    CONSTRAINT chk_anatel_pst_run_csv_sha256 CHECK (csv_sha256 ~ '^[0-9a-f]{64}$')
);

COMMENT ON TABLE anatel_pst_run IS
    'Histórico das execuções do collector-anatel-pst que baixaram um arquivo novo
     da fonte (aplicado ou recusado). Verificações sem mudança não geram linha:
     ficam só em jobs.last_check_at. A última linha com status = 1 descreve o
     dataset que está em anatel_pst_provider/anatel_pst_service, e seu uuid é a
     versão do dataset usada pela api-anatel-pst no cache e no ETag.';
COMMENT ON COLUMN anatel_pst_run.uuid              IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree. Nas linhas com status = 1 é também a versão do dataset.';
COMMENT ON COLUMN anatel_pst_run.status            IS 'Resultado (0=recusado | 1=aplicado). Recusado: ZIP, parser, mínimo ou trava de remoção recusaram o arquivo, e as tabelas ficaram como estavam.';
COMMENT ON COLUMN anatel_pst_run.forced            IS 'true quando a execução foi forçada (--force): ignora as checagens de mudança, o CSV mais antigo e a trava de remoção em massa.';
COMMENT ON COLUMN anatel_pst_run.url               IS 'URL de onde o ZIP foi baixado.';
COMMENT ON COLUMN anatel_pst_run.http_status       IS 'Status HTTP da resposta do ZIP. NULL = falhou antes de receber resposta.';
COMMENT ON COLUMN anatel_pst_run.etag              IS 'Cabeçalho ETag da resposta; reenviado em If-None-Match na próxima verificação.';
COMMENT ON COLUMN anatel_pst_run.last_modified     IS 'Cabeçalho Last-Modified da resposta, como texto HTTP; reenviado em If-Modified-Since.';
COMMENT ON COLUMN anatel_pst_run.sha256            IS 'SHA-256 (hex minúsculo) do ZIP baixado. Igual ao da última execução aplicada = sem mudança. A fonte não publica hash.';
COMMENT ON COLUMN anatel_pst_run.bytes             IS 'Tamanho do ZIP baixado, em bytes.';
COMMENT ON COLUMN anatel_pst_run.csv_name          IS 'Nome da entrada CSV dentro do ZIP.';
COMMENT ON COLUMN anatel_pst_run.csv_sha256        IS 'SHA-256 (hex minúsculo) do CSV extraído. Igual ao da última execução aplicada = sem mudança (o ZIP pode ser regerado com o mesmo CSV).';
COMMENT ON COLUMN anatel_pst_run.csv_bytes         IS 'Tamanho do CSV extraído, em bytes.';
COMMENT ON COLUMN anatel_pst_run.csv_modified_at   IS 'Data de modificação da entrada CSV no ZIP (campo MS-DOS, sem fuso), lida como hora de Brasília (UTC-3). Um CSV mais antigo que o aplicado não é aplicado.';
COMMENT ON COLUMN anatel_pst_run.rows              IS 'Linhas de dados do CSV, sem o cabeçalho.';
COMMENT ON COLUMN anatel_pst_run.rows_cnpj         IS 'Linhas de pessoa jurídica (CNPJ), inclusive cópias e descartadas.';
COMMENT ON COLUMN anatel_pst_run.rows_cpf          IS 'Linhas de pessoa física (CPF), ignoradas: o BadBlock não guarda pessoas físicas.';
COMMENT ON COLUMN anatel_pst_run.duplicates        IS 'Linhas de CNPJ ignoradas por serem cópia exata de uma linha anterior.';
COMMENT ON COLUMN anatel_pst_run.skipped           IS 'Linhas de CNPJ descartadas pelo parser (formato inválido); no máximo 1% das linhas de CNPJ.';
COMMENT ON COLUMN anatel_pst_run.providers         IS 'Prestadoras (CNPJs distintos) aceitas.';
COMMENT ON COLUMN anatel_pst_run.services          IS 'Serviços notificados aceitos.';
COMMENT ON COLUMN anatel_pst_run.provider_inserted IS 'Prestadoras novas gravadas em anatel_pst_provider. NULL quando status = 0.';
COMMENT ON COLUMN anatel_pst_run.provider_updated  IS 'Prestadoras com algum dado cadastral alterado. NULL quando status = 0.';
COMMENT ON COLUMN anatel_pst_run.provider_deleted  IS 'Prestadoras que sumiram da fonte e foram apagadas (com os serviços, em cascata). NULL quando status = 0.';
COMMENT ON COLUMN anatel_pst_run.service_inserted  IS 'Serviços novos gravados em anatel_pst_service. NULL quando status = 0.';
COMMENT ON COLUMN anatel_pst_run.service_updated   IS 'Serviços com algum dado alterado. NULL quando status = 0.';
COMMENT ON COLUMN anatel_pst_run.service_deleted   IS 'Serviços que sumiram da fonte e foram apagados, inclusive os apagados em cascata com a prestadora. NULL quando status = 0.';
COMMENT ON COLUMN anatel_pst_run.warnings          IS 'Lista JSON (array de strings) com os avisos do parser: até 50 e, se houver mais, um item "... e mais N avisos".';
COMMENT ON COLUMN anatel_pst_run.error             IS 'Mensagem da recusa quando status = 0. NULL quando aplicado.';
COMMENT ON COLUMN anatel_pst_run.started_at        IS 'Início da verificação (antes do download).';
COMMENT ON COLUMN anatel_pst_run.created_at        IS 'Timestamp de criação do registro, gravado no fim da execução.';

-- -----------------------------------------------------------------------------
-- anatel_pst_run: última execução aplicada (versão do dataset), para o coletor
-- e a API
-- -----------------------------------------------------------------------------
CREATE INDEX ix_anatel_pst_run_applied ON anatel_pst_run (created_at DESC) WHERE status = 1;

-- -----------------------------------------------------------------------------
-- Tabela: anatel_pst_provider
-- -----------------------------------------------------------------------------
CREATE TABLE anatel_pst_provider (
    uuid                 uuid        PRIMARY KEY DEFAULT uuidv7(),
    document             text        NOT NULL,
    name                 text        NOT NULL,
    trade_name           text,

    street               text,
    number               text,
    complement           text,
    district             text,
    postal_code          text,
    city_ibge_code       integer,
    city                 text,
    state                text,
    phone                text,
    email                text,

    created_at           timestamptz NOT NULL DEFAULT NOW(),
    updated_at           timestamptz NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_anatel_pst_provider_document       UNIQUE (document),
    CONSTRAINT chk_anatel_pst_provider_document      CHECK (document ~ '^[0-9]{14}$'),
    CONSTRAINT chk_anatel_pst_provider_state         CHECK (state    ~ '^[A-Z]{2}$'),
    CONSTRAINT chk_anatel_pst_provider_city_ibge_code CHECK (city_ibge_code BETWEEN 1000000 AND 9999999)
);

COMMENT ON TABLE anatel_pst_provider IS
    'Prestadoras de serviços de telecomunicações (pessoas jurídicas) do cadastro
     da Anatel: uma linha por CNPJ, com a razão social, o nome fantasia e o
     endereço da sede. Os serviços notificados por cada uma estão em
     anatel_pst_service. Valores ausentes na fonte (N/I, N/A, -, vazio) são NULL.
     Prestadora que some da fonte é apagada com os seus serviços (cascata).';
COMMENT ON COLUMN anatel_pst_provider.uuid           IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree.';
COMMENT ON COLUMN anatel_pst_provider.document       IS 'CNPJ da prestadora, 14 dígitos sem pontuação; chave natural. Os dígitos verificadores não são conferidos (vale o cadastro da Anatel).';
COMMENT ON COLUMN anatel_pst_provider.name           IS 'Razão social (coluna "Nome Entidade Prestadora de Serviço").';
COMMENT ON COLUMN anatel_pst_provider.trade_name     IS 'Nome fantasia. NULL = não informado na fonte (N/I).';
COMMENT ON COLUMN anatel_pst_provider.street         IS 'Logradouro do endereço da sede.';
COMMENT ON COLUMN anatel_pst_provider.number         IS 'Número do endereço da sede, como texto publicado (ex.: S/N, 1740A).';
COMMENT ON COLUMN anatel_pst_provider.complement     IS 'Complemento do endereço da sede.';
COMMENT ON COLUMN anatel_pst_provider.district       IS 'Bairro do endereço da sede.';
COMMENT ON COLUMN anatel_pst_provider.postal_code    IS 'CEP da sede como publicado, só dígitos (alguns com 7 dígitos, sem o zero à esquerda).';
COMMENT ON COLUMN anatel_pst_provider.city_ibge_code IS 'Código IBGE (7 dígitos) do município da sede. NULL quando a fonte traz valor inválido.';
COMMENT ON COLUMN anatel_pst_provider.city           IS 'Nome do município da sede.';
COMMENT ON COLUMN anatel_pst_provider.state          IS 'UF da sede (sigla de 2 letras).';
COMMENT ON COLUMN anatel_pst_provider.phone          IS 'Telefone principal, texto livre como publicado.';
COMMENT ON COLUMN anatel_pst_provider.email          IS 'Endereço eletrônico (e-mail) como publicado.';
COMMENT ON COLUMN anatel_pst_provider.created_at     IS 'Timestamp de criação do registro: primeira vez que o CNPJ apareceu na fonte (desde a primeira carga deste banco).';
COMMENT ON COLUMN anatel_pst_provider.updated_at     IS 'Timestamp de última alteração, mantido automaticamente pelo trigger trg_anatel_pst_provider_updated_at.';

CREATE TRIGGER trg_anatel_pst_provider_updated_at
    BEFORE UPDATE ON anatel_pst_provider
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

-- -----------------------------------------------------------------------------
-- anatel_pst_provider: busca por parte da razão social ou do nome fantasia
-- (/anatel/pst/search: name ILIKE '%...%' OR trade_name ILIKE '%...%')
-- -----------------------------------------------------------------------------
CREATE INDEX ix_anatel_pst_provider_name_trgm       ON anatel_pst_provider USING gin (name       gin_trgm_ops);
CREATE INDEX ix_anatel_pst_provider_trade_name_trgm ON anatel_pst_provider USING gin (trade_name gin_trgm_ops);

-- -----------------------------------------------------------------------------
-- Tabela: anatel_pst_service
-- -----------------------------------------------------------------------------
CREATE TABLE anatel_pst_service (
    uuid                 uuid        PRIMARY KEY DEFAULT uuidv7(),
    provider_uuid        uuid        NOT NULL,

    entity_type          text        NOT NULL,
    grant_type           text        NOT NULL,
    grant_fistel         text,
    grant_process        text,
    granted_on           date,

    service_group        text        NOT NULL,
    service_code         text        NOT NULL,
    service_name         text        NOT NULL,
    notification_fistel  text        NOT NULL,
    notification_process text,
    notified_on          date,

    created_at           timestamptz NOT NULL DEFAULT NOW(),
    updated_at           timestamptz NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_anatel_pst_service_key
        UNIQUE NULLS NOT DISTINCT (provider_uuid, grant_fistel, notification_fistel, service_code),
    CONSTRAINT fk_anatel_pst_service_provider FOREIGN KEY (provider_uuid)
        REFERENCES anatel_pst_provider(uuid) ON DELETE CASCADE,
    CONSTRAINT chk_anatel_pst_service_service_code        CHECK (service_code        ~ '^[0-9]{3}$'),
    CONSTRAINT chk_anatel_pst_service_notification_fistel CHECK (notification_fistel ~ '^[0-9]{11}$'),
    CONSTRAINT chk_anatel_pst_service_grant_fistel        CHECK (grant_fistel        ~ '^[0-9]{11}$')
);

COMMENT ON TABLE anatel_pst_service IS
    'Serviços de telecomunicações notificados à Anatel por cada prestadora: uma
     linha por linha única de CNPJ do CSV, com a outorga (tipo, Fistel, processo,
     data) e o serviço (grupo, código de 3 dígitos, nome, Fistel da notificação,
     processo, data). Ex.: código 045 = SCM (Serviço de Comunicação Multimídia),
     a licença de provedor de internet. Chave natural: (provider_uuid,
     grant_fistel, notification_fistel, service_code). Apagada em cascata com a
     prestadora.';
COMMENT ON COLUMN anatel_pst_service.uuid                 IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree.';
COMMENT ON COLUMN anatel_pst_service.provider_uuid        IS 'Prestadora que notificou o serviço (FK → anatel_pst_provider.uuid), removido em cascata.';
COMMENT ON COLUMN anatel_pst_service.entity_type          IS 'Tipo de entidade como publicado: Outorgada ou Dispensada de Outorga.';
COMMENT ON COLUMN anatel_pst_service.grant_type           IS 'Tipo de outorga como publicado: Serviços de Interesse Coletivo e Restrito - SIC, Serviços de Interesse Restrito - SIR ou Dispensada de Outorga.';
COMMENT ON COLUMN anatel_pst_service.grant_fistel         IS 'Número Fistel (11 dígitos) da outorga. NULL = entidade dispensada de outorga (N/A na fonte).';
COMMENT ON COLUMN anatel_pst_service.grant_process        IS 'Número do processo SEI da outorga. NULL = dispensada ou não informado.';
COMMENT ON COLUMN anatel_pst_service.granted_on           IS 'Data de inclusão da outorga. NULL = dispensada.';
COMMENT ON COLUMN anatel_pst_service.service_group        IS 'Grupo do serviço como publicado (coluna "Serviço da Notificação"), ex.: Banda Larga Fixa, Telefonia Fixa.';
COMMENT ON COLUMN anatel_pst_service.service_code         IS 'Código do serviço na Anatel, 3 dígitos com zeros à esquerda (ex.: 045 = SCM, 171 = STFC, 010 = SMP, 750 = SeAC).';
COMMENT ON COLUMN anatel_pst_service.service_name         IS 'Nome do serviço, o texto depois de "NNN - " na fonte (ex.: Serviço de Comunicação Multimídia).';
COMMENT ON COLUMN anatel_pst_service.notification_fistel  IS 'Número Fistel (11 dígitos) da notificação do serviço.';
COMMENT ON COLUMN anatel_pst_service.notification_process IS 'Número do processo SEI da notificação, como publicado. NULL = não informado (N/I).';
COMMENT ON COLUMN anatel_pst_service.notified_on          IS 'Data de inclusão da notificação do serviço.';
COMMENT ON COLUMN anatel_pst_service.created_at           IS 'Timestamp de criação do registro: primeira vez que o serviço apareceu na fonte (desde a primeira carga deste banco).';
COMMENT ON COLUMN anatel_pst_service.updated_at           IS 'Timestamp de última alteração, mantido automaticamente pelo trigger trg_anatel_pst_service_updated_at.';

CREATE TRIGGER trg_anatel_pst_service_updated_at
    BEFORE UPDATE ON anatel_pst_service
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

-- -----------------------------------------------------------------------------
-- anatel_pst_service: catálogo de serviços e prestadoras de um serviço
-- (/anatel/pst/services, /anatel/pst/service/{code}). Os serviços de uma
-- prestadora usam o índice de uq_anatel_pst_service_key (provider_uuid é a
-- primeira coluna), que serve também a cascata.
-- -----------------------------------------------------------------------------
CREATE INDEX ix_anatel_pst_service_service_code ON anatel_pst_service (service_code, provider_uuid);

-- migrate:down

-- A extensão pg_trgm fica: é compartilhada pelo banco (ver cabeçalho).
DROP TABLE anatel_pst_service;
DROP TABLE anatel_pst_provider;
DROP TABLE anatel_pst_run;
