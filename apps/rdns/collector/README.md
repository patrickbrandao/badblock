# collector-rdns

Varre o DNS reverso (PTR) de todo IPv4 dos prefixos cadastrados em
`rdns_prefix` — os blocos IPv4 do `cgibr`, quebrados em /24, e os estáticos do
administrador — e grava o resultado nas tabelas `rdns_*` do PostgreSQL do
BadBlock. É uma sonda ativa: não baixa arquivo, pergunta aos resolvedores de
`DNS_SERVERS`, sem parar, em várias instâncias que dividem a fila. Atende
também o tempo real da [`api-rdns`](../api/) pelo Valkey. Não tem API web.

Especificações (fonte da verdade): [`specs/fontes/rdns/`](../../../specs/fontes/rdns/README.md), o padrão de sonda em [`specs/padroes/sonda.md`](../../../specs/padroes/sonda.md) e o padrão comum em [`specs/padroes/coletor.md`](../../../specs/padroes/coletor.md).

```bash
make test          # unitários (servidor DNS falso com testdata/zone.txt)
make test-int      # integração (PG18 e Valkey via testcontainers, migrations reais)
make lint          # golangci-lint
make up            # sobe só este serviço, com o .env da raiz
make once          # uma derivação agora, na 1ª réplica no ar
make test-real DNS_SERVERS=1.1.1.1 PREFIX=200.160.2.0/24   # um prefixo de verdade, a 64 consultas/s
```
