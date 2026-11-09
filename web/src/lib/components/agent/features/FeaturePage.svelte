<script lang="ts">
	// The frame of a function's view as a page in the agent's main area
	// (PLAN 72): back button and title on top, one column of cards below.
	import type { Snippet } from 'svelte';
	import { t } from '#lib/i18n/index.svelte.ts';

	type Props = { title: string; onclose: () => void; children: Snippet };
	let { title, onclose, children }: Props = $props();
	const id = $props.id();
</script>

<svelte:window onkeydown={(e) => e.key === 'Escape' && onclose()} />

<section class="feature-page" aria-labelledby="{id}-t">
	<header>
		<button type="button" class="btn back" onclick={onclose}>‹ {t('agent.f.docs.back')}</button>
		<h2 id="{id}-t">{title}</h2>
	</header>
	<div class="body">
		{@render children()}
	</div>
</section>

<style>
	.feature-page {
		position: absolute;
		inset: 0;
		z-index: 5;
		display: flex;
		flex-direction: column;
		background: var(--bg);
		color: var(--text);
	}
	header {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		min-height: 56px;
		padding: var(--space-2) 20px;
		border-bottom: 1px solid var(--panel-border, var(--border));
	}
	h2 {
		flex: 1;
		min-width: 0;
		margin: 0;
		font-size: var(--text-md);
		font-weight: 600;
		overflow-wrap: anywhere;
	}
	.back {
		min-height: 32px;
		padding: 0 var(--space-3);
		font-size: var(--text-base);
	}
	.body {
		flex: 1;
		overflow-y: auto;
		display: flex;
		flex-direction: column;
		gap: 14px;
		width: min(100%, 880px);
		box-sizing: border-box;
		margin: 0 auto;
		padding: 20px 20px 32px;
	}
	.body > :global(*) {
		flex-shrink: 0;
	}
</style>
