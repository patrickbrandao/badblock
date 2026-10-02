# Fonte: arquivo delegated-extended da LACNIC

O formato, o arquivo `.md5`, as regras do parser e a política de fixtures são
os do modelo: [../rir/formato.md](../rir/formato.md). Aqui, só o que é da
LACNIC.

## URLs

| O quê | URL |
|---|---|
| Arquivo | `https://ftp.lacnic.net/pub/stats/lacnic/delegated-lacnic-extended-latest` |
| Hash publicado | `https://ftp.lacnic.net/pub/stats/lacnic/delegated-lacnic-extended-latest.md5` |
| Histórico diário | `delegated-lacnic-extended-AAAAMMDD` (+ `.md5`, `.asc`) na mesma pasta |

## Publicação

- Um arquivo por dia, por volta de **23:56 (UTC−3)**, com o cabeçalho datado
  do dia (o `serial`); o `enddate` fica 2 dias antes do `serial` (no arquivo
  de serial 20260927, `enddate` 20260925).
- As respostas do arquivo e do `.md5` trazem `ETag` e `Last-Modified`, e o
  servidor responde `304` a `If-None-Match` e a `If-Modified-Since`
  (conferido em 2026-09-28).
- O `.md5` está no formato BSD, com o nome `-latest`:

  ```
  MD5 (delegated-lacnic-extended-latest) = 1d11010d5e9cc31ce507817ac4fae1bf
  ```

## Formato

O delegated-extended do modelo, sem comentários. Início do arquivo de
2026-09-28 e alguns registros reais:

```
2.3|lacnic|20260927|97301|19870101|20260925|-0300
lacnic|*|ipv4|*|20804|summary
lacnic|*|ipv6|*|59982|summary
lacnic|*|asn|*|16515|summary
lacnic|BR|ipv4|45.171.60.0|1024|20190211|allocated|258500
lacnic|BR|ipv6|2804:5964::|32|20190211|allocated|258500
lacnic|BR|asn|61613|1|20230505|allocated|258500
lacnic||ipv4|45.68.105.0|256||reserved|
lacnic||ipv6|2001:1201:20::|43||available
```

## Fatos medidos (arquivo de 2026-09-28, serial 20260927)

- 4.566.386 bytes; 97.305 linhas: 1 cabeçalho, 3 summaries (na ordem ipv4,
  ipv6, asn) e 97.301 registros. Cabeçalho versão `2.3`, `startdate`
  19870101, `enddate` 20260925, `UTCoffset` `-0300`. Sem comentários, sem
  linhas vazias, sem `\r`.
- Registros: 16.515 `asn`, 20.804 `ipv4`, 59.982 `ipv6` — batem com o
  cabeçalho e os summaries.
- Status: asn 14.364 allocated / 458 available / 1.693 reserved (nenhum
  assigned); ipv4 17.832 allocated / 2.579 assigned / 393 reserved; ipv6
  11.932 allocated / 1.618 assigned / 28.516 available / 17.916 reserved.
- Linhas `available` com **7 campos** (sem o opaque-id); `reserved` com 8 e
  o opaque-id vazio. Nenhuma linha tem extensões depois do opaque-id.
- `cc` **vazio** em todos os available/reserved (48.976 linhas); nenhum
  `ZZ`. Data vazia nos mesmos registros; nenhuma data `00000000`.
- opaque-id numérico de 2 a 6 dígitos (ex.: `258500`), 14.811 titulares
  distintos; o mesmo titular aparece em ASNs e blocos.
- **Todo registro IPv4 forma um CIDR** (de `/11` a `/24`): nenhuma divisão,
  um bloco por registro. IPv6 de `/13` a `/48`.
- Faixas de ASN com mais de um ASN só em available: 33 registros, a maior com
  312 ASNs.
- Nenhum registro repetido e nenhuma sobreposição de faixas (ASN, IPv4,
  IPv6).
- O parser aceita o arquivo inteiro sem descartes nem avisos (tempos em
  [collector.md](collector.md#medições)).

## Recorte (`testdata/delegated-extended-sample.txt`)

Recorte real do arquivo de 2026-09-28: 25 linhas (1.133 bytes, LF, sem
comentários) — o cabeçalho `2.3|lacnic|20260927|21|19870101|20260925|-0300`
(só `records` ajustado, de 97301 para 21), os três summaries na ordem do
arquivo com as contagens do recorte (ipv4 8, ipv6 6, asn 7) e 21 registros,
agrupados por tipo na mesma ordem e crescentes pelo início:

| Tipo | Registros | Cobre |
|---|---|---|
| ipv4 (8) | `2.152.0.0` + 1024 (GT, allocated, 20260714, `71316`); `2.152.252.0` + 1024 (PA, assigned, 20260826, `75377`); `45.68.105.0` + 256 (reserved, 8 campos com opaque-id vazio); `45.171.60.0` + 1024 (BR, allocated, 20190211, `258500`); `150.165.0.0` + 65536 (BR, assigned, 19930607, `130343`); `187.192.0.0` + 2097152 (MX, allocated, 20110606, `21461`); `200.17.0.0` + 4096 (BR, assigned, 20000216, `130343`); `200.192.152.0` + 1024 (BR, allocated, 20031125, `258500`) | todos CIDR, do `/11` (o maior da LACNIC) ao `/24` |
| ipv6 (6) | `2001:1201::/44` (MX, assigned, 20190404, `27835`); `2001:1201:20::/43` (available, 7 campos); `2001:12b8::/32` (reserved, 8 campos); `2001:12f0::/32` (BR, assigned, 20071219, `130343`); `2001:12f8::/48` (BR, assigned, 20071219, `114721`); `2804:5964::/32` (BR, allocated, 20190211, `258500`) | available de 7 e reserved de 8 campos, `/48` |
| asn (7) | AS1916 (BR, allocated, 19991116, `130343`); AS6064 (available, 7 campos); AS6065 (reserved, 8 campos); AS22548 (BR, allocated, 20011016, `114721`); AS26596 + 2 (available); AS28003 + 3 (available); AS61613 (BR, allocated, 20230505, `258500`) | faixas de ASN em available |

Titulares com ASN e blocos: `258500` (AS61613, `45.171.60.0/22`,
`200.192.152.0/22`, `2804:5964::/32`), `130343` (AS1916, `150.165.0.0/16`,
`200.17.0.0/20`, `2001:12f0::/32`) e `114721` (AS22548, `2001:12f8::/48`).
Available e reserved com `cc`, data e opaque-id vazios em ASN, IPv4 e IPv6.

Como é o modelo, o `testdata/formats/` do `collector-lacnic` traz os recortes
da AFRINIC, APNIC, ARIN e RIPE NCC; o `formats/lacnic.txt` dos clones sai
deste recorte ([../rir/formato.md](../rir/formato.md#fixtures)). Os números
que os testes citam estão em [collector.md](collector.md#testes).
