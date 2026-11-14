<script lang="ts">
	// PLAN 90-4: a sheet that comes up from the bottom on the phone (large
	// rows of at least 48 px, a swipe down closes it) and stands as a small
	// panel in the middle on the desktop. Rows inside use .sh-item,
	// headings .sh-group, a line .sh-rule.
	import type { Snippet } from 'svelte';
	import { fade, fly } from 'svelte/transition';
	import { swipeDown } from '#lib/components/sheet.ts';
	import { t } from '#lib/i18n/index.svelte.ts';

	let {
		label,
		calm = false,
		onclose,
		children,
	}: { label: string; calm?: boolean; onclose: () => void; children: Snippet } = $props();

	function focus(node: HTMLElement) {
		const before = document.activeElement as HTMLElement | null;
		node.focus();
		return { destroy: () => before?.focus?.() };
	}
</script>

<svelte:window onkeydown={(e) => e.key === 'Escape' && onclose()} />

<button
	type="button"
	class="scrim"
	aria-label={t('common.close')}
	tabindex="-1"
	transition:fade={{ duration: calm ? 0 : 180 }}
	onclick={onclose}
></button>
<div
	class="sheet"
	role="dialog"
	aria-modal="true"
	aria-label={label}
	tabindex="-1"
	use:focus
	use:swipeDown={{ close: onclose }}
	transition:fly={{ y: 360, duration: calm ? 0 : 240, opacity: 1 }}
>
	<div class="grab" aria-hidden="true"></div>
	{@render children()}
</div>

<style>
	.scrim {
		position: fixed;
		inset: 0;
		z-index: 80;
		border: 0;
		padding: 0;
		background: var(--overlay, rgb(0 0 0 / 0.5));
		cursor: default;
	}
	.sheet {
		position: fixed;
		z-index: 81;
		left: 0;
		right: 0;
		bottom: 0;
		max-height: 86dvh;
		overflow-y: auto;
		padding: var(--space-2) 0 calc(env(safe-area-inset-bottom, 0px) + 18px);
		border-radius: 22px 22px 0 0;
		border-top: 1px solid var(--glass-border, var(--border));
		background: var(--surface);
		color: var(--text);
		outline: 0;
	}
	.grab {
		width: 40px;
		height: 4px;
		border-radius: 2px;
		margin: 0 auto var(--space-2);
		background: var(--muted);
		opacity: 0.6;
	}
	.sheet :global(.sh-group) {
		margin: 0;
		padding: var(--space-2) 22px 2px;
		color: var(--muted);
		font-size: var(--text-sm);
		font-weight: 400;
	}
	.sheet :global(.sh-item) {
		display: flex;
		align-items: center;
		gap: 14px;
		width: 100%;
		min-height: 48px;
		padding: var(--space-1) 22px;
		border: 0;
		background: none;
		color: var(--text);
		font: inherit;
		font-size: 1rem;
		text-align: left;
		cursor: pointer;
	}
	.sheet :global(.sh-item:hover) {
		background: color-mix(in srgb, var(--text) 6%, transparent);
	}
	.sheet :global(.sh-item:focus-visible) {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.sheet :global(.sh-item > svg) {
		color: var(--muted);
		flex: none;
	}
	.sheet :global(.sh-item > .sh-text) {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		line-height: 1.25;
		overflow-wrap: anywhere;
	}
	.sheet :global(.sh-item small) {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.sheet :global(.sh-item .sh-check) {
		color: var(--accent);
		font-weight: 700;
		flex: none;
	}
	.sheet :global(.sh-item.danger),
	.sheet :global(.sh-item.danger > svg) {
		color: var(--error-text);
	}
	.sheet :global(.sh-rule) {
		height: 1px;
		margin: var(--space-1-5) var(--space-4);
		border: 0;
		background: var(--border);
	}
	.sheet :global(.sh-quote) {
		margin: 2px 22px var(--space-2);
		color: var(--muted);
		overflow-wrap: anywhere;
	}
	@media (min-width: 761px) {
		.sheet {
			left: 50%;
			right: auto;
			bottom: auto;
			top: 50%;
			width: min(420px, calc(100vw - 32px));
			max-height: min(640px, 86dvh);
			transform: translate(-50%, -50%);
			padding-bottom: var(--space-3);
			border: 1px solid var(--border);
			border-radius: var(--radius-tile);
			box-shadow: var(--shadow-menu, 0 18px 44px rgb(0 0 0 / 0.5));
		}
		.grab {
			display: none;
		}
	}
</style>
