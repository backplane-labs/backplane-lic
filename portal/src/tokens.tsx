import { useState, type FormEvent } from 'react';
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@backplane-labs/backplane-ui';
import { secretSession, type Resource } from './api';
import { useAuth } from './auth';
import { Button, CopyButton, DateValue, Empty, ErrorNotice, Field, LoadError, Loading, Notice, useLoad } from './components';
import { useLocale } from './i18n';

type Action = { kind: 'revoke' | 'regenerate'; token: Resource };
export function Tokens() {
  const { t } = useLocale();
  const { api, session, replaceSession } = useAuth();
  const result = useLoad(signal => api.list('/tokens', signal), [api]);
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState('');
  const [expiry, setExpiry] = useState('');
  const [action, setAction] = useState<Action | null>(null);
  const [secret, setSecret] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [message, setMessage] = useState('');
  async function create(event: FormEvent) {
    event.preventDefault(); setError(null); setBusy(true); setMessage('');
    try {
      const attributes = { name: name.trim(), ...(expiry ? { expiry: new Date(expiry).toISOString() } : {}) };
      // The nested /users/:id/tokens route is admin-only. Users generate their own
      // tokens through the public /tokens route, with no bearer override.
      const response = await api.request('/tokens', { method: 'POST', body: { data: { type: 'tokens', attributes } } });
      setSecret(secretSession(response.data).token); setCreating(false); setName(''); setExpiry(''); setMessage(t('tokenCreated')); result.reload();
    } catch (cause) { setError(cause); } finally { setBusy(false); }
  }
  async function confirmAction() {
    if (!action) return;
    setBusy(true); setError(null); setMessage('');
    try {
      const path = `/tokens/${encodeURIComponent(action.token.id)}`;
      if (action.kind === 'regenerate') {
        const response = await api.request(path, { method: 'PUT' });
        const next = secretSession(response.data);
        // Regenerating this tab's token invalidates the old secret immediately.
        if (action.token.id === session?.tokenId) replaceSession(next);
        setSecret(next.token); setMessage(t('tokenRegenerated'));
      } else {
        await api.request(path, { method: 'DELETE' });
        if (action.token.id === session?.tokenId) { replaceSession(null); return; }
        setMessage(t('tokenRevoked'));
      }
      setAction(null); result.reload();
    } catch (cause) { setError(cause); } finally { setBusy(false); }
  }
  return <><header className="page-header page-header-inline"><div><h1>{t('tokens')}</h1><p>{t('tokensDetail')}</p></div><Button onClick={() => { setCreating(true); setError(null); setMessage(''); }}>{t('createToken')}</Button></header>
    {message && <Notice>{message}</Notice>}
    {!creating && !action && <ErrorNotice error={error} />}
    {result.loading ? <Loading /> : result.error ? <LoadError error={result.error} retry={result.reload} /> : !result.value?.length ? <Empty title={t('noTokens')} description={t('noTokensDetail')} /> :
      <div className="token-list">{result.value.map(token => <article className="token-row" key={token.id}>
        <div className="token-info"><div className="token-name">{token.attributes.name || t('unnamed')}{token.id === session?.tokenId && <span className="session-badge">{t('current')}</span>}</div><dl className="token-dates"><div><dt>{t('created')}</dt><dd><DateValue value={token.attributes.created} empty="unknown" /></dd></div><div><dt>{t('expiry')}</dt><dd><DateValue value={token.attributes.expiry} /></dd></div></dl></div>
        <div className="token-actions"><Button variant="outline" onClick={() => { setAction({ kind: 'regenerate', token }); setError(null); }}>{t('regenerate')}</Button><Button variant="destructive" onClick={() => { setAction({ kind: 'revoke', token }); setError(null); }}>{t('revoke')}</Button></div>
      </article>)}</div>}
    <Dialog open={creating} onOpenChange={open => { if (!busy) { setCreating(open); setError(null); } }}><DialogContent className="portal-dialog" showCloseButton={!busy}><DialogHeader><DialogTitle>{t('createToken')}</DialogTitle><DialogDescription>{t('tokensDetail')}</DialogDescription></DialogHeader>
      <form onSubmit={create}><fieldset disabled={busy}><Field label={t('tokenName')} placeholder={t('tokenNameHint')} value={name} onChange={e => setName(e.target.value)} maxLength={255} autoFocus /><Field label={t('tokenExpiry')} hint={t('tokenExpiryHint')} type="datetime-local" value={expiry} onChange={e => setExpiry(e.target.value)} />
        <ErrorNotice error={error} /><DialogFooter><Button type="button" variant="outline" onClick={() => { setCreating(false); setError(null); }}>{t('cancel')}</Button><Button type="submit">{busy ? t('working') : t('createToken')}</Button></DialogFooter></fieldset></form>
    </DialogContent></Dialog>
    <Dialog open={!!action} onOpenChange={open => { if (!open && !busy) { setAction(null); setError(null); } }}><DialogContent className="portal-dialog" showCloseButton={!busy}><DialogHeader><DialogTitle>{t(action?.kind === 'regenerate' ? 'regenerateTitle' : 'revokeTitle')}</DialogTitle><DialogDescription>{t(action?.kind === 'regenerate' ? 'regenerateDetail' : 'revokeDetail')}</DialogDescription></DialogHeader>
      <p className="confirm-token-name">{action?.token.attributes.name || t('unnamed')}</p>{action?.kind === 'revoke' && action.token.id === session?.tokenId && <Notice>{t('currentWarning')}</Notice>}<ErrorNotice error={error} />
      <DialogFooter><Button variant="outline" disabled={busy} onClick={() => { setAction(null); setError(null); }}>{t('cancel')}</Button><Button variant={action?.kind === 'revoke' ? 'destructive' : 'default'} disabled={busy} onClick={confirmAction}>{busy ? t('working') : t(action?.kind === 'regenerate' ? 'regenerate' : 'revoke')}</Button></DialogFooter>
    </DialogContent></Dialog>
    <Dialog open={!!secret} onOpenChange={open => { if (!open) setSecret(null); }}><DialogContent className="portal-dialog"><DialogHeader><DialogTitle>{t('secretTitle')}</DialogTitle><DialogDescription>{t('secretDetail')}</DialogDescription></DialogHeader><div className="field"><label>{t('tokenSecret')}</label><code className="secret-value" tabIndex={0}>{secret}</code></div><DialogFooter><CopyButton value={secret ?? ''} label="copyToken" /><Button onClick={() => setSecret(null)}>{t('close')}</Button></DialogFooter></DialogContent></Dialog>
  </>;
}
