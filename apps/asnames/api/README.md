# api-asnames

API HTTP (JSON, só leitura) com o nome e o país de todos os ASNs alocados, do
arquivo [`asn.txt`](https://ftp.ripe.net/ripe/asnames/asn.txt) do RIPE NCC
(que cobre todos os RIRs), servida abaixo de `/asnames`. Faz par com
[`collector-asnames`](../collector/) na fonte `asnames`.

```bash
curl https://api.badblock.net.br/asnames/asn/15169
curl https://api.badblock.net.br/asnames/v1/country/BR
curl https://api.badblock.net.br/asnames/handle/GOOGLE
curl 'https://api.badblock.net.br/asnames/search?q=google+llc'
```

Especificações (fonte da verdade): [`specs/fontes/asnames/api.md`](../../../specs/fontes/asnames/api.md) e o padrão comum em [`specs/padroes/api.md`](../../../specs/padroes/api.md). Manifesto OpenAPI 3.1: [`openapi/openapi.yaml`](openapi/openapi.yaml), servido em `/asnames/openapi.yaml`.

```bash
make test                                    # unitários
make test-int                                # integração (PG18 via testcontainers, migrations reais)
make lint                                    # golangci-lint
make up                                      # sobe só este serviço, com o .env da raiz
make smoke                                   # consulta a API local (porta 8108)
ASNAMES_REAL_FILE=/tmp/asn.txt make test-int # com o asn.txt real inteiro, baixado antes com curl
```
