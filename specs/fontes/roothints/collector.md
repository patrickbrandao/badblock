# roothints — coletor (`collector-roothints`)

O que o `collector-roothints` faz de diferente ou a mais que o
[padrão dos coletores](../../padroes/coletor.md). O resto — `main.go`, laço,
download, recusas, `--force`, contrato com `jobs`, configuração comum,
container — segue o padrão; as mensagens de log exatas estão em
[Recusas, erros e logs](#recusas-erros-e-logs). Formato e parser:
[fonte.md](fonte.md); tabelas: [dados.md](dados.md).

## Valores desta fonte

| Item | Valor |
|---|---|
| App, binário | `collector-roothints` (`/collector-roothints` na imagem) |
| Module Go | `github.com/patrickbrandao/badblock/apps/roothints/collector` |
| Imagem, container | `tmsoftbrasil/badblock-collector-roothints`, `badblock-collector-roothints` |
| Descrição da imagem (`org.opencontainers.image.description`) | `Importa os servidores raiz do DNS (named.root da InterNIC, root hints) para o PostgreSQL do BadBlock` |
| Linha em `jobs` | `app = 'collector-roothints'` |
| Trava de concorrência | `pg_try_advisory_xact_lock(hashtext('collector-roothints'))` |
| Tabelas | `roothints_server`, `roothints_run`; temporária `stage_server` |
| Pool pgx | no máximo 2 conexões, fechadas depois de 5 min ociosas; `application_name = collector-roothints` (se a URL não trouxer outro) |
| User-Agent | `badblock-collector-roothints/<versão> (+https://github.com/patrickbrandao/badblock)` |
| Tamanho máximo do download | **1 MiB** (`MaxBytes: 1 << 20`; o arquivo tem 3.315 bytes) — o `.md5`, 4 KiB, como no padrão |
| Fixture | `testdata/named.root` e `testdata/named.root.md5` ([fonte.md](fonte.md#fixture)) |
| Arquivo real nos testes | variável `ROOTHINTS_REAL_FILE` |

Os pacotes são os do padrão (`buildinfo`, `config`, `fetch`, `parse`,
`store`, `collector`), sem pacote a mais. `internal/fetch` tem o `.md5`
publicado (`PublishedMD5`, `ParseMD5`), no molde do `collector-lacnic`, e o
`IfNoneMatch` do ETag `-gzip`, copiado do `collector-rootzone`;
`internal/collector` tem o `SerialLess` (RFC 1982), também copiado do
`collector-rootzone`.

## Checagens de mudança

Da mais barata para a mais cara; a primeira que disser "igual" encerra a
verificação como sem mudança:

| # | Checagem | "Igual" quando | `reason` no log | Custo |
|---|---|---|---|---|
| 1 | hash de `SOURCE_MD5_URL` | igual ao `md5` da última execução aplicada | `md5 publicado igual ao último aplicado` | o `.md5` (33 bytes) |
| 2 | GET condicional com `If-None-Match`/`If-Modified-Since` da última execução aplicada | HTTP 304 | `HTTP 304` | só cabeçalhos |
| 3 | SHA-256 do arquivo baixado | igual ao `sha256` da última execução aplicada | `conteúdo igual ao último aplicado` | download (3,3 KB) |
| 4 | serial da zona raiz do cabeçalho, depois da conferência do MD5 e do parser | menor que o `zone_serial` da última execução aplicada (RFC 1982) | `arquivo mais antigo que o aplicado (serial 2026092400 < 2026092401)` | download e parser |

- **Nunca remova essas checagens.**
- O `.md5` vem primeiro porque o `Last-Modified`/ETag do `named.root` muda a
  cada publicação da zona raiz, cerca de 2 vezes por dia, sem o conteúdo
  mudar ([fonte.md](fonte.md#servidor-http-medido-em-2026-09-30)): o GET
  condicional sozinho baixaria o arquivo toda vez que isso acontece. Com o
  `.md5`, quase toda verificação termina na checagem 1.
- O `.md5` é pedido em toda verificação com `SOURCE_MD5_URL` ligado (também
  com `--force`, para a conferência). Fora do ar ou ilegível: log
  `md5 publicado indisponível; seguindo sem conferência` (`url`, `err`), e a
  verificação segue pelas checagens 2 a 4, sem conferência. Aí, depois de
  cada regravação do servidor, a checagem 2 falha (validadores novos) e a 3
  decide: um download de 3,3 KB, sem linha em `roothints_run`.
- "Sem mudança" não grava linha em `roothints_run`, então os validadores
  continuam os da última execução aplicada.
- Os validadores só são enviados se a URL da última execução aplicada for a
  mesma; o MD5 e o SHA-256 valem para qualquer URL.
- **ETag com `-gzip`** (checagem 2): o transporte do Go pede gzip, e o
  Apache da InterNIC, quando comprime, devolve o ETag com o sufixo `-gzip`
  e não o reconhece no `If-None-Match` — responderia 200 e, como o
  `If-None-Match` presente faz o servidor ignorar o `If-Modified-Since`, a
  checagem 2 nunca daria 304
  ([fonte.md](fonte.md#compressão-e-o-etag--gzip-medido-em-2026-09-30)).
  Por isso `fetch.IfNoneMatch` envia o ETag guardado com `-gzip` também sem
  o sufixo: `If-None-Match: "x-gzip", "x"` (e `W/"x-gzip", W/"x"` no fraco);
  o ETag sem o sufixo vai como está. Hoje o `named.root` sai sem compressão
  (ETag sem sufixo) e isto não muda nada; o `named.cache`, cópia dele no
  mesmo servidor, sai comprimido, então a compressão pode passar a valer
  para o `named.root` sem aviso. `etag` em `roothints_run` guarda o ETag
  como veio, com o sufixo.
- `--force` não envia validadores e pula as checagens 1, 3 e 4 (mas ainda
  confere o `.md5`).

### Arquivo mais antigo

O serial de `related version of root zone:` (`AAAAMMDDnn`) é comparado com
o `zone_serial` da última execução aplicada pela **aritmética de seriais da
RFC 1982** (`collector.SerialLess`, 32 bits: `a < b` quando são diferentes
e `b − a` módulo 2³² é menor que 2³¹; com o formato de data da raiz é a
comparação numérica comum). Não compara na primeira carga nem quando a
última aplicada não tem serial.

- **Menor**: **não é aplicado e não é falha**; conta como sem mudança, sem
  linha em `roothints_run` e só com `jobs.last_check_at`, com o aviso no log
  `arquivo mais antigo que o aplicado; ignorado (espelho ou servidor com
  cópia velha?)` (`serial`, `applied_serial`, `md5`) — como no
  `collector-rootzone` ([../rootzone/collector.md](../rootzone/collector.md#arquivo-mais-antigo-e-serial-igual))
  e na família RIR ([../rir/collector.md](../rir/collector.md#arquivo-mais-antigo-que-o-aplicado)).
  Motivo: o servidor manda `max-age=420` e há um cache no caminho
  ([fonte.md](fonte.md#servidor-http-medido-em-2026-09-30)), que pode servir
  a versão anterior por alguns minutos depois de uma publicação. Se fosse
  recusa, cada verificação enquanto a cópia velha estivesse no ar a baixaria
  de novo (o MD5 dela não é o do aplicado) e gravaria mais uma linha
  `status = 0` igual. Os avisos do parser desse arquivo se perdem.
- **Igual, com conteúdo diferente** (o SHA-256 já disse que mudou): aplica
  normalmente, sem aviso (correção do arquivo sem troca de serial; o serial
  é o da zona raiz na data da atualização do `named.root`, não muda a cada
  zona nova).
- **Maior**: aplica.
- `--force` pula a comparação: é o jeito de voltar a um arquivo anterior de
  propósito.

## Validações antes de aplicar

Na ordem; qualquer uma que falhe grava a recusa (`status = 0`) em
`roothints_run`:

| # | Validação | Mensagem |
|---|---|---|
| 1 | **conferência**: com hash publicado, o MD5 do download tem de ser igual | `md5 divergente: publicado <hash>, baixado <hash> (arquivo e .md5 publicados em momentos diferentes?)` |
| 2 | **parser**: as regras de [fonte.md](fonte.md#regras-do-parser); nenhuma linha é descartada | `parser: <mensagem>` |
| 3 | **`MIN_SERVERS`** (padrão `13`) | `só <n> servidores no arquivo (mínimo <m>): arquivo truncado?` |
| 4 | **trava de remoção**, já na aplicação | `o arquivo removeria <r> de <c> servidores (<x.y>%, limite <z.w>%); use --force se for legítimo` |

- Entre o parser e o `MIN_SERVERS` roda a checagem 4 (arquivo mais antigo,
  [acima](#arquivo-mais-antigo)): um serial menor encerra a verificação como
  sem mudança, sem recusa.
- **`MIN_SERVERS` e a trava de remoção com 13 linhas**: o arquivo tem
  exatamente 13 servidores, então o mínimo padrão recusa qualquer arquivo com
  menos, e a trava de 5% recusa qualquer remoção (1 de 13 = 7,7%) mesmo com
  `MIN_SERVERS` rebaixado. Um servidor raiz aposentado ou renomeado (renomear
  é remover um e inserir outro) só entra com `--force` depois de conferido.
  Trocas de endereço (IPv4 do `b` em 2023, por exemplo) são atualizações e
  passam sem `--force`.

## Aplicação

Tudo numa transação (`internal/store`, `Store.Apply`):

```sql
SELECT pg_try_advisory_xact_lock(hashtext('collector-roothints'));  -- false: ErrBusy

CREATE TEMP TABLE stage_server (
    name      text    PRIMARY KEY,
    letter    text    NOT NULL,
    ipv4      inet,
    ipv6      inet,
    ns_ttl    integer NOT NULL,
    ipv4_ttl  integer,
    ipv6_ttl  integer,
    note      text
) ON COMMIT DROP;
-- COPY stage_server (...): endereço ausente e o TTL dele, e comentário vazio, viram NULL

-- trava de remoção (pulada com --force): linhas atuais e as que sumiriam
SELECT (SELECT count(*) FROM roothints_server),
       (SELECT count(*) FROM roothints_server r
         WHERE NOT EXISTS (SELECT 1 FROM stage_server s WHERE s.name = r.name));

WITH m AS (
    MERGE INTO roothints_server t
    USING stage_server s ON t.name = s.name
    WHEN MATCHED AND (t.letter, t.ipv4, t.ipv6, t.ns_ttl, t.ipv4_ttl, t.ipv6_ttl, t.note)
                     IS DISTINCT FROM (s.letter, s.ipv4, s.ipv6, s.ns_ttl, s.ipv4_ttl, s.ipv6_ttl, s.note) THEN
        UPDATE SET letter = s.letter, ipv4 = s.ipv4, ipv6 = s.ipv6, ns_ttl = s.ns_ttl,
                   ipv4_ttl = s.ipv4_ttl, ipv6_ttl = s.ipv6_ttl, note = s.note
    WHEN NOT MATCHED BY TARGET THEN
        INSERT (name, letter, ipv4, ipv6, ns_ttl, ipv4_ttl, ipv6_ttl, note)
        VALUES (s.name, s.letter, s.ipv4, s.ipv6, s.ns_ttl, s.ipv4_ttl, s.ipv6_ttl, s.note)
    WHEN NOT MATCHED BY SOURCE THEN
        DELETE
    RETURNING merge_action() AS action
)
SELECT action, count(*) FROM m GROUP BY action;   -- INSERT / UPDATE / DELETE

INSERT INTO roothints_run (status, forced, url, http_status, etag, last_modified, md5, sha256, bytes,
                           last_update, zone_serial, servers, ipv4_addresses, ipv6_addresses,
                           server_inserted, server_updated, server_deleted, warnings, error, started_at)
VALUES (1, ...) RETURNING uuid::text;             -- a versão nova do dataset

INSERT INTO jobs (app, last_sync_at, last_check_at, consolidated)
VALUES ('collector-roothints', NOW(), NOW(), 0)
ON CONFLICT (app) DO UPDATE SET
    last_sync_at  = NOW(),
    last_check_at = NOW(),
    consolidated  = CASE WHEN <alguma linha mudou> THEN 0 ELSE jobs.consolidated END;
```

- **Trava de remoção**: recusa quando a tabela tem linhas e
  `removidos / atuais > REMOVAL_THRESHOLD` (estritamente maior); a primeira
  carga (tabela vazia) nunca trava.
- **Um `MERGE` só**, com `WHEN NOT MATCHED BY SOURCE` e
  `RETURNING merge_action()` (PostgreSQL 17 ou mais novo; o projeto usa o 18).
  Chave: `name`. Uma mudança de letra sem mudança de nome é impossível (a
  letra é o primeiro rótulo do nome).
- A tabela é pequena: não há `ANALYZE` da temporária.
- `consolidated = 0` só quando o `MERGE` alterou alguma linha: um arquivo
  novo com o mesmo conteúdo lógico (serial novo, servidores iguais) não pede
  consolidação e mantém o valor que estava. Se a linha de `jobs` ainda não
  existe, ela nasce com `consolidated = 0`.
- Última execução aplicada (MD5, validadores, SHA-256 e serial da próxima
  verificação):
  `SELECT uuid::text, url, coalesce(etag, ''), coalesce(last_modified, ''), coalesce(md5, ''), sha256, zone_serial, created_at FROM roothints_run WHERE status = 1 ORDER BY created_at DESC LIMIT 1`.
- Verificação sem mudança: `INSERT INTO jobs (app, last_check_at) VALUES ('collector-roothints', NOW()) ON CONFLICT (app) DO UPDATE SET last_check_at = NOW()`
  (cria a linha só com `last_check_at` se ela não existir).

## Recusas, erros e logs

| Momento | Causa | Linha em `roothints_run` | Mensagem (`err` do log `verificação falhou`; `error` da linha) |
|---|---|---|---|
| antes do download | banco fora ao ler a última execução | não | `lendo o último arquivo aplicado: <erro>` |
| antes do download | rede, HTTP diferente de 200 e 304, corpo cortado ou acima de 1 MiB (rede, 5xx e corpo depois das 2 novas tentativas) | não | `download: <erro>` (ex.: `download: HTTP 503`) |
| sem mudança | falha ao gravar `jobs` | não | `jobs: <erro>` |
| depois do download | MD5 divergente | sim, `status = 0`, sem cabeçalho nem contagens | `md5 divergente: …` |
| depois do download | parser | sim, sem cabeçalho nem contagens | `parser: <mensagem do parser>` |
| depois do download | menos que `MIN_SERVERS` | sim, com cabeçalho e contagens | `só <n> servidores no arquivo (mínimo <m>): arquivo truncado?` |
| depois do download | trava de remoção | sim | `o arquivo removeria …; use --force se for legítimo` |
| depois do download | erro do banco na aplicação | sim | `staging: …`, `copy stage_server: …`, `merge roothints_server: …`, `roothints_run: …`, `jobs: …` |
| depois do download | outra execução aplicando (advisory lock ocupado) | **não** | `outra execução do collector-roothints está aplicando dados` |

- A recusa é gravada com um contexto sem cancelamento
  (`context.WithoutCancel`): fica registrada mesmo com o `RUN_TIMEOUT`
  esgotado. Se nem ela puder ser gravada, o log diz
  `não consegui gravar a falha em roothints_run`.
- Avisos do parser (servidor sem IPv4 ou sem IPv6): no máximo 13 na prática;
  o limite de 50 e o `... e mais <n> avisos` do padrão valem. No log, uma
  linha `aviso do parser` por aviso, só quando o arquivo é aplicado.

| Log | Nível | Atributos (além de `app`) |
|---|---|---|
| `iniciando` | info | `version`, `commit`, `source`, `md5` (a URL do hash; vazia com `off`), `interval`, `retry`, `postgres` (sem a senha) |
| `Postgres indisponível; tentando de novo` / `sem conexão com o Postgres` | warn / error | `err` |
| `md5 publicado indisponível; seguindo sem conferência` | warn | `url`, `err` |
| `fonte sem mudança` | info | `reason` (tabela das checagens), `version`, `ms` |
| `arquivo mais antigo que o aplicado; ignorado (espelho ou servidor com cópia velha?)` | warn | `serial`, `applied_serial`, `md5` (seguido de `fonte sem mudança`) |
| `arquivo novo aplicado` | info | `version`, `zone_serial`, `last_update` (`AAAA-MM-DD`), `md5`, `sha256`, `bytes`, `servers`, `ipv4_addresses`, `ipv6_addresses`, `server_inserted`, `server_updated`, `server_deleted`, `warnings` (quantos), `forced`, `ms` |
| `aviso do parser` | warn | `warning` |
| `verificação falhou` | error | `err`, `ms` |
| `não consegui gravar a falha em roothints_run` | error | `err` |
| `próxima verificação` | debug | `in` |
| `encerrando` | info | — |

## Configuração

As opções comuns do [padrão](../../padroes/coletor.md#configuração-comum),
com os mesmos padrões (`SYNC_INTERVAL` `1h`, `RETRY_INTERVAL` `5m`,
`RUN_TIMEOUT` `10m`, `REMOVAL_THRESHOLD` `0.05`, `LOG_LEVEL` `info`,
`LOG_FORMAT` `json`, `--once`, `--force`, `--version`), mais:

| Variável | Argumento | Padrão | Uso |
|---|---|---|---|
| `SOURCE_URL` | `--source-url` | `https://www.internic.net/domain/named.root` | arquivo da fonte; `http://` ou `https://` |
| `SOURCE_MD5_URL` | `--source-md5-url` | vazio = `SOURCE_URL` + `.md5` | hash publicado; `off`, `none` ou `false` (em qualquer caixa) desligam a checagem 1 e a conferência |
| `MIN_SERVERS` | `--min-servers` | `13` | inteiro ≥ 0; abaixo disso o arquivo é tratado como truncado e recusado (`0` desliga) |
| `USER_AGENT` | `--user-agent` | vazio = `badblock-collector-roothints/<versão> (+https://github.com/patrickbrandao/badblock)` | |

`SYNC_INTERVAL` fica em `1h`: a checagem normal é um GET de 33 bytes.

Valores inválidos (stderr `collector-roothints: <erro>`, saída 2):

| Opção | Regra | Mensagem |
|---|---|---|
| `SYNC_INTERVAL`, `RETRY_INTERVAL`, `RUN_TIMEOUT` | duração Go (`time.ParseDuration`) maior que zero | `--sync-interval inválido: "0s"` (idem para as outras) |
| `MIN_SERVERS` | inteiro ≥ 0 | `--min-servers inválido: "-1"` |
| `REMOVAL_THRESHOLD` | número de 0 a 1 | `--removal-threshold inválido (0 a 1): "2"` |
| `LOG_LEVEL` | `debug`, `info`, `warn` ou `error` (sem diferenciar maiúsculas) | `--log-level inválido: "trace"` |
| `LOG_FORMAT` | `json` ou `text` (idem) | `--log-format inválido: "xml"` |
| `SOURCE_URL`, `SOURCE_MD5_URL` | `http://` ou `https://` | `--source-url precisa ser http(s): "ftp://x"` (idem `--source-md5-url`) |
| `POSTGRES_URL` | obrigatória (menos com `--version`) | `defina POSTGRES_URL (ou --postgres-url)` |
| argumento posicional | não aceito | `argumento inesperado: <arg>` |

`--help` lista as opções em ordem alfabética, cada uma com a variável e o
padrão, e depois `--once`, `--force`, `--version` e `-h, --help`.

### Compose e `.env`

O `docker-compose.yml` do app passa ao container:

| Variável no container | Valor |
|---|---|
| `POSTGRES_URL` | `${POSTGRES_URL:-postgres://postgres:${POSTGRES_PASSWORD:?defina POSTGRES_PASSWORD no .env}@badblock-postgres:5432/badblock?sslmode=disable}` |
| `SYNC_INTERVAL` | `${COLLECTOR_ROOTHINTS_SYNC_INTERVAL:-1h}` |
| `RETRY_INTERVAL` | `${COLLECTOR_ROOTHINTS_RETRY_INTERVAL:-5m}` |
| `SOURCE_URL` | `${COLLECTOR_ROOTHINTS_SOURCE_URL:-https://www.internic.net/domain/named.root}` |
| `SOURCE_MD5_URL` | `${COLLECTOR_ROOTHINTS_SOURCE_MD5_URL:-}` (vazio = `SOURCE_URL` + `.md5`) |
| `MIN_SERVERS` | `${COLLECTOR_ROOTHINTS_MIN_SERVERS:-13}` |
| `REMOVAL_THRESHOLD` | `${COLLECTOR_ROOTHINTS_REMOVAL_THRESHOLD:-0.05}` |
| `LOG_LEVEL` | `${LOG_LEVEL:-info}` |
| `LOG_FORMAT` | `json` |

Imagem `${DOCKERHUB_NAMESPACE:-tmsoftbrasil}/badblock-collector-roothints:${COLLECTOR_ROOTHINTS_TAG:-latest}`,
com `build` da pasta (`VERSION: ${COLLECTOR_ROOTHINTS_TAG:-dev}`). `RUN_TIMEOUT`
e `USER_AGENT` não passam pelo compose (valem os padrões do app).

`.env.example` do app e da raiz: `COLLECTOR_ROOTHINTS_TAG=latest` (na raiz,
na seção das imagens), `COLLECTOR_ROOTHINTS_SYNC_INTERVAL=1h`,
`COLLECTOR_ROOTHINTS_RETRY_INTERVAL=5m`, `COLLECTOR_ROOTHINTS_MIN_SERVERS=13`
e `COLLECTOR_ROOTHINTS_REMOVAL_THRESHOLD=0.05` (na raiz, sob o comentário
`# collector-roothints: servidores raiz do DNS da InterNIC (named.root)`); o do
app traz também `POSTGRES_PASSWORD=`, a `POSTGRES_URL` comentada e
`LOG_LEVEL=info`. `COLLECTOR_ROOTHINTS_SOURCE_URL` e
`COLLECTOR_ROOTHINTS_SOURCE_MD5_URL` são aceitas pelo compose, mas não
aparecem em nenhum dos dois `.env.example`.

## Operação

```bash
make -C apps/roothints/collector once    # verificação agora, no container no ar
make -C apps/roothints/collector force   # reaplica o arquivo atual
make -C apps/roothints/collector logs
```

- `--force` aqui serve para: aceitar a remoção ou a troca de nome de um
  servidor raiz (a trava recusa qualquer remoção, ver acima), aceitar um
  arquivo com serial menor de propósito, reaplicar depois de mudar o schema.
- Estado: a linha `collector-roothints` de `jobs`, o `/roothints/meta` da API
  e as últimas execuções:

  ```sql
  SELECT created_at, status, forced, zone_serial, last_update, servers,
         server_inserted, server_updated, server_deleted, error
    FROM roothints_run ORDER BY created_at DESC LIMIT 5;
  ```

## Medições

| O quê | Valor | Como |
|---|---|---|
| Primeira verificação (`.md5` + download + aplicação) | ~2,9 s (quase tudo rede) | `go run ./cmd/collector-roothints --once` contra a InterNIC e um PG18 local descartável, em 2026-09-30 |
| Verificação sem mudança pelo `.md5` | ~0,5 s | idem, logo depois (`md5 publicado igual ao último aplicado`) |
| Verificação sem mudança por 304 (`SOURCE_MD5_URL=off`) | ~0,5 s | idem (`HTTP 304`) |
| `--force` (13 servidores, 0 alterações) | ~0,8 s | idem |
| Parser | ~50 µs | `TestParseRealFile`, em 2026-09-30 |
| Carga inicial / reaplicação sem mudança | ~6 ms / ~3 ms | `TestApplyRealFile` (testcontainers), em 2026-09-30 |

Na verificação real de 2026-09-30: 13 servidores, 13 IPv4, 13 IPv6, serial
`2026092401`, `last update` 2026-09-24, MD5 conferido
(`d0732825a760fee171258b4890ca5243`), 0 avisos.

## Testes

Camadas e comandos do [padrão](../../padroes/coletor.md#testes). O que é
desta fonte:

| Pacote | Testes | O quê |
|---|---|---|
| `parse` | `TestParseFixture` | o arquivo inteiro: cabeçalho (2026-09-24, `2026092401`), 13 servidores `a`…`m` em ordem, TTLs, comentários, endereços de 6 servidores conferidos, nenhum aviso |
| `parse` | `TestParseTolerated` | CRLF, BOM, LF no fim, linhas vazias, classe `IN`, tab, tudo em minúsculas, comentário no fim da linha, `;End of file` |
| `parse` | `TestParseMissingFamilyWarns`, `TestParseBlockWithoutComment` | servidor sem `AAAA` e outro sem `A`: aceitos com aviso; bloco sem comentário: `note` vazio, sem herdar o cabeçalho |
| `parse` | `TestParseRejects` | cada linha da tabela de recusas de [fonte.md](fonte.md#regras-do-parser) (vazio, HTML, sem cabeçalho, data e serial ilegíveis ou acima de 32 bits, truncado entre blocos e no meio da linha, dados depois do fim, só o cabeçalho, `NS` fora da raiz, fora de `root-servers.net`, sem ponto final, repetido, `A` sem `NS`/antes do `NS`, endereço ilegível, da família errada, mapeado, com zona, privado, loopback, repetido, segundo `A`/`AAAA`, sem endereço, tipo `TXT`, TTL com unidade e acima de 2^31−1, classe `CH`, campo a mais, nome relativo, UTF-8 inválido, NUL, linha acima de 1 MiB) |
| `parse` | `TestParseRealFile` | com `ROOTHINTS_REAL_FILE`: o arquivo do dia, 13 servidores ou mais; loga contagens, cabeçalho e tempo |
| `fetch` | `TestParseMD5Formats`, `TestPublishedMD5`, `TestPublishedMD5Garbage`, `TestPublishedMD5TooBig` | o `.md5` real (só o hash), sem `\n`, BSD, GNU, `*`, maiúsculas, CRLF; recusa HTML, vazio, SHA-256, hash curto e longo; User-Agent; conteúdo sem hash não se repete; acima de 4 KiB repete 3 vezes e falha |
| `fetch` | `TestDownloadConditional`, `TestDownloadIfModifiedSince`, `TestIfNoneMatch`, `TestDownloadApacheGzipETag` e os de novas tentativas, 404, limite e contexto | 200 com MD5 e SHA-256 da fixture, depois 304 por ETag e por data; `IfNoneMatch` (`-gzip` forte e fraco ganha a forma sem sufixo, o resto vai igual); servidor que imita o Apache com gzip (ETag `-gzip` não reconhecido, `If-Modified-Since` ignorado com `If-None-Match`): corpo e MD5 do arquivo descomprimido e 304 na segunda requisição; 5xx até 3 tentativas, corpo cortado repetido, 404 sem repetir, limite de tamanho, contexto cancelado |
| `config` | `TestDefaults`, `TestPrecedence`, `TestAllFromEnv`, `TestMD5URL`, `TestVersionWithoutPostgres`, `TestHelpListsEveryOption`, `TestForceImpliesOnce`, `TestInvalid`, `TestRedact`, `TestUnexpectedArgument` | padrões (URL, `.md5`, `13`), ordem padrão → ambiente → argumento, `SOURCE_MD5_URL` `off`/`OFF`/`none`/`false`/própria/vazia, validações |
| `collector` | `TestFirstRunApplies` … `TestMissingFamilyWarningGoesToRun` | store e fonte falsos: primeira carga; MD5 igual não baixa; 304 com validadores; outra URL sem validadores; conteúdo igual com `.md5` fora do ar; `.md5` desligado; `.md5` indisponível aplica; MD5 divergente gravado; `--force` aplica o mesmo arquivo e confere o `.md5`; serial menor sem mudança, sem recusa e com o aviso no log (e aplicado com `--force`); serial menor pela volta de 2³² (`TestOlderSerialAcrossWrapIsIgnored`); `TestSerialLess`; serial igual com conteúdo novo, maior e aplicado sem serial aplicam; falhas antes do download sem registro; parser, `MIN_SERVERS` e trava gravados; `ErrBusy` sem registro; segunda verificação sem mudança; aviso de família ausente na execução |
| `store` (integração) | `TestApplyLifecycle` | carga inicial (13 inserções, `/32` e `/128`, TTLs, `note`, lista por letra, consulta por nome, `roothints_run` com cabeçalho e MD5, `LastApplied` com serial); reaplicação igual (versão nova, nada muda, `consolidated = 1` fica, `updated_at` intacto); IPv4 do `b` trocado e `m` sem `AAAA` e sem comentário (2 atualizações, NULLs); trava 1 de 13 (7,7%) e `--force`; recusas do parser (sem cabeçalho) e de `MIN_SERVERS` (com cabeçalho) sem virar versão; `TouchCheck` |
| `store` (integração) | `TestConstraints`, `TestTouchCheckCreatesJob`, `TestApplyBusy`, `TestApplyRealFile` | as constraints recusam nome fora do padrão, letra errada, família e máscara erradas, sem endereço, TTL sem endereço ou negativo, `note` vazio, serial acima de 32 bits, MD5 fora do formato; linha de `jobs` só com `last_check_at`; advisory lock ocupado dá `ErrBusy`; com `ROOTHINTS_REAL_FILE`, carga e reaplicação idempotente com tempos |

Integração: `postgres:18-trixie` descartável (banco `badblock`, usuário
`postgres`, senha `pg`), com as seções `migrate:up` de
`database/postgres/central/` e `database/postgres/roothints/`.

**Arquivo real**: o `Makefile` deste app não tem o alvo `test-real`; os testes
leem `ROOTHINTS_REAL_FILE`:

```bash
curl -o /tmp/named.root https://www.internic.net/domain/named.root
ROOTHINTS_REAL_FILE=/tmp/named.root make test-int   # TestParseRealFile e TestApplyRealFile
```

Verificação completa contra a InterNIC, num Postgres descartável com as
migrations de `central/` e `roothints/` aplicadas:

```bash
POSTGRES_URL='postgres://postgres:<senha>@127.0.0.1:<porta>/badblock?sslmode=disable' \
LOG_FORMAT=text go run ./cmd/collector-roothints --once
```
