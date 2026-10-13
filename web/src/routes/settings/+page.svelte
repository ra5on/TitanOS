<script lang="ts">
	import { t } from '#lib/i18n/index.svelte.ts';
</script>

<h1>{t('settings.title')}</h1>

<div class="areas">
	<a class="tile" href="/settings/appearance">
		<span class="name">{t('menu.appearance')}</span>
		<span class="muted">{t('settings.appearanceDesc')}</span>
	</a>
</div>

<style>
	h1 {
		font-size: 1.5rem;
		font-weight: 600;
	}
	.areas {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
		gap: var(--gap);
	}
	a {
		display: flex;
		flex-direction: column;
		gap: 6px;
		padding: var(--tile-pad);
		color: var(--text);
		text-decoration: none;
		font-size: 0.875rem;
	}
	a:hover {
		border-color: var(--accent);
	}
	.name {
		font-size: 1rem;
		font-weight: 600;
	}
</style>
