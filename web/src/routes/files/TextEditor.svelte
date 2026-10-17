<script lang="ts">
	import { api, ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';

	type Props = { root: string; path: string; onclose: () => void };
	let { root, path, onclose }: Props = $props();

	let text = $state('');
	let original = $state('');
	let loaded = $state(false);
	let error = $state('');
	let busy = $state(false);
	let saved = $state(false);

	$effect(() => {
		fetch(`/api/v1/files/download?root=${encodeURIComponent(root)}&path=${encodeURIComponent(path)}`)
			.then((res) => (res.ok ? res.text() : Promise.reject(new Error('failed'))))
			.then((body) => {
				text = original = body;
				loaded = true;
			})
			.catch(() => (error = errorText('internal')));
	});

	async function save(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		error = '';
		try {
			await api.put('/files/text', { root, path, content: text });
			original = text;
			saved = true;
		} catch (err) {
			error = errorText(err instanceof ApiError ? err.code : 'internal');
		}
		busy = false;
	}
</script>

<Dialog title={t('files.editTitle', { name: path })} {onclose} wide>
	<form onsubmit={save}>
		<p class="notice-warn">{t('files.editWarning')}</p>
		<textarea class="field mono" rows="18" bind:value={text} oninput={() => (saved = false)} spellcheck="false" disabled={!loaded} aria-label={path}></textarea>
		{#if saved}<p class="notice-ok" role="status">{t('files.editSaved', { name: path })}</p>{/if}
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={onclose}>{t('files.editClose')}</button>
			<button type="submit" class="btn btn-primary" disabled={busy || !loaded || text === original}>{t('notify.save')}</button>
		</div>
	</form>
</Dialog>

<style>
	textarea { width: 100%; box-sizing: border-box; resize: vertical; min-height: 16rem; margin-bottom: 0.8rem; font-size: 0.85rem; }
</style>
