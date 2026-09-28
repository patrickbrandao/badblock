# PostgreSQL

PostgreSQL 18 do BadBlock: bootstrap, migrations, backup e o contrato de
leitura das APIs. **Todo SQL de schema do projeto mora aqui**; os apps só leem
e escrevem dados.

| Arquivo | Papel |
|---|---|
| `docker-compose.yml` | Serviços `postgres`, `migrate` (dbmate, roda a cada `up` e sai) e `backup` |
| `bootstrap/00-bootstrap.sh` + `bootstrap.psql` | Roles, banco, grants e extensões; idempotente |
| `migrations/*.sql` | Schemas `ingest`, `registry` e `api` (dbmate: `-- migrate:up` / `-- migrate:down`) |
| `backup/backup.sh`, `restore.sh`, `verify.sh` | Dump diário com retenção, restauração e prova de restauração |
| `test-migrations.sh` | Sobe, desce e sobe todas as migrations num PG descartável |

## Roles

| Role | Acesso |
|---|---|
| `badblock_owner` | Dono do banco e dos objetos; roda as migrations e o backup |
| `badblock_sync` | registry-sync: lê e escreve `ingest` e `registry` |
| `badblock_api` | registry-api: `SELECT` só nas views de `api` |

As senhas vêm do `.env` (`BADBLOCK_*_PASSWORD`). O bootstrap roda sozinho na
primeira subida do volume; `make bootstrap` reaplica (troca de senhas).

## Rotinas

```bash
make migrate                 # aplica as pendentes
make status                  # aplicadas e pendentes
make new NAME=descricao      # cria migrations/AAAAMMDDHHMMSS_descricao.sql
make rollback                # desfaz a última
make test                    # up → rollback de todas → up, e confere permissões
make psql                    # psql como superusuário
make backup / backups / verify / restore FILE=...
make schema                  # exporta schema.sql só para leitura
```

Ao mudar o que a API lê, altere as views de `api` numa migration nova; as
tabelas podem mudar livremente desde que as views mantenham o contrato.

O volume fica em `/var/lib/postgresql` (no PG 18 o `PGDATA` é
`/var/lib/postgresql/18/docker`).
