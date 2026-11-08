<script lang="ts">
	// The views of the group "Schutz" (PLAN 54-S) opened from the agent or
	// from the list of functions: the ransomware alert (guard), the
	// emergency handbook (handbook) and the emergency access (emergency).
	// A sheet from the right (full screen on a phone), one column; the
	// settings of the function can follow below (extra).
	import type { Snippet } from 'svelte';
	import { fly } from 'svelte/transition';
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import type { FeatureViewId } from '#lib/agent-features.ts';
	import ProtectGuard from './ProtectGuard.svelte';
	import ProtectHandbook from './ProtectHandbook.svelte';
	import ProtectEmergency from './ProtectEmergency.svelte';
	import { featurePage } from './page-mode.ts';

	type Props = { id: FeatureViewId; onclose: () => void; calm?: boolean; extra?: Snippet };
	let { id, onclose, calm = false, extra }: Props = $props();
	// The sheet goes to <body>: inside a tile with backdrop-filter "fixed"
	// would be relative to the tile.
	function portal(node: HTMLElement) {
		document.body.appendChild(node);
		return { destroy: () => node.remove() };
	}
	// PLAN 72: a page in the agent's main area on a computer.
	const page = featurePage();
	const mine = $derived(id === 'guard' || id === 'handbook' || id === 'emergency');
	const titles: Partial<Record<FeatureViewId, string>> = $derived({
		guard: t('agent.f.protect.guard'),
		handbook: t('agent.f.protect.handbook'),
		emergency: t('agent.f.protect.emergency'),
	});
</script>

<svelte:window onkeydown={(e) => mine && e.key === 'Escape' && onclose()} />

{#if mine && page}
	<section class="sheet page" aria-label={titles[id]}>
		<header>
			<button type="button" class="back" aria-label={t('agent.f.protect.back')} onclick={onclose}
				><Icon name="chevronLeft" size={18} /></button
			>
			<h2>{titles[id]}</h2>
		</header>
		<div class="body">
			{#if id === 'guard'}
				<ProtectGuard />
			{:else if id === 'handbook'}
				<ProtectHandbook />
			{:else}
				<ProtectEmergency />
			{/if}
			{#if extra}{@render extra()}{/if}
		</div>
	</section>
{:else if mine}
	<div class="protect-layer" use:portal>
		<div class="scrim" role="presentation" onclick={onclose}></div>
		<div
			class="sheet"
			role="dialog"
			aria-modal="true"
			aria-label={titles[id]}
			transition:fly={{ x: 380, duration: calm ? 0 : 240, opacity: 1 }}
		>
			<header>
				<button type="button" class="back" aria-label={t('agent.f.protect.back')} onclick={onclose}
					><Icon name="chevronLeft" size={18} /></button
				>
				<h2>{titles[id]}</h2>
			</header>
			<div class="body">
				{#if id === 'guard'}
					<ProtectGuard />
				{:else if id === 'handbook'}
					<ProtectHandbook />
				{:else}
					<ProtectEmergency />
				{/if}
				{#if extra}{@render extra()}{/if}
			</div>
		</div>
	</div>
{/if}

<style>
	.scrim {
		position: fixed;
		inset: 0;
		z-index: 70;
		background: rgb(0 0 0 / 0.45);
	}
	.sheet {
		position: fixed;
		z-index: 71;
		top: 0;
		right: 0;
		bottom: 0;
		width: min(480px, 100%);
		display: flex;
		flex-direction: column;
		color: var(--text);
		background: var(--bg, var(--surface));
		border-left: 1px solid var(--border);
		box-shadow: var(--shadow-menu, -18px 0 44px rgb(0 0 0 / 0.5));
	}
	/* PLAN 72: inside the agent's main area, no panel over the side bar. */
	.sheet.page {
		position: absolute;
		z-index: 5;
		inset: 0;
		width: auto;
		border-left: 0;
		box-shadow: none;
	}
	.page .body {
		width: min(100%, 880px);
		box-sizing: border-box;
		margin: 0 auto;
	}
	header {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		padding: 14px var(--space-4) var(--space-2-5);
	}
	h2 {
		margin: 0;
		font-size: var(--text-lg, 1.1rem);
		font-weight: 650;
	}
	.back {
		width: 34px;
		height: 34px;
		display: grid;
		place-items: center;
		border-radius: 50%;
		border: 1px solid color-mix(in srgb, var(--text) 14%, transparent);
		background: color-mix(in srgb, var(--text) 4%, transparent);
		color: var(--text);
		cursor: pointer;
	}
	.back:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.body {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
		padding: var(--space-1-5) var(--space-4) 28px;
		display: flex;
		flex-direction: column;
		gap: var(--space-2-5);
	}
	.body > :global(*) {
		flex-shrink: 0;
	}
	/* Shared look of the three views. */
	.body :global(.pcap) {
		margin: var(--space-2-5) 2px -2px;
		font-size: var(--text-xs, 0.75rem);
		font-weight: 600;
		letter-spacing: 0.08em;
		text-transform: uppercase;
		color: var(--muted);
	}
	.body :global(.pgrp) {
		border: 1px solid color-mix(in srgb, var(--text) 10%, transparent);
		border-radius: 14px;
		overflow: hidden;
		background: color-mix(in srgb, var(--text) 3%, transparent);
	}
	.body :global(.prow) {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		width: 100%;
		min-height: 48px;
		padding: var(--space-2-5) 14px;
		border: 0;
		border-top: 1px solid color-mix(in srgb, var(--text) 8%, transparent);
		background: none;
		color: var(--text);
		font: inherit;
		text-align: left;
	}
	.body :global(.prow:first-child) {
		border-top: 0;
	}
	.body :global(.prow.wrap) {
		flex-wrap: wrap;
	}
	.body :global(.pt) {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 2px;
		overflow-wrap: anywhere;
	}
	.body :global(.pt strong) {
		font-weight: 600;
	}
	.body :global(.pt small) {
		color: var(--muted);
		line-height: 1.35;
	}
	.body :global(.pic) {
		width: 30px;
		height: 30px;
		flex-shrink: 0;
		display: grid;
		place-items: center;
		border-radius: 9px;
		font-size: 16px;
		background: color-mix(in srgb, var(--c, var(--accent)) 18%, transparent);
	}
	.body :global(.pcard) {
		border-radius: 14px;
		padding: 14px;
		border: 1px solid color-mix(in srgb, var(--c) 55%, transparent);
		background: color-mix(in srgb, var(--c) 12%, transparent);
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		overflow-wrap: anywhere;
	}
	.body :global(.pcard h3) {
		margin: 0;
		font-size: var(--text-base, 1rem);
	}
	.body :global(.pcard p) {
		margin: 0;
		line-height: 1.45;
	}
</style>
