# src/ — onde mudar o quê

Código do site `www` do BadBlock (Vite + React 18 + Tailwind 3, JavaScript).
O padrão de todo site está em
[`specs/padroes/website.md`](../../../specs/padroes/website.md). O texto do
site é em português (PT-BR).

## Páginas

| Caminho | Página | Arquivo |
|---|---|---|
| `/` | página inicial | `HomePage.jsx` |
| `/apis` | lista das APIs | `ApisPage.jsx` |
| `/apis/<slug>` | uma API (`/apis/lacnic`, `/apis/anatel/pst`…) | `ApiPage.jsx` |
| outro (só no `npm run dev`) | página não encontrada | `NotFoundPage.jsx` |

`routes.js` escolhe a página pelo `location.pathname`; `main.jsx` renderiza.
Em produção o lighttpd entrega o mesmo `index.src.html` nessas rotas
(`url.rewrite-once` dos `lighttpd*.conf`) e responde 404 no resto.

## Estrutura

```
src/
  main.jsx, routes.js     # entry e rotas
  HomePage.jsx            # Hero / DatasourceFlow (só isso, entre a navbar e o rodapé)
  ApisPage.jsx            # cards das APIs, por grupo
  ApiPage.jsx             # caminho base, OpenAPI, fonte, sobre, dados, rotas
  NotFoundPage.jsx
  content/
    apis.js               # ★ as APIs: nome, descrição, fonte, rotas e exemplos
    nav.js                # menu "APIs" (usado só pela navbar do draft01)
    features.js           # textos das seções e cards da home do draft01
    datasources.js        # ★ quadro de fluxo: os 4 grupos de fontes (título, itens, cor), título e itens do node central
  components/
    Layout.jsx            # navbar + conteúdo + rodapé (todas as páginas)
    Navbar.jsx            # navbar (58px): só a marca e o botão de tema claro/escuro
    Logo.jsx              # marca: landing-2/brand/globe-ralf-dots.png + "BadBlock"
    Footer.jsx            # rodapé: APIs por grupo e links do projeto
    ApiParts.jsx          # cabeçalho, títulos, caixas e código das páginas de API
    FeatureCard.jsx, icons.jsx, badges.jsx, gears.jsx, deco.jsx
    flow/nodes.jsx        # nodes do quadro de fluxo (grupo de fontes e node central)
  sections/               # Hero.jsx e DatasourceFlow.jsx (o quadro de fluxo) na home; SectionNav,
                          # FeatureSection e FinalCta só no draft01
  drafts/draft01/         # home anterior guardada (fora das rotas e do bundle); ver o README.md dela
  styles/tokens.css       # tokens do tema (CSS variables) + @font-face + utilitários
```

## Tarefas comuns

- **Mudar o texto de uma API**: `content/apis.js`.
- **Nova API**: um item em `APIS` (`content/apis.js`) e o slug na regra de
  `url.rewrite-once` de `../lighttpd.conf` e `../lighttpd.docker.conf` — o
  `tests/run.mjs` falha se as listas divergirem.
- **Textos da página inicial**: título e subtítulo em `sections/Hero.jsx`;
  o resto é o quadro de fluxo. A home anterior (SectionNav, 6 seções de
  `content/features.js` e FinalCta, com a navbar do menu "APIs") está
  guardada em `drafts/draft01/` — não está em nenhuma rota nem no bundle; o
  `tests/run.mjs` confere que os imports dela existem.
- **Quadro de fluxo** (abaixo do Hero): diagrama em
  [`@xyflow/react`](https://reactflow.dev/) no estilo do hero do
  reactflow.dev — cartões com cabeçalho mono, handles cinza de 10px, edges
  bezier animadas (tracejadas, sem seta) e fundo de pontos uniforme, num
  quadro com borda fina e cantos arredondados. As fontes ficam em 6 nodes de
  grupo (IANA, DNS, BGP, Governamental, CGI.br, CyberOps), como o node "output" do
  reactflow.dev: título no cabeçalho e uma linha por item, só com o ícone e o
  rótulo (sem o caminho da API). Os itens ficam em 1 ou 2 colunas,
  preenchendo por coluna (ceil(n/2) na esquerda): `columns` no grupo fixa o
  número (BGP: 2 colunas, RPKI e IRR na esquerda, Blackhole e Flowspec na
  direita; CGI.br: 2 colunas, "ASNs do Brasil" e "Prefixos IPv4 e IPv6" na
  esquerda, "Sites críticos" e "PTTs (IX)" na direita; Governamental: 2
  colunas, "Anatel PST" e "Receita Federal" lado a lado e "Anablock
  (bloqueios judiciais)" com `span: 'full'`, numa linha inteira embaixo); sem ele, grupo com mais de 4 itens (IANA, DNS) usa 2 e os outros,
  1. Cada grupo tem um único handle, no lado voltado para o node central
  "BadBlock", na altura do título, e uma edge até um
  handle próprio do node central. Item sem API (Receita Federal, Anablock, Prefixos IPv4 e IPv6,
  Sites críticos, PTTs (IX), TopSites, Open Resolvers e todos os de BGP e CyberOps) aparece igual aos outros. Sem
  texto fora dos nodes.
  - Grupos, itens, rótulos, ícones, cores e colunas: `content/datasources.js`
    (`icon`: quadradinho ou estrela; `tone`: token de cor — pink, blue, cyan,
    green, yellow, orange, red — ou branco; Sites críticos é branco, Anablock é a
    estrela amarela, BGP é laranja (`--orange-500`, em `styles/tokens.css`) e
    CyberOps é vermelho; `FLOW_SPLIT`: quantos grupos, do começo da lista,
    ficam à esquerda/em cima). O `tests/run.mjs` confere que todo item com
    slug é uma API de `apis.js`, que nenhuma API aparece em mais de um item
    (/iana e /asnames ficam fora do quadro), que cores e colunas são válidas
    e que os títulos e rótulos estão no bundle.
  - Node central: `FLOW_HUB_LABEL` ("BadBlock") no cabeçalho e `FLOW_HUB_ITEMS`
    no corpo (Collectors, Database, Cache, Webhook, API, MCP Server,
    Self-hosted), cada um com um ícone de `components/icons.jsx`
    (download-cloud, database, zap, webhook, code, server, home) preto no
    tema claro e branco no escuro; sempre em 2 colunas preenchidas por coluna
    (Collectors, Database, Cache e Webhook à esquerda; API, MCP Server e
    Self-hosted à direita).
  - Visual dos nodes: `components/flow/nodes.jsx`.
  - Disposição e interação: `sections/DatasourceFlow.jsx` — a partir de
    900px, IANA, DNS e BGP empilhados à esquerda e Governamental, CGI.br e
    CyberOps à direita, cada coluna centralizada na altura do node central, com os
    handles do node central empilhados em cada lado na ordem dos nodes (as
    edges não se cruzam nem passam por cima de nodes); abaixo, IANA, DNS e
    BGP em cima e Governamental, CGI.br e CyberOps embaixo, em zigue-zague, com as
    edges em corredores laterais, aninhadas, sem rolagem horizontal. Todos os
    grupos têm a mesma largura (`GROUP_WIDTH`, a da linha de 2 colunas com
    "Prefixos IPv4 e IPv6"; "Anablock (bloqueios judiciais)" em 2 colunas e
    "Ocorrências e bloqueios" cabem nela).
    Arrastar nodes; sem zoom, sem pan, sem rolagem presa.
- **Marca**: `components/Logo.jsx` e a imagem em `../landing-2/brand/`.

## Uso

```bash
npm ci
npm run dev       # servidor do Vite (abre /index.src.html; /apis/... também funciona)
npm run build     # gera dist/
npm test          # build já feito: node tests/run.mjs
npm run serve     # lighttpd local → http://localhost:8090/
```

## Pendência

Os textos e as ilustrações das seções guardadas no draft01
(`content/features.js`) ainda não descrevem o BadBlock; precisam ser
escritos se ele voltar à página inicial
([pendências do `www`](../../../specs/padroes/website.md#pendências-do-www)).
