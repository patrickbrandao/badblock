# Rotas da api-iana

Cada rota da `api-iana`, com parâmetros, campos e exemplo real de resposta.
O que vale para todas — caminho de base e versões, formato, bloco `dataset`,
cabeçalhos, `HEAD`, saúde e formato dos erros — está no
[padrão](../../padroes/api.md); validações, mensagens de erro, chaves de
cache, ETags e a regra de bogon, em [api.md](api.md). O manifesto
(`apps/iana/api/openapi/openapi.yaml`) é derivado deste arquivo e muda junto
com ele ([api.md](api.md#manifesto)).

Os exemplos são respostas reais do dataset de 2026-09-28 (versão
`01a0eaa2-fd5c-7c24-bc6c-4e3c35d61c53`, aplicada em `2026-09-29T00:49:05Z`),
reformatadas: a API responde numa linha só. Os de `/asn`, `/ip` e `/prefix`
e o `dataset` do `/meta` foram conferidos em 2026-09-29 com a API sobre o
`testdata/seed.sql`, que tem as mesmas linhas. `"…"` marca o que foi cortado.

## Rotas

| Rota (a mesma em `/iana/v1/...`) | Resposta (tipo e `schema`) | Store | Cache |
|---|---|---|---|
| `GET /iana/asn/{asn}` | `ASNResponse` | `ASN` | `serveCached` |
| `GET /iana/ip/{ip}` | `IPResponse` | `Prefix` (`/32` ou `/128`) | `serveCached` |
| `GET /iana/prefix/{ip}/{len}` | `PrefixResponse` | `Prefix` | `serveCached` |
| `GET /iana/asns` | `ASNBlocksResponse` | `ASNBlocks` | `serveCached` |
| `GET /iana/ipv4`, `GET /iana/ipv6` | `PrefixBlocksResponse` | `PrefixBlocks(4)`, `PrefixBlocks(6)` | `serveCached` |
| `GET /iana/special` | `SpecialResponse` | `Special` | `serveCached` |
| `GET /iana/rdap` | `RDAPResponse` (com `RDAPPublication`) | `RDAP` | `serveCached` |
| `GET /iana/meta` | `MetaResponse` (`MetaDataset`, `MetaFile`, `MetaCollector`) | `Dataset`, `Job` | nenhum, `Cache-Control: no-store` |
| `GET /iana/` | `IndexResponse` | — | nenhum, `Cache-Control: public, max-age=300` |

Fora do versionamento, como no [padrão](../../padroes/api.md#rotas-comuns):
`GET` e `POST` em `/iana/health` e `/iana/status`, `GET /iana/ping` e
`GET /iana/openapi.yaml`.

## Convenções das rotas de dados

- Texto que a IANA publica vazio sai `null` (`registry`, `whois`,
  `reference`, `rfc`, `note`, datas...); listas nunca são `null`: vazias
  saem `[]` (`rdap_urls`, `special`, `urls`, `blocks`...).
- Datas da IANA são **texto** como publicadas, `AAAA-MM` ou `AAAA-MM-DD`
  (`registration_date`, `allocation_date`, `termination_date`).
- `registry` é o RIR responsável: `afrinic`, `apnic`, `arin`, `lacnic`,
  `ripencc` ou `null` (faixa ou bloco sem RIR: reservado, IANA, 6to4...; no
  RDAP, servidor de outro operador).
- Nenhuma rota tem parâmetro de consulta: as listas vêm sempre inteiras, sem
  paginação nem filtro, e a query string é ignorada.
- `/asn`, `/ip` e `/prefix` **nunca respondem 404** para entrada válida: os
  blocos de ASN cobrem 0–4294967295 e os `/8`, todo o IPv4; no IPv6 só há
  bloco para o que a IANA entregou (`block: null` fora disso, ex.:
  `fe80::1`).

## Objetos

### Faixa de ASN (`ASNBlock`)

`block` do `/asn` e itens de `blocks` do `/asns` (`iana_asn_block`,
[dados.md](dados.md#iana_asn_block)):

| Campo | Tipo | Conteúdo |
|---|---|---|
| `start`, `end` | inteiro | primeiro e último ASN da faixa, inclusive |
| `description` | texto | `Description` da IANA (`Assigned by LACNIC`, `Reserved`, `AS_TRANS`, `Unallocated`...) |
| `registry` | texto ou `null` | RIR da faixa |
| `whois` | texto ou `null` | servidor WHOIS (`whois.lacnic.net`) |
| `rdap_urls` | lista de texto | URLs RDAP do registro, na ordem publicada |
| `reference` | texto ou `null` | `Reference` (`[RFC6793]`) |
| `registration_date` | data da IANA ou `null` | `Registration Date` |

### ASN de uso especial (`SpecialASN`)

`special` do `/asn` e itens de `asn` do `/special` (`iana_special_asn`):

| Campo | Tipo | Conteúdo |
|---|---|---|
| `start`, `end` | inteiro | faixa, inclusive |
| `reason` | texto | `Reason for Reservation` (`AS_TRANS; reserved by [RFC6793]`) |
| `reference` | texto ou `null` | `Reference` |

### Bloco IP (`PrefixBlock`)

`block` do `/ip` e do `/prefix` e itens de `blocks` do `/ipv4` e do `/ipv6`
(`iana_prefix_block`):

| Campo | Tipo | Conteúdo |
|---|---|---|
| `prefix` | texto (CIDR) | `/8` de `ipv4-address-space` ou bloco de `ipv6-unicast-address-assignments` |
| `designation` | texto | `Designation` (`Administered by ARIN`, `APNIC`, `IANA - Private Use`, `Multicast`...) |
| `registry` | texto ou `null` | RIR do bloco |
| `status` | texto | `ALLOCATED` (entregue a um RIR), `LEGACY` (anterior aos RIRs, hoje administrado por um) ou `RESERVED` |
| `whois` | texto ou `null` | servidor WHOIS |
| `rdap_urls` | lista de texto | URLs RDAP do registro |
| `allocation_date` | data da IANA ou `null` | `AAAA-MM` no IPv4, quase sempre `AAAA-MM-DD` no IPv6 |
| `note` | texto ou `null` | no IPv4, só as marcas de nota de rodapé (`[10][11]`); no IPv6, o texto da nota |

### Bloco de uso especial (`SpecialPrefix`)

`special` do `/ip` e do `/prefix` e itens de `ipv4` e `ipv6` do `/special`
(`iana_special_prefix`):

| Campo | Tipo | Conteúdo |
|---|---|---|
| `prefix` | texto (CIDR) | bloco do special-purpose registry |
| `name` | texto | `Name` (`Private-Use`, `"This network"` com as aspas da IANA) |
| `rfc` | texto ou `null` | `RFC` (`[RFC6890], Section 2.1`) |
| `allocation_date` | data da IANA ou `null` | `Allocation Date` |
| `termination_date` | data da IANA ou `null` | `null` = em vigor; preenchida = registro encerrado, que não conta na regra de bogon |
| `source`, `destination` | booleano ou `null` | pode ser endereço de origem / de destino |
| `forwardable` | booleano ou `null` | roteadores podem encaminhar |
| `globally_reachable` | booleano ou `null` | alcançável na Internet pública; decide a regra de bogon quando é a entrada em vigor mais específica |
| `reserved_by_protocol` | booleano ou `null` | reservado pela especificação do IP |

As 5 flags saem `null` quando a IANA publica vazio ou `N/A` (registros
encerrados; `globally_reachable` do TEREDO e do 6to4).

### Serviço RDAP (`RDAPService`)

`rdap` do `/asn`, do `/ip` e do `/prefix` e itens de `asn`, `ipv4` e `ipv6`
do `/rdap` (`iana_rdap_service`):

| Campo | Tipo | Conteúdo |
|---|---|---|
| `resource` | texto | entrada do bootstrap: faixa (`61440-61951`), um ASN só (`2043`) ou CIDR (`192.0.0.0/8`) |
| `registry` | texto ou `null` | RIR do servidor; `null` = servidor de outro operador |
| `urls` | lista de texto | URLs base do servidor RDAP, na ordem publicada (a https primeiro) |

## `GET /iana/asn/{asn}`

`{asn}`: de 0 a 4294967295, com ou sem o prefixo `AS`
([validação](api.md#validações)).

| Campo | Tipo | Conteúdo |
|---|---|---|
| `asn` | inteiro | ASN consultado, normalizado |
| `block` | faixa de ASN ou `null` | faixa de `as-numbers-1`/`-2` que contém o ASN (`null` só com dados incompletos) |
| `special` | lista de ASN especial | faixas de `special-purpose-as-numbers` que contêm o ASN, em ordem de início |
| `rdap` | serviço RDAP ou `null` | entrada de `asn.json` que contém o ASN (faixas reservadas e não alocadas não têm) |

`GET /iana/asn/61610`:

```json
{"asn": 61610,
 "block": {"start": 61440, "end": 61951, "description": "Assigned by LACNIC", "registry": "lacnic",
           "whois": "whois.lacnic.net", "rdap_urls": ["https://rdap.lacnic.net/rdap/"], "reference": null,
           "registration_date": "2013-06-11"},
 "special": [],
 "rdap": {"resource": "61440-61951", "registry": "lacnic", "urls": ["https://rdap.lacnic.net/rdap/"]},
 "dataset": {"version": "01a0eaa2-fd5c-7c24-bc6c-4e3c35d61c53", "updated_at": "2026-09-29T00:49:05Z"}}
```

`GET /iana/asn/AS23456`:

```json
{"asn": 23456,
 "block": {"start": 23456, "end": 23456, "description": "AS_TRANS", "registry": null, "whois": null,
           "rdap_urls": [], "reference": "[RFC6793]", "registration_date": null},
 "special": [{"start": 23456, "end": 23456, "reason": "AS_TRANS; reserved by [RFC6793]", "reference": "[RFC6793]"}],
 "rdap": null,
 "dataset": {"version": "01a0eaa2-fd5c-7c24-bc6c-4e3c35d61c53", "updated_at": "2026-09-29T00:49:05Z"}}
```

Erros: 400, 503 (`dataset_not_ready`, `database_unavailable`), 504 e 500
([mensagens](api.md#erros)); o mesmo vale para `/ip` e `/prefix`.

## `GET /iana/ip/{ip}`

`{ip}`: IPv4 ou IPv6; IPv4 mapeado em IPv6 (`::ffff:10.0.0.1`) vira IPv4;
endereço com zona (`fe80::1%eth0`) é inválido
([validação](api.md#validações)).

| Campo | Tipo | Conteúdo |
|---|---|---|
| `ip` | texto | IP consultado, normalizado |
| `block` | bloco IP ou `null` | bloco mais específico de `ipv4-address-space` ou `ipv6-unicast-address-assignments` que contém o IP; `null` fora deles |
| `special` | lista de bloco especial | **todas** as entradas dos special-purpose registries que contêm o IP, da mais específica para a menos, inclusive as encerradas |
| `bogon` | booleano | resultado da [regra de bogon](api.md#regra-de-bogon) |
| `rdap` | serviço RDAP ou `null` | entrada mais específica de `ipv4.json`/`ipv6.json` que contém o IP |

`GET /iana/ip/192.0.0.9`:

```json
{"ip": "192.0.0.9",
 "block": {"prefix": "192.0.0.0/8", "designation": "Administered by ARIN", "registry": "arin", "status": "LEGACY",
           "whois": "whois.arin.net", "rdap_urls": ["https://rdap.arin.net/registry", "http://rdap.arin.net/registry"],
           "allocation_date": "1993-05", "note": "[10][11]"},
 "special": [{"prefix": "192.0.0.9/32", "name": "Port Control Protocol Anycast", "rfc": "[RFC7723]",
              "allocation_date": "2015-10", "termination_date": null, "source": true, "destination": true,
              "forwardable": true, "globally_reachable": true, "reserved_by_protocol": false},
             {"prefix": "192.0.0.0/24", "name": "IETF Protocol Assignments", "rfc": "[RFC6890], Section 2.1",
              "allocation_date": "2010-01", "termination_date": null, "source": false, "destination": false,
              "forwardable": false, "globally_reachable": false, "reserved_by_protocol": false}],
 "bogon": false,
 "rdap": {"resource": "192.0.0.0/8", "registry": "arin",
          "urls": ["https://rdap.arin.net/registry/", "http://rdap.arin.net/registry/"]},
 "dataset": {"version": "01a0eaa2-fd5c-7c24-bc6c-4e3c35d61c53", "updated_at": "2026-09-29T00:49:05Z"}}
```

`GET /iana/ip/10.0.0.1`:

```json
{"ip": "10.0.0.1",
 "block": {"prefix": "10.0.0.0/8", "designation": "IANA - Private Use", "registry": null, "status": "RESERVED",
           "whois": null, "rdap_urls": [], "allocation_date": "1995-06", "note": "[4]"},
 "special": [{"prefix": "10.0.0.0/8", "name": "Private-Use", "rfc": "[RFC1918]", "allocation_date": "1996-02",
              "termination_date": null, "source": true, "destination": true, "forwardable": true,
              "globally_reachable": false, "reserved_by_protocol": false}],
 "bogon": true, "rdap": null,
 "dataset": {"version": "01a0eaa2-fd5c-7c24-bc6c-4e3c35d61c53", "updated_at": "2026-09-29T00:49:05Z"}}
```

`GET /iana/ip/fe80::1` (fora de todo bloco da IANA):

```json
{"ip": "fe80::1", "block": null,
 "special": [{"prefix": "fe80::/10", "name": "Link-Local Unicast", "rfc": "[RFC4291]", "allocation_date": "2006-02",
              "termination_date": null, "source": true, "destination": true, "forwardable": false,
              "globally_reachable": false, "reserved_by_protocol": true}],
 "bogon": true, "rdap": null,
 "dataset": {"version": "01a0eaa2-fd5c-7c24-bc6c-4e3c35d61c53", "updated_at": "2026-09-29T00:49:05Z"}}
```

## `GET /iana/prefix/{ip}/{len}`

Como o `/ip`, para um prefixo: `{ip}` segue as regras do `/ip`; `{len}` vai
de 0 a 32 (IPv4) ou 128 (IPv6); os bits de host são zerados
(`/prefix/10.1.2.3/16` consulta `10.1.0.0/16`).

| Campo | Tipo | Conteúdo |
|---|---|---|
| `query` | texto (CIDR) | prefixo consultado, com os bits de host zerados |
| `block` | bloco IP ou `null` | bloco mais específico que **contém** o prefixo (`null` para um prefixo maior que ele, ex.: `8.0.0.0/7`) |
| `special` | lista de bloco especial | entradas que contêm o prefixo, da mais específica para a menos; as que estão dentro dele não entram (consulte `/ip` para um endereço) |
| `bogon` | booleano | a regra de bogon aplicada ao prefixo inteiro |
| `rdap` | serviço RDAP ou `null` | entrada mais específica que contém o prefixo |

`GET /iana/prefix/2001:db8::/48`:

```json
{"query": "2001:db8::/48",
 "block": {"prefix": "2001:c00::/23", "designation": "APNIC", "registry": "apnic", "status": "ALLOCATED",
           "whois": "whois.apnic.net", "rdap_urls": ["https://rdap.apnic.net/"], "allocation_date": "2002-05-02",
           "note": "2001:db8::/32 is reserved for Documentation [RFC3849]. See [IPv6 Special-Purpose Address Space] for details."},
 "special": [{"prefix": "2001:db8::/32", "name": "Documentation", "rfc": "[RFC3849]", "allocation_date": "2004-07",
              "termination_date": null, "source": false, "destination": false, "forwardable": false,
              "globally_reachable": false, "reserved_by_protocol": false}],
 "bogon": true,
 "rdap": {"resource": "2001:c00::/23", "registry": "apnic", "urls": ["https://rdap.apnic.net/"]},
 "dataset": {"version": "01a0eaa2-fd5c-7c24-bc6c-4e3c35d61c53", "updated_at": "2026-09-29T00:49:05Z"}}
```

`GET /iana/prefix/8.0.0.0/7` (maior que um `/8`; cruza `8.0.0.0/8` `LEGACY`):

```json
{"query": "8.0.0.0/7", "block": null, "special": [], "bogon": false, "rdap": null,
 "dataset": {"version": "01a0eaa2-fd5c-7c24-bc6c-4e3c35d61c53", "updated_at": "2026-09-29T00:49:05Z"}}
```

## `GET /iana/asns`

Todas as faixas de ASN, em ordem numérica: `count` (inteiro, o tamanho de
`blocks`), `blocks` (faixas de ASN) e `dataset`. Com o dataset de
2026-09-28, 173 faixas (~35 KB).

```json
{"count": 173,
 "blocks": [{"start": 0, "end": 0, "description": "Reserved", "registry": null, "whois": null, "rdap_urls": [],
             "reference": "[RFC7607]", "registration_date": null},
            {"start": 1, "end": 1876, "description": "Assigned by ARIN", "registry": "arin", "whois": "whois.arin.net",
             "rdap_urls": ["https://rdap.arin.net/registry", "http://rdap.arin.net/registry"], "reference": null,
             "registration_date": null},
            "…"],
 "dataset": {"version": "…", "updated_at": "…"}}
```

## `GET /iana/ipv4` e `GET /iana/ipv6`

Todos os blocos de uma família, em ordem de endereço: `count`, `blocks`
(blocos IP) e `dataset`. Os 256 `/8` do IPv4 (~54 KB) e os 51 blocos do IPv6
unicast (~12 KB).

```json
{"count": 256,
 "blocks": [{"prefix": "0.0.0.0/8", "designation": "IANA - Local Identification", "registry": null, "status": "RESERVED",
             "whois": null, "rdap_urls": [], "allocation_date": "1981-09", "note": "[2][3]"},
            "…"],
 "dataset": {"version": "…", "updated_at": "…"}}
```

```json
{"count": 51,
 "blocks": [{"prefix": "2001::/23", "designation": "IANA", "registry": null, "status": "ALLOCATED",
             "whois": "whois.iana.org", "rdap_urls": [], "allocation_date": "1999-07-01",
             "note": "This range has been partially allocated. See [IPv6 Special-Purpose Address Space] for details."},
            "…"],
 "dataset": {"version": "…", "updated_at": "…"}}
```

## `GET /iana/special`

Os três registros de uso especial inteiros: `ipv4` e `ipv6` (blocos de uso
especial em ordem de endereço, inclusive os encerrados), `asn` (ASNs de uso
especial em ordem numérica) e `dataset`. Com o dataset de 2026-09-28, 26 +
25 blocos e 9 faixas (13.212 bytes).

```json
{"ipv4": [{"prefix": "0.0.0.0/8", "name": "\"This network\"", "rfc": "[RFC791], Section 3.2", "allocation_date": "1981-09",
           "termination_date": null, "source": true, "destination": false, "forwardable": false,
           "globally_reachable": false, "reserved_by_protocol": true},
          "…"],
 "ipv6": [{"prefix": "::/128", "name": "Unspecified Address", "rfc": "[RFC4291]", "allocation_date": "2006-02",
           "termination_date": null, "source": true, "destination": false, "forwardable": false,
           "globally_reachable": false, "reserved_by_protocol": true},
          "…"],
 "asn": [{"start": 0, "end": 0, "reason": "Reserved by [RFC7607]", "reference": "[RFC7607]"}, "…"],
 "dataset": {"version": "…", "updated_at": "…"}}
```

## `GET /iana/rdap`

O bootstrap RDAP (RFC 9224) inteiro: `publication` (objeto com `asn`,
`ipv4` e `ipv6`: o campo `publication` de cada JSON, RFC 3339 em UTC, ou
`null` se o arquivo não trouxe), `asn` (entradas de `asn.json`, em ordem de
faixa), `ipv4` e `ipv6` (de `ipv4.json` e `ipv6.json`, em ordem de
endereço), todas como serviço RDAP, e `dataset`. Com o dataset de
2026-09-28, 159 + 221 + 34 entradas (~41 KB).

```json
{"publication": {"asn": "2026-06-01T20:00:01Z", "ipv4": "2019-06-07T19:00:02Z", "ipv6": "2024-11-01T22:00:01Z"},
 "asn": [{"resource": "1-1876", "registry": "arin", "urls": ["https://rdap.arin.net/registry/", "http://rdap.arin.net/registry/"]}, "…"],
 "ipv4": [{"resource": "1.0.0.0/8", "registry": "apnic", "urls": ["https://rdap.apnic.net/"]}, "…"],
 "ipv6": [{"resource": "2001:200::/23", "registry": "apnic", "urls": ["https://rdap.apnic.net/"]}, "…"],
 "dataset": {"version": "…", "updated_at": "…"}}
```

## `GET /iana/meta`

Estado dos dados, sem cache e sem ETag (`Cache-Control: no-store`); responde
200 também antes da primeira carga, com `dataset` e `collector` `null`.

| Campo | Tipo | Conteúdo |
|---|---|---|
| `app` | texto | `api-iana` |
| `version` | texto | versão do build da API (`dev` fora de um build com tag) |
| `dataset` | objeto ou `null` | a última execução aplicada (`iana_run` com `status = 1`, [dados.md](dados.md#iana_run)) |
| `dataset.version`, `dataset.updated_at` | uuid, timestamp | `iana_run.uuid` (a versão do dataset) e `iana_run.created_at` |
| `dataset.sha256` | texto | SHA-256 combinado dos 10 arquivos |
| `dataset.files` | lista | os 10 arquivos, na ordem fixa ([fonte.md](fonte.md#arquivos)) |
| `files[].name`, `files[].url` | texto | nome do arquivo e URL de onde veio |
| `files[].sha256`, `files[].bytes` | texto, inteiro | SHA-256 e tamanho do conteúdo |
| `files[].rows` | inteiro ou `null` | registros gerados pelo parser |
| `files[].last_modified` | texto ou `null` | cabeçalho `Last-Modified` como veio (data HTTP) |
| `files[].publication` | texto ou `null` | só nos `rdap-*`: o campo `publication` do JSON |
| `collector` | objeto ou `null` | a linha do `collector-iana` em `jobs` |
| `collector.app` | texto | `collector-iana` |
| `collector.last_sync_at`, `collector.last_check_at` | timestamp ou `null` | último dataset aplicado; última verificação |
| `collector.consolidated` | booleano | `jobs.consolidated = 1` |

Em cada arquivo, `rows`, `last_modified` e `publication` saem `null` quando o
coletor não os gravou ([dados.md](dados.md#files-e-changes)). De
`iana_run.files`, o `/meta` não mostra `http_status`, `etag` nem `changed`.

```json
{"app": "api-iana", "version": "0.1.0",
 "dataset": {"version": "01a0eaa2-fd5c-7c24-bc6c-4e3c35d61c53", "updated_at": "2026-09-29T00:49:05Z",
             "sha256": "b22ef6b2e76eafd675b5d1a144ad6488720da7c39d56e1d8c09a6cf6401ad06e",
             "files": [{"name": "as-numbers-1", "url": "https://www.iana.org/assignments/as-numbers/as-numbers-1.csv",
                        "sha256": "47f5fe7842029d8077ef749d6b3590293df3252841f1446a188284c5a1839949", "bytes": 7936,
                        "rows": 88, "last_modified": "Sat, 19 Sep 2026 00:44:44 GMT", "publication": null},
                       "…",
                       {"name": "rdap-asn", "url": "https://data.iana.org/rdap/asn.json",
                        "sha256": "80a0659f933b45130435c0bc7d6143ca2b815b3c5d2ef80c769b985a7821bb65", "bytes": 4408,
                        "rows": 159, "last_modified": "Mon, 01 Jun 2026 20:00:01 GMT", "publication": "2026-06-01T20:00:01Z"},
                       "…"]},
 "collector": {"app": "collector-iana", "last_sync_at": "2026-09-29T00:49:05Z",
               "last_check_at": "2026-09-29T00:49:05Z", "consolidated": false}}
```

Antes da primeira carga:
`{"app": "api-iana", "version": "0.1.0", "dataset": null, "collector": null}`.

## `GET /iana/` e `GET /iana/v1/`

Índice (`Cache-Control: public, max-age=300`): `app`, `version` (do build),
`base_path`, `versions` (`["v1"]`), `endpoints` (as 11 rotas do exemplo, com
o caminho de base; sem `/health`, `/ping` e as formas com `/v1`) e `source`
(a página da IANA com os registros de numeração).

```json
{"app": "api-iana", "version": "0.1.0", "base_path": "/iana", "versions": ["v1"],
 "endpoints": ["/iana/asn/{asn}", "/iana/ip/{ip}", "/iana/prefix/{ip}/{len}", "/iana/asns", "/iana/ipv4",
               "/iana/ipv6", "/iana/special", "/iana/rdap", "/iana/meta", "/iana/status", "/iana/openapi.yaml"],
 "source": "https://www.iana.org/numbers"}
```

## Saúde e manifesto

`/iana/health` e `/iana/status` seguem o
[padrão](../../padroes/api.md#saúde), com `api-iana` e `collector-iana` nas
mensagens. Exemplo (o do manifesto):

```json
{"success": true, "status": "ok", "timestamp": "2026-09-29T01:00:00Z", "message": "api-iana operacional",
 "checks": {"dataset": "ok", "postgres": "ok", "valkey": "ok"}}
```

`/iana/ping` responde `pong`; `/iana/openapi.yaml`, o manifesto
([openapi.md](../../padroes/openapi.md)).
