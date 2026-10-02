# AFRINIC

Fonte `afrinic`: o arquivo delegated-extended do RIR AFRINIC (África), com as
delegações de ASNs e blocos IPv4/IPv6 (país, data, status e titular, o
opaque-id). É uma fonte da [família RIR](../rir/README.md): o
`collector-afrinic` e a `api-afrinic` são clones dos apps do
[`lacnic`](../lacnic/README.md), o modelo, e a spec comum fica em
[../rir/](../rir/README.md). Esta pasta traz só o que é da AFRINIC — o menor
dos cinco arquivos (19.786 registros em 2026-09-28), o cabeçalho na versão
`2` com `startdate` `00000000` e `UTCoffset` `00000`, `ZZ` no país de todo
available/reserved, o opaque-id hex de 8 caracteres, 52 registros IPv4 que
não formam CIDR e um servidor lento, com o certificado TLS válido até
2026-10-23 ([fonte.md](fonte.md)).

| Item | Valor |
|---|---|
| Quem publica | AFRINIC, um arquivo por dia, por volta de 00:07 UTC ([fonte.md](fonte.md)) |
| Arquivo | `https://ftp.afrinic.net/pub/stats/afrinic/delegated-afrinic-extended-latest` |
| Coletor | `collector-afrinic`, em [`apps/afrinic/collector/`](../../../apps/afrinic/collector/README.md) — clone do `collector-lacnic` |
| API | `api-afrinic`, em [`apps/afrinic/api/`](../../../apps/afrinic/api/README.md) — clone da `api-lacnic` |
| Sub-agentes | [`collector-afrinic`](../../../.claude/agents/collector-afrinic.md) (esta pasta, menos a API) e [`api-afrinic`](../../../.claude/agents/api-afrinic.md) (`api.md` e a seção [API](#api)); o modelo em `../rir/` é do `collector-lacnic` e da `api-lacnic` |
| Tabelas | `afrinic_asn`, `afrinic_prefix`, `afrinic_run` ([dados.md](dados.md)), migrations em [`database/postgres/afrinic/`](../../../database/postgres/afrinic/); linha `collector-afrinic` em `jobs` |
| Caminho | `/afrinic/` (`https://api.badblock.net.br/afrinic/`) |
| Porta local | 8102 (`http://127.0.0.1:8102/afrinic/`) |
| Parâmetros | [coletores](../rir/README.md#parâmetros-dos-coletores) e [APIs](../rir/README.md#parâmetros-das-apis) |

## Arquivos

| Arquivo | Conteúdo | Dono |
|---|---|---|
| [README.md](README.md) | este resumo | `collector-afrinic` (a seção API, `api-afrinic`) |
| [fonte.md](fonte.md) | URLs, publicação, validadores, `.md5`, servidor e certificado TLS, formato e exemplo real, fatos medidos, o recorte de `testdata/` | `collector-afrinic` |
| [dados.md](dados.md) | o que as tabelas `afrinic_*` têm de particular (o modelo está em [../rir/dados.md](../rir/dados.md)) | `collector-afrinic` |
| [collector.md](collector.md) | valores das opções, `MIN_RECORDS` e o porquê, o servidor lento e o certificado, medições, testes, `--help` (o modelo está em [../rir/collector.md](../rir/collector.md)) | `collector-afrinic` |
| [api.md](api.md) | valores da `api-afrinic`, respostas reais, o que a AFRINIC muda nelas (`ZZ`, um ASN por registro, registros não-CIDR, opaque-id hex), ETags, manifesto e medições (o modelo está em [../rir/api.md](../rir/api.md)) | `api-afrinic` |

## API

A `api-afrinic` serve as tabelas `afrinic_*` abaixo de `/afrinic/`: registro
que contém um ASN, bloco mais específico de um IP ou prefixo, recursos de um
titular e o estado dos dados. É um clone da `api-lacnic`, o modelo das APIs
de RIR: rotas, validações, cache, manifesto, testes e a clonagem estão em
[../rir/api.md](../rir/api.md); [api.md](api.md) traz os valores da AFRINIC
(porta 8102, `SMOKE_ASN` 37100), as respostas reais do arquivo de
2026-09-28, o que a AFRINIC muda nelas (`cc` `ZZ` em available/reserved, um
ASN por registro, 52 registros IPv4 não-CIDR em 146 blocos, opaque-id hex
maiúsculo de 8 caracteres), os ETags, as particularidades do manifesto e as
medições.
