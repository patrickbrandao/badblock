# Coletor do modelo RIR

Vale para os cinco `collector-<rir>` (o modelo `collector-lacnic` e os
clones). Leia antes [../../padroes/coletor.md](../../padroes/coletor.md): aqui
fica só o que a família faz a mais ou de outro jeito. Os valores de cada RIR
estão em [README.md](README.md#parâmetros-dos-coletores) e no
`specs/fontes/<rir>/collector.md`; o formato e o parser, em
[formato.md](formato.md); as tabelas, em [dados.md](dados.md).

## Código

Os pacotes do [padrão](../../padroes/coletor.md#estrutura-do-código) mais
`internal/rir` ([README.md](README.md#organização-do-código)). Download,
novas tentativas, conexões HTTP e pool pgx têm os valores do padrão
([download](../../padroes/coletor.md#download-internalfetch),
[`main.go`](../../padroes/coletor.md#maingo)): em `main.go`, `fetch.Fetcher`
com `MaxBytes` 64 MiB, `Retries` 2 e `RetryDelay` 10 s. A mais, na família:

- só `200` é sucesso (e `304`, no arquivo); outro status vira o erro
  `HTTP <código>`, repetido só se for 5xx; corpo acima do limite é o erro
  `resposta maior que <n> bytes`;
- `store.Open` valida a conexão com `SELECT 1`;
- `collector.Options`: `URL`, `MD5URL` (vazio = sem hash publicado),
  `Registry` (`rir.Registry`), `MinRecords`, `RemovalThreshold`.

A linha `iniciando` do log traz `version`, `commit`, `source`, `md5` (a URL
do hash; vazia com `off`), `interval`, `retry`, `min_records` e `postgres`.

O download fica inteiro em memória (até os 64 MiB do limite), e o parser monta
o dataset inteiro antes da aplicação: o pico de memória de uma aplicação cresce
com o arquivo (medições no `collector.md` de cada RIR).

## Checagens de mudança

Da mais barata para a mais cara; a primeira que diz "igual" encerra a
verificação como sem mudança (só `jobs.last_check_at`; log
`fonte sem mudança` com o `reason`).

| # | Checagem | Custo | `reason` |
|---|---|---|---|
| 1 | hash de `SOURCE_MD5_URL` igual ao `md5` da última execução aplicada | o `.md5` (~75 bytes) | `md5 publicado igual ao último aplicado` |
| 2 | GET condicional com o `etag`/`last_modified` da última aplicada → `304` | só cabeçalhos | `HTTP 304` |
| 3 | SHA-256 do download igual ao `sha256` da última aplicada | o download | `conteúdo igual ao último aplicado` |
| 4 | cabeçalho **mais antigo** que o do último aplicado (abaixo) | download, conferência e parser | `arquivo mais antigo que o aplicado (enddate 2026-09-24 < 2026-09-25)` ou `(serial 20260926 < 20260927)` |

- O `.md5` é pedido antes do arquivo em toda verificação com `SOURCE_MD5_URL`
  ligada (inclusive com `--force`, para a conferência). Fora do ar ou ilegível: log
  `md5 publicado indisponível; seguindo sem conferência` (`url`, `err`), e a
  verificação segue pelas checagens 2 a 4, sem conferência.
- A checagem 4 vem **depois** da conferência e do parser: uma cópia velha com
  `.md5` divergente ou quebrada é recusada (linha `status = 0`); só a que
  passa nos dois conta como sem mudança.

### Arquivo mais antigo que o aplicado

Um espelho ou um servidor com cache pode servir uma cópia velha. Sem
`--force` e com uma execução aplicada, o arquivo é **mais antigo** quando:

1. os dois `enddate` existem e são diferentes, e o novo é anterior — nesse
   caso só o `enddate` decide (`enddate` mais novo com serial menor é
   aplicado);
2. senão (mesmo `enddate`, ou um dos dois sem data), o `serial` novo é menor,
   comparado **só** quando os dois têm só dígitos e o mesmo tamanho (aí a
   comparação de texto é a numérica). Um RIR que troque o formato do serial
   (ex.: data → época Unix) não trava a coleta: serial de outro tamanho ou não
   numérico não é comparável, e o arquivo segue.

O arquivo mais antigo **não é aplicado e não é falha**: conta como sem
mudança, sem linha em `<rir>_run`, com o aviso no log
`arquivo mais antigo que o aplicado; ignorado (espelho ou servidor com cópia velha?)`
(`serial`, `end_date`, `applied_serial`, `applied_end_date`, `md5`); os avisos
do parser desse arquivo se perdem. Enquanto a cópia velha estiver no ar, cada
verificação a baixa de novo (o MD5 e os validadores não são os do aplicado).

## Validações do arquivo novo

Na ordem, depois das checagens 1 a 3 (regras gerais no
[padrão](../../padroes/coletor.md#fonte-nova-validar-antes-de-aplicar)):

| # | Validação | Recusa (`error` em `<rir>_run`) |
|---|---|---|
| 1 | conferência: com hash publicado, o MD5 do download tem de ser igual | `md5 divergente: publicado <hash>, baixado <hash> (arquivo e .md5 publicados em momentos diferentes?)` |
| 2 | parser ([formato.md](formato.md#regras-do-parser-internalparse)) | `parser: <mensagem do parser>` |
| 3 | arquivo mais antigo (acima) | — (sem mudança) |
| 4 | `MIN_RECORDS`: `Records()` (registros aceitos de ASN + IPv4 + IPv6, antes da divisão, sem repetidos) ≥ `MIN_RECORDS` | `só <n> registros no arquivo (mínimo <m>): arquivo truncado?` |
| 5 | aplicação (abaixo): trava de remoção e erros do banco | a mensagem do erro |

Quando um `.md5` atrasado ou de outra cópia faz a conferência falhar — e
quando não faz — está em [formato.md](formato.md#arquivo-md5). A linha de
recusa grava o que se sabe: o download sempre; cabeçalho, contagens e avisos
quando o parser aceitou (recusas 4 e 5).

## Aplicação

Numa transação ([padrão](../../padroes/coletor.md#aplicação-internalstore)):

1. `SELECT pg_try_advisory_xact_lock(hashtext('collector-<rir>'))`. Sem o
   lock: erro `outra execução do collector-<rir> está aplicando dados`, só no
   log, sem linha em `<rir>_run` (concorrência não é defeito do arquivo).
2. Tabelas temporárias carregadas com `COPY` (texto vazio e data zero viram
   NULL), seguidas de `ANALYZE stage_asn; ANALYZE stage_prefix`:

   ```sql
   CREATE TEMP TABLE stage_asn (asn_start bigint PRIMARY KEY, asn_count bigint NOT NULL,
       cc text, reg_date date, status text NOT NULL, opaque_id text) ON COMMIT DROP;
   CREATE TEMP TABLE stage_prefix (prefix cidr PRIMARY KEY, cc text, reg_date date,
       status text NOT NULL, opaque_id text, record_start inet NOT NULL,
       record_value bigint NOT NULL) ON COMMIT DROP;
   ```

3. **Trava de remoção** (sem `--force`), por tabela, primeiro a de ASN:
   removidas = linhas atuais cuja chave (`asn_start`, `prefix`) não está no
   stage; recusa se removidas / atuais > `REMOVAL_THRESHOLD` numa tabela não
   vazia (a primeira carga nunca trava):
   `o arquivo removeria <r> de <c> registros de ASN (<p>%, limite <l>%); use --force se for legítimo`
   (ou `blocos`).
4. `MERGE` nas duas tabelas, **independentes**, com `RETURNING merge_action()`
   para contar `INSERT`, `UPDATE` e `DELETE`:
   - `<rir>_asn` `ON t.asn_start = s.asn_start`, atualizando quando
     `(asn_count, cc, reg_date, status, opaque_id)` `IS DISTINCT FROM` o stage;
   - `<rir>_prefix` `ON t.prefix = s.prefix`, atualizando quando
     `(cc, reg_date, status, opaque_id, record_start, record_value)`
     `IS DISTINCT FROM` o stage;
   - `WHEN NOT MATCHED BY TARGET` insere; `WHEN NOT MATCHED BY SOURCE` apaga.
     Erros: `merge <rir>_asn: ...`, `merge <rir>_prefix: ...`.
5. Linha `status = 1` em `<rir>_run` ([dados.md](dados.md#rir_run)); o `uuid`
   dela é a versão nova.
6. `jobs`, com `$2` = alguma das seis contagens > 0:

   ```sql
   INSERT INTO jobs (app, last_sync_at, last_check_at, consolidated) VALUES ($1, NOW(), NOW(), 0)
   ON CONFLICT (app) DO UPDATE SET last_sync_at = NOW(), last_check_at = NOW(),
       consolidated = CASE WHEN $2 THEN 0 ELSE jobs.consolidated END;
   ```

   Um arquivo novo com o mesmo conteúdo lógico (só o cabeçalho mudou) gera
   versão nova e não pede consolidação.

Sem mudança, `jobs` recebe só
`INSERT INTO jobs (app, last_check_at) VALUES ($1, NOW()) ON CONFLICT (app) DO UPDATE SET last_check_at = NOW()`.
A linha de recusa é gravada fora da transação, com um contexto que não expira
(`context.WithoutCancel`): vale mesmo depois do `RUN_TIMEOUT` ou de um sinal.

## Logs

| Mensagem | Nível | Campos |
|---|---|---|
| `fonte sem mudança` | info | `reason`, `version`, `ms` |
| `arquivo novo aplicado` | info | `version`, `serial`, `md5`, `asn_records`, `ipv4_records`, `ipv6_records`, `prefixes_v4`, `prefixes_v6`, `asn_inserted`, `asn_updated`, `asn_deleted`, `prefix_inserted`, `prefix_updated`, `prefix_deleted`, `warnings` (itens guardados em `<rir>_run.warnings`, até 51 — não o total de avisos), `forced`, `ms` |
| `aviso do parser` | warn | `warning`: uma linha por aviso guardado, logo depois de `arquivo novo aplicado` |
| `verificação falhou` | error | `err`, `ms` |

- Avisos do parser: como no
  [padrão](../../padroes/coletor.md#recusas-e-falhas) — os 50 primeiros e
  `... e mais N avisos` em `<rir>_run.warnings`, também nas recusas depois do
  parser (`MIN_RECORDS`, trava, banco); no log, só os do arquivo aplicado. Os
  de um arquivo mais antigo que o aplicado se perdem (não há linha nem log).
- Falhas que só vão para o log, sem linha em `<rir>_run`:
  `lendo o último arquivo aplicado: ...`, `download: ...` (rede, HTTP que não
  seja 200 nem 304, corpo acima do limite; os erros repetíveis, depois das
  novas tentativas), `jobs: ...` (ao registrar uma verificação sem mudança) e
  o lock ocupado.

## `--force`

Além do [padrão](../../padroes/coletor.md#--force): pula as checagens 1, 3 e 4
e não manda validadores (não há `304`); ainda baixa o `.md5` e confere o
download; ignora a trava de remoção; parser e `MIN_RECORDS` continuam valendo.
A linha sai com `forced = true`, e a versão é nova mesmo que nada mude.

## Opções da família

Além das [comuns](../../padroes/coletor.md#configuração-comum):

| Variável | Argumento | Padrão | Uso |
|---|---|---|---|
| `SOURCE_URL` | `--source-url` | `rir.DefaultSourceURL` | arquivo delegated-extended do RIR; `http://` ou `https://` |
| `SOURCE_MD5_URL` | `--source-md5-url` | vazio = `SOURCE_URL` + `.md5` | hash publicado; `off`, `none` ou `false` (maiúsculas ou minúsculas) desligam a checagem 1 e a conferência |
| `MIN_RECORDS` | `--min-records` | `rir.DefaultMinRecords` | inteiro ≥ 0; menos registros aceitos = arquivo truncado |

Erros de configuração (`collector-<rir>: <erro>` no stderr, saída 2):
`defina POSTGRES_URL (ou --postgres-url)` (não vale com `--version`),
`--source-url precisa ser http(s): "<url>"` (idem `--source-md5-url`),
`--min-records inválido: "<v>"`, `--sync-interval inválido: "<v>"` (idem
`--retry-interval` e `--run-timeout`: duração Go maior que zero),
`--removal-threshold inválido (0 a 1): "<v>"`, `--log-level inválido: "<v>"`
(`debug`, `info`, `warn`, `error`), `--log-format inválido: "<v>"` e
`argumento inesperado: <arg>`. O `--help` lista as opções em ordem alfabética,
cada uma com `env <VARIÁVEL>, padrão <valor>`, e termina em `--once`,
`--force`, `--version` e `-h, --help` (saída real em
[../lacnic/collector.md](../lacnic/collector.md#--help)).

## Compose e `.env`

- `docker-compose.yml` do app (molde em
  [../../plataforma/docker.md](../../plataforma/docker.md#coletor)):
  `SOURCE_URL: ${COLLECTOR_<RIR>_SOURCE_URL:-}`,
  `SOURCE_MD5_URL: ${COLLECTOR_<RIR>_SOURCE_MD5_URL:-}` e
  `MIN_RECORDS: ${COLLECTOR_<RIR>_MIN_RECORDS:-}` vêm vazios quando o `.env`
  não os define, e o app usa o padrão do `rir.go` (o valor fica num lugar
  só). O comentário do cabeçalho do compose,
  `# SOURCE_URL e MIN_RECORDS vazios valem o padrão do app (internal/rir).`,
  não cita `SOURCE_MD5_URL`, que também vem vazio (igual nos cinco coletores,
  conferido em 2026-09-29). `RUN_TIMEOUT` e `USER_AGENT` não passam pelo
  compose.
- Variáveis no `.env`: `COLLECTOR_<RIR>_TAG` (`latest`),
  `COLLECTOR_<RIR>_SYNC_INTERVAL` (`1h`), `COLLECTOR_<RIR>_RETRY_INTERVAL`
  (`5m`), `COLLECTOR_<RIR>_SOURCE_URL`, `COLLECTOR_<RIR>_SOURCE_MD5_URL` e
  `COLLECTOR_<RIR>_MIN_RECORDS` (vazias) e
  `COLLECTOR_<RIR>_REMOVAL_THRESHOLD` (`0.05`).
- `.env.example` do app: `POSTGRES_PASSWORD=`, a `POSTGRES_URL` comentada,
  essas variáveis (as três vazias com o comentário de que valem o padrão do
  app) e `LOG_LEVEL=info`. No `.env.example` da raiz, um bloco por RIR que
  começa com
  `# collector-<rir>: delegações da <Title> (delegated-extended). MIN_RECORDS vazio = <n>.`
  (`do RIPE NCC`).

## Testes

Camadas do [padrão](../../padroes/coletor.md#testes); o que cada arquivo cobre:

| Arquivo | Casos |
|---|---|
| `config_test.go` | padrões (URL e `.md5` do `rir.go`, `1h`, `5m`, `10m`, `MIN_RECORDS`, `0.05`, `info`, `json`); argumento > ambiente > padrão; variável vazia = padrão; `SOURCE_MD5_URL` `off`/`OFF` → sem hash, URL própria, vazia → `SOURCE_URL` + `.md5`; `--force` implica `--once`; `--version` sem `POSTGRES_URL`; `--help` com todas as opções e variáveis; valores inválidos; `Redact` |
| `fetch_test.go` | `ParseMD5`: BSD com e sem `\n`, GNU, GNU com `*`, só o hash, maiúsculas; recusa HTML, vazio, linha SHA256, hash curto. `.md5` com o User-Agent; conteúdo sem hash não se repete; GET condicional (200, depois `304` por `ETag` e por `Last-Modified`); MD5 e SHA-256 do corpo; 5xx repetido (3 tentativas) e desistência; 404 não se repete; limite de tamanho |
| `parse_test.go` | o recorte do RIR (`TestParseSample`); os `formats/` (`TestParseOtherRIRFormats`); registry errado; arquivos quebrados (vazio, sem cabeçalho, versão 3, registry errado, cabeçalho curto, contagem, resumo divergente, truncado, resumo ilegível, sem registros, mais de 1% ruim); 0,5% ruim passa; regras de registro (normalização, bits de host, repetidos, data inválida, status, país, faixas que estouram, prefixo inválido, vazios, extensões); comentários, BOM e CRLF; sobreposição e resumo faltando; `SplitIPv4` ([formato.md](formato.md#ipv4-divisão-em-cidrs) e as recusas: 0, estouro, 2³² + 1, IPv6) |
| `collector_test.go` | store e fonte falsos com o recorte: primeira carga; MD5 igual não baixa; `304` com os validadores; validadores só para a mesma URL; conteúdo igual; `.md5` desligado; `--force` aplica o mesmo arquivo; MD5 divergente, erro do parser, poucos registros (`MinRecords` = registros do recorte + 1) e erro da aplicação gravados; `ErrBusy` não gravado; arquivo mais antigo ignorado (3 casos; com `--force`, aplica); mais novo ou não comparável aplica (4 casos no modelo; o `collector-arin` tem um quinto: um aplicado com serial em segundos contra o arquivo em ms); segunda verificação sem mudança |
| `store_integration_test.go` | PG18 (`postgres:18-trixie`, banco `badblock`, usuário `postgres`) com o `migrate:up` de `central/` e `<rir>/`. `TestApplyLifecycle`: carga do recorte (contagens, `jobs`, `family`, `LastApplied`, colunas do cabeçalho, as consultas da API, recursos de um titular, NULLs dos reservados); `consolidated = 1` e o mesmo conteúdo → 0 alterações, versão nova, flag intacta; mudanças (ASN alocado, removido e novo; bloco com titular novo, bloco removido e um IPv4 não-CIDR em 2 blocos) com as contagens exatas, `consolidated = 0` e `updated_at`; trava de remoção e `--force`; recusa gravada que não vira versão; `TouchCheck`. `TestApplyBusy`: lock tomado por outra transação → `ErrBusy`, sem linha |

### Arquivo real (`make test-real`)

```bash
curl -o /tmp/delegated <DefaultSourceURL do RIR>
make test-real FILE=/tmp/delegated
```

Sem `FILE`, o alvo diz
`use: make test-real FILE=/caminho/do/delegated-...-extended-latest (baixe com curl)`
e sai com 1; com ele, roda
`<RIR>_REAL_FILE=<caminho absoluto> go test -count=1 -tags integration -run RealFile -v ./internal/parse/ ./internal/store/`.

- `TestParseRealFile`: loga tempo, cabeçalho, contagens, descartes e avisos;
  falha com qualquer descarte ou com menos registros que `DefaultMinRecords`.
- `TestApplyRealFile` (PG18 descartável): a primeira carga insere tudo; a
  reaplicação não muda nada; as tabelas têm o que o dataset tem; nenhuma faixa
  de ASN sobreposta; depois de `ANALYZE`, o `EXPLAIN` das consultas por ASN,
  por IP e por titular usa índice. Os tempos logados viram as medições do
  `collector.md` do RIR.

## Clonar o coletor para outro RIR

`<rir>` é o nome da fonte (o `registry` do arquivo, ex.: `ripencc`), `<RIR>` o
mesmo em maiúsculas e `<Title>` o nome nos textos (`RIPE NCC`).

1. **Specs**: meça o arquivo real e crie `specs/fontes/<rir>/` com
   `README.md`, `fonte.md`, `dados.md` e `collector.md`, que apontam este
   modelo e trazem só o que é do RIR (URLs, publicação, `.md5`, fatos medidos
   com data, o recorte, `MIN_RECORDS` e o porquê, medições); o `api.md` é do
   sub-agente da API. Acrescente o RIR em
   [Parâmetros dos coletores](README.md#parâmetros-dos-coletores) e na
   [comparação](formato.md#comparação-entre-os-rirs).
2. **Cópia**, sem `bin/`, e troca de nomes nos arquivos copiados
   (module Go, nomes SQL, `collector-lacnic`, `COLLECTOR_LACNIC_*`,
   `LACNIC_REAL_FILE`):

   ```bash
   mkdir -p apps/<rir>
   cp -R apps/lacnic/collector apps/<rir>/collector
   mv apps/<rir>/collector/cmd/collector-lacnic apps/<rir>/collector/cmd/collector-<rir>
   # em cada arquivo copiado: sed 's/lacnic/<rir>/g; s/LACNIC/<RIR>/g'
   ```

3. **Confira à mão**:
   - `internal/rir/rir.go`: `Title`, `DefaultSourceURL` (host e caminho mudam:
     a APNIC não tem `/pub`, o RIPE NCC está em `ftp.ripe.net`) e
     `DefaultMinRecords` (≈ metade dos registros medidos), com o total e a
     data no comentário;
   - textos que o `sed` estraga: os que citam os cinco RIRs (comentário do
     pacote `internal/parse`) e o artigo (`da LACNIC` → `do RIPE NCC`) no
     `Dockerfile` (label `description`) e no `docker-compose.yml`;
   - `testdata/delegated-extended-sample.txt`: recorte real do RIR
     ([formato.md](formato.md#fixtures)) e os testes que citam os números
     dele: `TestParseSample`; em `collector_test.go`, o comentário de
     `sample()`, `TestFirstRunApplies`, seriais e datas de
     `TestOlderFileIsIgnored` e `TestNewerOrIncomparableFileApplies` (no
     formato de serial do RIR) e o `MinRecords` de
     `TestTooFewRecordsIsRecorded`; em `TestApplyLifecycle`, contagens,
     cabeçalho, o ASN, o IP e o titular das consultas e os registros
     alterados e removidos;
   - `testdata/formats/`: tire `<rir>.txt`, ponha `lacnic.txt`, troque o caso
     do próprio RIR pelo da LACNIC em `TestParseOtherRIRFormats` — o
     `enddate` do `lacnic.txt` é 20260925, e não 20260928 como nos outros
     quatro, então cada caso ganha o campo `end` (é o que os quatro clones
     fazem) — e aponte `TestParseWrongRegistry` para um recorte que o clone
     tem.
4. **Migration** `database/postgres/<rir>/20260929000000_<rir>.sql`: a do
   lacnic com o nome trocado; mude só o texto (URL no cabeçalho, notas e
   exemplos do RIR nos `COMMENT ON`), nunca o DDL ([dados.md](dados.md#migration)).
5. **Sub-agente** `.claude/agents/collector-<rir>.md`, no molde do
   `collector-lacnic`.
6. **Integração na raiz** (sessão principal):
   [../../processos/nova-fonte.md](../../processos/nova-fonte.md#6-integração-na-raiz),
   com o bloco `COLLECTOR_<RIR>_*` do `.env.example` descrito acima.
7. **Conferir**: `make test test-int lint vet` no app,
   `make test-real FILE=...` com o arquivo do dia,
   `make -C database/postgres test` e `make specs-check`.
