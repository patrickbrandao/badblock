# api-apnic

API HTTP das delegações de ASNs e blocos IP do RIR APNIC (arquivo
delegated-extended): o registro que contém um ASN, o bloco mais específico de
um IP ou prefixo e os recursos de um titular, abaixo de `/apnic`. Faz par com
[`collector-apnic`](../collector/) na fonte `apnic`; é um clone da
[`api-lacnic`](../../lacnic/api/), o modelo das APIs de RIR.

```bash
curl https://api.badblock.net.br/apnic/asn/4608
curl https://api.badblock.net.br/apnic/v1/ip/1.1.1.1
curl https://api.badblock.net.br/apnic/prefix/2001:dc0::/32
curl https://api.badblock.net.br/apnic/holder/A91DC5BE
```

Especificações (fonte da verdade):
[`specs/fontes/apnic/api.md`](../../../specs/fontes/apnic/api.md), o modelo
dos RIRs em [`specs/fontes/rir/api.md`](../../../specs/fontes/rir/api.md) e o
padrão comum em [`specs/padroes/api.md`](../../../specs/padroes/api.md).
Manifesto OpenAPI 3.1: [`openapi/openapi.yaml`](openapi/openapi.yaml), servido
em `/apnic/openapi.yaml`.

```bash
make test                            # unitários
make test-int                        # integração (PG18 via testcontainers, migrations reais)
make test-real FILE=/tmp/delegated   # carga real pelo collector + medições
make lint                            # golangci-lint
make vet                             # go vet, com os testes de integração
make up                              # sobe só este serviço, com o .env da raiz
make smoke                           # consulta a API local
```
