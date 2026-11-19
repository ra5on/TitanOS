<script lang="ts">
	// 1.11 (step 15): the month, Monday first with ISO weeks.
	// 1.15 (PLAN 115-5, drafts 36/37, owner 19 Nov 2026 "wie Apple"): one
	// bar per event in its calendar's color, a multi-day event one bar
	// across its days (a piece per week, month-lanes.ts), today a red circle
	// (and the word for screen readers). Each week is a CSS grid: the days
	// as columns behind, the bars in lanes over them – placed from the dates
	// alone, nothing measured.
	// Desktop (and every window from 620 px): the weeks share the frame's
	// height; a bar carries its time and title, which wrap (never cut); the
	// lanes that do not fit in a week's row give way to "+N weitere" under
	// each day (measured once per size or change of the events, as Apple's
	// "+3"); it opens the day, as the day's number does. The day's number opens
	// the day. An event of an own calendar is dragged onto another day
	// (day-drag.ts; its time stays). Reminders due that day stand after the
	// events (TaskChip).
	// Phone (`phone`): no titles, bars of 6 px, at most `maxLanes` lanes and
	// "+N" for the rest of a day; a tap on a day picks it. With `only` the
	// days of other months stay empty and the month's name stands over its
	// first day (the continuous months of MonthScroll).
	import { onMount, tick } from 'svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { clock, isoWeek, monthGrid } from '../caldates.ts';
	import { calColor, type Calendar, type CalEvent, type DayItem } from '../calendar.ts';
	import {
		calName,
		eventTitle,
		longDay,
		monthName,
		monthTitle,
		weekdayShort,
	} from '../calendar-text.ts';
	import { dayDrag } from './day-drag.ts';
	import { hiddenOf, weekLayout, weekSplit } from './month-lanes.ts';
	import type { DueTask } from './tasks.ts';
	import TaskChip from './TaskChip.svelte';

	type Props = {
		month: string;
		today: string;
		byDay: Map<string, DayItem[]>;
		calFor: (id: string) => Calendar | undefined;
		phone?: boolean;
		/** Phone: only the month's own days, its name over the first one. */
		only?: boolean;
		/** Phone: the lanes shown before "+N". */
		maxLanes?: number;
		/** The chosen day (phone). */
		selected?: string;
		canEdit?: (ev: CalEvent) => boolean;
		onopen: (ev: CalEvent) => void;
		onday: (day: string, cell?: HTMLElement) => void;
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
		only = false,
		maxLanes = 3,
		selected = '',
		canEdit = () => false,
		onopen,
		onday,
		onshift,
		tasks,
		ontask,
		ontaskdone,
	}: Props = $props();

	const inMonth = (d: string) => d.slice(0, 7) === month.slice(0, 7);
	/** The weeks; with `only` the other months' days are '' (empty places). */
	const weeks = $derived(
		monthGrid(month)
			.map((w) => (only ? w.map((d) => (inMonth(d) ? d : '')) : w))
			.filter((w) => w.some((d) => d)),
	);
	// Phone: everything in lanes (bars without titles). Desktop: the bars of
	// events over days in lanes, each day's own events in its stack.
	const layouts = $derived(phone ? weeks.map((w) => weekLayout(w, byDay, tasks)) : []);
	const splits = $derived(phone ? [] : weeks.map((w) => weekSplit(w, byDay, tasks)));
	const items = (d: string) => (d ? (byDay.get(d) ?? []) : []);
	const dueOn = (d: string) => (d ? (tasks?.get(d) ?? []) : []);
	const firstCol = $derived(weeks[0]?.findIndex((d) => d) ?? 0);

	// ---- Desktop: what fits each day of a week's row, the rest as "+N weitere" ----
	let root = $state<HTMLElement>();
	/** Per day: how many of its own entries are shown (missing: all). */
	let fit = $state<Record<string, number>>({});
	/** Per week (its Monday): the lanes of bars shown when not all fit. */
	let fitLanes = $state<Record<string, number>>({});
	let frame = 0;
	function refitSoon() {
		if (phone || frame) return;
		frame = requestAnimationFrame(() => {
			frame = 0;
			void refit();
		});
	}
	async function refit() {
		if (!root) return;
		fit = {};
		fitLanes = {};
		await tick();
		if (!root) return;
		const next: Record<string, number> = {};
		const lanes: Record<string, number> = {};
		for (const week of root.querySelectorAll<HTMLElement>('.mweek')) {
			if (week.scrollHeight <= week.clientHeight + 1) continue;
			// Room for the line "+N weitere" below what stays.
			const limit = week.getBoundingClientRect().top + week.clientHeight - 22;
			// Even the bars over days do not fit: the lanes that do, then only "+N".
			let laneCap = Infinity;
			for (const el of week.querySelectorAll<HTMLElement>(':scope > .ev[data-lane]'))
				if (el.getBoundingClientRect().bottom > limit)
					laneCap = Math.min(laneCap, Number(el.dataset.lane));
			if (laneCap < Infinity) {
				lanes[week.dataset.week ?? ''] = laneCap;
				continue;
			}
			for (const stack of week.querySelectorAll<HTMLElement>('.stack')) {
				const kids = [...stack.querySelectorAll<HTMLElement>(':scope > [data-entry]')];
				const bottoms = kids.map((el) => el.getBoundingClientRect().bottom);
				// All of them fit without the line: nothing to hide.
				if (!bottoms.length || bottoms[bottoms.length - 1] <= limit + 24) continue;
				const over = bottoms.findIndex((b) => b > limit);
				if (over >= 0) next[stack.dataset.stack ?? ''] = over;
			}
		}
		fit = next;
		fitLanes = lanes;
	}
	$effect(() => {
		void splits;
		refitSoon();
	});
	onMount(() => {
		if (phone || typeof ResizeObserver !== 'function' || !root) return;
		const watch = new ResizeObserver(refitSoon);
		watch.observe(root);
		return () => {
			watch.disconnect();
			cancelAnimationFrame(frame);
		};
	});

	const label = (d: string, n: number, due: number) =>
		`${longDay(d, today)}${d === today ? ' · ' + t('calendar.today') : ''} · ${t('calendar.count', { n })}${due ? ' · ' + t('calendar.tasks.count', { n: due }) : ''}`;
</script>

<div
	class="month"
	bind:this={root}
	class:phone
	class:only
	aria-label={monthTitle(month)}
	role="group"
	data-month={month.slice(0, 7)}
	style:--weeks={weeks.length}
	use:dayDrag={{
		enabled: !phone && !!onshift,
		eventOf: (key) => [...byDay.values()].flat().find((it) => it.key === key)?.ev,
		onmove: (ev, n) => onshift?.(ev, n),
	}}
>
	{#if only}
		<h2 class="mname" style:--col={firstCol < 5 ? firstCol + 1 : 1}>{monthName(month)}</h2>
	{:else}
		<div class="mrow head">
			{#if !phone}<span class="kwh">{t('calendar.kw')}</span>{/if}
			{#each [0, 1, 2, 3, 4, 5, 6] as i (i)}<span class="mh">{weekdayShort(i)}</span>{/each}
		</div>
	{/if}
	{#each weeks as w, wi (w.find((d) => d))}
		{#if phone}
			{@const lay = layouts[wi]}
			{@const shown = Math.min(lay.lanes, maxLanes)}
			{@const hidden = maxLanes < lay.lanes ? hiddenOf(lay, maxLanes) : []}
			<div class="mweek" style:--lanes={Math.max(1, shown)} data-week={w.find((d) => d)}>
				{#each w as d, i (d || 'x' + i)}
					{#if !d}
						<span class="cell blank" style:grid-column={i + 1}></span>
					{:else}
						{@const n = items(d).length}
						<button
							type="button"
							class="cell pick"
							class:today={d === today}
							class:sel={d === selected}
							class:we={i > 4}
							class:out={!inMonth(d)}
							style:grid-column={i + 1}
							data-day={d}
							aria-pressed={d === selected}
							aria-label={label(d, n, dueOn(d).length)}
							onclick={(e) => onday(d, e.currentTarget)}
							><span class="n">{Number(d.slice(8))}</span>{#if hidden[i]}<span
									class="more"
									aria-hidden="true">+{hidden[i]}</span
								>{/if}</button
						>
					{/if}
				{/each}
				{#each lay.bars as b (b.key)}
					{#if b.lane < maxLanes}
						<span
							class="bar"
							class:before={b.before}
							class:after={b.after}
							style:--c={calColor(calFor(b.item.ev.calendar)?.color ?? '')}
							style:grid-column="{b.start + 1} / span {b.span}"
							style:grid-row={b.lane + 2}
							aria-hidden="true"
						></span>
					{/if}
				{/each}
				{#each lay.tasks as tk (tk.task.list + '/' + tk.task.id)}
					{#if tk.lane < maxLanes}
						<span
							class="bar ring"
							style:grid-column={tk.start + 1}
							style:grid-row={tk.lane + 2}
							aria-hidden="true"
						></span>
					{/if}
				{/each}
			</div>
		{:else}
			{@const sp = splits[wi]}
			{@const laneCap = fitLanes[w[0]] ?? Infinity}
			{@const lanesShown = Math.min(sp.lanes, laneCap)}
			<div class="mweek kw" style:--lanes={Math.max(1, lanesShown)} data-week={w[0]}>
				<span class="kwc">{isoWeek(w[0])}</span>
				{#each w as d, i (d)}
					<div
						class="cell"
						class:out={!inMonth(d)}
						class:today={d === today}
						class:we={i > 4}
						style:grid-column={i + 2}
						data-day={d}
					>
						<span class="dn"
							><button
								type="button"
								class="n"
								aria-label={label(d, items(d).length, dueOn(d).length)}
								onclick={() => onday(d)}>{Number(d.slice(8))}</button
							>{#if d === today}<span class="sr-only">{t('calendar.today')}</span>{/if}</span
						>
					</div>
				{/each}
				{#each sp.bars.filter((x) => x.lane < laneCap) as b (b.key)}
					{@const c = calFor(b.item.ev.calendar)}
					{@const day = w[b.start]}
					<button
						type="button"
						class="ev long"
						class:before={b.before}
						class:after={b.after}
						style:--c={calColor(c?.color ?? '')}
						style:grid-column="{b.start + 2} / span {b.span}"
						style:grid-row={b.lane + 2}
						data-start={day}
						data-lane={b.lane}
						data-drag-day={canEdit(b.item.ev) && onshift ? day : undefined}
						data-drag-key={b.item.key}
						onclick={() => onopen(b.item.ev)}
						>{#if !b.item.part.allDay && b.item.part.first}<b>{clock(b.item.part.from)}</b
							>{/if}<span class="ev-t">{eventTitle(b.item.ev)}</span><span class="sr-only"
							>{calName(c)}</span
						></button
					>
				{/each}
				{#each w as d, i (d)}
					{@const st = sp.days[i]}
					{@const own = st.items.length + st.tasks.length}
					{@const barsHidden = sp.bars.filter(
						(b) => b.lane >= laneCap && b.start <= i && i < b.start + b.span,
					).length}
					{@const all = own + barsHidden}
					{@const cap = laneCap < Infinity ? 0 : (fit[d] ?? own)}
					<div
						class="stack"
						style:grid-column={i + 2}
						style:grid-row={lanesShown + 2}
						data-stack={d}
					>
						{#each st.items.slice(0, cap) as it (it.key)}
							{@const c = calFor(it.ev.calendar)}
							<button
								type="button"
								class="ev"
								style:--c={calColor(c?.color ?? '')}
								data-entry
								data-start={d}
								data-drag-day={canEdit(it.ev) && onshift ? d : undefined}
								data-drag-key={it.key}
								onclick={() => onopen(it.ev)}
								>{#if it.part.first}<b>{clock(it.part.from)}</b>{/if}<span class="ev-t"
									>{eventTitle(it.ev)}</span
								><span class="sr-only">{calName(c)}</span></button
							>
						{/each}
						{#each st.tasks.slice(0, Math.max(0, cap - st.items.length)) as task (task.list + '/' + task.id)}
							<span class="tk" data-entry data-start={d}
								><TaskChip
									{task}
									onopen={() => ontask?.(task)}
									ondone={() => ontaskdone?.(task)}
								/></span
							>
						{/each}
						{#if cap < all}
							<button
								type="button"
								class="more-btn"
								aria-label={`${t('calendar.more', { n: all - cap })} · ${longDay(d, today)}`}
								onclick={() => onday(d)}>{t('calendar.more', { n: all - cap })}</button
							>
						{/if}
					</div>
				{/each}
			</div>
		{/if}
	{/each}
</div>

<style>
	.month {
		--cal-gap: 2px;
		display: grid;
		grid-template-rows: auto repeat(var(--weeks), minmax(4.5rem, 1fr));
		flex: 1 1 auto;
		min-height: 0;
		container-type: inline-size;
	}
	.mrow {
		display: grid;
		grid-template-columns: 2.2rem repeat(7, minmax(0, 1fr));
	}
	.phone .mrow {
		grid-template-columns: repeat(7, minmax(0, 1fr));
	}
	.mh,
	.kwh {
		padding: var(--space-1-5) var(--space-2);
		color: var(--muted);
		font-size: var(--text-sm);
		text-align: right;
		border-bottom: 1px solid var(--border);
	}
	.kwh {
		text-align: center;
		padding-inline: 0;
	}
	.phone .mh {
		padding: var(--space-1) 0;
		text-align: center;
		font-size: var(--text-xs);
		font-weight: 600;
	}

	/* A week: the days behind, the bars in lanes over them. */
	.mweek {
		position: relative;
		display: grid;
		grid-template-columns: repeat(7, minmax(0, 1fr));
		grid-template-rows: 30px repeat(var(--lanes), auto) minmax(0, 1fr);
		row-gap: var(--cal-gap);
		min-height: 0;
		overflow-y: auto;
		overscroll-behavior: contain;
		scrollbar-width: thin;
		padding-bottom: var(--space-1);
	}
	.mweek.kw {
		grid-template-columns: 2.2rem repeat(7, minmax(0, 1fr));
	}
	.kwc {
		grid-column: 1;
		grid-row: 1 / -1;
		padding-top: var(--space-1-5);
		color: var(--muted);
		font-size: var(--text-xs);
		text-align: center;
		border-bottom: 1px solid var(--srow-line);
	}
	.cell {
		grid-row: 1 / -1;
		min-width: 0;
		min-height: 0;
		border-left: 1px solid var(--srow-line);
		border-bottom: 1px solid var(--srow-line);
	}
	.cell:global([data-drop]) {
		background: color-mix(in srgb, var(--accent) 14%, transparent);
	}
	.cell.out {
		background: var(--srow-bg);
	}
	.dn {
		position: sticky;
		top: 0;
		display: flex;
		justify-content: flex-end;
		padding: var(--space-1) var(--space-1) 0;
	}
	.dn .n {
		display: inline-grid;
		place-items: center;
		min-width: 26px;
		height: 26px;
		padding: 0 5px;
		border: 0;
		border-radius: 13px;
		background: none;
		color: var(--text);
		font: inherit;
		font-size: var(--text-sm);
		font-variant-numeric: tabular-nums;
		cursor: pointer;
	}
	.dn .n:hover {
		background: color-mix(in srgb, var(--text) 8%, transparent);
	}
	.cell.out .n,
	.cell.we:not(.today) .n {
		color: var(--muted);
	}
	.cell.today .n {
		background: var(--cal-today, #d70015);
		color: var(--cal-on-today, #fff);
		font-weight: 700;
	}
	/* The day numbers stand in the first row, the bars below them. */
	.mweek > .ev {
		align-self: start;
		margin: 0 3px;
		min-width: 0;
		z-index: 1;
	}
	/* A day's own events under the bars. */
	.stack {
		z-index: 1;
		display: flex;
		flex-direction: column;
		align-items: stretch;
		gap: var(--cal-gap);
		min-width: 0;
		min-height: 0;
		padding: 0 3px;
	}
	.stack > .tk {
		display: flex;
	}
	.ev {
		position: relative;
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		column-gap: 5px;
		padding: 1px 6px 1px 7px;
		border: 0;
		border-left: 3px solid var(--c);
		border-radius: 4px;
		background: color-mix(in srgb, var(--c) 16%, transparent);
		color: var(--text);
		font: inherit;
		font-size: var(--text-sm);
		line-height: 1.35;
		text-align: left;
		cursor: pointer;
	}
	/* A multi-day bar: filled in its color, open where it runs on. */
	.ev.long {
		background: color-mix(in srgb, var(--c) 30%, transparent);
	}
	.ev.before {
		margin-left: 0;
		border-left-width: 0;
		border-top-left-radius: 0;
		border-bottom-left-radius: 0;
	}
	.ev.after {
		margin-right: 0;
		border-top-right-radius: 0;
		border-bottom-right-radius: 0;
	}
	.ev[data-drag-day] {
		cursor: grab;
	}
	.ev:global([data-dragging]) {
		opacity: 0.5;
	}
	.ev:hover {
		background: color-mix(in srgb, var(--c) 26%, transparent);
	}
	.more-btn {
		align-self: flex-start;
		padding: 0 6px;
		border: 0;
		border-radius: 4px;
		background: none;
		color: var(--muted);
		font: inherit;
		font-size: var(--text-sm);
		font-weight: 600;
		cursor: pointer;
	}
	.more-btn:hover {
		background: color-mix(in srgb, var(--text) 8%, transparent);
		color: var(--text);
	}
	.more-btn:focus-visible {
		outline: 2px solid var(--accent);
	}
	/* Titles are free text: in a narrow cell a long word may break anywhere. */
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
	/* A narrow month: smaller titles, the time stays in the day and the details. */
	@container (max-width: 760px) {
		.ev,
		.more-btn {
			font-size: var(--text-xs);
		}
		.stack .ev b {
			display: none;
		}
	}
	@container (max-width: 640px) {
		.mrow,
		.mweek.kw {
			grid-template-columns: 0 repeat(7, minmax(0, 1fr));
		}
		.kwh,
		.kwc {
			visibility: hidden;
			border: 0;
			padding: 0;
		}
		.ev {
			padding: 1px 3px 1px 4px;
		}
	}

	/* ---- Phone: tall days, bars without titles, "+N" ---- */
	.month.phone {
		grid-template-rows: auto;
		flex: none;
		container-type: normal;
	}
	.phone .mweek {
		grid-template-rows: 46px repeat(var(--lanes), 6px) minmax(14px, 1fr);
		row-gap: 4px;
		min-height: 92px;
		overflow: visible;
		padding-bottom: 0;
	}
	.phone .cell {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: space-between;
		padding: 6px 0 2px;
		border: 0;
		border-bottom: 1px solid var(--border);
		background: none;
		color: var(--text);
		font: inherit;
		cursor: pointer;
	}
	.phone .cell.blank {
		border-bottom-color: transparent;
	}
	.phone .cell.out {
		background: none;
	}
	.phone .n {
		display: inline-grid;
		place-items: center;
		width: 38px;
		height: 38px;
		border-radius: 50%;
		font-size: 19px;
		font-weight: 500;
		font-variant-numeric: tabular-nums;
	}
	.phone .we .n,
	.phone .out .n {
		color: var(--muted);
	}
	.phone .sel .n {
		background: var(--text);
		color: var(--bg);
		font-weight: 700;
	}
	.phone .today .n {
		background: var(--cal-today, #d70015);
		color: var(--cal-on-today, #fff);
		font-weight: 700;
	}
	.more {
		color: var(--muted);
		font-size: 11px;
		line-height: 14px;
		font-variant-numeric: tabular-nums;
	}
	.bar {
		height: 6px;
		margin: 0 4px;
		border-radius: 3px;
		background: var(--c);
		pointer-events: none;
		z-index: 1;
	}
	.bar.before {
		margin-left: 0;
		border-top-left-radius: 0;
		border-bottom-left-radius: 0;
	}
	.bar.after {
		margin-right: 0;
		border-top-right-radius: 0;
		border-bottom-right-radius: 0;
	}
	.bar.ring {
		background: none;
		border: 1.5px solid var(--muted);
	}
	.mname {
		grid-column: var(--col) / -1;
		margin: 0;
		padding: var(--space-4) 0 var(--space-1) var(--space-2);
		font-size: 22px;
		font-weight: 700;
		overflow-wrap: break-word;
	}
	.only {
		grid-template-columns: repeat(7, minmax(0, 1fr));
	}
	.only > .mweek {
		grid-column: 1 / -1;
	}
</style>
