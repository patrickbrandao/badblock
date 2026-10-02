# Coletor da LACNIC (`collector-lacnic`)

O `collector-lacnic` é o **modelo** dos coletores de RIR: o comportamento está
em [../../padroes/coletor.md](../../padroes/coletor.md) e em
[../rir/collector.md](../rir/collector.md), sem nada de diferente. Aqui, os
valores, as medições e os números da LACNIC.

## Valores

| Opção | Valor na LACNIC |
|---|---|
| `SOURCE_URL` | `https://ftp.lacnic.net/pub/stats/lacnic/delegated-lacnic-extended-latest` (`rir.DefaultSourceURL`) |
| `SOURCE_MD5_URL` | vazio = `SOURCE_URL` + `.md5` |
| `MIN_RECORDS` | `50000` (`rir.DefaultMinRecords`) |
| `SYNC_INTERVAL`, `RETRY_INTERVAL`, `RUN_TIMEOUT`, `REMOVAL_THRESHOLD` | `1h`, `5m`, `10m`, `0.05` (os do padrão) |

- **`MIN_RECORDS` = 50000** porque o arquivo real tinha 97.301 registros em
  2026-09-28: o mínimo fica em cerca de metade, com ~50% de folga para a
  variação diária; abaixo disso o arquivo é tratado como truncado.
- A LACNIC publica um arquivo por dia (~23:56, UTC−3, [fonte.md](fonte.md#publicação)):
  com `SYNC_INTERVAL` de 1h, o arquivo novo é aplicado até ~1h depois de
  publicado, e as outras verificações do dia param na checagem 1 (MD5
  publicado igual).

## Medições

Arquivo real de 2026-09-28 (97.301 registros), Postgres 18 local:

| Etapa | Tempo |
|---|---|
| Download | ~9 s |
| Parser | ~75 ms |
| Primeira carga | ~0,7 s |
| Reaplicação sem mudança | ~0,2 s |
| Verificação sem mudança pelo MD5 publicado | ~75 ms |

## Compose e `.env`

- Variáveis: `COLLECTOR_LACNIC_TAG=latest`,
  `COLLECTOR_LACNIC_SYNC_INTERVAL=1h`, `COLLECTOR_LACNIC_RETRY_INTERVAL=5m`,
  `COLLECTOR_LACNIC_SOURCE_URL=`, `COLLECTOR_LACNIC_SOURCE_MD5_URL=`,
  `COLLECTOR_LACNIC_MIN_RECORDS=` e `COLLECTOR_LACNIC_REMOVAL_THRESHOLD=0.05`.
- No `.env.example` da raiz, o bloco começa com
  `# collector-lacnic: delegações da LACNIC (delegated-extended). MIN_RECORDS vazio = 50000.`
- Imagem `tmsoftbrasil/badblock-collector-lacnic`, label `description`
  `Importa as delegações de ASNs e blocos IP da LACNIC (delegated-extended) para o PostgreSQL do BadBlock`;
  container `badblock-collector-lacnic`.

## Testes

Arquivo real ([../rir/collector.md](../rir/collector.md#arquivo-real-make-test-real)):

```bash
curl -o /tmp/delegated https://ftp.lacnic.net/pub/stats/lacnic/delegated-lacnic-extended-latest
make -C apps/lacnic/collector test-real FILE=/tmp/delegated   # LACNIC_REAL_FILE
```

Os números do recorte ([fonte.md](fonte.md#recorte-testdatadelegated-extended-sampletxt))
que os testes citam — são os que um clone troca pelos do seu recorte:

- `TestParseSample`: cabeçalho `2.3`, `lacnic`, 21 registros; 7 asn, 8 ipv4
  e 6 ipv6; 8 + 6 blocos; nenhum aviso. AS61610 (BR, allocated, `258500`,
  2023-02-27); AS28003 + 3 = AS28005 (available, sem país, data nem titular);
  AS6065 (reserved, opaque-id vazio no fim da linha); `187.87.28.0/22`
  (`258500`, `record_value` 1024); `187.192.0.0/11` (MX, 2097152);
  `45.68.105.0/24` (reserved, vazios); `2001:1201:20::/43` (available, 43);
  `2001:12f8::/48` (assigned, `114721`).
- `collector_test.go`: recorte de serial 20260927 e `enddate` 20260925;
  `MinRecords` 5 (e 22 em `TestTooFewRecordsIsRecorded`). Mais antigo que o
  aplicado quando este tem `enddate` 20260926 (serial 20260920); o mesmo
  `enddate` e serial 20260928; ou só o serial 20260928. Aplica quando o
  aplicado tem `enddate` 20260924 e serial 20260928; o mesmo cabeçalho;
  serial `1790600421096`; ou serial `x`.
- `TestApplyLifecycle`: a carga insere 7 ASNs e 14 blocos (8 IPv4, 6 IPv6);
  `format_version` `2.3`, `utc_offset` `-0300`, `header_records` 21; as
  consultas de [dados.md](dados.md#consultas-no-recorte). Passo das mudanças,
  com `REMOVAL_THRESHOLD` 0.2: AS6064 sai, AS26596 vira allocated (BR,
  titular `999`), AS64500 entra; `2.152.0.0/22` sai, `2.152.252.0/22` passa
  ao titular `999` e `62.122.208.0` + 1280 entra (2 blocos) → ASN 1/1/1 e
  blocos 2/1/1 (inseridos/alterados/apagados). Trava: um dataset com 2 ASNs e
  2 blocos é recusado com 0.05 e, com `--force`, apaga 5 ASNs e 13 blocos.

## `--help`

Saída real (versão `dev`, 2026-09-29):

```
collector-lacnic — importa o arquivo delegated-extended do RIR LACNIC (ASNs e blocos IP
delegados pelo RIR) para as tabelas lacnic_* do PostgreSQL do BadBlock.

Verifica a fonte a cada SYNC_INTERVAL e só aplica quando o arquivo muda
(MD5 publicado, ETag/Last-Modified e hash do conteúdo). Não tem API HTTP.

Uso:
  collector-lacnic [opções]

Opções (padrão → variável de ambiente → argumento):
  --log-format           json ou text
                         env LOG_FORMAT, padrão json
  --log-level            debug, info, warn ou error
                         env LOG_LEVEL, padrão info
  --min-records          abaixo disso (registros ASN + IPv4 + IPv6) o arquivo é tratado como truncado e recusado
                         env MIN_RECORDS, padrão 50000
  --postgres-url         URL do Postgres, ex.: postgres://postgres:senha@badblock-postgres:5432/badblock (obrigatória)
                         env POSTGRES_URL, padrão ""
  --removal-threshold    fração máxima de registros de ASN ou blocos removidos de uma vez sem --force
                         env REMOVAL_THRESHOLD, padrão 0.05
  --retry-interval       espera até a próxima tentativa depois de uma verificação que falhou
                         env RETRY_INTERVAL, padrão 5m
  --run-timeout          tempo máximo de uma verificação (download + aplicação)
                         env RUN_TIMEOUT, padrão 10m
  --source-md5-url       hash publicado; vazio = SOURCE_URL + ".md5", "off" desliga a conferência
                         env SOURCE_MD5_URL, padrão ""
  --source-url           arquivo delegated-extended do RIR LACNIC
                         env SOURCE_URL, padrão https://ftp.lacnic.net/pub/stats/lacnic/delegated-lacnic-extended-latest
  --sync-interval        intervalo entre verificações da fonte
                         env SYNC_INTERVAL, padrão 1h
  --user-agent           User-Agent das requisições (vazio = badblock-collector-lacnic/<versão>)
                         env USER_AGENT, padrão ""
  --once                 faz uma verificação e sai
  --force                aplica mesmo sem mudança ou com arquivo mais antigo e ignora
                         a trava de remoção (implica --once)
  --version              mostra a versão e sai
  -h, --help             mostra esta ajuda
```

Num clone, mudam `collector-<rir>`, `RIR <Title>`, `<rir>_*`, o padrão de
`--min-records` e o de `--source-url`.
