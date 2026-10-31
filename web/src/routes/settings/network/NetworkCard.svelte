<script lang="ts">
	// One named network: what it is for, where it runs, its live state and
	// the addresses, gateway and name servers at a glance.
	import { t } from '#lib/i18n/index.svelte.ts';
	import { revealTarget } from '#lib/reveal-target.ts';
	import Dropdown from '#lib/components/Dropdown.svelte';
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
	const gateways = $derived([n.gateway, n.gateway6].filter(Boolean));
</script>

<article class="net" aria-labelledby="net-{n.id}">
	<header>
		<div class="title">
			<strong id="net-{n.id}">{n.name}</strong>
			<span class="muted">{t(`netx.purpose.${n.purpose}`)}</span>
		</div>
		<Dropdown label={t('netx.actionsFor', { name: n.name })} icon="more" align="right">
			{#snippet children(close)}
				<button
					type="button"
					role="menuitem"
					{disabled}
					onclick={() => {
						close();
						onedit();
					}}><Icon name="edit" size={16} /><span>{t('netx.editShort')}</span></button
				>
				<button
					type="button"
					role="menuitem"
					disabled={!applied || !!status.pending}
					onclick={() => {
						close();
						ondiagnose();
					}}><Icon name="activity" size={16} /><span>{t('netx.tab.diagnostics')}</span></button
				>
				<button
					type="button"
					role="menuitem"
					class="danger"
					disabled={disabled || n.management}
					title={n.management ? t('netx.removeManagement') : undefined}
					onclick={() => {
						close();
						onremove();
					}}><Icon name="trash" size={16} /><span>{t('netx.remove')}</span></button
				>
			{/snippet}
		</Dropdown>
	</header>
	<div class="chips">
		{#if state === 'draft'}<span class="chip">{t('netx.live.draft')}</span
			>{:else if state !== 'unknown'}<span class="chip {chip[state]}"
				>{t(`netx.live.${state}`)}</span
			>{/if}
		{#if n.management}<span class="chip info">{t('netx.management')}</span>{/if}
		{#if n.vlan}<span class="chip">{t('netx.vlanChip', { id: n.vlan })}</span>{/if}
		{#if n.bridge}<span class="chip">{t('netx.bridgeChip')}</span>{/if}
	</div>
	<dl>
		<div>
			<dt>{t('netx.connection')}</dt>
			<dd class="mono">
				{parentLabel(n.parent) || '–'}{live?.device && state !== 'draft' ? ` → ${live.device}` : ''}
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
</article>

<style>
	.net {
		display: flex;
		flex-direction: column;
		gap: 10px;
		padding: var(--tile-pad);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		min-width: 0;
	}
	header {
		display: flex;
		align-items: flex-start;
		justify-content: space-between;
		gap: 8px;
	}
	.title {
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.title .muted {
		font-size: var(--text-base);
	}
	.chips {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
	}
	dl {
		display: grid;
		grid-template-columns: auto 1fr;
		gap: 4px 12px;
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
</style>
