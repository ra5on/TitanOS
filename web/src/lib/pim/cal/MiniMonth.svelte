<script lang="ts">
	// 1.11 (step 15): the small month in the column of T-Kalender (draft 01):
	// ISO weeks on the left, today framed, the shown week or day tinted, a
	// dot under days with events. A day opens it in the view.
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { addMonths, isoWeek, monthGrid } from '../caldates.ts';
	import { longDay, monthTitle, weekdayShort } from '../calendar-text.ts';

	type Props = {
		/** A day of the month shown. */
		month: string;
		today: string;
		/** Days shown in the view (tinted). */
		selected: Set<string>;
		busy: (day: string) => boolean;
		onpick: (day: string) => void;
		onmonth: (day: string) => void;
	};
	let { month, today, selected, busy, onpick, onmonth }: Props = $props();

	const weeks = $derived(monthGrid(month));
	const inMonth = (d: string) => d.slice(0, 7) === month.slice(0, 7);
</script>

<div class="mini">
	<div class="mini-head">
		<b>{monthTitle(month)}</b>
		<span class="mini-nav">
			<button
				type="button"
				class="ib"
				aria-label={t('calendar.prevMonth')}
				onclick={() => onmonth(addMonths(month, -1))}><Icon name="chevronLeft" size={16} /></button
			>
			<button
				type="button"
				class="ib"
				aria-label={t('calendar.nextMonth')}
				onclick={() => onmonth(addMonths(month, 1))}><Icon name="chevronRight" size={16} /></button
			>
		</span>
	</div>
	<div class="mini-grid" role="grid" aria-label={monthTitle(month)}>
		<div class="row" role="row">
			<span class="kw" role="columnheader">{t('calendar.kw')}</span>
			{#each [0, 1, 2, 3, 4, 5, 6] as i (i)}<span class="wd" role="columnheader"
					>{weekdayShort(i)}</span
				>{/each}
		</div>
		{#each weeks as w (w[0])}
			<div class="row" role="row">
				<span class="kw" role="rowheader">{isoWeek(w[0])}</span>
				{#each w as d (d)}
					<span role="gridcell" class="cellwrap">
						<button
							type="button"
							class="md"
							class:out={!inMonth(d)}
							class:today={d === today}
							class:sel={selected.has(d)}
							class:has={busy(d)}
							aria-label={longDay(d, today) + (d === today ? ` · ${t('calendar.today')}` : '')}
							aria-current={d === today ? 'date' : undefined}
							onclick={() => onpick(d)}>{Number(d.slice(8))}</button
						>
					</span>
				{/each}
			</div>
		{/each}
	</div>
</div>

<style>
	.mini-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		flex-wrap: wrap;
		gap: var(--space-2);
		margin-bottom: var(--space-2);
	}
	.mini-head b {
		overflow-wrap: break-word;
	}
	.mini-nav {
		display: flex;
		gap: var(--space-1);
	}
	.ib {
		display: inline-grid;
		place-items: center;
		min-width: 34px;
		min-height: 34px;
		padding: 0;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		background: none;
		color: var(--text);
		cursor: pointer;
	}
	.ib:focus-visible,
	.md:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
	}
	.mini-grid {
		display: flex;
		flex-direction: column;
		gap: 2px;
		font-size: var(--text-sm);
	}
	.row {
		display: grid;
		grid-template-columns: 1.6rem repeat(7, minmax(0, 1fr));
		gap: 2px;
		text-align: center;
	}
	.wd,
	.kw {
		color: var(--muted);
		font-size: var(--text-xs);
		line-height: 1.9rem;
	}
	.cellwrap {
		display: block;
	}
	.md {
		position: relative;
		width: 100%;
		padding: 0;
		border: 0;
		background: none;
		color: var(--text);
		font: inherit;
		line-height: 1.9rem;
		border-radius: var(--radius-small);
		cursor: pointer;
	}
	.md:hover {
		background: color-mix(in srgb, var(--text) 7%, transparent);
	}
	.md.out {
		color: var(--muted);
	}
	.md.sel {
		background: var(--srow-bg);
	}
	.md.has::after {
		content: '';
		position: absolute;
		left: 50%;
		bottom: 3px;
		width: 3px;
		height: 3px;
		margin-left: -1.5px;
		border-radius: 50%;
		background: var(--muted);
	}
	.md.today {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
		font-weight: 700;
	}
</style>
