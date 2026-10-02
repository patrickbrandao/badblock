// Página inicial: Hero e o quadro de fluxo, entre a navbar e o rodapé. A
// versão anterior (com SectionNav, as 6 seções e FinalCta) está guardada em
// drafts/draft01/.
import { Layout } from './components/Layout.jsx';
import { Hero } from './sections/Hero.jsx';
import { DatasourceFlow } from './sections/DatasourceFlow.jsx';

export function HomePage() {
  return (
    <Layout>
      <Hero />
      <DatasourceFlow />
    </Layout>
  );
}
