# ripe/asnames — nomes de AS (RIPE NCC `asn.txt`)

O arquivo [`asn.txt`](https://ftp.ripe.net/ripe/asnames/asn.txt) do RIPE NCC
traz o **nome e o país de todos os ASNs alocados**, de todos os RIRs (AFRINIC,
APNIC, ARIN, LACNIC e RIPE NCC): ~122,6 mil linhas, uma por ASN, atualizado
cerca de uma vez por dia e sem hash publicado. Não é da família RIR (o
formato não é o delegated-extended); as delegações do RIPE NCC são a fonte
[`ripencc`](../../ripencc/README.md).

| Item | Valor |
|---|---|
| Fonte | `ripe/asnames` |
| Coletor | `collector-ripe-asnames`, em [`apps/ripe/asnames/collector/`](../../../../apps/ripe/asnames/collector/) — sub-agente `.claude/agents/collector-ripe-asnames.md` |
| API | `api-ripe-asnames`, em [`apps/ripe/asnames/api/`](../../../../apps/ripe/asnames/api/) — sub-agente `.claude/agents/api-ripe-asnames.md` |
| Tabelas | `ripe_asnames_asn` (um ASN por linha do arquivo), `ripe_asnames_run` (execuções) |
| Migrations | [`database/postgres/ripe_asnames/`](../../../../database/postgres/ripe_asnames/) |
| Linha em `jobs` | `app = 'collector-ripe-asnames'` |
| Caminho HTTP | `/ripe/asnames/` (`https://api.badblock.net.br/ripe/asnames/`) |
| Porta local | 8108 (`API_RIPE_ASNAMES_HOST_PORT`, só no loopback) |
| Imagens | `tmsoftbrasil/badblock-collector-ripe-asnames`, `tmsoftbrasil/badblock-api-ripe-asnames` |

## Arquivos

| Arquivo | Conteúdo | Dono |
|---|---|---|
| [fonte.md](fonte.md) | URL e servidor HTTP, formato com exemplos, fatos medidos, regras do parser (`handle`, `name`, `country`), fixture | sub-agente `collector-ripe-asnames` |
| [dados.md](dados.md) | `ripe_asnames_asn` e `ripe_asnames_run` (colunas, constraints, índices, trigram), mapeamento da fonte para as colunas, consultas da API | sub-agente `collector-ripe-asnames` |
| [collector.md](collector.md) | o que o coletor faz além do padrão: checagens sem hash publicado, gzip e ETag fraco, `MIN_ASNS`, `MERGE`, opções e variáveis `COLLECTOR_RIPE_ASNAMES_*`, operação, medições, testes | sub-agente `collector-ripe-asnames` |
| [api.md](api.md) | a `api-ripe-asnames` além do padrão: valores, cache e chaves normalizadas, consultas e `plan_cache_mode`, configuração, manifesto, medições, testes | sub-agente `api-ripe-asnames` |
| [api-rotas.md](api-rotas.md) | cada rota da `api-ripe-asnames`: validação, normalização, campos, exemplos reais, erros, roteamento | sub-agente `api-ripe-asnames` |

Mexer no coletor: [../../../padroes/coletor.md](../../../padroes/coletor.md) →
[fonte.md](fonte.md) → [dados.md](dados.md) → [collector.md](collector.md).
Mexer na API: [../../../padroes/api.md](../../../padroes/api.md) e
[../../../padroes/openapi.md](../../../padroes/openapi.md) → [dados.md](dados.md) →
[api.md](api.md) → [api-rotas.md](api-rotas.md).

## API

*(Seção do sub-agente `api-ripe-asnames`.)* A `api-ripe-asnames` serve estes dados
abaixo de `/ripe/asnames/` (versão atual e `/ripe/asnames/v1/`), só lendo
`ripe_asnames_asn`, `ripe_asnames_run` e a linha do coletor em `jobs`: um ASN pelo
número (`/ripe/asnames/asn/15169`, aceita também `AS15169`), os ASNs de um país
(`/ripe/asnames/country/BR`, inclusive `EU` e `AP`), os de um handle sem
diferenciar maiúsculas (`/ripe/asnames/handle/GOOGLE` — o handle não é único) e a
busca por trecho de texto na `description` original, com até 100 resultados
(`/ripe/asnames/search?q=google+llc`), além de `/ripe/asnames/meta`, das rotas de saúde
e do manifesto OpenAPI 3.1 em `/ripe/asnames/openapi.yaml`. É pública, sem chave
(rate limit por IP no Traefik), com cache opcional no Valkey. Valores, cache,
consultas e planos, configuração, medições e testes: [api.md](api.md); cada
rota, com validações, campos, exemplos reais e erros:
[api-rotas.md](api-rotas.md).
