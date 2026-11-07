# 017 — Specs de plataforma tratam o collector-rdns como a única sonda e omitem registrobr/rdapnum; o .env.example não documenta variáveis lidas pelos composes

- **Severidade:** baixa
- **Confiança:** confirmado
- **Categoria:** incoerência
- **Subcategoria:** documentação de plataforma desatualizada
- **Status:** aberto — correção não aplicada
- **Arquivos afetados:** `specs/plataforma/docker.md:105-108,209-210,241,273`, `specs/projeto/arquitetura.md:28-31`, `specs/plataforma/publicacao.md:70-71`, `.env.example:128-132,154-164,166-178,219-231`, `Makefile:32`, `apps/registrobr/rdapnum/collector/docker-compose.yml:12-15,29-31,41`, `apps/roothints/collector/docker-compose.yml:29`, `apps/rdns/collector/docker-compose.yml:40-41`, `apps/lacnic/api/docker-compose.yml:32`
- **Depende de / relacionado a:** nenhum
- **Esforço estimado:** P (até ~1 h)
- **Auditoria:** 2026-11-07, commit `db43842`

## Explicação para leigos

A fonte nova `registrobr/rdapnum` também é uma "sonda ativa" que roda em várias cópias, igual à `rdns`. As specs gerais, porém, ainda dizem que a `rdns` é a única desse tipo e deixam a fonte nova fora de listas como a de portas e a de imagens publicadas.

Além disso, o arquivo de exemplo de configuração não mostra algumas opções que os programas aceitam, como a lista de proxies confiáveis das APIs. Quem monta o ambiente seguindo só a documentação não fica sabendo que elas existem.

## Evidência e análise técnica

- **`specs/plataforma/docker.md:105-108`:** diz "O coletor de sonda ativa (`collector-rdns`) é a exceção: **sem `container_name`** e com `deploy.replicas`…". O compose do rdapnum (linhas 12-15 e 29-31) também é assim.
- **`docker.md:209-210`:** a ordem dos blocos termina em `rdns`, mas o `.env.example:166-178` já tem o bloco do rdapnum.
- **`docker.md:241`:** cita `STACK ?= badblock` só no `collector-rdns`. `apps/registrobr/rdapnum/collector/Makefile:27` também o define.
- **`docker.md:273`:** a lista do `make up` termina em `rdns:8113`, mas o `Makefile:32` já tem `registrobr/rdapnum:8114`.
- **`specs/projeto/arquitetura.md:28-31`:** cita só o `collector-rdns` como sonda.
- **`specs/plataforma/publicacao.md:70-71`:** fala em "28 imagens (26 apps de fontes, …)", mas o `release-images.sh:42-51` lista 30.
- **Variáveis lidas pelos composes e ausentes do `.env.example`:**
  - `COLLECTOR_ROOTHINTS_SOURCE_MD5_URL` (`apps/roothints/collector/docker-compose.yml:29`)
  - `COLLECTOR_RDNS_DERIVE_INTERVAL` e `COLLECTOR_RDNS_SOURCES` (`apps/rdns/collector/docker-compose.yml:40-41`)
  - `COLLECTOR_REGISTROBR_RDAPNUM_SOURCES` (`apps/registrobr/rdapnum/collector/docker-compose.yml:41`)
  - `API_TRUSTED_PROXIES`, nas 14 APIs (ex.: `apps/lacnic/api/docker-compose.yml:32`; citada em `specs/padroes/api.md:316`)

## Verificação (tentativa de refutação)

1. **A evidência existe?** Sim.
2. **Há proteção?** Os composes têm valores padrão, então nada quebra. As specs de cada fonte e `specs/padroes/sonda.md` descrevem as variáveis.
3. **Quem é afetado?** É documentação: atinge quem opera o projeto ou o reconstrói a partir das specs (`specs/processos/reconstrucao.md`).
4. **É intencional?** Não. A tabela de `docker.md:213-224` não promete ser exaustiva, mas `docker.md:205` diz que o `.env.example` "Documenta o stack inteiro".

## Impacto

- Quem reconstrói a partir das specs pode pôr `container_name` no rdapnum, o que quebra as réplicas, ou esquecer a porta 8114.
- Operadores não descobrem `API_TRUSTED_PROXIES`. Por padrão, as APIs aceitam cabeçalhos de IP do cliente vindos de qualquer faixa privada, o que pesa num host compartilhado com outros containers na rede `network_public`.

## Solução proposta

- **Specs (sessão principal):**
  - em `docker.md` e `arquitetura.md`, trocar "o coletor de sonda" por `collector-rdns` e `collector-registrobr-rdapnum`;
  - incluir `registrobr/rdapnum:8114` na lista do `up` e no fim da ordem dos blocos;
  - corrigir a contagem de imagens em `publicacao.md`.
- **`.env.example`:** acrescentar as cinco variáveis, comentadas e com seus padrões.

## Diff/patch proposto — NÃO APLICADO

```diff
--- a/specs/plataforma/docker.md
+++ b/specs/plataforma/docker.md
@@ -105,4 +105,5 @@
-O coletor de sonda ativa (`collector-rdns`) é a exceção: **sem
-`container_name`** e com `deploy.replicas: ${COLLECTOR_<FONTE>_REPLICAS:-1}`,
+Os coletores de sonda ativa (`collector-rdns`, `collector-registrobr-rdapnum`)
+são a exceção: **sem `container_name`** e com
+`deploy.replicas: ${COLLECTOR_<FONTE>_REPLICAS:-1}`,
--- a/specs/projeto/arquitetura.md
+++ b/specs/projeto/arquitetura.md
@@ -28,4 +28,5 @@
    ([../padroes/coletor.md](../padroes/coletor.md)). A exceção é o coletor
-   de **sonda ativa** (`collector-rdns`), que não tem arquivo: mede o dado
+   de **sonda ativa** (`collector-rdns`, `collector-registrobr-rdapnum`), que
+   não tem arquivo: mede o dado
--- a/specs/plataforma/publicacao.md
+++ b/specs/plataforma/publicacao.md
@@ -70,2 +70,2 @@
-1. `APPS` lista as 28 imagens (26 apps de fontes, `website-www` e
+1. `APPS` lista as 30 imagens (28 apps de fontes, `website-www` e
--- a/.env.example
+++ b/.env.example
@@ -231,2 +231,5 @@
 API_RATE_BURST=20
+# Proxies cujos cabeçalhos de IP do cliente valem (todas as APIs); o padrão são as
+# faixas privadas. No servidor, restrinja à sub-rede do Traefik.
+# API_TRUSTED_PROXIES=127.0.0.0/8,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,fc00::/7
```

O mesmo vale para:
- a lista do `up` e a ordem dos blocos em `docker.md:209-210,273`;
- as variáveis `COLLECTOR_ROOTHINTS_SOURCE_MD5_URL`, `COLLECTOR_RDNS_SOURCES`, `COLLECTOR_RDNS_DERIVE_INTERVAL` e `COLLECTOR_REGISTROBR_RDAPNUM_SOURCES` no `.env.example`.

O padrão de `SOURCE_MD5_URL` do roothints deve ser conferido pelo sub-agente `collector-roothints` em `specs/fontes/roothints/collector.md`.

## Critérios de aceite

- [ ] Nenhuma spec de projeto ou plataforma diz que o `collector-rdns` é a única sonda.
- [ ] A lista do `up` em `docker.md` bate com o `Makefile:32`.
- [ ] A contagem em `publicacao.md` bate com o `release-images.sh:42-51`.
- [ ] Toda variável `${...}` lida pelos composes aparece no `.env.example`, ou a spec diz explicitamente quais ficam de fora.

## Testes recomendados

- `make specs-check`.
- Um grep comparando as variáveis `${VAR` dos composes com as chaves do `.env.example`.

## Limitações e suposições

A spec não exige que o `.env.example` seja exaustivo. Por isso, a parte das variáveis é uma lacuna de documentação, não uma violação estrita.
