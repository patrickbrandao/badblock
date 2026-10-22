# Coletor `collector-registrobr-rdapnum`

O que o `collector-registrobr-rdapnum` faz além do
[padrão dos coletores](../../../padroes/coletor.md) e do
[padrão de sonda ativa](../../../padroes/sonda.md), que ele segue. Alvos,
consultas e respostas: [fonte.md](fonte.md). Tabelas: [dados.md](dados.md) e
[dados-processados.md](dados-processados.md). Dono: sub-agente
`collector-registrobr-rdapnum`.

## Valores desta fonte

| Item | Valor |
|---|---|
| App e `jobs.app` | `collector-registrobr-rdapnum` |
| Tabelas escritas | todas as `registrobr_rdapnum_*` (menos `_config`) e a linha dele em `jobs` |
| Tabelas lidas de outra fonte | `cgibr_run`, `cgibr_asn`, `cgibr_prefix` (só na derivação) |
| Tabela temporária | `stage_target` (`ON COMMIT DROP`) |
| Conexão | pool pgx de no máximo **4** conexões (fila, processamento, batida e derivação correm juntos), ociosas fechadas em 5 min; `application_name=collector-registrobr-rdapnum` |
| Valkey | cliente com `ClientName` `badblock-collector-registrobr-rdapnum`, `MaxRetries` 0, pool de 2, 1 s de prazo fora do `BLPOP` |
| Dependência a mais | `github.com/redis/go-redis/v9` (a mesma do `rdns`); o HTTP, o proxy e o JSON são da biblioteca padrão |
| Descrição da imagem | `Coleta o RDAP do registro.br de cada ASN e bloco brasileiro para o PostgreSQL do BadBlock` |

## Desvios do padrão de sonda

| Padrão de sonda | Neste coletor |
|---|---|
| sem `SOURCE_URL` e `USER_AGENT` (não há download) | há HTTP: `RDAP_URL` e o `User-Agent` do padrão de nomes |
| `rate_limit` em consultas por segundo | **por minuto** (decisão #41): o limite do servidor se mede em fichas por minuto |
| grava o resultado direto | grava a resposta **crua** e processa depois, noutra transação (decisão #38) |
| tempo real: a API espera a resposta | **atualização**: a API só pede; o coletor marca `rescan` e ninguém espera (decisão #42) |
| falha = estado gravado no resultado | truncada e erro **não concluem**: o alvo volta com espera (`retry_at`) |
| `SCAN_INTERVAL` padrão `24h` | `168h` (7 dias) |

## Estrutura do código

```
apps/registrobr/rdapnum/collector/
├── cmd/collector-registrobr-rdapnum/main.go
├── internal/
│   ├── buildinfo/, config/
│   ├── store/      pgx: derivação, fila, respostas, processadas, _worker, _config, jobs
│   ├── derive/     uma passada da derivação
│   ├── rdap/       cliente HTTP (proxy, prazos), classificação (completa, truncada, 404, erro), aviso
│   ├── parse/      resposta completa → linhas das processadas (PARSER_VERSION)
│   ├── limiter/    taxa por instância, por minuto
│   ├── scan/       laço da fila: pega, consulta, grava a crua, processa
│   ├── reprocess/  laço do que falta processar (parser novo, falha anterior)
│   ├── refresh/    laço da fila de atualização (BLPOP no Valkey)
│   └── worker/     batida e releitura de _config a cada 30 s; uuid da instância
├── testdata/       respostas reais e sintéticas (fonte.md#fixtures)
└── os arquivos de todo app (Dockerfile, compose, Makefile, vendor/…)
```

`main.go`: passos 1 a 4 do [padrão](../../../padroes/coletor.md#maingo) e os
do [`rdns`](../../rdns/collector.md#maingo) (`--once`/`--force`, Valkey,
linha em `_worker`, laços, encerramento em até 10 s), com os laços desta
fonte: derivação, batida, fila, reprocessamento e atualização.

## Derivação

A passada da [sonda](../../../padroes/sonda.md#derivação) com `SOURCES` =
`cgibr`, como no [`rdns`](../../rdns/collector.md#derivação) (trava de
sessão `pg_try_advisory_lock(hashtext('collector-registrobr-rdapnum'))`,
`REPEATABLE READ`, versão de `cgibr_run`, `RUN_TIMEOUT`):

1. `stage_target (kind text, asn bigint, prefix cidr)` com cada
   `cgibr_asn.asn` (`asn`) e cada `cgibr_prefix.prefix` (`prefix`).
2. Novos: `INSERT INTO registrobr_rdapnum_target (kind, asn, prefix, source)
   SELECT kind, asn, prefix, 'cgibr' FROM stage_target ON CONFLICT DO
   NOTHING` — o que já existe (derivado ou estático) fica como está. Não há
   cadastro a corrigir: a chave é o próprio ASN ou bloco.
3. `not_in_source`: alvos `cgibr` que não estão em `stage_target`;
   `INSERT` em `_run` com `status = 1`.
4. Manutenção, ainda com a trava: instâncias sem sinal há mais de 24 h e
   **entidades sem nenhuma ligação** (`DELETE FROM registrobr_rdapnum_entity e
   WHERE NOT EXISTS (SELECT 1 FROM registrobr_rdapnum_link l WHERE
   l.entity_uuid = e.uuid OR l.via_uuid = e.uuid)`).
5. `jobs`: `last_check_at` em toda passada com a trava; `last_sync_at` e
   `consolidated = 0` se `inserted > 0` ou se alguma entidade órfã saiu (o
   dado processado mudou).

Com o arquivo de 2026-09-28, a primeira derivação insere 31.125 alvos. Log
`derivação aplicada` com `source`, `version`, `asns`, `prefixes`,
`inserted`, `not_in_source`, `forced` e `ms`.

## Fila e lease

A [fila da sonda](../../../padroes/sonda.md#fila-de-varredura) sobre
`registrobr_rdapnum_target`, sem regra de alvo e com a espera:

```sql
WHERE active
  AND (scan_worker IS NULL OR scan_started_at < NOW() - $2::interval)
  AND (retry_at IS NULL OR retry_at <= NOW())
  AND (rescan OR last_scan_at IS NULL OR last_scan_at < NOW() - $3::interval)
ORDER BY rescan DESC, last_scan_at ASC NULLS FIRST
LIMIT 1 FOR UPDATE SKIP LOCKED
```

- **Dois coletores nunca pegam o mesmo alvo**: o `SKIP LOCKED` e o lease
  (`SCAN_LEASE`, `2m`) garantem, como no `rdns`.
- Um alvo por vez por instância: a 32/min sai uma consulta a cada 1,9 s, e
  cada uma leva ~0,1 s ([fonte.md](fonte.md#consulta)). A instância só pega
  o próximo alvo quando o limitador libera a vez.
- O `rescan` respeita `retry_at`: um alvo que acabou de vir truncado não
  volta antes da espera, mesmo pedido.
- Sem alvo vencido: olha a fila de novo em 30 s.

## Coleta de um alvo

1. Espera a vez no [limitador](#taxa) e faz o `GET`
   ([fonte.md](fonte.md#consulta)), com prazo de `HTTP_TIMEOUT` (`20s`),
   corpo até **1 MiB**, até 3 redirecionamentos no mesmo host.
2. Classifica:

   | Resposta | `result` | Conclusiva? |
   |---|---|---|
   | 200, JSON válido, `objectClassName` esperado, sem a marca de truncagem | `complete` | sim |
   | 200 com a marca ([fonte.md](fonte.md#resposta-truncada)) | `truncated` | não |
   | 404 | `not_found` | sim |
   | qualquer outro status (`429`, `5xx`…), prazo, erro de rede ou de TLS, corpo maior que 1 MiB, JSON inválido, `objectClassName` errado | `error` | não |

3. **Grava a crua** (transação 1, sempre que não é `error`): a
   [resposta igual](dados.md#resposta-igual) só anda `last_seen_at`, a
   diferente vira linha nova; e o `UPDATE` do alvo com a conferência do
   lease (`WHERE uuid = $1 AND scan_worker = $2 AND scan_started_at = $3`):
   - conclusiva: `last_scan_at = NOW()`, `response_uuid` = a resposta,
     `failures = 0`, `retry_at = NULL`, `last_error = NULL`, `rescan`
     zerado se era o lido ao pegar, `scan_worker`/`scan_started_at` = NULL;
   - truncada ou erro: `failures + 1`, `retry_at` pela
     [espera](#truncada-e-erros), `last_error` (no erro), e o alvo livre.

   Sempre `last_attempt_at`, `last_result` e `last_http_status`. Lease
   perdido: `ROLLBACK`, log `lease perdido; resposta descartada`.
4. **Processa** (transação 2, só conclusiva): o [processamento](#processamento)
   da resposta atual. Falhou: a crua fica com `process_error` e o dado
   processado anterior continua; o [reprocessamento](#reprocessamento) tenta
   de novo.

Log `alvo coletado` (`debug`) com `kind`, `target`, `result`, `http_status`,
`egress_ip`, `changed` (linha nova na crua) e `ms`.

## Truncada e erros

- **Espera do alvo** (`retry_at = NOW() + espera[failures]`): **1 min**,
  **5 min**, **30 min**, **2 h**, depois **6 h** em toda tentativa seguinte.
  Não há desistência: o alvo segue tentando a cada 6 h até uma conclusiva, e
  o `/meta` conta quantos estão nessa situação.
- **Pausa da instância**: `TRUNCATED_PAUSE_AFTER` (`3`) truncadas seguidas na
  instância → para de consultar por `TRUNCATED_PAUSE` (`2m`), grava
  `paused_until` em `_worker` e loga `respostas truncadas seguidas;
  pausando` (`warn`, `count`, `egress_ip`, `until`). Uma completa zera a
  conta. Com o balde medido em [fonte.md](fonte.md#limite-do-servidor), 2 min
  recarregam ~16 fichas do IP.
- A truncada **nunca** é processada e nunca vira `response_uuid`: o dado bom
  anterior continua na API (decisão #40).
- `429` e `5xx` contam como erro (espera do alvo), não como truncada (não
  pausam a instância).

## Proxy

- `PROXY_URL` (opcional): `http://`, `https://` ou `socks5://`, com
  `usuário:senha@` se o proxy pedir. Vale para toda consulta da instância
  (`http.Transport.Proxy`); o `CONNECT` leva o TLS do registro.br de ponta a
  ponta. **Rodar o IP de saída é do proxy**, não do coletor.
- Segredo: só no ambiente (`.env`, fora do git); nunca no banco, no `/meta`
  nem nos logs — a linha `iniciando` mostra `proxy` com a senha trocada por
  `xxxxx` ([../../../plataforma/seguranca.md](../../../plataforma/seguranca.md)).
- Cada instância pode ter o seu proxy: num host, réplicas com o mesmo
  `.env` usam o mesmo; para proxies diferentes, uma pasta (projeto compose)
  por proxy ou outro host.
- **Conferência da rotação**: o coletor lê o IP de saída de cada resposta
  ([fonte.md](fonte.md#aviso-com-o-ip-de-quem-consultou)). Com `PROXY_URL`,
  `PROXY_SAME_IP_WARN` (`10`) respostas seguidas com o mesmo IP → `proxy sem
  rotação de IP` (`warn`, `egress_ip`, `count`), uma vez por sequência.
  `_worker.egress_ips` mostra quantos IPs distintos saíram nas últimas 100.
- Erro do proxy (conexão recusada, `407`, `CONNECT` falho) é `error` do
  alvo; o log diz `falha no proxy` (`warn`, `err`).

## Taxa

- `internal/limiter`: uma consulta a cada `60 s / rate_limit`, sem rajada,
  por instância. Toda consulta conta (inclusive as que vêm truncadas ou
  dão erro). A taxa vale **por coletor**: 10 coletores a 32/min fazem
  320/min (decisão #41).
- `rate_limit` e `scan_interval` de `_config` a cada 30 s
  ([sonda](../../../padroes/sonda.md#taxa-e-configuração-em-tempo-de-execução)),
  com `RATE_LIMIT` (`32`) e `SCAN_INTERVAL` (`168h`) de reserva; valor
  inválido: `valor inválido em registrobr_rdapnum_config; usando a
  reserva`.
- Sem `PROXY_URL` e com `rate_limit` acima de **8**: aviso `taxa acima de
  8/min sem proxy: espere respostas truncadas` na subida e quando a taxa
  muda ([fonte.md](fonte.md#limite-do-servidor)).
- Capacidade: uma volta de 31.125 alvos leva ~**16 h** a 32/min com um
  coletor; a meta de 7 dias cabe com folga numa instância. As tentativas
  das truncadas somam consultas: medir na primeira volta.

## Processamento

`internal/parse` transforma uma resposta conclusiva em linhas
([dados-processados.md](dados-processados.md#mapeamento-resposta--colunas))
e o store grava numa transação, que começa com `SELECT ... FROM
registrobr_rdapnum_response WHERE uuid = $1 FOR UPDATE SKIP LOCKED` (outra
instância processando a mesma: pula) e confere que ela ainda é a
`response_uuid` do alvo (senão, uma mais nova já entrou: marca
`processed_at` e sai sem gravar).

- Grava `parser_version = PARSER_VERSION` (a constante `parse.Version`,
  começa em `1`), `processed_at = NOW()`, `process_error = NULL`.
- Recusa (`process_error`, log `processamento recusado`, `warn`, com
  `response` e `err`): a regra do bloco que não é CIDR
  ([fonte.md](fonte.md#regras-do-parser)), qualquer erro de leitura e um
  `objectClassName` que não é o do tipo do alvo. A recusa também grava
  `parser_version` e `processed_at` (o reprocessamento tenta de novo depois
  de 1 h, não a cada minuto); uma falha do banco no meio do processamento
  vai para `process_error` do mesmo jeito, fora da transação desfeita.
- Log `resposta processada` (`debug`) com `response`, `kind`, `changed` e
  `ms`.

### Reprocessamento

Laço em cada instância, a cada **1 min**: pega até 100 respostas atuais
(`target.response_uuid`) que precisam de processamento —
`parser_version IS DISTINCT FROM PARSER_VERSION` (inclui as nunca
processadas, como as de uma coleta que caiu entre as duas transações) ou
`process_error IS NOT NULL` com `processed_at` há mais de 1 h — e processa
uma a uma. Subir o `PARSER_VERSION` numa versão nova do app refaz todo o
dado processado a partir das cruas, **sem consultar o registro.br**
(decisão #38). Log `reprocessamento` (`info`, `count`, `ms`) quando há o que
fazer.

## Atualização pedida pela API

O laço da fila de [dados.md](dados.md#fila-de-atualização-valkey):

1. `BLPOP badblock:registrobr-rdapnum:refresh:queue 5`.
2. Mensagem ilegível, `kind` desconhecido ou alvo mal escrito: descartada
   (`pedido de atualização inválido`, `warn`).
3. `UPDATE registrobr_rdapnum_target SET rescan = true WHERE <asn ou prefix>
   = $1 AND active`. Nenhuma linha: `pedido de atualização para alvo
   inexistente ou inativo` (`debug`).
4. Alvo marcado: `_worker.refreshes + 1`. O alvo sai na frente da fila
   (respeitando `retry_at`), na taxa normal.

Pedido velho não é descartado: marcar `rescan` de novo é inofensivo. Sem
`REDIS_URL`: `atualização desligada (REDIS_URL vazia)` na subida; Valkey
fora: tenta a cada 5 s, um aviso por queda.

## Instâncias e réplicas

Como no [`rdns`](../../rdns/collector.md#instâncias-e-réplicas): batida a
cada 30 s em `_worker` (upsert), compose sem `container_name`, `deploy.replicas:
${COLLECTOR_REGISTROBR_RDAPNUM_REPLICAS:-1}`, containers
`<projeto>-collector-registrobr-rdapnum-<n>`, outros hosts com o mesmo
compose alcançando o Postgres e o Valkey pelo caminho de rede do operador.
A batida grava os contadores, o último IP de saída e `paused_until`.

## Opções

As da [sonda](../../../padroes/sonda.md#configuração-comum) (`SOURCES` =
`cgibr` ou `none`), mais:

| Variável | Argumento | Padrão | Uso |
|---|---|---|---|
| `RDAP_URL` | `--rdap-url` | `https://rdap.registro.br` | base das consultas (`http://` só para teste) |
| `PROXY_URL` | `--proxy-url` | vazio (saída direta) | proxy de saída ([acima](#proxy)) |
| `HTTP_TIMEOUT` | `--http-timeout` | `20s` | prazo de uma consulta inteira |
| `RATE_LIMIT` | `--rate-limit` | `32` | reserva de `rate_limit`, consultas **por minuto** (≥ 1) |
| `SCAN_INTERVAL` | `--scan-interval` | `168h` | reserva de `scan_interval` |
| `SCAN_LEASE` | `--scan-lease` | `2m` | mínimo `30s`; maior que `HTTP_TIMEOUT` + 10 s |
| `TRUNCATED_PAUSE_AFTER` | `--truncated-pause-after` | `3` | truncadas seguidas que pausam a instância (≥ 1) |
| `TRUNCATED_PAUSE` | `--truncated-pause` | `2m` | duração da pausa |
| `PROXY_SAME_IP_WARN` | `--proxy-same-ip-warn` | `10` | respostas seguidas com o mesmo IP que geram o aviso; `0` desliga |
| `DERIVE_INTERVAL`, `RUN_TIMEOUT`, `REDIS_URL` | | `5m`, `10m`, vazio | como no `rdns` |

Inválidas (stderr `collector-registrobr-rdapnum: <mensagem>`, saída 2):
`--rdap-url inválida: "<v>"`, `--proxy-url inválida (use http, https ou
socks5)` (sem ecoar o valor, que tem senha), `--rate-limit inválido:
"<v>"`, `--scan-lease inválido (mínimo 30s e maior que --http-timeout +
10s): "<v>"`, `--truncated-pause-after inválido: "<v>"`,
`--proxy-same-ip-warn inválido: "<v>"`, durações como no padrão.

A linha `iniciando` traz `version`, `commit`, `worker`, `host`, `rdap_url`,
`proxy` (mascarado, ou `none`), `rate_limit`, `scan_interval`,
`scan_lease`, `sources`, `parser_version`, `refresh` (`true` com
`REDIS_URL`) e `postgres` (sem a senha).

## Compose e `.env`

| Variável no `.env` | Vira | Padrão no compose | `.env.example` do app e da raiz |
|---|---|---|---|
| `COLLECTOR_REGISTROBR_RDAPNUM_TAG` | tag e `VERSION` do build | `latest` | `latest` |
| `COLLECTOR_REGISTROBR_RDAPNUM_REPLICAS` | `deploy.replicas` | `1` | `1` |
| `COLLECTOR_REGISTROBR_RDAPNUM_PROXY_URL` | `PROXY_URL` | vazio | vazia, com o comentário "proxy de saída que roda o IP; leva senha, nunca no git" |
| `COLLECTOR_REGISTROBR_RDAPNUM_RATE_LIMIT` | `RATE_LIMIT` | `32` | `32` |
| `COLLECTOR_REGISTROBR_RDAPNUM_SCAN_INTERVAL` | `SCAN_INTERVAL` | `168h` | `168h` |
| `COLLECTOR_REGISTROBR_RDAPNUM_SOURCES` | `SOURCES` | `cgibr` | — |
| `COLLECTOR_REGISTROBR_RDAPNUM_REDIS_URL` | `REDIS_URL` | `redis://:${VALKEY_PASSWORD:-}@badblock-valkey:6379/0` | comentada |

As demais opções não passam pelo compose.

## Operação

```bash
make -C apps/registrobr/rdapnum/collector once     # uma derivação agora (1ª réplica)
make -C apps/registrobr/rdapnum/collector force    # deriva mesmo sem versão nova do cgibr
COLLECTOR_REGISTROBR_RDAPNUM_REPLICAS=2 make -C apps/registrobr/rdapnum/collector up
make -C database/postgres psql   # e: SELECT last_result, count(*) FROM registrobr_rdapnum_target GROUP BY 1;
```

`STACK` como no `rdns` (`badblock` ou `badblock-collector-registrobr-rdapnum`).
A saúde se vê no `/registrobr/rdapnum/meta` (alvos por resultado, em
espera, truncadas por instância, IPs de saída) e nos logs:

| Mensagem | Nível | Campos |
|---|---|---|
| `iniciando` / `encerrando` | info | acima / — |
| `derivação aplicada` / `derivação sem mudança` / `derivação falhou` | info / debug / error | acima / `reason` / `err` |
| `alvo coletado` | debug | acima |
| `lease perdido; resposta descartada` | warn | `target`, `ms` |
| `respostas truncadas seguidas; pausando` | warn | `count`, `egress_ip`, `until` |
| `proxy sem rotação de IP` / `falha no proxy` | warn | `egress_ip`, `count` / `err` |
| `taxa acima de 8/min sem proxy: espere respostas truncadas` | warn | `rate_limit` |
| `erro na consulta` | warn | `target`, `http_status`, `err`, `retry_at` |
| `resposta processada` / `processamento recusado` / `reprocessamento` | debug / warn / info | acima |
| `pedido de atualização inválido` | warn | `payload` |
| `configuração alterada` / `valor inválido em registrobr_rdapnum_config; usando a reserva` | info / warn | `rate_limit`, `scan_interval` / `key`, `value` |
| `Valkey indisponível; atualização parada` | warn | `err` (um por queda) |

## Testes específicos

Unitários (`make test`, servidor RDAP falso com `httptest` servindo as
fixtures de `testdata/`, proxy falso com `httptest` que conta os `CONNECT`):

| Pacote | Confere |
|---|---|
| `rdap` | classificação: completa, truncada (a marca, e só ela), 404, `429`, `500`, prazo, JSON inválido, `objectClassName` errado, corpo acima de 1 MiB; aviso lido (hora e IP) e ausente; uso do proxy e erro do proxy; `Accept` e `User-Agent` |
| `parse` | cada fixture real → linhas (titular, CNPJ, representante, contato aninhado com `via`, contato com dois papéis, blocos IPv4 e IPv6, reverso com nameservers); as sintéticas (faixa não CIDR recusada, entidade sem vCard, `events` ausente) |
| `store` (hash) | normalização sem `notices`, ordem das chaves, mesmo `sha256` para a mesma resposta em horas e IPs diferentes |
| `scan` | espera escalonada (1 min, 5 min, 30 min, 2 h, 6 h), pausa após 3 truncadas e volta após completa, aviso de IP repetido, truncada nunca processada |
| `limiter` | intervalo de `60/rate_limit` s, troca de taxa sem reiniciar |
| `config` | padrões, precedência, `PROXY_URL` inválida sem ecoar a senha, `SCAN_LEASE` × `HTTP_TIMEOUT`, aviso de taxa sem proxy |

Integração (`make test-int`): PG18 com as migrations de `central/`, `cgibr/`
e `registrobr_rdapnum/`, Valkey e o servidor RDAP falso:

1. derivação: alvos dos ASNs e blocos do `cgibr` de teste, `ON CONFLICT`
   com estático, `not_in_source`, versão repetida sem mudança;
2. fila: duas instâncias nunca pegam o mesmo alvo; ordem `rescan` → nunca
   → mais antigo; `retry_at` respeitado mesmo com `rescan`; lease vencido;
3. crua e histórico: completa → linha; a mesma de novo → `seen_count` 2;
   truncada → outra linha, sem processar e sem mudar `response_uuid`; A → B
   → A → três linhas;
4. processamento: completa grava as processadas; nova completa com contato
   trocado regrava ligações; 404 apaga o ASN; entidade órfã sai na
   manutenção; subir `PARSER_VERSION` reprocessa sem consultar;
5. atualização: pedido no Valkey → `rescan = true`; alvo inexistente
   ignorado.

Rede real (opcional, nunca no `make test`):
`make -C apps/registrobr/rdapnum/collector test-real ASN=61610` coleta um
ASN e os blocos dele num PG18 descartável, a 8/min, e mostra o resultado e
o IP de saída (com `PROXY_URL` no ambiente, sai pelo proxy); `BURST=30`
faz uma rajada curta e mostra a partir de qual consulta veio truncada (o
ensaio de [fonte.md](fonte.md#limite-do-servidor)). Em 2026-10-21, sem
proxy: AS61610 e os três blocos, 4 consultas, 4 completas, em 23 s.
