<script lang="ts">
	// "Ollama per click": nothing happens before the click. Then the Ollama
	// app of the catalog is installed through the normal app path, titand
	// waits until it answers, the assistant is pointed at it and a model
	// chosen by the memory there is gets loaded. Every step shows what it
	// does; a failed one can be retried, finished steps are not repeated.
	import { onMount } from 'svelte';
	import { api, ApiError, type DockerStatus } from '#lib/api.ts';
	import type { AssistantSettings } from '#lib/assistant.svelte.ts';
	import { pullModel, type PullProgress } from '#lib/assistant-pull.ts';
	import type { Template } from '#lib/compose/template.ts';
	import { formatBytes } from '#lib/format.ts';
	import { errorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { dockerName } from '#lib/docker-name.ts';
	import { OLLAMA_TEMPLATE, prepareOllama } from '#lib/ollama-setup.ts';

	let { onstart, ondone }: { onstart: () => void; ondone: () => void } = $props();

	type Proposal = {
		docker: { enabled: boolean; running: boolean; volume: string };
		apps: string[];
		gpu: { id: string; title: string; vramBytes?: number } | null;
		ramBytes: number;
		model: string;
		basis: 'ram' | 'vram';
	};
	type Step = 'install' | 'start' | 'connect' | 'settings' | 'model' | 'done';

	let proposal = $state<Proposal | null>(null);
	let loadError = $state('');
	let step = $state<Step | null>(null);
	let failed = $state<{ step: Exclude<Step, 'done'>; text: string; detail: string } | null>(null);
	let progress = $state<PullProgress | null>(null);
	// What this page already did, so that a retry goes on from there.
	let created = '';
	let address = '';

	const STEPS: Exclude<Step, 'done'>[] = ['install', 'start', 'connect', 'settings', 'model'];
	const ready = $derived(!!proposal?.docker.enabled && !!proposal.docker.volume && proposal.docker.running);
	const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

	async function load() {
		loadError = '';
		try {
			proposal = await api.get<Proposal>('/assistant/ollama-setup');
		} catch (err) {
			loadError = errorText(err instanceof ApiError ? err.code : 'internal');
		}
	}
	onMount(load);

	class StepError extends Error {
		code: MessageKey;
		detail: string;
		constructor(code: MessageKey, detail = '') {
			super(code);
			this.code = code;
			this.detail = detail;
		}
	}

	/** An Ollama app the NAS already runs, with the address titand uses. */
	async function findApp(): Promise<{ container: string; address: string; running: boolean } | undefined> {
		const res = await api.get<{ apps: { container: string; address: string; running: boolean }[] }>('/assistant/ollama-apps');
		return res.apps.find((app) => app.running) ?? res.apps[0];
	}

	async function install() {
		const docker = await api.get<DockerStatus>('/docker');
		if (created && docker.jobs[created]?.state === 'failed') {
			// The app is there but did not start: the Docker page's retry.
			await api.post(`/docker/apps/${encodeURIComponent(created)}/update`);
			return;
		}
		if (created || (await findApp())) return;
		let tpl: Template;
		try {
			tpl = await api.get<Template>(`/apps/sources/titanos/apps/${OLLAMA_TEMPLATE}`);
		} catch (err) {
			throw new StepError(err instanceof ApiError && err.status === 404 ? 'assistant.setup.noTemplate' : 'assistant.setup.failed', err instanceof Error ? err.message : '');
		}
		const used = new Map<string, string>();
		for (const port of tpl.listening ?? []) used.set(`${port.port}/${port.protocol}`, '');
		for (const container of docker.containers)
			for (const port of container.ports) used.set(`${port.host}/${port.protocol}`, dockerName(container, 'distinct'));
		let app;
		try {
			app = prepareOllama(tpl, docker.apps, used, proposal?.gpu?.id ?? null);
		} catch (err) {
			throw new StepError('assistant.setup.templateBroken', err instanceof Error ? err.message : '');
		}
		await api.post('/docker/apps', { name: app.name, compose: app.compose, env: app.env });
		created = app.name;
	}

	/** Waits until the Ollama container runs; images may take a while. */
	async function start() {
		for (let waited = 0; waited < 30 * 60_000; waited += 3000) {
			const docker = await api.get<DockerStatus>('/docker');
			const job = created ? docker.jobs[created] : undefined;
			if (job?.state === 'failed') throw new StepError('assistant.setup.installFailed', job.output.slice(-2000));
			const app = await findApp();
			if (app?.running) {
				address = app.address;
				return;
			}
			await sleep(3000);
		}
		throw new StepError('assistant.setup.startTimeout');
	}

	async function connect() {
		let last: unknown = null;
		for (let tries = 0; tries < 40; tries++) {
			try {
				await api.post('/assistant/models', { provider: 'ollama', ollama: address });
				return;
			} catch (err) {
				last = err;
				await sleep(3000);
			}
		}
		throw new StepError('assistant.setup.noAnswer', last instanceof ApiError ? last.message : '');
	}

	async function configure() {
		const current = await api.get<AssistantSettings>('/assistant');
		await api.put('/assistant', { ...current, provider: 'ollama', ollama: address, model: proposal!.model });
	}

	async function model() {
		progress = { status: '', total: 0, completed: 0 };
		await pullModel(proposal!.model, (p) => (progress = p));
		progress = null;
	}

	async function run() {
		if (!proposal || step) return;
		onstart();
		const start0 = failed?.step ?? 'install';
		failed = null;
		const actions: Record<Exclude<Step, 'done'>, () => Promise<void>> = { install, start, connect, settings: configure, model };
		for (const s of STEPS.slice(STEPS.indexOf(start0))) {
			step = s;
			try {
				await actions[s]();
			} catch (err) {
				failed =
					err instanceof StepError
						? { step: s, text: t(err.code), detail: err.detail }
						: { step: s, text: errorText(err instanceof ApiError ? err.code : 'internal'), detail: err instanceof ApiError ? err.message : '' };
				// The address may be needed again after a failed start.
				if (s === 'connect' || s === 'start') failed.step = 'start';
				step = null;
				return;
			}
		}
		step = 'done';
		ondone();
	}

	const gib = (bytes: number) => formatBytes(bytes);
</script>

<section class="tile panel" aria-labelledby="ollama-setup">
	<h2 class="tile-title" id="ollama-setup">{t('assistant.setup.title')}</h2>
	{#if loadError}
		<div class="notice-error" role="alert">{loadError} <button type="button" class="btn" onclick={load}>{t('ui.retry')}</button></div>
	{:else if !proposal}
		<p class="loading" role="status">{t('common.loading')}</p>
	{:else if step === 'done'}
		<p class="ok" role="status">{t('assistant.setup.done', { model: proposal.model })}</p>
	{:else}
		<p class="muted">{t('assistant.setup.text')}</p>
		{#if !ready}
			<div class="notice-warn" role="note">
				{t('assistant.setup.dockerOff')}
				<a href="/docker">{t('assistant.setup.dockerLink')}</a>
			</div>
		{:else}
			<p>
				{t('assistant.setup.plan', {
					model: proposal.model,
					basis: proposal.basis === 'vram' && proposal.gpu?.vramBytes ? t('assistant.setup.basisVram', { size: gib(proposal.gpu.vramBytes) }) : t('assistant.setup.basisRam', { size: gib(proposal.ramBytes) }),
					where: proposal.gpu ? t('assistant.setup.gpu', { name: proposal.gpu.title }) : t('assistant.setup.cpu'),
				})}
			</p>
			{#if step || failed}
				<ol class="steps">
					{#each STEPS as s (s)}
						{@const index = STEPS.indexOf(s)}
						{@const current = step ? STEPS.indexOf(step) : failed ? STEPS.indexOf(failed.step) : -1}
						<li class:active={s === step} class:finished={index < current} class:failed={failed?.step === s}>
							<span class="state">{index < current ? t('assistant.setup.stepDone') : s === step ? t('assistant.setup.stepRunning') : failed?.step === s ? t('assistant.setup.stepFailed') : t('assistant.setup.stepOpen')}</span>
							{t(`assistant.setup.step.${s as Exclude<Step, 'done'>}`, { model: proposal.model })}
							{#if s === 'model' && step === 'model' && progress}
								<div class="progress" role="status">
									<span class="small">{progress.status || t('assistant.pulling')}{#if progress.total}{` · ${formatBytes(progress.completed)} / ${formatBytes(progress.total)}`}{/if}</span>
									{#if progress.total}<div class="bar"><span style:width="{Math.round((progress.completed / progress.total) * 100)}%"></span></div>{/if}
								</div>
							{/if}
						</li>
					{/each}
				</ol>
			{/if}
			{#if failed}
				<div class="notice-error" role="alert">
					{failed.text}{#if failed.detail}<pre class="mono small">{failed.detail}</pre>{/if}
				</div>
			{/if}
			{#if step === 'model'}<p class="muted small">{t('assistant.setup.keepOpen')}</p>{/if}
			<div class="row-actions">
				<button type="button" class="btn btn-primary" disabled={!!step} onclick={run}>{t(failed ? 'ui.retry' : 'assistant.setup.button')}</button>
			</div>
		{/if}
	{/if}
</section>

<style>
	section {
		font-size: 0.875rem;
	}
	.steps {
		display: grid;
		gap: 6px;
		margin: 0;
		padding-left: 1.25rem;
	}
	.steps li.finished {
		color: var(--muted);
	}
	.steps li.active {
		font-weight: 600;
	}
	.steps li.failed {
		color: var(--status-error);
	}
	.state {
		display: inline-block;
		min-width: 6.5rem;
		font-size: 0.75rem;
		color: var(--muted);
	}
	.small {
		font-size: 0.75rem;
	}
	pre {
		white-space: pre-wrap;
		max-height: 12rem;
		overflow: auto;
	}
	.ok::before {
		content: '● ';
		color: var(--status-ok);
	}
	.progress {
		display: grid;
		gap: 6px;
		max-width: 36rem;
		margin-top: 4px;
		font-weight: 400;
	}
	.bar {
		height: 6px;
		border-radius: var(--radius-bar);
		background: var(--track);
		overflow: hidden;
	}
	.bar span {
		display: block;
		height: 100%;
		background: var(--accent);
	}
</style>
