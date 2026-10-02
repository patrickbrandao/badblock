# rootanchors — API (`api-rootanchors`)

O que a `api-rootanchors` faz de diferente ou a mais que o
[padrão das APIs](../../padroes/api.md) e o
[padrão do manifesto](../../padroes/openapi.md). O resto — `main.go`, caminho
de base e versões, formato, versão do dataset, `serveCached`, ETag,
cabeçalhos, saúde, erros, IP real, configuração comum, container — segue os
padrões. Cada rota, com validações, campos, exemplos reais e mensagens de
erro: [api-rotas.md](api-rotas.md). Tabelas e o SQL das consultas:
[dados.md](dados.md#consultas-da-api-rootanchors); de onde vêm os campos:
[fonte.md](fonte.md). Dono: sub-agente `api-rootanchors`. O código partiu da
`api-asnames` (fonte única), com o `Makefile` e o teste do manifesto da
família RIR (`PORT` lido do `Makefile`).

## Valores desta fonte

| Item | Valor |
|---|---|
| App, binário | `api-rootanchors` (`/api-rootanchors` na imagem) |
| Module Go | `github.com/patrickbrandao/badblock/apps/rootanchors/api` |
| Imagem, container | `tmsoftbrasil/badblock-api-rootanchors`, `badblock-api-rootanchors` |
| Descrição da imagem (`org.opencontainers.image.description`) | `API HTTP das âncoras de confiança DNSSEC da raiz (root-anchors.xml da IANA), do BadBlock` |
| Caminho de base (`BASE_PATH`) | `/rootanchors` — `https://api.badblock.net.br/rootanchors/` |
| Porta local | 8111 (`API_ROOTANCHORS_HOST_PORT`, só no loopback) |
| Lê | `rootanchors_key`, `rootanchors_run` e a linha `collector-rootanchors` de `jobs` |
| `source` do índice | `https://data.iana.org/root-anchors/root-anchors.xml` (constante `SourceURL`) |
| Chave no Valkey | `badblock:api-rootanchors:<versão>:<consulta>` ([abaixo](#cache-e-etag)) |
| Cliente do Valkey | `ClientName` = `badblock-api-rootanchors` |
| Cabeçalho `Server` | `badblock-api-rootanchors/<versão>` |
| Pool pgx | `application_name = api-rootanchors` quando a `POSTGRES_URL` não traz outro; sem outro parâmetro (as consultas não têm termo livre) |
| Pacotes | os do padrão, mais `internal/testdb` (o PG18 descartável dos testes de integração, como na `api-asnames`) |
| Arquivo real nos testes | a fixture do coletor (`apps/rootanchors/collector/testdata/root-anchors.xml`, o arquivo real inteiro) no `make test-int`; outro arquivo com `make test-real FILE=...` |

Em `internal/httpapi`: `server.go` (`Handler`, a lista `dataRoutes`,
middleware, `serveCached`, `etagFor`, erros), `handlers.go` (rotas,
validação do key tag, `SourceURL`) e `types.go` (os tipos do JSON e `keyOf`,
que monta os textos `ds` e `dnskey`). `internal/store` tem as consultas de
[dados.md](dados.md#consultas-da-api-rootanchors), com a lista de colunas de
`rootanchors_key` numa constante (`keyColumns`) usada por todas as consultas
de chaves.

O `--help` começa assim (depois vêm as opções do padrão, em ordem
alfabética, cada uma com a variável e o padrão):

```
api-rootanchors — API HTTP das âncoras de confiança DNSSEC da zona raiz
(root-anchors.xml da IANA).

Lê as tabelas rootanchors_* mantidas pelo collector-rootanchors, com cache
opcional no Valkey. Responde tudo abaixo de BASE_PATH (/rootanchors):
/rootanchors/keys é a versão atual e /rootanchors/v1/keys fixa a v1.

Uso:
  api-rootanchors [opções]
  api-rootanchors healthcheck    (usa HTTP_PORT e BASE_PATH; para o HEALTHCHECK do Docker)

Opções (padrão → variável de ambiente → argumento):
```

## Rotas

Todas abaixo de `/rootanchors`; as de dados, o índice e `/meta` respondem
também em `/rootanchors/v1/...`. Detalhes: [api-rotas.md](api-rotas.md).

| Rota | `operationId` | Tag | Chave de cache |
|---|---|---|---|
| `GET /rootanchors/keys` | `listKeys` | `dados` | `keys` |
| `GET /rootanchors/key/{key_tag}` | `getKeyTag` | `dados` | `key:20326` |
| `GET /rootanchors/meta` | `getMeta` | `meta` | — (sem cache) |
| `GET /rootanchors/` | `getIndex` | `meta` | — |
| `GET`, `POST /rootanchors/health` | `getHealth`, `postHealth` | `saúde` | — |
| `GET`, `POST /rootanchors/status` | `getStatus`, `postStatus` | `saúde` | — |
| `GET /rootanchors/ping` | `ping` | `saúde` | — |
| `GET /rootanchors/openapi.yaml` | `getOpenAPI` | `meta` | — |

Os nomes seguem o estilo das outras APIs: o plural para a lista (`/keys`,
como `/cgibr/asns`) e o singular para o item (`/key/{key_tag}`, como
`/cgibr/asn/{asn}`). Rotas futuras (ex.: uma chave pelo `key_id`, que já tem
índice — [dados.md](dados.md#consultas-da-api-rootanchors)) entram na mesma
forma: spec, manifesto, uma linha em `dataRoutes`, o handler e o endpoint no
índice.

## Dados servidos

- **Todas** as chaves do arquivo, inclusive as aposentadas: a IANA mantém a
  KSK-2010 (19036) com `validUntil` 2019-01-11. Em 2026-09-30 são 3.
- O **registro DS em texto** (`ds`) em toda chave, e o **DNSKEY em texto**
  (`dnskey`) nas que trazem `PublicKey`, no formato de arquivo de zona sem
  TTL: `. IN DS 20326 8 2 E06D…EC8D` e `. IN DNSKEY 257 3 8 AwEAAaz/…74bU=`
  (o formato completo em [api-rotas.md](api-rotas.md#a-chave-key)). O dono é
  a `zone` do `TrustAnchor` (sempre `.`), não uma constante no código.
  Escolhido por ser o formato de apresentação padrão dos registros (o que o
  `dig` mostra, sem o TTL), que um resolvedor aceita como âncora sem
  conversão (ex.: `trust-anchor:` e `trust-anchor-file` do Unbound), e o que
  [dados.md](dados.md#consultas-da-api-rootanchors) lista como derivação da
  API.
- O **`trust_anchor`** (`id`, `source`, `zone`) vem da última execução
  aplicada de `rootanchors_run`, lida pela mesma consulta da versão do
  dataset e do `/meta`. Numa resposta nova (`MISS`) são duas consultas: a
  execução e as chaves.
- `first_seen` e `updated_at` são o `created_at` e o `updated_at` de
  `rootanchors_key`: datas **deste banco**, não da IANA.

### Chave ativa e o relógio

"Ativa" depende do relógio: uma chave está ativa no instante `t` quando
`valid_from <= t` e (`valid_until` é `null` ou `valid_until > t`)
([dados.md](dados.md#rootanchors_key)). O corpo de uma rota de dados, porém,
é guardado no Valkey (até `REDIS_KEY_TTL`, 1 h) e em clientes e proxies
(`max-age=300`, e o ETag só muda com a versão dos dados), e a versão pode
ficar meses igual — o arquivo muda só nas etapas de uma rolagem. Um campo
`active` gravado nesse corpo passaria a mentir no instante em que um
`valid_from` ou `valid_until` fosse cruzado, sem que nada invalidasse o cache.

Decisão: **a API não publica campo dependente do relógio** — nem `active`,
nem `expired`, nem "chave atual". Ela entrega `valid_from` e `valid_until`
(UTC, RFC 3339) e o cliente calcula com a própria hora, pela regra acima (em
2026-09-30: 20326 e 38696 ativas, 19036 expirada). Alternativas descartadas:
calcular o campo fora do cache a cada pedido (o corpo deixaria de depender
só da chave e da versão, e o ETag e o 304 mentiriam) ou pôr a hora na chave
de cache (acaba com o cache). Um filtro "só as ativas" teria o mesmo
problema e fica fora pelo mesmo motivo. O `TestKeys` falha se aparecer
`active` no corpo.

## Cache e ETag

Toda rota de dados passa pelo `serveCached` do
[padrão](../../padroes/api.md#cache-valkey-cache-aside-e-servecached). A
chave é a consulta normalizada, e a consulta ao banco usa o mesmo valor.

| Pedido | Chave | Normalização |
|---|---|---|
| `/keys`, `/v1/keys` | `keys` | — (a query é ignorada) |
| `/key/020326`, `/v1/key/20326` | `key:20326` | decimal sem zeros à esquerda |

ETags reais (`etagFor` em `internal/httpapi/server.go`) no dataset
`01a0f06a-259b-7abc-a1fd-a7b4bd279125`, vistos em 2026-09-30 (o
`TestETagOfExamples` os confere):

| Consulta | ETag |
|---|---|
| `keys` | `W/"c3581f03fc89adf3"` |
| `key:19036` | `W/"54e586113b1ebf5f"` |
| `key:20326` | `W/"e2045590e46611e7"` |
| `key:38696` | `W/"9e9bffe234a5244"` |

- Chave completa, por exemplo:
  `badblock:api-rootanchors:01a0f06a-259b-7abc-a1fd-a7b4bd279125:key:20326`.
- 404 (key tag sem chave) e erros não entram no cache; o 503
  `dataset_not_ready` da corrida (watcher com versão, banco sem execução
  aplicada) também não.
- Limite do corpo guardado: 8 MiB (`maxCachedBody`); a maior resposta,
  `/keys`, tem 2.907 bytes.

## Consultas

O SQL e os índices estão em
[dados.md](dados.md#consultas-da-api-rootanchors) (dono: o coletor):
`ix_rootanchors_run_applied` (versão, `trust_anchor` e `/meta`),
`ix_rootanchors_key_key_tag` (`/key/{key_tag}`) e a tabela inteira em
`/keys`. A tabela tem poucas linhas e o planejador prefere percorrê-la; o
`TestQueries` confere, com `enable_seqscan = off`, que os índices servem às
consultas. Não há `plan_cache_mode` nem outro ajuste do planejador.

## Configuração

Só as opções comuns do [padrão](../../padroes/api.md#configuração-comum),
com `BASE_PATH` = `/rootanchors` (inválido:
`--base-path inválido: "/" (ex.: /rootanchors)`); nenhuma opção a mais.

- Compose: o [modelo da API](../../plataforma/docker.md#api) com
  `<FONTE>` = `ROOTANCHORS` e a porta 8111 — `API_ROOTANCHORS_TAG`
  (`latest`; `dev` no build), `API_ROOTANCHORS_HOST_PORT` (`8111`),
  `API_ROOTANCHORS_REDIS_URL`
  (`redis://:${VALKEY_PASSWORD:-}@badblock-valkey:6379/0`),
  `API_ROOTANCHORS_CACHE_ENABLED` (`true`), `API_ROOTANCHORS_CACHE_TTL`
  (`3600`) e `API_ROOTANCHORS_DB_POOL_MAX` (`10`), mais as compartilhadas do
  padrão (`POSTGRES_URL`/`POSTGRES_PASSWORD`, `VALKEY_PASSWORD`,
  `API_TRUSTED_PROXIES`, `API_FQDN`, `TRAEFIK_*`, `API_RATE_*`,
  `LOG_LEVEL`). Fixos: `HTTP_PORT` `8080`, `BASE_PATH` `/rootanchors`,
  `LOG_FORMAT` `json`.
- `.env.example` do app: o [modelo](../../plataforma/docker.md#envexample-do-app),
  com os opcionais comentados (`API_ROOTANCHORS_CACHE_ENABLED`,
  `API_ROOTANCHORS_DB_POOL_MAX`, `API_TRUSTED_PROXIES`).
- `.env.example` da raiz: `API_ROOTANCHORS_TAG=latest` (imagens),
  `API_ROOTANCHORS_HOST_PORT=8111` e `API_ROOTANCHORS_CACHE_TTL=3600` (APIs).
- Traefik: as labels do [modelo](../../plataforma/publicacao.md#traefik) com
  `<fonte>` = `rootanchors` (router e serviço `badblock-api-rootanchors`,
  middleware `badblock-api-rootanchors-ratelimit`; regra
  ``Host(`api.badblock.net.br`) && (Path(`/rootanchors`) || PathPrefix(`/rootanchors/`))``).

## Operação

```bash
make -C apps/rootanchors/api up      # só este serviço, com o .env da raiz (o stack inteiro: make up na raiz)
make -C apps/rootanchors/api smoke   # com o stack no ar
curl http://127.0.0.1:8111/rootanchors/keys
curl https://api.badblock.net.br/rootanchors/key/20326
```

`make smoke` faz `curl -fsS` em `http://127.0.0.1:$(PORT)` — `PORT` =
`${API_ROOTANCHORS_HOST_PORT:-8111}` — nas rotas `/rootanchors/status`,
`/rootanchors/meta`, `/rootanchors/keys` (corpo descartado),
`/rootanchors/key/$(SMOKE_KEY_TAG)` (padrão `20326`) e
`/rootanchors/openapi.yaml` (corpo descartado). O `Makefile` tem `SOURCE`,
`PORT` e `SMOKE_KEY_TAG`, como os das APIs de RIR.

Logs: os do [padrão](../../padroes/api.md#logs), com `app=api-rootanchors`.

## Manifesto OpenAPI

`openapi/openapi.yaml` segue [openapi.md](../../padroes/openapi.md), com os
exemplos e as mensagens de [api-rotas.md](api-rotas.md):

- `info`: `title: api-rootanchors`, `version: '0.1.0'`, `summary`,
  `description` (os dados, o DS e o DNSKEY em texto, a regra de "ativa" e
  por que ela não é campo, key tag não único, versões como servidores,
  `HEAD`/`OPTIONS`, redirect e 404, cabeçalhos comuns, erros e validação
  antes de tudo) e `license` MIT; `externalDocs` para
  `https://github.com/patrickbrandao/badblock/tree/main/specs/fontes/rootanchors`.
- `servers`: os quatro do padrão, com a porta 8111 e as descrições do
  padrão; nos paths fora do versionamento, `Produção (fora do versionamento)`
  e `Desenvolvimento local (fora do versionamento)`.
- `tags`: `dados`, `meta`, `saúde`.

| Path | `operationId` | Tag | Respostas |
|---|---|---|---|
| `/` | `getIndex` | `meta` | 200 `IndexResponse` |
| `/keys` | `listKeys` | `dados` | 200 `KeysResponse` (exemplo `todas`, a resposta real inteira), 304, 500, 503, 504 — sem 400 nem 404 |
| `/key/{key_tag}` | `getKeyTag` | `dados` | 200 `KeyTagResponse` (`ksk_2017`, `ksk_2010`), 304, 400, 404, 500, 503, 504 |
| `/meta` | `getMeta` | `meta` | 200 `MetaResponse` (`carregado` e `antes_da_primeira_carga`), 500, 503 `DatabaseUnavailable`, 504 |
| `/health`, `/status` | `getHealth`, `postHealth`, `getStatus`, `postStatus` | `saúde` | 200 `StatusOK` (`ok`, `starting`, `degraded`), 500, 503 `StatusError` |
| `/ping` | `ping` | `saúde` | 200 `text/plain`, `const: pong` |
| `/openapi.yaml` | `getOpenAPI` | `meta` | 200 `application/yaml` |

- `components.parameters`: `KeyTag` (texto, `pattern: '^[0-9]+$'`, exemplo
  `'20326'`) e `IfNoneMatch`.
- `components.headers`: `ETag` e `IfNoneMatch` com o exemplo
  `W/"e2045590e46611e7"` (o de `/rootanchors/key/20326`), `CacheControlPublic`,
  `CacheControlNoStore`, `XCache` (`enum: [HIT, MISS, BYPASS]`) e
  `XDatasetVersion` (`01a0f06a-259b-7abc-a1fd-a7b4bd279125`).
- `components.responses`: os do padrão; `BadRequest` e `NotFound` com o
  exemplo `key_tag`; `ServiceUnavailable` com `dataset_not_ready` e
  `database_unavailable`.
- `components.schemas`: `Dataset`, `TrustAnchor` (`zone` com `const: '.'`),
  `Key` (`key_tag` de 0 a 65535, `algorithm` de 0 a 255, `digest_type` em
  `[1, 2, 4]`, `digest` `'^[0-9A-F]+$'`, `public_key`/`flags`/`dnskey` e
  `valid_until` com `null`), `KeysResponse` (`count` ≥ 0), `KeyTagResponse`
  (`count` ≥ 1), `MetaResponse`, `MetaDataset`, `Collector`, `Status`,
  `StatusChecks`, `IndexResponse` e `Error`.

## Medições

Em 2026-09-30, com o arquivo do dia (1.861 bytes, 3 chaves):

| O quê | Valor |
|---|---|
| Tamanho das respostas | `/keys` 2.907 bytes; `/key/20326` e `/key/38696` 1.384; `/key/19036` 691; `/meta` ~565; `/status` 168 |
| Binário local (macOS, Postgres descartável na mesma máquina), por `curl` na porta 8111 | `MISS` 3 a 4 ms; `HIT` ~1 ms; `/meta` e `/status` ~1,5 ms |
| `TestRealFile` (handler e store reais, sem cache, média de 200 pedidos) | `/keys` 368 µs; `/key/20326` 281 µs |
| Carga do arquivo pelo coletor no `TestRealFile` | ~340 ms (inclui o processo do coletor) |
| Manifesto | ~38 KB |

## Testes

Camadas e comandos: [padrão](../../padroes/api.md#testes) e
[testes.md](../../processos/testes.md). O que é desta API:

| Pacote | Testes | O quê |
|---|---|---|
| `httpapi` | `TestKeys`, `TestKeyDSAndDNSKEY`, `TestKeyTag`, `TestParseKeyTag` | store falso com as 3 chaves reais (e, quando pedido, uma artificial com o key tag 20326): ordem, `trust_anchor`, `source` `null`, o JSON exato da 19036 (nulos), `ds` e `dnskey` (dono vindo da zona), nenhum campo `active`, JSON compacto; key tag com zeros à esquerda e `/v1` numa chave só (uma consulta ao banco); key tag repetido; 0 e 65535 aceitos (404); faixa, sinal, espaço, `1e3`, `0x10`, dígitos não-ASCII e números enormes recusados |
| `httpapi` | `TestErrors`, `TestDatabaseErrors`, `TestPanicIsJSON500`, `TestNotReady` | 400 e 404 (inclusive `/key`, `/key/`, barra no fim, `/keys/20326`, `/v2`, métodos não registrados), erro sempre JSON com `no-store`; 503 `database_unavailable` (também na leitura do `trust_anchor`) e 504 `timeout`; erro e 404 fora do cache; panic → 500 JSON; `dataset_not_ready` sem consultar o banco, 400 antes do 503, a corrida watcher × banco, `/status` `starting`, `/meta` com `null` |
| `httpapi` | `TestETagOfExamples`, `TestCacheAndETag`, `TestHEAD`, `TestBypassAndBigBody` | os ETags desta spec e do manifesto; `MISS` → `HIT` com a mesma chave e o mesmo ETag com e sem `/v1`; `If-None-Match` fraco, forte, em lista e `*` → 304 sem corpo, sem `Content-Type` e sem consultar; `HEAD` nas rotas de dados, índice, meta, status e ping; `BYPASS` sem cache; corpo acima de 8 MiB fora do cache |
| `httpapi` | `TestStatus`, `TestCORSAndHeaders`, `TestIndexMetaAndRedirect` | `/health` e `/status` em GET e POST nos quatro estados (e `degraded` sem dataset), `disabled` sem cache, `pong`; preflight (inclusive fora da base) e cabeçalhos comuns no 200, 400 e 404; índice nas duas versões, redirect com e sem query, `/meta` com `no-store` e sem `X-Cache`/`ETag` |
| `httpapi` | `TestOpenAPIMatchesRoutes`, `TestOpenAPIDocument`, `TestOpenAPIRoute` (`openapi_test.go`) | o teste completo do [padrão](../../padroes/openapi.md#teste), igual ao da família RIR: `openapi: 3.1.`, tabulação, `info.title`, os quatro `servers` com a porta lida do `PORT` do `Makefile`, `servers` próprios, rotas × paths nos dois sentidos, `operationId` único e presente em toda operação, `$ref` resolvido, `GET`/`HEAD` de `/openapi.yaml`, 404 em `/v1` e o índice × manifesto |
| `config` | `TestDefaults`, `TestBasePathNormalized`, `TestPrecedence`, `TestInvalid`, `TestVersionWithoutPostgres`, `TestHelpListsEveryOption` | padrões (`/rootanchors`, 8080, 1 h, 50 ms); normalização do caminho; argumento > ambiente > padrão; recusas; `--version` sem `POSTGRES_URL`; `--help` com toda opção, o uso e o cabeçalho |
| `dataset` | `TestWatcherRefresh`, `TestWatcherRunStopsWithContext` | releitura da versão, erro mantém a conhecida, versão nova; `Run` para com o contexto |
| `cache` | `TestBreakerOpensAndCloses`, `TestNoop` | disjuntor e cache desligado |
| `realip` | `TestClientIP`, `TestNewRejectsBadInput` | como nas outras APIs |
| `store` (integração) | `TestQueries` | abaixo |
| `httpapi` (integração) | `TestRealFile` | abaixo |

**`TestQueries`** (num PG18 do `testdb`, com o `migrate:up` de `central/` e
`rootanchors/`): as 3 chaves reais e uma artificial com o key tag 20326
(algoritmo 13, SHA-384), duas execuções aplicadas e uma recusada (a mais
nova). Confere: banco vazio sem versão, sem `jobs` e com lista vazia (não
`nil`); a versão é a última aplicada, com `anchor_source` NULL; a lista em
ordem de `valid_from` e `key_id`, com os NULLs da 19036; por key tag,
repetido (duas, em ordem), único e inexistente (0, 65535, 12345); o
`EXPLAIN` (com `enable_seqscan = off`) usa `ix_rootanchors_run_applied`,
`ix_rootanchors_key_key_tag` e `uq_jobs_app`; sem execução aplicada, a
versão volta a ser nula.

**`TestRealFile`** (roda no `make test-int`; com tempos em `make test-real`):
compila o coletor irmão (`../collector`), serve o arquivo por um
`httptest.Server` e o carrega com `--once` (`SOURCE_SHA256_URL=off`) num PG18
do `testdb`. Sem `ROOTANCHORS_REAL_FILE`, o arquivo é a fixture do coletor
(o `root-anchors.xml` real inteiro de 2026-09-30); com ela, o do dia:

```bash
curl -o /tmp/root-anchors.xml https://data.iana.org/root-anchors/root-anchors.xml
make -C apps/rootanchors/api test-real FILE=/tmp/root-anchors.xml
```

Confere, com o handler e o store reais e sem cache: `/meta` com as chaves da
tabela e o SHA-256 do arquivo; `/keys` com todas, em ordem; o `ds` de cada
chave; `public_key`, `flags` e `dnskey` juntos; o **key tag e o digest
SHA-256 recalculados a partir do texto `dnskey`** batem com os publicados;
`/key/{key_tag}` de cada chave igual à de `/keys`; `/v1/keys`, `/key/1`
(404), `/key/65536` (400), `/status`; e mede tamanhos e tempos.

## Mudar a API

- Rota, parâmetro, campo ou resposta: [api-rotas.md](api-rotas.md) e o
  manifesto no mesmo trabalho, depois o código (`dataRoutes`, handler,
  tipos, endpoint no índice) e os testes de `httpapi`
  ([fluxo](../../processos/fluxo-de-trabalho.md)). Mudança incompatível no
  JSON vai para `/rootanchors/v2/...`.
- Campo que dependa do relógio: não entra no corpo em cache
  ([acima](#chave-ativa-e-o-relógio)).
- Consulta ou índice novo: o pedido, com a migration proposta, vai ao
  coletor ([dados.md](dados.md#mudando-o-schema)); depois `TestQueries` e
  `TestRealFile`.
- Formato novo da fonte (ex.: outro `DigestType`, algoritmo novo numa
  rolagem): os textos `ds`/`dnskey` não mudam de forma; confira o
  `checkDNSKEY` do `TestRealFile`, que hoje recalcula só SHA-256.
- Opção nova: `--help`, `.env.example` do app e da raiz, compose e esta spec
  ([convenções](../../projeto/convencoes.md#configuração)).
