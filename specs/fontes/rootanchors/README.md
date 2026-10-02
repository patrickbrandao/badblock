# rootanchors — âncoras de confiança DNSSEC da raiz (IANA `root-anchors.xml`)

O arquivo [`root-anchors.xml`](https://data.iana.org/root-anchors/root-anchors.xml)
da IANA traz as **âncoras de confiança DNSSEC da zona raiz**: as KSKs (chaves
de assinatura de chave) da raiz, cada uma com o registro DS (key tag,
algoritmo, tipo e valor do digest), a validade (`validFrom`/`validUntil`) e,
nas mais novas, o DNSKEY (flags e chave pública). É um XML de ~1,9 KB com 3
chaves (2026-09-30), que muda só nas etapas de uma rolagem de KSK (o
`Last-Modified` medido é de 2024-11-05), com o SHA-256 publicado no `checksums-sha256.txt` da
mesma pasta. A IANA mantém no arquivo as chaves já aposentadas. É uma das três
fontes da raiz do DNS (decisão #18 em
[../../projeto/decisoes.md](../../projeto/decisoes.md)), ao lado de
[`roothints`](../roothints/README.md) e [`rootzone`](../rootzone/README.md).

| Item | Valor |
|---|---|
| Fonte | `rootanchors` |
| Coletor | `collector-rootanchors`, em [`apps/rootanchors/collector/`](../../../apps/rootanchors/collector/) — sub-agente `.claude/agents/collector-rootanchors.md` |
| API | `api-rootanchors`, em [`apps/rootanchors/api/`](../../../apps/rootanchors/api/) — sub-agente `.claude/agents/api-rootanchors.md` |
| Tabelas | `rootanchors_key` (uma chave por `KeyDigest` do arquivo), `rootanchors_run` (execuções) |
| Migrations | [`database/postgres/rootanchors/`](../../../database/postgres/rootanchors/) |
| Linha em `jobs` | `app = 'collector-rootanchors'` |
| Caminho HTTP | `/rootanchors/` (`https://api.badblock.net.br/rootanchors/`) |
| Porta local | 8111 (`API_ROOTANCHORS_HOST_PORT`, só no loopback) |
| Imagens | `tmsoftbrasil/badblock-collector-rootanchors`, `tmsoftbrasil/badblock-api-rootanchors` |

## Arquivos

| Arquivo | Conteúdo | Dono |
|---|---|---|
| [fonte.md](fonte.md) | URLs, servidor HTTP e `checksums-sha256.txt`, formato XML com o arquivo real, fatos medidos, regras do parser (recálculo do key tag e do digest), fixtures, a assinatura `.p7s` como trabalho futuro | sub-agente `collector-rootanchors` |
| [dados.md](dados.md) | `rootanchors_key` e `rootanchors_run` (colunas, constraints, índices), mapeamento do XML para as colunas, consultas da API | sub-agente `collector-rootanchors` |
| [collector.md](collector.md) | o que o coletor faz além do padrão: hash numa linha do `checksums-sha256.txt`, gzip e ETag fraco, `MIN_KEYS`, trava de remoção com 3 linhas, opções e variáveis `COLLECTOR_ROOTANCHORS_*`, operação, medições, testes | sub-agente `collector-rootanchors` |
| [api.md](api.md) | a `api-rootanchors` além do padrão: valores, dados servidos (DS e DNSKEY em texto), por que não há campo "ativa", cache e ETags, consultas, configuração, manifesto, medições, testes | sub-agente `api-rootanchors` |
| [api-rotas.md](api-rotas.md) | cada rota da `api-rootanchors`: validação do key tag, campos, exemplos reais, erros, roteamento | sub-agente `api-rootanchors` |

Mexer no coletor: [../../padroes/coletor.md](../../padroes/coletor.md) →
[fonte.md](fonte.md) → [dados.md](dados.md) → [collector.md](collector.md).
Mexer na API: [../../padroes/api.md](../../padroes/api.md) e
[../../padroes/openapi.md](../../padroes/openapi.md) → [dados.md](dados.md) →
[api.md](api.md) → [api-rotas.md](api-rotas.md).

## API

*(Seção do sub-agente `api-rootanchors`.)* A `api-rootanchors` serve as
âncoras de confiança abaixo de `/rootanchors/` (versão atual e
`/rootanchors/v1/`), só lendo `rootanchors_key`, `rootanchors_run` e a linha
do coletor em `jobs`: todas as chaves do arquivo, inclusive as aposentadas
(`/rootanchors/keys`), e as chaves de um key tag (`/rootanchors/key/20326` —
sempre uma lista, porque o key tag não é único), cada uma com os campos da
tabela, o registro DS pronto em texto (`. IN DS 20326 8 2 E06D…`) e, quando o
arquivo publica a chave pública, o DNSKEY em texto; além de
`/rootanchors/meta`, das rotas de saúde e do manifesto OpenAPI 3.1 em
`/rootanchors/openapi.yaml`. Não há campo "ativa": ele depende do relógio e
mentiria numa resposta em cache; o cliente calcula com `valid_from` e
`valid_until`. É pública, sem chave (rate limit por IP no Traefik), com cache
opcional no Valkey. Valores, decisões, cache, configuração, medições e
testes: [api.md](api.md); cada rota, com validações, campos, exemplos reais e
erros: [api-rotas.md](api-rotas.md).
