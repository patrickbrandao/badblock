# Coletor `collector-cgibr`

O que o `collector-cgibr` faz de diferente ou a mais que o
[padrão dos coletores](../../padroes/coletor.md); o resto (`main.go`, laço,
`--once`/`--force`, contrato com `jobs`, container) segue o padrão. Formato do
arquivo e parser: [fonte.md](fonte.md). Tabelas: [dados.md](dados.md). Dono:
sub-agente `collector-cgibr`.

## Valores desta fonte

| Item | Valor |
|---|---|
| App e `jobs.app` | `collector-cgibr` (demais nomes: [../../projeto/estrutura.md](../../projeto/estrutura.md#nomes)) |
| Tabelas escritas | `cgibr_asn`, `cgibr_prefix`, `cgibr_run` e a linha `collector-cgibr` de `jobs` |
| Tabelas temporárias | `stage_asn`, `stage_prefix` (`ON COMMIT DROP`) |
| Pacotes | só os do padrão (`buildinfo`, `config`, `fetch`, `parse`, `store`, `collector`) |
| Conexão | pool pgx de no máximo 2 conexões, ociosas fechadas em 5 min; `application_name=collector-cgibr` quando a `POSTGRES_URL` não define outro |
| Descrição da imagem | `Importa os ASNs e blocos IP brasileiros do NIC.br (registro.br) para o PostgreSQL do BadBlock` (`org.opencontainers.image.description`) |

## Checagens de mudança

Três, da mais barata para a mais cara. A primeira que disser "igual" encerra
a verificação como sem mudança, com o `reason` no log `fonte sem mudança`:

| # | Checagem | Custo | `reason` |
|---|---|---|---|
| 1 | SHA-256 publicado (`SOURCE_SHA256_URL`) igual ao `sha256` da última execução aplicada | ~100 bytes | `sha256 publicado igual ao último aplicado` |
| 2 | GET condicional do arquivo com o `etag` e o `last_modified` da última execução aplicada → `304` | só cabeçalhos | `HTTP 304` |
| 3 | SHA-256 do arquivo baixado igual ao `sha256` da última execução aplicada | o download (~844 KB) | `conteúdo igual ao último aplicado` |

- Sem execução aplicada (banco vazio), nenhuma checagem diz "igual": a
  primeira verificação sempre baixa e aplica.
- Não há checagem de "arquivo mais antigo que o aplicado": o arquivo não traz
  serial nem data, e o `Last-Modified` só é reenviado, nunca comparado.
- O `.sha256` é baixado mesmo com `--force` (a conferência continua valendo);
  só a checagem 1 é pulada.
- Leitura do `.sha256`: o coletor lê até 4096 bytes e pega a primeira palavra
  de 64 dígitos hexadecimais em qualquer ponto do texto
  (`\b[0-9a-fA-F]{64}\b`), passada para minúsculas (a família RIR, ao
  contrário, lê só a primeira linha não vazia). Com o `.sha256` de uma linha
  do registro.br, valem os formatos BSD (`SHA256 (arq) = <hash>`) e GNU
  (`<hash>  arq`).
- Erros do `.sha256` (a verificação segue sem conferência, com o aviso
  `sha256 publicado indisponível; seguindo sem conferência`, campos `url` e
  `err`): `sem hash SHA-256 reconhecível em <url>`,
  `resposta maior que 4096 bytes`, `HTTP <código>` ou erro de rede.

## Download

Os valores do [padrão](../../padroes/coletor.md#download-internalfetch), com
estes detalhes:

| Item | Valor |
|---|---|
| Tamanho máximo do arquivo | 64 MiB (o arquivo tinha ~844 KB em 2026-09-28) |
| Tamanho máximo do `.sha256` | 4096 bytes |
| Status aceito | `200` (e `304` no GET condicional do arquivo); qualquer outro é o erro `HTTP <código>` |
| Novas tentativas | 2, com 10 s entre elas, em erro de rede, erro na leitura do corpo (inclusive corpo acima do limite) ou HTTP ≥ 500; valem também para o `.sha256` |
| Cliente HTTP | o do [padrão](../../padroes/coletor.md#download-internalfetch) (keep-alive de 30 s, ociosa fechada em 90 s, até 2 ociosas por host) |

## Validação de um arquivo novo

Na ordem do padrão; qualquer falha recusa o arquivo
([Recusas e falhas](#recusas-e-falhas)):

1. **Conferência**: com hash publicado, o SHA-256 do download tem de ser
   igual —
   `sha256 divergente: publicado <p>, baixado <b> (arquivo e .sha256 publicados em momentos diferentes?)`.
2. **Parser** ([fonte.md](fonte.md#regras-do-parser-internalparse)):
   `parser: <erro>`.
3. **Mínimo de ASNs**: menos de `MIN_ASNS` ASNs lidos (padrão `5000`, ~55%
   dos 9.134 de 2026-09-28) é arquivo truncado —
   `só <n> ASNs no arquivo (mínimo <m>): arquivo truncado?`. Não há mínimo de
   blocos.

A trava de remoção (na aplicação, abaixo) vale para ASNs e para blocos: com
os números de 2026-09-28 e `REMOVAL_THRESHOLD=0.05`, ela recusa a partir de
457 ASNs ou 1.100 blocos removidos de uma vez.

## Aplicação (`internal/store`)

A transação única do padrão, com estes passos:

1. `SELECT pg_try_advisory_xact_lock(hashtext($1))` com `collector-cgibr`.
   Sem a trava: `outra execução do collector-cgibr está aplicando dados` (só
   log; nenhuma linha em `cgibr_run`).
2. Carga das tabelas temporárias:

   ```sql
   CREATE TEMP TABLE stage_asn (
       asn       bigint PRIMARY KEY,
       name      text   NOT NULL,
       document  text   NOT NULL
   ) ON COMMIT DROP;
   CREATE TEMP TABLE stage_prefix (
       prefix    cidr   PRIMARY KEY,
       asn       bigint NOT NULL
   ) ON COMMIT DROP;
   ```

   `COPY` de todos os ASNs (`asn`, `name`, `document`) e de todos os blocos
   (`prefix`, `asn` da linha); depois `ANALYZE stage_asn; ANALYZE stage_prefix`.
3. **Trava de remoção** (pulada com `--force`): conta os ASNs de `cgibr_asn`
   que não estão em `stage_asn` e os blocos de `cgibr_prefix` que não estão em
   `stage_prefix`. Tabela vazia não trava. Se removidas / atuais passar de
   `REMOVAL_THRESHOLD` (ASNs conferidos primeiro), recusa:
   `o arquivo removeria <r> de <n> ASNs (<x.x>%, limite <y.y>%); use --force se for legítimo`
   (ou `... blocos ...`).
4. ASNs novos e alterados:

   ```sql
   MERGE INTO cgibr_asn t
   USING stage_asn s ON t.asn = s.asn
   WHEN MATCHED AND (t.name, t.document) IS DISTINCT FROM (s.name, s.document) THEN
       UPDATE SET name = s.name, document = s.document
   WHEN NOT MATCHED THEN
       INSERT (asn, name, document) VALUES (s.asn, s.name, s.document)
   RETURNING merge_action()
   ```

5. Blocos novos, trocados de ASN e removidos, com o `asn_uuid` resolvido pelo
   ASN da linha (já gravado no passo 4); a única coluna de dado é `asn_uuid`,
   NOT NULL, então `<>` faz o papel do `IS DISTINCT FROM`:

   ```sql
   MERGE INTO cgibr_prefix t
   USING (SELECT s.prefix, a.uuid AS asn_uuid
            FROM stage_prefix s
            JOIN cgibr_asn a ON a.asn = s.asn) s
      ON t.prefix = s.prefix
   WHEN MATCHED AND t.asn_uuid <> s.asn_uuid THEN
       UPDATE SET asn_uuid = s.asn_uuid
   WHEN NOT MATCHED BY TARGET THEN
       INSERT (prefix, asn_uuid) VALUES (s.prefix, s.asn_uuid)
   WHEN NOT MATCHED BY SOURCE THEN
       DELETE
   RETURNING merge_action()
   ```

6. ASNs que sumiram, **depois** dos blocos:
   `DELETE FROM cgibr_asn a WHERE NOT EXISTS (SELECT 1 FROM stage_asn s WHERE s.asn = a.asn)`.
   Difere do padrão (um `MERGE` que também apaga) por causa da cascata de
   `fk_cgibr_prefix_asn`: apagando o ASN antes, os blocos dele sairiam sem
   passar pelo `MERGE` (`prefix_deleted` errado), e um bloco que passou para
   outro ASN seria apagado e reinserido como novo. Nesta ordem, a cascata não
   encontra nada.
7. `INSERT` em `cgibr_run` com `status = 1`, contagens e alterações (as de
   `merge_action()` e as linhas do `DELETE`); o `uuid` devolvido é a versão
   nova.
8. `jobs`:

   ```sql
   INSERT INTO jobs (app, last_sync_at, last_check_at, consolidated) VALUES ($1, NOW(), NOW(), 0)
   ON CONFLICT (app) DO UPDATE SET
       last_sync_at  = NOW(),
       last_check_at = NOW(),
       consolidated  = CASE WHEN $2 THEN 0 ELSE jobs.consolidated END
   ```

   `$1` = `collector-cgibr`; `$2` = a soma das seis alterações é maior que
   zero. Na primeira aplicação (linha nova), `consolidated` nasce `0`. Um
   arquivo novo com o mesmo conteúdo lógico (um `--force`, ou só a ordem das
   linhas mudou) gera versão nova e não pede consolidação.
9. `COMMIT`.

Sem mudança, só:
`INSERT INTO jobs (app, last_check_at) VALUES ($1, NOW()) ON CONFLICT (app) DO UPDATE SET last_check_at = NOW()`.

A última execução aplicada (checagens e validadores) vem de
`SELECT uuid::text, url, coalesce(etag, ''), coalesce(last_modified, ''), sha256, created_at FROM cgibr_run WHERE status = 1 ORDER BY created_at DESC LIMIT 1`.

## Recusas e falhas

| Situação | Linha em `cgibr_run` | O que a linha traz além de `url`, `forced`, `started_at` | Mensagem (`err` do log `verificação falhou`) |
|---|---|---|---|
| banco fora ao ler a última execução | não | — | `lendo o último arquivo aplicado: <erro>` |
| download falhou (rede, 4xx, 5xx depois das tentativas, limite) | não | — | `download: <erro>` |
| `jobs` não atualizou numa verificação sem mudança | não | — | `jobs: <erro>` |
| outra execução aplicando | não | — | `outra execução do collector-cgibr está aplicando dados` |
| hash divergente | `status = 0` | download (`http_status`, `etag`, `last_modified`, `sha256`, `bytes`); contagens NULL; `warnings` `[]` | `sha256 divergente: ...` |
| parser recusou | `status = 0` | idem | `parser: ...` (acima de 1% descartado, os 3 primeiros avisos vão na mensagem; `warnings` fica `[]`) |
| menos de `MIN_ASNS` | `status = 0` | download + `asns`, `prefixes_v4`, `prefixes_v6` e `warnings` | `só <n> ASNs no arquivo ...` |
| trava de remoção | `status = 0` | idem | `o arquivo removeria ...` |
| erro no banco durante a aplicação | `status = 0` | idem | o erro, com o passo (ex.: `staging: ...`, `copy stage_prefix: ...`, `merge cgibr_asn: ...`, `delete cgibr_asn: ...`, `cgibr_run: ...`, `jobs: ...`) |

- Numa recusa, as seis alterações ficam NULL e as tabelas de dados ficam como
  estavam (a aplicação não começou, ou a transação foi desfeita).
- A linha de recusa é gravada numa instrução à parte, fora da transação, com
  um contexto sem cancelamento (`context.WithoutCancel`): é gravada mesmo com o
  `RUN_TIMEOUT` estourado. Se ela falhar, o log diz
  `não consegui gravar a falha em cgibr_run`.
- `POSTGRES_URL` inválida (`POSTGRES_URL inválida: ...`) é tratada como
  qualquer falha de conexão na subida: novas tentativas por 2 minutos e depois
  `sem conexão com o Postgres`, saída 1.

### Avisos

Como no [padrão](../../padroes/coletor.md#recusas-e-falhas):

- `cgibr_run.warnings` recebe os 50 primeiros avisos do parser e, se houve
  mais, um último item `... e mais <n> avisos` (até 51 itens).
- No log, cada aviso sai numa linha `aviso do parser` (campo `warning`) **só
  quando o arquivo é aplicado**, logo depois de `arquivo novo aplicado`; numa
  recusa depois do parser, ficam só em `cgibr_run.warnings` (numa recusa do
  próprio parser, `warnings` fica `[]` e os 3 primeiros vão na mensagem).

## Opções

As [comuns](../../padroes/coletor.md#configuração-comum), com os mesmos
padrões (`SYNC_INTERVAL=1h`, `RETRY_INTERVAL=5m`, `RUN_TIMEOUT=10m`,
`REMOVAL_THRESHOLD=0.05` — fração de ASNs ou de blocos), mais:

| Variável | Argumento | Padrão | Uso |
|---|---|---|---|
| `SOURCE_URL` | `--source-url` | `https://ftp.registro.br/pub/numeracao/origin/nicbr-asn-blk-latest.txt` | arquivo `nicbr-asn-blk`; tem de começar com `http://` ou `https://` |
| `SOURCE_SHA256_URL` | `--source-sha256-url` | vazio = `SOURCE_URL` + `.sha256` | hash publicado; `off`, `none` ou `false` (em qualquer caixa) desligam a checagem 1 e a conferência |
| `MIN_ASNS` | `--min-asns` | `5000` | inteiro ≥ 0; abaixo disso o arquivo é recusado como truncado |

`collector-cgibr --help` (sai com 0; escreve no stderr):

```
collector-cgibr — importa o arquivo de ASNs e blocos do NIC.br (registro.br)
para as tabelas cgibr_* do PostgreSQL do BadBlock.

Verifica a fonte a cada SYNC_INTERVAL e só aplica quando o arquivo muda
(SHA-256 publicado, ETag/Last-Modified e hash do conteúdo). Não tem API HTTP.

Uso:
  collector-cgibr [opções]

Opções (padrão → variável de ambiente → argumento):
  --log-format           json ou text
                         env LOG_FORMAT, padrão json
  --log-level            debug, info, warn ou error
                         env LOG_LEVEL, padrão info
  --min-asns             abaixo disso o arquivo é tratado como truncado e recusado
                         env MIN_ASNS, padrão 5000
  --postgres-url         URL do Postgres, ex.: postgres://postgres:senha@badblock-postgres:5432/badblock (obrigatória)
                         env POSTGRES_URL, padrão ""
  --removal-threshold    fração máxima de ASNs ou blocos removidos de uma vez sem --force
                         env REMOVAL_THRESHOLD, padrão 0.05
  --retry-interval       espera até a próxima tentativa depois de uma verificação que falhou
                         env RETRY_INTERVAL, padrão 5m
  --run-timeout          tempo máximo de uma verificação (download + aplicação)
                         env RUN_TIMEOUT, padrão 10m
  --source-sha256-url    hash publicado; vazio = SOURCE_URL + ".sha256", "off" desliga a conferência
                         env SOURCE_SHA256_URL, padrão ""
  --source-url           arquivo nicbr-asn-blk do NIC.br
                         env SOURCE_URL, padrão https://ftp.registro.br/pub/numeracao/origin/nicbr-asn-blk-latest.txt
  --sync-interval        intervalo entre verificações da fonte
                         env SYNC_INTERVAL, padrão 1h
  --user-agent           User-Agent das requisições (vazio = badblock-collector-cgibr/<versão>)
                         env USER_AGENT, padrão ""
  --once                 faz uma verificação e sai
  --force                aplica mesmo sem mudança e ignora a trava de remoção (implica --once)
  --version              mostra a versão e sai
  -h, --help             mostra esta ajuda
```

Opções inválidas (stderr `collector-cgibr: <mensagem>`, saída 2):

| Caso | Mensagem |
|---|---|
| duração que `time.ParseDuration` não lê (ex.: `1d`) ou ≤ 0 | `--sync-interval inválido: "<valor>"` (idem `--retry-interval`, `--run-timeout`) |
| `MIN_ASNS` não inteiro ou negativo | `--min-asns inválido: "<valor>"` |
| `REMOVAL_THRESHOLD` fora de 0 a 1 | `--removal-threshold inválido (0 a 1): "<valor>"` |
| `LOG_LEVEL` fora de `debug`, `info`, `warn`, `error` (em qualquer caixa) | `--log-level inválido: "<valor>"` |
| `LOG_FORMAT` fora de `json`, `text` (em qualquer caixa) | `--log-format inválido: "<valor>"` |
| `SOURCE_URL` sem `http://`/`https://` | `--source-url precisa ser http(s): "<valor>"` |
| sem `POSTGRES_URL` (dispensada com `--version`) | `defina POSTGRES_URL (ou --postgres-url)` |
| argumento que não é opção | `argumento inesperado: <arg>` |

A linha `iniciando` traz `version`, `commit`, `source`, `sha256` (a URL do
hash; vazia quando desligado), `interval`, `retry` e `postgres` (sem a
senha); não inclui `MIN_ASNS`, `RUN_TIMEOUT` nem `REMOVAL_THRESHOLD`.

## Compose e `.env`

| Variável no `.env` | Vira | Padrão no compose | `.env.example` do app | `.env.example` da raiz |
|---|---|---|---|---|
| `COLLECTOR_CGIBR_TAG` | tag da imagem e `VERSION` do build | `latest` (`dev` no build) | `latest` | `latest` |
| `COLLECTOR_CGIBR_SYNC_INTERVAL` | `SYNC_INTERVAL` | `1h` | `1h` | `1h` |
| `COLLECTOR_CGIBR_RETRY_INTERVAL` | `RETRY_INTERVAL` | `5m` | `5m` | `5m` |
| `COLLECTOR_CGIBR_SOURCE_URL` | `SOURCE_URL` | a URL padrão acima | — | — |
| `COLLECTOR_CGIBR_MIN_ASNS` | `MIN_ASNS` | `5000` | `5000` | `5000` |
| `COLLECTOR_CGIBR_REMOVAL_THRESHOLD` | `REMOVAL_THRESHOLD` | `0.05` | `0.05` | `0.05` |

O compose passa também as comuns `POSTGRES_URL` (montada com
`POSTGRES_PASSWORD` se ausente) e `LOG_LEVEL`, e fixa `LOG_FORMAT: json`.
`SOURCE_SHA256_URL`, `RUN_TIMEOUT` e `USER_AGENT` não passam pelo compose
(valem os padrões do app), e `COLLECTOR_CGIBR_SOURCE_URL` não está nos
`.env.example` — o que a [regra das opções](../../projeto/convencoes.md#configuração)
permite: só entra ali o que o deploy precisa ajustar.

## Operação

```bash
make -C apps/cgibr/collector once    # verificação agora, no container no ar
make -C apps/cgibr/collector force   # reaplica o arquivo atual (ignora "sem mudança" e a trava de remoção)
make -C apps/cgibr/collector logs
make -C database/postgres psql       # e: SELECT created_at, status, forced, asns, error FROM cgibr_run ORDER BY created_at DESC LIMIT 5;
```

A saúde se vê em `jobs.last_check_at` da linha `collector-cgibr` (anda a cada
`SYNC_INTERVAL`), no bloco `collector` de `GET /cgibr/meta` e nos logs (JSON,
`app=collector-cgibr` em toda linha):

| Mensagem | Nível | Campos |
|---|---|---|
| `iniciando` | info | acima |
| `fonte sem mudança` | info | `reason`, `version`, `ms` |
| `arquivo novo aplicado` | info | `version`, `sha256`, `asns`, `prefixes_v4`, `prefixes_v6`, `asn_inserted`, `asn_updated`, `asn_deleted`, `prefix_inserted`, `prefix_updated`, `prefix_deleted`, `warnings` (quantidade), `forced`, `ms` |
| `aviso do parser` | warn | `warning` |
| `sha256 publicado indisponível; seguindo sem conferência` | warn | `url`, `err` |
| `verificação falhou` | error | `err`, `ms` |
| `não consegui gravar a falha em cgibr_run` | error | `err` |
| `Postgres indisponível; tentando de novo` / `sem conexão com o Postgres` | warn / error | `err` |
| `próxima verificação` | debug | `in` |
| `encerrando` | info | — |

## Medições

- Arquivo de 2026-09-28: ~844 KB, 9.134 ASNs e 21.991 blocos
  ([fonte.md](fonte.md#fatos-medidos-2026-09-28)).
- Não há tempos de parser nem de aplicação registrados: o app não tem o alvo
  `make test-real` (no [padrão](../../padroes/coletor.md#testes), ele existe só
  nos RIRs e na IANA). **Pendente**: um teste com o arquivo real inteiro
  (`CGIBR_REAL_FILE`) para medir.

## Testes específicos

Unitários (`make test`; `parse` e `collector` usam a fixture de
[fonte.md](fonte.md#fixture-testdatanicbr-asn-blk-sampletxt)):

| Pacote | Teste | Confere |
|---|---|---|
| `parse` | `TestParseSample` | 11 ASNs, 52/7 blocos, zero avisos e descartes; AS61610 (nome, documento, os 3 blocos na ordem do arquivo); AS6125 sem blocos; AS275689 com `10996639`; nome UTF-8 do AS174 |
| `parse` | `TestParseEdgeCases` | BOM, comentário, CRLF, linha vazia; bits de host; bloco repetido na linha e em outro ASN; bloco inválido (`lixo`) e campo vazio; ASN repetido em minúsculas (`as1`) somando blocos com o nome da primeira ocorrência; 100 linhas do mesmo ASN |
| `parse` | `TestParseRejectsBrokenFile` | recusa: só comentário; separador `;`; ASN sem `AS` |
| `parse` | `TestParseToleratesFewBadLines` | 1 linha ruim em 201 (0,5%) passa, com `Skipped = 1` |
| `fetch` | `TestPublishedSHA256Formats`, `TestPublishedSHA256Garbage` | `.sha256` BSD, GNU e em maiúsculas; HTML sem hash é erro |
| `fetch` | `TestDownloadConditional` | `ETag`/`Last-Modified` lidos, SHA-256 do corpo, `304` com `If-None-Match` |
| `fetch` | `TestDownloadRetriesServerErrors`, `TestDownloadDoesNotRetry404`, `TestDownloadMaxBytes` | 502 duas vezes e depois 200 (3 tentativas); 404 uma vez só; corpo acima do limite é erro |
| `config` | `TestDefaults`, `TestPrecedence`, `TestSHA256Off`, `TestForceImpliesOnce`, `TestInvalid`, `TestRedact` | padrões (URLs, `1h`, `5m`, `5000`, `0.05`); argumento > ambiente > padrão; `off`; `--force` implica `--once`; sem `POSTGRES_URL`, `0s`, limite `2`, `ftp://`, `trace`; senha trocada por `***` |
| `collector` | `TestFirstRunApplies` | banco vazio aplica; a execução leva 11/52/7, o SHA-256 e o `ETag` |
| `collector` | `TestSameSHA256SkipsDownload` | hash publicado igual: sem download, `jobs` tocado, versão anterior |
| `collector` | `TestNotModifiedSendsValidators` | `.sha256` fora do ar: GET condicional com o `ETag` anterior, `304` = sem mudança |
| `collector` | `TestSameContentIsUnchanged` | `.sha256` fora e conteúdo igual: sem mudança |
| `collector` | `TestForceAppliesSameFile` | `--force` aplica o mesmo arquivo, sem validadores, com `forced` |
| `collector` | `TestSHA256MismatchIsRecorded`, `TestTooFewASNsIsRecorded` | hash divergente e mínimo (5 no teste) viram recusa gravada |
| `collector` | `TestBusyIsNotRecorded` | outra execução aplicando: erro sem linha de recusa |

Integração (`make test-int`): `TestApplyLifecycle` em `internal/store`, num
`postgres:18-trixie` descartável (banco `badblock`, usuário `postgres`, senha
`pg`), com só o `migrate:up` de `central/` e `cgibr/`:

1. banco vazio: nenhuma execução aplicada;
2. carga da fixture: 11 ASNs e 59 blocos inseridos (70 alterações), `jobs`
   com `last_sync_at`, `last_check_at` e `consolidated = 0`, 52/7 por
   `family`, `document_digits` do AS61610 = `35980592000130`;
3. `consolidated = 1` (como a fase 2) e a mesma carga de novo: 0 alterações,
   versão nova, `consolidated` continua `1`;
4. o AS6505 sai, o AS61610 muda de nome e passa `187.87.28.0/22` para o novo
   AS64500 (que traz também `2001:db8::/32`), com limite `0.2` (1 de 11 ASNs =
   9,1%): 1 ASN inserido, 1 alterado e 1 apagado; 1 bloco inserido e 1
   trocado; `consolidated` volta a `0`; o dono do bloco é o AS64500;
5. um dataset de 2 ASNs dispara a trava (`RemovalError`) sem mexer nas
   tabelas (11 ASNs); com `--force`, 9 ASNs apagados;
6. `RecordFailure` grava `status = 0` com `error` e não vira versão;
7. `TouchCheck` só anda `last_check_at`.
