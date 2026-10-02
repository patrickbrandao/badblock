# Fonte `cgibr`: arquivo `nicbr-asn-blk` do NIC.br

De onde vem o dado, o formato do arquivo, os fatos medidos, as regras do
parser (`internal/parse`) e a fixture. Dono: sub-agente `collector-cgibr`. As
tabelas estão em [dados.md](dados.md); o que o coletor faz com o arquivo
(checagens, conferência do hash, mínimos, aplicação), em
[collector.md](collector.md).

## URLs

| O quê | URL |
|---|---|
| Arquivo | `https://ftp.registro.br/pub/numeracao/origin/nicbr-asn-blk-latest.txt` |
| Hash publicado | `https://ftp.registro.br/pub/numeracao/origin/nicbr-asn-blk-latest.txt.sha256` |
| Histórico diário | `nicbr-asn-blk-AAAAMMDD.txt` (+ `.sha256`) na mesma pasta |

O coletor lê só o `-latest` (`SOURCE_URL`) e o `.sha256` ao lado dele
(`SOURCE_SHA256_URL`); o histórico diário não é usado.

## Publicação

Registrado em 2026-09-28:

- O NIC.br publica um arquivo por dia útil, por volta de 00:06 (UTC−3).
- As respostas HTTP trazem `ETag` e `Last-Modified` (o coletor os reenvia no
  GET condicional).
- O `.sha256` vem no formato BSD, numa linha só. Exemplo real:

```
SHA256 (nicbr-asn-blk-latest.txt) = 0a474b20ea017ffeebfc069f378acf41872a682c423856afc94f6ecfa3bb65c2
```

Como o coletor extrai o hash (formatos aceitos, limite de tamanho):
[collector.md](collector.md#checagens-de-mudança).

## Formato do arquivo

Texto UTF-8, **uma linha por ASN**, campos separados por `|`, sem cabeçalho
nem rodapé (em 2026-09-28 o número de linhas era igual ao de ASNs):

```
AS61610|ELEA DATA CENTERS|35.980.592/0001-30|187.87.28.0/22|2804:8ae0::/32|200.225.48.0/21
AS6125|Rede Nacional de Ensino e Pesquisa|03.508.097/0001-36
AS275689|Internet Systems Consortium|10996639
```

| Campo | Conteúdo |
|---|---|
| 1 | ASN com o prefixo `AS` |
| 2 | Nome do titular |
| 3 | Documento: CNPJ formatado (`00.000.000/0000-00`) ou identificador estrangeiro de 8 dígitos |
| 4… | Zero ou mais blocos CIDR, IPv4 e IPv6 misturados, sem ordem |

## Fatos medidos (2026-09-28)

- 9.134 linhas/ASNs, 13.037 blocos IPv4, 8.954 blocos IPv6 (21.991 blocos);
  ~844 KB.
- Nenhum ASN repetido e nenhum bloco repetido entre linhas.
- Algumas linhas não têm blocos (ex.: AS6125).
- 24 linhas com identificador estrangeiro de 8 dígitos (todas da Internet
  Systems Consortium, `10996639`); o resto é CNPJ.
- Algumas linhas usam ASN registrado fora do Brasil (AS174, AS8075…): o nome
  e o documento são do titular brasileiro dos blocos, não do ASN.

O que esses números fixam na configuração (`MIN_ASNS`, trava de remoção):
[collector.md](collector.md#validação-de-um-arquivo-novo).

## Regras do parser (`internal/parse`)

`parse.Parse` lê o arquivo inteiro e devolve os ASNs **na ordem do arquivo**,
cada um com os blocos na ordem em que aparecem, as contagens `PrefixesV4` e
`PrefixesV6`, as linhas de dados lidas (`Lines`) e descartadas (`Skipped`) e
os avisos.

### Leitura

- Linha a linha, com até 8 MiB por linha (buffer inicial de 64 KiB); uma
  linha maior recusa o arquivo (`leitura: ...`).
- O BOM UTF-8 (`EF BB BF`) no começo da primeira linha é ignorado, e o `\r`
  de fim de linha (CRLF) também.
- Bytes que não formam UTF-8 válido viram `U+FFFD` (`�`) antes de separar os
  campos — o Postgres recusaria o texto.
- Espaços nas pontas da linha e de cada campo são removidos.
- Linhas vazias e as começadas por `#` são ignoradas e não contam como linha
  de dados.
- O `N` dos avisos (`linha N: ...`) conta todas as linhas físicas, a partir
  de 1.

### Linha descartada

A linha é dividida em `|` e **descartada inteira** (conta em `Skipped`, com
aviso) quando:

| Caso | Aviso |
|---|---|
| menos de 3 campos | `linha N: linha descartada: esperados ao menos 3 campos, vieram K` |
| campo 1 sem o prefixo `AS` (maiúsculas ou minúsculas) | `linha N: linha descartada: ASN sem o prefixo AS: "<campo>"` |
| depois do `AS`, algo que não é um decimal de 0 a 4294967295 (sinal, espaço, letra, acima de 32 bits) | `linha N: linha descartada: ASN inválido: "<campo>"` |

- Zeros à esquerda são aceitos (`AS061610` é o 61610).
- Nome (campo 2) e documento (campo 3) não são validados: entram como vieram,
  sem os espaços das pontas, mesmo vazios.

### ASN repetido

Vale a primeira ocorrência para o nome e o documento; os blocos das
ocorrências seguintes são somados a ela, com o aviso
`linha N: AS<n> repetido; blocos somados à primeira ocorrência`. A linha
repetida não conta como descartada.

### Blocos (campo 4 em diante)

- Campo vazio (`||`, ou `|` no fim da linha) é ignorado sem aviso.
- Cada campo é lido por `netip.ParsePrefix` (`endereço/tamanho`, IPv4 ou
  IPv6). O que não passa descarta só o bloco (o resto da linha vale):
  `linha N: bloco inválido "<campo>" descartado`.
- Bloco com bits de host vira a forma canônica:
  `linha N: bloco 10.0.0.1/8 com bits de host; usando 10.0.0.0/8`.
- Bloco repetido (comparado já na forma canônica) fica com a primeira
  ocorrência: no mesmo ASN (na mesma linha ou num ASN repetido) é descartado
  sem aviso; em outro ASN,
  `linha N: bloco <p> já pertence a AS<a>; ocorrência em AS<b> descartada`.
- Endereço IPv4 conta em `PrefixesV4`; o resto, em `PrefixesV6`.

### Arquivo recusado

O parser devolve erro — e o coletor recusa o arquivo com `parser: <erro>` —
quando:

| Caso | Erro |
|---|---|
| erro de leitura (inclusive linha acima de 8 MiB) | `leitura: <erro>` |
| nenhuma linha de dados | `arquivo sem nenhuma linha de dados` |
| mais de **1%** das linhas de dados descartadas (`Skipped / Lines > 0.01`; exatamente 1% passa) | `<d> de <n> linhas descartadas (<x.x>%, limite 1.0%): formato mudou? primeiros avisos: <até 3 avisos separados por "; ">` |

O limite de 1% existe porque um formato novo ou um arquivo corrompido não
pode virar uma remoção em massa: um arquivo com muitas linhas ilegíveis é
recusado, em vez de apagar os ASNs dessas linhas.

### Avisos

Cada aviso começa com `linha N: `. O parser guarda os **50** primeiros
(`MaxWarnings`) e conta o total (`WarningCount()`); o que o coletor faz com
eles está em [collector.md](collector.md#avisos).

## Fixture (`testdata/nicbr-asn-blk-sample.txt`)

Recorte real do arquivo do NIC.br: **11 linhas inteiras**, em ordem numérica
de ASN; nenhum ajuste foi registrado e a data do arquivo de origem não foi
anotada. 1.527 bytes, UTF-8 sem BOM, fim de linha LF (com quebra na última
linha), SHA-256
`f853675334ef890e4222d6d7b088fea8874ddc195877796db69bfadedd842954`. O parser
lê dela **11 ASNs, 52 blocos IPv4 e 7 IPv6** (59), sem nenhum aviso.

| Linha | ASN | IPv4 | IPv6 | O que cobre |
|---|---|---|---|---|
| 1 | AS174 | 0 | 1 | ASN registrado fora do Brasil (Cogent); nome com acentos em UTF-8 (`COGENT BRASIL TELECOMUNICAÇÕES LTDA.`) |
| 2 | AS1916 | 38 | 1 | a linha mais longa (672 bytes, 39 blocos), com o IPv6 no meio dos IPv4, sem ordem (RNP) |
| 3 | AS2635 | 1 | 1 | IPv6 `/48` |
| 4 | AS6125 | 0 | 0 | sem blocos; mesmo titular e CNPJ (`03.508.097/0001-36`) do AS1916 |
| 5 | AS6505 | 0 | 0 | sem blocos |
| 6 | AS8075 | 2 | 0 | ASN registrado fora do Brasil (Microsoft) |
| 7 | AS61610 | 2 | 1 | exemplo das specs e da API: documento `35.980.592/0001-30`, blocos `187.87.28.0/22`, `2804:8ae0::/32`, `200.225.48.0/21`, nessa ordem |
| 8 | AS262287 | 4 | 1 | ASN de 32 bits, IPv6 no meio dos IPv4 |
| 9 | AS263009 | 4 | 1 | idem |
| 10 | AS264409 | 1 | 1 | ASN de 32 bits |
| 11 | AS275689 | 0 | 0 | identificador estrangeiro `10996639` (Internet Systems Consortium), sem blocos |

Os testes dependem destes números: 11/52/7 e zero avisos (`parse`,
`collector`); os campos e a ordem dos blocos do AS61610, o AS6125 sem blocos,
o documento do AS275689 e o nome do AS174 (`parse`); 11 + 59 linhas inseridas,
`document_digits` do AS61610 e a saída do AS6505 (`store`)
([collector.md](collector.md#testes-específicos)).

### Mudou o formato

Atualize juntos, no mesmo trabalho: esta spec, a fixture (um novo recorte
real) e os testes do parser (`internal/parse/parse_test.go`), além dos números
que dependem da fixture em `internal/collector/collector_test.go` e
`internal/store/store_integration_test.go`.
