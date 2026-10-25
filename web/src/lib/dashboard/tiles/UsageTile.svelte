<script lang="ts">
	import { formatBytes } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { system, systemHistory } from '#lib/system.svelte.ts';
	import { memoryParts } from '#lib/memory.ts';
	import type { TileOptions } from '../options.ts';
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
	const temperature = $derived(
		info?.temperatures.find(
			(s) =>
				(s.chip === 'coretemp' && s.label === 'Package id 0') ||
				(s.chip === 'k10temp' && s.label === 'Tctl'),
		),
	);
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
					{options}
					history={systemHistory[kind]}
				/>
				{#if kind === 'memory' && ram && system.reachable}
					<div class="memory-occupancy" aria-label={t('system.memoryIncludesCache')}>
						<div class="memory-track" aria-hidden="true">
							<span
								class="other"
								style:width="{ram.total ? ((ram.other ?? ram.used) / ram.total) * 100 : 0}%"
							></span>
							<span
								class="cache"
								style:width="{ram.total ? ((ram.cache ?? 0) / ram.total) * 100 : 0}%"
							></span>
						</div>
						<p>{t('system.memoryIncludesCache')}: <b>{formatBytes(ram.used)}</b></p>
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
					<span>{t('dashboard.memoryCache')}</span><b
						>{!system.reachable || ram?.cache == null ? '–' : formatBytes(ram.cache)}</b
					>
				</div>
				<div>
					<span>{t('memoryPolicy.other')}</span><b
						>{!system.reachable || ram?.other == null ? '–' : formatBytes(ram.other)}</b
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
		margin: 5px 0 0;
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
	.memory-track .other {
		background: var(--accent);
	}
	.memory-track .cache {
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
