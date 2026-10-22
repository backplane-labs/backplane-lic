import { useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { type API, type Resource } from './api';
import { useAuth } from './auth';
import { Button, CopyButton, DateValue, Empty, ErrorNotice, LoadError, Loading, Status, useLoad } from './components';
import { useLocale } from './i18n';

interface LicenseView { license: Resource; product: Resource; policy: Resource; entitlements: Resource[] }
async function enrich(api: API, license: Resource, signal: AbortSignal): Promise<LicenseView> {
  const path = `/licenses/${encodeURIComponent(license.id)}`;
  const [product, policy, entitlements] = await Promise.all([
    api.request(`${path}/product`, { signal }), api.request(`${path}/policy`, { signal }), api.list(`${path}/entitlements`, signal),
  ]);
  return { license, product: product.data, policy: policy.data, entitlements };
}
function Usage({ license }: { license: Resource }) {
  const { t } = useLocale();
  return <>{license.attributes.uses ?? 0} <span className="muted">/ {license.attributes.maxUses ?? t('unlimited')}</span></>;
}
function Entitlements({ values }: { values: Resource[] }) {
  const { t } = useLocale();
  return values.length ? <ul className="entitlements">{values.map(value => <li key={value.id}>{value.attributes.name ?? value.attributes.code ?? value.id}{value.attributes.name && value.attributes.code && <span className="entitlement-code">{value.attributes.code}</span>}</li>)}</ul> : <p className="hint">{t('noEntitlements')}</p>;
}
export function Licenses() {
  const { t } = useLocale();
  const { api, user } = useAuth();
  const result = useLoad(async signal => {
    const licenses = await api.list(`/users/${encodeURIComponent(user!.id)}/licenses`, signal);
    return Promise.all(licenses.map(license => enrich(api, license, signal)));
  }, [api, user?.id]);
  return <><header className="page-header"><h1>{t('licenses')}</h1><p>{t('licensesDetail')}</p></header>
    {result.loading ? <Loading /> : result.error ? <LoadError error={result.error} retry={result.reload} /> : !result.value?.length ? <Empty title={t('noLicenses')} description={t('noLicensesDetail')} /> :
      <div className="license-list">{result.value.map(({ license, product, policy, entitlements }) => <article className="license-card" key={license.id}>
        <div className="license-top"><div><h2><Link to={`/licenses/${encodeURIComponent(license.id)}`}>{product.attributes.name ?? t('unknown')}</Link></h2><p className="plan-name">{policy.attributes.name ?? t('unknown')}{license.attributes.name && <> / {license.attributes.name}</>}</p></div><Status value={license.attributes.status} /></div>
        <div className="license-key-row"><code className="license-key">{license.attributes.key ?? t('unknown')}</code>{license.attributes.key && <CopyButton value={license.attributes.key} />}</div>
        <dl className="license-metrics"><div><dt>{t('expiry')}</dt><dd><DateValue value={license.attributes.expiry} /></dd></div><div><dt>{t('uses')}</dt><dd><Usage license={license} /></dd></div></dl>
        <div className="license-footer"><Entitlements values={entitlements} /><Link className="text-link" to={`/licenses/${encodeURIComponent(license.id)}`}>{t('details')}</Link></div>
      </article>)}</div>}
  </>;
}
export function LicenseDetail() {
  const { t } = useLocale();
  const { api, user } = useAuth();
  const { id } = useParams();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const result = useLoad(async signal => {
    const response = await api.request(`/users/${encodeURIComponent(user!.id)}/licenses/${encodeURIComponent(id!)}`, { signal });
    return enrich(api, response.data, signal);
  }, [api, user?.id, id]);
  if (result.loading) return <Loading />;
  if (result.error) return <LoadError error={result.error} retry={result.reload} />;
  if (!result.value) return null;
  const { license, product, policy, entitlements } = result.value;
  return <><Link className="back-link" to="/licenses">‹ {t('backLicenses')}</Link><header className="page-header page-header-inline"><div><h1>{product.attributes.name ?? t('unknown')}</h1><p>{policy.attributes.name ?? t('unknown')}{license.attributes.name && <> / {license.attributes.name}</>}</p></div><Status value={license.attributes.status} /></header>
    <section className="panel"><h2>{t('key')}</h2><div className="license-key-row"><code className="license-key">{license.attributes.key ?? t('unknown')}</code>{license.attributes.key && <CopyButton value={license.attributes.key} />}</div>
      <dl className="detail-grid"><div><dt>{t('product')}</dt><dd>{product.attributes.name ?? t('unknown')}</dd></div><div><dt>{t('policy')}</dt><dd>{policy.attributes.name ?? t('unknown')}</dd></div><div><dt>{t('expiry')}</dt><dd><DateValue value={license.attributes.expiry} /></dd></div><div><dt>{t('uses')}</dt><dd><Usage license={license} /></dd></div></dl>
    </section>
    <section className="panel"><h2>{t('entitlements')}</h2><Entitlements values={entitlements} /></section>
    <section className="panel"><h2>{t('download')}</h2><p className="section-description">{license.attributes.protected ? t('protectedLicense') : t('downloadHelp')}</p><ErrorNotice error={error} />
      <Button disabled={busy || license.attributes.protected} onClick={async () => {
        setError(null); setBusy(true);
        try {
          // POST returns the certificate in JSON:API, so no token is exposed in a download URL.
          const response = await api.request<Resource & { attributes: { certificate: string } }>(`/licenses/${encodeURIComponent(license.id)}/actions/check-out`, { method: 'POST', body: { meta: {} } });
          const certificate = response.data.attributes.certificate;
          if (typeof certificate !== 'string' || !certificate) throw new Error('The API returned no license certificate.');
          const url = URL.createObjectURL(new Blob([certificate], { type: 'application/octet-stream' }));
          const anchor = document.createElement('a'); anchor.href = url; anchor.download = `${license.id.replace(/[^a-zA-Z0-9-]/g, '_')}.lic`;
          document.body.append(anchor); anchor.click(); anchor.remove();
          setTimeout(() => URL.revokeObjectURL(url), 1000);
        } catch (cause) { setError(cause); } finally { setBusy(false); }
      }}>{busy ? t('working') : t('download')}</Button>
    </section>
  </>;
}
