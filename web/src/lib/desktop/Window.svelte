<script lang="ts">
	import { untrack } from 'svelte';
	// PLAN 61: one window of the desktop. TitanOS programs show their pages
	// in a frame of the same origin with the program's own side bar; Docker
	// apps always show their remote view (PLAN 63: the app runs in a browser
	// on the NAS, its picture comes over the TitanOS address).
	import Icon from '../components/Icon.svelte';
	import { t } from '../i18n/index.svelte.ts';
	import DesktopIcon from './DesktopIcon.svelte';
	import { sideActive, type SideItem } from './apps.ts';
	import { frameMessage } from './frame.ts';
	import { openTab as openProgramTab } from './programs.svelte.ts';
	import RemoteView from './RemoteView.svelte';
	import {
		closeWindow,
		desktop,
		focus,
		minimize,
		navigateWindow,
		registerFrame,
		saveWindows,
		savePrefs,
		setSnap,
		snapAt,
		snapRect,
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

	const folded = $derived(desktop.prefs.folded.includes(foldKey));
	function toggleFold() {
		savePrefs((p) => {
			p.folded = folded ? p.folded.filter((k) => k !== foldKey) : [...p.folded, foldKey];
		});
	}

	const rect = $derived(snapRect(win.snap) ?? { x: win.x, y: win.y, w: win.w, h: win.h });
	const focused = $derived(wm.focused === win.id);
	const path = $derived(win.href.split('?')[0]);

	// The frame says which page it shows; the address of the browser follows the window in front.
	function message(event: MessageEvent) {
		if (!frame || event.source !== frame.contentWindow) return;
		const msg = frameMessage(event);
		if (msg?.type === 'titanos-page') {
			win.href = msg.path;
			if (focused) onaddress?.(msg.path);
			saveWindows();
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
		if (focused && !external) onaddress?.(win.href);
	});

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
			if (edge.includes('s')) win.h = Math.max(MIN_H, start.h + dy);
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
		void openProgramTab(win.app, win.href);
	}
	const EDGES = ['n', 's', 'e', 'w', 'ne', 'nw', 'se', 'sw'];
</script>

<svelte:window onmessage={message} />

<section
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
	{#if side.length && !external}
		<nav class="side" class:folded aria-label={win.title}>
			<div class="side-title">
				<DesktopIcon {glyph} {gradient} size={folded ? 32 : 28} name={win.title} />
				{#if !folded}<b>{win.title}</b>{/if}
			</div>
			<div class="items">
				{#each side as item, i (i)}
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
						<button
							type="button"
							class="item"
							class:on={sideActive(item, path, side)}
							title={folded ? t(item.label) : undefined}
							aria-current={sideActive(item, path, side) ? 'page' : undefined}
							onclick={() => navigateWindow(win.id, item.href)}
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
			{#if !side.length || external}<DesktopIcon
					{glyph}
					{gradient}
					{image}
					size={22}
					name={win.title}
				/>{/if}
			<b class="title">{win.title}</b>
			{#if external}<span class="addr"
					>{win.href.replace(/^https?:\/\//, '').replace(/\/$/, '')}</span
				>{/if}
			<span class="fill"></span>
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
				onclick={() => closeWindow(win.id)}
				title={t('desktop.window.close')}
				aria-label={t('desktop.window.close')}><Icon name="close" size={15} /></button
			>
		</header>
		<div class="body">
			{#if external}
				<RemoteView app={win.app.replace(/^docker:/, '')} title={win.title} onopentab={openTab} />
			{:else}
				<iframe
					bind:this={frame}
					data-window={win.id}
					{src}
					title={win.title}
					allow="clipboard-read; clipboard-write; fullscreen; publickey-credentials-get; publickey-credentials-create; camera; microphone"
				></iframe>
			{/if}
			{#if dragging || !focused}<div class="shield" aria-hidden="true"></div>{/if}
		</div>
	</div>
	{#if !win.snap}
		{#each EDGES as edge (edge)}
			<!-- svelte-ignore a11y_no_static_element_interactions -->
			<div class="edge {edge}" onpointerdown={(e) => startResize(e, edge)}></div>
		{/each}
	{/if}
</section>

<style>
	.win {
		position: absolute;
		display: flex;
		border-radius: 14px;
		overflow: hidden;
		background: var(--bg);
		border: 1px solid var(--border);
		box-shadow: 0 10px 28px rgb(0 0 0 / 0.35);
	}
	.win.focused {
		box-shadow: 0 24px 60px rgb(0 0 0 / 0.45);
	}
	/* While moving: one layer for the GPU and a lighter shadow (smooth in every browser). */
	.win.dragging {
		will-change: transform;
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
	.side {
		width: 220px;
		flex: none;
		display: flex;
		flex-direction: column;
		padding: 12px 10px;
		background: color-mix(in srgb, var(--surface) 88%, transparent);
		border-right: 1px solid var(--border);
		overflow: hidden;
	}
	.side.folded {
		width: 60px;
		align-items: center;
		padding: 12px 8px;
	}
	.side-title {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 4px 6px 14px;
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
		display: flex;
		flex-direction: column;
		gap: 2px;
		width: 100%;
	}
	.group {
		margin: 12px 10px 4px;
		font-size: var(--text-xs);
		color: var(--muted);
		text-transform: uppercase;
		letter-spacing: 0.05em;
	}
	.item.thing {
		padding-left: 16px;
	}
	.state {
		flex: none;
		width: 9px;
		height: 9px;
		margin: 0 4px;
		border-radius: 50%;
		border: 1.5px solid var(--muted);
	}
	.state.on {
		border-color: var(--ok-text);
		background: var(--ok-text);
	}
	.rule {
		height: 1px;
		margin: 8px 6px;
		background: var(--border);
	}
	.item {
		display: flex;
		align-items: center;
		gap: 10px;
		min-height: 36px;
		padding: 6px 10px;
		border: 0;
		border-radius: 9px;
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
		color: var(--accent);
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
		margin-top: 6px;
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
		display: flex;
		align-items: center;
		gap: 9px;
		padding: 0 6px 0 14px;
		background: var(--header);
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
	.addr {
		color: var(--muted);
		font-size: var(--text-sm);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		min-width: 0;
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
		border-radius: 7px;
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
	.body {
		position: relative;
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
