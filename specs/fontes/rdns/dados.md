# Dados da fonte `rdns`

Tabelas `rdns_*` que o `collector-rdns` grava e a `api-rdns` lê, o que o
administrador grava nelas e a fila de tempo real no Valkey (o outro contrato
entre os dois apps). Dono: sub-agente `collector-rdns`; a API lê este arquivo
e não o altera ([../README.md](../README.md#pasta-de-uma-fonte)).

Uma migration em `database/postgres/rdns/` (controle
`rdns_schema_migrations`) implementa este arquivo. Ela depende só de
`central/` (`set_updated_at()` e `jobs`); as tabelas do `cgibr` são lidas
pelo coletor em tempo de execução, sem chave estrangeira. Estilo:
[../../plataforma/postgres.md](../../plataforma/postgres.md#estilo-das-tabelas).

## Tabelas

| Tabela | Uma linha por | Chave natural | Quem escreve |
|---|---|---|---|
| `rdns_prefix` | prefixo a varrer, com o estado da varredura | `prefix` | coletor (derivação, varredura) e administrador |
| `rdns_ptr` | IPv4 com PTR ou com falha | `ip` (é a PK) | coletor |
| `rdns_run` | derivação que leu uma versão nova de uma fonte de origem | — | coletor (só `INSERT`) |
| `rdns_worker` | instância do coletor | — | a própria instância; a manutenção apaga as mortas |
| `rdns_config` | parâmetro que vale para todos os coletores | `key` | administrador |

## `rdns_prefix`

| Coluna | Tipo | Nulo | Padrão | Conteúdo |
|---|---|---|---|---|
| `uuid` | `uuid` | não (PK) | `uuidv7()` | chave |
| `prefix` | `cidr` | não | — | prefixo; a varredura só pega IPv4 de /22 a /24 ([fonte.md](fonte.md#regra-da-sonda)) |
| `asn` | `bigint` | não | `0` | ASN do prefixo; `0` = sem ASN (ex.: privado) |
| `source` | `text` | não | `'static'` | `cgibr` = derivado pelo coletor; `static` = gravado pelo administrador |
| `active` | `boolean` | não | `true` | `false` tira da varredura e do tempo real sem apagar nada |
| `rescan` | `boolean` | não | `false` | `true` fura a fila; o coletor zera ao concluir a varredura |
| `created_at` | `timestamptz` | não | `NOW()` | entrada do prefixo |
| `updated_at` | `timestamptz` | não | `NOW()` | última mudança de **cadastro** (`prefix`, `asn`, `source`, `active`) |
| `scan_worker` | `uuid` | sim | — | instância que está varrendo (FK → `rdns_worker.uuid`); NULL = livre |
| `scan_started_at` | `timestamptz` | sim | — | quando essa instância pegou o prefixo; o lease vence em `scan_started_at + SCAN_LEASE` |
| `last_scan_at` | `timestamptz` | sim | — | fim da última varredura completa (a data do dado de todos os IPs do prefixo); NULL = nunca varrido |
| `last_scan_ms` | `integer` | sim | — | duração dela, em ms |
| `last_scan_ptr` | `integer` | sim | — | IPs com PTR |
| `last_scan_no_ptr` | `integer` | sim | — | IPs sem PTR (`NXDOMAIN`/NODATA) |
| `last_scan_failed` | `integer` | sim | — | IPs com falha (`servfail`, `refused`, `timeout`) |
| `last_scan_changed` | `integer` | sim | — | linhas de `rdns_ptr` inseridas, alteradas ou apagadas |

- `ex_rdns_prefix_overlap`: `EXCLUDE USING gist (prefix inet_ops WITH &&)` —
  **nenhum prefixo sobrepõe outro** (inclusive o igual). O índice GiST dela
  serve também as buscas por continência (`>>=`, `<<=`) e por sobreposição.
- `chk_rdns_prefix_asn`: `CHECK (asn BETWEEN 0 AND 4294967295)`.
- `chk_rdns_prefix_source`: `CHECK (source IN ('cgibr', 'static'))`.
- `fk_rdns_prefix_worker`: `FOREIGN KEY (scan_worker) REFERENCES
  rdns_worker(uuid) ON DELETE SET NULL` — apagar a instância devolve os
  prefixos dela à fila.
- `ix_rdns_prefix_queue`: B-tree em `(rescan DESC, last_scan_at ASC NULLS
  FIRST) WHERE active AND family(prefix) = 4 AND masklen(prefix) BETWEEN 22
  AND 24` — a fila, na ordem dela
  ([../../padroes/sonda.md](../../padroes/sonda.md#fila-de-varredura)).
- `ix_rdns_prefix_worker`: B-tree em `(scan_worker) WHERE scan_worker IS NOT
  NULL` — a cascata da FK e a devolução no encerramento.
- `trg_rdns_prefix_updated_at`: `BEFORE UPDATE OF prefix, asn, source, active
  ... FOR EACH ROW WHEN ((OLD.prefix, OLD.asn, OLD.source, OLD.active) IS
  DISTINCT FROM (NEW.prefix, NEW.asn, NEW.source, NEW.active)) EXECUTE
  FUNCTION set_updated_at()` — o estado da varredura e o `rescan` não
  andam o `updated_at`.
- `trg_rdns_prefix_split` e `trg_rdns_prefix_delete_ptr`: abaixo.
- Nenhuma coluna liga um IP ao prefixo: a ligação é por continência
  (`prefix >>= ip`), que a regra de não sobreposição torna única.

### Sobreposição: `trg_rdns_prefix_split`

`BEFORE INSERT ... FOR EACH ROW EXECUTE FUNCTION rdns_prefix_split()`:

```sql
CREATE FUNCTION rdns_prefix_split() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM rdns_prefix p WHERE p.prefix && NEW.prefix) THEN
        RETURN NEW;                                   -- sem sobreposição: entra como veio
    END IF;
    IF EXISTS (SELECT 1 FROM rdns_prefix p WHERE p.prefix >>= NEW.prefix) THEN
        RETURN NULL;                                  -- já coberto inteiro: descartado
    END IF;
    IF family(NEW.prefix) = 4 AND masklen(NEW.prefix) <= 24 THEN
        INSERT INTO rdns_prefix (prefix, asn, source, active, rescan)
        SELECT u.prefix, NEW.asn, NEW.source, NEW.active, NEW.rescan
          FROM (SELECT set_masklen(network(NEW.prefix) + i * 256, 24)::cidr AS prefix
                  FROM generate_series(0::bigint, (1::bigint << (24 - masklen(NEW.prefix))) - 1) AS i) u
         WHERE NOT EXISTS (SELECT 1 FROM rdns_prefix p WHERE p.prefix && u.prefix);
    END IF;
    RETURN NULL;                                      -- o original nunca entra
END $$;
```

- O prefixo novo que contém linhas existentes vira **os /24 que faltam**,
  herdando `asn`, `source`, `active` e `rescan`; o original é descartado. Um
  /24 só parcialmente coberto (por uma linha estática menor que /24) não
  entra.
- IPv6 ou IPv4 mais longo que /24 que sobrepõe alguém: descartado.
- Um `INSERT` que cai no trigger devolve **0 linhas** (o `RETURNING` não traz
  nada); os /24 inseridos aparecem numa consulta depois.
- Dois `INSERT` simultâneos que se sobrepõem passam pelo trigger sem se ver;
  a `ex_rdns_prefix_overlap` barra o segundo (`conflicting key value violates
  exclusion constraint`), e quem gravou tenta de novo. A derivação do coletor
  não cai nisso: ela roda sob trava e já filtra o que está coberto.

### Remoção: `trg_rdns_prefix_delete_ptr`

`AFTER DELETE ... FOR EACH ROW EXECUTE FUNCTION rdns_prefix_delete_ptr()`,
que faz `DELETE FROM rdns_ptr WHERE ip <<= OLD.prefix` e devolve `NULL`.
Apagar um prefixo apaga os resultados dos IPs dele.

## `rdns_ptr`

| Coluna | Tipo | Nulo | Padrão | Conteúdo |
|---|---|---|---|---|
| `ip` | `inet` | não (PK) | — | o endereço IPv4 (`/32`) |
| `status` | `smallint` | não | — | `1=ptr` \| `2=servfail` \| `3=refused` \| `4=timeout` |
| `name` | `text` | sim | — | o primeiro PTR em ordem de bytes; NULL nas falhas |
| `names` | `text[]` | sim | — | todos os PTR, inclusive o primeiro, em ordem de bytes ([fonte.md](fonte.md#nomes)); NULL nas falhas |
| `created_at` | `timestamptz` | não | `NOW()` | primeira vez que o IP teve linha |
| `updated_at` | `timestamptz` | não | `NOW()` | última mudança de `status`, `name` ou `names` |

- PK **natural** `ip`, declarada na coluna (`rdns_ptr_pkey`), exceção à PK
  `uuidv7()` (decisão #27): com ~55 M linhas, uma PK `uuid` somaria ~16 bytes
  por linha e um segundo índice só para achar o IP.
- O B-tree da PK serve a busca de um IP (`=`) e de um prefixo (`ip <<=
  prefixo`, que o Postgres transforma em faixa no B-tree).
- `chk_rdns_ptr_ip`: `CHECK (family(ip) = 4 AND masklen(ip) = 32)`.
- `chk_rdns_ptr_status`: `CHECK (status BETWEEN 1 AND 4)`.
- `chk_rdns_ptr_name`: `CHECK ((status = 1 AND names IS NOT NULL AND
  cardinality(names) >= 1 AND coalesce(name = names[1], false)) OR (status <>
  1 AND name IS NULL AND names IS NULL))` — o `IS NOT NULL` e o `coalesce`
  existem porque um `CHECK` que dá NULL passa.
- `trg_rdns_ptr_updated_at`: `BEFORE UPDATE ... EXECUTE FUNCTION
  set_updated_at()`.

Regras de escrita (o coletor as aplica na varredura e no tempo real):

| Linha atual | Resultado novo | Faz |
|---|---|---|
| nenhuma | `ptr` ou falha | `INSERT` |
| nenhuma | `no_ptr` | nada |
| `ptr` | `ptr` com outros nomes | `UPDATE` |
| `ptr` | falha | nada (**a falha nunca apaga um PTR**) |
| falha | `ptr` ou outra falha | `UPDATE` |
| qualquer | `no_ptr` | `DELETE` |
| igual ao resultado | o mesmo | nada (a linha só é regravada quando muda) |

Sem TTL, sem histórico e **sem data por IP**: a data em que o dado foi
conferido é o `last_scan_at` do prefixo que contém o IP. Um IP sem linha num
prefixo já varrido não tem PTR; num prefixo nunca varrido, não foi
consultado.

## `rdns_run`

| Coluna | Tipo | Nulo | Padrão | Conteúdo |
|---|---|---|---|---|
| `uuid` | `uuid` | não (PK) | `uuidv7()` | chave |
| `status` | `smallint` | não | — | `0` falhou, `1` aplicada |
| `forced` | `boolean` | não | `false` | veio de `--force` |
| `source` | `text` | não | — | fonte de origem: `cgibr` |
| `source_version` | `uuid` | não | — | versão lida (`cgibr_run.uuid`) |
| `source_updated_at` | `timestamptz` | não | — | quando a origem aplicou essa versão (`cgibr_run.created_at`) |
| `blocks` | `integer` | sim | — | blocos IPv4 lidos da origem |
| `units` | `integer` | sim | — | /24 derivados |
| `inserted` | `integer` | sim | — | /24 novos em `rdns_prefix` |
| `asn_updated` | `integer` | sim | — | linhas `cgibr` com o ASN corrigido |
| `covered` | `integer` | sim | — | /24 que já estavam lá ou cobertos por outra linha |
| `not_in_source` | `integer` | sim | — | linhas `cgibr` que esta versão não traz mais (continuam lá) |
| `warnings` | `jsonb` | não | `'[]'::jsonb` | avisos (bloco mais longo que /24), até 50 e `... e mais N avisos` |
| `error` | `text` | sim | — | motivo da falha |
| `started_at` | `timestamptz` | não | — | início da passada |
| `created_at` | `timestamptz` | não | `NOW()` | gravação da linha |

- `chk_rdns_run_status`: `CHECK (status BETWEEN 0 AND 1)`;
  `chk_rdns_run_source`: `CHECK (source IN ('cgibr'))`.
- `ix_rdns_run_applied`: B-tree em `(source, created_at DESC) WHERE status =
  1` — a última versão derivada de cada origem.
- Sem `updated_at`: a linha nunca muda. Nas linhas `status = 1`, as contagens
  nunca são NULL e `error` é NULL.

## `rdns_worker`

| Coluna | Tipo | Nulo | Padrão | Conteúdo |
|---|---|---|---|---|
| `uuid` | `uuid` | não (PK) | `uuidv7()` | a instância: um UUIDv7 que ela mesma gera na subida (antes de conectar, para sair na linha `iniciando`) |
| `host` | `text` | não | — | `os.Hostname()` (no Docker, o ID curto do container) |
| `version` | `text` | não | — | versão do app |
| `rate_limit` | `integer` | não | — | taxa em vigor, em consultas/s |
| `queries` | `bigint` | não | `0` | consultas DNS enviadas desde a subida (com as novas tentativas) |
| `live_queries` | `bigint` | não | `0` | pedidos de tempo real atendidos |
| `prefixes` | `bigint` | não | `0` | prefixos varridos até o fim |
| `created_at` | `timestamptz` | não | `NOW()` | subida da instância |
| `updated_at` | `timestamptz` | não | `NOW()` | último sinal (a batida a cada 30 s anda pelo trigger) |

`trg_rdns_worker_updated_at`: `BEFORE UPDATE ... EXECUTE FUNCTION
set_updated_at()`. Linha sem sinal há mais de 24 h sai na manutenção da
derivação; o encerramento limpo apaga a própria.

## `rdns_config`

| Coluna | Tipo | Nulo | Padrão | Conteúdo |
|---|---|---|---|---|
| `uuid` | `uuid` | não (PK) | `uuidv7()` | chave |
| `key` | `text` | não | — | `rate_limit` (consultas/s **por coletor**, inteiro ≥ 1) ou `scan_interval` (meta por prefixo, duração Go, ex.: `24h`) |
| `value` | `text` | não | — | o valor, em texto |
| `created_at`, `updated_at` | `timestamptz` | não | `NOW()` | criação e última alteração (`trg_rdns_config_updated_at`) |

`uq_rdns_config_key`: `UNIQUE (key)`; `chk_rdns_config_key`: `CHECK (key IN
('rate_limit', 'scan_interval'))`. A migration **não** grava linhas: sem
linha vale a variável do ambiente de cada coletor (`RATE_LIMIT`,
`SCAN_INTERVAL`).

## O que o administrador grava

O sistema de gestão do administrador escreve direto no banco, com a mesma
`POSTGRES_URL`:

| Ação | Como | Efeito |
|---|---|---|
| incluir um prefixo | `INSERT INTO rdns_prefix (prefix, asn) VALUES ('203.0.113.0/24', 64500)` (`source` vale `static`) | entra na fila como nunca varrido; se sobrepõe, o [trigger](#sobreposição-trg_rdns_prefix_split) decide |
| tirar da varredura | `UPDATE rdns_prefix SET active = false WHERE prefix = $1` | sai da fila e do tempo real; os resultados ficam e a API continua servindo |
| remover | `DELETE FROM rdns_prefix WHERE prefix = $1` | apaga o prefixo e os resultados dele |
| pedir nova varredura | `UPDATE rdns_prefix SET rescan = true WHERE prefix = $1` | passa na frente da fila |
| mudar a taxa ou a meta | `INSERT INTO rdns_config (key, value) VALUES ('rate_limit', '512') ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value` | todos os coletores em até 30 s |

- Um /24 `cgibr` apagado volta na próxima versão do `cgibr` que ainda o
  traga: para tirá-lo de vez, **desative** em vez de apagar.
- Nunca troque o `prefix` de uma linha (apague e insira), nunca escreva as
  colunas `scan_*`/`last_scan_*` e nunca escreva em `rdns_ptr`.

## Fila de tempo real (Valkey)

Contrato entre a `api-rdns` (produtora) e o `collector-rdns` (consumidor), no
mesmo Valkey do cache ([../../padroes/sonda.md](../../padroes/sonda.md#tempo-real)):

| Chave | Tipo | TTL | Escreve | Lê |
|---|---|---|---|---|
| `badblock:rdns:live:queue` | lista | **nenhum** | API (`RPUSH`) | coletor (`BLPOP`) |
| `badblock:rdns:live:inflight:<ip>` | texto `1` | 30 s | API (`SET NX EX 30`) | API; o coletor apaga (`DEL`) depois de responder |
| `badblock:rdns:live:reply:<ip>` | lista | 30 s | coletor (`RPUSH` e `EXPIRE 30`) | API (`BLPOP`) |

`<ip>` é o IPv4 com pontos (`200.160.2.3`). Pedido, na fila:

```json
{"ip":"200.160.2.3","at":"2026-10-10T14:03:11.123Z"}
```

Resposta, na lista de retorno, gravada **depois** de `rdns_ptr`:

```json
{"ip":"200.160.2.3","result":"ptr","names":["registro.br"],"checked_at":"2026-10-10T14:03:11Z","worker":"01a1243c-6b0e-7c5d-9a51-3f0f6c2d1e77"}
```

`result`: `ptr`, `no_ptr`, `servfail`, `refused`, `timeout` ou
`not_allowed` (o IP não está num prefixo ativo na regra: nada é consultado
nem gravado); `names` é `[]` fora do `ptr`. O formato da mensagem é o mesmo
nos dois apps: mudou, mudam os dois juntos.

## Consultas da `api-rdns`

Só leitura. A API não lê `rdns_worker` nem `rdns_config` fora do `/meta`.

| Uso | Consulta | Índice |
|---|---|---|
| pronta? (a cada `DATASET_POLL`) | `SELECT EXISTS (SELECT 1 FROM rdns_prefix)` | qualquer |
| `/rdns/ip/{ip}` e a releitura depois do tempo real | `SELECT p.prefix, p.asn, p.source, p.active, p.last_scan_at, r.status, r.name, r.names, r.created_at, r.updated_at FROM rdns_prefix p LEFT JOIN rdns_ptr r ON r.ip = $1::inet WHERE p.prefix >>= $1::inet` | GiST da `ex_rdns_prefix_overlap`, `rdns_ptr_pkey` |
| `/rdns/prefix/{ip}/{len}` | `SELECT prefix, asn, source, active, last_scan_at FROM rdns_prefix WHERE prefix >>= $1::cidr` e `SELECT ip, status, name, names FROM rdns_ptr WHERE ip <<= $1::cidr ORDER BY ip` | idem |
| `/rdns/meta`, bloco `scan` | um `SELECT` com `count(*) FILTER (...)`, `sum(1::bigint << (32 - masklen(prefix)))`, `min(last_scan_at)` e `sum(last_scan_*)` sobre `rdns_prefix` inteira (~309 mil linhas) | — |
| `/rdns/meta`, demais blocos | a última `rdns_run` aplicada de cada origem; `rdns_worker` com sinal nas últimas 24 h; `rdns_config`; a linha `collector-rdns` de `jobs` | `ix_rdns_run_applied`, `uq_rdns_config_key`, `uq_jobs_app` |

## Tamanho

Estimativa com os fatos de [fonte.md](fonte.md#fatos-medidos): ~55 M linhas
em `rdns_ptr` × ~160 bytes (cabeçalho, `inet`, `smallint`, o nome de ~34
bytes duas vezes — em `name` e em `names` —, duas datas) ≈ 9 GB, mais ~1,2 GB
do B-tree da PK: **~10 GB**. `rdns_prefix` (~309 mil linhas) fica em dezenas
de MB (46 MB com os índices, medido em 2026-10-10 com os 308.529 /24). É o
que fez o `shared_buffers` padrão subir para 2 GB
([../../plataforma/postgres.md](../../plataforma/postgres.md#servidor-databasepostgresdocker-composeyml)).
Medir com a primeira volta completa (`pg_total_relation_size('rdns_ptr')`).

## A migration

- Cabeçalho: o que cria, de que depende (`central`), quem escreve (o coletor
  e o sistema de gestão do administrador) e quem lê, a regra de não
  sobreposição e o contrato do tempo real (apontando este arquivo).
- `migrate:up`, cada tabela seguida dos seus `COMMENT ON` (com a legenda de
  `rdns_ptr.status` e o alvo da FK): `rdns_run` → `ix_rdns_run_applied` →
  `rdns_config` → `trg_rdns_config_updated_at` → `rdns_worker` →
  `trg_rdns_worker_updated_at` → `rdns_ptr` → `trg_rdns_ptr_updated_at` →
  `rdns_prefix` → as funções `rdns_prefix_split()` e
  `rdns_prefix_delete_ptr()` (com `COMMENT ON FUNCTION`) → os três triggers
  de `rdns_prefix` → `ix_rdns_prefix_queue` → `ix_rdns_prefix_worker`.
- As duas funções levam `SET enable_seqscan = off`: o plano das consultas
  delas fica em cache na sessão, e o feito com a tabela vazia (na primeira
  derivação, ou com `rdns_ptr` pequena) seria uma leitura sequencial refeita
  a cada linha — sobre os ~309 mil `INSERT` da primeira derivação, com a
  tabela crescendo, a carga não terminava. O GiST da `ex_rdns_prefix_overlap`
  (em `rdns_prefix_split()`) e o B-tree da PK de `rdns_ptr` (em
  `rdns_prefix_delete_ptr()`) servem as consultas.
- `migrate:down`: `DROP TABLE rdns_prefix; DROP TABLE rdns_ptr; DROP TABLE
  rdns_worker; DROP TABLE rdns_config; DROP TABLE rdns_run;` e depois `DROP
  FUNCTION rdns_prefix_split(); DROP FUNCTION rdns_prefix_delete_ptr();`.

## Mudando o schema

Como no [cgibr](../cgibr/dados.md#mudando-o-schema): migration nova, `make -C
database/postgres test`, este arquivo, o `internal/store` do coletor e o
teste de integração; se a API lê a coluna, avise o sub-agente dela. Mudou o
formato da fila de tempo real: os dois apps mudam juntos.
