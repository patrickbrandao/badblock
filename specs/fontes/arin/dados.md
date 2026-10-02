# Dados da ARIN

As tabelas `arin_asn`, `arin_prefix` e `arin_run` são as do modelo, com
`<rir>` = `arin`: colunas, constraints, índices, mapeamento e consultas da
API em [../rir/dados.md](../rir/dados.md). A migration é
`database/postgres/arin/20260929000000_arin.sql`. Aqui, só o que é da ARIN.

## Migration

O DDL é o do modelo com o nome trocado; só os textos dos comentários mudam
(conferido em 2026-09-29, comparando com a migration do lacnic). O
cabeçalho cita a URL
`https://ftp.arin.net/pub/stats/arin/delegated-arin-extended-latest`, e os
`COMMENT ON` trazem estas notas da ARIN no lugar dos exemplos genéricos:

| Onde | Nota da ARIN |
|---|---|
| `arin_run.format_version` | `2.3` na ARIN (o modelo diz "2 ou 2.3") |
| `arin_run.serial` | não é uma data: a época Unix em milissegundos da geração do arquivo (ex.: `1790600421096`) |
| `arin_run.start_date` | `1970-01-01` na ARIN |
| `arin_run.end_date` | na ARIN, o dia da publicação |
| `arin_run.utc_offset` | `-0400` no horário de verão de Nova York e `-0500` no inverno (o modelo dá o exemplo `-0300`) |
| `arin_asn.cc`, `arin_prefix.cc` | NULL em todos os available/reserved; a ARIN não usa `ZZ` (o modelo diz "ZZ = sem país, como publicado") |
| `arin_asn.reg_date` | "data da designação"; NULL em available, reserved e "~100 ASNs antigos (ex.: AS3)" — são 108 no arquivo de 2026-09-28 |
| `arin_prefix.reg_date` | "data da alocação"; NULL em available e reserved |
| `arin_asn.status` | todo ASN delegado vem como `assigned`; `allocated`/`assigned` sem o "a um LIR/ISP" e o "a um usuário final" do modelo |
| `arin_prefix.status` | todo bloco delegado vem como `allocated`, seja alocação a provedor ou designação a usuário final (idem, sem "LIR/ISP" e "usuário final") |
| `arin_asn.opaque_id`, `arin_prefix.opaque_id` | hash hex de 32 caracteres |
| `arin_prefix` (tabela) | a divisão em CIDRs só acontece em registros reserved; exemplo `23.128.1.0 + 768 = /24 + /23` (no lugar do `62.122.208.0 + 1280` do modelo) |

## O que as tabelas guardam

Com o arquivo de 2026-09-28 (serial 1790600421096), pelos fatos de
[fonte.md](fonte.md#fatos-medidos-arquivo-de-2026-09-28-serial-1790600421096):

| Tabela | Conteúdo |
|---|---|
| `arin_asn` | 32.988 linhas; `asn_count` > 1 em 144 (143 assigned, até 290 ASNs, e AS403010 available com 1.371) |
| `arin_prefix` | 173.950 linhas (84.737 IPv4 + 89.213 IPv6). Os 2.765 registros IPv4 reservados que não formam CIDR viram 6.652 blocos, então `prefixes_v4` (84.737) > `ipv4_records` (80.850); os blocos de um mesmo registro têm o mesmo `record_start`/`record_value` |
| `arin_run` (linha desse arquivo) | `format_version` `2.3`, `serial` `1790600421096`, `header_records` 203051, `start_date` 1970-01-01, `end_date` 2026-09-28, `utc_offset` `-0400`, `asn_records` 32988, `ipv4_records` 80850, `ipv6_records` 89213, `prefixes_v4` 84737, `prefixes_v6` 89213, `bytes` 12799845 |

- `serial` guarda os 13 dígitos da época em ms como texto; é por ele (com
  `end_date`) que o coletor recusa um arquivo mais antigo.
- `cc` nunca é `ZZ`: fica NULL em todos os available/reserved (82.931
  registros); `reg_date` é NULL nos mesmos registros e nos 108 ASNs assigned
  com data `00000000` (ex.: AS3).
- `status`: em `arin_asn` só aparecem `assigned`, `available` (1 linha) e
  `reserved`; em `arin_prefix`, `allocated`, `available` e `reserved`. Na
  ARIN, `allocated` num bloco **não** quer dizer "alocado a um LIR/ISP" nem
  `assigned` num ASN "designado a um usuário final": o dado não distingue
  alocação de designação.
- `opaque_id` é hex minúsculo de 32 caracteres, gravado como publicado, com
  39.138 titulares distintos; NULL em available/reserved. A consulta de
  titular do modelo procura o pedido também em minúsculas, então um
  opaque-id digitado em maiúsculas acha o titular
  ([../rir/dados.md](../rir/dados.md#consultas-da-api)).

## Consultas no recorte

Com `testdata/delegated-extended-sample.txt` carregado (o que o teste de
integração confere, [fonte.md](fonte.md#recorte-testdatadelegated-extended-sampletxt)):

| Consulta | Resultado |
|---|---|
| ASN 404000 | registro 403010–404380, available |
| IP `208.65.33.10` | `208.65.32.0/22`, titular `e0082a77a634f2cc5817ffb7de12b38e` |
| titular `e0082a77a634f2cc5817ffb7de12b38e` | 4 recursos: AS11472, `208.65.32.0/22`, `216.7.64.0/20`, `2605:8480::/32` |
| blocos reserved com `cc`, `reg_date` e `opaque_id` NULL | 6 (`23.128.1.0/24`, `23.128.2.0/23`, `23.128.81.0/24`, `64.112.29.0/24`, `64.112.30.0/24`, `260a::/15`) |
| blocos com `record_start` `23.128.1.0` e `record_value` 768 | 2 (`23.128.1.0/24` e `23.128.2.0/23`): o registro reservado que não forma CIDR |
