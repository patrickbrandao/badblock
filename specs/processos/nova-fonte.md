# Nova fonte de dados

Passo a passo para acrescentar uma fonte. As specs vêm antes do código.

## 1. Decidir

- **Nome da fonte**: token minúsculo sem hífen (`^[a-z0-9]+$`), curto e
  estável — batiza apps, tabelas, caminho HTTP, variáveis e specs
  ([../projeto/estrutura.md](../projeto/estrutura.md#nomes)).
- **Família**: se a fonte usa um formato que já tem modelo (ex.: um RIR com
  delegated-extended), ela é um clone do modelo
  ([../fontes/rir/README.md](../fontes/rir/README.md)); senão, um par novo no
  molde do `cgibr`.
- **Porta local** da API: a próxima livre na faixa `81NN`
  ([../fontes/README.md](../fontes/README.md)).
- Um site com mais de um conjunto de dados (ex.: Anatel): uma fonte de dois
  níveis por conjunto, `<site>/<conjunto>`, com o nome na forma de cada lugar
  (`apps/anatel/pst/`, `collector-anatel-pst`, `anatel_pst_*`)
  ([../projeto/estrutura.md](../projeto/estrutura.md#fonte-de-dois-níveis-siteconjunto)).
  Nos passos abaixo, `<fonte>` vale a forma do lugar; o primeiro conjunto de
  um site cria também `specs/fontes/<site>/README.md`.

## 2. Specs

1. `specs/fontes/<fonte>/` com `README.md`, `fonte.md`, `dados.md`,
   `collector.md` e `api.md` ([../fontes/README.md](../fontes/README.md#pasta-de-uma-fonte)).
   Meça o arquivo real e registre os fatos com data.
2. Linha da fonte na tabela de [../fontes/README.md](../fontes/README.md) e no
   mapa de [../README.md](../README.md#fontes).

## 3. Banco

`make -C database/postgres new APP=<fonte> NAME=<fonte>` e escreva a
migration das tabelas `<fonte>_*` (inclusive `<fonte>_run`) conforme o
`dados.md`, no estilo de [../plataforma/postgres.md](../plataforma/postgres.md#estilo-das-tabelas).
Teste com `make -C database/postgres test`.

## 4. Apps

1. `apps/<fonte>/collector/` e `apps/<fonte>/api/` (clone do modelo ou no
   molde do `cgibr`), com todos os arquivos de
   [../projeto/estrutura.md](../projeto/estrutura.md#pasta-de-um-app);
   `go.mod` com o module `github.com/patrickbrandao/badblock/apps/<fonte>/<tipo>`
   e o `vendor/` gerado por `make vendor` (versionado).
2. O manifesto `openapi/openapi.yaml` da API, derivado do `api.md`
   ([../padroes/openapi.md](../padroes/openapi.md)).
3. Recortes reais da fonte em `testdata/`.
4. `make test test-int lint vet` em cada app; `make test-real FILE=...` com o
   arquivo do dia.

## 5. Sub-agentes

`.claude/agents/collector-<fonte>.md` e `.claude/agents/api-<fonte>.md`, no
[modelo de sub-agente](fluxo-de-trabalho.md#modelo-de-um-sub-agente) — sem
repetir o conteúdo das specs.

## 6. Integração na raiz

| Onde | O quê |
|---|---|
| `docker-compose.yml` | include do coletor com `compose.wait-migrate/collector-<fonte>.yml` e include da API, em ordem alfabética |
| `compose.wait-migrate/collector-<fonte>.yml` | o override de espera ([../plataforma/docker.md](../plataforma/docker.md#compose-da-raiz-docker-composeyml)) |
| `.env.example` | tags, variáveis do coletor, porta e TTL da API |
| `release-images.sh` | os dois apps em `APPS` |
| `Makefile` | a porta no laço do alvo `up` |
| `README.md` | a linha da fonte na tabela |
| `AGENTS.md` | a linha da fonte na tabela "Fontes, apps e sub-agentes" |
| `.gitleaks.toml` | nada, se as fixtures ficam em `testdata/` |
| `run-prod.sh`, `run-builder.sh` | um bloco por app — **só o mantenedor edita**: descreva o bloco no relatório |

## 7. Conferir

```bash
make test test-int lint vet
make specs-check
make gitleaks
make up && make -C apps/<fonte>/api smoke
```
