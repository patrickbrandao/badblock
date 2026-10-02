# Fonte: arquivo delegated-extended do RIPE NCC

O formato, o arquivo `.md5`, as regras do parser e a política de fixtures são
os do modelo: [../rir/formato.md](../rir/formato.md). Aqui, só o que é do
RIPE NCC. No BadBlock a fonte se chama `ripencc` (o valor do campo `registry`
do arquivo); nos textos para pessoas, "RIPE NCC".

## URLs

| O quê | URL |
|---|---|
| Arquivo | `https://ftp.ripe.net/pub/stats/ripencc/delegated-ripencc-extended-latest` |
| Hash publicado | `https://ftp.ripe.net/pub/stats/ripencc/delegated-ripencc-extended-latest.md5` |
| Arquivo do dia | `delegated-ripencc-extended-AAAAMMDD` (+ `.md5`) na mesma pasta |
| Histórico | `2026/delegated-ripencc-extended-AAAAMMDD.bz2` (+ `.md5` do arquivo descompactado), uma pasta por ano |
| Descrição do formato | `https://ftp.ripe.net/pub/stats/ripencc/RIR-Statistics-Exchange-Format.txt` |

## Publicação

- Um arquivo por dia, por volta de **22:12 UTC** — logo depois da meia-noite
  em Amsterdã (00:12 no horário de verão, `+0200`) —, com o cabeçalho do dia
  que terminou. O de 2026-09-28 saiu com `Last-Modified`
  `Mon, 28 Sep 2026 22:11:50 GMT`, `enddate` 20260928 e `serial` 1790632799
  (2026-09-28 23:59:59 `+0200`).
- O arquivo e o `.md5` saem com o mesmo `Last-Modified` (conferido nos
  últimos dias, em 2026-09-28).
- O servidor é um nginx. As respostas do arquivo e do `.md5` trazem
  `Last-Modified` e um `ETag` no estilo do nginx — a época Unix do
  `Last-Modified` e o tamanho em bytes, os dois em hexadecimal: em
  2026-09-28, `"6abae626-113deed"` no arquivo (18.079.469 bytes) e
  `"6abae626-4a"` no `.md5` (74 bytes). O servidor responde `304` a
  `If-None-Match` e a `If-Modified-Since` (conferido em 2026-09-28).
- Há também uma cópia `delegated-ripencc-extended-latest.txt`, idêntica
  (mesmo `ETag`, 2026-09-28); o coletor usa a URL sem `.txt`.
- O `.md5` está no formato BSD, com o nome `-latest` e **sem quebra de linha
  no fim** (74 bytes):

  ```
  MD5 (delegated-ripencc-extended-latest) = 9dc7efb094d86f3d03e3cfab51716cd1
  ```

## Formato

O delegated-extended do modelo, sem comentários. Início do arquivo de
2026-09-28 e alguns registros reais:

```
2|ripencc|1790632799|260793|19700101|20260928|+0200
ripencc|*|ipv4|*|100877|summary
ripencc|*|asn|*|48682|summary
ripencc|*|ipv6|*|111234|summary
ripencc|BG|ipv4|87.116.83.0|2304|20050913|allocated|162f9494-4727-4143-89e3-1a5a64454c0d
ripencc||ipv4|185.0.40.64|448||reserved
ripencc|BG|asn|9070|1|19981125|allocated|162f9494-4727-4143-89e3-1a5a64454c0d
ripencc||asn|1877|1||available
ripencc|BG|ipv6|2a02:4c8::|32|20080611|allocated|162f9494-4727-4143-89e3-1a5a64454c0d
```

Como é o arquivo do RIPE NCC (detalhes e números abaixo; a comparação com os
outros RIRs está em
[../rir/formato.md](../rir/formato.md#comparação-entre-os-rirs)). Só dele são
o `serial` em segundos, o `cc` `EU`, o opaque-id UUID novo a cada arquivo e os
blocos até `/29`; o resto aparece também em outro RIR, indicado entre
parênteses:

- `version` `2` (a descrição do formato fala em `2.3`; a AFRINIC também usa
  `2`);
- `serial` em **época Unix, em segundos**: o último segundo do dia do arquivo
  no fuso do cabeçalho (`1790632799` = 2026-09-28 23:59:59 `+0200`);
- `UTCoffset` `+0200` no horário de verão europeu (`+0100` no inverno);
- available e reserved com **7 campos** (sem o opaque-id), `cc` e data
  vazios (na LACNIC, só os available têm 7);
- `cc` `EU` (Europa, sem país específico) em alguns registros; nunca `ZZ`;
- opaque-id **UUID minúsculo**, novo a cada arquivo (abaixo);
- registros IPv4 que não formam um CIDR, inclusive desalinhados
  (`185.0.40.64` + 448) — a AFRINIC e a ARIN também têm —, e blocos até
  `/29`;
- nenhuma faixa de ASN: todo registro tem um ASN só (como na AFRINIC);
- linhas ordenadas por status dentro de cada tipo (a AFRINIC ordena assim só
  o IPv4).

## Opaque-id novo a cada arquivo

A descrição do formato avisa que o opaque-id não tem garantia de ser
constante entre versões do arquivo, e no RIPE NCC ele **é regenerado a cada
arquivo**: um UUID novo por titular, todo dia. Comparando os arquivos de
2026-09-27 e 2026-09-28 pelas chaves e colunas do `MERGE` do coletor
(`asn_start`, `prefix`), **nenhum** UUID se repetiu (44.828 titulares num,
44.811 no outro); 39.911 registros de ASN e 128.847 blocos mudaram só no
opaque-id, e 28 e 19 em outros campos. Dentro de um arquivo o agrupamento
vale: os recursos de um titular têm o mesmo UUID.

Consequências — esperadas, não são defeito, e o código é o do modelo, sem
tratamento especial:

- cada arquivo diário atualiza quase todo registro allocated/assigned (~169
  mil linhas) e pede consolidação ([collector.md](collector.md#atualização-diária));
- `updated_at` dessas linhas muda todo dia, e um `opaque_id` só serve dentro
  do dataset atual ([dados.md](dados.md#opaque_id-e-updated_at));
- a trava de remoção não é afetada: o que muda é atualização, não remoção.

## Fatos medidos (arquivo de 2026-09-28, serial 1790632799)

- 18.079.469 bytes; 260.797 linhas: 1 cabeçalho, 3 summaries (na ordem ipv4,
  asn, ipv6) e 260.793 registros. Cabeçalho versão `2`, `startdate`
  19700101, `enddate` 20260928, `UTCoffset` `+0200`. Sem comentários, sem
  linhas vazias, sem `\r`, só ASCII, termina em `\n`.
- Registros: 48.682 `asn`, 100.877 `ipv4`, 111.234 `ipv6` — batem com o
  cabeçalho e os summaries.
- Ordem das linhas: por tipo (ipv4, asn, ipv6, como os summaries); dentro do
  tipo, por status (allocated, assigned, reserved, available); dentro do
  status, por início crescente. O formato não garante ordem, e o parser não
  depende dela.
- Status: asn 39.927 allocated / 836 reserved / 7.919 available (nenhum
  assigned); ipv4 76.223 allocated / 24.308 assigned / 340 reserved / 6
  available; ipv6 22.647 allocated / 4.195 assigned / 83.570 reserved / 822
  available.
- Linhas `available` e `reserved` com **7 campos** (sem o opaque-id): 93.493
  (8.747 available, 84.746 reserved); allocated/assigned com 8. Nenhuma linha
  tem extensões depois do opaque-id.
- `cc` **vazio** em todos os available/reserved; nenhum `ZZ`. 151 códigos
  distintos, entre eles `EU` (393 registros: 383 ASNs e 10 blocos IPv4).
- Data vazia exatamente nos available/reserved; nenhuma `00000000`. De
  19700101 a 20260928; 19700101 só em 2 ASNs (AS6204 e AS6206), provável
  marcador de "sem data" do RIPE NCC, gravado como publicado.
- opaque-id UUID minúsculo (`8-4-4-4-12`) em todo allocated/assigned; 44.811
  titulares distintos, 22.704 com ASN e blocos. Muda todo dia (acima).
- ASN: todo registro tem um ASN só (nenhuma faixa), de AS7 a AS400158;
  22.937 ASNs de 32 bits (acima de 65535).
- IPv4: blocos CIDR de `/8` (`25.0.0.0/8` e `53.0.0.0/8`) a `/29` (9
  registros). **970 registros não formam um CIDR** (944 assigned, 18
  reserved, 8 allocated), de 2 a 10 pedaços (635 com 2, 196 com 3, 81 com 4,
  58 com 5 a 10), e viram 2.515 blocos, até `/30` — ex.: `87.116.83.0` +
  2304 = `/24` + `/22` + `/22`; `185.0.40.64` + 448 = `/26` + `/25` + `/24`.
  Total: 102.422 blocos IPv4 (99.907 registros que já são CIDR + 2.515
  pedaços).
- IPv6: de `/13` (`2a18::/13`, available) a `/48`; nenhum com bits de host.
- Nenhum registro repetido e nenhuma sobreposição de faixas (ASN, IPv4,
  IPv6).
- Variação real, fora o opaque-id: de 2026-09-27 para 2026-09-28, +1 ASN e
  +47/−26 blocos (0,012% dos blocos removidos); de 2026-09-21 para
  2026-09-28, +1 ASN e +199/−69 blocos (0,03%). Bem abaixo da trava de
  remoção (5%).
- O parser aceita o arquivo inteiro sem descartes nem avisos (tempos em
  [collector.md](collector.md#medições)).

## Recorte (`testdata/delegated-extended-sample.txt`)

Recorte real do arquivo de 2026-09-28: 22 linhas (1.355 bytes, LF, sem
comentários) — o cabeçalho `2|ripencc|1790632799|18|19700101|20260928|+0200`
(só `records` ajustado, de 260793 para 18), os três summaries na ordem do
arquivo com as contagens do recorte (ipv4 7, asn 6, ipv6 5) e 18 registros,
linhas inteiras do arquivo, na ordem dele — as linhas 5, 21.105, 76.253,
83.793, 84.995, 100.657, 100.879 (ipv4), 100.882, 102.019, 102.825, 140.808,
140.809, 141.645 (asn), 149.564, 153.427, 176.026, 176.406 e 260.797 (ipv6):

| Tipo | Registros | Cobre |
|---|---|---|
| ipv4 (7) | `1.178.112.0` + 4096 (PS, allocated, 20071126, o primeiro registro do arquivo); `87.116.83.0` + 2304 (BG, allocated, 20050913, titular `162f9494-…`); `25.0.0.0` + 16777216 (GB, assigned, 19950101); `139.191.0.0` + 65536 (EU, assigned, 19930125); `156.67.6.0` + 8 (DE, assigned, 19920327, `f7b5eb5d-2252-4fcc-97fa-2efcd27631a5`); `185.0.40.64` + 448 (reserved, 7 campos); `194.147.40.0` + 1024 (available, 7 campos) | `/8` e `/29` (o maior e o menor registro CIDR do arquivo); dois registros que não formam CIDR: `87.116.83.0` + 2304 → `/24` + `/22` + `/22` e o reservado desalinhado `185.0.40.64` + 448 → `/26` + `/25` + `/24`; país `EU` |
| asn (6) | AS7 (EU, allocated, 19930901, o menor ASN); AS6204 (RO, allocated, 19700101); AS9070 (BG, allocated, 19981125, titular `162f9494-…`); AS400158 (NL, allocated, 20211021, o maior ASN); AS5575 (reserved, 7 campos); AS1877 (available, 7 campos) | `EU`, data 19700101, ASN de 32 bits, reserved e available de 7 campos |
| ipv6 (5) | `2001:600::/29` (NL, allocated, 19990826, `a71dd866-c717-4529-950b-2040370c754a`); `2a02:4c8::/32` (BG, allocated, 20080611, titular `162f9494-…`); `2001:7f8:6::/48` (BG, assigned, 20011218, titular `162f9494-…`); `2001:609::/32` (reserved, 7 campos); `2a18::/13` (available, o maior bloco IPv6 e o último registro do arquivo) | `/13` e `/48`; reserved e available de 7 campos |

O titular `162f9494-4727-4143-89e3-1a5a64454c0d` tem ASN, IPv4 e IPv6 no
recorte: AS9070, `87.116.83.0` + 2304 (3 blocos), `2a02:4c8::/32` e
`2001:7f8:6::/48`. Available e reserved têm `cc`, data e opaque-id vazios em
ASN, IPv4 e IPv6. Para refazer o recorte, tire essas linhas do arquivo de
2026-09-28 (no histórico, `2026/delegated-ripencc-extended-20260928.bz2`) e
ajuste `records` e os `count` dos summaries.

Como é um clone, o `testdata/formats/` do `collector-ripencc` traz os
recortes da AFRINIC, APNIC, ARIN e LACNIC
([../rir/formato.md](../rir/formato.md#fixtures)). O `formats/ripencc.txt`
dos outros quatro coletores sai do mesmo arquivo de 2026-09-28: quatro
registros deste recorte (`156.67.6.0` + 8, AS1877, `2001:600::/29`,
`2001:609::/32`) e `62.122.208.0` + 1280 (RU, allocated, 20090417, a linha
11.412), o exemplo de IPv4 não-CIDR das specs do modelo. Os números que os
testes citam estão em [collector.md](collector.md#testes).
