import React from 'react';
import { createRoot } from 'react-dom/client';
import { HomePage } from './HomePage.jsx';
import { ApisPage } from './ApisPage.jsx';
import { ApiPage } from './ApiPage.jsx';
import { NotFoundPage } from './NotFoundPage.jsx';
import { resolveRoute } from './routes.js';
import './styles/tokens.css';

const route = resolveRoute(window.location.pathname);
document.title = route.title;

function App() {
  switch (route.page) {
    case 'home': return <HomePage />;
    case 'apis': return <ApisPage />;
    case 'api': return <ApiPage api={route.api} />;
    default: return <NotFoundPage />;
  }
}

createRoot(document.getElementById('root')).render(<App />);
