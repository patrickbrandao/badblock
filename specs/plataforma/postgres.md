# PostgreSQL

PostgreSQL 18 **dedicado** ao BadBlock. Todo SQL de schema do projeto mora
em `database/postgres/`; os apps só leem e escrevem dados.

## Acesso

- Banco `badblock`, criado pela própria imagem na primeira subida do volume
  (`POSTGRES_DB`), schema `public` (o prefixo da fonte separa as tabelas).
- Migrations, coletores e APIs conectam com o usuário `postgres`
  (superusuário), cada um pela sua `POSTGRES_URL`:
  `postgres://postgres:<senha>@badblock-postgres:5432/badblock?sslmode=disable`.
  Sem `POSTGRES_URL`, os composes a montam com `POSTGRES_PASSWORD`.
- **Não há** roles por app, `GRANT`, `CREATE ROLE` nem bootstrap. APIs não
  escrevem por disciplina de código; cada coletor só escreve nas próprias
  tabelas, na própria linha de `jobs` e em tabelas temporárias.
- **Sem backup**: os dados vêm das fontes externas e são descartáveis.
  Apagar o volume e subir de novo recarrega tudo (`make clean-data`).
- Senha só com letras e números (entra na URL sem codificação).

## Servidor (`database/postgres/docker-compose.yml`)

Projeto compose `badblock-postgres`; sobe sozinho a partir da pasta (com o
`.env` dela) ou pelo compose da raiz.

| Item | Valor |
|---|---|
| Serviço / container | `postgres` / `badblock-postgres` (alias `badblock-postgres` na rede `badblock`) |
| Imagem | `postgres:18-trixie` |
| Ambiente | `POSTGRES_USER=postgres`, `POSTGRES_PASSWORD` (obrigatória), `POSTGRES_DB=badblock` |
| Parâmetros | `shared_buffers=${POSTGRES_SHARED_BUFFERS:-512MB}`, `work_mem=32MB`, `maintenance_work_mem=256MB`, `max_connections=${POSTGRES_MAX_CONNECTIONS:-200}` |
| Porta | `127.0.0.1:${POSTGRES_HOST_PORT:-5433}:5432` — só loopback (admin local ou túnel SSH) |
| Volume | `badblock-postgres-data` em `/var/lib/postgresql` (no PG 18 o `PGDATA` é `/var/lib/postgresql/18/docker`) |
| Healthcheck | `pg_isready -h 127.0.0.1 -U postgres -d badblock` (TCP: no initdb o servidor temporário só escuta no socket), a cada 5 s, prazo de 5 s, 30 tentativas, 60 s de início |
| Outros | `shm_size: 256mb`, `stop_grace_period: 60s`, `restart: unless-stopped`, `traefik.enable=false`, log json-file 10m × 3 |

`database/postgres/.env.example` (usado só quando a pasta sobe sozinha):
`POSTGRES_PASSWORD=`, `# POSTGRES_URL=`, `POSTGRES_HOST_PORT=5433`,
`POSTGRES_SHARED_BUFFERS=512MB`, `POSTGRES_MAX_CONNECTIONS=200`.

## Migrations

- Ferramenta: **dbmate** (imagem `ghcr.io/amacneil/dbmate:2.36`), arquivos
  `AAAAMMDDHHMMSS_<descricao>.sql` com `-- migrate:up` e `-- migrate:down`
  **funcionais** (o down desfaz o up por inteiro). Única exceção: uma
  extensão do Postgres (ex.: `pg_trgm`, do ripe/asnames) é criada com
  `CREATE EXTENSION IF NOT EXISTS` e **não** sai no down, porque é do banco
  inteiro e outras pastas podem usá-la.
- Pastas: `central/` (função `set_updated_at()` e tabela `jobs`) e **uma pasta
  por fonte** (`database/postgres/<fonte>/`, só com tabelas `<fonte>_*`).
- Cada pasta tem a própria tabela de controle, `<pasta>_schema_migrations`:
  as fontes evoluem o schema sem conflito entre si.
- Serviço `migrate` do compose (container `badblock-postgres-migrate`,
  `restart: "no"`): espera o Postgres saudável (`depends_on` com
  `service_healthy`), roda `entrypoint: ["/bin/sh", "/db/migrate.sh"]` com
  `command: ["up"]` (a pasta montada em `/db`, só leitura) e sai;
  `POSTGRES_URL` montada como a dos apps, `DBMATE_WAIT_TIMEOUT=120s`, rede
  `badblock`, `traefik.enable=false`. Roda a cada `up` do stack.

### `migrate.sh`

1. Exige `POSTGRES_URL`; raiz das pastas em `MIGRATIONS_ROOT` (padrão `/db`);
   comando = argumentos (padrão `up`).
2. Ordem: `central` primeiro, depois cada pasta com `.sql`, em ordem
   alfabética. Em `rollback`/`down`, a ordem inversa (fontes antes do
   central).
3. `MIGRATE_ONLY=<pasta>` restringe a uma pasta.
4. Para cada pasta: `dbmate --env POSTGRES_URL --no-dump-schema --wait
   --migrations-dir <raiz>/<pasta> --migrations-table <pasta>_schema_migrations <comando>`.

### `test-migrations.sh` (`make -C database/postgres test`)

Num PG18 descartável (`postgres:18-trixie`, senha `testpw`, banco
`badblock`; container e rede `badblock-migrations-test-<pid>`, removidos no
fim por um `trap`):

1. espera o `pg_isready` (até 60 tentativas de 1 s; estourou, mostra o
   `docker logs` e falha);
2. `up` de tudo, rodando o próprio `migrate.sh` na imagem do dbmate com a
   pasta montada só leitura;
3. `rollback` de cada migration, pasta por pasta (fontes em ordem alfabética
   inversa, `central` por último), e confere que **não sobra nenhuma tabela**
   (`pg_tables` do schema `public`, sem as `%schema_migrations`);
4. `up` de novo, conferindo que há tabelas; termina com
   `migrations OK (<n> tabelas)`.

Falha se qualquer up ou down quebrar.

### Rotinas (`database/postgres/Makefile`)

```bash
make -C database/postgres migrate                    # aplica o que falta (central primeiro)
make -C database/postgres status                     # estado de cada pasta
make -C database/postgres rollback APP=<fonte>       # desfaz a última migration da pasta
make -C database/postgres new APP=<fonte> NAME=x     # cria <fonte>/AAAAMMDDHHMMSS_x.sql (UTC) só com os marcadores up/down
make -C database/postgres test                       # up → rollback de tudo → up
make -C database/postgres psql                       # psql no container
```

O `Makefile` usa `docker compose --env-file ../../.env`.

## Estilo das tabelas

Siga as skills `postgres-table-style` e `postgres-uuidv7` do MCP
`badblock-dev` (`.mcp.json`). Em resumo:

- PK `uuid uuid PRIMARY KEY DEFAULT uuidv7()`; datas `timestamptz`;
  `created_at` e `updated_at` `NOT NULL DEFAULT NOW()`, com o trigger
  `trg_<tabela>_updated_at BEFORE UPDATE ... EXECUTE FUNCTION set_updated_at()`.
- Colunas alinhadas; constraints **nomeadas** — `uq_<tabela>_<coluna>`,
  `fk_<tabela>_<referência>`, `chk_<tabela>_<regra>` —, índices
  `ix_<tabela>_<coluna>` e triggers `trg_<tabela>_<evento>`. A PK é declarada
  inline (`uuid uuid PRIMARY KEY DEFAULT uuidv7()`) e fica com o nome padrão
  do Postgres, `<tabela>_pkey`.
- `COMMENT ON` em **toda** tabela e coluna (em produção, `\d+ <tabela>`
  explica o modelo sem as specs).
- Cabeçalho do arquivo com o que a migration cria e de que depende.
- Sem `GRANT`, `CREATE ROLE` ou dono explícito.

O DDL de cada fonte (colunas, tipos, constraints, índices) está em
`specs/fontes/<fonte>/dados.md`; a migration o implementa.

## Tabela `jobs`

Contrato entre os coletores (fase 1) e a consolidação central (fase 2). Uma
linha por coletor, criada por ele na primeira verificação bem-sucedida.

| Coluna | Tipo | Regras | Significado |
|---|---|---|---|
| `uuid` | `uuid` | PK, `DEFAULT uuidv7()` | |
| `app` | `text` | `NOT NULL`, `uq_jobs_app`, `chk_jobs_app` (`^[a-z0-9]+(-[a-z0-9]+)*$`) | nome do app coletor (`collector-<fonte>`) |
| `last_sync_at` | `timestamptz` | | último dataset novo aplicado; NULL = nenhum ainda |
| `last_check_at` | `timestamptz` | | última verificação bem-sucedida, com ou sem mudança (o coletor está vivo?) |
| `consolidated` | `smallint` | `NOT NULL DEFAULT 0`, `chk_jobs_consolidated` (0–1) | `0` = as tabelas do app mudaram e precisam ser consolidadas; `1` = consolidado |
| `created_at`, `updated_at` | `timestamptz` | `NOT NULL DEFAULT NOW()`; trigger `trg_jobs_updated_at` | |

| Quem | Grava |
|---|---|
| coletor | `app` (upsert), `last_check_at` em toda verificação bem-sucedida, `last_sync_at` em todo dataset novo aplicado e `consolidated = 0` quando essa aplicação mudou alguma linha |
| consolidação (fase 2) | `consolidated = 1` depois de copiar os dados |

`central/20260928230000_jobs.sql` cria também a função de trigger
`set_updated_at()` (`plpgsql`: `NEW.updated_at = NOW(); RETURN NEW;`), usada por
toda tabela com `updated_at`. O down apaga `jobs` e a função.
