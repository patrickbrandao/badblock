# api-afrinic

API HTTP das delegações de ASNs e blocos IP do RIR AFRINIC (arquivo
delegated-extended): o registro que contém um ASN, o bloco mais específico de
um IP ou prefixo e os recursos de um titular, abaixo de `/afrinic`. Faz par
com [`collector-afrinic`](../collector/) na fonte `afrinic`; é um clone da
[`api-lacnic`](../../lacnic/api/), o modelo das APIs de RIR.

```bash
curl https://api.badblock.net.br/afrinic/asn/37100
curl https://api.badblock.net.br/afrinic/v1/ip/196.1.88.10
curl https://api.badblock.net.br/afrinic/prefix/2c0f:feb0::/32
curl https://api.badblock.net.br/afrinic/holder/F365C741
```

Especificações (fonte da verdade):
[`specs/fontes/afrinic/api.md`](../../../specs/fontes/afrinic/api.md), o
modelo dos RIRs em [`specs/fontes/rir/api.md`](../../../specs/fontes/rir/api.md)
e o padrão comum em [`specs/padroes/api.md`](../../../specs/padroes/api.md).
Manifesto OpenAPI 3.1: [`openapi/openapi.yaml`](openapi/openapi.yaml), servido
em `/afrinic/openapi.yaml`.

```bash
make test                            # unitários
make test-int                        # integração (PG18 via testcontainers, migrations reais)
make test-real FILE=/tmp/delegated   # carga real pelo collector + medições
make lint                            # golangci-lint
make vet                             # go vet, com os testes de integração
make up                              # sobe só este serviço, com o .env da raiz
make smoke                           # consulta a API local
```
