// Fontes de dados do quadro de fluxo da página inicial (sections/DatasourceFlow.jsx),
// em nodes de grupo: o título vai no cabeçalho do node e cada item é uma
// linha do corpo.
// - `slug` é o da API em content/apis.js. Item sem `slug` é uma fonte que
//   ainda não tem API; aparece igual aos outros. O tests/run.mjs confere que
//   todo item com slug é uma API de apis.js e que nenhuma API aparece em mais
//   de um item (nem toda API precisa estar no quadro).
// - `label` é o rótulo da linha (só o ícone e o rótulo; sem caminho).
// - `icon` é o ícone antes do rótulo: 'square' (padrão, o quadradinho) ou
//   'star' (estrela). `tone` é a cor dele: um token do tema (pink, blue, cyan,
//   green, yellow, orange, red) ou 'white' (branco; no tema claro ganha borda
//   cinza).
// - `columns` (opcional) é o número de colunas dos itens do grupo: 1 ou 2.
//   Sem ele, grupo com mais de 4 itens usa 2 colunas e os outros, 1. Em 2
//   colunas os itens preenchem por coluna (ceil(n/2) na da esquerda).
// - `span: 'full'` (opcional, só em grupo de 2 colunas) faz o item ocupar uma
//   linha inteira (as 2 colunas), depois das linhas dos outros itens.
// Cada node tem uma edge só até o node central.
// Ordem: os três primeiros grupos (IANA, DNS, BGP) ficam empilhados à
// esquerda do node central (em cima, no celular); os outros três (Governamental,
// CGI.br, CyberOps), à direita (embaixo, no celular). Os itens aparecem de
// cima para baixo.
// Grupos à esquerda do node central (em cima, no celular): os FLOW_SPLIT
// primeiros de FLOW_GROUPS.
export const FLOW_SPLIT = 3;

export const FLOW_GROUPS = [
  {
    id: 'iana', title: 'IANA',
    items: [
      { slug: 'arin', label: 'ARIN', tone: 'pink' },
      { slug: 'ripencc', label: 'RIPE NCC', tone: 'pink' },
      { slug: 'apnic', label: 'APNIC', tone: 'pink' },
      { slug: 'afrinic', label: 'AFRINIC', tone: 'pink' },
      { slug: 'lacnic', label: 'LACNIC', tone: 'pink' },
    ],
  },
  {
    id: 'dns', title: 'DNS',
    items: [
      { slug: 'rootanchors', label: 'Root Anchors', tone: 'cyan' },
      { slug: 'roothints', label: 'Root Hints', tone: 'cyan' },
      { slug: 'rootzone', label: 'Root Zone', tone: 'cyan' },
      { label: 'TopSites', tone: 'cyan' },
      { label: 'Open Resolvers', tone: 'cyan' },
    ],
  },
  {
    id: 'bgp', title: 'BGP', columns: 2,
    items: [
      { label: 'RPKI', tone: 'orange' },
      { label: 'IRR', tone: 'orange' },
      { label: 'Blackhole', tone: 'orange' },
      { label: 'Flowspec', tone: 'orange' },
    ],
  },
  {
    id: 'anatel', title: 'Governamental', columns: 2,
    items: [
      { slug: 'anatel/pst', label: 'Anatel PST', tone: 'green' },
      { label: 'Receita Federal', tone: 'green' },
      { label: 'Anablock (bloqueios judiciais)', icon: 'star', tone: 'yellow', span: 'full' },
    ],
  },
  {
    id: 'cgibr', title: 'CGI.br', columns: 2,
    items: [
      { slug: 'cgibr', label: 'ASNs do Brasil', tone: 'green' },
      { label: 'Prefixos IPv4 e IPv6', tone: 'green' },
      { label: 'Sites críticos', tone: 'white' },
      { label: 'PTTs (IX)', tone: 'green' },
    ],
  },
  {
    id: 'cyberops', title: 'CyberOps',
    items: [
      { label: 'Botnets e Zumbis', tone: 'red' },
      { label: 'Ocorrências e bloqueios', tone: 'red' },
    ],
  },
];

// Título do node central.
export const FLOW_HUB_LABEL = 'BadBlock';

// Itens do corpo do node central, em 2 colunas preenchidas por coluna (a
// metade de cima da lista, arredondada para cima, à esquerda): `label` é o
// rótulo e `icon`, o nome do ícone em components/icons.jsx (desenhado antes
// dele).
export const FLOW_HUB_ITEMS = [
  { label: 'Collectors', icon: 'download-cloud' },
  { label: 'Database', icon: 'database' },
  { label: 'Cache', icon: 'zap' },
  { label: 'Webhook', icon: 'webhook' },
  { label: 'API', icon: 'code' },
  { label: 'MCP Server', icon: 'server' },
  { label: 'Self-hosted', icon: 'home' },
];
