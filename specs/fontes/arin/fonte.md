# Fonte: arquivo delegated-extended da ARIN

O formato, o arquivo `.md5`, as regras do parser e a política de fixtures são
os do modelo: [../rir/formato.md](../rir/formato.md). Aqui, só o que é da
ARIN.

## URLs

| O quê | URL |
|---|---|
| Arquivo | `https://ftp.arin.net/pub/stats/arin/delegated-arin-extended-latest` |
| Hash publicado | `https://ftp.arin.net/pub/stats/arin/delegated-arin-extended-latest.md5` |
| Histórico diário | `delegated-arin-extended-AAAAMMDD` (+ `.md5`) na mesma pasta; os anos anteriores em `archive/` |

## Publicação

- Um arquivo por dia, por volta das **09:00 de Nova York** — 13:00 UTC no
  horário de verão e 14:00 UTC no inverno (em junho e julho de 2026 saiu às
  09:00 UTC) —, com o `enddate` igual ao dia da publicação. O de 2026-09-28
  foi gerado às 13:00:21 UTC (é o `serial`, ver [Formato](#formato)).
- Em dias sem mudança nos registros (ex.: fim de semana), só o cabeçalho
  muda (`serial` e `enddate`). O MD5 é outro, então o coletor baixa e aplica
  um arquivo novo que não altera nenhuma linha: versão nova do dataset, sem
  `consolidated = 0` ([../rir/collector.md](../rir/collector.md#aplicação)).
- Servidor nginx. As respostas do arquivo e do `.md5` trazem `ETag` e
  `Last-Modified`, e o servidor responde `304` a `If-None-Match` e a
  `If-Modified-Since` (conferido em 2026-09-28). O `ETag` é o do nginx,
  `"<mtime>-<tamanho>"` em hex: `"6aba64eb-c34f65"` = mtime 1790600427
  (2026-09-28 13:00:27 UTC) e 12.799.845 bytes. O `.md5` sai junto com o
  arquivo (`Last-Modified` 1 s depois).
- **Vários servidores**: `ftp.arin.net` resolve para 3 endereços IPv4 e 3
  IPv6 (2026-09-28), servidores com cópias de horários diferentes (ex.:
  `Last-Modified` 13:00:23 num e 13:00:27 noutro). O `ETag` e o
  `Last-Modified` mudam conforme o servidor que atende, e o `304` só vem
  quando a verificação cai num servidor com a mesma cópia do último download
  aplicado. O conteúdo e o `.md5` são iguais em todos: a checagem pelo MD5
  publicado (a 1ª) e a do SHA-256 (a 3ª) resolvem, e o custo é no máximo um
  download a mais quando o `.md5` está fora do ar. Na janela de sincronização
  entre os servidores, o arquivo e o `.md5` podem vir de cópias diferentes: a
  conferência recusa o arquivo (linha `status = 0`, `md5 divergente: ...`) e
  a próxima verificação resolve
  ([../rir/collector.md](../rir/collector.md#validações-do-arquivo-novo)).
- O `.md5` (67 bytes, com `\n` no fim) está no formato **GNU** — o único
  entre os cinco RIRs em 2026-09-28 — e cita o nome do arquivo **datado**,
  não o `-latest`:

  ```
  0bb8d65f1ff9d820f4bb56844f9948be  delegated-arin-extended-20260928
  ```

  `fetch.ParseMD5` aceita o GNU e não confere o nome
  ([../rir/formato.md](../rir/formato.md#arquivo-md5)). Pelo nome, o
  `-latest` é o próprio arquivo datado do dia, que fica no histórico.

## Formato

O delegated-extended do modelo, sem comentários. Início do arquivo de
2026-09-28 e alguns registros reais:

```
2.3|arin|1790600421096|203051|19700101|20260928|-0400
arin|*|asn|*|32988|summary
arin|*|ipv4|*|80850|summary
arin|*|ipv6|*|89213|summary
arin|US|asn|11472|1|20081218|assigned|e0082a77a634f2cc5817ffb7de12b38e
arin|US|ipv4|208.65.32.0|1024|20140701|allocated|e0082a77a634f2cc5817ffb7de12b38e
arin||ipv4|23.128.1.0|768||reserved|
arin|US|ipv6|2605:8480::|32|20140611|allocated|e0082a77a634f2cc5817ffb7de12b38e
arin||ipv6|2001:488::|29||available|
```

| Campo | Na ARIN |
|---|---|
| `version` | `2.3` |
| `serial` | **não é uma data**: a época Unix em **milissegundos** da geração do arquivo, 13 dígitos (`1790600421096` = 2026-09-28 13:00:21 UTC) |
| `startdate` | `19700101` |
| `enddate` | o dia da publicação |
| `UTCoffset` | o fuso de Nova York: `-0400` no horário de verão, `-0500` no inverno |
| `cc` | vazio em available/reserved; a ARIN não usa `ZZ` |
| `date` | `00000000` em 108 ASNs antigos (ex.: AS3); vazia em available/reserved |
| `status` | `assigned` em todo ASN delegado e `allocated` em todo bloco delegado (além de `available` e `reserved`): o status **não distingue** alocação (a provedor) de designação (a usuário final) |
| opaque-id | hash hex de **32 caracteres minúsculos**; vazio em available/reserved, que mesmo assim têm 8 campos (`...||reserved|`) |

- **Arquivo mais antigo**: o coletor compara primeiro o `enddate` e, com o
  mesmo `enddate`, o `serial` — 13 dígitos dos dois lados, então a comparação
  de texto é a numérica e a checagem funciona como nos RIRs de serial em data
  ([../rir/collector.md](../rir/collector.md#arquivo-mais-antigo-que-o-aplicado)).
  Um serial de outro tamanho (em segundos, ou uma data) não seria comparável
  com o aplicado, e o arquivo seguiria.
- **IPv4 que não forma CIDR**: só em registros `reserved`, as lacunas entre
  blocos alocados (ex.: em `23.128.0.0/10`), como `23.128.1.0` + 768 →
  `23.128.1.0/24` + `23.128.2.0/23`
  ([../rir/formato.md](../rir/formato.md#ipv4-divisão-em-cidrs)). Quando a
  ARIN aloca um pedaço de uma lacuna, a linha reservada muda de tamanho de um
  dia para o outro: algumas inserções e remoções de blocos por dia.

## Fatos medidos (arquivo de 2026-09-28, serial 1790600421096)

- 12.799.845 bytes; 203.055 linhas: 1 cabeçalho, 3 summaries (na ordem asn,
  ipv4, ipv6) e 203.051 registros, agrupados na mesma ordem de tipo.
  Cabeçalho versão `2.3`, `startdate` 19700101, `enddate` 20260928,
  `UTCoffset` `-0400`. Sem comentários, sem linhas vazias, sem `\r`.
- Em 2026, o total de registros variou entre ~200 mil (janeiro) e ~204,5 mil
  (junho).
- Registros: 32.988 `asn`, 80.850 `ipv4`, 89.213 `ipv6` — batem com o
  cabeçalho e os summaries.
- Status: asn 31.849 assigned / 1 available / 1.138 reserved (nenhum
  allocated); ipv4 76.802 allocated / 4.048 reserved (nenhum assigned nem
  available); ipv6 11.469 allocated / 27.421 available / 50.323 reserved
  (nenhum assigned).
- Toda linha de registro tem **8 campos**: available e reserved terminam com
  o opaque-id vazio. Nenhuma linha tem extensões depois do opaque-id.
- `cc` **vazio** em todos os available/reserved (82.931 registros); nenhum
  `ZZ`; 62 países nos demais (US 105.854, CA 12.630, PR 417...). Data vazia
  nos mesmos available/reserved; **108 ASNs assigned com data `00000000`**
  (ex.: AS3); as demais datas vão de 19821213 a 20260928.
- opaque-id hex minúsculo de 32 caracteres (ex.:
  `e0082a77a634f2cc5817ffb7de12b38e`), 39.138 titulares distintos; o mesmo
  titular aparece em ASNs e blocos (7.707 titulares têm ASN, IPv4 e IPv6).
- IPv4: 78.085 registros formam um CIDR (de `/8` a `/24`); **2.765 não
  formam** — todos `reserved` — e viram 6.652 blocos (de 2 a 6 pedaços cada,
  de `/19` a `/24`): 84.737 blocos IPv4 no total. Desses 2.765, 51 têm
  tamanho potência de 2, mas início desalinhado (ex.: `64.112.29.0` + 512 →
  `/24` + `/24`).
- IPv6 de `/13` a `/48`, nenhum com bits de host: um bloco por registro
  (173.950 blocos no total, com os IPv4).
- Faixas de ASN com mais de um ASN: 143 assigned (até 290 ASNs; ex.:
  AS5120 + 257) e 1 available (AS403010 + 1371, o único registro available
  de ASN).
- Nenhum registro repetido e nenhuma sobreposição de faixas (ASN, IPv4,
  IPv6).
- O parser aceita o arquivo inteiro sem descartes nem avisos (tempos em
  [collector.md](collector.md#medições)).

## Recorte (`testdata/delegated-extended-sample.txt`)

Recorte real do arquivo de 2026-09-28: 25 linhas (1.397 bytes, LF, sem
comentários) — o cabeçalho `2.3|arin|1790600421096|21|19700101|20260928|-0400`
(só `records` ajustado, de 203051 para 21), os três summaries na ordem do
arquivo com as contagens do recorte (asn 7, ipv4 8, ipv6 6) e 21 registros
de 8 campos, agrupados por tipo na mesma ordem e crescentes pelo início. Para
refazê-lo, copie as linhas abaixo do `delegated-arin-extended-20260928` do
histórico ([URLs](#urls)) e ajuste o cabeçalho e os summaries.

| Tipo | Registros | Cobre |
|---|---|---|
| asn (7) | AS1 (US, assigned, 20010920, `e5e3b9c1…`); AS3 (US, assigned, data `00000000`, `d98c567c…`); AS212 (reserved); AS5120 + 257 (US, assigned, 19950509, `45fe880b…`); AS11472 (US, assigned, 20081218, `e0082a77…`); AS54044 (CA, assigned, 20110915, `5faa6218…`); AS403010 + 1371 (available) | só `assigned` nos delegados; data `00000000`; reserved com o opaque-id vazio no fim da linha; faixas de ASN assigned (257) e available (1.371) |
| ipv4 (8) | `6.0.0.0` + 16777216 (US, allocated, 19940201, `45fe880b…`); `23.128.1.0` + 768 (reserved); `23.128.16.0` + 256 (PR, allocated, 20190315, `38bfb095…`); `23.128.81.0` + 256 (reserved); `64.112.29.0` + 512 (reserved); `108.160.96.0` + 4096 (CA, allocated, 20111006, `5faa6218…`); `208.65.32.0` + 1024 (US, allocated, 20140701, `e0082a77…`); `216.7.64.0` + 4096 (US, allocated, 20090114, `e0082a77…`) | o `/8` (o maior da ARIN); `cc` `PR`; dois reserved que não formam CIDR — 768 endereços (`/24` + `/23`) e 512 desalinhados (`/24` + `/24`) —, então 8 registros viram 10 blocos |
| ipv6 (6) | `2001:488::/29` (available); `2605:8480::/32` (US, allocated, 20140611, `e0082a77…`); `2606:2700::/32` (CA, allocated, 20111010, `5faa6218…`); `260a::/15` (reserved); `2620:de:6000::/48` (US, allocated, 20251015, `083b0cd1…`); `2638::/13` (available) | do `/13` ao `/48` (os extremos da ARIN); available e reserved de 8 campos |

Os titulares do recorte, com o opaque-id completo:

| Titular | Recursos no recorte |
|---|---|
| `e0082a77a634f2cc5817ffb7de12b38e` | AS11472, `208.65.32.0/22`, `216.7.64.0/20`, `2605:8480::/32` |
| `5faa6218c796a04196600acabcc8744c` | AS54044, `108.160.96.0/20`, `2606:2700::/32` |
| `45fe880b68a8f2850ebdcfdc57b4556c` | AS5120 + 257, `6.0.0.0/8` |
| `e5e3b9c13678dfc483fb1f819d70883c` | AS1 |
| `d98c567cda2db06e693f2b574eafe848` | AS3 |
| `38bfb095fb64d8412707395afeff79b2` | `23.128.16.0/24` |
| `083b0cd1b1e8e914c5745ade6bd0a7c4` | `2620:de:6000::/48` |

Available e reserved com `cc`, data e opaque-id vazios em ASN, IPv4 e IPv6.

O `testdata/formats/` do `collector-arin` traz os recortes da AFRINIC, APNIC
e RIPE NCC (iguais aos do modelo) e o `lacnic.txt`, tirado do recorte do
modelo com AS26596 + 2 no lugar de AS28003 + 3
([../rir/formato.md](../rir/formato.md#fixtures)). O `formats/arin.txt` dos
outros quatro coletores é outro recorte do mesmo arquivo de 2026-09-28:
AS3, AS212 e `23.128.1.0` + 768 (também neste recorte) e `2001:4:112::/48`
(US, allocated, 20240912, `9064d089391b6dcc310a2188da60c437`). Os números
que os testes citam estão em [collector.md](collector.md#testes).
