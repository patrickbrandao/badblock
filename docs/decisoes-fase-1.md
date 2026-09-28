# Decisões da fase 1 — BadBlock

Registro da sessão de desenho de 2026-09-28. É a fonte da verdade para a
implementação da fase 1. Mudanças posteriores entram como novas seções datadas
no fim deste arquivo, sem reescrever o histórico.

## Escopo

Entra na fase 1:

- Banco PostgreSQL com todos os ASNs e prefixos IP do mundo, na hierarquia
  IANA → RIR → NIC.br.
- `registry-sync`: importa as fontes públicas e sincroniza o banco (cadastra,
  atualiza, remove).
- `registry-api`: API HTTP de consulta, publicada em HTTPS pelo Traefik, com
  suporte a `X-Forwarded-For` e `X-Real-IP`.
- Ambiente local em Docker e deploy em servidor único com imagens do Docker Hub.

Fica fora da fase 1: dados de roteamento BGP (quem anuncia cada prefixo),
consulta RDAP por recurso, geolocalização, consulta em lote (bulk), chaves de
API, métricas Prometheus e alertas push.

## Fontes

| Fonte | URL primária |
|---|---|
| AFRINIC delegated-extended | https://ftp.afrinic.net/pub/stats/afrinic/delegated-afrinic-extended-latest |
| APNIC delegated-extended | https://ftp.apnic.net/stats/apnic/delegated-apnic-extended-latest |
| ARIN delegated-extended | https://ftp.arin.net/pub/stats/arin/delegated-arin-extended-latest |
| LACNIC delegated-extended | https://ftp.lacnic.net/pub/stats/lacnic/delegated-lacnic-extended-latest |
| RIPE NCC delegated-extended | https://ftp.ripe.net/pub/stats/ripencc/delegated-ripencc-extended-latest |
| IANA RDAP bootstrap | https://data.iana.org/rdap/asn.json, ipv4.json, ipv6.json |
| IANA registros de topo | as-numbers-1.csv, as-numbers-2.csv, ipv4-address-space.csv, ipv6-unicast-address-assignments.csv |
| IANA special-purpose | iana-ipv4-special-registry-1.csv, iana-ipv6-special-registry-1.csv, special-purpose AS numbers |
| NIC.br (ASN → blocos, BR) | https://ftp.registro.br/pub/numeracao/origin/nicbr-asn-blk-latest.txt |
| Nomes de AS (mundo) | https://ftp.ripe.net/ripe/asnames/asn.txt |

Os espelhos da LACNIC para os outros RIRs (`ftp.lacnic.net/pub/stats/<rir>/`)
são usados só como fallback: em 2026-09-28 o espelho do APNIC estava um dia
atrasado em relação ao `ftp.apnic.net`.

## Fatos medidos nas fontes (2026-09-28)

- Um ciclo completo tem ~46 MB e ~780 mil linhas. Todas as URLs respondem com
  `ETag` e `Last-Modified`.
- São ~140 mil ASNs depois de expandir as faixas, ~270 mil registros IPv4,
  ~335 mil registros IPv6 e ~128 mil titulares (`opaque-id`) distintos.
- Nenhum recurso aparece em mais de um RIR: `(tipo, início)` é chave natural
  global.
- 3.787 registros IPv4 não são CIDR (contagem que não é potência de 2 ou bloco
  desalinhado) e precisam ser divididos na camada normalizada.
- 253 delegações IPv4 são menores que /24 (a menor é /29) e 21 delegações IPv6
  são maiores que /48 (até /64).
- Os arquivos existem em dois formatos: v2 (AFRINIC, RIPE) e v2.3 (demais). A
  data pode vir vazia ou `00000000`, e o formato do `opaque-id` muda por RIR.
- Os arquivos delegated não trazem nome de AS nem ligação ASN → prefixo.
- NIC.br: 9.134 ASNs, 13.037 prefixos IPv4 e 8.954 IPv6. São 9.110 CNPJs e 24
  identificadores estrangeiros de 8 dígitos, nenhum CPF.
- 21 linhas do NIC.br usam ASN registrado fora da LACNIC/BR (ex.: AS8075,
  AS174, AS2914). Nesses casos o nome e o CNPJ são do titular brasileiro dos
  prefixos, não do ASN.

## Decisões

### Plataforma

| Tema | Decisão |
|---|---|
| Linguagem | Go no importador e na API |
| Módulos | Um `go.mod` por app, sem código compartilhado. O contrato entre os apps é o SQL (views do schema `api`) |
| Imagem | `distroless/static` nonroot. O healthcheck chama o próprio binário |
| Versão | SemVer por app (tag `registry-api/v1.2.0`), injetada via `-ldflags` com commit e data do build |
| Rotinas | Makefile por app com os mesmos alvos, e um Makefile na raiz que delega |
| Idioma | Identificadores em inglês. Comentários, documentação e commits em PT-BR com acentos |
| Licença | MIT |
| Segredos | Repositório público: nenhuma senha versionada. `.env` fica fora do git, `.env.example` é versionado e o gitleaks roda no CI. As senhas antigas de `tmp/` são descartadas |
| Estrutura | `apps/`, `database/postgresql/`, `database/valkey/`, `infra/traefik/`, `docs/`. A pasta `tmp/` não vai para o git |

### Banco

| Tema | Decisão |
|---|---|
| Versão | PostgreSQL 18 |
| Organização | Banco `badblock` com os schemas `ingest` (execuções, staging, dados brutos por fonte), `registry` (normalizado e central) e `api` (views estáveis) |
| Roles | `badblock_owner` (dono dos objetos, roda migrations), `badblock_sync` (escreve em `ingest` e `registry`), `badblock_api` (SELECT só em `api`). Senhas via `.env` |
| Migrations | dbmate, com SQL puro `up/down`. O bootstrap de roles, banco e extensões é separado e roda como superusuário |
| Tipo de IP | `cidr` nativo com índice GiST `inet_ops` |
| Convenções | `timestamptz` em UTC, PK `bigint` identity, chaves naturais `UNIQUE`, `uuid` nativo com `uuidv7()` só onde precisar de id externo |

### Modelo central

| Tabela | Conteúdo |
|---|---|
| `registry.prefix` | Todos os níveis (`iana`, `rir`, e `nicbr` quando mais específico) e todos os status. Um lookup devolve o prefixo mais específico e a cadeia até a IANA |
| `registry.asn` | Uma linha por ASN |
| `registry.holder` | Uma linha por `(rir, opaque-id)`. O nome vem do NIC.br (com CNPJ) no Brasil ou é inferido dos nomes dos ASNs do titular, com `name_source = inferred` |
| `registry.asn_prefix` | Vínculos tipados: `holder` (mesmo titular) e `nicbr` (explícito). `bgp` entra numa fase futura |

Status incluídos: `allocated`, `assigned`, `available` e `reserved`, mais os
blocos special-purpose da IANA, que alimentam as flags `bogon` e `special`.

### Sync

| Tema | Decisão |
|---|---|
| Frequência | Checagem a cada 1 hora por fonte, com GET condicional (`If-None-Match`/`If-Modified-Since`). Intervalo configurável por fonte e comando `sync --once --source=X` |
| Integridade | Confere o MD5 publicado pelos RIRs e compara as linhas `summary` com os registros lidos. Se uma fonte for remover mais de 5% do que tinha, a aplicação é abortada e só segue com `--force`. Os últimos 7 arquivos de cada fonte ficam num volume |
| Aplicação | `COPY` numa staging `UNLOGGED`, depois `MERGE` por fonte numa transação. O central é reconstruído numa transação única, e cada reconstrução gera uma nova versão do dataset com `NOTIFY` |
| Remoção | Soft delete (`removed_at`), com `registry.change_log` guardando antes e depois (`jsonb`) e o id da execução |
| Observabilidade | Logs JSON no stdout e histórico em `ingest.source_run`, consultável pela API. Sem Telegram e sem `/metrics` |

### API

| Tema | Decisão |
|---|---|
| Endpoints v1 | `/v1/ip/{ip}`, `/v1/ip` (IP de quem chama), `/v1/asn/{asn}`, `/v1/prefix/{ip}/{len}`, `/v1/holder/{rir}/{id}`, `/v1/country/{cc}/prefixes`, `/v1/country/{cc}/asns`, `/v1/rir/{rir}/prefixes`, `/v1/rir/{rir}/asns`, `/v1/asn/{asn}/history`, `/v1/prefix/{ip}/{len}/history` |
| Endpoints fixos | `/health` e `/status` (GET/POST, JSON com `success`, `status`, `timestamp`, `message`) e `/ping` (texto `pong`) |
| Legado | `GET /asn/{asn}` no host novo, no formato exato do POC (`asn`, `name`, `cid`, `country`, `rir`, `status`, `score` = `"0"`), com 404 respondendo `{}` |
| Formato | JSON aninhado (`prefix`, `holder`, `asns`, `chain`, `flags`, `dataset`), `snake_case`, datas em ISO-8601 |
| Acesso | Pública e sem chave. Rate limit por IP no Traefik, dimensionado para tráfego público: 5 req/s com rajada de 20 no geral, e 1 req/s com rajada de 5 nas listas |
| IP real | O Traefik é a borda e descarta `X-Forwarded-For`/`X-Real-IP` vindos do cliente. A API só confia nesses headers quando o peer está em `TRUSTED_PROXIES`, lê o `X-Forwarded-For` da direita para a esquerda pulando proxies confiáveis e usa o `X-Real-IP` como fallback |
| Cache | Valkey cache-aside, com a versão do dataset na chave. O lookup de IP usa o bloco /24 ou /48 que o contém, com chave por IP nos blocos que têm delegações menores. Fail-open com timeout de ~50 ms, e `/status` responde `degraded` quando só o cache está fora |

### Infraestrutura, testes e deploy

| Tema | Decisão |
|---|---|
| Compose | `docker-compose.yml` na raiz com `include` de `database/postgresql`, `database/valkey`, `infra/traefik` e `apps/*`, usado no dev e em produção |
| Traefik | No dev, `infra/traefik` (profile `dev`) com mkcert e `*.badblock.localhost`. Em produção, o Traefik que já existe no servidor (rede `traefik`, entrypoint `websecure`, certresolver `le`) |
| Banco em produção | Postgres e Valkey em containers no mesmo servidor, sem porta pública |
| Backup | `pg_dump -Fc` diário, com 7 cópias diárias e 4 semanais, e restore testado |
| Testes | `go test` com fixtures reais recortadas das fontes. Integração com testcontainers-go (PG18, Valkey e migrations). e2e em shell via `https://api.badblock.localhost`, com `X-Forwarded-For` e `X-Real-IP` forjados |
| CI | GitHub Actions: lint, testes, gitleaks e build por app, com filtro de path |
| Imagens | Docker Hub `tmsoftbrasil/badblock-<app>`, multi-arch, geradas pelo Actions a partir da tag de cada app |
| Deploy | `make deploy` por app: ssh no servidor, seguido de `compose pull` e `up -d` do serviço |
| Git | Primeiro push direto na `main`. Depois disso, `main` protegida e PR com CI obrigatório |

## Padrões adotados sem pergunta (vetáveis)

1. A fonte primária é sempre o RIR autoritativo, e o espelho da LACNIC fica
   como fallback.
2. A carga inicial é um baseline: não gera um evento por linha no `change_log`.
3. Remoção com carência: um recurso ausente de todas as fontes só é marcado
   removido depois de 26 horas (`REMOVAL_GRACE`). Assim uma transferência entre
   RIRs, cujos arquivos saem em horários diferentes, não vira remoção seguida
   de recadastro.
4. O nome do ASN vem do asn.txt. Para ASNs LACNIC/BR, vale o nome do NIC.br. O
   NIC.br nunca sobrescreve o nome de um ASN estrangeiro (a regra dos 21).
5. O campo `country` das respostas é o país de registro no RIR, não
   geolocalização. Isso fica documentado na API.
6. Nas listas, o filtro padrão de status é `allocated` + `assigned`
   (sobrescrevível), com paginação por cursor (padrão 100, máximo 1000). Com
   `?format=txt` a lista sai completa, um CIDR por linha, e `?aggregate=true`
   junta blocos contíguos.
7. Erros da v1 seguem o formato `{"error": {"code": "...", "message": "..."}}`.
   A rota legado mantém o 404 com `{}`.
8. As respostas levam `ETag` (versão do dataset) e `Cache-Control` curto, além
   do Valkey. CORS aberto para GET.
9. A especificação OpenAPI 3.1 fica versionada em `openapi.yaml` e é servida em
   `/openapi.yaml`, com uma página em `/docs`.
10. A configuração segue padrão → variável de ambiente → argumento de linha de
    comando. `-h`/`--help` mostra o propósito, os argumentos e as variáveis.
11. As conexões usam `POSTGRES_URL` e `REDIS_URL` (o Valkey fala o protocolo
    Redis).
12. No dev, o Postgres é publicado em `127.0.0.1:5433`, porque a 5432 do Mac
    de desenvolvimento já é de outro projeto. O Valkey não publica porta no
    host, e o Traefik local usa 80/443.
13. O Valkey funciona como cache puro: sem RDB/AOF, `maxmemory` de 256 MB e
    política `allkeys-lru`.
14. Os logs dos containers usam o driver `json-file` com rotação (10 MB × 3).
15. `database/` é o único lugar que escreve SQL e migrations (regra registrada
    no `AGENTS.md`). Os apps só consomem.
16. O fluxo n8n `ASN-BR-Sync-ALL` é desligado no corte, e o banco novo nasce
    vazio.

## Pendências que dependem de você

- Os dados do servidor de deploy (host, porta SSH e caminho) num `deploy.env`
  fora do git.
- Os secrets do GitHub para o Docker Hub: `DOCKERHUB_USERNAME` e
  `DOCKERHUB_TOKEN`.
- (Opcional) `brew install mkcert` e `mkcert -install` no Mac, para o
  navegador confiar no certificado local sem importar a CA.
- Desligar o workflow n8n no corte.

## Plano de implementação

1. Esqueleto do repositório: `.gitignore`, `LICENSE`, `README.md`,
   `AGENTS.md`/`CLAUDE.md`, Makefile e compose da raiz, gitleaks.
2. Banco: bootstrap, migrations (`ingest`, `registry`, `api`), compose do
   Postgres e do Valkey, backup.
3. `registry-sync`: download condicional, parsers, validação, staging e
   `MERGE`, reconstrução do central, `change_log` e testes.
4. `registry-api`: rotas v1 e legado, IP real, cache no Valkey, OpenAPI e
   testes.
5. Traefik local com mkcert e testes e2e.
6. CI, release, deploy e documentação. Com a suíte completa verde, você revisa
   o resultado e só então acontece o push inicial.

## 2026-09-28 — ajustes durante a implementação

O que mudou em relação ao plano acima, e por quê:

1. **URL RDAP pelo RIR que delegou**, e não pelo bootstrap da IANA: o bootstrap
   IPv4 é por /8, e em blocos legados a LACNIC delega partes de /8 da ARIN
   (`45.171.60.0/22` fica dentro de `45/8`, "Administered by ARIN").
2. **Nível `nicbr` na prática**: hoje todos os blocos do NIC.br coincidem com
   delegações da LACNIC, mas se a LACNIC deixar de listar um bloco que o NIC.br
   ainda liga a um ASN, ele continua visível no nível `nicbr` (coberto pelos
   testes de integração).
3. **Nomes do asn.txt**: o handle é o texto antes do primeiro ` - `, mesmo com
   espaços (padrão da AFRINIC); sem o separador, o primeiro token.
4. **Rota extra** `GET /v1/holder/{rir}/{id}/history`: o histórico de
   titulares já existia no `change_log`, então a rota veio junto com as de ASN
   e prefixo.
5. **`/status` responde HTTP 503 quando `offline`** (sem Postgres), para
   healthchecks de orquestradores enxergarem a falha; `online` e `degraded`
   respondem 200.
6. **Mínimo de registros desligado no modo fixture** (`SOURCES_DIR`): as
   fixtures são recortes de poucas linhas.
7. **Sem `docker compose up --wait`**: o serviço `migrate` roda e sai, e o
   `--wait` trata isso como falha. O `tests/e2e/e2e.sh` espera a API e a
   primeira sincronização.
8. **Certificado local sem mkcert**: `make certs` cai numa CA local criada com
   openssl, sem instalar nada no sistema; com o mkcert instalado, usa o mkcert.
9. **Rate limit**: 5 req/s com rajada de 20 no geral; 1 req/s com rajada de 5
   nas listas.
10. **Restore sem extensões**: o dump inclui `CREATE EXTENSION
    pg_stat_statements`, que exige superusuário; o restore pula essas entradas
    porque o bootstrap já cria as extensões.
11. **Produção em `api.badblock.net.br`**: o domínio inteiro fica com os
    containers deste projeto, e o site `badblock.net.br` será refeito aqui
    também. A primeira versão publicada das imagens é a 0.0.1.

Resultados medidos com os dados reais: carga inicial das 17 fontes em ~1,5 min,
reconstrução do central em ~14 s (140.286 ASNs, 659.765 prefixos, 128.546
titulares), lookup de IP no Postgres abaixo de 1 ms e ~4–7 ms ponta a ponta via
Traefik/TLS.
