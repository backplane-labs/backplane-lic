import { createRoot } from 'react-dom/client';
import '@backplane-labs/backplane-theme/style.css';
import '@backplane-labs/backplane-ui/style.css';
import './style.css';
import { App } from './App';
import { config } from './api';
import { applyBrand } from './brand';

applyBrand(config.brand.accentColor);

const root = document.getElementById('root');
if (root) createRoot(root).render(<App />);
