<script lang="ts">
	// A schedule without cron (owner request of 4 Nov 2026): one form with
	// every N minutes, every hour, daily, on chosen weekdays or monthly; below
	// it the schedule in words and the next three runs in the zone of the
	// NAS. The cron expression stays under "Advanced", for those who want it.
	// The value is always a cron expression (lib/schedule.ts).
	import { formatDateTime } from '#lib/format.ts';
	import { i18n, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import HelpTip from './HelpTip.svelte';
	import {
		MINUTE_STEPS,
		WEEK,
		describe,
		fromCron,
		nextRuns,
		parseCron,
		simpleOf,
		toCron,
		type ScheduleKind,
		type SimpleSchedule,
	} from '#lib/schedule.ts';

	type Props = { cron?: string; timezone?: string };
	let { cron = $bindable(''), timezone }: Props = $props();

	const kinds: ScheduleKind[] = ['minutes', 'hourly', 'daily', 'weekdays', 'monthly'];
	const monthDays = Array.from({ length: 28 }, (_, i) => i + 1);
	const id = $props.id();

	// A new schedule starts daily at 03:00; a stored one that fits the simple
	// choice is shown there, any other stays under "Advanced".
	const stored = fromCron(cron);
	const fresh = !stored && !cron.trim() ? simpleOf('daily') : null;
	let simple = $state<SimpleSchedule | null>(stored ?? fresh);
	let advanced = $state(!stored && !fresh);
	if (fresh) cron = toCron(fresh) ?? '';

	function set(next: SimpleSchedule) {
		simple = next;
		// Incomplete (no weekday chosen): no expression, nothing to save.
		cron = toCron(next) ?? '';
	}
	function typed(value: string) {
		cron = value;
		simple = fromCron(value);
	}
	function toggleDay(day: number) {
		if (simple?.kind !== 'weekdays') return;
		const current = simple.days;
		const days = current.includes(day)
			? current.filter((d) => d !== day)
			: WEEK.filter((d) => d === day || current.includes(d));
		set({ ...simple, days });
	}

	const tr = (key: string, values?: Record<string, string | number>) =>
		t(key as MessageKey, values);
	const join = (items: string[]) =>
		new Intl.ListFormat(i18n.locale, { type: 'conjunction' }).format(items);
	const valid = $derived(parseCron(cron) !== null);
	const noDays = $derived(simple?.kind === 'weekdays' && simple.days.length === 0);
	const sentence = $derived(valid && !noDays ? describe(cron, tr, join, 'say') : '');
	const zone = $derived(timezone || Intl.DateTimeFormat().resolvedOptions().timeZone);
	const next = $derived(valid && !noDays ? nextRuns(cron, 3, zone) : []);
</script>

<div class="schedule-field">
	<div class="simple">
		<label class="stack"
			>{t('schedule.kind')}<select
				class="field"
				value={simple?.kind ?? ''}
				onchange={(e) => set(simpleOf(e.currentTarget.value as ScheduleKind, simple))}
				>{#if !simple}<option value="" disabled>{t('schedule.kind.advanced')}</option
					>{/if}{#each kinds as kind (kind)}<option value={kind}
						>{t(`schedule.kind.${kind}` as MessageKey)}</option
					>{/each}</select
			></label
		>
		{#if simple?.kind === 'minutes'}<label class="stack"
				>{t('schedule.every')}<select
					class="field"
					value={simple.every}
					onchange={(e) => set({ kind: 'minutes', every: Number(e.currentTarget.value) })}
					>{#each MINUTE_STEPS as n (n)}<option value={n}>{t('schedule.minutes', { n })}</option
						>{/each}</select
				></label
			>{:else if simple?.kind === 'hourly'}<label class="stack"
				>{t('schedule.minute')}<input
					class="field"
					type="number"
					min="0"
					max="59"
					value={simple.minute}
					oninput={(e) => {
						const minute = e.currentTarget.valueAsNumber;
						if (Number.isInteger(minute) && minute >= 0 && minute < 60)
							set({ kind: 'hourly', minute });
					}}
				/></label
			>{/if}
		{#if simple?.kind === 'monthly'}<label class="stack"
				>{t('schedule.monthDay')}<select
					class="field"
					value={String(simple.day)}
					onchange={(e) => {
						const v = e.currentTarget.value;
						if (simple?.kind === 'monthly')
							set({ ...simple, day: v === 'last' ? 'last' : Number(v) });
					}}
					>{#each monthDays as day (day)}<option value={String(day)}>{day}.</option>{/each}<option
						value="last">{t('schedule.lastDay')}</option
					></select
				></label
			>{/if}
		{#if simple && 'time' in simple}<label class="stack"
				>{t('schedule.time')}<input
					class="field"
					type="time"
					required
					value={simple.time}
					oninput={(e) => {
						const time = e.currentTarget.value;
						if (simple && 'time' in simple && time) set({ ...simple, time });
					}}
				/></label
			>{/if}
	</div>
	{#if simple?.kind === 'weekdays'}<div class="days" role="group" aria-labelledby="{id}-days">
			<span id="{id}-days" class="label">{t('schedule.weekdays')}</span>
			<div class="toggles">
				{#each WEEK as day (day)}<button
						type="button"
						class="btn day"
						class:on={simple.days.includes(day)}
						aria-pressed={simple.days.includes(day)}
						aria-label={t(`schedule.dayName.${day}` as MessageKey)}
						onclick={() => toggleDay(day)}>{t(`schedule.dayShort.${day}` as MessageKey)}</button
					>{/each}
			</div>
		</div>{/if}

	{#if noDays}<p class="notice-warn">{t('schedule.noDays')}</p>
	{:else if !valid}<p class="notice-error" role="alert">{t('schedule.invalid')}</p>
	{:else}<div class="preview" aria-live="polite">
			<p class="sentence">{sentence}</p>
			{#if next.length}<p class="muted">{t('schedule.next', { zone })}</p>
				<ul>
					{#each next as at (at.getTime())}<li>{formatDateTime(at, zone)}</li>{/each}
				</ul>{:else}<p class="muted">{t('schedule.never')}</p>{/if}
		</div>{/if}

	<details bind:open={advanced}>
		<summary>{t('schedule.advanced')}</summary>
		<label class="stack cron"
			><span>{t('schedule.cron')}<HelpTip term="cron" /></span><input
				class="field mono"
				value={cron}
				oninput={(e) => typed(e.currentTarget.value)}
				autocomplete="off"
				spellcheck="false"
				placeholder={t('schedule.cronExample')}
			/></label
		>
		<p class="muted">{t('schedule.advancedHint')}</p>
	</details>
</div>

<style>
	.schedule-field {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}
	.schedule-field p {
		margin: 0;
	}
	.simple {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-3);
	}
	.simple label {
		flex: 1 1 10rem;
	}
	.days {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}
	.toggles {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1);
	}
	.day {
		min-width: 3rem;
	}
	.day.on {
		background: var(--accent);
		border-color: var(--accent);
		color: var(--on-accent);
	}
	.preview {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}
	.sentence {
		font-weight: 600;
	}
	.preview ul {
		margin: 0;
		padding-left: var(--space-5);
	}
	summary {
		cursor: pointer;
	}
	details[open] summary {
		margin-bottom: var(--space-2);
	}
	details p {
		margin-top: var(--space-2);
	}
</style>
