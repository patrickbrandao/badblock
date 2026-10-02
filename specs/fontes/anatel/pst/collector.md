# Coletor `collector-anatel-pst`

O que o `collector-anatel-pst` faz de diferente ou a mais que o
[padrão dos coletores](../../../padroes/coletor.md); o resto (`main.go`, laço,
`--once`/`--force`, contrato com `jobs`, container) segue o padrão. O ZIP, o
CSV e o parser: [fonte.md](fonte.md). Tabelas: [dados.md](dados.md). Dono:
sub-agente `collector-anatel-pst`.

## Valores desta fonte

| Item | Valor |
|---|---|
| App e `jobs.app` | `collector-anatel-pst` (demais nomes: [../../../projeto/estrutura.md](../../../projeto/estrutura.md#fonte-de-dois-níveis-siteconjunto)) |
| Pasta e module Go | `apps/anatel/pst/collector/`, `github.com/patrickbrandao/badblock/apps/anatel/pst/collector` |
| Tabelas escritas | `anatel_pst_provider`, `anatel_pst_service`, `anatel_pst_run` e a linha `collector-anatel-pst` de `jobs` |
| Tabelas temporárias | `stage_provider`, `stage_service` (`ON COMMIT DROP`) |
| Pacotes | os do padrão (`buildinfo`, `config`, `fetch`, `parse`, `store`, `collector`) e **`archive`**: extrai o CSV do ZIP ([abaixo](#o-zip-internalarchive)) |
| Conexão | pool pgx de no máximo 2 conexões, ociosas fechadas em 5 min; `application_name=collector-anatel-pst` quando a `POSTGRES_URL` não define outro |
| Descrição da imagem | `Importa as prestadoras de serviços de telecomunicações da Anatel para o PostgreSQL do BadBlock` (`org.opencontainers.image.description`) |
| Verificação | a cada **24 h** (`SYNC_INTERVAL=24h`): a Anatel regera o arquivo uma vez por dia |

## Checagens de mudança

A Anatel não publica hash do ZIP ([fonte.md](fonte.md#url)): não há
conferência nem opção de URL de hash. Quatro checagens, da mais barata para a
mais cara; a primeira que disser "igual" encerra a verificação como sem
mudança, com o `reason` no log `fonte sem mudança`:

| # | Checagem | Custo | `reason` |
|---|---|---|---|
| 1 | GET condicional com o `etag` e o `last_modified` da última execução aplicada → `304` | só cabeçalhos | `HTTP 304` |
| 2 | SHA-256 do ZIP baixado igual ao `sha256` da última execução aplicada | o download (~14,7 MB) | `conteúdo igual ao último aplicado` |
| 3 | SHA-256 do CSV extraído igual ao `csv_sha256` da última execução aplicada | download e extração (~79 MB) | `csv igual ao último aplicado` |
| 4 | CSV **mais antigo** que o aplicado (abaixo) | download e extração | `csv mais antigo que o aplicado (2026-09-29T06:15:08-03:00 < 2026-09-30T06:15:08-03:00)` |

- Sem execução aplicada (banco vazio), nenhuma checagem diz "igual": a
  primeira verificação sempre baixa e aplica.
- Os validadores só vão se a URL for a mesma da última execução aplicada. A
  Anatel (pelo Cloudflare) responde `304` a qualquer um dos dois.
- A checagem 3 existe porque o ZIP pode ser regerado com o mesmo CSV (outro
  ZIP, outro `ETag`): sem ela, cada regeração viraria uma versão nova. Ela
  não grava nada; enquanto o ZIP novo estiver no ar, cada verificação o
  baixa e extrai de novo (os validadores e o `sha256` aplicados são os do ZIP
  antigo) — uma vez por dia, ~1 s.
- As checagens 3 e 4 vêm **depois** da extração: um ZIP quebrado é recusado
  (linha `status = 0`) antes delas. Vêm **antes** do parser, porque a data e
  o hash do CSV não dependem dele.

### CSV mais antigo que o aplicado

O Cloudflare pode servir uma cópia de até 4 h atrás e um espelho pode servir
uma velha ([fonte.md](fonte.md#publicação)). Sem `--force` e com uma
execução aplicada, o CSV é **mais antigo** quando os dois têm data
(`csv_modified_at`, a data da entrada no ZIP lida como hora de Brasília —
[abaixo](#o-zip-internalarchive)) e a nova é **anterior** à aplicada. Data
igual ou mais nova segue; sem data num dos lados (entrada com a data MS-DOS
zerada), a checagem não roda.

Como na [família RIR](../../rir/collector.md#arquivo-mais-antigo-que-o-aplicado),
o CSV mais antigo **não é aplicado e não é falha**: conta como sem mudança,
sem linha em `anatel_pst_run`, com o aviso no log
`csv mais antigo que o aplicado; ignorado (espelho ou servidor com cópia velha?)`
(`csv_modified_at`, `applied_csv_modified_at`, `sha256`, `csv_sha256`). O CSV
não chega ao parser. `--force` pula esta checagem (para voltar a um arquivo
anterior de propósito).

## Download

Os valores do [padrão](../../../padroes/coletor.md#download-internalfetch), com
estes detalhes:

| Item | Valor |
|---|---|
| Tamanho máximo do ZIP | 64 MiB, o do padrão (o ZIP tinha 14.734.094 bytes em 2026-09-30) |
| Status aceito | `200` (e `304` no GET condicional); qualquer outro é o erro `HTTP <código>` |
| Novas tentativas | 2, com 10 s entre elas, em erro de rede, erro na leitura do corpo (inclusive corpo acima do limite) ou HTTP ≥ 500 |
| Cliente HTTP | o do [padrão](../../../padroes/coletor.md#download-internalfetch) (keep-alive de 30 s, ociosa fechada em 90 s, até 2 ociosas por host) |

## O ZIP (`internal/archive`)

`archive.Extract(zip, max)` lê o ZIP **da memória** (`archive/zip`, sem
arquivo temporário) e devolve a entrada CSV descomprimida, o SHA-256 dela, o
nome, a data e as entradas ignoradas.

| Regra | Recusa (`error` em `anatel_pst_run`) |
|---|---|
| o corpo não é um ZIP | `ZIP inválido: <erro do archive/zip>` |
| nenhuma entrada terminada em `.csv` (sem distinguir caixa; pastas não contam) | `ZIP sem CSV` |
| mais de uma entrada `.csv` | `ZIP com <n> arquivos CSV: <nome1>, <nome2>` |
| CSV descomprimido acima de **512 MiB** (pelo tamanho declarado e, de novo, na leitura: o declarado pode mentir) | `CSV <nome> maior que 536870912 bytes descomprimido` |
| erro ao descomprimir, inclusive CRC-32 errado (conferido pelo `archive/zip` no fim da entrada) | `lendo <nome> do ZIP: <erro>` |

- As outras entradas (não `.csv`) são ignoradas, cada uma com o aviso no log
  `entrada do ZIP ignorada` (`name`); não vão para `warnings`.
- O limite de 512 MiB fica ~6,5 vezes acima do CSV de 2026-09-30 (79 MB) e
  protege contra um ZIP corrompido ou uma "bomba" de compressão. ZIP e CSV
  ficam na memória durante a verificação (~95 MB, mais o dataset).
- **Data da entrada** (`csv_modified_at`): o campo MS-DOS do ZIP (data e
  hora, resolução de 2 s, sem fuso) lido como hora de Brasília, **UTC−3
  fixo** (sem horário de verão desde 2019), e gravado como `timestamptz`.
  O campo `Modified` do `archive/zip` **não** é usado como está, porque ele
  muda conforme o ZIP:
  - sem timestamp estendido, o Go o preenche com o relógio MS-DOS marcado
    como UTC (3 h adiantado, se usado direto);
  - com timestamp estendido (o ZIP da Anatel traz o NTFS, extra `0x000a`), o
    Go usa o instante do NTFS, num fuso estimado pela diferença para o
    MS-DOS. Em 2026-09-30: MS-DOS `06:15:08`, NTFS `09:15:06.14 UTC`
    (`06:15:06` em Brasília) — 2 s de diferença.

  O coletor lê sempre os campos MS-DOS (`ModifiedDate`/`ModifiedTime`,
  marcados como obsoletos no Go, com `//nolint:staticcheck`), para a regra
  ser a mesma com ou sem timestamp estendido. Data MS-DOS inválida (mês ou
  dia zero): `csv_modified_at` NULL e a checagem 4 não roda.

## Validação de um arquivo novo

Na ordem do padrão; qualquer falha recusa o arquivo
([Recusas e falhas](#recusas-e-falhas)):

1. **ZIP** (acima). Não há conferência de hash publicado.
2. **Parser** ([fonte.md](fonte.md#regras-do-parser-internalparse)):
   `parser: <erro>` — cabeçalho, aspas quebradas, ou mais de **1%** das
   linhas de CNPJ descartadas (`Skipped` / `RowsCNPJ`; cópias exatas e linhas
   de CPF não contam).
3. **Mínimo de prestadoras**: menos de `MIN_PROVIDERS` CNPJs aceitos (padrão
   `30000`, ~67% dos 45.074 de 2026-09-30) é arquivo truncado —
   `só <n> prestadoras no arquivo (mínimo <m>): arquivo truncado?`. Não há
   mínimo de serviços.
4. **Aplicação** (abaixo): trava de remoção e erros do banco.

A trava de remoção vale para prestadoras e para serviços: com os números de
2026-09-30 e `REMOVAL_THRESHOLD=0.05`, ela recusa a partir de 2.254
prestadoras ou 2.716 serviços removidos de uma vez.

## Aplicação (`internal/store`)

A transação única do padrão, com estes passos:

1. `SELECT pg_try_advisory_xact_lock(hashtext($1))` com `collector-anatel-pst`.
   Sem a trava: `outra execução do collector-anatel-pst está aplicando dados`
   (só log; nenhuma linha em `anatel_pst_run`).
2. Carga das tabelas temporárias (texto vazio, código IBGE zero e data zero
   do parser viram NULL):

   ```sql
   CREATE TEMP TABLE stage_provider (
       document       text PRIMARY KEY,
       name           text NOT NULL,
       trade_name     text,
       street         text,
       number         text,
       complement     text,
       district       text,
       postal_code    text,
       city_ibge_code integer,
       city           text,
       state          text,
       phone          text,
       email          text
   ) ON COMMIT DROP;
   CREATE TEMP TABLE stage_service (
       document             text NOT NULL,
       entity_type          text NOT NULL,
       grant_type           text NOT NULL,
       grant_fistel         text,
       grant_process        text,
       granted_on           date,
       service_group        text NOT NULL,
       service_code         text NOT NULL,
       service_name         text NOT NULL,
       notification_fistel  text NOT NULL,
       notification_process text,
       notified_on          date,
       UNIQUE NULLS NOT DISTINCT (document, grant_fistel, notification_fistel, service_code)
   ) ON COMMIT DROP;
   ```

   `COPY` de todas as prestadoras e de todos os serviços (com o CNPJ da
   prestadora em `document`); depois `ANALYZE stage_provider; ANALYZE stage_service`.
   O `UNIQUE` de `stage_service` só confirma o que o parser já garante (um
   serviço repetido é erro de código, não da fonte).
3. **Trava de remoção** (pulada com `--force`): conta as prestadoras de
   `anatel_pst_provider` cujo `document` não está em `stage_provider` e os
   serviços de `anatel_pst_service` cuja chave natural (o CNPJ da prestadora,
   `grant_fistel` com NULLs iguais, `notification_fistel`, `service_code`)
   não está em `stage_service`. Tabela vazia não trava. Se removidas / atuais
   passar de `REMOVAL_THRESHOLD` (prestadoras conferidas primeiro), recusa:
   `o arquivo removeria <r> de <n> prestadoras (<x.x>%, limite <y.y>%); use --force se for legítimo`
   (ou `... serviços ...`).
4. Prestadoras novas e alteradas:

   ```sql
   MERGE INTO anatel_pst_provider t
   USING stage_provider s ON t.document = s.document
   WHEN MATCHED AND (t.name, t.trade_name, t.street, t.number, t.complement, t.district,
                     t.postal_code, t.city_ibge_code, t.city, t.state, t.phone, t.email)
         IS DISTINCT FROM
                    (s.name, s.trade_name, s.street, s.number, s.complement, s.district,
                     s.postal_code, s.city_ibge_code, s.city, s.state, s.phone, s.email) THEN
       UPDATE SET name = s.name, trade_name = s.trade_name, ... , email = s.email
   WHEN NOT MATCHED THEN
       INSERT (document, name, trade_name, ..., email) VALUES (s.document, s.name, s.trade_name, ..., s.email)
   RETURNING merge_action()
   ```

5. Serviços novos, alterados e removidos, com o `provider_uuid` resolvido pelo
   CNPJ (já gravado no passo 4). A chave natural compara `grant_fistel` com
   `IS NOT DISTINCT FROM` (NULL = NULL nas entidades dispensadas, como o
   `UNIQUE NULLS NOT DISTINCT` da tabela); as colunas de dado são as demais:

   ```sql
   MERGE INTO anatel_pst_service t
   USING (SELECT p.uuid AS provider_uuid, s.*
            FROM stage_service s
            JOIN anatel_pst_provider p ON p.document = s.document) s
      ON t.provider_uuid = s.provider_uuid
     AND t.grant_fistel IS NOT DISTINCT FROM s.grant_fistel
     AND t.notification_fistel = s.notification_fistel
     AND t.service_code = s.service_code
   WHEN MATCHED AND (t.entity_type, t.grant_type, t.grant_process, t.granted_on, t.service_group,
                     t.service_name, t.notification_process, t.notified_on)
         IS DISTINCT FROM
                    (s.entity_type, s.grant_type, s.grant_process, s.granted_on, s.service_group,
                     s.service_name, s.notification_process, s.notified_on) THEN
       UPDATE SET entity_type = s.entity_type, ... , notified_on = s.notified_on
   WHEN NOT MATCHED BY TARGET THEN
       INSERT (provider_uuid, entity_type, ..., notified_on) VALUES (s.provider_uuid, s.entity_type, ..., s.notified_on)
   WHEN NOT MATCHED BY SOURCE THEN
       DELETE
   RETURNING merge_action()
   ```

   Os serviços das prestadoras que sumiram não estão na origem e saem aqui:
   `service_deleted` conta **também** os serviços que a cascata apagaria.
6. Prestadoras que sumiram, **depois** dos serviços:
   `DELETE FROM anatel_pst_provider p WHERE NOT EXISTS (SELECT 1 FROM stage_provider s WHERE s.document = p.document)`.
   Como no `cgibr`, difere do padrão (um `MERGE` que também apaga) por causa
   da cascata de `fk_anatel_pst_service_provider`: apagando a prestadora
   antes, os serviços dela sairiam sem passar pelo `MERGE` e ficariam fora de
   `service_deleted`. Nesta ordem, a cascata não encontra nada.
7. `INSERT` em `anatel_pst_run` com `status = 1`, todas as colunas de
   [dados.md](dados.md#anatel_pst_run) (download, `csv_*`, contagens do
   parser, as seis alterações, `warnings`); o `uuid` devolvido é a versão
   nova.
8. `jobs`, como no [padrão](../../../padroes/coletor.md#contrato-com-a-tabela-jobs):

   ```sql
   INSERT INTO jobs (app, last_sync_at, last_check_at, consolidated) VALUES ($1, NOW(), NOW(), 0)
   ON CONFLICT (app) DO UPDATE SET
       last_sync_at  = NOW(),
       last_check_at = NOW(),
       consolidated  = CASE WHEN $2 THEN 0 ELSE jobs.consolidated END
   ```

   `$1` = `collector-anatel-pst`; `$2` = a soma das seis alterações é maior
   que zero. Um arquivo novo com o mesmo conteúdo lógico (um `--force`, ou só
   a ordem das linhas mudou) gera versão nova e não pede consolidação.
9. `COMMIT`.

Sem mudança, só:
`INSERT INTO jobs (app, last_check_at) VALUES ($1, NOW()) ON CONFLICT (app) DO UPDATE SET last_check_at = NOW()`.

A última execução aplicada (checagens e validadores) vem de
`SELECT uuid::text, url, coalesce(etag, ''), coalesce(last_modified, ''), coalesce(sha256, ''), coalesce(csv_sha256, ''), csv_modified_at, created_at FROM anatel_pst_run WHERE status = 1 ORDER BY created_at DESC LIMIT 1`.

## Recusas e falhas

| Situação | Linha em `anatel_pst_run` | O que a linha traz além de `url`, `forced`, `started_at` | Mensagem (`err` do log `verificação falhou`) |
|---|---|---|---|
| banco fora ao ler a última execução | não | — | `lendo o último arquivo aplicado: <erro>` |
| download falhou (rede, 4xx, 5xx depois das tentativas, limite) | não | — | `download: <erro>` |
| `jobs` não atualizou numa verificação sem mudança | não | — | `jobs: <erro>` |
| outra execução aplicando | não | — | `outra execução do collector-anatel-pst está aplicando dados` |
| ZIP recusado | `status = 0` | download (`http_status`, `etag`, `last_modified`, `sha256`, `bytes`); `csv_*` e contagens NULL; `warnings` `[]` | `ZIP inválido: ...`, `ZIP sem CSV`, `ZIP com <n> arquivos CSV: ...`, `CSV ... maior que ...`, `lendo ... do ZIP: ...` |
| parser recusou | `status = 0` | download + `csv_name`, `csv_sha256`, `csv_bytes`, `csv_modified_at`; contagens NULL; `warnings` `[]` | `parser: ...` (acima de 1% descartado, os 3 primeiros avisos vão na mensagem) |
| menos de `MIN_PROVIDERS` | `status = 0` | download + `csv_*` + contagens (`rows` … `services`) e `warnings` | `só <n> prestadoras no arquivo ...` |
| trava de remoção | `status = 0` | idem | `o arquivo removeria ...` |
| erro no banco durante a aplicação | `status = 0` | idem | o erro, com o passo (ex.: `staging: ...`, `copy stage_service: ...`, `trava de remoção: ...`, `merge anatel_pst_provider: ...`, `merge anatel_pst_service: ...`, `delete anatel_pst_provider: ...`, `anatel_pst_run: ...`, `jobs: ...`) |

- Numa recusa, as seis alterações ficam NULL e as tabelas de dados ficam como
  estavam (a aplicação não começou, ou a transação foi desfeita).
- A linha de recusa é gravada numa instrução à parte, fora da transação, com
  um contexto sem cancelamento (`context.WithoutCancel`): é gravada mesmo com o
  `RUN_TIMEOUT` estourado. Se ela falhar, o log diz
  `não consegui gravar a falha em anatel_pst_run`.
- `POSTGRES_URL` inválida (`POSTGRES_URL inválida: ...`) é tratada como
  qualquer falha de conexão na subida: novas tentativas por 2 minutos e depois
  `sem conexão com o Postgres`, saída 1.

### Avisos

Como no [padrão](../../../padroes/coletor.md#recusas-e-falhas):

- `anatel_pst_run.warnings` recebe os 50 primeiros avisos do parser e, se
  houve mais, um último item `... e mais <n> avisos` (até 51 itens).
- No log, cada aviso sai numa linha `aviso do parser` (campo `warning`) **só
  quando o arquivo é aplicado**, logo depois de `arquivo novo aplicado`; numa
  recusa depois do parser, ficam só em `anatel_pst_run.warnings` (numa recusa
  do próprio parser, `warnings` fica `[]` e os 3 primeiros vão na mensagem).
  Os avisos de um CSV mais antigo que o aplicado não existem (ele não chega
  ao parser).
- O arquivo de 2026-09-30 não gera nenhum aviso.

## `--force`

Além do [padrão](../../../padroes/coletor.md#--force): não manda validadores
(não há `304`), pula as checagens 2, 3 e 4 e ignora a trava de remoção; ZIP,
parser e `MIN_PROVIDERS` continuam valendo. A linha sai com `forced = true`,
e a versão é nova mesmo que nada mude.

## Opções

As [comuns](../../../padroes/coletor.md#configuração-comum), com
`SYNC_INTERVAL=24h` (verificar uma vez por dia e aplicar só se mudou: a
Anatel regera o arquivo diariamente), `RETRY_INTERVAL=5m`, `RUN_TIMEOUT=10m`
e `REMOVAL_THRESHOLD=0.05` (fração de prestadoras ou de serviços), mais:

| Variável | Argumento | Padrão | Uso |
|---|---|---|---|
| `SOURCE_URL` | `--source-url` | `https://www.anatel.gov.br/dadosabertos/paineis_de_dados/outorga_e_licenciamento/prestadoras_servicos_telecomunicacoes.zip` | o ZIP; tem de começar com `http://` ou `https://` |
| `MIN_PROVIDERS` | `--min-providers` | `30000` | inteiro ≥ 0; abaixo disso o arquivo é recusado como truncado |

Não há `SOURCE_SHA256_URL` (a fonte não publica hash).

`collector-anatel-pst --help` (sai com 0; escreve no stderr):

```
collector-anatel-pst — importa as prestadoras de serviços de telecomunicações
da Anatel (pessoas jurídicas e os serviços notificados por elas) para as
tabelas anatel_pst_* do PostgreSQL do BadBlock.

Verifica a fonte a cada SYNC_INTERVAL e só aplica quando o arquivo muda
(ETag/Last-Modified, hash do ZIP, hash e data do CSV). Não tem API HTTP.

Uso:
  collector-anatel-pst [opções]

Opções (padrão → variável de ambiente → argumento):
  --log-format           json ou text
                         env LOG_FORMAT, padrão json
  --log-level            debug, info, warn ou error
                         env LOG_LEVEL, padrão info
  --min-providers        abaixo disso o arquivo é tratado como truncado e recusado
                         env MIN_PROVIDERS, padrão 30000
  --postgres-url         URL do Postgres, ex.: postgres://postgres:senha@badblock-postgres:5432/badblock (obrigatória)
                         env POSTGRES_URL, padrão ""
  --removal-threshold    fração máxima de prestadoras ou serviços removidos de uma vez sem --force
                         env REMOVAL_THRESHOLD, padrão 0.05
  --retry-interval       espera até a próxima tentativa depois de uma verificação que falhou
                         env RETRY_INTERVAL, padrão 5m
  --run-timeout          tempo máximo de uma verificação (download + aplicação)
                         env RUN_TIMEOUT, padrão 10m
  --source-url           ZIP prestadoras_servicos_telecomunicacoes.zip da Anatel
                         env SOURCE_URL, padrão https://www.anatel.gov.br/dadosabertos/paineis_de_dados/outorga_e_licenciamento/prestadoras_servicos_telecomunicacoes.zip
  --sync-interval        intervalo entre verificações da fonte
                         env SYNC_INTERVAL, padrão 24h
  --user-agent           User-Agent das requisições (vazio = badblock-collector-anatel-pst/<versão>)
                         env USER_AGENT, padrão ""
  --once                 faz uma verificação e sai
  --force                aplica mesmo sem mudança e ignora a trava de remoção (implica --once)
  --version              mostra a versão e sai
  -h, --help             mostra esta ajuda
```

Opções inválidas (stderr `collector-anatel-pst: <mensagem>`, saída 2):

| Caso | Mensagem |
|---|---|
| duração que `time.ParseDuration` não lê (ex.: `1d`) ou ≤ 0 | `--sync-interval inválido: "<valor>"` (idem `--retry-interval`, `--run-timeout`) |
| `MIN_PROVIDERS` não inteiro ou negativo | `--min-providers inválido: "<valor>"` |
| `REMOVAL_THRESHOLD` fora de 0 a 1 | `--removal-threshold inválido (0 a 1): "<valor>"` |
| `LOG_LEVEL` fora de `debug`, `info`, `warn`, `error` (em qualquer caixa) | `--log-level inválido: "<valor>"` |
| `LOG_FORMAT` fora de `json`, `text` (em qualquer caixa) | `--log-format inválido: "<valor>"` |
| `SOURCE_URL` sem `http://`/`https://` | `--source-url precisa ser http(s): "<valor>"` |
| sem `POSTGRES_URL` (dispensada com `--version`) | `defina POSTGRES_URL (ou --postgres-url)` |
| argumento que não é opção | `argumento inesperado: <arg>` |

A linha `iniciando` traz `version`, `commit`, `source` (a `SOURCE_URL`),
`interval`, `retry`, `min_providers` e `postgres` (sem a senha); não inclui
`RUN_TIMEOUT` nem `REMOVAL_THRESHOLD`.

## Compose e `.env`

| Variável no `.env` | Vira | Padrão no compose | `.env.example` do app | `.env.example` da raiz |
|---|---|---|---|---|
| `COLLECTOR_ANATEL_PST_TAG` | tag da imagem e `VERSION` do build | `latest` (`dev` no build) | `latest` | `latest` |
| `COLLECTOR_ANATEL_PST_SYNC_INTERVAL` | `SYNC_INTERVAL` | `24h` | `24h` | `24h` |
| `COLLECTOR_ANATEL_PST_RETRY_INTERVAL` | `RETRY_INTERVAL` | `5m` | `5m` | `5m` |
| `COLLECTOR_ANATEL_PST_SOURCE_URL` | `SOURCE_URL` | a URL padrão acima | — | — |
| `COLLECTOR_ANATEL_PST_MIN_PROVIDERS` | `MIN_PROVIDERS` | `30000` | `30000` | `30000` |
| `COLLECTOR_ANATEL_PST_REMOVAL_THRESHOLD` | `REMOVAL_THRESHOLD` | `0.05` | `0.05` | `0.05` |

O compose (`name: badblock-collector-anatel-pst`, serviço
`collector-anatel-pst`, container `badblock-collector-anatel-pst`) passa
também as comuns `POSTGRES_URL` (montada com `POSTGRES_PASSWORD` se ausente)
e `LOG_LEVEL`, e fixa `LOG_FORMAT: json`. `RUN_TIMEOUT` e `USER_AGENT` não
passam pelo compose (valem os padrões do app), e
`COLLECTOR_ANATEL_PST_SOURCE_URL` não está nos `.env.example` — o que a
[regra das opções](../../../projeto/convencoes.md#configuração) permite: só
entra ali o que o deploy precisa ajustar.

## Operação

```bash
make -C apps/anatel/pst/collector once    # verificação agora, no container no ar
make -C apps/anatel/pst/collector force   # reaplica o arquivo atual (ignora "sem mudança", o CSV mais antigo e a trava de remoção)
make -C apps/anatel/pst/collector logs
make -C database/postgres psql            # e: SELECT created_at, status, forced, providers, services, csv_modified_at, error FROM anatel_pst_run ORDER BY created_at DESC LIMIT 5;
```

A saúde se vê em `jobs.last_check_at` da linha `collector-anatel-pst` (anda a
cada `SYNC_INTERVAL`), no bloco `collector` de `GET /anatel/pst/meta` e nos
logs (JSON, `app=collector-anatel-pst` em toda linha):

| Mensagem | Nível | Campos |
|---|---|---|
| `iniciando` | info | acima |
| `fonte sem mudança` | info | `reason`, `version`, `ms` |
| `arquivo novo aplicado` | info | `version`, `sha256`, `csv_sha256`, `csv_modified_at`, `rows`, `rows_cpf`, `duplicates`, `skipped`, `providers`, `services`, `provider_inserted`, `provider_updated`, `provider_deleted`, `service_inserted`, `service_updated`, `service_deleted`, `warnings` (quantidade), `forced`, `ms` |
| `aviso do parser` | warn | `warning` |
| `entrada do ZIP ignorada` | warn | `name` |
| `csv mais antigo que o aplicado; ignorado (espelho ou servidor com cópia velha?)` | warn | `csv_modified_at`, `applied_csv_modified_at`, `sha256`, `csv_sha256` |
| `verificação falhou` | error | `err`, `ms` |
| `não consegui gravar a falha em anatel_pst_run` | error | `err` |
| `Postgres indisponível; tentando de novo` / `sem conexão com o Postgres` | warn / error | `err` |
| `próxima verificação` | debug | `in` |
| `encerrando` | info | — |

## Medições

Arquivo de 2026-09-30 (ZIP de 14.734.094 bytes, CSV de 79.203.643 bytes,
entrada de `2026-09-30 06:15:08` em Brasília), num Mac M-series com o
Postgres 18 no Docker Desktop:

| Etapa | Tempo |
|---|---|
| download (Cloudflare, `cf-cache-status: HIT`) | ~0,3 s |
| extração do CSV (`archive.Extract`, com SHA-256) | 0,3 a 0,6 s |
| parser (276.261 linhas) | 0,3 a 1,0 s |
| primeira carga (banco vazio: 45.074 + 54.314 inserções) | ~6,9 s (`make test-real`) |
| reaplicação sem alterações (`--force`) | ~2,5 s |
| verificação completa com o binário, banco vazio (`--once`) | 5,7 s |
| verificação seguinte (`HTTP 304`) | 70 ms |

- Contagens (iguais às de [fonte.md](fonte.md#fatos-medidos-2026-09-30)):
  276.261 linhas (64.902 de CNPJ, 211.359 de CPF), 10.588 cópias exatas, 0
  descartadas, **45.074 prestadoras e 54.314 serviços**, 0 avisos; SCM (045)
  20.510 prestadoras, STFC (171) 1.661, SeAC (750) 1.446, SMP (010) 27; 55
  códigos de serviço.
- Tabelas `anatel_pst_provider` + `anatel_pst_service` com índices: ~50 MB.
- As consultas da API usam os índices: `document` (`uq_anatel_pst_provider_document`),
  busca por nome (os dois GIN trigram, `BitmapOr`) e prestadoras de um
  serviço (`ix_anatel_pst_service_service_code`).

## Testes específicos

Unitários (`make test`; `parse`, `archive` e `collector` usam a fixture de
[fonte.md](fonte.md#fixture)):

| Pacote | Teste | Confere |
|---|---|---|
| `archive` | `TestExtractSample` | `pst-sample.zip`: nome, bytes iguais ao `pst-sample.csv`, SHA-256, data `09:15:08 UTC` |
| `archive` | `TestExtractDOSTimeIsBrasilia`, `TestExtractIgnoresExtendedTimestamp`, `TestExtractWithoutDate` | só MS-DOS → UTC−3; com timestamp estendido vale o MS-DOS; sem data → zero |
| `archive` | `TestExtractIgnoresOtherEntries`, `TestExtractRejects`, `TestExtractRejectsCorruptEntry` | outras entradas e pastas ignoradas, `.CSV` em maiúsculas; não-ZIP, sem CSV, dois CSVs, acima do limite; dados corrompidos |
| `parse` | `TestParseSample` | 158 linhas (155 de CNPJ, 3 de CPF), 103 cópias, 0 descartes, 8 prestadoras na ordem do arquivo, 52 serviços, 0 avisos; Telefonica com 42 serviços; SCM com nome fantasia; duas outorgas com o mesmo Fistel de notificação; aspas com `""` e com `;`; `-`, vazio, espaço e `N/I` → NULL e `.` mantido; dispensada com outorga NULL |
| `parse` | `TestParseSampleZip` | o mesmo pelo ZIP |
| `parse` | `TestParseWithoutBOMAndLF`, `TestParseTrimAndInvalidUTF8`, `TestParseKeepsLiteralValues` | sem BOM e com LF; espaços nas pontas e UTF-8 inválido → `U+FFFD`; `0` e `S/N` ficam |
| `parse` | `TestParseCPFIgnored`, `TestParseExactCopy` | CPF ignorado sem aviso (mesmo com outro número de campos); cópia exata (também com espaços) contada em `Duplicates` |
| `parse` | `TestParseRepeatedService`, `TestParseInvalidCityAndState` | serviço repetido e dados de prestadora diferentes: vale a primeira, um aviso por CNPJ; chave com `grant_fistel` NULL; IBGE e UF inválidos → NULL com aviso |
| `parse` | `TestParseSkippedLines`, `TestParseSkippedLimit` | cada motivo de descarte da [tabela](fonte.md#linhas) (inclusive 23 campos), com a mensagem; 2% recusa, 1% passa |
| `parse` | `TestParseRejects`, `TestParseWarningsCap` | vazio, só BOM, só cabeçalho, coluna trocada, 25 colunas, separador `,`, aspas quebradas e sem fechar; 50 avisos guardados de 60 |
| `fetch` | `TestDownloadConditional` | `ETag`/`Last-Modified` lidos, SHA-256 do corpo, `304` com `If-None-Match` e com `If-Modified-Since` |
| `fetch` | `TestDownloadRetriesServerErrors`, `TestDownloadGivesUpAfterRetries`, `TestDownloadDoesNotRetry404`, `TestDownloadMaxBytes` | 502 duas vezes e depois 200; 503 três vezes; 404 uma vez só; corpo acima do limite |
| `config` | `TestDefaults`, `TestPrecedence`, `TestForceImpliesOnce`, `TestVersionWithoutPostgres`, `TestInvalid`, `TestHelp`, `TestRedact` | padrões (`24h`, `5m`, `10m`, `30000`, `0.05`); argumento > ambiente > padrão; cada mensagem de opção inválida |
| `collector` | `TestFirstRunApplies` | banco vazio aplica; a execução leva contagens, SHA-256 do ZIP e do CSV, `csv_modified_at` |
| `collector` | `TestNotModifiedSendsValidators`, `TestOtherURLSendsNoValidators` | `304` = sem mudança com os validadores anteriores; outra URL não os envia |
| `collector` | `TestSameZipIsUnchanged`, `TestSameCSVInNewZipIsUnchanged` | checagens 2 e 3 |
| `collector` | `TestOlderCSVIsUnchanged` | checagem 4 com o `reason`; `--force` aplica a cópia velha; data mais nova aplica (com entrada extra no ZIP) |
| `collector` | `TestForceAppliesSameFile` | `--force` aplica o mesmo arquivo, sem validadores, com `forced` |
| `collector` | `TestBadZipIsRecorded`, `TestParserErrorIsRecorded`, `TestTooFewProvidersIsRecorded`, `TestRemovalErrorIsRecorded` | recusas gravadas, com o que se sabe (ZIP sem `csv_*`, parser sem contagens, mínimo com contagens) |
| `collector` | `TestBusyIsNotRecorded`, `TestDownloadErrorIsNotRecorded` | lock ocupado e download falho: erro sem linha de recusa |

Integração (`make test-int`): em `internal/store`, num `postgres:18-trixie`
descartável (banco `badblock`, usuário `postgres`, senha `pg`), com só o
`migrate:up` de `database/postgres/central/` e `database/postgres/anatel_pst/`
(o teste sobe seis níveis até a raiz: o app está um nível mais fundo que os
das fontes de um nível):

- `TestApplyLifecycle`:
  1. banco vazio: nenhuma execução aplicada;
  2. carga da fixture: 8 prestadoras e 52 serviços inseridos, `jobs` com
     `last_sync_at`, `last_check_at` e `consolidated = 0`; 42 serviços da
     Telefonica; a dispensada com outorga NULL; aspas e `;` gravados; a
     linha de `anatel_pst_run` com as contagens e `csv_modified_at`;
     `LastApplied` com `csv_sha256` e `csv_modified_at`;
  3. `consolidated = 1` (como a fase 2) e a mesma carga de novo: 0
     alterações (inclusive a dispensada: NULL = NULL), versão nova,
     `consolidated` continua `1`;
  4. sai a Vigillare (4 serviços), a WHIM muda de nome, um serviço da
     Telefonica muda de data, outro sai e um entra, chega uma prestadora
     nova, com limite `0.2`: prestadoras 1/1/1 e serviços 2 inseridos, 1
     alterado e **5 apagados** (os 4 da prestadora removida entram na conta);
     o `uuid` da Telefonica se mantém; nenhum serviço órfão;
  5. a cascata da FK apaga os serviços de uma prestadora apagada à mão;
  6. trava de remoção de prestadoras (2 de 7) e de serviços (30 de 48), sem
     mexer nas tabelas; com `--force`, 5 prestadoras apagadas;
  7. `RecordFailure` grava `status = 0` com `error`, `csv_*` e contagens NULL,
     e não vira versão;
  8. `TouchCheck` só anda `last_check_at`.
- `TestApplyBusy`: com o advisory lock tomado, `ErrBusy` e nenhuma linha em
  `anatel_pst_run`.

Arquivo real (`make test-real`): o ZIP do dia, baixado com `curl` e passado
em `FILE` (vira `ANATEL_PST_REAL_FILE`; também aceita o CSV já extraído):

```bash
curl -o /tmp/pst.zip https://www.anatel.gov.br/dadosabertos/paineis_de_dados/outorga_e_licenciamento/prestadoras_servicos_telecomunicacoes.zip
make -C apps/anatel/pst/collector test-real FILE=/tmp/pst.zip
```

`TestParseRealFile` (extração e parser, com tempos, contagens e prestadoras
por serviço; falha com descarte ou abaixo de 30.000 prestadoras) e
`TestApplyRealFile` (PG18 descartável: primeira carga, reaplicação
idempotente, contagens das tabelas, tamanho e os planos das consultas da
API). Os números estão em [Medições](#medições).
