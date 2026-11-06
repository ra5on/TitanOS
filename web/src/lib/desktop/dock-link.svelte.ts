// PLAN 64: the dock's settings outside the dock (Settings › Appearance ›
// Dock). In a window of the desktop the page asks the desktop for the dock
// and sends changes back (the desktop is the only writer, lib/desktop/frame.ts);
// without a desktop (phone, own tab) it uses the store of this page.
import { frameMessage, inWindow } from './frame.ts';
import { defaultDock, dockPinned, sanitizeDock, type Dock } from './prefs.ts';
export { moveItem } from './prefs.ts';
import { desktop, loadDesktop, savePrefs } from './wm.svelte.ts';

export const dockLink = $state<{ dock: Dock | null }>({ dock: null });
// While a slider is dragged the desktop's answers to earlier steps must not pull it back.
// Once the dragging stops, the desktop's state is asked for again.
let lastLocal = 0;
let resync: ReturnType<typeof setTimeout> | undefined;

/** Starts following the dock; returns the function that stops it. */
export function connectDock(): () => void {
	if (!inWindow) {
		loadDesktop();
		dockLink.dock = $state.snapshot(desktop.prefs.dock);
		return () => {};
	}
	const message = (event: MessageEvent) => {
		if (event.source !== window.parent) return;
		const msg = frameMessage(event);
		if (msg?.type === 'titanos-dock' && Date.now() - lastLocal > 600)
			dockLink.dock = sanitizeDock(msg.dock);
	};
	window.addEventListener('message', message);
	window.parent.postMessage({ type: 'titanos-dock-get' }, location.origin);
	return () => window.removeEventListener('message', message);
}

/**
 * Changes the dock. With `known` (the programs of the account) the change
 * concerns the kept programs: the standard set becomes the account's own
 * list first, so that moving or removing one keeps the others.
 */
export function changeDock(change: (dock: Dock) => void, known?: Set<string>) {
	const current = dockLink.dock ?? defaultDock();
	const next: Dock = $state.snapshot(current);
	if (known) {
		next.pinned = dockPinned({ ...desktop.prefs, dock: current }, known);
		next.customPinned = true;
	}
	change(next);
	const clean = sanitizeDock(next);
	dockLink.dock = clean;
	lastLocal = Date.now();
	if (inWindow) {
		window.parent.postMessage({ type: 'titanos-prefs', dock: clean }, location.origin);
		clearTimeout(resync);
		resync = setTimeout(
			() => window.parent.postMessage({ type: 'titanos-dock-get' }, location.origin),
			700,
		);
	} else savePrefs((p) => (p.dock = clean));
}
