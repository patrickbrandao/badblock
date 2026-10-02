# Coletor `collector-rootanchors`

O que o `collector-rootanchors` faz de diferente ou a mais que o
[padrão dos coletores](../../padroes/coletor.md); o resto (`main.go`, laço,
`--once`/`--force`, contrato com `jobs`, container) segue o padrão. Formato do
arquivo e parser: [fonte.md](fonte.md). Tabelas: [dados.md](dados.md). Dono:
sub-agente `collector-rootanchors`. O código partiu do `collector-cgibr`
(arquivo único com hash publicado).

## Valores desta fonte

| Item | Valor |
|---|---|
| App e `jobs.app` | `collector-rootanchors` (demais nomes: [../../projeto/estrutura.md](../../projeto/estrutura.md#nomes)) |
| Tabelas escritas | `rootanchors_key`, `rootanchors_run` e a linha `collector-rootanchors` de `jobs` |
| Tabela temporária | `stage_key` (`ON COMMIT DROP`) |
| Pacotes | só os do padrão (`buildinfo`, `config`, `fetch`, `parse`, `store`, `collector`) |
| Dependências | só as do padrão; XML com `encoding/xml`, digests com `crypto/sha1`, `crypto/sha256`, `crypto/sha512` |
| Conexão | pool pgx de no máximo 2 conexões, ociosas fechadas em 5 min; `application_name=collector-rootanchors` quando a `POSTGRES_URL` não define outro |
| Descrição da imagem | `Importa as âncoras de confiança DNSSEC da raiz (root-anchors.xml da IANA) para o PostgreSQL do BadBlock` |

## Checagens de mudança

Três, da mais barata para a mais cara. A primeira que disser "igual" encerra
a verificação como sem mudança, com o `reason` no log `fonte sem mudança`:

| # | Checagem | Custo | `reason` |
|---|---|---|---|
| 1 | SHA-256 publicado (a linha de `root-anchors.xml` em `SOURCE_SHA256_URL`) igual ao `sha256` da última execução aplicada | 248 bytes | `sha256 publicado igual ao último aplicado` |
| 2 | GET condicional do arquivo com o `etag` (fraco) e o `last_modified` da última execução aplicada → `304` | só cabeçalhos | `HTTP 304` |
| 3 | SHA-256 do arquivo baixado (descomprimido) igual ao `sha256` da última execução aplicada | o download (1.861 bytes; 1.063 com gzip) | `conteúdo igual ao último aplicado` |

- Sem execução aplicada (banco vazio), nenhuma checagem diz "igual": a
  primeira verificação sempre baixa e aplica.
- Os validadores só são enviados se a URL da última execução aplicada for a
  mesma; a comparação do SHA-256 vale para qualquer URL.
- Não há checagem de "arquivo mais antigo que o aplicado": o XML não traz
  serial nem data de geração (`validFrom`/`validUntil` são das chaves, não do
  arquivo), e o `Last-Modified` só é reenviado, nunca comparado. Um arquivo
  antigo servido de novo seria aplicado; como a IANA só acrescenta chaves, a
  trava de remoção pega o caso típico (uma chave nova que some).
- O arquivo de hashes é baixado mesmo com `--force` (a conferência continua
  valendo); só a checagem 1 é pulada.
- **Leitura do `checksums-sha256.txt`** (`fetch.FindSHA256`): até 4096 bytes,
  linha a linha (sem os espaços das pontas, `\r` incluído). Vale a linha cujo
  nome de arquivo — o último segmento do caminho, então `./x/root-anchors.xml`
  também serve — é igual ao último segmento do caminho de `SOURCE_URL`
  (`root-anchors.xml`). Formatos aceitos: GNU (`<64 hex> <espaços ou tab>[*]<arquivo>`)
  e BSD (`SHA256 (<arquivo>) = <64 hex>`), hash em qualquer caixa (guardado em
  minúsculas). **Não** vale "o primeiro hash do texto", como no
  `collector-cgibr`: a primeira linha do arquivo real é a do `icannbundle.pem`.
- Erros do arquivo de hashes (a verificação segue sem conferência, com o aviso
  `sha256 publicado indisponível; seguindo sem conferência`, campos `url` e
  `err`): `sem hash SHA-256 de root-anchors.xml em <url>`,
  `resposta maior que 4096 bytes`, `HTTP <código>` ou erro de rede.

## Download

Os valores do [padrão](../../padroes/coletor.md#download-internalfetch), com
estes detalhes:

| Item | Valor |
|---|---|
| Tamanho máximo do arquivo | **1 MiB** (`MaxBytes: 1 << 20`; o arquivo tinha 1.861 bytes em 2026-09-30) — a fonte baixa o limite de 64 MiB do padrão |
| Tamanho máximo do arquivo de hashes | 4096 bytes |
| Compressão | o `fetch` não define `Accept-Encoding`; o transporte do Go pede `gzip` e descomprime sozinho. Corpo, limite, SHA-256 e `bytes` valem para o XML descomprimido |
| ETag | fraco (`W/"745-6262f56bfe940-br"`), guardado como veio e reenviado igual; o Cloudflare responde 304 ([fonte.md](fonte.md#servidor-http-medido-em-2026-09-30)) |
| Status aceito | `200` (e `304` no GET condicional do arquivo); qualquer outro é o erro `HTTP <código>` |
| Novas tentativas | 2, com 10 s entre elas, em erro de rede, erro na leitura do corpo (inclusive acima do limite) ou HTTP ≥ 500; valem também para o arquivo de hashes |

## Validação de um arquivo novo

Na ordem do padrão; qualquer falha recusa o arquivo
([Recusas e falhas](#recusas-e-falhas)):

1. **Conferência**: com hash publicado, o SHA-256 do download tem de ser
   igual —
   `sha256 divergente: publicado <p>, baixado <b> (arquivo e checksums-sha256.txt publicados em momentos diferentes?)`.
2. **Parser** ([fonte.md](fonte.md#regras-do-parser-internalparse)), com o
   recálculo do key tag e do digest de cada chave com `PublicKey`:
   `parser: <erro>`. Não há registro descartado: qualquer regra que falhe
   recusa.
3. **Mínimo de chaves**: menos de `MIN_KEYS` `KeyDigest` (padrão `1`) é
   arquivo truncado —
   `só <n> chaves no arquivo (mínimo <m>): arquivo truncado?`. Com o padrão, o
   mínimo repete a regra do parser (`nenhum KeyDigest no arquivo`); o
   operador pode subir para `3` (as chaves de 2026-09-30) se quiser que um
   arquivo com menos chaves nunca passe, mesmo com `--force`.

**Trava de remoção** (na aplicação, abaixo): com 3 linhas e
`REMOVAL_THRESHOLD=0.05`, remover **qualquer** chave (1 de 3 = 33,3%) passa do
limite, então toda remoção é recusada sem `--force`. É o comportamento
desejado: a IANA mantém as chaves aposentadas no arquivo (com `validUntil`),
então uma chave que some é inesperada — arquivo errado, espelho desatualizado
ou mudança de formato — e pede um operador. Remoção legítima:
`make -C apps/rootanchors/collector force`. Alterações (ex.: a IANA
preenchendo `validUntil` numa rolagem) e inserções não passam pela trava.

## Aplicação (`internal/store`)

A transação única do padrão:

1. `SELECT pg_try_advisory_xact_lock(hashtext($1))` com `collector-rootanchors`.
   Sem a trava: `outra execução do collector-rootanchors está aplicando dados`
   (só log; nenhuma linha em `rootanchors_run`).
2. Carga da tabela temporária e `COPY` de todas as chaves:

   ```sql
   CREATE TEMP TABLE stage_key (
       key_id       text        PRIMARY KEY,
       key_tag      integer     NOT NULL,
       algorithm    smallint    NOT NULL,
       digest_type  smallint    NOT NULL,
       digest       text        NOT NULL,
       public_key   text,
       flags        integer,
       valid_from   timestamptz NOT NULL,
       valid_until  timestamptz
   ) ON COMMIT DROP;
   ```

3. **Trava de remoção** (pulada com `--force`): conta as chaves de
   `rootanchors_key` que não estão em `stage_key`. Tabela vazia não trava. Se
   removidas / atuais passar de `REMOVAL_THRESHOLD`, recusa:
   `o arquivo removeria <r> de <n> chaves (<x.x>%, limite <y.y>%); use --force se for legítimo`.
4. Um `MERGE` só (não há FK, então inserir, alterar e apagar vão juntos):

   ```sql
   MERGE INTO rootanchors_key t
   USING stage_key s ON t.key_id = s.key_id
   WHEN MATCHED AND (t.key_tag, t.algorithm, t.digest_type, t.digest, t.public_key, t.flags, t.valid_from, t.valid_until)
          IS DISTINCT FROM (s.key_tag, s.algorithm, s.digest_type, s.digest, s.public_key, s.flags, s.valid_from, s.valid_until) THEN
       UPDATE SET key_tag = s.key_tag, algorithm = s.algorithm, digest_type = s.digest_type, digest = s.digest,
                  public_key = s.public_key, flags = s.flags, valid_from = s.valid_from, valid_until = s.valid_until
   WHEN NOT MATCHED BY TARGET THEN
       INSERT (key_id, key_tag, algorithm, digest_type, digest, public_key, flags, valid_from, valid_until)
       VALUES (s.key_id, s.key_tag, s.algorithm, s.digest_type, s.digest, s.public_key, s.flags, s.valid_from, s.valid_until)
   WHEN NOT MATCHED BY SOURCE THEN
       DELETE
   RETURNING merge_action()
   ```

5. `INSERT` em `rootanchors_run` com `status = 1`, `anchor_id`,
   `anchor_source`, `zone`, `keys` e as três alterações; o `uuid` devolvido é
   a versão nova.
6. `jobs`, como no padrão:
   `INSERT ... ON CONFLICT (app) DO UPDATE SET last_sync_at = NOW(), last_check_at = NOW(), consolidated = CASE WHEN $2 THEN 0 ELSE jobs.consolidated END`,
   com `$2` = alguma alteração. Um `--force` com o mesmo conteúdo gera versão
   nova e não pede consolidação.
7. `COMMIT`.

Sem mudança, só:
`INSERT INTO jobs (app, last_check_at) VALUES ($1, NOW()) ON CONFLICT (app) DO UPDATE SET last_check_at = NOW()`.

A última execução aplicada (checagens e validadores) vem de
`SELECT uuid::text, url, coalesce(etag, ''), coalesce(last_modified, ''), sha256, created_at FROM rootanchors_run WHERE status = 1 ORDER BY created_at DESC LIMIT 1`.

## Recusas e falhas

| Situação | Linha em `rootanchors_run` | O que a linha traz além de `url`, `forced`, `started_at` | Mensagem (`err` do log `verificação falhou`) |
|---|---|---|---|
| banco fora ao ler a última execução | não | — | `lendo o último arquivo aplicado: <erro>` |
| download falhou (rede, 4xx, 5xx depois das tentativas, limite) | não | — | `download: <erro>` |
| `jobs` não atualizou numa verificação sem mudança | não | — | `jobs: <erro>` |
| outra execução aplicando | não | — | `outra execução do collector-rootanchors está aplicando dados` |
| hash divergente | `status = 0` | download (`http_status`, `etag`, `last_modified`, `sha256`, `bytes`); `anchor_id`, `zone`, `keys` NULL; `warnings` `[]` | `sha256 divergente: ...` |
| parser recusou | `status = 0` | idem | `parser: ...` |
| menos de `MIN_KEYS` | `status = 0` | download + `anchor_id`, `anchor_source`, `zone`, `keys` e `warnings` | `só <n> chaves no arquivo ...` |
| trava de remoção | `status = 0` | idem | `o arquivo removeria ...` |
| erro no banco durante a aplicação | `status = 0` | idem | o erro, com o passo (`staging: ...`, `copy stage_key: ...`, `merge rootanchors_key: ...`, `rootanchors_run: ...`, `jobs: ...`) |

- Numa recusa, as três alterações ficam NULL e `rootanchors_key` fica como
  estava.
- A linha de recusa é gravada fora da transação, com um contexto sem
  cancelamento (`context.WithoutCancel`). Se ela falhar, o log diz
  `não consegui gravar a falha em rootanchors_run`.
- `POSTGRES_URL` inválida é tratada como falha de conexão na subida (2 minutos
  de tentativas e `sem conexão com o Postgres`, saída 1).

### Avisos

Como no [padrão](../../padroes/coletor.md#recusas-e-falhas): os 50 primeiros
avisos do parser ([fonte.md](fonte.md#avisos)) e, se houve mais,
`... e mais <n> avisos` vão para `rootanchors_run.warnings`; no log, cada um
sai numa linha `aviso do parser` só quando o arquivo é aplicado.

## Opções

As [comuns](../../padroes/coletor.md#configuração-comum), com
`SYNC_INTERVAL=6h` (o arquivo muda poucas vezes por ano e o Cloudflare o
guarda por 24 h — o mesmo motivo da decisão #13 da IANA em
[../../projeto/decisoes.md](../../projeto/decisoes.md)), `RETRY_INTERVAL=5m`,
`RUN_TIMEOUT=10m` e `REMOVAL_THRESHOLD=0.05` (fração de chaves), mais:

| Variável | Argumento | Padrão | Uso |
|---|---|---|---|
| `SOURCE_URL` | `--source-url` | `https://data.iana.org/root-anchors/root-anchors.xml` | tem de começar com `http://` ou `https://` e ter um nome de arquivo no caminho |
| `SOURCE_SHA256_URL` | `--source-sha256-url` | vazio = `checksums-sha256.txt` na pasta de `SOURCE_URL` (sem query) | arquivo de hashes; o coletor procura nele a linha do último segmento de `SOURCE_URL`. `off`, `none` ou `false` (qualquer caixa) desligam a checagem 1 e a conferência; outro valor tem de ser `http(s)` |
| `MIN_KEYS` | `--min-keys` | `1` | inteiro ≥ 0; abaixo disso o arquivo é recusado como truncado |

**Diferença do padrão**: vazia, a URL do hash do padrão é `SOURCE_URL` +
`.sha256`; aqui é `<pasta de SOURCE_URL>/checksums-sha256.txt`, porque a IANA
publica um arquivo de hashes por pasta, não um por arquivo
([fonte.md](fonte.md#urls)). Um espelho em `http://m/dns/root-anchors.xml`
tem o hash procurado em `http://m/dns/checksums-sha256.txt`.

`collector-rootanchors --help` (sai com 0; escreve no stderr):

```
collector-rootanchors — importa o root-anchors.xml da IANA (âncoras de
confiança DNSSEC da zona raiz) para as tabelas rootanchors_* do PostgreSQL do
BadBlock.

Verifica a fonte a cada SYNC_INTERVAL e só aplica quando o arquivo muda
(SHA-256 publicado, ETag/Last-Modified e hash do conteúdo). Confere o key tag
e o digest de cada chave com PublicKey antes de aplicar. Não tem API HTTP.

Uso:
  collector-rootanchors [opções]

Opções (padrão → variável de ambiente → argumento):
  --log-format           json ou text
                         env LOG_FORMAT, padrão json
  --log-level            debug, info, warn ou error
                         env LOG_LEVEL, padrão info
  --min-keys             mínimo de chaves (KeyDigest) no arquivo; abaixo disso ele é tratado como truncado e recusado
                         env MIN_KEYS, padrão 1
  --postgres-url         URL do Postgres, ex.: postgres://postgres:senha@badblock-postgres:5432/badblock (obrigatória)
                         env POSTGRES_URL, padrão ""
  --removal-threshold    fração máxima de chaves removidas de uma vez sem --force (com 3 chaves, qualquer remoção passa do limite)
                         env REMOVAL_THRESHOLD, padrão 0.05
  --retry-interval       espera até a próxima tentativa depois de uma verificação que falhou
                         env RETRY_INTERVAL, padrão 5m
  --run-timeout          tempo máximo de uma verificação (download + aplicação)
                         env RUN_TIMEOUT, padrão 10m
  --source-sha256-url    hashes publicados; vazio = checksums-sha256.txt na pasta de SOURCE_URL, "off" desliga a conferência
                         env SOURCE_SHA256_URL, padrão ""
  --source-url           arquivo root-anchors.xml da IANA
                         env SOURCE_URL, padrão https://data.iana.org/root-anchors/root-anchors.xml
  --sync-interval        intervalo entre verificações da fonte
                         env SYNC_INTERVAL, padrão 6h
  --user-agent           User-Agent das requisições (vazio = badblock-collector-rootanchors/<versão>)
                         env USER_AGENT, padrão ""
  --once                 faz uma verificação e sai
  --force                aplica mesmo sem mudança e ignora a trava de remoção (implica --once)
  --version              mostra a versão e sai
  -h, --help             mostra esta ajuda
```

Opções inválidas (stderr `collector-rootanchors: <mensagem>`, saída 2):

| Caso | Mensagem |
|---|---|
| duração que `time.ParseDuration` não lê (ex.: `1d`) ou ≤ 0 | `--sync-interval inválido: "<valor>"` (idem `--retry-interval`, `--run-timeout`) |
| `MIN_KEYS` não inteiro ou negativo | `--min-keys inválido: "<valor>"` |
| `REMOVAL_THRESHOLD` fora de 0 a 1 | `--removal-threshold inválido (0 a 1): "<valor>"` |
| `LOG_LEVEL` / `LOG_FORMAT` fora dos valores | `--log-level inválido: "<valor>"` / `--log-format inválido: "<valor>"` |
| `SOURCE_URL` sem `http://`/`https://` | `--source-url precisa ser http(s): "<valor>"` |
| `SOURCE_URL` sem host ou sem nome de arquivo no caminho (ex.: `https://x/`) | `--source-url inválido: "<valor>"` |
| `SOURCE_SHA256_URL` que não é `off` nem `http(s)` | `--source-sha256-url precisa ser http(s): "<valor>"` |
| sem `POSTGRES_URL` (dispensada com `--version`) | `defina POSTGRES_URL (ou --postgres-url)` |
| argumento que não é opção | `argumento inesperado: <arg>` |

A linha `iniciando` traz `version`, `commit`, `source`, `sha256` (a URL do
arquivo de hashes; vazia quando desligado), `min_keys`, `interval`, `retry` e
`postgres` (sem a senha).

## Compose e `.env`

| Variável no `.env` | Vira | Padrão no compose | `.env.example` do app | `.env.example` da raiz |
|---|---|---|---|---|
| `COLLECTOR_ROOTANCHORS_TAG` | tag da imagem e `VERSION` do build | `latest` (`dev` no build) | `latest` | `latest` |
| `COLLECTOR_ROOTANCHORS_SYNC_INTERVAL` | `SYNC_INTERVAL` | `6h` | `6h` | `6h` |
| `COLLECTOR_ROOTANCHORS_RETRY_INTERVAL` | `RETRY_INTERVAL` | `5m` | `5m` | `5m` |
| `COLLECTOR_ROOTANCHORS_SOURCE_URL` | `SOURCE_URL` | a URL padrão acima | — | — |
| `COLLECTOR_ROOTANCHORS_MIN_KEYS` | `MIN_KEYS` | `1` | `1` | `1` |
| `COLLECTOR_ROOTANCHORS_REMOVAL_THRESHOLD` | `REMOVAL_THRESHOLD` | `0.05` | `0.05` | `0.05` |

O compose passa também as comuns `POSTGRES_URL` (montada com
`POSTGRES_PASSWORD` se ausente) e `LOG_LEVEL`, e fixa `LOG_FORMAT: json`.
`SOURCE_SHA256_URL`, `RUN_TIMEOUT` e `USER_AGENT` não passam pelo compose
(valem os padrões do app).

## Operação

```bash
make -C apps/rootanchors/collector once    # verificação agora, no container no ar
make -C apps/rootanchors/collector force   # reaplica o arquivo atual (ignora "sem mudança" e a trava de remoção)
make -C apps/rootanchors/collector logs
make -C database/postgres psql             # e: SELECT created_at, status, forced, keys, key_inserted, key_updated, key_deleted, error FROM rootanchors_run ORDER BY created_at DESC LIMIT 5;
```

A saúde se vê em `jobs.last_check_at` da linha `collector-rootanchors` (anda a
cada `SYNC_INTERVAL`), no `/meta` da `api-rootanchors` e nos logs (JSON,
`app=collector-rootanchors` em toda linha):

| Mensagem | Nível | Campos |
|---|---|---|
| `iniciando` | info | acima |
| `fonte sem mudança` | info | `reason`, `version`, `ms` |
| `arquivo novo aplicado` | info | `version`, `sha256`, `anchor_id`, `keys`, `key_inserted`, `key_updated`, `key_deleted`, `warnings` (quantidade), `forced`, `ms` |
| `aviso do parser` | warn | `warning` |
| `sha256 publicado indisponível; seguindo sem conferência` | warn | `url`, `err` |
| `verificação falhou` | error | `err`, `ms` |
| `não consegui gravar a falha em rootanchors_run` | error | `err` |
| `Postgres indisponível; tentando de novo` / `sem conexão com o Postgres` | warn / error | `err` |
| `próxima verificação` | debug | `in` |
| `encerrando` | info | — |

## Medições

Verificação real contra `data.iana.org` em 2026-09-30, com
`go run ./cmd/collector-rootanchors --once` num `postgres:18-trixie`
descartável com as migrations de `central/` e `rootanchors/` (máquina de
desenvolvimento, macOS):

| Execução | Resultado | Tempo |
|---|---|---|
| 1ª `--once` (banco vazio) | `arquivo novo aplicado`: 3 chaves inseridas, 0 avisos, `anchor_id` `0C05FDD6-422C-4910-8ED6-430ED15E11C2`, `etag` `W/"745-6262f56bfe940-br"` | 267 ms |
| 2ª `--once` | `fonte sem mudança`, `sha256 publicado igual ao último aplicado` | 82 ms |
| `--once` com `SOURCE_SHA256_URL=off` | `fonte sem mudança`, `HTTP 304` (o ETag fraco reenviado) | 88 ms |
| `--force` | `arquivo novo aplicado`, `forced=true`, 0 alterações, versão nova | 117 ms |

Depois: 3 linhas em `rootanchors_key` (19036 sem `public_key`/`flags` e com
`valid_until` 2019-01-11; 20326 e 38696 com `flags` 257), 2 linhas
`status = 1` em `rootanchors_run` e `jobs` com `consolidated = 0`. O `EXPLAIN`
das consultas da API ([dados.md](dados.md#consultas-da-api-rootanchors)) usa
`ix_rootanchors_run_applied` e `ix_rootanchors_key_key_tag`.

Não há alvo `make test-real`: o arquivo real **inteiro** já é a fixture
(`testdata/root-anchors.xml`), e o parser e a aplicação são exercitados com
ele em `make test` e `make test-int`. Para conferir o arquivo do dia, a
verificação acima (`--once` num Postgres descartável) basta.

## Testes específicos

Unitários (`make test`):

| Pacote | Teste | Confere |
|---|---|---|
| `parse` | `TestParseSample` | as 3 chaves do arquivo real (ids, key tags, digests, validade em UTC, `PublicKey`/`Flags` só na 20326 e na 38696), `id` e `source` do `TrustAnchor`, zero avisos |
| `parse` | `TestKeyTagAndDigestOfRealKeys`, `TestKeyTagAlgorithm1` | key tag e digest recalculados batem com os publicados; tamanhos de SHA-1/SHA-384; um `KeyDigest` SHA-384 calculado (em minúsculas) é aceito; regra do algoritmo 1 |
| `parse` | `TestParseRejectsFixtures` | as quatro variantes de `testdata/` |
| `parse` | `TestParseRejects` | 29 casos: vazio, outra raiz, lixo depois da raiz, `id`/`Zone`/`KeyDigest` ausentes ou repetidos, datas inválidas ou invertidas, `KeyTag`/`Algorithm` fora da faixa, `DigestType` 3, digest curto ou não hex, `PublicKey` sem `Flags` e vice-versa, `Flags` sem Zone Key, `Flags` 256 (muda o key tag), base64 inválido, chave trocada |
| `parse` | `TestParseTolerates`, `TestParseWarnsUnknown`, `TestWarningsAreCapped` | chave quebrada em linhas, digest com espaços e minúsculas, `validUntil` = `validFrom`; avisos de elemento/atributo desconhecido; 60 avisos → 50 guardados |
| `fetch` | `TestFindSHA256RealFile`, `TestFindSHA256Formats` | a linha certa do `checksums-sha256.txt` real (não a primeira); GNU com `*`, caminho e CRLF, BSD, maiúsculas; HTML, hash curto e nome ausente dão "" |
| `fetch` | `TestPublishedSHA256`, `TestPublishedSHA256TooLarge` | hash pelo HTTP, nome ausente, 404; resposta acima de 4096 bytes |
| `fetch` | `TestDownloadGzipWeakETag`, `TestDownloadConditional`, `TestDownloadRetriesServerErrors`, `TestDownloadDoesNotRetry404`, `TestDownloadMaxBytes` | gzip com ETag fraco (1.861 bytes e o SHA-256 do XML descomprimido; 304 com o ETag fraco); GET condicional; 5xx repetido; 404 não; limite |
| `config` | `TestDefaults`, `TestSHA256URLFollowsSource`, `TestSHA256Off`, `TestPrecedence`, `TestAllFromEnv`, `TestVersionWithoutPostgres`, `TestHelpListsEveryOption`, `TestForceImpliesOnce`, `TestInvalid`, `TestRedact`, `TestUnexpectedArgument` | padrões (`6h`, `MIN_KEYS=1`, URL do `checksums-sha256.txt`); URL do hash segue a pasta de `SOURCE_URL`; `off`/`none`/`false`; validações |
| `collector` | `TestFirstRunApplies` … `TestDownloadFailureIsNotRecorded` | store e fonte falsos: primeira carga (nome `root-anchors.xml` pedido ao arquivo de hashes), hash igual sem download, 304 com validadores, outra URL sem validadores, conteúdo igual, `--force`, hash divergente, recusas do parser com as 4 variantes, `MIN_KEYS`, trava de remoção, avisos limitados a 50 + resumo, `ErrBusy` e falha de download sem linha |

Integração (`make test-int`), num `postgres:18-trixie` descartável (banco
`badblock`, usuário `postgres`, senha `pg`) com o `migrate:up` de `central/`
e `rootanchors/`:

- `TestApplyLifecycle`: banco vazio; carga do arquivo real (3 inseridas,
  NULLs da 19036, `anchor_id`/`zone`/`keys` na execução); mesma carga com
  `consolidated = 1` (0 alterações, versão nova, flag mantida, `updated_at`
  intacto); rolagem simulada (chave nova + `validUntil` na 20326: 1/1/0,
  `consolidated` volta a 0); trava (1 de 4 removida → `RemovalError`, tabela
  intacta) e `--force` (1 apagada); recusa gravada com colunas do parser
  NULL, sem virar versão; `TouchCheck`.
- `TestApplyBusy`: advisory lock ocupado dá `ErrBusy` sem linha em
  `rootanchors_run`.
- `TestTouchCheckCreatesJob`: a linha de `jobs` nasce só com `last_check_at`.
