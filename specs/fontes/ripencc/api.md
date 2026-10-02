# API do RIPE NCC (`api-ripencc`)

A `api-ripencc` é um clone da `api-lacnic`, o modelo das APIs de RIR: o
comportamento está em [../../padroes/api.md](../../padroes/api.md),
[../../padroes/openapi.md](../../padroes/openapi.md) e
[../rir/api.md](../rir/api.md). O código é o do modelo com o nome trocado
(conferido em 2026-09-29, comparando com a `api-lacnic` depois do `sed` da
[clonagem](../rir/api.md#clonar-a-api-para-outro-rir)); o que é do RIPE NCC
está em `internal/rir/rir.go`, no `Makefile`, nos arquivos de deploy e no
manifesto. Aqui, os valores, o que o opaque-id diário muda na API, as
respostas reais e as medições do RIPE NCC. Dono: sub-agente `api-ripencc`.

## Valores

| Item | Valor |
|---|---|
| `internal/rir/rir.go` | `Source` `ripencc`, `Title` `RIPE NCC` (não `RIPENCC`), `SourceURL` `https://ftp.ripe.net/pub/stats/ripencc/delegated-ripencc-extended-latest` (host `ftp.ripe.net`; o `sed` da clonagem daria `ftp.ripencc.net`), `OpaqueIDChangesDaily` `true` — o único dos cinco RIRs ([parâmetros](../rir/README.md#parâmetros-das-apis)) |
| Caminho | `BASE_PATH` `/ripencc`; em produção, `https://api.badblock.net.br/ripencc/` |
| Porta no loopback | 8106 (`API_RIPENCC_HOST_PORT`, `PORT` do `Makefile`): `http://127.0.0.1:8106/ripencc/` |
| `SMOKE_ASN` | 3333 (NL, allocated; o AS do próprio RIPE NCC) |
| Tabelas lidas | `ripencc_run`, `ripencc_asn` e `ripencc_prefix` ([dados.md](dados.md); consultas em [../rir/dados.md](../rir/dados.md#consultas-da-api)) e a linha `collector-ripencc` de `jobs` |
| `.env` | `API_RIPENCC_TAG=latest`, `API_RIPENCC_HOST_PORT=8106`, `API_RIPENCC_CACHE_TTL=3600`; opcionais `API_RIPENCC_CACHE_ENABLED` (`true`), `API_RIPENCC_DB_POOL_MAX` (`10`) e `API_RIPENCC_REDIS_URL` |
| Imagem e container | `tmsoftbrasil/badblock-api-ripencc`, label `description` `API HTTP das delegações de ASNs e blocos IP do RIPE NCC (delegated-extended), do BadBlock` ("do RIPE NCC", não "do RIR RIPENCC"); `badblock-api-ripencc` |
| Traefik | router e serviço `badblock-api-ripencc`, middleware `badblock-api-ripencc-ratelimit`, regra `Host(api.badblock.net.br) && (Path(/ripencc) \|\| PathPrefix(/ripencc/))` ([molde](../../plataforma/publicacao.md#traefik)) |

## Opaque-id diário

O RIPE NCC gera um UUID **novo para cada titular a cada arquivo diário**
([fonte.md](fonte.md#opaque-id-novo-a-cada-arquivo)): de 2026-09-27 para
2026-09-28 nenhum UUID se repetiu, e o do AS3333 foi `48d99710-…` no arquivo
de 2026-09-21, `87debb42-f92a-447a-ae52-ae9ff30e6f5b` no de 2026-09-27 e
`c949c45e-fcab-480a-ba5b-804b0addae55` no de 2026-09-28. Dentro de um arquivo
o agrupamento vale (os recursos de um titular têm o mesmo UUID). O código é o
do modelo; o que muda sai de `OpaqueIDChangesDaily` e dos textos
([dados.md](dados.md#opaque_id-e-updated_at)):

- `/ripencc/holder/{opaque_id}` **só vale dentro do dataset atual**: um
  opaque-id guardado ontem dá 404 hoje, com a mensagem que explica e aponta
  `/ripencc/ip/{ip}` e `/ripencc/asn/{asn}` ([Erros](#erros); texto do
  [modelo](../rir/api.md#titular)).
- A API **nunca** apresenta o opaque-id como identificador permanente do
  titular (nem no manifesto, nas mensagens, nas specs ou no README). Quem
  consulta não o guarda nem o publica: guarda um ASN ou IP do titular e obtém
  o opaque-id atual por `/asn` ou `/ip` antes de chamar `/holder`
  ([Operação](#operação)).
- A chave `holder:<opaque_id>` e o ETag levam a versão do dataset, que muda a
  cada arquivo aplicado junto com os opaque-ids: uma resposta de `/holder`
  nunca sobrevive à troca de arquivo, e as chaves `holder:` de um dia nunca
  são lidas no outro.
- Em `/asn`, `updated_at` muda **todo dia** em quase todo registro
  allocated/assigned (o `MERGE` do coletor conta o opaque-id novo como
  alteração) e não quer dizer que país, data, status ou tamanho mudaram;
  `first_seen` não é afetado.
- Em `/meta`, `collector.consolidated` volta a `false` a cada arquivo diário
  (toda aplicação altera linhas, [collector.md](collector.md#atualização-diária)).

## Exemplos

Respostas reais com o arquivo de 2026-09-28 (serial 1790632799,
[fonte.md](fonte.md#fatos-medidos-arquivo-de-2026-09-28-serial-1790632799))
carregado pelo `collector-ripencc`: dataset
`01a0eab5-6ada-7d2d-bb6b-90785b30dd1e`, aplicado em 2026-09-29T01:09:10Z
(`01a0eab5-…` nos exemplos curtos). São também os exemplos do manifesto. Os
opaque-ids são os desse arquivo; nos outros dias, os mesmos titulares têm
outros UUIDs. O que cada campo quer dizer está em [../rir/api.md](../rir/api.md);
o que os campos de delegação trazem no RIPE NCC (`cc` e `reg_date` `null` em
todo available/reserved, `EU` e nunca `ZZ`, `1970-01-01` só em AS6204 e
AS6206), em [dados.md](dados.md#o-que-as-tabelas-guardam).

### ASN

`/ripencc/asn/3333` (o mesmo em `/ripencc/v1/asn/AS3333`):

```json
{
  "asn": 3333,
  "range": {"start": 3333, "end": 3333, "count": 1},
  "cc": "NL", "reg_date": "1994-05-19", "status": "allocated",
  "opaque_id": "c949c45e-fcab-480a-ba5b-804b0addae55",
  "first_seen": "2026-09-29T01:09:10Z", "updated_at": "2026-09-29T01:09:10Z",
  "dataset": {"version": "01a0eab5-6ada-7d2d-bb6b-90785b30dd1e", "updated_at": "2026-09-29T01:09:10Z"}
}
```

ASN disponível (`/ripencc/asn/1877`):

```json
{"asn": 1877, "range": {"start": 1877, "end": 1877, "count": 1},
 "cc": null, "reg_date": null, "status": "available", "opaque_id": null,
 "first_seen": "2026-09-29T01:09:10Z", "updated_at": "2026-09-29T01:09:10Z",
 "dataset": {"version": "01a0eab5-…", "updated_at": "2026-09-29T01:09:10Z"}}
```

No RIPE NCC todo registro tem um ASN só (`count` 1 nos 48.682 registros de
2026-09-28): não há ASN no meio de uma faixa, e o exemplo `disponivel` do
manifesto ocupa o lugar do `faixa` do modelo.

### IP

`/ripencc/ip/193.0.6.139`:

```json
{
  "ip": "193.0.6.139",
  "prefix": "193.0.0.0/20",
  "cc": "NL", "reg_date": "1993-09-01", "status": "allocated",
  "opaque_id": "c949c45e-fcab-480a-ba5b-804b0addae55",
  "record": {"start": "193.0.0.0", "value": 4096},
  "dataset": {"version": "01a0eab5-…", "updated_at": "2026-09-29T01:09:10Z"}
}
```

`/ripencc/ip/2001:67c:2e8::1`:

```json
{"ip": "2001:67c:2e8::1", "prefix": "2001:67c:2e8::/48",
 "cc": "NL", "reg_date": "2010-09-17", "status": "assigned", "opaque_id": "c949c45e-fcab-480a-ba5b-804b0addae55",
 "record": {"start": "2001:67c:2e8::", "value": 48},
 "dataset": {"version": "01a0eab5-…", "updated_at": "2026-09-29T01:09:10Z"}}
```

Pedaço de um registro IPv4 que não forma CIDR (970 registros assim, em 2.515
blocos, no arquivo de 2026-09-28): `87.116.83.0` + 2304 endereços vira
`87.116.83.0/24` + `87.116.84.0/22` + `87.116.88.0/22`, e
`/ripencc/ip/87.116.90.1` responde (exemplo `nao_cidr` do manifesto; é o
registro do titular do AS9070 no [recorte](fonte.md#recorte-testdatadelegated-extended-sampletxt)
do coletor):

```json
{"ip": "87.116.90.1", "prefix": "87.116.88.0/22",
 "cc": "BG", "reg_date": "2005-09-13", "status": "allocated", "opaque_id": "162f9494-4727-4143-89e3-1a5a64454c0d",
 "record": {"start": "87.116.83.0", "value": 2304},
 "dataset": {"version": "01a0eab5-…", "updated_at": "2026-09-29T01:09:10Z"}}
```

### Prefixo

`/ripencc/prefix/193.0.6.0/24` (e `/ripencc/prefix/193.0.6.139/24`, que
consulta o mesmo `193.0.6.0/24`):

```json
{"query": "193.0.6.0/24", "prefix": "193.0.0.0/20", "exact": false,
 "cc": "NL", "reg_date": "1993-09-01", "status": "allocated", "opaque_id": "c949c45e-fcab-480a-ba5b-804b0addae55",
 "record": {"start": "193.0.0.0", "value": 4096},
 "dataset": {"version": "01a0eab5-…", "updated_at": "2026-09-29T01:09:10Z"}}
```

`/ripencc/prefix/193.0.0.0/20` responde o mesmo bloco com
`"query": "193.0.0.0/20"` e `"exact": true`; em IPv6,
`/ripencc/prefix/2001:67c:2e8::/48` responde o bloco do `/ip` IPv6 acima,
com `"exact": true`.

### Titular

`/ripencc/holder/c949c45e-fcab-480a-ba5b-804b0addae55` — o próprio RIPE NCC,
titular do AS3333 no arquivo de 2026-09-28:

```json
{
  "opaque_id": "c949c45e-fcab-480a-ba5b-804b0addae55",
  "cc": "NL",
  "ccs": ["NL"],
  "counts": {"asns": 6, "ipv4": 8, "ipv6": 5},
  "asns": [
    {"start": 3333, "end": 3333, "count": 1, "cc": "NL", "status": "allocated", "reg_date": "1994-05-19"},
    {"start": 12654, "end": 12654, "count": 1, "cc": "NL", "status": "allocated", "reg_date": "1999-09-15"},
    {"start": 25152, "end": 25152, "count": 1, "cc": "NL", "status": "allocated", "reg_date": "2002-08-09"},
    {"start": 196615, "end": 196615, "count": 1, "cc": "NL", "status": "allocated", "reg_date": "2007-03-19"},
    {"start": 197000, "end": 197000, "count": 1, "cc": "NL", "status": "allocated", "reg_date": "2010-02-17"},
    {"start": 201965, "end": 201965, "count": 1, "cc": "NL", "status": "allocated", "reg_date": "2015-01-19"}
  ],
  "prefixes": {
    "ipv4": [
      {"prefix": "84.205.64.0/19", "cc": "NL", "status": "assigned", "reg_date": "2004-11-05"},
      {"prefix": "93.175.144.0/21", "cc": "NL", "status": "assigned", "reg_date": "2008-07-16"},
      {"prefix": "93.175.152.0/22", "cc": "NL", "status": "assigned", "reg_date": "2008-07-16"},
      {"prefix": "93.175.156.0/23", "cc": "NL", "status": "assigned", "reg_date": "2008-07-16"},
      {"prefix": "93.175.158.0/24", "cc": "NL", "status": "assigned", "reg_date": "2008-07-16"},
      {"prefix": "93.175.159.0/24", "cc": "NL", "status": "assigned", "reg_date": "2008-07-16"},
      {"prefix": "193.0.0.0/20", "cc": "NL", "status": "allocated", "reg_date": "1993-09-01"},
      {"prefix": "193.0.16.0/21", "cc": "NL", "status": "allocated", "reg_date": "1993-09-01"}
    ],
    "ipv6": [
      {"prefix": "2001:67c:2e8::/48", "cc": "NL", "status": "assigned", "reg_date": "2010-09-17"},
      {"prefix": "2001:67c:2d7c::/48", "cc": "NL", "status": "assigned", "reg_date": "2015-01-19"},
      {"prefix": "2001:7fb::/32", "cc": "NL", "status": "assigned", "reg_date": "2007-06-05"},
      {"prefix": "2001:7fd::/32", "cc": "NL", "status": "assigned", "reg_date": "2003-08-29"},
      {"prefix": "2a13:27c0::/29", "cc": "NL", "status": "allocated", "reg_date": "2022-11-10"}
    ]
  },
  "dataset": {"version": "01a0eab5-…", "updated_at": "2026-09-29T01:09:10Z"}
}
```

- O opaque-id do mesmo AS3333 no arquivo da véspera
  (`/ripencc/holder/87debb42-f92a-447a-ae52-ae9ff30e6f5b`) responde 404, com
  a mensagem que explica o [opaque-id diário](#opaque-id-diário)
  ([Erros](#erros)).
- Caixa: o UUID é gravado em minúsculas;
  `/ripencc/holder/C949C45E-FCAB-480A-BA5B-804B0ADDAE55` acha o mesmo titular
  e responde o valor gravado, com outra chave e outro ETag
  ([Chaves de cache e ETags](#chaves-de-cache-e-etags)).
- Países: em 2026-09-28, só 3 dos 44.811 titulares tinham dois países. Ex.:
  `dcce4e70-b9d0-4aac-a8d5-44508931525d`, com 43 recursos `NO` e 1 `GB` →
  `"cc": "NO", "ccs": ["NO", "GB"]`.
- O maior titular do arquivo é `422db66e-88a2-489c-bf20-66c96d820c91` (6
  registros de ASN, 445 blocos IPv4 e 353 IPv6, 804 itens, todos `SC`): 67 KB.

### Meta

`/ripencc/meta`:

```json
{
  "app": "api-ripencc", "version": "0.1.0",
  "dataset": {
    "version": "01a0eab5-6ada-7d2d-bb6b-90785b30dd1e", "updated_at": "2026-09-29T01:09:10Z",
    "source": "https://ftp.ripe.net/pub/stats/ripencc/delegated-ripencc-extended-latest",
    "sha256": "9c094c7457a544ef813e95e54f28aa45f425ae7ca97fcc9c15c5bb580d0c128f",
    "md5": "9dc7efb094d86f3d03e3cfab51716cd1",
    "serial": "1790632799", "start_date": "1970-01-01", "end_date": "2026-09-28",
    "asn_records": 48682, "ipv4_records": 100877, "ipv6_records": 111234,
    "prefixes_v4": 102422, "prefixes_v6": 111234
  },
  "collector": {
    "app": "collector-ripencc", "last_sync_at": "2026-09-29T01:09:10Z",
    "last_check_at": "2026-09-29T01:09:10Z", "consolidated": false
  }
}
```

- `serial` é a época Unix em segundos: `1790632799` = 2026-09-28 23:59:59
  `+0200`, o último segundo do dia do arquivo no fuso do cabeçalho
  ([fonte.md](fonte.md#formato)).
- `prefixes_v4` (102.422) passa de `ipv4_records` (100.877) porque os 970
  registros IPv4 que não formam CIDR viram 2.515 blocos; no IPv6 são iguais.
- `consolidated` volta a `false` a cada arquivo diário
  ([Opaque-id diário](#opaque-id-diário)).

Antes da primeira carga:
`{"app": "api-ripencc", "version": "0.1.0", "dataset": null, "collector": null}`.

### Índice

`/ripencc/` e `/ripencc/v1/`:

```json
{
  "app": "api-ripencc", "version": "0.1.0", "registry": "RIPE NCC",
  "base_path": "/ripencc", "versions": ["v1"],
  "endpoints": [
    "/ripencc/asn/{asn}", "/ripencc/ip/{ip}", "/ripencc/prefix/{ip}/{len}",
    "/ripencc/holder/{opaque_id}", "/ripencc/meta", "/ripencc/status",
    "/ripencc/openapi.yaml"
  ],
  "source": "https://ftp.ripe.net/pub/stats/ripencc/delegated-ripencc-extended-latest"
}
```

### Saúde

`/ripencc/status` (e `/ripencc/health`, GET ou POST):

```json
{"success": true, "status": "ok", "timestamp": "2026-09-29T01:11:59Z", "message": "api-ripencc operacional",
 "checks": {"dataset": "ok", "postgres": "ok", "valkey": "ok"}}
```

Os outros estados, como no manifesto (mesmo `timestamp`):

| `status` | HTTP | `message` | `checks` |
|---|---|---|---|
| `starting` | 200 | `aguardando a primeira sincronização do collector-ripencc` | `dataset` `empty`, `postgres` `ok`, `valkey` `ok` |
| `degraded` | 200 | `Valkey indisponível; respondendo sem cache` | `dataset` `ok`, `postgres` `ok`, `valkey` `error` |
| `error` | 503 (`success: false`) | `PostgreSQL indisponível` | `dataset` `ok`, `postgres` `error`, `valkey` `ok` |

### Erros

Os do manifesto, com as mensagens reais:

| Pedido | HTTP | `code` | `message` |
|---|---|---|---|
| `/ripencc/asn/abc` | 400 | `bad_request` | `ASN inválido: use um número de 0 a 4294967295, com ou sem o prefixo AS` |
| `/ripencc/ip/x` | 400 | `bad_request` | `endereço IP inválido` |
| `/ripencc/prefix/193.0.0.0/33` | 400 | `bad_request` | `tamanho de prefixo inválido` |
| `/ripencc/holder/nao%20existe` | 400 | `bad_request` | `opaque_id inválido: use de 1 a 128 letras, dígitos, ponto, hífen ou sublinhado` |
| `/ripencc/asn/61613` (ASN da LACNIC) | 404 | `not_found` | `AS61613 não consta no arquivo do RIR RIPE NCC` |
| `/ripencc/ip/192.0.2.1` | 404 | `not_found` | `192.0.2.1 não pertence a nenhum bloco no arquivo do RIR RIPE NCC` |
| `/ripencc/prefix/192.0.2.0/24` | 404 | `not_found` | `192.0.2.0/24 não está contido em nenhum bloco no arquivo do RIR RIPE NCC` |
| `/ripencc/holder/87debb42-f92a-447a-ae52-ae9ff30e6f5b` (opaque-id da véspera) | 404 | `not_found` | `o titular 87debb42-f92a-447a-ae52-ae9ff30e6f5b não consta no arquivo do RIR RIPE NCC; o RIPE NCC gera opaque_id novos a cada arquivo diário: consulte /ripencc/ip/{ip} ou /ripencc/asn/{asn} para obter o atual` |
| `/ripencc/nada` | 404 | `not_found` | `rota inexistente; veja /ripencc/` |
| uma consulta antes da primeira carga | 503 | `dataset_not_ready` | `a primeira sincronização do collector-ripencc ainda não terminou; tente em alguns minutos` |

A parte do 404 do titular depois do `;` sai de `OpaqueIDChangesDaily`, cita o
`BASE_PATH` configurado e vale para todo titular sem recursos no arquivo,
inclusive um que nunca existiu.

### Chaves de cache e ETags

Com o dataset dos exemplos (chave completa:
`badblock:api-ripencc:01a0eab5-6ada-7d2d-bb6b-90785b30dd1e:<consulta>`); ETags
calculados por `etagFor` em 2026-09-29 (o de `asn:3333` é o das respostas
reais abaixo):

| Pedido | Consulta | ETag |
|---|---|---|
| `/ripencc/asn/3333`, `/ripencc/v1/asn/AS3333` | `asn:3333` | `W/"8d75bf38e0721bd"` (o exemplo do manifesto; 15 dígitos: sem zeros à esquerda) |
| `/ripencc/asn/1877` | `asn:1877` | `W/"61963ce18504add6"` |
| `/ripencc/ip/193.0.6.139`, `/ripencc/ip/::ffff:193.0.6.139` | `ip:193.0.6.139` | `W/"ce23242bf7eb5ea2"` |
| `/ripencc/ip/2001:67c:2e8::1` | `ip:2001:67c:2e8::1` | `W/"a92950247835d0cb"` |
| `/ripencc/ip/87.116.90.1` | `ip:87.116.90.1` | `W/"d7a78f8634944a9"` |
| `/ripencc/prefix/193.0.6.139/24`, `/ripencc/prefix/193.0.6.0/24` | `prefix:193.0.6.0/24` | `W/"707adb24f71260ef"` |
| `/ripencc/prefix/193.0.0.0/20` | `prefix:193.0.0.0/20` | `W/"5680c5927efb7be9"` |
| `/ripencc/prefix/2001:67c:2e8::/48` | `prefix:2001:67c:2e8::/48` | `W/"201c077f4c06cfd0"` |
| `/ripencc/holder/c949c45e-fcab-480a-ba5b-804b0addae55` | `holder:c949c45e-fcab-480a-ba5b-804b0addae55` | `W/"5e6304c7e7a24245"` |
| `/ripencc/holder/C949C45E-FCAB-480A-BA5B-804B0ADDAE55` (a mesma resposta) | `holder:C949C45E-FCAB-480A-BA5B-804B0ADDAE55` | `W/"d8d519a4b4aec7e5"` |

Sequência real com esse dataset (no arquivo do dia seguinte, a versão muda e
todas as chaves e ETags mudam com ela):

```
GET /ripencc/asn/3333                          → 200, ETag: W/"8d75bf38e0721bd", X-Cache: MISS
GET /ripencc/v1/asn/AS3333                     → 200, mesmo ETag, X-Cache: HIT
GET /ripencc/v1/asn/AS3333
    If-None-Match: W/"8d75bf38e0721bd"         → 304 sem corpo (também com "8d75bf38e0721bd" ou *)
```

## Manifesto

O do [modelo](../rir/api.md#manifesto), com `info.version` `'0.1.0'`, os
exemplos desta página e, no RIPE NCC:

| Parte | No RIPE NCC |
|---|---|
| `externalDocs.url`, `servers` | `https://github.com/patrickbrandao/badblock/tree/main/specs/fontes/ripencc`; servidores locais na porta 8106 |
| parâmetros | `asn` `'3333'` (a descrição cita `3333`, `AS3333` e `as3333`); `ip` `193.0.6.139`; `ip` e `len` do prefixo `193.0.6.0` e `24` (a descrição cita `193.0.6.139/24` consulta `193.0.6.0/24`); `opaque_id` `'c949c45e-fcab-480a-ba5b-804b0addae55'` |
| cabeçalhos | `IfNoneMatch` e `ETag` com `W/"8d75bf38e0721bd"` (a descrição do `ETag` cita `/ripencc/asn/3333` e `/ripencc/v1/asn/AS3333`); `X-Dataset-Version` com a versão dos exemplos |
| exemplos | `/asn`: `asn` (`ASN alocado (/asn/3333)`) e `disponivel` (`ASN disponível (/asn/1877)`); `/ip`: `ipv4` (`IPv4 (/ip/193.0.6.139)`), `ipv6` (`IPv6 (/ip/2001:67c:2e8::1)`) e `nao_cidr`; `/prefix`: `contido` (`Prefixo contido num bloco maior (/prefix/193.0.6.0/24)`) e `exato` (`O próprio bloco (/prefix/193.0.0.0/20)`); `/meta`: `carregado` e `vazio`; `BadRequest` `len`: `/prefix/193.0.0.0/33`; `NotFound` `asn`: `/asn/61613 (um ASN da LACNIC)`; `NotFound` `holder`: `/holder/87debb42-f92a-447a-ae52-ae9ff30e6f5b (opaque_id do AS3333 no arquivo da véspera)`; `StatusOK` e `StatusError`: `timestamp` `2026-09-29T01:11:59Z` |
| `info.summary` | `Delegações de ASNs e blocos IP do RIPE NCC, do BadBlock.` |
| `info.description`, 1º parágrafo | o do modelo, com "do RIPE NCC" no lugar de "do RIR <Title>" |
| `info.description`, 2º parágrafo (antes dos itens) | **O opaque_id do RIPE NCC muda todo dia.** O RIPE NCC gera um UUID novo para cada titular a cada arquivo diário, então `/holder/{opaque_id}` só vale dentro do dataset atual: um opaque_id de ontem dá 404 hoje. Não o guarde como identificador do titular; guarde um ASN ou IP dele e obtenha o opaque_id atual por `/asn/{asn}` ou `/ip/{ip}`. |
| `/asn/{asn}`, `get.description` (no lugar da do modelo) | Responde o registro do arquivo do RIR que contém o ASN; `asn` é o número pedido. Um registro pode cobrir uma faixa (`range`), mas no RIPE NCC todo registro tem um ASN só (`count` 1). No RIPE NCC, `updated_at` muda todo dia em quase todo registro allocated/assigned, porque o opaque_id é regenerado a cada arquivo; `first_seen` não é afetado. |
| `/ip/{ip}`, exemplo `nao_cidr` | `summary` `Pedaço de um registro IPv4 não-CIDR (/ip/87.116.90.1)`; `description`: O registro `87.116.83.0` + 2304 endereços não forma um CIDR; o collector o divide em `87.116.83.0/24`, `87.116.84.0/22` e `87.116.88.0/22`, e cada pedaço aponta para o mesmo `record`. |
| `/prefix/{ip}/{len}`, `get.description` | a do modelo, terminando em "IPv6 também funciona: `/prefix/2001:67c:2e8::/48`." |
| parâmetro `opaque_id` | depois da primeira frase: No RIPE NCC é um UUID regenerado a cada arquivo diário: só vale dentro do dataset atual (obtenha o atual por `/asn` ou `/ip` antes de chamar esta rota). Na lista de formatos, "UUID minúsculo com hífens (o do RIPE NCC)"; o exemplo de caixa, "`C949C45E-FCAB-…` acha `c949c45e-fcab-…`". |
| `/holder/{opaque_id}`, `get.description` (bloco `\|-`), 2º parágrafo | **O opaque_id do RIPE NCC muda todo dia**: o RIPE NCC gera um UUID novo para cada titular a cada arquivo diário (nenhum titular mantém o UUID de um dia para o outro). Esta rota só vale dentro do dataset atual: um link ou um opaque_id guardado ontem dá 404 hoje. Não guarde nem publique o opaque_id como identificador do titular; guarde um ASN ou IP dele e obtenha o opaque_id atual por `/asn/{asn}` ou `/ip/{ip}`. Dentro de um mesmo arquivo o agrupamento vale (os recursos de um titular têm o mesmo UUID), e a chave de cache e o ETag levam a versão do dataset, que muda junto com os opaque_id. |
| `/holder/{opaque_id}`, 404 | O titular não tem nenhum recurso no arquivo atual do RIR. No RIPE NCC, também quando o opaque_id é de outro dia (o RIPE NCC gera opaque_id novos a cada arquivo diário): a mensagem explica isso e aponta `/ip/{ip}` e `/asn/{asn}` para obter o atual. |
| schema `CountryCode` | País, ISO 3166-1 alfa-2 (`ZZ` = sem país, como alguns RIRs publicam; o RIPE NCC usa também `EU`, Europa sem país específico); `null` quando vazio na fonte (no RIPE NCC, todo available/reserved). |
| schema `OpaqueID`, no fim | No RIPE NCC é um UUID regenerado a cada arquivo diário: só vale dentro do dataset atual. |
| schema `IPPrefix` | Bloco CIDR, IPv4 ou IPv6 (ex. `193.0.0.0/20`, `2001:67c:2e8::/48`). |
| `MetaDataset.serial` | Serial do cabeçalho do arquivo, como o RIR publica (data AAAAMMDD ou época Unix; no RIPE NCC, época Unix em segundos). |

## `--help`

Igual ao do [modelo](../lacnic/api.md#--help), só com os nomes trocados
(conferido em 2026-09-29, versão `dev`, comparando as duas saídas): mudam
`api-ripencc`, `RIR RIPE NCC`, `ripencc_*`, `collector-ripencc` e `/ripencc`
(inclusive o padrão de `--base-path`). A primeira linha é
`api-ripencc — API HTTP das delegações de ASNs e blocos IP do RIR RIPE NCC.`

## Operação

```bash
curl http://127.0.0.1:8106/ripencc/status          # sem Traefik, pela porta do loopback
curl http://127.0.0.1:8106/ripencc/asn/3333
make -C apps/ripencc/api smoke                      # /status, /meta, /asn/3333 e /openapi.yaml
make -C apps/ripencc/api logs
# titular: o opaque_id do dia sai de /asn ou /ip, antes de /holder
id=$(curl -s https://api.badblock.net.br/ripencc/asn/3333 | jq -r .opaque_id)
curl https://api.badblock.net.br/ripencc/holder/$id
```

Arquivo real ([../rir/api.md](../rir/api.md#arquivo-real-make-test-real)):

```bash
curl -o /tmp/delegated https://ftp.ripe.net/pub/stats/ripencc/delegated-ripencc-extended-latest
make -C apps/ripencc/api test-real FILE=/tmp/delegated   # RIPENCC_REAL_FILE
```

## Medições

Arquivo real de 2026-09-28 (serial 1790632799; 260.793 registros; 48.682
registros de ASN e 213.656 blocos gravados, 102.422 IPv4 e 111.234 IPv6):

| Medição | Valor |
|---|---|
| Carga pelo `collector-ripencc` (`--once`) | ~2,9 s com o arquivo servido localmente; ~9,6 s pela URL real (~7 s são o download de 18 MB) |
| Uma consulta no Postgres, com o cache do banco quente | `/asn` ~0,04 ms (índice único de `asn_start`); `/ip` ~0,3 ms (GiST); `/holder` pelos índices de `opaque_id` |
| `make test-real` (handler completo, sem cache) | 500 `/ip` aleatórios em 0,77 ms de média; 500 `/asn` em 0,17 ms; o maior titular (`422db66e-…`, 804 itens, 67 KB) em ~20 ms |
| Por HTTP, binário no host e Postgres/Valkey no Docker Desktop | 1.000 `/ip` aleatórios em 1,6 ms de média sem cache e 1,2 ms com cache; o maior titular em 13 ms sem cache e 3,3 ms com cache |

Conferido de ponta a ponta em 2026-09-28, com o arquivo real, o binário no
host e Postgres e Valkey no Docker: com o Valkey parado, `/status` fica
`degraded` e as rotas seguem respondendo (as três primeiras em ~50–100 ms,
esperando o `REDIS_TIMEOUT` na leitura e na gravação; depois das 5 falhas o
disjuntor abre e as respostas voltam a ~2–3 ms); com o Postgres parado,
`/status` responde 503 e as consultas fora do cache,
`503 database_unavailable`, enquanto as do cache continuam 200
(`X-Cache: HIT`). Os dois voltam sozinhos.

## Testes

Os do modelo, sem nada do RIPE NCC além do que sai de `rir.go`:
[../rir/api.md](../rir/api.md#testes). O store falso e o seed do teste de
integração usam os registros do
[recorte do modelo](../lacnic/fonte.md#recorte-testdatadelegated-extended-sampletxt)
(AS61613, AS28003–AS28005, titulares `258500` e `130343`), iguais nos cinco;
o recorte do RIPE NCC é só do coletor. Com `OpaqueIDChangesDaily` `true`, o
`TestErrors` exige o aviso no 404 do titular (`/ripencc/holder/999999` cita
`/ripencc/ip/{ip}` e `/ripencc/asn/{asn}`), e o `openapi_test.go` confere a
porta 8106 dos servidores locais contra o `PORT` do `Makefile`.
