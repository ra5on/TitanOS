<script lang="ts">
	// Lifts the ransomware guard's lock of a share.
	import { api, type Share } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';

	type Props = { share: Share; onclose: () => void; ondone: () => Promise<void> };
	let { share, onclose, ondone }: Props = $props();

	let error = $state('');
	async function unlock() {
		error = '';
		try {
			await api.post(`/shares/${encodeURIComponent(share.name)}/unlock`, {});
			await ondone();
		} catch (err) {
			error = apiErrorText(err);
		}
	}
</script>

<Dialog title={t('shares.guardUnlockTitle', { name: share.name })} {onclose}>
	<p>{t('shares.guardUnlockText')}</p>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	<div class="dialog-actions">
		<button type="button" class="btn" onclick={onclose}>{t('common.cancel')}</button>
		<button type="button" class="btn btn-warn" onclick={unlock}>{t('shares.guardUnlock')}</button>
	</div>
</Dialog>

<style>
	.notice-error {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
		align-items: center;
	}
</style>
