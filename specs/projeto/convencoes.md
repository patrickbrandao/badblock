# Convenções

## Idioma

- Identificadores de código, nomes SQL e campos JSON em **inglês**
  (`snake_case` no JSON e no SQL).
- Comentários, documentação, specs, mensagens de log e de erro, `--help` e
  mensagens de commit em **português (PT-BR), com acentos**.

## Go

- Go **1.27** (`go 1.27.1` no `go.mod`), um módulo por app
  ([estrutura.md](estrutura.md#nomes)).
- Dependências permitidas: biblioteca padrão mais

  | Módulo | Versão | Quem usa |
  |---|---|---|
  | `github.com/jackc/pgx/v5` | v5.11 | todos |
  | `github.com/redis/go-redis/v9` | v9.22 | APIs (Valkey) |
  | `golang.org/x/sync` | v0.23 | APIs (`singleflight`) |
  | `github.com/testcontainers/testcontainers-go` (+ `modules/postgres`) | v0.44 | só testes de integração |

  Qualquer outra precisa de decisão registrada em [decisoes.md](decisoes.md).
- Dependências **vendorizadas**: cada app versiona o seu `vendor/` (gerado por
  `go mod vendor`), e o build da imagem usa só ele, sem rede
  ([../plataforma/docker.md](../plataforma/docker.md#dockerfile-igual-em-todo-app-trocando-app-e-a-descrição)).
  Mudou o `go.mod` (`go get`, versão nova), roda `make vendor` e o `vendor/`
  entra no mesmo commit; fora de sincronia, o `go build` e o `go test` param
  com `inconsistent vendoring`. O `vendor/` não se edita à mão.
- Logs com `log/slog` (JSON no stdout por padrão), atributo `app` em toda
  linha, mensagens curtas em português (`fonte sem mudança`, `ouvindo`).
- Binários estáticos (`CGO_ENABLED=0`, `-trimpath`), versão injetada por
  `-ldflags -X <module>/internal/buildinfo.{Version,Commit,Date}`.
- Erros com contexto (`fmt.Errorf("...: %w", err)`); `errors.Is/As` para
  decidir.

## Configuração

- Toda opção tem **padrão → variável de ambiente → argumento de linha de
  comando**, nessa ordem de precedência (o argumento vence).
- Variável vazia vale o padrão (o compose passa variáveis vazias quando o
  `.env` não as define).
- O argumento é a variável em minúsculas com hífens (`SYNC_INTERVAL` →
  `--sync-interval`); `--help` lista cada opção com a variável e o padrão.
- Opção inválida: mensagem no stderr e saída 2.
- Conexões seguem as skills `postgres-url-standalone` e
  `redis-url-standalone` do MCP `badblock-dev`: uma URL completa por serviço
  (`POSTGRES_URL`, `REDIS_URL`), nunca host/porta/senha soltos. A senha nunca
  aparece em log (`Redact`).
- Toda opção nova aparece no `--help` e na spec da fonte. Quando o deploy
  precisa ajustá-la, entra também no `docker-compose.yml` e no `.env.example`
  do app; no `.env.example` da raiz entram só as que se ajustam no stack
  inteiro (tags; intervalos, mínimos e trava dos coletores; porta e TTL das
  APIs). As demais valem pelo padrão do app.

## Testes

- Unitários sem Docker, com `-race` (`make test`).
- Integração com a build tag `integration` e PostgreSQL 18 descartável via
  testcontainers, aplicando as migrations reais de `database/postgres/`
  (`make test-int`).
- Fixtures são **recortes reais** da fonte em `testdata/`; mudou o formato,
  mudam a fixture, os testes e a spec juntos.
- Fonte real inteira: `make test-real` onde existe (RIRs: `FILE=...`, pela
  variável `<FONTE>_REAL_FILE`; IANA: `IANA_REAL_DIR`); no asnames, sem alvo,
  `ASNAMES_REAL_FILE=<arquivo> make test-int`
  ([../processos/testes.md](../processos/testes.md#arquivo-real)).
- Os testes não dependem de rede externa (a fonte é servida por
  `httptest.Server`).

Camadas e comandos: [../processos/testes.md](../processos/testes.md).

## Lint e formatação

- `gofmt` e golangci-lint **v2.14.0** (local ou pela imagem
  `golangci/golangci-lint:v2.14.0`), com o mesmo `.golangci.yml` em todo app:

  ```yaml
  version: "2"
  run:
    build-tags: [integration]
  linters:
    default: standard
    enable: [bodyclose, errorlint, nilerr, unconvert, copyloopvar, rowserrcheck]
    settings:
      errcheck:
        exclude-functions:
          - (github.com/jackc/pgx/v5.Tx).Rollback      # depois do Commit devolve ErrTxClosed
          - (*github.com/jackc/pgx/v5.Conn).Close
    exclusions:
      presets: [std-error-handling, common-false-positives]
  formatters:
    enable: [gofmt]
  ```

- `.editorconfig` da raiz: UTF-8, LF, linha final, sem espaço no fim da linha
  (exceto `.md`), indentação de 2 espaços; tabulação em `*.go` e `Makefile`.

## Documentação e specs

- Regras de escrita das specs: [../README.md](../README.md#como-escrever-uma-spec).
- O `README.md` de cada app é curto (o que é, comandos, link para a spec); o
  conteúdo técnico fica nas specs.
- Comentários no código explicam o **porquê**; o **o quê** está na spec.

## Git

- Mensagens de commit em português. Commit, push, tag e publicação de imagem
  só com pedido explícito do usuário.
- Tags de release por app: `<app>/vX.Y.Z` (SemVer, sufixo opcional
  `-rc.1`) ([../plataforma/publicacao.md](../plataforma/publicacao.md#release-das-imagens)).
