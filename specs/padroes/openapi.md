# Manifesto OpenAPI das APIs

Vale para todas as `api-<fonte>`. Dono: sessão principal (a regra); o
conteúdo de cada manifesto é de quem cuida da API.

## Papel e precedência

- Toda API tem um manifesto **OpenAPI 3.1** em
  `apps/<fonte>/api/openapi/openapi.yaml`, embutido no binário
  (`openapi/embed.go`: `//go:embed openapi.yaml` → `var Spec []byte`) e servido
  em `GET /<fonte>/openapi.yaml`.
- O manifesto é **derivado** da spec de rotas da fonte
  (`specs/fontes/<fonte>/api.md` e, nos RIRs, `specs/fontes/rir/api.md`). Se
  os dois divergirem, vale a spec, e o manifesto é corrigido. Rota nova ou
  alterada muda a spec e o manifesto no mesmo trabalho.
- Ele fica dentro do app (e não em `specs/`) porque o `go:embed` não enxerga
  fora do módulo e o build da imagem usa só a pasta do app. Para reconstruir
  um manifesto perdido, gere-o a partir da spec da fonte com as regras abaixo.

## Estrutura

```yaml
openapi: 3.1.0
info:
  title: api-<fonte>            # igual ao nome do app (o teste confere)
  version: '<versão do app>'
  summary: <uma linha>
  description: |                # caminho de base, versões, HEAD/OPTIONS, cache e ETag,
    ...                         # formato do JSON, cabeçalhos comuns, formato dos erros
  license: {name: MIT, identifier: MIT}
externalDocs:
  description: Especificações da fonte (fonte da verdade)
  url: https://github.com/patrickbrandao/badblock/tree/main/specs/fontes/<fonte>
servers:                        # exatamente estes quatro, nesta ordem
  - url: https://api.badblock.net.br/<fonte>
    description: Produção, versão atual (hoje v1)
  - url: https://api.badblock.net.br/<fonte>/v1
    description: Produção, v1 fixa
  - url: http://127.0.0.1:<porta>/<fonte>
    description: Desenvolvimento local, versão atual (porta publicada no loopback pelo docker-compose.yml)
  - url: http://127.0.0.1:<porta>/<fonte>/v1
    description: Desenvolvimento local, v1 fixa
tags: [...]
paths:
  /asn/{asn}:                   # relativo ao caminho de base; vale na versão atual e na v1
    get: {operationId: getASN, ...}
  /health:
    servers:                    # saúde e manifesto: só os dois servidores sem /v1
      - url: https://api.badblock.net.br/<fonte>
      - url: http://127.0.0.1:<porta>/<fonte>
    get: {operationId: getHealth, ...}
    post: {operationId: postHealth, ...}
components:
  parameters: {IfNoneMatch, ...}
  headers: {ETag, CacheControlPublic, CacheControlNoStore, XCache, XDatasetVersion}
  responses: {NotModified, BadRequest, NotFound, ServiceUnavailable, DatabaseUnavailable, Timeout, InternalError, StatusOK, StatusError}
  schemas: {Error, Dataset, ...}
```

Regras:

- **A v1 fixa é um servidor**, não caminhos duplicados: cada rota aparece
  **uma vez** em `paths`, relativa ao caminho de base. Duplicar os caminhos
  em `/v1/...` repetiria `operationId`, o que a especificação OpenAPI proíbe.
- Rotas que não existem em `/v1` (saúde e o próprio manifesto) sobrescrevem
  `servers` no path item com os dois servidores sem `/v1`.
- O redirect de `/<fonte>` e o 404 genérico não entram.
- `operationId` único, em camelCase com o verbo (`getIndex`, `getASN`,
  `getMeta`, `getHealth`, `postHealth`, `getStatus`, `postStatus`, `ping`,
  `getOpenAPI`...).
- Rotas de dados documentam `If-None-Match`, os cabeçalhos das respostas
  (`ETag`, `Cache-Control`, `X-Cache`, `X-Dataset-Version`), o 304 e os erros
  que podem responder (400, 404, 503 `dataset_not_ready`, 503
  `database_unavailable`, 504, 500), reusando `components`.
- **Exemplos reais**: os `example`/`examples` de parâmetros, cabeçalhos e
  respostas são as respostas reais que estão na spec da fonte, e os erros
  trazem as mensagens reais de cada rota. O ETag de exemplo sai da versão do
  dataset e da consulta (`etagFor` em `internal/httpapi/server.go`).
- YAML indentado com espaços (nunca tabulação), no subconjunto que o teste lê:
  mapas em bloco, itens `- `, listas em fluxo numa linha e escalares `|`/`>`.
- As `description` de operações e componentes resumem as regras da spec (ex.:
  em `/status`, a precedência `error` > `degraded` > `starting` > `ok`). O
  texto não é contrato — o teste não o confere —, então um manifesto regerado
  pode dizê-lo com outras palavras; o que é contrato são caminhos, métodos,
  parâmetros, esquemas, códigos de resposta, cabeçalhos e exemplos.

## Resposta de `GET /<fonte>/openapi.yaml`

`Content-Type: application/yaml`, `Cache-Control: public, max-age=300`, sem
ETag, sem banco e sem Valkey; `HEAD` responde os mesmos cabeçalhos. Fora do
versionamento (`/<fonte>/v1/openapi.yaml` é 404) e listado no índice
(`endpoints`).

## Teste

`internal/httpapi/openapi_test.go` lê o manifesto linha a linha (sem
biblioteca de YAML). O teste completo, abaixo, é o da família RIR (o da
`api-lacnic` e dos quatro clones); cgibr, iana e asnames conferem um
subconjunto e fixam a porta no próprio teste — o que falta a cada um está na
spec da fonte, como pendência. O teste completo falha se:

- o arquivo não começa com `openapi: 3.1.` ou tem tabulação na indentação;
- `info.title` não é o nome do app;
- os `servers` da raiz não são os quatro acima, com a porta igual ao `PORT`
  do `Makefile` do app;
- um path com `servers` próprio não tem exatamente os dois sem `/v1`;
- uma rota registrada em `Handler()` falta no manifesto, ou o manifesto
  descreve uma rota que o servidor não registra (a rota com `/v1` corresponde
  ao path sem `servers` próprio; a rota só sem versão, ao path com `servers`
  próprio);
- um `operationId` se repete ou falta numa operação;
- um `$ref` aponta para um componente inexistente;
- `/openapi.yaml` não serve o arquivo embutido (GET e HEAD), responde em
  `/v1`, ou o índice não o lista (ou lista rota que falta no manifesto).
