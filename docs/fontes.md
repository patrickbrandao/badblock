# Fontes de dados

As 17 fontes que o registry-sync acompanha, com o que cada uma traz e as
particularidades encontradas nos arquivos reais (medições de 2026-09-28). O
catálogo em código fica em
[`apps/registry-sync/internal/source/catalog.go`](../apps/registry-sync/internal/source/catalog.go);
o estado de cada uma aparece em `GET /v1/meta/sources`.

## Delegações dos RIRs (`rir-*`)

| Id | URL primária | Espelho (fallback) | Registros |
|---|---|---|---|
| `rir-afrinic` | ftp.afrinic.net/pub/stats/afrinic/delegated-afrinic-extended-latest | ftp.lacnic.net | 19.786 |
| `rir-apnic` | ftp.apnic.net/stats/apnic/delegated-apnic-extended-latest | ftp.lacnic.net | 190.268 |
| `rir-arin` | ftp.arin.net/pub/stats/arin/delegated-arin-extended-latest | ftp.lacnic.net | 203.051 |
| `rir-lacnic` | ftp.lacnic.net/pub/stats/lacnic/delegated-lacnic-extended-latest | ftp.ripe.net, ftp.apnic.net | 97.301 |
| `rir-ripencc` | ftp.ripe.net/pub/stats/ripencc/delegated-ripencc-extended-latest | ftp.lacnic.net | 260.769 |

Formato `registry|cc|type|start|value|date|status|opaque-id`:

- `type`: `asn` (value = quantidade de ASNs), `ipv4` (value = quantidade de
  endereços) ou `ipv6` (value = tamanho do prefixo).
- `status`: `allocated`, `assigned`, `available`, `reserved`.
- `opaque-id`: identifica o titular dentro do RIR (numérico na LACNIC, hash
  hex na ARIN/APNIC/AFRINIC, UUID no RIPE); vazio em available/reserved.
- Duas versões de cabeçalho: `2` (AFRINIC, RIPE) e `2.3` (demais). O APNIC
  começa com 27 linhas de comentário.
- Data vazia ou `00000000` em registros antigos e reservados; `ZZ` como país
  de recursos sem país.
- 3.787 registros IPv4 não formam um CIDR (ARIN 2.765, RIPE 970, AFRINIC 52):
  são divididos, ex.: `62.122.208.0` + 1.280 → `/22` + `/24`.
- Registros IPv4 menores que /24 (253, o menor é /29) e IPv6 maiores que /48
  (21, até /64): por isso o cache da API tem exceções por bloco.
- Nenhum recurso aparece em dois RIRs ao mesmo tempo, exceto durante
  transferências.
- Todos publicam `<url>.md5` (formato BSD `MD5 (arq) = hash` ou GNU
  `hash  arq`), conferido a cada download.
- O cabeçalho e as linhas `summary` sempre batem com os registros: qualquer
  divergência é tratada como arquivo corrompido.
- Os espelhos atrasam: o do APNIC na LACNIC estava um dia atrás.

## IANA

| Id | Arquivo | Uso |
|---|---|---|
| `iana-asn-16`, `iana-asn-32` | as-numbers-1.csv, as-numbers-2.csv | Blocos de ASN por RIR (cadeia de /v1/asn) |
| `iana-ipv4` | ipv4-address-space.csv | Os 256 blocos /8: RIR, legado, reservado |
| `iana-ipv6` | ipv6-unicast-address-assignments.csv | Blocos IPv6 unicast por RIR |
| `iana-special-ipv4`, `iana-special-ipv6` | iana-ipv*-special-registry-1.csv | Special-purpose: privado, documentação, loopback... (flag bogon) |
| `iana-special-asn` | special-purpose-as-numbers.csv | ASNs 0, 23456, privados, documentação |
| `iana-rdap-asn`, `iana-rdap-ipv4`, `iana-rdap-ipv6` | data.iana.org/rdap/*.json | Servidor RDAP de cada RIR (RFC 9224) |

Particularidades:

- A coluna RDAP dos CSVs às vezes traz duas URLs coladas
  (`https://rdap.arin.net/registryhttp://rdap.arin.net/registry`).
- Os special-purpose têm notas de rodapé (`192.0.0.0/24 [2]`, `False [1]`),
  vários blocos numa célula (`192.0.0.170/32, 192.0.0.171/32`) e
  `Globally Reachable` vazio ou `N/A` em alguns blocos.
- `as-numbers-2.csv` repete a faixa 0–65535 como "See Sub-registry 16-bit AS
  numbers"; a linha é ignorada.
- Blocos legados: `45/8` é "Administered by ARIN", mas a LACNIC delega partes
  dele (`45.171.60.0/22`). O RIR de cada bloco da IANA sai da coluna WHOIS.
- `www.iana.org` responde `Last-Modified` (sem ETag); `data.iana.org`, os dois.

## NIC.br (`nicbr`)

`ftp.registro.br/pub/numeracao/origin/nicbr-asn-blk-latest.txt`, formato
`AS<n>|nome|documento|bloco|bloco|...` em UTF-8.

- 9.134 ASNs, 13.037 blocos IPv4 e 8.954 IPv6. Todos os 21.991 blocos
  coincidem com registros da LACNIC depois de divididos em CIDR.
- Documento: 9.110 CNPJs e 24 identificadores estrangeiros de 8 dígitos;
  nenhum CPF.
- 21 linhas usam ASN registrado fora da LACNIC/BR (AS8075 Microsoft, AS174
  Cogent, AS2914 NTT...): o nome e o CNPJ são do titular brasileiro dos
  blocos, não do ASN.

## Nomes de AS (`asnames`)

`ftp.ripe.net/ripe/asnames/asn.txt`: 122.591 linhas `<asn> <handle> - <nome>, <CC>`.

- ~39 mil linhas não têm o separador ` - ` (`28 DFVLR-SYS Deutsches Zentrum...`).
- 3.199 têm handle com espaços antes do separador (padrão da AFRINIC, como
  `SEACOM Limited - SEACOM Limited`); cortar no primeiro ` - ` preserva o nome.

## Atualização e uso

O registry-sync confere cada fonte a cada hora (`SYNC_INTERVAL`); os RIRs
publicam uma vez por dia, a IANA raramente muda. Os arquivos são públicos e
redistribuíveis; a API informa origem e data de cada fonte em
`/v1/meta/sources`.
