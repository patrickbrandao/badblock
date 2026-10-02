# Dados do RIPE NCC

As tabelas `ripencc_asn`, `ripencc_prefix` e `ripencc_run` são as do modelo,
com `<rir>` = `ripencc`: colunas, constraints, índices, mapeamento e
consultas da API em [../rir/dados.md](../rir/dados.md). A migration é
`database/postgres/ripencc/20260929000000_ripencc.sql`. Aqui, só o que é do
RIPE NCC.

## Migration

O DDL é o do modelo com o nome trocado (conferido em 2026-09-29 contra a
migration do lacnic); mudam só os textos, com as notas do RIPE NCC:

| Onde | Texto do RIPE NCC |
|---|---|
| cabeçalho | título `ripencc: delegações de ASNs e blocos IP publicadas pelo RIPE NCC`; URL `https://ftp.ripe.net/pub/stats/ripencc/delegated-ripencc-extended-latest`; na nota do `opaque_id`, que no RIPE NCC ele é um UUID regenerado a cada arquivo diário — vale dentro do dataset atual, não entre dias, e quase todo registro allocated/assigned é atualizado a cada arquivo novo |
| `ripencc_run.serial` | no RIPE NCC é a época Unix em segundos (ex.: `1790632799` = 23:59:59 do dia do arquivo no fuso do cabeçalho) |
| `ripencc_run.utc_offset` | exemplo `+0200` no horário de verão europeu |
| `ripencc_asn.cc`, `ripencc_prefix.cc` | maiúsculo, como publicado; o RIPE NCC usa também `EU` = Europa, sem país específico (outros RIRs usam `ZZ`); NULL = vazio na fonte (no RIPE NCC, todo available/reserved) |
| `ripencc_asn.reg_date`, `ripencc_prefix.reg_date` | NULL só em available e reserved; gravada como publicada — no ASN, "inclusive 19700101 (poucos registros antigos do RIPE NCC)" |
| `ripencc_asn.opaque_id`, `ripencc_prefix.opaque_id` | no RIPE NCC é um UUID regenerado a cada arquivo: vale só dentro do dataset atual, não entre dias |
| `COMMENT ON TABLE` de `ripencc_asn` e `ripencc_prefix` | "do RIPE NCC"; o exemplo de divisão do modelo, `62.122.208.0 + 1280 = /22 + /24`, é um registro real do RIPE NCC |

O `COMMENT ON` de `ripencc_run.format_version` fica com o texto do modelo
("2 ou 2.3"; o RIPE NCC publica `2`).

## O que as tabelas guardam

Com o arquivo de 2026-09-28 (serial 1790632799), pelos fatos de
[fonte.md](fonte.md#fatos-medidos-arquivo-de-2026-09-28-serial-1790632799):

| Tabela | Conteúdo |
|---|---|
| `ripencc_asn` | 48.682 linhas, todas com `asn_count` 1 (nenhuma faixa): `asn_end` = `asn_start`, de 7 a 400158, com 22.937 ASNs de 32 bits |
| `ripencc_prefix` | 213.656 linhas (102.422 IPv4 + 111.234 IPv6). 99.907 registros IPv4 formam um CIDR e viram um bloco cada (`record_start` = endereço do `prefix`, `record_value` = tamanho dele); os outros 970 viram 2.515 blocos, de 2 a 10 por registro, que repetem o `record_start`/`record_value` do registro |
| `ripencc_run` (linha desse arquivo) | `format_version` `2`, `serial` `1790632799`, `header_records` 260793, `start_date` 1970-01-01, `end_date` 2026-09-28, `utc_offset` `+0200`, `asn_records` 48682, `ipv4_records` 100877, `ipv6_records` 111234, `prefixes_v4` 102422, `prefixes_v6` 111234, `bytes` 18079469, `md5` `9dc7efb094d86f3d03e3cfab51716cd1` |

- `cc` nunca é `ZZ`: fica NULL em available/reserved — 93.523 linhas, 8.755
  em `ripencc_asn` e 84.768 em `ripencc_prefix` (30 a mais que os 93.493
  registros, porque 18 reservados IPv4 não formam CIDR e viram vários
  blocos). `EU` em 383 linhas de ASN e 10 blocos IPv4.
- `reg_date` é NULL exatamente nas mesmas linhas; `1970-01-01` só em AS6204
  e AS6206.
- `opaque_id` é um UUID minúsculo, com 44.811 titulares distintos (22.704
  com ASN e blocos); NULL em available/reserved.

## `opaque_id` e `updated_at`

O RIPE NCC gera um UUID novo por titular a cada arquivo
([fonte.md](fonte.md#opaque-id-novo-a-cada-arquivo)), e o `MERGE` compara o
`opaque_id` como qualquer coluna de dado. Por isso:

- cada aplicação diária **atualiza** quase toda linha allocated/assigned: de
  2026-09-27 para 2026-09-28, 39.911 das 48.682 linhas de `ripencc_asn` e
  128.847 das 213.656 de `ripencc_prefix` mudaram só no `opaque_id`;
- o `updated_at` dessas linhas muda todo dia e não quer dizer que país,
  data, status ou tamanho mudaram; `created_at` continua marcando a primeira
  vez que o registro apareceu na fonte;
- a busca por titular ([consultas da API](../rir/dados.md#consultas-da-api))
  vale dentro da versão atual do dataset: um opaque-id guardado ontem não
  acha nada hoje. Quem lê as tabelas (a `api-ripencc`, a consolidação da
  fase 2) pode listar os outros recursos do mesmo titular, mas não deve
  oferecer o opaque-id como identificador permanente.

## Consultas no recorte

Com `testdata/delegated-extended-sample.txt` carregado (o que o teste de
integração confere, [fonte.md](fonte.md#recorte-testdatadelegated-extended-sampletxt)):

| Consulta | Resultado |
|---|---|
| ASN 9070 | registro 9070–9070, allocated |
| ASN 9071 | o registro de maior `asn_start` ≤ 9071 é o do AS9070, mas `asn_end` 9070 < 9071: o ASN não está no arquivo |
| IP `87.116.90.10` | `87.116.88.0/22` (o terceiro pedaço de `87.116.83.0` + 2304: `record_start` `87.116.83.0`, `record_value` 2304), titular `162f9494-4727-4143-89e3-1a5a64454c0d` |
| titular `162f9494-4727-4143-89e3-1a5a64454c0d` | 6 recursos: AS9070, `87.116.83.0/24`, `87.116.84.0/22`, `87.116.88.0/22`, `2a02:4c8::/32`, `2001:7f8:6::/48` |
| blocos reserved com `cc`, `reg_date` e `opaque_id` NULL | 4 (`185.0.40.64/26`, `185.0.40.128/25`, `185.0.41.0/24`, `2001:609::/32`) |
