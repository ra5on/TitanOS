<script lang="ts">
	import { session } from '#lib/session.svelte.ts';
	import { defaultDockerLayout, sanitizeDockerLayout, dockerGroups, orderedDockerGroups, moveDockerGroup, type DockerView, type DockerWidth } from '#lib/docker-layout.ts';
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
 import ContainerCard from './ContainerCard.svelte';

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
	let expanded = $state<Record<string, boolean>>({});
	let layout = $state(sanitizeDockerLayout(session.docker));
	let savedLayout = $state(sanitizeDockerLayout(session.docker));
	let editing = $state(false);
	let savingLayout = $state(false);
	let layoutError = $state('');
	let layoutSaved = $state(false);
	let dragged = $state<string | null>(null);
	function startEditing() { editing = true; layoutSaved = false; layoutError = ''; search = ''; filter = 'all'; }
	function setView(view: DockerView) { startEditing(); layout.view = view; expanded = {}; }
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
		| { kind: 'removeApp'; app: string }
		| null
	>(null);
	let draft = $state({ name: '', compose: '', enabled: true, volume: '' });
	let dialogError = $state<{ text: string; detail: string } | null>(null);

	const volumes = $derived((storage.status?.volumes ?? []).map((volume) => volume.name));
	const host = $derived(system.info?.network?.ipv4.split('/')[0] || location.hostname);
	const containers = $derived(status?.containers ?? []);
	const isRunning = (container: DockerContainer) => container.state === 'running' || container.state === 'restarting';
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
	const updateCount = $derived(containers.filter((container) => container.updateAvailable).length);
	const updatable = $derived(containers.some((container) => container.updateAvailable && container.managed));
	const checked = $derived.by(() => {
		const time = new Date(status?.updatesChecked ?? '').getTime();
		if (!time || time < 0) return t('docker.updatesNever');
		return t('docker.updatesChecked', { time: new Intl.DateTimeFormat(i18n.locale, { dateStyle: 'short', timeStyle: 'short' }).format(time) });
	});
	const jobs = $derived(Object.entries(status?.jobs ?? {}).filter(([, job]) => job.state !== 'done'));

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
		run(() => api.delete(container.managed ? `/docker/apps/${container.app}` : `/docker/containers/${container.id}`));
	}

	function removeApp(app: string) {
		dialog = null;
		run(() => api.delete(`/docker/apps/${app}`));
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

<div class="head">
	<div class="title">
		<h1>{t('docker.title')}</h1>
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
	</div>
	{#if status?.settings.enabled}
		<div class="buttons">
			<button type="button" class="btn" onclick={startEditing} disabled={editing || !status.running || containers.length === 0}><Icon name="edit" />{t('docker.layout.edit')}</button>
			<button type="button" class="btn" onclick={() => open('settings')}>{t('docker.settings')}</button>
			<button type="button" class="btn" onclick={() => open('import')} disabled={!status.running}>{t('docker.import')}</button>
			{#if updatable}
				<button type="button" class="btn" onclick={() => run(() => api.post('/docker/updates/apply'))} disabled={busy}>{t('docker.updateAll')}</button>
			{/if}
			<a class="btn btn-primary" href="/apps">{t('docker.installApp')}</a>
		</div>
	{/if}
</div>

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
					<button type="button" class="btn" disabled={busy} onclick={() => (dialog = { kind: 'removeApp', app })}>{t('docker.remove')}</button>
					<button type="button" class="btn" disabled={busy} onclick={() => run(() => api.delete(`/docker/apps/${app}/job`))}>{t('docker.jobDismiss')}</button>
				</div>
			{/if}
		</div>
	{/each}

	{#if containers.length > 0 || editing}
		<div class="layout-toolbar">
			<label class="view-picker"><span>{t('docker.layout.view')}</span><select class="field" value={layout.view} disabled={savingLayout} onchange={(event) => setView(event.currentTarget.value as DockerView)}>
				<option value="cards">{t('docker.layout.cards')}</option><option value="list">{t('docker.layout.list')}</option><option value="minimal">{t('docker.layout.minimal')}</option>
			</select></label>
			{#if layoutSaved}<span class="muted" role="status">{t('docker.layout.saved')}</span>{/if}
		</div>
		{#if editing}
			<div class="tile layout-editor">
				<p>{t('docker.layout.hint')}</p>
				<div class="buttons">
					<label class="check"><input type="checkbox" bind:checked={layout.details} disabled={savingLayout} />{t('docker.layout.details')}</label>
					<button class="btn" disabled={savingLayout} onclick={() => { layout = defaultDockerLayout(); expanded = {}; }}>{t('dashboard.restore')}</button>
					<button class="btn" disabled={savingLayout} onclick={cancelLayout}>{t('common.cancel')}</button>
					<button class="btn btn-primary" disabled={savingLayout} onclick={saveLayout}>{t('docker.layout.save')}</button>
				</div>
				{#if layoutError}<p class="notice-error" role="alert">{layoutError}</p>{/if}
			</div>
		{/if}
		<div class="controls">
			<input class="field search" type="search" disabled={editing} bind:value={search} placeholder={t('docker.search')} aria-label={t('docker.search')} />
			<div class="filters" role="group">
				{#each [['all', containers.length], ['running', runCount], ['stopped', containers.length - runCount], ['updates', updateCount]] as [id, count] (id)}
					<button type="button" aria-pressed={filter === id} disabled={editing} onclick={() => (filter = id as typeof filter)}>
						{t(id === 'all' ? 'docker.filterAll' : id === 'running' ? 'docker.filterRunning' : id === 'stopped' ? 'docker.filterStopped' : 'docker.filterUpdates')}<span class="mono">{count}</span>
					</button>
				{/each}
			</div>
		</div>
	{/if}

	<div class="cards" class:editing class:list={layout.view === 'list'}>
		{#each shownGroups as group (group.id)}
			{@const stack = group.members.length > 1}
			{@const openStack = expanded[group.id] ?? layout.view !== 'minimal'}
			<div class="layout-item" style:--columns={layout.widths[group.id] ?? (layout.view === 'minimal' ? 3 : stack ? 8 : 4)}>
				{#if editing}
					<div class="arrange">
						<button class="btn grip" draggable={!savingLayout} disabled={savingLayout} aria-label={t('dashboard.move', { tile: group.name })}
							ondragstart={(event) => { dragged = group.id; event.dataTransfer?.setData('text/plain', group.id); }} ondragend={() => (dragged = null)}
							ondragover={(event) => { if (dragged && !savingLayout) event.preventDefault(); }}
							ondrop={(event) => { event.preventDefault(); if (dragged && !savingLayout) move(dragged, group.id); dragged = null; }}><Icon name="grip" /></button>
						<button class="btn" disabled={savingLayout || groups[0]?.id === group.id} aria-label={t('docker.layout.before', { name: group.name })} onclick={() => moveBy(group.id, -1)}>←</button>
						<button class="btn" disabled={savingLayout || groups.at(-1)?.id === group.id} aria-label={t('docker.layout.after', { name: group.name })} onclick={() => moveBy(group.id, 1)}>→</button>
						<select class="field" disabled={savingLayout || layout.view === 'list'} aria-label={t('docker.layout.width', { name: group.name })}
							value={layout.widths[group.id] ?? (layout.view === 'minimal' ? 3 : stack ? 8 : 4)} onchange={(event) => (layout.widths[group.id] = Number(event.currentTarget.value) as DockerWidth)}>
							<option value="3">{t('docker.layout.narrow')}</option><option value="4">{t('docker.layout.normal')}</option><option value="8">{t('docker.layout.wide')}</option>
						</select>
					</div>
				{/if}
				{#if stack}
					<section class="tile stack" aria-label="{t('docker.stack')} {group.name}">
						<button type="button" class="stack-head" aria-expanded={openStack} onclick={() => (expanded[group.id] = !openStack)}>
							<span class="stack-name"><span aria-hidden="true">{openStack ? '▾' : '▸'}</span>{group.name} <span class="chip">{t('docker.stack')}</span></span>
							<span class="muted">{t('docker.stackSummary', { running: group.members.filter(isRunning).length, total: group.members.length })}</span>
							{#if group.members.some(container => container.updateAvailable)}<span class="chip warn">{t('docker.update')}</span>{/if}
						</button>
						{#if openStack}
							{#each group.members.filter(container => visibleIDs.has(container.id)) as container (container.name)}
								{@render card(container, true)}
							{/each}
						{/if}
					</section>
				{:else}
					{@render card(group.members[0], false)}
				{/if}
			</div>
		{/each}
	</div>
	{#if containers.length === 0 && jobs.length === 0}
		<p class="tile empty">{t('docker.empty')}</p>
	{:else if visible.length === 0 && containers.length > 0}
		<p class="muted">{t('docker.noMatch')}</p>
	{/if}
{/if}

{#snippet card(container: DockerContainer, compact: boolean)}
	<ContainerCard {container} {host} {tailscaleHost} {busy} {compact} view={layout.view} details={layout.details}
		onaction={(action) => run(() => api.post(`/docker/containers/${container.id}/${action}`))}
		onlogs={() => showLogs(container)} onremove={() => (dialog = { kind: 'remove', container })}
		onconsole={() => addContainerConsole(container.id, dockerName(container, 'distinct'))}
		onupdate={() => run(() => api.post(`/docker/apps/${container.app}/update`))} />
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
	<Dialog title={t('docker.removeTitle', { name: container.managed ? container.app : container.name })} onclose={() => (dialog = null)}>
		<p class="text">
			{container.managed
				? t('docker.removeApp', { app: container.app, path: `/mnt/${status?.settings.volume}/appdata/${container.app}` })
				: t('docker.removeContainer')}
		</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (dialog = null)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-warn" onclick={() => remove(container)}>{t('docker.removeSubmit')}</button>
		</div>
	</Dialog>
{:else if dialog?.kind === 'removeApp'}
	{@const app = dialog.app}
	<Dialog title={t('docker.removeTitle', { name: app })} onclose={() => (dialog = null)}>
		<p class="text">{t('docker.removeApp', { app, path: `/mnt/${status?.settings.volume}/appdata/${app}` })}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (dialog = null)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-warn" onclick={() => removeApp(app)}>{t('docker.removeSubmit')}</button>
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
	.head {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-end;
		justify-content: space-between;
		gap: 12px;
	}
	.title {
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
	h1 {
		font-size: var(--text-page);
		font-weight: 600;
	}
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
		gap: 12px;
	}
	.search {
		flex: 1 1 260px;
		max-width: 420px;
	}
	.filters {
		display: flex;
 min-width: 0;
 max-width: 100%;
 overflow-x: auto;
 scrollbar-width: thin;
		gap: 4px;
		padding: 3px;
		background: var(--panel-bg);
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
	}
	.filters button {
 flex: none;
		display: inline-flex;
		align-items: center;
		gap: 8px;
		height: 2.25rem;
		padding: 0 12px;
		background: transparent;
		border: 0;
		border-radius: var(--radius-ctl);
		color: var(--muted);
		font: inherit;
		font-size: 0.8125rem;
		cursor: pointer;
	}
	.filters button[aria-pressed='true'] {
		background: var(--chip);
		color: var(--text);
		font-weight: 500;
	}
	.cards { display: grid; grid-template-columns: repeat(12, minmax(0, 1fr)); gap: var(--gap); align-items: start; }
	.layout-item { grid-column: span var(--columns); min-width: 0; container-type: inline-size; }
	.layout-item:focus-within, .layout-item:has(:global(.menu-open)) { z-index: 30; }
	.list .layout-item { grid-column: 1 / -1; }
	.editing .layout-item { outline: 1px dashed var(--accent); border-radius: var(--radius-tile); }
	.arrange, .layout-toolbar { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; }
	.arrange { padding: 8px; }
	.arrange .btn { padding: 0 8px; }
	.arrange select { flex: 1; min-width: 6rem; width: auto; }
	.grip { cursor: grab; }
	.layout-editor { padding: 16px; display: flex; flex-direction: column; gap: 12px; }
	.layout-editor p { font-size: .875rem; }
	.view-picker { flex-direction: row; align-items: center; }
	.layout-toolbar { justify-content: space-between; }
	@media (max-width: 1100px) { .layout-item { grid-column: span 6; } }
	@media (max-width: 760px) { .layout-item { grid-column: 1 / -1; } }

	.stack {
		display: flex;
		flex-direction: column;
	}
	.stack:has(:global(.menu-open)) { position: relative; z-index: 20; }
	.stack-head {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
		padding: calc(var(--row-pad) + 2px) var(--tile-pad);
		background: transparent;
		border: 0;
		color: var(--text);
		font: inherit;
		font-size: 0.875rem;
		text-align: left;
		cursor: pointer;
	}
	.stack-name {
		overflow-wrap: anywhere;
		flex-wrap: wrap;
		display: inline-flex;
		align-items: center;
		gap: 10px;
		font-size: 1rem;
		font-weight: 600;
	}
	.empty {
		padding: 28px 22px;
		font-size: 0.875rem;
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
