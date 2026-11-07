# 002 — PTR de zonas de terceiros gravados e servidos sem teto de quantidade ou de tamanho

- **Severidade:** média
- **Confiança:** provável
- **Categoria:** ponto fraco
- **Subcategoria:** validação de entrada externa / consumo de recursos
- **Status:** aberto — correção não aplicada
- **Arquivos afetados:** `apps/rdns/collector/internal/resolver/resolver.go:159-162,174-178,243-259`, `apps/rdns/collector/internal/store/store.go:251-263`, `database/postgres/rdns/20261010120000_rdns.sql:171-172,179`, `apps/rdns/api/internal/httpapi/types.go:236-256`, `apps/rdns/api/internal/httpapi/server.go:267-279`, `specs/fontes/rdns/fonte.md:80-100`
- **Depende de / relacionado a:** 003 (o `live=true` permite forçar a regravação na hora)
- **Esforço estimado:** P (até ~1 h)
- **Auditoria:** 2026-11-07, commit `db43842`

## Explicação para leigos

O coletor pergunta ao DNS "qual é o nome deste IP?" e guarda tudo o que vier. Quem decide a resposta é o dono da zona reversa: qualquer provedor ou cliente com bloco no Brasil. Ele pode responder milhares de nomes longos para cada IP. O coletor grava tudo, o banco cresce e a API passa a montar respostas enormes a cada pedido.

## Evidência e análise técnica

- `resolver.go:159-162`: uma resposta UDP truncada é repetida por TCP, que comporta mensagens de até 65.535 bytes.
- `resolver.go:243-259` (`ptrNames`): percorre `m.Answer`, normaliza os nomes, tira os repetidos e ordena. Não há limite de quantidade nem de tamanho. `Normalize` ainda transforma bytes não imprimíveis em `\DDD`, o que pode quadruplicar o tamanho.
- `store.go:253-255`: `INSERT INTO rdns_ptr (ip, status, name, names) VALUES (...)` grava `res.Names` inteiro.
- Migration `:171-172`: as colunas são `name text` e `names text[]`. O `chk_rdns_ptr_name` (`:179`) só exige `cardinality(names) >= 1`.
- API:
  - `types.go:247-252` copia `p.Names` para cada item do prefixo;
  - `server.go:268-279`: corpos acima de `maxCachedBody` (8 MiB) não entram no cache, então cada pedido refaz a consulta e a serialização.
- Ordem de grandeza:
  - com compressão de nomes, uma mensagem de 64 KiB carrega alguns milhares de PTR distintos;
  - descomprimido, cada um pode ter até ~250 bytes;
  - mesmo com um teto de 100 registros no resolvedor, são ~25 KB por IP, ou ~25 MB num /22.

## Verificação (tentativa de refutação)

- **Proteção no projeto:** nenhuma, nem no código, nem no CHECK, nem na API.
- **Proteção externa:** o resolvedor recursivo de `DNS_SERVERS` pode limitar quantos registros guarda por tipo. Isso depende do software e da versão que o operador usa, e a spec não exige.
- **Alcance:** a varredura de todo prefixo de `rdns_prefix` e o `live=true` da API (`live.go:180`).
- **Quem dispara:** o dono do reverso de qualquer IP coberto, inclusive delegações RFC 2317, que a spec segue de propósito (`fonte.md:80-84`).
- **Documentação:** `fonte.md:88-100` descreve a normalização e a ordem, mas não fala de teto. Não é um risco que a spec aceitou.

## Impacto

- `rdns_ptr` cresce sem limite. A spec estima o tamanho só para PTR comuns (`dados.md:270-274`).
- `/rdns/prefix` e `/rdns/ip` ficam com corpos enormes e fora do cache. Isso gasta CPU e memória da API e do Postgres a cada pedido, um vetor de DoS barato.
- Hipótese: com MBs por IP, uma transação de gravação pode passar do orçamento de 5 s. Nesse caso o prefixo perde o lease toda vez e nunca termina.

## Solução proposta

1. Definir na spec um teto de nomes por IP, por exemplo **16**, guardando os primeiros em ordem de bytes. Arquivos: `specs/fontes/rdns/fonte.md` (seção "Nomes") e `dados.md`. O tamanho de cada nome já é limitado a 255 bytes no formato DNS.
2. Aplicar o teto em `ptrNames`.
3. Como defesa extra, criar numa migration nova o CHECK `cardinality(names) <= 16`.

O dono é o sub-agente `collector-rdns`. Em `specs/fontes/rdns/api.md`, basta citar o teto.

## Diff/patch proposto — NÃO APLICADO

```diff
--- a/apps/rdns/collector/internal/resolver/resolver.go
+++ b/apps/rdns/collector/internal/resolver/resolver.go
@@ -240,6 +240,10 @@
+// MaxNames é o teto de nomes gravados por IP (os primeiros em ordem de
+// bytes): o dono da zona reversa decide quantos PTR responde.
+const MaxNames = 16
+
 // ptrNames devolve os PTR da seção de resposta, qualquer que seja o dono
 // (no RFC 2317 o PTR é do nome para onde o CNAME aponta), normalizados, sem
 // repetição e em ordem de bytes.
 func ptrNames(m *dns.Msg) []string {
@@ -255,5 +259,8 @@ func ptrNames(m *dns.Msg) []string {
 		}
 	}
 	sort.Strings(out)
+	if len(out) > MaxNames {
+		out = out[:MaxNames]
+	}
 	return out
 }
```

## Critérios de aceite

- [ ] A spec define o teto de nomes por IP.
- [ ] Uma resposta com mais de 16 PTR grava só os 16 primeiros em ordem de bytes.
- [ ] (Opcional) Um CHECK no banco recusa `cardinality(names) > 16`.

## Testes recomendados

- Teste de unidade em `resolver` com uma `dns.Msg` de 1.000 PTR.
- Teste com um resolvedor falso em `127.0.0.1` que devolve primeiro uma resposta TC e depois uma resposta TCP grande.

## Limitações e suposições

Não testei contra resolvedores reais (Unbound, BIND, PowerDNS) para ver quantos registros eles repassam. O pior caso depende disso.
