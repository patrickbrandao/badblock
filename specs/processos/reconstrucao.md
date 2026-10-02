# Reconstrução a partir das specs

Se o código for perdido, estas specs bastam para refazer o projeto. A ordem
abaixo vai do que não depende de nada ao que depende de tudo; cada passo diz
de onde vem a verdade e como saber que ficou certo.

Os fatos medidos nas specs (contagens, tempos, tamanhos) são de uma data:
refaça as medições com os arquivos do dia e atualize as specs se mudaram.

## 1. Esqueleto do repositório

- Árvore, arquivos da raiz e o que fica fora do git:
  [../projeto/estrutura.md](../projeto/estrutura.md).
- `.editorconfig`, `.gitignore`, `.gitleaks.toml`:
  [../projeto/convencoes.md](../projeto/convencoes.md#lint-e-formatação),
  [../projeto/estrutura.md](../projeto/estrutura.md),
  [../plataforma/seguranca.md](../plataforma/seguranca.md).
- `LICENSE`: MIT.

**Pronto quando**: a árvore bate com `estrutura.md` e `make gitleaks` passa.

## 2. Banco e cache

- `database/postgres/` (compose, `migrate.sh`, `test-migrations.sh`,
  `Makefile`, `.env.example`, `central/` com `set_updated_at()` e `jobs`):
  [../plataforma/postgres.md](../plataforma/postgres.md).
- Uma migration por fonte, a partir de `specs/fontes/<fonte>/dados.md` (nos
  RIRs, [../fontes/rir/dados.md](../fontes/rir/dados.md) com o nome de cada
  RIR).
- `database/valkey/`: [../plataforma/valkey.md](../plataforma/valkey.md).

**Pronto quando**: `make -C database/postgres test` passa e `\d+` de cada
tabela bate com o `dados.md`.

## 3. Coletores

Para cada fonte, na ordem: `cgibr`, `lacnic` (depois os quatro clones de
RIR), `iana`, `ripe/asnames`, `roothints`, `rootzone`, `rootanchors`, `anatel/pst`.

- Estrutura, laço, verificação, aplicação e configuração comuns:
  [../padroes/coletor.md](../padroes/coletor.md).
- Da fonte: `specs/fontes/<fonte>/fonte.md` (formato, parser, fixtures),
  `dados.md` e `collector.md`. RIR: primeiro o modelo
  ([../fontes/rir/](../fontes/rir/README.md)) no `lacnic`, depois clone com
  os parâmetros de cada RIR.
- Fixtures: recorte dos arquivos reais conforme o `fonte.md`.
- Build e deploy: [../plataforma/docker.md](../plataforma/docker.md).

**Pronto quando**: `make test test-int lint vet` passa; `make test-real` com
o arquivo do dia carrega sem descartes inesperados e as contagens batem com
os fatos da spec (dentro da variação diária); uma segunda verificação diz
"fonte sem mudança".

## 4. APIs

- Estrutura, rotas comuns, cache, ETag, erros e configuração:
  [../padroes/api.md](../padroes/api.md).
- Rotas da fonte, exemplos reais, chaves de cache: `specs/fontes/<fonte>/api.md`
  (RIR: [../fontes/rir/api.md](../fontes/rir/api.md) + o `api.md` do RIR).
- Manifesto `openapi/openapi.yaml` gerado a partir do `api.md`, com as regras
  de [../padroes/openapi.md](../padroes/openapi.md).

**Pronto quando**: `make test test-int lint vet` passa (inclusive o
`openapi_test.go`); com o coletor carregado, cada exemplo do `api.md`
responde com a mesma forma (valores do dia); `make smoke` passa.

## 5. Integração e publicação

- `docker-compose.yml` da raiz, `compose.wait-migrate/`, `.env.example`,
  `Makefile` da raiz: [../plataforma/docker.md](../plataforma/docker.md).
- `release-images.sh`, Traefik:
  [../plataforma/publicacao.md](../plataforma/publicacao.md).
- `AGENTS.md`, `CLAUDE.md`, `README.md` e `.claude/agents/`: mapas e papéis,
  sem conteúdo técnico ([fluxo-de-trabalho.md](fluxo-de-trabalho.md)).

**Pronto quando**: `make up` sobe tudo do zero, cada coletor faz a primeira
carga, cada API responde `status: ok` e `make specs-check` passa.
