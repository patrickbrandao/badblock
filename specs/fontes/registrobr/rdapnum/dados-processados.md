# Dados processados da fonte `registrobr/rdapnum`

Parte do [dados.md](dados.md): as tabelas que o processamento grava a partir
das respostas **completas** e dos 404, e que a API lê. Dono: sub-agente
`collector-registrobr-rdapnum`. `<p>` abrevia `registrobr_rdapnum` nos nomes
de constraints e índices.

O dado processado espelha a **resposta atual** de cada alvo
(`target.response_uuid`): uma resposta nova completa substitui o que a
anterior gerou para o mesmo objeto; uma truncada nunca toca estas tabelas
([fonte.md](fonte.md#resposta-truncada)).

## Tabelas

| Tabela | Uma linha por | Chave natural |
|---|---|---|
| `registrobr_rdapnum_autnum` | ASN com resposta completa | `asn` |
| `registrobr_rdapnum_network` | bloco registrado devolvido pelo servidor | `prefix` |
| `registrobr_rdapnum_entity` | entidade (titular ou contato) | `handle` |
| `registrobr_rdapnum_link` | papel de uma entidade num ASN ou bloco | — |
| `registrobr_rdapnum_reverse` | delegação reversa de um bloco | — |

## `registrobr_rdapnum_autnum`

| Coluna | Tipo | Nulo | Padrão | Conteúdo |
|---|---|---|---|---|
| `uuid` | `uuid` | não (PK) | `uuidv7()` | chave |
| `asn` | `bigint` | não | — | `startAutnum` (igual a `endAutnum` no registro.br) |
| `target_uuid` | `uuid` | não | — | alvo que gerou (FK → `_target.uuid`, `ON DELETE CASCADE`) |
| `response_uuid` | `uuid` | não | — | resposta processada (FK → `_response.uuid`) |
| `handle`, `name`, `type`, `country` | `text` | sim | — | da resposta (`61610`, `ASN61610`, `DIRECT ALLOCATION`, `BR`) |
| `registrant_uuid` | `uuid` | sim | — | o titular (FK → `_entity.uuid`, `ON DELETE SET NULL`) |
| `registered_at`, `changed_at` | `timestamptz` | sim | — | `events` `registration` e `last changed` |
| `created_at` | `timestamptz` | não | `NOW()` | primeira vez processado |
| `updated_at` | `timestamptz` | não | `NOW()` | última mudança (trigger) |

`uq_<p>_autnum_asn`: `UNIQUE (asn)`; `uq_<p>_autnum_target`: `UNIQUE
(target_uuid)`; `chk_<p>_autnum_asn` como nos alvos;
`trg_<p>_autnum_updated_at`: `BEFORE UPDATE ... WHEN ((OLD.<colunas de
dado>) IS DISTINCT FROM (NEW.<colunas de dado>))` — as colunas de dado são
todas menos `uuid`, `target_uuid`, `response_uuid`, `created_at` e
`updated_at`: o `response_uuid` anda a cada processamento sem andar o
`updated_at`.

## `registrobr_rdapnum_network`

| Coluna | Tipo | Nulo | Padrão | Conteúdo |
|---|---|---|---|---|
| `uuid` | `uuid` | não (PK) | `uuidv7()` | chave |
| `prefix` | `cidr` | não | — | o bloco **devolvido** (`cidr0_cidrs`), que pode ser maior que o consultado |
| `family` | `smallint` | não | gerada: `family(prefix)` | `4` ou `6` |
| `target_uuid` | `uuid` | não | — | alvo que gerou por último (FK, `ON DELETE CASCADE`) |
| `response_uuid` | `uuid` | não | — | resposta processada (FK) |
| `handle`, `name`, `type`, `country`, `parent_handle` | `text` | sim | — | da resposta (`200.225.48.0/21`, `462539`, `ALLOCATED`, `BR`, `parentHandle`) |
| `status` | `text[]` | não | `'{}'` | `status` (`{active}`) |
| `asn` | `bigint` | sim | — | `nicbr_autnum` |
| `registrant_uuid` | `uuid` | sim | — | o titular (FK → `_entity.uuid`, `ON DELETE SET NULL`) |
| `registered_at`, `changed_at` | `timestamptz` | sim | — | `events` |
| `created_at`, `updated_at` | `timestamptz` | não | `NOW()` | como no `autnum` |

- `uq_<p>_network_prefix`: `UNIQUE (prefix)`. Dois alvos que devolvem o
  mesmo bloco (ex.: um `/24` estático dentro de um `/21` do `cgibr`) gravam a
  mesma linha; vale o último processado, e `target_uuid` aponta para ele.
- `ix_<p>_network_prefix_gist`: GiST em `(prefix inet_ops)` — o bloco que
  contém um IP ou prefixo (`>>=`).
- `ix_<p>_network_asn`: B-tree em `(asn)` — os blocos de um ASN.
- `ix_<p>_network_target`: B-tree em `(target_uuid)`.
- `trg_<p>_network_updated_at`: como o do `autnum` (fora das colunas de dado
  também `family`, que é gerada).

## `registrobr_rdapnum_entity`

| Coluna | Tipo | Nulo | Padrão | Conteúdo |
|---|---|---|---|---|
| `uuid` | `uuid` | não (PK) | `uuidv7()` | chave |
| `handle` | `text` | não | — | `35980592000130` (titular) ou o handle do contato (`ROBEL308`) |
| `kind` | `text` | sim | — | vCard `kind`: `org`, `individual` |
| `name` | `text` | sim | — | vCard `fn` |
| `email` | `text` | sim | — | vCard `email` (o primeiro, minúsculo) |
| `country` | `text` | sim | — | país do `adr` |
| `document` | `text` | sim | — | `publicIds[0].identifier` como publicado (`35.980.592/0001-30`) |
| `document_type` | `text` | sim | — | `publicIds[0].type` (`cnpj`, `cpf`) |
| `document_digits` | `text` | sim | gerada: `regexp_replace(document, '[^0-9]', '', 'g')` | busca por documento |
| `legal_representative` | `text` | sim | — | `legalRepresentative` |
| `registered_at`, `changed_at` | `timestamptz` | sim | — | `events` |
| `response_uuid` | `uuid` | sim | — | última resposta que atualizou a linha (`fk_<p>_entity_response`, `ON DELETE SET NULL`) |
| `created_at`, `updated_at` | `timestamptz` | não | `NOW()` | como no `autnum` |

- `uq_<p>_entity_handle`: `UNIQUE (handle)`.
- `ix_<p>_entity_document_digits`: B-tree em `(document_digits)`.
- A mesma entidade aparece em muitas respostas: cada resposta completa
  processada **atualiza** a linha com o que trouxe (o mais recente vence).
  Como só completas são processadas, um e-mail nunca é apagado por
  truncagem; um campo que a resposta completa não traz mais vira NULL.
- `fk_<p>_entity_response` é `SET NULL`, não cascata: a entidade é
  compartilhada por vários objetos, e apagar o alvo que a atualizou por
  último não pode levar as ligações dos outros. A que fica sem ligação
  nenhuma sai na manutenção.
- `trg_<p>_entity_updated_at`: como o do `autnum` (fora das colunas de dado
  também `document_digits`, que é gerada).

## `registrobr_rdapnum_link`

| Coluna | Tipo | Nulo | Padrão | Conteúdo |
|---|---|---|---|---|
| `uuid` | `uuid` | não (PK) | `uuidv7()` | chave |
| `entity_uuid` | `uuid` | não | — | a entidade (FK, `ON DELETE CASCADE`) |
| `autnum_uuid` | `uuid` | sim | — | o ASN (FK, `ON DELETE CASCADE`) |
| `network_uuid` | `uuid` | sim | — | ou o bloco (FK, `ON DELETE CASCADE`) |
| `role` | `text` | não | — | um papel por linha: `registrant`, `administrative`, `technical`, `abuse`… |
| `via_uuid` | `uuid` | sim | — | a entidade dentro da qual o contato veio (o titular); NULL = contato do próprio objeto (FK, `ON DELETE CASCADE`) |

- `chk_<p>_link_object`: `CHECK (num_nonnulls(autnum_uuid, network_uuid) =
  1)`.
- `uq_<p>_link`: `UNIQUE NULLS NOT DISTINCT (entity_uuid, autnum_uuid,
  network_uuid, role, via_uuid)`.
- `ix_<p>_link_entity`, `ix_<p>_link_autnum` (`WHERE autnum_uuid IS NOT
  NULL`), `ix_<p>_link_network` (`WHERE network_uuid IS NOT NULL`).
- Sem `updated_at`: o processamento apaga e regrava as ligações do objeto.

## `registrobr_rdapnum_reverse`

| Coluna | Tipo | Nulo | Padrão | Conteúdo |
|---|---|---|---|---|
| `uuid` | `uuid` | não (PK) | `uuidv7()` | chave |
| `network_uuid` | `uuid` | não | — | o bloco (FK, `ON DELETE CASCADE`) |
| `start_address`, `end_address` | `inet` | não | — | a faixa delegada |
| `prefix` | `cidr` | sim | — | a faixa como CIDR, quando é exata |
| `nameservers` | `jsonb` | não | `'[]'` | `[{"name":"keenan.ns.cloudflare.com","status":["ns aa"],"checked_at":"…","last_ok_at":"…"}]`, em ordem de nome |
| `signed` | `boolean` | sim | — | `secureDNS.delegationSigned` |
| `registered_at`, `changed_at` | `timestamptz` | sim | — | `events` |

`ix_<p>_reverse_network`: B-tree em `(network_uuid)`. Sem `updated_at`:
apagadas e regravadas com o bloco.

## Mapeamento resposta → colunas

Processar uma resposta é uma transação só ([collector.md](collector.md#processamento)):

| Resposta atual | Faz |
|---|---|
| `autnum` completa | `UPSERT` das entidades (titular, contatos, aninhados) por `handle`; `UPSERT` de `_autnum` por `asn`; apaga e regrava os `_link` do ASN |
| `ip network` completa | entidades como acima; `UPSERT` de `_network` por `prefix`; apaga e regrava os `_link` e os `_reverse` do bloco |
| 404 | apaga o `_autnum` ou os `_network` cujo `target_uuid` é o alvo (as ligações e reversos caem em cascata) |

- `UPSERT` (`... ON CONFLICT ... DO UPDATE SET` todas as colunas): o
  `response_uuid` (e, no bloco, o `target_uuid`) anda sempre, e o trigger
  só anda o `updated_at` quando um valor de dado mudou — ele diz quando o
  dado mudou. O processamento sabe que a linha mudou por `RETURNING
  updated_at = NOW()`.
- `_link` e `_reverse` são comparados com o que já está gravado: iguais,
  ficam; diferentes, o objeto tem os dele apagados e regravados.
- Bloco devolvido diferente do anterior para o mesmo alvo (o registro.br
  reagrupou): a linha antiga com esse `target_uuid` é apagada.
- Entidade que ficou sem nenhuma ligação sai na manutenção
  ([collector.md](collector.md#derivação)).
- Algo mudou: `jobs.last_sync_at = NOW()` e `consolidated = 0`, como última
  instrução da transação.

## Consultas da `api-registrobr-rdapnum`

Só leitura. A API não lê `_worker` nem `_config` fora do `/meta`.

| Uso | Consulta | Índice |
|---|---|---|
| pronta? (a cada `DATASET_POLL`) | `SELECT EXISTS (SELECT 1 FROM registrobr_rdapnum_target)` | qualquer |
| `/asn/{asn}` | o alvo (`WHERE asn = $1`) com o `_autnum` dele (`LEFT JOIN`), as ligações com as entidades (`WHERE autnum_uuid = $2`) e os blocos `SELECT prefix, type, status FROM registrobr_rdapnum_network WHERE asn = $1 ORDER BY family, prefix` | `uq_<p>_target_asn`, `ix_<p>_link_autnum`, `ix_<p>_network_asn` |
| `/ip/{ip}`, `/prefix/{ip}/{len}` | `SELECT ... FROM registrobr_rdapnum_network WHERE prefix >>= $1::cidr ORDER BY masklen(prefix) DESC LIMIT 1`, as ligações, os reversos e o alvo | `ix_<p>_network_prefix_gist`, `ix_<p>_link_network`, `ix_<p>_reverse_network` |
| `/document/{documento}` | as entidades com `document_digits = $1`; os ASNs e blocos ligados a elas com `role = 'registrant'` | `ix_<p>_entity_document_digits`, `ix_<p>_link_entity` |
| `/contact/{handle}` | a entidade por `handle` e todas as ligações dela, com o ASN ou o bloco | `uq_<p>_entity_handle`, `ix_<p>_link_entity` |
| `/raw`, `/history` | a resposta atual do alvo (`target.response_uuid`) e `SELECT uuid, result, http_status, sha256, bytes, fetched_at, last_seen_at, seen_count FROM registrobr_rdapnum_response WHERE target_uuid = $1 ORDER BY fetched_at DESC` | `ix_<p>_response_target` |
| `/response/{uuid}` | `SELECT ... FROM registrobr_rdapnum_response WHERE uuid = $1` | PK |
| `refresh` | o alvo por `asn` ou `prefix` (e `active`, `last_scan_at`) | `uq_<p>_target_*` |
| `/meta` | contagens com `count(*) FILTER (...)` sobre `_target` (~31 mil linhas) e `_response`; `count(*)` das processadas; a última `_run`; `_worker` das últimas 24 h; `_config`; a linha de `jobs` | `ix_<p>_run_applied`, `uq_jobs_app` |

## A migration

- Cabeçalho: o que cria, de que depende (`central`), quem escreve (o coletor
  e o sistema de gestão do administrador) e quem lê, a regra "a truncada
  nunca é processada" e o contrato da fila de atualização (apontando
  [dados.md](dados.md)).
- `migrate:up`, cada tabela seguida dos seus `COMMENT ON` (com a legenda de
  `result`/`last_result`): `_run` → `ix_<p>_run_applied` → `_config` →
  `_worker` → `_target` (sem a FK de `response_uuid`) → `_response` →
  `fk_<p>_target_response` → `_entity` → `_autnum` → `_network` → `_link` →
  `_reverse` → triggers e índices de cada uma.
- `migrate:down`: `DROP TABLE` na ordem inversa (`_reverse`, `_link`,
  `_network`, `_autnum`, `_entity`, depois `ALTER TABLE ... DROP CONSTRAINT
  fk_<p>_target_response`, `_response`, `_target`, `_worker`, `_config`,
  `_run`).
