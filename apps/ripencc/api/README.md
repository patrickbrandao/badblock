# api-ripencc

API HTTP das delegações de ASNs e blocos IP do RIPE NCC (arquivo
delegated-extended): o registro que contém um ASN, o bloco mais específico de
um IP ou prefixo e os recursos de um titular, abaixo de `/ripencc` — o
opaque_id do titular é novo a cada arquivo diário e só vale dentro do dataset
atual. Faz par com [`collector-ripencc`](../collector/) na fonte `ripencc`; é
um clone da [`api-lacnic`](../../lacnic/api/), o modelo das APIs de RIR.

```bash
curl https://api.badblock.net.br/ripencc/asn/3333
curl https://api.badblock.net.br/ripencc/v1/ip/193.0.6.139
# Titular: o opaque_id muda todo dia; pegue o atual por /asn ou /ip.
id=$(curl -s https://api.badblock.net.br/ripencc/asn/3333 | jq -r .opaque_id)
curl https://api.badblock.net.br/ripencc/holder/$id
```

Especificações (fonte da verdade):
[`specs/fontes/ripencc/api.md`](../../../specs/fontes/ripencc/api.md), o
modelo dos RIRs em [`specs/fontes/rir/api.md`](../../../specs/fontes/rir/api.md)
e o padrão comum em [`specs/padroes/api.md`](../../../specs/padroes/api.md).
Manifesto OpenAPI 3.1: [`openapi/openapi.yaml`](openapi/openapi.yaml), servido
em `/ripencc/openapi.yaml`.

```bash
make test                            # unitários
make test-int                        # integração (PG18 via testcontainers, migrations reais)
make test-real FILE=/tmp/delegated   # carga real pelo collector + medições
make lint                            # golangci-lint
make vet                             # go vet, com os testes de integração
make up                              # sobe só este serviço, com o .env da raiz
make smoke                           # consulta a API local
```
