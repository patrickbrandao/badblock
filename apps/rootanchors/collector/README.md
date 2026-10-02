# collector-rootanchors

Importa o `root-anchors.xml` da IANA — as âncoras de confiança DNSSEC da zona
raiz (KSKs, com key tag, algoritmo, digest, chave pública e validade) — para
as tabelas `rootanchors_*` do PostgreSQL do BadBlock; processo sem API web,
que verifica a fonte a cada 6 horas, confere o hash publicado e recalcula o
key tag e o digest de cada chave antes de aplicar. Faz par com
[`api-rootanchors`](../api/) na fonte `rootanchors`.

Especificações (fonte da verdade): [`specs/fontes/rootanchors/`](../../../specs/fontes/rootanchors/README.md) e o padrão comum em [`specs/padroes/coletor.md`](../../../specs/padroes/coletor.md).

```bash
make test          # unitários
make test-int      # integração (PG18 via testcontainers, migrations reais)
make lint          # golangci-lint
make up            # sobe só este serviço, com o .env da raiz
make once          # verificação agora no container no ar
```
