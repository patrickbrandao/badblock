# API da APNIC (`api-apnic`)

A `api-apnic` é um **clone** da `api-lacnic`, o modelo das APIs de RIR: o
comportamento está em [../../padroes/api.md](../../padroes/api.md),
[../../padroes/openapi.md](../../padroes/openapi.md) e
[../rir/api.md](../rir/api.md), sem nada de diferente. O código comum é o do
modelo com o nome trocado (conferido em 2026-09-29), e o que é da APNIC fica
em `internal/rir/rir.go` ([../rir/api.md](../rir/api.md#código)). Aqui, os
valores, as respostas reais, os ETags e as medições da APNIC. Dono:
sub-agente `api-apnic`.

O arquivo da APNIC muda o conteúdo de algumas respostas, sem mudar o código
([fonte.md](fonte.md#fatos-medidos-arquivo-de-2026-09-28-serial-20260929)):
`cc` `null` em todo available/reserved e em um bloco designado com titular,
e nunca `ZZ`; `reg_date` `null` só nos available/reserved; `start_date`
`null` na [meta](#meta) (a `startdate` vem vazia); opaque-id hex maiúsculo de
8 dígitos, que o [titular](#titular) acha também em minúsculas; todo registro
IPv4 forma um CIDR ([IP](#ip)); 743 faixas de mais de um ASN no `range` de
[`/asn`](#asn); e o maior titular, `A9117E4D`, com ≈ 164 KB.

## Valores

| Item | Valor |
|---|---|
| `internal/rir/rir.go` | `Source` `apnic`, `Title` `APNIC`, `SourceURL` `https://ftp.apnic.net/stats/apnic/delegated-apnic-extended-latest` (sem `/pub`, [fonte.md](fonte.md#urls)), `OpaqueIDChangesDaily` `false` |
| Caminho | `BASE_PATH` `/apnic`; em produção, `https://api.badblock.net.br/apnic/` |
| Porta no loopback | 8103 (`API_APNIC_HOST_PORT`, `PORT` do `Makefile`): `http://127.0.0.1:8103/apnic/` |
| `SMOKE_ASN` | 4608 (AU, allocated, titular `A91DC5BE`) |
| Tabelas lidas | `apnic_run`, `apnic_asn` e `apnic_prefix` ([dados.md](dados.md)) e a linha `collector-apnic` de `jobs` |
| `.env` | `API_APNIC_TAG=latest`, `API_APNIC_HOST_PORT=8103`, `API_APNIC_CACHE_TTL=3600`; opcionais `API_APNIC_CACHE_ENABLED` (`true`), `API_APNIC_DB_POOL_MAX` (`10`) e `API_APNIC_REDIS_URL` |
| Imagem e container | `tmsoftbrasil/badblock-api-apnic`, label `description` `API HTTP das delegações de ASNs e blocos IP do RIR APNIC (delegated-extended), do BadBlock`; `badblock-api-apnic` |
| Traefik | router e serviço `badblock-api-apnic`, middleware `badblock-api-apnic-ratelimit`, regra `Host(api.badblock.net.br) && (Path(/apnic) \|\| PathPrefix(/apnic/))` ([molde](../../plataforma/publicacao.md#traefik)) |

## Exemplos

Respostas reais com o arquivo de 2026-09-28 (serial 20260929, `enddate`
2026-09-28, [fonte.md](fonte.md#fatos-medidos-arquivo-de-2026-09-28-serial-20260929)),
baixado em 2026-09-29 às 01:09 UTC e carregado pelo `collector-apnic`:
dataset `01a0eab5-7637-7be3-9d60-271b74f11794`, aplicado em
2026-09-29T01:09:14Z (`01a0eab5-…` nos exemplos curtos). São também os
exemplos do manifesto. O que cada campo quer dizer está em
[../rir/api.md](../rir/api.md).

### ASN

`/apnic/asn/4608` (o mesmo em `/apnic/v1/asn/AS4608` e `/apnic/asn/as4608`):

```json
{
  "asn": 4608,
  "range": {"start": 4608, "end": 4608, "count": 1},
  "cc": "AU",
  "reg_date": "1995-05-30",
  "status": "allocated",
  "opaque_id": "A91DC5BE",
  "first_seen": "2026-09-29T01:09:14Z",
  "updated_at": "2026-09-29T01:09:14Z",
  "dataset": {"version": "01a0eab5-7637-7be3-9d60-271b74f11794", "updated_at": "2026-09-29T01:09:14Z"}
}
```

ASN no meio de uma faixa (`/apnic/asn/4677`, registro AS4672–AS4681):

```json
{
  "asn": 4677,
  "range": {"start": 4672, "end": 4681, "count": 10},
  "cc": "JP", "reg_date": "1995-08-30", "status": "allocated", "opaque_id": "A91A7381",
  "first_seen": "2026-09-29T01:09:14Z", "updated_at": "2026-09-29T01:09:14Z",
  "dataset": {"version": "01a0eab5-…", "updated_at": "2026-09-29T01:09:14Z"}
}
```

Faixa reservada, sem país, data nem titular (`/apnic/asn/9428`):

```json
{
  "asn": 9428,
  "range": {"start": 9427, "end": 9429, "count": 3},
  "cc": null, "reg_date": null, "status": "reserved", "opaque_id": null,
  "first_seen": "2026-09-29T01:09:14Z", "updated_at": "2026-09-29T01:09:14Z",
  "dataset": {"version": "01a0eab5-…", "updated_at": "2026-09-29T01:09:14Z"}
}
```

Na APNIC, 743 dos 14.762 registros de ASN são faixas de mais de um ASN, em
allocated, available e reserved; a maior, AS143674–AS146745, tem 3.072
([dados.md](dados.md#o-que-as-tabelas-guardam)).

### IP

`/apnic/ip/1.1.1.1` (e `/apnic/ip/::ffff:1.1.1.1`, a mesma resposta):

```json
{
  "ip": "1.1.1.1",
  "prefix": "1.1.1.0/24",
  "cc": "AU", "reg_date": "2011-08-11", "status": "assigned", "opaque_id": "A91872ED",
  "record": {"start": "1.1.1.0", "value": 256},
  "dataset": {"version": "01a0eab5-…", "updated_at": "2026-09-29T01:09:14Z"}
}
```

`/apnic/ip/2001:dc0::1`:

```json
{
  "ip": "2001:dc0::1",
  "prefix": "2001:dc0::/32",
  "cc": "AU", "reg_date": "2003-01-24", "status": "assigned", "opaque_id": "A91DC5BE",
  "record": {"start": "2001:dc0::", "value": 32},
  "dataset": {"version": "01a0eab5-…", "updated_at": "2026-09-29T01:09:14Z"}
}
```

Na APNIC todo registro IPv4 forma um CIDR (de `/8` a `/26`; conferido no
arquivo inteiro): `record.start` é sempre o endereço do próprio `prefix` e,
no IPv4, `record.value` é o tamanho do bloco. O registro dividido do modelo
([../rir/api.md](../rir/api.md#ip)) não acontece aqui.

### Prefixo

`/apnic/prefix/1.1.1.128/25`:

```json
{
  "query": "1.1.1.128/25",
  "prefix": "1.1.1.0/24",
  "exact": false,
  "cc": "AU", "reg_date": "2011-08-11", "status": "assigned", "opaque_id": "A91872ED",
  "record": {"start": "1.1.1.0", "value": 256},
  "dataset": {"version": "01a0eab5-…", "updated_at": "2026-09-29T01:09:14Z"}
}
```

`/apnic/prefix/1.1.1.0/24` (e `/apnic/prefix/1.1.1.9/24`, que consulta o
mesmo `1.1.1.0/24`) responde o mesmo bloco com `"query": "1.1.1.0/24"` e
`"exact": true`. Em IPv6, `/apnic/prefix/2001:dc0:abcd::/48`:

```json
{
  "query": "2001:dc0:abcd::/48",
  "prefix": "2001:dc0::/32",
  "exact": false,
  "cc": "AU", "reg_date": "2003-01-24", "status": "assigned", "opaque_id": "A91DC5BE",
  "record": {"start": "2001:dc0::", "value": 32},
  "dataset": {"version": "01a0eab5-…", "updated_at": "2026-09-29T01:09:14Z"}
}
```

### Titular

`/apnic/holder/A91DC5BE` (o titular do AS4608; `/apnic/holder/a91dc5be`
responde o mesmo, com outra chave e outro ETag, [abaixo](#chaves-de-cache-e-etags)):

```json
{
  "opaque_id": "A91DC5BE",
  "cc": "AU",
  "ccs": ["AU", "JP"],
  "counts": {"asns": 7, "ipv4": 11, "ipv6": 6},
  "asns": [
    {"start": 4608, "end": 4608, "count": 1, "cc": "AU", "status": "allocated", "reg_date": "1995-05-30"},
    {"start": 4777, "end": 4777, "count": 1, "cc": "JP", "status": "allocated", "reg_date": "1996-08-09"},
    {"start": 9545, "end": 9545, "count": 1, "cc": "AU", "status": "allocated", "reg_date": "2000-12-28"},
    {"start": 18366, "end": 18370, "count": 5, "cc": "AU", "status": "allocated", "reg_date": "2002-08-19"},
    {"start": 24555, "end": 24555, "count": 1, "cc": "AU", "status": "allocated", "reg_date": "2007-01-17"},
    {"start": 55638, "end": 55638, "count": 1, "cc": "AU", "status": "allocated", "reg_date": "2010-07-23"},
    {"start": 131076, "end": 131076, "count": 1, "cc": "AU", "status": "allocated", "reg_date": "2007-02-13"}
  ],
  "prefixes": {
    "ipv4": [
      {"prefix": "202.12.28.0/23", "cc": "AU", "status": "assigned", "reg_date": "1997-03-04"},
      {"prefix": "202.12.31.0/24", "cc": "AU", "status": "assigned", "reg_date": "2002-08-19"},
      {"prefix": "203.119.0.0/24", "cc": "AU", "status": "allocated", "reg_date": "2003-01-13"},
      {"prefix": "203.119.42.0/24", "cc": "AU", "status": "allocated", "reg_date": "2005-09-28"},
      {"prefix": "203.119.76.0/24", "cc": "AU", "status": "assigned", "reg_date": "2008-11-18"},
      {"prefix": "203.119.77.0/24", "cc": "AU", "status": "assigned", "reg_date": "2009-09-09"},
      {"prefix": "203.119.86.0/24", "cc": "AU", "status": "assigned", "reg_date": "2010-03-09"},
      {"prefix": "203.119.95.0/24", "cc": "AU", "status": "assigned", "reg_date": "2010-09-28"},
      {"prefix": "203.119.100.0/22", "cc": "AU", "status": "assigned", "reg_date": "2010-11-04"},
      {"prefix": "203.119.104.0/21", "cc": "AU", "status": "assigned", "reg_date": "2010-11-04"},
      {"prefix": "220.247.144.0/20", "cc": "AU", "status": "allocated", "reg_date": "2011-03-30"}
    ],
    "ipv6": [
      {"prefix": "2001:dc0::/32", "cc": "AU", "status": "assigned", "reg_date": "2003-01-24"},
      {"prefix": "2001:dd8:6::/48", "cc": "AU", "status": "assigned", "reg_date": "2010-03-09"},
      {"prefix": "2001:dd8:8::/45", "cc": "AU", "status": "assigned", "reg_date": "2010-03-09"},
      {"prefix": "2001:dd8:12::/48", "cc": "AU", "status": "assigned", "reg_date": "2010-09-28"},
      {"prefix": "2001:ddd::/48", "cc": "AU", "status": "assigned", "reg_date": "2015-11-20"},
      {"prefix": "2001:df9::/32", "cc": "AU", "status": "allocated", "reg_date": "2007-01-17"}
    ]
  },
  "dataset": {"version": "01a0eab5-…", "updated_at": "2026-09-29T01:09:14Z"}
}
```

- O opaque-id da APNIC é hex maiúsculo de 8 dígitos, com 26.807 titulares;
  307 deles (1,1%) têm mais de um país: este (23 recursos `AU` e 1 `JP`) e
  `A92E1062` (1.104 blocos `CN` e 5 `HK` → `"cc": "CN", "ccs": ["CN", "HK"]`).
- O bloco designado sem país (`2001:de3::/48`, titular `A919DB08`) sai com
  `"cc": null` e não conta: o titular responde `"cc": "SG", "ccs": ["SG"]`.
- O maior titular do arquivo é `A9117E4D` (19 registros de ASN, 1.925 blocos
  IPv4 e 11 IPv6): 164.351 bytes (≈ 164 KB), em ~14–19 ms sem cache
  ([medições](#medições)).

### Meta

`/apnic/meta`:

```json
{
  "app": "api-apnic",
  "version": "0.1.0",
  "dataset": {
    "version": "01a0eab5-7637-7be3-9d60-271b74f11794",
    "updated_at": "2026-09-29T01:09:14Z",
    "source": "https://ftp.apnic.net/stats/apnic/delegated-apnic-extended-latest",
    "sha256": "3e1d41f227d2f3990c88797333467c47e32595fb328c92c49a24bb001f1525c0",
    "md5": "a30f818f9c90fa519bdbcb1c298edb17",
    "serial": "20260929",
    "start_date": null,
    "end_date": "2026-09-28",
    "asn_records": 14762, "ipv4_records": 61684, "ipv6_records": 113822,
    "prefixes_v4": 61684, "prefixes_v6": 113822
  },
  "collector": {
    "app": "collector-apnic",
    "last_sync_at": "2026-09-29T01:09:14Z", "last_check_at": "2026-09-29T01:09:14Z",
    "consolidated": false
  }
}
```

- `start_date` é `null` porque a APNIC publica a `startdate` vazia; o
  `serial` é a data de geração em Brisbane, um dia depois do `end_date`
  ([fonte.md](fonte.md#publicação)).
- `md5` é o do `.md5` publicado com o arquivo, e `prefixes_v4` =
  `ipv4_records` porque nenhum registro IPv4 é dividido.

Antes da primeira carga:
`{"app": "api-apnic", "version": "0.1.0", "dataset": null, "collector": null}`.

### Índice

`/apnic/` e `/apnic/v1/`:

```json
{
  "app": "api-apnic",
  "version": "0.1.0",
  "registry": "APNIC",
  "base_path": "/apnic",
  "versions": ["v1"],
  "endpoints": [
    "/apnic/asn/{asn}", "/apnic/ip/{ip}", "/apnic/prefix/{ip}/{len}",
    "/apnic/holder/{opaque_id}", "/apnic/meta", "/apnic/status",
    "/apnic/openapi.yaml"
  ],
  "source": "https://ftp.apnic.net/stats/apnic/delegated-apnic-extended-latest"
}
```

### Saúde

`/apnic/status` (e `/apnic/health`, GET ou POST):

```json
{"success": true, "status": "ok", "timestamp": "2026-09-29T01:09:57Z", "message": "api-apnic operacional",
 "checks": {"dataset": "ok", "postgres": "ok", "valkey": "ok"}}
```

Os outros estados, como no manifesto (mesmo `timestamp`):

| `status` | HTTP | `message` | `checks` |
|---|---|---|---|
| `starting` | 200 | `aguardando a primeira sincronização do collector-apnic` | `dataset` `empty`, `postgres` `ok`, `valkey` `ok` |
| `degraded` | 200 | `Valkey indisponível; respondendo sem cache` | `dataset` `ok`, `postgres` `ok`, `valkey` `error` |
| `error` | 503 (`success: false`) | `PostgreSQL indisponível` | `dataset` `ok`, `postgres` `error`, `valkey` `ok` |

### Erros

Os do manifesto, com as mensagens reais (conferidas em 2026-09-29):

| Pedido | HTTP | `code` | `message` |
|---|---|---|---|
| `/apnic/asn/abc` | 400 | `bad_request` | `ASN inválido: use um número de 0 a 4294967295, com ou sem o prefixo AS` |
| `/apnic/ip/x` | 400 | `bad_request` | `endereço IP inválido` |
| `/apnic/prefix/1.1.1.0/33` | 400 | `bad_request` | `tamanho de prefixo inválido` |
| `/apnic/holder/nao%20existe` | 400 | `bad_request` | `opaque_id inválido: use de 1 a 128 letras, dígitos, ponto, hífen ou sublinhado` |
| `/apnic/asn/61613` (ASN da LACNIC; o exemplo do manifesto) | 404 | `not_found` | `AS61613 não consta no arquivo do RIR APNIC` |
| `/apnic/asn/1` (ASN da ARIN) | 404 | `not_found` | `AS1 não consta no arquivo do RIR APNIC` |
| `/apnic/ip/8.8.8.8` | 404 | `not_found` | `8.8.8.8 não pertence a nenhum bloco no arquivo do RIR APNIC` |
| `/apnic/prefix/8.8.8.0/24` | 404 | `not_found` | `8.8.8.0/24 não está contido em nenhum bloco no arquivo do RIR APNIC` |
| `/apnic/holder/NAOEXISTE` | 404 | `not_found` | `o titular NAOEXISTE não consta no arquivo do RIR APNIC` |
| `/apnic/nada` | 404 | `not_found` | `rota inexistente; veja /apnic/` |
| uma consulta antes da primeira carga | 503 | `dataset_not_ready` | `a primeira sincronização do collector-apnic ainda não terminou; tente em alguns minutos` |

### Chaves de cache e ETags

Com o dataset dos exemplos (chave completa:
`badblock:api-apnic:01a0eab5-7637-7be3-9d60-271b74f11794:<consulta>`); ETags
calculados por `etagFor` em 2026-09-29:

| Pedido | Consulta | ETag |
|---|---|---|
| `/apnic/asn/4608`, `/apnic/v1/asn/AS4608` | `asn:4608` | `W/"6b76420aed360c08"` (o exemplo do manifesto) |
| `/apnic/asn/4677` | `asn:4677` | `W/"6b8e410aed4a9c36"` |
| `/apnic/asn/9428` | `asn:9428` | `W/"86e0d876ba5e4a59"` |
| `/apnic/ip/1.1.1.1`, `/apnic/ip/::ffff:1.1.1.1` | `ip:1.1.1.1` | `W/"baeb111b10523e27"` |
| `/apnic/ip/2001:dc0::1` | `ip:2001:dc0::1` | `W/"277fdec14592c7f6"` |
| `/apnic/prefix/1.1.1.128/25` | `prefix:1.1.1.128/25` | `W/"7c60a4e93cf59400"` |
| `/apnic/prefix/1.1.1.9/24`, `/apnic/prefix/1.1.1.0/24` | `prefix:1.1.1.0/24` | `W/"724dd32b53099fd0"` |
| `/apnic/prefix/2001:dc0:abcd::/48` | `prefix:2001:dc0:abcd::/48` | `W/"b555d7eac573786b"` |
| `/apnic/holder/A91DC5BE` | `holder:A91DC5BE` | `W/"778121aec792c1d4"` |
| `/apnic/holder/a91dc5be` | `holder:a91dc5be` | `W/"f19b5cecc6d90f34"` (outra chave, a mesma resposta) |

## Manifesto

O do [modelo](../rir/api.md#manifesto), sem o aviso do opaque-id diário
(`OpaqueIDChangesDaily` é `false`): `info.version` `'0.1.0'`,
`externalDocs.url`
`https://github.com/patrickbrandao/badblock/tree/main/specs/fontes/apnic`,
servidores locais na porta 8103 e os exemplos desta página.

- Parâmetros: `asn` `'4608'` (na descrição, `4608`, `AS4608` e `as4608` são
  a mesma consulta), `ip` `1.1.1.1`, `ip` e `len` do prefixo `1.1.1.128` e
  `25` (na descrição, `1.1.1.9/24` consulta `1.1.1.0/24`) e `opaque_id`
  `A91DC5BE`; `IfNoneMatch` e `ETag` com `W/"6b76420aed360c08"` (a descrição
  do `ETag` cita `/apnic/asn/4608` e `/apnic/v1/asn/AS4608` e termina com "o
  do exemplo é o de `/asn/4608` com o dataset dos exemplos") e
  `X-Dataset-Version` com a versão dos exemplos.
- Exemplos, dois a mais que o modelo: `/asn` com `asn` (4608), `faixa` (4677,
  "registro AS4672–AS4681" no `summary`) e `reservada` (9428); `/ip` com
  `ipv4` ("ou /ip/::ffff:1.1.1.1" no `summary`) e `ipv6`; `/prefix` com
  `contido` (`1.1.1.128/25`), `exato` (`1.1.1.0/24`, "ou /prefix/1.1.1.9/24")
  e `ipv6` (`2001:dc0:abcd::/48`, citado também na descrição da rota);
  `/holder`, o de `A91DC5BE`; `/meta`, `carregado` e `vazio`; saúde com o
  `timestamp` `2026-09-29T01:09:57Z`. Nos erros, `len` com
  `/prefix/1.1.1.0/33` e os 404 `/asn/61613 (ASN de outro RIR)`,
  `/ip/8.8.8.8`, `/prefix/8.8.8.0/24` e `/holder/NAOEXISTE`.
- Descrições com a particularidade da APNIC (o texto, em markdown como no
  manifesto):

| Onde | Texto |
|---|---|
| parâmetro `opaque_id` | os formatos: "que cobre os formatos dos cinco RIRs: na APNIC, hex maiúsculo de 8 (`A91DC5BE`); nos outros, número de 2 a 6 dígitos, hex minúsculo de 32 e UUID minúsculo com hífens"; a caixa: "(`a91dc5be` acha `A91DC5BE`)" |
| `CountryCode` | País, ISO 3166-1 alfa-2; `null` quando vazio na fonte. A APNIC deixa o `cc` vazio em todos os available/reserved e em um bloco designado (`2001:de3::/48`) e não usa `ZZ` (outros RIRs usam `ZZ` = sem país, que sai como veio). |
| `RegDate` | Data da alocação/designação; `null` quando vazia ou `00000000` na fonte (na APNIC, só nos available/reserved). |
| `IPPrefix` | Bloco CIDR, IPv4 ou IPv6 (ex. `1.1.1.0/24`, `2001:dc0::/32`). |
| `Record` | o texto do modelo seguido de "Na APNIC todo registro IPv4 forma um CIDR, então `start` é sempre o endereço do próprio bloco." |
| `MetaDataset.start_date` | Data inicial do cabeçalho do arquivo (a APNIC publica vazia, então sai `null`). |

## `--help`

A saída real (versão `dev`, 2026-09-29, no stderr) é a de
[../lacnic/api.md](../lacnic/api.md#--help) com os nomes trocados —
`api-apnic`, `RIR APNIC`, `apnic_*`, `collector-apnic` e `/apnic`, inclusive
no padrão de `--base-path` —, conferida linha a linha; a primeira linha é
`api-apnic — API HTTP das delegações de ASNs e blocos IP do RIR APNIC.`.
`--version` sem tag: `api-apnic dev (commit unknown, build unknown)`. Opção
inválida, ex.: `api-apnic: --base-path inválido: "/" (ex.: /apnic)`, saída 2.

## Operação

```bash
curl http://127.0.0.1:8103/apnic/status          # sem Traefik, pela porta do loopback
curl http://127.0.0.1:8103/apnic/asn/4608
make -C apps/apnic/api smoke                     # /status, /meta, /asn/4608 e /openapi.yaml
make -C apps/apnic/api logs
```

Arquivo real ([../rir/api.md](../rir/api.md#arquivo-real-make-test-real)):

```bash
curl -o /tmp/delegated https://ftp.apnic.net/stats/apnic/delegated-apnic-extended-latest
make -C apps/apnic/api test-real FILE=/tmp/delegated   # APNIC_REAL_FILE
```

## Medições

Arquivo real de 2026-09-28 (serial 20260929; 190.268 registros: 14.762
registros de ASN e 175.506 blocos gravados), medido em 2026-09-29:

| Medição | Valor |
|---|---|
| Carga pelo `collector-apnic` (`--once`, PG18 descartável no Docker Desktop) | ~2–5,5 s com o arquivo servido localmente; ~8,3 s baixando da APNIC |
| Uma consulta no Postgres (`EXPLAIN ANALYZE`) | `/asn` ~0,15 ms (índice único de `asn_start`); `/ip` ~0,6–1,2 ms (GiST sobre 175 mil blocos, ~58 páginas lidas por consulta; na LACNIC, com 80 mil blocos, ~0,1 ms) |
| `make test-real` (handler completo, sem cache; duas rodadas) | 500 `/ip` aleatórios em 0,45–0,87 ms de média; 500 `/asn` em 0,16–0,29 ms; o maior titular (`A9117E4D`, 164 KB) em 13–14 ms |
| Por HTTP, binário no host e Postgres/Valkey no Docker Desktop, 1.000 consultas aleatórias com keep-alive | sem cache, `/ip` e `/asn` em ~1,7 ms de média; com cache, ~1,6–2,1 ms nos `HIT` e 6–8,5 ms nos `MISS` (Valkey + Postgres + gravação no Valkey; quase tudo é o ida e volta ao Docker) |

Conferido de ponta a ponta em 2026-09-29, com o arquivo real num Postgres e
num Valkey descartáveis: com o Valkey parado, `/status` fica `degraded` e as
rotas seguem respondendo (5 tentativas de ~50 ms; depois o disjuntor abre e
as respostas voltam a ~1,7 ms); com o Postgres parado, `/status` responde 503
e as consultas fora do cache, `503 database_unavailable`, enquanto as que
estão no cache continuam 200. Os dois voltam sozinhos.

## Testes

Os do modelo, sem nada da APNIC além do nome:
[../rir/api.md](../rir/api.md#testes). O código dos testes é o do modelo com
o nome trocado (conferido em 2026-09-29), com os dados de lá (o
[recorte da LACNIC](../lacnic/fonte.md#recorte-testdatadelegated-extended-sampletxt)
e os casos extras), não os do recorte da APNIC; o arquivo real vai em
`APNIC_REAL_FILE`.
