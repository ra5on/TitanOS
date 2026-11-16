<script lang="ts">
	// The steps and thoughts of one turn as one quiet line ("3 Schritte ·
	// 1,6 s · Docker-Apps gelesen ›"); a tap opens the sheet "Zusammenfassung".
	// With "Schritte offen zeigen" the list stands under it as well.
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import type { Entry } from '#lib/agent.ts';
	import StepList from './StepList.svelte';
	import { seconds, stepsGist, type StepsBlock } from './chat-text.svelte.ts';

	let {
		block,
		showSteps = false,
		busy = false,
		undone,
		onopen,
		onundo,
	}: {
		block: StepsBlock;
		/** Open thinking and tool steps by default (PLAN 53). */
		showSteps?: boolean;
		busy?: boolean;
		undone: Set<string>;
		onopen: () => void;
		onundo?: (step: Entry) => void;
	} = $props();

	const tools = $derived(block.steps.filter((s) => s.role === 'tool').length);
	const gist = $derived(stepsGist(block.steps));
</script>

<button type="button" class="steps-line" aria-haspopup="dialog" onclick={onopen}>
	<span class="count"
		>{block.steps.length
			? t('agent.steps', { count: tools }) + seconds(block.steps)
			: t('agent.thoughts')}</span
	>
	{#if gist}<span class="gist">· {gist}</span>{/if}
	<Icon name="chevronRight" size={14} />
</button>
{#if showSteps}<StepList {block} inline {busy} {undone} {onundo} />{/if}

<style>
	/* Steps and thoughts of a turn: one quiet line; a tap opens the sheet
	   "Zusammenfassung" with every step, its input and its output. */
	.steps-line {
		align-self: flex-start;
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
		max-width: 100%;
		min-height: 32px;
		padding: 0 var(--space-1);
		margin-left: calc(-1 * var(--space-1));
		border: 0;
		border-radius: 8px;
		background: none;
		color: var(--muted);
		font: inherit;
		font-size: 0.9em;
		text-align: left;
		cursor: pointer;
		animation: come 0.32s cubic-bezier(0.22, 1, 0.36, 1) both;
		transition:
			background-color 0.16s ease,
			color 0.16s ease,
			border-color 0.16s ease;
	}
	@keyframes come {
		from {
			opacity: 0;
			transform: translateY(6px);
		}
	}
	:global(.agent.calm) .steps-line {
		animation: none;
		transition: none;
	}
	@media (prefers-reduced-motion: reduce) {
		.steps-line {
			animation: none;
		}
	}
	.steps-line:hover {
		color: var(--text);
		background: color-mix(in srgb, var(--text) 6%, transparent);
	}
	.steps-line:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
	}
	.steps-line :global(svg) {
		flex: none;
	}
	.count {
		flex: none;
	}
	/* The short summary of the steps wraps instead of ending in "…"
	   (owner 16 Nov 2026: no cut-off text). */
	.gist {
		min-width: 0;
		overflow-wrap: anywhere;
		text-align: left;
	}
	@media (max-width: 760px) {
		.steps-line {
			min-height: 44px;
		}
		.gist {
			display: none;
		}
	}
</style>
