<script lang="ts">
	import { onMount } from 'svelte';
	import { api } from '#lib/api.ts';
	import { t, apiErrorText } from '#lib/i18n/index.svelte.ts';
	type Profile = { local: string; cloud: string; keySet: boolean };
	let profile = $state<Profile | null>(null),
		models = $state<string[]>([]),
		problem = $state(''),
		message = $state(''),
		key = $state(''),
		forget = $state(false),
		busy = $state(false);
	onMount(async () => {
		try {
			profile = await api.get<Profile>('/personal-agent/model');
		} catch (e) {
			problem = apiErrorText(e);
		}
	});
	async function scan() {
		busy = true;
		problem = '';
		message = '';
		try {
			models = await api.get<string[]>('/personal-agent/models');
			message = t('papim.model.found', { count: models.length });
		} catch (e) {
			problem = apiErrorText(e);
		} finally {
			busy = false;
		}
	}
	async function save() {
		if (!profile) return;
		busy = true;
		problem = '';
		message = '';
		try {
			profile = await api.put<Profile>('/personal-agent/model', {
				local: profile.local,
				cloud: profile.cloud,
				key,
				forgetKey: forget,
			});
			key = '';
			forget = false;
			message = t('papim.model.saved');
		} catch (e) {
			problem = apiErrorText(e);
		} finally {
			busy = false;
		}
	}
</script>

<section class="model">
	<h3>{t('papim.model.title')}</h3>
	{#if profile}
		<label
			>{t('papim.model.local')}<input
				list="personal-ollama-models"
				bind:value={profile.local}
				placeholder={t('papim.model.auto')}
				maxlength="128"
			/></label
		>
		<datalist id="personal-ollama-models"
			>{#each models as model (model)}<option value={model}></option>{/each}</datalist
		>
		<button class="btn" disabled={busy} onclick={scan}>{t('papim.model.scan')}</button>
		<details>
			<summary>{t('papim.model.cloud')}</summary>
			<p>{t('papim.model.scope')}</p>
			<label>{t('papim.model.external')}<input maxlength="128" bind:value={profile.cloud} /></label>
			<label
				>{t('papim.model.key')}<input
					type="password"
					autocomplete="new-password"
					bind:value={key}
					placeholder={profile.keySet ? t('papim.model.stored') : ''}
				/></label
			>
			{#if profile.keySet}<label class="check"
					><input type="checkbox" bind:checked={forget} />{t('papim.model.forget')}</label
				>{/if}
		</details>
		<button class="btn primary" disabled={busy} onclick={save}>{t('common.save')}</button>
	{/if}
	{#if problem}<p role="alert">{problem}</p>{/if}{#if message}<p role="status">{message}</p>{/if}
</section>

<style>
	.model {
		display: grid;
		gap: 0.7rem;
		color: inherit;
	}
	.model h3,
	.model p {
		margin: 0;
	}
	.model label:not(.check) {
		display: grid;
		gap: 0.3rem;
	}
	.model input:not([type='checkbox']) {
		width: 100%;
		box-sizing: border-box;
		color: inherit;
		background: var(--surface, transparent);
		padding: 0.6rem;
		border: 1px solid var(--border);
		border-radius: 0.55rem;
	}
	.model button {
		justify-self: start;
	}
	.model summary {
		cursor: pointer;
	}
	.model details[open] {
		display: grid;
		gap: 0.6rem;
	}
	.model details[open] > :global(*) {
		margin-block: 0.45rem;
	}
	.model .check {
		display: flex;
		align-items: center;
		gap: 0.4rem;
	}
</style>
