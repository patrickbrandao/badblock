# Fonte `anatel/pst`: prestadoras de serviços de telecomunicações

De onde vem o dado, o ZIP e o CSV, os fatos medidos, as regras do parser
(`internal/parse`) e a fixture. Dono: sub-agente `collector-anatel-pst`. As
tabelas estão em [dados.md](dados.md); o que o coletor faz com o arquivo
(checagens, CSV mais antigo, mínimos, aplicação), em
[collector.md](collector.md).

## URL

| O quê | URL |
|---|---|
| Arquivo | `https://www.anatel.gov.br/dadosabertos/paineis_de_dados/outorga_e_licenciamento/prestadoras_servicos_telecomunicacoes.zip` |
| Hash publicado | **não há** |

O coletor lê só o ZIP (`SOURCE_URL`). Não há `.md5` nem `.sha256`: o servidor
responde `200` com uma página HTML (`text/html; charset=windows-1252`) para
qualquer caminho inexistente, inclusive `<arquivo>.md5` e `<arquivo>.sha256`
(verificado em 2026-09-30). Por isso o coletor não tem opção de URL de hash
nem conferência.

## Publicação

Registrado em 2026-09-30:

- O arquivo é regerado pela Anatel uma vez por dia (o CSV dentro do ZIP de
  2026-09-30 tinha data de modificação `2026-09-30 06:15:08`, hora de
  Brasília; o `Last-Modified` do ZIP era `Wed, 30 Sep 2026 10:56:48 GMT`).
- Servido pelo Cloudflare (`cf-cache-status: HIT`,
  `cache-control: public, max-age=14400`): uma cópia pode ficar até 4 h
  atrás da origem.
- As respostas trazem `ETag` (ex.: `"3714464ca50dd1:1a98"`, do IIS) e
  `Last-Modified`; o GET condicional com qualquer um dos dois responde `304`.
- `Content-Type: application/x-zip-compressed`.

## O ZIP

- Um ZIP comum (método `deflate`) com **uma entrada só**:
  `prestadoras_servicos_telecomunicacoes.csv`.
- Em 2026-09-30: ZIP com 14.734.094 bytes; CSV com 79.203.643 bytes.
- A data de modificação da entrada (campo MS-DOS do ZIP, sem fuso) é a hora
  de Brasília (UTC−3, sem horário de verão desde 2019); o coletor a guarda
  como `csv_modified_at` e a usa para não aplicar um CSV mais antigo que o
  aplicado ([collector.md](collector.md#o-zip-internalarchive)).
- A entrada traz também um timestamp estendido NTFS (extra `0x000a`, gerado
  no Windows): em 2026-09-30, `09:15:06.14 UTC` contra o MS-DOS `06:15:08`
  (o MS-DOS tem resolução de 2 s). O coletor usa o MS-DOS
  ([collector.md](collector.md#o-zip-internalarchive)).

## Formato do CSV

- UTF-8 **com BOM** (`EF BB BF`), fim de linha CRLF.
- Separador `;`. Aspas no padrão RFC 4180: campo com `;` ou `"` vem entre
  aspas, e `"` dentro dele dobra (`""`). Ex.: `"QUADRA 08, MODULOS 18 A 21;"`,
  `"Pérola d'Oeste"`. O `encoding/csv` do Go, no modo estrito, lê o arquivo
  inteiro sem erro (verificado em 2026-09-30).
- Primeira linha: cabeçalho com **24 colunas**; depois, uma linha por
  serviço notificado. Sem rodapé.
- Valores ausentes vêm como `N/I` ("não informado"), `N/A` ("não se
  aplica"), `-` ou vazio, conforme a coluna.

### Colunas

| # | Cabeçalho (exato) | Conteúdo | Exemplo |
|---|---|---|---|
| 1 | `Tipo de Identificação` | `CNPJ` ou `CPF` | `CNPJ` |
| 2 | `CNPJ ou CPF` | CNPJ com 14 dígitos, sem pontuação; CPF mascarado pela Anatel (`***03745**`) | `02558157000162` |
| 3 | `Nome Entidade Prestadora de Serviço` | razão social (ou nome da pessoa) | `TELEFONICA BRASIL S.A.` |
| 4 | `Nome Fantasia` | nome fantasia; `N/I` quando não há | `N/I` |
| 5 | `Tipo de Entidade` | `Outorgada` ou `Dispensada de Outorga` | `Outorgada` |
| 6 | `Tipo de Outorga` | `Serviços de Interesse Coletivo e Restrito - SIC`, `Serviços de Interesse Restrito - SIR` ou `Dispensada de Outorga` | `Serviços de Interesse Coletivo e Restrito - SIC` |
| 7 | `Fistel da Outorga` | Fistel da outorga, 11 dígitos; `N/A` quando dispensada | `50423150120` |
| 8 | `Processo SEI da Outorga` | número do processo SEI; `N/A` quando dispensada | `53500036134202046` |
| 9 | `Data Inclusão da Outorga` | `DD/MM/AAAA`; `N/A` quando dispensada | `05/01/2021` |
| 10 | `Serviço da Notificação` | grupo do serviço | `Banda Larga Fixa` |
| 11 | `Código e Nome do Serviço da Notificação` | `NNN - nome` (código de 3 dígitos) | `045 - Serviço de Comunicação Multimídia` |
| 12 | `Fistel da Notificação` | Fistel da notificação do serviço, 11 dígitos | `50013053736` |
| 13 | `Processo SEI da Notificação` | número do processo; às vezes `N/I` ou `0` | `535000020652002` |
| 14 | `Data Inclusão da Notificação` | `DD/MM/AAAA` | `13/02/2003` |
| 15 | `Logradouro do Endereço Sede` | logradouro (`-` nas linhas de CPF) | |
| 16 | `Número do Endereço Sede` | número, como texto (`S/N`, `.`, `1740A`…) | |
| 17 | `Complemento do Endereço Sede` | complemento; `N/I`, espaço ou vazio quando não há | |
| 18 | `Bairro do Endereço Sede` | bairro | |
| 19 | `CEP do Endereço Sede` | CEP, só dígitos (8; alguns com 7, sem o zero à esquerda) | |
| 20 | `Código IBGE do Munícipio do Endereço Sede` | código IBGE do município, 7 dígitos | `3550308` |
| 21 | `Nome do Munícipio do Endereço Sede` | município | `São Paulo` |
| 22 | `UF do Endereço Sede` | sigla da UF | `SP` |
| 23 | `Telefone Principal` | telefone, texto livre; `N/I` ou vazio | |
| 24 | `Endereço Eletrônico` | e-mail; `N/I` ou vazio | |

Linha real (endereço, telefone e e-mail omitidos aqui):

```
CNPJ;02558157000162;TELEFONICA BRASIL S.A.;N/I;Outorgada;Serviços de Interesse Coletivo e Restrito - SIC;50423150120;53500036134202046;05/01/2021;Banda Larga Fixa;045 - Serviço de Comunicação Multimídia;50013053736;535000020652002;13/02/2003;…
```

## Fatos medidos (2026-09-30)

Do arquivo inteiro:

- 276.261 linhas de dados: **64.902 de CNPJ** e 211.359 de CPF (77%: rádio
  do cidadão, radioamador, móvel marítimo e aeronáutico de pessoas físicas,
  com o CPF mascarado — não serve de chave: o mesmo `***nnnnnn**` aparece com
  pessoas diferentes).
- 20.226 linhas são cópias exatas de outra linha (10.588 delas de CNPJ).

Só das linhas de CNPJ, depois de tirar as cópias exatas (54.314 linhas):

- **45.074 CNPJs**, todos com 14 dígitos e dígitos verificadores válidos.
- Os dados da prestadora (colunas 3, 4 e 15 a 24) são **iguais em todas as
  linhas do mesmo CNPJ**.
- Cada Fistel de outorga pertence a um CNPJ só; 211 CNPJs têm mais de uma
  outorga (ex.: SIR e SIC). 1.009 linhas são de entidade `Dispensada de
  Outorga` (colunas 7 a 9 = `N/A`).
- A chave `(CNPJ, Fistel da Outorga, Fistel da Notificação, código do
  serviço)` é única. Sem o Fistel da outorga, não é: o mesmo Fistel de
  notificação aparece sob duas outorgas do mesmo CNPJ em 188 linhas.
- 55 códigos de serviço; cada código tem um grupo só (coluna 10). Um CNPJ tem
  de 1 a 72 serviços; o mesmo código pode aparecer várias vezes no mesmo CNPJ,
  com Fistéis de notificação diferentes.
- Prestadoras por serviço: **SCM (045) 20.510**, STFC (171) 1.661, SeAC (750)
  1.446, SMP (010) 27.
- Datas: todas `DD/MM/AAAA` ou `N/A`; notificações de 1982 a 2026.
- `Nome Fantasia` = `N/I` em 18.989 linhas; telefone `N/I` em 11.190, e-mail
  `N/I` em 4.269; 606 campos com espaços nas pontas.
- UF: as 27 siglas. Código IBGE: sempre 7 dígitos. CEP: 8 dígitos, menos 48
  linhas com 7.
- Maior nome: 120 caracteres; maior nome fantasia: 83.

O que esses números fixam na configuração (`MIN_PROVIDERS`, trava de
remoção): [collector.md](collector.md).

## Regras do parser (`internal/parse`)

`parse.Parse(r io.Reader)` recebe o CSV já extraído do ZIP e devolve as
prestadoras **na ordem do arquivo**, cada uma com os seus serviços na ordem
em que aparecem, mais as contagens e os avisos.

### Leitura

- `encoding/csv` com `Comma = ';'`, `FieldsPerRecord = -1` (o número de
  campos é conferido por linha, abaixo) e `LazyQuotes = false`. Um erro de
  sintaxe do CSV (aspas quebradas) **recusa o arquivo**
  (`csv: linha N: ...`).
- O BOM no começo do arquivo é ignorado.
- Bytes que não formam UTF-8 válido viram `U+FFFD` antes de gravar.
- Espaços nas pontas de cada campo são removidos.
- **Valor ausente**: depois de tirar os espaços, `""`, `N/I`, `N/A` e `-`
  viram NULL (`.`, `0` e `S/N` ficam como vieram).
- O `N` dos avisos (`linha N: ...`) é a linha do CSV, contando o cabeçalho
  como linha 1.

### Cabeçalho

A primeira linha tem de ter **exatamente** as 24 colunas da tabela acima, com
esses nomes e nessa ordem (comparados depois de tirar o BOM e os espaços das
pontas). Qualquer diferença recusa o arquivo
(`cabeçalho inesperado na coluna K: "<veio>" (esperado "<nome>")` ou
`cabeçalho com K colunas (esperadas 24)`): o parser lê as colunas pela
posição, e uma coluna nova ou trocada gravaria dado no lugar errado.

### Linhas

| Caso | O que acontece | Conta em |
|---|---|---|
| coluna 1 = `CPF` (sem espaços nas pontas), com qualquer número de campos | ignorada, sem aviso (pessoa física: fora do escopo; nenhum outro campo é lido) | `RowsCPF` |
| número de campos ≠ 24 | descartada: `linha N: linha descartada: esperados 24 campos, vieram K` | `Skipped` |
| linha idêntica a uma anterior (as 24 colunas, já sem espaços) | ignorada, sem aviso | `Duplicates` |
| coluna 1 diferente de `CNPJ` e `CPF` | descartada: `linha N: linha descartada: tipo de identificação desconhecido: "<v>"` | `Skipped` |
| CNPJ sem exatamente 14 dígitos | descartada: `linha N: linha descartada: CNPJ inválido: "<v>"` | `Skipped` |
| código do serviço fora do formato `NNN - nome` | descartada: `linha N: linha descartada: serviço inválido: "<v>"` | `Skipped` |
| Fistel da notificação sem 11 dígitos | descartada: `linha N: linha descartada: Fistel da notificação inválido: "<v>"` | `Skipped` |
| Fistel da outorga presente e sem 11 dígitos | descartada: `linha N: linha descartada: Fistel da outorga inválido: "<v>"` | `Skipped` |
| data presente e que não é `DD/MM/AAAA` válida | descartada: `linha N: linha descartada: data inválida em "<coluna>": "<v>"` | `Skipped` |
| nome (coluna 3) ausente | descartada: `linha N: linha descartada: CNPJ <c> sem nome` | `Skipped` |
| tipo de entidade, tipo de outorga ou grupo do serviço (colunas 5, 6 e 10, `NOT NULL` no banco) ausente | descartada: `linha N: linha descartada: CNPJ <c> sem "<cabeçalho da coluna>"` | `Skipped` |

- As regras valem na ordem da tabela; a primeira que pegar decide (uma linha
  com 23 campos é descartada mesmo que seja cópia de outra).
- Toda linha que não é de CPF conta em `RowsCNPJ`, inclusive as descartadas
  (tipo desconhecido, campos a menos) e as cópias: `Rows = RowsCNPJ + RowsCPF`.
- Os dígitos verificadores do CNPJ **não** são conferidos: o cadastro é da
  Anatel e o BadBlock não corrige a fonte (em 2026-09-30 todos eram
  válidos).
- `Duplicates` e `RowsCPF` não entram no limite de 1% de descartadas; o
  limite é `Skipped` / `RowsCNPJ` ≤ 1%. Acima dele o arquivo é recusado:
  `<s> de <n> linhas de CNPJ descartadas (<x.x>%, limite 1.0%): formato mudou? primeiros avisos: <até 3 avisos>`.
- Arquivo vazio (`arquivo vazio`) ou só com o cabeçalho
  (`arquivo sem nenhuma linha de dados`) é recusado.
- Mesma chave natural de serviço (abaixo) com outros campos diferentes: vale
  a primeira ocorrência, com o aviso
  `linha N: serviço repetido (CNPJ <c>, Fistel <f>, código <nnn>); vale a primeira ocorrência`.
- Mesmo CNPJ com dados da prestadora diferentes dos da primeira linha dele:
  vale a primeira linha, com o aviso
  `linha N: CNPJ <c> com dados da prestadora diferentes da primeira linha; vale a primeira`
  (um aviso por CNPJ).

### Campos

| Campo | Regra |
|---|---|
| código do serviço | os 3 dígitos antes de ` - ` (`045`), como texto |
| nome do serviço | o que vem depois do primeiro ` - ` (`Serviço de Comunicação Multimídia`) |
| datas | `DD/MM/AAAA` → data (`2003-02-13`); `N/A` → NULL |
| código IBGE | 7 dígitos, o primeiro de 1 a 9 (o `CHECK` do banco é `1000000` a `9999999`) → inteiro; ausente → NULL sem aviso; outro valor → NULL, com o aviso `linha N: código IBGE inválido: "<v>"` (a linha vale) |
| UF | 2 letras maiúsculas; ausente → NULL sem aviso; outro valor → NULL, com o aviso `linha N: UF inválida: "<v>"` (a linha vale) |
| demais textos | como vieram (sem espaços nas pontas; ausente → NULL) |

Os campos da prestadora (colunas 3, 4 e 15 a 24) vêm da primeira linha
aceita do CNPJ; os avisos de código IBGE e UF saem só dela (um por CNPJ).

### Contagens devolvidas

`Rows` (linhas de dados), `RowsCNPJ`, `RowsCPF`, `Duplicates`, `Skipped`,
`Providers` (CNPJs distintos aceitos; no código, `len(Providers)`) e
`Services` (serviços distintos aceitos; no código, `Services()`), mais os
avisos (`Warnings`, os 50 primeiros, e `WarningCount()`, o total).

## Fixture

`apps/anatel/pst/collector/testdata/pst-sample.csv`: recorte real do CSV de
2026-09-30 (cabeçalho com o BOM, CRLF, as linhas na ordem do arquivo, cada
uma byte a byte como veio, menos o nome nas de CPF), com todas as linhas de
8 CNPJs e as 3 primeiras de CPF — 158 linhas de dados (155 de CNPJ, 3 de
CPF), 103 cópias exatas, 8 prestadoras e 52 serviços, nenhum aviso:

| CNPJ | Linhas | Por quê |
|---|---|---|
| `02558157000162` (Telefonica) | 145 (42 distintas) | prestadora grande, muitos serviços e muitas cópias exatas |
| `07807833000108` | 1 | SCM (045) com nome fantasia |
| `02883607000192` | 4 | duas outorgas (SIR e SIC), o mesmo Fistel de notificação sob as duas |
| `07157343000103` | 1 | `Dispensada de Outorga` (colunas 7 a 9 = `N/A`) |
| `01600200001110` | 1 | campo entre aspas com `;` (`": KM 11; GALPAO: 1;"`) e número `S.N.` |
| `01763250000146` | 1 | aspas dobradas (`"TRANS ""S"" LTDA"`) |
| `02839640000115` | 1 | `-`, vazio e `N/I` no endereço/contato; número `.` |
| `72063654000256` | 1 | complemento só com espaço; processo da notificação `N/I` |

- As linhas de CPF têm o nome da pessoa trocado por `PESSOA FISICA EXEMPLO`
  (o resto como veio: CPF mascarado pela Anatel, município e UF) — o
  repositório é público e o BadBlock não guarda pessoas físicas. Os CNPJs
  foram escolhidos entre empresas com nome de empresa e e-mail genérico
  (`contato@`, `financeiro@`...), sem nome de pessoa.

`testdata/pst-sample.zip` é o mesmo CSV num ZIP de uma entrada
(`prestadoras_servicos_telecomunicacoes.csv`, `deflate`) com **só** a data
MS-DOS `2026-09-30 06:15:08`, sem timestamp estendido (o caso em que o
`Modified` do Go vem errado; o ZIP real traz o NTFS). Recortes inválidos
(cabeçalho trocado, linha com 23 campos, ZIP sem CSV, ZIP com dois CSVs,
aspas quebradas, CSV mais antigo) são gerados pelos próprios testes.
