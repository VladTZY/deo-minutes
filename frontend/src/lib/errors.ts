import {t} from './i18n';

// Bindings reject with a Romanian, user-facing string. Anything else (a JS
// error, an empty value) is unexpected and gets the generic message.
export function errorText(e: unknown): string {
    if (typeof e === 'string' && e.trim() !== '') {
        return e;
    }
    return t("err.unexpected");
}
