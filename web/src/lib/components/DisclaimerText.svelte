<script lang="ts">
	// The disclaimer of TitanOS (DISCLAIMER.md in every repository, owner
	// decision of 23 Nov 2026), in the language of the interface.
	import { t } from '#lib/i18n/index.svelte.ts';
</script>

<div class="disclaimer">
	<p>{t('disclaimer.intro')}</p>
	<ul>
		<li><b>{t('disclaimer.dataTitle')}</b> {t('disclaimer.data')}</li>
		<li><b>{t('disclaimer.pluginsTitle')}</b> {t('disclaimer.plugins')}</li>
		<li><b>{t('disclaimer.networkTitle')}</b> {t('disclaimer.network')}</li>
		<li><b>{t('disclaimer.promiseTitle')}</b> {t('disclaimer.promise')}</li>
	</ul>
	<p>{t('disclaimer.liability')}</p>
	<p class="muted">{t('disclaimer.license')}</p>
</div>

<style>
	.disclaimer {
		display: flex;
		flex-direction: column;
		gap: var(--space-2-5);
		font-size: var(--text-md);
		line-height: 1.5;
	}
	p,
	ul {
		margin: 0;
	}
	ul {
		padding-left: 1.2em;
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
	}
</style>
