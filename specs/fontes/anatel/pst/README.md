# Fonte `anatel/pst` — Prestadoras de Serviços de Telecomunicações (Anatel)

A Anatel publica, no painel de dados abertos de outorga e licenciamento, o
arquivo `prestadoras_servicos_telecomunicacoes.zip`: um CSV com uma linha por
**serviço notificado** de cada prestadora — quem tem outorga (ou é dispensado
dela), de que tipo, e cada serviço (SCM, STFC, SMP, SeAC, SLP…) com o número
Fistel e a data. O BadBlock guarda só as **pessoas jurídicas** (CNPJ): é o que
cruza com os titulares de ASNs e blocos (ex.: o CNPJ do `cgibr`) e responde
"este CNPJ é uma prestadora autorizada? de quê?" — por exemplo, se tem SCM
(045), a licença de provedor de internet. As linhas de pessoa física (CPF,
mascarado pela própria Anatel: radioamador, rádio do cidadão) são ignoradas.
Em 2026-09-30: 45.074 prestadoras e 54.314 serviços ([fonte.md](fonte.md)).

| Item | Valor |
|---|---|
| Quem publica | Anatel (site [`anatel`](../README.md)) |
| Apps | `collector-anatel-pst` em [`apps/anatel/pst/collector/`](../../../../apps/anatel/pst/collector/) e `api-anatel-pst` em [`apps/anatel/pst/api/`](../../../../apps/anatel/pst/api/) |
| Sub-agentes | `collector-anatel-pst` e `api-anatel-pst` (`.claude/agents/<app>.md`) |
| Migrations | `database/postgres/anatel_pst/` (controle `anatel_pst_schema_migrations`) |
| Tabelas | `anatel_pst_provider`, `anatel_pst_service`, `anatel_pst_run`, e a linha `collector-anatel-pst` de `jobs` |
| Caminho HTTP | `/anatel/pst/` (`https://api.badblock.net.br/anatel/pst/`) |
| Porta local da API | 8112 (`API_ANATEL_PST_HOST_PORT`, no loopback) |
| Verificação | a cada 24 h (`SYNC_INTERVAL=24h`); aplica só se o arquivo mudou |

É uma fonte de dois níveis: o nome muda de forma conforme o lugar
(`anatel/pst`, `anatel-pst`, `anatel_pst`, `ANATEL_PST`), como diz
[../../../projeto/estrutura.md](../../../projeto/estrutura.md#fonte-de-dois-níveis-siteconjunto);
os demais nomes (imagens, containers, tags, chave de cache) seguem a tabela
de nomes de lá.

## Arquivos desta pasta

| Arquivo | Conteúdo | Dono |
|---|---|---|
| [fonte.md](fonte.md) | URL, publicação, o ZIP e o CSV, colunas, fatos medidos, regras do parser, fixture | `collector-anatel-pst` |
| [dados.md](dados.md) | tabelas `anatel_pst_*` (colunas, constraints, índices), mapeamento CSV → colunas, consultas da API | `collector-anatel-pst` |
| [collector.md](collector.md) | o que o coletor faz além do [padrão](../../../padroes/coletor.md): checagens de mudança, CSV mais antigo, mínimos, aplicação, opções, `COLLECTOR_ANATEL_PST_*`, operação, testes | `collector-anatel-pst` |
| [api.md](api.md) | o que a API faz além do [padrão](../../../padroes/api.md): valores, dados servidos, cache e ETag, consultas, opções, compose, manifesto, medições, testes | `api-anatel-pst` |
| [api-rotas.md](api-rotas.md) | cada rota: parâmetros, validação, normalização, chave de cache, campos, exemplos reais, erros, roteamento | `api-anatel-pst` |

Este `README.md` é do `collector-anatel-pst`, menos a seção "API", que é da
`api-anatel-pst`.

## API

A `api-anatel-pst` serve estes dados por HTTP (JSON), só leitura, e é dona de
tudo abaixo de `/anatel/pst` no host da API: a prestadora de um CNPJ com os
seus serviços (`https://api.badblock.net.br/anatel/pst/provider/02558157000162`),
o catálogo de serviços (`/anatel/pst/services`), as prestadoras de um serviço
(`/anatel/pst/service/045`) e a busca por nome
(`/anatel/pst/search?q=...`, por parte da razão social ou do nome
fantasia). Valores, cache, consultas, medições e testes: [api.md](api.md);
rotas, validações e exemplos reais: [api-rotas.md](api-rotas.md).
