<script lang="ts">
	import { onDestroy, untrack } from 'svelte';
	// PLAN 61: one window of the desktop. TitanOS programs show their pages
	// in a frame of the same origin with the program's own side bar; Docker
	// apps always show their remote view (PLAN 63: the app runs in a browser
	// on the NAS, its picture comes over the TitanOS address).
	import Icon from '../components/Icon.svelte';
	import { t } from '../i18n/index.svelte.ts';
	import DesktopIcon from './DesktopIcon.svelte';
	import { pageDrawsSide, sideActive, type SideItem } from './apps.ts';
	import { frameMessage, type FrameMessage } from './frame.ts';
	import { openProgram, openTab as openProgramTab } from './programs.svelte.ts';
	import Dialog from '../components/Dialog.svelte';
	import '../components/menu.css';
	import WebAppView from '../webapps/WebAppView.svelte';
	import { webAppById, webapps } from '../webapps/webapps.svelte.ts';
	import { baseName } from '../webapps/protocol.ts';
	import type RemoteViewType from './RemoteView.svelte';
	import { clearFrameCommands } from './menubar/state.svelte.ts';
	import { sources } from './sources.svelte.ts';
	import DirectView from './DirectView.svelte';
	import { chooseDirect, directChosen, hasOwnStream } from './direct.ts';
	import { registerWindowMenu } from './window-commands.ts';
	import { WindowDrag } from './window-drag.svelte.ts';
	import { hookDeskKeys } from './window-keys.ts';
	import WindowMenu from './WindowMenu.svelte';
	import WindowSide from './WindowSide.svelte';
	import {
		closeWindow,
		requestClose,
		setFrameUnsaved,
		unsavedKind,
		desktop,
		focus,
		minimize,
		frameListening,
		navigateWindow,
		openWindow,
		registerFrame,
		saveWindows,
		savePrefs,
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
	let restartAsk = $state(false);
	const docName = $derived(win.docName || (win.doc ? baseName(win.doc.path) : ''));
	const closing = $derived(wm.closing.includes(win.id));
	// Only running work (an upload), no changed fields: the question says so.
	const busyOnly = $derived(closing && unsavedKind(win.id) === 'busy');

	let frame = $state<HTMLIFrameElement>();
	// The frame loads its first page once; later pages are reached inside it.
	const src = untrack(() => win.href);

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
		// A page loaded anew holds no unsaved work yet (it says so itself once it does).
		setFrameUnsaved(win.id, '');
		tellVisible();
		tellSide();
		// PLAN 67-1: the new page announces its menu commands; ask in case it already did.
		clearFrameCommands(win.id);
		frame?.contentWindow?.postMessage({ type: 'titanos-menu-get' }, location.origin);
		try {
			hookDeskKeys(frame?.contentWindow);
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

	const rect = $derived(
		snapRect(win.snap) ?? clearOfDock({ x: win.x, y: win.y, w: win.w, h: win.h }),
	);
	const foldedPref = $derived(desktop.prefs.folded.includes(foldKey));
	// PLAN 76 (F-12-15): in a narrow window the side bar folds to its icons by
	// itself, so the page keeps the width of a phone and nothing is cut off.
	// Unfolding it there holds until the window is wide again.
	const NARROW = 620;
	let heldOpen = $state(false);
	const narrow = $derived(rect.w < NARROW);
	$effect(() => {
		if (!narrow) heldOpen = false;
	});
	const folded = $derived(narrow ? !heldOpen : foldedPref);
	function toggleFold() {
		if (narrow) {
			heldOpen = !heldOpen;
			return;
		}
		savePrefs((p) => {
			p.folded = foldedPref ? p.folded.filter((k) => k !== foldKey) : [...p.folded, foldKey];
		});
	}

	const focused = $derived(wm.focused === win.id);
	const path = $derived(win.href.split('?')[0]);
	// PLAN 69: on these pages the page itself draws the one side bar.
	const ownSide = $derived(pageDrawsSide(side, path));

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
		} else if (msg?.type === 'titanos-unsaved') {
			setFrameUnsaved(win.id, msg.state);
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

	// Moving and resizing (window-drag.svelte.ts).
	const drag = new WindowDrag(
		() => win,
		() => rect,
	);
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
	let sideBar = $state<WindowSide>();
	registerWindowMenu({
		win: () => win,
		external: () => external,
		webapp: () => webapp,
		view: () => view,
		remote: () => remote,
		restartApp,
		openTab,
		focusSearch: () => sideBar?.focusSearch(),
	});
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
	class:dragging={drag.dragging}
	class:snapped={!!win.snap}
	class:min={win.minimized}
	class:grow={!!win.from}
	style:left="{rect.x}px"
	style:top="{rect.y}px"
	style:width="{rect.w}px"
	style:height="{rect.h}px"
	style:z-index={win.z}
	style:transform={drag.shift.x || drag.shift.y
		? `translate3d(${drag.shift.x}px, ${drag.shift.y}px, 0)`
		: undefined}
	style:--from-x="{(win.from?.x ?? rect.x) - rect.x}px"
	style:--from-y="{(win.from?.y ?? rect.y) - rect.y}px"
	aria-label={win.title}
	onpointerdown={() => focus(win.id)}
	onkeydown={drag.keyResize}
	onanimationend={() => (win.from = undefined)}
>
	{#if side.length && !external && !ownSide}
		<WindowSide
			bind:this={sideBar}
			{win}
			{glyph}
			{gradient}
			{side}
			{folded}
			{path}
			ontoggle={toggleFold}
		/>
	{/if}
	<div class="main">
		<!-- svelte-ignore a11y_no_static_element_interactions -->
		<header class="titlebar" onpointerdown={drag.startMove} ondblclick={() => toggleMax(win.id)}>
			{#if !side.length || external || ownSide}<DesktopIcon
					{glyph}
					{gradient}
					{image}
					size={22}
					name={win.title}
				/>{/if}
			<!-- Program and page: the page's name takes a second line before a word is broken. -->
			<span class="titles"
				><b class="title">{win.title}</b>{#if webappId}{#if docName}<span class="title page"
							><span class="crumb" aria-hidden="true">·</span>{docName}</span
						>{/if}{:else if crumb}<span class="title page"
						><span class="crumb" aria-hidden="true">›</span>{crumb}</span
					>{/if}</span
			>
			<span class="fill"></span>
			{#if webappId || external}
				<WindowMenu
					bind:moreOpen
					bind:remoteHost
					{external}
					{webapp}
					{view}
					{ownStream}
					{direct}
					{directOn}
					{directFailed}
					{switchDirect}
					{openTab}
					{restartApp}
				/>
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
			{#if drag.dragging || (!focused && !(webappId && wm.fileDrag))}<div
					class="shield"
					aria-hidden="true"
				></div>{/if}
		</div>
	</div>
	{#if closing}
		<Dialog
			title={t(busyOnly ? 'desktop.closeBusyTitle' : 'webapps.unsavedTitle')}
			onclose={() => (wm.closing = wm.closing.filter((c) => c !== win.id))}
		>
			<p>
				{t(busyOnly ? 'desktop.closeBusyText' : 'webapps.unsavedText', { app: win.title })}
			</p>
			<div class="dialog-actions">
				<button
					type="button"
					class="btn btn-primary"
					onclick={() => (wm.closing = wm.closing.filter((c) => c !== win.id))}
					>{t('common.cancel')}</button
				>
				<button type="button" class="btn btn-danger" onclick={() => closeWindow(win.id)}
					>{t(busyOnly ? 'desktop.closeBusyAnyway' : 'webapps.closeAnyway')}</button
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
			<div class="edge {edge}" onpointerdown={(e) => drag.startResize(e, edge)}></div>
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
	.ctl:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.main {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
	}
	.titlebar {
		min-height: 42px;
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
	/* Never cut off (owner 10.11.2026): a long title wraps, the bar grows with it. */
	.titles {
		flex: 0 1 auto;
		min-width: 0;
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		column-gap: 9px;
		padding: 5px 0;
	}
	.title {
		min-width: 0;
		line-height: 1.2;
		overflow-wrap: break-word;
		hyphens: auto;
	}
	.title .crumb {
		margin: 0 6px 0 0;
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
