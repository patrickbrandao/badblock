# api-rdns

API HTTP (JSON), pública, do DNS reverso (PTR) dos IPv4 que o BadBlock varre,
servida abaixo de `/rdns`: o PTR de um IP, os de um prefixo de /24 a /31 e,
com `live=true`, uma resolução na hora pedida ao
[`collector-rdns`](../collector/) pela fila do Valkey. Faz par com ele na
fonte `rdns`.

```bash
curl https://api.badblock.net.br/rdns/ip/200.160.2.3
curl 'https://api.badblock.net.br/rdns/ip/200.160.2.3?live=true'
curl https://api.badblock.net.br/rdns/prefix/200.160.2.0/24
curl https://api.badblock.net.br/rdns/openapi.yaml
```

Especificações (fonte da verdade): [`specs/fontes/rdns/api.md`](../../../specs/fontes/rdns/api.md) e o padrão comum em [`specs/padroes/api.md`](../../../specs/padroes/api.md). Manifesto OpenAPI 3.1: [`openapi/openapi.yaml`](openapi/openapi.yaml), servido em `/rdns/openapi.yaml`.

```bash
make test          # unitários
make test-int      # integração (PG18 e Valkey via testcontainers, migrations reais)
make lint          # golangci-lint
make up            # sobe só este serviço, com o .env da raiz
make smoke         # consulta a API local (SMOKE_IP=200.160.2.3)
```
