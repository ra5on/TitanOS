// PLAN 61: what the desktop keeps per account (preference "desktop" on the
// NAS, written only when something changes). Open windows stay in this
// browser (wm.svelte.ts): they change all the time and must not wear the stick.

import { defaultNvrPrefs, sanitizeNvrPrefs, type NvrPrefs } from '../nvr/layout.ts';

export type WidgetKind =
	| 'status'
	| 'cpuram'
	| 'network'
	| 'volumes'
	| 'docker'
	| 'vms'
	| 'backups'
	| 'temperatures'
	| 'clock'
	| 'homeassistant'
	| 'tasks'
	| 'igpu'
	| 'npu'
	| 'gpu';

export const WIDGET_KINDS: WidgetKind[] = [
	'status',
	'cpuram',
	'network',
	'volumes',
	'docker',
	'vms',
	'backups',
	'temperatures',
	'clock',
	'homeassistant',
	'tasks',
	'igpu',
	'npu',
	'gpu',
];

/** PLAN 79-5: the widgets of the graphics devices; their kind is the kind of the device. */
export const GPU_WIDGET_KINDS = ['igpu', 'npu', 'gpu'] as const;
export type GpuWidgetKind = (typeof GPU_WIDGET_KINDS)[number];
export function isGpuWidget(kind: WidgetKind): kind is GpuWidgetKind {
	return (GPU_WIDGET_KINDS as readonly string[]).includes(kind);
}
/** The values such a widget can show; each can be switched off per widget. */
export const GPU_VALUES = ['usage', 'history', 'temperature', 'memory', 'power', 'clock'] as const;
export type GpuValue = (typeof GPU_VALUES)[number];

/** A widget on the desktop: position and size in grid cells of 20 px. */
export type Widget = {
	id: string;
	kind: WidgetKind;
	x: number;
	y: number;
	w: number;
	h: number;
	/** PLAN 79-5, graphics widgets: the values switched off (none: all shown). */
	hide?: GpuValue[];
	/** PLAN 79-5, graphics widgets: the PCI address of the chosen device (none: the first). */
	device?: string;
};

export type Folder = { id: string; name: string; apps: string[] };

export type SortMode = 'free' | 'name' | 'kind' | 'recent';
export type IconSize = 's' | 'm' | 'l';

export type Wallpaper =
	| { source: 'builtin'; id: string; live: boolean }
	| { source: 'custom'; version: number; live: boolean };

export type DesktopPrefs = {
	version: 1;
	/** Programs and folders on the desktop in their order ("app:docker", "docker:immich", "folder:x"). */
	icons: string[];
	/** Whether `icons` was set by the account (false: the standard set). */
	customIcons: boolean;
	folders: Folder[];
	widgets: Widget[];
	/** Whether `widgets` was set by the account. */
	customWidgets: boolean;
	sort: SortMode;
	iconSize: IconSize;
	wallpaper: Wallpaper;
	/** Minutes without use until the desktop locks itself; 0 = never. */
	lockMinutes: number;
	/** The automatic lock of a login from a phone (PLAN 69), 0 = never (default). */
	lockMinutesPhone: number;
	/** Programs whose side bar is folded. */
	folded: string[];
	/** Saved window arrangements. */
	layouts: Layout[];
	/** Recently opened programs, newest first (for "Zuletzt"). */
	recent: string[];
	/** Places of icons on the desktop's grid (sort "free"): column and row. */
	iconPos: Record<string, { c: number; r: number }>;
	/** All widgets hidden (they stay as they are and come back as they were). */
	widgetsHidden: boolean;
	dock: Dock;
	/** Size of the two bars at the top. */
	barSize: IconSize;
	/** PLAN 79-10: CPU and RAM in the top bar (on unless switched off). */
	barLoad: boolean;
	/** PLAN 79-11: the account's own order of the programs in the menu; empty = A–Z. */
	menuOrder: string[];
	/** PLAN 79-7: the live view of the NVR – the account's arrangement of the cameras and what it shows. */
	nvr: NvrPrefs;
};

export type DockPosition = 'bottom' | 'left' | 'right';
export type Dock = {
	show: boolean;
	/** Programs kept in the dock, in their order. */
	pinned: string[];
	/** Whether `pinned` was set by the account (false: the standard set). */
	customPinned: boolean;
	/** Size of the icons in px. */
	size: number;
	/** Opacity of the dock's plate in percent. */
	opacity: number;
	/** Growing under the pointer: 0 off, 1 light, 2 strong. */
	magnify: 0 | 1 | 2;
	position: DockPosition;
	/** PLAN 73: the plate with its frame (true) or the icons alone (false). */
	border: boolean;
};
export const DOCK_DEFAULT_PINNED = [
	'app:files',
	'app:docker',
	'app:vms',
	'app:agent',
	'app:monitor',
	'app:settings',
];
export const defaultDock = (): Dock => ({
	show: true,
	pinned: [],
	customPinned: false,
	size: 52,
	opacity: 90,
	magnify: 1,
	position: 'bottom',
	border: true,
});

export type LayoutWindow = {
	app: string;
	href: string;
	x: number;
	y: number;
	w: number;
	h: number;
};
export type Layout = { id: string; name: string; windows: LayoutWindow[] };

export const WALLPAPERS = [
	'wave',
	'aurora',
	'dunes',
	'peaks',
	'grid',
	'calm',
	// PLAN 74: the photos of the login page; nordlicht and bergsee move (lib/live).
	'nordlicht',
	'bergsee',
	'kueste',
	'nebelwald',
	'wueste',
	'weltraum',
] as const;

const ID = /^[a-z0-9][a-z0-9:._-]{0,95}$/i;

export const defaultWidgets = (): Widget[] => [
	{ id: 'w-status', kind: 'status', x: -17, y: 0, w: 17, h: 9 },
	{ id: 'w-cpuram', kind: 'cpuram', x: -17, y: 9, w: 17, h: 6 },
	{ id: 'w-volumes', kind: 'volumes', x: -17, y: 15, w: 17, h: 9 },
	{ id: 'w-docker', kind: 'docker', x: -17, y: 24, w: 17, h: 12 },
];

export const defaultDesktop = (): DesktopPrefs => ({
	version: 1,
	icons: [],
	customIcons: false,
	folders: [],
	widgets: defaultWidgets(),
	customWidgets: false,
	sort: 'kind',
	iconSize: 'm',
	wallpaper: { source: 'builtin', id: 'wave', live: true },
	lockMinutes: 15,
	lockMinutesPhone: 0,
	folded: [],
	layouts: [],
	recent: [],
	iconPos: {},
	widgetsHidden: false,
	dock: defaultDock(),
	barSize: 'm',
	barLoad: true,
	menuOrder: [],
	nvr: defaultNvrPrefs(),
});

function strings(value: unknown, max = 200): string[] {
	if (!Array.isArray(value)) return [];
	return [...new Set(value.filter((v): v is string => typeof v === 'string' && ID.test(v)))].slice(
		0,
		max,
	);
}

function int(value: unknown, min: number, max: number, fallback: number): number {
	return typeof value === 'number' && Number.isFinite(value)
		? Math.min(max, Math.max(min, Math.round(value)))
		: fallback;
}

function text(value: unknown, max = 60): string {
	return typeof value === 'string'
		? value
				.replace(/[\x00-\x1f\x7f]/g, '')
				.trim()
				.slice(0, max)
		: '';
}

/** Reads what the NAS sent; anything unknown or broken falls back to the standard. */
export function sanitizeDesktop(input: unknown): DesktopPrefs {
	const base = defaultDesktop();
	if (typeof input !== 'object' || input === null) return base;
	const v = input as Record<string, unknown>;
	const widgets = Array.isArray(v.widgets)
		? v.widgets
				.filter((w): w is Record<string, unknown> => typeof w === 'object' && w !== null)
				.filter(
					(w) =>
						WIDGET_KINDS.includes(w.kind as WidgetKind) &&
						typeof w.id === 'string' &&
						ID.test(w.id),
				)
				.map((w) => {
					const widget: Widget = {
						id: w.id as string,
						kind: w.kind as WidgetKind,
						x: int(w.x, -200, 200, 0),
						y: int(w.y, 0, 200, 0),
						w: int(w.w, 6, 60, 12),
						h: int(w.h, 3, 60, 6),
					};
					if (isGpuWidget(widget.kind)) {
						const hide = Array.isArray(w.hide)
							? GPU_VALUES.filter((value) => (w.hide as unknown[]).includes(value))
							: [];
						if (hide.length) widget.hide = hide;
						const device = text(w.device, 40);
						if (/^[0-9a-f:.]+$/i.test(device)) widget.device = device;
					}
					return widget;
				})
				.slice(0, 40)
		: base.widgets;
	const folders = Array.isArray(v.folders)
		? v.folders
				.filter(
					(f): f is Record<string, unknown> =>
						typeof f === 'object' && f !== null && typeof f.id === 'string' && ID.test(f.id),
				)
				.map((f) => ({
					id: f.id as string,
					name: text(f.name) || 'Ordner',
					apps: strings(f.apps, 40),
				}))
				.slice(0, 40)
		: [];
	const wp = (typeof v.wallpaper === 'object' && v.wallpaper ? v.wallpaper : {}) as Record<
		string,
		unknown
	>;
	const live = wp.live !== false;
	const wallpaper: Wallpaper =
		wp.source === 'custom'
			? { source: 'custom', version: int(wp.version, 0, 1e12, 0), live }
			: {
					source: 'builtin',
					id: WALLPAPERS.includes(wp.id as (typeof WALLPAPERS)[number])
						? (wp.id as string)
						: 'wave',
					live,
				};
	const layouts = Array.isArray(v.layouts)
		? v.layouts
				.filter(
					(l): l is Record<string, unknown> =>
						typeof l === 'object' && l !== null && typeof l.id === 'string' && ID.test(l.id),
				)
				.map((l) => ({
					id: l.id as string,
					name: text(l.name) || 'Anordnung',
					windows: (Array.isArray(l.windows) ? l.windows : [])
						.filter((w): w is Record<string, unknown> => typeof w === 'object' && w !== null)
						.filter(
							(w) =>
								typeof w.app === 'string' &&
								ID.test(w.app) &&
								typeof w.href === 'string' &&
								(w.href as string).startsWith('/') &&
								!(w.href as string).startsWith('//'),
						)
						.map((w) => ({
							app: w.app as string,
							href: (w.href as string).slice(0, 500),
							x: int(w.x, -5000, 10000, 40),
							y: int(w.y, 0, 10000, 40),
							w: int(w.w, 320, 10000, 900),
							h: int(w.h, 220, 10000, 640),
						}))
						.slice(0, 20),
				}))
				.slice(0, 10)
		: [];
	return {
		version: 1,
		icons: strings(v.icons),
		customIcons: v.customIcons === true,
		folders,
		widgets,
		customWidgets: v.customWidgets === true,
		sort: (['free', 'name', 'kind', 'recent'] as SortMode[]).includes(v.sort as SortMode)
			? (v.sort as SortMode)
			: base.sort,
		iconSize: (['s', 'm', 'l'] as IconSize[]).includes(v.iconSize as IconSize)
			? (v.iconSize as IconSize)
			: base.iconSize,
		wallpaper,
		lockMinutes: int(v.lockMinutes, 0, 1440, base.lockMinutes),
		lockMinutesPhone: int(v.lockMinutesPhone, 0, 1440, base.lockMinutesPhone),
		folded: strings(v.folded, 60),
		layouts,
		recent: strings(v.recent, 12),
		iconPos: positions(v.iconPos),
		widgetsHidden: v.widgetsHidden === true,
		dock: sanitizeDock(v.dock),
		barSize: (['s', 'm', 'l'] as IconSize[]).includes(v.barSize as IconSize)
			? (v.barSize as IconSize)
			: 'm',
		barLoad: v.barLoad !== false,
		menuOrder: strings(v.menuOrder, 300),
		nvr: sanitizeNvrPrefs(v.nvr),
	};
}

function positions(value: unknown): Record<string, { c: number; r: number }> {
	const out: Record<string, { c: number; r: number }> = {};
	if (typeof value !== 'object' || value === null || Array.isArray(value)) return out;
	for (const [key, at] of Object.entries(value).slice(0, 300)) {
		if (!ID.test(key) || typeof at !== 'object' || at === null) continue;
		const p = at as Record<string, unknown>;
		out[key] = { c: int(p.c, 0, 200, 0), r: int(p.r, 0, 200, 0) };
	}
	return out;
}

export function sanitizeDock(value: unknown): Dock {
	const base = defaultDock();
	if (typeof value !== 'object' || value === null) return base;
	const d = value as Record<string, unknown>;
	return {
		show: d.show !== false,
		pinned: strings(d.pinned, 40),
		customPinned: d.customPinned === true,
		size: int(d.size, 36, 80, base.size),
		opacity: int(d.opacity, 30, 100, base.opacity),
		magnify: int(d.magnify, 0, 2, base.magnify) as 0 | 1 | 2,
		position: (['bottom', 'left', 'right'] as DockPosition[]).includes(d.position as DockPosition)
			? (d.position as DockPosition)
			: base.position,
		border: d.border !== false,
	};
}

/**
 * Where each icon sits on the grid. Sorted modes fill the columns from the
 * top left; "free" keeps the account's places and puts new icons in the
 * first free cell.
 *
 * The stored places never change here (PLAN 65-10): an icon whose place lies
 * outside a smaller window is shown at the nearest free cell inside, and
 * comes back to its own place once the window is large enough again. Icons
 * whose place fits always win over such stand-ins.
 */
export function placeIcons(
	keys: string[],
	prefs: Pick<DesktopPrefs, 'sort' | 'iconPos'>,
	rows: number,
	columns: number,
): Map<string, { c: number; r: number }> {
	const out = new Map<string, { c: number; r: number }>();
	const rowsN = Math.max(1, rows);
	const colsN = Math.max(1, columns);
	const taken = new Set<string>();
	const cell = (c: number, r: number) => c + ':' + r;
	const take = (key: string, c: number, r: number) => {
		taken.add(cell(c, r));
		out.set(key, { c, r });
	};
	if (prefs.sort === 'free') {
		const outside: string[] = [];
		for (const key of keys) {
			const at = prefs.iconPos[key];
			if (!at) continue;
			if (at.c >= colsN || at.r >= rowsN) outside.push(key);
			// The same cell twice: the later icon goes to the first free cell.
			else if (!taken.has(cell(at.c, at.r))) take(key, at.c, at.r);
		}
		for (const key of outside) {
			const at = prefs.iconPos[key];
			const c0 = Math.min(at.c, colsN - 1);
			const r0 = Math.min(at.r, rowsN - 1);
			let best: { c: number; r: number } | null = null;
			let bestD = Infinity;
			for (let c = 0; c < colsN; c++)
				for (let r = 0; r < rowsN; r++) {
					if (taken.has(cell(c, r))) continue;
					// Ties stay in the same column (the icons stack like before).
					const d = (c - c0) ** 2 + (r - r0) ** 2 + Math.abs(c - c0) / 100;
					if (d < bestD) {
						bestD = d;
						best = { c, r };
					}
				}
			if (best) take(key, best.c, best.r);
		}
	}
	let next = 0;
	for (const key of keys) {
		if (out.has(key)) continue;
		while (taken.has(cell(Math.floor(next / rowsN), next % rowsN))) next++;
		take(key, Math.floor(next / rowsN), next % rowsN);
	}
	return out;
}

/**
 * The places to store after `key` was dropped on `to` (PLAN 65-10). Only the
 * moved icon and, when it lands on another icon, that one change; every other
 * stored place stays as it is, even when a smaller window shows it elsewhere.
 * `shown` are the places on screen now; when the desktop was sorted until now,
 * all shown places become the stored ones first, so nothing jumps.
 */
export function dropIcon(
	prefs: Pick<DesktopPrefs, 'sort' | 'iconPos'>,
	shown: Map<string, { c: number; r: number }>,
	key: string,
	to: { c: number; r: number },
	other?: string,
): Record<string, { c: number; r: number }> {
	const next: Record<string, { c: number; r: number }> = {};
	if (prefs.sort === 'free')
		for (const [k, at] of Object.entries(prefs.iconPos)) next[k] = { ...at };
	else for (const [k, at] of shown) next[k] = { ...at };
	const from = shown.get(key);
	if (other && other !== key) {
		if (from) next[other] = { ...from };
		else delete next[other];
	}
	next[key] = { ...to };
	return next;
}

/** The programs in the dock: the account's own, or the standard set of those it may use. */
export function dockPinned(prefs: DesktopPrefs, known: Set<string>): string[] {
	const list = prefs.dock.customPinned ? prefs.dock.pinned : DOCK_DEFAULT_PINNED;
	return list.filter((k) => known.has(k));
}

/** The icons shown, in order: the account's own list, or every program. */
export function arrangeIcons(
	prefs: DesktopPrefs,
	available: { key: string; label: string; kind: string }[],
): string[] {
	const known = new Set(available.map((a) => a.key));
	const folders = new Set(prefs.folders.map((f) => 'folder:' + f.id));
	const inFolders = new Set(prefs.folders.flatMap((f) => f.apps));
	let keys = prefs.customIcons
		? prefs.icons.filter((k) => known.has(k) || folders.has(k))
		: available.map((a) => a.key).filter((k) => !inFolders.has(k));
	if (!prefs.customIcons) keys = [...keys, ...[...folders].filter((f) => !keys.includes(f))];
	const label = new Map(available.map((a) => [a.key, a.label]));
	const kind = new Map(available.map((a) => [a.key, a.kind]));
	const name = (k: string) =>
		k.startsWith('folder:')
			? (prefs.folders.find((f) => 'folder:' + f.id === k)?.name ?? '')
			: (label.get(k) ?? k);
	if (prefs.sort === 'name') keys = [...keys].sort((a, b) => name(a).localeCompare(name(b)));
	else if (prefs.sort === 'kind') {
		const rank = (k: string) => (k.startsWith('folder:') ? 2 : kind.get(k) === 'system' ? 0 : 1);
		keys = [...keys].sort((a, b) => rank(a) - rank(b) || keys.indexOf(a) - keys.indexOf(b));
	} else if (prefs.sort === 'recent') {
		const at = (k: string) => {
			const i = prefs.recent.indexOf(k);
			return i < 0 ? 999 : i;
		};
		keys = [...keys].sort((a, b) => at(a) - at(b));
	}
	return keys;
}

/** Puts a widget where it does not cover another one (first free row). */
export function placeWidget(
	widgets: Widget[],
	w: number,
	h: number,
	columns: number,
	minX = 0,
): { x: number; y: number } {
	const overlaps = (x: number, y: number) =>
		widgets.some((o) => {
			const ox = o.x < 0 ? columns + o.x : o.x;
			return x < ox + o.w && ox < x + w && y < o.y + o.h && o.y < y + h;
		});
	for (let y = 0; y < 200; y++)
		for (let x = Math.min(minX, Math.max(0, columns - w)); x + w <= columns; x++)
			if (!overlaps(x, y)) return { x, y };
	return { x: Math.min(minX, Math.max(0, columns - w)), y: 0 };
}

/** Moves the entry at `from` to the place `to` (the dock's list in the settings). */
export function moveItem<T>(list: T[], from: number, to: number): T[] {
	if (from < 0 || from >= list.length) return list;
	const out = [...list];
	const [item] = out.splice(from, 1);
	out.splice(Math.max(0, Math.min(out.length, to)), 0, item);
	return out;
}

/**
 * PLAN 79-11: the programs of the menu in their order. Without an own order
 * they are sorted A–Z by name; with one, the stored order counts and programs
 * it does not know yet (a new app) follow A–Z.
 */
export function menuSorted<T extends { key: string; label: string }>(
	programs: T[],
	order: string[],
	locale?: string,
): T[] {
	const byName = [...programs].sort(
		(a, b) =>
			a.label.localeCompare(b.label, locale, { sensitivity: 'base', numeric: true }) ||
			a.key.localeCompare(b.key),
	);
	if (!order.length) return byName;
	const at = new Map(order.map((key, i) => [key, i]));
	const known = byName.filter((p) => at.has(p.key));
	known.sort((a, b) => (at.get(a.key) ?? 0) - (at.get(b.key) ?? 0));
	return [...known, ...byName.filter((p) => !at.has(p.key))];
}

/**
 * The order to store after `key` was put in front of `before` (or at the end
 * when `before` is null). `shown` is the whole menu in its order now, so the
 * first move out of A–Z keeps every other program where it is.
 */
export function menuMove(shown: string[], key: string, before: string | null): string[] {
	if (key === before || !shown.includes(key)) return shown;
	const out = shown.filter((k) => k !== key);
	const at = before === null ? -1 : out.indexOf(before);
	if (at < 0) out.push(key);
	else out.splice(at, 0, key);
	return out;
}
