import { describe, expect, it } from 'vitest';
import { APIError } from './api';
import { CustomerError, customerErrorMessage } from './errors';
import type { TranslationKey } from './i18n';

const t = (key: TranslationKey) => key;

describe('customerErrorMessage', () => {
  it('gives the same text for a wrong email and a wrong password', () => {
    const email = new APIError(401, [{ code: 'EMAIL_INVALID', title: 'Unauthorized', detail: 'email must be valid' }]);
    const password = new APIError(401, [{ code: 'PASSWORD_INVALID', title: 'Unauthorized', detail: 'password must be valid' }]);
    expect(customerErrorMessage(email, t)).toBe('errorCredentials');
    expect(customerErrorMessage(password, t)).toBe('errorCredentials');
  });

  it('never renders codes or identifiers from the API', () => {
    const leak = new APIError(404, [{ code: 'NOT_FOUND', title: 'Not found', detail: "The requested user 'f15af6df' was not found" }]);
    const message = customerErrorMessage(leak, t);
    expect(message).toBe('errorUnknown');
    expect(message).not.toContain('f15af6df');
  });

  it('reads a used reset link as invalid on the reset page only', () => {
    const used = new APIError(404, [{ code: 'NOT_FOUND', title: 'Not found', detail: "The requested user 'f15af6df' was not found" }]);
    expect(customerErrorMessage(used, t, true)).toBe('errorReset');
  });

  it('maps throttling with and without Retry-After', () => {
    expect(customerErrorMessage(new APIError(429, [], 12), t)).toBe('errorThrottled');
    expect(customerErrorMessage(new APIError(429, []), t)).toBe('errorThrottledUnknown');
  });

  it('maps a wrong second factor', () => {
    expect(customerErrorMessage(new APIError(401, [{ code: 'OTP_INVALID', title: 'Unauthorized' }]), t)).toBe('errorOTP');
  });

  it('shows application-authored messages and hides other errors', () => {
    expect(customerErrorMessage(new CustomerError('passwordsDiffer'), t)).toBe('passwordsDiffer');
    expect(customerErrorMessage(new Error('TypeError: x is undefined'), t)).toBe('errorUnknown');
  });
});
