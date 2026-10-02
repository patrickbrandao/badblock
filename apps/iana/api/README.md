# api-iana

API HTTP, só leitura, dos registros de numeração da IANA — a quem cada faixa
de ASN e bloco IP foi entregue, blocos e ASNs de uso especial (bogons) e
servidores RDAP —, servida abaixo de `/iana`. Faz par com
[`collector-iana`](../collector/) na fonte `iana`.

```bash
curl https://api.badblock.net.br/iana/asn/61610
curl https://api.badblock.net.br/iana/ip/10.0.0.1          # bogon: true
curl https://api.badblock.net.br/iana/v1/prefix/2001:db8::/48
curl https://api.badblock.net.br/iana/special
```

Especificações (fonte da verdade): [`specs/fontes/iana/api.md`](../../../specs/fontes/iana/api.md) e o padrão comum em [`specs/padroes/api.md`](../../../specs/padroes/api.md). Manifesto OpenAPI 3.1: [`openapi/openapi.yaml`](openapi/openapi.yaml), servido em `/iana/openapi.yaml`.

```bash
make test          # unitários
make test-int      # integração (PG18 via testcontainers, migrations reais, recorte real)
make test-real     # integração com o dataset inteiro do dia, carregado pelo collector-iana
make lint          # golangci-lint
make up            # sobe só este serviço, com o .env da raiz
make smoke         # consulta a API local
```
