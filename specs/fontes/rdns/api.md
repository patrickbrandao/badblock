# API `api-rdns`

O que a `api-rdns` faz de diferente ou a mais que o
[padrão das APIs](../../padroes/api.md) e o
[padrão do manifesto](../../padroes/openapi.md); o resto (estrutura do
código, `main.go`, caminho de base e versões, formato do JSON, cabeçalhos
comuns, erros, IP real, configuração comum, container) segue os padrões.
Tabelas, consultas SQL e a fila de tempo real: [dados.md](dados.md). Dono:
sub-agente `api-rdns`.

## Valores desta fonte

| Item | Valor |
|---|---|
| App | `api-rdns` |
| Caminho de base | `BASE_PATH` = `/rdns`; em produção, `https://api.badblock.net.br/rdns/` |
| Porta no loopback | 8113 (`API_RDNS_HOST_PORT`) |
| Tabelas lidas | `rdns_prefix`, `rdns_ptr`; no `/meta`, também `rdns_run`, `rdns_worker`, `rdns_config` e a linha `collector-rdns` de `jobs` |
| Escreve | **nada** no Postgres; no Valkey, o cache e a fila de tempo real ([dados.md](dados.md#fila-de-tempo-real-valkey)) |
| Pacotes | os do padrão, com `internal/dataset` reduzido a "há prefixos?", e `internal/live` (o pedido de tempo real) |
| Nomes nas conexões | `application_name=api-rdns`; `ClientName` `badblock-api-rdns` (cache) e `badblock-api-rdns-live` (tempo real) |
| Fonte citada no índice | `source`: `https://github.com/patrickbrandao/badblock/tree/main/specs/fontes/rdns`; `derived_from`: `["cgibr"]` |
| Descrição da imagem | `API HTTP do DNS reverso (PTR) dos IPv4 varridos pelo BadBlock` |

## Desvios do padrão

O dado do rdns muda um prefixo de cada vez, o dia inteiro: não existe uma
"versão do dataset" que mude junto para tudo. Por isso (decisão #33):

| Padrão | Na `api-rdns` |
|---|---|
| versão do dataset na chave de cache e no ETag | chave **sem versão**; o dado em cache pode ter até `REDIS_KEY_TTL` (1 h) de atraso em relação ao banco |
| ETag = FNV-1a de `versão + 0x00 + chave`; 304 sem consultar nada | ETag = `W/"<FNV-1a de 64 bits do corpo>"`, em hexadecimal minúsculo; o 304 precisa do corpo (do cache ou do banco) para comparar |
| bloco `dataset` em toda rota de dados | bloco **`scan`**: o prefixo que contém o pedido e a data da varredura dele |
| cabeçalho `X-Dataset-Version` | não existe (nem em `Access-Control-Expose-Headers`) |
| `503 dataset_not_ready` antes da primeira carga | antes de haver **qualquer** linha em `rdns_prefix` (a primeira derivação, ou um estático) |
| a API não escreve nada | escreve na fila de tempo real do Valkey |
| `X-Cache`: `HIT`, `MISS`, `BYPASS` | também `LIVE` (resposta de tempo real) |
| códigos de erro do padrão | também `503 live_queue_full` |
| um router no Traefik | um segundo router para `live=true`, com rate limit próprio |

## Rotas

Todas abaixo de `/rdns`; as de dados, o índice e `/meta` também em
`/rdns/v1/...`. Não há rota por ASN (um ASN tem até 43.656 /24 —
[fonte.md](fonte.md#fatos-medidos)).

| Rota | `operationId` | Tag | Chave de cache |
|---|---|---|---|
| `GET /rdns/ip/{ip}` (`?live=true\|false`) | `getIP` | `dados` | `badblock:api-rdns:ip:200.160.2.3` |
| `GET /rdns/prefix/{ip}/{len}` | `getPrefix` | `dados` | `badblock:api-rdns:prefix:200.160.2.0/24` |
| `GET /rdns/meta` | `getMeta` | `meta` | — (sem cache) |
| `GET /rdns/` | `getIndex` | `meta` | — |
| `GET`, `POST /rdns/health` e `/rdns/status` | `getHealth`, `postHealth`, `getStatus`, `postStatus` | `saúde` | — |
| `GET /rdns/ping` / `GET /rdns/openapi.yaml` | `ping` / `getOpenAPI` | `saúde` / `meta` | — |

## IP — `GET /rdns/ip/{ip}`

- `{ip}`: IPv4 que `netip.ParseAddr` aceita; o IPv4 mapeado em IPv6 vira
  IPv4. IPv6 → 400 `o rdns varre só IPv4`; o resto → 400 `endereço IP
  inválido`. Chave: `ip:<IPv4 com pontos>`.
- `live`: ausente, `false` ou `true` — **só essas grafias**, uma vez só.
  `live=1`, `live=TRUE` ou `live` repetido → 400 `live inválido: use true ou
  false` (o router do Traefik casa só `live=true`; outra grafia escaparia do
  rate limit dele).
- IP dentro de um prefixo de `rdns_prefix` (ativo ou não): **200**. Fora de
  qualquer prefixo: 404 `<ip> não está em nenhum prefixo do rdns`.

| Campo | Tipo | Conteúdo |
|---|---|---|
| `ip` | texto | o IPv4 consultado, normalizado |
| `status` | texto | `ptr`, `no_ptr`, `servfail`, `refused`, `timeout` (de `rdns_ptr.status`) ou, sem linha em `rdns_ptr`, `no_ptr` (prefixo já varrido) ou `not_scanned` (nunca varrido) |
| `name` | texto ou `null` | `rdns_ptr.name`; `null` fora do `ptr` |
| `names` | lista | `rdns_ptr.names`; `[]` fora do `ptr` |
| `first_seen`, `updated_at` | timestamp ou `null` | `rdns_ptr.created_at` e `updated_at` (última mudança do registro); `null` sem linha |
| `scan` | objeto | `prefix`, `asn`, `source`, `active` e `checked_at` (`last_scan_at`: quando o dado foi conferido; `null` = nunca) do prefixo que contém o IP |
| `live` | objeto | só com `live=true` ([abaixo](#tempo-real-livetrue)) |

Formato (resposta real, 2026-10-10, do stack local com a derivação do arquivo
do NIC.br do dia e o `200.160.2.0/24` varrido pelo `1.1.1.1`):

```json
{
  "ip": "200.160.2.3",
  "status": "ptr",
  "name": "registro.br",
  "names": ["registro.br"],
  "first_seen": "2026-10-10T17:26:56Z",
  "updated_at": "2026-10-10T17:26:56Z",
  "scan": {"prefix": "200.160.2.0/24", "asn": 22548, "source": "cgibr", "active": true, "checked_at": "2026-10-10T17:26:56Z"}
}
```

## Prefixo — `GET /rdns/prefix/{ip}/{len}`

Todos os endereços de um prefixo de /24 a /31.

- `{ip}` como em `/ip`; `{len}` inteiro de **24 a 31** (`/32` é o `/ip`); fora
  disso → 400 `tamanho de prefixo inválido: use de 24 a 31`. Bits de host
  zerados; chave `prefix:<prefixo zerado>`.
- `live` → 400 `live só vale em /rdns/ip`.
- O prefixo pedido tem de estar **contido** num prefixo de `rdns_prefix`;
  senão 404 `<prefixo> não está contido em nenhum prefixo do rdns`.

| Campo | Conteúdo |
|---|---|
| `prefix` | o prefixo pedido, normalizado |
| `scan` | como em `/ip`, do prefixo que o contém |
| `counts` | `ptr`, `no_ptr`, `servfail`, `refused`, `timeout`, `not_scanned`: quantos itens em cada estado |
| `items` | um por endereço, em ordem: `ip`, `status`, `name`, `names` (as regras de `/ip`) |

Formato, com o `200.160.2.0/30` (resposta real, 2026-10-10, a mesma
varredura):

```json
{
  "prefix": "200.160.2.0/30",
  "scan": {"prefix": "200.160.2.0/24", "asn": 22548, "source": "cgibr", "active": true, "checked_at": "2026-10-10T17:26:56Z"},
  "counts": {"ptr": 2, "no_ptr": 2, "servfail": 0, "refused": 0, "timeout": 0, "not_scanned": 0},
  "items": [
    {"ip": "200.160.2.0", "status": "no_ptr", "name": null, "names": []},
    {"ip": "200.160.2.1", "status": "ptr", "name": "ae1-0.ar1.vr.registro.br", "names": ["ae1-0.ar1.vr.registro.br"]},
    {"ip": "200.160.2.2", "status": "no_ptr", "name": null, "names": []},
    {"ip": "200.160.2.3", "status": "ptr", "name": "registro.br", "names": ["registro.br"]}
  ]
}
```

Um /24 sai com ~256 itens (~20 KB).

## Tempo real (`live=true`)

Só em `/rdns/ip`. A resposta tem sempre o corpo de `/ip` mais o bloco
`live`: `status` (abaixo), `result` (o que a consulta achou: `ptr`, `no_ptr`,
`servfail`, `refused`, `timeout`, ou `null` sem consulta) e `checked_at`
(quando foi resolvido, ou `null`).

| `live.status` | Quando | Corpo |
|---|---|---|
| `resolved` | o coletor respondeu em até `LIVE_TIMEOUT` (`5s`) | o IP relido do Postgres depois da gravação (uma falha não apaga um PTR: `status` pode seguir `ptr` com `live.result` `timeout`) |
| `recent` | este IP foi resolvido há menos de `LIVE_MIN_AGE` (`60s`) | o daquela resolução, sem consulta nova |
| `timeout` | `LIVE_TIMEOUT` sem resposta | o dado frio |
| `unavailable` | Valkey desligado (`REDIS_URL` vazia) ou fora (erro em qualquer passo; log `Valkey indisponível no tempo real`) | o dado frio |
| `not_allowed` | o prefixo do IP está inativo ou fora da regra (IPv4 de /22 a /24), ou o coletor respondeu `not_allowed` (o prefixo mudou no meio) | o dado frio; nada é pedido (no primeiro caso) |

Passos, depois da validação e da leitura fria — do cache ou do banco, como
sem `live` (404 e `503 dataset_not_ready` valem igual):

1. Regra e Valkey (acima). Marca `badblock:api-rdns:live:recent:<ip>`
   presente → `recent`.
2. **Coalescência**: um `singleflight` por IP na instância; o líder faz `SET
   badblock:rdns:live:inflight:<ip> 1 NX EX 30`. Conseguiu: se `LLEN
   badblock:rdns:live:queue` ≥ `LIVE_QUEUE_MAX` (`1000`), apaga a marca e
   responde **503 `live_queue_full`**; senão `RPUSH` do pedido. Não conseguiu:
   o pedido já está na fila — só espera.
3. `BLPOP badblock:rdns:live:reply:<ip>` por até `LIVE_TIMEOUT`.
4. Resposta: relê o IP no Postgres, **regrava a chave fria**
   `badblock:api-rdns:ip:<ip>` com o corpo novo, grava a marca `recent`
   (`{"result":…,"checked_at":…}`, `EX LIVE_MIN_AGE`) e responde `resolved`.
   Uma resposta que chega depois do prazo fica na lista de retorno até 30 s e
   serve o próximo pedido desse IP.

- No máximo **128** esperas simultâneas por instância; acima, 503
  `live_queue_full`. O tempo real usa um segundo cliente Valkey (pool de 128,
  1 s de prazo fora do `BLPOP`), fora do disjuntor do cache.
- Cabeçalhos: `Cache-Control: no-store`, `X-Cache: LIVE`, **sem `ETag`**
  (`If-None-Match` é ignorado). `HEAD` com `live=true` também resolve.
- Mensagem do 503: `fila de tempo real cheia; tente sem live=true ou mais
  tarde`.

## Cache e ETag

`serveCached` com a [regra desta API](#desvios-do-padrão): sem prefixos →
503 `dataset_not_ready`; procura a chave; `MISS` → consulta uma vez por
chave (singleflight, `DB_TIMEOUT`) e grava se 200 e até 8 MiB; ETag do corpo;
`If-None-Match` igual → 304, com `ETag`, `Cache-Control` e o `X-Cache` de
onde veio o corpo comparado (`HIT`, `MISS` ou `BYPASS`). Fail-open, disjuntor, `REDIS_TIMEOUT` e
`REDIS_CACHE_ENABLED` como no padrão — `REDIS_CACHE_ENABLED=false` desliga só
o cache; o tempo real depende só da `REDIS_URL`. O Valkey usa
`volatile-lru`, e toda chave desta API tem TTL
([../../plataforma/valkey.md](../../plataforma/valkey.md)).

## `/rdns/meta`

Sem cache, lida direto do banco:

| Bloco | Conteúdo |
|---|---|
| `app`, `version` | do padrão |
| `scan` | `prefixes`, `active`, `static`, `out_of_rule` (fora da regra: nunca varridos), `addresses` (endereços dos ativos na regra), `never_scanned`, `overdue` (`last_scan_at` mais velho que `scan_interval`), `rescan`, `scanning` (com `scan_worker`), `oldest_scan_at`, e as somas `ptr`, `no_ptr`, `failed` da última varredura de cada prefixo. `addresses`, `never_scanned`, `overdue`, `rescan` e `oldest_scan_at` contam só os prefixos **ativos na regra** (os que a fila pega); os demais, a tabela inteira |
| `config` | `rate_limit` e `scan_interval` de `rdns_config` (`null` = sem a linha ou com valor inválido: cada coletor usa a reserva do ambiente); `overdue` usa `scan_interval` ou, sem ele, `24h` |
| `derivations` | a última `rdns_run` aplicada de cada origem: `source`, `version` (`uuid`), `source_version`, `source_updated_at`, `created_at`, `blocks`, `units`, `inserted`, `asn_updated`, `covered`, `not_in_source`; `[]` antes da primeira |
| `workers` | instâncias com sinal nas últimas 24 h: `host`, `version`, `started_at`, `seen_at`, `alive` (sinal há menos de 90 s), `rate_limit`, `queries`, `live_queries`, `prefixes` — sem os servidores DNS (são da rede do operador) |
| `collector` | a linha de `jobs`, como no padrão |
| `live` | `queue` (`LLEN` da fila; `null` sem Valkey) |

## Índice e saúde

- Índice: os campos do padrão, `source` e `derived_from` (acima).
- `/status`: `checks.dataset` é `ok` com algum prefixo e `empty` sem nenhum
  (`starting`, `aguardando a primeira derivação do collector-rdns`); Valkey
  fora é `degraded` com `Valkey indisponível; respondendo sem cache e sem
  tempo real`.
- `503 dataset_not_ready`: `nenhum prefixo cadastrado ainda: a primeira
  derivação do collector-rdns não terminou; tente em alguns minutos`.

## Manifesto

`openapi/openapi.yaml` no [padrão](../../padroes/openapi.md), derivado
deste arquivo:

- `info.title` `api-rdns`; a `description` explica o que é a varredura, o
  bloco `scan`, a data por prefixo, o cache sem versão (até 1 h de atraso), o
  `live=true` (estados, prazo, rate limit próprio) e o que não existe (rota por
  ASN, `X-Dataset-Version`).
- `servers`: os quatro do padrão, com a porta 8113.
- `components.parameters`: `IP` (`format: ipv4`), `PrefixIP` (`format:
  ipv4`), `PrefixLength` (inteiro de 24 a 31), `Live` (`enum: ['true',
  'false']`), `IfNoneMatch`.
- `components.headers`: `ETag`, `CacheControlPublic`, `CacheControlNoStore` e
  `XCache` (`enum: [HIT, MISS, BYPASS, LIVE]`); sem `XDatasetVersion`.
- `components.responses`: as do padrão, mais `LiveQueueFull` (503).
- `components.schemas`: `Scan`, `Live`, `IPResponse`, `PrefixItem`,
  `PrefixCounts`, `PrefixResponse`, `MetaResponse` (com `MetaScan`,
  `MetaConfig`, `MetaDerivation`, `MetaWorker`, `MetaCollector`), `Status`,
  `IndexResponse` e `Error` (`code` com os seis do padrão e
  `live_queue_full`). `status` dos itens com `enum` dos seis estados;
  `live.status` com os cinco.

## Opções

As [comuns](../../padroes/api.md#configuração-comum) (`BASE_PATH` = `/rdns`;
`DATASET_POLL` relê se há prefixos), mais:

| Variável | Argumento | Padrão | Uso |
|---|---|---|---|
| `LIVE_TIMEOUT` | `--live-timeout` | `5s` | espera da resposta do coletor (de 1 s a 25 s: abaixo dos 30 s da marca `inflight`) |
| `LIVE_MIN_AGE` | `--live-min-age` | `60s` | idade mínima para resolver de novo o mesmo IP |
| `LIVE_QUEUE_MAX` | `--live-queue-max` | `1000` | tamanho da fila acima do qual o pedido é recusado (≥ 1) |

Inválidas: `--live-timeout inválido (1s a 25s): "<v>"`, `--live-min-age
inválido: "<v>"`, `--live-queue-max inválido: "<v>"`.

## Compose, `.env` e Traefik

O compose é o [modelo](../../plataforma/docker.md#api) com a porta 8113 e as
variáveis `API_RDNS_*` do padrão (`TAG`, `HOST_PORT`, `CACHE_TTL`,
`CACHE_ENABLED`, `DB_POOL_MAX`, `REDIS_URL`); as opções `LIVE_*` não passam
por ele. As labels são as de
[../../plataforma/publicacao.md](../../plataforma/publicacao.md#traefik), mais
o router do tempo real:

```yaml
      traefik.http.routers.badblock-api-rdns-live.rule: Host(`${API_FQDN:-api.badblock.net.br}`) && (Path(`/rdns`) || PathPrefix(`/rdns/`)) && Query(`live`, `true`)
      traefik.http.routers.badblock-api-rdns-live.entrypoints: ${TRAEFIK_ENTRYPOINT:-websecure}
      traefik.http.routers.badblock-api-rdns-live.tls: "true"
      traefik.http.routers.badblock-api-rdns-live.tls.certresolver: ${TRAEFIK_CERTRESOLVER:-letsencrypt}
      traefik.http.routers.badblock-api-rdns-live.service: badblock-api-rdns
      traefik.http.routers.badblock-api-rdns-live.middlewares: badblock-api-rdns-live-ratelimit

      traefik.http.middlewares.badblock-api-rdns-live-ratelimit.ratelimit.average: ${API_RDNS_LIVE_RATE_AVERAGE:-1}
      traefik.http.middlewares.badblock-api-rdns-live-ratelimit.ratelimit.burst: ${API_RDNS_LIVE_RATE_BURST:-5}
      traefik.http.middlewares.badblock-api-rdns-live-ratelimit.ratelimit.period: 1s
```

A regra mais longa tem prioridade no Traefik v3 (sem `priority`, ela vale o
tamanho da regra): com `live=true`, o pedido cai neste router (1 req/s por IP,
rajada 5); sem, no router comum.

## Operação

```bash
curl http://127.0.0.1:8113/rdns/ip/200.160.2.3
curl 'http://127.0.0.1:8113/rdns/ip/200.160.2.3?live=true'
make -C apps/rdns/api smoke     # /status, /meta, /ip/$(SMOKE_IP) e /openapi.yaml
```

`Makefile`: `SOURCE := rdns`, `PORT := $${API_RDNS_HOST_PORT:-8113}`,
`SMOKE_IP ?= 200.160.2.3`. Logs do padrão, mais `tempo real` (`debug`: `ip`,
`live_status`, `result`, `ms`), `fila de tempo real cheia` (`warn`: `ip`) e
`Valkey indisponível no tempo real` (`warn`: `ip`, `err`); no lugar de
`versão do dataset` e `não consegui ler a versão do dataset`, `prefixos
cadastrados` (`info`: `ready`, quando muda) e `não consegui conferir se há
prefixos` (`warn`: `err`). A linha `iniciando` traz também `live` (`true` com
`REDIS_URL`).

## Medições

Stack local (2026-10-10, um Mac de desenvolvimento com Docker Desktop;
308.529 prefixos derivados do arquivo do NIC.br do dia, 8 deles varridos;
`curl` pela porta do loopback):

| Rota | Tamanho | `MISS` | `HIT` |
|---|---|---|---|
| `/rdns/ip/200.160.2.7` | 291 B | 7,8 ms | 2,6 ms |
| `/rdns/prefix/200.160.3.0/24` (nunca varrido) | 17.773 B | 5,6 ms | 2,5 ms |
| `/rdns/prefix/200.160.2.0/24` (varrido) | 19.687 B | — | — |
| `/rdns/meta` (sem cache, sobre os 308.529 prefixos) | 969 B | 38–51 ms | — |
| `/rdns/ip/<ip>?live=true`, `resolved` (coletor a 20 consultas/s, `1.1.1.1`) | ~400 B | 34–200 ms | — |

**Pendente**: medir de novo no servidor, com a primeira volta completa
(`rdns_ptr` com dezenas de milhões de linhas).

## Testes específicos

Unitários (`make test`, store e Valkey falsos):

| Pacote | Confere |
|---|---|
| `httpapi` | `/ip`: `ptr`, `no_ptr` (sem linha, prefixo varrido), `not_scanned`, falha, prefixo inativo, 404 fora, 400 em IPv6, texto e `live` mal escrito ou repetido; `/prefix`: /24 com 256 itens e `counts`, /31, `len` 23 e 32 → 400, `live` → 400, 404 fora; ETag do corpo e 304; chave sem versão, igual com e sem `/v1`; `503 dataset_not_ready` sem prefixos |
| `live` | coalescência (dois pedidos, um `RPUSH`, uma releitura), quem achou a marca `inflight` só espera, 503 com a fila cheia (e a marca apagada) e acima de 128 esperas, `timeout`, `recent`, `unavailable` (Valkey fora), erro da releitura |
| `httpapi` (`live_test.go`) | pela rota, com o `live` e um Valkey falso: `resolved` (relê o banco e regrava a chave fria), falha sobre PTR (`status` `ptr`, `live.result` `timeout`), `recent`, `timeout`, `unavailable` (sem `REDIS_URL` e com o Valkey fora), `not_allowed` (inativo, fora da regra, resposta do coletor), 503 `live_queue_full`, `HEAD`, `Cache-Control: no-store`, `X-Cache: LIVE` e sem `ETag` |
| `openapi_test.go` | o do [padrão](../../padroes/openapi.md#teste) |

Integração (`make test-int`, em `internal/httpapi`): PG18 com as migrations
de `central/` e `rdns/` e Valkey (container genérico): cada consulta de
[dados.md](dados.md#consultas-da-api-rdns) com uso de índice (GiST e PK; o
plano sai com `enable_seqscan = off`, para valer com a tabela pequena do
teste), e o tempo real com um coletor falso que responde na lista de
retorno (`resolved`, a chave fria regravada, a marca `recent` com TTL; sem
coletor, `timeout` e o pedido fica na fila, sem TTL).
