<script lang="ts">
	// Physical ports with what uses them, and the bonds of the draft.
	import { t } from '#lib/i18n/index.svelte.ts';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import {
		bondModeKeys,
		bondUsers,
		portUsers,
		type NetworkBond,
		type NetworkConfig,
		type NetworkPort,
	} from '#lib/network.ts';

	let {
		draft,
		ports,
		disabled,
		onaddbond,
		oneditbond,
		onremovebond,
	}: {
		draft: NetworkConfig;
		ports: NetworkPort[];
		disabled: boolean;
		onaddbond: () => void;
		oneditbond: (bond: NetworkBond) => void;
		onremovebond: (bond: NetworkBond) => void;
	} = $props();

	const speed = (mbps: number) =>
		mbps >= 1000 ? t('network.gbe', { speed: mbps / 1000 }) : t('network.mbit', { speed: mbps });
	const mode = (m: string) =>
		m in bondModeKeys ? t(bondModeKeys[m as keyof typeof bondModeKeys]) : m;
</script>

<section class="tile panel" aria-labelledby="net-ports">
	<h2 class="tile-title" id="net-ports">{t('netx.ports')}</h2>
	<div class="table-wrap">
		<table class="data-table">
			<thead>
				<tr>
					<th>{t('netx.port')}</th>
					<th>{t('netx.state')}</th>
					<th>{t('network.mac')}</th>
					<th>{t('netx.usedBy')}</th>
				</tr>
			</thead>
			<tbody>
				{#each ports as port (port.name)}
					{@const users = portUsers(draft, port.name)}
					<tr>
						<td class="mono">{port.name}</td>
						<td
							><span class="chip" class:ok={port.carrier}
								>{port.carrier ? t('netx.connected') : t('netx.disconnected')}</span
							>{#if port.carrier && port.speedMbps}<span class="muted speed"
									>{speed(port.speedMbps)}</span
								>{/if}</td
						>
						<td class="mono muted">{port.mac}</td>
						<td>
							{#each users as user, i (user.kind + user.name)}{i ? ', ' : ''}{user.kind === 'bond'
									? t('netx.bondNamed', { name: user.name })
									: user.name}{:else}<span class="muted">{t('netx.unused')}</span>{/each}
						</td>
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
</section>

<section class="tile panel" aria-labelledby="net-bonds">
	<h2 class="tile-title" id="net-bonds">{t('netx.bonds')}</h2>
	{#if draft.bonds?.length}
		<div class="table-wrap">
			<table class="data-table">
				<thead>
					<tr>
						<th>{t('netx.bondName')}</th>
						<th>{t('netx.ports')}</th>
						<th>{t('netx.bondMode')}</th>
						<th>{t('netx.usedBy')}</th>
						<th><span class="sr-only">{t('netx.actions')}</span></th>
					</tr>
				</thead>
				<tbody>
					{#each draft.bonds as bond (bond.id)}
						{@const users = bondUsers(draft, bond.id)}
						<tr>
							<td class="mono">{bond.id}</td>
							<td class="mono">{bond.ports.join(' + ')}</td>
							<td>{mode(bond.mode)}</td>
							<td>{users.join(', ') || t('netx.unused')}</td>
							<td class="actions"
								><button class="btn" {disabled} onclick={() => oneditbond(bond)}
									>{t('netx.editShort')}</button
								><button
									class="btn"
									disabled={disabled || users.length > 0}
									title={users.length
										? t('netx.bondInUse', { names: users.join(', ') })
										: undefined}
									onclick={() => onremovebond(bond)}>{t('netx.remove')}</button
								></td
							>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{:else}
		<EmptyState inline title={t('netx.noBonds')} />
	{/if}
	<div class="row-actions">
		<button class="btn" disabled={disabled || ports.length < 2} onclick={onaddbond}
			>{t('netx.addBond')}</button
		>
	</div>
	<p class="muted">{t('netx.switchHint')}</p>
</section>

<style>
	.speed {
		margin-left: 8px;
		font-size: 0.8125rem;
	}
	td.actions {
		display: flex;
		gap: 6px;
		justify-content: flex-end;
	}
</style>
