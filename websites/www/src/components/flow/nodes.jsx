import { Handle, Position } from '@xyflow/react';
import { Icon } from '../icons.jsx';

// Nodes do quadro de fluxo da página inicial (sections/DatasourceFlow.jsx),
// no estilo do hero do reactflow.dev: cartão rounded-2xl com borda fina,
// fundo translúcido e sombra quase nula; cabeçalho em mono com border-b e
// corpo opaco embaixo. Os handles são bolinhas cinza de 10px; existem para as
// edges se prenderem e não aceitam conexão.

const card = 'rounded-2xl border border-[#e5e5e5] dark:border-[#333] bg-[color-mix(in_srgb,var(--background)_70%,transparent)] shadow-[0_7px_9px_0_rgba(0,0,0,0.02)]';
const header = 'px-3 py-2 border-b border-[#e5e5e5] dark:border-[#333] font-mono text-xs font-semibold leading-4 text-gray-900 dark:text-white whitespace-nowrap';

const handleStyle = {
  width: 10, height: 10, minWidth: 0, minHeight: 0, border: 0, borderRadius: '50%',
  background: 'var(--flow-handle)', pointerEvents: 'none',
};

// Altura de uma linha de item do node de grupo (py-1.5 + text-xs), o respiro
// em cima e embaixo da lista, o limite de itens em 1 coluna e a altura, a
// partir do topo do node, do meio do cabeçalho (borda + py-2 + metade da
// linha), onde fica o handle: sections/DatasourceFlow.jsx usa esses valores
// para dispor os nodes.
export const ROW = 28;
export const LIST_PAD = 6;
export const ONE_COLUMN_MAX = 4;
export const HEADER_MID = 17;
// Altura de uma linha de item do node central (py-1.5 + text-sm) e o respiro
// em cima e embaixo da lista dele.
export const HUB_ROW = 32;
export const HUB_LIST_PAD = 8;
// Linhas da lista do node central com n itens em `cols` colunas (preenchidas
// por coluna: a da esquerda fica com ceil(n/cols) itens).
export const hubRows = (n, cols) => Math.ceil(n / cols);

// Colunas dos itens de um grupo ({ items, columns }): `columns` de
// content/datasources.js (1 ou 2) ou, sem ele, 2 acima de ONE_COLUMN_MAX
// itens e 1 nos outros.
export const columns = (group) => group.columns ?? (group.items.length > ONE_COLUMN_MAX ? 2 : 1);

// Posição de cada item no grid ({ row, col, span }, a partir de 1) e o total
// de linhas: os itens sem `span` preenchem por coluna (ceil(n/cols) linhas);
// cada item com span 'full' (só em 2 colunas) ganha uma linha inteira depois.
export function placement(group) {
  const cols = columns(group);
  const full = (it) => cols === 2 && it.span === 'full';
  const flow = group.items.filter((it) => !full(it));
  const base = Math.ceil(flow.length / cols);
  let k = 0; let extra = 0;
  const cells = group.items.map((it) => {
    if (full(it)) { extra += 1; return { row: base + extra, col: 1, span: 2 }; }
    const i = k++;
    return { row: (i % base) + 1, col: Math.floor(i / base) + 1, span: 1 };
  });
  return { cells, rows: base + extra };
}

// Ícone antes do rótulo, do tamanho do quadradinho (10px): it.icon é
// 'square' ou 'star'; it.tone é um token de cor do tema ou 'white'.
function ItemIcon({ icon, tone }) {
  const color = tone === 'white' ? '#fff' : `var(--${tone}-500)`;
  if (icon === 'star') {
    return (
      <svg viewBox="0 0 10 10" className="block h-2.5 w-2.5 shrink-0 overflow-visible" aria-hidden="true">
        <path
          d="M5 0.2 6.4 3.3 9.8 3.6 7.2 5.9 8 9.3 5 7.5 2 9.3 2.8 5.9 0.2 3.6 3.6 3.3Z"
          fill={color} stroke={color} strokeWidth="0.6" strokeLinejoin="round"
        />
      </svg>
    );
  }
  const border = tone === 'white' ? 'border border-[#c4c4c8] dark:border-transparent' : '';
  return <span className={`block h-2.5 w-2.5 shrink-0 rounded-[3px] ${border}`} style={{ background: color }} aria-hidden="true" />;
}

// Node de grupo, como o node "output" do reactflow.dev: o título no
// cabeçalho e uma linha por item — ícone e rótulo —, em 1 ou 2 colunas
// (columns(); em 2, preenchendo por coluna, e o item com span 'full' numa
// linha inteira: placement()). Um
// único handle, no lado voltado para o node central (data.side), na altura do
// meio do cabeçalho.
export function GroupNode({ data }) {
  const cols = columns(data);
  const { cells, rows } = placement(data);
  return (
    <div className={card} style={{ width: data.width }}>
      <div className={header}>{data.title}</div>
      <ul
        className={`bg-background rounded-b-2xl text-xs leading-4 grid ${cols === 2 ? 'grid-cols-2' : 'grid-cols-1'}`}
        style={{
          paddingTop: LIST_PAD, paddingBottom: LIST_PAD,
          gridTemplateRows: `repeat(${rows}, auto)`,
        }}
      >
        {data.items.map((it, i) => (
          <li
            key={it.label} className="flex items-center gap-2 px-3 py-1.5 whitespace-nowrap"
            style={{ gridRow: cells[i].row, gridColumn: `${cells[i].col} / span ${cells[i].span}` }}
          >
            <ItemIcon icon={it.icon} tone={it.tone} />
            <span className="text-gray-900">{it.label}</span>
          </li>
        ))}
      </ul>
      <Handle id="out" type="source" position={data.side} isConnectable={false} style={{ ...handleStyle, top: HEADER_MID }} />
    </div>
  );
}

// Lado do node → a coordenada que espalha os handles empilhados nele.
const along = {
  [Position.Left]: 'top', [Position.Right]: 'top',
  [Position.Top]: 'left', [Position.Bottom]: 'left',
};

// Node central, como o node "output" do reactflow.dev: o nome no cabeçalho e,
// no corpo, uma linha por item de data.items ({ label, icon }) — o ícone
// (components/icons.jsx), preto no tema claro e branco no escuro, e o rótulo,
// um pouco maior que o dos nodes de grupo — em data.columns colunas (1 ou 2,
// preenchendo por coluna: hubRows() linhas). data.handles traz um handle de
// entrada por edge:
// { id, position, offset } — offset em px a partir do meio do lado.
export function HubNode({ data }) {
  return (
    <div className={card} style={{ width: data.width }}>
      <div className={header}>{data.label}</div>
      <ul
        className={`bg-background rounded-b-2xl grid grid-flow-col ${data.columns === 2 ? 'grid-cols-2' : 'grid-cols-1'}`}
        style={{
          paddingTop: HUB_LIST_PAD, paddingBottom: HUB_LIST_PAD,
          gridTemplateRows: `repeat(${hubRows(data.items.length, data.columns)}, auto)`,
        }}
      >
        {data.items.map((it) => (
          <li key={it.label} className="flex items-center gap-2.5 px-4 py-1.5 text-sm leading-5 whitespace-nowrap">
            <Icon name={it.icon} className="block h-4 w-4 shrink-0 text-black dark:text-white" />
            <span className="text-gray-900">{it.label}</span>
          </li>
        ))}
      </ul>
      {data.handles.map((h) => (
        <Handle
          key={h.id} id={h.id} type="target" position={h.position} isConnectable={false}
          style={{ ...handleStyle, [along[h.position]]: `calc(50% + ${h.offset}px)` }}
        />
      ))}
    </div>
  );
}
