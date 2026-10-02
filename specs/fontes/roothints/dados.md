# roothints — dados

Tabelas `roothints_*` no schema `public` do banco `badblock`. A migration
[`database/postgres/roothints/20260930032921_roothints.sql`](../../../database/postgres/roothints/20260930032921_roothints.sql)
implementa este arquivo; `jobs` e a função `set_updated_at()` são de
`database/postgres/central/` ([../../plataforma/postgres.md](../../plataforma/postgres.md#tabela-jobs)).
Estilo (nomes, `COMMENT ON` em toda tabela e coluna, UUIDv7, migrations):
[../../plataforma/postgres.md](../../plataforma/postgres.md#estilo-das-tabelas).

| Tabela | Uma linha por | Chave natural | Escreve | Lê |
|---|---|---|---|---|
| `roothints_server` | servidor raiz do arquivo (linha `. NS`) | `name` (e `letter`, também única) | coletor (`MERGE`) | API |
| `roothints_run` | execução que baixou um arquivo novo (aplicado ou recusado) | — | coletor (`INSERT`) | coletor e API |

As tabelas espelham o **último arquivo aplicado**: um servidor que some da
fonte é apagado, sem soft delete (histórico e consolidação são das tabelas
centrais da fase 2). A aplicação é numa transação só, então a API nunca vê um
arquivo pela metade ([collector.md](collector.md#aplicação)).

## Migration

Um arquivo, `20260930032921_roothints.sql` (dbmate). Cabeçalho: o que cria
(`roothints_run` e `roothints_server`, tabelas independentes), de que depende
(`central`, pela função `set_updated_at`), quem escreve e quem lê, a URL da
fonte e do hash, o formato do arquivo e a nota do espelho do último arquivo
aplicado.

- `migrate:up`, nesta ordem: a tabela `roothints_run` com os comentários,
  `ix_roothints_run_applied`, a tabela `roothints_server` com os comentários
  e o trigger. Nenhum índice além dos das constraints (abaixo).
- `migrate:down`: `DROP TABLE roothints_server;` e `DROP TABLE roothints_run;`.

## `roothints_run`

Histórico das execuções que baixaram um arquivo novo, aplicado ou recusado
(molde em [../../padroes/coletor.md](../../padroes/coletor.md#tabela-fonte_run)).
Verificações sem mudança não geram linha: ficam só em `jobs.last_check_at`. A
última linha com `status = 1` descreve o que está em `roothints_server`, e o
seu `uuid` é a **versão do dataset** que a `api-roothints` usa no cache e no
ETag.

| Coluna | Tipo | Nulo | Padrão | Conteúdo (resumo do `COMMENT ON`) |
|---|---|---|---|---|
| `uuid` | `uuid` | não | `uuidv7()` | PK; nas linhas `status = 1`, a versão do dataset |
| `status` | `smallint` | não | — | `0` = recusado (a tabela ficou como estava), `1` = aplicado |
| `forced` | `boolean` | não | `false` | execução com `--force` (ignora "arquivo igual", "mais antigo" e a trava de remoção) |
| `url` | `text` | não | — | URL de onde o arquivo foi baixado |
| `http_status` | `smallint` | sim | — | status HTTP da resposta do arquivo |
| `etag` | `text` | sim | — | `ETag` da resposta, como veio; reenviado em `If-None-Match` (muda a cada publicação da zona raiz) |
| `last_modified` | `text` | sim | — | `Last-Modified` da resposta, como texto HTTP; reenviado em `If-Modified-Since` |
| `md5` | `text` | sim | — | MD5 (hex minúsculo) do arquivo baixado; comparado com o `.md5` publicado |
| `sha256` | `text` | sim | — | SHA-256 (hex minúsculo) do arquivo; comparado com o da última aplicação |
| `bytes` | `bigint` | sim | — | tamanho do arquivo, em bytes |
| `last_update` | `date` | sim | — | data de `last update:` do cabeçalho; NULL = o parser não aceitou o arquivo |
| `zone_serial` | `bigint` | sim | — | serial de `related version of root zone:` (`AAAAMMDDnn`); menor que o do último aplicado = arquivo ignorado (sem linha); NULL = o parser não aceitou o arquivo |
| `servers` | `integer` | sim | — | servidores lidos (linhas `. NS`); NULL = o parser não rodou ou recusou o arquivo |
| `ipv4_addresses` | `integer` | sim | — | servidores com registro `A` (NULL como `servers`) |
| `ipv6_addresses` | `integer` | sim | — | servidores com registro `AAAA` (NULL como `servers`) |
| `server_inserted` | `integer` | sim | — | servidores novos em `roothints_server`; NULL quando `status = 0` |
| `server_updated` | `integer` | sim | — | servidores com endereço, TTL ou comentário alterado; NULL quando `status = 0` |
| `server_deleted` | `integer` | sim | — | servidores que sumiram da fonte e foram apagados; NULL quando `status = 0` |
| `warnings` | `jsonb` | não | `'[]'::jsonb` | array de strings com os avisos do parser (servidor sem IPv4 ou sem IPv6) |
| `error` | `text` | sim | — | mensagem da recusa; NULL quando aplicado |
| `started_at` | `timestamptz` | não | — | início da verificação (antes do download) |
| `created_at` | `timestamptz` | não | `NOW()` | gravação da linha, no fim da execução |

| Constraint | Regra |
|---|---|
| `roothints_run_pkey` | `PRIMARY KEY (uuid)` (declarada na coluna, nome dado pelo Postgres) |
| `chk_roothints_run_status` | `CHECK (status BETWEEN 0 AND 1)` |
| `chk_roothints_run_md5` | `CHECK (md5 ~ '^[0-9a-f]{32}$')` (NULL passa) |
| `chk_roothints_run_sha256` | `CHECK (sha256 ~ '^[0-9a-f]{64}$')` (NULL passa) |
| `chk_roothints_run_zone_serial` | `CHECK (zone_serial BETWEEN 0 AND 4294967295)` (serial de zona: 32 bits sem sinal) |

| Índice | Método | Colunas | Condição | Para quê |
|---|---|---|---|---|
| `ix_roothints_run_applied` | btree | `created_at DESC` | `WHERE status = 1` | última execução aplicada: validadores, MD5, SHA-256 e serial do coletor; versão do dataset e `/meta` da API |

Sem `updated_at` nem trigger: as linhas nunca são alteradas.

## `roothints_server`

Uma linha por servidor raiz do `named.root` (hoje os 13, `a` a
`m.root-servers.net`), com os dois endereços e o comentário do bloco.

| Coluna | Tipo | Nulo | Padrão | Conteúdo (resumo do `COMMENT ON`) |
|---|---|---|---|---|
| `uuid` | `uuid` | não | `uuidv7()` | PK |
| `name` | `text` | não | — | nome do servidor em minúsculas e sem o ponto final (`a.root-servers.net`); chave natural |
| `letter` | `text` | não | — | letra do servidor (`a`), o primeiro rótulo de `name`; única |
| `ipv4` | `inet` | sim | — | endereço do registro `A`, `/32`; NULL = o arquivo não traz `A` (o coletor avisa) |
| `ipv6` | `inet` | sim | — | endereço do registro `AAAA`, `/128`; NULL = o arquivo não traz `AAAA` (o coletor avisa) |
| `ns_ttl` | `integer` | não | — | TTL, em segundos, da linha `. NS` (3600000 no arquivo atual) |
| `ipv4_ttl` | `integer` | sim | — | TTL do registro `A`; NULL junto com `ipv4` |
| `ipv6_ttl` | `integer` | sim | — | TTL do registro `AAAA`; NULL junto com `ipv6` |
| `note` | `text` | sim | — | comentário do bloco no arquivo, sem o `;` (`FORMERLY NS.INTERNIC.NET`, `OPERATED BY VERISIGN, INC.`); NULL = sem comentário |
| `created_at` | `timestamptz` | não | `NOW()` | primeira vez que o servidor apareceu na fonte |
| `updated_at` | `timestamptz` | não | `NOW()` | última alteração; mantido pelo trigger `trg_roothints_server_updated_at` |

| Constraint | Regra |
|---|---|
| `roothints_server_pkey` | `PRIMARY KEY (uuid)` (declarada na coluna, nome dado pelo Postgres) |
| `uq_roothints_server_name` | `UNIQUE (name)` |
| `uq_roothints_server_letter` | `UNIQUE (letter)` |
| `chk_roothints_server_name` | `CHECK (name ~ '^[a-z]\.root-servers\.net$')` — a mesma regra do parser |
| `chk_roothints_server_letter` | `CHECK (letter = left(name, 1))` |
| `chk_roothints_server_ipv4` | `CHECK (family(ipv4) = 4 AND masklen(ipv4) = 32)` |
| `chk_roothints_server_ipv6` | `CHECK (family(ipv6) = 6 AND masklen(ipv6) = 128)` |
| `chk_roothints_server_address` | `CHECK (ipv4 IS NOT NULL OR ipv6 IS NOT NULL)` |
| `chk_roothints_server_ns_ttl` | `CHECK (ns_ttl BETWEEN 0 AND 2147483647)` |
| `chk_roothints_server_v4_ttl` | `CHECK (ipv4_ttl BETWEEN 0 AND 2147483647 AND (ipv4_ttl IS NULL) = (ipv4 IS NULL))` |
| `chk_roothints_server_v6_ttl` | `CHECK (ipv6_ttl BETWEEN 0 AND 2147483647 AND (ipv6_ttl IS NULL) = (ipv6 IS NULL))` |
| `chk_roothints_server_note` | `CHECK (note <> '')` — ausente é NULL, nunca `''` |

Trigger `trg_roothints_server_updated_at`: `BEFORE UPDATE ON roothints_server
FOR EACH ROW EXECUTE FUNCTION set_updated_at()`. Como o `MERGE` do coletor só
atualiza as linhas que mudaram, `updated_at` só anda quando um endereço, TTL
ou comentário muda.

**Índices**: só os das constraints únicas — `uq_roothints_server_name` e
`uq_roothints_server_letter` (btree) servem às consultas por nome e por
letra. Com 13 linhas, a lista inteira é um *seq scan* de uma página; não crie
outro índice.

`created_at` é "primeira vez que **este banco** viu o servidor": apagar o
volume e recarregar zera essas datas.

## Mapeamento da fonte para as colunas

`roothints_server`, um bloco do arquivo ([fonte.md](fonte.md#regras-do-parser)):

| Origem | Coluna |
|---|---|
| alvo da linha `. <ttl> NS <X.ROOT-SERVERS.NET.>`, em minúsculas e sem o ponto final | `name` |
| primeiro rótulo de `name` | `letter` |
| TTL da linha `NS` | `ns_ttl` |
| dado e TTL do registro `A` do servidor (ausente = NULL nos dois) | `ipv4`, `ipv4_ttl` |
| dado e TTL do registro `AAAA` do servidor (ausente = NULL nos dois) | `ipv6`, `ipv6_ttl` |
| linhas de comentário não vazias do bloco, sem o `;`, unidas por um espaço (vazio = NULL) | `note` |
| `NOW()` da aplicação que inseriu o servidor neste banco | `created_at` |
| `NOW()` da aplicação que mudou alguma coluna de dado (trigger) | `updated_at` |

`roothints_run`, uma verificação que baixou um arquivo novo:

| Origem | Coluna |
|---|---|
| resultado (`1` aplicado, `0` recusado) e `--force` | `status`, `forced` |
| `SOURCE_URL` | `url` |
| resposta HTTP: status e cabeçalhos `ETag` e `Last-Modified`, como vieram | `http_status`, `etag`, `last_modified` |
| corpo | `md5`, `sha256`, `bytes` |
| cabeçalho do arquivo (`last update:`, `related version of root zone:`) | `last_update`, `zone_serial` |
| parser | `servers`, `ipv4_addresses`, `ipv6_addresses`, `warnings` |
| `MERGE` | `server_inserted`, `server_updated`, `server_deleted` |
| recusa | `error` |
| início da verificação, em UTC | `started_at` |

Quando o parser aceitou o arquivo, o cabeçalho e as três contagens são
gravados mesmo numa recusa posterior (`MIN_SERVERS`, trava de remoção, erro
do banco), e `ipv4_addresses`/`ipv6_addresses` podem ser `0`; quando o parser
recusou (ou nem rodou, na conferência do MD5), ficam NULL. Os outros valores
zero ou vazios são gravados como NULL. Na prática toda linha tem
`http_status = 200`, `md5` e `sha256` (falhas antes do download e respostas
304 não geram linha); nas linhas `status = 1`, `last_update`, `zone_serial` e
`servers` estão sempre preenchidos.

## Consultas da API

A `api-roothints` só lê; o que ela lê é o contrato destas tabelas com ela
(as consultas exatas, com validação e cache, ficam em [api.md](api.md)):

| Uso | Consulta | Índice |
|---|---|---|
| versão do dataset e `/meta` | `SELECT uuid::text, created_at, url, md5, sha256, last_update, zone_serial, servers FROM roothints_run WHERE status = 1 ORDER BY created_at DESC LIMIT 1` | `ix_roothints_run_applied` |
| `/meta` (linha do coletor) | `SELECT last_sync_at, last_check_at, consolidated FROM jobs WHERE app = 'collector-roothints'` | `uq_jobs_app` |
| todos os servidores, em ordem | `SELECT name, letter, ipv4, ipv6, ns_ttl, ipv4_ttl, ipv6_ttl, note, created_at, updated_at FROM roothints_server ORDER BY letter` | nenhum (13 linhas) |
| um servidor pela letra (`a`–`m`) | `... FROM roothints_server WHERE letter = $1` (`$1` em minúsculas) | `uq_roothints_server_letter` |
| um servidor pelo nome | a API reduz o nome à letra (`A.ROOT-SERVERS.NET.` → `a`) e usa a consulta pela letra — os CHECKs `chk_roothints_server_name` e `chk_roothints_server_letter` garantem que a letra determina o nome | `uq_roothints_server_letter` |

- `ipv4` e `ipv6` são `inet` com máscara `/32` e `/128`: `host(ipv4)` devolve só o
  endereço (`ipv4::text` traz a máscara, `198.41.0.4/32`); no pgx, escaneie
  em `netip.Prefix` e use `.Addr()`, ou em `*string` com `host(...)`. NULL = o servidor não tem aquela família.
- As colunas `*_ttl` podem ser mostradas ou não; `note` é texto livre da
  fonte (em maiúsculas, como publicado).

Colunas lidas: `roothints_run` (`uuid`, `status`, `created_at`, `url`, `md5`,
`sha256`, `last_update`, `zone_serial`, `servers`), `roothints_server` (todas
menos `uuid`) e `jobs` (`app`, `last_sync_at`, `last_check_at`,
`consolidated`).

## Mudar o schema

- Sempre numa migration **nova** em `database/postgres/roothints/`
  (`make -C database/postgres new APP=roothints NAME=<descricao>`), testada
  com `make -C database/postgres test`
  ([../../plataforma/postgres.md](../../plataforma/postgres.md#migrations)).
- No mesmo trabalho: este arquivo, `internal/store` e o teste de integração
  do coletor.
- Mudou uma coluna ou regra que a API lê (tabela acima)? Avise a
  `api-roothints` (spec e `internal/store` dela).
