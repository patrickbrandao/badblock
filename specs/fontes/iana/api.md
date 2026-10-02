# api-iana

O que a `api-iana` faz **de diferente ou a mais** que o
[padrão das APIs](../../padroes/api.md); o resto (estrutura do código,
`main.go`, caminho de base e versões, formato, `serveCached`, cabeçalhos,
saúde, IP real, configuração comum, container) segue o padrão, e o manifesto
segue [../../padroes/openapi.md](../../padroes/openapi.md). Cada rota, com
parâmetros, campos e exemplo real, está em [api-rotas.md](api-rotas.md); as
tabelas e o SQL das consultas, em [dados.md](dados.md#consultas-da-api-iana).

Para um ASN, um IP ou um prefixo, a API diz o que os 10 registros da IANA
dizem ([fonte.md](fonte.md#arquivos)): a faixa ou o bloco (RIR, status,
WHOIS, RDAP), os registros de uso especial que o contêm, o servidor RDAP e,
para IP e prefixo, se é **bogon** ([regra](#regra-de-bogon)). Serve também as
listas inteiras (`/asns`, `/ipv4`, `/ipv6`, `/special`, `/rdap`), que são
pequenas ([medições](#medições)).

## Diferenças do padrão

| Assunto | Padrão | `api-iana` | Por quê |
|---|---|---|---|
| Registro inexistente | `404 not_found` | `/asn`, `/ip` e `/prefix` **nunca** respondem 404 para entrada válida: o que a IANA não diz sai `null`/`[]`. O 404 é só de rota | os registros cobrem o espaço inteiro (as faixas de ASN, 0–4294967295; os `/8`, todo o IPv4) e, no IPv6, estar fora de todo bloco é informação (espaço não entregue: bogon) |
| Consultas | — | `/asn`, `/ip`, `/prefix`, `/special` e `/rdap` leem várias tabelas numa transação só de leitura `REPEATABLE READ` ([abaixo](#dados-e-consistência)) | o coletor aplica os 10 arquivos numa transação: a resposta nunca mistura dois datasets |
| Regra de negócio | — | [regra de bogon](#regra-de-bogon) em `/ip` e `/prefix` | é o que a API acrescenta aos registros |
| Datas | `AAAA-MM-DD` | as datas da IANA saem como texto, como publicadas: `AAAA-MM` ou `AAAA-MM-DD` | inventar o dia 1 mudaria o dado ([dados.md](dados.md)) |
| `/meta` | `dataset` com os campos da fonte | `version`, `updated_at`, `sha256` (combinado) e `files` (os 10 arquivos); `files[].last_modified` é o cabeçalho HTTP como veio (`Sat, 19 Sep 2026 00:44:44 GMT`) | a versão cobre 10 arquivos; o `Last-Modified` é o validador que o coletor reenvia |
| Índice | campos da fonte | nenhum a mais; `source` = `https://www.iana.org/numbers` | os 10 arquivos e as URLs estão no `/meta` |
| `make test-int` do store | dados inseridos pelo teste | recorte real `testdata/seed.sql` ([testes](#testes)) | a regra de bogon depende do aninhamento real dos registros de uso especial |
| `make test-real` | `FILE=...`, em `httpapi`, mede as rotas | sem `FILE` (baixa os 10 arquivos ou usa `IANA_REAL_DIR`), em `internal/store`: as consultas do recorte e contagens mínimas | são 10 arquivos, carregados pelo próprio `collector-iana` |

## Código

Os pacotes do [padrão](../../padroes/api.md#estrutura-do-código), sem pacote
a mais. O que é da IANA:

| Arquivo | Conteúdo |
|---|---|
| `internal/store/store.go` | `Store` (pool pgx): `Open`, `Close`, `Ping`, `Dataset`, `Job`, `ASN`, `Prefix`, `ASNBlocks`, `PrefixBlocks`, `Special`, `RDAP`; `readTx` (transação `pgx.RepeatableRead` + `pgx.ReadOnly`); tipos `Dataset`, `File` (objeto de `iana_run.files`), `Job`, `ASNBlock`, `PrefixBlock`, `SpecialPrefix`, `SpecialASN`, `RDAPService`, `ASNLookup`, `PrefixLookup`, `SpecialLists`, `RDAPBootstrap`; `CollectorApp = "collector-iana"`; `FileRDAPASN`, `FileRDAPIPv4`, `FileRDAPIPv6` (`rdap-asn`, `rdap-ipv4`, `rdap-ipv6`) |
| `internal/store/bogon.go` | `PrefixLookup.Bogon()` ([regra](#regra-de-bogon)) |
| `internal/httpapi/server.go` | interface `Store` (abaixo), `Handler`, middleware, `serveCached`, `etagFor`, `match`; `CurrentAPIVersion = "v1"` |
| `internal/httpapi/handlers.go` | um handler por rota; `SourceURL = "https://www.iana.org/numbers"` |
| `internal/httpapi/types.go` | os tipos JSON ([api-rotas.md](api-rotas.md)), com os nomes dos `schemas` do manifesto (menos `DatasetInfo` → `Dataset` e `StatusResponse` → `Status`); lista `nil` do store vira `[]` |
| `testdata/seed.sql`, `testdata/gen-seed.sql` | recorte real para o teste do store ([testes](#testes)) |

```go
// internal/httpapi: o que as rotas usam (store.Store implementa; os testes usam um falso).
type Store interface {
	Ping(ctx context.Context) error
	Dataset(ctx context.Context) (*store.Dataset, error)
	Job(ctx context.Context) (*store.Job, error)
	ASN(ctx context.Context, asn int64) (*store.ASNLookup, error)
	Prefix(ctx context.Context, p netip.Prefix) (*store.PrefixLookup, error)
	ASNBlocks(ctx context.Context) ([]store.ASNBlock, error)
	PrefixBlocks(ctx context.Context, family int) ([]store.PrefixBlock, error)
	Special(ctx context.Context) (*store.SpecialLists, error)
	RDAP(ctx context.Context) (*store.RDAPBootstrap, error)
}
```

`ASNLookup` = `Block`, `Special`, `RDAP`. `PrefixLookup` = `Block` (bloco
mais específico que contém a consulta), `Special` (especiais que a contêm,
do mais específico ao menos), `RDAP` e `Unreserved` (algum bloco de
`iana_prefix_block` que **cruza** a consulta tem status diferente de
`RESERVED`). Um IP é consultado como o prefixo `/32` ou `/128`.

## Dados e consistência

A API só lê; o SQL e o índice de cada consulta estão em
[dados.md](dados.md#consultas-da-api-iana).

| Rota | Store | Tabelas | Leitura |
|---|---|---|---|
| `/asn/{asn}` | `ASN` | `iana_asn_block`, `iana_special_asn`, `iana_rdap_service` (`kind = 'asn'`) | transação `REPEATABLE READ` só de leitura |
| `/ip/{ip}`, `/prefix/{ip}/{len}` | `Prefix` | `iana_prefix_block` (bloco e `Unreserved`), `iana_special_prefix`, `iana_rdap_service` (`kind` = `ipv4` ou `ipv6`, pela família do endereço) | idem |
| `/special` | `Special` | `iana_special_prefix`, `iana_special_asn` | idem |
| `/rdap` | `RDAP` | `iana_run` (`publication` dos 3 JSONs), `iana_rdap_service` | idem |
| `/asns` | `ASNBlocks` | `iana_asn_block` | uma consulta |
| `/ipv4`, `/ipv6` | `PrefixBlocks(4)`, `PrefixBlocks(6)` | `iana_prefix_block` (`family`) | uma consulta |
| `/meta` | `Dataset`, `Job` | `iana_run` (última `status = 1`), `jobs` (`app = 'collector-iana'`) | duas consultas separadas: são estados independentes |
| versão do dataset | `Dataset` (o `dataset.Watcher`) | `iana_run` | uma consulta |

- As faixas de ASN não se sobrepõem: a de maior `asn_start <= X` que
  termina em `>= X` é a única candidata (`ORDER BY asn_start DESC LIMIT 1`).
- `iana_run.files` é lido inteiro; JSON ilegível é erro
  (`iana_run.files ilegível: ...`), que a rota responde como 503
  `database_unavailable`.
- Precisa de coluna ou índice novo? O pedido, com a migration proposta, vai
  para o `collector-iana` ([../README.md](../README.md#pasta-de-uma-fonte)).

## Regra de bogon

`bogon: true` = o endereço (ou o prefixo inteiro, em `/prefix`) não deve
aparecer como origem ou destino na Internet pública segundo os registros da
IANA. `store.PrefixLookup.Bogon` avalia duas regras, em ordem:

1. **Registros de uso especial** (`iana_special_prefix`). Entre as entradas
   **em vigor** (`termination_date` nulo) que contêm a consulta, a **mais
   específica** decide se tiver `globally_reachable` preenchido: `false` →
   bogon; `true` → não bogon. É o que as notas de rodapé da IANA mandam
   ("unless allowed by a more specific allocation"): `192.0.0.9/32`
   (`true`) vale dentro de `192.0.0.0/24` (`false`), e `2001:20::/28`
   (ORCHIDv2, `true`) dentro de `2001::/23` (`false`). Se a mais específica
   em vigor tem `globally_reachable` nulo (a IANA publica `N/A`: TEREDO
   `2001::/32`, 6to4 `2002::/16`), a IANA não decide ali e vale a regra 2 —
   as entradas menos específicas não são olhadas. Entradas encerradas não
   contam (`192.88.99.0/24`, `2001:10::/28`): o espaço volta à regra da
   entrada maior, se houver.
2. **Alocação da IANA** (`iana_prefix_block`). Bogon se **nenhum** bloco que
   cruza a consulta tem status diferente de `RESERVED` — isto é, a consulta
   está toda em blocos `RESERVED` (`0/8`, `10/8`, `127/8`, multicast
   224–239, 240–255, `3ffe::/16`, `3fff::/20`...) ou fora de qualquer bloco
   da IANA (IPv6 que a IANA não entregou a ninguém: multicast `ff00::/8`,
   `2000::/16`, `4000::/3`...). Com um bloco `ALLOCATED` ou `LEGACY` na
   consulta, não é bogon.

Em `/prefix`, a regra 1 só olha as entradas que **contêm** o prefixo
inteiro (as que estão dentro dele não entram) e a regra 2, todos os blocos
que o cruzam: `8.0.0.0/7` não é bogon (cruza `8.0.0.0/8` `LEGACY`),
`224.0.0.0/4` é (só cruza blocos `RESERVED`), `::/0` não é.

A regra marca o espaço **reservado** (bogons clássicos), não o "fullbogon"
dos RIRs: um IP de um bloco `ALLOCATED` a um RIR que o RIR ainda não delegou
a ninguém não é bogon aqui (os RIRs são outras fontes do BadBlock).

Resultados com o dataset real de 2026-09-28, testados em `internal/store`
(recorte e `make test-real`) e em `internal/httpapi`:

| Consulta | `bogon` | Por quê |
|---|---|---|
| `10.0.0.1` | true | special `10.0.0.0/8` Private-Use, `globally_reachable` false |
| `192.168.1.1` | true | special `192.168.0.0/16` Private-Use |
| `100.64.0.1` | true | special `100.64.0.0/10` Shared Address Space (o `/8` é ALLOCATED ARIN) |
| `192.0.2.1` | true | special `192.0.2.0/24` Documentation (TEST-NET-1) |
| `8.8.8.8` | false | nenhum special; `8.0.0.0/8` LEGACY |
| `187.87.29.10` | false | nenhum special; `187.0.0.0/8` ALLOCATED LACNIC |
| `2001:db8::1` | true | special `2001:db8::/32` Documentation (bloco `2001:c00::/23` APNIC) |
| `fe80::1` | true | special `fe80::/10` Link-Local Unicast |
| `2804:8ae0::1` | false | nenhum special; `2800::/12` ALLOCATED LACNIC |
| `::1` | true | special `::1/128` Loopback Address |
| `240.0.0.1` | true | special `240.0.0.0/4` Reserved (e `/8` RESERVED) |
| `0.0.0.1` | true | special `0.0.0.0/8` "This network" |
| `192.0.0.9` | false | `192.0.0.9/32` (true) é mais específico que `192.0.0.0/24` (false) |
| `2001::1` | false | TEREDO `2001::/32` é N/A; `2001::/23` do registro unicast é ALLOCATED |
| `2001:10::1` | true | ORCHID `2001:10::/28` encerrado; vale `2001::/23` (false) |
| `192.88.99.1` | false | `192.88.99.0/24` encerrado; `192.0.0.0/8` LEGACY |
| `192.88.99.2` | true | `192.88.99.2/32` 6a44-relay (false) |
| `64:ff9b::1` | false | special `64:ff9b::/96` (true), mesmo fora dos blocos unicast |
| `224.0.0.1` | true | nenhum special; `224.0.0.0/8` RESERVED (multicast) |
| `ff02::1` | true | nenhum special; fora de todo bloco da IANA |
| prefixo `224.0.0.0/4` | true | só cruza blocos RESERVED |
| prefixo `8.0.0.0/7` | false | cruza `8.0.0.0/8` LEGACY |

Mudar a regra começa aqui: a regra, esta tabela e os casos dos testes
(`store/bogon_test.go`, `ipCases` do teste de integração, `TestIPBogon` e
`TestPrefix` do `httpapi`) mudam juntos, e `make test-real` confere o
dataset do dia.

## Validações

| Parâmetro | Regra (código) | Normalização | Inválido → 400 `bad_request` |
|---|---|---|---|
| `{asn}` | prefixo `AS` opcional, em qualquer caixa (`strings.ToUpper` + `CutPrefix`); o resto em `strconv.ParseUint(…, 10, 32)`: só dígitos decimais, de 0 a 4294967295 | sem `AS` e sem zeros à esquerda (`as061610` → `61610`) | `ASN inválido: use um número de 0 a 4294967295, com ou sem o prefixo AS` (`abc`, `-1`, `+1`, `1.0`, `4294967296`, `AS`, `ASAS1`) |
| `{ip}` (`/ip` e `/prefix`) | `netip.ParseAddr`, sem zona | IPv4 mapeado em IPv6 vira IPv4 (`Unmap`); forma canônica (`2001:DB8::1` → `2001:db8::1`) | `endereço IP inválido` (`999.1.1.1`, `010.0.0.1`, `fe80::1%eth0`, `10.0.0.0%2F8`, `x`) |
| `{len}` (`/prefix`) | `strconv.Atoi` (aceita `08` e `+8`), de 0 ao tamanho do endereço **já convertido**: 32 ou 128 | bits de host zerados (`Masked`: `10.1.2.3/16` → `10.1.0.0/16`) | `tamanho de prefixo inválido` (`/33` no IPv4, `/129`, `/-1`, `::ffff:10.0.0.0/104`) |

- O `{ip}` é conferido antes do `{len}`; `::ffff:10.0.0.0/8` consulta
  `10.0.0.0/8`.
- A resposta ecoa a entrada normalizada (`asn`, `ip`, `query`), que é também
  a chave de cache.

Roteamento (conferido em 2026-09-29):

- Caminho exato: barra no fim (`/iana/ip/10.0.0.1/`, `/iana/special/`),
  parâmetro vazio (`/iana/asn/`) ou segmento a mais → 404.
- Método que a rota não aceita (`POST /iana/asn/61610`, `POST /iana/ping`,
  `POST /iana/meta`, `PUT`, `DELETE`) → 404 JSON, nunca 405: o catch-all
  `/` responde qualquer método.
- `/iana` → 301 para `/iana/` (rota própria). `/iana/v1` (sem a barra) →
  307 para `/iana/v1/`, e caminhos com `//`, `.` ou `..` → 307 para o
  caminho limpo: redirects automáticos do `http.ServeMux`, com corpo HTML.
- `/iana/v1/health`, `/iana/v1/status`, `/iana/v1/ping`,
  `/iana/v1/openapi.yaml`, `/iana/v2/...` e `/iana/V1/...` → 404.

## Erros

Formato e códigos no [padrão](../../padroes/api.md#erros). Mensagens reais:

| HTTP | `code` | `message` | Rotas |
|---|---|---|---|
| 400 | `bad_request` | as três de [validações](#validações) | `/asn`, `/ip`, `/prefix` |
| 404 | `not_found` | `rota inexistente; veja /iana/` | qualquer caminho ou método fora das rotas |
| 503 | `dataset_not_ready` | `a primeira sincronização do collector-iana ainda não terminou; tente em alguns minutos` | rotas de dados (não o `/meta`) |
| 503 | `database_unavailable` | `banco de dados indisponível` | rotas de dados e `/meta` |
| 504 | `timeout` | `a consulta demorou demais` | rotas de dados e `/meta` |
| 500 | `internal_error` | `erro interno` | qualquer rota (panic) |

## Cache e ETag

Chave `badblock:api-iana:<versão>:<consulta>` e ETag como no
[padrão](../../padroes/api.md#cache-valkey-cache-aside-e-servecached). As
consultas normalizadas e os ETags reais da versão
`01a0eaa2-fd5c-7c24-bc6c-4e3c35d61c53` (`etagFor`):

| Rota | `<consulta>` | ETag |
|---|---|---|
| `/asn/61610`, `/asn/AS61610`, `/asn/as061610` | `asn:61610` | `W/"3db9c89d25843ce7"` (o exemplo do manifesto) |
| `/asn/AS23456` | `asn:23456` | `W/"db00d0d5ed53d15"` |
| `/ip/192.0.0.9` | `ip:192.0.0.9` | `W/"5ab563c8b8887ceb"` |
| `/ip/10.0.0.1`, `/ip/::ffff:10.0.0.1` | `ip:10.0.0.1` | `W/"3f7421dde56f0402"` |
| `/prefix/2001:db8::/48`, `/prefix/2001:db8::1/48` | `prefix:2001:db8::/48` | `W/"b32ac55320e001d9"` |
| `/prefix/10.1.2.3/16` | `prefix:10.1.0.0/16` | |
| `/asns`, `/ipv4`, `/ipv6` | `asns`, `ipv4`, `ipv6` | `W/"3904847c4c39be36"`, `W/"46b2de3ac362fbd6"`, `W/"46b2dc3ac362f870"` |
| `/special`, `/rdap` | `special`, `rdap` | `W/"e23a3e5acde9a968"`, `W/"a60ad11cb549c2ea"` |

- Com `/v1`, a mesma chave e o mesmo ETag; a query string não entra.
- O `304` sai com `X-Cache: HIT`, sem corpo e sem `Content-Type`.
- Todos os corpos ficam muito abaixo do limite de 8 MB do Valkey (o maior,
  `/ipv4`, tem ~54 KB: [medições](#medições)).

## Manifesto

`apps/iana/api/openapi/openapi.yaml` segue o
[padrão](../../padroes/openapi.md) e é derivado de
[api-rotas.md](api-rotas.md) (rota, parâmetro, campo ou resposta nova muda os
dois no mesmo trabalho); os exemplos são os de lá. O que é da IANA:

| Parte | Conteúdo |
|---|---|
| `info` | `title: api-iana`, `version: 0.1.0`, licença MIT; `description` com o que a API serve (tabelas `iana_*`, bogon, RDAP, fonte `https://www.iana.org/numbers`), as versões (servidores com e sem `/v1`; saúde e manifesto fora do versionamento), `HEAD`, `OPTIONS` (204), o 301 de `/iana`, o 404 `not_found` para outro caminho ou método, os cabeçalhos de toda resposta, o formato dos erros e o rate limit do Traefik |
| `servers` | porta 8107, com as descrições `Produção, versão atual (hoje v1)`, `Produção, v1 fixa (só o índice, as rotas de dados e o /meta)`, `Desenvolvimento local, versão atual (porta API_IANA_HOST_PORT no loopback)` e `Desenvolvimento local, v1 fixa`; nos paths sem `/v1`, `Produção` e `Desenvolvimento local` |
| `tags` | `dados` (consultas e listas, com ETag, `X-Cache` e cache no Valkey), `meta` (índice, `/meta` e o manifesto), `saúde` (healthchecks, fora do versionamento e sem cache) |
| `parameters` | `ASN` (texto, `pattern: '^([Aa][Ss])?[0-9]+$'`, exemplo `AS61610`), `IP` (`anyOf` `ipv4`/`ipv6`, exemplo `192.0.0.9`), `PrefixAddress` (idem, exemplo `2001:db8::`), `PrefixLength` (inteiro 0–128, exemplo 48), `IfNoneMatch` (exemplo `W/"3db9c89d25843ce7"`) |
| `headers` | `ETag`, `CacheControlPublic`, `CacheControlNoStore`, `XCache` (`HIT` também no 304), `XDatasetVersion` |
| `responses` | `NotModified`; `BadRequest` (exemplos `asn`, `ip` e `len`, com as três mensagens); `NotFound` (definido, sem uso nas operações); `DatasetUnavailable` (o 503 das rotas de dados: `dataset_not_ready` e `database_unavailable`); `DatabaseUnavailable` (o 503 do `/meta`); `Timeout`; `InternalError`; `StatusOK`; `StatusUnavailable` (`status: error`, `PostgreSQL indisponível`) |
| `schemas` | `Registry` (os 5 RIRs ou `null`), `IANADate` (`^[0-9]{4}-[0-9]{2}(-[0-9]{2})?$` ou `null`), `Dataset`, os objetos e as respostas de [api-rotas.md](api-rotas.md) (com os nomes dos tipos de `types.go`), `Status`, `StatusChecks` e `Error` (`code` com os 6 códigos) |

| Path | `operationId` | Tag | Respostas |
|---|---|---|---|
| `/` | `getIndex` | `meta` | 200 (`Cache-Control` público) |
| `/asn/{asn}`, `/ip/{ip}`, `/prefix/{ip}/{len}` | `getASN`, `getIP`, `getPrefix` | `dados` | 200, 304, 400, 500, 503, 504 |
| `/asns`, `/ipv4`, `/ipv6`, `/special`, `/rdap` | `getASNs`, `getIPv4`, `getIPv6`, `getSpecial`, `getRDAP` | `dados` | 200, 304, 500, 503, 504 |
| `/meta` | `getMeta` | `meta` | 200 (exemplos `loaded` e `empty`), 500, 503, 504 |
| `/health`, `/status` | `getHealth`, `postHealth`, `getStatus`, `postStatus` | `saúde` | 200 (`StatusOK`), 500, 503 (`StatusUnavailable`) |
| `/ping` | `ping` | `saúde` | 200 `text/plain` (`const: pong`, `no-store`) |
| `/openapi.yaml` | `getOpenAPI` | `meta` | 200 `application/yaml` |

## Configuração

As opções são as do [padrão](../../padroes/api.md#configuração-comum), com
`BASE_PATH` = `/iana`. Opção nova segue
[../../projeto/convencoes.md](../../projeto/convencoes.md#configuração) e
entra nesta seção. O `--help` abre com:

```
api-iana — API HTTP dos registros de numeração da IANA: a quem cada faixa de
ASN e bloco IP foi entregue, blocos e ASNs de uso especial (bogons) e RDAP.

Lê as tabelas iana_* mantidas pelo collector-iana, com cache opcional no
Valkey. Responde tudo abaixo de BASE_PATH (/iana): /iana/ip/10.0.0.1 é a
versão atual e /iana/v1/ip/10.0.0.1 fixa a v1.
```

| Item | Valor |
|---|---|
| Porta no loopback | 8107 (`API_IANA_HOST_PORT`) |
| Nomes ([regras](../../projeto/estrutura.md#nomes)) | imagem `tmsoftbrasil/badblock-api-iana`, container `badblock-api-iana`, router e serviço do Traefik `badblock-api-iana`, middleware `badblock-api-iana-ratelimit`, `Server: badblock-api-iana/<versão>`, `application_name` `api-iana`, cliente Valkey `badblock-api-iana` |
| `LABEL` `description` da imagem | `API HTTP dos registros de numeração da IANA (blocos de ASN e IP, special-purpose, RDAP), do BadBlock` |

No compose ([docker.md](../../plataforma/docker.md#api) e
[publicacao.md](../../plataforma/publicacao.md#traefik)):

| `.env` | Variável do app | Padrão |
|---|---|---|
| `API_IANA_TAG` | tag da imagem (e `VERSION` do build) | `latest` (`dev` no build) |
| `API_IANA_HOST_PORT` | porta no loopback | `8107` |
| `API_IANA_REDIS_URL` | `REDIS_URL` | `redis://:${VALKEY_PASSWORD:-}@badblock-valkey:6379/0` |
| `API_IANA_CACHE_ENABLED` | `REDIS_CACHE_ENABLED` | `true` |
| `API_IANA_CACHE_TTL` | `REDIS_KEY_TTL` | `3600` |
| `API_IANA_DB_POOL_MAX` | `DB_POOL_MAX` | `10` |

O compose fixa `HTTP_PORT: "8080"`, `BASE_PATH: /iana` e `LOG_FORMAT: json`.
O `.env.example` da raiz traz `API_IANA_TAG=latest`,
`API_IANA_HOST_PORT=8107` e `API_IANA_CACHE_TTL=3600`; o do app,
`POSTGRES_PASSWORD=`, `VALKEY_PASSWORD=`, `POSTGRES_URL` e
`API_IANA_REDIS_URL` comentadas, essas três `API_IANA_*`, as variáveis do
Traefik e `LOG_LEVEL=info`.

## Operação

```bash
make up                        # na raiz: stack completo; a API fica em 127.0.0.1:8107
make -C apps/iana/api smoke    # /status, /asn/61610, /ip/10.0.0.1 e /openapi.yaml
make -C apps/iana/api logs
curl http://127.0.0.1:8107/iana/ip/10.0.0.1
```

O `smoke` faz `curl -fsS` na porta `API_IANA_HOST_PORT` do ambiente do shell
(8107 sem ela; o `Makefile` não lê o `.env`) e para no primeiro erro. O
estado dos dados está em `/iana/meta`; o do coletor, em
[collector.md](collector.md#operação).

## Medições

Dataset de 2026-09-28 (versão `01a0eaa2-fd5c-7c24-bc6c-4e3c35d61c53`,
aplicada em `2026-09-29T00:49:05Z`):

| Rota | Itens | Corpo |
|---|---|---|
| `/asns` | 173 faixas | ~35 KB |
| `/ipv4` | 256 blocos `/8` | ~54 KB (o maior) |
| `/ipv6` | 51 blocos | ~12 KB |
| `/special` | 26 + 25 blocos, 9 faixas de ASN | 13.212 bytes |
| `/rdap` | 159 + 221 + 34 entradas | ~41 KB |

Medido em 2026-09-29 com a API sobre o `testdata/seed.sql` (que tem as
tabelas de uso especial inteiras, daí o `/special` exato): consultas de 384
bytes (`/asn/AS23456`) a 1.023 bytes (`/ip/192.0.0.9`), `/meta` com 3.144
bytes, 2 a 9 ms por consulta sem cache numa máquina de desenvolvimento.
Tempos com o dataset inteiro ainda não foram medidos.

## Testes

Camadas e comandos: [padrão](../../padroes/api.md#testes) e
[../../processos/testes.md](../../processos/testes.md). A cada mudança,
`make test`, `make test-int`, `make lint` e `make vet`; `make test-real`
quando mexer numa consulta ou na regra de bogon; `make smoke` com o stack no
ar. O que é da IANA:

| Onde | O quê |
|---|---|
| `internal/httpapi` (`httpapi_test.go`) | store falso que aplica em Go as regras das consultas SQL sobre linhas reais de 2026-09-28 (22 especiais, 39 `/8` — com 224–255 inteiros — e 6 blocos IPv6, 6 faixas de ASN, 4 ASNs especiais, 4 entradas RDAP): `TestASN` (as 4 formas, `special: []`, AS23456, AS4294967295, ASN sem faixa → `block: null`), `TestIPBogon` (os 20 IPs da tabela de bogon, `0.0.0.0` e `3fff::1`), `TestIPDetails` (ordem dos aninhados, IPv4 mapeado, `8.8.8.8` LEGACY ARIN e `187.87.29.10` ALLOCATED LACNIC, `[]`, `globally_reachable: null`, `fe80::1` sem bloco nem RDAP), `TestPrefix` (10 prefixos), `TestLists`, `TestErrors` (17 caminhos), `TestHead`, `TestCacheAndETag` (MISS → HIT com `/v1` e IPv4 mapeado, uma consulta só, chave, `If-None-Match` nas 4 formas), `TestNotReady`, `TestDatabaseErrors`, `TestStatus`, `TestCORSAndHeaders`, `TestIndexMetaAndRedirect` |
| `internal/httpapi` (`openapi_test.go`) | o do [padrão](../../padroes/openapi.md#teste), com as URLs dos `servers` (porta 8107) fixas no teste |
| `internal/store` (`bogon_test.go`) | `TestBogonRule`: a regra isolada, 11 casos, sem banco |
| `internal/store` (`store_integration_test.go`, tag `integration`) | `postgres:18-trixie` (banco `badblock`, usuário `postgres`, senha `pg`) com o `migrate:up` de `central/` e `iana/`, `Store` com 4 conexões. `TestQueries`, sobre o `seed.sql`: a versão é a última aplicada (nem a recusada mais nova nem a aplicada mais antiga), `files` (10, `rows`, `etag` ausente nos CSVs, `W/"1138-65336a3cb9688-br"` no `rdap-asn`, `publication`), `Job`, os 34 `ipCases` (a tabela de bogon, aninhamentos, N/A, encerrados, 7 prefixos) e os 13 `asnCases` (bloco, especiais e RDAP), colunas de `187.0.0.0/8` (ALLOCATED LACNIC) e de `8.0.0.0/8` (LEGACY ARIN), AS61610, TEREDO e 6to4 relay, e as listas do recorte com a ordem (13 faixas, 9 + 6 blocos, 51 + 9 especiais, 9 RDAP). `TestEmptyDatabase`: banco sem carga, `Dataset`/`Job` `nil` e consultas vazias sem erro |

### `testdata/seed.sql`

Recorte **real** das tabelas `iana_*`: cópias exatas das linhas que o
`collector-iana --once` gravou num PostgreSQL 18 com os 10 arquivos de
2026-09-28, escolhidas para cobrir os casos do teste:

- `iana_asn_block`: 13 faixas (as de início 0, 1, 23456, 61440, 64496,
  64512, 65535, 65536, 65552, 262144, 275869, 4200000000 e 4294967295);
- `iana_prefix_block`: 9 `/8` (`0`, `8`, `10`, `100`, `127`, `187`, `192`,
  `224`, `240`) e 6 blocos IPv6 (`2001::/23`, `2001:c00::/23`, `2002::/16`,
  `2800::/12`, `3ffe::/16`, `3fff::/20`);
- `iana_special_prefix` (51) e `iana_special_asn` (9) **inteiras**: a regra
  de bogon depende do aninhamento entre elas;
- `iana_rdap_service`: 9 entradas (`1-1876`, `61440-61951`,
  `262144-263167`, `8/8`, `100/8`, `187/8`, `192/8`, `2001:c00::/23`,
  `2800::/12`);
- `iana_run`: a execução real aplicada (`01a0eaa2-…`, com os 10 `files`) e
  duas sintéticas, escritas à mão, que testam a escolha da versão — uma
  aplicada mais antiga (`01a0e000-0000-7000-8000-000000000001`) e uma
  recusada mais nova (`01a0f000-0000-7000-8000-000000000002`); e a linha de
  `jobs` do `collector-iana`, também sintética.

Para regerar (schema mudou numa coluna que a API lê, ou um caso novo pede
outra linha — acrescente-a ao filtro): carregue um banco com o
`collector-iana`, rode `psql -q -f testdata/gen-seed.sql "$POSTGRES_URL"` e
mantenha à mão o fim sintético (`iana_run` extras e `jobs`).

### `make test-real`

Sem `IANA_REAL_DIR`, baixa os 10 arquivos com `curl -fsS` para uma pasta
temporária (bases `IANA_BASE` e `RDAP_BASE` do `Makefile`, as padrão da
IANA); com `IANA_REAL_DIR=<pasta>`, usa os arquivos dela, com os nomes do
servidor ([fonte.md](fonte.md#arquivos)). Roda
`go test -count=1 -tags integration -run 'Real' -v ./internal/store`
(`TestRealDataset`): compila `../collector` (`go build ./cmd/collector-iana`),
serve a pasta num `httptest.Server`, roda
`collector-iana --once --iana-base-url <servidor>/assignments --rdap-base-url <servidor>/rdap`
num PG18 vazio e confere: 10 `files` com `publication` nos 3 `rdap-*`, os
mesmos `ipCases` e `asnCases` do recorte e as contagens mínimas (faixas de
ASN ≥ 150, `/8` = 256, blocos IPv6 ≥ 30, especiais ≥ 30 e ≥ 6, RDAP ≥ 270),
que o teste imprime. Precisa de rede (só no `curl`) e de Docker.

## Pendências

Divergências do código com o padrão, a alinhar (2026-09-29):

- `make smoke` não consulta `/meta`, e o `Makefile` não tem `SOURCE`, `PORT`
  nem `SMOKE_ASN` ([docker.md](../../plataforma/docker.md#makefile-dos-apps));
  o `openapi_test.go` fixa a porta 8107 em vez de ler o `PORT` do `Makefile`.
- Sem teste unitário de `internal/dataset`; o teste de integração do store
  não confere o uso de índice.
- Manifesto sem `info.summary`, com descrições de `servers` próprias e com
  `DatasetUnavailable`/`StatusUnavailable` no lugar de
  `ServiceUnavailable`/`StatusError`.
- `.env.example` do app sem os opcionais comentados
  (`API_IANA_CACHE_ENABLED`, `API_IANA_DB_POOL_MAX`, `API_TRUSTED_PROXIES`).
