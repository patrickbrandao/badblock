# 001 — /rootzone/tld/{tld}: punycode roda antes do limite de 63 caracteres e permite DoS de CPU

- **Severidade:** média
- **Confiança:** provável
- **Categoria:** segurança
- **Subcategoria:** negação de serviço por complexidade algorítmica
- **Status:** aberto — correção não aplicada
- **Arquivos afetados:** `apps/rootzone/api/internal/tldname/tldname.go:29-53`, `apps/rootzone/api/internal/tldname/punycode.go:25-86`, `apps/rootzone/api/internal/httpapi/handlers_tld.go:38-45`, `apps/rootzone/api/cmd/api-rootzone/main.go:116-123`, `specs/fontes/rootzone/api.md:117-140`
- **Depende de / relacionado a:** nenhum (o rate limit do Traefik por IP, `specs/plataforma/publicacao.md:37`, só atenua)
- **Esforço estimado:** P (até ~1 h)
- **Auditoria:** 2026-11-07, commit `db43842`

## Explicação para leigos

A rota que consulta um domínio de topo (como `.br` ou `.рф`) converte nomes com acentos ou de outros alfabetos para a forma técnica `xn--...`. A conferência de que o nome tem no máximo 63 caracteres, como o DNS exige, só acontece depois dessa conversão. E a conversão fica muito mais lenta quando o nome é longo e cheio de caracteres diferentes.

Alguém pode mandar um "nome" com centenas de milhares de caracteres chineses distintos. Cada pedido desses prende o processador por segundos. Uma rajada pequena deixa lenta ou parada a API e tudo o que dividir a mesma máquina.

## Evidência e análise técnica

- `Normalize` (`tldname.go:29-53`):
  1. `utf8.ValidString` e a checagem de categoria Unicode (linhas 31-41) custam O(L).
  2. Depois vem `Encode(strings.ToLower(name))` (linha 42).
  3. Só no fim vem `validLabel`, com o limite de 63 bytes (linhas 49 e 64-76).
- `Encode` (`punycode.go:25-86`):
  - o laço `for h < len(input)` (linha 42) processa um ponto de código distinto por volta;
  - cada volta percorre `input` inteiro duas vezes: para achar o mínimo (linhas 44-48) e para contar e emitir (linhas 54-81);
  - o custo é Θ(D·L), em que D é o número de pontos de código não-ASCII distintos e L é o total de runas.
- O `delta` volta a zero a cada emissão (linha 79), e a diferença entre pontos de código consecutivos é pequena. Por isso as checagens de estouro (linhas 49 e 56) não interrompem uma entrada de ideogramas consecutivos.
- `handleTLD` (`handlers_tld.go:38-45`) chama `Normalize` antes de `serveCached`, sem cache e sem prazo. O middleware (`server.go:146-180`) não limita o tamanho do caminho.
- O `http.Server` (`main.go:116-123`) não define `MaxHeaderBytes`. Vale o padrão do Go, 1 MiB para a linha de requisição mais os cabeçalhos.
- Estimativa (não medida):
  - Em percent-encoding, 1 MiB carrega cerca de 349 KB de UTF-8, ou seja, cerca de 116 mil runas de 3 bytes.
  - D pode chegar a ~40 mil: CJK unificado (~21 mil), extensão A (~6,6 mil) e Hangul (~11 mil), todos em `unicode.L`.
  - Custo: 2 × 40.000 × 116.000 ≈ 9,3×10^9 iterações, segundos de CPU por requisição.
  - Com UTF-8 cru na linha de requisição, L chega a ~349 mil e o custo fica cerca de 3 vezes maior.

## Verificação (tentativa de refutação)

- **Proteção em outro lugar?** Não há no código.
  - O rate limit do Traefik (5 req/s, rajada 20, por IP) limita a taxa, não o custo. Só a rajada de 20 requisições já soma dezenas a centenas de segundos de CPU.
  - O `ReadTimeout` de 15 s cobre só a leitura. O handler continua rodando depois disso.
  - `Encode` não consulta o contexto.
  - O limite do Traefik de produção fica fora do repositório. Se for o padrão, também é ~1 MiB.
- **Alcançável?** Sim, pela rota pública `GET /rootzone/tld/{tld}` e por `/rootzone/v1/tld/{tld}`.
- **Pré-condições?** Basta um cliente HTTP comum, sem nenhum privilégio.
- **Intencional?** A spec (`specs/fontes/rootzone/api.md:117-140`) exige o limite de 63 caracteres "também depois de codificar", mas não manda conferir o tamanho antes. O teste `tldname_test.go:89` só cobre 60 runas.

## Impacto

Esgota a CPU do container `api-rootzone` e de quem divide os núcleos do host: as outras APIs, o Postgres e o Valkey no mesmo servidor. Um único IP basta, com uma rajada de 20 requisições de ~1 MiB e depois 5 por segundo.

## Solução proposta

1. **Spec** (`specs/fontes/rootzone/api.md`, sub-agente `api-rootzone`): na regra do nome Unicode, dizer que um nome com mais de 59 runas (63 − len("xn--")) é recusado com 400 **antes** do punycode. O punycode nunca encurta o nome, porque cada runa gera ao menos um caractere.
2. **Código** (`api-rootzone`):
   - em `Normalize`, recusar cedo nomes com `len(name) > 4*MaxLabel` bytes;
   - para nomes não-ASCII, recusar também `utf8.RuneCountInString(name) > MaxLabel-4`.
3. **Opcional:** um `MaxHeaderBytes` menor (ex.: 16 KiB) no `http.Server`, como padrão de todas as APIs (`specs/padroes/api.md`).

## Diff/patch proposto — NÃO APLICADO

```diff
--- a/apps/rootzone/api/internal/tldname/tldname.go
+++ b/apps/rootzone/api/internal/tldname/tldname.go
@@ -28,10 +28,19 @@
 // prefixo "xn--"; o resultado passa pela mesma regra do rótulo ASCII.
 func Normalize(raw string) (string, error) {
 	name := strings.TrimSuffix(raw, ".")
-	if name == "" || !utf8.ValidString(name) {
+	// Nenhum rótulo válido passa de 63 runas de até 4 bytes: corta cedo,
+	// antes de qualquer trabalho proporcional ao tamanho da entrada.
+	if name == "" || len(name) > 4*MaxLabel || !utf8.ValidString(name) {
 		return "", ErrInvalid
 	}
 	if !isASCII(name) {
+		// O punycode nunca encurta (cada runa vira ao menos um caractere):
+		// mais de 59 runas não cabe em "xn--" + 59. Conferir antes de Encode,
+		// que custa O(distintos × tamanho).
+		if utf8.RuneCountInString(name) > MaxLabel-len("xn--") {
+			return "", ErrInvalid
+		}
 		// Fora do ASCII, só letras, marcas e dígitos (o ASCII é conferido
 		// depois, pela regra do rótulo).
 		if strings.ContainsFunc(name, func(r rune) bool {
```

## Critérios de aceite

- [ ] `Normalize` recusa, sem chamar `Encode`, nomes com mais de 252 bytes e nomes não-ASCII com mais de 59 runas.
- [ ] Os 151 IDN da zona continuam normalizando como antes (`specs/fontes/rootzone/api.md:139-140`).
- [ ] A spec `specs/fontes/rootzone/api.md` descreve a checagem antecipada.

## Testes recomendados

- Em `tldname_test.go`:
  - `strings.Repeat("ф", 60)` continua dando 400;
  - uma entrada com ~300 mil runas CJK distintas devolve `ErrInvalid` em menos de 1 ms (benchmark).
- Teste HTTP: `GET /rootzone/tld/<~1 MiB percent-encoded>` responde 400 rápido.

## Limitações e suposições

- O tempo por requisição foi estimado pela contagem de iterações. Nada foi executado.
- Não verifiquei a configuração global do Traefik de produção (limite de cabeçalho, repasse de UTF-8 cru) nem quantos núcleos o servidor tem.
