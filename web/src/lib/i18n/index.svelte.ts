import de from './de.json';
import en from './en.json';

export const LOCALES = ['de', 'en'] as const;
export type Locale = (typeof LOCALES)[number];
export type MessageKey = keyof typeof de;

// German is the reference: a key missing in another language is a type error.
const messages: Record<Locale, Record<MessageKey, string>> = { de, en };

const STORAGE_KEY = 'titanos.locale';

function detectLocale(): Locale {
	if (typeof navigator === 'undefined') return 'en';
	try {
		const stored = localStorage.getItem(STORAGE_KEY);
		if (stored === 'de' || stored === 'en') return stored;
	} catch {
		// No storage: follow the browser.
	}
	return navigator.language.toLowerCase().startsWith('de') ? 'de' : 'en';
}

export const i18n = $state<{ locale: Locale }>({ locale: detectLocale() });

export function setLocale(locale: Locale) {
	i18n.locale = locale;
	try {
		localStorage.setItem(STORAGE_KEY, locale);
	} catch {
		// It just will not be remembered in this browser.
	}
}

/** Returns the text for key in the current language; `{name}` is replaced from params. */
export function t(key: MessageKey, params?: Record<string, string | number>): string {
	const text = messages[i18n.locale][key];
	if (!params) return text;
	return text.replace(/\{(\w+)\}/g, (match, name: string) =>
		name in params ? String(params[name]) : match
	);
}

/** The text for a key that is put together at run time, or `fallback` if there is none. */
export function tOr(key: string, fallback: string): string {
	return key in de ? t(key as MessageKey) : fallback;
}

/** The text for an error code from the API, or a general one if the code is unknown. */
export function errorText(code: string): string {
	const key = `error.${code}`;
	return key in de ? t(key as MessageKey) : t('error.internal');
}
