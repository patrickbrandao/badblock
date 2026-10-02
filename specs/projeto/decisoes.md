# Decisões

Registro das decisões de projeto: o que foi decidido, quando e por quê. Uma
decisão nova entra no fim; uma que muda outra a cita. Antes de propor o
contrário de uma decisão daqui, leia o motivo.

| # | Data | Decisão | Motivo |
|---|---|---|---|
| 1 | 2026-09-28 | Redesign para simplificar: tudo o que existia foi para `old/` (fora do git); a fase 1 recomeçou com um par coletor/API por fonte | A versão anterior (registry-sync/registry-api) misturava fontes e responsabilidades |
| 2 | 2026-09-28 | Coletor e API são apps separados, sem código compartilhado; o contrato é o banco | Deploy, teste e evolução independentes; nenhuma mudança num app quebra outro |
| 3 | 2026-09-28 | Prefixo das tabelas = nome da fonte (`cgibr_*`), não o nome do app | Nomes SQL sem hífen; o mesmo nome nos dois apps |
| 4 | 2026-09-28 | Postgres dedicado, todos os serviços com o usuário `postgres` no banco `badblock`, só por `POSTGRES_URL`; sem roles, `GRANT`, bootstrap nem backup | Simplicidade: os dados são descartáveis e recarregáveis das fontes |
| 5 | 2026-09-28 | dbmate com uma pasta por fonte e tabela de controle `<pasta>_schema_migrations`; `central/` sempre primeiro | Cada fonte evolui o schema sozinha, sem conflito de numeração |
| 6 | 2026-09-28 | Tabela `jobs` com `consolidated` 0/1 como contrato com a fase 2 | A consolidação futura sabe o que mudou sem conhecer cada fonte |
| 7 | 2026-09-28 | Imagens distroless não-root; healthcheck da API pelo próprio binário (`healthcheck`) | Superfície mínima: a imagem não tem shell nem curl |
| 8 | 2026-09-28 | Containers internos com `traefik.enable=false` | O Traefik do servidor publica todo container por padrão (`exposedByDefault`) |
| 9 | 2026-09-28 | Scripts de operação do mantenedor fora do git; só `release-images.sh` versionado; `run-prod.sh`/`run-builder.sh` não se editam sem pedido explícito | Guardam valores do ambiente dele; o repositório é público |
| 10 | 2026-09-29 | Cada RIR é uma fonte (`afrinic`, `apnic`, `arin`, `lacnic`, `ripencc`), não uma fonte `rir` com cinco arquivos | O nome da fonte é um token sem hífen que casa com `jobs.app`; cada RIR publica e muda num ritmo próprio |
| 11 | 2026-09-29 | Os cinco pares de RIR são o mesmo código, clonado do lacnic; o específico fica em `internal/rir/rir.go` | Mesmo formato (delegated-extended); sem código compartilhado entre apps (#2) |
| 12 | 2026-09-29 | `compose.wait-migrate/` é uma pasta com um override por coletor | Um override único com vários serviços cria serviços sem imagem em cada `include` |
| 13 | 2026-09-29 | `collector-iana` verifica a cada 6h | A IANA muda poucas vezes por ano e o Cloudflare guarda os arquivos por 1h/24h (ver `specs/fontes/iana/collector.md`) |
| 14 | 2026-09-29 | Toda API tem manifesto OpenAPI 3.1 embutido; a v1 fixa é um *servidor*, não caminhos `/v1` duplicados | Caminhos duplicados repetiriam `operationId`, o que a especificação proíbe |
| 15 | 2026-09-29 | Apps agrupados por fonte: `apps/<fonte>/collector/` e `apps/<fonte>/api/` (antes `apps/collector-<fonte>/`); nomes de app, imagem, container, tag e `jobs.app` mantidos | Um site com vários conjuntos de dados fica numa pasta só; nada muda em produção |
| 16 | 2026-09-29 | Specs centralizadas em `specs/` (fora dos apps), em camadas: projeto, plataforma, padrões, fontes (com um modelo único para os RIRs) e processos; `AGENTS.md`/`CLAUDE.md` só com regras de trabalho e o mapa | As specs têm de permitir reconstruir o projeto mesmo se o código for apagado, sem repetir o mesmo texto em 16 lugares |
| 17 | 2026-09-29 | O manifesto OpenAPI continua dentro do app, derivado da spec | O `go:embed` não enxerga fora do módulo e o build usa só a pasta do app |
| 18 | 2026-09-30 | Os três arquivos da raiz do DNS são três fontes: `roothints` (`named.root`), `rootzone` (`root.zone`) e `rootanchors` (`root-anchors.xml`), cada uma com o seu par de apps; nenhuma entra na `iana` nem numa fonte `internic` | Cada arquivo muda num ritmo próprio (`root.zone` cerca de 2 vezes por dia; os outros, poucas vezes por ano), como os RIRs (#10); assim não é preciso fechar a questão de vários conjuntos por site (abaixo) |
| 19 | 2026-09-30 | Sites em `websites/<site>/`, app `website-<site>`: Vite + React + Tailwind (JavaScript), build em `dist/` servido na raiz do host por lighttpd não-root no Alpine; o `www` responde pelo domínio puro e pelo `www`, sem redirecionar um para o outro | Sem servidor Node em produção (menos superfície, imagem pequena); cada site implantado sozinho, como os outros apps |
| 20 | 2026-09-30 | Dependências Go vendorizadas (`vendor/` versionado em cada app); o Dockerfile copia só a pasta do app e compila com `GOFLAGS=-mod=vendor GOPROXY=off`, sem `go mod download` | O build da imagem usa só os arquivos do projeto e não busca nada no GitHub nem no proxy do Go; as versões das dependências são as que estão no git. O `vendor/` igual entre apps não pesa no repositório (o git guarda cada arquivo uma vez) |
| 21 | 2026-09-30 | Site com vários conjuntos de dados: cada conjunto é uma fonte de **dois níveis**, `<site>/<conjunto>` (ex.: `anatel/pst`), com o seu par de apps. O caminho (pastas, specs, HTTP) usa `/`; nomes de app, imagem, container, tag e `jobs.app` usam `-` (`collector-anatel-pst`); nomes SQL (tabelas, pasta de migrations) usam `_` (`anatel_pst_*`); variáveis usam `_` em maiúsculas (`API_ANATEL_PST_*`). Fecha a questão em aberto de vários conjuntos por site | A Anatel publica vários conjuntos independentes (cada um com formato e ritmo próprios, como #10 e #18); agrupá-los por site mantém `apps/anatel/` e `specs/fontes/anatel/` juntos, e cada forma do nome continua válida onde é usada (SQL sem hífen, `jobs.app` sem `_`) |
| 22 | 2026-10-02 | O `asn.txt` do RIPE NCC passa a ser a fonte de dois níveis `ripe/asnames` (antes `asnames`): apps `collector-ripe-asnames` e `api-ripe-asnames`, caminho `/ripe/asnames/`, tabelas `ripe_asnames_*` | O arquivo é publicado pelo RIPE NCC (`ftp.ripe.net/ripe/asnames/`) e deve ficar no domínio do RIPE; as delegações continuam na fonte de um nível `ripencc` (família RIR) |

## Em aberto

- **Fase 2** (consolidação): não especificada.
