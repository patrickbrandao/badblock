# Estrutura do repositório e nomes

## Árvore

```
badblock/
├── AGENTS.md               regras para agentes de IA e mapa destas specs
├── CLAUDE.md               importa o AGENTS.md (+ o que é só do Claude Code)
├── README.md               entrada para pessoas
├── LICENSE                 MIT
├── Makefile                rotinas da raiz (stack local, testes de todos os apps, specs-check)
├── docker-compose.yml      junta todos os composes para o desenvolvimento local
├── compose.wait-migrate/   um override por coletor: espera as migrations (só no compose da raiz)
├── release-images.sh       publica as imagens multi-arch a partir das tags <app>/vX.Y.Z
├── .env.example            variáveis do stack completo (o .env real fica fora do git)
├── .editorconfig  .gitignore  .gitleaks.toml
├── .claude/agents/         um sub-agente por app (<app>.md)
├── specs/                  ESTAS SPECS — fonte da verdade do projeto
│   ├── README.md           índice e regras das specs
│   ├── check.sh            verificação automática das specs (make specs-check)
│   ├── projeto/            visão, estrutura, arquitetura, convenções, glossário, decisões
│   ├── plataforma/         PostgreSQL, Valkey, Docker, publicação, segurança
│   ├── padroes/            o que é comum a todos os coletores, APIs e manifestos
│   ├── fontes/             uma pasta por fonte (+ rir/, modelo dos cinco RIRs)
│   └── processos/          fluxo de trabalho, testes, nova fonte, reconstrução
├── apps/
│   ├── <fonte>/            uma pasta por fonte, com os apps dela
│   │   ├── collector/      app collector-<fonte>
│   │   └── api/            app api-<fonte>
│   └── <site>/<conjunto>/  fonte de dois níveis (ex.: anatel/pst), mesmos apps
├── websites/
│   └── <site>/             app website-<site>: site estático (Vite + React + lighttpd)
└── database/
    ├── postgres/           servidor, migrations (central/ e uma pasta por fonte) e rotinas
    └── valkey/             cache das APIs
```

Fora do git (no `.gitignore`), presentes só na máquina do mantenedor:

| Caminho | O que é |
|---|---|
| `.env` (e qualquer `.env.*` que não seja `.env.example`) | valores reais: senhas, domínio |
| `.mcp.json` | servidores MCP do projeto (skills `badblock-dev`), com token |
| `old/`, `tmp/` | versão anterior do projeto e material de referência. **Não são lidas** sem pedido explícito do usuário |
| `build.sh`, `destroy.sh`, `hub.sh`, `sync.sh`, `run-*.sh`, `Update_and_fixes` | scripts de operação do mantenedor ([../plataforma/publicacao.md](../plataforma/publicacao.md#scripts-do-mantenedor)) |
| `bin/`, `dist/`, `coverage.out`, `*.test` | artefatos de build e teste |
| `websites/*/node_modules/`, `dist/`, `*.log` | dependências, build e logs locais dos sites |

## Pasta de um app

| Arquivo | Coletor | API | Conteúdo |
|---|---|---|---|
| `go.mod`, `go.sum` | ✓ | ✓ | módulo próprio ([convencoes.md](convencoes.md#go)) |
| `vendor/` | ✓ | ✓ | dependências Go, geradas por `make vendor` e versionadas; o build da imagem usa só elas ([convencoes.md](convencoes.md#go)) |
| `cmd/<app>/main.go` | ✓ | ✓ | ponto de entrada |
| `internal/...` | ✓ | ✓ | pacotes ([../padroes/coletor.md](../padroes/coletor.md#estrutura-do-código), [../padroes/api.md](../padroes/api.md#estrutura-do-código)) |
| `testdata/` | ✓ | quando precisa | recortes reais da fonte |
| `openapi/openapi.yaml`, `openapi/embed.go` | — | ✓ | manifesto ([../padroes/openapi.md](../padroes/openapi.md)) |
| `Dockerfile`, `.dockerignore` | ✓ | ✓ | [../plataforma/docker.md](../plataforma/docker.md) |
| `docker-compose.yml`, `.env.example` | ✓ | ✓ | sobe o app sozinho a partir da pasta |
| `Makefile` | ✓ | ✓ | build, testes, imagem, operação |
| `.golangci.yml` | ✓ | ✓ | lint ([convencoes.md](convencoes.md#lint-e-formatação)) |
| `README.md` | ✓ | ✓ | curto: o que é, comandos, link para a spec da fonte |

As specs **não** ficam dentro do app: moram em `specs/fontes/<fonte>/`, para
sobreviver mesmo que o código seja apagado.

## Nomes

O nome da fonte é a chave de tudo: um token minúsculo, **sem hífen**
(`^[a-z0-9]+$`), porque entra em nomes SQL e em `jobs.app`.

| Coisa | Forma | Exemplo (`cgibr`) |
|---|---|---|
| Fonte | `<fonte>` | `cgibr` |
| Apps | `collector-<fonte>`, `api-<fonte>` | `collector-cgibr`, `api-cgibr` |
| Pastas dos apps | `apps/<fonte>/collector/`, `apps/<fonte>/api/` | `apps/cgibr/collector/` |
| Module Go | `github.com/patrickbrandao/badblock/apps/<fonte>/<collector\|api>` | `.../apps/cgibr/api` |
| Binário e `cmd/` | `cmd/<app>/main.go` → `/<app>` na imagem | `/api-cgibr` |
| Imagem | `tmsoftbrasil/badblock-<app>` | `tmsoftbrasil/badblock-api-cgibr` |
| Container | `badblock-<app>` | `badblock-collector-cgibr` |
| Serviço e projeto compose | serviço `<app>`, projeto `badblock-<app>` | `api-cgibr` |
| Tag de release | `<app>/vX.Y.Z` | `api-cgibr/v0.1.0` |
| Tabelas | `<fonte>_<tabela>` | `cgibr_asn`, `cgibr_run` |
| Migrations | `database/postgres/<fonte>/` (controle `<fonte>_schema_migrations`) | `database/postgres/cgibr/` |
| Linha em `jobs` | `app = 'collector-<fonte>'` | `collector-cgibr` |
| Caminho HTTP | `/<fonte>/` | `/cgibr/asn/61613` |
| Variáveis no `.env` da raiz | `COLLECTOR_<FONTE>_*`, `API_<FONTE>_*` | `API_CGIBR_HOST_PORT` |
| Porta no loopback | `81NN`, uma por API ([../fontes/README.md](../fontes/README.md)) | `8101` |
| Chave de cache | `badblock:api-<fonte>:<versão>:<consulta>` | `badblock:api-cgibr:0190…:asn:61613` |
| Traefik | router e serviço `badblock-api-<fonte>`, middleware `badblock-api-<fonte>-ratelimit` | |
| User-Agent do coletor | `badblock-collector-<fonte>/<versão> (+https://github.com/patrickbrandao/badblock)` | |
| Cabeçalho `Server` da API | `badblock-api-<fonte>/<versão>` | |
| Sub-agentes | `.claude/agents/<app>.md` | `.claude/agents/api-cgibr.md` |
| Specs | `specs/fontes/<fonte>/` | `specs/fontes/cgibr/` |
| Override do compose da raiz | `compose.wait-migrate/collector-<fonte>.yml` | |

Os sites (`websites/<site>/`, app `website-<site>`) têm a própria tabela de
nomes em [../padroes/website.md](../padroes/website.md#nomes).

### Fonte de dois níveis (`<site>/<conjunto>`)

Um site que publica vários conjuntos de dados independentes (ex.: a Anatel)
tem uma fonte por conjunto, com dois tokens `^[a-z0-9]+$`: `<site>/<conjunto>`
([decisoes.md](decisoes.md) #21). O nome muda de forma conforme o lugar:

| Forma | Onde | `anatel/pst` |
|---|---|---|
| caminho (`/`) | pastas `apps/`, `specs/fontes/`, module Go, caminho HTTP e `BASE_PATH` | `apps/anatel/pst/collector/`, `specs/fontes/anatel/pst/`, `/anatel/pst/` |
| nome (`-`) | apps, binário, imagem, container, serviço e projeto compose, tag, `jobs.app`, chave de cache, Traefik, User-Agent, `Server`, sub-agentes, override do compose da raiz | `collector-anatel-pst`, `tmsoftbrasil/badblock-api-anatel-pst`, `badblock:api-anatel-pst:…` |
| SQL (`_`) | tabelas, pasta de migrations e a tabela de controle dela | `anatel_pst_provider`, `database/postgres/anatel_pst/`, `anatel_pst_schema_migrations` |
| variável (`_`, maiúsculas) | `.env` da raiz | `COLLECTOR_ANATEL_PST_SYNC_INTERVAL`, `API_ANATEL_PST_HOST_PORT` |

Nas tabelas de nomes acima, `<fonte>` vale a forma do lugar. A pasta do site
(`apps/<site>/`, `specs/fontes/<site>/`) não é uma fonte: em `specs/` ela tem
só um `README.md` com os conjuntos do site.

Nomes SQL nunca levam hífen; identificadores de código e campos JSON são em
inglês ([convencoes.md](convencoes.md)).
