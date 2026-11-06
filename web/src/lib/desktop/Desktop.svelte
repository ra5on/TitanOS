<script lang="ts">
	// PLAN 61: the desktop mode – TitanOS as a desktop with programs in
	// windows, widgets and a main menu. It replaces the head line and side
	// bar on large screens; every page of TitanOS opens in a window.
	import { onMount, untrack } from 'svelte';
	import { page } from '$app/state';
	import { replaceState } from '$app/navigation';
	import { api } from '../api.ts';
	import ContextMenu from '../components/ContextMenu.svelte';
	import Icon from '../components/Icon.svelte';
	import Notifications from '../components/Notifications.svelte';
	import TasksPanel from '../components/TasksPanel.svelte';
	import AccountMenu from '../components/AccountMenu.svelte';
	import NodeMenu from '../components/NodeMenu.svelte';
	import { clusterNodes } from '../cluster.svelte.ts';
	import { t } from '../i18n/index.svelte.ts';
	import { session } from '../session.svelte.ts';
	import { system } from '../system.svelte.ts';
	import { appearance } from '../theme/appearance.svelte.ts';
	import { THEMES } from '../theme/themes.ts';
	import { lock } from '../lock.svelte.ts';
	import { appForPath, appAllowed } from './apps.ts';
	import DesktopIcon from './DesktopIcon.svelte';
	import Launcher from './Launcher.svelte';
	import NotifyCenter from './NotifyCenter.svelte';
	import DesktopDialogs from './DesktopDialogs.svelte';
	import Window from './Window.svelte';
	import Dock, { dockPin, dockUnpin } from './Dock.svelte';
	import WidgetView from './WidgetView.svelte';
	import {
		arrangeIcons,
		defaultWidgets,
		dockPinned,
		placeIcons,
		placeWidget,
		WIDGET_KINDS,
		type Widget,
		type WidgetKind,
	} from './prefs.ts';
	import { findProgram, openProgram, openTab, programs, type Program } from './programs.svelte.ts';
	import { sources, watchSources } from './sources.svelte.ts';
	import { dayTint, wallpaperCss } from './wallpapers.ts';
	import {
		byRecency,
		closeWindow,
		desktop,
		focus,
		loadDesktop,
		minimize,
		savePrefs,
		setSnap,
		snapRect,
		wm,
	} from './wm.svelte.ts';
	import { formatPercent, formatMegabytesPerSecond } from '../format.ts';

	const onlock = () => void lock();

	let area = $state<HTMLElement>();
	let launcher = $state(false);
	let center = $state(false);
	let editWidgets = $state(false);
	let editDock = $state(false);
	let dialog = $state<'wallpaper' | 'settings' | 'gallery' | 'layout' | null>(null);
	let openFolder = $state<string | null>(null);
	let menu = $state<{
		x: number;
		y: number;
		kind: 'desk' | 'icon' | 'sort' | 'size' | 'bar' | 'widget' | 'widgetSize' | 'dock' | 'dockPos';
		key?: string;
		/** Opened from the dock (its own items in the program menu). */
		dock?: boolean;
	} | null>(null);
	let renaming = $state<string | null>(null);
	let dragKey = $state<string | null>(null);
	let dropBefore = $state<string | null>(null);
	let fileDrop = $state(false);
	let bubble = $state(false);

	const role = $derived(session.data?.user?.role ?? 'user');
	const light = $derived(THEMES[appearance.current.theme as keyof typeof THEMES]?.dark === false);
	const all = $derived(programs());
	const byKey = $derived(new Map(all.map((p) => [p.key, p])));
	const icons = $derived(
		arrangeIcons(
			desktop.prefs,
			all.map((p) => ({ key: p.key, label: p.label, kind: p.kind })),
		),
	);
	const agentAllowed = $derived(all.some((p) => p.key === 'app:agent'));
	const sizes = { s: 48, m: 60, l: 72 } as const;
	const iconPx = $derived(sizes[desktop.prefs.iconSize]);
	const barIcon = $derived({ s: 15, m: 18, l: 22 }[desktop.prefs.barSize]);
	const known = $derived(new Set(all.map((p) => p.key)));
	const inDock = $derived(new Set(dockPinned(desktop.prefs, known)));

	// The dock keeps its edge free: snapped windows and icons stay clear of it.
	const dockRoom = $derived(desktop.prefs.dock.size + 34);
	$effect(() => {
		const d = desktop.prefs.dock;
		const room = d.show ? dockRoom : 0;
		wm.inset = {
			l: d.position === 'left' ? room : 0,
			r: d.position === 'right' ? room : 0,
			b: d.position === 'bottom' ? room : 0,
		};
	});

	// Icons sit on a grid; "free" keeps each icon where it was put (PLAN 62-1).
	const colW = $derived(iconPx + 60);
	const rowH = $derived(iconPx + 56);
	const ICON_LEFT = 18;
	const ICON_TOP = 8;
	const gridRows = $derived(
		Math.max(1, Math.floor((wm.area.h - ICON_TOP - 8 - wm.inset.b) / rowH)),
	);
	const gridCols = $derived(
		Math.max(1, Math.floor((wm.area.w - ICON_LEFT - wm.inset.l - wm.inset.r) / colW)),
	);
	const places = $derived(placeIcons(icons, desktop.prefs, gridRows, gridCols));
	function cellAt(clientX: number, clientY: number): { c: number; r: number } {
		const box = area?.getBoundingClientRect();
		const x = clientX - (box?.left ?? 0) - ICON_LEFT - wm.inset.l;
		const y = clientY - (box?.top ?? 0) - ICON_TOP;
		return {
			c: Math.max(0, Math.min(gridCols - 1, Math.floor(x / colW))),
			r: Math.max(0, Math.min(gridRows - 1, Math.floor(y / rowH))),
		};
	}
	/** Writes every icon's present place, so switching to "free" moves nothing. */
	function freeze(p: typeof desktop.prefs) {
		const next: Record<string, { c: number; r: number }> = {};
		for (const [k, at] of places) next[k] = { ...at };
		p.iconPos = next;
		p.sort = 'free';
	}
	function autoArrange() {
		savePrefs((p) => {
			p.iconPos = {};
			p.sort = 'kind';
		});
	}

	// The wallpaper: built in (light/dark by the theme) or the account's own picture.
	let tint = $state(dayTint());
	$effect(() => {
		const timer = setInterval(() => (tint = dayTint()), 10 * 60 * 1000);
		return () => clearInterval(timer);
	});
	const wallpaper = $derived(
		desktop.prefs.wallpaper.source === 'custom'
			? `url("/api/v1/background?v=${desktop.prefs.wallpaper.version}") center / cover`
			: `${wallpaperCss(desktop.prefs.wallpaper.id, light)} center / cover`,
	);

	onMount(() => {
		loadDesktop();
		// A link to a page opens it in its window (PLAN 61-8: the address shows the window in front).
		const path = page.url.pathname + page.url.search;
		if (page.url.pathname !== '/') {
			const app = appForPath(page.url.pathname);
			if (app && appAllowed(app, role, session.services))
				void openProgram('app:' + app.id, { href: path });
		}
		const stop = watchSources();
		const measure = () => {
			if (!area) return;
			wm.area = { w: area.clientWidth, h: area.clientHeight };
		};
		measure();
		const observer = new ResizeObserver(measure);
		if (area) observer.observe(area);
		return () => {
			stop();
			observer.disconnect();
		};
	});

	// The address of the browser follows the window in front.
	function address(path: string) {
		if (location.pathname + location.search === path) return;
		try {
			replaceState(path, page.state);
		} catch {
			// Before the router is ready the address stays as it is.
		}
	}
	$effect(() => {
		const front = wm.windows.find((w) => w.id === wm.focused && !w.minimized);
		if (!front || front.app.startsWith('docker:')) address('/');
	});

	function iconCenter(event: Event): { x: number; y: number } | undefined {
		const el = (event.currentTarget as HTMLElement | null)?.getBoundingClientRect();
		const box = area?.getBoundingClientRect();
		return el && box
			? { x: el.left - box.left + el.width / 2 - 40, y: el.top - box.top + el.height / 2 - 30 }
			: undefined;
	}
	function open(key: string, event?: Event, href?: string) {
		launcher = false;
		openFolder = null;
		if (key.startsWith('folder:')) {
			openFolder = key;
			return;
		}
		void openProgram(key, { href, from: event ? iconCenter(event) : undefined });
	}

	// Icons: dragged to another place, onto a folder, or out of the menu.
	function iconsNow(): string[] {
		return [...icons];
	}
	function dropOn(event: DragEvent, target: string | null) {
		event.preventDefault();
		const key = event.dataTransfer?.getData('text/x-titanos-program') || dragKey;
		dragKey = null;
		dropBefore = null;
		if (!key) return;
		if (target?.startsWith('folder:') && !key.startsWith('folder:')) {
			const id = target.slice(7);
			savePrefs((p) => {
				p.folders = p.folders.map((f) =>
					f.id === id
						? { ...f, apps: [...new Set([...f.apps.filter((a) => a !== key), key])] }
						: { ...f, apps: f.apps.filter((a) => a !== key) },
				);
				p.icons = iconsNow().filter((k) => k !== key);
				p.customIcons = true;
			});
			return;
		}
		const cell = cellAt(event.clientX, event.clientY);
		const from = places.get(key);
		savePrefs((p) => {
			freeze(p);
			if (!iconsNow().includes(key)) p.icons = [...iconsNow(), key];
			else p.icons = iconsNow();
			p.customIcons = true;
			p.folders = p.folders.map((f) => ({ ...f, apps: f.apps.filter((a) => a !== key) }));
			// Onto another icon: the two change places; onto free space: it stays there.
			const other = target ?? [...places].find(([, at]) => at.c === cell.c && at.r === cell.r)?.[0];
			const to = target ? places.get(target) : cell;
			if (!to) return;
			if (other && other !== key) {
				if (from) p.iconPos[other] = { ...from };
				else delete p.iconPos[other];
			}
			p.iconPos[key] = { ...to };
		});
	}
	function removeIcon(key: string) {
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
	function newFolder() {
		const id = 'f' + Date.now().toString(36);
		savePrefs((p) => {
			p.folders = [...p.folders, { id, name: t('desktop.folder.newName'), apps: [] }];
			p.icons = [...iconsNow(), 'folder:' + id];
			p.customIcons = true;
		});
		renaming = 'folder:' + id;
	}
	function renameFolder(key: string, name: string) {
		renaming = null;
		const clean = name.trim().slice(0, 60);
		if (!clean) return;
		savePrefs(
			(p) =>
				(p.folders = p.folders.map((f) => ('folder:' + f.id === key ? { ...f, name: clean } : f))),
		);
	}
	function takeOut(folderKey: string, app: string) {
		savePrefs((p) => {
			p.folders = p.folders.map((f) =>
				'folder:' + f.id === folderKey ? { ...f, apps: f.apps.filter((a) => a !== app) } : f,
			);
			p.icons = [...iconsNow(), app];
			p.customIcons = true;
		});
	}
	const folderOf = (key: string) => desktop.prefs.folders.find((f) => 'folder:' + f.id === key);

	// Files from the computer dropped on the desktop go to the file manager's upload.
	function dropFiles(event: DragEvent) {
		const files = [...(event.dataTransfer?.files ?? [])];
		fileDrop = false;
		if (!files.length) return false;
		event.preventDefault();
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
		return true;
	}

	// Widgets: placed freely on a grid of 20 px; negative x counts from the right edge.
	const CELL = 20;
	const columns = $derived(Math.max(10, Math.floor(wm.area.w / CELL)));
	function widgetLeft(w: Widget) {
		return (w.x < 0 ? columns + w.x : w.x) * CELL;
	}
	function startWidget(event: PointerEvent, w: Widget, mode: 'move' | 'size') {
		if (!editWidgets || event.button !== 0) return;
		event.preventDefault();
		const start = {
			px: event.clientX,
			py: event.clientY,
			x: w.x < 0 ? columns + w.x : w.x,
			y: w.y,
			w: w.w,
			h: w.h,
		};
		const move = (e: PointerEvent) => {
			const dx = Math.round((e.clientX - start.px) / CELL);
			const dy = Math.round((e.clientY - start.py) / CELL);
			if (mode === 'move') {
				w.x = Math.max(0, Math.min(columns - w.w, start.x + dx));
				w.y = Math.max(0, start.y + dy);
			} else {
				w.w = Math.max(6, Math.min(columns - start.x, start.w + dx));
				w.h = Math.max(3, start.h + dy);
				w.x = start.x;
			}
		};
		const up = () => {
			window.removeEventListener('pointermove', move);
			window.removeEventListener('pointerup', up);
			// Near the right edge it stays anchored there when the window grows.
			if (w.x + w.w >= columns - 1)
				w.x = -w.w - (columns - w.x - w.w > 0 ? columns - w.x - w.w : 0);
			savePrefs((p) => (p.customWidgets = true));
		};
		window.addEventListener('pointermove', move);
		window.addEventListener('pointerup', up);
	}
	function removeWidget(id: string) {
		savePrefs((p) => {
			p.widgets = p.widgets.filter((w) => w.id !== id);
			p.customWidgets = true;
		});
	}
	function addWidget(kind: WidgetKind, size: 's' | 'm' | 'l') {
		const dims = { s: [12, 5], m: [17, 8], l: [24, 12] }[size];
		// New widgets go beside the icons, never on top of them.
		const iconsRight =
			ICON_LEFT + wm.inset.l + (Math.max(0, ...[...places.values()].map((at) => at.c)) + 1) * colW;
		const at = placeWidget(
			desktop.prefs.widgets,
			dims[0],
			dims[1],
			columns,
			Math.ceil(iconsRight / CELL) + 1,
		);
		savePrefs((p) => {
			p.widgets = [
				...p.widgets,
				{ id: 'w' + Date.now().toString(36), kind, x: at.x, y: at.y, w: dims[0], h: dims[1] },
			];
			p.customWidgets = true;
		});
	}
	function resizeWidget(id: string, size: 's' | 'm' | 'l') {
		const dims = { s: [12, 5], m: [17, 8], l: [24, 12] }[size];
		savePrefs((p) => {
			p.widgets = p.widgets.map((w) =>
				w.id === id
					? { ...w, x: w.x < 0 ? -dims[0] : w.x, w: dims[0], h: Math.max(w.h, dims[1]) }
					: w,
			);
			p.customWidgets = true;
		});
	}
	function resetWidgets() {
		savePrefs((p) => {
			p.widgets = defaultWidgets();
			p.customWidgets = false;
		});
	}

	// The switcher (Alt+Q, Alt+Tab belongs to the operating system) and window keys.
	function keys(event: KeyboardEvent) {
		if (event.altKey && !event.ctrlKey && !event.metaKey) {
			const k = event.key.toLowerCase();
			if (k === 'q' && wm.windows.length) {
				event.preventDefault();
				const list = byRecency();
				wm.switcherIndex = wm.switcher
					? (wm.switcherIndex + (event.shiftKey ? list.length - 1 : 1)) % list.length
					: Math.min(1, list.length - 1);
				wm.switcher = true;
				return;
			}
			const front = wm.focused;
			if (!front) return;
			if (k === 'w') {
				event.preventDefault();
				closeWindow(front);
			} else if (k === 'm') {
				event.preventDefault();
				minimize(front);
			} else if (
				event.key === 'ArrowLeft' ||
				event.key === 'ArrowRight' ||
				event.key === 'ArrowUp' ||
				event.key === 'ArrowDown'
			) {
				event.preventDefault();
				setSnap(
					front,
					event.key === 'ArrowLeft'
						? 'left'
						: event.key === 'ArrowRight'
							? 'right'
							: event.key === 'ArrowUp'
								? 'max'
								: null,
				);
			}
		}
		if ((event.ctrlKey || event.metaKey) && event.code === 'Space') {
			event.preventDefault();
			launcher = !launcher;
		}
		if (event.key === 'Escape') {
			launcher = false;
			center = false;
			openFolder = null;
			editWidgets = false;
			editDock = false;
		}
	}

	// Menu and message centre close on a click beside them (also into a window's frame).
	function outside(event: PointerEvent) {
		const el = event.target as HTMLElement | null;
		if (launcher && !el?.closest('.launcher, [data-opens="launcher"], .dropdown-panel, dialog'))
			launcher = false;
		if (center && !el?.closest('.center, [data-opens="center"], dialog')) center = false;
	}
	function blurred() {
		// Focus went into a window's frame: that was a click beside the menu.
		setTimeout(() => {
			if (document.activeElement instanceof HTMLIFrameElement) {
				launcher = false;
				center = false;
			}
		}, 0);
	}
	function keyUp(event: KeyboardEvent) {
		if (wm.switcher && (event.key === 'Alt' || !event.altKey)) {
			const target = byRecency()[wm.switcherIndex];
			wm.switcher = false;
			if (target) focus(target.id);
		}
	}

	function showDesktop() {
		const visible = wm.windows.filter((w) => !w.minimized);
		if (visible.length) visible.forEach((w) => minimize(w.id));
		else wm.windows.forEach((w) => focus(w.id));
	}
	function taskClick(id: string) {
		const w = wm.windows.find((x) => x.id === id);
		if (!w) return;
		if (wm.focused === id && !w.minimized) minimize(id);
		else focus(id);
	}
	// PLAN 62-3: the VMs window lists every machine below its pages (saves the page's own list).
	function sideOf(app: string, side: Program['side']): Program['side'] {
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
	function programOf(app: string): Program | undefined {
		return byKey.get(app) ?? findProgram(app);
	}

	async function dockerAction(key: string, action: 'restart' | 'stop' | 'start') {
		const app = key.slice('docker:'.length);
		menu = null;
		// Every container of the app, like the Docker page does it.
		const group = sources.containers.filter((c) => (c.app || c.name) === app);
		for (const c of group)
			await api.post(`/docker/containers/${encodeURIComponent(c.id)}/${action}`).catch(() => {});
	}

	const info = $derived(system.info);
	const memPct = $derived(
		info
			? (100 * (info.memory.totalBytes - info.memory.availableBytes)) / info.memory.totalBytes
			: 0,
	);
	const rx = $derived(
		(info?.networks ?? (info?.network ? [info.network] : [])).reduce(
			(s, n) => s + (n.rxBytesPerSecond || 0),
			0,
		),
	);
	const tx = $derived(
		(info?.networks ?? (info?.network ? [info.network] : [])).reduce(
			(s, n) => s + (n.txBytesPerSecond || 0),
			0,
		),
	);
	const gauge = (pct: number) => {
		const c = 2 * Math.PI * 9;
		return `${(c * Math.min(100, Math.max(0, pct))) / 100} ${c}`;
	};
	const reduced = matchMedia('(prefers-reduced-motion: reduce)').matches;
	// The helper bubble knows the window in front: its question starts with it (the agent's "ask").
	let bubbleSrc = $state('/agent');
	$effect(() => {
		if (!bubble) return;
		const front = untrack(() => wm.windows.find((w) => w.id === wm.focused && !w.minimized));
		bubbleSrc = front
			? '/agent?ask=' +
				encodeURIComponent(
					t('desktop.agent.context', {
						name: front.title,
						page: front.app.startsWith('docker:') ? front.title : front.href,
					}),
				)
			: '/agent';
	});
</script>

<svelte:window onkeydown={keys} onkeyup={keyUp} onpointerdowncapture={outside} onblur={blurred} />

<div
	class="desktop"
	class:light
	class:live={desktop.prefs.wallpaper.live && !reduced}
	class:moving={wm.dragging}
	style:--wallpaper={wallpaper}
	style:--tint="{tint}deg"
	style:--icon={iconPx + 'px'}
	style:--pb={{ s: 28, m: 34, l: 42 }[desktop.prefs.barSize] + 'px'}
>
	<div class="paper" aria-hidden="true"></div>
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<header
		class="topbar"
		oncontextmenu={(e) => {
			if ((e.target as HTMLElement).closest('.menu, .floating-menu, [role="menu"]')) return;
			e.preventDefault();
			menu = { x: e.clientX, y: e.clientY, kind: 'bar' };
		}}
	>
		<div class="pill glass-panel">
			<button
				type="button"
				class="pb"
				class:on={!wm.windows.some((w) => !w.minimized)}
				onclick={showDesktop}
				title={t('desktop.bar.showDesktop')}
				aria-label={t('desktop.bar.showDesktop')}><Icon name="monitor" size={barIcon} /></button
			>
			<button
				type="button"
				class="pb"
				class:on={launcher}
				data-opens="launcher"
				onclick={() => (launcher = !launcher)}
				title={t('desktop.menu.title')}
				aria-label={t('desktop.menu.title')}
				aria-expanded={launcher}><Icon name="grid" size={barIcon} /></button
			>
			{#if wm.windows.length}<span class="sep" aria-hidden="true"></span>{/if}
			{#each wm.windows as w (w.id)}
				{@const p = programOf(w.app)}
				<button
					type="button"
					class="pb task"
					class:on={wm.focused === w.id && !w.minimized}
					class:min={w.minimized}
					onclick={() => taskClick(w.id)}
					title={w.title}
					aria-label={w.title}
				>
					<DesktopIcon
						glyph={p?.glyph ?? ''}
						gradient={p?.gradient ?? 'grey'}
						image={p?.image ?? ''}
						name={w.title}
						size={barIcon + 6}
					/>
				</button>
			{/each}
		</div>
		{#if clusterNodes.nodes.length > 1}
			<div class="pill glass-panel right"><NodeMenu variant="desk" /></div>
		{/if}
		<span class="space"></span>
		<div class="pill glass-panel right">
			{#if info}
				<span class="gauge" title={t('desktop.widget.cpu')}>
					<svg width="22" height="22" viewBox="0 0 24 24" aria-hidden="true"
						><circle
							cx="12"
							cy="12"
							r="9"
							fill="none"
							stroke="var(--track)"
							stroke-width="3"
						/><circle
							cx="12"
							cy="12"
							r="9"
							fill="none"
							stroke="var(--accent)"
							stroke-width="3"
							stroke-dasharray={gauge(info.cpu.usagePercent)}
							transform="rotate(-90 12 12)"
						/></svg
					>
					<span>{t('desktop.widget.cpu')}<br /><b>{formatPercent(info.cpu.usagePercent)}</b></span>
				</span>
				<span class="gauge" title={t('desktop.widget.ram')}>
					<svg width="22" height="22" viewBox="0 0 24 24" aria-hidden="true"
						><circle
							cx="12"
							cy="12"
							r="9"
							fill="none"
							stroke="var(--track)"
							stroke-width="3"
						/><circle
							cx="12"
							cy="12"
							r="9"
							fill="none"
							stroke="var(--accent)"
							stroke-width="3"
							stroke-dasharray={gauge(memPct)}
							transform="rotate(-90 12 12)"
						/></svg
					>
					<span>{t('desktop.widget.ram')}<br /><b>{formatPercent(memPct)}</b></span>
				</span>
				<span class="gauge net"
					>↑ {formatMegabytesPerSecond(tx)}<br />↓ {formatMegabytesPerSecond(rx)}</span
				>
				<span class="sep" aria-hidden="true"></span>
			{/if}
			<TasksPanel badge />
			{#if role !== 'user'}<Notifications compact />{/if}
			<button
				type="button"
				class="pb"
				class:on={center}
				data-opens="center"
				onclick={() => (center = !center)}
				title={t('desktop.center.title')}
				aria-label={t('desktop.center.title')}
				aria-expanded={center}><Icon name="list" size={barIcon} /></button
			>
			<button
				type="button"
				class="pb"
				data-opens="launcher"
				onclick={() => (launcher = true)}
				title={t('desktop.menu.search')}
				aria-label={t('desktop.menu.search')}><Icon name="search" size={barIcon} /></button
			>
			<AccountMenu />
		</div>
	</header>

	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<div
		class="desk-area"
		bind:this={area}
		style:--dock-room="{dockRoom}px"
		ondragover={(e) => {
			e.preventDefault();
			if (e.dataTransfer?.types.includes('Files')) fileDrop = true;
		}}
		ondragleave={(e) => {
			if (e.target === area) fileDrop = false;
		}}
		ondrop={(e) => {
			if (!dropFiles(e)) dropOn(e, null);
		}}
		oncontextmenu={(e) => {
			if (e.target !== area && !(e.target as HTMLElement).classList.contains('icons')) return;
			editDock = false;
			e.preventDefault();
			menu = { x: e.clientX, y: e.clientY, kind: 'desk' };
		}}
	>
		<div class="icons" style:left="{ICON_LEFT + wm.inset.l}px">
			{#each icons as key (key)}
				{@const p = byKey.get(key)}
				{@const folder = key.startsWith('folder:') ? folderOf(key) : undefined}
				{#if p || folder}
					<button
						type="button"
						class="ico"
						class:drop={dropBefore === key}
						style:left="{(places.get(key)?.c ?? 0) * colW}px"
						style:top="{(places.get(key)?.r ?? 0) * rowH}px"
						style:width="{colW - 4}px"
						draggable="true"
						ondragstart={(e) => {
							dragKey = key;
							e.dataTransfer?.setData('text/x-titanos-program', key);
						}}
						ondragover={(e) => {
							e.preventDefault();
							e.stopPropagation();
							dropBefore = key;
						}}
						ondrop={(e) => {
							e.stopPropagation();
							dropOn(e, key);
						}}
						ondblclick={(e) => open(key, e)}
						onclick={(e) => {
							// One click opens, like the menu (a double click does the same).
							if (e.detail === 1) open(key, e);
						}}
						oncontextmenu={(e) => {
							e.preventDefault();
							e.stopPropagation();
							menu = { x: e.clientX, y: e.clientY, kind: 'icon', key };
						}}
						title={p?.badge?.title}
					>
						<span class="ico-wrap">
							{#if folder}
								<span class="folder-ico" style:width="var(--icon)" style:height="var(--icon)">
									{#each folder.apps.slice(0, 4) as app (app)}
										{@const fp = byKey.get(app)}
										{#if fp}<DesktopIcon
												glyph={fp.glyph}
												gradient={fp.gradient}
												image={fp.image}
												name={fp.label}
												size={Math.round(iconPx * 0.36)}
											/>{/if}
									{/each}
								</span>
							{:else if p}
								<DesktopIcon
									glyph={p.glyph}
									gradient={p.gradient}
									image={p.image}
									name={p.label}
									size={iconPx}
								/>
								{#if p.badge?.count}<span class="badge count" aria-label={p.badge.title}
										>{p.badge.count}</span
									>
								{:else if p.badge?.warn}<span class="badge warn" aria-label={p.badge.title}>!</span>
								{:else if p.badge?.dot}<span
										class="badge dot {p.badge.dot}"
										aria-label={p.badge.title}
									></span>{/if}
							{/if}
						</span>
						{#if renaming === key && folder}
							<!-- svelte-ignore a11y_autofocus -->
							<input
								class="rename"
								value={folder.name}
								autofocus
								onclick={(e) => e.stopPropagation()}
								onblur={(e) => renameFolder(key, e.currentTarget.value)}
								onkeydown={(e) => e.key === 'Enter' && renameFolder(key, e.currentTarget.value)}
								aria-label={t('desktop.folder.rename')}
							/>
						{:else}
							<span class="label">{folder?.name ?? p?.label}</span>
						{/if}
					</button>
				{/if}
			{/each}
		</div>

		{#each desktop.prefs.widgetsHidden ? [] : desktop.prefs.widgets as w (w.id)}
			<!-- svelte-ignore a11y_no_static_element_interactions -->
			<div
				class="widget glass-panel"
				class:edit={editWidgets}
				style:left="{widgetLeft(w)}px"
				style:top="{w.y * CELL}px"
				style:width="{w.w * CELL - 10}px"
				style:height="{w.h * CELL - 10}px"
				onpointerdown={(e) => startWidget(e, w, 'move')}
				oncontextmenu={(e) => {
					e.preventDefault();
					e.stopPropagation();
					menu = { x: e.clientX, y: e.clientY, kind: 'widget', key: w.id };
				}}
			>
				<WidgetView kind={w.kind} onopen={(key, href) => open(key, undefined, href)} />
				{#if editWidgets}
					<button
						type="button"
						class="w-remove"
						onclick={() => removeWidget(w.id)}
						aria-label={t('desktop.widgets.remove')}><Icon name="close" size={14} /></button
					>
					<!-- svelte-ignore a11y_no_static_element_interactions -->
					<span
						class="w-size"
						onpointerdown={(e) => (e.stopPropagation(), startWidget(e, w, 'size'))}
					></span>
				{/if}
			</div>
		{/each}
		{#if editWidgets}
			<div class="edit-bar glass-panel">
				<span>{t('desktop.widgets.editHint')}</span>
				<button type="button" class="btn" onclick={() => (dialog = 'gallery')}
					><Icon name="plus" size={15} />{t('desktop.widgets.add')}</button
				>
				<button type="button" class="btn" onclick={resetWidgets}
					>{t('desktop.widgets.reset')}</button
				>
				<button type="button" class="btn btn-primary" onclick={() => (editWidgets = false)}
					>{t('desktop.widgets.done')}</button
				>
			</div>
		{/if}

		{#each wm.windows as w (w.id)}
			{@const p = programOf(w.app)}
			<Window
				win={w}
				glyph={p?.glyph ?? ''}
				gradient={p?.gradient ?? 'grey'}
				image={p?.image ?? ''}
				side={sideOf(w.app, p?.side ?? [])}
				external={p?.external ?? w.app.startsWith('docker:')}
				foldKey={w.app}
				onaddress={address}
			/>
		{/each}

		{#if desktop.prefs.dock.show}
			<Dock
				{all}
				edit={editDock}
				onopen={(key, e) => open(key, e)}
				onmenu={(e, key) =>
					(menu = key
						? { x: e.clientX, y: e.clientY, kind: 'icon', key, dock: true }
						: { x: e.clientX, y: e.clientY, kind: 'dock' })}
				ondone={() => (editDock = false)}
			/>
		{/if}

		{#if wm.preview}
			{@const r = snapRect(wm.preview)}
			{#if r}<div
					class="snap-preview"
					style:left="{r.x + 6}px"
					style:top="{r.y + 6}px"
					style:width="{r.w - 12}px"
					style:height="{r.h - 12}px"
				></div>{/if}
		{/if}

		{#if fileDrop}<div class="file-drop">
				<Icon name="upload" size={28} />{t('desktop.files.drop')}
			</div>{/if}

		{#if openFolder}
			{@const folder = folderOf(openFolder)}
			{#if folder}
				<div class="folder-pop glass-panel" role="dialog" aria-label={folder.name}>
					<div class="folder-head">
						<b>{folder.name}</b>
						<button type="button" class="link" onclick={() => (renaming = openFolder)}
							>{t('desktop.folder.rename')}</button
						>
						<button
							type="button"
							class="pb"
							onclick={() => (openFolder = null)}
							aria-label={t('desktop.window.close')}><Icon name="close" size={15} /></button
						>
					</div>
					<div class="folder-grid">
						{#each folder.apps as app (app)}
							{@const fp = byKey.get(app)}
							{#if fp}
								<button
									type="button"
									class="ico small"
									onclick={(e) => open(app, e)}
									oncontextmenu={(e) => {
										e.preventDefault();
										takeOut(openFolder ?? '', app);
									}}
									title={t('desktop.folder.takeOutHint')}
								>
									<span class="ico-wrap"
										><DesktopIcon
											glyph={fp.glyph}
											gradient={fp.gradient}
											image={fp.image}
											name={fp.label}
											size={52}
										/>{#if fp.badge?.count}<span class="badge count">{fp.badge.count}</span
											>{/if}</span
									>
									<span class="label">{fp.label}</span>
								</button>
							{/if}
						{:else}
							<p class="muted">{t('desktop.folder.empty')}</p>
						{/each}
					</div>
				</div>
			{/if}
		{/if}

		{#if launcher}<Launcher
				onopen={(key, href) => open(key, undefined, href)}
				onclose={() => (launcher = false)}
				{onlock}
			/>{/if}
		{#if center}<NotifyCenter
				onclose={() => (center = false)}
				onopen={(key, href) => open(key, undefined, href)}
			/>{/if}

		{#if wm.switcher}
			<div class="switch-dim" aria-hidden="true"></div>
			<div class="switcher glass-panel" role="listbox" aria-label={t('desktop.switcher.title')}>
				{#each byRecency() as w, i (w.id)}
					{@const p = programOf(w.app)}
					<button
						type="button"
						class="thumb"
						class:on={i === wm.switcherIndex}
						role="option"
						aria-selected={i === wm.switcherIndex}
						onclick={() => {
							wm.switcher = false;
							focus(w.id);
						}}
					>
						<span class="mini"><span></span><span></span><span></span><span></span></span>
						<span class="thumb-name"
							><DesktopIcon
								glyph={p?.glyph ?? ''}
								gradient={p?.gradient ?? 'grey'}
								image={p?.image ?? ''}
								name={w.title}
								size={24}
							/><b>{w.title}</b></span
						>
					</button>
				{/each}
				<p class="keys">{t('desktop.switcher.keys')}</p>
			</div>
		{/if}

		{#if agentAllowed}
			<button
				type="button"
				class="bubble"
				class:on={bubble}
				onclick={() => (bubble = !bubble)}
				aria-label={t('desktop.agent.bubble')}
				title={t('desktop.agent.bubble')}
				aria-expanded={bubble}
			>
				<Icon name="sparkle" size={26} />
			</button>
			{#if bubble}
				<div class="bubble-pop glass-panel" role="dialog" aria-label={t('desktop.app.agent')}>
					<div class="bubble-head">
						<DesktopIcon glyph="agent" gradient="violet" size={24} />
						<b>{t('desktop.app.agent')}</b>
						<span class="space"></span>
						<button
							type="button"
							class="link"
							onclick={() => {
								bubble = false;
								open('app:agent');
							}}>{t('desktop.agent.openWindow')}</button
						>
						<button
							type="button"
							class="pb"
							onclick={() => (bubble = false)}
							aria-label={t('desktop.window.close')}><Icon name="close" size={15} /></button
						>
					</div>
					<iframe src={bubbleSrc} title={t('desktop.app.agent')}></iframe>
				</div>
			{/if}
		{/if}
	</div>
</div>

{#if menu}
	<ContextMenu
		label={t('desktop.menu.context')}
		x={menu.x}
		y={menu.y}
		onclose={() => (menu = null)}
	>
		{#if menu.kind === 'icon' && menu.key}
			{@const key = menu.key}
			{@const p = byKey.get(key)}
			{#if key.startsWith('folder:')}
				<button
					type="button"
					role="menuitem"
					onclick={() => {
						openFolder = key;
						menu = null;
					}}><span>{t('desktop.ctx.open')}</span></button
				>
				<button
					type="button"
					role="menuitem"
					onclick={() => {
						renaming = key;
						menu = null;
					}}><span>{t('desktop.folder.rename')}</span></button
				>
				<button
					type="button"
					role="menuitem"
					class="danger"
					onclick={() => {
						removeIcon(key);
						menu = null;
					}}><span>{t('desktop.folder.remove')}</span></button
				>
			{:else if p}
				<button
					type="button"
					role="menuitem"
					onclick={() => {
						open(key);
						menu = null;
					}}><span>{t('desktop.ctx.open')}</span></button
				>
				{#if p.multi || p.kind === 'system'}<button
						type="button"
						role="menuitem"
						onclick={() => {
							void openProgram(key, { fresh: true });
							menu = null;
						}}><span>{t('desktop.ctx.newWindow')}</span></button
					>{/if}
				<button
					type="button"
					role="menuitem"
					onclick={() => {
						void openTab(key, p.href);
						menu = null;
					}}><span>{t('desktop.window.openTab')}</span></button
				>
				{#if p.kind === 'docker' && role !== 'user'}
					<button type="button" role="menuitem" onclick={() => dockerAction(key, 'restart')}
						><span>{t('desktop.ctx.restart')}</span></button
					>
					<button
						type="button"
						role="menuitem"
						onclick={() => dockerAction(key, p.badge?.dot === 'off' ? 'start' : 'stop')}
						><span>{t(p.badge?.dot === 'off' ? 'desktop.ctx.start' : 'desktop.ctx.stop')}</span
						></button
					>
					<button
						type="button"
						role="menuitem"
						onclick={() => {
							open('app:docker', undefined, '/docker/apps/' + encodeURIComponent(key.slice(7)));
							menu = null;
						}}><span>{t('desktop.ctx.showInDocker')}</span></button
					>
					<button
						type="button"
						role="menuitem"
						onclick={() => {
							open('app:users', undefined, '/settings/programs');
							menu = null;
						}}><span>{t('desktop.ctx.whoMay')}</span></button
					>
				{/if}
				<span class="menu-rule" role="separator"></span>
				{#if inDock.has(key)}
					<button
						type="button"
						role="menuitem"
						onclick={() => {
							dockUnpin(key, known);
							menu = null;
						}}><span>{t('desktop.dock.unpin')}</span></button
					>
				{:else}
					<button
						type="button"
						role="menuitem"
						onclick={() => {
							dockPin(key, known);
							if (!desktop.prefs.dock.show) savePrefs((d) => (d.dock.show = true));
							menu = null;
						}}><span>{t('desktop.dock.pin')}</span></button
					>
				{/if}
				{#if icons.includes(key)}
					{#if !menu.dock}
						<button
							type="button"
							role="menuitem"
							class="danger"
							onclick={() => {
								removeIcon(key);
								menu = null;
							}}><span>{t('desktop.ctx.removeFromDesktop')}</span></button
						>
					{/if}
				{:else}
					<button
						type="button"
						role="menuitem"
						onclick={() => {
							savePrefs((d) => {
								d.icons = [...iconsNow(), key];
								d.customIcons = true;
							});
							menu = null;
						}}><span>{t('desktop.ctx.toDesktop')}</span></button
					>
				{/if}
				{#if wm.windows.some((w) => w.app === key)}
					<button
						type="button"
						role="menuitem"
						onclick={() => {
							wm.windows.filter((w) => w.app === key).forEach((w) => closeWindow(w.id));
							menu = null;
						}}><span>{t('desktop.dock.closeAll')}</span></button
					>
				{/if}
			{/if}
		{:else if menu.kind === 'bar'}
			{#each [['s', 'desktop.size.s'], ['m', 'desktop.size.m'], ['l', 'desktop.size.l']] as const as [size, label] (size)}
				<button
					type="button"
					role="menuitemradio"
					aria-checked={desktop.prefs.barSize === size}
					onclick={() => {
						savePrefs((p) => (p.barSize = size));
						menu = null;
					}}><span>{t('desktop.bar.size')}: {t(label)}</span></button
				>
			{/each}
		{:else if menu.kind === 'widget' && menu.key}
			{@const id = menu.key}
			<button
				type="button"
				role="menuitem"
				onclick={() => (menu = menu && { ...menu, kind: 'widgetSize' })}
				><span>{t('desktop.widgets.size')}</span><span aria-hidden="true">›</span></button
			>
			<button
				type="button"
				role="menuitem"
				class="danger"
				onclick={() => {
					removeWidget(id);
					menu = null;
				}}><span>{t('desktop.widgets.removeOne')}</span></button
			>
			<span class="menu-rule" role="separator"></span>
			<button
				type="button"
				role="menuitem"
				onclick={() => {
					editWidgets = true;
					menu = null;
				}}><span>{t('desktop.ctx.widgets')}</span></button
			>
			<button
				type="button"
				role="menuitem"
				onclick={() => {
					dialog = 'gallery';
					menu = null;
				}}><span>{t('desktop.widgets.add')}</span></button
			>
			<button
				type="button"
				role="menuitem"
				onclick={() => {
					savePrefs((p) => (p.widgetsHidden = true));
					editWidgets = false;
					menu = null;
				}}><span>{t('desktop.widgets.hideAll')}</span></button
			>
		{:else if menu.kind === 'widgetSize' && menu.key}
			{@const id = menu.key}
			{#each [['s', 'desktop.size.s'], ['m', 'desktop.size.m'], ['l', 'desktop.size.l']] as const as [size, label] (size)}
				<button
					type="button"
					role="menuitem"
					onclick={() => {
						resizeWidget(id, size);
						menu = null;
					}}><span>{t(label)}</span></button
				>
			{/each}
		{:else if menu.kind === 'dock'}
			<button
				type="button"
				role="menuitem"
				onclick={() => {
					editDock = true;
					menu = null;
				}}><span>{t('desktop.dock.edit')}</span></button
			>
			<button
				type="button"
				role="menuitem"
				onclick={() => (menu = menu && { ...menu, kind: 'dockPos' })}
				><span>{t('desktop.dock.position')}</span><span aria-hidden="true">›</span></button
			>
			<button
				type="button"
				role="menuitemcheckbox"
				aria-checked={desktop.prefs.dock.magnify > 0}
				onclick={() => {
					savePrefs((p) => (p.dock.magnify = p.dock.magnify ? 0 : 1));
					menu = null;
				}}><span>{t('desktop.dock.magnify')}</span></button
			>
			<span class="menu-rule" role="separator"></span>
			<button
				type="button"
				role="menuitem"
				onclick={() => {
					savePrefs((p) => (p.dock.show = false));
					editDock = false;
					menu = null;
				}}><span>{t('desktop.dock.hide')}</span></button
			>
		{:else if menu.kind === 'dockPos'}
			{#each [['bottom', 'desktop.dock.bottom'], ['left', 'desktop.dock.left'], ['right', 'desktop.dock.right']] as const as [pos, label] (pos)}
				<button
					type="button"
					role="menuitemradio"
					aria-checked={desktop.prefs.dock.position === pos}
					onclick={() => {
						savePrefs((p) => (p.dock.position = pos));
						menu = null;
					}}><span>{t(label)}</span></button
				>
			{/each}
		{:else if menu.kind === 'desk'}
			<button
				type="button"
				role="menuitem"
				onclick={() => (menu = menu && { ...menu, kind: 'sort' })}
				><span>{t('desktop.ctx.sort')}</span><span aria-hidden="true">›</span></button
			>
			<button
				type="button"
				role="menuitem"
				onclick={() => (menu = menu && { ...menu, kind: 'size' })}
				><span>{t('desktop.ctx.iconSize')}</span><span aria-hidden="true">›</span></button
			>
			<button
				type="button"
				role="menuitem"
				onclick={() => {
					autoArrange();
					menu = null;
				}}><span>{t('desktop.ctx.autoArrange')}</span></button
			>
			<button
				type="button"
				role="menuitem"
				onclick={() => {
					newFolder();
					menu = null;
				}}><span>{t('desktop.ctx.newFolder')}</span></button
			>
			<span class="menu-rule" role="separator"></span>
			<button
				type="button"
				role="menuitemcheckbox"
				aria-checked={!desktop.prefs.widgetsHidden}
				onclick={() => {
					savePrefs((p) => (p.widgetsHidden = !p.widgetsHidden));
					menu = null;
				}}><span>{t('desktop.widgets.show')}</span></button
			>
			<button
				type="button"
				role="menuitemcheckbox"
				aria-checked={desktop.prefs.dock.show}
				onclick={() => {
					savePrefs((p) => (p.dock.show = !p.dock.show));
					menu = null;
				}}><span>{t('desktop.dock.show')}</span></button
			>
			<span class="menu-rule" role="separator"></span>
			<button
				type="button"
				role="menuitem"
				onclick={() => {
					dialog = 'wallpaper';
					menu = null;
				}}><span>{t('desktop.ctx.wallpaper')}</span></button
			>
			<button
				type="button"
				role="menuitem"
				onclick={() => {
					editWidgets = true;
					if (desktop.prefs.widgetsHidden) savePrefs((p) => (p.widgetsHidden = false));
					menu = null;
				}}><span>{t('desktop.ctx.widgets')}</span></button
			>
			<button
				type="button"
				role="menuitem"
				onclick={() => {
					dialog = 'layout';
					menu = null;
				}}><span>{t('desktop.ctx.layouts')}</span></button
			>
			<button
				type="button"
				role="menuitem"
				onclick={() => {
					dialog = 'settings';
					menu = null;
				}}><span>{t('desktop.ctx.settings')}</span></button
			>
			<button
				type="button"
				role="menuitem"
				onclick={() => {
					open('app:settings', undefined, '/settings/appearance');
					menu = null;
				}}><span>{t('desktop.ctx.appearance')}</span></button
			>
		{:else if menu.kind === 'sort'}
			{#each [['name', 'desktop.sort.name'], ['kind', 'desktop.sort.kind'], ['recent', 'desktop.sort.recent'], ['free', 'desktop.sort.free']] as const as [mode, label] (mode)}
				<button
					type="button"
					role="menuitemradio"
					aria-checked={desktop.prefs.sort === mode}
					onclick={() => {
						savePrefs((p) => (p.sort = mode));
						menu = null;
					}}><span>{t(label)}</span></button
				>
			{/each}
		{:else if menu.kind === 'size'}
			{#each [['s', 'desktop.size.s'], ['m', 'desktop.size.m'], ['l', 'desktop.size.l']] as const as [size, label] (size)}
				<button
					type="button"
					role="menuitemradio"
					aria-checked={desktop.prefs.iconSize === size}
					onclick={() => {
						savePrefs((p) => (p.iconSize = size));
						menu = null;
					}}><span>{t(label)}</span></button
				>
			{/each}
		{/if}
	</ContextMenu>
{/if}

{#if dialog}<DesktopDialogs
		kind={dialog}
		onclose={() => (dialog = null)}
		onaddwidget={addWidget}
		kinds={WIDGET_KINDS}
	/>{/if}

<style>
	.desktop {
		position: fixed;
		inset: 0;
		display: flex;
		flex-direction: column;
		/* clip, not hidden: focusing something must never scroll the desktop away. */
		overflow: clip;
		color: var(--text);
		isolation: isolate;
	}
	.paper {
		position: absolute;
		inset: -4%;
		z-index: -1;
		background: var(--wallpaper);
		filter: hue-rotate(var(--tint));
	}
	.live .paper {
		animation: drift 90s ease-in-out infinite alternate;
	}
	/* Holding still while a window moves keeps the glass from being drawn anew each frame. */
	.moving .paper {
		animation-play-state: paused;
	}
	@keyframes drift {
		to {
			transform: translate(-2%, 1.5%) scale(1.04);
		}
	}
	:global(.glass-panel) {
		background: color-mix(in srgb, var(--surface) 70%, transparent);
		backdrop-filter: blur(22px) saturate(1.4);
		-webkit-backdrop-filter: blur(22px) saturate(1.4);
		border: 1px solid color-mix(in srgb, #fff 12%, transparent);
		box-shadow: 0 10px 30px rgb(0 0 0 / 0.3);
	}
	.topbar {
		height: calc(var(--pb) + 18px);
		flex: none;
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 0 10px;
		position: relative;
		z-index: 9500;
	}
	.pill {
		display: flex;
		align-items: center;
		gap: 4px;
		padding: 4px;
		border-radius: 12px;
		min-width: 0;
		overflow: hidden;
	}
	.pill.right {
		overflow: visible;
		flex: none;
	}
	.pb {
		width: var(--pb);
		height: var(--pb);
		flex: none;
		display: grid;
		place-items: center;
		border: 0;
		border-radius: 9px;
		background: none;
		color: var(--text);
		cursor: pointer;
	}
	.pb:hover {
		background: color-mix(in srgb, var(--text) 9%, transparent);
	}
	.pb.on {
		background: color-mix(in srgb, var(--accent) 24%, transparent);
	}
	.pb:focus-visible,
	.ico:focus-visible,
	.bubble:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.task.min {
		opacity: 0.6;
	}
	.task.on {
		box-shadow: inset 0 -2px 0 var(--accent);
	}
	.sep {
		width: 1px;
		height: 22px;
		background: var(--border);
		margin: 0 4px;
		flex: none;
	}
	.space {
		flex: 1;
	}
	.gauge {
		display: flex;
		align-items: center;
		gap: 6px;
		padding: 0 6px;
		font-size: max(10px, calc(var(--pb) * 0.32));
		line-height: 1.15;
		white-space: nowrap;
		font-variant-numeric: tabular-nums;
	}
	.desk-area {
		position: relative;
		flex: 1;
		min-height: 0;
		overflow: clip;
	}
	.icons {
		position: absolute;
		top: 8px;
		right: 0;
		bottom: 0;
		pointer-events: none;
	}
	.icons > .ico {
		position: absolute;
		pointer-events: auto;
		transition:
			left 0.18s ease,
			top 0.18s ease;
	}
	.ico {
		position: relative;
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 7px;
		padding: 6px 2px;
		border: 0;
		border-radius: 12px;
		background: none;
		color: #fff;
		font: inherit;
		font-size: var(--text-md);
		font-weight: 500;
		line-height: 1.2;
		text-align: center;
		text-shadow: 0 1px 3px rgb(0 0 0 / 0.65);
		cursor: pointer;
		height: fit-content;
	}
	.ico:hover {
		background: color-mix(in srgb, #fff 12%, transparent);
	}
	/* Light themes: dark names with a light halo, readable on light wallpapers. */
	.light .ico:not(.small) {
		color: var(--text);
		text-shadow:
			0 0 3px rgb(255 255 255 / 0.9),
			0 1px 2px rgb(255 255 255 / 0.8);
	}
	.ico.drop {
		box-shadow: inset 0 0 0 2px var(--accent);
	}
	:global(.menu-rule) {
		display: block;
		height: 1px;
		margin: 4px 6px;
		background: var(--border);
	}
	.ico .label {
		max-width: 100%;
		overflow-wrap: anywhere;
		display: -webkit-box;
		-webkit-line-clamp: 2;
		line-clamp: 2;
		-webkit-box-orient: vertical;
		overflow: hidden;
	}
	.ico-wrap {
		position: relative;
		display: inline-block;
	}
	.folder-ico {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 4px;
		padding: 8px;
		border-radius: 23%;
		background: color-mix(in srgb, #fff 20%, transparent);
		backdrop-filter: blur(10px);
		place-items: center;
	}
	.badge {
		position: absolute;
		top: -6px;
		right: -8px;
		min-width: 22px;
		height: 22px;
		padding: 0 6px;
		border-radius: 11px;
		display: grid;
		place-items: center;
		font-size: 12px;
		font-weight: 700;
		color: #fff;
		text-shadow: none;
		box-shadow: 0 0 0 2px rgb(0 0 0 / 0.35);
	}
	.badge.count {
		background: #d93a3f;
	}
	.badge.warn {
		background: #f0a91a;
		color: #2b2100;
	}
	.badge.dot {
		top: auto;
		bottom: -2px;
		right: -2px;
		min-width: 14px;
		width: 14px;
		height: 14px;
		padding: 0;
		background: #9aa7b6;
	}
	.badge.dot.on {
		background: #4fd67a;
	}
	.rename {
		width: 100%;
		font: inherit;
		font-size: var(--text-sm);
		border: 1px solid var(--accent);
		border-radius: 6px;
		background: var(--surface);
		color: var(--text);
		text-align: center;
	}
	.widget {
		position: absolute;
		border-radius: 18px;
		padding: 14px 16px;
		overflow: auto;
		scrollbar-width: thin;
		z-index: 1;
	}
	.widget.edit {
		outline: 2px dashed color-mix(in srgb, var(--accent) 70%, transparent);
		cursor: move;
		user-select: none;
	}
	.w-remove {
		position: absolute;
		top: 6px;
		right: 6px;
		width: 26px;
		height: 26px;
		display: grid;
		place-items: center;
		border: 0;
		border-radius: 50%;
		background: var(--surface);
		color: var(--text);
		cursor: pointer;
	}
	.w-size {
		position: absolute;
		right: 0;
		bottom: 0;
		width: 18px;
		height: 18px;
		border-radius: 4px 0 14px 0;
		background: var(--accent);
		cursor: nwse-resize;
	}
	.edit-bar {
		position: absolute;
		left: 50%;
		bottom: 18px;
		transform: translateX(-50%);
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: 10px;
		padding: 10px 14px;
		border-radius: 14px;
		z-index: 8000;
		max-width: calc(100% - 40px);
	}
	.edit-bar .btn {
		display: inline-flex;
		align-items: center;
		gap: 6px;
	}
	.snap-preview {
		position: absolute;
		z-index: 8500;
		border-radius: 16px;
		background: color-mix(in srgb, var(--accent) 18%, transparent);
		border: 2px solid color-mix(in srgb, var(--accent) 60%, transparent);
		backdrop-filter: blur(6px);
		pointer-events: none;
		transition: all 0.12s;
	}
	.file-drop {
		position: absolute;
		inset: 12px;
		z-index: 9800;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 10px;
		border: 3px dashed var(--accent);
		border-radius: 20px;
		background: color-mix(in srgb, var(--accent) 12%, transparent);
		font-size: var(--text-lg);
		pointer-events: none;
	}
	.folder-pop {
		position: absolute;
		left: 50%;
		top: 18%;
		transform: translateX(-50%);
		width: min(460px, calc(100% - 40px));
		border-radius: 22px;
		padding: 16px 18px;
		z-index: 8800;
	}
	.folder-head {
		display: flex;
		align-items: center;
		gap: 10px;
		margin-bottom: 12px;
	}
	.folder-head b {
		flex: 1;
		font-size: var(--text-lg);
	}
	.folder-grid {
		display: grid;
		grid-template-columns: repeat(4, 1fr);
		gap: 10px;
	}
	.ico.small {
		color: var(--text);
		text-shadow: none;
		font-size: var(--text-sm);
	}
	.link {
		border: 0;
		background: none;
		color: var(--accent);
		font: inherit;
		font-size: var(--text-sm);
		cursor: pointer;
	}
	.muted {
		color: var(--muted);
	}
	.switch-dim {
		position: absolute;
		inset: 0;
		background: rgb(0 0 0 / 0.35);
		z-index: 9600;
	}
	.switcher {
		position: absolute;
		left: 50%;
		top: 50%;
		transform: translate(-50%, -50%);
		z-index: 9700;
		display: flex;
		flex-wrap: wrap;
		justify-content: center;
		gap: 14px;
		padding: 20px;
		border-radius: 22px;
		max-width: calc(100% - 40px);
	}
	.thumb {
		width: 190px;
		padding: 8px;
		border: 0;
		border-radius: 14px;
		background: none;
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.thumb.on {
		outline: 3px solid var(--accent);
		background: color-mix(in srgb, var(--accent) 14%, transparent);
	}
	.mini {
		height: 110px;
		border-radius: 10px;
		background: var(--bg);
		border: 1px solid var(--border);
		padding: 8px;
		display: grid;
		grid-template-columns: 34px 1fr 1fr;
		grid-template-rows: 1fr 1fr;
		gap: 5px;
	}
	.mini span {
		background: var(--chip);
		border-radius: 4px;
	}
	.mini span:first-child {
		grid-row: 1 / 3;
		background: var(--surface);
	}
	.thumb-name {
		display: flex;
		align-items: center;
		gap: 8px;
		margin-top: 8px;
		min-width: 0;
	}
	.thumb-name b {
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		font-size: var(--text-sm);
	}
	.keys {
		flex-basis: 100%;
		margin: 4px 0 0;
		text-align: center;
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.bubble {
		position: absolute;
		right: 22px;
		bottom: 22px;
		width: 58px;
		height: 58px;
		border: 0;
		border-radius: 50%;
		display: grid;
		place-items: center;
		background: linear-gradient(160deg, #a78bfa, #6d3fe0);
		color: #fff;
		box-shadow:
			0 10px 30px rgb(109 63 224 / 0.55),
			0 0 0 6px color-mix(in srgb, #a78bfa 22%, transparent);
		cursor: pointer;
		z-index: 9300;
	}
	.bubble-pop {
		position: absolute;
		right: 22px;
		bottom: 92px;
		width: min(440px, calc(100% - 44px));
		height: min(620px, calc(100% - 110px));
		border-radius: 20px;
		display: flex;
		flex-direction: column;
		overflow: hidden;
		z-index: 9300;
		animation: pop 0.18s ease-out;
	}
	@keyframes pop {
		from {
			opacity: 0;
			transform: translateY(10px) scale(0.97);
		}
	}
	.bubble-head {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 8px 8px 8px 14px;
		border-bottom: 1px solid var(--border);
	}
	.bubble-pop iframe {
		flex: 1;
		border: 0;
		width: 100%;
	}
	@media (prefers-reduced-motion: reduce) {
		.live .paper,
		.bubble-pop {
			animation: none;
		}
		.snap-preview,
		.icons > .ico {
			transition: none;
		}
	}
</style>
