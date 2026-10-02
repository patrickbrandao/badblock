# api-rootanchors

API HTTP (JSON, só leitura) com as âncoras de confiança DNSSEC da zona raiz
(as KSKs do [`root-anchors.xml`](https://data.iana.org/root-anchors/root-anchors.xml)
da IANA), com o registro DS e o DNSKEY prontos em texto, servida abaixo de
`/rootanchors`. Faz par com [`collector-rootanchors`](../collector/) na fonte
`rootanchors`.

```bash
curl https://api.badblock.net.br/rootanchors/keys
curl https://api.badblock.net.br/rootanchors/key/20326
curl https://api.badblock.net.br/rootanchors/v1/key/38696
```

Não há campo "ativa" (dependeria do relógio numa resposta em cache): uma chave
está ativa em `t` quando `valid_from <= t` e `valid_until` é `null` ou maior
que `t`.

Especificações (fonte da verdade): [`specs/fontes/rootanchors/api.md`](../../../specs/fontes/rootanchors/api.md), [`specs/fontes/rootanchors/api-rotas.md`](../../../specs/fontes/rootanchors/api-rotas.md) e o padrão comum em [`specs/padroes/api.md`](../../../specs/padroes/api.md). Manifesto OpenAPI 3.1: [`openapi/openapi.yaml`](openapi/openapi.yaml), servido em `/rootanchors/openapi.yaml`.

```bash
make test                                     # unitários
make test-int                                 # integração (PG18 via testcontainers, migrations reais; carrega o arquivo real com o collector)
make test-real FILE=/tmp/root-anchors.xml     # só o teste com o arquivo real, com tempos (baixe antes com curl; sem FILE, a fixture do collector)
make lint                                     # golangci-lint
make up                                       # sobe só este serviço, com o .env da raiz
make smoke                                    # consulta a API local (porta 8111)
```
