# 015 — O allowlist do gitleaks não cobre vendor/ e testdata/ das fontes de dois níveis

- **Severidade:** baixa
- **Confiança:** confirmado
- **Categoria:** incoerência
- **Subcategoria:** configuração de segurança × spec
- **Status:** aberto — correção não aplicada
- **Arquivos afetados:** `.gitleaks.toml:12-15`, `specs/plataforma/seguranca.md:37-38`
- **Depende de / relacionado a:** nenhum
- **Esforço estimado:** P (até ~1 h)
- **Auditoria:** 2026-11-07, commit `db43842`

## Explicação para leigos

O gitleaks procura senhas esquecidas no código. Ele tem uma lista de pastas que pode ignorar, como código de terceiros e amostras de dados públicos.

A regra dessa lista foi escrita para caminhos como `apps/lacnic/api/vendor/`. Ela não reconhece `apps/ripe/asnames/api/vendor/`, que tem um nível a mais. Por isso, as três fontes "de dois níveis" são varridas inteiras, ao contrário do que a documentação diz. Isso pode gerar alarmes falsos que travam a verificação antes de um PR.

## Evidência e análise técnica

As regras atuais em `.gitleaks.toml`:

```
12	  # Recortes dos arquivos públicos das fontes.
13	  '''apps/[^/]+/[^/]+/testdata/''',
14	  # Dependências Go vendorizadas: código de terceiros.
15	  '''apps/[^/]+/[^/]+/vendor/''',
```

- `apps/lacnic/api/vendor/x` casa com a regra.
- `apps/ripe/asnames/api/vendor/x` não casa: o segundo `[^/]+` pega `asnames`, e o que vem depois é `/api/`, não `/vendor/`.
- Pastas que ficam de fora, todas versionadas:
  - `apps/ripe/asnames/{api,collector}/vendor` e `apps/ripe/asnames/collector/testdata`
  - `apps/anatel/pst/{api,collector}/vendor` e `apps/anatel/pst/collector/testdata`
  - `apps/registrobr/rdapnum/{api,collector}/vendor` e `apps/registrobr/rdapnum/collector/testdata`
- A spec (`specs/plataforma/seguranca.md:37-38`) diz que libera `apps/<fonte>/<tipo>/testdata/` e `apps/<fonte>/<tipo>/vendor/`, e `<fonte>` inclui as fontes de dois níveis (`specs/projeto/estrutura.md`).

## Verificação (tentativa de refutação)

1. **A evidência existe?** Sim.
2. **Há proteção em outro lugar?** Não. Não há `.gitleaksignore` nem outro allowlist.
3. **É alcançável?** Sim. `make gitleaks` faz parte das verificações antes de um PR (`specs/processos/testes.md`).
4. **A pré-condição é realista?** Basta uma string nesses vendor/ ou testdata/ que case com alguma regra padrão do gitleaks. Não rodei o gitleaks para ver um caso concreto.
5. **É intencional?** Não. A spec diz o contrário.

## Impacto

Falsos positivos em `make gitleaks`. Isso trava o PR ou leva a exceções feitas às pressas. Não há vazamento: a varredura fica mais ampla, não mais estreita.

## Solução proposta

Aceitar na regex um nível opcional de conjunto, e deixar explícito na spec que `<fonte>` pode ser `<site>/<conjunto>`. Dono: a sessão principal (raiz e segurança).

## Diff/patch proposto — NÃO APLICADO

```diff
--- a/.gitleaks.toml
+++ b/.gitleaks.toml
@@ -11,8 +11,9 @@
 paths = [
-  # Recortes dos arquivos públicos das fontes.
-  '''apps/[^/]+/[^/]+/testdata/''',
-  # Dependências Go vendorizadas: código de terceiros.
-  '''apps/[^/]+/[^/]+/vendor/''',
+  # Recortes dos arquivos públicos das fontes (também <site>/<conjunto>).
+  '''apps/[^/]+/([^/]+/)?(collector|api)/testdata/''',
+  # Dependências Go vendorizadas: código de terceiros (também <site>/<conjunto>).
+  '''apps/[^/]+/([^/]+/)?(collector|api)/vendor/''',
--- a/specs/plataforma/seguranca.md
+++ b/specs/plataforma/seguranca.md
@@ -36,4 +36,4 @@
 |---|---|
-| `apps/<fonte>/<tipo>/testdata/` | recortes dos arquivos públicos das fontes |
+| `apps/<fonte>/<tipo>/testdata/` (`<fonte>` também `<site>/<conjunto>`) | recortes dos arquivos públicos das fontes |
```

## Critérios de aceite

- [ ] As regexes casam com `apps/lacnic/api/vendor/`, `apps/ripe/asnames/api/vendor/`, `apps/anatel/pst/collector/testdata/` e `apps/registrobr/rdapnum/collector/testdata/`.
- [ ] As regexes não casam com `apps/<fonte>/<tipo>/internal/...`.
- [ ] `make gitleaks` passa sem achados nesses caminhos.

## Testes recomendados

Rodar `make gitleaks` antes e depois da mudança e comparar os achados.

## Limitações e suposições

- Não rodei o gitleaks, então não sei se hoje já existe falso positivo.
- Supus que o gitleaks aplica `paths` como regex sem âncora sobre o caminho do arquivo.
