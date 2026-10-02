# APNIC

Fonte `apnic`: o arquivo delegated-extended do RIR APNIC (Ásia-Pacífico),
com as delegações de ASNs e blocos IPv4/IPv6 (país, data, status e titular, o
opaque-id). É uma fonte da [família RIR](../rir/README.md): o coletor e a API
são clones dos apps da LACNIC, o modelo, e a spec comum fica em
[../rir/](../rir/README.md). Esta pasta traz só o que é da APNIC — o que a
distingue dos outros RIRs (URL sem `/pub`, 27 linhas de comentário antes do
cabeçalho, `startdate` vazia, blocos IPv6 até `/64`, `.md5` publicado ~9 min
depois do arquivo) está em [fonte.md](fonte.md).

| Item | Valor |
|---|---|
| Quem publica | APNIC, um arquivo por dia ([fonte.md](fonte.md)) |
| Arquivo | `https://ftp.apnic.net/stats/apnic/delegated-apnic-extended-latest` (sem `/pub`) |
| Coletor | `collector-apnic`, em [`apps/apnic/collector/`](../../../apps/apnic/collector/README.md) — clone do `collector-lacnic` |
| API | `api-apnic`, em [`apps/apnic/api/`](../../../apps/apnic/api/README.md) — clone da `api-lacnic` |
| Sub-agentes | [`collector-apnic`](../../../.claude/agents/collector-apnic.md) (esta pasta, menos a API) e [`api-apnic`](../../../.claude/agents/api-apnic.md) (`api.md` e a seção [API](#api)); o modelo em `../rir/` é do `collector-lacnic` e da `api-lacnic` |
| Tabelas | `apnic_asn`, `apnic_prefix`, `apnic_run` ([dados.md](dados.md)), migrations em [`database/postgres/apnic/`](../../../database/postgres/apnic/); linha `collector-apnic` em `jobs` |
| Caminho | `/apnic/` (`https://api.badblock.net.br/apnic/`) |
| Porta local | 8103 (`http://127.0.0.1:8103/apnic/`) |
| Parâmetros | [coletores](../rir/README.md#parâmetros-dos-coletores) e [APIs](../rir/README.md#parâmetros-das-apis) |

## Arquivos

| Arquivo | Conteúdo | Dono |
|---|---|---|
| [README.md](README.md) | este resumo | `collector-apnic` (a seção API, `api-apnic`) |
| [fonte.md](fonte.md) | URLs, publicação, validadores, `.md5`, formato e exemplo real, fatos medidos, o recorte de `testdata/` | `collector-apnic` |
| [dados.md](dados.md) | o que as tabelas `apnic_*` têm de particular (o modelo está em [../rir/dados.md](../rir/dados.md)) | `collector-apnic` |
| [collector.md](collector.md) | valores das opções, `MIN_RECORDS` e o porquê, a janela do `.md5`, medições, testes, `--help` (o modelo está em [../rir/collector.md](../rir/collector.md)) | `collector-apnic` |
| [api.md](api.md) | valores da `api-apnic`, respostas reais, ETags, o que a APNIC muda nas respostas e medições (o modelo está em [../rir/api.md](../rir/api.md)) | `api-apnic` |

## API

A `api-apnic` serve as tabelas `apnic_*` abaixo de `/apnic/`: registro que
contém um ASN, bloco mais específico de um IP ou prefixo, recursos de um
titular e o estado dos dados. É um clone da `api-lacnic`, o modelo das APIs
de RIR: rotas, validações, cache, manifesto e testes estão em
[../rir/api.md](../rir/api.md); [api.md](api.md) traz os valores da APNIC
(porta 8103, `SMOKE_ASN` 4608), as respostas reais do arquivo de 2026-09-28
(serial 20260929), os ETags, o que a APNIC muda nas respostas (`cc` e
`start_date` `null`, todo IPv4 CIDR, faixas de ASN, o maior titular) e as
medições.
