# Traefik local

HTTPS para testar as APIs no ambiente de desenvolvimento, em
`*.badblock.localhost`. Só sobe com o profile `dev` (`COMPOSE_PROFILES=dev` no
`.env`); em produção os apps usam o Traefik que já roda no servidor, com a
mesma convenção: rede externa `traefik`, entrypoint `websecure` e certresolver
`le`.

```bash
make certs    # certificado de *.badblock.localhost (mkcert ou CA local via openssl)
make up       # sobe só o Traefik
```

- `https://api.badblock.localhost` — registry-api
- `https://traefik.badblock.localhost` — dashboard
- HTTP redireciona para HTTPS; portas só no loopback.

Sem o mkcert, `make certs` cria uma CA em `certs/ca.pem` sem instalar nada no
sistema: use `curl --cacert infra/traefik/certs/ca.pem` ou importe a CA no
navegador. Com `brew install mkcert && mkcert -install`, o certificado já sai
confiável.

O entrypoint `websecure` não confia em `X-Forwarded-*` de ninguém: o Traefik é a
borda e grava os valores reais. Se houver CDN ou balanceador na frente, liste
as faixas dele em `forwardedHeaders.trustedIPs` (`traefik.yml`).
