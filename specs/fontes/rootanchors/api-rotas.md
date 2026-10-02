# rootanchors — rotas da API

Cada rota da `api-rootanchors`: parâmetros, validação, normalização, chave de
cache, campos da resposta, exemplos reais e erros. O que vale para toda API
(formato do JSON, `HEAD`, `OPTIONS`, cabeçalhos, `serveCached`, ETag, saúde,
formato dos erros) está no [padrão](../../padroes/api.md); valores, decisões,
cache, medições e testes desta API, em [api.md](api.md); o SQL de cada rota,
em [dados.md](dados.md#consultas-da-api-rootanchors). Dono: sub-agente
`api-rootanchors`.

Os exemplos são respostas reais com o `root-anchors.xml` de 2026-09-30 (3
chaves, SHA-256 `3ccaab38…5278b908`), baixado da IANA pelo
`collector-rootanchors` e aplicado em 2026-09-30T03:44:43Z num
`postgres:18-trixie` descartável (versão do dataset
`01a0f06a-259b-7abc-a1fd-a7b4bd279125`). Estão formatados para leitura — a
API responde JSON compacto, numa linha.

## Rotas

| Rota | Métodos | O quê | Chave de cache |
|---|---|---|---|
| `/rootanchors/keys` | GET | todas as chaves | `keys` |
| `/rootanchors/key/{key_tag}` | GET | as chaves de um key tag | `key:<n>` |
| `/rootanchors/meta` | GET | estado dos dados | sem cache |
| `/rootanchors/` | GET | índice | sem cache |
| `/rootanchors/health`, `/rootanchors/status` | GET, POST | saúde | — |
| `/rootanchors/ping` | GET | `pong` | — |
| `/rootanchors/openapi.yaml` | GET | manifesto OpenAPI 3.1 | — |

As quatro primeiras respondem também em `/rootanchors/v1/...`, com o mesmo
conteúdo, a mesma chave e o mesmo ETag; as três últimas ficam fora do
versionamento. Todo `GET` aceita `HEAD`.

Nas duas rotas de dados:

- A validação vem **antes de tudo**: pedido inválido recebe 400 mesmo antes
  da primeira carga do coletor. Depois vêm, nesta ordem, a versão do dataset
  (503 `dataset_not_ready`), o `If-None-Match` (304), o Valkey e o Postgres.
- Toda resposta 200 traz o bloco `trust_anchor` (do arquivo aplicado), a
  lista `keys` no formato [abaixo](#a-chave-key) e o bloco `dataset`
  (`version`, `updated_at`) do [padrão](../../padroes/api.md#formato-das-respostas).
- Não há campo que dependa do relógio ([api.md](api.md#chave-ativa-e-o-relógio)).

## A chave (`Key`)

Uma linha de `rootanchors_key` (um `KeyDigest` do arquivo), com os textos do
DS e do DNSKEY montados pela API. Campos, nesta ordem:

| Campo | Tipo | Significado |
|---|---|---|
| `key_id` | texto | `id` do `KeyDigest` (ex.: `Kmyv6jo`); único no arquivo |
| `key_tag` | inteiro, 0 a 65535 | key tag do DNSKEY; **não é único** |
| `algorithm` | inteiro, 0 a 255 | algoritmo DNSSEC (8 = RSA/SHA-256) |
| `digest_type` | inteiro | tipo do digest do DS: 1 SHA-1, 2 SHA-256, 4 SHA-384 |
| `digest` | texto | digest do DS em hex maiúsculo (40, 64 ou 96 dígitos) |
| `ds` | texto | o registro DS em texto: `<zona> IN DS <key_tag> <algorithm> <digest_type> <digest>` |
| `public_key` | texto ou `null` | chave pública do DNSKEY em base64, sem espaços; `null` nas chaves que o arquivo publica só com o DS (a 19036) |
| `flags` | inteiro ou `null` | flags do DNSKEY (257 = zone key + SEP); `null` junto com `public_key` |
| `dnskey` | texto ou `null` | o registro DNSKEY em texto: `<zona> IN DNSKEY <flags> 3 <algorithm> <public_key>`; `null` junto com `public_key` |
| `valid_from` | timestamp | `validFrom` (UTC) |
| `valid_until` | timestamp ou `null` | `validUntil` (UTC); `null` = sem data de fim |
| `first_seen` | timestamp | `rootanchors_key.created_at`: quando **este banco** viu a chave pela primeira vez — não é data da IANA |
| `updated_at` | timestamp | `rootanchors_key.updated_at`: última alteração de algum dado da chave neste banco |

- `ds` e `dnskey` seguem o formato de apresentação de arquivo de zona (RFC
  1035, 5.1; RDATA da RFC 4034, 2.2 e 5.3), **sem TTL**: dono, classe `IN`,
  tipo e os campos do RDATA separados por um espaço; o digest em hex
  maiúsculo, como vem da fonte; a chave pública em base64 numa linha só. O
  dono é a `zone` do `trust_anchor` (sempre `.`). O protocolo do DNSKEY é
  sempre 3 (RFC 4034, 2.1.2). Os textos servem de âncora sem conversão
  (ex.: `trust-anchor:` e `trust-anchor-file` do Unbound).
- A ordem da lista é `valid_from`, depois `key_id` (a do arquivo, na
  prática: da mais antiga para a mais nova).
- Coerência: o coletor recusa o arquivo se o key tag e o digest publicados
  não batem com os recalculados da `PublicKey`
  ([fonte.md](fonte.md#keydigest)); o `TestRealFile` refaz a conta a partir
  do texto `dnskey` da resposta ([api.md](api.md#testes)).

`trust_anchor` (de `rootanchors_run`, a última execução aplicada):

| Campo | Tipo | Significado |
|---|---|---|
| `id` | texto | `TrustAnchor@id` (UUID em maiúsculas) |
| `source` | texto ou `null` | `TrustAnchor@source`, a URL que a IANA declara (hoje em `http://`); `null` quando vazio |
| `zone` | texto | `Zone`: sempre `.` |

## `GET /rootanchors/keys`

Todas as chaves do último arquivo aplicado, inclusive as aposentadas.

- Sem parâmetros; a query é ignorada. Chave: `keys`.
- Não há 404: a tabela inteira, mesmo vazia (`count: 0`, `keys: []`), o que o
  coletor não deixa acontecer (`MIN_KEYS` ≥ 1).

| Campo | Tipo | Significado |
|---|---|---|
| `trust_anchor` | objeto | [acima](#a-chave-key) |
| `count` | inteiro (≥ 0) | chaves na lista |
| `keys` | lista de `Key` | todas as chaves, em ordem de `valid_from` e `key_id` |
| `dataset` | objeto | versão dos dados usada na resposta |

```json
{
  "trust_anchor": {"id": "0C05FDD6-422C-4910-8ED6-430ED15E11C2", "source": "http://data.iana.org/root-anchors/root-anchors.xml", "zone": "."},
  "count": 3,
  "keys": [
    {
      "key_id": "Kjqmt7v",
      "key_tag": 19036,
      "algorithm": 8,
      "digest_type": 2,
      "digest": "49AAC11D7B6F6446702E54A1607371607A1A41855200FD2CE1CDDE32F24E8FB5",
      "ds": ". IN DS 19036 8 2 49AAC11D7B6F6446702E54A1607371607A1A41855200FD2CE1CDDE32F24E8FB5",
      "public_key": null,
      "flags": null,
      "dnskey": null,
      "valid_from": "2010-07-15T00:00:00Z",
      "valid_until": "2019-01-11T00:00:00Z",
      "first_seen": "2026-09-30T03:44:43Z",
      "updated_at": "2026-09-30T03:44:43Z"
    },
    {
      "key_id": "Klajeyz",
      "key_tag": 20326,
      "algorithm": 8,
      "digest_type": 2,
      "digest": "E06D44B80B8F1D39A95C0B0D7C65D08458E880409BBC683457104237C7F8EC8D",
      "ds": ". IN DS 20326 8 2 E06D44B80B8F1D39A95C0B0D7C65D08458E880409BBC683457104237C7F8EC8D",
      "public_key": "AwEAAaz/tAm8yTn4Mfeh5eyI96WSVexTBAvkMgJzkKTOiW1vkIbzxeF3+/4RgWOq7HrxRixHlFlExOLAJr5emLvN7SWXgnLh4+B5xQlNVz8Og8kvArMtNROxVQuCaSnIDdD5LKyWbRd2n9WGe2R8PzgCmr3EgVLrjyBxWezF0jLHwVN8efS3rCj/EWgvIWgb9tarpVUDK/b58Da+sqqls3eNbuv7pr+eoZG+SrDK6nWeL3c6H5Apxz7LjVc1uTIdsIXxuOLYA4/ilBmSVIzuDWfdRUfhHdY6+cn8HFRm+2hM8AnXGXws9555KrUB5qihylGa8subX2Nn6UwNR1AkUTV74bU=",
      "flags": 257,
      "dnskey": ". IN DNSKEY 257 3 8 AwEAAaz/tAm8yTn4Mfeh5eyI96WSVexTBAvkMgJzkKTOiW1vkIbzxeF3+/4RgWOq7HrxRixHlFlExOLAJr5emLvN7SWXgnLh4+B5xQlNVz8Og8kvArMtNROxVQuCaSnIDdD5LKyWbRd2n9WGe2R8PzgCmr3EgVLrjyBxWezF0jLHwVN8efS3rCj/EWgvIWgb9tarpVUDK/b58Da+sqqls3eNbuv7pr+eoZG+SrDK6nWeL3c6H5Apxz7LjVc1uTIdsIXxuOLYA4/ilBmSVIzuDWfdRUfhHdY6+cn8HFRm+2hM8AnXGXws9555KrUB5qihylGa8subX2Nn6UwNR1AkUTV74bU=",
      "valid_from": "2017-02-02T00:00:00Z",
      "valid_until": null,
      "first_seen": "2026-09-30T03:44:43Z",
      "updated_at": "2026-09-30T03:44:43Z"
    },
    {
      "key_id": "Kmyv6jo",
      "key_tag": 38696,
      "algorithm": 8,
      "digest_type": 2,
      "digest": "683D2D0ACB8C9B712A1948B27F741219298D0A450D612C483AF444A4C0FB2B16",
      "ds": ". IN DS 38696 8 2 683D2D0ACB8C9B712A1948B27F741219298D0A450D612C483AF444A4C0FB2B16",
      "public_key": "AwEAAa96jeuknZlaeSrvyAJj6ZHv28hhOKkx3rLGXVaC6rXTsDc449/cidltpkyGwCJNnOAlFNKF2jBosZBU5eeHspaQWOmOElZsjICMQMC3aeHbGiShvZsx4wMYSjH8e7Vrhbu6irwCzVBApESjbUdpWWmEnhathWu1jo+siFUiRAAxm9qyJNg/wOZqqzL/dL/q8PkcRU5oUKEpUge71M3ej2/7CPqpdVwuMoTvoB+ZOT4YeGyxMvHmbrxlFzGOHOijtzN+u1TQNatX2XBuzZNQ1K+s2CXkPIZo7s6JgZyvaBevYtxPvYLw4z9mR7K2vaF18UYH9Z9GNUUeayffKC73PYc=",
      "flags": 257,
      "dnskey": ". IN DNSKEY 257 3 8 AwEAAa96jeuknZlaeSrvyAJj6ZHv28hhOKkx3rLGXVaC6rXTsDc449/cidltpkyGwCJNnOAlFNKF2jBosZBU5eeHspaQWOmOElZsjICMQMC3aeHbGiShvZsx4wMYSjH8e7Vrhbu6irwCzVBApESjbUdpWWmEnhathWu1jo+siFUiRAAxm9qyJNg/wOZqqzL/dL/q8PkcRU5oUKEpUge71M3ej2/7CPqpdVwuMoTvoB+ZOT4YeGyxMvHmbrxlFzGOHOijtzN+u1TQNatX2XBuzZNQ1K+s2CXkPIZo7s6JgZyvaBevYtxPvYLw4z9mR7K2vaF18UYH9Z9GNUUeayffKC73PYc=",
      "valid_from": "2024-07-18T00:00:00Z",
      "valid_until": null,
      "first_seen": "2026-09-30T03:44:43Z",
      "updated_at": "2026-09-30T03:44:43Z"
    }
  ],
  "dataset": {"version": "01a0f06a-259b-7abc-a1fd-a7b4bd279125", "updated_at": "2026-09-30T03:44:43Z"}
}
```

Erros: 503 `dataset_not_ready` e `database_unavailable`, 504 `timeout`, 500
`internal_error` (nunca 400 nem 404).

## `GET /rootanchors/key/{key_tag}`

As chaves de um key tag. Como o key tag não é único (é uma soma de 16 bits
do DNSKEY; duas chaves podem coincidir, e uma rolagem pode trazer uma chave
nova com o tag de uma antiga), a resposta é **sempre uma lista**.

- `{key_tag}`: só dígitos ASCII (`0`–`9`), lidos com
  `strconv.ParseUint(…, 10, 16)`: de 0 a 65535, zeros à esquerda valem
  (`020326` é 20326). Sinal (`+1`, `-1`), espaço, notação (`1e3`, `0x10`),
  dígitos não-ASCII e valores acima de 65535 são 400. `/rootanchors/key/`
  (vazio) e `/rootanchors/key` não casam com a rota: 404 genérico.
- Chave: `key:` + o número em decimal, sem zeros à esquerda
  (`/v1/key/020326` → `key:20326`). A consulta usa o mesmo número.
- Nenhuma chave com o key tag: 404 (fora do cache).

| Campo | Tipo | Significado |
|---|---|---|
| `key_tag` | inteiro | key tag consultado, normalizado |
| `trust_anchor` | objeto | [acima](#a-chave-key) |
| `count` | inteiro (≥ 1) | chaves com esse key tag |
| `keys` | lista de `Key` | as chaves, em ordem de `valid_from` e `key_id` |
| `dataset` | objeto | versão dos dados |

`GET /rootanchors/key/20326`:

```json
{
  "key_tag": 20326,
  "trust_anchor": {"id": "0C05FDD6-422C-4910-8ED6-430ED15E11C2", "source": "http://data.iana.org/root-anchors/root-anchors.xml", "zone": "."},
  "count": 1,
  "keys": [
    {
      "key_id": "Klajeyz",
      "key_tag": 20326,
      "algorithm": 8,
      "digest_type": 2,
      "digest": "E06D44B80B8F1D39A95C0B0D7C65D08458E880409BBC683457104237C7F8EC8D",
      "ds": ". IN DS 20326 8 2 E06D44B80B8F1D39A95C0B0D7C65D08458E880409BBC683457104237C7F8EC8D",
      "public_key": "AwEAAaz/tAm8yTn4Mfeh5eyI96WSVexTBAvkMgJzkKTOiW1vkIbzxeF3+/4RgWOq7HrxRixHlFlExOLAJr5emLvN7SWXgnLh4+B5xQlNVz8Og8kvArMtNROxVQuCaSnIDdD5LKyWbRd2n9WGe2R8PzgCmr3EgVLrjyBxWezF0jLHwVN8efS3rCj/EWgvIWgb9tarpVUDK/b58Da+sqqls3eNbuv7pr+eoZG+SrDK6nWeL3c6H5Apxz7LjVc1uTIdsIXxuOLYA4/ilBmSVIzuDWfdRUfhHdY6+cn8HFRm+2hM8AnXGXws9555KrUB5qihylGa8subX2Nn6UwNR1AkUTV74bU=",
      "flags": 257,
      "dnskey": ". IN DNSKEY 257 3 8 AwEAAaz/tAm8yTn4Mfeh5eyI96WSVexTBAvkMgJzkKTOiW1vkIbzxeF3+/4RgWOq7HrxRixHlFlExOLAJr5emLvN7SWXgnLh4+B5xQlNVz8Og8kvArMtNROxVQuCaSnIDdD5LKyWbRd2n9WGe2R8PzgCmr3EgVLrjyBxWezF0jLHwVN8efS3rCj/EWgvIWgb9tarpVUDK/b58Da+sqqls3eNbuv7pr+eoZG+SrDK6nWeL3c6H5Apxz7LjVc1uTIdsIXxuOLYA4/ilBmSVIzuDWfdRUfhHdY6+cn8HFRm+2hM8AnXGXws9555KrUB5qihylGa8subX2Nn6UwNR1AkUTV74bU=",
      "valid_from": "2017-02-02T00:00:00Z",
      "valid_until": null,
      "first_seen": "2026-09-30T03:44:43Z",
      "updated_at": "2026-09-30T03:44:43Z"
    }
  ],
  "dataset": {"version": "01a0f06a-259b-7abc-a1fd-a7b4bd279125", "updated_at": "2026-09-30T03:44:43Z"}
}
```

A chave aposentada, só com o DS — `GET /rootanchors/key/19036`:
`{"key_tag": 19036, …, "count": 1, "keys": [{"key_id": "Kjqmt7v", …, "public_key": null, "flags": null, "dnskey": null, "valid_from": "2010-07-15T00:00:00Z", "valid_until": "2019-01-11T00:00:00Z", …}], …}`
(a chave inteira está no exemplo de `/keys`).

| HTTP | Mensagem |
|---|---|
| 400 | `key tag inválido: use um número de 0 a 65535 (ex.: 20326)` |
| 404 | `nenhuma chave com o key tag <n> no root-anchors.xml da IANA`, com o número normalizado (`/key/00012345` → `… key tag 12345 …`) |

## `GET /rootanchors/meta`

Estado dos dados, lido do banco a cada pedido — sem Valkey, sem ETag,
`Cache-Control: no-store`: a última execução aplicada de `rootanchors_run` e
a linha `collector-rootanchors` de `jobs`
([dados.md](dados.md#consultas-da-api-rootanchors)), as duas consultas dentro
de um prazo `DB_TIMEOUT`. Responde 200 também antes da primeira carga.

| Campo | Tipo | Significado |
|---|---|---|
| `app` | texto | `api-rootanchors` |
| `version` | texto | versão do build (`buildinfo.Version`) |
| `dataset` | objeto ou `null` | última execução aplicada; `null` antes da primeira |
| `dataset.version` | texto (uuid) | `rootanchors_run.uuid`: a versão usada no cache e no ETag |
| `dataset.updated_at` | timestamp | `rootanchors_run.created_at` da execução |
| `dataset.source` | texto | `rootanchors_run.url`: de onde o arquivo foi baixado |
| `dataset.sha256` | texto | `rootanchors_run.sha256` (hex minúsculo) |
| `dataset.keys` | inteiro | `rootanchors_run.keys`: `KeyDigest` do arquivo |
| `dataset.trust_anchor` | objeto | `anchor_id`, `anchor_source` e `zone` da execução, como nas rotas de dados |
| `collector` | objeto ou `null` | a linha do coletor em `jobs`; `null` enquanto ela não existe |
| `collector.app` | texto | `collector-rootanchors` |
| `collector.last_sync_at` | timestamp ou `null` | `jobs.last_sync_at` |
| `collector.last_check_at` | timestamp ou `null` | `jobs.last_check_at` |
| `collector.consolidated` | booleano | `jobs.consolidated = 1` ([postgres.md](../../plataforma/postgres.md#tabela-jobs)) |

```json
{
  "app": "api-rootanchors",
  "version": "0.1.0",
  "dataset": {
    "version": "01a0f06a-259b-7abc-a1fd-a7b4bd279125",
    "updated_at": "2026-09-30T03:44:43Z",
    "source": "https://data.iana.org/root-anchors/root-anchors.xml",
    "sha256": "3ccaab38830025ee0a0f6c1f25769427544f81ea2865aa860468f3ef5278b908",
    "keys": 3,
    "trust_anchor": {"id": "0C05FDD6-422C-4910-8ED6-430ED15E11C2", "source": "http://data.iana.org/root-anchors/root-anchors.xml", "zone": "."}
  },
  "collector": {"app": "collector-rootanchors", "last_sync_at": "2026-09-30T03:44:43Z", "last_check_at": "2026-09-30T03:44:43Z", "consolidated": false}
}
```

Antes da primeira carga: `{"app": "api-rootanchors", "version": "0.1.0", "dataset": null, "collector": null}`.

Erros: 503 `database_unavailable`, 504 `timeout`, 500 `internal_error`
(nunca 400, 404 nem `dataset_not_ready`).

## `GET /rootanchors/` e `GET /rootanchors/v1/`

Índice, igual nos dois caminhos; não consulta o banco nem o Valkey.
`Cache-Control: public, max-age=300`, sem ETag.

| Campo | Tipo | Significado |
|---|---|---|
| `app` | texto | `api-rootanchors` |
| `version` | texto | versão do build |
| `base_path` | texto | `BASE_PATH` normalizado |
| `versions` | lista de texto | `["v1"]`; a última é a das rotas sem versão |
| `endpoints` | lista de texto | as cinco rotas abaixo, nesta ordem, já com o caminho de base (não traz `/health`, `/ping` nem as rotas com `/v1`) |
| `source` | texto | `https://data.iana.org/root-anchors/root-anchors.xml` (constante `SourceURL`, não vem do banco) |

```json
{
  "app": "api-rootanchors",
  "version": "0.1.0",
  "base_path": "/rootanchors",
  "versions": ["v1"],
  "endpoints": ["/rootanchors/keys", "/rootanchors/key/{key_tag}", "/rootanchors/meta", "/rootanchors/status", "/rootanchors/openapi.yaml"],
  "source": "https://data.iana.org/root-anchors/root-anchors.xml"
}
```

## Saúde: `/rootanchors/health`, `/rootanchors/status` e `/rootanchors/ping`

Como no [padrão](../../padroes/api.md#saúde), com `<fonte>` = `rootanchors`
(mensagens `api-rootanchors operacional` e
`aguardando a primeira sincronização do collector-rootanchors`):

```json
{"success": true, "status": "ok", "timestamp": "2026-09-30T03:55:03Z", "message": "api-rootanchors operacional", "checks": {"dataset": "ok", "postgres": "ok", "valkey": "ok"}}
```

- `/health` e `/status` são o mesmo handler, em GET e POST (o corpo do POST
  é ignorado); precedência `error` > `degraded` > `starting` > `ok`.
- `/rootanchors/ping`: `pong` em `text/plain; charset=utf-8`,
  `Cache-Control: no-store`, fora do log de acesso.

## `GET /rootanchors/openapi.yaml`

O manifesto embutido, como em
[openapi.md](../../padroes/openapi.md#resposta-de-get-fonteopenapiyaml);
`/rootanchors/v1/openapi.yaml` é 404. Conteúdo: [api.md](api.md#manifesto-openapi).

## Erros

Formato, `Cache-Control: no-store` e códigos: [padrão](../../padroes/api.md#erros).
Mensagens reais desta API:

| HTTP | `code` | `message` | Onde |
|---|---|---|---|
| 400 | `bad_request` | `key tag inválido: use um número de 0 a 65535 (ex.: 20326)` | `/key` |
| 404 | `not_found` | `nenhuma chave com o key tag <n> no root-anchors.xml da IANA` | `/key` |
| 404 | `not_found` | `rota inexistente; veja /rootanchors/` | qualquer outro caminho, ou método não registrado |
| 503 | `dataset_not_ready` | `a primeira sincronização do collector-rootanchors ainda não terminou; tente em alguns minutos` | rotas de dados (também se o watcher já tem versão e o banco ainda não devolve a execução aplicada) |
| 503 | `database_unavailable` | `banco de dados indisponível` | rotas de dados e `/meta` |
| 504 | `timeout` | `a consulta demorou demais` | rotas de dados e `/meta` |
| 500 | `internal_error` | `erro interno` | panic, em qualquer rota |

Exemplo: `{"error": {"code": "not_found", "message": "nenhuma chave com o key tag 12345 no root-anchors.xml da IANA"}}`.

## Roteamento

`ServeMux` do Go 1.27 com o catch-all `/` registrado:

| Pedido | Resposta |
|---|---|
| `/rootanchors` (com ou sem query) | 301, `Location: /rootanchors/` (a query se perde) |
| `/rootanchors/v1` (sem barra) | 307, `Location: /rootanchors/v1/`, corpo HTML do `ServeMux` (não é JSON) |
| caminho com `//`, `.` ou `..` | 307 para o caminho limpo, corpo HTML do `ServeMux` |
| barra no fim de uma rota (`/rootanchors/key/20326/`, `/rootanchors/keys/`), `/rootanchors/key`, `/rootanchors/key/`, `/rootanchors/keys/20326` | 404 JSON |
| método não registrado numa rota existente (`POST`, `PUT` ou `DELETE` em `/rootanchors/keys`, `POST /rootanchors/meta`) | 404 JSON `rota inexistente; veja /rootanchors/` — o catch-all casa antes do 405 do `ServeMux` |
| `OPTIONS` em qualquer caminho | 204, preflight de CORS |
| `/rootanchors/v2/...`, `/rootanchors/v1/health`, `/rootanchors/v1/status`, `/rootanchors/v1/ping`, `/rootanchors/v1/openapi.yaml` | 404 JSON |
