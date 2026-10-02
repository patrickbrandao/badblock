import { Logo } from './Logo.jsx';
import { APIS, API_GROUPS, API_HOST, apiHref } from '../content/apis.js';

const GROUPS = [
  ...API_GROUPS.map((g) => ({
    head: g.label,
    links: APIS.filter((a) => a.group === g.id).map((a) => ({ href: apiHref(a), text: a.name })),
  })),
  {
    head: 'Projeto',
    links: [
      { href: '/', text: 'Início' },
      { href: '/apis', text: 'Todas as APIs' },
      { href: `${API_HOST}/`, text: 'api.badblock.net.br' },
    ],
  },
];

export function Footer() {
  return (
    <footer className="max-w-7xl mx-auto pt-12 pb-24 px-12 2xl:px-0">
      <div className="grid grid-cols-1 lg:grid-cols-5 gap-y-12 lg:gap-y-0 lg:gap-x-32">
        <div className="lg:col-span-2 hidden sm:flex flex-col justify-between">
          <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-1">
            <p className="text-sm font-medium text-gray-950 md:col-span-2 lg:col-span-1">Destaque</p>
            <a href="/apis" aria-label="Dados públicos da internet em APIs abertas. Conheça as APIs." className="py-4 px-6 bg-white/30 dark:bg-gray-50 border border-black/10 dark:border-white/10 rounded-xl hover:border-black/20 hover:dark:border-white/10 group relative">
              <div className="flex flex-col space-y-1 w-full relative z-10">
                <p className="text-sm font-medium text-gray-700 group-hover:text-gray-900">Dados públicos da internet <span className="dark:text-[#a78bfa]">em APIs abertas</span></p>
                <p className="text-sm text-gray-600">ASNs, blocos IP, a raiz do DNS e as prestadoras da Anatel, coletados das fontes oficiais e servidos em JSON. <span className="underline decoration-solid underline-offset-2 group-hover:text-gray-900">Conheça as APIs →</span></p>
              </div>
            </a>
          </div>
        </div>
        <div className="mx-auto xs:mx-0 lg:col-span-3 grid grid-cols-2 xs:grid-cols-3 gap-x-8 gap-y-12">
          {GROUPS.map((g) => (
            <div key={g.head} className="flex flex-col space-y-4">
              <p className="text-sm font-medium text-gray-950">{g.head}</p>
              <div className="flex flex-col space-y-3">
                {g.links.map((l) => (
                  <a
                    key={l.href + l.text}
                    href={l.href}
                    {...(l.href.startsWith('http') ? { target: '_blank', rel: 'noreferrer' } : {})}
                    className="text-gray-600 hover:text-gray-950 max-w-max"
                  >
                    <p className="text-sm">{l.text}</p>
                  </a>
                ))}
              </div>
            </div>
          ))}
        </div>
      </div>
      <div className="flex flex-col gap-y-4 lg:gap-y-0 lg:flex-row items-center justify-between mt-12">
        <div className="flex flex-col lg:flex-row items-center gap-y-4 lg:gap-y-0 lg:gap-x-8">
          <Logo />
        </div>
        <div className="text-xs text-gray-500">© 2026 Badblock. Uma iniciativa TMSoft, todos os direitos reservados.</div>
      </div>
    </footer>
  );
}
