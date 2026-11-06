// The lock of the session (PLAN 61, lock screen of the desktop). The NAS
// keeps the lock: while it holds, every request of this browser answers 423
// "locked" (api.ts reports that here), except GET /session, unlocking and
// logging out. This module knows whether the screen is locked, locks it by
// hand (account menu, Strg/⌘+Alt+L) and after the account's minutes without
// use, and unlocks it with the password (and code) or a passkey.
//
// Every window of the NAS shares one cookie and so one lock: tabs and the
// same-origin frames of the desktop tell each other through a
// BroadcastChannel (frames also through window.top), and they share the time
// of the last use in localStorage, so that an idle second tab does not lock
// the one in use. Only the top window shows the lock screen; a frame shows
// nothing while locked.

import { api, ApiError, setLockedHandler, type Preferences } from './api.ts';
import { confirmWithPasskey } from './passkeys.ts';
import { refreshSession, session } from './session.svelte.ts';

/** Minutes without use before the lock while the account chose nothing (the NAS uses the same). */
export const LOCK_DEFAULT_MINUTES = 15;
/** The longest automatic lock the NAS accepts. */
export const LOCK_MAX_MINUTES = 24 * 60;
/** Choices offered for the automatic lock; 0 = off. */
export const LOCK_CHOICES = [0, 5, 10, 15, 30, 60] as const;

const CHANNEL = 'titanos-lock';
const ACTIVITY_KEY = 'titanos.lastActivity';
/** How often the time of the last use is written for the other windows. */
const SHARE_EVERY_MS = 5_000;
/** How often the NAS hears of use, so that its own automatic lock waits too. */
const PING_EVERY_MS = 60_000;
/** How often the timer looks at the clock (comparing times survives sleep). */
const CHECK_EVERY_MS = 10_000;
/** How often a locked window asks whether another one unlocked. */
const LOCKED_POLL_MS = 30_000;

export const lockState = $state({
	/** Set by a 423 "locked" answer, a message of another window or lock(). */
	locked: false,
	/** An unlock is under way. */
	busy: false,
});

/** Whether the session is locked: by this window's knowledge or by what the NAS said at start. */
export function isLocked(): boolean {
	return !!session.data?.user && (lockState.locked || !!session.data.locked);
}

/** Whether this document runs in a frame of the desktop (same origin), not as the top window. */
export function inFrame(): boolean {
	try {
		return typeof window !== 'undefined' && window.top !== window.self;
	} catch {
		return true;
	}
}

/** The account's automatic lock in minutes from its desktop preferences; 0 = off. */
export function readLockMinutes(desktop: unknown): number {
	const value =
		desktop && typeof desktop === 'object'
			? (desktop as Record<string, unknown>).lockMinutes
			: undefined;
	if (typeof value !== 'number' || !Number.isFinite(value)) return LOCK_DEFAULT_MINUTES;
	if (value <= 0) return 0;
	return Math.min(Math.round(value), LOCK_MAX_MINUTES);
}

/** The minutes that count now: what the NAS said, else the default. */
export function lockMinutes(): number {
	const minutes = session.data?.lockMinutes;
	return typeof minutes === 'number' ? minutes : LOCK_DEFAULT_MINUTES;
}

/** Whether the time without use has run out (minutes 0 never locks). */
export function idleExpired(lastActivity: number, now: number, minutes: number): boolean {
	return minutes > 0 && now - lastActivity >= minutes * 60_000;
}

/** Strg+Alt+L, on the Mac ⌘+Alt+L (by key position: Alt+L types "¬" there). */
export function isLockShortcut(
	e: Pick<KeyboardEvent, 'code' | 'altKey' | 'ctrlKey' | 'metaKey' | 'shiftKey'> & {
		getModifierState?: (key: string) => boolean;
	},
): boolean {
	// On Windows AltGr comes as Ctrl+Alt: AltGr+L types a letter on some
	// layouts (Polish ł) and must not lock.
	if (e.getModifierState?.('AltGraph')) return false;
	return e.code === 'KeyL' && e.altKey && (e.ctrlKey || e.metaKey) && !e.shiftKey;
}

let channel: BroadcastChannel | null = null;
function tell(type: 'locked' | 'unlocked') {
	try {
		channel?.postMessage({ type });
	} catch {
		// A closed channel: the other windows notice on their next request.
	}
	if (inFrame()) {
		try {
			window.top?.postMessage({ type: `titanos-${type}` }, location.origin);
		} catch {
			// Not the same origin: nothing to tell.
		}
	}
}

/** Shows the lock screen; with `tellOthers` the other windows follow. */
function markLocked(tellOthers: boolean) {
	if (!session.data?.user) return;
	const was = lockState.locked;
	lockState.locked = true;
	if (tellOthers && !was) tell('locked');
}

async function markUnlocked(tellOthers: boolean) {
	lockState.locked = false;
	if (session.data) session.data = { ...session.data, locked: false };
	if (tellOthers) tell('unlocked');
	lastActivity = Date.now();
	shareActivity(true);
	// Preferences and services were not loaded while locked.
	await refreshSession();
}

// A 423 "locked" answer anywhere in this window shows the lock screen.
setLockedHandler(() => markLocked(true));

/** Locks the session now (account menu, shortcut, timer). */
export async function lock(): Promise<void> {
	if (!session.data?.user || isLocked()) return;
	markLocked(true);
	try {
		await api.post('/session/lock');
	} catch (err) {
		// Logged out meanwhile: the login page follows from the session.
		if (err instanceof ApiError && err.status === 401) await refreshSession();
	}
}

export type UnlockProof = { password: string; code?: string };

/** Unlocks with the password and, with two-factor login, the code. Throws the ApiError of the NAS. */
export async function unlock(proof: UnlockProof): Promise<void> {
	lockState.busy = true;
	try {
		await api.post('/session/unlock', { password: proof.password, code: proof.code ?? '' });
		await markUnlocked(true);
	} finally {
		lockState.busy = false;
	}
}

/** Unlocks with a passkey of the account. Throws when the browser or the NAS refuses. */
export async function unlockWithPasskey(): Promise<void> {
	lockState.busy = true;
	try {
		const passkey = await confirmWithPasskey();
		await api.post('/session/unlock', { passkey });
		await markUnlocked(true);
	} finally {
		lockState.busy = false;
	}
}

/**
 * Stores the automatic lock of the account: the field lockMinutes of the
 * desktop preferences, the other fields of the desktop as they are.
 */
export async function saveLockMinutes(minutes: number): Promise<void> {
	const value = minutes <= 0 ? 0 : Math.min(Math.round(minutes), LOCK_MAX_MINUTES);
	const prefs = await api.get<Preferences>('/preferences');
	const desktop = prefs.desktop && typeof prefs.desktop === 'object' ? prefs.desktop : {};
	await api.put('/preferences/desktop', { ...desktop, lockMinutes: value });
	if (session.data) session.data = { ...session.data, lockMinutes: value };
}

// The time of the last use, shared by all windows of the NAS.
let lastActivity = Date.now();
let lastShared = 0;
let lastPing = Date.now();

function storedActivity(): number {
	try {
		return Number(localStorage.getItem(ACTIVITY_KEY)) || 0;
	} catch {
		return 0;
	}
}

function shareActivity(force = false) {
	const now = Date.now();
	if (!force && now - lastShared < SHARE_EVERY_MS) return;
	lastShared = now;
	try {
		localStorage.setItem(ACTIVITY_KEY, String(lastActivity));
	} catch {
		// Private mode: this window counts on its own.
	}
}

/** Notes that someone uses this window (keys, pointer, wheel, touch). */
export function noteActivity() {
	if (isLocked()) return;
	lastActivity = Date.now();
	shareActivity();
}

const ACTIVITY_EVENTS = ['pointerdown', 'pointermove', 'keydown', 'wheel', 'touchstart'] as const;

/**
 * Starts listening: the shortcut, use of the window, the other windows and,
 * in the top window, the timer of the automatic lock. Returns the stop.
 */
export function startLock(): () => void {
	if (typeof window === 'undefined') return () => {};
	const top = !inFrame();
	lastActivity = Date.now();
	const onKey = (e: KeyboardEvent) => {
		if (isLockShortcut(e)) {
			e.preventDefault();
			void lock();
		}
	};
	const onUse = () => noteActivity();
	window.addEventListener('keydown', onKey, true);
	for (const name of ACTIVITY_EVENTS)
		window.addEventListener(name, onUse, { passive: true, capture: true });

	try {
		channel = new BroadcastChannel(CHANNEL);
		channel.onmessage = (e: MessageEvent) => {
			if (e.data?.type === 'locked') markLocked(false);
			else if (e.data?.type === 'unlocked' && isLocked()) void markUnlocked(false);
		};
	} catch {
		channel = null;
	}
	// A frame without BroadcastChannel tells the top window directly.
	const onMessage = (e: MessageEvent) => {
		if (e.origin !== location.origin) return;
		if (e.data?.type === 'titanos-locked') markLocked(false);
		else if (e.data?.type === 'titanos-unlocked' && isLocked()) void markUnlocked(false);
	};
	window.addEventListener('message', onMessage);

	let timer: ReturnType<typeof setInterval> | undefined;
	let lockedPoll = 0;
	if (top) {
		timer = setInterval(() => {
			if (!session.data?.user) return;
			const now = Date.now();
			if (isLocked()) {
				// Unlocked in a window that could not tell us?
				if (now - lockedPoll >= LOCKED_POLL_MS) {
					lockedPoll = now;
					void api
						.get<{ locked?: boolean; user: unknown }>('/session')
						.then((s) => {
							if (s.user && !s.locked && isLocked()) void markUnlocked(false);
						})
						.catch(() => {});
				}
				return;
			}
			const last = Math.max(lastActivity, storedActivity());
			if (idleExpired(last, now, lockMinutes())) {
				void lock();
				return;
			}
			// The NAS locks on its own after the same time without requests;
			// use without requests (typing in a console) tells it now and then.
			if (last > lastPing && now - lastPing >= PING_EVERY_MS) {
				lastPing = now;
				void api
					.get<{ locked?: boolean; user: unknown }>('/session')
					.then((s) => {
						if (s.locked) markLocked(true);
					})
					.catch(() => {});
			}
		}, CHECK_EVERY_MS);
	}
	return () => {
		window.removeEventListener('keydown', onKey, true);
		for (const name of ACTIVITY_EVENTS) window.removeEventListener(name, onUse, { capture: true });
		window.removeEventListener('message', onMessage);
		if (timer) clearInterval(timer);
		channel?.close();
		channel = null;
	};
}
