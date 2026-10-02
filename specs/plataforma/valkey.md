# Valkey

Cache **cache-aside** das APIs (Valkey 9, protocolo Redis). É opcional: sem
ele, cada API responde direto do Postgres. Como as APIs usam o cache está em
[../padroes/api.md](../padroes/api.md#cache-valkey-cache-aside-e-servecached).

## Servidor (`database/valkey/`)

Projeto compose `badblock-valkey`; sobe sozinho a partir da pasta (com o
`.env` dela) ou pelo compose da raiz.

| Item | Valor |
|---|---|
| Serviço / container | `valkey` / `badblock-valkey` (alias `badblock-valkey` na rede `badblock`) |
| Imagem | `valkey/valkey:9-alpine` |
| Comando | `sh -c 'exec valkey-server /usr/local/etc/valkey/valkey.conf --maxmemory "$VALKEY_MAXMEMORY" ${VALKEY_PASSWORD:+--requirepass "$VALKEY_PASSWORD"}'` (no compose, com `$$` para o shell do container expandir) |
| Ambiente | `VALKEY_PASSWORD` (vazia = sem senha), `VALKEY_MAXMEMORY` (`256mb`), `VALKEYCLI_AUTH` = a senha |
| Porta | **nenhuma** publicada no host: só a rede `badblock` o alcança |
| Healthcheck | `valkey-cli ping \| grep -q PONG`, desfazendo antes o `VALKEYCLI_AUTH` vazio (que faria um AUTH sem senha); a cada 5 s, prazo de 3 s, 20 tentativas |
| Outros | `restart: unless-stopped`, `traefik.enable=false`, log json-file 10m × 3 |

`valkey.conf` (montado só leitura):

```
bind 0.0.0.0 -::*
port 6379
save ""
appendonly no
maxmemory-policy allkeys-lru
lazyfree-lazy-eviction yes
```

Nada é persistido: se o container reiniciar, o cache recomeça vazio e as
APIs voltam a buscar no Postgres. Com a memória cheia, as chaves menos usadas
saem primeiro.

`database/valkey/.env.example`: `VALKEY_PASSWORD=`, `VALKEY_MAXMEMORY=256mb`.

## Senha

Opcional. Vazia, o Valkey sobe sem senha — aceitável porque não publica porta
e só a rede `badblock` o alcança. Com senha: só letras e números (entra na
`REDIS_URL`: `redis://:<senha>@badblock-valkey:6379/0`).

## Chaves

Cada API usa o prefixo `badblock:api-<fonte>:` e põe a versão do dataset na
chave (`badblock:api-<fonte>:<versão>:<consulta>`), então dado novo nunca
serve resposta velha e nada precisa ser apagado: as chaves antigas expiram
pelo TTL.

```bash
docker exec -it badblock-valkey valkey-cli          # inspecionar
```
