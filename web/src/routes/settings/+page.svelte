<script lang="ts">
	// The start page of the settings. In a window of the desktop (PLAN 71,
	// owner's decision "Variante C" of 7 Nov 2026) it is the "Übersicht": the
	// window's side bar with its search lists every area, so the page shows
	// only the overview cards. On the phone and in a tab of its own there is no
	// side bar: there it keeps "Alle Einstellungen" (PLAN 61-8, drafts 34/35) –
	// a search, every area as a tile, grouped like the side bar – and the same
	// overview cards below.
	import { inWindow } from '#lib/desktop/frame.ts';
	import SettingsOverview from './SettingsOverview.svelte';
	import { showsNew } from '#lib/settings-seen.svelte.ts';
	import { settingsAreas, settingsEntries, settingsGroupLabel } from '#lib/settings-search.ts';
	import Icon from '#lib/components/Icon.svelte';
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
		viewer: 'blue',
		backups: 'teal',
		files: 'amber',
		memory: 'cyan',
	};
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

{#if inWindow}
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

		{#if !searching}<SettingsOverview {admin} prefix="{prefix}-overview" />{/if}
	</div>
{/if}

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
	/* Two columns only where a long German word still fits (PLAN 69). */
	@container (min-width: 34rem) {
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
		gap: var(--space-1) var(--space-1-5);
		font-weight: 600;
		font-size: var(--text-md);
		overflow-wrap: break-word;
		hyphens: auto;
	}
	.desc {
		color: var(--muted);
		font-size: var(--text-sm);
		line-height: 1.35;
		overflow-wrap: break-word;
		hyphens: auto;
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
</style>
