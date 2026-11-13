import { ApiError } from '../api.ts';
// PLAN 65-6: only the language in use is loaded (the texts of both were
// most of the main chunk). German is the reference for the keys.
import type deBase from './de.json';
// PLAN 54: each group of the agent's functions has texts of its own.
import type docsDe from './features/docs.de.json';
import type protectDe from './features/protect.de.json';
import type foresightDe from './features/foresight.de.json';
import type familyDe from './features/family.de.json';
// PLAN 61: the desktop mode and its parts have texts of their own.
import type desktopDe from './features/desktop.de.json';
import type appaccessDe from './features/appaccess.de.json';
import type taskmanagerDe from './features/taskmanager.de.json';
import type lockDe from './features/lock.de.json';
import type settingshomeDe from './features/settingshome.de.json';
// PLAN 63: the remote view of Docker apps.
import type viewerDe from './features/viewer.de.json';
import type lookDe from './features/look.de.json';
// PLAN 66: web apps directly in the browser.
import type webappsDe from './features/webapps.de.json';
// PLAN 67-1: the menu bar of the programs.
import type menubarDe from './features/menubar.de.json';
// PLAN 67: Docker images/volumes, the design of the windows, the login picker.
import type designDe from './features/design.de.json';
// PLAN 76 (F-12-17): the texts of the error codes that had none.
import type errorsDe from './features/errors.de.json';
// PLAN 79-7: the NVR.
import type nvrDe from './features/nvr.de.json';
// PLAN 87: the phone (home screen, gestures, VM screen by touch).
import type phoneDe from './features/phone.de.json';

type Messages = typeof deBase &
	typeof docsDe &
	typeof protectDe &
	typeof foresightDe &
	typeof familyDe &
	typeof desktopDe &
	typeof appaccessDe &
	typeof taskmanagerDe &
	typeof lockDe &
	typeof settingshomeDe &
	typeof viewerDe &
	typeof lookDe &
	typeof webappsDe &
	typeof designDe &
	typeof menubarDe &
	typeof errorsDe &
	typeof nvrDe &
	typeof phoneDe;

/** The files of a language, in the order they are merged. */
const FILES = {
	de: () =>
		Promise.all([
			import('./de.json'),
			import('./features/docs.de.json'),
			import('./features/protect.de.json'),
			import('./features/foresight.de.json'),
			import('./features/family.de.json'),
			import('./features/desktop.de.json'),
			import('./features/appaccess.de.json'),
			import('./features/taskmanager.de.json'),
			import('./features/lock.de.json'),
			import('./features/settingshome.de.json'),
			import('./features/viewer.de.json'),
			import('./features/look.de.json'),
			import('./features/webapps.de.json'),
			import('./features/menubar.de.json'),
			import('./features/design.de.json'),
			import('./features/errors.de.json'),
			import('./features/nvr.de.json'),
			import('./features/phone.de.json'),
		]),
	en: () =>
		Promise.all([
			import('./en.json'),
			import('./features/docs.en.json'),
			import('./features/protect.en.json'),
			import('./features/foresight.en.json'),
			import('./features/family.en.json'),
			import('./features/desktop.en.json'),
			import('./features/appaccess.en.json'),
			import('./features/taskmanager.en.json'),
			import('./features/lock.en.json'),
			import('./features/settingshome.en.json'),
			import('./features/viewer.en.json'),
			import('./features/look.en.json'),
			import('./features/webapps.en.json'),
			import('./features/menubar.en.json'),
			import('./features/design.en.json'),
			import('./features/errors.en.json'),
			import('./features/nvr.en.json'),
			import('./features/phone.en.json'),
		]),
};

export const LOCALES = ['de', 'en'] as const;
export type Locale = (typeof LOCALES)[number];
export type MessageKey = keyof Messages;

// The loaded languages; a missing key shows the key (tests/i18n checks both files).
const messages: Partial<Record<Locale, Record<string, string>>> = {};
const loading: Partial<Record<Locale, Promise<void>>> = {};

/** Loads the texts of a language once; the UI waits for `i18n.ready`. */
export function loadLocale(locale: Locale): Promise<void> {
	if (messages[locale]) return Promise.resolve();
	loading[locale] ??= FILES[locale]().then((parts) => {
		messages[locale] = Object.assign({}, ...parts.map((part) => part.default));
	});
	return loading[locale];
}

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

export const i18n = $state<{ locale: Locale; ready: boolean }>({
	locale: detectLocale(),
	ready: false,
});
void loadLocale(i18n.locale).then(() => (i18n.ready = true));

/** Switches the language; the texts of a language not used yet load first. */
export function setLocale(locale: Locale) {
	try {
		localStorage.setItem(STORAGE_KEY, locale);
	} catch {
		// It just will not be remembered in this browser.
	}
	if (messages[locale]) i18n.locale = locale;
	else void loadLocale(locale).then(() => (i18n.locale = locale));
}

function table(): Record<string, string> {
	// Read so that texts follow once the language has loaded.
	void i18n.ready;
	return messages[i18n.locale] ?? {};
}

/** Returns the text for key in the current language; `{name}` is replaced from params. */
export function t(key: MessageKey, params?: Record<string, string | number>): string {
	const text = table()[key] ?? key;
	if (!params) return text;
	return text.replace(/\{(\w+)\}/g, (match, name: string) =>
		name in params ? String(params[name]) : match,
	);
}

/** The text for a key that is put together at run time, or `fallback` if there is none. */
export function tOr(key: string, fallback: string): string {
	return key in table() ? t(key as MessageKey) : fallback;
}

/** The text for an error code from the API, or a general one if the code is unknown. */
export function errorText(code: string): string {
	const key = `error.${code}`;
	return key in table() ? t(key as MessageKey) : t('error.internal');
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
