import { Layout } from './components/Layout.jsx';
import { PageHeader } from './components/ApiParts.jsx';

// Só aparece no servidor do Vite: em produção o lighttpd responde 404 antes.
export function NotFoundPage() {
  return (
    <Layout>
      <div className="px-5 md:px-8 pb-24">
        <PageHeader title="Página não encontrada" sub="O endereço não existe. Volte para o início ou veja a lista de APIs." />
        <div className="max-w-container mx-auto flex gap-4">
          <a href="/" className="px-6 py-3 rounded-lg border border-black/[0.12] dark:border-white/[0.12] hover:bg-black/5 dark:hover:bg-white/5">Início</a>
          <a href="/apis" className="px-6 py-3 rounded-lg border border-black/[0.12] dark:border-white/[0.12] hover:bg-black/5 dark:hover:bg-white/5">APIs</a>
        </div>
      </div>
    </Layout>
  );
}
