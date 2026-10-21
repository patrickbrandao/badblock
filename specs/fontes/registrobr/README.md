# Site `registrobr` — serviço RDAP do registro.br

O NIC.br, pelo registro.br, responde RDAP (RFC 9082/9083) em
`https://rdap.registro.br/` para os números brasileiros: ASNs (`autnum`),
blocos IP (`ip`), entidades (`entity`) e domínios reversos (`domain`). Não
há arquivo publicado: cada objeto é uma consulta HTTP, e o servidor limita
quem consulta demais ([rdapnum/fonte.md](rdapnum/fonte.md#limite-do-servidor)).
Cada conjunto que o BadBlock coleta desse serviço é uma **fonte de dois
níveis**, `registrobr/<conjunto>`, com o seu par de apps, tabelas
`registrobr_<conjunto>_*` e pasta de specs aqui
([../../projeto/estrutura.md](../../projeto/estrutura.md#fonte-de-dois-níveis-siteconjunto),
decisão #21 em [../../projeto/decisoes.md](../../projeto/decisoes.md)).

Esta pasta não é uma fonte: não há `apps/registrobr/collector/` nem tabelas
`registrobr_*` fora dos conjuntos. O arquivo diário de ASNs e blocos do
NIC.br (`nicbr-asn-blk-latest.txt`) é outra fonte, de um nível:
[`cgibr`](../cgibr/README.md) — é dela que os conjuntos daqui tiram a lista
do que consultar.

## Conjuntos

| Conjunto | O que traz | Apps | Tabelas | Caminho | Porta local | Specs |
|---|---|---|---|---|---|---|
| `rdapnum` | o RDAP de cada ASN e de cada bloco do `cgibr`: titular, CNPJ, representante legal, contatos e e-mails, datas, delegações reversas | `collector-registrobr-rdapnum`, `api-registrobr-rdapnum` | `registrobr_rdapnum_*` ([dados](rdapnum/dados.md#tabelas)) | `/registrobr/rdapnum/` | 8114 | [rdapnum/](rdapnum/README.md) |

Previsto, sem specs ainda: `registrobr/rdap`, a busca **profunda** — IPs
específicos, entidades (`/entity/<handle>`), domínios reversos e outros
objetos sob demanda. O `rdapnum` não consulta nada além de `autnum` e `ip`
dos blocos do `cgibr`.

Um conjunto novo segue [../../processos/nova-fonte.md](../../processos/nova-fonte.md)
e ganha uma linha aqui e em [../README.md](../README.md).
