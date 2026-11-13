// PLAN 79-7: the live view's arrangement and view options are part of the
// desktop's settings of the account (prefs.ts, `nvr`). Like the dock's
// settings (desktop/dock-link.svelte.ts): in a window the page asks the
// desktop and sends changes back (the desktop is the only writer of its
// settings); without a desktop (phone, own tab) it uses this page's store.
import { untrack } from 'svelte';
import { frameMessage, inWindow } from '../desktop/frame.ts';
import { desktop, loadDesktop, savePrefs } from '../desktop/wm.svelte.ts';
import { defaultNvrPrefs, sanitizeNvrPrefs, type NvrPrefs } from './layout.ts';

export const nvrLink = $state<{ prefs: NvrPrefs | null }>({ prefs: null });

// The desktop's answer to an earlier step must not pull back a tile that is
// being dragged or a switch that was just set.
let lastLocal = 0;

/** Starts following the settings; returns the function that stops it. */
export function connectNvrPrefs(): () => void {
	if (!inWindow) {
		// Untracked: called from an effect, it must not depend on what it loads.
		untrack(() => {
			loadDesktop();
			nvrLink.prefs = sanitizeNvrPrefs($state.snapshot(desktop.prefs.nvr));
		});
		return () => {};
	}
	const message = (event: MessageEvent) => {
		if (event.source !== window.parent) return;
		const msg = frameMessage(event);
		if (msg?.type === 'titanos-nvr' && Date.now() - lastLocal > 600)
			nvrLink.prefs = sanitizeNvrPrefs(msg.prefs);
	};
	window.addEventListener('message', message);
	window.parent.postMessage({ type: 'titanos-nvr-get' }, location.origin);
	return () => window.removeEventListener('message', message);
}

/** Changes the settings and has them saved for the account. */
export function changeNvrPrefs(change: (prefs: NvrPrefs) => void) {
	const next: NvrPrefs = $state.snapshot(nvrLink.prefs ?? defaultNvrPrefs());
	change(next);
	const clean = sanitizeNvrPrefs(next);
	nvrLink.prefs = clean;
	lastLocal = Date.now();
	if (inWindow)
		window.parent.postMessage({ type: 'titanos-nvr-set', prefs: clean }, location.origin);
	else savePrefs((p) => (p.nvr = clean));
}
