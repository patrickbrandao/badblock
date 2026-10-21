# Valkey

Cache **cache-aside** das APIs (Valkey 9, protocolo Redis) e **fila de tempo
real** do `rdns` (entre a `api-rdns` e o `collector-rdns`). É opcional: sem
ele, cada API responde direto do Postgres, e o `rdns` só não atende
`live=true`. Como as APIs usam o cache está em
[../padroes/api.md](../padroes/api.md#cache-valkey-cache-aside-e-servecached);
a fila, em [../fontes/rdns/dados.md](../fontes/rdns/dados.md#fila-de-tempo-real-valkey).

## Servidor (`database/valkey/`)

Projeto compose `badblock-valkey`; sobe sozinho a partir da pasta (com o
`.env` dela) ou pelo compose da raiz.

| Item | Valor |
|---|---|
| Serviço / container | `valkey` / `badblock-valkey` (alias `badblock-valkey` na rede `badblock`) |
| Imagem | `valkey/valkey:9-alpine` |
| Comando | `sh -c` com `exec valkey-server` e toda a configuração em argumentos (abaixo); no compose, com `$$` para o shell do container expandir |
| Ambiente | `VALKEY_PASSWORD` (vazia = sem senha), `VALKEY_MAXMEMORY` (`256mb`), `VALKEYCLI_AUTH` = a senha |
| Porta | **nenhuma** publicada no host: só a rede `badblock` o alcança |
| Healthcheck | `valkey-cli ping \| grep -q PONG`, desfazendo antes o `VALKEYCLI_AUTH` vazio (que faria um AUTH sem senha); a cada 5 s, prazo de 3 s, 20 tentativas |
| Outros | `restart: unless-stopped`, `traefik.enable=false`, log json-file 10m × 3 |

Configuração toda na linha de comando, **sem arquivo montado** (o container
sobe em qualquer servidor só com a imagem; decisão #36 em
[../projeto/decisoes.md](../projeto/decisoes.md)). O `docker-compose.yml` e o
`run-server.sh` passam os mesmos argumentos:

```
exec valkey-server \
  --bind 0.0.0.0 '-::*' \
  --port 6379 \
  --save '' \
  --appendonly no \
  --maxmemory-policy volatile-lru \
  --lazyfree-lazy-eviction yes \
  --maxmemory "$VALKEY_MAXMEMORY" \
  ${VALKEY_PASSWORD:+--requirepass "$VALKEY_PASSWORD"}
```

Nada é persistido: se o container reiniciar, o cache recomeça vazio, as
APIs voltam a buscar no Postgres e os pedidos de tempo real na fila se
perdem (quem esperava recebe o dado frio). Com a memória cheia, saem primeiro
as chaves **com TTL** menos usadas — todo o cache das APIs e as listas de
retorno do `rdns` têm TTL. Chaves sem TTL (a fila do `rdns`) nunca são
despejadas: por isso `volatile-lru` e não `allkeys-lru` (decisão #30 em
[../projeto/decisoes.md](../projeto/decisoes.md)). Se só restarem chaves sem
TTL, as escritas falham (`OOM`) — a fila tem no máximo `LIVE_QUEUE_MAX`
(1000) pedidos de algumas dezenas de bytes, longe disso.

`database/valkey/.env.example`: `VALKEY_PASSWORD=`, `VALKEY_MAXMEMORY=256mb`.

## Senha

Opcional. Vazia, o Valkey sobe sem senha — aceitável porque não publica porta
e só a rede `badblock` o alcança. Com senha: só letras e números (entra na
`REDIS_URL`: `redis://:<senha>@badblock-valkey:6379/0`).

## Chaves

Cada API usa o prefixo `badblock:api-<fonte>:` e põe a versão do dataset na
chave (`badblock:api-<fonte>:<versão>:<consulta>`), então dado novo nunca
serve resposta velha e nada precisa ser apagado: as chaves antigas expiram
pelo TTL. Toda chave de cache é gravada com TTL (`REDIS_KEY_TTL`).

Exceções do `rdns`:

- a `api-rdns` não tem versão de dataset: as chaves dela são
  `badblock:api-rdns:<consulta>` e podem ficar até o TTL atrás do banco
  ([../fontes/rdns/api.md](../fontes/rdns/api.md#desvios-do-padrão));
- a fila de tempo real, comum aos dois apps da fonte, usa o prefixo
  `badblock:rdns:live:` (`queue` sem TTL; `inflight:<ip>` e `reply:<ip>` com
  30 s).

```bash
docker exec -it badblock-valkey valkey-cli          # inspecionar
```
