<script lang="ts">
	// Asks before a plugin source is removed.
	import Dialog from '#lib/components/Dialog.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';

	type Props = { name: string; onclose: () => void; onremove: () => void };
	let { name, onclose, onremove }: Props = $props();
</script>

<Dialog title={t('plugins.sourceRemoveTitle', { name })} {onclose}>
	<p class="text">{t('plugins.sourceRemoveText')}</p>
	<div class="dialog-actions">
		<button type="button" class="btn" onclick={onclose}>{t('common.cancel')}</button>
		<button type="button" class="btn btn-warn" onclick={onremove}
			>{t('plugins.sourceRemove')}</button
		>
	</div>
</Dialog>

<style>
	.text {
		font-size: 0.875rem;
	}
</style>
