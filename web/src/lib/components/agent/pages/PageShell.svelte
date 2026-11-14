<script lang="ts">
	// PLAN 90-4: the frame of a page of the agent (Start, Geplant,
	// Gedächtnis, Wissen, Einstellungen), like a page of the control panel:
	// "KI-Agent › Seite" as the heading, one sentence below it, then groups
	// of rows. On the phone the head of the app names the page already.
	import type { Snippet } from 'svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';

	let {
		title,
		crumbs = [],
		lead = '',
		plain = false,
		phone = false,
		onunfold,
		children,
	}: {
		title: string;
		/** Pages above this one: "KI-Agent › Einstellungen › Modelle". */
		crumbs?: { label: string; href: string }[];
		lead?: string;
		/** The title alone, without "KI-Agent ›" (the start page greets). */
		plain?: boolean;
		phone?: boolean;
		/** The side bar is folded away: a button brings it back. */
		onunfold?: () => void;
		children: Snippet;
	} = $props();
</script>

<div class="scroll" class:phone>
	<div class="page">
		{#if !phone || plain}
			<div class="head">
				{#if onunfold}
					<button
						type="button"
						class="unfold"
						aria-label={t('agent.sideUnfold')}
						title={t('agent.sideUnfold')}
						onclick={onunfold}><Icon name="sidebar" size={17} /></button
					>
				{/if}
				<h1>
					{#if !plain}<span>{t('nav.agent')}</span> <i aria-hidden="true">›</i>
						{#each crumbs as c (c.href)}<a href={c.href}>{c.label}</a>
							<i aria-hidden="true">›</i>
						{/each}{/if}
					{title}
				</h1>
			</div>
		{:else if crumbs.length}
			<a class="back" href={crumbs[crumbs.length - 1].href}
				><Icon name="chevronLeft" size={16} />{crumbs[crumbs.length - 1].label}</a
			>
		{/if}
		{#if lead}<p class="lead">{lead}</p>{/if}
		{@render children()}
	</div>
</div>

<style>
	.scroll {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
		overscroll-behavior: contain;
	}
	.page {
		max-width: 980px;
		margin: 0 auto;
		padding: var(--space-5) 28px;
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
	}
	.phone .page {
		padding: var(--space-4) var(--space-3) calc(env(safe-area-inset-bottom, 0px) + var(--space-4));
		gap: var(--space-3);
	}
	.head {
		display: flex;
		align-items: center;
		gap: var(--space-2-5);
		min-width: 0;
	}
	h1 {
		margin: 0;
		min-width: 0;
		font-size: 1.6rem;
		font-weight: 600;
		overflow-wrap: break-word;
	}
	.phone h1 {
		font-size: 1.35rem;
	}
	h1 span,
	h1 i,
	h1 a {
		color: var(--muted);
		font-weight: 500;
		font-style: normal;
		text-decoration: none;
	}
	h1 a:hover {
		color: var(--text);
	}
	.lead {
		margin: calc(-1 * var(--space-2)) 0 0;
		color: var(--muted);
	}
	.phone .lead {
		margin-top: 0;
	}
	.unfold {
		width: 36px;
		height: 36px;
		flex: none;
		display: grid;
		place-items: center;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		background: none;
		color: var(--text);
		cursor: pointer;
	}
	.back {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
		min-height: 44px;
		margin: calc(-1 * var(--space-2)) 0;
		color: var(--accent);
		text-decoration: none;
		align-self: flex-start;
	}
</style>
