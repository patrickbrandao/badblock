# 018 — Cache compartilhado sem limite de chaves pode ser inundado e despejar entradas úteis

- **Severidade:** baixa
- **Confiança:** provável
- **Categoria:** ponto fraco
- **Subcategoria:** esgotamento de recurso compartilhado (cache)
- **Status:** aberto — correção não aplicada
- **Arquivos afetados:** `apps/iana/api/internal/httpapi/handlers.go:82-129`, `apps/ripe/asnames/api/internal/httpapi/handlers.go:148-172`, `apps/cgibr/api/internal/httpapi/handlers.go:72-89`, `database/valkey/docker-compose.yml:21-36`, `specs/plataforma/valkey.md:40-61`, `specs/plataforma/publicacao.md:37`
- **Depende de / relacionado a:** 010; hipótese do rate limit sem agregar IPv6 (ver resumo)
- **Esforço estimado:** P (até ~1 h) para documentar e monitorar; M para dar TTL curto às respostas sem resultado
- **Auditoria:** 2026-11-07, commit `db43842`

## Explicação para leigos

As APIs guardam as respostas num cache em memória que todas dividem, limitado a 256 MB. Quando o cache enche, as entradas menos usadas são descartadas.

Algumas rotas respondem "ok" para qualquer endereço IP ou texto de busca, e cada pergunta diferente ocupa uma entrada nova. Com poucos computadores pedindo valores aleatórios, alguém consegue encher o cache de lixo e expulsar as respostas que os usuários de verdade usam.

Nada para de funcionar: as respostas só ficam mais lentas e o banco trabalha mais.

## Evidência e análise técnica

Rotas que geram uma chave nova por pedido diferente:
- **iana** `handleIP`/`handlePrefix` (`handlers.go:82-129`): a chave é `ip:<ip>` ou `prefix:<cidr>`, e a resposta é 200 mesmo quando `Block` é nulo. Há 2^32 + 2^128 chaves possíveis.
- **ripe/asnames** `handleSearch` (`handlers.go:148-172`): a chave é `search:<termo>`, com termos de 3 a 100 caracteres, e a resposta é 200 mesmo sem resultado.
- **cgibr** `handleIP` (`handlers.go:72-89`): IPs fora dos blocos dão 404, que não entra no cache. Mas cada endereço IPv6 dentro de um bloco brasileiro gera uma chave 200 diferente.

Configuração do Valkey:
- `--maxmemory-policy volatile-lru` e `VALKEY_MAXMEMORY` padrão de 256mb (`database/valkey/docker-compose.yml:21-36`).
- Todo o cache das APIs e as listas de retorno do rdns têm TTL, então todos podem ser despejados (`specs/plataforma/valkey.md:40-47`).

Estimativa (não medida):
- 5 req/s por IP durante 1 h de TTL dão 18.000 chaves vivas por IP e por API.
- Com ~500 B por chave, são ~9 MB por IP e por API.
- Nas 12 APIs versionadas, ~110 MB por IP. Três IPs bastam para encher o Valkey.

## Verificação (tentativa de refutação)

- **Proteção existente:** o rate limit do Traefik por IP limita a taxa, não o total de chaves. Não há limite de memória por API nem TTL menor para respostas sem resultado.
- **Alcance:** as rotas são públicas e não pedem autenticação.
- **Pré-condições:** poucos IPs, ou um bloco IPv6. É realista.
- **Fatores que reduzem a severidade:**
  - o cache falha aberto: um MISS vai ao Postgres por índice;
  - a carga extra no Postgres também é limitada pelo rate limit;
  - as listas `reply:<ip>` do rdns, com TTL de 30 s, são gravadas na hora, e o LRU tende a poupá-las;
  - a fila, que não tem TTL, nunca é despejada.
- **Documentação:** a spec explica a política de despejo (decisão #30), mas não trata de inundação deliberada.

## Impacto

- A taxa de acerto do cache cai para todos os usuários, e o Postgres recebe mais consultas.
- Em casos extremos, uma lista de retorno do rdns é despejada, e quem espera recebe o dado frio.
- Não há perda de dados nem indisponibilidade.

## Solução proposta

1. **Na spec** (`specs/plataforma/valkey.md` e `specs/padroes/api.md`, sessão principal):
   - registrar o risco;
   - definir um TTL curto (ex.: 60 s) para respostas 200 sem resultado: a iana com `block` nulo e sem `special`, e o `/search` com `count = 0`;
   - recomendar o monitoramento de `evicted_keys` em `INFO stats`.
2. **Opcional:** aumentar `VALKEY_MAXMEMORY` no `.env` de produção, ou separar as filas e os retornos do rdns e do rdapnum numa instância própria.

Apps afetados: `api-iana` e `api-ripe-asnames`, e o padrão `serveCached`/`cache.Set`, que passa a receber o TTL por chamada.

## Diff/patch proposto — NÃO APLICADO

Não aplicável. A correção começa por uma decisão de spec sobre TTL por resposta, que muda a interface `cache.Set`, comum a todas as APIs. Também pode ser resolvida só com configuração e monitoramento.

## Critérios de aceite

- [ ] `specs/plataforma/valkey.md` descreve o risco de inundação e a mitigação escolhida.
- [ ] Se o TTL curto for adotado: o `/ip` da iana sem bloco e o `/search` sem resultado são gravados com o TTL curto.

## Testes recomendados

- Teste unitário de `serveCached` com um cache falso que registra o TTL de cada `Set`.
- Teste de carga em ambiente de teste: N requisições com IPs aleatórios em `/iana/ip`, acompanhando `evicted_keys` e a taxa de HIT de uma rota fixa.

## Limitações e suposições

- O tamanho por chave e o número de IPs necessários são estimativas.
- Não verifiquei se o servidor de produção aceita IPv6.
