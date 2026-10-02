# api-anatel-pst

API HTTP (JSON, só leitura) com as prestadoras de serviços de
telecomunicações da Anatel — CNPJ, razão social, endereço da sede e os
serviços notificados (SCM 045, STFC 171, SMP 010, SeAC 750…) com a outorga e
os números Fistel —, do arquivo
[`prestadoras_servicos_telecomunicacoes.zip`](https://www.anatel.gov.br/dadosabertos/paineis_de_dados/outorga_e_licenciamento/prestadoras_servicos_telecomunicacoes.zip),
servida abaixo de `/anatel/pst`. Faz par com
[`collector-anatel-pst`](../collector/) na fonte `anatel/pst`.

```bash
curl https://api.badblock.net.br/anatel/pst/provider/02558157000162
curl https://api.badblock.net.br/anatel/pst/provider/02.558.157%2F0001-62   # o mesmo CNPJ, formatado
curl https://api.badblock.net.br/anatel/pst/services                        # catálogo de serviços
curl https://api.badblock.net.br/anatel/pst/service/045?state=SP            # prestadoras de SCM em SP
curl 'https://api.badblock.net.br/anatel/pst/search?q=telefonica&limit=10'
curl https://api.badblock.net.br/anatel/pst/meta
```

Especificações (fonte da verdade): [`specs/fontes/anatel/pst/api.md`](../../../../specs/fontes/anatel/pst/api.md) e o padrão comum em [`specs/padroes/api.md`](../../../../specs/padroes/api.md). Manifesto OpenAPI 3.1: [`openapi/openapi.yaml`](openapi/openapi.yaml), servido em `/anatel/pst/openapi.yaml`.

```bash
make test                               # unitários
make test-int                           # integração (PG18 via testcontainers, migrations reais, recorte carregado pelo collector)
make test-real                          # o ZIP inteiro do dia, carregado pelo collector (ou FILE=/caminho/pst.zip)
make lint                               # golangci-lint
make up                                 # sobe só este serviço, com o .env da raiz
make smoke                              # consulta a API local (porta 8112; SMOKE_CNPJ=02558157000162)
```
