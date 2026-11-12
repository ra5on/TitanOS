// PLAN 61: a page of TitanOS shown in a window of the desktop runs in a
// frame of its own origin. It then shows no head line, side bar or status
// bar of its own and tells the window which page it shows.
import { embedded } from '../embedded.ts';
import type { Dock } from './prefs.ts';

/** True inside a window of the desktop (a frame of the same origin, lib/embedded.ts). */
export const inWindow: boolean = embedded;

/** The program of the window this page runs in ("app:settings"), or "" outside a window. */
export function windowApp(): string {
	if (!inWindow) return '';
	try {
		return (window.frameElement as HTMLElement | null)?.dataset.app ?? '';
	} catch {
		return '';
	}
}

export type FrameMessage =
	| { type: 'titanos-page'; path: string; title: string }
	| { type: 'titanos-goto'; href: string }
	| { type: 'titanos-open'; href: string }
	| { type: 'titanos-upload'; files: File[] }
	/** PLAN 76: the page holds unsaved changes ('edits') or running work ('busy'); '' for neither. */
	| { type: 'titanos-unsaved'; state: 'edits' | 'busy' | '' }
	/** PLAN 64, Settings › Appearance › Dock: a window asks for the dock (answer: titanos-dock). */
	| { type: 'titanos-dock-get' }
	/** The desktop's dock, sent to the windows (answer, and after every change). */
	| { type: 'titanos-dock'; dock: Dock }
	/** A window changes the dock; the desktop checks it and saves it (one writer). */
	| { type: 'titanos-prefs'; dock: Dock }
	/** PLAN 79-10, Settings › Appearance: a window asks whether the top bar shows CPU and RAM. */
	| { type: 'titanos-bar-get' }
	/** The answer, and after every change on the desktop. */
	| { type: 'titanos-bar'; load: boolean }
	/** A window switches CPU and RAM in the top bar on or off; the desktop saves it. */
	| { type: 'titanos-bar-set'; load: boolean }
	/** PLAN 79-7, NVR live view: a window asks for the account's arrangement (answer: titanos-nvr). */
	| { type: 'titanos-nvr-get' }
	/** The answer; the page checks it (nvr/layout.ts). */
	| { type: 'titanos-nvr'; prefs: unknown }
	/** The NVR's window changes the arrangement or a view option; the desktop checks and saves it. */
	| { type: 'titanos-nvr-set'; prefs: unknown }
	/** A window asks the desktop to open one of its dialogs (the wallpaper). */
	| { type: 'titanos-desk-dialog'; dialog: 'wallpaper' }
	| { type: 'titanos-console'; session: unknown }
	/** PLAN 66: the file manager in a window drags files (null: the drag ended). */
	| { type: 'titanos-file-drag'; drag: { root: string; paths: string[] } | null }
	/** PLAN 66: the file manager opens a file in a web app (app: "webapp:<id>", else its standard app). */
	| { type: 'titanos-open-file'; root: string; path: string; app?: string }
	/** PLAN 67: a page asks the desktop to open a Docker app's window ("docker:<app>"). */
	| { type: 'titanos-open-program'; key: string }
	/** PLAN 69: a page that draws the window's side bar itself asks whether it is folded. */
	| { type: 'titanos-side-get' }
	/** PLAN 69: such a page folds or unfolds the window's side bar (saved by the desktop). */
	| { type: 'titanos-side-fold' }
	/** PLAN 69: the window tells its page whether the side bar is folded (answer, and after every change). */
	| { type: 'titanos-side'; folded: boolean }
	/** PLAN 65-6: the desktop tells a window's page whether it can be seen (minimized: no). */
	| { type: 'titanos-visible'; visible: boolean }
	| {
			type: 'titanos-key';
			key: string;
			alt: boolean;
			ctrl: boolean;
			shift: boolean;
			meta: boolean;
	  };

/** Tells the window which page the frame shows now. */
export function announcePage(path: string, title: string) {
	if (!inWindow) return;
	window.parent.postMessage(
		{ type: 'titanos-page', path, title } satisfies FrameMessage,
		location.origin,
	);
}

/** Only messages of our own origin from a frame or the top window count. */
export function frameMessage(event: MessageEvent): FrameMessage | null {
	if (event.origin !== location.origin) return null;
	const data = event.data as FrameMessage | undefined;
	if (!data || typeof data !== 'object' || typeof data.type !== 'string') return null;
	if (!data.type.startsWith('titanos-')) return null;
	return data;
}

/** Whether a path is a page of this UI (not an outside address). */
export function ownPath(href: string): boolean {
	return href.startsWith('/') && !href.startsWith('//') && !href.startsWith('/api/');
}
