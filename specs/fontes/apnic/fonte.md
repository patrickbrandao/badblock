# Fonte: arquivo delegated-extended da APNIC

O formato, o arquivo `.md5`, as regras do parser e a política de fixtures são
os do modelo: [../rir/formato.md](../rir/formato.md). Aqui, só o que é da
APNIC.

## URLs

| O quê | URL |
|---|---|
| Arquivo | `https://ftp.apnic.net/stats/apnic/delegated-apnic-extended-latest` |
| Hash publicado | `https://ftp.apnic.net/stats/apnic/delegated-apnic-extended-latest.md5` |
| Assinatura PGP (não usada) | `https://ftp.apnic.net/stats/apnic/delegated-apnic-extended-latest.asc` |
| Cópia do dia | `delegated-apnic-extended-AAAAMMDD` (+ `.md5`, `.asc`) na mesma pasta |
| Histórico | `https://ftp.apnic.net/stats/apnic/<ano>/delegated-apnic-extended-AAAAMMDD` (+ `.gz`, `.md5`, `.asc`) |

Diferente dos outros quatro RIRs (`/pub/stats/<rir>/`), o caminho da APNIC
**não tem `/pub`**: `ftp.apnic.net/stats/apnic/`. É o valor de
`rir.DefaultSourceURL` ([../rir/README.md](../rir/README.md#parâmetros-dos-coletores)).

## Publicação

- Um arquivo por dia, sempre por volta de **15:15 UTC** (01:15 em Brisbane,
  12:15 em Brasília); o `.md5` e o `.asc` saem **~9 minutos depois**
  (~15:24 UTC). Conferido na listagem de `https://ftp.apnic.net/stats/apnic/2026/`
  (arquivos de 22 a 29/09/2026): o atraso de 9 minutos se repete todo dia.
  Nessa janela o `-latest` já é o arquivo novo e o `.md5` ainda é o do dia
  anterior; o que o coletor faz com isso está em
  [collector.md](collector.md#janela-do-md5).
- O `serial` é a data de geração em Brisbane (UTC+10), um dia depois do
  `enddate` (no arquivo baixado em 2026-09-28, serial 20260929 e `enddate`
  20260928). A `startdate` vem vazia.
- Servidor nginx (HTTP/2). As respostas do arquivo e do `.md5` trazem `ETag`
  e `Last-Modified`, e o servidor responde `304` a `If-None-Match` e a
  `If-Modified-Since` (conferido em 2026-09-28). O `ETag` é o do nginx,
  `"<mtime em hex>-<tamanho em hex>"`: o de 2026-09-28, `"6aba8476-8d298f"`,
  é 2026-09-28 15:15:02 UTC e 9.251.215 bytes — o arquivo de serial 20260929
  (decodificado em 2026-09-29).
- O `.md5` está no formato BSD, com o nome `-latest`, em 73 bytes (a linha
  de 72 caracteres e o `\n` do fim). Exemplo real (2026-09-28):

  ```
  MD5 (delegated-apnic-extended-latest) = a30f818f9c90fa519bdbcb1c298edb17
  ```

## Formato

O delegated-extended do modelo, com um **bloco de 27 linhas de comentário**
antes do cabeçalho (dos cinco RIRs, só a APNIC publica comentários). Início
do arquivo de 2026-09-28 (serial 20260929) e alguns registros reais:

```
######################################################################
#
...
######################################################################
#
2.3|apnic|20260929|190268||20260928|+1000
apnic|*|asn|*|14762|summary
apnic|*|ipv4|*|61684|summary
apnic|*|ipv6|*|113822|summary
apnic|AU|ipv4|1.0.0.0|256|20110811|assigned|A91872ED
apnic|NZ|ipv4|14.128.4.0|1024|20100914|allocated|A913D7D6
apnic||ipv4|27.0.8.0|1024||reserved|
apnic|NZ|ipv6|2404:6600::|32|20100914|allocated|A913D7D6
apnic||ipv6|2001:201::|32||available|
apnic|NZ|asn|55759|1|20100914|allocated|A913D7D6
apnic|CN|asn|143674|3072|20210507|allocated|A91E5D61
```

O bloco de comentários (como no recorte, copiado do arquivo):

| Linhas | Conteúdo |
|---|---|
| 1 e 26 | 70 `#` |
| 2, 21, 25 e 27 | só `#` |
| 3 e 4 | o título `CONDITIONS OF USE` (`#`, espaço, **tabulação** e o título) e o sublinhado (`# ` e 68 `_`) |
| 5 a 19 | o texto das condições de uso dos relatórios, com linhas `# ` entre os parágrafos |
| 20 a 24 | `For more information see:` e as duas URLs da descrição do formato: `http://www.apnic.net/db/rir-stats-format.html` (22ª, depois de `#` e 7 espaços), `# or` (23ª) e `ftp://ftp.apnic.net/pub/apnic/stats/apnic/README-EXTENDED.TXT` (24ª, depois de `#` e uma **tabulação**) |

Doze linhas do bloco terminam com um espaço. O parser ignora todas (linha
que começa com `#`, depois de tirar os espaços das pontas); o cabeçalho é a
28ª linha.

## Fatos medidos (arquivo de 2026-09-28, serial 20260929)

- 9.251.215 bytes; 190.299 linhas: 27 de comentário (duas com tabulação), 1
  cabeçalho, 3 summaries (na ordem asn, ipv4, ipv6) e 190.268 registros. Sem
  linhas vazias, sem `\r`, só ASCII.
- Cabeçalho versão `2.3`, `serial` 20260929, `startdate` **vazia**,
  `enddate` 20260928, `UTCoffset` `+1000`.
- Registros na ordem ipv4, ipv6, asn (diferente da dos summaries): 14.762
  `asn`, 61.684 `ipv4`, 113.822 `ipv6` — batem com o cabeçalho e os
  summaries.
- Status: asn 13.907 allocated / 546 available / 309 reserved (nenhum
  assigned); ipv4 43.009 allocated / 15.203 assigned / 3.023 available / 449
  reserved; ipv6 9.383 allocated / 7.815 assigned / 96.062 available / 562
  reserved.
- Toda linha tem **8 campos**: available e reserved terminam com o opaque-id
  vazio (`||available|`). Nenhuma linha tem extensões.
- `cc`: 88 países; **vazio** em todos os available/reserved (100.951 linhas)
  e em **um** registro designado com titular (`2001:de3::/48`, assigned,
  `A919DB08`); nenhum `ZZ`. Data vazia exatamente nos available/reserved;
  nenhuma data `00000000`; datas de 19830613 a 20260928.
- opaque-id com 8 dígitos hex maiúsculos (ex.: `A913D7D6`), 26.807
  titulares distintos; o mesmo titular aparece em ASNs e blocos.
- **Todo registro IPv4 forma um CIDR** (nenhuma divisão), de `/8` a `/26`
  (8 registros `/26`, nenhum `/25`); IPv6 de `/13` a `/64` (6 registros
  `/64`). Blocos: 61.684 IPv4 + 113.822 IPv6 = 175.506.
- 743 faixas de ASN com mais de um ASN (629 allocated, 105 available, 9
  reserved), a maior com 3.072 (AS143674–AS146745); maior ASN: 202348.
- Nenhum registro repetido e nenhuma sobreposição de faixas (ASN, IPv4,
  IPv6).
- Variação diária pequena: do arquivo de serial 20260928 para o de 20260929,
  ASN +3/~4/−0 e blocos +88/~9/−16 (inseridos/alterados/apagados; 0,01%
  removido); em uma semana (20260922 → 20260929), 0,07% dos blocos
  removidos — longe da trava de 5% (`REMOVAL_THRESHOLD`).
- O parser aceita o arquivo inteiro sem descartes nem avisos (tempos em
  [collector.md](collector.md#medições)).

Para conferir o parser com o arquivo do dia: `make test-real FILE=...`
([collector.md](collector.md#testes)). O `TestParseRealFile` loga o
cabeçalho, as contagens por tipo, os blocos, os descartes e os avisos, que
devem bater com estes fatos (dentro da variação diária).

## Recorte (`testdata/delegated-extended-sample.txt`)

Recorte real do arquivo de 2026-09-28 (serial 20260929): 52 linhas (2.139
bytes, LF, ASCII) — o bloco de 27 linhas de comentário como está no arquivo
(inclusive as tabulações e os espaços no fim das linhas), o cabeçalho
`2.3|apnic|20260929|21||20260928|+1000` (só `records` ajustado, de 190268
para 21), os três summaries na ordem do arquivo com as contagens do recorte
(asn 8, ipv4 7, ipv6 6) e 21 registros, agrupados por tipo na ordem do
arquivo (ipv4, ipv6, asn) e crescentes pelo início:

| Tipo | Registros | Cobre |
|---|---|---|
| ipv4 (7) | `1.0.0.0` + 256 (AU, assigned, 20110811, `A91872ED`); `14.128.4.0` + 1024 (NZ, allocated, 20100914, `A913D7D6`); `14.160.0.0` + 2097152 (VN, allocated, 20100816, `A92BA6BC`); `27.0.8.0` + 1024 (reserved, opaque-id vazio no fim); `163.61.160.0` + 64 (available); `163.61.160.64` + 64 (BD, assigned, 20250417, `A91CDF1C`); `202.50.32.0` + 4096 (NZ, allocated, 19940514, `A913D7D6`) | todos CIDR, do `/11` aos dois `/26` (o menor bloco IPv4 da APNIC) |
| ipv6 (6) | `2001:200::/35` (JP, allocated, 19990813, `A916B6AA`); `2001:201::/32` (available); `2001:7fa::/64` (reserved); `2001:7fa:0:1::/64` (HK, assigned, 20020116, `A91972B6`); `2001:de3::/48` (sem país, assigned, 20250703, `A919DB08`); `2404:6600::/32` (NZ, allocated, 20100914, `A913D7D6`) | `/64` (o menor bloco IPv6 da APNIC) em reserved e assigned; `2001:de3::/48`, o único registro do arquivo com titular e sem país |
| asn (8) | AS173 (JP, allocated, 20020801, `A91A4B1A`); AS1768 + 2 (TW, allocated, 20020801, `A91BDB29`); AS7619 (reserved); AS9427 + 3 (reserved); AS17830 + 2 (available); AS45122 + 3 (available); AS55759 (NZ, allocated, 20100914, `A913D7D6`); AS143674 + 3072 (CN, allocated, 20210507, `A91E5D61`) | faixas de ASN em allocated (2 ASNs e 3.072, a maior do arquivo), available e reserved |

Titular com ASN e blocos: `A913D7D6` (AS55759, `14.128.4.0/22`,
`202.50.32.0/20`, `2404:6600::/32`). Available e reserved com `cc`, data e
opaque-id vazios em ASN, IPv4 e IPv6, todos com 8 campos (o `|` do fim).

Para refazer com o arquivo do dia: as 27 primeiras linhas (`head -27`), o
cabeçalho, os três summaries e os 21 registros acima, achados pelo tipo e
pelo `start` (ex.: `grep -F '|ipv4|14.128.4.0|'`), com `records` = 21 e cada
`count` = registros daquele tipo no recorte. Se um registro mudou ou sumiu da
fonte, troque-o por outro com a mesma característica e ajuste os testes que
citam o recorte ([collector.md](collector.md#testes)).

Como clone, o `testdata/formats/` do `collector-apnic` traz `afrinic.txt`,
`arin.txt` e `ripencc.txt` (iguais aos do modelo) e o `lacnic.txt` dos
clones, sem variação (conferido em 2026-09-29;
[../rir/formato.md](../rir/formato.md#fixtures)). O `formats/apnic.txt` dos
outros quatro coletores é outro recorte do mesmo arquivo, com o mesmo bloco
de comentários e 5 registros (4 deles também estão aqui; `14.102.240.0` +
4096 não), descrito no modelo.
