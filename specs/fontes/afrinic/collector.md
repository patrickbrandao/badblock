# Coletor da AFRINIC (`collector-afrinic`)

O `collector-afrinic` é um **clone** do `collector-lacnic`, o modelo dos
coletores de RIR: o comportamento está em
[../../padroes/coletor.md](../../padroes/coletor.md) e em
[../rir/collector.md](../rir/collector.md), sem nada de diferente. O código
comum é o do modelo com o nome trocado (conferido em 2026-09-29: fora os
testes, as fixtures e o `README.md`, só `internal/rir/rir.go` e o comentário
do pacote `internal/parse`, que cita os cinco RIRs, diferem), e o que é da
AFRINIC fica em `internal/rir/rir.go`
([../rir/README.md](../rir/README.md#organização-do-código)). Aqui, os
valores, as medições e os números da AFRINIC.

## Valores

| Opção | Valor na AFRINIC |
|---|---|
| `SOURCE_URL` | `https://ftp.afrinic.net/pub/stats/afrinic/delegated-afrinic-extended-latest` (`rir.DefaultSourceURL`) |
| `SOURCE_MD5_URL` | vazio = `SOURCE_URL` + `.md5` |
| `MIN_RECORDS` | `10000` (`rir.DefaultMinRecords`) |
| `SYNC_INTERVAL`, `RETRY_INTERVAL`, `RUN_TIMEOUT`, `REMOVAL_THRESHOLD` | `1h`, `5m`, `10m`, `0.05` (os do padrão) |

As outras constantes de `internal/rir/rir.go`: `Source` e `Registry` =
`afrinic`, `App` = `collector-afrinic`, `Title` = `AFRINIC`.

- **`MIN_RECORDS` = 10000** porque o arquivo real tinha 19.786 registros em
  2026-09-28 (o menor dos cinco RIRs): o mínimo fica em cerca de metade, com
  ~50% de folga para a variação diária (em setembro de 2026 o total foi de
  19.651 a 19.786); abaixo disso o arquivo é tratado como truncado.
- **`REMOVAL_THRESHOLD` = 0.05** fica longe da variação medida: nenhum ASN e
  0,21% dos blocos removidos entre 10/09 e 28/09/2026
  ([fonte.md](fonte.md#fatos-medidos-arquivo-de-2026-09-28-serial-20260928)).
- A AFRINIC publica um arquivo por dia (~00:07 UTC,
  [fonte.md](fonte.md#publicação)): com `SYNC_INTERVAL` de 1h, o arquivo novo
  é aplicado até ~1h depois de publicado, e as outras verificações do dia
  param na checagem 1 (MD5 publicado igual), que baixa só o `.md5` de 74
  bytes. Arquivo e `.md5` saem juntos, então a conferência não tem a janela
  da [APNIC](../apnic/collector.md#janela-do-md5).
- Num dia sem arquivo novo, o `-latest` não muda e toda verificação para na
  checagem 1. Num dia em que só o cabeçalho muda, o arquivo novo é aplicado
  com 0 alterações: versão nova do dataset, `consolidated` intacto.
- Com `serial` = `enddate`, a checagem 4 (arquivo mais antigo) decide pelo
  `enddate`; o serial só entra com o mesmo `enddate`, e aí é o mesmo serial.

## Servidor lento e certificado TLS

`ftp.afrinic.net` responde devagar ([fonte.md](fonte.md#servidor)): ~1,3 s
até o primeiro byte, mesmo para o `.md5`, e ~120 KB/s no download. Os limites
do [padrão](../../padroes/coletor.md#download-internalfetch) sobram —
conexão e TLS 15 s, cabeçalhos 60 s, `RUN_TIMEOUT` 10m, 2 novas tentativas
com 10 s entre elas — e não foi preciso mudar nada: em 2026-09-28/29, todas
as requisições deram certo.

O certificado de `ftp.afrinic.net` vale **até 2026-10-23** (conferido em
2026-09-28/29). Se não for renovado a tempo, o erro de TLS volta do
cliente HTTP como qualquer erro de rede: o `.md5` e o arquivo são tentados 3
vezes cada (10 s entre as tentativas), a verificação falha com
`download: ...` — só no log, sem linha em `afrinic_run` —, a próxima vem em
`RETRY_INTERVAL`, e `jobs.last_check_at` para de avançar; as tabelas ficam
com o último arquivo aplicado. Não há opção para aceitar um certificado
inválido. Para conferir a validade:

```bash
openssl s_client -connect ftp.afrinic.net:443 -servername ftp.afrinic.net </dev/null 2>/dev/null | openssl x509 -noout -enddate
```

## Medições

Arquivo real de 2026-09-28 (19.786 registros, 15.530 blocos, ~1 MB),
Postgres 18 local, medido em 2026-09-28:

| Etapa | Tempo |
|---|---|
| Verificação completa com arquivo novo | ~10,4 s, quase tudo download (~1 MB a ~120 KB/s) |
| Parser | 10–40 ms |
| Primeira carga (4.350 ASNs e 15.530 blocos) | 0,16–0,56 s |
| Reaplicação sem mudança | 0,06–0,2 s |
| Verificação sem mudança pelo MD5 publicado | ~1,3–1,5 s (o tempo de resposta do servidor, mesmo para 74 bytes) |
| Verificação sem mudança pelo `304` (com `SOURCE_MD5_URL=off`) | ~3 s |

Os tempos locais variam com a carga da máquina. Reconferido em 2026-09-29
com o mesmo arquivo (`make test-real`): parser 14–22 ms, primeira carga
0,15 s, reaplicação 0,06 s.

## Compose e `.env`

- Variáveis: `COLLECTOR_AFRINIC_TAG=latest`,
  `COLLECTOR_AFRINIC_SYNC_INTERVAL=1h`, `COLLECTOR_AFRINIC_RETRY_INTERVAL=5m`,
  `COLLECTOR_AFRINIC_SOURCE_URL=`, `COLLECTOR_AFRINIC_SOURCE_MD5_URL=`,
  `COLLECTOR_AFRINIC_MIN_RECORDS=` e `COLLECTOR_AFRINIC_REMOVAL_THRESHOLD=0.05`.
- No `.env.example` da raiz, o bloco começa com
  `# collector-afrinic: delegações da AFRINIC (delegated-extended). MIN_RECORDS vazio = 10000.`
- Imagem `tmsoftbrasil/badblock-collector-afrinic`, label `description`
  `Importa as delegações de ASNs e blocos IP da AFRINIC (delegated-extended) para o PostgreSQL do BadBlock`;
  container `badblock-collector-afrinic`.

## Testes

Arquivo real ([../rir/collector.md](../rir/collector.md#arquivo-real-make-test-real)):

```bash
curl -o /tmp/delegated https://ftp.afrinic.net/pub/stats/afrinic/delegated-afrinic-extended-latest
make -C apps/afrinic/collector test-real FILE=/tmp/delegated   # AFRINIC_REAL_FILE
```

O `curl` leva ~10 s ([fonte.md](fonte.md#servidor)). No arquivo de
2026-09-28 (reconferido em 2026-09-29), o `EXPLAIN` de `TestApplyRealFile`
usou `uq_afrinic_asn_asn_start` na consulta por ASN (`asn_start <= 37000`),
`ix_afrinic_prefix_prefix_gist` na por IP (`196.4.21.3`) e
`ix_afrinic_prefix_opaque_id` na por titular (`F3619C8C`, o titular com
mais registros, 187).

Os números do recorte ([fonte.md](fonte.md#recorte-testdatadelegated-extended-sampletxt))
que os testes citam, no lugar dos do modelo:

- `TestParseSample`: o cabeçalho inteiro — `2`, `afrinic`, serial
  `20260928`, 15 registros, `startdate` sem data (`00000000`), `enddate`
  2026-09-28, `UTCOffset` `00000` (o modelo confere só versão, registry e
  `records`); 5 asn, 6 ipv4 e 4 ipv6; **9** + 4 blocos; nenhum aviso nem
  descarte. AS329814 (NE, allocated, `F3626E7D`, 2026-09-18); AS6187 (a data
  mais antiga do arquivo, 1984-01-01, `F36771FA`); AS8770 (available, `ZZ`,
  sem data nem titular, `End()` 8770); AS10803 (reserved, `ZZ`, sem
  titular); os 9 blocos IPv4, na ordem, `160.115.0.0/16`, `164.146.0.0/15`,
  `164.148.0.0/14`, `196.4.20.0/22`, `196.4.24.0/22`, `196.4.28.0/23`,
  `102.201.36.0/22`, `41.57.112.0/21` e `102.201.0.0/20`; `196.4.28.0/23`
  (pedaço de registro não-CIDR: `record_start` `196.4.20.0`,
  `record_value` 2560, `F369838C`); `102.201.36.0/22` (assigned,
  `F3626E7D`, 1024); `41.57.112.0/21` (reserved, `ZZ`, vazios);
  `2001:4208::/29` (available, 29, `ZZ`); `2001:43fe:3800::/48` (assigned,
  `F3626E7D`).
- `TestParseOtherRIRFormats`: `lacnic`, `apnic`, `arin` e `ripencc`, cada
  caso com o seu `enddate`, conferido pelo valor (o do `lacnic.txt` é
  20260925; os outros, 20260928), e a `startdate` só pela presença. No
  `lacnic`: 3 asn (com o AS6065 a mais), 2 ipv4 e 2 ipv6; AS28003 + 3
  (available, sem país nem titular), o titular numérico `258500` no AS61610
  e em dois blocos, `2001:1201:20::/43` (available de 7 campos) e
  `UTCOffset` `-0300`. `TestParseWrongRegistry`: `formats/arin.txt` lido com
  o registry `ripencc`, como no modelo.
- `collector_test.go`: recorte de serial 20260928 e `enddate` 20260928
  (`TestFirstRunApplies` confere 5/6/4 registros e 9 + 4 blocos);
  `MinRecords` 5 (e 16 em `TestTooFewRecordsIsRecorded`). Mais antigo que o
  aplicado quando este tem `enddate` 20260929 (serial 20260920); o mesmo
  `enddate` e serial 20260929; ou só o serial 20260929. Aplica quando o
  aplicado tem `enddate` 20260927 e serial 20260929; o mesmo cabeçalho;
  serial `1790600421096`; ou serial `x` (os três últimos com `enddate`
  20260928).
- `TestApplyLifecycle`: a carga insere 5 ASNs e 13 blocos (9 IPv4, 4 IPv6);
  `format_version` `2`, `utc_offset` `00000`, `start_date` NULL,
  `header_records` 15, `asn_records` 5, `prefixes_v4` 9; `LastApplied` com
  serial `20260928` e `enddate` 2026-09-28; as consultas de
  [dados.md](dados.md#consultas-no-recorte), inclusive (a mais que o
  modelo) a do AS10000, que não está no arquivo, e os reservados com `cc`
  `ZZ` (no modelo, `cc` NULL). Passo das mudanças, com `REMOVAL_THRESHOLD`
  0.2: AS8770 sai, AS10803 vira allocated (ZA, titular `F3699999`), AS329999
  entra (ZA, allocated, `F3699999`); `160.115.0.0/16` sai,
  `102.201.36.0/22` passa ao titular `F3699999` e `196.1.87.0` + 1280 entra
  (ZA, assigned, `F369C3AE`, um registro real da AFRINIC:
  `196.1.87.0/24` + `196.1.88.0/22`) → ASN 1/1/1 e blocos 2/1/1
  (inseridos/alterados/apagados). Trava: um dataset com 2 ASNs e 2 blocos é
  recusado com 0.05 e, com `--force`, apaga 3 ASNs e 12 blocos.

## `--help`

Saída real (versão `dev`, 2026-09-29):

```
collector-afrinic — importa o arquivo delegated-extended do RIR AFRINIC (ASNs e blocos IP
delegados pelo RIR) para as tabelas afrinic_* do PostgreSQL do BadBlock.

Verifica a fonte a cada SYNC_INTERVAL e só aplica quando o arquivo muda
(MD5 publicado, ETag/Last-Modified e hash do conteúdo). Não tem API HTTP.

Uso:
  collector-afrinic [opções]

Opções (padrão → variável de ambiente → argumento):
  --log-format           json ou text
                         env LOG_FORMAT, padrão json
  --log-level            debug, info, warn ou error
                         env LOG_LEVEL, padrão info
  --min-records          abaixo disso (registros ASN + IPv4 + IPv6) o arquivo é tratado como truncado e recusado
                         env MIN_RECORDS, padrão 10000
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
  --source-url           arquivo delegated-extended do RIR AFRINIC
                         env SOURCE_URL, padrão https://ftp.afrinic.net/pub/stats/afrinic/delegated-afrinic-extended-latest
  --sync-interval        intervalo entre verificações da fonte
                         env SYNC_INTERVAL, padrão 1h
  --user-agent           User-Agent das requisições (vazio = badblock-collector-afrinic/<versão>)
                         env USER_AGENT, padrão ""
  --once                 faz uma verificação e sai
  --force                aplica mesmo sem mudança ou com arquivo mais antigo e ignora
                         a trava de remoção (implica --once)
  --version              mostra a versão e sai
  -h, --help             mostra esta ajuda
```

`--version` imprime `collector-afrinic dev (commit unknown, build unknown)`
num build sem `-ldflags`. Em relação ao modelo
([../lacnic/collector.md](../lacnic/collector.md#--help)), mudam
`collector-afrinic`, `RIR AFRINIC`, `afrinic_*`, o padrão de
`--min-records` (10000) e o de `--source-url`.
