// What the desktop hears from its windows and from the browser: messages of
// the pages in windows, files dragged in from this PC, and leaving the page.
import { applyLockSoon, clampLockMinutes, lockAnswer } from './lock-link.svelte.ts';
import { setWindowDrag } from '../file-drag.ts';
import { loadWebApps } from '../webapps/webapps.svelte.ts';
import { sanitizeNvrPrefs } from '../nvr/layout.ts';
import { frameMessage } from './frame.ts';
import { sanitizeDock } from './prefs.ts';
import { findProgram, openProgram } from './programs.svelte.ts';
import { anyUnsaved, desktop, savePrefs, wm } from './wm.svelte.ts';

// PLAN 64: Settings › Appearance › Dock runs in a window. The desktop stays
// the only writer of its settings: a window asks for the dock and sends
// changes; only frames of our own windows count, and what they send is
// checked like stored settings.
function windowFrames(): HTMLIFrameElement[] {
	return [...document.querySelectorAll<HTMLIFrameElement>('iframe[data-window]')];
}
function sendDock(target: WindowProxy, dock: unknown) {
	target.postMessage({ type: 'titanos-dock', dock }, location.origin);
}

/** PLAN 79-10: open settings windows follow "CPU und RAM in der Leiste" (menu Ansicht, right click). */
export function sendBarToWindows(load: boolean) {
	for (const f of windowFrames())
		f.contentWindow?.postMessage(
			{ type: 'titanos-bar', load, phoneDock: desktop.prefs.phone.dock.show },
			location.origin,
		);
}

/** Open settings windows follow changes made on the dock itself (right click). */
export function sendDockToWindows(dock: unknown) {
	for (const f of windowFrames()) if (f.contentWindow) sendDock(f.contentWindow, dock);
}

/** PLAN 66: the file manager opens a file in a web app (one window per document). */
async function openFile(msg: { root: string; path: string; app?: string }) {
	if (typeof msg.root !== 'string' || typeof msg.path !== 'string') return;
	if (typeof msg.app !== 'string' || !msg.app.startsWith('webapp:')) return;
	if (!findProgram(msg.app)) await loadWebApps();
	void openProgram(msg.app, { doc: { root: msg.root, path: msg.path } });
}

/**
 * Listens while the desktop is shown; returns how to stop. `dockNow` gives the
 * dock's settings as plain data, `onwallpaper` opens the wallpaper dialog.
 */
export function watchWindows(dockNow: () => unknown, onwallpaper: () => void): () => void {
	function fromWindow(event: MessageEvent) {
		const msg = frameMessage(event);
		if (!msg) return;
		const source = windowFrames().find((f) => f.contentWindow === event.source)?.contentWindow;
		if (!source) return;
		if (msg.type === 'titanos-dock-get') sendDock(source, dockNow());
		else if (msg.type === 'titanos-prefs') savePrefs((p) => (p.dock = sanitizeDock(msg.dock)));
		else if (msg.type === 'titanos-bar-get')
			source.postMessage(
				{
					type: 'titanos-bar',
					load: desktop.prefs.barLoad,
					phoneDock: desktop.prefs.phone.dock.show,
				},
				location.origin,
			);
		else if (msg.type === 'titanos-bar-set') savePrefs((p) => (p.barLoad = msg.load === true));
		else if (msg.type === 'titanos-phone-dock-set') {
			savePrefs(
				(p) => (p.phone = { ...p.phone, dock: { ...p.phone.dock, show: msg.show === true } }),
			);
			sendBarToWindows(desktop.prefs.barLoad);
		} else if (msg.type === 'titanos-lock-get') source.postMessage(lockAnswer(), location.origin);
		else if (msg.type === 'titanos-lock-set') {
			savePrefs((p) => {
				p.lockMinutes = clampLockMinutes(msg.minutes);
				p.lockMinutesPhone = clampLockMinutes(msg.phone);
			});
			applyLockSoon();
		} else if (msg.type === 'titanos-nvr-get')
			source.postMessage(
				{ type: 'titanos-nvr', prefs: JSON.parse(JSON.stringify(desktop.prefs.nvr)) },
				location.origin,
			);
		else if (msg.type === 'titanos-nvr-set')
			savePrefs((p) => (p.nvr = sanitizeNvrPrefs(msg.prefs)));
		else if (msg.type === 'titanos-desk-dialog' && msg.dialog === 'wallpaper') onwallpaper();
		else if (msg.type === 'titanos-file-drag') {
			// PLAN 66: the file manager in a window drags files; web app windows take them.
			setWindowDrag(msg.drag);
			wm.fileDrag = !!msg.drag;
		} else if (msg.type === 'titanos-open-file') void openFile(msg);
		// PLAN 67: Docker › Container "In Fernansicht öffnen" opens the app's own window.
		else if (msg.type === 'titanos-open-program') {
			const key = typeof msg.key === 'string' ? msg.key : '';
			if (key.startsWith('docker:') && findProgram(key)) void openProgram(key);
		}
	}

	// PLAN 66: files dragged from this PC over the desktop: web app windows
	// show where to drop them (their frame would swallow the drag otherwise).
	let dragDepth = 0;
	function dragEnter(e: DragEvent) {
		if (!e.dataTransfer?.types.includes('Files')) return;
		dragDepth++;
		wm.fileDrag = true;
	}
	function dragLeave(e: DragEvent) {
		if (!e.dataTransfer?.types.includes('Files')) return;
		dragDepth = Math.max(0, dragDepth - 1);
		if (!dragDepth) wm.fileDrag = false;
	}
	function dragDone() {
		dragDepth = 0;
		// After the drop's own handlers (this runs first, in the capture phase).
		setTimeout(() => {
			wm.fileDrag = false;
			setWindowDrag(null);
		}, 0);
	}
	// Leaving the page with unsaved work in a web app asks first.
	function beforeUnload(e: BeforeUnloadEvent) {
		if (anyUnsaved()) e.preventDefault();
	}

	window.addEventListener('message', fromWindow);
	window.addEventListener('dragenter', dragEnter, true);
	window.addEventListener('dragleave', dragLeave, true);
	window.addEventListener('drop', dragDone, true);
	window.addEventListener('dragend', dragDone, true);
	window.addEventListener('beforeunload', beforeUnload);
	return () => {
		window.removeEventListener('dragenter', dragEnter, true);
		window.removeEventListener('dragleave', dragLeave, true);
		window.removeEventListener('drop', dragDone, true);
		window.removeEventListener('dragend', dragDone, true);
		window.removeEventListener('beforeunload', beforeUnload);
		window.removeEventListener('message', fromWindow);
	};
}

/** Files from the computer dropped on the desktop go to the file manager's upload. */
export function sendFilesToFileManager(files: File[]) {
	void openProgram('app:files');
	// The file manager takes them once it has loaded (routes/files: titanos-upload) and says so.
	let tries = 0;
	let done = false;
	const ack = (e: MessageEvent) => {
		if (e.origin === location.origin && e.data?.type === 'titanos-upload-ok') done = true;
	};
	window.addEventListener('message', ack);
	const timer = setInterval(() => {
		if (done || ++tries > 20) {
			clearInterval(timer);
			window.removeEventListener('message', ack);
			return;
		}
		const id = wm.windows.find((w) => w.app === 'app:files')?.id;
		const frame = document.querySelector<HTMLIFrameElement>(`iframe[data-window="${id}"]`);
		frame?.contentWindow?.postMessage({ type: 'titanos-upload', files }, location.origin);
	}, 700);
}
