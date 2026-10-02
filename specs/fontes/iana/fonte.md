# Fonte `iana`: registros de numeração da IANA

A IANA publica, em arquivos pequenos e quase estáticos, a quem entregou cada
faixa de ASN e cada bloco IP (os cinco RIRs), o que é reservado para uso
especial e qual servidor RDAP responde por cada faixa. O `collector-iana`
trata os **10 arquivos como um dataset só**: baixa todos, confere todos e
aplica todos juntos ([collector.md](collector.md)). As tabelas estão em
[dados.md](dados.md).

## Arquivos

A lista está em `source.Files` (`internal/source/source.go`), na **ordem fixa
do dataset**: downloads, hash combinado e `iana_run.files` seguem esta ordem.
Os caminhos são fixos no código; só as bases são configuráveis
(`IANA_BASE_URL` para os 7 CSVs e `RDAP_BASE_URL` para os 3 JSONs, ver
[collector.md](collector.md#configuração)).

| # | Nome (`source_file`, `iana_run.files[].name`) | Base | Caminho abaixo da base | Tabela |
|---|---|---|---|---|
| 1 | `as-numbers-1` | IANA | `as-numbers/as-numbers-1.csv` | `iana_asn_block` |
| 2 | `as-numbers-2` | IANA | `as-numbers/as-numbers-2.csv` | `iana_asn_block` |
| 3 | `ipv4-address-space` | IANA | `ipv4-address-space/ipv4-address-space.csv` | `iana_prefix_block` |
| 4 | `ipv6-unicast-address-assignments` | IANA | `ipv6-unicast-address-assignments/ipv6-unicast-address-assignments.csv` | `iana_prefix_block` |
| 5 | `iana-ipv4-special-registry-1` | IANA | `iana-ipv4-special-registry/iana-ipv4-special-registry-1.csv` | `iana_special_prefix` |
| 6 | `iana-ipv6-special-registry-1` | IANA | `iana-ipv6-special-registry/iana-ipv6-special-registry-1.csv` | `iana_special_prefix` |
| 7 | `special-purpose-as-numbers` | IANA | `iana-as-numbers-special-registry/special-purpose-as-numbers.csv` | `iana_special_asn` |
| 8 | `rdap-asn` | RDAP | `asn.json` | `iana_rdap_service` (`kind = 'asn'`) |
| 9 | `rdap-ipv4` | RDAP | `ipv4.json` | `iana_rdap_service` (`kind = 'ipv4'`) |
| 10 | `rdap-ipv6` | RDAP | `ipv6.json` | `iana_rdap_service` (`kind = 'ipv6'`) |

Com as bases padrão:

```
https://www.iana.org/assignments/as-numbers/as-numbers-1.csv
https://www.iana.org/assignments/as-numbers/as-numbers-2.csv
https://www.iana.org/assignments/ipv4-address-space/ipv4-address-space.csv
https://www.iana.org/assignments/ipv6-unicast-address-assignments/ipv6-unicast-address-assignments.csv
https://www.iana.org/assignments/iana-ipv4-special-registry/iana-ipv4-special-registry-1.csv
https://www.iana.org/assignments/iana-ipv6-special-registry/iana-ipv6-special-registry-1.csv
https://www.iana.org/assignments/iana-as-numbers-special-registry/special-purpose-as-numbers.csv
https://data.iana.org/rdap/asn.json
https://data.iana.org/rdap/ipv4.json
https://data.iana.org/rdap/ipv6.json
```

`File.URL(ianaBase, rdapBase)` = base sem a barra final + `/` + caminho.
`File.Basename()` (`as-numbers-1.csv`, `asn.json`...) é o nome do arquivo no
servidor, das fixtures em `testdata/` e dos arquivos que `make test-real`
procura em `IANA_REAL_DIR`.

## Publicação (medido em 2026-09-28)

| Servidor | Validadores | Cache | GET condicional |
|---|---|---|---|
| `www.iana.org` (Cloudflare) | só `Last-Modified` (sem `ETag`) | `max-age=3600` | `If-Modified-Since` → `304` |
| `data.iana.org` (Cloudflare) | `ETag` fraco (`W/"1138-65336a3cb9688-br"`) e `Last-Modified` | `max-age=86400` | `If-None-Match` e `If-Modified-Since` → `304` |

- **Não há hash publicado** (nem MD5 nem SHA-256): a mudança se detecta por
  arquivo, pelo GET condicional e pelo SHA-256 do conteúdo
  ([collector.md](collector.md#uma-verificação)).
- Os `Last-Modified` dos CSVs eram de 2026-09-19; os dos JSONs, de 2019
  (`ipv4.json`), 2024 (`ipv6.json`) e 2026-06 (`asn.json`): a IANA muda
  raramente.

| Arquivo | Bytes | Linhas de dados / entradas | Registros gerados |
|---|---|---|---|
| `as-numbers-1` | 7.936 | 88 | 88 faixas |
| `as-numbers-2` | 7.641 | 86 (85 + "See Sub-registry") | 85 faixas |
| `ipv4-address-space` | 22.972 | 256 | 256 blocos `/8` |
| `ipv6-unicast-address-assignments` | 5.666 | 51 (em 59 linhas físicas) | 51 blocos |
| `iana-ipv4-special-registry-1` | 2.423 | 25 | 26 blocos |
| `iana-ipv6-special-registry-1` | 2.289 | 25 | 25 blocos |
| `special-purpose-as-numbers` | 593 | 9 | 9 faixas |
| `rdap-asn` | 4.408 | 5 serviços, 159 entradas | 159 |
| `rdap-ipv4` | 5.629 | 5 serviços, 221 entradas (`/8`) | 221 |
| `rdap-ipv6` | 1.476 | 5 serviços, 34 entradas | 34 |
| **Total** | **61.033** (~60 KB) | | 954 linhas nas 5 tabelas |

## Formato dos CSVs

Todos: UTF-8 só com ASCII, fim de linha `CRLF`, sem BOM, primeira linha é o
cabeçalho. Campos com vírgula, aspas ou quebra de linha vêm entre aspas;
aspas dentro do campo são dobradas (`"""This network"""`), e as quebras de
linha **dentro** das células são `\n` puro (às vezes seguidas de 8 espaços).
Notas de rodapé são marcas `[n]` (só dígitos entre colchetes) no nome de
colunas (`Status [1]`), em blocos (`192.0.0.0/24 [2]`), em flags
(`False [1]`) e na coluna `Note`; o texto das notas não está no CSV.

### `as-numbers-1.csv` e `as-numbers-2.csv`

```
Number,Description,WHOIS,RDAP,Reference,Registration Date
0,Reserved,,,[RFC7607],
1-1876,Assigned by ARIN,whois.arin.net,https://rdap.arin.net/registryhttp://rdap.arin.net/registry,,
1877-1901,Assigned by RIPE NCC,whois.ripe.net,https://rdap.db.ripe.net/,,
64297-64395,Assigned by APNIC,whois.apnic.net,https://rdap.apnic.net/,,2016-05-25
```

| | `as-numbers-1` | `as-numbers-2` |
|---|---|---|
| Faixa coberta | 0–65535, contígua | 65536–4294967295, contígua |
| Descrições | Assigned by ARIN (31), RIPE NCC (35), APNIC (12), LACNIC (4), AFRINIC (1), Reserved (2), AS_TRANS, documentação, uso privado | Assigned by APNIC (25), RIPE NCC (23), LACNIC (14), ARIN (11), AFRINIC (3), Unallocated (5), Reserved (2), documentação, uso privado |

- `Number` é um ASN (`2043`) ou uma faixa (`1-1876`).
- O `as-numbers-2` começa com `0-65535,See Sub-registry 16-bit AS numbers,,,[RFC1930],`,
  que só aponta para o `as-numbers-1`: **ignorada**.
- `Registration Date`: vazio, `AAAA-MM` (`2002-03`) ou `AAAA-MM-DD`
  (`2008-12-03`).
- A coluna `RDAP` do ARIN e do AFRINIC traz **duas URLs coladas, sem
  separador**: `https://rdap.arin.net/registryhttp://rdap.arin.net/registry`,
  `https://rdap.afrinic.net/rdap/http://rdap.afrinic.net/rdap/`.
- Uma alocação nova quase nunca remove linha
  ([collector.md](collector.md#trava-de-remoção-nas-tabelas-pequenas)).

### `ipv4-address-space.csv`

```
Prefix,Designation,Date,WHOIS,RDAP,Status [1],Note
000/8,IANA - Local Identification,1981-09,,,RESERVED,[2][3]
045/8,Administered by ARIN,1995-01,whois.arin.net,https://rdap.arin.net/registryhttp://rdap.arin.net/registry,LEGACY,
053/8,Daimler AG,1993-10,whois.ripe.net,https://rdap.db.ripe.net/,LEGACY,
```

- **Exatamente 256 linhas**, uma por `/8`, na forma `000/8`…`255/8` (só o
  primeiro octeto, com zeros à esquerda).
- Cabeçalho `Status [1]` (com nota de rodapé no nome da coluna).
- Status: `ALLOCATED` 129, `LEGACY` 92, `RESERVED` 35. `Date` sempre
  `AAAA-MM`.
- `Note` só tem as marcas de nota de rodapé (`[2][3]`, `[14]`…).
- 28 designações: nome do RIR (`APNIC`, `RIPE NCC`…), `Administered by X`,
  `Multicast`, `Future use`, `IANA - …` e 17 blocos legados com o nome do
  titular (`Apple Computer Inc.`, `US-DOD`, `Daimler AG`…). **O RIR de cada
  bloco sai da coluna WHOIS** (`whois.arin.net` → `arin`). `45/8` é
  "Administered by ARIN", mas a LACNIC delega partes dele (`45.171.60.0/22`):
  o RIR daqui é o da IANA, não o do bloco menor.
- Os 35 `RESERVED` não têm WHOIS nem RDAP.

### `ipv6-unicast-address-assignments.csv`

```
Prefix,Designation,Date,WHOIS,RDAP,Status,Note
2001::/23,IANA,1999-07-01,whois.iana.org,,ALLOCATED,This range has been partially allocated. ...
2400::/12,APNIC,2006-10-03,whois.apnic.net,https://rdap.apnic.net/,ALLOCATED,"2400::/19 was allocated on 2005-05-20. 2400:2000::/19 was allocated on 2005-07-08. 2400:4000::/21 was 
allocated on 2005-08-08.  2404::/23 was allocated on 2006-01-19. ..."
```

- 51 registros em 59 linhas físicas (5 notas com quebra de linha e espaço
  duplo). Cabeçalho `Status`, sem nota de rodapé.
- Prefixos de `/5` a `/23`, todos dentro de `2000::/3`, sem sobreposição.
- Status: `ALLOCATED` 36, `RESERVED` 15. `Date` em `AAAA-MM-DD`, exceto
  `3ffe::/16` (`2008-04`).
- `2001::/23` tem WHOIS `whois.iana.org` (não é RIR → `registry` NULL);
  `2002::/16` (6to4), `3fff::/20` (documentação) e os `RESERVED` não têm
  WHOIS.
- Os 5 RIRs aparecem.

### `iana-ipv4-special-registry-1.csv` e `iana-ipv6-special-registry-1.csv`

```
Address Block,Name,RFC,Allocation Date,Termination Date,Source,Destination,Forwardable,Globally Reachable,Reserved-by-Protocol
0.0.0.0/8,"""This network""","[RFC791], Section 3.2",1981-09,N/A,True,False,False,False,True
127.0.0.0/8,Loopback,"[RFC1122], Section 3.2.1.3",1981-09,N/A,False [1],False [1],False [1],False [1],True
192.0.0.0/24 [2],IETF Protocol Assignments,"[RFC6890], Section 2.1",2010-01,N/A,False,False,False,False,False
"192.0.0.170/32, 192.0.0.171/32",NAT64/DNS64 Discovery,"[RFC8880][RFC7050], Section 2.2",2013-02,N/A,False,False,False,False,True
192.88.99.0/24,Deprecated (6to4 Relay Anycast),[RFC7526],2001-06,2015-03,,,,,
255.255.255.255/32,Limited Broadcast,"[RFC8190]
        [RFC919], Section 7",1984-10,N/A,False,True,False,False,True
```

| | IPv4 | IPv6 |
|---|---|---|
| Linhas de dados | 25 (26 blocos) | 25 (25 blocos) |

- Notas de rodapé no bloco (`192.0.0.0/24 [2]`, `2002::/16 [3]`) e nas flags
  (`False [1]`, `N/A [2]`, `N/A [3]`, `False [4]`).
- Vários blocos numa célula: `"192.0.0.170/32, 192.0.0.171/32"`.
- `Termination Date` é `N/A` (em vigor) ou uma data (`2015-03`, `2014-03`).
- Registros encerrados (`192.88.99.0/24`, `2001:10::/28`) têm as 5 flags
  vazias; TEREDO (`2001::/32`) e 6to4 (`2002::/16`) têm `Globally Reachable`
  `N/A`.
- `RFC` com quebra de linha e 8 espaços no meio
  (`[RFC8190]\n        [RFC919], Section 7`).
- Blocos aninhados são normais (`0.0.0.0/8` e `0.0.0.0/32`; `2001::/23` e
  `2001::/32`).

### `special-purpose-as-numbers.csv`

```
AS Number,Reason for Reservation,Reference
0,Reserved by [RFC7607],[RFC7607]
64512-65534,For private use; reserved by [RFC6996],[RFC6996]
4200000000-4294967294,For private use; reserved by [RFC6996],[RFC6996]
```

9 linhas: 0, 112, 23456, 64496–64511, 64512–65534, 65535, 65536–65551,
4200000000–4294967294, 4294967295.

## Formato dos JSONs RDAP (RFC 9224)

```json
{
  "description": "RDAP bootstrap file for Autonomous System Number allocations",
  "publication": "2026-06-01T20:00:01Z",
  "services": [
    [["1-1876", "1902-2042", "..."], ["https://rdap.arin.net/registry/", "http://rdap.arin.net/registry/"]],
    [["1877-1901", "2043", "..."], ["https://rdap.db.ripe.net/"]]
  ],
  "version": "1.0"
}
```

| | `asn.json` | `ipv4.json` | `ipv6.json` |
|---|---|---|---|
| Serviços | 5 (um por RIR) | 5 | 5 |
| Entradas | 159 | 221 (`/8`) | 34 |
| `publication` | 2026-06-01T20:00:01Z | 2019-06-07T19:00:02Z | 2024-11-01T22:00:01Z |

- Como nos recortes de `testdata/`: 2 espaços de indentação, um item por
  linha (o parser não depende disso).
- As entradas batem 1:1 com as linhas de RIR dos CSVs (83 + 76 faixas de
  ASN; 256 − 35 reservados no IPv4; 51 − 17 sem RDAP no IPv6), sem repetição.
- ARIN e AFRINIC publicam duas URLs (https e http), com barra final
  (`https://rdap.arin.net/registry/`), diferente da coluna RDAP dos CSVs.

## Regras do parser (`internal/parse`)

`parse.Parse(files)` recebe os 10 conteúdos (nome → bytes) e interpreta cada
um na ordem fixa. Recusa o dataset inteiro (erro `<arquivo>: <motivo>`) se:
faltar um arquivo (`arquivo ausente`), o cabeçalho for ilegível (arquivo
vazio: `cabeçalho ilegível: EOF`) ou não tiver as colunas obrigatórias, um
JSON for inválido, um arquivo não tiver nenhuma linha de
dados (`arquivo sem nenhuma linha de dados`) ou mais de 1% das linhas de um
arquivo forem descartadas. Por arquivo, `FileStats` conta `Records` (linhas
ou entradas com dados), `Rows` (registros gerados), `Skipped` (linhas
descartadas) e, nos JSONs, `Publication`.

### Leitura dos CSVs

- BOM (`EF BB BF`) removido; UTF-8 inválido vira `U+FFFD`.
- `encoding/csv` com `LazyQuotes = true` e `FieldsPerRecord = -1`.
- Colunas achadas **pelo nome** normalizado (sem nota de rodapé, espaços
  juntados, minúsculas: `Status [1]` → `status`): a ordem pode mudar; nome
  repetido, vale o primeiro. Coluna obrigatória ausente recusa o arquivo
  (`coluna obrigatória "x" ausente no cabeçalho [...]: formato mudou?`);
  opcional ausente fica vazia, com aviso
  (`coluna "x" ausente no cabeçalho; fica vazia`).

  | Arquivo | Obrigatórias | Opcionais |
  |---|---|---|
  | `as-numbers-1`, `as-numbers-2` | `number`, `description` | `whois`, `rdap`, `reference`, `registration date` |
  | `ipv4-address-space`, `ipv6-unicast-address-assignments` | `prefix`, `designation`, `status` | `date`, `whois`, `rdap`, `note` |
  | special registries (IPv4 e IPv6) | `address block`, `name` | `rfc`, `allocation date`, `termination date`, `source`, `destination`, `forwardable`, `globally reachable`, `reserved-by-protocol` |
  | `special-purpose-as-numbers` | `as number`, `reason for reservation` | `reference` |

- Linha com todos os campos vazios (ou só brancos) é ignorada e não conta.
- Nos as-numbers, a linha cuja descrição começa com `See Sub-registry` (sem
  distinção de maiúsculas) é ignorada e não conta.
- Registro que o `encoding/csv` não leu conta como linha e é descartado; mais
  de 1000 desses → `CSV ilegível` (recusa).

### Campos

- **Texto** (`Description`, `Designation`, `Name`, `Note`, `RFC`,
  `Reference`, `Reason for Reservation`): espaços, tabs e quebras de linha
  viram um espaço só; aspas da IANA preservadas (`"This network"`); as marcas
  `[n]` ficam (a `note` do IPv4 é `[2][3]`).
- **Faixa de ASN** (`Number`, `AS Number`, entradas do `asn.json`): notas de
  rodapé e espaços tirados; prefixo `AS` opcional (`AS2043`); `N` ou `N-M`,
  cada número de 0 a 4294967295; início ≤ fim. Forma canônica (`resource`):
  `2043` ou `1-1876`.
- **Bloco** (`Prefix`, `Address Block`, entradas de `ipv4.json`/`ipv6.json`):
  notas de rodapé e espaços tirados; CIDR, ou a forma da IANA `NNN/8` (só o
  primeiro octeto: `045/8` → `45.0.0.0/8`); bits de host zerados, com aviso
  (o `cidr` do Postgres os recusaria); a família tem de bater com o arquivo.
- **Célula com vários blocos** (special registries): separada por vírgula,
  `;`, espaço, tab ou quebra de linha; cada bloco vira um registro com os
  mesmos atributos.
- **Flags** (`Source`, `Destination`, `Forwardable`, `Globally Reachable`,
  `Reserved-by-Protocol`): nota de rodapé tirada, sem distinção de
  maiúsculas; `true`/`yes` → true, `false`/`no` → false; vazio, `N/A`, `NA`
  e `-` → NULL; outro valor → NULL com aviso.
- **Datas** (`Date`, `Registration Date`, `Allocation Date`,
  `Termination Date`): nota de rodapé tirada; `AAAA-MM` ou `AAAA-MM-DD`
  válidas no calendário (`2008-13` e `2008-02-30` não passam), guardadas
  **como texto**, sem inventar o dia; vazio e `N/A` → NULL; outro formato →
  NULL com aviso.
- **RDAP** (coluna dos CSVs): cada `http://`/`https://` (sem distinção de
  maiúsculas) começa uma URL nova, o que separa as coladas; pontas sem
  brancos, `,` e `;`; repetidas saem; texto antes da primeira URL ou URL sem
  host → coluna vazia (`{}`), com aviso.
- **WHOIS**: brancos juntados, minúsculas.
- **Status**: nota de rodapé tirada, maiúsculas; tem de casar
  `^[A-Z][A-Z _-]*$`. Fora dos medidos (`ALLOCATED`, `LEGACY`, `RESERVED`),
  entra com aviso (`status desconhecido "X" (aceito)`).
- **RIR** (`registry`): o domínio do WHOIS (`afrinic.net`, `apnic.net`,
  `arin.net`, `lacnic.net`, `ripe.net` ou subdomínio; porta ignorada) dá
  `afrinic`, `apnic`, `arin`, `lacnic`, `ripencc`. Sem RIR no WHOIS (vazio ou
  `whois.iana.org`), vale o nome, pela expressão
  `(?i)^(?:(?:assigned|administered) by\s+)?(afrinic|apnic|arin|lacnic|ripe\s*ncc)$`
  (`Assigned by RIPE NCC` → `ripencc`, `LACNIC` → `lacnic`). WHOIS e nome
  divergentes: vale o WHOIS, com aviso. Nenhum dos dois → NULL. Nas entradas
  RDAP, o domínio da primeira URL de RIR do serviço.

### Descartes

| Situação | Efeito |
|---|---|
| Faixa de ASN ilegível ou invertida | descarta a linha (`faixa de ASN inválida: "x"`, `faixa de ASN invertida: "x"`) |
| `asn_start` repetido (inclusive entre `as-numbers-1` e `-2`) | descarta (`faixa começando em N repetida (já vista em <arquivo>)`) |
| Bloco ilegível, de família errada ou repetido (address space) | descarta (`bloco inválido: "x"`, `bloco <cidr> da família errada para este arquivo`, `bloco <cidr> repetido (já visto em <arquivo>)`) |
| Status fora de `^[A-Z][A-Z _-]*$` (inclusive vazio) | descarta (`status inválido "x"`) |
| Special registry: bloco ilegível, de família errada ou repetido | ignora só o bloco, com aviso; descarta a linha se não sobrar bloco (`nenhum bloco válido em "x"`) |
| Special ASN: faixa ilegível ou `asn_start` repetido | descarta |
| Data, flag ou URL irreconhecível | NULL (ou `{}`), com aviso; a linha fica |

- Se mais de **1%** (`parse.MaxSkippedRatio = 0.01`) das linhas de
  **qualquer** arquivo for descartada, o dataset inteiro é recusado:
  `<arquivo>: N de M linhas descartadas (x%, limite 1.0%): formato mudou?
  avisos: <até 3 avisos do arquivo>`. Nos arquivos pequenos (special
  registries, ~25 linhas) uma linha com chave ilegível já recusa: é
  preferível manter os dados atuais a aplicar um registro de bogons
  incompleto.
- Avisos: `<arquivo> linha <n>: <texto>` (nos JSONs, `<arquivo>: <texto>`);
  descartes começam com `linha descartada: `. O dataset guarda os 50
  primeiros (`parse.MaxWarnings`) e conta o total (`WarningCount()`).

### JSONs RDAP

- BOM removido; decodificado como `description`, `publication`, `version` e
  `services` (lista de `[entradas, URLs]`, só textos). JSON inválido (ou um
  elemento que não é texto) → `JSON inválido: ...`; sem `services` →
  `JSON sem a lista services: formato mudou?`; `services` vazio → sem linhas
  de dados → recusa.
- Serviço com menos de 2 elementos: as entradas dele contam como descartadas
  (`serviço N descartado: esperava [entradas, URLs]`); elementos além do
  segundo são ignorados.
- URLs: brancos das pontas tirados; o que não for `http(s)://` com host é
  ignorado com aviso (`serviço N: URL inválida "x" ignorada`); serviço sem
  URL válida descarta as entradas dele. As URLs ficam na ordem publicada.
- Entradas: `asn.json` → faixas de ASN; `ipv4.json`/`ipv6.json` → blocos
  (família conferida; bits de host zerados com aviso); entrada repetida no
  mesmo arquivo → descartada (`serviço N: entrada X repetida`).
- `publication` (RFC 3339) vai em UTC para `iana_run.files[].publication`;
  ilegível → aviso, sem `publication`.

## Sanidade do dataset (`parse.Check`)

Depois do parser, `parse.Check(ds, parse.DefaultLimits())` confere o dataset
inteiro e devolve **todas** as falhas juntas; qualquer uma recusa tudo
(`sanidade: <falhas>`).

| Checagem | Medido (2026-09-28) | Exigido | Mensagem |
|---|---|---|---|
| Todo arquivo gerou registros | — | ≥ 1 | `<arquivo>: nenhum registro` |
| Faixas de ASN sem sobreposição | — | sempre | `faixas de ASN sobrepostas: A (<arquivo>) e B (<arquivo>)` |
| `as-numbers-1` cobre 0–65535 sem buracos | sim | sim | `as-numbers-1: buraco entre A e B`, `... faixa X fora de 0–65535`, `... faixas terminam em N, esperado 65535` |
| `as-numbers-2` cobre 65536–4294967295 sem buracos | sim | sim | idem |
| `ipv4-address-space` | 256 × `/8` | exatamente 256, todos `/8` | `... N blocos /8, esperados exatamente 256`, `... bloco X não é /8` |
| `ipv6-unicast-address-assignments` | 51, 5 RIRs | ≥ 30 (`MinIPv6Blocks`), os 5 RIRs | `... só N blocos (mínimo 30)`, `... nenhum bloco de <rir>` |
| Special IPv4 / IPv6 (blocos) | 26 / 25 | ≥ 15 / ≥ 15 (`MinSpecialIPv4`, `MinSpecialIPv6`) | `<arquivo>: só N blocos (mínimo 15)` |
| Special ASN | 9 | ≥ 6 (`MinSpecialASNs`) | `special-purpose-as-numbers: só N faixas (mínimo 6)` |
| RDAP asn / ipv4 / ipv6 (entradas) | 159 / 221 / 34 | ≥ 100 / ≥ 150 / ≥ 20 (`MinRDAPASN`, `MinRDAPIPv4`, `MinRDAPIPv6`), e os 5 RIRs em cada | `<arquivo>: só N entradas (mínimo M)`, `<arquivo>: sem servidor RDAP de <rir>` |

- `Limits.Complete` (verdadeiro em `DefaultLimits`) liga as coberturas de
  ASN, os 256 `/8` e os 5 RIRs; sobreposição e mínimos valem sempre.
- Os mínimos ficam em ~60% do medido: os registros da IANA só crescem, então
  uma queda desse tamanho é arquivo truncado ou formato novo.

## Fixtures (`testdata/`)

Um recorte real de cada arquivo (de 2026-09-28), com o nome do servidor: as
linhas escolhidas têm os mesmos bytes do original (`CRLF`, aspas e quebras
de linha dentro das células preservadas), e os JSONs têm a estrutura inteira
com as 3 primeiras entradas de cada serviço (todas, quando há menos), na
ordem publicada. Juntos cobrem os casos esquisitos: URLs coladas,
"See Sub-registry", `000/8`, notas de rodapé, `N/A`, flags vazias, célula com
dois blocos, notas e RFC com quebra de linha, `2008-04` no IPv6.

| Fixture | Bytes | Linhas físicas (com cabeçalho) | Registros | O que tem |
|---|---|---|---|---|
| `as-numbers-1.csv` | 818 | 12 | 11 | AS0, ARIN `1-1876` (URLs coladas), RIPE NCC, APNIC, AS_TRANS, LACNIC (`2002-11`), AFRINIC (URLs coladas, `2005-04`), APNIC `2016-05-25`, documentação, uso privado, 65535 |
| `as-numbers-2.csv` | 900 | 12 | 10 | "See Sub-registry", documentação, `Reserved`, APNIC, LACNIC, AFRINIC e ARIN (estes dois com URLs coladas; um ARIN de `2026-03-14`), `Unallocated` `404381-4199999999`, uso privado, 4294967295 |
| `ipv4-address-space.csv` | 1.293 | 17 | 16 | `000/8` (`[2][3]`), APNIC, RIPE NCC, `Administered by ARIN`, legados com titular (`006/8`, `053/8` Daimler AG), `010/8`, AFRINIC, `045/8`, `127/8`, LACNIC, `Administered by LACNIC`, multicast, `Future use` |
| `ipv6-unicast-address-assignments.csv` | 2.141 | 19 (15 `CRLF`) | 14 | `2001::/23` (`whois.iana.org`), os 5 RIRs, nota com vírgulas, 6to4, 3 notas com quebra de linha, `2d00::/8`, `3ffe::/16` (`2008-04`), `3fff::/20` |
| `iana-ipv4-special-registry-1.csv` | 919 | 10 (9 `CRLF`) | 8 (9 blocos) | `"""This network"""`, privados, loopback (`False [1]`), `192.0.0.0/24 [2]`, célula com dois blocos, `192.88.99.0/24` encerrado, RFC com quebra de linha |
| `iana-ipv6-special-registry-1.csv` | 802 | 11 (9 `CRLF`) | 8 | `::1/128`, `2001::/23` e `2001::/32` (TEREDO, `N/A [2]`), ORCHID encerrado, documentação, `2002::/16 [3]`, `fc00::/7` (`False [4]`), link-local |
| `special-purpose-as-numbers.csv` | 306 | 6 | 5 | 0, 23456, 64512–65534, 4200000000–4294967294, 4294967295 |
| `asn.json` | 985 | 59 | 15 | 5 serviços × 3 entradas, com as duas URLs de ARIN e AFRINIC |
| `ipv4.json` | 983 | 59 | 15 | 5 × 3 |
| `ipv6.json` | 980 | 57 | 13 | AFRINIC e LACNIC com 2 (todas), os outros com 3 |

- Os recortes geram 21 faixas de ASN, 30 blocos IP, 17 blocos especiais, 5
  ASNs especiais e 43 entradas RDAP (116 linhas), **sem nenhum aviso**.
- Como são recortes, não passam na sanidade de produção (buracos nas faixas
  de ASN, 16 de 256 `/8`, mínimos): os testes usam limites relaxados
  (`Complete = false`, `MinIPv6Blocks`, `MinSpecialIPv4`, `MinSpecialIPv6` e
  os três `MinRDAP*` = 5, `MinSpecialASNs` = 3) e conferem que, com
  `DefaultLimits`, eles são recusados.
- Mudou o formato de um arquivo: atualize o recorte (linhas reais, não o
  arquivo inteiro), os testes do parser e esta spec; `make test-real` confere
  o arquivo inteiro do dia ([collector.md](collector.md#testes)).
