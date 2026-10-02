# Fonte `cgibr` — NIC.br / registro.br

O NIC.br, pelo registro.br (o registro nacional brasileiro), publica todo dia
útil o arquivo `nicbr-asn-blk-latest.txt`: uma linha por ASN de titular
brasileiro, com o nome e o documento do titular (CNPJ, ou identificador
estrangeiro de 8 dígitos) e os blocos IPv4 e IPv6 dele. Diferente dos arquivos
dos RIRs, liga ASNs e blocos ao **CNPJ** do titular; algumas linhas usam ASN
registrado fora do Brasil (ex.: AS174, AS8075) com os blocos do titular
brasileiro. Em 2026-09-28: 9.134 ASNs, 13.037 blocos IPv4 e 8.954 IPv6
([fonte.md](fonte.md)).

| Item | Valor |
|---|---|
| Quem publica | NIC.br / registro.br |
| Apps | `collector-cgibr` em [`apps/cgibr/collector/`](../../../apps/cgibr/collector/) e `api-cgibr` em [`apps/cgibr/api/`](../../../apps/cgibr/api/) |
| Sub-agentes | `collector-cgibr` e `api-cgibr` (`.claude/agents/<app>.md`) |
| Migrations | `database/postgres/cgibr/` (controle `cgibr_schema_migrations`) |
| Tabelas | `cgibr_asn`, `cgibr_prefix`, `cgibr_run`, e a linha `collector-cgibr` de `jobs` |
| Caminho HTTP | `/cgibr/` (`https://api.badblock.net.br/cgibr/`) |
| Porta local da API | 8101 (`API_CGIBR_HOST_PORT`, no loopback) |

Os demais nomes (imagens, containers, tags de release, chave de cache) seguem
a tabela de [../../projeto/estrutura.md](../../projeto/estrutura.md#nomes),
que usa o `cgibr` como exemplo.

## Arquivos desta pasta

| Arquivo | Conteúdo | Dono |
|---|---|---|
| [fonte.md](fonte.md) | URLs, publicação, formato do arquivo, fatos medidos, regras do parser, fixture | `collector-cgibr` |
| [dados.md](dados.md) | tabelas `cgibr_*` (colunas, constraints, índices, triggers), mapeamento arquivo → colunas, consultas da API | `collector-cgibr` |
| [collector.md](collector.md) | o que o coletor faz além do [padrão](../../padroes/coletor.md): checagens de mudança, `SOURCE_SHA256_URL`, `MIN_ASNS`, aplicação, recusas, opções, `COLLECTOR_CGIBR_*`, operação, testes | `collector-cgibr` |
| [api.md](api.md) | valores, resumo das rotas (`operationId`, chave de cache), manifesto, opções, compose, operação, medições, testes | `api-cgibr` |
| [api-rotas.md](api-rotas.md) | parte do `api.md`: cada rota com parâmetros, validações, chave de cache, campos, exemplos reais e erros | `api-cgibr` |

Este `README.md` é do `collector-cgibr`, menos a seção "API", que é da
`api-cgibr`.

## API

A `api-cgibr` serve estes dados por HTTP (JSON), só leitura, e é dona de tudo
abaixo de `/cgibr` no host da API: o ASN com titular e blocos
(`https://api.badblock.net.br/cgibr/asn/61613`), o bloco que contém um IP ou
prefixo (`/cgibr/v1/ip/45.171.61.10`, `/cgibr/prefix/200.192.152.0/24`), os
ASNs de um CNPJ ou identificador estrangeiro
(`/cgibr/document/08030063000100`) e a lista de todos os ASNs
(`/cgibr/asns`). É pública e sem chave (rate limit por IP no Traefik), usa o
Valkey como cache opcional e serve o manifesto OpenAPI 3.1 em
`/cgibr/openapi.yaml`, fora do versionamento. Valores, opções, manifesto e
testes: [api.md](api.md); cada rota, com validações e exemplos reais:
[api-rotas.md](api-rotas.md).
