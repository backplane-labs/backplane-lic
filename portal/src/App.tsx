import { useEffect, useState, type ReactNode } from 'react';
import { BrowserRouter, Link, NavLink, Navigate, Outlet, Route, Routes, useLocation } from 'react-router-dom';
import { config } from './api';
import { AuthProvider, useAuth } from './auth';
import { ForgotPassword, Login, ResetPassword } from './auth-pages';
import { Button, Empty, ErrorNotice, Loading } from './components';
import { LicenseDetail, Licenses } from './licenses';
import { LocaleProvider, useLocale } from './i18n';
import { Profile } from './profile';
import { Tokens } from './tokens';

type Theme = 'system' | 'light' | 'dark';
function Preferences() {
  const { locale, setLocale, t } = useLocale();
  const [theme, setTheme] = useState<Theme>(() => {
    try { const value = sessionStorage.getItem('lic.portal.theme'); if (value === 'light' || value === 'dark') return value; } catch { /* optional preference */ }
    return 'system';
  });
  useEffect(() => {
    const media = window.matchMedia('(prefers-color-scheme: dark)');
    function apply() {
      const resolved = theme === 'system' ? media.matches ? 'dark' : 'light' : theme;
      document.documentElement.dataset.theme = resolved;
      document.documentElement.classList.toggle('dark', resolved === 'dark');
      document.documentElement.classList.toggle('light', resolved === 'light');
    }
    apply(); media.addEventListener('change', apply);
    try { sessionStorage.setItem('lic.portal.theme', theme); } catch { /* optional preference */ }
    return () => media.removeEventListener('change', apply);
  }, [theme]);
  return <div className="preferences"><label><span className="sr-only">{t('theme')}</span><select aria-label={t('theme')} value={theme} onChange={e => setTheme(e.target.value as Theme)}><option value="system">{t('system')}</option><option value="light">{t('light')}</option><option value="dark">{t('dark')}</option></select></label><label><span className="sr-only">{t('language')}</span><select aria-label={t('language')} value={locale} onChange={e => setLocale(e.target.value as 'en' | 'ru')}><option value="en">English</option><option value="ru">Русский</option></select></label></div>;
}
function Brand({ linked = true }: { linked?: boolean }) {
  const { t } = useLocale();
  const { brand } = config;
  const mark = brand.logoUrl
    ? <img className="brand-logo" src={brand.logoUrl} alt="" />
    : <svg className="brand-mark" width="30" height="30" viewBox="0 0 30 30" fill="none" aria-hidden="true"><rect x="1" y="1" width="28" height="28" rx="8" stroke="currentColor" strokeWidth="1.5" /><path d="M10 10h10v4H10zm0 8h6" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" /></svg>;
  const content = <>{mark}<span>{brand.name || t('portal')}</span></>;
  return linked ? <Link className="brand" to="/licenses">{content}</Link> : <div className="brand">{content}</div>;
}
function Footer() {
  const { t } = useLocale();
  const { brand } = config;
  const links = [
    brand.supportUrl && <a key="support" href={brand.supportUrl} target="_blank" rel="noopener noreferrer">{t('support')}</a>,
    brand.supportEmail && <a key="email" href={`mailto:${brand.supportEmail}`}>{brand.supportEmail}</a>,
    brand.termsUrl && <a key="terms" href={brand.termsUrl} target="_blank" rel="noopener noreferrer">{t('terms')}</a>,
    brand.privacyUrl && <a key="privacy" href={brand.privacyUrl} target="_blank" rel="noopener noreferrer">{t('privacy')}</a>,
  ].filter(Boolean);
  return <footer className="portal-footer"><div className="footer-inner"><span>© {new Date().getFullYear()} {brand.name}</span>{links.length > 0 && <nav aria-label={t('support')}>{links}</nav>}</div></footer>;
}
function AuthLayout() {
  const { t } = useLocale();
  return <div className="auth-page"><header className="auth-header"><Brand /><Preferences /></header><main className="auth-main"><aside className="auth-story"><div className="license-illustration" aria-hidden="true"><span /><span /><span /><i /></div><h2>{config.brand.tagline || t('welcome')}</h2><p>{t('welcomeDetail')}</p></aside><Outlet /></main><Footer /></div>;
}
function RequireAuth() {
  const { t } = useLocale();
  const auth = useAuth();
  const location = useLocation();
  if (auth.checking) return <div className="loading-page"><Loading /></div>;
  if (!auth.user) {
    if (auth.session && auth.error) return <div className="loading-page"><ErrorNotice error={auth.error} /><Button onClick={() => window.location.reload()}>{t('retry')}</Button></div>;
    return <Navigate to="/login" state={{ from: location.pathname }} replace />;
  }
  return <Outlet />;
}
function CustomerLayout() {
  const { t } = useLocale();
  const { user, logout } = useAuth();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const location = useLocation();
  useEffect(() => { document.querySelector<HTMLElement>('#main')?.focus(); }, [location.pathname]);
  return <div className="customer-page"><a className="skip-link" href="#main">{t('skipContent')}</a><header className="customer-header"><div className="header-inner"><Brand /><div className="header-account"><span className="user-email" title={user?.attributes.email}>{user?.attributes.email}</span><Preferences /><Button variant="outline" disabled={busy} onClick={async () => { setBusy(true); setError(null); try { await logout(); } catch (cause) { setError(cause); } finally { setBusy(false); } }}>{busy ? t('working') : t('signOut')}</Button></div></div></header>
    <nav className="main-nav" aria-label={t('portal')}><div className="nav-inner"><NavLink to="/licenses">{t('licenses')}</NavLink><NavLink to="/tokens">{t('tokens')}</NavLink><NavLink to="/profile">{t('profile')}</NavLink></div></nav>
    <main id="main" tabIndex={-1} className="page-content"><ErrorNotice error={error} /><Outlet /></main>
    <Footer />
  </div>;
}
function NotFound() {
  const { t } = useLocale();
  return <><Empty title={t('notFound')} description={t('notFoundDetail')} /><Link className="text-link" to="/licenses">{t('backLicenses')}</Link></>;
}
function RouteTree() {
  useEffect(() => { document.title = config.brand.name; }, []);
  return <Routes>
    <Route element={<AuthLayout />}><Route path="/login" element={<Login />} /><Route path="/forgot" element={<ForgotPassword />} /><Route path="/reset" element={<ResetPassword />} /></Route>
    <Route element={<RequireAuth />}><Route element={<CustomerLayout />}><Route index element={<Navigate to="/licenses" replace />} /><Route path="/licenses" element={<Licenses />} /><Route path="/licenses/:id" element={<LicenseDetail />} /><Route path="/tokens" element={<Tokens />} /><Route path="/profile" element={<Profile />} /><Route path="*" element={<NotFound />} /></Route></Route>
  </Routes>;
}
export function Providers({ children }: { children: ReactNode }) {
  return <LocaleProvider><AuthProvider>{children}</AuthProvider></LocaleProvider>;
}
export default function App() {
  return <Providers><BrowserRouter basename={config.basePath}><RouteTree /></BrowserRouter></Providers>;
}
