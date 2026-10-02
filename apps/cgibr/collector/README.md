# collector-cgibr

Importa o arquivo de ASNs, titulares (CNPJ) e blocos IP brasileiros do NIC.br
(registro.br) para as tabelas `cgibr_*` do PostgreSQL do BadBlock; processo
sem API web, que verifica a fonte a cada hora e só aplica quando o arquivo
muda. Faz par com [`api-cgibr`](../api/) na fonte `cgibr`.

Especificações (fonte da verdade): [`specs/fontes/cgibr/`](../../../specs/fontes/cgibr/README.md) e o padrão comum em [`specs/padroes/coletor.md`](../../../specs/padroes/coletor.md).

```bash
make test          # unitários
make test-int      # integração (PG18 via testcontainers, migrations reais)
make lint          # golangci-lint
make up            # sobe só este serviço, com o .env da raiz
make once          # verificação agora no container no ar
```
