# 010 — Versão do dataset e dados lidos fora do mesmo snapshot nas APIs

- **Severidade:** baixa
- **Confiança:** confirmado
- **Categoria:** incoerência
- **Subcategoria:** consistência de leitura / rótulo de versão e cache
- **Status:** aberto — correção não aplicada
- **Arquivos afetados:**
  - `apps/lacnic/api/internal/httpapi/server.go:245-288`
  - `apps/lacnic/api/internal/dataset/dataset.go:47-82`
  - `apps/cgibr/api/internal/store/store.go:134-158`
  - `apps/lacnic/api/internal/store/store.go:210-274`, e o mesmo nos clones de RIR (afrinic, apnic, arin, ripencc)
  - `serveCached` em `apps/{afrinic,apnic,arin,cgibr,iana,lacnic,ripencc,rootanchors,roothints,rootzone}/api/internal/httpapi/server.go`, `apps/ripe/asnames/api/internal/httpapi/server.go` e `apps/anatel/pst/api/internal/httpapi/server.go`
  - `specs/padroes/api.md:105-142`
  - `specs/plataforma/valkey.md:56-61`
- **Depende de / relacionado a:** 018 (cache Valkey compartilhado)
- **Esforço estimado:** M (até 1 dia)
- **Auditoria:** 2026-11-07, commit `db43842`

## Explicação para leigos

Cada resposta da API diz de qual "edição" dos dados ela saiu: a versão do dataset. A API só relê essa versão a cada 30 segundos, mas os dados no banco mudam na hora em que o coletor termina. Nesses até 30 segundos, a API entrega dados da edição nova com a etiqueta da edição antiga e guarda essa resposta no cache sob a etiqueta errada.

Algumas rotas também montam a resposta com duas ou três perguntas separadas ao banco. Se o coletor gravar entre uma pergunta e outra, a resposta mistura as duas edições.

## Evidência e análise técnica

- **O coletor troca dados e versão de uma vez só:** `Apply` faz os MERGE em `lacnic_asn` e `lacnic_prefix`, depois `insertRun` e `tx.Commit` (`apps/lacnic/collector/internal/store/store.go:180-258`).
- **A API usa a versão guardada em memória:** `snap := a.data.Current()` (`server.go:249`). O watcher só a relê a cada `DATASET_POLL` (`dataset.go:67-82`).
- **No cache MISS, a versão e os dados se descolam:** `build(ctx, snap)` consulta o banco já atualizado, e o corpo é gravado em `CacheKey(snap.Version, key)` (`server.go:261, 266-279`) com o ETag `etagFor(snap.Version, key)` (`server.go:255`).
- **Resultado:** durante a janela, a mesma chave de cache e o mesmo ETag passam a ter dois corpos possíveis, um antigo (HIT gravado antes do commit) e um novo (MISS depois do commit). Os dois saem com o mesmo `dataset.version`.
- O padrão aparece nas 12 APIs com dataset versionado: afrinic, anatel/pst, apnic, arin, cgibr, iana, lacnic, ripe/asnames, ripencc, rootanchors, roothints e rootzone.
- **Subcaso: leitura não atômica.**
  - `cgibr.Store.ASN` (`apps/cgibr/api/internal/store/store.go:135-158`) lê `cgibr_asn` e depois `cgibr_prefix` em duas consultas separadas no pool. O comentário de `ByDocument` (`store.go:192-195`) diz que isso precisa ser uma consulta só, para "que ASNs e blocos venham do mesmo arquivo aplicado".
  - `lacnic.Store.Holder` (`apps/lacnic/api/internal/store/store.go:222-272`) faz três consultas. No ripencc o `opaque_id` muda a cada arquivo diário (`specs/fontes/ripencc/dados.md:21, 47-60`). Assim, a primeira consulta pode achar o titular e as duas seguintes, feitas já depois do commit, voltarem vazias. A resposta é 200 com listas vazias, quando o contrato pede 404, e vai para o cache.
  - A api-iana (`store.go:197-199`), a rootzone (`store.go:188`) e a anatel/pst (`store.go:200`) já leem em `REPEATABLE READ`.

## Verificação (tentativa de refutação)

- **Proteção em outro lugar?** Não. O coletor não avisa o watcher quando aplica um arquivo, e `build` não confere a versão no banco.
- **Alcançável?** Sim. Toda rota de dados passa por `serveCached`, e os coletores aplicam arquivos todo dia.
- **Pré-condição:** uma requisição com cache MISS até 30 s depois do commit. Isso acontece em toda aplicação de arquivo, sem atacante nenhum. No subcaso, a janela é de milissegundos.
- **Documentado?** Só em parte:
  - `api.md:119-121` aceita que a versão atrase;
  - mas `api.md:105-107` promete "a versão usada na resposta", e `valkey.md:58-61` promete "dado novo nunca serve resposta velha";
  - o rótulo trocado e o mesmo ETag para corpos diferentes não aparecem em lugar nenhum. Por isso a categoria é incoerência.
- **Duração:** depois que o watcher vira, as chaves antigas deixam de ser lidas e expiram.

## Impacto

- Até `DATASET_POLL` (30 s) por aplicação, as respostas novas saem com rótulo, ETag e `X-Dataset-Version` antigos. Um cliente que usa `dataset.version` para saber se algo mudou guarda a versão errada.
- Na janela, um `If-None-Match` com o ETag antigo recebe 304, mesmo com o corpo daquela chave já sendo outro.
- No subcaso, raro: um holder do ripencc com listas vazias, ou um ASN do cgibr sem blocos, cacheado por até 30 s.
- Não há impacto de segurança.

## Solução proposta

1. **Spec primeiro.** Em `specs/padroes/api.md` (dono: sessão principal), replicando nas `specs/fontes/<fonte>/api.md`:
   - a versão do bloco `dataset` passa a ser lida na mesma transação `REPEATABLE READ READ ONLY` dos dados;
   - se ela for diferente da versão do watcher, a resposta usa a versão lida no banco (com a chave de cache e o ETag dela) e o watcher é relido na hora;
   - alternativa mais barata: nesse caso, a resposta simplesmente não vai para o cache.
2. **Consultas com mais de um SELECT** (cgibr `ASN` e `Holder` dos cinco RIRs) passam a rodar numa transação `REPEATABLE READ` só de leitura, como a api-iana já faz em `readTx`.
3. **Corrigir `specs/plataforma/valkey.md:58-61`** para descrever o comportamento real.

Apps envolvidos: `api-cgibr`, `api-lacnic` (o modelo) e os clones de RIR; o `serveCached` das 12 APIs.

## Diff/patch proposto — NÃO APLICADO

```diff
--- a/apps/cgibr/api/internal/store/store.go
+++ b/apps/cgibr/api/internal/store/store.go
@@ -134,25 +134,33 @@
 // ASN devolve um ASN com seus blocos (IPv4 primeiro, em ordem de endereço).
+// As duas consultas rodam num snapshot só (REPEATABLE READ), para que ASN e
+// blocos venham do mesmo arquivo aplicado, como em ByDocument.
 func (s *Store) ASN(ctx context.Context, asn int64) (*ASN, error) {
 	var a ASN
-	var id string
-	err := s.pool.QueryRow(ctx, `
-		SELECT uuid::text, asn, name, document, document_digits, created_at, updated_at
-		  FROM cgibr_asn WHERE asn = $1`, asn).
-		Scan(&id, &a.ASN, &a.Name, &a.Document, &a.DocumentDigits, &a.CreatedAt, &a.UpdatedAt)
-	if errors.Is(err, pgx.ErrNoRows) {
-		return nil, ErrNotFound
-	}
-	if err != nil {
-		return nil, err
-	}
-	rows, err := s.pool.Query(ctx, `
-		SELECT prefix FROM cgibr_prefix WHERE asn_uuid = $1::uuid ORDER BY family, prefix`, id)
-	if err != nil {
-		return nil, err
-	}
-	a.Prefixes, err = pgx.CollectRows(rows, pgx.RowTo[netip.Prefix])
+	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly},
+		func(tx pgx.Tx) error {
+			var id string
+			err := tx.QueryRow(ctx, `
+				SELECT uuid::text, asn, name, document, document_digits, created_at, updated_at
+				  FROM cgibr_asn WHERE asn = $1`, asn).
+				Scan(&id, &a.ASN, &a.Name, &a.Document, &a.DocumentDigits, &a.CreatedAt, &a.UpdatedAt)
+			if errors.Is(err, pgx.ErrNoRows) {
+				return ErrNotFound
+			}
+			if err != nil {
+				return err
+			}
+			rows, err := tx.Query(ctx, `
+				SELECT prefix FROM cgibr_prefix WHERE asn_uuid = $1::uuid ORDER BY family, prefix`, id)
+			if err != nil {
+				return err
+			}
+			a.Prefixes, err = pgx.CollectRows(rows, pgx.RowTo[netip.Prefix])
+			return err
+		})
 	if err != nil {
 		return nil, err
 	}
 	return &a, nil
 }
```

A mesma mudança vale para `Holder` nos cinco RIRs. Ler a versão dentro da transação de `build` muda a assinatura de `serveCached`, que é comum a todas as APIs; por isso essa parte deve ser desenhada primeiro na spec.

## Critérios de aceite

- [ ] `specs/padroes/api.md` e `specs/plataforma/valkey.md` dizem como versão e dados ficam coerentes, ou declaram explicitamente que o rótulo atrasado é aceito.
- [ ] `ASN` do cgibr e `Holder` dos RIRs rodam numa transação `REPEATABLE READ READ ONLY`.
- [ ] Um MISS logo depois de um commit do coletor não grava corpo novo sob a chave da versão antiga.

## Testes recomendados

- Integração: aplicar um arquivo com o watcher parado e consultar uma rota. `dataset.version` e o corpo devem ser da mesma versão.
- Integração de `Holder`/`ASN` com escrita concorrente (`MERGE` em laço): nunca devolver um titular encontrado com listas vazias.

## Limitações e suposições

- Não medi com que frequência um MISS cai na janela.
- Os clones de RIR foram conferidos pelo grep de `serveCached`, não linha a linha.
