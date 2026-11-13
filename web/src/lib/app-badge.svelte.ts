// PLAN 87: the number of unread messages on TitanOS's symbol (Badging API:
// installed as an app in Chrome/Edge on Android, Windows and macOS, Safari
// on iOS 16.4+ for apps on the home screen). Elsewhere nothing happens.
import { unreadCount } from './notifications.svelte.ts';

type Badging = Navigator & {
	setAppBadge?: (count?: number) => Promise<void>;
	clearAppBadge?: () => Promise<void>;
};

/** Follows the unread count; returns the function that stops (inside an $effect). */
export function watchAppBadge(): () => void {
	const nav = navigator as Badging;
	if (typeof nav.setAppBadge !== 'function') return () => {};
	let last = -1;
	const stop = $effect.root(() => {
		$effect(() => {
			const count = unreadCount();
			if (count === last) return;
			last = count;
			const done = count > 0 ? nav.setAppBadge?.(count) : nav.clearAppBadge?.();
			void done?.catch(() => {
				// Not installed as an app, or not allowed: no number then.
			});
		});
	});
	return stop;
}
