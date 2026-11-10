<script lang="ts">
	// The start page of the settings. In a window of the desktop (PLAN 71,
	// owner's decision "Variante C" of 7 Nov 2026) it is the "Übersicht": the
	// window's menu with its search lists the areas, so the page shows only
	// the overview cards. On the phone and in a tab of its own there is no
	// menu: there it is "Alle Einstellungen" – a search and every page as a
	// row under the heading of its area (PLAN 78, draft 78) – and the same
	// overview cards below. With "?area=" it is the page of one area: the list
	// of its settings pages, where the menu's areas and the path of a page lead.
	import { inWindow } from '#lib/desktop/frame.ts';
	import SettingsOverview from './SettingsOverview.svelte';
	import { showsNew } from '#lib/settings-seen.svelte.ts';
	import {
		settingsAreas,
		settingsEntries,
		settingsGroupLabel,
		settingsGroupOf,
		settingsMenu,
		type SettingsEntry,
	} from '#lib/settings-search.ts';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { page } from '$app/state';
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingsSearch from '#lib/components/SettingsSearch.svelte';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { settingsSearch, settingsStates } from '#lib/settings-states.svelte.ts';

	const prefix = 'settings-home';
	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));
	const groups = $derived(settingsEntries(settingsSearch.query, admin, t));
	const flat = $derived(groups.flatMap((group) => group.entries));
	const searching = $derived(settingsSearch.query.trim() !== '');
	const states = $derived(settingsStates());
	const active = $derived(
		searching && flat.length ? Math.min(settingsSearch.active, flat.length - 1) : -1,
	);

	// The area "?area=" names, if the account may open one of its pages.
	const area = $derived.by(() => {
		const wanted = settingsGroupOf(page.url.pathname + page.url.search);
		return settingsMenu(admin).find((entry) => entry.group === wanted);
	});
	const areaEntries = $derived(
		area ? (settingsEntries('', admin, t).find((g) => g.group === area.group)?.entries ?? []) : [],
	);
	function clear() {
		settingsSearch.query = '';
		settingsSearch.active = 0;
	}

	const areaLabel = (id: string) => {
		const area = settingsAreas.find((a) => a.id === id);
		return area ? t(area.label) : id;
	};

	// PLAN 67-1: Systemsteuerung › Ablage › Einstellungen suchen puts the cursor
	// in the search. In a window the window itself answers (its side bar search,
	// PLAN 71); this one serves the phone's ⋯ menu.
	import { registerMenuCommands } from '#lib/menu-commands.svelte.ts';
	registerMenuCommands({
		'settings.search': () =>
			document.querySelector<HTMLInputElement>('.home-search input[type="search"]')?.focus(),
	});
</script>

{#snippet row(entry: SettingsEntry, id?: string, highlight = false)}
	{@const state = entry.section ? undefined : states[entry.id]}
	<div class="hit" class:highlight {id}>
		<SettingRow
			href={entry.href}
			icon={entry.icon}
			title={entry.label}
			desc={entry.section ? areaLabel(entry.id) : t(`settingshome.desc.${entry.id}` as MessageKey)}
		>
			{#snippet badge()}{#if showsNew(entry)}<span class="chip info">{t('settings.new')}</span
					>{/if}{/snippet}
			{#snippet control()}{#if state}<span class="chip {state.tone}">{t(state.text)}</span
					>{/if}{/snippet}
		</SettingRow>
	</div>
{/snippet}

{#if area}
	<PageHead title={t(area.label)} />
	<SettingGroup>
		{#each areaEntries as entry (entry.href)}{@render row(entry)}{/each}
	</SettingGroup>
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
			{#each groups as group (group.group)}
				{@const offset = flat.indexOf(group.entries[0])}
				<SettingGroup title={t(settingsGroupLabel(group.group))} id="{prefix}-group-{group.group}">
					{#each group.entries as entry, i (entry.href)}
						{@render row(entry, `${prefix}-${offset + i}`, offset + i === active)}
					{/each}
				</SettingGroup>
			{/each}
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
	.hit {
		border-radius: var(--srow-radius);
	}
	/* The hit the arrow keys of the search stand on. */
	.hit.highlight {
		box-shadow: inset 0 0 0 2px var(--accent);
	}
</style>
