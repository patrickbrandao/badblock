# API `api-cgibr`

O que a `api-cgibr` faz de diferente ou a mais que o
[padrão das APIs](../../padroes/api.md) e o
[padrão do manifesto](../../padroes/openapi.md); o resto (estrutura do
código, `main.go`, caminho de base e versões, formato, versão do dataset,
`serveCached`, cabeçalhos, erros, IP real, configuração comum, container)
segue os padrões. Cada rota, com validações, chaves de cache e exemplos
reais: [api-rotas.md](api-rotas.md). Tabelas e consultas SQL:
[dados.md](dados.md#consultas-da-api-cgibr). Dono: sub-agente `api-cgibr`.

## Valores desta fonte

| Item | Valor |
|---|---|
| App | `api-cgibr` (demais nomes: [../../projeto/estrutura.md](../../projeto/estrutura.md#nomes)) |
| Caminho de base | `BASE_PATH` = `/cgibr`; em produção, `https://api.badblock.net.br/cgibr/` |
| Porta no loopback | 8101 (`API_CGIBR_HOST_PORT`) |
| Tabelas lidas | `cgibr_run`, `cgibr_asn`, `cgibr_prefix` e a linha `collector-cgibr` de `jobs` |
| Pacotes | só os do padrão (sem `internal/rir` nem pacote extra) |
| Fonte citada no índice | `https://ftp.registro.br/pub/numeracao/origin/nicbr-asn-blk-latest.txt` (constante `SourceURL` do `httpapi`; não vem do banco) |
| Chave de cache | `badblock:api-cgibr:<versão>:<consulta>`, com as consultas da tabela abaixo |
| Nomes nas conexões | `application_name=api-cgibr` no Postgres (se a URL não define outro); `ClientName` `badblock-api-cgibr` no Valkey |
| Cabeçalho `Server` | `badblock-api-cgibr/<versão>` |
| Descrição da imagem | `API HTTP dos ASNs e blocos IP brasileiros do NIC.br, do BadBlock` (`org.opencontainers.image.description`) |
| Exemplo das specs | AS61613 (TMSoft, documento `08.030.063/0001-00`), também na [fixture do coletor](fonte.md#fixture-testdatanicbr-asn-blk-sampletxt) |

## Rotas

Todas abaixo de `/cgibr`. As de dados, o índice e `/meta` respondem também
em `/cgibr/v1/...`; saúde, `ping` e o manifesto existem só sem versão
([padrão](../../padroes/api.md#caminho-de-base-e-versões)).

| Rota | `operationId` | Tag | Chave de cache (exemplo) |
|---|---|---|---|
| `GET /cgibr/asn/{asn}` | `getASN` | `dados` | `asn:61613` |
| `GET /cgibr/ip/{ip}` | `getIP` | `dados` | `ip:45.171.61.10`, `ip:2804:5964::1` |
| `GET /cgibr/prefix/{ip}/{len}` | `getPrefix` | `dados` | `prefix:200.192.152.0/24` |
| `GET /cgibr/document/{doc}` | `getDocument` | `dados` | `doc:08030063000100` |
| `GET /cgibr/asns` | `listASNs` | `dados` | `asns` |
| `GET /cgibr/meta` | `getMeta` | `meta` | — (sem cache) |
| `GET /cgibr/` | `getIndex` | `meta` | — |
| `GET`, `POST /cgibr/health` | `getHealth`, `postHealth` | `saúde` | — |
| `GET`, `POST /cgibr/status` | `getStatus`, `postStatus` | `saúde` | — |
| `GET /cgibr/ping` | `ping` | `saúde` | — |
| `GET /cgibr/openapi.yaml` | `getOpenAPI` | `meta` | — |

Chave completa, por exemplo:
`badblock:api-cgibr:01a0eb55-1a1c-7bdb-915a-e885abbaaf72:asn:61613`. Como
cada consulta é normalizada: [api-rotas.md](api-rotas.md).

## Manifesto

`openapi/openapi.yaml` segue o [padrão](../../padroes/openapi.md) e é
derivado destas specs. O que é desta API:

- `info`: `title: api-cgibr`, `version: 0.1.0`, licença MIT e uma
  `description` que diz: o que a API serve (o arquivo
  `nicbr-asn-blk-latest.txt`, com a URL) e quem o importa
  (`collector-cgibr`); o caminho de base e as versões (a v1 fixa é o
  servidor `.../cgibr/v1`); saúde e manifesto fora do versionamento; `HEAD`
  e `OPTIONS` (204); os cabeçalhos de toda resposta; erros em JSON com
  `Cache-Control: no-store`, o redirect e o 404; sem chave, com rate limit no
  Traefik. **Pendente**: não tem o `info.summary` do padrão.
- `externalDocs`: `description: Especificações da fonte (fonte da verdade)` e
  `url: https://github.com/patrickbrandao/badblock/tree/main/specs/fontes/cgibr`.
- `servers`: os quatro do padrão, com a porta 8101. As descrições dizem
  `Produção, v1 fixa (só rotas de dados)`,
  `Desenvolvimento local, versão atual (porta API_CGIBR_HOST_PORT no loopback)`
  e `Desenvolvimento local, v1 fixa (só rotas de dados)`; nos paths fora do
  versionamento, `Produção (fora do versionamento)` e
  `Desenvolvimento local (fora do versionamento)`.
- `tags`: `dados` (consultas aos dados do NIC.br, cacheadas no Valkey e com
  ETag), `meta` (índice, estado dos dados e o manifesto) e `saúde`
  (verificações de saúde, fora do versionamento).
- `paths`: um por rota, com o `operationId`, a tag, as respostas e os
  exemplos de [api-rotas.md](api-rotas.md).
- `components.parameters`: `ASN` (texto, `pattern: '^([Aa][Ss])?[0-9]+$'`,
  exemplo `AS61613`), `IP` e `PrefixIP` (texto, `anyOf` de `format: ipv4` e
  `format: ipv6`; exemplos `45.171.61.10` e `200.192.152.9`), `PrefixLength`
  (inteiro de 0 a 128, exemplo `24`), `Document` (texto, exemplo
  `'08030063000100'`) e `IfNoneMatch` (cabeçalho).
- `components.headers`: `ETag`, `CacheControlPublic`
  (`const: public, max-age=300`), `CacheControlNoStore` (`const: no-store`),
  `XCache` (`enum: [HIT, MISS, BYPASS]`; o 304 sai sempre com `HIT`) e
  `XDatasetVersion` (`format: uuid`).
- `components.responses`: `NotModified`; `BadRequest` e `NotFound`, cada um
  com os exemplos `asn`, `ip`, `prefix` e `document`
  ([mensagens](api-rotas.md#erros)); `DataUnavailable`, o 503
  das rotas de dados, com os exemplos `dataset_not_ready` e
  `database_unavailable` (faz o papel do `ServiceUnavailable` do padrão);
  `DatabaseUnavailable`, o 503 do `/meta`; `Timeout`; `InternalError`;
  `StatusOK` (exemplos `ok`, `starting`, `degraded`) e `StatusError`.
- `components.schemas`: `Dataset`, `ASNRef`, `ASNName`, `Prefixes`,
  `ASNResponse` (`allOf` de `ASNRef` e dos demais campos), `IPResponse`,
  `PrefixResponse`, `DocumentResponse`, `ASNListResponse`, `MetaResponse`
  (`dataset` e `collector` como `oneOf` do objeto ou `null`), `MetaDataset`,
  `MetaCollector`, `Status`, `StatusChecks`, `IndexResponse` e `Error` (`code`
  com `enum` dos seis códigos do padrão). `asn` é `integer`/`int64` de 0 a
  4294967295; timestamps, `format: date-time`; versões, `format: uuid`;
  `source`, `format: uri`; `document_digits`,
  `pattern: '^([0-9]{8}|[0-9]{14})$'`.
- Exemplos: os de [api-rotas.md](api-rotas.md#exemplos); `XDatasetVersion`
  usa `01a0ea50-add2-7bcf-998a-a2e3b04b8cdc`; o de `ETag` e de `IfNoneMatch` é
  `W/"a344bf03cbbadb4a"`, o ETag de `/cgibr/document/08030063000100` no
  dataset `01a0eb55-…`.

ETags reais (`etagFor` em `internal/httpapi/server.go`) no dataset
`01a0eb55-1a1c-7bdb-915a-e885abbaaf72`, vistos em 2026-09-29:

| Consulta | ETag |
|---|---|
| `asn:61613` | `W/"95b0ac1a4da4653c"` |
| `ip:45.171.61.10` | `W/"1469e8c53664ae70"` |
| `prefix:200.192.152.0/24` | `W/"53a5a111c8334474"` |
| `doc:08030063000100` | `W/"a344bf03cbbadb4a"` |
| `asns` | `W/"e7b073256f3130d0"` |

## Opções

As [comuns](../../padroes/api.md#configuração-comum), com os padrões de lá;
não há opção própria, e o único valor desta fonte é `BASE_PATH` = `/cgibr`.
`BASE_PATH` ganha uma barra no começo e perde a do fim (`cgibr` e `/cgibr/`
viram `/cgibr`; `/x/y/` vira `/x/y`).

`api-cgibr --help` (sai com 0; escreve no stderr):

```
api-cgibr — API HTTP dos ASNs e blocos IP brasileiros do NIC.br (registro.br).

Lê as tabelas cgibr_* mantidas pelo collector-cgibr, com cache opcional no
Valkey. Responde tudo abaixo de BASE_PATH (/cgibr): /cgibr/asn/61613 é a versão
atual e /cgibr/v1/asn/61613 fixa a v1.

Uso:
  api-cgibr [opções]
  api-cgibr healthcheck    (usa HTTP_PORT e BASE_PATH; para o HEALTHCHECK do Docker)

Opções (padrão → variável de ambiente → argumento):
  --access-log           registra cada requisição no log
                         env ACCESS_LOG, padrão true
  --base-path            caminho de base: a API responde tudo abaixo dele
                         env BASE_PATH, padrão /cgibr
  --cors-origin          valor de Access-Control-Allow-Origin
                         env CORS_ORIGIN, padrão *
  --dataset-poll         intervalo de conferência da versão dos dados
                         env DATASET_POLL, padrão 30s
  --db-pool-max          conexões máximas no pool do Postgres
                         env DB_POOL_MAX, padrão 10
  --db-timeout           tempo máximo de cada consulta
                         env DB_TIMEOUT, padrão 5s
  --http-port            porta HTTP
                         env HTTP_PORT, padrão 8080
  --log-format           json ou text
                         env LOG_FORMAT, padrão json
  --log-level            debug, info, warn ou error
                         env LOG_LEVEL, padrão info
  --postgres-url         URL do Postgres, ex.: postgres://postgres:senha@badblock-postgres:5432/badblock (obrigatória)
                         env POSTGRES_URL, padrão ""
  --real-ip-headers      ordem de leitura dos headers de IP real
                         env REAL_IP_HEADERS, padrão X-Forwarded-For,X-Real-IP
  --redis-cache-enabled  liga ou desliga o cache
                         env REDIS_CACHE_ENABLED, padrão true
  --redis-key-ttl        validade das chaves de cache, em segundos
                         env REDIS_KEY_TTL, padrão 3600
  --redis-timeout        tempo máximo de cada operação no cache (fail-open)
                         env REDIS_TIMEOUT, padrão 50ms
  --redis-url            URL do Valkey/Redis, ex.: redis://:senha@badblock-valkey:6379/0 (vazio desliga o cache)
                         env REDIS_URL, padrão ""
  --trusted-proxies      faixas cujos X-Forwarded-For/X-Real-IP são aceitos
                         env TRUSTED_PROXIES, padrão 127.0.0.0/8,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,fc00::/7
  --version              mostra a versão e sai
  -h, --help             mostra esta ajuda
```

Opções inválidas (stderr `api-cgibr: <mensagem>`, saída 2):

| Caso | Mensagem |
|---|---|
| sem `POSTGRES_URL` (dispensada com `--version`) | `defina POSTGRES_URL (ou --postgres-url)` |
| `BASE_PATH` que fica `/` depois de tirar as barras das pontas, ou com espaço, `{`, `}`, `?` ou `#` | `--base-path inválido: "<valor>" (ex.: /cgibr)` |
| `REDIS_CACHE_ENABLED` ou `ACCESS_LOG` que `strconv.ParseBool` não lê | `--redis-cache-enabled inválido: "<valor>"` (idem `--access-log`) |
| `REDIS_KEY_TTL` não inteiro ou ≤ 0 | `--redis-key-ttl inválido: "<valor>"` |
| `REDIS_TIMEOUT`, `DB_TIMEOUT` ou `DATASET_POLL` que `time.ParseDuration` não lê (ex.: `5`) ou ≤ 0 | `--redis-timeout inválido: "<valor>"` (idem `--db-timeout`, `--dataset-poll`) |
| `HTTP_PORT` não inteiro ou fora de 1 a 65535 / `DB_POOL_MAX` não inteiro ou < 1 | `--http-port inválido: "<valor>"` / `--db-pool-max inválido: "<valor>"` |
| item de `TRUSTED_PROXIES` sem `/` que não é IP / faixa inválida | `proxy confiável inválido "<item>"` / `faixa de proxy confiável inválida "<item>"` |
| `REAL_IP_HEADERS` com outro cabeçalho | `header de IP real não suportado "<nome>" (use X-Forwarded-For e/ou X-Real-IP)` |
| `LOG_LEVEL` fora de `debug`, `info`, `warn`, `error` / `LOG_FORMAT` fora de `json`, `text` (em qualquer caixa; a mensagem mostra o valor em minúsculas) | `--log-level inválido: "<valor>"` / `--log-format inválido: "<valor>"` |
| argumento que não é opção / opção desconhecida | `argumento inesperado: <arg>` / `flag provided but not defined: -<nome>` (depois da ajuda) |

`POSTGRES_URL` que o pgx não lê não é erro de configuração: o app insiste
pelos 2 minutos do padrão e sai com 1 (`sem conexão com o Postgres`, `err`
`POSTGRES_URL inválida: ...`).

## Compose e `.env`

O `docker-compose.yml` do app é o [modelo](../../plataforma/docker.md#api)
com a porta 8101, e as labels são as de
[../../plataforma/publicacao.md](../../plataforma/publicacao.md#traefik)
(regra `Host(api.badblock.net.br) && (Path(/cgibr) || PathPrefix(/cgibr/))`).

| Variável no `.env` | Vira | Padrão no compose | `.env.example` do app | `.env.example` da raiz |
|---|---|---|---|---|
| `API_CGIBR_TAG` | tag da imagem e `VERSION` do build | `latest` (`dev` no build) | `latest` | `latest` |
| `API_CGIBR_HOST_PORT` | porta no loopback do host | `8101` | `8101` | `8101` |
| `API_CGIBR_CACHE_TTL` | `REDIS_KEY_TTL` | `3600` | `3600` | `3600` |
| `API_CGIBR_CACHE_ENABLED` | `REDIS_CACHE_ENABLED` | `true` | — | — |
| `API_CGIBR_DB_POOL_MAX` | `DB_POOL_MAX` | `10` | — | — |
| `API_CGIBR_REDIS_URL` | `REDIS_URL` | `redis://:${VALKEY_PASSWORD:-}@badblock-valkey:6379/0` | comentada | — |

Fixos no compose: `HTTP_PORT` `8080`, `BASE_PATH` `/cgibr` e `LOG_FORMAT`
`json`; comuns: `POSTGRES_URL` (montada com `POSTGRES_PASSWORD` se ausente),
`TRUSTED_PROXIES` (de `API_TRUSTED_PROXIES`) e `LOG_LEVEL`. `REDIS_TIMEOUT`,
`DB_TIMEOUT`, `DATASET_POLL`, `CORS_ORIGIN`, `ACCESS_LOG` e `REAL_IP_HEADERS`
não passam pelo compose (valem os padrões do app). **Pendente**: o
`.env.example` do app não traz os opcionais comentados do
[modelo](../../plataforma/docker.md#envexample-do-app)
(`API_CGIBR_CACHE_ENABLED`, `API_CGIBR_DB_POOL_MAX`, `API_TRUSTED_PROXIES`).

## Operação

```bash
curl http://127.0.0.1:8101/cgibr/status          # sem Traefik, pela porta do loopback
curl https://api.badblock.net.br/cgibr/asn/61613
make -C apps/cgibr/api smoke                      # com o stack no ar (make up na raiz)
make -C apps/cgibr/api logs
```

`make smoke` faz `curl -fsS` em `/cgibr/status`, `/cgibr/asn/61613` e
`/cgibr/openapi.yaml` na porta `${API_CGIBR_HOST_PORT:-8101}`. **Pendente**
(o [padrão](../../plataforma/docker.md#makefile-dos-apps) pede): o `Makefile`
não tem `SOURCE`, `PORT` nem `SMOKE_ASN` (o AS61613 está fixo no alvo) e o
`smoke` não consulta `/cgibr/meta`.

Logs (JSON, `app=api-cgibr` em toda linha):

| Mensagem | Nível | Campos |
|---|---|---|
| `iniciando` | info | `version`, `commit`, `port`, `base_path`, `postgres` (sem a senha), `cache` (`true` com `REDIS_URL` e `REDIS_CACHE_ENABLED`) |
| `Postgres indisponível; tentando de novo` / `sem conexão com o Postgres` | warn / error | `err` |
| `REDIS_URL inválida` | error | `err` (saída 2) |
| `Valkey indisponível no boot; seguindo sem cache até ele voltar` | warn | `err` |
| `não consegui ler a versão dos dados` | warn | `err` (a leitura da subida) |
| `versão do dataset` | info | `version`, `previous`, `applied_at` (quando a versão muda, inclusive na primeira leitura com dados) |
| `não consegui ler a versão do dataset` | warn | `err` (a releitura, com prazo de 5 s) |
| `ouvindo` | info | `addr` |
| `http` | info | os do [padrão](../../padroes/api.md#ip-real-e-log-de-acesso) |
| `consulta lenta` / `falha na consulta` | warn / error | `path`, `err` |
| `panic` | error | `path`, `panic` |
| `servidor HTTP parou` / `shutdown` | error | `err` (saída 1) |
| `encerrando` | info | — |

Outros valores do código: pool pgx com verificação de saúde a cada 30 s;
cliente Valkey com conexão em max(4 × `REDIS_TIMEOUT`, 200 ms), leitura,
escrita e espera no pool em `REDIS_TIMEOUT`, sem novas tentativas.

## Medições

Em 2026-09-29, no stack local (`make up`), com o dataset `01a0eb55-…` (9.135
ASNs), por `curl` na porta do loopback:

| Rota | Tamanho da resposta | Tempo |
|---|---|---|
| `/cgibr/asns` | 989.154 bytes | 28 ms no `MISS`; 5 a 15 ms no `HIT` |
| `/asn`, `/ip`, `/prefix`, `/document` de um registro | 265 a 1.481 bytes (`/asn/1916`, com 39 blocos: 1.002; `/document/10996639`, com 24 ASNs: 1.481) | 2,4 a 4,4 ms no `MISS`; 1,2 a 2,4 ms no `HIT` |
| `/cgibr/meta` e `/cgibr/status` (sem cache) | 480 e 156 bytes | ~1,4 ms |

O manifesto tem ~43 KB. Não há tempos com o arquivo real carregado num banco
descartável: o app não tem `make test-real`.

## Testes específicos

Unitários (`make test`), com o store falso de `internal/httpapi/httpapi_test.go`
(AS61613 com os três blocos; AS275689 com `10996639`; versão `0192-v1`;
`BASE_PATH` `/cgibr/`, que vira `/cgibr`):

| Pacote | Teste | Confere |
|---|---|---|
| `httpapi` | `TestASN` | `61613`, `/v1`, `AS61613` e `as61613`: 200, `cnpj`, 2 + 1 blocos, `dataset.version` |
| `httpapi` | `TestErrors` | 404 (`AS1`, `8.8.8.8`, `11111111`, `/cgibr/v2/...`, `/cgibr/nada`, fora do caminho de base) e 400 (`abc`, `4294967296`, `999.1.1.1`, `/33`, `123`), sempre JSON |
| `httpapi` | `TestIPAndPrefix` | IPv4 e IPv6; bits de host zerados (`45.171.61.1/24` → `45.171.61.0/24`); `exact` falso e verdadeiro |
| `httpapi` | `TestDocumentAcceptsFormatted` | só dígitos e `08.030.063%2F0001-00` |
| `httpapi` | `TestASNsList` | `count` 2 e `foreign` para 8 dígitos |
| `httpapi` | `TestCacheAndETag` | `MISS` e depois `HIT` pela `/v1` (uma consulta só, uma chave com o prefixo `badblock:api-cgibr:0192-v1:`), `X-Dataset-Version`, 304 com `If-None-Match` |
| `httpapi` | `TestNotReady` | 503 `dataset_not_ready` e `/status` `starting` |
| `httpapi` | `TestHealth` | `GET` e `POST /health` `ok`, `/ping` `pong`, Postgres fora → 503 com `success: false` |
| `httpapi` | `TestIndexMetaAndRedirect` | índice com e sem `/v1`, 301 de `/cgibr`, `/meta` com `dataset` e `collector` |
| `httpapi` | `TestOpenAPIHeaderAndServers`, `TestOpenAPICoversRoutes`, `TestOpenAPIOperationIDsUnique`, `TestOpenAPIRefsResolve`, `TestOpenAPIRoute` (`openapi_test.go`) | `openapi: 3.1.`; os quatro `servers` (porta 8101 fixa no teste); rotas × paths, sem `$ref` direto num path; `operationId` único; `$ref` resolvido; `/openapi.yaml` serve o embutido com `Cache-Control`, é 404 em `/v1` e está no índice |
| `cache` | `TestBreakerOpensAndCloses`, `TestNoop` | disjuntor (3 falhas no teste) e o cache desligado |
| `config` | `TestDefaults`, `TestBasePathNormalized`, `TestPrecedence`, `TestInvalid` | `/cgibr`, 8080, 1 h, 50 ms; normalização do caminho; argumento > ambiente > padrão; sem `POSTGRES_URL`, `BASE_PATH=/`, porta 0, TTL −1, proxy `lixo`, formato `xml` |
| `realip` | `TestClientIP`, `TestNewRejectsBadInput` | direto, via Traefik, `X-Forwarded-For` forjado à esquerda, `X-Real-IP` de reserva, `X-Forwarded-For` inválido, IPv4 mapeado; `/33` e `Forwarded` recusados |

Integração (`make test-int`): `TestQueries` em `internal/store`, num
`postgres:18-trixie` descartável (banco `badblock`, usuário `postgres`, senha
`pg`) com só o `migrate:up` de `central/` e `cgibr/` e esta carga: AS61613
(3 blocos), AS262287 (`45.171.62.0/24`, dentro do `/22` do AS61613), AS275689
e AS275690 (`10996639`), uma execução recusada e uma aplicada, e a linha de
`jobs`. Confere a execução aplicada (a recusada não conta), `jobs`, os blocos
do AS61613 em ordem (`45.171.60.0/22`, `200.192.152.0/22`, `2804:5964::/32`),
o bloco mais específico (`45.171.62.10/32` → AS262287), IPv6, IP fora
(`8.8.8.8`), os dois ASNs do `10996639` e a lista inteira em ordem.

**Pendente** — o que o [padrão](../../padroes/api.md#testes) pede e ainda
falta: em `httpapi`, `HEAD`, erro de banco (503), timeout (504), panic (500),
`/status` `degraded` e CORS (`OPTIONS`); um teste do `dataset`; o uso de
índice no teste de integração; no `openapi_test.go`, tabulação, `info.title`,
a porta lida do `PORT` do `Makefile`, `operationId` ausente,
`HEAD /openapi.yaml` e as rotas do índice × manifesto
([padrão](../../padroes/openapi.md#teste)).
