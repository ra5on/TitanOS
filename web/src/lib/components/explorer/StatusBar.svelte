<script lang="ts">
	// PLAN 75-19 (style W4, Windows-11 Explorer): the figures of a view in one
	// thin line at the bottom of the window instead of a tile per figure. In a
	// window of the desktop it stays at the window's lower edge; elsewhere it
	// ends the page.
	import type { Snippet } from 'svelte';
	import { inWindow } from '#lib/desktop/mode.svelte.ts';

	let { children, label }: { children: Snippet; label?: string } = $props();

	$effect(() => {
		if (!inWindow) return;
		document.body.classList.add('has-statusbar');
		return () => document.body.classList.remove('has-statusbar');
	});
</script>

<footer class="statusbar" class:pinned={inWindow} aria-label={label}>{@render children()}</footer>

<style>
	.statusbar {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		column-gap: var(--space-4);
		row-gap: var(--space-1);
		min-height: 28px;
		margin-top: var(--space-3);
		padding: var(--space-1) var(--space-4);
		border-top: 1px solid var(--border);
		background: color-mix(in srgb, var(--surface) 92%, var(--bg));
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.pinned {
		position: fixed;
		z-index: 20;
		left: 0;
		right: 0;
		bottom: 0;
		margin: 0;
	}
	:global(body.has-statusbar) {
		padding-bottom: 30px;
	}
	.statusbar :global(b) {
		color: var(--text);
		font-weight: 600;
		font-variant-numeric: tabular-nums;
	}
	.statusbar :global(.warn) {
		color: var(--warn, #fbbf24);
	}
	.statusbar :global(.end) {
		margin-left: auto;
	}
	@media (max-width: 560px) {
		.statusbar :global(.end) {
			margin-left: 0;
		}
	}
	.statusbar :global(button) {
		padding: 0 var(--space-1);
		border: 0;
		background: none;
		color: inherit;
		font: inherit;
		cursor: pointer;
	}
</style>
