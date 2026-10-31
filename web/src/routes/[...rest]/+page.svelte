<script lang="ts">
	// Unknown addresses are not promises of future features.
	import { t } from '#lib/i18n/index.svelte.ts';
</script>

<section class="tile">
	<h1>{t('placeholder.title')}</h1>
	<p class="muted">{t('placeholder.text')}</p>
	<a href="/">{t('nav.dashboard')}</a>
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
		font-size: var(--text-xl);
		font-weight: 600;
	}
</style>
