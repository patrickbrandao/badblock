# Formato delegated-extended (modelo dos RIRs)

Os cinco RIRs publicam, uma vez por dia, o arquivo **delegated-extended**
(formato "RIR Statistics Exchange", versão estendida) com as delegações de
ASNs e blocos IPv4/IPv6. A descrição oficial é publicada pelos RIRs (ex.:
`https://ftp.ripe.net/pub/stats/ripencc/RIR-Statistics-Exchange-Format.txt`;
o bloco de comentários do arquivo da APNIC cita
`http://www.apnic.net/db/rir-stats-format.html` e
`ftp://ftp.apnic.net/pub/apnic/stats/apnic/README-EXTENDED.TXT`). URLs,
horários e fatos de cada RIR ficam no
`fonte.md` dele; este arquivo descreve o que vale para os cinco e o parser
genérico (`internal/parse`), que não tem nenhuma regra de um RIR só: o
`registry` esperado vem de `internal/rir`.

## Linhas

Texto ASCII, uma linha por item, campos separados por `|`.

| Linha | Forma | Observação |
|---|---|---|
| Comentário | começa com `#` | só a APNIC usa (27 linhas antes do cabeçalho em 2026-09-28) |
| Cabeçalho | `version\|registry\|serial\|records\|startdate\|enddate\|UTCoffset` | a primeira linha que não é comentário nem vazia |
| Resumo (summary) | `registry\|*\|type\|*\|count\|summary` | uma por tipo, logo depois do cabeçalho; a ordem dos tipos varia por RIR |
| Registro | `registry\|cc\|type\|start\|value\|date\|status[\|opaque-id[\|extensões]]` | uma por faixa de ASNs ou bloco |

Exemplos reais estão no `fonte.md` de cada RIR (ex.:
[LACNIC](../lacnic/fonte.md#formato)).

### Cabeçalho

| Campo | Conteúdo |
|---|---|
| `version` | versão do formato: `2` ou `2.3` (a descrição fala em `2.3`, mas AFRINIC e RIPE NCC publicam `2`) |
| `registry` | nome do RIR, em minúsculas (`afrinic`, `apnic`, `arin`, `lacnic`, `ripencc`) |
| `serial` | série do arquivo, no formato do RIR: data `AAAAMMDD`, época Unix em segundos (RIPE NCC) ou em milissegundos (ARIN) |
| `records` | quantidade de registros, sem cabeçalho, resumos e comentários |
| `startdate`, `enddate` | período coberto, `AAAAMMDD`; vazio ou `00000000` = sem data |
| `UTCoffset` | fuso das datas, como publicado (`-0300`, `+1000`, `00000`...) |

### Registro

| Campo | Conteúdo |
|---|---|
| `registry` | o mesmo do cabeçalho |
| `cc` | país ISO 3166-1 alfa-2; sem país = vazio ou `ZZ`, conforme o RIR; o RIPE NCC usa também `EU` (Europa, sem país específico) |
| `type` | `asn`, `ipv4` ou `ipv6` |
| `start` | primeiro ASN, ou endereço inicial |
| `value` | `asn`: quantidade de ASNs (uma faixa); `ipv4`: quantidade de endereços (**nem sempre forma um CIDR**); `ipv6`: tamanho do prefixo |
| `date` | `AAAAMMDD` da alocação ou designação; vazia ou `00000000` quando não há |
| `status` | `allocated` (alocado a um LIR/ISP), `assigned` (designado a um usuário final), `available` (livre no estoque do RIR), `reserved` (reservado pelo RIR) — os únicos vistos nos cinco arquivos |
| `opaque-id` | identificador do titular dentro do RIR: liga os ASNs e blocos de uma mesma organização; não é documento nem nome. Vazio em available/reserved, e alguns RIRs omitem o campo nessas linhas (7 campos em vez de 8) |
| extensões | campos depois do opaque-id, sem definição; ignorados |

Cada `summary` conta os registros do seu tipo. Pela descrição do formato, os
registros **não têm ordem garantida** (o parser não depende dela) e o
opaque-id **não tem garantia de ser constante entre versões do arquivo** (no
RIPE NCC ele muda todo dia).

## Arquivo `.md5`

Os cinco RIRs publicam, ao lado do arquivo, `<arquivo>.md5` com o MD5 do
conteúdo. É o **hash publicado** do [padrão](../../padroes/coletor.md#uma-verificação):
serve para a checagem mais barata de mudança e para conferir o download.

| Formato | Exemplo | Quem usa (2026-09-28) |
|---|---|---|
| BSD | `MD5 (delegated-lacnic-extended-latest) = 1d11010d5e9cc31ce507817ac4fae1bf` | AFRINIC, APNIC, LACNIC, RIPE NCC |
| GNU | `0bb8d65f1ff9d820f4bb56844f9948be  delegated-arin-extended-20260928` | ARIN |

Leitura (`fetch.ParseMD5`):

- só a **primeira linha não vazia** conta (espaços em volta removidos): uma
  página de erro HTML não vira hash;
- BSD: `^MD5 ?\(.*\) ?= ?([0-9a-fA-F]{32})$`; GNU:
  `^([0-9a-fA-F]{32})(?:[ \t]+\*?.*)?$` (o hash sozinho, ou seguido de
  espaço, `*` opcional e o nome). O nome do arquivo não é conferido (a ARIN
  cita o nome datado; a AFRINIC cita `-latest` até nos `.md5` do histórico);
- o hash sai em hex minúsculo; maiúsculas são aceitas;
- linha que não casa: erro `sem hash MD5 reconhecível: "<linha, até 80 caracteres>"`;
  só linhas vazias: `arquivo .md5 vazio`. Conteúdo sem hash não se repete;
  erro de rede, erro na leitura do corpo (inclusive passar do limite) e
  HTTP 5xx se repetem, como no
  [download](../../padroes/coletor.md#download-internalfetch);
- o `.md5` é baixado sem validadores e com no máximo **4 KiB**: maior que
  isso é o erro `resposta maior que 4096 bytes` (repetido, como os outros
  erros de leitura), e a verificação segue sem conferência.

O `.md5` pode sair depois do arquivo (na APNIC, ~9 min) ou vir de outra cópia
(a ARIN tem vários servidores): nessa janela a conferência pode falhar, e a
próxima verificação resolve ([collector.md](collector.md#validações-do-arquivo-novo)).
Com o `.md5` atrasado, um coletor em dia não grava falha — o `.md5` ainda é o
do último aplicado e a checagem 1 encerra a verificação —; a recusa só
acontece quando o último aplicado não é o arquivo anterior (primeira carga,
coletor parado, arquivo anterior recusado) ou com `--force`
([../apnic/collector.md](../apnic/collector.md#janela-do-md5)).

## Regras do parser (`internal/parse`)

`Parse(r, registry)` lê o arquivo inteiro e devolve um `Dataset` ou um erro
(arquivo recusado). O coletor prefixa o erro com `parser: `.

### Leitura das linhas

- Linhas de até **1 MiB** (`bufio.Scanner`); uma linha maior é erro de
  leitura (`leitura: ...`) e recusa o arquivo.
- BOM (`EF BB BF`) no início da primeira linha é removido; bytes que não são
  UTF-8 válido viram `�`.
- Cada linha perde os espaços das pontas (inclusive o `\r` de CRLF). Linhas
  vazias e começadas por `#` são ignoradas em qualquer lugar do arquivo.
- Campos separados por `|`, cada um sem os espaços das pontas.
- A primeira linha que sobra é o cabeçalho. Depois dele, uma linha com 6 ou
  mais campos e o 6º exatamente `summary` é um resumo; qualquer outra é uma
  **linha de registro** (conta em `Lines`, mesmo que seja descartada).

### Recusa do arquivo inteiro

Na ordem em que o parser confere; `N` é o número da linha no arquivo.

| Condição | Mensagem |
|---|---|
| 1º campo do cabeçalho não casa `^[0-9]+(\.[0-9]+)*$` (falta o cabeçalho) | `linha N: cabeçalho ausente ou ilegível (esperado version\|registry\|serial\|records\|startdate\|enddate\|UTCoffset): "<linha>"` |
| versão com major diferente de `2` | `linha N: versão de formato "3.0" não suportada (esperada 2.x)` |
| cabeçalho com menos de 6 campos (o 7º, `UTCoffset`, é opcional) | `linha N: cabeçalho com 3 campos, esperados 7: "<linha>"` |
| `serial` vazio | `linha N: cabeçalho sem serial` |
| `records` não inteiro ou negativo | `linha N: cabeçalho com contagem de registros inválida: "<valor>"` |
| `registry` do cabeçalho (em minúsculas) diferente do esperado | `linha N: cabeçalho do registry "arin", esperado "ripencc"` |
| `count` de um resumo não inteiro ou negativo | `linha N: summary ilegível: "<linha>"` |
| erro de leitura | `leitura: <erro>` |
| nenhuma linha além de comentários e vazias | `arquivo sem cabeçalho nem registros` |
| `records` ≠ linhas de registro (inclusive as descartadas) | `o cabeçalho declara 4 registros e o arquivo tem 3: arquivo truncado ou corrompido?` |
| `count` de um resumo ≠ linhas de registro daquele tipo (3º campo, em minúsculas; inclusive as descartadas) | `a linha summary declara 2 registros asn e o arquivo tem 3: arquivo truncado ou corrompido?` |
| nenhuma linha de registro | `arquivo sem nenhum registro` |
| descartados / linhas de registro > **1%** (`MaxSkippedRatio`) | `2 de 100 registros descartados (2.0%, limite 1.0%): formato mudou? primeiros avisos: <até 3 avisos>` |

O limite de 1% existe porque um formato novo ou um arquivo corrompido não
pode virar uma remoção em massa. Registro repetido não conta como descartado.

### Descarte de um registro

O registro sai do dataset, conta em `Skipped` e gera o aviso
`linha N: registro descartado: <motivo>`. Conferido nesta ordem:

| Condição | Motivo |
|---|---|
| menos de 7 campos | `esperados ao menos 7 campos, vieram 5` |
| `registry` (em minúsculas) diferente do esperado | `registry "x", esperado "lacnic"` |
| `cc` (em maiúsculas) não vazio e diferente de duas letras `A`–`Z` | `país inválido "B1"` |
| `status` (em minúsculas) fora dos quatro | `status desconhecido "transferred"` |
| `type` (em minúsculas) fora de `asn`, `ipv4`, `ipv6` | `tipo desconhecido "foo"` |
| `asn`: `start` não é inteiro de 0 a 4294967295 | `ASN inicial inválido "x"` |
| `asn`: `value` não inteiro, menor que 1 ou faixa que passa de 4294967295 | `quantidade de ASNs inválida "2" a partir de 4294967295` |
| `ipv4`: `start` não é IPv4 | `endereço IPv4 inválido "<start>"` |
| `ipv4`: `value` não inteiro | `quantidade de endereços inválida "x"` |
| `ipv4`: `value` 0 ou faixa que passa de 255.255.255.255 | `quantidade de endereços inválida 512 a partir de 255.255.255.0` |
| `ipv6`: `start` não é IPv6 (IPv4 mapeado e zona também não valem) | `endereço IPv6 inválido "<start>"` |
| `ipv6`: `value` fora de 1–128 | `tamanho de prefixo IPv6 inválido "129"` |

### Normalização

- `cc` em maiúsculas; vazio fica vazio (NULL no banco); `ZZ` e `EU` ficam como
  vieram. `status`, `type` e `registry` comparados em minúsculas.
- `date`: vazia ou `00000000` = sem data; `AAAAMMDD` válida (8 dígitos, data
  existente) = a data; qualquer outra = sem data, com o aviso
  `linha N: data inválida "20201340"; gravada como vazia` (o registro fica).
  Datas válidas são gravadas como publicadas, inclusive `19700101`.
- `startdate`/`enddate` do cabeçalho inválidas viram sem data, com o aviso
  `linha N: startdate inválida no cabeçalho: "<valor>"` (idem `enddate`).
- opaque-id: o 8º campo como veio (maiúsculas preservadas); ausente ou vazio =
  sem titular (NULL). Extensões são ignoradas.

### ASN

Um registro vira uma faixa `Start`..`End()` (`End = Start + Count − 1`). O
mesmo `start` repetido fica com a **primeira** ocorrência, com o aviso
`linha N: registro do AS64512 repetido; vale a primeira ocorrência`.

Depois da leitura, faixas que se sobrepõem geram o aviso
`faixa AS64512-AS64512 sobrepõe AS64000-AS64599`, sem recusar: a API acha a
faixa de um ASN pelo maior início ≤ ASN, o que supõe faixas disjuntas (nunca
vistas sobrepostas nos cinco RIRs, 2026-09-28).

### IPv4: divisão em CIDRs

`value` endereços a partir de `start` viram os **CIDRs mínimos que cobrem a
faixa exatamente**, em ordem (`SplitIPv4`): a partir do endereço atual, o
maior bloco alinhado nele (o bit menos significativo ligado; `/0` no
endereço 0) que caiba no que falta; avança e repete.

| Registro | Blocos |
|---|---|
Os casos do `TestSplitIPv4` do modelo (iguais nos cinco, salvo a última
linha):

| Registro | Blocos |
|---|---|
| `10.0.0.0` + 256 | `10.0.0.0/24` |
| `10.0.0.0` + 1 | `10.0.0.0/32` |
| `62.122.208.0` + 1280 | `62.122.208.0/22`, `62.122.212.0/24` |
| `23.128.5.0` + 1792 | `23.128.5.0/24`, `23.128.6.0/23`, `23.128.8.0/22` |
| `87.116.83.0` + 2304 | `87.116.83.0/24`, `87.116.84.0/22`, `87.116.88.0/22` |
| `164.146.0.0` + 393216 | `164.146.0.0/15`, `164.148.0.0/14` |
| `10.0.0.1` + 6 | `10.0.0.1/32`, `10.0.0.2/31`, `10.0.0.4/31`, `10.0.0.6/32` |
| `192.0.2.48` + 48 | `192.0.2.48/28`, `192.0.2.64/27` |
| `0.0.0.0` + 2³² | `0.0.0.0/0` |
| `128.0.0.0` + 2³¹ | `128.0.0.0/1` |
| `255.255.255.255` + 1 | `255.255.255.255/32` |
| `1.0.0.0` + 3 × 2²⁴ | `1.0.0.0/8`, `2.0.0.0/7` |
| `0.0.0.1` + (2³² − 1) | 32 blocos, um de cada tamanho: `0.0.0.1/32`, `0.0.0.2/31`, `0.0.0.4/30`, … dobrando até `64.0.0.0/2` e `128.0.0.0/1` |
| `64.112.29.0` + 512 | `64.112.29.0/24`, `64.112.30.0/24` — potência de 2 com início desalinhado (51 casos na ARIN); **só no teste do `collector-arin`** |

Cada pedaço guarda o registro de origem (`RecordStart` = `start`,
`RecordValue` = `value`) e repete país, data, status e titular. Um pedaço
igual a um bloco já lido é descartado com o aviso
`linha N: bloco 192.0.2.0/24 repetido; vale a primeira ocorrência`; o
registro conta em `IPv4Records` se ao menos um pedaço ficou.

### IPv6

O bloco é `start/value`. Com bits de host, vira a forma canônica, com o aviso
`linha N: bloco 2001:db8::1/32 com bits de host; usando 2001:db8::/32`
(`RecordStart` guarda o `start` original). Bloco repetido: vale o primeiro,
com aviso. Blocos IP aninhados (um dentro do outro) não geram aviso — só o
bloco idêntico é repetido; a API devolve o mais específico.

### Resumos

- Resumo repetido: vale o primeiro, com o aviso
  `linha N: summary de asn repetido; vale o primeiro`.
- Tipo com registros e sem resumo: aviso
  `sem linha summary para asn (11 registros)`.

### Avisos e saída

- Até **50** avisos (`MaxWarnings`) são guardados, em ordem; o total fica em
  `WarningCount()`. O coletor acrescenta `... e mais N avisos` quando passa
  de 50, e a lista vai para `<rir>_run.warnings`
  ([padrão](../../padroes/coletor.md#recusas-e-falhas); logs em
  [collector.md](collector.md#logs)).
- `Dataset`: `Header` (`Version`, `Registry`, `Serial`, `Records`,
  `StartDate`, `EndDate`, `UTCOffset`); `ASNs` (`Start`, `Count`, `CC`,
  `Date`, `Status`, `OpaqueID`); `Prefixes` (`Prefix`, `CC`, `Date`,
  `Status`, `OpaqueID`, `RecordStart`, `RecordValue`); `ASNRecords`,
  `IPv4Records`, `IPv6Records` (registros aceitos por tipo, sem repetidos);
  `PrefixesV4`, `PrefixesV6` (blocos depois da divisão); `Lines`, `Skipped`,
  `Warnings`. `Records()` = `ASNRecords + IPv4Records + IPv6Records` (é o que
  `MIN_RECORDS` confere).

## Fixtures

Recortes **reais** dos arquivos, em `testdata/` de cada coletor. Mudou o
formato de um RIR: mudam o recorte dele, o `formats/<rir>.txt` nos outros
quatro coletores, os testes do parser e esta spec. O conteúdo literal dos
`formats/*.txt` está em [fixtures.md](fixtures.md).

- **`testdata/delegated-extended-sample.txt`**: recorte do arquivo do próprio
  RIR. Cabeçalho e resumos ajustados ao recorte (`records` e cada `count`); o
  resto do cabeçalho (`version`, `serial`, datas, `UTCoffset`), a ordem dos
  resumos e as linhas ficam como no arquivo real. Cobre um titular com ASN e
  blocos, available/reserved com campos vazios e as particularidades do RIR
  (faixas de ASN, IPv4 que não forma CIDR, comentários, `ZZ`...). O
  `fonte.md` do RIR diz de que dia é, quantos e quais registros tem e o que
  cobre. Os testes que citam os números dele estão em
  [collector.md](collector.md#testes).
- **`testdata/formats/<rir>.txt`**: recortes mínimos dos **outros quatro**
  RIRs, para o mesmo parser continuar coberto nos cinco formatos
  (`TestParseOtherRIRFormats`; `TestParseWrongRegistry` usa um deles). O
  modelo tem `afrinic`, `apnic`, `arin` e `ripencc`; cada clone tira o do
  próprio RIR e põe `lacnic.txt`. Os quatro primeiros são iguais em todos os
  coletores (conferido em 2026-09-29):

| Arquivo | Cabeçalho | Registros (arquivo de 2026-09-28) | Cobre |
|---|---|---|---|
| `afrinic.txt` (10 linhas) | `2\|afrinic\|20260928\|6\|00000000\|20260928\|00000`; resumos asn 2, ipv4 3, ipv6 1 | AS1228 (ZA, `F36B9F4B`); AS8770 (`ZZ`, available, 8 campos); `164.146.0.0` + 393216 (→ `/15` + `/14`); `196.4.20.0` + 2560 (→ `/22` + `/22` + `/23`); `41.57.112.0` + 2048 (`ZZ`, reserved); `2001:4200::/32` (mesmo titular do AS1228) | versão `2`, `startdate` `00000000`, `UTCoffset` `00000`, `ZZ`, opaque-id hex de 8, IPv4 não-CIDR |
| `apnic.txt` (36 linhas) | 27 linhas de comentário (duas com tabulação) e `2.3\|apnic\|20260929\|5\|\|20260928\|+1000`; resumos asn 1, ipv4 3, ipv6 1 | `1.0.0.0` + 256 (AU, assigned); `14.102.240.0` + 4096 (available); `163.61.160.0` + 64 (available, `/26`); `2001:7fa::/64` (reserved); AS1768 + 2 (TW) | comentários, `startdate` vazia, `/26`, `/64`, faixa de ASN |
| `arin.txt` (8 linhas) | `2.3\|arin\|1790600421096\|4\|19700101\|20260928\|-0400`; resumos asn 2, ipv4 1, ipv6 1 | AS3 (US, data `00000000`, assigned, opaque-id hex de 32); AS212 (reserved); `23.128.1.0` + 768 (reserved, → `/24` + `/23`); `2001:4:112::/48` (US) | serial em ms, `startdate` 19700101, data `00000000`, IPv4 não-CIDR |
| `ripencc.txt` (9 linhas) | `2\|ripencc\|1790632799\|5\|19700101\|20260928\|+0200`; resumos asn 1, ipv4 2, ipv6 2 (o arquivo real usa ipv4, asn, ipv6) | `62.122.208.0` + 1280 (RU, → `/22` + `/24`); `156.67.6.0` + 8 (DE, `/29`); AS1877 (available, 7 campos); `2001:600::/29` (NL, opaque-id UUID); `2001:609::/32` (reserved, 7 campos) | versão `2`, serial em segundos, UUID, linhas de 7 campos, `/29` |
| `lacnic.txt` (só nos clones) | `2.3\|lacnic\|20260927\|6\|19870101\|20260925\|-0300`; resumos ipv4 2, ipv6 2, asn 2 (a ordem da LACNIC) | tirados do recorte do modelo: `45.68.105.0` + 256 (reserved, opaque-id vazio no fim); `187.87.28.0` + 1024 (BR, `258500`); `2001:1201:20::/43` (available, 7 campos); `2804:8ae0::/32` (BR, `258500`); AS28003 + 3 (available); AS61610 (BR, `258500`) | `cc` vazio, available de 7 e reserved de 8 campos, opaque-id numérico, faixa de ASN |

Variações do `lacnic.txt` hoje: o `collector-afrinic` tem também AS6065
(reserved; 7 registros) e o `collector-arin` usa AS26596 + 2 no lugar de
AS28003 + 3.

## Comparação entre os RIRs

Medido nos arquivos de 2026-09-28 (detalhes no `fonte.md` de cada RIR). O
parser aceitou os cinco arquivos inteiros sem descartes nem avisos, e os
cinco servidores mandam `ETag` e `Last-Modified` e respondem `304` a
`If-None-Match` e a `If-Modified-Since` (na ARIN, só quando a verificação cai
no servidor que tem a mesma cópia: `ftp.arin.net` tem vários, com mtimes
diferentes; ver [../arin/fonte.md](../arin/fonte.md#publicação)).

| RIR | `version` | `serial` | `startdate` | `enddate` | `UTCoffset` | Registros | Comentários | `.md5` |
|---|---|---|---|---|---|---|---|---|
| afrinic | `2` | 20260928 (data, = `enddate`) | `00000000` | 20260928 | `00000` | 19.786 | não | BSD, 74 bytes, sem `\n` no fim |
| apnic | `2.3` | 20260929 (data em Brisbane, `enddate` + 1) | vazia | 20260928 | `+1000` | 190.268 | 27 linhas antes do cabeçalho | BSD, 73 bytes; sai ~9 min depois do arquivo |
| arin | `2.3` | 1790600421096 (época em ms) | 19700101 | 20260928 | `-0400` (verão; `-0500` no inverno) | 203.051 | não | GNU, 67 bytes, cita o arquivo datado |
| lacnic | `2.3` | 20260927 (data) | 19870101 | 20260925 (2 dias antes do serial) | `-0300` | 97.301 | não | BSD |
| ripencc | `2` | 1790632799 (época em s) | 19700101 | 20260928 | `+0200` (verão; `+0100` no inverno) | 260.793 | não | BSD, 74 bytes, sem `\n` no fim |

| RIR | Ordem dos resumos | `cc` em available/reserved | Campos em available/reserved | opaque-id | Faixas de ASN (> 1 ASN) | IPv4 que não forma CIDR | Outros |
|---|---|---|---|---|---|---|---|
| afrinic | asn, ipv4, ipv6 | `ZZ` | 8 (opaque-id vazio) | hex maiúsculo, 8 caracteres | nenhuma | 52 (5 allocated, 47 assigned), 146 blocos | IPv4 de `/11` a `/24`, agrupado por status no arquivo (allocated, assigned, reserved, available) |
| apnic | asn, ipv4, ipv6 | vazio | 8 | hex maiúsculo, 8 caracteres | 743 (até 3.072 ASNs) | nenhum | IPv4 até `/26`, IPv6 até `/64` |
| arin | asn, ipv4, ipv6 | vazio | 8 | hex minúsculo, 32 caracteres | 143 assigned (até 290) + 1 available (1.371) | 2.765 (todos reserved) | 108 ASNs com data `00000000`; status só `assigned` em ASNs e `allocated` em blocos; IPv4 de `/8` a `/24`, IPv6 de `/13` a `/48` |
| lacnic | ipv4, ipv6, asn | vazio | available 7, reserved 8 | numérico, 2 a 6 dígitos | 33, só available (até 312) | nenhum | IPv4 de `/11` a `/24` |
| ripencc | ipv4, asn, ipv6 | vazio (usa também `EU`) | 7 | UUID, novo a cada arquivo | nenhuma | 970 | IPv4 até `/29`; data 19700101 em 2 ASNs; ordenado por status dentro de cada tipo |
