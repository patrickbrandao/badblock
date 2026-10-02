# ripe/asnames — dados

Tabelas `ripe_asnames_*` no schema `public` do banco `badblock`. A migration
[`database/postgres/ripe_asnames/20260929000000_ripe_asnames.sql`](../../../../database/postgres/ripe_asnames/20260929000000_ripe_asnames.sql)
implementa este arquivo; `jobs` e a função `set_updated_at()` são de
`database/postgres/central/` ([../../../plataforma/postgres.md](../../../plataforma/postgres.md#tabela-jobs)).
Estilo (nomes, `COMMENT ON` em toda tabela e coluna, UUIDv7, migrations):
[../../../plataforma/postgres.md](../../../plataforma/postgres.md#estilo-das-tabelas).

| Tabela | Uma linha por | Chave natural | Escreve | Lê |
|---|---|---|---|---|
| `ripe_asnames_asn` | ASN do arquivo | `asn` | coletor (`MERGE`) | API |
| `ripe_asnames_run` | execução que baixou um arquivo novo (aplicado ou recusado) | — | coletor (`INSERT`) | coletor e API |

As tabelas espelham o **último arquivo aplicado**: um ASN que some da fonte é
apagado, sem soft delete (histórico e consolidação são das tabelas centrais
da fase 2). A aplicação é numa transação só, então a API nunca vê um arquivo
pela metade ([collector.md](collector.md#aplicação)).

## Migration

Um arquivo, `20260929000000_ripe_asnames.sql` (dbmate). Cabeçalho: o que cria
(`ripe_asnames_run` e `ripe_asnames_asn`, tabelas independentes), de que depende
(`central`, pela função `set_updated_at`, e a extensão `pg_trgm`, do contrib),
quem escreve e quem lê, a URL da fonte, o formato da linha e duas notas: o
espelho do último arquivo aplicado (acima) e a `pg_trgm` que fica no down
(abaixo).

- `migrate:up`, nesta ordem: `CREATE EXTENSION IF NOT EXISTS pg_trgm;`, a
  tabela `ripe_asnames_run` com os comentários, `ix_ripe_asnames_run_applied`, a tabela
  `ripe_asnames_asn` com os comentários, o trigger e os três índices.
- `migrate:down`: `DROP TABLE ripe_asnames_asn;` e `DROP TABLE ripe_asnames_run;`.
  **A extensão `pg_trgm` fica**: é do banco inteiro e outras pastas podem
  usá-la (um `DROP EXTENSION` quebraria os índices delas ou falharia no
  rollback).

## `ripe_asnames_run`

Histórico das execuções que baixaram um arquivo novo, aplicado ou recusado
(molde em [../../../padroes/coletor.md](../../../padroes/coletor.md#tabela-fonte_run)).
Verificações sem mudança não geram linha: ficam só em `jobs.last_check_at`. A
última linha com `status = 1` descreve o que está em `ripe_asnames_asn`, e o seu
`uuid` é a **versão do dataset** que a `api-ripe-asnames` usa no cache e no ETag.

| Coluna | Tipo | Nulo | Padrão | Conteúdo (resumo do `COMMENT ON`) |
|---|---|---|---|---|
| `uuid` | `uuid` | não | `uuidv7()` | PK; nas linhas `status = 1`, a versão do dataset |
| `status` | `smallint` | não | — | `0` = recusado (as tabelas ficaram como estavam), `1` = aplicado |
| `forced` | `boolean` | não | `false` | execução com `--force` (ignora "arquivo igual" e a trava de remoção) |
| `url` | `text` | não | — | URL de onde o arquivo foi baixado |
| `http_status` | `smallint` | sim | — | status HTTP da resposta do arquivo |
| `etag` | `text` | sim | — | `ETag` da resposta, como veio (fraco, `W/"…"`, com gzip); reenviado em `If-None-Match` |
| `last_modified` | `text` | sim | — | `Last-Modified` da resposta, como texto HTTP; reenviado em `If-Modified-Since` |
| `sha256` | `text` | sim | — | SHA-256 (hex minúsculo) do arquivo já descomprimido; comparado com o da última aplicação |
| `bytes` | `bigint` | sim | — | tamanho do arquivo descomprimido, em bytes |
| `asns` | `integer` | sim | — | ASNs aceitos pelo parser; NULL = o parser não rodou ou recusou o arquivo |
| `asn_inserted` | `integer` | sim | — | ASNs novos em `ripe_asnames_asn`; NULL quando `status = 0` |
| `asn_updated` | `integer` | sim | — | ASNs com `description`, `handle`, `name` ou `country` alterado; NULL quando `status = 0` |
| `asn_deleted` | `integer` | sim | — | ASNs que sumiram da fonte e foram apagados; NULL quando `status = 0` |
| `warnings` | `jsonb` | não | `'[]'::jsonb` | array de strings com os avisos do parser (só os primeiros) |
| `error` | `text` | sim | — | mensagem da recusa; NULL quando aplicado |
| `started_at` | `timestamptz` | não | — | início da verificação (antes do download) |
| `created_at` | `timestamptz` | não | `NOW()` | gravação da linha, no fim da execução |

| Constraint | Regra |
|---|---|
| `ripe_asnames_run_pkey` | `PRIMARY KEY (uuid)` (declarada na coluna, nome dado pelo Postgres) |
| `chk_ripe_asnames_run_status` | `CHECK (status BETWEEN 0 AND 1)` |
| `chk_ripe_asnames_run_sha256` | `CHECK (sha256 ~ '^[0-9a-f]{64}$')` (NULL passa) |

| Índice | Método | Colunas | Condição | Para quê |
|---|---|---|---|---|
| `ix_ripe_asnames_run_applied` | btree | `created_at DESC` | `WHERE status = 1` | última execução aplicada: validadores e SHA-256 do coletor, versão do dataset e `/meta` da API |

Sem `updated_at` nem trigger: as linhas nunca são alteradas.

## `ripe_asnames_asn`

Uma linha por ASN do `asn.txt`, que cobre os ASNs alocados por todos os RIRs.
`description` guarda o texto original; `handle`, `name` e `country` são
derivados dele pelo coletor ([fonte.md](fonte.md#campos-derivados)).

| Coluna | Tipo | Nulo | Padrão | Conteúdo (resumo do `COMMENT ON`) |
|---|---|---|---|---|
| `uuid` | `uuid` | não | `uuidv7()` | PK |
| `asn` | `bigint` | não | — | número do AS (0 a 4294967295), sem o prefixo `AS`; chave natural |
| `description` | `text` | não | — | texto da linha depois do número, exatamente como publicado (com o sufixo `, CC`, espaços extras e mojibake da fonte); fonte da verdade dos derivados |
| `handle` | `text` | sim | — | nome curto do AS (as-name) derivado: o trecho antes de ` - ` (ARIN/APNIC/LACNIC), a metade repetida de `X - X` (AFRINIC) ou a primeira palavra (RIPE NCC); **não é único**; NULL = a linha não traz |
| `name` | `text` | sim | — | nome da organização, derivado (o que vem depois do handle, sem o país); NULL = a linha não traz nome além do handle |
| `country` | `text` | sim | — | duas letras do sufixo `, CC` (ISO 3166-1 alfa-2 mais os códigos regionais `EU` e `AP`); NULL = a linha não termina em `, CC` |
| `created_at` | `timestamptz` | não | `NOW()` | primeira vez que o ASN apareceu na fonte |
| `updated_at` | `timestamptz` | não | `NOW()` | última alteração; mantido pelo trigger `trg_ripe_asnames_asn_updated_at` |

| Constraint | Regra |
|---|---|
| `ripe_asnames_asn_pkey` | `PRIMARY KEY (uuid)` (declarada na coluna, nome dado pelo Postgres) |
| `uq_ripe_asnames_asn_asn` | `UNIQUE (asn)` |
| `chk_ripe_asnames_asn_asn` | `CHECK (asn BETWEEN 0 AND 4294967295)` |
| `chk_ripe_asnames_asn_description` | `CHECK (description <> '')` |
| `chk_ripe_asnames_asn_handle` | `CHECK (handle <> '')` — ausente é NULL, nunca `''` |
| `chk_ripe_asnames_asn_name` | `CHECK (name <> '')` — idem |
| `chk_ripe_asnames_asn_country` | `CHECK (country ~ '^[A-Z]{2}$')` |

Trigger `trg_ripe_asnames_asn_updated_at`: `BEFORE UPDATE ON ripe_asnames_asn FOR EACH
ROW EXECUTE FUNCTION set_updated_at()`. Como o `MERGE` do coletor só atualiza
as linhas que mudaram, `updated_at` só anda quando um dos quatro campos muda.

| Índice | Método | Definição | Consulta |
|---|---|---|---|
| `uq_ripe_asnames_asn_asn` | btree único (da constraint) | `(asn)` | `WHERE asn = $1`; não crie outro índice em `asn` |
| `ix_ripe_asnames_asn_country` | btree | `(country, asn)` | `WHERE country = $1 ORDER BY asn` |
| `ix_ripe_asnames_asn_handle` | btree | `(lower(handle) text_pattern_ops)` | `WHERE lower(handle) = lower($1)`; serve também ao prefixo `lower(handle) LIKE lower($1) \|\| '%'` |
| `ix_ripe_asnames_asn_description_trgm` | GIN | `(description gin_trgm_ops)` | `WHERE description ILIKE '%' \|\| $1 \|\| '%'` |

**Busca por texto**: o índice trigram fica em `description` porque ela
contém o handle e o nome como publicados, sem depender das regras de
derivação (as 555 linhas ambíguas de [fonte.md](fonte.md#campos-derivados)).
Os trigramas do `pg_trgm` ignoram maiúsculas; termo com menos de 3
caracteres não aproveita o índice (a API exige 3 ou mais e escapa `%`, `_` e
`\` do termo), e termo sem letras nem dígitos não gera trigrama nenhum.
Medido com o arquivo de 2026-09-28 num PG18 local: `ILIKE '%google%'` em
~3 ms (49 linhas), pelo `ix_ripe_asnames_asn_description_trgm` (2026-09-28 e
2026-09-29, `TestApplyRealFile`).

## Mapeamento da fonte para as colunas

`ripe_asnames_asn`, uma linha do arquivo ([fonte.md](fonte.md#linhas)):

| Origem | Coluna |
|---|---|
| número antes do primeiro espaço em branco | `asn` |
| resto da linha, sem os espaços das pontas (UTF-8 inválido e NUL viram U+FFFD) | `description` |
| `parse.Derive(description)`; vazio vira NULL | `handle`, `name`, `country` |
| `NOW()` da aplicação que inseriu o ASN neste banco; não muda depois | `created_at` |
| `NOW()` da aplicação que mudou algum dos quatro campos (trigger) | `updated_at` |

`created_at` é "primeira vez que **este banco** viu o ASN", não a data de
alocação: apagar o volume e recarregar zera essas datas (a API as publica
como `first_seen`).

`ripe_asnames_run`, uma verificação que baixou um arquivo novo:

| Origem | Coluna |
|---|---|
| resultado (`1` aplicado, `0` recusado) e `--force` | `status`, `forced` |
| `SOURCE_URL` | `url` |
| resposta HTTP: status e cabeçalhos `ETag` e `Last-Modified`, como vieram | `http_status`, `etag`, `last_modified` |
| corpo descomprimido | `sha256`, `bytes` |
| parser | `asns`, `warnings` (os 50 primeiros avisos, mais `... e mais <n> avisos`) |
| `MERGE` | `asn_inserted`, `asn_updated`, `asn_deleted` |
| recusa | `error` |
| início da verificação, em UTC | `started_at` |

Valores zero ou vazios são gravados como NULL. Na prática, toda linha tem
`http_status = 200` e `sha256` preenchido (falhas antes do download e
respostas 304 não geram linha); `asns` só é NULL quando o parser recusou o
arquivo; nas linhas `status = 1`, `sha256` e `asns` estão sempre preenchidos.

## Consultas da API

A `api-ripe-asnames` só lê (`apps/ripe/asnames/api/internal/store/store.go`); o que ela
lê é o contrato destas tabelas com ela:

| Uso | Consulta | Índice |
|---|---|---|
| versão do dataset (a cada `DATASET_POLL`) e `/meta` | `SELECT uuid::text, created_at, url, COALESCE(sha256, ''), COALESCE(asns, 0) FROM ripe_asnames_run WHERE status = 1 ORDER BY created_at DESC LIMIT 1` | `ix_ripe_asnames_run_applied` |
| `/meta` (linha do coletor) | `SELECT last_sync_at, last_check_at, consolidated FROM jobs WHERE app = 'collector-ripe-asnames'` | `uq_jobs_app` |
| `/asn/{asn}` | `SELECT asn, description, handle, name, country, created_at, updated_at FROM ripe_asnames_asn WHERE asn = $1` | `uq_ripe_asnames_asn_asn` |
| `/country/{cc}` | `SELECT asn, handle, name FROM ripe_asnames_asn WHERE country = $1 ORDER BY asn` | `ix_ripe_asnames_asn_country` (ou `uq_ripe_asnames_asn_asn` em países muito grandes, como `US`) |
| `/handle/{handle}` | `SELECT asn, handle, name, country, description FROM ripe_asnames_asn WHERE lower(handle) = lower($1) ORDER BY asn` | `ix_ripe_asnames_asn_handle` |
| `/search?q=` | `SELECT asn, handle, name, country, description FROM ripe_asnames_asn WHERE description ILIKE $1 ORDER BY asn LIMIT $2`, com `$1 = '%<termo com \, % e _ escapados>%'` e `$2` = limite + 1 | `ix_ripe_asnames_asn_description_trgm` (termo raro) ou `uq_ripe_asnames_asn_asn` em ordem, parando no limite (termo comum) |

Colunas lidas: `ripe_asnames_run` (`uuid`, `status`, `created_at`, `url`,
`sha256`, `asns`), `ripe_asnames_asn` (`asn`, `description`, `handle`, `name`,
`country`, `created_at`, `updated_at`) e `jobs` (`app`, `last_sync_at`,
`last_check_at`, `consolidated`). O pool da API abre as conexões com
`plan_cache_mode = force_custom_plan`, para o planejador ver o termo da busca
em vez de um plano genérico. Validações, limites, planos medidos e tempos das
rotas: [api.md](api.md) e [api-rotas.md](api-rotas.md).

## Mudar o schema

- Sempre numa migration **nova** em `database/postgres/ripe_asnames/`
  (`make -C database/postgres new APP=ripe_asnames NAME=<descricao>`), testada com
  `make -C database/postgres test`
  ([../../../plataforma/postgres.md](../../../plataforma/postgres.md#migrations)).
- No mesmo trabalho: este arquivo, `internal/store` e o teste de integração
  do coletor.
- Mudou uma coluna, índice ou regra de derivação que a API lê (tabela
  acima)? Avise a `api-ripe-asnames` (spec, `internal/store` e o teste com o
  arquivo real dela). Regra de derivação nova vale para as linhas existentes
  depois de um `--force` ([fonte.md](fonte.md#mudar-uma-regra)).
