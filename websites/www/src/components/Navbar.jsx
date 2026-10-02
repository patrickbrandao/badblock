import { useState, useEffect } from 'react';
import { Logo } from './Logo.jsx';
import { Icon } from './icons.jsx';

// Navbar de todas as páginas: só a marca e o botão de tema claro/escuro, com
// 58px de altura (80% dos 72px da navbar anterior, guardada em
// drafts/draft01/Navbar.jsx com o menu "APIs").

function applyTheme(dark) {
  const c = document.documentElement.classList;
  c.toggle('dark', dark);
  c.toggle('light', !dark);
  document.documentElement.style.colorScheme = dark ? 'dark' : 'light';
}

export function Navbar() {
  const [dark, setDark] = useState(false);

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
      <div className="sticky top-0 z-[9999] h-[58px] flex items-center px-4 xl:px-0 bg-oatmeal dark:bg-background">
        <div className="absolute left-0 right-0 top-full h-32 pointer-events-none bg-gradient-to-b from-black/[0.04] dark:from-black/[0.35] to-transparent transition-opacity duration-300 opacity-100"></div>
        <div className="max-w-container w-full mx-auto">
          <div className="flex justify-between items-center">
            <Logo />
            <button
              onClick={toggleTheme}
              className="flex items-center justify-center p-2 rounded-md hover:bg-gray-50 dark:hover:bg-white/5 h-10 w-10"
              aria-label={dark ? 'Mudar para o tema claro' : 'Mudar para o tema escuro'}
            >
              {dark ? <Icon name="sun" /> : <Icon name="moon" />}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
