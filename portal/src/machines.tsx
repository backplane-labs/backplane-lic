import { type FormEvent, useRef, useState } from 'react';
import type { JSONValue, Resource, UsageAck } from './api';
import { useAuth } from './auth';
import {
  Button,
  ErrorNotice,
  Field,
  fileName,
  LoadError,
  Loading,
  Notice,
  Status,
  saveFile,
  useLoad,
} from './components';
import { toFailure } from './errors';
import { useLocale } from './i18n';

// The accepted head of a machine's usage journal, which the vendor keeps in
// its metadata (vendorUsageAck) and every machine file carries.
function lastAck(machine: Resource): UsageAck | null {
  const ack = machine.attributes.metadata?.vendorUsageAck;
  if (!ack || typeof ack !== 'object' || Array.isArray(ack)) return null;
  const { epoch, seq, hash } = ack;
  return typeof epoch === 'number' && typeof seq === 'number' && typeof hash === 'string' ? { epoch, seq, hash } : null;
}

// readText reads a chosen file as text.
function readText(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(typeof reader.result === 'string' ? reader.result : '');
    reader.onerror = () => reject(reader.error ?? new Error('The file could not be read.'));
    reader.readAsText(file);
  });
}

// Machines are the installations a license runs on: activated by their
// fingerprint, each takes its machine file (the lease) here and hands in its
// usage reports — the offline path, without a connection of its own.
export function Machines({ license }: { license: Resource }) {
  const { t } = useLocale();
  const { api } = useAuth();
  const machines = useLoad(
    (signal) => api.list(`/licenses/${encodeURIComponent(license.id)}/machines`, signal),
    [api, license.id],
  );
  return (
    <section className="panel">
      <h2>{t('machines')}</h2>
      <p className="section-description">{t('machinesHelp')}</p>
      {machines.loading ? (
        <Loading />
      ) : machines.error ? (
        <LoadError error={machines.error} retry={machines.reload} />
      ) : (
        <>
          {machines.value?.length ? (
            <ul className="machine-list">
              {machines.value.map((machine) => (
                <Machine key={machine.id} machine={machine} />
              ))}
            </ul>
          ) : (
            <p className="hint">{t('noMachines')}</p>
          )}
          {!license.attributes.protected && <Activate license={license} activated={machines.reload} />}
        </>
      )}
    </section>
  );
}

function Machine({ machine }: { machine: Resource }) {
  const { t } = useLocale();
  const { api } = useAuth();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const [accepted, setAccepted] = useState<UsageAck | null>(null);
  const reportInput = useRef<HTMLInputElement>(null);
  const ack = accepted ?? lastAck(machine);
  const path = `/machines/${encodeURIComponent(machine.id)}/actions`;

  async function run(action: () => Promise<void>) {
    setError(null);
    setBusy(true);
    try {
      await action();
    } catch (cause) {
      setError(toFailure(cause));
    } finally {
      setBusy(false);
    }
  }

  return (
    <li className="machine">
      <div className="machine-top">
        <div>
          {machine.attributes.name && <strong>{machine.attributes.name}</strong>}
          <p className="hint">
            <code>{machine.attributes.fingerprint}</code>
          </p>
        </div>
        <Status value={machine.attributes.status} />
      </div>
      <p className="hint">{ack ? `${t('lastReport')} ${ack.seq}` : t('noReports')}</p>
      <ErrorNotice error={error} />
      {accepted && <Notice>{`${t('reportAccepted')} ${accepted.seq}`}</Notice>}
      <div className="machine-actions">
        <Button
          disabled={busy}
          onClick={() =>
            run(async () => {
              // POST answers the certificate in JSON:API: no bearer in a download URL.
              const response = await api.request(`${path}/check-out`, { method: 'POST', body: { meta: {} } });
              const certificate = response.data?.attributes.certificate;
              if (!certificate) throw new Error('The API returned no machine file.');
              saveFile(certificate, fileName(machine.id, 'lic'));
            })
          }
        >
          {t('downloadLease')}
        </Button>
        <Button variant="outline" disabled={busy} onClick={() => reportInput.current?.click()}>
          {t('uploadReport')}
        </Button>
        <input
          ref={reportInput}
          className="sr-only"
          aria-label={t('uploadReport')}
          type="file"
          accept=".json,application/json"
          disabled={busy}
          onChange={(event) => {
            const file = event.target.files?.[0];
            event.target.value = '';
            if (file)
              void run(async () => {
                const report: JSONValue = JSON.parse(await readText(file));
                const response = await api.request(`${path}/report-usage`, {
                  method: 'POST',
                  body: { meta: { report } },
                });
                if (!response.meta?.ack) throw new Error('The API returned no acknowledgement.');
                setAccepted(response.meta.ack);
              });
          }}
        />
      </div>
    </li>
  );
}

function Activate({ license, activated }: { license: Resource; activated: () => void }) {
  const { t } = useLocale();
  const { api } = useAuth();
  const [fingerprint, setFingerprint] = useState('');
  const [publicKey, setPublicKey] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  async function submit(event: FormEvent) {
    event.preventDefault();
    setError(null);
    setBusy(true);
    try {
      await api.request('/machines', {
        method: 'POST',
        body: {
          data: {
            type: 'machines',
            attributes: {
              fingerprint: fingerprint.trim(),
              ...(publicKey.trim() ? { metadata: { publicKey: publicKey.trim() } } : {}),
            },
            relationships: { license: { data: { type: 'licenses', id: license.id } } },
          },
        },
      });
      setFingerprint('');
      setPublicKey('');
      activated();
    } catch (cause) {
      setError(toFailure(cause));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className="activate" onSubmit={submit}>
      <h3>{t('activateMachine')}</h3>
      <ErrorNotice error={error} />
      <Field
        label={t('fingerprint')}
        hint={t('fingerprintHelp')}
        required
        value={fingerprint}
        onChange={(event) => setFingerprint(event.target.value)}
      />
      <Field
        label={t('publicKey')}
        hint={t('publicKeyHelp')}
        value={publicKey}
        onChange={(event) => setPublicKey(event.target.value)}
      />
      <Button type="submit" disabled={busy || !fingerprint.trim()}>
        {busy ? t('working') : t('activate')}
      </Button>
    </form>
  );
}
