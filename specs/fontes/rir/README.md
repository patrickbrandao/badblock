# Família RIR: modelo dos cinco RIRs

`rir/` **não é uma fonte**: não existe `apps/rir/`, pasta
`database/postgres/rir/` nem tabela `rir_*`. É a spec comum das cinco fontes
de RIR, que publicam o mesmo formato (delegated-extended) e são tratadas pelo
mesmo código. O que vale para os cinco está aqui; a pasta de cada RIR traz só
os valores, fatos e exemplos dele.

As cinco fontes: [`afrinic`](../afrinic/README.md), [`apnic`](../apnic/README.md),
[`arin`](../arin/README.md), [`lacnic`](../lacnic/README.md) (o modelo) e
[`ripencc`](../ripencc/README.md). Apps, tabelas, caminhos e portas estão na
[tabela das fontes](../README.md); as decisões nº 10 (cada RIR é uma fonte) e
nº 11 (o mesmo código, clonado do lacnic) estão em
[../../projeto/decisoes.md](../../projeto/decisoes.md).

## Organização do código

- Os cinco coletores (`apps/<rir>/collector/`) são o mesmo código, clonado do
  `collector-lacnic`; as cinco APIs (`apps/<rir>/api/`), da `api-lacnic`. Não
  há código compartilhado: cada clone é uma cópia
  ([../../projeto/arquitetura.md](../../projeto/arquitetura.md#regras), regra 2).
- O que é do RIR e não sai do nome dele fica em `internal/rir/rir.go` de cada
  app. No coletor, as constantes abaixo (valores em
  [Parâmetros dos coletores](#parâmetros-dos-coletores)); na API, `Source`,
  `App`, `Collector`, `Title`, `SourceURL`, `OpaqueIDChangesDaily` e
  `BasePath` ([api.md](api.md#código); valores em
  [Parâmetros das APIs](#parâmetros-das-apis)).

  | Constante | Uso |
  |---|---|
  | `Source` | nome da fonte: prefixo das tabelas, pasta `database/postgres/<rir>/` (o teste de integração lê as migrations de lá), caminho da API |
  | `App` | `"collector-" + Source`: `jobs.app`, atributo `app` do log, User-Agent, `application_name` no Postgres, chave do advisory lock, prefixo dos erros de configuração |
  | `Registry` | valor esperado no campo `registry` do cabeçalho e de cada registro do arquivo |
  | `Title` | nome do RIR nos textos (`--help`) |
  | `DefaultSourceURL` | padrão de `SOURCE_URL`; o `.md5` padrão é essa URL + `.md5` |
  | `DefaultMinRecords` | padrão de `MIN_RECORDS`: ≈ metade dos registros medidos no arquivo real (folga de ~50%) |

- O nome do RIR aparece também, literal, onde a clonagem o troca por `sed`:
  nomes SQL em `internal/store` (`<rir>_run`, `<rir>_asn`, `<rir>_prefix`),
  module Go, `cmd/collector-<rir>/`, arquivos de build e deploy, a variável
  `<RIR>_REAL_FILE` dos testes e comentários.
- Por RIR, além do `rir.go`: `testdata/` (recorte do próprio arquivo e dos
  outros quatro, ver [formato.md](formato.md#fixtures)), os testes que citam
  os números do recorte, os textos dos `COMMENT ON` da migration
  `database/postgres/<rir>/` e a pasta `specs/fontes/<rir>/`.

## Manutenção dos clones

- Correção ou mudança no código comum (`parse`, `fetch`, `collector`, `store`,
  `config`, `main.go`) é feita no modelo (`collector-lacnic`) e **repetida nos
  quatro clones**, cada um pelo seu sub-agente
  ([../../processos/fluxo-de-trabalho.md](../../processos/fluxo-de-trabalho.md#quem-cuida-de-quê)).
- A spec do modelo muda **uma vez**, aqui em `rir/`. A pasta de um RIR só muda
  quando um valor, fato ou exemplo daquele RIR muda.
- O que é de um RIR nunca vai para o código comum: fica em
  `internal/rir/rir.go` e na pasta do RIR. Uma particularidade que exija
  código (ex.: um formato de `.md5` que só um RIR usa) vira regra genérica do
  modelo, valendo para os cinco.
- Nas APIs, o mesmo: todo pacote menos `internal/rir` muda na `api-lacnic` e
  nos quatro clones.
- Coletor novo a partir do modelo:
  [collector.md](collector.md#clonar-o-coletor-para-outro-rir); API nova:
  [api.md](api.md#clonar-a-api-para-outro-rir). Schema: as cinco migrations
  mudam juntas ([dados.md](dados.md#mudar-o-schema-da-família)).

## Arquivos

| Arquivo | Conteúdo | Dono |
|---|---|---|
| [formato.md](formato.md) | formato delegated-extended, arquivo `.md5`, regras do parser, fixtures, comparação entre os cinco RIRs | sub-agente `collector-lacnic` |
| [fixtures.md](fixtures.md) | conteúdo literal das fixtures `testdata/formats/*.txt` | sub-agente `collector-lacnic` |
| [dados.md](dados.md) | tabelas `<rir>_asn`, `<rir>_prefix` e `<rir>_run`, mapeamento arquivo → colunas, consultas da API, mudança de schema | sub-agente `collector-lacnic` |
| [collector.md](collector.md) | coletor do modelo: checagens de mudança, arquivo mais antigo, validações, aplicação, `--force`, opções, testes, clonagem | sub-agente `collector-lacnic` |
| [api.md](api.md) | API do modelo: o `internal/rir` da API, rotas, validações, chaves de cache, campos, manifesto, testes, clonagem | sub-agente `api-lacnic` |

Na pasta de cada RIR, `dados.md`, `collector.md` e `api.md` começam apontando
o arquivo do modelo e trazem só o que é do RIR; o `fonte.md` do RIR traz URLs,
publicação, fatos medidos e o recorte, e segue [formato.md](formato.md)
([../README.md](../README.md#pasta-de-uma-fonte)).

## Parâmetros dos coletores

Valores de `internal/rir/rir.go` dos cinco coletores (conferidos em
2026-09-29). Os registros medidos são os do arquivo real de 2026-09-28,
citados no comentário de `DefaultMinRecords`.

| Fonte | `Title` | `DefaultSourceURL` | `DefaultMinRecords` | Registros medidos |
|---|---|---|---|---|
| `afrinic` | `AFRINIC` | `https://ftp.afrinic.net/pub/stats/afrinic/delegated-afrinic-extended-latest` | `10000` | 19.786 (o menor dos cinco) |
| `apnic` | `APNIC` | `https://ftp.apnic.net/stats/apnic/delegated-apnic-extended-latest` (sem `/pub`) | `95000` | 190.268 (serial 20260929) |
| `arin` | `ARIN` | `https://ftp.arin.net/pub/stats/arin/delegated-arin-extended-latest` | `100000` | 203.051 |
| `lacnic` (modelo) | `LACNIC` | `https://ftp.lacnic.net/pub/stats/lacnic/delegated-lacnic-extended-latest` | `50000` | 97.301 |
| `ripencc` | `RIPE NCC` | `https://ftp.ripe.net/pub/stats/ripencc/delegated-ripencc-extended-latest` | `130000` | 260.793 |

Nos cinco, `Source` e `Registry` são o nome da fonte (primeira coluna) e
`App` é `collector-<rir>`.

## Parâmetros das APIs

Valores de `internal/rir/rir.go` das cinco APIs e do `Makefile` delas
(`PORT := $${API_<RIR>_HOST_PORT:-<porta>}`, `SMOKE_ASN ?= <asn>`),
conferidos em 2026-09-29. O que cada constante faz está em
[api.md](api.md#código).

| Fonte | `Title` | `SourceURL` | `OpaqueIDChangesDaily` | Porta (`PORT`) | `SMOKE_ASN` |
|---|---|---|---|---|---|
| `afrinic` | `AFRINIC` | `https://ftp.afrinic.net/pub/stats/afrinic/delegated-afrinic-extended-latest` | `false` | 8102 | 37100 |
| `apnic` | `APNIC` | `https://ftp.apnic.net/stats/apnic/delegated-apnic-extended-latest` (sem `/pub`) | `false` | 8103 | 4608 |
| `arin` | `ARIN` | `https://ftp.arin.net/pub/stats/arin/delegated-arin-extended-latest` | `false` | 8104 | 7018 |
| `lacnic` (modelo) | `LACNIC` | `https://ftp.lacnic.net/pub/stats/lacnic/delegated-lacnic-extended-latest` | `false` | 8105 | 61610 |
| `ripencc` | `RIPE NCC` | `https://ftp.ripe.net/pub/stats/ripencc/delegated-ripencc-extended-latest` | `true` | 8106 | 3333 |

Nos cinco, `Source` é o nome da fonte (primeira coluna), `App` é
`api-<rir>`, `Collector` é `collector-<rir>` e `BasePath` é `/<rir>`.
`SourceURL` é a mesma URL do `DefaultSourceURL` do coletor, e só o RIPE NCC
gera opaque-ids novos a cada arquivo
([../ripencc/fonte.md](../ripencc/fonte.md#opaque-id-novo-a-cada-arquivo)).
