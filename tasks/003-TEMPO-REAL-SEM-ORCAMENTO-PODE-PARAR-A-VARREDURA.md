# 003 — Tempo real (live=true) tem prioridade absoluta, sem orçamento, e pode parar a varredura

- **Severidade:** média
- **Confiança:** provável
- **Categoria:** ponto fraco
- **Subcategoria:** disponibilidade / fome de prioridade
- **Status:** aberto — correção não aplicada
- **Arquivos afetados:** `apps/rdns/collector/internal/limiter/limiter.go:1-4,60-69,142-181`, `apps/rdns/collector/internal/live/live.go:42,110,178-185`, `apps/rdns/api/internal/live/live.go:140-150`, `apps/rdns/api/docker-compose.yml:58-69`, `specs/padroes/sonda.md` (seção "Taxa e configuração em tempo de execução"), `specs/fontes/rdns/collector.md:186-187`
- **Depende de / relacionado a:** 002; hipótese do rate limit do Traefik sem agregar IPv6 (ver resumo)
- **Esforço estimado:** P (até ~1 h)
- **Auditoria:** 2026-11-07, commit `db43842`

## Explicação para leigos

O coletor tem um limite de consultas por segundo. Os pedidos feitos "na hora" (tempo real), que vêm de quem usa a API, sempre passam na frente da varredura diária. Se muita gente, ou um atacante com muitos endereços, fizer pedidos na hora sem parar, a varredura nunca chega a rodar e o banco para de ser atualizado.

## Evidência e análise técnica

- `limiter.go:142-155` (`next`): `for p := range l.queues` percorre primeiro a fila Live (0) e depois a Scan (1), e devolve a primeira reserva viva. A Scan só é atendida quando a Live está vazia.
- `limiter.go:170-178`: libera uma reserva a cada `1/rate_limit`, sem contar quantas foram seguidas para cada fila.
- `collector/internal/live/live.go:42,110`: cada instância atende 32 pedidos ao mesmo tempo. Cada pedido reserva na fila Live, e as novas tentativas também (`resolver.go:153-158`), até 3 por IP.
- Entrada de pedidos:
  - `api/docker-compose.yml:66-68` limita a 1 req/s por IP de origem, com rajada de 5.
  - `api/internal/live/live.go:140-150` só elimina repetições pelo IP consultado (marcas recent e inflight) e por `maxWaits`.
  - Não há teto global de pedidos live por segundo.
- Efeito na varredura: as reservas da Scan esperam até `Lease-SaveBudget` (`scan.go:176`). Depois disso o prefixo é abandonado e devolvido (`scan.go:194-200`), e o trabalho já feito se perde.

## Verificação (tentativa de refutação)

- **Documentação:** a prioridade é intencional. `sonda.md` diz "As consultas de tempo real passam na frente", e `collector.md:186-187` confirma. Mas nenhuma spec define cota, trata a fome da varredura ou relaciona isso com a meta `SCAN_INTERVAL`.
- **Proteção:**
  - o Traefik limita por IP de origem, não no total;
  - `LIVE_QUEUE_MAX` (1000) limita o acúmulo na fila, não a vazão;
  - eliminar repetições pelo IP consultado não ajuda quando os alvos são diferentes, e há milhões de IPs válidos nos prefixos.
- **Pré-condição:** cerca de 86 a 256 origens a 1 req/s por instância do coletor, escolhendo alvos que dão timeout (3 consultas por pedido). Se a API estiver exposta em IPv6, uma /64 basta. Sem IPv6, é preciso uma botnet modesta.
- **Alcance:** `GET /rdns/ip/{ip}?live=true`, uma rota pública.

## Impacto

Enquanto o abuso durar:
- a varredura de todas as instâncias para, com prefixos abandonados em loop;
- `last_scan_at` envelhece e a meta de 24 h deixa de ser cumprida;
- o tempo real continua atendendo, mas o atacante pode gastar toda a cota do resolvedor do operador.

## Solução proposta

1. Definir na spec um orçamento, em `specs/padroes/sonda.md` e em `specs/fontes/rdns/collector.md` (seção "Taxa"). Proposta: com as duas filas cheias, o tempo real recebe no máximo 3 de cada 4 liberações, e a varredura fica com pelo menos 1/4 da taxa.
2. Implementar no `limiter` com um contador de liberações seguidas da fila Live. Dono: sub-agente `collector-rdns`.
3. Conferir no mesmo passo o limitador do collector-registrobr-rdapnum, que segue a mesma ideia de prioridade (não verificado).
4. Opcional: um limite global no router live do Traefik, combinado com o sub-agente `api-rdns`.

## Diff/patch proposto — NÃO APLICADO

```diff
--- a/apps/rdns/collector/internal/limiter/limiter.go
+++ b/apps/rdns/collector/internal/limiter/limiter.go
@@ -60,10 +60,15 @@
+// maxLiveStreak: liberações seguidas do tempo real antes de uma da varredura
+// esperando (com as duas filas cheias, o tempo real fica com até 3/4 da taxa).
+const maxLiveStreak = 3
+
 // Limiter é o limitador de uma instância.
 type Limiter struct {
 	mu       sync.Mutex
 	interval time.Duration
 	queues   [2][]*Ticket
 	pending  [2]int
 	last     time.Time // última liberação
+	streak   int       // liberações seguidas da fila Live
 	wake     chan struct{}
 	now      func() time.Time
 }
@@ -142,6 +147,12 @@ func (l *Limiter) next() *Ticket {
-	for p := range l.queues {
+	order := [2]Priority{Live, Scan}
+	if l.streak >= maxLiveStreak {
+		order = [2]Priority{Scan, Live}
+	}
+	for _, p := range order {
 		for len(l.queues[p]) > 0 {
 			t := l.queues[p][0]
 			if t.canceled {
@@ -173,6 +184,11 @@ func (l *Limiter) Run(ctx context.Context) {
 				l.queues[t.prio][0] = nil
 				l.queues[t.prio] = l.queues[t.prio][1:]
 				l.pending[t.prio]--
+				if t.prio == Live {
+					l.streak++
+				} else {
+					l.streak = 0
+				}
 				t.released = true
 				close(t.ch)
 				l.last = l.now()
```

## Critérios de aceite

- [ ] A spec define a fração mínima da taxa garantida à varredura.
- [ ] Com as duas filas sempre cheias, a varredura recebe ao menos 1 de cada 4 liberações.
- [ ] Sem pedidos de varredura, o tempo real continua usando 100% da taxa.

## Testes recomendados

- Teste de unidade do `limiter` com relógio falso: 1.000 reservas Live e 1.000 Scan, conferindo a proporção liberada.
- Integração: uma rajada de pedidos live durante uma varredura, e o prefixo ainda termina dentro do lease.

## Limitações e suposições

- A fração 3/4 é só uma sugestão.
- Não verifiquei se o host de produção expõe a API em IPv6, nem qual `sourceCriterion` o Traefik usa. Assumi o padrão, que é o IP remoto.
