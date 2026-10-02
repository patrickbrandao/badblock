# ripe/asnames — coletor (`collector-ripe-asnames`)

O que o `collector-ripe-asnames` faz de diferente ou a mais que o
[padrão dos coletores](../../../padroes/coletor.md). O resto — `main.go`, laço,
download, recusas, `--force`, contrato com `jobs`, configuração comum,
container — segue o padrão; as mensagens de log exatas estão em
[Recusas, erros e logs](#recusas-erros-e-logs). Formato e parser:
[fonte.md](fonte.md); tabelas: [dados.md](dados.md).

## Valores desta fonte

| Item | Valor |
|---|---|
| App, binário | `collector-ripe-asnames` (`/collector-ripe-asnames` na imagem) |
| Module Go | `github.com/patrickbrandao/badblock/apps/ripe/asnames/collector` |
| Imagem, container | `tmsoftbrasil/badblock-collector-ripe-asnames`, `badblock-collector-ripe-asnames` |
| Descrição da imagem (`org.opencontainers.image.description`) | `Importa os nomes e países de todos os ASNs (asn.txt do RIPE NCC) para o PostgreSQL do BadBlock` |
| Linha em `jobs` | `app = 'collector-ripe-asnames'` |
| Trava de concorrência | `pg_try_advisory_xact_lock(hashtext('collector-ripe-asnames'))` |
| Tabelas | `ripe_asnames_asn`, `ripe_asnames_run`; temporária `stage_asn` |
| Pool pgx | no máximo 2 conexões, fechadas depois de 5 min ociosas; `application_name = collector-ripe-asnames` (se a URL não trouxer outro) |
| User-Agent | `badblock-collector-ripe-asnames/<versão> (+https://github.com/patrickbrandao/badblock)` |
| Fixture | `testdata/asn-sample.txt` ([fonte.md](fonte.md#fixture)) |
| Arquivo real nos testes | variável `RIPE_ASNAMES_REAL_FILE` |

Os pacotes são os do padrão (`buildinfo`, `config`, `fetch`, `parse`,
`store`, `collector`), sem pacote a mais; `internal/fetch` não tem a parte de
hash publicado.

## Checagens de mudança

O RIPE NCC não publica hash do `asn.txt` ([fonte.md](fonte.md#arquivo)), então
são duas checagens, da mais barata para a mais cara; a primeira que disser
"igual" encerra a verificação como sem mudança:

| # | Checagem | "Igual" quando | `reason` no log | Custo |
|---|---|---|---|---|
| 1 | GET condicional com `If-None-Match`/`If-Modified-Since` da última execução aplicada | HTTP 304 | `HTTP 304` | só cabeçalhos |
| 2 | SHA-256 do arquivo baixado, já descomprimido | igual ao `sha256` da última execução aplicada | `conteúdo igual ao último aplicado` | download (~2,3 MB com gzip) |

- **Nunca remova essas checagens.** Se o RIPE NCC passar a publicar um hash,
  ele entra como a primeira checagem (spec antes do código), com a opção de
  URL do hash do padrão.
- Não há opção de hash (`SOURCE_SHA256_URL`, `SOURCE_MD5_URL`) nem
  conferência do download.
- Não há checagem de "arquivo mais antigo que o aplicado": o `asn.txt` não
  traz data, serial nem cabeçalho. Um arquivo antigo servido de novo seria
  aplicado como novo; o `MIN_ASNS` e a trava de remoção limitam o estrago.
- Os validadores só são enviados se a URL da última execução aplicada for a
  mesma; a comparação do SHA-256 vale para qualquer URL (trocar para um
  espelho com o mesmo conteúdo dá "sem mudança").
- "Sem mudança" não grava linha em `ripe_asnames_run`, então os validadores
  continuam os da última execução aplicada: se o servidor regravar o arquivo
  com o mesmo conteúdo (ETag novo), cada verificação seguinte baixa o arquivo
  inteiro e para na checagem 2, até o conteúdo mudar.
- Com `SYNC_INTERVAL` de 1h e o arquivo atualizado cerca de uma vez por dia,
  quase toda verificação termina em 304.
- `--force` não envia validadores e pula a checagem 2.

## Download: gzip e ETag fraco

Cliente, prazos, limite e novas tentativas são os do padrão (limite de
64 MiB: `MaxBytes: 64 << 20`; o arquivo tem ~6,2 MB). O que é desta fonte:

- O `fetch` não define `Accept-Encoding`, então o transporte do Go pede
  `gzip` sozinho e descomprime de forma transparente: trafegam ~2,3 MB em vez
  de ~6,2 MB. Corpo, limite de tamanho e SHA-256 valem para o arquivo
  **descomprimido**; `ripe_asnames_run.bytes` é o tamanho descomprimido.
- Com gzip o nginx do RIPE manda o ETag **fraco** (`W/"6aba461c-5ec4fe"`). Ele
  é guardado como veio em `ripe_asnames_run.etag` e reenviado igual no
  `If-None-Match`, e o servidor responde 304 a ele
  ([fonte.md](fonte.md#servidor-http-medido-em-2026-09-28)).

## Validações antes de aplicar

1. Conferência de hash: não há.
2. Parser: as regras de [fonte.md](fonte.md#linhas); mais de 1% de linhas
   descartadas recusa o arquivo.
3. **`MIN_ASNS`** (padrão `100000`; o arquivo real tem ~122,6 mil, uma folga
   de ~18%): menos ASNs aceitos que isso é arquivo truncado, recusado com
   `só <n> ASNs no arquivo (mínimo <m>): arquivo truncado?`.

## Aplicação

Tudo numa transação (`internal/store`, `Store.Apply`):

```sql
SELECT pg_try_advisory_xact_lock(hashtext('collector-ripe-asnames'));  -- false: ErrBusy

CREATE TEMP TABLE stage_asn (
    asn          bigint PRIMARY KEY,
    description  text   NOT NULL,
    handle       text,
    name         text,
    country      text
) ON COMMIT DROP;
-- COPY stage_asn (asn, description, handle, name, country): campo derivado vazio vira NULL
ANALYZE stage_asn;

-- trava de remoção (pulada com --force): linhas atuais e as que sumiriam
SELECT (SELECT count(*) FROM ripe_asnames_asn),
       (SELECT count(*) FROM ripe_asnames_asn a
         WHERE NOT EXISTS (SELECT 1 FROM stage_asn s WHERE s.asn = a.asn));

WITH m AS (
    MERGE INTO ripe_asnames_asn t
    USING stage_asn s ON t.asn = s.asn
    WHEN MATCHED AND (t.description, t.handle, t.name, t.country)
                     IS DISTINCT FROM (s.description, s.handle, s.name, s.country) THEN
        UPDATE SET description = s.description, handle = s.handle, name = s.name, country = s.country
    WHEN NOT MATCHED BY TARGET THEN
        INSERT (asn, description, handle, name, country)
        VALUES (s.asn, s.description, s.handle, s.name, s.country)
    WHEN NOT MATCHED BY SOURCE THEN
        DELETE
    RETURNING merge_action() AS action
)
SELECT action, count(*) FROM m GROUP BY action;   -- INSERT / UPDATE / DELETE

INSERT INTO ripe_asnames_run (status, forced, url, http_status, etag, last_modified,
                         sha256, bytes, asns, asn_inserted, asn_updated, asn_deleted,
                         warnings, error, started_at)
VALUES (1, ...) RETURNING uuid::text;             -- a versão nova do dataset

INSERT INTO jobs (app, last_sync_at, last_check_at, consolidated)
VALUES ('collector-ripe-asnames', NOW(), NOW(), 0)
ON CONFLICT (app) DO UPDATE SET
    last_sync_at  = NOW(),
    last_check_at = NOW(),
    consolidated  = CASE WHEN <alguma linha mudou> THEN 0 ELSE jobs.consolidated END;
```

- **Trava de remoção**: recusa quando a tabela tem linhas e
  `removidos / atuais > REMOVAL_THRESHOLD` (estritamente maior); a primeira
  carga (tabela vazia) nunca trava. Mensagem:
  `o arquivo removeria <r> de <c> ASNs (<x.y>%, limite <z.w>%); use --force se for legítimo`.
- **Um `MERGE` só**, com `WHEN NOT MATCHED BY SOURCE` e
  `RETURNING merge_action()` (exigem PostgreSQL 17 ou mais novo; o projeto
  usa o 18). As contagens saem agregadas pelo próprio servidor: no máximo
  três linhas voltam ao coletor, e não uma por ASN.
- A comparação inclui os campos **derivados**: uma mudança nas regras do
  parser reaplicada com `--force` atualiza as linhas afetadas, e só elas
  (`updated_at` das outras não muda).
- `stage_asn` tem `PRIMARY KEY (asn)`: um ASN repetido faria o `COPY`
  falhar, mas o parser já descarta repetidos.
- `consolidated = 0` só quando o `MERGE` alterou alguma linha: um arquivo
  novo com o mesmo conteúdo lógico (SHA-256 diferente, nenhuma linha mudada)
  não pede consolidação e mantém o valor que estava. Se a linha de `jobs`
  ainda não existe, ela nasce com `consolidated = 0`.
- Última execução aplicada (validadores e SHA-256 da próxima verificação):
  `SELECT uuid::text, url, coalesce(etag, ''), coalesce(last_modified, ''), sha256, created_at FROM ripe_asnames_run WHERE status = 1 ORDER BY created_at DESC LIMIT 1`.
- Verificação sem mudança: `INSERT INTO jobs (app, last_check_at) VALUES ('collector-ripe-asnames', NOW()) ON CONFLICT (app) DO UPDATE SET last_check_at = NOW()`
  (cria a linha só com `last_check_at` se ela não existir).

## Recusas, erros e logs

| Momento | Causa | Linha em `ripe_asnames_run` | Mensagem (`err` do log `verificação falhou`; `error` da linha) |
|---|---|---|---|
| antes do download | banco fora ao ler a última execução | não | `lendo o último arquivo aplicado: <erro>` |
| antes do download | rede, HTTP diferente de 200 e 304, corpo cortado ou acima de 64 MiB (rede, 5xx e corpo depois das 2 novas tentativas) | não | `download: <erro>` (ex.: `download: HTTP 503`) |
| sem mudança | falha ao gravar `jobs` | não | `jobs: <erro>` |
| depois do download | parser | sim, `status = 0`, `asns` NULL | `parser: <mensagem do parser>` |
| depois do download | menos que `MIN_ASNS` | sim, com `asns` | `só <n> ASNs no arquivo (mínimo <m>): arquivo truncado?` |
| depois do download | trava de remoção | sim | `o arquivo removeria …; use --force se for legítimo` |
| depois do download | erro do banco na aplicação | sim | `staging: …`, `copy stage_asn: …`, `merge ripe_asnames_asn: …`, `ripe_asnames_run: …`, `jobs: …` |
| depois do download | outra execução aplicando (advisory lock ocupado) | **não** | `outra execução do collector-ripe-asnames está aplicando dados` |

- A recusa é gravada com um contexto sem cancelamento
  (`context.WithoutCancel`): fica registrada mesmo com o `RUN_TIMEOUT`
  esgotado. Se nem ela puder ser gravada, o log diz
  `não consegui gravar a falha em ripe_asnames_run`.
- Avisos do parser: os 50 primeiros, mais `... e mais <n> avisos` quando
  passam disso (até 51 itens em `ripe_asnames_run.warnings`). No log, uma linha
  `aviso do parser` por aviso, só quando o arquivo é aplicado. Numa recusa
  depois do parser, eles ficam só na linha de `ripe_asnames_run`; numa recusa do
  próprio parser, `warnings` fica `[]` e a mensagem traz os 3 primeiros.

| Log | Nível | Atributos (além de `app`) |
|---|---|---|
| `iniciando` | info | `version`, `commit`, `source`, `interval`, `retry`, `postgres` (sem a senha) |
| `Postgres indisponível; tentando de novo` / `sem conexão com o Postgres` | warn / error | `err` |
| `fonte sem mudança` | info | `reason` (`HTTP 304` ou `conteúdo igual ao último aplicado`), `version`, `ms` |
| `arquivo novo aplicado` | info | `version`, `sha256`, `bytes`, `asns`, `asn_inserted`, `asn_updated`, `asn_deleted`, `warnings` (quantos), `forced`, `ms` |
| `aviso do parser` | warn | `warning` |
| `verificação falhou` | error | `err`, `ms` |
| `não consegui gravar a falha em ripe_asnames_run` | error | `err` |
| `próxima verificação` | debug | `in` |
| `encerrando` | info | — |

## Configuração

As opções comuns do [padrão](../../../padroes/coletor.md#configuração-comum),
com os mesmos padrões (`SYNC_INTERVAL` `1h`, `RETRY_INTERVAL` `5m`,
`RUN_TIMEOUT` `10m`, `REMOVAL_THRESHOLD` `0.05`, `LOG_LEVEL` `info`,
`LOG_FORMAT` `json`, `--once`, `--force`, `--version`), mais:

| Variável | Argumento | Padrão | Uso |
|---|---|---|---|
| `SOURCE_URL` | `--source-url` | `https://ftp.ripe.net/ripe/asnames/asn.txt` | arquivo da fonte; tem de começar com `http://` ou `https://` |
| `MIN_ASNS` | `--min-asns` | `100000` | inteiro ≥ 0; abaixo disso o arquivo é tratado como truncado e recusado (`0` desliga) |
| `USER_AGENT` | `--user-agent` | vazio = `badblock-collector-ripe-asnames/<versão> (+https://github.com/patrickbrandao/badblock)` | |

Valores inválidos (stderr `collector-ripe-asnames: <erro>`, saída 2):

| Opção | Regra | Mensagem |
|---|---|---|
| `SYNC_INTERVAL`, `RETRY_INTERVAL`, `RUN_TIMEOUT` | duração Go (`time.ParseDuration`) maior que zero | `--sync-interval inválido: "0s"` (idem para as outras) |
| `MIN_ASNS` | inteiro ≥ 0 | `--min-asns inválido: "-1"` |
| `REMOVAL_THRESHOLD` | número de 0 a 1 | `--removal-threshold inválido (0 a 1): "2"` |
| `LOG_LEVEL` | `debug`, `info`, `warn` ou `error` (sem diferenciar maiúsculas) | `--log-level inválido: "trace"` |
| `LOG_FORMAT` | `json` ou `text` (idem) | `--log-format inválido: "xml"` |
| `SOURCE_URL` | `http://` ou `https://` | `--source-url precisa ser http(s): "ftp://x"` |
| `POSTGRES_URL` | obrigatória (menos com `--version`) | `defina POSTGRES_URL (ou --postgres-url)` |
| argumento posicional | não aceito | `argumento inesperado: <arg>` |

`--help` lista as opções em ordem alfabética, cada uma com a variável e o
padrão, e depois `--once`, `--force`, `--version` e `-h, --help`.

### Compose e `.env`

O `docker-compose.yml` do app passa ao container:

| Variável no container | Valor |
|---|---|
| `POSTGRES_URL` | `${POSTGRES_URL:-postgres://postgres:${POSTGRES_PASSWORD:?defina POSTGRES_PASSWORD no .env}@badblock-postgres:5432/badblock?sslmode=disable}` |
| `SYNC_INTERVAL` | `${COLLECTOR_RIPE_ASNAMES_SYNC_INTERVAL:-1h}` |
| `RETRY_INTERVAL` | `${COLLECTOR_RIPE_ASNAMES_RETRY_INTERVAL:-5m}` |
| `SOURCE_URL` | `${COLLECTOR_RIPE_ASNAMES_SOURCE_URL:-https://ftp.ripe.net/ripe/asnames/asn.txt}` |
| `MIN_ASNS` | `${COLLECTOR_RIPE_ASNAMES_MIN_ASNS:-100000}` |
| `REMOVAL_THRESHOLD` | `${COLLECTOR_RIPE_ASNAMES_REMOVAL_THRESHOLD:-0.05}` |
| `LOG_LEVEL` | `${LOG_LEVEL:-info}` |
| `LOG_FORMAT` | `json` |

Imagem `${DOCKERHUB_NAMESPACE:-tmsoftbrasil}/badblock-collector-ripe-asnames:${COLLECTOR_RIPE_ASNAMES_TAG:-latest}`,
com `build` da pasta (`VERSION: ${COLLECTOR_RIPE_ASNAMES_TAG:-dev}`). `RUN_TIMEOUT` e
`USER_AGENT` não passam pelo compose (valem os padrões do app).

`.env.example` do app e da raiz: `COLLECTOR_RIPE_ASNAMES_TAG=latest` (na raiz, na
seção das imagens), `COLLECTOR_RIPE_ASNAMES_SYNC_INTERVAL=1h`,
`COLLECTOR_RIPE_ASNAMES_RETRY_INTERVAL=5m`, `COLLECTOR_RIPE_ASNAMES_MIN_ASNS=100000` e
`COLLECTOR_RIPE_ASNAMES_REMOVAL_THRESHOLD=0.05` (na raiz, sob o comentário
`# collector-ripe-asnames: nomes de AS do RIPE NCC (asn.txt)`); o do app traz
também `POSTGRES_PASSWORD=`, a `POSTGRES_URL` comentada e `LOG_LEVEL=info`.
`COLLECTOR_RIPE_ASNAMES_SOURCE_URL` é aceita pelo compose, mas não aparece em
nenhum dos dois `.env.example`.

## Operação

```bash
make -C apps/ripe/asnames/collector once    # verificação agora, no container no ar
make -C apps/ripe/asnames/collector force   # reaplica o arquivo atual (recalcula handle/name/country)
make -C apps/ripe/asnames/collector logs
```

- `--force` aqui serve para: recalcular os campos derivados depois de mudar
  as regras do parser ([fonte.md](fonte.md#mudar-uma-regra)), reaplicar
  depois de mudar o schema, aceitar uma remoção grande legítima.
- Estado: a linha `collector-ripe-asnames` de `jobs`, o `/ripe/asnames/meta` da API e
  as últimas execuções:

  ```sql
  SELECT created_at, status, forced, asns, asn_inserted, asn_updated, asn_deleted, error
    FROM ripe_asnames_run ORDER BY created_at DESC LIMIT 5;
  ```

## Medições

Arquivo de 2026-09-28 (122.591 ASNs); PG18 local via testcontainers.

| O quê | Valor | Como |
|---|---|---|
| Download completo | ~7 s | do servidor do RIPE NCC, em 2026-09-28 |
| Parser | ~50 ms (47–49 ms) | `TestParseRealFile`, em 2026-09-28 e 2026-09-29 |
| Carga inicial (tabela vazia) | ~2,2–2,5 s | `TestApplyRealFile`, em 2026-09-28 e 2026-09-29 |
| Reaplicação sem mudança | ~0,3–0,6 s | idem |

A busca `ILIKE` medida no mesmo teste está em [dados.md](dados.md#ripe_asnames_asn).

## Testes

Camadas e comandos do [padrão](../../../padroes/coletor.md#testes). O que é
desta fonte:

| Pacote | Testes | O quê |
|---|---|---|
| `parse` | `TestParseSample` | a fixture: 37 ASNs, nenhum aviso, e 21 ASNs conferidos campo a campo (os três formatos, `X - X` com ` - ` dentro, nome e handle vazios, `EU`/`AP`, mojibake e controles C1, 32 bits) |
| `parse` | `TestDerive` | as regras em descrições sintéticas e reais (sem país, país em minúsculas ou com 3 letras, só handle, só país, `X - X` mínimo, AS401635) |
| `parse` | `TestParseEdgeCases`, `TestParseRejectsBrokenFile`, `TestParseToleratesFewBadLines`, `TestParseRejectsJustOverLimit` | BOM, comentário, CRLF, tab, UTF-8 inválido e NUL, repetido, 4294967295; recusa de arquivo vazio, formato novo, `AS` no número, acima de 32 bits, negativo, sem descrição e repetidos demais; 0,5% de descarte passa e 2% recusa |
| `fetch` | `TestDownloadConditional`, `TestDownloadIfModifiedSince`, `TestDownloadGzipWeakETag` | 304 por ETag e por data, User-Agent; gzip com ETag fraco: corpo e SHA-256 do arquivo descomprimido, e o ETag fraco devolve 304 |
| `fetch` | `TestDownloadRetriesServerErrors`, `TestDownloadGivesUpAfterRetries`, `TestDownloadRetriesTruncatedBody`, `TestDownloadDoesNotRetry404`, `TestDownloadMaxBytes`, `TestDownloadCanceledContextStopsRetries` | 5xx até 3 tentativas (1 + 2 novas), corpo cortado repetido, 404 sem repetir, limite de tamanho, contexto cancelado interrompe a espera |
| `config` | `TestDefaults`, `TestPrecedence`, `TestAllFromEnv`, `TestVersionWithoutPostgres`, `TestHelpListsEveryOption`, `TestForceImpliesOnce`, `TestInvalid`, `TestRedact`, `TestUnexpectedArgument` | padrões, ordem padrão → ambiente → argumento, `--version` sem `POSTGRES_URL`, `--help` com toda opção e variável, `--force` implica `--once`, validações, senha escondida, argumento posicional |
| `collector` | `TestFirstRunApplies` … `TestWarningsAreCapped` | store e fonte falsos: primeira carga sem validadores, 304, outra URL sem validadores, conteúdo igual, `--force`, falhas antes do download sem registro, recusas do parser, do `MIN_ASNS` e da trava gravadas, `ErrBusy` sem registro, avisos limitados a 50 + resumo |
| `store` (integração) | `TestApplyLifecycle` | carga inicial (37 inserções; NULLs; controles C1 preservados; busca `côte` → 2, handle `google` → 1, país `BR` → 3); reaplicação igual (versão nova, nada muda, `consolidated = 1` fica, `updated_at` intacto); mudança, remoção e inserção (1/1/1, `consolidated` volta a 0); trava 35 de 37 e `--force`; recusa gravada sem virar versão; `TouchCheck` |
| `store` (integração) | `TestTouchCheckCreatesJob`, `TestApplyBusy` | linha de `jobs` criada só com `last_check_at`; advisory lock ocupado dá `ErrBusy` sem linha em `ripe_asnames_run` |

Integração: `postgres:18-trixie` descartável (banco `badblock`, usuário
`postgres`, senha `pg`), com as seções `migrate:up` de
`database/postgres/central/` e `database/postgres/ripe_asnames/`.

**Arquivo real**: o `Makefile` deste app não tem o alvo `test-real`; os testes
leem `RIPE_ASNAMES_REAL_FILE`:

```bash
curl -o /tmp/asn.txt https://ftp.ripe.net/ripe/asnames/asn.txt
RIPE_ASNAMES_REAL_FILE=/tmp/asn.txt make test-int   # TestParseRealFile e TestApplyRealFile
```

- `TestParseRealFile` (roda também em `make test`): nenhum descarte, 100 mil
  ASNs ou mais; loga o total, o tempo e quantos ficaram sem país, handle e
  nome.
- `TestApplyRealFile`: carga inicial insere tudo, reaplicação sem mudança
  altera 0 linhas, a contagem bate, `ILIKE '%google%'` acha AS15169 com
  handle `GOOGLE` e o `EXPLAIN` usa `ix_ripe_asnames_asn_description_trgm`; loga
  os tempos.
- A `api-ripe-asnames` tem o próprio teste com o arquivo real, na mesma variável
  ([api.md](api.md)).
