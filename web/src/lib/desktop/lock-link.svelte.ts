// PLAN 84-2: the automatic lock of the account (desktop preferences
// lockMinutes, lockMinutesPhone) outside the desktop's own dialog, under
// Settings › Access next to "Angemeldet bleiben" (owner 13.11.2026: "am
// besten wäre, ich könnte das in den Einstellungen einstellen"). Like the
// dock (dock-link.svelte.ts): in a window of the desktop the page asks the
// desktop and sends the change back (the desktop is the only writer of its
// settings); without a desktop (phone, own tab) it uses the store of this page.
import { frameMessage, inWindow } from './frame.ts';
import { refreshSession } from '../session.svelte.ts';
import { desktop, loadDesktop, savePrefs } from './wm.svelte.ts';

/**
 * The NAS tells the minutes that count for this login with the session
 * (lockMinutes, by the kind of device): asked again once the preferences
 * are saved (savePrefs waits 1.5 s), so a change counts at once.
 */
export function applyLockSoon() {
	setTimeout(() => void refreshSession(), 2500);
}

/** The choices, as in the desktop's dialog; 0 is never. */
export const LOCK_CHOICES = [0, 5, 10, 15, 30, 60];
export const LOCK_CHOICES_PHONE = [0, 5, 15, 30, 60, 240, 1440];

export const lockLink = $state<{ minutes: number | null; phone: number | null }>({
	minutes: null,
	phone: null,
});

const clamp = (v: unknown) =>
	typeof v === 'number' && Number.isFinite(v) ? Math.min(Math.max(Math.round(v), 0), 1440) : 0;

/** Starts following the setting; returns the function that stops it. */
export function connectLock(): () => void {
	if (!inWindow) {
		loadDesktop();
		lockLink.minutes = desktop.prefs.lockMinutes;
		lockLink.phone = desktop.prefs.lockMinutesPhone;
		return () => {};
	}
	const message = (event: MessageEvent) => {
		if (event.source !== window.parent) return;
		const msg = frameMessage(event);
		if (msg?.type === 'titanos-lock') {
			lockLink.minutes = clamp(msg.minutes);
			lockLink.phone = clamp(msg.phone);
		}
	};
	window.addEventListener('message', message);
	window.parent.postMessage({ type: 'titanos-lock-get' }, location.origin);
	return () => window.removeEventListener('message', message);
}

export function setLock(minutes: number, phone: number) {
	lockLink.minutes = minutes;
	lockLink.phone = phone;
	if (inWindow)
		window.parent.postMessage({ type: 'titanos-lock-set', minutes, phone }, location.origin);
	else {
		savePrefs((p) => {
			p.lockMinutes = clamp(minutes);
			p.lockMinutesPhone = clamp(phone);
		});
		applyLockSoon();
	}
}

/** For the desktop: the answer to a window. */
export function lockAnswer() {
	return {
		type: 'titanos-lock' as const,
		minutes: desktop.prefs.lockMinutes,
		phone: desktop.prefs.lockMinutesPhone,
	};
}
export { clamp as clampLockMinutes };
