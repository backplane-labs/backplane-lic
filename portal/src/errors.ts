import { APIError } from './api';
import type { TranslationKey } from './i18n';

// Only application-authored message keys may be shown verbatim after translation.
// Arbitrary Error.message and API detail/title fields can contain codes or IDs.
export class CustomerError extends Error {
  constructor(public key: TranslationKey) {
    super(key);
  }
}

// A rejected promise may carry anything; the boundary narrows it to the error
// domain without changing what an Error instance would display.
export function toFailure<T>(value: T): Error {
  return value instanceof Error ? value : new Error(String(value));
}

export function isInvalidReset(error: Error): boolean {
  return (
    error instanceof APIError &&
    (error.status === 404 ||
      error.has('NOT_FOUND') ||
      error.errors.some((item) => item.source?.pointer === '/meta/passwordResetToken'))
  );
}

export function customerErrorMessage(error: Error, t: (key: TranslationKey) => string, reset = false): string {
  if (error instanceof CustomerError) return t(error.key);
  if (!(error instanceof APIError)) return t('errorUnknown');
  if (error.status === 429) {
    return error.retryAfter === undefined
      ? t('errorThrottledUnknown')
      : t('errorThrottled').replace('{seconds}', String(error.retryAfter));
  }
  if (reset && isInvalidReset(error)) return t('errorReset');
  if (
    error.has('EMAIL_INVALID') ||
    error.has('PASSWORD_INVALID') ||
    error.has('EMAIL_REQUIRED') ||
    error.has('PASSWORD_REQUIRED')
  )
    return t('errorCredentials');
  if (error.has('OTP_INVALID') || error.has('OTP_REQUIRED')) return t('errorOTP');
  if (error.has('PASSWORD_TOO_SHORT')) return t('passwordHint');
  return t('errorUnknown');
}
