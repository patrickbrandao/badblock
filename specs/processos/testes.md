# Testes

## Camadas

| Camada | Comando | Precisa de | O quê |
|---|---|---|---|
| Unitários | `make test` (no app ou na raiz) | Go | parser com recortes reais, HTTP com `httptest`, rotas com store falso, cache, configuração |
| Integração | `make test-int` | Docker | PG18 descartável (testcontainers) com as migrations reais; na raiz, também as migrations (up → rollback → up) |
| Lint | `make lint`, `make vet` | Go (ou Docker para o golangci-lint) | golangci-lint v2.14.0 e `go vet -tags integration` |
| Arquivo real | `make -C apps/<fonte>/<tipo> test-real FILE=...` | Docker, o arquivo do dia | parser, carga e rotas com a fonte inteira, com tempos |
| Migrations | `make -C database/postgres test` | Docker | up → rollback de tudo → up ([../plataforma/postgres.md](../plataforma/postgres.md#migrations)) |
| Specs | `make specs-check` | sh | links, estrutura e mapa das specs ([../README.md](../README.md#verificação)) |
| Segredos | `make gitleaks` | Docker | [../plataforma/seguranca.md](../plataforma/seguranca.md#gitleaks) |
| Stack no ar | `make up` e `make -C apps/<fonte>/api smoke` | Docker | cada API responde pela porta do loopback |

O que cada camada cobre em coletores e APIs: [../padroes/coletor.md](../padroes/coletor.md#testes)
e [../padroes/api.md](../padroes/api.md#testes); os testes específicos de
cada fonte estão em `specs/fontes/<fonte>/collector.md` e `api.md`.

## Antes de um PR

```bash
make test test-int lint vet   # todos os apps (+ migrations no test-int; sites em test e lint)
make specs-check
make gitleaks
```

Com o stack no ar (`make up`): `make -C apps/<fonte>/api smoke` em cada API
que mudou, e `make -C websites/<site> smoke` em cada site que mudou.

## Arquivo real

O arquivo da fonte é baixado com `curl` (a URL está em
`specs/fontes/<fonte>/fonte.md`) e passado em `FILE`:

```bash
curl -o /tmp/delegated https://ftp.lacnic.net/pub/stats/lacnic/delegated-lacnic-extended-latest
make -C apps/lacnic/collector test-real FILE=/tmp/delegated
make -C apps/lacnic/api test-real FILE=/tmp/delegated
```

Na IANA não há `FILE`: `make test-real` baixa os 10 arquivos sozinho, ou
reaproveita um download com `IANA_REAL_DIR=<pasta>`. Hoje o `test-real`
existe nos RIRs, na IANA e no rootzone (coletor e API; no rootzone, sem
`FILE`, baixa o `root.zone` do dia) e nas APIs de roothints e rootanchors
(sem `FILE`, usam a fixture do coletor, e o mesmo teste roda no
`make test-int`). No asnames não há alvo: os testes com o arquivo real rodam
no `make test-int` com `ASNAMES_REAL_FILE=<arquivo>`, e no coletor do roothints
também, com `ROOTHINTS_REAL_FILE`; no do rootanchors o arquivo real inteiro já
é a fixture. No coletor do `anatel/pst`, `make -C apps/anatel/pst/collector test-real FILE=<zip ou csv>`
(variável `ANATEL_PST_REAL_FILE`), e na API dele
`make -C apps/anatel/pst/api test-real FILE=<zip>` (sem `FILE`, baixa o ZIP do dia). O cgibr ainda não tem teste com o arquivo real.

As medições registradas nas specs (tempos, tamanhos, contagens) saem desses
testes, com a data do arquivo.

## Regras

- Testes não dependem de rede externa; a fonte é servida por
  `httptest.Server`.
- Fixtures são recortes reais em `testdata/`; mudou o formato, mudam a
  fixture, os testes e a spec juntos.
- Testes de integração usam a build tag `integration` e aplicam as migrations
  de `database/postgres/central/` e `database/postgres/<fonte>/` (só o
  `migrate:up` de cada arquivo).
- Senhas dos containers de teste são fixas e óbvias — `pg` (usuário
  `postgres`) nos testes de integração dos apps, `testpw` no teste das
  migrations —, liberadas no gitleaks.
