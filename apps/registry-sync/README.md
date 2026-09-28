# registry-sync

Importa e sincroniza os dados públicos de ASNs e prefixos IP com o PostgreSQL
do BadBlock: delegações dos cinco RIRs, registros da IANA, bootstrap RDAP,
NIC.br e nomes de AS do RIPE (17 fontes, ver [docs/fontes.md](../../docs/fontes.md)).

Como serviço, verifica cada fonte no seu intervalo com GET condicional, valida
o conteúdo, aplica o que mudou em `ingest` e reconstrói as tabelas centrais de
`registry`, com histórico de mudanças. Detalhes em
[docs/arquitetura.md](../../docs/arquitetura.md).

## Uso

```bash
registry-sync                                   # serviço (padrão no container)
registry-sync --once                            # um ciclo com todas as fontes e sai
registry-sync --once --source=rir-lacnic,nicbr  # só algumas fontes
registry-sync --once --source=rir-arin --force  # ignora a trava de remoção
registry-sync --rebuild                         # só reconstrói o central
registry-sync healthcheck                       # usado pelo HEALTHCHECK do Docker
registry-sync --help
```

No container: `docker exec badblock-registry-sync /registry-sync --once`
(ou `make once`).

## Configuração

Cada opção vale na ordem padrão → variável de ambiente → argumento.

| Variável | Padrão | Descrição |
|---|---|---|
| `POSTGRES_URL` | — | URL com o role `badblock_sync` (obrigatória) |
| `HTTP_PORT` | `8002` | /health, /status, /ping |
| `SYNC_INTERVAL` | `1h` | Intervalo padrão entre verificações de cada fonte |
| `SYNC_SOURCE_INTERVALS` | — | Intervalos por fonte: `iana-ipv4=6h,asnames=2h` |
| `SYNC_SOURCES` | todas | Subconjunto de fontes (ids separados por vírgula) |
| `REMOVAL_THRESHOLD` | `0.05` | Fração máxima que uma carga pode remover de uma fonte |
| `REMOVAL_GRACE` | `26h` | Carência até um recurso ausente ser marcado como removido |
| `DATA_DIR` | `/data` | Onde ficam os arquivos brutos aplicados (`raw/<fonte>/`) |
| `RAW_KEEP` | `7` | Arquivos brutos guardados por fonte |
| `SOURCES_DIR` | — | Lê `<dir>/<id>` em vez da rede (testes e e2e) |
| `HTTP_TIMEOUT` | `5m` | Tempo máximo de cada download |
| `RUN_RETENTION` | `4320h` | Retenção de `ingest.source_run` |
| `LOG_LEVEL` / `LOG_FORMAT` | `info` / `json` | Logs no stdout |

## Desenvolvimento

```bash
make test        # unitários: parsers, fetcher, catálogo, configuração
make test-int    # integração: PG18 real (testcontainers) com bootstrap e migrations
make lint        # golangci-lint
make build       # bin/registry-sync
make up          # sobe só este serviço (o banco precisa estar no ar)
```

Os testes usam fixtures reais recortadas das fontes, em `testdata/sources/`
(ver [testdata/README.md](testdata/README.md)). Os de integração cobrem o ciclo
completo: baseline, fonte sem mudança, atualização, remoção com carência,
restauração, trava de remoção, `--force`, transferência entre RIRs, arquivo
antigo recusado e permissões do role da API.

## Release e deploy

```bash
make release V=1.2.0   # tag registry-sync/v1.2.0 → imagem tmsoftbrasil/badblock-registry-sync:1.2.0
make deploy            # pull + up deste serviço no servidor (deploy.env na raiz)
```
