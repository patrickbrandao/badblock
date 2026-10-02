# API da LACNIC (`api-lacnic`)

A `api-lacnic` é o **modelo** das APIs de RIR: o comportamento está em
[../../padroes/api.md](../../padroes/api.md),
[../../padroes/openapi.md](../../padroes/openapi.md) e
[../rir/api.md](../rir/api.md), sem nada de diferente. Aqui, os valores, as
respostas reais e as medições da LACNIC. Dono: sub-agente `api-lacnic`.

## Valores

| Item | Valor |
|---|---|
| `internal/rir/rir.go` | `Source` `lacnic`, `Title` `LACNIC`, `SourceURL` `https://ftp.lacnic.net/pub/stats/lacnic/delegated-lacnic-extended-latest`, `OpaqueIDChangesDaily` `false` |
| Caminho | `BASE_PATH` `/lacnic`; em produção, `https://api.badblock.net.br/lacnic/` |
| Porta no loopback | 8105 (`API_LACNIC_HOST_PORT`, `PORT` do `Makefile`): `http://127.0.0.1:8105/lacnic/` |
| `SMOKE_ASN` | 61613 (BR, allocated, titular `258500`) |
| Tabelas lidas | `lacnic_run`, `lacnic_asn` e `lacnic_prefix` ([dados.md](dados.md)) e a linha `collector-lacnic` de `jobs` |
| `.env` | `API_LACNIC_TAG=latest`, `API_LACNIC_HOST_PORT=8105`, `API_LACNIC_CACHE_TTL=3600`; opcionais `API_LACNIC_CACHE_ENABLED` (`true`), `API_LACNIC_DB_POOL_MAX` (`10`) e `API_LACNIC_REDIS_URL` |
| Imagem e container | `tmsoftbrasil/badblock-api-lacnic`, label `description` `API HTTP das delegações de ASNs e blocos IP do RIR LACNIC (delegated-extended), do BadBlock`; `badblock-api-lacnic` |
| Traefik | router e serviço `badblock-api-lacnic`, middleware `badblock-api-lacnic-ratelimit`, regra `Host(api.badblock.net.br) && (Path(/lacnic) \|\| PathPrefix(/lacnic/))` ([molde](../../plataforma/publicacao.md#traefik)) |

## Exemplos

Respostas reais com o arquivo de 2026-09-28 (serial 20260927,
[fonte.md](fonte.md#fatos-medidos-arquivo-de-2026-09-28-serial-20260927))
carregado pelo `collector-lacnic`: dataset
`01a0eaa7-5807-78bb-993d-2ccb16b3c106`, aplicado em 2026-09-29T00:53:49Z
(`01a0eaa7-…` nos exemplos curtos). São também os exemplos do manifesto. O
que cada campo quer dizer está em [../rir/api.md](../rir/api.md).

### ASN

`/lacnic/asn/61613` (o mesmo em `/lacnic/v1/asn/AS61613`):

```json
{
  "asn": 61613,
  "range": {"start": 61613, "end": 61613, "count": 1},
  "cc": "BR",
  "reg_date": "2023-05-05",
  "status": "allocated",
  "opaque_id": "258500",
  "first_seen": "2026-09-29T00:53:49Z",
  "updated_at": "2026-09-29T00:53:49Z",
  "dataset": {"version": "01a0eaa7-5807-78bb-993d-2ccb16b3c106", "updated_at": "2026-09-29T00:53:49Z"}
}
```

ASN no meio de uma faixa available (`/lacnic/asn/28004`; na LACNIC, faixas de
mais de um ASN só existem em available, [fonte.md](fonte.md#fatos-medidos-arquivo-de-2026-09-28-serial-20260927)):

```json
{
  "asn": 28004,
  "range": {"start": 28003, "end": 28005, "count": 3},
  "cc": null, "reg_date": null, "status": "available", "opaque_id": null,
  "first_seen": "2026-09-29T00:53:49Z", "updated_at": "2026-09-29T00:53:49Z",
  "dataset": {"version": "01a0eaa7-…", "updated_at": "2026-09-29T00:53:49Z"}
}
```

### IP

`/lacnic/ip/45.171.61.10`:

```json
{
  "ip": "45.171.61.10",
  "prefix": "45.171.60.0/22",
  "cc": "BR",
  "reg_date": "2019-02-11",
  "status": "allocated",
  "opaque_id": "258500",
  "record": {"start": "45.171.60.0", "value": 1024},
  "dataset": {"version": "01a0eaa7-…", "updated_at": "2026-09-29T00:53:49Z"}
}
```

`/lacnic/ip/2804:5964::1`:

```json
{
  "ip": "2804:5964::1",
  "prefix": "2804:5964::/32",
  "cc": "BR", "reg_date": "2019-02-11", "status": "allocated", "opaque_id": "258500",
  "record": {"start": "2804:5964::", "value": 32},
  "dataset": {"version": "01a0eaa7-…", "updated_at": "2026-09-29T00:53:49Z"}
}
```

Na LACNIC todo registro IPv4 forma um CIDR (de `/11` a `/24`), então
`record.start` é sempre o endereço do próprio `prefix` e, no IPv4,
`record.value` é o tamanho do bloco ([dados.md](dados.md#o-que-as-tabelas-guardam)).

### Prefixo

`/lacnic/prefix/45.171.61.0/24` (e `/lacnic/prefix/45.171.61.9/24`, que
consulta o mesmo `45.171.61.0/24`):

```json
{
  "query": "45.171.61.0/24",
  "prefix": "45.171.60.0/22",
  "exact": false,
  "cc": "BR",
  "reg_date": "2019-02-11",
  "status": "allocated",
  "opaque_id": "258500",
  "record": {"start": "45.171.60.0", "value": 1024},
  "dataset": {"version": "01a0eaa7-…", "updated_at": "2026-09-29T00:53:49Z"}
}
```

`/lacnic/prefix/45.171.60.0/22` responde o mesmo bloco com
`"query": "45.171.60.0/22"` e `"exact": true`; em IPv6,
`/lacnic/prefix/2804:5964::/32` também funciona.

### Titular

`/lacnic/holder/258500`:

```json
{
  "opaque_id": "258500",
  "cc": "BR",
  "ccs": ["BR"],
  "counts": {"asns": 1, "ipv4": 2, "ipv6": 1},
  "asns": [
    {"start": 61613, "end": 61613, "count": 1, "cc": "BR", "status": "allocated", "reg_date": "2023-05-05"}
  ],
  "prefixes": {
    "ipv4": [
      {"prefix": "45.171.60.0/22", "cc": "BR", "status": "allocated", "reg_date": "2019-02-11"},
      {"prefix": "200.192.152.0/22", "cc": "BR", "status": "allocated", "reg_date": "2003-11-25"}
    ],
    "ipv6": [
      {"prefix": "2804:5964::/32", "cc": "BR", "status": "allocated", "reg_date": "2019-02-11"}
    ]
  },
  "dataset": {"version": "01a0eaa7-…", "updated_at": "2026-09-29T00:53:49Z"}
}
```

O opaque-id da LACNIC é numérico (2 a 6 dígitos, 14.811 titulares), então a
caixa não importa. O maior titular do arquivo é `124194` (3 registros de ASN,
421 blocos IPv4 e 4 IPv6): 36 KB.

### Meta

`/lacnic/meta`:

```json
{
  "app": "api-lacnic",
  "version": "0.1.0",
  "dataset": {
    "version": "01a0eaa7-5807-78bb-993d-2ccb16b3c106",
    "updated_at": "2026-09-29T00:53:49Z",
    "source": "https://ftp.lacnic.net/pub/stats/lacnic/delegated-lacnic-extended-latest",
    "sha256": "d45c47ffd9899906ed52995f2f116e7deb3496085fc2829442ed24a996fd43c5",
    "md5": "1d11010d5e9cc31ce507817ac4fae1bf",
    "serial": "20260927",
    "start_date": "1987-01-01",
    "end_date": "2026-09-25",
    "asn_records": 16515,
    "ipv4_records": 20804,
    "ipv6_records": 59982,
    "prefixes_v4": 20804,
    "prefixes_v6": 59982
  },
  "collector": {
    "app": "collector-lacnic",
    "last_sync_at": "2026-09-29T00:53:49Z",
    "last_check_at": "2026-09-29T00:53:49Z",
    "consolidated": false
  }
}
```

`prefixes_v4` = `ipv4_records` porque na LACNIC nenhum registro IPv4 é
dividido. Antes da primeira carga:
`{"app": "api-lacnic", "version": "0.1.0", "dataset": null, "collector": null}`.

### Índice

`/lacnic/` e `/lacnic/v1/`:

```json
{
  "app": "api-lacnic",
  "version": "0.1.0",
  "registry": "LACNIC",
  "base_path": "/lacnic",
  "versions": ["v1"],
  "endpoints": [
    "/lacnic/asn/{asn}", "/lacnic/ip/{ip}", "/lacnic/prefix/{ip}/{len}",
    "/lacnic/holder/{opaque_id}", "/lacnic/meta", "/lacnic/status",
    "/lacnic/openapi.yaml"
  ],
  "source": "https://ftp.lacnic.net/pub/stats/lacnic/delegated-lacnic-extended-latest"
}
```

### Saúde

`/lacnic/status` (e `/lacnic/health`, GET ou POST):

```json
{"success": true, "status": "ok", "timestamp": "2026-09-29T00:54:27Z", "message": "api-lacnic operacional",
 "checks": {"dataset": "ok", "postgres": "ok", "valkey": "ok"}}
```

Os outros estados, como no manifesto (mesmo `timestamp`):

| `status` | HTTP | `message` | `checks` |
|---|---|---|---|
| `starting` | 200 | `aguardando a primeira sincronização do collector-lacnic` | `dataset` `empty`, `postgres` `ok`, `valkey` `ok` |
| `degraded` | 200 | `Valkey indisponível; respondendo sem cache` | `dataset` `ok`, `postgres` `ok`, `valkey` `error` |
| `error` | 503 (`success: false`) | `PostgreSQL indisponível` | `dataset` `ok`, `postgres` `error`, `valkey` `ok` |

### Erros

Os do manifesto, com as mensagens reais:

| Pedido | HTTP | `code` | `message` |
|---|---|---|---|
| `/lacnic/asn/abc` | 400 | `bad_request` | `ASN inválido: use um número de 0 a 4294967295, com ou sem o prefixo AS` |
| `/lacnic/ip/x` | 400 | `bad_request` | `endereço IP inválido` |
| `/lacnic/prefix/45.171.60.0/33` | 400 | `bad_request` | `tamanho de prefixo inválido` |
| `/lacnic/holder/nao%20existe` | 400 | `bad_request` | `opaque_id inválido: use de 1 a 128 letras, dígitos, ponto, hífen ou sublinhado` |
| `/lacnic/asn/1` (ASN da ARIN) | 404 | `not_found` | `AS1 não consta no arquivo do RIR LACNIC` |
| `/lacnic/ip/192.0.2.1` | 404 | `not_found` | `192.0.2.1 não pertence a nenhum bloco no arquivo do RIR LACNIC` |
| `/lacnic/prefix/192.0.2.0/24` | 404 | `not_found` | `192.0.2.0/24 não está contido em nenhum bloco no arquivo do RIR LACNIC` |
| `/lacnic/holder/999999` | 404 | `not_found` | `o titular 999999 não consta no arquivo do RIR LACNIC` |
| `/lacnic/nada` | 404 | `not_found` | `rota inexistente; veja /lacnic/` |
| uma consulta antes da primeira carga | 503 | `dataset_not_ready` | `a primeira sincronização do collector-lacnic ainda não terminou; tente em alguns minutos` |

### Chaves de cache e ETags

Com o dataset dos exemplos (chave completa:
`badblock:api-lacnic:01a0eaa7-5807-78bb-993d-2ccb16b3c106:<consulta>`); ETags
calculados por `etagFor` em 2026-09-29:

| Pedido | Consulta | ETag |
|---|---|---|
| `/lacnic/asn/61613`, `/lacnic/v1/asn/AS61613` | `asn:61613` | `W/"4492aae57a0d5aa4"` (o exemplo do manifesto) |
| `/lacnic/asn/28004` | `asn:28004` | `W/"60c2f584abe16a37"` |
| `/lacnic/ip/45.171.61.10`, `/lacnic/ip/::ffff:45.171.61.10` | `ip:45.171.61.10` | `W/"4544c907eb832188"` |
| `/lacnic/ip/2804:5964::1` | `ip:2804:5964::1` | `W/"cc077ca539d61e63"` |
| `/lacnic/prefix/45.171.61.9/24`, `/lacnic/prefix/45.171.61.0/24` | `prefix:45.171.61.0/24` | `W/"a718a3b530004b1"` (15 dígitos: sem zeros à esquerda) |
| `/lacnic/prefix/45.171.60.0/22` | `prefix:45.171.60.0/22` | `W/"117d810f267381d6"` |
| `/lacnic/holder/258500` | `holder:258500` | `W/"2cbfe580cc52909f"` |

## Manifesto

O do [modelo](../rir/api.md#manifesto), sem particularidades
(`OpaqueIDChangesDaily` é `false`): `info.version` `'0.1.0'`,
`externalDocs.url`
`https://github.com/patrickbrandao/badblock/tree/main/specs/fontes/lacnic`,
servidores locais na porta 8105 e os exemplos desta página. Nos parâmetros:
`asn` `'61613'`, `ip` `45.171.61.10`, `ip` e `len` do prefixo `45.171.61.0` e
`24`, `opaque_id` `'258500'`; `IfNoneMatch` e `ETag` com `W/"4492aae57a0d5aa4"`
e `X-Dataset-Version` com a versão dos exemplos. `/asn` tem os exemplos `asn`
(61613) e `faixa` (28004); `/ip`, `ipv4` e `ipv6`; `/prefix`, `contido` e
`exato`; `/meta`, `carregado` e `vazio`.

## `--help`

Saída real (versão `dev`, 2026-09-29), no stderr:

```
api-lacnic — API HTTP das delegações de ASNs e blocos IP do RIR LACNIC.

Lê as tabelas lacnic_* mantidas pelo collector-lacnic, com cache opcional no Valkey.
Responde tudo abaixo de BASE_PATH (/lacnic): /lacnic/asn/{asn} é a versão atual
e /lacnic/v1/asn/{asn} fixa a v1.

Uso:
  api-lacnic [opções]
  api-lacnic healthcheck    (usa HTTP_PORT e BASE_PATH; para o HEALTHCHECK do Docker)

Opções (padrão → variável de ambiente → argumento):
  --access-log           registra cada requisição no log
                         env ACCESS_LOG, padrão true
  --base-path            caminho de base: a API responde tudo abaixo dele
                         env BASE_PATH, padrão /lacnic
  --cors-origin          valor de Access-Control-Allow-Origin
                         env CORS_ORIGIN, padrão *
  --dataset-poll         intervalo de conferência da versão dos dados
                         env DATASET_POLL, padrão 30s
  --db-pool-max          conexões máximas no pool do Postgres
                         env DB_POOL_MAX, padrão 10
  --db-timeout           tempo máximo de cada consulta
                         env DB_TIMEOUT, padrão 5s
  --http-port            porta HTTP
                         env HTTP_PORT, padrão 8080
  --log-format           json ou text
                         env LOG_FORMAT, padrão json
  --log-level            debug, info, warn ou error
                         env LOG_LEVEL, padrão info
  --postgres-url         URL do Postgres, ex.: postgres://postgres:senha@badblock-postgres:5432/badblock (obrigatória)
                         env POSTGRES_URL, padrão ""
  --real-ip-headers      ordem de leitura dos headers de IP real
                         env REAL_IP_HEADERS, padrão X-Forwarded-For,X-Real-IP
  --redis-cache-enabled  liga ou desliga o cache
                         env REDIS_CACHE_ENABLED, padrão true
  --redis-key-ttl        validade das chaves de cache, em segundos
                         env REDIS_KEY_TTL, padrão 3600
  --redis-timeout        tempo máximo de cada operação no cache (fail-open)
                         env REDIS_TIMEOUT, padrão 50ms
  --redis-url            URL do Valkey/Redis, ex.: redis://:senha@badblock-valkey:6379/0 (vazio desliga o cache)
                         env REDIS_URL, padrão ""
  --trusted-proxies      faixas cujos X-Forwarded-For/X-Real-IP são aceitos
                         env TRUSTED_PROXIES, padrão 127.0.0.0/8,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,fc00::/7
  --version              mostra a versão e sai
  -h, --help             mostra esta ajuda
```

Num clone, mudam `api-<rir>`, `RIR <Title>`, `<rir>_*`, `collector-<rir>` e
`/<rir>` (inclusive o padrão de `--base-path`).

## Operação

```bash
curl http://127.0.0.1:8105/lacnic/status          # sem Traefik, pela porta do loopback
curl http://127.0.0.1:8105/lacnic/asn/61613
make -C apps/lacnic/api smoke                      # /status, /meta, /asn/61613 e /openapi.yaml
make -C apps/lacnic/api logs
```

Arquivo real ([../rir/api.md](../rir/api.md#arquivo-real-make-test-real)):

```bash
curl -o /tmp/delegated https://ftp.lacnic.net/pub/stats/lacnic/delegated-lacnic-extended-latest
make -C apps/lacnic/api test-real FILE=/tmp/delegated   # LACNIC_REAL_FILE
```

## Medições

Arquivo real de 2026-09-28 (97.301 registros; 16.515 registros de ASN e
80.786 blocos gravados):

| Medição | Valor |
|---|---|
| Carga pelo `collector-lacnic` (no `make test-real`, PG18 descartável) | ~1,3–1,7 s |
| Uma consulta no Postgres | `/asn` ~0,01 ms (índice único de `asn_start`); `/ip` ~0,1 ms (GiST) |
| `make test-real` (handler completo, sem cache) | 500 `/ip` aleatórios em 0,33 ms de média; 500 `/asn` em 0,16 ms; o maior titular (`124194`, 36 KB) em 3,5 ms |
| Por HTTP, binário no host e Postgres/Valkey no Docker Desktop | 1.000 `/ip` aleatórios em 5,7 ms de média sem cache e 2,6 ms com cache (quase tudo é o ida e volta ao Docker) |

Conferido de ponta a ponta em 2026-09-28, com os dados reais: com o Valkey
parado, `/status` fica `degraded` e as rotas seguem respondendo (5 tentativas
de ~50 ms; depois o disjuntor abre e as respostas voltam a ~1,5 ms); com o
Postgres parado, `/status` responde 503 e as consultas fora do cache,
`503 database_unavailable`, enquanto as que estão no cache continuam 200. Os
dois voltam sozinhos.

## Testes

Os do modelo, sem nada da LACNIC além dos dados:
[../rir/api.md](../rir/api.md#testes). O store falso e o seed do teste de
integração usam registros do [recorte](fonte.md#recorte-testdatadelegated-extended-sampletxt)
(AS61613, AS28003–AS28005, titulares `258500` e `130343`), iguais nos clones.
