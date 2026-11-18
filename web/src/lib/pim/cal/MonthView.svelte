<script lang="ts">
	// 1.11 (step 15): the month (drafts 01/02, phone 42). Monday first with
	// ISO weeks; today framed and named "Heute"; up to three events per day,
	// then "+N weitere" (opens the day). Titles wrap, never cut (a long word
	// may break anywhere). On the phone no titles in the cells, one dash per
	// event in its calendar's color; the chosen day's events follow below.
	import { t } from '#lib/i18n/index.svelte.ts';
	import { clock, isoWeek, monthGrid } from '../caldates.ts';
	import { calColor, type Calendar, type CalEvent, type DayItem } from '../calendar.ts';
	import { calName, eventTitle, longDay, monthTitle, weekdayShort } from '../calendar-text.ts';

	type Props = {
		month: string;
		today: string;
		byDay: Map<string, DayItem[]>;
		calFor: (id: string) => Calendar | undefined;
		phone?: boolean;
		/** The chosen day (phone). */
		selected?: string;
		onopen: (ev: CalEvent) => void;
		onday: (day: string) => void;
	};
	let {
		month,
		today,
		byDay,
		calFor,
		phone = false,
		selected = '',
		onopen,
		onday,
	}: Props = $props();

	const MAX = 3;
	const weeks = $derived(monthGrid(month));
	const inMonth = (d: string) => d.slice(0, 7) === month.slice(0, 7);
	const items = (d: string) => byDay.get(d) ?? [];
</script>

<div class="month" class:phone role="grid" aria-label={monthTitle(month)}>
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
				{#if phone}
					<div class="cell" class:out={!inMonth(d)} class:today={d === today} role="gridcell">
						<button
							type="button"
							class="pick"
							class:sel={d === selected}
							aria-pressed={d === selected}
							aria-label={`${longDay(d, today)}${d === today ? ' · ' + t('calendar.today') : ''} · ${t('calendar.count', { n: list.length })}`}
							onclick={() => onday(d)}
						>
							<span class="n">{Number(d.slice(8))}</span>
							{#if list.length}
								<span class="marks" aria-hidden="true"
									>{#each list.slice(0, 4) as it (it.key)}<i
											style:--c={calColor(calFor(it.ev.calendar)?.color ?? '')}
										></i>{/each}{#if list.length > 4}<small>+{list.length - 4}</small>{/if}</span
								>
							{/if}
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
						{#each list.slice(0, MAX) as it (it.key)}
							{@const c = calFor(it.ev.calendar)}
							<button
								type="button"
								class="ev"
								style:--c={calColor(c?.color ?? '')}
								onclick={() => onopen(it.ev)}
								>{#if !it.part.allDay && it.part.first}<b>{clock(it.part.from)}</b>{/if}<span
									class="ev-t">{eventTitle(it.ev)}</span
								><span class="sr-only">{calName(c)}</span></button
							>
						{/each}
						{#if list.length > MAX}
							<button type="button" class="more" onclick={() => onday(d)}
								>{t('calendar.more', { n: list.length - MAX })}</button
							>
						{/if}
					</div>
				{/if}
			{/each}
		</div>
	{/each}
</div>

<style>
	.month {
		display: flex;
		flex-direction: column;
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
		border-bottom: 1px solid var(--border);
	}
	.phone .mh {
		padding: var(--space-1) var(--space-2);
	}
	.month.phone {
		margin-bottom: var(--space-4);
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
		min-height: 88px;
		display: flex;
		flex-direction: column;
		gap: 2px;
		padding: var(--space-1);
		border-left: 1px solid var(--srow-line);
		border-bottom: 1px solid var(--srow-line);
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
	.ev {
		display: flex;
		flex-wrap: wrap;
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
	.more {
		align-self: flex-start;
		border: 0;
		background: none;
		padding: 2px 6px;
		color: var(--muted);
		font: inherit;
		font-size: var(--text-sm);
		font-weight: 600;
		cursor: pointer;
	}
	.ev:focus-visible,
	.more:focus-visible,
	.n:focus-visible,
	.pick:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
	}

	/* Phone: no titles, a dash per event. */
	.phone .cell {
		min-height: 0;
		padding: 2px;
	}
	.phone .cell.today {
		box-shadow: none;
	}
	.pick {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 3px;
		width: 100%;
		min-height: 52px;
		padding: 4px 0;
		border: 0;
		border-radius: var(--radius-ctl);
		background: none;
		color: var(--text);
		font: inherit;
		font-size: var(--text-sm);
	}
	.cell:not(.today) .pick.sel {
		background: color-mix(in srgb, var(--accent) 16%, transparent);
	}
	.phone .today .n {
		display: inline-grid;
		place-items: center;
		min-width: 24px;
		height: 24px;
		border-radius: 12px;
		background: var(--accent);
		color: var(--on-accent);
	}
	.marks {
		display: flex;
		flex-wrap: wrap;
		justify-content: center;
		gap: 2px;
	}
	.marks i {
		width: 10px;
		height: 4px;
		border-radius: 2px;
		background: var(--c);
	}
	.marks small {
		color: var(--muted);
		font-size: var(--text-xs);
		line-height: 1;
	}
</style>
