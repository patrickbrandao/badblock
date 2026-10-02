# Docker, composes e Makefiles

Cada app vira uma imagem e sobe sozinho a partir da própria pasta; o compose
da raiz só junta tudo para o desenvolvimento local. Publicação (Traefik,
release) em [publicacao.md](publicacao.md).

## `Dockerfile` (igual em todo app, trocando `<app>` e a descrição)

```dockerfile
# syntax=docker/dockerfile:1
#
# <app>: binário Go estático numa imagem distroless (sem shell), rodando
# como usuário não-root.

FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
# Build só com os arquivos da pasta: dependências de vendor/, sem rede.
ENV GOFLAGS=-mod=vendor GOPROXY=off GOTOOLCHAIN=local
WORKDIR /src
COPY . .
ARG TARGETOS TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
      -ldflags "-s -w \
        -X <module>/internal/buildinfo.Version=${VERSION} \
        -X <module>/internal/buildinfo.Commit=${COMMIT} \
        -X <module>/internal/buildinfo.Date=${BUILD_DATE}" \
      -o /out/<app> ./cmd/<app>

FROM gcr.io/distroless/static-debian13:nonroot
LABEL org.opencontainers.image.title="badblock-<app>" \
      org.opencontainers.image.description="<o que o app faz>" \
      org.opencontainers.image.source="https://github.com/patrickbrandao/badblock" \
      org.opencontainers.image.licenses="MIT"
COPY --from=build /out/<app> /<app>
USER nonroot:nonroot
ENTRYPOINT ["/<app>"]
```

A API acrescenta a frase `O healthcheck usa o próprio binário.` ao
comentário do cabeçalho e, antes do `ENTRYPOINT`:

```dockerfile
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 CMD ["/<app>", "healthcheck"]
```

O build cruza plataformas pelo Go (`--platform=$BUILDPLATFORM` +
`GOOS/GOARCH`), sem emulação. `<module>` é o module Go do app
([../projeto/estrutura.md](../projeto/estrutura.md#nomes)).

O build **não busca nada na internet**: o contexto é a pasta do app, copiada
inteira para o estágio de build, e as dependências Go vêm do `vendor/`
versionado nela ([../projeto/convencoes.md](../projeto/convencoes.md#go)).
`GOPROXY=off` faz o build falhar se faltar algo no `vendor/`, em vez de ir
ao proxy do Go (que leva ao GitHub); `GOTOOLCHAIN=local` impede o download de
outro toolchain. Da rede só vêm as imagens de base (e o frontend do
`# syntax`), do registro de imagens.

`.dockerignore` (igual em todo app): `bin/`, `coverage.out`, `*.test`,
`.env`, `.env.*`, `testdata/`, `README.md`.

## `docker-compose.yml` do app

Sobe o app sozinho, a partir da pasta, com um `.env` próprio (copie o
`.env.example`) ou pelo compose da raiz. A imagem vem do Docker Hub; se a tag
não existir, o compose a gera do código da pasta (`build`).

### Coletor

```yaml
name: badblock-collector-<fonte>

services:
  collector-<fonte>:
    image: ${DOCKERHUB_NAMESPACE:-tmsoftbrasil}/badblock-collector-<fonte>:${COLLECTOR_<FONTE>_TAG:-latest}
    build:
      context: .
      args:
        VERSION: ${COLLECTOR_<FONTE>_TAG:-dev}
    container_name: badblock-collector-<fonte>
    restart: unless-stopped
    environment:
      POSTGRES_URL: ${POSTGRES_URL:-postgres://postgres:${POSTGRES_PASSWORD:?defina POSTGRES_PASSWORD no .env}@badblock-postgres:5432/badblock?sslmode=disable}
      SYNC_INTERVAL: ${COLLECTOR_<FONTE>_SYNC_INTERVAL:-1h}
      RETRY_INTERVAL: ${COLLECTOR_<FONTE>_RETRY_INTERVAL:-5m}
      # ... opções da fonte, cada uma como ${COLLECTOR_<FONTE>_<OPÇÃO>:-<padrão ou vazio>}
      REMOVAL_THRESHOLD: ${COLLECTOR_<FONTE>_REMOVAL_THRESHOLD:-0.05}
      LOG_LEVEL: ${LOG_LEVEL:-info}
      LOG_FORMAT: json
    networks: [badblock]
    labels:
      traefik.enable: "false"        # o Traefik do servidor publica todo container por padrão
    logging:
      driver: json-file
      options: {max-size: 10m, max-file: "3"}

networks:
  badblock:
    name: badblock
    external: true
```

### API

```yaml
name: badblock-api-<fonte>

services:
  api-<fonte>:
    image: ${DOCKERHUB_NAMESPACE:-tmsoftbrasil}/badblock-api-<fonte>:${API_<FONTE>_TAG:-latest}
    build:
      context: .
      args:
        VERSION: ${API_<FONTE>_TAG:-dev}
    container_name: badblock-api-<fonte>
    restart: unless-stopped
    environment:
      POSTGRES_URL: ${POSTGRES_URL:-postgres://postgres:${POSTGRES_PASSWORD:?defina POSTGRES_PASSWORD no .env}@badblock-postgres:5432/badblock?sslmode=disable}
      REDIS_URL: ${API_<FONTE>_REDIS_URL:-redis://:${VALKEY_PASSWORD:-}@badblock-valkey:6379/0}
      REDIS_CACHE_ENABLED: ${API_<FONTE>_CACHE_ENABLED:-true}
      REDIS_KEY_TTL: ${API_<FONTE>_CACHE_TTL:-3600}
      HTTP_PORT: "8080"
      BASE_PATH: /<fonte>
      TRUSTED_PROXIES: ${API_TRUSTED_PROXIES:-127.0.0.0/8,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,fc00::/7}
      DB_POOL_MAX: ${API_<FONTE>_DB_POOL_MAX:-10}
      LOG_LEVEL: ${LOG_LEVEL:-info}
      LOG_FORMAT: json
    ports:
      - "127.0.0.1:${API_<FONTE>_HOST_PORT:-<porta>}:8080"
    networks: [badblock, traefik]
    labels:
      # ver publicacao.md#traefik
    logging:
      driver: json-file
      options: {max-size: 10m, max-file: "3"}

networks:
  badblock:
    name: badblock
    external: true
  traefik:
    name: ${TRAEFIK_NETWORK:-network_public}
    external: true
```

### `.env.example` do app

Usado só quando a pasta sobe sozinha (pelo compose da raiz valem os valores
do `.env` da raiz). Traz `POSTGRES_PASSWORD=` (e, comentada, a
`POSTGRES_URL` inteira para um banco fora da rede `badblock`), a tag e as
variáveis do app com prefixo; a API traz também `VALKEY_PASSWORD=`, a
`API_<FONTE>_REDIS_URL` comentada, `API_<FONTE>_HOST_PORT`,
`API_<FONTE>_CACHE_TTL`, os opcionais comentados e as variáveis do Traefik
(`API_FQDN`, `TRAEFIK_NETWORK`, `TRAEFIK_ENTRYPOINT`, `TRAEFIK_CERTRESOLVER`,
`API_RATE_AVERAGE`, `API_RATE_BURST`). Nunca valores reais.

## Compose da raiz (`docker-compose.yml`)

Projeto `badblock`. Só `include`, nesta ordem:

1. `database/postgres/docker-compose.yml`
2. `database/valkey/docker-compose.yml`
3. cada coletor, **com o override** de espera:
   `- path: [apps/<fonte>/collector/docker-compose.yml, compose.wait-migrate/collector-<fonte>.yml]`
4. cada API: `- apps/<fonte>/api/docker-compose.yml`
5. cada site: `- websites/<site>/docker-compose.yml`

Fontes (e sites) em ordem alfabética dentro de cada grupo. Imagem, compose e
Makefile dos sites: [../padroes/website.md](../padroes/website.md).

`compose.wait-migrate/collector-<fonte>.yml` — só no compose da raiz: o
coletor espera as migrations terminarem antes da primeira verificação
(sozinho, a partir da pasta do app, ele não depende do migrate e tenta de
novo em `RETRY_INTERVAL`). Um arquivo por coletor, porque cada `include` é um
modelo à parte e um override que citasse outros serviços criaria serviços sem
imagem:

```yaml
services:
  collector-<fonte>:
    depends_on:
      migrate:
        condition: service_completed_successfully
```

## Redes

| Rede | Criada por | Quem usa |
|---|---|---|
| `badblock` | `make network` (externa em todo compose) | todos os containers |
| `TRAEFIK_NETWORK` (padrão `network_public`) | o Traefik do servidor; localmente, `make network` | as APIs e os sites (os sites só nela) |

## Variáveis (`.env.example` da raiz)

Documenta o stack inteiro, sem valores reais; `make env` o copia para `.env`
e preenche `POSTGRES_PASSWORD` com um valor aleatório (`openssl rand -hex 24`,
ou `PG_PASSWORD=...`), com permissão 600. Cada seção abre com um comentário
`# --- <nome> ---` e explica as variáveis que não são óbvias. As tags ficam em
ordem alfabética de app; os blocos dos coletores, na ordem da
[tabela das fontes](../fontes/README.md) (cgibr, afrinic, apnic, arin, lacnic,
ripencc, iana, ripe/asnames, roothints, rootzone, rootanchors, anatel/pst), cada um com um comentário de uma linha dizendo o que
a fonte traz e o padrão das opções vazias. Seções, nesta ordem:

| Seção | Variáveis |
|---|---|
| PostgreSQL: acesso | `POSTGRES_PASSWORD`, `# POSTGRES_URL` |
| Valkey: senha | `VALKEY_PASSWORD` |
| Imagens | `DOCKERHUB_NAMESPACE=tmsoftbrasil`; `COLLECTOR_<FONTE>_TAG`, `API_<FONTE>_TAG` e `WEBSITE_<SITE>_TAG` (`latest`) de cada app |
| PostgreSQL: servidor | `POSTGRES_HOST_PORT=5433`, `POSTGRES_SHARED_BUFFERS=512MB`, `POSTGRES_MAX_CONNECTIONS=200` |
| Valkey | `VALKEY_MAXMEMORY=256mb` |
| Coletores | por coletor: `COLLECTOR_<FONTE>_SYNC_INTERVAL`, `_RETRY_INTERVAL`, as opções da fonte e `_REMOVAL_THRESHOLD` |
| APIs | `API_<FONTE>_HOST_PORT` e `API_<FONTE>_CACHE_TTL` de cada API |
| Sites | `WEBSITE_<SITE>_HOST_PORT` de cada site |
| Traefik | `API_FQDN`, `WEBSITE_DOMAIN`, `TRAEFIK_NETWORK`, `TRAEFIK_ENTRYPOINT`, `TRAEFIK_CERTRESOLVER`, `API_RATE_AVERAGE`, `API_RATE_BURST` |
| Geral | `LOG_LEVEL=info` |

Valores de cada fonte: `specs/fontes/<fonte>/collector.md` e `api.md`.

## Makefile dos apps

Variáveis: `APP := <app>`, `MODULE := github.com/patrickbrandao/badblock/apps/<fonte>/<tipo>`,
`ROOT := ../../..` (`../../../..` numa fonte de dois níveis, um nível mais funda), `VERSION` (da tag `<app>/vX.Y.Z` mais recente, ou `dev`),
`COMMIT`, `BUILD_DATE`, `IMAGE ?= tmsoftbrasil/badblock-$(APP)`,
`PLATFORMS ?= linux/amd64,linux/arm64`, `LDFLAGS` (os `-X` do buildinfo),
`GOLANGCI ?= golangci/golangci-lint:v2.14.0`,
`COMPOSE := docker compose --env-file $(ROOT)/.env`. As APIs de RIR têm
ainda `SOURCE := <fonte>`, `PORT := $${API_<FONTE>_HOST_PORT:-<porta>}` (o
teste do manifesto lê este `PORT`) e `SMOKE_ASN ?= <ASN de exemplo>`; as de
roothints, rootzone, rootanchors e anatel/pst também, com `SMOKE_SERVER`,
`SMOKE_TLD`, `SMOKE_KEY_TAG` e `SMOKE_CNPJ` no lugar de `SMOKE_ASN` (na
anatel/pst, `SOURCE := anatel/pst`); as de cgibr, iana e ripe/asnames ainda
não (a porta está fixa no `smoke` e no teste — pendência registrada na spec
de cada uma).

Todo `Makefile` (apps, raiz e `database/postgres/`) documenta cada alvo com
um comentário `## ` na mesma linha; `help` (o alvo padrão) lista esses
comentários.

| Alvo | Coletor | API | Faz |
|---|---|---|---|
| `help` | ✓ | ✓ | lista os alvos (padrão) |
| `build` | ✓ | ✓ | `bin/<app>` com os `LDFLAGS` |
| `test` | ✓ | ✓ | `go test -race -count=1 ./...` |
| `test-int` | ✓ | ✓ | `go test -race -count=1 -tags integration ./...` |
| `test-real` | nos que têm | nas que têm | testes com a fonte real inteira (hoje: RIRs, IANA e rootzone, coletor e API; roothints e rootanchors, só a API; anatel/pst, coletor e API). RIR: `FILE=...` obrigatório, passado em `<FONTE>_REAL_FILE`; IANA: sem `FILE`, baixa os 10 arquivos ou usa `IANA_REAL_DIR=<pasta>`; rootzone: sem `FILE`, baixa o `root.zone` do dia; APIs de roothints e rootanchors: sem `FILE`, usam a fixture do coletor; anatel/pst, coletor e API: `FILE=` (ZIP ou CSV no coletor; na API, sem `FILE` baixa o ZIP do dia) |
| `lint` | ✓ | ✓ | golangci-lint local ou pela imagem |
| `fmt`, `vet` | ✓ | ✓ | `go fmt ./...` (não toca no `vendor/`); `go vet -tags integration ./...` |
| `vendor` | ✓ | ✓ | `go mod tidy && go mod vendor`: refaz o `vendor/` depois de mudar dependência |
| `image` | ✓ | ✓ | `docker build` com `VERSION`, `COMMIT`, `BUILD_DATE` → `:$(VERSION)` e `:latest` |
| `push` | ✓ | ✓ | `docker buildx build --platform $(PLATFORMS) ... --push` (recusa `VERSION=dev`) |
| `up`, `down`, `logs` | ✓ | ✓ | o compose do app com o `.env` da raiz |
| `once`, `force` | ✓ | — | `docker exec badblock-<app> /<app> --once` / `--force` |
| `smoke` | — | ✓ | `curl -fsS` pela porta do loopback: RIRs em `/status`, `/meta`, `/asn/$(SMOKE_ASN)` e `/openapi.yaml`; roothints, rootzone e rootanchors em `/status`, `/meta`, a lista, um item (`$(SMOKE_SERVER)`, `$(SMOKE_TLD)`, `$(SMOKE_KEY_TAG)`) e `/openapi.yaml`; cgibr, iana e ripe/asnames em `/status`, uma consulta fixa e `/openapi.yaml` (sem `/meta`) |

## Makefile da raiz

| Alvo | Faz |
|---|---|
| `help` | lista os alvos (padrão) |
| `env` | gera o `.env` a partir do `.env.example` (não sobrescreve um `.env` existente): troca só a linha `POSTGRES_PASSWORD=` vazia pela senha (`PG_PASSWORD=...` ou `openssl rand -hex 24`) e deixa o arquivo com permissão 600 |
| `network` | cria as redes `badblock` e `TRAEFIK_NETWORK` se faltarem (`TRAEFIK_NETWORK` lida do `.env`, padrão `network_public`) |
| `up` | `env` + `network` + `docker compose up -d --build`; depois imprime a URL local de cada API, a partir da lista fixa `cgibr:8101 afrinic:8102 apnic:8103 arin:8104 lacnic:8105 ripencc:8106 iana:8107 ripe/asnames:8108 roothints:8109 rootzone:8110 rootanchors:8111 anatel/pst:8112` (a porta de cada uma pode ser trocada por `API_<FONTE>_HOST_PORT` no `.env`; numa fonte de dois níveis, `/` vira `_` na variável e `-` no nome do app) e a de cada site (`www:8201`, `WEBSITE_<SITE>_HOST_PORT`) |
| `down`, `ps`, `logs`, `build` | o compose da raiz (`ps -a`; `logs -f --tail=100`) |
| `migrate` | `make -C database/postgres migrate` |
| `test`, `test-int`, `lint`, `vet` | o alvo em cada app (`APPS := $(sort $(wildcard apps/*/collector apps/*/api apps/*/*/collector apps/*/*/api))`, que inclui as fontes de dois níveis), parando no primeiro que falha; `test-int` roda também `make -C database/postgres test`; `test` e `lint` rodam também em cada site (`WEBSITES := $(sort $(wildcard websites/*))`, exige Node) |
| `specs-check` | `sh specs/check.sh` ([../README.md](../README.md#verificação)) |
| `gitleaks` | [seguranca.md](seguranca.md) |
| `clean-data` | `docker compose down -v`: **apaga** os volumes (os coletores recarregam tudo) |
