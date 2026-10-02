import { Layout } from './components/Layout.jsx';
import { PageHeader, SectionTitle, IconBox, cardClass } from './components/ApiParts.jsx';
import { APIS, API_GROUPS, API_HOST, apiHref } from './content/apis.js';

const GROUP_ICON = { rir: 'globe', dns: 'server' };

export function ApisPage() {
  return (
    <Layout>
      <div className="px-5 md:px-8">
        <PageHeader
          title="APIs do Badblock"
          sub="Dados públicos da internet — ASNs, blocos IP, a raiz do DNS e as prestadoras de telecomunicações — coletados das fontes oficiais, atualizados sozinhos e servidos em JSON, cada um num caminho de api.badblock.net.br."
        />
        {API_GROUPS.map((g) => (
          <section key={g.id} className="max-w-container mx-auto mb-20">
            <SectionTitle>{g.label}</SectionTitle>
            <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
              {APIS.filter((a) => a.group === g.id).map((a) => (
                <a key={a.slug} href={apiHref(a)} className={`${cardClass} group block p-6 md:p-8 hover:border-gray-300 dark:hover:border-opacity-[16%]`}>
                  <IconBox name={GROUP_ICON[g.id]} hue={g.id === 'rir' ? 272 : 322} />
                  <p className="text-h5 font-semibold mt-4 mb-1 text-gray-900 dark:text-white">{a.name}</p>
                  <p className="font-mono text-xs text-gray-500 mb-3">{API_HOST.replace('https://', '')}{a.path}/</p>
                  <p className="text-base text-gray-600 dark:text-white dark:opacity-75">{a.menuDesc}</p>
                  <span className="inline-block mt-6 text-sm text-gray-700 group-hover:text-gray-900 underline decoration-solid underline-offset-2">Ver a API →</span>
                </a>
              ))}
            </div>
          </section>
        ))}
      </div>
    </Layout>
  );
}
