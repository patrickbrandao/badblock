# ripe/asnames — rotas da API

Cada rota da `api-ripe-asnames`: parâmetros, validação, normalização, chave de
cache, campos da resposta, exemplos reais e erros. O que vale para toda API
(formato do JSON, `HEAD`, `OPTIONS`, cabeçalhos, `serveCached`, ETag, saúde,
formato dos erros) está no [padrão](../../../padroes/api.md); valores, cache,
consultas, planos, medições e testes desta API, em [api.md](api.md); o SQL de
cada rota, em [dados.md](dados.md#consultas-da-api).

Os exemplos são respostas reais com o arquivo de 2026-09-28 (122.591 ASNs),
carregado em 2026-09-29T00:43:39Z (versão do dataset
`01a0ea9e-0f44-7227-8b1e-7d4919aae5cf`). Estão formatados para leitura — a
API responde JSON compacto, numa linha — e as listas longas foram cortadas
em `…`.

## Rotas

| Rota | Métodos | O quê | Chave de cache |
|---|---|---|---|
| `/ripe/asnames/asn/{asn}` | GET | um ASN | `asn:<n>` |
| `/ripe/asnames/country/{cc}` | GET | os ASNs de um país | `country:<CC>` |
| `/ripe/asnames/handle/{handle}` | GET | os ASNs de um handle | `handle:<handle>` |
| `/ripe/asnames/search?q={texto}` | GET | busca por trecho da `description` | `search:<termo>` |
| `/ripe/asnames/meta` | GET | estado dos dados | sem cache |
| `/ripe/asnames/` | GET | índice | sem cache |
| `/ripe/asnames/health`, `/ripe/asnames/status` | GET, POST | saúde | — |
| `/ripe/asnames/ping` | GET | `pong` | — |
| `/ripe/asnames/openapi.yaml` | GET | manifesto OpenAPI 3.1 | — |

As seis primeiras respondem também em `/ripe/asnames/v1/...`, com o mesmo
conteúdo, a mesma chave e o mesmo ETag; as três últimas ficam fora do
versionamento. Todo `GET` aceita `HEAD`.

Nas quatro rotas de dados:

- A validação vem **antes de tudo**: pedido inválido recebe 400 mesmo antes
  da primeira carga do coletor. Depois vêm, nesta ordem, a versão do dataset
  (503 `dataset_not_ready`), o `If-None-Match` (304), o Valkey e o Postgres.
- `handle`, `name` e `country` ausentes na fonte saem `null`, nunca `""`
  ([dados.md](dados.md#ripe_asnames_asn)).
- Toda resposta 200 traz o bloco `dataset` (`version`, `updated_at`) do
  [padrão](../../../padroes/api.md#formato-das-respostas).

## `GET /ripe/asnames/asn/{asn}`

Um ASN pelo número.

- `{asn}`: número decimal de 0 a 4294967295, com ou sem o prefixo `AS` em
  qualquer caixa (`15169`, `AS15169`, `as15169`). O código põe o valor em
  maiúsculas, tira um `AS` do começo (`strings.CutPrefix`) e lê o resto com
  `strconv.ParseUint(…, 10, 32)`: zeros à esquerda valem (`00015169` é
  15169); sinal (`+15169`, `-1`), espaço, `AS` sozinho, texto e números
  acima de 4294967295 são 400. O `AS` é conferido depois do
  `strings.ToUpper`, então letras que ele leva a ASCII também contam
  (`aſ15169` é AS15169).
- Chave: `asn:` + o número em decimal, sem `AS` e sem zeros à esquerda
  (`/asn/AS015169` → `asn:15169`). A consulta usa o mesmo número.

| Campo | Tipo | Significado |
|---|---|---|
| `asn` | inteiro | número do AS |
| `handle` | texto ou `null` | handle (as-name) derivado; não é único |
| `name` | texto ou `null` | nome da organização, derivado |
| `country` | texto ou `null` | duas letras maiúsculas, inclusive `EU` e `AP` |
| `description` | texto | a linha do `asn.txt` depois do número, como publicada (fonte da verdade) |
| `first_seen` | timestamp | `ripe_asnames_asn.created_at`: quando **este banco** viu o ASN pela primeira vez — não é a data de alocação ([dados.md](dados.md#mapeamento-da-fonte-para-as-colunas)) |
| `updated_at` | timestamp | `ripe_asnames_asn.updated_at`: última mudança de `description`, `handle`, `name` ou `country` |
| `dataset` | objeto | versão dos dados usada na resposta |

```json
{
  "asn": 15169,
  "handle": "GOOGLE",
  "name": "Google LLC",
  "country": "US",
  "description": "GOOGLE - Google LLC, US",
  "first_seen": "2026-09-29T00:43:39Z",
  "updated_at": "2026-09-29T00:43:39Z",
  "dataset": {"version": "01a0ea9e-0f44-7227-8b1e-7d4919aae5cf", "updated_at": "2026-09-29T00:43:39Z"}
}
```

Linha incompleta na fonte — `GET /ripe/asnames/asn/7901`:
`{"asn": 7901, "handle": null, "name": null, "country": "NZ", "description": "- , NZ", …}`.

| HTTP | Mensagem |
|---|---|
| 400 | `ASN inválido: use um número de 0 a 4294967295, com ou sem o prefixo AS` |
| 404 | `AS<n> não consta no asn.txt do RIPE NCC`, com o número normalizado (`AS4294967295 não consta no asn.txt do RIPE NCC`) |

## `GET /ripe/asnames/country/{cc}`

Os ASNs de um país, em ordem numérica: a lista completa, sem paginação e sem
parâmetro de limite (o maior, `US`, tem 32.219 ASNs e 2.254.782 bytes —
[medições](api.md#medições)).

- `{cc}`: duas letras, sem diferenciar maiúsculas. O código põe em
  maiúsculas (`strings.ToUpper`) e exige exatamente 2 bytes de `A` a `Z`:
  `us`, `US` e `Us` são a mesma consulta. Valem os códigos regionais `EU` e
  `AP` ([fonte.md](fonte.md#fatos-medidos)). Qualquer par de letras passa na
  validação — um código sem ASN (`ZZ`) é 404 —; uma letra, três letras,
  dígito ou letra não-ASCII (`ç`) é 400. A conferência vale para o resultado
  do `ToUpper`: letras que ele leva a ASCII passam (`ſı` vira `SI`).
- Chave: `country:<CC>` em maiúsculas; a consulta usa o mesmo código.

| Campo | Tipo | Significado |
|---|---|---|
| `country` | texto | código consultado, em maiúsculas |
| `count` | inteiro (≥ 1) | ASNs na lista |
| `asns` | lista | os ASNs do país, em ordem numérica, cada um só com `asn` (inteiro), `handle` e `name` (texto ou `null`) |
| `dataset` | objeto | versão dos dados |

```json
{
  "country": "AO",
  "count": 79,
  "asns": [
    {"asn": 11259, "handle": "Angola Telecom", "name": "Angola Telecom"},
    {"asn": 17400, "handle": "MSTelcom-Mercury Servicos de Telecomunicacoes, S.A.R.L", "name": "MSTelcom-Mercury Servicos de Telecomunicacoes, S.A.R.L"},
    {"asn": 32717, "handle": "SNET ANGOLA", "name": "SNET ANGOLA"},
    …
  ],
  "dataset": {"version": "01a0ea9e-0f44-7227-8b1e-7d4919aae5cf", "updated_at": "2026-09-29T00:43:39Z"}
}
```

| HTTP | Mensagem |
|---|---|
| 400 | `país inválido: use o código de duas letras (ex.: BR, US, EU)` |
| 404 | `nenhum ASN do país <CC> no asn.txt do RIPE NCC`, com o código em maiúsculas (`nenhum ASN do país ZZ no asn.txt do RIPE NCC`) |

## `GET /ripe/asnames/handle/{handle}`

Os ASNs cujo `handle` é **igual** ao pedido, sem diferenciar maiúsculas: não
é prefixo, trecho nem curinga (`%` e `_` são texto; para trecho, use
`/search`). O handle não é único na fonte (`GOOGLE` em 6 ASNs,
`VRSN-AC50-340` em 290 — [fonte.md](fonte.md#fatos-medidos)), então a
resposta é sempre uma lista, em ordem numérica.

- `{handle}`: um segmento do caminho, decodificado pelo `ServeMux` (`%20`
  vira espaço, `%C3%B4` vira `ô`, `%2F` vira `/` sem partir o segmento).
  Depois de tirar os espaços das pontas (`strings.TrimSpace`; os de dentro
  ficam como vieram, sem colapsar), precisa ter de 1 a 255 caracteres
  (`HandleMaxLen`, contados em caracteres, não bytes), ser UTF-8 válido e não
  ter caractere de controle (`unicode.IsControl`: NUL, C0, C1); senão 400.
  Motivo: o Postgres recusa NUL e UTF-8 inválido num parâmetro `text`, o que
  viraria 503 em vez de 400. Os 255 sobram: o maior handle derivado do
  arquivo tem 82 caracteres e a maior descrição, 217
  ([fonte.md](fonte.md#fatos-medidos)).
- Normalização: `strings.ToLower` do valor validado. É ele a chave
  (`handle:<valor>`), o parâmetro da consulta (`lower(handle) = lower($1)`) e
  o campo `handle` da resposta.
- Espaços e não-ASCII vão codificados na URL
  (`/ripe/asnames/handle/Orange%20C%C3%B4te%20d'Ivoire`). Handles com `/` (30 no
  arquivo de 2026-09-28, ex.: `ICE/HT`) precisam de `%2F`
  (`/ripe/asnames/handle/ICE%2FHT`); se um proxy no caminho recusar `%2F`, use
  `/search`.

| Campo | Tipo | Significado |
|---|---|---|
| `handle` | texto | o handle consultado, sem os espaços das pontas e em minúsculas (a chave) |
| `count` | inteiro (≥ 1) | ASNs na lista |
| `asns` | lista | os ASNs com esse handle, em ordem numérica, cada um com `asn` (inteiro), `handle`, `name`, `country` (texto ou `null`; `handle` como está no banco) e `description` (texto) |
| `dataset` | objeto | versão dos dados |

```json
{
  "handle": "google",
  "count": 6,
  "asns": [
    {"asn": 15169, "handle": "GOOGLE", "name": "Google LLC", "country": "US", "description": "GOOGLE - Google LLC, US"},
    {"asn": 22859, "handle": "GOOGLE", "name": "Google LLC", "country": "US", "description": "GOOGLE - Google LLC, US"},
    {"asn": 32381, "handle": "GOOGLE", "name": "CLOUD NETWORKING - Google Access LLC", "country": "US", "description": "GOOGLE CLOUD NETWORKING - Google Access LLC, US"},
    {"asn": 36039, "handle": "GOOGLE", "name": "Google LLC", "country": "US", "description": "GOOGLE - Google LLC, US"},
    {"asn": 394507, "handle": "GOOGLE", "name": "Google LLC", "country": "US", "description": "GOOGLE - Google LLC, US"},
    {"asn": 394639, "handle": "GOOGLE", "name": "Google LLC", "country": "US", "description": "GOOGLE - Google LLC, US"}
  ],
  "dataset": {"version": "01a0ea9e-0f44-7227-8b1e-7d4919aae5cf", "updated_at": "2026-09-29T00:43:39Z"}
}
```

O AS32381 mostra a limitação da derivação
([fonte.md](fonte.md#campos-derivados)): a linha
`GOOGLE CLOUD NETWORKING - Google Access LLC, US` tem espaço antes do
separador, cai na regra "primeira palavra" e o handle derivado fica `GOOGLE`;
a `description` traz o texto original.

| HTTP | Mensagem |
|---|---|
| 400 | `handle inválido: use de 1 a 255 caracteres, sem caracteres de controle` |
| 404 | `nenhum ASN com o handle <handle> no asn.txt do RIPE NCC`, com o handle **como veio** (sem os espaços das pontas, na caixa do pedido): `/ripe/asnames/handle/Nada-Disso` → `nenhum ASN com o handle Nada-Disso no asn.txt do RIPE NCC`. O 404 não entra no cache, e essa mensagem é a única parte da resposta que a chave não determina |

## `GET /ripe/asnames/search?q={texto}`

Os ASNs cuja `description` contém o trecho `q`, sem diferenciar maiúsculas
(`ILIKE`), em ordem numérica, com no máximo **100** resultados, sem
paginação nem parâmetro de limite. A busca olha só a `description`, então um
`handle` ou `name` mal derivado não esconde nada ([fonte.md](fonte.md#modelo)).

- `q`: vale o primeiro, se vier repetido (`?q=abc&q=…` busca `abc`). A
  normalização vem antes de tudo, nesta ordem:
  1. `strings.Fields` e junção com um espaço: tira os espaços das pontas e
     colapsa os internos — qualquer espaço em branco Unicode (espaço, tab, LF,
     CR, NBSP U+00A0…) vira um espaço;
  2. UTF-8 válido e nenhum caractere de controle (`unicode.IsControl`) —
     tab e LF já viraram espaço no passo 1 (`abc%09def` → `abc def`), mas NUL
     e os controles C1 (U+0080…) dão 400;
  3. minúsculas (`strings.ToLower`; a validação vem antes porque ela trocaria
     bytes inválidos por U+FFFD);
  4. de **3 a 100 caracteres** (`SearchMinLen`, `SearchMaxLen`), contados em
     caracteres, não bytes. O mínimo de 3 é o que o índice trigram aproveita
     ([api.md](api.md#consultas-e-planos)).

  Ausente, vazio, só espaços, curto, longo ou inválido: 400.
  `?q=%20Google%20%20LLC` e `?q=google+llc` são a mesma consulta, com a mesma
  chave e o mesmo ETag.
- Chave: `search:<termo normalizado>`; a consulta usa o mesmo termo.
- **Texto literal**: `store.LikePattern` monta `%<termo>%` escapando `\`
  (`\\`), `%` (`\%`) e `_` (`\_`) — a barra invertida é o escape padrão do
  `LIKE` no Postgres. `?q=100%25` procura "100%", `?q=a_b` procura "a_b".
- O store recebe o limite 101 (`SearchLimit + 1`): vindo mais de 100, a
  lista é cortada em 100 e `truncated` fica `true`. Sem resultado: 200 com
  `count: 0` e `asns: []` (não é 404, e entra no cache).
- Os controles C1 que a fonte traz no AS59265 (`â<U+0080><U+0093>`,
  [fonte.md](fonte.md#fatos-medidos)) não podem ir no termo (400); busque
  outro trecho da mesma descrição.

| Campo | Tipo | Significado |
|---|---|---|
| `query` | texto | o termo normalizado |
| `count` | inteiro (0 a 100) | itens devolvidos |
| `truncated` | booleano | `true` quando havia mais de 100 resultados |
| `asns` | lista (até 100) | os ASNs encontrados, em ordem numérica, com os campos de `/handle` (`asn`, `handle`, `name`, `country`, `description`); `[]` sem resultado |
| `dataset` | objeto | versão dos dados |

```json
{
  "query": "google llc",
  "count": 26,
  "truncated": false,
  "asns": [
    {"asn": 13949, "handle": "SLIDE-INC", "name": "Google LLC", "country": "US", "description": "SLIDE-INC - Google LLC, US"},
    {"asn": 15169, "handle": "GOOGLE", "name": "Google LLC", "country": "US", "description": "GOOGLE - Google LLC, US"},
    {"asn": 16550, "handle": "GOOGLE-PRIVATE-CLOUD", "name": "Google LLC", "country": "US", "description": "GOOGLE-PRIVATE-CLOUD - Google LLC, US"},
    …
  ],
  "dataset": {"version": "01a0ea9e-0f44-7227-8b1e-7d4919aae5cf", "updated_at": "2026-09-29T00:43:39Z"}
}
```

`?q=net` → `{"query": "net", "count": 100, "truncated": true, "asns": [{"asn": 10, "handle": "CSNET-EXT-AS", …}, …]}`.

Tempos por tipo de termo: [api.md](api.md#medições).

| HTTP | Mensagem |
|---|---|
| 400 | `use o parâmetro q com um trecho de 3 a 100 caracteres, sem caracteres de controle (ex.: ?q=google)` |

Não há 404: termo sem resultado é 200.

## `GET /ripe/asnames/meta`

Estado dos dados, lido do banco a cada pedido — sem Valkey, sem ETag,
`Cache-Control: no-store`: a última execução aplicada de `ripe_asnames_run` e a
linha `collector-ripe-asnames` de `jobs`
([dados.md](dados.md#consultas-da-api)), as duas consultas dentro de um
prazo `DB_TIMEOUT`. Responde 200 também antes da primeira carga.

| Campo | Tipo | Significado |
|---|---|---|
| `app` | texto | `api-ripe-asnames` |
| `version` | texto | versão do build (`buildinfo.Version`) |
| `dataset` | objeto ou `null` | última execução aplicada; `null` antes da primeira |
| `dataset.version` | texto (uuid) | `ripe_asnames_run.uuid`: a versão usada no cache e no ETag |
| `dataset.updated_at` | timestamp | `ripe_asnames_run.created_at` da execução |
| `dataset.source` | texto | `ripe_asnames_run.url`: de onde o arquivo foi baixado |
| `dataset.sha256` | texto | `ripe_asnames_run.sha256` (hex); `""` se a linha não tiver |
| `dataset.asns` | inteiro | `ripe_asnames_run.asns`: ASNs aceitos pelo parser; `0` se a linha não tiver |
| `collector` | objeto ou `null` | a linha do coletor em `jobs`; `null` enquanto ela não existe |
| `collector.app` | texto | `collector-ripe-asnames` |
| `collector.last_sync_at` | timestamp ou `null` | `jobs.last_sync_at` |
| `collector.last_check_at` | timestamp ou `null` | `jobs.last_check_at` |
| `collector.consolidated` | booleano | `jobs.consolidated = 1` ([postgres.md](../../../plataforma/postgres.md#tabela-jobs)) |

```json
{
  "app": "api-ripe-asnames",
  "version": "0.1.0",
  "dataset": {
    "version": "01a0ea9e-0f44-7227-8b1e-7d4919aae5cf",
    "updated_at": "2026-09-29T00:43:39Z",
    "source": "https://ftp.ripe.net/ripe/asnames/asn.txt",
    "sha256": "ac06c215f3847712397fd9781b4d68a804197e5d3670658f9abab3d3cecc12e4",
    "asns": 122591
  },
  "collector": {"app": "collector-ripe-asnames", "last_sync_at": "2026-09-29T00:43:39Z", "last_check_at": "2026-09-29T00:43:39Z", "consolidated": false}
}
```

Antes da primeira carga: `{"app": "api-ripe-asnames", "version": "0.1.0", "dataset": null, "collector": null}`.

Erros: 503 `database_unavailable`, 504 `timeout`, 500 `internal_error`
(nunca 400, 404 nem `dataset_not_ready`).

## `GET /ripe/asnames/` e `GET /ripe/asnames/v1/`

Índice, igual nos dois caminhos; não consulta o banco nem o Valkey.
`Cache-Control: public, max-age=300`, sem ETag.

| Campo | Tipo | Significado |
|---|---|---|
| `app` | texto | `api-ripe-asnames` |
| `version` | texto | versão do build |
| `base_path` | texto | `BASE_PATH` normalizado |
| `versions` | lista de texto | `["v1"]`; a última é a das rotas sem versão |
| `endpoints` | lista de texto | as sete rotas abaixo, nesta ordem, já com o caminho de base (não traz `/health`, `/ping` nem as rotas com `/v1`) |
| `source` | texto | `https://ftp.ripe.net/ripe/asnames/asn.txt` (constante `SourceURL`, não vem do banco) |

```json
{
  "app": "api-ripe-asnames",
  "version": "0.1.0",
  "base_path": "/ripe/asnames",
  "versions": ["v1"],
  "endpoints": ["/ripe/asnames/asn/{asn}", "/ripe/asnames/country/{cc}", "/ripe/asnames/handle/{handle}", "/ripe/asnames/search?q={texto}", "/ripe/asnames/meta", "/ripe/asnames/status", "/ripe/asnames/openapi.yaml"],
  "source": "https://ftp.ripe.net/ripe/asnames/asn.txt"
}
```

## Saúde: `/ripe/asnames/health`, `/ripe/asnames/status` e `/ripe/asnames/ping`

Como no [padrão](../../../padroes/api.md#saúde), com `<fonte>` = `ripe-asnames`
(mensagens `api-ripe-asnames operacional` e
`aguardando a primeira sincronização do collector-ripe-asnames`):

```json
{"success": true, "status": "ok", "timestamp": "2026-09-29T00:48:15Z", "message": "api-ripe-asnames operacional", "checks": {"dataset": "ok", "postgres": "ok", "valkey": "ok"}}
```

- `/health` e `/status` são o mesmo handler, em GET e POST (o corpo do POST
  é ignorado).
- As verificações rodam nesta ordem, e a última que falha decide `status` e
  `message`: dataset (`starting`), Valkey (`degraded`), Postgres (`error`,
  503). Ou seja, `error` > `degraded` > `starting` > `ok`.
- O prazo de 2 s é **um só** para os dois pings (um `context.WithTimeout`); o
  do Valkey tem ainda o próprio limite, `max(4 × REDIS_TIMEOUT, 200 ms)`
  (200 ms com o padrão). O Postgres é conferido com `SELECT 1`.
- `/ripe/asnames/ping`: `pong` em `text/plain; charset=utf-8`.

## `GET /ripe/asnames/openapi.yaml`

O manifesto embutido, como em
[openapi.md](../../../padroes/openapi.md#resposta-de-get-fonteopenapiyaml);
`/ripe/asnames/v1/openapi.yaml` é 404. Conteúdo: [api.md](api.md#manifesto-openapi).

## Erros

Formato, `Cache-Control: no-store` e códigos: [padrão](../../../padroes/api.md#erros).
Mensagens reais desta API:

| HTTP | `code` | `message` | Onde |
|---|---|---|---|
| 400 | `bad_request` | `ASN inválido: use um número de 0 a 4294967295, com ou sem o prefixo AS` | `/asn` |
| 400 | `bad_request` | `país inválido: use o código de duas letras (ex.: BR, US, EU)` | `/country` |
| 400 | `bad_request` | `handle inválido: use de 1 a 255 caracteres, sem caracteres de controle` | `/handle` |
| 400 | `bad_request` | `use o parâmetro q com um trecho de 3 a 100 caracteres, sem caracteres de controle (ex.: ?q=google)` | `/search` |
| 404 | `not_found` | `AS<n> não consta no asn.txt do RIPE NCC` | `/asn` |
| 404 | `not_found` | `nenhum ASN do país <CC> no asn.txt do RIPE NCC` | `/country` |
| 404 | `not_found` | `nenhum ASN com o handle <handle> no asn.txt do RIPE NCC` | `/handle` |
| 404 | `not_found` | `rota inexistente; veja /ripe/asnames/` | qualquer outro caminho, ou método não registrado |
| 503 | `dataset_not_ready` | `a primeira sincronização do collector-ripe-asnames ainda não terminou; tente em alguns minutos` | rotas de dados |
| 503 | `database_unavailable` | `banco de dados indisponível` | rotas de dados e `/meta` |
| 504 | `timeout` | `a consulta demorou demais` | rotas de dados e `/meta` |
| 500 | `internal_error` | `erro interno` | panic, em qualquer rota |

Um `store.ErrNotFound` que chegasse sem tradução viraria 404
`registro não encontrado`; nenhuma rota atual deixa isso acontecer.

Exemplo: `{"error": {"code": "not_found", "message": "AS4294967295 não consta no asn.txt do RIPE NCC"}}`.

## Roteamento

`ServeMux` do Go 1.27 com o catch-all `/` registrado:

| Pedido | Resposta |
|---|---|
| `/ripe/asnames` (com ou sem query) | 301, `Location: /ripe/asnames/` (a query se perde) |
| `/ripe/asnames/v1` (sem barra) | 307, `Location: /ripe/asnames/v1/`, corpo HTML do `ServeMux` (não é JSON) |
| caminho com `//`, `.` ou `..` (`/ripe/asnames//asn/15169`) | 307 para o caminho limpo, corpo HTML do `ServeMux` |
| barra no fim de uma rota (`/ripe/asnames/asn/15169/`) | 404 JSON |
| método não registrado numa rota existente (`DELETE` ou `POST` em `/ripe/asnames/asn/15169`, `PUT /ripe/asnames/status`, `POST /ripe/asnames/meta`) | 404 JSON `rota inexistente; veja /ripe/asnames/` — o catch-all casa antes do 405 do `ServeMux` |
| `OPTIONS` em qualquer caminho | 204, preflight de CORS |
| `/ripe/asnames/v2/...`, `/ripe/asnames/v1/health`, `/ripe/asnames/v1/status`, `/ripe/asnames/v1/ping`, `/ripe/asnames/v1/openapi.yaml` | 404 JSON |
