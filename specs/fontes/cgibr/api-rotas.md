# Rotas da `api-cgibr`

Cada rota da `api-cgibr`: parâmetros e validação (com as mensagens reais),
chave de cache, campos da resposta e exemplos reais. Valores do app, opções,
manifesto, operação e testes: [api.md](api.md). O que vale para toda API
(formato do JSON, bloco `dataset`, `serveCached`, ETag e 304, cabeçalhos,
`HEAD`, `OPTIONS`, `/v1`, erros comuns) está no
[padrão](../../padroes/api.md) e não se repete aqui. As consultas SQL e os
índices: [dados.md](dados.md#consultas-da-api-cgibr). Dono: sub-agente
`api-cgibr`.

## Exemplos

Respostas reais, indentadas para leitura (a API responde JSON compacto numa
linha, com `\n` no fim). Vêm de dois datasets:

| `dataset.version` | `dataset.updated_at` | Arquivo do NIC.br | Usado em |
|---|---|---|---|
| `01a0ea50-add2-7bcf-998a-a2e3b04b8cdc` | 2026-09-28T23:19:10Z | de 2026-09-28: SHA-256 `0a47…65c2`, 9.134 ASNs, 13.037 blocos IPv4, 8.954 IPv6 ([fonte.md](fonte.md#fatos-medidos-2026-09-28)) | rotas de dados |
| `01a0eb55-1a1c-7bdb-915a-e885abbaaf72` | 2026-09-29T04:03:38Z | de 2026-09-29: SHA-256 `b9c3cb12…2d20`, 9.135 ASNs, 13.041 IPv4, 8.955 IPv6 | `/meta`, ETags ([api.md](api.md#manifesto)) e os fatos "em 2026-09-29" |

`first_seen` e `updated_at` do ASN contam desde a primeira carga **do banco**
([dados.md](dados.md#tabelas)): o exemplo é de um banco carregado pela
primeira vez em 2026-09-28T23:19:10Z; no stack local de 2026-09-29, cujo
banco foi carregado em 2026-09-28T23:54:26Z, o mesmo AS61613 responde esse
horário.

## ASN — `GET /cgibr/asn/{asn}`

- `{asn}`: número decimal de 0 a 4294967295, com ou sem o prefixo `AS` em
  qualquer caixa (`61613`, `AS61613`, `as61613`, `aS61613`); zeros à esquerda
  valem (`061613` é o 61613). O código tira o `AS` depois de passar para
  maiúsculas e lê com `strconv.ParseUint(…, 10, 32)`: sinal (`+1`, `-1`),
  espaço, `AS` sozinho, letra ou valor acima de 32 bits → 400
  `ASN inválido: use um número de 0 a 4294967295, com ou sem o prefixo AS`.
- Chave: `asn:<número>` em decimal, sem zeros à esquerda (`asn:61613`).
- 404: `AS<número> não consta no arquivo do NIC.br` (ex.: `AS1 não consta no arquivo do NIC.br`).
- Manifesto: `getASN`, tag `dados`; respostas 200, 304, 400, 404, 503, 504, 500.

| Campo | Tipo | Conteúdo |
|---|---|---|
| `asn` | inteiro, 0 a 4294967295 | `cgibr_asn.asn` |
| `name` | texto | nome do titular como publicado (`cgibr_asn.name`) |
| `document` | texto | documento como publicado: CNPJ `00.000.000/0000-00` ou identificador estrangeiro de 8 dígitos (`cgibr_asn.document`) |
| `document_type` | `cnpj` ou `foreign` | `cnpj` quando `document_digits` tem 14 dígitos; senão `foreign` |
| `prefixes.ipv4`, `prefixes.ipv6` | listas de CIDR | blocos do ASN, cada família em ordem de endereço; `[]` quando não há |
| `first_seen` | timestamp | `cgibr_asn.created_at`: primeira vez que o ASN apareceu na fonte |
| `updated_at` | timestamp | `cgibr_asn.updated_at`: última troca de nome ou documento |
| `dataset` | objeto | [padrão](../../padroes/api.md#formato-das-respostas) |

`asn`, `name`, `document` e `document_type` formam o `ASNRef`, o ASN resumido
que se repete em `/ip`, `/prefix` e `/asns`.

```json
{
  "asn": 61613,
  "name": "TMSoft Solucoes em Informatica Ltda",
  "document": "08.030.063/0001-00",
  "document_type": "cnpj",
  "prefixes": {
    "ipv4": ["45.171.60.0/22", "200.192.152.0/22"],
    "ipv6": ["2804:5964::/32"]
  },
  "first_seen": "2026-09-28T23:19:10Z",
  "updated_at": "2026-09-28T23:19:10Z",
  "dataset": {"version": "01a0ea50-add2-7bcf-998a-a2e3b04b8cdc", "updated_at": "2026-09-28T23:19:10Z"}
}
```

Em 2026-09-29, `/cgibr/asn/174` (ASN da Cogent, registrado fora do Brasil,
com o titular brasileiro dos blocos) respondia `prefixes.ipv4` `[]` e
`prefixes.ipv6` `["2804:5330::/32"]`.

## IP — `GET /cgibr/ip/{ip}`

Bloco mais específico do arquivo que contém o endereço.

- `{ip}`: IPv4 ou IPv6 que `netip.ParseAddr` aceita, sem zona. Zona
  (`fe80::1%25eth0`), IPv4 com zero à esquerda (`045.171.61.10`) ou texto
  qualquer → 400 `endereço IP inválido`. IPv4 mapeado em IPv6
  (`::ffff:45.171.61.10`) vira IPv4.
- Chave: `ip:<endereço normalizado>` — IPv4 com pontos, IPv6 na forma curta
  em minúsculas (`2804:5964:0:0::1` → `ip:2804:5964::1`). O campo `ip` da
  resposta sai na mesma forma.
- Consulta: o endereço como `/32` ou `/128` ([dados.md](dados.md#consultas-da-api-cgibr)).
- 404: `<ip> não pertence a nenhum bloco do arquivo do NIC.br` (ex.: `8.8.8.8 não pertence a nenhum bloco do arquivo do NIC.br`).
- Manifesto: `getIP`, tag `dados`; respostas 200, 304, 400, 404, 503, 504, 500.

| Campo | Tipo | Conteúdo |
|---|---|---|
| `ip` | texto (IPv4 ou IPv6) | endereço consultado, normalizado |
| `prefix` | CIDR | bloco mais específico do arquivo que contém o endereço |
| `asn` | `ASNRef` | ASN da linha do bloco, com o titular |
| `dataset` | objeto | [padrão](../../padroes/api.md#formato-das-respostas) |

```json
{
  "ip": "45.171.61.10",
  "prefix": "45.171.60.0/22",
  "asn": {"asn": 61613, "name": "TMSoft Solucoes em Informatica Ltda", "document": "08.030.063/0001-00", "document_type": "cnpj"},
  "dataset": {"version": "01a0ea50-add2-7bcf-998a-a2e3b04b8cdc", "updated_at": "2026-09-28T23:19:10Z"}
}
```

## Prefixo — `GET /cgibr/prefix/{ip}/{len}`

Bloco mais específico que contém o prefixo pedido (o próprio bloco conta).

- `{ip}`: como em `/ip`, validado primeiro, com a mesma mensagem.
- `{len}`: lido com `strconv.Atoi` (aceita sinal: `+24` vale 24); de 0 a 32
  para IPv4 — inclusive o mapeado, cujo tamanho já é contado no IPv4
  (`::ffff:200.192.152.9/120` é recusado) — e de 0 a 128 para IPv6. Fora
  disso, ou não numérico → 400 `tamanho de prefixo inválido`.
- Bits de host são zerados antes da consulta: `/prefix/200.192.152.9/24`
  consulta `200.192.152.0/24`.
- Chave: `prefix:<prefixo já zerado>` (`prefix:200.192.152.0/24`).
- 404: `<prefixo> não está contido em nenhum bloco do arquivo do NIC.br` (ex.: `8.8.8.0/24 não está contido em nenhum bloco do arquivo do NIC.br`).
- Manifesto: `getPrefix`, tag `dados`; respostas 200, 304, 400, 404, 503, 504, 500.

| Campo | Tipo | Conteúdo |
|---|---|---|
| `query` | CIDR | prefixo consultado, com os bits de host zerados |
| `prefix` | CIDR | bloco mais específico do arquivo que contém `query` |
| `exact` | booleano | `true` quando `prefix` é o próprio `query` |
| `asn` | `ASNRef` | ASN da linha do bloco |
| `dataset` | objeto | [padrão](../../padroes/api.md#formato-das-respostas) |

```json
{
  "query": "200.192.152.0/24",
  "prefix": "200.192.152.0/22",
  "exact": false,
  "asn": {"asn": 61613, "name": "TMSoft Solucoes em Informatica Ltda", "document": "08.030.063/0001-00", "document_type": "cnpj"},
  "dataset": {"version": "01a0ea50-add2-7bcf-998a-a2e3b04b8cdc", "updated_at": "2026-09-28T23:19:10Z"}
}
```

`/cgibr/prefix/45.171.60.0/22` e `/cgibr/prefix/2804:5964::/32` respondem o
próprio bloco com `exact: true`.

## Documento — `GET /cgibr/document/{doc}`

ASNs de um titular, pelo documento.

- `{doc}`: todo caractere que não é dígito é descartado; sobram 14 dígitos
  (CNPJ) ou 8 (identificador estrangeiro do NIC.br), senão → 400
  `documento inválido: use o CNPJ (14 dígitos) ou o identificador estrangeiro (8 dígitos)`.
  Aceita só dígitos (`08030063000100`) ou o CNPJ formatado com a barra
  codificada (`08.030.063%2F0001-00`). Com a barra sem codificar, o caminho
  ganha um segmento e cai no 404 `rota inexistente; veja /cgibr/`. Como os
  não dígitos são descartados, `abc08030063000100` também vale.
- Chave: `doc:<dígitos>` (`doc:08030063000100`).
- Consulta: `document_digits` igual, em ordem de ASN.
- 404 (nenhum ASN): `nenhum ASN com esse documento no arquivo do NIC.br`.
- Manifesto: `getDocument`, tag `dados`; respostas 200, 304, 400, 404, 503, 504, 500.

| Campo | Tipo | Conteúdo |
|---|---|---|
| `document` | texto | documento como publicado, do primeiro ASN da lista |
| `document_digits` | texto, 8 ou 14 dígitos | os dígitos pedidos |
| `document_type` | `cnpj` ou `foreign` | pelos dígitos pedidos: 14 = `cnpj`, 8 = `foreign` |
| `name` | texto | nome do titular, do primeiro ASN da lista |
| `count` | inteiro ≥ 1 | quantidade de ASNs do titular |
| `asns` | lista de `ASNName` (`asn`, `name`) | os ASNs, em ordem numérica, cada um com o seu nome |
| `dataset` | objeto | [padrão](../../padroes/api.md#formato-das-respostas) |

```json
{
  "document": "08.030.063/0001-00",
  "document_digits": "08030063000100",
  "document_type": "cnpj",
  "name": "TMSoft Solucoes em Informatica Ltda",
  "count": 1,
  "asns": [{"asn": 61613, "name": "TMSoft Solucoes em Informatica Ltda"}],
  "dataset": {"version": "01a0ea50-add2-7bcf-998a-a2e3b04b8cdc", "updated_at": "2026-09-28T23:19:10Z"}
}
```

Em 2026-09-29, `/cgibr/document/10996639` respondia `document_type`
`foreign`, `name` `Internet Systems Consortium` e `count` 24 (AS275689 a
AS275712), os 24 identificadores estrangeiros do arquivo
([fonte.md](fonte.md#fatos-medidos-2026-09-28)).

## Todos os ASNs — `GET /cgibr/asns`

Todos os ASNs do arquivo, sem os blocos, em ordem numérica. Sem parâmetros:
não há paginação nem filtro, a lista vem inteira numa resposta de ~1 MB
([medições](api.md#medições)).

- Chave: `asns`.
- Manifesto: `listASNs`, tag `dados`; respostas 200, 304, 503, 504, 500 (sem
  400 nem 404).

| Campo | Tipo | Conteúdo |
|---|---|---|
| `count` | inteiro ≥ 0 | quantidade de ASNs |
| `asns` | lista de `ASNRef` | todos os ASNs, em ordem numérica |
| `dataset` | objeto | [padrão](../../padroes/api.md#formato-das-respostas) |

Recorte (a lista real traz os 9.134 ASNs):

```json
{
  "count": 9134,
  "asns": [
    {"asn": 174, "name": "COGENT BRASIL TELECOMUNICAÇÕES LTDA.", "document": "29.484.413/0001-70", "document_type": "cnpj"},
    {"asn": 61613, "name": "TMSoft Solucoes em Informatica Ltda", "document": "08.030.063/0001-00", "document_type": "cnpj"}
  ],
  "dataset": {"version": "01a0ea50-add2-7bcf-998a-a2e3b04b8cdc", "updated_at": "2026-09-28T23:19:10Z"}
}
```

## Meta — `GET /cgibr/meta`

Estado dos dados, sem cache e sem ETag ([padrão](../../padroes/api.md#rotas-comuns)):
a última execução aplicada de `cgibr_run` e a linha `collector-cgibr` de
`jobs`, uma consulta de cada, com um prazo `DB_TIMEOUT` para as duas. Falha →
503 `database_unavailable` ou 504 `timeout`; nunca `dataset_not_ready`.
`Cache-Control: no-store`. Manifesto: `getMeta`, tag `meta`; respostas 200,
503, 504, 500.

| Campo | Tipo | Conteúdo |
|---|---|---|
| `app` | `api-cgibr` | |
| `version` | texto | versão do build da API |
| `dataset.version` | uuid | `cgibr_run.uuid` da última execução com `status = 1` |
| `dataset.updated_at` | timestamp | `cgibr_run.created_at` dela |
| `dataset.source` | URL | `cgibr_run.url`: de onde o arquivo foi baixado |
| `dataset.sha256` | 64 dígitos hexadecimais | `cgibr_run.sha256` |
| `dataset.asns`, `dataset.prefixes_v4`, `dataset.prefixes_v6` | inteiros ≥ 0 | `cgibr_run.asns`, `prefixes_v4`, `prefixes_v6` |
| `collector.app` | `collector-cgibr` | |
| `collector.last_sync_at`, `collector.last_check_at` | timestamp ou `null` | colunas de `jobs` |
| `collector.consolidated` | booleano | `jobs.consolidated = 1` |

`dataset` é `null` sem execução aplicada e `collector` é `null` sem a linha em
`jobs`; a resposta é 200 mesmo assim.

```json
{
  "app": "api-cgibr",
  "version": "0.1.0",
  "dataset": {
    "version": "01a0eb55-1a1c-7bdb-915a-e885abbaaf72",
    "updated_at": "2026-09-29T04:03:38Z",
    "source": "https://ftp.registro.br/pub/numeracao/origin/nicbr-asn-blk-latest.txt",
    "sha256": "b9c3cb12fdb5738c919c64f7a4472e830fd5e244e2ca0ae96e6a898077dc2d20",
    "asns": 9135,
    "prefixes_v4": 13041,
    "prefixes_v6": 8955
  },
  "collector": {"app": "collector-cgibr", "last_sync_at": "2026-09-29T04:03:38Z", "last_check_at": "2026-09-29T14:11:25Z", "consolidated": false}
}
```

Antes da primeira carga: `{"app": "api-cgibr", "version": "0.1.0", "dataset": null, "collector": null}`.
Com o arquivo de 2026-09-28, `dataset` trazia `sha256` `0a47…65c2`, `asns`
9134, `prefixes_v4` 13037 e `prefixes_v6` 8954.

## Índice — `GET /cgibr/` e `GET /cgibr/v1/`

Não consulta o banco nem o Valkey; `Cache-Control: public, max-age=300`, sem
ETag. `version` é a versão do build (`buildinfo.Version`): `0.1.0` no
exemplo, `latest` na imagem que o compose gera com `API_CGIBR_TAG=latest`
(visto em 2026-09-29), `dev` sem `-ldflags`. `source` é a constante
`SourceURL`. `endpoints` é uma lista fixa de 8 rotas, sem `/health` nem
`/ping`, e escreve o parâmetro do documento como `{cnpj}` (a rota registrada
e o manifesto usam `{doc}`). Manifesto: `getIndex`, tag `meta`; respostas
200 e 500.

```json
{
  "app": "api-cgibr",
  "version": "0.1.0",
  "base_path": "/cgibr",
  "versions": ["v1"],
  "endpoints": [
    "/cgibr/asn/{asn}", "/cgibr/ip/{ip}", "/cgibr/prefix/{ip}/{len}",
    "/cgibr/document/{cnpj}", "/cgibr/asns", "/cgibr/meta", "/cgibr/status",
    "/cgibr/openapi.yaml"
  ],
  "source": "https://ftp.registro.br/pub/numeracao/origin/nicbr-asn-blk-latest.txt"
}
```

`/cgibr` redireciona (301) para `/cgibr/`, como no padrão; `/cgibr/v1` (sem
barra) não tem rota própria e recebe o redirect automático do `http.ServeMux`:
307 para `/cgibr/v1/`, com corpo HTML.

## Saúde, `ping` e manifesto

Como no [padrão](../../padroes/api.md#saúde), com as mensagens
`api-cgibr operacional` e `aguardando a primeira sincronização do collector-cgibr`.
Manifesto: `getHealth`/`postHealth` e `getStatus`/`postStatus` (tag `saúde`;
200 `StatusOK`, 503 `StatusError`, 500), `ping` (tag `saúde`; 200
`text/plain`, `const: pong`) e `getOpenAPI` (tag `meta`; 200
`application/yaml`). Existem só sem versão: `/cgibr/v1/health`,
`/cgibr/v1/status`, `/cgibr/v1/ping` e `/cgibr/v1/openapi.yaml` respondem
404.

Como `/health` e `/status` decidem (resumo do [padrão](../../padroes/api.md#saúde), conferido no código):

- As verificações rodam nesta ordem e cada uma sobrescreve `status` e
  `message` da anterior: sem dataset → `starting`; Valkey fora → `degraded`
  (mesmo sem dataset); Postgres fora → `error` (503), sempre.
- `dataset` vem da versão em memória (a relida a cada `DATASET_POLL`), sem
  consulta; `valkey` é `disabled` com o cache desligado.
- Um prazo de 2 s vale para o handler inteiro (Valkey primeiro, depois
  Postgres); o ping do Valkey tem ainda o prazo próprio de
  max(4 × `REDIS_TIMEOUT`, 200 ms) — 200 ms com o padrão.
- As chaves de `checks` saem em ordem alfabética (é um mapa).

Resposta real (`GET /cgibr/status`):

```json
{"success": true, "status": "ok", "timestamp": "2026-09-29T14:44:16Z", "message": "api-cgibr operacional",
 "checks": {"dataset": "ok", "postgres": "ok", "valkey": "ok"}}
```

Os outros estados, como no manifesto (mesma hora):

| `status` | HTTP | `success` | `checks` (`dataset`, `postgres`, `valkey`) |
|---|---|---|---|
| `starting` | 200 | `true` | `empty`, `ok`, `ok` |
| `degraded` | 200 | `true` | `ok`, `ok`, `error` |
| `error` | 503 | `false` | `ok`, `error`, `ok` |

## Erros

O formato e os códigos são os do [padrão](../../padroes/api.md#erros). As
mensagens reais:

| Onde | HTTP | `code` | `message` |
|---|---|---|---|
| `/asn` | 400 | `bad_request` | `ASN inválido: use um número de 0 a 4294967295, com ou sem o prefixo AS` |
| `/ip`, `/prefix` | 400 | `bad_request` | `endereço IP inválido` |
| `/prefix` | 400 | `bad_request` | `tamanho de prefixo inválido` |
| `/document` | 400 | `bad_request` | `documento inválido: use o CNPJ (14 dígitos) ou o identificador estrangeiro (8 dígitos)` |
| `/asn` | 404 | `not_found` | `AS<número> não consta no arquivo do NIC.br` |
| `/ip` | 404 | `not_found` | `<ip> não pertence a nenhum bloco do arquivo do NIC.br` |
| `/prefix` | 404 | `not_found` | `<prefixo> não está contido em nenhum bloco do arquivo do NIC.br` |
| `/document` | 404 | `not_found` | `nenhum ASN com esse documento no arquivo do NIC.br` |
| caminho inexistente ou método que a rota não aceita (ex.: `POST /cgibr/asn/61613`, `/cgibr/v2/asn/61613`) | 404 | `not_found` | `rota inexistente; veja /cgibr/` |
| rotas de dados, antes da primeira carga | 503 | `dataset_not_ready` | `a primeira sincronização do collector-cgibr ainda não terminou; tente em alguns minutos` |
| rotas de dados e `/meta` | 503 | `database_unavailable` | `banco de dados indisponível` |
| rotas de dados e `/meta` | 504 | `timeout` | `a consulta demorou demais` |
| qualquer rota (panic) | 500 | `internal_error` | `erro interno` |

`store.ErrNotFound` que chegue sem mensagem própria vira 404
`registro não encontrado`; nas rotas atuais não acontece (cada uma troca pela
sua, e `/document` recebe lista vazia).
