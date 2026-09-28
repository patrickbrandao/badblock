# Arquitetura

Como os dados saem das fontes públicas e chegam às respostas da API. As
decisões e o porquê de cada uma estão em [decisoes-fase-1.md](decisoes-fase-1.md).

## Componentes

| Componente | Papel | Porta |
|---|---|---|
| `registry-sync` | Baixa as fontes, valida, grava em `ingest` e reconstrói `registry` | 8002 (interna: /health, /status, /ping) |
| `registry-api` | Responde as consultas pelas views do schema `api` | 8001 (só via Traefik) |
| PostgreSQL 18 | Guarda tudo: dados por fonte, central, histórico | 5432 (host: 127.0.0.1:5433) |
| Valkey 9 | Cache das respostas da API | 6379 (interna) |
| Traefik 3.7 | HTTPS, redirecionamento HTTP→HTTPS, rate limit por IP | 80/443 |
| backup | `pg_dump -Fc` diário com retenção | — |

Todos na rede Docker `badblock`; a API também na rede `traefik`.

## Banco de dados

```
ingest                          registry                          api (views)
────────────────────────        ──────────────────────────        ─────────────────
source_state  source_run        dataset (versões)                 dataset
delegation (5 RIRs)       ──▶   holder   ──┐                      holder
iana_asn_block                  asn      ──┤ change_log            asn, asn_block
iana_ip_block                   prefix   ──┘ (insert/update/       prefix
iana_special_ip / _asn          asn_block    remove/restore)      change_log
rdap_service                    cache_bucket_exception            cache_bucket_exception
nicbr_asn  nicbr_prefix                                           source_status
asname
```

- **ingest** guarda o que cada fonte publicou, já normalizado, com `source_id`.
  Cada carga substitui só as linhas da própria fonte.
- **registry** é o central, reconstruído a partir de `ingest`:
  - `prefix`: todos os níveis — `iana` (blocos da IANA para os RIRs),
    `special` (special-purpose), `rir` (delegações) e `nicbr` (blocos do NIC.br
    sem delegação idêntica da LACNIC). Chave `(level, prefix)`; índice GiST
    `inet_ops` para "quem contém este IP".
  - `asn`: um por número (as faixas dos RIRs são expandidas).
  - `holder`: um por `(rir, opaque-id)` dos arquivos delegated.
  - `asn_block`: blocos de ASN da IANA e de uso especial.
- **api** são views estáveis. A registry-api só tem `SELECT` nelas; as views
  rodam com os privilégios do dono, então a API não enxerga as tabelas.

## Sincronização (registry-sync)

A cada minuto o laço confere quais fontes venceram o intervalo (padrão 1h) e,
para cada uma:

1. **Download condicional**: `If-None-Match`/`If-Modified-Since` para a mesma
   URL; 304, ou conteúdo com o mesmo SHA-256, encerra como `unchanged`. A URL
   primária é a do próprio RIR; espelhos só em falha.
2. **Integridade**: MD5 publicado pelo RIR; cabeçalho e linhas `summary` do
   arquivo delegated contra os registros lidos; mínimo de registros por fonte;
   arquivo com data anterior à já aplicada é recusado (`stale`).
3. **Aplicação** numa transação: `COPY` para uma staging temporária, checagem
   de chave repetida, **trava de remoção** (mais de 5% das linhas da fonte
   sumindo aborta a carga, a menos que venha `--force`) e `MERGE` com
   `RETURNING merge_action()` para contar inserções, alterações e remoções.
4. Se alguma fonte mudou, **reconstrói o central** numa transação só.

### Reconstrução do central

- ASNs e prefixos de delegação aparecem em dois RIRs durante uma
  transferência: vence o arquivo mais novo (data do cabeçalho) e, no empate,
  o status `allocated`/`assigned`.
- Registros IPv4 fora de CIDR (contagem que não é potência de 2) são divididos
  em CIDRs; `source_start`/`source_value` guardam o registro original.
- **Titular**: nome do NIC.br (com CNPJ) para titulares da LACNIC; para os
  demais, o nome mais frequente entre os ASNs do titular no asn.txt
  (`name_source = inferred`). O NIC.br lista 21 ASNs estrangeiros (AS8075,
  AS174...): o nome dele vai para o titular brasileiro dos blocos, nunca para
  o ASN.
- **URL RDAP** do RIR que delegou o recurso (e não do bloco /8 da IANA: em
  blocos legados como 45/8, da ARIN, a LACNIC delega partes).
- Três `MERGE` (holder, asn, prefix) com `RETURNING old/new` (PG 18) alimentam
  `registry.change_log` só com mudanças visíveis. Linha ausente de todas as
  fontes ganha `missing_since`; passada a carência (`REMOVAL_GRACE`, 26h),
  `removed_at` e um evento `remove`. Se voltar, `restore`.
- A primeira carga é baseline: não gera um evento por linha.
- Fim: nova linha em `registry.dataset` e `NOTIFY badblock_dataset`. Enquanto
  a transação não termina, a API lê a versão anterior.

Com os dados de 2026-09-28: 140 mil ASNs, 660 mil prefixos e 128 mil
titulares; a carga inicial das 17 fontes leva ~1,5 min (quase tudo download) e
a reconstrução ~14 s.

## Consultas (registry-api)

- **Lookup de IP**: `prefix >>= ip` no índice GiST devolve a cadeia inteira
  (menos de 1 ms). O prefixo principal é o mais específico; `flags.bogon` é
  verdadeiro para special-purpose não roteável ou quando nenhum RIR delegou o
  endereço.
- **Versão do dataset**: a API escuta `LISTEN badblock_dataset` e confere a
  versão a cada 30 s, caso a notificação se perca.
- **Cache (Valkey)**: chaves com a versão do dataset
  (`bb:v1:<versão>:...`), então uma versão nova invalida tudo sem varredura.
  Lookups de IP compartilham a chave do /24 (IPv4) ou /48 (IPv6); os blocos
  com prefixos mais específicos que isso (`cache_bucket_exception`) usam chave
  por IP. Fail-open com timeout de 50 ms e um disjuntor que para de consultar o
  Valkey por 5 s depois de 5 falhas seguidas. `singleflight` evita consultas
  duplicadas ao banco na mesma chave.
- **ETag** fraco com a versão e a URL: `If-None-Match` responde 304.

## IP real do cliente

O Traefik é a borda: descarta `X-Forwarded-*` e `X-Real-Ip` vindos do cliente
e grava os reais. A API aceita esses headers só quando a conexão vem de
`TRUSTED_PROXIES` (faixas privadas: a API não publica porta) e lê o
`X-Forwarded-For` da direita para a esquerda, pulando os proxies confiáveis.
Uma entrada inválida interrompe a leitura; sem resultado, vale o
`X-Real-IP` e, por fim, o endereço da conexão. Se um dia houver CDN na frente,
acrescente as faixas dela em `forwardedHeaders.trustedIPs` no Traefik e em
`TRUSTED_PROXIES`.

## Rate limit

No Traefik, por IP do cliente: 5 req/s com rajada de 20 no geral; 1 req/s com
rajada de 5 nas listas por país/RIR (router de prioridade maior). Ajustável por
`.env` (`API_RATE_*`).
