# collector-registrobr-rdapnum

Consulta o RDAP do registro.br (`/autnum/<asn>` e `/ip/<bloco>`) de cada ASN
e bloco do `cgibr` — e dos estáticos do administrador — e grava nas tabelas
`registrobr_rdapnum_*` do PostgreSQL do BadBlock: a resposta crua, com
histórico, e o dado processado (titular com CNPJ, contatos, e-mails, blocos,
delegações reversas). É uma sonda ativa: várias instâncias dividem a fila, a
uma taxa por minuto, de preferência por um proxy que roda o IP de saída. A
resposta que o servidor trunca é guardada, mas nunca processada. Atende
também a atualização pedida pela
[`api-registrobr-rdapnum`](../api/) pelo Valkey. Não tem API web.

Especificações (fonte da verdade): [`specs/fontes/registrobr/rdapnum/`](../../../../specs/fontes/registrobr/rdapnum/README.md), o padrão de sonda em [`specs/padroes/sonda.md`](../../../../specs/padroes/sonda.md) e o padrão comum em [`specs/padroes/coletor.md`](../../../../specs/padroes/coletor.md).

```bash
make test          # unitários (servidor RDAP e proxy falsos com testdata/)
make test-int      # integração (PG18 e Valkey via testcontainers, migrations reais)
make lint          # golangci-lint
make up            # sobe só este serviço, com o .env da raiz
make once          # uma derivação agora, na 1ª réplica no ar
make test-real ASN=61610   # um ASN e os blocos dele de verdade, a 8/min
```
