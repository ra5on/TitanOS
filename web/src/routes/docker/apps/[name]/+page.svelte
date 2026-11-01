<script lang="ts">
	import { poll } from '#lib/poll.ts';
	import { beforeNavigate, goto } from '$app/navigation';
	import { onDestroy, untrack } from 'svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { dockerName } from '#lib/docker-name.ts';
	import { publicURL } from '#lib/app-links.ts';
	import HardwarePicker from '#lib/components/HardwarePicker.svelte';
	import LANAssignment from '#lib/components/LANAssignment.svelte';
	import AddEntryDialog, { type NewEntry } from '#lib/components/appform/AddEntryDialog.svelte';
	import CpuPicker from '#lib/components/appform/CpuPicker.svelte';
	import FormRow from '#lib/components/appform/FormRow.svelte';
	import FormSection from '#lib/components/appform/FormSection.svelte';
	import MemoryLimit from '#lib/components/appform/MemoryLimit.svelte';
	import PathField from '#lib/components/appform/PathField.svelte';
	import Toggle from '#lib/components/appform/Toggle.svelte';
	import ViewSwitch, { storedAdvanced } from '#lib/components/appform/ViewSwitch.svelte';
	import { where } from '#lib/components/appform/where.ts';
	import { validImage } from '#lib/compose/image.ts';
	import { validCpuset, validDevice, validMemLimit } from '#lib/compose/template.ts';
	import { parseDocument, type Document } from 'yaml';
	import { page } from '$app/state';
	import {
		api,
		ApiError,
		type AppProxyStatus,
		type DockerContainer,
		type DockerStatus,
	} from '#lib/api.ts';
	import Icon from '#lib/components/Icon.svelte';
	import { stackPrimary, sanitizeDockerLayout } from '#lib/docker-layout.ts';
	import { formatBytes, formatPercent } from '#lib/format.ts';
	import { apiErrorText, errorText, t } from '#lib/i18n/index.svelte.ts';
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
	let advanced = $state(storedAdvanced());
	let adding = $state(false);

	// The service as it was when the page opened, to mark what changed.
	const before = $derived.by(() => {
		if (!original || !service) return null;
		const parsed = parseDocument(original, { merge: true });
		return parsed.errors.length || !serviceNames(parsed).includes(service)
			? null
			: readService(parsed, service);
	});
	const differs = (key: keyof ServiceForm) =>
		!!before && !!form && JSON.stringify(before[key]) !== JSON.stringify(form[key]);
	const envChanged = (pair: { name: string; value: string }) =>
		!!before && before.env.find((entry) => entry.name === pair.name)?.value !== pair.value;
	const rowChanged = (key: 'paths' | 'ports', index: number) =>
		!!before && !!form && JSON.stringify(before[key][index]) !== JSON.stringify(form[key][index]);

	function addEntry(entry: NewEntry) {
		adding = false;
		if (!form) return;
		if (entry.kind === 'path')
			form.paths.push({ host: entry.host, container: entry.container, readOnly: entry.readOnly });
		else if (entry.kind === 'port')
			form.ports.push({ host: entry.host, container: entry.container, protocol: entry.protocol });
		else if (entry.kind === 'variable') form.env.push({ name: entry.name, value: entry.value });
		else if (entry.kind === 'label') form.labels.push({ name: entry.name, value: entry.value });
		else form.devices.push({ value: entry.device });
		commit();
	}

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
				if (app === name) loadError = apiErrorText(err);
			},
		);
	}
	let statusError = $state('');
	async function loadStatus() {
		if (applying) return;
		try {
			status = await api.get<DockerStatus>('/docker');
			statusError = '';
		} catch (err) {
			statusError = apiErrorText(err);
		}
	}
	$effect(() => {
		const app = name;
		untrack(() => loadCompose(app));
	});
	$effect(() => poll(loadStatus, { interval: 5000 }));

	// The head of the page: state and load of the app's containers, with
	// the actions of the Docker page for the container of the chosen service.
	const members = $derived(
		(status?.containers ?? []).filter((container) => container.app === name),
	);
	const runningCount = $derived(members.filter(isRunning).length);
	const primary = $derived(
		stackPrimary(
			{ id: `app:${name}`, name, members },
			sanitizeDockerLayout(session.docker).primaryServices,
		),
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
			actionError = apiErrorText(err);
		} finally {
			restarting = false;
			await loadStatus();
		}
	}
	let logs = $state<{ container: DockerContainer; text: string | null; error: string } | null>(
		null,
	);
	let logsRequest = 0;
	async function showLogs(container: DockerContainer) {
		const request = ++logsRequest;
		if (logs?.container.id !== container.id) logs = { container, text: null, error: '' };
		else logs.error = '';
		try {
			const res = await api.get<{ logs: string }>(
				`/docker/containers/${encodeURIComponent(container.id)}/logs?tail=300`,
			);
			if (request === logsRequest && logs?.container.id === container.id) logs.text = res.logs;
		} catch (err) {
			if (request === logsRequest && logs?.container.id === container.id)
				logs.error = apiErrorText(err);
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

	const limitsBad = $derived(
		!!form &&
			(!validCpuset(form.cpuset) ||
				!validMemLimit(form.memLimit) ||
				form.devices.some((row) => row.raw === undefined && !validDevice(row.value))),
	);
	const blocked = $derived(
		applying || !changed || (view === 'form' ? !!problems?.any || limitsBad : false),
	);
	const updateAvailable = $derived(members.some((container) => container.updateAvailable));
	const hiddenCount = $derived(form ? 4 + form.labels.length : 0);

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
	{#snippet actions()}<ViewSwitch bind:advanced />{/snippet}
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
					{formatPercent(
						members.filter(isRunning).reduce((sum, container) => sum + container.cpuPercent, 0),
					)}</span
				>
				<span class="mono value"
					><span class="muted">{t('docker.ram')}</span>
					{formatBytes(
						members.filter(isRunning).reduce((sum, container) => sum + container.memoryBytes, 0),
					)}</span
				>
			{/if}
		{:else if status}
			<span class="muted">{t('editor.noContainers')}</span>
		{:else if !statusError}
			<span class="muted" role="status">{t('common.loading')}</span>
		{/if}
	</div>
	<div class="quick">
		{#each quickLinks as link (link.label)}<a
				class="btn small"
				href={link.href}
				target="_blank"
				rel="noreferrer"
				title={link.label}><Icon name={link.icon} size={14} />{link.text}</a
			>{/each}
		<button
			type="button"
			class="btn small"
			disabled={!target}
			onclick={() => target && showLogs(target)}
			><Icon name="fileText" size={14} />{t('docker.logs')}</button
		>
		<button
			type="button"
			class="btn small"
			disabled={!target || !isRunning(target)}
			onclick={() => target && addContainerConsole(target.id, dockerName(target, 'distinct'))}
			><Icon name="terminal" size={14} />{t('docker.console')}</button
		>
		<button
			type="button"
			class="btn small"
			disabled={!members.length || restarting || applying}
			onclick={restartAll}
			><Icon name="restart" size={14} />{t(
				restarting ? 'editor.restarting' : 'docker.restart',
			)}</button
		>
		{#if proxy}<button
				type="button"
				class="btn small"
				onclick={() => (tailnetDialog = published ? 'remove' : 'publish')}
				><Icon name="tailnet" size={14} />{t(
					published ? 'docker.tailnetUnpublish' : 'docker.tailnetPublish',
				)}</button
			>{/if}
	</div>
	{#if statusError}<div class="notice-warn row-note" role="alert">
			<span>{statusError}</span><button type="button" class="btn small" onclick={loadStatus}
				>{t('common.retry')}</button
			>
		</div>{/if}
	{#if actionError}<div class="notice-error row-note" role="alert">
			<span>{actionError}</span><button
				type="button"
				class="btn small"
				disabled={restarting}
				onclick={restartAll}>{t('common.retry')}</button
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

{#if form && problems && view === 'form' && services.length > 1}
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

{#if loaded}
	<section class="tile form" aria-label={t('editor.title', { name })}>
		<FormRow
			label={t('apps.name')}
			hint={t('editor.location', { path: `/config/apps/${name}/compose.yaml` })}
		>
			<p class="text mono">
				{name}{#if services.length > 1}<span class="muted"> · {service}</span>{/if}
			</p>
		</FormRow>
		{#if view === 'compose'}
			<FormRow label={t('appform.composeFile')}>
				<textarea
					class="field mono yaml"
					bind:value={text}
					spellcheck="false"
					aria-label={t('editor.compose')}></textarea>
			</FormRow>
		{:else if form && problems}
			<FormRow
				label={t('appform.repository')}
				id="image"
				changed={differs('image')}
				hint={!validImage(form.image)
					? t('editor.imageInvalid')
					: updateAvailable
						? t('appform.updateAvailable')
						: ''}
				tone={!validImage(form.image) ? 'error' : updateAvailable ? 'warn' : ''}
			>
				<div class="line">
					<input
						id="image"
						class="field mono"
						bind:value={form.image}
						oninput={commit}
						spellcheck="false"
						aria-invalid={!validImage(form.image)}
					/>
				</div>
			</FormRow>
			<FormRow
				label={t('editor.webui')}
				id="webui"
				changed={differs('webui')}
				hint={t('editor.webuiHint')}
			>
				<div class="line">
					<input
						id="webui"
						class="field mono"
						bind:value={form.webui}
						oninput={commit}
						placeholder="8096"
						spellcheck="false"
					/>
				</div>
			</FormRow>
			<FormRow label={t('editor.restart')} id="restart" changed={differs('restart')}>
				<div class="line">
					<select id="restart" class="field" bind:value={form.restart} onchange={commit}
						>{#each RESTART_POLICIES as policy (policy)}<option value={policy}
								>{t(`editor.restart.${policy}` as 'editor.restart.no')}</option
							>{/each}{#if !RESTART_POLICIES.includes(form.restart)}<option value={form.restart}
								>{form.restart}</option
							>{/if}</select
					>
				</div>
			</FormRow>
			{#if advanced}
				<FormRow
					label={t('editor.publicUrl')}
					id="public-url"
					changed={differs('publicUrl')}
					hint={form.publicUrl.trim() && !publicURL(form.publicUrl)
						? t('editor.publicUrlInvalid')
						: t('editor.publicUrlHint')}
					tone={form.publicUrl.trim() && !publicURL(form.publicUrl) ? 'error' : ''}
				>
					<div class="line">
						<input
							id="public-url"
							class="field mono"
							type="url"
							bind:value={form.publicUrl}
							oninput={commit}
							placeholder="https://app.example.org"
							spellcheck="false"
						/>
					</div>
				</FormRow>
				<FormRow
					label={t('appform.cpus')}
					changed={differs('cpuset')}
					hint={validCpuset(form.cpuset) ? t('appform.cpusHint') : t('appform.cpusInvalid')}
					tone={validCpuset(form.cpuset) ? '' : 'error'}
				>
					<CpuPicker
						bind:cpuset={form.cpuset}
						threads={system.info?.cpu.threads ?? 0}
						onchange={commit}
					/>
				</FormRow>
				<FormRow
					label={t('appform.memory')}
					id="memory"
					changed={differs('memLimit')}
					hint={validMemLimit(form.memLimit) ? t('appform.memoryHint') : t('appform.memoryInvalid')}
					tone={validMemLimit(form.memLimit) ? '' : 'error'}
				>
					<MemoryLimit id="memory" bind:value={form.memLimit} onchange={commit} />
				</FormRow>
			{/if}

			<FormSection title={t('appform.sectionAccess')} />
			<FormRow
				label={t('editor.network')}
				id="network"
				changed={differs('network')}
				hint={t('appform.networkHint')}
			>
				<div class="line">
					<select id="network" class="field" bind:value={form.network} onchange={commit}
						>{#each NETWORKS as mode (mode)}<option value={mode}
								>{t(`editor.network.${mode}` as 'editor.network.host')}</option
							>{/each}{#if !NETWORKS.includes(form.network)}<option value={form.network}
								>{form.network}</option
							>{/if}</select
					>
				</div>
			</FormRow>
			{#if form.network !== 'host' && form.network !== 'none'}
				<FormRow label={t('lan.title')}>
					<LANAssignment
						{text}
						{service}
						onchange={(next) => {
							text = next;
							parse();
						}}
					/>
				</FormRow>
			{/if}
			<FormRow label={t('appform.httpsName')}>
				<AppProxy app={name} {members} version={proxyVersion} embedded />
			</FormRow>
			{#if proxy}
				<FormRow
					label={t('appform.tailscale')}
					hint={published ? t('appform.tailnetOnHint') : t('appform.tailnetOffHint')}
					tone={published ? 'ok' : ''}
				>
					<div class="line">
						<span class="state-text">{t(published ? 'appform.on' : 'appform.off')}</span>
						<button
							type="button"
							class="btn"
							onclick={() => (tailnetDialog = published ? 'remove' : 'publish')}
							><Icon name="tailnet" size={15} />{t(
								published ? 'docker.tailnetUnpublish' : 'docker.tailnetPublish',
							)}</button
						>
					</div>
				</FormRow>
			{/if}
			<FormRow label={t('appform.ipv64')}>
				<AppIpv64 app={name} embedded />
			</FormRow>
			<FormRow
				label={t('editor.privileged')}
				changed={differs('privileged')}
				hint={form.privileged ? t('editor.privilegedWarning') : ''}
				tone={form.privileged ? 'warn' : ''}
			>
				<Toggle bind:checked={form.privileged} label={t('editor.privileged')} onchange={commit} />
			</FormRow>

			<FormSection title={t('appform.sectionDevices')} />
			<FormRow
				label={t('appform.gpu')}
				changed={differs('hardware')}
				hint={t('hardware.containerHint')}
			>
				<HardwarePicker bind:selected={form.hardware} onchange={commit} />
			</FormRow>
			{#each form.devices as row, index (index)}
				{#if row.raw !== undefined}
					<FormRow label={t('appform.device')} hint={t('editor.rawEntry')}>
						<p class="text mono">{JSON.stringify(row.raw)}</p>
					</FormRow>
				{:else}
					<FormRow
						label={t('appform.device')}
						changed={differs('devices')}
						hint={validDevice(row.value) ? '' : t('appform.addDeviceHint')}
						tone={validDevice(row.value) ? '' : 'error'}
					>
						<div class="line">
							<input
								class="field mono"
								bind:value={row.value}
								oninput={commit}
								aria-label={t('appform.device')}
								spellcheck="false"
							/>
							{#if advanced}<button
									type="button"
									class="btn"
									onclick={() => (form!.devices.splice(index, 1), commit())}
									>{t('editor.remove')}</button
								>{/if}
						</div>
					</FormRow>
				{/if}
			{/each}

			<FormSection title={t('appform.sectionEntries')} />
			{#each form.paths as row, index (index)}
				{#if row.raw !== undefined}
					<FormRow label={t('editor.paths')} hint={t('editor.rawEntry')}>
						<p class="text mono">
							{typeof row.raw === 'string' ? row.raw : JSON.stringify(row.raw)}
						</p>
					</FormRow>
				{:else}
					<FormRow
						label={row.container || t('apps.ownPath')}
						changed={rowChanged('paths', index)}
						hint={problems.paths[index] ||
							[
								t('appform.containerPathHint', { path: row.container }),
								where(row.host),
								row.readOnly ? t('editor.readOnly') : '',
							]
								.filter(Boolean)
								.join(' · ')}
						tone={problems.paths[index] ? 'error' : ''}
					>
						<PathField label={row.container} bind:value={row.host} oninput={commit} />
						{#if advanced}
							<div class="line">
								<input
									class="field mono"
									bind:value={row.container}
									oninput={commit}
									aria-label={t('editor.containerPath')}
									spellcheck="false"
								/>
								<select
									class="field ro"
									value={row.readOnly ? 'ro' : 'rw'}
									aria-label={t('editor.readOnly')}
									onchange={(event) => (
										(row.readOnly = event.currentTarget.value === 'ro'),
										commit()
									)}
									><option value="rw">{t('editor.readWrite')}</option><option value="ro"
										>{t('editor.readOnly')}</option
									></select
								>
								<button
									type="button"
									class="btn"
									onclick={() => (form!.paths.splice(index, 1), commit())}
									>{t('editor.remove')}</button
								>
							</div>
						{/if}
					</FormRow>
				{/if}
			{/each}
			{#if form.network !== 'host' && form.network !== 'none'}
				{#each form.ports as row, index (index)}
					{#if row.raw !== undefined}
						<FormRow label={t('editor.ports')} hint={t('editor.rawEntry')}>
							<p class="text mono">
								{typeof row.raw === 'string' ? row.raw : JSON.stringify(row.raw)}
							</p>
						</FormRow>
					{:else}
						<FormRow
							label={t('apps.portDefault', { port: row.container || '?' })}
							changed={rowChanged('ports', index)}
							hint={problems.ports[index] ||
								t('appform.containerPortHint', { port: `${row.container}/${row.protocol}` })}
							tone={problems.ports[index] ? 'error' : ''}
						>
							<div class="line">
								<input
									class="field mono short"
									inputmode="numeric"
									bind:value={row.host}
									oninput={commit}
									aria-label={t('editor.hostPort')}
								/>
								{#if advanced}
									<input
										class="field mono short"
										inputmode="numeric"
										bind:value={row.container}
										oninput={commit}
										aria-label={t('editor.containerPort')}
									/>
									<select
										class="field short"
										bind:value={row.protocol}
										onchange={commit}
										aria-label={t('apps.protocol')}
										><option value="tcp">TCP</option><option value="udp">UDP</option></select
									>
									<button
										type="button"
										class="btn"
										onclick={() => (form!.ports.splice(index, 1), commit())}
										>{t('editor.remove')}</button
									>
								{/if}
							</div>
						</FormRow>
					{/if}
				{/each}
			{/if}
			{#each form.env as pair, index (index)}
				<FormRow
					label={pair.name || t('apps.ownVar')}
					changed={envChanged(pair)}
					hint={problems.env[index] || t('appform.variable', { name: pair.name })}
					tone={problems.env[index] ? 'error' : ''}
				>
					<div class="line">
						{#if advanced}<input
								class="field mono short-name"
								bind:value={pair.name}
								oninput={commit}
								aria-label={t('editor.envName')}
								spellcheck="false"
							/>{/if}
						<input
							class="field mono"
							bind:value={pair.value}
							oninput={commit}
							aria-label={pair.name}
							spellcheck="false"
						/>
						{#if advanced}<button
								type="button"
								class="btn"
								onclick={() => (form!.env.splice(index, 1), commit())}>{t('editor.remove')}</button
							>{/if}
					</div>
				</FormRow>
			{/each}
			{#if advanced}
				{#each form.labels as pair, index (index)}
					<FormRow
						label={pair.name || t('appform.label')}
						changed={differs('labels')}
						hint={t('appform.label')}
					>
						<div class="line">
							<input
								class="field mono short-name"
								bind:value={pair.name}
								oninput={commit}
								aria-label={t('editor.envName')}
								spellcheck="false"
							/>
							<input
								class="field mono"
								bind:value={pair.value}
								oninput={commit}
								aria-label={t('editor.envValue')}
								spellcheck="false"
							/>
							<button
								type="button"
								class="btn"
								onclick={() => (form!.labels.splice(index, 1), commit())}
								>{t('editor.remove')}</button
							>
						</div>
					</FormRow>
				{/each}
			{/if}
			<div class="links">
				<button type="button" class="link" onclick={() => (advanced = !advanced)}
					>{advanced ? t('appform.fewer') : t('appform.more', { count: hiddenCount })}</button
				>
				<button type="button" class="link" onclick={() => (adding = true)}
					><Icon name="plus" size={14} />{t('appform.addEntry')}</button
				>
			</div>
		{:else if !loadError && !yamlError && original}
			<p class="notice-warn">{t('editor.noServices')}</p>
		{/if}

		{#if result}
			<div class="result {result.ok ? 'notice-ok' : 'notice-error'}" role="status">
				{result.text}{#if result.detail}<pre>{result.detail}</pre>{/if}
			</div>
		{/if}
		<div class="apply">
			<button type="button" class="btn btn-primary" onclick={apply} disabled={blocked}
				>{t('editor.apply')}</button
			>
			<button
				type="button"
				class="btn"
				disabled={!changed || applying}
				onclick={() => (confirmDiscard = true)}>{t('editor.discard')}</button
			>
			<a class="btn" href="/docker">{t('common.done')}</a>
			<span class="muted note" role="status"
				>{applying
					? t('editor.applying')
					: changed
						? t('editor.applyNote')
						: t('editor.unchanged')}</span
			>
			<button type="button" class="link" onclick={() => show(view === 'form' ? 'compose' : 'form')}
				>{t(view === 'form' ? 'appform.editCompose' : 'apps.asForm')}</button
			>
		</div>
	</section>
{/if}

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

{#if adding}<AddEntryDialog onadd={addEntry} onclose={() => (adding = false)} />{/if}

{#if confirmDiscard}<Dialog
		title={t('editor.discardTitle')}
		onclose={() => (confirmDiscard = false)}
	>
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
				<span>{open.error}</span><button
					type="button"
					class="btn small"
					onclick={() => showLogs(open.container)}>{t('common.retry')}</button
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
		font-size: var(--text-base);
		font-weight: 500;
		cursor: pointer;
	}
	.segments button[aria-checked='true'] {
		background: var(--accent);
		color: var(--on-accent);
	}
	.yaml {
		min-height: 60vh;
		height: auto;
		padding: calc(var(--row-pad) + 2px) var(--tile-pad);
		font-size: var(--text-base);
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
		font-size: var(--text-md);
	}
	.quick {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px 16px;
		font-size: var(--text-md);
	}
	.state {
		display: inline-flex;
		align-items: center;
		gap: 8px;
	}
	.value {
		font-size: var(--text-base);
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
	.note {
		font-size: var(--text-base);
	}
	pre {
		margin: 8px 0 0;
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		white-space: pre-wrap;
		word-break: break-word;
	}
	.segments.services {
		margin-bottom: calc(-1 * var(--space-2));
	}
	.form {
		padding: var(--space-2) 0 0;
	}
	.text {
		margin: 0;
		padding-top: 0.5rem;
		font-size: var(--text-md);
	}
	.state-text {
		min-width: 3rem;
		font-size: var(--text-md);
	}
	.form :global(.line > .field.ro) {
		flex: 0 1 12rem;
	}
	.form :global(.short-name) {
		flex: 0 1 12rem;
	}
	.links {
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: var(--space-2);
		padding: var(--space-3) var(--space-4) var(--space-2) calc(min(17rem, 30%) + 2 * var(--space-4));
	}
	.link {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		padding: 0;
		background: none;
		border: 0;
		color: var(--accent);
		font: inherit;
		font-size: var(--text-md);
		cursor: pointer;
	}
	.link:hover {
		text-decoration: underline;
	}
	.result {
		margin: var(--space-2) var(--space-4);
	}
	/* Always in reach at the bottom of the window while editing. */
	.apply {
		position: sticky;
		bottom: 0;
		z-index: 5;
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2) 12px;
		margin-top: var(--space-2);
		padding: var(--space-3) var(--space-4) var(--space-3) calc(min(17rem, 30%) + 2 * var(--space-4));
		background: var(--surface);
		border-top: 1px solid var(--border);
		border-radius: 0 0 var(--radius-tile) var(--radius-tile);
	}
	.apply .note {
		flex: 1 1 12rem;
		text-align: right;
	}
	@media (max-width: 760px) {
		.links,
		.apply {
			padding-left: var(--space-3);
		}
		.apply .note {
			text-align: left;
		}
	}
</style>
