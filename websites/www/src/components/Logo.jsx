// Marca do BadBlock: o globo (landing-2/brand/globe-ralf-dots.png) e o nome "BadBlock".
export function Logo() {
  return (
    <a href="/" className="group flex items-center gap-2 mr-3" aria-label="BadBlock — página inicial">
      <img src="/landing-2/brand/globe-ralf-dots.png" alt="" width="28" height="28" className="block h-6 w-6 md:h-7 md:w-7 shrink-0" />
      <span className="text-[19px] md:text-[21px] font-semibold tracking-[-0.02em] text-gray-900 dark:text-white" style={{ fontFamily: 'var(--font-inter-display)' }}>BadBlock</span>
    </a>
  );
}
