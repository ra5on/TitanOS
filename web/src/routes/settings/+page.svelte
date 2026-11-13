<script lang="ts">
	// The start page of the settings. In a window of the desktop (PLAN 71,
	// owner's decision "Variante C" of 7 Nov 2026) it is the "Übersicht": the
	// window's menu with its search lists the areas, so the page shows only
	// the overview cards. On the phone and in a tab of its own there is no
	// menu: there it is "Alle Einstellungen" – a search and every page as a
	// row under the heading of its area (PLAN 78, PLAN 86) – and the same
	// overview cards below.
	//
	// PLAN 86 (variant A): an area has no page of its own any more; the menu
	// opens its pages in the column. "?area=<area>" (old links, the menu bar,
	// the phone's ⋯ menu) opens the area's first page in a window, and
	// scrolls "Alle Einstellungen" to the area's heading elsewhere.
	import { goto } from '$app/navigation';
	import { untrack } from 'svelte';
	import { inWindow } from '#lib/desktop/frame.ts';
	import SettingsOverview from './SettingsOverview.svelte';
	import { showsNew } from '#lib/settings-seen.svelte.ts';
	import {
		settingsAreaTarget,
		settingsEntries,
		settingsGroupLabel,
		settingsGroupOf,
		settingsGroups,
		type SettingsEntry,
	} from '#lib/settings-search.ts';
	import {
		findSettings,
		loadSettingsIndex,
		settingsIndexNow,
		type SettingsHit,
		type SettingsIndexEntry,
	} from '#lib/settings-find.ts';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { navigating, page } from '$app/state';
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingsSearch from '#lib/components/SettingsSearch.svelte';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { settingsSearch, settingsStates } from '#lib/settings-states.svelte.ts';

	const prefix = 'settings-home';
	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));
	const searching = $derived(settingsSearch.query.trim() !== '');
	let index = $state<SettingsIndexEntry[] | null>(settingsIndexNow());
	$effect(() => {
		if (searching && !index) void loadSettingsIndex().then((loaded) => (index = loaded));
	});
	const hits = $derived(searching ? findSettings(settingsSearch.query, admin, t, index) : []);
	// Without a search: every page under its area, in the order of the menu.
	const groups = $derived(searching ? [] : settingsEntries('', admin, t));
	// With a search: the hits under the heading of their area, pages of other
	// programs (Mein Konto) after them.
	const hitGroups = $derived.by(() => {
		const out: { id: string; title: string; hits: SettingsHit[] }[] = [];
		for (const group of settingsGroups) {
			const mine = hits.filter((hit) => hit.group === group);
			if (mine.length) out.push({ id: group, title: t(settingsGroupLabel(group)), hits: mine });
		}
		const others = hits.filter((hit) => !hit.group);
		if (others.length)
			out.push({ id: 'other', title: others[0].where.split(' › ')[0], hits: others });
		return out;
	});
	const flat = $derived(searching ? hitGroups.flatMap((g) => g.hits) : []);
	const states = $derived(settingsStates());
	const active = $derived(
		searching && flat.length ? Math.min(settingsSearch.active, flat.length - 1) : -1,
	);

	// "?area=": in a window the area's first page the account may open (the
	// overview when it may open none); elsewhere its heading in the list.
	// Once the navigation that brought it has ended (a goto during it would
	// abort it); also when the page appears only after it (session, texts).
	const wanted = $derived(page.url.searchParams.has('area') ? page.url.search : '');
	let handled = '';
	$effect(() => {
		const href = page.url.href;
		if (!wanted || navigating.to || handled === href) return;
		handled = href;
		const search = wanted;
		untrack(() => toArea(search));
	});
	function toArea(search: string) {
		if (inWindow) {
			goto(settingsAreaTarget(search, admin) ?? '/settings', { replaceState: true }).catch(
				() => {},
			);
			return;
		}
		const group = settingsGroupOf(`/settings${search}`);
		if (!group) return;
		// The list comes with the session and the texts; look for it a while.
		const until = Date.now() + 5000;
		const find = () => {
			const heading = document.getElementById(`${prefix}-group-${group}`);
			if (heading) requestAnimationFrame(() => heading.scrollIntoView({ block: 'start' }));
			else if (Date.now() < until) setTimeout(find, 100);
		};
		find();
	}

	function clear() {
		settingsSearch.query = '';
		settingsSearch.active = 0;
	}

	// PLAN 67-1: Systemsteuerung › Ablage › Einstellungen suchen puts the cursor
	// in the search. In a window the window itself answers (its side bar search,
	// PLAN 71); this one serves the phone's ⋯ menu.
	import { registerMenuCommands } from '#lib/menu-commands.svelte.ts';
	registerMenuCommands({
		'settings.search': () =>
			document.querySelector<HTMLInputElement>('.home-search input[type="search"]')?.focus(),
	});
</script>

{#snippet row(entry: SettingsEntry)}
	{@const state = states[entry.id]}
	<SettingRow
		href={entry.href}
		icon={entry.icon}
		title={entry.label}
		desc={t(`settingshome.desc.${entry.id}` as MessageKey)}
	>
		{#snippet badge()}{#if showsNew(entry)}<span class="chip info">{t('settings.new')}</span
				>{/if}{/snippet}
		{#snippet control()}{#if state}<span class="chip {state.tone}">{t(state.text)}</span
				>{/if}{/snippet}
	</SettingRow>
{/snippet}

{#snippet found(hit: SettingsHit, id: string, highlight: boolean)}
	<div class="hit" class:highlight {id}>
		<SettingRow
			href={hit.href}
			icon={hit.icon}
			title={hit.label}
			desc={hit.kind === 'page' && hit.group
				? t(`settingshome.desc.${hit.page}` as MessageKey)
				: hit.where}
		/>
	</div>
{/snippet}

{#if inWindow && wanted}
	<!-- An area's address: the window moves on to its first page. -->
	<PageHead title={t('settingshome.overview')} />
{:else if inWindow}
	<PageHead title={t('settingshome.overview')} />
	<SettingsOverview {admin} />
{:else}
	<PageHead title={t('settingshome.all')} />

	<div class="home">
		<div class="home-search">
			<SettingsSearch results={flat} idPrefix={prefix} placeholder={t('settingshome.searchHint')} />
		</div>
		{#if searching}<div class="search-result">
				<span role="status"
					>{flat.length ? t('ui.results', { count: flat.length }) : t('ui.noSettings')}</span
				><button class="btn" type="button" onclick={clear}>{t('ui.clear')}</button>
			</div>{/if}

		<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
		<div class="groups" id="{prefix}-results" onclick={clear}>
			{#if searching}
				{#each hitGroups as group (group.id)}
					{@const offset = flat.indexOf(group.hits[0])}
					<SettingGroup title={group.title} id="{prefix}-group-{group.id}">
						{#each group.hits as hit, i (hit.href + '\n' + hit.label + '\n' + hit.where)}
							{@render found(hit, `${prefix}-${offset + i}`, offset + i === active)}
						{/each}
					</SettingGroup>
				{/each}
			{:else}
				{#each groups as group (group.group)}
					<SettingGroup
						title={t(settingsGroupLabel(group.group))}
						id="{prefix}-group-{group.group}"
					>
						{#each group.entries as entry (entry.href)}{@render row(entry)}{/each}
					</SettingGroup>
				{/each}
			{/if}
		</div>

		{#if !searching}<SettingsOverview {admin} prefix="{prefix}-overview" />{/if}
	</div>
{/if}

<style>
	.home {
		display: flex;
		flex-direction: column;
		gap: var(--gap);
		min-width: 0;
	}
	.home-search {
		width: 100%;
		max-width: 36rem;
	}
	.search-result {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-3);
	}
	.groups {
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
	}
	/* "?area=" scrolls to a heading: below the phone's bar, not under it. */
	.groups :global(.sgroup) {
		scroll-margin-top: calc(var(--header-height, 0px) + var(--space-3));
	}
	.hit {
		border-radius: var(--srow-radius);
	}
	/* The hit the arrow keys of the search stand on. */
	.hit.highlight {
		box-shadow: inset 0 0 0 2px var(--accent);
	}
</style>
