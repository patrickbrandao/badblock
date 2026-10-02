# RIPE NCC

Fonte `ripencc`: o arquivo delegated-extended do RIR RIPE NCC, com as
delegações de ASNs e blocos IPv4/IPv6 (país, data, status e titular, o
opaque-id). É uma fonte da [família RIR](../rir/README.md): o
`collector-ripencc` e a `api-ripencc` são clones dos apps da
[LACNIC](../lacnic/README.md), o modelo, e a spec comum fica em
[../rir/](../rir/README.md). Esta pasta traz só o que é do RIPE NCC — em
especial, o opaque-id é um UUID **novo a cada arquivo diário**
([fonte.md](fonte.md#opaque-id-novo-a-cada-arquivo)).

O `asn.txt` do RIPE NCC (nomes de AS) é outra fonte, [`ripe/asnames`](../ripe/asnames/README.md).

| Item | Valor |
|---|---|
| Quem publica | RIPE NCC, um arquivo por dia ([fonte.md](fonte.md)) |
| Arquivo | `https://ftp.ripe.net/pub/stats/ripencc/delegated-ripencc-extended-latest` |
| Coletor | `collector-ripencc`, em [`apps/ripencc/collector/`](../../../apps/ripencc/collector/README.md) |
| API | `api-ripencc`, em [`apps/ripencc/api/`](../../../apps/ripencc/api/README.md) |
| Sub-agentes | [`collector-ripencc`](../../../.claude/agents/collector-ripencc.md) (esta pasta, menos a API) e [`api-ripencc`](../../../.claude/agents/api-ripencc.md) (`api.md` e a seção [API](#api)); o modelo em `../rir/` é dos sub-agentes do lacnic |
| Tabelas | `ripencc_asn`, `ripencc_prefix`, `ripencc_run` ([dados.md](dados.md)), migrations em [`database/postgres/ripencc/`](../../../database/postgres/ripencc/); linha `collector-ripencc` em `jobs` |
| Caminho | `/ripencc/` (`https://api.badblock.net.br/ripencc/`) |
| Porta local | 8106 (`http://127.0.0.1:8106/ripencc/`) |
| Parâmetros | [coletores](../rir/README.md#parâmetros-dos-coletores) e [APIs](../rir/README.md#parâmetros-das-apis) |

## Arquivos

| Arquivo | Conteúdo | Dono |
|---|---|---|
| [README.md](README.md) | este resumo | `collector-ripencc` (a seção API, `api-ripencc`) |
| [fonte.md](fonte.md) | URLs, publicação, validadores, `.md5`, exemplo real, opaque-id diário, fatos medidos, o recorte de `testdata/` | `collector-ripencc` |
| [dados.md](dados.md) | o que as tabelas `ripencc_*` têm de particular (o modelo está em [../rir/dados.md](../rir/dados.md)) | `collector-ripencc` |
| [collector.md](collector.md) | valores das opções, `MIN_RECORDS` e o porquê, atualização diária, medições, `--help` (o modelo está em [../rir/collector.md](../rir/collector.md)) | `collector-ripencc` |
| [api.md](api.md) | valores da `api-ripencc`, o que o opaque-id diário muda na API, respostas reais, ETags, `--help` e medições (o modelo está em [../rir/api.md](../rir/api.md)) | `api-ripencc` |

## API

A `api-ripencc` serve as tabelas `ripencc_*` abaixo de `/ripencc/`: registro
que contém um ASN, bloco mais específico de um IP ou prefixo, recursos de um
titular e o estado dos dados. É um clone da `api-lacnic`, o modelo das APIs
de RIR: rotas, validações, cache, manifesto e testes estão em
[../rir/api.md](../rir/api.md); [api.md](api.md) traz os valores do RIPE NCC
(porta 8106, `SMOKE_ASN` 3333, `OpaqueIDChangesDaily` `true`), o que o
[opaque-id diário](api.md#opaque-id-diário) muda na API — o opaque-id de
`/ripencc/holder/{opaque_id}` só vale dentro do dataset atual
([dados.md](dados.md#opaque_id-e-updated_at)) —, as respostas reais do
arquivo de 2026-09-28, os ETags, o `--help` e as medições.
