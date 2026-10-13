# Padrão do coletor de sonda ativa

Vale para o coletor que **mede** a internet em vez de importar um arquivo
publicado: ele mesmo faz as consultas (DNS, hoje) a uma lista de alvos e
grava o resultado, sem parar. Hoje só o `collector-rdns` segue este padrão
([../fontes/rdns/collector.md](../fontes/rdns/collector.md)). O que é da
fonte (protocolo, alvos, tabelas, valores) fica na spec dela; quando ela diz
outra coisa, vale a da fonte — e ela diz por quê.

Dono: sessão principal. É um padrão **só do coletor**: a API de uma fonte de
sonda segue [api.md](api.md), e o que ela faz de diferente fica no `api.md`
da fonte.

## O que vale do padrão dos coletores

Do [padrão dos coletores](coletor.md) valem, sem mudança: a estrutura geral
do código (`cmd/`, `internal/buildinfo`, `internal/config`, `internal/store`),
os passos 1 a 4 do `main.go` (configuração, logger, sinais, Postgres com 2
minutos de tentativas), a ordem padrão → ambiente → argumento, as validações
comuns, a imagem distroless sem porta e sem `HEALTHCHECK`, a disciplina de
escrita (só nas tabelas `<fonte>_*`, na própria linha de `jobs` e em tabelas
temporárias) e as camadas de teste.

**Não valem**: as checagens de mudança, o download, o parser, a trava de
remoção, `SYNC_INTERVAL`, `RETRY_INTERVAL`, `REMOVAL_THRESHOLD`,
`SOURCE_URL` e `USER_AGENT` — não há arquivo a baixar. Também não há
"versão do dataset": o dado muda um alvo de cada vez, o dia inteiro.

## Papel

- Mantém uma **tabela de alvos** (as unidades de varredura) e uma **tabela de
  resultados**. Os alvos vêm de duas origens:
  - **derivados** das tabelas de outras fontes (ex.: os blocos de
    `cgibr_prefix`), que o coletor só **lê** — nunca escreve nelas;
  - **estáticos**, que o administrador grava direto no banco pelo sistema de
    gestão dele.
- Varre cada alvo ativo com uma **meta de intervalo** (`SCAN_INTERVAL`) e
  grava só o que mudou no resultado.
- Atende pedidos de **tempo real** que a API da fonte põe numa fila do
  Valkey, com prioridade sobre a varredura.
- Roda em **várias instâncias** ao mesmo tempo, no mesmo host ou em outros,
  que dividem a fila de alvos sem coordenação além do Postgres.

## Tabelas

Os nomes exatos estão no `dados.md` da fonte; os papéis são estes:

| Papel | Uma linha por | Quem escreve |
|---|---|---|
| alvos (`<fonte>_prefix` no rdns) | unidade de varredura, com o estado da varredura em colunas | o coletor (derivação e varredura) e o sistema de gestão do administrador (estáticos, ativar, desativar, remover, pedir nova varredura) |
| resultados | o que a medição achou de cada alvo | o coletor |
| `<fonte>_run` | derivação que leu uma versão nova de uma fonte de origem | o coletor |
| `<fonte>_worker` | instância do coletor no ar | a própria instância |
| `<fonte>_config` | chave/valor que vale para todas as instâncias | o administrador |

Regras da tabela de alvos:

- **Alvos só entram sozinhos, nunca saem sozinhos.** A derivação insere
  alvos novos e corrige dados de cadastro das linhas derivadas; remover,
  ativar, desativar e pedir nova varredura é do administrador. Um alvo que
  sumiu da fonte de origem continua sendo varrido até o administrador
  decidir — a `<fonte>_run` conta quantos estão nessa situação.
- A derivação **nunca** toca uma linha estática.
- O estado da varredura (quem está varrendo, desde quando, a última
  varredura completa e os contadores dela) mora **em colunas da própria
  linha**: a fila é a tabela.
- `updated_at` anda **só** em mudança de cadastro, nunca em mudança de
  estado da varredura (o trigger de `updated_at` lista as colunas de
  cadastro).
- A regra de quais alvos a sonda aceita (ex.: IPv4 de /22 a /24) fica **só no
  software**: o banco aceita linhas fora dela, a fila nunca as pega, e elas
  são contadas no `/meta` da API e num aviso de log.

## Derivação

A cada `DERIVE_INTERVAL` (padrão `5m`) e na subida, **cada** instância tenta
derivar; com a trava, numa transação só:

1. `SELECT pg_try_advisory_lock(hashtext('collector-<fonte>'))` — trava de
   **sessão**, numa conexão dedicada, pega **antes** do `BEGIN` e solta
   (`pg_advisory_unlock`) depois do `COMMIT`; se nem isso der, a conexão é
   fechada e o servidor solta a trava. Não é trava de transação porque a
   derivação lê em `REPEATABLE READ`, cujo instantâneo sai no início da
   primeira instrução: pega nessa instrução, a trava podia vir com um
   instantâneo anterior ao `COMMIT` da instância que acabou de derivar. Sem a
   trava, outra instância está derivando: a passada termina sem fazer nada
   (log `debug`).
2. Para cada fonte de origem em `SOURCES`: lê a versão do dataset dela (o
   `uuid` da última linha `status = 1` de `<origem>_run`) e compara com a
   última versão derivada (`<fonte>_run`). Igual, ou a origem ainda sem
   dataset aplicado: sem mudança.
3. Mudou: deriva os alvos da origem numa tabela temporária, insere os novos,
   atualiza o cadastro das linhas derivadas que mudaram e grava a linha
   `status = 1` em `<fonte>_run` com a versão lida e as contagens.
4. Manutenção, ainda com a trava: apaga as linhas de `<fonte>_worker` sem
   sinal há mais de **24 h** e conta os alvos fora da regra.
5. `jobs`: `last_check_at = NOW()` em toda passada que pegou a trava;
   `last_sync_at = NOW()` e `consolidated = 0` quando a derivação inseriu ou
   alterou algum alvo.

Falha depois de ler a versão da origem: linha `status = 0` em `<fonte>_run`
com o erro, gravada fora da transação, como as recusas do
[padrão](coletor.md#recusas-e-falhas); antes disso, só log. Não há trava de
remoção: a derivação não remove nada. `--once` faz uma passada de derivação
e sai; `--force` (implica `--once`) deriva mesmo sem versão nova.

A fonte de sonda lê as tabelas de outra fonte: uma mudança no schema dessas
tabelas avisa o sub-agente do coletor de sonda.

## Fila de varredura

Cada instância pega o próximo alvo numa instrução só, sem transação longa:

```sql
UPDATE <alvos> SET scan_worker = $1, scan_started_at = NOW()
 WHERE uuid = (SELECT uuid FROM <alvos>
                WHERE active AND <regra da sonda>
                  AND (scan_worker IS NULL OR scan_started_at < NOW() - $2::interval)
                  AND (rescan OR last_scan_at IS NULL OR last_scan_at < NOW() - $3::interval)
                ORDER BY rescan DESC, last_scan_at ASC NULLS FIRST
                LIMIT 1
                FOR UPDATE SKIP LOCKED)
RETURNING uuid, prefix, rescan, scan_started_at
```

(`$1` a instância, `$2` o `SCAN_LEASE`, `$3` o `SCAN_INTERVAL`.)

- **Ordem**: pedidos de nova varredura (`rescan`) → nunca varridos →
  `last_scan_at` mais antigo. Um índice parcial na ordem da fila, com o
  predicado `active AND <regra da sonda>`, serve a consulta.
- **Meta**: um alvo vence `SCAN_INTERVAL` (padrão `24h`) depois da última
  varredura completa. Sem alvo vencido, a instância espera **30 s** e olha de
  novo. Se a capacidade das instâncias não cobre a meta, a fila só segue a
  ordem (o mais antigo primeiro) — a spec da fonte diz quantas instâncias a
  meta pede.
- **Lease** (`SCAN_LEASE`, padrão `2m`): um alvo pego por uma instância fica
  fora da fila até `scan_started_at + SCAN_LEASE`. A varredura de um alvo
  tem de terminar (resultado gravado) dentro desse prazo; passou, a
  instância **abandona** o alvo sem gravar nada, devolve-o à fila
  (`scan_worker = NULL`) e loga `varredura abandonada: passou de SCAN_LEASE`.
  Uma instância que morre deixa o alvo preso só até o lease vencer.
- **Gravação**: os resultados de um alvo são gravados **no fim**, numa
  transação só, e só o que mudou (linhas novas, alteradas e apagadas). A
  transação lê o estado atual dos resultados do alvo com `FOR UPDATE`, grava
  as diferenças e fecha com:

  ```sql
  UPDATE <alvos> SET last_scan_at = NOW(), scan_worker = NULL, scan_started_at = NULL,
         rescan = CASE WHEN $3 THEN false ELSE rescan END, <contadores> = ...
   WHERE uuid = $1 AND scan_worker = $2 AND scan_started_at = $4
  ```

  (`$3` = o `rescan` lido ao pegar o alvo: um pedido feito durante a
  varredura não se perde.) Nenhuma linha atualizada = o lease venceu e outra
  instância pegou o alvo: `ROLLBACK`, e o resultado é descartado.
- Mudou algum resultado: `jobs.last_sync_at = NOW()` e `consolidated = 0`,
  como **última** instrução da transação (segura a trava da linha de `jobs`
  o menor tempo possível).
- Falha no Postgres ao pegar ou gravar: log, o alvo volta à fila (o lease
  vence) e a instância tenta de novo em **10 s**.

## Taxa e configuração em tempo de execução

- Cada instância faz no máximo `rate_limit` consultas por segundo — a taxa
  vale **por instância**, não para o conjunto. Toda consulta enviada conta,
  inclusive as novas tentativas. As consultas de tempo real passam na frente
  das da varredura.
- `rate_limit` e `scan_interval` vêm da tabela `<fonte>_config`, relida a
  cada **30 s**; a linha que falta, ou que não pode ser lida (banco fora,
  valor inválido), vale a variável de reserva do ambiente (`RATE_LIMIT`,
  `SCAN_INTERVAL`), com aviso no log quando o valor é inválido. Mudar a
  linha muda todas as instâncias em até 30 s, sem reiniciar.

## Instâncias

- Cada instância grava a sua linha em `<fonte>_worker` na subida (host,
  versão) e a renova a cada **30 s** (a batida: taxa em vigor e contadores).
  "Viva" = sinal há menos de **90 s**. O encerramento limpo
  (`SIGINT`/`SIGTERM`) devolve os alvos em varredura à fila e apaga a própria
  linha; a de uma instância que morreu sai na manutenção da derivação, 24 h
  depois.
- No compose, o serviço **não tem `container_name`** e sobe com
  `deploy.replicas: ${COLLECTOR_<FONTE>_REPLICAS:-1}` (um nome fixo impede
  réplicas). Os alvos `once`/`force` do `Makefile` usam
  `docker compose -p $(STACK) exec` (a primeira réplica), não `docker exec`:
  `STACK` é o projeto compose onde as réplicas estão no ar (`badblock` pelo
  compose da raiz, `badblock-collector-<fonte>` com a pasta do app sozinha).
- Instâncias em **outros hosts** sobem pelo `docker-compose.yml` do app, com
  a `POSTGRES_URL` e a `REDIS_URL` apontando para o host principal. O stack
  não publica o Postgres nem o Valkey fora do host (o Postgres só no
  loopback, o Valkey em lugar nenhum): o caminho de rede entre os hosts
  (VPN, túnel) é do operador e fica fora do repositório.

## Tempo real

A API da fonte põe pedidos numa fila do Valkey; as instâncias os atendem com
prioridade. As chaves e o formato de cada mensagem estão no `dados.md` da
fonte; o mecanismo é este:

1. A API faz `RPUSH` do pedido na fila (uma lista **sem TTL**) e espera a
   resposta numa **lista de retorno** própria do alvo, com `BLPOP`.
2. Cada instância tem um laço com `BLPOP` na fila. Pedido mais velho que
   **30 s** é descartado (ninguém mais espera por ele).
3. A instância mede o alvo com prioridade na taxa, grava o resultado na
   tabela de resultados com as mesmas regras da varredura (e `jobs`, se
   mudou) e **só depois** faz `RPUSH` da resposta na lista de retorno, com
   expiração de **30 s**.
4. `REDIS_URL` vazia desliga o tempo real na instância (log na subida);
   Valkey fora: o laço tenta de novo a cada **5 s**, com um aviso por queda.

A fila não tem TTL, por isso o Valkey usa `maxmemory-policy volatile-lru`:
só as chaves com TTL (o cache das APIs, as listas de retorno) podem ser
despejadas ([../plataforma/valkey.md](../plataforma/valkey.md)).

## Configuração comum

Ordem e validação como no [padrão](coletor.md#configuração-comum); valem
`POSTGRES_URL`, `LOG_LEVEL`, `LOG_FORMAT`, `--once`, `--force` e
`--version`, mais:

| Variável | Argumento | Padrão | Uso |
|---|---|---|---|
| `REDIS_URL` | `--redis-url` | vazio (sem tempo real) | `redis://:<senha>@badblock-valkey:6379/0` |
| `SOURCES` | `--sources` | da fonte | fontes de origem dos alvos derivados, separadas por vírgula; `none` desliga a derivação (só estáticos) |
| `DERIVE_INTERVAL` | `--derive-interval` | `5m` | entre passadas da derivação |
| `RUN_TIMEOUT` | `--run-timeout` | `10m` | limite de uma passada da derivação |
| `SCAN_INTERVAL` | `--scan-interval` | `24h` | meta por alvo; reserva de `scan_interval` |
| `SCAN_LEASE` | `--scan-lease` | `2m` | prazo de um alvo pego; mínimo `30s` |
| `RATE_LIMIT` | `--rate-limit` | da fonte | consultas por segundo; reserva de `rate_limit` |

As opções do protocolo (servidores, prazos, novas tentativas) ficam na spec
da fonte. No compose, cada variável ganha o prefixo `COLLECTOR_<FONTE>_` no
`.env`, mais `COLLECTOR_<FONTE>_REPLICAS`.

## Testes

| Camada | O quê |
|---|---|
| Unitários (`make test`) | o cliente do protocolo contra um **servidor falso** em `127.0.0.1` (respostas, falhas, prazos, truncamento); a interpretação das respostas; a taxa; a configuração; a varredura com store falso |
| Integração (`make test-int`) | PG18 e Valkey descartáveis (testcontainers; o Valkey como container genérico `valkey/valkey:9-alpine`), com as migrations reais de `central/`, das fontes de origem e da fonte: derivação, fila com duas instâncias, lease vencido, gravação só do que mudou, triggers, tempo real ponta a ponta |
| Rede real (`make test-real`, opcional) | um alvo de verdade contra resolvedores de verdade, com tempos; nunca roda sozinho |

Os testes unitários e de integração não usam a rede externa.
