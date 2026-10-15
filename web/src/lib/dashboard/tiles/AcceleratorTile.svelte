<script lang="ts">
	import { formatBytes, formatBytesOf, formatPercent } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { system } from '#lib/system.svelte.ts';

	import type { TileOptions } from '../options.ts';
 import { tileOptions } from '../options.ts';
 let { kind, options = tileOptions(null, kind) }: { kind: 'igpu' | 'npu' | 'nvidia'; options?: TileOptions } = $props();
	const devices = $derived((system.info?.accelerators ?? []).filter((device) =>
		kind === 'npu' ? device.kind === 'npu' : device.kind !== 'npu' && (kind === 'nvidia' ? device.vendor === 'NVIDIA' : device.vendor !== 'NVIDIA')));
	function historyPath(history: (number | null)[]): string {
		let connected = false;
		return history.map((value, index) => {
			if (value === null) { connected = false; return ''; }
			const command = connected ? 'L' : 'M'; connected = true;
			return `${command}${(index * 240) / 35},${60 - value * 0.56}`;
		}).join(' ');
	}
	function value(value: number | null, unit: string): string {
		return value === null ? '–' : `${Math.round(value)} ${unit}`;
	}
</script>

{#if !system.info}
	<p class="empty muted">{t('accelerator.loading')}</p>
{:else if devices.length === 0}
	<div class="empty">
		<svg viewBox="0 0 64 64" width="52" height="52" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><rect x="14" y="14" width="36" height="36" rx="4" /><path d="M23 6v8m9-8v8m9-8v8M23 50v8m9-8v8m9-8v8M6 23h8m-8 9h8m-8 9h8m36-18h8m-8 9h8m-8 9h8" /><rect x="23" y="23" width="18" height="18" rx="2" /></svg>
		<p>{t('accelerator.none')}</p>
		<span class="muted">{t(kind === 'npu' ? 'accelerator.npuHint' : kind === 'nvidia' ? 'accelerator.nvidiaHint' : 'accelerator.igpuHint')}</span>
	</div>
{:else}
	{#each devices as device (device.address)}
		<section class="accelerator" aria-label={device.name}>
			<div class="device-heading"><strong>{device.name}</strong><span class="muted mono address">{device.address}</span></div>
			<div class="status"><span class:warning={device.state === 'driver_missing'} class="chip">{t(`accelerator.state.${device.state}`)}</span><span class="muted mono">{device.driver || '–'}{device.pState ? ` · ${device.pState}` : ''}</span></div>
			<div class="graphics">
				<div class="gauge" aria-label={t('accelerator.usage')}>
					<svg viewBox="0 0 100 100" aria-hidden="true"><circle class="track" cx="50" cy="50" r="43" /><circle class="load" cx="50" cy="50" r="43" pathLength="100" stroke-dasharray="{device.usagePercent ?? 0} 100" /></svg>
					<span class="mono">{device.usagePercent === null ? '–' : formatPercent(device.usagePercent)}</span>
				</div>
				{#if options.history}<div class="history"><span class="muted">{t('accelerator.usage')}</span><svg viewBox="0 0 240 64" preserveAspectRatio="none" role="img" aria-label={t('accelerator.history')}><path class="guide" d="M0 4H240M0 32H240M0 60H240" /><path class="line" d={historyPath(device.history)} /></svg><span class="muted caption">{t('accelerator.history')}</span></div>{/if}
			</div>
            {#if options.details}
			<dl>
				<div><dt>{t('accelerator.temperature')}</dt><dd class="mono">{value(device.temperatureC, '°C')}</dd></div>
				<div><dt>{t('accelerator.power')}</dt><dd class="mono">{value(device.powerWatts, 'W')}</dd></div>
				<div><dt>{t('accelerator.memory')}</dt><dd class="mono">{device.memoryUsedBytes === null ? '–' : device.memoryTotalBytes === null ? formatBytes(device.memoryUsedBytes) : formatBytesOf(device.memoryUsedBytes, device.memoryTotalBytes)}</dd></div>
				<div><dt>{t('accelerator.clock')}</dt><dd class="mono">{value(device.frequencyMHz, 'MHz')}</dd></div>
			</dl>
			{#each device.engines as engine (engine.name)}
				<div class="engine"><div><span>{engine.name === 'encode' ? t('accelerator.encode') : engine.name === 'decode' ? t('accelerator.decode') : engine.name}</span><span class="mono">{formatPercent(engine.usagePercent)}</span></div><div class="bar"><span style:width="{engine.usagePercent}%"></span></div></div>
			{/each}
            {/if}
			{#if device.state === 'driver_missing' && kind === 'nvidia'}<a href="/plugins">{t('accelerator.installNvidia')}</a>{/if}
			{#if options.details && [device.usagePercent, device.temperatureC, device.powerWatts, device.memoryUsedBytes, device.frequencyMHz].some((metric) => metric === null)}<p class="muted caption">{t('accelerator.unavailable')}</p>{/if}
		</section>
	{/each}
{/if}

<style>
	.accelerator { display: flex; flex-direction: column; gap: 10px; min-width: 0; }
	.accelerator + .accelerator { border-top: 1px solid var(--panel-border); padding-top: 16px; }
	.device-heading { display: flex; flex-direction: column; gap: 4px; overflow-wrap: anywhere; }
	.device-heading strong { font-weight: 500; }
	.address, .status, .caption { font-size: 0.75rem; }
	.status { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
	.warning { color: var(--warn-text); background: var(--warn-bg); border-color: var(--warn-border); }
	.graphics { display: flex; align-items: center; gap: 18px; }
	.gauge { width: 90px; height: 90px; flex: none; position: relative; display: grid; place-items: center; }
	.gauge svg { position: absolute; inset: 0; width: 100%; height: 100%; transform: rotate(-90deg); fill: none; stroke-width: 5; }
	.track { stroke: var(--track); }
	.load { stroke: var(--accent); stroke-linecap: round; }
	.gauge span { font-size: 1.2rem; }
	.history { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 4px; font-size: 0.75rem; }
	.history svg { width: 100%; height: 64px; overflow: visible; fill: none; }
	.guide { stroke: var(--panel-border); stroke-width: 1; }
	.line { stroke: var(--accent); stroke-width: 2; vector-effect: non-scaling-stroke; }
	dl { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; margin: 0; padding-top: 12px; border-top: 1px solid var(--panel-border); }
	dt { color: var(--muted); font-size: 0.75rem; }
	dd { margin: 3px 0 0; font-size: 0.875rem; overflow-wrap: anywhere; }
	.engine { display: flex; flex-direction: column; gap: 6px; font-size: 0.75rem; }
	.engine > div:first-child { display: flex; justify-content: space-between; gap: 8px; }
	.caption { margin: 0; }
	.empty { margin: auto; text-align: center; display: flex; flex-direction: column; align-items: center; gap: 10px; padding: 16px 0; font-size: 0.875rem; }
	.empty svg { color: var(--muted); }
	.empty p { margin: 0; }
	a { font-size: 0.875rem; }
</style>
