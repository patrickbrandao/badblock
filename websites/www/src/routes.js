// Rotas do site, resolvidas no navegador pelo pathname. O lighttpd entrega o
// mesmo index.src.html em "/", "/apis" e "/apis/<slug>" (url.rewrite nos
// lighttpd*.conf, com a lista de slugs) e responde 404 no resto.
import { APIS, findApi } from './content/apis.js';

export const API_SLUGS = APIS.map((a) => a.slug);

export function resolveRoute(pathname) {
  const p = pathname.replace(/\/index\.src\.html$/, '/').replace(/\/+$/, '') || '/';
  if (p === '/') return { page: 'home', title: 'Badblock — dados públicos da internet em APIs abertas' };
  if (p === '/apis') return { page: 'apis', title: 'APIs | Badblock' };
  const m = p.match(/^\/apis\/(.+)$/);
  const api = m ? findApi(m[1]) : null;
  if (api) return { page: 'api', api, title: `${api.name} — API | Badblock` };
  return { page: 'notfound', title: 'Página não encontrada | Badblock' };
}
