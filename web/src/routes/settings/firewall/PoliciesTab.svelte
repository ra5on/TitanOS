<script lang="ts">
	// All policies of all cells as one list, filterable by source zone and
	// destination; cards instead of a wide table so it works on phones.
	import type { FirewallConfig, FirewallService } from '#lib/api.ts';
	import { APPS, destinations, editCell, NAS } from '#lib/firewall.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import PolicyEditor from './PolicyEditor.svelte';

	let {
		draft = $bindable(),
		services,
		apps,
		zoneLabel,
		locked,
	}: {
		draft: FirewallConfig;
		services: FirewallService[];
		apps: { key: string; name: string }[];
		zoneLabel: (id: string) => string;
		locked: boolean;
	} = $props();

	let from = $state('');
	let to = $state('');
	const destLabel = (id: string) =>
		id === NAS ? t('firewall.toNas') : id === APPS ? t('firewall.toApps') : zoneLabel(id);
	const allDestinations = $derived([NAS, APPS, ...draft.zones.map((z) => z.id)]);
	const entries = $derived(
		draft.cells.flatMap((cell, ci) =>
			(!from || cell.from === from) &&
			(!to || cell.to === to) &&
			destinations(draft, cell.from).includes(cell.to)
				? cell.policies.map((_, pi) => ({ ci, pi }))
				: [],
		),
	);
	const canAdd = $derived(!!from && !!to && destinations(draft, from).includes(to));

	function add() {
		if (!canAdd) return;
		const cell = editCell(draft, services, from, to);
		cell.policies.push(
			to === NAS
				? { action: 'allow', protocol: 'any', service: services[0]?.id ?? 'webui' }
				: { action: 'allow', protocol: 'tcp', ports: '' },
		);
	}
	function move(ci: number, pi: number, by: number) {
		const list = draft.cells[ci].policies;
		const j = pi + by;
		if (j < 0 || j >= list.length) return;
		[list[pi], list[j]] = [list[j], list[pi]];
	}
</script>

<section class="tile panel" aria-labelledby="firewall-policies">
	<h2 class="tile-title" id="firewall-policies">{t('firewall.tab.policies')}</h2>
	<p class="muted">{t('firewall.policiesText')}</p>
	<div class="filters">
		<label class="stack"
			><span>{t('firewall.from')}</span><select class="field" bind:value={from}
				><option value="">{t('firewall.filterAll')}</option>{#each draft.zones as z (z.id)}<option
						value={z.id}>{zoneLabel(z.id)}</option
					>{/each}</select
			></label
		>
		<label class="stack"
			><span>{t('firewall.to')}</span><select class="field" bind:value={to}
				><option value="">{t('firewall.filterAll')}</option>{#each allDestinations as d (d)}<option
						value={d}>{destLabel(d)}</option
					>{/each}</select
			></label
		>
	</div>
	{#if entries.length}<ol class="policies">
			{#each entries as entry (`${entry.ci}:${entry.pi}`)}
				{@const cell = draft.cells[entry.ci]}
				<PolicyEditor
					bind:policy={draft.cells[entry.ci].policies[entry.pi]}
					to={cell.to}
					{services}
					{apps}
					index={entry.pi}
					count={cell.policies.length}
					{locked}
					heading={t('firewall.cellTitle', { from: zoneLabel(cell.from), to: destLabel(cell.to) })}
					onmove={(by) => move(entry.ci, entry.pi, by)}
					onremove={() => draft.cells[entry.ci].policies.splice(entry.pi, 1)}
				/>
			{/each}
		</ol>{:else}<p class="muted">{t('firewall.noMatch')}</p>{/if}
	<div class="row-actions start">
		<button class="btn" type="button" disabled={locked || !canAdd} onclick={add}
			>{t('firewall.addPolicy')}</button
		>
		{#if !canAdd}<span class="muted small">{t('firewall.addPolicyHint')}</span>{/if}
	</div>
</section>

<style>
	.filters {
		display: flex;
		flex-wrap: wrap;
		gap: 12px;
	}
	.filters .stack {
		flex: 1 1 12rem;
	}
	.policies {
		display: flex;
		flex-direction: column;
		gap: 8px;
		margin: 0;
		padding: 0;
	}
	.start {
		justify-content: flex-start;
		align-items: center;
	}
	.small {
		font-size: var(--text-base);
	}
</style>
