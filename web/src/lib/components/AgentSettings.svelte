<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '#lib/api.ts';
	import { t, errorText } from '#lib/i18n/index.svelte.ts';
	import type { AssistantSettings, ModelProfile } from '#lib/assistant.svelte.ts';
	import Dialog from './Dialog.svelte';
	let { settings = $bindable() }: { settings: AssistantSettings } = $props();
	type PromptInfo = {
		defaultPrompt: string;
		version: string;
		context: string;
		nasVersion: string;
		historyBytes: number;
		persistent: boolean;
	};
	let prompt = $state<PromptInfo | null>(null);
	let direct = $state(false);
	let profileName = $state('');
	let question = $state('');
	let scenario = $state('failure');
	let testing = $state(false);
	let result = $state('');
	let error = $state('');
	onMount(async () => {
		try {
			prompt = await api.get<PromptInfo>('/assistant/prompt');
			settings.systemPrompt ||= prompt!.defaultPrompt;
			settings.execution ||= 'confirm';
		} catch {
			error = errorText('internal');
		}
	});
	function mode(e: Event) {
		const value = (e.currentTarget as HTMLSelectElement).value;
		if (value === 'direct' && settings.execution !== 'direct') {
			direct = true;
			(e.currentTarget as HTMLSelectElement).value = settings.execution || 'confirm';
		} else settings.execution = value as 'confirm' | 'direct';
	}
	function saveProfile() {
		if (!profileName.trim()) return;
		settings.profiles = [
			...(settings.profiles ?? []).filter((p: ModelProfile) => p.name !== profileName.trim()),
			{
				name: profileName.trim(),
				model: settings.model,
				prompt: settings.systemPrompt ?? '',
				numCtx: settings.numCtx,
				temperature: settings.temperature,
			},
		].slice(-20);
		profileName = '';
	}
	async function probe() {
		testing = true;
		result = '';
		error = '';
		try {
			const answer = await api.post<{
				toolCalled: boolean;
				answer: string;
				note: string;
				saved?: boolean;
				durationMs?: number;
			}>('/assistant/probe', { settings, question, scenario });
			result = `${t(answer.toolCalled ? 'agent.probeCalled' : 'agent.probeMissing')}\n\n${answer.answer}\n\n${t('agent.probeNote')}\n${t(answer.saved ? 'agent.probeSaved' : 'agent.probeUnsaved')}\n${answer.durationMs ?? 0} ms`;
		} catch (e) {
			error = e instanceof Error ? e.message : errorText('internal');
		} finally {
			testing = false;
		}
	}
</script>

<section class="tile panel">
	<h2 class="tile-title">{t('agent.execution')}</h2>
	<label
		>{t('agent.mode')}<select class="field" value={settings.execution || 'confirm'} onchange={mode}
			><option value="confirm">{t('agent.confirm')}</option><option value="direct"
				>{t('agent.direct')}</option
			></select
		></label
	>
	<p class="muted">{t('agent.executionHint')}</p>
	<label class="check"
		><input type="checkbox" bind:checked={settings.paused} />{t('agent.globalPause')}</label
	>
	<p class="muted">{t('agent.unlimitedHint')}</p>
</section>
<section class="tile panel">
	<h2 class="tile-title">{t('agent.promptTitle')}</h2>
	<p class="muted">{t('agent.promptHint')}</p>
	<textarea
		class="field mono"
		rows="16"
		maxlength="24000"
		bind:value={settings.systemPrompt}
		aria-label={t('agent.promptTitle')}></textarea>
	<div class="row">
		<button
			type="button"
			class="btn"
			onclick={() => {
				if (prompt) {
					settings.systemPrompt = prompt.defaultPrompt;
					settings.promptVersion = prompt.version;
				}
			}}>{t('agent.restorePrompt')}</button
		><span class="muted">{prompt?.version ?? ''}</span>
	</div>
	<details>
		<summary>{t('agent.standardPrompt')}</summary>
		<pre>{prompt?.defaultPrompt ?? ''}</pre>
	</details>
	<details>
		<summary>{t('agent.contextPreview')}</summary>
		<p class="muted">{t('agent.contextHint')}</p>
		<pre>{prompt?.context ?? ''}</pre>
	</details>
	{#each settings.promptHistory ?? [] as old, i (i)}<details>
			<summary>{t('agent.revision', { count: i + 1 })}</summary>
			<pre>{old || prompt?.defaultPrompt || ''}</pre>
			<button
				type="button"
				class="btn"
				onclick={() => (settings.systemPrompt = old || prompt?.defaultPrompt || '')}
				>{t('agent.useRevision')}</button
			>
		</details>{/each}
	<label
		>{t('agent.preferences')}<textarea
			class="field"
			rows="4"
			maxlength="8000"
			bind:value={settings.memory}></textarea></label
	>
	<p class="muted">{t('agent.preferencesHint')}</p>
	<h3 class="divided">{t('agent.profiles')}</h3>
	{#each settings.profiles ?? [] as p, i (i)}<div class="row">
			<span>{p.name} · {p.model}</span><button
				type="button"
				class="btn"
				onclick={() => {
					settings.model = p.model;
					settings.systemPrompt = p.prompt;
					settings.numCtx = p.numCtx;
					settings.temperature = p.temperature;
				}}>{t('agent.apply')}</button
			><button
				type="button"
				class="btn"
				onclick={() =>
					(settings.profiles = settings.profiles?.filter((_: ModelProfile, n: number) => n !== i))}
				>{t('agent.removeProfile')}</button
			>
		</div>{/each}
	<div class="row">
		<input
			class="field"
			bind:value={profileName}
			maxlength="80"
			aria-label={t('agent.profileName')}
			placeholder={t('agent.profileName')}
		/><button type="button" class="btn" onclick={saveProfile} disabled={!profileName.trim()}
			>{t('agent.saveProfile')}</button
		>
	</div>
	<h3 class="divided">{t('agent.taskModels')}</h3>
	<p class="muted">{t('agent.taskModelsHint')}</p>
	<label>{t('agent.voiceModel')}<input class="field mono" bind:value={settings.voiceModel} /></label
	><label
		>{t('agent.documentModel')}<input
			class="field mono"
			bind:value={settings.documentModel}
		/></label
	><label
		>{t('agent.planningModel')}<input
			class="field mono"
			bind:value={settings.planningModel}
		/></label
	>
	<label class="check"><input type="checkbox" bind:checked={settings.ocr} />{t('agent.ocr')}</label>
	<p class="muted">{t('agent.ocrHint')}</p>
	<h3 class="divided">{t('agent.probe')}</h3>
	<p class="muted">{t('agent.probeHint')}</p>
	<label
		>{t('agent.probeScenario')}<select class="field" bind:value={scenario}
			><option value="failure">{t('agent.probeFailure')}</option><option value="ambiguous"
				>{t('agent.probeAmbiguous')}</option
			><option value="partial">{t('agent.probePartial')}</option><option value="untrusted"
				>{t('agent.probeUntrusted')}</option
			></select
		></label
	>
	<textarea
		class="field"
		rows="2"
		bind:value={question}
		aria-label={t('agent.testQuestion')}
		placeholder={t('agent.testQuestion')}></textarea>
	<button type="button" class="btn" onclick={probe} disabled={testing || !settings.model}
		>{t(testing ? 'agent.testing' : 'agent.test')}</button
	>
	{#if result}<pre role="status">{result}</pre>{/if}
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
</section>
<section class="tile panel">
	<h2 class="tile-title">{t('agent.notifications')}</h2>
	<p class="muted">
		{t('agent.notificationsHint')}
		<a href="/account#personal-notifications">{t('agent.notificationsLink')}</a>
	</p>
	<label class="check"
		><input type="checkbox" bind:checked={settings.pushApproval} />{t('agent.pushApproval')}</label
	>
	<p class="muted">{t('agent.pushApprovalHint')}</p>
</section>
<section class="tile panel">
	<h2 class="tile-title">{t('agent.privacy')}</h2>
	<p class="muted">
		{t('agent.historyUsage', { size: ((prompt?.historyBytes ?? 0) / 1048576).toFixed(2) })}
	</p>
	<label class="check"
		><input type="checkbox" bind:checked={settings.ephemeral} />{t('agent.ephemeral')}</label
	>
	<p class="muted">{t('agent.ephemeralHint')}</p>
	<label
		>{t('agent.retention')}<input
			class="field"
			type="number"
			min="0"
			max="36500"
			bind:value={settings.retentionDays}
		/></label
	>
	<p class="muted">
		{t('agent.privacyHint', {
			address:
				settings.provider === 'anthropic'
					? 'api.anthropic.com'
					: settings.provider === 'openai'
						? settings.openaiBase || ''
						: settings.ollama,
		})}
	</p>
</section>
{#if direct}<Dialog title={t('agent.direct')} onclose={() => (direct = false)}
		><p>{t('agent.directWarning')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (direct = false)}
				>{t('common.cancel')}</button
			><button
				type="button"
				class="btn btn-danger"
				onclick={() => {
					settings.execution = 'direct';
					direct = false;
				}}>{t('agent.enableDirect')}</button
			>
		</div></Dialog
	>{/if}

<style>
	section {
		font-size: 0.875rem;
	}
	h3,
	p {
		margin: 0;
	}
	h3 {
		font-size: 0.9375rem;
	}
	label {
		display: grid;
		gap: 8px;
		max-width: 36rem;
	}
	.check,
	.row {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: 10px;
	}
	textarea {
		width: 100%;
		resize: vertical;
	}
	pre {
		white-space: pre-wrap;
		overflow-wrap: anywhere;
		max-height: 28rem;
		overflow: auto;
		font-size: 0.8125rem;
	}
	select {
		max-width: 36rem;
	}
	summary {
		cursor: pointer;
	}
	input[type='number'] {
		max-width: 12rem;
	}
</style>
