<script lang="ts">
	import { formatMegabytesPerSecond } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { system } from '#lib/system.svelte.ts';

	const network = $derived(system.info?.network);
</script>

{#if network}
	<div class="rates">
		<div><span class="label">{t('network.received')}</span><span class="mono value">{formatMegabytesPerSecond(network.rxBytesPerSecond)}</span><span class="label">{t('network.unit')}</span></div>
		<div><span class="label">{t('network.sent')}</span><span class="mono value">{formatMegabytesPerSecond(network.txBytesPerSecond)}</span><span class="label">{t('network.unit')}</span></div>
	</div>
	<dl>
		<div><dt>{t('network.ipv4')}</dt><dd class="mono">{network.ipv4 || '–'}</dd></div>
		<div><dt>{t('network.gateway')}</dt><dd class="mono">{network.gateway || '–'}</dd></div>
	</dl>
{:else if system.info}
	<p class="muted none">{t('network.none')}</p>
{/if}

<style>
	.rates {
		display: grid;
		grid-template-columns: repeat(2, minmax(0, 1fr));
		gap: 12px;
	}
	.rates div {
		display: flex;
		flex-direction: column;
		gap: 4px;
		padding: 14px;
		background: var(--chip);
		border-radius: var(--radius-ctl);
	}
	.label {
		font-size: 0.75rem;
		color: var(--muted);
	}
	.value {
		font-size: 1.375rem;
		font-weight: 500;
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
	}
	dd {
		margin: 0;
	}
	.none {
		font-size: 0.875rem;
	}
</style>
