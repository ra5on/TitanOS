<script lang="ts">
	// 1.15 (PLAN 115-5, drafts 36/37): the phone's months one after the
	// other, as Apple Kalender – scrolling runs from month to month, each
	// month's name stands over its first day. Months are added at both ends
	// while scrolling (the place kept: the height added above is scrolled
	// over); the month at the top is told to the page (`onvisible`), which
	// reads the events around it. The weekdays stay above.
	import { tick, untrack } from 'svelte';
	import { addMonths, daysBetween, monthGrid } from '../caldates.ts';
	import { weekdayShort } from '../calendar-text.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import type { Calendar, CalEvent, DayItem } from '../calendar.ts';
	import MonthView from './MonthView.svelte';
	import { motionOn } from './motion.ts';
	import type { DueTask } from './tasks.ts';

	type Props = {
		/** The day to show (its month at the top, its week in view). */
		target: string;
		/** Bumped to scroll to `target` again ("Heute", a month of the year). */
		jump?: number;
		today: string;
		byDay: Map<string, DayItem[]>;
		calFor: (id: string) => Calendar | undefined;
		selected?: string;
		tasks?: Map<string, DueTask[]>;
		onopen: (ev: CalEvent) => void;
		onday: (day: string, cell?: HTMLElement) => void;
		/** The first day of the month at the top now. */
		onvisible: (month: string) => void;
	};
	let {
		target,
		jump = 0,
		today,
		byDay,
		calFor,
		selected = '',
		tasks,
		onopen,
		onday,
		onvisible,
	}: Props = $props();

	const first = (d: string) => d.slice(0, 7) + '-01';
	const BEFORE = 4;
	const AFTER = 8;
	const MORE = 6;
	let from = $state(untrack(() => addMonths(first(target), -BEFORE)));
	let count = $state(BEFORE + 1 + AFTER);
	const months = $derived(Array.from({ length: count }, (_, i) => addMonths(from, i)));
	let scroller = $state<HTMLElement>();
	let growing = false;

	/** Scrolls so that the week of a day is in view below its month's name. */
	async function show(day: string, smooth = false) {
		const m = first(day);
		const at = months.indexOf(m);
		if (at < 0) {
			from = addMonths(m, -BEFORE);
			count = BEFORE + 1 + AFTER;
		}
		await tick();
		const el = scroller;
		const section = el?.querySelector<HTMLElement>(`[data-month="${m.slice(0, 7)}"]`);
		if (!el || !section) return;
		const weeks = monthGrid(day);
		const row = weeks.findIndex((w) => w.includes(day));
		const week = section.querySelectorAll<HTMLElement>('.mweek')[row];
		let top = section.offsetTop;
		// A week far down the month: keep it in view (with its month's name above when it fits).
		if (week && week.offsetTop + week.offsetHeight - section.offsetTop > el.clientHeight * 0.8)
			top = week.offsetTop - el.clientHeight * 0.3;
		el.scrollTo({ top: Math.max(0, top), behavior: smooth && motionOn() ? 'smooth' : 'instant' });
		seen();
	}

	let shownJump = -1;
	$effect(() => {
		const j = jump;
		const d = target;
		if (!scroller || j === shownJump) return;
		const smooth = shownJump >= 0;
		shownJump = j;
		untrack(() => void show(d, smooth));
	});

	/** The month whose name or weeks stand at the top. */
	function seen() {
		const el = scroller;
		if (!el) return;
		const line = el.scrollTop + 8;
		let top = months[0];
		for (const s of el.querySelectorAll<HTMLElement>('[data-month]'))
			if (s.offsetTop <= line) top = (s.dataset.month ?? '') + '-01';
		onvisible(top);
	}

	let frame = 0;
	function scrolled() {
		if (frame) return;
		frame = requestAnimationFrame(() => {
			frame = 0;
			seen();
			void grow();
		});
	}
	async function grow() {
		const el = scroller;
		if (!el || growing) return;
		// At most five years either way.
		if (el.scrollTop < 700 && daysBetween(addMonths(today, -60), from) > 0) {
			growing = true;
			const before = el.scrollHeight;
			from = addMonths(from, -MORE);
			count += MORE;
			await tick();
			el.scrollTop += el.scrollHeight - before;
			growing = false;
		} else if (
			el.scrollHeight - el.scrollTop - el.clientHeight < 900 &&
			daysBetween(addMonths(from, count), addMonths(today, 60)) > 0
		) {
			growing = true;
			count += MORE;
			await tick();
			growing = false;
		}
	}
</script>

<div class="mscroll">
	<div class="wdays" aria-hidden="true">
		{#each [0, 1, 2, 3, 4, 5, 6] as i (i)}<span class:we={i > 4}
				>{t('calendar.wdNarrow').split(',')[i] ?? weekdayShort(i)}</span
			>{/each}
	</div>
	<div class="months" bind:this={scroller} onscroll={scrolled} data-month-scroll>
		{#each months as m (m)}
			<MonthView
				month={m}
				{today}
				{byDay}
				{calFor}
				phone
				only
				{selected}
				{tasks}
				{onopen}
				{onday}
			/>
		{/each}
		<div class="room" aria-hidden="true"></div>
	</div>
</div>

<style>
	.mscroll {
		display: flex;
		flex-direction: column;
		flex: 1 1 auto;
		min-height: 0;
	}
	.wdays {
		flex: none;
		display: grid;
		grid-template-columns: repeat(7, minmax(0, 1fr));
		padding: 2px 0 6px;
		border-bottom: 1px solid var(--border);
		text-align: center;
		font-size: 11px;
		font-weight: 600;
	}
	.wdays .we {
		color: var(--muted);
	}
	.months {
		position: relative;
		flex: 1 1 auto;
		min-height: 0;
		overflow-y: auto;
		overscroll-behavior: contain;
		overflow-anchor: none;
	}
	/* Room for the floating buttons at the bottom. */
	.room {
		height: calc(76px + env(safe-area-inset-bottom, 0px));
	}
</style>
