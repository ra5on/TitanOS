<script lang="ts">
	// 1.11 (step 15): the list from today (draft "Liste", phone 40/41), also
	// the chosen day below the phone's month. Per day: time, a bar in the
	// calendar's color, the title, the calendar's **name**, the place, the
	// repetition in words and the bell for a reminder (with words for
	// screen readers).
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { addDays, clock } from '../caldates.ts';
	import { calColor, type Calendar, type CalEvent, type DayItem } from '../calendar.ts';
	import { calName, eventTitle, longDay, ruleText } from '../calendar-text.ts';

	type Props = {
		days: string[];
		today: string;
		byDay: Map<string, DayItem[]>;
		calFor: (id: string) => Calendar | undefined;
		phone?: boolean;
		onopen: (ev: CalEvent) => void;
	};
	let { days, today, byDay, calFor, phone = false, onopen }: Props = $props();

	const dayName = (d: string) =>
		d === today ? t('calendar.today') : d === addDays(today, 1) ? t('calendar.tomorrow') : '';
</script>

<div class="agenda" class:phone>
	{#each days as d (d)}
		{@const list = byDay.get(d) ?? []}
		<h3 class="ah" data-day={d}>
			{#if dayName(d)}<span class="tag" class:accent={d === today}>{dayName(d)}</span>{/if}<span
				>{longDay(d, today)}</span
			>
		</h3>
		{#each list as it (it.key)}
			{@const c = calFor(it.ev.calendar)}
			{@const rule = ruleText(it.ev)}
			<button
				type="button"
				class="arow"
				style:--c={calColor(c?.color ?? '')}
				onclick={() => onopen(it.ev)}
			>
				<span class="at">
					{#if it.part.allDay}
						<small>{t('calendar.allDay')}</small>
					{:else}
						<b>{it.part.first ? clock(it.part.from) : '00:00'}</b>
						<small>{it.part.last ? clock(it.part.to) : '24:00'}</small>
					{/if}
				</span>
				<span class="abar" aria-hidden="true"></span>
				<span class="ax">
					<b class="title">{eventTitle(it.ev)}</b>
					<small class="meta"
						><span class="cname">{calName(c)}</span>{#if it.ev.location}<span
								>{' · '}{it.ev.location}</span
							>{/if}{#if it.ev.recurring}<span class="rep"
								>{' · '}<Icon name="repeat" size={13} />{rule || t('calendar.repeats')}</span
							>{/if}</small
					>
				</span>
				{#if it.ev.alarms.length}<span class="al" title={t('calendar.withAlarm')}
						><Icon name="bell" size={16} /><span class="sr-only">{t('calendar.withAlarm')}</span
						></span
					>{/if}
			</button>
		{:else}
			<p class="muted none">{t('calendar.noEvents')}</p>
		{/each}
	{/each}
</div>

<style>
	.agenda {
		display: flex;
		flex-direction: column;
		gap: 3px;
		padding: var(--space-3);
	}
	.agenda.phone {
		padding: 0;
	}
	.ah {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: var(--space-1);
		margin: var(--space-3) 0 var(--space-1);
		font-size: var(--text-md);
		font-weight: 600;
	}
	.ah:first-child {
		margin-top: 0;
	}
	.tag {
		display: inline-block;
		padding: 0 6px;
		border-radius: var(--radius-small);
		background: var(--chip);
		color: var(--text);
		font-size: var(--text-xs);
		font-weight: 600;
		line-height: 1.6;
	}
	.tag.accent {
		background: var(--accent);
		color: var(--on-accent);
	}
	.arow {
		display: flex;
		align-items: stretch;
		gap: var(--space-2);
		width: 100%;
		min-height: 52px;
		padding: var(--space-2);
		border: 0;
		border-radius: var(--srow-radius, 6px);
		background: var(--srow-bg);
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.arow:hover {
		background: color-mix(in srgb, var(--text) 7%, var(--srow-bg));
	}
	.arow:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
	}
	.at {
		flex: none;
		width: 4.2rem;
		display: flex;
		flex-direction: column;
		font-variant-numeric: tabular-nums;
	}
	.at small,
	.ax small {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	.abar {
		flex: none;
		width: 4px;
		border-radius: 2px;
		background: var(--c);
	}
	.ax {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 2px;
		overflow-wrap: break-word;
		hyphens: auto;
	}
	.title {
		font-weight: 600;
		overflow-wrap: anywhere;
	}
	.cname {
		font-weight: 600;
		color: var(--text);
	}
	.rep :global(svg) {
		vertical-align: -2px;
		margin-right: 2px;
	}
	.al {
		flex: none;
		align-self: center;
		color: var(--muted);
	}
	.none {
		margin: 0 0 0 var(--space-2);
	}
</style>
