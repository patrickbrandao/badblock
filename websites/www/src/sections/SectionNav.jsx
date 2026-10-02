import { sections } from '../content/features.js';

const SHORT = {
  'home': 'Implantar',
  'network-and-connect': 'Rede',
  'scale-and-grow': 'Escalar',
  'monitor-and-observe': 'Monitorar',
  'improve-and-automate': 'Evoluir',
  'security-and-compliance': 'Proteger',
};

export function SectionNav() {
  return (
    <div>
      <div className="sticky top-[72px] px-5 md:px-8 pt-6 z-20 bg-oatmeal dark:bg-background">
        <nav className="max-w-container mx-auto overflow-x-auto border-b border-black/10 dark:border-white/10 hide-scrollbar scroll-p-5">
          <ul className="flex gap-8">
            {sections.map((s) => (
              <li key={s.id} className="relative">
                <a href={`#${s.id}`} className="block pb-6 text-sm opacity-50">{SHORT[s.id] ?? s.title}</a>
              </li>
            ))}
          </ul>
        </nav>
      </div>
    </div>
  );
}
