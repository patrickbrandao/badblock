# asnames — API (`api-asnames`)

O que a `api-asnames` faz de diferente ou a mais que o
[padrão das APIs](../../padroes/api.md). O resto — `main.go`, caminho de base
e versões, formato, versão do dataset, `serveCached`, ETag, cabeçalhos, saúde,
erros, IP real, configuração comum, container — segue o padrão. Cada rota,
com validações, campos, exemplos reais e mensagens de erro:
[api-rotas.md](api-rotas.md). Tabelas e o SQL das consultas:
[dados.md](dados.md); de onde vêm os campos: [fonte.md](fonte.md).

## Valores desta fonte

| Item | Valor |
|---|---|
| App, binário | `api-asnames` (`/api-asnames` na imagem) |
| Module Go | `github.com/patrickbrandao/badblock/apps/asnames/api` |
| Imagem, container | `tmsoftbrasil/badblock-api-asnames`, `badblock-api-asnames` |
| Descrição da imagem (`org.opencontainers.image.description`) | `API HTTP dos nomes e países de todos os ASNs (asn.txt do RIPE NCC), do BadBlock` |
| Caminho de base (`BASE_PATH`) | `/asnames` — `https://api.badblock.net.br/asnames/` |
| Porta local | 8108 (`API_ASNAMES_HOST_PORT`, só no loopback) |
| Lê | `asnames_asn`, `asnames_run` e a linha `collector-asnames` de `jobs` ([dados.md](dados.md#consultas-da-api)) |
| `source` do índice | `https://ftp.ripe.net/ripe/asnames/asn.txt` (constante `SourceURL`) |
| Chave no Valkey | `badblock:api-asnames:<versão>:<consulta>` ([abaixo](#cache-e-etag)) |
| Cliente do Valkey | `ClientName` = `badblock-api-asnames` |
| Cabeçalho `Server` | `badblock-api-asnames/<versão>` |
| Pool pgx | `application_name = api-asnames` e **`plan_cache_mode = force_custom_plan`** ([abaixo](#consultas-e-planos)), cada um só quando a `POSTGRES_URL` não traz outro valor |
| Limites | `/search`: termo de 3 a 100 caracteres, até 100 resultados; `/handle`: até 255 caracteres; `/country`: lista completa, sem limite |
| Pacote a mais | `internal/testdb` ([testes](#testes)) |
| Arquivo real nos testes | variável `ASNAMES_REAL_FILE`, pelo `make test-int` (não há alvo `test-real`) |

Pacotes: os do padrão (sem `internal/rir`) mais `internal/testdb`. Em
`internal/httpapi`: `server.go` (`Handler`, middleware, `serveCached`,
`etagFor`, erros), `handlers.go` (rotas, validações e os limites
`SearchMinLen` = 3, `SearchMaxLen` = 100, `SearchLimit` = 100 e
`HandleMaxLen` = 255) e `types.go` (os tipos do JSON). `internal/store` tem
as consultas de [dados.md](dados.md#consultas-da-api) e `LikePattern`.

O `--help` começa assim (depois vêm o uso — `api-asnames [opções]` e
`api-asnames healthcheck` — e as opções do padrão, em ordem alfabética, cada
uma com a variável e o padrão):

```
api-asnames — API HTTP dos nomes e países de todos os ASNs alocados (asn.txt do
RIPE NCC).

Lê as tabelas asnames_* mantidas pelo collector-asnames, com cache opcional no
Valkey. Responde tudo abaixo de BASE_PATH (/asnames): /asnames/asn/15169 é a
versão atual e /asnames/v1/asn/15169 fixa a v1.
```

## Dados servidos

- `description` é a linha do `asn.txt` depois do número, como publicada: a
  fonte da verdade e o que `/search` procura.
- `handle`, `name` e `country` são derivados pelo coletor
  ([fonte.md](fonte.md#campos-derivados)) e saem `null` quando a linha não os
  traz. Nas 555 linhas ambíguas o handle derivado pode ser só a primeira
  palavra (o AS32381 em [api-rotas.md](api-rotas.md)); por isso a busca usa
  `description`.
- `country` tem duas letras maiúsculas e inclui `EU` e `AP`, códigos
  regionais dos RIRs que não são países ISO 3166.
- O handle **não é único** (`GOOGLE` em 6 ASNs, `VRSN-AC50-340` em 290):
  `/handle` responde sempre uma lista.
- `first_seen` e `updated_at` de `/asn` são o `created_at` e o `updated_at` de
  `asnames_asn`: datas deste banco, não da alocação
  ([dados.md](dados.md#mapeamento-da-fonte-para-as-colunas)).

## Cache e ETag

Toda rota de dados passa pelo `serveCached` do
[padrão](../../padroes/api.md#cache-valkey-cache-aside-e-servecached). A chave
é a consulta **normalizada**, e a consulta ao banco usa **o mesmo valor**: a
chave determina a resposta (a única exceção é a mensagem do 404 de
`/handle`, que não entra no cache — [api-rotas.md](api-rotas.md)).

| Pedido | Chave | Normalização |
|---|---|---|
| `/asn/AS15169` | `asn:15169` | sem o `AS`, decimal sem zeros à esquerda |
| `/country/us` | `country:US` | maiúsculas |
| `/handle/%20Google%20` | `handle:google` | sem os espaços das pontas (os de dentro ficam como vieram), minúsculas |
| `/search?q=%20Google%20%20LLC` | `search:google llc` | sem os espaços das pontas, internos colapsados em um, minúsculas |

- Com a versão dos exemplos, `/asnames/asn/15169` (e `/asnames/v1/asn/AS15169`)
  usa a chave
  `badblock:api-asnames:01a0ea9e-0f44-7227-8b1e-7d4919aae5cf:asn:15169` e o
  ETag `W/"c78f48237c990ec"` (`etagFor`; o hex sai sem zeros à esquerda).
  Outros: `country:US` → `W/"9703d1ade2137414"`, `handle:google` →
  `W/"ca6a76a156910b33"`, `search:google llc` → `W/"a775e6fe37421d68"`.
- Versão nova, ETag novo: um `If-None-Match` antigo recebe 200 com o corpo
  novo. O 304 sai com `X-Cache: HIT`.
- Busca sem resultado é 200 (`asns: []`) e entra no cache; 404 e erros não.
- Limite do corpo guardado: 8 MiB (`maxCachedBody = 8 << 20`). A maior
  resposta, `/country/US`, tem 2.254.782 bytes (27% do limite); as outras
  listas de país ficam abaixo de 700 KB. `TestRealFile` falha se a maior
  passar de 4 MiB (metade do limite).

## Consultas e planos

O SQL de cada rota e o índice que ela usa estão em
[dados.md](dados.md#consultas-da-api) (dono: o coletor). O que é da API:

- **`plan_cache_mode = force_custom_plan`** no pool — difere do padrão, que
  não mexe no planejador. O pgx prepara as consultas e, depois de 5
  execuções, o Postgres pode trocar para um plano genérico, que não enxerga
  o termo da busca: no `ILIKE`, esse plano percorre a tabela inteira pelo
  índice de `asn` mesmo com termo raro (~65 ms em vez de ~1 ms no arquivo
  real). Planejar de novo a cada consulta custa menos de 1 ms. `store.Open`
  só põe o parâmetro se a `POSTGRES_URL` não trouxer outro
  (`?plan_cache_mode=…`). **Não remova sem medir com o arquivo real**:
  `TestRealFile` repete uma busca rara 12 vezes e falha se alguma passar de
  25 ms.
- `/search`: o planejador escolhe sozinho entre o índice trigram
  (`ix_asnames_asn_description_trgm`: termo raro, bitmap e ordenação) e o
  índice de `asn` em ordem, com filtro, parando nos 101 primeiros (termo
  comum). Forçar o trigram (CTE `MATERIALIZED`) deixava os termos comuns
  (~28 mil linhas) 10 vezes mais lentos; por isso a consulta é um `SELECT`
  simples.
- Termo com menos de 3 caracteres não aproveita o trigram (daí o mínimo de
  `/search`); termo sem letras nem dígitos (`---`) não gera trigrama e varre
  a tabela (~60 ms, o pior caso).
- `/country`: `ix_asnames_asn_country`, ou `uq_asnames_asn_asn` em ordem nos
  países muito grandes (`US`, com 26% das linhas). `/handle`:
  `ix_asnames_asn_handle` (`lower(handle)`). `/asn`: `uq_asnames_asn_asn`.

## Configuração

Só as opções comuns do [padrão](../../padroes/api.md#configuração-comum),
com `BASE_PATH` = `/asnames` (inválido: `--base-path inválido: "/" (ex.: /asnames)`);
nenhuma opção a mais.

- Compose: o [modelo da API](../../plataforma/docker.md#api) com
  `<FONTE>` = `ASNAMES` e a porta 8108 — `API_ASNAMES_TAG` (`latest`),
  `API_ASNAMES_HOST_PORT` (`8108`), `API_ASNAMES_REDIS_URL`
  (`redis://:${VALKEY_PASSWORD:-}@badblock-valkey:6379/0`),
  `API_ASNAMES_CACHE_ENABLED` (`true`), `API_ASNAMES_CACHE_TTL` (`3600`) e
  `API_ASNAMES_DB_POOL_MAX` (`10`), mais as compartilhadas do padrão.
- `.env.example` do app: `POSTGRES_PASSWORD=`, `VALKEY_PASSWORD=`, as URLs
  inteiras comentadas (`# POSTGRES_URL=…`, `# API_ASNAMES_REDIS_URL=…`),
  `API_ASNAMES_TAG=latest`, `API_ASNAMES_HOST_PORT=8108`,
  `API_ASNAMES_CACHE_TTL=3600`, as variáveis do Traefik e `LOG_LEVEL=info`.
  Difere do modelo: não traz o bloco de opcionais comentados
  (`API_ASNAMES_CACHE_ENABLED`, `API_ASNAMES_DB_POOL_MAX`,
  `API_TRUSTED_PROXIES`).
- `.env.example` da raiz: `API_ASNAMES_TAG=latest` (imagens),
  `API_ASNAMES_HOST_PORT=8108` e `API_ASNAMES_CACHE_TTL=3600` (APIs).
- Traefik: as labels do [modelo](../../plataforma/publicacao.md#traefik) com
  `<fonte>` = `asnames` (router e serviço `badblock-api-asnames`, middleware
  `badblock-api-asnames-ratelimit`).

## Operação

```bash
make -C apps/asnames/api up      # só este serviço, com o .env da raiz (o stack inteiro: make up na raiz)
make -C apps/asnames/api smoke   # com o stack no ar
curl http://127.0.0.1:8108/asnames/asn/15169
```

`make smoke` faz `curl -fsS` em
`http://127.0.0.1:${API_ASNAMES_HOST_PORT:-8108}` nas rotas
`/asnames/status`, `/asnames/asn/15169` e `/asnames/openapi.yaml` (o corpo
do manifesto é descartado). Difere do padrão: não consulta `/asnames/meta`,
e o `Makefile` não tem as variáveis `SOURCE`, `PORT` e `SMOKE_ASN`.

## Manifesto OpenAPI

`openapi/openapi.yaml` segue [openapi.md](../../padroes/openapi.md), com os
exemplos e as mensagens de [api-rotas.md](api-rotas.md):

- `info`: `title: api-asnames`, `version: '0.1.0'`, `description` (os dados,
  `description` × campos derivados, `EU`/`AP`, versões como servidores,
  `HEAD`/`OPTIONS`, redirect, cabeçalhos comuns, erros e validação antes de
  tudo) e `license` MIT; `externalDocs` para
  `https://github.com/patrickbrandao/badblock/tree/main/specs/fontes/asnames`.
  Difere do padrão: não tem `info.summary`.
- `servers`: `https://api.badblock.net.br/asnames`,
  `https://api.badblock.net.br/asnames/v1`, `http://127.0.0.1:8108/asnames` e
  `http://127.0.0.1:8108/asnames/v1`. As descrições diferem das do padrão:
  `Produção, versão atual (hoje v1)`, `Produção, v1 fixa (só rotas de dados)`,
  `Desenvolvimento local, versão atual`, `Desenvolvimento local, v1 fixa (só rotas de dados)`;
  nos paths fora do versionamento, `Produção` e `Desenvolvimento local`.
- `tags`: `dados` (consultas, com ETag e Valkey), `meta` (índice, estado e
  manifesto), `saúde`.

| Path | `operationId` | Tag | Respostas |
|---|---|---|---|
| `/` | `getIndex` | `meta` | 200 `IndexResponse` |
| `/asn/{asn}` | `getASN` | `dados` | 200 `ASNResponse` (exemplos `google` e `linha_incompleta`), 304, 400, 404, 500, 503, 504 |
| `/country/{cc}` | `getCountry` | `dados` | 200 `CountryResponse` (`angola`), 304, 400, 404, 500, 503, 504 |
| `/handle/{handle}` | `getHandle` | `dados` | 200 `HandleResponse` (`google`), 304, 400, 404, 500, 503, 504 |
| `/search` | `searchASNs` | `dados` | 200 `SearchResponse` (`google_llc`), 304, 400, 500, 503, 504 — sem 404 |
| `/meta` | `getMeta` | `meta` | 200 `MetaResponse` (`carregado` e `antes_da_primeira_carga`), 500, 503 `DatabaseUnavailable`, 504 |
| `/health`, `/status` | `getHealth`, `postHealth`, `getStatus`, `postStatus` | `saúde` | 200 `StatusOK`, 500, 503 `StatusError` |
| `/ping` | `ping` | `saúde` | 200 `text/plain`, `const: pong` |
| `/openapi.yaml` | `getOpenAPI` | `meta` | 200 `application/yaml` |

- `components.parameters`: `ASN` (`pattern: '^([Aa][Ss])?[0-9]+$'`),
  `CountryCode` (`'^[A-Za-z]{2}$'`), `Handle` (`minLength: 1`), `SearchQuery`
  (`minLength: 3`) e `IfNoneMatch`.
- `components.responses`: os do padrão; `BadRequest` com um exemplo por rota
  (`asn`, `country`, `handle`, `search`), `NotFound` com `asn`, `country` e
  `handle`, `ServiceUnavailable` com `dataset_not_ready` e
  `database_unavailable`.
- `components.schemas`: `Dataset`, `ASNResponse`, `ASNBrief` (lista de
  país), `ASNEntry` (listas de handle e busca), `CountryResponse`,
  `HandleResponse`, `SearchResponse`, `MetaResponse`, `MetaDataset`,
  `Collector`, `Status`, `StatusChecks`, `IndexResponse` e `Error`, com os
  campos de [api-rotas.md](api-rotas.md): `asn` inteiro de 0 a 4294967295;
  `handle`/`name` texto ou `null`; `country` `'^[A-Z]{2}$'` ou `null`;
  `count` ≥ 1 em país e handle, de 0 a 100 na busca; `asns` da busca com no
  máximo 100 itens.
- ETag de exemplo (`IfNoneMatch` e `components.headers.ETag`): o de
  `/asnames/asn/15169`, `W/"c78f48237c990ec"`. Difere do padrão: o manifesto
  traz `W/"8c3f1e2a9b7d4c60"`, que não sai do `etagFor`.

## Medições

Arquivo de 2026-09-28 (122.591 ASNs); `TestRealFile` num PG18 local
(testcontainers), com o handler e o store reais e sem cache; medido entre
2026-09-28 e 2026-09-29.

| O quê | Valor |
|---|---|
| `/country/US` | 32.219 ASNs, 2.254.782 bytes (27% do limite de 8 MiB do cache), ~15–35 ms |
| `/country/BR`, `/country/CN` | 9.162 ASNs e 668 KB; 6.623 ASNs e 605 KB |
| demais países | abaixo de 700 KB |
| `/search`, termo raro (`google`, `côte d'ivoire`, `100%`) | 1–4 ms, pelo índice trigram |
| `/search`, termo comum (`net`, `telecom ltd`) | 1–7 ms |
| `/search`, palavras de uma letra (`a b`) | ~16 ms |
| `/search`, sem letras nem dígitos (`---`) | ~60 ms (varre a tabela; pior caso) |
| busca rara com plano genérico (sem `force_custom_plan`) | ~65 ms, em vez de ~1 ms |
| planejar de novo a cada consulta | menos de 1 ms |

`ILIKE '%google%'` direto no banco, no teste do coletor:
[dados.md](dados.md#asnames_asn).

## Testes

Camadas e comandos: [padrão](../../padroes/api.md#testes) e
[testes.md](../../processos/testes.md). O que é desta API:

| Pacote | Testes | O quê |
|---|---|---|
| `httpapi` | `TestASN`, `TestCountry`, `TestHandle`, `TestSearch`, `TestNormalizeQuery` | store falso com 8 ASNs reais (513, 1297, 4745, 7901, 15169, 16509, 29571, 327710), campos derivados como o coletor grava: `AS`/`as` e `/v1`; `null` no AS7901; país e handle em qualquer caixa e com `/v1` caindo numa chave só (uma consulta ao banco); handle repetido, com espaço e não-ASCII (29571 e 327710); normalização da busca (pontas, internos, tab, maiúsculas), curingas chegando literais ao store, corte em 100 (`bulk` devolve 150, `cent` exatamente 100), limites de 3 e 100 caracteres (não bytes), NUL, UTF-8 inválido, `q` repetido |
| `httpapi` | `TestErrors`, `TestDatabaseErrors`, `TestPanicIsJSON500`, `TestNotReady` | 400 e 404 de cada rota (`AS` sozinho, 4294967296, `USA`, `ç`, handle de 256 caracteres, `/v2`, caminho inexistente), erro sempre JSON com `no-store`; 503 `database_unavailable` e 504 `timeout`; erro e 404 fora do cache; panic → 500 JSON; `dataset_not_ready`, 400 antes do 503, `/status` `starting`, `/meta` com `null` |
| `httpapi` | `TestCacheAndETag`, `TestHEAD`, `TestBypassAndBigBody` | `MISS` → `HIT` com a mesma chave e o mesmo ETag com e sem `/v1`; `If-None-Match` fraco, forte, em lista e `*` → 304 sem consultar; ETag por consulta; `HEAD` nas rotas de dados, no índice e na meta; `BYPASS` sem cache; corpo acima de 8 MiB (um nome de 9 MiB) fora do cache |
| `httpapi` | `TestStatus`, `TestCORSAndHeaders`, `TestIndexMetaAndRedirect` | `/health` e `/status` em GET e POST nos quatro estados e com o cache desligado (`disabled`), `pong`, `/v1/status` 404; preflight e cabeçalhos comuns, inclusive no 404; índice nas duas versões, redirect de `/asnames` com e sem query, `/meta` com `no-store` e sem `X-Cache` |
| `httpapi` | `TestOpenAPIManifest`, `TestOpenAPIRoute` | manifesto × rotas (abaixo) |
| `store` | `TestLikePattern` | escape de `%`, `_` e `\` |
| `store` (integração) | `TestQueries` | as consultas no PG18 (abaixo) |
| `httpapi` (integração) | `TestRealFile` | o arquivo real (abaixo) |
| `config` | `TestDefaults`, `TestBasePathNormalized`, `TestPrecedence`, `TestInvalid` | padrões (`/asnames`, 8080, 1 h, 50 ms); `asnames`, `/asnames/` e `/x/y/` normalizados; argumento vence o ambiente; sem `POSTGRES_URL`, `BASE_PATH=/`, porta 0, TTL negativo, proxy inválido e `LOG_FORMAT=xml` recusados |
| `cache` | `TestBreakerOpensAndCloses`, `TestNoop` | disjuntor abre, não chama o Valkey e fecha depois do cooldown; cache desligado |
| `realip` | `TestClientIP`, `TestNewRejectsBadInput` | conexão direta, via Traefik, `X-Forwarded-For` forjado à esquerda, `X-Real-IP` de reserva, `X-Forwarded-For` inválido, IPv4 mapeado; faixa e cabeçalho inválidos |

`internal/dataset` não tem teste próprio (difere do padrão).

**Manifesto** (`openapi_test.go`): `TestOpenAPIManifest` confere o começo
`openapi: 3.1.`, a falta de tabulação, os `servers` da raiz contra a lista
`rootServers` do teste (as quatro URLs, com a porta 8108 escrita nele), os
`servers` próprios (exatamente os dois sem `/v1`), a falta de `$ref` num path
item, `operationId` sem repetição, todo `$ref` apontando para uma chave que
existe, e cada rota registrada em `Handler()` (menos o redirect e o
catch-all) no manifesto, e vice-versa. `TestOpenAPIRoute`: `GET` e `HEAD` de
`/asnames/openapi.yaml` (tipo, `Cache-Control`, corpo igual a
`openapi.Spec`), `/asnames/v1/openapi.yaml` 404 e o índice listando
`/asnames/openapi.yaml`. Difere do padrão: não confere `info.title`, não lê
a porta do `Makefile`, só exige que haja algum `operationId` (não um por
operação) e não confere se o índice lista rota que falta no manifesto.

**`internal/testdb`** (build tag `integration`): `testdb.New(t)` sobe um
`postgres:18-trixie` descartável pelo testcontainers (banco `badblock`,
usuário `postgres`, senha `pg`), aplica o `migrate:up` de cada `.sql` de
`database/postgres/central/` e `database/postgres/asnames/`, em ordem de
nome (a pasta sai da posição do próprio arquivo, por `runtime.Caller`), e
devolve a URL (`sslmode=disable`) e um pool administrativo para carregar
dados; o container e o pool somem no fim do teste. Sem nenhum `.sql` numa
das pastas, falha com `nenhuma migration em database/postgres/<pasta>`.

**`TestQueries`**: 10 ASNs reais (513, 1297, 4745, 7901, 15169, 16509, 29571,
61613, 327710 — com o handle em maiúsculas, `ORANGE CÔTE D'IVOIRE` — e
403009) e 6 artificiais (64500 a 64505) com `%`, `_` e `\` na
`description` (`TEST-PCT - 100% Fibra, BR`, `TEST_UND - Under_score Ltda, BR`,
`TEST-BS - Back\Slash Ltda, BR`…), mais três execuções: duas aplicadas e uma
recusada, a mais nova. Confere: banco vazio sem versão nem `jobs`; a versão é
a última aplicada, não a última linha; ASN com e sem nulos e inexistente
(`ErrNotFound`); país em ordem, com nulos e vazio; handle em qualquer caixa,
repetido com caixas diferentes no banco, e só igualdade (`goog`, `cern%`,
`test_und_` e `test%` não acham nada; `test_und` acha o 64502); busca com os
curingas literais (`100%`, `% fibra`, `under_score`, `r_s` — sem o escape
casaria `UnderXscore` —, `back\slash`, `\`, `%%%`, `___`), maiúsculas,
limite e ordem; sem execução aplicada, a versão volta a ser nula.

### Arquivo real (`TestRealFile`)

```bash
curl -o /tmp/asn.txt https://ftp.ripe.net/ripe/asnames/asn.txt
ASNAMES_REAL_FILE=/tmp/asn.txt make test-int       # passa ou falha (roda todos os testes de integração)
ASNAMES_REAL_FILE=/tmp/asn.txt go test -count=1 -tags integration -run TestRealFile -v ./internal/httpapi/   # com tempos e tamanhos no log
```

De dentro de `apps/asnames/api/`, com caminho absoluto no
`ASNAMES_REAL_FILE` (o teste roda na pasta do pacote); sem a variável, o
teste é pulado. Rode quando mudar uma consulta, um índice, o `plan_cache_mode` ou
um limite, e compare com as [medições](#medições).

Difere do padrão: não há alvo `test-real` e o teste não compila o coletor
irmão. `parseReal` lê o arquivo e `derive` repete as regras de `handle`,
`name` e `country` do coletor ([fonte.md](fonte.md#campos-derivados)):
mudou uma regra do parser, `derive` muda junto
([fonte.md](fonte.md#mudar-uma-regra)). `parseReal` é mais estrito que o
parser do coletor — sem tratar BOM, comentário, linha vazia, UTF-8 inválido
nem ASN repetido, com o número até o primeiro espaço (`strconv.ParseInt`) —
e qualquer linha fora do formato falha o teste (o arquivo de 2026-09-28 não
tem nenhuma).

Num PG18 do `testdb`, com o handler e o store reais e sem cache:

1. `COPY` das linhas em `asnames_asn`, uma execução aplicada em
   `asnames_run` (URL, SHA-256, bytes e ASNs do arquivo), a linha de `jobs` e
   `ANALYZE`.
2. Rotas pontuais: `/asnames/asn/15169` (`GOOGLE`, `US`),
   `/asnames/v1/asn/AS61613`, `/asnames/asn/4294967295` (404),
   `/asnames/handle/google`, `VRSN-AC50-340`, `ICE%2FHT` e
   `orange%20c%C3%B4te%20d'ivoire`.
3. Buscas, conferindo corte e ordem: `google`, `100%`, `côte d'ivoire`,
   `zzqqxx` e `---` sem corte; `net`, `telecom ltd` e `a b` cortadas em 100.
4. Busca rara repetida 12 vezes direto no store (`zzqq0` a `zzqq11`): falha
   se alguma passar de 25 ms.
5. `/country` de todos os países: todos 200, a soma dos `count` igual aos
   ASNs carregados, os 5 maiores no log; falha se o maior passar de 4 MiB.
6. `/asnames/meta` com os ASNs e o coletor.
7. `EXPLAIN` com literais, pelo pool administrativo: `asn = 15169` usa
   `uq_asnames_asn_asn`; `lower(handle) = lower('google')`,
   `ix_asnames_asn_handle`; `ILIKE '%google%' ORDER BY asn LIMIT 101`,
   `ix_asnames_asn_description_trgm`; `country = 'BR' ORDER BY asn`, qualquer
   índice (o teste procura `asnames_asn`, que é também o nome da tabela).
   Qualquer `Seq Scan` falha.

## Mudar a API

- Rota, parâmetro, campo ou resposta: [api-rotas.md](api-rotas.md) e o
  manifesto no mesmo trabalho, depois o código e os testes de `httpapi`
  ([fluxo](../../processos/fluxo-de-trabalho.md)).
- Consulta ou índice novo: o pedido, com a migration proposta, vai ao
  coletor ([dados.md](dados.md#mudar-o-schema)); depois `TestQueries` e
  `TestRealFile`.
- Regra de derivação nova ([fonte.md](fonte.md#mudar-uma-regra)): `derive`
  no `TestRealFile` e os exemplos de [api-rotas.md](api-rotas.md).
- Opção nova: `--help`, `.env.example` do app e da raiz, compose e esta spec
  ([convenções](../../projeto/convencoes.md#configuração)).
