import type { ReactNode } from 'react';
import { ClipboardButton, KeyValueList, Meter, StatRow, StatTile, StatusBadge, StatusText, Text, Timestamp, type KeyValueItem, type StatusTone } from '@backplane-labs/backplane-ui';
import type { Resource } from './model';
import { useText } from './components';

const tones: Record<string, StatusTone> = {
  ACTIVE: 'success', DELIVERED: 'success', VALID: 'success', enabled: 'success',
  EXPIRING: 'warning', DELIVERING: 'info', FAILING: 'warning', INACTIVE: 'neutral',
  EXPIRED: 'danger', SUSPENDED: 'danger', BANNED: 'danger', FAILED: 'danger', BANNED_USER: 'danger',
};

/** statusTone maps the API's status vocabulary onto the kit's tones. */
export function statusTone(value: unknown): StatusTone {
  return tones[String(value)] ?? 'neutral';
}

export function Status({ value }: { value: unknown }) {
  if (value === null || value === undefined || value === '') return <Text muted>—</Text>;
  return <StatusBadge tone={statusTone(value)} dot>{String(value)}</StatusBadge>;
}

const isoDate = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}/;

/** Value renders one attribute the way a person reads it. */
export function Value({ name, value }: { name: string; value: unknown }): ReactNode {
  const t = useText();
  if (value === null || value === undefined || value === '') return <Text muted>—</Text>;
  if (name === 'status') return <Status value={value} />;
  if (typeof value === 'boolean') return <StatusText tone={value ? 'success' : 'neutral'}>{t(value ? 'yes' : 'no')}</StatusText>;
  if (typeof value === 'string' && isoDate.test(value)) return <Timestamp value={new Date(value)} absolute />;
  if (Array.isArray(value)) return value.length ? value.map(String).join(', ') : <Text muted>—</Text>;
  if (typeof value === 'object') {
    const entries = Object.entries(value as Record<string, unknown>);
    if (!entries.length) return <Text muted>—</Text>;
    return <KeyValueList items={entries.map(([key, item]) => ({ key, label: key, value: <Value name={key} value={item} /> }))} />;
  }
  return String(value);
}

const copyable = new Set(['key', 'id', 'token', 'url', 'email', 'fingerprint']);

/** Attributes lists a record's attributes; long identifiers get a copy button. */
export function Attributes({ resource, omit = [] }: { resource: Resource; omit?: string[] }) {
  const t = useText();
  const items: KeyValueItem[] = [{ key: 'id', label: 'ID', value: <Copyable value={resource.id} />, mono: true }];
  for (const [name, value] of Object.entries(resource.attributes ?? {})) {
    if (omit.includes(name)) continue;
    const text = typeof value === 'string' ? value : '';
    items.push({ key: name, label: t(`${name}Column`, { defaultValue: t(name) }), mono: copyable.has(name), value: copyable.has(name) && text ? <Copyable value={text} /> : <Value name={name} value={value} /> });
  }
  return <KeyValueList items={items} />;
}

function Copyable({ value }: { value: string }) {
  return <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6, minWidth: 0 }}><span style={{ overflowWrap: 'anywhere' }}>{value}</span><ClipboardButton value={value} /></span>;
}

/** LicenseSummary is the at-a-glance strip of a license: state, usage and term. */
export function LicenseSummary({ resource }: { resource: Resource }) {
  const t = useText(), a = resource.attributes ?? {};
  const uses = Number(a.uses ?? 0), max = typeof a.maxUses === 'number' ? a.maxUses : undefined;
  return <StatRow>
    <StatTile label={t('status')} value={<Status value={a.status} />} />
    <StatTile label={t('uses')} value={max ? `${uses} / ${max}` : String(uses)} hint={max ? <Meter value={uses} max={max} label={t('uses')} tone={uses >= max ? 'danger' : uses / max > 0.8 ? 'warning' : 'success'} /> : t('unlimited')} />
    <StatTile label={t('expiryColumn')} value={typeof a.expiry === 'string' ? <Timestamp value={new Date(a.expiry)} /> : t('never')} hint={typeof a.expiry === 'string' ? <Timestamp value={new Date(a.expiry)} absolute /> : undefined} />
    <StatTile label={t('maxMachines')} value={a.maxMachines === null || a.maxMachines === undefined ? t('unlimited') : String(a.maxMachines)} />
  </StatRow>;
}
