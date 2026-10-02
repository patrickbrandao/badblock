# rootzone — API (`api-rootzone`)

O que a `api-rootzone` faz de diferente ou a mais que o
[padrão das APIs](../../padroes/api.md) e o
[padrão do manifesto](../../padroes/openapi.md). O resto — `main.go`,
caminho de base e versões, formato, versão do dataset, `serveCached`, ETag,
cabeçalhos, saúde, erros, IP real, configuração comum, container — segue os
padrões. Cada rota, com validações, campos, exemplos reais e mensagens de
erro: [api-rotas.md](api-rotas.md). Tabelas e o SQL das consultas:
[dados.md](dados.md#consultas-da-api); de onde vêm os campos:
[fonte.md](fonte.md). Dono: sub-agente `api-rootzone`.

## Valores desta fonte

| Item | Valor |
|---|---|
| App, binário | `api-rootzone` (`/api-rootzone` na imagem) |
| Module Go | `github.com/patrickbrandao/badblock/apps/rootzone/api` |
| Imagem, container | `tmsoftbrasil/badblock-api-rootzone`, `badblock-api-rootzone` |
| Descrição da imagem (`org.opencontainers.image.description`) | `API HTTP da zona raiz do DNS (root.zone da InterNIC: TLDs, servidores, glue e DS), do BadBlock` |
| Caminho de base (`BASE_PATH`) | `/rootzone` — `https://api.badblock.net.br/rootzone/` |
| Porta local | 8110 (`API_ROOTZONE_HOST_PORT`, só no loopback) |
| Lê | `rootzone_run`, `rootzone_tld`, `rootzone_record` e a linha `collector-rootzone` de `jobs` ([dados.md](dados.md#consultas-da-api)) |
| `source` do índice | `https://www.internic.net/domain/root.zone` (constante `SourceURL`; não vem do banco) |
| Chave no Valkey | `badblock:api-rootzone:<versão>:<consulta>` ([abaixo](#cache-e-etag)) |
| Cliente do Valkey | `ClientName` = `badblock-api-rootzone` |
| Cabeçalho `Server` | `badblock-api-rootzone/<versão>` |
| Pool pgx | `application_name = api-rootzone` quando a `POSTGRES_URL` não traz outro (o planejador fica no padrão) |
| Pacotes a mais | `internal/tldname` (normalização do TLD e punycode) e `internal/testdb` ([testes](#testes)) |
| Arquivo real nos testes | `make test-real` (baixa o `root.zone` do dia, ou `FILE=...`); `make test-int` roda o mesmo teste com a fixture do coletor |

Pacotes: os do padrão (sem `internal/rir`) mais `internal/tldname` e
`internal/testdb`. Em `internal/httpapi`: `server.go` (`Handler`,
middleware, `serveCached`, `etagFor`, erros), `handlers.go` (índice,
`/meta`, saúde, manifesto e a tabela `dataRoutes`), `handlers_tld.go`
(`/tlds` e `/tld/{tld}`) e `types.go` (os tipos do JSON). `internal/store`
tem as consultas de [dados.md](dados.md#consultas-da-api).

O `--help` começa assim (depois vêm o uso — `api-rootzone [opções]` e
`api-rootzone healthcheck` — e as opções do padrão, em ordem alfabética,
cada uma com a variável e o padrão):

```
api-rootzone — API HTTP da zona raiz do DNS (root.zone da InterNIC): TLDs
delegados, servidores de nome, glue e DS.

Lê as tabelas rootzone_* mantidas pelo collector-rootzone, com cache opcional no
Valkey. Responde tudo abaixo de BASE_PATH (/rootzone): /rootzone/tld/br é a
versão atual e /rootzone/v1/tld/br fixa a v1.
```

## Rotas

Todas abaixo de `/rootzone`. As de dados, o índice e `/meta` respondem
também em `/rootzone/v1/...`; saúde, `ping` e o manifesto existem só sem
versão ([padrão](../../padroes/api.md#caminho-de-base-e-versões)). Estilo
das outras APIs: plural para a lista, singular para um item.

| Rota | `operationId` | Tag | Chave de cache |
|---|---|---|---|
| `GET /rootzone/tlds` | `listTLDs` | `dados` | `tlds` |
| `GET /rootzone/tld/{tld}` | `getTLD` | `dados` | `tld:<tld normalizado>` (`tld:br`, `tld:xn--p1ai`) |
| `GET /rootzone/meta` | `getMeta` | `meta` | — (sem cache) |
| `GET /rootzone/` | `getIndex` | `meta` | — |
| `GET`, `POST /rootzone/health` | `getHealth`, `postHealth` | `saúde` | — |
| `GET`, `POST /rootzone/status` | `getStatus`, `postStatus` | `saúde` | — |
| `GET /rootzone/ping` | `ping` | `saúde` | — |
| `GET /rootzone/openapi.yaml` | `getOpenAPI` | `meta` | — |

### Rota de dados nova

O código foi montado para crescer (o usuário vai pedir outras rotas: o ápice,
os TLDs de um servidor, o dono de um endereço de glue — as consultas já
estão em [dados.md](dados.md#consultas-da-api)). Para acrescentar uma:

1. Esta spec e [api-rotas.md](api-rotas.md), com exemplo real, e o path no
   manifesto (o `openapi_test.go` falha sem ele).
2. A consulta em `internal/store` (método novo no `Store` e na interface
   `Store` de `server.go`), com o caso no `TestQueries`.
3. O handler em `handlers_<assunto>.go`, validando antes de tudo e chamando
   `serveCached` com a chave normalizada; os tipos em `types.go`, com o
   bloco `zone` (`zoneInfo(snap)`) e o `dataset`.
4. Uma linha em `dataRoutes()` (`handlers.go`): registra a rota com e sem
   `/v1` e a põe no `endpoints` do índice, na ordem da tabela.
5. O caso no store falso e nos testes de `httpapi`, e a rota no
   `TestRealFile`.

## Dados servidos

- Nomes como o coletor grava ([fonte.md](fonte.md#linhas-parseparse)):
  minúsculas, sem o ponto final; IPv6 na forma canônica da RFC 5952; digest
  do DS em hexadecimal maiúsculo. A raiz (`.`) não é um TLD.
- `tld` é a forma ASCII (`xn--p1ai`), a chave; `tld_unicode` é a forma
  Unicode que o coletor decodifica (`рф`), só para exibir, e sai em UTF-8 no
  JSON (sem `\u`).
- **Bloco `zone`**: toda resposta de dados traz `"zone": {"serial": N}`, o
  serial do SOA da execução aplicada que é a versão do dataset. O watcher
  (`internal/dataset`) lê o serial junto com a versão, na mesma linha de
  `rootzone_run`, então o serial sempre corresponde ao ETag — sem consulta a
  mais. `null` só numa linha fora do padrão (o coletor sempre grava o serial
  nas aplicadas). O SOA inteiro está em `/meta`.
- `first_seen` e `updated_at` de `/tld` são o `created_at` e o `updated_at`
  de `rootzone_tld`: datas **deste banco**, não da delegação
  ([dados.md](dados.md#mapeamento-da-fonte-para-as-colunas)).
- Os RRSIG não são guardados, e esta API não os mostra; NSEC, DNSKEY e
  ZONEMD estão em `rootzone_record`, mas nenhuma rota atual os serve.
- **Ordem**: a do banco, sem reordenar na API — `ORDER BY tld` em `/tlds`,
  `ORDER BY type, rdata` nos NS e DS, `ORDER BY g.owner, g.type, g.rdata` no
  glue. Com a collation do banco (`en_US.utf8`, a da imagem do Postgres), a
  pontuação pesa menos que letras e dígitos: `ns2.nic.fr` vem antes de
  `ns.dns.br`. Nos 1.438 TLDs de 2026-09-29 a ordem de `/tlds` é igual à
  ordem por bytes (`COLLATE "C"`).

## Normalização do TLD (`internal/tldname`)

`/tld/{tld}` aceita o nome como a pessoa o escreveria e o leva à forma de
`rootzone_tld.tld` (regra de `chk_rootzone_tld_tld`, `^[a-z0-9_-]{1,63}$`):

1. Tira **um** ponto final (`br.` → `br`; `br..` é inválido).
2. Nome vazio ou UTF-8 inválido: 400.
3. Com algum caractere fora do ASCII: todo caractere não-ASCII tem de ser
   letra, marca ou dígito Unicode (`unicode.L`, `unicode.M`, `unicode.Nd`;
   espaço, controle, pontuação e formatação como U+200B são 400); o nome
   passa a minúsculas (`strings.ToLower`) e é codificado em punycode com o
   prefixo `xn--` (`рф`/`РФ` → `xn--p1ai`, `Vermögensberater` →
   `xn--vermgensberater-ctb`).
4. O resultado passa a minúsculas e tem de casar com
   `^[a-z0-9_-]{1,63}$`; senão, 400 (ponto no meio, espaço, `*`, `%`,
   mais de 63 caracteres — também depois de codificar).
5. Um rótulo `xn--` pedido em ASCII não é decodificado para conferir o
   punycode: `xn--zz` é válido e dá 404 (o coletor também aceita qualquer
   rótulo `xn--` na zona).

O punycode (RFC 3492) é feito à mão, sem dependência: `Encode` (seção 6.3)
e `Decode` (6.2), este uma cópia de `parse.Punycode` do coletor
(`apps/rootzone/collector/internal/parse/punycode.go`) — não há código
compartilhado entre apps. Mudou o decodificador do coletor: a cópia muda
junto. Não há o mapeamento completo da UTS #46 nem a normalização NFC: um
nome em Unicode decomposto (NFD) não acha o TLD (404); os 151 IDN da zona
foram conferidos pela forma Unicode que o coletor grava
([testes](#testes)).

## Cache e ETag

Toda rota de dados passa pelo `serveCached` do
[padrão](../../padroes/api.md#cache-valkey-cache-aside-e-servecached). A
chave é a consulta **normalizada**, e a consulta ao banco usa o mesmo
valor:

| Pedido | Chave | Normalização |
|---|---|---|
| `/tlds`, `/v1/tlds` | `tlds` | — |
| `/tld/br`, `/tld/BR.`, `/v1/tld/Br` | `tld:br` | [acima](#normalização-do-tld-internaltldname) |
| `/tld/xn--p1ai`, `/tld/XN--P1AI.`, `/tld/рф`, `/tld/РФ.` | `tld:xn--p1ai` | idem |

- Com a versão dos exemplos (`01a0f073-f7ab-73d8-bf10-586563066827`), a
  chave de `/rootzone/tld/br` é
  `badblock:api-rootzone:01a0f073-f7ab-73d8-bf10-586563066827:tld:br` e o
  ETag, `W/"c378da2e80b8654"` (`etagFor`; o hex sai sem zeros à esquerda).
  Outros: `tlds` → `W/"f60b39e5f95a1753"`, `tld:xn--p1ai` →
  `W/"8c99d0d875f78a75"` (o `TestCacheAndETag` confere os três).
- Uma zona nova (cerca de 2 por dia) é uma versão nova: ETags e chaves
  novos, e um `If-None-Match` antigo recebe 200 com o corpo novo. O 304 sai
  com `X-Cache: HIT`.
- 404, 400 e erros não entram no cache.
- Limite do corpo guardado: 8 MiB (`maxCachedBody = 8 << 20`). A maior
  resposta, `/tlds`, tem 161.044 bytes (2% do limite); o maior `/tld`, o do
  `com`, 2.294 bytes.

## Consultas

O SQL de cada rota e o índice que ela usa estão em
[dados.md](dados.md#consultas-da-api) (dono: o coletor). O que é da API:

- `/tlds`: uma consulta (`rootzone_tld` inteira, ~1,4 mil linhas).
- `/tld/{tld}`: três consultas — a linha de `rootzone_tld` (404 se não
  existe, sem as outras duas), os NS e DS do dono e o glue A/AAAA dos NS —
  numa transação `REPEATABLE READ` só de leitura (`pgx.TxOptions`), para as
  três verem a mesma zona mesmo se o coletor aplicar outra no meio (a
  aplicação é uma transação só: [collector.md](collector.md#aplicação)).
- `/meta`: a última linha aplicada de `rootzone_run` (versão, URL,
  SHA-256, serial, SOA e contagens) e a linha do coletor em `jobs`.
- O glue de um servidor sob outro TLD (`ns.dns.br` servindo `bo`) vem pela
  mesma junção: o glue está na zona raiz, qualquer que seja o TLD do nome.
- Um rdata fora do formato gravado pelo coletor (um DS que não se decompõe
  em `keytag alg tipo DIGEST`, um tipo que não é NS/DS, glue de quem não é
  NS) é quebra de contrato com `rootzone_record`: log `registro fora do
  formato` e **500 `internal_error`** — difere do padrão, que reserva o 500
  para panic. Nada disso acontece com o coletor atual.

## Configuração

Só as opções comuns do [padrão](../../padroes/api.md#configuração-comum),
com `BASE_PATH` = `/rootzone` (inválido:
`--base-path inválido: "/" (ex.: /rootzone)`); nenhuma opção a mais.

- Compose: o [modelo da API](../../plataforma/docker.md#api) com
  `<FONTE>` = `ROOTZONE` e a porta 8110 — `API_ROOTZONE_TAG` (`latest`),
  `API_ROOTZONE_HOST_PORT` (`8110`), `API_ROOTZONE_REDIS_URL`
  (`redis://:${VALKEY_PASSWORD:-}@badblock-valkey:6379/0`),
  `API_ROOTZONE_CACHE_ENABLED` (`true`), `API_ROOTZONE_CACHE_TTL` (`3600`)
  e `API_ROOTZONE_DB_POOL_MAX` (`10`), mais as compartilhadas do padrão.
- `.env.example` do app: o [modelo](../../plataforma/docker.md#envexample-do-app),
  com os opcionais comentados (`API_ROOTZONE_CACHE_ENABLED`,
  `API_ROOTZONE_DB_POOL_MAX`, `API_TRUSTED_PROXIES`).
- `.env.example` da raiz: `API_ROOTZONE_TAG=latest` (imagens),
  `API_ROOTZONE_HOST_PORT=8110` e `API_ROOTZONE_CACHE_TTL=3600` (APIs).
- Traefik: as labels do [modelo](../../plataforma/publicacao.md#traefik) com
  `<fonte>` = `rootzone` (router e serviço `badblock-api-rootzone`,
  middleware `badblock-api-rootzone-ratelimit`).

## Operação

```bash
make -C apps/rootzone/api up      # só este serviço, com o .env da raiz (o stack inteiro: make up na raiz)
make -C apps/rootzone/api smoke   # com o stack no ar
curl http://127.0.0.1:8110/rootzone/tld/br
```

`make smoke` faz `curl -fsS` em `http://127.0.0.1:$(PORT)` (`PORT` =
`${API_ROOTZONE_HOST_PORT:-8110}`) nas rotas `/rootzone/status`,
`/rootzone/meta`, `/rootzone/tlds` (corpo descartado),
`/rootzone/tld/$(SMOKE_TLD)` (padrão `br`) e `/rootzone/openapi.yaml`
(corpo descartado). O `Makefile` tem as variáveis `SOURCE`, `PORT`,
`SMOKE_TLD` e `SOURCE_URL` (a do `test-real`).

## Manifesto OpenAPI

`openapi/openapi.yaml` segue [openapi.md](../../padroes/openapi.md), com os
exemplos e as mensagens de [api-rotas.md](api-rotas.md):

- `info`: `title: api-rootzone`, `version: '0.1.0'`, `summary`,
  `description` (os dados, a normalização dos nomes, o bloco `zone`, versões
  como servidores, `HEAD`/`OPTIONS`, redirect, cabeçalhos comuns, erros e
  validação antes de tudo) e `license` MIT; `externalDocs` para
  `https://github.com/patrickbrandao/badblock/tree/main/specs/fontes/rootzone`.
- `servers`: os quatro do padrão, com a porta 8110 e as descrições do
  padrão; nos paths fora do versionamento, `Produção` e
  `Desenvolvimento local`.
- `tags`: `dados` (consultas, com ETag e Valkey), `meta` (índice, estado e
  manifesto), `saúde`.

| Path | `operationId` | Tag | Respostas |
|---|---|---|---|
| `/` | `getIndex` | `meta` | 200 `IndexResponse` |
| `/tlds` | `listTLDs` | `dados` | 200 `TLDsResponse` (exemplo `trecho`), 304, 500, 503, 504 — sem 400 nem 404 |
| `/tld/{tld}` | `getTLD` | `dados` | 200 `TLDResponse` (exemplos `br`, `bo` e `idn`, este um trecho), 304, 400, 404, 500, 503, 504 |
| `/meta` | `getMeta` | `meta` | 200 `MetaResponse` (`carregado` e `antes_da_primeira_carga`), 500, 503 `DatabaseUnavailable`, 504 |
| `/health`, `/status` | `getHealth`, `postHealth`, `getStatus`, `postStatus` | `saúde` | 200 `StatusOK`, 500, 503 `StatusError` |
| `/ping` | `ping` | `saúde` | 200 `text/plain`, `const: pong` |
| `/openapi.yaml` | `getOpenAPI` | `meta` | 200 `application/yaml` |

- `components.parameters`: `TLD` (com os exemplos `br`, `xn--p1ai` e `рф`)
  e `IfNoneMatch`.
- `components.responses`: os do padrão; `BadRequest` e `NotFound` com o
  exemplo de `/tld`, `ServiceUnavailable` com `dataset_not_ready` e
  `database_unavailable`; `InternalError` cita também o registro fora do
  formato.
- `components.schemas`: `Dataset`, `Zone`, `TLDsResponse`, `TLDSummary`,
  `TLDResponse`, `Nameserver`, `Address`, `DS`, `MetaResponse`,
  `MetaDataset`, `SOA`, `Collector`, `Status`, `StatusChecks`,
  `IndexResponse` e `Error`, com os campos de [api-rotas.md](api-rotas.md):
  `tld` com `pattern: '^[a-z0-9_-]{1,63}$'`; `serial` inteiro de 0 a
  4294967295 ou `null`; `key_tag` de 0 a 65535, `algorithm` e
  `digest_type` de 0 a 255, `digest` `'^[0-9A-F]+$'`.
- ETag de exemplo (`IfNoneMatch` e `components.headers.ETag`): o de
  `/rootzone/tld/br` na versão dos exemplos, `W/"c378da2e80b8654"`.

## Medições

Arquivo de 2026-09-29 (serial `2026092901`, 2.250.583 bytes, 1.438 TLDs,
22.131 RRs, 2.794 RRSIG), medido em 2026-09-30 no `TestRealFile`
(`make test-real FILE=...`, PG18 local via testcontainers, handler e store
reais, sem cache) e com a imagem no ar (`curl` no loopback).

| O quê | Valor |
|---|---|
| carga pelo coletor (`--once`, arquivo servido localmente) | ~0,6 s |
| `/tlds` | 1.438 TLDs, 161.044 bytes, ~5 ms sem cache; ~3–4 ms pelo `curl` com `X-Cache: HIT` |
| `/tld/{tld}`, cada um dos 1.438 (três consultas numa transação) | 0,7–2,4 ms; os 1.438 mais os 151 IDN pela forma Unicode em ~1,2 s |
| maior `/tld` | `com`: 13 NS, 26 endereços, 2.294 bytes |
| `/tld/br`, `/tld/рф` | 1.205 e 1.325 bytes, ~0,7 ms |
| `/meta` | 587 bytes, ~1 ms |
| TLDs sem DS, sem nenhum servidor IPv6 | 87 e 18 (conferem com [fonte.md](fonte.md#fatos-medidos)) |

`TestRealFile` falha, só com o arquivo inteiro, se `/tlds` passar de
500 ms ou algum `/tld` de 100 ms.

## Testes

Camadas e comandos: [padrão](../../padroes/api.md#testes) e
[testes.md](../../processos/testes.md). O que é desta API:

| Pacote | Testes | O quê |
|---|---|---|
| `httpapi` | `TestTLDs`, `TestTLD`, `TestTLDUnicode` | store falso com um recorte real (`bo`, `br`, `top`, `xn--p1ai`, com NS, DS e glue como o coletor grava): `/tlds` com e sem `/v1` numa chave só, formato do JSON (`zone` primeiro); `/tld` em qualquer caixa, com ponto final e `/v1` numa chave só (uma consulta ao banco); NS com glue, DS decomposto; `bo` sem DS (`ds: []`), servidor sem AAAA (`ipv6: []`), glue sob outro TLD, a ordem do store mantida; `top` com 2 DS e servidores só IPv4 ou só IPv6; `рф`, `РФ`, `рф.`, `XN--P1AI.` e `/v1/tld/рф` na chave `tld:xn--p1ai`, Unicode sem escape no JSON |
| `httpapi` | `TestErrors`, `TestDatabaseErrors`, `TestParseDS`, `TestPanicIsJSON500`, `TestNotReady` | 400 (64 caracteres, ponto no meio, `br..`, espaço, NUL, UTF-8 inválido, `*`, U+200B, `рф!`) e 404 (`nada-disso`, `xn--zz`, 63 caracteres, Unicode válido fora do recorte, com a mensagem no nome normalizado), rotas inexistentes, `DELETE`, `/tld/.` limpo pelo `ServeMux`; 503 `database_unavailable` e 504 `timeout`; erro e 404 fora do cache; DS fora do formato → 500; `parseDS` com campos a mais, a menos e fora da faixa; panic → 500 JSON; `dataset_not_ready` sem consultar o banco, 400 antes do 503, `/status` `starting`, `/meta` com `null` |
| `httpapi` | `TestCacheAndETag`, `TestHEAD`, `TestBypassAndBigBody` | `MISS` → `HIT` com a mesma chave e o mesmo ETag com e sem `/v1`; `If-None-Match` fraco, forte, em lista e `*` → 304 sem corpo nem `Content-Type`, sem consultar; ETag por consulta; os três ETags dos exemplos; `HEAD` nas rotas de dados, no índice e na meta; `BYPASS` sem cache; corpo acima de 8 MiB (60 mil TLDs sintéticos) fora do cache |
| `httpapi` | `TestStatus`, `TestCORSAndHeaders`, `TestIndexMetaAndRedirect` | `/health` e `/status` em GET e POST nos quatro estados e com o cache desligado (`disabled`), `pong`, saúde e `ping` 404 em `/v1`; preflight e cabeçalhos comuns, inclusive no 404; índice nas duas versões (`endpoints` na ordem), redirect de `/rootzone` com e sem query, `/meta` com o SOA, `no-store` e sem `X-Cache` |
| `httpapi` | `TestOpenAPIMatchesRoutes`, `TestOpenAPIDocument`, `TestOpenAPIRoute` | o teste completo do [padrão](../../padroes/openapi.md#teste): rotas × paths nos dois sentidos, `info.title`, os quatro `servers` com a porta lida do `Makefile`, `servers` próprios, `operationId` um por operação e sem repetição, `$ref` existentes, `GET`/`HEAD` do manifesto, `/v1/openapi.yaml` 404 e o índice sem rota fora do manifesto |
| `tldname` | `TestPunycodeRoundTrip`, `TestNormalize` | `Encode` e `Decode` em 14 IDN reais da zona (cirílico, CJK, coreano, devanágari, árabe, tâmil, latino com `ö`) e nos exemplos da RFC 3492; `Normalize` com caixa, ponto final, Unicode, 63/64 caracteres e os inválidos |
| `dataset` | `TestWatcherRefresh`, `TestWatcherRunStopsWithContext` | versão e serial lidos juntos, erro mantém a versão, linha sem SOA (serial nulo), volta a "sem carga"; `Run` para com o contexto |
| `store` (integração) | `TestQueries` | as consultas no PG18 (abaixo) |
| `httpapi` (integração) | `TestRealFile` | a fonte real, carregada pelo coletor (abaixo) |
| `config` | `TestDefaults`, `TestBasePathNormalized`, `TestPrecedence`, `TestInvalid` | padrões (`/rootzone`, 8080, 1 h, 50 ms); `rootzone`, `/rootzone/` e `/x/y/` normalizados; argumento vence o ambiente; sem `POSTGRES_URL`, `BASE_PATH=/`, porta 0, TTL negativo, proxy inválido e `LOG_FORMAT=xml` recusados |
| `cache` | `TestBreakerOpensAndCloses`, `TestNoop` | disjuntor abre, não chama o Valkey e fecha depois do cooldown; cache desligado |
| `realip` | `TestClientIP`, `TestNewRejectsBadInput` | conexão direta, via Traefik, `X-Forwarded-For` forjado à esquerda, `X-Real-IP` de reserva, `X-Forwarded-For` inválido, IPv4 mapeado; faixa e cabeçalho inválidos |

**`internal/testdb`** (build tag `integration`): `testdb.New(t)` sobe um
`postgres:18-trixie` descartável pelo testcontainers (banco `badblock`,
usuário `postgres`, senha `pg`), aplica o `migrate:up` de cada `.sql` de
`database/postgres/central/` e `database/postgres/rootzone/`, em ordem de
nome, e devolve a URL e um pool administrativo; tudo some no fim do teste.

**`TestQueries`**: o banco vazio (sem versão, sem `jobs`, sem TLDs,
`Delegation` → `ErrNotFound`); depois um recorte real de 2026-09-29 — `bo`,
`top` e `xn--p1ai` em `rootzone_tld` e, em `rootzone_record`, os NS, DS e
NSEC deles, o glue de todos os servidores (inclusive `ns.dns.br`, sob
`br`), o SOA e um NS da raiz com o glue —, duas execuções aplicadas e uma
recusada, a mais nova, e a linha de `jobs`. Confere: a versão é a última
aplicada, com serial, SOA e contagens; `TLDs` em ordem com as contagens;
`Delegation("bo")` com os 4 NS na ordem do banco, sem o NSEC, e os 7
endereços de glue; `top` com os 2 DS antes dos 8 NS e 9 endereços; o IDN
pela forma ASCII; `.`, `""`, `BO`, `bo.`, `a.root-servers.net` e `nada`
dão `ErrNotFound` (o store não normaliza); sem execução aplicada, a versão
volta a ser nula.

### Fonte real (`TestRealFile`)

```bash
make test-real                          # baixa o root.zone do dia (curl --compressed) e roda o teste com tempos no log
make test-real FILE=/tmp/root.zone      # com um arquivo já baixado
make test-int                           # o mesmo teste com a fixture do coletor, sem rede
```

No molde da `api-roothints`: compila o coletor irmão
(`go build ./cmd/collector-rootzone` em `../collector`), serve o arquivo e
o `.md5` por um `httptest.Server` e roda `collector-rootzone --once` contra
um PG18 do `testdb` (com a fixture, `MIN_TLDS=0`, porque o recorte tem 7
TLDs); depois `ANALYZE` e, com o handler e o store reais e sem cache:

1. `/meta` igual à linha de `rootzone_run` (serial, TLDs, RRs, RRSIG, SOA)
   e ao SHA-256 do arquivo.
2. `/tlds`: `count` igual a `rootzone_run.tlds` e às linhas de
   `rootzone_tld`, na mesma ordem da tabela, com o serial da versão.
3. `/tld` de **todos** os TLDs: nome, forma Unicode, quantidade de NS, de
   servidores com glue IPv4 e IPv6 e de DS iguais às contagens de
   `/tlds`; todo NS com TTL e algum glue; cada IDN também pela forma Unicode
   em maiúsculas, com ponto final e `/v1`, com o mesmo corpo. Log com o
   tempo total, o mais lento e o maior.
4. `/tld/nada-disso` é 404. Com a fixture, os valores de
   [fonte.md](fonte.md#fixture) (serial `2026092901`, 7 TLDs, 195 RRs,
   17 RRSIG, o DS e o IPv6 canônico do `br`).
5. `EXPLAIN` das três consultas de `/tld`: `uq_rootzone_tld_tld` e
   `uq_rootzone_record_owner_type_rdata` (nas duas pontas da junção do
   glue), sem `Seq Scan` — com a zona inteira, o planejador escolhe sozinho;
   com a fixture, com `enable_seqscan = off`.

Rode quando mudar uma consulta, um índice, a normalização ou o formato da
resposta, e compare com as [medições](#medições).

## Mudar a API

- Rota, parâmetro, campo ou resposta: [api-rotas.md](api-rotas.md) e o
  manifesto no mesmo trabalho, depois o código e os testes de `httpapi`
  ([fluxo](../../processos/fluxo-de-trabalho.md)); rota nova, pelos passos
  de [Rota de dados nova](#rota-de-dados-nova). Mudança incompatível no JSON
  = `/rootzone/v2/...`.
- Consulta ou índice novo: o pedido, com a migration proposta, vai ao
  coletor ([dados.md](dados.md#mudar-o-schema)); depois `TestQueries` e
  `TestRealFile`.
- Normalização dos nomes no coletor ([fonte.md](fonte.md#linhas-parseparse))
  ou o punycode dele: `internal/tldname` e os exemplos de
  [api-rotas.md](api-rotas.md).
- Opção nova: `--help`, `.env.example` do app e da raiz, compose e esta spec
  ([convenções](../../projeto/convencoes.md#configuração)).
