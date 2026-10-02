# Dados (modelo dos RIRs)

Cada RIR tem as mesmas três tabelas, com o nome dele no prefixo: `<rir>_asn`,
`<rir>_prefix` e `<rir>_run`. Escreve o `collector-<rir>`; lê a `api-<rir>`.
Este arquivo basta para reescrever a migration; o estilo (colunas alinhadas,
constraints nomeadas, `COMMENT ON` em tudo) é o de
[../../plataforma/postgres.md](../../plataforma/postgres.md#estilo-das-tabelas),
e a tabela `jobs`, de [../../plataforma/postgres.md](../../plataforma/postgres.md#tabela-jobs).

| Tabela | Uma linha por | Chave natural |
|---|---|---|
| `<rir>_asn` | registro `type = asn` (uma faixa de ASNs) | `asn_start` |
| `<rir>_prefix` | bloco CIDR de um registro `ipv4`/`ipv6` | `prefix` |
| `<rir>_run` | execução que baixou um arquivo novo (aplicado ou recusado) | — |

- As tabelas espelham o **último arquivo aplicado**: o que some da fonte é
  apagado (sem soft delete); histórico e consolidação são da fase 2.
- ASNs e blocos são **independentes** (sem FK): no arquivo do RIR eles não
  têm ligação; o que une os recursos de um titular é o `opaque_id`, indexado
  nas duas tabelas.
- A aplicação é numa transação só: a API nunca vê um arquivo pela metade.

## Migration

`database/postgres/<rir>/20260929000000_<rir>.sql`, uma por RIR. As cinco têm
o **mesmo DDL** a menos do nome (conferido em 2026-09-29, trocando o nome e
comparando sem os comentários); só os textos de comentário diferem, com notas
do RIR (ex.: `ZZ` na AFRINIC, serial em ms na ARIN, opaque-id diário no RIPE
NCC) — cada `specs/fontes/<rir>/dados.md` lista as suas.

- `-- migrate:up`, depois o cabeçalho, com: o título
  (`<rir>: delegações de ASNs e blocos IP publicadas pela <Title>`),
  `Depende de: central (função set_updated_at)`, a lista das três tabelas
  (cada uma "tabela independente"), quem escreve e quem lê, a URL da fonte, a
  forma do registro (`registry|cc|type|start|value|date|status|opaque-id`) e
  as duas notas acima (espelho do último arquivo; ASNs e blocos ligados só
  pelo `opaque_id`).
- Ordem: `<rir>_run` e o índice dela; `<rir>_asn`, comentários, trigger e
  índice; `<rir>_prefix`, comentários, trigger e índices. Antes de cada índice,
  um comentário com a consulta da API que ele atende.
- `-- migrate:down`: `DROP TABLE <rir>_prefix;`, `DROP TABLE <rir>_asn;`,
  `DROP TABLE <rir>_run;` (índices e triggers vão junto).

## `<rir>_run`

Histórico das execuções que baixaram um arquivo novo (aplicado ou recusado).
Verificações sem mudança, inclusive "arquivo mais antigo que o aplicado", não
geram linha. A última linha `status = 1` descreve o dataset que está em
`<rir>_asn`/`<rir>_prefix`, e o `uuid` dela é a versão do dataset (cache e
ETag da API). Só recebe `INSERT`; não tem `updated_at`.

| Coluna | Tipo | Nulo | Padrão | `COMMENT ON` (resumo) |
|---|---|---|---|---|
| `uuid` | `uuid` | PK | `uuidv7()` | UUIDv7; com `status = 1`, a versão do dataset |
| `status` | `smallint` | NOT NULL | | `0` = falhou (conferência do MD5, parser ou trava recusaram; tabelas intactas), `1` = aplicado |
| `forced` | `boolean` | NOT NULL | `false` | veio de `--force` (ignora "igual", "mais antigo" e a trava de remoção) |
| `url` | `text` | NOT NULL | | de onde o arquivo foi baixado |
| `http_status` | `smallint` | sim | | status HTTP do arquivo; NULL = sem resposta |
| `etag` | `text` | sim | | `ETag` da resposta, reenviado em `If-None-Match` |
| `last_modified` | `text` | sim | | `Last-Modified`, como texto HTTP, reenviado em `If-Modified-Since` |
| `md5` | `text` | sim | | MD5 hex minúsculo do arquivo; comparado com o `.md5` publicado |
| `sha256` | `text` | sim | | SHA-256 hex minúsculo; detecta conteúdo igual sem o `.md5` |
| `bytes` | `bigint` | sim | | tamanho baixado, em bytes |
| `format_version` | `text` | sim | | `version` do cabeçalho; NULL = cabeçalho não lido |
| `serial` | `text` | sim | | `serial` como publicado (data ou época, conforme o RIR); com `end_date`, recusa arquivo mais antigo |
| `header_records` | `integer` | sim | | `records` do cabeçalho |
| `start_date` | `date` | sim | | `startdate`; NULL = vazio ou `00000000` |
| `end_date` | `date` | sim | | `enddate`; com `serial`, recusa arquivo mais antigo |
| `utc_offset` | `text` | sim | | `UTCoffset`, como publicado |
| `asn_records` | `integer` | sim | | registros `asn` aceitos (= linhas de `<rir>_asn`) |
| `ipv4_records` | `integer` | sim | | registros `ipv4` aceitos, antes da divisão em CIDRs |
| `ipv6_records` | `integer` | sim | | registros `ipv6` aceitos |
| `prefixes_v4` | `integer` | sim | | blocos IPv4 gravados, depois da divisão |
| `prefixes_v6` | `integer` | sim | | blocos IPv6 gravados |
| `asn_inserted`, `asn_updated`, `asn_deleted` | `integer` | sim | | registros de ASN novos / com quantidade, país, data, status ou titular alterados / apagados; NULL quando `status = 0` |
| `prefix_inserted`, `prefix_updated`, `prefix_deleted` | `integer` | sim | | blocos novos / com país, data, status, titular ou registro de origem alterados / apagados; NULL quando `status = 0` |
| `warnings` | `jsonb` | NOT NULL | `'[]'::jsonb` | array de strings com os avisos do parser, limitado às primeiras ocorrências |
| `error` | `text` | sim | | mensagem da recusa; NULL quando aplicado |
| `started_at` | `timestamptz` | NOT NULL | | início da execução (antes do download) |
| `created_at` | `timestamptz` | NOT NULL | `NOW()` | gravação da linha, no fim da execução |

Constraints (a PK fica sem nome explícito):

| Nome | Regra |
|---|---|
| `chk_<rir>_run_status` | `CHECK (status BETWEEN 0 AND 1)` |
| `chk_<rir>_run_md5` | `CHECK (md5 ~ '^[0-9a-f]{32}$')` |
| `chk_<rir>_run_sha256` | `CHECK (sha256 ~ '^[0-9a-f]{64}$')` |

Índice `ix_<rir>_run_applied`: btree `(created_at DESC) WHERE status = 1` — a
última execução aplicada, para o coletor e para a API.

Como o coletor preenche ([collector.md](collector.md#aplicação)): a linha só é
gravada depois de um download, então `url`, `http_status`, `md5`, `sha256` e
`bytes` vêm sempre; `etag`/`last_modified` ficam NULL se o servidor não os
mandou. Cabeçalho (`format_version` a `utc_offset`) e contagens do arquivo
(`asn_records` a `prefixes_v6`) ficam NULL quando o parser não aceitou o
arquivo (MD5 divergente ou recusa do parser); as alterações, quando
`status = 0`; `warnings` é `[]` sem avisos.

## `<rir>_asn`

Uma linha por registro de ASN. Um registro cobre a faixa `asn_start`..`asn_end`
(a maioria tem um ASN só). As faixas não se sobrepõem na fonte, então o
registro que contém um ASN é o de maior `asn_start` ≤ ASN, conferindo
`asn_end` ≥ ASN.

| Coluna | Tipo | Nulo | Padrão / expressão | `COMMENT ON` (resumo) |
|---|---|---|---|---|
| `uuid` | `uuid` | PK | `uuidv7()` | UUIDv7 |
| `asn_start` | `bigint` | NOT NULL | | primeiro ASN da faixa (campo `start`), 0 a 4294967295; chave natural |
| `asn_count` | `bigint` | NOT NULL | | quantidade de ASNs (campo `value`), no mínimo 1 |
| `asn_end` | `bigint` | NOT NULL | `GENERATED ALWAYS AS (asn_start + asn_count - 1) STORED` | último ASN da faixa, calculado pelo banco |
| `cc` | `text` | sim | | país (ISO 3166-1 alfa-2, maiúsculo; `ZZ` como publicado); NULL = vazio na fonte (comum em available/reserved) |
| `reg_date` | `date` | sim | | data da alocação/designação; NULL = vazio ou `00000000` (registros antigos, available, reserved) |
| `status` | `text` | NOT NULL | | `allocated` = alocado a um LIR/ISP, `assigned` = designado a um usuário final, `available` = livre no estoque do RIR, `reserved` = reservado pelo RIR |
| `opaque_id` | `text` | sim | | titular dentro do RIR; liga ASNs e blocos (`<rir>_prefix.opaque_id`); não é documento nem nome; NULL = vazio (available/reserved) |
| `created_at` | `timestamptz` | NOT NULL | `NOW()` | primeira vez que o registro apareceu na fonte |
| `updated_at` | `timestamptz` | NOT NULL | `NOW()` | última alteração, mantida pelo trigger |

| Nome | Tipo | Regra |
|---|---|---|
| `uq_<rir>_asn_asn_start` | unique | `UNIQUE (asn_start)` — o índice dele atende a consulta por ASN, sem índice extra |
| `chk_<rir>_asn_range` | check | `asn_start >= 0 AND asn_count >= 1 AND asn_start + asn_count - 1 <= 4294967295` |
| `chk_<rir>_asn_cc` | check | `cc ~ '^[A-Z]{2}$'` |
| `chk_<rir>_asn_status` | check | `status IN ('allocated', 'assigned', 'available', 'reserved')` |
| `ix_<rir>_asn_opaque_id` | índice btree | `(opaque_id)` — recursos de um titular |
| `trg_<rir>_asn_updated_at` | trigger | `BEFORE UPDATE ... FOR EACH ROW EXECUTE FUNCTION set_updated_at()` |

## `<rir>_prefix`

Blocos IPv4 e IPv6, um CIDR por linha. Registro IPv6 vira um bloco
(`value` = tamanho do prefixo). Registro IPv4 que não forma um CIDR é dividido
nos CIDRs mínimos (ex.: `62.122.208.0` + 1280 = `/22` + `/24`), e cada pedaço
repete os dados do registro; `record_start` e `record_value` guardam o
registro original.

| Coluna | Tipo | Nulo | Padrão / expressão | `COMMENT ON` (resumo) |
|---|---|---|---|---|
| `uuid` | `uuid` | PK | `uuidv7()` | UUIDv7 |
| `prefix` | `cidr` | NOT NULL | | bloco em notação CIDR canônica; chave natural |
| `family` | `smallint` | NOT NULL | `GENERATED ALWAYS AS (family(prefix)) STORED` | `4` = IPv4, `6` = IPv6, calculada pelo banco |
| `cc` | `text` | sim | | como em `<rir>_asn` |
| `reg_date` | `date` | sim | | data do registro de origem; como em `<rir>_asn` |
| `status` | `text` | NOT NULL | | como em `<rir>_asn` |
| `opaque_id` | `text` | sim | | liga blocos e ASNs (`<rir>_asn.opaque_id`); como em `<rir>_asn` |
| `record_start` | `inet` | NOT NULL | | endereço inicial do registro de origem (campo `start`); igual ao de `prefix` quando o registro formava um CIDR |
| `record_value` | `bigint` | NOT NULL | | campo `value` do registro de origem: endereços (IPv4) ou tamanho do prefixo (IPv6); os blocos com o mesmo `record_start` formam o registro original |
| `created_at` | `timestamptz` | NOT NULL | `NOW()` | primeira vez que o bloco apareceu na fonte |
| `updated_at` | `timestamptz` | NOT NULL | `NOW()` | última alteração, mantida pelo trigger |

| Nome | Tipo | Regra |
|---|---|---|
| `uq_<rir>_prefix_prefix` | unique | `UNIQUE (prefix)` |
| `chk_<rir>_prefix_cc` | check | `cc ~ '^[A-Z]{2}$'` |
| `chk_<rir>_prefix_status` | check | `status IN ('allocated', 'assigned', 'available', 'reserved')` |
| `chk_<rir>_prefix_record` | check | `family(record_start) = family(prefix) AND record_value >= 1` |
| `ix_<rir>_prefix_prefix_gist` | índice GiST | `(prefix inet_ops)` — bloco mais específico que contém um IP ou prefixo (`>>=`) |
| `ix_<rir>_prefix_opaque_id` | índice btree | `(opaque_id)` — recursos de um titular |
| `trg_<rir>_prefix_updated_at` | trigger | `BEFORE UPDATE ... FOR EACH ROW EXECUTE FUNCTION set_updated_at()` |

## Mapeamento arquivo → colunas

| Campo do registro | `<rir>_asn` | `<rir>_prefix` |
|---|---|---|
| `cc` | `cc` (vazio → NULL; `ZZ`/`EU` como vieram) | `cc` (idem) |
| `start` | `asn_start` | `record_start` (e o endereço de `prefix`) |
| `value` | `asn_count` (`asn_end` calculado pelo banco) | `record_value`; IPv4: os CIDRs mínimos da faixa, um por linha; IPv6: `prefix` = `start/value` |
| `date` | `reg_date` (vazia/`00000000`/inválida → NULL) | `reg_date` |
| `status` | `status` | `status` |
| `opaque-id` | `opaque_id` (vazio ou ausente → NULL) | `opaque_id` |

`family` é calculado pelo banco. Do cabeçalho vão para `<rir>_run`:
`version` → `format_version`, `serial` → `serial`, `records` →
`header_records`, `startdate` → `start_date`, `enddate` → `end_date`,
`UTCoffset` → `utc_offset`. As regras de cada campo estão em
[formato.md](formato.md#normalização).

## Consultas da API

As que a `api-<rir>` faz (código da `api-lacnic`, conferido em 2026-09-29).
Todas usam índice; no arquivo real, `TestApplyRealFile` do coletor confere
com `EXPLAIN` a consulta por ASN, a por IP e a por titular em `<rir>_prefix`
([collector.md](collector.md#testes)), e o `TestRealFile` da API confere as
mesmas e a por titular em `<rir>_asn`
([api.md](api.md#arquivo-real-make-test-real)).

```sql
-- Versão do dataset e /meta (ix_<rir>_run_applied)
SELECT uuid::text, created_at, url, sha256, md5, serial, start_date, end_date,
       asn_records, ipv4_records, ipv6_records, prefixes_v4, prefixes_v6
  FROM <rir>_run WHERE status = 1 ORDER BY created_at DESC LIMIT 1;

-- Estado do coletor em /meta (uq_jobs_app)
SELECT last_sync_at, last_check_at, consolidated FROM jobs WHERE app = $1;  -- 'collector-<rir>'

-- ASN → registro que o contém (índice de uq_<rir>_asn_asn_start); a API
-- confere asn_end >= $1 (senão, o ASN não está no arquivo)
SELECT asn_start, asn_end, asn_count, cc, reg_date, status, opaque_id, created_at, updated_at
  FROM <rir>_asn WHERE asn_start <= $1 ORDER BY asn_start DESC LIMIT 1;

-- IP ou prefixo → bloco mais específico que o contém (ix_<rir>_prefix_prefix_gist);
-- a API zera os bits de host antes (o tipo cidr os recusa)
SELECT prefix, cc, reg_date, status, opaque_id, host(record_start), record_value, created_at, updated_at
  FROM <rir>_prefix WHERE prefix >>= $1::cidr ORDER BY masklen(prefix) DESC LIMIT 1;

-- Titular: acha o opaque_id gravado entre o pedido, em maiúsculas e em
-- minúsculas ($1), preferindo o exato ($2) (ix_<rir>_asn_opaque_id, ix_<rir>_prefix_opaque_id)
WITH ids AS (
    SELECT opaque_id FROM <rir>_asn    WHERE opaque_id = ANY($1::text[])
    UNION
    SELECT opaque_id FROM <rir>_prefix WHERE opaque_id = ANY($1::text[])
)
SELECT opaque_id FROM ids ORDER BY opaque_id = $2 DESC, opaque_id LIMIT 1;
-- ... e todos os recursos dele
SELECT asn_start, asn_end, asn_count, cc, reg_date, status, opaque_id, created_at, updated_at
  FROM <rir>_asn WHERE opaque_id = $1 ORDER BY asn_start;
SELECT prefix, cc, reg_date, status, opaque_id, host(record_start), record_value, created_at, updated_at
  FROM <rir>_prefix WHERE opaque_id = $1 ORDER BY family, prefix;
```

O coletor lê a última aplicada pelo mesmo índice:

```sql
SELECT uuid::text, url, coalesce(etag, ''), coalesce(last_modified, ''),
       coalesce(md5, ''), coalesce(sha256, ''), coalesce(serial, ''), end_date, created_at
  FROM <rir>_run WHERE status = 1 ORDER BY created_at DESC LIMIT 1;
```

Um registro IPv4 dividido aparece como vários blocos com o mesmo
`record_start`/`record_value`: para mostrar o registro original, agrupe por
eles. O que a API faz com cada resultado está em [api.md](api.md).

## Mudar o schema da família

1. Spec primeiro: este arquivo (e o `dados.md` do RIR, se o texto de um
   comentário for dele).
2. As **cinco migrations mudam juntas**: uma migration nova em cada pasta
   (`make -C database/postgres new APP=<rir> NAME=<descricao>`), com o mesmo
   SQL e o nome trocado; `-- migrate:up` e `-- migrate:down` funcionais.
3. `make -C database/postgres test` (up → rollback de tudo → up).
4. `internal/store` e o teste de integração nos cinco coletores (modelo e
   clones, [README.md](README.md#manutenção-dos-clones)); se a API lê a
   mudança, o pedido vai para os sub-agentes das APIs, com [api.md](api.md).
5. Depois do deploy, `--force` em cada coletor se as linhas existentes
   precisam ser regravadas com o arquivo atual.
