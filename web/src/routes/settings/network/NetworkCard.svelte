<script lang="ts">
	// One named network: what it is for, where it runs, its live state and
	// the addresses, gateway and name servers at a glance – as a row that
	// opens (PLAN 78-1).
	import { t } from '#lib/i18n/index.svelte.ts';
	import { revealTarget } from '#lib/reveal-target.ts';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { liveState, parentLabel, type NASNetwork, type NetworkStatus } from '#lib/network.ts';

	let {
		network: n,
		status,
		applied,
		disabled,
		onedit,
		ondiagnose,
		onremove,
	}: {
		network: NASNetwork;
		status: NetworkStatus;
		applied: boolean;
		disabled: boolean;
		onedit: () => void;
		ondiagnose: () => void;
		onremove: () => void;
	} = $props();

	const state = $derived(liveState(status, n, applied));
	const live = $derived(status.live?.[n.id]);
	const chip = { up: 'ok', noAddress: 'warn', down: 'error' } as const;
	const ipv4 = $derived(
		n.ipv4 === 'static' ? n.address : n.ipv4 === 'dhcp' ? t('netx.mode.dhcp') : t('netx.off'),
	);
	const ipv6 = $derived(
		n.ipv6 === 'static'
			? n.address6
			: n.ipv6 === 'auto'
				? t('netx.mode.auto')
				: n.ipv6 === 'dhcp'
					? t('netx.mode.dhcp6')
					: t('netx.off'),
	);
	// Closed, the row says where the network lives: its address now, else how
	// it gets one.
	const summary = $derived(
		(state !== 'draft' && live?.addresses?.[0]) || (n.ipv4 !== 'off' ? ipv4 : ipv6) || '',
	);
	const gateways = $derived([n.gateway, n.gateway6].filter(Boolean));
</script>

<!-- PLAN 78-1: a network is a row – name, use and state; it opens to its
     addresses and its actions (edit, diagnostics, remove). -->
<SettingRow
	icon="globe"
	title={n.name}
	desc={t(`netx.purpose.${n.purpose}`)}
	id="net-{n.id}"
	anchors={n.management ? ['network-dns'] : []}
	{summary}
>
	{#snippet control()}
		{#if state === 'draft'}<span class="chip">{t('netx.live.draft')}</span
			>{:else if state !== 'unknown'}<span class="chip {chip[state]}"
				>{t(`netx.live.${state}`)}</span
			>{/if}
		{#if n.management}<span class="chip info">{t('netx.management')}</span>{/if}
		{#if n.vlan}<span class="chip">{t('netx.vlanChip', { id: n.vlan })}</span>{/if}
		{#if n.bridge}<span class="chip">{t('netx.bridgeChip')}</span>{/if}
	{/snippet}
	<div class="srow-block">
		<dl>
			<div>
				<dt>{t('netx.connection')}</dt>
				<dd class="mono">
					{parentLabel(n.parent) || '–'}{live?.device && state !== 'draft'
						? ` → ${live.device}`
						: ''}
				</dd>
			</div>
			{#if state !== 'draft' && state !== 'unknown'}
				<div>
					<dt>{t('netx.current')}</dt>
					<dd class="mono">
						{#each live?.addresses ?? [] as address (address)}<span>{address}</span>{:else}–{/each}
					</dd>
				</div>
			{/if}
			<div>
				<dt>{t('network.ipv4')}</dt>
				<dd class="mono">{ipv4}</dd>
			</div>
			<div>
				<dt>{t('network.ipv6')}</dt>
				<dd class="mono">{ipv6}</dd>
			</div>
			<div>
				<dt>{t('netx.gateway')}</dt>
				<dd class="mono">
					{#each gateways as gateway (gateway)}<span>{gateway}</span>{:else}{n.management
							? t('netx.fromNetwork')
							: t('netx.none')}{/each}
				</dd>
			</div>
			<div>
				<dt id={n.management ? 'network-dns' : undefined} use:revealTarget>{t('netcfg.dns')}</dt>
				<dd class="mono">
					{#each n.dns as server (server)}<span>{server}</span>{:else}{n.management
							? t('netx.fromNetwork')
							: t('netx.none')}{/each}
				</dd>
			</div>
		</dl>
	</div>
	<div class="srow-block actions" role="group" aria-label={t('netx.actionsFor', { name: n.name })}>
		<button type="button" class="btn" {disabled} onclick={onedit}
			><Icon name="edit" size={16} />{t('netx.editShort')}</button
		>
		<button type="button" class="btn" disabled={!applied || !!status.pending} onclick={ondiagnose}
			><Icon name="activity" size={16} />{t('netx.tab.diagnostics')}</button
		>
		<button
			type="button"
			class="btn btn-danger"
			disabled={disabled || n.management}
			onclick={onremove}><Icon name="trash" size={16} />{t('netx.remove')}</button
		>
		{#if n.management}<span class="muted why">{t('netx.removeManagement')}</span>{/if}
	</div>
</SettingRow>

<style>
	dl {
		display: grid;
		grid-template-columns: auto 1fr;
		gap: var(--space-1) var(--space-3);
		margin: 0;
		font-size: var(--text-md);
	}
	dl > div {
		display: contents;
	}
	dt {
		color: var(--muted);
	}
	dd {
		margin: 0;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	dd > span {
		display: block;
	}
	.why {
		flex: 1 1 16rem;
		font-size: var(--text-sm);
	}
</style>
