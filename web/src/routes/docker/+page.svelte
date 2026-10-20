<script lang="ts">
	import EmptyState from '#lib/components/EmptyState.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { session } from '#lib/session.svelte.ts';
	import { defaultDockerLayout, sanitizeDockerLayout, dockerGroups, orderedDockerGroups, moveDockerGroup, type DockerGroup, type DockerView } from '#lib/docker-layout.ts';
	import Icon from '#lib/components/Icon.svelte';
	import { dockerName } from '#lib/docker-name.ts';
	import { api, ApiError, type DockerContainer, type DockerStatus } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { formatBytes, formatPercent } from '#lib/format.ts';
	import { errorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import { storage } from '#lib/storage.svelte.ts';
	import { system } from '#lib/system.svelte.ts';
	import { addContainerConsole } from '#lib/terminal.svelte.ts';
	import { tailnetHost, type TailnetStatus } from '#lib/app-links.ts';
	import AppIcon from '#lib/components/AppIcon.svelte';
	import ContainerCard from './ContainerCard.svelte';
	import ContainerRow from './ContainerRow.svelte';
	import { isRunning, monogramHue, type ContainerActions } from './container.ts';

	let tailnet = $state<TailnetStatus | null>(null);
 const tailscaleHost = $derived(tailnetHost(tailnet));
 $effect(() => {
  let disposed = false;
  const refresh = async () => {
   if (document.hidden) return;
   try { const value = await api.get<TailnetStatus>('/plugins/tailscale'); if (!disposed) tailnet = value; } catch { if (!disposed) tailnet = null; }
  };
  refresh(); const timer = setInterval(refresh, 15000);
  return () => { disposed = true; clearInterval(timer); };
 });
 const NAME = /^[a-z0-9][a-z0-9_-]{0,31}$/;

	let status = $state<DockerStatus | null>(null);
	let error = $state<{ text: string; detail: string } | null>(null);
	let busy = $state(false);
	let search = $state('');
	let filter = $state<'all' | 'running' | 'stopped' | 'updates'>('all');
	// Stacks the user folded; all others show their services.
	let folded = $state<Record<string, boolean>>({});
	let layout = $state(sanitizeDockerLayout(session.docker));
	let savedLayout = $state(sanitizeDockerLayout(session.docker));
	let editing = $state(false);
	let savingLayout = $state(false);
	let layoutError = $state('');
	let layoutSaved = $state(false);
	let dragged = $state<string | null>(null);
	function startEditing() { editing = true; layoutSaved = false; layoutError = ''; search = ''; filter = 'all'; }
	// The view is saved right away; only the order waits for "Save layout".
	async function setView(view: DockerView) {
		if (layout.view === view) return;
		layout.view = view;
		layoutError = '';
		const value = sanitizeDockerLayout({ ...savedLayout, view });
		try {
			await api.put('/preferences/docker', value);
			session.docker = value;
			savedLayout = value;
		} catch (err) {
			layoutError = errorText(err instanceof ApiError ? err.code : 'internal');
		}
	}
	function move(from: string, to: string) { layout.order = moveDockerGroup(layout.order, groups, from, to); }
	function moveBy(id: string, offset: number) {
		const target = groups[groups.findIndex(group => group.id === id) + offset];
		if (target) move(id, target.id);
	}
	function cancelLayout() { layout = sanitizeDockerLayout(savedLayout); editing = false; layoutError = ''; }
	async function saveLayout() {
		savingLayout = true; layoutError = '';
		const value = sanitizeDockerLayout(layout);
		try {
			await api.put('/preferences/docker', value);
			session.docker = value; savedLayout = value; editing = false; layoutSaved = true;
		} catch (err) { layoutError = errorText(err instanceof ApiError ? err.code : 'internal'); }
		finally { savingLayout = false; }
	}
	let dialog = $state<
		| { kind: 'import' | 'settings' }
		| { kind: 'logs'; container: DockerContainer; text: string | null }
		| { kind: 'remove'; container: DockerContainer }
		| { kind: 'removeApp'; app: string; purge: boolean }
		| null
	>(null);
	let draft = $state({ name: '', compose: '', enabled: true, volume: '' });
	let dialogError = $state<{ text: string; detail: string } | null>(null);

	const volumes = $derived((storage.status?.volumes ?? []).map((volume) => volume.name));
	const host = $derived(system.info?.network?.ipv4.split('/')[0] || location.hostname);
	const containers = $derived(status?.containers ?? []);
	const runCount = $derived(containers.filter(isRunning).length);
	const totals = $derived(
		t('docker.totals', {
			cpu: formatPercent(containers.reduce((sum, container) => sum + container.cpuPercent, 0)),
			ram: formatBytes(containers.reduce((sum, container) => sum + container.memoryBytes, 0))
		})
	);

	const visible = $derived(
		containers.filter(
			(container) =>
				(filter === 'all' || (filter === 'updates' ? container.updateAvailable : (filter === 'running') === isRunning(container))) &&
				(search === '' || `${container.name} ${container.image} ${container.app}`.toLowerCase().includes(search.toLowerCase()))
		)
	);
	const groups = $derived(orderedDockerGroups(dockerGroups(containers), layout.order));
	const visibleIDs = $derived(new Set(visible.map(container => container.id)));
	const shownGroups = $derived(groups.filter(group => group.members.some(container => visibleIDs.has(container.id))));
	const shownMembers = (group: DockerGroup) => group.members.filter((container) => visibleIDs.has(container.id));
	const sum = (group: DockerGroup, value: (container: DockerContainer) => number) =>
		group.members.filter(isRunning).reduce((total, container) => total + value(container), 0);
	const updateCount = $derived(containers.filter((container) => container.updateAvailable).length);
	const updatable = $derived(containers.some((container) => container.updateAvailable && container.managed));
	const checked = $derived.by(() => {
		const time = new Date(status?.updatesChecked ?? '').getTime();
		if (!time || time < 0) return t('docker.updatesNever');
		return t('docker.updatesChecked', { time: new Intl.DateTimeFormat(i18n.locale, { dateStyle: 'short', timeStyle: 'short' }).format(time) });
	});
	const jobs = $derived(Object.entries(status?.jobs ?? {}).filter(([, job]) => job.state !== 'done'));
	// Apps whose setup never created a container (or whose containers are
	// gone) have no card; without this they could not be removed.
	const idleApps = $derived((status?.apps ?? []).filter((app) => !containers.some((container) => container.app === app) && !jobs.some(([name]) => name === app)));

	function problem(err: unknown): { text: string; detail: string } {
		const code = err instanceof ApiError ? err.code : 'internal';
		const detailed = code === 'invalid_compose' || code === 'docker_failed';
		return { text: errorText(code), detail: detailed && err instanceof ApiError ? err.message : '' };
	}

	async function load() {
		if (document.hidden) return;
		try {
			status = await api.get<DockerStatus>('/docker');
		} catch (err) {
			if (!status) error = problem(err);
		}
	}
	$effect(() => {
		load();
		const timer = setInterval(load, 3000);
		return () => clearInterval(timer);
	});

	async function run(action: () => Promise<unknown>) {
		busy = true;
		error = null;
		try {
			await action();
		} catch (err) {
			error = problem(err);
		}
		await load();
		busy = false;
	}

	async function showLogs(container: DockerContainer) {
		dialog = { kind: 'logs', container, text: null };
		try {
			const res = await api.get<{ logs: string }>(`/docker/containers/${container.id}/logs?tail=300`);
			if (dialog?.kind === 'logs') dialog.text = res.logs;
		} catch (err) {
			if (dialog?.kind === 'logs') dialog.text = problem(err).text;
		}
	}

	function remove(container: DockerContainer) {
		dialog = null;
		run(() => api.delete(`/docker/containers/${container.id}`));
	}

	function actionsFor(container: DockerContainer): ContainerActions {
		return {
			onaction: (action) => run(() => api.post(`/docker/containers/${container.id}/${action}`)),
			onlogs: () => showLogs(container),
			onremove: () => (container.managed ? askRemoveApp(container.app) : (dialog = { kind: 'remove', container })),
			onconsole: () => addContainerConsole(container.id, dockerName(container, 'distinct')),
			onupdate: () => run(() => api.post(`/docker/apps/${container.app}/update`))
		};
	}

	function askRemoveApp(app: string) {
		dialog = { kind: 'removeApp', app, purge: false };
	}

	function removeApp(app: string, purge: boolean) {
		dialog = null;
		run(() => api.delete(`/docker/apps/${app}${purge ? `?deleteData=${encodeURIComponent(app)}` : ''}`));
	}

	function open(kind: 'import' | 'settings') {
		draft = { name: '', compose: '', enabled: status?.settings.enabled ?? true, volume: status?.settings.volume || volumes[0] || '' };
		dialogError = null;
		dialog = { kind };
	}

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		dialogError = null;
		try {
			if (dialog?.kind === 'import') {
				if (!NAME.test(draft.name)) return void (dialogError = { text: errorText('invalid_app_name'), detail: '' });
				await api.post('/docker/apps', { name: draft.name, compose: draft.compose });
			} else {
				await api.put('/docker/settings', { enabled: draft.enabled, volume: draft.volume });
			}
			dialog = null;
			await load();
		} catch (err) {
			dialogError = problem(err);
		}
	}

	// "Import compose file" on the Apps page leads here.
	$effect(() => {
		if (status?.running && location.hash === '#import') {
			history.replaceState(history.state, '', location.pathname);
			open('import');
		}
	});

	const enable = () => run(() => api.put('/docker/settings', { enabled: true, volume: draft.volume || volumes[0] }));
</script>

<PageHead title={t('docker.title')}>
	{#if status?.running}
		<p class="muted summary">
			<span><span class="dot ok" aria-hidden="true"></span>{t('docker.running', { count: runCount })}</span>
			<span><span class="dot" aria-hidden="true"></span>{t('docker.stopped', { count: containers.length - runCount })}</span>
			<span class="mono">{totals}</span>
			<span>{t('docker.version', { version: status.version })}</span>
			<span>{checked}</span>
			<button type="button" class="link" onclick={() => run(async () => (status = await api.post<DockerStatus>('/docker/updates/check')))} disabled={busy}>{t('docker.checkUpdates')}</button>
		</p>
	{/if}
	{#snippet actions()}
		{#if status?.settings.enabled}
			<button type="button" class="btn" onclick={() => open('settings')}>{t('docker.settings')}</button>
			<button type="button" class="btn" onclick={() => open('import')} disabled={!status.running}>{t('docker.import')}</button>
			{#if updatable}
				<button type="button" class="btn" onclick={() => run(() => api.post('/docker/updates/apply'))} disabled={busy}>{t('docker.updateAll')}</button>
			{/if}
			<a class="btn btn-primary" href="/apps">{t('docker.installApp')}</a>
		{/if}
	{/snippet}
</PageHead>

{#if error}
	<div class="notice-error" role="alert">{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}</div>
{/if}

{#if status && !status.settings.enabled}
	<section class="tile setup">
		<h2>{t('docker.setupTitle')}</h2>
		<p class="muted">{t('docker.setupText')}</p>
		{#if volumes.length === 0}
			<p class="notice-warn">{t('docker.setupNeedVolume')}</p>
		{:else}
			<div class="setup-row">
				<label><span>{t('docker.volume')}</span>
					<select class="field" bind:value={draft.volume}>{#each volumes as volume (volume)}<option value={volume}>{volume}</option>{/each}</select>
				</label>
				<button type="button" class="btn btn-primary" onclick={enable} disabled={busy}>{t('docker.enable')}</button>
			</div>
		{/if}
	</section>
{:else if status && !status.running}
	<p class="notice-warn" role="status">
		{volumes.includes(status.settings.volume) && storage.status?.volumes.find((volume) => volume.name === status?.settings.volume)?.mounted
			? t('docker.starting')
			: t('docker.waitingVolume', { volume: status.settings.volume })}
	</p>
{:else if status}
	{#each jobs as [app, job] (app)}
		<div class={job.state === 'failed' ? 'notice-error' : 'notice-warn'} role="status">
			{t(job.state === 'failed' ? 'docker.jobFailed' : 'docker.jobRunning', { app })}
			{#if job.state === 'failed' && job.output}<pre>{job.output}</pre>{/if}
			{#if job.state === 'failed'}
				<div class="buttons job-actions">
					<button type="button" class="btn" disabled={busy} onclick={() => run(() => api.post(`/docker/apps/${app}/update`))}>{t('common.retry')}</button>
					<a class="btn" href="/docker/apps/{app}">{t('docker.edit')}</a>
					<button type="button" class="btn" disabled={busy} onclick={() => askRemoveApp(app)}>{t('docker.remove')}</button>
					<button type="button" class="btn" disabled={busy} onclick={() => run(() => api.delete(`/docker/apps/${app}/job`))}>{t('docker.jobDismiss')}</button>
				</div>
			{/if}
		</div>
	{/each}

	{#each idleApps as app (app)}
		<div class="notice-warn" role="status">
			{t('docker.appIdle', { app, path: `/config/apps/${app}/compose.yaml` })}
			<div class="buttons job-actions">
				<button type="button" class="btn" disabled={busy} onclick={() => run(() => api.post(`/docker/apps/${app}/update`))}>{t('docker.appStart')}</button>
				<a class="btn" href="/docker/apps/{app}">{t('docker.edit')}</a>
				<button type="button" class="btn" disabled={busy} onclick={() => askRemoveApp(app)}>{t('docker.remove')}</button>
			</div>
		</div>
	{/each}

	{#if containers.length > 0}
		<div class="controls">
			<input class="field search" type="search" disabled={editing} bind:value={search} placeholder={t('docker.search')} aria-label={t('docker.search')} />
			<div class="segmented" role="group" aria-label={t('docker.filter')}>
				{#each [['all', containers.length], ['running', runCount], ['stopped', containers.length - runCount], ['updates', updateCount]] as [id, count] (id)}
					<button type="button" aria-pressed={filter === id} disabled={editing} onclick={() => (filter = id as typeof filter)}>
						{t(id === 'all' ? 'docker.filterAll' : id === 'running' ? 'docker.filterRunning' : id === 'stopped' ? 'docker.filterStopped' : 'docker.filterUpdates')}<span class="mono count">{count}</span>
					</button>
				{/each}
			</div>
			<div class="view-tools">
				<div class="segmented" role="group" aria-label={t('docker.layout.view')}>
					{#each [['list', t('docker.layout.list')], ['cards', t('docker.layout.cards')]] as [id, label] (id)}
						<button type="button" aria-pressed={layout.view === id} disabled={savingLayout} onclick={() => setView(id as DockerView)}>
							<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><path d={id === 'list' ? 'M4 6h16M4 12h16M4 18h16' : 'M4 4h7v7H4zM13 4h7v7h-7zM4 13h7v7H4zM13 13h7v7h-7z'} /></svg>{label}
						</button>
					{/each}
				</div>
				<button type="button" class="btn" onclick={startEditing} disabled={editing}><Icon name="edit" size={16} />{t('docker.layout.edit')}</button>
			</div>
		</div>
		{#if layoutError && !editing}<p class="notice-error" role="alert">{layoutError}</p>{/if}
		{#if layoutSaved}<p class="muted saved" role="status">{t('docker.layout.saved')}</p>{/if}
	{/if}

	{#if editing}
		<section class="tile layout-editor" aria-label={t('docker.layout.edit')}>
			<p>{t('docker.layout.hint')}</p>
			<label class="check"><input type="checkbox" bind:checked={layout.details} disabled={savingLayout} />{t('docker.layout.details')}</label>
			<ol class="order">
				{#each groups as group, index (group.id)}
					<li class:dragging={dragged === group.id}
						ondragover={(event) => { if (dragged && !savingLayout) event.preventDefault(); }}
						ondrop={(event) => { event.preventDefault(); if (dragged && !savingLayout) move(dragged, group.id); dragged = null; }}>
						<button type="button" class="btn grip" draggable={!savingLayout} disabled={savingLayout} aria-label={t('dashboard.move', { tile: group.name })}
							ondragstart={(event) => { dragged = group.id; event.dataTransfer?.setData('text/plain', group.id); }} ondragend={() => (dragged = null)}><Icon name="grip" size={16} /></button>
						<span class="order-name">{group.name}</span>
						{#if group.members.length > 1}<span class="chip">{t('docker.stack')}</span>{/if}
						<button type="button" class="btn arrow" disabled={savingLayout || index === 0} aria-label={t('docker.layout.before', { name: group.name })} onclick={() => moveBy(group.id, -1)}>↑</button>
						<button type="button" class="btn arrow" disabled={savingLayout || index === groups.length - 1} aria-label={t('docker.layout.after', { name: group.name })} onclick={() => moveBy(group.id, 1)}>↓</button>
					</li>
				{/each}
			</ol>
			{#if layoutError}<p class="notice-error" role="alert">{layoutError}</p>{/if}
			<div class="buttons">
				<button type="button" class="btn" disabled={savingLayout} onclick={() => { layout = { ...defaultDockerLayout(), view: layout.view }; }}>{t('dashboard.restore')}</button>
				<span class="spacer"></span>
				<button type="button" class="btn" disabled={savingLayout} onclick={cancelLayout}>{t('common.cancel')}</button>
				<button type="button" class="btn btn-primary" disabled={savingLayout} onclick={saveLayout}>{t('docker.layout.save')}</button>
			</div>
		</section>
	{/if}

	{#if shownGroups.length > 0}
		{#if layout.view === 'list'}
			<div class="list-wrap">
				<div class="tile list" role="list" aria-label={t('docker.title')}>
					<div class="head" aria-hidden="true">
						<span>{t('docker.column.name')}</span><span>{t('docker.column.state')}</span><span>{t('docker.cpu')}</span><span>{t('docker.ram')}</span><span>{t('docker.column.ports')}</span><span></span>
					</div>
					{#each shownGroups as group (group.id)}
						{#if group.members.length > 1}
							<div class="stack" role="listitem" aria-label="{t('docker.stack')} {group.name}">
								{@render stackHead(group)}
								{#if !folded[group.id]}
									<div class="members" role="list">
										{#each shownMembers(group) as container (container.name)}
											{@render row(container, true)}
										{/each}
									</div>
								{/if}
							</div>
						{:else}
							{@render row(group.members[0], false)}
						{/if}
					{/each}
				</div>
			</div>
		{:else}
			<div class="cards">
				{#each shownGroups as group (group.id)}
					{#if group.members.length > 1}
						<section class="tile stack-card" aria-label="{t('docker.stack')} {group.name}">
							<div class="list-wrap">
								<div class="list">
									{@render stackHead(group)}
									{#if !folded[group.id]}
										<div class="members" role="list">
											{#each shownMembers(group) as container (container.name)}
												{@render row(container, true)}
											{/each}
										</div>
									{/if}
								</div>
							</div>
						</section>
					{:else}
						{@const container = group.members[0]}
						<ContainerCard {container} {host} {tailscaleHost} {busy} details={layout.details} {...actionsFor(container)} />
					{/if}
				{/each}
			</div>
		{/if}
	{/if}
	{#if containers.length === 0 && jobs.length === 0 && idleApps.length === 0}
		<EmptyState title={t('docker.empty')} />
	{:else if visible.length === 0 && containers.length > 0}
		<EmptyState title={t('docker.noMatch')} />
	{/if}
{/if}

{#snippet row(container: DockerContainer, member: boolean)}
	<ContainerRow {container} {host} {tailscaleHost} {busy} {member} details={layout.details} {...actionsFor(container)} />
{/snippet}

{#snippet stackHead(group: DockerGroup)}
	{@const running = group.members.filter(isRunning).length}
	<div class="stack-head">
		<button type="button" class="stack-toggle" aria-expanded={!folded[group.id]} onclick={() => (folded[group.id] = !folded[group.id])}>
			<span class="stack-icon" style:background="hsl({monogramHue(group.name)} 45% 42%)"><AppIcon src={group.members.find((container) => container.icon)?.icon ?? ''} name={group.name} /></span>
			<span class="stack-names">
				<span class="stack-line"><span class="stack-name">{group.name}</span><span class="chip">{t('docker.stack')}</span>{#if group.members.some((container) => container.updateAvailable)}<span class="chip warn">{t('docker.update')}</span>{/if}<span class="chevron" class:folded={folded[group.id]}><Icon name="chevronDown" size={16} /></span></span>
				<span class="stack-sub">{t('docker.stackServices', { count: group.members.length })}</span>
			</span>
		</button>
		<span class="stack-state"><span class="dot" class:ok={running === group.members.length} class:warn={running > 0 && running < group.members.length} aria-hidden="true"></span>{t('docker.stackSummary', { running, total: group.members.length })}</span>
		<span class="mono stack-value"><span class="label">{t('docker.cpu')}</span>{running ? formatPercent(sum(group, (container) => container.cpuPercent)) : '–'}</span>
		<span class="mono stack-value"><span class="label">{t('docker.ram')}</span>{running ? formatBytes(sum(group, (container) => container.memoryBytes)) : '–'}</span>
	</div>
{/snippet}

{#if dialog?.kind === 'logs'}
	{@const container = dialog.container}
	<Dialog title={t('docker.logsTitle', { name: dockerName(container, 'distinct') })} wide onclose={() => (dialog = null)}>
		<pre class="logs">{dialog.text === null ? '…' : dialog.text || t('docker.logsEmpty')}</pre>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => showLogs(container)}>{t('docker.logsRefresh')}</button>
			<button type="button" class="btn btn-primary" onclick={() => (dialog = null)}>{t('common.done')}</button>
		</div>
	</Dialog>
{:else if dialog?.kind === 'remove'}
	{@const container = dialog.container}
	<Dialog title={t('docker.removeTitle', { name: container.name })} onclose={() => (dialog = null)}>
		<p class="text">{t('docker.removeContainer')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (dialog = null)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-warn" onclick={() => remove(container)}>{t('docker.removeSubmit')}</button>
		</div>
	</Dialog>
{:else if dialog?.kind === 'removeApp'}
	{@const app = dialog.app}
	<Dialog title={t('docker.removeTitle', { name: app })} onclose={() => (dialog = null)}>
		{@const path = `/mnt/${status?.settings.volume}/appdata/${app}`}
		<p class="text">{t(dialog.purge ? 'docker.removeAppPurge' : 'docker.removeApp', { app, path })}</p>
		<label class="check"><input type="checkbox" bind:checked={dialog.purge} /><span>{t('docker.removeData', { path })}</span></label>
		{#if dialog.purge}
			<p class="notice-error">{t('docker.removeDataWarning')}</p>
		{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (dialog = null)}>{t('common.cancel')}</button>
			<button type="button" class="btn {dialog.purge ? 'btn-danger' : 'btn-warn'}"
				onclick={() => dialog?.kind === 'removeApp' && removeApp(app, dialog.purge)}>{t(dialog.purge ? 'docker.removeDataSubmit' : 'docker.removeSubmit')}</button>
		</div>
	</Dialog>
{:else if dialog}
	{@const kind = dialog.kind}
	<Dialog title={t(kind === 'import' ? 'docker.importTitle' : 'docker.settingsTitle')} wide={kind === 'import'} onclose={() => (dialog = null)}>
		<form onsubmit={submit}>
			{#if kind === 'import'}
				<p class="notice-warn">{t('docker.importWarning')}</p>
				<label><span>{t('docker.importName')}</span>
					<!-- svelte-ignore a11y_autofocus -->
					<input class="field mono name-field" type="text" bind:value={draft.name} maxlength="32" autocapitalize="none" spellcheck="false" required autofocus />
				</label>
				<label><span>{t('docker.importCompose')}</span>
					<textarea class="field mono" rows="14" bind:value={draft.compose} spellcheck="false" required placeholder={'services:\n  web:\n    image: …'}></textarea>
					<small>{t('docker.importHint', { path: `/mnt/${status?.settings.volume}/appdata/${draft.name || '<name>'}` })}</small>
				</label>
			{:else}
				<label class="check"><input type="checkbox" bind:checked={draft.enabled} /><span>{t('docker.enabled')}</span></label>
				<label><span>{t('docker.volume')}</span>
					<select class="field" bind:value={draft.volume} disabled={!draft.enabled}>{#each volumes as volume (volume)}<option value={volume}>{volume}</option>{/each}</select>
				</label>
				<p class="muted note">{t('docker.disableNote')}</p>
			{/if}
			{#if dialogError}
				<div class="notice-error" role="alert">{dialogError.text}{#if dialogError.detail}<pre>{dialogError.detail}</pre>{/if}</div>
			{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" onclick={() => (dialog = null)}>{t('common.cancel')}</button>
				<button type="submit" class="btn btn-primary">{t(kind === 'import' ? 'docker.importSubmit' : 'docker.save')}</button>
			</div>
		</form>
	</Dialog>
{/if}

<style>
	.summary {
		display: flex;
		flex-wrap: wrap;
		gap: 6px 18px;
		font-size: 0.875rem;
	}
	.summary span {
		display: inline-flex;
		align-items: center;
		gap: 6px;
	}
	.buttons {
		display: flex;
		flex-wrap: wrap;
		gap: 10px;
	}
	.job-actions {
		margin-top: 10px;
	}
	.link {
		padding: 0;
		background: none;
		border: 0;
		color: var(--accent);
		font: inherit;
		cursor: pointer;
	}
	.setup {
		display: flex;
		flex-direction: column;
		gap: 14px;
		padding: 24px;
		font-size: 0.875rem;
	}
	.setup h2 {
		font-size: 1.125rem;
		font-weight: 600;
	}
	.setup-row {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-end;
		gap: 12px;
	}
	.setup-row .field {
		min-width: 14rem;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: 0.875rem;
	}
	.controls {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 10px;
	}
	.search {
		flex: 1 1 240px;
		max-width: 360px;
	}
	.view-tools {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 10px;
		margin-left: auto;
	}
	.segmented {
		display: flex;
		min-width: 0;
		max-width: 100%;
		overflow-x: auto;
		scrollbar-width: thin;
		gap: 2px;
		padding: 3px;
		background: var(--panel-bg);
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
	}
	.segmented button {
		flex: none;
		display: inline-flex;
		align-items: center;
		gap: 7px;
		height: 1.875rem;
		padding: 0 10px;
		background: transparent;
		border: 0;
		border-radius: var(--radius-small);
		color: var(--muted);
		font: inherit;
		font-size: 0.8125rem;
		cursor: pointer;
	}
	.segmented button:hover:not(:disabled) {
		color: var(--text);
	}
	.segmented button[aria-pressed='true'] {
		background: var(--chip);
		color: var(--text);
		font-weight: 500;
	}
	.count {
		font-size: 0.75rem;
		opacity: 0.8;
	}
	.saved {
		font-size: 0.8125rem;
	}

	.layout-editor {
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding: var(--tile-pad);
		font-size: 0.875rem;
	}
	.order {
		display: flex;
		flex-direction: column;
		gap: 4px;
		max-height: 22rem;
		overflow-y: auto;
		margin: 0;
		padding: 0;
		list-style: none;
	}
	.order li {
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 4px 6px;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
	}
	.order li.dragging {
		border-style: dashed;
		border-color: var(--accent);
	}
	.order-name {
		flex: 1 1 auto;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		font-weight: 500;
	}
	.grip {
		padding: 0 8px;
		cursor: grab;
	}
	.arrow {
		width: 2.25rem;
		padding: 0;
	}
	.spacer {
		flex: 1 1 auto;
	}

	/* The list: one grid whose columns every row and stack takes over
	   (subgrid), so that values line up from top to bottom. */
	.list-wrap {
		container: docker-list / inline-size;
	}
	.list {
		display: grid;
		grid-template-columns: minmax(12rem, 2.4fr) minmax(8rem, auto) minmax(7rem, 1fr) minmax(5rem, auto) minmax(0, 1.3fr) auto;
		column-gap: 20px;
	}
	.head {
		grid-column: 1 / -1;
		display: grid;
		grid-template-columns: subgrid;
		padding: 10px var(--tile-pad) 8px;
		color: var(--muted);
		font-size: 0.6875rem;
		font-weight: 600;
		letter-spacing: 0.06em;
		text-transform: uppercase;
	}
	.stack,
	.members {
		grid-column: 1 / -1;
		display: grid;
		grid-template-columns: subgrid;
	}
	.stack-head {
		grid-column: 1 / -1;
		display: grid;
		grid-template-columns: subgrid;
		align-items: center;
		min-height: 3.5rem;
		padding: 8px var(--tile-pad);
		border-top: 1px solid var(--panel-border);
		font-size: 0.875rem;
	}
	.stack-card .stack-head {
		border-top: 0;
	}
	.stack-toggle {
		display: flex;
		align-items: center;
		gap: 12px;
		min-width: 0;
		padding: 0;
		background: transparent;
		border: 0;
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.chevron {
		display: inline-flex;
		color: var(--muted);
		transition: transform 0.15s ease;
	}
	.chevron.folded {
		transform: rotate(-90deg);
	}
	.stack-icon {
		width: 32px;
		height: 32px;
		flex: none;
		overflow: hidden;
		border-radius: var(--radius-ctl);
		color: var(--on-monogram);
		font-size: 0.8125rem;
		font-weight: 600;
	}
	.stack-names {
		display: flex;
		flex-direction: column;
		gap: 1px;
		min-width: 0;
	}
	.stack-line {
		display: flex;
		align-items: center;
		gap: 8px;
		min-width: 0;
	}
	.stack-name {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		font-weight: 600;
	}
	.stack-line .chip {
		flex: none;
		font-size: 0.6875rem;
	}
	.stack-sub {
		color: var(--muted);
		font-size: 0.75rem;
	}
	.stack-state {
		display: flex;
		align-items: center;
		gap: 6px;
		white-space: nowrap;
	}
	.stack-value {
		font-size: 0.8125rem;
	}
	.stack-value .label {
		position: absolute;
		width: 1px;
		height: 1px;
		overflow: hidden;
		clip-path: inset(50%);
		white-space: nowrap;
	}
	.list .stack-head:hover {
		background: color-mix(in srgb, var(--chip) 45%, transparent);
	}

	.cards {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(17rem, 1fr));
		gap: var(--gap);
	}
	.stack-card {
		grid-column: 1 / -1;
		position: relative;
		padding-bottom: 4px;
	}
	.stack-card:has(:global(.menu-open)) {
		z-index: 30;
	}

	@media (prefers-reduced-motion: reduce) {
		.chevron {
			transition: none;
		}
	}
	@container docker-list (max-width: 760px) {
		.list,
		.stack,
		.members {
			display: block;
		}
		.head {
			display: none;
		}
		.stack-head {
			display: flex;
			flex-wrap: wrap;
			gap: 6px 14px;
			padding: 10px 12px;
		}
		.stack-toggle {
			flex: 1 1 100%;
		}
		.stack-value .label {
			position: static;
			width: auto;
			height: auto;
			clip-path: none;
			margin-right: 6px;
			color: var(--muted);
			font-family: var(--font-sans);
			font-size: 0.75rem;
		}
	}
	@media (max-width: 760px) {
		.search {
			max-width: none;
		}
		.view-tools {
			margin-left: 0;
		}
		.segmented button {
			height: 2.5rem;
		}
	}

	form {
		display: flex;
		flex-direction: column;
		gap: 16px;
	}
	.name-field {
		max-width: 18rem;
	}
	textarea {
		height: auto;
		padding: 0.625rem 0.75rem;
		font-size: 0.8125rem;
		line-height: 1.5;
		resize: vertical;
	}
	small,
	.note {
		font-size: 0.75rem;
		color: var(--muted);
	}
	.check {
		flex-direction: row;
		align-items: center;
		gap: 10px;
	}
	.check input {
		width: 18px;
		height: 18px;
		margin: 0;
	}
	.text {
		font-size: 0.875rem;
	}
	pre {
		margin: 8px 0 0;
		font-family: var(--font-mono);
		font-size: 0.75rem;
		white-space: pre-wrap;
		word-break: break-word;
	}
	.logs {
		max-height: 60vh;
		margin: 0;
		padding: 12px 14px;
		overflow: auto;
		background: var(--bg);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		line-height: 1.5;
	}
</style>
