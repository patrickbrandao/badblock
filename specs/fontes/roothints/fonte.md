# roothints — fonte (`named.root` da InterNIC)

De onde vem o dado, como é o arquivo e como o parser (`internal/parse`) o lê.
Tabelas em [dados.md](dados.md); detecção de mudança, aplicação e tempos em
[collector.md](collector.md).

## Arquivo

| Item | Valor |
|---|---|
| URL | `https://www.internic.net/domain/named.root` (padrão de `SOURCE_URL`) |
| Quem publica | InterNIC (IANA/ICANN), junto com a zona raiz (`root.zone`, fonte [`rootzone`](../rootzone/README.md)) |
| Conteúdo | os 13 servidores raiz do DNS (`a` a `m.root-servers.net`), cada um com um endereço IPv4 (registro `A`) e um IPv6 (`AAAA`) |
| Hash publicado | `https://www.internic.net/domain/named.root.md5` (padrão de `SOURCE_MD5_URL` = `SOURCE_URL` + `.md5`) |
| Assinatura | `named.root.sig` (PGP destacada, binária, 95 bytes): **não é verificada** (ver [Trabalho futuro](#trabalho-futuro)) |
| Cópia | `https://www.internic.net/domain/named.cache`: o mesmo arquivo, byte a byte (medido em 2026-09-30); não é usada |
| Atualização do conteúdo | poucas vezes por ano (o cabeçalho diz `last update: September 24, 2026`) |
| Regravação no servidor | a cada publicação da zona raiz, cerca de 2 vezes por dia, **mesmo sem o conteúdo mudar** (abaixo) |

### Servidor HTTP (medido em 2026-09-30)

Apache, HTTP/2. `http://` responde 200 também (sem redirecionar); o padrão
é `https://`. Há um cache no caminho: as respostas trazem `Age` (de 0 a
~400 s, variando entre requisições) e `cache-control: max-age=420`, então
uma verificação pode receber a cópia anterior do arquivo por alguns minutos
depois de uma publicação.

```
HTTP/2 200
server: Apache
last-modified: Tue, 29 Sep 2026 18:37:00 GMT
content-length: 3315
etag: "cf3-65ca377d2eb00"
cache-control: max-age=420
content-type: text/plain; charset=UTF-8
```

- O ETag é forte, `<tamanho em hex>-<mtime em µs, hex>` do Apache:
  `0xcf3` = 3.315 bytes e `0x65ca377d2eb00` µs = 2026-09-29 18:37:00 UTC (o
  mesmo instante do `Last-Modified`).
- `If-None-Match` com o ETag e `If-Modified-Since` com o `Last-Modified`
  devolvem **304**.
- O `Last-Modified` (e com ele o ETag) muda a cada publicação da zona raiz,
  cerca de 2 vezes por dia: em 2026-09-30 o arquivo trazia
  `Last-Modified: Tue, 29 Sep 2026 18:37:00 GMT` e o cabeçalho
  `last update: September 24, 2026`. Por isso o GET condicional sozinho
  baixaria o arquivo duas vezes por dia sem mudança nenhuma; a checagem mais
  barata é o `.md5` ([collector.md](collector.md#checagens-de-mudança)).

### Compressão e o ETag `-gzip` (medido em 2026-09-30)

- O `named.root` sai **sem compressão**: com `Accept-Encoding: gzip` (curl
  em HTTP/2 e HTTP/1.1, `http://` e `https://`, com e sem uma query que fura
  o cache, e o transporte do Go, que pede gzip sozinho) a resposta é a mesma,
  3.315 bytes, sem `Content-Encoding` nem `Vary`, ETag `"cf3-65ca377d2eb00"`.
- A cópia `named.cache`, no mesmo servidor e com o mesmo conteúdo, sai
  **com** gzip (821 bytes, `content-encoding: gzip`, `vary: Accept-Encoding`)
  e o ETag ganha o sufixo `-gzip` do `mod_deflate`: `"cf3-65ca377d2eb00-gzip"`.
  O `root.zone` (fonte [`rootzone`](../rootzone/fonte.md)) também. Ou seja, a
  compressão é ligada por arquivo no servidor e pode passar a valer para o
  `named.root`.
- Com gzip, o Apache **não reconhece** o próprio ETag `-gzip` no
  `If-None-Match` (medido no `named.cache`, com `Accept-Encoding: gzip`):

  | Requisição | Resposta |
  |---|---|
  | `If-None-Match: "cf3-65ca377d2eb00"` | 304 |
  | `If-None-Match: "cf3-65ca377d2eb00-gzip"` | **200** (às vezes 304, quando responde o cache do caminho) |
  | `If-None-Match: "cf3-65ca377d2eb00-gzip", "cf3-65ca377d2eb00"` | 304 |
  | só `If-Modified-Since` | 304 |
  | `If-None-Match` com `-gzip` e `If-Modified-Since` | **200** (com `If-None-Match` presente, o `If-Modified-Since` é ignorado) |

  O `named.root` responde igual às linhas sem `-gzip` (304) e 200 às com
  `-gzip`. Por isso o coletor envia as duas formas do ETag quando o guardado
  tem o sufixo ([collector.md](collector.md#checagens-de-mudança)); com o
  ETag de hoje, sem sufixo, nada muda.

### Arquivo `.md5` (medido em 2026-09-30)

```
HTTP/2 200
last-modified: Tue, 29 Sep 2026 18:38:00 GMT
content-length: 33
etag: "21-65ca37b667200"
```

- 33 bytes: **só o hash**, 32 dígitos hex minúsculos e `\n`, sem nome de
  arquivo (`d0732825a760fee171258b4890ca5243`). Confere com o MD5 do
  `named.root` do mesmo momento.
- É regravado um minuto depois do `named.root` (18:38 contra 18:37): uma
  verificação nesse minuto pode ver o arquivo novo com o `.md5` velho. Como o
  conteúdo quase nunca muda, o hash continua igual e nada acontece; se mudar,
  a conferência recusa e a próxima verificação resolve
  ([collector.md](collector.md#validações-antes-de-aplicar)).
- Leitura tolerante, como no padrão: só o hash, formato BSD
  (`MD5 (named.root) = <hash>`) ou GNU (`<hash>  named.root`), maiúsculas ou
  minúsculas, `\r\n`; olha só a primeira linha não vazia, e o que não for um
  hash de 32 dígitos (página HTML de erro, linha SHA-256, hash curto) é erro
  (`sem hash MD5 reconhecível`). Até 4 KiB.

## Formato

Arquivo de zona do BIND (RFC 1035), ASCII, linhas terminadas em LF, **sem
LF depois da última linha**. Um cabeçalho em comentários (`;`) e um bloco por
servidor, com um comentário, a linha `NS` da raiz e os registros `A` e `AAAA`
do servidor; termina em `; End of file`. Início e fim do arquivo de
2026-09-30:

```
;       This file holds the information on root name servers needed to
;       initialize cache of Internet domain name servers
...
;       last update:     September 24, 2026
;       related version of root zone:     2026092401
;
; FORMERLY NS.INTERNIC.NET
;
.                        3600000      NS    A.ROOT-SERVERS.NET.
A.ROOT-SERVERS.NET.      3600000      A     198.41.0.4
A.ROOT-SERVERS.NET.      3600000      AAAA  2001:503:ba3e::2:30
;
; FORMERLY NS1.ISI.EDU
...
; OPERATED BY WIDE
;
.                        3600000      NS    M.ROOT-SERVERS.NET.
M.ROOT-SERVERS.NET.      3600000      A     202.12.27.33
M.ROOT-SERVERS.NET.      3600000      AAAA  2001:dc3::35
; End of file
```

Linha de dados: `<nome> <ttl> <tipo> <dado>`, sem a classe (`IN` fica
implícita), nomes absolutos (com o ponto final) em maiúsculas.

## Fatos medidos

Arquivo baixado em 2026-09-30 (`Last-Modified: Tue, 29 Sep 2026 18:37:00 GMT`,
cabeçalho `last update: September 24, 2026`, serial da zona `2026092401`):

- **Tamanho**: 3.315 bytes, 92 linhas (91 LF: a última, `; End of file`,
  não termina em LF) — 53 de comentário e 39 de dados. MD5 `d0732825a760fee171258b4890ca5243`, SHA-256
  `18f27fc4801c9a16337047cb2e18419a42623cfd15f53b73cb37c98f496e7730`.
- ASCII puro: sem byte acima de 0x7E, sem `\r`, sem tab, sem BOM. 22 linhas
  de comentário terminam em espaço (`; `, `; FORMERLY NS.INTERNIC.NET `…).
- **13 blocos**, `A` a `M`, nesta ordem; cada um com 1 `NS`, 1 `A` e 1 `AAAA`
  — 39 linhas de dados. Todos os TTLs são `3600000` (1000 horas, ~41,7 dias).
- Comentário de cada bloco: `FORMERLY <nome antigo>` (A a I: `NS.INTERNIC.NET`,
  `NS1.ISI.EDU`, `C.PSI.NET`, `TERP.UMD.EDU`, `NS.NASA.GOV`, `NS.ISC.ORG`,
  `NS.NIC.DDN.MIL`, `AOS.ARL.ARMY.MIL`, `NIC.NORDU.NET`) ou
  `OPERATED BY <operador>` (J a M: `VERISIGN, INC.`, `RIPE NCC`, `ICANN`,
  `WIDE`).
- Os endereços, um por servidor e família, todos unicast global e distintos:

  | Letra | IPv4 | IPv6 |
  |---|---|---|
  | a | 198.41.0.4 | 2001:503:ba3e::2:30 |
  | b | 170.247.170.2 | 2801:1b8:10::b |
  | c | 192.33.4.12 | 2001:500:2::c |
  | d | 199.7.91.13 | 2001:500:2d::d |
  | e | 192.203.230.10 | 2001:500:a8::e |
  | f | 192.5.5.241 | 2001:500:2f::f |
  | g | 192.112.36.4 | 2001:500:12::d0d |
  | h | 198.97.190.53 | 2001:500:1::53 |
  | i | 192.36.148.17 | 2001:7fe::53 |
  | j | 192.58.128.30 | 2001:503:c27::2:30 |
  | k | 193.0.14.129 | 2001:7fd::1 |
  | l | 199.7.83.42 | 2001:500:9f::42 |
  | m | 202.12.27.33 | 2001:dc3::35 |

- **Cabeçalho**: `last update:` com a data em inglês (`September 24, 2026`) e
  `related version of root zone:` com o serial da zona raiz no formato
  `AAAAMMDDnn` (`2026092401`).

## Modelo

Cada linha `. NS <servidor>` vira um servidor (uma linha de
`roothints_server`), e os registros `A` e `AAAA` com o nome dele viram as
colunas `ipv4` e `ipv6`. O comentário do bloco fica em `note`. O cabeçalho
(data e serial) vai para `roothints_run`, e o serial serve para ignorar um
arquivo mais antigo que o aplicado. Colunas:
[dados.md](dados.md#mapeamento-da-fonte-para-as-colunas).

## Regras do parser

`parse.Parse` lê o arquivo linha a linha (`bufio.Scanner`, linha de até
1 MiB). São só 13 servidores e 39 linhas de dados: **nenhuma linha é
descartada** — qualquer uma que não bata recusa o arquivo inteiro (um
descarte já seria 1 de 39, bem acima do limite de 1% do padrão). O coletor
grava `parser: <mensagem>`.

1. **Linhas**: tira o BOM UTF-8 da primeira linha; linha com UTF-8 inválido
   ou NUL recusa o arquivo. Tira os espaços das pontas (o que tolera `\r` de
   fim de linha, tab e espaço no fim do comentário). Linha vazia é ignorada.
2. **Comentário** (começa com `;`): o texto é o resto sem os espaços das
   pontas.
   - Antes da primeira linha de dados, `last update: <data>` e
     `related version of root zone: <serial>` (sem diferenciar maiúsculas)
     são o **cabeçalho**; o que vem depois da última delas e antes do
     primeiro `NS` é o comentário do primeiro bloco.
   - `; End of file` (sem diferenciar maiúsculas, com ou sem espaço depois do
     `;`) marca o fim.
3. **Cabeçalho**: validado na primeira linha de dados (e no fim, se não houver
   nenhuma). A data é lida no formato `January 2, 2006` e vira a data (UTC)
   `last_update`; o serial é decimal de 0 a 4294967295 (serial de zona, 32
   bits sem sinal) e vira `zone_serial`.
4. **Linha de dados**: o que vem depois de um `;` no meio da linha é
   comentário e sai; o resto se divide por espaços em branco em
   `<nome> <ttl> [IN] <tipo> <dado>` — a classe `IN` é opcional (sem
   diferenciar maiúsculas); qualquer outra quantidade de campos, ou outra
   classe, recusa. O TTL é decimal de 0 a 2147483647 (RFC 2181; sem
   unidades como `1h`). Os nomes têm de ser absolutos (terminar em `.`) e são
   guardados em minúsculas e sem o ponto final. O tipo não diferencia
   maiúsculas e só pode ser `NS`, `A` ou `AAAA`.
5. **`NS`**: o dono tem de ser a raiz (`.`), e o alvo, depois de
   normalizado, `<letra>.root-servers.net` com uma letra de `a` a `z`
   (regex `^[a-z]\.root-servers\.net$`); a letra é o primeiro rótulo. Cria o
   servidor, com o comentário do bloco: as linhas de comentário não vazias
   desde a linha de dados anterior (no primeiro bloco, desde o fim do
   cabeçalho), unidas por um espaço (vazio = sem comentário, `NULL`).
6. **`A` / `AAAA`**: o dono tem de ser um servidor com `NS` **antes**; o dado
   é lido por `netip.ParseAddr` e tem de ser IPv4 no `A`, IPv6 no `AAAA`
   (IPv4 mapeado `::ffff:a.b.c.d` e endereço com zona `%eth0` não valem) e
   **unicast global** (`IsGlobalUnicast` e não `IsPrivate`: loopback,
   link-local, multicast, `0.0.0.0` e as faixas privadas recusam). Um
   servidor tem no máximo um `A` e um `AAAA`, e nenhum endereço se repete
   entre servidores.
7. **No fim**: pelo menos um servidor; a última linha não vazia tem de ser
   `; End of file` (dados depois dela, ou a falta dela, é arquivo truncado);
   todo servidor tem ao menos um endereço. Servidor só com IPv4 ou só com
   IPv6 é aceito, **com aviso**, e a outra coluna fica `NULL`.

Recusas (mensagem depois de `parser: `; `linha <n>: ` na frente das que são
de uma linha):

| Caso | Mensagem |
|---|---|
| UTF-8 inválido ou NUL | `linha <n>: UTF-8 inválido ou NUL` |
| linha acima de 1 MiB | `leitura: bufio.Scanner: token too long` |
| sem as linhas do cabeçalho | `cabeçalho sem a linha "last update:"` / `cabeçalho sem a linha "related version of root zone:"` (com `linha <n>: ` quando notado na primeira linha de dados) |
| data ou serial ilegível | `cabeçalho: data de "last update:" inválida: "<v>"` / `cabeçalho: serial de "related version of root zone:" inválido: "<v>"` |
| campos errados, classe diferente de `IN` | `linha <n>: esperava <nome> <ttl> [IN] <tipo> <dado>: "<linha>"` |
| TTL | `linha <n>: TTL inválido "<v>"` |
| nome relativo | `linha <n>: nome sem o ponto final "<nome>"` |
| outro tipo | `linha <n>: tipo "<tipo>" não esperado (só NS, A e AAAA)` |
| `NS` fora da raiz | `linha <n>: NS de "<dono>": só a raiz "." pode ter NS no named.root` |
| alvo fora do padrão | `linha <n>: servidor "<nome>" fora do padrão <letra>.root-servers.net` |
| `NS` repetido | `linha <n>: servidor <nome> repetido` |
| `A`/`AAAA` sem `NS` antes | `linha <n>: registro <A\|AAAA> de <nome> sem a linha NS dele antes` |
| endereço ilegível ou da família errada | `linha <n>: endereço <IPv4\|IPv6> inválido em <nome>: "<v>"` |
| endereço não global | `linha <n>: endereço <ip> de <nome> não é unicast global` |
| endereço repetido | `linha <n>: endereço <ip> repetido (já é de <nome>)` |
| segundo `A`/`AAAA` | `linha <n>: segundo registro <A\|AAAA> de <nome>` |
| nenhum `NS` | `arquivo sem nenhum servidor (linha ". NS")` |
| sem `; End of file` no fim | `arquivo sem a linha "; End of file" no fim: arquivo truncado?` |
| servidor sem endereço | `servidor <nome> sem endereço (nem A nem AAAA)` |

Avisos (em `roothints_run.warnings`; no arquivo de 2026-09-30, nenhum):
`servidor <nome> sem IPv4 (registro A)` e `servidor <nome> sem IPv6
(registro AAAA)`. O limite de 50 avisos do padrão vale, mas não é
alcançável com 13 servidores.

Por que tão estrito: o arquivo é pequeno, muda pouco e é consumido por
resolvedores; uma mudança de formato é rara e merece uma pessoa olhando
(spec, fixture e parser juntos), e um arquivo servido pela metade ou trocado
por uma página de erro nunca pode virar remoção de servidores.

## Fixture

`apps/roothints/collector/testdata/`:

| Arquivo | Conteúdo |
|---|---|
| `named.root` | o arquivo **inteiro** de 2026-09-30, sem alteração (3.315 bytes, MD5 `d0732825…5243`, `last update: September 24, 2026`, serial `2026092401`) |
| `named.root.md5` | o `.md5` publicado junto (33 bytes, só o hash e `\n`) |

Os casos de recusa e de tolerância não têm arquivo próprio: os testes os
derivam do `named.root` com uma troca de texto cada (sem cabeçalho, serial
menor, `NS` fora da raiz, `A` antes do `NS`, endereço privado ou repetido,
truncado entre blocos e no meio de uma linha, CRLF, BOM, classe `IN`,
minúsculas…), listados em `TestParseRejects` e `TestParseTolerated`
([collector.md](collector.md#testes)).

Mudou o formato da fonte: meça o arquivo real, troque a fixture (o arquivo
inteiro do dia e o `.md5`), ajuste os testes do parser, os valores esperados
(MD5, SHA-256, serial, endereços) e esta spec juntos.

## Trabalho futuro

- **Assinatura PGP** (`named.root.sig`): hoje a integridade vem do `.md5`
  publicado pelo mesmo servidor (protege contra corte e corrida, não contra
  um servidor comprometido). Verificar a assinatura exigiria uma chave
  pública fixa e uma biblioteca OpenPGP, que não está entre as dependências
  permitidas ([../../projeto/convencoes.md](../../projeto/convencoes.md#go)):
  decisão a registrar antes.
