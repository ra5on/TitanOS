// Whether this page runs inside a window of the desktop mode (PLAN 61): the
// desktop shows TitanOS pages in an iframe of the same origin. Then the page
// leaves out its own navigation (the window has it). A frame of another
// origin is not "embedded": reading its location throws, which counts as no.
function framedBySameOrigin(): boolean {
	if (typeof window === 'undefined') return false;
	try {
		return window.self !== window.top && window.top?.location.origin === window.location.origin;
	} catch {
		return false;
	}
}

/** True when this page is framed by a page of the same origin (a desktop window). */
export const embedded: boolean = framedBySameOrigin();

// PLAN 65-6: a minimized window tells its page so (message "titanos-visible"
// from the desktop). The page then counts as hidden like a background tab:
// pollers (lib/poll.ts) skip their runs and catch up when it is shown again.
let parked = false;

/** Fired on the document when a window is minimized or shown again. */
export const PAGE_VISIBILITY = 'titanos-visibility';

/** Whether nobody can see this page: a hidden tab or a minimized window. */
export function pageHidden(): boolean {
	return (typeof document !== 'undefined' && document.hidden) || parked;
}

if (embedded)
	window.addEventListener('message', (event) => {
		if (event.origin !== location.origin || event.source !== window.parent) return;
		const data = event.data as { type?: unknown; visible?: unknown } | null;
		if (data?.type !== 'titanos-visible' || typeof data.visible !== 'boolean') return;
		if (parked === !data.visible) return;
		parked = !data.visible;
		// Pollers listen for this (not a fake "visibilitychange": the lock and
		// other listeners must only hear the real tab).
		document.dispatchEvent(new Event(PAGE_VISIBILITY));
	});

/**
 * The desktop's copy of data every window would otherwise ask for again
 * (PLAN 65-6): the top page publishes it, windows read it from there. Same
 * origin only (embedded); null when there is nothing fresh.
 */
export function fromDesktop<T>(name: string): T | null {
	if (!embedded) return null;
	try {
		const shared = (window.top as unknown as Record<string, unknown>)[name];
		return (shared as T) ?? null;
	} catch {
		return null;
	}
}

/** Publishes data of the top page for its windows (see fromDesktop). */
export function shareWithWindows(name: string, value: unknown) {
	if (embedded || typeof window === 'undefined') return;
	(window as unknown as Record<string, unknown>)[name] = value;
}
