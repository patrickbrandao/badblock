# Fonte `iana` — IANA

A IANA publica os **registros de numeração** que ficam acima dos cinco RIRs:
a quem entregou cada faixa de ASN, cada `/8` do IPv4 e cada bloco IPv6
unicast; o que é reservado para uso especial (a base da marcação de
**bogon**); e qual servidor RDAP responde por cada faixa (bootstrap RDAP,
RFC 9224). São **10 arquivos pequenos** (~60 KB no total) e quase estáticos,
tratados como **um dataset só**: o coletor só aplica quando algum muda, e
aplica os 10 juntos, numa transação, ou nenhum.

| Item | Valor |
|---|---|
| Quem publica | IANA: 7 CSVs em `https://www.iana.org/assignments/` e 3 JSONs em `https://data.iana.org/rdap/` ([fonte.md](fonte.md#arquivos)) |
| Apps | [`collector-iana`](../../../apps/iana/collector/) (`apps/iana/collector/`) e [`api-iana`](../../../apps/iana/api/) (`apps/iana/api/`) |
| Migrations | [`database/postgres/iana/`](../../../database/postgres/iana/) |
| Sub-agentes | [`collector-iana`](../../../.claude/agents/collector-iana.md) e [`api-iana`](../../../.claude/agents/api-iana.md) |
| Tabelas | `iana_asn_block`, `iana_prefix_block`, `iana_special_prefix`, `iana_special_asn`, `iana_rdap_service`, `iana_run` ([dados.md](dados.md)) |
| Linha em `jobs` | `app = 'collector-iana'` |
| Caminho HTTP | `/iana/` (`https://api.badblock.net.br/iana/...`) |
| Porta local da API | 8107 (`API_IANA_HOST_PORT`, só no loopback) |
| Verificação | a cada 6 h, sem hash publicado: GET condicional e SHA-256 por arquivo ([collector.md](collector.md)) |

| Tabela | O que guarda |
|---|---|
| `iana_asn_block` | faixas de ASN de `as-numbers-1`/`-2`: RIR, WHOIS, RDAP, data |
| `iana_prefix_block` | os 256 `/8` do IPv4 e os blocos IPv6 unicast: RIR, status, data |
| `iana_special_prefix` | blocos IPv4/IPv6 de uso especial, com as flags (`globally_reachable`...) |
| `iana_special_asn` | ASNs de uso especial |
| `iana_rdap_service` | servidor RDAP de cada faixa de ASN e bloco IP |
| `iana_run` | execuções que baixaram um dataset novo; a última aplicada é a versão dos dados |

O que difere das outras fontes, em resumo: 10 arquivos num dataset (pacote
`internal/source`), bases `IANA_BASE_URL`/`RDAP_BASE_URL` no lugar de
`SOURCE_URL`, SHA-256 combinado, sanidade estrutural (`parse.Check`), piso de
2 linhas na trava de remoção e `iana_run.files`/`changes` em jsonb — tudo em
[collector.md](collector.md#diferenças-do-padrão). O comum a todos os apps
está em [../../padroes/coletor.md](../../padroes/coletor.md) e
[../../padroes/api.md](../../padroes/api.md).

## Arquivos desta pasta

| Arquivo | Conteúdo | Dono |
|---|---|---|
| [fonte.md](fonte.md) | os 10 arquivos: URLs, publicação e cabeçalhos HTTP, formato de cada um com exemplos reais, fatos medidos, regras do parser, sanidade, fixtures | `collector-iana` |
| [dados.md](dados.md) | tabelas `iana_*`: colunas, constraints, índices, triggers, mapeamento fonte → colunas, `iana_run.files`/`changes`, consultas da `api-iana` | `collector-iana` |
| [collector.md](collector.md) | o que o `collector-iana` faz além do padrão: verificação dos 10 arquivos, hash combinado, aplicação e trava de remoção, recusas, por que 6 h, opções e `.env`, operação, medições, testes | `collector-iana` |
| [api.md](api.md) | o que a `api-iana` faz além do padrão: dados e consistência (`REPEATABLE READ`), regra de bogon com a tabela de casos, validações e erros, chaves de cache e ETags, manifesto, opções e `.env`, operação, medições, testes (`testdata/seed.sql`, `make test-real`) | `api-iana` |
| [api-rotas.md](api-rotas.md) | cada rota da `api-iana`: parâmetros, objetos e campos, exemplos reais de resposta | `api-iana` |

Este `README.md` é do `collector-iana`, menos a seção [API](#api), que é da
`api-iana`.

## API

A [`api-iana`](../../../apps/iana/api/) serve, só lendo as tabelas acima,
tudo abaixo de `/iana` (`https://api.badblock.net.br/iana/...`, e
`/iana/v1/...` para a v1 fixa): para um ASN, IP ou prefixo (`/iana/asn/61613`,
`/iana/ip/10.0.0.1`, `/iana/prefix/2001:db8::/48`), a faixa ou bloco da IANA
(RIR, status, WHOIS, RDAP), os registros de uso especial que o contêm, o
servidor RDAP e, para IPs e prefixos, se é bogon; também as listas completas
(`/iana/asns`, `/iana/ipv4`, `/iana/ipv6`, `/iana/special`, `/iana/rdap`),
`/iana/meta` e o manifesto OpenAPI 3.1 em `/iana/openapi.yaml`. O que ela
faz além do [padrão](../../padroes/api.md) — regra de bogon, validações,
cache, opções e testes — está em [api.md](api.md); cada rota, com exemplo
real, em [api-rotas.md](api-rotas.md).
