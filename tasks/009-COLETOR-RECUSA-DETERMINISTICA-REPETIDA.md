# 009 — Arquivo recusado por validação determinística é baixado e recusado de novo a cada RETRY_INTERVAL

- **Severidade:** baixa
- **Confiança:** confirmado
- **Categoria:** ponto fraco
- **Subcategoria:** laço de novas tentativas / consumo de recursos
- **Status:** aberto — correção não aplicada
- **Arquivos afetados:**
  - `apps/lacnic/collector/cmd/collector-lacnic/main.go:85-97`
  - `apps/lacnic/collector/internal/collector/collector.go:95-169`
  - `apps/lacnic/collector/internal/collector/collector.go:217-223`
  - `apps/lacnic/collector/internal/store/store.go:140-147`
  - os equivalentes em afrinic, apnic, arin, ripencc, cgibr, iana, ripe/asnames, anatel/pst, rootzone, roothints e rootanchors
  - `specs/padroes/coletor.md:151-160`
- **Depende de / relacionado a:** 005 e 008 (causas possíveis de recusa determinística)
- **Esforço estimado:** M (até 1 dia): padrão comum a 12 coletores
- **Auditoria:** 2026-11-07, commit `db43842`

## Explicação para leigos

Às vezes o coletor recusa um arquivo novo por um motivo que não se resolve sozinho. Por exemplo: "esse arquivo apagaria muita coisa, confirme com --force".

O problema é que ele não guarda que já recusou aquele arquivo. Cinco minutos depois, baixa o mesmo arquivo, refaz todo o trabalho e recusa de novo. Isso continua o dia inteiro, até alguém intervir ou a fonte publicar outro arquivo.

## Evidência e análise técnica

- `LastApplied` lê só as execuções com `status = 1` (`store.go:140-147`). É dela que `RunOnce` tira os validadores HTTP, o MD5 e o SHA-256 (`collector.go:95-102`).
- Quando a recusa acontece depois do download, `c.fail(...)` grava `status = 0` e devolve erro (`collector.go:130-169` e `217-223`).
- Com erro, o laço espera `min(cfg.RetryInterval, cfg.SyncInterval)`, por padrão 5 min (`main.go:85-97`).
- Na verificação seguinte, nenhuma das checagens de "sem mudança" acerta:
  - o GET condicional usa os validadores do arquivo **aplicado**, então o servidor não responde 304;
  - o MD5 publicado é diferente do MD5 aplicado;
  - o SHA do arquivo baixado é diferente do SHA aplicado.
- O ciclo inteiro se repete: download, parse, transação e uma nova linha `status = 0`.
- Recusas determinísticas que caem nesse laço:
  - parser com mais de 1% das linhas descartadas;
  - mínimos como `MIN_RECORDS` (e equivalentes);
  - trava de remoção (`store.RemovalError`, "use --force se for legítimo").
- O padrão `WHERE status = 1` aparece em todos os 12 coletores de arquivo, por exemplo `apps/ripe/asnames/collector/internal/store/store.go:126` e `apps/anatel/pst/collector/internal/store/store.go:151`.

## Verificação (tentativa de refutação)

1. **A evidência existe?** Sim.
2. **Há proteção?** Não. Nada registra que o arquivo já foi recusado, e não há retenção nem limpeza de `<fonte>_run`.
3. **É alcançável?** Sim, no laço normal de produção.
4. **As pré-condições são realistas?** Sim. Basta uma remoção grande e legítima na fonte, uma mudança de formato que derrube mais de 1% das linhas, ou um arquivo truncado que fique horas no ar.
5. **É intencional?** Só em parte. A repetição está documentada para a recusa **transitória**: MD5 divergente por corrida (`specs/padroes/coletor.md:120-122`, `specs/fontes/apnic/collector.md:43`). Para as recusas determinísticas, a spec só diz "recusa → falha → RETRY_INTERVAL" (`specs/padroes/coletor.md:151-160`).

## Impacto

- Downloads repetidos contra a fonte, com risco de bloqueio por abuso.
- CPU e memória gastas com parse a cada 5 minutos.
- `<fonte>_run` cresce cerca de 288 linhas por dia, cada uma com até 51 avisos em jsonb.
- Logs de erro a cada 5 minutos, que escondem outras falhas.
- Os dados não ficam errados: as tabelas continuam na versão anterior.

## Solução proposta

Mudar primeiro `specs/padroes/coletor.md`, na seção "Recusas e falhas":
- Um arquivo recusado por parser, sanidade ou trava de remoção fica lembrado em memória: SHA-256 e validadores HTTP.
- Enquanto a fonte servir o mesmo conteúdo, a verificação conta como **sem mudança**, com o motivo `arquivo já recusado` e um aviso no log. O próximo GET condicional usa os validadores desse arquivo.
- MD5 divergente e erro de banco continuam sendo repetidos a cada `RETRY_INTERVAL`.
- `--force` ignora essa memória.

Implementar no modelo `collector-lacnic` e propagar para os clones de RIR. Depois, cada sub-agente aplica no seu coletor: `collector-cgibr`, `collector-iana`, `collector-ripe-asnames`, `collector-anatel-pst`, `collector-rootzone`, `collector-roothints` e `collector-rootanchors`.

## Diff/patch proposto — NÃO APLICADO

Esboço; as posições `@@` são aproximadas.

```diff
--- a/apps/lacnic/collector/internal/collector/collector.go
+++ b/apps/lacnic/collector/internal/collector/collector.go
@@ -71,10 +71,14 @@
 // Collector junta fonte, banco e opções.
 type Collector struct {
 	store Store
 	src   Source
 	opt   Options
 	log   *slog.Logger
 	now   func() time.Time
+	// refused: o último arquivo recusado por motivo que não muda sozinho
+	// (parser, sanidade, trava de remoção). Só em memória.
+	refusedSHA string
+	refusedVal fetch.Validators
 }
@@ -100,6 +104,9 @@
 		if last.URL == c.opt.URL {
 			prev = fetch.Validators{ETag: last.ETag, LastModified: last.LastModified}
 		}
 	}
+	if c.refusedSHA != "" {
+		prev = c.refusedVal
+	}
@@ -128,6 +135,10 @@
 	if !force && dl.SHA256 == lastSHA {
 		return c.unchanged(ctx, lastVersion, "conteúdo igual ao último aplicado")
 	}
+	if !force && dl.SHA256 == c.refusedSHA {
+		c.log.Warn("arquivo já recusado; aguardando um novo ou --force", "sha256", dl.SHA256)
+		return c.unchanged(ctx, lastVersion, "arquivo já recusado")
+	}
@@ -138,7 +149,7 @@
 	ds, err := parse.Parse(bytes.NewReader(dl.Body), c.opt.Registry)
 	if err != nil {
-		return nil, c.fail(ctx, run, fmt.Errorf("parser: %w", err))
+		return nil, c.refuse(ctx, run, fmt.Errorf("parser: %w", err))
 	}
@@ -158,7 +169,7 @@
 	if n := ds.Records(); n < c.opt.MinRecords {
-		return nil, c.fail(ctx, run, fmt.Errorf("só %d registros no arquivo (mínimo %d): arquivo truncado?", n, c.opt.MinRecords))
+		return nil, c.refuse(ctx, run, fmt.Errorf("só %d registros no arquivo (mínimo %d): arquivo truncado?", n, c.opt.MinRecords))
 	}
@@ -166,6 +177,10 @@
 		if errors.Is(err, store.ErrBusy) {
 			return nil, err
 		}
+		var rem *store.RemovalError
+		if errors.As(err, &rem) {
+			return nil, c.refuse(ctx, run, err)
+		}
 		return nil, c.fail(ctx, run, err)
 	}
+	c.refusedSHA, c.refusedVal = "", fetch.Validators{}
 	return &Result{Outcome: Applied, Version: version, Run: run, Changes: changes}, nil
 }
@@ -216,6 +231,13 @@
+// refuse grava uma recusa determinística e lembra o arquivo para não
+// reprocessá-lo a cada RETRY_INTERVAL.
+func (c *Collector) refuse(ctx context.Context, run store.Run, cause error) error {
+	c.refusedSHA = run.SHA256
+	c.refusedVal = fetch.Validators{ETag: run.ETag, LastModified: run.LastModified}
+	return c.fail(ctx, run, cause)
+}
+
 // fail grava a recusa em <fonte>_run e devolve a causa.
 func (c *Collector) fail(ctx context.Context, run store.Run, cause error) error {
```

## Critérios de aceite

- [ ] Depois de uma recusa por trava de remoção, a verificação seguinte com o mesmo conteúdo termina como "sem mudança" (`arquivo já recusado`), não grava nova linha em `<fonte>_run`, e o laço volta a esperar `SYNC_INTERVAL`.
- [ ] MD5 divergente e erro de banco continuam sendo repetidos a cada `RETRY_INTERVAL`.
- [ ] Um arquivo novo (outro SHA) é processado normalmente, e `--force` ignora a memória.
- [ ] A spec do padrão e as specs de cada fonte estão atualizadas.

## Testes recomendados

Testes em `internal/collector`, com store e fonte falsos:
- recusa por trava de remoção, seguida de uma segunda verificação com o mesmo conteúdo: o resultado é `Unchanged` e só existe uma linha de recusa;
- o mesmo arranjo com MD5 divergente: o resultado são duas recusas.

## Limitações e suposições

- Não medi o tamanho real dos arquivos nem a frequência de recusas determinísticas.
- A memória se perde quando o processo reinicia. Isso é aceitável: no máximo uma recusa a mais por reinício.
