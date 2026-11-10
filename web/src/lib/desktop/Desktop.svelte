<script lang="ts">
	import MigrationDialog from '../webapps/MigrationDialog.svelte';
	// PLAN 61: the desktop mode – TitanOS as a desktop with programs in
	// windows, widgets and a main menu. It replaces the head line and side
	// bar on large screens; every page of TitanOS opens in a window.
	import { onMount, untrack } from 'svelte';
	import { afterNavigate } from '$app/navigation';
	import { page } from '$app/state';
	import { replaceState } from '$app/navigation';
	import Icon from '../components/Icon.svelte';
	import { t } from '../i18n/index.svelte.ts';
	import { session } from '../session.svelte.ts';
	import { appearance } from '../theme/appearance.svelte.ts';
	import { THEMES } from '../theme/themes.ts';
	import { lock } from '../lock.svelte.ts';
	import { appForPath, appAllowed } from './apps.ts';
	import Launcher from './Launcher.svelte';
	import NotifyCenter from './NotifyCenter.svelte';
	import LiveBackground from '../live/LiveBackground.svelte';
	import { takeEnter } from '../login/enter.ts';
	import { isLivePicture } from '../live/shaders.ts';
	import AgentBubble from './AgentBubble.svelte';
	import AppSwitcher from './AppSwitcher.svelte';
	import DesktopDialogs from './DesktopDialogs.svelte';
	import DesktopIcons from './DesktopIcons.svelte';
	import DesktopMenu from './DesktopMenu.svelte';
	import DesktopWidgets from './DesktopWidgets.svelte';
	import FolderPop from './FolderPop.svelte';
	import TopBar from './TopBar.svelte';
	import Window from './Window.svelte';
	import Dock from './Dock.svelte';
	import {
		arrangeIcons,
		dockPinned,
		placeIcons,
		dropIcon,
		placeWidget,
		WIDGET_KINDS,
		type WidgetKind,
	} from './prefs.ts';
	import { findProgram, openProgram, programs, type Program } from './programs.svelte.ts';
	import { watchSources } from './sources.svelte.ts';
	import { sendDockToWindows, sendFilesToFileManager, watchWindows } from './desk-messages.ts';
	import {
		autoArrange,
		CELL,
		menuAt,
		newFolder as makeFolder,
		setSort,
		showDesktop,
		sideOf,
		WIDGET_DIMS,
		widgetLeft,
		type DeskDialog,
		type DeskMenu,
	} from './desk-ops.ts';
	import { dayTint, wallpaperCss } from './wallpapers.ts';
	import {
		pullWindowsIn,
		byRecency,
		requestClose,
		desktop,
		focus,
		loadDesktop,
		minimize,
		savePrefs,
		setSnap,
		snapRect,
		wm,
	} from './wm.svelte.ts';
	import { deskKey, menuKeyDown, menuKeyReset, menuKeyUp, screenOwnsKeys } from './menubar/keys.ts';
	import type { DeskOp } from './menubar/menus.ts';

	const onlock = () => void lock();

	let area = $state<HTMLElement>();
	let launcher = $state(false);
	/** Where the main menu opens: under the top bar, or at the dock's menu button. */
	let launcherAt = $state<'top' | 'bottom' | 'left' | 'right'>('top');
	let center = $state(false);
	let editWidgets = $state(false);
	let editDock = $state(false);
	let dialog = $state<DeskDialog | null>(null);
	let openFolder = $state<string | null>(null);
	let menu = $state<DeskMenu | null>(null);
	let renaming = $state<string | null>(null);
	let dragKey = $state<string | null>(null);
	let dropBefore = $state<string | null>(null);
	// PLAN 66: while a program is dragged over the desktop the grid shows, with the cell it would land in.
	let programDrag = $state(false);
	let dragCell = $state<{ c: number; r: number } | null>(null);
	function endIconDrag() {
		dragKey = null;
		dropBefore = null;
		programDrag = false;
		dragCell = null;
	}
	let fileDrop = $state(false);

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
	const known = $derived(new Set(all.map((p) => p.key)));
	const inDock = $derived(new Set(dockPinned(desktop.prefs, known)));

	// The dock keeps its edge free: snapped windows and icons stay clear of it.
	// Plate (icon + 2 × 8 px padding + slot and border, ~size + 27) + 10 px to the edge + a gap.
	const dockRoom = $derived(desktop.prefs.dock.size + 46);
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
	// PLAN 66 (owner): the rows reach down to the dock – as many as fit at
	// their nominal height, stretched evenly over the room; icons sit on the
	// same stretched rows, so cells and icons stay aligned.
	const colW = $derived(iconPx + 60);
	const ICON_LEFT = 18;
	const ICON_TOP = 8;
	const rowRoom = $derived(Math.max(1, wm.area.h - ICON_TOP - 8 - wm.inset.b));
	const gridRows = $derived(Math.max(1, Math.floor(rowRoom / (iconPx + 56))));
	const rowH = $derived(Math.max(iconPx + 56, Math.floor(rowRoom / gridRows)));
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

	// The wallpaper: built in (light/dark by the theme) or the account's own picture.
	let tint = $state(dayTint());
	$effect(() => {
		const timer = setInterval(() => (tint = dayTint()), 10 * 60 * 1000);
		return () => clearInterval(timer);
	});
	const livePicture = $derived(
		desktop.prefs.wallpaper.source === 'builtin' && isLivePicture(desktop.prefs.wallpaper.id)
			? desktop.prefs.wallpaper.id
			: null,
	);
	// LiveBackground holds still by itself with "reduce motion".
	const liveOn = $derived(desktop.prefs.wallpaper.live);
	// PLAN 74: right after a login the chosen animation plays once (lib/login/enter.ts).
	const entering = takeEnter();
	const firstEnter =
		entering && !matchMedia('(prefers-reduced-motion: reduce)').matches ? entering.kind : '';
	let enterKind = $state(firstEnter);
	if (firstEnter) setTimeout(() => (enterKind = ''), 1600);
	const wallpaper = $derived(
		desktop.prefs.wallpaper.source === 'custom'
			? `url("/api/v1/background?v=${desktop.prefs.wallpaper.version}") center / cover`
			: `${wallpaperCss(desktop.prefs.wallpaper.id, light)} center / cover`,
	);

	// A link clicked on the desktop itself (the bell's "Details öffnen", the
	// tasks dialog …) changes the address without a reload: open its window
	// too. Only clicked links – the desktop's own address updates never come here.
	afterNavigate((nav) => {
		if (nav.type !== 'link' || !nav.to) return;
		const url = nav.to.url;
		if (url.pathname === '/') return;
		const app = appForPath(url.pathname);
		if (app && appAllowed(app, role, session.services))
			void openProgram('app:' + app.id, { href: url.pathname + url.search + url.hash });
	});

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
		let measured = false;
		const measure = () => {
			if (!area) return;
			const narrower = !measured || area.clientWidth < wm.area.w;
			measured = true;
			wm.area = { w: area.clientWidth, h: area.clientHeight };
			// PLAN 76 (F-12-11): a smaller screen pulls the windows back in, buttons included.
			if (narrower) pullWindowsIn();
		};
		measure();
		const observer = new ResizeObserver(measure);
		if (area) observer.observe(area);
		const unwatch = watchWindows(
			() => $state.snapshot(desktop.prefs.dock),
			() => (dialog = 'wallpaper'),
		);
		return () => {
			unwatch();
			stop();
			observer.disconnect();
		};
	});

	// Open settings windows follow changes made on the dock itself (right click).
	$effect(() => {
		JSON.stringify(desktop.prefs.dock);
		untrack(() => sendDockToWindows($state.snapshot(desktop.prefs.dock)));
	});

	// The address of the browser follows the window in front.
	function address(href: string) {
		// Without a jump mark: "/settings/access#access-ssh" (a hit of the side
		// bar search, PLAN 71) never equals the address and was written again
		// and again – the page hung and its memory grew (found 7.11.2026).
		const path = href.split('#')[0];
		if (location.pathname + location.search === path) return;
		try {
			replaceState(path, page.state);
		} catch {
			// Before the router is ready the address stays as it is.
		}
	}
	$effect(() => {
		const front = wm.windows.find((w) => w.id === wm.focused && !w.minimized);
		if (!front || front.app.startsWith('docker:') || front.app.startsWith('webapp:')) address('/');
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
		endIconDrag();
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
		const shown = new Map(places);
		savePrefs((p) => {
			if (!iconsNow().includes(key)) p.icons = [...iconsNow(), key];
			else p.icons = iconsNow();
			p.customIcons = true;
			p.folders = p.folders.map((f) => ({ ...f, apps: f.apps.filter((a) => a !== key) }));
			// Onto another icon: the two change places; onto free space: it stays there.
			const other = target ?? [...shown].find(([, at]) => at.c === cell.c && at.r === cell.r)?.[0];
			const to = target ? shown.get(target) : cell;
			if (!to) return;
			// Only the moved icon (and the one it swaps with) change their stored place (PLAN 65-10).
			p.iconPos = dropIcon(p, shown, key, to, other);
			p.sort = 'free';
		});
	}
	function newFolder() {
		renaming = makeFolder(iconsNow);
	}

	// Files from the computer dropped on the desktop go to the file manager's upload.
	function dropFiles(event: DragEvent) {
		const files = [...(event.dataTransfer?.files ?? [])];
		fileDrop = false;
		if (!files.length) return false;
		event.preventDefault();
		sendFilesToFileManager(files);
		return true;
	}

	// Widgets: placed freely on a grid of 20 px; negative x counts from the right edge.
	const columns = $derived(Math.max(10, Math.floor(wm.area.w / CELL)));
	// PLAN 66: the cells shown while dragging a program – not those under a widget.
	const dragCells = $derived.by(() => {
		if (!programDrag && !dragKey) return [];
		const boxes = (desktop.prefs.widgetsHidden ? [] : desktop.prefs.widgets).map((w) => ({
			l: widgetLeft(w, columns),
			t: w.y * CELL,
			r: widgetLeft(w, columns) + w.w * CELL - 10,
			b: w.y * CELL + w.h * CELL - 10,
		}));
		const used = new Set([...places.values()].map((at) => at.c + ':' + at.r));
		const out: { c: number; r: number }[] = [];
		for (let r = 0; r < gridRows; r++)
			for (let c = 0; c < gridCols; c++) {
				const l = ICON_LEFT + wm.inset.l + c * colW;
				const t = ICON_TOP + r * rowH;
				const covered = boxes.some(
					(b) => l < b.r && l + colW - 4 > b.l && t < b.b && t + rowH - 4 > b.t,
				);
				if (!covered || used.has(c + ':' + r)) out.push({ c, r });
			}
		return out;
	});
	function addWidget(kind: WidgetKind, size: 's' | 'm' | 'l') {
		const dims = WIDGET_DIMS[size];
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

	// The switcher (Alt+Q, Alt+Tab belongs to the operating system) and window keys.
	function keys(event: KeyboardEvent) {
		if (menuKeyDown(event)) {
			event.preventDefault();
			topBar?.focusBar();
			return;
		}
		// The window passes its page's keys on with the target lost: deskKey ran there already.
		const k = deskKey(event);
		if (k) {
			// PLAN 67-1: Alt + K shows the desktop (Alt + D belongs to the browser's address bar).
			if (k === 'k') {
				if (screenOwnsKeys(event.target)) return;
				event.preventDefault();
				showDesktop();
				return;
			}
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
				requestClose(front);
			} else if (k === 'm') {
				event.preventDefault();
				minimize(front);
			} else if (k === 'ArrowLeft' || k === 'ArrowRight' || k === 'ArrowUp' || k === 'ArrowDown') {
				event.preventDefault();
				setSnap(
					front,
					k === 'ArrowLeft'
						? 'left'
						: k === 'ArrowRight'
							? 'right'
							: k === 'ArrowUp'
								? 'max'
								: null,
				);
			}
		}
		if ((event.ctrlKey || event.metaKey) && event.code === 'Space') {
			event.preventDefault();
			launcherAt = 'top';
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

	// PLAN 65-5: when the menu or the message centre closes and the focus went
	// nowhere, it returns to the button that opened it.
	let wasOpen = { launcher: false, center: false };
	$effect(() => {
		const now = { launcher, center };
		for (const kind of ['launcher', 'center'] as const)
			if (wasOpen[kind] && !now[kind])
				setTimeout(() => {
					const active = document.activeElement;
					if (active && active !== document.body) return;
					document.querySelector<HTMLElement>(`[data-opens="${kind}"]`)?.focus();
				}, 0);
		wasOpen = now;
	});

	// Menu and message centre close on a click beside them (also into a window's frame).
	function outside(event: PointerEvent) {
		menuKeyReset();
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
		if (menuKeyUp(event) && !wm.switcher) {
			event.preventDefault();
			topBar?.focusBar();
		}
		if (wm.switcher && (event.key === 'Alt' || !event.altKey)) {
			const target = byRecency()[wm.switcherIndex];
			wm.switcher = false;
			if (target) focus(target.id);
		}
	}

	// PLAN 67-1: the menu bar of the top bar; Alt alone or F10 puts the keyboard there.
	let topBar = $state<TopBar>();
	function deskOp(op: DeskOp) {
		if (op === 'newFolder') newFolder();
		else if (op === 'wallpaper') dialog = 'wallpaper';
		else if (op === 'autoArrange') autoArrange();
		else if (op === 'widgets') savePrefs((p) => (p.widgetsHidden = !p.widgetsHidden));
		else if (op === 'dock') savePrefs((p) => (p.dock.show = !p.dock.show));
		else if (op.startsWith('size:')) {
			const size = op.slice(5) as 's' | 'm' | 'l';
			savePrefs((p) => (p.iconSize = size));
		} else if (op.startsWith('sort:')) {
			setSort(op.slice(5) as 'name' | 'kind' | 'recent' | 'free', () => places);
		}
	}

	function programOf(app: string): Program | undefined {
		return byKey.get(app) ?? findProgram(app);
	}

	const reduced = matchMedia('(prefers-reduced-motion: reduce)').matches;
</script>

<svelte:window onkeydown={keys} onkeyup={keyUp} onpointerdowncapture={outside} onblur={blurred} />

<div
	class:light
	class:live={desktop.prefs.wallpaper.live && !reduced}
	class:moving={wm.dragging}
	class="desktop enter-{enterKind || 'none'}"
	data-cut="frame"
	style:--wallpaper={wallpaper}
	style:--tint="{tint}deg"
	style:--icon={iconPx + 'px'}
	style:--bar-h={{ s: 34, m: 40, l: 48 }[desktop.prefs.barSize] + 'px'}
	style:--pb={{ s: 26, m: 32, l: 40 }[desktop.prefs.barSize] + 'px'}
>
	{#if livePicture}
		<!-- PLAN 74: a photo that moves (aurora, water); the drift of .paper is not needed then. -->
		<div class="paper photo" aria-hidden="true">
			<LiveBackground picture={livePicture} moving={liveOn} paused={wm.dragging} />
		</div>
	{:else}
		<div class="paper" aria-hidden="true"></div>
	{/if}
	{#if enterKind === 'curtain' || enterKind === 'split'}
		<!-- The login picture leaves: upwards, or to both sides. -->
		<div class="enter-cover {enterKind}" aria-hidden="true">
			<span style:background-image={entering?.picture ? `url("${entering.picture}")` : undefined}
			></span>
			{#if enterKind === 'split'}<span
					style:background-image={entering?.picture ? `url("${entering.picture}")` : undefined}
				></span>{/if}
		</div>
	{/if}
	<TopBar bind:this={topBar} bind:menu bind:launcher bind:launcherAt bind:center {role} {deskOp} />

	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<div
		class="desk-area"
		bind:this={area}
		style:--dock-room="{dockRoom}px"
		ondragover={(e) => {
			e.preventDefault();
			if (e.dataTransfer?.types.includes('Files')) fileDrop = true;
			if (dragKey || e.dataTransfer?.types.includes('text/x-titanos-program')) {
				programDrag = true;
				dropBefore = null;
				const cell = cellAt(e.clientX, e.clientY);
				if (dragCell?.c !== cell.c || dragCell?.r !== cell.r) dragCell = cell;
			}
		}}
		ondragleave={(e) => {
			if (e.target === area) fileDrop = false;
			// Left the desktop (not only moved onto an icon on it).
			if (!e.relatedTarget || !area?.contains(e.relatedTarget as Node)) {
				programDrag = false;
				dragCell = null;
			}
		}}
		ondrop={(e) => {
			if (!dropFiles(e)) dropOn(e, null);
		}}
		oncontextmenu={(e) => {
			if (e.target !== area && !(e.target as HTMLElement).classList.contains('icons')) return;
			editDock = false;
			e.preventDefault();
			menu = { ...menuAt(e), kind: 'desk' };
		}}
	>
		<DesktopIcons
			bind:menu
			bind:renaming
			bind:dragKey
			bind:dropBefore
			bind:programDrag
			bind:dragCell
			{icons}
			{byKey}
			{places}
			{dragCells}
			{colW}
			{rowH}
			{iconPx}
			left={ICON_LEFT + wm.inset.l}
			{open}
			{dropOn}
			{endIconDrag}
		/>

		<DesktopWidgets bind:menu bind:editWidgets bind:dialog {columns} {open} />

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
						? { ...menuAt(e), kind: 'icon', key, dock: true }
						: { ...menuAt(e), kind: 'dock' })}
				ondone={() => (editDock = false)}
				{launcher}
				onlauncher={() => {
					launcherAt = desktop.prefs.dock.position;
					launcher = !launcher;
				}}
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

		{#if openFolder}<FolderPop bind:openFolder bind:renaming {byKey} {iconsNow} {open} />{/if}

		{#if launcher}<Launcher
				at={launcherAt}
				onopen={(key, href) => open(key, undefined, href)}
				onclose={() => (launcher = false)}
				{onlock}
			/>{/if}
		{#if center}<NotifyCenter
				onclose={() => (center = false)}
				onopen={(key, href) => open(key, undefined, href)}
			/>{/if}

		<AppSwitcher {programOf} />

		{#if agentAllowed}<AgentBubble {open} />{/if}
	</div>
</div>

<DesktopMenu
	bind:menu
	bind:openFolder
	bind:renaming
	bind:editWidgets
	bind:editDock
	bind:dialog
	{byKey}
	{role}
	{inDock}
	{known}
	{icons}
	{places}
	{iconsNow}
	{open}
	{newFolder}
/>

{#if dialog}<DesktopDialogs
		kind={dialog}
		onclose={() => (dialog = null)}
		onaddwidget={addWidget}
		kinds={WIDGET_KINDS}
	/>{/if}

<!-- PLAN 66: once after the update to 0.63, for owners and administrators. -->
<MigrationDialog />

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
		/* "Farbstärke" (Settings › Appearance, PLAN 64) reaches the wallpaper too. */
		filter: hue-rotate(var(--tint)) var(--wallpaper-filter, saturate(1));
	}
	/* PLAN 74: the way into the desktop after a login. */
	.enter-zoom {
		animation: enter-zoom 0.9s cubic-bezier(0.2, 0.8, 0.2, 1) both;
	}
	@keyframes enter-zoom {
		from {
			opacity: 0;
			transform: scale(1.1);
		}
	}
	.enter-focus {
		animation: enter-focus 1.1s ease both;
	}
	@keyframes enter-focus {
		from {
			filter: blur(28px) brightness(0.7);
		}
	}
	.enter-cover {
		position: fixed;
		inset: 0;
		z-index: 2147483000;
		pointer-events: none;
		display: flex;
	}
	.enter-cover span {
		flex: 1;
		background: #0b0f16 center / cover no-repeat;
	}
	.enter-cover.curtain span {
		animation: enter-up 0.9s cubic-bezier(0.7, 0, 0.2, 1) 0.1s forwards;
	}
	.enter-cover.split span:first-child {
		background-position: left center;
		background-size: 200% 100%;
		animation: enter-left 0.9s cubic-bezier(0.7, 0, 0.2, 1) 0.1s forwards;
	}
	.enter-cover.split span:last-child {
		background-position: right center;
		background-size: 200% 100%;
		animation: enter-right 0.9s cubic-bezier(0.7, 0, 0.2, 1) 0.1s forwards;
	}
	@keyframes enter-up {
		to {
			transform: translateY(-100%);
		}
	}
	@keyframes enter-left {
		to {
			transform: translateX(-100%);
		}
	}
	@keyframes enter-right {
		to {
			transform: translateX(100%);
		}
	}
	.live .paper:not(.photo) {
		animation: drift 90s ease-in-out infinite alternate;
	}
	.paper.photo {
		inset: 0;
		background: none;
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
	/* Follows "Transparenz" and "Glaseffekt" (Settings › Appearance, PLAN 64). */
	:global(.glass-panel) {
		background: var(--desk-surface, color-mix(in srgb, var(--surface) 70%, transparent));
		-webkit-backdrop-filter: blur(var(--desk-blur, 22px)) saturate(1.4);
		backdrop-filter: blur(var(--desk-blur, 22px)) saturate(1.4);
		border: 1px solid color-mix(in srgb, #fff 12%, transparent);
		box-shadow: 0 10px 30px rgb(0 0 0 / 0.3);
	}
	.desk-area {
		position: relative;
		flex: 1;
		min-height: 0;
		overflow: clip;
		/* Windows stack inside the desktop; the top bar's menus stay above them all. */
		isolation: isolate;
	}
	:global(.menu-rule) {
		display: block;
		height: 1px;
		margin: var(--space-1) var(--space-1-5);
		background: var(--border);
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
		gap: var(--space-2-5);
		border: 3px dashed var(--accent);
		border-radius: 20px;
		background: color-mix(in srgb, var(--accent) 12%, transparent);
		font-size: var(--text-lg);
		pointer-events: none;
	}
	@media (prefers-reduced-motion: reduce) {
		.live .paper {
			animation: none;
		}
		.snap-preview {
			transition: none;
		}
	}
</style>
