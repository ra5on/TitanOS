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
	| {
			type: 'titanos-page';
			path: string;
			title: string;
			crumbs?: { label: string; href?: string }[];
	  }
	| { type: 'titanos-goto'; href: string }
	| { type: 'titanos-open'; href: string }
	| { type: 'titanos-upload'; files: File[]; drop?: string }
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
	| { type: 'titanos-bar'; load: boolean; phoneDock?: boolean }
	/** A window switches CPU and RAM in the top bar on or off; the desktop saves it. */
	| { type: 'titanos-bar-set'; load: boolean }
	// PLAN 87: the phone's dock, switched in Settings › Appearance › Dock.
	| { type: 'titanos-phone-dock-set'; show: boolean }
	/** PLAN 84-2, Settings › Access: a window asks for the automatic lock (answer: titanos-lock). */
	| { type: 'titanos-lock-get' }
	/** The answer, and after every change on the desktop: minutes, 0 = never. */
	| { type: 'titanos-lock'; minutes: number; phone: number }
	/** A window changes the automatic lock; the desktop checks and saves it. */
	| { type: 'titanos-lock-set'; minutes: number; phone: number }
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
	/** 1.05: a page asks for a window of its own for one task (writing an e-mail, apps.ts taskPage). */
	| { type: 'titanos-open-task'; href: string }
	/** 1.13: a page asks to show a page of another program (the agent's panel opens a draft or a
	 *  mail); only own pages of a program the account has (desk-messages.ts). */
	| { type: 'titanos-open-app'; href: string }
	/** 1.05: a task window is done and closes itself (sent e-mail). */
	| { type: 'titanos-close' }
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
export function announcePage(
	path: string,
	title: string,
	crumbs: { label: string; href?: string }[] = [],
) {
	if (!inWindow) return;
	window.parent.postMessage(
		{ type: 'titanos-page', path, title, crumbs } satisfies FrameMessage,
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

/** Whether a path is a page of this UI (not an outside address). The URL
 *  parser reads a backslash as a slash ("/\evil.example" is another host)
 *  and drops tabs and line breaks ("/\t/evil.example"), so neither and no
 *  other control character is allowed (unabhängige Durchsicht 1.13, A-5). */
export function ownPath(href: string): boolean {
	if (typeof href !== 'string' || !href.startsWith('/') || href.startsWith('//')) return false;
	if (/[\\\u0000-\u001f\u007f]/.test(href)) return false;
	if (href.startsWith('/api/')) return false;
	try {
		return new URL(href, 'http://own.invalid').host === 'own.invalid';
	} catch {
		return false;
	}
}
