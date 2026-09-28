# registry-api

API HTTP de consulta dos ASNs e prefixos IP do BadBlock. Lê só as views do
schema `api` do PostgreSQL (role `badblock_api`), com cache no Valkey, e roda
atrás do Traefik. Especificação: [`openapi/openapi.yaml`](openapi/openapi.yaml),
servida em `/openapi.yaml`, com documentação interativa em `/docs`.

## Rotas

| Rota | Descrição |
|---|---|
| `GET /v1/ip/{ip}` | Prefixo mais específico, titular, ASNs ligados, cadeia IANA→RIR→NIC.br, `flags.bogon`/`special` |
| `GET /v1/ip` | O mesmo para o IP de quem chama (`X-Client-IP-Source` diz de onde veio) |
| `GET /v1/asn/{asn}` | ASN, titular, prefixos (vínculo `nicbr` ou `holder`), blocos da IANA |
| `GET /v1/prefix/{ip}/{len}` | Prefixo exato ou o bloco que o contém, e as delegações dentro dele |
| `GET /v1/holder/{rir}/{id}` | Titular, seus ASNs e prefixos (paginado) |
| `GET /v1/country/{cc}/prefixes`, `/asns` | Listas por país; `?format=txt` (e `&aggregate=true`) para firewall |
| `GET /v1/rir/{rir}/prefixes`, `/asns` | Listas por RIR, com `?country=` opcional |
| `GET /v1/{asn,prefix,holder}/.../history` | Eventos do `change_log` (insert, update, remove, restore) |
| `GET /v1/meta/sources` | Estado e data de cada fonte |
| `GET /asn/{asn}` | Formato do POC antigo (compatibilidade), 404 com `{}` |
| `GET\|POST /health`, `/status`; `GET /ping` | Saúde: `online`, `degraded` (cache fora) ou `offline` (503) |

Listas: `status` (padrão `allocated,assigned`; `all` para todos), `family`
(`4` ou `6`), `limit` (1–1000, padrão 100) e `cursor` (o `next_cursor` da página
anterior).

## Comportamento

- **IP real**: `X-Forwarded-For`/`X-Real-IP` só valem se a conexão vier de
  `TRUSTED_PROXIES`; o XFF é lido da direita para a esquerda.
- **Cache**: respostas no Valkey com a versão do dataset na chave; lookups de IP
  compartilham a chave do /24 ou /48. Cabeçalho `X-Cache: HIT|MISS|BYPASS`.
  Valkey fora: a API segue pelo Postgres e `/status` fica `degraded`.
- **ETag** com a versão do dataset: `If-None-Match` devolve 304.
- Antes da primeira sincronização, as rotas de dados respondem 503
  `dataset_not_ready`.
- Erros: `{"error": {"code": "...", "message": "..."}}`.

## Configuração

| Variável | Padrão | Descrição |
|---|---|---|
| `POSTGRES_URL` | — | URL com o role `badblock_api` (obrigatória) |
| `REDIS_URL` | — | `redis://:senha@badblock-valkey:6379/0`; vazio desliga o cache |
| `REDIS_CACHE_ENABLED` | `true` | Liga/desliga o cache |
| `REDIS_KEY_TTL` | `3600` | Validade das chaves (s) |
| `REDIS_TIMEOUT` | `50ms` | Tempo máximo por operação no cache |
| `HTTP_PORT` | `8001` | Porta HTTP |
| `TRUSTED_PROXIES` | faixas privadas e loopback | De onde aceitar os headers de IP real |
| `REAL_IP_HEADERS` | `X-Forwarded-For,X-Real-IP` | Ordem de leitura |
| `DB_POOL_MAX` | `20` | Conexões no pool |
| `DB_TIMEOUT` / `LIST_TIMEOUT` | `5s` / `60s` | Tempo máximo das consultas |
| `DATASET_POLL` | `30s` | Conferência periódica da versão (além do NOTIFY) |
| `CORS_ORIGIN` | `*` | `Access-Control-Allow-Origin` |
| `ACCESS_LOG` | `true` | Uma linha de log por requisição |
| `LOG_LEVEL` / `LOG_FORMAT` | `info` / `json` | Logs no stdout |

No compose, o Traefik aplica o rate limit (`API_RATE_*` no `.env`) e publica a
API em `API_FQDN`.

## Desenvolvimento

```bash
make test        # unitários: rotas (store falso), IP real, cache, agregação, config
make test-int    # integração: consultas no PG18 real, NOTIFY, Valkey real
make lint
make e2e         # ponta a ponta via Traefik (stack da raiz no ar)
```

## Release e deploy

```bash
make release V=1.2.0   # tag registry-api/v1.2.0 → imagem tmsoftbrasil/badblock-registry-api:1.2.0
make deploy            # pull + up deste serviço no servidor (deploy.env na raiz)
```
