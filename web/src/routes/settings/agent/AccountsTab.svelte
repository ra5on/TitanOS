<script lang="ts">
	// Settings › KI-Agent › Konten: who may use the agent, and its named places.
	import Icon from '#lib/components/Icon.svelte';
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

<section class="tile ui-card" aria-labelledby="ag-users">
	<div class="card-head"><h2 id="ag-users">{t('agent.sectionAccounts')}</h2></div>
	<div class="set-row one"><span class="set-hint">{t('agent.accountsHint')}</span></div>
	{#each userAccounts as a (a.name)}
		<label class="set-row">
			<span class="set-label"
				>{a.name}<span class="set-hint"
					>{t(a.role === 'admin' ? 'agent.roleAdmin' : 'agent.roleUser')}</span
				></span
			>
			<span class="set-control end"
				><input
					type="checkbox"
					class="switch"
					role="switch"
					checked={settings.users?.includes(a.name)}
					onchange={(e) => toggleUser(a.name, e.currentTarget.checked)}
				/></span
			>
		</label>
	{:else}
		<div class="set-row one"><span class="set-hint">{t('agent.accountsNone')}</span></div>
	{/each}
</section>

<section class="tile ui-card" aria-labelledby="ag-loc">
	<div class="card-head">
		<h2 id="ag-loc">{t('agent.sectionLocations')}</h2>
		<div class="card-tools">
			<button
				type="button"
				class="btn"
				onclick={() =>
					(settings!.locations = [
						...(settings!.locations ?? []),
						{ name: '', root: roots[0]?.id ?? '', path: '', note: '' },
					])}><Icon name="plus" size={15} />{t('agent.locationAdd')}</button
			>
		</div>
	</div>
	<div class="set-row one"><span class="set-hint">{t('agent.locationsHint')}</span></div>
	{#each settings.locations ?? [] as l, i (i)}
		<div class="set-row loc">
			<span class="set-label loc-fields">
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
			<div class="set-control end">
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
</section>

<style>
	/* PLAN 68: every tab in cards with rows "label (+ hint) left, control
	   right"; lists as rows; the card's own actions in its foot. */
	.set-row.one {
		grid-template-columns: minmax(0, 1fr);
	}

	/* The hint on the card's surface: a little toward the text for 4.5:1. */
	.set-hint {
		color: color-mix(in srgb, var(--muted) 85%, var(--text));
	}

	.set-control .btn {
		max-width: 100%;
		white-space: normal;
		height: auto;
	}

	/* Ablageorte: the fields of a place left, its delete button right. */
	.loc-fields {
		flex-direction: row;
		flex-wrap: wrap;
		gap: var(--space-2);
	}
	.loc-fields .field {
		flex: 1 1 9rem;
		min-width: 0;
	}
	.set-row.loc {
		grid-template-columns: minmax(0, 1fr) auto;
	}
	/* The phone: long choices on a line of their own. */
	@media (max-width: 639px) {
		.set-row.loc {
			align-items: start;
		}
	}
</style>
