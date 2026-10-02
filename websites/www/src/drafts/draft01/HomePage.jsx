// draft01: página inicial do www como era em 2026-10-02 (Hero, quadro de fluxo,
// SectionNav, 6 seções e FinalCta). Guardada para uso futuro: nenhuma rota a
// importa, então não entra no bundle. Ver README.md desta pasta.
import { Layout } from './Layout.jsx';
import { Hero } from '../../sections/Hero.jsx';
import { DatasourceFlow } from '../../sections/DatasourceFlow.jsx';
import { SectionNav } from '../../sections/SectionNav.jsx';
import { FeatureSection } from '../../sections/FeatureSection.jsx';
import { FinalCta } from '../../sections/FinalCta.jsx';
import { sections } from '../../content/features.js';

export function Draft01HomePage() {
  return (
    <Layout>
      <Hero />
      <DatasourceFlow />
      <SectionNav />
      {sections.map((s) => <FeatureSection key={s.id} section={s} />)}
      <FinalCta />
    </Layout>
  );
}
