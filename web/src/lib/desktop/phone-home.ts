// PLAN 87: the phone's home screen, kept per account in the preference
// "desktop" under `phone` (optional: 0.80 ignores it; a save from 0.80 – a
// rollback, an old tab – leaves it out, the phone then starts from the
// programs again). The phone has its own pages, folders, widgets and dock,
// apart from the desktop's icons, so arranging one never moves the other.

import {
	GPU_VALUES,
	WIDGET_KINDS,
	isGpuWidget,
	type Folder,
	type GpuValue,
	type WidgetKind,
} from './prefs.ts';

/** A widget on the phone: 'l' fills the row (4 × 2 cells), 's' half of it (2 × 2). */
export type PhoneWidget = {
	id: string;
	kind: WidgetKind;
	size: 's' | 'l';
	hide?: GpuValue[];
	device?: string;
	camera?: string;
};

export type PhoneDock = {
	show: boolean;
	/** The programs in the dock, in their order. */
	apps: string[];
	/** Whether `apps` was set by the account (false: the standard set). */
	custom: boolean;
};

export type PhonePrefs = {
	/** Whether the account arranged the home screen (false: built from the programs). */
	custom: boolean;
	/** The pages, each a list of programs ("app:files"), folders ("folder:x") and widgets ("widget:x") in order. */
	pages: string[][];
	folders: Folder[];
	widgets: PhoneWidget[];
	dock: PhoneDock;
};

/** Columns of the home screen. */
export const PHONE_COLUMNS = 4;
/** How many programs the dock holds. */
export const PHONE_DOCK_MAX = 4;
export const PHONE_DOCK_DEFAULT = ['app:files', 'app:docker', 'app:monitor', 'app:settings'];

export const defaultPhone = (): PhonePrefs => ({
	custom: false,
	pages: [],
	folders: [],
	widgets: [{ id: 'p-status', kind: 'status', size: 'l' }],
	dock: { show: true, apps: [], custom: false },
});

const ID = /^[a-z0-9][a-z0-9:._-]{0,95}$/i;

function keys(value: unknown, max: number): string[] {
	if (!Array.isArray(value)) return [];
	return [...new Set(value.filter((v): v is string => typeof v === 'string' && ID.test(v)))].slice(
		0,
		max,
	);
}

function name(value: unknown): string {
	return typeof value === 'string'
		? value
				.replace(/[\x00-\x1f\x7f]/g, '')
				.trim()
				.slice(0, 60)
		: '';
}

/** Reads what the NAS sent; anything unknown or broken falls back to the standard. */
export function sanitizePhone(input: unknown): PhonePrefs {
	const base = defaultPhone();
	if (typeof input !== 'object' || input === null) return base;
	const v = input as Record<string, unknown>;
	const widgets = Array.isArray(v.widgets)
		? v.widgets
				.filter((w): w is Record<string, unknown> => typeof w === 'object' && w !== null)
				.filter(
					(w) =>
						typeof w.id === 'string' &&
						ID.test(w.id) &&
						WIDGET_KINDS.includes(w.kind as WidgetKind),
				)
				.map((w) => {
					const widget: PhoneWidget = {
						id: w.id as string,
						kind: w.kind as WidgetKind,
						size: w.size === 's' ? 's' : 'l',
					};
					if (isGpuWidget(widget.kind)) {
						const hide = Array.isArray(w.hide)
							? GPU_VALUES.filter((x) => (w.hide as unknown[]).includes(x))
							: [];
						if (hide.length) widget.hide = hide;
						if (typeof w.device === 'string' && /^[0-9a-f:.]{1,40}$/i.test(w.device))
							widget.device = w.device;
					}
					if (
						widget.kind === 'camera' &&
						typeof w.camera === 'string' &&
						/^[a-z][a-z0-9_]{0,31}$/.test(w.camera)
					)
						widget.camera = w.camera;
					return widget;
				})
				.slice(0, 30)
		: base.widgets;
	const folders = Array.isArray(v.folders)
		? v.folders
				.filter(
					(f): f is Record<string, unknown> =>
						typeof f === 'object' && f !== null && typeof f.id === 'string' && ID.test(f.id),
				)
				.map((f) => ({
					id: f.id as string,
					name: name(f.name) || 'Ordner',
					apps: keys(f.apps, 40),
				}))
				.slice(0, 40)
		: [];
	const pages = Array.isArray(v.pages) ? v.pages.slice(0, 12).map((p) => keys(p, 80)) : [];
	const d = (typeof v.dock === 'object' && v.dock ? v.dock : {}) as Record<string, unknown>;
	return {
		custom: v.custom === true,
		pages,
		folders,
		widgets,
		dock: {
			show: d.show !== false,
			apps: keys(d.apps, PHONE_DOCK_MAX),
			custom: d.custom === true,
		},
	};
}

/** Cells an entry takes on its page. */
export function cellsOf(key: string, prefs: PhonePrefs): number {
	if (!key.startsWith('widget:')) return 1;
	const w = prefs.widgets.find((x) => 'widget:' + x.id === key);
	return w?.size === 's' ? 4 : 8;
}

/** Splits a list into pages of at most `cells` cells (a page holds at least one entry). */
function paginate(list: string[], prefs: PhonePrefs, cells: number): string[][] {
	const pages: string[][] = [];
	let page: string[] = [];
	let used = 0;
	for (const key of list) {
		const c = cellsOf(key, prefs);
		if (page.length && used + c > cells) {
			pages.push(page);
			page = [];
			used = 0;
		}
		page.push(key);
		used += c;
	}
	if (page.length) pages.push(page);
	return pages;
}

export type PhoneShown = {
	pages: string[][];
	dock: string[];
	folders: Folder[];
};

/**
 * The home screen as shown. `programs` are the keys of the programs the
 * account may use, in the order of the standard home screen; `cells` the
 * cells of a page. Programs the arrangement does not know yet (a new app)
 * come at the end; programs no longer there drop out; every program shows
 * once. With the dock switched off its programs come at the end of the pages.
 */
export function phoneLayout(prefs: PhonePrefs, programs: string[], cells: number): PhoneShown {
	const known = new Set(programs);
	const dockAll = (prefs.dock.custom ? prefs.dock.apps : PHONE_DOCK_DEFAULT)
		.filter((k) => known.has(k))
		.slice(0, PHONE_DOCK_MAX);
	const dock = prefs.dock.show ? dockAll : [];
	const seen = new Set<string>(dock);
	const folders: Folder[] = [];
	for (const f of prefs.folders) {
		const apps = f.apps.filter((k) => known.has(k) && !seen.has(k));
		apps.forEach((k) => seen.add(k));
		if (apps.length) folders.push({ ...f, apps });
	}
	const folderKeys = new Set(folders.map((f) => 'folder:' + f.id));
	const widgetKeys = new Set(prefs.widgets.map((w) => 'widget:' + w.id));
	const usable = (k: string) =>
		!seen.has(k) && (known.has(k) || folderKeys.has(k) || widgetKeys.has(k));
	let pages: string[][];
	if (prefs.custom) {
		pages = prefs.pages.map((page) =>
			page.filter((k) => {
				if (!usable(k)) return false;
				seen.add(k);
				return true;
			}),
		);
	} else {
		const list = [
			...prefs.widgets.map((w) => 'widget:' + w.id),
			...programs.filter((k) => !seen.has(k)),
			...folders.map((f) => 'folder:' + f.id),
		].filter((k) => {
			if (!usable(k)) return false;
			seen.add(k);
			return true;
		});
		pages = paginate(list, prefs, cells);
	}
	// What the arrangement does not hold: new programs, folders and widgets, and the dock's when it is off.
	const rest = [
		...programs.filter((k) => !seen.has(k) && !dockAll.includes(k)),
		...(prefs.dock.show ? [] : dockAll.filter((k) => !seen.has(k))),
		...[...folderKeys, ...widgetKeys].filter((k) => !seen.has(k)),
	];
	rest.forEach((k) => seen.add(k));
	pages = pages.filter((p) => p.length);
	if (rest.length) {
		const last = pages.pop() ?? [];
		pages.push(...paginate([...last, ...rest], prefs, cells));
	}
	pages = spill(pages, prefs, cells);
	if (!pages.length) pages = [[]];
	return { pages, dock, folders };
}

/**
 * A page holding more than fits (a smaller screen, a widget added) shows
 * the rest at the start of the next page; nothing is ever cut off.
 */
function spill(pages: string[][], prefs: PhonePrefs, cells: number): string[][] {
	const out: string[][] = [];
	let carry: string[] = [];
	for (const page of pages) {
		const fitted = paginate([...carry, ...page], prefs, cells);
		out.push(fitted[0] ?? []);
		carry = fitted.slice(1).flat();
	}
	if (carry.length) out.push(...paginate(carry, prefs, cells));
	return out.filter((p) => p.length);
}

/** The arrangement as shown becomes the account's own (the first change on the phone). */
export function ownLayout(prefs: PhonePrefs, shown: PhoneShown): PhonePrefs {
	return {
		...prefs,
		custom: true,
		pages: shown.pages.map((p) => [...p]),
		folders: shown.folders.map((f) => ({ ...f, apps: [...f.apps] })),
		dock: {
			...prefs.dock,
			custom: true,
			apps: prefs.dock.show
				? [...shown.dock]
				: (prefs.dock.custom ? prefs.dock.apps : PHONE_DOCK_DEFAULT).filter((k) =>
						shown.pages.some((p) => p.includes(k)),
					),
		},
	};
}

/** Takes an entry out of wherever it is (page, dock, folder); a folder left with one program dissolves. */
function takeOut(p: PhonePrefs, key: string): PhonePrefs {
	let next: PhonePrefs = {
		...p,
		pages: p.pages.map((page) => page.filter((k) => k !== key)),
		dock: { ...p.dock, apps: p.dock.apps.filter((k) => k !== key) },
		folders: p.folders.map((f) => ({ ...f, apps: f.apps.filter((k) => k !== key) })),
	};
	for (const f of next.folders) if (f.apps.length <= 1) next = dissolveFolder(next, f.id);
	return next;
}

/** Puts `key` on page `page` at `index` (the end if the index is past it). */
export function moveTo(p: PhonePrefs, key: string, page: number, index: number): PhonePrefs {
	const next = takeOut(p, key);
	const pages = next.pages.map((x) => [...x]);
	while (pages.length <= page) pages.push([]);
	const target = pages[page];
	target.splice(Math.max(0, Math.min(target.length, index)), 0, key);
	return { ...next, pages };
}

/** Puts a program into the dock at `index`; refused (unchanged) when the dock is full or it is no program. */
export function moveToDock(p: PhonePrefs, key: string, index: number): PhonePrefs {
	if (key.startsWith('folder:') || key.startsWith('widget:')) return p;
	if (!p.dock.apps.includes(key) && p.dock.apps.length >= PHONE_DOCK_MAX) return p;
	const next = takeOut(p, key);
	const apps = [...next.dock.apps];
	apps.splice(Math.max(0, Math.min(apps.length, index)), 0, key);
	return { ...next, dock: { ...next.dock, apps, custom: true } };
}

/**
 * A program dropped on another program makes a folder in the other's
 * place; dropped on a folder it goes into the folder. Folders and widgets
 * are not put into folders.
 */
export function combine(
	p: PhonePrefs,
	dragged: string,
	target: string,
	folderName: string,
	newId: string,
): PhonePrefs {
	if (dragged === target || dragged.startsWith('folder:') || dragged.startsWith('widget:'))
		return p;
	if (target.startsWith('widget:')) return p;
	if (target.startsWith('folder:')) {
		const id = target.slice(7);
		const next = takeOut(p, dragged);
		return {
			...next,
			folders: next.folders.map((f) =>
				f.id === id && !f.apps.includes(dragged) ? { ...f, apps: [...f.apps, dragged] } : f,
			),
		};
	}
	const next = takeOut(p, dragged);
	const folder: Folder = { id: newId, name: folderName, apps: [target, dragged] };
	const replace = (list: string[]) => list.map((k) => (k === target ? 'folder:' + newId : k));
	return {
		...next,
		pages: next.pages.map(replace),
		dock: { ...next.dock, apps: next.dock.apps.filter((k) => k !== target) },
		folders: [...next.folders, folder],
		// A program from the dock makes its folder at the start of the first page.
		...(next.dock.apps.includes(target) ? dockFolder(next, target, newId) : {}),
	};
}

function dockFolder(p: PhonePrefs, target: string, newId: string): Partial<PhonePrefs> {
	const pages = p.pages.map((x) => [...x]);
	if (!pages.length) pages.push([]);
	pages[0].unshift('folder:' + newId);
	return { pages, dock: { ...p.dock, apps: p.dock.apps.filter((k) => k !== target) } };
}

/** A program taken out of its folder goes right after the folder. */
export function outOfFolder(p: PhonePrefs, key: string, folderId: string): PhonePrefs {
	const at = p.pages.findIndex((page) => page.includes('folder:' + folderId));
	const page = Math.max(0, at);
	const index = at >= 0 ? p.pages[at].indexOf('folder:' + folderId) + 1 : 0;
	const next = takeOut(p, key);
	// The folder may have dissolved: its last program stands where it stood.
	const pages = next.pages.map((x) => [...x]);
	while (pages.length <= page) pages.push([]);
	const after = pages[page].indexOf('folder:' + folderId);
	pages[page].splice(after >= 0 ? after + 1 : Math.min(index, pages[page].length), 0, key);
	return { ...next, pages };
}

/** The folder goes; its programs stand in its place. */
export function dissolveFolder(p: PhonePrefs, folderId: string): PhonePrefs {
	const folder = p.folders.find((f) => f.id === folderId);
	if (!folder) return p;
	const key = 'folder:' + folderId;
	let placed = false;
	const pages = p.pages.map((page) =>
		page.flatMap((k) => {
			if (k !== key) return [k];
			placed = true;
			return folder.apps;
		}),
	);
	if (!placed && folder.apps.length) {
		if (!pages.length) pages.push([]);
		pages[pages.length - 1].push(...folder.apps);
	}
	return { ...p, pages, folders: p.folders.filter((f) => f.id !== folderId) };
}

export function renameFolder(p: PhonePrefs, folderId: string, to: string): PhonePrefs {
	const clean = name(to);
	if (!clean) return p;
	return { ...p, folders: p.folders.map((f) => (f.id === folderId ? { ...f, name: clean } : f)) };
}

/** A new widget at the start of `page`. */
export function addWidget(p: PhonePrefs, kind: WidgetKind, page: number, id: string): PhonePrefs {
	const widget: PhoneWidget = { id, kind, size: kind === 'clock' ? 's' : 'l' };
	const pages = p.pages.map((x) => [...x]);
	while (pages.length <= page) pages.push([]);
	pages[page].unshift('widget:' + id);
	return { ...p, pages, widgets: [...p.widgets, widget] };
}

export function removeWidget(p: PhonePrefs, id: string): PhonePrefs {
	return {
		...p,
		pages: p.pages.map((page) => page.filter((k) => k !== 'widget:' + id)),
		widgets: p.widgets.filter((w) => w.id !== id),
	};
}

export function resizeWidget(p: PhonePrefs, id: string): PhonePrefs {
	return {
		...p,
		widgets: p.widgets.map((w) => (w.id === id ? { ...w, size: w.size === 's' ? 'l' : 's' } : w)),
	};
}

/** Pages left empty after arranging go away (one page always stays). */
export function dropEmptyPages(p: PhonePrefs): PhonePrefs {
	const pages = p.pages.filter((page) => page.length);
	return { ...p, pages: pages.length ? pages : [[]] };
}
