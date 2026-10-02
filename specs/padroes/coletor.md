# Padrão dos coletores

Vale para todos os `collector-<fonte>`. O que é de uma fonte (URLs, checagens
de mudança, mínimos, tabelas, medições) fica em
[`fontes/<fonte>/collector.md`](../fontes/README.md) e, na família RIR, em
[`fontes/rir/collector.md`](../fontes/rir/collector.md). Quando a spec da
fonte diz outra coisa, vale a da fonte — e ela diz por quê.

Dono: sessão principal. Mudar este arquivo muda **todos** os coletores: a
mesma mudança vai para o código de cada um (não há código compartilhado).

## Papel

- Importa a fonte externa para as tabelas `<fonte>_*` do PostgreSQL. Não tem
  servidor HTTP nem porta.
- Roda em laço e **só aplica quando a fonte mudou**. Nunca aplica um dataset
  parcial ou truncado, nem — quando a fonte permite saber (serial ou data no
  arquivo) — mais antigo que o atual.
- Escreve apenas nas tabelas `<fonte>_*`, na sua linha de `jobs` e em tabelas
  temporárias da própria transação. Nunca cria nem altera schema (isso é das
  migrations, ver [../plataforma/postgres.md](../plataforma/postgres.md)).

## Estrutura do código

```
apps/<fonte>/collector/
├── cmd/collector-<fonte>/main.go   config → logger → Postgres → laço (ou --once)
├── internal/
│   ├── buildinfo/    Version, Commit, Date (via -ldflags) e String()
│   ├── config/       opções: padrão → variável de ambiente → argumento; --help
│   ├── fetch/        cliente HTTP: GET condicional, hash publicado, limite, novas tentativas
│   ├── parse/        parser da fonte → dataset + avisos; recusa o arquivo que não bate
│   ├── store/        pgx: última execução aplicada, aplicação transacional, recusas, jobs
│   ├── collector/    uma verificação: checagens → download → conferência → parser → mínimos → aplicação
│   └── rir/          só na família RIR: tudo o que é do RIR (ver fontes/rir)
├── testdata/         recortes reais da fonte (fixtures dos testes)
└── Dockerfile, .dockerignore, docker-compose.yml, .env.example, Makefile, .golangci.yml, README.md, go.mod, go.sum, vendor/
```

Um pacote a mais só quando a fonte pede (ex.: `internal/source` no
`collector-iana`, que baixa 10 arquivos), descrito na spec da fonte. Os
arquivos de build e deploy seguem [../plataforma/docker.md](../plataforma/docker.md).

### `main.go`

1. `config.Load(args, getenv, stderr)`. `--help` escreve a ajuda no stderr e
   sai com 0; opção desconhecida → a ajuda e `collector-<fonte>: flag provided
   but not defined: -<nome>`, saída 2; opção inválida escreve
   `collector-<fonte>: <erro>` no stderr e sai com **2**;
   `--version` imprime `collector-<fonte> <versão> (commit <c>, build <d>)` e
   sai com 0.
2. Logger `log/slog` no stdout (JSON; `text` com `LOG_FORMAT=text`), com
   `app=collector-<fonte>` em toda linha. A primeira linha é `iniciando`, com
   versão, commit, URL(s) da fonte, intervalos, os parâmetros da fonte que o
   app loga (a spec da fonte diz quais) e a `POSTGRES_URL` **sem a senha**
   (`config.Redact`).
3. `SIGINT`/`SIGTERM` cancelam o contexto (encerramento limpo, log
   `encerrando`, saída 0).
4. Postgres: tenta conectar por até **2 minutos** (cada tentativa com 10 s,
   5 s entre elas, log `Postgres indisponível; tentando de novo`) — o banco e
   as migrations podem estar subindo junto. Esgotado: `sem conexão com o
   Postgres`, saída 1. O pool pgx tem no máximo **2 conexões** (fechadas
   depois de 5 min ociosas) e `application_name = collector-<fonte>` quando a
   URL não define outro. Uma `POSTGRES_URL` que o pgx não lê não é erro de
   configuração: cai nas mesmas tentativas e termina em `sem conexão com o
   Postgres`.
5. User-Agent: `USER_AGENT` ou
   `badblock-collector-<fonte>/<versão> (+https://github.com/patrickbrandao/badblock)`.
6. `--once` (ou `--force`): uma verificação e sai com 0 (sucesso) ou 1
   (falha). Sem eles, o laço abaixo.

## Laço

1. Verifica **na subida**.
2. Sucesso: espera `SYNC_INTERVAL`. Falha: espera
   `min(RETRY_INTERVAL, SYNC_INTERVAL)`. Com `LOG_LEVEL=debug`, loga
   `próxima verificação` com a espera.
3. Cada verificação tem no máximo `RUN_TIMEOUT` (contexto com prazo).
4. O sinal de encerramento interrompe a espera.

## Uma verificação

As checagens de mudança e as travas deste padrão **nunca se removem**: são
elas que impedem aplicar um dataset velho, parcial ou truncado. As checagens
de mudança vão **da mais barata para a mais cara**; a primeira que disser
"igual" encerra a verificação como **sem mudança**. Cada fonte
define as suas (hash publicado, GET condicional, SHA-256 do conteúdo e, onde
faz sentido, "arquivo mais antigo que o aplicado"). Regras comuns:

- Os validadores HTTP (`If-None-Match` / `If-Modified-Since`) vêm da **última
  execução aplicada** e só são enviados se a URL for a mesma daquela vez.
- Hash publicado fora do ar ou ilegível: a verificação segue pelas outras
  checagens, sem conferência, com aviso no log (`<hash> publicado
  indisponível; seguindo sem conferência`). O valor `off` (ou `none`,
  `false`) na opção da URL do hash desliga a checagem e a conferência. Vazia,
  a URL do hash é `SOURCE_URL` + a extensão do hash (`.md5`, `.sha256`).
- **Sem mudança**: atualiza só `jobs.last_check_at`; nenhuma linha em
  `<fonte>_run`; log `fonte sem mudança` com o motivo (`reason`).

### Download (`internal/fetch`)

| Item | Valor |
|---|---|
| Conexão / TLS / cabeçalhos da resposta | 15 s / 15 s / 60 s |
| Tempo total | o que resta do `RUN_TIMEOUT` |
| Tamanho máximo | 64 MiB (a fonte pode baixar o limite) |
| Novas tentativas | 2, com 10 s entre elas, em erro de rede, corpo cortado, corpo acima do limite ou HTTP 5xx; 4xx não se repete. "Erro de rede" é qualquer erro do `http.Client`: DNS, conexão, TLS (inclusive certificado vencido ou inválido — não há opção para aceitá-lo) e prazo esgotado |
| Conexões | keep-alive de 30 s, conexão ociosa fechada em 90 s, até 2 ociosas por host |
| Proxy | `HTTP(S)_PROXY` do ambiente |
| Hash publicado | até 4 KiB — maior que isso é o erro `resposta maior que 4096 bytes`, repetido como os outros erros de leitura, e a verificação segue sem conferência —, lido de forma tolerante: formatos BSD (`MD5 (arq) = <hash>`) e GNU (`<hash>  arq`), maiúsculas ou minúsculas; uma página de erro não vira hash. A regra exata de cada fonte está na spec dela |

### Fonte nova: validar antes de aplicar

Na ordem; qualquer uma que falhe **recusa** o dataset inteiro:

1. **Conferência**: com hash publicado, o hash do download tem de bater
   (divergência costuma ser corrida entre a publicação do arquivo e a do hash;
   a próxima verificação resolve).
2. **Parser** da fonte: formato, cabeçalhos e contagens da própria fonte, e
   **no máximo 1%** de linhas/registros descartados.
3. **Sanidade** da fonte (mínimos: `MIN_RECORDS`, `MIN_ASNS` ou regras
   próprias): abaixo disso, arquivo truncado.

### Aplicação (`internal/store`)

Numa **transação só**:

1. `SELECT pg_try_advisory_xact_lock(hashtext('collector-<fonte>'))`: outra
   execução aplicando ao mesmo tempo aborta esta (erro no log, sem linha em
   `<fonte>_run`).
2. `COPY` do dataset para tabelas temporárias `stage_*`.
3. **Trava de remoção**: se o dataset removeria mais de `REMOVAL_THRESHOLD`
   (fração, padrão `0.05`) das linhas atuais de uma tabela, a aplicação é
   recusada. A fonte pode definir um piso de linhas (ex.: IANA).
4. `MERGE` em cada tabela pela chave natural: insere as novas, atualiza só
   as que mudaram (`IS DISTINCT FROM` em cada coluna de dado) e apaga as que
   sumiram. As tabelas espelham o **último dataset aplicado** (sem soft
   delete, sem histórico).
5. Linha `status = 1` em `<fonte>_run` com o que a fonte registra (URL,
   validadores, hashes, cabeçalho, contagens e alterações).
6. `jobs` (ver abaixo): `last_sync_at = last_check_at = NOW()` e
   `consolidated = 0` **só se alguma linha mudou**.

Log `arquivo novo aplicado` (`dataset novo aplicado` na IANA) com a versão,
o hash ou serial e as contagens.

### Recusas e falhas

- Recusa **depois do download** (conferência, parser, sanidade, trava de
  remoção, erro no banco): linha `status = 0` em `<fonte>_run` com a mensagem
  em `error` (e o que se sabe do arquivo); as tabelas ficam como estavam; log
  `verificação falhou`. Se nem a linha de recusa puder ser gravada, o log diz
  `não consegui gravar a falha na tabela de execuções` (família RIR) ou
  `não consegui gravar a falha em <fonte>_run` (cgibr, iana, ripe/asnames).
- Falha **antes do download** (rede, fonte fora, banco fora): só log. A
  próxima tentativa vem em `RETRY_INTERVAL`.
- Avisos do parser vão para `<fonte>_run.warnings` (jsonb, também nas
  recusas depois do parser): os **50** primeiros e, se houver mais, um item
  `... e mais N avisos`. Quando o dataset é aplicado, cada aviso guardado
  também vai para o log (`aviso do parser`).

### `--force`

Implica `--once`. Aplica mesmo sem mudança (ignora as checagens de mudança,
não envia validadores) e ignora a trava de remoção — para reaplicar depois de
mudar o schema, aceitar uma remoção grande legítima ou voltar a um arquivo
anterior de propósito. Conferência do hash, parser e sanidade continuam
valendo.

## Tabela `<fonte>_run`

Uma linha por execução que **baixou um dataset novo** (aplicado ou recusado);
verificações sem mudança não geram linha. Colunas do molde (o DDL exato de
cada fonte está em `fontes/<fonte>/dados.md`):

| Coluna | Conteúdo |
|---|---|
| `uuid` | PK `uuidv7()`; quando `status = 1`, é a **versão do dataset** |
| `status` | `1` aplicado, `0` recusado |
| `forced` | veio de `--force` |
| `url`, `http_status`, `etag`, `last_modified` | do download (validadores da próxima verificação) |
| `sha256` (e `md5` quando a fonte publica MD5) | hash do conteúdo baixado |
| `bytes` | tamanho baixado |
| contagens | do arquivo (ex.: `asns`, `prefixes_v4`) e do cabeçalho, quando a fonte tem |
| alterações | `<tabela>_inserted/_updated/_deleted`; NULL quando `status = 0` |
| `warnings` | jsonb: até 50 avisos e, se houver mais, `... e mais N avisos` |
| `error` | motivo da recusa (NULL quando aplicado) |
| `started_at`, `created_at` | início da verificação e gravação da linha |

Fonte com vários arquivos guarda os dados de download por arquivo numa
coluna jsonb (`iana_run.files`) e as alterações em `changes` (jsonb), em vez
das colunas por arquivo e por tabela.

Índice parcial `ix_<fonte>_run_applied (created_at DESC) WHERE status = 1`:
é por ele que coletor e API acham a última execução aplicada.

**Versão do dataset** = `uuid` da última linha `status = 1`. A API usa essa
versão nas chaves de cache e no ETag; toda aplicação (inclusive `--force`)
gera uma versão nova.

## Contrato com a tabela `jobs`

| Coluna | Quando o coletor grava |
|---|---|
| `app` | `'collector-<fonte>'`, na primeira verificação bem-sucedida (upsert) |
| `last_check_at` | toda verificação bem-sucedida, com ou sem mudança |
| `last_sync_at` | todo dataset novo aplicado |
| `consolidated` | `0` quando a aplicação alterou linhas; **nunca** grava `1` |

Quem grava `consolidated = 1` é a consolidação central (fase 2). DDL em
[../plataforma/postgres.md](../plataforma/postgres.md#tabela-jobs).

## Configuração comum

Ordem: padrão → variável de ambiente → argumento de linha de comando;
variável vazia vale o padrão; `--help` lista cada opção com o nome da
variável ([../projeto/convencoes.md](../projeto/convencoes.md#configuração)).

| Variável | Argumento | Padrão | Uso |
|---|---|---|---|
| `POSTGRES_URL` | `--postgres-url` | — (obrigatória) | `postgres://postgres:<senha>@badblock-postgres:5432/badblock?sslmode=disable` |
| `SOURCE_URL` | `--source-url` | da fonte | arquivo da fonte (a IANA usa outras, ver a spec dela) |
| `SYNC_INTERVAL` | `--sync-interval` | `1h` (a fonte pode mudar) | entre verificações bem-sucedidas |
| `RETRY_INTERVAL` | `--retry-interval` | `5m` | depois de uma verificação que falhou |
| `RUN_TIMEOUT` | `--run-timeout` | `10m` | limite de uma verificação |
| `REMOVAL_THRESHOLD` | `--removal-threshold` | `0.05` | fração máxima removida sem `--force` |
| `USER_AGENT` | `--user-agent` | `badblock-collector-<fonte>/<versão> (+https://github.com/patrickbrandao/badblock)` | |
| `LOG_LEVEL` | `--log-level` | `info` | `debug` mostra a próxima espera |
| `LOG_FORMAT` | `--log-format` | `json` | `json` ou `text` |
| — | `--once` | | uma verificação e sai |
| — | `--force` | | ver acima |
| — | `--version` | | versão e sai |

Validação comum: durações (`SYNC_INTERVAL`, `RETRY_INTERVAL`,
`RUN_TIMEOUT`) > 0 pelo `time.ParseDuration` (`--<opção> inválido: "<v>"`);
`REMOVAL_THRESHOLD` de 0 a 1 (`--removal-threshold inválido (0 a 1): "<v>"`);
mínimos inteiros ≥ 0; URLs da fonte com `http://` ou `https://`
(`--<opção> precisa ser http(s): "<v>"`); `LOG_LEVEL` (`debug`, `info`,
`warn`, `error`) e `LOG_FORMAT` (`json`, `text`) em qualquer caixa; sem
`POSTGRES_URL`, `defina POSTGRES_URL (ou --postgres-url)` (dispensada com
`--version`); argumento solto, `argumento inesperado: <arg>`.

Opções da fonte (URL do hash, mínimos, URLs extras) ficam na spec dela. Onde
uma opção nova aparece (`--help`, compose, `.env.example` do app e da raiz):
[../projeto/convencoes.md](../projeto/convencoes.md#configuração).

No compose, cada variável do app ganha o prefixo `COLLECTOR_<FONTE>_` no
`.env` (ex.: `COLLECTOR_CGIBR_SYNC_INTERVAL`); `POSTGRES_URL`,
`POSTGRES_PASSWORD` e `LOG_LEVEL` são comuns. Opção que o compose passa vazia
vale o padrão do app.

## Container e operação

- Imagem `tmsoftbrasil/badblock-collector-<fonte>`, distroless, não-root,
  **sem porta e sem `HEALTHCHECK`** (não há HTTP): a saúde se vê em
  `jobs.last_check_at`, no `/meta` da API da fonte e nos logs.
- Container `badblock-collector-<fonte>`, só na rede `badblock`, com
  `traefik.enable=false`.

```bash
make -C apps/<fonte>/collector once    # verificação agora, no container no ar
make -C apps/<fonte>/collector force   # reaplica o dataset atual
make -C apps/<fonte>/collector logs
```

Alvos do `Makefile` e build da imagem:
[../plataforma/docker.md](../plataforma/docker.md#makefile-dos-apps).

## Testes

| Camada | Onde | O quê |
|---|---|---|
| Unitários (`make test`) | `parse`, `fetch`, `config`, `collector` | parser com os recortes de `testdata/`; `fetch` com `httptest`; `collector` com store e fonte falsos (sem mudança, aplicação, recusas, `--force`) |
| Integração (`make test-int`) | `store` | PG18 descartável (testcontainers) com as migrations reais de `database/postgres/central/` e `database/postgres/<fonte>/`: aplicação, idempotência, trava de remoção, `jobs`, recusa |
| Arquivo real (`make test-real`) | `parse`, `store` | nas fontes que têm (os cinco RIRs, a IANA e o rootzone): o arquivo inteiro do dia, com tempos. RIR: `FILE=<arquivo baixado com curl>`, pela variável `<FONTE>_REAL_FILE`; IANA: sem `FILE`, baixa os 10 arquivos sozinha ou usa `IANA_REAL_DIR=<pasta>`; rootzone: sem `FILE`, baixa o do dia. roothints: `ROOTHINTS_REAL_FILE=... make test-int`; rootanchors: o arquivo inteiro já é a fixture; anatel/pst: `FILE=<zip ou csv>`, pela variável `ANATEL_PST_REAL_FILE` |

Detalhes e comandos em [../processos/testes.md](../processos/testes.md).
