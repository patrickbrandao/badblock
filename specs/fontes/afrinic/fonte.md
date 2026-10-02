# Fonte: arquivo delegated-extended da AFRINIC

O formato, o arquivo `.md5`, as regras do parser e a política de fixtures são
os do modelo: [../rir/formato.md](../rir/formato.md). Aqui, só o que é da
AFRINIC.

## URLs

| O quê | URL |
|---|---|
| Arquivo | `https://ftp.afrinic.net/pub/stats/afrinic/delegated-afrinic-extended-latest` |
| Hash publicado | `https://ftp.afrinic.net/pub/stats/afrinic/delegated-afrinic-extended-latest.md5` |
| Assinatura PGP (não usada) | `https://ftp.afrinic.net/pub/stats/afrinic/delegated-afrinic-extended-latest.asc`, com a chave em `AFRINICPUBKEY.TXT`, na mesma pasta |
| Cópia do dia | `delegated-afrinic-extended-AAAAMMDD` (+ `.md5`, `.asc`) na mesma pasta |
| Histórico diário | `https://ftp.afrinic.net/pub/stats/afrinic/AAAA/delegated-afrinic-extended-AAAAMMDD` (+ `.md5`, `.asc`), sem compressão, uma pasta por ano (de `2005/` a `2026/`) |
| Descrição do formato | `README-EXTENDED.txt` e `RIR-Statistics-Exchange-Format.txt`, na mesma pasta do arquivo |

Conferido na listagem de `https://ftp.afrinic.net/pub/stats/afrinic/` de
2026-09-28, onde fica também o `delegated-afrinic-latest` (o formato sem a
extensão; não usado).

## Publicação

- Um arquivo por dia, por volta de **00:07 UTC**, com o cabeçalho datado do
  próprio dia em UTC (`serial` = `enddate`). O arquivo, o `.md5` e o `.asc`
  saem com o mesmo `Last-Modified` (ex.: `Mon, 28 Sep 2026 00:07:04 GMT`),
  então a janela em que o `.md5` não bate com o arquivo é mínima — não há o
  atraso da [APNIC](../apnic/collector.md#janela-do-md5).
- **Dias sem arquivo novo** acontecem: o histórico de setembro de 2026 tem
  cópias idênticas do dia anterior, com o mesmo cabeçalho e o mesmo MD5
  (`...-20260911` a `...-20260914` iguais ao de 10/09; `...-20260925` igual
  ao de 24/09). Nesses dias o `-latest` não muda, e a verificação para na
  checagem 1 (MD5 publicado igual). Às 00:44 UTC de 2026-09-29, o `-latest`
  ainda era o de 2026-09-28 (`Last-Modified` 2026-09-28 00:07:04).
- **Dias com os mesmos registros e só o cabeçalho novo** também acontecem:
  os arquivos de 26, 27 e 28/09/2026 só diferem na primeira linha. O MD5
  muda, e o coletor aplica o arquivo sem alterar linhas — versão nova em
  `afrinic_run`, `consolidated` intacto
  ([../rir/collector.md](../rir/collector.md#aplicação)).
- As respostas do arquivo e do `.md5` trazem `ETag` e `Last-Modified`, e o
  servidor responde `304` a `If-None-Match` e a `If-Modified-Since`
  (conferido em 2026-09-28/29). O `ETag` é o do Apache,
  `"<tamanho em hex>-<mtime em µs, em hex>"`: o do arquivo de 2026-09-28,
  `"f4082-65c7fd88d3a00"`, é 999.554 bytes e 2026-09-28 00:07:04 UTC; o do
  `.md5`, `"4a-65c7fd88d3a00"`, é 74 bytes com o mesmo mtime.
- O `.md5` está no formato BSD, com **74 bytes e sem quebra de linha no
  fim**; mesmo os `.md5` do histórico citam o nome `-latest`. Exemplo real
  (2026-09-28):

  ```
  MD5 (delegated-afrinic-extended-latest) = 5a82aad62da62ef064e04c62a8baaeb7
  ```

  `fetch.ParseMD5` lê a linha sem o `\n` e não confere o nome
  ([../rir/formato.md](../rir/formato.md#arquivo-md5)).

## Servidor

`ftp.afrinic.net` (conferido em 2026-09-28/29): 196.216.2.24, Apache 2.4.6
em CentOS com OpenSSL 1.0.1e-fips (cabeçalho `Server`), TLS 1.2 com
`ECDHE-RSA-AES128-GCM-SHA256` e certificado GeoTrust **válido até
2026-10-23**. É lento, mas estável:

| Medida | Valor |
|---|---|
| RTT | ~0,32 s |
| TLS pronto | ~1 s |
| Primeiro byte | ~1,3 s, mesmo para o `.md5` de 74 bytes (um `HEAD` do arquivo levou 1,64 s) |
| Download do arquivo (~1 MB, sem compressão gzip) | ~120 KB/s, de 7,5 a 10,3 s |
| Falhas | nenhuma em 2026-09-28/29: mais de 10 requisições ao `.md5` e 10 downloads completos |

O que isso quer dizer para o coletor (limites de tempo, certificado
vencido) está em [collector.md](collector.md#servidor-lento-e-certificado-tls).

## Formato

O delegated-extended do modelo, sem comentários. Início do arquivo de
2026-09-28 e alguns registros reais:

```
2|afrinic|20260928|19786|00000000|20260928|00000
afrinic|*|asn|*|4350|summary
afrinic|*|ipv4|*|6091|summary
afrinic|*|ipv6|*|9345|summary
afrinic|ZA|asn|1228|1|19910301|allocated|F36B9F4B
afrinic|ZZ|asn|8770|1||available|
afrinic|ZA|ipv4|196.4.20.0|2560|19930831|allocated|F369838C
afrinic|ZZ|ipv4|41.57.112.0|2048||reserved|
afrinic|ZA|ipv6|2001:4200::|32|20051021|allocated|F36B9F4B
```

| Campo | Na AFRINIC |
|---|---|
| `version` | `2` (a descrição do formato fala em `2.3`; o parser aceita qualquer `2.x`) |
| `serial` | a data do arquivo, `AAAAMMDD`, igual ao `enddate` |
| `startdate` | `00000000` (sem data) |
| `enddate` | o dia da publicação, em UTC |
| `UTCoffset` | `00000` (cinco zeros), guardado como publicado |
| `cc` | `ZZ` em todo available/reserved, e só neles; nunca vazio |
| `value` de ASN | sempre 1: um ASN por linha, sem faixas, inclusive em available/reserved |
| `date` | vazia em available/reserved; nunca `00000000` |
| `status` | os quatro, mas nenhum ASN `assigned` (os ASNs delegados são `allocated`) |
| opaque-id | 8 dígitos hex maiúsculos (ex.: `F36B9F4B`); vazio em available/reserved, que mesmo assim têm 8 campos (`...||available|`) |

- **Ordem dos registros**: por tipo, na ordem dos summaries (asn, ipv4,
  ipv6). ASNs e IPv6 vêm crescentes pelo início, com os status misturados;
  os IPv4 vêm agrupados por status — allocated (3.865), assigned (1.657),
  reserved (555) e available (14) —, cada grupo crescente pelo endereço. O
  parser não depende da ordem.
- **`ZZ`** fica `ZZ` no banco ([normalização](../rir/formato.md#normalização)):
  como o `cc` nunca vem vazio, a AFRINIC não tem `cc` NULL.
- **IPv4 que não forma CIDR**: 52 registros, todos allocated ou assigned
  (com país e titular), viram os CIDRs mínimos
  ([../rir/formato.md](../rir/formato.md#ipv4-divisão-em-cidrs)). Exemplos:
  `164.146.0.0` + 393216 → `/15` + `/14`; `196.4.20.0` + 2560 → `/22` +
  `/22` + `/23`; o maior caso, `196.6.1.0` + 25600 → 9 blocos, de
  `196.6.1.0/24` a `196.6.100.0/24`.
- **Arquivo mais antigo**: com `serial` = `enddate`, a checagem do coletor
  decide pelo `enddate`
  ([../rir/collector.md](../rir/collector.md#arquivo-mais-antigo-que-o-aplicado)).

## Fatos medidos (arquivo de 2026-09-28, serial 20260928)

- 999.554 bytes; 19.790 linhas: 1 cabeçalho, 3 summaries (na ordem asn,
  ipv4, ipv6) e 19.786 registros — o menor dos cinco RIRs. Cabeçalho versão
  `2`, `serial` = `enddate` = 20260928, `startdate` `00000000`, `UTCoffset`
  `00000`. Sem comentários, sem linhas vazias, sem `\r`, só ASCII, com `\n`
  no fim.
- MD5 `5a82aad62da62ef064e04c62a8baaeb7` (o do `.md5` publicado); SHA-256
  `5594fbfe9cf656e9e23ff25c061ec7c0dbefec7872c4e7720d3074f956c39046`.
- Registros: 4.350 `asn`, 6.091 `ipv4`, 9.345 `ipv6` — batem com o
  cabeçalho e os summaries.
- Status: asn 2.797 allocated / 1.127 available / 426 reserved (nenhum
  assigned); ipv4 3.865 allocated / 1.657 assigned / 14 available / 555
  reserved; ipv6 1.287 allocated / 390 assigned / 4.649 available / 3.019
  reserved.
- Toda linha de registro tem **8 campos**: available e reserved terminam com
  o opaque-id vazio (`...||available|`). Nenhuma linha tem extensões.
- `cc` **`ZZ`** em todos os available/reserved (9.790 registros) e só neles;
  nenhum `cc` vazio. Os allocated/assigned têm 56 países (África, mais `RE` e
  `YT`; ZA 3.498, NG 852, KE 840...). Data vazia exatamente nos
  available/reserved; nenhuma data `00000000`. A data mais antiga é
  19840101 (7 registros), a mais nova 20260924.
- opaque-id hex maiúsculo de 8 caracteres (ex.: `F36B9F4B`), 2.962
  titulares distintos; o mesmo titular aparece em ASNs e blocos (2.431 têm
  ASN e bloco; 1.440, ASN, IPv4 e IPv6). O titular com mais registros é
  `F3619C8C` (CI), com 187.
- Todo registro de ASN tem `value` 1, inclusive available/reserved: não há
  faixas. ASNs de 1228 a 330751.
- IPv4 de `/11` a `/24`. **52 registros IPv4 não formam um CIDR** (5
  allocated, 47 assigned) e viram 146 blocos, de 2 a 9 por registro — 13
  deles têm tamanho potência de 2, mas início desalinhado (ex.:
  `168.209.0.0` + 131072 → `/16` + `/16`); os outros 6.039 viram um bloco
  cada: 6.185 blocos IPv4. IPv6 de `/13` a `/48`, nenhum com bits de host:
  9.345 blocos. 15.530 blocos no total.
- Nenhum registro repetido e nenhuma sobreposição de faixas (ASN, IPv4,
  IPv6).
- Variação pequena de um dia para o outro: entre 10/09 e 28/09/2026, nenhum
  ASN entrou ou saiu (12 alterados), 168 blocos entraram, 8 mudaram e 33
  saíram (0,21% dos blocos); de 23/09 para 24/09, 29 blocos entraram, 5
  saíram e 3 ASNs mudaram. O total de registros foi de 19.651 (10/09) a
  19.786 (26 a 28/09). A trava de remoção (5%) fica longe.
- O parser aceita o arquivo inteiro sem descartes nem avisos (tempos em
  [collector.md](collector.md#medições)).

Para conferir o parser com o arquivo do dia: `make test-real FILE=...`
([collector.md](collector.md#testes)). O `TestParseRealFile` loga o
cabeçalho, as contagens por tipo, os blocos, os descartes e os avisos, que
devem bater com estes fatos (dentro da variação diária).

## Recorte (`testdata/delegated-extended-sample.txt`)

Recorte real do arquivo de 2026-09-28: 19 linhas (888 bytes, LF, sem
comentários) — o cabeçalho `2|afrinic|20260928|15|00000000|20260928|00000`
(só `records` ajustado, de 19786 para 15), os três summaries na ordem do
arquivo com as contagens do recorte (asn 5, ipv4 6, ipv6 4) e 15 registros,
linhas inteiras do arquivo, na ordem dele — as linhas 5, 27, 44, 55, 3.417
(asn), 7.281, 7.334, 7.594, 8.332, 9.877, 10.434 (ipv4), 10.446, 10.447,
10.450 e 12.521 (ipv6):

| Tipo | Registros | Cobre |
|---|---|---|
| asn (5) | AS1228 (ZA, allocated, 19910301, `F36B9F4B`, o primeiro registro do arquivo); AS6187 (ZA, allocated, 19840101, `F36771FA`); AS8770 (`ZZ`, available); AS10803 (`ZZ`, reserved); AS329814 (NE, allocated, 20260918, `F3626E7D`) | a data mais antiga do arquivo; available e reserved com `ZZ`, data vazia e o opaque-id vazio no fim da linha (8 campos); ASN de 32 bits |
| ipv4 (6) | `160.115.0.0` + 65536 (ZA, allocated, 19840101, `F36180A1`); `164.146.0.0` + 393216 (ZA, allocated, 19930312, `F363E51A`); `196.4.20.0` + 2560 (ZA, allocated, 19930831, `F369838C`); `102.201.36.0` + 1024 (NE, assigned, 20260918, `F3626E7D`); `41.57.112.0` + 2048 (`ZZ`, reserved); `102.201.0.0` + 4096 (`ZZ`, available) | dois registros que não formam CIDR — `164.146.0.0` + 393216 → `/15` + `/14` e `196.4.20.0` + 2560 → `/22` + `/22` + `/23` —, então 6 registros viram 9 blocos; os quatro status, na ordem do arquivo (allocated, assigned, reserved, available) |
| ipv6 (4) | `2001:4200::/32` (ZA, allocated, 20051021, `F36B9F4B`); `2001:4201::/32` (`ZZ`, reserved); `2001:4208::/29` (`ZZ`, available); `2001:43fe:3800::/48` (NE, assigned, 20260918, `F3626E7D`) | de `/29` a `/48`; available e reserved com `ZZ` |

Titulares com ASN e blocos: `F3626E7D` (AS329814, `102.201.36.0/22` e
`2001:43fe:3800::/48`, os três de 2026-09-18) e `F36B9F4B` (AS1228 e
`2001:4200::/32`). Available e reserved têm `cc` `ZZ` e data e opaque-id
vazios em ASN, IPv4 e IPv6.

Para refazer: do arquivo de 2026-09-28 (no histórico,
`2026/delegated-afrinic-extended-20260928`), a linha 1 com `records` = 15,
as linhas 2 a 4 com os `count` do recorte e as 15 linhas acima. Com o
arquivo de outro dia, ache cada registro pelo tipo e pelo `start` (ex.:
`grep -F '|ipv4|196.4.20.0|'`), use o cabeçalho daquele dia (só `records`
ajustado) e ajuste os testes que citam o recorte
([collector.md](collector.md#testes)); se um registro mudou ou sumiu da
fonte, troque-o por outro com a mesma característica.

Como clone, o `testdata/formats/` do `collector-afrinic` traz `apnic.txt`,
`arin.txt` e `ripencc.txt` (iguais aos do modelo) e o `lacnic.txt` dos
clones com um registro a mais, AS6065 (reserved, 8 campos com o opaque-id
vazio): 11 linhas, 7 registros, summary asn 3 (conferido em 2026-09-29;
[../rir/formato.md](../rir/formato.md#fixtures)). O `formats/afrinic.txt`
dos outros quatro coletores é outro recorte do mesmo arquivo de 2026-09-28,
descrito no modelo: 6 registros, todos também neste recorte (AS1228, AS8770,
`164.146.0.0` + 393216, `196.4.20.0` + 2560, `41.57.112.0` + 2048 e
`2001:4200::/32`).
