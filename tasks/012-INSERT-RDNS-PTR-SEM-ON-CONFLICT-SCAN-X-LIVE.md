# 012 — INSERT em rdns_ptr sem ON CONFLICT: varredura e tempo real colidem na chave primária

- **Severidade:** baixa
- **Confiança:** confirmado
- **Categoria:** bug
- **Subcategoria:** condição de corrida / concorrência no banco
- **Status:** aberto — correção não aplicada
- **Arquivos afetados:** `apps/rdns/collector/internal/store/store.go:251-263,280-315,333-368,382-402`, `apps/rdns/collector/internal/scan/scan.go:217-221`, `apps/rdns/collector/internal/live/live.go:187-190`, `database/postgres/rdns/20261010120000_rdns.sql:170-172`, `specs/fontes/rdns/collector.md:138-143`
- **Depende de / relacionado a:** 013 (mesma transação de SaveLive; aplicar os dois diffs juntos)
- **Esforço estimado:** P (até ~1 h)
- **Auditoria:** 2026-11-07, commit `db43842`

## Explicação para leigos

Duas partes do coletor podem tentar criar, ao mesmo tempo, a linha de um IP que ainda não existe no banco: a varredura e o pedido feito "na hora". O banco aceita a primeira e recusa a segunda com erro.

Quando a recusada é a varredura, o prejuízo não fica naquele IP: perde-se o resultado do bloco inteiro, de 256 a 1.024 IPs.

## Evidência e análise técnica

- `store.go:253-255`: `INSERT INTO rdns_ptr (ip, status, name, names) VALUES ($1::inet, $2, $3, $4)`, sem `ON CONFLICT`.
- Migration `:171`: `ip inet PRIMARY KEY`.
- `store.go:282-283` (`current`): `SELECT ... WHERE ip <<= $1::cidr FOR UPDATE` só trava linhas que já existem. Para um IP sem linha, `Decide` (`store.go:204-219`) devolve `Insert` nas duas transações.
- Sequência que dispara a colisão:
  1. A SaveScan lê `current`: o IP X não tem linha.
  2. A SaveLive lê `current`: o IP X também não tem linha.
  3. A SaveScan insere X, ainda sem commit.
  4. A SaveLive tenta inserir X e fica esperando a trava.
  5. A SaveScan faz commit, e a SaveLive falha com `duplicate key value violates unique constraint "rdns_ptr_pkey"`.
- Na ordem inversa, quem falha é a SaveScan:
  - **Se a SaveScan falha:** `scan.go:219-221` registra o erro e pausa por `RetryAfter`. O prefixo inteiro é descartado.
  - **Se a SaveLive falha:** `live.go:187-190` chama `l.fail`, e quem fez o pedido recebe o dado frio.

## Verificação (tentativa de refutação)

- **Varredura contra varredura:** não acontece. O claim é exclusivo e a constraint `ex_rdns_prefix_overlap` impede prefixos sobrepostos.
- **Live contra live do mesmo IP:** a API marca o pedido com `SET badblock:rdns:live:inflight:<ip> NX` (`specs/fontes/rdns/api.md:152`), então só colidiriam no caso raro de a marca expirar.
- **Varredura contra live:** possível. Basta um live de um IP do prefixo durante a transação de gravação da varredura. A janela é estreita.
- **"Release não chamado depois de erro":** é o comportamento previsto na spec (`specs/padroes/sonda.md`, falha no Postgres ao gravar), então não conta como defeito.
- **Spec:** ela prescreve o INSERT simples (`collector.md:138-143`). A correção começa por ela.

## Impacto

De vez em quando, a varredura de um prefixo inteiro se perde, volta para a fila e é refeita, desperdiçando consultas. Ou um live responde com dado frio. Não há corrupção de dados.

## Solução proposta

Mudar primeiro a spec: `specs/fontes/rdns/collector.md` (passo 3 da varredura) e `dados.md#rdns_ptr`. O INSERT passa a ser `ON CONFLICT (ip) DO UPDATE`, e o `WHERE` do DO UPDATE mantém a regra "uma falha nunca apaga um PTR". Dono: sub-agente `collector-rdns`.

## Diff/patch proposto — NÃO APLICADO

```diff
--- a/apps/rdns/collector/internal/store/store.go
+++ b/apps/rdns/collector/internal/store/store.go
@@ -251,8 +251,12 @@ func queueChange(b *pgx.Batch, op Op, res resolver.Result) {
 	ip := res.IP.String()
 	switch op {
 	case Insert:
-		b.Queue(`INSERT INTO rdns_ptr (ip, status, name, names) VALUES ($1::inet, $2, $3, $4)`,
+		// A linha pode ter nascido noutra transação (varredura × tempo real)
+		// depois da leitura: vira um UPDATE, sem uma falha apagar um PTR.
+		b.Queue(`INSERT INTO rdns_ptr (ip, status, name, names) VALUES ($1::inet, $2, $3, $4)
+			ON CONFLICT (ip) DO UPDATE SET status = EXCLUDED.status, name = EXCLUDED.name, names = EXCLUDED.names
+			 WHERE EXCLUDED.status = 1 OR rdns_ptr.status <> 1`,
 			ip, int16(res.Status), nullName(res), nullNames(res))
 	case Update:
 		b.Queue(`UPDATE rdns_ptr SET status = $2, name = $3, names = $4 WHERE ip = $1::inet`,
```

## Critérios de aceite

- [ ] A spec descreve o INSERT com `ON CONFLICT (ip) DO UPDATE` e a guarda que impede uma falha de apagar um PTR.
- [ ] SaveScan e SaveLive concorrentes no mesmo IP sem linha terminam as duas sem erro.
- [ ] Uma falha que chega por último não sobrescreve um PTR gravado pela outra transação.

## Testes recomendados

Teste de integração com duas transações intercaladas:
1. A primeira insere X e não faz commit.
2. A segunda insere X e fica esperando.
3. A primeira faz commit.

A segunda não deve falhar, e a linha final deve obedecer às regras de escrita.

## Limitações e suposições

- `last_scan_changed` pode contar uma mudança que acabou virando no-op. Isso é aceitável.
- Não medi a frequência real da colisão.
