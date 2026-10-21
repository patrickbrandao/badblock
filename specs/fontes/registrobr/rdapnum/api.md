# API `api-registrobr-rdapnum`

O que a `api-registrobr-rdapnum` faz de diferente ou a mais que o
[padrão das APIs](../../../padroes/api.md) e o
[padrão do manifesto](../../../padroes/openapi.md); o resto segue os
padrões. Cada rota, com parâmetros, campos e exemplos:
[api-rotas.md](api-rotas.md). Tabelas e consultas:
[dados-processados.md](dados-processados.md); a fila de atualização:
[dados.md](dados.md#fila-de-atualização-valkey). Dono: sub-agente
`api-registrobr-rdapnum`.

## Valores desta fonte

| Item | Valor |
|---|---|
| App | `api-registrobr-rdapnum` |
| Caminho de base | `BASE_PATH` = `/registrobr/rdapnum`; em produção, `https://api.badblock.net.br/registrobr/rdapnum/` |
| Porta no loopback | 8114 (`API_REGISTROBR_RDAPNUM_HOST_PORT`) |
| Tabelas lidas | `_target`, `_response`, `_autnum`, `_network`, `_entity`, `_link`, `_reverse`; no `/meta`, também `_run`, `_worker`, `_config` e a linha `collector-registrobr-rdapnum` de `jobs` (todas com o prefixo `registrobr_rdapnum`) |
| Escreve | **nada** no Postgres; no Valkey, o cache e a fila de atualização |
| Pacotes | os do padrão, com `internal/dataset` reduzido a "há alvos?", e `internal/refresh` (o pedido de atualização) |
| Nomes nas conexões | `application_name=api-registrobr-rdapnum`; `ClientName` `badblock-api-registrobr-rdapnum` |
| Fonte citada no índice | `source`: `https://github.com/patrickbrandao/badblock/tree/main/specs/fontes/registrobr/rdapnum`; `upstream`: `https://rdap.registro.br/`; `derived_from`: `["cgibr"]` |
| Descrição da imagem | `API HTTP do RDAP do registro.br por ASN e bloco, coletado pelo BadBlock` |

## Desvios do padrão

O dado muda um alvo de cada vez, o dia inteiro, como no `rdns`
([../../rdns/api.md](../../rdns/api.md#desvios-do-padrão)); por isso
(decisão #42):

| Padrão | Nesta API |
|---|---|
| versão do dataset na chave de cache e no ETag | chave **sem versão**, TTL de `REDIS_KEY_TTL` = **300 s** (o dado pode ter até 5 min de atraso em relação ao banco) |
| ETag de `versão + chave`, 304 sem consultar | ETag = `W/"<FNV-1a de 64 bits do corpo>"`; o 304 precisa do corpo (do cache ou do banco) |
| bloco `dataset` em toda rota de dados | bloco **`scan`**: o alvo que gerou o dado e o estado da coleta dele ([api-rotas.md](api-rotas.md#bloco-scan)) |
| cabeçalho `X-Dataset-Version` | não existe |
| `503 dataset_not_ready` antes da primeira carga | antes de haver **qualquer** alvo em `_target` |
| só `GET` nas rotas de dados | `POST .../refresh` (atualização); responde **202** |
| a API não escreve nada | escreve na fila de atualização do Valkey |
| códigos de erro do padrão | também `409 target_inactive` e `503 refresh_unavailable` |
| um router no Traefik | um segundo router para o `POST .../refresh`, com rate limit próprio |

A API é **pública e sem chave, com contatos e e-mails** — decisão do
mantenedor, registrada com o aviso do NIC.br de que os dados não podem ser
redistribuídos (#42 em [../../../projeto/decisoes.md](../../../projeto/decisoes.md)).
O que ela nunca mostra: o IP de saída do coletor (a coluna `egress_ip` e a
linha do aviso dentro do corpo cru — [api-rotas.md](api-rotas.md#resposta-crua)),
a `PROXY_URL` e os contadores de IP do proxy fora do `/meta`.

## Rotas

Todas abaixo de `/registrobr/rdapnum`; as de dados, o índice e `/meta`
também em `/registrobr/rdapnum/v1/...`. Chave de cache com o prefixo
`badblock:api-registrobr-rdapnum:`.

| Rota | `operationId` | Tag | Chave de cache |
|---|---|---|---|
| `GET /asn/{asn}` | `getASN` | `dados` | `asn:61610` |
| `GET /asn/{asn}/raw` | `getASNRaw` | `cru` | `asn-raw:61610` |
| `GET /asn/{asn}/history` | `getASNHistory` | `cru` | `asn-history:61610` |
| `POST /asn/{asn}/refresh` | `refreshASN` | `atualização` | — |
| `GET /ip/{ip}` | `getIP` | `dados` | `ip:200.225.48.10` |
| `GET /prefix/{ip}/{len}` | `getPrefix` | `dados` | `prefix:200.225.48.0/24` |
| `GET /prefix/{ip}/{len}/raw` | `getPrefixRaw` | `cru` | `prefix-raw:200.225.48.0/24` |
| `GET /prefix/{ip}/{len}/history` | `getPrefixHistory` | `cru` | `prefix-history:200.225.48.0/24` |
| `POST /prefix/{ip}/{len}/refresh` | `refreshPrefix` | `atualização` | — |
| `GET /document/{document}` | `getDocument` | `dados` | `document:35980592000130` |
| `GET /contact/{handle}` | `getContact` | `dados` | `contact:ROBEL308` |
| `GET /response/{uuid}` | `getResponse` | `cru` | `response:<uuid>` |
| `GET /meta` | `getMeta` | `meta` | — (sem cache) |
| `GET /` | `getIndex` | `meta` | — |
| `GET`, `POST /health` e `/status` | `getHealth`, `postHealth`, `getStatus`, `postStatus` | `saúde` | — |
| `GET /ping` / `GET /openapi.yaml` | `ping` / `getOpenAPI` | `saúde` / `meta` | — |

Não há lista de todos os ASNs (é a `/cgibr/asns`) nem busca por nome.

## Atualização (`POST .../refresh`)

1. Validação como no `GET` da mesma rota; corpo do pedido ignorado.
2. Alvo: `asn` → o alvo do ASN; `prefix` → o alvo com esse bloco exato, ou,
   sem ele, o alvo que gerou o bloco registrado que contém o pedido
   (`_network.target_uuid`). Nenhum → 404. Inativo → 409 `target_inactive`.
3. `REDIS_URL` vazia ou Valkey fora → 503 `refresh_unavailable`.
4. Coletado há menos de `REFRESH_MIN_AGE` (`10m`, por `last_scan_at`) → 202
   com `queued: false`, `reason: "recent"`.
5. `SET badblock:registrobr-rdapnum:refresh:inflight:<kind>:<alvo> 1 NX EX
   60`: não conseguiu → 202 `queued: false`, `reason: "already_queued"`.
   Conseguiu: `LLEN` da fila ≥ `REFRESH_QUEUE_MAX` (`1000`) → apaga a marca
   e 503 `refresh_unavailable` (`fila de atualização cheia; tente mais
   tarde`); senão `RPUSH` do pedido e 202 `queued: true`.

Corpo do 202 e mensagens: [api-rotas.md](api-rotas.md#atualização). O
coletor marca `rescan` e coleta na taxa normal, sem prazo garantido: o
resultado aparece no `scan.checked_at` da rota de dados. `Cache-Control:
no-store`, sem `ETag`, `X-Cache: BYPASS`.

## Cache e ETag

`serveCached` como no [`rdns`](../../rdns/api.md#cache-e-etag): sem alvos →
503 `dataset_not_ready`; chave sem versão; `MISS` consulta uma vez por
chave e grava se 200 e até 8 MiB; ETag do corpo; `If-None-Match` igual →
304. Fail-open e disjuntor do padrão. O `POST` de atualização **não**
apaga o cache: o dado novo aparece quando a chave vence (até 300 s depois
da coleta).

## `/registrobr/rdapnum/meta`

Sem cache, lida direto do banco:

| Bloco | Conteúdo |
|---|---|
| `app`, `version` | do padrão |
| `targets` | `total`, `asn`, `prefix`, `active`, `static`, `never_scanned`, `overdue` (`last_scan_at` mais velho que `scan_interval`), `rescan`, `scanning`, `waiting` (`retry_at` no futuro), `oldest_scan_at`, e por `last_result`: `complete`, `truncated`, `not_found`, `error` |
| `responses` | `total`, `complete`, `truncated`, `not_found`, `pending` (conclusivas atuais sem processar), `process_errors` |
| `data` | linhas de `_autnum`, `_network`, `_entity`, `_reverse` |
| `config` | `rate_limit` (por minuto) e `scan_interval` de `_config` (`null` sem linha válida); `overdue` usa `scan_interval` ou `168h` |
| `derivations` | a última `_run` aplicada: `source`, `version`, `source_version`, `source_updated_at`, `created_at`, `asns`, `prefixes`, `inserted`, `not_in_source`; `[]` antes da primeira |
| `workers` | instâncias com sinal nas últimas 24 h: `host`, `version`, `started_at`, `seen_at`, `alive` (< 90 s), `rate_limit`, `proxy` (booleano), `requests`, `complete`, `truncated`, `not_found`, `errors`, `refreshes`, `egress_ips` (distintos nas últimas 100), `paused_until` — **sem** o IP de saída |
| `collector` | a linha de `jobs`, como no padrão |
| `refresh` | `queue` (`LLEN`; `null` sem Valkey) |

## Índice e saúde

- Índice: os campos do padrão, `source`, `upstream` e `derived_from`.
- `/status`: `checks.dataset` `ok` com algum alvo, `empty` sem nenhum
  (`starting`, `aguardando a primeira derivação do
  collector-registrobr-rdapnum`); Valkey fora é `degraded` com `Valkey
  indisponível; respondendo sem cache e sem atualização`.
- `503 dataset_not_ready`: `nenhum alvo cadastrado ainda: a primeira
  derivação do collector-registrobr-rdapnum não terminou; tente em alguns
  minutos`.

## Manifesto

`openapi/openapi.yaml` no [padrão](../../../padroes/openapi.md), derivado
deste arquivo e de [api-rotas.md](api-rotas.md):

- `info.title` `api-registrobr-rdapnum`; a `description` explica a coleta
  (alvos do `cgibr`, 7 dias, truncagem e por que o dado nunca é truncado na
  API), o bloco `scan`, o cache sem versão (até 5 min), a atualização (202,
  sem prazo) e o aviso de uso do NIC.br.
- `servers`: os quatro do padrão, com a porta 8114.
- `components.parameters`: `ASN` (inteiro de 0 a 4294967295), `IP`
  (`format: ip`), `PrefixIP`, `PrefixLength` (0 a 128), `Document`,
  `Handle`, `ResponseUUID` (`format: uuid`), `IfNoneMatch`.
- `components.schemas`: `Scan`, `Entity`, `Contact`, `ASNResponse`,
  `NetworkResponse`, `Reverse`, `Nameserver`, `DocumentResponse`,
  `ContactResponse`, `HistoryResponse`, `RawResponse` (objeto livre),
  `RefreshResponse`, `MetaResponse` e os do padrão; `Error` com os códigos
  do padrão mais `target_inactive` e `refresh_unavailable`.
- `components.responses`: as do padrão, mais `Accepted` (202), `Conflict`
  (409) e `RefreshUnavailable` (503).

## Opções

As [comuns](../../../padroes/api.md#configuração-comum) (`BASE_PATH` =
`/registrobr/rdapnum`; `REDIS_KEY_TTL` padrão **300**; `DATASET_POLL` relê se
há alvos), mais:

| Variável | Argumento | Padrão | Uso |
|---|---|---|---|
| `REFRESH_MIN_AGE` | `--refresh-min-age` | `10m` | coleta mais nova que isso não é pedida de novo |
| `REFRESH_QUEUE_MAX` | `--refresh-queue-max` | `1000` | tamanho da fila acima do qual o pedido é recusado (≥ 1) |

Inválidas: `--refresh-min-age inválido: "<v>"`, `--refresh-queue-max
inválido: "<v>"`.

## Compose, `.env` e Traefik

O compose é o [modelo](../../../plataforma/docker.md#api) com a porta 8114 e
as variáveis `API_REGISTROBR_RDAPNUM_*` do padrão (`TAG`, `HOST_PORT`,
`CACHE_TTL` com padrão `300`, `CACHE_ENABLED`, `DB_POOL_MAX`, `REDIS_URL`).
As labels são as de
[../../../plataforma/publicacao.md](../../../plataforma/publicacao.md#traefik),
mais o router da atualização:

```yaml
      traefik.http.routers.badblock-api-registrobr-rdapnum-refresh.rule: Host(`${API_FQDN:-api.badblock.net.br}`) && Method(`POST`) && PathRegexp(`^/registrobr/rdapnum/(v1/)?(asn|prefix)/.+/refresh$`)
      traefik.http.routers.badblock-api-registrobr-rdapnum-refresh.entrypoints: ${TRAEFIK_ENTRYPOINT:-websecure}
      traefik.http.routers.badblock-api-registrobr-rdapnum-refresh.tls: "true"
      traefik.http.routers.badblock-api-registrobr-rdapnum-refresh.tls.certresolver: ${TRAEFIK_CERTRESOLVER:-letsencrypt}
      traefik.http.routers.badblock-api-registrobr-rdapnum-refresh.service: badblock-api-registrobr-rdapnum
      traefik.http.routers.badblock-api-registrobr-rdapnum-refresh.middlewares: badblock-api-registrobr-rdapnum-refresh-ratelimit

      traefik.http.middlewares.badblock-api-registrobr-rdapnum-refresh-ratelimit.ratelimit.average: ${API_REGISTROBR_RDAPNUM_REFRESH_RATE_AVERAGE:-6}
      traefik.http.middlewares.badblock-api-registrobr-rdapnum-refresh-ratelimit.ratelimit.burst: ${API_REGISTROBR_RDAPNUM_REFRESH_RATE_BURST:-3}
      traefik.http.middlewares.badblock-api-registrobr-rdapnum-refresh-ratelimit.ratelimit.period: 1m
```

A regra mais longa vence no Traefik v3: o `POST` de atualização cai neste
router (6 por minuto por IP, rajada 3); o resto, no router comum.

## Operação

```bash
curl http://127.0.0.1:8114/registrobr/rdapnum/asn/61610
curl -X POST http://127.0.0.1:8114/registrobr/rdapnum/asn/61610/refresh
make -C apps/registrobr/rdapnum/api smoke   # /status, /meta, /asn/$(SMOKE_ASN) e /openapi.yaml
```

`Makefile`: `SOURCE := registrobr/rdapnum`,
`PORT := $${API_REGISTROBR_RDAPNUM_HOST_PORT:-8114}`, `SMOKE_ASN ?= 22548`.
Logs do padrão, mais `atualização pedida` (`info`: `kind`, `target`,
`queued`, `reason`) e `Valkey indisponível na atualização` (`warn`: `err`);
no lugar dos logs de versão do dataset, `alvos cadastrados` (`info`:
`ready`) e `não consegui conferir se há alvos` (`warn`: `err`).

## Medições

**Pendente**: medir no stack local com a primeira volta do coletor (tamanho
e tempo de `/asn`, `/prefix`, `/document` de um CNPJ com muitos ASNs, e o
`/meta` sobre os ~31 mil alvos).

## Testes específicos

Unitários (`make test`, store e Valkey falsos):

| Pacote | Confere |
|---|---|
| `httpapi` | cada rota de [api-rotas.md](api-rotas.md) com as validações e os 404; `scan` com cada `last_result`; contatos agrupados por handle com os papéis; `via`; `/raw` sem a linha do IP no aviso; `/history` sem `egress_ip`; ETag do corpo e 304; chave igual com e sem `/v1`; `503 dataset_not_ready` sem alvos |
| `refresh` | `queued: true` com `RPUSH`; `recent`; `already_queued` (marca presente); 404 sem alvo; 409 inativo; 503 sem Valkey e com a fila cheia (marca apagada); bloco sem alvo exato cai no alvo do bloco que o contém |
| `openapi_test.go` | o do [padrão](../../../padroes/openapi.md#teste) |

Integração (`make test-int`): PG18 com as migrations de `central/` e
`registrobr_rdapnum/`, as tabelas carregadas pelas fixtures processadas, e
Valkey: cada consulta de
[dados-processados.md](dados-processados.md#consultas-da-api-registrobr-rdapnum)
com uso de índice (`enable_seqscan = off`), e o `POST` pondo o pedido na
fila com a marca `inflight` de 60 s.
