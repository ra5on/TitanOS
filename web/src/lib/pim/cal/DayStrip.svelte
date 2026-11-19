<script lang="ts">
	// 1.15 (PLAN 115-5, drafts 38/39): the week strip over a day – the
	// weekdays and their numbers, the chosen day in a white circle, today's
	// number red (chosen and today: a red circle), as Apple Kalender. A tap
	// picks a day (the circle glides there), a swipe sideways shows the week
	// before or after (the same weekday). The words for screen readers say
	// the day and "Heute"; nothing is told by color alone.
	import { untrack } from 'svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { addDays, daysBetween, weekDays, weekStart } from '../caldates.ts';
	import { longDay } from '../calendar-text.ts';
	import { play, swipe } from './motion.ts';

	type Props = {
		day: string;
		today: string;
		onpick: (day: string) => void;
		/** Days with events (a small mark under the number). */
		busy?: (day: string) => boolean;
	};
	let { day, today, onpick, busy }: Props = $props();

	const days = $derived(weekDays(day));
	const col = $derived(days.indexOf(day));
	const letters = $derived(t('calendar.wdNarrow').split(','));
	let row = $state<HTMLElement>();
	let follow = $state(0);

	// Another week: its days come in from the side they were swiped from.
	let shownWeek = untrack(() => weekStart(day));
	$effect(() => {
		const w = weekStart(day);
		if (w === shownWeek) return;
		const dir = Math.sign(daysBetween(shownWeek, w));
		shownWeek = w;
		play(row, [{ transform: `translateX(${dir * 40}%)`, opacity: 0.2 }, { transform: 'none' }], {
			duration: 260,
		});
	});
</script>

<div
	class="strip"
	data-strip
	use:swipe={{
		onfollow: (dx) => (follow = dx),
		onswipe: (dir) => {
			follow = 0;
			onpick(addDays(day, 7 * dir));
		},
	}}
>
	<div class="letters" aria-hidden="true">
		{#each letters as l, i (i)}<span class:we={i > 4}>{l}</span>{/each}
	</div>
	<div
		class="days"
		bind:this={row}
		style:transform={follow ? `translateX(${follow}px)` : undefined}
	>
		<span class="sel" class:red={day === today} style:--col={col} aria-hidden="true"><i></i></span>
		{#each days as d, i (d)}
			<button
				type="button"
				class="d"
				class:we={i > 4}
				class:today={d === today}
				class:on={d === day}
				aria-pressed={d === day}
				aria-label={`${longDay(d, today)}${d === today ? ' · ' + t('calendar.today') : ''}`}
				onclick={() => onpick(d)}
				><span class="num">{Number(d.slice(8))}</span>{#if busy?.(d)}<span class="dot"
					></span>{/if}</button
			>
		{/each}
	</div>
</div>

<style>
	.strip {
		overflow: hidden;
		padding: 0 0 var(--space-1);
	}
	.letters,
	.days {
		display: grid;
		grid-template-columns: repeat(7, minmax(0, 1fr));
		text-align: center;
	}
	.letters span {
		padding-bottom: 2px;
		color: var(--text);
		font-size: 11px;
		font-weight: 600;
	}
	.letters span.we {
		color: var(--muted);
	}
	.days {
		position: relative;
		will-change: transform;
	}
	.d {
		position: relative;
		z-index: 1;
		display: flex;
		flex-direction: column;
		align-items: center;
		min-height: 48px;
		padding: 3px 0 0;
		border: 0;
		background: none;
		color: var(--text);
		font: inherit;
		cursor: pointer;
	}
	.num {
		display: grid;
		place-items: center;
		width: 38px;
		height: 38px;
		font-size: 19px;
		font-variant-numeric: tabular-nums;
	}
	.d.we .num {
		color: var(--muted);
	}
	.d.today .num {
		color: var(--cal-today-text, #c00012);
		font-weight: 600;
	}
	.d.on .num {
		color: var(--bg);
		font-weight: 700;
	}
	.d.on.today .num {
		color: var(--cal-on-today, #fff);
	}
	.dot {
		width: 4px;
		height: 4px;
		margin-top: 1px;
		border-radius: 50%;
		background: var(--muted);
	}
	.d:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
		border-radius: var(--radius-ctl);
	}
	/* The circle of the chosen day: one element that glides from day to day. */
	.sel {
		position: absolute;
		left: 0;
		top: 3px;
		width: calc(100% / 7);
		height: 38px;
		display: flex;
		justify-content: center;
		transform: translateX(calc(var(--col) * 100%));
		pointer-events: none;
	}
	.sel i {
		width: 38px;
		height: 38px;
		border-radius: 50%;
		background: var(--text);
	}
	.sel.red i {
		background: var(--cal-today, #d70015);
	}
	:global(:root[data-fx-transitions='on']) .sel {
		transition: transform 280ms cubic-bezier(0.2, 0.8, 0.2, 1);
	}
	:global(:root[data-fx-transitions='on']) .sel i {
		transition: background-color 200ms;
	}
	@media (prefers-reduced-motion: reduce) {
		:global(:root[data-fx-transitions='on']) .sel,
		:global(:root[data-fx-transitions='on']) .sel i {
			transition: none;
		}
	}
</style>
