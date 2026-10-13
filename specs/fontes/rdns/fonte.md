# Fonte `rdns`: o DNS reverso dos prefixos cadastrados

De onde vem o dado: a lista de prefixos (derivada do `cgibr` e estática), a
consulta PTR, como cada resposta vira um estado, a normalização dos nomes,
os fatos medidos e as fixtures. Dono: sub-agente `collector-rdns`. As
tabelas estão em [dados.md](dados.md); o que o coletor faz (derivação, fila,
cliente DNS, taxa, tempo real), em [collector.md](collector.md).

Diferente das outras fontes, não há arquivo publicado: o dado é **medido**
pelo próprio coletor, consultando resolvedores recursivos
([../../padroes/sonda.md](../../padroes/sonda.md)).

## Prefixos

### Derivados do `cgibr`

- Origem: os blocos **IPv4** de `cgibr_prefix`, cada um com o ASN da linha
  (`cgibr_asn.asn`), lidos quando a versão do dataset do `cgibr` (o `uuid` da
  última linha `status = 1` de `cgibr_run`) muda
  ([../cgibr/dados.md](../cgibr/dados.md#tabelas)).
- Cada bloco é **quebrado em /24**: um /20 vira 16 linhas, um /24 fica como
  está. A unidade de varredura é o /24 porque é a unidade de delegação da
  zona reversa (`<c>.<b>.<a>.in-addr.arpa`) e cabe com folga no lease de uma
  instância ([collector.md](collector.md#varredura-de-um-prefixo)).
- Bloco mais longo que /24 (nenhum em 2026-10-08) não vira unidade: é
  ignorado com aviso em `rdns_run.warnings`.
- Dois blocos do `cgibr` que se sobreponham (nenhum em 2026-10-08) dão o
  mesmo /24 duas vezes: vale o ASN do bloco mais específico.
- O IPv6 do `cgibr` não entra: a varredura cobre só IPv4 (um /64 não se
  varre endereço a endereço).

### Estáticos

Linhas `source = 'static'` gravadas pelo sistema de gestão do administrador,
direto no banco ([dados.md](dados.md#o-que-o-administrador-grava)): blocos
que não estão no `cgibr` (de outro RIR, de cliente, privados com
`asn = 0`). São sempre varridas, qualquer que seja `SOURCES`.

### Regra da sonda

A fila só pega prefixos **IPv4 de /22 a /24**, ativos. A regra fica só no
software: o banco aceita qualquer `cidr`, e uma linha fora dela (IPv6, /21,
/25…) nunca é varrida — é contada no `/rdns/meta` e no aviso de log
`prefixos fora da regra ignorados`. Um /22 tem 1.024 endereços, o máximo que
cabe no lease com folga.

## Consulta

Uma consulta por endereço, **todos** os endereços do prefixo (inclusive o
`.0` e o `.255`, que também podem ter PTR):

| Item | Valor |
|---|---|
| Nome | `dns.ReverseAddr(ip)`: `3.2.160.200.in-addr.arpa.` para `200.160.2.3` |
| Tipo / classe | `PTR` / `IN` |
| Cabeçalho | `RD` ligado (o resolvedor é recursivo); ID aleatório |
| EDNS0 | presente, tamanho UDP **1.232** bytes; `DO` desligado (o BadBlock não valida DNSSEC no reverso) |
| Transporte | UDP; **TCP só quando a resposta UDP vem truncada** (`TC`) |
| Para quem | os resolvedores recursivos de `DNS_SERVERS`, em rodízio ([collector.md](collector.md#cliente-dns)) |

Os resolvedores são do operador de cada host (sem padrão): a consulta sai da
rede dele e o resolvedor faz a recursão e o cache. O BadBlock não fala com os
servidores autoritativos.

## Respostas e estados

| Resposta | Estado gravado | Linha em `rdns_ptr` |
|---|---|---|
| `NOERROR` com um ou mais `PTR` na seção de resposta | `ptr` | gravada (ou atualizada) com os nomes |
| `NOERROR` sem `PTR` (NODATA) ou `NXDOMAIN` | `no_ptr` | **nenhuma**: apaga a existente |
| `SERVFAIL` em dois servidores | `servfail` | gravada, **salvo** se o IP já tem PTR |
| `REFUSED` (e `FORMERR`, `NOTIMP` e qualquer outro `RCODE`) até esgotar as tentativas | `refused` | idem |
| sem resposta em `DNS_TIMEOUT` (ou erro de rede) até esgotar as tentativas | `timeout` | idem |

- **Uma falha nunca apaga um PTR**: o IP com nome continua com o nome até um
  `NXDOMAIN`/NODATA ou um nome novo. Falha sobre falha só regrava se o estado
  mudou (`servfail` → `timeout`).
- Quem decide o número de tentativas, o rodízio e o que conta como falha do
  servidor: [collector.md](collector.md#cliente-dns).
- **CNAME (RFC 2317)**: a delegação de um bloco menor que /24 usa CNAME do
  nome reverso para outra zona; o resolvedor recursivo segue a cadeia e a
  resposta traz o CNAME e o PTR. O coletor pega **todos os `PTR` da seção de
  resposta**, qualquer que seja o dono (o nome do CNAME de destino), e ignora
  os CNAME.
- Resposta cuja pergunta não é a enviada (outro nome ou tipo) é descartada
  como erro de rede.

## Nomes

- Forma de apresentação da `miekg/dns` (o `Ptr` do registro): bytes fora do
  ASCII imprimível saem como `\DDD` (decimal, três dígitos), e o ponto dentro
  de um rótulo como `\.`.
- Letras ASCII em **minúsculas**; **sem o ponto final** (`registro.br.` →
  `registro.br`). Um PTR que aponta para a raiz (`.`) fica `.`.
- Nomes repetidos na mesma resposta contam uma vez.
- `names` = todos os nomes, em **ordem de bytes** (`sort.Strings`); `name` =
  o primeiro deles. Exemplo real (2026-10-10, pelo `1.1.1.1`): `200.160.2.5`
  responde `mailx.registro.br.` e `mailexploder.registro.br.`, gravados como
  `names = {mailexploder.registro.br, mailx.registro.br}` e
  `name = mailexploder.registro.br`.

## Fatos medidos

### Prefixos (arquivo do NIC.br de 2026-10-08)

- 13.045 blocos IPv4, com **79,0 M** endereços.
- Nenhum bloco menor que /24 e nenhum contido em outro.
- Quebrados em /24: **308.529** unidades.
- Por ASN: mediana de **4** /24; o maior, AS26599, com **43.656** /24.
- Volta completa: com **1** coletor a 256 consultas/s, ~**86 h**; com **4**,
  ~**21 h**. A meta de 24 h por prefixo pede **4 coletores** a 256 req/s (ou
  taxa maior): com menos, a fila só segue a ordem, o mais antigo primeiro.
- Arquivo de 2026-10-10 (a primeira derivação do `collector-rdns`, no stack
  local): 13.048 blocos IPv4, os mesmos **308.529** /24, nenhum bloco mais longo
  que /24.

### Respostas (amostra de 1.500 IPs, 2026-10-09, pelo `1.1.1.1`)

| Medida | Valor |
|---|---|
| com PTR | 70% |
| `NXDOMAIN` | 29% |
| `SERVFAIL` ou sem resposta | 0,8% |
| com mais de um PTR | 0,13% |
| tamanho do nome | 33 bytes em média, 62 no máximo |

Projeção para os 79,0 M endereços: ~55 M linhas em `rdns_ptr` (os 70% com
PTR mais as falhas), ~**10 GB** com o índice
([dados.md](dados.md#tamanho)).

### Exemplos reais (2026-10-10, pelo `1.1.1.1`)

O bloco `200.160.0.0/20` do AS22548 (NIC.br) vira 16 /24; no
`200.160.2.0/24`:

| IP | Resposta | Estado |
|---|---|---|
| `200.160.2.1` | `ae1-0.ar1.vr.registro.br.` | `ptr` |
| `200.160.2.2` | `NXDOMAIN` | `no_ptr` |
| `200.160.2.3` | `registro.br.` | `ptr` |
| `200.160.2.5` | `mailx.registro.br.`, `mailexploder.registro.br.` | `ptr`, dois nomes |

## Fixtures

Não há arquivo da fonte para recortar: os testes sobem um **servidor DNS
falso** (`dns.Server` da `miekg/dns`) em `127.0.0.1`, numa porta livre, UDP e
TCP, com as respostas em `testdata/zone.txt`, um arquivo de zona (RFC 1035)
que o servidor falso serve como um resolvedor recursivo (segue o CNAME;
nome ausente = `NXDOMAIN`; nome sem PTR = NODATA):

- **reais**, consultados em 2026-10-10 pelo `1.1.1.1`: os PTR do
  `200.160.2.0/24` acima (um nome, dois nomes; o `.0`, o `.2` e o `.255` sem
  PTR), o `177.85.0.1` e um **CNAME RFC 2317** de verdade —
  `2.200.62.64.in-addr.arpa.` → `2.subnet0.200.62.64.in-addr.arpa.` →
  `ipv6.he.net.` (Hurricane Electric);
- **sintéticos**, em `192.0.2.0/24` (TEST-NET-1), para o que o DNS real não
  garante numa amostra pequena: letras maiúsculas, bytes fora do ASCII
  (`caf\195\169`), o mesmo nome duas vezes (só a caixa muda), ponto dentro
  de um rótulo, PTR para a raiz, NODATA, e, por linhas de controle
  `;! <nome> <comportamento>`, `SERVFAIL`, `REFUSED`, silêncio (prazo),
  resposta truncada que só vem inteira por TCP e pergunta trocada.

Os testes ainda sobem servidores falsos que respondem `SERVFAIL`, `REFUSED`
ou nada a qualquer pergunta (o rodízio e a saúde dos servidores). Os testes
que usam cada caso: [collector.md](collector.md#testes-específicos).
