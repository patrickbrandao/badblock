# Coletor da ARIN (`collector-arin`)

O `collector-arin` é um clone do `collector-lacnic`, o modelo dos coletores
de RIR: o comportamento está em
[../../padroes/coletor.md](../../padroes/coletor.md) e em
[../rir/collector.md](../rir/collector.md), sem nada de diferente. O código
comum é o do modelo com o nome trocado (conferido em 2026-09-29: fora os
testes e as fixtures, só `internal/rir/rir.go` e o comentário do pacote
`internal/parse`, que cita os cinco RIRs, diferem). Aqui, os valores, as
medições e os números da ARIN.

## Valores

| Opção | Valor na ARIN |
|---|---|
| `SOURCE_URL` | `https://ftp.arin.net/pub/stats/arin/delegated-arin-extended-latest` (`rir.DefaultSourceURL`) |
| `SOURCE_MD5_URL` | vazio = `SOURCE_URL` + `.md5` (formato GNU, [fonte.md](fonte.md#publicação)) |
| `MIN_RECORDS` | `100000` (`rir.DefaultMinRecords`) |
| `SYNC_INTERVAL`, `RETRY_INTERVAL`, `RUN_TIMEOUT`, `REMOVAL_THRESHOLD` | `1h`, `5m`, `10m`, `0.05` (os do padrão) |

As outras constantes de `internal/rir/rir.go`: `Source` e `Registry` =
`arin`, `App` = `collector-arin`, `Title` = `ARIN`.

- **`MIN_RECORDS` = 100000** porque o arquivo real tinha 203.051 registros em
  2026-09-28: o mínimo fica em cerca de metade, com ~50% de folga para a
  variação diária (em 2026 o total ficou entre ~200 mil e ~204,5 mil); abaixo
  disso o arquivo é tratado como truncado.
- A ARIN publica um arquivo por dia (~09:00 de Nova York,
  [fonte.md](fonte.md#publicação)): com `SYNC_INTERVAL` de 1h, o arquivo novo
  é aplicado até ~1h depois de publicado, e as outras verificações do dia
  param na checagem 1 (MD5 publicado igual), que baixa só o `.md5` de 67
  bytes. Num dia sem mudança nos registros (só o cabeçalho muda), o arquivo
  novo é aplicado com 0 alterações: versão nova do dataset, `consolidated`
  intacto.
- **Vários servidores** ([fonte.md](fonte.md#publicação)): a checagem 2
  (`304`) só acontece quando a verificação cai no servidor com a mesma cópia
  do último download aplicado. Como a checagem 1 vem antes e o `.md5` é igual
  em todos, isso só custa um download a mais quando o `.md5` está fora do ar
  (a checagem 3, SHA-256, resolve). Arquivo e `.md5` de cópias diferentes, na
  janela de sincronização: recusa `md5 divergente: ...` (linha `status = 0`),
  resolvida na próxima verificação.
- **`serial` em ms**: a checagem 4 compara o `enddate` e, com o mesmo
  `enddate`, o `serial` de 13 dígitos (ex.: `reason`
  `arquivo mais antigo que o aplicado (serial 1790600421096 < 1790600421097)`).
  Um serial de outro tamanho (em segundos, ou uma data) não é comparável, e o
  arquivo segue.

## Medições

Arquivo real de 2026-09-28 (203.051 registros, 12,8 MB), Postgres 18 local:

| Etapa | Tempo |
|---|---|
| Parser | ~140 ms |
| Primeira carga (32.988 ASNs e 173.950 blocos) | ~1,6 s |
| Reaplicação sem mudança | ~0,5 s |
| Verificação completa, com download pela rede, na primeira carga | ~10 s |
| Verificação completa, com download pela rede, num `--force` sem mudança | ~6,6 s |
| Verificação sem mudança pelo MD5 publicado | 0,5–1,8 s (TLS com `ftp.arin.net`) |

A troca do arquivo de 2026-09-27 pelo de 2026-09-28 mudou 15 blocos (7
novos, 6 alterados, 2 removidos) e nenhum ASN — muito abaixo da trava de
remoção (`0.05`).

## Compose e `.env`

- Variáveis: `COLLECTOR_ARIN_TAG=latest`, `COLLECTOR_ARIN_SYNC_INTERVAL=1h`,
  `COLLECTOR_ARIN_RETRY_INTERVAL=5m`, `COLLECTOR_ARIN_SOURCE_URL=`,
  `COLLECTOR_ARIN_SOURCE_MD5_URL=`, `COLLECTOR_ARIN_MIN_RECORDS=` e
  `COLLECTOR_ARIN_REMOVAL_THRESHOLD=0.05`.
- No `.env.example` da raiz, o bloco começa com
  `# collector-arin: delegações da ARIN (delegated-extended). MIN_RECORDS vazio = 100000.`
- Imagem `tmsoftbrasil/badblock-collector-arin`, label `description`
  `Importa as delegações de ASNs e blocos IP da ARIN (delegated-extended) para o PostgreSQL do BadBlock`;
  container `badblock-collector-arin`.

## Testes

Arquivo real ([../rir/collector.md](../rir/collector.md#arquivo-real-make-test-real)):

```bash
curl -o /tmp/delegated https://ftp.arin.net/pub/stats/arin/delegated-arin-extended-latest
make -C apps/arin/collector test-real FILE=/tmp/delegated   # ARIN_REAL_FILE
```

`TestApplyRealFile` confere o `EXPLAIN` das consultas por ASN
(`asn_start <= 11472`), por IP (`208.65.33.10`) e por titular
(`e0082a77a634f2cc5817ffb7de12b38e`).

Os números do recorte ([fonte.md](fonte.md#recorte-testdatadelegated-extended-sampletxt))
que os testes citam, e os casos que o clone tem a mais que o modelo:

- `TestParseSample`: o cabeçalho inteiro — `2.3`, `arin`, 21 registros,
  serial `1790600421096`, `startdate` 19700101, `enddate` 20260928,
  `UTCOffset` `-0400` (o modelo confere só versão, registry e `records`); 7
  asn, 8 ipv4 e 6 ipv6; **10** + 6 blocos (os dois reservados não-CIDR viram
  2 blocos cada); nenhum aviso nem descarte. AS11472 (US, assigned,
  `e0082a77…`, 2008-12-18); AS5120 + 257 = AS5376 (US, assigned);
  AS403010 + 1371 = AS404380 (available, sem país, data nem titular); AS3
  (US, assigned, data `00000000` → sem data,
  `d98c567cda2db06e693f2b574eafe848`); AS212
  (reserved, opaque-id vazio no fim da linha); `208.65.32.0/22`
  (`e0082a77…`, `record_value` 1024, allocated); `6.0.0.0/8` (US, 16777216);
  `23.128.16.0/24` (PR, allocated); `23.128.81.0/24` (reserved, vazios); os
  pedaços `23.128.1.0/24` + `23.128.2.0/23` (`record_start` `23.128.1.0`) e
  `64.112.29.0/24` + `64.112.30.0/24` (`record_start` `64.112.29.0`),
  reserved; `2001:488::/29` (available, 29, sem titular); `260a::/15`
  (reserved); `2620:de:6000::/48` (allocated, 48,
  `083b0cd1b1e8e914c5745ade6bd0a7c4`); `2605:8480::/32` (US, `e0082a77…`).
- `TestParseOtherRIRFormats`: `afrinic`, `apnic`, `lacnic` e `ripencc`, cada
  caso com o seu `enddate` (o do `lacnic.txt` é 20260925; o modelo fixa
  20260928 para todos). No `lacnic`: AS26596 + 2 (available, 7 campos, sem
  país nem titular), AS61610 (BR, allocated, `258500`), `45.68.105.0/24`
  (reserved, opaque-id vazio) e `UTCOffset` `-0300`. `TestParseWrongRegistry`:
  `formats/lacnic.txt` lido com o registry `arin` → erro com
  `registry "lacnic"`.
- `TestSplitIPv4`: os casos do modelo e mais `64.112.29.0` + 512 →
  `64.112.29.0/24` + `64.112.30.0/24`.
- `collector_test.go`: recorte de serial `1790600421096` e `enddate`
  20260928; `MinRecords` 5 (e 22 em `TestTooFewRecordsIsRecorded`);
  `TestFirstRunApplies` com 7/8/6 registros e 10 + 6 blocos. Mais antigo que
  o aplicado (`TestOlderFileIsIgnored`, 3 casos; com `--force`, aplica)
  quando este tem `enddate` 20260929 (com serial `1790514021692`, menor); o
  mesmo `enddate` e serial `1790600421097`; ou só o serial `1790686821000`.
  Aplica (`TestNewerOrIncomparableFileApplies`, **5 casos**, um a mais que o
  modelo) quando o aplicado tem `enddate` 20260927 e serial `1790686821000`
  (maior); o mesmo cabeçalho; serial `20260929` (uma data); serial
  `1790600422` (em segundos); ou serial `x`.
- `TestApplyLifecycle`: a carga insere 7 ASNs e 16 blocos (10 IPv4, 6 IPv6);
  `format_version` `2.3`, `utc_offset` `-0400`, `header_records` 21,
  `prefixes_v4` 10; `LastApplied` com serial `1790600421096` e `enddate`
  2026-09-28; as consultas de [dados.md](dados.md#consultas-no-recorte),
  inclusive (a mais que o modelo) os 2 blocos do registro `23.128.1.0` + 768.
  Passo das mudanças, com `REMOVAL_THRESHOLD` 0.2: AS212 sai, AS403010 vira
  assigned (US, titular `999`), AS64500 entra (US, assigned, `999`);
  `6.0.0.0/8` sai, `23.128.16.0/24` passa ao titular `999` e
  `23.128.5.0` + 1792 entra (reserved, sem titular: `/24` + `/23` + `/22`) →
  ASN 1/1/1 e blocos 3/1/1 (inseridos/alterados/apagados). Trava: um dataset
  com 2 ASNs e 2 blocos é recusado com 0.05 e, com `--force`, apaga 5 ASNs e
  16 blocos.

## `--help`

Saída real (versão `dev`, 2026-09-29):

```
collector-arin — importa o arquivo delegated-extended do RIR ARIN (ASNs e blocos IP
delegados pelo RIR) para as tabelas arin_* do PostgreSQL do BadBlock.

Verifica a fonte a cada SYNC_INTERVAL e só aplica quando o arquivo muda
(MD5 publicado, ETag/Last-Modified e hash do conteúdo). Não tem API HTTP.

Uso:
  collector-arin [opções]

Opções (padrão → variável de ambiente → argumento):
  --log-format           json ou text
                         env LOG_FORMAT, padrão json
  --log-level            debug, info, warn ou error
                         env LOG_LEVEL, padrão info
  --min-records          abaixo disso (registros ASN + IPv4 + IPv6) o arquivo é tratado como truncado e recusado
                         env MIN_RECORDS, padrão 100000
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
  --source-url           arquivo delegated-extended do RIR ARIN
                         env SOURCE_URL, padrão https://ftp.arin.net/pub/stats/arin/delegated-arin-extended-latest
  --sync-interval        intervalo entre verificações da fonte
                         env SYNC_INTERVAL, padrão 1h
  --user-agent           User-Agent das requisições (vazio = badblock-collector-arin/<versão>)
                         env USER_AGENT, padrão ""
  --once                 faz uma verificação e sai
  --force                aplica mesmo sem mudança ou com arquivo mais antigo e ignora
                         a trava de remoção (implica --once)
  --version              mostra a versão e sai
  -h, --help             mostra esta ajuda
```

`--version` imprime `collector-arin dev (commit unknown, build unknown)` num
build sem `-ldflags`. Em relação ao modelo
([../lacnic/collector.md](../lacnic/collector.md#--help)), mudam
`collector-arin`, `RIR ARIN`, `arin_*`, o padrão de `--min-records` (100000)
e o de `--source-url`.
