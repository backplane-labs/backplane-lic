import { createContext, useContext, useEffect, useState, type ReactNode } from 'react';
import i18next from 'i18next';
import { englishResources } from '@backplane-labs/backplane-ui';
import { initReactI18next } from 'react-i18next';

export const en = {
  portal: 'Customer portal', licenses: 'My licenses', tokens: 'API tokens', profile: 'Profile & security',
  signIn: 'Sign in', signOut: 'Sign out', email: 'Email', password: 'Password',
  welcome: 'Your software, in one place.', welcomeDetail: 'Access your licenses and manage your account securely.',
  loginHeading: 'Welcome back', loginDetail: 'Sign in with your customer account.',
  forgot: 'Forgot password?', otp: 'Authentication code', otpHelp: 'Enter the six-digit code from your authenticator app.',
  backLogin: 'Back to sign in', forgotHeading: 'Reset your password', forgotDetail: 'We’ll send a reset link if an account exists for this email address.',
  sendLink: 'Send reset link', linkSent: 'If your account exists, a password reset link has been requested. Check your email.',
  newPassword: 'New password', confirmPassword: 'Confirm new password', passwordHint: 'Use at least 6 characters. Maximum 72 bytes.',
  passwordsDiffer: 'The passwords do not match.', resetPassword: 'Set new password', resetDone: 'Your password has been reset. Sign in with your new password.',
  invalidReset: 'This reset link is incomplete. Request a new password reset link.',
  loading: 'Loading…', working: 'Working…', retry: 'Try again', cancel: 'Cancel', close: 'Close',
  licensesDetail: 'Your license keys, usage, and included features.',
  noLicenses: 'No licenses yet', noLicensesDetail: 'Licenses assigned to your account will appear here. Contact your vendor if you expected to see one.',
  product: 'Product', policy: 'Plan', status: 'Status', expiry: 'Expires', uses: 'Usage',
  unlimited: 'Unlimited', noExpiry: 'No expiry', key: 'License key', entitlements: 'Included features',
  noEntitlements: 'No additional entitlements.', details: 'View license', backLicenses: 'All licenses',
  copy: 'Copy key', copied: 'Copied', download: 'Download license file',
  protectedLicense: 'License file downloads are not available for this protected license.',
  downloadHelp: 'Download a signed .lic file for offline use.',
  tokensDetail: 'Create tokens for your own integrations. Keep them as safe as your password.',
  createToken: 'Create token', tokenName: 'Token name', tokenNameHint: 'For example, my build server',
  tokenExpiry: 'Expiry (optional)', tokenExpiryHint: 'Leave blank to use the API default (two weeks).',
  noTokens: 'No API tokens', noTokensDetail: 'Create a token to connect an integration to your account.',
  name: 'Name', created: 'Created', unnamed: 'Unnamed token', current: 'This session',
  regenerate: 'Regenerate', revoke: 'Revoke', regenerateTitle: 'Regenerate this token?',
  regenerateDetail: 'The old secret will stop working immediately. Update any integrations that use it.',
  revokeTitle: 'Revoke this token?', revokeDetail: 'Any integration using this token will lose access. This cannot be undone.',
  currentWarning: 'This is your portal session token. Revoking it will sign you out.',
  secretTitle: 'Save your token', secretDetail: 'This secret is shown only once. Copy it now and store it securely.',
  copyToken: 'Copy token', tokenSecret: 'Token secret', tokenCreated: 'Token created.', tokenRegenerated: 'Token regenerated.', tokenRevoked: 'Token revoked.',
  account: 'Account', profileDetail: 'Manage your password and two-factor authentication.',
  changePassword: 'Change password', oldPassword: 'Current password', updatePassword: 'Update password',
  passwordUpdated: 'Password updated. Other sessions have been signed out.',
  twoFactor: 'Two-factor authentication', twoFactorDetail: 'Add an authenticator app for an extra layer of security at sign in.',
  enabled: 'Enabled', notEnabled: 'Not enabled', enable2FA: 'Set up two-factor authentication',
  confirmSetup: 'Confirm setup', setupDetail: 'Scan this code with your authenticator app, or enter the setup key manually. Then enter a code to finish setup.',
  manualSecret: 'Setup key', copySetupKey: 'Copy setup key', skipContent: 'Skip to content', qrAlt: 'Authenticator setup QR code',
  passwordVerify: 'Confirm your current password to begin.',
  disable2FA: 'Disable two-factor authentication', disableDetail: 'Enter a new code from your authenticator app to remove two-factor authentication.',
  factorEnabled: 'Two-factor authentication enabled.', factorDisabled: 'Two-factor authentication disabled.',
  pendingSetup: 'Finish the setup below to enable two-factor authentication.',
  discardSetup: 'Discard setup',
  securityNote: 'Your session is kept in this tab only. Closing the tab signs you out locally.',
  customerOnly: 'This portal is for customer accounts only. Use a customer account with the user role.',
  sessionExpired: 'Your session is no longer valid. Sign in again.',
  light: 'Light', dark: 'Dark', system: 'System', theme: 'Appearance', language: 'Language',
  unknown: 'Not available', active: 'Active', inactive: 'Inactive', suspended: 'Suspended', expired: 'Expired', banned: 'Banned',
  notFound: 'Page not found', notFoundDetail: 'This page does not exist. Return to your licenses.',
  loadError: 'Could not load this page.', copiedError: 'Could not copy. Select and copy the text manually.',
} as const;
export type TranslationKey = keyof typeof en;
const ru: Record<TranslationKey, string> = {
  portal: 'Личный кабинет', licenses: 'Мои лицензии', tokens: 'API-токены', profile: 'Профиль и безопасность',
  signIn: 'Войти', signOut: 'Выйти', email: 'Электронная почта', password: 'Пароль',
  welcome: 'Ваше ПО — в одном месте.', welcomeDetail: 'Управляйте лицензиями и безопасностью учётной записи.',
  loginHeading: 'Вход в личный кабинет', loginDetail: 'Войдите с вашей учётной записью клиента.',
  forgot: 'Забыли пароль?', otp: 'Код аутентификации', otpHelp: 'Введите шестизначный код из приложения-аутентификатора.',
  backLogin: 'Вернуться ко входу', forgotHeading: 'Сброс пароля', forgotDetail: 'Если учётная запись существует, мы отправим ссылку для сброса на этот адрес.',
  sendLink: 'Отправить ссылку', linkSent: 'Если учётная запись существует, запрошена ссылка для сброса пароля. Проверьте почту.',
  newPassword: 'Новый пароль', confirmPassword: 'Повторите новый пароль', passwordHint: 'Не менее 6 символов, не более 72 байт.',
  passwordsDiffer: 'Пароли не совпадают.', resetPassword: 'Установить пароль', resetDone: 'Пароль изменён. Войдите с новым паролем.',
  invalidReset: 'Ссылка для сброса неполная. Запросите новую ссылку.',
  loading: 'Загрузка…', working: 'Выполняется…', retry: 'Повторить', cancel: 'Отмена', close: 'Закрыть',
  licensesDetail: 'Ваши лицензионные ключи, использование и доступные функции.',
  noLicenses: 'Лицензий пока нет', noLicensesDetail: 'Здесь появятся лицензии вашей учётной записи. Если вы ожидали увидеть лицензию, обратитесь к поставщику.',
  product: 'Продукт', policy: 'Тариф', status: 'Статус', expiry: 'Срок действия', uses: 'Использование',
  unlimited: 'Без ограничений', noExpiry: 'Бессрочно', key: 'Лицензионный ключ', entitlements: 'Доступные функции',
  noEntitlements: 'Нет дополнительных функций.', details: 'Открыть лицензию', backLicenses: 'Все лицензии',
  copy: 'Скопировать ключ', copied: 'Скопировано', download: 'Скачать файл лицензии',
  protectedLicense: 'Для этой защищённой лицензии скачивание файла недоступно.',
  downloadHelp: 'Скачайте подписанный файл .lic для автономного использования.',
  tokensDetail: 'Создавайте токены для ваших интеграций. Храните их так же надёжно, как пароль.',
  createToken: 'Создать токен', tokenName: 'Название токена', tokenNameHint: 'Например, сервер сборки',
  tokenExpiry: 'Срок действия (необязательно)', tokenExpiryHint: 'Оставьте пустым для срока по умолчанию — две недели.',
  noTokens: 'Нет API-токенов', noTokensDetail: 'Создайте токен, чтобы подключить интеграцию к вашей учётной записи.',
  name: 'Название', created: 'Создан', unnamed: 'Токен без названия', current: 'Этот сеанс',
  regenerate: 'Перевыпустить', revoke: 'Отозвать', regenerateTitle: 'Перевыпустить токен?',
  regenerateDetail: 'Старый секрет сразу перестанет работать. Обновите его во всех интеграциях.',
  revokeTitle: 'Отозвать токен?', revokeDetail: 'Интеграции с этим токеном потеряют доступ. Это действие нельзя отменить.',
  currentWarning: 'Это токен вашего сеанса. После его отзыва потребуется войти заново.',
  secretTitle: 'Сохраните токен', secretDetail: 'Секрет показывается только один раз. Скопируйте его сейчас и сохраните в надёжном месте.',
  copyToken: 'Скопировать токен', tokenSecret: 'Секрет токена', tokenCreated: 'Токен создан.', tokenRegenerated: 'Токен перевыпущен.', tokenRevoked: 'Токен отозван.',
  account: 'Учётная запись', profileDetail: 'Управляйте паролем и двухфакторной аутентификацией.',
  changePassword: 'Изменение пароля', oldPassword: 'Текущий пароль', updatePassword: 'Изменить пароль',
  passwordUpdated: 'Пароль изменён. Остальные сеансы завершены.',
  twoFactor: 'Двухфакторная аутентификация', twoFactorDetail: 'Подключите приложение-аутентификатор для дополнительной защиты входа.',
  enabled: 'Включена', notEnabled: 'Не включена', enable2FA: 'Настроить двухфакторную аутентификацию',
  confirmSetup: 'Подтвердить настройку', setupDetail: 'Отсканируйте код в приложении-аутентификаторе или введите ключ вручную. Затем введите код, чтобы завершить настройку.',
  manualSecret: 'Ключ настройки', copySetupKey: 'Скопировать ключ настройки', skipContent: 'Перейти к содержимому', qrAlt: 'QR-код настройки аутентификатора',
  passwordVerify: 'Подтвердите текущий пароль, чтобы начать настройку.',
  disable2FA: 'Отключить двухфакторную аутентификацию', disableDetail: 'Введите новый код из приложения-аутентификатора, чтобы отключить двухфакторную аутентификацию.',
  factorEnabled: 'Двухфакторная аутентификация включена.', factorDisabled: 'Двухфакторная аутентификация отключена.',
  pendingSetup: 'Завершите настройку ниже, чтобы включить двухфакторную аутентификацию.', discardSetup: 'Отменить настройку',
  securityNote: 'Сеанс хранится только в этой вкладке. Закрытие вкладки завершит локальный сеанс.',
  customerOnly: 'Этот кабинет предназначен только для клиентов. Войдите с учётной записью с ролью user.',
  sessionExpired: 'Сеанс истёк или был отозван. Войдите снова.',
  light: 'Светлая', dark: 'Тёмная', system: 'Системная', theme: 'Оформление', language: 'Язык',
  unknown: 'Нет данных', active: 'Активна', inactive: 'Неактивна', suspended: 'Приостановлена', expired: 'Истекла', banned: 'Заблокирована',
  notFound: 'Страница не найдена', notFoundDetail: 'Такой страницы нет. Вернитесь к вашим лицензиям.',
  loadError: 'Не удалось загрузить страницу.', copiedError: 'Не удалось скопировать. Выделите и скопируйте текст вручную.',
};

i18next.use(initReactI18next).init({
  lng: 'en', fallbackLng: 'en', interpolation: { escapeValue: false },
  resources: { en: { 'backplane.ui': englishResources }, ru: { 'backplane.ui': { ...englishResources, close: 'Закрыть' } } },
});

type Locale = 'en' | 'ru';
const LocaleContext = createContext<{ locale: Locale; setLocale: (value: Locale) => void; t: (key: TranslationKey) => string }>({
  locale: 'en', setLocale: () => {}, t: key => en[key],
});
export function LocaleProvider({ children }: { children: ReactNode }) {
  const [locale, setLocale] = useState<Locale>(() => {
    try { const stored = sessionStorage.getItem('lic.portal.locale'); if (stored === 'en' || stored === 'ru') return stored; } catch { /* optional preference */ }
    return navigator.language.startsWith('ru') ? 'ru' : 'en';
  });
  useEffect(() => {
    document.documentElement.lang = locale;
    void i18next.changeLanguage(locale);
    try { sessionStorage.setItem('lic.portal.locale', locale); } catch { /* optional preference */ }
  }, [locale]);
  return <LocaleContext.Provider value={{ locale, setLocale, t: key => (locale === 'ru' ? ru : en)[key] }}>{children}</LocaleContext.Provider>;
}
export const useLocale = () => useContext(LocaleContext);
