<script lang="ts">
	// Task manager (PLAN 61, the Monitor app of the desktop): live values of
	// the NAS, its processes, containers, VMs, system containers, networks
	// and disks. Polls every two seconds while the page is visible and only
	// what the open tab shows. Users see the overview, read only.
	import { onMount, untrack } from 'svelte';
	import { page } from '$app/state';
	import {
		api,
		ApiError,
		type DockerContainer,
		type DockerStatus,
		type StorageStatus,
		type VM,
	} from '#lib/api.ts';
	import { formatBytes, formatBytesOf, formatPercent } from '#lib/format.ts';
	import { apiErrorText, t, tOr } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { poll } from '#lib/poll.ts';
	import { containerLabel, type LXCList, type LXCStatus } from '#lib/lxc.ts';
	import { vmLabel } from '#lib/vm-name.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import Tabs from '#lib/components/Tabs.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import Segments from '#lib/components/Segments.svelte';
	import Sparkline from '#lib/components/Sparkline.svelte';
	import {
		pushHistory,
		scaleToPeak,
		shortCommand,
		sortProcesses,
		volumeRates,
		type ProcessList,
		type ProcessRow,
		type ProcessSort,
		type TaskSystem,
	} from '#lib/taskmanager.ts';

	type Tab = 'overview' | 'processes' | 'containers' | 'guests' | 'network' | 'disks';
	const tabs: Tab[] = ['overview', 'processes', 'containers', 'guests', 'network', 'disks'];

	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));
	const initial = page.url.searchParams.get('tab') as Tab | null;
	let chosen = $state<Tab>(initial && tabs.includes(initial) ? initial : 'overview');
	const tab = $derived<Tab>(admin ? chosen : 'overview');

	let system = $state<TaskSystem | null>(null);
	let storage = $state<StorageStatus | null>(null);
	let storageAt = 0;
	let processes = $state<ProcessList | null>(null);
	let docker = $state<DockerStatus | null>(null);
	let vms = $state<VM[] | null>(null);
	let lxc = $state<LXCList | null>(null);
	let error = $state('');
	let actionError = $state('');
	let busy = $state('');

	// Histories of the last 40 samples (80 s).
	let cpuHistory = $state<number[]>([]);
	let ramHistory = $state<number[]>([]);
	let netHistory = $state<Record<string, { rx: number[]; tx: number[] }>>({});

	let sort = $state<ProcessSort>('cpu');
	let query = $state('');
	const shownLimit = 300;
	const sorted = $derived(processes ? sortProcesses(processes.processes, sort, query) : []);

	type Confirm = {
		title: string;
		text: string;
		label: string;
		danger: boolean;
		run: () => Promise<void>;
	};
	let confirming = $state<Confirm | null>(null);

	function record(s: TaskSystem) {
		cpuHistory = pushHistory(cpuHistory, s.cpu.usagePercent);
		const total = s.memory.totalBytes || 1;
		ramHistory = pushHistory(ramHistory, ((total - s.memory.availableBytes) / total) * 100);
		const next: typeof netHistory = {};
		for (const n of s.networks ?? []) {
			const before = netHistory[n.interface] ?? { rx: [], tx: [] };
			next[n.interface] = {
				rx: pushHistory(before.rx, n.rxBytesPerSecond),
				tx: pushHistory(before.tx, n.txBytesPerSecond),
			};
		}
		netHistory = next;
	}

	async function loadSystem() {
		const s = await api.get<TaskSystem>('/system');
		system = s;
		record(s);
	}

	// Sleep state and SMART values change slowly and are read from the
	// cache of titand (no disk is woken); every 30 s is enough.
	async function loadStorage(force = false) {
		if (!force && storage && Date.now() - storageAt < 30000) return;
		storageAt = Date.now();
		storage = await api.get<StorageStatus>('/storage');
	}

	async function load() {
		const current = untrack(() => tab);
		try {
			if (current === 'overview') await Promise.all([loadSystem(), loadStorage()]);
			else if (current === 'network') await loadSystem();
			else if (current === 'disks') await Promise.all([loadSystem(), loadStorage()]);
			else if (current === 'processes') processes = await api.get<ProcessList>('/system/processes');
			else if (current === 'containers') docker = await api.get<DockerStatus>('/docker');
			else if (current === 'guests') {
				const [v, l] = await Promise.all([
					api.get<{ vms: VM[] }>('/vms').catch(() => null),
					api.get<LXCList>('/lxc').catch(() => null),
				]);
				vms = v?.vms ?? [];
				lxc = l;
			}
			error = '';
		} catch (err) {
			error = apiErrorText(err);
		}
	}

	let poller: ReturnType<typeof poll> | null = null;
	onMount(() => {
		poller = poll(load, { interval: 2000 });
		return () => poller?.();
	});
	function choose(next: Tab) {
		chosen = next;
		actionError = '';
		void poller?.now();
	}

	function problem(err: unknown): string {
		if (err instanceof ApiError) return tOr(`taskmanager.error.${err.code}`, apiErrorText(err));
		return apiErrorText(err);
	}

	async function act(id: string, run: () => Promise<unknown>) {
		busy = id;
		actionError = '';
		try {
			await run();
		} catch (err) {
			actionError = problem(err);
		} finally {
			busy = '';
			confirming = null;
			void poller?.now();
		}
	}

	function askSignal(p: ProcessRow, signal: 'term' | 'kill') {
		const params = { name: p.name, pid: p.pid };
		confirming = {
			title: t(signal === 'term' ? 'taskmanager.proc.termTitle' : 'taskmanager.proc.killTitle'),
			text: t(
				signal === 'term' ? 'taskmanager.proc.termText' : 'taskmanager.proc.killText',
				params,
			),
			label: t(signal === 'term' ? 'taskmanager.proc.term' : 'taskmanager.proc.kill'),
			danger: true,
			run: () =>
				act(`p${p.pid}`, () =>
					api.post(`/system/processes/${p.pid}/signal`, { signal, start: p.start }),
				),
		};
	}

	function ownerText(p: ProcessRow): string {
		const o = p.owner;
		if (o.kind === 'docker')
			return t('taskmanager.owner.docker', { name: o.name || (o.id ?? '').slice(0, 12) });
		if (o.kind === 'vm') return t('taskmanager.owner.vm', { name: o.name ?? '' });
		if (o.kind === 'lxc') return t('taskmanager.owner.lxc', { name: o.name ?? '' });
		if (o.kind === 'service') return t('taskmanager.owner.service', { name: o.name ?? '' });
		return t('taskmanager.owner.system');
	}

	const rate = (bps: number) => `${formatBytes(bps)}/s`;

	function containerAction(c: DockerContainer, action: 'start' | 'stop' | 'restart') {
		const run = () =>
			act(`c${c.id}`, () => api.post(`/docker/containers/${encodeURIComponent(c.id)}/${action}`));
		if (action === 'start') return run();
		confirming = {
			title: t(action === 'stop' ? 'taskmanager.ct.stopTitle' : 'taskmanager.ct.restartTitle'),
			text: t(action === 'stop' ? 'taskmanager.ct.stopText' : 'taskmanager.ct.restartText', {
				name: c.name,
			}),
			label: t(action === 'stop' ? 'taskmanager.action.stop' : 'taskmanager.action.restart'),
			danger: action === 'stop',
			run,
		};
	}

	function vmAction(v: VM, action: 'start' | 'shutdown') {
		const run = () =>
			act(`v${v.name}`, () => api.post(`/vms/${encodeURIComponent(v.name)}/${action}`));
		if (action === 'start') return run();
		confirming = {
			title: t('taskmanager.vm.stopTitle'),
			text: t('taskmanager.vm.stopText', { name: vmLabel(v) }),
			label: t('taskmanager.vm.shutdown'),
			danger: true,
			run,
		};
	}

	function lxcAction(c: LXCStatus, action: 'start' | 'stop' | 'restart') {
		const run = () =>
			act(`l${c.name}`, () => api.post(`/lxc/${encodeURIComponent(c.name)}/${action}`));
		if (action === 'start') return run();
		confirming = {
			title: t(action === 'stop' ? 'taskmanager.ct.stopTitle' : 'taskmanager.ct.restartTitle'),
			text: t(action === 'stop' ? 'taskmanager.ct.stopText' : 'taskmanager.ct.restartText', {
				name: containerLabel(c),
			}),
			label: t(action === 'stop' ? 'taskmanager.action.stop' : 'taskmanager.action.restart'),
			danger: action === 'stop',
			run,
		};
	}

	const running = (c: DockerContainer) => c.state === 'running' || c.state === 'restarting';
	const volumes = $derived(
		storage && system ? volumeRates(storage.volumes, system.disks ?? []) : [],
	);
	const sleeping = $derived(
		new Set(
			(storage?.diskSleep?.diskStatus ?? []).filter((d) => d.state === 'asleep').map((d) => d.name),
		),
	);
	const gpus = $derived((system?.accelerators ?? []).filter((a) => a.usagePercent != null));
	const memoryUsed = $derived(system ? system.memory.totalBytes - system.memory.availableBytes : 0);
</script>

<PageHead
	title={t('taskmanager.title')}
	parent={admin ? { href: '/tools', label: t('nav.tools') } : undefined}
/>

{#if admin}
	<Tabs
		label={t('taskmanager.tabs')}
		value={tab}
		onchange={choose}
		tabs={tabs.map((id) => ({ id, label: t(`taskmanager.tab.${id}`) }))}
	/>
{:else}
	<p class="muted">{t('taskmanager.readOnly')}</p>
{/if}

{#if error}<p class="notice-error" role="alert">
		{error}
		<button class="btn small" onclick={() => poller?.now()}>{t('ui.retry')}</button>
	</p>{/if}
{#if actionError}<p class="notice-error" role="alert">{actionError}</p>{/if}

<div class="taskmanager">
	{#if tab === 'overview'}
		{#if system}
			<section class="tile metric" aria-label={t('taskmanager.cpu')}>
				<div class="head">
					<h2 class="tile-title">{t('taskmanager.cpu')}</h2>
					<strong class="value">{formatPercent(system.cpu.usagePercent)}</strong>
				</div>
				<Sparkline values={cpuHistory} label={t('taskmanager.cpuHistory')} />
				<p class="muted small">
					{system.cpu.model} · {t('taskmanager.threads', { n: system.cpu.threads })}
				</p>
				{#if system.cpu.threadUsage?.length}
					<ul class="cores" aria-label={t('taskmanager.cores')}>
						{#each system.cpu.threadUsage as core (core.id)}
							<li
								title={`${core.id}: ${core.usagePercent == null ? '–' : formatPercent(core.usagePercent)}`}
							>
								<span class="core-bar" aria-hidden="true"
									><span style:height={`${Math.min(100, core.usagePercent ?? 0)}%`}></span></span
								>
								<span class="core-value"
									>{core.usagePercent == null ? '–' : Math.round(core.usagePercent)}</span
								>
							</li>
						{/each}
					</ul>
				{/if}
			</section>
			<section class="tile metric" aria-label={t('taskmanager.ram')}>
				<div class="head">
					<h2 class="tile-title">{t('taskmanager.ram')}</h2>
					<strong class="value">{formatBytesOf(memoryUsed, system.memory.totalBytes)}</strong>
				</div>
				<Sparkline values={ramHistory} label={t('taskmanager.ramHistory')} />
				<p class="muted small">
					{t('taskmanager.ramAvailable', { value: formatBytes(system.memory.availableBytes) })}
				</p>
			</section>
			{#if system.network}
				{@const n = system.network}
				{@const h = netHistory[n.interface]}
				<section class="tile metric" aria-label={t('taskmanager.net')}>
					<div class="head">
						<h2 class="tile-title">{t('taskmanager.net')} · {n.interface}</h2>
					</div>
					<dl class="pairs">
						<div>
							<dt>{t('taskmanager.rx')}</dt>
							<dd class="mono">{rate(n.rxBytesPerSecond)}</dd>
						</div>
						<div>
							<dt>{t('taskmanager.tx')}</dt>
							<dd class="mono">{rate(n.txBytesPerSecond)}</dd>
						</div>
					</dl>
					{#if h}<Sparkline
							values={scaleToPeak([...h.rx, ...h.tx]).slice(0, h.rx.length)}
							label={t('taskmanager.rxHistory')}
						/>{/if}
				</section>
			{/if}
			<section class="tile metric" aria-label={t('taskmanager.volumes')}>
				<h2 class="tile-title">{t('taskmanager.volumes')}</h2>
				{#if volumes.length}
					<ul class="rows">
						{#each volumes as v (v.name)}
							<li>
								<span class="name">{v.name}</span>
								{#if v.known}
									<span class="mono"
										>{t('taskmanager.read')}
										{rate(v.read)} · {t('taskmanager.write')}
										{rate(v.write)}</span
									>
								{:else}<span class="muted">–</span>{/if}
							</li>
						{/each}
					</ul>
					<p class="muted small">{t('taskmanager.volumesHint')}</p>
				{:else}<p class="muted">{t('taskmanager.noVolumes')}</p>{/if}
			</section>
			{#if gpus.length}
				<section class="tile metric" aria-label={t('taskmanager.gpu')}>
					<h2 class="tile-title">{t('taskmanager.gpu')}</h2>
					<ul class="rows">
						{#each gpus as g (g.address)}
							<li>
								<span class="name">{g.name}</span>
								<span class="mono">{formatPercent(g.usagePercent ?? 0)}</span>
							</li>
						{/each}
					</ul>
				</section>
			{/if}
		{:else if !error}<p class="muted">{t('taskmanager.loading')}</p>{/if}
	{:else if tab === 'processes'}
		<div class="toolbar">
			<input
				class="field search"
				type="search"
				bind:value={query}
				placeholder={t('taskmanager.proc.search')}
				aria-label={t('taskmanager.proc.search')}
			/>
			<Segments
				label={t('taskmanager.proc.sort')}
				bind:value={sort}
				options={[
					{ value: 'cpu', label: t('taskmanager.proc.sortCpu') },
					{ value: 'memory', label: t('taskmanager.proc.sortRam') },
					{ value: 'name', label: t('taskmanager.proc.sortName') },
				]}
			/>
		</div>
		{#if processes}
			<p class="muted small">
				{t('taskmanager.proc.count', { n: sorted.length, all: processes.processes.length })} · {t(
					'taskmanager.proc.scale',
				)}
			</p>
			<div class="proc-head" aria-hidden="true">
				<span>{t('taskmanager.proc.name')}</span>
				<span class="cols"
					><span>{t('taskmanager.proc.pid')}</span><span>{t('taskmanager.proc.user')}</span><span
						>{t('taskmanager.proc.owner')}</span
					><span>{t('taskmanager.cpu')}</span><span>{t('taskmanager.ram')}</span></span
				>
				<span></span>
			</div>
			<ul class="proc-list" aria-label={t('taskmanager.tab.processes')}>
				{#each sorted.slice(0, shownLimit) as p (`${p.pid}-${p.start}`)}
					<li class="proc tile">
						<div class="proc-main">
							<span class="name">{p.name}</span>
							<span class="muted mono cmd" title={p.command}>{shortCommand(p.command)}</span>
						</div>
						<dl class="proc-facts">
							<div>
								<dt>{t('taskmanager.proc.pid')}</dt>
								<dd class="mono">{p.pid}</dd>
							</div>
							<div>
								<dt>{t('taskmanager.proc.user')}</dt>
								<dd>{p.user}</dd>
							</div>
							<div>
								<dt>{t('taskmanager.proc.owner')}</dt>
								<dd>{ownerText(p)}</dd>
							</div>
							<div>
								<dt>{t('taskmanager.cpu')}</dt>
								<dd class="mono">{p.cpuPercent.toFixed(1)} %</dd>
							</div>
							<div>
								<dt>{t('taskmanager.ram')}</dt>
								<dd class="mono">{formatBytes(p.memoryBytes)}</dd>
							</div>
						</dl>
						<div class="actions">
							{#if p.protected}
								<span class="chip">{t('taskmanager.proc.protected')}</span>
							{:else}
								<button
									class="btn small"
									disabled={busy !== ''}
									aria-label={t('taskmanager.proc.termFor', { name: p.name, pid: p.pid })}
									onclick={() => askSignal(p, 'term')}>{t('taskmanager.proc.term')}</button
								>
								<button
									class="btn small btn-danger-outline"
									disabled={busy !== ''}
									aria-label={t('taskmanager.proc.killFor', { name: p.name, pid: p.pid })}
									onclick={() => askSignal(p, 'kill')}>{t('taskmanager.proc.kill')}</button
								>
							{/if}
						</div>
					</li>
				{/each}
			</ul>
			{#if sorted.length > shownLimit}<p class="muted small">
					{t('taskmanager.proc.more', { n: sorted.length - shownLimit })}
				</p>{/if}
		{:else if !error}<p class="muted">{t('taskmanager.loading')}</p>{/if}
	{:else if tab === 'containers'}
		{#if docker && !docker.running}
			<p class="muted">{t('taskmanager.ct.off')}</p>
		{:else if docker}
			<ul class="rows-list">
				{#each docker.containers as c (c.id)}
					<li class="tile entry">
						<div class="entry-main">
							<span class="name">{c.name}</span>
							<span class="muted small mono">{c.image}</span>
						</div>
						<span class="state"
							><span class="dot" class:ok={running(c)} aria-hidden="true"></span>{t(
								running(c) ? 'taskmanager.state.running' : 'taskmanager.state.stopped',
							)}</span
						>
						<span class="mono num">{running(c) ? `${c.cpuPercent.toFixed(1)} %` : '–'}</span>
						<span class="mono num">{running(c) ? formatBytes(c.memoryBytes) : '–'}</span>
						<div class="actions">
							{#if running(c)}
								<button
									class="btn small"
									disabled={busy !== ''}
									onclick={() => containerAction(c, 'restart')}
									>{t('taskmanager.action.restart')}</button
								>
								<button
									class="btn small"
									disabled={busy !== ''}
									onclick={() => containerAction(c, 'stop')}>{t('taskmanager.action.stop')}</button
								>
							{:else}
								<button
									class="btn small"
									disabled={busy !== ''}
									onclick={() => containerAction(c, 'start')}
									>{t('taskmanager.action.start')}</button
								>
							{/if}
						</div>
					</li>
				{:else}<li class="muted">{t('taskmanager.ct.none')}</li>{/each}
			</ul>
		{:else if !error}<p class="muted">{t('taskmanager.loading')}</p>{/if}
	{:else if tab === 'guests'}
		<h2 class="tile-title">{t('taskmanager.vm.title')}</h2>
		{#if vms}
			<ul class="rows-list">
				{#each vms as v (v.name)}
					{@const on = v.state === 'running'}
					<li class="tile entry">
						<div class="entry-main"><span class="name">{vmLabel(v)}</span></div>
						<span class="state"
							><span class="dot" class:ok={on} aria-hidden="true"></span>{t(
								on ? 'taskmanager.state.running' : 'taskmanager.state.stopped',
							)}</span
						>
						<span class="mono num">{on ? `${v.cpuPercent.toFixed(1)} %` : '–'}</span>
						<span class="mono num">{on ? formatBytes(v.memoryBytes) : '–'}</span>
						<div class="actions">
							{#if on}
								<button
									class="btn small"
									disabled={busy !== ''}
									onclick={() => vmAction(v, 'shutdown')}>{t('taskmanager.vm.shutdown')}</button
								>
							{:else}
								<button
									class="btn small"
									disabled={busy !== '' || !v.available}
									onclick={() => vmAction(v, 'start')}>{t('taskmanager.action.start')}</button
								>
							{/if}
						</div>
					</li>
				{:else}<li class="muted">{t('taskmanager.vm.none')}</li>{/each}
			</ul>
		{/if}
		<h2 class="tile-title">{t('taskmanager.lxc.title')}</h2>
		{#if lxc}
			<ul class="rows-list">
				{#each lxc.containers as c (c.name)}
					{@const on = c.state === 'running'}
					<li class="tile entry">
						<div class="entry-main"><span class="name">{containerLabel(c)}</span></div>
						<span class="state"
							><span class="dot" class:ok={on} aria-hidden="true"></span>{t(
								on ? 'taskmanager.state.running' : 'taskmanager.state.stopped',
							)}</span
						>
						<span class="mono num">{on ? `${c.cpuPercent.toFixed(1)} %` : '–'}</span>
						<span class="mono num">{on ? formatBytes(c.memoryBytes) : '–'}</span>
						<div class="actions">
							{#if on}
								<button
									class="btn small"
									disabled={busy !== '' || lxc.dev}
									onclick={() => lxcAction(c, 'restart')}>{t('taskmanager.action.restart')}</button
								>
								<button
									class="btn small"
									disabled={busy !== '' || lxc.dev}
									onclick={() => lxcAction(c, 'stop')}>{t('taskmanager.action.stop')}</button
								>
							{:else}
								<button
									class="btn small"
									disabled={busy !== '' || lxc.dev || !c.available}
									onclick={() => lxcAction(c, 'start')}>{t('taskmanager.action.start')}</button
								>
							{/if}
						</div>
					</li>
				{:else}<li class="muted">{t('taskmanager.lxc.none')}</li>{/each}
			</ul>
		{/if}
	{:else if tab === 'network'}
		{#if system}
			<ul class="rows-list">
				{#each system.networks ?? [] as n (n.interface)}
					{@const h = netHistory[n.interface]}
					{@const up = n.state === 'up'}
					<li class="tile net">
						<div class="entry-main">
							<span class="name">{n.interface}</span>
							<span class="state"
								><span class="dot" class:ok={up} aria-hidden="true"></span>{t(
									up ? 'taskmanager.net.up' : 'taskmanager.net.down',
								)}{#if n.speedMbps > 0}&nbsp;· {n.speedMbps} Mbit/s{/if}</span
							>
						</div>
						<dl class="pairs">
							<div>
								<dt>{t('taskmanager.rx')}</dt>
								<dd class="mono">{rate(n.rxBytesPerSecond)}</dd>
							</div>
							<div>
								<dt>{t('taskmanager.tx')}</dt>
								<dd class="mono">{rate(n.txBytesPerSecond)}</dd>
							</div>
						</dl>
						{#if h}
							{@const scaled = scaleToPeak([...h.rx, ...h.tx])}
							<div class="spark">
								<Sparkline
									values={scaled.slice(0, h.rx.length)}
									label={t('taskmanager.rxHistory')}
								/>
								<Sparkline values={scaled.slice(h.rx.length)} label={t('taskmanager.txHistory')} />
							</div>
						{/if}
					</li>
				{:else}<li class="muted">{t('taskmanager.net.none')}</li>{/each}
			</ul>
		{:else if !error}<p class="muted">{t('taskmanager.loading')}</p>{/if}
	{:else if tab === 'disks'}
		{#if storage && system}
			<p class="muted small">{t('taskmanager.disk.hint')}</p>
			<ul class="rows-list">
				{#each storage.disks as d (d.name)}
					{@const io = (system.disks ?? []).find((x) => x.name === d.name)}
					{@const asleep = sleeping.has(d.name)}
					<li class="tile entry disk">
						<div class="entry-main">
							<span class="name">{d.name}</span>
							<span class="muted small">{d.model}{d.volume ? ` · ${d.volume}` : ''}</span>
						</div>
						<span class="state"
							><span class="dot" class:ok={!asleep} aria-hidden="true"></span>{t(
								asleep ? 'taskmanager.disk.asleep' : 'taskmanager.disk.active',
							)}</span
						>
						<span class="mono num" title={t('taskmanager.disk.temp')}
							>{d.smart && d.smart.temperatureC > 0 ? `${d.smart.temperatureC} °C` : '–'}</span
						>
						<span class="mono io">
							{#if asleep || !io}–{:else}{t('taskmanager.read')}
								{rate(io.readBytesPerSecond)} · {t('taskmanager.write')}
								{rate(io.writeBytesPerSecond)} · {t('taskmanager.disk.busy', {
									n: Math.round(io.busyPercent),
								})}{/if}
						</span>
					</li>
				{:else}<li class="muted">{t('taskmanager.disk.none')}</li>{/each}
			</ul>
		{:else if !error}<p class="muted">{t('taskmanager.loading')}</p>{/if}
	{/if}
</div>

{#if confirming}
	{@const c = confirming}
	<Dialog title={c.title} locked={busy !== ''} onclose={() => (confirming = null)}>
		<p>{c.text}</p>
		<div class="dialog-actions">
			<button class="btn" disabled={busy !== ''} onclick={() => (confirming = null)}
				>{t('common.cancel')}</button
			>
			<button
				class={c.danger ? 'btn btn-danger' : 'btn btn-primary'}
				disabled={busy !== ''}
				onclick={() => c.run()}>{c.label}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.taskmanager {
		display: flex;
		flex-direction: column;
		gap: var(--gap);
		container-type: inline-size;
		min-width: 0;
	}
	.metric {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		padding: var(--tile-pad);
	}
	.head {
		display: flex;
		justify-content: space-between;
		align-items: baseline;
		gap: var(--space-2);
		flex-wrap: wrap;
	}
	h2.tile-title {
		margin: 0;
	}
	.value {
		font-size: var(--text-lg, 1.2rem);
		font-variant-numeric: tabular-nums;
	}
	.small {
		font-size: var(--text-sm, 0.85rem);
		margin: 0;
	}
	.cores {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(2.2rem, 1fr));
		gap: 6px;
	}
	.cores li {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 2px;
	}
	.core-bar {
		display: flex;
		align-items: flex-end;
		width: 100%;
		height: 28px;
		background: var(--track);
		border-radius: var(--radius-bar);
		overflow: hidden;
	}
	.core-bar > span {
		display: block;
		width: 100%;
		background: var(--accent);
	}
	.core-value {
		font-size: var(--text-xs);
		color: var(--muted);
		font-variant-numeric: tabular-nums;
	}
	.pairs {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2) var(--space-4, 24px);
		margin: 0;
	}
	.pairs div {
		display: flex;
		gap: 6px;
	}
	dt {
		color: var(--muted);
	}
	dd {
		margin: 0;
	}
	.rows {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
	.rows li {
		display: flex;
		justify-content: space-between;
		flex-wrap: wrap;
		gap: 4px var(--space-2);
		min-width: 0;
	}
	.name {
		font-weight: 600;
		overflow-wrap: anywhere;
	}
	.toolbar {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
		align-items: center;
	}
	.search {
		flex: 1 1 14rem;
		min-width: 0;
	}
	.proc-list,
	.rows-list {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
	.proc {
		display: grid;
		grid-template-columns: minmax(0, 1fr);
		gap: 6px;
		padding: var(--space-2) var(--tile-pad);
	}
	.proc-main,
	.entry-main {
		display: flex;
		flex-direction: column;
		min-width: 0;
	}
	.cmd {
		font-size: var(--text-xs);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.proc-facts {
		display: flex;
		flex-wrap: wrap;
		gap: 2px var(--space-3, 12px);
		margin: 0;
		font-size: var(--text-sm, 0.85rem);
	}
	.proc-facts div {
		display: flex;
		gap: 4px;
		min-width: 0;
	}
	.actions {
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
		align-items: center;
	}
	.entry {
		display: grid;
		grid-template-columns: minmax(0, 1fr);
		gap: 6px;
		padding: var(--space-2) var(--tile-pad);
		align-items: center;
	}
	.state {
		display: inline-flex;
		align-items: center;
		gap: 6px;
	}
	.num {
		font-variant-numeric: tabular-nums;
	}
	.net {
		display: flex;
		flex-direction: column;
		gap: 6px;
		padding: var(--space-2) var(--tile-pad);
	}
	.spark {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: var(--space-2);
	}
	.proc-head {
		display: none;
	}
	/* Wide windows: one line per process or guest. */
	@container (min-width: 700px) {
		.proc-head {
			display: grid;
			grid-template-columns: minmax(0, 1.4fr) minmax(0, 2fr) 11.5rem;
			gap: 6px;
			padding: 0 var(--tile-pad);
			color: var(--muted);
			font-size: var(--text-sm, 0.85rem);
		}
		.proc-head .cols {
			display: grid;
			grid-template-columns: 4rem minmax(0, 5rem) minmax(0, 1fr) 4.5rem 5rem;
			gap: var(--space-2);
		}
		.proc {
			grid-template-columns: minmax(0, 1.4fr) minmax(0, 2fr) 11.5rem;
			align-items: center;
		}
		.proc-facts {
			display: grid;
			grid-template-columns: 4rem minmax(0, 5rem) minmax(0, 1fr) 4.5rem 5rem;
			gap: var(--space-2);
		}
		.proc-facts dt {
			position: absolute;
			width: 1px;
			height: 1px;
			overflow: hidden;
			clip: rect(0 0 0 0);
			white-space: nowrap;
		}
		.proc-facts dd {
			overflow: hidden;
			text-overflow: ellipsis;
			white-space: nowrap;
		}
		.entry {
			grid-template-columns: minmax(0, 1fr) 8rem 5rem 6rem 13rem;
		}
		.entry .actions {
			justify-content: flex-end;
		}
		.entry.disk {
			grid-template-columns: minmax(0, 1fr) 8rem 4rem minmax(0, 1.4fr);
		}
	}
</style>
