# draft01 — página inicial guardada

Cópia da página inicial do `www` como era em 2026-10-02, antes de ela ficar
só com o Hero e o quadro de fluxo. Fica guardada para reaproveitar depois:
nenhuma rota importa estes arquivos, então eles não entram no bundle (o build
do Vite também não os verifica).

| Arquivo | O quê |
|---|---|
| `HomePage.jsx` | `Draft01HomePage`: Hero, DatasourceFlow, SectionNav, as 6 seções de `content/features.js` e FinalCta |
| `Layout.jsx` | navbar do draft01 + conteúdo + rodapé (`components/Footer.jsx`) |
| `Navbar.jsx` | navbar com o menu "APIs" (`content/nav.js`), dropdown, menu mobile e tema; 72px de altura |

Usa, sem cópia, o que continua em `src/`: `sections/` (Hero, DatasourceFlow,
SectionNav, FeatureSection, FinalCta), `content/features.js`,
`content/nav.js`, `components/` (Logo, Footer, FeatureCard, icons…) e as
imagens de `landing-2/features/`. Esses arquivos ficam no repositório por
causa do draft01; não os apague enquanto ele existir.

Para voltar a usar: em `main.jsx`, renderize `Draft01HomePage` (de
`./drafts/draft01/HomePage.jsx`) no lugar de `HomePage`.
