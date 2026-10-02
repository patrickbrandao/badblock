# collector-roothints

Importa o arquivo [`named.root`](https://www.internic.net/domain/named.root) da InterNIC (root hints: nomes e endereços IPv4/IPv6 dos 13 servidores raiz do DNS) para as tabelas `roothints_*` do PostgreSQL; processo sem API web, que verifica a fonte a cada hora (pelo `named.root.md5` publicado) e só aplica quando o arquivo muda. Faz par com [`api-roothints`](../api/) na fonte `roothints`.

Especificações (fonte da verdade): [`specs/fontes/roothints/`](../../../specs/fontes/roothints/README.md) e o padrão comum em [`specs/padroes/coletor.md`](../../../specs/padroes/coletor.md).

```bash
make test                                                 # unitários
make test-int                                             # integração (PG18 via testcontainers, migrations reais)
make lint                                                 # golangci-lint
make up                                                   # sobe só este serviço, com o .env da raiz
make once                                                 # verificação agora no container no ar
ROOTHINTS_REAL_FILE=/caminho/named.root make test-int     # com o arquivo do dia
```
