# BadBlock — instruções para agentes

Vale para agentes de IA e para pessoas. Este arquivo diz **como trabalhar** e
**onde está cada coisa**; o conteúdo técnico do projeto mora em
[`specs/`](specs/README.md), a fonte da verdade. Não copie para cá o que é
das specs: aponte o arquivo.

## Regras de trabalho

1. **Spec primeiro.** Leia a spec do assunto antes de mexer; mudança de
   comportamento começa na spec, depois vêm o código e os testes
   ([fluxo de trabalho](specs/processos/fluxo-de-trabalho.md)).
2. **Cada app tem um sub-agente** (`.claude/agents/<app>.md`). Trabalho num
   app vai para ele; quem é dono de cada spec está em
   [fluxo de trabalho](specs/processos/fluxo-de-trabalho.md#quem-cuida-de-quê).
3. **Não leia `old/` nem `tmp/`** — só com pedido explícito do usuário para
   analisar algo nelas.
4. **O repositório é público**: nenhum segredo no git
   ([segurança](specs/plataforma/seguranca.md)).
5. **Commit, push, tag e publicação de imagem só com pedido explícito.**
   `run-prod.sh` e `run-builder.sh` nunca mudam sem pedido explícito.
6. **Idioma**: português (PT-BR) em specs, docs, comentários, logs e commits;
   inglês em identificadores, SQL e JSON ([convenções](specs/projeto/convencoes.md)).
7. **Antes de entregar**: o que [specs/processos/testes.md](specs/processos/testes.md#antes-de-um-pr)
   manda rodar.

## Mapa das specs

| Assunto | Arquivo |
|---|---|
| Índice, roteiros de leitura, regras de escrita das specs | [specs/README.md](specs/README.md) |
| O que é o projeto | [specs/projeto/visao-geral.md](specs/projeto/visao-geral.md) |
| Árvore do repositório e **nomes** (apps, pastas, imagens, tabelas, variáveis) | [specs/projeto/estrutura.md](specs/projeto/estrutura.md) |
| Regras de arquitetura | [specs/projeto/arquitetura.md](specs/projeto/arquitetura.md) |
| Convenções de código, configuração, testes, lint, git | [specs/projeto/convencoes.md](specs/projeto/convencoes.md) |
| Glossário | [specs/projeto/glossario.md](specs/projeto/glossario.md) |
| Decisões tomadas e em aberto | [specs/projeto/decisoes.md](specs/projeto/decisoes.md) |
| PostgreSQL, migrations, tabela `jobs` | [specs/plataforma/postgres.md](specs/plataforma/postgres.md) |
| Valkey | [specs/plataforma/valkey.md](specs/plataforma/valkey.md) |
| Dockerfile, composes, `.env`, Makefiles | [specs/plataforma/docker.md](specs/plataforma/docker.md) |
| Traefik, release de imagens, produção | [specs/plataforma/publicacao.md](specs/plataforma/publicacao.md) |
| Segredos e gitleaks | [specs/plataforma/seguranca.md](specs/plataforma/seguranca.md) |
| Padrão de todo coletor | [specs/padroes/coletor.md](specs/padroes/coletor.md) |
| Padrão de toda API | [specs/padroes/api.md](specs/padroes/api.md) |
| Manifesto OpenAPI | [specs/padroes/openapi.md](specs/padroes/openapi.md) |
| Padrão de todo site (`websites/<site>/`) | [specs/padroes/website.md](specs/padroes/website.md) |
| Fontes de dados (tabela, pasta de uma fonte, donos) | [specs/fontes/README.md](specs/fontes/README.md) |
| Modelo dos cinco RIRs | [specs/fontes/rir/README.md](specs/fontes/rir/README.md) |
| Fluxo de trabalho e donos | [specs/processos/fluxo-de-trabalho.md](specs/processos/fluxo-de-trabalho.md) |
| Testes | [specs/processos/testes.md](specs/processos/testes.md) |
| Nova fonte de dados | [specs/processos/nova-fonte.md](specs/processos/nova-fonte.md) |
| Reconstruir o projeto a partir das specs | [specs/processos/reconstrucao.md](specs/processos/reconstrucao.md) |

## Mapa do código

| Caminho | O quê | Specs |
|---|---|---|
| `apps/<fonte>/collector/` | app `collector-<fonte>` | [specs/padroes/coletor.md](specs/padroes/coletor.md) + `specs/fontes/<fonte>/` |
| `apps/<fonte>/api/` | app `api-<fonte>` | [specs/padroes/api.md](specs/padroes/api.md) + `specs/fontes/<fonte>/` |
| `apps/<site>/<conjunto>/{collector,api}/` | fonte de dois níveis (ex.: `anatel/pst` → `collector-anatel-pst`) | [specs/projeto/estrutura.md](specs/projeto/estrutura.md#fonte-de-dois-níveis-siteconjunto) |
| `websites/<site>/` | app `website-<site>` (sub-agente de mesmo nome) | [specs/padroes/website.md](specs/padroes/website.md) |
| `database/postgres/` | servidor, migrations, rotinas | [specs/plataforma/postgres.md](specs/plataforma/postgres.md) |
| `database/valkey/` | cache | [specs/plataforma/valkey.md](specs/plataforma/valkey.md) |
| raiz (`Makefile`, `docker-compose.yml`, `compose.wait-migrate/`, `release-images.sh`, `.env.example`) | integração e publicação | [specs/plataforma/docker.md](specs/plataforma/docker.md), [specs/plataforma/publicacao.md](specs/plataforma/publicacao.md) |
| raiz (`.gitignore`, `.gitleaks.toml`, `.editorconfig`, `LICENSE`) | repositório | [specs/projeto/estrutura.md](specs/projeto/estrutura.md), [specs/plataforma/seguranca.md](specs/plataforma/seguranca.md), [specs/projeto/convencoes.md](specs/projeto/convencoes.md#lint-e-formatação) |
| `.claude/agents/` | sub-agentes | [specs/processos/fluxo-de-trabalho.md](specs/processos/fluxo-de-trabalho.md) |

## Fontes, apps e sub-agentes

| Fonte | Specs | Coletor (app e sub-agente) | API (app e sub-agente) |
|---|---|---|---|
| `afrinic` | [specs/fontes/afrinic/](specs/fontes/afrinic/README.md) | `collector-afrinic` | `api-afrinic` |
| `apnic` | [specs/fontes/apnic/](specs/fontes/apnic/README.md) | `collector-apnic` | `api-apnic` |
| `arin` | [specs/fontes/arin/](specs/fontes/arin/README.md) | `collector-arin` | `api-arin` |
| `ripe/asnames` | [specs/fontes/ripe/asnames/](specs/fontes/ripe/asnames/README.md) | `collector-ripe-asnames` | `api-ripe-asnames` |
| `cgibr` | [specs/fontes/cgibr/](specs/fontes/cgibr/README.md) | `collector-cgibr` | `api-cgibr` |
| `iana` | [specs/fontes/iana/](specs/fontes/iana/README.md) | `collector-iana` | `api-iana` |
| `lacnic` | [specs/fontes/lacnic/](specs/fontes/lacnic/README.md) | `collector-lacnic` | `api-lacnic` |
| `ripencc` | [specs/fontes/ripencc/](specs/fontes/ripencc/README.md) | `collector-ripencc` | `api-ripencc` |
| `rootanchors` | [specs/fontes/rootanchors/](specs/fontes/rootanchors/README.md) | `collector-rootanchors` | `api-rootanchors` |
| `anatel/pst` | [specs/fontes/anatel/pst/](specs/fontes/anatel/pst/README.md) | `collector-anatel-pst` | `api-anatel-pst` |
| `roothints` | [specs/fontes/roothints/](specs/fontes/roothints/README.md) | `collector-roothints` | `api-roothints` |
| `rootzone` | [specs/fontes/rootzone/](specs/fontes/rootzone/README.md) | `collector-rootzone` | `api-rootzone` |
