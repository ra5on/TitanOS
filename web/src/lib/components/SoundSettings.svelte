<script lang="ts">
	// The sound of a new message, for this account. One row of "Mein Konto"
	// (PLAN 78-5): tone, volume and occasion are its parts.
	import SettingRow from './settings/SettingRow.svelte';
	import SettingSub from './settings/SettingSub.svelte';
	import { api } from '#lib/api.ts';
	import { sound, armSound, playSound } from '#lib/sound.svelte.ts';
	import { tones, sanitizeSound } from '#lib/sounds.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	let draft = $state(sanitizeSound(sound.settings));
	let error = $state('');
	let saved = $state(false);
	let busy = $state(false);
	// What the row says closed: the stored tone.
	const summary = $derived(
		t(sound.settings.tone === 'none' ? 'sound.none' : `sound.${sound.settings.tone}`),
	);
	async function save() {
		busy = true;
		error = '';
		saved = false;
		try {
			const value = sanitizeSound(draft);
			await api.put('/preferences/sound', value);
			sound.settings = value;
			saved = true;
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
	async function preview() {
		error = '';
		const previous = sound.settings;
		sound.settings = sanitizeSound(draft);
		const ready = await armSound();
		sound.settings = previous;
		if (!ready || !playSound(sanitizeSound(draft))) error = t('sound.blocked');
	}
</script>

<SettingRow id="sounds" icon="volume" title={t('sound.title')} desc={t('sound.hint')} {summary}>
	<SettingSub title={t('sound.tone')} for="sound-tone">
		{#snippet control()}
			<select id="sound-tone" class="field" bind:value={draft.tone} onchange={() => (saved = false)}
				><option value="none">{t('sound.none')}</option>{#each tones as tone}<option value={tone}
						>{t(`sound.${tone}`)}</option
					>{/each}</select
			>
		{/snippet}
	</SettingSub>
	<SettingSub title="{t('sound.volume')} · {draft.volume}%" for="sound-volume">
		{#snippet control()}
			<input
				id="sound-volume"
				type="range"
				min="0"
				max="100"
				step="5"
				bind:value={draft.volume}
				oninput={() => (saved = false)}
			/>
		{/snippet}
	</SettingSub>
	<SettingSub title={t('sound.when')} for="sound-when">
		{#snippet control()}
			<select
				id="sound-when"
				class="field"
				bind:value={draft.severity}
				onchange={() => (saved = false)}
				><option value="important">{t('sound.important')}</option><option value="all"
					>{t('sound.all')}</option
				></select
			>
		{/snippet}
	</SettingSub>
	<div class="srow-block actions">
		<button
			class="btn"
			type="button"
			onclick={preview}
			disabled={draft.tone === 'none' || draft.volume === 0}>{t('sound.preview')}</button
		><button class="btn btn-primary" type="button" onclick={save} disabled={busy}
			>{t('common.save')}</button
		>{#if saved}<span role="status">{t('sound.saved')}</span>{/if}
	</div>
	{#if error}<div class="srow-block">
			<p class="notice-error" role="alert">{error}</p>
		</div>{/if}
</SettingRow>

<style>
	input[type='range'] {
		width: 12rem;
		max-width: 100%;
	}
</style>
