<script lang="ts">
	// 1.13 (owner 18 Nov 2026): the tool bar at the bottom of a compact
	// level (compact.svelte.ts) – Mail's list ("Neue E-Mail" at the right)
	// and message (answer, move, delete, mark, more); Kontakte and Kalender
	// follow. Fixed above the phone's home indicator (safe area), the page
	// keeps room for it below its content. Its items: buttons or links with
	// the class "bar-btn" (an icon, the name for screen readers and as
	// tooltip; 44 px), Dropdown menus (their button takes the same look),
	// and one "bar-btn bar-new" for the main action, at the right.
	import type { Snippet } from 'svelte';

	let { label, children }: { label: string; children: Snippet } = $props();
</script>

<div class="bb-room" aria-hidden="true"></div>
<div class="bottombar" role="toolbar" aria-label={label}>
	{@render children()}
</div>

<style>
	.bb-room {
		flex: none;
		height: calc(57px + env(safe-area-inset-bottom, 0px));
	}
	.bottombar {
		position: fixed;
		left: 0;
		right: 0;
		bottom: 0;
		z-index: 40;
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-1);
		min-height: 56px;
		padding: 6px max(var(--space-2), env(safe-area-inset-right, 0px))
			calc(6px + env(safe-area-inset-bottom, 0px))
			max(var(--space-2), env(safe-area-inset-left, 0px));
		border-top: 1px solid var(--border);
		background: var(--surface);
	}
	.bottombar :global(:is(.bar-btn, .dropdown > .btn)) {
		display: inline-grid;
		place-items: center;
		min-width: 44px;
		min-height: 44px;
		padding: 0 var(--space-2);
		border: 0;
		border-radius: var(--radius-small);
		background: none;
		color: var(--text);
		font: inherit;
		text-decoration: none;
		cursor: pointer;
	}
	.bottombar :global(:is(.bar-btn, .dropdown > .btn):hover) {
		background: var(--chip);
	}
	.bottombar :global(:is(.bar-btn, .dropdown > .btn):focus-visible) {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.bottombar :global(:is(.bar-btn, .dropdown > .btn):disabled) {
		opacity: 0.5;
		cursor: default;
	}
	.bottombar :global(.bar-btn[aria-pressed='true']) {
		color: var(--status-warn);
	}
	/* The main action ("Neue E-Mail"): at the right, the accent's colour. */
	.bottombar :global(.bar-btn.bar-new) {
		margin-left: auto;
		background: var(--accent);
		color: var(--on-accent);
		border-radius: 999px;
	}
	.bottombar :global(.bar-btn.bar-new:hover) {
		background: color-mix(in srgb, var(--accent) 85%, var(--text));
	}
</style>
