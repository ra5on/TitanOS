// What the desktop does to its own settings (icons, folders, widgets) and
// to its windows: the steps behind the context menus, the menu bar and the
// icons themselves. The components of the desktop share them.
import { api } from '../api.ts';
import { t } from '../i18n/index.svelte.ts';
import {
	defaultWidgets,
	isGpuWidget,
	type GpuValue,
	type Widget,
	type WidgetKind,
} from './prefs.ts';
import type { Program } from './programs.svelte.ts';
import { sources } from './sources.svelte.ts';
import { desktop, focus, minimize, savePrefs, wm } from './wm.svelte.ts';

/** The context menu of the desktop: where it opened and what it is about. */
export type DeskMenu = {
	x: number;
	y: number;
	kind: 'desk' | 'icon' | 'sort' | 'size' | 'bar' | 'widget' | 'widgetSize' | 'dock' | 'dockPos';
	key?: string;
	/** Opened from the dock (its own items in the program menu). */
	dock?: boolean;
};

export type DeskDialog = 'wallpaper' | 'settings' | 'gallery' | 'layout';

/** The place of an icon on the grid. */
export type Cell = { c: number; r: number };

/** The icons on the desktop right now, in order (read at the moment of the change). */
export type IconsNow = () => string[];

/** Where a context menu opens: at the pointer, or under the element for the keyboard's menu key. */
export function menuAt(e: MouseEvent): { x: number; y: number } {
	if (e.clientX || e.clientY) return { x: e.clientX, y: e.clientY };
	const r = ((e.currentTarget ?? e.target) as HTMLElement).getBoundingClientRect();
	return { x: r.left + r.width / 2, y: r.top + r.height / 2 };
}

export function autoArrange() {
	savePrefs((p) => {
		p.iconPos = {};
		p.sort = 'kind';
	});
}

export function setSort(
	mode: 'name' | 'kind' | 'recent' | 'free',
	places: () => Map<string, Cell>,
) {
	savePrefs((p) => {
		// Into "free" without own places: the icons stay where they are now.
		if (mode === 'free' && p.sort !== 'free' && !Object.keys(p.iconPos).length)
			p.iconPos = Object.fromEntries([...places()].map(([k, at]) => [k, { ...at }]));
		p.sort = mode;
	});
}

export function removeIcon(key: string, iconsNow: IconsNow) {
	savePrefs((p) => {
		p.icons = iconsNow().filter((k) => k !== key);
		p.customIcons = true;
		if (key.startsWith('folder:')) {
			const folder = p.folders.find((f) => 'folder:' + f.id === key);
			p.folders = p.folders.filter((f) => 'folder:' + f.id !== key);
			// The folder's programs come back onto the desktop.
			if (folder) p.icons = [...p.icons, ...folder.apps];
		}
	});
}

export function addIcon(key: string, iconsNow: IconsNow) {
	savePrefs((d) => {
		d.icons = [...iconsNow(), key];
		d.customIcons = true;
	});
}

/** A new folder on the desktop; returns its key. */
export function newFolder(iconsNow: IconsNow): string {
	const id = 'f' + Date.now().toString(36);
	savePrefs((p) => {
		p.folders = [...p.folders, { id, name: t('desktop.folder.newName'), apps: [] }];
		p.icons = [...iconsNow(), 'folder:' + id];
		p.customIcons = true;
	});
	return 'folder:' + id;
}

export function renameFolder(key: string, name: string) {
	const clean = name.trim().slice(0, 60);
	if (!clean) return;
	savePrefs(
		(p) =>
			(p.folders = p.folders.map((f) => ('folder:' + f.id === key ? { ...f, name: clean } : f))),
	);
}

export function takeOut(folderKey: string, app: string, iconsNow: IconsNow) {
	savePrefs((p) => {
		p.folders = p.folders.map((f) =>
			'folder:' + f.id === folderKey ? { ...f, apps: f.apps.filter((a) => a !== app) } : f,
		);
		p.icons = [...iconsNow(), app];
		p.customIcons = true;
	});
}

export const folderOf = (key: string) =>
	desktop.prefs.folders.find((f) => 'folder:' + f.id === key);

// Widgets: placed freely on a grid of 20 px; negative x counts from the right edge.
export const CELL = 20;
export const WIDGET_DIMS = { s: [12, 5], m: [17, 8], l: [24, 12] } as const;
// PLAN 79-5: the graphics widgets hold a headline, a line and up to four tiles.
const GPU_DIMS = { s: [15, 17], m: [17, 17], l: [24, 18] } as const;
/** Width and height in cells a widget of this kind gets for a size. */
export function widgetDims(kind: WidgetKind, size: 's' | 'm' | 'l'): readonly [number, number] {
	return (isGpuWidget(kind) ? GPU_DIMS : WIDGET_DIMS)[size];
}

export function widgetLeft(w: Widget, columns: number) {
	return (w.x < 0 ? columns + w.x : w.x) * CELL;
}

export function removeWidget(id: string) {
	savePrefs((p) => {
		p.widgets = p.widgets.filter((w) => w.id !== id);
		p.customWidgets = true;
	});
}

export function resizeWidget(id: string, size: 's' | 'm' | 'l') {
	savePrefs((p) => {
		p.widgets = p.widgets.map((w) => {
			if (w.id !== id) return w;
			const dims = widgetDims(w.kind, size);
			return { ...w, x: w.x < 0 ? -dims[0] : w.x, w: dims[0], h: Math.max(w.h, dims[1]) };
		});
		p.customWidgets = true;
	});
}

/** PLAN 79-5: shows or hides one value of a graphics widget. */
export function toggleWidgetValue(id: string, value: GpuValue) {
	savePrefs((p) => {
		p.widgets = p.widgets.map((w) => {
			if (w.id !== id) return w;
			const hide = (w.hide ?? []).includes(value)
				? (w.hide ?? []).filter((v) => v !== value)
				: [...(w.hide ?? []), value];
			const next: Widget = { ...w };
			if (hide.length) next.hide = hide;
			else delete next.hide;
			return next;
		});
		p.customWidgets = true;
	});
}

/** PLAN 79-5: the device a graphics widget shows (its PCI address). */
export function setWidgetDevice(id: string, device: string) {
	savePrefs((p) => {
		p.widgets = p.widgets.map((w) => (w.id === id ? { ...w, device } : w));
		p.customWidgets = true;
	});
}

export function resetWidgets() {
	savePrefs((p) => {
		p.widgets = defaultWidgets();
		p.customWidgets = false;
	});
}

export function showDesktop() {
	const visible = wm.windows.filter((w) => !w.minimized);
	if (visible.length) visible.forEach((w) => minimize(w.id));
	else wm.windows.forEach((w) => focus(w.id));
}

// PLAN 62-3: the VMs window lists every machine below its pages (saves the page's own list).
export function sideOf(app: string, side: Program['side']): Program['side'] {
	if (app !== 'app:vms' || !sources.vms.length) return side;
	return [
		...side,
		{ group: 'desktop.side.vmList' },
		...sources.vms.map((vm) => ({
			href: '/vms?vm=' + encodeURIComponent(vm.name),
			text: vm.displayName || vm.name,
			state: vm.state === 'running' ? ('on' as const) : ('off' as const),
		})),
	];
}

/** Restart, stop or start every container of a Docker app, like the Docker page does it. */
export async function dockerAction(key: string, action: 'restart' | 'stop' | 'start') {
	const app = key.slice('docker:'.length);
	const group = sources.containers.filter((c) => (c.app || c.name) === app);
	for (const c of group)
		await api.post(`/docker/containers/${encodeURIComponent(c.id)}/${action}`).catch(() => {});
}
