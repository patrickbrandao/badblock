# Dados da APNIC

As tabelas `apnic_asn`, `apnic_prefix` e `apnic_run` são as do modelo, com
`<rir>` = `apnic`: colunas, constraints, índices, mapeamento e consultas da
API em [../rir/dados.md](../rir/dados.md). A migration é
`database/postgres/apnic/20260929000000_apnic.sql`. Aqui, só o que é da
APNIC.

## Migration

O DDL é o do modelo: a migration do lacnic com o nome trocado (conferido em
2026-09-29, comparando com a do lacnic depois de
`sed 's/lacnic/apnic/g; s/LACNIC/APNIC/g'`). Só três textos são da APNIC:

| Onde | Texto da APNIC |
|---|---|
| cabeçalho | a URL `https://ftp.apnic.net/stats/apnic/delegated-apnic-extended-latest` (sem `/pub`) |
| `COMMENT ON COLUMN apnic_run.start_date` | termina em `NULL = vazio ou 00000000 (a APNIC publica o campo vazio).` |
| `COMMENT ON COLUMN apnic_run.utc_offset` | `... como publicado (na APNIC, +1000).` (o modelo diz "ex.: -0300") |

Os outros textos são os genéricos do modelo: `format_version` "2 ou 2.3",
`serial` "data AAAAMMDD ou época Unix, conforme o RIR", `cc` "ZZ = sem
país, como publicado" (a APNIC não usa `ZZ`) e, na tabela `apnic_prefix`, a
divisão `62.122.208.0 + 1280 = /22 + /24` (um registro do RIPE NCC: na APNIC
todo IPv4 já é um CIDR).

## O que as tabelas guardam

Com o arquivo de 2026-09-28 (serial 20260929), pelos fatos de
[fonte.md](fonte.md#fatos-medidos-arquivo-de-2026-09-28-serial-20260929):

| Tabela | Conteúdo |
|---|---|
| `apnic_asn` | 14.762 linhas; `asn_count` > 1 em 743 (629 allocated, 105 available, 9 reserved), até 3.072 (AS143674–AS146745) |
| `apnic_prefix` | 175.506 linhas (61.684 IPv4 + 113.822 IPv6). Todo IPv4 forma um CIDR, então há um bloco por registro: `prefixes_v4` = `ipv4_records`, e em todo bloco IPv4 `record_start` é o endereço do `prefix` e `record_value` o tamanho dele. Blocos de `/8` a `/26` (IPv4) e de `/13` a `/64` (IPv6) |
| `apnic_run` (linha desse arquivo) | `format_version` `2.3`, `serial` `20260929`, `header_records` 190268, `start_date` NULL (`startdate` vazia), `end_date` 2026-09-28, `utc_offset` `+1000`, `asn_records` 14762, `ipv4_records` 61684, `ipv6_records` 113822, `prefixes_v4` 61684, `prefixes_v6` 113822, `bytes` 9251215 |

- `cc` nunca é `ZZ`: fica NULL nos available/reserved (100.951 linhas nas
  duas tabelas) e num bloco designado com titular (`2001:de3::/48`,
  `A919DB08`); `reg_date` é NULL exatamente nos available/reserved.
- `opaque_id` tem 8 dígitos hex maiúsculos (ex.: `A913D7D6`), com 26.807
  titulares distintos; NULL nos available/reserved.
- `start_date` fica NULL (a `startdate` vem vazia) e o `serial` é a data de
  Brisbane (8 dígitos), um dia depois do `end_date`: a checagem de arquivo
  mais antigo decide pelo `end_date` e, com o mesmo `end_date`, pelo `serial`
  ([../rir/collector.md](../rir/collector.md#arquivo-mais-antigo-que-o-aplicado)).

## Consultas no recorte

Com `testdata/delegated-extended-sample.txt` carregado (o que o teste de
integração confere, [fonte.md](fonte.md#recorte-testdatadelegated-extended-sampletxt)):

| Consulta | Resultado |
|---|---|
| ASN 45123 | registro 45122–45124, available |
| IP `14.128.5.10` | `14.128.4.0/22`, titular `A913D7D6` |
| titular `A913D7D6` | 4 recursos: AS55759, `14.128.4.0/22`, `202.50.32.0/20`, `2404:6600::/32` |
| blocos reserved com `cc`, `reg_date` e `opaque_id` NULL | 2 (`27.0.8.0/22`, `2001:7fa::/64`) |

A `api-apnic` faz as consultas do modelo: o `internal/store` dela é o da
`api-lacnic` com o nome trocado (conferido em 2026-09-29).
