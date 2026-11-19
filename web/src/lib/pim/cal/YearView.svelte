<script lang="ts">
	// 1.15 (PLAN 115-5): the year behind "‹ 2026" of the month – twelve small
	// months, today in a red circle, this month's name in its color and
	// bold; a tap on a month shows it (the continuous months scroll there).
	// The years before and after are a swipe or the arrows away.
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { addMonths, monthGrid } from '../caldates.ts';
	import { monthName, monthTitle, yearText } from '../calendar-text.ts';
	import { swipe } from './motion.ts';

	type Props = {
		/** A day of the year shown. */
		year: string;
		today: string;
		onmonth: (day: string) => void;
		onyear: (day: string) => void;
	};
	let { year, today, onmonth, onyear }: Props = $props();

	const months = $derived(
		Array.from(
			{ length: 12 },
			(_, i) => `${year.slice(0, 4)}-${String(i + 1).padStart(2, '0')}-01`,
		),
	);
	const letters = $derived(t('calendar.wdNarrow').split(','));
</script>

<div
	class="year"
	data-year={year.slice(0, 4)}
	use:swipe={{ onswipe: (dir) => onyear(addMonths(year, 12 * dir)) }}
>
	<div class="yhead">
		<h2>{yearText(year)}</h2>
		<span class="ynav">
			<button
				type="button"
				class="ib"
				aria-label={t('calendar.prevYear')}
				title={t('calendar.prevYear')}
				onclick={() => onyear(addMonths(year, -12))}><Icon name="chevronLeft" size={18} /></button
			>
			<button
				type="button"
				class="ib"
				aria-label={t('calendar.nextYear')}
				title={t('calendar.nextYear')}
				onclick={() => onyear(addMonths(year, 12))}><Icon name="chevronRight" size={18} /></button
			>
		</span>
	</div>
	<div class="grid">
		{#each months as m (m)}
			{@const now = m.slice(0, 7) === today.slice(0, 7)}
			<button
				type="button"
				class="ym"
				class:now
				aria-label={monthTitle(m)}
				data-month={m.slice(0, 7)}
				onclick={() => onmonth(m)}
			>
				<b class="yn">{monthName(m)}</b>
				<span class="yg" aria-hidden="true">
					{#each letters as l, i (i)}<i class="yl">{l}</i>{/each}
					{#each monthGrid(m).flat() as d (d)}
						<i class="yd" class:out={d.slice(0, 7) !== m.slice(0, 7)} class:today={d === today}
							>{Number(d.slice(8))}</i
						>
					{/each}
				</span>
			</button>
		{/each}
	</div>
</div>

<style>
	.year {
		padding: 0 var(--space-3) var(--space-4);
	}
	.yhead {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2);
		padding: var(--space-2) 0;
	}
	.yhead h2 {
		margin: 0;
		font-size: 34px;
		font-weight: 700;
	}
	.ynav {
		display: flex;
		gap: var(--space-1);
	}
	.ib {
		display: inline-grid;
		place-items: center;
		min-width: 44px;
		min-height: 44px;
		border: 0;
		border-radius: var(--radius-ctl);
		background: none;
		color: var(--text);
		cursor: pointer;
	}
	.grid {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(min(100%, 9.5rem), 1fr));
		gap: var(--space-4) var(--space-3);
	}
	.ym {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		min-width: 0;
		padding: var(--space-1);
		border: 0;
		border-radius: var(--radius-ctl);
		background: none;
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.ym:hover {
		background: color-mix(in srgb, var(--text) 6%, transparent);
	}
	.ym:focus-visible {
		outline: 2px solid var(--accent);
	}
	.yn {
		font-size: var(--text-md);
		overflow-wrap: anywhere;
	}
	.ym.now .yn {
		color: var(--cal-today-text, #c00012);
	}
	.yg {
		display: grid;
		grid-template-columns: repeat(7, minmax(0, 1fr));
		row-gap: 2px;
		text-align: center;
		font-style: normal;
		font-size: 10px;
		line-height: 1.5;
		font-variant-numeric: tabular-nums;
	}
	.yg i {
		font-style: normal;
	}
	.yl {
		color: var(--muted);
		font-weight: 600;
	}
	.yd.out {
		visibility: hidden;
	}
	.yd.today {
		justify-self: center;
		min-width: 1.6em;
		border-radius: 0.8em;
		background: var(--cal-today, #d70015);
		color: var(--cal-on-today, #fff);
		font-weight: 700;
	}
</style>
