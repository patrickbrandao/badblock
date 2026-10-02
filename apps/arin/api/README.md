# api-arin

API HTTP das delegações de ASNs e blocos IP do RIR ARIN (arquivo
delegated-extended): o registro que contém um ASN, o bloco mais específico de
um IP ou prefixo e os recursos de um titular, abaixo de `/arin`. Faz par com
[`collector-arin`](../collector/) na fonte `arin`; é um clone da
[`api-lacnic`](../../lacnic/api/), o modelo das APIs de RIR.

```bash
curl https://api.badblock.net.br/arin/asn/7018
curl https://api.badblock.net.br/arin/v1/ip/12.34.56.78
curl https://api.badblock.net.br/arin/prefix/2001:1890::/29
curl https://api.badblock.net.br/arin/holder/81e05477cc28a48ed3088e3408139c2a
```

Especificações (fonte da verdade):
[`specs/fontes/arin/api.md`](../../../specs/fontes/arin/api.md), o modelo
dos RIRs em [`specs/fontes/rir/api.md`](../../../specs/fontes/rir/api.md) e o
padrão comum em [`specs/padroes/api.md`](../../../specs/padroes/api.md).
Manifesto OpenAPI 3.1: [`openapi/openapi.yaml`](openapi/openapi.yaml), servido
em `/arin/openapi.yaml`.

```bash
make test                            # unitários
make test-int                        # integração (PG18 via testcontainers, migrations reais)
make test-real FILE=/tmp/delegated   # carga real pelo collector + medições
make lint                            # golangci-lint
make vet                             # go vet, com os testes de integração
make up                              # sobe só este serviço, com o .env da raiz
make smoke                           # consulta a API local
```
