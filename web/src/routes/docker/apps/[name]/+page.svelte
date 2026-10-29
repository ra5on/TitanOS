<script lang="ts">
	import { beforeNavigate, goto } from '$app/navigation';
	import { onDestroy } from 'svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { dockerName } from '#lib/docker-name.ts';
	import { publicURL } from '#lib/app-links.ts';
	import HardwarePicker from '#lib/components/HardwarePicker.svelte';
	import ImageField from '#lib/components/ImageField.svelte';
import LANAssignment from '#lib/components/LANAssignment.svelte';
	import { parseDocument, type Document } from 'yaml';
	import { page } from '$app/state';
	import { api, ApiError, type AppProxyStatus, type DockerContainer, type DockerStatus } from '#lib/api.ts';
	import Icon from '#lib/components/Icon.svelte';
	import { stackPrimary, sanitizeDockerLayout } from '#lib/docker-layout.ts';
	import { formatBytes, formatPercent } from '#lib/format.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { system } from '#lib/system.svelte.ts';
	import { addContainerConsole } from '#lib/terminal.svelte.ts';
	import { isRunning, webLinks } from '../../container.ts';
	import AppProxy from './AppProxy.svelte';
	import TailnetDialog from '../../TailnetDialog.svelte';
	import { inTailnet, tailnetPort } from '#lib/app-proxy.ts';
	import AppIpv64 from './AppIpv64.svelte';
	import {
		check,
		NETWORKS,
		readService,
		RESTART_POLICIES,
		serviceNames,
		writeService,
		type ServiceForm,
	} from '#lib/compose/service.ts';

	const name = $derived(page.params.name ?? '');

	// The compose file as text is the single truth. The form is a view of
	// one service in it: every change in the form is written into the text,
	// and the text is read again when the form is shown.
	let original = $state('');
	let text = $state('');
	let view = $state<'form' | 'compose'>('form');
	let services = $state<string[]>([]);
	let service = $state('');
	let form = $state<ServiceForm | null>(null);
	let yamlError = $state('');
	let status = $state<DockerStatus | null>(null);
	let loadError = $state('');
	let loaded = $state(false);
	let applying = $state(false);
	let result = $state<{ ok: boolean; text: string; detail: string } | null>(null);

	let doc: Document | undefined;

	/** Reads the text into the form. Returns false if it is not valid YAML. */
	function parse(): boolean {
		const parsed = parseDocument(text, { merge: true });
		if (parsed.errors.length > 0) {
			yamlError = parsed.errors[0].message;
			return false;
		}
		yamlError = '';
		doc = parsed;
		services = serviceNames(parsed);
		if (!services.includes(service)) service = services[0] ?? '';
		form = service ? readService(parsed, service) : null;
		return true;
	}

	/** Writes the form into the text. Called after every change in the form. */
	function commit() {
		if (!doc || !form || !service) return;
		writeService(doc, service, $state.snapshot(form));
		text = doc.toString();
	}

	function show(next: 'form' | 'compose') {
		if (next === 'form' && !parse()) return; // stay in the text until it is valid
		view = next;
	}

	function pickService(next: string) {
		service = next;
		if (doc) form = readService(doc, next);
	}

	function loadCompose(app = name) {
		loadError = '';
		api.get<{ compose: string }>(`/docker/apps/${app}`).then(
			(res) => {
				if (app !== name) return;
				original = text = res.compose;
				loaded = true;
				if (!parse()) view = 'compose';
			},
			(err) => {
				if (app === name) loadError = errorText(err instanceof ApiError ? err.code : 'internal');
			},
		);
	}
	let statusError = $state('');
	async function loadStatus() {
		if (document.hidden || applying) return;
		try {
			status = await api.get<DockerStatus>('/docker');
			statusError = '';
		} catch (err) {
			statusError = errorText(err instanceof ApiError ? err.code : 'internal');
		}
	}
	$effect(() => {
		loadCompose(name);
		void loadStatus();
		const timer = setInterval(loadStatus, 5000);
		return () => clearInterval(timer);
	});

	// The head of the page: state and load of the app's containers, with
	// the actions of the Docker page for the container of the chosen service.
	const members = $derived((status?.containers ?? []).filter((container) => container.app === name));
	const runningCount = $derived(members.filter(isRunning).length);
	const primary = $derived(
		stackPrimary({ id: `app:${name}`, name, members }, sanitizeDockerLayout(session.docker).primaryServices),
	);
	const target = $derived(
		members.find((container) => container.service === service) ?? primary ?? members[0],
	);
	const host = $derived(system.info?.network?.ipv4.split('/')[0] || location.hostname);
	const quickLinks = $derived(
		primary ? webLinks(primary, host, '').filter((link) => link.icon !== 'external') : [],
	);
	// "Im Tailnet veröffentlichen": the app's own device in the tailnet.
	let proxy = $state<AppProxyStatus | null>(null);
	let tailnetDialog = $state<'publish' | 'remove' | null>(null);
	// Counts changes, so that the HTTPS name section below reads them.
	let proxyVersion = $state(0);
	const published = $derived(inTailnet(proxy?.apps?.find((route) => route.app === name)));
	async function loadProxy() {
		try {
			proxy = await api.get<AppProxyStatus>('/docker/proxy');
		} catch {
			proxy = null;
		}
	}
	$effect(() => {
		void name;
		void loadProxy();
	});
	let restarting = $state(false),
		actionError = $state('');
	async function restartAll() {
		if (restarting || members.length === 0) return;
		restarting = true;
		actionError = '';
		try {
			for (const container of members)
				await api.post(`/docker/containers/${encodeURIComponent(container.id)}/restart`);
		} catch (err) {
			actionError = errorText(err instanceof ApiError ? err.code : 'internal');
		} finally {
			restarting = false;
			await loadStatus();
		}
	}
	let logs = $state<{ container: DockerContainer; text: string | null; error: string } | null>(null);
	let logsRequest = 0;
	async function showLogs(container: DockerContainer) {
		const request = ++logsRequest;
		if (logs?.container.id !== container.id) logs = { container, text: null, error: '' };
		else logs.error = '';
		try {
			const res = await api.get<{ logs: string }>(`/docker/containers/${encodeURIComponent(container.id)}/logs?tail=300`);
			if (request === logsRequest && logs?.container.id === container.id) logs.text = res.logs;
		} catch (err) {
			if (request === logsRequest && logs?.container.id === container.id)
				logs.error = errorText(err instanceof ApiError ? err.code : 'internal');
		}
	}
	let confirmDiscard = $state(false);
	function discardChanges() {
		confirmDiscard = false;
		text = original;
		result = null;
		if (!parse()) view = 'compose';
	}

	// Ports other containers already publish, for the live check.
	const taken = $derived.by(() => {
		const map = new Map<string, string>();
		for (const container of status?.containers ?? []) {
			if (container.app === name) continue;
			for (const port of container.ports)
				map.set(`${port.host}/${port.protocol}`, dockerName(container, 'distinct'));
		}
		return map;
	});
	const problems = $derived(form ? check(form, taken, t) : null);
	const changed = $derived(text !== original);
	let leaving = $state(false),
		pendingURL = '',
		allowNavigation = false;
	beforeNavigate((navigation) => {
		if (!changed || applying || allowNavigation) return;
		navigation.cancel();
		if (!navigation.willUnload) {
			pendingURL = navigation.to?.url.href ?? '';
			leaving = true;
		}
	});
	function discard() {
		allowNavigation = true;
		leaving = false;
		if (pendingURL) void goto(pendingURL);
	}

	const blocked = $derived(applying || !changed || (view === 'form' ? !!problems?.any : false));

	// Leaving the page ends the wait for the outcome of an apply.
	let destroyed = false;
	onDestroy(() => (destroyed = true));

	async function apply() {
		if (view === 'compose' && !parse()) return;
		applying = true;
		result = null;
		try {
			await api.put(`/docker/apps/${name}`, { compose: text });
			// The containers are recreated in the background; wait for the outcome.
			for (;;) {
				await new Promise((resolve) => setTimeout(resolve, 1500));
				if (destroyed) return;
				const next = await api.get<DockerStatus>('/docker');
				if (destroyed) return;
				status = next;
				const job = status.jobs[name];
				if (job?.state === 'running') continue;
				if (!job) {
					// The NAS registers the job before it answers; a missing job
					// means it is gone (titand restarted), so its outcome is unknown.
					result = { ok: false, text: t('editor.jobLost'), detail: '' };
					break;
				}
				if (job.state === 'done') {
					original = text;
					result = { ok: true, text: t('editor.applied'), detail: '' };
					if (pendingURL) {
						allowNavigation = true;
						leaving = false;
						void goto(pendingURL);
					}
				} else {
					result = { ok: false, text: t('editor.failed'), detail: job.output };
				}
				break;
			}
		} catch (err) {
			const code = err instanceof ApiError ? err.code : 'internal';
			result = {
				ok: false,
				text: errorText(code),
				detail:
					code === 'invalid_compose' || code === 'docker_failed' ? (err as ApiError).message : '',
			};
		}
		applying = false;
	}
</script>

<PageHead
	title={t('editor.title', { name })}
	parent={{ href: '/docker', label: t('docker.title') }}
>
	<p class="muted mono location">
		{t('editor.location', { path: `/config/apps/${name}/compose.yaml` })}
	</p>
	{#snippet actions()}
		<div class="segments" role="radiogroup">
			<button type="button" role="radio" aria-checked={view === 'form'} onclick={() => show('form')}
				>{t('editor.form')}</button
			>
			<button
				type="button"
				role="radio"
				aria-checked={view === 'compose'}
				onclick={() => show('compose')}>{t('editor.compose')}</button
			>
		</div>
	{/snippet}
</PageHead>

<section class="tile app-head" aria-label={t('editor.appState')}>
	<div class="app-state">
		{#if members.length}
			<span class="state"
				><span
					class="dot"
					class:ok={runningCount === members.length}
					class:warn={runningCount > 0 && runningCount < members.length}
					aria-hidden="true"
				></span>{t('docker.stackSummary', { running: runningCount, total: members.length })}</span
			>
			{#if runningCount}
				<span class="mono value"
					><span class="muted">{t('docker.cpu')}</span>
					{formatPercent(members.filter(isRunning).reduce((sum, container) => sum + container.cpuPercent, 0))}</span
				>
				<span class="mono value"
					><span class="muted">{t('docker.ram')}</span>
					{formatBytes(members.filter(isRunning).reduce((sum, container) => sum + container.memoryBytes, 0))}</span
				>
			{/if}
		{:else if status}
			<span class="muted">{t('editor.noContainers')}</span>
		{:else if !statusError}
			<span class="muted" role="status">{t('common.loading')}</span>
		{/if}
	</div>
	<div class="quick">
		{#each quickLinks as link (link.label)}<a class="btn small" href={link.href} target="_blank" rel="noreferrer" title={link.label}
				><Icon name={link.icon} size={14} />{link.text}</a
			>{/each}
		<button type="button" class="btn small" disabled={!target} onclick={() => target && showLogs(target)}
			><Icon name="fileText" size={14} />{t('docker.logs')}</button
		>
		<button
			type="button"
			class="btn small"
			disabled={!target || !isRunning(target)}
			onclick={() => target && addContainerConsole(target.id, dockerName(target, 'distinct'))}
			><Icon name="terminal" size={14} />{t('docker.console')}</button
		>
		<button type="button" class="btn small" disabled={!members.length || restarting || applying} onclick={restartAll}
			><Icon name="restart" size={14} />{t(restarting ? 'editor.restarting' : 'docker.restart')}</button
		>
		{#if proxy}<button type="button" class="btn small" onclick={() => (tailnetDialog = published ? 'remove' : 'publish')}
				><Icon name="tailnet" size={14} />{t(published ? 'docker.tailnetUnpublish' : 'docker.tailnetPublish')}</button
			>{/if}
	</div>
	{#if statusError}<div class="notice-warn row-note" role="alert">
			<span>{statusError}</span><button type="button" class="btn small" onclick={loadStatus}
				>{t('common.retry')}</button
			>
		</div>{/if}
	{#if actionError}<div class="notice-error row-note" role="alert">
			<span>{actionError}</span><button type="button" class="btn small" disabled={restarting} onclick={restartAll}
				>{t('common.retry')}</button
			>
		</div>{/if}
</section>

{#if loadError}<div class="notice-error row-note" role="alert">
		<span>{loadError}</span><button type="button" class="btn small" onclick={() => loadCompose()}
			>{t('common.retry')}</button
		>
	</div>
{:else if !loaded}<p class="muted loading" role="status">{t('common.loading')}</p>{/if}
{#if yamlError}<div class="notice-error" role="alert">
		{t('editor.yamlError')}
		<pre>{yamlError}</pre>
	</div>{/if}

{#if view === 'compose'}
	<textarea
		class="field mono yaml"
		bind:value={text}
		spellcheck="false"
		aria-label={t('editor.compose')}></textarea>
{:else if form && problems}
	{#if services.length > 1}
		<div class="segments services" role="radiogroup" aria-label={t('editor.service')}>
			{#each services as entry (entry)}
				<button
					type="button"
					role="radio"
					aria-checked={service === entry}
					onclick={() => pickService(entry)}>{entry}</button
				>
			{/each}
		</div>
	{/if}

	<section class="tile" aria-labelledby="h-general">
		<h2 class="tile-title" id="h-general">{t('editor.general')}</h2>
		<div class="grid">
			<ImageField bind:image={form.image} onchange={commit} />
			<label
				><span>{t('editor.restart')}</span>
				<select class="field" bind:value={form.restart} onchange={commit}
					>{#each RESTART_POLICIES as policy (policy)}<option value={policy}
							>{t(`editor.restart.${policy}` as 'editor.restart.no')}</option
						>{/each}{#if !RESTART_POLICIES.includes(form.restart)}<option value={form.restart}
							>{form.restart}</option
						>{/if}</select
				>
			</label>
			<label
				><span>{t('editor.network')}</span>
				<select class="field" bind:value={form.network} onchange={commit}
					>{#each NETWORKS as mode (mode)}<option value={mode}
							>{t(`editor.network.${mode}` as 'editor.network.host')}</option
						>{/each}{#if !NETWORKS.includes(form.network)}<option value={form.network}
							>{form.network}</option
						>{/if}</select
				>
			</label>
			<label
				><span>{t('editor.publicUrl')}</span><input
					class="field mono"
					type="url"
					bind:value={form.publicUrl}
					oninput={commit}
					placeholder="https://app.example.org"
					spellcheck="false"
				/><small>{t('editor.publicUrlHint')}</small
				>{#if form.publicUrl.trim() && !publicURL(form.publicUrl)}<small
						class="notice-error"
						role="alert">{t('editor.publicUrlInvalid')}</small
					>{/if}</label
			>
			<label
				><span>{t('editor.webui')}</span><input
					class="field mono"
					type="text"
					bind:value={form.webui}
					oninput={commit}
					placeholder="8096"
					spellcheck="false"
				/><small>{t('editor.webuiHint')}</small></label
			>
		</div>
		<label class="check"
			><input type="checkbox" bind:checked={form.privileged} onchange={commit} /><span
				>{t('editor.privileged')}</span
			></label
		>
		{#if form.privileged}<p class="notice-warn" role="alert">
				{t('editor.privilegedWarning')}
			</p>{/if}
	</section>

	<section class="tile"><HardwarePicker bind:selected={form.hardware} onchange={commit} /></section>
	<section class="tile" aria-labelledby="h-paths">
		<div class="section-head">
			<h2 class="tile-title" id="h-paths">{t('editor.paths')}</h2>
			<button
				type="button"
				class="btn small"
				onclick={() => (
					form!.paths.push({ host: './data', container: '/data', readOnly: false }),
					commit()
				)}>{t('editor.addPath')}</button
			>
		</div>
		{#each form.paths as row, index (index)}
			<div class="row">
				{#if row.raw !== undefined}
					<span class="mono raw"
						>{typeof row.raw === 'string' ? row.raw : JSON.stringify(row.raw)}</span
					><span class="muted small-text">{t('editor.rawEntry')}</span>
				{:else}
					<label class="grow"
						><span>{t('editor.hostPath')}</span><input
							class="field mono"
							type="text"
							bind:value={row.host}
							oninput={commit}
							spellcheck="false"
						/></label
					>
					<label class="grow"
						><span>{t('editor.containerPath')}</span><input
							class="field mono"
							type="text"
							bind:value={row.container}
							oninput={commit}
							spellcheck="false"
						/></label
					>
					<label
						><span>&nbsp;</span>
						<select
							class="field"
							value={row.readOnly ? 'ro' : 'rw'}
							onchange={(event) => ((row.readOnly = event.currentTarget.value === 'ro'), commit())}
							><option value="rw">{t('editor.readWrite')}</option><option value="ro"
								>{t('editor.readOnly')}</option
							></select
						>
					</label>
				{/if}
				<button
					type="button"
					class="btn small remove"
					aria-label={t('editor.remove')}
					onclick={() => (form!.paths.splice(index, 1), commit())}>✕</button
				>
				{#if problems.paths[index]}<p class="problem" role="alert">{problems.paths[index]}</p>{/if}
			</div>
		{:else}<p class="muted small-text">{t('editor.none')}</p>{/each}
	</section>

	{#if form.network !== 'host' && form.network !== 'none'}
<LANAssignment {text} {service} onchange={(next)=>{text=next;parse();}} />
		<section class="tile" aria-labelledby="h-ports">
			<div class="section-head">
				<h2 class="tile-title" id="h-ports">{t('editor.ports')}</h2>
				<button
					type="button"
					class="btn small"
					onclick={() => (form!.ports.push({ host: '', container: '', protocol: 'tcp' }), commit())}
					>{t('editor.addPort')}</button
				>
			</div>
			{#each form.ports as row, index (index)}
				<div class="row">
					{#if row.raw !== undefined}
						<span class="mono raw"
							>{typeof row.raw === 'string' ? row.raw : JSON.stringify(row.raw)}</span
						><span class="muted small-text">{t('editor.rawEntry')}</span>
					{:else}
						<label
							><span>{t('editor.hostPort')}</span><input
								class="field mono port"
								type="text"
								inputmode="numeric"
								bind:value={row.host}
								oninput={commit}
							/></label
						>
						<label
							><span>{t('editor.containerPort')}</span><input
								class="field mono port"
								type="text"
								inputmode="numeric"
								bind:value={row.container}
								oninput={commit}
							/></label
						>
						<label
							><span>&nbsp;</span><select class="field" bind:value={row.protocol} onchange={commit}
								><option value="tcp">TCP</option><option value="udp">UDP</option></select
							></label
						>
						<span class="grow"></span>
					{/if}
					<button
						type="button"
						class="btn small remove"
						aria-label={t('editor.remove')}
						onclick={() => (form!.ports.splice(index, 1), commit())}>✕</button
					>
					{#if problems.ports[index]}<p class="problem" role="alert">
							{problems.ports[index]}
						</p>{/if}
				</div>
			{:else}<p class="muted small-text">{t('editor.none')}</p>{/each}
		</section>
	{/if}

	{#each [['env', 'editor.env', 'editor.addEnv'], ['labels', 'editor.labels', 'editor.addLabel']] as [key, title, add] (key)}
		{@const rows = key === 'env' ? form.env : form.labels}
		<section class="tile" aria-labelledby="h-{key}">
			<div class="section-head">
				<h2 class="tile-title" id="h-{key}">{t(title as 'editor.env')}</h2>
				<button
					type="button"
					class="btn small"
					onclick={() => (rows.push({ name: '', value: '' }), commit())}
					>{t(add as 'editor.addEnv')}</button
				>
			</div>
			{#each rows as pair, index (index)}
				<div class="row">
					<label class="grow"
						><span>{t('editor.envName')}</span><input
							class="field mono"
							type="text"
							bind:value={pair.name}
							oninput={commit}
							spellcheck="false"
						/></label
					>
					<label class="grow"
						><span>{t('editor.envValue')}</span><input
							class="field mono"
							type="text"
							bind:value={pair.value}
							oninput={commit}
							spellcheck="false"
						/></label
					>
					<button
						type="button"
						class="btn small remove"
						aria-label={t('editor.remove')}
						onclick={() => (rows.splice(index, 1), commit())}>✕</button
					>
					{#if key === 'env' && problems.env[index]}<p class="problem" role="alert">
							{problems.env[index]}
						</p>{/if}
				</div>
			{:else}<p class="muted small-text">{t('editor.none')}</p>{/each}
		</section>
	{/each}
{:else if !loadError && !yamlError && original}
	<p class="notice-warn">{t('editor.noServices')}</p>
{/if}

{#if loaded}<AppProxy app={name} {members} version={proxyVersion} /><AppIpv64 app={name} />{/if}
{#if tailnetDialog}<TailnetDialog
		app={name}
		mode={tailnetDialog}
		port={tailnetPort(members, primary)}
		onclose={() => (tailnetDialog = null)}
		onchanged={() => {
			proxyVersion += 1;
			void loadProxy();
			void loadStatus();
		}}
	/>{/if}

{#if result}
	<div class={result.ok ? 'notice-ok' : 'notice-error'} role="status">
		{result.text}{#if result.detail}<pre>{result.detail}</pre>{/if}
	</div>
{/if}
{#if loaded}
	<div class="apply">
		<span class="muted note" role="status"
			>{applying
				? t('editor.applying')
				: changed
					? t('editor.applyNote')
					: t('editor.unchanged')}</span
		>
		<button
			type="button"
			class="btn"
			disabled={!changed || applying}
			onclick={() => (confirmDiscard = true)}>{t('editor.discard')}</button
		>
		<button type="button" class="btn btn-primary" onclick={apply} disabled={blocked}
			>{t('editor.apply')}</button
		>
	</div>
{/if}

{#if confirmDiscard}<Dialog title={t('editor.discardTitle')} onclose={() => (confirmDiscard = false)}>
		<p>{t('editor.discardText')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (confirmDiscard = false)}
				>{t('shares.keepEditing')}</button
			><button type="button" class="btn btn-warn" onclick={discardChanges}
				>{t('editor.discard')}</button
			>
		</div>
	</Dialog>{/if}

{#if logs}
	{@const open = logs}
	<Dialog
		title={t('docker.logsTitle', { name: dockerName(open.container, 'distinct') })}
		wide
		onclose={() => (logs = null)}
	>
		{#if open.error}<div class="notice-error row-note" role="alert">
				<span>{open.error}</span><button type="button" class="btn small" onclick={() => showLogs(open.container)}
					>{t('common.retry')}</button
				>
			</div>{/if}
		<pre class="logs">{open.text === null ? '…' : open.text || t('docker.logsEmpty')}</pre>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => showLogs(open.container)}
				>{t('docker.logsRefresh')}</button
			>
			<button type="button" class="btn btn-primary" onclick={() => (logs = null)}
				>{t('common.done')}</button
			>
		</div>
	</Dialog>
{/if}

{#if leaving}<Dialog
		title={t('shares.unsaved')}
		locked={applying}
		onclose={() => {
			leaving = false;
			pendingURL = '';
		}}
	>
		<p>{t('editor.unsavedHint')}</p>
		<div class="dialog-actions">
			<button
				class="btn"
				disabled={applying}
				onclick={() => {
					leaving = false;
					pendingURL = '';
				}}>{t('shares.keepEditing')}</button
			><button class="btn btn-warn" disabled={applying} onclick={discard}
				>{t('shares.discard')}</button
			><button class="btn btn-primary" disabled={blocked} onclick={apply}
				>{t('editor.apply')}</button
			>
		</div>
		{#if result && !result.ok}<p class="notice-error" role="alert">{result.text}</p>{/if}
	</Dialog>{/if}

<style>
	.location {
		font-size: 0.8125rem;
	}
	.segments {
		display: inline-flex;
		align-self: flex-start;
		gap: 4px;
		padding: 3px;
		background: var(--panel-bg);
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
	}
	.segments button {
		height: 2.25rem;
		padding: 0 14px;
		background: transparent;
		border: 0;
		border-radius: var(--radius-ctl);
		color: var(--text);
		font: inherit;
		font-size: 0.8125rem;
		font-weight: 500;
		cursor: pointer;
	}
	.segments button[aria-checked='true'] {
		background: var(--accent);
		color: var(--on-accent);
	}
	section {
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding: 18px;
	}
	.section-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
	}
	.grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
		gap: 14px;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: 0.8125rem;
	}
	label > span {
		color: var(--muted);
	}
	small {
		font-size: 0.75rem;
		color: var(--muted);
	}
	.check {
		flex-direction: row;
		align-items: center;
		gap: 10px;
		font-size: 0.875rem;
	}
	.check span {
		color: var(--text);
	}
	.check input {
		width: 18px;
		height: 18px;
		margin: 0;
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-end;
		gap: 10px;
		padding-top: 12px;
		border-top: 1px solid var(--panel-border);
	}
	.grow {
		flex: 1 1 200px;
		min-width: 0;
	}
	.port {
		width: 8rem;
	}
	.raw {
		flex: 1 1 200px;
		align-self: center;
		font-size: 0.8125rem;
		overflow-wrap: anywhere;
	}
	.small {
		height: 2.25rem;
		padding: 0 0.75rem;
		font-size: 0.8125rem;
	}
	.small-text {
		align-self: center;
		font-size: 0.8125rem;
	}
	.remove {
		height: 2.75rem;
		color: var(--muted);
	}
	.problem {
		flex: 1 1 100%;
		font-size: 0.8125rem;
		color: var(--warn-text);
	}
	.yaml {
		min-height: 60vh;
		height: auto;
		padding: calc(var(--row-pad) + 2px) var(--tile-pad);
		font-size: 0.8125rem;
		line-height: 1.55;
		resize: vertical;
		tab-size: 2;
		font-family: var(--font-mono);
		scrollbar-width: thin;
		scrollbar-color: var(--muted) transparent;
	}
	.app-head {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 10px 16px;
		padding: 12px 18px;
	}
	.app-state,
	.quick {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px 16px;
		font-size: 0.875rem;
	}
	.quick {
		gap: 8px;
	}
	.state {
		display: inline-flex;
		align-items: center;
		gap: 8px;
	}
	.value {
		font-size: 0.8125rem;
	}
	.row-note {
		flex: 1 1 100%;
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
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
	/* Always in reach at the bottom of the window while editing. */
	.apply {
		position: sticky;
		bottom: 0;
		z-index: 5;
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: flex-end;
		gap: 12px;
		padding: 10px var(--tile-pad);
		background: var(--surface);
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-tile);
		box-shadow: var(--panel-shadow);
	}
	.apply .note {
		flex: 1 1 16rem;
	}
	.note {
		font-size: 0.8125rem;
	}
	pre {
		margin: 8px 0 0;
		font-family: var(--font-mono);
		font-size: 0.75rem;
		white-space: pre-wrap;
		word-break: break-word;
	}
	@media (max-width: 760px) {
		.grid {
			grid-template-columns: minmax(0, 1fr);
		}
		.row {
			flex-wrap: wrap;
		}
		.row > label {
			min-width: 0;
			flex: 1 1 100%;
		}
	}
</style>
