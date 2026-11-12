// PLAN 79-10: the top bar's setting "CPU und RAM in der Leiste" outside the
// bar (Settings › Appearance). Like the dock's settings (dock-link.svelte.ts):
// in a window of the desktop the page asks the desktop and sends the change
// back (the desktop is the only writer of its settings); without a desktop
// (phone, own tab) it uses the store of this page.
import { frameMessage, inWindow } from './frame.ts';
import { desktop, loadDesktop, savePrefs } from './wm.svelte.ts';

export const barLink = $state<{ load: boolean | null }>({ load: null });

/** Starts following the setting; returns the function that stops it. */
export function connectBar(): () => void {
	if (!inWindow) {
		loadDesktop();
		barLink.load = desktop.prefs.barLoad;
		return () => {};
	}
	const message = (event: MessageEvent) => {
		if (event.source !== window.parent) return;
		const msg = frameMessage(event);
		if (msg?.type === 'titanos-bar') barLink.load = msg.load === true;
	};
	window.addEventListener('message', message);
	window.parent.postMessage({ type: 'titanos-bar-get' }, location.origin);
	return () => window.removeEventListener('message', message);
}

export function setBarLoad(on: boolean) {
	barLink.load = on;
	if (inWindow) window.parent.postMessage({ type: 'titanos-bar-set', load: on }, location.origin);
	else savePrefs((p) => (p.barLoad = on));
}
