<script lang="ts">
	// Installs an app: a template of the catalog, or a blank one for an own
	// container. One column of lines like the app editor (decided 1.11.2026,
	// PLAN 21): name, repository, access, devices, paths, ports and
	// variables; advanced lines on demand. "Install" then shows the log of
	// the job until the app runs.
	import { beforeNavigate, goto } from '$app/navigation';
	import { onDestroy, onMount } from 'svelte';
	import {
		api,
		ApiError,
		type AppProxyStatus,
		type AppStore,
		type CatalogApp,
		type DockerStatus,
		type Share,
	} from '#lib/api.ts';
	import { pullProgress } from '#lib/appform.ts';
	import { proxyBody, proxyForm, suggestedProxyName, validProxyName } from '#lib/app-proxy.ts';
	import AppIcon from '#lib/components/AppIcon.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import HardwarePicker from '#lib/components/HardwarePicker.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import AddEntryDialog, { type NewEntry } from '#lib/components/appform/AddEntryDialog.svelte';
	import CpuPicker from '#lib/components/appform/CpuPicker.svelte';
	import FormRow from '#lib/components/appform/FormRow.svelte';
	import FormSection from '#lib/components/appform/FormSection.svelte';
	import MemoryLimit from '#lib/components/appform/MemoryLimit.svelte';
	import PathField from '#lib/components/appform/PathField.svelte';
	import SecretField from '#lib/components/appform/SecretField.svelte';
	import Toggle from '#lib/components/appform/Toggle.svelte';
	import ViewSwitch, { storedAdvanced } from '#lib/components/appform/ViewSwitch.svelte';
	import { where } from '#lib/components/appform/where.ts';
	import { fromCasaOS, isCasaOS } from '#lib/compose/casaos.ts';
	import { hardwareSelections, withHardware } from '#lib/compose/hardware.ts';
	import { validImage } from '#lib/compose/image.ts';
	import {
		build,
		check,
		defaultHost,
		isRelative,
		localize,
		readTemplate,
		type InstallForm,
		type PathField as TemplatePath,
		type PortField,
		type Template,
		type VarField,
	} from '#lib/compose/template.ts';
	import { dockerName } from '#lib/docker-name.ts';
	import { formatBytesOf } from '#lib/format.ts';
	import { needList } from '#lib/hwcheck.ts';
	import { apiErrorText, errorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import { system } from '#lib/system.svelte.ts';
	import { webLinks } from '../docker/container.ts';

	/** The app of the catalog; without it an own container is set up. */
	let { source = '', id = '' }: { source?: string; id?: string } = $props();
	const own = $derived(!source || !id);

	let app = $state<CatalogApp | null>(null);
	let sourceName = $state('');
	let docker = $state<DockerStatus | null>(null);
	let shares = $state<Share[]>([]);
	let proxy = $state<AppProxyStatus | null>(null);
	let tpl = $state<Template | null>(null);
	let form = $state<InstallForm | null>(null);
	let loadError = $state('');
	let view = $state<'form' | 'compose'>('form');
	let text = $state('');
	let draftEnv: Record<string, string> = {};
	let selectedHardware = $state<Record<string, string[]>>({});
	let advanced = $state(storedAdvanced());
	let adding = $state(false);
	let error = $state<{ text: string; detail: string } | null>(null);
	// The HTTPS name the app gets once it runs (Settings › HTTPS names for apps).
	let https = $state({ enabled: true, name: '' });
	let httpsTouched = false;

	// After "Install": the job of the app until it runs.
	let phase = $state<'form' | 'installing' | 'done' | 'failed'>('form');
	let job = $state<{ state: string; output: string } | null>(null);
	let started = 0;
	let elapsed = $state(0);
	let httpsResult = $state<{ ok: boolean; text: string } | null>(null);

	const installedApps = $derived(docker?.apps ?? []);
	const ready = $derived(!!docker?.settings.enabled && docker.running);
	const host = $derived(system.info?.network?.ipv4.split('/')[0] || location.hostname);
	const threads = $derived(system.info?.cpu.threads ?? 0);
	const shareRefs = $derived(
		shares
			.filter((share) => share.available)
			.map((share) => ({ name: share.name, volume: share.volume })),
	);

	// Who has which port: containers by name, everything else is the system.
	const used = $derived.by(() => {
		const map = new Map<string, string>();
		for (const port of tpl?.listening ?? []) map.set(`${port.port}/${port.protocol}`, '');
		for (const container of docker?.containers ?? [])
			for (const port of container.ports)
				map.set(`${port.host}/${port.protocol}`, dockerName(container, 'distinct'));
		return map;
	});
	const by = (name: string | null | undefined) => name || t('apps.system');

	onMount(async () => {
		try {
			const [store, status, shareList] = await Promise.all([
				own ? Promise.resolve(null) : api.get<AppStore>('/apps'),
				api.get<DockerStatus>('/docker'),
				api.get<Share[]>('/shares').catch(() => [] as Share[]),
			]);
			docker = status;
			shares = shareList;
			if (store) {
				app = store.apps.find((entry) => entry.source === source && entry.id === id) ?? null;
				sourceName = store.sources.find((entry) => entry.id === source)?.name ?? source;
				if (!app) throw new ApiError(404, 'template_not_found', '');
			}
			proxy = await api.get<AppProxyStatus>('/docker/proxy').catch(() => null);
			const res = await api.get<Template>(
				own
					? '/apps/blank'
					: `/apps/sources/${encodeURIComponent(source)}/apps/${encodeURIComponent(id)}`,
			);
			// A second copy of an installed app needs a name of its own.
			const base = own ? 'container' : (app?.id ?? id);
			let name = base;
			for (let n = 2; installedApps.includes(name); n++) name = `${base.slice(0, 29)}-${n}`;
			// Templates of CasaOS stores are translated first.
			if (isCasaOS(res.compose)) {
				res.compose = fromCasaOS(res.compose, base, {
					timezone: res.timezone,
					uid: res.uid,
					gid: res.gid,
					host,
				});
			}
			tpl = res;
			selectedHardware = hardwareSelections(res.compose);
			form = readTemplate(
				res,
				name,
				shares
					.filter((share) => share.available)
					.map((share) => ({ name: share.name, path: share.path })),
				used,
			);
			if (own) form.images = [{ service: form.main, image: '' }];
			https.name = suggestedProxyName(name);
		} catch (err) {
			loadError =
				err instanceof Error && !(err instanceof ApiError)
					? `${t('apps.templateBroken')} ${err.message}`
					: apiErrorText(err);
		}
	});

	const problems = $derived(form ? check(form, installedApps, used) : null);
	const imageProblem = $derived(!!form && form.images.some((field) => !validImage(field.image)));

	// Ask before leaving a form with entries of the user.
	let baseline = '';
	const snapshot = () => JSON.stringify({ form, selectedHardware, https, text });
	$effect(() => {
		if (form && !baseline) baseline = snapshot();
	});
	let leaving = $state<string | null>(null);
	let allowLeave = false;
	beforeNavigate((navigation) => {
		if (allowLeave || phase !== 'form' || !form || snapshot() === baseline) return;
		navigation.cancel();
		if (!navigation.willUnload) leaving = navigation.to?.url.href ?? '/apps';
	});
	function leave(href: string) {
		allowLeave = true;
		void goto(href);
	}

	/** Paths nobody changed follow the name of the app; so does the HTTPS name. */
	function renamed() {
		if (!form || !tpl) return;
		for (const field of form.paths)
			if (!field.touched) field.host = defaultHost(field, form.name, tpl.appdata);
		if (!httpsTouched) https.name = suggestedProxyName(form.name);
	}
	function editPath(field: TemplatePath) {
		if (form && tpl) field.touched = field.host !== defaultHost(field, form.name, tpl.appdata);
	}

	function origin(kind: 'template' | 'titanos' | 'generated' | 'required' | 'own') {
		return t(`apps.origin.${kind}`);
	}
	function varOrigin(field: VarField) {
		if (field.required && field.value.trim() === '') return 'required' as const;
		return field.origin === 'required' ? ('own' as const) : field.origin;
	}
	function pathOrigin(field: TemplatePath) {
		if (field.touched) return 'own' as const;
		return field.share || isRelative(field.template) ? ('titanos' as const) : ('template' as const);
	}
	function varHint(field: VarField): string {
		const help = localize(field.help, i18n.locale);
		const note =
			field.secret && field.origin === 'generated'
				? t('apps.note.generated')
				: field.fill && field.origin === 'titanos'
					? t(`apps.note.${field.fill}`)
					: '';
		return [t('appform.variable', { name: field.name }), help || note, origin(varOrigin(field))]
			.filter(Boolean)
			.join(' · ');
	}
	function pathHint(field: TemplatePath, problem: string): { text: string; tone: '' | 'warn' } {
		if (problem === 'invalid') return { text: t('apps.pathInvalid'), tone: 'warn' };
		if (problem === 'volatile') return { text: t('apps.pathVolatile'), tone: 'warn' };
		const note = field.share
			? t('apps.note.share', { name: field.share.name })
			: !field.touched && isRelative(field.template) && field.wanted
				? t('apps.note.noShare')
				: '';
		return {
			text: [
				t('appform.containerPathHint', { path: field.container }),
				where(field.host) || note,
				origin(pathOrigin(field)),
			]
				.filter(Boolean)
				.join(' · '),
			tone: '',
		};
	}
	function portHint(field: PortField, problem: string): { text: string; tone: '' | 'warn' | 'ok' } {
		const target = t('appform.containerPortHint', { port: `${field.container}/${field.protocol}` });
		if (problem === 'invalid') return { text: t('apps.portInvalid'), tone: 'warn' };
		if (problem === 'duplicate') return { text: t('apps.portDuplicate'), tone: 'warn' };
		if (problem === 'inUse')
			return {
				text: `${t('apps.portInUse', { by: by(used.get(`${Number(field.host)}/${field.protocol}`)) })} · ${target}`,
				tone: 'warn',
			};
		if (field.blockedBy !== null)
			return {
				text: `${t('apps.portMoved', { port: field.templateHost, by: by(field.blockedBy), free: field.host })} · ${target}`,
				tone: 'warn',
			};
		return { text: `${target} · ${t('apps.portFree', { port: field.host })}`, tone: '' };
	}

	/** The next port nothing uses, for "take <port>". */
	function freePort(from: number, protocol: string): number {
		const taken = new Set(
			[...(form?.ports ?? []), ...(form?.own.ports ?? [])].map(
				(port) => `${Number(port.host)}/${port.protocol}`,
			),
		);
		let port = from;
		while (port < 65535 && (used.has(`${port}/${protocol}`) || taken.has(`${port}/${protocol}`)))
			port++;
		return port;
	}

	function addEntry(entry: NewEntry) {
		if (!form) return;
		adding = false;
		if (entry.kind === 'path')
			form.own.paths.push({ host: entry.host, container: entry.container });
		else if (entry.kind === 'port')
			form.own.ports.push({
				host: entry.host,
				container: entry.container,
				protocol: entry.protocol,
			});
		else if (entry.kind === 'variable')
			form.own.vars.push({ name: entry.name, value: entry.value });
		else if (entry.kind === 'label') form.own.labels.push({ name: entry.name, value: entry.value });
		else form.own.devices.push(entry.device);
	}

	// Lines only the advanced view shows, for "Show more settings (n)".
	const hiddenCount = $derived(
		form ? 3 + (form.images.length > 1 ? form.images.length - 1 : 0) : 0,
	);

	// The port the HTTPS name leads to: the web interface of the main service.
	const webPort = $derived(
		form?.ports.find((port) => port.webui && port.service === form?.main) ??
			form?.ports.find((port) => port.service === form?.main) ??
			null,
	);
	const offerHttps = $derived(!!proxy && !proxy.off && !!webPort);
	const httpsProblem = $derived(
		offerHttps && https.enabled && !validProxyName(https.name.trim().toLowerCase())
			? t('appform.httpsNameInvalid')
			: offerHttps &&
				  https.enabled &&
				  proxy!.apps.some((route) => route.name === https.name.trim().toLowerCase())
				? t('appform.httpsNameTaken')
				: '',
	);

	function show(next: 'form' | 'compose') {
		if (next === view || !tpl || !form) return;
		if (next === 'compose') {
			const built = build(tpl, $state.snapshot(form));
			text = withHardware(built.compose, selectedHardware);
			draftEnv = built.env;
		} else if (!readCompose()) return;
		view = next;
	}
	function readCompose(): boolean {
		if (!tpl || !form) return false;
		try {
			const next = { ...tpl, compose: text };
			const edited = readTemplate(next, form.name, [], used, draftEnv);
			selectedHardware = hardwareSelections(text);
			tpl = next;
			form = edited;
			error = null;
			return true;
		} catch (err) {
			error = { text: t('editor.yamlError'), detail: err instanceof Error ? err.message : '' };
			return false;
		}
	}

	let destroyed = false;
	onDestroy(() => (destroyed = true));

	async function install() {
		if (!tpl || !form) return;
		if (view === 'compose' && !readCompose()) return;
		if (check(form, installedApps, used).blocking || imageProblem || httpsProblem) {
			error = { text: t('apps.correctFields'), detail: '' };
			return;
		}
		error = null;
		const name = form.name;
		const result = build(tpl, $state.snapshot(form));
		phase = 'installing';
		started = Date.now();
		job = { state: 'running', output: '' };
		try {
			await api.post('/docker/apps', {
				name,
				compose: withHardware(result.compose, selectedHardware),
				env: result.env,
			});
		} catch (err) {
			const code = err instanceof ApiError ? err.code : 'internal';
			phase = 'form';
			job = null;
			error = {
				text: errorText(code),
				detail:
					code === 'invalid_compose' || code === 'docker_failed' ? (err as ApiError).message : '',
			};
			return;
		}
		// Images are pulled in the background; the job tells how far it is.
		for (;;) {
			await new Promise((resolve) => setTimeout(resolve, 1200));
			if (destroyed) return;
			elapsed = Math.round((Date.now() - started) / 1000);
			try {
				docker = await api.get<DockerStatus>('/docker');
			} catch {
				continue; // a lost answer is asked again
			}
			const current = docker.jobs[name];
			if (!current) {
				job = { state: 'failed', output: job?.output ?? '' };
				phase = 'failed';
				return;
			}
			job = current;
			if (current.state === 'running') continue;
			if (current.state === 'failed') {
				phase = 'failed';
				return;
			}
			break;
		}
		if (offerHttps && https.enabled && webPort) {
			const route = proxyForm(null, https.name.trim().toLowerCase());
			route.enabled = true;
			route.port = Number(webPort.host);
			try {
				await api.put(`/docker/proxy/apps/${encodeURIComponent(name)}`, proxyBody(route, false));
				httpsResult = { ok: true, text: '' };
			} catch (err) {
				httpsResult = { ok: false, text: apiErrorText(err) };
			}
			try {
				docker = await api.get<DockerStatus>('/docker');
			} catch {
				// The links below may lack the new name until the page is opened again.
			}
		}
		phase = 'done';
	}

	const progress = $derived(job ? pullProgress(job.output) : null);
	// The main service first; the others show in the advanced view.
	const gpuServices = $derived(
		Object.keys(selectedHardware)
			.sort((a, b) => Number(b === form?.main) - Number(a === form?.main))
			.filter((service, index) => advanced || index === 0),
	);
	const installedName = $derived(form?.name ?? '');
	const links = $derived(
		(docker?.containers ?? [])
			.filter((container) => container.app === installedName)
			.flatMap((container) => webLinks(container, host, ''))
			.filter((link) => link.icon !== 'external'),
	);
	const title = $derived(
		own ? t('appform.ownTitle') : t('appform.installTitle', { name: app?.name ?? id }),
	);
</script>

<PageHead {title} parent={{ href: '/apps', label: t('apps.title') }}>
	{#snippet actions()}
		{#if phase === 'form' && form}<ViewSwitch bind:advanced />{/if}
	{/snippet}
</PageHead>

{#if loadError}
	<div class="notice-error" role="alert">{loadError}</div>
{:else if !form || !problems || !tpl}
	<p class="muted" role="status">{t('apps.loading')}</p>
{:else if phase !== 'form'}
	<section class="tile log-tile" aria-live="polite">
		<div class="log-head">
			<span class="state {phase}"
				>{#if phase === 'installing'}<span class="spinner" aria-hidden="true"></span>{t(
						'appform.installing',
						{ name: form.name },
					)}{:else if phase === 'done'}{t('appform.installed', {
						name: form.name,
					})}{:else}{t('appform.installFailed', { name: form.name })}{/if}</span
			>
			<span class="muted mono"
				>{Math.floor(elapsed / 60)}:{String(elapsed % 60).padStart(2, '0')}</span
			>
		</div>
		<div class="log mono">
			<p>
				<span class="muted">{t('appform.logFolders')}</span>
				{form.paths.map((path) => path.host).join(', ') || '–'}
				<span class="ok">✓</span>
			</p>
			{#if progress && progress.images.length}
				<p>
					<span class="muted">{t('appform.logImages')}</span>
					{#each progress.images as image, index (image.name)}{index ? ', ' : ''}{image.name}
						<span class={image.state === 'done' ? 'ok' : image.state === 'failed' ? 'bad' : 'muted'}
							>{image.state === 'done' ? '✓' : image.state === 'failed' ? '✕' : '…'}</span
						>{/each}
				</p>
				{#if progress.totalBytes > 0}
					<p class="bytes">
						<span class="bar" aria-hidden="true"
							><span
								style:width="{Math.min(100, (100 * progress.doneBytes) / progress.totalBytes)}%"
							></span></span
						>{formatBytesOf(progress.doneBytes, progress.totalBytes)}
					</p>
				{/if}
			{/if}
			{#if Object.values(selectedHardware).some((list) => list.length)}
				<p>
					<span class="muted">{t('appform.logDevices')}</span>
					{Object.entries(selectedHardware)
						.filter(([, list]) => list.length)
						.map(([service, list]) => `${service}: ${list.length}`)
						.join(', ')}
				</p>
			{/if}
			<p>
				<span class="muted">{t('appform.logCommand')}</span>
				<span class="accent">docker compose -p {form.name} up -d</span>
			</p>
			{#each progress?.other ?? [] as line, index (index)}<p class="indent">{line}</p>{/each}
			{#if phase === 'done'}
				{#if httpsResult}<p>
						<span class="muted">{t('appform.httpsName')}:</span>
						{#if httpsResult.ok}{https.name}.{proxy?.suffixes[0] ?? ''}
							<span class="ok">✓</span>{:else}<span class="bad">{httpsResult.text}</span>{/if}
					</p>{/if}
				<p class="ok">{t('appform.logSuccess')}</p>
			{:else if phase === 'failed'}
				<p class="bad">{t('appform.logFailed')}</p>
			{/if}
		</div>
		<div class="foot">
			{#if phase === 'installing'}
				<button type="button" class="btn" onclick={() => leave('/docker')}
					>{t('appform.background')}</button
				>
				<span class="muted note">{t('appform.backgroundHint')}</span>
			{:else if phase === 'done'}
				<button type="button" class="btn btn-primary" onclick={() => leave('/docker')}
					>{t('common.done')}</button
				>
				{#each links.slice(0, 1) as link (link.href)}<a
						class="btn"
						href={link.href}
						target="_blank"
						rel="noreferrer"
						><Icon name="external" size={15} />{t('appform.open', { name: form.name })}</a
					>{/each}
				<a class="btn" href="/docker/apps/{form.name}" onclick={() => (allowLeave = true)}
					>{t('docker.edit')}</a
				>
			{:else}
				<a
					class="btn btn-primary"
					href="/docker/apps/{form.name}"
					onclick={() => (allowLeave = true)}>{t('docker.edit')}</a
				>
				<button type="button" class="btn" onclick={() => leave('/docker')}
					>{t('docker.title')}</button
				>
			{/if}
		</div>
	</section>
{:else}
	{#if !ready && docker}<p class="notice-warn" role="status">{t('apps.dockerOff')}</p>{/if}
	<section class="tile form">
		{#if !own && app}
			<FormRow
				label={t('apps.name')}
				id="app-name"
				hint={problems.name
					? t(problems.name === 'exists' ? 'apps.nameExists' : 'apps.nameInvalid')
					: tpl.appdata
						? t('apps.nameHint', { path: `${tpl.appdata}/${form.name}` })
						: ''}
				tone={problems.name ? 'error' : ''}
			>
				<div class="line">
					<span class="icon"><AppIcon src={app.icon} name={app.name} /></span>
					<input
						id="app-name"
						class="field mono"
						bind:value={form.name}
						oninput={renamed}
						maxlength="32"
						autocapitalize="none"
						spellcheck="false"
						aria-invalid={problems.name !== ''}
					/>
				</div>
			</FormRow>
			<FormRow label={t('appform.description')}>
				<p class="text">
					{localize(app.description, i18n.locale) || app.image}
					<span class="muted">· {t('apps.panelSource', { source: sourceName })}</span>
				</p>
			</FormRow>
		{:else}
			<FormRow
				label={t('apps.name')}
				id="app-name"
				hint={problems.name
					? t(problems.name === 'exists' ? 'apps.nameExists' : 'apps.nameInvalid')
					: t('appform.ownHint')}
				tone={problems.name ? 'error' : ''}
			>
				<div class="line">
					<input
						id="app-name"
						class="field mono"
						bind:value={form.name}
						oninput={renamed}
						maxlength="32"
						autocapitalize="none"
						spellcheck="false"
						aria-invalid={problems.name !== ''}
					/>
				</div>
			</FormRow>
		{/if}
		{#if tpl.cpuMissing?.length || form.risks.length || form.missingDevices.length || localize(form.tips, i18n.locale)}
			<FormRow label={t('appform.requirements')}>
				<div class="notes">
					{#if tpl.cpuMissing?.length}<p class="notice-warn" role="note">
							{t('apps.cpuMissing', { list: needList(tpl.cpuMissing) })}
						</p>{/if}
					{#if form.risks.length}<div class="notice-warn" role="note">
							{t('apps.risks')}
							<ul>
								{#each form.risks as risk (risk)}<li>{t(`apps.risk.${risk}`)}</li>{/each}
							</ul>
						</div>{/if}
					{#if form.missingDevices.length}<p class="muted text">
							{t('apps.missingDevices', { devices: form.missingDevices.join(', ') })}
						</p>{/if}
					{#if localize(form.tips, i18n.locale)}<details>
							<summary>{t('apps.tips')}</summary>
							<pre class="tips">{localize(form.tips, i18n.locale)}</pre>
						</details>{/if}
				</div>
			</FormRow>
		{/if}

		{#if view === 'compose'}
			<FormRow label={t('appform.composeFile')} hint={t('apps.composeNote')}>
				<textarea
					class="field mono yaml"
					bind:value={text}
					spellcheck="false"
					aria-label={t('apps.asCompose')}></textarea>
			</FormRow>
		{:else}
			{#each form.images as field, index (field.service)}
				{#if index === 0 || advanced}
					<FormRow
						label={form.images.length > 1
							? t('appform.repositoryOf', { service: field.service })
							: t('appform.repository')}
						id="image-{index}"
						hint={!validImage(field.image)
							? t('editor.imageInvalid')
							: index === 0 && form.images.length > 1 && !advanced
								? t('appform.moreImages', { count: form.images.length - 1 })
								: ''}
						tone={!validImage(field.image) ? 'error' : ''}
					>
						<div class="line">
							<input
								id="image-{index}"
								class="field mono"
								bind:value={field.image}
								placeholder="ghcr.io/…:latest"
								spellcheck="false"
								aria-invalid={!validImage(field.image)}
							/>
						</div>
					</FormRow>
				{/if}
			{/each}
			{#if advanced}
				<FormRow label={t('editor.publicUrl')} id="public-url" hint={t('editor.publicUrlHint')}>
					<div class="line">
						<input
							id="public-url"
							class="field mono"
							type="url"
							bind:value={form.publicUrl}
							placeholder="https://app.example.org"
							spellcheck="false"
						/>
					</div>
				</FormRow>
				<FormRow
					label={t('appform.cpus')}
					hint={problems.cpuset ? t('appform.cpusInvalid') : t('appform.cpusHint')}
					tone={problems.cpuset ? 'error' : ''}
				>
					<CpuPicker bind:cpuset={form.cpuset} {threads} />
				</FormRow>
				<FormRow
					label={t('appform.memory')}
					id="memory"
					hint={problems.memLimit ? t('appform.memoryInvalid') : t('appform.memoryHint')}
					tone={problems.memLimit ? 'error' : ''}
				>
					<MemoryLimit id="memory" bind:value={form.memLimit} />
				</FormRow>
			{/if}

			{#if offerHttps}
				<FormSection title={t('appform.sectionAccess')} />
				<FormRow
					label={t('appform.httpsName')}
					id="https-name"
					hint={httpsProblem ||
						(https.enabled ? t('appform.httpsHint') : t('appform.httpsOffHint'))}
					tone={httpsProblem ? 'error' : https.enabled ? 'ok' : ''}
				>
					<div class="line">
						<Toggle bind:checked={https.enabled} label={t('appform.httpsName')} text="" />
						{#if https.enabled}<input
								id="https-name"
								class="field mono short-name"
								bind:value={https.name}
								oninput={() => (httpsTouched = true)}
								spellcheck="false"
								autocapitalize="none"
							/><span class="muted mono">.{proxy?.suffixes[0] ?? ''}</span>{/if}
					</div>
				</FormRow>
			{/if}

			{#if Object.keys(selectedHardware).length}
				<FormSection title={t('appform.sectionDevices')} />
				{#each gpuServices as service (service)}
					<FormRow
						label={advanced && Object.keys(selectedHardware).length > 1
							? t('appform.gpuFor', { service })
							: t('appform.gpu')}
						hint={t('hardware.containerHint')}
					>
						<HardwarePicker bind:selected={selectedHardware[service]} />
					</FormRow>
				{/each}
			{/if}

			{#if form.paths.length || form.ports.length || form.vars.length || form.own.paths.length || form.own.ports.length || form.own.vars.length || form.own.labels.length || form.own.devices.length}
				<FormSection title={t('appform.sectionEntries')} />
			{/if}
			{#each form.paths as field, index (field.service + field.index)}
				{@const label = localize(field.label, i18n.locale) || field.container}
				{@const hint = pathHint(field, problems.paths[index])}
				<FormRow {label} id="path-{index}" hint={hint.text} tone={hint.tone}>
					<PathField
						id="path-{index}"
						{label}
						bind:value={field.host}
						shares={shareRefs}
						invalid={problems.paths[index] === 'invalid'}
						oninput={() => editPath(field)}
					/>
				</FormRow>
			{/each}
			{#each form.own.paths as row, index (index)}
				{@const problem = problems.ownPaths[index]}
				<FormRow
					label={row.container || t('apps.ownPath')}
					hint={problem === 'invalid'
						? t('apps.pathInvalid')
						: [
								t('appform.containerPathHint', { path: row.container }),
								where(row.host),
								origin('own'),
							]
								.filter(Boolean)
								.join(' · ')}
					tone={problem === 'invalid' ? 'error' : problem ? 'warn' : ''}
				>
					<PathField label={row.container} bind:value={row.host} shares={shareRefs}>
						{#snippet extra()}<button
								type="button"
								class="btn"
								onclick={() => form!.own.paths.splice(index, 1)}>{t('apps.remove')}</button
							>{/snippet}
					</PathField>
				</FormRow>
			{/each}
			{#each form.ports as field, index (field.service + field.index)}
				{@const label =
					localize(field.label, i18n.locale) || t('apps.portDefault', { port: field.container })}
				{@const hint = portHint(field, problems.ports[index])}
				<FormRow {label} id="port-{index}" hint={hint.text} tone={hint.tone}>
					<div class="line">
						<input
							id="port-{index}"
							class="field mono short"
							inputmode="numeric"
							bind:value={field.host}
							aria-invalid={problems.ports[index] !== ''}
						/>
						{#if problems.ports[index] === 'inUse' || problems.ports[index] === 'duplicate'}
							{@const free = freePort(
								Number(field.host) || Number(field.container),
								field.protocol,
							)}
							<button type="button" class="btn" onclick={() => (field.host = String(free))}
								>{t('appform.takePort', { port: free })}</button
							>
						{/if}
					</div>
				</FormRow>
			{/each}
			{#each form.own.ports as row, index (index)}
				{@const problem = problems.ownPorts[index]}
				<FormRow
					label={t('apps.portDefault', { port: row.container || '?' })}
					hint={problem === 'invalid'
						? t('apps.portInvalid')
						: problem === 'duplicate'
							? t('apps.portDuplicate')
							: problem === 'inUse'
								? t('apps.portInUse', { by: by(used.get(`${Number(row.host)}/${row.protocol}`)) })
								: `${t('appform.containerPortHint', { port: `${row.container}/${row.protocol}` })} · ${origin('own')}`}
					tone={problem && problem !== 'empty' ? 'warn' : ''}
				>
					<div class="line">
						<input
							class="field mono short"
							inputmode="numeric"
							bind:value={row.host}
							aria-label={t('apps.ownPortHost')}
						/>
						<button type="button" class="btn" onclick={() => form!.own.ports.splice(index, 1)}
							>{t('apps.remove')}</button
						>
					</div>
				</FormRow>
			{/each}
			{#each form.vars as field, index (field.service + field.name)}
				{@const label = localize(field.label, i18n.locale) || field.name}
				<FormRow
					{label}
					id="var-{index}"
					hint={problems.vars[index] ? t('appform.required') : varHint(field)}
					tone={problems.vars[index] ? 'error' : ''}
				>
					{#if field.secret}
						<SecretField
							id="var-{index}"
							bind:value={field.value}
							placeholder={field.placeholder}
							renew={field.origin === 'generated'}
							invalid={problems.vars[index] !== ''}
						/>
					{:else}
						<div class="line">
							<input
								id="var-{index}"
								class="field mono"
								bind:value={field.value}
								placeholder={field.placeholder}
								autocomplete="off"
								spellcheck="false"
								aria-invalid={problems.vars[index] !== ''}
							/>
						</div>
					{/if}
				</FormRow>
			{/each}
			{#each form.own.vars as pair, index (index)}
				{@const problem = problems.ownVars[index]}
				<FormRow
					label={pair.name || t('apps.ownVar')}
					hint={problem === 'invalid'
						? t('apps.ownVarInvalid')
						: problem === 'duplicate'
							? t('apps.ownVarDuplicate')
							: problem === 'replaces'
								? t('apps.ownVarReplaces')
								: `${t('appform.variable', { name: pair.name })} · ${origin('own')}`}
					tone={problem && problem !== 'replaces' ? 'error' : ''}
				>
					<div class="line">
						<input
							class="field mono"
							bind:value={pair.value}
							aria-label={t('apps.ownVarValue')}
							spellcheck="false"
						/>
						<button type="button" class="btn" onclick={() => form!.own.vars.splice(index, 1)}
							>{t('apps.remove')}</button
						>
					</div>
				</FormRow>
			{/each}
			{#each form.own.labels as pair, index (index)}
				<FormRow label={pair.name} hint={`${t('appform.label')} · ${origin('own')}`}>
					<div class="line">
						<input
							class="field mono"
							bind:value={pair.value}
							aria-label={pair.name}
							spellcheck="false"
						/>
						<button type="button" class="btn" onclick={() => form!.own.labels.splice(index, 1)}
							>{t('apps.remove')}</button
						>
					</div>
				</FormRow>
			{/each}
			{#each form.own.devices as _, index (index)}
				<FormRow
					label={t('appform.device')}
					hint={problems.ownDevices[index] ? t('appform.addDeviceHint') : origin('own')}
					tone={problems.ownDevices[index] ? 'error' : ''}
				>
					<div class="line">
						<input
							class="field mono"
							bind:value={form.own.devices[index]}
							aria-label={t('appform.device')}
							spellcheck="false"
						/>
						<button type="button" class="btn" onclick={() => form!.own.devices.splice(index, 1)}
							>{t('apps.remove')}</button
						>
					</div>
				</FormRow>
			{/each}

			<div class="links">
				<button type="button" class="link" onclick={() => (advanced = !advanced)}
					>{advanced ? t('appform.fewer') : t('appform.more', { count: hiddenCount })}</button
				>
				<button type="button" class="link add" onclick={() => (adding = true)}
					><Icon name="plus" size={14} />{t('appform.addEntry')}</button
				>
			</div>
		{/if}

		{#if error}
			<div class="notice-error error" role="alert">
				{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}
			</div>
		{/if}
		<div class="foot">
			<button
				type="button"
				class="btn btn-primary"
				onclick={install}
				disabled={!ready ||
					(view === 'form' && (problems.blocking || imageProblem || !!httpsProblem))}
				>{t('apps.install')}</button
			>
			<a class="btn" href="/apps">{t('common.cancel')}</a>
			<span class="spacer"></span>
			<button type="button" class="link" onclick={() => show(view === 'form' ? 'compose' : 'form')}
				>{t(view === 'form' ? 'appform.editCompose' : 'apps.asForm')}</button
			>
		</div>
	</section>
{/if}

{#if adding}<AddEntryDialog
		shares={shareRefs}
		onadd={addEntry}
		onclose={() => (adding = false)}
	/>{/if}

{#if leaving}
	<Dialog title={t('apps.discardTitle')} onclose={() => (leaving = null)}>
		<p>{t('apps.discardText', { name: form?.name ?? '' })}</p>
		<div class="dialog-actions">
			<button type="button" class="btn btn-warn" onclick={() => leave(leaving ?? '/apps')}
				>{t('shares.discard')}</button
			><button type="button" class="btn" onclick={() => (leaving = null)}
				>{t('shares.keepEditing')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.form {
		padding: var(--space-2) 0 0;
	}
	.icon {
		display: inline-flex;
		width: 2.25rem;
		height: 2.25rem;
		flex: none;
	}
	.notes {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		max-width: 46rem;
		padding-top: 4px;
		font-size: var(--text-md);
	}
	.notes p,
	.notes ul {
		margin: 0;
	}
	.tips {
		white-space: pre-wrap;
		font-size: var(--text-sm);
	}
	.yaml {
		min-height: 24rem;
		max-width: 100%;
		font-size: var(--text-sm);
		tab-size: 2;
	}
	.short-name {
		flex: 0 1 12rem !important;
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
	.error {
		margin: var(--space-2) var(--space-4);
	}
	.error pre {
		white-space: pre-wrap;
	}
	.foot {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
		margin-top: var(--space-2);
		padding: var(--space-3) var(--space-4) var(--space-4) calc(min(17rem, 30%) + 2 * var(--space-4));
		border-top: 1px solid var(--border);
	}
	.spacer {
		flex: 1;
	}
	.log-tile {
		display: flex;
		flex-direction: column;
	}
	.log-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-3);
		padding: var(--space-3) var(--space-4);
		border-bottom: 1px solid var(--border);
	}
	.state {
		display: inline-flex;
		align-items: center;
		gap: 10px;
		font-weight: 600;
	}
	.state.done {
		color: var(--ok-text);
	}
	.state.failed {
		color: var(--error-text);
	}
	.spinner {
		width: 14px;
		height: 14px;
		border: 2px solid var(--accent);
		border-right-color: transparent;
		border-radius: 50%;
		animation: spin 0.9s linear infinite;
	}
	@keyframes spin {
		to {
			transform: rotate(360deg);
		}
	}
	@media (prefers-reduced-motion: reduce) {
		.spinner {
			animation: none;
		}
	}
	.log {
		padding: var(--space-3) var(--space-4);
		font-size: var(--text-sm);
		line-height: 1.75;
		overflow-wrap: anywhere;
	}
	.log p {
		margin: 0;
	}
	.log .indent {
		padding-left: 1.5em;
		color: var(--muted);
	}
	.ok {
		color: var(--ok-text);
	}
	.bad {
		color: var(--error-text);
	}
	.accent {
		color: var(--accent);
	}
	.bytes {
		display: flex;
		align-items: center;
		gap: 10px;
		padding-left: 1.5em;
	}
	.bar {
		position: relative;
		display: inline-block;
		width: 12rem;
		height: 6px;
		border-radius: 3px;
		background: var(--track);
		overflow: hidden;
	}
	.bar span {
		position: absolute;
		inset: 0 auto 0 0;
		background: var(--accent);
	}
	.log-tile .foot {
		padding-left: var(--space-4);
	}
	.note {
		font-size: var(--text-sm);
	}
	@media (max-width: 760px) {
		.links,
		.foot {
			padding-left: var(--space-3);
		}
	}
</style>
