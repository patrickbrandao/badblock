# 021 — Processamento não confere se o objeto devolvido corresponde ao alvo consultado

- **Severidade:** baixa
- **Confiança:** provável
- **Categoria:** ponto fraco
- **Subcategoria:** validação de dados de origem
- **Status:** aberto — correção não aplicada
- **Arquivos afetados:** `apps/registrobr/rdapnum/collector/internal/store/process.go:39-48`, `apps/registrobr/rdapnum/collector/internal/store/process.go:73-77`, `apps/registrobr/rdapnum/collector/internal/store/process.go:166-218`, `apps/registrobr/rdapnum/collector/internal/parse/parse.go:175-200`, `specs/fontes/registrobr/rdapnum/dados-processados.md:28,61-63,138-155`
- **Depende de / relacionado a:** nenhum
- **Esforço estimado:** P (até ~1 h)
- **Auditoria:** 2026-11-07, commit `db43842`

## Explicação para leigos

O coletor pergunta pelo ASN 100, mas a resposta pode vir falando do ASN 200. Hoje ele aceita isso sem reclamar e grava os dados do 200 como se fossem a resposta do 100. Isso pode tirar o "dono" da linha do ASN 200 e, mais tarde, fazer essa linha sumir.

Com blocos IP acontece algo parecido: o coletor não confere se o bloco devolvido contém o bloco que ele pediu.

## Evidência e análise técnica

- `process.go:73-77`: a única conferência é se o tipo do objeto bate com o tipo do alvo (`p.Class != rdap.ExpectedClass(kind)`).
- `parse.go:175-184`: o ASN gravado é o `startAutnum` da resposta. Em `parse.go:185-200`, o bloco vem de `cidr0_cidrs`. Nenhum dos dois é comparado com o alvo consultado.
- `process.go:168-186`: o coletor apaga o `_autnum` do alvo com `asn <> a.ASN` e grava com `INSERT ... ON CONFLICT (asn) DO UPDATE SET target_uuid = EXCLUDED.target_uuid`. Se `a.ASN` for o ASN de outro alvo, a linha desse outro alvo passa a apontar para o alvo errado.
- `process.go:195-214`: a tabela `_network` segue o mesmo padrão, com o conflito por `prefix`.
- `process.go:120-130`: quando um alvo recebe 404, o coletor apaga todas as linhas com o `target_uuid` dele, inclusive a que ele "tomou" de outro alvo.

## Verificação (tentativa de refutação)

1. A evidência confere nas linhas citadas.
2. Para blocos, a spec aceita que o bloco devolvido seja maior que o pedido e que dois alvos compartilhem a mesma linha (`dados-processados.md:61-63`). Isso é intencional e não faz parte do achado. Para ASN não há regra nenhuma. Também não há regra para um bloco devolvido que não contém o pedido.
3. O caminho é alcançável: todo processamento de resposta completa passa por ele.
4. A pré-condição é improvável. O TLS vai de ponta a ponta, então seria preciso um bug ou uma mudança no próprio registro.br.
5. Não está documentado.

## Impacto

Se a fonte devolver um objeto diferente do pedido:
- a API mostra dados de outro ASN sob o alvo errado;
- a linha alterna o `target_uuid` entre dois alvos;
- um 404 posterior de um dos alvos apaga o dado do outro até a próxima volta, que pode levar até 168 h.

## Solução proposta

No `Process`, recusar a resposta com `process_error` (via `parse.ErrRefused`) em dois casos:
- quando o `startAutnum` difere do ASN do alvo;
- quando o bloco devolvido não contém o bloco pedido.

Como em toda recusa, o dado anterior continua valendo.

Antes do código, a spec (dono: `collector-registrobr-rdapnum`): em `collector.md#processamento` e em `dados-processados.md`, acrescentar as duas recusas.

## Diff/patch proposto — NÃO APLICADO

```diff
--- a/apps/registrobr/rdapnum/collector/internal/store/process.go
+++ b/apps/registrobr/rdapnum/collector/internal/store/process.go
@@ -5,6 +5,7 @@ import (
 	"encoding/json"
 	"errors"
 	"fmt"
+	"net/netip"
 	"slices"
 	"strings"
 	"time"
@@ -39,13 +40,15 @@ func (s *Store) Process(ctx context.Context, responseUUID string) (Processed, error) {
 	var target, kind string
 	var current *string
 	var result int16
 	var body []byte
+	var tASN *int64
+	var tPrefix *netip.Prefix
 	err = tx.QueryRow(ctx, `
-		SELECT r.target_uuid::text, r.result, r.body::text, t.response_uuid::text, t.kind
+		SELECT r.target_uuid::text, r.result, r.body::text, t.response_uuid::text, t.kind, t.asn, t.prefix
 		  FROM registrobr_rdapnum_response r
 		  JOIN registrobr_rdapnum_target t ON t.uuid = r.target_uuid
 		 WHERE r.uuid = $1::uuid
-		   FOR UPDATE OF r SKIP LOCKED`, responseUUID).Scan(&target, &result, &body, &current, &kind)
+		   FOR UPDATE OF r SKIP LOCKED`, responseUUID).Scan(&target, &result, &body, &current, &kind, &tASN, &tPrefix)
@@ -73,6 +76,14 @@ func (s *Store) Process(ctx context.Context, responseUUID string) (Processed, error) {
 	case rdap.Complete:
 		p, perr := parse.Parse(body)
 		if perr == nil && p.Class != rdap.ExpectedClass(kind) {
 			perr = fmt.Errorf("%w: objectClassName %q não é de um alvo %s", parse.ErrRefused, p.Class, kind)
 		}
+		if perr == nil && p.Autnum != nil && tASN != nil && p.Autnum.ASN != *tASN {
+			perr = fmt.Errorf("%w: ASN devolvido %d não é o do alvo %d", parse.ErrRefused, p.Autnum.ASN, *tASN)
+		}
+		if perr == nil && p.Network != nil && tPrefix != nil &&
+			(p.Network.Prefix.Bits() > tPrefix.Bits() || !p.Network.Prefix.Contains(tPrefix.Addr())) {
+			perr = fmt.Errorf("%w: bloco devolvido %s não contém o alvo %s", parse.ErrRefused, p.Network.Prefix, *tPrefix)
+		}
 		if perr != nil {
```

## Critérios de aceite

- [ ] Uma resposta `autnum` com `startAutnum` diferente do ASN do alvo é recusada com `process_error`, e o `_autnum` existente não muda.
- [ ] Uma resposta `ip network` cujo bloco não contém o bloco do alvo é recusada. Um bloco maior que contém o alvo continua aceito.
- [ ] As specs listam as duas recusas.

## Testes recomendados

- Integração com respostas falsas:
  - `/autnum/100` devolvendo `startAutnum` 200;
  - `/ip/200.225.48.0/24` devolvendo `10.0.0.0/8`.
- Regressão: a fixture `ip-200.225.48.0-21.json`, pedida como `/24`, continua sendo processada.

## Limitações e suposições

Não há registro de o registro.br já ter devolvido um objeto diferente do pedido. O achado é de defesa contra uma anomalia da fonte.
