<script lang="ts">
	// Create or edit an own zone: name, networks of the NAS (grouped:
	// networks/VLANs, Docker, VM/LXC NAT) and source networks. A network is
	// in at most one zone; picking it here takes it out of another one.
	import type { FirewallConfig, FirewallNetwork, FirewallZoneDef } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { zoneOfNetwork, zoneProblem } from '#lib/firewall.ts';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { parseNetworks } from '#lib/ssh-networks.ts';

	let {
		zone,
		isNew = false,
		cfg,
		networks,
		zoneLabel,
		onsave,
		onremove,
		onclose,
	}: {
		/** The zone to edit, or a new one. */
		zone: FirewallZoneDef;
		isNew?: boolean;
		cfg: FirewallConfig;
		networks: FirewallNetwork[];
		zoneLabel: (id: string) => string;
		onsave: (zone: FirewallZoneDef) => void;
		onremove?: () => void;
		onclose: () => void;
	} = $props();

	// svelte-ignore state_referenced_locally
	let name = $state(zone.name ?? '');
	// svelte-ignore state_referenced_locally
	let picked = $state<string[]>([...zone.networks]);
	// svelte-ignore state_referenced_locally
	let sourcesText = $state(zone.sources.join('\n'));
	let confirmRemove = $state(false);

	const sources = $derived(parseNetworks(sourcesText));
	const result = $derived<FirewallZoneDef>({ ...zone, name: name.trim(), networks: picked, sources });
	const problem = $derived(zoneProblem(result));
	const groups = $derived(
		(['network', 'docker', 'vm', 'dockerlan'] as const).map((kind) => ({
			kind,
			list: networks.filter((n) => n.kind === kind),
		})),
	);

	function toggle(id: string, on: boolean) {
		picked = on ? [...picked.filter((x) => x !== id), id] : picked.filter((x) => x !== id);
	}
	function elsewhere(n: FirewallNetwork): string {
		const z = zoneOfNetwork(cfg, n);
		return z && z !== zone.id ? z : '';
	}
</script>

<Dialog title={t(isNew ? 'firewall.zoneAdd' : 'firewall.zoneEdit')} wide {onclose}>
	<form
		class="column"
		onsubmit={(event) => {
			event.preventDefault();
			if (!problem) onsave(result);
		}}
	>
		<label class="stack"
			><span>{t('firewall.zoneNameLabel')}</span><input class="field" bind:value={name} maxlength="40" required /></label
		>
		{#each groups as group (group.kind)}
			<fieldset>
				<legend>{t(`firewall.group.${group.kind}` as MessageKey)}</legend>
				{#if group.list.length === 0}<p class="muted small">
						{t(group.kind === 'vm' ? 'firewall.groupEmptyVm' : 'firewall.groupEmpty')}
					</p>{/if}
				{#if group.kind === 'dockerlan' && group.list.length}<p class="muted small">{t('firewall.bypassText')}</p>{/if}
				{#each group.list as n (n.id)}
					<label class="check"
						><input
							type="checkbox"
							checked={picked.includes(n.id)}
							disabled={n.kind === 'dockerlan'}
							onchange={(event) => toggle(n.id, event.currentTarget.checked)}
						/><span
							><strong>{n.name}</strong>
							<span class="mono small muted">{n.device ?? n.parent ?? ''} {n.subnets.join(', ')}</span>
							{#if n.missing}<span class="chip warn">{t('firewall.netMissing')}</span>{/if}
							{#if elsewhere(n) && !picked.includes(n.id)}<span class="muted small"
									>· {t('firewall.inZone', { zone: zoneLabel(elsewhere(n)) })}</span
								>{/if}</span
						></label
					>
				{/each}
			</fieldset>
		{/each}
		<label class="stack"
			><span>{t('firewall.zoneSources')}</span><textarea
				class="field mono"
				rows="3"
				spellcheck="false"
				placeholder="10.20.0.0/16"
				bind:value={sourcesText}
			></textarea></label
		>
		<p class="muted small">{t('firewall.zoneSourcesHint')}</p>
		{#if problem}<p class="notice-warn" role="alert">{t(problem as MessageKey)}</p>{/if}
		<div class="dialog-actions">
			{#if onremove}<button type="button" class="btn btn-warn" onclick={() => (confirmRemove = true)}
					>{t('firewall.zoneRemove')}</button
				>{/if}
			<button type="button" class="btn" onclick={onclose}>{t('common.cancel')}</button>
			<button type="submit" class="btn btn-primary" disabled={!!problem}>{t('firewall.zoneApply')}</button>
		</div>
	</form>
</Dialog>

{#if confirmRemove && onremove}
	<Dialog title={t('firewall.zoneRemove')} onclose={() => (confirmRemove = false)}>
		<p>{t('firewall.zoneRemoveText', { name: zone.name ?? '' })}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (confirmRemove = false)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-warn" onclick={onremove}>{t('firewall.zoneRemove')}</button>
		</div>
	</Dialog>
{/if}

<style>
	fieldset {
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		padding: 8px 12px;
		margin: 0;
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
	legend {
		padding: 0 4px;
		font-weight: 500;
	}
	.column {
		display: flex;
		flex-direction: column;
		gap: 12px;
	}
	.small {
		font-size: 0.8125rem;
	}
</style>
