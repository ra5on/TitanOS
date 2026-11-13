<script lang="ts">
	import MigrationDialog from '../webapps/MigrationDialog.svelte';
	// PLAN 61: TitanOS on the phone – programs as a home screen; each opens
	// full screen with a way back (PhoneBar). PLAN 79-10: the bar starts with
	// the button "Menü" and the search next to it; the menu (Launcher.svelte,
	// the same as on the desktop) opens across the width.
	// PLAN 87 (owner 13.11.2026): the look of the desktop (wallpaper, moving
	// ones too, glass, accent, same symbols); pages with dots to swipe; a long
	// press starts arranging (symbols wiggle, drag to move, one on another
	// makes a folder); widgets on the pages; a dock below that the account
	// switches on or off; pulling down the pages searches, pulling down from
	// the top shows the notifications.
	import { onMount, tick } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import Icon from '../components/Icon.svelte';
	import Dialog from '../components/Dialog.svelte';
	import NodeMenu from '../components/NodeMenu.svelte';
	import { clusterNodes } from '../cluster.svelte.ts';
	import { t } from '../i18n/index.svelte.ts';
	import { system } from '../system.svelte.ts';
	import DesktopIcon from './DesktopIcon.svelte';
	import Launcher from './Launcher.svelte';
	import WidgetView from './WidgetView.svelte';
	import PowerDialog from '../components/PowerDialog.svelte';
	import LiveBackground from '../live/LiveBackground.svelte';
	import { isLivePicture } from '../live/shaders.ts';
	import { arrangeIcons, isGpuWidget, WIDGET_KINDS, type WidgetKind } from './prefs.ts';
	import {
		addWidget,
		cellsOf,
		combine,
		dissolveFolder,
		dropEmptyPages,
		moveTo,
		moveToDock,
		outOfFolder,
		ownLayout,
		phoneLayout,
		PHONE_COLUMNS,
		PHONE_DOCK_MAX,
		removeWidget,
		renameFolder,
		resizeWidget,
		type PhonePrefs,
	} from './phone-home.ts';
	import { openTab, programs, vmScreens, type Program } from './programs.svelte.ts';
	import { watchSources } from './sources.svelte.ts';
	import { desktop, loadDesktop, savePrefs } from './wm.svelte.ts';
	import { dayTint, wallpaperCss } from './wallpapers.ts';
	import { WIDGET_TEXT } from './widget-text.ts';
	import { appearance } from '../theme/appearance.svelte.ts';
	import { effectOn, resolvePalette } from '../theme/themes.ts';
	import { loadWidgetCameras, widgetCameras } from '../nvr/widget.svelte.ts';
	import { motionOK } from '../components/sheet.ts';
	import { closeIntoSymbol, markOpening } from './phone-motion.ts';

	onMount(() => {
		loadDesktop();
		// Coming from the login (keyboard open) or a long page, Safari keeps the
		// old scroll position: the home screen always starts at its top.
		window.scrollTo(0, 0);
		// PLAN 87: back from a program, it closes into its symbol.
		void tick().then(() =>
			closeIntoSymbol((key) => {
				const at = shown.pages.findIndex((p) => p.includes(key));
				if (at > 0 && pagesEl) pagesEl.scrollLeft = at * pagesEl.clientWidth;
				return document.querySelector<HTMLElement>(
					`[data-home-key="${CSS.escape(key)}"] .icon-box`,
				);
			}),
		);
		return watchSources();
	});
	let query = $state('');
	const all = $derived(programs());
	const byKey = $derived(new Map(all.map((p) => [p.key, p])));
	// The standard order: the desktop's arrangement, its folders opened up.
	const standard = $derived(
		arrangeIcons(
			desktop.prefs,
			all.map((p) => ({ key: p.key, label: p.label, kind: p.kind })),
		).flatMap((k) =>
			k.startsWith('folder:')
				? (desktop.prefs.folders.find((f) => 'folder:' + f.id === k)?.apps ?? [])
				: [k],
		),
	);
	const order = $derived([
		...standard,
		...all.map((p) => p.key).filter((k) => !standard.includes(k)),
	]);
	const phone = $derived(desktop.prefs.phone);

	// How many cells a page holds: four columns, as many rows as fit.
	const ROW = 112;
	let pagesEl = $state<HTMLElement>();
	let rows = $state(5);
	$effect(() => {
		if (!pagesEl) return;
		const el = pagesEl;
		const measure = () => (rows = Math.max(3, Math.floor((el.clientHeight - 8) / ROW)));
		measure();
		const sizes = new ResizeObserver(measure);
		sizes.observe(el);
		return () => sizes.disconnect();
	});
	// While arranging, the pages keep their size (the bar below them takes room).
	let frozenRows = $state<number | null>(null);
	const cells = $derived((frozenRows ?? rows) * PHONE_COLUMNS);
	const shown = $derived(phoneLayout(phone, order, cells));
	const folderOf = (key: string) => shown.folders.find((f) => 'folder:' + f.id === key);
	const widgetOf = (key: string) => phone.widgets.find((w) => 'widget:' + w.id === key);

	// The wallpaper: as on the desktop, a moving photo moves here too.
	const light = $derived(!resolvePalette(appearance.current, appearance.systemLight).dark);
	const wallpaper = $derived(
		desktop.prefs.wallpaper.source === 'custom'
			? `url('/api/v1/background?v=${desktop.prefs.wallpaper.version}') center / cover`
			: `${wallpaperCss(
					desktop.prefs.wallpaper.source === 'builtin' ? desktop.prefs.wallpaper.id : 'wave',
					light,
				)} center / cover`,
	);
	const livePicture = $derived(
		desktop.prefs.wallpaper.source === 'builtin' && isLivePicture(desktop.prefs.wallpaper.id)
			? desktop.prefs.wallpaper.id
			: null,
	);
	const reduced = !motionOK();
	let tint = $state(dayTint());
	$effect(() => {
		const timer = setInterval(() => (tint = dayTint()), 10 * 60 * 1000);
		return () => clearInterval(timer);
	});
	const wiggle = $derived(!reduced && effectOn(appearance.current, 'micro'));

	// PLAN 63: a Docker app opens full screen in its remote view (a history
	// entry of its own, so the phone's Back returns to the grid).
	const remote = $derived(page.state.remote ? (byKey.get(page.state.remote) ?? null) : null);
	function open(p: Program, from?: HTMLElement | null) {
		// PLAN 87: the program grows out of its symbol (lib/desktop/phone-motion.ts).
		markOpening(p.key, from ?? null);
		// PLAN 66: a web app opens full screen above the grid, like a Docker app.
		if (p.kind === 'webapp')
			return void goto(page.url.pathname, {
				state: { webapp: { id: p.key.slice(7) } },
				shallow: true,
			});
		if (p.kind !== 'docker') return void goto(p.href);
		void goto(page.url.pathname, { state: { remote: p.key }, shallow: true });
	}

	// PLAN 79-10: the menu; PLAN 87: also what the search finds.
	let menu = $state(false);
	let menuButton = $state<HTMLButtonElement>();
	let searchField = $state<HTMLInputElement>();
	let power = $state<'restart' | 'shutdown' | null>(null);
	function closeMenu() {
		if (!menu) return;
		menu = false;
		menuButton?.focus();
	}
	function openFromMenu(key: string, href?: string) {
		menu = false;
		query = '';
		const p = byKey.get(key) ?? vmScreens().find((v) => v.key === key);
		if (href) void goto(href);
		else if (p) open(p);
	}
	/** PLAN 87: the search – the menu with its hits; the field gets the keyboard (inside the gesture). */
	function startSearch() {
		menu = true;
		searchField?.focus();
	}

	// --- PLAN 87: pages ----------------------------------------------------

	let current = $state(0);
	function onPagesScroll() {
		if (!pagesEl) return;
		current = Math.round(pagesEl.scrollLeft / Math.max(1, pagesEl.clientWidth));
	}
	function showPage(n: number) {
		if (!pagesEl) return;
		pagesEl.scrollTo({ left: n * pagesEl.clientWidth, behavior: reduced ? 'auto' : 'smooth' });
	}

	// --- PLAN 87: arranging ---------------------------------------------------

	let editing = $state(false);
	let selected = $state<string | null>(null);
	/** Every change starts from what is on the screen, which becomes the account's own. */
	function change(edit: (p: PhonePrefs) => PhonePrefs) {
		const base = ownLayout(phone, shown);
		const next = edit(base);
		if (next === base && phone.custom) return;
		savePrefs((p) => (p.phone = next));
	}
	function startEditing() {
		frozenRows = rows;
		editing = true;
		menu = false;
	}
	function stopEditing() {
		editing = false;
		selected = null;
		frozenRows = null;
		if (phone.custom) change(dropEmptyPages);
	}

	// A long press (half a second without moving) starts arranging.
	let press: { timer: ReturnType<typeof setTimeout>; x: number; y: number; id: number } | null =
		null;
	let suppressClick = false;
	function pressStart(event: PointerEvent, key: string | null) {
		if (event.button !== 0 && event.pointerType === 'mouse') return;
		// Only the click right after a long press (or a pull) is swallowed.
		suppressClick = false;
		if (editing) {
			if (key) dragStart(event, key);
			return;
		}
		const at = { x: event.clientX, y: event.clientY, id: event.pointerId };
		clearPress();
		press = {
			...at,
			timer: setTimeout(() => {
				press = null;
				suppressClick = true;
				startEditing();
				// The finger may go on dragging what it holds.
				if (key) dragStart(event, key);
			}, 500),
		};
	}
	function pressMove(event: PointerEvent) {
		if (press && Math.hypot(event.clientX - press.x, event.clientY - press.y) > 8) clearPress();
	}
	function clearPress() {
		if (press) clearTimeout(press.timer);
		press = null;
	}
	function activate(event: MouseEvent, key: string) {
		if (suppressClick) {
			suppressClick = false;
			event.preventDefault();
			return;
		}
		if (key.startsWith('folder:')) {
			openFolder = key.slice(7);
			return;
		}
		if (editing) {
			selected = selected === key ? null : key;
			return;
		}
		const p = byKey.get(key);
		if (p) open(p, (event.currentTarget as HTMLElement).querySelector('.icon-box'));
	}

	// Dragging while arranging: the symbol follows the finger; on another
	// program it makes a folder, between others it takes the place, at the
	// edge of the screen it goes to the next page, on the dock into the dock.
	type Target =
		| { kind: 'place'; page: number; index: number }
		| { kind: 'combine'; key: string }
		| { kind: 'dock'; index: number }
		| null;
	let drag = $state<{ key: string; x: number; y: number; id: number; moved: boolean } | null>(null);
	let target = $state<Target>(null);
	/** Where a dropped entry would go: a line before (or after the last) entry of the page. */
	const mark = $derived.by(() => {
		if (target?.kind !== 'place') return null;
		const list = (shown.pages[target.page] ?? []).filter((k) => k !== drag?.key);
		const at = Math.min(target.index, list.length);
		if (!list.length) return null;
		return at < list.length
			? { key: list[at], side: 'before' }
			: { key: list.at(-1)!, side: 'after' };
	});
	let edgeTimer: ReturnType<typeof setTimeout> | undefined;

	function dragStart(event: PointerEvent, key: string) {
		drag = { key, x: event.clientX, y: event.clientY, id: event.pointerId, moved: false };
		target = null;
		window.addEventListener('pointermove', dragMove);
		window.addEventListener('pointerup', dragEnd);
		window.addEventListener('pointercancel', dragEnd);
	}
	function dragMove(event: PointerEvent) {
		if (!drag || event.pointerId !== drag.id) return;
		if (!drag.moved && Math.hypot(event.clientX - drag.x, event.clientY - drag.y) < 8) return;
		drag = { ...drag, x: event.clientX, y: event.clientY, moved: true };
		target = targetAt(event.clientX, event.clientY, drag.key);
		// At the edge of the screen for a moment: the page before or after.
		const edge = event.clientX < 24 ? -1 : event.clientX > window.innerWidth - 24 ? 1 : 0;
		if (!edge) {
			clearTimeout(edgeTimer);
			edgeTimer = undefined;
		} else if (!edgeTimer)
			edgeTimer = setTimeout(() => {
				edgeTimer = undefined;
				const next = current + edge;
				if (next >= 0 && next <= shown.pages.length) showPage(next);
			}, 550);
	}
	function targetAt(x: number, y: number, key: string): Target {
		const hits = document.elementsFromPoint(x, y) as HTMLElement[];
		const dock = hits.find((el) => el.matches?.('[data-phone-dock]'));
		if (dock && !key.startsWith('widget:') && !key.startsWith('folder:')) {
			const slots = [...dock.querySelectorAll<HTMLElement>('[data-home-key]')].filter(
				(el) => el.dataset.homeKey !== key,
			);
			let index = slots.length;
			slots.forEach((el, i) => {
				const r = el.getBoundingClientRect();
				if (index === slots.length && x < r.left + r.width / 2) index = i;
			});
			return { kind: 'dock', index };
		}
		const item = hits.find((el) => el.dataset?.homeKey && el.dataset.homeKey !== key);
		if (item) {
			const other = item.dataset.homeKey!;
			const pageNo = Number(item.closest<HTMLElement>('[data-page]')?.dataset.page ?? current);
			const list = shown.pages[pageNo] ?? [];
			const icon = item.querySelector('.icon-box')?.getBoundingClientRect();
			const into =
				!key.startsWith('widget:') &&
				!key.startsWith('folder:') &&
				!other.startsWith('widget:') &&
				!item.closest('[data-phone-dock]') &&
				icon &&
				x > icon.left + icon.width * 0.18 &&
				x < icon.right - icon.width * 0.18 &&
				y > icon.top + icon.height * 0.18 &&
				y < icon.bottom - icon.height * 0.18;
			if (into) return { kind: 'combine', key: other };
			const r = item.getBoundingClientRect();
			const after = x > r.left + r.width / 2;
			const at = list.indexOf(other);
			return { kind: 'place', page: pageNo, index: at < 0 ? list.length : at + (after ? 1 : 0) };
		}
		const pageEl = hits.find((el) => el.dataset?.page !== undefined);
		if (pageEl) {
			const pageNo = Number(pageEl.dataset.page);
			return { kind: 'place', page: pageNo, index: (shown.pages[pageNo] ?? []).length };
		}
		return null;
	}
	function dragEnd(event: PointerEvent) {
		if (!drag || event.pointerId !== drag.id) return;
		window.removeEventListener('pointermove', dragMove);
		window.removeEventListener('pointerup', dragEnd);
		window.removeEventListener('pointercancel', dragEnd);
		clearTimeout(edgeTimer);
		edgeTimer = undefined;
		const { key, moved } = drag;
		const to = target;
		drag = null;
		target = null;
		if (!moved || !to || event.type === 'pointercancel') return;
		apply(key, to);
	}
	function apply(key: string, to: NonNullable<Target>) {
		if (to.kind === 'combine')
			change((p) => combine(p, key, to.key, t('phone.folder.new'), 'pf' + Date.now().toString(36)));
		else if (to.kind === 'dock') change((p) => moveToDock(p, key, to.index));
		else {
			// Taken out first, the index after the old place counts one less.
			const list = shown.pages[to.page] ?? [];
			const from = list.indexOf(key);
			const index = from >= 0 && from < to.index ? to.index - 1 : to.index;
			change((p) => moveTo(p, key, to.page, index));
		}
	}
	/** The way without dragging: the chosen entry one place on, back, or to the next page. */
	function step(by: -1 | 1, pageStep = false) {
		if (!selected) return;
		const key = selected;
		const pageNo = shown.pages.findIndex((p) => p.includes(key));
		if (pageNo < 0) return;
		const index = shown.pages[pageNo].indexOf(key);
		if (pageStep) {
			const to = pageNo + by;
			if (to < 0) return;
			change((p) => moveTo(p, key, to, by > 0 ? 0 : (shown.pages[to]?.length ?? 0)));
			void tick().then(() => showPage(to));
		} else change((p) => moveTo(p, key, pageNo, Math.max(0, index + by)));
	}
	const selectedPlace = $derived.by(() => {
		if (!selected) return null;
		const pageNo = shown.pages.findIndex((p) => p.includes(selected!));
		return pageNo < 0 ? null : { page: pageNo, index: shown.pages[pageNo].indexOf(selected) };
	});
	function toggleDock(key: string) {
		if (shown.dock.includes(key)) change((p) => moveTo(p, key, current, 0));
		else change((p) => moveToDock(p, key, shown.dock.length));
	}

	// --- PLAN 87: folders -----------------------------------------------------

	let openFolder = $state<string | null>(null);
	const folder = $derived(openFolder ? shown.folders.find((f) => f.id === openFolder) : undefined);
	let folderName = $state('');
	let renaming = $state(false);
	$effect(() => {
		if (!folder) {
			openFolder = null;
			renaming = false;
		}
	});

	// --- PLAN 87: widgets -----------------------------------------------------

	let picker = $state(false);
	const widgetKinds = $derived(
		WIDGET_KINDS.filter(
			(kind) =>
				(!isGpuWidget(kind) || (system.info?.accelerators ?? []).some((a) => a.kind === kind)) &&
				(kind !== 'camera' || widgetCameras.list.length > 0),
		),
	);
	$effect(() => {
		if (picker) void loadWidgetCameras(true);
	});
	function pick(kind: WidgetKind) {
		picker = false;
		const at = current;
		change((p) => addWidget(p, kind, at, 'pw' + Date.now().toString(36)));
	}
	function openWidget(app: string, href?: string) {
		if (editing) return;
		const p = byKey.get(app);
		if (href) void goto(href);
		else if (p) open(p);
	}

	// --- PLAN 87: pulling down --------------------------------------------------
	// On the pages: the search. From the bar at the top: the notifications
	// (PhoneNotices, in the shell). A small way first, so taps stay taps.

	let pull = $state(0);
	let pullFrom: { x: number; y: number; top: boolean } | null = null;
	function pullStart(event: TouchEvent, top: boolean) {
		if (editing || menu || event.touches.length !== 1) return;
		const touch = event.touches[0];
		pullFrom = { x: touch.clientX, y: touch.clientY, top };
		pull = 0;
	}
	function pullMove(event: TouchEvent) {
		if (!pullFrom) return;
		const touch = event.touches[0];
		const dy = touch.clientY - pullFrom.y;
		const dx = touch.clientX - pullFrom.x;
		if (pull === 0 && (Math.abs(dx) > Math.abs(dy) || dy < 0) && Math.hypot(dx, dy) > 10) {
			pullFrom = null;
			return;
		}
		pull = Math.max(0, dy);
		if (pull > 10 && event.cancelable) event.preventDefault();
	}
	function pullEnd() {
		if (!pullFrom) return;
		const { top } = pullFrom;
		pullFrom = null;
		const far = pull > 70;
		pull = 0;
		if (!far) return;
		suppressClick = true;
		if (top) window.dispatchEvent(new CustomEvent('titanos-phone-notices'));
		else startSearch();
	}
</script>

{#snippet symbol(key: string, size: number)}
	{@const p = byKey.get(key)}
	{@const f = folderOf(key)}
	{#if f}
		<span class="icon-box folder-box" style:--s="{size}px">
			{#each f.apps.slice(0, 4) as k (k)}
				{@const q = byKey.get(k)}
				{#if q}<DesktopIcon
						glyph={q.glyph}
						gradient={q.gradient}
						image={q.image}
						name=""
						size={Math.round(size * 0.36)}
					/>{/if}
			{/each}
		</span>
	{:else if p}
		<span class="icon-box">
			<DesktopIcon glyph={p.glyph} gradient={p.gradient} image={p.image} name={p.label} {size} />
			{#if p.badge?.count}<span class="badge">{p.badge.count}</span>{:else if p.badge?.warn}<span
					class="badge warn">!</span
				>{/if}
		</span>
	{/if}
{/snippet}

{#snippet entry(key: string, inDock: boolean)}
	{@const p = byKey.get(key)}
	{@const f = folderOf(key)}
	{@const label = f ? f.name : (p?.label ?? '')}
	<button
		type="button"
		class="app"
		class:folder={!!f}
		class:lifted={drag?.moved && drag.key === key}
		class:into={target?.kind === 'combine' && target.key === key}
		class:mark-before={mark?.key === key && mark.side === 'before'}
		class:mark-after={mark?.key === key && mark.side === 'after'}
		class:chosen={selected === key}
		data-home-key={key}
		aria-label={f
			? t('phone.folder.label', { name: f.name, count: f.apps.length })
			: inDock
				? label
				: undefined}
		aria-pressed={editing ? selected === key : undefined}
		onpointerdown={(e) => pressStart(e, key)}
		onpointermove={pressMove}
		onpointerup={clearPress}
		onpointercancel={clearPress}
		onclick={(e) => activate(e, key)}
	>
		<span class="wrap">{@render symbol(key, inDock ? 54 : 58)}</span>
		{#if !inDock}<span class="label">{label}</span>{/if}
	</button>
{/snippet}

{#if remote}
	<div class="remote-full">
		{#await import('./RemoteView.svelte') then { default: RemoteView }}
			<RemoteView
				app={remote.key.replace(/^docker:/, '')}
				title={remote.label}
				phone
				onback={() => history.back()}
				onopentab={() => remote && openTab(remote.key, remote.href)}
			/>
		{:catch}
			<p class="load-failed">{t('desktop.window.loadFailed')}</p>
		{/await}
	</div>
{/if}
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
	class="home"
	class:light
	class:editing
	class:wiggle
	class:live={desktop.prefs.wallpaper.live && !reduced}
	style:--wallpaper={wallpaper}
	style:--tint="{tint}deg"
	style:--row="{ROW}px"
	oncontextmenu={(e) => e.preventDefault()}
>
	{#if livePicture}
		<div class="paper photo" aria-hidden="true">
			<LiveBackground picture={livePicture} moving={desktop.prefs.wallpaper.live} paused={!!drag} />
		</div>
	{:else}
		<div class="paper" aria-hidden="true"></div>
	{/if}
	<header
		class="top"
		data-pull="notices"
		ontouchstart={(e) => pullStart(e, true)}
		ontouchmove={pullMove}
		ontouchend={pullEnd}
		ontouchcancel={pullEnd}
	>
		{#if editing}
			<button
				type="button"
				class="bar-btn glass-panel"
				data-phone-add-widget
				onclick={() => (picker = true)}
				><Icon name="plus" size={18} /><span>{t('phone.widgets.add')}</span></button
			>
			<span class="grow"></span>
			<button type="button" class="bar-btn glass-panel done" data-phone-done onclick={stopEditing}
				>{t('phone.edit.done')}</button
			>
		{:else}
			<button
				type="button"
				class="menu-btn glass-panel"
				class:on={menu}
				bind:this={menuButton}
				data-menu-button
				aria-haspopup="dialog"
				aria-expanded={menu}
				onclick={() => (menu = !menu)}
				><Icon name="grid" size={18} /><span>{t('desktop.menu.title')}</span></button
			>
			<label class="search glass-panel">
				<Icon name="search" size={17} />
				<input
					bind:this={searchField}
					bind:value={query}
					oninput={() => (menu = true)}
					placeholder={t('desktop.menu.searchShort')}
					aria-label={t('desktop.menu.search')}
				/>
			</label>
			{#if clusterNodes.nodes.length > 1}<span class="node"><NodeMenu variant="phone" /></span>{/if}
		{/if}
	</header>
	{#if menu}
		<!-- svelte-ignore a11y_click_events_have_key_events -->
		<div class="menu-back" onclick={closeMenu}></div>
		<Launcher
			phone
			{query}
			onopen={openFromMenu}
			onclose={closeMenu}
			onpower={(action) => {
				menu = false;
				power = action;
			}}
		/>
	{/if}
	{#if pull > 10}
		<div class="pull-hint glass-panel" style:--pull="{Math.min(pull, 90)}px" role="status">
			<Icon name="search" size={16} />{t(pull > 70 ? 'phone.pull.release' : 'phone.pull.search')}
		</div>
	{/if}
	<div
		class="pages"
		class:dragging={!!drag?.moved}
		bind:this={pagesEl}
		onscroll={onPagesScroll}
		data-pull="search"
		ontouchstart={(e) => pullStart(e, false)}
		ontouchmove={pullMove}
		ontouchend={pullEnd}
		ontouchcancel={pullEnd}
		onpointerdown={(e) => {
			if (!(e.target as HTMLElement).closest('[data-home-key]')) pressStart(e, null);
		}}
		onpointermove={pressMove}
		onpointerup={clearPress}
		onpointercancel={clearPress}
	>
		{#each [...shown.pages, ...(editing ? [[]] : [])] as list, pageNo (pageNo)}
			<section
				class="page"
				data-page={pageNo}
				aria-label={t('phone.page', { n: pageNo + 1, of: shown.pages.length })}
			>
				<div class="grid">
					{#each list as key (key)}
						{#if key.startsWith('widget:')}
							{@const w = widgetOf(key)}
							{#if w}
								<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
								<div
									class="widget glass-panel"
									class:small={w.size === 's'}
									class:lifted={drag?.moved && drag.key === key}
									class:chosen={selected === key}
									style:grid-column="span {w.size === 's' ? 2 : 4}"
									data-home-key={key}
									data-cells={cellsOf(key, phone)}
									role="group"
									aria-label={t(WIDGET_TEXT[w.kind][0])}
									onpointerdown={(e) => pressStart(e, key)}
									onpointermove={pressMove}
									onpointerup={clearPress}
									onpointercancel={clearPress}
									onclick={(e) => {
										if (suppressClick) {
											suppressClick = false;
											e.stopPropagation();
										} else if (editing) selected = selected === key ? null : key;
									}}
								>
									<div class="widget-body" inert={editing}>
										<WidgetView
											kind={w.kind}
											hide={w.hide}
											device={w.device}
											camera={w.camera}
											large={w.size === 'l'}
											onopen={openWidget}
										/>
									</div>
									{#if editing}
										<span class="widget-tools">
											<button
												type="button"
												class="tool"
												aria-label={t('phone.widgets.resize', { name: t(WIDGET_TEXT[w.kind][0]) })}
												title={t('phone.widgets.resize', { name: t(WIDGET_TEXT[w.kind][0]) })}
												onclick={(e) => {
													e.stopPropagation();
													change((p) => resizeWidget(p, w.id));
												}}><Icon name="resize" size={16} /></button
											>
											<button
												type="button"
												class="tool remove"
												data-widget-remove
												aria-label={t('phone.widgets.remove', { name: t(WIDGET_TEXT[w.kind][0]) })}
												title={t('phone.widgets.remove', { name: t(WIDGET_TEXT[w.kind][0]) })}
												onclick={(e) => {
													e.stopPropagation();
													change((p) => removeWidget(p, w.id));
												}}><Icon name="close" size={16} /></button
											>
										</span>
									{/if}
								</div>
							{/if}
						{:else}
							{@render entry(key, false)}
						{/if}
					{/each}
				</div>
				{#if editing && !list.length}
					<p class="empty-page">{t('phone.edit.newPage')}</p>
				{/if}
			</section>
		{/each}
	</div>
	{#if editing}
		<div class="edit-bar glass-panel" role="toolbar" aria-label={t('phone.edit.bar')}>
			{#if selected && selectedPlace}
				{@const label =
					folderOf(selected)?.name ??
					byKey.get(selected)?.label ??
					(widgetOf(selected) ? t(WIDGET_TEXT[widgetOf(selected)!.kind][0]) : '')}
				<span class="edit-name">{label}</span>
				<button
					type="button"
					class="bar-btn"
					data-step="back"
					disabled={selectedPlace.index === 0}
					aria-label={t('phone.edit.back', { name: label })}
					onclick={() => step(-1)}><Icon name="arrowLeft" size={18} /></button
				>
				<button
					type="button"
					class="bar-btn"
					data-step="on"
					disabled={selectedPlace.index >= shown.pages[selectedPlace.page].length - 1}
					aria-label={t('phone.edit.on', { name: label })}
					onclick={() => step(1)}><Icon name="arrowRight" size={18} /></button
				>
				<button
					type="button"
					class="bar-btn"
					data-step="page-back"
					disabled={selectedPlace.page === 0}
					onclick={() => step(-1, true)}>{t('phone.edit.pageBack')}</button
				>
				<button type="button" class="bar-btn" data-step="page-on" onclick={() => step(1, true)}
					>{t('phone.edit.pageOn')}</button
				>
				{#if phone.dock.show && byKey.has(selected)}
					<button
						type="button"
						class="bar-btn"
						data-step="dock"
						disabled={!shown.dock.includes(selected) && shown.dock.length >= PHONE_DOCK_MAX}
						onclick={() => selected && toggleDock(selected)}>{t('phone.edit.toDock')}</button
					>
				{/if}
			{:else if selected && shown.dock.includes(selected)}
				{@const label = byKey.get(selected)?.label ?? ''}
				<span class="edit-name">{label}</span>
				<button
					type="button"
					class="bar-btn"
					data-step="dock"
					onclick={() => selected && toggleDock(selected)}>{t('phone.edit.fromDock')}</button
				>
			{:else}
				<span class="edit-hint">{t('phone.edit.hint')}</span>
			{/if}
		</div>
	{/if}
	{#if shown.pages.length > 1 || editing}
		<nav class="dots" aria-label={t('phone.pages')}>
			{#each [...shown.pages, ...(editing ? [[]] : [])] as _, n (n)}
				<button
					type="button"
					class="dot-btn"
					aria-label={t('phone.page', { n: n + 1, of: shown.pages.length })}
					aria-current={current === n ? 'page' : undefined}
					onclick={() => showPage(n)}><span class="dot" class:on={current === n}></span></button
				>
			{/each}
		</nav>
	{/if}
	{#if phone.dock.show}
		<nav
			class="dock glass-panel"
			class:target={target?.kind === 'dock'}
			data-phone-dock
			aria-label={t('phone.dock')}
		>
			{#each shown.dock as key (key)}{@render entry(key, true)}{/each}
			{#if editing && shown.dock.length < PHONE_DOCK_MAX}
				<span class="dock-free" aria-hidden="true"></span>
			{/if}
		</nav>
	{/if}
	{#if drag?.moved}
		<div class="ghost" style:left="{drag.x}px" style:top="{drag.y}px" aria-hidden="true">
			{#if drag.key.startsWith('widget:')}
				<span class="ghost-widget glass-panel"
					>{t(WIDGET_TEXT[widgetOf(drag.key)?.kind ?? 'status'][0])}</span
				>
			{:else}
				{@render symbol(drag.key, 62)}
			{/if}
		</div>
	{/if}
</div>

{#if folder}
	<Dialog
		title={folder.name}
		onclose={() => {
			openFolder = null;
			renaming = false;
		}}
	>
		{#if renaming}
			<form
				class="rename"
				onsubmit={(e) => {
					e.preventDefault();
					const id = folder.id;
					change((p) => renameFolder(p, id, folderName));
					renaming = false;
				}}
			>
				<label for="phone-folder-name">{t('phone.folder.name')}</label>
				<input id="phone-folder-name" class="field" bind:value={folderName} maxlength="60" />
				<button type="submit" class="btn btn-primary">{t('common.save')}</button>
			</form>
		{/if}
		<div class="folder-grid">
			{#each folder.apps as key (key)}
				{@const p = byKey.get(key)}
				{#if p}
					<div class="folder-app">
						<button
							type="button"
							class="app in-folder"
							onclick={(e) => {
								const id = folder.id;
								openFolder = null;
								void id;
								open(p, (e.currentTarget as HTMLElement).querySelector('.icon-box'));
							}}
						>
							<span class="icon-box"
								><DesktopIcon
									glyph={p.glyph}
									gradient={p.gradient}
									image={p.image}
									name={p.label}
									size={58}
								/></span
							>
							<span class="label">{p.label}</span>
						</button>
						<button
							type="button"
							class="btn small out"
							data-folder-out={key}
							onclick={() => {
								const id = folder.id;
								change((x) => outOfFolder(x, key, id));
							}}>{t('phone.folder.takeOut')}</button
						>
					</div>
				{/if}
			{/each}
		</div>
		<div class="dialog-actions">
			<button
				type="button"
				class="btn"
				data-folder-rename
				onclick={() => {
					folderName = folder.name;
					renaming = !renaming;
				}}>{t('phone.folder.rename')}</button
			>
			<button
				type="button"
				class="btn"
				data-folder-dissolve
				onclick={() => {
					const id = folder.id;
					openFolder = null;
					change((p) => dissolveFolder(p, id));
				}}>{t('phone.folder.dissolve')}</button
			>
		</div>
	</Dialog>
{/if}

{#if picker}
	<Dialog title={t('phone.widgets.addTitle')} onclose={() => (picker = false)}>
		<ul class="kinds">
			{#each widgetKinds as kind (kind)}
				<li>
					<button type="button" class="kind" data-widget-kind={kind} onclick={() => pick(kind)}>
						<b>{t(WIDGET_TEXT[kind][0])}</b>
						<span class="muted">{t(WIDGET_TEXT[kind][1])}</span>
					</button>
				</li>
			{/each}
		</ul>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (picker = false)}
				>{t('common.cancel')}</button
			>
		</div>
	</Dialog>
{/if}

{#if power}<PowerDialog action={power} onclose={() => (power = null)} />{/if}

<!-- PLAN 66: once after the update to 0.63, for owners and administrators. -->
<MigrationDialog />

<style>
	.load-failed {
		margin: 0;
		padding: var(--space-5);
		color: var(--text);
	}
	.remote-full {
		position: fixed;
		inset: 0;
		z-index: 50;
		display: flex;
		background: var(--bg);
	}
	/* PLAN 87: one screen high – bar, pages, dots, dock; nothing scrolls the page itself. */
	.home {
		position: fixed;
		inset: 0;
		display: flex;
		flex-direction: column;
		overflow: clip;
		overscroll-behavior: none;
		/* Safe areas on every side (iPhone across: the notch is at a side). */
		padding: 0 env(safe-area-inset-right) calc(8px + env(safe-area-inset-bottom))
			env(safe-area-inset-left);
		isolation: isolate;
		-webkit-user-select: none;
		user-select: none;
		-webkit-touch-callout: none;
	}
	.paper {
		position: fixed;
		inset: -4%;
		z-index: -1;
		background: var(--wallpaper);
		/* "Farbstärke" (Settings › Appearance, PLAN 64) reaches the wallpaper too. */
		filter: hue-rotate(var(--tint)) var(--wallpaper-filter, saturate(1));
	}
	.paper.photo {
		inset: 0;
		background: none;
	}
	.live .paper:not(.photo) {
		animation: drift 90s ease-in-out infinite alternate;
	}
	@keyframes drift {
		to {
			transform: translate(-2%, 1.5%) scale(1.04);
		}
	}
	.top {
		flex: none;
		position: relative;
		z-index: 5;
		display: flex;
		gap: var(--space-2);
		align-items: center;
		min-height: 64px;
		padding: calc(var(--space-2-5) + env(safe-area-inset-top)) var(--space-3) var(--space-2-5);
		background: linear-gradient(to bottom, rgb(0 0 0 / 0.28), transparent);
		touch-action: pan-x;
	}
	.light .top {
		background: linear-gradient(to bottom, rgb(255 255 255 / 0.4), transparent);
	}
	.grow {
		flex: 1;
	}
	.node {
		flex: none;
		display: flex;
	}
	.search {
		flex: 1;
		display: flex;
		align-items: center;
		gap: var(--space-2);
		height: 44px;
		padding: 0 14px;
		border-radius: calc(14px * min(var(--radius-scale, 1), 1.6));
		color: var(--muted);
		min-width: 0;
	}
	.search input {
		flex: 1;
		min-width: 0;
		border: 0;
		background: none;
		color: var(--text);
		font: inherit;
		font-size: 16px;
		outline: none;
		-webkit-user-select: text;
		user-select: text;
	}
	.menu-btn,
	.bar-btn {
		flex: none;
		min-height: 44px;
		display: flex;
		align-items: center;
		gap: var(--space-1-5);
		padding: 0 var(--space-3);
		border: 1px solid color-mix(in srgb, var(--text) 18%, transparent);
		border-radius: calc(14px * min(var(--radius-scale, 1), 1.6));
		color: var(--text);
		font: inherit;
		font-size: var(--text-md);
		white-space: nowrap;
		cursor: pointer;
	}
	.bar-btn:disabled {
		opacity: 0.5;
	}
	.bar-btn.done {
		background: var(--accent);
		border-color: var(--accent);
		color: var(--on-accent);
		font-weight: 600;
	}
	.menu-btn.on {
		background: color-mix(in srgb, var(--accent) 30%, var(--surface));
	}
	.menu-btn:focus-visible,
	.bar-btn:focus-visible,
	.app:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	/* A touch beside the open menu closes it. */
	.menu-back {
		position: fixed;
		inset: 0;
		z-index: 4;
		background: rgb(0 0 0 / 0.35);
	}
	.pull-hint {
		position: absolute;
		z-index: 6;
		left: 50%;
		top: calc(64px + env(safe-area-inset-top));
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3);
		border-radius: 999px;
		color: var(--text);
		font-size: var(--text-md);
		transform: translate(-50%, calc(var(--pull) - 60px));
		opacity: min(1, calc(var(--pull) / 70px));
	}
	/* Pages side by side; a swipe snaps to the next (the browser's own scrolling). */
	.pages {
		flex: 1 1 auto;
		min-height: 0;
		display: flex;
		overflow-x: auto;
		overflow-y: hidden;
		scroll-snap-type: x mandatory;
		scrollbar-width: none;
		overscroll-behavior: contain;
		touch-action: pan-x;
	}
	.pages::-webkit-scrollbar {
		display: none;
	}
	.page {
		flex: 0 0 100%;
		scroll-snap-align: start;
		scroll-snap-stop: always;
		overflow-y: auto;
		padding: var(--space-2) var(--space-3);
		position: relative;
	}
	.grid {
		display: grid;
		grid-template-columns: repeat(4, minmax(0, 1fr));
		grid-auto-rows: calc(var(--row) - 16px);
		gap: 16px var(--space-1);
	}
	.empty-page {
		position: absolute;
		inset: 30% 10% auto;
		margin: 0;
		padding: var(--space-4);
		border: 2px dashed color-mix(in srgb, #fff 55%, transparent);
		border-radius: 18px;
		color: #fff;
		text-align: center;
		text-shadow: 0 1px 3px rgb(0 0 0 / 0.65);
	}
	/* Where the dragged entry goes: a bar before or after an entry. */
	.mark-before,
	.mark-after {
		position: relative;
	}
	.mark-before::before,
	.mark-after::after {
		content: '';
		position: absolute;
		top: 4px;
		width: 4px;
		height: 54px;
		border-radius: 2px;
		background: var(--accent);
		box-shadow: 0 0 0 1px #fff;
	}
	.mark-before::before {
		left: -3px;
	}
	.mark-after::after {
		right: -3px;
	}
	.app {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-1-5);
		min-width: 0;
		min-height: 44px;
		padding: 0;
		border: 0;
		background: none;
		color: #fff;
		font: inherit;
		font-size: 11.5px;
		font-weight: 500;
		text-align: center;
		text-shadow: 0 1px 3px rgb(0 0 0 / 0.65);
		cursor: pointer;
		-webkit-tap-highlight-color: transparent;
	}
	.light .app {
		color: var(--text);
		text-shadow: 0 0 3px rgb(255 255 255 / 0.9);
	}
	.label {
		max-width: 84px;
		hyphens: auto;
		overflow-wrap: anywhere;
		line-height: 1.2;
	}
	.wrap {
		position: relative;
		display: block;
	}
	.icon-box {
		position: relative;
		display: block;
	}
	.folder-box {
		width: var(--s);
		height: var(--s);
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 3px;
		padding: 6px;
		place-items: center;
		border-radius: calc(var(--s) * 0.26);
		background: color-mix(in srgb, var(--surface) 55%, transparent);
		-webkit-backdrop-filter: blur(var(--desk-blur, 22px));
		backdrop-filter: blur(var(--desk-blur, 22px));
		border: 1px solid color-mix(in srgb, #fff 22%, transparent);
	}
	.badge {
		position: absolute;
		top: -6px;
		right: -6px;
		min-width: 20px;
		height: 20px;
		padding: 0 5px;
		border-radius: 10px;
		background: #d93a3f;
		color: #fff;
		font-size: 11px;
		font-weight: 700;
		display: grid;
		place-items: center;
		text-shadow: none;
	}
	.badge.warn {
		background: #f0a91a;
		color: #2b2100;
	}
	/* Arranging: the symbols wiggle (not with reduced motion; then a dashed ring says it). */
	.editing .app .wrap,
	.editing .widget {
		outline: 2px dashed color-mix(in srgb, #fff 55%, transparent);
		outline-offset: 3px;
		border-radius: 16px;
	}
	.editing.wiggle .app .wrap {
		outline: none;
		animation: wiggle 0.28s ease-in-out infinite alternate;
	}
	.editing.wiggle .grid > :nth-child(2n) .wrap {
		animation-delay: -0.14s;
	}
	@keyframes wiggle {
		from {
			transform: rotate(-2deg);
		}
		to {
			transform: rotate(2deg);
		}
	}
	.editing .app,
	.editing .widget {
		touch-action: none;
	}
	.app.chosen .wrap,
	.widget.chosen {
		outline: 3px solid var(--accent) !important;
		outline-offset: 3px;
		animation: none !important;
	}
	.lifted {
		opacity: 0.25;
	}
	.app.into .wrap {
		transform: scale(1.12);
		animation: none !important;
		outline: 3px solid var(--accent) !important;
		border-radius: 18px;
	}
	.ghost {
		position: fixed;
		z-index: 70;
		pointer-events: none;
		transform: translate(-50%, -60%) scale(1.08);
		filter: drop-shadow(0 10px 18px rgb(0 0 0 / 0.45));
	}
	.ghost-widget {
		display: block;
		padding: var(--space-4) var(--space-5);
		border-radius: 18px;
		color: var(--text);
		font-weight: 600;
		white-space: nowrap;
	}
	.widget {
		position: relative;
		grid-row: span 2;
		border-radius: 20px;
		overflow: hidden;
		color: var(--text);
		min-width: 0;
	}
	.widget-body {
		height: 100%;
		overflow-y: auto;
		padding: var(--space-3);
	}
	.widget-tools {
		position: absolute;
		top: 6px;
		right: 6px;
		display: flex;
		gap: 6px;
	}
	.tool {
		width: 44px;
		height: 44px;
		display: grid;
		place-items: center;
		border: 1px solid var(--border);
		border-radius: 50%;
		background: var(--surface);
		color: var(--text);
		cursor: pointer;
	}
	.tool.remove {
		background: var(--status-error);
		border-color: var(--status-error);
		color: var(--on-warn);
	}
	.dots {
		flex: none;
		display: flex;
		justify-content: center;
		gap: 0;
	}
	.dot-btn {
		width: 28px;
		height: 28px;
		display: grid;
		place-items: center;
		padding: 0;
		border: 0;
		background: none;
		cursor: pointer;
	}
	.dot {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: color-mix(in srgb, #fff 45%, transparent);
		box-shadow: 0 0 2px rgb(0 0 0 / 0.6);
	}
	/* The page shown: larger and full white, not by colour alone. */
	.dot.on {
		width: 10px;
		height: 10px;
		background: #fff;
	}
	.light .dot {
		background: color-mix(in srgb, var(--text) 35%, transparent);
		box-shadow: none;
	}
	.light .dot.on {
		background: var(--text);
	}
	.dock {
		flex: none;
		display: flex;
		justify-content: space-around;
		align-items: center;
		gap: var(--space-2);
		min-height: 80px;
		margin: var(--space-1) var(--space-3) 0;
		padding: var(--space-2) var(--space-3);
		border-radius: 28px;
	}
	.dock.target {
		outline: 3px solid var(--accent);
	}
	.dock-free {
		width: 54px;
		height: 54px;
		border: 2px dashed color-mix(in srgb, #fff 50%, transparent);
		border-radius: 14px;
	}
	.edit-bar {
		flex: none;
		margin: var(--space-1) var(--space-3);
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-1-5);
		padding: var(--space-2);
		border-radius: 18px;
		color: var(--text);
	}
	.edit-bar .bar-btn {
		border-radius: 12px;
		background: var(--chip);
	}
	.edit-name {
		flex: 1 1 100%;
		font-weight: 600;
		overflow-wrap: anywhere;
	}
	.edit-hint {
		flex: 1;
		overflow-wrap: break-word;
	}
	.folder-grid {
		display: grid;
		grid-template-columns: repeat(3, minmax(0, 1fr));
		gap: var(--space-4) var(--space-2);
	}
	.folder-app {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-1-5);
	}
	.app.in-folder {
		color: var(--text);
		text-shadow: none;
		font-size: var(--text-sm);
	}
	.out {
		min-height: 44px;
	}
	.rename {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
	}
	.rename .field {
		flex: 1 1 12rem;
		font-size: 16px;
	}
	.kinds {
		margin: 0;
		padding: 0;
		list-style: none;
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
	}
	.kind {
		width: 100%;
		min-height: 52px;
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: 2px;
		padding: var(--space-2-5) var(--space-3);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		background: var(--chip);
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.muted {
		color: var(--muted);
	}
	@media (prefers-reduced-motion: reduce) {
		.live .paper {
			animation: none;
		}
	}
</style>
