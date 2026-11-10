<script lang="ts">
	import { onDestroy, tick, untrack } from 'svelte';
	import { focusFirstItem, moveFocus } from '../components/menu.ts';
	// PLAN 61: one window of the desktop. TitanOS programs show their pages
	// in a frame of the same origin with the program's own side bar; Docker
	// apps always show their remote view (PLAN 63: the app runs in a browser
	// on the NAS, its picture comes over the TitanOS address).
	import Icon from '../components/Icon.svelte';
	import { t } from '../i18n/index.svelte.ts';
	import DesktopIcon from './DesktopIcon.svelte';
	import { pageDrawsSide, sideActive, type SideItem } from './apps.ts';
	import { session } from '../session.svelte.ts';
	import { settingsSideAllowed, settingsSideMatches } from '../settings-search.ts';
	import { frameMessage, type FrameMessage } from './frame.ts';
	import { openProgram, openTab as openProgramTab } from './programs.svelte.ts';
	import Dialog from '../components/Dialog.svelte';
	import '../components/menu.css';
	import WebAppView from '../webapps/WebAppView.svelte';
	import { webAppById, webapps } from '../webapps/webapps.svelte.ts';
	import { baseName } from '../webapps/protocol.ts';
	import { api } from '../api.ts';
	import { registerWindowCommands } from '../menu-commands.svelte.ts';
	import type RemoteViewType from './RemoteView.svelte';
	import { menuKeyReset, screenOwnsKeys } from './menubar/keys.ts';
	import { clearFrameCommands } from './menubar/state.svelte.ts';
	import { sources } from './sources.svelte.ts';
	import DirectView from './DirectView.svelte';
	import { chooseDirect, directChosen, hasOwnStream } from './direct.ts';
	import {
		closeWindow,
		requestClose,
		desktop,
		focus,
		minimize,
		frameListening,
		navigateWindow,
		openWindow,
		registerFrame,
		saveWindows,
		savePrefs,
		setSnap,
		snapAt,
		snapRect,
		clearOfDock,
		toggleMax,
		wm,
		type Win,
	} from './wm.svelte.ts';

	let {
		win,
		glyph,
		gradient,
		image = '',
		side = [],
		external = false,
		foldKey,
		onaddress,
	}: {
		win: Win;
		glyph: string;
		gradient: string;
		image?: string;
		side?: SideItem[];
		external?: boolean;
		foldKey: string;
		onaddress?: (path: string) => void;
	} = $props();

	// PLAN 66: a web app running in the browser (its window has no address and no side bar).
	const webappId = $derived(win.app.startsWith('webapp:') ? win.app.slice(7) : '');
	const webapp = $derived(webappId ? webAppById(webappId) : undefined);
	let view = $state<WebAppView>();
	let moreOpen = $state(false);
	let remoteHost = $state<HTMLElement | null>(null);
	let moreButton = $state<HTMLButtonElement>();
	let menuEl = $state<HTMLElement>();
	// The ⋯ menu by keyboard like every menu of TitanOS: arrows, Escape back to ⋯.
	async function toggleMore() {
		moreOpen = !moreOpen;
		if (!moreOpen) return;
		await tick();
		focusFirstItem(menuEl);
	}
	function menuKey(e: KeyboardEvent) {
		if (e.key === 'Escape') {
			e.preventDefault();
			e.stopPropagation();
			moreOpen = false;
			moreButton?.focus();
			return;
		}
		if ((e.target as HTMLElement).matches?.('[role^="menuitem"]')) moveFocus(menuEl, e);
	}
	let restartAsk = $state(false);
	const docName = $derived(win.docName || (win.doc ? baseName(win.doc.path) : ''));
	const closing = $derived(wm.closing.includes(win.id));

	const MIN_W = 360;
	const MIN_H = 240;
	let frame = $state<HTMLIFrameElement>();
	// The frame loads its first page once; later pages are reached inside it.
	const src = untrack(() => win.href);
	let dragging = $state(false);
	let shift = $state({ x: 0, y: 0 });

	$effect(() => {
		registerFrame(win.id, frame ?? null);
		return () => registerFrame(win.id, null);
	});

	// PLAN 65-6: a minimized window's page pauses its polling (lib/embedded.ts).
	function tellVisible() {
		frame?.contentWindow?.postMessage(
			{ type: 'titanos-visible', visible: !win.minimized },
			location.origin,
		);
	}
	$effect(() => {
		void win.minimized;
		tellVisible();
	});

	// PLAN 65-5: keyboard focus goes into a window when it opens (or comes to
	// the front) and returns to the desktop when it closes.
	let section = $state<HTMLElement>();
	function takeFocus() {
		if (!focused || win.minimized) return;
		const active = document.activeElement;
		if (active instanceof HTMLIFrameElement && active !== frame) return;
		if (section?.contains(active)) return;
		if (frame) frame.focus();
		else section?.focus();
	}
	// The desktop's keys (Alt+Q switcher, Alt+W/M, Alt+arrows, Ctrl+Space) reach
	// the desktop at once while the focus is in the window's page (same origin),
	// before the page sees them; releasing Alt ends the switcher there too.
	function hookKeys() {
		const inner = frame?.contentWindow;
		if (!inner) return;
		const pass = (type: 'keydown' | 'keyup', e: KeyboardEvent) =>
			window.dispatchEvent(
				new KeyboardEvent(type, {
					key: e.key,
					code: e.code,
					altKey: e.altKey,
					ctrlKey: e.ctrlKey,
					shiftKey: e.shiftKey,
					metaKey: e.metaKey,
				}),
			);
		inner.addEventListener(
			'keydown',
			(e) => {
				// PLAN 67-1: Alt alone and F10 lead into the menu bar (not from a screen or terminal).
				if (e.key === 'Alt' || e.key === 'F10') {
					if (screenOwnsKeys(e.target) || (e.key === 'F10' && (e.shiftKey || e.ctrlKey))) {
						menuKeyReset();
						return;
					}
					if (e.key === 'F10') e.preventDefault();
					pass('keydown', e);
					return;
				}
				menuKeyReset();
				// Alt + K (show the desktop) stays with a screen or terminal (readline, guests).
				if (e.altKey && e.code === 'KeyK' && screenOwnsKeys(e.target)) return;
				const desk =
					(e.altKey && !e.ctrlKey && !e.metaKey && /^(q|w|m|k|arrow)/i.test(e.key)) ||
					((e.ctrlKey || e.metaKey) && e.code === 'Space');
				if (!desk) return;
				e.preventDefault();
				e.stopImmediatePropagation();
				pass('keydown', e);
			},
			true,
		);
		inner.addEventListener(
			'keyup',
			(e) => (wm.switcher || e.key === 'Alt') && pass('keyup', e),
			true,
		);
		inner.addEventListener('pointerdown', menuKeyReset, true);
	}
	// PLAN 69: a page that draws the side bar itself (the file manager's tree)
	// learns whether it is folded; folding stays one setting of the desktop.
	function tellSide() {
		frame?.contentWindow?.postMessage(
			{ type: 'titanos-side', folded } satisfies FrameMessage,
			location.origin,
		);
	}
	$effect(() => {
		void folded;
		untrack(tellSide);
	});
	function loaded() {
		tellVisible();
		tellSide();
		// PLAN 67-1: the new page announces its menu commands; ask in case it already did.
		clearFrameCommands(win.id);
		frame?.contentWindow?.postMessage({ type: 'titanos-menu-get' }, location.origin);
		try {
			hookKeys();
		} catch {
			// A page of another origin (never ours): its keys stay its own.
		}
		takeFocus();
	}
	$effect(() => {
		if (focused && !win.minimized) untrack(takeFocus);
	});
	onDestroy(() => {
		setTimeout(() => {
			if (document.activeElement && document.activeElement !== document.body) return;
			const next =
				document.querySelector<HTMLElement>('section.win.focused iframe') ??
				document.querySelector<HTMLElement>('section.win.focused') ??
				document.querySelector<HTMLElement>('[data-opens="launcher"]');
			next?.focus();
		}, 0);
	});

	const folded = $derived(desktop.prefs.folded.includes(foldKey));
	function toggleFold() {
		savePrefs((p) => {
			p.folded = folded ? p.folded.filter((k) => k !== foldKey) : [...p.folded, foldKey];
		});
	}

	const rect = $derived(
		snapRect(win.snap) ?? clearOfDock({ x: win.x, y: win.y, w: win.w, h: win.h }),
	);
	const focused = $derived(wm.focused === win.id);
	const path = $derived(win.href.split('?')[0]);
	// PLAN 69: on these pages the page itself draws the one side bar.
	const ownSide = $derived(pageDrawsSide(side, path));

	// PLAN 71 (owner's decision "Variante C"): Systemsteuerung has one menu,
	// its side bar. A search above it filters the entries by name and the
	// keywords of the settings search ("ssh" → Zugang); a hit in a section
	// shows its area and opens the section. Accounts with the role "user" see
	// only the areas they may open. Other programs keep their side bar as is.
	const searchable = $derived(win.app === 'app:settings');
	let query = $state('');
	let hit = $state(0);
	let searchEl = $state<HTMLInputElement>();
	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));
	type Link = Extract<SideItem, { label: unknown }>;
	const isLink = (item: SideItem): item is Link => 'label' in item && 'href' in item;
	const matches = $derived(
		searchable && query.trim() ? settingsSideMatches(query, admin, t, side.filter(isLink)) : null,
	);
	const shownSide = $derived.by(() => {
		if (!searchable) return side;
		const kept = side.filter((item) =>
			isLink(item)
				? settingsSideAllowed(item.href, admin) && (!matches || matches.has(item.href))
				: true,
		);
		// A group title only above entries of its own.
		return kept.filter(
			(item, i) => !('group' in item) || (kept[i + 1] && !('group' in kept[i + 1])),
		);
	});
	const links = $derived(matches ? shownSide.filter(isLink) : []);
	const current = $derived(links.length ? Math.min(hit, links.length - 1) : -1);
	function openSide(item: Link) {
		navigateWindow(win.id, matches?.get(item.href) ?? item.href);
	}
	function searchKey(e: KeyboardEvent) {
		if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
			if (!links.length) return;
			e.preventDefault();
			hit = (Math.max(current, 0) + (e.key === 'ArrowDown' ? 1 : -1) + links.length) % links.length;
			document.getElementById(`side-${win.id}-${hit}`)?.scrollIntoView({ block: 'nearest' });
		} else if (e.key === 'Enter') {
			const item = links[current];
			if (!item) return;
			e.preventDefault();
			openSide(item);
		} else if (e.key === 'Escape' && query) {
			e.preventDefault();
			e.stopPropagation();
			query = '';
			hit = 0;
		}
	}
	/** Ablage › Einstellungen suchen, and the magnifier of the folded side bar. */
	async function focusSearch() {
		if (folded) toggleFold();
		await tick();
		searchEl?.focus();
		searchEl?.select();
	}

	// PLAN 64: the title bar names the program and the page ("Docker › Container"):
	// the page's own title, or the entry of the side bar when the page is
	// called like its program.
	let pageName = $state('');
	const crumb = $derived.by(() => {
		if (external) return '';
		const vm = new URLSearchParams(win.href.split('?')[1] ?? '').get('vm');
		const thing = side.find(
			(item) =>
				'text' in item && !!vm && new URLSearchParams(item.href.split('?')[1]).get('vm') === vm,
		);
		if (thing && 'text' in thing) return thing.text;
		const label = (exact: boolean) => {
			const item = side.find(
				(i) =>
					'label' in i &&
					!('text' in i) &&
					(exact ? (i.match ?? [i.href.split('?')[0]]).includes(path) : sideActive(i, path, side)),
			);
			const text = item && 'label' in item ? t(item.label) : '';
			return text === win.title ? '' : text;
		};
		// The entry of the side bar for exactly this page, else the page's own
		// title (a page below an entry, e.g. one app), else the entry above it.
		const own = pageName.trim();
		return label(true) || (own && own !== win.title ? own : '') || label(false);
	});

	// The frame says which page it shows; the address of the browser follows the window in front.
	function message(event: MessageEvent) {
		if (!frame || event.source !== frame.contentWindow) return;
		const msg = frameMessage(event);
		if (msg?.type === 'titanos-page') {
			pageName = msg.title;
			// A page chosen in the side bar while this one still loaded comes next.
			const path = frameListening(win.id) ?? msg.path;
			win.href = path;
			if (focused) onaddress?.(path);
			saveWindows();
		} else if (msg?.type === 'titanos-side-get') {
			tellSide();
		} else if (msg?.type === 'titanos-side-fold') {
			toggleFold();
		} else if (msg?.type === 'titanos-console') {
			// A console asked for by this page opens in the Terminal window (PLAN 62-5).
			const href = '/terminal?open=' + encodeURIComponent(JSON.stringify(msg.session));
			const term = wm.windows.find((w) => w.app === 'app:terminal');
			if (term) {
				navigateWindow(term.id, href);
				focus(term.id);
			} else openWindow('app:terminal', href, t('desktop.app.terminal'), { multi: true });
		} else if (msg?.type === 'titanos-key') {
			window.dispatchEvent(
				new KeyboardEvent('keydown', {
					key: msg.key,
					altKey: msg.alt,
					ctrlKey: msg.ctrl,
					shiftKey: msg.shift,
					metaKey: msg.meta,
				}),
			);
		}
	}
	$effect(() => {
		if (focused && !external && !webappId) onaddress?.(win.href);
	});

	function restartApp() {
		moreOpen = false;
		if (view?.isUnsaved()) restartAsk = true;
		else view?.restart();
	}

	// Moving: from the title bar; a snapped window comes loose under the pointer.
	function startMove(event: PointerEvent) {
		if (event.button !== 0 || (event.target as HTMLElement).closest('button, a, input')) return;
		focus(win.id);
		const start = { px: event.clientX, py: event.clientY, ...rect };
		if (win.snap) {
			const ratio = (event.clientX - rect.x) / rect.w;
			win.w = Math.min(win.w, wm.area.w);
			start.x = event.clientX - ratio * win.w;
			start.w = win.w;
			start.h = win.h;
		}
		const bounds = (event.currentTarget as HTMLElement)
			.closest('.desk-area')
			?.getBoundingClientRect();
		let moved = false;
		let frameReq = 0;
		let last: PointerEvent | null = null;
		// The window follows the pointer by a transform (no new layout per frame);
		// its place is written once, when the pointer lets go.
		const apply = () => {
			frameReq = 0;
			const e = last;
			if (!e) return;
			const dx = e.clientX - start.px;
			const dy = e.clientY - start.py;
			const x = Math.round(start.x + dx);
			const y = Math.max(0, Math.round(start.y + dy));
			shift = { x: x - start.x, y: y - start.y };
			const snap = bounds ? snapAt(e.clientX - bounds.left, e.clientY - bounds.top) : null;
			if (snap !== wm.preview) wm.preview = snap;
		};
		const move = (e: PointerEvent) => {
			const dx = e.clientX - start.px;
			const dy = e.clientY - start.py;
			if (!moved && Math.abs(dx) + Math.abs(dy) < 4) return;
			if (!moved) {
				moved = true;
				dragging = true;
				wm.dragging = true;
				if (win.snap) {
					// Comes loose under the pointer at its own size.
					Object.assign(win, { x: Math.round(start.x), y: start.y, snap: null });
					start.x = win.x;
				}
			}
			last = e;
			if (!frameReq) frameReq = requestAnimationFrame(apply);
		};
		const up = () => {
			window.removeEventListener('pointermove', move);
			window.removeEventListener('pointerup', up);
			if (frameReq) cancelAnimationFrame(frameReq);
			if (moved) {
				apply();
				win.x = start.x + shift.x;
				win.y = start.y + shift.y;
				// Let go over the dock: the window stays above it (PLAN 71).
				if (!wm.preview) Object.assign(win, clearOfDock(win));
				shift = { x: 0, y: 0 };
			}
			dragging = false;
			wm.dragging = false;
			if (wm.preview) setSnap(win.id, wm.preview);
			wm.preview = null;
			saveWindows();
		};
		window.addEventListener('pointermove', move);
		window.addEventListener('pointerup', up);
	}

	// Resizing at every edge and corner.
	function startResize(event: PointerEvent, edge: string) {
		if (event.button !== 0) return;
		event.preventDefault();
		event.stopPropagation();
		focus(win.id);
		const start = { px: event.clientX, py: event.clientY, ...rect };
		win.snap = null;
		Object.assign(win, { x: start.x, y: start.y, w: start.w, h: start.h });
		dragging = true;
		wm.dragging = true;
		const move = (e: PointerEvent) => {
			const dx = e.clientX - start.px;
			const dy = e.clientY - start.py;
			if (edge.includes('e')) win.w = Math.max(MIN_W, start.w + dx);
			if (edge.includes('s'))
				win.h = Math.max(MIN_H, Math.min(start.h + dy, wm.area.h - wm.inset.b - start.y));
			if (edge.includes('w')) {
				const w = Math.max(MIN_W, start.w - dx);
				win.x = start.x + start.w - w;
				win.w = w;
			}
			if (edge.includes('n')) {
				const h = Math.max(MIN_H, start.h - dy);
				const y = Math.max(0, start.y + start.h - h);
				win.h = start.y + start.h - y;
				win.y = y;
			}
		};
		const up = () => {
			window.removeEventListener('pointermove', move);
			window.removeEventListener('pointerup', up);
			dragging = false;
			wm.dragging = false;
			saveWindows();
		};
		window.addEventListener('pointermove', move);
		window.addEventListener('pointerup', up);
	}

	function keyResize(event: KeyboardEvent) {
		// Alt + arrows dock the window in front (also announced in the switcher).
		if (!event.altKey) return;
		const map: Record<string, Parameters<typeof setSnap>[1]> = {
			ArrowLeft: 'left',
			ArrowRight: 'right',
			ArrowUp: 'max',
			ArrowDown: null,
		};
		if (event.key in map) {
			event.preventDefault();
			setSnap(win.id, map[event.key]);
		}
	}

	function openTab() {
		if (webappId) return view?.openTab();
		void openProgramTab(win.app, win.href);
	}
	const EDGES = ['n', 's', 'e', 'w', 'ne', 'nw', 'se', 'sw'];

	// PLAN 67-1: web apps and the remote view live in the desktop itself, so
	// their commands for the menu bar are registered here (not by a frame).
	let remote = $state<RemoteViewType>();
	// PLAN 75-14: an app with its own desktop stream shows it directly; the
	// remote view when the account chose it or the app was not reached.
	const dockerApp = $derived(win.app.startsWith('docker:') ? win.app.slice('docker:'.length) : '');
	const ownStream = $derived(
		!!dockerApp && hasOwnStream(sources.mine?.find((m) => m.app === dockerApp)?.stream),
	);
	let directOn = $state(directChosen(untrack(() => win.app.replace(/^docker:/, ''))));
	let directFailed = $state(false);
	const direct = $derived(ownStream && directOn && !directFailed);
	function switchDirect(on: boolean) {
		chooseDirect(dockerApp, on);
		directOn = on;
		directFailed = false;
	}
	async function restartDockerApp() {
		const app = win.app.slice('docker:'.length);
		for (const c of sources.containers.filter((x) => (x.app || x.name) === app))
			await api.post(`/docker/containers/${encodeURIComponent(c.id)}/restart`).catch(() => {});
	}
	if (untrack(() => win.app.startsWith('webapp:')))
		registerWindowCommands(
			untrack(() => win.id),
			{
				'webapp.open': { run: () => view?.openPicker(), enabled: () => !!webapp },
				'webapp.saveAs': {
					run: () => view?.saveAs(),
					enabled: () => !!webapp && webapp.bridge === 'host',
				},
				'webapp.reload': { run: restartApp, enabled: () => !!webapp },
				// A new window of the app with no file (the app starts empty).
				'webapp.new': {
					run: () => void openProgram(win.app, { fresh: true }),
					enabled: () => !!webapp,
				},
				...Object.fromEntries(
					(
						[
							['webapp.save', 'save'],
							['webapp.undo', 'undo'],
							['webapp.redo', 'redo'],
							['webapp.find', 'find'],
							['webapp.selectAll', 'select-all'],
						] as const
					).map(([id, name]) => [
						id,
						{
							run: () => view?.command(name),
							enabled: () => !!webapp && webapp.bridge === 'host',
						},
					]),
				),
				'window.openTab': openTab,
			},
		);
	else if (untrack(() => win.app === 'app:settings'))
		registerWindowCommands(
			untrack(() => win.id),
			{ 'settings.search': () => void focusSearch() },
		);
	else if (untrack(() => external))
		registerWindowCommands(
			untrack(() => win.id),
			{
				'remote.upload': { run: () => remote?.menu.upload(), enabled: () => !!remote?.menu.live() },
				'remote.clipSend': {
					run: () => void remote?.menu.clipSend(),
					enabled: () => !!remote?.menu.live(),
				},
				'remote.clipGet': {
					run: () => void remote?.menu.clipGet(),
					enabled: () => !!remote?.menu.remoteClip(),
				},
				...Object.fromEntries(
					(['auto', 'high', 'balanced', 'low'] as const).map((q) => [
						'remote.quality.' + q,
						{
							run: () => remote?.menu.setQuality(q),
							checked: () => remote?.menu.quality() === q,
						},
					]),
				),
				'remote.mute': {
					run: () => remote?.menu.toggleMute(),
					enabled: () => !!remote?.menu.audio(),
					checked: () => !!remote?.menu.muted(),
				},
				'remote.fullscreen': {
					run: () => remote?.menu.fullscreen(),
					enabled: () => !!remote?.menu.live(),
				},
				'remote.reconnect': { run: () => remote?.menu.reconnect(), enabled: () => !!remote },
				'remote.downloads': {
					items: () => remote?.menu.downloads() ?? [],
					run: (id) => id && remote?.menu.download(id),
				},
				'remote.stats': {
					run: () => remote?.menu.toggleStats(),
					enabled: () => !!remote?.menu.live(),
					checked: () => !!remote?.menu.stats(),
				},
				'remote.restart': restartDockerApp,
				'window.openTab': openTab,
			},
		);
</script>

<svelte:window
	onmessage={message}
	onblur={() => (moreOpen = false)}
	onpointerdown={(e) =>
		moreOpen && !(e.target as Element).closest?.('.more-wrap') && (moreOpen = false)}
/>

<!-- A window is a non-modal dialog: it takes the keyboard (resize keys) and the focus. -->
<!-- svelte-ignore a11y_no_noninteractive_element_to_interactive_role -->
<section
	bind:this={section}
	role="dialog"
	tabindex="-1"
	class="win"
	class:focused
	class:dragging
	class:snapped={!!win.snap}
	class:min={win.minimized}
	class:grow={!!win.from}
	style:left="{rect.x}px"
	style:top="{rect.y}px"
	style:width="{rect.w}px"
	style:height="{rect.h}px"
	style:z-index={win.z}
	style:transform={shift.x || shift.y ? `translate3d(${shift.x}px, ${shift.y}px, 0)` : undefined}
	style:--from-x="{(win.from?.x ?? rect.x) - rect.x}px"
	style:--from-y="{(win.from?.y ?? rect.y) - rect.y}px"
	aria-label={win.title}
	onpointerdown={() => focus(win.id)}
	onkeydown={keyResize}
	onanimationend={() => (win.from = undefined)}
>
	{#if side.length && !external && !ownSide}
		<nav class="side" class:folded aria-label={win.title}>
			<div class="side-title">
				<DesktopIcon {glyph} {gradient} size={folded ? 32 : 28} name={win.title} />
				{#if !folded}<b>{win.title}</b>{/if}
			</div>
			{#if searchable}
				{#if folded}
					<button
						type="button"
						class="item"
						title={t('settingshome.sideSearch')}
						aria-label={t('settingshome.sideSearch')}
						onclick={focusSearch}><Icon name="search" size={17} /></button
					>
				{:else}
					<div class="side-search" role="search">
						<span class="glass" aria-hidden="true"><Icon name="search" size={15} /></span>
						<input
							bind:this={searchEl}
							type="search"
							autocomplete="off"
							spellcheck="false"
							placeholder={t('settingshome.sideSearch')}
							aria-label={t('settingshome.sideSearch')}
							aria-controls="side-{win.id}"
							aria-activedescendant={current >= 0 ? `side-${win.id}-${current}` : undefined}
							bind:value={query}
							oninput={() => (hit = 0)}
							onkeydown={searchKey}
						/>
					</div>
					{#if matches && !links.length}<p class="side-none" role="status">
							{t('settingshome.sideNone')}
						</p>{/if}
				{/if}
			{/if}
			<div class="items" id="side-{win.id}">
				{#each shownSide as item, i (i)}
					{#if 'group' in item}
						{#if !folded}<div class="group">{t(item.group)}</div>{:else}<div
								class="rule"
							></div>{/if}
					{:else if 'text' in item}
						{@const on =
							new URLSearchParams(win.href.split('?')[1] ?? '').get('vm') ===
							new URLSearchParams(item.href.split('?')[1]).get('vm')}
						<button
							type="button"
							class="item thing"
							class:on
							title={folded ? item.text : undefined}
							aria-current={on ? 'page' : undefined}
							onclick={() => navigateWindow(win.id, item.href)}
						>
							<span class="state {item.state}" aria-hidden="true"></span>
							{#if !folded}<span>{item.text}</span>{/if}
						</button>
					{:else}
						{@const n = links.indexOf(item)}
						<button
							type="button"
							class="item"
							id={n >= 0 ? `side-${win.id}-${n}` : undefined}
							class:on={sideActive(item, path, side)}
							class:hit={n >= 0 && n === current}
							title={folded ? t(item.label) : undefined}
							aria-current={sideActive(item, path, side) ? 'page' : undefined}
							onclick={() => openSide(item)}
						>
							<Icon name={item.icon} size={17} />
							{#if !folded}<span>{t(item.label)}</span>{/if}
						</button>
					{/if}
				{/each}
			</div>
			<button
				type="button"
				class="item fold"
				onclick={toggleFold}
				title={folded ? t('desktop.window.unfold') : undefined}
			>
				<Icon name="sidebar" size={17} />
				{#if !folded}<span>{t('desktop.window.fold')}</span>{/if}
			</button>
		</nav>
	{/if}
	<div class="main">
		<!-- svelte-ignore a11y_no_static_element_interactions -->
		<header class="titlebar" onpointerdown={startMove} ondblclick={() => toggleMax(win.id)}>
			{#if !side.length || external || ownSide}<DesktopIcon
					{glyph}
					{gradient}
					{image}
					size={22}
					name={win.title}
				/>{/if}
			<b class="title">{win.title}</b>{#if webappId}{#if docName}<span
						class="crumb"
						aria-hidden="true">·</span
					><span class="title page">{docName}</span>{/if}{:else if crumb}<span
					class="crumb"
					aria-hidden="true">›</span
				><span class="title page">{crumb}</span>{/if}
			<span class="fill"></span>
			{#if webappId || external}
				<span class="more-wrap">
					<button
						type="button"
						class="ctl"
						class:on={moreOpen}
						aria-haspopup="menu"
						aria-expanded={moreOpen}
						bind:this={moreButton}
						onclick={toggleMore}
						title={t('webapps.menu.more')}
						aria-label={t('webapps.menu.more')}><Icon name="more" size={16} /></button
					>
					{#if moreOpen}
						<!-- svelte-ignore a11y_no_static_element_interactions -->
						<div
							class="win-menu dropdown-panel floating-menu"
							role="menu"
							tabindex="-1"
							bind:this={menuEl}
							onkeydown={menuKey}
						>
							{#if external}
								<!-- PLAN 66: the remote view puts its controls here (RemoteView, inTitle). -->
								<div class="remote-host" bind:this={remoteHost}></div>
								<hr />
								{#if ownStream}
									<button
										type="button"
										role="menuitemcheckbox"
										aria-checked={direct}
										onclick={() => {
											moreOpen = false;
											switchDirect(!directOn || directFailed);
										}}
										><Icon name={direct ? 'monitor' : 'zap'} size={16} /><span
											>{t(direct ? 'desktop.direct.useRemote' : 'desktop.direct.useDirect')}</span
										></button
									>
									{#if directFailed && directOn}<p class="menu-note">
											{t('desktop.direct.notReached')}
										</p>{/if}
								{/if}
								<button
									type="button"
									role="menuitem"
									onclick={() => {
										moreOpen = false;
										openTab();
									}}
									><Icon name="external" size={16} /><span>{t('desktop.window.openTab')}</span
									></button
								>
							{:else}
								<button
									type="button"
									role="menuitem"
									disabled={!webapp}
									onclick={() => {
										moreOpen = false;
										view?.openPicker();
									}}><Icon name="folder" size={16} /><span>{t('webapps.menu.open')}</span></button
								>
								<button
									type="button"
									role="menuitem"
									disabled={!webapp || webapp.bridge !== 'host'}
									onclick={() => {
										moreOpen = false;
										view?.saveAs();
									}}
									><Icon name="download" size={16} /><span>{t('webapps.menu.saveAs')}</span></button
								>
								<hr />
								<button
									type="button"
									role="menuitem"
									onclick={() => {
										moreOpen = false;
										openTab();
									}}
									><Icon name="external" size={16} /><span>{t('desktop.window.openTab')}</span
									></button
								>
								<button type="button" role="menuitem" disabled={!webapp} onclick={restartApp}
									><Icon name="restart" size={16} /><span>{t('webapps.restart')}</span></button
								>
								{#if webapp}<p class="menu-status muted">
										{t('webapps.menu.status', { version: webapp.version })}
									</p>{/if}
							{/if}
						</div>
					{/if}
				</span>
			{/if}
			<button
				type="button"
				class="ctl"
				onclick={openTab}
				title={t('desktop.window.openTab')}
				aria-label={t('desktop.window.openTab')}><Icon name="external" size={15} /></button
			>
			<button
				type="button"
				class="ctl"
				onclick={() => minimize(win.id)}
				title={t('desktop.window.minimize')}
				aria-label={t('desktop.window.minimize')}
				><svg width="14" height="14" viewBox="0 0 24 24" aria-hidden="true"
					><path d="M5 18h14" stroke="currentColor" stroke-width="2" /></svg
				></button
			>
			<button
				type="button"
				class="ctl"
				onclick={() => toggleMax(win.id)}
				title={t(win.snap === 'max' ? 'desktop.window.restore' : 'desktop.window.maximize')}
				aria-label={t(win.snap === 'max' ? 'desktop.window.restore' : 'desktop.window.maximize')}
				><Icon name="maximize" size={14} /></button
			>
			<button
				type="button"
				class="ctl close"
				onclick={() => requestClose(win.id)}
				title={t('desktop.window.close')}
				aria-label={t('desktop.window.close')}><Icon name="close" size={15} /></button
			>
		</header>
		<div class="body">
			{#if webappId}
				{#if webapp?.installed && webapp.url}
					<WebAppView
						bind:this={view}
						app={webapp}
						doc={win.doc}
						winId={win.id}
						ondoc={(doc) => {
							win.doc = doc;
							win.docName = undefined;
							saveWindows();
						}}
						onname={(name) => {
							win.docName = name;
							saveWindows();
						}}
						onopen={(doc) => void openProgram(win.app, { doc })}
					/>
				{:else if webapps.loaded}
					<p class="load-failed">{t('webapps.unknownApp')}</p>
				{/if}
			{:else if external}
				<!-- The remote view's player loads with the first Docker app (PLAN 65-6). -->
				{#if direct}
					<DirectView app={dockerApp} title={win.title} onfallback={() => (directFailed = true)}>
						{#snippet splash()}<DesktopIcon
								{glyph}
								{gradient}
								{image}
								size={72}
								name={win.title}
							/>{/snippet}
					</DirectView>
				{:else}
					{#await import('./RemoteView.svelte') then { default: RemoteView }}
						<RemoteView
							bind:this={remote}
							app={win.app.replace(/^docker:/, '')}
							title={win.title}
							onopentab={openTab}
							inTitle
							menuHost={moreOpen ? remoteHost : null}
						>
							{#snippet splash()}<DesktopIcon
									{glyph}
									{gradient}
									{image}
									size={72}
									name={win.title}
								/>{/snippet}
						</RemoteView>
					{:catch}
						<p class="load-failed">{t('desktop.window.loadFailed')}</p>
					{/await}
				{/if}
			{:else}
				<iframe
					bind:this={frame}
					onload={loaded}
					data-window={win.id}
					{src}
					title={win.title}
					allow="clipboard-read; clipboard-write; fullscreen; publickey-credentials-get; publickey-credentials-create; camera; microphone"
				></iframe>
			{/if}
			{#if dragging || (!focused && !(webappId && wm.fileDrag))}<div
					class="shield"
					aria-hidden="true"
				></div>{/if}
		</div>
	</div>
	{#if closing}
		<Dialog
			title={t('webapps.unsavedTitle')}
			onclose={() => (wm.closing = wm.closing.filter((c) => c !== win.id))}
		>
			<p>{t('webapps.unsavedText', { app: win.title })}</p>
			<div class="dialog-actions">
				<button
					type="button"
					class="btn btn-primary"
					onclick={() => (wm.closing = wm.closing.filter((c) => c !== win.id))}
					>{t('common.cancel')}</button
				>
				<button type="button" class="btn btn-danger" onclick={() => closeWindow(win.id)}
					>{t('webapps.closeAnyway')}</button
				>
			</div>
		</Dialog>
	{/if}
	{#if restartAsk}
		<Dialog title={t('webapps.unsavedTitle')} onclose={() => (restartAsk = false)}>
			<p>{t('webapps.unsavedRestart', { app: win.title })}</p>
			<div class="dialog-actions">
				<button type="button" class="btn btn-primary" onclick={() => (restartAsk = false)}
					>{t('common.cancel')}</button
				>
				<button
					type="button"
					class="btn btn-danger"
					onclick={() => {
						restartAsk = false;
						view?.restart();
					}}>{t('webapps.restartAnyway')}</button
				>
			</div>
		</Dialog>
	{/if}
	{#if !win.snap}
		{#each EDGES as edge (edge)}
			<!-- svelte-ignore a11y_no_static_element_interactions -->
			<div class="edge {edge}" onpointerdown={(e) => startResize(e, edge)}></div>
		{/each}
	{/if}
</section>

<style>
	/* PLAN 64: corners, transparency and glass follow Settings › Appearance
	   (--radius-scale, --desk-surface, --desk-blur); the page itself stays opaque. */
	.win {
		position: absolute;
		display: flex;
		border-radius: calc(14px * var(--radius-scale, 1));
		overflow: hidden;
		/* PLAN 65: even at high transparency nothing behind a window stays
		   readable (icon labels shone through the side bar): a second layer
		   keeps at least ~75 % cover, and the glass always blurs. */
		background:
			linear-gradient(var(--desk-surface, var(--bg)), var(--desk-surface, var(--bg))),
			color-mix(in srgb, var(--surface) 55%, transparent);
		-webkit-backdrop-filter: blur(max(var(--desk-blur, 0px), 14px)) saturate(1.3);
		backdrop-filter: blur(max(var(--desk-blur, 0px), 14px)) saturate(1.3);
		border: 1px solid var(--border);
		box-shadow: 0 10px 28px rgb(0 0 0 / 0.35);
	}
	.win.focused {
		box-shadow: 0 24px 60px rgb(0 0 0 / 0.45);
	}
	/* While moving: one layer for the GPU and a lighter shadow (smooth in every browser). */
	.win.dragging {
		will-change: transform;
		-webkit-backdrop-filter: none;
		backdrop-filter: none;
		box-shadow: 0 14px 34px rgb(0 0 0 / 0.4);
	}
	.win.snapped {
		border-radius: 0;
	}
	.win.min {
		display: none;
	}
	.win.grow {
		animation: grow 0.22s cubic-bezier(0.2, 0.8, 0.2, 1);
	}
	@keyframes grow {
		from {
			transform: translate(var(--from-x), var(--from-y)) scale(0.08);
			opacity: 0;
		}
	}
	/* PLAN 71: the side bar covers what lies behind the window completely
	   (icon names of the desktop shone through it, owner's photo of 7 Nov
	   2026): an opaque layer from the theme, tinted a little toward the page
	   so it still reads as its own glass pane; also while the window moves. */
	/* PLAN 74: as wide as its longest entry (owner: automatically, no
	   dragging), within 200–320 px; the scroll bar has room of its own. */
	.side {
		width: max-content;
		min-width: 200px;
		max-width: 320px;
		flex: none;
		display: flex;
		flex-direction: column;
		padding: var(--space-3) var(--space-2-5);
		background:
			linear-gradient(
				180deg,
				color-mix(in srgb, var(--text) 3%, transparent),
				color-mix(in srgb, var(--accent) 3%, transparent)
			),
			color-mix(in srgb, var(--surface) 88%, var(--bg));
		border-right: 1px solid var(--border);
		overflow: hidden;
	}
	.side-search {
		position: relative;
		margin: 0 0 var(--space-2);
	}
	.side-search .glass {
		position: absolute;
		left: 9px;
		top: 50%;
		transform: translateY(-50%);
		display: flex;
		color: var(--muted);
		pointer-events: none;
	}
	.side-search input {
		width: 100%;
		min-height: 32px;
		padding: var(--space-1) var(--space-2) var(--space-1) 30px;
		border: 1px solid var(--border);
		border-radius: calc(8px * var(--radius-scale, 1));
		background: var(--bg);
		color: var(--text);
		font: inherit;
		font-size: var(--text-base);
	}
	.side-search input::placeholder {
		color: var(--muted);
		opacity: 1;
	}
	.side-search input:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -1px;
	}
	.side-none {
		margin: var(--space-1) var(--space-2-5) var(--space-2);
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.item.hit {
		box-shadow: inset 0 0 0 2px var(--accent);
	}
	.side.folded {
		width: 60px;
		min-width: 60px;
		align-items: center;
		padding: var(--space-3) var(--space-2);
	}
	.side-title {
		display: flex;
		align-items: center;
		gap: var(--space-2-5);
		padding: var(--space-1) var(--space-1-5) 14px;
		font-size: var(--text-lg);
		min-width: 0;
	}
	.side-title b {
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.items {
		flex: 1;
		overflow-y: auto;
		scrollbar-width: thin;
		scrollbar-gutter: stable;
		display: flex;
		flex-direction: column;
		gap: 2px;
		width: 100%;
	}
	.group {
		margin: var(--space-3) var(--space-2-5) var(--space-1);
		font-size: var(--text-xs);
		color: var(--muted);
		text-transform: uppercase;
		letter-spacing: 0.05em;
	}
	.item.thing {
		padding-left: var(--space-4);
	}
	.state {
		flex: none;
		width: 9px;
		height: 9px;
		margin: 0 var(--space-1);
		border-radius: 50%;
		border: 1.5px solid var(--muted);
	}
	.state.on {
		border-color: var(--ok-text);
		background: var(--ok-text);
	}
	.rule {
		height: 1px;
		margin: var(--space-2) var(--space-1-5);
		background: var(--border);
	}
	.item {
		display: flex;
		align-items: center;
		gap: var(--space-2-5);
		min-height: 36px;
		padding: var(--space-1-5) var(--space-2-5);
		border: 0;
		border-radius: calc(9px * var(--radius-scale, 1));
		background: none;
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
		width: 100%;
	}
	.folded .item {
		justify-content: center;
		padding: 0;
		width: 44px;
	}
	.item :global(svg) {
		color: var(--muted);
		flex: none;
	}
	.item:hover {
		background: color-mix(in srgb, var(--text) 7%, transparent);
	}
	.item.on {
		background: color-mix(in srgb, var(--accent) 18%, transparent);
		/* Toward the text color: 4.5:1 on the tinted glass in every theme (PLAN 65-5). */
		color: color-mix(in srgb, var(--accent) 65%, var(--text));
		font-weight: 600;
	}
	.item.on :global(svg) {
		color: var(--accent);
	}
	.item:focus-visible,
	.ctl:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.fold {
		color: var(--muted);
		margin-top: var(--space-1-5);
	}
	.main {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
	}
	.titlebar {
		height: 42px;
		flex: none;
		/* Above the body (the ⋯ menu of a web app opens over the app), below the resize edges. */
		position: relative;
		z-index: 1;
		display: flex;
		align-items: center;
		gap: 9px;
		padding: 0 var(--space-1-5) 0 14px;
		background: color-mix(in srgb, var(--header) 55%, transparent);
		border-bottom: 1px solid var(--border);
		user-select: none;
		touch-action: none;
		cursor: default;
	}
	.title {
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.title {
		flex: none;
		max-width: 45%;
	}
	.title.page {
		flex: 0 1 auto;
		max-width: none;
		min-width: 0;
		font-weight: 400;
		color: var(--muted);
	}
	.crumb {
		margin: 0 -4px;
		color: var(--muted);
	}
	.fill {
		flex: 1;
	}
	.more-wrap {
		position: relative;
		flex: none;
	}
	.ctl.on {
		background: color-mix(in srgb, var(--text) 12%, transparent);
		color: var(--text);
	}
	.win-menu {
		position: absolute;
		right: 0;
		top: calc(100% + 6px);
		z-index: 20;
		min-width: 260px;
		max-width: min(340px, calc(100vw - 24px));
		/* Over a busy app (a remote picture) the glass alone would not keep the text readable. */
		background:
			linear-gradient(var(--menu-bg), var(--menu-bg)),
			color-mix(in srgb, var(--surface) 88%, transparent);
		display: flex;
		flex-direction: column;
		padding: var(--space-1-5);
	}
	.menu-status {
		margin: var(--space-1) var(--space-2-5) 2px;
		font-size: var(--text-xs);
	}
	.ctl {
		width: 32px;
		height: 28px;
		display: grid;
		place-items: center;
		border: 0;
		border-radius: calc(7px * var(--radius-scale, 1));
		background: none;
		color: var(--muted);
		cursor: pointer;
		flex: none;
	}
	.ctl:hover {
		background: color-mix(in srgb, var(--text) 9%, transparent);
		color: var(--text);
	}
	.ctl.close:hover {
		background: #c42b1c;
		color: #fff;
	}
	.menu-note {
		margin: 0;
		padding: var(--space-1) var(--space-3) var(--space-2);
		font-size: var(--text-sm);
		color: var(--muted);
		max-width: 18rem;
	}
	.load-failed {
		margin: 0;
		padding: var(--space-5);
		color: var(--text);
	}
	.body {
		position: relative;
		/* Its own layer: nothing of the content (the remote view's canvas) rises above the ⋯ menu. */
		isolation: isolate;
		flex: 1;
		min-height: 0;
		display: flex;
	}
	iframe {
		flex: 1;
		width: 100%;
		border: 0;
		background: var(--bg);
	}
	.shield {
		position: absolute;
		inset: 0;
	}
	.win:not(.focused) .shield {
		background: rgb(0 0 0 / 0.06);
	}
	.dragging .shield {
		cursor: grabbing;
	}
	.edge {
		position: absolute;
		z-index: 5;
	}
	.edge.n,
	.edge.s {
		left: 10px;
		right: 10px;
		height: 7px;
		cursor: ns-resize;
	}
	.edge.n {
		top: 0;
		height: 5px;
	}
	.edge.s {
		bottom: 0;
	}
	.edge.e,
	.edge.w {
		top: 10px;
		bottom: 10px;
		width: 7px;
		cursor: ew-resize;
	}
	.edge.e {
		right: 0;
	}
	.edge.w {
		left: 0;
	}
	.edge.ne,
	.edge.nw,
	.edge.se,
	.edge.sw {
		width: 14px;
		height: 14px;
	}
	.edge.ne {
		top: 0;
		right: 0;
		cursor: nesw-resize;
	}
	.edge.sw {
		bottom: 0;
		left: 0;
		cursor: nesw-resize;
	}
	.edge.nw {
		top: 0;
		left: 0;
		cursor: nwse-resize;
	}
	.edge.se {
		bottom: 0;
		right: 0;
		cursor: nwse-resize;
	}
	@media (prefers-reduced-motion: reduce) {
		.win.grow {
			animation: none;
		}
		.win {
			transition: none;
		}
	}
</style>
