import { Icon } from './icons.jsx';

export const cardClass = 'relative w-full bg-white dark:bg-white dark:bg-opacity-[1.5%] border border-gray-200 dark:border-white dark:border-opacity-[8%] rounded-lg';

export function PageHeader({ eyebrow, title, sub }) {
  return (
    <header className="w-full max-w-container mx-auto pt-20 md:pt-28 pb-12">
      {eyebrow}
      <h1 className="text-[36px] md:text-[48px] lg:text-[56px] leading-tight font-plexSerif font-medium tracking-[-0.025em]">{title}</h1>
      {sub && (
        <p className="mt-4 text-[18px] sm:text-[20px] leading-[28px] sm:leading-[32px] tracking-[-0.0125em] max-w-[720px] text-gray-600 dark:text-white dark:opacity-[45%]">{sub}</p>
      )}
    </header>
  );
}

export function SectionTitle({ children, id }) {
  return (
    <h2 id={id} className="font-medium text-[24px] md:text-[28px] leading-[1.4] mb-6" style={{ fontFamily: 'var(--font-inter-display)' }}>{children}</h2>
  );
}

export function IconBox({ name, hue = 262 }) {
  return (
    <div className="shrink-0 text-current icon-container icon-40 text-2xl border border-black/10 dark:border-white/10 p-[10px] rounded-lg" style={{ backgroundColor: `hsla(${hue}, 65%, 46%, 0.25)`, color: 'white' }} aria-hidden="true">
      <Icon name={name} />
    </div>
  );
}

export function Code({ children }) {
  return <code className="font-mono text-[13px] md:text-sm break-all">{children}</code>;
}
