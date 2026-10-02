# Dados da LACNIC

As tabelas `lacnic_asn`, `lacnic_prefix` e `lacnic_run` são as do modelo, com
`<rir>` = `lacnic`: colunas, constraints, índices, mapeamento e consultas da
API em [../rir/dados.md](../rir/dados.md). A migration é
`database/postgres/lacnic/20260929000000_lacnic.sql`. Aqui, só o que é da
LACNIC.

## Migration

Por ser o modelo, os textos da migration do lacnic são os genéricos, que os
clones copiam e ajustam; não há nota só da LACNIC. O cabeçalho cita a URL
`https://ftp.lacnic.net/pub/stats/lacnic/delegated-lacnic-extended-latest`, e
os `COMMENT ON` trazem os exemplos do modelo: `utc_offset` "ex.: -0300",
`format_version` "2 ou 2.3", `serial` "data AAAAMMDD ou época Unix, conforme
o RIR", `cc` "ZZ = sem país, como publicado" e, na tabela `lacnic_prefix`, a
divisão `62.122.208.0 + 1280 = /22 + /24` (um registro do RIPE NCC: na LACNIC
todo IPv4 já é um CIDR).

## O que as tabelas guardam

Com o arquivo de 2026-09-28 (serial 20260927), pelos fatos de
[fonte.md](fonte.md#fatos-medidos-arquivo-de-2026-09-28-serial-20260927):

| Tabela | Conteúdo |
|---|---|
| `lacnic_asn` | 16.515 linhas; `asn_count` > 1 só em available (33 linhas, até 312) |
| `lacnic_prefix` | 80.786 linhas (20.804 IPv4 + 59.982 IPv6). Todo IPv4 forma um CIDR, então há um bloco por registro: `prefixes_v4` = `ipv4_records`, e em todo bloco IPv4 `record_start` é o endereço do `prefix` e `record_value` o tamanho dele |
| `lacnic_run` (linha desse arquivo) | `format_version` `2.3`, `serial` `20260927`, `header_records` 97301, `start_date` 1987-01-01, `end_date` 2026-09-25, `utc_offset` `-0300`, `asn_records` 16515, `ipv4_records` 20804, `ipv6_records` 59982, `bytes` 4566386 |

- `cc` nunca é `ZZ`: fica NULL em available/reserved (48.976 linhas nas duas
  tabelas); `reg_date` é NULL exatamente nos mesmos registros.
- `opaque_id` é numérico, gravado como texto (ex.: `258500`), com 14.811
  titulares distintos; NULL em available/reserved.

## Consultas no recorte

Com `testdata/delegated-extended-sample.txt` carregado (o que o teste de
integração confere, [fonte.md](fonte.md#recorte-testdatadelegated-extended-sampletxt)):

| Consulta | Resultado |
|---|---|
| ASN 28004 | registro 28003–28005, available |
| IP `187.87.29.10` | `187.87.28.0/22`, titular `258500` |
| titular `258500` | 4 recursos: AS61610, `187.87.28.0/22`, `200.225.48.0/21`, `2804:8ae0::/32` |
| blocos reserved com `cc`, `reg_date` e `opaque_id` NULL | 2 (`45.68.105.0/24`, `2001:12b8::/32`) |
