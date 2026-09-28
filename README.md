# BadBlock

Dados públicos de registro da Internet — ASNs e prefixos IPv4/IPv6 do Brasil e
do mundo — importados das fontes oficiais, sincronizados de hora em hora e
servidos por uma API HTTP.

```
 IANA · AFRINIC · APNIC · ARIN · LACNIC · RIPE NCC · NIC.br · asn.txt (RIPE)
                               │  GET condicional, MD5, validação
                               ▼
                       ┌───────────────┐        ┌────────────────────────┐
                       │ registry-sync │──────▶ │ PostgreSQL 18          │
                       └───────────────┘ COPY + │  ingest   (por fonte)  │
                                         MERGE  │  registry (central +   │
                                                │            histórico)  │
                                                │  api      (views)      │
                                                └───────────┬────────────┘
                                                  NOTIFY    │ SELECT
                                                            ▼
 cliente ──HTTPS──▶ Traefik ──▶ registry-api ◀──cache──▶ Valkey
             (rate limit por IP, X-Forwarded-For confiável só do Traefik)
```

## O que tem aqui

| Pasta | Componente |
|---|---|
| [`apps/registry-sync`](apps/registry-sync) | Importa e sincroniza as 17 fontes públicas e reconstrói as tabelas centrais (Go) |
| [`apps/registry-api`](apps/registry-api) | API HTTP de consulta de IP, ASN, prefixo, titular, listas e histórico (Go) |
| [`database/postgresql`](database/postgresql) | PostgreSQL 18: bootstrap, migrations (dbmate), backup diário |
| [`database/valkey`](database/valkey) | Cache da API |
| [`infra/traefik`](infra/traefik) | Traefik local com HTTPS em `*.badblock.localhost` (desenvolvimento) |
| [`tests/e2e`](tests/e2e) | Teste ponta a ponta via Traefik |
| [`docs`](docs) | Decisões, arquitetura, fontes e operação |

## Começando

Requisitos: Docker (com Compose v2), Go 1.27, `make` e `openssl`.

```bash
make up
```

O `make up` gera o `.env` com senhas aleatórias, cria as redes Docker e o
certificado local, compila as imagens e sobe tudo. A primeira sincronização
baixa ~46 MB das fontes e leva uns 2 minutos; acompanhe com
`docker logs -f badblock-registry-sync`.

```bash
curl --cacert infra/traefik/certs/ca.pem https://api.badblock.localhost/v1/ip/45.171.60.1
```

Com o [mkcert](https://github.com/FiloSottile/mkcert) instalado
(`brew install mkcert && mkcert -install`), `make certs` emite um certificado
confiável para o navegador e o `--cacert` deixa de ser necessário.
Documentação interativa: <https://api.badblock.localhost/docs>.

## A API em exemplos

```bash
GET /v1/ip/45.171.60.1              # prefixo, titular, ASNs, cadeia IANA→RIR, bogon
GET /v1/ip                          # o mesmo, para o IP de quem chama
GET /v1/asn/61613                   # ASN, titular, prefixos, blocos da IANA
GET /v1/prefix/45.171.60.0/22       # prefixo exato ou o bloco que o contém
GET /v1/holder/lacnic/258500        # titular com todos os ASNs e prefixos
GET /v1/country/BR/prefixes?format=txt&aggregate=true   # lista para firewall
GET /v1/asn/61613/history           # o que mudou e quando
GET /asn/61613                      # formato do POC antigo (compatibilidade)
```

```json
{
  "ip": "45.171.60.1",
  "prefix": { "cidr": "45.171.60.0/22", "level": "rir", "rir": "LACNIC", "country": "BR",
              "status": "allocated", "registered": "2019-02-11",
              "rdap": "https://rdap.lacnic.net/rdap/ip/45.171.60.0/22" },
  "holder": { "id": "lacnic:258500", "name": "TMSoft Solucoes em Informatica Ltda",
              "name_source": "nicbr", "document": "08.030.063/0001-00" },
  "asns": [ { "asn": 61613, "name": "TMSoft Solucoes em Informatica Ltda", "link": "nicbr" } ],
  "chain": [ { "cidr": "45.0.0.0/8", "level": "iana", "rir": "ARIN", "status": "legacy", ... },
             { "cidr": "45.171.60.0/22", "level": "rir", "rir": "LACNIC", ... } ],
  "flags": { "bogon": false, "special": null },
  "dataset": { "version": 1, "updated_at": "2026-09-28T19:43:53Z" }
}
```

`country` é o país de registro no RIR, não geolocalização. Os vínculos
ASN ↔ prefixo são de registro (NIC.br ou mesmo titular), não de roteamento
BGP. Especificação completa em [`openapi.yaml`](apps/registry-api/openapi/openapi.yaml).

## Desenvolvimento

```bash
make test        # unitários dos dois apps
make test-int    # integração (PostgreSQL 18 e Valkey via testcontainers)
make lint        # golangci-lint
make e2e         # ponta a ponta contra o stack no ar
make -C database/postgresql test   # bootstrap e migrations: up, rollback, up
```

Cada app também tem o próprio `Makefile` (`make -C apps/registry-api help`).
Regras do repositório para pessoas e agentes: [`AGENTS.md`](AGENTS.md).

## Documentação

- [Decisões da fase 1](docs/decisoes-fase-1.md) — o que foi decidido e por quê
- [Arquitetura](docs/arquitetura.md) — modelo de dados, sincronização, cache, IP real
- [Fontes](docs/fontes.md) — de onde vêm os dados e as particularidades de cada uma
- [Operação](docs/operacao.md) — deploy, backup, restauração, troca de senhas, corte do n8n

## Licença

[MIT](LICENSE). Os dados servidos vêm de fontes públicas da IANA, dos RIRs, do
NIC.br e do RIPE NCC; `/v1/meta/sources` mostra a origem e a data de cada uma.
