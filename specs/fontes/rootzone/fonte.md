# rootzone — fonte (`root.zone` da InterNIC)

De onde vem o dado, como é o arquivo e como o parser (`internal/parse`) o lê.
Tabelas em [dados.md](dados.md); detecção de mudança, aplicação e tempos em
[collector.md](collector.md).

## Arquivos publicados

| Item | Valor |
|---|---|
| URL | `https://www.internic.net/domain/root.zone` (padrão de `SOURCE_URL`) |
| Hash publicado | `https://www.internic.net/domain/root.zone.md5` (padrão de `SOURCE_MD5_URL`: `SOURCE_URL` + `.md5`): **33 bytes**, só o MD5 em hex minúsculo e um LF, sem nome de arquivo |
| Assinatura | `root.zone.sig` (PGP destacada) — **não conferida** (trabalho futuro, abaixo) |
| Comprimido | `root.zone.gz` (991.635 bytes) — **não usado**: o `.md5` confere o arquivo sem compressão, e o gzip do próprio HTTP já reduz o tráfego ao mesmo tamanho |
| Quem publica | InterNIC (IANA / PTI), a partir da zona gerada pela Verisign (o `RNAME` do SOA é `nstld.verisign-grs.com`) |
| Atualização | uma versão nova cerca de **2 vezes por dia**, com serial `AAAAMMDDNN` (ex.: `2026092901`); o `.md5` sai cerca de 1 min depois do arquivo (`Last-Modified` 18:37:00 e 18:38:00 em 2026-09-29) |

O mesmo diretório publica o `named.root` (fonte
[`roothints`](../roothints/README.md)); as âncoras DNSSEC vêm da IANA (fonte
[`rootanchors`](../rootanchors/README.md)).

### Servidor HTTP (medido em 2026-09-30)

Apache, HTTP/2. Resposta sem compressão:

```
HTTP/2 200
server: Apache
vary: Accept-Encoding
last-modified: Tue, 29 Sep 2026 18:37:00 GMT
etag: "225757-65ca377d2eb00"
content-length: 2250583
cache-control: max-age=420
content-type: text/plain; charset=UTF-8
```

- O ETag é o do Apache, `<tamanho em hex>-<mtime em µs, hex>`: `0x225757` =
  2.250.583 bytes.
- Com `Accept-Encoding: gzip` (o cliente HTTP do Go pede sozinho) a resposta
  vem comprimida — **991.536 bytes** em vez de 2.250.583 (~5 s contra ~12 s
  no download medido) — e o `mod_deflate` acrescenta `-gzip` ao ETag:
  `"225757-65ca377d2eb00-gzip"`.
- **O Apache não reconhece o próprio ETag `-gzip` no `If-None-Match`**
  (comportamento conhecido do `mod_deflate`): medido com gzip, o
  `If-None-Match: "…-gzip"` devolve **200** — mesmo junto com um
  `If-Modified-Since` válido, porque o `If-None-Match` manda —, enquanto o ETag
  sem o sufixo (`"225757-65ca377d2eb00"`) e o `If-Modified-Since` sozinho
  devolvem **304**. Uma lista com os dois
  (`If-None-Match: "…-gzip", "…"`) devolve 304. O coletor guarda o ETag como
  veio e envia a lista ([collector.md](collector.md#download-gzip-e-o-etag--gzip)).
- `cache-control: max-age=420`: um cache no caminho pode servir a versão
  anterior por até 7 minutos; é um dos casos da checagem de serial.
- O `.md5`: `etag: "21-65ca37b667200"`, `content-length: 33`, o mesmo
  `max-age=420`.

## Formato

Arquivo mestre de zona (RFC 1035, seção 5), em ASCII, **uma RR por linha**
terminada em LF, no formato:

```
<dono> <ttl> IN <tipo> <rdata>
```

O dono vem seguido de 1 a 3 TABs (alinhamento), os outros campos separados
por um TAB; o rdata tem espaços entre os seus campos. Todos os nomes são
**absolutos** (com o ponto final) e em minúsculas; não há `$ORIGIN`, `$TTL`,
comentários, parênteses (registro em várias linhas), linhas vazias nem dono
herdado. As RRs vêm na ordem canônica do DNSSEC: primeiro o ápice (`.`), e
cada TLD seguido do glue dos servidores de nome que ficam abaixo dele.

```
.			86400	IN	SOA	a.root-servers.net. nstld.verisign-grs.com. 2026092901 1800 900 604800 86400
.			518400	IN	NS	a.root-servers.net.
.			172800	IN	DNSKEY	257 3 8 AwEAAaz/tAm8yTn4Mfeh5eyI96WSVexTBAvkMgJzkKTOiW1vkIbzxeF3+/4RgWOq7HrxRixHlFlExOLAJr5e…
.			86400	IN	ZONEMD	2026092901 1 1 E80BFF012C499FB7532E91A1926E336362AE85DFD6715DB7390175BC8A800F10AE18369000A47566523182623B0FFF99
br.			172800	IN	NS	a.dns.br.
br.			86400	IN	DS	38298 13 2 9F2D4993F47B0F2751DE0007D70A2754EE532FE373761154D9EA7A8CB9D8EA18
br.			86400	IN	RRSIG	DS 8 1 86400 20261012170000 20260929160000 57780 . 2DqrJwByMpJ7ZybZOQNmh2UV…
br.			86400	IN	NSEC	bradesco. NS DS RRSIG NSEC
a.dns.br.		172800	IN	A	200.219.148.10
a.dns.br.		172800	IN	AAAA	2001:12f8:6:0:0:0:0:10
```

## Fatos medidos

Arquivo de 2026-09-29 (serial `2026092901`, `Last-Modified: Tue, 29 Sep 2026
18:37:00 GMT`, MD5 `0df326149210107d5dcb2cac740ca4b0`, SHA-256
`78e2a7f6f7c151534979196f781ba5ce7c5d9b8ce84eee84372d60fd86bda8ec`), medido
em 2026-09-30.

- **Tamanho**: 2.250.583 bytes (991.536 com gzip no HTTP), **24.925 linhas**,
  todas com exatamente 5 campos separados por TAB (o dono com 1 a 3 TABs); a
  última termina em LF. ASCII puro: sem `\r`, sem BOM, sem linha vazia. Maior
  linha: um RRSIG, rdata de 400 caracteres.
- **Classe**: `IN` em todas. **TTL**: 172.800 (19.135 linhas), 86.400
  (5.750) e 518.400 (40: os 13 NS da raiz, o glue dos 13 servidores raiz e o
  RRSIG desses NS).
- **Linhas por tipo**:

  | Tipo | Linhas | Donos | TTL |
  |---|---|---|---|
  | NS | 7.580 | 1.439 (a raiz e 1.438 TLDs) | 172.800 (518.400 na raiz) |
  | A | 5.940 | 5.924 | 172.800 (518.400 nos servidores raiz) |
  | AAAA | 5.649 | 5.647 | idem |
  | RRSIG | 2.794 | — | 86.400 (172.800 no do DNSKEY, 518.400 no do NS da raiz) |
  | DS | 1.517 | 1.351 TLDs | 86.400 |
  | NSEC | 1.439 | 1.439 (a raiz e cada TLD) | 86.400 |
  | DNSKEY | 4 | `.` (2 ZSK `256`, 2 KSK `257`, algoritmo 8) | 172.800 |
  | ZONEMD | 1 | `.` (esquema 1 SIMPLE, hash 1 SHA-384) | 86.400 |
  | SOA | 1 | `.` | 86.400 |

- **RRSIG** cobrem: DS (1.351, um por TLD assinado), NSEC (1.439), SOA,
  DNSKEY, NS da raiz e ZONEMD (1 cada). NS de TLD e glue não são assinados
  (são da zona filha).
- **TLDs**: **1.438** (donos de NS que não são a raiz), todos com um só
  rótulo; **151 IDN** (`xn--…`), 248 de duas letras ASCII (ccTLDs). 1.351 têm
  DS (1.190 com 1 DS, 156 com 2, 5 com 3); **87 sem DNSSEC** (ex.: `aq`,
  `bo`, `cu`, `zw`). NS por TLD: de 2 (14 TLDs) a 13 (3, entre eles o `com`);
  os mais comuns, 6 (575) e 4 (501).
- **Glue**: 5.926 nomes de servidor distintos nos NS e **todos** têm glue
  (nenhum sem A e sem AAAA; nenhum glue que não seja alvo de NS). O mesmo
  servidor serve vários TLDs (`ns01.trs-dns.net` serve 61) e o glue fica
  abaixo do TLD do nome, não do TLD servido (`ns.dns.br` serve `bo`, `cu`,
  `gh`, `gt`, `pa`, `pt`, `sv` e `uy`). 279 servidores não têm AAAA e 2 não
  têm A (`i.zdnscloud.cn` e `j.zdnscloud.com`, só IPv6); **18 TLDs sem
  nenhum servidor com IPv6**, 1.196 com todos.
- **IPv6 fora da forma canônica**: 5.595 dos 5.649 AAAA vêm com os zeros por
  extenso (`2001:12f8:6:0:0:0:0:10`); o parser grava a forma
  canônica da RFC 5952 (`2001:12f8:6::10`).
- Hex do DS e do ZONEMD em maiúsculas; base64 do DNSKEY numa palavra só;
  nomes todos em minúsculas; nenhuma linha repetida.

## Modelo

- **Guarda**: toda RR menos RRSIG, normalizada, em `rootzone_record`; um
  resumo por TLD em `rootzone_tld`; o SOA e as contagens por tipo da versão
  em `rootzone_run` ([dados.md](dados.md)).
- **RRSIG não são guardados** (só contados, em `rootzone_run.rrsigs` e
  `type_counts`): a zona é reassinada a cada publicação (no arquivo medido,
  2.793 dos 2.794 RRSIG têm início de validade em 2026-09-29 16:00 UTC,
  ~2h30 antes da publicação, e fim 13 dias depois; só o do DNSKEY, feito
  pela KSK, vai de 2026-09-19 a 2026-10-10), então, com o rdata na chave natural,
  as ~2,8 mil linhas seriam apagadas e inseridas 2 vezes por dia — 13% da
  tabela, o que também dispararia a trava de remoção de 5%. E
  quem precisa validar a zona deve usar o arquivo e o `.sig`, não esta
  cópia normalizada.
- **SOA e ZONEMD** entram em `rootzone_record` (a zona fica completa, menos
  as assinaturas): mudam a cada versão, 2 linhas removidas e 2 inseridas por
  publicação.

## Linhas (`parse.Parse`)

O parser lê o arquivo linha a linha (até o LF), linhas de até 64 KiB
(`MaxLineBytes`):

1. Tira o BOM UTF-8 da primeira linha, o `\r` do fim e o que vier depois de
   `;` (comentário). Linha vazia ou só com comentário é ignorada e não conta.
2. Linha que começa com espaço ou TAB (dono herdado), que começa com `$`
   (diretiva), com parênteses ou com texto que não é UTF-8 é **descartada**.
3. Os campos são separados por qualquer espaço em branco
   (`strings.Fields`): TAB ou espaço, um ou vários. São no mínimo 5:
   `dono`, `ttl`, `classe`, `tipo` e o rdata (o resto, que pode ter vários
   campos). Não há TTL nem classe opcionais: a linha tem de trazer os dois.
4. **Dono** e nomes do rdata: absolutos (terminam em `.`; nome relativo é
   descartado, porque não há `$ORIGIN`), rótulos de 1 a 63 caracteres
   `[a-z0-9_-]` depois de passar para minúsculas, nome de até 253.
   **Normalização**: minúsculas e **sem o ponto final**; a raiz é `.`
   (`BR.` → `br`, `a.dns.br.` → `a.dns.br`).
5. **TTL**: decimal de 0 a 2.147.483.647 (RFC 2181). **Classe**: `IN`
   (qualquer caixa); outra é descartada. **Tipo**: em qualquer caixa,
   gravado em maiúsculas; só os 9 da zona raiz (`SOA`, `NS`, `A`, `AAAA`,
   `DS`, `DNSKEY`, `NSEC`, `RRSIG`, `ZONEMD`) — outro tipo é descartado
   (um tipo novo na raiz aparece como aviso até a spec e o parser o
   aceitarem).
6. **Rdata**, por tipo (a forma normalizada é o que vai para
   `rootzone_record.rdata`):

   | Tipo | Regra | Normalizado |
   |---|---|---|
   | `SOA` | só no dono `.`; 7 campos: `mname rname serial refresh retry expire minimum` (nomes e 5 inteiros de 32 bits) | `a.root-servers.net nstld.verisign-grs.com 2026092901 1800 900 604800 86400` |
   | `NS` | dono `.` ou um TLD (um rótulo); 1 nome | `a.dns.br` |
   | `A` | 1 endereço IPv4 (`net/netip`) | `200.219.148.10` |
   | `AAAA` | 1 endereço IPv6, sem zona (`%eth0`) | forma canônica da RFC 5952: `2001:12f8:6::10` |
   | `DS` | dono é um TLD; `keytag algoritmo tipo-digest digest` (16, 8 e 8 bits); o digest em hex, que pode vir quebrado em vários campos | `38298 13 2 9F2D…EA18` (hex em maiúsculas, sem espaços) |
   | `DNSKEY` | `flags protocolo algoritmo chave` (16, 8 e 8 bits); a chave em base64, que pode vir quebrada | `257 3 8 AwEAAaz/…` (base64 sem espaços) |
   | `NSEC` | próximo nome e a lista de tipos (`[A-Z][A-Z0-9]*`, inclusive `TYPEnnn`) | `bradesco NS DS RRSIG NSEC`; o último TLD aponta para a raiz: `. NS RRSIG NSEC` |
   | `ZONEMD` | `serial esquema algoritmo digest` (32, 8 e 8 bits); digest em hex | `2026092901 1 1 E80B…FF99` |
   | `RRSIG` | 9 campos ou mais, o primeiro um tipo; **só contado** | — |

7. **RR repetida** — mesmo dono, tipo e rdata **depois de normalizar**
   (ex.: o mesmo IPv6 com e sem os zeros) — é descartada; vale a primeira
   (e o TTL dela).
8. Linhas descartadas geram o aviso
   `linha <n>: linha descartada: <motivo>` (textos longos cortados em 20 a
   60 bytes, com `…`). Exemplos de motivo: `dono relativo (sem o ponto
   final): "br"`, `TTL inválido "-1"`, `classe "CH" (só IN é aceita)`,
   `tipo "TXT" desconhecido`, `4 campos, esperado <dono> <ttl> IN <tipo>
   <rdata>: "…"`, `diretiva não suportada "$ORIGIN ."`, `parênteses (registro
   em várias linhas) não são aceitos`, `linha sem dono (começa com espaço;
   dono herdado não é aceito)`, `a.dns.br A: endereço inválido "2001:db8::1"`,
   `br DS: digest em hex inválido "XYZ"`, `. DNSKEY: chave em base64
   inválida "***"`, `NS de dns.br, que não é a raiz nem um TLD`, `DS de .,
   que não é um TLD`, `SOA fora da raiz (br)`, `br NSEC: tipo inválido "D-S"
   no bitmap`, `br RRSIG: 4 campos no rdata, esperado 9 ou mais`,
   `a.root-servers.net A 198.41.0.4 repetido; vale a primeira ocorrência`.
9. Avisos que não descartam linha: `TLD <tld>: punycode inválido (<erro>);
   unicode fica igual ao nome` e `DS de <dono> sem NS (não é uma
   delegação)`; o coletor acrescenta o de serial igual
   ([collector.md](collector.md#arquivo-mais-antigo-e-serial-igual)).
10. O arquivo inteiro é **recusado** (o coletor grava `parser: <mensagem>`):

    | Caso | Mensagem |
    |---|---|
    | erro de leitura | `leitura: <erro>` |
    | a última linha sem LF (arquivo cortado) | `linha <n> sem fim de linha no fim do arquivo: arquivo cortado?` |
    | linha acima de 64 KiB | `linha <n> com mais de 65536 bytes` |
    | segundo SOA | `linha <n>: mais de um SOA no arquivo` |
    | nenhuma linha de dados | `arquivo sem nenhum registro` |
    | mais de 1% (`MaxSkippedRatio`) das linhas com conteúdo descartadas | `<d> de <n> linhas descartadas (<x.y>%, limite 1.0%): formato mudou? primeiros avisos: <até 3, separados por "; ">` |
    | nenhum SOA válido na raiz | `arquivo sem o SOA da raiz` |
    | nenhum NS da raiz | `arquivo sem os NS da raiz` |

11. Avisos: os 50 primeiros (`MaxWarnings`) ficam no dataset e o total é
    contado ([collector.md](collector.md#recusas-erros-e-logs)).

No arquivo real: 0 descartes, 0 avisos.

## TLDs (`rootzone_tld`)

Cada dono de NS que não é a raiz é um TLD (a regra 6 já descartou NS com
dono de mais de um rótulo). Para cada um:

| Campo | Regra |
|---|---|
| `tld` | o dono, normalizado (`br`, `xn--p1ai`) |
| `tld_unicode` | o rótulo `xn--` decodificado de punycode (RFC 3492, `parse.Punycode`, feito à mão — a biblioteca padrão não o exporta); igual a `tld` nos ASCII e quando o punycode é inválido (com aviso) |
| `nameservers` | quantidade de NS do TLD |
| `nameservers_ipv4` / `nameservers_ipv6` | quantos desses servidores têm pelo menos um A / AAAA na zona (glue) |
| `ds_records` | quantidade de DS do TLD (0 = sem DNSSEC) |

A decodificação foi conferida nos 151 IDN do arquivo de 2026-09-29 contra o
codec `punycode` do Python: nenhuma diferença (ex.: `xn--p1ai` → `рф`,
`xn--fiqs8s` → `中国`, `xn--11b4c3d` → `कॉम`).

**Resultado no arquivo real**: 22.131 RRs guardadas, 2.794 RRSIG contados,
1.438 TLDs (151 IDN, 1.351 com DS, 1.420 com algum servidor IPv6).

## Trabalho futuro

- **`.sig`**: conferir a assinatura PGP do `root.zone.sig` com a chave da
  IANA; exige uma biblioteca OpenPGP (dependência nova, decisão da sessão
  principal) e a chave publicada.
- **ZONEMD** (RFC 8976): o digest SHA-384 da zona inteira permitiria
  conferir o arquivo sem depender do `.md5` do mesmo servidor. Exige
  serializar todas as RRs (menos o ZONEMD e seu RRSIG) na forma canônica
  **em wire format** (RFC 4034, seção 6), ordenadas — um codificador de
  RDATA por tipo. Fica fora do parser atual; o valor publicado é guardado em
  `rootzone_record` (tipo `ZONEMD`).
- Validação DNSSEC dos RRSIG com as DNSKEY (e das DNSKEY com a âncora da
  fonte `rootanchors`): mesma exigência de wire format.

## Fixture

`apps/rootzone/collector/testdata/root-zone-sample.zone`: **212 linhas
reais**, copiadas sem alteração do arquivo de 2026-09-29 (serial
`2026092901`; cada linha existe nele byte a byte), na ordem do arquivo;
17.715 bytes. Não é um trecho contínuo: são o ápice inteiro e 7 TLDs
completos (todas as linhas com o TLD como dono) com o glue de todos os seus
servidores, onde quer que ele esteja no arquivo.

| Parte | Por quê |
|---|---|
| ápice: SOA, 13 NS, 4 DNSKEY, ZONEMD, NSEC da raiz e os RRSIG deles; glue dos 13 `*.root-servers.net` (TTL 518.400) | SOA e serial, NS da raiz, KSK/ZSK, ZONEMD |
| `aaa` | primeiro TLD do arquivo (o NSEC da raiz aponta para ele) |
| `bo` | sem DS; um servidor sem AAAA; usa `ns.dns.br` (glue sob outro TLD) |
| `br` | glue dentro do próprio TLD, IPv6 fora da forma canônica |
| `com` | 13 NS (o máximo), glue sob `net` (`*.gtld-servers.net`) |
| `top` | 2 DS; servidores só com IPv6 (`i.zdnscloud.cn`, `j.zdnscloud.com`) |
| `xn--p1ai` | IDN (`рф`) |
| `zw` | último TLD: o NSEC aponta para a raiz (`. NS RRSIG NSEC`); sem DS |

Conteúdo por tipo: NS 61, A 59, AAAA 55, RRSIG 17, NSEC 8, DS 6, DNSKEY 4,
SOA 1, ZONEMD 1. Resultado esperado (`TestParseSample`): 195 RRs guardadas,
17 RRSIG, 7 TLDs:

| `tld` | `tld_unicode` | NS | com IPv4 | com IPv6 | DS |
|---|---|---|---|---|---|
| `aaa` | `aaa` | 6 | 6 | 6 | 1 |
| `bo` | `bo` | 4 | 4 | 3 | 0 |
| `br` | `br` | 6 | 6 | 6 | 1 |
| `com` | `com` | 13 | 13 | 13 | 1 |
| `top` | `top` | 8 | 6 | 3 | 2 |
| `xn--p1ai` | `рф` | 6 | 6 | 6 | 1 |
| `zw` | `zw` | 5 | 5 | 5 | 0 |

Os casos de recusa e de descarte não têm arquivo próprio: os testes os
montam a partir do recorte (uma linha ruim acrescentada, o SOA removido ou
duplicado, `IN` trocado por `CH` em todas, o arquivo cortado ao meio etc.;
[collector.md](collector.md#testes)).

Para refazer o recorte de um arquivo novo (mesmos TLDs, glue incluído):

```bash
curl -fsS -o /tmp/root.zone https://www.internic.net/domain/root.zone
perl -e '
  my %t = map {$_=>1} (".", qw(aaa. bo. br. com. top. xn--p1ai. zw.));
  open F, "/tmp/root.zone"; my @l = <F>; my %o = %t;
  for (@l) { my @f = split /\t+/; chomp $f[4]; $o{$f[4]} = 1 if $f[3] eq "NS" && $t{$f[0]} }
  for (@l) { my @f = split /\t+/; print if $o{$f[0]} }
' > apps/rootzone/collector/testdata/root-zone-sample.zone
```

Depois, ajuste os valores esperados nos testes (serial, contagens, DS) e
nesta seção. Mudou o formato da fonte: meça o arquivo real, atualize o
recorte, os testes do parser e esta spec juntos.
