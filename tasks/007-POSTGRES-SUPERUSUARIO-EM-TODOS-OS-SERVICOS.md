# 007 — Todos os serviços, inclusive as 14 APIs públicas, usam o superusuário do Postgres

- **Severidade:** baixa
- **Confiança:** confirmado
- **Categoria:** ponto fraco
- **Subcategoria:** privilégio mínimo / defesa em profundidade
- **Status:** aberto — correção não aplicada
- **Arquivos afetados:**
  - `specs/plataforma/postgres.md:10-17`
  - `specs/projeto/arquitetura.md:47-49`
  - `specs/projeto/decisoes.md:12`
  - `.env.example:8-14`
  - `apps/lacnic/api/docker-compose.yml:25-26` (o mesmo trecho nos composes das outras 13 APIs e dos 14 coletores)
  - `database/postgres/migrate.sh:12`
- **Depende de / relacionado a:** nenhum (Valkey sem senha e `sslmode=disable` nas sondas remotas foram avaliados como contexto; ver Limitações)
- **Esforço estimado:** M (até 1 dia)
- **Auditoria:** 2026-11-07, commit `db43842`

## Explicação para leigos

Todos os programas do BadBlock entram no banco de dados com a "chave mestra", o usuário `postgres`. Isso vale também para as 14 APIs abertas na internet.

Hoje nenhuma delas tem falha conhecida que deixe alguém mandar comandos ao banco. Mas, se um dia aparecer uma, o invasor não fica limitado a ler dados públicos: ele pode apagar tudo e até rodar programas dentro do servidor do banco. Dar às APIs uma chave "só de leitura" fecharia essa porta sem mudar nada do que elas fazem.

## Evidência e análise técnica

- `specs/plataforma/postgres.md:10-17`:
  - migrations, coletores e APIs se conectam com o usuário `postgres` (superusuário);
  - **não há** roles por app, `GRANT`, `CREATE ROLE` nem bootstrap;
  - as APIs não escrevem "por disciplina de código".
- `specs/projeto/arquitetura.md:47-49` (regra 9) e `specs/projeto/decisoes.md:12` (decisão nº 4) registram essa escolha, com o motivo "simplicidade".
- `apps/lacnic/api/docker-compose.yml:26`: `POSTGRES_URL: ${POSTGRES_URL:-postgres://postgres:${POSTGRES_PASSWORD:?...}@badblock-postgres:5432/badblock?sslmode=disable}`. A mesma API está nas redes `badblock` e `traefik` (linha 38) e é publicada pelo Traefik (linhas 39-54).
- Em `database/postgres/**` não há `GRANT`, `REVOKE` nem `CREATE ROLE`. As únicas ocorrências de "grant" são colunas `grant_*` da anatel_pst.
- Nenhuma API faz INSERT, UPDATE ou DELETE fora dos testes. Só três abrem transação `pgx.ReadOnly`:
  - `apps/anatel/pst/api/internal/store/store.go:200`
  - `apps/iana/api/internal/store/store.go:199`
  - `apps/rootzone/api/internal/store/store.go:188`
- `READ ONLY` não segura um superusuário, que pode simplesmente rodar `SET TRANSACTION READ WRITE`. E `COPY ... TO PROGRAM`, permitido a superusuários, executa comandos do sistema operacional dentro do container do Postgres.

## Verificação (tentativa de refutação)

1. **A evidência existe?** Sim, nas linhas citadas.
2. **Há proteção em outro lugar?** Em parte:
   - o SQL é parametrizado (pgx), e não achei SQL montado com `Sprintf` fora dos testes;
   - o Postgres só escuta no loopback, e o container fica só na rede `badblock`;
   - o Traefik aplica rate limit.

   Nada disso limita o estrago de uma injeção de SQL.
3. **É alcançável?** Sim. As APIs são o ponto de entrada público, e os coletores processam dados de terceiros (RDAP, root.zone, CSV da Anatel).
4. **A pré-condição é realista?** Ela exige uma vulnerabilidade de injeção, que hoje não existe. Por isso a severidade é baixa.
5. **É intencional?** Sim, é a decisão nº 4: risco aceito em troca de simplicidade. Mas o motivo dado ("os dados são descartáveis") não cobre execução de comandos no host do banco.

## Impacto

Se uma injeção de SQL aparecer numa API ou num coletor, o atacante ganha:
- leitura e escrita em todas as tabelas, inclusive `rdns_prefix` e `rdns_config`, que o administrador grava;
- `DROP` de tudo;
- `COPY TO PROGRAM`, isto é, execução de comandos no container `badblock-postgres`, com acesso à rede `badblock`.

Hoje não há vetor conhecido.

## Solução proposta

1. **Spec primeiro.** Revisar a decisão nº 4 em `specs/projeto/decisoes.md` e as regras de `specs/plataforma/postgres.md` e `specs/projeto/arquitetura.md` (regra 9). Donos: a sessão principal e o app `postgres-migrate`.
2. **Role só de leitura.** Numa migration de `database/postgres/central/`, criar a role `badblock_readonly`:
   - LOGIN, sem senha na migration;
   - `CONNECT` no banco, `USAGE` em `public` e `SELECT` em todas as tabelas;
   - `ALTER DEFAULT PRIVILEGES ... GRANT SELECT` para as tabelas futuras.
3. **Senha fora do git.** O `migrate.sh` aplica a senha com `ALTER ROLE` quando `POSTGRES_READONLY_PASSWORD` existir, e o `make env` passa a gerá-la.
4. **APIs com a role nova.** As APIs passam a usar `API_POSTGRES_URL` com `badblock_readonly`, e caem em `POSTGRES_URL` se ela não existir, para manter compatibilidade. As APIs `api-rdns` e `api-registrobr-rdapnum` escrevem só no Valkey, então também funcionam só com leitura; confirmar com os sub-agentes delas.
5. **Coletores (opcional, esforço G).** Uma role por coletor, com escrita só nas próprias tabelas e na própria linha de `jobs`.

## Diff/patch proposto — NÃO APLICADO

```diff
--- /dev/null
+++ b/database/postgres/central/<timestamp>_readonly_role.sql
@@ -0,0 +1,17 @@
+-- migrate:up
+DO $$ BEGIN
+  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'badblock_readonly') THEN
+    CREATE ROLE badblock_readonly LOGIN;
+  END IF;
+END $$;
+GRANT CONNECT ON DATABASE badblock TO badblock_readonly;
+GRANT USAGE ON SCHEMA public TO badblock_readonly;
+GRANT SELECT ON ALL TABLES IN SCHEMA public TO badblock_readonly;
+ALTER DEFAULT PRIVILEGES FOR ROLE postgres IN SCHEMA public
+  GRANT SELECT ON TABLES TO badblock_readonly;
+
+-- migrate:down
+ALTER DEFAULT PRIVILEGES FOR ROLE postgres IN SCHEMA public
+  REVOKE SELECT ON TABLES FROM badblock_readonly;
+REVOKE ALL ON ALL TABLES IN SCHEMA public FROM badblock_readonly;
+DROP ROLE IF EXISTS badblock_readonly;
--- a/apps/lacnic/api/docker-compose.yml
+++ b/apps/lacnic/api/docker-compose.yml
@@ -24,4 +24,4 @@
     environment:
-      # Usuário postgres no banco badblock. Senhas só com letras e números.
-      POSTGRES_URL: ${POSTGRES_URL:-postgres://postgres:${POSTGRES_PASSWORD:?defina POSTGRES_PASSWORD no .env}@badblock-postgres:5432/badblock?sslmode=disable}
+      # Role só de leitura no banco badblock. Senhas só com letras e números.
+      POSTGRES_URL: ${API_POSTGRES_URL:-postgres://badblock_readonly:${POSTGRES_READONLY_PASSWORD:?defina POSTGRES_READONLY_PASSWORD no .env}@badblock-postgres:5432/badblock?sslmode=disable}
```

Antes de aplicar, conferir se a imagem do migrate tem `psql` para o `ALTER ROLE ... PASSWORD`. Se não tiver, a senha vai por outro passo.

## Critérios de aceite

- [ ] A decisão nº 4 e as specs `postgres.md` e `arquitetura.md` foram revisadas antes do código.
- [ ] A role `badblock_readonly` existe só com `SELECT`, e as tabelas novas herdam a permissão.
- [ ] As 14 APIs usam essa role. Pela conexão de uma API, `INSERT` e `COPY ... TO PROGRAM` falham com `permission denied`.
- [ ] Nenhuma senha entra no git, e o `make env` gera `POSTGRES_READONLY_PASSWORD`.

## Testes recomendados

- Teste de integração com testcontainers: aplicar as migrations, conectar como `badblock_readonly`, confirmar que `SELECT` funciona e que `INSERT`, `DROP` e `COPY TO PROGRAM` falham.
- Rodar `make test-int` de cada API com a role nova.

## Limitações e suposições

- Não achei vetor de injeção. O achado é só de defesa em profundidade.
- Avaliado e refutado como achado próprio: o Valkey sem senha por padrão. Ele só é alcançável pela rede `badblock`, cujos membros receberiam a senha de qualquer forma. A escolha está documentada em `specs/plataforma/valkey.md:56-58`.
- O `sslmode=disable` das sondas remotas pressupõe o túnel ou VPN do operador (`specs/padroes/sonda.md:193-194`).
