<script lang="ts">
	// The chat with a model from Ollama: on or off, where Ollama is, which model.
	import { onMount } from 'svelte';
	import { api, ApiError } from '#lib/api.ts';
	import { assistant, type AssistantSettings } from '#lib/assistant.svelte.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';

	let settings = $state<AssistantSettings | null>(null);
	let models = $state<string[] | null>(null);
	let loadError = $state('');
	let checking = $state(false);
	let checkError = $state<{ text: string; detail: string } | null>(null);
	let saving = $state(false);
	let saved = $state(false);
	let error = $state('');

	const problem = (err: unknown) => errorText(err instanceof ApiError ? err.code : 'internal');

	async function load() {
		loadError = '';
		try {
			settings = await api.get<AssistantSettings>('/assistant');
			check();
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

	/** Asks Ollama at the address in the field which models it has. */
	async function check() {
		if (!settings) return;
		checking = true;
		checkError = null;
		try {
			models = (await api.post<{ models: string[] }>('/assistant/models', { ollama: settings.ollama })).models;
		} catch (err) {
			models = null;
			checkError = { text: problem(err), detail: err instanceof ApiError && err.code === 'ollama_failed' ? err.message : '' };
		}
		checking = false;
	}

	async function save(event: SubmitEvent) {
		event.preventDefault();
		if (!settings || saving) return;
		saving = true;
		saved = false;
		error = '';
		try {
			settings = await api.put<AssistantSettings>('/assistant', settings);
			// The header shows the button from now on, or no longer.
			assistant.settings = settings;
			assistant.models = models ?? [];
			saved = true;
		} catch (err) {
			error = problem(err);
		}
		saving = false;
	}

	const choices = $derived(settings?.model && !(models ?? []).includes(settings.model) ? [settings.model, ...(models ?? [])] : (models ?? []));
</script>

<PageHead title={t('assistant.settingsTitle')} parent={{ href: '/settings', label: t('nav.settings') }} />

<section class="tile">
	{#if loadError}<div class="notice-error" role="alert">{loadError} <button class="btn" onclick={load}>{t('ui.retry')}</button></div>{/if}
	{#if settings}
		<form onsubmit={save}>
			<label class="check">
				<input type="checkbox" bind:checked={settings.enabled} />
				<span>{t('assistant.enable')}</span>
			</label>
			<p class="muted">{t('assistant.settingsText')}</p>

			<label>
				<span>{t('assistant.ollama')}</span>
				<div class="row">
					<input class="field mono" bind:value={settings.ollama} aria-label={t('assistant.ollama')} placeholder="http://127.0.0.1:11434" spellcheck="false" autocapitalize="none" required />
					<button type="button" class="btn" onclick={check} disabled={checking}>{t(checking ? 'assistant.checking' : 'assistant.check')}</button>
				</div>
				<span class="muted small">{t('assistant.ollamaHint')}</span>
			</label>
			{#if checkError}
				<div class="notice-warn" role="alert">{checkError.text}{#if checkError.detail}<br /><span class="mono small">{checkError.detail}</span>{/if}</div>
			{:else if models}
				<p class="ok" role="status">{t('assistant.reachable', { count: models.length })}</p>
			{/if}

			<label>
				<span>{t('assistant.defaultModel')}</span>
				<select class="field" bind:value={settings.model}>
					<option value="">{t('assistant.pickModel')}</option>
					{#each choices as model (model)}<option value={model}>{model}</option>{/each}
				</select>
				<span class="muted small">{t('assistant.modelHint')}</span>
			</label>

			<div class="actions">
				<button type="submit" class="btn btn-primary" disabled={saving}>{t('assistant.save')}</button>
				{#if saving || saved}<span role="status">{t(saving ? 'ui.saving' : 'ui.saved')}</span>{/if}
			</div>
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		</form>
	{:else if !loadError}
		<p role="status">{t('common.loading')}</p>
	{/if}
</section>

<section class="tile">
	<h2 class="tile-title">{t('assistant.privacyTitle')}</h2>
	<p class="muted">{t('assistant.privacy')}</p>
</section>

<style>
	section {
		display: grid;
		gap: 14px;
		padding: var(--tile-pad);
		font-size: 0.875rem;
	}
	form {
		display: grid;
		gap: 14px;
	}
	label {
		display: grid;
		gap: 8px;
		max-width: 36rem;
	}
	label.check {
		display: flex;
		align-items: center;
		gap: 10px;
		font-weight: 600;
	}
	.check input {
		width: 18px;
		height: 18px;
		margin: 0;
	}
	.row {
		display: flex;
		gap: 8px;
	}
	.row input {
		flex: 1;
		min-width: 0;
	}
	p {
		margin: 0;
		max-width: 60rem;
	}
	.small {
		font-size: 0.75rem;
	}
	.ok::before {
		content: '● ';
		color: var(--status-ok);
	}
	.actions {
		display: flex;
		align-items: center;
		gap: 12px;
	}
</style>
