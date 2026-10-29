<script lang="ts">
	// The assistant: on or off, which provider runs the model (Ollama or a
	// cloud provider), which models it has, who may chat, the web search,
	// where chats are kept, and additions to what the model is told.
	import { onMount } from 'svelte';
	import { api, ApiError } from '#lib/api.ts';
	import { AREAS, loadAssistant, loadModels, type AssistantModel, type AssistantSettings } from '#lib/assistant.svelte.ts';
	import { pullModel } from '#lib/assistant-pull.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import OllamaSetup from './OllamaSetup.svelte';
	import AgentSettings from '#lib/components/AgentSettings.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { formatBytes, formatNumber } from '#lib/format.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { storage } from '#lib/storage.svelte.ts';

	let settings = $state<AssistantSettings | null>(null);
	let models = $state<AssistantModel[] | null>(null);
	let found = $state<{ container: string; address: string; running: boolean }[]>([]);
	let loadError = $state('');
	let checking = $state(false);
	let checkError = $state<{ text: string; detail: string } | null>(null);
	let saving = $state(false);
	let saved = $state(false);
	let error = $state('');
	let pullName = $state('');
	let pulling = $state<{ status: string; total: number; completed: number } | null>(null);
	let pullError = $state('');
	let removing = $state<string | null>(null);
	// Keys typed in; they go to the NAS with the next check or save and are
	// never shown again. Empty keeps the stored key.
	let anthropicKey = $state('');
	let openaiKey = $state('');
	let clearAnthropic = $state(false);
	let clearOpenai = $state(false);
	let setupActive = $state(false);

	const provider = $derived(settings?.provider || 'ollama');
	const cloud = $derived(provider !== 'ollama');

	const problem = (err: unknown) => errorText(err instanceof ApiError ? err.code : 'internal');

	// What the assistant may do where, as titand decides it without a setting.
	const DEFAULT_ACCESS: Record<string, 'off' | 'read' | 'write'> = {
		files: 'write', docker: 'write', vms: 'write', storage: 'write', shares: 'write', backups: 'write', accounts: 'read', system: 'read', updates: 'read', home: 'off', plugins: 'off', network: 'off'
	};


	async function load() {
		loadError = '';
		try {
			settings = await api.get<AssistantSettings>('/assistant');
			settings.areas = Object.fromEntries(AREAS.map((a) => [a, settings!.areas?.[a] ?? DEFAULT_ACCESS[a]]));
			settings.maxSteps ??= 0;
			if (!settings.provider) settings.provider = 'ollama';
			settings.openaiBase ??= '';
			check();
			api.get<{ apps: typeof found }>('/assistant/ollama-apps').then((res) => (found = res.apps)).catch(() => (found = []));
		} catch (err) {
			loadError = problem(err);
		}
	}
	onMount(load);
	$effect(() => {
		if (saved) {
			const timer = setTimeout(() => (saved = false), 5000);
			return () => clearTimeout(timer);
		}
	});

	/** Asks the provider in the fields which models it has. */
	async function check() {
		if (!settings) return;
		checking = true;
		checkError = null;
		try {
			models = (
				await api.post<{ models: AssistantModel[] }>('/assistant/models', {
					provider,
					ollama: settings.ollama,
					openaiBase: settings.openaiBase ?? '',
					anthropicKey: clearAnthropic ? '' : anthropicKey,
					openaiKey: clearOpenai ? '' : openaiKey
				})
			).models;
		} catch (err) {
			models = null;
			const code = err instanceof ApiError ? err.code : '';
			checkError = { text: problem(err), detail: code === 'ollama_failed' || code === 'provider_failed' ? (err as ApiError).message : '' };
		}
		checking = false;
	}

	/** Another provider has other models; Anthropic starts with the default. */
	function switchProvider(next: string) {
		if (!settings) return;
		settings.provider = next as AssistantSettings['provider'];
		settings.model = next === 'anthropic' ? 'claude-sonnet-5-5' : '';
		models = null;
		checkError = null;
		check();
	}

	async function save(event: SubmitEvent) {
		event.preventDefault();
		if (!settings || saving) return;
		saving = true;
		saved = false;
		error = '';
		try {
			const value = settings.temperature;
			const areas = settings.areas;
			settings = await api.put<AssistantSettings>('/assistant', {
				...settings,
				numCtx: Number(settings.numCtx) || 0,
				maxSteps: 0,
				temperature: value,
				anthropicKey: clearAnthropic ? '' : anthropicKey,
				openaiKey: clearOpenai ? '' : openaiKey,
				clearAnthropicKey: clearAnthropic,
				clearOpenaiKey: clearOpenai
			});
			settings.areas = { ...areas, ...settings.areas };
			anthropicKey = openaiKey = '';
			clearAnthropic = clearOpenai = false;
			// The buttons in the header follow the new settings.
			await loadAssistant();
			loadModels();
			saved = true;
		} catch (err) {
			error = problem(err);
		}
		saving = false;
	}

	/** Loads a model into Ollama; the NAS reports the progress as it comes. */
	async function pull(event: SubmitEvent) {
		event.preventDefault();
		const model = pullName.trim();
		if (!model || pulling) return;
		pullError = '';
		pulling = { status: '', total: 0, completed: 0 };
		try {
			await pullModel(model, (p) => (pulling = p));
			pullName = '';
		} catch (err) {
			pullError = err instanceof ApiError && err.message ? err.message : problem(err);
		}
		pulling = null;
		await check();
	}

	async function removeModel(name: string) {
		try {
			await api.post('/assistant/models/delete', { model: name });
			removing = null;
			if (settings?.model === name) settings.model = '';
			await check();
		} catch (err) {
			error = problem(err);
			removing = null;
		}
	}

	const usable = $derived((models ?? []).filter((m) => m.capabilities.length === 0 || m.capabilities.includes('tools')));
	const choices = $derived(settings?.model && !usable.some((m) => m.name === settings?.model) ? [settings.model, ...usable.map((m) => m.name)] : usable.map((m) => m.name));
	const volumes = $derived((storage.status?.volumes ?? []).map((v) => v.name));
	const capability = (name: string) => t(name === 'tools' ? 'assistant.capTools' : name === 'vision' ? 'assistant.capVision' : name === 'thinking' ? 'assistant.capThinking' : 'assistant.capOther', { name });
</script>

<PageHead title={t('assistant.settingsTitle')} parent={{ href: '/settings', label: t('nav.settings') }} />

{#if loadError}<div class="notice-error" role="alert">{loadError} <button class="btn" onclick={load}>{t('ui.retry')}</button></div>{/if}
{#if settings}
	<form onsubmit={save} class="sections">
		<section class="tile panel">
			<label class="check">
				<input type="checkbox" bind:checked={settings.enabled} />
				<span>{t('assistant.enable')}</span>
			</label>
			<p class="muted">{t('assistant.settingsText')}</p>

			<label class="stack">
				<span>{t('assistant.provider')}</span>
				<select class="field" value={settings.provider} onchange={(e) => switchProvider(e.currentTarget.value)}>
					<option value="ollama">{t('assistant.provider.ollama')}</option>
					<option value="anthropic">{t('assistant.provider.anthropic')}</option>
					<option value="openai">{t('assistant.provider.openai')}</option>
				</select>
			</label>
			{#if cloud}
				<div class="notice-warn" role="note">{t('assistant.cloudNotice')}</div>
			{/if}

			{#if provider === 'ollama'}
				<label class="stack">
					<span>{t('assistant.ollama')}</span>
					<div class="row">
						<input class="field mono" bind:value={settings.ollama} aria-label={t('assistant.ollama')} placeholder="http://127.0.0.1:11434" spellcheck="false" autocapitalize="none" required />
						<button type="button" class="btn" onclick={check} disabled={checking}>{t(checking ? 'assistant.checking' : 'assistant.check')}</button>
					</div>
					<span class="muted small">{t('assistant.ollamaHint')}</span>
				</label>
				{#each found.filter((f) => f.address !== settings?.ollama) as app (app.container)}
					<p class="small">
						{t('assistant.foundApp', { name: app.container, address: app.address })}
						<button type="button" class="btn small-btn" onclick={() => { settings!.ollama = app.address; check(); }}>{t('assistant.useApp')}</button>
					</p>
				{/each}
			{:else}
				{#if provider === 'openai'}
					<label class="stack">
						<span>{t('assistant.openaiBase')}</span>
						<input class="field mono" bind:value={settings.openaiBase} placeholder="https://api.openai.com/v1" spellcheck="false" autocapitalize="none" required />
						<span class="muted small">{t('assistant.openaiBaseHint')}</span>
					</label>
				{/if}
				{@const keySet = provider === 'anthropic' ? settings.anthropicKeySet : settings.openaiKeySet}
				<label class="stack">
					<span>{t(provider === 'anthropic' ? 'assistant.anthropicKey' : 'assistant.openaiKey')}</span>
					<div class="row">
						{#if provider === 'anthropic'}
							<input class="field mono" type="password" bind:value={anthropicKey} disabled={clearAnthropic} placeholder={keySet ? t('assistant.keyKeep') : 'sk-ant-…'} autocomplete="off" spellcheck="false" autocapitalize="none" />
						{:else}
							<input class="field mono" type="password" bind:value={openaiKey} disabled={clearOpenai} placeholder={keySet ? t('assistant.keyKeep') : 'sk-…'} autocomplete="off" spellcheck="false" autocapitalize="none" />
						{/if}
						<button type="button" class="btn" onclick={check} disabled={checking}>{t(checking ? 'assistant.checking' : 'assistant.check')}</button>
					</div>
					<span class="muted small">{t(keySet ? 'assistant.keySet' : 'assistant.keyNone')} {t(provider === 'anthropic' ? 'assistant.anthropicKeyHint' : 'assistant.openaiKeyHint')}</span>
				</label>
				{#if keySet}
					<label class="check">
						{#if provider === 'anthropic'}
							<input type="checkbox" bind:checked={clearAnthropic} />
						{:else}
							<input type="checkbox" bind:checked={clearOpenai} />
						{/if}
						<span>{t('assistant.keyRemove')}</span>
					</label>
				{/if}
			{/if}
			{#if checkError}
				<div class="notice-warn" role="alert">{checkError.text}{#if checkError.detail}<br /><span class="mono small">{checkError.detail}</span>{/if}</div>
			{:else if models}
				<p class="ok" role="status">{t(cloud ? 'assistant.reachableCloud' : 'assistant.reachable', { count: models.length })}</p>
			{/if}

			<label class="stack">
				<span>{t('assistant.defaultModel')}</span>
				{#if cloud}
					<input class="field mono" bind:value={settings.model} list="assistant-cloud-models" placeholder={provider === 'anthropic' ? 'claude-sonnet-5-5' : 'gpt-…'} spellcheck="false" autocapitalize="none" />
					<datalist id="assistant-cloud-models">
						{#each choices as model (model)}<option value={model}></option>{/each}
					</datalist>
					<span class="muted small">{t('assistant.modelCloudHint')}</span>
				{:else}
					<select class="field" bind:value={settings.model}>
						<option value="">{t('assistant.pickModel')}</option>
						{#each choices as model (model)}<option value={model}>{model}</option>{/each}
					</select>
					<span class="muted small">{t('assistant.modelHint')}</span>
				{/if}
			</label>
		</section>

		<!-- Once clicked, the card stays to show its steps and the result. -->
		{#if provider === 'ollama' && (setupActive || (checkError && !checking && found.length === 0))}
			<OllamaSetup onstart={() => (setupActive = true)} ondone={load} />
		{/if}

		<section class="tile panel" aria-labelledby="assistant-access">
			<h2 class="tile-title" id="assistant-access">{t('assistant.access')}</h2>
			<label class="check">
				<input type="checkbox" bind:checked={settings.users} />
				<span>{t('assistant.allowUsers')}</span>
			</label>
			<p class="muted">{t('assistant.allowUsersText')}</p>
			<label class="check">
				<input type="checkbox" bind:checked={settings.web} />
				<span>{t('assistant.web')}</span>
			</label>
			<p class="muted">{t('assistant.webText')}</p>
			<label class="check indent">
				<input type="checkbox" bind:checked={settings.webUsers} disabled={!settings.web || !settings.users} />
				<span>{t('assistant.webUsers')}</span>
			</label>
			<p class="muted">{t('assistant.actionsText')}</p>
			<div class="table">
				<table class="areas">
					<thead><tr><th>{t('assistant.area')}</th><th>{t('assistant.areaAccess')}</th></tr></thead>
					<tbody>
						{#each AREAS as area (area)}
							<tr>
								<td><strong>{t(`assistant.area.${area}`)}</strong><br /><span class="muted small">{t(`assistant.areaText.${area}`)}</span></td>
								<td>
									<select class="field" bind:value={settings.areas[area]} aria-label={t(`assistant.area.${area}`)}>
										<option value="off">{t('assistant.access.off')}</option>
										<option value="read">{t('assistant.access.read')}</option>
										<option value="write">{t('assistant.access.write')}</option>
									</select>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
			<p class="muted small">{t('assistant.areasHint')}</p>
		</section>

		<section class="tile panel" aria-labelledby="assistant-storage">
			<h2 class="tile-title" id="assistant-storage">{t('assistant.storageTitle')}</h2>
			<label class="stack">
				<span>{t('assistant.volume')}</span>
				<select class="field" bind:value={settings.volume}>
					<option value="">{t('assistant.volumeFirst')}</option>
					{#each volumes as volume (volume)}<option value={volume}>{volume}</option>{/each}
					{#if settings.volume && !volumes.includes(settings.volume)}<option value={settings.volume}>{settings.volume}</option>{/if}
				</select>
				<span class="muted small">{t('assistant.volumeHint')}</span>
			</label>
		</section>

		<section class="tile panel" aria-labelledby="assistant-advanced">
			<h2 class="tile-title" id="assistant-advanced">{t('assistant.advanced')}</h2>
			<label class="stack">
				<span>{t('assistant.prompt')}</span>
				<textarea class="field" bind:value={settings.prompt} rows="4" maxlength="24000" placeholder={t('assistant.promptPlaceholder')}></textarea>
				<span class="muted small">{t('assistant.promptHint')}</span>
			</label>
			<div class="pair">
				<label class="stack">
					<span>{t('assistant.numCtx')}</span>
					<input class="field mono" type="number" min="0" max="1048576" step="1024" bind:value={settings.numCtx} />
					<span class="muted small">{t('assistant.numCtxHint')}</span>
				</label>
				<label class="stack">
					<span>{t('assistant.temperature')}</span>
					<input class="field mono" type="number" min="0" max="2" step="0.1" bind:value={settings.temperature} placeholder="–" />
					<span class="muted small">{t('assistant.temperatureHint')}</span>
				</label>
			</div>
		</section>

		<AgentSettings bind:settings />

		<div class="actions">
			<button type="submit" class="btn btn-primary" disabled={saving}>{t('assistant.save')}</button>
			{#if saving || saved}<span role="status">{t(saving ? 'ui.saving' : 'ui.saved')}</span>{/if}
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		</div>
	</form>

	{#if !cloud}
	<section class="tile panel" aria-labelledby="assistant-models">
		<h2 class="tile-title" id="assistant-models">{t('assistant.models')}</h2>
		{#if models}
			<div class="table">
				<table>
					<thead><tr><th>{t('assistant.modelName')}</th><th>{t('assistant.modelSize')}</th><th>{t('assistant.modelCan')}</th><th><span class="sr-only">{t('assistant.modelActions')}</span></th></tr></thead>
					<tbody>
						{#each models as model (model.name)}
							<tr>
								<td class="mono">{model.name}{#if model.parameters}<span class="muted">{` · ${model.parameters}`}</span>{/if}</td>
								<td class="mono">{formatBytes(model.sizeBytes)}{#if model.contextLength}<span class="muted">{` · ${t('assistant.context', { count: formatNumber(model.contextLength) })}`}</span>{/if}</td>
								<td>
									{#each model.capabilities.filter((c) => c !== 'completion') as cap (cap)}<span class="chip">{capability(cap)}</span>{/each}
									{#if model.capabilities.length > 0 && !model.capabilities.includes('tools')}<span class="warn">{t('assistant.noToolsModel')}</span>{/if}
								</td>
								<td class="end"><button type="button" class="btn small-btn" onclick={() => (removing = model.name)}>{t('assistant.delete')}</button></td>
							</tr>
						{:else}
							<tr><td colspan="4" class="muted">{t('assistant.noModels')}</td></tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
		<form class="row pull" onsubmit={pull}>
			<input class="field mono" bind:value={pullName} placeholder="qwen3:8b" aria-label={t('assistant.pullName')} spellcheck="false" autocapitalize="none" disabled={!!pulling} />
			<button type="submit" class="btn" disabled={!!pulling || !pullName.trim() || !models}>{t('assistant.pull')}</button>
		</form>
		{#if pulling}
			<div class="progress" role="status">
				<span class="small">{pulling.status || t('assistant.pulling')}{#if pulling.total}{` · ${formatBytes(pulling.completed)} / ${formatBytes(pulling.total)}`}{/if}</span>
				{#if pulling.total}<div class="bar"><span style:width="{Math.round((pulling.completed / pulling.total) * 100)}%"></span></div>{/if}
			</div>
		{/if}
		{#if pullError}<p class="notice-error" role="alert">{pullError}</p>{/if}
		<p class="muted small">{t('assistant.pullHint')} {t('assistant.pullChangeHint')}</p>
	</section>
	{/if}
{:else if !loadError}
	<p class="loading" role="status">{t('common.loading')}</p>
{/if}

<section class="tile panel" aria-labelledby="assistant-privacy">
	<h2 class="tile-title" id="assistant-privacy">{t('assistant.privacyTitle')}</h2>
	<p class="muted">{t('assistant.privacy')}</p>
</section>

{#if removing}
	{@const name = removing}
	<Dialog title={t('assistant.deleteModel')} onclose={() => (removing = null)}>
		<p>{t('assistant.deleteModelText', { name })}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (removing = null)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-danger" onclick={() => removeModel(name)}>{t('assistant.delete')}</button>
		</div>
	</Dialog>
{/if}

<style>
	.sections {
		display: flex;
		flex-direction: column;
		gap: var(--gap);
	}
	section {
		font-size: 0.875rem;
	}
	label.indent {
		padding-left: 28px;
	}
	.row {
		display: flex;
		gap: 8px;
		max-width: 36rem;
	}
	.row input {
		flex: 1;
		min-width: 0;
	}
	.pair {
		display: flex;
		flex-wrap: wrap;
		gap: 14px 24px;
	}
	.pair label {
		flex: 1 1 14rem;
	}
	textarea {
		resize: vertical;
		min-height: 90px;
	}
	.small {
		font-size: 0.75rem;
	}
	.small-btn {
		height: 28px;
		padding: 0 10px;
		font-size: 0.75rem;
	}
	.ok::before {
		content: '● ';
		color: var(--status-ok);
	}
	.warn {
		color: var(--status-warn);
		font-size: 0.75rem;
	}
	.actions {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 12px;
	}
	.table {
		overflow-x: auto;
	}
	table {
		width: 100%;
		border-collapse: collapse;
	}
	th,
	td {
		padding: 8px 10px;
		border-bottom: 1px solid var(--border);
		text-align: left;
		vertical-align: middle;
	}
	th {
		color: var(--muted);
		font-weight: 500;
		font-size: 0.75rem;
	}
	.areas td:last-child {
		width: 13rem;
	}
	td .chip {
		margin-right: 4px;
	}
	.end {
		text-align: right;
	}
	.progress {
		display: grid;
		gap: 6px;
		max-width: 36rem;
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
