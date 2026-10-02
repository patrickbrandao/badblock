# ARIN

Fonte `arin`: o arquivo delegated-extended do RIR ARIN, com as delegações de
ASNs e blocos IPv4/IPv6 (país, data, status e titular, o opaque-id). É uma
fonte da [família RIR](../rir/README.md): o `collector-arin` e a `api-arin`
são clones dos apps do [`lacnic`](../lacnic/README.md), o modelo, e a spec
comum fica em [../rir/](../rir/README.md). Esta pasta traz só o que é da
ARIN — entre outras coisas, o `serial` em época Unix em milissegundos, o
`.md5` no formato GNU, os vários servidores atrás de `ftp.arin.net` e os
registros IPv4 reservados que não formam um CIDR ([fonte.md](fonte.md)).

| Item | Valor |
|---|---|
| Quem publica | ARIN, um arquivo por dia ([fonte.md](fonte.md)) |
| Arquivo | `https://ftp.arin.net/pub/stats/arin/delegated-arin-extended-latest` |
| Coletor | `collector-arin`, em [`apps/arin/collector/`](../../../apps/arin/collector/README.md) |
| API | `api-arin`, em [`apps/arin/api/`](../../../apps/arin/api/README.md) |
| Sub-agentes | [`collector-arin`](../../../.claude/agents/collector-arin.md) (esta pasta, menos a API) e [`api-arin`](../../../.claude/agents/api-arin.md) (`api.md` e a seção [API](#api)) |
| Tabelas | `arin_asn`, `arin_prefix`, `arin_run` ([dados.md](dados.md)), migrations em [`database/postgres/arin/`](../../../database/postgres/arin/); linha `collector-arin` em `jobs` |
| Caminho | `/arin/` (`https://api.badblock.net.br/arin/`) |
| Porta local | 8104 (`http://127.0.0.1:8104/arin/`) |
| Parâmetros | [coletores](../rir/README.md#parâmetros-dos-coletores) e [APIs](../rir/README.md#parâmetros-das-apis) |

## Arquivos

| Arquivo | Conteúdo | Dono |
|---|---|---|
| [README.md](README.md) | este resumo | `collector-arin` (a seção API, `api-arin`) |
| [fonte.md](fonte.md) | URLs, publicação, validadores e servidores, `.md5`, o `serial` em ms, exemplo real, fatos medidos, o recorte de `testdata/` | `collector-arin` |
| [dados.md](dados.md) | o que as tabelas `arin_*` têm de particular (o modelo está em [../rir/dados.md](../rir/dados.md)) | `collector-arin` |
| [collector.md](collector.md) | valores das opções, `MIN_RECORDS` e o porquê, medições, testes, `--help` (o modelo está em [../rir/collector.md](../rir/collector.md)) | `collector-arin` |
| [api.md](api.md) | valores da `api-arin`, particularidades nas respostas, respostas reais, ETags, textos do manifesto, `--help` e medições (o modelo está em [../rir/api.md](../rir/api.md)) | `api-arin` |

## API

A `api-arin` serve as tabelas `arin_*` abaixo de `/arin/`: registro que
contém um ASN, bloco mais específico de um IP ou prefixo, recursos de um
titular e o estado dos dados. É um clone da `api-lacnic`, o modelo das APIs
de RIR: rotas, validações, cache, manifesto, testes e a clonagem estão em
[../rir/api.md](../rir/api.md); [api.md](api.md) traz os valores da ARIN
(porta 8104, `SMOKE_ASN` 7018), as particularidades que mudam as respostas
(`status` sem distinção entre alocação e designação, opaque-id hex de 32,
registros IPv4 não-CIDR, o maior titular dos cinco RIRs), as respostas reais
do arquivo de 2026-09-28, os ETags, os textos do manifesto e as medições.
