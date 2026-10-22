# Fonte `registrobr/rdapnum`: o RDAP do registro.br

De onde vem o dado: os alvos (derivados do `cgibr` e estáticos), as
consultas RDAP, o formato das respostas, a resposta truncada, o limite do
servidor, as regras do parser e as fixtures. Dono: sub-agente
`collector-registrobr-rdapnum`. As tabelas estão em [dados.md](dados.md) e
[dados-processados.md](dados-processados.md); o que o coletor faz, em
[collector.md](collector.md).

Não há arquivo publicado: cada objeto é uma consulta HTTP que o próprio
coletor faz ([../../../padroes/sonda.md](../../../padroes/sonda.md)).

## Alvos

| Tipo | Origem | Consulta |
|---|---|---|
| `asn` | cada `cgibr_asn.asn` | `GET /autnum/<asn>` |
| `prefix` | cada `cgibr_prefix.prefix`, IPv4 e IPv6 | `GET /ip/<prefixo>` (ex.: `/ip/200.225.48.0/21`, `/ip/2804:8ae0::/32`) |

- Lidos quando a versão do dataset do `cgibr` (o `uuid` da última linha
  `status = 1` de `cgibr_run`) muda ([../../cgibr/dados.md](../../cgibr/dados.md#tabelas)),
  como no `rdns`.
- **Estáticos**: ASNs e blocos que o administrador grava direto no banco
  ([dados.md](dados.md#o-que-o-administrador-grava)); sempre consultados,
  qualquer que seja `SOURCES`.
- Os blocos que a resposta de um ASN lista em `links` (`rel` `related`) **não
  viram alvo**: em 2026-10-21 eram os mesmos blocos do `cgibr` (AS61610: os
  três; AS22548: `200.160.0.0/20` e `2001:12ff::/32`). IPs soltos, entidades
  e domínios reversos são da busca profunda, que não é esta fonte
  ([../README.md](../README.md#conjuntos)).

## Consulta

| Item | Valor |
|---|---|
| URL | `RDAP_URL` (`https://rdap.registro.br`) + `/autnum/<asn>` ou `/ip/<prefixo>` |
| Método | `GET`, HTTP/2 quando o servidor aceita (é o caso em 2026-10-21) |
| Cabeçalhos | `Accept: application/rdap+json`; `User-Agent` do [padrão de nomes](../../../projeto/estrutura.md#nomes) |
| Saída | pelo proxy de `PROXY_URL`, se houver ([collector.md](collector.md#proxy)) |
| Resposta | `200` com `Content-Type: application/rdap+json` (~2,7 a 3,8 KB nos exemplos); `404` com corpo vazio para objeto que o registro.br não tem |

Medido em 2026-10-21: as respostas levaram de 70 a 115 ms, sem cabeçalho de
limite (`Retry-After`, `RateLimit-*`) e sem `429` em nenhuma das ~350
consultas, nem quando truncadas.

## Respostas

### ASN (`/autnum/61610`, 2026-10-21)

`objectClassName` `autnum`, `handle` (`"61610"`), `startAutnum` e
`endAutnum`, `name` (`ASN61610`), `type` (`DIRECT ALLOCATION`), `country`,
`events` (`registration`, `last changed`), `links` (`self` e um `related`
por bloco do titular), `notices`, `rdapConformance`
(`["rdap_level_0","nicbr_level_0"]`), `port43` e `entities`:

- o **titular** (`roles` `["registrant"]`): `handle` = o CNPJ só com dígitos
  (`35980592000130`), `vcardArray` com `kind` `org`, `fn` (`ELEA DATA
  CENTERS`) e `adr` (só o país), `publicIds`
  (`[{"type":"cnpj","identifier":"35.980.592/0001-30"}]`),
  `legalRepresentative`, `events`, e os **contatos do titular** em
  `entities` (ex.: o `administrative`);
- os **contatos do objeto** (`administrative`, `abuse`): `handle` (ex.:
  `ROBEL308`), `vcardArray` com `kind` `individual`, `fn`, `email`, `lang` e
  `adr`, e `events`. Um contato com vários papéis aparece uma vez, com todos
  em `roles` (`["administrative","abuse"]`).

ASN registrado fora do Brasil que o `cgibr` liga a titular brasileiro também
responde 200: `/autnum/174` devolveu, em 2026-10-21, o titular brasileiro
(2.697 bytes).

### Bloco (`/ip/200.225.48.0/24`, 2026-10-21)

O servidor responde o **bloco registrado que contém** o pedido: o `/24`
voltou como `200.225.48.0/21`. Campos: `objectClassName` `ip network`,
`handle` (`200.225.48.0/21`), `startAddress`, `endAddress`, `ipVersion`
(`v4`/`v6`), `name` (um número interno, `462539`), `type` (`ALLOCATED`),
`country`, `status` (`["active"]`), `parentHandle` (quando há),
`nicbr_autnum` (o ASN do bloco, `61610`), `cidr0_cidrs`
(`[{"length":"21","v4prefix":"200.225.48.0"}]`), `events`, `entities`
(titular com os contatos dele e os contatos do bloco — `technical`,
`abuse`) e `nicbr_reverseDelegations`: uma por faixa delegada, com
`startAddress`, `endAddress`, `nameservers` (`ldhName` e os `events`
`delegation check`, com `status` como `["ns aa"]`, e `last correct
delegation check`), `events` e `secureDNS.delegationSigned`.

`/ip/2804:8ae0::/32` (IPv6) tem a mesma forma, com `ipVersion` `v6`,
`cidr0_cidrs` com `v6prefix` e uma delegação reversa de
`2804:8ae0:4000::` a `2804:8ae0:40ff:ffff:ffff:ffff:ffff:ffff`. Um IP solto
(`/ip/187.87.29.10`) devolve o bloco que o contém (`187.87.28.0/22`); um
bloco fora do registro.br (`/ip/8.8.8.0/24`) responde 404.

### Aviso com o IP de quem consultou

Toda resposta traz em `notices` a política de uso e, como **segunda linha
de `description`**, a hora e o IP de quem consultou:

```json
{"title": "Use and Privacy Policy",
 "description": ["Copyright (c) Nic.br - Use of this data is governed by the Use and Privacy Policy. Distribution, commercialization, reproduction, and use for advertising or similar purposes are expressly prohibited.",
                 "2026-10-21T17:36:01-03:00 - <ip de saída>"]}
```

- O coletor lê dessa linha (`^(\S+) - (\S+)$`, hora RFC 3339 e IP) a **hora
  do servidor** e o **IP de saída** de cada resposta
  ([dados.md](dados.md#registrobr_rdapnum_response)); é assim que ele vê se
  o proxy está mesmo trocando de IP. Linha ausente ou diferente: as duas
  colunas ficam NULL (log `debug`), sem recusar a resposta.
- Por isso o `notices` inteiro fica **fora do hash** que decide se a
  resposta mudou ([dados.md](dados.md#resposta-igual)): ele muda a cada
  consulta.
- O aviso diz que distribuição e reprodução são proibidas; a decisão de
  servir os dados pela API pública mesmo assim é a #42 de
  [../../../projeto/decisoes.md](../../../projeto/decisoes.md).

## Resposta truncada

Quem consulta demais recebe a resposta **truncada**, ainda com `200`, e
marcada pelo `remarks` de nível superior do RFC 9083 (§10.2.1):

```json
"remarks": [{"type": "object truncated due to server policy",
             "description": ["Some of the data in this object has been removed."]}]
```

Diferença medida em 2026-10-21 entre a resposta completa e a truncada do
AS61610: saem **todos os `email`** dos vCards, **todos os `adr`** e o
**`publicIds`** (o CNPJ) do titular. Ficam `fn`, `kind`, `lang`,
`handle`, `roles`, `events`, `legalRepresentative`, `links`, o bloco e as
delegações reversas.

Regra do coletor (`complete` em [dados.md](dados.md#registrobr_rdapnum_response)):

- **truncada** = algum item de `remarks` de nível superior com `type`
  `object truncated due to server policy` (comparação exata, sem caixa);
- **completa** = 200 com JSON válido e sem essa marca.

Não há outra heurística (contar e-mails, por exemplo): um contato pode não
ter e-mail, e a marca é o que o servidor diz. A truncada é guardada, mas
**nunca** vira o dado processado ([collector.md](collector.md#truncada-e-erros)).

## Limite do servidor

Medido em 2026-10-21, de um IP só, sempre `/autnum/61610`:

| Ensaio | Resultado |
|---|---|
| ~100 consultas seguidas a ASNs inexistentes (404) e depois uma ao AS61610 | a do AS61610 veio truncada: os 404 também contam |
| depois de ~5 min parado, consultas seguidas sem pausa | a **28ª** veio truncada, 4 s depois da primeira |
| logo depois, uma consulta a cada 20 s | a primeira dessas já veio completa |
| depois de 90 s parado, 120 consultas a ~1 por segundo | as **12** primeiras completas; daí em diante, só **1 em cada ~7** completa (28 completas, 92 truncadas) |

Leitura (estimativa, não documentada pelo registro.br): um **balde de
fichas por IP**, com capacidade perto de 28 e recarga de ~1 ficha a cada
7,5 s (**~8 por minuto**); sem ficha, a resposta sai truncada. Daí:

- sem proxy, um coletor a mais de ~8 consultas/min recebe truncadas — o
  padrão de 32/min ([collector.md](collector.md#taxa)) **pressupõe** o proxy
  que roda o IP;
- uma truncada se resolve esperando dezenas de segundos ou trocando de IP —
  é o que a espera da [coleta](collector.md#truncada-e-erros) faz.

Medir de novo quando o comportamento mudar (o teste
[`test-real`](collector.md#testes-específicos) mostra completas e truncadas
de uma rajada curta).

## Regras do parser

O processamento lê só respostas **completas** (e os 404) e grava as tabelas
de [dados-processados.md](dados-processados.md#mapeamento-resposta--colunas):

- **vCard** (`vcardArray`, RFC 7095): `["vcard", [[nome, parâmetros, tipo,
  valor], ...]]`; lidos `kind`, `fn`, `email` (o primeiro, em minúsculas,
  sem espaços nas pontas) e o país (o 7º item do valor de `adr`). Outras
  propriedades são ignoradas (ficam na resposta crua).
- **Documento**: o primeiro `publicIds` do titular, como publicado
  (`35.980.592/0001-30`) em `document`, o `type` em `document_type`
  (`cnpj`, `cpf` ou o que vier, em minúsculas).
- **Datas**: `events` com `eventAction` `registration` → `registered_at`,
  `last changed` → `changed_at` (RFC 3339; ausente = NULL).
- **Entidades aninhadas**: os contatos dentro do titular ficam ligados ao
  objeto com `via` = o titular; os do objeto, sem `via`. A mesma entidade
  em vários lugares é **uma** linha por `handle`.
- **Bloco**: o prefixo vem de `cidr0_cidrs` (o primeiro item); sem ele, de
  `handle`. Bloco cuja faixa não é um CIDR: o processamento recusa a
  resposta (`process_error` `faixa não é CIDR: <início>-<fim>`) e o dado
  processado anterior continua. Nenhum caso em 2026-10-21.
- **Delegação reversa**: `startAddress`–`endAddress` vira `prefix` quando é
  um CIDR exato; senão `prefix` NULL com as duas pontas.
- **Texto**: sem os espaços das pontas; vazio vira NULL; nada é traduzido
  nem mudado de caixa (fora o e-mail).
- JSON inválido num 200, ou `objectClassName` diferente do esperado
  (`autnum`, `ip network`): a coleta trata como **erro**
  ([collector.md](collector.md#truncada-e-erros)), não como resposta.

## Fixtures

Em `apps/registrobr/rdapnum/collector/testdata/`, respostas **reais** de
2026-10-21 com os **dados de pessoas físicas trocados** (o `fn` dos vCards
`individual` por `CONTATO EXEMPLO`, o e-mail deles por `contato@example.com`,
o `legalRepresentative` por `REPRESENTANTE EXEMPLO`, o IP do aviso por
`192.0.2.1`)
— o repositório é público ([../../../plataforma/seguranca.md](../../../plataforma/seguranca.md)).
Dados de empresas (razão social, CNPJ) e do NIC.br ficam como vieram:

| Arquivo | O que é |
|---|---|
| `autnum-61610.json` | ASN completo: titular com CNPJ, representante e contato aninhado; contato `administrative`+`abuse` |
| `autnum-61610-truncated.json` | o mesmo ASN truncado (sem `email`, `adr` e `publicIds`, com o `remarks`) |
| `autnum-22548.json` | ASN do NIC.br, com um bloco IPv4 e um IPv6 em `links` |
| `autnum-174.json` | ASN estrangeiro com titular brasileiro |
| `ip-200.225.48.0-21.json` | bloco IPv4 (pedido como `/24`), com contatos e uma delegação reversa |
| `ip-2804-8ae0--32.json` | bloco IPv6 com delegação reversa |
| `ip-187.87.28.0-22.json` | bloco sem delegação reversa |

Sintéticas, ao lado, para o que o real não garante (blocos e ASNs de
documentação, CNPJ fictício `11.111.111/0001-11`):

| Arquivo | O que é |
|---|---|
| `synthetic-ip-reverse-not-cidr.json` | bloco com uma faixa reversa que não é CIDR (`prefix` NULL) e outra que é, nameserver sem `events` |
| `synthetic-ip-block-not-cidr.json` | bloco cuja faixa não é CIDR, sem `cidr0_cidrs`: o processamento recusa |
| `synthetic-autnum-no-vcard.json` | ASN sem `events`, titular sem vCard, entidade sem `handle` (ignorada), `fn` vazio e e-mail com espaços e maiúsculas |
| `synthetic-invalid.json` | JSON cortado no meio |
| `synthetic-wrong-class.json` | `objectClassName` `domain` |

Os testes que usam cada uma: [collector.md](collector.md#testes-específicos);
um teste do `parse` confere que nenhuma fixture tem nome de pessoa física
ou e-mail fora de `example.com`.
