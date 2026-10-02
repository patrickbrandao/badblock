# collector-asnames

Importa o arquivo [`asn.txt`](https://ftp.ripe.net/ripe/asnames/asn.txt) do RIPE NCC (nome e país de todos os ASNs alocados, de todos os RIRs) para as tabelas `asnames_*` do PostgreSQL; processo sem API web, que verifica a fonte a cada hora e só aplica quando o arquivo muda. Faz par com [`api-asnames`](../api/) na fonte `asnames`.

Especificações (fonte da verdade): [`specs/fontes/asnames/`](../../../specs/fontes/asnames/README.md) e o padrão comum em [`specs/padroes/coletor.md`](../../../specs/padroes/coletor.md).

```bash
make test                                        # unitários
make test-int                                    # integração (PG18 via testcontainers, migrations reais)
make lint                                        # golangci-lint
make up                                          # sobe só este serviço, com o .env da raiz
make once                                        # verificação agora no container no ar
ASNAMES_REAL_FILE=/caminho/asn.txt make test-int # com o arquivo real inteiro
```
