import { CustomerError } from './errors';
import { useEffect, useState, type FormEvent } from 'react';
import QRCode from 'qrcode';
import { useAuth } from './auth';
import { Button, CopyButton, ErrorNotice, Field, LoadError, Loading, Notice, useLoad } from './components';
import { useLocale } from './i18n';

function PasswordForm() {
  const { t } = useLocale();
  const { api, user } = useAuth();
  const [oldPassword, setOldPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [done, setDone] = useState(false);
  async function update(event: FormEvent) {
    event.preventDefault(); setError(null); setDone(false);
    if (newPassword !== confirm) { setError(new CustomerError('passwordsDiffer')); return; }
    setBusy(true);
    try {
      await api.request(`/users/${encodeURIComponent(user!.id)}/actions/update-password`, { method: 'POST', body: { meta: { oldPassword, newPassword } } });
      setOldPassword(''); setNewPassword(''); setConfirm(''); setDone(true);
    } catch (cause) { setError(cause); } finally { setBusy(false); }
  }
  return <section className="panel security-section"><div><h2>{t('changePassword')}</h2><p className="section-description">{t('passwordHint')}</p></div><form onSubmit={update}><ErrorNotice error={error} />{done && <Notice>{t('passwordUpdated')}</Notice>}<fieldset disabled={busy}>
    <Field label={t('oldPassword')} type="password" autoComplete="current-password" required value={oldPassword} onChange={e => setOldPassword(e.target.value)} />
    <Field label={t('newPassword')} type="password" autoComplete="new-password" required minLength={6} value={newPassword} onChange={e => setNewPassword(e.target.value)} />
    <Field label={t('confirmPassword')} type="password" autoComplete="new-password" required value={confirm} onChange={e => setConfirm(e.target.value)} />
    <Button type="submit">{busy ? t('working') : t('updatePassword')}</Button>
  </fieldset></form></section>;
}
function QR({ uri }: { uri: string }) {
  const { t } = useLocale();
  const [image, setImage] = useState('');
  const [error, setError] = useState<unknown>(null);
  useEffect(() => {
    let active = true;
    // Generate locally. Sending the provisioning URI to a QR service would leak the TOTP secret.
    QRCode.toDataURL(uri, { width: 224, margin: 2, errorCorrectionLevel: 'M' }).then(value => { if (active) setImage(value); }).catch(cause => { if (active) setError(cause); });
    return () => { active = false; };
  }, [uri]);
  return <>{image && <img className="totp-qr" src={image} width={224} height={224} alt={t('qrAlt')} />}<ErrorNotice error={error} /></>;
}
function SecondFactor() {
  const { t } = useLocale();
  const { api, user } = useAuth();
  const path = `/users/${encodeURIComponent(user!.id)}/second-factors`;
  const factors = useLoad(signal => api.list(path, signal), [api, path]);
  const factor = factors.value?.[0];
  const [password, setPassword] = useState('');
  const [otp, setOTP] = useState('');
  const [begin, setBegin] = useState(false);
  const [disabling, setDisabling] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [message, setMessage] = useState('');
  async function create(event: FormEvent) {
    event.preventDefault(); setBusy(true); setError(null); setMessage('');
    try { await api.request(path, { method: 'POST', body: { meta: { password } } }); setPassword(''); setBegin(false); factors.reload(); }
    catch (cause) { setError(cause); } finally { setBusy(false); }
  }
  async function confirm(event: FormEvent) {
    event.preventDefault(); setBusy(true); setError(null); setMessage('');
    try {
      const response = await api.request(`${path}/${encodeURIComponent(factor!.id)}`, {
        method: disabling ? 'DELETE' : 'PATCH',
        body: disabling ? { meta: { otp } } : { data: { type: 'second-factors', id: factor!.id, attributes: { enabled: true } }, meta: { otp } },
      });
      factors.replace(disabling ? [] : [response.data]);
      setOTP(''); setDisabling(false); setMessage(t(disabling ? 'factorDisabled' : 'factorEnabled')); factors.reload();
    } catch (cause) { setError(cause); } finally { setBusy(false); }
  }
  return <section className="panel security-section"><div><h2>{t('twoFactor')}</h2><p className="section-description">{t('twoFactorDetail')}</p>{!factors.loading && !factors.error && <span className={`status ${factor?.attributes.enabled ? 'status-active' : ''}`}>{t(factor?.attributes.enabled ? 'enabled' : 'notEnabled')}</span>}</div>
    <div>{factors.loading ? <Loading /> : factors.error ? <LoadError error={factors.error} retry={factors.reload} /> : <>
      <ErrorNotice error={error} />{message && <Notice>{message}</Notice>}
      {!factor && (begin ? <form onSubmit={create}><p className="section-description">{t('passwordVerify')}</p><fieldset disabled={busy}><Field label={t('oldPassword')} type="password" autoComplete="current-password" required value={password} onChange={e => setPassword(e.target.value)} autoFocus /><div className="button-row"><Button type="submit">{busy ? t('working') : t('enable2FA')}</Button><Button type="button" variant="outline" onClick={() => { setBegin(false); setPassword(''); setError(null); }}>{t('cancel')}</Button></div></fieldset></form> : <Button onClick={() => { setBegin(true); setMessage(''); setError(null); }}>{t('enable2FA')}</Button>)}
      {factor && !factor.attributes.enabled && <div className="totp-setup"><p>{t('setupDetail')}</p>{factor.attributes.uri && <QR uri={factor.attributes.uri} />}
        <div className="field"><label>{t('manualSecret')}</label><code className="secret-value" tabIndex={0}>{factor.attributes.secret}</code><CopyButton value={factor.attributes.secret ?? ''} label="copySetupKey" /></div>
        <form onSubmit={confirm}><fieldset disabled={busy}><Field label={t('otp')} hint={t('otpHelp')} value={otp} onChange={e => setOTP(e.target.value.replace(/\D/g, ''))} pattern="[0-9]{6}" maxLength={6} inputMode="numeric" autoComplete="one-time-code" required /><div className="button-row"><Button type="submit">{busy ? t('working') : t('confirmSetup')}</Button><Button type="button" variant="outline" onClick={async () => {
          setBusy(true); setError(null);
          try { await api.request(`${path}/${encodeURIComponent(factor.id)}`, { method: 'DELETE' }); setOTP(''); factors.reload(); } catch (cause) { setError(cause); } finally { setBusy(false); }
        }}>{t('discardSetup')}</Button></div></fieldset></form>
      </div>}
      {factor?.attributes.enabled && (disabling ? <form onSubmit={confirm}><p className="section-description">{t('disableDetail')}</p><fieldset disabled={busy}><Field label={t('otp')} value={otp} onChange={e => setOTP(e.target.value.replace(/\D/g, ''))} pattern="[0-9]{6}" maxLength={6} inputMode="numeric" autoComplete="one-time-code" required autoFocus /><div className="button-row"><Button variant="destructive" type="submit">{busy ? t('working') : t('disable2FA')}</Button><Button variant="outline" type="button" onClick={() => { setDisabling(false); setOTP(''); setError(null); }}>{t('cancel')}</Button></div></fieldset></form> : <Button variant="outline" onClick={() => { setDisabling(true); setMessage(''); }}>{t('disable2FA')}</Button>)}
    </>}</div>
  </section>;
}
export function Profile() {
  const { t } = useLocale();
  const { user } = useAuth();
  return <><header className="page-header"><h1>{t('profile')}</h1><p>{t('profileDetail')}</p></header><section className="panel account-panel"><h2>{t('account')}</h2><p className="account-name">{user?.attributes.fullName ?? user?.attributes.email}</p>{user?.attributes.fullName && <p className="muted">{user.attributes.email}</p>}</section><PasswordForm /><SecondFactor /></>;
}
