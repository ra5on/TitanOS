<script lang="ts">
	// PLAN 61: the side bar of a window – the program's pages, for virtual
	// machines the machines themselves, and at the bottom the fold button.
	// For Systemsteuerung (PLAN 78, PLAN 86) the account, the search and the
	// areas, which open their pages right in the column.
	import { tick } from 'svelte';
	import Icon from '../components/Icon.svelte';
	import { t } from '../i18n/index.svelte.ts';
	import { session } from '../session.svelte.ts';
	import {
		settingsAreaOf,
		settingsGroupOf,
		settingsMenu,
		type SettingsGroup,
	} from '../settings-search.ts';
	import {
		findSettings,
		loadSettingsIndex,
		settingsIndexNow,
		type SettingsHit,
		type SettingsIndexEntry,
	} from '../settings-find.ts';
	import { showsNew } from '../settings-seen.svelte.ts';
	import { appForPath, sideActive, type SideItem } from './apps.ts';
	import DesktopIcon from './DesktopIcon.svelte';
	import { openProgram } from './programs.svelte.ts';
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
	// its side bar, with a search above it. PLAN 86 (owner 13.11.2026): the
	// search finds every page, tab, group, row and part of a row
	// (settings-find.ts) and a hit leads to the row itself. Accounts with the
	// role "user" see only what they may open. Other programs keep their bar.
	const searchable = $derived(win.app === 'app:settings');
	let query = $state('');
	let hit = $state(0);
	let searchEl = $state<HTMLInputElement>();
	let index = $state<SettingsIndexEntry[] | null>(settingsIndexNow());
	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));
	// PLAN 78: on top the signed-in account with its real role, then the
	// search, then the overview and the areas.
	const account = $derived(session.data?.user);
	const areas = $derived(searchable ? settingsMenu(admin) : []);
	const area = $derived(searchable ? settingsGroupOf(win.href) : undefined);
	const pageId = $derived(searchable ? settingsAreaOf(path) : '');
	const links = $derived<SettingsHit[]>(
		searchable && query.trim() ? findSettings(query, admin, t, index) : [],
	);
	const searching = $derived(searchable && query.trim() !== '');
	const current = $derived(links.length ? Math.min(hit, links.length - 1) : -1);
	/** The index comes with the first search, not with the desktop. */
	function wantIndex() {
		if (!index) void loadSettingsIndex().then((loaded) => (index = loaded));
	}
	function openHit(entry: SettingsHit) {
		// A setting of another program (Mein Konto) opens in that program.
		const program = appForPath(entry.href.split(/[?#]/)[0]);
		if (program && program.id !== 'settings')
			void openProgram(`app:${program.id}`, { href: entry.href });
		else navigateWindow(win.id, entry.href);
		query = '';
		hit = 0;
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
			openHit(item);
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

	// PLAN 86 (variant A): an area opens its pages in the column, one area at
	// a time; the area of the page shown is open by itself. A choice holds
	// while the window shows the page it was made on.
	let choice = $state<{ href: string; group: SettingsGroup | null } | null>(null);
	const openGroup = $derived(choice && choice.href === win.href ? choice.group : (area ?? null));
	function toggleArea(group: SettingsGroup) {
		choice = { href: win.href, group: openGroup === group ? null : group };
	}
	let itemsEl = $state<HTMLElement>();
	/** Arrow keys in the menu: up/down move, right opens or goes in, left closes or goes up. */
	async function menuKey(e: KeyboardEvent) {
		const target = e.target as HTMLElement;
		if (!itemsEl || !target.matches('[data-nav]')) return;
		const all = [...itemsEl.querySelectorAll<HTMLElement>('[data-nav]')];
		const at = all.indexOf(target);
		const group = target.dataset.group as SettingsGroup | undefined;
		const move = (to: HTMLElement | undefined) => {
			if (!to) return;
			e.preventDefault();
			to.focus();
		};
		if (e.key === 'ArrowDown') move(all[at + 1]);
		else if (e.key === 'ArrowUp') move(all[at - 1]);
		else if (e.key === 'Home') move(all[0]);
		else if (e.key === 'End') move(all.at(-1));
		else if (e.key === 'ArrowRight' && target.dataset.kind === 'area' && group) {
			e.preventDefault();
			if (openGroup !== group) toggleArea(group);
			else move(all[at + 1]);
		} else if (e.key === 'ArrowLeft' && group) {
			e.preventDefault();
			if (target.dataset.kind === 'area') {
				if (openGroup === group) toggleArea(group);
			} else {
				choice = { href: win.href, group: null };
				await tick();
				itemsEl.querySelector<HTMLElement>(`[data-kind="area"][data-group="${group}"]`)?.focus();
			}
		}
	}
</script>

<nav class="side" class:folded class:settings={searchable} aria-label={win.title}>
	{#if searchable && account}
		<!-- PLAN 78: who is signed in, with the role the session reports. -->
		<div
			class="side-account"
			title={folded ? `${account.name} · ${t(`role.${account.role}`)}` : undefined}
		>
			<span class="avatar" aria-hidden="true">{account.name.charAt(0).toUpperCase()}</span>
			{#if !folded}<span class="who"
					><b>{account.name}</b><small>{t(`role.${account.role}`)}</small></span
				>{:else}<span class="sr-only">{account.name} · {t(`role.${account.role}`)}</span>{/if}
		</div>
	{:else}
		<div class="side-title">
			<DesktopIcon {glyph} {gradient} size={folded ? 32 : 28} name={win.title} />
			{#if !folded}<b>{win.title}</b>{/if}
		</div>
	{/if}
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
					onfocus={wantIndex}
					oninput={() => {
						hit = 0;
						wantIndex();
					}}
					onkeydown={searchKey}
				/>
			</div>
			{#if searching}<p class="side-none" role="status">
					{links.length ? t('ui.results', { count: links.length }) : t('settingshome.sideNone')}
				</p>{/if}
		{/if}
	{/if}
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<div class="items" id="side-{win.id}" bind:this={itemsEl} onkeydown={menuKey}>
		{#if searching}
			{#each links as entry, n (entry.href + '\n' + entry.label + '\n' + entry.where)}
				<button
					type="button"
					class="item result"
					id="side-{win.id}-{n}"
					class:hit={n === current}
					onclick={() => openHit(entry)}
				>
					<Icon name={entry.icon} size={17} />
					<span class="two"><span>{entry.label}</span><small>{entry.where}</small></span>
				</button>
			{/each}
		{:else if searchable}
			{@const start = path === '/settings' && !area}
			<button
				type="button"
				class="item"
				class:on={start}
				data-nav
				title={folded ? t('desktop.side.allSettings') : undefined}
				aria-current={start ? 'page' : undefined}
				onclick={() => navigateWindow(win.id, '/settings')}
			>
				<Icon name="home" size={17} />
				{#if !folded}<span>{t('desktop.side.allSettings')}</span>{/if}
			</button>
			{#each areas as entry (entry.group)}
				{@const single = entry.pages.length === 1}
				{@const isOpen = !single && !folded && openGroup === entry.group}
				{@const here = area === entry.group}
				{#if single || folded}
					<!-- One page (Benachrichtigungen, Cluster) opens directly; in the
					     folded column an area opens its first page. -->
					<button
						type="button"
						class="item"
						class:on={here}
						data-nav
						data-kind="page"
						title={folded ? t(entry.label) : undefined}
						aria-current={here ? 'page' : undefined}
						onclick={() => navigateWindow(win.id, entry.href)}
					>
						<Icon name={entry.icon} size={17} />
						{#if !folded}<span>{t(entry.label)}</span>{/if}
					</button>
				{:else}
					<button
						type="button"
						class="item area"
						class:here={here && !isOpen}
						data-nav
						data-kind="area"
						data-group={entry.group}
						aria-expanded={isOpen}
						aria-controls="side-{win.id}-{entry.group}"
						aria-current={here && !isOpen ? 'true' : undefined}
						onclick={() => toggleArea(entry.group)}
					>
						<Icon name={entry.icon} size={17} />
						<span>{t(entry.label)}</span>
						<span class="chev" class:open={isOpen} aria-hidden="true"
							><Icon name="chevronRight" size={14} /></span
						>
					</button>
					{#if isOpen}
						<div
							class="pages"
							id="side-{win.id}-{entry.group}"
							role="group"
							aria-label={t(entry.label)}
						>
							{#each entry.pages as p (p.id)}
								{@const on = pageId === p.id}
								<button
									type="button"
									class="item page"
									class:on
									data-nav
									data-kind="page"
									data-group={entry.group}
									aria-current={on ? 'page' : undefined}
									onclick={() => navigateWindow(win.id, `/settings/${p.id}`)}
								>
									<Icon name={p.icon} size={15} />
									<span>{t(p.label)}</span>
									{#if showsNew(p)}<span class="chip info new">{t('settings.new')}</span>{/if}
								</button>
							{/each}
						</div>
					{/if}
				{/if}
			{/each}
		{:else}
			{#each side as item, i (i)}
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
		{/if}
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
	/* PLAN 78: the account on top of the control panel's menu. */
	.side-account {
		display: flex;
		align-items: center;
		gap: var(--space-2-5);
		padding: var(--space-1) var(--space-1-5) var(--space-3);
		min-width: 0;
	}
	.side-account .avatar {
		display: grid;
		place-items: center;
		flex: none;
		width: 38px;
		height: 38px;
		border-radius: 50%;
		background: var(--accent);
		color: var(--on-accent);
		font-weight: 700;
	}
	.folded .side-account {
		padding: var(--space-1) 0 var(--space-3);
	}
	.side-account .who {
		display: flex;
		flex-direction: column;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.side-account small {
		color: var(--muted);
		font-size: var(--text-sm);
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
	/* PLAN 86: the control panel's column keeps one width whichever area is
	   open (the entries wrap instead of widening the column). */
	.side.settings:not(.folded) {
		width: 264px;
	}
	.item > span:not(.chev, .two, .new) {
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.area .chev {
		display: flex;
		margin-left: auto;
		color: var(--muted);
		transition: transform 0.15s ease;
	}
	.area .chev.open {
		transform: rotate(90deg);
	}
	.item.here {
		font-weight: 600;
	}
	.item.here :global(svg) {
		color: var(--accent);
	}
	.pages {
		display: flex;
		flex-direction: column;
		gap: 2px;
		margin: 0 0 var(--space-1) var(--space-3);
		padding-left: var(--space-2);
		border-left: 1px solid var(--border);
	}
	.item.page {
		min-height: 32px;
		padding-block: var(--space-1);
	}
	.item .new {
		margin-left: auto;
		flex: none;
	}
	.item.result {
		align-items: flex-start;
	}
	.item.result :global(svg) {
		margin-top: 2px;
	}
	.two {
		display: flex;
		flex-direction: column;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.two small {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	@media (prefers-reduced-motion: reduce) {
		.area .chev {
			transition: none;
		}
	}
</style>
