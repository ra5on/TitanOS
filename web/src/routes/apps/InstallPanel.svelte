<script lang="ts">
	import { dockerName } from '#lib/docker-name.ts';
	import { publicURL } from '#lib/app-links.ts';
	import AppIcon from '#lib/components/AppIcon.svelte';
	// Installs an app from a template: everything is filled in, and every
	// field says where its value comes from.
	import HardwarePicker from '#lib/components/HardwarePicker.svelte';
	import ImageField from '#lib/components/ImageField.svelte';
	import { hardwareSelections, withHardware } from '#lib/compose/hardware.ts';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { api, ApiError, type CatalogApp, type DockerStatus, type Share } from '#lib/api.ts';
	import { fromCasaOS, isCasaOS } from '#lib/compose/casaos.ts';
	import {
		build,
		check,
		defaultHost,
		isRelative,
		localize,
		readTemplate,
		type InstallForm,
		type PathField,
		type PortField,
		type Template,
		type VarField,
	} from '#lib/compose/template.ts';
	import { apiErrorText, errorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import { needList } from '#lib/hwcheck.ts';
	import { system } from '#lib/system.svelte.ts';

	type Props = {
		app: CatalogApp;
		source: string;
		description: string;
		docker: DockerStatus | null;
		shares: Share[];
		/** True once the user changed something in the form. */
		dirty?: boolean;
		onclose: () => void;
	};
	let {
		app,
		source,
		description,
		docker,
		shares,
		dirty = $bindable(false),
		onclose,
	}: Props = $props();

	let tpl = $state<Template | null>(null);
	let form = $state<InstallForm | null>(null);
	let loadError = $state<{ text: string; detail: string } | null>(null);
	let view = $state<'form' | 'compose'>('form');
	let text = $state('');
	let draftEnv: Record<string, string> = {};
	let selectedHardware = $state<Record<string, string[]>>({});
	let shown = $state<string[]>([]);
	let installing = $state(false);
	let error = $state<{ text: string; detail: string } | null>(null);
	let panel: HTMLElement | undefined = $state();

	const installedApps = $derived(docker?.apps ?? []);
	const ready = $derived(!!docker?.settings.enabled && docker.running);

	// Who has which port: containers by name, everything else is the system.
	const used = $derived.by(() => {
		const map = new Map<string, string>();
		for (const port of tpl?.listening ?? []) map.set(`${port.port}/${port.protocol}`, '');
		for (const container of docker?.containers ?? [])
			for (const port of container.ports)
				map.set(`${port.host}/${port.protocol}`, dockerName(container, 'distinct'));
		return map;
	});

	$effect(() => {
		panel?.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
	});

	// The panel is created anew for every app, so this runs once.
	onMount(() => {
		api.get<Template>(`/apps/sources/${app.source}/apps/${app.id}`).then(
			(res) => {
				// A second copy of an installed app needs a name of its own.
				let name = app.id;
				for (let n = 2; installedApps.includes(name); n++) name = `${app.id.slice(0, 29)}-${n}`;
				try {
					// Templates of CasaOS stores are translated first.
					if (isCasaOS(res.compose)) {
						res.compose = fromCasaOS(res.compose, app.id, {
							timezone: res.timezone,
							uid: res.uid,
							gid: res.gid,
							host: system.info?.network?.ipv4.split('/')[0] || location.hostname,
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
				} catch (err) {
					loadError = {
						text: t('apps.templateBroken'),
						detail: err instanceof Error ? err.message : '',
					};
				}
			},
			(err) =>
				(loadError = {
					text: apiErrorText(err),
					detail: '',
				}),
		);
	});

	const problems = $derived(form ? check(form, installedApps, used) : null);

	// Changes of the user, against what the template filled in. Until the
	// user touches the panel, values filled in by the page move the start.
	const formState = $derived(
		form ? JSON.stringify({ form, selectedHardware, text: view === 'compose' ? text : '' }) : '',
	);
	let baseline = $state('');
	let edited = false;
	$effect(() => {
		const current = formState;
		if (!edited) baseline = current;
	});
	const markEdited = () => (edited = true);
	$effect(() => {
		const element = panel;
		if (!element) return;
		for (const type of ['input', 'change', 'keydown']) element.addEventListener(type, markEdited);
		return () => {
			for (const type of ['input', 'change', 'keydown'])
				element.removeEventListener(type, markEdited);
		};
	});
	$effect(() => {
		dirty = !!formState && formState !== baseline;
	});
	const by = (name: string | null | undefined) => name || t('apps.system');

	/** Paths nobody changed follow the name of the app. */
	function renamed() {
		if (!form || !tpl) return;
		for (const field of form.paths)
			if (!field.touched) field.host = defaultHost(field, form.name, tpl.appdata);
	}

	function editPath(field: PathField) {
		if (form && tpl) field.touched = field.host !== defaultHost(field, form.name, tpl.appdata);
	}

	function varOrigin(field: VarField): 'template' | 'titanos' | 'generated' | 'required' | 'own' {
		if (field.required && field.value.trim() === '') return 'required';
		return field.origin === 'required' ? 'own' : field.origin;
	}
	function pathOrigin(field: PathField) {
		if (field.touched) return 'own';
		return field.share || isRelative(field.template) ? 'titanos' : 'template';
	}
	const chipClass = (origin: string) =>
		origin === 'required' ? 'warn' : origin === 'titanos' || origin === 'generated' ? 'info' : '';

	function varNote(field: VarField): string {
		const help = localize(field.help, i18n.locale);
		if (field.secret) return [help, t('apps.note.generated')].filter(Boolean).join(' ');
		if (field.fill && field.origin === 'titanos') return help || t(`apps.note.${field.fill}`);
		return help;
	}
	function pathNote(field: PathField): string {
		if (field.share) return t('apps.note.share', { name: field.share.name });
		if (!field.touched && isRelative(field.template))
			return t(field.wanted ? 'apps.note.noShare' : 'apps.note.appdata');
		return '';
	}
	function portNote(field: PortField, problem: string): { text: string; warn: boolean } {
		if (problem === 'invalid') return { text: t('apps.portInvalid'), warn: true };
		if (problem === 'duplicate') return { text: t('apps.portDuplicate'), warn: true };
		if (problem === 'inUse')
			return {
				text: t('apps.portInUse', { by: by(used.get(`${Number(field.host)}/${field.protocol}`)) }),
				warn: true,
			};
		if (field.blockedBy !== null)
			return {
				text: t('apps.portMoved', {
					port: field.templateHost,
					by: by(field.blockedBy),
					free: field.host,
				}),
				warn: true,
			};
		return { text: t('apps.portFree', { port: field.host }), warn: false };
	}
	const ownPortNote = (problem: string, host: string, protocol: string) =>
		problem === 'empty'
			? t('apps.portEmpty')
			: problem === 'invalid'
				? t('apps.portInvalid')
				: problem === 'duplicate'
					? t('apps.portDuplicate')
					: problem === 'inUse'
						? t('apps.portInUse', { by: by(used.get(`${Number(host)}/${protocol}`)) })
						: t('apps.portFree', { port: host });
	const ownVarNote = (problem: string) =>
		problem === 'empty'
			? t('apps.ownVarEmpty')
			: problem === 'invalid'
				? t('apps.ownVarInvalid')
				: problem === 'duplicate'
					? t('apps.ownVarDuplicate')
					: problem === 'replaces'
						? t('apps.ownVarReplaces')
						: '';
	const pathProblem = (problem: string) =>
		problem === 'invalid'
			? t('apps.pathInvalid')
			: problem === 'volatile'
				? t('apps.pathVolatile')
				: '';

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
			const hardware = hardwareSelections(text);
			tpl = next;
			form = edited;
			selectedHardware = hardware;
			error = null;
			return true;
		} catch (err) {
			error = { text: t('editor.yamlError'), detail: err instanceof Error ? err.message : '' };
			return false;
		}
	}

	async function install() {
		if (!tpl || !form) return;
		if (view === 'compose' && !readCompose()) return;
		if (check(form, installedApps, used).blocking) {
			error = { text: t('apps.correctFields'), detail: '' };
			return;
		}
		installing = true;
		error = null;
		const result = build(tpl, $state.snapshot(form));
		try {
			await api.post('/docker/apps', {
				name: form.name,
				compose: withHardware(result.compose, selectedHardware),
				env: result.env,
			});
			// Images are pulled in the background; the Docker page shows the progress.
			await goto('/docker');
		} catch (err) {
			const code = err instanceof ApiError ? err.code : 'internal';
			error = {
				text: errorText(code),
				detail:
					code === 'invalid_compose' || code === 'docker_failed' ? (err as ApiError).message : '',
			};
			installing = false;
		}
	}
</script>

<aside class="tile" aria-labelledby="install-title" bind:this={panel}>
	<div class="top">
		<span class="monogram" aria-hidden="true"><AppIcon src={app.icon} name={app.name} /></span>
		<div class="about">
			<h2 id="install-title">{t('apps.panelTitle', { name: app.name })}</h2>
			<span class="muted text">{description}</span>
			<span class="muted mono meta">{app.image} · {t('apps.panelSource', { source })}</span>
		</div>
		<button type="button" class="btn icon" aria-label={t('apps.close')} onclick={onclose}>✕</button>
	</div>

	{#if loadError}
		<div class="body">
			<div class="notice-error" role="alert">
				{loadError.text}{#if loadError.detail}<pre>{loadError.detail}</pre>{/if}
			</div>
		</div>
	{:else if !form || !problems || !tpl}
		<div class="body"><p class="muted">{t('apps.loading')}</p></div>
	{:else}
		<div class="summary">
			<p>
				<span class="dot ok" aria-hidden="true"></span>{t('apps.taken', {
					vars: form.vars.length,
					paths: form.paths.length,
					ports: form.ports.length,
				})}
			</p>
			{#if problems.missing > 0}
				<p class="warn">
					<span class="dot warn" aria-hidden="true"></span>{problems.missing === 1
						? t('apps.missingOne')
						: t('apps.missing', { count: problems.missing })}
				</p>
			{/if}
			{#if installedApps.includes(app.id)}
				<p class="muted">
					{t('apps.alreadyInstalled', { name: app.name })}
					<a href="/docker/apps/{app.id}">{t('apps.editInstalled')}</a>
				</p>
			{/if}
		</div>

		{#if form.risks.length > 0 || form.missingDevices.length > 0 || (tpl.cpuMissing?.length ?? 0) > 0 || localize(form.tips, i18n.locale)}
			<div class="body notes">
				{#if tpl.cpuMissing?.length}
					<p class="notice-warn" role="note">
						{t('apps.cpuMissing', { list: needList(tpl.cpuMissing) })}
					</p>
				{/if}
				{#if form.risks.length > 0}
					<div class="notice-warn" role="note">
						{t('apps.risks')}
						<ul>
							{#each form.risks as risk (risk)}<li>{t(`apps.risk.${risk}`)}</li>{/each}
						</ul>
					</div>
				{/if}
				{#if form.missingDevices.length > 0}
					<p class="muted note">
						{t('apps.missingDevices', { devices: form.missingDevices.join(', ') })}
					</p>
				{/if}
				{#if localize(form.tips, i18n.locale)}
					<details>
						<summary>{t('apps.tips')}</summary>
						<pre class="tips">{localize(form.tips, i18n.locale)}</pre>
					</details>
				{/if}
			</div>
		{/if}

		{#if view === 'compose'}
			<div class="body">
				<p class="muted note">{t('apps.composeNote')}</p>
				<textarea
					class="field mono yaml"
					bind:value={text}
					spellcheck="false"
					aria-label={t('apps.asCompose')}></textarea>
			</div>
		{:else}
			<div class="body">
				<label class="stack"
					><span>{t('editor.publicUrl')}</span><input
						class="field mono"
						type="url"
						bind:value={form.publicUrl}
						placeholder="https://app.example.org"
						spellcheck="false"
					/><small>{t('editor.publicUrlHint')}</small
					>{#if form.publicUrl.trim() && !publicURL(form.publicUrl)}<small
							class="notice-error"
							role="alert">{t('editor.publicUrlInvalid')}</small
						>{/if}</label
				>
				<label class="stack">
					<span>{t('apps.name')}</span>
					<input
						class="field mono"
						type="text"
						bind:value={form.name}
						oninput={renamed}
						maxlength="32"
						autocapitalize="none"
						spellcheck="false"
						aria-invalid={problems.name !== ''}
					/>
					{#if problems.name}
						<small class="warn" role="alert"
							>{t(problems.name === 'exists' ? 'apps.nameExists' : 'apps.nameInvalid')}</small
						>
					{:else if tpl.appdata}
						<small>{t('apps.nameHint', { path: `${tpl.appdata}/${form.name}` })}</small>
					{/if}
				</label>

				<section aria-labelledby="install-images">
					<h3 id="install-images">{t('apps.images')}</h3>
					<p class="muted note">{t('apps.imagesHint')}</p>
					{#each form.images as field (field.service)}
						<fieldset class="box">
							<legend class="mono">{field.service}</legend><ImageField bind:image={field.image} />
						</fieldset>
					{/each}
				</section>
				<section aria-labelledby="install-vars">
					<h3 id="install-vars">{t('apps.vars')}</h3>
					{#each form.vars as field, index (field.service + field.name)}
						{@const origin = varOrigin(field)}
						{@const note = varNote(field)}
						{@const id = `var-${index}`}
						<div class="stack">
							<div class="line">
								<label for={id}
									>{localize(field.label, i18n.locale) || field.name}
									{#if localize(field.label, i18n.locale)}<span class="muted mono key"
											>{field.name}</span
										>{/if}</label
								>
								<span class="chip {chipClass(origin)}">{t(`apps.origin.${origin}`)}</span>
							</div>
							<div class="joined">
								<input
									{id}
									class="field mono"
									type={field.secret && !shown.includes(id) ? 'password' : 'text'}
									bind:value={field.value}
									placeholder={field.placeholder}
									autocomplete="off"
									spellcheck="false"
									aria-invalid={problems.vars[index] !== ''}
								/>
								{#if field.secret}
									<button
										type="button"
										class="btn"
										onclick={() =>
											(shown = shown.includes(id)
												? shown.filter((entry) => entry !== id)
												: [...shown, id])}
										>{t(shown.includes(id) ? 'apps.hide' : 'apps.show')}</button
									>
								{/if}
							</div>
							{#if note}<small>{note}</small>{/if}
						</div>
					{/each}
					{#each form.own.vars as pair, index (index)}
						{@const note = ownVarNote(problems.ownVars[index])}
						<div class="stack">
							<div class="line">
								<span>{t('apps.ownVar')}</span><span class="chip">{t('apps.origin.own')}</span>
							</div>
							<div class="joined">
								<input
									class="field mono own-name"
									type="text"
									bind:value={pair.name}
									placeholder="NAME"
									aria-label={t('apps.ownVarName')}
									autocapitalize="none"
									spellcheck="false"
								/>
								<input
									class="field mono"
									type="text"
									bind:value={pair.value}
									aria-label={t('apps.ownVarValue')}
									placeholder={t('apps.ownVarValue')}
									spellcheck="false"
								/>
								<button
									type="button"
									class="btn icon"
									aria-label={t('apps.remove')}
									onclick={() => form!.own.vars.splice(index, 1)}>✕</button
								>
							</div>
							{#if note}<small class:warn={problems.ownVars[index] !== 'empty'}>{note}</small>{/if}
						</div>
					{/each}
					<button
						type="button"
						class="add"
						onclick={() => form!.own.vars.push({ name: '', value: '' })}
						>+ {t('apps.ownVar')}</button
					>
				</section>

				<section aria-labelledby="install-paths">
					<h3 id="install-paths">{t('apps.paths')}</h3>
					<datalist id="install-shares"
						>{#each shares.filter((share) => share.available) as share (share.name)}<option
								value={share.path}
							></option>{/each}</datalist
					>
					{#each form.paths as field, index (field.service + field.index)}
						{@const label = localize(field.label, i18n.locale) || field.container}
						{@const origin = pathOrigin(field)}
						{@const problem = pathProblem(problems.paths[index])}
						<div class="box">
							<div class="line">
								<span>{label} <span class="muted mono key">→ {field.container}</span></span>
								<span class="chip {chipClass(origin)}">{t(`apps.origin.${origin}`)}</span>
							</div>
							<input
								class="field mono"
								type="text"
								list="install-shares"
								bind:value={field.host}
								oninput={() => editPath(field)}
								aria-label={t('apps.pathHost', { label })}
								spellcheck="false"
							/>
							{#if problem}<small class="warn" role="alert">{problem}</small
								>{:else if pathNote(field)}<small>{pathNote(field)}</small>{/if}
						</div>
					{/each}
					{#each form.own.paths as row, index (index)}
						{@const problem = pathProblem(problems.ownPaths[index])}
						<div class="box">
							<div class="line">
								<span>{t('apps.ownPath')}</span><span class="chip">{t('apps.origin.own')}</span>
							</div>
							<div class="joined">
								<input
									class="field mono"
									type="text"
									list="install-shares"
									bind:value={row.host}
									placeholder="/mnt/…"
									aria-label={t('apps.ownPathHost')}
									spellcheck="false"
								/>
								<span class="muted" aria-hidden="true">→</span>
								<input
									class="field mono"
									type="text"
									bind:value={row.container}
									placeholder="/…"
									aria-label={t('apps.ownPathContainer')}
									spellcheck="false"
								/>
								<button
									type="button"
									class="btn icon"
									aria-label={t('apps.remove')}
									onclick={() => form!.own.paths.splice(index, 1)}>✕</button
								>
							</div>
							{#if problem}<small class="warn">{problem}</small>{/if}
						</div>
					{/each}
					<button
						type="button"
						class="add"
						onclick={() => form!.own.paths.push({ host: '', container: '' })}
						>+ {t('apps.ownPath')}</button
					>
				</section>

				<section aria-labelledby="install-ports">
					<h3 id="install-ports">{t('apps.ports')}</h3>
					{#each form.ports as field, index (field.service + field.index)}
						{@const label =
							localize(field.label, i18n.locale) ||
							t('apps.portDefault', { port: field.container })}
						{@const note = portNote(field, problems.ports[index])}
						<div class="box">
							<div class="joined">
								<span class="port-label">{label}</span>
								<input
									class="field mono port"
									type="text"
									inputmode="numeric"
									bind:value={field.host}
									aria-label={t('apps.portHost', { label })}
									aria-invalid={problems.ports[index] !== ''}
								/>
								<span class="muted mono key">→ {field.container}/{field.protocol}</span>
							</div>
							<small class:warn={note.warn} role="status"
								><span class="dot" class:ok={!note.warn} class:warn={note.warn} aria-hidden="true"
								></span>{note.text}</small
							>
						</div>
					{/each}
					{#each form.own.ports as row, index (index)}
						{@const problem = problems.ownPorts[index]}
						<div class="box">
							<div class="line">
								<span>{t('apps.ownPort')}</span><span class="chip">{t('apps.origin.own')}</span>
							</div>
							<div class="joined">
								<input
									class="field mono port"
									type="text"
									inputmode="numeric"
									bind:value={row.host}
									placeholder={t('apps.ownPortHostShort')}
									aria-label={t('apps.ownPortHost')}
								/>
								<span class="muted" aria-hidden="true">→</span>
								<input
									class="field mono port"
									type="text"
									inputmode="numeric"
									bind:value={row.container}
									placeholder={t('apps.ownPortContainerShort')}
									aria-label={t('apps.ownPortContainer')}
								/>
								<select
									class="field proto"
									bind:value={row.protocol}
									aria-label={t('apps.protocol')}
									><option value="tcp">TCP</option><option value="udp">UDP</option></select
								>
								<button
									type="button"
									class="btn icon"
									aria-label={t('apps.remove')}
									onclick={() => form!.own.ports.splice(index, 1)}>✕</button
								>
							</div>
							<small class:warn={problem !== '' && problem !== 'empty'} role="status"
								>{ownPortNote(problem, row.host, row.protocol)}</small
							>
						</div>
					{/each}
					<button
						type="button"
						class="add"
						onclick={() => form!.own.ports.push({ host: '', container: '', protocol: 'tcp' })}
						>+ {t('apps.ownPort')}</button
					>
				</section>
			</div>
		{/if}

		{#if form && view === 'form' && Object.keys(selectedHardware).length > 0}
			<div class="body">
				<section aria-labelledby="install-hardware">
					<h3 id="install-hardware">{t('apps.hardware')}</h3>
					{#each Object.keys(selectedHardware) as serviceName (serviceName)}
						<fieldset class="box">
							<legend class="mono">{serviceName}</legend>
							<HardwarePicker bind:selected={selectedHardware[serviceName]} onchange={markEdited} />
						</fieldset>
					{/each}
				</section>
			</div>
		{/if}
		{#if error}
			<div class="body">
				<div class="notice-error" role="alert">
					{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}
				</div>
			</div>
		{/if}
		<div class="bottom">
			<button type="button" class="link" onclick={() => show(view === 'form' ? 'compose' : 'form')}
				>{t(view === 'form' ? 'apps.asCompose' : 'apps.asForm')}</button
			>
			<button
				type="button"
				class="btn btn-primary"
				onclick={install}
				disabled={installing || !ready || (view === 'form' && problems.blocking)}
				>{t(installing ? 'apps.installing' : 'apps.install')}</button
			>
		</div>
	{/if}
</aside>

<style>
	aside {
		flex: 0 1 480px;
		min-width: 0;
		display: flex;
		flex-direction: column;
		overflow: hidden;
	}
	.top {
		display: flex;
		align-items: flex-start;
		gap: 14px;
		padding: 20px;
		border-bottom: 1px solid var(--panel-border);
	}
	.monogram {
		width: 56px;
		height: 56px;
		flex: none;
		display: flex;
		align-items: center;
		justify-content: center;
		border-radius: var(--radius-ctl);
		color: var(--text);
		font-size: 1.25rem;
		font-weight: 600;
	}
	.about {
		flex: 1 1 auto;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
	h2 {
		font-size: 1.125rem;
		font-weight: 600;
	}
	.text {
		font-size: 0.8125rem;
	}
	.meta {
		font-size: 0.75rem;
		overflow-wrap: anywhere;
	}
	.icon {
		width: 2.5rem;
		height: 2.5rem;
		flex: none;
		padding: 0;
		color: var(--muted);
	}
	.summary {
		display: flex;
		flex-direction: column;
		gap: 8px;
		padding: 16px 20px;
		border-bottom: 1px solid var(--panel-border);
		font-size: 0.875rem;
	}
	.summary p {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 6px 10px;
	}
	.warn {
		color: var(--warn-text);
	}
	.body {
		display: flex;
		flex-direction: column;
		gap: 18px;
		padding: 16px 20px;
	}
	section {
		display: flex;
		flex-direction: column;
		gap: 10px;
	}
	h3 {
		font-size: 0.75rem;
		font-weight: 600;
		letter-spacing: 0.06em;
		text-transform: uppercase;
		color: var(--muted);
	}
	.stack,
	.box {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: 0.8125rem;
	}
	.box {
		padding: 12px;
		border: 1px solid var(--panel-border);
		border-radius: var(--radius-ctl);
	}
	.line {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
	}
	.key {
		font-size: 0.75rem;
	}
	.joined {
		display: flex;
		align-items: center;
		gap: 6px;
	}
	.joined .field {
		flex: 1 1 auto;
		min-width: 0;
	}
	.joined .own-name {
		flex: 0 1 45%;
	}
	.joined .port {
		flex: 0 0 5.75rem;
	}
	.joined .proto {
		flex: 0 0 5rem;
	}
	.port-label {
		flex: 1 1 100px;
		min-width: 0;
	}
	small {
		display: flex;
		align-items: center;
		gap: 8px;
		font-size: 0.75rem;
		color: var(--muted);
	}
	small.warn {
		color: var(--warn-text);
	}
	.add {
		align-self: flex-start;
		height: 2.25rem;
		padding: 0 12px;
		background: transparent;
		border: 1px dashed var(--border);
		border-radius: var(--radius-ctl);
		color: var(--text);
		font: inherit;
		font-size: 0.8125rem;
		cursor: pointer;
	}
	.note {
		font-size: 0.8125rem;
	}
	.notes {
		gap: 10px;
		border-bottom: 1px solid var(--panel-border);
	}
	ul {
		margin: 6px 0 0;
		padding-left: 1.25rem;
	}
	summary {
		font-size: 0.875rem;
		cursor: pointer;
	}
	.tips {
		font-family: inherit;
		font-size: 0.8125rem;
		line-height: 1.5;
	}
	.yaml {
		min-height: 24rem;
		height: auto;
		padding: 12px 14px;
		font-size: 0.75rem;
		line-height: 1.55;
		resize: vertical;
		tab-size: 2;
		font-family: var(--font-mono);
		scrollbar-width: thin;
		scrollbar-color: var(--muted) transparent;
	}
	.bottom {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 10px;
		margin-top: auto;
		padding: 16px 20px;
		border-top: 1px solid var(--panel-border);
	}
	.link {
		padding: 0;
		background: none;
		border: 0;
		color: var(--accent);
		font: inherit;
		font-size: 0.875rem;
		cursor: pointer;
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
	}
</style>
