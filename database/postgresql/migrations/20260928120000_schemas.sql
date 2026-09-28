-- migrate:up

-- Schemas do BadBlock. As migrations rodam como badblock_owner, que fica dono
-- de tudo o que for criado aqui e nas migrations seguintes.
--
--   ingest    execuções de sync e dados de cada fonte, já normalizados
--   registry  tabelas centrais (holder, asn, prefix) e histórico de mudanças
--   api       views estáveis: o único contrato de leitura das APIs

CREATE SCHEMA ingest;
COMMENT ON SCHEMA ingest IS 'Execuções de sync e dados normalizados por fonte';

CREATE SCHEMA registry;
COMMENT ON SCHEMA registry IS 'Tabelas centrais (holder, asn, prefix) e histórico de mudanças';

CREATE SCHEMA api;
COMMENT ON SCHEMA api IS 'Views estáveis consumidas pelas APIs (contrato de leitura)';

REVOKE ALL ON SCHEMA ingest, registry, api FROM PUBLIC;

-- registry-sync: lê e escreve ingest e registry.
GRANT USAGE ON SCHEMA ingest, registry TO badblock_sync;
ALTER DEFAULT PRIVILEGES IN SCHEMA ingest, registry
    GRANT SELECT, INSERT, UPDATE, DELETE, TRUNCATE ON TABLES TO badblock_sync;
ALTER DEFAULT PRIVILEGES IN SCHEMA ingest, registry
    GRANT USAGE, SELECT, UPDATE ON SEQUENCES TO badblock_sync;

-- registry-api: só leitura, e só no schema api. As views rodam com os
-- privilégios do dono, então a API não precisa enxergar as tabelas.
GRANT USAGE ON SCHEMA api TO badblock_api;
ALTER DEFAULT PRIVILEGES IN SCHEMA api
    GRANT SELECT ON TABLES TO badblock_api;

-- migrate:down

ALTER DEFAULT PRIVILEGES IN SCHEMA api
    REVOKE SELECT ON TABLES FROM badblock_api;
ALTER DEFAULT PRIVILEGES IN SCHEMA ingest, registry
    REVOKE USAGE, SELECT, UPDATE ON SEQUENCES FROM badblock_sync;
ALTER DEFAULT PRIVILEGES IN SCHEMA ingest, registry
    REVOKE SELECT, INSERT, UPDATE, DELETE, TRUNCATE ON TABLES FROM badblock_sync;

DROP SCHEMA api;
DROP SCHEMA registry;
DROP SCHEMA ingest;
