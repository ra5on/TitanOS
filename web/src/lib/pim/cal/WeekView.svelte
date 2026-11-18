<script lang="ts">
	// 1.11 (step 15): the week (drafts 03/04) and the day (the week with one
	// column). All-day and multi-day events on top, below the hours 00–24.
	// The time stands in every event, a series carries the repeat sign, a
	// reminder the bell (both with words for screen readers). Overlapping
	// events share the width. A line in the accent color marks now.
	//
	// 1.13 (Baustand steps 9/10): as high as the frame, the hours scroll on
	// their own. Every block is placed from its minutes alone – top and
	// height from start and end (at least 45 minutes high, as overlapLanes
	// counts it), so nothing lies on another and nothing is measured; a
	// title longer than its block scrolls inside it, shows whole while the
	// block has the focus and as its tooltip. The hours scroll to 07:00 (earlier
	// for an earlier event) when the view opens or shows other days, never
	// when the events are read again.
	// Changing (desktop, own calendars): a block is dragged to another time
	// or day, its lower edge to another end, both in steps of 15 minutes; a
	// drag over free space makes a new event of that time. The keys do the
	// same on a block: Alt+↑/↓ 15 minutes, Alt+←/→ a day, Alt+Shift+↑/↓ the
	// end; Enter keeps it, Escape leaves it. What is about to happen is said
	// (onannounce). In the all-day row an event is dragged to another day.
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { addDays, clock, daysBetween, overlapLanes } from '../caldates.ts';
	import { calColor, type Calendar, type CalEvent, type DayItem } from '../calendar.ts';
	import { calName, eventTitle, longDay, partTime, weekdayOf } from '../calendar-text.ts';
	import { dayDrag } from './day-drag.ts';
	import type { DueTask } from './tasks.ts';
	import TaskChip from './TaskChip.svelte';

	/** A new place of a timed event: its day and minutes (to may pass 1440). */
	export type TimedChange = { date: string; from: number; to: number };

	type Props = {
		days: string[];
		today: string;
		byDay: Map<string, DayItem[]>;
		calFor: (id: string) => Calendar | undefined;
		/** Minutes after midnight now (the line on today). */
		now: number;
		/** Bumped when the events of the days came (the first scroll waits for them). */
		loaded?: number;
		/** Whether an event may be changed here (own calendar, desktop). */
		canEdit?: (ev: CalEvent) => boolean;
		onopen: (ev: CalEvent) => void;
		/** A click or drag on free space: a new event at that time (to: its end). */
		onslot: (day: string, minutes: number, to?: number) => void;
		/** A timed event dragged or changed with the keys. */
		onchange?: (ev: CalEvent, change: TimedChange) => void;
		/** An event of the all-day row moved by whole days. */
		onshift?: (ev: CalEvent, days: number) => void;
		/** Says what is about to happen (a status line of the page). */
		onannounce?: (text: string) => void;
		tasks?: Map<string, DueTask[]>;
		ontask?: (task: DueTask) => void;
		ontaskdone?: (task: DueTask) => void;
	};
	let {
		days,
		today,
		byDay,
		calFor,
		now,
		loaded = 0,
		canEdit = () => false,
		onopen,
		onslot,
		onchange,
		onshift,
		onannounce,
		tasks,
		ontask,
		ontaskdone,
	}: Props = $props();

	const HOUR = 52;
	const STEP = 15;
	/** The shortest block (two lines: title and time), as overlapLanes counts it. */
	const SHORTEST = 45;
	const hours = Array.from({ length: 24 }, (_, i) => i);
	const items = (d: string) => byDay.get(d) ?? [];
	const top = (d: string) => items(d).filter((it) => it.long);
	const timedOf = $derived(
		new Map(
			days.map((d) => {
				const list = items(d).filter((it) => !it.long);
				const lanes = overlapLanes(
					list.map((it) => it.part),
					SHORTEST,
				);
				return [d, list.map((it, i) => ({ ...it, ...lanes[i] }))];
			}),
		),
	);
	const px = (minutes: number) => (minutes / 60) * HOUR;
	const snap = (minutes: number) => Math.round(minutes / STEP) * STEP;

	let scroller = $state<HTMLDivElement>();
	// Scroll to the morning when other days show; once more when their events came.
	let scrolledFor = '';
	let waitFor = -1;
	function toMorning() {
		if (!scroller) return;
		const starts = days.flatMap((d) => (timedOf.get(d) ?? []).map((it) => it.part.from));
		const first = Math.min(7 * 60, ...starts);
		scroller.scrollTop = Math.max(0, px(first) - 8);
	}
	$effect(() => {
		const key = days.join();
		if (!scroller || key === scrolledFor) return;
		scrolledFor = key;
		waitFor = loaded;
		toMorning();
	});
	$effect(() => {
		if (waitFor >= 0 && loaded !== waitFor) {
			waitFor = -1;
			toMorning();
		}
	});

	// ---- Dragging in the hours ----
	type Drag = {
		mode: 'move' | 'resize' | 'new';
		it?: DayItem;
		pointer: number;
		x0: number;
		y0: number;
		/** Minutes between the pointer and the start, for a move. */
		grab: number;
		/** Where a new event began (minutes). */
		anchor: number;
		cols: { day: string; left: number; right: number }[];
		top0: number;
		scroll0: number;
		moved: boolean;
		date: string;
		from: number;
		to: number;
	};
	let drag = $state<Drag | null>(null);
	let swallowClick = false;
	/** A change with the keys, waiting for Enter. */
	let pending = $state<{ it: DayItem; date: string; from: number; to: number } | null>(null);

	function minuteAt(d: Drag, y: number): number {
		const shift = (scroller?.scrollTop ?? 0) - d.scroll0;
		return ((y - d.top0 + shift) / HOUR) * 60;
	}
	function dayAt(d: Drag, x: number): string {
		const col = d.cols.find((c) => x >= c.left && x < c.right);
		if (col) return col.day;
		return x < d.cols[0].left ? d.cols[0].day : d.cols[d.cols.length - 1].day;
	}
	function begin(e: PointerEvent, mode: Drag['mode'], day: string, it?: DayItem) {
		if (e.button !== 0 || e.pointerType === 'touch' || !scroller) return;
		const grid = scroller.querySelector('.wk-grid');
		const cols = [...scroller.querySelectorAll<HTMLElement>('.col')].map((el) => {
			const r = el.getBoundingClientRect();
			return { day: el.dataset.day ?? '', left: r.left, right: r.right };
		});
		if (!grid || !cols.length) return;
		const d: Drag = {
			mode,
			it,
			pointer: e.pointerId,
			x0: e.clientX,
			y0: e.clientY,
			grab: 0,
			anchor: 0,
			cols,
			top0: grid.getBoundingClientRect().top,
			scroll0: scroller.scrollTop,
			moved: false,
			date: day,
			from: it?.part.from ?? 0,
			to: it?.part.to ?? 0,
		};
		if (mode === 'move' && it) d.grab = minuteAt(d, e.clientY) - it.part.from;
		// The edge follows the pointer by what it moved (a short block is drawn longer).
		if (mode === 'resize' && it) d.grab = minuteAt(d, e.clientY) - it.part.to;
		if (mode === 'new') {
			d.anchor = Math.max(
				0,
				Math.min(1440 - STEP, Math.floor(minuteAt(d, e.clientY) / STEP) * STEP),
			);
			d.from = d.anchor;
			d.to = d.anchor + STEP;
		}
		drag = d;
		(e.currentTarget as HTMLElement).setPointerCapture?.(e.pointerId);
	}
	function move(e: PointerEvent) {
		const d = drag;
		if (!d || e.pointerId !== d.pointer) return;
		if (!d.moved && Math.abs(e.clientX - d.x0) + Math.abs(e.clientY - d.y0) < 5) return;
		d.moved = true;
		// Near the edge of the hours they scroll along.
		const box = scroller?.getBoundingClientRect();
		if (scroller && box) {
			if (e.clientY < box.top + 28) scroller.scrollTop -= 14;
			else if (e.clientY > box.bottom - 28) scroller.scrollTop += 14;
		}
		const at = minuteAt(d, e.clientY);
		if (d.mode === 'move' && d.it) {
			const length = d.it.part.to - d.it.part.from;
			const from = Math.max(0, Math.min(1440 - STEP, snap(at - d.grab)));
			d.date = dayAt(d, e.clientX);
			d.from = from;
			d.to = from + length;
		} else if (d.mode === 'resize' && d.it) {
			d.to = Math.max(d.it.part.from + STEP, Math.min(1440, snap(at - d.grab)));
		} else if (d.mode === 'new') {
			const here = Math.max(0, Math.min(1440, snap(at)));
			d.from = Math.min(d.anchor, here);
			d.to = Math.max(d.anchor + STEP, here);
		}
		drag = { ...d };
	}
	function end(e: PointerEvent) {
		const d = drag;
		if (!d || e.pointerId !== d.pointer) return;
		drag = null;
		if (!d.moved) {
			if (d.mode === 'new') onslot(d.date, Math.floor(d.anchor / 30) * 30);
			return;
		}
		swallowClick = true;
		setTimeout(() => (swallowClick = false), 0);
		if (d.mode === 'new') onslot(d.date, d.from, d.to);
		else if (
			d.it &&
			(d.date !== d.it.part.date || d.from !== d.it.part.from || d.to !== d.it.part.to)
		)
			onchange?.(d.it.ev, { date: d.date, from: d.from, to: d.to });
	}
	function cancel() {
		drag = null;
	}

	function blockKey(e: KeyboardEvent, it: DayItem) {
		if (e.key === 'Escape' && pending) {
			e.preventDefault();
			pending = null;
			onannounce?.(t('calendar.drag.cancelled'));
			return;
		}
		if (e.key === 'Enter' && pending && pending.it.key === it.key) {
			e.preventDefault();
			const p = pending;
			pending = null;
			onchange?.(it.ev, { date: p.date, from: p.from, to: p.to });
			return;
		}
		if (!e.altKey || !canEdit(it.ev) || !onchange || !it.part.first || !it.part.last) return;
		if (!['ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight'].includes(e.key)) return;
		e.preventDefault();
		const p =
			pending && pending.it.key === it.key
				? { ...pending }
				: { it, date: it.part.date, from: it.part.from, to: it.part.to };
		const length = p.to - p.from;
		if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
			const next = addDays(p.date, e.key === 'ArrowLeft' ? -1 : 1);
			if (daysBetween(days[0], next) < 0 || daysBetween(next, days[days.length - 1]) < 0) return;
			p.date = next;
		} else if (e.shiftKey) {
			p.to = Math.max(p.from + STEP, Math.min(1440, p.to + (e.key === 'ArrowUp' ? -STEP : STEP)));
		} else {
			p.from = Math.max(0, Math.min(1440 - STEP, p.from + (e.key === 'ArrowUp' ? -STEP : STEP)));
			p.to = p.from + length;
		}
		pending = p;
		onannounce?.(
			t('calendar.drag.pending', {
				title: eventTitle(it.ev),
				when: `${longDay(p.date)}, ${clock(p.from)}–${clock(p.to)}`,
			}),
		);
	}

	/** Where the ghost of a drag or a pending key change stands. */
	const ghost = $derived.by(() => {
		if (drag?.moved) return { date: drag.date, from: drag.from, to: drag.to, it: drag.it };
		if (pending) return { date: pending.date, from: pending.from, to: pending.to, it: pending.it };
		return null;
	});
	const ghostKey = $derived(ghost?.it?.key ?? '');
</script>

<div class="week" style:--n={days.length} style:--hour="{HOUR}px">
	<div class="wk-head">
		<span></span>
		{#each days as d (d)}
			<span class="wh" class:today={d === today}
				><small>{weekdayOf(d)}</small><b>{Number(d.slice(8))}.</b>{#if d === today}<span class="tag"
						>{t('calendar.today')}</span
					>{/if}<span class="sr-only">{longDay(d, today)}</span></span
			>
		{/each}
	</div>
	<div
		class="wk-all"
		use:dayDrag={{
			onmove: (ev, n) => onshift?.(ev, n),
			enabled: !!onshift,
			eventOf: (key) => days.flatMap(top).find((it) => it.key === key)?.ev,
		}}
	>
		<span class="lab">{t('calendar.allDay')}</span>
		{#each days as d (d)}
			<span class="ad" data-day={d}>
				{#each top(d) as it (it.key)}
					{@const c = calFor(it.ev.calendar)}
					<button
						type="button"
						class="ev"
						style:--c={calColor(c?.color ?? '')}
						data-drag-day={canEdit(it.ev) && it.ev.allDay ? d : undefined}
						data-drag-key={it.key}
						onclick={() => onopen(it.ev)}
						>{#if !it.part.allDay}<b>{partTime(it.part)}</b>{/if}<span class="ev-t"
							>{eventTitle(it.ev)}</span
						><span class="sr-only">{calName(c)}</span></button
					>
				{/each}
				{#each tasks?.get(d) ?? [] as task (task.list + '/' + task.id)}
					<TaskChip {task} onopen={() => ontask?.(task)} ondone={() => ontaskdone?.(task)} />
				{/each}
			</span>
		{/each}
	</div>
	<div class="wk-scroll" bind:this={scroller}>
		<div class="wk-grid" class:dragging={!!drag?.moved}>
			<div class="hours" aria-hidden="true">
				{#each hours as h (h)}<span>{clock(h * 60)}</span>{/each}
			</div>
			{#each days as d (d)}
				<!-- svelte-ignore a11y_no_static_element_interactions -->
				<div
					class="col"
					class:today={d === today}
					data-day={d}
					onpointerdown={(e) => {
						const el = e.target as HTMLElement;
						if (el === e.currentTarget || el.classList.contains('line')) begin(e, 'new', d);
					}}
					onpointermove={move}
					onpointerup={end}
					onpointercancel={cancel}
				>
					{#each hours as h (h)}<i class="line" style:top="{h * HOUR}px"></i>{/each}
					{#if d === today}
						<span class="now" style:top="{px(now)}px"
							><span class="sr-only">{t('calendar.now', { time: clock(now) })}</span></span
						>
					{/if}
					{#each timedOf.get(d) ?? [] as it (it.key)}
						{@const c = calFor(it.ev.calendar)}
						{@const length = it.part.to - it.part.from}
						{@const edit = canEdit(it.ev) && !!onchange && it.part.first && it.part.last}
						<button
							type="button"
							class="blk"
							class:moving={ghostKey === it.key}
							class:editable={edit}
							class:short={length < 50}
							data-key={it.key}
							data-lane={it.lane}
							title={`${eventTitle(it.ev)} · ${partTime(it.part)}`}
							style:--c={calColor(c?.color ?? '')}
							style:top="{px(it.part.from)}px"
							style:--h="{px(Math.max(length, SHORTEST))}px"
							style:left="calc({(it.lane / it.lanes) * 100}% + 2px)"
							style:width="calc({100 / it.lanes}% - 4px)"
							aria-keyshortcuts={edit
								? 'Alt+ArrowUp Alt+ArrowDown Alt+ArrowLeft Alt+ArrowRight Alt+Shift+ArrowDown'
								: undefined}
							onpointerdown={(e) => edit && begin(e, 'move', d, it)}
							onpointermove={move}
							onpointerup={end}
							onpointercancel={cancel}
							onkeydown={(e) => blockKey(e, it)}
							onblur={() => {
								if (pending?.it.key === it.key) pending = null;
							}}
							onclick={() => {
								if (!swallowClick) onopen(it.ev);
							}}
						>
							<small class="tm"
								>{partTime(it.part)}{#if it.ev.recurring}<span
										class="sym"
										title={t('calendar.repeats')}
										><Icon name="repeat" size={12} /><span class="sr-only"
											>{t('calendar.repeats')}</span
										></span
									>{/if}{#if it.ev.alarms.length}<span class="sym" title={t('calendar.withAlarm')}
										><Icon name="bell" size={12} /><span class="sr-only"
											>{t('calendar.withAlarm')}</span
										></span
									>{/if}</small
							>
							<b>{eventTitle(it.ev)}</b>
							{#if it.ev.location && length >= 60}<small class="pl">{it.ev.location}</small>{/if}
							<span class="sr-only">{calName(c)}</span>
							{#if edit}
								<!-- svelte-ignore a11y_no_static_element_interactions -->
								<span
									class="rs"
									aria-hidden="true"
									title={t('calendar.drag.resize')}
									onpointerdown={(e) => {
										e.stopPropagation();
										begin(e, 'resize', d, it);
									}}
								></span>
							{/if}
						</button>
					{/each}
					{#if ghost && ghost.date === d}
						<span
							class="ghost"
							style:top="{px(ghost.from)}px"
							style:height="{px(Math.max(ghost.to - ghost.from, STEP))}px"
							style:--c={calColor(ghost.it ? (calFor(ghost.it.ev.calendar)?.color ?? '') : '')}
							aria-hidden="true"
							><b>{clock(ghost.from)}–{clock(ghost.to)}</b>{#if ghost.it}<span
									>{eventTitle(ghost.it.ev)}</span
								>{/if}</span
						>
					{/if}
				</div>
			{/each}
		</div>
	</div>
</div>

<style>
	.week {
		display: flex;
		flex-direction: column;
		flex: 1 1 auto;
		min-height: 0;
	}
	.wk-head,
	.wk-all,
	.wk-grid {
		display: grid;
		grid-template-columns: 3.6rem repeat(var(--n), minmax(0, 1fr));
	}
	.wk-head,
	.wk-all {
		flex: none;
		/* The hours' scroll bar has a lane; the heads keep the same columns. */
		overflow-y: auto;
		scrollbar-gutter: stable;
	}
	.wh {
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		gap: 2px 6px;
		padding: var(--space-2);
		border-left: 1px solid var(--srow-line);
		border-bottom: 1px solid var(--border);
	}
	.wh small {
		color: var(--muted);
	}
	.wh b {
		font-size: var(--text-lg);
	}
	.wh.today b {
		color: var(--accent);
	}
	.wk-head > span:first-child {
		border-bottom: 1px solid var(--border);
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
	.lab {
		padding: var(--space-1) 4px var(--space-1) 0;
		color: var(--muted);
		font-size: var(--text-xs);
		text-align: right;
		border-bottom: 1px solid var(--border);
		overflow-wrap: break-word;
		hyphens: auto;
	}
	.ad {
		position: relative;
		display: flex;
		flex-direction: column;
		gap: 2px;
		padding: 3px;
		min-height: 30px;
		max-height: 7.5rem;
		overflow-y: auto;
		min-width: 0;
		border-left: 1px solid var(--srow-line);
		border-bottom: 1px solid var(--border);
	}
	.ad:global([data-drop]) {
		background: color-mix(in srgb, var(--accent) 14%, transparent);
	}
	.ev {
		position: relative;
		display: flex;
		flex-wrap: wrap;
		flex: none;
		column-gap: 5px;
		width: 100%;
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
	.ev b {
		font-weight: 600;
	}
	.ev-t {
		min-width: 0;
		overflow-wrap: anywhere;
		hyphens: auto;
	}
	.wk-scroll {
		flex: 1 1 auto;
		min-height: 0;
		overflow-y: auto;
		overscroll-behavior: contain;
		scrollbar-gutter: stable;
	}
	.hours span {
		display: block;
		height: var(--hour);
		padding-right: var(--space-1);
		color: var(--muted);
		font-size: var(--text-xs);
		text-align: right;
		transform: translateY(-0.5em);
	}
	.hours span:first-child {
		transform: none;
	}
	.col {
		position: relative;
		height: calc(24 * var(--hour));
		border-left: 1px solid var(--srow-line);
	}
	.col.today {
		background: color-mix(in srgb, var(--accent) 5%, transparent);
	}
	.line {
		position: absolute;
		left: 0;
		right: 0;
		border-top: 1px solid var(--srow-line);
	}
	.now {
		position: absolute;
		left: 0;
		right: 0;
		z-index: 2;
		border-top: 2px solid var(--accent);
		pointer-events: none;
	}
	.now::before {
		content: '';
		position: absolute;
		left: -4px;
		top: -5px;
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: var(--accent);
	}
	.blk {
		position: absolute;
		z-index: 1;
		display: flex;
		flex-direction: column;
		align-items: stretch;
		gap: 1px;
		height: var(--h);
		margin: 0;
		padding: 2px 6px 3px 7px;
		border: 0;
		border-left: 3px solid var(--c);
		border-radius: 4px;
		background: color-mix(in srgb, var(--c) 16%, var(--surface));
		color: var(--text);
		font: inherit;
		font-size: var(--text-sm);
		line-height: 1.3;
		text-align: left;
		overflow-wrap: break-word;
		hyphens: auto;
		/* A title longer than its block scrolls in it (never cut) … */
		overflow-y: auto;
		overscroll-behavior: contain;
		scrollbar-width: thin;
		box-shadow: 0 0 0 1px var(--surface);
		cursor: pointer;
	}
	/* … and shows whole while focused (pointed at: its tooltip – the
	   resize edge does not move away under the pointer). */
	.blk:focus-visible {
		height: auto;
		min-height: var(--h);
		z-index: 3;
		overflow: visible;
	}
	/* Half an hour: the title first, its time after it in the same line. */
	.blk.short {
		flex-direction: row;
		flex-wrap: wrap;
		align-items: baseline;
		align-content: flex-start;
		column-gap: 6px;
		padding-top: 3px;
	}
	.blk.short b {
		order: -1;
	}
	.blk.editable {
		cursor: grab;
	}
	.dragging .blk {
		pointer-events: none;
	}
	.blk.moving {
		opacity: 0.45;
	}
	.blk small {
		color: var(--muted);
		font-size: var(--text-xs);
		font-variant-numeric: tabular-nums;
	}
	.blk .tm {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 0 4px;
	}
	.blk b {
		font-weight: 600;
		overflow-wrap: anywhere;
		hyphens: auto;
	}
	.pl {
		overflow-wrap: anywhere;
	}
	.sym {
		display: inline-flex;
	}
	.rs {
		position: absolute;
		left: 0;
		right: 0;
		bottom: 0;
		height: 7px;
		cursor: ns-resize;
	}
	.ghost {
		position: absolute;
		left: 2px;
		right: 2px;
		z-index: 4;
		display: flex;
		flex-direction: column;
		padding: 2px 6px;
		border: 2px dashed var(--c, var(--accent));
		border-radius: 4px;
		background: color-mix(in srgb, var(--c, var(--accent)) 22%, var(--surface));
		color: var(--text);
		font-size: var(--text-xs);
		pointer-events: none;
		overflow-wrap: anywhere;
	}
	.ev:focus-visible,
	.blk:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
		z-index: 3;
	}
</style>
