<script lang="ts">
	// One answer of the agent: its text, the cards of what the turn's steps
	// read (under the turn's last answer), the buttons of a question, and the
	// small row under it – Vorlesen, Kopieren, "Neu erzeugen" under the last
	// answer and what the agent remembered right before it ("✦ Gemerkt: …").
	import Icon from '#lib/components/Icon.svelte';
	import { canSpeak, speak, speech } from '#lib/agent-speech.svelte.ts';
	import { i18n, t } from '#lib/i18n/index.svelte.ts';
	import type { Entry } from '#lib/agent.ts';
	import ResultCards from '../ResultCards.svelte';
	import Markdown from './Markdown.svelte';
	import { noteText, type Copied } from './chat-text.svelte.ts';

	let {
		entry,
		cards,
		active = false,
		learned = [],
		copied,
		onchoice,
		onregenerate,
	}: {
		entry: Entry;
		/** The steps of the turn when this answer ends it: their cards. */
		cards: Entry[] | null;
		/** The newest answer, nothing runs or waits: cards and choices work. */
		active?: boolean;
		/** Notes the agent wrote right before this answer. */
		learned?: Entry[];
		copied: Copied;
		onchoice: (choice: string) => void;
		/** "Neu erzeugen": only under the last answer, when it can be asked again. */
		onregenerate?: () => void;
	} = $props();
	const done = $derived(copied.of(entry.id));
</script>

<div class="answer" id={'agent-entry-' + entry.id} tabindex="-1">
	{#if entry.content}
		<Markdown text={entry.content} />
	{/if}
	{#if cards}
		<ResultCards steps={cards} {active} onask={onchoice} />
	{/if}
	{#if entry.choices?.length && active}
		<div class="choices">
			{#each entry.choices as choice, n (choice)}
				<button
					type="button"
					class="btn"
					class:btn-primary={n === entry.choices.length - 1}
					onclick={() => onchoice(choice)}>{choice}</button
				>
			{/each}
		</div>
	{/if}
	{#if entry.content || learned.length}
		<div class="acts">
			{#if entry.content && canSpeak()}
				{@const reading = speech.id === entry.id}
				<button
					type="button"
					class="act"
					class:on={reading}
					aria-pressed={reading}
					onclick={() => speak(entry.id, entry.content ?? '', i18n.locale)}
					title={reading ? t('agent.readStop') : t('agent.readAloud')}
					><Icon name={reading ? 'stop' : 'volume'} size={17} /><span class="sr-only"
						>{reading ? t('agent.readStop') : t('agent.readAloud')}</span
					></button
				>
			{/if}
			{#if entry.content}
				<button
					type="button"
					class="act"
					title={t('agent.copy')}
					onclick={() => copied.copy(entry.id, entry.content ?? '')}
					><Icon name={done?.ok ? 'check' : 'copy'} size={17} /><span class="sr-only"
						>{done
							? done.ok
								? t('agent.copiedShort')
								: t('agent.copyFailed')
							: t('agent.copy')}</span
					></button
				>
				{#if done}<span class="sr-only" role="status"
						>{done.ok ? t('agent.copiedShort') : t('agent.copyFailed')}</span
					>{/if}
			{/if}
			{#if onregenerate}
				<button type="button" class="act" title={t('agent.regenerate')} onclick={onregenerate}
					><Icon name="refresh" size={17} /><span class="sr-only">{t('agent.regenerate')}</span
					></button
				>
			{/if}
			{#each learned as n (n.id)}
				<span class="learned">✦ {t(noteText[n.note ?? 'memory'])}: {n.content}</span>
			{/each}
		</div>
	{/if}
</div>

<style>
	.answer {
		line-height: 1.6;
		overflow-wrap: anywhere;
	}
	.choices {
		display: flex;
		gap: var(--space-2);
		flex-wrap: wrap;
		margin-top: var(--space-1-5);
	}
	/* The small row under an answer: Vorlesen, Kopieren, Gemerkt. */
	.acts {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: var(--space-1) var(--space-1-5);
		margin-top: 2px;
		margin-left: calc(-1 * var(--space-1-5));
		font-size: var(--text-sm);
		color: var(--muted);
		/* What comes with a finished answer fades in instead of popping up. */
		animation: settle 0.35s ease-out both;
	}
	@keyframes settle {
		from {
			opacity: 0;
		}
	}
	.act {
		display: inline-flex;
		align-items: center;
		gap: 5px;
		justify-content: center;
		min-height: 32px;
		min-width: 32px;
		padding: 0 var(--space-1-5);
		border: 0;
		border-radius: 8px;
		background: none;
		color: var(--muted);
		font: inherit;
		cursor: pointer;
	}
	.act:hover,
	.act.on {
		color: var(--text);
		background: color-mix(in srgb, var(--text) 7%, transparent);
	}
	.act:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
	}
	.learned {
		min-width: 0;
		overflow-wrap: anywhere;
	}
	/* What can be pressed changes colour softly, not at once (0.100). */
	.act,
	.choices :global(.btn) {
		transition:
			background-color 0.16s ease,
			color 0.16s ease,
			border-color 0.16s ease;
	}
	:global(.agent.calm) .act {
		transition: none;
	}
	@media (prefers-reduced-motion: reduce) {
		.acts {
			animation: none;
		}
	}
	@media (max-width: 760px) {
		.act {
			min-height: 44px;
			min-width: 44px;
		}
	}
</style>
