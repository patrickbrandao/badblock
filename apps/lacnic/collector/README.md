# collector-lacnic

Importa o arquivo delegated-extended da LACNIC (delegações de ASNs e blocos
IPv4/IPv6, com país, data, status e titular) para as tabelas `lacnic_*` do
PostgreSQL: sem API web, verifica a fonte a cada hora e só aplica quando o
arquivo muda. Faz par com [`api-lacnic`](../api/) na fonte `lacnic`, e é o
modelo dos coletores dos outros RIRs.

Especificações (fonte da verdade): [`specs/fontes/lacnic/`](../../../specs/fontes/lacnic/README.md),
o modelo dos RIRs em [`specs/fontes/rir/`](../../../specs/fontes/rir/README.md)
e o padrão comum em [`specs/padroes/coletor.md`](../../../specs/padroes/coletor.md).

```bash
make test                            # unitários
make test-int                        # integração (PG18 via testcontainers, migrations reais)
make test-real FILE=/tmp/delegated   # arquivo real inteiro (baixe com curl)
make lint                            # golangci-lint
make up                              # sobe só este serviço, com o .env da raiz
make once                            # verificação agora, no container no ar
```
