import { useEffect, useMemo, useRef, useState } from 'react';
import {
  ReactFlow, ReactFlowProvider, Background, BackgroundVariant, Position,
  useNodesState, useReactFlow,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import {
  GroupNode, HubNode, ROW, LIST_PAD, HEADER_MID, HUB_ROW, HUB_LIST_PAD, columns, placement, hubRows,
} from '../components/flow/nodes.jsx';
import { FLOW_GROUPS, FLOW_HUB_ITEMS, FLOW_HUB_LABEL, FLOW_SPLIT } from '../content/datasources.js';

// Quadro de fluxo da página inicial, logo abaixo do Hero, no estilo do hero
// do reactflow.dev: nodes de grupo de fontes de dados ligados ao node
// central (o BadBlock) por edges bezier animadas, sem seta: uma edge
// por grupo, do handle único dele (na altura do título) até um handle próprio
// no node central. Sem texto além do que está nos nodes.

const nodeTypes = { sources: GroupNode, hub: HubNode }; // não "group": é um tipo do React Flow, com estilo próprio
const HUB = 'badblock';
const HEADER = 33; // altura do cabeçalho dos nodes (py-2 + text-xs + border-b)
// Largura fixa de todos os nodes de grupo: a da linha mais larga, a de 2
// colunas iguais com "Prefixos IPv4 e IPv6" (CGI.br; medida no navegador:
// 2 × 144px de coluna + 2px de borda = 290px), com folga. "Anablock
// (bloqueios judiciais)" ocupa as 2 colunas (266px) e cabe; a linha de 1
// coluna mais larga, "Ocorrências e bloqueios" (CyberOps), mede 180px.
const GROUP_WIDTH = 296;
// Largura do node central a partir de 900px (2 colunas de itens).
const HUB_WIDTH = 296;

const rows = (g) => placement(g).rows;
const groupHeight = (g) => 2 + HEADER + 2 * LIST_PAD + rows(g) * ROW;
// Node central: cabeçalho e a lista de FLOW_HUB_ITEMS em hub.columns colunas.
const hubHeight = (hub) => 2 + HEADER + 2 * HUB_LIST_PAD + hubRows(FLOW_HUB_ITEMS.length, hub.columns) * HUB_ROW;
const sum = (xs) => xs.reduce((a, b) => a + b, 0);
// Os FLOW_SPLIT primeiros grupos (IANA, DNS, BGP) ficam de um lado do node
// central; os outros (Governamental, CGI.br, CyberOps), do outro.
const LEFT = FLOW_GROUPS.slice(0, FLOW_SPLIT);
const RIGHT = FLOW_GROUPS.slice(FLOW_SPLIT);

// Empilha nodes de alturas `hs` a partir de `top`, com `gap` entre eles, e
// devolve o centro de cada um.
function stack(hs, top, gap) {
  let y = top;
  return hs.map((h) => { const c = y + h / 2; y += h + gap; return c; });
}

// Altura do handle de um grupo de altura h com centro em y (nodeOrigin no
// meio do node): o meio do cabeçalho.
const handleY = (y, h) => y - h / 2 + HEADER_MID;

// Cada layout diz o tamanho do node central, a altura do quadro (e, se tiver
// `width`, a proporção dele), onde fica o centro de cada grupo (o node
// central fica em 0,0), o lado do handle do grupo (`side`) e o lado do node
// central em que a edge dele entra (`hubSide`), e `hubOffset(side, k, n, y)`:
// onde fica, a partir do meio desse lado, o handle da k-ésima de n edges que
// entram nele (k = 0 é a do primeiro grupo; y é a altura do handle do grupo).
const LAYOUTS = {
  // A partir de 900px: IANA, DNS e BGP empilhados à esquerda; Governamental, CGI.br
  // e CyberOps empilhados à direita, cada coluna centralizada na altura do
  // node central. Em cada lado do node central os handles ficam
  // empilhados no meio do corpo (a lista, abaixo do cabeçalho), a `step` px
  // um do outro, na ordem dos nodes, então nenhuma curva
  // cruza outra; e como as curvas só andam entre a coluna dos grupos e o node
  // central, nenhuma passa por cima de um node.
  wide: (() => {
    // Node central em 2 colunas iguais: cada uma da largura do item mais
    // largo ("Self-hosted"/"Collectors", ~135px), com folga.
    const hub = { width: HUB_WIDTH, columns: 2 };
    const gapX = 130; const gapY = 28; const step = HUB_ROW;
    const x = hub.width / 2 + gapX + GROUP_WIDTH / 2;
    const col = (gs) => {
      const hs = gs.map(groupHeight);
      const total = sum(hs) + gapY * (hs.length - 1);
      return { total, ys: stack(hs, -total / 2, gapY) };
    };
    const l = col(LEFT); const r = col(RIGHT);
    const fit = { padding: 0.06 };
    const scale = 1 + 2 * fit.padding;
    return {
      hub, fit,
      // Altura com zoom 1, na largura `width` (perto da do max-w-container):
      // o quadro guarda essa proporção e encolhe junto com o zoom, sem sobrar
      // espaço vazio em cima e embaixo.
      height: Math.ceil(Math.max(l.total, r.total, hubHeight(hub)) * scale) + 24,
      width: Math.ceil(2 * x + GROUP_WIDTH) * scale,
      groups: [
        ...l.ys.map((y) => ({ x: -x, y, side: Position.Right, hubSide: Position.Left })),
        ...r.ys.map((y) => ({ x, y, side: Position.Left, hubSide: Position.Right })),
      ],
      hubOffset: (side, k, n) => HEADER / 2 + (k - (n - 1) / 2) * step,
    };
  })(),
  // Abaixo de 900px (celular e tablet): IANA, DNS e BGP empilhados em cima
  // do node central, um pouco à esquerda, com o handle à direita (na altura
  // do título); as edges descem pelo corredor à direita deles até o topo do
  // node central. Embaixo, Governamental, CGI.br e CyberOps empilhados um pouco à
  // direita, com o handle à esquerda, subindo pelo corredor da esquerda até a
  // base do node central. Em cada lado, o grupo mais perto do node central
  // pega o handle mais de dentro, então as curvas ficam aninhadas, sem
  // cruzar. Os handles ficam a `lane` px um do outro e o de fora, a `margin`
  // px da borda do node central (fora do canto arredondado). O deslocamento
  // `xr` deixa o node central da largura do conjunto, sem rolagem horizontal.
  narrow: (() => {
    const inset = 10; const lane = 14; const margin = 22; const gapY = 36; const gapStack = 18;
    const lanes = Math.max(LEFT.length, RIGHT.length) - 1;
    const xr = Math.round((GROUP_WIDTH - inset - lane * lanes - margin) / 2);
    // Node central da largura do conjunto, com os itens em 2 colunas.
    const hub = { width: 2 * (GROUP_WIDTH - xr), columns: 2 };
    const hubH = hubHeight(hub);
    const ths = LEFT.map(groupHeight);
    const bhs = RIGHT.map(groupHeight);
    const topH = sum(ths) + gapStack * (ths.length - 1);
    const top = -hubH / 2 - gapY - topH;
    const bottom = hubH / 2 + gapY + sum(bhs) + gapStack * (bhs.length - 1);
    const fit = { padding: 0.02 };
    return {
      hub, fit,
      height: Math.ceil((bottom - top) * (1 + 2 * fit.padding)) + 8,
      groups: [
        ...stack(ths, top, gapStack).map((y) => ({
          x: xr - GROUP_WIDTH / 2, y, side: Position.Right, hubSide: Position.Top,
        })),
        ...stack(bhs, hubH / 2 + gapY, gapStack).map((y) => ({
          x: GROUP_WIDTH / 2 - xr, y, side: Position.Left, hubSide: Position.Bottom,
        })),
      ],
      hubOffset: (side, k, n) => (side === Position.Top
        ? xr + inset + lane * (n - 1 - k)
        : -(xr + inset + lane * k)),
    };
  })(),
};

function buildGraph(layout) {
  const edges = [];
  const links = {}; // lado do node central → [{ id, y }], na ordem dos grupos
  const nodes = FLOW_GROUPS.map((group, gi) => {
    const { x, y, side, hubSide } = layout.groups[gi];
    const id = `group-${group.id}`;
    const target = `in-${group.id}`;
    (links[hubSide] ||= []).push({ id: target, y: handleY(y, groupHeight(group)) });
    edges.push({
      id: `${id}->${HUB}`, source: id, sourceHandle: 'out',
      target: HUB, targetHandle: target, type: 'default',
      animated: true, focusable: false, selectable: false,
      style: { stroke: 'var(--flow-edge)', strokeWidth: 2 },
    });
    const items = group.items.map((it) => ({ label: it.label, icon: it.icon || 'square', tone: it.tone, span: it.span }));
    return {
      id, type: 'sources', position: { x, y },
      data: { title: group.title, items, columns: columns(group), side, width: GROUP_WIDTH },
    };
  });
  const handles = Object.entries(links).flatMap(([position, list]) => list.map((h, k) => ({
    id: h.id, position, offset: layout.hubOffset(position, k, list.length, h.y),
  })));
  nodes.unshift({
    id: HUB, type: 'hub', position: { x: 0, y: 0 },
    data: {
      label: FLOW_HUB_LABEL, items: FLOW_HUB_ITEMS, columns: layout.hub.columns, handles, width: layout.hub.width,
    },
  });
  return { nodes, edges };
}

function useMatchMedia(query) {
  const [match, setMatch] = useState(() => typeof window !== 'undefined' && window.matchMedia(query).matches);
  useEffect(() => {
    const mq = window.matchMedia(query);
    const on = () => setMatch(mq.matches);
    on();
    mq.addEventListener('change', on);
    return () => mq.removeEventListener('change', on);
  }, [query]);
  return match;
}

// O tema do site é a classe .dark/.light no <html> (Navbar.jsx troca).
function useDarkClass() {
  const [dark, setDark] = useState(() => typeof document !== 'undefined' && document.documentElement.classList.contains('dark'));
  useEffect(() => {
    const el = document.documentElement;
    const obs = new MutationObserver(() => setDark(el.classList.contains('dark')));
    obs.observe(el, { attributes: true, attributeFilter: ['class'] });
    return () => obs.disconnect();
  }, []);
  return dark;
}

function Flow({ layout }) {
  const graph = useMemo(() => buildGraph(layout), [layout]);
  const [nodes, , onNodesChange] = useNodesState(graph.nodes);
  const { fitView } = useReactFlow();
  const ref = useRef(null);
  const dark = useDarkClass();

  // Reenquadra quando o quadro muda de tamanho (rotação, janela redimensionada).
  useEffect(() => {
    const el = ref.current;
    if (!el || typeof ResizeObserver === 'undefined') return undefined;
    const obs = new ResizeObserver(() => fitView(layout.fit));
    obs.observe(el);
    return () => obs.disconnect();
  }, [fitView, layout]);

  return (
    <div ref={ref} className="h-full w-full">
      <ReactFlow
        nodes={nodes}
        edges={graph.edges}
        onNodesChange={onNodesChange}
        nodeTypes={nodeTypes}
        nodeOrigin={[0.5, 0.5]}
        colorMode={dark ? 'dark' : 'light'}
        fitView
        fitViewOptions={layout.fit}
        minZoom={0.3}
        maxZoom={1}
        nodesConnectable={false}
        elementsSelectable={false}
        panOnDrag={false}
        zoomOnScroll={false}
        zoomOnPinch={false}
        zoomOnDoubleClick={false}
        preventScrolling={false}
        proOptions={{ hideAttribution: true }}
        style={{ background: 'transparent' }}
      >
        <Background variant={BackgroundVariant.Dots} gap={20} size={1} color="rgb(145,145,154)" bgColor="transparent" />
      </ReactFlow>
    </div>
  );
}

export function DatasourceFlow() {
  const wide = useMatchMedia('(min-width: 900px)');
  const layout = wide ? LAYOUTS.wide : LAYOUTS.narrow;
  return (
    <section
      className="relative w-full mb-8 px-5 md:px-8 lg:px-12 [--flow-edge:rgb(177,177,183)] [--flow-handle:rgb(156,163,175)] dark:[--flow-edge:rgb(210,210,210)]"
      aria-label="Fontes de dados que alimentam a API do BadBlock"
      data-testid="datasource-flow"
    >
      <div
        className="relative max-w-container mx-auto overflow-hidden rounded-2xl border border-[rgb(122,130,150)] dark:border-[#3c464b]"
        style={layout.width
          ? { aspectRatio: `${layout.width} / ${layout.height}` }
          : { height: layout.height }}
      >
        <div className="pointer-events-none absolute left-1/2 top-1/2 h-[520px] w-[760px] max-w-full -translate-x-1/2 -translate-y-1/2 bg-[radial-gradient(closest-side,rgba(124,77,255,0.10),transparent)] dark:bg-[radial-gradient(closest-side,rgba(124,77,255,0.16),transparent)]" aria-hidden="true" />
        <ReactFlowProvider key={wide ? 'wide' : 'narrow'}>
          <Flow layout={layout} />
        </ReactFlowProvider>
      </div>
    </section>
  );
}
