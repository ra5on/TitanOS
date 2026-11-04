<script lang="ts">
	// "Import compose file" and the Docker settings share one form.
	import Dialog from '#lib/components/Dialog.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';

	let {
		kind,
		draft = $bindable(),
		volumes,
		appdata,
		busy,
		error,
		onclose,
		onsubmit,
	}: {
		kind: 'import' | 'settings';
		draft: { name: string; compose: string; enabled: boolean; volume: string };
		volumes: string[];
		/** Folder the app data go to, e.g. /mnt/data/appdata. */
		appdata: string;
		busy: boolean;
		error: { text: string; detail: string } | null;
		onclose: () => void;
		onsubmit: (event: SubmitEvent) => void;
	} = $props();
</script>

<Dialog
	title={t(kind === 'import' ? 'docker.importTitle' : 'docker.settingsTitle')}
	wide={kind === 'import'}
	locked={busy}
	{onclose}
>
	<form {onsubmit}>
		{#if kind === 'import'}
			<p class="notice-warn">{t('docker.importWarning')}</p>
			<label
				><span>{t('docker.importName')}</span>
				<!-- svelte-ignore a11y_autofocus -->
				<input
					class="field mono name-field"
					type="text"
					bind:value={draft.name}
					maxlength="32"
					autocapitalize="none"
					spellcheck="false"
					required
					autofocus
				/>
			</label>
			<label
				><span>{t('docker.importCompose')}</span>
				<textarea
					class="field mono"
					rows="14"
					bind:value={draft.compose}
					spellcheck="false"
					required
					placeholder={'services:\n  web:\n    image: …'}></textarea>
				<small
					>{t('docker.importHint', {
						path: `${appdata}/${draft.name || '<name>'}`,
					})}</small
				>
			</label>
		{:else}
			<label class="check"
				><input type="checkbox" bind:checked={draft.enabled} /><span>{t('docker.enabled')}</span
				></label
			>
			<label
				><span>{t('docker.volume')}</span>
				<select class="field" bind:value={draft.volume} disabled={!draft.enabled}
					>{#each volumes as volume (volume)}<option value={volume}>{volume}</option>{/each}</select
				>
			</label>
			<p class="muted note">{t('docker.disableNote')}</p>
		{/if}
		{#if error}
			<div class="notice-error" role="alert">
				{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}
			</div>
		{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={onclose} disabled={busy}
				>{t('common.cancel')}</button
			>
			<button type="submit" class="btn btn-primary" disabled={busy}
				>{t(kind === 'import' ? 'docker.importSubmit' : 'docker.save')}</button
			>
		</div>
	</form>
</Dialog>

<style>
	form {
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: var(--text-md);
	}
	.check {
		flex-direction: row;
		align-items: center;
		gap: 10px;
	}
	.check input {
		width: 18px;
		height: 18px;
		margin: 0;
	}
	.name-field {
		max-width: 18rem;
	}
	textarea {
		height: auto;
		padding: 0.625rem 0.75rem;
		font-size: var(--text-base);
		line-height: 1.5;
		resize: vertical;
	}
	small,
	.note {
		font-size: var(--text-sm);
		color: var(--muted);
	}
	pre {
		margin: var(--space-2) 0 0;
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		white-space: pre-wrap;
		word-break: break-word;
	}
</style>
