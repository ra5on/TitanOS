import { api } from '../api.ts';
import { isAgentPath } from '../agent-app.ts';
import {
	cssVariables,
	dataAttributes,
	defaultAppearance,
	sanitizeAppearance,
	type Appearance,
} from './themes.ts';

const CACHE_KEY = 'titanos.appearance';

/** The appearance in use, and whether the system prefers a light theme (for "auto"). */
export const appearance = $state<{ current: Appearance; systemLight: boolean }>({
	current: defaultAppearance(),
	systemLight: false,
});

function apply() {
	const root = document.documentElement;
	// The KI-Agent keeps its own look (PLAN 64): the colour strength of the
	// desktop does not reach it.
	const value = isAgentPath(location.pathname)
		? { ...appearance.current, saturation: 100 }
		: appearance.current;
	for (const [name, value_] of Object.entries(cssVariables(value, appearance.systemLight))) {
		root.style.setProperty(name, value_);
	}
	for (const [name, value_] of Object.entries(dataAttributes(value, appearance.systemLight))) {
		root.setAttribute(name, value_);
	}
}

/**
 * Applies the appearance last used in this browser, so that the login page
 * and the first paint already look right, and follows the system theme.
 * Every window of the desktop is a document of its own: a change in one of
 * them (Settings › Appearance) reaches the others through the stored copy.
 */
export function initAppearance() {
	try {
		const cached = localStorage.getItem(CACHE_KEY);
		if (cached) appearance.current = sanitizeAppearance(JSON.parse(cached));
	} catch {
		// No storage or broken content: defaults.
	}
	const query = window.matchMedia('(prefers-color-scheme: light)');
	appearance.systemLight = query.matches;
	query.addEventListener('change', (event) => {
		appearance.systemLight = event.matches;
		apply();
	});
	window.addEventListener('storage', (event) => {
		if (event.key !== CACHE_KEY || !event.newValue) return;
		try {
			appearance.current = sanitizeAppearance(JSON.parse(event.newValue));
			apply();
		} catch {
			// Broken content: keep what is shown.
		}
	});
	apply();
}

/** Uses settings for the whole UI (all windows) without saving them to the account. */
export function useAppearance(value: Appearance) {
	appearance.current = value;
	apply();
	try {
		localStorage.setItem(CACHE_KEY, JSON.stringify(value));
	} catch {
		// Private mode: it just will not be remembered in this browser.
	}
}

/** Saves settings to the account on the NAS and uses them. */
export async function saveAppearance(value: Appearance) {
	await api.put('/preferences/appearance', value);
	useAppearance(value);
}
