import { createRoot } from 'react-dom/client';
import '@backplane-labs/backplane-theme/style.css';
import '@backplane-labs/backplane-ui/style.css';
import './style.css';
import App from './App';
import { config } from './api';
import { applyBrand } from './brand';

applyBrand(config.brand.accentColor);

createRoot(document.getElementById('root')!).render(<App />);
