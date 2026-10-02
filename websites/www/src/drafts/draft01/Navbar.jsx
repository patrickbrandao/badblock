// draft01: navbar com o menu "APIs" (content/nav.js), dropdown, menu mobile e
// tema claro/escuro, altura de 72px (py-4 + botões de 40px).
import { useState, useEffect } from 'react';
import { Logo } from '../../components/Logo.jsx';
import { Icon } from '../../components/icons.jsx';
import { MENUS } from '../../content/nav.js';

function isExternal(href) {
  return href.startsWith('http');
}

function MenuPanel({ menu, onNavigate }) {
  const wide = menu.items.length > 6;
  return (
    <div className="absolute z-50 top-full left-1/2 -translate-x-1/2 pt-1.5">
      <div className="p-2 rounded-lg border border-black/10 dark:border-white/10 bg-white dark:bg-[#13111c] shadow-xl">
        <div className={`grid gap-1.5 ${wide ? 'grid-cols-2 w-[640px]' : 'min-w-[320px]'}`}>
          {menu.items.map((it) => (
            <a
              key={it.href}
              href={it.href}
              {...(isExternal(it.href) ? { target: '_blank', rel: 'noreferrer' } : {})}
              onClick={onNavigate}
              className="group bg-secondaryBg hover:bg-gray-50 dark:hover:bg-secondaryBg lg:bg-oatmeal dark:lg:bg-background p-4 rounded-md"
            >
              <span className="block text-gray-800 font-medium text-sm md:text-base">{it.title}</span>
              {it.desc && (
                <span className="block text-gray-600 group-hover:text-gray-800 text-xs md:text-sm">{it.desc}</span>
              )}
            </a>
          ))}
        </div>
        {menu.footer && (
          <a href={menu.footer.href} onClick={onNavigate} className="block mt-1.5 px-4 py-3 rounded-md text-sm font-medium text-gray-700 hover:text-gray-900 hover:bg-gray-50 dark:hover:bg-secondaryBg">
            {menu.footer.title} →
          </a>
        )}
      </div>
    </div>
  );
}

function MobileMenu({ dark, onToggleTheme, onNavigate }) {
  return (
    <div className="lg:hidden absolute left-0 right-0 top-full z-50 px-4 pb-6 bg-oatmeal dark:bg-background border-b border-black/10 dark:border-white/10 max-h-[calc(100vh-72px)] overflow-y-auto">
      {MENUS.map((m) => (
        <div key={m.label} className="pt-4">
          <p className="px-1 pb-2 text-xs font-medium uppercase tracking-wide text-gray-500">{m.label}</p>
          <div className="grid gap-1.5 sm:grid-cols-2">
            {m.items.map((it) => (
              <a key={it.href} href={it.href} onClick={onNavigate} className="block p-3 rounded-md bg-white/60 dark:bg-white/[0.04] hover:bg-gray-50 dark:hover:bg-white/[0.08]">
                <span className="block text-gray-800 font-medium text-sm">{it.title}</span>
                {it.desc && <span className="block text-gray-600 text-xs">{it.desc}</span>}
              </a>
            ))}
          </div>
          {m.footer && (
            <a href={m.footer.href} onClick={onNavigate} className="block mt-2 px-1 py-2 text-sm font-medium text-gray-700 hover:text-gray-900">{m.footer.title} →</a>
          )}
        </div>
      ))}
      <button onClick={onToggleTheme} className="mt-4 flex items-center gap-2 px-1 py-2 text-sm font-medium text-gray-700 hover:text-gray-900">
        {dark ? <Icon name="sun" /> : <Icon name="moon" />}
        <span>{dark ? 'Tema claro' : 'Tema escuro'}</span>
      </button>
    </div>
  );
}

function applyTheme(dark) {
  const c = document.documentElement.classList;
  c.toggle('dark', dark);
  c.toggle('light', !dark);
  document.documentElement.style.colorScheme = dark ? 'dark' : 'light';
}

export function Navbar() {
  const [open, setOpen] = useState(null);
  const [dark, setDark] = useState(false);
  const [mobileOpen, setMobileOpen] = useState(false);

  useEffect(() => {
    const saved = localStorage.getItem('theme');
    const prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
    const initialDark = saved ? saved === 'dark' : prefersDark;
    setDark(initialDark);
    applyTheme(initialDark);
  }, []);

  const toggleTheme = () => {
    const next = !dark;
    setDark(next);
    localStorage.setItem('theme', next ? 'dark' : 'light');
    applyTheme(next);
  };

  return (
    <div className="text-foreground bg-oatmeal dark:bg-background">
      <div className="sticky top-0 z-[9999] py-4 px-4 xl:px-0 bg-oatmeal dark:bg-background">
        <div className="absolute left-0 right-0 top-full h-32 pointer-events-none bg-gradient-to-b from-black/[0.04] dark:from-black/[0.35] to-transparent transition-opacity duration-300 opacity-100"></div>
        <div className="max-w-container w-full mx-auto">
          <div className="flex justify-between items-center">
            <Logo />
            <div className="hidden lg:block">
              <nav aria-label="Principal" data-orientation="horizontal" dir="ltr" className="relative z-50 flex justify-center">
                <div style={{ position: 'relative' }}>
                  <ul data-orientation="horizontal" className="flex items-center gap-2" dir="ltr">
                    {MENUS.map((m) => (
                      <li key={m.label} className="relative" onMouseEnter={() => setOpen(m.label)} onMouseLeave={() => setOpen(null)}>
                        <button
                          aria-expanded={open === m.label}
                          className="group flex items-center gap-2 py-1.5 px-3 rounded-md data-[state=open]:bg-gray-50"
                          data-state={open === m.label ? 'open' : 'closed'}
                          onClick={() => setOpen(open === m.label ? null : m.label)}
                        >
                          <span className="text-gray-800 group-data-[state=open]:text-gray-900 text-sm font-medium">{m.label}</span>
                          <div className="icon-container icon-xs text-lg text-gray-600 relative transition-transform ease-in group-data-[state=open]:-rotate-180" aria-hidden="true">
                            <Icon name="chevron-down" />
                          </div>
                        </button>
                        {open === m.label && <MenuPanel menu={m} onNavigate={() => setOpen(null)} />}
                      </li>
                    ))}
                  </ul>
                </div>
              </nav>
            </div>
            <div className="flex space-x-3 items-center">
              <button
                onClick={toggleTheme}
                className="hidden md:flex md:items-center justify-center p-2 rounded-md hover:bg-gray-50 dark:hover:bg-white/5 h-10 w-10"
                aria-label={dark ? 'Mudar para o tema claro' : 'Mudar para o tema escuro'}
              >
                {dark ? <Icon name="sun" /> : <Icon name="moon" />}
              </button>
              <button
                className="group/button flex items-center justify-center border transform transition-transform duration-50 active:scale-95 focus:outline-none focus-visible:ring-2 h-[34px] py-1.5 rounded-md text-sm leading-5 space-x-2 w-[34px] px-0 lg:hidden"
                title={mobileOpen ? 'Fechar o menu' : 'Abrir o menu'}
                aria-label={mobileOpen ? 'Fechar o menu' : 'Abrir o menu'}
                aria-expanded={mobileOpen}
                onClick={() => setMobileOpen(!mobileOpen)}
              >
                <div className="text-current icon-container icon-md text-2xl !w-4 !h-4 h-4 w-4" aria-hidden="true">
                  <svg width="1em" height="1em" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
                    <path d="M4.5 9h15m-15 6h15" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
                  </svg>
                </div>
              </button>
            </div>
          </div>
        </div>
        {mobileOpen && <MobileMenu dark={dark} onToggleTheme={toggleTheme} onNavigate={() => setMobileOpen(false)} />}
      </div>
    </div>
  );
}
