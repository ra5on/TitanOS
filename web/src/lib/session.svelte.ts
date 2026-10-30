import { api, ApiError, type Preferences, type Session, type User } from './api.ts';
import { sound, resetSound } from './sound.svelte.ts';
import { sanitizeSound } from './sounds.ts';
import { useAppearance } from './theme/appearance.svelte.ts';
import { sanitizeAppearance } from './theme/themes.ts';
import { resetAssistant, useAssistantPrefs } from './assistant.svelte.ts';
import { bell } from './notifications.svelte.ts';
import { sanitizeBell } from './notify-groups.ts';

export const session = $state<{
	status: 'loading' | 'ready' | 'unreachable';
	data: Session | null;
	/** The dashboard layout stored for the account, as sent by the NAS. */
	shares: Record<string, unknown> | null;
	dashboard: Record<string, unknown> | null;
	docker: Record<string, unknown> | null;
	vnc: Record<string, unknown> | null;
}>({ status: 'loading', data: null, shares: null, dashboard: null, docker: null, vnc: null });

async function loadPreferences() {
	try {
		const prefs = await api.get<Preferences>('/preferences');
		session.shares = prefs.shares ?? null;
		session.dashboard = prefs.dashboard;
		session.docker = prefs.docker;
		session.vnc = prefs.vnc;
		sound.settings = sanitizeSound(prefs.sound);
		bell.settings = sanitizeBell(prefs.bell);
		if (prefs.appearance)
			useAppearance(sanitizeAppearance(prefs.appearance, prefs.dashboard?.visual));
		useAssistantPrefs(prefs.assistant);
	} catch {
		// The UI works with defaults; saving will report the problem.
		session.shares = null;
		session.dashboard = null;
		session.docker = null;
		session.vnc = null;
	}
}

/** Asks the NAS who we are. Called at start and after anything that changes it. */
export async function refreshSession() {
	try {
		const data = await api.get<Session>('/session');
		if (data.user?.name !== session.data?.user?.name) {
			resetSound();
			resetAssistant();
		}
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
	resetAssistant();
	await refreshSession();
}

/** Called when the NAS says the session is gone (restart, timeout). */
export function sessionExpired() {
	resetSound();
	resetAssistant();
	if (session.data) session.data = { ...session.data, user: null };
}
