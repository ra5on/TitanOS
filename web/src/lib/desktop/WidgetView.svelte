<script lang="ts">
	// PLAN 61: what one widget of the desktop shows. Reads only data the UI
	// keeps anyway (system, storage, Docker/VM lists); nothing wakes disks.
	import { api } from '../api.ts';
	import {
		formatBytes,
		formatBytesOf,
		formatDuration,
		formatMegabytesPerSecond,
		formatPercent,
	} from '../format.ts';
	import { i18n, t } from '../i18n/index.svelte.ts';
	import { storage } from '../storage.svelte.ts';
	import { system } from '../system.svelte.ts';
	import DesktopIcon from './DesktopIcon.svelte';
	import type { WidgetKind } from './prefs.ts';
	import { dockerPrograms, sources } from './sources.svelte.ts';
	import { session } from '../session.svelte.ts';

	let { kind, onopen }: { kind: WidgetKind; onopen: (app: string, href?: string) => void } =
		$props();

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
{:else if kind === 'tasks'}
	<h4>{t('desktop.widget.tasks')}</h4>
	<button type="button" class="btn small-btn" onclick={() => onopen('app:monitor', '/tools/tasks')}
		>{t('desktop.widget.openTasks')}</button
	>
{/if}

<style>
	h4 {
		margin: 0 0 10px;
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
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		min-width: 0;
	}
	.grow {
		flex: 1;
	}
	.status {
		display: flex;
		align-items: center;
		gap: 12px;
		margin-bottom: 12px;
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
		gap: 8px;
	}
	.in {
		background: color-mix(in srgb, var(--bg) 45%, transparent);
		border-radius: 12px;
		padding: 10px 12px;
		min-width: 0;
	}
	.row-btn {
		display: block;
		width: 100%;
		margin-bottom: 6px;
		border: 0;
		color: inherit;
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.row {
		display: flex;
		justify-content: space-between;
		gap: 8px;
		padding: 3px 0;
	}
	.meter {
		height: 5px;
		border-radius: 3px;
		background: var(--track);
		margin-top: 6px;
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
		gap: 10px;
		width: 100%;
		padding: 5px 4px;
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
		color: var(--ok-text, #9be3be);
	}
	.state::before {
		content: '';
		display: inline-block;
		width: 8px;
		height: 8px;
		margin-right: 6px;
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
		margin-top: 8px;
	}
</style>
