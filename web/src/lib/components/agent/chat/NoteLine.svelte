<script lang="ts">
	// A note of TitanOS in the chat: a planned task or reminder as a card, the
	// summary of older lines or a changed copy to open, what the agent
	// remembered, a snapshot taken, or a line such as "Angehalten" – with
	// "Weitermachen" after an error or a limit at the end of the chat.
	import { t } from '#lib/i18n/index.svelte.ts';
	import type { Entry } from '#lib/agent.ts';
	import { noteText } from './chat-text.svelte.ts';

	let {
		entry,
		learn = false,
		last = false,
		onretry,
		onplanned,
	}: {
		entry: Entry;
		/** Something the agent remembered (shown as a small chip). */
		learn?: boolean;
		/** The last line of the chat: an error or a limit offers to go on. */
		last?: boolean;
		onretry?: () => void;
		/** Opens the overview of planned tasks (from a confirmation). */
		onplanned?: () => void;
	} = $props();
</script>

{#if entry.note === 'schedule' && entry.content?.includes('„')}
	<div class="plan-card">
		<span class="plan-ic" aria-hidden="true"
			>{entry.content.startsWith('Erinnerung') ? '⏰' : '🗓️'}</span
		>
		<span class="plan-text">{entry.content}</span>
		{#if onplanned}<button type="button" class="btn" onclick={onplanned}
				>{t('agent.planned')}</button
			>{/if}
	</div>
{:else if entry.note === 'summary'}
	<details class="note edited">
		<summary>{t('agent.noteSummary')}</summary>
		<pre>{entry.content}</pre>
	</details>
{:else if entry.note === 'snapshot'}
	<span class="learn snap">{entry.content}</span>
{:else if entry.note === 'edited'}
	<details class="note edited">
		<summary>{t('agent.noteEdited')}</summary>
		<pre>{entry.content}</pre>
	</details>
{:else if learn}
	<span class="learn">✦ {t(noteText[entry.note ?? 'memory'])}: {entry.content}</span>
{:else}
	<div class="note" class:bad={entry.note === 'error'}>
		{t(noteText[entry.note ?? ''] ?? 'agent.noteError', {
			count: entry.content ?? '',
		})}
		{#if entry.note === 'error' && entry.content}<span class="muted"> · {entry.content}</span>{/if}
		{#if ['error', 'interrupted', 'limit', 'timelimit', 'loop'].includes(entry.note ?? '') && last && onretry}
			<button type="button" class="btn" onclick={onretry}>{t('agent.continue')}</button>
		{/if}
	</div>
{/if}

<style>
	.learn {
		align-self: flex-start;
		font-size: var(--text-sm);
		padding: 3px var(--space-2-5);
		border-radius: 999px;
		background: var(--chip);
		color: var(--text);
	}
	.note {
		font-size: var(--text-base);
		color: var(--muted);
		display: flex;
		align-items: center;
		gap: var(--space-2);
		flex-wrap: wrap;
	}
	.note.bad {
		color: var(--warn-text);
	}
	.note summary {
		cursor: pointer;
	}
	.edited pre {
		white-space: pre-wrap;
		font-size: var(--text-sm);
		max-height: 240px;
		overflow: auto;
	}
	.plan-card {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		padding: var(--space-2-5) var(--space-4);
		border-radius: var(--radius-tile);
		border: 1px solid var(--border);
		background: var(--surface);
	}
	.plan-ic {
		font-size: 20px;
		flex: none;
	}
	.plan-text {
		flex: 1;
		min-width: 0;
	}
</style>
