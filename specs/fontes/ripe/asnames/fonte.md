# ripe/asnames — fonte (`asn.txt` do RIPE NCC)

De onde vem o dado, como é o arquivo e como o parser (`internal/parse`) o lê.
Tabelas em [dados.md](dados.md); detecção de mudança, aplicação e tempos em
[collector.md](collector.md).

## Arquivo

| Item | Valor |
|---|---|
| URL | `https://ftp.ripe.net/ripe/asnames/asn.txt` (padrão de `SOURCE_URL`) |
| Quem publica | RIPE NCC, a partir dos dados dos cinco RIRs (AFRINIC, APNIC, ARIN, LACNIC e RIPE NCC) |
| Conteúdo | todos os ASNs alocados, de todos os RIRs, cada um com um nome e o país |
| Hash publicado | **não existe**: `asn.txt.sha256`, `asn.txt.md5`, `asn.txt.sha1`, `asn.txt.gz`, `SHA256SUMS` e `CHECKSUMS` dão 404, e a pasta só lista `asn.txt` (medido em 2026-09-28) |
| Atualização | cerca de uma vez por dia (observado em 2026-09-28) |

Não é da família RIR: o formato não é o delegated-extended e o arquivo cobre
os cinco registros. As delegações do próprio RIPE NCC são outra fonte,
[`ripencc`](../../ripencc/README.md).

### Servidor HTTP (medido em 2026-09-28)

nginx. Resposta sem compressão:

```
HTTP/1.1 200 OK
Server: nginx
Content-Type: text/plain
Content-Length: 6210814
Last-Modified: Mon, 28 Sep 2026 10:49:00 GMT
ETag: "6aba461c-5ec4fe"
Access-Control-Allow-Origin: *
Accept-Ranges: bytes
```

- O ETag é o do nginx, `<mtime em hex>-<tamanho em hex>`: `0x6aba461c` =
  2026-09-28 10:49:00 UTC (o mesmo instante do `Last-Modified`) e
  `0x5ec4fe` = 6.210.814 bytes.
- Com `Accept-Encoding: gzip` (o cliente HTTP do Go pede sozinho) a resposta
  vem comprimida — 2.297.980 bytes em vez de 6.210.814 — e o ETag vira fraco:
  `W/"6aba461c-5ec4fe"`.
- `If-None-Match` com o ETag da última resposta (forte ou fraco) e
  `If-Modified-Since` com o `Last-Modified` dela devolvem **304**. Um ETag
  diferente com o mesmo `If-Modified-Since` devolve 200: o ETag manda.

## Formato

Texto UTF-8, uma linha por ASN terminada em LF, sem cabeçalho e sem
comentários, em ordem crescente de ASN:

```
<asn> <descrição>
```

O número vem sem o prefixo `AS` e é separado da descrição por um espaço. A
descrição termina em `, <CC>` e é montada de três jeitos, conforme o RIR de
origem:

| Formato | Molde da descrição | Exemplo real |
|---|---|---|
| ARIN, APNIC, LACNIC | `<handle> - <nome>, <CC>` | `15169 GOOGLE - Google LLC, US` |
| AFRINIC | `<nome> - <nome>, <CC>` (as duas metades iguais) | `29571 Orange Côte d'Ivoire - Orange Côte d'Ivoire, CI` |
| RIPE NCC | `<handle> <nome>, <CC>` (sem separador) | `28 DFVLR-SYS Deutsches Zentrum fuer Luft- und Raumfahrt e.V., DE` |

## Fatos medidos

Arquivo de 2026-09-28 (`Last-Modified: Mon, 28 Sep 2026 10:49:00 GMT`,
SHA-256 `ac06c215f3847712397fd9781b4d68a804197e5d3670658f9abab3d3cecc12e4`),
medido em 2026-09-28 e conferido de novo em 2026-09-29.

- **Tamanho**: 6.210.814 bytes (2.297.980 com gzip), **122.591 linhas**, uma
  por ASN; a última termina em LF.
- **Codificação**: UTF-8 válido em todas as linhas (não é Latin-1); 63 linhas
  com caracteres não-ASCII (`Côte d'Ivoire`, `Moçambique`…). Sem BOM, sem
  `\r`, sem tab, sem NUL, sem linha vazia, sem zero à esquerda no número.
- **Mojibake da própria fonte**, preservado como veio:
  - AS56122 `Avenida GraÃƒÂ§a Aranha`: o `ç` passou duas vezes por uma
    conversão errada (UTF-8 lido como Windows-1252 e regravado em UTF-8):
    `ç` → `Ã§` → `ÃƒÂ§` (bytes `C3 83 C6 92 C3 82 C2 A7`).
  - AS59265 `China telecom â<U+0080><U+0093> China Next Generation Internet`:
    um travessão `–` (U+2013, bytes `E2 80 93`) lido como Latin-1, que virou
    `â` e os caracteres de controle C1 U+0080 e U+0093 (bytes
    `C3 A2 C2 80 C2 93`).
- **ASNs**: ordem estritamente crescente, **nenhum repetido**; de 1 a
  403.009; 65.992 acima de 65.535 (ASN de 32 bits).
- **País**: **todas** as linhas terminam em `, CC` com duas letras
  maiúsculas; 242 códigos distintos, incluindo `EU` (383 linhas) e `AP` (2),
  códigos regionais dos RIRs que não são países ISO 3166. Os maiores: `US`
  32.219 (26% das linhas), `BR` 9.162, `CN` 6.623.
- **Separador ` - `**: 83.566 linhas têm, 39.025 não têm (formato RIPE NCC).
- 3.199 linhas têm espaço no trecho antes do primeiro ` - `: 2.643 são do
  formato AFRINIC `X - X` (X com espaço); 1 tem espaço duplo antes do
  separador (`401635 WALSWORTH  - EAU CLAIRE - WALSWORTH PUBLISHING COMPANY, US`);
  e 555 são do formato RIPE NCC com ` - ` dentro do nome da organização
  (`13040 ASN-FIZ FIZ Karlsruhe - Leibniz-Institut…`) **ou** de handles ARIN
  com espaço (`511 PRISMA HEALTH - Prisma Health, US`; 144 dessas 555 são
  `US`/`CA`). Por isso "cortar no primeiro ` - `" não serve como regra geral.
- 3.120 linhas são exatamente `X - X`; em 84 delas o próprio X tem ` - `
  (`30619 TMCEL - Moçambique Telecom, SA - TMCEL - Moçambique Telecom, SA, MZ`).
- **Campos vazios**: 49 linhas terminam em ` - , CC` (nome vazio) — 44 com
  handle (`4745 AS4745-138 - , KR`) e 5 sem handle nem nome (`7901 - , NZ`,
  e AS11157, AS22354, AS36997, AS37110); 5 linhas trazem só o handle
  (`2799 Polismyndigheten, SE`).
- 140 linhas têm espaços duplos dentro do texto (preservados).
- Linha mais longa: 223 caracteres (AS57438); média 49,7. Maior descrição:
  217 caracteres; maior handle derivado: 82.
- **O handle não é único**: 113.452 handles distintos (112.981 sem
  diferenciar maiúsculas), 4.614 se repetem; `VRSN-AC50-340` aparece em 290
  ASNs, `GOOGLE` em 6.

## Modelo

Cada linha vira um ASN com o texto original e três campos derivados:
`description` guarda **tudo** o que vem depois do número, como publicado — é
a fonte da verdade —, e `handle`, `name` e `country` são a melhor leitura
possível dela, porque o formato varia por RIR e há casos ambíguos (abaixo).
A busca por texto da API usa `description`, então nenhum ASN deixa de ser
encontrado por causa de uma derivação imperfeita. Colunas:
[dados.md](dados.md#mapeamento-da-fonte-para-as-colunas).

## Linhas

`parse.Parse` lê o arquivo linha a linha (`bufio.Scanner`, linha de até
1 MiB):

1. Tira o BOM UTF-8 da primeira linha; troca UTF-8 inválido e NUL por
   U+FFFD (o Postgres recusaria o `COPY`); tira os espaços das pontas (o que
   tolera `\r` de fim de linha). Não há decodificação Latin-1: a medição
   mostrou UTF-8.
2. Linha vazia ou começada por `#` é ignorada e não conta no total. Um `#` no
   meio da linha é texto (`4640 ASN-TIC-HK  # AS-TIC-HK converted…`).
3. O número vai até o primeiro espaço em branco (`unicode.IsSpace`: espaço,
   tab…) e tem de ser decimal de 0 a 4294967295, sem `AS` nem sinal
   (`strconv.ParseUint(..., 10, 32)`).
   A descrição é o resto, sem os espaços das pontas; os de dentro ficam.
4. Linhas **descartadas**, cada uma com um aviso:

   | Caso | Aviso |
   |---|---|
   | número inválido | `linha <n>: linha descartada: ASN inválido "<texto>"` (o texto é cortado em 40 bytes, com `…`) |
   | sem descrição | `linha <n>: linha descartada: AS<asn> sem descrição` |
   | ASN repetido (vale a primeira ocorrência) | `linha <n>: linha descartada: AS<asn> repetido; vale a primeira ocorrência` |

5. O arquivo inteiro é **recusado** (o coletor grava `parser: <mensagem>`):

   | Caso | Mensagem |
   |---|---|
   | erro de leitura (ex.: linha acima de 1 MiB) | `leitura: <erro>` |
   | nenhuma linha de dados | `arquivo sem nenhuma linha de dados` |
   | mais de 1% (`MaxSkippedRatio`) das linhas com conteúdo descartadas — formato novo ou arquivo corrompido não pode virar remoção em massa | `<d> de <n> linhas descartadas (<x.y>%, limite 1.0%): formato mudou? primeiros avisos: <até 3 avisos, separados por "; ">` |

6. Avisos: os 50 primeiros (`MaxWarnings`) ficam no dataset e o total é
   contado; o coletor acrescenta `... e mais <n> avisos` quando passam disso
   ([collector.md](collector.md#recusas-erros-e-logs)).

No arquivo real: 0 descartes, 0 avisos.

## Campos derivados

`parse.Derive(description)` devolve `handle`, `name` e `country`; vazio vira
`NULL` no banco. Espaço em branco é `unicode.IsSpace`. As regras, em ordem:

1. **`country`**: se a descrição tem 4 bytes ou mais e termina em `, XX` com
   `XX` em `A`–`Z` (ASCII), `XX` é o país e o **corpo** é o que vem antes.
   Senão `country` é vazio e o corpo é a descrição inteira (`, br` e `, BRA`
   não contam como país).
2. **AFRINIC `X - X`**: o corpo sem os espaços das pontas é exatamente
   `X + " - " + X`, byte a byte (comprimento de 5 ou mais, `comprimento - 3`
   par) → `handle` = `name` = X. Pega também X com ` - ` dentro.
   *(3.120 linhas)*
3. **ARIN/APNIC/LACNIC `handle - nome`**: no corpo cercado por um espaço de
   cada lado (`" " + corpo + " "`), acha o primeiro ` - `; se o trecho antes
   dele, sem os espaços das pontas, não tem espaço, `handle` = esse trecho e
   `name` = o resto sem os espaços das pontas (vírgulas e outros ` - ` ficam
   no nome). Os espaços acrescentados fazem `- , NZ` (handle vazio) e
   `AS4745-138 - , KR` (nome vazio) caírem aqui. *(79.896 linhas)*
4. **RIPE NCC** (qualquer outro caso): `handle` = a primeira palavra do corpo,
   `name` = o resto sem os espaços das pontas (vazio se não houver).
   *(39.575 linhas: 39.015 sem separador, 5 só com o handle e as 555
   ambíguas)*

Contagens por regra no arquivo de 2026-09-28. Exemplos reais e casos de teste
(`TestDerive`, `TestParseSample`):

| Descrição | Regra | `handle` | `name` | `country` |
|---|---|---|---|---|
| `GOOGLE - Google LLC, US` | 3 | `GOOGLE` | `Google LLC` | `US` |
| `LVLT-1 - Level 3 Parent, LLC, US` | 3 | `LVLT-1` | `Level 3 Parent, LLC` | `US` |
| `AS28000 - LACNIC - Latin American and Caribbean IP address, UY` | 3 | `AS28000` | `LACNIC - Latin American and Caribbean IP address` | `UY` |
| `WALSWORTH  - EAU CLAIRE - WALSWORTH PUBLISHING COMPANY, US` | 3 | `WALSWORTH` | `EAU CLAIRE - WALSWORTH PUBLISHING COMPANY` | `US` |
| `AS4745-138 - , KR` | 3 | `AS4745-138` | NULL | `KR` |
| `- , NZ` | 3 | NULL | NULL | `NZ` |
| `FOO - Bar, br` | 3 | `FOO` | `Bar, br` | NULL |
| `Orange Côte d'Ivoire - Orange Côte d'Ivoire, CI` | 2 | `Orange Côte d'Ivoire` | `Orange Côte d'Ivoire` | `CI` |
| `TMCEL - Moçambique Telecom, SA - TMCEL - Moçambique Telecom, SA, MZ` | 2 | `TMCEL - Moçambique Telecom, SA` | `TMCEL - Moçambique Telecom, SA` | `MZ` |
| `DFVLR-SYS Deutsches Zentrum fuer Luft- und Raumfahrt e.V., DE` | 4 | `DFVLR-SYS` | `Deutsches Zentrum fuer Luft- und Raumfahrt e.V.` | `DE` |
| `ASN-FIZ FIZ Karlsruhe - Leibniz-Institut fuer Informationsinfrastruktur GmbH, DE` | 4 | `ASN-FIZ` | `FIZ Karlsruhe - Leibniz-Institut fuer Informationsinfrastruktur GmbH` | `DE` |
| `ASN-HRTNET  HRT - Croatian Radio Television, HR` | 4 | `ASN-HRTNET` | `HRT - Croatian Radio Television` | `HR` |
| `X -Y - Z, KR` | 4 | `X` | `-Y - Z` | `KR` |
| `PRISMA HEALTH - Prisma Health, US` | 4 | `PRISMA` | `HEALTH - Prisma Health` | `US` |
| `Polismyndigheten, SE` | 4 | `Polismyndigheten` | NULL | `SE` |
| `, US` | 4 | NULL | NULL | `US` |

**Limitação conhecida**: nas 555 linhas ambíguas da regra 4, quando o handle é
do ARIN e tem espaço (`511 PRISMA HEALTH - Prisma Health, US`), o resultado é
`handle = PRISMA`, `name = HEALTH - Prisma Health`. A regra oposta estragaria
as linhas do RIPE NCC com ` - ` no nome
(`2047 ASN-ROCHE-BASLE Hoffmann - La Roche Ltd., CH`), que são a maioria. A
busca por texto usa `description`, então nada deixa de ser encontrado.

**Resultado no arquivo real**: 122.591 ASNs, 0 descartes, 0 sem país, 5 sem
handle, 54 sem nome.

### Mudar uma regra

1. Esta spec primeiro, com as contagens medidas no arquivo do dia.
2. `TestDerive` e `TestParseSample` com linhas reais (e a fixture, se faltar
   um caso).
3. Em produção, `--force` recalcula os campos derivados: o `MERGE` compara
   `handle`, `name` e `country` e só atualiza as linhas que mudaram
   ([collector.md](collector.md#aplicação)).
4. Avise a `api-ripe-asnames`: a spec dela descreve esses campos, e o teste com o
   arquivo real dela (`apps/ripe/asnames/api/internal/httpapi/real_integration_test.go`,
   função `derive`) repete estas regras para carregar o arquivo sem o coletor.

## Fixture

`apps/ripe/asnames/collector/testdata/asn-sample.txt`: **37 linhas reais**,
copiadas sem alteração do arquivo de 2026-09-28 (conferido em 2026-09-29:
cada linha existe nele, byte a byte), na ordem do arquivo; 1.964 bytes,
UTF-8, LF, com a última linha terminada. Não é um trecho contínuo: cada
linha foi escolhida por um caso. Conteúdo (na fixture, `<U+0080><U+0093>` são
os dois caracteres de controle C1, bytes `C2 80 C2 93`):

```
1 LVLT-1 - Level 3 Parent, LLC, US
2 UDEL-DCN - University of Delaware, US
28 DFVLR-SYS Deutsches Zentrum fuer Luft- und Raumfahrt e.V., DE
248 IDDQD-AS - IDDQD-AS, EU
378 MACHBA-AS IUCC - Israel InterUniversity Computation Center, IL
511 PRISMA HEALTH - Prisma Health, US
513 CERN CERN - European Organization for Nuclear Research, CH
1297 CERN1297 CERN - European Organization for Nuclear Research, CH
2047 ASN-ROCHE-BASLE Hoffmann - La Roche Ltd., CH
2712 HALLIBURTON COMPANY - Halliburton Company, US
2799 Polismyndigheten, SE
3256 Renater, FR
4640 ASN-TIC-HK  # AS-TIC-HK converted to ASN-TIC-HK for RPSL compliance - The Internetworking Corporation, HK
4745 AS4745-138 - , KR
5536 Xyberdata - Xyberdata, MU
6794 ASN-HRTNET  HRT - Croatian Radio Television, HR
7901 - , NZ
9487 KTFC1120-AS-KR -KR - Korea Technology Finance Corporation, KR
13040 ASN-FIZ FIZ Karlsruhe - Leibniz-Institut fuer Informationsinfrastruktur GmbH, DE
15169 GOOGLE - Google LLC, US
16509 AMAZON-02 - Amazon.com, Inc., US
28000 AS28000 - LACNIC - Latin American and Caribbean IP address, UY
29571 Orange Côte d'Ivoire - Orange Côte d'Ivoire, CI
30619 TMCEL - Moçambique Telecom, SA - TMCEL - Moçambique Telecom, SA, MZ
33764 African Network Information Center - (AfriNIC) Ltd - African Network Information Center - (AfriNIC) Ltd, MU
37622 Mahanagar Telephone (Mauritius) Ltd - Mahanagar Telephone (Mauritius) Ltd, MU
56122 VALE-SA-AP - Avenida GraÃƒÂ§a Aranha, 26 Castelo, SG
59265 CT-CNGI - China telecom â<U+0080><U+0093> China Next Generation Internet, CN
61613 AS61613 - TMSoft Solucoes em Informatica Ltda, BR
136209 CYBERFORESTLLC-AS-AP - CyberForest LLC., AP
139247 PPPWAW-AS-AP - pppwaw, AP
262287 AS262287 - Latitude.sh LTDA, BR
263009 AS263009 - FORTE TELECOM LTDA., BR
327710 Orange Côte d'Ivoire - Orange Côte d'Ivoire, CI
328289 CEE DEE INVESTMENT Company Limited - CEE DEE INVESTMENT Company Limited, SL
399999 LYON-BV-01 - LYON COLLEGE, US
403009 LOREM-IPSUM - Lorem, US
```

| Caso | ASNs |
|---|---|
| Regra 3 (`handle - nome`), inclusive vírgulas e ` - ` no nome | 1, 2, 15169, 16509, 28000, 61613, 262287, 263009, 399999, 403009 |
| Regra 3 com nome vazio / com handle e nome vazios | 4745 / 7901 |
| Regra 2 (`X - X`), inclusive X com espaços e com ` - ` dentro | 248, 5536, 29571, 30619, 33764, 37622, 327710, 328289 |
| Regra 4 sem separador / só o handle | 28 / 2799, 3256 |
| Regra 4 ambígua: ` - ` no nome RIPE (espaço duplo em 6794, `#` no meio em 4640, `-KR` em 9487) | 378, 513, 1297, 2047, 4640, 6794, 9487, 13040 |
| Regra 4 ambígua: handle ARIN com espaço (limitação) | 511, 2712 |
| Códigos regionais `EU` / `AP` | 248 / 136209, 139247 |
| Não-ASCII | 29571 e 327710 (a mesma descrição em dois ASNs), 30619 |
| Mojibake (conversão dupla / controles C1) | 56122 / 59265 |
| ASN de 32 bits, até o maior do arquivo | 136209, 139247, 262287, 263009, 327710, 328289, 399999, 403009 |
| País `BR` (consulta por país nos testes) | 61613, 262287, 263009 |

Os testes contam com estas 37 linhas ([collector.md](collector.md#testes)).
Para refazer a fixture a partir de um arquivo novo, pegue as mesmas linhas
pelo ASN; se alguma sumiu ou mudou, troque por outra linha real do mesmo caso
e ajuste os valores esperados nos testes e nesta lista. Mudou o formato da
fonte: meça o arquivo real, atualize a fixture, os testes do parser e esta
spec juntos.
