# API do modelo RIR

Vale para as cinco `api-<rir>` (o modelo `api-lacnic` e os clones). Leia
antes [../../padroes/api.md](../../padroes/api.md) e
[../../padroes/openapi.md](../../padroes/openapi.md): aqui fica só o que a
família faz a mais. As tabelas e o SQL de cada consulta estão em
[dados.md](dados.md#consultas-da-api); os valores de cada RIR, em
[README.md](README.md#parâmetros-das-apis); as respostas reais, os ETags, as
medições e o que for de um RIR só, em `specs/fontes/<rir>/api.md` — o do
modelo, [../lacnic/api.md](../lacnic/api.md), é de onde vêm os exemplos
abaixo. Dono: sub-agente `api-lacnic`.

Nomes: `<rir>` é a fonte (`ripencc`), `<RIR>` o mesmo em maiúsculas
(`RIPENCC`), `<Title>` o nome do RIR nos textos (`rir.Title`: `RIPE NCC`),
`<porta>` a porta no loopback e `<b>` o `BASE_PATH` configurado (`/<rir>`).

## Código

Os pacotes do [padrão](../../padroes/api.md#estrutura-do-código) mais
`internal/rir`, sem pacote extra. O resto do código não conhece o RIR: o que é
dele sai de `internal/rir/rir.go`:

| Constante | Valor | Onde aparece |
|---|---|---|
| `Source` | nome da fonte (`"lacnic"`) | `BasePath`; as tabelas `<rir>_*` no `--help`; a pasta `database/postgres/<rir>/` que os testes de integração aplicam |
| `App` | `"api-" + Source` | os usos do nome do app no [padrão](../../padroes/api.md#maingo) (log, `--version`, erros de configuração, `application_name`, `ClientName`, prefixo da chave de cache, `Server`); `app` do índice e da meta; `api-<rir> operacional` no `/status`; o `info.title` que o teste do manifesto confere |
| `Collector` | `"collector-" + Source` | a linha de `jobs` que a meta lê (`store.CollectorApp`) e o `collector.app` dela; as mensagens de `dataset_not_ready` e do `/status` `starting`; o `--help`; o `cmd/` que o `make test-real` compila |
| `Title` | nome do RIR nos textos (`"LACNIC"`, `"RIPE NCC"`) | `--help`, `registry` do índice, o fim das mensagens 404 (`... no arquivo do RIR <Title>`) e o aviso de `OpaqueIDChangesDaily` |
| `SourceURL` | arquivo delegated-extended do RIR | `source` do índice (a meta mostra a URL que o coletor gravou) |
| `OpaqueIDChangesDaily` | `true` só no RIR que gera opaque-ids novos a cada arquivo | o 404 do [titular](#titular) |
| `BasePath` | `"/" + Source` | padrão de `BASE_PATH`; o exemplo do erro `--base-path inválido`; o `healthcheck` com `BASE_PATH` vazia; o `base` dos testes |

- Literal, o nome do RIR só aparece onde a clonagem o troca por `sed`: o SQL
  de `internal/store` e dos testes de integração (`<rir>_run`, `<rir>_asn`,
  `<rir>_prefix`), o module Go, `cmd/api-<rir>/`, o `Makefile` (`APP`,
  `SOURCE`, `API_<RIR>_HOST_PORT`, `<RIR>_REAL_FILE`), o `Dockerfile`, o
  `docker-compose.yml`, o `.env.example`, o manifesto e comentários.
- Nenhum texto que deve ficar igual num clone cita um RIR (ex.: o comentário
  de `opaqueIDPattern`, que fala dos formatos dos cinco), e nenhum teste
  depende do RIR ([Testes](#testes)). Correção no código comum (todo pacote
  menos `internal/rir`) é feita no modelo e repetida nos quatro clones, cada
  um pelo seu sub-agente ([README.md](README.md#manutenção-dos-clones)).
- `internal/store` implementa a interface `Store` do `httpapi`: `Ping`,
  `Dataset` (a última execução aplicada; `nil` antes da primeira carga),
  `Job` (a linha do coletor em `jobs`; `nil` se não existe), `ASN`,
  `Covering` (o bloco mais específico que contém um prefixo) e `Holder`, com
  as consultas de [dados.md](dados.md#consultas-da-api); `store.ErrNotFound`
  quando não há registro.

## Rotas

| Rota | `operationId` | Tag | `summary` no manifesto | Chave de cache |
|---|---|---|---|---|
| `GET <b>/` | `getIndex` | `meta` | Índice da API | — |
| `GET <b>/asn/{asn}` | `getASN` | `dados` | Registro que contém um ASN | `asn:<n>` |
| `GET <b>/ip/{ip}` | `getIP` | `dados` | Bloco mais específico que contém um IP | `ip:<ip>` |
| `GET <b>/prefix/{ip}/{len}` | `getPrefix` | `dados` | Bloco mais específico que contém um prefixo | `prefix:<cidr>` |
| `GET <b>/holder/{opaque_id}` | `getHolder` | `dados` | Todos os recursos de um titular | `holder:<opaque_id>` |
| `GET <b>/meta` | `getMeta` | `meta` | Estado dos dados | — (sem cache) |
| `GET`, `POST <b>/health` | `getHealth`, `postHealth` | `saúde` | Saúde do serviço; Saúde do serviço (POST) | — |
| `GET`, `POST <b>/status` | `getStatus`, `postStatus` | `saúde` | Estado do serviço; Estado do serviço (POST) | — |
| `GET <b>/ping` | `ping` | `saúde` | Teste de vida | — |
| `GET <b>/openapi.yaml` | `getOpenAPI` | `meta` | Este manifesto | — |

- O índice, as quatro consultas e a meta respondem também em `<b>/v1/...`,
  com a mesma chave e o mesmo ETag; saúde, `ping` e manifesto só sem versão
  ([padrão](../../padroes/api.md#caminho-de-base-e-versões)). Chave completa:
  `badblock:api-<rir>:<versão>:<consulta>`.
- `Handler()` registra o titular como `/holder/{id}`; o manifesto chama o
  parâmetro de `opaque_id` (o teste do manifesto ignora o nome).
- Numa consulta: validação (400, antes de tudo), versão do dataset (503
  `dataset_not_ready`), ETag (304), Valkey e Postgres
  ([padrão](../../padroes/api.md#cache-valkey-cache-aside-e-servecached)).
  Sem registro: 404 com a mensagem da rota, fora do cache.

## Campos de delegação

Iguais em `/asn`, `/ip` e `/prefix` (e, sem o `opaque_id`, nos itens do
titular), das colunas de mesmo nome ([dados.md](dados.md#rir_asn)):

| Campo | Tipo | Conteúdo |
|---|---|---|
| `cc` | texto ou `null` | país ISO 3166-1 alfa-2 como publicado (`ZZ` e `EU` saem como vieram); `null` = vazio na fonte (available/reserved na maioria dos RIRs: [comparação](formato.md#comparação-entre-os-rirs)) |
| `reg_date` | `AAAA-MM-DD` ou `null` | data da alocação ou designação; `null` = vazia, `00000000` ou inválida na fonte |
| `status` | texto | `allocated`, `assigned`, `available` ou `reserved` |
| `opaque_id` | texto ou `null` | o titular dentro do RIR, como gravado (liga ASNs e blocos: [titular](#titular)); `null` em available/reserved |

## ASN

`GET <b>/asn/{asn}`: o **registro** do arquivo que contém o ASN.

- `asn`: número decimal de 0 a 4294967295, com ou sem o prefixo `AS` em
  qualquer caixa (`61613`, `AS61613`, `as61613`, `As61613`); zeros à
  esquerda valem (`061613`). Sinal, espaço, `AS` sozinho ou número acima do
  limite: 400 `ASN inválido: use um número de 0 a 4294967295, com ou sem o prefixo AS`.
- Chave `asn:<n>`, com o número sem o `AS` e sem zeros à esquerda:
  `/asn/61613`, `/v1/asn/AS61613` e `/asn/061613` são a mesma consulta.
- O registro é o de maior `asn_start` ≤ ASN, se o `asn_end` dele for ≥ ASN
  (as faixas não se sobrepõem na fonte). Sem ele — ASN de outro RIR ou num
  buraco entre faixas —, 404 `AS<n> não consta no arquivo do RIR <Title>`.

| Campo | Tipo | Conteúdo |
|---|---|---|
| `asn` | inteiro | o ASN pedido |
| `range` | objeto | a faixa do registro: `start` (`asn_start`), `end` (`asn_end`) e `count` (`asn_count`, ≥ 1). Quase todo registro tem um ASN só; as faixas de cada RIR estão na [comparação](formato.md#comparação-entre-os-rirs) |
| `cc`, `reg_date`, `status`, `opaque_id` | | [campos de delegação](#campos-de-delegação) |
| `first_seen` | timestamp | `created_at` da linha: quando o registro apareceu na fonte pela primeira vez (desde que o banco foi carregado) |
| `updated_at` | timestamp | `updated_at` da linha: a última mudança de quantidade, país, data, status ou titular |
| `dataset` | objeto | o do [padrão](../../padroes/api.md#formato-das-respostas) |

## IP

`GET <b>/ip/{ip}`: o bloco **mais específico** do arquivo que contém o IP.

- `ip`: IPv4 ou IPv6 que `netip.ParseAddr` lê, sem zona (`fe80::1%25eth0`)
  e, no IPv4, sem zeros à esquerda (`045.171.61.10`); fora disso, 400
  `endereço IP inválido`. IPv4 mapeado em IPv6 (`::ffff:a.b.c.d`) vira IPv4.
- Chave `ip:<ip>`, com o endereço normalizado: o IPv4 mapeado vira IPv4 e o
  IPv6 vai na forma canônica, em minúsculas e comprimida
  (`/ip/2804:5964:0:0::1` → `ip:2804:5964::1`).
- Consulta: o bloco de maior máscara com `prefix >>= <ip>/32` (ou `/128`).
  Sem bloco: 404 `<ip> não pertence a nenhum bloco no arquivo do RIR <Title>`,
  com o IP normalizado.

| Campo | Tipo | Conteúdo |
|---|---|---|
| `ip` | texto | o IP pedido, normalizado |
| `prefix` | texto (CIDR) | o bloco encontrado |
| `cc`, `reg_date`, `status`, `opaque_id` | | [campos de delegação](#campos-de-delegação) |
| `record` | objeto | o registro original do arquivo: `start` (endereço inicial, `host(record_start)`) e `value` (`record_value`: no IPv4, a quantidade de endereços; no IPv6, o tamanho do prefixo) |
| `dataset` | objeto | o do padrão |

- Um registro IPv4 que não forma CIDR vira vários blocos
  ([formato.md](formato.md#ipv4-divisão-em-cidrs)), e cada pedaço aponta para
  o mesmo `record`: `62.122.212.0/24` responde `record`
  `{"start": "62.122.208.0", "value": 1280}` (`/22` + `/24`). Quando o
  registro forma um CIDR, `record.start` é o endereço do próprio `prefix` (e,
  no IPv4, `record.value` é o tamanho do bloco). Quais RIRs têm registros
  não-CIDR e quantos (AFRINIC, ARIN e RIPE NCC em 2026-09-28):
  [comparação](formato.md#comparação-entre-os-rirs).
- Blocos available e reserved também respondem. Nenhuma resposta de bloco
  traz `first_seen` ou `updated_at`.

## Prefixo

`GET <b>/prefix/{ip}/{len}`: o bloco mais específico que contém **todo** o
prefixo pedido.

- `ip`: como em [`/ip`](#ip), e o IPv4 mapeado vira IPv4 **antes** de conferir
  o tamanho. `len`: inteiro (o que `strconv.Atoi` lê: `+22` e `022` valem) de
  0 a 32 no IPv4 e de 0 a 128 no IPv6 — `::ffff:45.171.60.0/22` consulta
  `45.171.60.0/22`, e `::ffff:45.171.60.0/120` é 400. Mensagens:
  `endereço IP inválido` e `tamanho de prefixo inválido`.
- Os bits de host são zerados (`/prefix/45.171.61.9/24` consulta
  `45.171.61.0/24`); chave `prefix:<cidr>`, com o CIDR normalizado.
- Consulta: o bloco de maior máscara com `prefix >>= <cidr>` (um bloco menor
  que o pedido não conta). Sem bloco: 404
  `<cidr> não está contido em nenhum bloco no arquivo do RIR <Title>`, com o
  CIDR normalizado (ex.: `0.0.0.0/0`).

Campos: `query` (o CIDR consultado, normalizado), `prefix`, `exact`
(booleano, `true` quando o bloco encontrado é o próprio `query`; um pedaço
de registro dividido também é exato), os
[campos de delegação](#campos-de-delegação), `record` e `dataset`, como em
[`/ip`](#ip).

## Titular

`GET <b>/holder/{opaque_id}`: todos os ASNs e blocos de um titular. O arquivo
do RIR não liga ASNs a blocos; o que os une é o opaque-id.

- `opaque_id`: `^[A-Za-z0-9._-]{1,128}$`; fora disso (longo demais, com
  espaço, `%2F` ou acento), 400
  `opaque_id inválido: use de 1 a 128 letras, dígitos, ponto, hífen ou sublinhado`
  (vazio nem chega à rota: `<b>/holder/` é o 404 genérico).
  Cobre os formatos dos cinco RIRs nos arquivos reais de 2026-09-28
  ([comparação](formato.md#comparação-entre-os-rirs)): número de 2 a 6 dígitos
  (LACNIC, `258500`), hex maiúsculo de 8 (AFRINIC, `F3619C8C`; APNIC, `A91DC5BE`), hex
  minúsculo de 32 (ARIN, `45fe880b68a8f2850ebdcfdc57b4556c`) e UUID
  minúsculo com hífens (RIPE NCC, `422db66e-88a2-489c-bf20-66c96d820c91`).
- Busca sem diferenciar maiúsculas: o store procura o opaque-id gravado entre
  o pedido, ele em maiúsculas e ele em minúsculas, preferindo o exato (e,
  entre os outros, o primeiro em ordem de texto;
  [dados.md](dados.md#consultas-da-api)). Acha o gravado todo maiúsculo ou
  todo minúsculo, que é o caso dos cinco RIRs (`f3619c8c` acha `F3619C8C`); um
  gravado em caixa mista só casa exato. A resposta traz o valor gravado.
- Chave `holder:<opaque_id como pedido>`, sem normalizar a caixa:
  `holder:F3619C8C` e `holder:f3619c8c` são chaves (e ETags) diferentes para
  a mesma resposta.
- Sem nenhum recurso: 404 `o titular <opaque_id> não consta no arquivo do RIR <Title>`,
  com o valor pedido. Com `OpaqueIDChangesDaily`, a mensagem continua com
  `; o <Title> gera opaque_id novos a cada arquivo diário: consulte <b>/ip/{ip} ou <b>/asn/{asn} para obter o atual`:
  nesse RIR o opaque-id só vale dentro do dataset atual
  ([RIPE NCC](../ripencc/fonte.md#opaque-id-novo-a-cada-arquivo)), a chave e
  o ETag mudam junto com ele (levam a versão) e o `updated_at` dos registros
  com titular muda a cada arquivo.

| Campo | Tipo | Conteúdo |
|---|---|---|
| `opaque_id` | texto | o valor gravado |
| `cc` | texto ou `null` | o país mais frequente (o primeiro de `ccs`); `null` quando nenhum recurso tem país |
| `ccs` | lista de textos | os países dos recursos, do mais frequente ao menos (empate: ordem alfabética); cada registro de ASN e cada bloco conta 1, recurso sem país não conta, `ZZ` e `EU` contam como qualquer país; `[]` sem nenhum |
| `counts` | objeto | `asns` (registros de ASN — faixas, não ASNs), `ipv4` e `ipv6` (blocos CIDR: um registro não-CIDR conta um por pedaço) |
| `asns` | lista | por `asn_start`: `start`, `end`, `count`, `cc`, `status`, `reg_date` |
| `prefixes` | objeto | `ipv4` e `ipv6`, cada lista por endereço: `prefix`, `cc`, `status`, `reg_date` |
| `dataset` | objeto | o do padrão |

- Listas vazias saem `[]`; os itens não trazem `opaque_id`, `record`,
  `first_seen` nem `updated_at`.
- Quase todo titular tem um país só: nos cinco RIRs, no máximo ~1% tem mais de
  um (2026-09-28). Ex.: `A92E1062`, da APNIC, com 1.104 blocos `CN` e 5 `HK` →
  `"cc": "CN", "ccs": ["CN", "HK"]`.
- Tamanho: o maior titular dos cinco arquivos de 2026-09-28 —
  `45fe880b68a8f2850ebdcfdc57b4556c`, da ARIN, com 152 registros de ASN,
  2.259 blocos IPv4 e 23 IPv6 — responde 203.731 bytes (≈ 204 KB); o maior de
  cada um dos outros fica entre 16 KB e 164 KB (o de cada RIR está no
  `api.md` dele). Tudo muito abaixo dos 8 MiB do cache.

## Meta

`GET <b>/meta` segue o [padrão](../../padroes/api.md#rotas-comuns): sem
cache, lida do banco a cada pedido, com 503 `database_unavailable` e 504
`timeout` como as consultas (nunca `dataset_not_ready`).

| Campo | Tipo | Origem |
|---|---|---|
| `app`, `version` | texto | `api-<rir>` e a versão do binário (`dev` sem tag) |
| `dataset` | objeto ou `null` | a última linha `status = 1` de `<rir>_run`; `null` antes da primeira carga |
| `dataset.version`, `dataset.updated_at` | uuid, timestamp | `uuid` e `created_at` |
| `dataset.source` | texto | `url`: de onde o coletor baixou o arquivo |
| `dataset.sha256`, `dataset.md5` | texto ou `null` | hashes do arquivo, hex minúsculo |
| `dataset.serial` | texto ou `null` | o `serial` do cabeçalho como o RIR publica (data `AAAAMMDD`, época Unix em segundos ou em milissegundos) |
| `dataset.start_date`, `dataset.end_date` | `AAAA-MM-DD` ou `null` | `startdate` e `enddate` do cabeçalho (vazio ou `00000000` → `null`) |
| `dataset.asn_records`, `dataset.ipv4_records`, `dataset.ipv6_records` | inteiro ou `null` | registros aceitos do arquivo, por tipo |
| `dataset.prefixes_v4`, `dataset.prefixes_v6` | inteiro ou `null` | blocos gravados (um registro IPv4 não-CIDR vira vários) |
| `collector` | objeto ou `null` | a linha `collector-<rir>` de `jobs`; `null` se não existe |
| `collector.app` | texto | `collector-<rir>` |
| `collector.last_sync_at`, `collector.last_check_at` | timestamp ou `null` | colunas de mesmo nome |
| `collector.consolidated` | booleano | `jobs.consolidated = 1` |

## Índice

`GET <b>/` e `<b>/v1/`, sem banco (responde antes da primeira carga):

| Campo | Conteúdo |
|---|---|
| `app`, `version` | `api-<rir>` e a versão do binário |
| `registry` | `rir.Title` (`LACNIC`, `RIPE NCC`) |
| `base_path` | `BASE_PATH` |
| `versions` | `["v1"]` |
| `endpoints` | nesta ordem, com o `BASE_PATH` na frente: `/asn/{asn}`, `/ip/{ip}`, `/prefix/{ip}/{len}`, `/holder/{opaque_id}`, `/meta`, `/status`, `/openapi.yaml` |
| `source` | `rir.SourceURL` (constante; não vem do banco) |

## Erros

Os `code` e os casos são os do [padrão](../../padroes/api.md#erros); as
mensagens, que também vão para os exemplos do manifesto:

| HTTP e `code` | Mensagem |
|---|---|
| 400 `bad_request` | a da validação de cada consulta (acima) |
| 404 `not_found` | a de cada consulta (acima); rota: `rota inexistente; veja <b>/` |
| 503 `dataset_not_ready` | `a primeira sincronização do collector-<rir> ainda não terminou; tente em alguns minutos` |
| 503 `database_unavailable` | `banco de dados indisponível` |
| 504 `timeout` | `a consulta demorou demais` |
| 500 `internal_error` | `erro interno` |

## Manifesto

`openapi/openapi.yaml` segue o [padrão](../../padroes/openapi.md) e é igual
nos cinco RIRs, a menos do que vem do `api.md` de cada um (exemplos, porta e
particularidades):

| Parte | Conteúdo |
|---|---|
| `info` | `title: api-<rir>`, `version: '0.1.0'`, licença MIT, `summary: Delegações de ASNs e blocos IP do RIR <Title>, do BadBlock.` (no RIPE NCC, `do RIPE NCC`) e uma `description` com o que a API serve (o arquivo delegated-extended do RIR, importado pelo `collector-<rir>`, com país, data, status e titular; só leitura, pública, sem chave, rate limit no proxy) e, em itens: caminho de base e versões (a v1 fixa é o servidor `/v1`; saúde e manifesto fora; o 301 e o 404 JSON), `HEAD` e `OPTIONS` (204), cache e ETag com o 304, formato do JSON, cabeçalhos de toda resposta e formato dos erros |
| `externalDocs` | `description: Especificações da fonte (fonte da verdade)`, `url: https://github.com/patrickbrandao/badblock/tree/main/specs/fontes/<rir>` |
| `servers` | os quatro do padrão, com `<porta>`: `Produção, versão atual (hoje v1)`, `Produção, v1 fixa (índice, consultas e meta)`, `Desenvolvimento local, versão atual (porta publicada no loopback pelo docker-compose.yml)` e `Desenvolvimento local, v1 fixa`; nos paths fora do versionamento, os dois sem `/v1`, com `Produção` e `Desenvolvimento local` |
| `tags` | `dados` (`Consultas às delegações do RIR, com cache e ETag.`), `meta` (`Índice, estado dos dados e este manifesto.`), `saúde` (`Saúde do serviço, fora do versionamento.`) |
| `paths` | na ordem de [Rotas](#rotas). Parâmetros no path item, com o `IfNoneMatch`: `asn` (texto, `pattern: '^([Aa][Ss])?[0-9]+$'`), `ip` (texto), `len` (inteiro de 0 a 128) e `opaque_id` (texto, `pattern: '^[A-Za-z0-9._-]{1,128}$'`, `minLength: 1`, `maxLength: 128`), cada um com a validação na descrição e um `example` do RIR |
| respostas | consultas: 200 (`ETag`, `Cache-Control` público, `X-Cache`, `X-Dataset-Version`, `examples` do RIR), 304, 400 e 404 (o `$ref` mais uma `description` da rota), 503, 504, 500; `/meta`: 200 (`no-store`; exemplos `carregado` e `vazio`), 503 `DatabaseUnavailable`, 504, 500; índice: 200 (público), 500; saúde (GET e POST): 200 `StatusOK`, 503 `StatusError`, 500; `ping`: 200 `text/plain` (`const: pong`), 500; manifesto: 200 `application/yaml`, 500. Descrições das respostas 200: `Registro encontrado.` (`/asn`), `Bloco encontrado.` (`/ip`, `/prefix`), `Recursos do titular.` (`/holder`), `Processo no ar.` (saúde); 400 e 404 com a regra da rota (ex.: `ASN fora de 0 a 4294967295 ou em outro formato.`). `Cache-Control` declarado também no 200 de `/ping` e de `/openapi.yaml` e nos componentes de erro e de saúde |
| `components.parameters`, `headers` | `IfNoneMatch`; `ETag`, `CacheControlPublic` (`const: 'public, max-age=300'`), `CacheControlNoStore` (`const: no-store`), `XCache` (`enum: [HIT, MISS, BYPASS]`), `XDatasetVersion` (`format: uuid`) |
| `components.responses` | `NotModified`; `BadRequest` (exemplos `asn`, `ip`, `len`, `opaque_id`); `NotFound` (`asn`, `ip`, `prefix`, `holder`); `ServiceUnavailable` (`dataset_not_ready`, `database_unavailable`); `DatabaseUnavailable`; `Timeout`; `InternalError`; `StatusOK` (`ok`, `starting`, `degraded`); `StatusError` |
| `components.schemas` | `Error` (`code` com o `enum` dos seis), `Dataset`, `CountryCode` (`type: [string, 'null']`, `pattern: '^[A-Z]{2}$'`), `RegDate` (`[string, 'null']`, `format: date`), `DelegationStatus` (`enum` dos quatro), `OpaqueID` (`[string, 'null']`), `IPAddress` (`anyOf` de `format: ipv4` e `ipv6`), `IPPrefix`, `Range` e `HolderASN` (`int64` de 0 a 4294967295, `count` ≥ 1), `Record` (`value` ≥ 1), `ASNResponse`, `IPResponse`, `PrefixResponse`, `HolderResponse`, `HolderCounts`, `HolderPrefix`, `HolderPrefixes`, `MetaResponse` (`dataset` e `collector` como `oneOf` do objeto ou `null`), `MetaDataset` (`sha256` e `md5` com `pattern` de hex minúsculo de 64 e 32), `MetaCollector`, `Status` (`checks` com os `enum` do padrão) e `IndexResponse`. Timestamps com `format: date-time`, datas com `format: date`, versões com `format: uuid` e `source` com `format: uri`. Todo campo das respostas é `required`: a API sempre o manda (`null` quando é o caso). Restrições que não saem dos nomes: `ASNResponse.asn` `int64` de 0 a 4294967295; `minimum: 0` em `HolderCounts` e nos contadores de `MetaDataset` (`[integer, 'null']`); `ccs.items` com `pattern: '^[A-Z]{2}$'`; `HolderResponse.opaque_id` é `string` (não `OpaqueID`, que admite `null`); `Record.start` e `ip` são `IPAddress`; `query` e `prefix`, `IPPrefix` |
| exemplos | as respostas reais do `api.md` do RIR; os erros com as mensagens reais (o 404 de ASN com um ASN que não é do RIR — `AS1` é da ARIN); o `X-Dataset-Version` dos exemplos; o `ETag` e o `If-None-Match` de uma consulta dos exemplos, por `etagFor` com a versão deles. Rotas com mais de um caso usam `examples` (cada um com `summary`); `/holder` e o índice têm um `example` só. `summary` dos exemplos: `Registro de um ASN só (/asn/<asn>)` e `ASN no meio de uma faixa (/asn/<asn>)` (só nos RIRs com faixas), `IPv4 (/ip/<ip>)`, `IPv6 (/ip/<ip>)`, `Prefixo contido num bloco maior (/prefix/<cidr>)`, `O próprio bloco (/prefix/<cidr>)`; `Com dados` e `Antes da primeira carga` (meta); `Tudo no ar`, `Antes da primeira carga` e `Valkey fora` (`StatusOK`); `Antes da primeira carga` e `PostgreSQL fora` (`ServiceUnavailable`); nos erros, o próprio pedido (`/asn/abc`, `/ip/x`...) |
| particularidades | uma que muda o uso de uma rota ganha texto próprio; com `OpaqueIDChangesDaily`, o aviso do opaque-id diário entra na `description` do `info`, no parâmetro, na descrição e no 404 do `/holder` e na descrição do `/asn` (o `updated_at` que muda todo dia); um fato do RIR que muda a leitura de um campo entra também na descrição do schema (no RIPE NCC: `CountryCode` com `EU`, `OpaqueID` com o UUID diário, `MetaDataset.serial` com a época em segundos), e um caso do RIR sem par no modelo ganha exemplo próprio com `summary` — e `description` quando o caso precisa de explicação, como o `nao_cidr` de `/ip` na ARIN e no RIPE NCC (ex.: no RIPE NCC, `nao_cidr` em `/ip`, e `disponivel` no lugar de `faixa` em `/asn`, que não tem faixas) |

## Opções, compose e `.env`

As [comuns](../../padroes/api.md#configuração-comum); a família não tem opção
própria. `BASE_PATH` vale `/<rir>` (`rir.BasePath`), ganha a barra do começo
e perde a do fim (`<rir>` e `/<rir>/` viram `/<rir>`). O `--help` (no stderr,
saída 0) começa com
`api-<rir> — API HTTP das delegações de ASNs e blocos IP do RIR <Title>.`,
cita as tabelas `<rir>_*`, o `collector-<rir>` e o `BASE_PATH` e lista as
opções em ordem alfabética, cada uma com `env <VARIÁVEL>, padrão <valor>`
(saída real em [../lacnic/api.md](../lacnic/api.md#--help)). Opção nova da
família entra aqui, nos cinco apps e nos lugares de
[../../projeto/convencoes.md](../../projeto/convencoes.md#configuração).

`docker-compose.yml`, `.env.example`, labels do Traefik e `Makefile` são os
moldes de [../../plataforma/docker.md](../../plataforma/docker.md#api) e
[../../plataforma/publicacao.md](../../plataforma/publicacao.md#traefik) com
`<rir>`, `<RIR>`, `<porta>` e `SMOKE_ASN ?= <ASN do RIR>`; os valores de cada
RIR estão no `api.md` dele.

## Testes

Camadas do [padrão](../../padroes/api.md#testes). Os testes não dependem do
RIR: usam as constantes de `rir.go` em vez do nome, e os dados são inseridos
pelo próprio teste — do recorte do modelo
([../lacnic/fonte.md](../lacnic/fonte.md#recorte-testdatadelegated-extended-sampletxt)),
iguais nos cinco — ou tirados do arquivo real.

| Arquivo | Casos |
|---|---|
| `httpapi_test.go` | store falso com AS28003–AS28005 (available) e AS61613, `45.171.60.0/22`, `200.192.152.0/22` e `2804:5964::/32` do titular `258500`, mais o que nem todo RIR tem: `62.122.208.0` + 1280 em `/22` + `/24` (`ZZ`, reserved) e o titular `A92E1062` com três blocos (`HK`, `CN`, `CN`); versão `0192-v1`; `BASE_PATH` `rir.BasePath + "/"`. `TestASN` (as quatro formas, `/v1`, faixa, `null`s), `TestErrors` (400 e 404 de cada rota, `/v2`, rota inexistente, fora do caminho de base; o 404 cita `rir.Title` e traz o aviso do titular só com `OpaqueIDChangesDaily`), `TestIPAndPrefix` (IPv4, mapeado, IPv6, pedaço de registro dividido, bits de host, `exact`), `TestHolder` e `TestHolderCountryTie` (contagens, `[]`, dois países, empate `CA`/`US`, sem país), `TestHead` (servidor HTTP de verdade), `TestCacheAndETag` (`MISS` e `HIT` pela `/v1` com uma consulta só, chave `badblock:api-<rir>:0192-v1:asn:61613`, 304 com o ETag, sem `W/`, numa lista e `*` sem consultar, 404 fora do cache), `TestCacheDisabledBypass`, `TestNotReady`, `TestStoreFailures` (503 e 504 nas consultas e na meta, fora do cache), `TestPanicIsRecovered`, `TestStatus` (ok, sem cache, starting, degraded e error, GET e POST, `/health` e `/status`; `ping`; `/v1/ping` 404), `TestCORSAndHeaders`, `TestIndexMetaAndRedirect` (índice com e sem `/v1`, 301, meta com e sem dados) e `TestHolderAcceptsEveryRIRFormat` |
| `openapi_test.go` | `TestOpenAPIMatchesRoutes`, `TestOpenAPIDocument` e `TestOpenAPIRoute`, com as regras de [../../padroes/openapi.md](../../padroes/openapi.md#teste) (a porta vem do `PORT` do `Makefile`) |
| `config_test.go`, `cache_test.go`, `dataset_test.go`, `realip_test.go` | padrões (`/<rir>`, 8080, 1 h, 50 ms), normalização do `BASE_PATH`, precedência, inválidos (sem `POSTGRES_URL`, `BASE_PATH=/`, porta 0, TTL −1, proxy `lixo`, formato `xml`), `--help` com toda opção e variável e sem `%!`; disjuntor (3 falhas no teste) e `Noop`; releitura da versão (erro mantém a conhecida) e `Run`; IP real (direto, via Traefik, `X-Forwarded-For` forjado à esquerda, `X-Real-IP`, inválido, mapeado; `/33` e `Forwarded` recusados) |
| `store_integration_test.go` (`make test-int`) | PG18 (`postgres:18-trixie`, banco `badblock`, usuário `postgres`, senha `pg`) com o `migrate:up` de `central/` e `<rir>/` e um seed: o recorte do modelo (titulares `258500` e `130343`, faixas available), um registro dividido, um bloco aninhado (`150.165.10.0/24`, titular `999`, dentro de `150.165.0.0/16`), `ZZ`, a faixa `4294967294` + 2 e o hex maiúsculo `F367B216`; duas execuções aplicadas e uma recusada. `TestQueries`: sem carga (`nil`), `Dataset` (a última aplicada), `Job`, `ASN` (início, meio e fim de faixa; fora), `Covering` (exato, aninhado, dividido, IPv6, fora, `::/0`), `Holder` (ordem, só blocos, `F367B216`/`f367b216`/`F367b216`, inexistentes) e contexto cancelado |

### Arquivo real (`make test-real`)

```bash
curl -o /tmp/delegated <SourceURL do RIR>
make test-real FILE=/tmp/delegated
```

Sem `FILE`: `use: make test-real FILE=/caminho/do/delegated-<rir>-extended-latest (baixe com curl)`,
saída 1. Com ele:
`<RIR>_REAL_FILE=<caminho absoluto> go test -count=1 -tags integration -run RealFile -v ./internal/httpapi/`.
`TestRealFile` (`realfile_integration_test.go`), pulado sem a variável:

1. Sobe um PG18 descartável com as migrations, compila o coletor irmão
   (`../collector`, `./cmd/collector-<rir>`) e carrega o arquivo com
   `--once`, servido por `httptest.Server` (`SOURCE_MD5_URL=off`); loga o
   tempo da carga e roda `ANALYZE`.
2. A meta tem de bater com as tabelas (registros de ASN, blocos IPv4 e IPv6).
3. Amostras tiradas dos dados: o ASN do meio entre os que têm titular (`/asn`
   e `/v1/asn/AS...`), o meio da maior faixa (`count` ≥ 2), se houver — na
   AFRINIC e no RIPE NCC, sem faixas, o passo é pulado com o log
   `sem amostra para ...` —, um IPv4 e um IPv6
   do meio, o primeiro bloco IPv4 como prefixo (tem de ser `exact`) e, se
   houver, um pedaço de registro dividido (`record.start` ≠ o IP).
4. O maior titular (ASNs + blocos): `counts` batem com as tabelas e a resposta
   cabe em 1/10 dos 8 MiB do cache.
5. Média de 500 `/ip` e de 500 `/asn` aleatórios (ASN abaixo de 400.000), sem
   cache.
6. `EXPLAIN` das consultas por ASN (com o AS61613 fixo; o plano não depende
   dele), por IP e por titular nas duas tabelas: tem de usar índice (`Index`
   ou `Bitmap`).

Os tempos e o maior titular viram as medições do `api.md` do RIR.

## Clonar a API para outro RIR

Com `specs/fontes/<rir>/`, `database/postgres/<rir>/` e o `collector-<rir>`
prontos ([collector.md](collector.md#clonar-o-coletor-para-outro-rir)):

1. **Spec**: `specs/fontes/<rir>/api.md`, que aponta este modelo e traz só o
   que é do RIR — os valores (`rir.go`, porta, `SMOKE_ASN`) e as
   particularidades que mudam uma resposta —, a seção "API" do `README.md` da
   pasta e o RIR em [Parâmetros das APIs](README.md#parâmetros-das-apis).
2. **Cópia**, sem `bin/`, e troca de nomes em todo arquivo copiado (o
   manifesto também) — a URL da fonte inteira **antes** dos nomes, porque o
   `sed` simples a estraga na APNIC (sem `/pub`) e no RIPE NCC
   (`ftp.ripe.net`):

   ```bash
   rsync -a --exclude bin/ apps/lacnic/api/ apps/<rir>/api/
   mv apps/<rir>/api/cmd/api-lacnic apps/<rir>/api/cmd/api-<rir>
   # em cada arquivo copiado:
   sed 's|https://ftp.lacnic.net/pub/stats/lacnic/delegated-lacnic-extended-latest|<URL do RIR>|g; s/LACNIC/<RIR>/g; s/Lacnic/<Rir>/g; s/lacnic/<rir>/g'
   ```

3. **Confira à mão**:
   - `internal/rir/rir.go`: `Title` (o nome nos textos: `RIPE NCC`, não
     `RIPENCC`), `SourceURL` e `OpaqueIDChangesDaily`; e o comentário do
     pacote, que o `sed` estraga (`lacnic → <rir>` vira `<rir> → <rir>`): no
     clone ele fica `(lacnic → <rir>, LACNIC → <RIR>, Lacnic → <Rir>) a partir
     da api-lacnic (o modelo)`;
   - a porta, 8105 → `<porta>`: `Makefile` (`PORT`), `docker-compose.yml`,
     `.env.example` e os servidores locais do manifesto (os dois da raiz e os
     de `/health`, `/status`, `/ping` e `/openapi.yaml`: todo
     `127.0.0.1:8105/`); o `openapi_test.go` confere a porta contra o `PORT`;
   - `SMOKE_ASN` no `Makefile` (e o comentário do alvo `smoke`), com um ASN do
     RIR;
   - textos que o `sed` estraga: `do RIR RIPENCC` → `do RIPE NCC` no
     `Dockerfile` (label `description`), no `info` do manifesto e nos
     comentários;
   - as particularidades do RIR no manifesto ([Manifesto](#manifesto));
   - `README.md` do app: os `curl` com consultas do RIR, os links para
     `specs/fontes/<rir>/api.md` e "clone da `api-lacnic`" no lugar de
     "modelo das APIs dos outros RIRs".
4. **Exemplos e medições**: com o arquivo real carregado (`make test-real`,
   que loga as amostras, ou o binário contra um PG carregado pelo coletor),
   ponha no `api.md` do RIR as respostas reais de cada rota (com a data do
   arquivo), os ETags, o maior titular, a saída do `--help` se ela mudar além
   dos nomes e as medições; e troque os `example`/`examples` do manifesto
   (parâmetros, cabeçalhos e respostas) pelos mesmos, inclusive os erros (o
   404 de ASN com um ASN que não é do RIR: `AS1` existe na ARIN), o ETag de
   exemplo e os textos que citam um exemplo da LACNIC: as descrições dos
   parâmetros `asn` (`61613`, `AS61613` e `as61613`) e `ip` do `/prefix`
   (`45.171.61.9/24` consulta `45.171.61.0/24`), o fim da descrição do
   `/prefix` (`/prefix/2804:5964::/32`), as do cabeçalho `ETag`
   (`/lacnic/asn/61613` e `/lacnic/v1/asn/AS61613`) e do `IPPrefix`
   (`45.171.60.0/22`, `2804:5964::/32`) e os `summary` dos exemplos, inclusive
   o `len` do `BadRequest` (`/prefix/45.171.60.0/33`) e o `faixa`
   (`/asn/28004`), que só vale num RIR com faixas.
5. **Sub-agente** `.claude/agents/api-<rir>.md`, no molde do `api-lacnic`,
   sem a parte de modelo.
6. **Integração na raiz** (sessão principal):
   [../../processos/nova-fonte.md](../../processos/nova-fonte.md#6-integração-na-raiz).
7. **Conferir**: `make test test-int lint vet` e `go build ./...` no app,
   `make test-real FILE=<arquivo do dia>`, `make smoke` com o stack no ar e
   `make specs-check`.
