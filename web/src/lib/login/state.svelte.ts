import { api } from '../api.ts';
import { sanitizeLoginLook, type LoginLook } from './look.ts';

const CACHE_KEY = 'titanos.loginLook';

/** Key under which the editor hands an unsaved look to the preview tab. */
export const PREVIEW_KEY = 'titanos.loginPreview';

function cached(): LoginLook | null {
	try {
		const value = localStorage.getItem(CACHE_KEY);
		return value ? sanitizeLoginLook(JSON.parse(value)) : null;
	} catch {
		return null;
	}
}

/**
 * The look of the login page as configured on the NAS; null = the default
 * look. The last answer is kept in this browser so that the first paint of
 * the login page already looks right.
 */
export const loginLook = $state<{ value: LoginLook | null; loaded: boolean }>({
	value: typeof localStorage === 'undefined' ? null : cached(),
	loaded: false,
});

/** Asks the NAS for the look of the login page (no login needed). */
export async function loadLoginLook() {
	try {
		const data = await api.get<Record<string, unknown>>('/login-appearance');
		useLoginLook(data && data.configured === true ? sanitizeLoginLook(data) : null);
	} catch {
		// The default look; the page works without it.
	} finally {
		loginLook.loaded = true;
	}
}

/** Uses a look that was just saved or loaded. */
export function useLoginLook(value: LoginLook | null) {
	loginLook.value = value;
	try {
		if (value) localStorage.setItem(CACHE_KEY, JSON.stringify(value));
		else localStorage.removeItem(CACHE_KEY);
	} catch {
		// Not remembered in this browser.
	}
}
