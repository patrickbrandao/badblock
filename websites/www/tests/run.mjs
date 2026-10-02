// Teste do build do website-www (specs/padroes/website.md#testes), em Node
// puro. Roda depois do `npm run build`: sobe um servidor estático com dist/ na
// raiz e landing-2/ ao lado, com as rotas da spec, e confere páginas, assets,
// imagens e os textos esperados no bundle JS.
import http from 'node:http';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const dist = path.join(root, 'dist');
const { APIS } = await import(path.join(root, 'src/content/apis.js'));
const { FLOW_GROUPS, FLOW_HUB_ITEMS, FLOW_HUB_LABEL, FLOW_SPLIT } = await import(path.join(root, 'src/content/datasources.js'));
const slugs = APIS.map((a) => a.slug);

let failures = 0;
const fail = (msg) => { failures++; console.error(`FALHOU: ${msg}`); };
const ok = (msg) => console.log(`ok - ${msg}`);
const check = (cond, msg) => (cond ? ok(msg) : fail(msg));

// Rotas de página: as mesmas do url.rewrite dos lighttpd*.conf.
const pageRe = new RegExp(`^/apis(/(${slugs.map((s) => s.replace(/[/]/g, '\\/')).join('|')}))?/?$`);

function resolveFile(urlPath) {
  if (urlPath === '/' || pageRe.test(urlPath)) return path.join(dist, 'index.src.html');
  if (urlPath.startsWith('/landing-2/')) return path.join(root, decodeURIComponent(urlPath));
  if (urlPath.startsWith('/assets/')) return path.join(dist, decodeURIComponent(urlPath));
  return null;
}

const server = http.createServer((req, res) => {
  const urlPath = new URL(req.url, 'http://x').pathname;
  if (urlPath === '/healthz') { res.writeHead(200); res.end('ok'); return; }
  const file = resolveFile(urlPath);
  if (!file || !file.startsWith(root) || !fs.existsSync(file) || !fs.statSync(file).isFile()) {
    res.writeHead(404); res.end('not found'); return;
  }
  res.writeHead(200);
  res.end(fs.readFileSync(file));
});

await new Promise((r) => server.listen(0, '127.0.0.1', r));
const base = `http://127.0.0.1:${server.address().port}`;
const get = async (p) => {
  const r = await fetch(base + p, { redirect: 'manual' });
  return { status: r.status, body: await r.text(), location: r.headers.get('location') };
};

try {
  // Páginas.
  const home = await get('/');
  check(home.status === 200, '/ responde 200');
  check(home.body.includes('lang="pt-BR"'), 'index.src.html com lang="pt-BR"');
  check((await get('/apis')).status === 200, '/apis responde 200');
  for (const s of slugs) {
    const r = await get(`/apis/${s}`);
    check(r.status === 200 && r.body === home.body, `/apis/${s} entrega o index.src.html`);
  }
  check((await get('/apis/nao-existe')).status === 404, '/apis/nao-existe responde 404');
  check((await get('/nao-existe')).status === 404, '/nao-existe responde 404');
  check((await get('/healthz')).status === 200, '/healthz responde 200');

  // Assets citados pelo HTML.
  const assets = [...home.body.matchAll(/(?:src|href)="(\/[^"]+)"/g)].map((m) => m[1]);
  check(assets.length > 0, 'index.src.html cita assets');
  for (const a of assets) check((await get(a)).status === 200, `asset ${a} responde 200`);

  // Imagens /landing-2/ citadas no código-fonte.
  const srcFiles = [];
  const walk = (d) => fs.readdirSync(d, { withFileTypes: true }).forEach((e) => {
    const p = path.join(d, e.name);
    if (e.isDirectory()) walk(p); else if (/\.(js|jsx)$/.test(e.name)) srcFiles.push(p);
  });
  walk(path.join(root, 'src'));
  const imgs = new Set();
  for (const f of srcFiles) for (const m of fs.readFileSync(f, 'utf8').matchAll(/\/landing-2\/[A-Za-z0-9_./-]+/g)) imgs.add(m[0]);
  for (const i of imgs) check((await get(i)).status === 200, `imagem ${i} existe`);
  check(imgs.has('/landing-2/brand/globe-ralf-dots.png'), 'logo do Badblock citado no código');

  // A lista de slugs dos lighttpd*.conf é a de src/content/apis.js.
  for (const conf of ['lighttpd.conf', 'lighttpd.docker.conf']) {
    const txt = fs.readFileSync(path.join(root, conf), 'utf8');
    const m = txt.match(/"\^\/apis\(\/\(([^)]*)\)\)/);
    const confSlugs = m ? m[1].split('|').sort() : [];
    check(JSON.stringify(confSlugs) === JSON.stringify([...slugs].sort()), `${conf}: slugs do url.rewrite iguais aos de apis.js`);
  }

  // Textos no bundle JS (a página é renderizada no navegador).
  const jsFile = assets.find((a) => a.endsWith('.js'));
  const js = jsFile ? (await get(jsFile)).body : '';
  const expected = [
    'Badblock', 'BadBlock', 'BadBlock Datasource', 'APIs', 'Conheça as APIs',
    'Principais rotas', 'Rotas comuns', 'Caminho base', 'Manifesto OpenAPI 3.1', 'Fonte dos dados',
    'https://api.badblock.net.br', '/openapi.yaml', 'Mudar para o tema escuro',
    ...APIS.map((a) => a.name),
    FLOW_HUB_LABEL, ...FLOW_HUB_ITEMS.map((it) => it.label), ...FLOW_GROUPS.map((g) => g.title), ...FLOW_GROUPS.flatMap((g) => g.items.map((it) => it.label)),
  ];
  for (const t of expected) check(js.includes(t), `bundle contém "${t}"`);
  // Node central: título "BadBlock" e os 7 itens, nessa ordem (todos no
  // bundle, conferido acima), cada um com um ícone de components/icons.jsx.
  const hubLabels = ['Collectors', 'Database', 'Cache', 'Webhook', 'API', 'MCP Server', 'Self-hosted'];
  check(FLOW_HUB_LABEL === 'BadBlock', 'content/datasources.js: título do node central é "BadBlock"');
  check(JSON.stringify(FLOW_HUB_ITEMS.map((it) => it.label)) === JSON.stringify(hubLabels),
    `content/datasources.js: itens do node central (${hubLabels.join(', ')})`);
  const iconsSrc = fs.readFileSync(path.join(root, 'src/components/icons.jsx'), 'utf8');
  for (const it of FLOW_HUB_ITEMS) check(iconsSrc.includes(`'${it.icon}':`), `components/icons.jsx tem o ícone ${it.icon} (${it.label})`);
  check(!fs.readFileSync(path.join(root, 'src/components/flow/nodes.jsx'), 'utf8').includes('globe'), 'node central sem o globo');
  // Quadro de fluxo: todo item com slug é uma API de apis.js e nenhuma API
  // aparece em mais de um item (nem toda API precisa estar no quadro; item
  // sem slug: fonte sem API); o ícone e a cor de todo item são conhecidos.
  const flowSlugs = FLOW_GROUPS.flatMap((g) => g.items.filter((it) => it.slug).map((it) => it.slug));
  for (const f of flowSlugs) check(slugs.includes(f), `content/datasources.js: o item ${f} é uma API de apis.js`);
  for (const f of new Set(flowSlugs)) check(flowSlugs.filter((x) => x === f).length === 1, `content/datasources.js: a API ${f} está em um item só`);
  for (const s of ['iana', 'asnames']) check(!flowSlugs.includes(s), `content/datasources.js: /${s} fora do quadro`);
  const tones = ['pink', 'blue', 'cyan', 'green', 'yellow', 'orange', 'red', 'white'];
  const css = fs.readFileSync(path.join(root, 'src/styles/tokens.css'), 'utf8');
  for (const t of tones.filter((x) => x !== 'white')) check(css.includes(`--${t}-500:`), `tokens.css define --${t}-500`);
  // Colunas: `columns`, quando existe, é 1 ou 2; os ids dos grupos não se
  // repetem; FLOW_SPLIT deixa ao menos um grupo de cada lado.
  for (const g of FLOW_GROUPS) check(g.columns === undefined || [1, 2].includes(g.columns), `content/datasources.js: colunas do grupo ${g.id} (1 ou 2)`);
  check(new Set(FLOW_GROUPS.map((g) => g.id)).size === FLOW_GROUPS.length, 'content/datasources.js: ids dos grupos únicos');
  check(Number.isInteger(FLOW_SPLIT) && FLOW_SPLIT > 0 && FLOW_SPLIT < FLOW_GROUPS.length, 'content/datasources.js: FLOW_SPLIT deixa grupos dos dois lados');
  check(FLOW_GROUPS.find((g) => g.id === 'bgp')?.columns === 2, 'content/datasources.js: BGP em 2 colunas');
  for (const it of FLOW_GROUPS.flatMap((g) => g.items)) {
    check(['square', 'star'].includes(it.icon || 'square') && tones.includes(it.tone), `content/datasources.js: ícone e cor de "${it.label}"`);
  }
  for (const [id, labels] of [['bgp', ['RPKI', 'IRR', 'Blackhole', 'Flowspec']], ['cyberops', ['Botnets e Zumbis', 'Ocorrências e bloqueios']]]) {
    const g = FLOW_GROUPS.find((x) => x.id === id);
    check(g && JSON.stringify(g.items.map((it) => it.label)) === JSON.stringify(labels) && g.items.every((it) => !it.slug), `content/datasources.js: itens do ${id}, sem API`);
  }
  // `span`, quando existe, é 'full' e só aparece em grupo de 2 colunas.
  for (const g of FLOW_GROUPS) for (const it of g.items) {
    check(it.span === undefined || (it.span === 'full' && g.columns === 2), `content/datasources.js: span de "${it.label}" ('full', em grupo de 2 colunas)`);
  }
  const gov = FLOW_GROUPS.find((g) => g.id === 'anatel');
  check(gov?.title === 'Governamental' && gov.columns === 2, 'content/datasources.js: grupo "Governamental" em 2 colunas');
  check(JSON.stringify(gov?.items.map((it) => [it.label, it.slug ?? null, it.span ?? null])) === JSON.stringify([['Anatel PST', 'anatel/pst', null], ['Receita Federal', null, null], ['Anablock (bloqueios judiciais)', null, 'full']]), 'content/datasources.js: itens do Governamental');
  const cg = FLOW_GROUPS.find((g) => g.id === 'cgibr');
  check(cg?.columns === 2 && JSON.stringify(cg.items.map((it) => [it.label, it.slug ?? null])) === JSON.stringify([['ASNs do Brasil', 'cgibr'], ['Prefixos IPv4 e IPv6', null], ['Sites críticos', null], ['PTTs (IX)', null]]), 'content/datasources.js: itens do CGI.br em 2 colunas');
  for (const t of ['Governamental', 'Anatel PST', 'Receita Federal', 'Anablock (bloqueios judiciais)', 'ASNs do Brasil', 'Prefixos IPv4 e IPv6', 'Sites críticos', 'PTTs (IX)']) check(js.includes(t), `bundle com "${t}"`);
  for (const t of ['TopSites', 'Open Resolvers']) check(FLOW_GROUPS.find((g) => g.id === 'dns').items.some((it) => it.label === t && !it.slug), `content/datasources.js: "${t}" no DNS, sem API`);
  // Página inicial só com Hero e quadro de fluxo; navbar só com a marca e o
  // tema. A home anterior está em src/drafts/draft01/, fora do bundle.
  for (const t of ['Ver todas as APIs', 'Abrir o menu', 'Pronto para começar?', 'Build e implantação', 'Segurança e conformidade']) {
    check(!js.includes(t), `bundle sem "${t}" (home e navbar do draft01)`);
  }
  // draft01: os arquivos existem e todo import relativo deles aponta para um
  // arquivo que existe (o build do Vite não os vê).
  const draftDir = path.join(root, 'src/drafts/draft01');
  for (const f of ['HomePage.jsx', 'Layout.jsx', 'Navbar.jsx', 'README.md']) check(fs.existsSync(path.join(draftDir, f)), `src/drafts/draft01/${f} existe`);
  for (const f of ['HomePage.jsx', 'Layout.jsx', 'Navbar.jsx']) {
    for (const m of fs.readFileSync(path.join(draftDir, f), 'utf8').matchAll(/from '(\.[^']+)'/g)) {
      check(fs.existsSync(path.resolve(draftDir, m[1])), `draft01/${f}: import ${m[1]} existe`);
    }
  }
  const absent = ['Platform features', 'Deploy a new project', 'Sign in', 'Pricing', '"Developers"', '"Company"', 'All systems operational'];
  for (const t of absent) check(!js.includes(t), `bundle sem "${t}"`);
} finally {
  server.close();
}

if (failures) { console.error(`\n${failures} falha(s)`); process.exit(1); }
console.log('\ntodos os testes passaram');
