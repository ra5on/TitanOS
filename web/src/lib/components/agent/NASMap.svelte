<script lang="ts">
	// The NAS map of the KI-Agent (PLAN 44, Runde 6): what the agent knows
	// about where things are, with a description per share, folder, app, VM,
	// system container, backup and room. Owners and administrators rebuild
	// it and write descriptions; the agent also writes some (marked).
	import { api } from '#lib/api.ts';
	import { formatDateTime } from '#lib/format.ts';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';

	let { admin = false }: { admin?: boolean } = $props();

	type Item = { name: string; kind?: string; state?: string; detail?: string; address?: string };
	type Share = { name: string; root: string; volume?: string; folders?: string[] };
	type NASMap = {
		built: string;
		shares?: Share[];
		apps?: Item[];
		vms?: Item[];
		lxc?: Item[];
		backups?: Item[];
		rooms?: string[];
		volumes?: Item[];
	};
	type Description = { text: string; by?: string; at: string };

	let map = $state<NASMap | null>(null);
	let built = $state(false);
	let desc = $state<Record<string, Description>>({});
	let busy = $state(false);
	let error = $state('');
	let editing = $state('');
	let draft = $state('');

	async function load(refresh = false) {
		busy = true;
		error = '';
		try {
			const r = refresh
				? await api.post<{
						map: NASMap;
						built: boolean;
						descriptions: Record<string, Description>;
					}>('/agent/map/refresh')
				: await api.get<{ map: NASMap; built: boolean; descriptions: Record<string, Description> }>(
						'/agent/map',
					);
			map = r.map;
			built = r.built;
			desc = r.descriptions ?? {};
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			busy = false;
		}
	}
	$effect(() => {
		void load();
	});
	async function save(key: string) {
		try {
			const r = await api.put<{ descriptions: Record<string, Description> }>(
				'/agent/descriptions',
				{ key, text: draft },
			);
			desc = r.descriptions;
			editing = '';
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	const sections = $derived(
		map
			? ([
					['apps', 'app:', map.apps ?? []],
					['vms', 'vm:', map.vms ?? []],
					['lxc', 'lxc:', map.lxc ?? []],
					['backups', 'backup:', map.backups ?? []],
				] as const)
			: [],
	);
</script>

{#snippet note(key: string)}
	{#if editing === key}
		<span class="edit">
			<input class="field" bind:value={draft} maxlength="300" aria-label={t('agent.mapDescribe')} />
			<button type="button" class="btn btn-primary" onclick={() => save(key)}
				>{t('common.save')}</button
			>
			<button type="button" class="btn" onclick={() => (editing = '')}>{t('common.cancel')}</button>
		</span>
	{:else}
		{#if desc[key]}<span class="desc"
				>– {desc[key].text}{#if desc[key].by?.startsWith('agent')}<span class="chip"
						>{t('agent.mapByAgent')}</span
					>{/if}</span
			>{/if}
		{#if admin}<button
				type="button"
				class="link"
				onclick={() => {
					editing = key;
					draft = desc[key]?.text ?? '';
				}}>{desc[key] ? t('agent.edit') : t('agent.mapDescribe')}</button
			>{/if}
	{/if}
{/snippet}

<p class="muted">{t('agent.mapIntro')}</p>
<div class="head">
	<span class="muted"
		>{built && map
			? t('agent.mapBuilt', { when: formatDateTime(new Date(map.built)) })
			: t('agent.mapNotBuilt')}</span
	>
	{#if admin}<button type="button" class="btn" disabled={busy} onclick={() => load(true)}
			>{busy ? t('agent.loading') : t('agent.mapRefresh')}</button
		>{/if}
</div>
{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
{#if map}
	<div class="map">
		{#if map.shares?.length}
			<h3>{t('agent.mapShares')}</h3>
			{#each map.shares as s (s.root)}
				<div class="row">
					<strong>{s.name}</strong> <span class="muted">{s.volume ?? ''}</span>
					{@render note('share:' + s.name)}
				</div>
				{#each s.folders ?? [] as f (f)}
					<div class="row sub">📁 {f} {@render note('folder:' + s.root + '/' + f)}</div>
				{/each}
			{/each}
		{/if}
		{#each sections as [label, prefix, items] (label)}
			{#if items.length}
				<h3>{t(`agent.map.${label}` as MessageKey)}</h3>
				{#each items as it (it.name)}
					<div class="row">
						<strong>{it.name}</strong>
						<span class="muted"
							>{[it.state, it.address, it.detail].filter(Boolean).join(' · ')}</span
						>
						{@render note(prefix + it.name)}
					</div>
				{/each}
			{/if}
		{/each}
		{#if map.rooms?.length}
			<h3>{t('agent.mapRooms')}</h3>
			{#each map.rooms as r (r)}
				<div class="row">{r} {@render note('room:' + r)}</div>
			{/each}
		{/if}
	</div>
{/if}

<style>
	.head {
		display: flex;
		justify-content: space-between;
		align-items: center;
		gap: var(--space-2);
		flex-wrap: wrap;
		margin: var(--space-2) 0;
	}
	.map {
		max-height: 50vh;
		overflow-y: auto;
	}
	h3 {
		font-size: var(--text-sm);
		text-transform: uppercase;
		letter-spacing: 0.06em;
		color: var(--muted);
		margin: var(--space-3) 0 var(--space-1);
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1-5);
		align-items: baseline;
		padding: 5px 0;
		border-bottom: 1px solid var(--border);
		font-size: var(--text-base);
	}
	.row.sub {
		padding-left: 18px;
	}
	.desc {
		color: var(--text);
	}
	.link {
		border: 0;
		background: none;
		color: var(--accent);
		font: inherit;
		font-size: var(--text-sm);
		cursor: pointer;
		padding: 0;
	}
	.edit {
		display: flex;
		gap: var(--space-1-5);
		flex: 1;
		min-width: 240px;
	}
	.edit .field {
		flex: 1;
	}
</style>
