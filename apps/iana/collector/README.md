# collector-iana

Importa os 10 registros de numeração da IANA (blocos de ASN e de IP por RIR,
blocos e ASNs de uso especial, bootstrap RDAP), tratados como um dataset,
para as tabelas `iana_*` do PostgreSQL; verifica a cada 6 horas e só aplica
quando algum arquivo muda. Faz par com [`api-iana`](../api/) na fonte `iana`.

Especificações (fonte da verdade): [`specs/fontes/iana/`](../../../specs/fontes/iana/README.md) e o padrão comum em [`specs/padroes/coletor.md`](../../../specs/padroes/coletor.md).

```bash
make test          # unitários
make test-int      # integração (PG18 via testcontainers, migrations reais)
make test-real     # parser + Postgres com os 10 arquivos reais, baixados agora
make lint
make up            # sobe só este serviço, com o .env da raiz
make once          # verificação agora no container no ar
```
