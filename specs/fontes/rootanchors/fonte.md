# Fonte `rootanchors`: `root-anchors.xml` da IANA

De onde vem o dado, o formato do arquivo, os fatos medidos, as regras do
parser (`internal/parse`) e as fixtures. Dono: sub-agente
`collector-rootanchors`. As tabelas estão em [dados.md](dados.md); o que o
coletor faz com o arquivo (checagens, conferência do hash, mínimo, aplicação),
em [collector.md](collector.md).

## URLs

Todas na pasta `https://data.iana.org/root-anchors/`:

| O quê | Arquivo | Usado pelo coletor |
|---|---|---|
| Âncoras de confiança | `root-anchors.xml` | sim (`SOURCE_URL`) |
| Hashes publicados | `checksums-sha256.txt` | sim (`SOURCE_SHA256_URL`; só a linha de `root-anchors.xml`) |
| Assinatura CMS/S-MIME (detached) do XML, feita pela ICANN | `root-anchors.p7s` | não ([abaixo](#assinatura-root-anchorsp7s-trabalho-futuro)) |
| Cadeia de certificados da ICANN para conferir o `.p7s` | `icannbundle.pem` | não |

A IANA **não** publica `root-anchors.xml.sha256`: o hash do XML é uma linha do
`checksums-sha256.txt`, que tem uma linha por arquivo da pasta. Por isso a URL
padrão do hash não é `SOURCE_URL` + `.sha256` como no
[padrão](../../padroes/coletor.md#uma-verificação), e sim o
`checksums-sha256.txt` da pasta de `SOURCE_URL`
([collector.md](collector.md#opções)).

## Servidor HTTP (medido em 2026-09-30)

| Item | `root-anchors.xml` | `checksums-sha256.txt` |
|---|---|---|
| Servidor | Cloudflare (`server: cloudflare`, `cf-cache-status: HIT`, `age` de ~71 mil s) | idem |
| `Cache-Control` | `max-age=86400` (24 h) | `max-age=86400` |
| `Content-Type` | `text/xml` | `text/plain; charset=UTF-8` |
| `Last-Modified` | `Tue, 05 Nov 2024 19:23:41 GMT` | `Thu, 28 May 2026 22:26:08 GMT` |
| `ETag` | `W/"745-6262f56bfe940-br"` | `W/"f8-652e836fc76c6-br"` |
| Tamanho | 1.861 bytes (1.063 com gzip) | 248 bytes |

- O ETag é **fraco** e o mesmo com e sem `Accept-Encoding: gzip` (o sufixo
  `-br` é da compressão na origem; `745` hex = 1.861 bytes). Reenviado como
  veio em `If-None-Match`, o Cloudflare responde **304**; `If-Modified-Since`
  com o `Last-Modified` também dá 304.
- Com `Accept-Encoding: gzip` (o que o transporte do Go manda sozinho) a
  resposta vem com `Content-Encoding: gzip`; o Go descomprime de forma
  transparente, e hash, tamanho e parser valem para o XML descomprimido.
- O `Last-Modified` do `checksums-sha256.txt` (2026-05-28) é mais novo que o do
  XML: a IANA regrava o arquivo de hashes sem mexer no XML. O coletor só
  compara a linha do XML.

### `checksums-sha256.txt`

Formato GNU (`sha256sum`), três linhas, dois espaços entre hash e nome
(conteúdo real de 2026-09-30):

```
18ce7215812d1a2cad8d9d4d3d7c26f7235a9b5ec6f0c1e214e15230fd4f9e24  icannbundle.pem
644e0e22842c6b7a57fda13d286941fd7449a179f0a3763578fa9a251d307dd1  root-anchors.p7s
3ccaab38830025ee0a0f6c1f25769427544f81ea2865aa860468f3ef5278b908  root-anchors.xml
```

A linha de `root-anchors.xml` confere com o SHA-256 do arquivo baixado. A
**primeira** linha é de outro arquivo: pegar "o primeiro hash do texto", como
faz o `collector-cgibr`, daria o hash do `icannbundle.pem`. Como o coletor lê
a linha certa: [collector.md](collector.md#checagens-de-mudança).

## Formato do arquivo

XML UTF-8 com um elemento raiz `TrustAnchor` e um `KeyDigest` por chave
(RFC 9718; o arquivo real, com as chaves públicas encurtadas):

```xml
<?xml version="1.0" encoding="UTF-8"?>
<TrustAnchor id="0C05FDD6-422C-4910-8ED6-430ED15E11C2" source="http://data.iana.org/root-anchors/root-anchors.xml">
    <Zone>.</Zone>
    <KeyDigest id="Kjqmt7v" validFrom="2010-07-15T00:00:00+00:00" validUntil="2019-01-11T00:00:00+00:00">
        <KeyTag>19036</KeyTag>
        <Algorithm>8</Algorithm>
        <DigestType>2</DigestType>
        <Digest>49AAC11D7B6F6446702E54A1607371607A1A41855200FD2CE1CDDE32F24E8FB5</Digest>
    </KeyDigest>
    <KeyDigest id="Klajeyz" validFrom="2017-02-02T00:00:00+00:00">
        <KeyTag>20326</KeyTag>
        <Algorithm>8</Algorithm>
        <DigestType>2</DigestType>
        <Digest>E06D44B80B8F1D39A95C0B0D7C65D08458E880409BBC683457104237C7F8EC8D</Digest>
        <PublicKey>AwEAAaz/tAm8yTn4Mfeh5eyI96WSVexTBAvkMgJzkKTOiW1vkIbzxeF3…74bU=</PublicKey>
        <Flags>257</Flags>
    </KeyDigest>
    <KeyDigest id="Kmyv6jo" validFrom="2024-07-18T00:00:00+00:00">
        <KeyTag>38696</KeyTag>
        <Algorithm>8</Algorithm>
        <DigestType>2</DigestType>
        <Digest>683D2D0ACB8C9B712A1948B27F741219298D0A450D612C483AF444A4C0FB2B16</Digest>
        <PublicKey>AwEAAa96jeuknZlaeSrvyAJj6ZHv28hhOKkx3rLGXVaC6rXTsDc449/c…3PYc=</PublicKey>
        <Flags>257</Flags>
    </KeyDigest>
</TrustAnchor>
```

| Onde | O quê | Obrigatório |
|---|---|---|
| `TrustAnchor@id` | identificador do arquivo (UUID em maiúsculas) | sim |
| `TrustAnchor@source` | URL de origem declarada (em `http://`) | não |
| `Zone` | a zona das âncoras: `.` (a raiz) | sim, uma vez |
| `KeyDigest@id` | identificador da chave (7 caracteres, ex.: `Kmyv6jo`) | sim, único |
| `KeyDigest@validFrom`, `@validUntil` | validade em RFC 3339 (`2010-07-15T00:00:00+00:00`); sem `validUntil`, sem data de fim | `validFrom` sim; `validUntil` não |
| `KeyTag` | key tag do DNSKEY (decimal, 16 bits) | sim |
| `Algorithm` | algoritmo DNSSEC (decimal, 8 bits; 8 = RSA/SHA-256) | sim |
| `DigestType` | tipo do digest do DS (decimal, 8 bits; 2 = SHA-256) | sim |
| `Digest` | digest do DS em hexadecimal | sim |
| `PublicKey` | chave pública do DNSKEY em base64 | não (junto com `Flags`) |
| `Flags` | flags do DNSKEY (decimal; 257 = zone key + SEP) | não (junto com `PublicKey`) |

## Fatos medidos (2026-09-30)

- 1.861 bytes, SHA-256
  `3ccaab38830025ee0a0f6c1f25769427544f81ea2865aa860468f3ef5278b908`, fim de
  linha LF, sem BOM; `Last-Modified` 2024-11-05.
- **3 chaves**, todas algoritmo 8 e `DigestType` 2:

  | `id` | Key tag | Validade | `PublicKey`/`Flags` |
  |---|---|---|---|
  | `Kjqmt7v` | 19036 | 2010-07-15 a 2019-01-11 (a KSK-2010, aposentada) | não |
  | `Klajeyz` | 20326 | desde 2017-02-02 (a KSK-2017) | sim, `257` |
  | `Kmyv6jo` | 38696 | desde 2024-07-18 (a KSK-2024) | sim, `257` |

- A chave aposentada continua no arquivo, com `validUntil`: a lista só cresce
  numa rolagem. É por isso que uma chave que some é tratada como inesperada
  ([collector.md](collector.md#validação-de-um-arquivo-novo)).
- Recalculados a partir de `PublicKey` e `Flags`, o key tag e o digest SHA-256
  da 20326 e da 38696 batem com os publicados.

## Regras do parser (`internal/parse`)

`parse.Parse` lê o arquivo inteiro com `encoding/xml` e devolve o `id`, o
`source` e a zona do `TrustAnchor`, as chaves **na ordem do arquivo** e os
avisos. O arquivo é pequeno e crítico: **não há registro descartado** — o
limite de 1% do padrão fica, na prática, em zero, e qualquer regra abaixo que
falhe recusa o arquivo inteiro (o coletor grava `parser: <erro>`).

### Leitura

- O decodificador é o do `encoding/xml` em modo estrito: XML mal formado ou
  truncado é `XML: <erro do decodificador>`; arquivo vazio (ou só com a
  declaração) é `XML: arquivo vazio`; raiz que não é `TrustAnchor` é
  `XML: expected element type <TrustAnchor> but have <...>`.
- Depois de `</TrustAnchor>` só podem vir espaços, comentários e instruções de
  processamento: `XML: elemento <x> depois de </TrustAnchor>`,
  `XML: texto depois de </TrustAnchor>`.
- Os nomes são comparados sem namespace. Espaços nas pontas de atributos e
  textos são removidos.

### `TrustAnchor`

| Regra | Erro |
|---|---|
| atributo `id` presente e não vazio | `sem o atributo id no TrustAnchor` |
| exatamente um `Zone` | `sem o elemento Zone no TrustAnchor`, `<n> elementos Zone no TrustAnchor` |
| `Zone` é `.` | `zona "<z>": só a raiz (".") é aceita` |
| ao menos um `KeyDigest` | `nenhum KeyDigest no arquivo` |

O atributo `source` não é validado (entra como veio, vazio vira NULL).

### `KeyDigest`

Os erros saem como `KeyDigest <id>: <erro>` (ou `KeyDigest <n>: ...`, pela
posição a partir de 1, quando falta o `id`):

| Regra | Erro |
|---|---|
| atributo `id` presente e não vazio | `sem o atributo id` |
| `id` único no arquivo | `id repetido` |
| `validFrom` presente, em RFC 3339 (`time.RFC3339`; guardado em UTC) | `sem o atributo validFrom`, `validFrom inválido: "<v>"` |
| `validUntil`, quando presente, em RFC 3339 (presente e vazio é inválido) | `validUntil inválido: "<v>"` |
| `validUntil` ≥ `validFrom` (iguais passam) | `validUntil <u> anterior a validFrom <f>` |
| `KeyTag`, `Algorithm`, `DigestType`, `Digest` presentes uma vez cada | `sem o elemento <X>`, `elemento <X> repetido` |
| `KeyTag` decimal de 0 a 65535; `Algorithm` e `DigestType` de 0 a 255 | `KeyTag inválido: "<v>"` (idem `Algorithm`, `DigestType`) |
| `DigestType` é 1 (SHA-1), 2 (SHA-256) ou 4 (SHA-384) | `tipo de digest <n> não suportado (DigestType aceitos: 1, 2, 4)` |
| `Digest` hexadecimal (espaços internos removidos, maiúsculas ou minúsculas; guardado em maiúsculas) | `digest não é hexadecimal: "<v>"` |
| tamanho do `Digest` = o do tipo (40, 64 ou 96 dígitos) | `digest com <n> dígitos hex; DigestType <t> pede <m>` |
| `PublicKey` e `Flags` vêm juntos ou nenhum vem (e no máximo uma vez) | `elemento PublicKey sem Flags`, `elemento Flags sem PublicKey` |
| `Flags` de 0 a 65535, com o bit Zone Key (256) ligado | `Flags inválido: "<v>"`, `flags <n> sem o bit Zone Key (256)` |
| `PublicKey` em base64 padrão (espaços e quebras de linha removidos; guardada assim) | `PublicKey: não é base64 válido` |
| **key tag recalculado** = `KeyTag` | `key tag <t> não bate com o calculado da PublicKey (<c>)` |
| **digest recalculado** = `Digest` | `digest não bate com o calculado da PublicKey (<hex>)` |

Recálculo, quando há `PublicKey` (as chaves sem ela, como a 19036, só passam
pelas regras de formato):

1. RDATA do DNSKEY (RFC 4034, 2.1): `flags` (2 bytes, big-endian) |
   protocolo `3` (1 byte) | `algorithm` (1 byte) | chave pública decodificada.
2. Key tag: o algoritmo do apêndice B da RFC 4034 sobre esse RDATA (soma dos
   bytes em pares de 16 bits, com o vai-um dobrado); para o algoritmo 1
   (RSA/MD5), a regra própria do apêndice B.1.
3. Digest do DS (RFC 4034, 5.1.4): o hash do tipo (`crypto/sha1`,
   `crypto/sha256` ou SHA-384 de `crypto/sha512`) sobre o nome do dono em wire
   format — a raiz é o byte `0x00` — seguido do RDATA.

O recálculo garante que o DS publicado corresponde à chave publicada no mesmo
arquivo (um digest ou key tag corrompido, ou uma chave trocada, recusa o
arquivo). Não prova quem publicou: isso seria a assinatura
[`.p7s`](#assinatura-root-anchorsp7s-trabalho-futuro).

`Algorithm` não é restrito a uma lista: um algoritmo novo numa rolagem futura
passa, desde que o recálculo bata.

### Avisos

Elementos e atributos que o parser não conhece são ignorados, com aviso — uma
extensão do formato não recusa o arquivo, mas aparece em
`rootanchors_run.warnings`:

- `TrustAnchor: atributo desconhecido <nome> ignorado`
- `TrustAnchor: elemento desconhecido <<nome>> ignorado`
- `KeyDigest <id>: atributo desconhecido <nome> ignorado`
- `KeyDigest <id>: elemento desconhecido <<nome>> ignorado`

O parser guarda os **50** primeiros (`MaxWarnings`) e conta o total
(`WarningCount()`). O arquivo de 2026-09-30 não gera nenhum aviso.

## Assinatura `root-anchors.p7s` (trabalho futuro)

A IANA publica `root-anchors.p7s`, uma assinatura CMS (PKCS#7) destacada do
XML, feita pela ICANN, com a cadeia em `icannbundle.pem`. Conferi-la provaria
a origem do arquivo, mas exige ler CMS/PKCS#7, que a biblioteca padrão do Go
não tem, e as dependências permitidas
([../../projeto/convencoes.md](../../projeto/convencoes.md#go)) não incluem.
Hoje o coletor confia no HTTPS de `data.iana.org`, no hash do
`checksums-sha256.txt` (mesmo servidor) e na coerência interna DS ↔ DNSKEY.
Adotar a conferência pede uma decisão registrada em
[../../projeto/decisoes.md](../../projeto/decisoes.md) (dependência nova ou
um leitor CMS mínimo próprio).

## Fixtures (`testdata/`)

| Arquivo | Bytes | SHA-256 | O quê |
|---|---|---|---|
| `root-anchors.xml` | 1.861 | `3ccaab38…5278b908` | o arquivo real inteiro, baixado em 2026-09-30, sem ajuste |
| `checksums-sha256.txt` | 248 | `6581557b…a53ae951` | o arquivo de hashes real do mesmo dia |
| `root-anchors-bad-digest.xml` | 1.861 | `cc302804…837f5998` | o real com o último dígito do `Digest` da 20326 trocado (`…EC8D` → `…EC8E`) |
| `root-anchors-bad-keytag.xml` | 1.861 | `3eac8591…5928ef9b` | o real com `<KeyTag>20326</KeyTag>` → `20327` |
| `root-anchors-bad-zone.xml` | 1.864 | `e37bf635…bf7f5ba2` | o real com `<Zone>com.</Zone>` |
| `root-anchors-truncated.xml` | 1.200 | `f0f51dd6…1f9ef973` | os 1.200 primeiros bytes do real (corta no meio da tag de abertura do `KeyDigest` da 38696) |

O parser lê de `root-anchors.xml` **3 chaves** (19036 sem `PublicKey`, 20326 e
38696 com `Flags` 257), o `id` e o `source` do `TrustAnchor` e **zero
avisos**; as variantes são recusadas com `digest não bate`,
`key tag 20327 não bate com o calculado da PublicKey (20326)`,
`zona "com."` e `XML: ...`. Os outros casos de recusa são montados nos
testes trocando um trecho do arquivo real
([collector.md](collector.md#testes-específicos)).

### Mudou o formato

Atualize juntos, no mesmo trabalho: esta spec, as fixtures (um arquivo real
novo e as variantes derivadas dele) e os testes do parser
(`internal/parse/parse_test.go`), além dos números que dependem da fixture em
`internal/collector/collector_test.go` (SHA-256, 3 chaves, `id` do
`TrustAnchor`), `internal/fetch/fetch_test.go` (o hash do
`checksums-sha256.txt`) e `internal/store/store_integration_test.go`.
