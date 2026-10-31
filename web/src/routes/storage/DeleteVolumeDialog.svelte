<script lang="ts">
	import { api, ApiError, type Volume } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';

	/** What lives on the volume, when the page knows it (see the volume details). */
	type Usage = { shares: string[]; vms: string[]; docker: string[] | null };
	type Props = { volume: Volume; usage?: Usage | null; onclose: () => void; ondone: () => void };
	let { volume, usage, onclose, ondone }: Props = $props();

	let working = $state(false);
	let error = $state<{ text: string; detail: string } | null>(null);

	async function submit() {
		if (working) return;
		working = true;
		error = null;
		try {
			// DELETE with a body: the API still expects the volume name as the
			// confirmation; the dialog sends it once the button is clicked.
			await api.delete(`/storage/volumes/${volume.name}`, { confirm: volume.name });
			ondone();
		} catch (err) {
			const code = err instanceof ApiError ? err.code : 'unreachable';
			error = {
				text: errorText(code),
				detail: code === 'storage_failed' && err instanceof ApiError ? err.message : '',
			};
			working = false;
		}
	}
</script>

<Dialog title={t('delete.title', { volume: volume.name })} locked={working} {onclose}>
	<div class="body">
		<p class="notice-error">{t('delete.warning', { volume: volume.name })}</p>
		{#if usage && (usage.shares.length || usage.vms.length || usage.docker)}
			<div class="affected">
				<p>{t('delete.affected')}</p>
				<ul>
					{#if usage.shares.length}<li>
							{t('delete.affectedShares', { shares: usage.shares.join(', ') })}
						</li>{/if}
					{#if usage.vms.length}<li>
							{t('delete.affectedVms', { vms: usage.vms.join(', ') })}
						</li>{/if}
					{#if usage.docker}<li>
							{usage.docker.length
								? t('storage.onVolumeDockerApps', { apps: usage.docker.join(', ') })
								: t('storage.onVolumeDockerEngine')}
						</li>{/if}
				</ul>
			</div>
		{/if}
		{#if error}
			<div class="notice-error" role="alert">
				{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}
			</div>
		{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={onclose} disabled={working}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-danger" disabled={working} onclick={submit}
				>{t('delete.submit')}</button
			>
		</div>
	</div>
</Dialog>

<style>
	.body {
		display: flex;
		flex-direction: column;
		gap: 16px;
	}
	.affected {
		font-size: 0.875rem;
	}
	.affected p,
	.affected ul {
		margin: 0;
	}
	.affected ul {
		padding-left: 18px;
		overflow-wrap: anywhere;
	}
	pre {
		margin: 8px 0 0;
		font-family: var(--font-mono);
		font-size: 0.75rem;
		white-space: pre-wrap;
		word-break: break-word;
	}
</style>
