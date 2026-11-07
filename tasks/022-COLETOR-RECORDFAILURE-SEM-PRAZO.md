# 022 — Gravação da recusa sem prazo pode travar o laço do coletor com o Postgres parado

- **Severidade:** baixa
- **Confiança:** provável
- **Categoria:** ponto fraco
- **Subcategoria:** timeouts / resiliência
- **Status:** aberto — correção não aplicada
- **Arquivos afetados:**
  - `apps/anatel/pst/collector/internal/collector/collector.go:179-184`
  - `apps/anatel/pst/collector/internal/store/store.go:108-129`
  - `apps/anatel/pst/collector/internal/store/store.go:175-178`
  - o mesmo padrão nos outros 11 coletores de arquivo:
    - `apps/lacnic/collector/internal/collector/collector.go:217-223`
    - afrinic, apnic, arin e ripencc: `collector.go:219`
    - cgibr: `:168`
    - iana: `:222`
    - ripe/asnames: `:145`
    - rootzone: `:193`
    - roothints: `:192`
    - rootanchors: `:172`
  - `specs/fontes/anatel/pst/collector.md:282-285`
  - `specs/padroes/coletor.md:151-157`
- **Depende de / relacionado a:** 009
- **Esforço estimado:** P (até ~1 h) por app; o padrão é comum a 12 coletores
- **Auditoria:** 2026-11-07, commit `db43842`

## Explicação para leigos

Quando o coletor recusa um arquivo, ele anota a recusa no banco. Essa anotação foi feita de propósito para não ser interrompida pelo relógio da verificação, mas ficou sem limite de tempo nenhum.

Se o banco estiver travado, por exemplo esperando outra operação longa terminar, o coletor fica parado ali indefinidamente. Ele não volta a verificar arquivos novos e nem atende o pedido de desligar.

## Evidência e análise técnica

- `fail` (anatel/pst, `collector.go:179-184`) chama `c.store.RecordFailure(context.WithoutCancel(ctx), run, cause)`. O `WithoutCancel` tira o prazo do `RUN_TIMEOUT` e também o cancelamento pelo sinal de encerramento.
- `store.Open` (`store.go:108-129`) não define `statement_timeout`, `lock_timeout` nem `connect_timeout`.
- pgx v5.11.0: o dialer padrão é `&net.Dialer{}` (`vendor/github.com/jackc/pgx/v5/pgconn/config.go:1085-1088`), com o keepalive TCP padrão do Go. Isso detecta uma conexão morta, mas não um servidor vivo que não responde ao comando (por exemplo, por lock ou I/O travado).
- Dentro do próprio projeto, rdns e rdapnum já limitam operações parecidas com `context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)` (`apps/rdns/collector/internal/scan/scan.go:206`, `apps/registrobr/rdapnum/collector/internal/scan/scan.go:186`).
- O gatilho mais plausível: o `Apply` estoura o `RUN_TIMEOUT` porque o banco está lento, e logo em seguida `RecordFailure` roda sem prazo nesse mesmo banco.

## Verificação (tentativa de refutação)

1. **Evidência:** confere.
2. **Proteção:** o keepalive TCP só cobre conexão morta. Ninguém define `statement_timeout`. Num `docker stop`, o Docker acaba mandando SIGKILL.
3. **Alcance:** acontece em toda recusa depois do download.
4. **Pré-condição:** o Postgres lento ou travado por lock em `<fonte>_run`. É plausível durante migração ou manutenção.
5. **Intencional?** Não ser cancelado é intencional e está documentado (`specs/fontes/anatel/pst/collector.md:282-285`). Não ter prazo nenhum não está.

## Impacto

O processo fica preso até o banco liberar: não faz novas verificações, não gera log e não encerra de forma limpa no `docker stop`. Não corrompe dados, e o problema se resolve sozinho quando o banco volta.

## Solução proposta

Dar à gravação da recusa um prazo próprio, separado do `RUN_TIMEOUT`, por exemplo 30 s: `context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)`.

- Atualizar `specs/padroes/coletor.md` e `specs/fontes/anatel/pst/collector.md:282-285`.
- Aplicar nos 12 coletores, cada um pelo seu sub-agente.
- Opcional: definir um `statement_timeout`/`lock_timeout` padrão em `RuntimeParams` quando a URL não trouxer um.

## Diff/patch proposto — NÃO APLICADO

```diff
--- a/apps/anatel/pst/collector/internal/collector/collector.go
+++ b/apps/anatel/pst/collector/internal/collector/collector.go
@@ -178,7 +178,11 @@
 // fail grava a recusa em anatel_pst_run e devolve a causa.
 func (c *Collector) fail(ctx context.Context, run store.Run, cause error) error {
-	if err := c.store.RecordFailure(context.WithoutCancel(ctx), run, cause); err != nil {
+	// Sem o cancelamento do RUN_TIMEOUT (a recusa é gravada mesmo com ele
+	// estourado), mas com prazo próprio: um banco parado não trava o laço.
+	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
+	defer cancel()
+	if err := c.store.RecordFailure(rctx, run, cause); err != nil {
 		c.log.Error("não consegui gravar a falha em anatel_pst_run", "err", err)
 	}
 	return cause
```

## Critérios de aceite

- [ ] Com um store falso que bloqueia em `RecordFailure`, `RunOnce` retorna em ~30 s, com o log de falha.
- [ ] Com `RUN_TIMEOUT` estourado e o banco saudável, a recusa continua sendo gravada.
- [ ] As specs estão atualizadas, e os 12 coletores seguem o mesmo padrão.

## Testes recomendados

Teste unitário com um store falso cujo `RecordFailure` espera `ctx.Done()`. O teste confere que a chamada retorna `context.DeadlineExceeded` dentro do prazo. Deixe o prazo injetável para o teste rodar rápido.

## Limitações e suposições

- Não reproduzi um Postgres travado.
- O valor de 30 s é uma sugestão.
- Não verifiquei se a `POSTGRES_URL` de produção já traz `statement_timeout`.
