# rdns — reverso (PTR) dos IPv4 cadastrados

O BadBlock varre ele mesmo o DNS reverso: para cada endereço IPv4 dos
prefixos cadastrados, pergunta o PTR (`<d>.<c>.<b>.<a>.in-addr.arpa`) a
resolvedores recursivos e guarda o nome — ou a falha. Não há arquivo
publicado: é uma **sonda ativa**
([../../padroes/sonda.md](../../padroes/sonda.md)). Os prefixos vêm dos
blocos IPv4 do [`cgibr`](../cgibr/README.md), quebrados em /24, e das linhas
estáticas que o administrador grava. Em 2026-10-08 eram 308.529 /24
(79,0 M endereços); em 2026-10-09, 70% dos endereços de uma amostra tinham
PTR ([fonte.md](fonte.md)).

| Item | Valor |
|---|---|
| Fonte | `rdns` |
| Coletor | `collector-rdns`, em `apps/rdns/collector/` — sub-agente [`.claude/agents/collector-rdns.md`](../../../.claude/agents/collector-rdns.md); segue também o [padrão de sonda](../../padroes/sonda.md) |
| API | `api-rdns`, em `apps/rdns/api/` — sub-agente [`.claude/agents/api-rdns.md`](../../../.claude/agents/api-rdns.md) |
| Tabelas | `rdns_prefix` (prefixos e estado da varredura), `rdns_ptr` (resultado por IP), `rdns_run` (derivações), `rdns_worker` (instâncias), `rdns_config` (taxa e meta) |
| Migrations | `database/postgres/rdns/` (controle `rdns_schema_migrations`) |
| Linha em `jobs` | `app = 'collector-rdns'` |
| Lê de outra fonte | `cgibr_run`, `cgibr_prefix`, `cgibr_asn` (só leitura, na derivação) |
| Caminho HTTP | `/rdns/` (`https://api.badblock.net.br/rdns/`) |
| Porta local da API | 8113 (`API_RDNS_HOST_PORT`, só no loopback) |
| Imagens | `tmsoftbrasil/badblock-collector-rdns`, `tmsoftbrasil/badblock-api-rdns` |
| Valkey | cache da API e **fila de tempo real** entre a API e o coletor ([dados.md](dados.md#fila-de-tempo-real-valkey)) |

Os demais nomes seguem a tabela de
[../../projeto/estrutura.md](../../projeto/estrutura.md#nomes). Decisões
desta fonte: #24 a #33 em [../../projeto/decisoes.md](../../projeto/decisoes.md).

## Arquivos

| Arquivo | Conteúdo | Dono |
|---|---|---|
| [fonte.md](fonte.md) | de onde vêm os prefixos e as respostas: derivação do `cgibr`, consulta PTR, interpretação das respostas, normalização dos nomes, fatos medidos, fixtures | `collector-rdns` |
| [dados.md](dados.md) | as cinco tabelas `rdns_*` (colunas, constraints, índices, triggers de sobreposição e de remoção), o que o administrador grava, a fila de tempo real no Valkey, consultas da API, tamanho estimado | `collector-rdns` |
| [collector.md](collector.md) | o coletor além dos padrões: derivação, fila e lease, varredura, cliente DNS (rodízio, prazos, novas tentativas), taxa, tempo real, instâncias e réplicas, opções `COLLECTOR_RDNS_*`, operação, testes | `collector-rdns` |
| [api.md](api.md) | a API além do padrão: rotas `/rdns/ip` e `/rdns/prefix`, `live=true`, cache sem versão, `/meta`, Traefik com o segundo router, opções, manifesto, testes | `api-rdns` |

Este `README.md` é do `collector-rdns`, menos a seção "API", que é da
`api-rdns`.

Mexer no coletor: [../../padroes/coletor.md](../../padroes/coletor.md) →
[../../padroes/sonda.md](../../padroes/sonda.md) → [fonte.md](fonte.md) →
[dados.md](dados.md) → [collector.md](collector.md). Mexer na API:
[../../padroes/api.md](../../padroes/api.md) e
[../../padroes/openapi.md](../../padroes/openapi.md) → [dados.md](dados.md) →
[api.md](api.md).

## API

*(Seção do sub-agente `api-rdns`.)* A `api-rdns` serve o reverso abaixo de
`/rdns/` (versão atual e `/rdns/v1/`): o PTR de um IPv4
(`/rdns/ip/200.160.2.3`) e os de todos os endereços de um prefixo de /24 a
/31 (`/rdns/prefix/200.160.2.0/24`), cada endereço com o estado (`ptr`,
`no_ptr`, `servfail`, `refused`, `timeout` ou `not_scanned`) e a data da
última varredura do prefixo. Com `live=true`, `/rdns/ip` pede ao coletor uma
resolução na hora e espera até 5 s; estourou, responde o dado frio. Só lê o
Postgres; no Valkey, além do cache, escreve só na fila de tempo real. Não há
rota por ASN. Pública, sem chave, com rate limit por IP no Traefik — mais
apertado para `live=true`. Detalhes: [api.md](api.md).
