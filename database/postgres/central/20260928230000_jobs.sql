-- migrate:up

-- =============================================================================
-- Central: função compartilhada de updated_at e tabela jobs
-- Depende de: (nada) — é a primeira pasta aplicada pelo migrate.sh
-- Tabelas: jobs (tabela independente)
--
-- Nota: jobs é o ponto de encontro entre os collectors (fase 1) e a
-- consolidação central (fase 2). Cada collector mantém uma linha com o próprio
-- nome; a consolidação procura as linhas com consolidated = 0, copia os dados
-- das tabelas <fonte>_* para as tabelas centrais e marca consolidated = 1.
-- =============================================================================

-- -----------------------------------------------------------------------------
-- Função: set_updated_at (usada pelos triggers trg_<tabela>_updated_at)
-- -----------------------------------------------------------------------------
CREATE FUNCTION set_updated_at() RETURNS trigger
    LANGUAGE plpgsql AS
$$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$;

COMMENT ON FUNCTION set_updated_at() IS
    'Função de trigger compartilhada: grava NOW() em updated_at a cada UPDATE.
     Toda tabela com updated_at tem um trigger trg_<tabela>_updated_at que a chama.';

-- -----------------------------------------------------------------------------
-- Tabela: jobs
-- -----------------------------------------------------------------------------
CREATE TABLE jobs (
    uuid             uuid        PRIMARY KEY DEFAULT uuidv7(),
    app              text        NOT NULL,
    last_sync_at     timestamptz,
    last_check_at    timestamptz,
    consolidated     smallint    NOT NULL DEFAULT 0,

    created_at       timestamptz NOT NULL DEFAULT NOW(),
    updated_at       timestamptz NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_jobs_app           UNIQUE (app),
    CONSTRAINT chk_jobs_app          CHECK (app ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
    CONSTRAINT chk_jobs_consolidated CHECK (consolidated BETWEEN 0 AND 1)
);

COMMENT ON TABLE jobs IS
    'Estado de sincronização de cada app coletor (collector-*). Uma linha por app,
     criada pelo próprio collector na primeira verificação da fonte. É o contrato
     entre os collectors e a consolidação central (fase 2): o collector zera
     consolidated sempre que suas tabelas mudam; a consolidação copia os dados
     para as tabelas centrais e volta a flag para 1.';
COMMENT ON COLUMN jobs.uuid          IS 'Chave primária usando UUIDv7 para ordenação temporal e performance de B-Tree.';
COMMENT ON COLUMN jobs.app           IS 'Nome do app coletor, collector-<fonte> (ex.: collector-cgibr, cujo código fica em apps/cgibr/collector/). Minúsculas, dígitos e hífens.';
COMMENT ON COLUMN jobs.last_sync_at  IS 'Momento da última sincronização realizada, isto é, da última vez que um arquivo novo da fonte foi aplicado às tabelas do app. NULL = nenhuma sincronização concluída ainda.';
COMMENT ON COLUMN jobs.last_check_at IS 'Momento da última verificação bem-sucedida da fonte, com ou sem mudança. Serve para saber se o collector está vivo. NULL = nunca verificou.';
COMMENT ON COLUMN jobs.consolidated  IS 'Flag de consolidação (0=pendente | 1=consolidado). O collector grava 0 quando uma sincronização altera linhas das tabelas do app; a consolidação central (fase 2) grava 1 depois de copiar os dados. Linhas com last_sync_at NULL não têm dados a consolidar.';
COMMENT ON COLUMN jobs.created_at    IS 'Timestamp de criação do registro.';
COMMENT ON COLUMN jobs.updated_at    IS 'Timestamp de última alteração, determina a versão do registro; mantido pelo trigger trg_jobs_updated_at.';

CREATE TRIGGER trg_jobs_updated_at
    BEFORE UPDATE ON jobs
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

-- migrate:down

DROP TABLE jobs;
DROP FUNCTION set_updated_at();
