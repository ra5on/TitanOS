<script lang="ts">
	// 1.13 (Baustand step 11): a reminder due on a day of the month, week,
	// day or list – told apart from events by its shape, not only its color:
	// a round box to tick off (a check mark when done) and the words
	// "Erinnerung" for screen readers; a dashed edge instead of the event's
	// bar. The box ticks it off where it stands, the title opens it.
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { clock, viewZone } from '../caldates.ts';
	import { dueMinutes, type DueTask } from './tasks.ts';

	type Props = { task: DueTask; onopen: () => void; ondone: () => void; wide?: boolean };
	let { task, onopen, ondone, wide = false }: Props = $props();
	const minutes = $derived(dueMinutes(task, viewZone()));
	const title = $derived(task.title.trim() || t('calendar.untitled'));
</script>

<span class="task" class:done={task.done} class:wide data-task={task.list + '/' + task.id}>
	<button
		type="button"
		class="tick"
		aria-pressed={task.done}
		aria-label={task.done
			? t('calendar.tasks.reopen', { title })
			: t('calendar.tasks.done', { title })}
		title={task.done ? t('calendar.tasks.reopen', { title }) : t('calendar.tasks.done', { title })}
		onclick={ondone}
		><span class="ring"
			>{#if task.done}<Icon name="check" size={11} />{/if}</span
		></button
	><button type="button" class="tt" onclick={onopen}
		><span class="sr-only">{t('calendar.tasks.one')}: </span>{#if minutes !== null}<b
				>{clock(minutes)}</b
			>{/if}<span class="tn">{title}</span></button
	>
</span>

<style>
	.task {
		position: relative;
		display: flex;
		flex-wrap: wrap;
		align-items: flex-start;
		gap: 4px;
		flex: none;
		width: 100%;
		padding: 1px 4px;
		border: 1px dashed var(--muted);
		border-radius: 3px;
		font-size: var(--text-sm);
		line-height: 1.35;
	}
	.task.wide {
		padding: var(--space-1-5) var(--space-2);
		gap: var(--space-2);
	}
	.tick {
		flex: none;
		display: inline-grid;
		place-items: center;
		margin-top: 2px;
		padding: 0;
		border: 0;
		border-radius: 50%;
		background: none;
		color: var(--on-accent);
		cursor: pointer;
	}
	.ring {
		display: inline-grid;
		place-items: center;
		width: 14px;
		height: 14px;
		border: 1.5px solid var(--muted);
		border-radius: 50%;
	}
	.done .ring {
		border-color: var(--accent);
		background: var(--accent);
	}
	/* The list and the phone: a target of 44 px around a larger ring. */
	.wide {
		align-items: center;
	}
	.wide .tick {
		width: 44px;
		height: 44px;
		margin: -6px 0 -6px -6px;
	}
	.wide .ring {
		width: 20px;
		height: 20px;
	}
	.wide .tt {
		min-height: 44px;
		align-items: center;
		align-content: center;
	}
	.tt {
		display: flex;
		flex-wrap: wrap;
		column-gap: 5px;
		flex: 1 1 3.6rem;
		min-width: 0;
		padding: 0;
		border: 0;
		background: none;
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.tt b {
		font-weight: 600;
		font-variant-numeric: tabular-nums;
	}
	.tn {
		min-width: 0;
		overflow-wrap: anywhere;
		hyphens: auto;
	}
	.done .tn {
		color: var(--muted);
		text-decoration: line-through;
	}
	.tick:focus-visible,
	.tt:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
	}
</style>
