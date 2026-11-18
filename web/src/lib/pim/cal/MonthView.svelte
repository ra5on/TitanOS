<script lang="ts">
	// 1.11 (step 15): the month (drafts 01/02, phone 42). Monday first with
	// ISO weeks; today framed and named "Heute". Titles wrap, never cut (a
	// long word may break anywhere).
	// 1.13 (Baustand steps 9/10): the month fills the frame – its weeks share
	// the height (never below about four lines each), the events of a day
	// scroll in their cell instead of "+N weitere" (the day's number opens
	// the day). Reminders due that day follow the events (TaskChip). On the
	// desktop an event of an own calendar is dragged onto another day
	// (day-drag.ts; its time stays). The phone shows no titles, a dot per
	// event in its calendar's color (a ring for a reminder), and the chosen
	// day's events below (the page).
	import { t } from '#lib/i18n/index.svelte.ts';
	import { clock, isoWeek, monthGrid } from '../caldates.ts';
	import { calColor, type Calendar, type CalEvent, type DayItem } from '../calendar.ts';
	import { calName, eventTitle, longDay, monthTitle, weekdayShort } from '../calendar-text.ts';
	import { dayDrag } from './day-drag.ts';
	import type { DueTask } from './tasks.ts';
	import TaskChip from './TaskChip.svelte';

	type Props = {
		month: string;
		today: string;
		byDay: Map<string, DayItem[]>;
		calFor: (id: string) => Calendar | undefined;
		phone?: boolean;
		/** The chosen day (phone). */
		selected?: string;
		canEdit?: (ev: CalEvent) => boolean;
		onopen: (ev: CalEvent) => void;
		onday: (day: string) => void;
		/** An event dragged onto another day. */
		onshift?: (ev: CalEvent, days: number) => void;
		tasks?: Map<string, DueTask[]>;
		ontask?: (task: DueTask) => void;
		ontaskdone?: (task: DueTask) => void;
	};
	let {
		month,
		today,
		byDay,
		calFor,
		phone = false,
		selected = '',
		canEdit = () => false,
		onopen,
		onday,
		onshift,
		tasks,
		ontask,
		ontaskdone,
	}: Props = $props();

	const weeks = $derived(monthGrid(month));
	const inMonth = (d: string) => d.slice(0, 7) === month.slice(0, 7);
	const items = (d: string) => byDay.get(d) ?? [];
	const dueOn = (d: string) => tasks?.get(d) ?? [];
</script>

<div
	class="month"
	class:phone
	role="grid"
	aria-label={monthTitle(month)}
	style:--weeks={weeks.length}
	use:dayDrag={{
		enabled: !phone && !!onshift,
		eventOf: (key) => [...byDay.values()].flat().find((it) => it.key === key)?.ev,
		onmove: (ev, n) => onshift?.(ev, n),
	}}
>
	<div class="mrow head" role="row">
		{#if !phone}<span class="kwh" role="columnheader">{t('calendar.kw')}</span>{/if}
		{#each [0, 1, 2, 3, 4, 5, 6] as i (i)}<span class="mh" role="columnheader"
				>{weekdayShort(i)}</span
			>{/each}
	</div>
	{#each weeks as w (w[0])}
		<div class="mrow" role="row">
			{#if !phone}<span class="kwc" role="rowheader">{isoWeek(w[0])}</span>{/if}
			{#each w as d (d)}
				{@const list = items(d)}
				{@const due = dueOn(d)}
				{#if phone}
					<div class="cell" class:out={!inMonth(d)} class:today={d === today} role="gridcell">
						<button
							type="button"
							class="pick"
							class:sel={d === selected}
							aria-pressed={d === selected}
							aria-label={`${longDay(d, today)}${d === today ? ' · ' + t('calendar.today') : ''} · ${t('calendar.count', { n: list.length })}${due.length ? ' · ' + t('calendar.tasks.count', { n: due.length }) : ''}`}
							onclick={() => onday(d)}
						>
							<span class="n">{Number(d.slice(8))}</span>
							<span class="marks" aria-hidden="true"
								>{#each list.slice(0, 3) as it (it.key)}<i
										style:--c={calColor(calFor(it.ev.calendar)?.color ?? '')}
									></i>{/each}{#if due.length}<i class="ring"></i>{/if}</span
							>
						</button>
					</div>
				{:else}
					<div
						class="cell"
						class:out={!inMonth(d)}
						class:today={d === today}
						role="gridcell"
						data-day={d}
					>
						<span class="dn"
							>{#if d === today}<span class="tag">{t('calendar.today')}</span>{/if}<button
								type="button"
								class="n"
								aria-label={longDay(d, today)}
								onclick={() => onday(d)}>{Number(d.slice(8))}</button
							></span
						>
						<div class="evs">
							{#each list as it (it.key)}
								{@const c = calFor(it.ev.calendar)}
								<button
									type="button"
									class="ev"
									style:--c={calColor(c?.color ?? '')}
									data-drag-day={canEdit(it.ev) && onshift ? d : undefined}
									data-drag-key={it.key}
									onclick={() => onopen(it.ev)}
									>{#if !it.part.allDay && it.part.first}<b>{clock(it.part.from)}</b>{/if}<span
										class="ev-t">{eventTitle(it.ev)}</span
									><span class="sr-only">{calName(c)}</span></button
								>
							{/each}
							{#each due as task (task.list + '/' + task.id)}
								<TaskChip {task} onopen={() => ontask?.(task)} ondone={() => ontaskdone?.(task)} />
							{/each}
						</div>
					</div>
				{/if}
			{/each}
		</div>
	{/each}
</div>

<style>
	.month {
		display: grid;
		grid-template-rows: auto repeat(var(--weeks), minmax(5.6rem, 1fr));
		flex: 1 1 auto;
		min-height: 0;
	}
	/* A narrow month leaves out the weeks' numbers: the days keep room for their titles. */
	.month:not(.phone) {
		container-type: inline-size;
	}
	@container (max-width: 640px) {
		.month .mrow {
			grid-template-columns: repeat(7, minmax(0, 1fr));
		}
		.kwh,
		.kwc {
			display: none;
		}
		.month .ev {
			padding: 1px 3px 1px 4px;
		}
	}
	.month.phone {
		grid-template-rows: auto;
		flex: none;
	}
	.mrow {
		display: grid;
		grid-template-columns: 2.2rem repeat(7, minmax(0, 1fr));
		min-height: 0;
	}
	.phone .mrow {
		grid-template-columns: repeat(7, minmax(0, 1fr));
	}
	.mh,
	.kwh {
		padding: var(--space-1-5) var(--space-2);
		color: var(--muted);
		font-size: var(--text-sm);
		border-bottom: 1px solid var(--border);
	}
	.phone .mh {
		padding: var(--space-1) 0;
		text-align: center;
		font-size: var(--text-xs);
	}
	.kwh,
	.kwc {
		text-align: center;
		padding-left: 0;
		padding-right: 0;
	}
	.kwc {
		padding-top: var(--space-1-5);
		color: var(--muted);
		font-size: var(--text-xs);
		border-bottom: 1px solid var(--srow-line);
	}
	.cell {
		min-width: 0;
		min-height: 0;
		display: flex;
		flex-direction: column;
		gap: 2px;
		padding: var(--space-1);
		border-left: 1px solid var(--srow-line);
		border-bottom: 1px solid var(--srow-line);
	}
	.cell:global([data-drop]) {
		background: color-mix(in srgb, var(--accent) 14%, transparent);
	}
	.cell.out {
		background: var(--srow-bg);
	}
	.cell.out .n {
		color: var(--muted);
	}
	.cell.today {
		box-shadow: inset 0 0 0 2px var(--accent);
	}
	.dn {
		display: flex;
		align-items: center;
		justify-content: flex-end;
		flex-wrap: wrap;
		flex: none;
		gap: var(--space-1);
		padding: 0 var(--space-1);
		font-size: var(--text-sm);
		line-height: 1.6;
	}
	.dn .n {
		padding: 0 2px;
		border: 0;
		border-radius: var(--radius-small);
		background: none;
		color: var(--text);
		font: inherit;
		cursor: pointer;
	}
	.today .n {
		font-weight: 700;
	}
	.tag {
		display: inline-block;
		padding: 0 6px;
		border-radius: var(--radius-small);
		background: var(--accent);
		color: var(--on-accent);
		font-size: var(--text-xs);
		font-weight: 600;
		line-height: 1.6;
	}
	/* The day's events scroll in their cell. */
	.evs {
		position: relative;
		flex: 1 1 auto;
		min-height: 0;
		display: flex;
		flex-direction: column;
		gap: 2px;
		overflow-y: auto;
		overscroll-behavior: contain;
		scrollbar-width: thin;
	}
	.ev {
		position: relative;
		display: flex;
		flex-wrap: wrap;
		flex: none;
		column-gap: 5px;
		width: 100%;
		margin: 0;
		padding: 1px 6px 1px 7px;
		border: 0;
		border-left: 3px solid var(--c);
		border-radius: 3px;
		background: color-mix(in srgb, var(--c) 14%, transparent);
		color: var(--text);
		font: inherit;
		font-size: var(--text-sm);
		line-height: 1.35;
		text-align: left;
		cursor: pointer;
	}
	.ev[data-drag-day] {
		cursor: grab;
	}
	.ev:global([data-dragging]) {
		opacity: 0.5;
	}
	.ev:hover {
		background: color-mix(in srgb, var(--c) 22%, transparent);
	}
	/* Titles are free text: in a narrow cell a long word like a file name
	   may break anywhere (WEB-UI "umbrechen"). */
	.ev-t {
		min-width: 0;
		overflow-wrap: anywhere;
		hyphens: auto;
	}
	.ev b {
		font-weight: 600;
		font-variant-numeric: tabular-nums;
	}
	.ev:focus-visible,
	.n:focus-visible,
	.pick:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
	}

	/* Phone: no titles, a dot per event. */
	.phone .cell {
		padding: 1px;
		border-left: 0;
		border-bottom: 0;
	}
	.phone .cell.today {
		box-shadow: none;
	}
	.phone .cell.out {
		background: none;
	}
	.pick {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 3px;
		width: 100%;
		min-height: 48px;
		padding: 5px 0 4px;
		border: 0;
		border-radius: var(--radius-ctl);
		background: none;
		color: var(--text);
		font: inherit;
		font-size: var(--text-md);
		cursor: pointer;
	}
	.phone .n {
		display: inline-grid;
		place-items: center;
		min-width: 30px;
		height: 30px;
		border-radius: 15px;
	}
	.pick.sel .n {
		background: var(--text);
		color: var(--bg);
		font-weight: 600;
	}
	.phone .today .n {
		color: var(--accent);
		font-weight: 700;
	}
	.phone .today .pick.sel .n {
		background: var(--accent);
		color: var(--on-accent);
	}
	.marks {
		display: flex;
		justify-content: center;
		gap: 3px;
		min-height: 6px;
	}
	.marks i {
		width: 6px;
		height: 6px;
		border-radius: 50%;
		background: var(--c);
	}
	.marks i.ring {
		background: none;
		border: 1.5px solid var(--muted);
	}
</style>
