// PLAN 61: the windows of the desktop and the desktop's own settings.
import { api } from '../api.ts';
import { session } from '../session.svelte.ts';
import { defaultDesktop, sanitizeDesktop, type DesktopPrefs } from './prefs.ts';

export type Snap = 'left' | 'right' | 'max' | 'tl' | 'tr' | 'bl' | 'br' | null;

export type Win = {
	id: string;
	/** "app:<id>" for a TitanOS program, "docker:<name>" for a Docker app, "vm:<name>" for a VM screen. */
	app: string;
	/** The page in the frame (TitanOS pages) or the address of the Docker app. */
	href: string;
	title: string;
	x: number;
	y: number;
	w: number;
	h: number;
	z: number;
	minimized: boolean;
	snap: Snap;
	/** Where the window grew from (the icon), for the opening effect. */
	from?: { x: number; y: number };
	/** A one-time entry of a Docker app's gate (PLAN 61-1); never stored. */
	entry?: string;
};

const KEY = 'titanos.desktop.windows';

export const wm = $state<{
	windows: Win[];
	focused: string | null;
	area: { w: number; h: number };
	/** A window being dragged towards an edge: where it would snap. */
	preview: Snap;
	switcher: boolean;
	switcherIndex: number;
	/** A window is being moved or resized (the desktop holds still meanwhile). */
	dragging: boolean;
	/** Room the dock takes at the edges of the desktop (snapped windows stay clear of it). */
	inset: { l: number; r: number; b: number };
}>({
	windows: [],
	focused: null,
	area: { w: 1280, h: 800 },
	preview: null,
	switcher: false,
	switcherIndex: 0,
	dragging: false,
	inset: { l: 0, r: 0, b: 0 },
});

export const desktop = $state<{ prefs: DesktopPrefs }>({ prefs: defaultDesktop() });

let topZ = 10;
let counter = 0;

function storageKey() {
	return KEY + '.' + (session.data?.user?.name ?? '');
}

/** Loads the account's desktop and the windows open in this browser. */
export function loadDesktop() {
	desktop.prefs = sanitizeDesktop(session.desktop);
	try {
		const recent = JSON.parse(localStorage.getItem(recentKey()) ?? 'null');
		if (Array.isArray(recent)) desktop.prefs.recent = sanitizeDesktop({ recent }).recent;
	} catch {
		// Keep what the NAS has.
	}
	try {
		const raw = JSON.parse(localStorage.getItem(storageKey()) ?? '[]');
		if (Array.isArray(raw))
			wm.windows = raw
				.filter(
					(w) =>
						typeof w?.id === 'string' && typeof w?.app === 'string' && typeof w?.href === 'string',
				)
				.slice(0, 30)
				.map((w, i) => ({
					...w,
					z: 10 + i,
					minimized: !!w.minimized,
					snap: w.snap ?? null,
					from: undefined,
				}));
	} catch {
		wm.windows = [];
	}
	topZ = 10 + wm.windows.length;
	wm.focused = [...wm.windows].sort((a, b) => b.z - a.z).find((w) => !w.minimized)?.id ?? null;
}

let saveWindowsTimer: ReturnType<typeof setTimeout> | undefined;
export function saveWindows() {
	clearTimeout(saveWindowsTimer);
	saveWindowsTimer = setTimeout(() => {
		try {
			const plain = wm.windows.map(({ from: _from, entry: _entry, ...w }) => w);
			localStorage.setItem(storageKey(), JSON.stringify(plain));
		} catch {
			// Not remembered in this browser.
		}
	}, 400);
}

let savePrefsTimer: ReturnType<typeof setTimeout> | undefined;
/** Saves the desktop's settings on the NAS, a moment after the last change. */
export function savePrefs(change?: (p: DesktopPrefs) => void) {
	if (change) change(desktop.prefs);
	// Known at once (PLAN 64): a page that loads the desktop before the
	// timer runs (the phone's home after Settings) must not read old values.
	session.desktop = $state.snapshot(desktop.prefs) as unknown as Record<string, unknown>;
	clearTimeout(savePrefsTimer);
	savePrefsTimer = setTimeout(() => {
		const value = $state.snapshot(desktop.prefs);
		session.desktop = value as unknown as Record<string, unknown>;
		api.put('/preferences/desktop', value).catch(() => {
			// Kept in this browser until the next change succeeds.
		});
	}, 1500);
}

export function focus(id: string) {
	const win = wm.windows.find((w) => w.id === id);
	if (!win) return;
	win.minimized = false;
	if (win.z !== topZ) win.z = ++topZ;
	wm.focused = id;
	saveWindows();
}

/** The size a new window gets: most of the desktop, smaller screens get all of it. */
function newGeometry(index: number) {
	const aw = wm.area.w - wm.inset.l - wm.inset.r;
	const ah = wm.area.h - wm.inset.b;
	if (wm.area.w < 1280) return { x: wm.inset.l, y: 0, w: aw, h: ah, snap: 'max' as Snap };
	const w = Math.min(1300, Math.round(aw * 0.78));
	const h = Math.min(900, Math.round(ah * 0.9));
	const offset = (index % 6) * 28;
	return {
		x: wm.inset.l + Math.max(0, Math.round((aw - w) / 2) + offset - 60),
		y: Math.max(8, Math.round((ah - h) / 2) + offset - 20),
		w,
		h,
		snap: null as Snap,
	};
}

/** Opens a program: brings its window forward, or opens a new one. */
export function openWindow(
	app: string,
	href: string,
	title: string,
	opts: { multi?: boolean; from?: { x: number; y: number }; fresh?: boolean; entry?: string } = {},
) {
	const existing = !opts.multi && !opts.fresh ? wm.windows.find((w) => w.app === app) : undefined;
	if (existing) {
		if (href && existing.href !== href && !app.startsWith('docker:'))
			navigateWindow(existing.id, href);
		focus(existing.id);
		return existing.id;
	}
	const g = newGeometry(wm.windows.length);
	const win: Win = {
		id: 'w' + Date.now().toString(36) + (counter++).toString(36),
		app,
		href,
		title,
		...g,
		z: ++topZ,
		minimized: false,
		from: opts.from,
		entry: opts.entry,
	};
	wm.windows.push(win);
	wm.focused = win.id;
	saveWindows();
	rememberRecent(app);
	return win.id;
}

// Recently used programs stay in this browser: opening a program must not write the stick.
function recentKey() {
	return 'titanos.desktop.recent.' + (session.data?.user?.name ?? '');
}
function rememberRecent(app: string) {
	desktop.prefs.recent = [app, ...desktop.prefs.recent.filter((r) => r !== app)].slice(0, 12);
	try {
		localStorage.setItem(recentKey(), JSON.stringify(desktop.prefs.recent));
	} catch {
		// Not remembered in this browser.
	}
}

export function closeWindow(id: string) {
	wm.windows = wm.windows.filter((w) => w.id !== id);
	if (wm.focused === id)
		wm.focused = [...wm.windows].sort((a, b) => b.z - a.z).find((w) => !w.minimized)?.id ?? null;
	saveWindows();
}

export function minimize(id: string) {
	const win = wm.windows.find((w) => w.id === id);
	if (!win) return;
	win.minimized = true;
	if (wm.focused === id)
		wm.focused = [...wm.windows].sort((a, b) => b.z - a.z).find((w) => !w.minimized)?.id ?? null;
	saveWindows();
}

export function toggleMax(id: string) {
	const win = wm.windows.find((w) => w.id === id);
	if (!win) return;
	win.snap = win.snap === 'max' ? null : 'max';
	focus(id);
}

/** The rectangle a snapped window takes. */
export function snapRect(snap: Snap): { x: number; y: number; w: number; h: number } | null {
	const { l, r, b } = wm.inset;
	const w = Math.max(200, wm.area.w - l - r);
	const h = Math.max(200, wm.area.h - b);
	const hw = Math.round(w / 2);
	const hh = Math.round(h / 2);
	const at = (x: number, y: number, ww: number, wh: number) => ({ x: x + l, y, w: ww, h: wh });
	switch (snap) {
		case 'max':
			return at(0, 0, w, h);
		case 'left':
			return at(0, 0, hw, h);
		case 'right':
			return at(hw, 0, w - hw, h);
		case 'tl':
			return at(0, 0, hw, hh);
		case 'tr':
			return at(hw, 0, w - hw, hh);
		case 'bl':
			return at(0, hh, hw, h - hh);
		case 'br':
			return at(hw, hh, w - hw, h - hh);
		default:
			return null;
	}
}

export function setSnap(id: string, snap: Snap) {
	const win = wm.windows.find((w) => w.id === id);
	if (!win) return;
	win.snap = snap;
	focus(id);
}

/** Where a window dragged to (x, y) of the desktop would snap. */
export function snapAt(x: number, y: number): Snap {
	const { w, h } = wm.area;
	const edge = 14;
	const corner = 90;
	if (y <= edge) return x <= corner ? 'tl' : x >= w - corner ? 'tr' : 'max';
	if (x <= edge) return y >= h - corner ? 'bl' : y <= corner ? 'tl' : 'left';
	if (x >= w - edge) return y >= h - corner ? 'br' : y <= corner ? 'tr' : 'right';
	return null;
}

/** Frames register here so the side bar can steer them. */
const frames = new Map<string, HTMLIFrameElement>();
export function registerFrame(id: string, frame: HTMLIFrameElement | null) {
	if (frame) frames.set(id, frame);
	else frames.delete(id);
}

/** Shows another page of the program in its window. */
export function navigateWindow(id: string, href: string) {
	const win = wm.windows.find((w) => w.id === id);
	if (!win) return;
	win.href = href;
	const frame = frames.get(id);
	try {
		// Inside the frame SvelteKit moves without loading the app anew.
		frame?.contentWindow?.postMessage({ type: 'titanos-goto', href }, location.origin);
	} catch {
		// The frame loads the page through its src instead.
	}
	saveWindows();
}

/** The windows in the order of the switcher: most recently used first. */
export function byRecency(): Win[] {
	return [...wm.windows].sort((a, b) => b.z - a.z);
}
