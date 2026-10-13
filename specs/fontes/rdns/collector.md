# Coletor `collector-rdns`

O que o `collector-rdns` faz além do [padrão dos coletores](../../padroes/coletor.md)
e do [padrão de sonda ativa](../../padroes/sonda.md), que ele segue (o que
vale de cada um está no segundo). Prefixos, consulta e respostas:
[fonte.md](fonte.md). Tabelas e fila de tempo real: [dados.md](dados.md).
Dono: sub-agente `collector-rdns`.

## Valores desta fonte

| Item | Valor |
|---|---|
| App e `jobs.app` | `collector-rdns` |
| Tabelas escritas | `rdns_prefix` (derivação e estado da varredura), `rdns_ptr`, `rdns_run`, `rdns_worker` e a linha `collector-rdns` de `jobs` |
| Tabelas lidas de outra fonte | `cgibr_run`, `cgibr_prefix`, `cgibr_asn` (só na derivação) |
| Tabela temporária | `stage_unit` (`ON COMMIT DROP`) |
| Conexão | pool pgx de no máximo **4** conexões (o padrão é 2: aqui correm juntos a fila, o tempo real, a batida e a derivação), ociosas fechadas em 5 min; `application_name=collector-rdns` |
| Valkey | cliente com `ClientName` `badblock-collector-rdns`, `MaxRetries` 0, pool de 4, 1 s de prazo fora do `BLPOP` |
| Dependência a mais | `github.com/miekg/dns` v1.1.73 (decisão #32) e `github.com/redis/go-redis/v9` |
| Descrição da imagem | `Varre o DNS reverso (PTR) dos IPv4 cadastrados para o PostgreSQL do BadBlock` |

## Estrutura do código

```
apps/rdns/collector/
├── cmd/collector-rdns/main.go
├── internal/
│   ├── buildinfo/, config/
│   ├── store/      pgx: derivação, fila, gravação, rdns_worker, rdns_config, jobs
│   ├── derive/     uma passada da derivação
│   ├── resolver/   cliente DNS (miekg/dns): rodízio, prazos, tentativas, respostas, nomes
│   ├── limiter/    taxa por instância, com fila de prioridade para o tempo real
│   ├── scan/       laço da fila: pega, varre, grava ou abandona
│   ├── live/       laço do tempo real (BLPOP no Valkey)
│   └── worker/     batida e releitura de rdns_config a cada 30 s; o uuid (UUIDv7) da instância
├── testdata/zone.txt   respostas do servidor DNS falso (fonte.md#fixtures)
└── os arquivos de todo app (Dockerfile, compose, Makefile, vendor/…)
```

### `main.go`

Passos 1 a 4 do [padrão](../../padroes/coletor.md#maingo), e então:

1. `--once`/`--force`: uma passada da derivação e sai (0 ou 1; sem a trava,
   `outra instância está derivando`, saída 1). Nada mais sobe.
2. Valkey, se `REDIS_URL`: URL inválida → `REDIS_URL inválida`, saída 2;
   fora do ar não impede a subida.
3. Grava a linha em `rdns_worker` (a primeira batida, com o `uuid` UUIDv7
   que a instância gerou antes da linha `iniciando`) e sobe, juntos: a
   derivação (na subida e a cada `DERIVE_INTERVAL`), a batida, o laço da fila
   e o laço do tempo real.
4. `SIGINT`/`SIGTERM`: para de pegar prefixos e pedidos, abandona os que
   estão em varredura (até **10 s**), apaga a própria linha de `rdns_worker`
   (o que devolve os prefixos à fila) e sai com 0.

## Derivação

A passada da [sonda](../../padroes/sonda.md#derivação), com `SOURCES` =
`cgibr`, numa transação **`REPEATABLE READ`** (a versão lida e os blocos
lidos são do mesmo instante) com prazo de `RUN_TIMEOUT`:

1. Trava de sessão `pg_try_advisory_lock(hashtext('collector-rdns'))`, numa
   conexão dedicada, **antes** do `BEGIN`; solta com `pg_advisory_unlock`
   depois do `COMMIT` (ou do `ROLLBACK`), e, se nem isso der, a conexão é
   fechada (o servidor solta a trava). Em `REPEATABLE READ` o instantâneo sai
   no início da primeira instrução: uma trava de transação pega nessa
   instrução podia vir com um instantâneo anterior ao `COMMIT` da instância
   que acabou de derivar, e esta derivaria de novo sobre dados velhos.
2. Versão: `SELECT uuid, created_at FROM cgibr_run WHERE status = 1 ORDER BY
   created_at DESC LIMIT 1`; a última derivada: `SELECT source_version FROM
   rdns_run WHERE status = 1 AND source = 'cgibr' ORDER BY created_at DESC
   LIMIT 1`. Iguais → sem mudança (`reason` `versão do cgibr já derivada`);
   `cgibr_run` sem linha aplicada → sem mudança (`cgibr sem dataset
   aplicado`). Com `--force`, segue mesmo igual.
3. Unidades:

   ```sql
   CREATE TEMP TABLE stage_unit (
       prefix  cidr    PRIMARY KEY,
       asn     bigint  NOT NULL
   ) ON COMMIT DROP;

   INSERT INTO stage_unit (prefix, asn)
   SELECT DISTINCT ON (u.prefix) u.prefix, u.asn
     FROM (SELECT set_masklen(network(p.prefix) + i * 256, 24)::cidr AS prefix,
                  a.asn, masklen(p.prefix) AS len
             FROM cgibr_prefix p
             JOIN cgibr_asn a ON a.uuid = p.asn_uuid
            CROSS JOIN LATERAL generate_series(0::bigint, (1::bigint << (24 - masklen(p.prefix))) - 1) AS i
            WHERE family(p.prefix) = 4 AND masklen(p.prefix) <= 24) u
    ORDER BY u.prefix, u.len DESC;   -- /24 repetido: vale o bloco mais específico
   ```

   Blocos IPv4 mais longos que /24 viram avisos (`bloco <p> mais longo que
   /24 ignorado`).
4. Novos: `INSERT INTO rdns_prefix (prefix, asn, source) SELECT s.prefix,
   s.asn, 'cgibr' FROM stage_unit s WHERE NOT EXISTS (SELECT 1 FROM
   rdns_prefix p WHERE p.prefix && s.prefix)` — o que já está coberto (pela
   própria linha, por uma estática) fica de fora. Entre `SET LOCAL
   enable_seqscan = off` e `SET LOCAL enable_seqscan = on`: com
   `rdns_prefix` vazia (a primeira derivação) o plano seria uma leitura
   sequencial da tabela, que cresce durante o `INSERT`, para cada /24.
5. ASN: `UPDATE rdns_prefix p SET asn = s.asn FROM stage_unit s WHERE
   p.prefix = s.prefix AND p.source = 'cgibr' AND p.asn <> s.asn` — sem
   `rescan` (o PTR não depende do ASN). Linhas `static` nunca são tocadas.
6. Contagens: `units`, `inserted`, `asn_updated`, `covered` (`units −
   inserted`) e `not_in_source` (linhas `cgibr` sem `/24` igual em
   `stage_unit`); `INSERT` em `rdns_run` com `status = 1`.
7. Manutenção da [sonda](../../padroes/sonda.md#derivação) e `jobs`
   (`last_sync_at` e `consolidated = 0` se `inserted + asn_updated > 0`).

Log `derivação aplicada` com `source`, `version`, `blocks`, `units`,
`inserted`, `asn_updated`, `covered`, `not_in_source`, `warnings`, `forced`
e `ms`; cada aviso também vai para o log (`aviso da derivação`). Com o
arquivo de 2026-10-08, a primeira derivação insere os 308.529 /24 de uma
vez.

## Fila e lease

A [fila da sonda](../../padroes/sonda.md#fila-de-varredura), com a regra
`family(prefix) = 4 AND masklen(prefix) BETWEEN 22 AND 24` e
`SCAN_LEASE` = `2m`:

- Cada instância mantém **vários prefixos em varredura**: pega o próximo
  sempre que restam menos de **256** endereços esperando vez no limitador,
  até **16** prefixos ao mesmo tempo. Um prefixo por vez deixaria a taxa
  ociosa esperando os últimos prazos (até 6 s por endereço) de cada um.
- A varredura de um prefixo tem **`SCAN_LEASE − 5 s`** para as consultas e os
  5 s restantes para a gravação; estourou, abandona.
- Sem prefixo vencido: olha a fila de novo em 30 s.

## Varredura de um prefixo

1. Põe todos os endereços do prefixo no limitador (256 de um /24, 1.024 de um
   /22), em ordem.
2. Cada endereço passa pelo [cliente DNS](#cliente-dns) e vira um estado
   ([fonte.md](fonte.md#respostas-e-estados)).
3. No fim, numa transação: `SELECT ip, status, name, names FROM rdns_ptr
   WHERE ip <<= $1::cidr FOR UPDATE`; as diferenças pelas
   [regras de escrita](dados.md#rdns_ptr) vão num `pgx.Batch` (uma volta ao
   banco) com uma instrução por linha mudada — `INSERT INTO rdns_ptr (ip,
   status, name, names) VALUES (...)`, `UPDATE rdns_ptr SET status = $2, name
   = $3, names = $4 WHERE ip = $1` ou `DELETE FROM rdns_ptr WHERE ip = $1`;
   depois o `UPDATE` de `rdns_prefix` com a conferência do lease e os
   contadores (`last_scan_ms`, `last_scan_ptr`, `last_scan_no_ptr`,
   `last_scan_failed`, `last_scan_changed`); por último, `jobs` se
   `last_scan_changed > 0`.
4. Lease perdido: `ROLLBACK` e log `lease perdido; resultado descartado`.

Log `prefixo varrido` (`debug`) com `prefix`, `ptr`, `no_ptr`, `failed`,
`changed` e `ms`.

## Cliente DNS

`internal/resolver`, com a `miekg/dns`; a consulta e o que cada resposta
vira estão em [fonte.md](fonte.md#consulta).

- **Servidores**: `DNS_SERVERS`, obrigatória e sem padrão — os resolvedores
  recursivos do host onde a instância roda. Rodízio circular por consulta.
- **Tentativas**: até `1 + DNS_RETRIES` (3 no padrão), cada nova tentativa
  noutro servidor do rodízio (quando há mais de um), cada uma com
  `DNS_TIMEOUT` (`2s`):

  | Resposta da tentativa | Faz |
  |---|---|
  | `NOERROR` com `PTR` / NODATA / `NXDOMAIN` | estado final (`ptr` / `no_ptr`) |
  | `SERVFAIL` | a primeira: tenta mais um servidor; a segunda: grava `servfail` |
  | `REFUSED` ou outro `RCODE` | falha do servidor; próxima tentativa |
  | prazo esgotado ou erro de rede | falha do servidor; próxima tentativa |
  | `TC` (truncada) | repete a mesma pergunta por TCP no mesmo servidor, com o mesmo prazo (conta como outra consulta) |

  Esgotou: grava a última falha (`refused`, `timeout` ou `servfail`).
- **Saúde dos servidores**: **5** falhas seguidas (`REFUSED`, outro `RCODE`,
  prazo, erro de rede) tiram o servidor do rodízio por **30 s** (log
  `servidor DNS fora do rodízio`, `server`); qualquer resposta válida
  (inclusive `NXDOMAIN` e `SERVFAIL`) zera a conta. Todos fora: usa todos
  mesmo assim (log `todos os servidores DNS fora do rodízio; usando todos`).
- **Em voo**: no máximo `DNS_INFLIGHT` (`512`) consultas esperando resposta
  por instância.
- Cliente: `dns.Client{Net: "udp", Timeout: DNS_TIMEOUT, UDPSize: 1232}`;
  `msg.SetQuestion(dns.ReverseAddr(ip), dns.TypePTR)`, `msg.RecursionDesired
  = true`, `msg.SetEdns0(1232, false)`.

## Taxa

- `internal/limiter`: uma consulta a cada `1/rate_limit` s (sem rajada), com
  duas filas — a do tempo real é sempre servida antes da da varredura. Toda
  consulta enviada (inclusive novas tentativas e a repetição por TCP) passa
  por ele.
- `rate_limit` e `scan_interval` vêm de `rdns_config` a cada 30 s
  ([sonda](../../padroes/sonda.md#taxa-e-configuração-em-tempo-de-execução)),
  com `RATE_LIMIT` (`256`) e `SCAN_INTERVAL` (`24h`) de reserva. Valor
  inválido na tabela: `valor inválido em rdns_config; usando a reserva`
  (`key`, `value`). Mudou: `configuração alterada` com os valores novos.
- `rate_limit × (SCAN_LEASE − 5 s) < 2.048` (um /22 com uma nova tentativa
  por endereço não cabe no lease): aviso `taxa baixa demais para
  SCAN_LEASE`; suba `SCAN_LEASE` junto.

## Tempo real

O laço da [sonda](../../padroes/sonda.md#tempo-real), com as chaves e
mensagens de [dados.md](dados.md#fila-de-tempo-real-valkey):

1. `BLPOP badblock:rdns:live:queue 5` (5 s de bloqueio, para notar o
   encerramento). Cada pedido tirado é atendido à parte, até **32** ao mesmo
   tempo por instância (o limitador e `DNS_INFLIGHT` continuam valendo).
2. Mensagem ilegível ou IP que não é IPv4: descartada (aviso `pedido de
   tempo real inválido`). `at` com mais de 30 s: descartada, e `DEL` da marca
   `inflight`.
3. O IP tem de estar num prefixo ativo na regra (`SELECT 1 FROM rdns_prefix
   WHERE prefix >>= $1::inet AND active AND family(prefix) = 4 AND
   masklen(prefix) BETWEEN 22 AND 24`); se não, resposta `not_allowed` sem
   consulta.
4. Consulta pela fila de prioridade do limitador; grava em `rdns_ptr` numa
   transação de um IP, com as mesmas regras (e `jobs`, se mudou); o
   `last_scan_at` do prefixo não muda.
5. Num só pipeline (`MULTI`/`EXEC`): `RPUSH` da resposta em
   `badblock:rdns:live:reply:<ip>`, `EXPIRE` de 30 s e `DEL` de
   `badblock:rdns:live:inflight:<ip>`. Falha no Postgres antes disso: aviso
   `não consegui atender o tempo real` e `DEL` da marca `inflight` (o próximo
   pedido do IP pode entrar na fila); quem esperava recebe o dado frio.

Log `tempo real` (`debug`) com `ip`, `result` e `ms`. Sem `REDIS_URL`: `tempo
real desligado (REDIS_URL vazia)` na subida.

## Instâncias e réplicas

- Batida a cada 30 s: `INSERT INTO rdns_worker (uuid, host, version,
  rate_limit, queries, live_queries, prefixes) VALUES (...) ON CONFLICT (uuid)
  DO UPDATE SET rate_limit = EXCLUDED.rate_limit, queries = ...` — o upsert
  recria a linha se a manutenção a apagou durante uma queda longa do banco.
- Compose: sem `container_name`, `deploy.replicas:
  ${COLLECTOR_RDNS_REPLICAS:-1}`; os containers se chamam
  `<projeto>-collector-rdns-<n>` (`badblock-collector-rdns-1` no compose da
  raiz). Com os números de 2026-10-08, **4** instâncias a 256 req/s cumprem a
  meta de 24 h ([fonte.md](fonte.md#fatos-medidos)).
- Outro host: o mesmo `docker-compose.yml`, com `DNS_SERVERS` daquele host
  e `POSTGRES_URL`/`REDIS_URL` que alcancem o host principal pelo caminho de
  rede que o operador montar (fora do repositório).

## Opções

As da [sonda](../../padroes/sonda.md#configuração-comum), mais as do DNS:

| Variável | Argumento | Padrão | Uso |
|---|---|---|---|
| `DNS_SERVERS` | `--dns-servers` | — (obrigatória) | resolvedores recursivos, `ip` ou `ip:porta` (IPv6 `[ip]:porta`), separados por vírgula; porta 53 |
| `DNS_TIMEOUT` | `--dns-timeout` | `2s` | prazo de uma tentativa |
| `DNS_RETRIES` | `--dns-retries` | `2` | novas tentativas, noutro servidor (0 a 5) |
| `DNS_INFLIGHT` | `--dns-inflight` | `512` | consultas em voo por instância (≥ 1) |
| `RATE_LIMIT` | `--rate-limit` | `256` | reserva de `rate_limit` (≥ 1) |
| `SCAN_INTERVAL` | `--scan-interval` | `24h` | reserva de `scan_interval` |
| `SCAN_LEASE` | `--scan-lease` | `2m` | mínimo `30s` |
| `SOURCES` | `--sources` | `cgibr` | `cgibr` ou `none` (só estáticos) |
| `DERIVE_INTERVAL` | `--derive-interval` | `5m` | |
| `RUN_TIMEOUT` | `--run-timeout` | `10m` | prazo de uma derivação |
| `REDIS_URL` | `--redis-url` | vazio | sem ela, sem tempo real |

Opções inválidas (stderr `collector-rdns: <mensagem>`, saída 2), além das
comuns: `defina DNS_SERVERS (ou --dns-servers)` (dispensada só com
`--version`); `servidor DNS inválido "<item>"`; `--dns-retries inválido (0 a
5): "<v>"`; `--dns-inflight inválido: "<v>"`; `--rate-limit inválido:
"<v>"`; `--scan-lease inválido (mínimo 30s): "<v>"`; `--sources inválido:
"<v>" (use cgibr ou none)`; durações como no padrão (`--dns-timeout
inválido: "<v>"`, idem `--scan-interval`, `--derive-interval`,
`--run-timeout`).

A linha `iniciando` traz `version`, `commit`, `worker` (o `uuid`), `host`,
`dns_servers`, `rate_limit`, `scan_interval`, `scan_lease`, `sources`,
`live` (`true` com `REDIS_URL`) e `postgres` (sem a senha).

## Compose e `.env`

| Variável no `.env` | Vira | Padrão no compose | `.env.example` do app e da raiz |
|---|---|---|---|
| `COLLECTOR_RDNS_TAG` | tag e `VERSION` do build | `latest` (`dev` no build) | `latest` |
| `COLLECTOR_RDNS_REPLICAS` | `deploy.replicas` | `1` | `1` |
| `COLLECTOR_RDNS_DNS_SERVERS` | `DNS_SERVERS` | vazio (o app recusa e sai com 2) | vazia, com o comentário "resolvedores recursivos deste host; obrigatória" |
| `COLLECTOR_RDNS_RATE_LIMIT` | `RATE_LIMIT` | `256` | `256` |
| `COLLECTOR_RDNS_SCAN_INTERVAL` | `SCAN_INTERVAL` | `24h` | `24h` |
| `COLLECTOR_RDNS_DERIVE_INTERVAL` | `DERIVE_INTERVAL` | `5m` | — |
| `COLLECTOR_RDNS_SOURCES` | `SOURCES` | `cgibr` | — |
| `COLLECTOR_RDNS_REDIS_URL` | `REDIS_URL` | `redis://:${VALKEY_PASSWORD:-}@badblock-valkey:6379/0` | comentada |

`DNS_SERVERS` passa vazia quando falta, em vez de `:?`: um `:?` derrubaria o
`make up` do stack inteiro; assim só este container recusa subir (e o log
diz por quê). `DNS_TIMEOUT`, `DNS_RETRIES`, `DNS_INFLIGHT`, `SCAN_LEASE` e
`RUN_TIMEOUT` não passam pelo compose.

## Operação

```bash
make -C apps/rdns/collector once     # uma derivação agora (docker compose exec, 1ª réplica)
make -C apps/rdns/collector force    # deriva mesmo sem versão nova do cgibr
COLLECTOR_RDNS_REPLICAS=4 make -C apps/rdns/collector up
make -C database/postgres psql       # e: SELECT count(*) FILTER (WHERE last_scan_at IS NULL), min(last_scan_at) FROM rdns_prefix WHERE active;
```

`once` e `force` rodam `docker compose -p $(STACK) exec collector-rdns`: o
`STACK` padrão é `badblock` (o compose da raiz); com esta pasta no ar
sozinha, `STACK=badblock-collector-rdns`.

A saúde se vê no `/rdns/meta` (fila, instâncias vivas, última derivação), em
`jobs.last_check_at` (anda a cada `DERIVE_INTERVAL`) e nos logs:

| Mensagem | Nível | Campos |
|---|---|---|
| `iniciando` / `encerrando` | info | acima / — |
| `derivação aplicada` | info | acima |
| `derivação sem mudança` | debug | `reason`, `version` |
| `derivação falhou` | error | `err`, `ms` |
| `aviso da derivação` | warn | `source`, `warning` |
| `não consegui gravar a falha em rdns_run` | error | `err` |
| `instâncias sem sinal há mais de 24 h apagadas` | info | `count` |
| `outra instância está derivando` | debug (warn no `--once`) | — |
| `prefixos fora da regra ignorados` | warn | `count` (na subida e quando muda) |
| `prefixo varrido` | debug | acima |
| `varredura abandonada: passou de SCAN_LEASE` / `lease perdido; resultado descartado` | warn | `prefix`, `ms` |
| `não consegui gravar a varredura` / `não consegui pegar um prefixo da fila` | error | `prefix`, `err` / `err` |
| `encerramento sem esperar os prefixos em varredura` | warn | `active` (passou de 10 s) |
| `servidor DNS fora do rodízio` / `servidor DNS de volta ao rodízio` | warn / info | `server`, `failures` |
| `todos os servidores DNS fora do rodízio; usando todos` | warn | — |
| `configuração alterada` / `valor inválido em rdns_config; usando a reserva` | info / warn | `rate_limit`, `scan_interval` / `key`, `value` |
| `taxa baixa demais para SCAN_LEASE` | warn | `rate_limit`, `scan_lease` |
| `batida falhou` / `não consegui apagar a linha da instância` | warn | `err` |
| `tempo real` | debug | `ip`, `result`, `ms` |
| `pedido de tempo real inválido` / `pedido de tempo real vencido descartado` | warn / debug | `payload` / `ip`, `at` |
| `não consegui atender o tempo real` / `não consegui responder o tempo real` | warn | `ip`, `err` |
| `Valkey indisponível; tempo real parado` | warn | `err` (um por queda) |

## Medições

`make test-real DNS_SERVERS=1.1.1.1 PREFIX=200.160.2.0/24` (2026-10-10, um
Mac de desenvolvimento, a 64 consultas/s): o /24 inteiro em **4,2 s** (60,6
endereços/s, perto do teto de 64), **256** consultas (1,00 por endereço: sem
nova tentativa), **91** com PTR (36%), **165** sem PTR, **0** falhas, 91 linhas
gravadas em `rdns_ptr` (64 KiB com o índice, o mínimo de duas páginas).

Stack local (2026-10-10, o mesmo Mac, Docker Desktop, `shared_buffers` de
512 MB; arquivo do NIC.br do dia, 13.048 blocos IPv4):

| Medida | Valor |
|---|---|
| primeira derivação (308.529 /24 inseridos de uma vez, com os triggers) | **152 s** |
| derivação com `--force` sem nada novo (308.529 cobertos) | 65 s |
| `200.160.2.0/24` a 20 consultas/s (`rescan`, `1.1.1.1`) | 13,3 s (256 consultas: 12,8 s de taxa) |
| `rdns_prefix` com os 308.529 prefixos, com os índices | 46 MB |

A primeira tentativa, sem `enable_seqscan = off` nas funções dos triggers e
no `INSERT` da derivação, não terminou em 5 min: os planos feitos com
`rdns_prefix` vazia eram leituras sequenciais, refeitas a cada um dos 308.529
`INSERT` sobre a tabela que crescia ([dados.md](dados.md#a-migration)).

**Pendente**: medir na primeira volta completa, no servidor: consultas/s
reais por instância a 256 req/s, tempo médio de um /24 e tamanho de
`rdns_ptr`.

## Testes específicos

Unitários (`make test`, servidor DNS falso em `127.0.0.1` com
`testdata/zone.txt`):

| Pacote | Confere |
|---|---|
| `resolver` | `ptr` com um e dois nomes (ordem de bytes, minúsculas, sem ponto final, `\DDD`); `no_ptr` em `NXDOMAIN` e NODATA; CNAME RFC 2317 seguido; `SERVFAIL` duas vezes → `servfail`, uma vez e depois `PTR` → `ptr`; `REFUSED` e silêncio esgotando as tentativas, cada uma noutro servidor; 5 falhas seguidas tiram o servidor por 30 s (relógio falso); todos fora → usa todos; truncada → TCP; pergunta trocada descartada; EDNS0 1.232 e `RD` na mensagem enviada |
| `limiter` | intervalo de `1/rate_limit`; tempo real antes da varredura; troca de taxa sem reiniciar |
| `scan` | diferenças pelas regras de escrita (a falha não apaga PTR; `no_ptr` apaga; igual não grava); vários prefixos em voo; abandono no prazo; lease perdido descarta |
| `config` | padrões, precedência, `DNS_SERVERS` obrigatória, itens inválidos, `SOURCES`, mínimo do `SCAN_LEASE` |
| `store` | as regras de escrita de `rdns_ptr` (`Decide`), a contagem dos estados e o teto de avisos |
| `worker` | `rdns_config` vence a reserva, valor inválido volta à reserva com um aviso só, banco fora = reservas, aviso de taxa baixa, o UUIDv7 da instância |

Integração (`make test-int`): PG18 com as migrations de `central/`, `cgibr/`
e `rdns/`, e Valkey (container genérico) — os itens 1 a 3 em `store`, o 4 em
`live` (com um resolvedor falso em `127.0.0.1`):

1. triggers: inserir um /20 sobre um /24 existente vira 15 /24 herdando
   `asn` e `source`; um /24 dentro de um /22 é descartado; o `DELETE` de um
   prefixo apaga os IPs dele em `rdns_ptr`; o `updated_at` não anda com o
   estado da varredura;
2. derivação: carga do `cgibr` de teste, /24 derivados, ASN corrigido sem
   tocar linha `static`, `not_in_source`, versão repetida = sem mudança, duas
   instâncias ao mesmo tempo = uma deriva;
3. fila: duas instâncias nunca pegam o mesmo prefixo; ordem `rescan` →
   nunca varrido → mais antigo; lease vencido é retomado; `rescan` pedido
   durante a varredura sobrevive;
4. tempo real ponta a ponta: pedido na fila → linha em `rdns_ptr` →
   resposta na lista de retorno com expiração → marca `inflight` apagada;
   `not_allowed` fora de prefixo ativo (sem consulta); pedido vencido e
   mensagem ilegível descartados.

Também em `store`: a gravação de uma varredura (só o que mudou, contadores
de `rdns_prefix`, `jobs` só com mudança), o abandono (`Release`), apagar a
instância devolve os prefixos, a restrição de exclusão sem o trigger e as
`CHECK` de `rdns_ptr`.

Rede real (opcional, nunca no `make test`):
`make -C apps/rdns/collector test-real DNS_SERVERS=<resolvedor> PREFIX=200.160.2.0/24`
(variáveis `RDNS_REAL_DNS_SERVERS` e `RDNS_REAL_PREFIX`; o teste
`TestRealNet` de `internal/scan`, pulado sem elas) varre o prefixo num PG18
descartável, a 64 consultas/s, e mostra as contagens e os tempos.
