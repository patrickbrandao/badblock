# API da AFRINIC (`api-afrinic`)

A `api-afrinic` é um **clone** da `api-lacnic`, o modelo das APIs de RIR: o
comportamento está em [../../padroes/api.md](../../padroes/api.md),
[../../padroes/openapi.md](../../padroes/openapi.md) e
[../rir/api.md](../rir/api.md), sem nada de diferente. O código é o do
modelo com o nome trocado, inclusive o `internal/rir/rir.go`, onde o `sed` já
dá o `Title` e a `SourceURL` da AFRINIC (conferido em 2026-09-29: a cópia do
modelo passada pelo `sed` da
[clonagem](../rir/api.md#clonar-a-api-para-outro-rir) difere do app só na
porta, no `SMOKE_ASN`, no `README.md` e nos exemplos e particularidades do
manifesto). Aqui, os valores, as respostas reais, as medições e o que a
AFRINIC muda nas respostas:

- `cc` é `ZZ` (nunca `null`) e `reg_date` é `null` em todo available/reserved
  ([Exemplos](#exemplos));
- um ASN por registro: `range.count` é sempre 1 ([ASN](#asn));
- 52 registros IPv4 não formam CIDR e viram 146 blocos, que apontam para o
  mesmo `record` ([IP](#ip), [Prefixo](#prefixo), [Meta](#meta));
- opaque-id hex maiúsculo de 8 caracteres: a busca acha em qualquer caixa,
  mas a chave de cache e o ETag seguem o pedido ([Titular](#titular)).

Dono: sub-agente `api-afrinic`.

## Valores

Conferidos no código em 2026-09-29.

| Item | Valor |
|---|---|
| `internal/rir/rir.go` | `Source` `afrinic`, `Title` `AFRINIC`, `SourceURL` `https://ftp.afrinic.net/pub/stats/afrinic/delegated-afrinic-extended-latest`, `OpaqueIDChangesDaily` `false` ([parâmetros das APIs](../rir/README.md#parâmetros-das-apis)) |
| Caminho | `BASE_PATH` `/afrinic`; em produção, `https://api.badblock.net.br/afrinic/` |
| Porta no loopback | 8102 (`API_AFRINIC_HOST_PORT`, `PORT` do `Makefile`): `http://127.0.0.1:8102/afrinic/` |
| `SMOKE_ASN` | 37100 (MU, allocated, titular `F365C741`) |
| Tabelas lidas | `afrinic_run`, `afrinic_asn` e `afrinic_prefix` ([dados.md](dados.md)) e a linha `collector-afrinic` de `jobs` |
| `.env` | `API_AFRINIC_TAG=latest`, `API_AFRINIC_HOST_PORT=8102`, `API_AFRINIC_CACHE_TTL=3600`; opcionais `API_AFRINIC_CACHE_ENABLED` (`true`), `API_AFRINIC_DB_POOL_MAX` (`10`) e `API_AFRINIC_REDIS_URL` |
| Imagem e container | `tmsoftbrasil/badblock-api-afrinic`, label `description` `API HTTP das delegações de ASNs e blocos IP do RIR AFRINIC (delegated-extended), do BadBlock`; `badblock-api-afrinic` |
| Traefik | router e serviço `badblock-api-afrinic`, middleware `badblock-api-afrinic-ratelimit`, regra `Host(api.badblock.net.br) && (Path(/afrinic) \|\| PathPrefix(/afrinic/))` ([molde](../../plataforma/publicacao.md#traefik)) |

## Exemplos

Respostas reais com o arquivo de 2026-09-28 (serial 20260928,
[fonte.md](fonte.md#fatos-medidos-arquivo-de-2026-09-28-serial-20260928))
carregado pelo `collector-afrinic`: dataset
`01a0eab5-886c-75d9-bd2d-d99f6b5f735e`, aplicado em 2026-09-29T01:09:20Z
(`01a0eab5-…` nos exemplos curtos). São também os exemplos do manifesto. O
que cada campo quer dizer está em [../rir/api.md](../rir/api.md).

Nos [campos de delegação](../rir/api.md#campos-de-delegação), a AFRINIC
difere dos outros RIRs ([dados.md](dados.md#o-que-as-tabelas-guardam)):
`cc` **nunca é `null`** — é `ZZ` (sem país, como publicado) em todo
available/reserved e um país nos demais; nos outros quatro RIRs, o mesmo caso
sai `null` — e `reg_date` é `null` exatamente nos available/reserved (data
vazia na fonte; nenhuma `00000000`).

### ASN

`/afrinic/asn/37100` (o mesmo em `/afrinic/v1/asn/AS37100`):

```json
{
  "asn": 37100,
  "range": {"start": 37100, "end": 37100, "count": 1},
  "cc": "MU", "reg_date": "2009-05-28", "status": "allocated", "opaque_id": "F365C741",
  "first_seen": "2026-09-29T01:09:20Z", "updated_at": "2026-09-29T01:09:20Z",
  "dataset": {"version": "01a0eab5-886c-75d9-bd2d-d99f6b5f735e", "updated_at": "2026-09-29T01:09:20Z"}
}
```

ASN available (`/afrinic/asn/8770`), com o `ZZ` que a AFRINIC publica:

```json
{
  "asn": 8770,
  "range": {"start": 8770, "end": 8770, "count": 1},
  "cc": "ZZ", "reg_date": null, "status": "available", "opaque_id": null,
  "first_seen": "2026-09-29T01:09:20Z", "updated_at": "2026-09-29T01:09:20Z",
  "dataset": {"version": "01a0eab5-…", "updated_at": "2026-09-29T01:09:20Z"}
}
```

Na AFRINIC todo registro de ASN tem um ASN só, inclusive em available e
reserved (4.350 registros, de AS1228 a AS330751:
[fonte.md](fonte.md#fatos-medidos-arquivo-de-2026-09-28-serial-20260928)),
então `range` é sempre `{"start": <asn>, "end": <asn>, "count": 1}` — o
exemplo de faixa do modelo (`/lacnic/asn/28004`) não tem par aqui.

### IP

`/afrinic/ip/41.87.100.10`:

```json
{
  "ip": "41.87.100.10",
  "prefix": "41.87.96.0/19",
  "cc": "MU", "reg_date": "2010-08-16", "status": "allocated", "opaque_id": "F365C741",
  "record": {"start": "41.87.96.0", "value": 8192},
  "dataset": {"version": "01a0eab5-…", "updated_at": "2026-09-29T01:09:20Z"}
}
```

`/afrinic/ip/2c0f:feb0::1`:

```json
{
  "ip": "2c0f:feb0::1",
  "prefix": "2c0f:feb0::/32",
  "cc": "MU", "reg_date": "2010-10-13", "status": "allocated", "opaque_id": "F365C741",
  "record": {"start": "2c0f:feb0::", "value": 32},
  "dataset": {"version": "01a0eab5-…", "updated_at": "2026-09-29T01:09:20Z"}
}
```

Registro que não forma CIDR: `196.1.87.0` + 1280 endereços virou
`196.1.87.0/24` + `196.1.88.0/22`, e os dois pedaços apontam para o mesmo
`record`. `/afrinic/ip/196.1.88.10`:

```json
{
  "ip": "196.1.88.10",
  "prefix": "196.1.88.0/22",
  "cc": "ZA", "reg_date": "1993-11-02", "status": "assigned", "opaque_id": "F369C3AE",
  "record": {"start": "196.1.87.0", "value": 1280},
  "dataset": {"version": "01a0eab5-…", "updated_at": "2026-09-29T01:09:20Z"}
}
```

São 52 registros IPv4 assim, todos allocated ou assigned, que viram 146
blocos (de 2 a 9 por registro;
[fonte.md](fonte.md#fatos-medidos-arquivo-de-2026-09-28-serial-20260928));
nos outros 6.039, `record.start` é o endereço do próprio `prefix` e
`record.value`, o tamanho do bloco.

Blocos available e reserved também respondem, com `ZZ`
(`/afrinic/ip/41.57.113.5`):

```json
{
  "ip": "41.57.113.5",
  "prefix": "41.57.112.0/21",
  "cc": "ZZ", "reg_date": null, "status": "reserved", "opaque_id": null,
  "record": {"start": "41.57.112.0", "value": 2048},
  "dataset": {"version": "01a0eab5-…", "updated_at": "2026-09-29T01:09:20Z"}
}
```

### Prefixo

`/afrinic/prefix/41.87.100.0/24` (e `/afrinic/prefix/41.87.100.9/24`, que
consulta o mesmo `41.87.100.0/24`):

```json
{
  "query": "41.87.100.0/24", "prefix": "41.87.96.0/19", "exact": false,
  "cc": "MU", "reg_date": "2010-08-16", "status": "allocated", "opaque_id": "F365C741",
  "record": {"start": "41.87.96.0", "value": 8192},
  "dataset": {"version": "01a0eab5-…", "updated_at": "2026-09-29T01:09:20Z"}
}
```

`/afrinic/prefix/41.87.96.0/19` responde o mesmo bloco com
`"query": "41.87.96.0/19"` e `"exact": true`; em IPv6,
`/afrinic/prefix/2c0f:feb0:1::/48` responde `2c0f:feb0::/32` com
`"exact": false`. Um pedaço de registro dividido também é exato:
`/afrinic/prefix/196.1.88.0/22` responde `"exact": true`, com `record`
`{"start": "196.1.87.0", "value": 1280}`.

### Titular

`/afrinic/holder/F365C741`:

```json
{
  "opaque_id": "F365C741", "cc": "MU", "ccs": ["MU"],
  "counts": {"asns": 1, "ipv4": 4, "ipv6": 2},
  "asns": [
    {"start": 37100, "end": 37100, "count": 1, "cc": "MU", "status": "allocated", "reg_date": "2009-05-28"}
  ],
  "prefixes": {
    "ipv4": [
      {"prefix": "41.87.96.0/19", "cc": "MU", "status": "allocated", "reg_date": "2010-08-16"},
      {"prefix": "41.206.96.0/19", "cc": "MU", "status": "allocated", "reg_date": "2012-11-05"},
      {"prefix": "41.217.212.0/22", "cc": "MU", "status": "allocated", "reg_date": "2009-08-05"},
      {"prefix": "105.16.0.0/12", "cc": "MU", "status": "allocated", "reg_date": "2014-06-01"}
    ],
    "ipv6": [
      {"prefix": "2c0f:feb0::/32", "cc": "MU", "status": "allocated", "reg_date": "2010-10-13"},
      {"prefix": "2c0f:feb1::/32", "cc": "MU", "status": "allocated", "reg_date": "2013-11-05"}
    ]
  },
  "dataset": {"version": "01a0eab5-…", "updated_at": "2026-09-29T01:09:20Z"}
}
```

- O opaque-id da AFRINIC tem 8 dígitos hex maiúsculos (2.962 titulares no
  arquivo; [fonte.md](fonte.md#fatos-medidos-arquivo-de-2026-09-28-serial-20260928)):
  `/afrinic/holder/f365c741` acha o `F365C741` gravado e responde o mesmo
  corpo, com `"opaque_id": "F365C741"`, mas com a chave `holder:f365c741` e
  outro ETag ([Chaves de cache e ETags](#chaves-de-cache-e-etags)).
- Titulares com mais de um país: 18 dos 2.962 (2026-09-28). Ex.:
  `F3668037`, com 36 recursos `MU`, 3 `ZW` e 2 `ZM` →
  `"cc": "MU", "ccs": ["MU", "ZW", "ZM"]`. Como `ZZ` só aparece em
  available/reserved, que não têm titular, nenhum titular tem `ZZ` em `ccs`.
- O maior titular do arquivo é `F3619C8C` (CI, o de mais registros em
  [fonte.md](fonte.md#fatos-medidos-arquivo-de-2026-09-28-serial-20260928)),
  com 2 registros de ASN, 184 blocos IPv4 e 1 IPv6: 16 KB (o maior dos cinco
  RIRs e o limite do cache estão no [modelo](../rir/api.md#titular)).

### Meta

`/afrinic/meta`:

```json
{
  "app": "api-afrinic", "version": "0.1.0",
  "dataset": {
    "version": "01a0eab5-886c-75d9-bd2d-d99f6b5f735e", "updated_at": "2026-09-29T01:09:20Z",
    "source": "https://ftp.afrinic.net/pub/stats/afrinic/delegated-afrinic-extended-latest",
    "sha256": "5594fbfe9cf656e9e23ff25c061ec7c0dbefec7872c4e7720d3074f956c39046",
    "md5": "5a82aad62da62ef064e04c62a8baaeb7",
    "serial": "20260928", "start_date": null, "end_date": "2026-09-28",
    "asn_records": 4350, "ipv4_records": 6091, "ipv6_records": 9345,
    "prefixes_v4": 6185, "prefixes_v6": 9345
  },
  "collector": {
    "app": "collector-afrinic", "last_sync_at": "2026-09-29T01:09:20Z",
    "last_check_at": "2026-09-29T01:09:20Z", "consolidated": false
  }
}
```

- `start_date` sai `null` porque a AFRINIC publica `startdate` `00000000`, e
  o `serial` é a data do arquivo, a mesma de `end_date` (como texto).
- `prefixes_v4` (6.185) é maior que `ipv4_records` (6.091) porque os 52
  registros não-CIDR viram 146 blocos; no IPv6, um bloco por registro.

Antes da primeira carga:
`{"app": "api-afrinic", "version": "0.1.0", "dataset": null, "collector": null}`.

### Índice

`/afrinic/` e `/afrinic/v1/`:

```json
{
  "app": "api-afrinic", "version": "0.1.0", "registry": "AFRINIC",
  "base_path": "/afrinic", "versions": ["v1"],
  "endpoints": [
    "/afrinic/asn/{asn}", "/afrinic/ip/{ip}", "/afrinic/prefix/{ip}/{len}",
    "/afrinic/holder/{opaque_id}", "/afrinic/meta", "/afrinic/status",
    "/afrinic/openapi.yaml"
  ],
  "source": "https://ftp.afrinic.net/pub/stats/afrinic/delegated-afrinic-extended-latest"
}
```

### Saúde

`/afrinic/status` (e `/afrinic/health`, GET ou POST):

```json
{"success": true, "status": "ok", "timestamp": "2026-09-29T01:13:02Z", "message": "api-afrinic operacional",
 "checks": {"dataset": "ok", "postgres": "ok", "valkey": "ok"}}
```

Os outros estados, como no manifesto (mesmo `timestamp`):

| `status` | HTTP | `message` | `checks` |
|---|---|---|---|
| `starting` | 200 | `aguardando a primeira sincronização do collector-afrinic` | `dataset` `empty`, `postgres` `ok`, `valkey` `ok` |
| `degraded` | 200 | `Valkey indisponível; respondendo sem cache` | `dataset` `ok`, `postgres` `ok`, `valkey` `error` |
| `error` | 503 (`success: false`) | `PostgreSQL indisponível` | `dataset` `ok`, `postgres` `error`, `valkey` `ok` |

### Erros

Os do manifesto, com as mensagens reais:

| Pedido | HTTP | `code` | `message` |
|---|---|---|---|
| `/afrinic/asn/abc` | 400 | `bad_request` | `ASN inválido: use um número de 0 a 4294967295, com ou sem o prefixo AS` |
| `/afrinic/ip/x` | 400 | `bad_request` | `endereço IP inválido` |
| `/afrinic/prefix/41.87.96.0/33` | 400 | `bad_request` | `tamanho de prefixo inválido` |
| `/afrinic/holder/nao%20existe` | 400 | `bad_request` | `opaque_id inválido: use de 1 a 128 letras, dígitos, ponto, hífen ou sublinhado` |
| `/afrinic/asn/1` (ASN da ARIN) | 404 | `not_found` | `AS1 não consta no arquivo do RIR AFRINIC` |
| `/afrinic/ip/192.0.2.1` | 404 | `not_found` | `192.0.2.1 não pertence a nenhum bloco no arquivo do RIR AFRINIC` |
| `/afrinic/prefix/192.0.2.0/24` | 404 | `not_found` | `192.0.2.0/24 não está contido em nenhum bloco no arquivo do RIR AFRINIC` |
| `/afrinic/holder/999999` | 404 | `not_found` | `o titular 999999 não consta no arquivo do RIR AFRINIC` |
| `/afrinic/nada` | 404 | `not_found` | `rota inexistente; veja /afrinic/` |
| uma consulta antes da primeira carga | 503 | `dataset_not_ready` | `a primeira sincronização do collector-afrinic ainda não terminou; tente em alguns minutos` |

### Chaves de cache e ETags

Com o dataset dos exemplos (chave completa:
`badblock:api-afrinic:01a0eab5-886c-75d9-bd2d-d99f6b5f735e:<consulta>`);
ETags calculados por `etagFor` em 2026-09-29:

| Pedido | Consulta | ETag |
|---|---|---|
| `/afrinic/asn/37100`, `/afrinic/v1/asn/AS37100` | `asn:37100` | `W/"97bec918322e57d8"` (o exemplo do manifesto) |
| `/afrinic/asn/8770` | `asn:8770` | `W/"5d076aa86f2219e9"` |
| `/afrinic/ip/41.87.100.10` | `ip:41.87.100.10` | `W/"30d074b6543bcce"` (15 dígitos: sem zeros à esquerda) |
| `/afrinic/ip/2c0f:feb0::1` | `ip:2c0f:feb0::1` | `W/"4012396a242ad8f1"` |
| `/afrinic/ip/196.1.88.10`, `/afrinic/ip/::ffff:196.1.88.10` | `ip:196.1.88.10` | `W/"de3caef763560c34"` |
| `/afrinic/ip/41.57.113.5` | `ip:41.57.113.5` | `W/"5d1f9141b36c0dbf"` |
| `/afrinic/prefix/41.87.100.9/24`, `/afrinic/prefix/41.87.100.0/24` | `prefix:41.87.100.0/24` | `W/"423bb6021626f703"` |
| `/afrinic/prefix/41.87.96.0/19` | `prefix:41.87.96.0/19` | `W/"659f3a2ce0ada335"` |
| `/afrinic/prefix/2c0f:feb0:1::/48` | `prefix:2c0f:feb0:1::/48` | `W/"b7ca1dbb9b684877"` |
| `/afrinic/prefix/196.1.88.0/22` | `prefix:196.1.88.0/22` | `W/"f5161a613f4d1425"` |
| `/afrinic/holder/F365C741` | `holder:F365C741` | `W/"57de6aa24515458e"` |
| `/afrinic/holder/f365c741` | `holder:f365c741` | `W/"d4944c714dc46d4e"` (a mesma resposta, outra chave) |

## Manifesto

O do [modelo](../rir/api.md#manifesto), sem o aviso do opaque-id diário
(`OpaqueIDChangesDaily` é `false`), com os exemplos desta página:

| Parte | Na AFRINIC |
|---|---|
| `info`, `externalDocs`, `servers` | `version` `'0.1.0'`; `summary` `Delegações de ASNs e blocos IP do RIR AFRINIC, do BadBlock.`; `externalDocs.url` `https://github.com/patrickbrandao/badblock/tree/main/specs/fontes/afrinic`; servidores locais `http://127.0.0.1:8102/afrinic` e `http://127.0.0.1:8102/afrinic/v1` (o primeiro também em `/health`, `/status`, `/ping` e `/openapi.yaml`) |
| parâmetros | `asn` `'37100'` (a descrição cita `37100`, `AS37100` e `as37100`); `ip` `41.87.100.10`; no `/prefix`, `ip` `41.87.100.0` e `len` `24` (a descrição: `41.87.100.9/24` consulta `41.87.100.0/24`); `opaque_id` `F365C741` |
| cabeçalhos e `IPPrefix` | `IfNoneMatch` e `ETag` com `W/"97bec918322e57d8"` (a descrição do `ETag` cita `/afrinic/asn/37100` e `/afrinic/v1/asn/AS37100`); `X-Dataset-Version` com a versão dos exemplos; `IPPrefix` com `41.87.96.0/19` e `2c0f:feb0::/32` |
| exemplos do `/asn` | `asn` (`ASN alocado (/asn/37100)`) e `available` (`ASN available, com o ZZ que a AFRINIC publica (/asn/8770)`), no lugar do `faixa` do modelo |
| exemplos do `/ip` | `ipv4` (`IPv4 (/ip/41.87.100.10)`), `ipv6` (`IPv6 (/ip/2c0f:feb0::1)`), `nao_cidr` (`Pedaço de um registro IPv4 que não forma CIDR (/ip/196.1.88.10)`) e `reserved` (`Bloco reserved (/ip/41.57.113.5)`) |
| exemplos do `/prefix`, `/holder` e `/meta` | `contido` (`Prefixo contido num bloco maior (/prefix/41.87.100.0/24)`) e `exato` (`O próprio bloco (/prefix/41.87.96.0/19)`); um `example`, o de `F365C741`; `carregado` (`Com dados`) e `vazio` (`Antes da primeira carga`) |
| `BadRequest`, `NotFound` | `asn` (`/asn/abc`), `ip` (`/ip/x`), `len` (`/prefix/41.87.96.0/33`) e `opaque_id` (`/holder/nao%20existe`); `asn` (`/asn/1`), `ip` (`/ip/192.0.2.1`), `prefix` (`/prefix/192.0.2.0/24`) e `holder` (`/holder/999999`) — com as mensagens de [Erros](#erros) |
| `StatusOK`, `StatusError` | `ok`, `starting`, `degraded`; `error` — os de [Saúde](#saúde), com `timestamp` `2026-09-29T01:13:02Z` |

Particularidades da AFRINIC, com texto próprio no manifesto (no lugar do
texto do modelo, ou a mais):

| Onde | Texto |
|---|---|
| `/asn`, `description` | "Responde o registro do arquivo do RIR que contém o ASN (`range`); `asn` é o número pedido. Um registro pode cobrir uma faixa de ASNs em outros RIRs, mas a AFRINIC lista um ASN por linha: aqui `range.count` é sempre 1, inclusive em available/reserved." |
| `/prefix`, fim da `description` | "IPv6 também funciona: `/prefix/2c0f:feb0:1::/48` responde `2c0f:feb0::/32` com `exact` false. Um pedaço de registro dividido também é exato (`/prefix/196.1.88.0/22`)." |
| parâmetro `opaque_id` | na lista dos cinco formatos, "hex maiúsculo de 8 (`F3619C8C`, o da AFRINIC)"; o exemplo de caixa é "(`f365c741` acha `F365C741`)" |
| `CountryCode` | "País, ISO 3166-1 alfa-2, como o RIR publica. A AFRINIC publica `ZZ` (sem país) em todo available/reserved; `null` só quando o campo vem vazio na fonte (o que outros RIRs fazem em available/reserved)." |
| `RegDate` | "Data da alocação/designação; `null` quando vazia ou `00000000` na fonte (na AFRINIC, vazia em todo available/reserved)." |
| `Record` | a descrição do modelo mais "Ex. da AFRINIC: `196.1.87.0` + 1280 endereços vira `196.1.87.0/24` + `196.1.88.0/22`." |
| `MetaDataset.start_date` | "Data inicial do cabeçalho do arquivo (a AFRINIC publica `00000000`, que sai `null`)." |

## `--help`

Saída real (versão `dev`, 2026-09-29), no stderr: a do modelo
([../lacnic/api.md](../lacnic/api.md#--help)) com `api-afrinic`,
`RIR AFRINIC`, `afrinic_*`, `collector-afrinic` e `/afrinic` (inclusive o
padrão de `--base-path`) no lugar dos nomes da LACNIC — comparada linha a
linha, nada mais muda. A primeira linha é
`api-afrinic — API HTTP das delegações de ASNs e blocos IP do RIR AFRINIC.`,
e `--version` imprime `api-afrinic dev (commit unknown, build unknown)` num
build sem `-ldflags`.

## Operação

```bash
curl http://127.0.0.1:8102/afrinic/status          # sem Traefik, pela porta do loopback
curl http://127.0.0.1:8102/afrinic/asn/37100
make -C apps/afrinic/api smoke                      # /status, /meta, /asn/37100 e /openapi.yaml
make -C apps/afrinic/api logs
```

Arquivo real ([../rir/api.md](../rir/api.md#arquivo-real-make-test-real)); o
`curl` leva ~10 s, porque o servidor da AFRINIC é lento
([fonte.md](fonte.md#servidor)):

```bash
curl -o /tmp/delegated https://ftp.afrinic.net/pub/stats/afrinic/delegated-afrinic-extended-latest
make -C apps/afrinic/api test-real FILE=/tmp/delegated   # AFRINIC_REAL_FILE
```

## Medições

Arquivo real de 2026-09-28 (19.786 registros; 4.350 registros de ASN e
15.530 blocos gravados — 6.185 IPv4 e 9.345 IPv6):

| Medição | Valor |
|---|---|
| Carga pelo `collector-afrinic` (no `make test-real`, PG18 descartável, arquivo servido localmente) | ~0,65 s; baixando do servidor da AFRINIC, ~9 s ([fonte.md](fonte.md#servidor)) |
| Uma consulta no Postgres | `/asn` ~0,05 ms (índice único de `asn_start`); `/ip` ~0,15 ms (GiST) |
| `make test-real` (handler completo, sem cache) | 500 `/ip` aleatórios em 0,27 ms de média; 500 `/asn` em 0,16 ms; o maior titular (`F3619C8C`, 16 KB) em 3 ms |
| Por HTTP, binário no host e Postgres/Valkey no Docker Desktop | 1.000 `/ip` aleatórios em ~1 ms de média sem cache e ~0,4 ms com cache (quase tudo é o ida e volta ao Docker); com a máquina carregada por outros testes, 27 ms e 14 ms |

Conferido de ponta a ponta em 2026-09-28, com os dados reais da AFRINIC: com
o Valkey parado, `/status` fica `degraded` e as rotas seguem respondendo (5
tentativas de ~50 ms; depois o disjuntor abre e as respostas voltam a
~2 ms); com o Postgres parado, `/status` responde 503 e as consultas fora do
cache, `503 database_unavailable` — com o Postgres congelado, sem recusar a
conexão, `504 timeout` depois de `DB_TIMEOUT` —, enquanto as que estão no
cache continuam 200. Os dois voltam sozinhos.

## Testes

Os do modelo, sem nada da AFRINIC além dos dados:
[../rir/api.md](../rir/api.md#testes). O store falso e o seed do teste de
integração usam registros do recorte do modelo
([../lacnic/fonte.md](../lacnic/fonte.md#recorte-testdatadelegated-extended-sampletxt):
AS61613, AS28003–AS28005, titulares `258500` e `130343`), iguais nos clones —
não o recorte da AFRINIC ([fonte.md](fonte.md#recorte-testdatadelegated-extended-sampletxt)),
que é do coletor.

Com o arquivo da AFRINIC, o `make test-real`
([../rir/api.md](../rir/api.md#arquivo-real-make-test-real)) pula a amostra
do meio da maior faixa — nenhum registro de ASN tem `count` ≥ 2, e o teste
loga `sem amostra para ...` —, acha um pedaço de registro dividido (há 52
registros não-CIDR) e mede o maior titular, `F3619C8C`.
