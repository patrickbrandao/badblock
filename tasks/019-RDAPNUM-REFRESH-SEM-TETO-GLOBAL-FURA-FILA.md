# 019 — POST de atualização do rdapnum sem teto global deixa poucos clientes travarem a varredura normal

- **Severidade:** baixa
- **Confiança:** provável
- **Categoria:** ponto fraco
- **Subcategoria:** disponibilidade / justiça da fila
- **Status:** aberto — correção não aplicada
- **Arquivos afetados:** `apps/registrobr/rdapnum/api/internal/refresh/refresh.go:93-125`, `apps/registrobr/rdapnum/api/internal/httpapi/handlers.go:390-424`, `apps/registrobr/rdapnum/api/internal/httpapi/server.go:193-229`, `apps/registrobr/rdapnum/api/docker-compose.yml:60-72`, `apps/registrobr/rdapnum/collector/internal/refresh/refresh.go:139-157`, `apps/registrobr/rdapnum/collector/internal/store/store.go:176-190,345-350`, `database/postgres/registrobr_rdapnum/20261021120000_registrobr_rdapnum.sql:622-623`
- **Depende de / relacionado a:** hipótese do rate limit do Traefik sem agregar IPv6 (ver resumo)
- **Esforço estimado:** M (até 1 dia)
- **Auditoria:** 2026-11-07, commit `db43842`

## Explicação para leigos

Qualquer pessoa pode pedir à API que "atualize agora" um ASN ou um bloco. Esse pedido passa na frente da fila de coleta. Existe um limite por endereço IP, mas não existe um limite total de pedidos por minuto.

Com alguns IPs, ou com uma página que faz os navegadores dos visitantes enviarem pedidos, dá para manter o coletor ocupado só com esses pedidos. A atualização normal dos outros milhares de alvos para.

O registro.br não recebe mais consultas por causa disso, porque o coletor continua na taxa configurada. O efeito é que os dados ficam velhos.

## Evidência e análise técnica

- **API** (`refresh.go:93-125`): só recusa o pedido em quatro casos:
  - o alvo foi coletado há menos de `REFRESH_MIN_AGE` (10 min);
  - já existe a marca inflight de 60 s para o mesmo alvo;
  - a fila do Valkey passou de `REFRESH_QUEUE_MAX` (1000);
  - o Valkey caiu.

  Nada conta o total de pedidos por período.
- **Coletor** (`collector/internal/refresh/refresh.go:139-157`, `store.go:345-350`): consome a fila com BLPOP e só faz `UPDATE ... SET rescan = true`. A fila esvazia na hora, então o teto de 1000 não limita quantos alvos ficam com `rescan` marcado no banco.
- **Ordem de coleta:** o `Claim` (`store.go:176-190`) escolhe por `ORDER BY rescan DESC, last_scan_at ASC NULLS FIRST`, apoiado no índice `ix_registrobr_rdapnum_target_queue` (`...sql:623`). Enquanto houver algum alvo com `rescan`, a varredura normal não anda.
- **Conta da taxa:** o coletor faz por padrão 32 consultas/min. O router do POST deixa 6 pedidos/min por IP (`docker-compose.yml:70-72`). Com cerca de 6 IPs pedindo alvos diferentes, a capacidade inteira do coletor vai para esses pedidos.
- **Pedidos vindos de páginas de terceiros:** o CORS `*` (`server.go:200`) e o corpo ignorado (`api.md:81`) deixam um POST simples vindo de qualquer página funcionar. Assim os navegadores dos visitantes viram IPs extras. Um POST simples dispensa o preflight mesmo com CORS fechado, então o CORS amplia o problema, mas não é a causa.

## Verificação (tentativa de refutação)

- **Aumenta o tráfego para o registro.br?** Não. O limitador fixa a taxa (`collector.md:182`), e a spec diz "coleta na taxa normal" (`collector.md:243-245`, decisão #42). Essa parte do achado original foi refutada.
- **Há proteção em outro lugar?** Só por alvo (REFRESH_MIN_AGE e inflight), o teto da fila (que esvazia na hora) e o rate limit por IP. Não há limite global.
- **É alcançável?** Sim. O POST é público e não exige chave (decisão #42).
- **É intencional?** A spec aceita uma atualização "sem prazo garantido", mas não discute a vazão total dos pedidos.

## Impacto

A atualização normal (ciclo de 168 h) fica parada enquanto durar o abuso, e `overdue` no `/meta` só cresce. Nada vaza e a carga sobre o registro.br não aumenta. O operador consegue ver o problema no `/meta` (`targets.rescan`).

## Solução proposta

Pôr um teto global de pedidos aceitos por minuto, por exemplo `REFRESH_GLOBAL_MAX` = 8/min, abaixo da taxa do coletor:
- contar no Valkey com INCR e EXPIRE, numa janela fixa;
- passou do teto: responder 202 com `queued: false, reason: "busy"`.

Complemento opcional no coletor: alternar um rescan a cada N coletas, para que a fila normal nunca pare.

Para fechar a origem via navegador: exigir `Content-Type: application/json`, o que força o preflight, e usar um `CORS_ORIGIN` restrito para o POST.

Specs a atualizar primeiro:
- `specs/fontes/registrobr/rdapnum/api.md`;
- `api-rotas.md`;
- o manifesto OpenAPI;
- `decisoes.md` (complemento à #42).

Dono: `api-registrobr-rdapnum`. A alternância no `Claim` é do `collector-registrobr-rdapnum`.

## Diff/patch proposto — NÃO APLICADO

```diff
--- a/apps/registrobr/rdapnum/api/internal/refresh/refresh.go
+++ b/apps/registrobr/rdapnum/api/internal/refresh/refresh.go
@@
 	// InflightPrefix + "<kind>:<alvo>" marca o pedido que já está na fila.
 	InflightPrefix = "badblock:registrobr-rdapnum:refresh:inflight:"
+	// BudgetPrefix + "<minuto unix>" conta os pedidos aceitos no minuto.
+	BudgetPrefix = "badblock:registrobr-rdapnum:refresh:budget:"
@@
 	Recent        = "recent"
 	AlreadyQueued = "already_queued"
+	Busy          = "busy" // teto global do minuto (REFRESH_GLOBAL_MAX)
 )
@@
 	QueueLen(ctx context.Context) (int64, error)
 	Push(ctx context.Context, req Request) error
+	Spend(ctx context.Context, window string) (int64, error) // INCR + EXPIRE 120
 }
@@
 	if !claimed {
 		return Outcome{Reason: AlreadyQueued}, nil
 	}
 	// Daqui em diante a marca é deste pedido: falhou, apaga.
 	ctx = context.WithoutCancel(ctx)
+	used, err := r.b.Spend(ctx, strconv.FormatInt(r.now().Unix()/60, 10))
+	if err != nil {
+		_ = r.b.Release(ctx, mark)
+		return Outcome{}, unavailable(err)
+	}
+	if used > r.perMin {
+		_ = r.b.Release(ctx, mark)
+		return Outcome{Reason: Busy}, nil
+	}
 	n, err := r.b.QueueLen(ctx)
```

Também é preciso:
- `NewRequester` receber `perMin`;
- o backend Valkey implementar `Spend`;
- `config.go` ganhar `RefreshGlobalMax`, com padrão 8.

## Critérios de aceite

- [ ] A spec e o OpenAPI descrevem o teto global e o motivo `busy`.
- [ ] Com o teto em N/min, o pedido N+1 do mesmo minuto não entra na fila, e a marca inflight dele é apagada.
- [ ] Com pedidos no limite, a varredura normal continua andando.

## Testes recomendados

- Unitário em `refresh`, com backend falso:
  - janela cheia → `Busy`, com Release chamado;
  - erro no Spend → `ErrUnavailable`.
- Em `httpapi`: 202 com `reason: "busy"`.

## Limitações e suposições

O número de IPs necessário foi estimado com os valores padrão. A taxa real de produção está em `registrobr_rdapnum_config`, que não consultei.
