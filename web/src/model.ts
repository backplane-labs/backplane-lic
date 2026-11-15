// Parsed JSON with int64 kept as bigint; every decoded API value is one.
export type JSONValue = null | boolean | number | bigint | string | JSONValue[] | { [key: string]: JSONValue };
// What the serializers accept: parsed JSON plus native form values and sparse records.
export type WireValue =
  | undefined
  | null
  | boolean
  | number
  | bigint
  | string
  | Uint8Array
  | WireValue[]
  | { [key: string]: WireValue };
export type Resource = {
  type: string;
  id: string;
  attributes?: Record<string, JSONValue>;
  relationships?: Record<string, { data?: Resource | Resource[] | null; links?: Record<string, JSONValue> }>;
};
export type Document = {
  data?: Resource | Resource[] | null;
  meta?: Record<string, JSONValue>;
  links?: Record<string, string | null>;
  errors?: APIError[];
};
export type APIError = {
  title?: string;
  detail?: string;
  code?: string;
  source?: { pointer?: string; parameter?: string };
};
export class RequestError extends Error {
  constructor(
    public status: number,
    public errors: APIError[],
  ) {
    super(
      errors
        .map((e) => [e.source?.pointer ?? e.source?.parameter, e.detail ?? e.title, e.code].filter(Boolean).join(': '))
        .join('\n') || `HTTP ${status}`,
    );
  }
}
// The platform's query state types its error as a boundary value; every read
// here rejects with an Error, and anything else is wrapped without changing its text.
export function toError<T>(value: T): Error {
  return value instanceof Error ? value : new Error(String(value));
}
export const accountPath = (account: string, path = '') =>
  `/v1/accounts/${encodeURIComponent(account)}${path ? `/${path}` : ''}`;
// parseDocument validates the JSON:API shape at the boundary so the rest of
// the app can trust the Document type; a malformed payload is a request error.
function isRecord(value: JSONValue): value is { [key: string]: JSONValue } {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
function isResource(value: JSONValue): value is Resource {
  return isRecord(value) && typeof value.type === 'string' && typeof value.id === 'string';
}
// Unknown extra members are tolerated; the known ones must match the APIError shape.
function isAPIError(value: JSONValue): value is APIError {
  if (!isRecord(value)) return false;
  if (['title', 'detail', 'code'].some((key) => value[key] !== undefined && typeof value[key] !== 'string'))
    return false;
  const source = value.source;
  if (source !== undefined) {
    if (!isRecord(source)) return false;
    if (
      (source.pointer !== undefined && typeof source.pointer !== 'string') ||
      (source.parameter !== undefined && typeof source.parameter !== 'string')
    )
      return false;
  }
  return true;
}
function parseDocument(text: string): Document {
  const value = parseJSON(text);
  if (!isRecord(value)) throw new Error('Expected a JSON:API document object');
  const document: Document = {};
  if (value.data !== undefined && value.data !== null) {
    if (Array.isArray(value.data)) {
      if (!value.data.every(isResource)) throw new Error('Expected resources in data');
      document.data = value.data;
    } else if (isResource(value.data)) document.data = value.data;
    else throw new Error('Expected a resource in data');
  }
  if (value.meta !== undefined) {
    if (!isRecord(value.meta)) throw new Error('Expected an object in meta');
    document.meta = value.meta;
  }
  if (value.links !== undefined) {
    if (!isRecord(value.links)) throw new Error('Expected an object in links');
    const links: Record<string, string | null> = {};
    for (const [name, link] of Object.entries(value.links)) {
      if (link !== null && typeof link !== 'string') throw new Error('Expected string links');
      links[name] = link;
    }
    document.links = links;
  }
  if (value.errors !== undefined) {
    if (!Array.isArray(value.errors) || !value.errors.every(isAPIError)) throw new Error('Expected errors array');
    document.errors = value.errors;
  }
  return document;
}
export function decodeDocument(status: number, bytes: Uint8Array): Document {
  const text = new TextDecoder().decode(bytes);
  let document: Document = {};
  if (text) {
    try {
      document = parseDocument(text);
    } catch {
      throw new RequestError(status, [{ detail: text }]);
    }
  }
  if (status < 200 || status >= 300) throw new RequestError(status, document.errors ?? []);
  return document;
}
export function listData(document?: Document): Resource[] {
  return Array.isArray(document?.data) ? document.data : document?.data ? [document.data] : [];
}
export function labelOf(resource: Resource) {
  return String(
    resource.attributes?.name ??
      resource.attributes?.email ??
      resource.attributes?.key ??
      resource.attributes?.code ??
      resource.attributes?.event ??
      resource.attributes?.url ??
      resource.id,
  );
}
export function nextPage(links: Document['links']): Record<string, string> | undefined {
  if (!links?.next) return undefined;
  const next = new URL(links.next, 'https://console.invalid');
  return Object.fromEntries(next.searchParams);
}
export function writableAttributes(
  resource: Resource,
  fields: readonly { name: string; createOnly?: boolean }[],
  updating = false,
) {
  const attributes = resource.attributes ?? {};
  return Object.fromEntries(
    fields
      .filter((f) => !(updating && f.createOnly) && f.name !== 'password')
      .filter((f) => Object.hasOwn(attributes, f.name))
      .map((f) => [f.name, attributes[f.name]]),
  );
}
// Mask credentials everywhere except the explicit one-time result panel.
export function redact(value: WireValue): WireValue {
  if (Array.isArray(value)) return value.map(redact);
  if (!value || typeof value !== 'object') return value;
  return Object.fromEntries(
    Object.entries(value).map(([key, child]) => [
      key,
      /^(token|secret|password|passwordResetToken|recoveryCodes|otpAuthUrl|uri)$/i.test(key) && child
        ? '••••••••'
        : redact(child),
    ]),
  );
}
// Encode integer form values without rounding int64 or converting them to strings.
export function wireJSON(value: WireValue): string {
  if (typeof value === 'bigint') return value.toString();
  if (Array.isArray(value)) return `[${value.map((item) => wireJSON(item ?? null)).join(',')}]`;
  if (value && typeof value === 'object')
    return `{${Object.entries(value)
      .filter(([, v]) => v !== undefined)
      .map(([k, v]) => `${JSON.stringify(k)}:${wireJSON(v)}`)
      .join(',')}}`;
  return JSON.stringify(value) ?? 'null';
}
export function parseJSON(text: string): JSONValue {
  // JSON.parse's source context preserves integer lexemes in modern browsers.
  return JSON.parse(text, (_key, value, context?: { source?: string }) => {
    const source = context?.source;
    if (typeof value === 'number' && !Number.isSafeInteger(value) && source !== undefined && /^-?\d+$/.test(source))
      return BigInt(source);
    return value;
  });
}
export function json(value: WireValue): string {
  const pretty = (v: WireValue, depth: number): string => {
    const indent = '  '.repeat(depth),
      childIndent = `${indent}  `;
    if (Array.isArray(v))
      return v.length ? `[\n${v.map((item) => childIndent + pretty(item, depth + 1)).join(',\n')}\n${indent}]` : '[]';
    if (v && typeof v === 'object') {
      const entries = Object.entries(v).filter(([, child]) => child !== undefined);
      return entries.length
        ? `{\n${entries
            .map(([key, child]) => `${childIndent}${JSON.stringify(key)}: ${pretty(child, depth + 1)}`)
            .join(',\n')}\n${indent}}`
        : '{}';
    }
    return wireJSON(v);
  };
  return pretty(value, 0);
}
export function secretsOf(document: Document): Record<string, JSONValue> {
  const secrets: Record<string, JSONValue> = {};
  for (const resource of listData(document))
    for (const [key, value] of Object.entries(resource.attributes ?? {})) {
      if (/^(token|secret|recoveryCodes|otpAuthUrl|uri)$/i.test(key) && value) secrets[key] = value;
    }
  // Token generation/regeneration returns the secret as data.attributes.token.
  return secrets;
}

// humanize is the fallback label of an untranslated attribute name:
// distributionStrategy reads as "Distribution strategy".
export function humanize(key: string): string {
  const words = key
    .replace(/[-_]/g, ' ')
    .replace(/([a-z0-9])([A-Z])/g, '$1 $2')
    .toLowerCase()
    .trim();
  return words.charAt(0).toUpperCase() + words.slice(1);
}
