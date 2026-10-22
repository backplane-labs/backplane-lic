import { CustomerError } from './errors';
import { createContext, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { API, APIError, readSession, saveSession, type Resource, type Session } from './api';

interface Auth {
  session: Session | null; user: Resource | null; api: API; checking: boolean; error: unknown;
  login: (session: Session) => Promise<void>; logout: () => Promise<void>;
  replaceSession: (session: Session | null) => void;
}
const AuthContext = createContext<Auth | null>(null);
export function AuthProvider({ children }: { children: ReactNode }) {
  const [session, setSession] = useState<Session | null>(readSession);
  const activeSession = useRef(session);
  const [user, setUser] = useState<Resource | null>(null);
  const [checking, setChecking] = useState(!!session);
  const [error, setError] = useState<unknown>(null);
  const api = useMemo(() => new API(session?.token), [session?.token]);
  function replaceSession(value: Session | null) {
    activeSession.current = value;
    saveSession(value); setSession(value);
    if (!value) setUser(null);
  }
  useEffect(() => {
    function invalidate(event: Event) {
      // A late failure from a revoked or regenerated credential must not clear a newer session.
      if (!(event instanceof CustomEvent) || event.detail?.token !== activeSession.current?.token) return;
      replaceSession(null); setError(new CustomerError('sessionExpired'));
    }
    window.addEventListener('lic-session-invalid', invalidate);
    return () => window.removeEventListener('lic-session-invalid', invalidate);
  }, []);
  useEffect(() => {
    if (!session) { setChecking(false); return; }
    const controller = new AbortController();
    // Revalidating a regenerated session must not unmount its show-once secret dialog.
    if (!user) setChecking(true);
    api.request('/profile', { signal: controller.signal }).then(response => {
      if (controller.signal.aborted) return;
      if (response.data.type !== 'users' || response.data.attributes.role !== 'user') {
        replaceSession(null); setError(new CustomerError('customerOnly')); return;
      }
      setUser(response.data);
    }).catch(cause => {
      if (controller.signal.aborted) return;
      if (cause instanceof APIError && (cause.status === 401 || cause.has('USER_BANNED'))) {
        replaceSession(null); setError(new CustomerError('sessionExpired'));
      } else setError(cause);
    }).finally(() => { if (!controller.signal.aborted) setChecking(false); });
    return () => controller.abort();
  }, [api]);
  async function login(value: Session) {
    const client = new API(value.token);
    try {
      const response = await client.request('/profile');
      if (response.data.type !== 'users' || response.data.attributes.role !== 'user') {
        await client.request(`/tokens/${encodeURIComponent(value.tokenId)}`, { method: 'DELETE' });
        throw new CustomerError('customerOnly');
      }
      setError(null); setUser(response.data); replaceSession(value);
    } catch (cause) {
      // A generated login token must not be persisted before its bearer is verified.
      throw cause;
    }
  }
  async function logout() {
    if (session) await api.request(`/tokens/${encodeURIComponent(session.tokenId)}`, { method: 'DELETE' });
    setError(null); replaceSession(null);
  }
  return <AuthContext.Provider value={{ session, user, api, checking, error, login, logout, replaceSession }}>{children}</AuthContext.Provider>;
}
export function useAuth() {
  const auth = useContext(AuthContext);
  if (!auth) throw new Error('AuthProvider is missing');
  return auth;
}
