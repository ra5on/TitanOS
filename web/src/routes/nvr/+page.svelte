<script lang="ts">
	// PLAN 79-7 (drawing 79-nvr/live2, approved): the live view. Every camera
	// lies where the account put it, in the size it was given – moved and
	// sized like the desktop's widgets ("Anordnen"), stored per account with
	// the desktop's settings. Events beside the pictures are an option. At
	// phone width the tiles stand below each other; their order is the
	// account's own too.
	import { untrack } from 'svelte';
	import { page } from '$app/state';
	import Dropdown from '#lib/components/Dropdown.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import EventCard from '#lib/nvr/EventCard.svelte';
	import { feed, watchFeed } from '#lib/nvr/feed.svelte.ts';
	import {
		defaultNvrPrefs,
		dragTile,
		gridTiles,
		layoutRows,
		LIVE_COLS,
		phoneOrderFor,
		tilesFor,
		type NvrPrefs,
		type Tile,
	} from '#lib/nvr/layout.ts';
	import LiveTile from '#lib/nvr/LiveTile.svelte';
	import { changeNvrPrefs, connectNvrPrefs, nvrLink } from '#lib/nvr/nvr-link.svelte.ts';
	import { cameraName, mayLive, mayRecordings, nvr, nvrAdmin } from '#lib/nvr/nvr.svelte.ts';

	const admin = $derived(nvrAdmin());
	const running = $derived(nvr.status?.state === 'running');
	const cameras = $derived(nvr.cameras.filter(mayLive));
	const ids = $derived(cameras.map((c) => c.id));
	const byId = $derived(new Map(cameras.map((c) => [c.id, c])));
	const anyRecordings = $derived(nvr.cameras.some(mayRecordings));

	$effect(() => connectNvrPrefs());
	$effect(() => untrack(() => watchFeed()));
	const prefs = $derived<NvrPrefs>(nvrLink.prefs ?? defaultNvrPrefs());

	// ---- The wall ----
	let wall = $state<HTMLElement>();
	let width = $state(0);
	$effect(() => {
		if (!wall) return;
		const observer = new ResizeObserver(() => (width = wall?.clientWidth ?? 0));
		observer.observe(wall);
		width = wall.clientWidth;
		return () => observer.disconnect();
	});
	/** Below this width the tiles stand below each other (phone). */
	const stacked = $derived(width > 0 && width < 560);
	const cell = $derived(width / LIVE_COLS);
	const GAP = 6;

	let arranging = $state(false);
	/** The tiles while they are arranged; saved with "Fertig". */
	let draft = $state<Tile[] | null>(null);
	let order = $state<string[] | null>(null);
	const tiles = $derived(draft ?? tilesFor(prefs, ids));
	const stack = $derived(order ?? phoneOrderFor(prefs, ids));
	const rows = $derived(layoutRows(tiles));
	let active = $state<{ id: string; w: number; h: number } | null>(null);

	function arrange() {
		solo = '';
		arranging = true;
		draft = tilesFor(prefs, ids).map((tile) => ({ ...tile }));
		order = phoneOrderFor(prefs, ids);
	}
	function done() {
		const next = draft;
		const nextOrder = order;
		arranging = false;
		draft = null;
		order = null;
		active = null;
		changeNvrPrefs((p) => {
			if (stacked) {
				if (nextOrder) p.phoneOrder = nextOrder;
			} else if (next) {
				p.tiles = next;
				p.custom = true;
			}
		});
	}
	function reset() {
		if (arranging) {
			draft = gridTiles(ids);
			order = [...ids];
			return;
		}
		changeNvrPrefs((p) => {
			p.tiles = [];
			p.custom = false;
			p.phoneOrder = [];
		});
	}

	function startDrag(event: PointerEvent, tile: Tile, mode: 'move' | 'size') {
		if (!arranging || event.button !== 0 || !draft || !cell) return;
		event.preventDefault();
		event.stopPropagation();
		const start = { ...tile };
		const from = { x: event.clientX, y: event.clientY };
		const move = (e: PointerEvent) => {
			const next = dragTile(
				start,
				mode,
				Math.round((e.clientX - from.x) / cell),
				Math.round((e.clientY - from.y) / cell),
			);
			draft = (draft ?? []).map((other) => (other.id === tile.id ? next : other));
			active = { id: tile.id, w: next.w, h: next.h };
		};
		const up = () => {
			window.removeEventListener('pointermove', move);
			window.removeEventListener('pointerup', up);
			window.removeEventListener('pointercancel', up);
			active = null;
		};
		active = { id: tile.id, w: tile.w, h: tile.h };
		// The dragged tile lies on top.
		draft = [...draft.filter((other) => other.id !== tile.id), start];
		window.addEventListener('pointermove', move);
		window.addEventListener('pointerup', up);
		window.addEventListener('pointercancel', up);
	}
	// Arranging with the keyboard: arrows move, Shift + arrows size.
	function tileKey(event: KeyboardEvent, tile: Tile) {
		if (!arranging || !draft) return;
		const step: Record<string, [number, number]> = {
			ArrowLeft: [-1, 0],
			ArrowRight: [1, 0],
			ArrowUp: [0, -1],
			ArrowDown: [0, 1],
		};
		const by = step[event.key];
		if (!by) return;
		event.preventDefault();
		const next = dragTile(tile, event.shiftKey ? 'size' : 'move', by[0], by[1]);
		draft = draft.map((other) => (other.id === tile.id ? next : other));
	}
	function moveInStack(id: string, by: number) {
		const list = [...(order ?? stack)];
		const at = list.indexOf(id);
		const to = at + by;
		if (at < 0 || to < 0 || to >= list.length) return;
		[list[at], list[to]] = [list[to], list[at]];
		order = list;
	}

	// ---- One camera large, full screen ----
	let solo = $state('');
	/** The large camera opens with its pad for moving it. */
	let soloPtz = $state(false);
	function showSolo(id: string, ptz = false) {
		soloPtz = ptz;
		solo = id;
	}
	$effect(() => {
		// A link from the desktop's widget or an event: /nvr?camera=<id>.
		const wanted = page.url.searchParams.get('camera');
		if (wanted && byId.has(wanted)) solo = wanted;
	});
	let root = $state<HTMLElement>();
	let full = $state(false);
	$effect(() => {
		const change = () => (full = !!document.fullscreenElement);
		document.addEventListener('fullscreenchange', change);
		return () => document.removeEventListener('fullscreenchange', change);
	});
	function fullscreen() {
		if (document.fullscreenElement) void document.exitFullscreen().catch(() => {});
		else void root?.requestFullscreen?.().catch(() => {});
	}
	function keys(event: KeyboardEvent) {
		if (event.key === 'Escape' && solo && !document.fullscreenElement) solo = '';
	}

	const OPTIONS: { key: 'events' | 'names' | 'marks' | 'recDot' | 'hideBar'; label: MessageKey }[] =
		[
			{ key: 'events', label: 'nvr.view.events' },
			{ key: 'names', label: 'nvr.view.names' },
			{ key: 'marks', label: 'nvr.view.marks' },
			{ key: 'recDot', label: 'nvr.view.recDot' },
			{ key: 'hideBar', label: 'nvr.view.hideBar' },
		];
	const showEvents = $derived(prefs.events && anyRecordings && !arranging);
	const nameOf = cameraName;
</script>

<svelte:window onkeydown={keys} />

<PageHead title={t('nvr.menu.live')} />

{#if cameras.length === 0}
	{#if admin && nvr.cameras.length === 0}
		<EmptyState title={t('nvr.live.noneTitle')} text={t('nvr.live.noneAdmin')}>
			<a class="btn btn-primary" href="/nvr/cameras?new=1">{t('nvr.add.title')}</a>
		</EmptyState>
	{:else if !admin && anyRecordings}
		<EmptyState title={t('nvr.live.noneTitle')} text={t('nvr.live.onlyRecordings')}>
			<a class="btn btn-primary" href="/nvr/recordings">{t('nvr.menu.recordings')}</a>
			<a class="btn" href="/nvr/events">{t('nvr.menu.events')}</a>
		</EmptyState>
	{:else}
		<EmptyState
			title={t('nvr.live.noneTitle')}
			text={admin ? t('nvr.live.noneAdmin') : t('nvr.live.noneUser')}
		/>
	{/if}
{:else}
	<div class="live" class:full class:hide-bar={full && prefs.hideBar} bind:this={root}>
		<div class="live-bar" role="toolbar" aria-label={t('nvr.live.toolbar')}>
			{#if arranging}
				<span class="hint">{stacked ? t('nvr.arrange.hintPhone') : t('nvr.arrange.hint')}</span>
				<span class="spacer"></span>
				<button type="button" class="btn" onclick={reset}>{t('nvr.arrange.reset')}</button>
				<button type="button" class="btn btn-primary" onclick={done}
					><Icon name="check" size={15} />{t('nvr.arrange.done')}</button
				>
			{:else}
				<span class="spacer"></span>
				{#if anyRecordings}
					<button
						type="button"
						class="btn"
						class:on={prefs.events}
						aria-pressed={prefs.events}
						onclick={() => changeNvrPrefs((p) => (p.events = !p.events))}
						><Icon name="clock" size={15} />{t('nvr.menu.events')}</button
					>
				{/if}
				<button type="button" class="btn" onclick={arrange}
					><Icon name="edit" size={15} />{t('nvr.arrange.start')}</button
				>
				{#if typeof document !== 'undefined' && document.fullscreenEnabled}
					<button type="button" class="btn" aria-pressed={full} onclick={fullscreen}
						><Icon name="maximize" size={15} />{full
							? t('nvr.live.fullOff')
							: t('nvr.live.full')}</button
					>
				{/if}
				<Dropdown
					label={t('nvr.view.title')}
					text={t('nvr.view.title')}
					icon="eye"
					kind="panel"
					align="right"
				>
					<div class="view-menu">
						{#each OPTIONS as option (option.key)}
							{#if option.key !== 'events' || anyRecordings}
								<label class="view-row">
									<span>{t(option.label)}</span>
									<input
										type="checkbox"
										role="switch"
										class="switch"
										checked={prefs[option.key]}
										onchange={(e) => {
											const on = e.currentTarget.checked;
											changeNvrPrefs((p) => (p[option.key] = on));
										}}
									/>
								</label>
							{/if}
						{/each}
						<button type="button" class="view-reset" onclick={reset}>{t('nvr.view.reset')}</button>
					</div>
				</Dropdown>
			{/if}
		</div>
		<div class="body">
			<div
				class="wall"
				class:arranging
				class:stacked
				class:has-solo={!!solo}
				bind:this={wall}
				style:height={stacked || solo ? undefined : `${rows * cell}px`}
				style:--cell="{cell}px"
			>
				{#if solo && byId.has(solo)}
					<div class="tile solo">
						<LiveTile
							camera={byId.get(solo)!}
							{running}
							{prefs}
							activity={feed.activity[solo]}
							solo
							ptzStart={soloPtz}
							onsolo={() => (solo = '')}
						/>
					</div>
				{:else if stacked}
					{#each stack as id, i (id)}
						{@const camera = byId.get(id)}
						{#if camera}
							<div class="tile row">
								<LiveTile
									{camera}
									{running}
									{prefs}
									activity={feed.activity[id]}
									{arranging}
									small={admin && !!camera.source?.subURL}
									onsolo={() => showSolo(id)}
									onptz={() => showSolo(id, true)}
								/>
								{#if arranging}
									<div class="reorder">
										<button
											type="button"
											class="btn"
											disabled={i === 0}
											aria-label={t('nvr.arrange.up', { name: camera.name })}
											onclick={() => moveInStack(id, -1)}><Icon name="arrowUp" size={16} /></button
										>
										<button
											type="button"
											class="btn"
											disabled={i === stack.length - 1}
											aria-label={t('nvr.arrange.down', { name: camera.name })}
											onclick={() => moveInStack(id, 1)}><Icon name="arrowDown" size={16} /></button
										>
									</div>
								{/if}
							</div>
						{/if}
					{/each}
				{:else if cell > 0}
					{#each tiles as tile (tile.id)}
						{@const camera = byId.get(tile.id)}
						{#if camera}
							<!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_static_element_interactions -->
							<div
								class="tile free"
								class:active={active?.id === tile.id}
								data-tile={tile.id}
								role={arranging ? 'group' : undefined}
								aria-label={arranging ? t('nvr.arrange.tile', { name: camera.name }) : undefined}
								tabindex={arranging ? 0 : undefined}
								style:left="{tile.x * cell}px"
								style:top="{tile.y * cell}px"
								style:width="{tile.w * cell - GAP}px"
								style:height="{tile.h * cell - GAP}px"
								onpointerdown={(e) => startDrag(e, tile, 'move')}
								onkeydown={(e) => tileKey(e, tile)}
							>
								<LiveTile
									{camera}
									{running}
									{prefs}
									activity={feed.activity[tile.id]}
									{arranging}
									small={admin && !!camera.source?.subURL && tile.w * cell < 520}
									onsolo={() => showSolo(tile.id)}
									onptz={() => showSolo(tile.id, true)}
								/>
								{#if arranging}
									{#if active?.id === tile.id}
										<span class="size"
											>{Math.round(active.w * cell - GAP)} × {Math.round(
												active.h * cell - GAP,
											)}</span
										>
									{/if}
									<span class="grip" onpointerdown={(e) => startDrag(e, tile, 'size')}></span>
								{/if}
							</div>
						{/if}
					{/each}
				{/if}
			</div>
			{#if showEvents}
				<aside class="events" aria-label={t('nvr.menu.events')}>
					<h2>{t('nvr.menu.events')}</h2>
					{#if !feed.available}
						<p class="muted">{t('nvr.events.unavailable')}</p>
					{:else if feed.events.length === 0}
						<p class="muted">{t('nvr.events.none')}</p>
					{:else}
						<ul>
							{#each feed.events.slice(0, 8) as event (event.id)}
								<li>
									<EventCard
										{event}
										camera={nameOf(event.camera)}
										href="/nvr/events?event={encodeURIComponent(event.id)}"
									/>
								</li>
							{/each}
						</ul>
						<a class="all" href="/nvr/events">{t('nvr.events.all')}</a>
					{/if}
				</aside>
			{/if}
		</div>
	</div>
{/if}

<style>
	.live {
		display: flex;
		flex-direction: column;
		gap: var(--space-2-5);
		min-width: 0;
	}
	.live.full {
		padding: var(--space-2-5);
		background: var(--bg);
		overflow: auto;
	}
	.live.hide-bar .live-bar {
		display: none;
	}
	.live-bar {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
		min-width: 0;
	}
	.live-bar .btn {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
		min-height: 32px;
	}
	.live-bar .btn.on {
		border-color: var(--accent);
		background: color-mix(in srgb, var(--accent) 14%, var(--surface));
	}
	.spacer {
		flex: 1 1 0;
	}
	.hint {
		flex: 1 1 14rem;
		color: var(--muted);
		font-size: var(--text-base);
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.body {
		display: flex;
		align-items: flex-start;
		gap: var(--space-3);
		min-width: 0;
	}
	.wall {
		position: relative;
		flex: 1 1 0;
		min-width: 0;
	}
	.wall.arranging:not(.stacked) {
		min-height: calc(var(--cell) * 12);
		border-radius: 10px;
		background-image:
			linear-gradient(to right, var(--border) 1px, transparent 1px),
			linear-gradient(to bottom, var(--border) 1px, transparent 1px);
		background-size: var(--cell) var(--cell);
	}
	.wall.stacked {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}
	.tile {
		position: relative;
	}
	.tile.free {
		position: absolute;
	}
	.tile.row {
		flex: none;
		width: 100%;
		aspect-ratio: 16 / 9;
	}
	/* At least 340 px high: on a phone in portrait the picture is letterboxed
	   and the pad for moving the camera has room. */
	.tile.solo {
		width: 100%;
		height: min(calc(100dvh - 120px), max(56.25vw, 340px));
		min-height: 240px;
	}
	.live.full .tile.solo {
		height: calc(100dvh - 20px);
	}
	.live.full:not(.hide-bar) .tile.solo {
		height: calc(100dvh - 70px);
	}
	.arranging .tile.free {
		cursor: move;
		user-select: none;
		touch-action: none;
		outline: 2px dashed color-mix(in srgb, var(--accent) 70%, transparent);
		outline-offset: -2px;
		border-radius: 10px;
	}
	.arranging .tile.free.active,
	.arranging .tile.free:focus-visible {
		outline-style: solid;
		z-index: 2;
	}
	.size {
		position: absolute;
		top: 8px;
		right: 8px;
		padding: 2px 8px;
		border-radius: 6px;
		background: var(--accent);
		color: #fff;
		font-size: var(--text-sm);
		font-weight: 600;
	}
	.grip {
		position: absolute;
		right: 0;
		bottom: 0;
		width: 22px;
		height: 22px;
		border-radius: 4px 0 10px 0;
		background: var(--accent);
		cursor: nwse-resize;
		touch-action: none;
	}
	.reorder {
		position: absolute;
		top: 8px;
		right: 8px;
		display: flex;
		gap: 4px;
	}
	/* Over the dark picture the arrows need a solid ground of their own. */
	.reorder .btn {
		display: grid;
		place-items: center;
		width: 40px;
		height: 40px;
		padding: 0;
		background: var(--surface);
		color: var(--text);
	}
	.reorder .btn:disabled {
		opacity: 0.55;
	}
	.events {
		flex: 0 0 min(17rem, 34%);
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		min-width: 0;
	}
	.events h2 {
		margin: 0;
		font-size: var(--text-md);
		font-weight: 600;
	}
	.events ul {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
	}
	.events p {
		margin: 0;
	}
	.all {
		font-size: var(--text-base);
	}
	.view-menu {
		display: flex;
		flex-direction: column;
		min-width: min(18rem, calc(100vw - 48px));
		padding: var(--space-1);
	}
	.view-row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-3);
		padding: var(--space-2) var(--space-2-5);
		font-size: var(--text-base);
	}
	.view-row span {
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.view-reset {
		margin-top: var(--space-1);
		padding: var(--space-2) var(--space-2-5);
		border: 0;
		border-top: 1px solid var(--border);
		background: none;
		color: var(--text);
		font: inherit;
		font-size: var(--text-base);
		text-align: left;
		cursor: pointer;
	}
	.view-reset:hover {
		background: var(--srow-hover, var(--surface));
	}
	@media (max-width: 760px) {
		.body {
			flex-direction: column;
			align-items: stretch;
		}
		.wall {
			flex: none;
			width: 100%;
		}
		.events {
			flex: none;
			width: 100%;
		}
	}
</style>
