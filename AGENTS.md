# Regras do repositório BadBlock

Vale para pessoas e para agentes de IA. Leia antes de mudar qualquer coisa.

## Estrutura

- `apps/<app>/` — cada aplicação é independente: `go.mod` próprio, `Dockerfile`,
  `docker-compose.yml`, `Makefile`, `.env.example` e testes. **Não existe
  código Go compartilhado entre apps**; o contrato entre eles é o banco.
- `database/postgresql/` — **único lugar que escreve SQL de schema**:
  bootstrap (roles, banco, extensões) e migrations (dbmate). Apps nunca criam
  tabelas, views ou roles.
- `database/valkey/`, `infra/traefik/` — infraestrutura de apoio.
- `docker-compose.yml` da raiz — só `include` dos composes de cada pasta; é o
  mesmo em desenvolvimento e produção.
- `tmp/` — material de referência local, fora do git.
- `release-images.sh` — publicação manual das imagens no Docker Hub,
  alternativa ao CI. Os demais `.sh` da raiz (`run-local.sh`, `run-prod.sh`,
  `sync.sh`...) são do ambiente do mantenedor e ficam fora do git;
  `run-prod.sh` e `run-builder.sh` só mudam com pedido explícito.

## Banco de dados

- Schemas: `ingest` (dados por fonte), `registry` (central e histórico), `api`
  (views). Roles: `badblock_owner` (dono, migrations), `badblock_sync`
  (escreve ingest/registry), `badblock_api` (lê só `api`).
- A registry-api só lê views do schema `api`. Precisa de um campo novo? Crie
  uma migration que altere a view — nunca dê à API acesso às tabelas.
- Toda migration tem `-- migrate:up` e `-- migrate:down` funcionais;
  `make -C database/postgresql test` sobe, desce e sobe tudo de novo.
- Nomes de migration: `make -C database/postgresql new NAME=descricao`.
- Nada é apagado das tabelas centrais: remoção é `removed_at`, e toda mudança
  visível vai para `registry.change_log`.

## Código

- Go 1.27, só biblioteca padrão mais pgx, go-redis e testcontainers.
- Identificadores em inglês; comentários, documentação e mensagens de commit em
  português (PT-BR, com acentos).
- Configuração sempre na ordem: padrão → variável de ambiente → argumento de
  linha de comando; `--help` lista tudo com o nome da variável.
- Todo serviço HTTP expõe `/health` e `/status` (GET e POST, JSON com
  `success`, `status`, `timestamp`, `message`) e `/ping` (`pong`).
- Imagens distroless, não-root; healthcheck pelo próprio binário
  (`<app> healthcheck`).

## Segredos

O repositório é **público**. Nenhuma senha, token ou chave entra no git:
valores reais só em `.env` (fora do git); `.env.example` documenta as
variáveis sem valores. O CI roda gitleaks no histórico inteiro. Senhas só com
letras e números (entram em URLs de conexão).

## Testes antes de abrir PR

```bash
make test test-int lint          # apps
make -C database/postgresql test # se mexeu em database/
make e2e                         # com o stack no ar (make up)
```

Mudou o formato de uma fonte? Atualize as fixtures
(`make -C apps/registry-sync fixtures SRC=...`) e os testes do parser.

## Git

- `main` protegida: mudanças entram por PR com o CI verde.
- Release de um app: `make -C apps/<app> release V=X.Y.Z` cria a tag
  `<app>/vX.Y.Z`, e o GitHub Actions publica `tmsoftbrasil/badblock-<app>`.
