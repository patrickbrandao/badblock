# Rotas da `api-registrobr-rdapnum`

Parte do [api.md](api.md): cada rota com parâmetros, validações, campos,
exemplos e erros. Dono: sub-agente `api-registrobr-rdapnum`. Caminhos
relativos a `/registrobr/rdapnum` (e `/registrobr/rdapnum/v1`).

Os exemplos são respostas da API (2026-10-21, teste de integração) sobre as
respostas reais do registro.br do mesmo dia — as fixtures do coletor
([fonte.md](fonte.md#fixtures)) — gravadas nas tabelas processadas como o
processamento as grava; as pessoas físicas estão trocadas por `CONTATO
EXEMPLO` / `contato@example.com` e `REPRESENTANTE EXEMPLO` (o repositório é
público). O `uuid` das respostas é ilustrativo. Medir de novo com o stack no
ar e a primeira volta do coletor ([api.md](api.md#medições)).

## Bloco `scan`

Em toda rota de dados, do alvo que gerou o objeto:

| Campo | Conteúdo |
|---|---|
| `kind`, `target` | `asn` e `"61610"`, ou `prefix` e o bloco do alvo (`"200.225.48.0/21"`) |
| `source`, `active` | do alvo |
| `checked_at` | `last_scan_at`: data do dado (última coleta conclusiva); `null` = nunca |
| `attempted_at` | `last_attempt_at` |
| `last_result` | `complete`, `truncated`, `not_found`, `error` ou `null` |
| `retry_at` | próxima tentativa depois de truncada ou erro; `null` sem espera |
| `response` | `uuid` da resposta atual (a do dado); `null` sem ela |

`last_result` `truncated` com `checked_at` antigo quer dizer: a última
tentativa veio truncada, e o dado mostrado é o da última completa.

## Entidades e contatos

`registrant` (o titular) e cada item de `contacts`:

| Campo | Conteúdo |
|---|---|
| `handle`, `kind`, `name`, `email`, `country` | de `_entity` |
| `document`, `document_type`, `document_digits`, `legal_representative` | de `_entity` (em geral só no titular) |
| `registered_at`, `changed_at` | de `_entity` |
| `roles` | papéis da entidade **neste objeto**, em ordem alfabética (só em `contacts`) |
| `via` | `handle` da entidade dentro da qual veio (o titular), ou `null` (só em `contacts`) |

`contacts` tem um item por par (`handle`, `via`), ordenado por `via`
(`null` primeiro) e `handle`. O papel `registrant` não entra em `contacts`
(o titular sai em `registrant`); a entidade que só tem esse papel não
aparece lá.

## ASN — `GET /asn/{asn}`

- `{asn}`: inteiro de 0 a 4294967295, com ou sem `AS`/`as` na frente
  (`As`, sinal e espaço não valem; zeros à esquerda sim); senão 400 `ASN
  inválido`. Chave `asn:<número>`.
- 404 `AS<n> não é alvo do rdapnum` sem alvo; com alvo mas sem dado
  processado (nunca coletado, ou 404 no registro.br): **200** com os campos
  do ASN `null`, `contacts` e `networks` `[]` e o `scan` (é por ele que se
  sabe o porquê).

| Campo | Conteúdo |
|---|---|
| `asn` | o número |
| `handle`, `name`, `type`, `country`, `registered_at`, `changed_at` | de `_autnum` |
| `registrant` | a entidade titular, ou `null` |
| `contacts` | contatos do ASN e do titular ([acima](#entidades-e-contatos)) |
| `networks` | blocos com `asn` igual: `prefix`, `type`, `status`, em ordem (IPv4 primeiro) |
| `scan` | [acima](#bloco-scan) |

```json
{
  "asn": 61610, "handle": "61610", "name": "ASN61610", "type": "DIRECT ALLOCATION", "country": "BR",
  "registered_at": "2023-02-27T15:58:28Z", "changed_at": "2023-02-27T15:58:28Z",
  "registrant": {"handle": "35980592000130", "kind": "org", "name": "ELEA DATA CENTERS", "email": null, "country": "BR",
    "document": "35.980.592/0001-30", "document_type": "cnpj", "document_digits": "35980592000130",
    "legal_representative": "REPRESENTANTE EXEMPLO",
    "registered_at": "2023-02-06T21:58:30Z", "changed_at": "2026-04-22T15:11:18Z"},
  "contacts": [
    {"handle": "ROBEL308", "kind": "individual", "name": "CONTATO EXEMPLO", "email": "contato@example.com", "country": "BR",
     "document": null, "document_type": null, "document_digits": null, "legal_representative": null,
     "registered_at": "2023-02-08T17:39:14Z", "changed_at": "2026-07-07T18:30:11Z",
     "roles": ["abuse", "administrative"], "via": null},
    {"handle": "ROBEL308", "kind": "individual", "name": "CONTATO EXEMPLO", "email": "contato@example.com", "country": "BR",
     "document": null, "document_type": null, "document_digits": null, "legal_representative": null,
     "registered_at": "2023-02-08T17:39:14Z", "changed_at": "2026-07-07T18:30:11Z",
     "roles": ["administrative"], "via": "35980592000130"}
  ],
  "networks": [
    {"prefix": "187.87.28.0/22", "type": "ALLOCATED", "status": ["active"]},
    {"prefix": "200.225.48.0/21", "type": "ALLOCATED", "status": ["active"]},
    {"prefix": "2804:8ae0::/32", "type": "ALLOCATED", "status": ["active"]}
  ],
  "scan": {"kind": "asn", "target": "61610", "source": "cgibr", "active": true,
    "checked_at": "2026-10-21T20:36:01Z", "attempted_at": "2026-10-21T20:36:01Z",
    "last_result": "complete", "retry_at": null, "response": "019a0a4e-…"}
}
```

## IP e prefixo — `GET /ip/{ip}` e `GET /prefix/{ip}/{len}`

- `{ip}`: IPv4 ou IPv6 que `netip.ParseAddr` aceita (IPv4 mapeado vira
  IPv4); senão 400 `endereço IP inválido`. `/ip` consulta como `/32` ou
  `/128`.
- `{len}`: 0 a 32 (IPv4) ou 0 a 128 (IPv6), em decimal sem zero à esquerda
  nem sinal; senão 400 `tamanho de prefixo inválido`. Bits de host zerados;
  chave `prefix:<prefixo>`; `/ip` usa `ip:<ip>`.
- Responde o **bloco registrado mais específico que contém** o pedido; nenhum
  → 404 `<pedido> não está em nenhum bloco do rdapnum`.

| Campo | Conteúdo |
|---|---|
| `query` | o IP ou prefixo pedido, normalizado |
| `prefix`, `start_address`, `end_address`, `ip_version` (`4`/`6`) | do bloco |
| `handle`, `name`, `type`, `country`, `status`, `parent_handle`, `asn`, `registered_at`, `changed_at` | de `_network` |
| `registrant`, `contacts` | como no ASN |
| `reverse` | delegações: `prefix` (ou `null`), `start_address`, `end_address`, `nameservers` (`name`, `status`, `checked_at`, `last_ok_at`), `signed`, `registered_at`, `changed_at` |
| `scan` | do alvo que gerou o bloco |

```json
{
  "query": "200.225.48.0/24",
  "prefix": "200.225.48.0/21", "start_address": "200.225.48.0", "end_address": "200.225.55.255", "ip_version": 4,
  "handle": "200.225.48.0/21", "name": "462539", "type": "ALLOCATED", "country": "BR", "status": ["active"],
  "parent_handle": null, "asn": 61610, "registered_at": "1999-11-18T12:00:00Z", "changed_at": "2023-07-19T11:05:57Z",
  "registrant": {"handle": "35980592000130", "name": "ELEA DATA CENTERS", "document": "35.980.592/0001-30", "…": "…"},
  "contacts": [
    {"handle": "ROBEL308", "name": "CONTATO EXEMPLO", "email": "contato@example.com", "roles": ["abuse", "technical"], "via": null, "…": "…"},
    {"handle": "ROBEL308", "name": "CONTATO EXEMPLO", "email": "contato@example.com", "roles": ["administrative"], "via": "35980592000130", "…": "…"}],
  "reverse": [{"prefix": "200.225.48.0/24", "start_address": "200.225.48.0", "end_address": "200.225.48.255",
    "nameservers": [
      {"name": "keenan.ns.cloudflare.com", "status": ["ns aa"], "checked_at": "2026-10-21T12:00:00Z", "last_ok_at": "2026-10-21T12:00:00Z"},
      {"name": "sureena.ns.cloudflare.com", "status": ["ns aa"], "checked_at": "2026-10-21T12:00:00Z", "last_ok_at": "2026-10-21T12:00:00Z"}],
    "signed": false, "registered_at": "2024-09-27T15:31:56Z", "changed_at": "2024-09-27T15:31:56Z"}],
  "scan": {"kind": "prefix", "target": "200.225.48.0/21", "last_result": "complete", "…": "…"}
}
```

(`"…": "…"` abrevia aqui os campos já mostrados no ASN; a resposta traz
todos.)

## Documento — `GET /document/{document}`

- `{document}`: CNPJ ou CPF com ou sem pontuação (só dígitos, `.`, `/`, `-`
  e espaço); os dígitos têm de ser 11 ou 14; senão 400 `documento inválido:
  use CNPJ (14 dígitos) ou CPF (11)`. A barra pode ir crua
  (`/document/35.980.592/0001-30`: a rota pega o resto do caminho) ou como
  `%2F`. Chave `document:<dígitos>`.
- Nenhuma entidade com esses dígitos → 404 `documento <dígitos> não
  encontrado no rdapnum`.

| Campo | Conteúdo |
|---|---|
| `document_digits`, `document_type` | o pedido e o tipo (`cnpj`, `cpf`) |
| `entities` | as entidades com o documento (em geral uma), como o `registrant` |
| `asns` | ASNs em que alguma delas é titular: `asn`, `name` e `scan` só com `checked_at`, em ordem de ASN |
| `networks` | blocos em que alguma delas é titular: `prefix`, `asn`, `type`, `status`, IPv4 primeiro |

```json
{"document_digits": "35980592000130", "document_type": "cnpj",
 "entities": [{"handle": "35980592000130", "kind": "org", "name": "ELEA DATA CENTERS", "…": "…"}],
 "asns": [{"asn": 61610, "name": "ASN61610", "scan": {"checked_at": "2026-10-21T20:36:01Z"}}],
 "networks": [
   {"prefix": "187.87.28.0/22", "asn": 61610, "type": "ALLOCATED", "status": ["active"]},
   {"prefix": "200.225.48.0/21", "asn": 61610, "type": "ALLOCATED", "status": ["active"]},
   {"prefix": "2804:8ae0::/32", "asn": 61610, "type": "ALLOCATED", "status": ["active"]}]}
```

## Contato — `GET /contact/{handle}`

- `{handle}`: 1 a 64 caracteres `[A-Za-z0-9._-]`; senão 400 `handle
  inválido`. Comparação exata (o registro.br usa maiúsculas). Chave
  `contact:<handle>`.
- Sem entidade → 404 `contato <handle> não encontrado no rdapnum`.

| Campo | Conteúdo |
|---|---|
| `entity` | a entidade, como o `registrant` |
| `objects` | onde aparece, um item por objeto e `via`: `kind` (`asn`/`prefix`), `asn` e `prefix` (o do outro tipo `null`), `roles` (em ordem alfabética, com `registrant` quando é o titular), `via`; em ordem de `kind`, objeto (blocos IPv4 primeiro) e `via` (`null` primeiro) |

```json
{"entity": {"handle": "ROBEL308", "kind": "individual", "name": "CONTATO EXEMPLO", "email": "contato@example.com", "…": "…"},
 "objects": [
   {"kind": "asn", "asn": 61610, "prefix": null, "roles": ["abuse", "administrative"], "via": null},
   {"kind": "asn", "asn": 61610, "prefix": null, "roles": ["administrative"], "via": "35980592000130"},
   {"kind": "prefix", "asn": null, "prefix": "187.87.28.0/22", "roles": ["administrative"], "via": "35980592000130"},
   {"kind": "prefix", "asn": null, "prefix": "200.225.48.0/21", "roles": ["abuse", "technical"], "via": null},
   {"kind": "prefix", "asn": null, "prefix": "200.225.48.0/21", "roles": ["administrative"], "via": "35980592000130"},
   {"kind": "prefix", "asn": null, "prefix": "2804:8ae0::/32", "roles": ["abuse", "technical"], "via": null},
   {"kind": "prefix", "asn": null, "prefix": "2804:8ae0::/32", "roles": ["administrative"], "via": "35980592000130"}]}
```

## Resposta crua

### `GET /asn/{asn}/raw` e `GET /prefix/{ip}/{len}/raw`

O corpo RDAP da resposta **atual** do alvo (a do dado processado), como o
registro.br mandou, com `Content-Type: application/rdap+json`, **menos** as
linhas de `notices[*].description` no formato `<hora> - <ip>` (o IP de
saída do coletor). O alvo sai como no `refresh`: `asn` pelo número; `prefix`
pelo bloco exato ou pelo bloco registrado que contém o pedido; nenhum → 404
`<pedido> não é alvo do rdapnum`. Sem resposta atual completa (nunca
coletado, ou 404) → 404 `<alvo> sem resposta coletada` (`AS61610` ou o
bloco do alvo). Cabeçalhos a mais: `X-Response-UUID` (o `uuid`) e
`Last-Modified` (`last_seen_at`). Chaves de cache `asn-raw:<n>` e
`prefix-raw:<pedido>`; os cabeçalhos vão ao cache junto com o corpo.

O corpo sai em JSON compacto, com as chaves na ordem em que o `jsonb`
as guarda (não é byte a byte o que chegou). A regra que tira o IP de saída é
conservadora: some de `description` a linha `^\S+ - \S+$` e **qualquer**
linha com um endereço IP solto; um `notices` fora do formato (não é lista)
sai como `[]`. Nada fora de `notices` é mexido.

### `GET /asn/{asn}/history` e `GET /prefix/{ip}/{len}/history`

| Campo | Conteúdo |
|---|---|
| `target`, `kind` | o alvo (`"61610"` ou o bloco do alvo); sem alvo → 404 `<pedido> não é alvo do rdapnum` |
| `current` | `uuid` da resposta atual, ou `null` |
| `responses` | todas as versões, a mais nova primeiro: `uuid`, `result` (`complete`, `truncated`, `not_found`), `http_status`, `sha256`, `bytes`, `fetched_at`, `last_seen_at`, `seen_count`, `processed` (booleano), `process_error` — **sem** `egress_ip` nem `server_time` |

### `GET /response/{uuid}`

Uma versão do histórico pelo `uuid` (inválido → 400 `uuid inválido`;
inexistente → 404 `resposta <uuid> não encontrada`), com a mesma regra do
`/raw` (corpo RDAP sem a linha do IP; `X-Response-UUID`, `Last-Modified`).
Uma `not_found` responde 200 com `{"result":"not_found","http_status":404}`
(`application/json`). Serve também as versões truncadas do histórico (é uma
versão pedida pelo `uuid`, não o dado atual). O `uuid` é imutável (aceito em
qualquer caixa, normalizado em minúsculas na chave `response:<uuid>`):
`Cache-Control: public, max-age=86400`.

## Atualização

`POST /asn/{asn}/refresh` e `POST /prefix/{ip}/{len}/refresh` (regras em
[api.md](api.md#atualização-post-refresh)). Resposta 202:

```json
{"kind": "asn", "target": "61610", "queued": true, "reason": null, "checked_at": "2026-10-21T20:36:01Z"}
```

| `queued` | `reason` | Quando |
|---|---|---|
| `true` | `null` | o pedido entrou na fila |
| `false` | `recent` | coletado há menos de `REFRESH_MIN_AGE` |
| `false` | `already_queued` | pedido do mesmo alvo há menos de 60 s |

`target` é sempre o alvo (no `prefix`, o bloco do alvo, que pode ser maior que
o pedido) e `checked_at` o `last_scan_at` dele antes do pedido. Cabeçalhos:
`Cache-Control: no-store`, `X-Cache: BYPASS`, sem `ETag`. A marca de 60 s é
`badblock:registrobr-rdapnum:refresh:inflight:asn:61610` (ou
`...:inflight:prefix:200.225.48.0/21`).

Erros: 400 (validação da rota), 404 `<alvo> não é alvo do rdapnum`, 409
`target_inactive` (`<alvo> está desativado no rdapnum`), 503
`refresh_unavailable` (`atualização indisponível: Valkey fora do ar` ou
`fila de atualização cheia; tente mais tarde`).
