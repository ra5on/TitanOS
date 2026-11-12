<script lang="ts">
	// PLAN 61: what one widget of the desktop shows. Reads only data the UI
	// keeps anyway (system, storage, Docker/VM lists); nothing wakes disks.
	import { api } from '../api.ts';
	import {
		formatBytes,
		formatBytesOf,
		formatDecimal,
		formatDuration,
		formatMegabytesPerSecond,
		formatNumber,
		formatPercent,
	} from '../format.ts';
	import { i18n, t } from '../i18n/index.svelte.ts';
	import { storage } from '../storage.svelte.ts';
	import { system } from '../system.svelte.ts';
	import DesktopIcon from './DesktopIcon.svelte';
	import LiveVideo from '../nvr/LiveVideo.svelte';
	import { loadWidgetCameras, widgetCameras } from '../nvr/widget.svelte.ts';
	import { isGpuWidget, type GpuValue, type WidgetKind } from './prefs.ts';
	import { dockerPrograms, sources } from './sources.svelte.ts';
	import { session } from '../session.svelte.ts';

	let {
		kind,
		hide = [],
		device = '',
		camera = '',
		large = false,
		onopen,
	}: {
		/** PLAN 79-7, camera widget: the id of the chosen camera. */
		camera?: string;
		/** PLAN 79-7, camera widget: large enough for live video instead of still pictures. */
		large?: boolean;
		kind: WidgetKind;
		/** PLAN 79-5, graphics widgets: the values switched off for this widget. */
		hide?: GpuValue[];
		/** PLAN 79-5, graphics widgets: the PCI address of the chosen device. */
		device?: string;
		onopen: (app: string, href?: string) => void;
	} = $props();

	const info = $derived(system.info);
	const memPct = $derived(
		info
			? (100 * (info.memory.totalBytes - info.memory.availableBytes)) / info.memory.totalBytes
			: 0,
	);
	const volumes = $derived(storage.status?.volumes ?? []);
	const problems = $derived(volumes.filter((v) => v.state !== 'ok').length);
	// The main connection first (the one with the default route), then the others for the totals.
	const net = $derived(
		info?.network
			? [
					info.network,
					...(info.networks ?? []).filter((n) => n.interface !== info.network?.interface),
				]
			: (info?.networks ?? []),
	);
	const rx = $derived(net.reduce((s, n) => s + (n.rxBytesPerSecond || 0), 0));
	const tx = $derived(net.reduce((s, n) => s + (n.txBytesPerSecond || 0), 0));
	const temps = $derived((info?.temperatures ?? []).filter((x) => x.celsius !== null).slice(0, 6));
	const host = $derived(session.data?.hostname ? location.hostname : location.hostname);

	// Network history for the line: the last two minutes, kept in this widget.
	let history = $state<{ rx: number; tx: number }[]>([]);
	$effect(() => {
		if (kind !== 'network') return;
		history = [...history.slice(-59), { rx, tx }];
	});
	const maxRate = $derived(Math.max(1, ...history.map((h) => Math.max(h.rx, h.tx))));
	const line = (key: 'rx' | 'tx') =>
		history
			.map((h, i) => `${i === 0 ? 'M' : 'L'}${(i / 59) * 300} ${60 - (h[key] / maxRate) * 56}`)
			.join(' ');

	let now = $state(new Date());
	$effect(() => {
		if (kind !== 'clock') return;
		const timer = setInterval(() => (now = new Date()), 1000 * 15);
		return () => clearInterval(timer);
	});

	type BackupJob = {
		job: { id: string; name?: string };
		state: { running: boolean; lastRun?: string; lastOk?: boolean; nextRun?: string };
	};
	let backups = $state<BackupJob[] | null>(null);
	$effect(() => {
		if (kind !== 'backups') return;
		api.get<{ jobs: BackupJob[] }>('/backups').then(
			(r) => (backups = r.jobs ?? []),
			() => (backups = []),
		);
	});

	type Entity = { id: string; name: string; state: string; domain: string; area?: string };
	let entities = $state<Entity[] | null>(null);
	$effect(() => {
		if (kind !== 'homeassistant') return;
		api.get<{ entities: Entity[] }>('/homeassistant/entities').then(
			(r) =>
				(entities = (r.entities ?? [])
					.filter((e) =>
						['light', 'switch', 'lock', 'sensor', 'climate', 'cover'].includes(e.domain),
					)
					.slice(0, 6)),
			() => (entities = []),
		);
	});

	const locale = $derived(i18n.locale === 'de' ? 'de-DE' : 'en-GB');

	// PLAN 79-7: one camera of the NVR. A small widget shows a still picture
	// every five seconds, a large one live video; a click opens the NVR.
	$effect(() => {
		if (kind === 'camera') void loadWidgetCameras();
	});
	const shownCamera = $derived(
		kind === 'camera'
			? (widgetCameras.list.find((c) => c.id === camera) ?? widgetCameras.list[0])
			: undefined,
	);

	// PLAN 79-5: iGPU, NPU and graphics card. The devices come with the system
	// data the desktop asks for anyway. A value the device does not report
	// (null) gets no tile – never a zero, never "not reported".
	const gpuDevices = $derived(
		isGpuWidget(kind) ? (info?.accelerators ?? []).filter((a) => a.kind === kind) : [],
	);
	const gpu = $derived(gpuDevices.find((a) => a.address === device) ?? gpuDevices[0]);
	const shows = (value: GpuValue) => !hide.includes(value);
	const gpuHistory = $derived((gpu?.history ?? []).slice(-60));
	const gpuLine = $derived.by(() => {
		// Gaps (null) break the line; they are not drawn as zero.
		const n = gpuHistory.length;
		if (n < 2 || !gpuHistory.some((v) => v !== null)) return '';
		let path = '';
		let pen = false;
		gpuHistory.forEach((v, i) => {
			if (v === null) {
				pen = false;
				return;
			}
			const x = (i / (n - 1)) * 300;
			const y = 58 - (Math.max(0, Math.min(100, v)) / 100) * 54;
			path += `${pen ? 'L' : 'M'}${x.toFixed(1)} ${y.toFixed(1)} `;
			pen = true;
		});
		return path.trim();
	});
	const gpuMemory = $derived(
		gpu && gpu.memoryUsedBytes !== null && gpu.memoryTotalBytes !== null && gpu.memoryTotalBytes > 0
			? { used: gpu.memoryUsedBytes, total: gpu.memoryTotalBytes }
			: null,
	);
	const GPU_TITLE = {
		igpu: 'desktop.widget.igpu',
		npu: 'desktop.widget.npu',
		gpu: 'desktop.widget.gpu',
	} as const;
	const programs = $derived(dockerPrograms(host));
</script>

{#if kind === 'status'}
	<div class="status">
		<span class="dot" class:warn={problems > 0} aria-hidden="true">{problems > 0 ? '!' : '✓'}</span>
		<div>
			<b class="big">{problems > 0 ? t('desktop.widget.attention') : t('desktop.widget.allGood')}</b
			>
			<div class="muted small">
				{problems > 0
					? t('desktop.widget.volumesAttention', { count: problems })
					: t('desktop.widget.allGoodText')}
			</div>
		</div>
	</div>
	<div class="two">
		<div class="in">
			<b>{info ? formatDuration(info.uptimeSeconds) : '–'}</b>
			<div class="muted small">{t('desktop.widget.uptime')}</div>
		</div>
		<div class="in">
			<b class="ellipsis">{(net[0]?.ipv4 || '–').replace(/\/\d+$/, '')}</b>
			<div class="muted small ellipsis">
				{net[0]
					? `${net[0].interface ?? ''}${net[0].speedMbps > 0 ? ' · ' + (net[0].speedMbps >= 1000 ? net[0].speedMbps / 1000 + ' Gbit/s' : net[0].speedMbps + ' Mbit/s') : ''}`
					: ''}
			</div>
		</div>
	</div>
{:else if kind === 'cpuram'}
	<div class="two">
		<div class="in">
			<div class="muted small">{t('desktop.widget.cpu')}</div>
			<b class="big">{info ? formatPercent(info.cpu.usagePercent) : '–'}</b>
			<div
				class="meter"
				role="meter"
				aria-valuenow={info?.cpu.usagePercent ?? 0}
				aria-valuemin="0"
				aria-valuemax="100"
				aria-label={t('desktop.widget.cpu')}
			>
				<i style:width="{info?.cpu.usagePercent ?? 0}%"></i>
			</div>
		</div>
		<div class="in">
			<div class="muted small">{t('desktop.widget.ram')}</div>
			<b class="big">{formatPercent(memPct)}</b>
			<span class="muted small"
				>{info ? formatBytes(info.memory.totalBytes - info.memory.availableBytes) : ''}</span
			>
			<div
				class="meter"
				role="meter"
				aria-valuenow={memPct}
				aria-valuemin="0"
				aria-valuemax="100"
				aria-label={t('desktop.widget.ram')}
			>
				<i style:width="{memPct}%"></i>
			</div>
		</div>
	</div>
{:else if kind === 'network'}
	<h4>{t('desktop.widget.network')}</h4>
	<svg class="chart" viewBox="0 0 300 62" preserveAspectRatio="none" aria-hidden="true">
		<path
			d={line('rx')}
			fill="none"
			stroke="var(--accent)"
			stroke-width="2"
			vector-effect="non-scaling-stroke"
		/>
		<path
			d={line('tx')}
			fill="none"
			stroke="#a78bfa"
			stroke-width="2"
			vector-effect="non-scaling-stroke"
		/>
	</svg>
	<div class="muted small">↓ {formatMegabytesPerSecond(rx)} · ↑ {formatMegabytesPerSecond(tx)}</div>
{:else if kind === 'volumes'}
	<h4>{t('desktop.widget.volumes')}</h4>
	{#each volumes as v (v.name)}
		<button type="button" class="in row-btn" onclick={() => onopen('app:storage')}>
			<div class="row">
				<span class="ellipsis">{v.name} · {v.level.toUpperCase().replace('RAID', 'RAID ')}</span
				><span class="muted">{formatBytesOf(v.usedBytes, v.sizeBytes)}</span>
			</div>
			<div class="meter">
				<i style:width="{(100 * v.usedBytes) / Math.max(1, v.sizeBytes)}%"></i>
			</div>
			{#if v.state !== 'ok'}<div class="warn-text small">
					{t('desktop.widget.volumeState', { state: v.state })}
				</div>{/if}
		</button>
	{:else}
		<p class="muted small">{t('desktop.widget.noVolumes')}</p>
	{/each}
{:else if kind === 'docker'}
	<h4>{t('desktop.widget.docker')}</h4>
	{#each programs.slice(0, 8) as p (p.key)}
		<button type="button" class="list-row" onclick={() => onopen(p.key)}>
			<DesktopIcon image={p.icon} name={p.label} gradient="blue" size={24} />
			<span class="ellipsis grow">{p.label}</span>
			<span class="state" class:on={p.running}
				>{p.running ? t('desktop.widget.running') : t('desktop.widget.stopped')}</span
			>
		</button>
	{:else}
		<p class="muted small">{t('desktop.widget.noApps')}</p>
	{/each}
{:else if kind === 'vms'}
	<h4>{t('desktop.widget.vms')}</h4>
	{#each sources.vms.slice(0, 8) as vm (vm.name)}
		<button
			type="button"
			class="list-row"
			onclick={() => onopen('app:vms', '/vms?vm=' + encodeURIComponent(vm.name))}
		>
			<DesktopIcon glyph="screen" gradient="violet" size={24} name={vm.displayName || vm.name} />
			<span class="ellipsis grow">{vm.displayName || vm.name}</span>
			<span class="state" class:on={vm.state === 'running'}
				>{vm.state === 'running' ? t('desktop.widget.running') : t('desktop.widget.stopped')}</span
			>
		</button>
	{:else}
		<p class="muted small">{t('desktop.widget.noVms')}</p>
	{/each}
{:else if kind === 'temperatures'}
	<h4>{t('desktop.widget.temperatures')}</h4>
	{#each temps as x (x.id)}
		<div class="row small">
			<span class="ellipsis">{x.label || x.device || x.chip}</span>
			<span
				class:warn-text={x.criticalC !== null &&
					x.celsius !== null &&
					x.celsius >= x.criticalC - 10}>{Math.round(x.celsius ?? 0)} °C</span
			>
		</div>
	{:else}
		<p class="muted small">{t('desktop.widget.noTemps')}</p>
	{/each}
{:else if kind === 'clock'}
	<div class="clock">{now.toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit' })}</div>
	<div class="muted">
		{now.toLocaleDateString(locale, { weekday: 'long', day: 'numeric', month: 'long' })}
	</div>
{:else if kind === 'backups'}
	<h4>{t('desktop.widget.backups')}</h4>
	{#if backups === null}
		<p class="muted small">{t('common.loading')}</p>
	{:else}
		{#each backups.slice(0, 4) as b (b.job.id)}
			<div class="row small">
				<span class="ellipsis">{b.job.name || b.job.id}</span>
				<span class="muted"
					>{b.state.running
						? t('desktop.widget.backupRunning')
						: b.state.lastRun
							? new Date(b.state.lastRun).toLocaleDateString(locale)
							: '–'}</span
				>
			</div>
		{:else}
			<p class="muted small">{t('desktop.widget.noBackups')}</p>
		{/each}
		<button type="button" class="btn small-btn" onclick={() => onopen('app:backup')}
			>{t('desktop.widget.openBackups')}</button
		>
	{/if}
{:else if kind === 'homeassistant'}
	<h4>{t('desktop.widget.homeassistant')}</h4>
	{#if entities === null}
		<p class="muted small">{t('common.loading')}</p>
	{:else}
		<div class="two">
			{#each entities as e (e.id)}
				<div class="in">
					<div class="muted small ellipsis">{e.name}</div>
					<b class="ellipsis">{e.state}</b>
				</div>
			{:else}
				<p class="muted small">{t('desktop.widget.noHa')}</p>
			{/each}
		</div>
	{/if}
{:else if isGpuWidget(kind)}
	<h4 class="gpu-title">{t(GPU_TITLE[kind])}</h4>
	{#if !gpu}
		<p class="muted small">{t('desktop.widget.gpuNone')}</p>
	{:else}
		<div class="muted small gpu-name">{gpu.name || gpu.vendor || gpu.address}</div>
		{#if shows('usage') && gpu.usagePercent !== null}
			<div class="gpu-usage" data-gpu-value="usage">
				<b class="huge">{formatPercent(gpu.usagePercent)}</b>
				<span class="muted">{t('desktop.widget.gpuUsage')}</span>
			</div>
		{/if}
		{#if shows('history') && gpuLine}
			<svg
				class="chart gpu-chart"
				data-gpu-value="history"
				viewBox="0 0 300 62"
				preserveAspectRatio="none"
				role="img"
				aria-label={t('desktop.widget.gpuHistory')}
			>
				<path
					d={gpuLine}
					fill="none"
					stroke="var(--accent)"
					stroke-width="2"
					stroke-linejoin="round"
					vector-effect="non-scaling-stroke"
				/>
			</svg>
		{/if}
		<div class="two gpu-tiles">
			{#if shows('temperature') && gpu.temperatureC !== null}
				<div class="in" data-gpu-value="temperature">
					<div class="muted small">{t('desktop.widget.gpuTemperature')}</div>
					<b>{formatNumber(Math.round(gpu.temperatureC))} °C</b>
				</div>
			{/if}
			{#if shows('memory') && gpuMemory}
				<div class="in" data-gpu-value="memory">
					<div class="muted small">{t('desktop.widget.gpuMemory')}</div>
					<b>{formatBytesOf(gpuMemory.used, gpuMemory.total)}</b>
					<div
						class="meter"
						role="meter"
						aria-valuenow={Math.round((100 * gpuMemory.used) / gpuMemory.total)}
						aria-valuemin="0"
						aria-valuemax="100"
						aria-label={t('desktop.widget.gpuMemory')}
					>
						<i style:width="{Math.min(100, (100 * gpuMemory.used) / gpuMemory.total)}%"></i>
					</div>
				</div>
			{/if}
			{#if shows('power') && gpu.powerWatts !== null}
				<div class="in" data-gpu-value="power">
					<div class="muted small">{t('desktop.widget.gpuPower')}</div>
					<b>{formatDecimal(gpu.powerWatts, gpu.powerWatts < 10 ? 1 : 0)} W</b>
				</div>
			{/if}
			{#if shows('clock') && gpu.frequencyMHz !== null}
				<div class="in" data-gpu-value="clock">
					<div class="muted small">{t('desktop.widget.gpuClock')}</div>
					<b>{formatNumber(Math.round(gpu.frequencyMHz))} MHz</b>
				</div>
			{/if}
		</div>
	{/if}
{:else if kind === 'camera'}
	{#if shownCamera}
		<button
			type="button"
			class="cam"
			aria-label={t('desktop.widget.cameraOpen', { name: shownCamera.name })}
			onclick={() => onopen('app:nvr', '/nvr?camera=' + encodeURIComponent(shownCamera.id))}
		>
			{#key shownCamera.id}
				<LiveVideo
					id={shownCamera.id}
					name={shownCamera.name}
					stillOnly={!large}
					stillMs={5000}
					height={large ? 720 : 360}
				/>
			{/key}
			<span class="cam-name">{shownCamera.name}</span>
		</button>
	{:else}
		<h4>{t('desktop.widget.camera')}</h4>
		<p class="muted">
			{widgetCameras.loaded ? t('desktop.widget.cameraNone') : t('common.loading')}
		</p>
	{/if}
{:else if kind === 'tasks'}
	<h4>{t('desktop.widget.tasks')}</h4>
	<button type="button" class="btn small-btn" onclick={() => onopen('app:monitor', '/tools/tasks')}
		>{t('desktop.widget.openTasks')}</button
	>
{/if}

<style>
	.cam {
		position: absolute;
		inset: 0;
		padding: 0;
		border: 0;
		border-radius: inherit;
		overflow: hidden;
		background: #0b0f14;
		cursor: pointer;
	}
	.cam:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.cam-name {
		position: absolute;
		left: 8px;
		bottom: 8px;
		max-width: calc(100% - 16px);
		padding: 2px 8px;
		border-radius: 6px;
		background: rgb(11 15 20 / 0.72);
		color: #fff;
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
		text-align: left;
	}
	h4 {
		margin: 0 0 var(--space-2-5);
		font-size: var(--text-md);
		font-weight: 600;
	}
	.muted {
		color: var(--muted);
	}
	.small {
		font-size: var(--text-sm);
	}
	.big {
		font-size: 20px;
		font-weight: 600;
	}
	.ellipsis {
		min-width: 0;
		overflow-wrap: break-word;
		hyphens: auto;
	}
	.grow {
		flex: 1;
	}
	.status {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		margin-bottom: var(--space-3);
	}
	.dot {
		width: 40px;
		height: 40px;
		flex: none;
		border-radius: 50%;
		display: grid;
		place-items: center;
		background: linear-gradient(160deg, #4fd67a, #1b9a4b);
		color: #fff;
		font-weight: 700;
		font-size: 18px;
	}
	.dot.warn {
		background: linear-gradient(160deg, #ffd84d, #f0a91a);
		color: #2b2100;
	}
	.two {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: var(--space-2);
	}
	.in {
		background: color-mix(in srgb, var(--bg) 45%, transparent);
		border-radius: 12px;
		padding: var(--space-2-5) var(--space-3);
		min-width: 0;
	}
	.row-btn {
		display: block;
		width: 100%;
		margin-bottom: var(--space-1-5);
		border: 0;
		color: inherit;
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.row {
		display: flex;
		justify-content: space-between;
		gap: var(--space-2);
		padding: 3px 0;
	}
	.meter {
		height: 5px;
		border-radius: 3px;
		background: var(--track);
		margin-top: var(--space-1-5);
		overflow: hidden;
	}
	.meter i {
		display: block;
		height: 100%;
		background: var(--accent);
	}
	.list-row {
		display: flex;
		align-items: center;
		gap: var(--space-2-5);
		width: 100%;
		padding: 5px var(--space-1);
		border: 0;
		border-radius: 8px;
		background: none;
		color: inherit;
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.list-row:hover,
	.row-btn:hover {
		background: color-mix(in srgb, var(--text) 7%, transparent);
	}
	.state {
		font-size: var(--text-sm);
		color: var(--muted);
	}
	.state.on {
		color: color-mix(in srgb, var(--ok-text, #9be3be) 75%, var(--text));
	}
	.state::before {
		content: '';
		display: inline-block;
		width: 8px;
		height: 8px;
		margin-right: var(--space-1-5);
		border-radius: 50%;
		background: var(--muted);
	}
	.state.on::before {
		background: #4fd67a;
	}
	.warn-text {
		color: var(--warn-text);
	}
	.clock {
		font-size: 48px;
		font-weight: 300;
		line-height: 1.1;
		font-variant-numeric: tabular-nums;
	}
	.chart {
		width: 100%;
		height: 62px;
		display: block;
	}
	.small-btn {
		margin-top: var(--space-2);
	}
	/* PLAN 79-5: the graphics widgets. */
	h4.gpu-title {
		margin-bottom: 2px;
	}
	.gpu-name {
		overflow-wrap: anywhere;
		margin-bottom: var(--space-2-5);
	}
	.gpu-usage {
		display: flex;
		align-items: baseline;
		flex-wrap: wrap;
		gap: 0 var(--space-2);
	}
	.huge {
		font-size: 28px;
		font-weight: 600;
		line-height: 1.15;
		font-variant-numeric: tabular-nums;
	}
	.gpu-chart {
		height: 54px;
		margin: var(--space-1-5) 0 0;
	}
	.gpu-tiles {
		margin-top: var(--space-2-5);
	}
	.gpu-tiles:empty {
		display: none;
	}
	.gpu-tiles b {
		display: block;
		font-variant-numeric: tabular-nums;
	}
</style>
