<script lang="ts">
	// PLAN 65-3: a page that failed. In a window of the desktop the title bar
	// names it (no big "500" inside the window); elsewhere a calm note with
	// the way back. The status number stays for screen readers and support.
	import { page } from '$app/state';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { pageTitle } from '#lib/page-title.svelte.ts';
	import { windowView } from '#lib/desktop/mode.svelte.ts';

	const missing = $derived(page.status === 404);
	const title = $derived(t(missing ? 'placeholder.title' : 'desktop.error.title'));
	$effect(() => {
		const shown = title;
		pageTitle.value = shown;
		return () => {
			if (pageTitle.value === shown) pageTitle.value = '';
		};
	});
</script>

<section class="tile" role="alert">
	<h1 class:sr-only={windowView()}>{title}</h1>
	<p class="muted">
		{t(missing ? 'placeholder.text' : 'desktop.error.text')}
		<span class="code mono">({page.status})</span>
	</p>
	<div class="row">
		{#if !missing}
			<button type="button" onclick={() => location.reload()}>{t('ui.retry')}</button>
		{/if}
		<a href="/">{t('desktop.phone.home')}</a>
	</div>
</section>

<style>
	section {
		margin: var(--space-5);
		padding: 32px;
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		align-items: center;
		text-align: center;
	}
	h1 {
		font-size: var(--text-xl);
		font-weight: 600;
	}
	.row {
		display: flex;
		gap: var(--space-3);
		align-items: center;
		flex-wrap: wrap;
		justify-content: center;
	}
	.code {
		white-space: nowrap;
	}
</style>
