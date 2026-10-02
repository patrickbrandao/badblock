# Site `anatel` — Agência Nacional de Telecomunicações

A Anatel publica, no portal de dados abertos
(`https://www.anatel.gov.br/dadosabertos/`), vários conjuntos de dados
independentes, cada um com formato e ritmo próprios. Cada conjunto que o
BadBlock coleta é uma **fonte de dois níveis**, `anatel/<conjunto>`, com o
seu par de apps, tabelas `anatel_<conjunto>_*` e pasta de specs aqui
([../../projeto/estrutura.md](../../projeto/estrutura.md#fonte-de-dois-níveis-siteconjunto),
decisão #21 em [../../projeto/decisoes.md](../../projeto/decisoes.md)).

Esta pasta não é uma fonte: não há `apps/anatel/collector/` nem tabelas
`anatel_*` fora dos conjuntos.

## Conjuntos

| Conjunto | O que traz | Apps | Tabelas | Caminho | Porta local | Specs |
|---|---|---|---|---|---|---|
| `pst` | prestadoras de serviços de telecomunicações (CNPJ) e os serviços notificados por cada uma | `collector-anatel-pst`, `api-anatel-pst` | `anatel_pst_provider`, `anatel_pst_service`, `anatel_pst_run` | `/anatel/pst/` | 8112 | [pst/](pst/README.md) |

Um conjunto novo da Anatel segue [../../processos/nova-fonte.md](../../processos/nova-fonte.md)
e ganha uma linha aqui e em [../README.md](../README.md).
