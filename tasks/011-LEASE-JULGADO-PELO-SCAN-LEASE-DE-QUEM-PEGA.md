# 011 — O lease de um alvo é julgado pelo SCAN_LEASE de quem pega, não pelo de quem está varrendo

- **Severidade:** baixa
- **Confiança:** confirmado
- **Categoria:** ponto fraco
- **Subcategoria:** concorrência entre réplicas / configuração heterogênea
- **Status:** aberto — correção não aplicada
- **Arquivos afetados:** `apps/rdns/collector/internal/store/store.go:145-165`, `apps/rdns/collector/internal/config/config.go:53,109-111`, `apps/rdns/collector/internal/scan/scan.go:176,215-224`, `apps/registrobr/rdapnum/collector/internal/store/store.go:176-191`, `apps/registrobr/rdapnum/collector/internal/config/config.go:67,126-127`, `specs/padroes/sonda.md:128,138-143,230`, `specs/fontes/registrobr/rdapnum/collector.md:100-101`
- **Depende de / relacionado a:** nenhum (padrão comum a collector-rdns e collector-registrobr-rdapnum)
- **Esforço estimado:** M (até 1 dia)
- **Auditoria:** 2026-11-07, commit `db43842`

## Explicação para leigos

Várias cópias do coletor dividem uma fila de tarefas. Cada cópia "reserva" uma tarefa por um prazo, o lease. Só depois que esse prazo vence outra cópia pode assumir a tarefa.

O problema é que o prazo é medido pelo relógio de quem quer assumir, não pelo de quem reservou. Se uma cópia foi configurada com prazo curto e outra com prazo longo, a de prazo curto toma tarefas que a outra ainda está fazendo dentro do prazo dela. O trabalho é feito duas vezes, e o da primeira cópia vai para o lixo.

## Evidência e análise técnica

- `Claim` (rdns, `store.go:147-157`) usa a condição `AND (scan_worker IS NULL OR scan_started_at < NOW() - $2::float8 * interval '1 second')`. O `$2` é `seconds(lease)`, tirado do `Options.Lease` da instância local.
- O SCAN_LEASE é configurado em cada instância: `config.go:53` (padrão `2m`) e `config.go:109-111` (mínimo de 30 s).
- O dono da tarefa calcula o próprio prazo com o lease dele: `scan.go:176` e `scan.go:215`.
- O rdapnum repete o padrão em `store.go:184`, com o lease vindo de `config.go:67`.
- Nenhuma coluna guarda o vencimento ou o lease do dono. A tabela só tem `scan_worker` e `scan_started_at` (`database/postgres/rdns/20261010120000_rdns.sql:223-224`).
- Cenário concreto:
  1. A instância A tem `SCAN_LEASE=5m` e B tem `2m`.
  2. A pega um /22 às 12:00:00 e vai varrer até 12:04:55.
  3. Às 12:02:01, B roda o Claim: o prefixo começou às 12:00:00, antes do limite de 12:00:01, então B o toma.
  4. Quando A termina, o `UPDATE ... WHERE scan_started_at = $4` não acha a linha. A recebe ErrLeaseLost e descarta a varredura.
- O caso inverso também acontece: se o dono morre e só sobram instâncias com lease maior, o alvo fica preso por mais tempo do que o lease do dono previa.

## Verificação (tentativa de refutação)

- **Proteção:** SaveScan, Release e SaveAttempt só gravam se `scan_worker = $2 AND scan_started_at = $4`. Isso impede gravação dupla ou corrompida. O dano fica restrito a trabalho desperdiçado.
- **Alcance:** é o laço normal da fila em produção.
- **Pré-condição:** réplicas com SCAN_LEASE diferente. Os composes não repassam SCAN_LEASE (`specs/fontes/rdns/collector.md:287`), então no deploy padrão todas usam `2m`. Mas a configuração diferente é um caminho natural:
  - `sonda.md:43-44` prevê instâncias em outros hosts;
  - `collector.md:195-197` manda subir o SCAN_LEASE quando a taxa local é baixa, e RATE_LIMIT também é configurado por instância.
- **Documentação:** a spec não exige o mesmo lease em todas as réplicas. Além disso, a spec do rdapnum (`collector.md:100-101`) afirma que "dois coletores nunca pegam o mesmo alvo", o que deixa de valer nesse cenário.

## Impacto

- **rdns:** consultas DNS em dobro. Prefixos grandes podem nunca terminar na instância de lease longo, porque são sempre tomados por outra, o que atrasa a meta `SCAN_INTERVAL`.
- **rdapnum:** requisições RDAP em dobro ao registro.br, que tem limite de taxa. Isso pode antecipar a espera ou a pausa.
- Não há perda nem corrupção de dados.

## Solução proposta

Gravar o vencimento do lease na própria linha, no momento da reserva: `scan_lease_until = NOW() + lease do dono`. A fila passa a comparar com essa coluna. Release e a gravação final zeram a coluna junto com `scan_worker`.

A spec muda antes do código:
- `specs/padroes/sonda.md`: o SQL da fila e o item "Lease";
- `specs/fontes/rdns/dados.md` e `specs/fontes/registrobr/rdapnum/dados.md`: a coluna nova;
- `specs/fontes/registrobr/rdapnum/collector.md:100-101`.

Donos: os sub-agentes `collector-rdns` e `collector-registrobr-rdapnum`.

Alternativa mais simples: documentar que o SCAN_LEASE tem de ser igual em todas as réplicas, ou levá-lo para `rdns_config`/`registrobr_rdapnum_config`.

## Diff/patch proposto — NÃO APLICADO

```diff
--- a/apps/rdns/collector/internal/store/store.go
+++ b/apps/rdns/collector/internal/store/store.go
@@ -145,10 +145,11 @@ func (s *Store) Claim(ctx context.Context, worker string, lease, interval time.Duration) (*Claim, error) {
 	var c Claim
 	err := s.pool.QueryRow(ctx, `
-		UPDATE rdns_prefix SET scan_worker = $1::uuid, scan_started_at = NOW()
+		UPDATE rdns_prefix SET scan_worker = $1::uuid, scan_started_at = NOW(),
+		       scan_lease_until = NOW() + $2::float8 * interval '1 second'
 		 WHERE uuid = (SELECT uuid FROM rdns_prefix
 		                WHERE active AND `+RuleSQL+`
-		                  AND (scan_worker IS NULL OR scan_started_at < NOW() - $2::float8 * interval '1 second')
+		                  AND (scan_worker IS NULL OR coalesce(scan_lease_until, scan_started_at + $2::float8 * interval '1 second') < NOW())
 		                  AND (rescan OR last_scan_at IS NULL OR last_scan_at < NOW() - $3::float8 * interval '1 second')
 		                ORDER BY rescan DESC, last_scan_at ASC NULLS FIRST
 		                LIMIT 1
@@ -168,7 +169,7 @@ func (s *Store) Release(ctx context.Context, worker string, c *Claim) error {
 	_, err := s.pool.Exec(ctx, `
-		UPDATE rdns_prefix SET scan_worker = NULL, scan_started_at = NULL
+		UPDATE rdns_prefix SET scan_worker = NULL, scan_started_at = NULL, scan_lease_until = NULL
 		 WHERE uuid = $1::uuid AND scan_worker = $2::uuid AND scan_started_at = $3`,
 		c.UUID, worker, c.StartedAt)
 	return err
```

Também é preciso:
- uma migration nova com `ALTER TABLE rdns_prefix ADD COLUMN scan_lease_until timestamptz;`;
- zerar a coluna no `UPDATE` final de SaveScan;
- aplicar o mesmo em `registrobr_rdapnum_target`.

## Critérios de aceite

- [ ] A spec do padrão da sonda define que o alvo vence pelo lease do dono, gravado na reserva.
- [ ] Uma instância com SCAN_LEASE curto não pega um alvo reservado por outra cujo lease ainda não venceu.
- [ ] Linhas antigas, sem `scan_lease_until`, continuam funcionando graças ao `coalesce`.

## Testes recomendados

Integração com duas instâncias de SCAN_LEASE diferente (30 s e 2 min): a de 30 s não retoma o alvo da outra antes de 2 min. Repetir no rdapnum.

## Limitações e suposições

O impacto depende de réplicas com SCAN_LEASE diferente, o que o compose padrão não produz. Não medi com que frequência isso acontece em produção.
