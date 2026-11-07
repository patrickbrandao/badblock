# 004 — Conexão persistente com o proxy prende o IP de saída e anula a rotação

- **Severidade:** média
- **Confiança:** provável
- **Categoria:** ponto fraco
- **Subcategoria:** rede / proxy / limite do servidor
- **Status:** aberto — correção não aplicada
- **Arquivos afetados:** `apps/registrobr/rdapnum/collector/internal/rdap/rdap.go:94-105`, `specs/fontes/registrobr/rdapnum/collector.md:160-178`, `specs/fontes/registrobr/rdapnum/fonte.md:154-161`
- **Depende de / relacionado a:** pausa por truncadas e conferência da rotação (decisão #40)
- **Esforço estimado:** P (até ~1 h)
- **Auditoria:** 2026-11-07, commit `db43842`

## Explicação para leigos

O registro.br limita quantas consultas cada IP pode fazer por minuto, cerca de 8. O coletor faz 32 por minuto porque conta com um proxy que troca de IP.

O problema é que o coletor abre uma única "ligação" com o proxy e reaproveita essa ligação em todas as consultas. Para o proxy, tudo é uma ligação só, e ela sai sempre pelo mesmo IP. O registro.br vê então um único IP fazendo 32 consultas por minuto e começa a devolver respostas truncadas.

## Evidência e análise técnica

- `internal/rdap/rdap.go:94-105`: o `http.Transport` tem `KeepAlive: 30 * time.Second`, `ForceAttemptHTTP2: true`, `MaxIdleConns: 4` e `IdleConnTimeout: 90 * time.Second`, além de `tr.Proxy = http.ProxyURL(opt.Proxy)`. O `Client` é criado uma vez por instância e reaproveitado em todas as consultas.
- O alvo é `https://rdap.registro.br`. Por um proxy HTTP/HTTPS, o Go abre um túnel `CONNECT` e faz o TLS de ponta a ponta dentro dele (`collector.md:163-165`). Com SOCKS5 acontece o mesmo: uma conexão TCP por túnel. Com keep-alive, e com HTTP/2 multiplexando, as consultas seguintes passam pelo mesmo túnel.
- Na taxa de 32/min (`collector.md:182-185`), sai uma consulta a cada ~1,9 s. O túnel nunca fica 90 s parado, então o cliente nunca o fecha.
- Um proxy rotativo só escolhe o IP de saída quando abre a conexão de saída, ou seja, a cada CONNECT. Ele não tem como trocar o IP dentro de um túnel já aberto.
- Não há `DisableKeepAlives`, `CloseIdleConnections`, `req.Close` nem `Connection: close` no app (grep fora de `vendor/`).
- `fonte.md:156-158` diz que "o padrão de 32/min ... **pressupõe** o proxy que roda o IP". O balde do registro.br tem ~28 fichas e recarrega ~8/min (`fonte.md:146-154`).

## Verificação (tentativa de refutação)

1. A evidência existe como descrita (linhas 94-105 conferidas).
2. Há proteções em outro lugar, mas elas só detectam e amortecem o problema, sem evitá-lo:
   - o aviso `proxy sem rotação de IP` (`internal/scan/scan.go:282-285`);
   - a pausa depois de 3 truncadas (`scan.go:254-264`);
   - a espera por alvo.

   A resposta truncada nunca é processada (decisão #40), então o dado não se corrompe.
3. O problema é alcançável: é o caminho de toda consulta com `PROXY_URL` definida, que é a configuração de produção que a taxa padrão pressupõe.
4. A pré-condição é comum: um proxy rotativo que troca de IP por conexão ou por sessão. Não verifiquei se o registro.br fecha a conexão depois de cada resposta (por exemplo, com `Connection: close` ou GOAWAY). Se fechar, o túnel é refeito a cada consulta e o problema não aparece. Por isso a confiança é "provável".
5. A spec não trata do assunto. Ela diz só que "rodar o IP de saída é do proxy" (`collector.md:165`), sem exigir uma conexão por consulta. O aviso de "proxy sem rotação" leva o operador a culpar o proxy, quando o problema está no coletor.

## Impacto

Com o túnel preso, a instância faz 32 consultas por minuto por um único IP. Pelo ensaio de `fonte.md:146-154`, só cerca de 1 em cada 7 respostas volta completa. O resultado é:
- truncadas em série;
- pausas de 2 min;
- espera crescente por alvo, de 1 min até 6 h;
- uma volta completa muito mais lenta que as ~16 h previstas (`collector.md:194-196`).

Não há corrupção de dado: o dado bom anterior continua valendo.

## Solução proposta

Com `PROXY_URL` definida, abrir uma conexão nova por consulta: `DisableKeepAlives: true` e HTTP/2 sem forçar. O custo é um handshake TCP+TLS a cada ~1,9 s, desprezível. Sem proxy, o keep-alive pode continuar, porque o IP de saída é o mesmo de qualquer jeito.

A spec muda antes, pelo dono `collector-registrobr-rdapnum`:
- em `specs/fontes/registrobr/rdapnum/collector.md#proxy`, acrescentar: "com `PROXY_URL`, cada consulta abre um túnel novo (sem keep-alive nem HTTP/2), para que o proxy possa trocar o IP a cada `CONNECT`";
- opcionalmente, citar a regra em `fonte.md#limite-do-servidor`.

## Diff/patch proposto — NÃO APLICADO

```diff
--- a/apps/registrobr/rdapnum/collector/internal/rdap/rdap.go
+++ b/apps/registrobr/rdapnum/collector/internal/rdap/rdap.go
@@ -101,8 +101,14 @@ func New(opt Options) *Client {
 		TLSClientConfig:     opt.TLS,
 	}
 	if opt.Proxy != nil {
 		tr.Proxy = http.ProxyURL(opt.Proxy)
+		// Um túnel CONNECT novo por consulta: num túnel reaproveitado o proxy
+		// não consegue trocar o IP de saída (collector.md#proxy).
+		tr.DisableKeepAlives = true
+		tr.ForceAttemptHTTP2 = false
+		tr.MaxIdleConns = 0
 	}
 	hc := &http.Client{
 		Transport: tr,
```

## Critérios de aceite

- [ ] A spec `collector.md#proxy` diz que, com `PROXY_URL`, toda consulta abre um túnel novo.
- [ ] Com `PROXY_URL`, duas consultas seguidas abrem duas conexões com o proxy (dois `CONNECT`).
- [ ] Sem `PROXY_URL`, o comportamento atual (keep-alive) não muda.

## Testes recomendados

- Teste unitário com um proxy HTTP falso (`httptest`) que conta os `CONNECT`: N consultas devem abrir N túneis.
- `test-real` com um proxy rotativo: `_worker.egress_ips` maior que 1 nas últimas 100 respostas, sem nenhum aviso `proxy sem rotação de IP`.

## Limitações e suposições

A análise supõe que o proxy troca de IP por conexão ou por sessão nova e que o registro.br mantém a conexão aberta entre respostas. Nada disso foi testado na rede, porque a auditoria é estática.
