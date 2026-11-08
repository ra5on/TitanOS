// PLAN 74: the animation from the login page into the desktop. The login
// page notes it right after a successful login (this tab only); the desktop
// plays it once when it appears. A reload of the desktop plays nothing.
import type { LoginEnter } from './look.ts';

const KEY = 'titanos.enter';

export type Enter = { kind: Exclude<LoginEnter, 'none'>; picture: string };

export function noteEnter(kind: LoginEnter, picture: string) {
	try {
		if (kind === 'none') sessionStorage.removeItem(KEY);
		else sessionStorage.setItem(KEY, JSON.stringify({ kind, picture, at: Date.now() }));
	} catch {
		// Without storage the desktop simply appears.
	}
}

/** The animation to play now, once; only within a few seconds of the login. */
export function takeEnter(): Enter | null {
	try {
		const raw = sessionStorage.getItem(KEY);
		sessionStorage.removeItem(KEY);
		if (!raw) return null;
		const v = JSON.parse(raw) as { kind?: string; picture?: string; at?: number };
		if (!v || typeof v.at !== 'number' || Date.now() - v.at > 15_000) return null;
		if (!['zoom', 'curtain', 'focus', 'split'].includes(v.kind ?? '')) return null;
		const picture = typeof v.picture === 'string' && /^\/[\w\-/.?=&]*$/.test(v.picture) ? v.picture : '';
		return { kind: v.kind as Enter['kind'], picture };
	} catch {
		return null;
	}
}
