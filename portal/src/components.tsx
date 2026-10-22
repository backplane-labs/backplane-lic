import { useEffect, useId, useRef, useState, type InputHTMLAttributes, type ReactNode } from 'react';
import { Button, Input } from '@backplane-labs/backplane-ui';
import { Link } from 'react-router-dom';
import { customerErrorMessage, isInvalidReset } from './errors';
import { useLocale, type TranslationKey } from './i18n';

export { Button };
export function Field({ label, hint, ...props }: InputHTMLAttributes<HTMLInputElement> & { label: string; hint?: string }) {
  const id = useId();
  return <div className="field"><label htmlFor={id}>{label}</label><Input {...props} id={id} aria-describedby={hint ? `${id}-hint` : undefined} />{hint && <p id={`${id}-hint`} className="hint">{hint}</p>}</div>;
}
export function Notice({ children, error = false }: { children: ReactNode; error?: boolean }) {
  return <div className={`notice ${error ? 'notice-error' : ''}`} role={error ? 'alert' : 'status'}>{children}</div>;
}
export function ErrorNotice({ error, reset = false }: { error: unknown; reset?: boolean }) {
  const { t } = useLocale();
  if (!error) return null;
  return <Notice error>{customerErrorMessage(error, t, reset)}{reset && isInvalidReset(error) && <><br /><Link className="text-link" to="/forgot">{t('requestNewReset')}</Link></>}</Notice>;
}
export function Loading() {
  const { t } = useLocale();
  return <p role="status" className="loading"><span className="loading-dot" />{t('loading')}</p>;
}
export function Empty({ title, description }: { title: string; description: string }) {
  return <div className="empty"><div className="empty-symbol" aria-hidden="true">—</div><h2>{title}</h2><p>{description}</p></div>;
}
export function useLoad<T>(loader: (signal: AbortSignal) => Promise<T>, dependencies: unknown[] = []) {
  const [value, setValue] = useState<T | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [loading, setLoading] = useState(true);
  const [version, setVersion] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true); setError(null); setValue(null);
    loader(controller.signal).then(result => { if (!controller.signal.aborted) setValue(result); }).catch(cause => { if (!controller.signal.aborted) setError(cause); }).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [...dependencies, version]);
  return { value, loading, error, replace: setValue, reload: () => setVersion(x => x + 1) };
}
export function LoadError({ error, retry }: { error: unknown; retry: () => void }) {
  const { t } = useLocale();
  return <div><ErrorNotice error={error} /><Button variant="outline" onClick={retry}>{t('retry')}</Button></div>;
}
export function CopyButton({ value, label = 'copy' }: { value: string; label?: TranslationKey }) {
  const { t } = useLocale();
  const [copied, setCopied] = useState(false);
  const [error, setError] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);
  return <span className="copy-control"><Button type="button" variant="outline" onClick={async () => {
    setError(false);
    try { await navigator.clipboard.writeText(value); setCopied(true); clearTimeout(timer.current); timer.current = setTimeout(() => setCopied(false), 2200); }
    catch { setError(true); }
  }}>{copied ? t('copied') : t(label)}</Button>{error && <span className="hint" role="alert">{t('copiedError')}</span>}</span>;
}
export function Status({ value }: { value?: string }) {
  const { t } = useLocale();
  const translated: Record<string, TranslationKey> = { ACTIVE: 'active', INACTIVE: 'inactive', SUSPENDED: 'suspended', EXPIRED: 'expired', BANNED: 'banned' };
  return <span className={`status status-${value?.toLowerCase() ?? 'unknown'}`}>{value && translated[value] ? t(translated[value]) : value ?? t('unknown')}</span>;
}
export function DateValue({ value, empty = 'noExpiry' }: { value?: string | null; empty?: TranslationKey }) {
  const { locale, t } = useLocale();
  if (!value) return <>{t(empty)}</>;
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return <>{value}</>;
  return <time dateTime={value} title={date.toLocaleString(locale)}>{date.toLocaleDateString(locale, { year: 'numeric', month: 'short', day: 'numeric' })}</time>;
}
