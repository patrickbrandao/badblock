# Dados da AFRINIC

As tabelas `afrinic_asn`, `afrinic_prefix` e `afrinic_run` são as do modelo,
com `<rir>` = `afrinic`: colunas, constraints, índices, mapeamento e
consultas da API em [../rir/dados.md](../rir/dados.md). A migration é
`database/postgres/afrinic/20260929000000_afrinic.sql`. Aqui, só o que é da
AFRINIC.

## Migration

O DDL é o do modelo com o nome trocado (conferido em 2026-09-29, comparando
com a migration do lacnic depois de
`sed 's/lacnic/afrinic/g; s/LACNIC/AFRINIC/g'`, sem os comentários). O
cabeçalho cita a URL
`https://ftp.afrinic.net/pub/stats/afrinic/delegated-afrinic-extended-latest`,
e os `COMMENT ON` trazem estas notas da AFRINIC no lugar dos exemplos
genéricos:

| Onde | Nota da AFRINIC |
|---|---|
| `afrinic_run.format_version` | `campo version; a AFRINIC publica 2` (o modelo diz "2 ou 2.3") |
| `afrinic_run.serial` | `na AFRINIC, a data AAAAMMDD do arquivo, igual a end_date` (o modelo diz "data AAAAMMDD ou época Unix, conforme o RIR") |
| `afrinic_run.start_date` | termina em `(a AFRINIC publica 00000000)` |
| `afrinic_run.utc_offset` | `(a AFRINIC publica 00000)` (o modelo dá o exemplo `-0300`) |
| `COMMENT ON TABLE afrinic_asn` | `(na AFRINIC, todos têm um ASN só)` no lugar de "(a maioria tem um ASN só)" |
| `afrinic_asn.cc`, `afrinic_prefix.cc` | `ZZ = sem país, como publicado (a AFRINIC usa ZZ em todo available/reserved). NULL = campo vazio na fonte.` (o modelo diz que o NULL é "comum em available/reserved") |
| `afrinic_asn.reg_date`, `afrinic_prefix.reg_date` | `NULL = vazio ou 00000000 na fonte (na AFRINIC, só available e reserved)` (o modelo cita também "registros antigos") |
| `COMMENT ON TABLE afrinic_prefix` | o exemplo de divisão `196.4.20.0 + 2560 = /22 + /22 + /23`, um registro real da AFRINIC, no lugar do `62.122.208.0 + 1280 = /22 + /24` do modelo |

## O que as tabelas guardam

Com o arquivo de 2026-09-28 (serial 20260928), pelos fatos de
[fonte.md](fonte.md#fatos-medidos-arquivo-de-2026-09-28-serial-20260928):

| Tabela | Conteúdo |
|---|---|
| `afrinic_asn` | 4.350 linhas, todas com `asn_count` 1 (nenhuma faixa): `asn_end` = `asn_start`, de 1228 a 330751 |
| `afrinic_prefix` | 15.530 linhas (6.185 IPv4 + 9.345 IPv6). 6.039 registros IPv4 formam um CIDR e viram um bloco cada (`record_start` = endereço do `prefix`, `record_value` = tamanho dele); os outros 52, todos allocated ou assigned, viram 146 blocos, de 2 a 9 por registro, que repetem país, data, status, titular e o `record_start`/`record_value` do registro. Por isso `prefixes_v4` (6.185) > `ipv4_records` (6.091) |
| `afrinic_run` (linha desse arquivo) | `format_version` `2`, `serial` `20260928`, `header_records` 19786, `start_date` NULL (`startdate` `00000000`), `end_date` 2026-09-28, `utc_offset` `00000`, `asn_records` 4350, `ipv4_records` 6091, `ipv6_records` 9345, `prefixes_v4` 6185, `prefixes_v6` 9345, `bytes` 999554, `md5` `5a82aad62da62ef064e04c62a8baaeb7`, `etag` `"f4082-65c7fd88d3a00"`, `last_modified` `Mon, 28 Sep 2026 00:07:04 GMT` |

- `cc` **nunca é NULL**: é `ZZ` em todos os available/reserved — 9.790
  linhas, 1.553 em `afrinic_asn` e 8.237 em `afrinic_prefix` (os IPv4
  available/reserved formam CIDR, um bloco por registro) — e um país nos
  demais. `ZZ` quer dizer sem país (como no `COMMENT ON`); nos outros quatro
  RIRs o mesmo caso fica NULL.
- `reg_date` é NULL exatamente nas mesmas linhas (available/reserved).
- `opaque_id` tem 8 dígitos hex maiúsculos (ex.: `F36B9F4B`), com 2.962
  titulares distintos (2.431 com ASN e blocos); NULL em available/reserved.
- `start_date` é sempre NULL (`startdate` `00000000`), e o `serial` guarda a
  mesma data do `end_date`, como texto: a checagem de arquivo mais antigo
  decide pelo `end_date`
  ([../rir/collector.md](../rir/collector.md#arquivo-mais-antigo-que-o-aplicado)).

## Consultas no recorte

Com `testdata/delegated-extended-sample.txt` carregado (o que o teste de
integração confere, [fonte.md](fonte.md#recorte-testdatadelegated-extended-sampletxt)):

| Consulta | Resultado |
|---|---|
| ASN 329814 | registro 329814–329814, allocated |
| ASN 10000 | o registro de maior `asn_start` ≤ 10000 é o do AS8770, mas `asn_end` 8770 < 10000: o ASN não está no arquivo |
| IP `102.201.37.10` | `102.201.36.0/22`, titular `F3626E7D` |
| titular `F3626E7D` | 3 recursos: AS329814, `102.201.36.0/22`, `2001:43fe:3800::/48` |
| blocos reserved com `cc` `ZZ`, `reg_date` e `opaque_id` NULL | 2 (`41.57.112.0/21`, `2001:4201::/32`) |
