import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// No servidor de desenvolvimento, "/" e as páginas "/apis..." abrem o
// index.src.html, como faz o url.rewrite dos lighttpd*.conf em produção.
const pageRoutes = {
  name: 'badblock-page-routes',
  configureServer(server) {
    server.middlewares.use((req, _res, next) => {
      const [path, query] = req.url.split('?');
      if (path === '/' || /^\/apis(\/[a-z0-9/]+)?\/?$/.test(path)) {
        req.url = '/index.src.html' + (query ? `?${query}` : '');
      }
      next();
    });
  },
};

export default defineConfig({
  plugins: [react(), pageRoutes],
  base: '/',
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    rollupOptions: { input: 'index.src.html' },
  },
  publicDir: false,
});
