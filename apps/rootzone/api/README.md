# api-rootzone

API HTTP (JSON, só leitura) com a zona raiz do DNS — TLDs delegados,
servidores de nome, glue A/AAAA e DS —, do arquivo
[`root.zone`](https://www.internic.net/domain/root.zone) da InterNIC, servida
abaixo de `/rootzone`. Faz par com [`collector-rootzone`](../collector/) na
fonte `rootzone`.

```bash
curl https://api.badblock.net.br/rootzone/tlds
curl https://api.badblock.net.br/rootzone/tld/br
curl https://api.badblock.net.br/rootzone/v1/tld/xn--p1ai
curl https://api.badblock.net.br/rootzone/tld/%D1%80%D1%84      # рф, o mesmo que xn--p1ai
curl https://api.badblock.net.br/rootzone/meta                  # serial e SOA da zona
```

Especificações (fonte da verdade): [`specs/fontes/rootzone/api.md`](../../../specs/fontes/rootzone/api.md) e o padrão comum em [`specs/padroes/api.md`](../../../specs/padroes/api.md). Manifesto OpenAPI 3.1: [`openapi/openapi.yaml`](openapi/openapi.yaml), servido em `/rootzone/openapi.yaml`.

```bash
make test                               # unitários
make test-int                           # integração (PG18 via testcontainers, migrations reais, recorte carregado pelo collector)
make test-real                          # o root.zone inteiro do dia, carregado pelo collector (ou FILE=/caminho/root.zone)
make lint                               # golangci-lint
make up                                 # sobe só este serviço, com o .env da raiz
make smoke                              # consulta a API local (porta 8110; SMOKE_TLD=br)
```
