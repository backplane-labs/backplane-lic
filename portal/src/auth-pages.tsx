import { CustomerError } from './errors';
import { useEffect, useState, type FormEvent } from 'react';
import { Link, Navigate, useLocation, useNavigate } from 'react-router-dom';
import { API, APIError, basic, config, resetParameters, secretSession } from './api';
import { useAuth } from './auth';
import { Button, ErrorNotice, Field, Notice } from './components';
import { useLocale } from './i18n';

export function Login() {
  const { t } = useLocale();
  const auth = useAuth();
  const navigate = useNavigate();
  const location = useLocation();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [otp, setOTP] = useState('');
  const [otpRequired, setOTPRequired] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  if (auth.user) return <Navigate to="/licenses" replace />;
  async function submit(event: FormEvent) {
    event.preventDefault(); setError(null); setBusy(true);
    try {
      const response = await new API().request('/tokens', {
        method: 'POST', authorization: basic(email.trim(), password),
        body: { data: { type: 'tokens', attributes: { name: 'Portal session' } }, ...(otpRequired ? { meta: { otp } } : {}) },
      });
      await auth.login(secretSession(response.data));
      setPassword(''); setOTP('');
      const from = (location.state as { from?: string } | null)?.from;
      navigate(from?.startsWith('/') && !from.startsWith('//') && from !== '/login' ? from : '/licenses', { replace: true });
    } catch (cause) {
      if (cause instanceof APIError && cause.has('OTP_REQUIRED')) { setOTPRequired(true); }
      else setError(cause);
    } finally { setBusy(false); }
  }
  return <div className="auth-form"><h1>{t('loginHeading')}</h1><p className="intro">{t('loginDetail')}</p>
    <ErrorNotice error={error ?? auth.error} />
    <form onSubmit={submit}>
      <fieldset disabled={busy}>
        {!otpRequired ? <><Field label={t('email')} type="email" autoComplete="username" required value={email} onChange={e => setEmail(e.target.value)} autoFocus />
          <Field label={t('password')} type="password" autoComplete="current-password" required value={password} onChange={e => setPassword(e.target.value)} /></>
          : <><p className="signed-email">{email}</p><Field label={t('otp')} hint={t('otpHelp')} value={otp} onChange={e => setOTP(e.target.value.replace(/\D/g, ''))} inputMode="numeric" autoComplete="one-time-code" pattern="[0-9]{6}" maxLength={6} required autoFocus />
          </>}
        <Button type="submit" className="full-width">{busy ? t('working') : t('signIn')}</Button>
      </fieldset>
    </form>
    {otpRequired ? <button type="button" className="text-link auth-back" disabled={busy} onClick={() => { setOTPRequired(false); setOTP(''); setError(null); }}>{t('backLogin')}</button> : <Link className="text-link" to="/forgot">{t('forgot')}</Link>}
    <p className="session-note">{t('securityNote')}</p>
  </div>;
}
export function ForgotPassword() {
  const { t } = useLocale();
  const [email, setEmail] = useState('');
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);
  const [error, setError] = useState<unknown>(null);
  return <div className="auth-form"><h1>{t('forgotHeading')}</h1><p className="intro">{t('forgotDetail')}</p>
    <ErrorNotice error={error} />
    {done ? <Notice>{t('linkSent')}</Notice> : <form onSubmit={async event => {
      event.preventDefault(); setError(null); setBusy(true);
      try { await new API().request('/passwords', { method: 'POST', body: { meta: { email: email.trim() } } }); setDone(true); }
      catch (cause) { setError(cause); } finally { setBusy(false); }
    }}><fieldset disabled={busy}><Field label={t('email')} type="email" autoComplete="email" required value={email} onChange={e => setEmail(e.target.value)} autoFocus />
      <Button className="full-width" type="submit">{busy ? t('working') : t('sendLink')}</Button></fieldset></form>}
    <Link className="text-link" to="/login">{t('backLogin')}</Link>
  </div>;
}
export function ResetPassword() {
  const { t } = useLocale();
  const auth = useAuth();
  // Keep the reset credential in memory and remove it from the address bar/history.
  const [params] = useState(() => resetParameters(window.location.hash));
  useEffect(() => { window.history.replaceState(window.history.state, '', window.location.pathname); }, []);
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);
  const [error, setError] = useState<unknown>(null);
  if (!params.userId || !params.token || (params.account && params.account !== config.account)) return <div className="auth-form"><h1>{t('forgotHeading')}</h1><Notice error>{t('invalidReset')}</Notice><Link className="text-link" to="/forgot">{t('requestNewReset')}</Link></div>;
  return <div className="auth-form"><h1>{t('forgotHeading')}</h1><p className="intro">{t('resetDetail')}</p>
    <ErrorNotice error={error} reset />
    {done ? <Notice>{t('resetDone')}</Notice> : <form onSubmit={async event => {
      event.preventDefault(); setError(null);
      if (password !== confirm) { setError(new CustomerError('passwordsDiffer')); return; }
      setBusy(true);
      try {
        await new API().request(`/users/${encodeURIComponent(params.userId)}/actions/reset-password`, { method: 'POST', body: { meta: { passwordResetToken: params.token, newPassword: password } } });
        auth.replaceSession(null); setPassword(''); setConfirm(''); setDone(true);
      } catch (cause) { setError(cause); } finally { setBusy(false); }
    }}><fieldset disabled={busy}><Field label={t('newPassword')} hint={t('passwordHint')} type="password" autoComplete="new-password" minLength={6} required value={password} onChange={e => setPassword(e.target.value)} autoFocus />
      <Field label={t('confirmPassword')} type="password" autoComplete="new-password" required value={confirm} onChange={e => setConfirm(e.target.value)} />
      <Button className="full-width" type="submit">{busy ? t('working') : t('resetPassword')}</Button></fieldset></form>}
    <Link className="text-link" to="/login">{t('backLogin')}</Link>
  </div>;
}
