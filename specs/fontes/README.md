# Fontes de dados

Cada fonte tem um par de apps (`collector-<fonte>` e `api-<fonte>`), tabelas
`<fonte>_*`, uma pasta de migrations e uma pasta de specs aqui. O que é comum
a todos os coletores e APIs está em [../padroes/](../padroes/coletor.md); as
pastas abaixo trazem só o que é de cada fonte.

| Fonte | Quem publica | O que traz | Família | Caminho | Porta local | Tabelas | Specs |
|---|---|---|---|---|---|---|---|
| `cgibr` | NIC.br / registro.br | ASNs, titulares (CNPJ) e blocos IP brasileiros | — | `/cgibr/` | 8101 | `cgibr_asn`, `cgibr_prefix`, `cgibr_run` | [cgibr/](cgibr/README.md) |
| `afrinic` | AFRINIC | delegações de ASNs e blocos (delegated-extended) | RIR | `/afrinic/` | 8102 | `afrinic_asn`, `afrinic_prefix`, `afrinic_run` | [afrinic/](afrinic/README.md) |
| `apnic` | APNIC | idem | RIR | `/apnic/` | 8103 | `apnic_asn`, `apnic_prefix`, `apnic_run` | [apnic/](apnic/README.md) |
| `arin` | ARIN | idem | RIR | `/arin/` | 8104 | `arin_asn`, `arin_prefix`, `arin_run` | [arin/](arin/README.md) |
| `lacnic` | LACNIC | idem (é o modelo da família) | RIR | `/lacnic/` | 8105 | `lacnic_asn`, `lacnic_prefix`, `lacnic_run` | [lacnic/](lacnic/README.md) |
| `ripencc` | RIPE NCC | idem | RIR | `/ripencc/` | 8106 | `ripencc_asn`, `ripencc_prefix`, `ripencc_run` | [ripencc/](ripencc/README.md) |
| `iana` | IANA | blocos de ASN e IP por RIR, uso especial (bogons), bootstrap RDAP | — | `/iana/` | 8107 | `iana_asn_block`, `iana_prefix_block`, `iana_special_asn`, `iana_special_prefix`, `iana_rdap_service`, `iana_run` | [iana/](iana/README.md) |
| `asnames` | RIPE NCC (`asn.txt`) | nome e país de todos os ASNs alocados | — | `/asnames/` | 8108 | `asnames_asn`, `asnames_run` | [asnames/](asnames/README.md) |
| `roothints` | InterNIC (`named.root`) | nomes e endereços IPv4/IPv6 dos 13 servidores raiz do DNS (root hints) | — | `/roothints/` | 8109 | `roothints_server`, `roothints_run` | [roothints/](roothints/README.md) |
| `rootzone` | InterNIC (`root.zone`) | a zona raiz do DNS: TLDs delegados, servidores, glue e DS | — | `/rootzone/` | 8110 | `rootzone_tld`, `rootzone_record`, `rootzone_run` | [rootzone/](rootzone/README.md) |
| `rootanchors` | IANA (`root-anchors.xml`) | âncoras de confiança DNSSEC da raiz (KSKs) | — | `/rootanchors/` | 8111 | `rootanchors_key`, `rootanchors_run` | [rootanchors/](rootanchors/README.md) |
| `anatel/pst` | Anatel (site [`anatel`](anatel/README.md)) | prestadoras de serviços de telecomunicações (CNPJ) e os serviços que cada uma notificou (SCM, STFC, SMP…) | — | `/anatel/pst/` | 8112 | `anatel_pst_provider`, `anatel_pst_service`, `anatel_pst_run` | [anatel/pst/](anatel/pst/README.md) |

Para cada fonte: apps em `apps/<fonte>/collector/` e `apps/<fonte>/api/`,
migrations em `database/postgres/<fonte>/`, sub-agentes
`.claude/agents/collector-<fonte>.md` e `.claude/agents/api-<fonte>.md`.
Numa fonte de dois níveis (`<site>/<conjunto>`, ex.: `anatel/pst`), cada um
desses nomes usa a forma do lugar — `apps/anatel/pst/`,
`database/postgres/anatel_pst/`, `collector-anatel-pst.md`
([../projeto/estrutura.md](../projeto/estrutura.md#fonte-de-dois-níveis-siteconjunto)) —, e
`specs/fontes/<site>/README.md` lista os conjuntos do site.

## Família RIR

Os cinco RIRs publicam o mesmo formato (delegated-extended) e seus apps são o
mesmo código, clonado do `lacnic`. A spec comum da família está em
[rir/](rir/README.md) — `rir/` **não é uma fonte** (não há `apps/rir/`); as
pastas de cada RIR trazem só os valores, fatos e exemplos daquele RIR.

## Pasta de uma fonte

Todas têm os mesmos arquivos, com o mesmo papel:

| Arquivo | Conteúdo | Dono |
|---|---|---|
| `README.md` | resumo da fonte, apps, tabelas, caminho, porta, índice dos arquivos | sub-agente do coletor (a seção da API, o da API) |
| `fonte.md` | de onde vem o dado: URLs, publicação, formato, fatos medidos (com data), regras do parser, fixtures | sub-agente do coletor |
| `dados.md` | tabelas `<fonte>_*`: colunas, tipos, constraints, índices, mapeamento fonte → colunas, consultas que a API usa | sub-agente do coletor |
| `collector.md` | o que o coletor da fonte faz de diferente ou a mais que o [padrão](../padroes/coletor.md): checagens de mudança, validações, aplicação, opções e valores, operação, medições, testes | sub-agente do coletor |
| `api.md` | rotas da API com exemplos reais, validações, regras de negócio, chaves de cache, opções e valores, medições, testes | sub-agente da API |

- Um arquivo que passe de ~400 linhas pode ser dividido em
  `<arquivo>-<tema>.md` (ex.: `api-rotas.md`), listado no `README.md` da
  fonte.
- Nos RIRs, `dados.md`, `collector.md` e `api.md` começam apontando o
  arquivo do modelo em `rir/` e trazem só o que é do RIR.
- A API **lê** `fonte.md` e `dados.md` e não os altera: precisa de uma coluna
  ou índice novo? O pedido vai para o coletor (e a migration para
  `database/postgres/<fonte>/`).

## Fonte nova

Siga [../processos/nova-fonte.md](../processos/nova-fonte.md).
