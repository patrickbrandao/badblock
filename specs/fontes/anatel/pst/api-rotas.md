# anatel/pst — rotas da API

Cada rota da `api-anatel-pst`: parâmetros, validação, normalização, chave de
cache, campos da resposta, exemplos reais e erros. O que vale para toda API
(formato do JSON, `HEAD`, `OPTIONS`, cabeçalhos, `serveCached`, ETag, saúde,
formato dos erros) está no [padrão](../../../padroes/api.md); valores,
cache, consultas, manifesto, medições e testes desta API, em
[api.md](api.md); o SQL, em [dados.md](dados.md#consultas-da-api-anatel-pst).
Dono: sub-agente `api-anatel-pst`.

Os exemplos são respostas reais com o arquivo de 2026-09-30 (45.074
prestadoras, 54.314 serviços), carregado pelo `collector-anatel-pst` num
banco novo em 2026-10-01T01:33:48Z (versão do dataset
`01a0f518-b4d2-7810-80b5-b58a6c2e4614`). Estão formatados para leitura — a
API responde JSON compacto, numa linha — e as listas longas foram cortadas
em `…`. `first_seen` e `updated_at` contam desde a primeira carga **deste
banco**.

## Rotas

| Rota | Métodos | O quê | Chave de cache |
|---|---|---|---|
| `/anatel/pst/provider/{cnpj}` | GET | uma prestadora e todos os seus serviços | `provider:<cnpj>` |
| `/anatel/pst/services` | GET | catálogo de serviços | `services` |
| `/anatel/pst/service/{code}[?state=UF]` | GET | as prestadoras de um serviço | `service:<código>[:<UF>]` |
| `/anatel/pst/search?q={texto}[&limit=N]` | GET | busca por razão social ou nome fantasia | `search:<limit>:<termo>` |
| `/anatel/pst/meta` | GET | estado dos dados | sem cache |
| `/anatel/pst/` | GET | índice | sem cache |
| `/anatel/pst/health`, `/anatel/pst/status` | GET, POST | saúde | — |
| `/anatel/pst/ping` | GET | `pong` | — |
| `/anatel/pst/openapi.yaml` | GET | manifesto OpenAPI 3.1 | — |

As seis primeiras respondem também em `/anatel/pst/v1/...`, com o mesmo
conteúdo, a mesma chave e o mesmo ETag; as três últimas ficam fora do
versionamento. Todo `GET` aceita `HEAD`.

Nas quatro rotas de dados:

- A validação vem **antes de tudo**: pedido inválido recebe 400 mesmo antes
  da primeira carga do coletor. Depois vêm, nesta ordem, a versão do dataset
  (503 `dataset_not_ready`), o `If-None-Match` (304), o Valkey e o Postgres.
- Colunas ausentes na fonte saem `null`, nunca `""`
  ([dados.md](dados.md#anatel_pst_provider)).
- Toda resposta 200 traz o bloco `dataset` (`version`, `updated_at`) do
  [padrão](../../../padroes/api.md#formato-das-respostas).
- `ProviderBrief`, a prestadora resumida das listas de `/service` e
  `/search`, tem `document`, `name`, `trade_name`, `city` e `state` (os três
  últimos texto ou `null`).

## `GET /anatel/pst/provider/{cnpj}`

A prestadora de um CNPJ e todos os serviços que ela notificou.

- `{cnpj}`: todo caractere que não é dígito ASCII (`0`–`9`) é descartado;
  sobram exatamente 14 dígitos, senão 400 — como em
  [`/cgibr/document/{doc}`](../../cgibr/api-rotas.md#documento--get-cgibrdocumentdoc),
  sem o identificador estrangeiro de 8 dígitos. Vale só dígitos
  (`02558157000162`) ou o CNPJ formatado com a barra codificada
  (`02.558.157%2F0001-62`); com a barra sem codificar o caminho ganha um
  segmento e cai no 404 `rota inexistente; veja /anatel/pst/`. Como os não
  dígitos são descartados, `cnpj02558157000162` e
  `02%20558%20157%200001%2062` também valem; dígitos de outros alfabetos
  (`١٢٣`) não contam. Os dígitos verificadores não são conferidos (vale o
  cadastro da Anatel).
- Chave: `provider:<14 dígitos>`; a consulta usa os mesmos dígitos.

| Campo | Tipo | Significado |
|---|---|---|
| `document` | texto, 14 dígitos | CNPJ (`anatel_pst_provider.document`) |
| `name` | texto | razão social |
| `trade_name` | texto ou `null` | nome fantasia (`null` quando a fonte traz `N/I`) |
| `address` | objeto | sede: `street`, `number` (texto: `S/N`, `1740A`), `complement`, `district`, `postal_code` (dígitos, como publicado), `city_ibge_code` (inteiro de 7 dígitos), `city`, `state` (UF) — cada um `null` quando ausente |
| `phone` | texto ou `null` | telefone principal, texto livre como publicado |
| `email` | texto ou `null` | e-mail como publicado |
| `first_seen` | timestamp | `anatel_pst_provider.created_at`: quando **este banco** viu o CNPJ pela primeira vez |
| `updated_at` | timestamp | `anatel_pst_provider.updated_at`: última mudança dos dados da prestadora |
| `service_codes` | lista de texto | os códigos distintos de `services`, em ordem (`["010", "045", …]`) |
| `services` | lista | os serviços, em ordem de `service_code`, `notified_on` e `notification_fistel` (campos abaixo) |
| `dataset` | objeto | versão dos dados |

Cada item de `services` (uma linha de `anatel_pst_service`):

| Campo | Tipo | Significado |
|---|---|---|
| `service_code` | texto, 3 dígitos | código do serviço (`045` = SCM, `171` = STFC, `010` = SMP, `750` = SeAC) |
| `service_name` | texto | nome do serviço, como publicado |
| `service_group` | texto | grupo do serviço (`Banda Larga Fixa`, `Telefonia Fixa`…) |
| `notification_fistel` | texto, 11 dígitos | Fistel da notificação |
| `notification_process` | texto ou `null` | processo SEI da notificação |
| `notified_on` | data ou `null` | data de inclusão da notificação |
| `entity_type` | texto | `Outorgada` ou `Dispensada de Outorga` |
| `grant_type` | texto | tipo de outorga (`Serviços de Interesse Coletivo e Restrito - SIC`, `Serviços de Interesse Restrito - SIR` ou `Dispensada de Outorga`) |
| `grant_fistel` | texto (11 dígitos) ou `null` | Fistel da outorga; `null` quando dispensada |
| `grant_process` | texto ou `null` | processo SEI da outorga; `null` quando dispensada |
| `granted_on` | data ou `null` | data de inclusão da outorga; `null` quando dispensada |

O mesmo código pode aparecer várias vezes: com Fistéis de notificação
diferentes (a Telefônica tem 16 linhas do `053`) ou com a mesma notificação
sob duas outorgas (188 linhas do arquivo — [fonte.md](fonte.md#fatos-medidos-2026-09-30)).
`GET /anatel/pst/provider/02558157000162` (3 dos 42 serviços):

```json
{
  "document": "02558157000162",
  "name": "TELEFONICA BRASIL S.A.",
  "trade_name": null,
  "address": {
    "street": "Avenida Engenheiro Luiz Carlos Berrini",
    "number": "1376",
    "complement": "Telefônica Brasil S/A",
    "district": "Cidade Monções",
    "postal_code": "04571936",
    "city_ibge_code": 3550308,
    "city": "São Paulo",
    "state": "SP"
  },
  "phone": "(11) 3430-4532",
  "email": "cadastro.fiscal.br@telefonica.com",
  "first_seen": "2026-10-01T01:33:48Z",
  "updated_at": "2026-10-01T01:33:48Z",
  "service_codes": ["010", "011", "019", "045", "046", "053", "171", "175", "176", "181", "750"],
  "services": [
    {"service_code": "010", "service_name": "SERVIÇO MOVEL PESSOAL", "service_group": "Telefonia Móvel", "notification_fistel": "50409146285", "notification_process": "535000247042011", "notified_on": "2012-04-03", "entity_type": "Outorgada", "grant_type": "Serviços de Interesse Coletivo e Restrito - SIC", "grant_fistel": "50423150120", "grant_process": "53500036134202046", "granted_on": "2021-01-05"},
    {"service_code": "010", "service_name": "SERVIÇO MOVEL PESSOAL", "service_group": "Telefonia Móvel", "notification_fistel": "50409146366", "notification_process": "535000247042011", "notified_on": "2012-04-03", "entity_type": "Outorgada", "grant_type": "Serviços de Interesse Coletivo e Restrito - SIC", "grant_fistel": "50423150120", "grant_process": "53500036134202046", "granted_on": "2021-01-05"},
    …
    {"service_code": "045", "service_name": "Serviço de Comunicação Multimídia", "service_group": "Banda Larga Fixa", "notification_fistel": "50013053736", "notification_process": "535000020652002", "notified_on": "2003-02-13", "entity_type": "Outorgada", "grant_type": "Serviços de Interesse Coletivo e Restrito - SIC", "grant_fistel": "50423150120", "grant_process": "53500036134202046", "granted_on": "2021-01-05"},
    …
  ],
  "dataset": {"version": "01a0f518-b4d2-7810-80b5-b58a6c2e4614", "updated_at": "2026-10-01T01:33:48Z"}
}
```

Serviço dispensado de outorga — o `190` de
`GET /anatel/pst/provider/21557625000129` (TRANSAT, `service_codes`
`["045", "110", "182", "190"]`):

```json
{"service_code": "190", "service_name": "Limitado Privado - Dispensa de Autorização", "service_group": "Limitado Privado - Dispensa de Outorga", "notification_fistel": "50418027773", "notification_process": "53500018606201945", "notified_on": "2019-05-10", "entity_type": "Dispensada de Outorga", "grant_type": "Dispensada de Outorga", "grant_fistel": null, "grant_process": null, "granted_on": null}
```

| HTTP | Mensagem |
|---|---|
| 400 | `CNPJ inválido: use os 14 dígitos, com ou sem pontuação (ex.: 02558157000162 ou 02.558.157%2F0001-62)` |
| 404 | `CNPJ <14 dígitos> não consta no cadastro de prestadoras da Anatel` (`/provider/00.000.000%2F0000-00` → `CNPJ 00000000000000 não consta no cadastro de prestadoras da Anatel`) |

## `GET /anatel/pst/services`

O catálogo: um item por código de serviço do arquivo, em ordem de código.
Sem parâmetros (55 itens, 8.512 bytes em 2026-09-30).

- Chave: `services`.
- Nome e grupo de cada código: `min()` das linhas do código — cada código
  tem um nome e um grupo só na fonte ([api.md](api.md#consultas)).

| Campo | Tipo | Significado |
|---|---|---|
| `count` | inteiro | códigos na lista |
| `services` | lista | cada um com `service_code`, `service_name`, `service_group`, `providers` (prestadoras — CNPJs distintos — com o código) e `services` (serviços notificados, as linhas) |
| `dataset` | objeto | versão dos dados |

```json
{
  "count": 55,
  "services": [
    {"service_code": "010", "service_name": "SERVIÇO MOVEL PESSOAL", "service_group": "Telefonia Móvel", "providers": 27, "services": 100},
    {"service_code": "011", "service_name": "Limitado Privado - Prestação a terceiros", "service_group": "Limitado Privado", "providers": 552, "services": 564},
    …
    {"service_code": "045", "service_name": "Serviço de Comunicação Multimídia", "service_group": "Banda Larga Fixa", "providers": 20510, "services": 20856},
    …
    {"service_code": "171", "service_name": "SERVICO TELEFONICO FIXO COMUTADO", "service_group": "Telefonia Fixa", "providers": 1661, "services": 1740},
    …
    {"service_code": "750", "service_name": "Serviço de Acesso Condicionado", "service_group": "Tv por Assinatura", "providers": 1446, "services": 1515},
    {"service_code": "820", "service_name": "DE TELEV. EM CIRCUITO FECHADO (RADIOENLACE)", "service_group": "DE TELEV. EM CIRCUITO FECHADO (RADIOENLACE)", "providers": 5, "services": 5}
  ],
  "dataset": {"version": "01a0f518-b4d2-7810-80b5-b58a6c2e4614", "updated_at": "2026-10-01T01:33:48Z"}
}
```

Sem 400 nem 404.

## `GET /anatel/pst/service/{code}`

As prestadoras que notificaram o serviço, em ordem de CNPJ, cada uma uma vez
(mesmo com várias linhas do código): a lista completa, sem paginação e sem
parâmetro de limite (o SCM, `045`, tem 20.510 prestadoras e 2.730.896 bytes
— [medições](api.md#medições)).

- `{code}`: de 1 a 3 dígitos ASCII, completados com zeros à esquerda (`45`
  e `045` são a mesma consulta; `0` é `000`). Quatro dígitos (`0045`),
  letra, sinal ou espaço: 400. Código válido que não está no arquivo: 404.
- `?state=UF` (opcional): duas letras ASCII em qualquer caixa, passadas a
  maiúsculas (`rr` = `RR`); ausente ou vazio (`?state=`) = sem filtro; uma
  letra, três letras, dígito ou letra não ASCII: 400. Qualquer par de letras
  passa na validação: UF sem prestadora do serviço (ou que não é UF, `ZZ`)
  responde 200 com `count: 0` e `providers: []` (e entra no cache).
  Prestadora sem UF no cadastro só aparece sem o filtro.
- A validação do código vem antes da de `state`; com os dois válidos, o
  código é conferido antes (código fora do arquivo é 404 mesmo com UF).
- Chave: `service:<código com 3 dígitos>`, mais `:<UF>` com o filtro
  (`/service/45?state=rr` → `service:045:RR`).

| Campo | Tipo | Significado |
|---|---|---|
| `service_code` | texto, 3 dígitos | o código consultado |
| `service_name`, `service_group` | texto | como no catálogo |
| `state` | texto ou `null` | o filtro de UF, em maiúsculas; `null` sem filtro |
| `count` | inteiro (≥ 0) | prestadoras na lista |
| `providers` | lista de `ProviderBrief` | em ordem de CNPJ; `[]` quando a UF não tem nenhuma |
| `dataset` | objeto | versão dos dados |

`GET /anatel/pst/service/045` (3 das 20.510):

```json
{
  "service_code": "045",
  "service_name": "Serviço de Comunicação Multimídia",
  "service_group": "Banda Larga Fixa",
  "state": null,
  "count": 20510,
  "providers": [
    {"document": "00001180000126", "name": "CENTRAIS ELETRICAS BRASILEIRAS SA ELETROBRAS", "trade_name": "Eletrobras", "city": "Rio de Janeiro", "state": "RJ"},
    {"document": "00057274000117", "name": "MICKS TELECOM EIRELI", "trade_name": "MICKS TELECOM", "city": "Guanambi", "state": "BA"},
    {"document": "00068334000105", "name": "ITANEL PROVEDORES DE INFORMATICA LTDA - EPP", "trade_name": "ITA NET", "city": "Itabira", "state": "MG"},
    …
  ],
  "dataset": {"version": "01a0f518-b4d2-7810-80b5-b58a6c2e4614", "updated_at": "2026-10-01T01:33:48Z"}
}
```

`GET /anatel/pst/service/45?state=rr` → `{"service_code": "045", …, "state": "RR", "count": 88, "providers": [{"document": "04968416000159", "name": "EAGLE VISION COMERCIO E SERVICOS LTDA", "trade_name": "EAGLE VISION", "city": "Boa Vista", "state": "RR"}, …]}`.
`GET /anatel/pst/service/010` (SMP) → `count` 27, 3.530 bytes.

| HTTP | Mensagem |
|---|---|
| 400 | `código de serviço inválido: use de 1 a 3 dígitos (ex.: 045 ou 45 para o SCM)` |
| 400 | `UF inválida: use a sigla de duas letras (ex.: ?state=SP)` |
| 404 | `serviço <código> não consta no cadastro de prestadoras da Anatel`, com o código de 3 dígitos (`/service/999` → `serviço 999 não consta no cadastro de prestadoras da Anatel`) |

## `GET /anatel/pst/search?q={texto}[&limit=N]`

As prestadoras cuja razão social **ou** nome fantasia contém o trecho `q`,
sem diferenciar maiúsculas (`ILIKE`), em ordem de razão social e CNPJ (a
collation do banco — [api.md](api.md#dados-servidos)), com no máximo
`limit` itens, sem paginação. Não olha CNPJ, cidade nem serviço (para o
CNPJ, use `/provider`). As regras de `q` são as da `/ripe/asnames/search`
([ripe/asnames](../../ripe/asnames/api-rotas.md#get-asnamessearchqtexto)).

- `q`: vale o primeiro, se vier repetido. A normalização vem antes de tudo,
  nesta ordem:
  1. `strings.Fields` e junção com um espaço: tira os espaços das pontas e
     colapsa os internos — qualquer espaço em branco Unicode (espaço, tab,
     LF, CR, NBSP…) vira um espaço;
  2. UTF-8 válido e nenhum caractere de controle (`unicode.IsControl`): NUL
     e os controles C1 dão 400;
  3. minúsculas (`strings.ToLower`);
  4. de **3 a 100 caracteres** (`SearchMinLen`, `SearchMaxLen`), contados em
     caracteres, não bytes — menos de 3 não aproveita os índices trigram.

  Ausente, vazio, só espaços, curto, longo ou inválido: 400.
  `?q=%20Telefonica` e `?q=TELEFONICA` são a mesma consulta.
- `limit` (opcional): de **1 a 100** (`SearchLimit`), só dígitos ASCII
  (zeros à esquerda valem: `007` = 7); ausente ou vazio = 100. `0`, `101`,
  sinal, espaço, letra: 400. É validado depois de `q`.
- **Texto literal**: `store.LikePattern` monta `%<termo>%` escapando `\`,
  `%` e `_` — `?q=100%25` procura "100%", `?q=net_` procura "net_".
- O store recebe `limit + 1`: vindo mais que `limit`, a lista é cortada e
  `truncated` fica `true`. Sem resultado: 200 com `count: 0` e
  `providers: []` (não é 404, e entra no cache).
- Chave: `search:<limit>:<termo normalizado>` (`search:100:telefonica`); o
  limite vem antes porque o termo pode ter `:`.

| Campo | Tipo | Significado |
|---|---|---|
| `query` | texto | o termo normalizado |
| `limit` | inteiro (1 a 100) | o limite usado |
| `count` | inteiro (0 a `limit`) | itens devolvidos |
| `truncated` | booleano | `true` quando havia mais que `limit` resultados |
| `providers` | lista de `ProviderBrief` | em ordem de razão social e CNPJ; `[]` sem resultado |
| `dataset` | objeto | versão dos dados |

`GET /anatel/pst/search?q=telefonica` (5 dos 8 itens; a LVS casa pelo nome
fantasia, as outras pela razão social — `Telefonicas` também contém o
termo):

```json
{
  "query": "telefonica",
  "limit": 100,
  "count": 8,
  "truncated": false,
  "providers": [
    {"document": "82863291000106", "name": "Cunha Instalacoes Telefonicas Ltda", "trade_name": "Cunha Tecnologia em Telefonia", "city": "Rio do Sul", "state": "SC"},
    …
    {"document": "27480481000136", "name": "LVS TELECOMUNICACOES LTDA", "trade_name": "CIA TELEFONICA", "city": "São Paulo", "state": "SP"},
    {"document": "02558157000162", "name": "TELEFONICA BRASIL S.A.", "trade_name": null, "city": "São Paulo", "state": "SP"},
    {"document": "03441668000162", "name": "TELEFONICA ENGENHARIA DE SEGURANCA DO BRASIL LTDA.", "trade_name": null, "city": "São Paulo", "state": "SP"},
    {"document": "14314117000154", "name": "TELEFONICA GLOBAL SOLUTIONS BRASIL LTDA", "trade_name": null, "city": "São Paulo", "state": "SP"},
    …
  ],
  "dataset": {"version": "01a0f518-b4d2-7810-80b5-b58a6c2e4614", "updated_at": "2026-10-01T01:33:48Z"}
}
```

`GET /anatel/pst/search?q=telecom&limit=3` (resposta inteira):

```json
{
  "query": "telecom",
  "limit": 3,
  "count": 3,
  "truncated": true,
  "providers": [
    {"document": "21939944000107", "name": "0800 FLEX SERVICOS DE TELECOMUNICACOES LTDA. - ME", "trade_name": "0800 FLEX TELECOM", "city": "Vinhedo", "state": "SP"},
    {"document": "31063800000185", "name": "101telecom Servicos de Telecomunicacoes Ltda", "trade_name": "101telecom", "city": "São Paulo", "state": "SP"},
    {"document": "03988104000144", "name": "10x Telecom & Store Ltda", "trade_name": null, "city": "Catu", "state": "BA"}
  ],
  "dataset": {"version": "01a0f518-b4d2-7810-80b5-b58a6c2e4614", "updated_at": "2026-10-01T01:33:48Z"}
}
```

O `&` sai como `&` (o escape HTML do `encoding/json`, como diz o
[padrão](../../../padroes/api.md#formato-das-respostas)).

| HTTP | Mensagem |
|---|---|
| 400 | `use o parâmetro q com um trecho de 3 a 100 caracteres, sem caracteres de controle (ex.: ?q=telefonica)` |
| 400 | `limit inválido: use um número de 1 a 100 (padrão 100)` |

Não há 404: termo sem resultado é 200.

## `GET /anatel/pst/meta`

Estado dos dados, lido do banco a cada pedido — sem Valkey, sem ETag,
`Cache-Control: no-store`: a última execução aplicada de `anatel_pst_run` e
a linha `collector-anatel-pst` de `jobs`, as duas consultas dentro de um
prazo `DB_TIMEOUT`. Responde 200 também antes da primeira carga.

| Campo | Tipo | Significado |
|---|---|---|
| `app` | texto | `api-anatel-pst` |
| `version` | texto | versão do build (`buildinfo.Version`) |
| `dataset` | objeto ou `null` | última execução aplicada; `null` antes da primeira |
| `dataset.version` | texto (uuid) | `anatel_pst_run.uuid`: a versão usada no cache e no ETag |
| `dataset.updated_at` | timestamp | `anatel_pst_run.created_at` da execução |
| `dataset.source` | texto | `anatel_pst_run.url`: de onde o ZIP foi baixado |
| `dataset.sha256` | texto | `anatel_pst_run.sha256` do ZIP (hex); `""` se a linha não tiver |
| `dataset.csv_sha256` | texto ou `null` | `anatel_pst_run.csv_sha256` do CSV extraído |
| `dataset.csv_modified_at` | timestamp ou `null` | `anatel_pst_run.csv_modified_at`, em UTC (a fonte publica a hora de Brasília: `06:15:08` → `09:15:08Z`) |
| `dataset.providers`, `dataset.services` | inteiro ou `null` | `anatel_pst_run.providers` e `services` |
| `collector` | objeto ou `null` | a linha do coletor em `jobs`; `null` enquanto ela não existe |
| `collector.app` | texto | `collector-anatel-pst` |
| `collector.last_sync_at`, `collector.last_check_at` | timestamp ou `null` | colunas de `jobs` |
| `collector.consolidated` | booleano | `jobs.consolidated = 1` ([postgres.md](../../../plataforma/postgres.md#tabela-jobs)) |

```json
{
  "app": "api-anatel-pst",
  "version": "0.1.0",
  "dataset": {
    "version": "01a0f518-b4d2-7810-80b5-b58a6c2e4614",
    "updated_at": "2026-10-01T01:33:48Z",
    "source": "https://www.anatel.gov.br/dadosabertos/paineis_de_dados/outorga_e_licenciamento/prestadoras_servicos_telecomunicacoes.zip",
    "sha256": "ec1062af935a6b8fd1f9ef4688b2a9a567893ded581b07ec3fb877f091301df4",
    "csv_sha256": "4c6a726564c7db52d350e0c8ba8f0a3a8c10a3d48c3664cf957f35517acb1e5e",
    "csv_modified_at": "2026-09-30T09:15:08Z",
    "providers": 45074,
    "services": 54314
  },
  "collector": {"app": "collector-anatel-pst", "last_sync_at": "2026-10-01T01:33:48Z", "last_check_at": "2026-10-01T01:33:48Z", "consolidated": false}
}
```

Na medição, o mesmo ZIP foi servido por uma cópia local, e `source` trazia
a URL dela; em produção é a URL da Anatel, como acima.

Antes da primeira carga: `{"app": "api-anatel-pst", "version": "0.1.0", "dataset": null, "collector": null}`.

Erros: 503 `database_unavailable`, 504 `timeout`, 500 `internal_error`
(nunca 400, 404 nem `dataset_not_ready`).

## `GET /anatel/pst/` e `GET /anatel/pst/v1/`

Índice, igual nos dois caminhos; não consulta o banco nem o Valkey.
`Cache-Control: public, max-age=300`, sem ETag. `endpoints` é a lista fixa
abaixo, já com o caminho de base, sem `/health`, `/ping` nem as rotas com
`/v1`; `source` é a constante `SourceURL`.

```json
{
  "app": "api-anatel-pst",
  "version": "0.1.0",
  "base_path": "/anatel/pst",
  "versions": ["v1"],
  "endpoints": [
    "/anatel/pst/provider/{cnpj}", "/anatel/pst/services", "/anatel/pst/service/{code}",
    "/anatel/pst/search?q={texto}", "/anatel/pst/meta", "/anatel/pst/status", "/anatel/pst/openapi.yaml"
  ],
  "source": "https://www.anatel.gov.br/dadosabertos/paineis_de_dados/outorga_e_licenciamento/prestadoras_servicos_telecomunicacoes.zip"
}
```

## Saúde: `/anatel/pst/health`, `/anatel/pst/status` e `/anatel/pst/ping`

Como no [padrão](../../../padroes/api.md#saúde), com as mensagens
`api-anatel-pst operacional` e
`aguardando a primeira sincronização do collector-anatel-pst`:

```json
{"success": true, "status": "ok", "timestamp": "2026-10-01T01:35:50Z", "message": "api-anatel-pst operacional", "checks": {"dataset": "ok", "postgres": "ok", "valkey": "ok"}}
```

- `/health` e `/status` são o mesmo handler, em GET e POST (o corpo do POST
  é ignorado).
- Precedência `error` > `degraded` > `starting` > `ok`; `valkey` é
  `disabled` com o cache desligado.
- `/anatel/pst/ping`: `pong` em `text/plain; charset=utf-8`.

## `GET /anatel/pst/openapi.yaml`

O manifesto embutido, como em
[openapi.md](../../../padroes/openapi.md#resposta-de-get-fonteopenapiyaml);
`/anatel/pst/v1/openapi.yaml` é 404. Conteúdo: [api.md](api.md#manifesto-openapi).

## Erros

Formato, `Cache-Control: no-store` e códigos: [padrão](../../../padroes/api.md#erros).
Mensagens reais desta API:

| HTTP | `code` | `message` | Onde |
|---|---|---|---|
| 400 | `bad_request` | `CNPJ inválido: use os 14 dígitos, com ou sem pontuação (ex.: 02558157000162 ou 02.558.157%2F0001-62)` | `/provider` |
| 400 | `bad_request` | `código de serviço inválido: use de 1 a 3 dígitos (ex.: 045 ou 45 para o SCM)` | `/service` |
| 400 | `bad_request` | `UF inválida: use a sigla de duas letras (ex.: ?state=SP)` | `/service` |
| 400 | `bad_request` | `use o parâmetro q com um trecho de 3 a 100 caracteres, sem caracteres de controle (ex.: ?q=telefonica)` | `/search` |
| 400 | `bad_request` | `limit inválido: use um número de 1 a 100 (padrão 100)` | `/search` |
| 404 | `not_found` | `CNPJ <cnpj> não consta no cadastro de prestadoras da Anatel` | `/provider` |
| 404 | `not_found` | `serviço <código> não consta no cadastro de prestadoras da Anatel` | `/service` |
| 404 | `not_found` | `rota inexistente; veja /anatel/pst/` | qualquer outro caminho, ou método não registrado |
| 503 | `dataset_not_ready` | `a primeira sincronização do collector-anatel-pst ainda não terminou; tente em alguns minutos` | rotas de dados |
| 503 | `database_unavailable` | `banco de dados indisponível` | rotas de dados e `/meta` |
| 504 | `timeout` | `a consulta demorou demais` | rotas de dados e `/meta` |
| 500 | `internal_error` | `erro interno` | panic, em qualquer rota |

Um `store.ErrNotFound` que chegasse sem tradução viraria 404
`registro não encontrado`; nenhuma rota atual deixa isso acontecer.

## Roteamento

`ServeMux` do Go 1.27 com o catch-all `/` registrado:

| Pedido | Resposta |
|---|---|
| `/anatel/pst` (com ou sem query) | 301, `Location: /anatel/pst/` (a query se perde) |
| `/anatel/pst/v1` (sem barra) | 307, `Location: /anatel/pst/v1/`, corpo HTML do `ServeMux` (não é JSON) |
| caminho com `//`, `.` ou `..` | 307 para o caminho limpo, corpo HTML do `ServeMux` |
| barra no fim de uma rota (`/anatel/pst/services/`) | 404 JSON |
| `/anatel/pst/provider/02.558.157/0001-62` (barra sem `%2F`) | 404 JSON |
| método não registrado numa rota existente (`DELETE`, `POST` ou `PUT` em `/anatel/pst/provider/…`, `POST /anatel/pst/meta`) | 404 JSON `rota inexistente; veja /anatel/pst/` |
| `OPTIONS` em qualquer caminho | 204, preflight de CORS |
| `/anatel/pst/v2/...`, `/anatel/pst/v1/health`, `/anatel/pst/v1/status`, `/anatel/pst/v1/ping`, `/anatel/pst/v1/openapi.yaml` | 404 JSON |
| `/anatel/` e outros caminhos de `/anatel` fora de `/anatel/pst` | não chegam a esta API (o Traefik só encaminha `/anatel/pst` e `/anatel/pst/*`); direto na porta, 404 JSON |
