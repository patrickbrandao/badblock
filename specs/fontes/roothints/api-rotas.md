# roothints — rotas da API

Cada rota da `api-roothints`: parâmetros, validação, normalização, chave de
cache, campos da resposta, exemplos reais e erros. O que vale para toda API
(formato do JSON, `HEAD`, `OPTIONS`, cabeçalhos, `serveCached`, ETag, saúde,
formato dos erros) está no [padrão](../../padroes/api.md); valores, cache,
configuração, medições e testes desta API, em [api.md](api.md); o SQL de
cada rota, em [dados.md](dados.md#consultas-da-api).

Os exemplos são respostas reais: o `named.root` publicado em 2026-09-30
(cabeçalho `last update: September 24, 2026`, serial `2026092401`, 13
servidores) carregado pelo `collector-roothints --once` num PostgreSQL 18
descartável em 2026-09-30T03:47:52Z (versão do dataset
`01a0f06d-08c2-7f4a-924a-b345129d0f5a`) e a API `0.1.0` rodando sobre ele,
com Valkey. Estão formatados para leitura — a API responde JSON compacto,
numa linha.

## Rotas

| Rota | Métodos | O quê | Chave de cache |
|---|---|---|---|
| `/roothints/servers` | GET | os servidores raiz, em ordem de letra | `servers` |
| `/roothints/server/{server}` | GET | um servidor, pela letra ou pelo nome | `server:<letra>` |
| `/roothints/meta` | GET | estado dos dados | sem cache |
| `/roothints/` | GET | índice | sem cache |
| `/roothints/health`, `/roothints/status` | GET, POST | saúde | — |
| `/roothints/ping` | GET | `pong` | — |
| `/roothints/openapi.yaml` | GET | manifesto OpenAPI 3.1 | — |

As quatro primeiras respondem também em `/roothints/v1/...`, com o mesmo
conteúdo, a mesma chave e o mesmo ETag; as três últimas ficam fora do
versionamento. Todo `GET` aceita `HEAD`.

Nas rotas de dados:

- A validação vem **antes de tudo**: pedido inválido recebe 400 mesmo antes
  da primeira carga do coletor. Depois vêm, nesta ordem, a versão do dataset
  (503 `dataset_not_ready`), o `If-None-Match` (304), o Valkey e o Postgres.
- Toda resposta 200 traz o bloco `dataset` (`version`, `updated_at`) do
  [padrão](../../padroes/api.md#formato-das-respostas) e o bloco `source`
  (abaixo).

### Campos de um servidor

Iguais na lista e na rota de um servidor; saem de `roothints_server`
([dados.md](dados.md#roothints_server)).

| Campo | Tipo | Significado |
|---|---|---|
| `name` | texto | nome em minúsculas e sem o ponto final (`a.root-servers.net`) |
| `letter` | texto | letra do servidor (`a`), o primeiro rótulo de `name` |
| `ipv4` | texto ou `null` | endereço do registro `A`, **sem máscara** (`198.41.0.4`, não `198.41.0.4/32`); `null` se o arquivo não traz `A` |
| `ipv6` | texto ou `null` | endereço do registro `AAAA`, sem máscara e na forma curta (`2001:503:ba3e::2:30`); `null` se o arquivo não traz `AAAA` |
| `ns_ttl` | inteiro | TTL, em segundos, da linha `. NS` (hoje `3600000`, ~41,7 dias) |
| `ipv4_ttl`, `ipv6_ttl` | inteiro ou `null` | TTL do `A` e do `AAAA`; `null` junto com o endereço |
| `note` | texto ou `null` | comentário do bloco no arquivo, sem o `;`, como publicado (`FORMERLY NS.INTERNIC.NET`, `OPERATED BY VERISIGN, INC.`); `null` se o bloco não tem comentário |

No arquivo de 2026-09-30 todos os servidores têm os dois endereços e um
comentário; os `null` existem para um arquivo futuro que o coletor aceite
com aviso ([fonte.md](fonte.md#regras-do-parser)).

### Bloco `source`

O cabeçalho do `named.root` **da versão usada na resposta**, lido da mesma
linha de `roothints_run` que dá a versão do dataset (o watcher guarda os
dois juntos; nenhuma consulta a mais):

| Campo | Tipo | Significado |
|---|---|---|
| `last_update` | data (`AAAA-MM-DD`) ou `null` | `last update:` do cabeçalho (`2026-09-24`) |
| `zone_serial` | inteiro ou `null` | `related version of root zone:`, o serial da zona raiz (`AAAAMMDDnn`, `2026092401`) |

Nas linhas aplicadas os dois estão sempre preenchidos
([dados.md](dados.md#mapeamento-da-fonte-para-as-colunas)); `null` só numa
linha fora do padrão.

## `GET /roothints/servers`

Todos os servidores raiz, em ordem de letra (`ORDER BY letter`). Sem
parâmetros; a lista é sempre inteira (13 itens, 2.462 bytes).

- Chave: `servers`.

| Campo | Tipo | Significado |
|---|---|---|
| `source` | objeto | [bloco `source`](#bloco-source) |
| `count` | inteiro | servidores na lista |
| `servers` | lista | os servidores ([campos](#campos-de-um-servidor)); `[]` se a tabela estiver vazia |
| `dataset` | objeto | versão dos dados |

```json
{
  "source": {"last_update": "2026-09-24", "zone_serial": 2026092401},
  "count": 13,
  "servers": [
    {"name": "a.root-servers.net", "letter": "a", "ipv4": "198.41.0.4", "ipv6": "2001:503:ba3e::2:30", "ns_ttl": 3600000, "ipv4_ttl": 3600000, "ipv6_ttl": 3600000, "note": "FORMERLY NS.INTERNIC.NET"},
    {"name": "b.root-servers.net", "letter": "b", "ipv4": "170.247.170.2", "ipv6": "2801:1b8:10::b", "ns_ttl": 3600000, "ipv4_ttl": 3600000, "ipv6_ttl": 3600000, "note": "FORMERLY NS1.ISI.EDU"},
    {"name": "c.root-servers.net", "letter": "c", "ipv4": "192.33.4.12", "ipv6": "2001:500:2::c", "ns_ttl": 3600000, "ipv4_ttl": 3600000, "ipv6_ttl": 3600000, "note": "FORMERLY C.PSI.NET"},
    {"name": "d.root-servers.net", "letter": "d", "ipv4": "199.7.91.13", "ipv6": "2001:500:2d::d", "ns_ttl": 3600000, "ipv4_ttl": 3600000, "ipv6_ttl": 3600000, "note": "FORMERLY TERP.UMD.EDU"},
    {"name": "e.root-servers.net", "letter": "e", "ipv4": "192.203.230.10", "ipv6": "2001:500:a8::e", "ns_ttl": 3600000, "ipv4_ttl": 3600000, "ipv6_ttl": 3600000, "note": "FORMERLY NS.NASA.GOV"},
    {"name": "f.root-servers.net", "letter": "f", "ipv4": "192.5.5.241", "ipv6": "2001:500:2f::f", "ns_ttl": 3600000, "ipv4_ttl": 3600000, "ipv6_ttl": 3600000, "note": "FORMERLY NS.ISC.ORG"},
    {"name": "g.root-servers.net", "letter": "g", "ipv4": "192.112.36.4", "ipv6": "2001:500:12::d0d", "ns_ttl": 3600000, "ipv4_ttl": 3600000, "ipv6_ttl": 3600000, "note": "FORMERLY NS.NIC.DDN.MIL"},
    {"name": "h.root-servers.net", "letter": "h", "ipv4": "198.97.190.53", "ipv6": "2001:500:1::53", "ns_ttl": 3600000, "ipv4_ttl": 3600000, "ipv6_ttl": 3600000, "note": "FORMERLY AOS.ARL.ARMY.MIL"},
    {"name": "i.root-servers.net", "letter": "i", "ipv4": "192.36.148.17", "ipv6": "2001:7fe::53", "ns_ttl": 3600000, "ipv4_ttl": 3600000, "ipv6_ttl": 3600000, "note": "FORMERLY NIC.NORDU.NET"},
    {"name": "j.root-servers.net", "letter": "j", "ipv4": "192.58.128.30", "ipv6": "2001:503:c27::2:30", "ns_ttl": 3600000, "ipv4_ttl": 3600000, "ipv6_ttl": 3600000, "note": "OPERATED BY VERISIGN, INC."},
    {"name": "k.root-servers.net", "letter": "k", "ipv4": "193.0.14.129", "ipv6": "2001:7fd::1", "ns_ttl": 3600000, "ipv4_ttl": 3600000, "ipv6_ttl": 3600000, "note": "OPERATED BY RIPE NCC"},
    {"name": "l.root-servers.net", "letter": "l", "ipv4": "199.7.83.42", "ipv6": "2001:500:9f::42", "ns_ttl": 3600000, "ipv4_ttl": 3600000, "ipv6_ttl": 3600000, "note": "OPERATED BY ICANN"},
    {"name": "m.root-servers.net", "letter": "m", "ipv4": "202.12.27.33", "ipv6": "2001:dc3::35", "ns_ttl": 3600000, "ipv4_ttl": 3600000, "ipv6_ttl": 3600000, "note": "OPERATED BY WIDE"}
  ],
  "dataset": {"version": "01a0f06d-08c2-7f4a-924a-b345129d0f5a", "updated_at": "2026-09-30T03:47:52Z"}
}
```

Sem 400 nem 404 (o `?` e qualquer query string são ignorados).

## `GET /roothints/server/{server}`

Um servidor raiz, pela letra ou pelo nome.

- `{server}`: sem diferenciar maiúsculas, uma das duas formas:
  - a **letra**, uma de `a` a `z` (`k`, `K`);
  - o **nome** `<letra>.root-servers.net`, com ou sem o ponto final
    (`k.root-servers.net`, `K.ROOT-SERVERS.NET.`).

  Vale de `a` a `z` e não só de `a` a `m`: é a regra do schema
  (`chk_roothints_server_name`) e do parser do coletor
  ([fonte.md](fonte.md#regras-do-parser)), então uma letra nova na fonte
  aparece sem mudar a API. Letra válida que não está no arquivo (`n`,
  `z.root-servers.net`) é **404**. O resto é **400**: mais de uma letra
  (`ab`), dígito (`1`), ponto depois da letra sozinha (`a.`), dois pontos
  finais (`a.root-servers.net..`), outro domínio (`a.example.com`), espaço
  e qualquer byte não-ASCII — conferido **antes** de passar para
  minúsculas, porque `strings.ToLower` (e o `(?i)` do `regexp`) levaria o
  sinal de kelvin `K` (U+212A) a `k`.
- Normalização (`normalizeServer`): minúsculas, sem o ponto final, e o nome
  reduzido à letra — as constraints de `roothints_server` garantem
  `name = letter || '.root-servers.net'`, então letra e nome são a mesma
  consulta. Chave `server:<letra>`: `/server/k`, `/server/K`,
  `/server/k.root-servers.net` e `/v1/server/K.ROOT-SERVERS.NET.` usam
  `server:k`, o mesmo ETag e uma consulta só ao banco
  (`WHERE letter = $1`, [dados.md](dados.md#consultas-da-api)).

Campos: os do [servidor](#campos-de-um-servidor), mais:

| Campo | Tipo | Significado |
|---|---|---|
| `first_seen` | timestamp | `roothints_server.created_at`: quando **este banco** viu o servidor pela primeira vez (apagar o volume e recarregar zera a data — [dados.md](dados.md#roothints_server)) |
| `updated_at` | timestamp | `roothints_server.updated_at`: última mudança de endereço, TTL ou comentário |
| `source` | objeto | [bloco `source`](#bloco-source) |
| `dataset` | objeto | versão dos dados |

`GET /roothints/server/a` (e `/roothints/server/A.ROOT-SERVERS.NET.`, que
veio do cache — `X-Cache: HIT` — com o mesmo ETag `W/"c618b310d98767be"`):

```json
{
  "name": "a.root-servers.net",
  "letter": "a",
  "ipv4": "198.41.0.4",
  "ipv6": "2001:503:ba3e::2:30",
  "ns_ttl": 3600000,
  "ipv4_ttl": 3600000,
  "ipv6_ttl": 3600000,
  "note": "FORMERLY NS.INTERNIC.NET",
  "first_seen": "2026-09-30T03:47:52Z",
  "updated_at": "2026-09-30T03:47:52Z",
  "source": {"last_update": "2026-09-24", "zone_serial": 2026092401},
  "dataset": {"version": "01a0f06d-08c2-7f4a-924a-b345129d0f5a", "updated_at": "2026-09-30T03:47:52Z"}
}
```

`GET /roothints/server/k.root-servers.net`:
`{"name": "k.root-servers.net", "letter": "k", "ipv4": "193.0.14.129", "ipv6": "2001:7fd::1", …, "note": "OPERATED BY RIPE NCC", …}`.

| HTTP | Mensagem |
|---|---|
| 400 | `servidor inválido: use a letra (ex.: a) ou o nome (ex.: a.root-servers.net), sem diferenciar maiúsculas` |
| 404 | `o servidor raiz <letra>.root-servers.net não consta no named.root da InterNIC`, com o nome normalizado (`/server/n` → `o servidor raiz n.root-servers.net não consta no named.root da InterNIC`) |

## `GET /roothints/meta`

Estado dos dados, lido na hora (sem Valkey e sem ETag; `Cache-Control:
no-store`). `dataset` é a última execução aplicada de `roothints_run` e
`collector` a linha `collector-roothints` de `jobs`
([dados.md](dados.md#consultas-da-api)); os dois são `null` antes da
primeira carga, e a rota responde 200 mesmo assim.

| Campo de `dataset` | Tipo | Significado |
|---|---|---|
| `version` | uuid | versão do dataset (`roothints_run.uuid`) |
| `updated_at` | timestamp | `created_at` da execução |
| `source` | URL | de onde o arquivo foi baixado (`roothints_run.url`) |
| `md5`, `sha256` | texto | hashes do arquivo, em hexadecimal minúsculo |
| `last_update` | data ou `null` | `last update:` do cabeçalho |
| `zone_serial` | inteiro ou `null` | serial da zona raiz do cabeçalho |
| `servers` | inteiro | servidores lidos do arquivo |

`collector`: `app` (`collector-roothints`), `last_sync_at`, `last_check_at`
(timestamp ou `null`) e `consolidated` (booleano), como no
[padrão](../../padroes/api.md#rotas-comuns).

```json
{
  "app": "api-roothints",
  "version": "0.1.0",
  "dataset": {
    "version": "01a0f06d-08c2-7f4a-924a-b345129d0f5a",
    "updated_at": "2026-09-30T03:47:52Z",
    "source": "https://www.internic.net/domain/named.root",
    "md5": "d0732825a760fee171258b4890ca5243",
    "sha256": "18f27fc4801c9a16337047cb2e18419a42623cfd15f53b73cb37c98f496e7730",
    "last_update": "2026-09-24",
    "zone_serial": 2026092401,
    "servers": 13
  },
  "collector": {
    "app": "collector-roothints",
    "last_sync_at": "2026-09-30T03:47:52Z",
    "last_check_at": "2026-09-30T03:47:52Z",
    "consolidated": false
  }
}
```

Antes da primeira carga:
`{"app": "api-roothints", "version": "0.1.0", "dataset": null, "collector": null}`.

Erros: 503 `database_unavailable` e 504 `timeout` do
[padrão](../../padroes/api.md#erros).

## `GET /roothints/` (índice)

`Cache-Control: public, max-age=300`, sem ETag; igual em `/roothints/v1/`.
`endpoints` sai da mesma lista que registra as rotas de dados
(`dataRoutes`), mais `/meta`, `/status` e `/openapi.yaml`.

```json
{
  "app": "api-roothints",
  "version": "0.1.0",
  "base_path": "/roothints",
  "versions": ["v1"],
  "endpoints": [
    "/roothints/servers",
    "/roothints/server/{server}",
    "/roothints/meta",
    "/roothints/status",
    "/roothints/openapi.yaml"
  ],
  "source": "https://www.internic.net/domain/named.root"
}
```

## Saúde, `ping` e manifesto

Como no [padrão](../../padroes/api.md#saúde). `GET /roothints/status` com
tudo no ar:

```json
{"success": true, "status": "ok", "timestamp": "2026-09-30T03:47:59Z", "message": "api-roothints operacional",
 "checks": {"dataset": "ok", "postgres": "ok", "valkey": "ok"}}
```

Antes da primeira carga:

```json
{"success": true, "status": "starting", "timestamp": "2026-09-30T03:47:45Z",
 "message": "aguardando a primeira sincronização do collector-roothints",
 "checks": {"dataset": "empty", "postgres": "ok", "valkey": "ok"}}
```

`/roothints/ping` responde `pong`; `/roothints/openapi.yaml`, o manifesto
([api.md](api.md#manifesto-openapi)).

## Erros

Além dos 400 e 404 de `/server/{server}` (acima), os do
[padrão](../../padroes/api.md#erros), com as mensagens reais:

| HTTP | `code` | Mensagem |
|---|---|---|
| 404 | `not_found` | `rota inexistente; veja /roothints/` (caminho desconhecido, `/roothints/v2/...`, `/roothints/server/` sem valor, `/roothints/server/a/b`, `/roothints/v1/status`, método que a rota não aceita) |
| 503 | `dataset_not_ready` | `a primeira sincronização do collector-roothints ainda não terminou; tente em alguns minutos` |
| 503 | `database_unavailable` | `banco de dados indisponível` |
| 504 | `timeout` | `a consulta demorou demais` |
| 500 | `internal_error` | `erro interno` |

Antes da primeira carga, `GET /roothints/servers`:

```json
{"error": {"code": "dataset_not_ready", "message": "a primeira sincronização do collector-roothints ainda não terminou; tente em alguns minutos"}}
```

## Roteamento

- `/roothints` → 301 para `/roothints/`; `/roothints/v1` → 307 do
  `http.ServeMux` para `/roothints/v1/`; caminhos com `//`, `.` ou `..` →
  307 para o caminho limpo (`/roothints//` → `/roothints/`).
- `OPTIONS` em qualquer caminho: 204 com os cabeçalhos de CORS do padrão.
- Um `%2F` dentro de `{server}` não vira separador (o `ServeMux` casa pelo
  caminho escapado) e cai no 400 da validação.
