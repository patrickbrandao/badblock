import { Navbar } from './Navbar.jsx';
import { Footer } from '../../components/Footer.jsx';

// draft01: moldura da página inicial guardada, com a navbar do draft01.
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
