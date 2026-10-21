# registrobr/rdapnum — RDAP dos ASNs e blocos brasileiros

> **Estado:** só specs

O BadBlock consulta ele mesmo o RDAP do registro.br
(`https://rdap.registro.br/`) para **cada ASN e cada bloco IP** do
[`cgibr`](../../cgibr/README.md) e guarda o que o arquivo do NIC.br não
traz: titular com CNPJ e representante legal, contatos (administrativo,
técnico, abuso) com nome e e-mail, datas de registro e de alteração,
situação do bloco e as delegações reversas com os nameservers. Em 2026-09-28
eram 9.134 ASNs e 21.991 blocos: **31.125** consultas por volta.

Não há arquivo publicado: é uma **sonda ativa**
([../../../padroes/sonda.md](../../../padroes/sonda.md)), como o
[`rdns`](../../rdns/README.md), com duas diferenças que mandam no desenho:

- o servidor **trunca** as respostas de quem consulta demais — tira e-mails,
  endereços e o CNPJ e responde 200 mesmo assim
  ([fonte.md](fonte.md#resposta-truncada)) —, por isso o coletor detecta a
  truncagem, não deixa o dado truncado sobrescrever o bom e sai por um
  **proxy** que roda o IP de saída ([collector.md](collector.md#proxy));
- a resposta é **guardada crua antes** de ser processada, com histórico das
  versões, e o processamento pode ser refeito sem consultar de novo
  ([dados.md](dados.md#registrobr_rdapnum_response)).

| Item | Valor |
|---|---|
| Fonte | `registrobr/rdapnum` (site [`registrobr`](../README.md)) |
| Coletor | `collector-registrobr-rdapnum`, em `apps/registrobr/rdapnum/collector/` — sub-agente [`.claude/agents/collector-registrobr-rdapnum.md`](../../../../.claude/agents/collector-registrobr-rdapnum.md); segue também o [padrão de sonda](../../../padroes/sonda.md) |
| API | `api-registrobr-rdapnum`, em `apps/registrobr/rdapnum/api/` — sub-agente [`.claude/agents/api-registrobr-rdapnum.md`](../../../../.claude/agents/api-registrobr-rdapnum.md) |
| Tabelas | alvos e estado da coleta (`_target`), respostas cruas (`_response`), dados processados (`_autnum`, `_network`, `_entity`, `_link`, `_reverse`), derivações (`_run`), instâncias (`_worker`), taxa e meta (`_config`), todas com o prefixo `registrobr_rdapnum` |
| Migrations | `database/postgres/registrobr_rdapnum/` (controle `registrobr_rdapnum_schema_migrations`) |
| Linha em `jobs` | `app = 'collector-registrobr-rdapnum'` |
| Lê de outra fonte | `cgibr_run`, `cgibr_asn`, `cgibr_prefix` (só leitura, na derivação) |
| Caminho HTTP | `/registrobr/rdapnum/` (`https://api.badblock.net.br/registrobr/rdapnum/`) |
| Porta local da API | 8114 (`API_REGISTROBR_RDAPNUM_HOST_PORT`, só no loopback) |
| Imagens | `tmsoftbrasil/badblock-collector-registrobr-rdapnum`, `tmsoftbrasil/badblock-api-registrobr-rdapnum` |
| Valkey | cache da API e **fila de atualização** entre a API e o coletor ([dados.md](dados.md#fila-de-atualização-valkey)) |

Os demais nomes seguem a tabela de
[../../../projeto/estrutura.md](../../../projeto/estrutura.md#nomes), na forma
de dois níveis. Decisões desta fonte: #37 a #42 em
[../../../projeto/decisoes.md](../../../projeto/decisoes.md).

## Arquivos

| Arquivo | Conteúdo | Dono |
|---|---|---|
| [fonte.md](fonte.md) | o serviço RDAP: consultas, formato das respostas, resposta truncada, limite do servidor (medido), IP de saída no aviso, regras do parser, fixtures | `collector-registrobr-rdapnum` |
| [dados.md](dados.md) | alvos, respostas cruas, derivações, instâncias, configuração, o que o administrador grava, a fila de atualização no Valkey | `collector-registrobr-rdapnum` |
| [dados-processados.md](dados-processados.md) | parte do `dados.md`: as tabelas processadas (ASN, bloco, entidade, ligação, reverso), o mapeamento resposta → colunas, as consultas da API, a migration | `collector-registrobr-rdapnum` |
| [collector.md](collector.md) | o coletor além dos padrões: derivação, fila, coleta, truncagem e espera, proxy, taxa por minuto, processamento, atualização, opções, operação, testes | `collector-registrobr-rdapnum` |
| [api.md](api.md) | a API além do padrão: resumo das rotas, cache sem versão, atualização por `POST`, `/meta`, Traefik, opções, manifesto, testes | `api-registrobr-rdapnum` |
| [api-rotas.md](api-rotas.md) | parte do `api.md`: cada rota com parâmetros, validações, campos, exemplos e erros | `api-registrobr-rdapnum` |

Este `README.md` é do `collector-registrobr-rdapnum`, menos a seção "API",
que é da `api-registrobr-rdapnum`.

Mexer no coletor: [../../../padroes/coletor.md](../../../padroes/coletor.md) →
[../../../padroes/sonda.md](../../../padroes/sonda.md) → [fonte.md](fonte.md) →
[dados.md](dados.md) → [dados-processados.md](dados-processados.md) →
[collector.md](collector.md). Mexer na API:
[../../../padroes/api.md](../../../padroes/api.md) e
[../../../padroes/openapi.md](../../../padroes/openapi.md) →
[dados-processados.md](dados-processados.md) → [api.md](api.md) →
[api-rotas.md](api-rotas.md).

## API

*(Seção do sub-agente `api-registrobr-rdapnum`.)* A `api-registrobr-rdapnum`
serve o dado processado abaixo de `/registrobr/rdapnum/` (versão atual e
`/registrobr/rdapnum/v1/`): o ASN com titular, contatos e blocos
(`/asn/61610`), o bloco que contém um IP ou prefixo, com contatos e
delegações reversas (`/ip/200.225.48.10`, `/prefix/200.225.48.0/24`), tudo
de um CNPJ (`/document/35980592000130`), os objetos de um contato
(`/contact/<handle>`), a resposta RDAP crua e o histórico dela (`/raw`,
`/history`, `/response/{uuid}`) e o pedido de atualização
(`POST /asn/61610/refresh`), que entra numa fila do Valkey e o coletor
atende antes da fila normal. É pública e sem chave, **com contatos e
e-mails** (decisão #42), e rate limit por IP no Traefik — mais apertado no
`POST`. Só lê o Postgres; no Valkey, além do cache, escreve só na fila de
atualização. Detalhes: [api.md](api.md) e [api-rotas.md](api-rotas.md).
