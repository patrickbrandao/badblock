# rootzone — zona raiz do DNS (InterNIC `root.zone`)

O arquivo [`root.zone`](https://www.internic.net/domain/root.zone) da
InterNIC é a **zona raiz do DNS inteira**: o SOA (com o serial da versão), os
NS e as chaves (DNSKEY) da raiz, o ZONEMD e, para cada um dos ~1,4 mil **TLDs
delegados**, os servidores de nome (NS), o glue A/AAAA desses servidores, os
DS (a cadeia DNSSEC a partir da raiz) e o NSEC. São ~2,2 MB, ~24,9 mil RRs,
com um `.md5` publicado ao lado e uma versão nova cerca de 2 vezes por dia.
Os RRSIG são contados, não guardados. Os root hints são a fonte
[`roothints`](../roothints/README.md) e as âncoras DNSSEC, a
[`rootanchors`](../rootanchors/README.md) (decisão #18 em
[../../projeto/decisoes.md](../../projeto/decisoes.md)).

| Item | Valor |
|---|---|
| Fonte | `rootzone` |
| Coletor | `collector-rootzone`, em [`apps/rootzone/collector/`](../../../apps/rootzone/collector/) — sub-agente `.claude/agents/collector-rootzone.md` |
| API | `api-rootzone`, em [`apps/rootzone/api/`](../../../apps/rootzone/api/) — sub-agente `.claude/agents/api-rootzone.md` |
| Tabelas | `rootzone_tld` (um TLD delegado por linha), `rootzone_record` (uma RR por linha, menos RRSIG), `rootzone_run` (execuções, com o SOA de cada versão) |
| Migrations | [`database/postgres/rootzone/`](../../../database/postgres/rootzone/) |
| Linha em `jobs` | `app = 'collector-rootzone'` |
| Caminho HTTP | `/rootzone/` (`https://api.badblock.net.br/rootzone/`) |
| Porta local | 8110 (`API_ROOTZONE_HOST_PORT`, só no loopback) |
| Imagens | `tmsoftbrasil/badblock-collector-rootzone`, `tmsoftbrasil/badblock-api-rootzone` |

## Arquivos

| Arquivo | Conteúdo | Dono |
|---|---|---|
| [fonte.md](fonte.md) | URLs e servidor HTTP (gzip e o ETag `-gzip` do Apache), `.md5` e `.sig`, formato, fatos medidos, regras do parser e normalização, RRSIG e ZONEMD, fixture | sub-agente `collector-rootzone` |
| [dados.md](dados.md) | `rootzone_run`, `rootzone_tld` e `rootzone_record` (colunas, constraints, índices), mapeamento da fonte para as colunas, consultas da API | sub-agente `collector-rootzone` |
| [collector.md](collector.md) | o que o coletor faz além do padrão: checagens (`.md5`, GET condicional, SHA-256, serial do SOA), serial igual, `MIN_TLDS`, trava de remoção, `MERGE`, opções e variáveis `COLLECTOR_ROOTZONE_*`, operação, medições, testes | sub-agente `collector-rootzone` |
| [api.md](api.md) | a `api-rootzone` além do padrão: valores, rotas e como acrescentar uma, normalização do TLD (punycode), cache e chaves, consultas, configuração, manifesto, medições, testes | sub-agente `api-rootzone` |
| [api-rotas.md](api-rotas.md) | cada rota da `api-rootzone`: validação, normalização, campos, exemplos reais, erros, roteamento | sub-agente `api-rootzone` |

Mexer no coletor: [../../padroes/coletor.md](../../padroes/coletor.md) →
[fonte.md](fonte.md) → [dados.md](dados.md) → [collector.md](collector.md).
Mexer na API: [../../padroes/api.md](../../padroes/api.md) e
[../../padroes/openapi.md](../../padroes/openapi.md) → [dados.md](dados.md) →
[api.md](api.md) → [api-rotas.md](api-rotas.md).

## API

*(Seção do sub-agente `api-rootzone`.)* A `api-rootzone` serve a zona raiz
abaixo de `/rootzone/` (versão atual e `/rootzone/v1/`), só lendo
`rootzone_tld`, `rootzone_record`, `rootzone_run` e a linha do coletor em
`jobs`: todos os TLDs delegados com as contagens da delegação
(`/rootzone/tlds`) e a delegação de um TLD — os NS com TTL, o glue A/AAAA
de cada servidor e os DS — pelo nome em qualquer caixa, com ou sem o ponto
final, em ASCII ou em Unicode (`/rootzone/tld/br`, `/rootzone/tld/рф` =
`/rootzone/tld/xn--p1ai`). Toda resposta de dados traz o serial do SOA da
zona (`zone.serial`); o SOA inteiro está em `/rootzone/meta`. Mais as rotas
de saúde e o manifesto OpenAPI 3.1 em `/rootzone/openapi.yaml`. Pública,
sem chave (rate limit por IP no Traefik), com cache opcional no Valkey.
Valores, normalização, cache, consultas, configuração, medições e testes:
[api.md](api.md); cada rota, com campos, exemplos reais e erros:
[api-rotas.md](api-rotas.md).
