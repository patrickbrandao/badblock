# Valkey

Cache cache-aside das APIs do BadBlock (Valkey 9) e fila de tempo real do
`rdns` (`api-rdns` → `collector-rdns`), sem persistência, com
`maxmemory-policy volatile-lru` (a fila, sem TTL, nunca é despejada).

Especificação (fonte da verdade): [`specs/plataforma/valkey.md`](../../specs/plataforma/valkey.md).

```bash
docker exec -it badblock-valkey valkey-cli    # inspecionar
```
