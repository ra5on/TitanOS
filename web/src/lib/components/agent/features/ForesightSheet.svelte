<script lang="ts">
	// A page of the group "Vorausschau" (PLAN 54-V, Entwürfe 09/10): over the
	// whole screen on the phone, as a side panel on wider screens; back
	// button, title, one column. Escape closes it.
	import type { Snippet } from 'svelte';
	import { t } from '#lib/i18n/index.svelte.ts';

	type Props = { title: string; onclose: () => void; children: Snippet };
	let { title, onclose, children }: Props = $props();
	let dialog: HTMLDialogElement;
	const id = $props.id();
	$effect(() => {
		dialog.showModal();
	});
</script>

<dialog bind:this={dialog} class="fsheet" aria-labelledby="{id}-t" {onclose}>
	<header>
		<button type="button" class="back" aria-label={t('agent.f.foresight.back')} onclick={onclose}
			>‹</button
		>
		<h2 id="{id}-t">{title}</h2>
	</header>
	<div class="body">
		{@render children()}
	</div>
</dialog>

<style>
	.fsheet {
		position: fixed;
		inset: 0;
		width: 100%;
		max-width: none;
		height: 100%;
		max-height: none;
		margin: 0;
		padding: 0;
		border: 0;
		display: flex;
		flex-direction: column;
		color: var(--text);
		background: var(--bg);
		font-size: 15px;
	}
	.fsheet:not([open]) {
		display: none;
	}
	.fsheet::backdrop {
		background: color-mix(in srgb, #000 45%, transparent);
	}
	@media (min-width: 768px) {
		.fsheet {
			left: auto;
			width: 460px;
			border-left: 1px solid var(--border);
			box-shadow: -20px 0 50px -20px #000a;
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
		min-width: 0;
		margin: 0;
		font-size: 17px;
		overflow-wrap: anywhere;
	}
	.back {
		width: 34px;
		height: 34px;
		flex-shrink: 0;
		border-radius: 50%;
		display: grid;
		place-items: center;
		border: 1px solid var(--border);
		background: none;
		color: var(--muted);
		font-size: 18px;
		cursor: pointer;
	}
	.body {
		flex: 1;
		overflow: auto;
		padding: 4px 16px calc(env(safe-area-inset-bottom, 0px) + 24px);
		display: flex;
		flex-direction: column;
		gap: 14px;
	}
	.body > :global(*) {
		flex-shrink: 0;
	}
	/* Shared look of the group's pages. */
	.body :global(.fh) {
		font-size: 12px;
		letter-spacing: 0.06em;
		text-transform: uppercase;
		color: var(--muted);
		margin: 6px 2px -6px;
	}
	.body :global(.fgrp2) {
		border: 1px solid var(--border);
		border-radius: 14px;
		overflow: hidden;
		background: color-mix(in srgb, var(--surface) 70%, transparent);
	}
	.body :global(.frow2) {
		display: flex;
		align-items: center;
		gap: 12px;
		padding: 12px 14px;
		border-top: 1px solid color-mix(in srgb, var(--border) 70%, transparent);
	}
	.body :global(.frow2:first-child) {
		border-top: 0;
	}
	.body :global(.ft) {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 2px;
		overflow-wrap: anywhere;
	}
	.body :global(.ft small) {
		color: var(--muted);
		font-size: 13px;
		line-height: 1.35;
	}
	.body :global(.fic) {
		width: 30px;
		height: 30px;
		border-radius: 9px;
		display: grid;
		place-items: center;
		font-size: 16px;
		flex-shrink: 0;
		background: color-mix(in srgb, var(--c, var(--accent)) 18%, transparent);
	}
	.body :global(.ftag) {
		font-size: 12px;
		padding: 2px 8px;
		border-radius: 999px;
		border: 1px solid currentColor;
		white-space: nowrap;
		flex-shrink: 0;
	}
	.body :global(.ftag.ok) {
		color: var(--ok-text);
	}
	.body :global(.ftag.warn) {
		color: var(--warn-text);
	}
	.body :global(.ftag.bad) {
		color: var(--error-text);
	}
	.body :global(.fnote) {
		border: 1px solid;
		border-radius: 14px;
		padding: 12px 14px;
		display: flex;
		flex-direction: column;
		gap: 8px;
		overflow-wrap: anywhere;
	}
	.body :global(.fnote small) {
		font-size: 13px;
		line-height: 1.4;
	}
	.body :global(.fnote.ok) {
		border-color: var(--ok-border);
		background: var(--ok-bg);
	}
	.body :global(.fnote.warn) {
		border-color: var(--warn-border);
		background: var(--warn-bg);
	}
	.body :global(.fnote.bad) {
		border-color: var(--error-border);
		background: var(--error-bg);
	}
	.body :global(.fbtns) {
		display: flex;
		gap: 8px;
		flex-wrap: wrap;
	}
	.body :global(.fmuted) {
		margin: 0;
		font-size: 13px;
		color: var(--muted);
	}
</style>
