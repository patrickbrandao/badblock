# Dados da fonte `rootanchors`

Tabelas `rootanchors_*` que o `collector-rootanchors` grava e a
`api-rootanchors` lê. Dono: sub-agente `collector-rootanchors`; a API lê este
arquivo e não o altera ([../README.md](../README.md#pasta-de-uma-fonte)).

A migration `database/postgres/rootanchors/20260930032822_rootanchors.sql`
implementa este arquivo (tabela de controle
`rootanchors_schema_migrations`). Ela depende da pasta `central/`, que cria a
função `set_updated_at()` e a tabela `jobs`
([../../plataforma/postgres.md](../../plataforma/postgres.md#tabela-jobs)).
Estilo (PK `uuidv7()`, constraints nomeadas, `COMMENT ON` em toda tabela e
coluna, trigger de `updated_at`):
[../../plataforma/postgres.md](../../plataforma/postgres.md#estilo-das-tabelas).
Em produção, `\d+ rootanchors_key` explica o modelo sem este arquivo.

## Tabelas

| Tabela | Uma linha por | Chave natural | Muda quando |
|---|---|---|---|
| `rootanchors_run` | execução que baixou um arquivo novo (aplicado ou recusado) | — | a cada arquivo novo (só recebe `INSERT`) |
| `rootanchors_key` | elemento `KeyDigest` do arquivo (uma KSK, inclusive as aposentadas) | `key_id` | chave nova numa rolagem, validade ou outro dado alterado, chave que sumiu |

- `rootanchors_key` espelha o **último arquivo aplicado**: o que some da fonte
  é apagado (sem soft delete e sem histórico — histórico e consolidação são da
  fase 2).
- A aplicação é uma transação só: a API nunca vê um arquivo pela metade.
- Linha que continua na fonte mantém `uuid` e `created_at` (o `MERGE` a
  atualiza no lugar). `created_at` é o "visto pela primeira vez" deste banco.
- O `id`, o `source` e a zona do `TrustAnchor` não têm tabela própria: ficam
  em `rootanchors_run` (há um `TrustAnchor` por arquivo).

## `rootanchors_run`

| Coluna | Tipo | Nulo | Padrão | Conteúdo |
|---|---|---|---|---|
| `uuid` | `uuid` | não (PK) | `uuidv7()` | chave; com `status = 1`, é também a **versão do dataset** |
| `status` | `smallint` | não | — | `0` recusado, `1` aplicado |
| `forced` | `boolean` | não | `false` | `true` quando a execução veio de `--force` |
| `url` | `text` | não | — | URL de onde o arquivo foi baixado |
| `http_status` | `smallint` | sim | — | status HTTP da resposta do arquivo |
| `etag` | `text` | sim | — | cabeçalho `ETag` (fraco, `W/"..."`); reenviado em `If-None-Match` |
| `last_modified` | `text` | sim | — | cabeçalho `Last-Modified`, como texto HTTP; reenviado em `If-Modified-Since` |
| `sha256` | `text` | sim | — | SHA-256 do arquivo baixado (descomprimido), hex minúsculo; comparado com a linha de `root-anchors.xml` do `checksums-sha256.txt` |
| `bytes` | `bigint` | sim | — | tamanho baixado (descomprimido), em bytes |
| `anchor_id` | `text` | sim | — | atributo `id` do `TrustAnchor` |
| `anchor_source` | `text` | sim | — | atributo `source` do `TrustAnchor` (NULL se vazio) |
| `zone` | `text` | sim | — | elemento `Zone` (sempre `.`) |
| `keys` | `integer` | sim | — | `KeyDigest` lidos do arquivo |
| `key_inserted` | `integer` | sim | — | chaves novas gravadas em `rootanchors_key` |
| `key_updated` | `integer` | sim | — | chaves com algum dado alterado |
| `key_deleted` | `integer` | sim | — | chaves que sumiram da fonte e foram apagadas |
| `warnings` | `jsonb` | não | `'[]'::jsonb` | array de strings com os avisos do parser (elementos e atributos desconhecidos) |
| `error` | `text` | sim | — | mensagem da recusa quando `status = 0` |
| `started_at` | `timestamptz` | não | — | início da verificação (antes do download) |
| `created_at` | `timestamptz` | não | `NOW()` | gravação da linha, no fim da execução (na aplicação, `NOW()` é o início da transação) |

- PK declarada na coluna (nome automático `rootanchors_run_pkey`).
- `chk_rootanchors_run_status`: `CHECK (status BETWEEN 0 AND 1)`.
- `chk_rootanchors_run_sha256`: `CHECK (sha256 ~ '^[0-9a-f]{64}$')` (NULL passa).
- `chk_rootanchors_run_zone`: `CHECK (zone = '.')` (NULL passa).
- `ix_rootanchors_run_applied`: B-tree em `(created_at DESC) WHERE status = 1`
  — a última execução aplicada (versão atual do dataset), para o coletor e a
  API.
- Sem `updated_at` nem trigger: a linha nunca é alterada depois de gravada.
- `COMMENT ON TABLE`: histórico das execuções que baixaram um arquivo novo;
  verificações sem mudança ficam só em `jobs.last_check_at`; a última linha
  com `status = 1` descreve o que está em `rootanchors_key`, e o `uuid` dela é
  a versão usada pela `api-rootanchors` no cache e no ETag.

Nas linhas `status = 1` (a API depende disso): `http_status` é `200`;
`sha256`, `bytes`, `anchor_id`, `zone`, `keys` e as três contagens de
alterações nunca são NULL; `error` é NULL; `anchor_source` pode ser NULL.
Numa recusa, `anchor_id`, `anchor_source`, `zone` e `keys` só vêm preenchidos
quando o parser aceitou o arquivo (recusa do `MIN_KEYS`, da trava de remoção
ou do banco); o resto em [collector.md](collector.md#recusas-e-falhas).

## `rootanchors_key`

| Coluna | Tipo | Nulo | Padrão | Conteúdo |
|---|---|---|---|---|
| `uuid` | `uuid` | não (PK) | `uuidv7()` | chave |
| `key_id` | `text` | não | — | atributo `id` do `KeyDigest` (ex.: `Kmyv6jo`); chave natural |
| `key_tag` | `integer` | não | — | key tag (0 a 65535), ex.: `20326`; **não** é único |
| `algorithm` | `smallint` | não | — | algoritmo DNSSEC (0 a 255; `8` = RSA/SHA-256) |
| `digest_type` | `smallint` | não | — | tipo do digest do DS: `1` SHA-1, `2` SHA-256, `4` SHA-384 |
| `digest` | `text` | não | — | digest do DS em hex **maiúsculo**, com o tamanho do tipo (40, 64 ou 96) |
| `public_key` | `text` | sim | — | chave pública do DNSKEY em base64, sem espaços; NULL = o `KeyDigest` não traz `PublicKey` |
| `flags` | `integer` | sim | — | flags do DNSKEY (`257` = zone key + SEP); NULL junto com `public_key` |
| `valid_from` | `timestamptz` | não | — | `validFrom` |
| `valid_until` | `timestamptz` | sim | — | `validUntil`; NULL = sem data de fim |
| `created_at` | `timestamptz` | não | `NOW()` | primeira vez que a chave apareceu na fonte |
| `updated_at` | `timestamptz` | não | `NOW()` | última alteração, mantida pelo trigger |

- PK declarada na coluna (nome automático `rootanchors_key_pkey`).
- `uq_rootanchors_key_key_id`: `UNIQUE (key_id)`.
- `chk_rootanchors_key_key_id`: `CHECK (key_id <> '')`.
- `chk_rootanchors_key_key_tag`: `CHECK (key_tag BETWEEN 0 AND 65535)`.
- `chk_rootanchors_key_algorithm`, `chk_rootanchors_key_digest_type`:
  `CHECK (... BETWEEN 0 AND 255)`.
- `chk_rootanchors_key_digest`: `CHECK (digest ~ '^[0-9A-F]+$')` (o tamanho
  por tipo é regra do parser, não do banco).
- `chk_rootanchors_key_public_key`: `CHECK (public_key ~ '^[A-Za-z0-9+/]+={0,2}$')`.
- `chk_rootanchors_key_flags`: `CHECK (flags BETWEEN 0 AND 65535)`.
- `chk_rootanchors_key_dnskey`: `CHECK ((public_key IS NULL) = (flags IS NULL))`.
- `chk_rootanchors_key_validity`: `CHECK (valid_until >= valid_from)`.
- `ix_rootanchors_key_key_tag`: B-tree em `(key_tag)` — as chaves de um key
  tag. A consulta por `key_id` usa o índice de `uq_rootanchors_key_key_id`.
- `trg_rootanchors_key_updated_at`:
  `BEFORE UPDATE ... FOR EACH ROW EXECUTE FUNCTION set_updated_at()`.
- `COMMENT ON TABLE`: uma linha por `KeyDigest`, com o DS e, nas mais novas, o
  DNSKEY; inclui as chaves aposentadas; a chave ativa num instante `t` é a que
  tem `valid_from <= t` e (`valid_until IS NULL` ou `valid_until > t`); chave
  que some da fonte é apagada.

"Ativa" não é coluna: depende do relógio, e uma coluna gerada não pode usar
`NOW()`. Quem precisa calcula na consulta (ver abaixo).

## A migration

- Cabeçalho: o que cria (`rootanchors_run` e `rootanchors_key`, tabelas
  independentes), de que depende (`central`), quem escreve e quem lê, a URL da
  fonte, o formato (`TrustAnchor` com `Zone` e um `KeyDigest` por KSK) e a nota
  de que as tabelas espelham o último arquivo aplicado numa transação só.
- `migrate:up`, nesta ordem, cada tabela seguida dos seus `COMMENT ON`:
  `rootanchors_run` → `ix_rootanchors_run_applied` → `rootanchors_key` →
  `trg_rootanchors_key_updated_at` → `ix_rootanchors_key_key_tag`.
- `migrate:down`: `DROP TABLE rootanchors_key;`, `DROP TABLE rootanchors_run;`.

## Mapeamento arquivo → colunas

| Arquivo | Coluna | Como |
|---|---|---|
| `TrustAnchor@id` | `rootanchors_run.anchor_id` | sem os espaços das pontas |
| `TrustAnchor@source` | `rootanchors_run.anchor_source` | idem; vazio vira NULL |
| `Zone` | `rootanchors_run.zone` | só `.` é aceito |
| `KeyDigest@id` | `rootanchors_key.key_id` | sem os espaços das pontas |
| `KeyDigest@validFrom`, `@validUntil` | `valid_from`, `valid_until` | RFC 3339, em UTC; `validUntil` ausente = NULL |
| `KeyTag`, `Algorithm`, `DigestType` | `key_tag`, `algorithm`, `digest_type` | decimal |
| `Digest` | `digest` | sem espaços, em maiúsculas |
| `PublicKey` | `public_key` | sem espaços nem quebras de linha; ausente = NULL |
| `Flags` | `flags` | decimal; ausente = NULL |
| elementos/atributos desconhecidos | `rootanchors_run.warnings` | um aviso cada ([fonte.md](fonte.md#avisos)) |
| arquivo inteiro | `rootanchors_run.sha256`, `bytes`, `keys` | hash e tamanho do download; contagem do parser |
| resposta HTTP | `rootanchors_run.url`, `http_status`, `etag`, `last_modified` | da requisição do arquivo |

## Consultas da `api-rootanchors`

Só leitura. A tabela tem poucas linhas (3 em 2026-09-30); os índices existem
para o plano não depender disso (conferido com `EXPLAIN` em 2026-09-30: a
versão usa `ix_rootanchors_run_applied`, o key tag usa
`ix_rootanchors_key_key_tag`).

| Uso na API | Consulta | Índice |
|---|---|---|
| versão do dataset e bloco `dataset` do `/meta` | `SELECT uuid::text, created_at, url, sha256, anchor_id, anchor_source, zone, keys FROM rootanchors_run WHERE status = 1 ORDER BY created_at DESC LIMIT 1` | `ix_rootanchors_run_applied` |
| bloco `collector` do `/meta` | `SELECT last_sync_at, last_check_at, consolidated FROM jobs WHERE app = $1` (`collector-rootanchors`) | `uq_jobs_app` |
| **todas as chaves** | `SELECT key_id, key_tag, algorithm, digest_type, digest, public_key, flags, valid_from, valid_until, created_at, updated_at FROM rootanchors_key ORDER BY valid_from, key_id` | — (a tabela inteira) |
| **as chaves de um key tag** | a mesma lista de colunas com `WHERE key_tag = $1 ORDER BY valid_from, key_id` (0, 1 ou mais linhas) | `ix_rootanchors_key_key_tag` |
| uma chave pelo `id` do `KeyDigest` (se a API tiver a rota) | a mesma lista com `WHERE key_id = $1` | `uq_rootanchors_key_key_id` |

Derivações que cabem à API, sem coluna própria:

- **ativa agora**: `valid_from <= NOW() AND (valid_until IS NULL OR valid_until > NOW())`
  (em 2026-09-30: 20326 e 38696; a 19036 expirou em 2019-01-11);
- o registro DS em texto: `. IN DS <key_tag> <algorithm> <digest_type> <digest>`;
- o DNSKEY em texto, quando há `public_key`: `. IN DNSKEY <flags> 3 <algorithm> <public_key>`;
- `first_seen` = `created_at`; `dataset.updated_at` = `rootanchors_run.created_at` da versão.

## Mudando o schema

1. `make -C database/postgres new APP=rootanchors NAME=<descricao>` e escreva
   `migrate:up` e `migrate:down` no estilo acima
   ([../../plataforma/postgres.md](../../plataforma/postgres.md#migrations)).
2. `make -C database/postgres test` (up → rollback → up).
3. Atualize este arquivo, o `internal/store` do coletor e o teste de
   integração dele.
4. Se a `api-rootanchors` lê a coluna ou a tabela, avise o sub-agente dela.
