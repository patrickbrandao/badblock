# Coletor do RIPE NCC (`collector-ripencc`)

O `collector-ripencc` é um clone do `collector-lacnic`, o modelo dos
coletores de RIR: o comportamento está em
[../../padroes/coletor.md](../../padroes/coletor.md) e em
[../rir/collector.md](../rir/collector.md), sem nada de diferente — o código
comum é o do modelo com o nome trocado (conferido em 2026-09-29), e o que é
do RIPE NCC está em `internal/rir/rir.go`. Aqui, os valores, as medições e
os números do RIPE NCC.

## Valores

| Opção | Valor no RIPE NCC |
|---|---|
| `SOURCE_URL` | `https://ftp.ripe.net/pub/stats/ripencc/delegated-ripencc-extended-latest` (`rir.DefaultSourceURL`) |
| `SOURCE_MD5_URL` | vazio = `SOURCE_URL` + `.md5` |
| `MIN_RECORDS` | `130000` (`rir.DefaultMinRecords`) |
| `SYNC_INTERVAL`, `RETRY_INTERVAL`, `RUN_TIMEOUT`, `REMOVAL_THRESHOLD` | `1h`, `5m`, `10m`, `0.05` (os do padrão) |

- `rir.Source` e `rir.Registry` são `ripencc`; `rir.Title` é `RIPE NCC`
  (nos textos, "do RIPE NCC"). O host da URL é `ftp.ripe.net`: o `sed` da
  clonagem produz `ftp.ripencc.net`, que não é o do RIPE NCC, e a URL é
  conferida à mão ([../rir/collector.md](../rir/collector.md#clonar-o-coletor-para-outro-rir)).
- **`MIN_RECORDS` = 130000** porque o arquivo real tinha 260.793 registros em
  2026-09-28: o mínimo fica em cerca de metade (49,8%), com ~50% de folga
  para a variação diária; abaixo disso o arquivo é tratado como truncado.
- O RIPE NCC publica um arquivo por dia (~22:12 UTC,
  [fonte.md](fonte.md#publicação)): com `SYNC_INTERVAL` de 1h, o arquivo novo
  é aplicado até ~1h depois de publicado, e as outras verificações do dia
  param na checagem 1 (MD5 publicado igual).
- `REMOVAL_THRESHOLD` `0.05` (5%) fica muito acima da remoção real, 0,012%
  dos blocos num dia e 0,03% numa semana
  ([fonte.md](fonte.md#fatos-medidos-arquivo-de-2026-09-28-serial-1790632799)).
- O `serial` é a época Unix em segundos, sempre com 10 dígitos (de 2001 a
  2286): a comparação de seriais do
  [arquivo mais antigo](../rir/collector.md#arquivo-mais-antigo-que-o-aplicado)
  vale entre quaisquer dois arquivos do RIPE NCC.

## Atualização diária

Como o opaque-id é novo a cada arquivo
([fonte.md](fonte.md#opaque-id-novo-a-cada-arquivo)), todo arquivo diário
atualiza quase todo registro allocated/assigned. Isso é **esperado, não é
defeito**, e o coletor não tem tratamento especial para isso:

- a linha `arquivo novo aplicado` traz ~40 mil `asn_updated` e ~129 mil
  `prefix_updated`. De 2026-09-27 para 2026-09-28, comparando os dois
  arquivos pelas regras do `MERGE`: `asn_inserted` 1, `asn_updated` 39.939,
  `asn_deleted` 0, `prefix_inserted` 47, `prefix_updated` 128.866,
  `prefix_deleted` 26 (dos alterados, 39.911 e 128.847 só pelo opaque-id);
- `jobs.consolidated` volta a `0` a cada arquivo novo: toda atualização
  diária pede consolidação;
- a trava de remoção não é afetada (é atualização, não remoção);
- reaplicar o mesmo arquivo (`--force`) não altera nenhuma linha: os UUIDs
  são os mesmos dentro de um arquivo.

## Medições

Arquivo real de 2026-09-28 (18.079.469 bytes, 260.793 registros → 48.682
registros de ASN e 213.656 blocos), Postgres 18 local:

| Etapa | Tempo |
|---|---|
| Download (a partir do Brasil) | 7–10 s (~2,4 MB/s) |
| Parser | ~0,35 s |
| Primeira carga | ~4,7 s |
| Reaplicação sem mudança (o mesmo arquivo com `--force`) | ~1,6 s |
| Atualização diária real (2026-09-27 → 2026-09-28: 1 ASN e 47 blocos novos, 26 blocos removidos, ~169 mil linhas com opaque-id novo) | ~10 s de aplicação, ~20 s a verificação inteira |
| Verificação sem mudança pelo MD5 publicado | ~0,65 s |

- Pico de memória numa aplicação: ~230 MB de RSS (o arquivo e o dataset
  ficam em memória).
- Folgas: `RUN_TIMEOUT` (`10m`) é ~30× a verificação completa (~20 s), e o
  limite do download (64 MiB = 67.108.864 bytes) é ~3,7× o arquivo.

## Compose e `.env`

- Variáveis: `COLLECTOR_RIPENCC_TAG=latest`,
  `COLLECTOR_RIPENCC_SYNC_INTERVAL=1h`, `COLLECTOR_RIPENCC_RETRY_INTERVAL=5m`,
  `COLLECTOR_RIPENCC_SOURCE_URL=`, `COLLECTOR_RIPENCC_SOURCE_MD5_URL=`,
  `COLLECTOR_RIPENCC_MIN_RECORDS=` e `COLLECTOR_RIPENCC_REMOVAL_THRESHOLD=0.05`.
- No `.env.example` da raiz, o bloco começa com
  `# collector-ripencc: delegações do RIPE NCC (delegated-extended). MIN_RECORDS vazio = 130000.`
- Imagem `tmsoftbrasil/badblock-collector-ripencc`, label `description`
  `Importa as delegações de ASNs e blocos IP do RIPE NCC (delegated-extended) para o PostgreSQL do BadBlock`;
  container `badblock-collector-ripencc`. O cabeçalho do `docker-compose.yml`
  do app também diz "do RIPE NCC".

## Testes

Arquivo real ([../rir/collector.md](../rir/collector.md#arquivo-real-make-test-real)):

```bash
curl -o /tmp/delegated https://ftp.ripe.net/pub/stats/ripencc/delegated-ripencc-extended-latest
make -C apps/ripencc/collector test-real FILE=/tmp/delegated   # RIPENCC_REAL_FILE
```

No arquivo de 2026-09-28, o `EXPLAIN` de `TestApplyRealFile` usou
`uq_ripencc_asn_asn_start` na consulta por ASN (9070),
`ix_ripencc_prefix_prefix_gist` na por IP (`193.0.6.139`) e
`ix_ripencc_prefix_opaque_id` na por titular
(`162f9494-4727-4143-89e3-1a5a64454c0d`).

Os números do recorte ([fonte.md](fonte.md#recorte-testdatadelegated-extended-sampletxt))
que os testes citam — os que o clone trocou pelos do modelo:

- `TestParseSample`: cabeçalho `2`, `ripencc`, serial `1790632799`, 18
  registros, `startdate` 1970-01-01, `enddate` 2026-09-28, `UTCOffset`
  `+0200`; 6 asn, 7 ipv4 e 5 ipv6; 11 + 5 blocos; nenhum aviso. AS9070 (BG,
  allocated, titular `162f9494-…`, 1998-11-25); AS7 (EU); AS6204 (data
  1970-01-01, gravada); AS400158 (NL, `End()` 400158); AS1877 (available,
  sem país, data nem titular); AS5575 (reserved, sem titular);
  `87.116.83.0/24`, `87.116.84.0/22` e `87.116.88.0/22` (BG, titular
  `162f9494-…`, `record_start` `87.116.83.0`, `record_value` 2304);
  `185.0.40.64/26`, `185.0.40.128/25` e `185.0.41.0/24` (reserved, vazios,
  448); `25.0.0.0/8` (GB, assigned, 16777216); `156.67.6.0/29` (8,
  `f7b5eb5d-2252-4fcc-97fa-2efcd27631a5`); `139.191.0.0/16` (EU);
  `194.147.40.0/22` (available); `2a18::/13` (available, 13);
  `2001:7f8:6::/48` (assigned, titular `162f9494-…`); `2001:609::/32`
  (reserved).
- `TestParseOtherRIRFormats`: `formats/` com AFRINIC, APNIC, ARIN e LACNIC;
  o caso da LACNIC confere AS28003 + 3 (available de 7 campos), AS61610
  (`258500`), `45.68.105.0/24` (reserved com o opaque-id vazio no fim) e
  `UTCOffset` `-0300`. Cada caso tem o seu `enddate` (o da LACNIC é
  20260925; os outros, 20260928). `TestParseWrongRegistry` usa
  `formats/arin.txt`.
- `collector_test.go`: recorte de serial 1790632799 e `enddate` 20260928;
  `MinRecords` 5 (e 19 em `TestTooFewRecordsIsRecorded`). Mais antigo que o
  aplicado quando este tem `enddate` 20260929 (serial 1790546399, o do
  arquivo de 2026-09-27); o mesmo `enddate` e serial 1790632800; ou só o
  serial 1790719199. Aplica quando o aplicado tem `enddate` 20260927 e serial
  1790719199; o mesmo cabeçalho; serial `20260929` (outro formato); ou
  serial `x`.
- `TestApplyLifecycle`: a carga insere 6 ASNs e 16 blocos (11 IPv4, 5 IPv6);
  `format_version` `2`, `utc_offset` `+0200`, `header_records` 18; as
  consultas de [dados.md](dados.md#consultas-no-recorte). Passo das
  mudanças, com `REMOVAL_THRESHOLD` 0.2: AS1877 sai, AS5575 vira allocated
  (NL, titular `999`), AS64500 + 4 entra (com `asn_end` 64503 calculado pelo
  banco); `1.178.112.0/20` sai, `156.67.6.0/29` passa ao titular `999` e
  `62.122.208.0` + 1280 entra (2 blocos) → ASN 1/1/1 e blocos 2/1/1
  (inseridos/alterados/apagados). Trava: um dataset com 2 ASNs e 2 blocos é
  recusado com 0.05 e, com `--force`, apaga 4 ASNs e 15 blocos.

Além dos números, o clone adapta os testes ao arquivo do RIPE NCC:
`TestParseSample` confere o cabeçalho inteiro (serial, datas e `UTCOffset`);
`TestApplyLifecycle` consulta AS9070 e AS9071 (não há faixa de ASN para
consultar um ASN do meio), confere `record_start`/`record_value` na consulta
por IP e usa uma faixa de ASN nova no passo das mudanças.

## `--help`

Saída real (versão `dev`, 2026-09-29):

```
collector-ripencc — importa o arquivo delegated-extended do RIR RIPE NCC (ASNs e blocos IP
delegados pelo RIR) para as tabelas ripencc_* do PostgreSQL do BadBlock.

Verifica a fonte a cada SYNC_INTERVAL e só aplica quando o arquivo muda
(MD5 publicado, ETag/Last-Modified e hash do conteúdo). Não tem API HTTP.

Uso:
  collector-ripencc [opções]

Opções (padrão → variável de ambiente → argumento):
  --log-format           json ou text
                         env LOG_FORMAT, padrão json
  --log-level            debug, info, warn ou error
                         env LOG_LEVEL, padrão info
  --min-records          abaixo disso (registros ASN + IPv4 + IPv6) o arquivo é tratado como truncado e recusado
                         env MIN_RECORDS, padrão 130000
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
  --source-url           arquivo delegated-extended do RIR RIPE NCC
                         env SOURCE_URL, padrão https://ftp.ripe.net/pub/stats/ripencc/delegated-ripencc-extended-latest
  --sync-interval        intervalo entre verificações da fonte
                         env SYNC_INTERVAL, padrão 1h
  --user-agent           User-Agent das requisições (vazio = badblock-collector-ripencc/<versão>)
                         env USER_AGENT, padrão ""
  --once                 faz uma verificação e sai
  --force                aplica mesmo sem mudança ou com arquivo mais antigo e ignora
                         a trava de remoção (implica --once)
  --version              mostra a versão e sai
  -h, --help             mostra esta ajuda
```

Em relação ao [modelo](../lacnic/collector.md#--help), mudam só
`collector-ripencc`, `RIR RIPE NCC`, `ripencc_*` e os padrões de
`--min-records` e de `--source-url`.
