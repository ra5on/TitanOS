<script lang="ts">
	import { ApiError, type Volume } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';

	type Props = { volume: Volume; onclose: () => void; ondone: () => void };
	let { volume, onclose, ondone }: Props = $props();

	let working = $state(false);
	let error = $state<{ text: string; detail: string } | null>(null);

	async function submit() {
		if (working) return;
		working = true;
		error = null;
		try {
			// DELETE with a body: the API still expects the volume name as the
			// confirmation; the dialog sends it once the button is clicked.
			const res = await fetch(`/api/v1/storage/volumes/${volume.name}`, {
				method: 'DELETE',
				credentials: 'same-origin',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ confirm: volume.name })
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
	<div class="body">
		<p class="notice-error">{t('delete.warning', { volume: volume.name })}</p>
		{#if error}
			<div class="notice-error" role="alert">{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}</div>
		{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={onclose} disabled={working}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-danger" disabled={working} onclick={submit}>{t('delete.submit')}</button>
		</div>
	</div>
</Dialog>

<style>
	.body {
		display: flex;
		flex-direction: column;
		gap: 16px;
	}
	pre {
		margin: 8px 0 0;
		font-family: var(--font-mono);
		font-size: 0.75rem;
		white-space: pre-wrap;
		word-break: break-word;
	}
</style>
