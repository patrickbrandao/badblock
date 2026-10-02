# Arquitetura

Regras que valem para o projeto inteiro. Cada uma tem o motivo; o histórico
das decisões está em [decisoes.md](decisoes.md).

## Regras

1. **Um processo por app, uma pasta por app**, agrupadas pela fonte:
   `apps/<fonte>/collector/` e `apps/<fonte>/api/`, cada uma com módulo Go,
   `Dockerfile`, `docker-compose.yml`, `Makefile`, `.env.example` e `README.md`
   próprios ([estrutura.md](estrutura.md#pasta-de-um-app)).
   *Motivo: cada app evolui, é testado e é implantado sozinho.*
2. **Não existe código compartilhado entre apps.** O contrato entre eles é o
   banco (tabelas `<fonte>_*` e `jobs`). Apps parecidos são **clones** de um
   modelo (os cinco RIRs, clonados do lacnic): a spec do modelo é uma só, o
   código é copiado, e o que é de cada um fica num pacote próprio
   (`internal/rir`). Bug no código comum se corrige no modelo e nos clones.
   *Motivo: nenhuma mudança num app quebra outro por tabela.*
3. **Deploy e container individuais.** O `docker-compose.yml` de cada app sobe
   sozinho a partir da pasta dele; a conexão com o banco vem sempre de
   `POSTGRES_URL` e a do cache, de `REDIS_URL`. O compose da raiz só junta tudo
   para o desenvolvimento local ([../plataforma/docker.md](../plataforma/docker.md)).
4. **Coletores não têm API web.** Rodam em laço, com o período em
   `SYNC_INTERVAL`, e **só aplicam quando a fonte muda** (hash publicado,
   `ETag`/`Last-Modified`, hash do conteúdo). Nunca aplicam dataset parcial ou
   truncado, nem — quando a fonte permite saber (serial ou data no arquivo) —
   mais antigo que o atual
   ([../padroes/coletor.md](../padroes/coletor.md)).
5. **Toda fonte tem uma API** puramente HTTP que só lê as tabelas da fonte.
   Ela é dona de tudo abaixo de `https://api.badblock.net.br/<fonte>/`,
   inclusive das versões (`/<fonte>/v1/...`); a rota sem versão é a versão
   atual ([../padroes/api.md](../padroes/api.md)).
6. **Tabelas com o prefixo da fonte**: `<fonte>_<tabela>`. O nome da fonte é o
   mesmo nos dois apps, no prefixo das tabelas, na pasta das migrations e na
   pasta das specs.
7. **Todo SQL de schema mora em `database/postgres/`** (migrations dbmate com
   `-- migrate:up` e `-- migrate:down` funcionais). Apps nunca criam tabelas,
   views, índices ou roles — só as tabelas temporárias de staging da própria
   transação do coletor ([../plataforma/postgres.md](../plataforma/postgres.md)).
8. **Tabela central `jobs`**: cada coletor mantém a própria linha
   (`app = 'collector-<fonte>'`) com `last_sync_at`, `last_check_at` e
   `consolidated`; grava `consolidated = 0` quando suas tabelas mudam, e a
   consolidação da fase 2 grava `1` depois de copiar os dados.
9. **Banco dedicado, acesso único**: todos os serviços usam o usuário
   `postgres` no banco `badblock`, só por `POSTGRES_URL`. Sem roles por app,
   `GRANT`, bootstrap nem backup (os dados são descartáveis).
10. **Cache opcional e fail-open**: a API funciona sem o Valkey; a versão do
    dataset vai na chave, então nada precisa ser invalidado.
11. **Publicação pelo Traefik do servidor**, que publica todo container por
    padrão: containers internos levam `traefik.enable=false`
    ([../plataforma/publicacao.md](../plataforma/publicacao.md)).
12. **Specs são a fonte da verdade**: a mudança de comportamento começa na
    spec, depois vem o código e os testes
    ([../processos/fluxo-de-trabalho.md](../processos/fluxo-de-trabalho.md)).

## Componentes

| Componente | Imagem | Rede | Publica | Depende de |
|---|---|---|---|---|
| `badblock-postgres` | `postgres:18-trixie` | `badblock` | `127.0.0.1:5433` (admin) | — |
| `badblock-postgres-migrate` | `ghcr.io/amacneil/dbmate:2.36` | `badblock` | — | Postgres saudável; roda e sai |
| `badblock-valkey` | `valkey/valkey:9-alpine` | `badblock` | — | — |
| `badblock-collector-<fonte>` | `tmsoftbrasil/badblock-collector-<fonte>` | `badblock` | — | Postgres (+ migrations, no compose da raiz) e a fonte externa |
| `badblock-api-<fonte>` | `tmsoftbrasil/badblock-api-<fonte>` | `badblock` + `TRAEFIK_NETWORK` | Traefik (`/<fonte>/`) e `127.0.0.1:<porta>` | Postgres; Valkey opcional |
| `badblock-website-<site>` | `tmsoftbrasil/badblock-website-<site>` | só `TRAEFIK_NETWORK` | Traefik (host do site) e `127.0.0.1:82NN` | — ([../padroes/website.md](../padroes/website.md)) |

```
            ┌──────────────── rede badblock ─────────────────┐
fonte ──▶ collector-<fonte> ──▶ postgres ◀── migrate (sai)    │
            │                     ▲                           │
            │                     │                           │
            │                 api-<fonte> ──▶ valkey          │
            └─────────────────────┼───────────────────────────┘
                                  │ rede TRAEFIK_NETWORK
                               Traefik ◀── https://api.badblock.net.br/<fonte>/
```

## O que é de cada lugar

| Assunto | Onde está a verdade |
|---|---|
| Formato de uma fonte, fatos medidos, regras do parser | `specs/fontes/<fonte>/fonte.md` |
| Tabelas de uma fonte | `specs/fontes/<fonte>/dados.md` (o SQL em `database/postgres/<fonte>/` o implementa) |
| Comportamento do coletor | [../padroes/coletor.md](../padroes/coletor.md) + `specs/fontes/<fonte>/collector.md` |
| Rotas e respostas da API | [../padroes/api.md](../padroes/api.md) + `specs/fontes/<fonte>/api.md` (o manifesto OpenAPI é derivado) |
| Infraestrutura | [../plataforma/](../plataforma/postgres.md) |
