<script lang="ts">
	import { formatBytes, formatPercent } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { system } from '#lib/system.svelte.ts';
	import type { Accelerator } from '#lib/api.ts';
	import ContextMenu from '#lib/components/ContextMenu.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	let menu = $state<{ device: Accelerator; x: number; y: number } | null>(null),
		selected = $state<Accelerator | null>(null),
		view = $state<'details' | 'history'>('details');
	import { tileOptions, type TileOptions } from '../options.ts';
	import Metric from './Metric.svelte';
	import History from './History.svelte';
	let {
		kind,
		options = tileOptions(null, kind),
	}: { kind: 'igpu' | 'npu' | 'nvidia' | 'accelerators'; options?: TileOptions } = $props();
	const devices = $derived(
		(system.info?.accelerators ?? []).filter((d) =>
			options.device
				? d.address === options.device
				: kind === 'accelerators'
					? true
					: kind === 'npu'
						? d.kind === 'npu'
						: d.kind !== 'npu' &&
							(kind === 'nvidia' ? d.vendor === 'NVIDIA' : d.vendor !== 'NVIDIA'),
		),
	);
	const value = (v: number | null, unit: string) => (v === null ? '–' : `${Math.round(v)} ${unit}`);
</script>

{#each devices as device (device.address)}
	<section
		class="accelerator"
		class:compact={options.display === 'auto' || options.display === 'bar'}
		aria-label={device.name}
	>
		<div class="device-label" data-edit-part="labels">
			<button
				class="device-name"
				data-edit-part="labels"
				aria-label={t('dashboard.menu', { name: device.name })}
				aria-haspopup="menu"
				onclick={(e) => {
					const r = e.currentTarget.getBoundingClientRect();
					menu = { device, x: r.left, y: r.bottom };
				}}>{device.name}</button
			><span class="device-state" class:warn={device.state === 'driver_missing'}
				>{t(`accelerator.state.${device.state}`)}{!options.details &&
				!options.hiddenFields.includes('temperature') &&
				device.temperatureC !== null
					? ' · ' + Math.round(device.temperatureC) + ' °C'
					: ''}</span
			>
		</div>
		{#each options.parts as part}
			{#if part === 'value'}<div data-part="value">
					<Metric
						value={system.reachable ? device.usagePercent : null}
						label={t('accelerator.usage')}
						options={{ ...options, display: options.display === 'auto' ? 'bar' : options.display }}
						history={device.history}
						compact={options.display === 'auto' || options.display === 'bar'}
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
								<span class="metric-level" style:width="{Math.min(100, engine.usagePercent)}%"
								></span>
							</div>
						</div>{/each}
				</div>{/if}
		{/each}
	</section>
{:else}<p class="muted">
		{t(options.device ? 'dashboard.deviceMissing' : 'accelerator.none')}
	</p>{/each}

{#if menu}<ContextMenu
		label={t('dashboard.menu', { name: menu.device.name })}
		x={menu.x}
		y={menu.y}
		onclose={() => (menu = null)}
		><div class="menu-title">{menu.device.name}</div>
		<button
			role="menuitem"
			onclick={() => {
				selected = menu!.device;
				view = 'details';
				menu = null;
			}}>{t('dashboard.showDetails')}</button
		><button
			role="menuitem"
			onclick={() => {
				selected = menu!.device;
				view = 'history';
				menu = null;
			}}>{t('dashboard.history')}</button
		><a role="menuitem" href="/settings/hardware">{t('dashboard.editor.device')}</a></ContextMenu
	>{/if}
{#if selected}<Dialog title={selected.name} onclose={() => (selected = null)}
		>{#if view === 'history'}<History values={selected.history} />{:else}<dl>
				<div>
					<dt>PCI</dt>
					<dd>{selected.address}</dd>
				</div>
				<div>
					<dt>{t('dashboard.editor.driver')}</dt>
					<dd>{selected.driver || '–'}</dd>
				</div>
				<div>
					<dt>{t('accelerator.temperature')}</dt>
					<dd>{value(selected.temperatureC, '°C')}</dd>
				</div>
				<div>
					<dt>{t('accelerator.power')}</dt>
					<dd>{value(selected.powerWatts, 'W')}</dd>
				</div>
				<div>
					<dt>{t('accelerator.memory')}</dt>
					<dd>{selected.memoryUsedBytes === null ? '–' : formatBytes(selected.memoryUsedBytes)}</dd>
				</div>
				<div>
					<dt>{t('accelerator.clock')}</dt>
					<dd>{value(selected.frequencyMHz, 'MHz')}</dd>
				</div>
			</dl>{/if}
		<div class="dialog-actions">
			<button class="btn btn-primary" onclick={() => (selected = null)}>{t('common.done')}</button>
		</div></Dialog
	>{/if}

<style>
	.accelerator {
		display: flex;
		flex-direction: column;
		gap: var(--tile-gap, 10px);
		min-width: 0;
	}
	.accelerator.compact {
		display: grid;
		grid-template-columns: minmax(130px, 1fr) minmax(90px, 1.2fr);
		align-items: center;
		gap: 10px;
	}
	.compact .device-label {
		display: block;
	}
	.compact .device-state {
		display: block;
		margin-top: 2px;
	}
	.compact [data-part='details'],
	.compact [data-part='engines'],
	.compact [data-part='history'] {
		grid-column: 1/-1;
	}
	.device-name {
		border: 0;
		background: none;
		padding: 0;
		text-align: left;
		cursor: pointer;
		font: var(--tile-value-weight) var(--tile-label-size, var(--text-sm)) var(--tile-value-font);
		color: var(--tile-value-color, var(--text));
		overflow-wrap: anywhere;
		text-decoration: none;
	}
	.accelerator + .accelerator {
		border-top: 1px solid var(--border);
		padding-top: 10px;
	}
	.device-label {
		display: flex;
		justify-content: space-between;
		gap: 6px;
		flex-wrap: wrap;
		color: var(--tile-label-color, var(--muted));
		font-size: var(--tile-label-size, var(--text-sm));
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
		font-size: var(--tile-label-size, var(--text-sm));
	}
	dd {
		margin: 4px 0 0;
		font-size: var(--tile-label-size, var(--text-sm));
		font-family: var(--tile-value-font);
		overflow-wrap: anywhere;
	}
	.engine {
		display: flex;
		justify-content: space-between;
		flex-wrap: wrap;
		gap: 5px;
		font-size: var(--tile-label-size, var(--text-sm));
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
