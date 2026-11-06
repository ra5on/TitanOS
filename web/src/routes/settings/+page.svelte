<script lang="ts">
	// "Alle Einstellungen" (owner's wish of 6 Nov 2026, PLAN 61-8, drafts 34/35):
	// a search on top, then every area as an equal tile with a colored icon,
	// its name and what is inside, grouped like the sidebar; below, the last
	// settings changes from the activity log. The same page on phones (one
	// column) and inside a desktop window (no sidebar there).
	import { onMount } from 'svelte';
	import { showsNew } from '#lib/settings-seen.svelte.ts';
	import { settingsEntries, settingsGroupLabel, settingsAreas } from '#lib/settings-search.ts';
	import Icon from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingsSearch from '#lib/components/SettingsSearch.svelte';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { settingsSearch, settingsStates } from '#lib/settings-states.svelte.ts';
	import { api } from '#lib/api.ts';
	import { activityLabel, type ActivityEntry, type ActivityPage } from '#lib/activity.ts';
	import { recentSettingsChanges } from '#lib/settings-recent.ts';
	import { formatDateTime } from '#lib/format.ts';

	const prefix = 'settings-home';
	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));
	const groups = $derived(settingsEntries(settingsSearch.query, admin, t));
	const flat = $derived(groups.flatMap((group) => group.entries));
	const searching = $derived(settingsSearch.query.trim() !== '');
	const states = $derived(settingsStates());
	const active = $derived(
		searching && flat.length ? Math.min(settingsSearch.active, flat.length - 1) : -1,
	);

	// One color per area; the values are theme-independent tokens in app.css.
	const colors: Record<string, string> = {
		about: 'slate',
		updates: 'blue',
		timezone: 'teal',
		appearance: 'violet',
		hardware: 'slate',
		temperatures: 'orange',
		energy: 'amber',
		ups: 'green',
		'power-button': 'red',
		network: 'blue',
		firewall: 'red',
		wireguard: 'green',
		ddns: 'orange',
		letsencrypt: 'green',
		proxy: 'cyan',
		access: 'violet',
		services: 'slate',
		notifications: 'red',
		homeassistant: 'blue',
		agent: 'violet',
		mcp: 'cyan',
		api: 'slate',
		cluster: 'teal',
		programs: 'pink',
		backups: 'teal',
		files: 'amber',
		memory: 'cyan',
	};
	const areaLabel = (id: string) => {
		const area = settingsAreas.find((a) => a.id === id);
		return area ? t(area.label) : id;
	};

	// The last settings changes; only owners and administrators may read the
	// log, and without it (off, error) the section is left out.
	let recent = $state<(ActivityEntry & { area: string })[]>([]);
	onMount(() => {
		if (!admin) return;
		api
			.get<ActivityPage>('/activity?days=30&limit=500')
			.then((page) => (recent = recentSettingsChanges(page.entries ?? [])))
			.catch(() => (recent = []));
	});
	function describe(entry: ActivityEntry): string {
		const label = activityLabel(entry.action);
		if (label.kind === 'named') return t(`activity.do.${label.key}` as 'activity.do.login');
		const verb = t(`activity.verb.${label.verb}` as 'activity.verb.run');
		return entry.target ? `${verb}: ${entry.target}` : verb;
	}

	function clear() {
		settingsSearch.query = '';
		settingsSearch.active = 0;
	}
</script>

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

	<div class="groups" id="{prefix}-results">
		{#each groups as group (group.group)}
			{@const offset = flat.indexOf(group.entries[0])}
			<section class="group" aria-labelledby="{prefix}-group-{group.group}">
				<h2 class="group-title" id="{prefix}-group-{group.group}">
					{t(settingsGroupLabel(group.group))}
				</h2>
				<ul class="tiles">
					{#each group.entries as entry, i (entry.href)}
						{@const state = entry.section ? undefined : states[entry.id]}
						<li>
							<a
								id="{prefix}-{offset + i}"
								class="tile home-tile"
								class:highlight={offset + i === active}
								href={entry.href}
								onclick={clear}
							>
								<span
									class="square"
									style:--square="var(--icon-square-{colors[entry.id] ?? 'slate'})"
									aria-hidden="true"><Icon name={entry.icon} size={20} /></span
								>
								<span class="text">
									<span class="name"
										>{entry.label}{#if showsNew(entry)}<span class="new">{t('settings.new')}</span
											>{/if}</span
									>
									<span class="desc"
										>{entry.section
											? areaLabel(entry.id)
											: t(`settingshome.desc.${entry.id}` as MessageKey)}</span
									>
									{#if state}<span class="chip {state.tone}">{t(state.text)}</span>{/if}
								</span>
							</a>
						</li>
					{/each}
				</ul>
			</section>
		{/each}
	</div>

	{#if admin && recent.length && !searching}
		<section class="tile panel recent-panel" aria-labelledby="{prefix}-recent">
			<div class="recent-head">
				<h2 class="tile-title" id="{prefix}-recent">{t('settingshome.recent')}</h2>
				<a href="/tools/activity">{t('settingshome.recentAll')}</a>
			</div>
			<ul class="recent">
				{#each recent as entry, index (index)}
					<li>
						<a class="area" href="/settings/{entry.area}">{areaLabel(entry.area)}</a>
						<span class="what">{describe(entry)}</span>
						<span class="who"
							>{entry.user ? t('settingshome.recentBy', { user: entry.user }) : ''}</span
						>
						<time datetime={entry.at}>{formatDateTime(new Date(entry.at))}</time>
					</li>
				{/each}
			</ul>
		</section>
	{/if}
</div>

<style>
	.home {
		display: flex;
		flex-direction: column;
		gap: var(--gap);
		min-width: 0;
		container-type: inline-size;
	}
	.home-search {
		width: 100%;
		max-width: 36rem;
		align-self: center;
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
	.group {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		min-width: 0;
	}
	.group-title {
		margin: 0;
		font-size: var(--text-xs);
		font-weight: 600;
		letter-spacing: 0.08em;
		text-transform: uppercase;
		color: var(--muted);
	}
	ul {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.tiles {
		display: grid;
		grid-template-columns: minmax(0, 1fr);
		grid-auto-rows: 1fr;
		gap: var(--gap);
	}
	.tiles li {
		display: flex;
		min-width: 0;
	}
	@container (min-width: 26rem) {
		.tiles {
			grid-template-columns: repeat(2, minmax(0, 1fr));
		}
	}
	@container (min-width: 40rem) {
		.tiles {
			grid-template-columns: repeat(3, minmax(0, 1fr));
		}
	}
	@container (min-width: 54rem) {
		.tiles {
			grid-template-columns: repeat(4, minmax(0, 1fr));
		}
	}
	.home-tile {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		width: 100%;
		/* Room for two lines of description: equal tiles across the groups. */
		min-height: 5.25rem;
		padding: var(--space-3) var(--tile-pad);
		color: var(--text);
		text-decoration: none;
	}
	.home-tile:hover,
	.home-tile.highlight {
		background: var(--chip);
	}
	.home-tile:focus-visible {
		outline-offset: -2px;
	}
	.square {
		display: grid;
		place-items: center;
		flex: none;
		width: 2.5rem;
		height: 2.5rem;
		border-radius: var(--radius-ctl, 10px);
		background: linear-gradient(
			160deg,
			color-mix(in srgb, var(--square) 78%, #fff),
			var(--square) 70%
		);
		color: var(--on-icon-square);
		box-shadow: inset 0 0 0 1px color-mix(in srgb, #fff 18%, transparent);
	}
	.text {
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-width: 0;
	}
	.name {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-1) 6px;
		font-weight: 600;
		font-size: var(--text-md);
		overflow-wrap: anywhere;
	}
	.desc {
		color: var(--muted);
		font-size: var(--text-sm);
		line-height: 1.35;
		overflow-wrap: anywhere;
	}
	.new {
		padding: 0 5px;
		border: 1px solid color-mix(in srgb, var(--accent) 45%, transparent);
		border-radius: var(--radius-small);
		color: color-mix(in srgb, var(--accent) 60%, var(--text));
		font-size: var(--text-xs);
		font-weight: 500;
	}
	.chip {
		align-self: flex-start;
		margin-top: 2px;
	}
	.recent-head {
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		justify-content: space-between;
		gap: var(--space-2);
	}
	.recent li {
		display: grid;
		grid-template-columns: minmax(7rem, 12rem) minmax(0, 1fr) auto auto;
		align-items: baseline;
		gap: var(--space-1) var(--space-3);
		padding: var(--space-2) 0;
		font-size: var(--text-base);
	}
	.recent li + li {
		border-top: 1px solid var(--panel-border);
	}
	.recent .area {
		font-weight: 500;
		overflow-wrap: anywhere;
	}
	.what {
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.who,
	time {
		color: var(--muted);
		white-space: nowrap;
	}
	@container (max-width: 34rem) {
		.recent li {
			grid-template-columns: minmax(0, 1fr) auto;
		}
		.recent .what {
			grid-column: 1;
			grid-row: 2;
		}
		.recent .who {
			grid-column: 2;
			grid-row: 2;
		}
	}
</style>
