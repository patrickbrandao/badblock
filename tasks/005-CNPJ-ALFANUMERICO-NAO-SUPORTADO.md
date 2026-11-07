# 005 — CNPJ alfanumérico não é suportado (anatel/pst, cgibr, registrobr/rdapnum)

- **Severidade:** média
- **Confiança:** provável
- **Categoria:** ponto fraco
- **Subcategoria:** formato de dado externo / validação rígida demais
- **Status:** aberto — correção não aplicada
- **Arquivos afetados:** `apps/anatel/pst/collector/internal/parse/parse.go:156`, `apps/anatel/pst/collector/internal/parse/parse.go:278-297`, `database/postgres/anatel_pst/20260930120000_anatel_pst.sql:138`, `apps/anatel/pst/api/internal/httpapi/handlers_provider.go:28-36`, `apps/anatel/pst/api/internal/httpapi/handlers_provider.go:150-158`, `apps/anatel/pst/api/openapi/openapi.yaml:635,945`, `database/postgres/cgibr/20260928230100_cgibr.sql:95`, `apps/cgibr/api/internal/httpapi/handlers.go:120-127`, `apps/cgibr/api/internal/httpapi/types.go:151-158`, `apps/cgibr/api/openapi/openapi.yaml:1001`, `database/postgres/registrobr_rdapnum/20261021120000_registrobr_rdapnum.sql:344`, `apps/registrobr/rdapnum/api/internal/httpapi/handlers.go:105-121`
- **Depende de / relacionado a:** 009 (a recusa da Anatel por mais de 1% de linhas descartadas entraria nesse laço)
- **Esforço estimado:** M (até 1 dia)
- **Auditoria:** 2026-11-07, commit `db43842`

## Explicação para leigos

A Receita Federal começou a emitir CNPJs com letras para empresas novas, por exemplo `12ABC34501DE35`. Todo o BadBlock supõe que um CNPJ tem só 14 números. Por isso:
- as prestadoras novas da Anatel são descartadas na importação;
- quando elas passarem de 1% do arquivo, o arquivo inteiro deixa de ser aceito e a base da Anatel para de atualizar;
- as buscas por CNPJ nas APIs apagam as letras e não encontram nada.

## Evidência e análise técnica

**anatel/pst (o caso mais grave)**

- Coletor:
  - `reCNPJ = regexp.MustCompile(`^[0-9]{14}$`)` (`parse.go:156`);
  - em `service`, `!reCNPJ.MatchString(doc)` leva a `d.skip(line, "CNPJ inválido: %q", doc)` (`parse.go:293-297`);
  - em `parse.go:278-283`, se `Skipped/RowsCNPJ > MaxSkippedRatio` (1%), `Parse` devolve erro e **o arquivo inteiro é recusado**.
- Banco: `CONSTRAINT chk_anatel_pst_provider_document CHECK (document ~ '^[0-9]{14}$')` (`20260930120000_anatel_pst.sql:138`).
- API:
  - `cnpj := onlyDigits(r.PathValue("cnpj")); if len(cnpj) != 14 { 400 }` (`handlers_provider.go:31-36`);
  - `onlyDigits` guarda só os caracteres de `0` a `9` (`150-158`);
  - OpenAPI: `pattern: '^[0-9]{14}$'` (`openapi.yaml:945`).

**cgibr**

- O coletor guarda `document` como veio. Mas `document_digits ... GENERATED ALWAYS AS (regexp_replace(document, '[^0-9]', '', 'g'))` (`cgibr.sql:95`) apaga as letras.
- API: `if len(digits) != 14 && len(digits) != 8` (`handlers.go:124`), e `documentType` classifica 14 dígitos como cnpj e o resto como "foreign" (`types.go:153-158`).
- Resultado: o CNPJ alfanumérico some da busca. Se tiver exatamente 6 letras, sobram 8 dígitos: ele é rotulado "foreign" e pode colidir com um identificador estrangeiro.

**registrobr/rdapnum**

- `document_digits` usa o mesmo `regexp_replace` (`registrobr_rdapnum.sql:344`).
- `docRE = ^[0-9./ -]+$` (`handlers.go:105`) recusa qualquer letra com 400.

## Verificação (tentativa de refutação)

1. **Evidência:** confere nas linhas citadas.
2. **Proteção:** nenhuma. Nenhuma camada normaliza letras, e não há menção a CNPJ alfanumérico em `specs/` nem em `apps/` (grep "alfanum" sem resultado).
3. **Alcance:** o parser da Anatel roda em toda carga, e as rotas `/anatel/pst/provider/{cnpj}`, `/cgibr/document/{doc}` e `/registrobr/rdapnum/document/...` são públicas.
4. **Pré-condição:** depende de um fato externo que **não pude conferir sem rede**: a IN RFB nº 2.229/2024, que prevê CNPJ alfanumérico para inscrições novas a partir de julho de 2026. Também não conferi se a Anatel e o NIC.br já publicam esses CNPJs, nem em que proporção.
5. **Intenção:** não está documentado. As specs falam em "14 dígitos".

## Impacto

- **anatel/pst:** as prestadoras novas ficam fora da base. Quando passarem de 1% das linhas de CNPJ, o arquivo inteiro é recusado e a base congela. A recusa se repete a cada `RETRY_INTERVAL` (relatório 009).
- **cgibr / rdapnum:** titulares novos não são encontrados por documento. No cgibr, ainda há risco de rótulo errado ("foreign").

## Solução proposta

Primeiro as specs:
- `specs/fontes/anatel/pst/{fonte,dados,collector,api}.md`;
- `specs/fontes/cgibr/{dados,api}.md`;
- `specs/fontes/registrobr/rdapnum/{dados,api-rotas}.md`;
- o glossário.

A regra: depois de normalizado (sem pontuação, em maiúsculas), um CNPJ casa com `^[0-9A-Z]{12}[0-9]{2}$`.

Depois o código:
- `collector-anatel-pst`: trocar a regex.
- Migrations novas, sem editar as existentes:
  - trocar o CHECK de `anatel_pst_provider`;
  - nas colunas geradas `document_digits` do cgibr e do rdapnum, usar `upper(regexp_replace(document,'[^0-9A-Za-z]','','g'))`. Isso exige recriar a coluna gerada e o índice.
- `api-anatel-pst`, `api-cgibr` e `api-registrobr-rdapnum`:
  - normalizador alfanumérico;
  - `documentType` decidido pelo formato, não só pelo tamanho;
  - OpenAPI atualizada.
- Opcional: validar o dígito verificador alfanumérico (valor ASCII − 48).

## Diff/patch proposto — NÃO APLICADO

```diff
--- a/apps/anatel/pst/collector/internal/parse/parse.go
+++ b/apps/anatel/pst/collector/internal/parse/parse.go
@@ -155,3 +155,4 @@
 var (
-	reCNPJ    = regexp.MustCompile(`^[0-9]{14}$`)
+	// CNPJ numérico ou alfanumérico (IN RFB 2.229/2024): 12 posições 0-9/A-Z + 2 DV.
+	reCNPJ    = regexp.MustCompile(`^[0-9A-Z]{12}[0-9]{2}$`)
 	reFistel  = regexp.MustCompile(`^[0-9]{11}$`)
--- a/apps/anatel/pst/api/internal/httpapi/handlers_provider.go
+++ b/apps/anatel/pst/api/internal/httpapi/handlers_provider.go
@@ -30,5 +30,5 @@
 func (a *API) handleProvider(w http.ResponseWriter, r *http.Request) {
-	cnpj := onlyDigits(r.PathValue("cnpj"))
-	if len(cnpj) != 14 {
+	cnpj := normalizeCNPJ(r.PathValue("cnpj"))
+	if !reCNPJ.MatchString(cnpj) {
 		writeError(w, http.StatusBadRequest, "bad_request",
@@ -150,9 +150,19 @@
-// onlyDigits guarda só os dígitos ASCII (como /cgibr/document/{doc}).
-func onlyDigits(s string) string {
+var reCNPJ = regexp.MustCompile(`^[0-9A-Z]{12}[0-9]{2}$`)
+
+// normalizeCNPJ guarda só dígitos e letras ASCII, em maiúsculas (CNPJ
+// numérico ou alfanumérico, com ou sem pontuação).
+func normalizeCNPJ(s string) string {
 	var b strings.Builder
 	for _, c := range s {
-		if c >= '0' && c <= '9' {
+		switch {
+		case c >= '0' && c <= '9', c >= 'A' && c <= 'Z':
 			b.WriteRune(c)
+		case c >= 'a' && c <= 'z':
+			b.WriteRune(c - 'a' + 'A')
 		}
 	}
 	return b.String()
 }
--- /dev/null
+++ b/database/postgres/anatel_pst/<timestamp>_anatel_pst_cnpj_alfanumerico.sql
@@ -0,0 +1,8 @@
+-- migrate:up
+ALTER TABLE anatel_pst_provider
+    DROP CONSTRAINT chk_anatel_pst_provider_document,
+    ADD CONSTRAINT chk_anatel_pst_provider_document CHECK (document ~ '^[0-9A-Z]{12}[0-9]{2}$');
+-- migrate:down
+ALTER TABLE anatel_pst_provider
+    DROP CONSTRAINT chk_anatel_pst_provider_document,
+    ADD CONSTRAINT chk_anatel_pst_provider_document CHECK (document ~ '^[0-9]{14}$');
```

cgibr e rdapnum precisam de migrations parecidas para `document_digits` e de mudanças nos handlers, cada uma feita pelo sub-agente dono.

## Critérios de aceite

- [ ] Uma linha da Anatel com CNPJ `12ABC34501DE35` é aceita, gravada e encontrada por `/anatel/pst/provider/12.ABC.345%2F01DE-35`.
- [ ] O CNPJ numérico continua funcionando igual.
- [ ] `/cgibr/document` e `/registrobr/rdapnum/document` acham titulares com CNPJ alfanumérico, e o cgibr não os rotula "foreign".
- [ ] OpenAPI e specs atualizadas.

## Testes recomendados

- Recorte da Anatel com linhas alfanuméricas no parser.
- Teste de integração do CHECK.
- Testes de handler com CNPJ alfanumérico formatado e em minúsculas.
- Teste do cgibr com 6 letras (o caso dos 8 dígitos).

## Limitações e suposições

Sem acesso à rede, não confirmei o texto nem as datas da IN RFB, nem se a Anatel e o NIC.br já publicam CNPJs alfanuméricos, nem com que formatação. A severidade média supõe que a proporção na Anatel chegue a 1% em alguns meses.
