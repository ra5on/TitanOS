<script lang="ts">
	// A page of the group "Dokumente und Fristen" (PLAN 54-D): full screen
	// on the phone, a side panel on the desktop (drafts 04/05), with a back
	// button. Escape closes it.
	import type { Snippet } from 'svelte';
	import { fly } from 'svelte/transition';
	import { t } from '#lib/i18n/index.svelte.ts';

	type Props = { title: string; onclose: () => void; calm?: boolean; children: Snippet };
	let { title, onclose, calm = false, children }: Props = $props();
	const id = $props.id();

	// To the end of the page: a parent with a backdrop filter (the tiles of
	// the settings) would otherwise hold the fixed panel inside it.
	function portal(node: HTMLElement) {
		document.body.appendChild(node);
		return { destroy: () => node.remove() };
	}
</script>

<svelte:window onkeydown={(e) => e.key === 'Escape' && onclose()} />

<div use:portal class="docs-layer">
	<div class="scrim" role="presentation" onclick={onclose}></div>
	<div
		class="sheet"
		role="dialog"
		aria-modal="true"
		aria-labelledby="{id}-t"
		transition:fly={{ x: 380, duration: calm ? 0 : 240, opacity: 1 }}
	>
		<header>
			<button type="button" class="back" aria-label={t('agent.f.docs.back')} onclick={onclose}
				><span aria-hidden="true">‹</span></button
			>
			<h2 id="{id}-t">{title}</h2>
		</header>
		<div class="body">
			{@render children()}
		</div>
	</div>
</div>

<style>
	.scrim {
		position: fixed;
		inset: 0;
		z-index: 88;
		background: rgb(3 6 14 / 0.4);
	}
	.sheet {
		position: fixed;
		z-index: 89;
		inset: 0;
		display: flex;
		flex-direction: column;
		color: var(--text);
		background: var(--bg);
	}
	@media (min-width: 900px) {
		.sheet {
			left: auto;
			width: 460px;
			border-left: 1px solid var(--border);
			box-shadow: -20px 0 50px -20px rgb(0 0 0 / 0.6);
		}
	}
	header {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: calc(env(safe-area-inset-top, 0px) + 14px) 16px 10px;
	}
	h2 {
		flex: 1;
		margin: 0;
		font-size: var(--text-md, 1.05rem);
	}
	.back {
		width: 34px;
		height: 34px;
		flex: none;
		border-radius: 50%;
		display: grid;
		place-items: center;
		border: 1px solid var(--border);
		background: none;
		color: var(--muted);
		font-size: 20px;
		line-height: 1;
		cursor: pointer;
	}
	.body {
		flex: 1;
		overflow-y: auto;
		padding: 4px 16px calc(env(safe-area-inset-bottom, 0px) + 24px);
		display: flex;
		flex-direction: column;
		gap: 14px;
	}
	.body > :global(*) {
		flex-shrink: 0;
	}
	/* Shared look of the group's pages. */
	.body :global(.dh) {
		margin: 6px 2px -6px;
		font-size: var(--text-xs, 0.75rem);
		font-weight: 600;
		letter-spacing: 0.06em;
		text-transform: uppercase;
		color: var(--muted);
	}
	.body :global(.dgrp) {
		border: 1px solid var(--border);
		border-radius: 14px;
		overflow: hidden;
		background: color-mix(in srgb, var(--surface) 70%, transparent);
	}
	.body :global(.drow) {
		display: flex;
		align-items: center;
		gap: 12px;
		padding: 12px 14px;
		border-top: 1px solid color-mix(in srgb, var(--border) 70%, transparent);
	}
	.body :global(.drow:first-child) {
		border-top: 0;
	}
	.body :global(.drow.col) {
		flex-direction: column;
		align-items: stretch;
		gap: 8px;
	}
	.body :global(.dt) {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 2px;
		overflow-wrap: anywhere;
	}
	.body :global(.dt small) {
		color: var(--muted);
		line-height: 1.35;
	}
	.body :global(.dic) {
		width: 30px;
		height: 30px;
		flex-shrink: 0;
		display: grid;
		place-items: center;
		border-radius: 9px;
		font-size: 16px;
		background: color-mix(in srgb, var(--c, var(--accent)) 18%, transparent);
	}
	.body :global(.tag) {
		font-size: var(--text-xs, 0.75rem);
		padding: 2px 8px;
		border-radius: 999px;
		border: 1px solid currentColor;
		white-space: nowrap;
	}
	.body :global(.ok) {
		color: var(--ok-text);
	}
	.body :global(.warn) {
		color: var(--warn-text);
	}
	.body :global(.bad) {
		color: var(--error-text);
	}
	.body :global(.amt) {
		font-variant-numeric: tabular-nums;
		font-weight: 600;
		white-space: nowrap;
	}
	.body :global(.seg) {
		display: flex;
		border: 1px solid var(--border);
		border-radius: 10px;
		overflow: hidden;
	}
	.body :global(.seg button) {
		flex: 1;
		min-width: 0;
		padding: 8px 4px;
		border: 0;
		background: none;
		color: var(--text);
		font: inherit;
		font-size: var(--text-sm, 0.875rem);
		cursor: pointer;
	}
	.body :global(.seg button[aria-pressed='true']) {
		background: var(--accent);
		color: var(--on-accent, #0b1622);
		font-weight: 600;
	}
	.body :global(.link) {
		border: 0;
		background: none;
		padding: 0;
		color: var(--accent);
		font: inherit;
		font-weight: 600;
		cursor: pointer;
		text-align: left;
	}
</style>
