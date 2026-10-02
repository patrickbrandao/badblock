# Segurança e segredos

## O repositório é público

- **Nenhuma** senha, token ou chave entra no git. Valores reais só no `.env`
  (fora do git) e no `.mcp.json` (token do servidor de skills, fora do git).
- `.env.example` (raiz e cada pasta) documenta as variáveis **sem valores**.
- O `.gitignore` exclui `.env`, `.env.*` (menos `.env.example`), `.mcp.json`,
  `old/`, `tmp/` e os scripts do mantenedor.
- Dados de produção (endereço do servidor, acessos, portas administrativas)
  não entram nas specs.
- Senhas só com **letras e números**: entram em URLs de conexão
  (`POSTGRES_URL`, `REDIS_URL`) sem codificação.
- Logs nunca mostram senha: a URL de conexão sai redigida (`Redact`).

## Superfície

- Postgres: porta só no loopback do host; Valkey: nenhuma porta publicada.
- Coletores: sem porta. APIs: só leitura, publicadas pelo Traefik com TLS e
  rate limit por IP; a porta local fica no loopback.
- Sites: só arquivos estáticos, publicados pelo Traefik com TLS; lighttpd
  não-root, sem acesso ao banco nem ao cache.
- Imagens distroless, não-root, sem shell (os sites: lighttpd no Alpine, não-root).
- A API aceita cabeçalhos de IP do cliente só de `TRUSTED_PROXIES`.

## gitleaks

```bash
make gitleaks    # docker run zricethezav/gitleaks:v8.30.1 dir /repo --config /repo/.gitleaks.toml --redact
```

`.gitleaks.toml` estende as regras padrão (`useDefault = true`) e libera só o
que é dado público ou falso:

| Liberado | Por quê |
|---|---|
| `apps/<fonte>/<tipo>/testdata/` | recortes dos arquivos públicos das fontes |
| `apps/<fonte>/<tipo>/vendor/` | código de terceiros (dependências Go vendorizadas), com chaves e tokens de exemplo dos próprios testes e docs |
| `old/`, `tmp/`, `.mcp.json`, `.env` | fora do git (o scan `dir` também os vê) |
| `websites/<site>/{node_modules,dist}/` | dependências (código de terceiros) e build dos sites, fora do git |
| as regexes `(collector\|api\|owner\|test)pw` e `POSTGRES_PASSWORD=pg\b` | senhas fixas e óbvias dos containers descartáveis dos testes |

Faz parte do que se roda antes de um PR ([../processos/testes.md](../processos/testes.md#antes-de-um-pr)).
