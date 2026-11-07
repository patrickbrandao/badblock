# 014 — "A → 404 → A" reaproveita a linha antiga de A e desmente a promessa de linha do tempo da spec

- **Severidade:** baixa
- **Confiança:** confirmado
- **Categoria:** incoerência
- **Subcategoria:** histórico de respostas / spec × código
- **Status:** aberto — correção não aplicada
- **Arquivos afetados:** `apps/registrobr/rdapnum/collector/internal/store/store.go:288-318`, `specs/fontes/registrobr/rdapnum/dados.md:120-132`
- **Depende de / relacionado a:** nenhum (a rota `/registrobr/rdapnum/history` da api-registrobr-rdapnum só lê esses dados)
- **Esforço estimado:** P (até ~1 h)
- **Auditoria:** 2026-11-07, commit `db43842`

## Explicação para leigos

O coletor guarda o histórico das respostas e só cria um registro novo quando a resposta muda. A spec promete que, se a resposta voltar a uma versão antiga, entra um registro novo, para o histórico ficar na ordem certa.

Isso não acontece quando, entre as duas respostas iguais, o objeto sumiu (resposta 404) ou veio cortado: o coletor reaproveita o registro antigo. O histórico passa a dizer que a versão A esteve presente durante o período em que o objeto não existia.

## Evidência e análise técnica

- `store/store.go:303-307`: o coletor busca a última resposta guardada **com o mesmo resultado**:
  `SELECT uuid::text, sha256 FROM registrobr_rdapnum_response WHERE target_uuid = $1::uuid AND result = $2 ORDER BY fetched_at DESC, uuid DESC LIMIT 1 FOR UPDATE`.
- `store/store.go:311-317`: se o hash é igual, ele só faz `UPDATE ... SET last_seen_at = NOW(), seen_count = seen_count + 1` e devolve o `uuid` antigo, que passa a ser a `response_uuid` do alvo.
- `dados.md:126-128` (item 3) diz: "Compara com a **última linha do mesmo `result`** do alvo". O código cumpre exatamente essa regra.
- `dados.md:130-132` diz: "O retorno a uma versão antiga (A → B → A) cria linha nova: a linha do tempo fica certa". Essa promessa falha quando B é um 404. Exemplo:
  1. t0: resposta A, gravada como linha A.
  2. t1: resposta 404, gravada como linha 404.
  3. t2: resposta A de novo. Como a busca só olha linhas com o mesmo resultado, acha a linha A e só atualiza o `last_seen_at` dela para t2.

  No fim existem só duas linhas: A, com `fetched_at` em t0 e `last_seen_at` em t2, e o 404, em t1. O intervalo da linha A cobre o momento do 404.

## Verificação (tentativa de refutação)

1. A evidência confere.
2. Não há perda de dado processado: o processamento roda de novo depois da resposta conclusiva (`scan.go:228-230`).
3. O caminho é alcançável: todo `SaveAttempt` que não termina em erro passa por ele.
4. As pré-condições são realistas: basta um objeto sumir temporariamente do registro.br.
5. A spec é ambígua: o item 3 e a frase de `dados.md:130-132` se contradizem.

## Impacto

O problema atinge só o histórico:
- o `/history` e o `/response/{uuid}` mostram para A um período que engloba o 404;
- o `fetched_at` da resposta atual fica mais antigo do que a volta do objeto.

## Solução proposta

Decidir na spec (dono: `collector-registrobr-rdapnum`). Há duas opções:

- **Opção 1:** manter o comportamento atual e corrigir a frase de `dados.md:130-132` para "A → B → A com B do mesmo `result`".
- **Opção 2 (recomendada):** uma resposta conclusiva mais recente com outro resultado (completa ↔ 404) quebra a igualdade e força linha nova. As respostas truncadas continuam sem quebrar, o que mantém a proteção contra a alternância entre completa e truncada.

## Diff/patch proposto — NÃO APLICADO

Diff da opção 2:

```diff
--- a/apps/registrobr/rdapnum/collector/internal/store/store.go
+++ b/apps/registrobr/rdapnum/collector/internal/store/store.go
@@ -302,9 +302,14 @@ func saveResponse(ctx context.Context, tx pgx.Tx, worker string, c *Claim, r rdap.Response) (string, bool, error) {
 	var id, last string
 	err = tx.QueryRow(ctx, `
-		SELECT uuid::text, sha256 FROM registrobr_rdapnum_response
-		 WHERE target_uuid = $1::uuid AND result = $2
+		SELECT r.uuid::text, r.sha256 FROM registrobr_rdapnum_response r
+		 WHERE r.target_uuid = $1::uuid AND r.result = $2
+		   -- uma conclusiva de outro result depois desta: a versão voltou, linha nova
+		   AND NOT EXISTS (SELECT 1 FROM registrobr_rdapnum_response o
+		                    WHERE o.target_uuid = r.target_uuid AND o.result IN (1, 3)
+		                      AND o.result <> r.result AND o.last_seen_at > r.last_seen_at)
-		 ORDER BY fetched_at DESC, uuid DESC LIMIT 1
-		 FOR UPDATE`, c.UUID, int16(r.Result)).Scan(&id, &last)
+		 ORDER BY r.fetched_at DESC, r.uuid DESC LIMIT 1
+		 FOR UPDATE OF r`, c.UUID, int16(r.Result)).Scan(&id, &last)
```

## Critérios de aceite

- [ ] `dados.md#resposta-igual` diz explicitamente o que acontece em A → 404 → A.
- [ ] Na opção 2, A → 404 → A gera três linhas, e A → truncada → A continua com uma linha A (`seen_count` 2) e uma truncada.

## Testes recomendados

- **Integração:** a sequência completa(A), 404, completa(A). Contar as linhas por resultado e conferir que `response_uuid` aponta para a linha nova.
- **Regressão:** a sequência completa(A), truncada, completa(A) não cria uma segunda linha A.

## Limitações e suposições

Se o comportamento atual for o desejado, a correção pode ser só um ajuste no texto da spec.
