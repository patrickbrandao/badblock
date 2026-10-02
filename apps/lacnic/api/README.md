# api-lacnic

API HTTP das delegações de ASNs e blocos IP do RIR LACNIC (arquivo
delegated-extended): o registro que contém um ASN, o bloco mais específico de
um IP ou prefixo e os recursos de um titular, abaixo de `/lacnic`. Faz par com
[`collector-lacnic`](../collector/) na fonte `lacnic`, e é o modelo das APIs
dos outros RIRs.

```bash
curl https://api.badblock.net.br/lacnic/asn/61610
curl https://api.badblock.net.br/lacnic/v1/ip/187.87.29.10
curl https://api.badblock.net.br/lacnic/prefix/2804:8ae0::/32
curl https://api.badblock.net.br/lacnic/holder/258500
```

Especificações (fonte da verdade):
[`specs/fontes/lacnic/api.md`](../../../specs/fontes/lacnic/api.md), o modelo
dos RIRs em [`specs/fontes/rir/api.md`](../../../specs/fontes/rir/api.md) e o
padrão comum em [`specs/padroes/api.md`](../../../specs/padroes/api.md).
Manifesto OpenAPI 3.1: [`openapi/openapi.yaml`](openapi/openapi.yaml), servido
em `/lacnic/openapi.yaml`.

```bash
make test                            # unitários
make test-int                        # integração (PG18 via testcontainers, migrations reais)
make test-real FILE=/tmp/delegated   # carga real pelo collector + medições
make lint                            # golangci-lint
make vet                             # go vet, com os testes de integração
make up                              # sobe só este serviço, com o .env da raiz
make smoke                           # consulta a API local
```
