import { api, ApiError, type Preferences, type Session, type User } from './api.ts';
import { sound, resetSound } from './sound.svelte.ts';
import { sanitizeSound } from './sounds.ts';
import { useAppearance } from './theme/appearance.svelte.ts';
import { sanitizeAppearance } from './theme/themes.ts';

export const session = $state<{
	status: 'loading' | 'ready' | 'unreachable';
	data: Session | null;
	/** The dashboard layout stored for the account, as sent by the NAS. */
	dashboard: Record<string, unknown> | null;
}>({ status: 'loading', data: null, dashboard: null });

async function loadPreferences() {
	try {
		const prefs = await api.get<Preferences>('/preferences');
		session.dashboard = prefs.dashboard;
		sound.settings = sanitizeSound(prefs.sound);
		if (prefs.appearance) useAppearance(sanitizeAppearance(prefs.appearance));
	} catch {
		// The UI works with defaults; saving will report the problem.
		session.dashboard = null;
	}
}

/** Asks the NAS who we are. Called at start and after anything that changes it. */
export async function refreshSession() {
	try {
		const data = await api.get<Session>('/session');
		if (data.user?.name !== session.data?.user?.name) resetSound();
		if (data.user) await loadPreferences();
		session.data = data;
		session.status = 'ready';
	} catch {
		session.status = 'unreachable';
	}
}

export async function login(name: string, password: string, code = '') {
	await api.post<User>('/login', { name, password, code });
	await refreshSession();
}

export async function setupOwner(name: string, password: string) {
	await api.post<User>('/setup', { name, password });
	await refreshSession();
}

export async function logout() {
	try {
		await api.post('/logout');
	} catch (err) {
		if (!(err instanceof ApiError)) throw err;
	}
	await refreshSession();
}

/** Called when the NAS says the session is gone (restart, timeout). */
export function sessionExpired() {
	resetSound();
	if (session.data) session.data = { ...session.data, user: null };
}
