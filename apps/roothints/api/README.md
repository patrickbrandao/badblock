# api-roothints

API HTTP (JSON, só leitura) dos servidores raiz do DNS — nome, letra, IPv4, IPv6, TTLs e a nota de cada um dos 13 servidores do arquivo [`named.root`](https://www.internic.net/domain/named.root) da InterNIC (root hints) —, servida abaixo de `/roothints`. Faz par com [`collector-roothints`](../collector/) na fonte `roothints`.

```bash
curl https://api.badblock.net.br/roothints/servers
curl https://api.badblock.net.br/roothints/server/k
curl https://api.badblock.net.br/roothints/v1/server/A.ROOT-SERVERS.NET.
```

Especificações (fonte da verdade): [`specs/fontes/roothints/api.md`](../../../specs/fontes/roothints/api.md) e o padrão comum em [`specs/padroes/api.md`](../../../specs/padroes/api.md). Manifesto OpenAPI 3.1: [`openapi/openapi.yaml`](openapi/openapi.yaml), servido em `/roothints/openapi.yaml`.

```bash
make test                                   # unitários
make test-int                               # integração (PG18 via testcontainers, migrations reais, named.root real da fixture)
make test-real FILE=/tmp/named.root         # o arquivo do dia (baixado com curl), com tempos no log
make lint                                   # golangci-lint
make up                                     # sobe só este serviço, com o .env da raiz
make smoke                                  # consulta a API local (porta 8109)
```
