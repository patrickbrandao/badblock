# Visão geral

BadBlock reúne dados públicos sobre **ASNs e blocos IP** (quem recebeu cada
número, de qual registro, em que país, desde quando, se é de uso especial) e
sobre a **raiz do DNS** (servidores raiz, zona raiz e âncoras DNSSEC), além
das **prestadoras de telecomunicações** autorizadas pela Anatel, e os
serve por uma API HTTP aberta, sem chave, em `https://api.badblock.net.br`.

## Fontes e apps

Uma **fonte** é um arquivo (ou um conjunto de arquivos) publicado por um
registro da internet — NIC.br, os cinco RIRs, a IANA, o RIPE NCC, a InterNIC —
ou por um órgão regulador (a Anatel, com fontes de dois níveis
`anatel/<conjunto>`). Cada fonte
tem **um par de apps independentes**:

| App | Faz | Não faz |
|---|---|---|
| `collector-<fonte>` | baixa a fonte e, só quando ela muda, grava nas tabelas `<fonte>_*` do PostgreSQL | não tem API web |
| `api-<fonte>` | lê essas tabelas e responde JSON abaixo de `/<fonte>/` | não escreve no banco |

O que cada fonte traz, os apps, as tabelas, o caminho e a porta local estão
em [../fontes/README.md](../fontes/README.md).

## Sites

Além das APIs, o projeto publica sites estáticos para pessoas, um por pasta
em `websites/<site>/`: o `www` responde por `badblock.net.br` e
`www.badblock.net.br`. Não leem o banco; o padrão está em
[../padroes/website.md](../padroes/website.md).

## Fluxo dos dados

```
fonte externa ──HTTP──▶ collector-<fonte> ──▶ PostgreSQL ◀── api-<fonte> ◀──HTTPS── Traefik ◀── cliente
 (arquivo publicado)    (laço; só aplica      tabelas <fonte>_*     │
                         quando muda)         + linha em jobs       └──▶ Valkey (cache opcional)
```

- O **banco é o contrato** entre coletor e API: nenhum app chama outro, e não
  há código compartilhado ([arquitetura.md](arquitetura.md)).
- A API identifica a versão dos dados pela última execução aplicada do
  coletor e a usa no cache e no ETag: dado novo nunca serve resposta velha.

## Fases

- **Fase 1 (atual)**: um par coletor/API por fonte, cada fonte com as
  próprias tabelas. Tudo o que está nestas specs.
- **Fase 2 (futura, não especificada)**: uma consolidação que lê `jobs`
  (linhas com `consolidated = 0`), copia os dados de todas as fontes para
  tabelas centrais e grava `consolidated = 1`. O contrato — a tabela `jobs` —
  já existe e os coletores já o cumprem
  ([../plataforma/postgres.md](../plataforma/postgres.md#tabela-jobs)).

## Fora do escopo (fase 1)

- Histórico: as tabelas espelham o **último** dataset aplicado de cada fonte.
- Backup: os dados vêm das fontes externas e são descartáveis; apagar o
  volume do Postgres e subir de novo recarrega tudo.
- Autenticação, escrita pela API, cruzamento entre fontes (é da fase 2).
