# LACNIC

Fonte `lacnic`: o arquivo delegated-extended do RIR LACNIC, com as delegações
de ASNs e blocos IPv4/IPv6 (país, data, status e titular, o opaque-id). É uma
fonte da [família RIR](../rir/README.md) e o **modelo** dela: os coletores e
as APIs dos outros quatro RIRs são clones dos apps desta fonte, e a spec comum
fica em [../rir/](../rir/README.md). Esta pasta traz só o que é da LACNIC.

| Item | Valor |
|---|---|
| Quem publica | LACNIC, um arquivo por dia ([fonte.md](fonte.md)) |
| Arquivo | `https://ftp.lacnic.net/pub/stats/lacnic/delegated-lacnic-extended-latest` |
| Coletor | `collector-lacnic`, em [`apps/lacnic/collector/`](../../../apps/lacnic/collector/README.md) |
| API | `api-lacnic`, em [`apps/lacnic/api/`](../../../apps/lacnic/api/README.md) |
| Sub-agentes | [`collector-lacnic`](../../../.claude/agents/collector-lacnic.md) (esta pasta, menos a API; e o modelo em `../rir/`) e [`api-lacnic`](../../../.claude/agents/api-lacnic.md) (`api.md`, a seção [API](#api) e `../rir/api.md`) |
| Tabelas | `lacnic_asn`, `lacnic_prefix`, `lacnic_run` ([dados.md](dados.md)), migrations em [`database/postgres/lacnic/`](../../../database/postgres/lacnic/); linha `collector-lacnic` em `jobs` |
| Caminho | `/lacnic/` (`https://api.badblock.net.br/lacnic/`) |
| Porta local | 8105 (`http://127.0.0.1:8105/lacnic/`) |
| Parâmetros | [coletores](../rir/README.md#parâmetros-dos-coletores) e [APIs](../rir/README.md#parâmetros-das-apis) |

## Arquivos

| Arquivo | Conteúdo | Dono |
|---|---|---|
| [README.md](README.md) | este resumo | `collector-lacnic` (a seção API, `api-lacnic`) |
| [fonte.md](fonte.md) | URLs, publicação, validadores, `.md5`, exemplo real, fatos medidos, o recorte de `testdata/` | `collector-lacnic` |
| [dados.md](dados.md) | o que as tabelas `lacnic_*` têm de particular (o modelo está em [../rir/dados.md](../rir/dados.md)) | `collector-lacnic` |
| [collector.md](collector.md) | valores das opções, `MIN_RECORDS` e o porquê, medições, `--help` (o modelo está em [../rir/collector.md](../rir/collector.md)) | `collector-lacnic` |
| [api.md](api.md) | valores da `api-lacnic`, respostas reais, ETags, `--help` e medições (o modelo está em [../rir/api.md](../rir/api.md)) | `api-lacnic` |

## API

A `api-lacnic` serve as tabelas `lacnic_*` abaixo de `/lacnic/`: registro que
contém um ASN, bloco mais específico de um IP ou prefixo, recursos de um
titular e o estado dos dados. É o modelo das APIs de RIR: rotas, validações,
cache, manifesto, testes e a clonagem estão em [../rir/api.md](../rir/api.md);
[api.md](api.md) traz os valores da LACNIC (porta 8105, `SMOKE_ASN` 61613),
as respostas reais do arquivo de 2026-09-28, os ETags, o `--help` e as
medições.
