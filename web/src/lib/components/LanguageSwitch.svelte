<script lang="ts">
	import { i18n, LOCALES, setLocale, t } from '#lib/i18n/index.svelte.ts';
</script>

<div class="switch" role="radiogroup" aria-label={t('lang.label')}>
	{#each LOCALES as locale (locale)}
		<button
			type="button"
			role="radio"
			aria-checked={i18n.locale === locale}
			onclick={() => setLocale(locale)}
		>
			{t(`lang.${locale}`)}
		</button>
	{/each}
</div>

<style>
	.switch {
		display: inline-flex;
		gap: 4px;
		padding: 3px;
		background: var(--bg);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
	}
	button {
		height: 2rem;
		padding: 0 0.75rem;
		background: transparent;
		border: 0;
		border-radius: var(--radius-ctl);
		color: var(--muted);
		font: inherit;
		font-size: var(--text-base);
		cursor: pointer;
	}
	button[aria-checked='true'] {
		background: var(--chip);
		color: var(--text);
		font-weight: 500;
	}
</style>
