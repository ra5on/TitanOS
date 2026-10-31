<script lang="ts">
	// A part of a page that could not be read, with a way to ask again.
	import { t } from '#lib/i18n/index.svelte.ts';

	let { text, onretry }: { text: string; onretry: () => void } = $props();
</script>

<div class="notice-error retry" role="alert">
	<span>{text}</span>
	<button type="button" class="btn" onclick={onretry}>{t('common.retry')}</button>
</div>

<style>
	.retry {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2);
	}
</style>
