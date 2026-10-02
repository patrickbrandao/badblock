# Padrão das APIs

Vale para todas as `api-<fonte>`. O que é de uma fonte (rotas de dados,
exemplos reais, chaves de cache, porta, medições) fica em
[`fontes/<fonte>/api.md`](../fontes/README.md) e, na família RIR, em
[`fontes/rir/api.md`](../fontes/rir/api.md). Quando a spec da fonte diz outra
coisa, vale a da fonte — e ela diz por quê. O manifesto OpenAPI segue
[openapi.md](openapi.md).

Dono: sessão principal. Mudar este arquivo muda **todas** as APIs: a mesma
mudança vai para o código de cada uma (não há código compartilhado).

## Papel

- API puramente HTTP (JSON) sobre as tabelas `<fonte>_*` que o
  `collector-<fonte>` grava. Não importa nada e **não escreve nada** no banco
  (a conexão é a mesma `POSTGRES_URL` de todos, com o usuário `postgres`; a
  disciplina é do código: nenhum `INSERT/UPDATE/DELETE`).
- Dona de tudo abaixo do caminho de base `/<fonte>` no host da API
  (`https://api.badblock.net.br/<fonte>/...`), inclusive das versões.
- Pública, sem chave; o rate limit por IP é do Traefik
  ([../plataforma/publicacao.md](../plataforma/publicacao.md)).

## Estrutura do código

```
apps/<fonte>/api/
├── cmd/api-<fonte>/main.go   config → logger → Postgres → Valkey → versão do dataset → HTTP
├── internal/
│   ├── buildinfo/   Version, Commit, Date (via -ldflags) e String()
│   ├── config/      opções: padrão → variável de ambiente → argumento; --help
│   ├── store/       pgx (pool): consultas só de leitura, última execução aplicada, linha de jobs
│   ├── dataset/     Watcher: relê a versão do dataset a cada DATASET_POLL
│   ├── cache/       Valkey: Get/Set com timeout, disjuntor, modo desligado
│   ├── realip/      IP real do cliente atrás de proxies confiáveis
│   ├── httpapi/     rotas, middlewares, serveCached, ETag, erros; openapi_test.go
│   └── rir/         só na família RIR: tudo o que é do RIR (ver fontes/rir)
├── openapi/         openapi.yaml (manifesto) e embed.go (go:embed)
└── Dockerfile, .dockerignore, docker-compose.yml, .env.example, Makefile, .golangci.yml, README.md, go.mod, go.sum, vendor/
```

Um pacote a mais só quando a fonte pede (ex.: `internal/testdb` na
`api-asnames`), descrito na spec da fonte.

### `main.go`

1. `config.Load`: `--help` escreve a ajuda no stderr e sai com 0; opção
   desconhecida → a ajuda e `api-<fonte>: flag provided but not defined:
   -<nome>`, saída 2; opção inválida → `api-<fonte>: <erro>` no stderr, saída 2; `--version` → `api-<fonte> <versão> (commit <c>,
   build <d>)`, saída 0. O subcomando `api-<fonte> healthcheck` (tratado antes
   do `config.Load`) faz `GET http://127.0.0.1:$HTTP_PORT$BASE_PATH/ping`
   (padrões 8080 e `/<fonte>`, prazo de 3 s) e sai com 0 (200) ou 1 — é o
   `HEALTHCHECK` da imagem distroless (sem shell nem curl).
2. Logger `log/slog` no stdout (JSON por padrão), com `app=api-<fonte>` em
   toda linha; primeira linha `iniciando`.
3. Postgres: tenta por até **2 minutos** (tentativas de 10 s, 5 s entre elas),
   porque o banco e as migrations podem estar subindo junto; esgotado →
   `sem conexão com o Postgres`, saída 1. Pool pgx com até
   `DB_POOL_MAX` conexões (fechadas depois de 5 min ociosas, conferidas a cada
   30 s) e `application_name = api-<fonte>` quando a URL não define outro. Uma
   `POSTGRES_URL` que o pgx não lê não é erro de configuração: cai nas mesmas
   tentativas e termina em `sem conexão com o Postgres`.
4. Valkey (se `REDIS_URL` e `REDIS_CACHE_ENABLED`): URL inválida é erro de
   configuração (log `REDIS_URL inválida`, saída 2); Valkey fora no boot
   **não impede a subida** (log
   `Valkey indisponível no boot; seguindo sem cache até ele voltar`). Cliente
   com `ClientName = badblock-api-<fonte>`, `MaxRetries = 0`,
   `ReadTimeout`/`WriteTimeout`/`PoolTimeout` = `REDIS_TIMEOUT` e
   `DialTimeout` = max(4 × `REDIS_TIMEOUT`, 200 ms).
5. Versão do dataset: lê uma vez, sem prazo próprio (falha só loga), e segue
   relendo em segundo plano.
6. `http.Server` em `:$HTTP_PORT` com `ReadHeaderTimeout` 5 s, `ReadTimeout`
   15 s, `WriteTimeout` 60 s, `IdleTimeout` 120 s. `SIGINT`/`SIGTERM` →
   `Shutdown` com prazo de 15 s.

## Caminho de base e versões

- A API responde **tudo** abaixo de `BASE_PATH` (`/<fonte>`). O Traefik
  encaminha `/<fonte>` e `/<fonte>/*` sem remover o prefixo, então a mesma URL
  funciona com e sem proxy.
- `/<fonte>/<rota>` é a versão atual (hoje v1); `/<fonte>/v1/<rota>` fixa a
  v1: mesmo conteúdo, mesmo ETag e mesma chave de cache.
- Mudança incompatível no JSON = versão nova (`/<fonte>/v2/...`): a rota sem
  versão passa a apontar para ela e `/<fonte>/v1/...` continua respondendo
  igual.
- `/<fonte>` redireciona (301) para `/<fonte>/`. O `http.ServeMux` redireciona
  sozinho (307, corpo HTML) `/<fonte>/v1` para `/<fonte>/v1/` e caminhos com
  `//`, `.` ou `..` para o caminho limpo.
- Qualquer outro caminho — e também método que a rota não aceita (o catch-all
  `/` atende todos os métodos, então nunca há 405) — é 404 JSON
  (`rota inexistente; veja /<fonte>/`).
- Saúde (`/health`, `/status`, `/ping`) e o manifesto (`/openapi.yaml`) ficam
  **fora** do versionamento.
- Toda rota é registrada por `handle(padrão, handler)` em `Handler()`, que
  guarda o padrão para o teste do manifesto.

## Formato das respostas

- JSON compacto (`encoding/json`, com o escape HTML padrão: `<`, `>` e `&`
  saem como `\u003c`, `\u003e` e `\u0026`), `snake_case`, UTF-8,
  `Content-Type: application/json; charset=utf-8`, uma quebra de linha no fim.
- Timestamps ISO-8601 em UTC (`2026-09-29T00:53:49Z`); datas sem hora como
  `AAAA-MM-DD`.
- Coluna `NULL` sai `null`; lista vazia sai `[]`.
- Toda rota de dados leva o bloco
  `"dataset": {"version": "<uuid>", "updated_at": "<timestamp>"}` com a versão
  usada na resposta.
- `GET` aceita `HEAD` (mesmos cabeçalhos, sem corpo).

## Versão do dataset

- Versão = `uuid` da última linha `status = 1` de `<fonte>_run`
  ([coletor.md](coletor.md#tabela-fonte_run)); `updated_at` = `created_at`
  dessa linha. Toda aplicação gera uma versão nova — inclusive um `--force` e
  um arquivo novo que não muda nenhuma linha —, e com ela chaves de cache e
  ETags novos.
- `internal/dataset` a relê na subida e a cada `DATASET_POLL` (30 s), com prazo
  de 5 s por releitura; falha na releitura mantém a versão conhecida (log
  `não consegui ler a versão do dataset`). As rotas de dados usam a versão do
  watcher, que pode estar até `DATASET_POLL` atrás; o `/meta` lê direto do
  banco.
- Sem versão (antes da primeira carga do coletor), as rotas de dados
  respondem `503 dataset_not_ready` **sem consultar o banco**.

## Cache (Valkey, cache-aside) e `serveCached`

Toda rota de dados passa por `serveCached(chave, build)`:

1. Sem versão → 503 `dataset_not_ready`.
2. ETag = `W/"<hash>"`, com o FNV-1a de 64 bits de `versão + 0x00 + chave` em
   hexadecimal minúsculo, sem zeros à esquerda (`etagFor`). `If-None-Match` com o
   ETag atual (com ou sem `W/`, numa lista, ou `*`) → **304** sem corpo e sem
   consultar nada.
3. Chave no Valkey: `badblock:api-<fonte>:<versão>:<chave>`, com a consulta
   **normalizada** (a fonte lista as suas; ex.: `asn:61613` sem o `AS`, IP
   mapeado vira IPv4, bits de host do prefixo zerados). Rotas com e sem `/v1`
   usam a mesma chave e o mesmo ETag.
4. Achou → corpo do cache (`X-Cache: HIT`).
5. Não achou → a consulta roda **uma vez por chave** (singleflight), com prazo
   `DB_TIMEOUT` independente do cliente que chegou primeiro; o corpo JSON
   pronto vai para o Valkey se tiver até **8 MiB** (`8 << 20` bytes;
   `X-Cache: MISS`, ou `BYPASS` com o cache desligado).

Regras:

- TTL `REDIS_KEY_TTL` (3600 s). Versão nova = chaves novas; as antigas deixam
  de ser lidas e expiram sozinhas — nada é apagado.
- Só respostas 200 entram no cache; 404 e erros não.
- **Fail-open**: erro ou lentidão do Valkey (`REDIS_TIMEOUT`, 50 ms) vira
  "não achei" e a consulta vai ao Postgres. **Disjuntor**: 5 falhas seguidas
  abrem o circuito por 5 s (o Valkey nem é consultado). Contam como falha os
  erros de `Get` e de `Set`; chave ausente (`redis.Nil`) é sucesso e zera a
  contagem; o ping do `/status` ignora o disjuntor.
- `REDIS_URL` vazia ou `REDIS_CACHE_ENABLED=false` desligam o cache.
- `/meta`, `/status`, `/health`, `/ping`, `/openapi.yaml` e o índice nunca usam
  o Valkey.

## Cabeçalhos

Rotas de dados (inclusive o 304):

| Cabeçalho | Valor |
|---|---|
| `ETag` | fraco, `W/"<hash>"` (acima) |
| `Cache-Control` | `public, max-age=300` |
| `X-Cache` | `HIT` (também no 304), `MISS` ou `BYPASS` |
| `X-Dataset-Version` | versão dos dados |

O 304 sai sem corpo e sem `Content-Type`. O índice leva
`Cache-Control: public, max-age=300` (sem ETag); `/meta`, `/health`,
`/status`, `/ping` e todo erro levam `Cache-Control: no-store`.

Todas as respostas: `X-Content-Type-Options: nosniff`,
`Referrer-Policy: no-referrer`, `Access-Control-Allow-Origin: $CORS_ORIGIN`,
`Access-Control-Expose-Headers: ETag, X-Cache, X-Dataset-Version` e
`Server: badblock-api-<fonte>/<versão>`.

`OPTIONS` (qualquer caminho) responde o preflight de CORS: **204**, com
`Access-Control-Allow-Methods: GET, HEAD, OPTIONS`,
`Access-Control-Allow-Headers: If-None-Match, Content-Type` e
`Access-Control-Max-Age: 86400`.

## Rotas comuns

| Rota | Métodos | Resposta |
|---|---|---|
| `/<fonte>/` e `/<fonte>/v1/` | GET | índice: `app`, `version`, `base_path`, `versions` (`["v1"]`), `endpoints` (lista de rotas), `source` (URL da fonte) e campos da fonte (ex.: `registry` nos RIRs) |
| `/<fonte>/meta` e `/v1/meta` | GET | estado dos dados, sem cache: `app`, `version`, `dataset` (a última execução aplicada, com os campos que a fonte registra) e `collector` (`app`, `last_sync_at`, `last_check_at`, `consolidated` booleano); `dataset`/`collector` são `null` antes da primeira carga, e a meta responde 200 mesmo assim |
| `/<fonte>/health`, `/<fonte>/status` | GET, POST | saúde (abaixo) |
| `/<fonte>/ping` | GET | texto `pong`, `Cache-Control: no-store`; fora do log de acesso (é o healthcheck do Docker, a cada 15 s) |
| `/<fonte>/openapi.yaml` | GET | manifesto ([openapi.md](openapi.md)) |

### Saúde

```json
{"success": true, "status": "ok", "timestamp": "2026-09-29T00:54:27Z", "message": "api-<fonte> operacional",
 "checks": {"dataset": "ok", "postgres": "ok", "valkey": "ok"}}
```

| `status` | HTTP | Quando | `message` |
|---|---|---|---|
| `ok` | 200 | tudo no ar | `api-<fonte> operacional` |
| `starting` | 200 | ainda sem dataset | `aguardando a primeira sincronização do collector-<fonte>` |
| `degraded` | 200 | Valkey fora; responde sem cache | `Valkey indisponível; respondendo sem cache` |
| `error` | 503 | Postgres fora (`success: false`) | `PostgreSQL indisponível` |

`checks`: `postgres` (`ok`/`error`), `valkey` (`ok`/`error`/`disabled`),
`dataset` (`ok`/`empty`, pela versão em memória, sem consulta), com as chaves
em ordem alfabética. As verificações rodam nesta ordem — dataset, Valkey,
Postgres — e cada falha sobrescreve `status` e `message`: Postgres fora é
sempre `error`; Valkey fora é `degraded` mesmo sem dataset. Um prazo de 2 s
vale para a verificação inteira, e o ping do Valkey tem ainda o seu,
max(4 × `REDIS_TIMEOUT`, 200 ms). `Cache-Control: no-store`.

## Erros

Sempre JSON, com `Cache-Control: no-store`:

```json
{"error": {"code": "not_found", "message": "AS1 não consta no arquivo do RIR LACNIC"}}
```

| HTTP | `code` | Quando |
|---|---|---|
| 400 | `bad_request` | parâmetro inválido (a fonte define as validações) |
| 404 | `not_found` | registro ou rota inexistente |
| 503 | `dataset_not_ready` | antes da primeira carga do coletor: `a primeira sincronização do collector-<fonte> ainda não terminou; tente em alguns minutos` |
| 503 | `database_unavailable` | falha no Postgres (logada como `falha na consulta`): `banco de dados indisponível` |
| 504 | `timeout` | consulta passou de `DB_TIMEOUT` (logada como `consulta lenta`): `a consulta demorou demais` |
| 500 | `internal_error` | panic (recuperado e logado): `erro interno` — se o cabeçalho já tinha saído, o panic só vai para o log |

A validação vem antes de tudo: parâmetro inválido é 400 mesmo antes da
primeira carga. `store.ErrNotFound` sem mensagem própria da rota vira 404
`registro não encontrado`. As mensagens são em português e dizem o que foi pedido; as reais
de cada rota estão na spec da fonte e nos exemplos do manifesto.

## IP real e log de acesso

- `X-Forwarded-For` e `X-Real-IP` só valem quando a conexão vem de
  `TRUSTED_PROXIES`; lidos na ordem de `REAL_IP_HEADERS`. O
  `X-Forwarded-For` é lido da direita para a esquerda, pulando proxies
  confiáveis: o primeiro endereço não confiável é o cliente; uma entrada
  inválida interrompe a leitura; se todos os saltos são confiáveis, vale o
  mais à esquerda. Um IP solto em `TRUSTED_PROXIES` vale como `/32` ou
  `/128`.
- `client_ip_source`: `peer` (a conexão), `x-forwarded-for`, `x-real-ip` ou
  `unknown` (endereço da conexão ilegível).
- Com `ACCESS_LOG=true`, uma linha `http` por requisição (exceto `/ping`):
  `method`, `path`, `query`, `status`, `bytes`, `ms`, `client_ip`,
  `client_ip_source`, `cache`.

## Logs

JSON no stdout, `app=api-<fonte>` em toda linha:

| Mensagem | Nível | Campos |
|---|---|---|
| `iniciando` | info | `version`, `commit`, `port`, `base_path`, `postgres` (sem a senha), `cache` (`true` com `REDIS_URL` e `REDIS_CACHE_ENABLED`) |
| `Postgres indisponível; tentando de novo` / `sem conexão com o Postgres` | warn / error | `err` |
| `REDIS_URL inválida` | error | `err` (saída 2) |
| `Valkey indisponível no boot; seguindo sem cache até ele voltar` | warn | `err` |
| `não consegui ler a versão dos dados` | warn | `err` (a leitura da subida) |
| `versão do dataset` | info | `version`, `previous`, `applied_at` (quando a versão muda, inclusive na primeira leitura com dados) |
| `não consegui ler a versão do dataset` | warn | `err` (uma releitura) |
| `ouvindo` | info | `addr` |
| `http` | info | ver [IP real e log de acesso](#ip-real-e-log-de-acesso) |
| `consulta lenta` / `falha na consulta` | warn / error | `path`, `err` |
| `panic` | error | `path`, `panic` |
| `servidor HTTP parou` / `shutdown` | error | `err` (saída 1) |
| `encerrando` | info | — |

## Configuração comum

Ordem: padrão → variável de ambiente → argumento; variável vazia vale o
padrão; `--help` lista tudo com o nome da variável.

| Variável | Argumento | Padrão | Uso |
|---|---|---|---|
| `POSTGRES_URL` | `--postgres-url` | — (obrigatória) | `postgres://postgres:<senha>@badblock-postgres:5432/badblock?sslmode=disable` |
| `REDIS_URL` | `--redis-url` | vazio (sem cache) | `redis://:<senha>@badblock-valkey:6379/0` |
| `REDIS_CACHE_ENABLED` | `--redis-cache-enabled` | `true` | |
| `REDIS_KEY_TTL` | `--redis-key-ttl` | `3600` | segundos |
| `REDIS_TIMEOUT` | `--redis-timeout` | `50ms` | por operação no Valkey |
| `HTTP_PORT` | `--http-port` | `8080` | porta no container |
| `BASE_PATH` | `--base-path` | `/<fonte>` | a API responde tudo abaixo dele |
| `TRUSTED_PROXIES` | `--trusted-proxies` | `127.0.0.0/8,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,fc00::/7` | proxies cujos cabeçalhos de IP valem |
| `REAL_IP_HEADERS` | `--real-ip-headers` | `X-Forwarded-For,X-Real-IP` | ordem de leitura |
| `DB_POOL_MAX` | `--db-pool-max` | `10` | conexões no pool |
| `DB_TIMEOUT` | `--db-timeout` | `5s` | por consulta |
| `DATASET_POLL` | `--dataset-poll` | `30s` | releitura da versão dos dados |
| `CORS_ORIGIN` | `--cors-origin` | `*` | |
| `ACCESS_LOG` | `--access-log` | `true` | |
| `LOG_LEVEL` / `LOG_FORMAT` | `--log-level` / `--log-format` | `info` / `json` | |
| — | `--version` | | versão e sai |

Validação: `BASE_PATH` não pode ser vazio nem ter espaço, `{`, `}`, `?` ou
`#`; `REDIS_KEY_TTL` inteiro > 0; `REDIS_TIMEOUT`, `DB_TIMEOUT` e
`DATASET_POLL` durações > 0; `HTTP_PORT` de 1 a 65535; `DB_POOL_MAX` ≥ 1;
booleanos pelo `strconv.ParseBool`; `LOG_LEVEL` e `LOG_FORMAT` em qualquer
caixa; `--version` dispensa a `POSTGRES_URL`.

Opção inválida (stderr `api-<fonte>: <mensagem>`, saída 2):
`--base-path inválido: "<v>" (ex.: /<fonte>)`;
`--redis-cache-enabled inválido`, `--access-log inválido`,
`--redis-key-ttl inválido`, `--redis-timeout inválido`, `--db-timeout inválido`,
`--dataset-poll inválido`, `--http-port inválido`, `--db-pool-max inválido`,
`--log-level inválido`, `--log-format inválido` (todas com `: "<v>"`);
`proxy confiável inválido "<v>"`, `faixa de proxy confiável inválida "<v>"`,
`header de IP real não suportado "<v>" (use X-Forwarded-For e/ou X-Real-IP)`;
`defina POSTGRES_URL (ou --postgres-url)`; `argumento inesperado: <arg>`.

No compose, as variáveis do app ganham o prefixo `API_<FONTE>_` no `.env`:
`API_<FONTE>_TAG`, `API_<FONTE>_HOST_PORT`, `API_<FONTE>_CACHE_TTL`,
`API_<FONTE>_CACHE_ENABLED`, `API_<FONTE>_DB_POOL_MAX`,
`API_<FONTE>_REDIS_URL`; e as comuns `POSTGRES_URL`/`POSTGRES_PASSWORD`,
`VALKEY_PASSWORD`, `API_TRUSTED_PROXIES`, `API_FQDN`, `TRAEFIK_*`,
`API_RATE_*`, `LOG_LEVEL` ([../plataforma/docker.md](../plataforma/docker.md)).

## Container e publicação

- Imagem `tmsoftbrasil/badblock-api-<fonte>`, distroless, não-root, porta
  8080, `HEALTHCHECK` pelo subcomando `healthcheck` (15 s de intervalo).
- Container `badblock-api-<fonte>`, nas redes `badblock` (Postgres, Valkey) e
  `TRAEFIK_NETWORK` (publicação). Porta `API_<FONTE>_HOST_PORT` publicada só
  no loopback do host, para testes sem Traefik.
- Labels do Traefik (router, TLS, rate limit):
  [../plataforma/publicacao.md](../plataforma/publicacao.md#traefik).

## Testes

| Camada | Onde | O quê |
|---|---|---|
| Unitários (`make test`) | `httpapi` | todas as rotas com um store falso: sucesso, 400, 404, `HEAD`, 304, `/v1` = sem versão (mesma chave e ETag), `dataset_not_ready`, erro de banco, timeout, panic, `/status` nos quatro estados, CORS, redirect |
| Unitários | `httpapi/openapi_test.go` | manifesto × rotas registradas ([openapi.md](openapi.md#teste)) |
| Unitários | `cache`, `config`, `realip`, `dataset` | disjuntor e fail-open, ordem da configuração, proxies confiáveis, releitura da versão |
| Integração (`make test-int`) | `store` | PG18 descartável (testcontainers) com as migrations reais e dados inseridos pelo teste: cada consulta e o uso de índice |
| Fonte real (`make test-real`) | `httpapi` (RIRs), `store` (IANA) | nas fontes que têm: compila o coletor irmão (`../collector`), carrega a fonte real num PG18 descartável e mede as rotas. RIRs: `FILE=...`; IANA: sem `FILE` (baixa os 10 arquivos ou usa `IANA_REAL_DIR`); asnames: `ASNAMES_REAL_FILE=... make test-int`; rootzone: sem `FILE`, baixa o do dia; roothints e rootanchors: sem `FILE`, a fixture do coletor (roda também no `make test-int`); anatel/pst: `FILE=<zip>` ou, sem `FILE`, o ZIP do dia (com a fixture do coletor, roda também no `make test-int`) |
| Stack no ar (`make smoke`) | — | `/status`, `/meta`, uma consulta e `/openapi.yaml` pela porta do loopback |

Detalhes e comandos em [../processos/testes.md](../processos/testes.md).
