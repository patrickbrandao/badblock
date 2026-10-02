/** Tailwind: breakpoints padrão + xs (500px); as cores gray/pink/green e os
 *  fundos usam os tokens de src/styles/tokens.css, que mudam com o tema
 *  (.light/.dark). */
const scale = (name) => Object.fromEntries(
  [50, 100, 200, 300, 400, 500, 600, 700, 800, 900, 950].map((n) => [n, `var(--${name}-${n})`]),
);

/** @type {import('tailwindcss').Config} */
export default {
  content: ['./src/**/*.{js,jsx}', './index.src.html'],
  darkMode: 'class',
  theme: {
    extend: {
      screens: { xs: '500px' },
      maxWidth: { container: '1160px' },
      colors: {
        background: 'var(--background)',
        foreground: 'var(--foreground)',
        oatmeal: 'var(--bg-oatmeal)',
        secondaryBg: 'var(--secondaryBg)',
        gray: scale('gray'),
        pink: scale('pink'),
        green: scale('green'),
      },
    },
  },
  plugins: [],
};
