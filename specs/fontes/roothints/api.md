# roothints — API (`api-roothints`)

O que a `api-roothints` faz de diferente ou a mais que o
[padrão das APIs](../../padroes/api.md) e o
[padrão do manifesto](../../padroes/openapi.md). O resto — `main.go`, caminho
de base e versões, formato, versão do dataset, `serveCached`, ETag,
cabeçalhos, saúde, erros, IP real, configuração comum, container — segue os
padrões. Cada rota, com validações, campos, exemplos reais e mensagens de
erro: [api-rotas.md](api-rotas.md). Tabelas e o SQL das consultas:
[dados.md](dados.md); de onde vêm os campos: [fonte.md](fonte.md). Dono:
sub-agente `api-roothints`.

## Valores desta fonte

| Item | Valor |
|---|---|
| App, binário | `api-roothints` (`/api-roothints` na imagem) |
| Module Go | `github.com/patrickbrandao/badblock/apps/roothints/api` |
| Imagem, container | `tmsoftbrasil/badblock-api-roothints`, `badblock-api-roothints` |
| Descrição da imagem (`org.opencontainers.image.description`) | `API HTTP dos servidores raiz do DNS (named.root da InterNIC, root hints), do BadBlock` |
| Caminho de base (`BASE_PATH`) | `/roothints` — `https://api.badblock.net.br/roothints/` |
| Porta local | 8109 (`API_ROOTHINTS_HOST_PORT`, só no loopback) |
| Lê | `roothints_server`, `roothints_run` e a linha `collector-roothints` de `jobs` ([dados.md](dados.md#consultas-da-api)) |
| `source` do índice | `https://www.internic.net/domain/named.root` (constante `SourceURL`; não vem do banco) |
| Chave no Valkey | `badblock:api-roothints:<versão>:<consulta>` ([abaixo](#cache-e-etag)) |
| Cliente do Valkey | `ClientName` = `badblock-api-roothints` |
| Cabeçalho `Server` | `badblock-api-roothints/<versão>` |
| Pool pgx | `application_name = api-roothints` (se a `POSTGRES_URL` não traz outro); sem `plan_cache_mode` (consultas por igualdade em 13 linhas) |
| Pacote a mais | `internal/testdb` ([testes](#testes)) |
| Arquivo real nos testes | a fixture do coletor (`apps/roothints/collector/testdata/named.root`, o arquivo inteiro de 2026-09-30) em todo `make test-int`; outro arquivo com `make test-real FILE=...` |
| Exemplo das specs | o servidor `a` (`/roothints/server/a`) |

Pacotes: os do padrão (sem `internal/rir`) mais `internal/testdb`. Em
`internal/httpapi`: `server.go` (`Handler`, middleware, `serveCached`,
`etagFor`, erros), `handlers.go` (a lista `dataRoutes`, os handlers,
`normalizeServer`) e `types.go` (os tipos do JSON). `internal/store` tem as
consultas de [dados.md](dados.md#consultas-da-api).

O `--help` começa assim (depois vêm o uso — `api-roothints [opções]` e
`api-roothints healthcheck` — e as opções do padrão, em ordem alfabética,
cada uma com a variável e o padrão):

```
api-roothints — API HTTP dos servidores raiz do DNS (named.root da InterNIC,
root hints).

Lê as tabelas roothints_* mantidas pelo collector-roothints, com cache opcional
no Valkey. Responde tudo abaixo de BASE_PATH (/roothints): /roothints/servers é
a versão atual e /roothints/v1/servers fixa a v1.
```

## Rotas

| Rota | `operationId` | Tag | Chave de cache (exemplo) |
|---|---|---|---|
| `GET /roothints/servers` | `listServers` | `dados` | `servers` |
| `GET /roothints/server/{server}` | `getServer` | `dados` | `server:a` |
| `GET /roothints/meta` | `getMeta` | `meta` | — (sem cache) |
| `GET /roothints/` | `getIndex` | `meta` | — |
| `GET`, `POST /roothints/health` | `getHealth`, `postHealth` | `saúde` | — |
| `GET`, `POST /roothints/status` | `getStatus`, `postStatus` | `saúde` | — |
| `GET /roothints/ping` | `ping` | `saúde` | — |
| `GET /roothints/openapi.yaml` | `getOpenAPI` | `meta` | — |

As de dados, o índice e `/meta` respondem também em `/roothints/v1/...`;
saúde, `ping` e o manifesto existem só sem versão. Detalhes de cada uma:
[api-rotas.md](api-rotas.md).

### Rota nova

As rotas de dados ficam numa lista só, `dataRoutes()` em
`internal/httpapi/handlers.go` (caminho relativo ao caminho de base e
handler). Cada item é registrado com e sem `/v1` e entra no índice
(`endpoints`) na ordem da lista; o `openapi_test.go` falha se a rota faltar
no manifesto. Uma rota nova é: a seção em [api-rotas.md](api-rotas.md) com o
exemplo real, o path no manifesto, o item em `dataRoutes`, o handler (validar
e normalizar → `serveCached(chave, build)`), a consulta em `internal/store`
(com o teste de integração) e os testes de `httpapi`. Uma consulta que as
tabelas não servem vira pedido ao coletor ([dados.md](dados.md#mudar-o-schema)).

## Dados servidos

- Um servidor por linha de `roothints_server`: hoje os 13 (`a` a `m`), com os
  dois endereços, os três TTLs (todos `3600000`) e a nota do bloco
  ([fonte.md](fonte.md#fatos-medidos)).
- Endereços **sem máscara**: as colunas são `inet` `/32` e `/128`; o store
  escaneia em `netip.Prefix` e devolve `.Addr()`, e o JSON usa
  `netip.Addr.String()` (IPv6 na forma curta, minúsculas).
- `ipv4`/`ipv6` (com o TTL) e `note` saem `null` quando o arquivo não os
  traz — nunca `""` nem `0`.
- `note` é texto livre da fonte, como publicado (em maiúsculas).
- O bloco `source` (`last_update`, `zone_serial`) é o cabeçalho do arquivo
  **da versão da resposta**: `dataset.Watcher` o lê na mesma consulta que a
  versão (a linha aplicada de `roothints_run`) e o guarda no `Snapshot`, então
  o bloco, a chave de cache e o ETag andam juntos, sem consulta a mais. É o
  único acréscimo ao `internal/dataset` do padrão.
- `first_seen` e `updated_at` (só em `/server/{server}`) são o `created_at` e
  o `updated_at` de `roothints_server`: datas deste banco, não da fonte.

## Cache e ETag

Toda rota de dados passa pelo `serveCached` do
[padrão](../../padroes/api.md#cache-valkey-cache-aside-e-servecached). A chave
é a consulta **normalizada**, e a consulta ao banco usa o mesmo valor: a
chave determina a resposta.

| Pedido | Chave | Normalização |
|---|---|---|
| `/servers` | `servers` | — |
| `/server/K`, `/server/k.root-servers.net`, `/server/K.ROOT-SERVERS.NET.` | `server:k` | só ASCII; minúsculas; sem o ponto final; o nome reduzido à letra |

- Com a versão dos exemplos (`01a0f06d-08c2-7f4a-924a-b345129d0f5a`),
  `/roothints/servers` (e `/roothints/v1/servers`) usa a chave
  `badblock:api-roothints:01a0f06d-08c2-7f4a-924a-b345129d0f5a:servers` e o
  ETag `W/"fbf484ee604cba0"` (`etagFor`; o hex sai sem zeros à esquerda).
  Outros: `server:a` → `W/"c618b310d98767be"`, `server:b` →
  `W/"c618b210d987660b"`, `server:c` → `W/"c618b110d9876458"`, `server:k` →
  `W/"c618a910d98756c0"`, `server:m` → `W/"c618af10d98760f2"`
  (`TestCacheAndETag` confere quatro deles).
- O coletor grava uma versão nova a cada aplicação (inclusive um `--force`
  sem mudança), e com ela chaves e ETags novos; como o conteúdo do
  `named.root` muda poucas vezes por ano, na prática a versão só muda quando
  o arquivo muda ([collector.md](collector.md)).
- 404 e erros não entram no cache; o 304 sai com `X-Cache: HIT`.
- Limite do corpo guardado: 8 MiB (`maxCachedBody = 8 << 20`); a maior
  resposta, `/servers`, tem 2.462 bytes.

## Consultas

O SQL e os índices são os de [dados.md](dados.md#consultas-da-api) (dono: o
coletor). O que é da API:

- `/servers`: `... FROM roothints_server ORDER BY letter` — 13 linhas numa
  página, *seq scan* e ordenação em memória; nenhum índice a criar.
- `/server/{server}`: sempre `WHERE letter = $1`, com a letra normalizada.
  A consulta pelo nome (`WHERE name = $1`) que [dados.md](dados.md#consultas-da-api)
  lista não é usada: as constraints `chk_roothints_server_name` e
  `chk_roothints_server_letter` fazem de `name` uma função de `letter`, então
  o nome pedido é reduzido à letra antes (uma chave de cache e uma consulta
  para as duas formas).
- Com 13 linhas o planejador prefere ler a página inteira; os testes
  conferem, com `enable_seqscan = off`, que `uq_roothints_server_letter` e
  `uq_roothints_server_name` servem às consultas por letra e por nome.
- Versão e `/meta`: `... FROM roothints_run WHERE status = 1 ORDER BY
  created_at DESC LIMIT 1` (`ix_roothints_run_applied`), com `COALESCE` em
  `md5`, `sha256` e `servers` para que uma linha fora do padrão não trave a
  leitura da versão.

## Configuração

Só as opções comuns do [padrão](../../padroes/api.md#configuração-comum),
com `BASE_PATH` = `/roothints` (inválido: `--base-path inválido: "/" (ex.:
/roothints)`); nenhuma opção a mais. `BASE_PATH` ganha uma barra no começo e
perde a do fim (`roothints` e `/roothints/` viram `/roothints`).

- Compose: o [modelo da API](../../plataforma/docker.md#api) com
  `<FONTE>` = `ROOTHINTS` e a porta 8109.
- `.env.example` do app: o [modelo](../../plataforma/docker.md#envexample-do-app)
  completo — `POSTGRES_PASSWORD=`, `VALKEY_PASSWORD=`, as URLs inteiras
  comentadas (`# POSTGRES_URL=…`, `# API_ROOTHINTS_REDIS_URL=…`),
  `API_ROOTHINTS_TAG=latest`, `API_ROOTHINTS_HOST_PORT=8109`,
  `API_ROOTHINTS_CACHE_TTL=3600`, os opcionais comentados
  (`API_ROOTHINTS_CACHE_ENABLED`, `API_ROOTHINTS_DB_POOL_MAX`,
  `API_TRUSTED_PROXIES`), as variáveis do Traefik e `LOG_LEVEL=info`.
- `.env.example` da raiz: `API_ROOTHINTS_TAG=latest` (imagens),
  `API_ROOTHINTS_HOST_PORT=8109` e `API_ROOTHINTS_CACHE_TTL=3600` (APIs).
- Traefik: as labels do [modelo](../../plataforma/publicacao.md#traefik) com
  `<fonte>` = `roothints` (router e serviço `badblock-api-roothints`,
  middleware `badblock-api-roothints-ratelimit`, regra
  `Host(api.badblock.net.br) && (Path(/roothints) || PathPrefix(/roothints/))`).

| Variável no `.env` | Vira | Padrão no compose |
|---|---|---|
| `API_ROOTHINTS_TAG` | tag da imagem e `VERSION` do build | `latest` (`dev` no build) |
| `API_ROOTHINTS_HOST_PORT` | porta no loopback do host | `8109` |
| `API_ROOTHINTS_CACHE_TTL` | `REDIS_KEY_TTL` | `3600` |
| `API_ROOTHINTS_CACHE_ENABLED` | `REDIS_CACHE_ENABLED` | `true` |
| `API_ROOTHINTS_DB_POOL_MAX` | `DB_POOL_MAX` | `10` |
| `API_ROOTHINTS_REDIS_URL` | `REDIS_URL` | `redis://:${VALKEY_PASSWORD:-}@badblock-valkey:6379/0` |

## Operação

```bash
make -C apps/roothints/api up      # só este serviço, com o .env da raiz (o stack inteiro: make up na raiz)
make -C apps/roothints/api smoke   # com o stack no ar
curl http://127.0.0.1:8109/roothints/servers
curl http://127.0.0.1:8109/roothints/server/K.ROOT-SERVERS.NET.
```

O `Makefile` segue o [modelo completo](../../plataforma/docker.md#makefile-dos-apps):
`SOURCE := roothints`, `PORT := $${API_ROOTHINTS_HOST_PORT:-8109}` (o teste
do manifesto lê este `PORT`) e `SMOKE_SERVER ?= a`. `make smoke` faz
`curl -fsS` em `http://127.0.0.1:$(PORT)` nas rotas `/roothints/status`,
`/roothints/meta`, `/roothints/servers`, `/roothints/server/$(SMOKE_SERVER)`
e `/roothints/openapi.yaml` (o corpo do manifesto é descartado).

## Manifesto OpenAPI

`openapi/openapi.yaml` segue [openapi.md](../../padroes/openapi.md), com os
exemplos e as mensagens de [api-rotas.md](api-rotas.md):

- `info`: `title: api-roothints`, `version: '0.1.0'`, `summary`,
  `description` (os dados, endereços sem máscara e `null`, o bloco `source`,
  versões como servidores, `HEAD`/`OPTIONS`, redirect, cabeçalhos comuns,
  erros e validação antes de tudo) e `license` MIT; `externalDocs` para
  `https://github.com/patrickbrandao/badblock/tree/main/specs/fontes/roothints`.
- `servers`: os quatro do padrão com a porta 8109 e as descrições do padrão;
  nos paths fora do versionamento, `Produção` e `Desenvolvimento local`.
- `tags`: `dados` (consultas, com ETag e Valkey), `meta` (índice, estado e
  manifesto), `saúde`.

| Path | `operationId` | Tag | Respostas |
|---|---|---|---|
| `/` | `getIndex` | `meta` | 200 `IndexResponse` |
| `/servers` | `listServers` | `dados` | 200 `ServersResponse` (exemplo `named_root_2026_09_30`, a lista inteira), 304, 500, 503, 504 — sem 400 nem 404 |
| `/server/{server}` | `getServer` | `dados` | 200 `ServerResponse` (exemplos `pela_letra` — o `a` — e `pelo_nome` — o `k`), 304, 400, 404, 500, 503, 504 |
| `/meta` | `getMeta` | `meta` | 200 `MetaResponse` (`carregado` e `antes_da_primeira_carga`), 500, 503 `DatabaseUnavailable`, 504 |
| `/health`, `/status` | `getHealth`, `postHealth`, `getStatus`, `postStatus` | `saúde` | 200 `StatusOK` (exemplos `ok` e `starting`), 500, 503 `StatusError` |
| `/ping` | `ping` | `saúde` | 200 `text/plain`, `const: pong` |
| `/openapi.yaml` | `getOpenAPI` | `meta` | 200 `application/yaml` |

- `components.parameters`: `Server` (texto,
  `pattern: '^[A-Za-z](\.[Rr][Oo][Oo][Tt]-[Ss][Ee][Rr][Vv][Ee][Rr][Ss]\.[Nn][Ee][Tt]\.?)?$'`,
  exemplo `a`) e `IfNoneMatch` (exemplo `W/"c618b310d98767be"`, o ETag de
  `server:a`).
- `components.headers`: `ETag` (exemplo `W/"fbf484ee604cba0"`, o de
  `/servers`), `CacheControlPublic`, `CacheControlNoStore`, `XCache`
  (`enum: [HIT, MISS, BYPASS]`) e `XDatasetVersion` (`format: uuid`, exemplo
  `01a0f06d-08c2-7f4a-924a-b345129d0f5a`).
- `components.responses`: `NotModified`, `BadRequest` e `NotFound` (os de
  `/server/{server}`), `ServiceUnavailable` (exemplos `dataset_not_ready` e
  `database_unavailable`), `DatabaseUnavailable` (o 503 do `/meta`),
  `Timeout`, `InternalError`, `StatusOK` e `StatusError`.
- `components.schemas`: `Dataset`, `Source`, `Server`, `ServersResponse`,
  `ServerResponse` (`allOf` de `Server` e dos demais campos), `MetaResponse`
  (`dataset` e `collector` como `oneOf` do objeto ou `null`), `MetaDataset`,
  `Collector`, `Status`, `StatusChecks`, `IndexResponse` e `Error` (`code`
  com `enum` dos seis códigos do padrão). `ipv4`/`ipv6`: texto
  `format: ipv4`/`ipv6` ou `null`; TTLs inteiros de 0 a 2147483647;
  `letter` `'^[a-z]$'`; `name` `'^[a-z]\.root-servers\.net$'`;
  `zone_serial` de 0 a 4294967295; `last_update` `format: date`.

## Medições

Em 2026-09-30, com o `named.root` do dia (3.315 bytes, 13 servidores):

| O quê | Valor | Como |
|---|---|---|
| `/roothints/servers` | 2.462 bytes; 1,8–1,9 ms no `MISS`, 1,3–1,8 ms no `HIT` | `curl` na API local (binário, fora do Docker) sobre o PG18 e o Valkey descartáveis, com o Valkey esvaziado antes de cada `MISS` |
| `/roothints/server/{server}` | 400 a 417 bytes (`m` e `j`); 1,4–1,6 ms no `MISS`, 1,4 ms no `HIT` | idem |
| `/roothints/meta`, `/roothints/status` | 515 e 160 bytes; 2–3 ms | idem |
| manifesto | 40.549 bytes | idem |
| carga pelo coletor + todas as rotas | ~0,5 s de carga; cada rota abaixo de 1 ms, sem cache | `TestRealFile` (PG18 do testcontainers, handler e store reais) |

## Testes

Camadas e comandos: [padrão](../../padroes/api.md#testes) e
[testes.md](../../processos/testes.md). O que é desta API (unitários com um
store falso com os 13 servidores reais de 2026-09-30, versão `0192-v1`,
`BASE_PATH` `/roothints/`, que vira `/roothints`):

| Pacote | Testes | O quê |
|---|---|---|
| `httpapi` | `TestServers`, `TestServer`, `TestNormalizeServer`, `TestNullFields` | lista em ordem, corpo exato, `source` e `dataset`, uma chave com e sem `/v1`; servidor pela letra e pelo nome em qualquer caixa, com e sem ponto final e `/v1` numa chave só (`server:k`, uma consulta), corpo exato do `a`; 404 (`n`, `Z.ROOT-SERVERS.NET.`) com a mensagem e fora do cache; a tabela de `normalizeServer` (sinal de kelvin, `a.`, dois pontos, outro domínio, espaço, NUL); `null` em endereço, TTL e nota; `source` `null` sem cabeçalho |
| `httpapi` | `TestErrors`, `TestDatabaseErrors`, `TestMetaDatabaseError`, `TestPanicIsJSON500`, `TestNotReady` | 400 com a mensagem e 404 de cada caso (inclusive `/v2`, `/v1/openapi.yaml`, fora do caminho de base, `DELETE`), erro sempre JSON com `no-store`; 503 `database_unavailable` e 504 `timeout` nas rotas de dados e no `/meta`; erro fora do cache; panic → 500 JSON; `dataset_not_ready` sem consultar o banco, 400 antes do 503, `/status` `starting`, `/meta` com `null` |
| `httpapi` | `TestCacheAndETag`, `TestHEAD`, `TestBypassAndBigBody` | `MISS` → `HIT` com a mesma chave e o mesmo ETag com e sem `/v1`; `If-None-Match` fraco, forte, em lista e `*` → 304 sem corpo, sem `Content-Type` e sem consultar; ETag por consulta, letra = nome; os ETags reais desta spec; `HEAD` (por um `httptest.Server`) nas rotas de dados, no índice, na meta e no status; `BYPASS` sem cache; corpo acima de 8 MiB fora do cache |
| `httpapi` | `TestStatus`, `TestCORSAndHeaders`, `TestIndexMetaAndRedirect` | `/health` e `/status` em GET e POST nos quatro estados (Valkey fora vence `starting`) e com o cache desligado (`disabled`), `pong`, `/v1/status` 404; preflight e cabeçalhos comuns, inclusive no 404; índice nas duas versões com os `endpoints` exatos, redirect com e sem query, `/meta` com `no-store` e sem `X-Cache` |
| `httpapi` | `TestOpenAPIMatchesRoutes`, `TestOpenAPIDocument`, `TestOpenAPIRoute` | o teste completo do [padrão](../../padroes/openapi.md#teste) (o da família RIR): `openapi: 3.1.`, sem tabulação, `info.title`, os quatro `servers` com a porta lida do `PORT` do `Makefile`, `servers` próprios só sem `/v1`, rotas registradas × manifesto nos dois sentidos, um `operationId` por operação e sem repetição, `$ref` resolvidos, `GET`/`HEAD /openapi.yaml`, 404 em `/v1` e o índice × manifesto |
| `dataset` | `TestWatcherRefresh`, `TestWatcherRunStopsWithContext` | versão, `LastUpdate` (`AAAA-MM-DD`) e `ZoneSerial` no `Snapshot`; erro mantém a versão; linha sem cabeçalho; `Run` atualiza e para com o contexto |
| `config` | `TestDefaults`, `TestBasePathNormalized`, `TestPrecedence`, `TestInvalid`, `TestHelpListsEveryOption` | padrões (`/roothints`, 8080, 1 h, 50 ms); normalização do caminho; argumento > ambiente > padrão; recusas; `--help` com toda opção e variável |
| `cache` | `TestBreakerOpensAndCloses`, `TestNoop` | disjuntor e o cache desligado |
| `realip` | `TestClientIP`, `TestNewRejectsBadInput` | proxies confiáveis e cabeçalhos |
| `store` (integração) | `TestQueries` | abaixo |
| `httpapi` (integração) | `TestRealFile` | abaixo |

**`internal/testdb`** (build tag `integration`): `testdb.New(t)` sobe um
`postgres:18-trixie` descartável pelo testcontainers (banco `badblock`,
usuário `postgres`, senha `pg`), aplica o `migrate:up` de cada `.sql` de
`database/postgres/central/` e `database/postgres/roothints/`, em ordem de
nome (a pasta sai da posição do próprio arquivo, por `runtime.Caller`), e
devolve a URL e um pool administrativo; o container e o pool somem no fim do
teste. Sem nenhum `.sql` numa das pastas, falha com `nenhuma migration em
database/postgres/<pasta>`.

**`TestQueries`**: banco vazio (sem versão, sem `jobs`, lista vazia); três
servidores reais (`a`, `k` e o `m` sem `AAAA` e sem comentário) inseridos
fora de ordem, duas execuções aplicadas e uma recusada, a mais nova. Confere a
versão (a última aplicada, não a última linha) com MD5, SHA-256, URL,
`last_update`, `zone_serial` e `servers`; `jobs`; a lista em ordem de letra
com os endereços sem máscara, TTLs, nota e datas; o `m` com os `NULL`;
`ErrNotFound` para `b`, `z`, `A` e vazio; o plano pela letra em
`uq_roothints_server_letter` (com `enable_seqscan = off`); sem execução
aplicada, a versão volta a ser nula.

### Arquivo real (`TestRealFile`)

```bash
make test-int                                   # inclui o TestRealFile com a fixture do coletor
curl -o /tmp/named.root https://www.internic.net/domain/named.root
make test-real FILE=/tmp/named.root             # o arquivo do dia, só este teste, com tempos no log
```

De dentro de `apps/roothints/api/`. `ROOTHINTS_REAL_FILE` aponta o arquivo
(`make test-real` a define com o caminho absoluto de `FILE`); sem ela, o
teste usa a fixture do coletor, `apps/roothints/collector/testdata/named.root`
— o `named.root` inteiro de 2026-09-30 —, então roda em todo `make test-int`
sem rede.

1. Compila o coletor irmão (`go build ./cmd/collector-roothints` em
   `../collector`), serve o arquivo e o `.md5` calculado dele por um
   `httptest.Server` e roda `collector-roothints --once` contra um PG18 do
   `testdb`.
2. `/roothints/meta` bate com a execução gravada (servidores, MD5,
   `last_update`, `zone_serial`, coletor).
3. `/roothints/servers` bate, item a item e em ordem, com
   `roothints_server` lida pelo pool administrativo (`host()` nos
   endereços), e o bloco `source` com o cabeçalho; cada servidor pela letra
   em maiúsculas e pelo nome absoluto em maiúsculas (`/v1/server/A.ROOT-SERVERS.NET.`)
   é igual ao item da lista; `/server/z` é 404.
4. Com a fixture, os valores de [fonte.md](fonte.md#fatos-medidos): 13
   servidores, serial `2026092401`, `2026-09-24`, os endereços e notas de
   `a` e `m`, TTL `3600000`.
5. `EXPLAIN` com `enable_seqscan = off`: `letter = 'k'` usa
   `uq_roothints_server_letter` e `name = 'k.root-servers.net'`,
   `uq_roothints_server_name`.

Rode `make test-real FILE=...` quando a fonte mudar de formato ou a fixture
for trocada ([fonte.md](fonte.md#fixture)), e compare com as
[medições](#medições).

## Mudar a API

- Rota, parâmetro, campo ou resposta: [api-rotas.md](api-rotas.md) e o
  manifesto no mesmo trabalho, depois o código ([rota nova](#rota-nova)) e os
  testes de `httpapi` ([fluxo](../../processos/fluxo-de-trabalho.md)).
  Mudança incompatível no JSON = `/roothints/v2/...`
  ([padrão](../../padroes/api.md#caminho-de-base-e-versões)).
- Consulta ou índice novo: o pedido, com a migration proposta, vai ao
  coletor ([dados.md](dados.md#mudar-o-schema)); depois `TestQueries` e
  `TestRealFile`.
- Mudou a fixture ou uma regra do parser do coletor: `TestRealFile` (os
  valores do passo 4) e os exemplos de [api-rotas.md](api-rotas.md).
- Opção nova: `--help`, `.env.example` do app e da raiz, compose e esta spec
  ([convenções](../../projeto/convencoes.md#configuração)).
