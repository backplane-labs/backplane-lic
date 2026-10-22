export interface PortalConfig { title: string; basePath: string; account: string }
declare global { interface Window { __PORTAL_CONFIG__?: PortalConfig } }

export const config: PortalConfig = window.__PORTAL_CONFIG__ ?? {
  title: 'Customer portal', basePath: '/portal', account: 'default',
};

export interface Attributes {
  name?: string | null; email?: string; fullName?: string | null; role?: string;
  token?: string; kind?: string; key?: string; status?: string;
  expiry?: string | null; created?: string; uses?: number; maxUses?: number | null;
  protected?: boolean; enabled?: boolean; secret?: string; uri?: string; code?: string;
}
export interface Resource {
  id: string; type: string; attributes: Attributes;
  relationships?: Record<string, { data?: { id: string; type: string } | null }>;
}
export interface Document<T = Resource> {
  data: T; meta?: { tokenId?: string }; links?: { next?: string | null };
}
export interface APIErrorItem { code?: string; title?: string; detail?: string; source?: { pointer?: string; parameter?: string } }
export class APIError extends Error {
  constructor(public status: number, public errors: APIErrorItem[]) {
    super(errors.map(e => [e.code, e.detail ?? e.title].filter(Boolean).join(': ')).join('\n') || `HTTP ${status}`);
  }
  has(code: string) { return this.errors.some(e => e.code === code); }
}
export type Session = { token: string; tokenId: string };
const storageKey = `lic.portal:${config.account}:${config.basePath}`;

export function readSession(): Session | null {
  try {
    const value: unknown = JSON.parse(sessionStorage.getItem(storageKey) ?? 'null');
    if (value && typeof value === 'object' && 'token' in value && 'tokenId' in value &&
      typeof value.token === 'string' && typeof value.tokenId === 'string') return value as Session;
  } catch { /* A blocked storage area must not stop the portal from rendering. */ }
  return null;
}
export function saveSession(value: Session | null) {
  // sessionStorage keeps credentials out of persistent localStorage and isolates
  // sessions to this tab. A closed tab must not leave a long-lived login behind.
  try {
    if (value) sessionStorage.setItem(storageKey, JSON.stringify(value));
    else sessionStorage.removeItem(storageKey);
  } catch { /* The session remains usable in memory if browser storage is blocked. */ }
}
export function basic(email: string, password: string) {
  return `Basic ${btoa(String.fromCharCode(...new TextEncoder().encode(`${email}:${password}`)))}`;
}
const apiPath = `/v1/accounts/${encodeURIComponent(config.account)}`;
export class API {
  constructor(private token: string | null = null) {}

  async request<T = Resource>(path: string, options: { method?: string; body?: unknown; authorization?: string; signal?: AbortSignal } = {}): Promise<Document<T>> {
    const response = await fetch(`${apiPath}${path}`, {
      method: options.method ?? 'GET', credentials: 'omit', cache: 'no-store', signal: options.signal,
      headers: {
        Accept: 'application/vnd.api+json',
        ...(options.body !== undefined ? { 'Content-Type': 'application/vnd.api+json' } : {}),
        ...(options.authorization || this.token ? { Authorization: options.authorization ?? `Bearer ${this.token}` } : {}),
      },
      ...(options.body !== undefined ? { body: JSON.stringify(options.body) } : {}),
    });
    if (!response.ok) {
      const error = await response.json().catch(() => null) as { errors?: APIErrorItem[] } | null;
      const failure = new APIError(response.status, error?.errors ?? [{ title: response.statusText || 'Request failed' }]);
      if (this.token && !options.signal?.aborted && (failure.has('TOKEN_INVALID') || failure.has('TOKEN_EXPIRED') || failure.has('TOKEN_MISSING') || failure.has('USER_BANNED'))) {
        window.dispatchEvent(new CustomEvent('lic-session-invalid', { detail: { token: this.token } }));
      }
      throw failure;
    }
    if (response.status === 204) return { data: null as T };
    return response.json() as Promise<Document<T>>;
  }

  async list(path: string, signal?: AbortSignal): Promise<Resource[]> {
    const resources: Resource[] = [];
    let next: string | null = `${path}${path.includes('?') ? '&' : '?'}page[size]=100`;
    const seen = new Set<string>();
    while (next) {
      if (seen.has(next)) throw new Error('The API returned a repeated pagination link.');
      seen.add(next);
      const response: Document<Resource[]> = await this.request<Resource[]>(next, { signal });
      resources.push(...response.data);
      const link: string | null | undefined = response.links?.next;
      if (!link) break;
      // Never forward the customer's token to a cross-origin pagination URL.
      const url = new URL(link, window.location.origin);
      if (url.origin !== window.location.origin || !url.pathname.startsWith(`${apiPath}/`)) {
        throw new Error('The API returned an unsafe pagination link.');
      }
      next = url.pathname.slice(apiPath.length) + url.search;
    }
    return resources;
  }
}

export function secretSession(resource: Resource): Session {
  if (!resource.attributes.token) throw new Error('The API response did not include the new token secret.');
  return { token: resource.attributes.token, tokenId: resource.id };
}

export function resetParameters(hash: string) {
  // Accept only fragments: reset secrets must never be sent in HTTP URLs or logs.
  const params = new URLSearchParams(hash.startsWith('#') ? hash.slice(1) : '');
  return {
    account: params.get('account') ?? '',
    userId: params.get('user') ?? params.get('userId') ?? params.get('id') ?? '',
    token: params.get('token') ?? params.get('passwordResetToken') ?? '',
  };
}
