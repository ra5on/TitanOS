<script lang="ts">
	import { formatBytes } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { system, systemHistory } from '#lib/system.svelte.ts';
	import { memoryParts } from '#lib/memory.ts';
	import type { TileOptions } from '../options.ts';
	import { cpuTemperature } from '../temperatures.ts';
	import Metric from './Metric.svelte';
	import History from './History.svelte';
	let {
		kind,
		options,
		ondetails,
	}: { kind: 'cpu' | 'memory'; options: TileOptions; ondetails: () => void } = $props();
	const info = $derived(system.info);
	const ram = $derived(info ? memoryParts(info.memory) : null);
	const percent = $derived(
		info && system.reachable
			? kind === 'cpu'
				? info.cpu.usagePercent
				: info.memory.totalBytes > 0
					? (info.memory.availableBytes / info.memory.totalBytes) * 100
					: null
			: null,
	);
	const temperature = $derived(cpuTemperature(info?.temperatures, options.sensor));
</script>

{#if info}
	{#if kind === 'cpu' && options.details}<p class="cpu-model">
			{info.cpu.model} · {t('system.threads', { count: info.cpu.threads })}
		</p>{/if}
	{#each options.parts as part}
		{#if part === 'value'}<div data-part="value">
				<Metric
					value={percent}
					label={kind === 'cpu'
						? t('dashboard.totalUsage')
						: t('dashboard.memoryAvailableTotal', {
								available: formatBytes(info.memory.availableBytes),
								total: formatBytes(info.memory.totalBytes),
							})}
					compact={kind === 'cpu'}
					showCompactLabel
					bar={kind !== 'memory'}
					{options}
					history={systemHistory[kind]}
				/>
				{#if kind === 'memory' && ram && system.reachable}
					<div
						class="memory-occupancy"
						role="img"
						aria-label={t('dashboard.memoryBar', {
							occupied: formatBytes(ram.occupied),
							write: formatBytes(ram.write),
							free: formatBytes(ram.free),
						})}
					>
						<div class="memory-track" aria-hidden="true">
							<span
								class="metric-level occupied"
								style:width="{ram.total ? (ram.occupied / ram.total) * 100 : 0}%"
							></span>
							<span
								class="metric-level write"
								style:width="{ram.total ? (ram.write / ram.total) * 100 : 0}%"
							></span>
						</div>
						<p aria-hidden="true">
							<span
								><i class="occupied"></i>{t('dashboard.memoryOccupied')}
								<b>{formatBytes(ram.occupied)}</b></span
							>
							<span
								><i class="write"></i>{t('dashboard.memoryWrite')}
								<b>{formatBytes(ram.write)}</b></span
							>
							<span><i></i>{t('dashboard.memoryFree')} <b>{formatBytes(ram.free)}</b></span>
						</p>
					</div>
					{#if info.memory.lowAvailable}<p class="notice-warn" role="status">
							{t('memoryPolicy.lowAvailable')}
						</p>{/if}
				{/if}
			</div>
		{:else if part === 'history' && options.history && options.display !== 'history'}<div
				data-part="history"
			>
				<History values={systemHistory[kind]} />
			</div>
		{:else if part === 'temperature' && kind === 'cpu' && options.temperature && !options.showSummary}<div
				class="detail"
				data-part="temperature"
				data-edit-part="labels"
			>
				<span>{t('accelerator.temperature')}</span><b
					>{temperature?.celsius == null ? '–' : `${temperature.celsius.toFixed(1)} °C`}</b
				>
			</div>
		{:else if part === 'details' && options.details && kind === 'memory'}<div
				class="detail memory-details"
				data-part="details"
				data-edit-part="labels"
			>
				<div>
					<span>{t('dashboard.memoryAvailable')}</span><b
						>{system.reachable ? formatBytes(info.memory.availableBytes) : '–'}</b
					>
				</div>
				<div>
					<span>{t('dashboard.memoryWrite')}</span><b
						>{!system.reachable || !ram ? '–' : formatBytes(ram.write)}</b
					>
				</div>
				<div>
					<span>{t('dashboard.memoryOccupied')}</span><b
						>{!system.reachable || !ram ? '–' : formatBytes(ram.occupied)}</b
					>
				</div>
				<div>
					<span>{t('memoryPolicy.pressure')}</span><b
						>{!system.reachable || !info.memory.pressure
							? t('common.unavailable')
							: `${info.memory.pressure.someAvg10.toFixed(1)} % / ${info.memory.pressure.fullAvg10.toFixed(1)} %`}</b
					>
				</div>
			</div>{/if}
		{#if part === 'details' && kind === 'cpu' && options.details && info.cpu.threadUsage?.length}<div
				class="core-grid"
			>
				{#each info.cpu.threadUsage as core (core.id)}<div class="core">
						<span>{core.id.toUpperCase().replace(/^CPU(?=\d)/, 'CPU ')}</span>
						<div class="track">
							<span
								class="metric-level"
								class:unknown={core.usagePercent === null || !system.reachable}
								style:width="{core.usagePercent ?? 0}%"
							></span>
						</div>
						<span class="mono"
							>{core.usagePercent === null || !system.reachable
								? '–'
								: Math.round(core.usagePercent) + '%'}</span
						>
					</div>{/each}
			</div>{/if}
	{/each}
	{#if kind === 'cpu' && info.cpu.threadUsage?.length}<button
			class="link thread-toggle"
			onclick={ondetails}
			>{t(options.details ? 'dashboard.hideThreads' : 'dashboard.showThreads')}</button
		>{/if}
{/if}

<style>
	.memory-occupancy {
		margin-top: 10px;
		font-size: var(--tile-label-size, 12px);
		color: var(--tile-label-color, var(--muted));
	}
	.memory-occupancy p {
		display: flex;
		flex-wrap: wrap;
		gap: 2px 12px;
		margin: 5px 0 0;
	}
	.memory-occupancy p span {
		display: inline-flex;
		align-items: center;
		gap: 5px;
	}
	.memory-occupancy i {
		width: 8px;
		height: 8px;
		border-radius: var(--radius-small);
		background: var(--track);
	}
	.memory-occupancy b {
		color: var(--text);
		font-weight: 500;
	}
	.memory-track {
		display: flex;
		height: 6px;
		background: var(--track);
		border-radius: var(--radius-bar);
		overflow: hidden;
	}
	.memory-track span {
		height: 100%;
	}
	.memory-occupancy .occupied {
		background: var(--accent);
	}
	.memory-occupancy .write {
		background: var(--muted);
	}
	.cpu-model {
		margin: 0;
		color: var(--tile-label-color);
		font-size: var(--tile-label-size);
		overflow-wrap: anywhere;
	}
	.core-grid {
		display: grid;
		grid-template-columns: repeat(2, minmax(0, 1fr));
		gap: 6px 20px;
	}
	.core {
		display: grid;
		grid-template-columns: minmax(0, max-content) minmax(12px, 1fr) max-content;
		align-items: center;
		gap: 7px;
		font-size: var(--tile-label-size, 12px);
		min-width: 0;
	}
	.core > span:first-child {
		overflow-wrap: anywhere;
	}
	.memory-details > div {
		display: flex;
		flex-direction: column;
		gap: 4px;
		flex: 1;
		min-width: 0;
	}
	.thread-toggle {
		border: 0;
		background: none;
		padding: 0;
		color: var(--muted);
		font: inherit;
		cursor: pointer;
		align-self: flex-start;
		font-size: var(--tile-label-size);
	}
	.core .mono {
		text-align: right;
		white-space: nowrap;
	}
	.track {
		height: 6px;
		background: var(--track);
		border-radius: var(--radius-bar);
		overflow: hidden;
	}
	.track span {
		display: block;
		height: 100%;
		background: var(--accent);
	}
	@container (max-width: 500px) {
		.core-grid {
			grid-template-columns: minmax(0, 1fr);
		}
	}

	.detail {
		display: flex;
		justify-content: space-between;
		flex-wrap: wrap;
		gap: 5px 12px;
		font-size: var(--tile-label-size, 12px);
		color: var(--tile-label-color, var(--muted));
		overflow-wrap: anywhere;
	}
	.detail b {
		font-weight: 500;
		color: var(--text);
	}
</style>
