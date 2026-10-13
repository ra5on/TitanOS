import { api } from '../api.ts';
import { cssVariables, dataAttributes, defaultAppearance, sanitizeAppearance, type Appearance } from './themes.ts';

const CACHE_KEY = 'titanos.appearance';

/** The appearance in use, and whether the system prefers a light theme (for "auto"). */
export const appearance = $state<{ current: Appearance; systemLight: boolean }>({
	current: defaultAppearance(),
	systemLight: false
});

function apply() {
	const root = document.documentElement;
	for (const [name, value] of Object.entries(cssVariables(appearance.current, appearance.systemLight))) {
		root.style.setProperty(name, value);
	}
	for (const [name, value] of Object.entries(dataAttributes(appearance.current, appearance.systemLight))) {
		root.setAttribute(name, value);
	}
}

/**
 * Applies the appearance last used in this browser, so that the login page
 * and the first paint already look right, and follows the system theme.
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
	apply();
}

/** Uses settings for the whole UI without saving them to the account. */
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
