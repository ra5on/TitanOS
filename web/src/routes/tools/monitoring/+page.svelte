<script lang="ts">
	import { onMount } from 'svelte';
	import NavigationGuard from '#lib/operations/NavigationGuard.svelte';
	import { api, type StorageStatus } from '#lib/api.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import HistoryChart from '#lib/operations/HistoryChart.svelte';
	import { problemText, requestProblem, type Problem } from '../problem.ts';
	import { type MonitorSettings, type MonitorStatus, type MonitorSample } from '#lib/operations.ts';
	type Trend = {
		volume: string;
		known: boolean;
		used: number;
		total: number;
		dailyBytes: number;
		daysTo90: number | null;
		daysToFull: number | null;
		warning: boolean;
		hours: number;
	};
	let trends = $state<Trend[]>([]);
	let initialSettings = $state('');
	let status = $state<MonitorStatus | null>(null),
		settings = $state<MonitorSettings | null>(null),
		storage = $state<StorageStatus | null>(null),
		samples = $state<MonitorSample[]>([]),
		hours = $state(24),
		busy = $state(false),
		error = $state<Problem | null>(null),
		saved = $state(false),
		disk = $state(''),
		network = $state('');
	const dirty = $derived(!!settings && JSON.stringify(settings) !== initialSettings);
	const disks = $derived([...new Set(samples.flatMap((s) => s.disks.map((d) => d.name)))].sort()),
		networks = $derived([...new Set(samples.flatMap((s) => s.networks.map((n) => n.name)))].sort());
	async function load() {
		busy = true;
		error = null;
		try {
			status = await api.get<MonitorStatus>('/monitoring');
			if (!settings) {
				settings = structuredClone($state.snapshot(status!.settings));
				initialSettings = JSON.stringify(settings);
			}
			samples = await api.get<MonitorSample[]>(`/monitoring/history?hours=${hours}`);
			if (status?.settings.enabled) trends = await api.get<Trend[]>('/monitoring/trends');
			else trends = [];
			if (!disks.includes(disk)) disk = disks[0] ?? '';
			if (!networks.includes(network)) network = networks[0] ?? '';
		} catch (e) {
			error = requestProblem(e, t);
		} finally {
			busy = false;
		}
	}
	onMount(() => {
		load();
		api
			.get<StorageStatus>('/storage')
			.then((s) => (storage = s))
			.catch((e) => (error = requestProblem(e, t)));
	});
	async function save() {
		if (!settings) return;
		busy = true;
		saved = false;
		error = null;
		try {
			status = await api.put<MonitorStatus>('/monitoring', settings);
			settings = structuredClone($state.snapshot(status!.settings));
			initialSettings = JSON.stringify(settings);
			saved = true;
		} catch (e) {
			error = requestProblem(e, t);
		} finally {
			busy = false;
		}
		if (!error) load();
	}
	async function download(format: 'csv' | 'json') {
		busy = true;
		error = null;
		try {
			await api.download(
				`/monitoring/export?hours=${hours}&format=${format}`,
				`titanos-monitoring-${hours}h.${format}`,
			);
		} catch (e) {
			error = requestProblem(e, t);
		} finally {
			busy = false;
		}
	}
</script>

<NavigationGuard {dirty} />
<PageHead title={t('ops.monitoring')} parent={{ href: '/tools', label: t('nav.tools') }}
	>{#snippet actions()}<button class="btn" disabled={busy} onclick={load}>{t('ops.refresh')}</button
		>{/snippet}</PageHead
>
<p class="muted">{t('ops.monitorHint')}</p>
{#snippet problemLine(problem: Problem)}{problem.text}{#if problem.detail}<code class="detail"
			>{problem.detail}</code
		>{/if}{/snippet}
{#if error}<p class="notice-error" role="alert">
		{@render problemLine(error)}
		<button class="btn small" disabled={busy} onclick={load}>{t('ui.retry')}</button>
	</p>{/if}{#if status?.error}<p class="notice-warn">
		{@render problemLine(problemText(status.error, t))}
	</p>{/if}
{#if settings}<form
		class="tile settings"
		onsubmit={(e) => {
			e.preventDefault();
			save();
		}}
	>
		<label class="check"
			><input type="checkbox" bind:checked={settings.enabled} />{t('ops.enabled')}</label
		><label
			>{t('ops.volume')}<select
				class="field"
				bind:value={settings.volume}
				required={settings.enabled}
				><option value="">{t('ops.choose')}</option
				>{#each storage?.volumes ?? [] as volume (volume.name)}<option value={volume.name}
						>{volume.name}</option
					>{/each}</select
			></label
		><label
			>{t('ops.days')}<select class="field" bind:value={settings.days}
				><option value={7}>7</option><option value={30}>30</option></select
			></label
		><label
			>{t('ops.budget')}<input
				class="field"
				type="number"
				min="8"
				max="256"
				bind:value={settings.maxMiB}
				required
			/></label
		><button class="btn btn-primary" disabled={busy}>{t('ops.save')}</button
		>{#if saved && !dirty}<span role="status">{t('notify.saved')}</span>{/if}
	</form>{/if}
<div class="toolbar">
	<label
		>{t('ops.range')}<select class="field" bind:value={hours} disabled={busy} onchange={load}
			><option value={24}>24 h</option><option value={168}>7 d</option><option value={720}
				>30 d</option
			></select
		></label
	><button class="btn" disabled={busy} onclick={() => download('csv')}
		>{t('tasks.exportCSV')}</button
	><button class="btn" disabled={busy} onclick={() => download('json')}
		>{t('tasks.exportJSON')}</button
	><span class="muted">{t('ops.samples', { count: samples.length })}</span>
</div>
{#if trends.length}<section class="tile trends">
		<h2 class="tile-title">{t('tasks.trends')}</h2>
		<p class="muted">{t('tasks.trendHint')}</p>
		{#each trends as trend (trend.volume)}<div>
				<strong>{trend.volume}</strong>{#if trend.warning}<p class="notice-warn">
						{t('tasks.capacityWarning')}
					</p>{/if}{#if trend.known && trend.daysTo90 !== null}<p>
						{t('tasks.days90', { count: Math.ceil(trend.daysTo90) })} · {t('tasks.daysFull', {
							count: Math.ceil(trend.daysToFull ?? 0),
						})}
					</p>{:else}<p class="muted">
						{t(trend.known ? 'tasks.noGrowth' : 'tasks.trendUnknown')}
					</p>{/if}
			</div>{/each}
	</section>{/if}
<div class="charts">
	<HistoryChart {samples} value={(s) => s.cpu} label={t('ops.cpu')} maximum={100} /><HistoryChart
		{samples}
		value={(s) => (s.ramTotal ? (100 * (s.ramTotal - s.ramAvailable)) / s.ramTotal : null)}
		label={t('ops.ram')}
		maximum={100}
	/>
</div>
<div class="toolbar">
	<label
		>{t('ops.io')}<select class="field" bind:value={disk}
			>{#each disks as name (name)}<option value={name}>{name}</option>{/each}</select
		></label
	>
</div>
<div class="charts">
	<HistoryChart
		{samples}
		value={(s) => {
			const d = s.disks.find((d) => d.name === disk);
			return d ? d.readBps / 1048576 : null;
		}}
		label={t('ops.read')}
		unit="MiB/s"
	/><HistoryChart
		{samples}
		value={(s) => {
			const d = s.disks.find((d) => d.name === disk);
			return d ? d.writeBps / 1048576 : null;
		}}
		label={t('ops.write')}
		unit="MiB/s"
	/><HistoryChart
		{samples}
		value={(s) => s.disks.find((d) => d.name === disk)?.latencyMs ?? null}
		label={t('ops.latency')}
		unit="ms"
	/>
</div>
<div class="toolbar">
	<label
		>{t('ops.network')}<select class="field" bind:value={network}
			>{#each networks as name (name)}<option value={name}>{name}</option>{/each}</select
		></label
	>
</div>
<div class="charts">
	<HistoryChart
		{samples}
		value={(s) => {
			const n = s.networks.find((n) => n.name === network);
			return n ? n.receive / 1048576 : null;
		}}
		label={t('ops.receive')}
		unit="MiB/s"
	/><HistoryChart
		{samples}
		value={(s) => {
			const n = s.networks.find((n) => n.name === network);
			return n ? n.send / 1048576 : null;
		}}
		label={t('ops.send')}
		unit="MiB/s"
	/>
	{#each [...new Set(samples.flatMap( (s) => s.volumes.map((v) => v.name) ))].sort() as name (name)}<HistoryChart
			{samples}
			value={(s) => {
				const v = s.volumes.find((v) => v.name === name);
				return v ? v.used / 1073741824 : null;
			}}
			label={`${t('ops.usage')} · ${name}`}
			unit="GiB"
		/>{/each}
</div>

<style>
	.trends {
		padding: var(--tile-pad);
		margin: var(--gap) 0;
	}
	.trends > div {
		border-bottom: 1px solid var(--border);
		padding: 12px 0;
	}
	.settings {
		display: flex;
		flex-wrap: wrap;
		align-items: end;
		gap: 14px;
		padding: var(--tile-pad);
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		min-width: 0;
		font-size: var(--text-base);
	}
	.settings label {
		flex: 1 1 150px;
	}
	.check {
		flex-direction: row;
		align-items: center;
	}
	.toolbar {
		display: flex;
		align-items: end;
		flex-wrap: wrap;
		gap: 16px;
		margin: 20px 0 12px;
	}
	.toolbar select {
		width: auto;
		min-width: 130px;
	}
	.charts {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(min(100%, 330px), 1fr));
		gap: var(--gap);
	}
	.detail {
		display: block;
		margin-top: 4px;
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
	}
</style>
