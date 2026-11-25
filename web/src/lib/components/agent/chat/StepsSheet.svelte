<script lang="ts">
	// The sheet "Zusammenfassung" of one turn (from below on the phone, in
	// the middle on the desktop): every step on a fine line; a tap on one
	// shows "Eingabe" (its arguments) and "Ausgabe" (its result), or the
	// thoughts.
	import { fly } from 'svelte/transition';
	import { quintOut } from 'svelte/easing';
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import type { Entry } from '#lib/agent.ts';
	import BottomSheet from '../BottomSheet.svelte';
	import StepList from './StepList.svelte';
	import { argLines, preview, stepLabel, stepOutput, type StepsBlock } from './chat-text.svelte.ts';

	let {
		block,
		runningId,
		calm = false,
		busy = false,
		undone,
		onclose,
		onundo,
	}: {
		block: StepsBlock;
		runningId?: string;
		calm?: boolean;
		busy?: boolean;
		undone: Set<string>;
		onclose: () => void;
		onundo?: (step: Entry) => void;
	} = $props();

	let selection = $state<string | null>(null);
	const shown = $derived(
		selection === 'thinking' ? 'thinking' : (block.steps.find((s) => s.id === selection) ?? null),
	);
</script>

<BottomSheet label={t('agent.stepsSummary')} {calm} {onclose}>
	<div class="sum-head">
		<button
			type="button"
			class="sum-x"
			aria-label={shown ? t('agent.set.back') : t('common.close')}
			onclick={() => {
				if (shown) selection = null;
				else onclose();
			}}><Icon name={shown ? 'chevronLeft' : 'close'} size={18} /></button
		>
		<strong
			>{shown === 'thinking'
				? t('agent.thoughts')
				: shown
					? stepLabel(shown)
					: t('agent.stepsSummary')}</strong
		>
	</div>
	{#key shown}
		<div
			class="sum-body"
			in:fly={{ x: shown ? 36 : -36, duration: calm ? 0 : 300, easing: quintOut }}
		>
			{#if shown === 'thinking'}
				<div class="sum-box"><pre>{block.thinking}</pre></div>
			{:else if shown}
				{@const lines = argLines(shown.args)}
				{#if lines.length || preview(shown.args)}
					<h3 class="sum-group">{t('agent.stepInput')}</h3>
					<div class="sum-box">
						{#if lines.length}
							<dl>
								{#each lines as [k, v] (k)}<dt>{k}</dt>
									<dd>{v}</dd>{/each}
							</dl>
						{/if}
						{#if preview(shown.args)}<pre>{preview(shown.args)}</pre>{/if}
					</div>
				{/if}
				{#if stepOutput(shown)}
					<h3 class="sum-group">{t('agent.stepOutput')}</h3>
					<div class="sum-box"><pre>{stepOutput(shown)}</pre></div>
				{/if}
			{:else}
				<StepList
					{runningId}
					{block}
					tappable
					{busy}
					{undone}
					onshow={(step) => (selection = step === 'thinking' ? step : step.id)}
					onundo={onundo
						? (step) => {
								onclose();
								onundo(step);
							}
						: undefined}
				/>
			{/if}
		</div>
	{/key}
</BottomSheet>

<style>
	.sum-head {
		position: relative;
		display: flex;
		align-items: center;
		justify-content: center;
		min-height: 48px;
		padding: 0 64px;
		margin-bottom: var(--space-1-5);
		text-align: center;
	}
	.sum-head strong {
		font-weight: 600;
		overflow-wrap: anywhere;
	}
	.sum-x {
		position: absolute;
		left: var(--space-4);
		top: 2px;
		width: 44px;
		height: 44px;
		display: grid;
		place-items: center;
		border: 1px solid var(--border);
		border-radius: 50%;
		background: color-mix(in srgb, var(--text) 6%, var(--surface));
		color: var(--text);
		cursor: pointer;
	}
	.sum-x:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
	}
	.sum-group {
		margin: var(--space-3) 22px var(--space-1-5);
		color: var(--muted);
		font-size: var(--text-md);
		font-weight: 400;
	}
	.sum-box {
		margin: 0 var(--space-4);
		padding: var(--space-3) var(--space-4);
		border: 1px solid var(--border);
		border-radius: 16px;
		background: color-mix(in srgb, var(--text) 4%, var(--surface));
	}
	.sum-box dl {
		margin: 0;
		display: grid;
		gap: 2px;
	}
	.sum-box dt {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.sum-box dd {
		margin: 0 0 var(--space-1-5);
		overflow-wrap: anywhere;
	}
	.sum-box pre {
		margin: 0;
		max-height: 46dvh;
		overflow: auto;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
		font-family: inherit;
		font-size: var(--text-md);
		line-height: 1.5;
	}
</style>
