<script lang="ts">
	// The policies of the selected cell of the matrix, in order, and its
	// default. Changes go into the draft (on trial when applied).
	import type { FirewallConfig, FirewallPolicy, FirewallService } from '#lib/api.ts';
	import Segments from '#lib/components/Segments.svelte';
	import { APPS, NAS } from '#lib/firewall.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import PolicyEditor from './PolicyEditor.svelte';

	let {
		draft = $bindable(),
		from,
		to,
		fromLabel,
		toLabel,
		services,
		apps,
		locked,
		hostNetworkApps = [],
		onclose,
	}: {
		/** The page made sure the cell is in draft.cells. */
		draft: FirewallConfig;
		from: string;
		to: string;
		fromLabel: string;
		toLabel: string;
		services: FirewallService[];
		apps: { key: string; name: string }[];
		locked: boolean;
		/** Apps in the host network: their ports are the NAS's own. */
		hostNetworkApps?: string[];
		onclose: () => void;
	} = $props();

	const index = $derived(draft.cells.findIndex((c) => c.from === from && c.to === to));
	const cell = $derived(draft.cells[index]);

	function add() {
		const policy: FirewallPolicy =
			cell.to === NAS
				? { action: 'allow', protocol: 'any', service: services[0]?.id ?? 'webui' }
				: cell.to === APPS && apps.length
					? { action: 'allow', protocol: 'any', app: apps[0].key }
					: { action: 'allow', protocol: 'tcp', ports: '' };
		cell.policies.push(policy);
	}
	function move(i: number, by: number) {
		const j = i + by;
		if (j < 0 || j >= cell.policies.length) return;
		const list = cell.policies;
		[list[i], list[j]] = [list[j], list[i]];
	}
</script>

{#if cell}<section class="tile panel cell" aria-labelledby="firewall-cell">
		<div class="title-row">
			<h2 class="tile-title" id="firewall-cell">
				{t('firewall.cellTitle', { from: fromLabel, to: toLabel })}
			</h2>
			<button class="btn small" type="button" onclick={onclose}>{t('common.close')}</button>
		</div>
		<p class="muted">{t('firewall.cellText')}</p>
		{#if to === APPS}<p class="muted">{t('firewall.appsText')}</p>{/if}
		{#if to === APPS && hostNetworkApps.length}<p class="notice-warn">
				{t('firewall.hostNetworkHint', { apps: hostNetworkApps.join(', ') })}
			</p>{/if}
		{#if cell.policies.length}<ol class="policies">
				{#each cell.policies as _, i (i)}
					<PolicyEditor
						bind:policy={draft.cells[index].policies[i]}
						to={cell.to}
						{services}
						{apps}
						index={i}
						count={cell.policies.length}
						{locked}
						onmove={(by) => move(i, by)}
						onremove={() => cell.policies.splice(i, 1)}
					/>
				{/each}
			</ol>{:else}<p class="muted">{t('firewall.noPolicies')}</p>{/if}
		<div class="row-actions start">
			<button
				class="btn"
				type="button"
				disabled={locked || cell.policies.length >= 640}
				onclick={add}>{t('firewall.addPolicy')}</button
			>
		</div>
		<div class="default">
			<span>{t('firewall.default')}</span>
			<Segments
				options={[
					{ value: 'allow', label: t('firewall.allow') },
					{ value: 'deny', label: t('firewall.deny') },
				]}
				bind:value={draft.cells[index].default}
				label={t('firewall.default')}
				disabled={locked}
			/>
		</div>
	</section>{/if}

<style>
	.title-row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
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
	}
	.default {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px 12px;
	}
</style>
