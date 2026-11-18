<script lang="ts">
	// 1.13 (Baustand step 11): the reminders in Kalender's side bar (and the
	// phone's level "Erinnerungen"): the lists with their color, name and
	// number of open reminders – one chosen, sorted by dragging or with the
	// keys (their own order, which the iPhone shares) –, below the chosen
	// list's open reminders to tick off or open, "Erledigte zeigen", a
	// field for a new one (Enter adds it to the chosen list) and the list's
	// settings (⋯). Reminders are told apart from events by their round box.
	import Icon from '#lib/components/Icon.svelte';
	import SortGrip, { sortKey } from '#lib/components/SortGrip.svelte';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { calColor } from '../calendar.ts';
	import { dueDay } from './tasks.ts';
	import { createTask, type DueTask, type TaskList } from './tasks.ts';
	import type { TaskData } from './task-data.svelte.ts';
	import { longDay } from '../calendar-text.ts';
	import { viewZone } from '../caldates.ts';
	import TaskChip from './TaskChip.svelte';

	type Props = {
		data: TaskData;
		today: string;
		phone?: boolean;
		onopen: (task: DueTask) => void;
		onedit: (list: TaskList) => void;
		onnewlist: () => void;
	};
	let { data, today, phone = false, onopen, onedit, onnewlist }: Props = $props();
	const keys = $derived(data.lists.map((l) => l.id));
	const chosen = $derived(data.lists.find((l) => l.id === data.chosen));
	let title = $state('');
	let adding = $state(false);
	let addError = $state('');
	const dueText = (task: DueTask) => {
		const d = dueDay(task, viewZone());
		return d
			? d < today && !task.done
				? t('calendar.tasks.overdue', { day: longDay(d, today) })
				: longDay(d, today)
			: '';
	};

	async function add(e: SubmitEvent) {
		e.preventDefault();
		const text = title.trim();
		if (!text || !data.chosen) return;
		adding = true;
		addError = '';
		try {
			await createTask({ list: data.chosen, title: text });
			title = '';
			data.refresh();
		} catch (err) {
			addError = apiErrorText(err);
		} finally {
			adding = false;
		}
	}
</script>

<section class="tasks" class:phone aria-labelledby="tasks-head" data-tasks>
	<h3 id="tasks-head">{t('calendar.tasks.heading')}</h3>
	{#if !data.ready && data.error}
		<p class="notice-error small" role="alert">{data.error}</p>
	{:else if !data.ready}
		<p class="muted small" role="status">{t('common.loading')}</p>
	{:else}
		<div class="lists" data-sort-list role="radiogroup" aria-labelledby="tasks-head">
			{#each data.lists as l (l.id)}
				<div
					class="lrow"
					class:on={l.id === data.chosen}
					style:--c={calColor(l.color)}
					data-tasklist={l.id}
					data-sort-group=""
					data-sort-key={l.id}
				>
					<button
						type="button"
						class="pick"
						role="radio"
						aria-checked={l.id === data.chosen}
						onclick={() => data.choose(l.id)}
						onkeydown={(e) => sortKey(e, keys, l.id, l.name, (next) => void data.sort(next))}
						><span class="sw" aria-hidden="true"></span><span class="ln">{l.name}</span><span
							class="cnt">{t('calendar.tasks.open', { n: l.open })}</span
						></button
					>
					<button
						type="button"
						class="ib"
						aria-label={t('calendar.list.settingsOf', { name: l.name })}
						title={t('calendar.list.settingsOf', { name: l.name })}
						onclick={() => onedit(l)}><Icon name="more" size={16} /></button
					>
					{#if keys.length > 1 && !phone}
						<SortGrip
							group=""
							key={l.id}
							{keys}
							name={l.name}
							onmove={(next) => void data.sort(next)}
						/>
					{/if}
				</div>
			{/each}
		</div>
		{#if chosen}
			<form class="add" onsubmit={add}>
				<input
					class="field"
					type="text"
					bind:value={title}
					maxlength="2000"
					disabled={adding}
					placeholder={t('calendar.tasks.addPlaceholder', { list: chosen.name })}
					aria-label={t('calendar.tasks.addPlaceholder', { list: chosen.name })}
				/>
				<button
					type="submit"
					class="ib"
					disabled={adding || !title.trim()}
					aria-label={t('calendar.tasks.add')}
					title={t('calendar.tasks.add')}><Icon name="plus" size={16} /></button
				>
			</form>
			{#if addError}<p class="notice-error small" role="alert">{addError}</p>{/if}
			<ul class="items" aria-label={chosen.name}>
				{#each data.items as task (task.list + '/' + task.id)}
					<li>
						<TaskChip
							wide={phone}
							{task}
							onopen={() => onopen(task)}
							ondone={() => void data.toggleDone(task)}
						/>
						{#if dueText(task)}<small
								class="due"
								class:late={!task.done && dueDay(task, viewZone()) < today}>{dueText(task)}</small
							>{/if}
					</li>
				{:else}
					<li class="muted small">{t('calendar.tasks.none')}</li>
				{/each}
			</ul>
			<label class="showdone"
				><input
					type="checkbox"
					bind:checked={data.showDone}
					onchange={() => void data.loadItems()}
				/>{t('calendar.tasks.showDone')}</label
			>
		{/if}
		{#if data.error}<p class="notice-error small" role="alert">{data.error}</p>{/if}
		<button type="button" class="btn add-list" onclick={onnewlist}
			><Icon name="plus" size={16} />{t('calendar.list.new')}</button
		>
	{/if}
</section>

<style>
	.tasks {
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
		min-width: 0;
	}
	h3 {
		margin: 0;
		font-size: var(--text-base);
		font-weight: 600;
	}
	.small {
		margin: 0;
		font-size: var(--text-sm);
	}
	.lists {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.lrow {
		display: flex;
		align-items: center;
		gap: 2px;
		border-radius: var(--radius-ctl);
	}
	.lrow.on {
		background: color-mix(in srgb, var(--accent) 12%, transparent);
	}
	.pick {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 2px var(--space-2);
		min-height: 36px;
		padding: 4px var(--space-2);
		border: 0;
		border-radius: var(--radius-ctl);
		background: none;
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.phone .pick {
		min-height: 44px;
	}
	.sw {
		flex: none;
		width: 12px;
		height: 12px;
		border-radius: 50%;
		border: 3px solid var(--c);
	}
	.ln {
		flex: 1 1 6rem;
		min-width: 0;
		overflow-wrap: break-word;
		hyphens: auto;
	}
	.cnt {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.ib {
		display: inline-grid;
		place-items: center;
		flex: none;
		min-width: 34px;
		min-height: 34px;
		padding: 0;
		border: 0;
		border-radius: var(--radius-ctl);
		background: none;
		color: var(--muted);
		cursor: pointer;
	}
	.phone .ib {
		min-width: 44px;
		min-height: 44px;
	}
	.ib:hover {
		background: color-mix(in srgb, var(--text) 7%, transparent);
	}
	.pick:focus-visible,
	.ib:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.add {
		display: flex;
		gap: var(--space-1);
		align-items: center;
	}
	.add .field {
		flex: 1;
		min-width: 0;
	}
	.items {
		display: flex;
		flex-direction: column;
		gap: 3px;
		margin: 0;
		padding: 0;
		list-style: none;
	}
	.due {
		display: block;
		padding-left: 22px;
		color: var(--muted);
		font-size: var(--text-xs);
	}
	.due.late {
		color: var(--warn-text);
		font-weight: 600;
	}
	.showdone {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		min-height: 32px;
		font-size: var(--text-sm);
	}
	.showdone input {
		accent-color: var(--accent);
	}
	.add-list {
		align-self: flex-start;
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
		min-height: 36px;
	}
	.phone .add-list {
		min-height: 44px;
	}
</style>
