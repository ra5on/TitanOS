<script lang="ts">
	// A page of the group "Vorausschau" (PLAN 54-V, Entwürfe 09/10): over the
	// whole screen on the phone, as a side panel on wider screens; back
	// button, title, one column. Escape closes it.
	import type { Snippet } from 'svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { featurePage } from './page-mode.ts';

	type Props = { title: string; onclose: () => void; children: Snippet };
	let { title, onclose, children }: Props = $props();
	let dialog = $state<HTMLDialogElement>();
	const id = $props.id();
	// PLAN 72: a page in the agent's main area on a computer.
	const page = featurePage();
	$effect(() => {
		dialog?.showModal();
	});
</script>

<svelte:window onkeydown={(e) => page && e.key === 'Escape' && onclose()} />

{#if page}
	<section class="fsheet page" aria-labelledby="{id}-t">
		<header>
			<button type="button" class="back" aria-label={t('agent.f.foresight.back')} onclick={onclose}
				>‹</button
			>
			<h2 id="{id}-t">{title}</h2>
		</header>
		<div class="body">
			{@render children()}
		</div>
	</section>
{:else}
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
{/if}

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
	.fsheet:not([open]):not(.page) {
		display: none;
	}
	/* PLAN 72: inside the agent's main area, no panel over the side bar. */
	.fsheet.page {
		position: absolute;
		z-index: 5;
	}
	.page .body {
		width: min(100%, 880px);
		box-sizing: border-box;
		margin: 0 auto;
	}
	.fsheet::backdrop {
		background: color-mix(in srgb, #000 45%, transparent);
	}
	@media (min-width: 768px) {
		.fsheet:not(.page) {
			left: auto;
			width: 460px;
			border-left: 1px solid var(--border);
			box-shadow: -20px 0 50px -20px #000a;
		}
	}
	header {
		display: flex;
		align-items: center;
		gap: var(--space-2-5);
		padding: calc(env(safe-area-inset-top, 0px) + 14px) var(--space-4) var(--space-2-5);
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
		padding: var(--space-1) var(--space-4) calc(env(safe-area-inset-bottom, 0px) + var(--space-5));
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
		margin: var(--space-1-5) 2px -6px;
	}
	/* PLAN 78-5: flat rows that stand together, no box around the group. */
	.body :global(.fgrp2) {
		display: flex;
		flex-direction: column;
		gap: 3px;
		min-width: 0;
	}
	.body :global(.frow2) {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		padding: var(--space-3) 14px;
		border: 1px solid var(--srow-line);
		border-radius: var(--srow-radius);
		background: var(--srow-bg);
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
		padding: 2px var(--space-2);
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
		padding: var(--space-3) 14px;
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
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
		gap: var(--space-2);
		flex-wrap: wrap;
	}
	.body :global(.fmuted) {
		margin: 0;
		font-size: 13px;
		color: var(--muted);
	}
</style>
