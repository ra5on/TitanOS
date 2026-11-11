<script lang="ts">
	// PLAN 61: the side bar of a window – the program's pages, for virtual
	// machines the machines themselves, and at the bottom the fold button.
	import { tick } from 'svelte';
	import Icon from '../components/Icon.svelte';
	import { t } from '../i18n/index.svelte.ts';
	import { session } from '../session.svelte.ts';
	import { settingsSideAllowed, settingsSideMatches } from '../settings-search.ts';
	import { sideActive, type SideItem } from './apps.ts';
	import DesktopIcon from './DesktopIcon.svelte';
	import { navigateWindow, type Win } from './wm.svelte.ts';

	let {
		win,
		glyph,
		gradient,
		side,
		folded,
		path,
		ontoggle,
	}: {
		win: Win;
		glyph: string;
		gradient: string;
		side: SideItem[];
		folded: boolean;
		/** The page the window shows (without its query). */
		path: string;
		/** Folds or unfolds the side bar. */
		ontoggle: () => void;
	} = $props();

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
	export async function focusSearch() {
		if (folded) ontoggle();
		await tick();
		searchEl?.focus();
		searchEl?.select();
	}
</script>

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
				{#if !folded}<div class="group">{t(item.group)}</div>{:else}<div class="rule"></div>{/if}
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
		onclick={ontoggle}
		title={folded ? t('desktop.window.unfold') : undefined}
	>
		<Icon name="sidebar" size={17} />
		{#if !folded}<span>{t('desktop.window.fold')}</span>{/if}
	</button>
</nav>

<style>
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
		min-width: 0;
		overflow-wrap: break-word;
		hyphens: auto;
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
	.item:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.fold {
		color: var(--muted);
		margin-top: var(--space-1-5);
	}
</style>
