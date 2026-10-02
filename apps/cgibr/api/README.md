# api-cgibr

API HTTP (JSON), pública e só de leitura, dos ASNs, titulares (CNPJ) e blocos
IP brasileiros que o NIC.br publica, servida abaixo de `/cgibr`. Faz par com
[`collector-cgibr`](../collector/) na fonte `cgibr`.

```bash
curl https://api.badblock.net.br/cgibr/asn/61610
curl https://api.badblock.net.br/cgibr/v1/ip/187.87.29.10
curl https://api.badblock.net.br/cgibr/document/35980592000130
curl https://api.badblock.net.br/cgibr/openapi.yaml
```

Especificações (fonte da verdade): [`specs/fontes/cgibr/api.md`](../../../specs/fontes/cgibr/api.md) e o padrão comum em [`specs/padroes/api.md`](../../../specs/padroes/api.md). Manifesto OpenAPI 3.1: [`openapi/openapi.yaml`](openapi/openapi.yaml), servido em `/cgibr/openapi.yaml`.

```bash
make test          # unitários
make test-int      # integração (PG18 via testcontainers, migrations reais)
make lint          # golangci-lint
make up            # sobe só este serviço, com o .env da raiz
make smoke         # consulta a API local
```
