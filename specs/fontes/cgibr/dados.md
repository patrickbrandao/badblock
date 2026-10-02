# Dados da fonte `cgibr`

Tabelas `cgibr_*` que o `collector-cgibr` grava e a `api-cgibr` lê. Dono:
sub-agente `collector-cgibr`; a API lê este arquivo e não o altera
([../README.md](../README.md#pasta-de-uma-fonte)).

A migration `database/postgres/cgibr/20260928230100_cgibr.sql` implementa
este arquivo (tabela de controle `cgibr_schema_migrations`). Ela depende da
pasta `central/`, que cria a função `set_updated_at()` e a tabela `jobs`
([../../plataforma/postgres.md](../../plataforma/postgres.md#tabela-jobs)).
Estilo (PK `uuidv7()`, constraints nomeadas, `COMMENT ON` em toda tabela e
coluna, trigger de `updated_at`):
[../../plataforma/postgres.md](../../plataforma/postgres.md#estilo-das-tabelas).
Em produção, `\d+ cgibr_asn` explica o modelo sem este arquivo.

## Tabelas

| Tabela | Uma linha por | Chave natural | Muda quando |
|---|---|---|---|
| `cgibr_run` | execução que baixou um arquivo novo (aplicado ou recusado) | — | a cada arquivo novo (só recebe `INSERT`) |
| `cgibr_asn` | ASN do arquivo | `asn` | ASN novo, nome ou documento alterado, ASN que sumiu |
| `cgibr_prefix` | bloco do arquivo, ligado ao ASN da linha em que aparece (`asn_uuid`) | `prefix` | bloco novo, bloco que mudou de ASN, bloco que sumiu |

- `cgibr_asn` e `cgibr_prefix` espelham o **último arquivo aplicado**: o que
  some da fonte é apagado (sem soft delete e sem histórico — histórico e
  consolidação são da fase 2).
- A aplicação é uma transação só: a API nunca vê um arquivo pela metade.
- Linha que continua na fonte mantém `uuid` e `created_at` (o `MERGE` a
  atualiza no lugar, nunca apaga e recria). Por isso `created_at` é o "visto
  pela primeira vez" — contado desde a primeira carga deste banco: os dados
  são descartáveis e uma recarga do zero reinicia as datas.

## `cgibr_run`

| Coluna | Tipo | Nulo | Padrão | Conteúdo |
|---|---|---|---|---|
| `uuid` | `uuid` | não (PK) | `uuidv7()` | chave; com `status = 1`, é também a **versão do dataset** |
| `status` | `smallint` | não | — | `0` recusado, `1` aplicado |
| `forced` | `boolean` | não | `false` | `true` quando a execução veio de `--force` (ignora "arquivo igual ao último" e a trava de remoção) |
| `url` | `text` | não | — | URL de onde o arquivo foi baixado |
| `http_status` | `smallint` | sim | — | status HTTP da resposta do arquivo |
| `etag` | `text` | sim | — | cabeçalho `ETag`; reenviado em `If-None-Match` na próxima verificação |
| `last_modified` | `text` | sim | — | cabeçalho `Last-Modified`, como texto HTTP; reenviado em `If-Modified-Since` |
| `sha256` | `text` | sim | — | SHA-256 do arquivo baixado, hex minúsculo; comparado com o `.sha256` publicado para decidir se houve mudança |
| `bytes` | `bigint` | sim | — | tamanho baixado, em bytes |
| `asns` | `integer` | sim | — | ASNs lidos do arquivo |
| `prefixes_v4` | `integer` | sim | — | blocos IPv4 lidos |
| `prefixes_v6` | `integer` | sim | — | blocos IPv6 lidos |
| `asn_inserted` | `integer` | sim | — | ASNs novos gravados em `cgibr_asn` |
| `asn_updated` | `integer` | sim | — | ASNs com nome ou documento alterado |
| `asn_deleted` | `integer` | sim | — | ASNs que sumiram da fonte e foram apagados |
| `prefix_inserted` | `integer` | sim | — | blocos novos gravados em `cgibr_prefix` |
| `prefix_updated` | `integer` | sim | — | blocos que mudaram de ASN |
| `prefix_deleted` | `integer` | sim | — | blocos que sumiram da fonte e foram apagados |
| `warnings` | `jsonb` | não | `'[]'::jsonb` | array de strings com os avisos do parser (linhas descartadas, blocos inválidos, duplicados), limitado às primeiras ocorrências |
| `error` | `text` | sim | — | mensagem da recusa quando `status = 0` |
| `started_at` | `timestamptz` | não | — | início da verificação (antes do download) |
| `created_at` | `timestamptz` | não | `NOW()` | gravação da linha, no fim da execução (na aplicação, `NOW()` é o início da transação) |

- PK declarada na coluna (nome automático `cgibr_run_pkey`).
- `chk_cgibr_run_status`: `CHECK (status BETWEEN 0 AND 1)`.
- `chk_cgibr_run_sha256`: `CHECK (sha256 ~ '^[0-9a-f]{64}$')` (NULL passa).
- `ix_cgibr_run_applied`: B-tree em `(created_at DESC) WHERE status = 1` —
  a última execução aplicada (versão atual do dataset), para o coletor e a
  API.
- Sem `updated_at` nem trigger: a linha nunca é alterada depois de gravada.
- `COMMENT ON TABLE`: histórico das execuções do `collector-cgibr` que
  baixaram um arquivo novo; verificações sem mudança ficam só em
  `jobs.last_check_at`; a última linha com `status = 1` descreve o que está em
  `cgibr_asn`/`cgibr_prefix`, e o `uuid` dela é a versão usada pela
  `api-cgibr` no cache e no ETag.

Nas linhas `status = 1` (a API depende disso): `http_status` é `200`;
`sha256`, `bytes`, `asns`, `prefixes_v4`, `prefixes_v6` e as seis contagens de
alterações nunca são NULL; `error` é NULL. O que cada coluna traz numa recusa:
[collector.md](collector.md#recusas-e-falhas).

## `cgibr_asn`

| Coluna | Tipo | Nulo | Padrão | Conteúdo |
|---|---|---|---|---|
| `uuid` | `uuid` | não (PK) | `uuidv7()` | chave |
| `asn` | `bigint` | não | — | número do sistema autônomo (0 a 4294967295), sem o `AS`; chave natural |
| `name` | `text` | não | — | nome do titular como publicado (campo 2) |
| `document` | `text` | não | — | documento como publicado (campo 3): CNPJ `00.000.000/0000-00` ou identificador estrangeiro de 8 dígitos |
| `document_digits` | `text` | não | gerada: `GENERATED ALWAYS AS (regexp_replace(document, '[^0-9]', '', 'g')) STORED` | só os dígitos de `document`: 14 = CNPJ, 8 = identificador estrangeiro; usada na busca por documento |
| `created_at` | `timestamptz` | não | `NOW()` | primeira vez que o ASN apareceu na fonte |
| `updated_at` | `timestamptz` | não | `NOW()` | última alteração (nome ou documento), mantida pelo trigger |

- PK declarada na coluna (nome automático `cgibr_asn_pkey`).
- `uq_cgibr_asn_asn`: `UNIQUE (asn)`.
- `chk_cgibr_asn_asn`: `CHECK (asn BETWEEN 0 AND 4294967295)`.
- `ix_cgibr_asn_document_digits`: B-tree em `(document_digits)` — busca por
  CNPJ ou identificador estrangeiro (`/cgibr/document`).
- `trg_cgibr_asn_updated_at`:
  `BEFORE UPDATE ... FOR EACH ROW EXECUTE FUNCTION set_updated_at()`.
- `COMMENT ON TABLE`: uma linha por ASN do arquivo; o nome e o documento são
  do titular brasileiro dos blocos, não necessariamente do ASN (algumas linhas
  usam ASN registrado fora do Brasil, ex.: AS174, AS8075, e nesse caso o
  vínculo vale para os blocos); ASN que some da fonte é apagado com seus
  blocos (cascata).

## `cgibr_prefix`

| Coluna | Tipo | Nulo | Padrão | Conteúdo |
|---|---|---|---|---|
| `uuid` | `uuid` | não (PK) | `uuidv7()` | chave |
| `prefix` | `cidr` | não | — | bloco em notação CIDR canônica (bits de host zerados pelo coletor); chave natural |
| `family` | `smallint` | não | gerada: `GENERATED ALWAYS AS (family(prefix)) STORED` | `4` IPv4, `6` IPv6 |
| `asn_uuid` | `uuid` | não | — | ASN da linha em que o bloco aparece (FK → `cgibr_asn.uuid`) |
| `created_at` | `timestamptz` | não | `NOW()` | primeira vez que o bloco apareceu na fonte |
| `updated_at` | `timestamptz` | não | `NOW()` | última alteração (troca de ASN), mantida pelo trigger |

- PK declarada na coluna (nome automático `cgibr_prefix_pkey`).
- `uq_cgibr_prefix_prefix`: `UNIQUE (prefix)` — um bloco pertence a um único
  ASN.
- `fk_cgibr_prefix_asn`:
  `FOREIGN KEY (asn_uuid) REFERENCES cgibr_asn(uuid) ON DELETE CASCADE`.
- `ix_cgibr_prefix_prefix_gist`: GiST em `(prefix inet_ops)` — bloco que
  contém um IP ou prefixo, com o operador `>>=` (`/cgibr/ip`, `/cgibr/prefix`).
- `ix_cgibr_prefix_asn`: B-tree em `(asn_uuid)` — blocos de um ASN
  (`/cgibr/asn`) e a cascata do `DELETE` em `cgibr_asn`.
- `trg_cgibr_prefix_updated_at`:
  `BEFORE UPDATE ... FOR EACH ROW EXECUTE FUNCTION set_updated_at()`.
- `COMMENT ON TABLE`: blocos IPv4 e IPv6 atribuídos a titulares brasileiros,
  cada um ligado ao ASN da linha em que aparece no arquivo; se a fonte repetir
  um bloco em outra linha, vale a primeira ocorrência.

## A migration

- Cabeçalho: o que cria (`cgibr_run`, tabela independente; `cgibr_asn`;
  `cgibr_prefix`), de que depende (`central`), quem escreve e quem lê, a URL da
  fonte, o formato da linha (`ASN|nome|documento|bloco|bloco|...`) e a nota de
  que as tabelas espelham o último arquivo aplicado numa transação só.
- `migrate:up`, nesta ordem, cada tabela seguida dos seus `COMMENT ON`:
  `cgibr_run` → `ix_cgibr_run_applied` → `cgibr_asn` →
  `trg_cgibr_asn_updated_at` → `ix_cgibr_asn_document_digits` →
  `cgibr_prefix` → `trg_cgibr_prefix_updated_at` →
  `ix_cgibr_prefix_prefix_gist` → `ix_cgibr_prefix_asn`.
- `migrate:down`, nesta ordem: `DROP TABLE cgibr_prefix;`,
  `DROP TABLE cgibr_asn;`, `DROP TABLE cgibr_run;` (índices e triggers caem
  com as tabelas).

## Mapeamento arquivo → colunas

| Arquivo | Coluna | Como |
|---|---|---|
| campo 1 (`AS61613`) | `cgibr_asn.asn` | sem o `AS`, como número ([fonte.md](fonte.md#linha-descartada)) |
| campo 2 | `cgibr_asn.name` | sem os espaços das pontas |
| campo 3 | `cgibr_asn.document` | sem os espaços das pontas; `document_digits` é calculado pelo banco |
| campos 4… | `cgibr_prefix.prefix`, uma linha por bloco | forma canônica; `family` é calculado pelo banco; `asn_uuid` = `uuid` do ASN da linha |
| ASN ou bloco repetido | — | vale a primeira ocorrência ([fonte.md](fonte.md#asn-repetido)) |
| arquivo inteiro | `cgibr_run.sha256`, `bytes`, `asns`, `prefixes_v4`, `prefixes_v6`, `warnings` | hash e tamanho do download; contagens e avisos do parser |
| resposta HTTP | `cgibr_run.url`, `http_status`, `etag`, `last_modified` | da requisição do arquivo |

## Consultas da `api-cgibr`

Só leitura (`apps/cgibr/api/internal/store`). O índice de cada uma é o que o
comentário da migration indica.

| Uso na API | Consulta | Índice |
|---|---|---|
| versão do dataset (na subida e a cada `DATASET_POLL`) e bloco `dataset` de `/cgibr/meta` | `SELECT uuid::text, created_at, url, sha256, asns, prefixes_v4, prefixes_v6 FROM cgibr_run WHERE status = 1 ORDER BY created_at DESC LIMIT 1` | `ix_cgibr_run_applied` |
| bloco `collector` de `/cgibr/meta` | `SELECT last_sync_at, last_check_at, consolidated FROM jobs WHERE app = $1` (`collector-cgibr`) | `uq_jobs_app` |
| `/cgibr/asn/{asn}` | `SELECT uuid::text, asn, name, document, document_digits, created_at, updated_at FROM cgibr_asn WHERE asn = $1` e depois `SELECT prefix FROM cgibr_prefix WHERE asn_uuid = $1::uuid ORDER BY family, prefix` (IPv4 primeiro, em ordem de endereço) | `uq_cgibr_asn_asn`, `ix_cgibr_prefix_asn` |
| `/cgibr/ip/{ip}` (o IP vira `/32` ou `/128`) e `/cgibr/prefix/{ip}/{len}` | `SELECT p.prefix, a.asn, a.name, a.document, a.document_digits FROM cgibr_prefix p JOIN cgibr_asn a ON a.uuid = p.asn_uuid WHERE p.prefix >>= $1::cidr ORDER BY masklen(p.prefix) DESC LIMIT 1` (o bloco mais específico) | `ix_cgibr_prefix_prefix_gist` |
| `/cgibr/document/{documento}` | `SELECT asn, name, document, document_digits FROM cgibr_asn WHERE document_digits = $1 ORDER BY asn` | `ix_cgibr_asn_document_digits` |
| `/cgibr/asns` | `SELECT asn, name, document, document_digits FROM cgibr_asn ORDER BY asn` | — (a tabela inteira, ~9 mil linhas) |

O que a API deriva dessas colunas: `first_seen` = `cgibr_asn.created_at`;
`document_type` sai do tamanho de `document_digits` (14 = `cnpj`, senão
`foreign`); `dataset.updated_at` = `cgibr_run.created_at` da versão.

## Mudando o schema

1. `make -C database/postgres new APP=cgibr NAME=<descricao>` e escreva
   `migrate:up` e `migrate:down` no estilo acima
   ([../../plataforma/postgres.md](../../plataforma/postgres.md#migrations)).
2. `make -C database/postgres test` (up → rollback → up).
3. Atualize este arquivo, o `internal/store` do coletor e o teste de
   integração dele.
4. Se a `api-cgibr` lê a coluna ou a tabela, avise o sub-agente dela (o
   `api.md` e o `internal/store` da API mudam junto).
