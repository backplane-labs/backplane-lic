export type Resource = { type: string; id: string; attributes?: Record<string, unknown>; relationships?: Record<string, { data?: Resource | Resource[] | null; links?: Record<string, unknown> }> };
export type Document = { data?: Resource | Resource[] | null; meta?: Record<string, unknown>; links?: Record<string, string | null>; errors?: APIError[] };
export type APIError = { title?: string; detail?: string; code?: string; source?: { pointer?: string; parameter?: string } };
export class RequestError extends Error {
  constructor(public status: number, public errors: APIError[]) { super(errors.map((e) => [e.source?.pointer ?? e.source?.parameter, e.detail ?? e.title, e.code].filter(Boolean).join(': ')).join('\n') || `HTTP ${status}`); }
}
export const accountPath = (account: string, path = '') => `/v1/accounts/${encodeURIComponent(account)}${path ? '/' + path : ''}`;
export function decodeDocument(status: number, bytes: Uint8Array): Document {
  const text = new TextDecoder().decode(bytes);
  let document: Document = {};
  if (text) {
    try { document = parseJSON(text) as Document; }
    catch { throw new RequestError(status, [{ detail: text }]); }
  }
  if (status < 200 || status >= 300) throw new RequestError(status, document.errors ?? []);
  return document;
}
export function listData(document?: Document): Resource[] { return Array.isArray(document?.data) ? document.data : document?.data ? [document.data] : []; }
export function labelOf(resource: Resource) { return String(resource.attributes?.name ?? resource.attributes?.email ?? resource.attributes?.key ?? resource.attributes?.code ?? resource.attributes?.event ?? resource.attributes?.url ?? resource.id); }
export function nextPage(links: Document['links']): Record<string, string> | undefined {
  if (!links?.next) return undefined;
  const next = new URL(links.next, 'https://console.invalid');
  return Object.fromEntries(next.searchParams);
}
export function writableAttributes(resource: Resource, fields: readonly { name: string; createOnly?: boolean }[], updating = false) {
  return Object.fromEntries(fields.filter((f) => !(updating && f.createOnly) && f.name !== 'password').filter((f) => Object.hasOwn(resource.attributes ?? {}, f.name)).map((f) => [f.name, resource.attributes![f.name]]));
}
// Mask credentials everywhere except the explicit one-time result panel.
export function redact(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(redact);
  if (!value || typeof value !== 'object') return value;
  return Object.fromEntries(Object.entries(value).map(([key, child]) => [key, /^(token|secret|password|passwordResetToken|recoveryCodes|otpAuthUrl|uri)$/i.test(key) && child ? '••••••••' : redact(child)]));
}
// Encode integer form values without rounding int64 or converting them to strings.
export function wireJSON(value: unknown): string {
  if (typeof value === 'bigint') return value.toString();
  if (Array.isArray(value)) return '[' + value.map((item) => wireJSON(item ?? null)).join(',') + ']';
  if (value && typeof value === 'object') return '{' + Object.entries(value).filter(([, v]) => v !== undefined).map(([k, v]) => JSON.stringify(k) + ':' + wireJSON(v)).join(',') + '}';
  return JSON.stringify(value) ?? 'null';
}
export function parseJSON(text: string): unknown {
  // JSON.parse's source context preserves integer lexemes in modern browsers.
  return JSON.parse(text, (_key, value, context?: { source?: string }) => typeof value === 'number' && !Number.isSafeInteger(value) && /^-?\d+$/.test(context?.source ?? '') ? BigInt(context!.source!) : value);
}
export function json(value: unknown): string {
  const pretty = (v: unknown, depth: number): string => {
    const indent = '  '.repeat(depth), childIndent = indent + '  ';
    if (Array.isArray(v)) return v.length ? '[\n' + v.map((item) => childIndent + pretty(item, depth + 1)).join(',\n') + '\n' + indent + ']' : '[]';
    if (v && typeof v === 'object') {
      const entries = Object.entries(v).filter(([, child]) => child !== undefined);
      return entries.length ? '{\n' + entries.map(([key, child]) => childIndent + JSON.stringify(key) + ': ' + pretty(child, depth + 1)).join(',\n') + '\n' + indent + '}' : '{}';
    }
    return wireJSON(v);
  };
  return pretty(value, 0);
}
export function secretsOf(document: Document): Record<string, unknown> {
  const secrets: Record<string, unknown> = {};
  for (const resource of listData(document)) for (const [key, value] of Object.entries(resource.attributes ?? {})) {
    if (/^(token|secret|recoveryCodes|otpAuthUrl|uri)$/i.test(key) && value) secrets[key] = value;
  }
  // Token generation/regeneration returns the secret as data.attributes.token.
  return secrets;
}

// humanize is the fallback label of an untranslated attribute name:
// distributionStrategy reads as "Distribution strategy".
export function humanize(key: string): string {
  const words = key.replace(/[-_]/g, ' ').replace(/([a-z0-9])([A-Z])/g, '$1 $2').toLowerCase().trim();
  return words.charAt(0).toUpperCase() + words.slice(1);
}
