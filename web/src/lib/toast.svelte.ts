// Short notices at the bottom of the screen (1.03, owner 16 Nov 2026): what
// failed where the page has no place for a message of its own – the
// desktop's context menu, its settings, files dropped on it. A notice
// stays a few seconds, longer while the pointer or focus is on it, and is
// never a notification of the bell.

export type ToastTone = 'info' | 'error';
export type Toast = { id: number; text: string; tone: ToastTone };

/** How long a notice stays, in milliseconds. */
export const TOAST_MS = 7000;
/** At most this many at once; the oldest goes first. */
const MAX = 3;

export const toasts = $state<{ list: Toast[] }>({ list: [] });

let next = 1;
const timers = new Map<number, ReturnType<typeof setTimeout>>();

function arm(id: number, ms: number) {
	clearTimeout(timers.get(id));
	timers.set(
		id,
		setTimeout(() => dismissToast(id), ms),
	);
}

/**
 * Shows a notice; the same text again only restarts its time (a setting
 * that fails on every change does not pile up). Returns its id.
 */
export function showToast(text: string, tone: ToastTone = 'error', ms = TOAST_MS): number {
	const same = toasts.list.find((x) => x.text === text && x.tone === tone);
	if (same) {
		arm(same.id, ms);
		return same.id;
	}
	const toast = { id: next++, text, tone };
	toasts.list = [...toasts.list, toast].slice(-MAX);
	for (const id of timers.keys())
		if (!toasts.list.some((x) => x.id === id)) {
			clearTimeout(timers.get(id));
			timers.delete(id);
		}
	arm(toast.id, ms);
	return toast.id;
}

export function dismissToast(id: number) {
	clearTimeout(timers.get(id));
	timers.delete(id);
	toasts.list = toasts.list.filter((x) => x.id !== id);
}

/** Holds the notice while it is pointed at or focused (time to read it). */
export function holdToast(id: number) {
	clearTimeout(timers.get(id));
	timers.delete(id);
}

/** Lets a held notice go again after a moment. */
export function releaseToast(id: number, ms = TOAST_MS / 2) {
	if (toasts.list.some((x) => x.id === id)) arm(id, ms);
}
