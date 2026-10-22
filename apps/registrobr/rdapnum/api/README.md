# api-registrobr-rdapnum

API HTTP (JSON), pública, do RDAP do registro.br por ASN e bloco que o
BadBlock coleta, servida abaixo de `/registrobr/rdapnum`: o ASN com titular,
contatos e blocos, o bloco que contém um IP ou prefixo (com as delegações
reversas), tudo de um CNPJ, os objetos de um contato, a resposta RDAP crua e o
histórico dela, e o pedido de atualização (`POST .../refresh`), que entra numa
fila do Valkey que o [`collector-registrobr-rdapnum`](../collector/) atende
antes da fila normal. Faz par com ele na fonte `registrobr/rdapnum`.

```bash
curl https://api.badblock.net.br/registrobr/rdapnum/asn/61610
curl https://api.badblock.net.br/registrobr/rdapnum/ip/200.225.48.10
curl https://api.badblock.net.br/registrobr/rdapnum/document/35980592000130
curl https://api.badblock.net.br/registrobr/rdapnum/asn/61610/raw
curl -X POST https://api.badblock.net.br/registrobr/rdapnum/asn/61610/refresh
curl https://api.badblock.net.br/registrobr/rdapnum/openapi.yaml
```

Os dados vêm do RDAP do NIC.br, que proíbe redistribuição e reprodução; a
decisão de servi-los mesmo assim é a #42 de
[`specs/projeto/decisoes.md`](../../../../specs/projeto/decisoes.md).

Especificações (fonte da verdade): [`specs/fontes/registrobr/rdapnum/api.md`](../../../../specs/fontes/registrobr/rdapnum/api.md), [`api-rotas.md`](../../../../specs/fontes/registrobr/rdapnum/api-rotas.md) e o padrão comum em [`specs/padroes/api.md`](../../../../specs/padroes/api.md). Manifesto OpenAPI 3.1: [`openapi/openapi.yaml`](openapi/openapi.yaml), servido em `/registrobr/rdapnum/openapi.yaml`.

```bash
make test          # unitários
make test-int      # integração (PG18 e Valkey via testcontainers, migrations reais)
make lint          # golangci-lint
make up            # sobe só este serviço, com o .env da raiz
make smoke         # consulta a API local (SMOKE_ASN=22548)
```
