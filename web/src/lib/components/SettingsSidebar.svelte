<script lang="ts">
	import { showsNew } from '#lib/settings-seen.svelte.ts';
	// The navigation beside every settings page (decision of 28 Oct 2026,
	// "Entwurf A – Seitenleiste"): search on top, then the groups with compact
	// rows. The current page is highlighted; a dot marks areas with a state.
	import { page } from '$app/state';
	import Icon from './Icon.svelte';
	import SettingsSearch from './SettingsSearch.svelte';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { settingsEntries } from '#lib/settings-search.ts';
	import { settingsSearch, settingsStates } from '#lib/settings-states.svelte.ts';

	const prefix = 'settings-side';
	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));
	const groups = $derived(settingsEntries(settingsSearch.query, admin, t));
	const flat = $derived(groups.flatMap((group) => group.entries));
	const searching = $derived(settingsSearch.query.trim() !== '');
	const states = $derived(settingsStates());
	const path = $derived(page.url.pathname.replace(/\/$/, ''));
	const active = $derived(
		searching && flat.length ? Math.min(settingsSearch.active, flat.length - 1) : -1,
	);

	function current(id: string): boolean {
		return path === `/settings/${id}` || path.startsWith(`/settings/${id}/`);
	}
	function picked() {
		settingsSearch.query = '';
		settingsSearch.active = 0;
	}
</script>

<nav class="settings-sidebar" aria-label={t('settings.navigation')}>
	<SettingsSearch results={flat} idPrefix={prefix} />
	<div class="groups" id="{prefix}-results">
		<!-- The overview says how many matched itself. -->
		{#if searching && path !== '/settings'}<p class="muted note" role="status">
				{flat.length ? t('ui.results', { count: flat.length }) : t('ui.noSettings')}
			</p>{/if}
		{#each groups as group (group.group)}
			{@const offset = flat.indexOf(group.entries[0])}
			<p class="group-title" id="{prefix}-group-{group.group}">
				{t(`settings.group.${group.group}` as MessageKey)}
			</p>
			<ul aria-labelledby="{prefix}-group-{group.group}">
				{#each group.entries as entry, i (entry.href)}
					{@const state = entry.section ? undefined : states[entry.id]}
					<li>
						<a
							id="{prefix}-{offset + i}"
							href={entry.href}
							class:highlight={offset + i === active}
							aria-current={!searching && current(entry.id) ? 'page' : undefined}
							onclick={picked}
						>
							<Icon name={entry.icon} size={16} />
							<span class="label">{entry.label}</span>
							{#if showsNew(entry)}<span class="new">{t('settings.new')}</span>{/if}
							{#if state}<span class="marker {state.tone}" title={t(state.text)}
									><span class="sr-only">{t(state.text)}</span></span
								>{/if}
						</a>
					</li>
				{/each}
			</ul>
		{/each}
	</div>
</nav>

<style>
	.settings-sidebar {
		display: flex;
		flex-direction: column;
		gap: 4px;
		min-width: 0;
	}
	.groups {
		display: flex;
		flex-direction: column;
	}
	.note {
		margin: 8px 8px 0;
		font-size: 0.8125rem;
	}
	.group-title {
		margin: 16px 8px 4px;
		font-size: 0.6875rem;
		font-weight: 600;
		letter-spacing: 0.08em;
		text-transform: uppercase;
		color: var(--muted);
	}
	ul {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: 1px;
	}
	a {
		display: flex;
		align-items: center;
		gap: 10px;
		min-height: 2rem;
		padding: 4px 8px;
		border-radius: var(--radius-ctl);
		color: var(--text);
		text-decoration: none;
		font-size: 0.875rem;
		line-height: 1.25;
	}
	a :global(svg) {
		flex: none;
		color: var(--muted);
	}
	a:hover,
	a.highlight {
		background: var(--chip);
	}
	a[aria-current='page'] {
		background: color-mix(in srgb, var(--accent) 14%, transparent);
		/* The accent alone is too faint on its own tint in light themes. */
		color: color-mix(in srgb, var(--accent) 60%, var(--text));
		font-weight: 500;
	}
	a[aria-current='page'] :global(svg) {
		color: var(--accent);
	}
	a:focus-visible {
		outline-offset: -2px;
	}
	.label {
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.new {
		flex: none;
		padding: 0 5px;
		border: 1px solid color-mix(in srgb, var(--accent) 45%, transparent);
		border-radius: var(--radius-small);
		color: color-mix(in srgb, var(--accent) 60%, var(--text));
		font-size: 0.6875rem;
		font-weight: 500;
		line-height: 1.4;
	}
	.marker {
		flex: none;
		width: 7px;
		height: 7px;
		margin-left: auto;
		border-radius: 50%;
		background: var(--accent);
	}
	.marker.warn {
		background: var(--status-warn);
	}
	.marker.error {
		background: var(--status-error);
	}
</style>
