# Resumo da auditoria — 2026-11-07 14:37

## 1. Parâmetros, data e commit

| Item | Valor |
|---|---|
| Skill | `problem-finder` (Purple Skills, servidor público) |
| Escopo | raiz inteira |
| Foco | todos |
| Severidade mínima | baixa |
| Máximo de relatórios | 25 |
| Rede | não |
| Data | 2026-11-07 14:20–14:37 |
| Commit | `db43842` (main, árvore limpa) |

## 2. Cobertura

Arquivos relevantes: cerca de 1.119 versionados, sem `vendor/` e sem binários do site. Destes, 493 são `.go`, com cerca de 100 mil linhas em 28 apps.

A descoberta foi feita em 8 lotes paralelos:

| Lote | Escopo |
|---|---|
| 1 | Plataforma e integração: `database/`, Makefile e compose da raiz, `.env.example`, `release-images.sh`, `compose.wait-migrate/`, `.gitleaks.toml`, Dockerfiles e composes dos 28 apps, `specs/plataforma/` |
| 2 | Modelo RIR: lacnic lido a fundo; afrinic, apnic, arin e ripencc comparados linha a linha com o modelo |
| 3 | cgibr, ripe/asnames e iana (coletor e API) |
| 4 | anatel/pst (coletor e API) |
| 5 | rdns (coletor, API e migration) |
| 6 | Coletor do registrobr/rdapnum |
| 7 | API do registrobr/rdapnum |
| 8 | roothints, rootzone, rootanchors e o site `websites/www` |

Cada lote declarou o que leu. Em geral, os lotes leram por inteiro os pacotes `internal/` e `cmd/` de produção, as migrations e as specs da fonte. Os testes (`*_test.go`) só foram consultados pontualmente. As specs longas foram lidas em parte, por grep e por faixas de linhas.

## 3. Exclusões

| Exclusão | Motivo |
|---|---|
| `old/` e `tmp/` | Regra do projeto (AGENTS.md): não ler sem pedido explícito |
| `apps/**/vendor/` (~61 mil arquivos) | Dependências Go de terceiros. Só foram consultadas para conferir comportamento de biblioteca (pgx) |
| `websites/www/node_modules`, fontes `.woff2`, SVG e imagens | Binários ou gerados |
| `package-lock.json` e `go.sum` | Lockfiles. Sem `rede=sim`, não houve consulta a bases de CVE |

## 4. Áreas não cobertas ou cobertas parcialmente

- Arquivos `*_test.go`: não auditados como alvo.
- Specs longas: lidas por grep e por faixas, não por inteiro. Exemplos: `specs/padroes/coletor.md`, `specs/padroes/openapi.md`, `specs/fontes/*/fixtures.md`.
- Coletores ripe/asnames e iana: só partes de `store`, `csv` e `fetch`.
- Componentes JSX do site, além de `content/` e da configuração do lighttpd.
- Dependências com vulnerabilidade conhecida: não verificadas, porque a rede estava desligada.
- Configuração de produção fora do git: `.env`, os `run-*.sh`, a configuração estática do Traefik e o DNS.

## 5. Relatórios criados

| ID | Título | Severidade | Confiança | Categoria | Esforço | Dependências |
|---|---|---|---|---|---|---|
| 001 | /rootzone/tld: punycode antes do limite de 63 permite DoS de CPU | média | provável | segurança | P | — |
| 002 | PTR de terceiros sem teto de quantidade e tamanho (rdns) | média | provável | ponto fraco | P | 003 |
| 003 | Tempo real (live=true) sem orçamento pode parar a varredura (rdns) | média | provável | ponto fraco | P | 002 |
| 004 | Keep-alive com o proxy prende o IP de saída (rdapnum) | média | provável | ponto fraco | P | — |
| 005 | CNPJ alfanumérico não suportado (anatel/pst, cgibr, rdapnum) | média | provável | ponto fraco | M | 009 |
| 006 | Redact não esconde senha em DSN/`?password=` (28 apps) | baixa | confirmado | segurança | M | — |
| 007 | Todos os serviços usam o superusuário do Postgres | baixa | confirmado | ponto fraco | M | — |
| 008 | Estouro de uint64 em SplitIPv4 (5 RIRs) | baixa | confirmado | bug | P | 009 |
| 009 | Recusa determinística repetida a cada RETRY_INTERVAL (12 coletores) | baixa | confirmado | ponto fraco | M | 005, 008, 022 |
| 010 | Versão do dataset lida fora do snapshot dos dados (12 APIs) | baixa | confirmado | incoerência | M | 018 |
| 011 | Lease julgado pelo SCAN_LEASE de quem pega (rdns, rdapnum) | baixa | confirmado | ponto fraco | M | — |
| 012 | INSERT em rdns_ptr sem ON CONFLICT (varredura × tempo real) | baixa | confirmado | bug | P | 013 |
| 013 | SaveLive grava sem reconferir rdns_prefix | baixa | confirmado | bug | P | 012 |
| 014 | Histórico rdapnum: A → 404 → A reaproveita a linha antiga | baixa | confirmado | incoerência | P | — |
| 015 | Allowlist do gitleaks não cobre fontes de dois níveis | baixa | confirmado | incoerência | P | — |
| 016 | Tag de versão por app inexistente (Makefiles e specs × release) | baixa | confirmado | incoerência | P | — |
| 017 | Specs de plataforma omitem registrobr/rdapnum; `.env.example` incompleto | baixa | confirmado | incoerência | P | — |
| 018 | Cache Valkey compartilhado pode ser inundado e despejar entradas | baixa | provável | ponto fraco | P/M | 010 |
| 019 | POST de refresh do rdapnum sem teto global fura a fila | baixa | provável | ponto fraco | M | — |
| 020 | Corpo recusado pelo jsonb prende o alvo sem espera (rdapnum) | baixa | provável | bug | P | — |
| 021 | Processamento rdapnum não confere se o objeto é o do alvo | baixa | provável | ponto fraco | P | — |
| 022 | RecordFailure sem prazo pode travar o laço (12 coletores) | baixa | provável | ponto fraco | P | 009 |

Total: 22 relatórios, sendo 5 de severidade média e 17 de baixa. Nenhum de severidade crítica ou alta.

## 6. Ordem sugerida de correção

1. **001**: DoS barato numa rota pública. A correção é de poucas linhas.
2. **004**: afeta a vazão da sonda rdapnum em produção. Correção de poucas linhas.
3. **002 e 003**: limites do rdns contra entrada externa e contra abuso do tempo real.
4. **005**: CNPJ alfanumérico. Atacar antes que a proporção na Anatel chegue a 1%. Pede mudança de spec e migrations.
5. **008 e 009**: corrigir o 008 primeiro (P). O 009 evita o laço de recusa que o 005 e o 008 podem disparar.
6. **012 com 013**: aplicar juntos, porque mexem na mesma transação de SaveLive.
7. **006 e 022**: padrões comuns a 28 e 12 apps. Vale fazer numa passada única pelos sub-agentes.
8. **010, 011, 014, 018, 019, 020, 021**: consistência e robustez.
9. **007**: mudança de decisão de projeto (nº 4). Discutir antes de implementar.
10. **015, 016, 017**: documentação e configuração. Podem ir juntos num PR de plataforma.

## 7. Achados além do limite de `max-relatorios`

Nenhum: 22 relatórios cabem no limite de 25.

Alguns candidatos de severidade baixa ficaram fora da verificação por refutação, para dar prioridade aos de maior risco. Eles **não foram verificados por um segundo agente** e por isso não viraram relatório. Ficam aqui como pistas:

**rdns**
- `not_allowed` decidido sobre o corpo frio do cache: até 1 h de atraso depois de mudar `active`.
- Uma única lista de retorno por IP: com várias instâncias da API, só um BLPOP recebe a resposta, e uma resposta atrasada serve ao pedido seguinte.
- Pool do Valkey do live igual a `MaxWaits` (128): na saturação, a resposta vira `unavailable` em vez de 503.
- Prefixo estático largo (menor que /22) só é varrido se sobrepuser outro prefixo.
- Possível deadlock entre o SaveScan e o DELETE do administrador (ordem de travas inversa).

**rdapnum (coletor)**
- Bloco compartilhado por dois alvos some quando o alvo "dono" é apagado ou recebe 404.
- Corrida entre processamento e nova coleta do mesmo alvo: o processamento não trava a linha do alvo.
- `truncRun` não zera depois da pausa: uma única truncada pausa de novo.
- Aviso sem a linha de hora/IP não gera o log debug que a spec pede.
- A resposta crua é guardada em jsonb, não "como veio" (reordena chaves e descarta duplicadas).

**rdapnum (API)**
- O teto da fila (LLEN e depois RPUSH) não é atômico.
- O comentário da marca inflight diverge do TTL de 60 s.
- O 503 com `database_unavailable` no refresh não aparece no OpenAPI.
- O índice `endpoints` não lista as rotas POST de refresh.
- O OpenAPI define o parâmetro ASN como string com pattern, sem faixa, enquanto a spec diz inteiro de 0 a 4294967295.

**anatel/pst**
- A regra do "CSV mais antigo" pode congelar o dataset sem sinal no `/meta`.
- O número de entradas do ZIP não tem limite, e cada entrada ignorada gera uma linha de log.
- A fixture pública guarda Fistel e processo SEI nas linhas de CPF. Isso é hipótese: falta confirmar se esses números reidentificam alguém.

**RIRs**
- O ETag e a chave de cache não incluem a versão do código: depois de um deploy que muda o JSON, sai corpo velho.
- `<rir>_run.created_at` é o início da transação, mas o COMMENT diz "fim da execução".

**Todas as APIs**
- `If-None-Match: *` devolve 304 para recurso inexistente. É o que a spec manda, mas diverge da RFC 9110.

**Plataforma**
- A soma dos pools pode passar de `max_connections=200` com réplicas das sondas.
- A tag da imagem é mutável e pode sair de uma árvore com alterações não commitadas, e o COMMIT não entra no build.
- Uma migration quebrada em qualquer pasta impede todos os coletores de subir no compose da raiz.

## 8. Hipóteses não confirmadas

| Hipótese | O que falta para confirmar |
|---|---|
| Rate limit do Traefik sem `sourceCriterion.ipStrategy.ipv6Subnet`: cada endereço de uma /64 IPv6 ganha cota própria, nas 14 APIs, no router do live do rdns e no refresh do rdapnum | Saber se `api.badblock.net.br` tem AAAA e se o Traefik de produção escuta em IPv6. Se sim, vira provável/baixa. A correção é uma label `ratelimit.sourcecriterion.ipstrategy.ipv6subnet: 64` |
| Exposição de CPF de pessoa física: (a) rdapnum aceita e devolve CPF; (b) cgibr rotula como "foreign" qualquer documento que não tenha 14 dígitos | (a) Conferir se existe `document_type = 'cpf'` real e decidir se a decisão #42 cobre CPF; é política, não código. (b) Um arquivo do cgibr com documento de 11 dígitos. Não há regra global de pessoa física nas specs |
| `/meta` público e sem cache faz agregações `count(*)` (rdapnum e rdns) | Tamanho real das tabelas e `EXPLAIN ANALYZE` em produção. Hoje é o padrão de todas as APIs (`specs/padroes/api.md`) |

## 9. Problemas já registrados e relatórios existentes

- Não havia `./tasks/` antes desta auditoria, então não houve deduplicação contra relatórios anteriores.
- O hardening do site www já está registrado como pendência em `specs/padroes/website.md:215-226` ("Pendências do `www`"). Não foi gerado relatório. Itens: sem cabeçalhos de segurança, processo como root, alpine 3.20 e porta publicada em todas as interfaces.

## 10. Candidatos refutados

Foram refutados 6 candidatos, e outros 2 foram mesclados:

| Candidato | Motivo da refutação |
|---|---|
| Busca do ripe/asnames sem trigrama varre a tabela | Documentado e aceito em `specs/fontes/ripe/asnames/api.md:118-120`. Custo limitado (~60 ms) e `DB_TIMEOUT` de 5 s |
| Conferência de integridade "falha aberta" (rootzone, roothints, rootanchors) | Documentada no padrão (`specs/padroes/coletor.md:95-98`). O hash vem do mesmo servidor do arquivo, então não prova autenticidade de qualquer forma. O limite de 4 KiB do `checksums-sha256.txt` cobre ~45 linhas, e o arquivo real tem 3 |
| Cliente HTTP dos coletores segue redirect https → http | Exige que o próprio servidor do RIR redirecione; quem controla esse servidor já serve qualquer conteúdo |
| Valkey sem senha | Só alcançável pela rede `badblock`, cujos membros receberiam a senha de todo jeito. Decisão documentada (`specs/plataforma/valkey.md:56-58`). Entrou como contexto no 007 |
| Site www não lista a API rdapnum | Não há regra que obrigue, e a API ainda não foi publicada (commits depois de v0.0.4) |
| Refresh do rdapnum "eleva o tráfego ao registro.br" | O coletor mantém a taxa configurada. O resto do candidato sobreviveu como 019 |

Mesclados:
- O "CSRF" no refresh foi mesclado ao 019: mesma causa, e o CORS `*` não é a causa.
- A leitura não atômica do cgibr `ASN` e do `Holder` dos RIRs foi mesclada ao 010.

## 11. Tentativas de prompt injection

Nenhuma encontrada. Nenhum arquivo lido tentou dar instruções aos agentes.

## 12. Limitações gerais

- **Análise só estática:** nada foi executado (testes, builds, gitleaks, consultas ao banco). Tempos e volumes são estimativas.
- **Sem acesso à rede:** fatos externos não foram conferidos (CNPJ alfanumérico, comportamento do registro.br e dos resolvedores, vulnerabilidades de dependências).
- **Configuração de produção fora do repositório:** Traefik estático, `.env` e os `run-*.sh` não foram vistos.
- **Desvios das regras da skill:**
  - um subagente de descoberta usou uma vez `| xargs -0 echo` só para listar nomes, fora da lista de comandos permitidos;
  - o orquestrador gravou um arquivo de instruções comuns aos verificadores no scratchpad da sessão, fora do projeto.

  Nenhum arquivo do projeto foi alterado além da criação de `./tasks/`.
