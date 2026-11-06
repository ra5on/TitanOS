import { ApiError } from '../api.ts';
import deBase from './de.json';
import enBase from './en.json';
// PLAN 54: each group of the agent's functions has texts of its own.
import docsDe from './features/docs.de.json';
import docsEn from './features/docs.en.json';
import protectDe from './features/protect.de.json';
import protectEn from './features/protect.en.json';
import foresightDe from './features/foresight.de.json';
import foresightEn from './features/foresight.en.json';
import familyDe from './features/family.de.json';
import familyEn from './features/family.en.json';
// PLAN 61: the desktop mode and its parts have texts of their own.
import desktopDe from './features/desktop.de.json';
import desktopEn from './features/desktop.en.json';
import appaccessDe from './features/appaccess.de.json';
import appaccessEn from './features/appaccess.en.json';
import taskmanagerDe from './features/taskmanager.de.json';
import taskmanagerEn from './features/taskmanager.en.json';
import lockDe from './features/lock.de.json';
import lockEn from './features/lock.en.json';
import settingshomeDe from './features/settingshome.de.json';
import settingshomeEn from './features/settingshome.en.json';

const de = {
	...deBase,
	...docsDe,
	...protectDe,
	...foresightDe,
	...familyDe,
	...desktopDe,
	...appaccessDe,
	...taskmanagerDe,
	...lockDe,
	...settingshomeDe,
};
const en = {
	...enBase,
	...docsEn,
	...protectEn,
	...foresightEn,
	...familyEn,
	...desktopEn,
	...appaccessEn,
	...taskmanagerEn,
	...lockEn,
	...settingshomeEn,
};

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
		name in params ? String(params[name]) : match,
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

/**
 * The text for an error thrown by `api`; anything else counts as an internal error.
 * With `detail`, the server's own message follows on a new line, for errors whose
 * cause only the server can name (e.g. what a tool printed).
 */
export function apiErrorText(err: unknown, options?: { detail?: boolean }): string {
	if (!(err instanceof ApiError)) return t('error.internal');
	const text = errorText(err.code);
	return options?.detail && err.message ? `${text}\n${err.message}` : text;
}
