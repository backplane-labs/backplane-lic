import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { API, APIError, basic, readSession, resetParameters, saveSession, secretSession } from './api';

const fetchMock = vi.fn();
beforeEach(() => { vi.stubGlobal('fetch', fetchMock); fetchMock.mockReset(); sessionStorage.clear(); });
afterEach(() => vi.unstubAllGlobals());
function json(body: unknown, status = 200) { return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/vnd.api+json' } }); }

describe('public API client', () => {
  it('uses same-origin public API URLs, bearer auth, no cookies, and no cache', async () => {
    fetchMock.mockResolvedValue(json({ data: [] }));
    await new API('customer-token').request('/tokens', { method: 'POST', body: { data: { type: 'tokens', attributes: { name: 'CI' } } } });
    const [url, options] = fetchMock.mock.calls[0];
    expect(url).toBe('/v1/accounts/default/tokens');
    expect(options).toMatchObject({ method: 'POST', credentials: 'omit', cache: 'no-store', headers: { Authorization: 'Bearer customer-token', 'Content-Type': 'application/vnd.api+json' } });
    expect(JSON.parse(options.body)).toEqual({ data: { type: 'tokens', attributes: { name: 'CI' } } });
  });
  it('encodes UTF-8 basic credentials rather than throwing on non-ASCII passwords', () => {
    const bytes = Uint8Array.from(atob(basic('user@example.test', 'пароль').slice(6)), char => char.charCodeAt(0));
    expect(new TextDecoder().decode(bytes)).toBe('user@example.test:пароль');
  });
  it('handles 204 without trying to parse an empty response', async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 204 }));
    expect(await new API().request('/passwords', { method: 'POST', body: { meta: { email: 'user@example.test' } } })).toEqual({ data: null });
  });
  it('preserves public API error codes and field sources', async () => {
    fetchMock.mockResolvedValue(json({ errors: [{ code: 'OTP_REQUIRED', detail: 'second factor is required', source: { pointer: '/meta/otp' } }] }, 401));
    const error = await new API().request('/tokens').catch(cause => cause as APIError);
    expect(error).toBeInstanceOf(APIError);
    if (!(error instanceof APIError)) throw new Error('Expected APIError');
    expect(error.has('OTP_REQUIRED')).toBe(true);
    expect(error.message).toContain('second factor is required');
    expect(error.errors[0].source?.pointer).toBe('/meta/otp');
  });
  it('loads all pages through safe public API pagination links', async () => {
    fetchMock.mockResolvedValueOnce(json({ data: [{ id: 'one' }], links: { next: '/v1/accounts/default/tokens?page[cursor]=one' } })).mockResolvedValueOnce(json({ data: [{ id: 'two' }], links: { next: null } }));
    expect(await new API('secret').list('/tokens')).toEqual([{ id: 'one' }, { id: 'two' }]);
    expect(fetchMock.mock.calls[1][0]).toBe('/v1/accounts/default/tokens?page[cursor]=one');
  });
  it.each(['https://other.example/v1/accounts/default/tokens', '/private/tokens', '/v1/accounts/other/tokens'])('rejects unsafe pagination %s before sending a token', async next => {
    fetchMock.mockResolvedValue(json({ data: [], links: { next } }));
    await expect(new API('secret').list('/tokens')).rejects.toThrow('unsafe pagination');
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
  it('rejects repeating pagination rather than looping forever', async () => {
    fetchMock.mockResolvedValue(json({ data: [], links: { next: '/v1/accounts/default/tokens?page[size]=100' } }));
    await expect(new API('secret').list('/tokens')).rejects.toThrow('repeated pagination');
  });
  it('signals an expired session, but not an OTP validation error', async () => {
    const invalid = vi.fn(); window.addEventListener('lic-session-invalid', invalid);
    fetchMock.mockResolvedValueOnce(json({ errors: [{ code: 'OTP_INVALID' }] }, 401)).mockResolvedValueOnce(json({ errors: [{ code: 'TOKEN_EXPIRED' }] }, 401));
    await new API('secret').request('/users/me/second-factors', { method: 'PATCH' }).catch(() => {});
    expect(invalid).not.toHaveBeenCalled();
    await new API('secret').request('/tokens').catch(() => {});
    expect(invalid).toHaveBeenCalledOnce(); window.removeEventListener('lic-session-invalid', invalid);
  });
});
describe('credentials and reset links', () => {
  it('keeps the token only in sessionStorage and removes it on logout', () => {
    const local = vi.spyOn(Storage.prototype, 'setItem');
    saveSession({ token: 'secret', tokenId: 'id' });
    expect(readSession()).toEqual({ token: 'secret', tokenId: 'id' });
    expect(local.mock.instances[0]).toBe(sessionStorage);
    expect(localStorage.length).toBe(0);
    saveSession(null); expect(readSession()).toBeNull(); local.mockRestore();
  });
  it('fails closed for malformed stored credentials', () => {
    sessionStorage.setItem('lic.portal:default:/portal', '{'); expect(readSession()).toBeNull();
    sessionStorage.setItem('lic.portal:default:/portal', JSON.stringify({ token: 42 })); expect(readSession()).toBeNull();
  });
  it('accepts vendor reset links with a user id and reset token', () => {
    expect(resetParameters('?user=user-1&token=reset-secret')).toEqual({ account: '', userId: 'user-1', token: 'reset-secret' });
    expect(resetParameters('?userId=user-1&passwordResetToken=reset-secret')).toEqual({ account: '', userId: 'user-1', token: 'reset-secret' });
    expect(resetParameters('?token=only-token')).toEqual({ account: '', userId: '', token: 'only-token' });
  });
  it('reads delivery fragment parameters ahead of legacy query parameters', () => {
    expect(resetParameters('?user=wrong&token=wrong', '#account=default&user=user-1&token=reset-secret')).toEqual({ account: 'default', userId: 'user-1', token: 'reset-secret' });
  });
  it('does not pretend a token was created if the secret is missing', () => {
    expect(() => secretSession({ id: 'id', type: 'tokens', attributes: {} })).toThrow('did not include');
  });
});
