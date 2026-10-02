# Especificações do BadBlock

Estas specs são a **fonte da verdade** do projeto inteiro: arquitetura,
infraestrutura, o padrão dos apps e o detalhe de cada fonte de dados. Elas
bastam para reconstruir o projeto se o código for apagado
([processos/reconstrucao.md](processos/reconstrucao.md)).

- Mudança de comportamento **começa aqui**: spec, depois código e testes
  ([processos/fluxo-de-trabalho.md](processos/fluxo-de-trabalho.md)).
- Spec e código divergindo é defeito: corrija o código, ou — se a spec é que
  ficou para trás — a spec, no mesmo trabalho.
- Cada assunto tem **um lugar só**. O comum a todos os apps está em
  `padroes/`; a pasta de cada fonte traz só o que é dela e aponta o resto.

## Mapa

### Projeto

| Arquivo | Conteúdo |
|---|---|
| [projeto/visao-geral.md](projeto/visao-geral.md) | O que é o BadBlock, fluxo dos dados, fases, o que fica fora |
| [projeto/estrutura.md](projeto/estrutura.md) | Árvore do repositório, o que fica fora do git, pasta de um app, **tabela de nomes** |
| [projeto/arquitetura.md](projeto/arquitetura.md) | Regras de arquitetura (com o motivo), componentes, onde está a verdade de cada assunto |
| [projeto/convencoes.md](projeto/convencoes.md) | Idioma, Go e dependências, configuração, testes, lint, git |
| [projeto/glossario.md](projeto/glossario.md) | Termos do projeto |
| [projeto/decisoes.md](projeto/decisoes.md) | Decisões tomadas, com data e motivo; questões em aberto |

### Plataforma

| Arquivo | Conteúdo |
|---|---|
| [plataforma/postgres.md](plataforma/postgres.md) | PostgreSQL dedicado, acesso, migrations (dbmate), estilo das tabelas, tabela `jobs` |
| [plataforma/valkey.md](plataforma/valkey.md) | Valkey (cache das APIs): configuração, senha, chaves |
| [plataforma/docker.md](plataforma/docker.md) | `Dockerfile`, composes (app e raiz), redes, `.env`, Makefiles |
| [plataforma/publicacao.md](plataforma/publicacao.md) | Traefik, release das imagens, scripts do mantenedor, produção |
| [plataforma/seguranca.md](plataforma/seguranca.md) | Segredos, superfície exposta, gitleaks |

### Padrões dos apps

| Arquivo | Conteúdo |
|---|---|
| [padroes/coletor.md](padroes/coletor.md) | O que todo `collector-<fonte>` faz: código, laço, detecção de mudança, aplicação, `<fonte>_run`, `jobs`, configuração, testes |
| [padroes/api.md](padroes/api.md) | O que toda `api-<fonte>` faz: código, caminho e versões, formato, cache e ETag, rotas comuns, erros, configuração, testes |
| [padroes/openapi.md](padroes/openapi.md) | Regras do manifesto OpenAPI 3.1 e o teste que o confere |
| [padroes/website.md](padroes/website.md) | O que todo `website-<site>` faz: nomes, Vite + React estático, lighttpd, imagem, compose e Traefik, testes, Makefile |

### Fontes

| Arquivo | Conteúdo |
|---|---|
| [fontes/README.md](fontes/README.md) | Tabela das fontes, família RIR, arquivos de uma pasta de fonte e seus donos |
| [fontes/rir/README.md](fontes/rir/README.md) | Modelo dos cinco RIRs: parâmetros de cada RIR, organização do código, manutenção dos clones |
| [fontes/rir/formato.md](fontes/rir/formato.md) | Formato delegated-extended, regras do parser, fixtures, comparação entre os RIRs |
| [fontes/rir/fixtures.md](fontes/rir/fixtures.md) | Conteúdo literal das fixtures `testdata/formats/*.txt` dos coletores de RIR |
| [fontes/rir/dados.md](fontes/rir/dados.md) | Tabelas `<rir>_asn`, `<rir>_prefix`, `<rir>_run` e as consultas da API |
| [fontes/rir/collector.md](fontes/rir/collector.md) | Coletor do modelo: checagens, arquivo mais antigo, opções, clonagem |
| [fontes/rir/api.md](fontes/rir/api.md) | API do modelo: rotas, validações, cache, opções, clonagem |
| [fontes/cgibr/](fontes/cgibr/README.md) | NIC.br / registro.br |
| [fontes/afrinic/](fontes/afrinic/README.md) | AFRINIC (RIR) |
| [fontes/apnic/](fontes/apnic/README.md) | APNIC (RIR) |
| [fontes/arin/](fontes/arin/README.md) | ARIN (RIR) |
| [fontes/lacnic/](fontes/lacnic/README.md) | LACNIC (RIR, modelo da família) |
| [fontes/ripencc/](fontes/ripencc/README.md) | RIPE NCC (RIR) |
| [fontes/iana/](fontes/iana/README.md) | IANA: blocos por RIR, uso especial, bootstrap RDAP |
| [fontes/ripe/](fontes/ripe/README.md) | RIPE NCC (site): os conjuntos de dados dele fora da família RIR |
| [fontes/ripe/asnames/](fontes/ripe/asnames/README.md) | RIPE NCC `asn.txt`: nomes de AS |
| [fontes/roothints/](fontes/roothints/README.md) | InterNIC `named.root`: servidores raiz do DNS |
| [fontes/rootzone/](fontes/rootzone/README.md) | InterNIC `root.zone`: zona raiz do DNS |
| [fontes/rootanchors/](fontes/rootanchors/README.md) | IANA `root-anchors.xml`: âncoras DNSSEC da raiz |
| [fontes/anatel/](fontes/anatel/README.md) | Anatel (site): os conjuntos de dados dela |
| [fontes/anatel/pst/](fontes/anatel/pst/README.md) | Anatel: prestadoras de serviços de telecomunicações |

Cada pasta de fonte tem `README.md`, `fonte.md`, `dados.md`, `collector.md` e
`api.md` ([fontes/README.md](fontes/README.md#pasta-de-uma-fonte)).

### Processos

| Arquivo | Conteúdo |
|---|---|
| [processos/fluxo-de-trabalho.md](processos/fluxo-de-trabalho.md) | Como mudar algo: spec → código → testes; sub-agentes e donos; relatório |
| [processos/testes.md](processos/testes.md) | Camadas de teste, comandos, o que rodar antes de um PR |
| [processos/nova-fonte.md](processos/nova-fonte.md) | Passo a passo de uma fonte nova |
| [processos/reconstrucao.md](processos/reconstrucao.md) | Como reconstruir o projeto a partir destas specs |

## Roteiros de leitura

| Tarefa | Leia |
|---|---|
| Entender o projeto | `projeto/visao-geral.md` → `projeto/arquitetura.md` → `projeto/estrutura.md` |
| Mexer num coletor | `padroes/coletor.md` → `fontes/<fonte>/README.md`, `fonte.md`, `dados.md`, `collector.md` (RIR: antes os de `fontes/rir/`) |
| Mexer numa API | `padroes/api.md`, `padroes/openapi.md` → `fontes/<fonte>/README.md`, `dados.md`, `api.md` (RIR: antes os de `fontes/rir/`) |
| Mudar o schema | `plataforma/postgres.md` → `fontes/<fonte>/dados.md` |
| Deploy, imagens, Traefik | `plataforma/docker.md`, `plataforma/publicacao.md` |
| Fonte nova | `processos/nova-fonte.md` |
| Testar antes de um PR | `processos/testes.md` |

## Como escrever uma spec

- Português (PT-BR, com acentos); identificadores, variáveis, colunas, rotas e
  campos em inglês, entre crases, **exatamente** como no código.
- **Um lugar só** para cada verdade: o comum vai em `padroes/`, a fonte traz
  o que é dela e **aponta** o resto (link, não cópia). Nos RIRs, o modelo fica
  em `fontes/rir/` e cada RIR traz só os seus valores, fatos e exemplos.
- Normativo e concreto: valores com unidade (`5 s`, `64 MiB`), padrões das
  opções, mensagens reais, limites. "Deve"/"nunca" para regras.
- O **motivo** junto da regra quando ele não for óbvio.
- **Fatos medidos com data** ("medido em 2026-09-28") e **exemplos reais**
  (respostas de API são respostas reais, com a data do arquivo carregado).
- Links relativos entre arquivos; nada de caminho de máquina nem dado de
  produção; nenhum segredo (o repositório é público).
- Arquivos focados, até ~400 linhas; títulos estáveis (outros arquivos
  apontam para eles).
- Arquivo novo entra no mapa acima (ou, numa pasta de fonte, no `README.md`
  dela).

## Verificação

```bash
make specs-check
```

Roda [check.sh](check.sh), que falha se:

- um link relativo em `specs/`, `AGENTS.md`, `CLAUDE.md`, `README.md`, nos
  `README.md` dos apps e de `database/` ou nos sub-agentes aponta para um
  arquivo que não existe, ou para uma âncora (`arquivo.md#seção`) que não
  existe nele — o nome da âncora segue a regra do GitHub (minúsculas, sem
  pontuação, espaços viram hífens; títulos repetidos ganham `-1`, `-2`...), e
  por isso **mudar um título quebra os links para ele**;
- uma fonte em `apps/` (`apps/<fonte>/` ou, de dois níveis,
  `apps/<site>/<conjunto>/`) não tem `specs/fontes/<fonte>/` com os cinco
  arquivos, `database/postgres/<fonte>/` e os dois sub-agentes, com os nomes
  na forma de cada lugar (ou sobra pasta de spec sem app);
- sobra uma pasta `specs/` dentro de um app, ou algum arquivo ainda cita o
  caminho antigo `apps/<fonte>/<tipo>/specs/`;
- um arquivo de `specs/` fora das pastas de fonte não aparece no mapa acima,
  ou um arquivo de uma pasta de fonte não aparece no `README.md` dela.
