# Coletor da APNIC (`collector-apnic`)

O `collector-apnic` é um **clone** do `collector-lacnic`, o modelo dos
coletores de RIR: o comportamento está em
[../../padroes/coletor.md](../../padroes/coletor.md) e em
[../rir/collector.md](../rir/collector.md), sem nada de diferente. O código
comum é o do modelo com o nome trocado (conferido em 2026-09-29), e o que é
da APNIC fica em `internal/rir/rir.go`
([../rir/README.md](../rir/README.md#organização-do-código)). Aqui, os
valores, as medições e os números da APNIC.

## Valores

| Opção | Valor na APNIC |
|---|---|
| `SOURCE_URL` | `https://ftp.apnic.net/stats/apnic/delegated-apnic-extended-latest` (`rir.DefaultSourceURL`; sem `/pub`) |
| `SOURCE_MD5_URL` | vazio = `SOURCE_URL` + `.md5` |
| `MIN_RECORDS` | `95000` (`rir.DefaultMinRecords`) |
| `SYNC_INTERVAL`, `RETRY_INTERVAL`, `RUN_TIMEOUT`, `REMOVAL_THRESHOLD` | `1h`, `5m`, `10m`, `0.05` (os do padrão) |

- **`MIN_RECORDS` = 95000** porque o arquivo real tinha 190.268 registros em
  2026-09-28 (serial 20260929): o mínimo fica em cerca de metade, com ~50% de
  folga para a variação diária; abaixo disso o arquivo é tratado como
  truncado.
- **`REMOVAL_THRESHOLD` = 0.05** fica longe da variação medida da APNIC:
  0,01% dos blocos removidos de um dia para o outro e 0,07% em uma semana
  ([fonte.md](fonte.md#fatos-medidos-arquivo-de-2026-09-28-serial-20260929)).
- A APNIC publica um arquivo por dia (~15:15 UTC, e o `.md5` ~15:24 UTC,
  [fonte.md](fonte.md#publicação)): com `SYNC_INTERVAL` de 1h, o arquivo
  novo é aplicado pela primeira verificação depois que o `.md5` novo sai
  (até ~1h depois), e as outras verificações do dia param na checagem 1 (MD5
  publicado igual).

## Janela do `.md5`

Entre ~15:15 e ~15:24 UTC o `-latest` já é o arquivo novo, mas o `.md5`
ainda é o do dia anterior. Uma verificação nessa janela
([checagens](../rir/collector.md#checagens-de-mudança)):

| Situação | Resultado |
|---|---|
| Coletor em dia (o último aplicado é o arquivo do dia anterior) | o `.md5` velho é igual ao `md5` do último aplicado: a verificação para na checagem 1 (`md5 publicado igual ao último aplicado`), sem baixar nada e **sem gravar falha**; a primeira verificação depois de ~15:24 aplica o arquivo novo |
| O último aplicado não é o do dia anterior (primeira carga num banco vazio, coletor parado por mais de um dia, arquivo do dia anterior recusado) | baixa o arquivo novo e a conferência falha: linha `status = 0` com `md5 divergente: publicado <hash>, baixado <hash> (arquivo e .md5 publicados em momentos diferentes?)`. As novas tentativas vêm a cada `RETRY_INTERVAL` (5 min): com a janela de ~9 min, uma ou duas recusas seguidas antes de o `.md5` novo sair |
| `--force` | a mesma recusa: `--force` pula a checagem 1, mas confere o download; sai com 1 e pode ser repetido depois de ~15:24 UTC |

Os dois casos — o coletor em dia, que não grava falha, e a recusa — foram
reproduzidos com o binário e um servidor local em 2026-09-28. Com
`SOURCE_MD5_URL=off` não há janela, mas também não há a checagem 1 nem a
conferência do download.

## Medições

Arquivo real de 2026-09-28 (serial 20260929; 190.268 registros, 9,25 MB),
binário `--once` contra um Postgres 18 local, a partir do Brasil:

| Etapa | Tempo |
|---|---|
| Primeira verificação completa | ~9,1 s |
| Download | ~7 s |
| Parser | ~95–130 ms |
| Primeira carga | ~1,4 s |
| Reaplicação sem mudança | ~0,4 s |
| Arquivo do dia seguinte sobre o anterior | ~0,8 s |
| Verificação sem mudança pelo MD5 publicado | ~1,2 s (ida e volta TLS até a Austrália) |
| Verificação sem mudança pelo `304` | ~1,2 s |

## Compose e `.env`

- Variáveis: `COLLECTOR_APNIC_TAG=latest`,
  `COLLECTOR_APNIC_SYNC_INTERVAL=1h`, `COLLECTOR_APNIC_RETRY_INTERVAL=5m`,
  `COLLECTOR_APNIC_SOURCE_URL=`, `COLLECTOR_APNIC_SOURCE_MD5_URL=`,
  `COLLECTOR_APNIC_MIN_RECORDS=` e `COLLECTOR_APNIC_REMOVAL_THRESHOLD=0.05`.
- No `.env.example` da raiz, o bloco começa com
  `# collector-apnic: delegações da APNIC (delegated-extended). MIN_RECORDS vazio = 95000.`
- Imagem `tmsoftbrasil/badblock-collector-apnic`, label `description`
  `Importa as delegações de ASNs e blocos IP da APNIC (delegated-extended) para o PostgreSQL do BadBlock`;
  container `badblock-collector-apnic`.

## Testes

Arquivo real ([../rir/collector.md](../rir/collector.md#arquivo-real-make-test-real)):

```bash
curl -o /tmp/delegated https://ftp.apnic.net/stats/apnic/delegated-apnic-extended-latest
make -C apps/apnic/collector test-real FILE=/tmp/delegated   # APNIC_REAL_FILE
```

Os números do recorte ([fonte.md](fonte.md#recorte-testdatadelegated-extended-sampletxt))
que os testes citam, no lugar dos do modelo:

- `TestParseSample`: cabeçalho `2.3`, `apnic`, 21 registros, serial
  `20260929`, `startdate` vazia, `enddate` 2026-09-28, `UTCOffset` `+1000`;
  8 asn, 7 ipv4 e 6 ipv6 (`Lines` 21); 7 + 6 blocos; nenhum aviso. AS55759
  (NZ, allocated, `A913D7D6`, 2010-09-14); AS45122 + 3 = AS45124 (available,
  sem país, data nem titular); AS7619 (reserved, opaque-id vazio no fim da
  linha); AS143674 + 3072 = AS146745 (CN, allocated); `14.128.4.0/22`
  (`A913D7D6`, `record_value` 1024); `14.160.0.0/11` (VN, 2097152);
  `27.0.8.0/22` (reserved, vazios); `163.61.160.0/26` (available, 64);
  `2001:201::/32` (available, 32); `2001:7fa::/64` (reserved, 64);
  `2001:de3::/48` (assigned, sem país, `A919DB08`, 2025-07-03).
- `TestParseOtherRIRFormats`: `afrinic`, `lacnic`, `arin` e `ripencc`. Como
  o `lacnic.txt` tem `startdate` 19870101 e `enddate` 20260925 (os outros
  três, `enddate` 20260928), a tabela do teste tem a coluna `end` e confere a
  `startdate` pelo valor. `TestParseWrongRegistry`: `formats/arin.txt` lido
  com o registry `ripencc`, como no modelo.
- `collector_test.go`: recorte de serial 20260929 e `enddate` 20260928
  (`TestFirstRunApplies` confere 8/7/6 registros e 7 + 6 blocos);
  `MinRecords` 5 (e 22 em `TestTooFewRecordsIsRecorded`). Mais antigo que o
  aplicado quando este tem `enddate` 20260929 (serial 20260920); o mesmo
  `enddate` e serial 20260930; ou só o serial 20260930. Aplica quando o
  aplicado tem `enddate` 20260927 e serial 20260930; o mesmo cabeçalho;
  serial `1790600421096`; ou serial `x` (os três últimos com `enddate`
  20260928).
- `TestApplyLifecycle`: a carga insere 8 ASNs e 13 blocos (7 IPv4, 6 IPv6);
  `format_version` `2.3`, `utc_offset` `+1000`, `header_records` 21,
  `asn_records` 8, `prefixes_v4` 7 e `start_date` NULL; as consultas de
  [dados.md](dados.md#consultas-no-recorte). Passo das mudanças, com
  `REMOVAL_THRESHOLD` 0.2: AS7619 sai, AS17830 vira allocated (BR, titular
  `999`), AS64500 entra; `1.0.0.0/24` sai, `163.61.160.64/26` passa ao
  titular `999` e `62.122.208.0` + 1280 entra (2 blocos) → ASN 1/1/1 e
  blocos 2/1/1 (inseridos/alterados/apagados). Trava: um dataset com 2 ASNs
  e 2 blocos é recusado com 0.05 e, com `--force`, apaga 6 ASNs e 12 blocos.
- `TestApplyRealFile`: o `EXPLAIN` usa índice na consulta pelo ASN 4608,
  pelo IP `1.1.1.1` e pelo titular `A913D7D6`.

## `--help`

Saída real (versão `dev`, 2026-09-29):

```
collector-apnic — importa o arquivo delegated-extended do RIR APNIC (ASNs e blocos IP
delegados pelo RIR) para as tabelas apnic_* do PostgreSQL do BadBlock.

Verifica a fonte a cada SYNC_INTERVAL e só aplica quando o arquivo muda
(MD5 publicado, ETag/Last-Modified e hash do conteúdo). Não tem API HTTP.

Uso:
  collector-apnic [opções]

Opções (padrão → variável de ambiente → argumento):
  --log-format           json ou text
                         env LOG_FORMAT, padrão json
  --log-level            debug, info, warn ou error
                         env LOG_LEVEL, padrão info
  --min-records          abaixo disso (registros ASN + IPv4 + IPv6) o arquivo é tratado como truncado e recusado
                         env MIN_RECORDS, padrão 95000
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
  --source-url           arquivo delegated-extended do RIR APNIC
                         env SOURCE_URL, padrão https://ftp.apnic.net/stats/apnic/delegated-apnic-extended-latest
  --sync-interval        intervalo entre verificações da fonte
                         env SYNC_INTERVAL, padrão 1h
  --user-agent           User-Agent das requisições (vazio = badblock-collector-apnic/<versão>)
                         env USER_AGENT, padrão ""
  --once                 faz uma verificação e sai
  --force                aplica mesmo sem mudança ou com arquivo mais antigo e ignora
                         a trava de remoção (implica --once)
  --version              mostra a versão e sai
  -h, --help             mostra esta ajuda
```
