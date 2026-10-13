<script lang="ts">
	// Volumes arrive with the storage area; until then the tile says so.
	import { t } from '#lib/i18n/index.svelte.ts';
</script>

<div class="empty">
	<p>{t('volumes.empty')}</p>
	<p class="muted">{t('volumes.emptyHint')}</p>
</div>

<style>
	.empty {
		flex: 1 1 auto;
		display: flex;
		flex-direction: column;
		justify-content: center;
		gap: 4px;
		font-size: 0.875rem;
	}
</style>
