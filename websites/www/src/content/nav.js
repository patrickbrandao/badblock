// Menus da navbar (desktop e mobile). O menu "APIs" lista as APIs de
// src/content/apis.js, cada uma com a página em /apis/<slug>.
import { APIS, apiHref } from './apis.js';

export const MENUS = [
  {
    label: 'APIs',
    items: APIS.map((a) => ({ title: a.name, desc: a.menuDesc, href: apiHref(a) })),
    footer: { title: 'Ver todas as APIs', href: '/apis' },
  },
];
