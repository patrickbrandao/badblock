import { Layout } from './components/Layout.jsx';
import { PageHeader, SectionTitle, IconBox, Code, cardClass } from './components/ApiParts.jsx';
import { Icon } from './components/icons.jsx';
import { APIS, API_HOST, COMMON_ROUTES, apiHref } from './content/apis.js';

function InfoCard({ icon, label, children }) {
  return (
    <div className={`${cardClass} p-6 flex gap-4 items-start`}>
      <IconBox name={icon} />
      <div className="min-w-0">
        <p className="text-sm text-gray-500 mb-1">{label}</p>
        <div className="text-gray-900 dark:text-white">{children}</div>
      </div>
    </div>
  );
}

function RouteCard({ route }) {
  const url = route.example ? `${API_HOST}${route.example}` : null;
  return (
    <div className={`${cardClass} p-6 md:p-8`}>
      <div className="flex flex-wrap items-center gap-3">
        <span className="font-mono text-xs font-semibold px-2 py-1 rounded-md border border-black/10 dark:border-white/10" style={{ backgroundColor: 'hsla(152, 65%, 46%, 0.18)' }}>GET</span>
        <Code>{route.path}</Code>
      </div>
      <p className="mt-3 text-base text-gray-600 dark:text-white dark:opacity-75">{route.desc}</p>
      {url && (
        <div className="mt-4 rounded-md border border-black/10 dark:border-white/10 bg-black/[0.03] dark:bg-white/[0.04] px-4 py-3 overflow-x-auto">
          <p className="text-xs text-gray-500 mb-1">Exemplo</p>
          <a href={url} target="_blank" rel="noreferrer" className="font-mono text-[13px] md:text-sm text-gray-800 hover:text-gray-950 hover:underline break-all">curl {url}</a>
        </div>
      )}
    </div>
  );
}

export function ApiPage({ api }) {
  const base = `${API_HOST}${api.path}/`;
  const openapi = `${API_HOST}${api.path}/openapi.yaml`;
  const others = APIS.filter((a) => a.slug !== api.slug);
  return (
    <Layout>
      <div className="px-5 md:px-8">
        <PageHeader
          eyebrow={(
            <nav aria-label="Trilha" className="mb-6 text-sm text-gray-500">
              <a href="/apis" className="hover:text-gray-900 hover:underline">APIs</a>
              <span className="mx-2" aria-hidden="true">/</span>
              <span className="text-gray-700">{api.name}</span>
            </nav>
          )}
          title={api.name}
          sub={api.summary}
        />

        <section className="max-w-container mx-auto grid grid-cols-1 md:grid-cols-3 gap-3 mb-20">
          <InfoCard icon="link" label="Caminho base">
            <a href={base} target="_blank" rel="noreferrer" className="hover:underline"><Code>{base}</Code></a>
          </InfoCard>
          <InfoCard icon="file-text" label="Manifesto OpenAPI 3.1">
            <a href={openapi} target="_blank" rel="noreferrer" className="hover:underline"><Code>{api.path}/openapi.yaml</Code></a>
          </InfoCard>
          <InfoCard icon="database" label="Fonte dos dados">
            <a href={api.source} target="_blank" rel="noreferrer" className="hover:underline"><Code>{api.source}</Code></a>
          </InfoCard>
        </section>

        <section className="max-w-container mx-auto grid md:grid-cols-12 gap-x-8 gap-y-10 mb-20">
          <div className="md:col-span-7">
            <SectionTitle>Sobre a fonte</SectionTitle>
            {api.about.map((p) => (
              <p key={p} className="text-lg text-gray-600 dark:text-white dark:opacity-75 mb-4">{p}</p>
            ))}
          </div>
          <div className="md:col-span-5">
            <SectionTitle>Dados oferecidos</SectionTitle>
            <ul className={`${cardClass} flex flex-col gap-3 p-6`}>
              {api.data.map((d) => (
                <li key={d} className="grid gap-3 items-center grid-cols-[auto_1fr] text-gray-600 dark:text-white dark:opacity-75">
                  <div className="text-current icon-container icon-md text-2xl" aria-hidden="true"><Icon name="check" /></div>
                  <p className="text-sm">{d}</p>
                </li>
              ))}
            </ul>
          </div>
        </section>

        <section className="max-w-container mx-auto mb-20">
          <SectionTitle>Principais rotas</SectionTitle>
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
            {api.routes.map((r) => <RouteCard key={r.path} route={r} />)}
          </div>
          <p className="mt-6 text-sm text-gray-500 max-w-[760px]">
            Respostas em JSON, com ETag e cache. As rotas de dados, o índice e /meta respondem também em <Code>{api.path}/v1/…</Code>, que fixa a versão 1; sem o <Code>/v1</Code>, vale a versão atual.
          </p>
        </section>

        <section className="max-w-container mx-auto mb-20">
          <SectionTitle>Rotas comuns</SectionTitle>
          <div className={`${cardClass} divide-y divide-black/10 dark:divide-white/10`}>
            {COMMON_ROUTES.map((r) => (
              <div key={r.path} className="grid md:grid-cols-[minmax(0,280px)_1fr] gap-1 md:gap-6 px-6 py-4">
                <Code>{api.path}{r.path}</Code>
                <p className="text-sm text-gray-600 dark:text-white dark:opacity-75">{r.desc}</p>
              </div>
            ))}
          </div>
        </section>

        <section className="max-w-container mx-auto mb-24">
          <SectionTitle>Outras APIs</SectionTitle>
          <div className="flex flex-wrap gap-2">
            {others.map((a) => (
              <a key={a.slug} href={apiHref(a)} className="px-4 py-2 rounded-md border border-black/[0.12] dark:border-white/[0.12] text-sm hover:bg-black/5 dark:hover:bg-white/5">{a.name}</a>
            ))}
          </div>
        </section>
      </div>
    </Layout>
  );
}
