<script lang="ts">
	import { showsNew } from '#lib/settings-seen.svelte.ts';
	// The overview: every area as a compact row with its state, in the groups
	// and order of the sidebar. On phones it is the navigation, with the search
	// above it; on wider screens the sidebar holds the search.
	import { settingsEntries } from '#lib/settings-search.ts';
	import Icon from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingsSearch from '#lib/components/SettingsSearch.svelte';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { settingsSearch, settingsStates } from '#lib/settings-states.svelte.ts';

	const prefix = 'settings-list';
	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));
	const groups = $derived(settingsEntries(settingsSearch.query, admin, t));
	const flat = $derived(groups.flatMap((group) => group.entries));
	const searching = $derived(settingsSearch.query.trim() !== '');
	const states = $derived(settingsStates());
	const active = $derived(
		searching && flat.length ? Math.min(settingsSearch.active, flat.length - 1) : -1,
	);

	function clear() {
		settingsSearch.query = '';
		settingsSearch.active = 0;
	}
</script>

<PageHead title={t('settings.title')} />

<div class="phone-search">
	<SettingsSearch results={flat} idPrefix={prefix} visibleLabel />
</div>
{#if searching}<div class="search-result">
		<span role="status">{t('ui.results', { count: flat.length })}</span><button
			class="btn"
			type="button"
			onclick={clear}>{t('ui.clear')}</button
		>
	</div>{/if}
{#if searching && flat.length === 0}<p>{t('ui.noSettings')}</p>{/if}
<div class="groups" id="{prefix}-results">
	{#each groups as group (group.group)}
		{@const offset = flat.indexOf(group.entries[0])}
		<section class="tile settings-group" aria-labelledby="group-{group.group}">
			<h2 class="tile-title" id="group-{group.group}">
				{t(`settings.group.${group.group}` as MessageKey)}
			</h2>
			<ul class="areas">
				{#each group.entries as entry, i (entry.href)}
					{@const state = entry.section ? undefined : states[entry.id]}
					<li>
						<a
							id="{prefix}-{offset + i}"
							href={entry.href}
							class:highlight={offset + i === active}
							onclick={clear}
							><span class="setting-icon"><Icon name={entry.icon} size={16} /></span><span
								class="name">{entry.label}</span
							>{#if showsNew(entry)}<span class="new">{t('settings.new')}</span
								>{/if}{#if state}<span class="chip {state.tone}">{t(state.text)}</span>{/if}<span
								class="chevron"><Icon name="arrowRight" size={14} /></span
							></a
						>
					</li>
				{/each}
			</ul>
		</section>
	{/each}
</div>

<style>
	.groups {
		display: flex;
		flex-direction: column;
		gap: var(--gap);
	}
	.settings-group {
		display: flex;
		flex-direction: column;
		min-width: 0;
		overflow: hidden;
	}
	.settings-group h2 {
		padding: 10px var(--tile-pad);
		border-bottom: 1px solid var(--panel-border);
	}
	ul {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	li + li {
		border-top: 1px solid var(--panel-border);
	}
	a {
		display: flex;
		align-items: center;
		gap: 12px;
		min-height: 44px;
		padding: 6px var(--tile-pad);
		color: var(--text);
		text-decoration: none;
	}
	a:hover,
	a.highlight {
		background: var(--chip);
	}
	a:focus-visible {
		outline-offset: -2px;
	}
	.setting-icon {
		display: grid;
		place-items: center;
		flex: none;
		width: 28px;
		height: 28px;
		border-radius: var(--radius-small);
		background: color-mix(in srgb, var(--accent) 12%, transparent);
		color: var(--accent);
	}
	.name {
		flex: 1;
		min-width: 0;
		font-weight: 500;
		overflow-wrap: anywhere;
	}
	.new {
		flex: none;
		padding: 0 5px;
		border: 1px solid color-mix(in srgb, var(--accent) 45%, transparent);
		border-radius: var(--radius-small);
		color: color-mix(in srgb, var(--accent) 60%, var(--text));
		font-size: 0.6875rem;
	}
	.chip {
		flex: none;
	}
	.chevron {
		display: flex;
		flex: none;
		color: var(--muted);
	}
	.search-result {
		display: flex;
		align-items: center;
		gap: 12px;
	}
	/* Wider screens search in the sidebar. */
	@media (min-width: 761px) {
		.phone-search {
			display: none;
		}
	}
</style>
