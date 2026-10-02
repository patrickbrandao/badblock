# rootzone — rotas da API

Cada rota da `api-rootzone`: parâmetros, validação, normalização, chave de
cache, campos da resposta, exemplos reais e erros. O que vale para toda API
(formato do JSON, `HEAD`, `OPTIONS`, cabeçalhos, `serveCached`, ETag, saúde,
formato dos erros) está no [padrão](../../padroes/api.md); valores, cache,
consultas, medições e testes desta API, em [api.md](api.md); o SQL de cada
rota, em [dados.md](dados.md#consultas-da-api).

Os exemplos são respostas reais com o `root.zone` publicado em 2026-09-29
(serial `2026092901`, 1.438 TLDs), carregado pelo `collector-rootzone` num
PG18 descartável em 2026-09-30T03:55:26Z (versão do dataset
`01a0f073-f7ab-73d8-bf10-586563066827`). Estão formatados para leitura — a
API responde JSON compacto, numa linha — e as listas longas foram cortadas
em `…` (o texto diz onde).

## Rotas

| Rota | Métodos | O quê | Chave de cache |
|---|---|---|---|
| `/rootzone/tlds` | GET | todos os TLDs delegados | `tlds` |
| `/rootzone/tld/{tld}` | GET | a delegação de um TLD | `tld:<tld>` |
| `/rootzone/meta` | GET | estado dos dados, com o SOA | sem cache |
| `/rootzone/` | GET | índice | sem cache |
| `/rootzone/health`, `/rootzone/status` | GET, POST | saúde | — |
| `/rootzone/ping` | GET | `pong` | — |
| `/rootzone/openapi.yaml` | GET | manifesto OpenAPI 3.1 | — |

As quatro primeiras respondem também em `/rootzone/v1/...`, com o mesmo
conteúdo, a mesma chave e o mesmo ETag; as três últimas ficam fora do
versionamento. Todo `GET` aceita `HEAD`.

Nas rotas de dados:

- A validação vem **antes de tudo**: pedido inválido recebe 400 mesmo antes
  da primeira carga do coletor. Depois vêm, nesta ordem, a versão do dataset
  (503 `dataset_not_ready`), o `If-None-Match` (304), o Valkey e o Postgres.
- Toda resposta 200 traz o bloco `zone` (`serial` do SOA da versão) e o
  bloco `dataset` (`version`, `updated_at`) do
  [padrão](../../padroes/api.md#formato-das-respostas).
- Lista vazia sai `[]`, nunca `null`.

## `GET /rootzone/tlds`

Todos os TLDs delegados na zona raiz, em ordem de nome
([api.md](api.md#dados-servidos)), com as contagens de `rootzone_tld`. Sem
parâmetros; nunca 400 nem 404.

| Campo | Tipo | Significado |
|---|---|---|
| `zone.serial` | inteiro ou `null` | serial do SOA da zona (`AAAAMMDDNN`) |
| `count` | inteiro | quantidade de TLDs na lista |
| `tlds` | lista | os TLDs |
| `tlds[].tld` | texto | nome em ASCII, minúsculas, sem o ponto final (`br`, `xn--p1ai`) |
| `tlds[].tld_unicode` | texto | forma Unicode (`рф`); igual a `tld` nos ASCII |
| `tlds[].nameservers` | inteiro | NS da delegação (≥ 1) |
| `tlds[].nameservers_ipv4` | inteiro | quantos desses servidores têm glue A na zona |
| `tlds[].nameservers_ipv6` | inteiro | quantos têm glue AAAA |
| `tlds[].ds_records` | inteiro | DS da delegação (0 = TLD sem DNSSEC) |
| `dataset` | objeto | versão dos dados usada na resposta |

Trecho da resposta real (5 dos 1.438 itens: os dois primeiros, `br`,
`xn--p1ai` e o último; 161.044 bytes no total):

```json
{
  "zone": {"serial": 2026092901},
  "count": 1438,
  "tlds": [
    {"tld": "aaa", "tld_unicode": "aaa", "nameservers": 6, "nameservers_ipv4": 6, "nameservers_ipv6": 6, "ds_records": 1},
    {"tld": "aarp", "tld_unicode": "aarp", "nameservers": 6, "nameservers_ipv4": 6, "nameservers_ipv6": 6, "ds_records": 1},
    …
    {"tld": "br", "tld_unicode": "br", "nameservers": 6, "nameservers_ipv4": 6, "nameservers_ipv6": 6, "ds_records": 1},
    …
    {"tld": "xn--p1ai", "tld_unicode": "рф", "nameservers": 6, "nameservers_ipv4": 6, "nameservers_ipv6": 6, "ds_records": 1},
    …
    {"tld": "zw", "tld_unicode": "zw", "nameservers": 5, "nameservers_ipv4": 5, "nameservers_ipv6": 5, "ds_records": 0}
  ],
  "dataset": {"version": "01a0f073-f7ab-73d8-bf10-586563066827", "updated_at": "2026-09-30T03:55:26Z"}
}
```

Na mesma resposta: 151 IDN (`xn--…`), 87 TLDs com `ds_records: 0` e 18 com
`nameservers_ipv6: 0`. ETag com a versão dos exemplos:
`W/"f60b39e5f95a1753"`.

Erros: 503 `dataset_not_ready` ou `database_unavailable`, 504 `timeout`,
500 `internal_error`.

## `GET /rootzone/tld/{tld}`

A delegação de um TLD: os servidores de nome (NS) com TTL, o glue A/AAAA
que a zona raiz traz de cada um e os DS.

- `{tld}`: um rótulo, em qualquer caixa, com ou sem **um** ponto final, em
  ASCII (`br`, `BR.`, `xn--p1ai`) ou em Unicode (`рф`, `РФ.`, codificado
  na URL: `/rootzone/tld/%D1%80%D1%84`). Normalizado para a forma de
  `rootzone_tld.tld` — minúsculas, sem o ponto final, `xn--` pelo punycode
  —, com as regras de [api.md](api.md#normalização-do-tld-internaltldname);
  fora delas, 400.
- Chave: `tld:` + o nome normalizado (`/tld/РФ.` → `tld:xn--p1ai`). A
  consulta usa o mesmo nome.
- 404 se o nome é válido mas não está em `rootzone_tld`. A raiz não é um
  TLD: `/rootzone/tld/.` é limpo pelo `ServeMux` (307 para
  `/rootzone/tld`, que é 404; um `curl` sem `--path-as-is` já limpa o
  caminho antes de enviar).

| Campo | Tipo | Significado |
|---|---|---|
| `tld` | texto | nome em ASCII, a forma normalizada do pedido |
| `tld_unicode` | texto | forma Unicode; igual a `tld` nos ASCII |
| `nameservers` | lista | os NS da delegação, na ordem do banco ([api.md](api.md#dados-servidos)) |
| `nameservers[].name` | texto | nome do servidor, minúsculas, sem o ponto final |
| `nameservers[].ttl` | inteiro | TTL do NS, em segundos |
| `nameservers[].ipv4` | lista | glue A: `{"address", "ttl"}`; `[]` quando não há |
| `nameservers[].ipv6` | lista | glue AAAA, na forma canônica da RFC 5952; `[]` quando não há |
| `ds` | lista | os DS, em ordem de rdata; `[]` = TLD sem DNSSEC |
| `ds[].key_tag` | inteiro | key tag (0 a 65535) |
| `ds[].algorithm` | inteiro | algoritmo (8 = RSA/SHA-256, 13 = ECDSA P-256…) |
| `ds[].digest_type` | inteiro | tipo do digest (2 = SHA-256…) |
| `ds[].digest` | texto | digest em hexadecimal maiúsculo |
| `ds[].ttl` | inteiro | TTL do DS, em segundos |
| `first_seen` | timestamp | `rootzone_tld.created_at`: quando **este banco** viu o TLD pela primeira vez — não é a data da delegação |
| `updated_at` | timestamp | `rootzone_tld.updated_at`: última mudança nas contagens ou na forma Unicode |
| `zone.serial` | inteiro ou `null` | serial do SOA da zona |
| `dataset` | objeto | versão dos dados usada na resposta |

`GET /rootzone/tld/br` (e `/tld/BR.`, `/v1/tld/br`), ETag
`W/"c378da2e80b8654"`:

```json
{
  "tld": "br",
  "tld_unicode": "br",
  "nameservers": [
    {"name": "a.dns.br", "ttl": 172800, "ipv4": [{"address": "200.219.148.10", "ttl": 172800}], "ipv6": [{"address": "2001:12f8:6::10", "ttl": 172800}]},
    {"name": "b.dns.br", "ttl": 172800, "ipv4": [{"address": "200.189.41.10", "ttl": 172800}], "ipv6": [{"address": "2001:12f8:8::10", "ttl": 172800}]},
    {"name": "c.dns.br", "ttl": 172800, "ipv4": [{"address": "200.192.233.10", "ttl": 172800}], "ipv6": [{"address": "2001:12f8:a::10", "ttl": 172800}]},
    {"name": "d.dns.br", "ttl": 172800, "ipv4": [{"address": "200.219.154.10", "ttl": 172800}], "ipv6": [{"address": "2001:12f8:4::10", "ttl": 172800}]},
    {"name": "e.dns.br", "ttl": 172800, "ipv4": [{"address": "200.229.248.10", "ttl": 172800}], "ipv6": [{"address": "2001:12f8:2::10", "ttl": 172800}]},
    {"name": "f.dns.br", "ttl": 172800, "ipv4": [{"address": "200.219.159.10", "ttl": 172800}], "ipv6": [{"address": "2001:12f8:c::10", "ttl": 172800}]}
  ],
  "ds": [
    {"key_tag": 38298, "algorithm": 13, "digest_type": 2, "digest": "9F2D4993F47B0F2751DE0007D70A2754EE532FE373761154D9EA7A8CB9D8EA18", "ttl": 86400}
  ],
  "first_seen": "2026-09-30T03:55:26Z",
  "updated_at": "2026-09-30T03:55:26Z",
  "zone": {"serial": 2026092901},
  "dataset": {"version": "01a0f073-f7ab-73d8-bf10-586563066827", "updated_at": "2026-09-30T03:55:26Z"}
}
```

Os AAAA do `br` vêm no arquivo com os zeros por extenso
(`2001:12f8:6:0:0:0:0:10`); o coletor grava a forma canônica.

`GET /rootzone/tld/bo` — sem DNSSEC (`ds: []`), um servidor sem AAAA
(`ipv6: []`) e `ns.dns.br`, cujo glue fica sob `br`; note a ordem do banco
(`ns2.nic.fr` antes de `ns.dns.br`):

```json
{
  "tld": "bo",
  "tld_unicode": "bo",
  "nameservers": [
    {"name": "anycast.ns.nic.bo", "ttl": 172800, "ipv4": [{"address": "204.61.216.48", "ttl": 172800}], "ipv6": [{"address": "2001:500:14:6048:ad::1", "ttl": 172800}]},
    {"name": "ns2.nic.fr", "ttl": 172800, "ipv4": [{"address": "192.93.0.4", "ttl": 172800}], "ipv6": [{"address": "2001:660:3005:1::1:2", "ttl": 172800}]},
    {"name": "ns.dns.br", "ttl": 172800, "ipv4": [{"address": "200.160.0.5", "ttl": 172800}], "ipv6": [{"address": "2001:12ff:0:a20::5", "ttl": 172800}]},
    {"name": "ns.nic.bo", "ttl": 172800, "ipv4": [{"address": "166.114.1.40", "ttl": 172800}], "ipv6": []}
  ],
  "ds": [],
  "first_seen": "2026-09-30T03:55:26Z",
  "updated_at": "2026-09-30T03:55:26Z",
  "zone": {"serial": 2026092901},
  "dataset": {"version": "01a0f073-f7ab-73d8-bf10-586563066827", "updated_at": "2026-09-30T03:55:26Z"}
}
```

`GET /rootzone/tld/top` — 2 DS (algoritmos 8 e 13) e servidores só com
IPv4 ou só com IPv6. Trecho (3 dos 8 NS):

```json
{
  "tld": "top",
  "tld_unicode": "top",
  "nameservers": [
    {"name": "a.zdnscloud.cn", "ttl": 172800, "ipv4": [{"address": "203.99.24.1", "ttl": 172800}], "ipv6": []},
    …
    {"name": "e.zdnscloud.cn", "ttl": 172800, "ipv4": [{"address": "203.119.82.1", "ttl": 172800}], "ipv6": [{"address": "2401:8d00:15::1", "ttl": 172800}]},
    …
    {"name": "i.zdnscloud.cn", "ttl": 172800, "ipv4": [], "ipv6": [{"address": "2401:8d00:1::1", "ttl": 172800}]},
    …
  ],
  "ds": [
    {"key_tag": 26780, "algorithm": 8, "digest_type": 2, "digest": "5D6E7869EE8E3B536A617DE89482DDD1DCB9DB9DBB1AC33D6ED351E2CA095B1B", "ttl": 86400},
    {"key_tag": 41508, "algorithm": 13, "digest_type": 2, "digest": "31422CDF4A9AF99914FF85C97D4FB2291F293C9ADB26011B39E7638A51E1C7DD", "ttl": 86400}
  ],
  "first_seen": "2026-09-30T03:55:26Z",
  "updated_at": "2026-09-30T03:55:26Z",
  "zone": {"serial": 2026092901},
  "dataset": {"version": "01a0f073-f7ab-73d8-bf10-586563066827", "updated_at": "2026-09-30T03:55:26Z"}
}
```

`GET /rootzone/tld/рф` (= `/tld/%D1%80%D1%84`, `/tld/xn--p1ai`,
`/tld/XN--P1AI.`), ETag `W/"8c99d0d875f78a75"`. Trecho (1 dos 6 NS; os
outros são `b`, `d`, `e` e `f.dns.ripn.net` e `c.tld-servers.ru`, todos com
IPv4 e IPv6):

```json
{
  "tld": "xn--p1ai",
  "tld_unicode": "рф",
  "nameservers": [
    {"name": "a.dns.ripn.net", "ttl": 172800, "ipv4": [{"address": "193.232.128.6", "ttl": 172800}], "ipv6": [{"address": "2001:678:17:0:193:232:128:6", "ttl": 172800}]},
    …
  ],
  "ds": [
    {"key_tag": 60491, "algorithm": 8, "digest_type": 2, "digest": "87F1F8C82EC00047C43AC499A73CC9BEB4FC1503E8558F086DCFB614405F7F21", "ttl": 86400}
  ],
  "first_seen": "2026-09-30T03:55:26Z",
  "updated_at": "2026-09-30T03:55:26Z",
  "zone": {"serial": 2026092901},
  "dataset": {"version": "01a0f073-f7ab-73d8-bf10-586563066827", "updated_at": "2026-09-30T03:55:26Z"}
}
```

Erros:

```json
{"error": {"code": "bad_request", "message": "TLD inválido: use um rótulo só, em ASCII (letras, dígitos, - e _, até 63 caracteres; ex.: br, xn--p1ai) ou em Unicode (ex.: рф), com ou sem o ponto final"}}
{"error": {"code": "not_found", "message": "o TLD nada-disso não está delegado na zona raiz"}}
```

A mensagem do 404 traz o nome normalizado, qualquer que seja a forma
pedida (`/tld/ОНЛАЙН.` num banco sem ele: `o TLD xn--80asehdb não está
delegado na zona raiz`). Também: 503 `dataset_not_ready` ou
`database_unavailable`, 504 `timeout`, 500 `internal_error` (panic ou
registro fora do formato, [api.md](api.md#consultas)).

## `GET /rootzone/meta`

Estado dos dados, lido do banco a cada pedido — sem Valkey, sem ETag,
`Cache-Control: no-store`: a última execução aplicada de `rootzone_run`,
com o SOA da zona, e a linha `collector-rootzone` de `jobs`
([dados.md](dados.md#consultas-da-api)), as duas consultas dentro de um
prazo `DB_TIMEOUT`. Responde 200 também antes da primeira carga.

| Campo | Tipo | Significado |
|---|---|---|
| `app` | texto | `api-rootzone` |
| `version` | texto | versão do build (`buildinfo.Version`) |
| `dataset` | objeto ou `null` | última execução aplicada; `null` antes da primeira |
| `dataset.version` | texto (uuid) | `rootzone_run.uuid`: a versão usada no cache e no ETag |
| `dataset.updated_at` | timestamp | `rootzone_run.created_at` da execução |
| `dataset.source` | texto | `rootzone_run.url`: de onde o arquivo foi baixado |
| `dataset.sha256` | texto | `rootzone_run.sha256` (hex); `""` se a linha não tiver |
| `dataset.serial` | inteiro ou `null` | `rootzone_run.serial`: serial do SOA |
| `dataset.soa` | objeto ou `null` | o SOA sem o serial; `null` se a linha não tiver `soa_mname` |
| `dataset.soa.mname` | texto | `soa_mname`: servidor primário |
| `dataset.soa.rname` | texto ou `null` | `soa_rname`: caixa do responsável, em forma de nome DNS |
| `dataset.soa.refresh`, `.retry`, `.expire`, `.minimum` | inteiro ou `null` | os temporizadores, em segundos |
| `dataset.tlds` | inteiro ou `null` | `rootzone_run.tlds`: TLDs delegados |
| `dataset.records` | inteiro ou `null` | `rootzone_run.records`: RRs guardadas (todas menos RRSIG) |
| `dataset.rrsigs` | inteiro ou `null` | `rootzone_run.rrsigs`: RRSIG contados, não guardados |
| `collector` | objeto ou `null` | a linha do coletor em `jobs`; `null` enquanto ela não existe |
| `collector.app` | texto | `collector-rootzone` |
| `collector.last_sync_at` | timestamp ou `null` | `jobs.last_sync_at` |
| `collector.last_check_at` | timestamp ou `null` | `jobs.last_check_at` |
| `collector.consolidated` | booleano | `jobs.consolidated = 1` ([postgres.md](../../plataforma/postgres.md#tabela-jobs)) |

Os campos que vêm do parser (`serial`, `soa`, `tlds`, `records`, `rrsigs`)
são sempre preenchidos nas execuções aplicadas
([dados.md](dados.md#rootzone_run)); `null` só numa linha fora do padrão.

```json
{
  "app": "api-rootzone",
  "version": "0.1.0",
  "dataset": {
    "version": "01a0f073-f7ab-73d8-bf10-586563066827",
    "updated_at": "2026-09-30T03:55:26Z",
    "source": "https://www.internic.net/domain/root.zone",
    "sha256": "78e2a7f6f7c151534979196f781ba5ce7c5d9b8ce84eee84372d60fd86bda8ec",
    "serial": 2026092901,
    "soa": {"mname": "a.root-servers.net", "rname": "nstld.verisign-grs.com", "refresh": 1800, "retry": 900, "expire": 604800, "minimum": 86400},
    "tlds": 1438,
    "records": 22131,
    "rrsigs": 2794
  },
  "collector": {"app": "collector-rootzone", "last_sync_at": "2026-09-30T03:55:26Z", "last_check_at": "2026-09-30T03:55:26Z", "consolidated": false}
}
```

Antes da primeira carga: `{"app": "api-rootzone", "version": "0.1.0", "dataset": null, "collector": null}`.

Erros: 503 `database_unavailable`, 504 `timeout`, 500 `internal_error`
(nunca 400, 404 nem `dataset_not_ready`).

## `GET /rootzone/` e `GET /rootzone/v1/`

Índice, igual nos dois caminhos; não consulta o banco nem o Valkey.
`Cache-Control: public, max-age=300`, sem ETag.

| Campo | Tipo | Significado |
|---|---|---|
| `app` | texto | `api-rootzone` |
| `version` | texto | versão do build |
| `base_path` | texto | `BASE_PATH` normalizado |
| `versions` | lista de texto | `["v1"]`; a última é a das rotas sem versão |
| `endpoints` | lista de texto | as rotas de dados (na ordem de `dataRoutes`), `/meta`, `/status` e `/openapi.yaml`, já com o caminho de base (não traz `/health`, `/ping` nem as rotas com `/v1`) |
| `source` | texto | `https://www.internic.net/domain/root.zone` (constante `SourceURL`, não vem do banco) |

```json
{
  "app": "api-rootzone",
  "version": "0.1.0",
  "base_path": "/rootzone",
  "versions": ["v1"],
  "endpoints": ["/rootzone/tlds", "/rootzone/tld/{tld}", "/rootzone/meta", "/rootzone/status", "/rootzone/openapi.yaml"],
  "source": "https://www.internic.net/domain/root.zone"
}
```

## Saúde: `/rootzone/health`, `/rootzone/status` e `/rootzone/ping`

Como no [padrão](../../padroes/api.md#saúde), com `<fonte>` = `rootzone`
(mensagens `api-rootzone operacional` e
`aguardando a primeira sincronização do collector-rootzone`). Respostas
reais, com a imagem no ar:

```json
{"success": true, "status": "ok", "timestamp": "2026-09-30T04:08:14Z", "message": "api-rootzone operacional", "checks": {"dataset": "ok", "postgres": "ok", "valkey": "ok"}}
{"success": true, "status": "degraded", "timestamp": "2026-09-30T04:08:22Z", "message": "Valkey indisponível; respondendo sem cache", "checks": {"dataset": "ok", "postgres": "ok", "valkey": "error"}}
{"success": false, "status": "error", "timestamp": "2026-09-30T04:08:22Z", "message": "PostgreSQL indisponível", "checks": {"dataset": "ok", "postgres": "error", "valkey": "ok"}}
```

- `/health` e `/status` são o mesmo handler, em GET e POST (o corpo do POST
  é ignorado). Precedência `error` > `degraded` > `starting` > `ok`.
- Com o Postgres fora, as respostas que já estão no Valkey continuam saindo
  (`X-Cache: HIT`); as outras são 503 `database_unavailable`.
- `/rootzone/ping`: `pong` em `text/plain; charset=utf-8`.

## `GET /rootzone/openapi.yaml`

O manifesto embutido, como em
[openapi.md](../../padroes/openapi.md#resposta-de-get-fonteopenapiyaml);
`/rootzone/v1/openapi.yaml` é 404. Conteúdo:
[api.md](api.md#manifesto-openapi).

## Erros

Formato, `Cache-Control: no-store` e códigos: [padrão](../../padroes/api.md#erros).
Mensagens reais desta API:

| HTTP | `code` | `message` | Onde |
|---|---|---|---|
| 400 | `bad_request` | `TLD inválido: use um rótulo só, em ASCII (letras, dígitos, - e _, até 63 caracteres; ex.: br, xn--p1ai) ou em Unicode (ex.: рф), com ou sem o ponto final` | `/tld` |
| 404 | `not_found` | `o TLD <tld> não está delegado na zona raiz` | `/tld` |
| 404 | `not_found` | `rota inexistente; veja /rootzone/` | qualquer outro caminho, ou método não registrado |
| 503 | `dataset_not_ready` | `a primeira sincronização do collector-rootzone ainda não terminou; tente em alguns minutos` | rotas de dados |
| 503 | `database_unavailable` | `banco de dados indisponível` | rotas de dados e `/meta` |
| 504 | `timeout` | `a consulta demorou demais` | rotas de dados e `/meta` |
| 500 | `internal_error` | `erro interno` | panic, em qualquer rota; registro fora do formato em `/tld` |

## Roteamento

`ServeMux` do Go 1.27 com o catch-all `/` registrado:

| Pedido | Resposta |
|---|---|
| `/rootzone` (com ou sem query) | 301, `Location: /rootzone/` (a query se perde) |
| `/rootzone/v1` (sem barra) | 307, `Location: /rootzone/v1/`, corpo HTML do `ServeMux` (não é JSON) |
| caminho com `//`, `.` ou `..` (`/rootzone//tld/br` → `/rootzone/tld/br`; `/rootzone/tld/.` → `/rootzone/tld`) | 307 para o caminho limpo, corpo HTML do `ServeMux` |
| barra no fim de uma rota (`/rootzone/tld/br/`), `/rootzone/tld`, `/rootzone/tld/`, `/rootzone/tlds/br` | 404 JSON |
| método não registrado numa rota existente (`DELETE /rootzone/tld/br`, `POST /rootzone/tlds`, `PUT /rootzone/status`) | 404 JSON `rota inexistente; veja /rootzone/` — o catch-all casa antes do 405 do `ServeMux` |
| `OPTIONS` em qualquer caminho | 204, preflight de CORS |
| `/rootzone/v2/...`, `/rootzone/v1/health`, `/rootzone/v1/status`, `/rootzone/v1/ping`, `/rootzone/v1/openapi.yaml` | 404 JSON |
