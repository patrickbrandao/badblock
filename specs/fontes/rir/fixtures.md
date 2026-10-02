# Fixtures `testdata/formats/` (conteúdo literal)

Recortes reais mínimos dos arquivos delegated-extended de cada RIR, usados
pelo `TestParseOtherRIRFormats` de todo coletor de RIR para garantir que o
mesmo parser aceita os cinco formatos (o que cada um cobre está em
[formato.md](formato.md#fixtures)). Estão aqui por inteiro para que possam ser
recriados sem baixar de novo os arquivos de 2026-09-28: são dados públicos e
pequenos.

Cada coletor tem os recortes dos **outros quatro** RIRs: o modelo
(`collector-lacnic`) tem `afrinic.txt`, `apnic.txt`, `arin.txt` e
`ripencc.txt`; cada clone troca o próprio por um `lacnic.txt`. Os quatro
primeiros são idênticos em todos os coletores que os têm; o `lacnic.txt` tem
três variantes (abaixo).

Dono: sub-agente `collector-lacnic`. Mudou uma fixture, mude este arquivo junto
(e a tabela de [formato.md](formato.md#fixtures)).

## `afrinic.txt`

```
2|afrinic|20260928|6|00000000|20260928|00000
afrinic|*|asn|*|2|summary
afrinic|*|ipv4|*|3|summary
afrinic|*|ipv6|*|1|summary
afrinic|ZA|asn|1228|1|19910301|allocated|F36B9F4B
afrinic|ZZ|asn|8770|1||available|
afrinic|ZA|ipv4|164.146.0.0|393216|19930312|allocated|F363E51A
afrinic|ZA|ipv4|196.4.20.0|2560|19930831|allocated|F369838C
afrinic|ZZ|ipv4|41.57.112.0|2048||reserved|
afrinic|ZA|ipv6|2001:4200::|32|20051021|allocated|F36B9F4B
```

## `apnic.txt`

```
######################################################################
#
# 	CONDITIONS OF USE
# ____________________________________________________________________
# 
# 
# The files are freely available for download and use on the condition 
# that APNIC will not be held responsible for any loss or damage 
# arising from the use of the information contained in these reports.
# 
# APNIC endeavours to the best of its ability to ensure the accuracy 
# of these reports; however, APNIC makes no guarantee in this regard.
# 
# In particular, it should be noted that these reports seek to 
# indicate where resources were first allocated or assigned. It is not
# intended that these reports be considered as an authoritative 
# statement of the location in which any specific resource may 
# currently be in use.
# 
# For more information see: 
#
#       http://www.apnic.net/db/rir-stats-format.html
# or
#	ftp://ftp.apnic.net/pub/apnic/stats/apnic/README-EXTENDED.TXT
#
######################################################################
#
2.3|apnic|20260929|5||20260928|+1000
apnic|*|asn|*|1|summary
apnic|*|ipv4|*|3|summary
apnic|*|ipv6|*|1|summary
apnic|AU|ipv4|1.0.0.0|256|20110811|assigned|A91872ED
apnic||ipv4|14.102.240.0|4096||available|
apnic||ipv4|163.61.160.0|64||available|
apnic||ipv6|2001:7fa::|64||reserved|
apnic|TW|asn|1768|2|20020801|allocated|A91BDB29
```

## `arin.txt`

```
2.3|arin|1790600421096|4|19700101|20260928|-0400
arin|*|asn|*|2|summary
arin|*|ipv4|*|1|summary
arin|*|ipv6|*|1|summary
arin|US|asn|3|1|00000000|assigned|d98c567cda2db06e693f2b574eafe848
arin||asn|212|1||reserved|
arin||ipv4|23.128.1.0|768||reserved|
arin|US|ipv6|2001:4:112::|48|20240912|allocated|9064d089391b6dcc310a2188da60c437
```

## `ripencc.txt`

```
2|ripencc|1790632799|5|19700101|20260928|+0200
ripencc|*|asn|*|1|summary
ripencc|*|ipv4|*|2|summary
ripencc|*|ipv6|*|2|summary
ripencc|RU|ipv4|62.122.208.0|1280|20090417|allocated|fa4615bf-0770-4e9a-8002-e125f84d6794
ripencc|DE|ipv4|156.67.6.0|8|19920327|assigned|f7b5eb5d-2252-4fcc-97fa-2efcd27631a5
ripencc||asn|1877|1||available
ripencc|NL|ipv6|2001:600::|29|19990826|allocated|a71dd866-c717-4529-950b-2040370c754a
ripencc||ipv6|2001:609::|32||reserved
```

## `lacnic.txt`

Nos clones `collector-apnic` e `collector-ripencc`:

```
2.3|lacnic|20260927|6|19870101|20260925|-0300
lacnic|*|ipv4|*|2|summary
lacnic|*|ipv6|*|2|summary
lacnic|*|asn|*|2|summary
lacnic||ipv4|45.68.105.0|256||reserved|
lacnic|BR|ipv4|45.171.60.0|1024|20190211|allocated|258500
lacnic||ipv6|2001:1201:20::|43||available
lacnic|BR|ipv6|2804:5964::|32|20190211|allocated|258500
lacnic||asn|28003|3||available
lacnic|BR|asn|61613|1|20230505|allocated|258500
```

Variantes:

- `collector-afrinic`: acrescenta o registro `lacnic||asn|6065|1||reserved|`
  entre a linha do `2804:5964::` e a faixa `28003` e, por isso, o cabeçalho
  diz `7` registros e o resumo de ASN, `3`.
- `collector-arin`: a faixa available é `lacnic||asn|26596|2||available` em vez
  de `lacnic||asn|28003|3||available`.
