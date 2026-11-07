# 006 — Redact não esconde a senha do Postgres em DSN keyword/value nem em `?password=` (28 apps)

- **Severidade:** baixa
- **Confiança:** confirmado
- **Categoria:** segurança
- **Subcategoria:** vazamento de segredo em log
- **Status:** aberto — correção não aplicada
- **Arquivos afetados:**
  - `apps/registrobr/rdapnum/collector/internal/config/config.go:243-254`
  - `apps/registrobr/rdapnum/collector/cmd/collector-registrobr-rdapnum/main.go:60-63`
  - a mesma `func Redact`, com corpo idêntico, em:
    - `apps/{afrinic,apnic,arin,cgibr,iana,lacnic,rdns,ripencc,rootanchors,roothints,rootzone}/{collector,api}/internal/config/config.go`
    - `apps/anatel/pst/{collector,api}/internal/config/config.go`
    - `apps/ripe/asnames/{collector,api}/internal/config/config.go`
    - `apps/registrobr/rdapnum/api/internal/config/config.go`
- **Depende de / relacionado a:** `specs/plataforma/seguranca.md` (logs sem senha)
- **Esforço estimado:** M (até 1 dia): a mesma mudança em 28 apps, com testes
- **Auditoria:** 2026-11-07, commit `db43842`

## Explicação para leigos

Ao subir, cada app grava no log o endereço do banco "sem a senha". Mas a função que esconde a senha só funciona no formato `postgres://usuario:senha@host`. O driver aceita outros dois jeitos de escrever a conexão: `host=... password=...` e `?password=` no fim da URL. Se o operador usar um deles, a senha vai inteira para o log.

## Evidência e análise técnica

- `config.go:244-253`: a função procura `@` e `://`. Se faltar um dos dois, devolve `u` intacto. Se achar os dois, troca só o trecho depois do `:` do userinfo.
- `store/store.go:42`: `pgxpool.ParseConfig(url)` aceita URL e keyword/value, inclusive com `password=` na query da URL.
- `config.go:163-164`: a validação só exige que `POSTGRES_URL` não seja vazia.
- `main.go:60-63`: `log.Info("iniciando", ..., "postgres", config.Redact(cfg.PostgresURL))`.
- Casos que vazam:
  - `host=badblock-postgres user=postgres password=[REDACTED] dbname=badblock` (sem `://`);
  - `postgres://badblock-postgres:5432/badblock?user=postgres&password=[REDACTED]` (sem `@`).
- Abrangência: `grep -rln "func Redact"` acha 28 arquivos `internal/config/config.go`, um por app (14 fontes × coletor e API). O corpo da função é idêntico (mesmo md5) nos 28, e as 28 chamadas fora de teste recebem `POSTGRES_URL`.

## Verificação (tentativa de refutação)

1. A evidência confere.
2. Não há outra proteção: nada valida o formato da URL, e o log vai para stdout/`docker logs`.
3. É alcançável: a linha `iniciando` sai de todo app, a cada subida.
4. A pré-condição é o operador usar keyword/value ou `?password=`. O formato documentado (`config.go:60`, `.env.example`, composes) é a URL com userinfo, por isso a severidade é baixa.
5. As specs prometem o contrário:
   - `collector.md:283-286`: "`postgres` (sem a senha)";
   - `padroes/coletor.md:59-60` e `padroes/api.md:258`;
   - `plataforma/seguranca.md:14`: "Logs nunca mostram senha";
   - `projeto/convencoes.md:50`.

## Impacto

A senha do Postgres pode parar nos logs dos containers, que costumam ser coletados e lidos por mais gente do que quem lê o `.env`.

## Solução proposta

Reescrever `Redact`:
- se houver `://`, usar `url.Parse` e mascarar a senha do userinfo e o `password` da query;
- se não houver `://`, mascarar o `password=` do formato keyword/value;
- se `url.Parse` falhar, devolver `[REDACTED]`.

Aplicar nos 28 apps, cada um pelo seu sub-agente. O padrão é da sessão principal, em `specs/padroes/coletor.md` e `specs/padroes/api.md`.

A spec muda antes: `specs/plataforma/seguranca.md` e os dois padrões passam a dizer "sem a senha, em qualquer formato aceito pelo pgx (URL, `?password=`, keyword/value)".

Alternativa: recusar na validação qualquer valor que não seja URL `postgres://`/`postgresql://`, alinhado com a skill `postgres-url-standalone`.

## Diff/patch proposto — NÃO APLICADO

```diff
--- a/apps/registrobr/rdapnum/collector/internal/config/config.go
+++ b/apps/registrobr/rdapnum/collector/internal/config/config.go
@@ -7,6 +7,7 @@ import (
 	"fmt"
 	"io"
 	"net/url"
+	"regexp"
 	"sort"
 	"strconv"
 	"strings"
 	"time"
 )
@@ -242,13 +243,31 @@ func PrintHelp(w io.Writer) {
 
-// Redact esconde a senha de uma URL para o log.
+var dsnPassword = regexp.MustCompile(`(?i)(\bpassword\s*=\s*)('(?:[^'\\]|\\.)*'|\S+)`)
+
+// Redact esconde a senha de uma conexão para o log: a do userinfo e a de
+// ?password= numa URL, e a de password= num DSN keyword/value.
 func Redact(u string) string {
-	at := strings.LastIndex(u, "@")
-	scheme := strings.Index(u, "://")
-	if at < 0 || scheme < 0 || at < scheme {
-		return u
-	}
-	if user, _, ok := strings.Cut(u[scheme+3:at], ":"); ok {
-		return u[:scheme+3] + user + ":***" + u[at:]
-	}
-	return u
+	if !strings.Contains(u, "://") {
+		return dsnPassword.ReplaceAllString(u, "${1}***")
+	}
+	p, err := url.Parse(u)
+	if err != nil {
+		return "[REDACTED]"
+	}
+	if p.User != nil {
+		if _, ok := p.User.Password(); ok {
+			p.User = url.UserPassword(p.User.Username(), "***")
+		}
+	}
+	if q := p.Query(); q.Has("password") {
+		q.Set("password", "***")
+		p.RawQuery = q.Encode()
+	}
+	return p.String()
 }
```

## Critérios de aceite

- [ ] `Redact("host=h user=u password=x dbname=d")` não contém `x`.
- [ ] `Redact("postgres://h/db?user=u&password=x")` não contém `x`.
- [ ] `Redact("postgres://postgres:pw@h:5432/db")` continua dando `postgres://postgres:***@h:5432/db`, e o `TestRedact` atual passa.
- [ ] A mudança vale nos 28 apps, e as specs de segurança e dos padrões citam os três formatos.

## Testes recomendados

Ampliar `TestRedact` em cada `internal/config/config_test.go` com estes casos:
- keyword/value, com e sem aspas;
- `?password=`;
- uma URL inválida.

## Limitações e suposições

- `url.String()` pode reordenar a query, porque `q.Encode()` ordena as chaves. Isso só muda a aparência da linha de log.
- Se o projeto preferir recusar DSN fora do formato URL, a correção vai para a validação em vez de `Redact`.
