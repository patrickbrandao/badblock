# Valkey

Cache cache-aside da registry-api (Valkey 9, protocolo Redis). Não persiste
nada (`save ""`, sem AOF): se reiniciar, o cache recomeça vazio e a API volta a
buscar no Postgres. `maxmemory` (padrão 256 MB, `VALKEY_MAXMEMORY`) com
`allkeys-lru`; senha em `VALKEY_PASSWORD`.

Não publica porta no host. Para inspecionar:

```bash
docker exec -it badblock-valkey valkey-cli    # autentica via VALKEYCLI_AUTH
> INFO keyspace
> SCAN 0 MATCH bb:v1:* COUNT 20
```

As chaves levam a versão do dataset (`bb:v1:<versão>:...`); versões antigas
expiram sozinhas pelo TTL da API (`REDIS_KEY_TTL`).
