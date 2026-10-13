<script lang="ts">
	import { ApiError, type Volume } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';

	type Props = { volume: Volume; onclose: () => void; ondone: () => void };
	let { volume, onclose, ondone }: Props = $props();

	let typed = $state('');
	let working = $state(false);
	let error = $state<{ text: string; detail: string } | null>(null);

	async function submit(event: SubmitEvent) {
		event.preventDefault();
		if (typed !== volume.name) return;
		working = true;
		error = null;
		try {
			// DELETE with a body: the confirmation travels with the request.
			const res = await fetch(`/api/v1/storage/volumes/${volume.name}`, {
				method: 'DELETE',
				credentials: 'same-origin',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ confirm: typed })
			});
			if (!res.ok) {
				const data = await res.json().catch(() => null);
				throw new ApiError(res.status, data?.error?.code ?? 'internal', data?.error?.message ?? '');
			}
			ondone();
		} catch (err) {
			const code = err instanceof ApiError ? err.code : 'unreachable';
			error = { text: errorText(code), detail: code === 'storage_failed' && err instanceof ApiError ? err.message : '' };
			working = false;
		}
	}
</script>

<Dialog title={t('delete.title', { volume: volume.name })} locked={working} {onclose}>
	<form onsubmit={submit}>
		<p class="notice-error">{t('delete.warning')}</p>
		<label>
			<span>{t('delete.typeName')}</span>
			<!-- svelte-ignore a11y_autofocus -->
			<input class="field mono" type="text" bind:value={typed} autocapitalize="none" autocomplete="off" spellcheck="false" autofocus placeholder={volume.name} />
		</label>
		{#if error}
			<div class="notice-error" role="alert">{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}</div>
		{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={onclose} disabled={working}>{t('common.cancel')}</button>
			<button type="submit" class="btn btn-danger" disabled={typed !== volume.name || working}>{t('delete.submit')}</button>
		</div>
	</form>
</Dialog>

<style>
	form {
		display: flex;
		flex-direction: column;
		gap: 16px;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 8px;
		font-size: 0.875rem;
	}
	pre {
		margin: 8px 0 0;
		font-family: var(--font-mono);
		font-size: 0.75rem;
		white-space: pre-wrap;
		word-break: break-word;
	}
</style>
