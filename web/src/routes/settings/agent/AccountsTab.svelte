<script lang="ts">
	// Settings › KI-Agent › Konten: who may use the agent, and its named places.
	import Icon from '#lib/components/Icon.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import type { FileRoot } from '#lib/api.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import type { Settings } from '#lib/agent.ts';

	let {
		settings = $bindable(),
		accounts,
		roots,
	}: {
		settings: Settings;
		accounts: { name: string; role: string }[];
		roots: FileRoot[];
	} = $props();

	const userAccounts = $derived(accounts.filter((a) => a.role !== 'owner'));
	function toggleUser(name: string, on: boolean) {
		if (!settings) return;
		const set = new Set(settings.users ?? []);
		if (on) set.add(name);
		else set.delete(name);
		settings.users = [...set];
	}
</script>

<!-- PLAN 78-1: plain headings with one row per account and per place. -->
<SettingGroup title={t('agent.sectionAccounts')} desc={t('agent.accountsHint')}>
	{#each userAccounts as a, n (a.name)}
		<SettingRow
			icon="users"
			title={a.name}
			desc={t(a.role === 'admin' ? 'agent.roleAdmin' : 'agent.roleUser')}
			for="ag-user-{n}"
		>
			{#snippet control()}
				<input
					id="ag-user-{n}"
					type="checkbox"
					class="switch"
					role="switch"
					checked={settings.users?.includes(a.name)}
					onchange={(e) => toggleUser(a.name, e.currentTarget.checked)}
				/>
			{/snippet}
		</SettingRow>
	{:else}
		<p class="muted">{t('agent.accountsNone')}</p>
	{/each}
</SettingGroup>

<SettingGroup title={t('agent.sectionLocations')} desc={t('agent.locationsHint')}>
	{#snippet tools()}
		<button
			type="button"
			class="btn"
			onclick={() =>
				(settings!.locations = [
					...(settings!.locations ?? []),
					{ name: '', root: roots[0]?.id ?? '', path: '', note: '' },
				])}><Icon name="plus" size={15} />{t('agent.locationAdd')}</button
		>
	{/snippet}
	{#each settings.locations ?? [] as l, i (i)}
		<!-- A place is a row of its fields, the delete button at its end. -->
		<div class="srow plain">
			<div class="srow-head loc">
				<span class="loc-fields">
					<input
						class="field"
						bind:value={l.name}
						placeholder={t('agent.locationName')}
						aria-label={t('agent.locationName')}
					/>
					<select class="field" bind:value={l.root} aria-label={t('agent.locationRoot')}>
						<option value="">{t('agent.locationRoot')}</option>
						{#each roots as r (r.id)}<option value={r.id}
								>{r.name} ({r.kind === 'share' ? t('agent.share') : t('agent.volume')})</option
							>{/each}
					</select>
					<input
						class="field"
						bind:value={l.path}
						placeholder={t('agent.locationPath')}
						aria-label={t('agent.locationPath')}
					/>
					<input
						class="field"
						bind:value={l.note}
						placeholder={t('agent.locationNote')}
						aria-label={t('agent.locationNote')}
					/>
				</span>
				<button
					type="button"
					class="btn"
					aria-label={t('common.delete')}
					title={t('common.delete')}
					onclick={() => settings!.locations!.splice(i, 1)}><Icon name="trash" size={15} /></button
				>
			</div>
		</div>
	{/each}
</SettingGroup>

<style>
	/* Ablageorte: the fields of a place left, its delete button right. */
	.loc {
		flex-wrap: nowrap;
		align-items: flex-start;
	}
	.loc-fields {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
		flex: 1 1 0;
		min-width: 0;
	}
	.loc-fields .field {
		flex: 1 1 9rem;
		min-width: 0;
	}
	.loc > .btn {
		flex: none;
	}
</style>
