# Site `ripe` — RIPE NCC

O RIPE NCC publica, em `https://ftp.ripe.net/ripe/`, vários conjuntos de
dados independentes, cada um com formato e ritmo próprios. Cada conjunto que
o BadBlock coleta (fora as delegações do RIR) é uma **fonte de dois níveis**,
`ripe/<conjunto>`, com o seu par de apps, tabelas `ripe_<conjunto>_*` e pasta
de specs aqui
([../../projeto/estrutura.md](../../projeto/estrutura.md#fonte-de-dois-níveis-siteconjunto),
decisão #21 em [../../projeto/decisoes.md](../../projeto/decisoes.md)).

Esta pasta não é uma fonte: não há `apps/ripe/collector/` nem tabelas
`ripe_*` fora dos conjuntos. As delegações do RIPE NCC (delegated-extended)
são outra fonte, de um nível, da família RIR: [`ripencc`](../ripencc/README.md).

## Conjuntos

| Conjunto | O que traz | Apps | Tabelas | Caminho | Porta local | Specs |
|---|---|---|---|---|---|---|
| `asnames` | nome e país de todos os ASNs alocados (`asn.txt`) | `collector-ripe-asnames`, `api-ripe-asnames` | `ripe_asnames_asn`, `ripe_asnames_run` | `/ripe/asnames/` | 8108 | [asnames/](asnames/README.md) |

Um conjunto novo do RIPE NCC segue [../../processos/nova-fonte.md](../../processos/nova-fonte.md)
e ganha uma linha aqui e em [../README.md](../README.md).
