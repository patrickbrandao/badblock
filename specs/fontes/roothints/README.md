# roothints — servidores raiz do DNS (InterNIC `named.root`)

O arquivo [`named.root`](https://www.internic.net/domain/named.root) da
InterNIC traz os **root hints**: o nome e os endereços IPv4 e IPv6 dos **13
servidores raiz do DNS** (`a` a `m.root-servers.net`), que um resolvedor usa
para achar a raiz na partida. São 3.315 bytes, com um `.md5` publicado ao
lado; o conteúdo muda poucas vezes por ano, mas o servidor regrava o arquivo
a cada publicação da zona raiz (cerca de 2 vezes por dia). A zona raiz em si
é a fonte [`rootzone`](../rootzone/README.md) e as âncoras DNSSEC, a
[`rootanchors`](../rootanchors/README.md) (decisão #18 em
[../../projeto/decisoes.md](../../projeto/decisoes.md)).

| Item | Valor |
|---|---|
| Fonte | `roothints` |
| Coletor | `collector-roothints`, em [`apps/roothints/collector/`](../../../apps/roothints/collector/) — sub-agente `.claude/agents/collector-roothints.md` |
| API | `api-roothints`, em [`apps/roothints/api/`](../../../apps/roothints/api/) — sub-agente `.claude/agents/api-roothints.md` |
| Tabelas | `roothints_server` (um servidor raiz por linha), `roothints_run` (execuções) |
| Migrations | [`database/postgres/roothints/`](../../../database/postgres/roothints/) |
| Linha em `jobs` | `app = 'collector-roothints'` |
| Caminho HTTP | `/roothints/` (`https://api.badblock.net.br/roothints/`) |
| Porta local | 8109 (`API_ROOTHINTS_HOST_PORT`, só no loopback) |
| Imagens | `tmsoftbrasil/badblock-collector-roothints`, `tmsoftbrasil/badblock-api-roothints` |

## Arquivos

| Arquivo | Conteúdo | Dono |
|---|---|---|
| [fonte.md](fonte.md) | URLs e servidor HTTP, `.md5` e `.sig` publicados, formato com o cabeçalho (data e serial da zona raiz), fatos medidos, regras do parser, fixture | sub-agente `collector-roothints` |
| [dados.md](dados.md) | `roothints_server` e `roothints_run` (colunas, constraints, índices), mapeamento da fonte para as colunas, consultas da API | sub-agente `collector-roothints` |
| [collector.md](collector.md) | o que o coletor faz além do padrão: checagens pelo `.md5`, arquivo mais antigo pelo serial, `MIN_SERVERS`, trava de remoção com 13 linhas, `MERGE`, opções e variáveis `COLLECTOR_ROOTHINTS_*`, operação, medições, testes | sub-agente `collector-roothints` |
| [api.md](api.md) | a `api-roothints` além do padrão: valores, rotas e como acrescentar uma, cache e chaves normalizadas, consultas, configuração, manifesto, medições, testes | sub-agente `api-roothints` |
| [api-rotas.md](api-rotas.md) | cada rota da `api-roothints`: validação, normalização, campos, exemplos reais, erros, roteamento | sub-agente `api-roothints` |

Mexer no coletor: [../../padroes/coletor.md](../../padroes/coletor.md) →
[fonte.md](fonte.md) → [dados.md](dados.md) → [collector.md](collector.md).
Mexer na API: [../../padroes/api.md](../../padroes/api.md) e
[../../padroes/openapi.md](../../padroes/openapi.md) → [dados.md](dados.md) →
[api.md](api.md) → [api-rotas.md](api-rotas.md).

## API

*(Seção do sub-agente `api-roothints`.)* A `api-roothints` serve os servidores
raiz abaixo de `/roothints/` (versão atual e `/roothints/v1/`), só lendo
`roothints_server`, `roothints_run` e a linha do coletor em `jobs`: a lista
dos 13 em ordem de letra, com nome, letra, IPv4 e IPv6 sem máscara, TTLs e a
nota do bloco (`/roothints/servers`), e um servidor pela letra ou pelo nome,
em qualquer caixa e com ou sem o ponto final (`/roothints/server/k`,
`/roothints/server/K.ROOT-SERVERS.NET.`); as duas levam o bloco `source`
com a data e o serial da zona raiz do cabeçalho do arquivo. Além delas,
`/roothints/meta`, as rotas de saúde e o manifesto OpenAPI 3.1 em
`/roothints/openapi.yaml`. É pública, sem chave (rate limit por IP no
Traefik), com cache opcional no Valkey. Valores, cache, consultas,
configuração, medições e testes: [api.md](api.md); cada rota, com
validações, campos, exemplos reais e erros: [api-rotas.md](api-rotas.md).
