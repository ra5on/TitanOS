<script lang="ts">
	import { formatBytes } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { system, systemHistory } from '#lib/system.svelte.ts';
	import type { TileOptions } from '../options.ts';
	import Metric from './Metric.svelte';
	import History from './History.svelte';
	let { kind, options }: { kind: 'cpu' | 'memory'; options: TileOptions } = $props();
	const info = $derived(system.info);
	const percent = $derived(
		info
			? kind === 'cpu'
				? info.cpu.usagePercent
				: info.memory.totalBytes > 0
					? (info.memory.usedBytes / info.memory.totalBytes) * 100
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
	{#each options.parts as part}
		{#if part === 'value'}<div data-part="value">
				<Metric
					value={percent}
					label={t(kind === 'cpu' ? 'system.cpu' : 'system.memory')}
					{options}
					history={systemHistory[kind]}
				/>
			</div>
		{:else if part === 'history' && options.history && options.display !== 'history'}<div
				data-part="history"
			>
				<History values={systemHistory[kind]} />
			</div>
		{:else if part === 'temperature' && kind === 'cpu' && options.temperature}<div
				class="detail"
				data-part="temperature"
				data-edit-part="labels"
			>
				<span>{t('accelerator.temperature')}</span><b
					>{temperature?.celsius == null ? '–' : `${temperature.celsius.toFixed(1)} °C`}</b
				>
			</div>
		{:else if part === 'details' && options.details}<div
				class="detail"
				data-part="details"
				data-edit-part="labels"
			>
				{#if kind === 'cpu'}<span>{info.cpu.model}</span><b>{info.cpu.threads} Threads</b
					>{:else}<span
						>{t('system.memoryAvailable', { value: formatBytes(info.memory.availableBytes) })}</span
					><b>{formatBytes(info.memory.usedBytes)} / {formatBytes(info.memory.totalBytes)}</b><span
						>{t('system.memoryCache', { value: formatBytes(info.memory.cacheBytes ?? 0) })}</span
					>{/if}
			</div>{/if}
		{#if part === 'details' && kind === 'cpu' && options.details && info.cpu.threadUsage?.length}<div
				class="core-grid"
			>
				{#each info.cpu.threadUsage as core (core.id)}<div class="core">
						<span>{core.id.toUpperCase()}</span>
						<div class="track"><span style:width="{core.usagePercent ?? 0}%"></span></div>
						<span class="mono"
							>{core.usagePercent === null ? '–' : Math.round(core.usagePercent) + '%'}</span
						>
					</div>{/each}
			</div>{/if}
	{/each}
{/if}

<style>
	.core-grid {
		display: grid;
		grid-template-columns: repeat(2, minmax(0, 1fr));
		gap: 6px 20px;
	}
	.core {
		display: grid;
		grid-template-columns: 50px minmax(0, 1fr) 38px;
		align-items: center;
		gap: 7px;
		font-size: var(--tile-label-size, 12px);
		min-width: 0;
	}
	.core .mono {
		text-align: right;
		white-space: nowrap;
	}
	.track {
		height: 6px;
		background: var(--track);
	}
	.track span {
		display: block;
		height: 100%;
		background: var(--accent);
	}
	@media (max-width: 450px) {
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
