<script lang="ts">
	// The steps and thoughts of one turn, one under the other on a fine line:
	// in the sheet "Zusammenfassung" (each a button that shows its input and
	// output) or open under the line of the turn ("Schritte offen zeigen").
	// "Rückgängig" stands where TitanOS can take a step back.
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { stepDetail, stepFailed, type Entry } from '#lib/agent.ts';
	import { stepLabel, UNDOABLE, type StepsBlock } from './chat-text.svelte.ts';

	let {
		block,
		tappable = false,
		inline = false,
		busy = false,
		undone,
		onshow,
		onundo,
	}: {
		block: StepsBlock;
		/** Each step opens what it got and gave (in the sheet). */
		tappable?: boolean;
		/** Open in the chat under the line of the turn. */
		inline?: boolean;
		busy?: boolean;
		/** Steps taken back already: tool|summary. */
		undone: Set<string>;
		onshow?: (step: Entry | 'thinking') => void;
		onundo?: (step: Entry) => void;
	} = $props();
</script>

{#snippet stepBody(step: Entry | null, chevron: boolean)}
	<span class="mark"
		><Icon
			name={!step ? 'bulb' : stepFailed(step) ? 'close' : step.role === 'note' ? 'shield' : 'check'}
			size={16}
		/></span
	>
	<span class="what"
		>{step ? stepLabel(step) : t('agent.thoughts')}{#if step && stepDetail(step.args)}<small
				>{stepDetail(step.args)}</small
			>{/if}</span
	>
	{#if step && stepFailed(step)}<span class="sr-only">{t('agent.stepFailed')}</span>{/if}
	{#if step && (step.durationMs ?? 0) >= 100}<span class="time"
			>{((step.durationMs ?? 0) / 1000).toFixed(1).replace('.', ',')} s</span
		>{/if}
	{#if chevron}<Icon name="chevronRight" size={15} />{/if}
{/snippet}

<ol class="step-list" class:inline>
	{#if block.thinking}
		<li>
			{#if tappable}
				<button type="button" class="step" onclick={() => onshow?.('thinking')}
					>{@render stepBody(null, true)}</button
				>
			{:else}
				<div class="step">{@render stepBody(null, false)}</div>
			{/if}
		</li>
	{/if}
	{#each block.steps as step (step.id)}
		<li class:failed={stepFailed(step)}>
			{#if tappable}
				<button type="button" class="step" onclick={() => onshow?.(step)}
					>{@render stepBody(step, true)}</button
				>
			{:else}
				<div class="step">{@render stepBody(step, false)}</div>
			{/if}
			{#if onundo && step.ok && step.tool && UNDOABLE.includes(step.tool) && !undone.has(step.tool + '|' + (step.summary ?? '')) && !busy}
				<button type="button" class="undo" onclick={() => onundo(step)}>{t('agent.undo')}</button>
			{/if}
		</li>
	{/each}
</ol>

<style>
	.step-list {
		list-style: none;
		margin: 0;
		padding: 0 var(--space-3);
	}
	.step-list.inline {
		padding: 0;
		font-size: 0.9em;
	}
	.step-list li {
		position: relative;
		display: flex;
		align-items: center;
		gap: var(--space-2);
	}
	/* The steps hang on one fine line. */
	.step-list li + li::before {
		content: '';
		position: absolute;
		left: 21px;
		top: -7px;
		height: 14px;
		width: 1px;
		background: var(--border);
	}
	.step {
		flex: 1;
		min-width: 0;
		display: flex;
		align-items: center;
		gap: var(--space-3);
		min-height: 52px;
		padding: var(--space-1-5) var(--space-2-5);
		border: 0;
		border-radius: 12px;
		background: none;
		color: var(--text);
		font: inherit;
		text-align: left;
	}
	button.step {
		cursor: pointer;
	}
	button.step:hover {
		background: color-mix(in srgb, var(--text) 6%, transparent);
	}
	.step:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
	}
	.inline .step {
		min-height: 34px;
		padding-left: 0;
	}
	.inline li + li::before {
		left: 8px;
		top: -4px;
		height: 8px;
	}
	.step :global(svg) {
		flex: none;
		color: var(--muted);
	}
	.mark {
		flex: none;
		display: grid;
		place-items: center;
		width: 22px;
	}
	.what {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		line-height: 1.3;
		overflow-wrap: anywhere;
	}
	.what small {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.step .time {
		flex: none;
		color: var(--muted);
		font-size: var(--text-xs);
	}
	.failed .mark :global(svg),
	.failed .what {
		color: var(--warn-text);
	}
	.undo {
		flex: none;
		min-height: 30px;
		border: 1px solid var(--border);
		background: none;
		color: var(--text);
		border-radius: 999px;
		padding: 0 var(--space-2-5);
		font: inherit;
		font-size: var(--text-sm);
		cursor: pointer;
	}
	/* What can be pressed changes colour softly, not at once (0.100). */
	button.step {
		transition:
			background-color 0.16s ease,
			color 0.16s ease,
			border-color 0.16s ease;
	}
	:global(.agent.calm) button.step {
		transition: none;
	}
</style>
