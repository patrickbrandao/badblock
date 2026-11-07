# 013 — SaveLive grava em rdns_ptr sem reconferir rdns_prefix e deixa linhas órfãs

- **Severidade:** baixa
- **Confiança:** confirmado
- **Categoria:** bug
- **Subcategoria:** condição de corrida / integridade referencial
- **Status:** aberto — correção não aplicada
- **Arquivos afetados:** `apps/rdns/collector/internal/live/live.go:170-195`, `apps/rdns/collector/internal/store/store.go:370-402`, `database/postgres/rdns/20261010120000_rdns.sql:313-321`, `specs/fontes/rdns/dados.md:104-108,220-222`, `specs/fontes/rdns/collector.md:199-221`
- **Depende de / relacionado a:** 012 (mesma transação de SaveLive; aplicar os dois diffs juntos)
- **Esforço estimado:** P (até ~1 h)
- **Auditoria:** 2026-11-07, commit `db43842`

## Explicação para leigos

Quando alguém pede o nome de um IP "na hora", o coletor faz três coisas, nesta ordem:
1. confere se o IP pertence a um bloco cadastrado;
2. faz a consulta DNS;
3. grava o resultado.

Se o administrador remover o bloco enquanto isso acontece, a limpeza automática roda antes da gravação. O coletor então grava um resultado de um bloco que não existe mais.

## Evidência e análise técnica

- `live.go:170`: `l.store.LiveAllowed(ctx, ip)` é uma consulta isolada, sem trava (`store.go:371-377`).
- `live.go:179-181`: o `Lookup` pode levar até `MaxAge`, que é 30 s.
- `live.go:187`: `l.store.SaveLive(ctx, res)`. A transação (`store.go:382-402`) não olha `rdns_prefix`.
- Migration `:313-321`: um gatilho `AFTER DELETE` em `rdns_prefix` faz `DELETE FROM rdns_ptr WHERE ip <<= OLD.prefix`.
- Sequência que deixa a linha órfã:
  1. O pedido "na hora" confere o IP, e ele está permitido.
  2. O administrador faz `DELETE` do prefixo. O gatilho apaga os PTR e a transação faz commit.
  3. O SaveLive encontra o IP sem linha, decide fazer `Insert` e grava uma linha órfã.

## Verificação (tentativa de refutação)

- **Linha que já existia:** o `FOR UPDATE` de `current` faz o gatilho esperar, e a linha é apagada certo. O problema é só a linha que o pedido "na hora" cria.
- **SaveScan:** está protegido. O `UPDATE rdns_prefix ...` do fim não acha a linha, e a transação volta atrás.
- **Desativação do prefixo:** pela spec os resultados ficam (`dados.md:32,220`), então não gera órfã.
- **Quem dispara:** só o administrador (`dados.md:222`). A derivação automática não apaga prefixos.
- **API:** as consultas fazem JOIN com `rdns_prefix` (`dados.md:265-266`), então a órfã fica invisível enquanto o prefixo não existir.

## Impacto

Ficam linhas órfãs em `rdns_ptr`, contrariando a regra de que apagar um prefixo apaga os resultados dele. Se o prefixo for recadastrado, a órfã aparece como dado antigo até a primeira varredura.

## Solução proposta

Na transação de `SaveLive`, reconferir o prefixo com trava `FOR KEY SHARE`:
- essa trava conflita com o `DELETE` do administrador, que passa a esperar o commit, e o gatilho então apaga também a linha nova;
- ela não conflita com os `UPDATE` do Claim e do SaveScan;
- se o prefixo não existir mais, a função não grava nada e devolve `ErrNotAllowed`, que o laço responde como `not_allowed`.

Atualizar `specs/fontes/rdns/collector.md` (passo 4 do tempo real). Dono: sub-agente `collector-rdns`.

## Diff/patch proposto — NÃO APLICADO

```diff
--- a/apps/rdns/collector/internal/store/store.go
+++ b/apps/rdns/collector/internal/store/store.go
@@ -379,12 +379,25 @@
+// ErrNotAllowed indica que o prefixo do IP sumiu (ou saiu da regra) entre o
+// pedido de tempo real e a gravação: nada é gravado.
+var ErrNotAllowed = errors.New("ip fora dos prefixos ativos")
+
 // SaveLive grava o resultado de um pedido de tempo real numa transação de um
 // IP, com as mesmas regras da varredura (e jobs, se mudou). O last_scan_at
 // do prefixo não muda. Devolve se a linha mudou.
 func (s *Store) SaveLive(ctx context.Context, res resolver.Result) (bool, error) {
 	tx, err := s.pool.Begin(ctx)
 	if err != nil {
 		return false, err
 	}
 	defer tx.Rollback(ctx)
+	// Reconfere o prefixo com trava: um DELETE do administrador espera este
+	// commit, e o trigger dele apaga também o que for gravado aqui.
+	var one int
+	err = tx.QueryRow(ctx, `
+		SELECT 1 FROM rdns_prefix WHERE prefix >>= $1::inet AND active AND `+RuleSQL+`
+		 LIMIT 1 FOR KEY SHARE`, res.IP.String()).Scan(&one)
+	if errors.Is(err, pgx.ErrNoRows) {
+		return false, ErrNotAllowed
+	}
+	if err != nil {
+		return false, err
+	}
 	cur, err := current(ctx, tx, netip.PrefixFrom(res.IP, 32))
 	if err != nil {
 		return false, err
--- a/apps/rdns/collector/internal/live/live.go
+++ b/apps/rdns/collector/internal/live/live.go
@@ -186,10 +186,14 @@ func (l *Loop) Handle(ctx context.Context, payload string) {
-		if _, err := l.store.SaveLive(ctx, res); err != nil {
+		_, err = l.store.SaveLive(ctx, res)
+		switch {
+		case errors.Is(err, store.ErrNotAllowed):
+			reply.Result = "not_allowed"
+		case err != nil:
 			l.fail(ctx, ip, err)
 			return
-		}
-		reply.Result = res.Status.String()
-		if res.Status == resolver.PTR {
-			reply.Names = res.Names
+		default:
+			reply.Result = res.Status.String()
+			if res.Status == resolver.PTR {
+				reply.Names = res.Names
+			}
 		}
 	}
```

Em `live.go`, importar `errors` e `internal/store` se ainda não estiverem importados.

## Critérios de aceite

- [ ] A spec do tempo real diz que a gravação reconfere o prefixo na mesma transação.
- [ ] Apagar o prefixo durante uma consulta "na hora" não deixa linha em `rdns_ptr`, e quem fez o pedido recebe `not_allowed`.
- [ ] Não há espera perceptível entre o pedido "na hora", o Claim e o SaveScan.

## Testes recomendados

Teste de integração:
1. Configurar um resolvedor falso que demora 3 s para responder.
2. Fazer um pedido "na hora".
3. Durante a consulta, rodar `DELETE FROM rdns_prefix`.
4. Conferir que `rdns_ptr` fica vazia para o IP.

Repetir com a ordem invertida: o `DELETE` esperando o commit do pedido.

## Limitações e suposições

Assumi o nível de isolamento READ COMMITTED, que é o padrão.
