# Dados da fonte `anatel/pst`

Tabelas `anatel_pst_*` que o `collector-anatel-pst` grava e a
`api-anatel-pst` lê. Dono: sub-agente `collector-anatel-pst`; a API lê este
arquivo e não o altera ([../../README.md](../../README.md#pasta-de-uma-fonte)).

A migration `database/postgres/anatel_pst/20260930120000_anatel_pst.sql`
implementa este arquivo (tabela de controle `anatel_pst_schema_migrations`).
Ela depende da pasta `central/`, que cria a função `set_updated_at()` e a
tabela `jobs` ([../../../plataforma/postgres.md](../../../plataforma/postgres.md#tabela-jobs)),
e da extensão `pg_trgm` (criada com `IF NOT EXISTS` e não removida no down,
como no `asnames`). Estilo:
[../../../plataforma/postgres.md](../../../plataforma/postgres.md#estilo-das-tabelas).

## Tabelas

| Tabela | Uma linha por | Chave natural | Muda quando |
|---|---|---|---|
| `anatel_pst_run` | execução que baixou um arquivo novo (aplicado ou recusado) | — | a cada arquivo novo (só recebe `INSERT`) |
| `anatel_pst_provider` | prestadora (CNPJ) do arquivo | `document` | CNPJ novo, dado cadastral alterado, CNPJ que sumiu |
| `anatel_pst_service` | serviço notificado por uma prestadora (linha única de CNPJ do CSV) | `(provider_uuid, grant_fistel, notification_fistel, service_code)` | serviço novo, alterado ou que sumiu |

- As tabelas espelham o **último arquivo aplicado** (sem soft delete e sem
  histórico); a aplicação é uma transação só.
- Linha que continua na fonte mantém `uuid` e `created_at` (o `MERGE` a
  atualiza no lugar): `created_at` é o "visto pela primeira vez" desde a
  primeira carga deste banco.
- Pessoas físicas (CPF) não entram em tabela nenhuma
  ([fonte.md](fonte.md#linhas)).

## `anatel_pst_run`

| Coluna | Tipo | Nulo | Padrão | Conteúdo |
|---|---|---|---|---|
| `uuid` | `uuid` | não (PK) | `uuidv7()` | chave; com `status = 1`, é também a **versão do dataset** |
| `status` | `smallint` | não | — | `0` recusado, `1` aplicado |
| `forced` | `boolean` | não | `false` | veio de `--force` |
| `url` | `text` | não | — | URL do ZIP |
| `http_status` | `smallint` | sim | — | status HTTP da resposta do ZIP |
| `etag` | `text` | sim | — | `ETag` do ZIP; reenviado em `If-None-Match` |
| `last_modified` | `text` | sim | — | `Last-Modified` do ZIP, como texto HTTP; reenviado em `If-Modified-Since` |
| `sha256` | `text` | sim | — | SHA-256 do ZIP baixado, hex minúsculo |
| `bytes` | `bigint` | sim | — | tamanho do ZIP, em bytes |
| `csv_name` | `text` | sim | — | nome da entrada CSV dentro do ZIP |
| `csv_sha256` | `text` | sim | — | SHA-256 do CSV extraído, hex minúsculo (o ZIP pode ser regerado com o mesmo CSV) |
| `csv_bytes` | `bigint` | sim | — | tamanho do CSV extraído, em bytes |
| `csv_modified_at` | `timestamptz` | sim | — | data de modificação da entrada CSV no ZIP (campo MS-DOS), lida como hora de Brasília (UTC−3); NULL se a data MS-DOS da entrada for inválida ([collector.md](collector.md#o-zip-internalarchive)) |
| `rows` | `integer` | sim | — | linhas de dados do CSV (sem o cabeçalho) |
| `rows_cnpj` | `integer` | sim | — | linhas que não são de CPF (inclusive cópias e descartadas); `rows = rows_cnpj + rows_cpf` |
| `rows_cpf` | `integer` | sim | — | linhas de CPF, ignoradas |
| `duplicates` | `integer` | sim | — | linhas de CNPJ ignoradas por serem cópia exata de outra |
| `skipped` | `integer` | sim | — | linhas de CNPJ descartadas pelo parser |
| `providers` | `integer` | sim | — | prestadoras (CNPJs) aceitas |
| `services` | `integer` | sim | — | serviços aceitos |
| `provider_inserted` | `integer` | sim | — | prestadoras novas |
| `provider_updated` | `integer` | sim | — | prestadoras com algum dado alterado |
| `provider_deleted` | `integer` | sim | — | prestadoras que sumiram e foram apagadas (com os serviços, em cascata) |
| `service_inserted` | `integer` | sim | — | serviços novos |
| `service_updated` | `integer` | sim | — | serviços com algum dado alterado |
| `service_deleted` | `integer` | sim | — | serviços que sumiram (inclusive os apagados em cascata com a prestadora) |
| `warnings` | `jsonb` | não | `'[]'::jsonb` | avisos do parser (até 50 e, se houver mais, `... e mais N avisos`) |
| `error` | `text` | sim | — | motivo da recusa (NULL quando aplicado) |
| `started_at` | `timestamptz` | não | — | início da verificação |
| `created_at` | `timestamptz` | não | `NOW()` | gravação da linha |

- `chk_anatel_pst_run_status`: `CHECK (status BETWEEN 0 AND 1)`.
- `chk_anatel_pst_run_sha256`: `CHECK (sha256 ~ '^[0-9a-f]{64}$')`;
  `chk_anatel_pst_run_csv_sha256`: `CHECK (csv_sha256 ~ '^[0-9a-f]{64}$')`
  (NULL passa).
- `ix_anatel_pst_run_applied`: B-tree em `(created_at DESC) WHERE status = 1`.
- Sem `updated_at` nem trigger: a linha nunca muda depois de gravada.
- Nas linhas `status = 1`: `http_status = 200`; `sha256`, `bytes`,
  `csv_name`, `csv_sha256`, `csv_bytes`, as contagens e as seis alterações
  nunca são NULL (`csv_modified_at` também não, salvo uma entrada com a data
  MS-DOS inválida, que nunca apareceu); `error` é NULL.

## `anatel_pst_provider`

| Coluna | Tipo | Nulo | Padrão | Conteúdo |
|---|---|---|---|---|
| `uuid` | `uuid` | não (PK) | `uuidv7()` | chave |
| `document` | `text` | não | — | CNPJ, 14 dígitos sem pontuação; chave natural |
| `name` | `text` | não | — | razão social (coluna 3) |
| `trade_name` | `text` | sim | — | nome fantasia (coluna 4) |
| `street` | `text` | sim | — | logradouro da sede (coluna 15) |
| `number` | `text` | sim | — | número (coluna 16), como texto |
| `complement` | `text` | sim | — | complemento (coluna 17) |
| `district` | `text` | sim | — | bairro (coluna 18) |
| `postal_code` | `text` | sim | — | CEP como publicado (coluna 19) |
| `city_ibge_code` | `integer` | sim | — | código IBGE do município (coluna 20) |
| `city` | `text` | sim | — | município (coluna 21) |
| `state` | `text` | sim | — | UF (coluna 22) |
| `phone` | `text` | sim | — | telefone principal (coluna 23), texto livre |
| `email` | `text` | sim | — | e-mail (coluna 24) |
| `created_at` | `timestamptz` | não | `NOW()` | primeira vez que o CNPJ apareceu |
| `updated_at` | `timestamptz` | não | `NOW()` | última alteração, mantida pelo trigger |

- `uq_anatel_pst_provider_document`: `UNIQUE (document)`.
- `chk_anatel_pst_provider_document`: `CHECK (document ~ '^[0-9]{14}$')`.
- `chk_anatel_pst_provider_state`: `CHECK (state ~ '^[A-Z]{2}$')`.
- `chk_anatel_pst_provider_city_ibge_code`:
  `CHECK (city_ibge_code BETWEEN 1000000 AND 9999999)`.
- `ix_anatel_pst_provider_name_trgm`: GIN em `(name gin_trgm_ops)`;
  `ix_anatel_pst_provider_trade_name_trgm`: GIN em `(trade_name gin_trgm_ops)`
  — busca por parte do nome (`/anatel/pst/search`).
- `trg_anatel_pst_provider_updated_at`: `BEFORE UPDATE ... EXECUTE FUNCTION set_updated_at()`.
- Ausentes (`N/I`, `N/A`, `-`, vazio) são NULL ([fonte.md](fonte.md#leitura)).

## `anatel_pst_service`

| Coluna | Tipo | Nulo | Padrão | Conteúdo |
|---|---|---|---|---|
| `uuid` | `uuid` | não (PK) | `uuidv7()` | chave |
| `provider_uuid` | `uuid` | não | — | prestadora (FK → `anatel_pst_provider.uuid`) |
| `entity_type` | `text` | não | — | tipo de entidade (coluna 5): `Outorgada` ou `Dispensada de Outorga` |
| `grant_type` | `text` | não | — | tipo de outorga (coluna 6) |
| `grant_fistel` | `text` | sim | — | Fistel da outorga (coluna 7); NULL quando dispensada |
| `grant_process` | `text` | sim | — | processo SEI da outorga (coluna 8) |
| `granted_on` | `date` | sim | — | data de inclusão da outorga (coluna 9) |
| `service_group` | `text` | não | — | grupo do serviço (coluna 10), ex.: `Banda Larga Fixa` |
| `service_code` | `text` | não | — | código do serviço, 3 dígitos (coluna 11), ex.: `045` |
| `service_name` | `text` | não | — | nome do serviço (coluna 11, depois do ` - `) |
| `notification_fistel` | `text` | não | — | Fistel da notificação (coluna 12) |
| `notification_process` | `text` | sim | — | processo SEI da notificação (coluna 13) |
| `notified_on` | `date` | sim | — | data de inclusão da notificação (coluna 14) |
| `created_at` | `timestamptz` | não | `NOW()` | primeira vez que o serviço apareceu |
| `updated_at` | `timestamptz` | não | `NOW()` | última alteração, mantida pelo trigger |

- `uq_anatel_pst_service_key`:
  `UNIQUE NULLS NOT DISTINCT (provider_uuid, grant_fistel, notification_fistel, service_code)`
  — a chave natural; `NULLS NOT DISTINCT` porque `grant_fistel` é NULL nas
  entidades dispensadas. Serve também a busca dos serviços de uma prestadora
  (coluna inicial) e a cascata.
- `fk_anatel_pst_service_provider`:
  `FOREIGN KEY (provider_uuid) REFERENCES anatel_pst_provider(uuid) ON DELETE CASCADE`.
- `chk_anatel_pst_service_service_code`: `CHECK (service_code ~ '^[0-9]{3}$')`.
- `chk_anatel_pst_service_notification_fistel`:
  `CHECK (notification_fistel ~ '^[0-9]{11}$')`;
  `chk_anatel_pst_service_grant_fistel`: `CHECK (grant_fistel ~ '^[0-9]{11}$')`
  (NULL passa).
- `ix_anatel_pst_service_service_code`: B-tree em `(service_code, provider_uuid)`
  — catálogo e prestadoras de um serviço.
- `trg_anatel_pst_service_updated_at`: `BEFORE UPDATE ... EXECUTE FUNCTION set_updated_at()`.

## A migration

- Cabeçalho: o que cria, de que depende (`central`, `pg_trgm`), quem escreve
  e quem lê, a URL da fonte, a nota de que só pessoas jurídicas são
  guardadas e de que as tabelas espelham o último arquivo aplicado.
- `migrate:up`: `CREATE EXTENSION IF NOT EXISTS pg_trgm` → `anatel_pst_run`
  → `ix_anatel_pst_run_applied` → `anatel_pst_provider` (+ trigger e os dois
  índices GIN) → `anatel_pst_service` (+ trigger e índice), cada tabela
  seguida dos seus `COMMENT ON`.
- `migrate:down`: `DROP TABLE anatel_pst_service;`,
  `DROP TABLE anatel_pst_provider;`, `DROP TABLE anatel_pst_run;`.

## Mapeamento CSV → colunas

| CSV | Coluna |
|---|---|
| colunas 2, 3, 4, 15 a 24 da primeira linha de cada CNPJ | `anatel_pst_provider.*` |
| colunas 5 a 14 de cada linha de CNPJ | `anatel_pst_service.*`, com `provider_uuid` = a prestadora do CNPJ |
| ZIP e CSV inteiros | `anatel_pst_run.sha256`, `bytes`, `csv_*`, contagens e `warnings` |
| resposta HTTP | `anatel_pst_run.url`, `http_status`, `etag`, `last_modified` |

Regras de cada campo (ausentes, datas, código do serviço): [fonte.md](fonte.md#campos).

## Consultas da `api-anatel-pst`

Só leitura (`apps/anatel/pst/api/internal/store`).

| Uso na API | Consulta | Índice |
|---|---|---|
| versão do dataset e bloco `dataset` de `/meta` | `SELECT uuid::text, created_at, url, sha256, csv_sha256, csv_modified_at, providers, services FROM anatel_pst_run WHERE status = 1 ORDER BY created_at DESC LIMIT 1` | `ix_anatel_pst_run_applied` |
| bloco `collector` de `/meta` | `SELECT last_sync_at, last_check_at, consolidated FROM jobs WHERE app = $1` (`collector-anatel-pst`) | `uq_jobs_app` |
| `/provider/{cnpj}` | a linha de `anatel_pst_provider WHERE document = $1` e depois `SELECT ... FROM anatel_pst_service WHERE provider_uuid = $1 ORDER BY service_code, notified_on, notification_fistel` | `uq_anatel_pst_provider_document`, `uq_anatel_pst_service_key` |
| `/services` | `SELECT service_code, min(service_name), min(service_group), count(DISTINCT provider_uuid), count(*) FROM anatel_pst_service GROUP BY service_code ORDER BY service_code` (cada código tem um nome e um grupo só, então `min()` não muda o resultado; agrupar só pelo código deixa o planejador usar o índice sem ordenar em disco: ~60 ms em vez de ~240 ms) | `ix_anatel_pst_service_service_code` |
| `/service/{code}`: nome e grupo | `SELECT min(service_name), min(service_group) FROM anatel_pst_service WHERE service_code = $1` (NULL = código fora do dataset → 404) | `ix_anatel_pst_service_service_code` |
| `/service/{code}`: prestadoras | `SELECT p.document, p.name, p.trade_name, p.city, p.state FROM anatel_pst_provider p WHERE EXISTS (SELECT 1 FROM anatel_pst_service s WHERE s.provider_uuid = p.uuid AND s.service_code = $1) AND ($2::text IS NULL OR p.state = $2) ORDER BY p.document` | `ix_anatel_pst_service_service_code` |
| `/search?q=` | `SELECT document, name, trade_name, city, state FROM anatel_pst_provider WHERE name ILIKE $1 OR trade_name ILIKE $1 ORDER BY name, document LIMIT $2` (`$1` = `%<q escapado>%`) | `ix_anatel_pst_provider_name_trgm`, `ix_anatel_pst_provider_trade_name_trgm` |

## Mudando o schema

1. `make -C database/postgres new APP=anatel_pst NAME=<descricao>` e escreva
   `migrate:up` e `migrate:down` no estilo acima.
2. `make -C database/postgres test` (up → rollback → up).
3. Atualize este arquivo, o `internal/store` do coletor e o teste de
   integração dele; se a API lê a coluna ou a tabela, avise o sub-agente
   `api-anatel-pst`.
