<script lang="ts">
	import { api, ApiError, type DockerContainer, type DockerStatus } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { formatBytes, formatPercent } from '#lib/format.ts';
	import { errorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import { storage } from '#lib/storage.svelte.ts';
	import { system } from '#lib/system.svelte.ts';
	import ConsoleDialog from './ConsoleDialog.svelte';
	import ContainerCard from './ContainerCard.svelte';

	const NAME = /^[a-z0-9][a-z0-9_-]{0,31}$/;

	let status = $state<DockerStatus | null>(null);
	let error = $state<{ text: string; detail: string } | null>(null);
	let busy = $state(false);
	let search = $state('');
	let filter = $state<'all' | 'running' | 'stopped' | 'updates'>('all');
	let collapsed = $state<string[]>([]);
	let dialog = $state<
		| { kind: 'import' | 'settings' }
		| { kind: 'logs'; container: DockerContainer; text: string | null }
		| { kind: 'remove' | 'console'; container: DockerContainer }
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
	// Containers of one Compose project with several services form a stack.
	const stacks = $derived.by(() => {
		const sizes = new Map<string, number>();
		for (const container of containers) if (container.app) sizes.set(container.app, (sizes.get(container.app) ?? 0) + 1);
		const names = [...new Set(visible.filter((container) => (sizes.get(container.app) ?? 0) > 1).map((container) => container.app))];
		return names.map((name) => ({ name, members: visible.filter((container) => container.app === name), all: containers.filter((container) => container.app === name) }));
	});
	const singles = $derived(visible.filter((container) => !stacks.some((stack) => stack.name === container.app)));
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
		</div>
	{/each}

	{#if containers.length > 0}
		<div class="controls">
			<input class="field search" type="search" bind:value={search} placeholder={t('docker.search')} aria-label={t('docker.search')} />
			<div class="filters" role="group">
				{#each [['all', containers.length], ['running', runCount], ['stopped', containers.length - runCount], ['updates', updateCount]] as [id, count] (id)}
					<button type="button" aria-pressed={filter === id} onclick={() => (filter = id as typeof filter)}>
						{t(id === 'all' ? 'docker.filterAll' : id === 'running' ? 'docker.filterRunning' : id === 'stopped' ? 'docker.filterStopped' : 'docker.filterUpdates')}<span class="mono">{count}</span>
					</button>
				{/each}
			</div>
		</div>
	{/if}

	{#each stacks as stack (stack.name)}
		{@const openStack = !collapsed.includes(stack.name)}
		<section class="tile stack" aria-label="{t('docker.stack')} {stack.name}">
			<button type="button" class="stack-head" aria-expanded={openStack} onclick={() => (collapsed = openStack ? [...collapsed, stack.name] : collapsed.filter((name) => name !== stack.name))}>
				<span class="stack-name">{stack.name} <span class="chip">{t('docker.stack')}</span></span>
				<span class="muted">{t('docker.stackSummary', { running: stack.all.filter(isRunning).length, total: stack.all.length })}</span>
			</button>
			{#if openStack}
				{#each stack.members as container (container.id)}
					<ContainerCard {container} {host} {busy} compact onaction={(action) => run(() => api.post(`/docker/containers/${container.id}/${action}`))} onlogs={() => showLogs(container)} onremove={() => (dialog = { kind: 'remove', container })} onconsole={() => (dialog = { kind: 'console', container })} onupdate={() => run(() => api.post(`/docker/apps/${container.app}/update`))} />
				{/each}
			{/if}
		</section>
	{/each}

	<div class="cards">
		{#each singles as container (container.id)}
			<ContainerCard {container} {host} {busy} onaction={(action) => run(() => api.post(`/docker/containers/${container.id}/${action}`))} onlogs={() => showLogs(container)} onremove={() => (dialog = { kind: 'remove', container })} onconsole={() => (dialog = { kind: 'console', container })} onupdate={() => run(() => api.post(`/docker/apps/${container.app}/update`))} />
		{/each}
	</div>
	{#if containers.length === 0 && jobs.length === 0}
		<p class="tile empty">{t('docker.empty')}</p>
	{:else if visible.length === 0 && containers.length > 0}
		<p class="muted">{t('docker.noMatch')}</p>
	{/if}
{/if}

{#if dialog?.kind === 'logs'}
	{@const container = dialog.container}
	<Dialog title={t('docker.logsTitle', { name: container.name })} wide onclose={() => (dialog = null)}>
		<pre class="logs">{dialog.text === null ? '…' : dialog.text || t('docker.logsEmpty')}</pre>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => showLogs(container)}>{t('docker.logsRefresh')}</button>
			<button type="button" class="btn btn-primary" onclick={() => (dialog = null)}>{t('common.done')}</button>
		</div>
	</Dialog>
{:else if dialog?.kind === 'console'}
	<ConsoleDialog container={dialog.container} onclose={() => (dialog = null)} />
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
		font-size: 1.5rem;
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
		gap: 4px;
		padding: 3px;
		background: var(--panel-bg);
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
	}
	.filters button {
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
	.cards {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
		gap: var(--gap);
	}
	.stack {
		display: flex;
		flex-direction: column;
	}
	.stack-head {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
		padding: 14px 16px;
		background: transparent;
		border: 0;
		color: var(--text);
		font: inherit;
		font-size: 0.875rem;
		text-align: left;
		cursor: pointer;
	}
	.stack-name {
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
