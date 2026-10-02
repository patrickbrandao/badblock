import { FeatureCard } from '../components/FeatureCard.jsx';
import { Icon } from '../components/icons.jsx';
import { GearLeft, GearRight } from '../components/gears.jsx';

const BANNERS = {
  'home': (
    <>O Badblock usa o <a href="https://railpack.com" target="_blank" rel="noreferrer" className="focus:outline-none focus-visible:ring-2 focus-visible:ring-pink-700 text-pink-600 hover:underline">Railpack</a> ou o seu Dockerfile para fazer o build e implantar o seu código.</>
  ),
  'network-and-connect': <>O Badblock criptografa automaticamente todo o tráfego da borda até as suas aplicações.</>,
  'scale-and-grow': (
    <>Usuários do plano <span className="text-pink-700">Enterprise</span> podem configurar até 2.400 vCPU e 2,4 TB de memória por serviço.</>
  ),
  'security-and-compliance': (
    <>O plano <span className="text-pink-700">Enterprise</span> libera <span className="text-pink-700">SSO, logs de auditoria, BYOC e VMs dedicadas</span>.</>
  ),
};

function LogoRow({ row }) {
  if (!row) return null;
  const imgs = (key) => row.logos.map((l) => (
    <img key={`${key}-${l.src}`} src={l.src} alt={l.alt} className="h-6 opacity-60" loading="lazy" title={l.title} />
  ));
  return (
    <div className="mt-8">
      <div className="hidden md:flex items-center flex-wrap gap-x-4 gap-y-2">
        <div className="flex items-center flex-nowrap">
          <div className="icon-container icon-md text-2xl text-gray-300" aria-hidden="true">
            <Icon name="refresh-cw" />
          </div>
          <p className="ml-3 text-gray-400 text-base">{row.label}</p>
          <div className="flex gap-4 ml-6">{imgs('d')}</div>
        </div>
      </div>
      <div className="flex flex-col md:hidden gap-4">
        <div className="flex items-center">
          <div className="icon-container icon-md text-2xl text-gray-300" aria-hidden="true">
            <Icon name="refresh-cw" />
          </div>
          <p className="ml-3 text-gray-400 text-base">{row.label}</p>
          <div className="flex gap-4 ml-6">{imgs('m')}</div>
        </div>
      </div>
    </div>
  );
}

export function FeatureSection({ section }) {
  const banner = BANNERS[section.id];
  return (
    <div className="px-5 md:px-8">
      <section className="relative max-w-container mx-auto my-24 md:my-56">
        <div id={section.id} className="absolute top-[-8rem]" />
        <header className="grid md:grid-cols-12 gap-x-4 gap-y-12 items-center mb-12 md:mb-12">
          <div className="md:col-span-4">
            <img src={section.imgLight} alt={section.alt} width="384" height="320" loading="eager" fetchPriority="high" className="mx-auto md:w-full md:h-full md:object-contain dark:hidden" />
            <img src={section.imgDark} alt={section.alt} width="384" height="320" loading="eager" fetchPriority="high" className="mx-auto md:w-full md:h-full md:object-contain hidden dark:block" />
          </div>
          <div className="md:col-span-8 max-w-[580px] px-0 sm:px-5 md:px-8">
            <h1 className="font-medium text-[32px] leading-[48px] mb-2" style={{ fontFamily: 'var(--font-inter-display)' }}>{section.title}</h1>
            <p className="text-lg max-w-[710px] text-gray-600 dark:text-white dark:opacity-50">{section.sub}</p>
            <LogoRow row={section.logoRow} />
          </div>
        </header>
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3 mb-9">
          {section.cards.slice(0, 3).map((c) => (
            <FeatureCard key={c.title} icon={c.icon} hue={c.hue} title={c.title} desc={c.desc} bullets={c.bullets} />
          ))}
          {banner && (
            <div className="relative col-span-1 sm:col-span-2 lg:col-span-3 px-5 py-5 border border-black/10 dark:border-white/10 bg-[#EBEAE9] dark:bg-white/[0.08] rounded-lg text-center text-sm text-gray-500">
              <GearLeft />
              <div className="relative z-10">{banner}</div>
              <GearRight />
            </div>
          )}
          {section.cards.slice(3).map((c) => (
            <FeatureCard key={c.title} icon={c.icon} hue={c.hue} title={c.title} desc={c.desc} bullets={c.bullets} />
          ))}
        </div>
      </section>
    </div>
  );
}
