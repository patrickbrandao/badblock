# 020 — Corpo que o jsonb recusa prende o alvo em nova consulta a cada lease, sem espera nem last_error

- **Severidade:** baixa
- **Confiança:** provável
- **Categoria:** bug
- **Subcategoria:** tratamento de erro / fila
- **Status:** aberto — correção não aplicada
- **Arquivos afetados:** `apps/registrobr/rdapnum/collector/internal/rdap/rdap.go:171-193`, `apps/registrobr/rdapnum/collector/internal/store/store.go:237-250`, `apps/registrobr/rdapnum/collector/internal/store/store.go:319-337`, `apps/registrobr/rdapnum/collector/internal/scan/scan.go:171-173`, `apps/registrobr/rdapnum/collector/internal/scan/scan.go:212-221`, `specs/fontes/registrobr/rdapnum/collector.md:114-134`
- **Depende de / relacionado a:** nenhum
- **Esforço estimado:** P (até ~1 h)
- **Auditoria:** 2026-11-07, commit `db43842`

## Explicação para leigos

O registro.br pode devolver um texto com um caractere que o banco não aceita, como o caractere nulo ou bytes que não formam UTF-8. Nesse caso, o coletor não consegue gravar a resposta.

O certo seria marcar o alvo com erro e esperar antes de tentar de novo, mas o coletor só desiste. Dois minutos depois o alvo volta sozinho para a frente da fila, e isso se repete para sempre. Cada repetição gasta uma consulta e não deixa rastro no banco.

## Evidência e análise técnica

1. **A classificação aceita o corpo.** `rdap.go:181-189`: `Classify` usa `json.Unmarshal`, que aceita `\u0000` e UTF-8 inválido. A resposta é classificada como `Complete` ou `Truncated`. Em `store/hash.go:16-30`, `Normalize` também decodifica esse corpo sem erro.
2. **O banco recusa o corpo.** `store/store.go:319-334`: o corpo vai como `body = string(r.Body)` para `$5::jsonb`. O Postgres recusa `\u0000` em jsonb e também texto com UTF-8 inválido.
3. **O erro desfaz tudo.** `store/store.go:244-249`: o erro de `saveResponse` acontece antes do `UPDATE` do alvo, e o `defer tx.Rollback` desfaz a transação. Nada de `failures`, `retry_at` ou `last_error` é gravado.
4. **O scan só registra no log.** `scan/scan.go:218-220`: registra `não consegui gravar a resposta` e devolve `false`. Em seguida a instância dorme 10 s (`scan.go:171-173`).
5. **O alvo volta para a frente da fila.** Ele fica preso até o lease vencer (2 min). Depois o `Claim` (`store.go:180-189`) o pega de novo como o primeiro da fila, porque `last_scan_at` não mudou.

## Verificação (tentativa de refutação)

1. **Evidência:** confere. Não é um laço sem espera: há a espera do lease (2 min) e os 10 s da instância.
2. **Proteção em outro lugar:** nenhuma. Quem recusa o corpo é justamente a validação do banco.
3. **Alcance:** toda resposta 200 ou 404 com JSON passa por esse caminho.
4. **Pré-condição:** o registro.br publicar `\u0000` ou bytes inválidos num texto. É improvável, mas esses textos vêm dos titulares, fora do controle do coletor.
5. **Documentação:** a spec (`collector.md:121`) lista "JSON inválido" como `error`, mas não trata do JSON que o Go aceita e o jsonb recusa.

## Impacto

Para cada alvo afetado:
- uma consulta perdida a cada 2 min, sem backoff;
- nenhum `last_error`, e nada contado no `/meta`;
- o alvo nunca é concluído, e o dado processado anterior fica para sempre.

O único sinal é o log de erro.

## Solução proposta

Classificar esse corpo como `error` antes de gravar: com UTF-8 inválido ou `\u0000`, a resposta vira `Result = Error`, e o caminho normal de `failures`, `retry_at` e `last_error` cuida do resto. Numa resposta 404, basta descartar o corpo.

Antes do código, a spec (dono: `collector-registrobr-rdapnum`): em `collector.md#coleta-de-um-alvo`, acrescentar à linha `error` o caso "corpo com UTF-8 inválido ou `\u0000`".

## Diff/patch proposto — NÃO APLICADO

```diff
--- a/apps/registrobr/rdapnum/collector/internal/rdap/rdap.go
+++ b/apps/registrobr/rdapnum/collector/internal/rdap/rdap.go
@@ -20,6 +20,7 @@ import (
 	"net/url"
 	"regexp"
 	"strings"
 	"time"
+	"unicode/utf8"
 )
@@ -181,10 +182,15 @@ func (c *Client) Fetch(ctx context.Context, kind, target string) Response {
 	r.Result, r.Err = Classify(resp.StatusCode, body, ExpectedClass(kind))
 	switch r.Result {
 	case Complete, Truncated:
+		if !Storable(body) {
+			r.Result, r.Err = Error, errors.New("corpo com UTF-8 inválido ou \\u0000 (o jsonb não aceita)")
+			break
+		}
 		r.Body = body
 	case NotFound:
-		if len(bytes.TrimSpace(body)) > 0 && json.Valid(body) {
+		if len(bytes.TrimSpace(body)) > 0 && json.Valid(body) && Storable(body) {
 			r.Body = body
 		}
 	}
@@ -194,6 +200,13 @@ func (c *Client) Fetch(ctx context.Context, kind, target string) Response {
 	return r
 }
 
+// Storable diz se o corpo entra numa coluna jsonb: UTF-8 válido e sem
+// \u0000 (o jsonb do Postgres recusa os dois).
+func Storable(body []byte) bool {
+	return utf8.Valid(body) && !bytes.Contains(body, []byte(`\u0000`))
+}
+
```

## Critérios de aceite

- [ ] Uma resposta 200 com `\u0000` ou UTF-8 inválido vira `error`: `failures + 1`, `retry_at` pela espera, `last_error` preenchido e lease liberado.
- [ ] Um 404 com corpo assim grava a linha `not_found` com `body` NULL.
- [ ] A spec lista o caso.

## Testes recomendados

- Unitário de `Fetch` com um servidor falso que devolve `{"objectClassName":"autnum","name":"a\u0000b"}`, e outro com o byte `0xff` dentro de uma string.
- Integração do store: depois da tentativa, `last_result = 4`, `retry_at > NOW()` e `scan_worker IS NULL`.

## Limitações e suposições

- Não verifiquei se o registro.br já publicou algum desses caracteres.
- `bytes.Contains` também pega o raro `\\u0000` literal (com a barra escapada). O efeito é só um `error` com espera, sem dano.
