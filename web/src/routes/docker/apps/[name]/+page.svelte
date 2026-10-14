<script lang="ts">
	import HardwarePicker from '#lib/components/HardwarePicker.svelte';
	import { parseDocument, type Document } from 'yaml';
	import { page } from '$app/state';
	import { api, ApiError, type DockerStatus } from '#lib/api.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { check, NETWORKS, readService, RESTART_POLICIES, serviceNames, writeService, type ServiceForm } from '#lib/compose/service.ts';

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
	let applying = $state(false);
	let result = $state<{ ok: boolean; text: string; detail: string } | null>(null);

	let doc: Document | undefined;

	/** Reads the text into the form. Returns false if it is not valid YAML. */
	function parse(): boolean {
		const parsed = parseDocument(text);
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

	$effect(() => {
		const app = name;
		api.get<{ compose: string }>(`/docker/apps/${app}`).then(
			(res) => {
				original = text = res.compose;
				if (!parse()) view = 'compose';
			},
			(err) => (loadError = errorText(err instanceof ApiError ? err.code : 'internal'))
		);
		api.get<DockerStatus>('/docker').then((res) => (status = res), () => {});
	});

	// Ports other containers already publish, for the live check.
	const taken = $derived.by(() => {
		const map = new Map<string, string>();
		for (const container of status?.containers ?? []) {
			if (container.app === name) continue;
			for (const port of container.ports) map.set(`${port.host}/${port.protocol}`, container.name);
		}
		return map;
	});
	const problems = $derived(form ? check(form, taken, t) : null);
	const changed = $derived(text !== original);
	const blocked = $derived(applying || !changed || (view === 'form' ? !!problems?.any : false));

	async function apply() {
		if (view === 'compose' && !parse()) return;
		applying = true;
		result = null;
		try {
			await api.put(`/docker/apps/${name}`, { compose: text });
			// The containers are recreated in the background; wait for the outcome.
			for (;;) {
				await new Promise((resolve) => setTimeout(resolve, 1500));
				status = await api.get<DockerStatus>('/docker');
				const job = status.jobs[name];
				if (!job || job.state === 'running') continue;
				if (job.state === 'done') {
					original = text;
					result = { ok: true, text: t('editor.applied'), detail: '' };
				} else {
					result = { ok: false, text: t('editor.failed'), detail: job.output };
				}
				break;
			}
		} catch (err) {
			const code = err instanceof ApiError ? err.code : 'internal';
			result = { ok: false, text: errorText(code), detail: code === 'invalid_compose' || code === 'docker_failed' ? (err as ApiError).message : '' };
		}
		applying = false;
	}
</script>

<div class="head">
	<div class="title">
		<a class="muted back" href="/docker">‹ {t('editor.back')}</a>
		<h1>{t('editor.title', { name })}</h1>
		<p class="muted mono location">{t('editor.location', { path: `/config/apps/${name}/compose.yaml` })}</p>
	</div>
	<div class="segments" role="radiogroup">
		<button type="button" role="radio" aria-checked={view === 'form'} onclick={() => show('form')}>{t('editor.form')}</button>
		<button type="button" role="radio" aria-checked={view === 'compose'} onclick={() => show('compose')}>{t('editor.compose')}</button>
	</div>
</div>

{#if loadError}<p class="notice-error" role="alert">{loadError}</p>{/if}
{#if yamlError}<div class="notice-error" role="alert">{t('editor.yamlError')}<pre>{yamlError}</pre></div>{/if}

{#if view === 'compose'}
	<textarea class="field mono yaml" bind:value={text} spellcheck="false" aria-label={t('editor.compose')}></textarea>
{:else if form && problems}
	{#if services.length > 1}
		<div class="segments services" role="radiogroup" aria-label={t('editor.service')}>
			{#each services as entry (entry)}
				<button type="button" role="radio" aria-checked={service === entry} onclick={() => pickService(entry)}>{entry}</button>
			{/each}
		</div>
	{/if}

	<section class="tile" aria-labelledby="h-general">
		<h2 class="tile-title" id="h-general">{t('editor.general')}</h2>
		<div class="grid">
			<label><span>{t('editor.image')}</span><input class="field mono" type="text" bind:value={form.image} oninput={commit} spellcheck="false" /></label>
			<label><span>{t('editor.restart')}</span>
				<select class="field" bind:value={form.restart} onchange={commit}>{#each RESTART_POLICIES as policy (policy)}<option value={policy}>{t(`editor.restart.${policy}` as 'editor.restart.no')}</option>{/each}{#if !RESTART_POLICIES.includes(form.restart)}<option value={form.restart}>{form.restart}</option>{/if}</select>
			</label>
			<label><span>{t('editor.network')}</span>
				<select class="field" bind:value={form.network} onchange={commit}>{#each NETWORKS as mode (mode)}<option value={mode}>{t(`editor.network.${mode}` as 'editor.network.host')}</option>{/each}{#if !NETWORKS.includes(form.network)}<option value={form.network}>{form.network}</option>{/if}</select>
			</label>
			<label><span>{t('editor.webui')}</span><input class="field mono" type="text" bind:value={form.webui} oninput={commit} placeholder="8096" spellcheck="false" /><small>{t('editor.webuiHint')}</small></label>
		</div>
		<label class="check"><input type="checkbox" bind:checked={form.privileged} onchange={commit} /><span>{t('editor.privileged')}</span></label>
		{#if form.privileged}<p class="notice-warn" role="alert">{t('editor.privilegedWarning')}</p>{/if}
	</section>

	<section class="tile"><HardwarePicker bind:selected={form.hardware} onchange={commit} /></section>
	<section class="tile" aria-labelledby="h-paths">
		<div class="section-head"><h2 class="tile-title" id="h-paths">{t('editor.paths')}</h2><button type="button" class="btn small" onclick={() => (form!.paths.push({ host: './data', container: '/data', readOnly: false }), commit())}>{t('editor.addPath')}</button></div>
		{#each form.paths as row, index (index)}
			<div class="row">
				{#if row.raw !== undefined}
					<span class="mono raw">{typeof row.raw === 'string' ? row.raw : JSON.stringify(row.raw)}</span><span class="muted small-text">{t('editor.rawEntry')}</span>
				{:else}
					<label class="grow"><span>{t('editor.hostPath')}</span><input class="field mono" type="text" bind:value={row.host} oninput={commit} spellcheck="false" /></label>
					<label class="grow"><span>{t('editor.containerPath')}</span><input class="field mono" type="text" bind:value={row.container} oninput={commit} spellcheck="false" /></label>
					<label><span>&nbsp;</span>
						<select class="field" value={row.readOnly ? 'ro' : 'rw'} onchange={(event) => ((row.readOnly = event.currentTarget.value === 'ro'), commit())}><option value="rw">{t('editor.readWrite')}</option><option value="ro">{t('editor.readOnly')}</option></select>
					</label>
				{/if}
				<button type="button" class="btn small remove" aria-label={t('editor.remove')} onclick={() => (form!.paths.splice(index, 1), commit())}>✕</button>
				{#if problems.paths[index]}<p class="problem" role="alert">{problems.paths[index]}</p>{/if}
			</div>
		{:else}<p class="muted small-text">{t('editor.none')}</p>{/each}
	</section>

	{#if form.network !== 'host' && form.network !== 'none'}
		<section class="tile" aria-labelledby="h-ports">
			<div class="section-head"><h2 class="tile-title" id="h-ports">{t('editor.ports')}</h2><button type="button" class="btn small" onclick={() => (form!.ports.push({ host: '', container: '', protocol: 'tcp' }), commit())}>{t('editor.addPort')}</button></div>
			{#each form.ports as row, index (index)}
				<div class="row">
					{#if row.raw !== undefined}
						<span class="mono raw">{typeof row.raw === 'string' ? row.raw : JSON.stringify(row.raw)}</span><span class="muted small-text">{t('editor.rawEntry')}</span>
					{:else}
						<label><span>{t('editor.hostPort')}</span><input class="field mono port" type="text" inputmode="numeric" bind:value={row.host} oninput={commit} /></label>
						<label><span>{t('editor.containerPort')}</span><input class="field mono port" type="text" inputmode="numeric" bind:value={row.container} oninput={commit} /></label>
						<label><span>&nbsp;</span><select class="field" bind:value={row.protocol} onchange={commit}><option value="tcp">TCP</option><option value="udp">UDP</option></select></label>
						<span class="grow"></span>
					{/if}
					<button type="button" class="btn small remove" aria-label={t('editor.remove')} onclick={() => (form!.ports.splice(index, 1), commit())}>✕</button>
					{#if problems.ports[index]}<p class="problem" role="alert">{problems.ports[index]}</p>{/if}
				</div>
			{:else}<p class="muted small-text">{t('editor.none')}</p>{/each}
		</section>
	{/if}

	{#each [['env', 'editor.env', 'editor.addEnv'], ['labels', 'editor.labels', 'editor.addLabel']] as [key, title, add] (key)}
		{@const rows = key === 'env' ? form.env : form.labels}
		<section class="tile" aria-labelledby="h-{key}">
			<div class="section-head"><h2 class="tile-title" id="h-{key}">{t(title as 'editor.env')}</h2><button type="button" class="btn small" onclick={() => (rows.push({ name: '', value: '' }), commit())}>{t(add as 'editor.addEnv')}</button></div>
			{#each rows as pair, index (index)}
				<div class="row">
					<label class="grow"><span>{t('editor.envName')}</span><input class="field mono" type="text" bind:value={pair.name} oninput={commit} spellcheck="false" /></label>
					<label class="grow"><span>{t('editor.envValue')}</span><input class="field mono" type="text" bind:value={pair.value} oninput={commit} spellcheck="false" /></label>
					<button type="button" class="btn small remove" aria-label={t('editor.remove')} onclick={() => (rows.splice(index, 1), commit())}>✕</button>
					{#if key === 'env' && problems.env[index]}<p class="problem" role="alert">{problems.env[index]}</p>{/if}
				</div>
			{:else}<p class="muted small-text">{t('editor.none')}</p>{/each}
		</section>
	{/each}
{:else if !loadError && !yamlError && original}
	<p class="notice-warn">{t('editor.noServices')}</p>
{/if}

<div class="apply">
	<button type="button" class="btn btn-primary" onclick={apply} disabled={blocked}>{t('editor.apply')}</button>
	<span class="muted note" role="status">{applying ? t('editor.applying') : changed ? t('editor.applyNote') : t('editor.unchanged')}</span>
</div>
{#if result}
	<div class={result.ok ? 'notice-ok' : 'notice-error'} role="status">{result.text}{#if result.detail}<pre>{result.detail}</pre>{/if}</div>
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
		gap: 4px;
	}
	.back {
		font-size: 0.8125rem;
		text-decoration: none;
	}
	h1 {
		font-size: 1.5rem;
		font-weight: 600;
	}
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
	.head .segments {
		align-self: flex-end;
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
		padding: 14px 16px;
		font-size: 0.8125rem;
		line-height: 1.55;
		resize: vertical;
		tab-size: 2;
		font-family: var(--font-mono);
		scrollbar-width: thin;
		scrollbar-color: var(--muted) transparent;
	}
	.apply {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 12px;
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
</style>
