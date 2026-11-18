<script lang="ts">
	// 1.11 (step 15): the week (drafts 03/04) and the day (the week with one
	// column, not drawn). All-day and multi-day events on top, below the
	// hours 00–24 in a box that starts at 07:00. The time stands in every
	// event, a series carries the repeat sign, a reminder the bell (both
	// with words for screen readers). An event with a long title grows
	// instead of cutting it; what it would cover in its lane moves down.
	// Overlapping events share the width. A line in the accent color marks
	// the current time.
	import { tick } from 'svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { clock, overlapLanes } from '../caldates.ts';
	import { calColor, type Calendar, type CalEvent, type DayItem } from '../calendar.ts';
	import { calName, eventTitle, longDay, partTime, weekdayOf } from '../calendar-text.ts';

	type Props = {
		days: string[];
		today: string;
		byDay: Map<string, DayItem[]>;
		calFor: (id: string) => Calendar | undefined;
		/** Minutes after midnight now (the line on today). */
		now: number;
		onopen: (ev: CalEvent) => void;
		/** A click on a free place of a day: a new event at that time. */
		onslot: (day: string, minutes: number) => void;
	};
	let { days, today, byDay, calFor, now, onopen, onslot }: Props = $props();

	const HOUR = 52;
	const hours = Array.from({ length: 24 }, (_, i) => i);
	const items = (d: string) => byDay.get(d) ?? [];
	const top = (d: string) => items(d).filter((it) => it.long);
	function timed(d: string) {
		const list = items(d).filter((it) => !it.long);
		const lanes = overlapLanes(list.map((it) => it.part));
		return list.map((it, i) => ({ ...it, ...lanes[i] }));
	}

	let scroller = $state<HTMLDivElement>();
	let grid = $state<HTMLDivElement>();

	// Start at 07:00, earlier when an event of the shown days begins earlier.
	$effect(() => {
		if (!scroller) return;
		const starts = days.flatMap((d) => timed(d).map((it) => it.part.from));
		const first = Math.min(7 * 60, ...starts);
		scroller.scrollTop = Math.max(0, (first / 60) * HOUR - 8);
	});

	// A grown block may reach into the next one of its lane: that one moves
	// down below it, so no text lies under another block.
	function relayout() {
		if (!grid) return;
		for (const col of grid.querySelectorAll<HTMLElement>('.col')) {
			const blocks = [...col.querySelectorAll<HTMLElement>('.blk')];
			const lanes = new Map<string, HTMLElement[]>();
			for (const b of blocks) {
				b.style.top = b.dataset.top + 'px';
				const k = b.dataset.lane ?? '0';
				lanes.set(k, [...(lanes.get(k) ?? []), b]);
			}
			let bottom = 0;
			for (const list of lanes.values()) {
				list.sort((a, b) => Number(a.dataset.top) - Number(b.dataset.top));
				let end = -Infinity;
				for (const b of list) {
					const want = Math.max(Number(b.dataset.top), end + 2);
					b.style.top = want + 'px';
					end = want + b.offsetHeight;
					bottom = Math.max(bottom, end);
				}
			}
			col.style.minHeight = Math.max(24 * HOUR, bottom + 4) + 'px';
		}
	}
	$effect(() => {
		void days;
		void byDay;
		void tick().then(relayout);
	});
	$effect(() => {
		if (!grid || typeof ResizeObserver === 'undefined') return;
		const ro = new ResizeObserver(() => relayout());
		ro.observe(grid);
		return () => ro.disconnect();
	});

	function slot(e: MouseEvent, d: string) {
		if (e.target !== e.currentTarget) return;
		const y = e.offsetY;
		onslot(d, Math.max(0, Math.min(23 * 60 + 30, Math.floor(y / (HOUR / 2)) * 30)));
	}
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
	<div class="wk-all">
		<span class="lab">{t('calendar.allDay')}</span>
		{#each days as d (d)}
			<span class="ad">
				{#each top(d) as it (it.key)}
					{@const c = calFor(it.ev.calendar)}
					<button
						type="button"
						class="ev"
						style:--c={calColor(c?.color ?? '')}
						onclick={() => onopen(it.ev)}
						>{#if !it.part.allDay}<b>{partTime(it.part)}</b>{/if}<span class="ev-t"
							>{eventTitle(it.ev)}</span
						><span class="sr-only">{calName(c)}</span></button
					>
				{/each}
			</span>
		{/each}
	</div>
	<div class="wk-scroll" bind:this={scroller}>
		<div class="wk-grid" bind:this={grid}>
			<div class="hours" aria-hidden="true">
				{#each hours as h (h)}<span>{clock(h * 60)}</span>{/each}
			</div>
			{#each days as d (d)}
				<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
				<div class="col" class:today={d === today} data-day={d} onclick={(e) => slot(e, d)}>
					{#each hours as h (h)}<i class="line" style:top="{h * HOUR}px"></i>{/each}
					{#if d === today}
						<span class="now" style:top="{(now / 60) * HOUR}px"
							><span class="sr-only">{t('calendar.now', { time: clock(now) })}</span></span
						>
					{/if}
					{#each timed(d) as it (it.key)}
						{@const c = calFor(it.ev.calendar)}
						{@const length = ((it.part.to - it.part.from) / 60) * HOUR}
						<button
							type="button"
							class="blk"
							style:--c={calColor(c?.color ?? '')}
							style:top="{(it.part.from / 60) * HOUR}px"
							style:min-height="{Math.max(length, 22)}px"
							style:left="calc({(it.lane / it.lanes) * 100}% + 3px)"
							style:width="calc({100 / it.lanes}% - 6px)"
							data-top={(it.part.from / 60) * HOUR}
							data-lane={it.lane}
							onclick={() => onopen(it.ev)}
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
							{#if it.ev.location && length >= HOUR}<small class="pl">{it.ev.location}</small>{/if}
							<span class="sr-only">{calName(c)}</span>
						</button>
					{/each}
				</div>
			{/each}
		</div>
	</div>
</div>

<style>
	.week {
		display: flex;
		flex-direction: column;
		min-height: 0;
	}
	.wk-head,
	.wk-all,
	.wk-grid {
		display: grid;
		grid-template-columns: 3.6rem repeat(var(--n), minmax(0, 1fr));
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
		display: flex;
		flex-direction: column;
		gap: 2px;
		padding: 3px;
		min-height: 30px;
		min-width: 0;
		border-left: 1px solid var(--srow-line);
		border-bottom: 1px solid var(--border);
	}
	.ev {
		display: flex;
		flex-wrap: wrap;
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
	.ev b {
		font-weight: 600;
	}
	.ev-t {
		min-width: 0;
		overflow-wrap: anywhere;
		hyphens: auto;
	}
	.wk-scroll {
		max-height: max(420px, calc(100vh - 230px));
		overflow-y: auto;
		overscroll-behavior: contain;
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
		min-height: calc(24 * var(--hour));
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
		pointer-events: none;
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
		margin: 0;
		padding: 3px 6px 4px 7px;
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
		box-shadow: 0 0 0 1px var(--surface);
		cursor: pointer;
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
	.ev:focus-visible,
	.blk:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
		z-index: 3;
	}
</style>
