<script lang="ts" module>
	// 1.13 (owner 18 Nov 2026, docs/rules/WEB-UI.md "Mail, Kontakte,
	// Kalender seit 1.13"): the frame of a program with columns, shared by
	// Mail, Kontakte and Kalender, after the KI-Agent and Dateien. Edge to
	// edge (the page gives up --page-pad), as high as the window (CSS only:
	// the page's <main> becomes a column of the window's height), a side bar
	// on the left in the window side bar's look with the program's main
	// action on top, an optional list column, the content; each scrolls on
	// its own. Side bar and list are dragged wider (ColumnResize, per device)
	// and the side bar folds to a narrow rail (per device). Below 620 px the
	// side bar is folded (a tap unfolds it until the window is wide again)
	// and nothing is dragged. With `columns` false the program shows one
	// level at a time (its phone and narrow view) and the frame only keeps
	// the parts' classes; compact (compact.svelte.ts: the phone or under
	// 620 px) the levels reach the screen's edges and the program puts its
	// actions into a BottomBar.
	import { readWidth } from './ColumnResize.svelte';

	export type FrameLevel = 'side' | 'list' | 'content';
	export type ColumnWidth = { min: number; max: number; fallback: number };

	/** The side bar folded to its rail is this wide. */
	export const RAIL = 60;
	/** Below this window width the side bar folds and no column is dragged. */
	export const NARROW = 620;

	export function readFolded(key: string): boolean {
		try {
			return localStorage.getItem(key) === '1';
		} catch {
			return false;
		}
	}
	export function storeFolded(key: string, folded: boolean) {
		try {
			if (folded) localStorage.setItem(key, '1');
			else localStorage.removeItem(key);
		} catch {
			// Not kept in this browser; it applies now.
		}
	}

	/**
	 * The widths shown: the stored ones, given up where the window is too
	 * narrow, so that the content keeps `contentMin` (the side bar first
	 * keeps its own minimum, then the list).
	 */
	export function columnWidths(o: {
		frame: number;
		side: number;
		list: number | null;
		folded: boolean;
		sideW: ColumnWidth;
		listW: ColumnWidth;
		contentMin: number;
	}) {
		const listMin = o.list === null ? 0 : o.listW.min;
		const sideMax = Math.max(o.sideW.min, Math.min(o.sideW.max, o.frame - o.contentMin - listMin));
		const side = o.folded ? RAIL : Math.min(o.side, sideMax);
		const listMax = Math.max(o.listW.min, Math.min(o.listW.max, o.frame - o.contentMin - side));
		const list = o.list === null ? 0 : Math.min(o.list, listMax);
		return { side, sideMax, list, listMax };
	}
</script>

<script lang="ts">
	import { onMount, untrack, type Snippet } from 'svelte';
	import ColumnResize from './ColumnResize.svelte';
	import { compactView } from './compact.svelte.ts';
	import Icon from './Icon.svelte';
	import { sortStatus } from './SortGrip.svelte';
	import { t } from '../i18n/index.svelte.ts';
	import { followSideOrder, sideOrderState } from '../side-order.svelte.ts';

	type Props = {
		/** localStorage prefix of the program ("titanos.mail"): .sideWidth, .listWidth, .sideFolded. */
		storage: string;
		/** Keys kept from before (Mail 1.12: "titanos.mail.foldersWidth"). */
		sideKey?: string;
		listKey?: string;
		/** Columns side by side; false: one level at a time (`level`), the program's narrow view. */
		columns?: boolean;
		level?: FrameLevel;
		/** Classes of the frame and its parts (the program's styles and tests). */
		class?: string;
		sideClass?: string;
		listClass?: string;
		contentClass?: string;
		sideWidth?: ColumnWidth;
		listWidth?: ColumnWidth;
		/** The content keeps at least this much; the columns give way. */
		contentMin?: number;
		/**
		 * 1.13 (Kalender): below this width of the frame the side bar folds by
		 * itself, as under 620 px (a tap unfolds it until the frame is wider
		 * again). 0: never (the side bar only narrows).
		 */
		foldBelow?: number;
		sideResizeLabel?: string;
		listResizeLabel?: string;
		resizeHint?: string;
		/** On top of the side bar (the main action); `folded`: as an icon. */
		action?: Snippet<[folded: boolean]>;
		/** The side bar's entries; scrolls on its own. */
		side: Snippet;
		/** The folded side bar's entries (icons); without, only the action and unfolding. */
		rail?: Snippet;
		/** Below the entries (links such as "Postfach hinzufügen"); `folded`: as icons. */
		sideFoot?: Snippet<[folded: boolean]>;
		/** One slim bar above list and content (search, filters, navigation). */
		bar?: Snippet;
		/** Notices above list and content (errors, the live stream). */
		notices?: Snippet;
		/** The list column; without it the content follows the side bar. */
		list?: Snippet;
		/** 1.13: a column right of the content (the agent's panel), only with
		 *  columns; dragged at its left border, its width kept under asideKey. */
		aside?: Snippet;
		asideWidth?: ColumnWidth;
		asideKey?: string;
		asideResizeLabel?: string;
		/** Whether the aside stands as a column now: only while side bar, list
		 *  and content keep their minimum next to it (else the page shows it
		 *  another way, e.g. as a sheet). */
		asideShown?: boolean;
		/** The content. */
		children: Snippet;
	};
	let {
		storage,
		sideKey = `${storage}.sideWidth`,
		listKey = `${storage}.listWidth`,
		columns = true,
		level = 'content',
		class: own = '',
		sideClass = '',
		listClass = '',
		contentClass = '',
		sideWidth = { min: 180, max: 420, fallback: 240 },
		listWidth = { min: 280, max: 720, fallback: 380 },
		contentMin = 320,
		foldBelow = 0,
		sideResizeLabel,
		listResizeLabel,
		resizeHint,
		action,
		side,
		rail,
		sideFoot,
		bar,
		notices,
		list,
		aside,
		asideWidth = { min: 300, max: 640, fallback: 380 },
		asideKey = `${storage}.asideWidth`,
		asideResizeLabel,
		asideShown = $bindable(false),
		children,
	}: Props = $props();

	const foldKey = $derived(`${storage}.sideFolded`);
	// What this browser keeps, read once: a frame's keys do not change while it stands.
	let sideStored = $state(
		untrack(() => readWidth(sideKey, sideWidth.min, sideWidth.max, sideWidth.fallback)),
	);
	let listStored = $state(
		untrack(() => readWidth(listKey, listWidth.min, listWidth.max, listWidth.fallback)),
	);
	let foldedPref = $state(untrack(() => readFolded(foldKey)));
	let asideStored = $state(
		untrack(() => readWidth(asideKey, asideWidth.min, asideWidth.max, asideWidth.fallback)),
	);

	// Below 620 px folded; unfolded by hand it stays so until the window is wide again.
	let narrow = $state(
		typeof matchMedia === 'function' && matchMedia(`(max-width: ${NARROW - 1}px)`).matches,
	);
	let heldOpen = $state(false);
	onMount(() => {
		followSideOrder();
		if (typeof matchMedia !== 'function') return;
		const query = matchMedia(`(max-width: ${NARROW - 1}px)`);
		const change = () => {
			narrow = query.matches;
			if (!narrow) heldOpen = false;
		};
		query.addEventListener('change', change);
		return () => query.removeEventListener('change', change);
	});
	let frameWidth = $state(0);
	const tight = $derived(!narrow && foldBelow > 0 && frameWidth > 0 && frameWidth < foldBelow);
	let heldTight = $state(false);
	$effect(() => {
		if (!tight) heldTight = false;
	});
	const folded = $derived(columns && (narrow ? !heldOpen : tight ? !heldTight : foldedPref));
	function toggleFold() {
		if (narrow) heldOpen = !heldOpen;
		else if (tight) heldTight = !heldTight;
		else {
			foldedPref = !foldedPref;
			storeFolded(foldKey, foldedPref);
		}
	}

	// The panel takes its width first, but leaves the content its minimum.
	const asideMax = $derived(
		Math.max(asideWidth.min, Math.min(asideWidth.max, (frameWidth || 1200) - contentMin - 240)),
	);
	// Room for the aside: the others at their minimum next to its minimum.
	const asideFits = $derived(
		(frameWidth || 1200) - asideWidth.min >=
			(folded ? RAIL : sideWidth.min) + (list ? listWidth.min : 0) + contentMin,
	);
	const asideW = $derived(aside && columns && asideFits ? Math.min(asideStored, asideMax) : 0);
	$effect(() => {
		asideShown = asideW > 0;
	});
	const widths = $derived(
		columnWidths({
			frame: (frameWidth || 1200) - asideW,
			side: sideStored,
			list: list ? listStored : null,
			folded,
			sideW: sideWidth,
			listW: listWidth,
			contentMin,
		}),
	);
	const template = $derived(
		(list
			? `${widths.side}px ${widths.list}px minmax(0, 1fr)`
			: `${widths.side}px minmax(0, 1fr)`) + (asideW ? ` ${asideW}px` : ''),
	);
	const resizable = $derived(columns && !narrow);
</script>

{#snippet sideColumn()}
	<div class="col cf-side {sideClass}" class:folded>
		{#if action}<div class="cf-action">{@render action(folded)}</div>{/if}
		<div class="cf-scroll">
			{#if folded}{#if rail}{@render rail()}{/if}{:else}{@render side()}{/if}
		</div>
		{#if sideOrderState.error && !folded}
			<p class="notice-error cf-order-error" role="alert">{sideOrderState.error}</p>
		{/if}
		{#if sideFoot || columns}
			<div class="cf-foot">
				{#if sideFoot}{@render sideFoot(folded)}{/if}
				{#if columns}
					<button
						type="button"
						class="cf-fold"
						aria-expanded={!folded}
						title={folded ? t('desktop.window.unfold') : undefined}
						aria-label={folded ? t('desktop.window.unfold') : undefined}
						onclick={toggleFold}
						><Icon name="sidebar" size={17} />{#if !folded}<span>{t('desktop.window.fold')}</span
							>{/if}</button
					>
				{/if}
			</div>
		{/if}
	</div>
{/snippet}

<p class="sr-only" role="status">{sortStatus.text}</p>
{#if columns}
	<div
		class="cf columns {own}"
		class:folded
		class:narrow
		class:has-aside={!!asideW}
		bind:clientWidth={frameWidth}
		style:grid-template-columns={template}
		style:--cf-side-w="{widths.side}px"
		style:--cf-list-w="{widths.list}px"
		style:--cf-aside-w="{asideW}px"
	>
		{@render sideColumn()}
		{#if bar || notices}
			<div class="cf-top">
				{#if notices}{@render notices()}{/if}
				{#if bar}<div class="cf-bar">{@render bar()}</div>{/if}
			</div>
		{/if}
		{#if list}<div class="col cf-list {listClass}">{@render list()}</div>{/if}
		<div class="col cf-content {contentClass}">{@render children()}</div>
		{#if aside && asideW}<div class="col cf-aside">{@render aside()}</div>{/if}
		{#if aside && asideW && !narrow}
			<ColumnResize
				bind:width={asideStored}
				min={asideWidth.min}
				max={asideMax}
				fallback={asideWidth.fallback}
				key={asideKey}
				label={asideResizeLabel ?? t('ui.columns.aside')}
				hint={resizeHint ?? t('ui.columns.hint')}
				left="calc(100% - var(--cf-aside-w))"
				invert
			/>
		{/if}
		{#if resizable}
			{#if !folded}
				<ColumnResize
					bind:width={sideStored}
					min={sideWidth.min}
					max={widths.sideMax}
					fallback={sideWidth.fallback}
					key={sideKey}
					label={sideResizeLabel ?? t('ui.columns.side')}
					hint={resizeHint ?? t('ui.columns.hint')}
					left="var(--cf-side-w)"
				/>
			{/if}
			{#if list}
				<ColumnResize
					bind:width={listStored}
					min={listWidth.min}
					max={widths.listMax}
					fallback={listWidth.fallback}
					key={listKey}
					label={listResizeLabel ?? t('ui.columns.list')}
					hint={resizeHint ?? t('ui.columns.hint')}
					left="calc(var(--cf-side-w) + var(--cf-list-w))"
				/>
			{/if}
		{/if}
	</div>
{:else}
	<!-- Compact (the phone, under 620 px): edge to edge, the program draws no cards. -->
	<div class="cf levels {own}" class:compact={compactView()} data-level={level}>
		{#if notices}{@render notices()}{/if}
		{#if level === 'side'}
			{@render sideColumn()}
		{:else if bar}
			<div class="cf-bar">{@render bar()}</div>
		{/if}
		<!-- Kept while another level is shown: going back finds the list (and a search) as it was. -->
		{#if list}<div class="col cf-list {listClass}" hidden={level !== 'list'}>
				{@render list()}
			</div>{/if}
		{#if level === 'content'}<div class="col cf-content {contentClass}">
				{@render children()}
			</div>{/if}
	</div>
{/if}

<style>
	/* The page around the frame: no padding, the window's height, a column
	   (banners above the frame keep their own height). */
	/* 1.15: also under 760 px, where the shell gives pages their padding
	   back (.bare main:not(.agent-app)); a frame with columns stays edge to
	   edge in every size of a window. */
	:global(main:has(> .cf.columns)),
	:global(.bare main:not(.agent-app):has(> .cf.columns)) {
		display: flex;
		flex-direction: column;
		gap: 0;
		padding: 0;
		height: 100dvh;
		min-height: 0;
		overflow: hidden;
	}
	:global(main:has(> .cf.columns) > :is(.notice-warn, .notice-error)) {
		margin: var(--space-2) var(--space-3);
	}
	.cf.columns {
		position: relative;
		flex: 1 1 auto;
		min-height: 0;
		min-width: 0;
		display: grid;
		grid-template-rows: auto minmax(0, 1fr);
		background: var(--bg);
	}
	.columns > .col {
		min-width: 0;
		min-height: 0;
		overflow: auto;
	}
	.columns > .cf-side {
		grid-column: 1;
		grid-row: 1 / -1;
	}
	.columns > .cf-top {
		grid-column: 2 / -1;
		grid-row: 1;
		min-width: 0;
		border-bottom: 1px solid var(--border);
	}
	.columns > .cf-list,
	.columns > .cf-content {
		grid-row: 2;
	}
	.columns > .cf-list + .cf-content {
		border-left: 1px solid var(--border);
	}
	.columns.has-aside > .cf-top {
		grid-column: 2 / -2;
	}
	.columns > .cf-aside {
		position: relative;
		grid-column: -2 / -1;
		grid-row: 1 / -1;
		display: flex;
		flex-direction: column;
		overflow: hidden;
		border-left: 1px solid var(--border);
		background: var(--surface);
	}
	.cf-bar {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
		min-height: 48px;
		padding: var(--space-1-5) var(--space-3);
		min-width: 0;
	}
	.levels > .col[hidden] {
		display: none;
	}
	.cf.levels.compact {
		margin-inline: calc(-1 * var(--page-pad));
	}
	/* The side bar in the look of the window's side bar (WindowSide, PLAN 71). */
	.columns > .cf-side {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		overflow: hidden;
		padding: var(--space-3) var(--space-1-5) var(--space-2);
		border-right: 1px solid var(--border);
		background:
			linear-gradient(
				180deg,
				color-mix(in srgb, var(--text) 3%, transparent),
				color-mix(in srgb, var(--accent) 3%, transparent)
			),
			color-mix(in srgb, var(--surface) 88%, var(--bg));
	}
	.columns > .cf-side.folded {
		align-items: center;
		padding-inline: var(--space-1);
	}
	.cf-action {
		flex: none;
		display: flex;
		flex-direction: column;
		padding: 0 var(--space-1);
	}
	.cf-scroll {
		min-height: 0;
		min-width: 0;
	}
	.columns .cf-scroll {
		flex: 1 1 auto;
		overflow-x: hidden;
		overflow-y: auto;
		/* The scroll bar gets a lane of its own instead of lying over the names. */
		scrollbar-gutter: stable;
		scrollbar-width: thin;
		align-self: stretch;
	}
	.levels .cf-side {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}
	.cf-foot {
		flex: none;
		display: flex;
		flex-direction: column;
		gap: 2px;
		align-self: stretch;
	}
	.folded .cf-foot {
		align-items: center;
	}
	.cf-fold {
		display: flex;
		align-items: center;
		gap: var(--space-2-5);
		min-height: 36px;
		padding: var(--space-1-5) var(--space-2-5);
		border: 0;
		border-radius: calc(9px * var(--radius-scale, 1));
		background: none;
		color: var(--muted);
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.folded .cf-fold {
		justify-content: center;
		width: 44px;
		padding: 0;
	}
	.cf-fold :global(svg) {
		flex: none;
	}
	.cf-fold:hover {
		background: color-mix(in srgb, var(--text) 7%, transparent);
		color: var(--text);
	}
	.cf-fold:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.cf-order-error {
		flex: none;
		margin: 0 var(--space-1);
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
	}
	@media (pointer: coarse) {
		.cf-fold {
			min-height: 44px;
		}
	}
</style>
