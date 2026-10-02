# collector-rootzone

Importa a zona raiz do DNS ([`root.zone`](https://www.internic.net/domain/root.zone) da InterNIC: SOA e serial, TLDs delegados, servidores de nome, glue A/AAAA, DS, DNSKEY, NSEC e ZONEMD) para as tabelas `rootzone_*` do PostgreSQL; processo sem API web, que verifica a fonte a cada hora e só aplica quando a zona muda e não é mais antiga que a aplicada (serial do SOA). Os RRSIG são contados, não guardados. Faz par com [`api-rootzone`](../api/) na fonte `rootzone`.

Especificações (fonte da verdade): [`specs/fontes/rootzone/`](../../../specs/fontes/rootzone/README.md) e o padrão comum em [`specs/padroes/coletor.md`](../../../specs/padroes/coletor.md).

```bash
make test                            # unitários
make test-int                        # integração (PG18 via testcontainers, migrations reais)
make test-real                       # root.zone inteiro do dia (baixa agora; ou FILE=/caminho/root.zone)
make lint                            # golangci-lint
make up                              # sobe só este serviço, com o .env da raiz
make once                            # verificação agora, no container no ar
```
