<script lang="ts">
	import { formatBytes, formatPercent } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { system } from '#lib/system.svelte.ts';
	import { tileOptions, type TileOptions } from '../options.ts';
	import Metric from './Metric.svelte';
	import History from './History.svelte';
	let {
		kind,
		options = tileOptions(null, kind),
	}: { kind: 'igpu' | 'npu' | 'nvidia'; options?: TileOptions } = $props();
	const devices = $derived(
		(system.info?.accelerators ?? []).filter((d) =>
			options.device
				? d.address === options.device
				: kind === 'npu'
					? d.kind === 'npu'
					: d.kind !== 'npu' && (kind === 'nvidia' ? d.vendor === 'NVIDIA' : d.vendor !== 'NVIDIA'),
		),
	);
	const value = (v: number | null, unit: string) => (v === null ? '–' : `${Math.round(v)} ${unit}`);
</script>

{#each devices as device (device.address)}
	<section class="accelerator" aria-label={device.name}>
		<div class="device-label" data-edit-part="labels">
			<span>{device.name}</span><span
				class="device-state"
				class:warn={device.state === 'driver_missing'}
				>{t(`accelerator.state.${device.state}`)}</span
			>
		</div>
		{#each options.parts as part}
			{#if part === 'value'}<div data-part="value">
					<Metric
						value={device.usagePercent}
						label={t('accelerator.usage')}
						options={{ ...options, display: options.display === 'auto' ? 'ring' : options.display }}
						history={device.history}
					/>
				</div>
			{:else if part === 'history' && options.history && options.display !== 'history'}<div
					data-part="history"
				>
					<History values={device.history} />
				</div>
			{:else if part === 'details' && options.details}<dl
					data-part="details"
					data-edit-part="labels"
				>
					{#if !options.hiddenFields.includes('temperature')}<div>
							<dt>{t('accelerator.temperature')}</dt>
							<dd>{value(device.temperatureC, '°C')}</dd>
						</div>{/if}
					{#if !options.hiddenFields.includes('power')}<div>
							<dt>{t('accelerator.power')}</dt>
							<dd>{value(device.powerWatts, 'W')}</dd>
						</div>{/if}
					{#if !options.hiddenFields.includes('memory')}<div>
							<dt>{t('accelerator.memory')}</dt>
							<dd>
								{device.memoryUsedBytes === null
									? '–'
									: `${formatBytes(device.memoryUsedBytes)}${device.memoryTotalBytes === null ? '' : ' / ' + formatBytes(device.memoryTotalBytes)}`}
							</dd>
						</div>{/if}
					{#if !options.hiddenFields.includes('clock')}<div>
							<dt>{t('accelerator.clock')}</dt>
							<dd>{value(device.frequencyMHz, 'MHz')}</dd>
						</div>{/if}
					{#if !options.hiddenFields.includes('driver')}<div>
							<dt>{t('dashboard.editor.driver')}</dt>
							<dd>{device.driver || '–'}</dd>
						</div>{/if}
				</dl>
			{:else if part === 'engines' && options.details && !options.hiddenFields.includes('engines')}<div
					class="engines"
					data-part="engines"
					data-edit-part="labels"
				>
					{#each device.engines as engine}<div class="engine">
							<span
								>{engine.name === 'encode'
									? t('accelerator.encode')
									: engine.name === 'decode'
										? t('accelerator.decode')
										: engine.name}</span
							><b>{formatPercent(engine.usagePercent)}</b>
							<div class="bar">
								<span style:width="{Math.min(100, engine.usagePercent)}%"></span>
							</div>
						</div>{/each}
				</div>{/if}
		{/each}
	</section>
{:else}<p class="muted">
		{t(options.device ? 'dashboard.deviceMissing' : 'accelerator.none')}
	</p>{/each}

<style>
	.accelerator {
		display: flex;
		flex-direction: column;
		gap: var(--tile-gap, 10px);
		min-width: 0;
	}
	.accelerator + .accelerator {
		border-top: 1px solid var(--border);
		padding-top: 15px;
	}
	.device-label {
		display: flex;
		justify-content: space-between;
		gap: 6px;
		flex-wrap: wrap;
		color: var(--tile-label-color, var(--muted));
		font-size: var(--tile-label-size, 12px);
	}
	.device-state {
		font-size: 0.85em;
		color: var(--ok-text);
	}
	.device-state.warn {
		color: var(--warn-text);
	}
	dl {
		margin: 0;
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 12px;
	}
	dt {
		color: var(--tile-label-color, var(--muted));
		font-size: var(--tile-label-size, 12px);
	}
	dd {
		margin: 4px 0 0;
		font-size: var(--tile-label-size, 12px);
		font-family: var(--tile-value-font);
		overflow-wrap: anywhere;
	}
	.engine {
		display: flex;
		justify-content: space-between;
		flex-wrap: wrap;
		gap: 5px;
		font-size: var(--tile-label-size, 12px);
	}
	.engine b {
		font-weight: 400;
	}
	.engine .bar {
		flex-basis: 100%;
		height: 4px;
	}
	.engines {
		display: grid;
		gap: 10px;
	}
</style>
