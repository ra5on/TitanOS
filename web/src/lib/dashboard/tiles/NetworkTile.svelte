<script lang="ts">
	import { formatMegabytesPerSecond } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { system } from '#lib/system.svelte.ts';
	import type { TileOptions } from '../options.ts';
	let { options }: { options: TileOptions } = $props();
	const network = $derived(
		options.interface
			? system.info?.networks?.find((n) => n.interface === options.interface)
			: system.info?.network,
	);
</script>

{#if network}
	{#if options.rates}
		<div class="rates">
			<div>
				<span class="label">{t('network.received')}</span><span
					class="mono value"
					data-edit-part="value"
					>{Number.isFinite(network.rxBytesPerSecond)
						? formatMegabytesPerSecond(network.rxBytesPerSecond)
						: '–'}</span
				><span class="label">{t('network.unit')}</span>
			</div>
			<div>
				<span class="label">{t('network.sent')}</span><span
					class="mono value"
					data-edit-part="value"
					>{Number.isFinite(network.txBytesPerSecond)
						? formatMegabytesPerSecond(network.txBytesPerSecond)
						: '–'}</span
				><span class="label">{t('network.unit')}</span>
			</div>
		</div>
	{/if}
	<dl class:compact={!options.details && !options.link}>
		<div>
			<dt>{t('dashboard.interface')}</dt>
			<dd class="mono">{network.interface || '–'}</dd>
		</div>
		{#if options.ipv4}
			<div>
				<dt>{t('network.ipv4')}</dt>
				<dd class="mono">
					{(network.ipv4Addresses ?? [network.ipv4]).filter(Boolean).join('\n') || '–'}
				</dd>
			</div>
			{#if options.details}<div>
					<dt>{t('network.gateway')}</dt>
					<dd class="mono">{network.gateway || '–'}</dd>
				</div>{/if}
		{/if}
		{#if options.ipv6}
			<div>
				<dt>{t('network.ipv6')}</dt>
				<dd class="mono">{network.ipv6Addresses?.join('\n') || '–'}</dd>
			</div>
			{#if options.details}<div>
					<dt>{t('network.gateway6')}</dt>
					<dd class="mono">{network.gateway6 || '–'}</dd>
				</div>{/if}
		{/if}
		{#if options.link}
			<div>
				<dt>{t('network.speed')}</dt>
				<dd class="mono">{network.speedMbps > 0 ? `${network.speedMbps} Mbit/s` : '–'}</dd>
			</div>
			<div>
				<dt>{t('network.mac')}</dt>
				<dd class="mono">{network.mac || '–'}</dd>
			</div>
			<div>
				<dt>{t('network.mtu')}</dt>
				<dd class="mono">{network.mtu || '–'}</dd>
			</div>
		{/if}
	</dl>
{:else if system.info}<p class="muted">
		{t(options.interface ? 'dashboard.interfaceMissing' : 'network.none')}
	</p>{/if}

<style>
	dl.compact {
		gap: 5px;
	}
	dl.compact > div:first-child {
		display: none;
	}
	dl.compact dt {
		font-size: 11px;
	}
	.rates {
		display: grid;
		grid-template-columns: repeat(2, minmax(0, 1fr));
		gap: 8px;
	}
	.rates div {
		display: flex;
		flex-direction: row;
		flex-wrap: wrap;
		align-items: baseline;
		gap: 4px;
		padding: 0;
		background: none;
		border-radius: var(--radius-ctl);
	}
	.rates div > .label:first-child {
		flex-basis: 100%;
	}
	.label {
		font-size: 0.75rem;
		color: var(--muted);
	}
	.value {
		font-size: 1.2rem;
		font-weight: 500;
		overflow-wrap: anywhere;
	}
	dl {
		margin: 0;
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: 0.8125rem;
	}
	dl div {
		display: flex;
		justify-content: space-between;
		gap: 12px;
	}
	dt {
		color: var(--muted);
		flex: none;
	}
	dd {
		margin: 0;
		white-space: pre-line;
		overflow-wrap: anywhere;
		text-align: right;
		min-width: 0;
	}
</style>
