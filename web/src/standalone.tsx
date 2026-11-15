import 'virtual:backplane-grafana-assets';
import { editorsEnglish } from '@backplane-labs/backplane-editors';
import { platformEnglish } from '@backplane-labs/backplane-platform-ui';
import { installPluginTranslations, PluginProvider } from '@backplane-labs/backplane-plugin-sdk';
import { BackplaneProvider } from '@backplane-labs/backplane-react';
import { schemaFormsEnglish } from '@backplane-labs/backplane-schema-forms';
import { createGrafanaTheme, type ThemeMode } from '@backplane-labs/backplane-theme';
import { Badge, Button, englishResources, SelectControl } from '@backplane-labs/backplane-ui';
import { ThemeContext } from '@grafana/ui';
import i18next from 'i18next';
import { useLayoutEffect, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { I18nextProvider, initReactI18next, useTranslation } from 'react-i18next';
import { BrowserRouter, Route, Routes } from 'react-router-dom';
import Embedded from './embedded';
import { fixtureClient } from './fixtures';
import ru, { schemaFormsRussian } from './locales/ru';
import navigation from './navigation';
import '@backplane-labs/backplane-ui/style.css';
import '@backplane-labs/backplane-theme/style.css';
import '@fontsource/ibm-plex-sans/400.css';
import '@fontsource/ibm-plex-sans/500.css';
import '@fontsource/ibm-plex-sans/600.css';
import '@fontsource/ibm-plex-mono/400.css';
import './standalone.css';

const i18n = i18next.createInstance();
await i18n.use(initReactI18next).init({
  lng: 'en',
  fallbackLng: 'en',
  supportedLngs: ['en', 'ru'],
  interpolation: { escapeValue: false },
  resources: {
    en: {
      'backplane.ui': englishResources,
      'backplane.schemaForms': schemaFormsEnglish,
      'backplane.editors': editorsEnglish,
      'backplane.platform': platformEnglish,
    },
    ru: { 'module.lic': ru, 'backplane.schemaForms': schemaFormsRussian },
  },
});
installPluginTranslations(i18n, navigation);
function Standalone() {
  const { t } = useTranslation('module.lic'),
    [mode, setMode] = useState<ThemeMode>('dark');
  useLayoutEffect(() => {
    document.documentElement.dataset.theme = mode;
    document.documentElement.classList.toggle('dark', mode === 'dark');
  }, [mode]);
  return (
    <ThemeContext.Provider value={createGrafanaTheme(mode)}>
      <BackplaneProvider client={fixtureClient}>
        <PluginProvider context={{ service: 'lic', basePath: '/', mode, environment: 'standalone' }}>
          <header>
            <span className="module-brand">
              {t('title')}
              <Badge variant="outline">{t('standalone')}</Badge>
            </span>
            <SelectControl
              aria-label="Language / Язык"
              value={i18n.language}
              options={[
                { value: 'en', label: 'English' },
                { value: 'ru', label: 'Русский' },
              ]}
              onValueChange={(language) => void i18n.changeLanguage(language)}
            />
            <Button variant="outline" size="sm" onClick={() => setMode(mode === 'dark' ? 'light' : 'dark')}>
              {t(mode === 'dark' ? 'light' : 'dark')}
            </Button>
          </header>
          <main>
            <Routes>
              <Route path="/*" element={<Embedded />} />
            </Routes>
          </main>
        </PluginProvider>
      </BackplaneProvider>
    </ThemeContext.Provider>
  );
}
createRoot(document.getElementById('root')!).render(
  <I18nextProvider i18n={i18n}>
    <BrowserRouter>
      <Standalone />
    </BrowserRouter>
  </I18nextProvider>,
);
