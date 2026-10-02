# API da ARIN (`api-arin`)

A `api-arin` é um clone da `api-lacnic`, o modelo das APIs de RIR: o
comportamento está em [../../padroes/api.md](../../padroes/api.md),
[../../padroes/openapi.md](../../padroes/openapi.md) e
[../rir/api.md](../rir/api.md), sem nada de diferente. O código é o do modelo
com o nome trocado (conferido em 2026-09-29: fora `internal/rir/rir.go`, a
porta, o `SMOKE_ASN` do `Makefile`, o manifesto e o `README.md`, os arquivos
são os da `api-lacnic` depois do `sed` da
[clonagem](../rir/api.md#clonar-a-api-para-outro-rir)). Aqui, os valores, as
particularidades que mudam as respostas, as respostas reais e as medições da
ARIN. Dono: sub-agente `api-arin`.

## Valores

| Item | Valor |
|---|---|
| `internal/rir/rir.go` | `Source` `arin`, `Title` `ARIN`, `SourceURL` `https://ftp.arin.net/pub/stats/arin/delegated-arin-extended-latest`, `OpaqueIDChangesDaily` `false` |
| Caminho | `BASE_PATH` `/arin`; em produção, `https://api.badblock.net.br/arin/` |
| Porta no loopback | 8104 (`API_ARIN_HOST_PORT`, `PORT` do `Makefile`): `http://127.0.0.1:8104/arin/` |
| `SMOKE_ASN` | 7018 (US, assigned, titular `81e05477cc28a48ed3088e3408139c2a`) |
| Tabelas lidas | `arin_run`, `arin_asn` e `arin_prefix` ([dados.md](dados.md)) e a linha `collector-arin` de `jobs` |
| `.env` | `API_ARIN_TAG=latest`, `API_ARIN_HOST_PORT=8104`, `API_ARIN_CACHE_TTL=3600`; opcionais `API_ARIN_CACHE_ENABLED` (`true`), `API_ARIN_DB_POOL_MAX` (`10`) e `API_ARIN_REDIS_URL` |
| Imagem e container | `tmsoftbrasil/badblock-api-arin`, label `description` `API HTTP das delegações de ASNs e blocos IP do RIR ARIN (delegated-extended), do BadBlock`; `badblock-api-arin` |
| Traefik | router e serviço `badblock-api-arin`, middleware `badblock-api-arin-ratelimit`, regra `Host(api.badblock.net.br) && (Path(/arin) \|\| PathPrefix(/arin/))` ([molde](../../plataforma/publicacao.md#traefik)) |

## Particularidades

Nada disso é código: a API é a do modelo, e o que muda é o dado da ARIN (os
fatos do arquivo de 2026-09-28 estão em
[fonte.md](fonte.md#fatos-medidos-arquivo-de-2026-09-28-serial-1790600421096)
e [dados.md](dados.md#o-que-as-tabelas-guardam)). Nas respostas:

| Onde | Na ARIN |
|---|---|
| `status` | não distingue alocação de designação: todo ASN delegado vem `assigned` e todo bloco delegado, `allocated` (os outros, `available` ou `reserved`). Aqui `allocated` não quer dizer "a um LIR/ISP", nem `assigned` "a um usuário final" |
| `cc` | `null` em todo available/reserved; nunca `ZZ` |
| `reg_date` | `null` em available/reserved e nos 108 ASNs assigned com data `00000000` na fonte (ex.: AS3) |
| `opaque_id` | hash hex minúsculo de 32 caracteres, `null` em available/reserved; pedido em maiúsculas, acha o titular ([Titular](#titular)) |
| `range` (`/asn`) | 143 registros assigned são faixas, de até 290 ASNs (ex.: AS5120 + 257), e o único registro available de ASN é a faixa AS403010 + 1.371 |
| `record` (`/ip`, `/prefix`) | 2.765 registros IPv4, todos reserved (lacunas entre blocos alocados), não formam CIDR e viram 6.652 blocos, cada um com o `record` do registro inteiro ([IP](#ip)); os outros formam CIDR de `/8` a `/24`, e os IPv6 vão de `/13` a `/48` |
| `/holder` | nenhum dos 39.138 titulares tem mais de um país (2026-09-28; o mesmo com o arquivo de 2026-09-29), então `ccs` traz sempre um país só; o maior titular dos cinco RIRs é da ARIN ([Titular](#titular)) |
| `/meta` | `serial` é a época Unix em milissegundos da geração do arquivo (13 dígitos), `start_date` é `1970-01-01` e `end_date`, o dia da publicação; `prefixes_v4` > `ipv4_records` por causa dos registros divididos |
| 404 de `/asn` | o AS1 existe na ARIN: o exemplo do 404 usa o AS3333, do RIPE NCC |

## Exemplos

Respostas reais com o arquivo de 2026-09-28 (serial 1790600421096,
[fonte.md](fonte.md#fatos-medidos-arquivo-de-2026-09-28-serial-1790600421096))
carregado pelo `collector-arin`: dataset
`01a0eab5-38a4-7bd6-a4c6-e1a66f55c539`, aplicado em 2026-09-29T01:08:58Z
(`01a0eab5-…` nos exemplos curtos). São também os exemplos do manifesto. O
que cada campo quer dizer está em [../rir/api.md](../rir/api.md). Refeitas em
2026-09-29 no stack local com o arquivo de 2026-09-29 (serial 1790686820753):
as mesmas respostas, a menos da versão do dataset, dos horários, da meta, da
`version` do binário (`dev`, sem tag) e da faixa de `/arin/asn/403500`
(abaixo).

### ASN

`/arin/asn/7018` (o mesmo em `/arin/v1/asn/AS7018`):

```json
{
  "asn": 7018,
  "range": {"start": 7018, "end": 7018, "count": 1},
  "cc": "US", "reg_date": "1996-07-30", "status": "assigned",
  "opaque_id": "81e05477cc28a48ed3088e3408139c2a",
  "first_seen": "2026-09-29T01:08:58Z", "updated_at": "2026-09-29T01:08:58Z",
  "dataset": {"version": "01a0eab5-38a4-7bd6-a4c6-e1a66f55c539", "updated_at": "2026-09-29T01:08:58Z"}
}
```

ASN no meio de uma faixa assigned (`/arin/asn/5200`):

```json
{
  "asn": 5200,
  "range": {"start": 5120, "end": 5376, "count": 257},
  "cc": "US", "reg_date": "1995-05-09", "status": "assigned",
  "opaque_id": "45fe880b68a8f2850ebdcfdc57b4556c",
  "first_seen": "2026-09-29T01:08:58Z", "updated_at": "2026-09-29T01:08:58Z",
  "dataset": {"version": "01a0eab5-…", "updated_at": "2026-09-29T01:08:58Z"}
}
```

A faixa available: `/arin/asn/403500` responde
`"range": {"start": 403010, "end": 404380, "count": 1371}`, `status`
`available` e `cc`, `reg_date` e `opaque_id` `null`. No arquivo de 2026-09-29
a ARIN já tinha designado AS403010–AS403012 (data 2026-09-28), e a mesma
consulta respondia `{"start": 403013, "end": 404380, "count": 1368}`.

### IP

`/arin/ip/12.34.56.78`:

```json
{
  "ip": "12.34.56.78",
  "prefix": "12.0.0.0/8",
  "cc": "US", "reg_date": "1983-08-23", "status": "allocated",
  "opaque_id": "81e05477cc28a48ed3088e3408139c2a",
  "record": {"start": "12.0.0.0", "value": 16777216},
  "dataset": {"version": "01a0eab5-…", "updated_at": "2026-09-29T01:08:58Z"}
}
```

`/arin/ip/2001:1890::1`:

```json
{
  "ip": "2001:1890::1",
  "prefix": "2001:1890::/29",
  "cc": "US", "reg_date": "2003-10-27", "status": "allocated",
  "opaque_id": "81e05477cc28a48ed3088e3408139c2a",
  "record": {"start": "2001:1890::", "value": 29},
  "dataset": {"version": "01a0eab5-…", "updated_at": "2026-09-29T01:08:58Z"}
}
```

Um pedaço de registro IPv4 que não forma CIDR (`/arin/ip/23.128.6.10`):

```json
{
  "ip": "23.128.6.10",
  "prefix": "23.128.6.0/23",
  "cc": null, "reg_date": null, "status": "reserved", "opaque_id": null,
  "record": {"start": "23.128.5.0", "value": 1792},
  "dataset": {"version": "01a0eab5-…", "updated_at": "2026-09-29T01:08:58Z"}
}
```

O registro `23.128.5.0` + 1792 endereços é `/24` + `/23` + `/22`
(`23.128.5.0/24`, `23.128.6.0/23`, `23.128.8.0/22`), e os três pedaços trazem
o mesmo `record`. Num registro que forma CIDR, `record.start` é o endereço do
próprio `prefix` e `record.value`, o tamanho do bloco (`12.0.0.0/8`:
16777216).

### Prefixo

`/arin/prefix/12.34.56.78/16` (consulta `12.34.0.0/16`: bits de host zerados):

```json
{
  "query": "12.34.0.0/16",
  "prefix": "12.0.0.0/8",
  "exact": false,
  "cc": "US", "reg_date": "1983-08-23", "status": "allocated",
  "opaque_id": "81e05477cc28a48ed3088e3408139c2a",
  "record": {"start": "12.0.0.0", "value": 16777216},
  "dataset": {"version": "01a0eab5-…", "updated_at": "2026-09-29T01:08:58Z"}
}
```

- `/arin/prefix/12.0.0.0/8` responde o mesmo bloco com
  `"query": "12.0.0.0/8"` e `"exact": true`.
- Em IPv6, `/arin/prefix/2001:1890:1::/48` acha `2001:1890::/29`:
  `"query": "2001:1890:1::/48"`, `"exact": false` e os campos de
  `/arin/ip/2001:1890::1`.
- Num registro dividido, `/arin/prefix/23.128.6.0/23` é exato (o pedaço é um
  bloco próprio): `"query"` e `"prefix"` `23.128.6.0/23`, `"exact": true` e os
  campos de `/arin/ip/23.128.6.10`, com `record` apontando para o registro
  inteiro (`23.128.5.0` + 1792).

### Titular

`/arin/holder/e0082a77a634f2cc5817ffb7de12b38e` (o titular de AS11472):

```json
{
  "opaque_id": "e0082a77a634f2cc5817ffb7de12b38e",
  "cc": "US",
  "ccs": ["US"],
  "counts": {"asns": 1, "ipv4": 2, "ipv6": 1},
  "asns": [
    {"start": 11472, "end": 11472, "count": 1, "cc": "US", "status": "assigned", "reg_date": "2008-12-18"}
  ],
  "prefixes": {
    "ipv4": [
      {"prefix": "208.65.32.0/22", "cc": "US", "status": "allocated", "reg_date": "2014-07-01"},
      {"prefix": "216.7.64.0/20", "cc": "US", "status": "allocated", "reg_date": "2009-01-14"}
    ],
    "ipv6": [
      {"prefix": "2605:8480::/32", "cc": "US", "status": "allocated", "reg_date": "2014-06-11"}
    ]
  },
  "dataset": {"version": "01a0eab5-…", "updated_at": "2026-09-29T01:08:58Z"}
}
```

- O titular de AS7018 e de `12.0.0.0/8`, `81e05477cc28a48ed3088e3408139c2a`,
  responde `"counts": {"asns": 70, "ipv4": 451, "ipv6": 14}`, em 45 KB.
  Pedido em maiúsculas (`/arin/holder/81E05477CC28A48ED3088E3408139C2A`), acha
  o mesmo titular e responde o valor gravado, em minúsculas — com outra chave
  de cache e outro ETag ([abaixo](#chaves-de-cache-e-etags)).
- O maior titular do arquivo, e dos cinco RIRs em 2026-09-28
  ([modelo](../rir/api.md#titular)), é `45fe880b68a8f2850ebdcfdc57b4556c` (o
  de AS5120 + 257 e `6.0.0.0/8`, no
  [recorte](fonte.md#recorte-testdatadelegated-extended-sampletxt)): 152
  registros de ASN, 2.259 blocos IPv4 e 23 IPv6, 203.731 bytes (≈ 204 KB),
  muito abaixo dos 8 MiB do cache. Os mesmos números e bytes com o arquivo de
  2026-09-29.

### Meta

`/arin/meta`:

```json
{
  "app": "api-arin",
  "version": "0.1.0",
  "dataset": {
    "version": "01a0eab5-38a4-7bd6-a4c6-e1a66f55c539", "updated_at": "2026-09-29T01:08:58Z",
    "source": "https://ftp.arin.net/pub/stats/arin/delegated-arin-extended-latest",
    "sha256": "7e8e5f0bcf02cad5043a6cbdba4006f1c50c7ac0d70a90dfd04a2ebf0cff4550",
    "md5": "0bb8d65f1ff9d820f4bb56844f9948be",
    "serial": "1790600421096", "start_date": "1970-01-01", "end_date": "2026-09-28",
    "asn_records": 32988, "ipv4_records": 80850, "ipv6_records": 89213,
    "prefixes_v4": 84737, "prefixes_v6": 89213
  },
  "collector": {
    "app": "collector-arin",
    "last_sync_at": "2026-09-29T01:08:58Z", "last_check_at": "2026-09-29T01:08:58Z",
    "consolidated": false
  }
}
```

O `serial` `1790600421096` é 2026-09-28 13:00:21 UTC (época em ms), e
`prefixes_v4` (84.737) = `ipv4_records` (80.850) − 2.765 registros não-CIDR +
os 6.652 blocos em que eles viram. Antes da primeira carga:
`{"app": "api-arin", "version": "0.1.0", "dataset": null, "collector": null}`.

### Índice

`/arin/` e `/arin/v1/`:

```json
{
  "app": "api-arin", "version": "0.1.0", "registry": "ARIN",
  "base_path": "/arin", "versions": ["v1"],
  "endpoints": [
    "/arin/asn/{asn}", "/arin/ip/{ip}", "/arin/prefix/{ip}/{len}",
    "/arin/holder/{opaque_id}", "/arin/meta", "/arin/status",
    "/arin/openapi.yaml"
  ],
  "source": "https://ftp.arin.net/pub/stats/arin/delegated-arin-extended-latest"
}
```

### Saúde

`/arin/status` (e `/arin/health`, GET ou POST):

```json
{"success": true, "status": "ok", "timestamp": "2026-09-29T01:13:23Z", "message": "api-arin operacional",
 "checks": {"dataset": "ok", "postgres": "ok", "valkey": "ok"}}
```

Os outros estados, como no manifesto (mesmo `timestamp`):

| `status` | HTTP | `message` | `checks` |
|---|---|---|---|
| `starting` | 200 | `aguardando a primeira sincronização do collector-arin` | `dataset` `empty`, `postgres` `ok`, `valkey` `ok` |
| `degraded` | 200 | `Valkey indisponível; respondendo sem cache` | `dataset` `ok`, `postgres` `ok`, `valkey` `error` |
| `error` | 503 (`success: false`) | `PostgreSQL indisponível` | `dataset` `ok`, `postgres` `error`, `valkey` `ok` |

### Erros

Os do manifesto, com as mensagens reais (conferidas de novo em 2026-09-29,
menos o 503):

| Pedido | HTTP | `code` | `message` |
|---|---|---|---|
| `/arin/asn/abc` | 400 | `bad_request` | `ASN inválido: use um número de 0 a 4294967295, com ou sem o prefixo AS` |
| `/arin/ip/x` | 400 | `bad_request` | `endereço IP inválido` |
| `/arin/prefix/12.0.0.0/33` | 400 | `bad_request` | `tamanho de prefixo inválido` |
| `/arin/holder/nao%20existe` | 400 | `bad_request` | `opaque_id inválido: use de 1 a 128 letras, dígitos, ponto, hífen ou sublinhado` |
| `/arin/asn/3333` (ASN do RIPE NCC) | 404 | `not_found` | `AS3333 não consta no arquivo do RIR ARIN` |
| `/arin/ip/192.0.2.1` | 404 | `not_found` | `192.0.2.1 não pertence a nenhum bloco no arquivo do RIR ARIN` |
| `/arin/prefix/192.0.2.0/24` | 404 | `not_found` | `192.0.2.0/24 não está contido em nenhum bloco no arquivo do RIR ARIN` |
| `/arin/holder/999999` | 404 | `not_found` | `o titular 999999 não consta no arquivo do RIR ARIN` |
| `/arin/nada` | 404 | `not_found` | `rota inexistente; veja /arin/` |
| uma consulta antes da primeira carga | 503 | `dataset_not_ready` | `a primeira sincronização do collector-arin ainda não terminou; tente em alguns minutos` |

### Chaves de cache e ETags

Com o dataset dos exemplos (chave completa:
`badblock:api-arin:01a0eab5-38a4-7bd6-a4c6-e1a66f55c539:<consulta>`); ETags
calculados por `etagFor` em 2026-09-29 (o mesmo cálculo reproduz os ETags que
o stack local responde com a versão dele):

| Pedido | Consulta | ETag |
|---|---|---|
| `/arin/asn/7018`, `/arin/v1/asn/AS7018` | `asn:7018` | `W/"364483959d43c590"` (o exemplo do manifesto) |
| `/arin/asn/5200` | `asn:5200` | `W/"5d313fa519821549"` |
| `/arin/asn/403500` | `asn:403500` | `W/"4fc04285b096162"` (15 dígitos: sem zeros à esquerda) |
| `/arin/ip/12.34.56.78`, `/arin/ip/::ffff:12.34.56.78` | `ip:12.34.56.78` | `W/"6088643db276719b"` |
| `/arin/ip/2001:1890::1` | `ip:2001:1890::1` | `W/"412338ded7c99ba3"` |
| `/arin/ip/23.128.6.10` | `ip:23.128.6.10` | `W/"4ffb92fe22de3106"` |
| `/arin/prefix/12.34.56.78/16`, `/arin/prefix/12.34.0.0/16` | `prefix:12.34.0.0/16` | `W/"66382b6226c7975a"` |
| `/arin/prefix/12.0.0.0/8` | `prefix:12.0.0.0/8` | `W/"8b6221d9bfa9ebae"` |
| `/arin/prefix/2001:1890:1::/48` | `prefix:2001:1890:1::/48` | `W/"2379fcbfb2648465"` |
| `/arin/prefix/23.128.6.0/23` | `prefix:23.128.6.0/23` | `W/"850e1c667a3e325c"` |
| `/arin/holder/e0082a77a634f2cc5817ffb7de12b38e` | `holder:e0082a77a634f2cc5817ffb7de12b38e` | `W/"50075c6e554d3fa4"` |
| `/arin/holder/81e05477cc28a48ed3088e3408139c2a` | `holder:81e05477cc28a48ed3088e3408139c2a` | `W/"24b1507f7e0c55dd"` |
| `/arin/holder/81E05477CC28A48ED3088E3408139C2A` | `holder:81E05477CC28A48ED3088E3408139C2A` | `W/"527503a227afa53d"` (a mesma resposta, outra chave) |

## Manifesto

O do [modelo](../rir/api.md#manifesto) (`OpaqueIDChangesDaily` é `false`: sem
o aviso do opaque-id diário), com `info.version` `'0.1.0'`,
`externalDocs.url`
`https://github.com/patrickbrandao/badblock/tree/main/specs/fontes/arin`,
servidores locais na porta 8104 e os exemplos desta página:

| Parte | Na ARIN |
|---|---|
| parâmetros | `asn` `'7018'` (a descrição cita `7018`, `AS7018` e `as7018`); `ip` `12.34.56.78`; no prefixo, `ip` `12.34.56.78` e `len` `16` (a descrição do `ip`: `12.34.56.78/16` consulta `12.34.0.0/16`); `opaque_id` `'e0082a77a634f2cc5817ffb7de12b38e'` |
| cabeçalhos | `IfNoneMatch` e `ETag` com `W/"364483959d43c590"` (a descrição do `ETag` cita `/arin/asn/7018` e `/arin/v1/asn/AS7018`); `X-Dataset-Version` com a versão dos exemplos |
| respostas | `/asn`: `asn` (7018) e `faixa` (5200); `/ip`: `ipv4`, `ipv6` e `nao_cidr` (23.128.6.10); `/prefix`: `contido` (12.34.56.78/16), `exato` (12.0.0.0/8), `ipv6` (2001:1890:1::/48) e `nao_cidr` (23.128.6.0/23); `/holder`: o de `e0082a77…`; `/meta`: `carregado` e `vazio`; `BadRequest` `len` com `/prefix/12.0.0.0/33`; `NotFound` `asn` com o `summary` `/asn/3333 (ASN do RIPE NCC)`; `timestamp` `2026-09-29T01:13:23Z` em `StatusOK` e `StatusError` |

As particularidades mudam o uso das rotas e ganham texto próprio. Cada texto
abaixo fecha a `description` do modelo — antes do ponto final quando começa
com parêntese, ou depois dele — ou entra onde a tabela diz:

| Onde | Texto |
|---|---|
| `info.description`, parágrafo novo depois do primeiro | Particularidades da ARIN: o `status` não distingue alocação de designação (ASNs em uso vêm sempre `assigned` e blocos em uso sempre `allocated`); o `opaque_id` é um hash hex minúsculo de 32 caracteres; `cc` vem `null` (nunca `ZZ`) em available/reserved; o `serial` da meta é a época Unix em milissegundos; e registros IPv4 reservados que não formam CIDR viram vários blocos, cada um com o mesmo `record`. |
| `/asn/{asn}`, `get` | Na ARIN, ASNs em uso vêm sempre `assigned`, e 143 registros designados são faixas (até 290 ASNs). |
| `/asn/{asn}`, 404 (no lugar do `por exemplo)` do modelo) | por exemplo `AS3333`). |
| `/ip/{ip}`, `get` | Na ARIN, 2.765 registros IPv4 (todos `reserved`) não formam CIDR e viram 6.652 blocos; o exemplo `nao_cidr` mostra um desses pedaços. |
| `/ip/{ip}`, exemplo `nao_cidr` (`summary` e `description`) | Pedaço de um registro IPv4 que não forma CIDR (/ip/23.128.6.10) — O registro `23.128.5.0` + 1792 endereços vira `/24` + `/23` + `/22` (`23.128.5.0/24`, `23.128.6.0/23`, `23.128.8.0/22`); cada pedaço traz o mesmo `record`. |
| `/prefix/{ip}/{len}`, `get` (no lugar do exemplo IPv6 da LACNIC) | IPv6 também funciona: `/prefix/2001:1890:1::/48`. Num registro IPv4 que não forma CIDR, o pedaço é um bloco próprio (`exact` verdadeiro), com `record` apontando para o registro inteiro. |
| `/prefix/{ip}/{len}`, `summary` dos exemplos `ipv6` e `nao_cidr` | IPv6 contido num bloco maior (/prefix/2001:1890:1::/48) — Pedaço de um registro IPv4 que não forma CIDR (/prefix/23.128.6.0/23) |
| `/holder/{opaque_id}`, parâmetro: o formato hex de 32 | hex minúsculo de 32 (o da ARIN, `45fe880b68a8f2850ebdcfdc57b4556c`) |
| `/holder/{opaque_id}`, parâmetro: o exemplo de caixa (no lugar do `f3619c8c` do modelo) | (`81E05477CC28A48ED3088E3408139C2A` acha `81e05477cc28a48ed3088e3408139c2a`) |
| `/holder/{opaque_id}`, `get` (no lugar do ponto final) | ; na ARIN, nenhum titular tem mais de um país. O maior titular dos cinco RIRs é da ARIN (`45fe880b68a8f2850ebdcfdc57b4556c`: 152 registros de ASN, 2.259 blocos IPv4 e 23 IPv6, ~204 KB). |
| `CountryCode` | Na ARIN, nunca vem `ZZ`. |
| `RegDate` | (na ARIN, 108 ASNs antigos designados têm `00000000`, ex.: AS3) |
| `DelegationStatus` | A ARIN não distingue alocação de designação: ASNs em uso vêm sempre `assigned` e blocos em uso sempre `allocated`. |
| `OpaqueID` | Na ARIN, hash hex minúsculo de 32 caracteres. |
| `IPPrefix` (no lugar dos exemplos da LACNIC) | (ex. `12.0.0.0/8`, `2001:1890::/29`) |
| `Record` | Na ARIN (2026-09-28), 2.765 registros IPv4 não formam CIDR (todos `reserved`, lacunas entre blocos alocados) e viram 6.652 blocos: `23.128.5.0` + 1792 endereços vira `23.128.5.0/24`, `23.128.6.0/23` e `23.128.8.0/22`, os três com `record: {start: 23.128.5.0, value: 1792}`. |
| `MetaDataset.serial` | Na ARIN, a época Unix em milissegundos da geração do arquivo (`1790600421096` = 2026-09-28 13:00:21 UTC). |
| `MetaDataset.start_date` | (na ARIN, `1970-01-01`) |

## `--help`

A saída real é a do [modelo](../lacnic/api.md#--help), no stderr, com
`api-arin`, `RIR ARIN`, `arin_*`, `collector-arin` e `/arin` (inclusive o
padrão de `--base-path`) e nada mais diferente: conferido em 2026-09-29,
versão `dev`, com `diff` contra a do modelo depois de trocar os nomes. A
primeira linha é
`api-arin — API HTTP das delegações de ASNs e blocos IP do RIR ARIN.`, e o
`--version` num build sem `-ldflags` imprime
`api-arin dev (commit unknown, build unknown)`.

## Operação

```bash
curl http://127.0.0.1:8104/arin/status          # sem Traefik, pela porta do loopback
curl http://127.0.0.1:8104/arin/asn/7018
make -C apps/arin/api smoke                      # /status, /meta, /asn/7018 e /openapi.yaml
make -C apps/arin/api logs
```

Arquivo real ([../rir/api.md](../rir/api.md#arquivo-real-make-test-real)):

```bash
curl -o /tmp/delegated https://ftp.arin.net/pub/stats/arin/delegated-arin-extended-latest
make -C apps/arin/api test-real FILE=/tmp/delegated   # ARIN_REAL_FILE
```

## Medições

Arquivo real de 2026-09-28 (203.051 registros; 32.988 registros de ASN,
84.737 blocos IPv4 e 89.213 IPv6 gravados), num Mac com outros testes
rodando em paralelo — os números variam até 10× com a carga:

| Medição | Valor |
|---|---|
| Carga pelo `collector-arin` (no `make test-real`, PG18 descartável) | ~2,4 s com o arquivo servido localmente (~11 s baixando da ARIN) |
| Uma consulta no Postgres | `/asn` ~0,1 ms (índice único de `asn_start`); `/ip` ~0,2–1 ms (GiST) |
| `make test-real` (handler completo, sem cache) | 500 `/ip` aleatórios em 0,40 ms de média; 500 `/asn` em 0,18 ms; o maior titular (`45fe880b…`, 204 KB) em 14 ms |
| Por HTTP, binário no host e Postgres/Valkey no Docker Desktop | 1.000 `/ip` aleatórios em 0,7–0,9 ms de média sem cache; com cache, 1,6 ms na primeira passada (`MISS`: Postgres e gravação no Valkey) e 0,4 ms na segunda (`HIT`) |

Conferido de ponta a ponta em 2026-09-28, com o arquivo real da ARIN: com o
Valkey parado, `/status` fica `degraded` e as rotas seguem respondendo (as
primeiras levam 50–150 ms; depois o disjuntor abre e as respostas voltam a
~2 ms); com o Postgres parado, `/status` responde 503 e as consultas fora do
cache, `503 database_unavailable`, enquanto as que estão no cache continuam
200. Os dois voltam sozinhos.

## Testes

Os do modelo, sem nada da ARIN: [../rir/api.md](../rir/api.md#testes)
(conferido em 2026-09-29: os arquivos de teste são os da `api-lacnic` com o
nome trocado, e o app não tem `testdata/`). O store falso e o seed do teste de
integração usam os registros do
[recorte da LACNIC](../lacnic/fonte.md#recorte-testdatadelegated-extended-sampletxt)
(AS61613, AS28003–AS28005, titulares `258500` e `130343`), iguais nos cinco —
não os da ARIN. Com o arquivo da ARIN, o `make test-real`
([Operação](#operação)) tem pedaços de registro dividido para a amostra de
`/ip`, e o maior titular que ele confere contra 1/10 dos 8 MiB do cache é o
`45fe880b…` (203.731 bytes), o maior dos cinco RIRs.
