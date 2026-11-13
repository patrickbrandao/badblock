# 008 — Estouro de inteiro em SplitIPv4 aceita registro IPv4 absurdo e faz recusar o arquivo inteiro

- **Severidade:** baixa
- **Confiança:** confirmado
- **Categoria:** bug
- **Subcategoria:** validação de entrada / estouro de inteiro
- **Status:** correção aplicada na branch local `melhorias/rdns-ptr-conflict-e-status-tasks` (13/11/2026) nos cinco coletores de RIR, com casos novos em `TestSplitIPv4`; NÃO compilada nem testada (sem Go na estação)
- **Arquivos afetados:** `apps/lacnic/collector/internal/parse/parse.go:323-345`, `apps/lacnic/collector/internal/parse/parse.go:386-410`, `apps/afrinic/collector/internal/parse/parse.go:392`, `apps/apnic/collector/internal/parse/parse.go:392`, `apps/arin/collector/internal/parse/parse.go:392`, `apps/ripencc/collector/internal/parse/parse.go:392`, `specs/fontes/rir/formato.md:154`
- **Depende de / relacionado a:** 009 (o arquivo recusado por este bug seria baixado e recusado de novo sem fim)
- **Esforço estimado:** P (até ~1 h)
- **Auditoria:** 2026-11-07, commit `db43842`

## Explicação para leigos

Cada linha IPv4 do arquivo de um RIR diz "a partir deste endereço, tantos endereços". O coletor confere se essa faixa não passa do último endereço IPv4. Só que a conta é feita de um jeito que, com um número gigantesco, "dá a volta no contador", como um hodômetro que passa de 999999 para 000000.

Com isso, um registro absurdo parece válido e vira blocos sem sentido, inclusive "a internet inteira" (0.0.0.0/0). No fim, o banco recusa o arquivo todo, quando a regra manda descartar só aquela linha.

## Evidência e análise técnica

Trecho de `SplitIPv4` (lacnic, `parse.go:386-410`):

```go
cur := uint64(b[0])<<24 | uint64(b[1])<<16 | uint64(b[2])<<8 | uint64(b[3])
if count < 1 || cur+count > 1<<32 {
```

- `cur` vale no máximo 2^32-1. `count` vem de `strconv.ParseUint(f[4], 10, 64)` (`parse.go:329`), então pode chegar a 2^64-1.
- Com `cur >= 1` e `count >= 2^64 - cur`, a soma `cur+count` estoura e cai abaixo de 2^32. A condição dá falso e a validação passa.
- Exemplo: `lacnic|BR|ipv4|0.0.0.1|18446744073709551615|20200101|allocated` dá `cur+count = 0`.
- O laço termina, mas gera blocos de 1, 2, 4, …, 2^63 endereços:
  - prefixos de `0.0.0.1/32` até `128.0.0.0/1`;
  - `0.0.0.0/0`, quando cur chega a 2^32;
  - prefixos inválidos, com bits negativos, para tamanhos acima de 2^32.
- Em `record` (`parse.go:340-341`), `RecordValue: int64(count)` vira -1.
- O banco recusa a carga pelo `CHECK (... record_value >= 1)` (`database/postgres/lacnic/20260929000000_lacnic.sql:191`). A aplicação falha e o arquivo inteiro é recusado.
- A spec (`specs/fontes/rir/formato.md:154`) manda o contrário: descartar a linha com o motivo `quantidade de endereços inválida`.

## Verificação (tentativa de refutação)

1. **A evidência existe como descrita?** Sim. A checagem fica em `parse.go:392`, nos cinco clones.
2. **Há proteção em outro lugar?** Não. O caso do ASN usa uma forma segura (`count > math.MaxUint32-start+1`); o do IPv4 não. O CHECK do banco só transforma o problema em recusa total.
3. **É alcançável?** Sim. `Parse` processa toda linha do arquivo baixado.
4. **As pré-condições são realistas?** Pouco. O RIR teria de publicar um `value` de ~1,8×10^19, o que só acontece por corrupção ou por bug no gerador do arquivo. O download é por HTTPS e conferido pelo `.md5`.
5. **É intencional?** Não. A spec diz o contrário, e o teste (`parse_test.go:358`) só cobre `0.0.0.0` com 2^32+1, que não estoura.

## Impacto

Uma única linha absurda faz o coletor recusar o arquivo inteiro do RIR, em vez de descartá-la com aviso. As tabelas ficam congeladas na versão anterior, e a recusa se repete (009). Nenhum dado errado chega a ser servido.

## Solução proposta

Trocar a soma por uma subtração que não estoura: `count > 1<<32 - cur`. Como `cur <= 2^32-1`, o lado direito vale no mínimo 1.

A correção começa no modelo `collector-lacnic` e é replicada nos clones (`collector-afrinic`, `collector-apnic`, `collector-arin`, `collector-ripencc`). A spec já descreve o comportamento certo. Vale acrescentar o caso do estouro aos testes de `specs/fontes/rir/collector.md`.

## Diff/patch proposto — NÃO APLICADO

```diff
--- a/apps/lacnic/collector/internal/parse/parse.go
+++ b/apps/lacnic/collector/internal/parse/parse.go
@@ -389,7 +389,8 @@ func SplitIPv4(start netip.Addr, count uint64) ([]netip.Prefix, error) {
 	b := start.As4()
 	cur := uint64(b[0])<<24 | uint64(b[1])<<16 | uint64(b[2])<<8 | uint64(b[3])
-	if count < 1 || cur+count > 1<<32 {
+	// 1<<32 - cur >= 1 (cur <= 2^32-1): a subtração não estoura, a soma sim.
+	if count < 1 || count > 1<<32-cur {
 		return nil, fmt.Errorf("quantidade de endereços inválida %d a partir de %s", count, start)
 	}
--- a/apps/lacnic/collector/internal/parse/parse_test.go
+++ b/apps/lacnic/collector/internal/parse/parse_test.go
@@ -355,5 +355,5 @@
 	for _, bad := range []struct {
 		start string
 		count uint64
-	}{{"10.0.0.0", 0}, {"255.255.255.0", 257}, {"0.0.0.0", 1<<32 + 1}, {"2001:db8::", 1}} {
+	}{{"10.0.0.0", 0}, {"255.255.255.0", 257}, {"0.0.0.0", 1<<32 + 1}, {"0.0.0.1", math.MaxUint64}, {"255.255.255.255", math.MaxUint64 - 4294967294}, {"2001:db8::", 1}} {
 		if _, err := SplitIPv4(netip.MustParseAddr(bad.start), bad.count); err == nil {
```

Repetir nos quatro clones. O teste precisa importar `math`.

## Critérios de aceite

- [ ] `SplitIPv4(0.0.0.1, 2^64-1)` devolve erro.
- [ ] Em `Parse`, uma linha assim conta em `Skipped`, gera o aviso de quantidade inválida, e o arquivo segue.
- [ ] Os cinco coletores de RIR recebem a mesma correção.

## Testes recomendados

- Caso novo em `TestSplitIPv4`.
- Teste de `Parse` com uma linha estourada no meio de um recorte válido, conferindo `Skipped == 1` e que nenhum prefixo `0.0.0.0/0` foi gerado.

## Limitações e suposições

O comportamento do laço foi analisado por raciocínio, sem executar o código. Não conferi se o encoder do pgx falha antes do CHECK com o prefixo inválido. Nos dois casos o resultado é o mesmo: o arquivo é recusado.
