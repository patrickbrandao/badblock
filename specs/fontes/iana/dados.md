# Dados da fonte `iana`

Migration: [`database/postgres/iana/20260929000000_iana.sql`](../../../database/postgres/iana/20260929000000_iana.sql)
(tabela de controle `iana_schema_migrations`), que depende de `central/`
(função `set_updated_at()` e tabela `jobs`, ver
[../../plataforma/postgres.md](../../plataforma/postgres.md#tabela-jobs)).
Estilo, acesso e rotinas das migrations:
[../../plataforma/postgres.md](../../plataforma/postgres.md).

O `migrate:up` cria 6 tabelas **independentes, sem FK**, com `COMMENT ON` em
toda tabela e coluna (em produção, `\d+ iana_asn_block` explica o modelo sem
estas specs). O `migrate:down` apaga na ordem inversa: `iana_rdap_service`,
`iana_special_asn`, `iana_special_prefix`, `iana_prefix_block`,
`iana_asn_block`, `iana_run`. O cabeçalho do arquivo diz o que ele cria, a
dependência, as 10 URLs ([fonte.md](fonte.md#arquivos)) e três notas:

- as tabelas espelham o **último dataset aplicado**: o que some da fonte é
  apagado (sem soft delete); histórico e cruzamento com outras fontes são das
  tabelas centrais da fase 2
  ([../../projeto/visao-geral.md](../../projeto/visao-geral.md#fases));
- os 10 arquivos são aplicados juntos, numa transação só: a `api-iana` nunca
  vê uma mistura de arquivos novos e velhos;
- datas são **texto**, porque a IANA publica ora `AAAA-MM` (`1981-09`), ora
  `AAAA-MM-DD` (`2008-12-03`): inventar o dia 1 mudaria o dado.

## Tabelas

| Tabela | Uma linha por | Chave natural | Linhas (2026-09-28) |
|---|---|---|---|
| [`iana_run`](#iana_run) | execução em que algum arquivo mudou (aplicada ou recusada) | — | — |
| [`iana_asn_block`](#iana_asn_block) | faixa de `as-numbers-1`/`-2` (sem a "See Sub-registry") | `asn_start` | 173 (88 + 85) |
| [`iana_prefix_block`](#iana_prefix_block) | `/8` do IPv4 e bloco do IPv6 unicast | `prefix` | 307 (256 + 51) |
| [`iana_special_prefix`](#iana_special_prefix) | bloco dos special-purpose registries IPv4/IPv6 | `prefix` | 51 (26 + 25) |
| [`iana_special_asn`](#iana_special_asn) | faixa de `special-purpose-as-numbers` | `asn_start` | 9 |
| [`iana_rdap_service`](#iana_rdap_service) | entrada dos 3 JSONs de bootstrap RDAP | `(kind, resource)` | 414 (159 + 221 + 34) |

Regras das 5 tabelas de dados:

- `uuid uuid PRIMARY KEY DEFAULT uuidv7()` (constraint com o nome padrão do
  Postgres, `<tabela>_pkey`);
- `created_at timestamptz NOT NULL DEFAULT NOW()` (primeira vez que o
  registro apareceu na fonte) e `updated_at timestamptz NOT NULL DEFAULT
  NOW()` (última alteração), mantido pelo trigger
  `trg_<tabela>_updated_at BEFORE UPDATE ... FOR EACH ROW EXECUTE FUNCTION
  set_updated_at()`;
- `registry`, onde existe: `CHECK (registry IN ('afrinic', 'apnic', 'arin',
  'lacnic', 'ripencc'))`; NULL = sem RIR;
- datas: `text` com `CHECK (<coluna> ~ '^[0-9]{4}-[0-9]{2}(-[0-9]{2})?$')`;
- os `CHECK` deixam passar NULL; o coletor grava texto vazio como NULL e
  lista vazia como `'{}'` (nunca NULL);
- o coletor não grava `uuid`, `family`, `created_at` nem `updated_at`
  (padrão, coluna gerada e trigger).

## `iana_run`

Comentário: histórico das execuções do `collector-iana` em que ao menos um
dos 10 arquivos mudou (dataset aplicado ou recusado); verificações sem
mudança não geram linha (ficam só em `jobs.last_check_at`). A última linha
com `status = 1` descreve o dataset que está nas tabelas, e o `uuid` dela é a
**versão do dataset** que a `api-iana` usa no cache e no ETag. Sem
`updated_at` nem trigger: as linhas não são alteradas.

| Coluna | Tipo | Nulo / padrão | Conteúdo |
|---|---|---|---|
| `uuid` | `uuid` | PK, `DEFAULT uuidv7()` | versão do dataset quando `status = 1` |
| `status` | `smallint` | `NOT NULL` | `0` recusado (parser, sanidade, trava de remoção ou erro no banco; tabelas intactas), `1` aplicado. Falha de download não gera linha |
| `forced` | `boolean` | `NOT NULL DEFAULT false` | `true` com `--force` |
| `sha256` | `text` | NULL | SHA-256 combinado do dataset, hex minúsculo: hash das linhas `<nome>:<sha256 do arquivo>\n` dos 10 arquivos, na ordem fixa; igual ao da última aplicada = mesmo dataset |
| `files` | `jsonb` | `NOT NULL DEFAULT '[]'::jsonb` | um objeto por arquivo, na ordem fixa ([abaixo](#files-e-changes)) |
| `changes` | `jsonb` | NULL | alterações por tabela; NULL quando `status = 0` |
| `warnings` | `jsonb` | `NOT NULL DEFAULT '[]'::jsonb` | array de textos com os avisos do parser (linhas descartadas, campos não reconhecidos, bits de host): os 50 primeiros e, se houve mais, uma última linha `... e mais N avisos` |
| `error` | `text` | NULL | motivo da recusa; NULL quando aplicado |
| `started_at` | `timestamptz` | `NOT NULL` | início da verificação (antes dos downloads) |
| `created_at` | `timestamptz` | `NOT NULL DEFAULT NOW()` | gravação da linha (fim da execução) |

| Constraint | Regra |
|---|---|
| `chk_iana_run_status` | `status BETWEEN 0 AND 1` |
| `chk_iana_run_sha256` | `sha256 ~ '^[0-9a-f]{64}$'` |
| `chk_iana_run_files` | `jsonb_typeof(files) = 'array'` |
| `chk_iana_run_changes` | `changes IS NULL OR jsonb_typeof(changes) = 'object'` |
| `chk_iana_run_applied` | `status = 0 OR (sha256 IS NOT NULL AND changes IS NOT NULL)` |

Índice `ix_iana_run_applied` — btree `(created_at DESC) WHERE status = 1`:
é por ele que o coletor e a API acham a última execução aplicada.

### `files` e `changes`

```json
"files": [
  {"name": "as-numbers-1", "url": "https://www.iana.org/assignments/as-numbers/as-numbers-1.csv",
   "http_status": 200, "last_modified": "Sat, 19 Sep 2026 00:44:44 GMT",
   "sha256": "47f5…", "bytes": 7936, "changed": true, "rows": 88},
  …
  {"name": "rdap-asn", "url": "https://data.iana.org/rdap/asn.json", "http_status": 200,
   "etag": "W/\"1138-65336a3cb9688-br\"", "last_modified": "Mon, 01 Jun 2026 20:00:01 GMT",
   "sha256": "…", "bytes": 4408, "changed": true, "rows": 159, "publication": "2026-06-01T20:00:01Z"}
]
```

| Chave | Conteúdo |
|---|---|
| `name` | nome do arquivo ([fonte.md](fonte.md#arquivos)); os 10, sempre na ordem fixa |
| `url` | URL pedida (base configurada + caminho) |
| `http_status` | da resposta usada; sempre `200` (um `304` é baixado de novo) |
| `etag`, `last_modified` | validadores HTTP da resposta, **ausentes** quando o servidor não mandou (os CSVs não têm `ETag`); reenviados no GET condicional da próxima verificação |
| `sha256`, `bytes` | do conteúdo baixado |
| `changed` | `true` se o SHA-256 difere do mesmo arquivo no último dataset aplicado |
| `rows` | registros gerados pelo parser; **ausente** quando o parser recusou o dataset |
| `publication` | só nos `rdap-*`: campo `publication` do JSON, RFC 3339 em UTC; ausente se o JSON não traz ou é ilegível |

`changes` tem sempre as 5 tabelas, cada uma com `inserted`, `updated` e
`deleted`. Como é `jsonb`, as chaves saem ordenadas por tamanho; na primeira
carga do dataset de 2026-09-28:

```json
{"iana_asn_block": {"deleted": 0, "updated": 0, "inserted": 173},
 "iana_special_asn": {"deleted": 0, "updated": 0, "inserted": 9},
 "iana_prefix_block": {"deleted": 0, "updated": 0, "inserted": 307},
 "iana_rdap_service": {"deleted": 0, "updated": 0, "inserted": 414},
 "iana_special_prefix": {"deleted": 0, "updated": 0, "inserted": 51}}
```

## `iana_asn_block`

Comentário: blocos de ASN do registro da IANA — a quem cada faixa foi
entregue (um dos 5 RIRs) ou por que está reservada. Junta `as-numbers-1.csv`
(16 bits, 0–65535) e `as-numbers-2.csv` (32 bits, 65536–4294967295); a
linha "See Sub-registry" do segundo é ignorada. As faixas não se sobrepõem e
cobrem 0–4294967295 sem buracos (conferido pela
[sanidade](fonte.md#sanidade-do-dataset-parsecheck)).

| Coluna | Tipo | Nulo / padrão | Origem |
|---|---|---|---|
| `asn_start` | `bigint` | `NOT NULL` | `Number`: `1-1876` → 1; `2043` → 2043 |
| `asn_end` | `bigint` | `NOT NULL` | fim da faixa, inclusive (= `asn_start` para um ASN só) |
| `description` | `text` | `NOT NULL` | `Description` normalizada: `Assigned by ARIN`, `Reserved`, `Unallocated`, `AS_TRANS`, `Reserved for Private Use`... |
| `registry` | `text` | NULL | RIR do domínio do `WHOIS` (`whois.arin.net` → `arin`) ou, sem ele, da descrição (`Assigned by RIPE NCC` → `ripencc`); NULL = faixa sem RIR (reservada, não alocada, AS_TRANS) |
| `whois` | `text` | NULL | `WHOIS` em minúsculas |
| `rdap_urls` | `text[]` | `NOT NULL DEFAULT '{}'` | `RDAP`, com as URLs coladas separadas; `{}` = sem RDAP |
| `reference` | `text` | NULL | `Reference` (`[RFC6996]`) |
| `registration_date` | `text` | NULL | `Registration Date`, `AAAA-MM` ou `AAAA-MM-DD`; NULL vazio ou irreconhecível |
| `source_file` | `text` | `NOT NULL` | `as-numbers-1` (16 bits) ou `as-numbers-2` (32 bits) |

| Constraint | Regra |
|---|---|
| `uq_iana_asn_block_asn_start` | `UNIQUE (asn_start)` — é também o índice de ASN → bloco |
| `chk_iana_asn_block_asn_start` | `asn_start BETWEEN 0 AND 4294967295` |
| `chk_iana_asn_block_asn_end` | `asn_end BETWEEN asn_start AND 4294967295` |
| `chk_iana_asn_block_registry` | `registry IN (...)` (os 5 RIRs) |
| `chk_iana_asn_block_date` | `registration_date` no formato de data |
| `chk_iana_asn_block_source` | `source_file IN ('as-numbers-1', 'as-numbers-2')` |

Trigger `trg_iana_asn_block_updated_at`.

## `iana_prefix_block`

Comentário: blocos de endereço do registro da IANA — os 256 `/8` do IPv4
(`ipv4-address-space.csv`, sempre 256 linhas) e os blocos IPv6 unicast
(`ipv6-unicast-address-assignments.csv`, dentro de `2000::/3`), com RIR,
status e data. Blocos legados podem ser administrados por um RIR e
delegados em parte por outro: o RIR daqui é o da coluna WHOIS da IANA.

| Coluna | Tipo | Nulo / padrão | Origem |
|---|---|---|---|
| `prefix` | `cidr` | `NOT NULL` | `Prefix` em CIDR canônico: `000/8` → `0.0.0.0/8`, `045/8` → `45.0.0.0/8` |
| `family` | `smallint` | `NOT NULL GENERATED ALWAYS AS (family(prefix)) STORED` | 4 ou 6, calculada pelo banco |
| `designation` | `text` | `NOT NULL` | `Designation`: RIR (`APNIC`, `RIPE NCC`), `Administered by ARIN`, titular legado (`Apple Computer Inc.`, `US-DOD`), `Multicast`, `Future use`, `IANA - Private Use`... |
| `registry` | `text` | NULL | RIR do `WHOIS` (`whois.ripe.net` → `ripencc`) ou da designação; NULL = sem RIR (reservado, multicast, 6to4, IANA) |
| `whois` | `text` | NULL | `WHOIS` (`whois.arin.net`, `whois.iana.org`) |
| `rdap_urls` | `text[]` | `NOT NULL DEFAULT '{}'` | `RDAP`, com as URLs coladas separadas |
| `status` | `text` | `NOT NULL` | `Status` em maiúsculas: `ALLOCATED` (entregue a um RIR), `LEGACY` (anterior aos RIRs, hoje administrado por um), `RESERVED` (reservado pela IANA/IETF) |
| `allocation_date` | `text` | NULL | `Date`: `AAAA-MM` (IPv4) ou `AAAA-MM-DD` (quase todo o IPv6) |
| `note` | `text` | NULL | `Note` com brancos normalizados; no IPv4 só as marcas `[n]` (`[2][3]`), no IPv6 texto |
| `source_file` | `text` | `NOT NULL` | `ipv4-address-space` ou `ipv6-unicast-address-assignments`, coerente com `family` |

| Constraint | Regra |
|---|---|
| `uq_iana_prefix_block_prefix` | `UNIQUE (prefix)` |
| `chk_iana_prefix_block_registry` | `registry IN (...)` (os 5 RIRs) |
| `chk_iana_prefix_block_status` | `status ~ '^[A-Z][A-Z _-]*$'` |
| `chk_iana_prefix_block_date` | `allocation_date` no formato de data |
| `chk_iana_prefix_block_source` | `(source_file = 'ipv4-address-space' AND family(prefix) = 4) OR (source_file = 'ipv6-unicast-address-assignments' AND family(prefix) = 6)` |

Índice `ix_iana_prefix_block_prefix_gist` — `USING gist (prefix inet_ops)`:
bloco que contém um IP ou prefixo (`>>=`) e blocos que o cruzam (`&&`).
Trigger `trg_iana_prefix_block_updated_at`.

## `iana_special_prefix`

Comentário: blocos IPv4 e IPv6 de uso especial (RFC 6890) — privados,
loopback, documentação, link-local, CGNAT, benchmarking... —, a base da
marcação de bogon. Vem dos dois special-purpose registries, uma linha por
bloco: uma célula com vários blocos vira várias linhas com os mesmos
atributos, e as notas de rodapé são retiradas. Blocos aninhados são normais
(`0.0.0.0/8` e `0.0.0.0/32`): um IP pode estar em mais de um.

| Coluna | Tipo | Nulo / padrão | Origem |
|---|---|---|---|
| `prefix` | `cidr` | `NOT NULL` | `Address Block` sem a nota de rodapé, CIDR canônico |
| `family` | `smallint` | `NOT NULL GENERATED ALWAYS AS (family(prefix)) STORED` | 4 ou 6; indica também o arquivo |
| `name` | `text` | `NOT NULL` | `Name` (`Private-Use`, `Loopback`, `"This network"` com as aspas da IANA) |
| `rfc` | `text` | NULL | `RFC` normalizada (`[RFC8190] [RFC919], Section 7`) |
| `allocation_date` | `text` | NULL | `Allocation Date` |
| `termination_date` | `text` | NULL | `Termination Date`; NULL = em vigor (a IANA publica `N/A`); preenchida = registro encerrado (`192.88.99.0/24`, 6to4 relay anycast) |
| `source` | `boolean` | NULL | `Source`: pode ser endereço de origem |
| `destination` | `boolean` | NULL | `Destination`: pode ser endereço de destino |
| `forwardable` | `boolean` | NULL | `Forwardable`: roteadores podem encaminhar |
| `globally_reachable` | `boolean` | NULL | `Globally Reachable`: `false` = não deve aparecer na Internet pública (bogon) |
| `reserved_by_protocol` | `boolean` | NULL | `Reserved-by-Protocol`: reservado pela especificação do IP |

Nas 5 flags, NULL = a IANA publica vazio ou `N/A` (registros encerrados;
TEREDO e 6to4 em `globally_reachable`, `N/A [2]`/`N/A [3]`).

| Constraint | Regra |
|---|---|
| `uq_iana_special_prefix_prefix` | `UNIQUE (prefix)` |
| `chk_iana_special_prefix_allocation` | `allocation_date` no formato de data |
| `chk_iana_special_prefix_termination` | `termination_date` no formato de data |

Índice `ix_iana_special_prefix_prefix_gist` — `USING gist (prefix inet_ops)`:
especiais que contêm ou cruzam um IP ou prefixo (`>>=`, `<<=`, `&&`).
Trigger `trg_iana_special_prefix_updated_at`.

## `iana_special_asn`

Comentário: ASNs de uso especial (`special-purpose-as-numbers.csv`): 0,
AS112, AS_TRANS (23456), documentação, uso privado, 65535 e 4294967295.

| Coluna | Tipo | Nulo / padrão | Origem |
|---|---|---|---|
| `asn_start` | `bigint` | `NOT NULL` | `AS Number`: `64512-65534` → 64512 |
| `asn_end` | `bigint` | `NOT NULL` | fim da faixa, inclusive |
| `reason` | `text` | `NOT NULL` | `Reason for Reservation` (`For private use; reserved by [RFC6996]`) |
| `reference` | `text` | NULL | `Reference` (`[RFC6996]`) |

| Constraint | Regra |
|---|---|
| `uq_iana_special_asn_asn_start` | `UNIQUE (asn_start)` |
| `chk_iana_special_asn_asn_start` | `asn_start BETWEEN 0 AND 4294967295` |
| `chk_iana_special_asn_asn_end` | `asn_end BETWEEN asn_start AND 4294967295` |

Sem outro índice (9 linhas). Trigger `trg_iana_special_asn_updated_at`.

## `iana_rdap_service`

Comentário: bootstrap RDAP da IANA (RFC 9224; `asn.json`, `ipv4.json`,
`ipv6.json`) — qual servidor RDAP responde por cada faixa de ASN ou bloco
IP. Uma linha por entrada de `services[][0]`; a data de publicação de cada
JSON fica em `iana_run.files` (`publication`).

| Coluna | Tipo | Nulo / padrão | Origem |
|---|---|---|---|
| `kind` | `text` | `NOT NULL` | arquivo: `asn` (`asn.json`), `ipv4`, `ipv6` |
| `resource` | `text` | `NOT NULL` | entrada canônica: `1-1876`, `2043` ou o CIDR `41.0.0.0/8` |
| `asn_start`, `asn_end` | `bigint` | NULL | faixa, só em `kind = 'asn'` |
| `prefix` | `cidr` | NULL | bloco, só em `ipv4`/`ipv6` |
| `registry` | `text` | NULL | RIR do domínio da primeira URL de RIR; NULL = servidor de outro operador |
| `urls` | `text[]` | `NOT NULL` (sem padrão) | URLs do serviço na ordem publicada (a https primeiro; às vezes também a http) |

| Constraint | Regra |
|---|---|
| `uq_iana_rdap_service_resource` | `UNIQUE (kind, resource)` |
| `chk_iana_rdap_service_kind` | `kind IN ('asn', 'ipv4', 'ipv6')` |
| `chk_iana_rdap_service_target` | abaixo |
| `chk_iana_rdap_service_registry` | `registry IN (...)` (os 5 RIRs) |
| `chk_iana_rdap_service_urls` | `cardinality(urls) > 0` |

```sql
CONSTRAINT chk_iana_rdap_service_target CHECK (
    (kind = 'asn'  AND asn_start IS NOT NULL AND asn_end IS NOT NULL AND prefix IS NULL
                   AND asn_start >= 0 AND asn_end BETWEEN asn_start AND 4294967295) OR
    (kind = 'ipv4' AND prefix IS NOT NULL AND family(prefix) = 4 AND asn_start IS NULL AND asn_end IS NULL) OR
    (kind = 'ipv6' AND prefix IS NOT NULL AND family(prefix) = 6 AND asn_start IS NULL AND asn_end IS NULL))
```

| Índice | Método | Colunas | Condição | Uso |
|---|---|---|---|---|
| `ix_iana_rdap_service_prefix_gist` | gist | `prefix inet_ops` | — | servidor RDAP de um IP ou prefixo |
| `ix_iana_rdap_service_asn` | btree | `asn_start` | `WHERE kind = 'asn'` | servidor RDAP de um ASN |

Trigger `trg_iana_rdap_service_updated_at`.

## Consultas da api-iana

A `api-iana` só lê (`apps/iana/api/internal/store/`). As consultas de
`/asn`, `/ip`, `/prefix`, `/special` e `/rdap` rodam numa transação só de
leitura `REPEATABLE READ`: como o coletor aplica os 10 arquivos numa
transação, a resposta nunca mistura dois datasets. Rotas e respostas:
[api.md](api.md).

| Pergunta | Consulta | Índice |
|---|---|---|
| Versão do dataset, `/meta`, `publication` de `/rdap` | `SELECT uuid::text, created_at, sha256, files FROM iana_run WHERE status = 1 ORDER BY created_at DESC LIMIT 1` | `ix_iana_run_applied` |
| Estado do coletor (`/meta`) | `SELECT last_sync_at, last_check_at, consolidated FROM jobs WHERE app = 'collector-iana'` | `uq_jobs_app` |
| ASN → bloco | `iana_asn_block WHERE asn_start <= $1 AND asn_end >= $1 ORDER BY asn_start DESC LIMIT 1` | `uq_iana_asn_block_asn_start` |
| ASN → especiais | `iana_special_asn WHERE asn_start <= $1 AND asn_end >= $1 ORDER BY asn_start` (pode haver mais de um) | tabela de 9 linhas |
| ASN → RDAP | `iana_rdap_service WHERE kind = 'asn' AND asn_start <= $1 AND asn_end >= $1 ORDER BY asn_start DESC LIMIT 1` | `ix_iana_rdap_service_asn` |
| IP/prefixo → bloco | `iana_prefix_block WHERE prefix >>= $1::cidr ORDER BY masklen(prefix) DESC LIMIT 1` | `ix_iana_prefix_block_prefix_gist` |
| IP/prefixo → especiais | `iana_special_prefix WHERE prefix >>= $1::cidr ORDER BY masklen(prefix) DESC` (todos, do mais específico ao menos) | `ix_iana_special_prefix_prefix_gist` |
| IP/prefixo → RDAP | `iana_rdap_service WHERE kind = $2 AND prefix >>= $1::cidr ORDER BY masklen(prefix) DESC LIMIT 1` (`$2` = `ipv4`/`ipv6`) | `ix_iana_rdap_service_prefix_gist` |
| Algum bloco não reservado cruza a consulta? (bogon) | `SELECT EXISTS (SELECT 1 FROM iana_prefix_block WHERE prefix && $1::cidr AND status <> 'RESERVED')` | `ix_iana_prefix_block_prefix_gist` |
| `/asns` | `iana_asn_block ORDER BY asn_start` | `uq_iana_asn_block_asn_start` |
| `/ipv4`, `/ipv6` | `iana_prefix_block WHERE family = $1 ORDER BY prefix` | — (307 linhas) |
| `/special` | `iana_special_prefix ORDER BY family, prefix` e `iana_special_asn ORDER BY asn_start` | — |
| `/rdap` | `iana_rdap_service ORDER BY CASE kind WHEN 'asn' THEN 0 WHEN 'ipv4' THEN 1 ELSE 2 END, asn_start, prefix` | — |

Colunas lidas: `iana_asn_block` (`asn_start`, `asn_end`, `description`,
`registry`, `whois`, `rdap_urls`, `reference`, `registration_date`),
`iana_prefix_block` (`prefix`, `designation`, `registry`, `whois`,
`rdap_urls`, `status`, `allocation_date`, `note`), `iana_special_prefix`
(`prefix` e todas as colunas de dado), `iana_special_asn` (`asn_start`,
`asn_end`, `reason`, `reference`), `iana_rdap_service` (`kind`, `resource`,
`registry`, `urls`) e, de `iana_run.files`, todas as chaves (a `/rdap` usa
`publication` de `rdap-asn`, `rdap-ipv4` e `rdap-ipv6`).

### Bogon

A regra de bogon é da `api-iana` ([api.md](api.md#regra-de-bogon)) e usa só
estes dados: decide o `iana_special_prefix` **em vigor** (`termination_date`
NULL) **mais específico** que contém a consulta, quando ele tem
`globally_reachable` preenchido (`false` = bogon, `true` = não); se ele tem
`globally_reachable` NULL (TEREDO `2001::/32`, 6to4 `2002::/16`) ou não há
nenhum em vigor, decide `iana_prefix_block`: bogon quando nenhum bloco que
cruza a consulta tem `status <> 'RESERVED'` — só blocos `RESERVED` (IPv4:
`0/8`, `10/8`, `127/8`, multicast 224–239 e "Future use" 240–255) ou nenhum
bloco (IPv6 não entregue). Registros encerrados não contam, quaisquer que
sejam as flags.

## Mudando o schema

1. `make -C database/postgres new APP=iana NAME=descricao` e escreva
   `migrate:up`/`migrate:down` no estilo de
   [../../plataforma/postgres.md](../../plataforma/postgres.md#estilo-das-tabelas);
   teste com `make -C database/postgres test`.
2. Atualize `internal/store` do coletor (`tables`, o SQL de `stage` e as
   linhas do `COPY`), o teste de integração e este arquivo.
3. Coluna que a API lê, ou mudança em `iana_run.files`/`changes`: avise o
   sub-agente `api-iana` (a `api.md` e o `testdata/seed.sql` dela mudam
   junto).
