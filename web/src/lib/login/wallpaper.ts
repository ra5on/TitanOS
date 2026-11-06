// PLAN 64: the login page shows the desktop's wallpaper. The account's
// wallpaper needs a login to be read, so the page uses the built-in one the
// last account of this browser had (only its name is kept); an own picture
// of the account stays private and the page shows the standard one instead.
import { WALLPAPERS } from '../desktop/prefs.ts';

const KEY = 'titanos.login.wallpaper';
export const DEFAULT_LOGIN_WALLPAPER = 'wave';

/** Remembers the built-in wallpaper of the account now logged in. */
export function rememberLoginWallpaper(wallpaper: { source: string; id?: string }) {
	try {
		if (wallpaper.source === 'builtin' && wallpaper.id) localStorage.setItem(KEY, wallpaper.id);
		else localStorage.removeItem(KEY);
	} catch {
		// Without storage the page shows the standard wallpaper.
	}
}

/** The built-in wallpaper the login page shows. */
export function loginWallpaper(): string {
	try {
		const id = localStorage.getItem(KEY) ?? '';
		if ((WALLPAPERS as readonly string[]).includes(id)) return id;
	} catch {
		// Fall through to the standard.
	}
	return DEFAULT_LOGIN_WALLPAPER;
}
