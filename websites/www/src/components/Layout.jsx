import { Navbar } from './Navbar.jsx';
import { Footer } from './Footer.jsx';

// Moldura comum a todas as páginas: navbar, conteúdo e rodapé, no tema do site.
export function Layout({ children }) {
  return (
    <div data-overlay-container="true">
      <div className="text-foreground bg-oatmeal dark:bg-background">
        <Navbar />
        {children}
        <hr className="w-full border-t border-foreground opacity-10 my-0" />
        <Footer />
      </div>
    </div>
  );
}
