# rootzone — coletor (`collector-rootzone`)

O que o `collector-rootzone` faz de diferente ou a mais que o
[padrão dos coletores](../../padroes/coletor.md). O resto — `main.go`, laço,
download, recusas, `--force`, contrato com `jobs`, configuração comum,
container — segue o padrão; as mensagens de log exatas estão em
[Recusas, erros e logs](#recusas-erros-e-logs). Formato e parser:
[fonte.md](fonte.md); tabelas: [dados.md](dados.md).

## Valores desta fonte

| Item | Valor |
|---|---|
| App, binário | `collector-rootzone` (`/collector-rootzone` na imagem) |
| Module Go | `github.com/patrickbrandao/badblock/apps/rootzone/collector` |
| Imagem, container | `tmsoftbrasil/badblock-collector-rootzone`, `badblock-collector-rootzone` |
| Descrição da imagem (`org.opencontainers.image.description`) | `Importa a zona raiz do DNS (root.zone da InterNIC: TLDs, servidores, glue e DS) para o PostgreSQL do BadBlock` |
| Linha em `jobs` | `app = 'collector-rootzone'` |
| Trava de concorrência | `pg_try_advisory_xact_lock(hashtext('collector-rootzone'))` |
| Tabelas | `rootzone_tld`, `rootzone_record`, `rootzone_run`; temporárias `stage_record` e `stage_tld` |
| Pool pgx | no máximo 2 conexões, fechadas depois de 5 min ociosas; `application_name = collector-rootzone` (se a URL não trouxer outro) |
| User-Agent | `badblock-collector-rootzone/<versão> (+https://github.com/patrickbrandao/badblock)` |
| Fixture | `testdata/root-zone-sample.zone` ([fonte.md](fonte.md#fixture)) |
| Arquivo real nos testes | variável `ROOTZONE_REAL_FILE` (`make test-real`) |

Os pacotes são os do padrão (`buildinfo`, `config`, `fetch`, `parse`,
`store`, `collector`), sem pacote a mais. Nenhuma dependência além das de
[../../projeto/convencoes.md](../../projeto/convencoes.md#go): o parser de
zona e o punycode são feitos com a biblioteca padrão.

## Checagens de mudança

Da mais barata para a mais cara; a primeira que disser "igual" encerra a
verificação como sem mudança (sem linha em `rootzone_run`, só
`jobs.last_check_at`):

| # | Checagem | "Igual" quando | `reason` no log | Custo |
|---|---|---|---|---|
| 1 | MD5 publicado (`SOURCE_MD5_URL`) | igual ao `md5` da última execução aplicada | `md5 publicado igual ao último aplicado` | 33 bytes |
| 2 | GET condicional com o `etag`/`last_modified` da última aplicada | HTTP 304 | `HTTP 304` | só cabeçalhos |
| 3 | SHA-256 do arquivo baixado (descomprimido) | igual ao `sha256` da última aplicada | `conteúdo igual ao último aplicado` | download (~1 MB com gzip) |
| 4 | serial do SOA, depois da conferência do MD5 e do parser | menor que o aplicado (RFC 1982) | `arquivo mais antigo que o aplicado (serial 2026092901 < 2026092902)` | download e parser |

- **Nunca remova essas checagens.** O `.md5` é pedido antes do arquivo em
  toda verificação com `SOURCE_MD5_URL` ligada (inclusive com `--force`, para
  a conferência). Fora do ar ou ilegível: log `md5 publicado indisponível;
  seguindo sem conferência` (`url`, `err`) e a verificação segue pelas
  checagens 2 a 4, sem conferência. Regras de leitura do `.md5`: as do
  padrão (até 4 KiB; formatos BSD, GNU ou só o hash; maiúsculas ou
  minúsculas; página HTML não vira hash).
- O `.md5` sai ~1 min depois do arquivo ([fonte.md](fonte.md#arquivos-publicados)):
  nessa janela, o MD5 publicado ainda é o da versão aplicada e a checagem 1
  diz "igual"; a versão nova entra na verificação seguinte. Se o arquivo
  novo for baixado com o `.md5` velho, a conferência falha (abaixo) e a
  próxima verificação resolve.
- Os validadores só são enviados se a URL da última execução aplicada for a
  mesma; o MD5 e o SHA-256 valem para qualquer URL.
- `--force` não envia validadores e pula as checagens 1, 3 e 4 (o `.md5`
  continua sendo pedido para a conferência).
- Com `SYNC_INTERVAL` de 1h e ~2 versões por dia, a maioria das verificações
  termina na checagem 1 (medido em 2026-09-30: ~0,6 s, inclusive o log e o
  `jobs`).

### Arquivo mais antigo e serial igual

O serial do SOA (`AAAAMMDDNN`) é comparado com o `rootzone_run.serial` da
última aplicada pela **aritmética de seriais da RFC 1982** (32 bits: `a < b`
quando são diferentes e `b − a` módulo 2³² é menor que 2³¹; com o formato de
data da raiz é a comparação numérica comum).

- **Menor** (um espelho, um cache no caminho — o servidor manda
  `max-age=420` — ou uma cópia velha): **não é aplicado e não é falha**;
  conta como sem mudança, sem linha em `rootzone_run`, com o aviso no log
  `arquivo mais antigo que o aplicado; ignorado (espelho ou servidor com
  cópia velha?)` (`serial`, `applied_serial`, `md5`), como na família RIR
  ([../rir/collector.md](../rir/collector.md#arquivo-mais-antigo-que-o-aplicado)).
  Não grava recusa porque, enquanto a cópia velha estiver no ar, cada
  verificação a baixaria de novo e encheria `rootzone_run` de linhas iguais.
  Os avisos do parser desse arquivo se perdem.
- **Igual, com conteúdo diferente** (o SHA-256 já disse que mudou):
  **aplica**, com o aviso `serial <s> igual ao aplicado, com conteúdo
  diferente (sha256 <hash>); aplicado mesmo assim` em
  `rootzone_run.warnings` e no log. Pela RFC 1034 toda mudança na zona
  incrementa o serial, então o mesmo serial com outro conteúdo é uma
  republicação do mesmo dado (formato, ordem, assinaturas — que nem são
  guardadas); o `MERGE` só altera o que de fato mudou (nada, no caso
  esperado), e recusar travaria a coleta até o próximo serial. Um arquivo
  corrompido com o mesmo serial não passa da conferência do MD5 nem do
  parser.
- **Maior**: aplica.
- Com `--force` a checagem não é feita (volta a uma versão anterior de
  propósito).

## Download: gzip e o ETag `-gzip`

Cliente, prazos, limite (64 MiB: `MaxBytes: 64 << 20`; o arquivo tem
~2,2 MB) e novas tentativas são os do padrão. O que é desta fonte:

- O `fetch` não define `Accept-Encoding`, então o transporte do Go pede
  `gzip` sozinho e descomprime: trafegam ~1 MB em vez de ~2,2 MB. Corpo,
  limite, MD5 e SHA-256 valem para o arquivo **descomprimido** (é o que o
  `.md5` confere); `rootzone_run.bytes` é o tamanho descomprimido.
- Com gzip, o Apache da InterNIC devolve o ETag com o sufixo `-gzip` e **não
  o reconhece** no `If-None-Match` ([fonte.md](fonte.md#servidor-http-medido-em-2026-09-30)).
  O ETag é guardado como veio em `rootzone_run.etag`, e o `If-None-Match`
  leva a lista com ele e com a forma sem o sufixo
  (`fetch.IfNoneMatch`: `"225757-65ca377d2eb00-gzip", "225757-65ca377d2eb00"`),
  que o Apache responde com 304 — e um servidor que reconheça a primeira
  forma também. ETag sem o sufixo vai sozinho, como veio. Medido em
  2026-09-30 com `SOURCE_MD5_URL=off`: `fonte sem mudança`, `reason=HTTP 304`,
  ~0,5 s.

## Validações antes de aplicar

Na ordem (regras gerais no
[padrão](../../padroes/coletor.md#fonte-nova-validar-antes-de-aplicar)):

| # | Validação | Mensagem de recusa |
|---|---|---|
| 1 | conferência: com hash publicado, o MD5 do download tem de ser igual | `md5 divergente: publicado <hash>, baixado <hash> (arquivo e .md5 publicados em momentos diferentes?)` |
| 2 | parser ([fonte.md](fonte.md#linhas-parseparse)): SOA único na raiz, NS da raiz, última linha com LF, no máximo 1% de descartes | `parser: <mensagem do parser>` |
| 3 | serial mais antigo (acima) | — (sem mudança) |
| 4 | **`MIN_TLDS`** (padrão `1000`): TLDs delegados ≥ `MIN_TLDS` | `só <n> TLDs na zona (mínimo <m>): arquivo truncado?` |
| 5 | aplicação: trava de remoção e erros do banco | a mensagem do erro |

**Por que 1000**: o arquivo de 2026-09-29 tem 1.438 TLDs; a raiz passou de
~1,5 mil no auge (2016) para os atuais com a retirada de gTLDs de marca, de
poucos em poucos, e nunca perde centenas de uma vez. Menos de 1000 (−30%)
só acontece com um arquivo cortado ou um formato que o parser não entende
direito — casos que a trava de remoção de 5% também pega a partir da segunda
versão, mas o mínimo protege já a **primeira carga** (tabela vazia, trava
desligada). O SOA único e os NS da raiz são exigidos pelo parser (regras de
recusa em [fonte.md](fonte.md#linhas-parseparse)).

## Aplicação

Tudo numa transação (`internal/store`, `Store.Apply`):

```sql
SELECT pg_try_advisory_xact_lock(hashtext('collector-rootzone'));  -- false: ErrBusy

CREATE TEMP TABLE stage_record (
    owner  text    NOT NULL,
    type   text    NOT NULL,
    rdata  text    NOT NULL,
    ttl    integer NOT NULL,
    PRIMARY KEY (owner, type, rdata)
) ON COMMIT DROP;
CREATE TEMP TABLE stage_tld (
    tld               text     PRIMARY KEY,
    tld_unicode       text     NOT NULL,
    nameservers       smallint NOT NULL,
    nameservers_ipv4  smallint NOT NULL,
    nameservers_ipv6  smallint NOT NULL,
    ds_records        smallint NOT NULL
) ON COMMIT DROP;
-- COPY stage_record (owner, type, rdata, ttl); COPY stage_tld (...)
ANALYZE stage_record; ANALYZE stage_tld;

-- trava de remoção (pulada com --force), só sobre rootzone_record
SELECT (SELECT count(*) FROM rootzone_record),
       (SELECT count(*) FROM rootzone_record r
         WHERE NOT EXISTS (SELECT 1 FROM stage_record s
                            WHERE s.owner = r.owner AND s.type = r.type AND s.rdata = r.rdata));

WITH m AS (
    MERGE INTO rootzone_record t
    USING stage_record s ON t.owner = s.owner AND t.type = s.type AND t.rdata = s.rdata
    WHEN MATCHED AND t.ttl IS DISTINCT FROM s.ttl THEN UPDATE SET ttl = s.ttl
    WHEN NOT MATCHED BY TARGET THEN INSERT (owner, type, rdata, ttl) VALUES (s.owner, s.type, s.rdata, s.ttl)
    WHEN NOT MATCHED BY SOURCE THEN DELETE
    RETURNING merge_action() AS action
)
SELECT action, count(*) FROM m GROUP BY action;   -- INSERT / UPDATE / DELETE

-- idem para rootzone_tld (chave tld; UPDATE quando tld_unicode ou uma
-- contagem mudou, com IS DISTINCT FROM)

INSERT INTO rootzone_run (status, forced, url, http_status, etag, last_modified, md5, sha256, bytes,
                          serial, soa_mname, soa_rname, soa_refresh, soa_retry, soa_expire, soa_minimum,
                          tlds, records, rrsigs, type_counts,
                          tld_inserted, tld_updated, tld_deleted, record_inserted, record_updated, record_deleted,
                          warnings, error, started_at)
VALUES (1, ...) RETURNING uuid::text;             -- a versão nova do dataset

INSERT INTO jobs (app, last_sync_at, last_check_at, consolidated)
VALUES ('collector-rootzone', NOW(), NOW(), 0)
ON CONFLICT (app) DO UPDATE SET
    last_sync_at  = NOW(),
    last_check_at = NOW(),
    consolidated  = CASE WHEN <alguma linha mudou> THEN 0 ELSE jobs.consolidated END;
```

- **Trava de remoção** (`REMOVAL_THRESHOLD`, padrão `0.05`) sobre
  `rootzone_record`: recusa quando a tabela tem linhas e
  `removidos / atuais > REMOVAL_THRESHOLD` (estritamente maior); a primeira
  carga nunca trava. Mensagem:
  `a zona removeria <r> de <c> registros (<x.y>%, limite <z.w>%); use --force se for legítimo`.
  Uma publicação normal remove o SOA e o ZONEMD antigos (2 de ~22,1 mil) e o
  que mudou nas delegações; 5% são ~1,1 mil RRs — a retirada de ~70 TLDs de
  uma vez. `rootzone_tld` não tem trava própria: um TLD removido leva junto
  as suas RRs, e a trava sobre `rootzone_record` já o cobre.
- A chave natural de `rootzone_record` é `(owner, type, rdata)`: uma RR que
  muda o rdata é um DELETE e um INSERT; só o TTL gera UPDATE.
- **Um `MERGE` por tabela**, com `WHEN NOT MATCHED BY SOURCE` e
  `RETURNING merge_action()` (PostgreSQL 17+); as contagens voltam agregadas.
- `consolidated = 0` só quando algum `MERGE` alterou linhas (um arquivo com o
  mesmo conteúdo lógico não pede consolidação).
- Última execução aplicada (validadores, MD5, SHA-256 e serial da próxima
  verificação):
  `SELECT uuid::text, url, coalesce(etag, ''), coalesce(last_modified, ''), coalesce(md5, ''), sha256, serial, created_at FROM rootzone_run WHERE status = 1 ORDER BY created_at DESC LIMIT 1`.
- Verificação sem mudança: `INSERT INTO jobs (app, last_check_at) VALUES ('collector-rootzone', NOW()) ON CONFLICT (app) DO UPDATE SET last_check_at = NOW()`.

## Recusas, erros e logs

| Momento | Causa | Linha em `rootzone_run` | Mensagem (`err` do log `verificação falhou`; `error` da linha) |
|---|---|---|---|
| antes do download | banco fora ao ler a última execução | não | `lendo o último arquivo aplicado: <erro>` |
| antes do download | rede, HTTP diferente de 200 e 304, corpo cortado ou acima de 64 MiB (depois das 2 novas tentativas) | não | `download: <erro>` (ex.: `download: HTTP 503`) |
| sem mudança | falha ao gravar `jobs` | não | `jobs: <erro>` |
| depois do download | MD5 divergente | sim, `status = 0`, sem SOA nem contagens | `md5 divergente: …` |
| depois do download | parser | sim, sem SOA nem contagens | `parser: <mensagem do parser>` |
| depois do download | menos que `MIN_TLDS` | sim, com SOA e contagens | `só <n> TLDs na zona (mínimo <m>): arquivo truncado?` |
| depois do download | trava de remoção | sim, com SOA e contagens | `a zona removeria …; use --force se for legítimo` |
| depois do download | erro do banco na aplicação | sim | `staging: …`, `copy stage_record: …`, `copy stage_tld: …`, `merge rootzone_record: …`, `merge rootzone_tld: …`, `rootzone_run: …`, `jobs: …` |
| depois do download | outra execução aplicando (advisory lock ocupado) | **não** | `outra execução do collector-rootzone está aplicando dados` |

- A recusa é gravada com um contexto sem cancelamento
  (`context.WithoutCancel`). Se nem ela puder ser gravada, o log diz
  `não consegui gravar a falha em rootzone_run`.
- Avisos: os 50 primeiros, mais `... e mais <n> avisos` (até 51 itens em
  `rootzone_run.warnings`); no log, uma linha `aviso do parser` por aviso,
  só quando o arquivo é aplicado. Numa recusa do próprio parser, `warnings`
  fica `[]` e a mensagem traz os 3 primeiros.

| Log | Nível | Atributos (além de `app`) |
|---|---|---|
| `iniciando` | info | `version`, `commit`, `source`, `md5` (URL do hash; vazio = desligado), `interval`, `retry`, `min_tlds`, `postgres` (sem a senha) |
| `Postgres indisponível; tentando de novo` / `sem conexão com o Postgres` | warn / error | `err` |
| `md5 publicado indisponível; seguindo sem conferência` | warn | `url`, `err` |
| `arquivo mais antigo que o aplicado; ignorado (espelho ou servidor com cópia velha?)` | warn | `serial`, `applied_serial`, `md5` |
| `fonte sem mudança` | info | `reason`, `version`, `ms` |
| `arquivo novo aplicado` | info | `version`, `serial`, `md5`, `bytes`, `tlds`, `records`, `rrsigs`, `tld_inserted`, `tld_updated`, `tld_deleted`, `record_inserted`, `record_updated`, `record_deleted`, `warnings` (quantos), `forced`, `ms` |
| `aviso do parser` | warn | `warning` |
| `verificação falhou` | error | `err`, `ms` |
| `não consegui gravar a falha em rootzone_run` | error | `err` |
| `próxima verificação` | debug | `in` |
| `encerrando` | info | — |

## Configuração

As opções comuns do [padrão](../../padroes/coletor.md#configuração-comum),
com os mesmos padrões (`SYNC_INTERVAL` `1h`, `RETRY_INTERVAL` `5m`,
`RUN_TIMEOUT` `10m`, `REMOVAL_THRESHOLD` `0.05`, `LOG_LEVEL` `info`,
`LOG_FORMAT` `json`, `--once`, `--force`, `--version`), mais:

| Variável | Argumento | Padrão | Uso |
|---|---|---|---|
| `SOURCE_URL` | `--source-url` | `https://www.internic.net/domain/root.zone` | arquivo da fonte; `http://` ou `https://` |
| `SOURCE_MD5_URL` | `--source-md5-url` | vazio = `SOURCE_URL` + `.md5` | hash publicado; `off`, `none` ou `false` (qualquer caixa) desligam a checagem 1 e a conferência |
| `MIN_TLDS` | `--min-tlds` | `1000` | inteiro ≥ 0; abaixo disso o arquivo é tratado como truncado e recusado (`0` desliga) |
| `USER_AGENT` | `--user-agent` | vazio = `badblock-collector-rootzone/<versão> (+https://github.com/patrickbrandao/badblock)` | |

Valores inválidos (stderr `collector-rootzone: <erro>`, saída 2): os do
padrão, mais `--min-tlds inválido: "-1"` e
`--source-md5-url precisa ser http(s): "ftp://x"`. `--help` lista as opções
em ordem alfabética, cada uma com a variável e o padrão, e depois `--once`,
`--force`, `--version` e `-h, --help`.

### Compose e `.env`

O `docker-compose.yml` do app passa ao container:

| Variável no container | Valor |
|---|---|
| `POSTGRES_URL` | `${POSTGRES_URL:-postgres://postgres:${POSTGRES_PASSWORD:?defina POSTGRES_PASSWORD no .env}@badblock-postgres:5432/badblock?sslmode=disable}` |
| `SYNC_INTERVAL` | `${COLLECTOR_ROOTZONE_SYNC_INTERVAL:-1h}` |
| `RETRY_INTERVAL` | `${COLLECTOR_ROOTZONE_RETRY_INTERVAL:-5m}` |
| `SOURCE_URL` | `${COLLECTOR_ROOTZONE_SOURCE_URL:-https://www.internic.net/domain/root.zone}` |
| `SOURCE_MD5_URL` | `${COLLECTOR_ROOTZONE_SOURCE_MD5_URL:-}` |
| `MIN_TLDS` | `${COLLECTOR_ROOTZONE_MIN_TLDS:-1000}` |
| `REMOVAL_THRESHOLD` | `${COLLECTOR_ROOTZONE_REMOVAL_THRESHOLD:-0.05}` |
| `LOG_LEVEL` | `${LOG_LEVEL:-info}` |
| `LOG_FORMAT` | `json` |

Imagem `${DOCKERHUB_NAMESPACE:-tmsoftbrasil}/badblock-collector-rootzone:${COLLECTOR_ROOTZONE_TAG:-latest}`,
com `build` da pasta (`VERSION: ${COLLECTOR_ROOTZONE_TAG:-dev}`). `RUN_TIMEOUT`
e `USER_AGENT` não passam pelo compose (valem os padrões do app).

`.env.example` do app e da raiz: `COLLECTOR_ROOTZONE_TAG=latest` (na raiz, na
seção das imagens), `COLLECTOR_ROOTZONE_SYNC_INTERVAL=1h`,
`COLLECTOR_ROOTZONE_RETRY_INTERVAL=5m`, `COLLECTOR_ROOTZONE_SOURCE_MD5_URL=`
(vazio), `COLLECTOR_ROOTZONE_MIN_TLDS=1000` e
`COLLECTOR_ROOTZONE_REMOVAL_THRESHOLD=0.05` (na raiz, sob o comentário
`# collector-rootzone: zona raiz do DNS da InterNIC (root.zone)`); o do app
traz também `POSTGRES_PASSWORD=`, a `POSTGRES_URL` comentada e
`LOG_LEVEL=info`. `COLLECTOR_ROOTZONE_SOURCE_URL` é aceita pelo compose, mas
não aparece nos `.env.example`.

## Operação

```bash
make -C apps/rootzone/collector once    # verificação agora, no container no ar
make -C apps/rootzone/collector force   # reaplica o arquivo atual
make -C apps/rootzone/collector logs
```

- `--force` serve para: reaplicar depois de mudar o schema ou a normalização,
  aceitar uma remoção grande legítima, voltar a um serial anterior de
  propósito.
- Estado: a linha `collector-rootzone` de `jobs`, o `/rootzone/meta` da API e
  as últimas execuções:

  ```sql
  SELECT created_at, status, forced, serial, tlds, records, rrsigs,
         record_inserted, record_updated, record_deleted, error
    FROM rootzone_run ORDER BY created_at DESC LIMIT 5;
  ```

## Medições

Arquivo de 2026-09-29 (serial `2026092901`: 24.925 linhas, 22.131 RRs
guardadas, 1.438 TLDs), medido em 2026-09-30; PG18 local.

| O quê | Valor | Como |
|---|---|---|
| Download completo com gzip (~1 MB) | ~5–6 s | `curl --compressed` e `go run ./cmd/collector-rootzone --once` |
| Download sem gzip (2,2 MB) | ~12 s | `curl` |
| Verificação sem mudança pelo `.md5` | ~0,6 s | `--once`, segunda execução |
| Verificação sem mudança por 304 (`SOURCE_MD5_URL=off`) | ~0,5 s | `--once` |
| Parser | ~20–35 ms | `TestParseRealFile` / `TestApplyRealFile` |
| Carga inicial (tabelas vazias) | ~0,24 s | `TestApplyRealFile` |
| Reaplicação sem mudança | ~0,12 s | idem |
| Verificação completa com carga inicial | ~6,7 s | `--once` num PG18 descartável (quase tudo é o download) |
| `--force` com o mesmo arquivo (0 alterações) | ~6,1 s | `--force` em seguida |

Tempos das consultas da API: [dados.md](dados.md#consultas-da-api). Quantas
RRs mudam entre duas versões consecutivas ainda não foi medido (só havia uma
versão no dia da medição): meça com o `record_*` de `rootzone_run` depois de
alguns dias em produção e registre aqui.

## Testes

Camadas e comandos do [padrão](../../padroes/coletor.md#testes). O que é
desta fonte:

| Pacote | Testes | O quê |
|---|---|---|
| `parse` | `TestParseSample` | o recorte: 212 linhas, 195 RRs, 17 RRSIG, contagens por tipo, SOA, os 7 TLDs campo a campo, rdata normalizado (IPv6 canônico, DS, NSEC para a raiz, ZONEMD, DNSKEY), nenhum RRSIG guardado |
| `parse` | `TestName`, `TestParseEdgeCases` | normalização de nomes (maiúsculas, raiz, rótulo de 63/64, nome de 253+, relativo, caractere inválido); BOM, CRLF, comentários, espaços no lugar de TAB, classe e tipo em minúsculas, digest e chave quebrados em vários campos, hex minúsculo |
| `parse` | `TestParseDiscards` | cada motivo de descarte, uma linha ruim acrescentada ao recorte (28 casos) |
| `parse` | `TestParseRejectsBrokenFile`, `TestParseSkippedLimit`, `TestWarningsAreCapped` | recusas: vazio, só comentários, sem LF no fim, cortado ao meio, sem SOA, dois SOA, sem NS da raiz, formato novo (`CH`), linha gigante, repetidas demais; 2 ruins em 214 passam e 3 em 215 recusam; 50 avisos guardados |
| `parse` | `TestPunycode`, `TestPunycodeInvalidTLDWarns` | IDN reais (`рф`, `中国`, `бел`, `امارات`, `कॉम`, `セール`, `한국`) e os exemplos da RFC 3492; punycode inválido vira aviso |
| `fetch` | `TestParseMD5Formats`, `TestPublishedMD5`, `TestPublishedMD5Garbage`, `TestPublishedMD5TooLarge` | o `.md5` da InterNIC (só o hash) e os formatos BSD/GNU; HTML, vazio, SHA-256 e hash curto recusados sem repetir; acima de 4 KiB |
| `fetch` | `TestDownloadConditional`, `TestDownloadIfModifiedSince`, `TestIfNoneMatch`, `TestDownloadApacheGzipETag` | 304 por ETag e por data, User-Agent, MD5 e SHA-256 do corpo; a lista do `If-None-Match`; um servidor que imita o Apache (gzip, ETag `-gzip` que ele não reconhece) devolve 304 na segunda requisição |
| `fetch` | `TestDownloadRetries…`, `TestDownloadDoesNotRetry404`, `TestDownloadMaxBytes`, `TestDownloadCanceledContextStopsRetries` | 5xx e corpo cortado repetidos (3 tentativas), 404 não, limite, contexto cancelado |
| `config` | `TestDefaults` … `TestMD5URL` | padrões, precedência, `SOURCE_MD5_URL` (vazio, `off`/`none`/`false`, URL), validações, `--help`, `--force` implica `--once` |
| `collector` | `TestFirstRunApplies` … `TestSecondRunUnchanged` | store e fonte falsos com o recorte: primeira carga; MD5 igual não baixa; 304 com validadores; outra URL sem validadores; conteúdo igual; `.md5` desligado e fora do ar; MD5 divergente gravado; `--force`; serial menor ignorado (e aplicado com `--force`), maior aplica, igual aplica com aviso; `SerialLess` (volta de 2³²); recusas do parser, do `MIN_TLDS` e da trava gravadas; `ErrBusy` e falhas antes do download sem registro; avisos limitados a 50 + resumo |
| `store` (integração) | `TestApplyLifecycle` | carga do recorte (7 TLDs, 195 RRs, `type_counts`, `LastApplied`, **as consultas da API** de [dados.md](dados.md#consultas-da-api)); reaplicação igual (versão nova, nada muda, `consolidated = 1` fica); versão nova (serial novo, `zw` removido, TTL do DS do `br` mudado, NS novo: 3/1/8 RRs e 0/1/1 TLDs); trava e `--force`; recusas gravadas sem virar versão (sem e com SOA); `TouchCheck` |
| `store` (integração) | `TestTouchCheckCreatesJob`, `TestApplyBusy` | linha de `jobs` só com `last_check_at`; lock ocupado dá `ErrBusy` sem linha |

Integração: `postgres:18-trixie` descartável (banco `badblock`, usuário
`postgres`), com as seções `migrate:up` de `database/postgres/central/` e
`database/postgres/rootzone/`.

### Arquivo real (`make test-real`)

```bash
make -C apps/rootzone/collector test-real                        # baixa o root.zone do dia
make -C apps/rootzone/collector test-real FILE=/tmp/root.zone    # ou usa um já baixado
```

Sem `FILE`, o alvo baixa `https://www.internic.net/domain/root.zone` com
`curl --compressed` para um arquivo temporário. Roda, com
`ROOTZONE_REAL_FILE`:

- `TestParseRealFile`: nenhum descarte e 1000 TLDs ou mais; loga o tempo, o
  serial, as linhas, as RRs guardadas, os RRSIG, os TLDs (IDN, com DS, com
  IPv6) e as contagens por tipo.
- `TestApplyRealFile` (Docker): carga inicial insere tudo, reaplicação altera
  0 linhas; cada consulta da API com o tempo, conferindo no `EXPLAIN` o índice
  esperado.

Resultado em 2026-09-30 (arquivo de 2026-09-29): parser 20–35 ms, 22.131 RRs
e 1.438 TLDs (151 IDN, 1.351 com DS, 1.420 com IPv6), 0 avisos; carga 0,24 s,
reaplicação 0,12 s; consultas da API de 0,4 a 4 ms; `make test-real` inteiro
~10 s (download, dois pacotes e o contêiner).

Verificação real contra a internet, no mesmo dia, com `go run
./cmd/collector-rootzone --once` num PG18 descartável com as migrations de
`central/` e `rootzone/`: 1ª execução `arquivo novo aplicado` (1.438 TLDs,
22.131 RRs, 2.794 RRSIG, ~6,7 s); 2ª `fonte sem mudança` pelo `.md5`
(~0,6 s); 3ª com `SOURCE_MD5_URL=off`, `HTTP 304` com o ETag `-gzip`
(~0,5 s); `--force` aplicou de novo com 0 alterações (~6,1 s).
