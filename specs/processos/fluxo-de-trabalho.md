# Fluxo de trabalho

Vale para pessoas e agentes de IA.

## A spec vem primeiro

1. **Spec**: descreva a mudança na spec que é dona do assunto
   ([../README.md](../README.md#mapa)). Rota nova ou alterada muda também o
   manifesto OpenAPI; tabela nova ou alterada, `dados.md` e uma migration nova.
2. **Código e testes**: implemente e ajuste os testes (fixtures reais quando
   o formato da fonte muda).
3. **Validação**: os testes do app, e os da raiz quando a mudança cruza apps
   ([testes.md](testes.md)); `make specs-check` quando mexer em specs.
4. **Relatório**: o que mudou, resultado dos testes, impacto em outros apps.

Corrigir um bug sem mudar o comportamento especificado dispensa o passo 1 —
mas, se o bug revelou uma lacuna na spec, feche a lacuna.

## Quem cuida de quê

| Assunto | Dono | Onde |
|---|---|---|
| Projeto, plataforma, padrões, processos, índices (`specs/README.md`, `specs/fontes/README.md`) e arquivos da raiz | sessão principal (quem integra) | `specs/projeto/`, `specs/plataforma/`, `specs/padroes/`, `specs/processos/`, raiz |
| Um coletor | sub-agente `collector-<fonte>` | `apps/<fonte>/collector/`, `specs/fontes/<fonte>/{README,fonte,dados,collector}.md`, `database/postgres/<fonte>/` |
| Uma API | sub-agente `api-<fonte>` | `apps/<fonte>/api/`, `specs/fontes/<fonte>/api.md` (e a seção "API" do `README.md` da fonte) |
| Modelo dos coletores de RIR | sub-agente `collector-lacnic` | `specs/fontes/rir/{README,formato,dados,collector}.md` |
| Modelo das APIs de RIR | sub-agente `api-lacnic` | `specs/fontes/rir/api.md` (e a seção "Parâmetros das APIs" do `README.md`) |
| Um site | sub-agente `website-<site>` | `websites/<site>/` (o padrão, `specs/padroes/website.md`, é da sessão principal) |

- Trabalho num app vai para o sub-agente dele (`.claude/agents/<app>.md`).
- A API não muda o coletor nem o schema: precisa de coluna ou índice novo? O
  pedido (com a migration proposta) vai para o sub-agente do coletor.
- Mudança num **padrão** (`specs/padroes/`) muda todos os apps afetados: a
  sessão principal atualiza o padrão e distribui o trabalho, um sub-agente
  por app.
- **Família RIR**: mudança no código comum vale para os cinco — é feita no
  modelo (lacnic) e repetida nos quatro clones, cada um pelo seu sub-agente; a
  spec do modelo (`specs/fontes/rir/`) muda uma vez. Um bug achado num clone
  pode ser corrigido nele, e o relatório leva a mesma correção ao modelo e aos
  outros clones. O que é de um RIR fica em `internal/rir/rir.go` e na pasta
  do RIR.
- Integração na raiz (`docker-compose.yml`, `.env.example`, `Makefile`,
  `release-images.sh`, `README.md`, `AGENTS.md`) é da sessão principal.

## Modelo de um sub-agente

Um arquivo por app em `.claude/agents/<app>.md`. O corpo é o prompt de
sistema do sub-agente; o `CLAUDE.md` (que importa o `AGENTS.md`) entra no
contexto dele sozinho, então o arquivo **não repete** regras nem conteúdo das
specs — só diz onde ler e até onde ir.

```markdown
---
name: <app>
description: Especialista no app <app> do BadBlock (<o que faz, em uma frase>). Use PROATIVAMENTE para qualquer pergunta, mudança, bug ou revisão em apps/<fonte>/<tipo>/, em specs/fontes/<fonte>/<arquivos>, <assuntos que devem acionar o agente>. [É um clone do <modelo>.]
model: inherit
---

Você é o responsável pelo app **<app>** do BadBlock. (idioma)

## Leitura obrigatória   padrões → modelo da família, se houver → pasta da fonte → plataforma conforme o assunto
## Escopo                código do app; os arquivos de spec que são dele; o que é de outro dono e como pedir
## Família RIR           (só na família) modelo ou clone, e como uma correção chega aos cinco
## Fluxo                 spec → código e testes → comandos de validação → relatório
## Nunca                 as poucas regras que, quebradas, estragam dados ou contrato
```

A `description` decide quando o sub-agente é chamado: cite o app, as pastas,
as rotas ou tabelas e as particularidades que alguém mencionaria ao pedir
algo.

## Relatório de um sub-agente

- Arquivos criados, alterados e apagados.
- Resultado dos testes que rodou.
- Impacto fora do seu escopo: na API ou no coletor da mesma fonte, na tabela
  `jobs`, no modelo e nos clones (RIR), nos padrões — com a correção
  proposta.
- Divergências entre spec e código que encontrou.

## Limites

- Não leia `old/` nem `tmp/` sem pedido explícito do usuário.
- Commit, push, tag e publicação de imagem só com pedido explícito.
- `run-prod.sh` e `run-builder.sh` só mudam com pedido explícito
  ([../plataforma/publicacao.md](../plataforma/publicacao.md#scripts-do-mantenedor)).
- Nenhum segredo no git ([../plataforma/seguranca.md](../plataforma/seguranca.md)).
- Todo processo ou container que um agente sobe para testar (um binário da
  API ouvindo numa porta, um Postgres descartável) é encerrado antes do
  relatório: um servidor deixado em segundo plano nunca termina e mantém o
  agente "rodando".
