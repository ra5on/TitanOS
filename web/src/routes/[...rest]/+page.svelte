<script lang="ts">
	// Every area that does not exist yet ends up here.
	import { t } from '#lib/i18n/index.svelte.ts';
</script>

<section class="tile">
	<h1>{t('placeholder.title')}</h1>
	<p class="muted">{t('placeholder.text')}</p>
</section>

<style>
	section {
		padding: 40px 32px;
		display: flex;
		flex-direction: column;
		gap: 8px;
		text-align: center;
	}
	h1 {
		font-size: 1.25rem;
		font-weight: 600;
	}
</style>
