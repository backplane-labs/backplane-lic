import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import App, { Providers } from './App';
import { Login, ForgotPassword, ResetPassword } from './auth-pages';
import { Profile } from './profile';
import { Tokens } from './tokens';
import { LicenseDetail, Licenses } from './licenses';
import { readSession, saveSession, type Resource } from './api';
import { useAuth } from './auth';

vi.mock('qrcode', () => ({ default: { toDataURL: vi.fn().mockResolvedValue('data:image/png;base64,local-qr') } }));
const fetchMock = vi.fn();
const user: Resource = { id: 'user-1', type: 'users', attributes: { role: 'user', email: 'customer@example.test' } };
const token = (id = 'session-1', secret?: string): Resource => ({ id, type: 'tokens', attributes: { name: 'My token', ...(secret ? { token: secret } : {}) } });
const json = (data: unknown, status = 200) => new Response(JSON.stringify({ data }), { status, headers: { 'Content-Type': 'application/vnd.api+json' } });
const failure = (code: string, status = 401) => new Response(JSON.stringify({ errors: [{ code, detail: code }] }), { status });
function Ready({ children }: { children: React.ReactNode }) {
  const { session, user } = useAuth();
  return session && !user ? <p>Checking session</p> : children;
}
function route(path: string, element: React.ReactNode) {
  return render(<Providers><MemoryRouter initialEntries={[path]}><Routes><Route path="/login" element={<Login />} /><Route path="/licenses" element={path === '/licenses' ? <Ready>{element}</Ready> : <div>License destination</div>} /><Route path="/test" element={<Ready>{element}</Ready>} /></Routes></MemoryRouter></Providers>);
}
function signedIn() { saveSession({ token: 'session-secret', tokenId: 'session-1' }); }
function endpoint(input: string) { return new URL(input, window.location.origin).pathname.replace('/v1/accounts/default', ''); }
beforeEach(() => {
  sessionStorage.clear(); localStorage.clear(); sessionStorage.setItem('lic.portal.locale', 'en');
  vi.stubGlobal('fetch', fetchMock); fetchMock.mockReset();
  vi.stubGlobal('matchMedia', vi.fn().mockReturnValue({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() }));
  window.history.replaceState({}, '', '/');
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

function LogoutProbe() {
  const { logout, session } = useAuth();
  return <button onClick={() => void logout()}>{session ? 'Revoke session' : 'Signed out'}</button>;
}

function InvalidationProbe() {
  const { api, session, replaceSession } = useAuth();
  return <><span>{session?.token}</span><button onClick={() => void api.request('/tokens').catch(() => {})}>Start old request</button><button onClick={() => replaceSession({ token: 'new-session-secret', tokenId: 'session-1' })}>Replace credential</button></>;
}

describe('customer authentication', () => {
  it('ignores a stale credential failure after regenerating the same token id', async () => {
    signedIn();
    let complete!: (response: Response) => void;
    const pending = new Promise<Response>(resolve => { complete = resolve; });
    fetchMock.mockImplementation(async (url: string) => endpoint(url) === '/profile' ? json(user) : pending);
    route('/test', <InvalidationProbe />);
    fireEvent.click(await screen.findByRole('button', { name: 'Start old request' }));
    fireEvent.click(screen.getByRole('button', { name: 'Replace credential' }));
    await screen.findByText('new-session-secret');
    const invalid = vi.fn(); window.addEventListener('lic-session-invalid', invalid);
    complete(failure('TOKEN_INVALID'));
    await waitFor(() => expect(invalid).toHaveBeenCalledOnce());
    expect(readSession()).toEqual({ token: 'new-session-secret', tokenId: 'session-1' });
    window.removeEventListener('lic-session-invalid', invalid);
  });
  it('switches the standalone portal to Russian and dark mode without persisting credentials', async () => {
    window.history.replaceState({}, '', '/portal/login');
    render(<App />);
    fireEvent.change(screen.getByLabelText('Language'), { target: { value: 'ru' } });
    await screen.findByRole('button', { name: 'Войти' });
    expect(document.documentElement.lang).toBe('ru');
    fireEvent.change(screen.getByLabelText('Оформление'), { target: { value: 'dark' } });
    expect(document.documentElement.dataset.theme).toBe('dark');
    expect(localStorage.length).toBe(0);
  });
  it('revokes the session token before clearing local credentials on logout', async () => {
    signedIn();
    fetchMock.mockImplementation(async (url: string, options: RequestInit) => {
      if (endpoint(url) === '/profile') return json(user);
      if (endpoint(url) === '/tokens/session-1' && options.method === 'DELETE') return new Response(null, { status: 204 });
      throw new Error(`Unexpected ${url}`);
    });
    route('/test', <LogoutProbe />);
    fireEvent.click(await screen.findByRole('button', { name: 'Revoke session' }));
    await screen.findByRole('button', { name: 'Signed out' });
    expect(readSession()).toBeNull();
    expect(fetchMock.mock.calls.some(call => endpoint(call[0]) === '/tokens/session-1' && call[1].method === 'DELETE')).toBe(true);
  });
  it('requests an OTP when required and logs in using Basic plus meta.otp', async () => {
    let attempts = 0;
    fetchMock.mockImplementation(async (url: string) => {
      if (endpoint(url) === '/tokens') return ++attempts === 1 ? failure('OTP_REQUIRED') : json(token('session-1', 'new-secret'), 201);
      if (endpoint(url) === '/profile') return json(user);
      throw new Error(`Unexpected ${url}`);
    });
    route('/login', <Login />);
    fireEvent.change(screen.getByLabelText('Email'), { target: { value: 'customer@example.test' } });
    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'correct-password' } });
    fireEvent.submit(screen.getByRole('button', { name: 'Sign in' }).closest('form')!);
    const otp = await screen.findByLabelText('Authentication code');
    fireEvent.change(otp, { target: { value: '123456' } });
    fireEvent.submit(screen.getByRole('button', { name: 'Sign in' }).closest('form')!);
    await screen.findByText('License destination');
    const calls = fetchMock.mock.calls.filter(call => endpoint(call[0]) === '/tokens');
    expect(calls[0][1].headers.Authorization).toMatch(/^Basic /);
    expect(JSON.parse(calls[1][1].body)).toEqual({ meta: { otp: '123456' } });
    expect(readSession()?.token).toBe('new-secret');
  });
  it('rejects operator accounts and revokes the just-created login token', async () => {
    fetchMock.mockImplementation(async (url: string, options: RequestInit) => {
      if (endpoint(url) === '/tokens') return json(token('operator-token', 'operator-secret'), 201);
      if (endpoint(url) === '/profile') return json({ ...user, attributes: { role: 'admin' } });
      if (endpoint(url) === '/tokens/operator-token' && options.method === 'DELETE') return new Response(null, { status: 204 });
      throw new Error(`Unexpected ${url}`);
    });
    route('/login', <Login />);
    fireEvent.change(screen.getByLabelText('Email'), { target: { value: 'admin@example.test' } });
    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'password' } });
    fireEvent.submit(screen.getByRole('button', { name: 'Sign in' }).closest('form')!);
    expect((await screen.findByRole('alert')).textContent).toContain('customer accounts only');
    expect(readSession()).toBeNull();
    expect(fetchMock.mock.calls.some(call => endpoint(call[0]) === '/tokens/operator-token' && call[1].method === 'DELETE')).toBe(true);
  });
  it('requests password reset anonymously without revealing account existence', async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 204 }));
    route('/test', <ForgotPassword />);
    fireEvent.change(screen.getByLabelText('Email'), { target: { value: 'unknown@example.test' } });
    fireEvent.submit(screen.getByRole('button', { name: 'Send reset link' }).closest('form')!);
    expect((await screen.findByRole('status')).textContent).toContain('If your account exists');
    expect(fetchMock.mock.calls[0][1].headers.Authorization).toBeUndefined();
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({ meta: { email: 'unknown@example.test' } });
  });
  it('consumes fragment credentials and immediately removes them from the URL', async () => {
    window.history.replaceState({}, '', '/portal/reset#account=default&user=user-1&token=reset-secret');
    fetchMock.mockResolvedValue(json(user));
    route('/test', <ResetPassword />);
    expect(window.location.search).toBe('');
    expect(window.location.hash).toBe('');
    fireEvent.change(screen.getByLabelText('New password'), { target: { value: 'new-password' } });
    fireEvent.change(screen.getByLabelText('Confirm new password'), { target: { value: 'new-password' } });
    fireEvent.submit(screen.getByRole('button', { name: 'Set new password' }).closest('form')!);
    await screen.findByText('Your password has been reset. Sign in with your new password.');
    expect(fetchMock.mock.calls[0][0]).toBe('/v1/accounts/default/users/user-1/actions/reset-password');
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({ meta: { passwordResetToken: 'reset-secret', newPassword: 'new-password' } });
  });
  it('rejects reset credentials supplied in the query string', () => {
    window.history.replaceState({}, '', '/portal/reset?account=default&user=user-1&token=reset-secret');
    route('/test', <ResetPassword />);
    expect(screen.getByRole('alert').textContent).toContain('incomplete');
    expect(fetchMock).not.toHaveBeenCalled();
  });
  it('rejects reset links with no user id instead of guessing an endpoint', () => {
    window.history.replaceState({}, '', '/portal/reset#account=default&token=reset-secret');
    route('/test', <ResetPassword />);
    expect(screen.getByRole('alert').textContent).toContain('incomplete');
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

describe('customer tokens', () => {
  it('creates through /tokens and shows the secret only until the dialog closes', async () => {
    signedIn();
    fetchMock.mockImplementation(async (url: string, options: RequestInit) => {
      if (endpoint(url) === '/profile') return json(user);
      if (endpoint(url) === '/tokens' && options.method === 'POST') return json(token('created-1', 'shown-once-secret'), 201);
      if (endpoint(url) === '/tokens') return json([token()]);
      throw new Error(`Unexpected ${url}`);
    });
    route('/test', <Tokens />);
    await screen.findByText('My token');
    fireEvent.click(screen.getByRole('button', { name: 'Create token' }));
    fireEvent.change(screen.getByLabelText('Token name'), { target: { value: 'Build server' } });
    fireEvent.submit(within(screen.getByRole('dialog')).getByRole('button', { name: 'Create token' }).closest('form')!);
    await screen.findByText('shown-once-secret');
    const call = fetchMock.mock.calls.find(call => call[1].method === 'POST')!;
    expect(call[0]).toBe('/v1/accounts/default/tokens');
    expect(JSON.parse(call[1].body)).toEqual({ data: { type: 'tokens', attributes: { name: 'Build server' } } });
    fireEvent.click(within(screen.getByRole('dialog')).getAllByRole('button', { name: 'Close' }).find(button => button.textContent === 'Close')!);
    await waitFor(() => expect(screen.queryByText('shown-once-secret')).toBeNull());
    expect(sessionStorage.getItem('lic.portal:default:/portal')).not.toContain('shown-once-secret');
  });
  it('replaces the session secret after regenerating the current token', async () => {
    signedIn();
    fetchMock.mockImplementation(async (url: string, options: RequestInit) => {
      if (endpoint(url) === '/profile') return json(user);
      if (endpoint(url) === '/tokens/session-1' && options.method === 'PUT') return json(token('session-1', 'replacement-secret'));
      if (endpoint(url) === '/tokens') return json([token()]);
      throw new Error(`Unexpected ${url}`);
    });
    window.history.replaceState({}, '', '/portal/tokens');
    render(<App />);
    fireEvent.click(await screen.findByRole('button', { name: 'Regenerate' }));
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Regenerate' }));
    await screen.findByText('replacement-secret');
    expect(readSession()).toEqual({ token: 'replacement-secret', tokenId: 'session-1' });
  });
});

describe('profile security', () => {
  it('changes passwords through the public update-password action', async () => {
    signedIn();
    fetchMock.mockImplementation(async (url: string) => {
      if (endpoint(url) === '/profile') return json(user);
      if (endpoint(url) === '/users/user-1/second-factors') return json([]);
      if (endpoint(url) === '/users/user-1/actions/update-password') return json(user);
      throw new Error(`Unexpected ${url}`);
    });
    route('/test', <Profile />);
    await screen.findByText('customer@example.test');
    fireEvent.change(screen.getByLabelText('Current password'), { target: { value: 'old-password' } });
    fireEvent.change(screen.getByLabelText('New password'), { target: { value: 'new-password' } });
    fireEvent.change(screen.getByLabelText('Confirm new password'), { target: { value: 'new-password' } });
    fireEvent.submit(screen.getByRole('button', { name: 'Update password' }).closest('form')!);
    await screen.findByText('Password updated. Other sessions have been signed out.');
    const call = fetchMock.mock.calls.find(call => endpoint(call[0]).endsWith('/actions/update-password'))!;
    expect(JSON.parse(call[1].body)).toEqual({ meta: { oldPassword: 'old-password', newPassword: 'new-password' } });
    expect(readSession()?.token).toBe('session-secret');
  });
  it('requires an OTP when disabling an enabled factor', async () => {
    signedIn(); let enabled = true;
    fetchMock.mockImplementation(async (url: string, options: RequestInit) => {
      if (endpoint(url) === '/profile') return json(user);
      if (endpoint(url) === '/users/user-1/second-factors') return json(enabled ? [{ id: 'factor-1', type: 'second-factors', attributes: { enabled: true } }] : []);
      if (endpoint(url) === '/users/user-1/second-factors/factor-1' && options.method === 'DELETE') { enabled = false; return new Response(null, { status: 204 }); }
      throw new Error(`Unexpected ${url}`);
    });
    route('/test', <Profile />);
    fireEvent.click(await screen.findByRole('button', { name: 'Disable two-factor authentication' }));
    fireEvent.change(screen.getByLabelText('Authentication code'), { target: { value: '123456' } });
    fireEvent.submit(screen.getByRole('button', { name: 'Disable two-factor authentication' }).closest('form')!);
    await screen.findByText('Two-factor authentication disabled.');
    const call = fetchMock.mock.calls.find(call => call[1].method === 'DELETE')!;
    expect(JSON.parse(call[1].body)).toEqual({ meta: { otp: '123456' } });
  });
  it('enrolls a local TOTP QR and enables only after OTP confirmation', async () => {
    signedIn();
    let factor: Resource | undefined;
    fetchMock.mockImplementation(async (url: string, options: RequestInit) => {
      if (endpoint(url) === '/profile') return json(user);
      if (endpoint(url) === '/users/user-1/second-factors') {
        if (options.method === 'POST') { factor = { id: 'factor-1', type: 'second-factors', attributes: { enabled: false, secret: 'BASE32SECRET', uri: 'otpauth://totp/Vendor?secret=BASE32SECRET' } }; return json(factor, 201); }
        return json(factor ? [factor] : []);
      }
      if (endpoint(url) === '/users/user-1/second-factors/factor-1' && options.method === 'PATCH') { factor = { ...factor!, attributes: { enabled: true } }; return json(factor); }
      throw new Error(`Unexpected ${url}`);
    });
    route('/test', <Profile />);
    fireEvent.click(await screen.findByRole('button', { name: 'Set up two-factor authentication' }));
    const verify = screen.getByText('Confirm your current password to begin.').closest('form')!;
    fireEvent.change(within(verify).getByLabelText('Current password'), { target: { value: 'correct-password' } });
    fireEvent.submit(verify);
    await screen.findByAltText('Authenticator setup QR code');
    expect(screen.getByText('BASE32SECRET')).toBeTruthy();
    fireEvent.change(screen.getByLabelText('Authentication code'), { target: { value: '123456' } });
    fireEvent.submit(screen.getByRole('button', { name: 'Confirm setup' }).closest('form')!);
    await screen.findByText('Two-factor authentication enabled.');
    const create = fetchMock.mock.calls.find(call => call[1].method === 'POST')!;
    const enable = fetchMock.mock.calls.find(call => call[1].method === 'PATCH')!;
    expect(JSON.parse(create[1].body)).toEqual({ meta: { password: 'correct-password' } });
    expect(JSON.parse(enable[1].body)).toEqual({ data: { type: 'second-factors', id: 'factor-1', attributes: { enabled: true } }, meta: { otp: '123456' } });
    expect(screen.queryByText('BASE32SECRET')).toBeNull();
  });
});

describe('licenses', () => {
  const license: Resource = { id: 'license-1', type: 'licenses', attributes: { key: 'ACME-KEY', status: 'ACTIVE', expiry: null, uses: 3, maxUses: 10, protected: false } };
  function mockLicenses(url: string) {
    const path = endpoint(url);
    if (path === '/profile') return json(user);
    if (path === '/users/user-1/licenses') return json([license]);
    if (path.endsWith('/product')) return json({ id: 'product-1', type: 'products', attributes: { name: 'Vendor Studio' } });
    if (path.endsWith('/policy')) return json({ id: 'policy-1', type: 'policies', attributes: { name: 'Professional' } });
    if (path.endsWith('/entitlements')) return json([{ id: 'entitlement-1', type: 'entitlements', attributes: { name: 'Export', code: 'EXPORT' } }]);
    if (path === '/users/user-1/licenses/license-1') return json(license);
    if (path.endsWith('/actions/check-out')) return json({ id: 'file-1', type: 'license-files', attributes: { certificate: '-----BEGIN LICENSE FILE-----\nSIGNED\n-----END LICENSE FILE-----' } });
    throw new Error(`Unexpected ${url}`);
  }
  it('shows only the user licenses with product, policy, usage and entitlements', async () => {
    signedIn(); fetchMock.mockImplementation(async (url: string) => mockLicenses(url));
    route('/licenses', <Licenses />);
    await screen.findByText('Vendor Studio');
    expect(screen.getByText('Professional')).toBeTruthy(); expect(screen.getByText('ACME-KEY')).toBeTruthy();
    expect(screen.getByText('Export')).toBeTruthy(); expect(screen.getByText('No expiry')).toBeTruthy();
    expect(fetchMock.mock.calls.some(call => endpoint(call[0]) === '/users/user-1/licenses')).toBe(true);
  });
  it('downloads the public checkout certificate as a .lic file without putting a bearer in the URL', async () => {
    signedIn(); fetchMock.mockImplementation(async (url: string) => mockLicenses(url));
    const createURL = vi.fn().mockReturnValue('blob:local-license'); const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});
    URL.createObjectURL = createURL; URL.revokeObjectURL = vi.fn();
    render(<Providers><MemoryRouter initialEntries={['/licenses/license-1']}><Routes><Route path="/licenses/:id" element={<LicenseDetail />} /></Routes></MemoryRouter></Providers>);
    fireEvent.click(await screen.findByRole('button', { name: 'Download license file' }));
    await waitFor(() => expect(createURL).toHaveBeenCalledOnce());
    const request = fetchMock.mock.calls.find(call => endpoint(call[0]).endsWith('/actions/check-out'))!;
    expect(request[1].method).toBe('POST'); expect(request[1].headers.Authorization).toBe('Bearer session-secret');
    expect(request[0]).not.toContain('token='); expect((click.mock.contexts[0] as unknown as HTMLAnchorElement).download).toBe('license-1.lic');
    click.mockRestore();
  });
});
