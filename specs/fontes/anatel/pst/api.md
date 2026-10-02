# anatel/pst — API (`api-anatel-pst`)

O que a `api-anatel-pst` faz de diferente ou a mais que o
[padrão das APIs](../../../padroes/api.md) e o
[padrão do manifesto](../../../padroes/openapi.md). O resto — `main.go`,
caminho de base e versões, formato, versão do dataset, `serveCached`, ETag,
cabeçalhos, saúde, erros, IP real, configuração comum, container — segue os
padrões. Cada rota, com validações, campos, exemplos reais e mensagens de
erro: [api-rotas.md](api-rotas.md). Tabelas e o SQL das consultas:
[dados.md](dados.md#consultas-da-api-anatel-pst); de onde vêm os campos:
[fonte.md](fonte.md). Dono: sub-agente `api-anatel-pst`.

## Valores desta fonte

É uma fonte de dois níveis
([estrutura.md](../../../projeto/estrutura.md#fonte-de-dois-níveis-siteconjunto)):
`anatel/pst` no caminho, `anatel-pst` no nome, `ANATEL_PST` nas variáveis.

| Item | Valor |
|---|---|
| App, binário | `api-anatel-pst` (`/api-anatel-pst` na imagem) |
| Module Go | `github.com/patrickbrandao/badblock/apps/anatel/pst/api` |
| Imagem, container | `tmsoftbrasil/badblock-api-anatel-pst`, `badblock-api-anatel-pst` |
| Descrição da imagem (`org.opencontainers.image.description`) | `API HTTP das prestadoras de serviços de telecomunicações da Anatel (CNPJ, serviços notificados, outorgas e Fistel), do BadBlock` |
| Caminho de base (`BASE_PATH`) | `/anatel/pst` — `https://api.badblock.net.br/anatel/pst/` |
| Porta local | 8112 (`API_ANATEL_PST_HOST_PORT`, só no loopback) |
| Lê | `anatel_pst_run`, `anatel_pst_provider`, `anatel_pst_service` e a linha `collector-anatel-pst` de `jobs` ([dados.md](dados.md#consultas-da-api-anatel-pst)) |
| `source` do índice | `https://www.anatel.gov.br/dadosabertos/paineis_de_dados/outorga_e_licenciamento/prestadoras_servicos_telecomunicacoes.zip` (constante `SourceURL`; não vem do banco) |
| Chave no Valkey | `badblock:api-anatel-pst:<versão>:<consulta>` ([abaixo](#cache-e-etag)) |
| Cliente do Valkey | `ClientName` = `badblock-api-anatel-pst` |
| Cabeçalho `Server` | `badblock-api-anatel-pst/<versão>` |
| Pool pgx | `application_name = api-anatel-pst` e **`plan_cache_mode = force_custom_plan`** ([abaixo](#consultas)), cada um só quando a `POSTGRES_URL` não traz outro valor |
| Limites | `/search`: termo de 3 a 100 caracteres, `limit` de 1 a 100 (padrão 100); `/service`: lista completa, sem limite (o maior, `045`, tem 2,7 MB) |
| Pacote a mais | `internal/testdb` ([testes](#testes)) |
| Arquivo real nos testes | `make test-real` (baixa o ZIP do dia, ou `FILE=...`); `make test-int` roda o mesmo teste com a fixture do coletor |
| Traefik | router e serviço `badblock-api-anatel-pst`, middleware `badblock-api-anatel-pst-ratelimit`, regra `Host(api.badblock.net.br) && (Path(/anatel/pst) \|\| PathPrefix(/anatel/pst/))` ([molde](../../../plataforma/publicacao.md#traefik)) |

Pacotes: os do padrão (sem `internal/rir`) mais `internal/testdb`. Em
`internal/httpapi`: `server.go` (`Handler`, middleware, `serveCached`,
`etagFor`, erros), `handlers.go` (índice, `/meta`, saúde, manifesto e a
tabela `dataRoutes`), `handlers_provider.go` (`/provider`, `/search` e as
validações de CNPJ, termo e `limit`, com os limites `SearchMinLen` = 3,
`SearchMaxLen` = 100 e `SearchLimit` = 100), `handlers_service.go`
(`/services`, `/service` e as validações de código e UF) e `types.go` (os
tipos do JSON). `internal/store` tem as consultas e `LikePattern`.

O `--help` começa assim (depois vêm o uso — `api-anatel-pst [opções]` e
`api-anatel-pst healthcheck` — e as opções do padrão, em ordem alfabética,
cada uma com a variável e o padrão):

```
api-anatel-pst — API HTTP das prestadoras de serviços de telecomunicações da
Anatel: CNPJ, serviços notificados (SCM, STFC, SMP, SeAC...), outorgas e Fistel.

Lê as tabelas anatel_pst_* mantidas pelo collector-anatel-pst, com cache opcional
no Valkey. Responde tudo abaixo de BASE_PATH (/anatel/pst):
/anatel/pst/provider/02558157000162 é a versão atual e
/anatel/pst/v1/provider/02558157000162 fixa a v1.
```

## Rotas

Todas abaixo de `/anatel/pst`. As de dados, o índice e `/meta` respondem
também em `/anatel/pst/v1/...`; saúde, `ping` e o manifesto existem só sem
versão ([padrão](../../../padroes/api.md#caminho-de-base-e-versões)). Estilo
das outras APIs: plural para a lista, singular para um item.

| Rota | `operationId` | Tag | Chave de cache |
|---|---|---|---|
| `GET /anatel/pst/provider/{cnpj}` | `getProvider` | `dados` | `provider:<14 dígitos>` |
| `GET /anatel/pst/services` | `listServices` | `dados` | `services` |
| `GET /anatel/pst/service/{code}` | `getService` | `dados` | `service:<código>` ou `service:<código>:<UF>` |
| `GET /anatel/pst/search?q={texto}[&limit=N]` | `searchProviders` | `dados` | `search:<limit>:<termo>` |
| `GET /anatel/pst/meta` | `getMeta` | `meta` | — (sem cache) |
| `GET /anatel/pst/` | `getIndex` | `meta` | — |
| `GET`, `POST /anatel/pst/health` | `getHealth`, `postHealth` | `saúde` | — |
| `GET`, `POST /anatel/pst/status` | `getStatus`, `postStatus` | `saúde` | — |
| `GET /anatel/pst/ping` | `ping` | `saúde` | — |
| `GET /anatel/pst/openapi.yaml` | `getOpenAPI` | `meta` | — |

### Rota de dados nova

1. Esta spec e [api-rotas.md](api-rotas.md), com exemplo real, e o path no
   manifesto (o `openapi_test.go` falha sem ele).
2. A consulta em `internal/store` (método novo no `Store` e na interface
   `Store` de `server.go`), com o caso no `TestQueries`; consulta nova vai
   também para [dados.md](dados.md#consultas-da-api-anatel-pst) (pedido ao
   coletor).
3. O handler em `handlers_<assunto>.go`, validando antes de tudo e chamando
   `serveCached` com a chave normalizada; os tipos em `types.go`, com o
   bloco `dataset`.
4. Uma linha em `dataRoutes()` (`handlers.go`): registra a rota com e sem
   `/v1` e a põe no `endpoints` do índice, na ordem da tabela.
5. O caso no store falso e nos testes de `httpapi`, e a rota no
   `TestRealFile`.

## Dados servidos

- Só pessoas jurídicas: as linhas de CPF da fonte não estão no banco
  ([fonte.md](fonte.md#linhas)). O mesmo CNPJ pode ter várias outorgas (SIC
  e SIR) e várias notificações do mesmo código.
- Textos como a Anatel publica, sem espaços nas pontas: nomes de serviço ora
  em maiúsculas (`SERVIÇO MOVEL PESSOAL`), ora não. Ausentes na fonte
  (`N/I`, `N/A`, `-`, vazio) saem `null` ([fonte.md](fonte.md#leitura)).
- Datas da fonte (`notified_on`, `granted_on`) como `AAAA-MM-DD`;
  `csv_modified_at` do `/meta` em UTC (a Anatel publica a hora de Brasília).
- `first_seen` e `updated_at` de `/provider` são o `created_at` e o
  `updated_at` de `anatel_pst_provider`: datas **deste banco**, desde a
  primeira carga, não datas da Anatel.
- Telefone e e-mail saem como publicados (dados cadastrais públicos de
  pessoas jurídicas).
- **Ordem**: a do banco, sem reordenar na API — serviços de uma prestadora
  por `service_code, notified_on, notification_fistel`; `/services` por
  código; `/service` por CNPJ; `/search` por `name, document` com a
  collation do banco (`en_US.utf8`, a da imagem do Postgres), que ignora a
  caixa e pesa pouco a pontuação: `Cunha Instalacoes…` vem antes de
  `LVS…` e de `TELEFONICA…`. Duas linhas com o mesmo código, a mesma data
  e o mesmo Fistel (a mesma notificação sob duas outorgas) saem em ordem
  indefinida entre si.
- `service_codes` de `/provider` são os códigos distintos dos `services`, em
  ordem (calculados na API).

## Cache e ETag

Toda rota de dados passa pelo `serveCached` do
[padrão](../../../padroes/api.md#cache-valkey-cache-aside-e-servecached). A
chave é a consulta **normalizada**, e a consulta ao banco usa o mesmo
valor:

| Pedido | Chave | Normalização |
|---|---|---|
| `/provider/02558157000162`, `/provider/02.558.157%2F0001-62`, `/v1/provider/...` | `provider:02558157000162` | só os dígitos ASCII |
| `/services`, `/v1/services` | `services` | — |
| `/service/45`, `/service/045`, `/service/045?state=` | `service:045` | zeros à esquerda até 3 dígitos; `state` vazio = sem filtro |
| `/service/45?state=rr` | `service:045:RR` | UF em maiúsculas |
| `/search?q=%20Telefonica`, `/search?q=telefonica&limit=100` | `search:100:telefonica` | espaços colapsados, minúsculas; `limit` ausente = 100, sem zeros à esquerda (`007` = 7) |

- Com a versão dos exemplos (`01a0f518-b4d2-7810-80b5-b58a6c2e4614`), a
  chave de `/anatel/pst/provider/02558157000162` é
  `badblock:api-anatel-pst:01a0f518-b4d2-7810-80b5-b58a6c2e4614:provider:02558157000162`
  e o ETag, `W/"7b5f3c4da8ce91e8"` (`etagFor`; o hex sai sem zeros à
  esquerda). Outros: `services` → `W/"8e6e540238112467"`, `service:045` →
  `W/"db1be8c5a42ed6df"`, `service:045:RR` → `W/"b4897f8aa1a806ff"`,
  `search:100:telefonica` → `W/"c9fcc4ac2ef1ece"`, `search:3:telecom` →
  `W/"5f469ae36664060f"` (o `TestCacheAndETag` confere os seis).
- Um arquivo novo (a Anatel regera o ZIP uma vez por dia) é uma versão nova:
  ETags e chaves novos, e um `If-None-Match` antigo recebe 200 com o corpo
  novo. O 304 sai com `X-Cache: HIT`.
- `/service` com UF sem prestadora e `/search` sem resultado são 200 (lista
  vazia) e entram no cache; 404, 400 e erros não.
- Limite do corpo guardado: 8 MiB (`maxCachedBody = 8 << 20`). A maior
  resposta, `/service/045`, tem 2.730.896 bytes (33% do limite); a seguinte,
  `/service/019`, 1.340.704 bytes; o maior `/provider`, 28.983 bytes.
  `TestRealFile` falha se algum `/service` passar de 4 MiB (metade do
  limite).

## Consultas

O SQL que [dados.md](dados.md#consultas-da-api-anatel-pst) lista (dono: o
coletor) e o que é da API:

- **`plan_cache_mode = force_custom_plan`** no pool, como na `api-asnames`
  ([asnames](../../asnames/api.md#consultas-e-planos)) — difere do padrão,
  que não mexe no planejador. O pgx prepara as consultas e, depois de 5
  execuções, o Postgres pode trocar para um plano genérico, que não vê o
  termo do `ILIKE` (deixaria de usar os índices trigram) nem o filtro de UF
  de `/service`. `store.Open` só põe o parâmetro se a `POSTGRES_URL` não
  trouxer outro. `TestRealFile` repete uma busca rara 12 vezes e falha,
  com o arquivo inteiro, se alguma passar de 25 ms.
- `/provider`: duas consultas — a linha de `anatel_pst_provider` (404 se não
  existe, sem a outra) e os serviços pelo `provider_uuid` — numa transação
  `REPEATABLE READ` só de leitura, para as duas verem o mesmo arquivo mesmo
  se o coletor aplicar outro no meio (a aplicação é uma transação só).
- `/services`: **difere de dados.md**, que agrupa por
  `service_code, service_name, service_group`. A API agrupa só pelo código
  e lê o nome e o grupo com `min()`:
  `SELECT service_code, min(service_name), min(service_group), count(DISTINCT provider_uuid), count(*) FROM anatel_pst_service GROUP BY service_code ORDER BY service_code`.
  O resultado é o mesmo (cada código tem um nome e um grupo só —
  [fonte.md](fonte.md#fatos-medidos-2026-09-30)), um item por código fica
  garantido (como em `/service`) e o planejador percorre
  `ix_anatel_pst_service_service_code` em ordem, sem a ordenação em disco
  (`external merge`, ~4 MB) do agrupamento por três textos: ~60 ms em vez de
  ~240 ms no arquivo real.
- `/service/{code}`: duas consultas na mesma transação só de leitura — o
  nome e o grupo
  (`SELECT min(service_name), min(service_group) FROM anatel_pst_service WHERE service_code = $1`;
  `NULL` = código fora do arquivo, 404) e as prestadoras de dados.md, com o
  filtro de UF como `AND ($2::text IS NULL OR p.state = $2)` (o plano custom
  descarta o termo sem filtro). As duas usam
  `ix_anatel_pst_service_service_code`; nos códigos grandes (`045`, 20 mil
  prestadoras) o planejador lê a tabela de prestadoras inteira e junta por
  hash, o que é mais barato.
- `/search`: o `SELECT` de dados.md, com o termo de `LikePattern` (`%`, `_`
  e `\` escapados com `\`) e `LIMIT` = `limit + 1` (o item a mais só marca
  `truncated`). O planejador usa os dois índices trigram (`BitmapOr`) e
  ordena os achados; termos comuns (`ltda`, ~31 mil prestadoras) ordenam
  muito e levam 40–60 ms; termo sem letras nem dígitos (`---`) não gera
  trigrama e varre a tabela (40–55 ms, o pior caso).
- `/meta`: a última linha aplicada de `anatel_pst_run` e a linha do coletor
  em `jobs`.

## Configuração

Só as opções comuns do [padrão](../../../padroes/api.md#configuração-comum),
com `BASE_PATH` = `/anatel/pst` (inválido:
`--base-path inválido: "/" (ex.: /anatel/pst)`); nenhuma opção a mais. O
`BASE_PATH` tem dois segmentos: `anatel/pst`, `/anatel/pst/` e `anatel`
são aceitos e normalizados (`/anatel/pst`, `/anatel/pst`, `/anatel`).

- Compose: o [modelo da API](../../../plataforma/docker.md#api) com
  `<FONTE>` = `ANATEL_PST` e a porta 8112 — `API_ANATEL_PST_TAG` (`latest`),
  `API_ANATEL_PST_HOST_PORT` (`8112`), `API_ANATEL_PST_REDIS_URL`
  (`redis://:${VALKEY_PASSWORD:-}@badblock-valkey:6379/0`),
  `API_ANATEL_PST_CACHE_ENABLED` (`true`), `API_ANATEL_PST_CACHE_TTL`
  (`3600`) e `API_ANATEL_PST_DB_POOL_MAX` (`10`), mais as compartilhadas do
  padrão. Projeto compose `badblock-api-anatel-pst`, serviço
  `api-anatel-pst`.
- `.env.example` do app: o [modelo](../../../plataforma/docker.md#envexample-do-app),
  com os opcionais comentados (`API_ANATEL_PST_CACHE_ENABLED`,
  `API_ANATEL_PST_DB_POOL_MAX`, `API_TRUSTED_PROXIES`).
- `.env.example` da raiz: `API_ANATEL_PST_TAG=latest` (imagens),
  `API_ANATEL_PST_HOST_PORT=8112` e `API_ANATEL_PST_CACHE_TTL=3600` (APIs).
- `Makefile`: `ROOT := ../../../..` (o app está um nível mais fundo que os
  das fontes de um nível), `SOURCE := anatel/pst`,
  `PORT := $${API_ANATEL_PST_HOST_PORT:-8112}` (o teste do manifesto lê
  este `PORT`), `SMOKE_CNPJ ?= 02558157000162` e `SOURCE_URL` (a do
  `test-real`).

## Operação

```bash
make -C apps/anatel/pst/api up      # só este serviço, com o .env da raiz (o stack inteiro: make up na raiz)
make -C apps/anatel/pst/api smoke   # com o stack no ar
curl http://127.0.0.1:8112/anatel/pst/provider/02558157000162
```

`make smoke` faz `curl -fsS` em `http://127.0.0.1:$(PORT)` nas rotas
`/anatel/pst/status`, `/anatel/pst/meta`, `/anatel/pst/services` (corpo
descartado), `/anatel/pst/provider/$(SMOKE_CNPJ)` e
`/anatel/pst/openapi.yaml` (corpo descartado).

## Manifesto OpenAPI

`openapi/openapi.yaml` segue [openapi.md](../../../padroes/openapi.md), com os
exemplos e as mensagens de [api-rotas.md](api-rotas.md):

- `info`: `title: api-anatel-pst`, `version: '0.1.0'`, `summary`,
  `description` (os dados, só pessoas jurídicas, ausentes como `null`, datas
  deste banco, versões como servidores, `HEAD`/`OPTIONS`, redirect,
  cabeçalhos comuns, erros e validação antes de tudo) e `license` MIT;
  `externalDocs` para
  `https://github.com/patrickbrandao/badblock/tree/main/specs/fontes/anatel/pst`.
- `servers`: os quatro do padrão, com a porta 8112 e as descrições do
  padrão; nos paths fora do versionamento, `Produção` e
  `Desenvolvimento local`.
- `tags`: `dados` (consultas, com ETag e Valkey), `meta` (índice, estado e
  manifesto), `saúde`.

| Path | `operationId` | Tag | Respostas |
|---|---|---|---|
| `/` | `getIndex` | `meta` | 200 `IndexResponse`, 500 |
| `/provider/{cnpj}` | `getProvider` | `dados` | 200 `ProviderResponse` (exemplo `telefonica`, um trecho), 304, 400, 404, 500, 503, 504 |
| `/services` | `listServices` | `dados` | 200 `ServicesResponse` (`trecho`), 304, 500, 503, 504 — sem 400 nem 404 |
| `/service/{code}` | `getService` | `dados` | 200 `ServiceResponse` (`scm` e `scm_rr`, trechos), 304, 400, 404, 500, 503, 504 |
| `/search` | `searchProviders` | `dados` | 200 `SearchResponse` (`telefonica`, um trecho, e `limite`), 304, 400, 500, 503, 504 — sem 404 |
| `/meta` | `getMeta` | `meta` | 200 `MetaResponse` (`carregado` e `antes_da_primeira_carga`), 500, 503 `DatabaseUnavailable`, 504 |
| `/health`, `/status` | `getHealth`, `postHealth`, `getStatus`, `postStatus` | `saúde` | 200 `StatusOK`, 500, 503 `StatusError` |
| `/ping` | `ping` | `saúde` | 200 `text/plain`, `const: pong` |
| `/openapi.yaml` | `getOpenAPI` | `meta` | 200 `application/yaml` |

- `components.parameters`: `CNPJ` (exemplos com e sem pontuação),
  `ServiceCode` (`pattern: '^[0-9]{1,3}$'`), `State`
  (`'^[A-Za-z]{2}$'`), `SearchQuery` (`minLength: 3`, `maxLength: 100`),
  `SearchLimit` (inteiro de 1 a 100, `default: 100`) e `IfNoneMatch`.
- `components.responses`: os do padrão; `BadRequest` com um exemplo por
  validação (`cnpj`, `code`, `state`, `q`, `limit`), `NotFound` com
  `provider` e `service`, `ServiceUnavailable` com `dataset_not_ready` e
  `database_unavailable`.
- `components.schemas`: `Dataset`, `ProviderResponse`, `Address`, `Service`,
  `ServicesResponse`, `ServiceSummary`, `ServiceResponse`, `SearchResponse`,
  `ProviderBrief`, `MetaResponse`, `MetaDataset`, `Collector`, `Status`,
  `StatusChecks`, `IndexResponse` e `Error`, com os campos de
  [api-rotas.md](api-rotas.md): `document` `'^[0-9]{14}$'`; `service_code`
  `'^[0-9]{3}$'`; Fistéis `'^[0-9]{11}$'`; `state` `'^[A-Z]{2}$'` ou `null`;
  `city_ibge_code` de 1000000 a 9999999 ou `null`; datas `format: date`.
- ETag de exemplo (`IfNoneMatch` e `components.headers.ETag`): o de
  `/anatel/pst/provider/02558157000162` na versão dos exemplos,
  `W/"7b5f3c4da8ce91e8"`.
- `source` do exemplo `carregado` de `/meta` é a URL da Anatel; na medição
  o mesmo ZIP foi servido por uma cópia local (o resto da resposta é o
  real).

## Medições

Arquivo de 2026-09-30 (ZIP com 14.734.094 bytes, 45.074 prestadoras, 54.314
serviços, 55 códigos), medido em 2026-09-30 no `TestRealFile`
(`make test-real FILE=...`, PG18 local via testcontainers, handler e store
reais, sem cache) e com o binário no ar (`curl` no loopback, com Valkey).

| O quê | Valor |
|---|---|
| carga pelo coletor (`--once`, arquivo servido localmente) | ~4 s |
| `/provider/{cnpj}`, cada um dos 45.074 (duas consultas numa transação) | ~0,85–1,1 ms em média (38–48 s os 45.074), o mais lento 14–21 ms; pelo `curl`, ~6 ms `MISS` e ~1,3 ms `HIT` |
| maior `/provider` | `07756651000155` (Brasil Tecpar): 72 serviços, 28.983 bytes; `02558157000162` (Telefônica): 42 serviços, 17.370 bytes, ~1 ms |
| `/services` | 55 códigos, 8.512 bytes, 30–65 ms sem cache (~240 ms com o agrupamento de dados.md); ~1,5 ms `HIT` |
| `/service/045` (SCM) | 20.510 prestadoras, 2.730.896 bytes, 70–80 ms sem cache; ~120 ms `MISS` e ~22 ms `HIT` pelo `curl` |
| `/service/045?state=SP`, `?state=RR` | 454.373 bytes ~30 ms; 88 prestadoras, 11.627 bytes, ~13 ms |
| `/service` dos outros 54 códigos | `019` (10.076 prestadoras) com 1.340.704 bytes e ~45 ms, `507` com 972.408 bytes; os demais abaixo disso |
| `/search`, termo raro (`telefonica`, `100%`, `são paulo`, `zzqqxx`) | 0,4–2 ms, pelos índices trigram |
| `/search`, termo comum (`telecom`, `internet`, `fibra`) | 3–14 ms, cortado em 100 |
| `/search?q=ltda` (~31 mil achados), `?q=---` (sem trigrama) | 40–60 ms (o pior caso) |
| `/meta` | 588 bytes, ~1 ms |

`TestRealFile` falha, só com o arquivo inteiro, se algum `/provider` passar
de 100 ms, `/services` de 500 ms, algum `/service` de 1,5 s, alguma busca de
250 ms ou a busca rara repetida de 25 ms; com qualquer arquivo, se algum
`/service` passar de 4 MiB.

## Testes

Camadas e comandos: [padrão](../../../padroes/api.md#testes) e
[testes.md](../../../processos/testes.md). O que é desta API:

| Pacote | Testes | O quê |
|---|---|---|
| `httpapi` | `TestProvider`, `TestServices`, `TestService`, `TestSearch`, `TestNormalizers` | store falso com um recorte real de 2026-09-30 (Telefônica, Copa Energia com duas outorgas SIR e a mesma notificação, Transat com uma notificação dispensada): CNPJ só com dígitos, formatado com `%2F`, com lixo em volta e `/v1` numa chave só (uma consulta); formato do JSON (endereço agrupado, `null`, datas, `service_codes` distintos e ordenados); dispensada com a outorga `null`; catálogo com as contagens; código `45`/`045`/`/v1` numa chave, UF em qualquer caixa numa chave por UF, UF sem prestadora → 200 `[]`; busca normalizada (pontas, internos, tab, maiúsculas, `q` repetido, `limit` vazio = 100, `0100` = 100), curingas literais até o store, corte em `limit` (`truncated`), `limit` menor; validadores de código, UF, `limit`, termo e `onlyDigits` (dígitos não ASCII não contam) |
| `httpapi` | `TestErrors`, `TestDatabaseErrors`, `TestPanicIsJSON500`, `TestNotReady` | 400 de cada validação (13 e 15 dígitos, 8 dígitos, `0045`, `4a`, `SPA`, `q` curto/longo/vazio/NUL/UTF-8 inválido, `limit` 0/101/-1/`x`) e 404 (CNPJ e código fora do arquivo, barra sem `%2F`, rotas e métodos inexistentes, barra no fim), com as mensagens; 503 `database_unavailable` e 504 `timeout` nas quatro rotas; erro e 404 fora do cache; panic → 500 JSON; `dataset_not_ready` sem consultar o banco, 400 antes do 503, `/status` `starting`, `/meta` com `null` |
| `httpapi` | `TestCacheAndETag`, `TestHEAD`, `TestBypassAndBigBody` | `MISS` → `HIT` com a mesma chave e o mesmo ETag com e sem `/v1`; `If-None-Match` fraco, forte, em lista e `*` → 304 sem corpo nem `Content-Type`, sem consultar; ETag por consulta (e nenhum no 400); os seis ETags dos exemplos; `HEAD` nas rotas de dados, no índice e na meta; `BYPASS` sem cache; corpo acima de 8 MiB (40 mil prestadoras sintéticas) fora do cache |
| `httpapi` | `TestStatus`, `TestCORSAndHeaders`, `TestIndexMetaAndRedirect` | `/health` e `/status` em GET e POST nos quatro estados e com o cache desligado (`disabled`), `pong`, saúde e `ping` 404 em `/v1`; preflight (também fora do caminho de base) e cabeçalhos comuns, inclusive no 404; índice nas duas versões (`endpoints` na ordem), redirect de `/anatel/pst` com e sem query, `/anatel/pst/v1` sem barra, `/meta` com os campos do arquivo, `no-store` e sem `X-Cache` |
| `httpapi` | `TestOpenAPIMatchesRoutes`, `TestOpenAPIDocument`, `TestOpenAPIRoute` | o teste completo do [padrão](../../../padroes/openapi.md#teste): rotas × paths nos dois sentidos, `info.title`, os quatro `servers` com a porta lida do `Makefile`, `servers` próprios, `operationId` um por operação e sem repetição, `$ref` existentes, `GET`/`HEAD` do manifesto, `/v1/openapi.yaml` 404 e o índice sem rota fora do manifesto (a query de `/search?q={texto}` é ignorada na comparação) |
| `dataset` | `TestWatcherRefresh`, `TestWatcherRunStopsWithContext` | versão lida, erro mantém a versão, versão nova, volta a "sem carga"; `Run` para com o contexto |
| `store` | `TestLikePattern` | escape de `%`, `_` e `\` |
| `store` (integração) | `TestQueries` | as consultas no PG18 (abaixo) |
| `httpapi` (integração) | `TestRealFile` | a fonte real, carregada pelo coletor (abaixo) |
| `config` | `TestDefaults`, `TestBasePathNormalized`, `TestPrecedence`, `TestInvalid` | padrões (`/anatel/pst`, 8080, 1 h, 50 ms); `anatel/pst`, `/anatel/pst/`, `anatel` e `/x/y/` normalizados; argumento vence o ambiente; sem `POSTGRES_URL`, `BASE_PATH=/`, porta 0, TTL negativo, proxy inválido e `LOG_FORMAT=xml` recusados |
| `cache` | `TestBreakerOpensAndCloses`, `TestNoop` | disjuntor abre, não chama o Valkey e fecha depois do cooldown; cache desligado |
| `realip` | `TestClientIP`, `TestNewRejectsBadInput` | conexão direta, via Traefik, `X-Forwarded-For` forjado à esquerda, `X-Real-IP` de reserva, `X-Forwarded-For` inválido, IPv4 mapeado; faixa e cabeçalho inválidos |

**`internal/testdb`** (build tag `integration`): `testdb.New(t)` sobe um
`postgres:18-trixie` descartável pelo testcontainers (banco `badblock`,
usuário `postgres`, senha `pg`), aplica o `migrate:up` de cada `.sql` de
`database/postgres/central/` e `database/postgres/anatel_pst/`, em ordem de
nome (a pasta sai da posição do próprio arquivo, seis níveis abaixo da
raiz), e devolve a URL e um pool administrativo; tudo some no fim do teste.

**`TestQueries`**: o banco vazio (sem versão, sem `jobs`, catálogo e busca
vazios, `Provider` e `ServiceProviders` → `ErrNotFound`); depois o recorte
real (Telefônica com seis serviços gravados fora de ordem, Copa Energia,
Transat com a dispensada), três prestadoras artificiais com `%`, `_` e `\`
no nome ou no nome fantasia, duas execuções aplicadas e uma recusada (a mais
nova) e a linha de `jobs`. Confere: a versão é a última aplicada, com os
campos do `/meta`; a prestadora com os serviços em ordem de código, data e
Fistel e a outorga `null` na dispensada; o store não normaliza o CNPJ; o
catálogo com CNPJs distintos e linhas; as prestadoras de um serviço em ordem
de CNPJ, uma vez cada (a Copa tem o `019` duas vezes), com e sem UF, e
`[]` (não `nil`) com UF sem prestadora; código fora do arquivo (`999`, `45`
sem zero) → `ErrNotFound`; a busca pelo nome fantasia, com os curingas
literais (`100%`, `% fibra`, `under_score`, `t_r` — sem o escape casaria
`INTERNET` —, `back\slash`, `\`, `%%%`, `___`), sem olhar a cidade, em
ordem de razão social e com limite; `EXPLAIN` (com `enable_seqscan = off`)
de cada consulta usando o índice dela; sem execução aplicada, a versão
volta a ser nula.

### Fonte real (`TestRealFile`)

```bash
make test-real                          # baixa o ZIP do dia (curl) e roda o teste com tempos no log
make test-real FILE=/tmp/pst.zip        # com um ZIP já baixado
make test-int                           # o mesmo teste com a fixture do coletor, sem rede
```

No molde da `api-rootzone`: compila o coletor irmão
(`go build ./cmd/collector-anatel-pst` em `../collector`), serve o ZIP por
um `httptest.Server` e roda `collector-anatel-pst --once` contra um PG18 do
`testdb` (com a fixture `testdata/pst-sample.zip`, `MIN_PROVIDERS=0`); a
variável é `ANATEL_PST_REAL_FILE` (caminho absoluto; o `make test-real` a
monta a partir de `FILE`). Depois `ANALYZE` e, com o handler e o store
reais e sem cache:

1. `/meta` igual à linha de `anatel_pst_run` (prestadoras, serviços), às
   contagens das tabelas e ao SHA-256 do ZIP.
2. `/services`: um item por código da tabela, em ordem, e a soma de
   `services` igual às linhas de `anatel_pst_service`.
3. `/service` de **todos** os códigos: `count` igual a `providers` do
   catálogo, em ordem de CNPJ; o mais lento e o maior no log; no código com
   mais prestadoras, cada UF da tabela (`?state=` em minúsculas) só com
   prestadoras dela, e a soma das UFs mais as sem UF igual ao total.
4. `/provider` de **todos** os CNPJs: o CNPJ pedido, com serviços e
   `service_codes` em ordem; a soma dos serviços igual às linhas da tabela;
   o tempo total, o mais lento e o maior no log; a Telefônica pelo CNPJ
   formatado; `00000000000000` é 404.
5. `/search` com termos raros, comuns, com curingas, acentos e sem letras:
   cada item contém o termo na razão social ou no nome fantasia; a busca
   rara repetida 12 vezes direto no store.
6. `EXPLAIN` de cada consulta: `uq_anatel_pst_provider_document`,
   `uq_anatel_pst_service_key`, `ix_anatel_pst_service_service_code` (nome
   do serviço, catálogo e prestadoras de um código pequeno) e
   `ix_anatel_pst_provider_name_trgm` (busca rara) — com o arquivo inteiro,
   o planejador escolhe sozinho (e o plano vai para o log); com a fixture,
   com `enable_seqscan = off`.

Rode quando mudar uma consulta, um índice, uma validação ou o formato da
resposta, e compare com as [medições](#medições).

## Mudar a API

- Rota, parâmetro, campo ou resposta: [api-rotas.md](api-rotas.md) e o
  manifesto no mesmo trabalho, depois o código e os testes de `httpapi`
  ([fluxo](../../../processos/fluxo-de-trabalho.md)); rota nova, pelos
  passos de [Rota de dados nova](#rota-de-dados-nova). Mudança incompatível
  no JSON = `/anatel/pst/v2/...`.
- Consulta ou índice novo: o pedido, com a migration proposta, vai ao
  coletor ([dados.md](dados.md#mudando-o-schema)); depois `TestQueries` e
  `TestRealFile`.
- Regra nova do parser ([fonte.md](fonte.md#regras-do-parser-internalparse)):
  os exemplos de [api-rotas.md](api-rotas.md) e o store falso.
- Opção nova: `--help`, `.env.example` do app e da raiz, compose e esta spec
  ([convenções](../../../projeto/convencoes.md#configuração)).
