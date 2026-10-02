# BadBlock

Dados públicos sobre ASNs, blocos IP, a raiz do DNS e prestadoras de telecomunicações, servidos por uma API HTTP aberta em
`https://api.badblock.net.br`.

Cada fonte de dados tem um par de apps: um **coletor**, que importa o arquivo
da fonte para o PostgreSQL só quando ele muda, e uma **API**, dona de um
caminho próprio no host da API.

| Fonte | Coletor | API |
|---|---|---|
| NIC.br / registro.br — ASNs, titulares (CNPJ) e blocos IP brasileiros | [`collector-cgibr`](apps/cgibr/collector/) | [`api-cgibr`](apps/cgibr/api/) em `/cgibr/` |
| AFRINIC — delegações de ASNs e blocos (África) | [`collector-afrinic`](apps/afrinic/collector/) | [`api-afrinic`](apps/afrinic/api/) em `/afrinic/` |
| APNIC — delegações de ASNs e blocos (Ásia-Pacífico) | [`collector-apnic`](apps/apnic/collector/) | [`api-apnic`](apps/apnic/api/) em `/apnic/` |
| ARIN — delegações de ASNs e blocos (América do Norte) | [`collector-arin`](apps/arin/collector/) | [`api-arin`](apps/arin/api/) em `/arin/` |
| LACNIC — delegações de ASNs e blocos (América Latina e Caribe) | [`collector-lacnic`](apps/lacnic/collector/) | [`api-lacnic`](apps/lacnic/api/) em `/lacnic/` |
| RIPE NCC — delegações de ASNs e blocos (Europa, Oriente Médio, Ásia Central) | [`collector-ripencc`](apps/ripencc/collector/) | [`api-ripencc`](apps/ripencc/api/) em `/ripencc/` |
| IANA — blocos por RIR, uso especial (bogons) e servidores RDAP | [`collector-iana`](apps/iana/collector/) | [`api-iana`](apps/iana/api/) em `/iana/` |
| RIPE NCC `asn.txt` — nome e país de todos os ASNs alocados | [`collector-ripe-asnames`](apps/ripe/asnames/collector/) | [`api-ripe-asnames`](apps/ripe/asnames/api/) em `/ripe/asnames/` |
| InterNIC `named.root` — nomes e endereços dos 13 servidores raiz do DNS | [`collector-roothints`](apps/roothints/collector/) | [`api-roothints`](apps/roothints/api/) em `/roothints/` |
| InterNIC `root.zone` — a zona raiz do DNS: TLDs, servidores, glue e DS | [`collector-rootzone`](apps/rootzone/collector/) | [`api-rootzone`](apps/rootzone/api/) em `/rootzone/` |
| IANA `root-anchors.xml` — âncoras de confiança DNSSEC da raiz | [`collector-rootanchors`](apps/rootanchors/collector/) | [`api-rootanchors`](apps/rootanchors/api/) em `/rootanchors/` |
| Anatel — prestadoras de serviços de telecomunicações (CNPJ) e serviços notificados (SCM, STFC, SMP…) | [`collector-anatel-pst`](apps/anatel/pst/collector/) | [`api-anatel-pst`](apps/anatel/pst/api/) em `/anatel/pst/` |

```bash
curl https://api.badblock.net.br/cgibr/asn/61610
curl https://api.badblock.net.br/lacnic/ip/187.87.29.10
curl https://api.badblock.net.br/arin/asn/7018
curl https://api.badblock.net.br/ripencc/prefix/193.0.0.0/21
curl https://api.badblock.net.br/iana/ip/10.0.0.1
curl https://api.badblock.net.br/ripe/asnames/asn/15169
curl https://api.badblock.net.br/roothints/server/K.ROOT-SERVERS.NET.
curl https://api.badblock.net.br/rootzone/tld/br
curl https://api.badblock.net.br/rootanchors/key/20326
curl https://api.badblock.net.br/anatel/pst/provider/02558157000162
```

Toda API responde também `/<fonte>/v1/...` (versão fixa), `/<fonte>/meta`
(estado dos dados), `/<fonte>/status`, `/<fonte>/ping` e o próprio manifesto
OpenAPI 3.1 em `/<fonte>/openapi.yaml`
(ex.: `curl https://api.badblock.net.br/cgibr/openapi.yaml`).

## Documentação

As especificações em [`specs/`](specs/README.md) são a fonte da verdade:
arquitetura, infraestrutura, o padrão dos apps e o detalhe de cada fonte —
as rotas de cada API, com exemplos reais, estão em `specs/fontes/<fonte>/api.md`
(ex.: [`cgibr`](specs/fontes/cgibr/api.md)). Para contribuir (pessoas e
agentes de IA), comece por [`AGENTS.md`](AGENTS.md).

## Rodando localmente

Precisa de Docker e, para os testes, Go 1.27.

```bash
make up                                    # gera .env, cria as redes, sobe tudo
curl http://127.0.0.1:8101/cgibr/status    # portas 8101 a 8112, uma por API
make test test-int lint vet                # testes e lint de todos os apps
make specs-check                           # links, estrutura e mapa das specs
```

A primeira carga de cada coletor leva menos de um minuto depois que o
Postgres fica pronto. A porta de cada API está em
[`specs/fontes/README.md`](specs/fontes/README.md).

## Estrutura

- `specs/` — especificações do projeto inteiro.
- `apps/<fonte>/` — uma pasta por fonte, com os apps dela: `collector/` e
  `api/`, cada um com deploy e container próprios.
- `database/postgres/` — todo o SQL de schema: `central/` (tabela `jobs`) e
  uma pasta por fonte.
- `database/valkey/` — cache das APIs.

## Licença

MIT — veja [`LICENSE`](LICENSE).
