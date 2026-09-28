# Operação

## Ambiente local

```bash
make up            # .env, redes, certificado, imagens e stack
make ps            # estado dos containers
make logs          # logs de tudo
make sync-once     # força um ciclo do registry-sync agora
make e2e           # teste ponta a ponta
make down          # para (os volumes ficam)
make clean-data    # APAGA banco, backups, cache e arquivos brutos
```

- API: <https://api.badblock.localhost> (docs em `/docs`); dashboard do
  Traefik: <https://traefik.badblock.localhost>.
- Postgres no host: `127.0.0.1:5433` (a 5432 do Mac é de outro projeto).
  `make -C database/postgresql psql` abre o psql como superusuário.
- Certificado: `make certs` usa o mkcert se ele estiver instalado; senão cria
  uma CA local em `infra/traefik/certs/ca.pem` (nada é instalado no sistema;
  use `curl --cacert infra/traefik/certs/ca.pem`). Navegadores e `curl`
  resolvem `*.localhost` sozinhos; para outras ferramentas, acrescente
  `127.0.0.1 api.badblock.localhost` ao `/etc/hosts`.

## Primeiro deploy em produção

Pré-requisitos no servidor: Docker com Compose v2, um Traefik rodando na rede
externa `traefik` com entrypoint `websecure` e certresolver `le`.

1. **Secrets do GitHub** (Settings → Secrets → Actions): `DOCKERHUB_USERNAME`
   e `DOCKERHUB_TOKEN` (token com permissão de push em `tmsoftbrasil`).
2. **Publicar as imagens**:
   ```bash
   make -C apps/registry-sync release V=1.0.0
   make -C apps/registry-api release V=1.0.0
   ```
   Sem os secrets (ou com o Actions fora do ar), crie as tags só localmente
   (`git tag -a registry-api/v1.0.0 -m "registry-api v1.0.0"`) e publique à
   mão com `./release-images.sh`: amd64 + arm64, tag de versão e `latest`,
   com uma confirmação antes de enviar.
3. **deploy.env** na raiz do repositório (fora do git):
   ```
   DEPLOY_HOST=servidor.exemplo.com.br
   DEPLOY_PORT=22
   DEPLOY_USER=root
   DEPLOY_PATH=/srv/badblock
   ```
4. **.env do servidor** em `$DEPLOY_PATH/.env`: copie o `.env.example`, gere
   senhas novas (só letras e números: `openssl rand -hex 24`) e ajuste:
   ```
   COMPOSE_PROFILES=
   API_FQDN=<domínio público da API>
   TRAEFIK_CERTRESOLVER=le
   REGISTRY_SYNC_TAG=1.0.0
   REGISTRY_API_TAG=1.0.0
   ```
5. `make deploy` envia os arquivos do compose (sem código e sem `.env`), cria a
   rede `badblock` se faltar e sobe o stack. A primeira sincronização leva ~2
   minutos; até lá a API responde 503 `dataset_not_ready`.

Atualizar um app depois de uma release: ajuste a tag no `.env` do servidor e
rode `make -C apps/registry-api deploy`.

## Corte do fluxo n8n antigo

1. Suba o BadBlock e confira `GET /v1/meta/sources` (17 fontes com
   `last_success_at`).
2. Desligue o workflow `ASN-BR-Sync-ALL` no n8n.
3. Aponte os consumidores do POC para `GET https://<API_FQDN>/asn/<asn>`: o
   corpo é idêntico ao do webhook do n8n. Diferença esperada:
   os 21 ASNs estrangeiros listados pelo NIC.br (ex.: AS8075) passam a mostrar o
   país e o nome do próprio registro (US/ARIN), e não os do titular brasileiro.

## Backup e restauração

- Diário às `BACKUP_TIME` (UTC), `pg_dump -Fc` no volume `badblock-backups`,
  com 7 cópias diárias e 4 semanais.
- `make backup` faz um agora; `make verify-backup` restaura o mais recente
  num banco descartável e confere as contagens (o CI faz o mesmo).
- Restaurar em produção (sobrescreve o banco `badblock`):
  ```bash
  make -C database/postgresql backups
  make -C database/postgresql restore FILE=/backups/daily/badblock-AAAAMMDDTHHMMSSZ.dump
  ```
- Só o histórico (`registry.change_log`) e o `first_seen` não se reconstroem
  das fontes; sem backup, um banco novo recomeça o histórico do zero.

## Trocar senhas

Edite o `.env`, rode `make -C database/postgresql bootstrap` (reaplica as
senhas dos roles) e recrie os apps (`docker compose up -d`). A senha do
Valkey vale ao recriar o container dele.

## Situações comuns

| Sintoma | Causa e ação |
|---|---|
| API responde 503 `dataset_not_ready` | Primeira sincronização em andamento; `docker logs badblock-registry-sync` |
| `/status` com `degraded` | Valkey fora; a API segue pelo Postgres. `docker compose up -d valkey` |
| Fonte com `last_error` "trava de remoção" | Uma carga removeria mais de 5% da fonte. Confira o arquivo; se for legítimo: `docker exec badblock-registry-sync /registry-sync --once --source=<id> --force` |
| Fonte com `stale` | O espelho está atrasado em relação ao arquivo já aplicado; a próxima checagem na URL primária resolve |
| `md5 divergente` | Corrida entre a publicação do arquivo e do .md5; resolve na checagem seguinte |
| Mudança de schema | `make -C database/postgresql new NAME=...`, edite, `make -C database/postgresql test` e `make migrate` |
