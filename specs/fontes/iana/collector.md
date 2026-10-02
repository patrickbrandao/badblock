# collector-iana

O que o `collector-iana` faz **de diferente ou a mais** que o
[padrão dos coletores](../../padroes/coletor.md); o resto segue o padrão. Os
arquivos da IANA, o parser e a sanidade estão em [fonte.md](fonte.md); as
tabelas, em [dados.md](dados.md).

## Diferenças do padrão

| Assunto | Padrão | `collector-iana` | Por quê |
|---|---|---|---|
| Fonte | um arquivo (`SOURCE_URL`) | 10 arquivos tratados como **um dataset**, com as bases `IANA_BASE_URL` e `RDAP_BASE_URL` | a `api-iana` junta bloco, uso especial e RDAP numa resposta e nunca pode ver arquivos novos misturados com velhos |
| Checagens de mudança | hash publicado, GET condicional, SHA-256 | GET condicional **por arquivo**, SHA-256 de cada arquivo e SHA-256 combinado | a IANA não publica hash |
| Download | um arquivo | falha em qualquer um dos 10 = nada aplicado; os que vieram `304` são baixados de novo | nunca um dataset parcial |
| Sanidade | mínimos | `parse.Check`: coberturas de ASN, 256 `/8`, mínimos por arquivo, 5 RIRs | os registros cobrem espaços inteiros (todo ASN, todo `/8`): buraco ou falta é arquivo truncado |
| Trava de remoção | `REMOVAL_THRESHOLD` | + piso `store.RemovalMinRows` = 2 linhas por tabela | tabelas minúsculas (9 linhas) |
| `SYNC_INTERVAL` | `1h` | `6h` | [abaixo](#por-que-6-h) |
| Tamanho máximo | 64 MiB | 8 MiB por arquivo (o excesso é repetido, como no padrão) | arquivos de 0,5 a 25 KB |
| `<fonte>_run` | colunas por arquivo e por tabela | `iana_run.files` e `iana_run.changes` (jsonb) | 10 arquivos, 5 tabelas |
| Logs | `arquivo novo aplicado` | `dataset novo aplicado`; `iniciando` sem mínimos (são fixos no código) | são 10 arquivos |
| `make test-real` | `FILE=...` | sem `FILE`: baixa os 10 arquivos (ou usa `IANA_REAL_DIR`) | são 10 arquivos |

## Código

Pacotes do [padrão](../../padroes/coletor.md#estrutura-do-código), mais
`internal/source`:

```
internal/
├── source/     Files: os 10 arquivos (Name, RDAP, Path) na ordem fixa; DefaultIANABaseURL,
│               DefaultRDAPBaseURL; File.URL(ianaBase, rdapBase), File.Basename()
├── fetch/      Fetcher.Download(ctx, url, Validators{ETag, LastModified})
│               → Download{URL, Status, NotModified, Body, SHA256, ETag, LastModified}
├── parse/      parse.go (Dataset, Parse), csv.go (7 CSVs), rdap.go (3 JSONs), fields.go (campos),
│               check.go (Limits, DefaultLimits, Check)
├── store/      Store: LastApplied, TouchCheck, Apply, RecordFailure; File (objeto de iana_run.files),
│               Applied, Run, Changes; Tables; RemovalMinRows, RemovalError, ErrBusy; AppName
└── collector/  Collector.RunOnce(ctx, force) → Result{Outcome, Reason, Version, Run, Changes};
                CombinedSHA256; interfaces Store e Source (os testes usam falsos)
```

## Uma verificação

`collector.RunOnce`:

1. Lê o último dataset aplicado (`store.LastApplied`: `iana_run` com
   `status = 1`, `ORDER BY created_at DESC, uuid DESC LIMIT 1`). Erro →
   `lendo o último dataset aplicado: ...`, só log.
2. **GET condicional de cada arquivo**, na ordem fixa, com o `ETag`
   (`If-None-Match`) e o `Last-Modified` (`If-Modified-Since`) do mesmo
   arquivo em `iana_run.files`. Os validadores só vão se a URL do arquivo for
   a mesma daquela vez (e nunca com `--force`). Cada arquivo sai como:
   `304` → não mudou; `200` com o SHA-256 igual ao do último aplicado →
   conteúdo igual (servidor que ignora o condicional, ou `ETag` novo com o
   mesmo conteúdo); senão → mudou.
3. **Nenhum mudou** → sem mudança (só `jobs.last_check_at`), com `reason`
   `nenhum dos 10 arquivos mudou (N com HTTP 304, M com conteúdo igual)`.
4. **Algum mudou** → os que vieram `304` são baixados de novo, **sem
   validadores** (o dataset inteiro tem ~60 KB), para o dataset ser
   interpretado e aplicado inteiro. Um `304` sem validadores é erro
   (`download de <nome>: HTTP 304 sem validadores`).
5. Monta `iana_run.files` ([dados.md](dados.md#files-e-changes)) e o
   **SHA-256 combinado** (`CombinedSHA256`): SHA-256, em hex minúsculo, do
   texto `<nome>:<sha256 do arquivo>\n` dos 10 arquivos, na ordem fixa.
   Igual ao do último aplicado → sem mudança, `reason`
   `conteúdo igual ao último dataset aplicado` (proteção redundante: na
   prática o passo 3 já decide).
6. Dataset novo: parser (`parse.Parse`), sanidade
   (`parse.Check(ds, parse.DefaultLimits())`), aplicação. As regras do parser
   e da sanidade estão em [fonte.md](fonte.md#regras-do-parser-internalparse).

Trocar uma base (`IANA_BASE_URL`, `RDAP_BASE_URL`) faz a próxima verificação
baixar tudo sem validadores (a URL mudou), mas só aplica se o conteúdo for
diferente.

Qualquer falha de download, em qualquer um dos 10 arquivos (inclusive no
download completo do passo 4), **falha a verificação inteira**: nada é
aplicado, nada vai para `iana_run`, só o log (`download de <nome>: <erro>`).

### Download (`internal/fetch`)

Como no [padrão](../../padroes/coletor.md#download-internalfetch), com:

- **8 MiB** por arquivo (`MaxBytes`); resposta maior (`resposta maior que
  8388608 bytes`) conta como corpo com defeito e também é repetida;
- 2 novas tentativas por arquivo, 10 s entre elas (rede, corpo cortado ou
  grande demais, `5xx`); `4xx` e os demais códigos abaixo de 500 que não
  sejam `200` ou `304` falham na hora (`HTTP 404`);
- conexão: keep-alive de 30 s, conexões ociosas por 90 s, até 2 por host;
- um `304` devolve os validadores enviados (o arquivo guarda os anteriores).

## Aplicação (`store.Apply`)

Numa transação só:

1. `SELECT pg_try_advisory_xact_lock(hashtext('collector-iana'))`; falso →
   `ErrBusy` (`outra execução do collector-iana está aplicando dados`), sem
   linha em `iana_run`.
2. Tabelas temporárias `stage_asn_block`, `stage_prefix_block`,
   `stage_special_prefix`, `stage_special_asn` e `stage_rdap_service`
   (`ON COMMIT DROP`), com as colunas de dado da tabela e a chave natural
   como PK; `COPY` e `ANALYZE` em cada uma.
3. **Trava de remoção** (sem `--force`), tabela por tabela: conta as linhas
   atuais e as que sumiriam (sem par na `stage_*` pela chave natural). Recusa
   se `atuais > 0`, `removidas > store.RemovalMinRows` (2) **e**
   `removidas / atuais > REMOVAL_THRESHOLD`, com
   `o dataset removeria N de M linhas de <tabela> (x%, limite 5.0% e mais de 2 linhas); use --force se for legítimo`.
4. `MERGE` em cada tabela, na ordem de `store.Tables` (`iana_asn_block`,
   `iana_prefix_block`, `iana_special_prefix`, `iana_special_asn`,
   `iana_rdap_service`), pela chave natural (`asn_start`; `prefix`; `prefix`;
   `asn_start`; `kind, resource`): `WHEN MATCHED AND (<colunas>) IS DISTINCT
   FROM (<colunas da stage>) THEN UPDATE`, `WHEN NOT MATCHED BY TARGET THEN
   INSERT`, `WHEN NOT MATCHED BY SOURCE THEN DELETE`, `RETURNING
   merge_action()`. As ações contadas viram `changes`.
5. `INSERT INTO iana_run` com `status = 1`, `forced`, `sha256` combinado,
   `files`, `changes` (sempre as 5 tabelas), `warnings` e `started_at`;
   o `uuid` devolvido é a versão nova.
6. `jobs`:

   ```sql
   INSERT INTO jobs (app, last_sync_at, last_check_at, consolidated) VALUES ('collector-iana', NOW(), NOW(), 0)
   ON CONFLICT (app) DO UPDATE SET
       last_sync_at  = NOW(),
       last_check_at = NOW(),
       consolidated  = CASE WHEN <alguma linha mudou> THEN 0 ELSE jobs.consolidated END
   ```

   Arquivos novos com o mesmo conteúdo lógico (um `Last-Modified` novo, por
   exemplo) geram versão nova sem pedir consolidação. A verificação sem
   mudança faz só `INSERT INTO jobs (app, last_check_at) ... ON CONFLICT (app)
   DO UPDATE SET last_check_at = NOW()`.

Conexão: pool de 2 conexões (`MaxConns`), ociosas por até 5 min,
`application_name = collector-iana` quando a `POSTGRES_URL` não define outro.

### Trava de remoção nas tabelas pequenas

| Tabela | Linhas (2026-09-28) | Dispara ao remover |
|---|---|---|
| `iana_asn_block` | 173 | 9 linhas ou mais (5%) |
| `iana_prefix_block` | 307 | 16 ou mais |
| `iana_special_prefix` | 51 | 3 ou mais (piso) |
| `iana_special_asn` | 9 | 3 ou mais (piso) |
| `iana_rdap_service` | 414 | 21 ou mais |

Com 5% puro, remover **uma** linha dos 9 ASNs especiais (11%) já travaria.
O piso de 2 linhas (fixo no código) deixa passar a retirada pontual de um
registro; um arquivo truncado é pego antes, pela sanidade (mínimos por
arquivo). Na prática a IANA quase não remove linhas: alocações novas trocam a
linha `Unallocated` (a chave `asn_start` dela é a mesma) e acrescentam outra;
registros especiais extintos ganham `Termination Date` em vez de sumir.

## Recusas e falhas

| Onde | O que fica |
|---|---|
| Antes dos downloads (Postgres fora, `LastApplied`), em qualquer download ou ao gravar `jobs` numa verificação sem mudança (`jobs: ...`) | só o log `verificação falhou`; nada em `iana_run`; nova tentativa em `RETRY_INTERVAL` |
| Parser | `status = 0`, `error` `parser: <arquivo>: <motivo>`, `files` sem `rows` nem `publication`, `warnings` vazio (a mensagem traz até 3 avisos do arquivo) |
| Sanidade | `status = 0`, `error` `sanidade: <falhas>`, `files` com `rows` e `publication`, `warnings` |
| Trava de remoção, erro no banco | `status = 0`, `error` com a mensagem; a transação é desfeita |
| Outra execução aplicando (`ErrBusy`) | só log; nada em `iana_run` |

- A linha de recusa é gravada fora da transação e com um contexto que não
  herda o cancelamento (`context.WithoutCancel`), para ficar registrada mesmo
  quando o `RUN_TIMEOUT` estourou. Se nem ela puder ser gravada, o log diz
  `não consegui gravar a falha em iana_run`.
- A recusa não vira versão: a próxima verificação compara com o último
  dataset **aplicado**, baixa tudo de novo e grava outra recusa enquanto a
  IANA não corrigir. Como a verificação falhou, ela vem em `RETRY_INTERVAL`:
  com os padrões, uma linha `status = 0` a cada ~5 min (até 288 por dia).
- `warnings` guarda os 50 primeiros avisos e, se houve mais, a linha
  `... e mais N avisos`. No log, os avisos só aparecem (`aviso do parser`,
  um por linha) quando o dataset é aplicado.

## `--force`

Como no [padrão](../../padroes/coletor.md#--force): baixa os 10 arquivos sem
validadores, ignora as comparações de hash (por arquivo e combinado) e a
trava de remoção; parser e sanidade continuam valendo. `changed` em `files`
continua dizendo o que difere do último dataset aplicado (tudo `false` numa
reaplicação do mesmo conteúdo), e `consolidated` só volta a 0 se alguma linha
mudou.

## Por que 6 h

A IANA muda esses registros poucas vezes por ano (os `Last-Modified` medidos
vão de 2019 a 2026-09), e o Cloudflare na frente dos dois servidores guarda os
arquivos por 1 h (`www.iana.org`) e 24 h (`data.iana.org`): verificar mais que
de hora em hora só consultaria o cache. Com 6 h, uma alocação nova chega em no
máximo 6 h; cada verificação sem mudança custa 10 GETs condicionais
respondidos com `304` (só cabeçalhos); e o dado é de referência (RIR de cada
faixa, bogons, RDAP), não de tempo real. Quem precisar antes roda
`make -C apps/iana/collector once`. Registro:
[decisão 13](../../projeto/decisoes.md).

## Configuração

Ordem, formato e opções comuns: [padrão](../../padroes/coletor.md#configuração-comum).
Não há `SOURCE_URL`; o que é da IANA:

| Variável | Argumento | Padrão | Uso |
|---|---|---|---|
| `IANA_BASE_URL` | `--iana-base-url` | `https://www.iana.org/assignments` | base dos 7 CSVs; os caminhos abaixo dela são fixos ([fonte.md](fonte.md#arquivos)) |
| `RDAP_BASE_URL` | `--rdap-base-url` | `https://data.iana.org/rdap` | base de `asn.json`, `ipv4.json` e `ipv6.json` |
| `SYNC_INTERVAL` | `--sync-interval` | `6h` | entre verificações bem-sucedidas ([por que 6 h](#por-que-6-h)) |
| `RUN_TIMEOUT` | `--run-timeout` | `10m` | uma verificação inteira: os GETs (10, ou até 19 quando algum arquivo mudou) e a aplicação |
| `REMOVAL_THRESHOLD` | `--removal-threshold` | `0.05` | fração máxima removida de **cada tabela** sem `--force`; remoções de até 2 linhas sempre passam |

- As bases servem para apontar o app para um espelho ou, nos testes, para um
  `httptest.Server`; a barra final é ignorada; têm de começar com `http://`
  ou `https://` (`--iana-base-url precisa ser http(s): "ftp://x"`).
- Validações: intervalos maiores que zero (`--sync-interval inválido:
  "0s"`); `REMOVAL_THRESHOLD` de 0 a 1 (`--removal-threshold inválido (0 a
  1): "2"`); `LOG_LEVEL` `debug`, `info`, `warn` ou `error`; `LOG_FORMAT`
  `json` ou `text`; argumento solto (`argumento inesperado: x`);
  `POSTGRES_URL` obrigatória, exceto com `--version` (`defina POSTGRES_URL
  (ou --postgres-url)`). Todas saem com código 2.
- Fixos no código (não são configuração): 2 novas tentativas com 10 s, 8 MiB
  por arquivo, `parse.DefaultLimits()`, `parse.MaxSkippedRatio` (1%),
  `parse.MaxWarnings` (50) e `store.RemovalMinRows` (2).

`collector-iana --help` (opções em ordem alfabética):

```
collector-iana — importa os registros de numeração da IANA (blocos de ASN e de
IP por RIR, special-purpose e bootstrap RDAP; 10 arquivos tratados como um
dataset) para as tabelas iana_* do PostgreSQL do BadBlock.

Verifica os arquivos a cada SYNC_INTERVAL e só aplica quando algum muda
(GET condicional com ETag/Last-Modified e hash do conteúdo). Não tem API HTTP.

Uso:
  collector-iana [opções]

Opções (padrão → variável de ambiente → argumento):
  --iana-base-url        base dos 7 CSVs da IANA (caminhos fixos abaixo dela, ex.: /as-numbers/as-numbers-1.csv)
                         env IANA_BASE_URL, padrão https://www.iana.org/assignments
  --log-format           json ou text
                         env LOG_FORMAT, padrão json
  --log-level            debug, info, warn ou error
                         env LOG_LEVEL, padrão info
  --postgres-url         URL do Postgres, ex.: postgres://postgres:senha@badblock-postgres:5432/badblock (obrigatória)
                         env POSTGRES_URL, padrão ""
  --rdap-base-url        base dos 3 JSONs do bootstrap RDAP (/asn.json, /ipv4.json, /ipv6.json)
                         env RDAP_BASE_URL, padrão https://data.iana.org/rdap
  --removal-threshold    fração máxima de linhas removidas de cada tabela sem --force (remoções de até 2 linhas sempre passam)
                         env REMOVAL_THRESHOLD, padrão 0.05
  --retry-interval       espera até a próxima tentativa depois de uma verificação que falhou
                         env RETRY_INTERVAL, padrão 5m
  --run-timeout          tempo máximo de uma verificação (downloads + aplicação)
                         env RUN_TIMEOUT, padrão 10m
  --sync-interval        intervalo entre verificações da fonte
                         env SYNC_INTERVAL, padrão 6h
  --user-agent           User-Agent das requisições (vazio = badblock-collector-iana/<versão>)
                         env USER_AGENT, padrão ""
  --once                 faz uma verificação e sai
  --force                aplica mesmo sem mudança e ignora a trava de remoção (implica --once)
  --version              mostra a versão e sai
  -h, --help             mostra esta ajuda
```

### Compose e `.env`

`apps/iana/collector/docker-compose.yml` passa ao container (o `.env` do app
e o da raiz trazem as mesmas variáveis, com estes valores):

| No container | Do `.env` | Padrão |
|---|---|---|
| `SYNC_INTERVAL` | `COLLECTOR_IANA_SYNC_INTERVAL` | `6h` |
| `RETRY_INTERVAL` | `COLLECTOR_IANA_RETRY_INTERVAL` | `5m` |
| `RUN_TIMEOUT` | `COLLECTOR_IANA_RUN_TIMEOUT` | `10m` |
| `IANA_BASE_URL` | `COLLECTOR_IANA_IANA_BASE_URL` | `https://www.iana.org/assignments` |
| `RDAP_BASE_URL` | `COLLECTOR_IANA_RDAP_BASE_URL` | `https://data.iana.org/rdap` |
| `REMOVAL_THRESHOLD` | `COLLECTOR_IANA_REMOVAL_THRESHOLD` | `0.05` |
| (tag da imagem) | `COLLECTOR_IANA_TAG` | `latest` |

Mais as comuns (`POSTGRES_URL`/`POSTGRES_PASSWORD`, `LOG_LEVEL`) e
`LOG_FORMAT: json` fixo; `USER_AGENT` não é passado (vale o padrão). No
`.env.example` da raiz, as variáveis ficam na seção dos coletores, sob o
comentário `collector-iana: 10 arquivos de registro da IANA (mudam poucas
vezes por ano)`.

## Operação

```bash
make -C apps/iana/collector once        # verificação agora, no container no ar
make -C apps/iana/collector force       # reaplica o dataset atual
make -C apps/iana/collector logs
make -C apps/iana/collector test-real   # parser + Postgres com os 10 arquivos reais de hoje
```

| Log | Nível | Campos |
|---|---|---|
| `iniciando` | info | `version`, `commit`, `iana`, `rdap` (as bases), `interval`, `retry`, `postgres` (sem a senha) |
| `fonte sem mudança` | info | `reason` (quantos vieram `304` e quantos tinham o conteúdo igual), `version`, `ms` |
| `dataset novo aplicado` | info | `version`, `sha256` (combinado), `changed_files`, `rows` (registros por arquivo), `changes` (por tabela), `total_changes`, `warnings` (quantidade), `forced`, `ms` |
| `aviso do parser` | warn | `warning`, um por aviso (só quando aplica) |
| `verificação falhou` | error | `err` (o arquivo e o motivo: download, parser, sanidade, trava de remoção, banco), `ms` |
| `não consegui gravar a falha em iana_run` | error | `err` |
| `próxima verificação` | debug | `in` |

A saúde se vê em `jobs.last_check_at` (andando a cada 6 h mesmo sem
mudança), no `/iana/meta` da `api-iana` e nas execuções:

```sql
SELECT created_at, status, forced, error, changes FROM iana_run ORDER BY created_at DESC LIMIT 5;
```

## Medições (2026-09-28)

- Verificação sem mudança: 10 GETs condicionais, 10 respostas `304` (só
  cabeçalhos).
- Dataset: 61.033 bytes nos 10 arquivos; 954 linhas nas 5 tabelas (173 +
  307 + 51 + 9 + 414); a primeira carga insere todas.
- Tempos de download e de aplicação ainda não foram registrados
  (`make test-real`, com `-v`, mostra a duração de cada teste).

## Testes

Camadas e comandos do [padrão](../../padroes/coletor.md#testes); o que é da
IANA:

| Onde | O quê |
|---|---|
| `internal/collector` | `httptest.Server` com os recortes (ETag = hash do conteúdo) e store falso: primeira carga (10 GETs sem validadores, `publication`), sem mudança por `304` e por conteúdo igual, um arquivo muda → os outros 9 são baixados de novo, falha de download (nada aplicado nem registrado), falha no download completo, `--force` (sem validadores, `changed` vazio), URL nova sem validadores, recusa do parser e da sanidade registradas, trava de remoção registrada e `ErrBusy` não, formato do hash combinado |
| `internal/parse` | contagens de cada recorte e zero avisos; faixas de ASN, blocos, especiais, RDAP; URLs coladas; campos (blocos, faixas, datas, flags, RIR); recusas (arquivo ausente, coluna obrigatória, sem linhas, JSON inválido ou sem `services`, linha ruim em arquivo pequeno, status vazio); 0,5% de linhas ruins passa; colunas fora de ordem e campos irreconhecíveis viram aviso; faixa repetida entre os arquivos; `Check` com os recortes (relaxado passa, `DefaultLimits` falha) e com um dataset completo quebrado de cada jeito |
| `internal/fetch` | condicional por `ETag` e só por `Last-Modified`, 3 tentativas em `5xx`, `404` sem repetição, limite de tamanho |
| `internal/config` | padrões, precedência, bases sem a barra final, `--force` implica `--once`, `--version` sem Postgres, valores inválidos, `--help` com todas as opções, `Redact` |
| `internal/store` (`make test-int`) | PG18 com as migrations de `central/` e `iana/`: carga inicial dos recortes (118 linhas, colunas calculadas, NULLs; `45.0.0.1` no `45/8` LEGACY da ARIN, `187.87.28.1` no `187/8` da LACNIC e no RDAP dela), reaplicação sem mudança (versão nova, `consolidated` continua 1), mudanças (inseridas, atualizadas, apagadas; trigger), remoção de 2 linhas passa e de 11 em 21 trava, `--force`, recusa sem virar versão, `TouchCheck`, `ErrBusy` com o advisory lock tomado |

`make test-real` (difere do padrão, que pede `FILE=...`): baixa os 10
arquivos com `curl` para uma pasta temporária, ou usa
`IANA_REAL_DIR=<pasta>` (com os nomes do servidor, para reaproveitar um
download), e roda `go test -count=1 -tags integration -run 'Real' -v` em
`./internal/parse` e `./internal/store`:

- `TestRealFiles`: parser e `DefaultLimits` nos arquivos inteiros, com
  **zero** linhas descartadas e **zero** avisos;
- `TestRealFilesApply`: aplica, reaplica (zero mudanças) e roda as consultas
  da API: AS61610 → `lacnic`, `2804:8ae0::1` → `lacnic`, RDAP de
  `187.87.28.1` → `lacnic`, `100.64.1.1` (CGNAT) especial com
  `globally_reachable = false`, 4200000001 em `iana_special_asn`.

Precisa de rede (os arquivos da IANA) e de Docker.
