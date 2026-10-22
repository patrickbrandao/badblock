# Dados da fonte `registrobr/rdapnum`

Tabelas de controle e de respostas cruas que o `collector-registrobr-rdapnum`
grava, o que o administrador grava nelas e a fila de atualização no Valkey
(o outro contrato entre os dois apps). As tabelas processadas, que a API lê
para responder, estão em [dados-processados.md](dados-processados.md). Dono:
sub-agente `collector-registrobr-rdapnum`; a API lê os dois arquivos e não
os altera ([../../README.md](../../README.md#pasta-de-uma-fonte)).

Uma migration em `database/postgres/registrobr_rdapnum/` (controle
`registrobr_rdapnum_schema_migrations`) implementa os dois arquivos. Ela
depende só de `central/` (`set_updated_at()` e `jobs`); as tabelas do
`cgibr` são lidas em tempo de execução, sem chave estrangeira. Estilo:
[../../../plataforma/postgres.md](../../../plataforma/postgres.md#estilo-das-tabelas).
Abaixo, `<p>` abrevia o prefixo `registrobr_rdapnum` nos nomes de
constraints e índices (o SQL usa o nome inteiro).

## Tabelas

| Tabela | Uma linha por | Chave natural | Quem escreve |
|---|---|---|---|
| `registrobr_rdapnum_target` | ASN ou bloco a consultar, com o estado da coleta | `asn` ou `prefix` | coletor (derivação, coleta) e administrador |
| `registrobr_rdapnum_response` | versão de uma resposta de um alvo (só as que mudaram) | — | coletor |
| `registrobr_rdapnum_autnum`, `_network`, `_entity`, `_link`, `_reverse` | dado processado ([dados-processados.md](dados-processados.md)) | — | coletor (processamento) |
| `registrobr_rdapnum_run` | derivação que leu uma versão nova do `cgibr` | — | coletor (só `INSERT`) |
| `registrobr_rdapnum_worker` | instância do coletor | — | a própria instância; a manutenção apaga as mortas |
| `registrobr_rdapnum_config` | parâmetro que vale para todos os coletores | `key` | administrador |

## `registrobr_rdapnum_target`

A fila é a tabela ([sonda](../../../padroes/sonda.md#tabelas)): cadastro e
estado da coleta na mesma linha.

| Coluna | Tipo | Nulo | Padrão | Conteúdo |
|---|---|---|---|---|
| `uuid` | `uuid` | não (PK) | `uuidv7()` | chave |
| `kind` | `text` | não | — | `asn` ou `prefix` |
| `asn` | `bigint` | sim | — | o ASN, quando `kind = 'asn'` |
| `prefix` | `cidr` | sim | — | o bloco consultado, quando `kind = 'prefix'` (IPv4 ou IPv6) |
| `source` | `text` | não | `'static'` | `cgibr` = derivado; `static` = gravado pelo administrador |
| `active` | `boolean` | não | `true` | `false` tira da fila e da atualização sem apagar nada |
| `rescan` | `boolean` | não | `false` | `true` passa na frente da fila; o coletor zera ao concluir |
| `created_at` | `timestamptz` | não | `NOW()` | entrada do alvo |
| `updated_at` | `timestamptz` | não | `NOW()` | última mudança de **cadastro** (`kind`, `asn`, `prefix`, `source`, `active`) |
| `scan_worker` | `uuid` | sim | — | instância que está consultando (FK → `registrobr_rdapnum_worker.uuid`); NULL = livre |
| `scan_started_at` | `timestamptz` | sim | — | quando a instância pegou o alvo; o lease vence em `scan_started_at + SCAN_LEASE` |
| `last_scan_at` | `timestamptz` | sim | — | última coleta **conclusiva** (completa ou 404): a data do dado; NULL = nunca |
| `last_attempt_at` | `timestamptz` | sim | — | última tentativa, conclusiva ou não |
| `last_result` | `smallint` | sim | — | resultado da última tentativa: `1=complete` \| `2=truncated` \| `3=not_found` \| `4=error` |
| `last_http_status` | `smallint` | sim | — | status HTTP da última tentativa (NULL em erro de rede) |
| `last_error` | `text` | sim | — | motivo do último `error`; NULL depois de uma conclusiva |
| `failures` | `smallint` | não | `0` | truncadas e erros **seguidos** desde a última conclusiva |
| `retry_at` | `timestamptz` | sim | — | antes disso o alvo não volta à fila (espera depois de truncada ou erro); NULL = sem espera |
| `response_uuid` | `uuid` | sim | — | a resposta **atual**: a da última coleta conclusiva (FK → `registrobr_rdapnum_response.uuid`, `ON DELETE SET NULL`) |

- `chk_<p>_target_kind`: `CHECK ((kind = 'asn' AND asn IS NOT NULL AND
  prefix IS NULL) OR (kind = 'prefix' AND prefix IS NOT NULL AND asn IS
  NULL))`.
- `chk_<p>_target_asn`: `CHECK (asn BETWEEN 0 AND 4294967295)`;
  `chk_<p>_target_source`: `CHECK (source IN ('cgibr', 'static'))`;
  `chk_<p>_target_result`: `CHECK (last_result BETWEEN 1 AND 4)`.
- `uq_<p>_target_asn`: `UNIQUE (asn)`; `uq_<p>_target_prefix`: `UNIQUE
  (prefix)` (os NULL do outro tipo não colidem). Blocos que se sobrepõem são
  permitidos: cada um é uma consulta.
- `fk_<p>_target_worker`: `FOREIGN KEY (scan_worker) REFERENCES
  registrobr_rdapnum_worker(uuid) ON DELETE SET NULL`.
- `fk_<p>_target_response`: a FK de `response_uuid`, criada **depois** de
  `registrobr_rdapnum_response` (as duas tabelas se apontam).
- `ix_<p>_target_queue`: B-tree em `(rescan DESC, last_scan_at ASC NULLS
  FIRST) WHERE active` — a fila na ordem dela.
- `ix_<p>_target_worker`: B-tree em `(scan_worker) WHERE scan_worker IS NOT
  NULL`.
- `trg_<p>_target_updated_at`: `BEFORE UPDATE OF kind, asn, prefix, source,
  active ... WHEN ((OLD.kind, OLD.asn, OLD.prefix, OLD.source, OLD.active)
  IS DISTINCT FROM (NEW.kind, ...)) EXECUTE FUNCTION set_updated_at()` — o
  estado da coleta não anda o `updated_at`.

## `registrobr_rdapnum_response`

A resposta crua, gravada (e confirmada) **antes** de qualquer processamento.
Só respostas do servidor: completas, truncadas e 404. Erros (rede, prazo,
`5xx`, `429`, JSON inválido) não têm linha — ficam em `last_error` do alvo e
no log.

| Coluna | Tipo | Nulo | Padrão | Conteúdo |
|---|---|---|---|---|
| `uuid` | `uuid` | não (PK) | `uuidv7()` | chave; a API a mostra no histórico e em `/response/{uuid}` |
| `target_uuid` | `uuid` | não | — | o alvo (FK → `registrobr_rdapnum_target.uuid`, `ON DELETE CASCADE`) |
| `result` | `smallint` | não | — | `1=complete` \| `2=truncated` \| `3=not_found` |
| `url` | `text` | não | — | a URL consultada |
| `http_status` | `smallint` | não | — | `200` ou `404` |
| `body` | `jsonb` | sim | — | o corpo inteiro como veio (inclusive `notices`); NULL no 404 sem JSON |
| `sha256` | `text` | não | — | SHA-256 do corpo normalizado ([abaixo](#resposta-igual)), hex minúsculo |
| `bytes` | `integer` | não | — | tamanho do corpo recebido |
| `egress_ip` | `inet` | sim | — | IP de saída que o servidor viu, lido do aviso ([fonte.md](fonte.md#aviso-com-o-ip-de-quem-consultou)), na primeira vez |
| `server_time` | `timestamptz` | sim | — | a hora do servidor no mesmo aviso |
| `worker` | `uuid` | não | — | instância que recebeu (sem FK: as instâncias morrem, o histórico fica) |
| `fetched_at` | `timestamptz` | não | `NOW()` | primeira vez que esta versão chegou |
| `last_seen_at` | `timestamptz` | não | `NOW()` | última vez que chegou igual |
| `seen_count` | `integer` | não | `1` | quantas vezes chegou igual |
| `parser_version` | `smallint` | sim | — | versão do parser que a processou; NULL = não processada (truncadas nunca são) |
| `processed_at` | `timestamptz` | sim | — | quando foi processada |
| `process_error` | `text` | sim | — | por que o processamento recusou (ex.: `faixa não é CIDR: ...`); NULL se deu certo |

- `chk_<p>_response_result`: `CHECK (result BETWEEN 1 AND 3)`;
  `chk_<p>_response_status`: `CHECK ((result IN (1, 2) AND http_status = 200
  AND body IS NOT NULL) OR (result = 3 AND http_status = 404))`;
  `chk_<p>_response_sha256`: `CHECK (sha256 ~ '^[0-9a-f]{64}$')`.
- `ix_<p>_response_target`: B-tree em `(target_uuid, result, fetched_at
  DESC)` — a última versão de cada tipo de um alvo (a comparação) e o
  histórico.
- `ix_<p>_response_pending`: B-tree em `(fetched_at) WHERE result IN (1, 3)
  AND processed_at IS NULL AND process_error IS NULL` — o que falta
  processar.
- Sem `updated_at` nem trigger: só `last_seen_at`, `seen_count` e as colunas
  de processamento mudam, pelo coletor.
- **Histórico inteiro, sem expurgo** (decisão #39): nenhuma linha é apagada
  pelo coletor; só a cascata quando o administrador apaga o alvo.

### Resposta igual

Uma resposta nova vira linha nova **só se mudou**:

1. **Normaliza** o corpo: tira o `notices` de nível superior (muda a cada
  consulta: hora e IP) e serializa com as chaves dos objetos em ordem (o
  `json.Marshal` de um `map` no Go); no 404, o corpo normalizado é vazio.
2. `sha256` desse texto.
3. Compara com a **última linha do mesmo `result`** do alvo
  (`ix_<p>_response_target`). Igual: `UPDATE ... SET last_seen_at = NOW(),
  seen_count = seen_count + 1`. Diferente, ou nenhuma: `INSERT`.

Comparar dentro do mesmo `result` impede que completas e truncadas
alternadas criem uma linha a cada coleta. O retorno a uma versão antiga (A →
B → A) cria linha nova: a linha do tempo fica certa.

Tamanho: ~3 KB por resposta (o `jsonb` comprime pouco nesse tamanho);
~31 mil alvos ≈ **100 MB** na primeira volta. Depois, só o que muda — dados
de registro mudam pouco — e as truncadas diferentes. Medir com a primeira
volta (`pg_total_relation_size('registrobr_rdapnum_response')`).

## `registrobr_rdapnum_run`

Como a `rdns_run` ([../../rdns/dados.md](../../rdns/dados.md#rdns_run)), com
estas contagens:

| Coluna | Tipo | Conteúdo |
|---|---|---|
| `uuid`, `status`, `forced`, `source`, `source_version`, `source_updated_at`, `warnings`, `error`, `started_at`, `created_at` | — | como em `rdns_run` (`source` só `cgibr`) |
| `asns` | `integer` | ASNs lidos de `cgibr_asn` |
| `prefixes` | `integer` | blocos lidos de `cgibr_prefix` (IPv4 e IPv6) |
| `inserted` | `integer` | alvos novos |
| `not_in_source` | `integer` | alvos `cgibr` que esta versão não traz mais (continuam lá) |

`chk_<p>_run_status`, `chk_<p>_run_source` e `ix_<p>_run_applied` como no
`rdns`.

## `registrobr_rdapnum_worker`

| Coluna | Tipo | Nulo | Padrão | Conteúdo |
|---|---|---|---|---|
| `uuid` | `uuid` | não (PK) | `uuidv7()` | a instância (UUIDv7 gerado na subida) |
| `host`, `version` | `text` | não | — | como no `rdns` |
| `rate_limit` | `integer` | não | — | taxa em vigor, em consultas **por minuto** |
| `proxy` | `boolean` | não | — | `true` com `PROXY_URL` (a URL nunca vai para o banco) |
| `requests` | `bigint` | não | `0` | consultas enviadas desde a subida |
| `complete`, `truncated`, `not_found`, `errors` | `bigint` | não | `0` | resultado de cada uma |
| `refreshes` | `bigint` | não | `0` | pedidos de atualização atendidos |
| `egress_ip` | `inet` | sim | — | último IP de saída visto |
| `egress_ips` | `integer` | não | `0` | IPs de saída distintos nas últimas 100 respostas |
| `paused_until` | `timestamptz` | sim | — | pausa por truncadas seguidas ([collector.md](collector.md#truncada-e-erros)) |
| `created_at`, `updated_at` | `timestamptz` | não | `NOW()` | subida e último sinal (`trg_<p>_worker_updated_at`) |

## `registrobr_rdapnum_config`

Como a `rdns_config` ([../../rdns/dados.md](../../rdns/dados.md#rdns_config)),
com `chk_<p>_config_key`: `CHECK (key IN ('rate_limit', 'scan_interval'))` e
estes valores:

| `key` | `value` | Reserva no ambiente |
|---|---|---|
| `rate_limit` | consultas **por minuto por coletor**, inteiro ≥ 1 | `RATE_LIMIT` (`32`) |
| `scan_interval` | meta por alvo, duração Go | `SCAN_INTERVAL` (`168h`) |

A migration não grava linhas.

## O que o administrador grava

Direto no banco, com a mesma `POSTGRES_URL`:

| Ação | Como | Efeito |
|---|---|---|
| incluir um ASN ou bloco | `INSERT INTO registrobr_rdapnum_target (kind, asn) VALUES ('asn', 64500)` ou `(kind, prefix) VALUES ('prefix', '203.0.113.0/24')` | entra na fila como nunca consultado |
| tirar da fila | `UPDATE registrobr_rdapnum_target SET active = false WHERE asn = $1` | sai da fila e da atualização; o dado fica e a API continua servindo |
| remover | `DELETE FROM registrobr_rdapnum_target WHERE asn = $1` | apaga o alvo, as respostas e o dado processado dele (cascata) |
| pedir nova coleta | `UPDATE registrobr_rdapnum_target SET rescan = true WHERE asn = $1` | passa na frente da fila (respeita `retry_at`) |
| mudar a taxa ou a meta | `INSERT INTO registrobr_rdapnum_config (key, value) VALUES ('rate_limit', '48') ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value` | todos os coletores em até 30 s |

- Um alvo `cgibr` apagado volta na próxima versão do `cgibr` que ainda o
  traga: para tirá-lo de vez, **desative**.
- Nunca troque `asn`/`prefix` de uma linha, nunca escreva as colunas de
  estado da coleta e nunca escreva em `_response` nem nas processadas.

## Fila de atualização (Valkey)

Contrato entre a `api-registrobr-rdapnum` (produtora) e o
`collector-registrobr-rdapnum` (consumidor), no Valkey do cache. Diferente do
tempo real do `rdns`, ninguém espera a coleta: o pedido só marca o alvo para
passar na frente ([collector.md](collector.md#atualização-pedida-pela-api)).

| Chave | Tipo | TTL | Escreve | Lê |
|---|---|---|---|---|
| `badblock:registrobr-rdapnum:refresh:queue` | lista | **nenhum** | API (`RPUSH`) | coletor (`BLPOP`) |
| `badblock:registrobr-rdapnum:refresh:inflight:<kind>:<alvo>` | texto `1` | 60 s | API (`SET NX EX 60`) | API (pedido repetido no minuto não entra de novo) |

`<kind>` é `asn` ou `prefix`, e `<alvo>` o ASN ou o bloco do alvo em forma
canônica: `...:inflight:asn:61610`, `...:inflight:prefix:200.225.48.0/21`.
Pedido:

```json
{"kind":"asn","target":"61610","at":"2026-10-21T21:10:00.123Z"}
```

O formato é o mesmo nos dois apps: mudou, mudam os dois juntos.

## Mudando o schema

Como no [cgibr](../../cgibr/dados.md#mudando-o-schema): migration nova,
`make -C database/postgres test`, estes dois arquivos, o `internal/store` do
coletor e o teste de integração; se a API lê a coluna, avise o sub-agente
dela.
