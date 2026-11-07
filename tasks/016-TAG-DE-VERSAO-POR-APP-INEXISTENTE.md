# 016 — Dois esquemas de versão: specs e Makefiles usam `<app>/vX.Y.Z`, o release usa `vX.Y.Z`

- **Severidade:** baixa
- **Confiança:** confirmado
- **Categoria:** incoerência
- **Subcategoria:** versionamento / release
- **Status:** aberto — correção não aplicada
- **Arquivos afetados:**
  - `apps/*/{collector,api}/Makefile` e `apps/*/*/{collector,api}/Makefile` (28 arquivos; ex.: `apps/lacnic/api/Makefile:16,64-65`)
  - `specs/projeto/estrutura.md:14,83`
  - `specs/projeto/convencoes.md:111-112`
  - `specs/plataforma/docker.md:231`
  - `specs/plataforma/publicacao.md:54-58,96-98`
  - `release-images.sh:7-10,59-62`
- **Depende de / relacionado a:** nenhum
- **Esforço estimado:** P (até ~1 h)
- **Auditoria:** 2026-11-07, commit `db43842`

## Explicação para leigos

O projeto marca as versões no git com etiquetas como `v0.0.4`, e o script oficial de publicação usa essas etiquetas. Já o comando de publicação de cada app (`make push`) e parte da documentação procuram outro formato, `api-lacnic/v0.0.4`, que nunca foi criado.

Resultado: o `make push` de um app sozinho sempre diz que não há versão e se recusa a publicar. E a documentação descreve um formato de versão que não existe.

## Evidência e análise técnica

- **Tags existentes** (`git --no-optional-locks tag`): `registry-api/v0.0.1`, `registry-sync/v0.0.1` e de `v0.0.1` a `v0.0.4`. Nenhuma tem o nome de um app do BadBlock.
- **Makefiles dos apps:**
  - `apps/lacnic/api/Makefile:16` procura a tag do app e, sem achar, cai em `dev`:
    `VERSION ?= $(shell git describe --tags --match '$(APP)/v*' --abbrev=0 2>/dev/null | sed 's|$(APP)/v||' | grep . || echo dev)`.
  - `apps/lacnic/api/Makefile:64-65`: o `push` se recusa a publicar quando `VERSION=dev`.
  - O mesmo padrão aparece nos 28 Makefiles.
- **Script de release:** `release-images.sh:61` usa a versão do projeto, igual para todos os apps: `git describe --tags --abbrev=0 --match 'v[0-9]*'`.
- **Specs que descrevem o esquema por app:**
  - `estrutura.md:14` diz que o `release-images.sh` publica "a partir das tags <app>/vX.Y.Z", o que é falso;
  - `estrutura.md:83`, `convencoes.md:111` e `docker.md:231` seguem o mesmo modelo por app.
- **Specs que descrevem o esquema do projeto:** `publicacao.md:54-58`.
- **Promessa que não se cumpre:** `publicacao.md:96-98` diz que cada app publica sozinho com `make -C apps/<fonte>/<tipo> push`.

## Verificação (tentativa de refutação)

1. **A evidência existe?** Sim.
2. **Há proteção?** Há uma saída: `make push VERSION=X.Y.Z`. E o caminho oficial, `release-images.sh`, funciona.
3. **É alcançável?** Sim, em qualquer `make image` ou `make push` de um app.
4. **As pré-condições são realistas?** Sim, é o uso documentado em `publicacao.md:97-98`.
5. **É intencional?** Não há decisão registrada para ter dois esquemas, e as specs se contradizem.

## Impacto

- O `make push` de um app sem `VERSION` sempre falha.
- O `make image` gera uma imagem `:dev`, com `--version` igual a `dev`.
- Quem seguir as specs pode criar tags `<app>/vX.Y.Z`, que o `release-images.sh` ignora.

## Solução proposta

Adotar o esquema que de fato existe, a tag do projeto `vX.Y.Z`:
- a sessão principal atualiza `estrutura.md`, `convencoes.md` e `docker.md`;
- o sub-agente de cada app aplica a mudança no seu Makefile.

A alternativa é registrar em `decisoes.md` que as tags por app continuam válidas para releases avulsos. Nesse caso, ainda é preciso corrigir `estrutura.md:14`.

## Diff/patch proposto — NÃO APLICADO

```diff
--- a/apps/lacnic/api/Makefile
+++ b/apps/lacnic/api/Makefile
@@ -15,3 +15,3 @@
 ROOT       := ../../..
-VERSION    ?= $(shell git describe --tags --match '$(APP)/v*' --abbrev=0 2>/dev/null | sed 's|$(APP)/v||' | grep . || echo dev)
+VERSION    ?= $(shell git describe --tags --match 'v[0-9]*' --abbrev=0 2>/dev/null | sed 's|^v||' | grep . || echo dev)
 COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
@@ -63,3 +63,3 @@
 push: ## Publica $(IMAGE):$(VERSION) e :latest, multi-arch (docker login antes)
-	@test "$(VERSION)" != dev || (echo "sem tag $(APP)/vX.Y.Z; use VERSION=X.Y.Z" && exit 1)
+	@test "$(VERSION)" != dev || (echo "sem tag vX.Y.Z do projeto; use VERSION=X.Y.Z" && exit 1)
--- a/specs/projeto/estrutura.md
+++ b/specs/projeto/estrutura.md
@@ -14 +14 @@
-├── release-images.sh       publica as imagens multi-arch a partir das tags <app>/vX.Y.Z
+├── release-images.sh       publica as imagens multi-arch a partir da tag do projeto vX.Y.Z
@@ -83 +83 @@
-| Tag de release | `<app>/vX.Y.Z` | `api-cgibr/v0.1.0` |
+| Tag de release | a do projeto, `vX.Y.Z` (igual para todos os apps) | `v0.0.4` |
```

O mesmo ajuste vale para `convencoes.md:111-112` e `docker.md:231`.

## Critérios de aceite

- [ ] Nenhuma spec cita `<app>/vX.Y.Z`, ou existe uma decisão registrada que explica os dois esquemas.
- [ ] No commit da tag, `make -C apps/lacnic/api -n push` resolve `VERSION=0.0.4`.
- [ ] Os 28 Makefiles usam o mesmo `--match`.

## Testes recomendados

- `make -C apps/<x> -n image`, em dry-run, para conferir a VERSION.
- `make specs-check`.

## Limitações e suposições

As tags `registry-*` parecem vir de outro projeto ou de uma versão antiga. Não as investiguei.
