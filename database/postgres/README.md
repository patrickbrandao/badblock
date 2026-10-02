# PostgreSQL

PostgreSQL 18 dedicado ao BadBlock e **todo o SQL de schema** do projeto:
`central/` (função `set_updated_at()` e tabela `jobs`) e uma pasta de
migrations (dbmate) por fonte, com as tabelas `<fonte>_*`.

Especificação (fonte da verdade): [`specs/plataforma/postgres.md`](../../specs/plataforma/postgres.md);
as tabelas de cada fonte, em `specs/fontes/<fonte>/dados.md`.

```bash
make -C database/postgres migrate                    # aplica o que falta
make -C database/postgres new APP=<fonte> NAME=x     # nova migration
make -C database/postgres rollback APP=<fonte>       # desfaz a última da pasta
make -C database/postgres test                       # up → rollback → up (PG descartável)
make -C database/postgres psql
```
