# rootzone — dados

Tabelas `rootzone_*` no schema `public` do banco `badblock`. A migration
[`database/postgres/rootzone/20260930033621_rootzone.sql`](../../../database/postgres/rootzone/20260930033621_rootzone.sql)
implementa este arquivo; `jobs` e a função `set_updated_at()` são de
`database/postgres/central/` ([../../plataforma/postgres.md](../../plataforma/postgres.md#tabela-jobs)).
Estilo (nomes, `COMMENT ON` em toda tabela e coluna, UUIDv7, migrations):
[../../plataforma/postgres.md](../../plataforma/postgres.md#estilo-das-tabelas).

| Tabela | Uma linha por | Chave natural | Escreve | Lê |
|---|---|---|---|---|
| `rootzone_run` | execução que baixou um arquivo novo (aplicado ou recusado), com o SOA da versão | — | coletor (`INSERT`) | coletor e API |
| `rootzone_tld` | TLD delegado (dono de NS que não é a raiz) | `tld` | coletor (`MERGE`) | API |
| `rootzone_record` | RR da zona, menos RRSIG | `(owner, type, rdata)` | coletor (`MERGE`) | API |

As tabelas espelham a **última versão aplicada** da zona: uma RR ou um TLD
que some da fonte é apagado, sem soft delete (histórico e consolidação são
das tabelas centrais da fase 2). A aplicação é numa transação só, então a API
nunca vê uma zona pela metade ([collector.md](collector.md#aplicação)). As
três tabelas são independentes (sem FK): `rootzone_record.owner` de uma RR
de TLD é igual a `rootzone_tld.tld`, e o `rdata` de um NS é igual ao `owner`
do glue do servidor — é por essas igualdades que a API junta as tabelas.

## Migration

Um arquivo, `20260930033621_rootzone.sql` (dbmate). Cabeçalho: o que cria
(três tabelas independentes), de que depende (`central`, pela função
`set_updated_at`), quem escreve e quem lê, a URL da fonte, o formato da
linha, por que os RRSIG não são guardados, a normalização e o espelho da
última versão aplicada.

- `migrate:up`, nesta ordem: `rootzone_run` com os comentários e
  `ix_rootzone_run_applied`; `rootzone_tld` com os comentários e o trigger;
  `rootzone_record` com os comentários, o trigger e
  `ix_rootzone_record_type_rdata`.
- `migrate:down`: `DROP TABLE` de `rootzone_record`, `rootzone_tld` e
  `rootzone_run`.

## `rootzone_run`

Histórico das execuções que baixaram um arquivo novo, aplicado ou recusado
(molde em [../../padroes/coletor.md](../../padroes/coletor.md#tabela-fonte_run)).
Verificações sem mudança — MD5 publicado igual, HTTP 304, conteúdo igual ou
serial mais antigo que o aplicado — não geram linha: ficam só em
`jobs.last_check_at`. A última linha com `status = 1` descreve a zona que
está em `rootzone_tld` e `rootzone_record`, traz o **SOA** dela e o seu
`uuid` é a **versão do dataset** que a `api-rootzone` usa no cache e no ETag.

| Coluna | Tipo | Nulo | Padrão | Conteúdo (resumo do `COMMENT ON`) |
|---|---|---|---|---|
| `uuid` | `uuid` | não | `uuidv7()` | PK; nas linhas `status = 1`, a versão do dataset |
| `status` | `smallint` | não | — | `0` = recusado (as tabelas ficaram como estavam), `1` = aplicado |
| `forced` | `boolean` | não | `false` | execução com `--force` (ignora as checagens de mudança, inclusive o serial, e a trava de remoção) |
| `url` | `text` | não | — | URL de onde o arquivo foi baixado |
| `http_status` | `smallint` | sim | — | status HTTP da resposta do arquivo |
| `etag` | `text` | sim | — | `ETag` da resposta, como veio (com gzip, `"…-gzip"`); reenviado no `If-None-Match`, junto com a forma sem o sufixo |
| `last_modified` | `text` | sim | — | `Last-Modified` da resposta, como texto HTTP; reenviado em `If-Modified-Since` |
| `md5` | `text` | sim | — | MD5 (hex minúsculo) do arquivo descomprimido; conferido com o `.md5` publicado e comparado com ele na verificação seguinte |
| `sha256` | `text` | sim | — | SHA-256 (hex minúsculo) do arquivo descomprimido; comparado com o da última aplicação |
| `bytes` | `bigint` | sim | — | tamanho do arquivo descomprimido, em bytes |
| `serial` | `bigint` | sim | — | serial do SOA (`AAAAMMDDNN`, 32 bits sem sinal); NULL = o parser não rodou ou recusou o arquivo |
| `soa_mname` | `text` | sim | — | MNAME do SOA, normalizado (`a.root-servers.net`) |
| `soa_rname` | `text` | sim | — | RNAME do SOA, normalizado (`nstld.verisign-grs.com`) |
| `soa_refresh`, `soa_retry`, `soa_expire`, `soa_minimum` | `bigint` | sim | — | temporizadores do SOA, em segundos |
| `tlds` | `integer` | sim | — | TLDs delegados no arquivo |
| `records` | `integer` | sim | — | RRs guardáveis (todas menos RRSIG), sem repetidas |
| `rrsigs` | `integer` | sim | — | RRSIG aceitos: contados, não guardados |
| `type_counts` | `jsonb` | sim | — | objeto tipo → linhas aceitas, inclusive RRSIG (`{"NS": 7580, "A": 5940, …}`) |
| `tld_inserted`, `tld_updated`, `tld_deleted` | `integer` | sim | — | alterações em `rootzone_tld`; NULL quando `status = 0` |
| `record_inserted`, `record_updated`, `record_deleted` | `integer` | sim | — | alterações em `rootzone_record` (o `updated` é só mudança de TTL); NULL quando `status = 0` |
| `warnings` | `jsonb` | não | `'[]'::jsonb` | array de strings com os avisos do parser e o de serial igual (os 50 primeiros e `... e mais N avisos`) |
| `error` | `text` | sim | — | mensagem da recusa; NULL quando aplicado |
| `started_at` | `timestamptz` | não | — | início da verificação (antes do download) |
| `created_at` | `timestamptz` | não | `NOW()` | gravação da linha, no fim da execução |

`serial`, os `soa_*`, `tlds`, `records`, `rrsigs` e `type_counts` são
preenchidos juntos, sempre que o parser aceitou o arquivo (também nas recusas
depois dele: `MIN_TLDS`, trava de remoção, banco), e ficam todos NULL quando
o parser não rodou (MD5 divergente) ou recusou o arquivo. Os temporizadores
do SOA são `bigint` porque o SOA os define como inteiros de 32 bits sem
sinal.

| Constraint | Regra |
|---|---|
| `rootzone_run_pkey` | `PRIMARY KEY (uuid)` (declarada na coluna, nome dado pelo Postgres) |
| `chk_rootzone_run_status` | `CHECK (status BETWEEN 0 AND 1)` |
| `chk_rootzone_run_md5` | `CHECK (md5 ~ '^[0-9a-f]{32}$')` (NULL passa) |
| `chk_rootzone_run_sha256` | `CHECK (sha256 ~ '^[0-9a-f]{64}$')` (NULL passa) |
| `chk_rootzone_run_serial` | `CHECK (serial BETWEEN 0 AND 4294967295)` (NULL passa) |

| Índice | Método | Colunas | Condição | Para quê |
|---|---|---|---|---|
| `ix_rootzone_run_applied` | btree | `created_at DESC` | `WHERE status = 1` | última execução aplicada: validadores, MD5, SHA-256 e serial do coletor; versão do dataset, SOA e `/meta` da API |

Sem `updated_at` nem trigger: as linhas nunca são alteradas.

## `rootzone_tld`

Um TLD delegado por linha, com as contagens da delegação, para a listagem da
API sem agregar `rootzone_record`. Regras dos campos:
[fonte.md](fonte.md#tlds-rootzone_tld).

| Coluna | Tipo | Nulo | Padrão | Conteúdo (resumo do `COMMENT ON`) |
|---|---|---|---|---|
| `uuid` | `uuid` | não | `uuidv7()` | PK |
| `tld` | `text` | não | — | nome em ASCII, minúsculas, sem pontos (`br`, `xn--p1ai`); chave natural; é o `owner` das RRs da delegação em `rootzone_record` |
| `tld_unicode` | `text` | não | — | forma Unicode (punycode decodificado: `xn--p1ai` → `рф`); igual a `tld` nos ASCII |
| `nameservers` | `smallint` | não | — | NS da delegação |
| `nameservers_ipv4` | `smallint` | não | — | quantos desses servidores têm glue A na zona |
| `nameservers_ipv6` | `smallint` | não | — | quantos têm glue AAAA |
| `ds_records` | `smallint` | não | — | DS da delegação (0 = TLD sem DNSSEC) |
| `created_at` | `timestamptz` | não | `NOW()` | primeira vez que este banco viu o TLD |
| `updated_at` | `timestamptz` | não | `NOW()` | última alteração; mantido pelo trigger `trg_rootzone_tld_updated_at` |

| Constraint | Regra |
|---|---|
| `rootzone_tld_pkey` | `PRIMARY KEY (uuid)` |
| `uq_rootzone_tld_tld` | `UNIQUE (tld)` |
| `chk_rootzone_tld_tld` | `CHECK (tld ~ '^[a-z0-9_-]{1,63}$')` — um rótulo só |
| `chk_rootzone_tld_unicode` | `CHECK (tld_unicode <> '')` |
| `chk_rootzone_tld_ns` | `CHECK (nameservers > 0)` |
| `chk_rootzone_tld_ns_ipv4` | `CHECK (nameservers_ipv4 BETWEEN 0 AND nameservers)` |
| `chk_rootzone_tld_ns_ipv6` | `CHECK (nameservers_ipv6 BETWEEN 0 AND nameservers)` |
| `chk_rootzone_tld_ds_records` | `CHECK (ds_records >= 0)` |

Trigger `trg_rootzone_tld_updated_at` (`BEFORE UPDATE … EXECUTE FUNCTION
set_updated_at()`). Índice: só o único de `uq_rootzone_tld_tld`, que serve
`WHERE tld = $1` e a listagem `ORDER BY tld` (~1,4 mil linhas).

## `rootzone_record`

Uma RR da zona por linha, normalizada ([fonte.md](fonte.md#linhas-parseparse)):
todas as do arquivo menos RRSIG — o ápice (SOA, NS da raiz, DNSKEY, NSEC da
raiz, ZONEMD), as delegações (NS, DS, NSEC de cada TLD) e o glue A/AAAA.

| Coluna | Tipo | Nulo | Padrão | Conteúdo (resumo do `COMMENT ON`) |
|---|---|---|---|---|
| `uuid` | `uuid` | não | `uuidv7()` | PK |
| `owner` | `text` | não | — | dono em minúsculas, sem o ponto final; a raiz é `.` (`.`, `br`, `a.dns.br`) |
| `type` | `text` | não | — | `SOA`, `NS`, `A`, `AAAA`, `DS`, `DNSKEY`, `NSEC` ou `ZONEMD` (a classe é sempre `IN`) |
| `rdata` | `text` | não | — | rdata normalizado: NS = nome do servidor (igual ao `owner` do glue dele); A/AAAA = endereço canônico; DS = `keytag alg tipo DIGEST`; DNSKEY = `flags protocolo alg base64`; NSEC = `próximo TIPOS…`; SOA = `mname rname serial refresh retry expire minimum`; ZONEMD = `serial esquema alg DIGEST` |
| `ttl` | `integer` | não | — | TTL em segundos (0 a 2.147.483.647) |
| `created_at` | `timestamptz` | não | `NOW()` | primeira vez que este banco viu a RR |
| `updated_at` | `timestamptz` | não | `NOW()` | última alteração (só o TTL muda sem mudar a chave); trigger `trg_rootzone_record_updated_at` |

| Constraint | Regra |
|---|---|
| `rootzone_record_pkey` | `PRIMARY KEY (uuid)` |
| `uq_rootzone_record_owner_type_rdata` | `UNIQUE (owner, type, rdata)` |
| `chk_rootzone_record_owner` | `CHECK (owner <> '')` |
| `chk_rootzone_record_type` | `CHECK (type IN ('SOA', 'NS', 'A', 'AAAA', 'DS', 'DNSKEY', 'NSEC', 'ZONEMD'))` — RRSIG não entra |
| `chk_rootzone_record_rdata` | `CHECK (rdata <> '')` |
| `chk_rootzone_record_ttl` | `CHECK (ttl >= 0)` |

| Índice | Método | Definição | Consulta |
|---|---|---|---|
| `uq_rootzone_record_owner_type_rdata` | btree único (da constraint) | `(owner, type, rdata)` | RRs de um dono: `WHERE owner = $1 [AND type …]`; o glue na junção `g.owner = ns.rdata AND g.type IN ('A', 'AAAA')`. Não crie outro índice em `owner` |
| `ix_rootzone_record_type_rdata` | btree | `(type, rdata)` | busca reversa: os TLDs de um servidor (`type = 'NS' AND rdata = $1`) e o dono de um endereço (`type IN ('A', 'AAAA') AND rdata = $1`) |

Tamanho com o arquivo de 2026-09-29 (22.131 linhas, PG18 local, com os
índices): `rootzone_record` 5,8 MB, `rootzone_tld` 280 kB.

## Mapeamento da fonte para as colunas

`rootzone_record`, uma linha aceita do arquivo que não é RRSIG
([fonte.md](fonte.md#linhas-parseparse)):

| Origem | Coluna |
|---|---|
| 1º campo (dono), normalizado | `owner` |
| 4º campo (tipo), em maiúsculas | `type` |
| 5º campo em diante (rdata), normalizado por tipo | `rdata` |
| 2º campo (TTL) | `ttl` |
| 3º campo (classe) | — (sempre `IN`) |

`rootzone_tld`, um dono de NS que não é a raiz
([fonte.md](fonte.md#tlds-rootzone_tld)): `tld` = o dono; `tld_unicode` =
punycode decodificado; `nameservers`, `nameservers_ipv4`, `nameservers_ipv6`
e `ds_records` = as contagens.

`rootzone_run`, uma verificação que baixou um arquivo novo:

| Origem | Coluna |
|---|---|
| resultado (`1` aplicado, `0` recusado) e `--force` | `status`, `forced` |
| `SOURCE_URL` | `url` |
| resposta HTTP: status e cabeçalhos `ETag` e `Last-Modified`, como vieram | `http_status`, `etag`, `last_modified` |
| corpo descomprimido | `md5`, `sha256`, `bytes` |
| SOA do arquivo | `serial`, `soa_mname`, `soa_rname`, `soa_refresh`, `soa_retry`, `soa_expire`, `soa_minimum` |
| parser | `tlds`, `records`, `rrsigs`, `type_counts`, `warnings` |
| `MERGE` | `tld_inserted` … `record_deleted` |
| recusa | `error` |
| início da verificação, em UTC | `started_at` |

`http_status`, `etag`, `last_modified`, `md5`, `sha256` e `bytes` vazios ou
zero são gravados como NULL. Na prática toda linha tem `http_status = 200`,
`md5` e `sha256` (falhas antes do download e respostas 304 não geram linha).

`created_at` de `rootzone_tld` e `rootzone_record` é "primeira vez que
**este banco** viu", não a data de delegação: apagar o volume e recarregar
zera essas datas.

## Consultas da API

A `api-rootzone` só lê; o que ela lê é o contrato destas tabelas com ela. As
consultas abaixo são as do teste de integração do coletor
(`apps/rootzone/collector/internal/store/store_integration_test.go`), que
confere os resultados com o recorte e, com o arquivo real
(`make test-real`), o índice usado e o tempo.

| Uso | Consulta | Índice | Arquivo real (2026-09-30) |
|---|---|---|---|
| versão do dataset, SOA e `/meta` | `SELECT uuid::text, created_at, url, sha256, serial, soa_mname, soa_rname, soa_refresh, soa_retry, soa_expire, soa_minimum, tlds, records, rrsigs FROM rootzone_run WHERE status = 1 ORDER BY created_at DESC LIMIT 1` | `ix_rootzone_run_applied` | — |
| `/meta` (linha do coletor) | `SELECT last_sync_at, last_check_at, consolidated FROM jobs WHERE app = 'collector-rootzone'` | `uq_jobs_app` | — |
| listar todos os TLDs | `SELECT tld, tld_unicode, nameservers, nameservers_ipv4, nameservers_ipv6, ds_records FROM rootzone_tld ORDER BY tld` | leitura da tabela inteira (1.438 linhas) | ~4 ms |
| um TLD | `SELECT tld, tld_unicode, nameservers, nameservers_ipv4, nameservers_ipv6, ds_records, created_at, updated_at FROM rootzone_tld WHERE tld = $1` | `uq_rootzone_tld_tld` | ~0,5 ms |
| delegação de um TLD: NS e DS | `SELECT type, rdata, ttl FROM rootzone_record WHERE owner = $1 AND type IN ('NS', 'DS') ORDER BY type, rdata` | `uq_rootzone_record_owner_type_rdata` | ~0,5 ms (`br`: 7 linhas) |
| delegação de um TLD: glue A/AAAA dos seus NS | `SELECT g.owner, g.type, g.rdata, g.ttl FROM rootzone_record ns JOIN rootzone_record g ON g.owner = ns.rdata AND g.type IN ('A', 'AAAA') WHERE ns.owner = $1 AND ns.type = 'NS' ORDER BY g.owner, g.type, g.rdata` | `uq_rootzone_record_owner_type_rdata` (nas duas pontas) | ~0,5–0,7 ms (`br`: 12, `com`: 26) |
| ápice da zona (SOA, NS da raiz, DNSKEY, NSEC, ZONEMD) | `SELECT type, rdata, ttl FROM rootzone_record WHERE owner = '.' ORDER BY type, rdata` | `uq_rootzone_record_owner_type_rdata` | ~0,5 ms (20 linhas) |
| TLDs servidos por um servidor de nome | `SELECT owner FROM rootzone_record WHERE type = 'NS' AND rdata = $1 ORDER BY owner` (nome normalizado: minúsculas, sem o ponto final) | `ix_rootzone_record_type_rdata` | ~0,4 ms |
| dono de um endereço de glue | `SELECT owner, type FROM rootzone_record WHERE type IN ('A', 'AAAA') AND rdata = $1 ORDER BY owner` (endereço na forma canônica: a API normaliza com `netip`) | `ix_rootzone_record_type_rdata` | ~0,4 ms |

- O TLD pedido chega à consulta normalizado como o coletor grava: minúsculas,
  sem o ponto final, em ASCII (um nome em Unicode precisa virar `xn--…`
  antes; a `tld_unicode` serve para exibir).
- A zona raiz não é consultada aqui (`owner = '.'` é o ápice, não um TLD).
- O glue de um servidor que fica sob outro TLD (`ns.dns.br` servindo `bo`)
  vem pela mesma junção: o glue está na zona raiz, qualquer que seja o TLD
  do nome.
- Colunas lidas: `rootzone_run` (`uuid`, `status`, `created_at`, `url`,
  `sha256`, `serial`, `soa_*`, `tlds`, `records`, `rrsigs`), `rootzone_tld`
  (todas menos `uuid`), `rootzone_record` (`owner`, `type`, `rdata`, `ttl`)
  e `jobs` (`app`, `last_sync_at`, `last_check_at`, `consolidated`).

## Mudar o schema

- Sempre numa migration **nova** em `database/postgres/rootzone/`
  (`make -C database/postgres new APP=rootzone NAME=<descricao>`), testada com
  `make -C database/postgres test`
  ([../../plataforma/postgres.md](../../plataforma/postgres.md#migrations)).
- No mesmo trabalho: este arquivo, `internal/store` e o teste de integração
  do coletor.
- Mudou uma coluna, um índice ou a normalização que a API lê (tabela acima)?
  Avise a `api-rootzone` (spec e `internal/store` dela). Normalização nova
  vale para as linhas existentes depois de um `--force`.
