# collector-anatel-pst

Importa o ZIP de prestadoras de serviços de telecomunicações da Anatel
(pessoas jurídicas por CNPJ e os serviços notificados por elas, como SCM,
STFC e SMP) para as tabelas `anatel_pst_*` do PostgreSQL do BadBlock; processo
sem API web, que verifica a fonte a cada 24 h e só aplica quando o arquivo
muda. As linhas de pessoa física (CPF) são ignoradas. Faz par com
[`api-anatel-pst`](../api/) na fonte `anatel/pst`.

Especificações (fonte da verdade): [`specs/fontes/anatel/pst/`](../../../../specs/fontes/anatel/pst/README.md) e o padrão comum em [`specs/padroes/coletor.md`](../../../../specs/padroes/coletor.md).

```bash
make test          # unitários
make test-int      # integração (PG18 via testcontainers, migrations reais)
make test-real FILE=/tmp/pst.zip   # o ZIP real inteiro, com tempos
make lint          # golangci-lint
make up            # sobe só este serviço, com o .env da raiz
make once          # verificação agora no container no ar
```
