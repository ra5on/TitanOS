// 1.39 (owner 28.11.2026): "wenn ein Chatfenster im Agent geöffnet ist, dass
// ich nicht trotzdem noch eine Benachrichtigung bekomme … aber nur wenn halt
// auch auf dem Desktop kein Fenster … geöffnet ist." An Agent window that can
// be seen (a visible tab, a window of the desktop that is not minimized)
// tells the NAS so every 15 seconds and at once when that changes; the NAS
// sends no push to any device of the account while one does
// (backend/internal/api/personal_presence.go, 45 seconds).
import { api, apiFetch } from '#lib/api.ts';
import { pageHidden, PAGE_VISIBILITY } from '#lib/embedded.ts';
import { base } from './api.ts';

const EVERY_MS = 15_000;

function windowName(): string {
	const bytes = new Uint8Array(12);
	crypto.getRandomValues(bytes);
	return Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
}

/** Starts telling the NAS whether this window is in view; returns the stop. */
export function startPresence(): () => void {
	if (typeof document === 'undefined') return () => {};
	const window_ = windowName();
	let timer: ReturnType<typeof setTimeout> | undefined;
	let last: boolean | undefined;
	const say = (visible: boolean) => {
		last = visible;
		api.post(base + '/presence', { window: window_, visible }).catch(() => {});
	};
	const tick = () => {
		clearTimeout(timer);
		const visible = !pageHidden();
		// A hidden window says so once and then stays quiet.
		if (visible || last !== false) say(visible);
		if (visible) timer = setTimeout(tick, EVERY_MS);
	};
	// The page goes away (tab closed, app swiped away): the request still
	// goes out while the page unloads.
	const gone = () => {
		clearTimeout(timer);
		last = false;
		try {
			void apiFetch(base + '/presence', {
				method: 'POST',
				keepalive: true,
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ window: window_, visible: false }),
			}).catch(() => {});
		} catch {
			// The 45 seconds end it on the NAS anyway.
		}
	};
	document.addEventListener('visibilitychange', tick);
	document.addEventListener(PAGE_VISIBILITY, tick);
	window.addEventListener('pagehide', gone);
	window.addEventListener('pageshow', tick);
	tick();
	return () => {
		document.removeEventListener('visibilitychange', tick);
		document.removeEventListener(PAGE_VISIBILITY, tick);
		window.removeEventListener('pagehide', gone);
		window.removeEventListener('pageshow', tick);
		gone();
	};
}
