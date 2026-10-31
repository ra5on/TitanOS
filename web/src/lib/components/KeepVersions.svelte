<script lang="ts" module>
	/** How many versions stay and which run of a day, week or month (0.41.0). */
	export type Keep = {
		daily: number;
		weekly: number;
		monthly: number;
		mode?: '' | 'rolling';
		dailyAt?: string;
		weeklyAt?: string;
		/** 1 = Monday … 7 = Sunday; 0 = the newest of the week. */
		weeklyDay?: number;
		monthlyAt?: string;
		/** 1 … 28 or -1 for the last day. */
		monthlyDay?: number;
		/** 1 … 4 or -1 for the last, with monthlyWeekday 1 … 7. */
		monthlyNth?: number;
		monthlyWeekday?: number;
		whenFull?: '' | 'oldest';
		maxAgeMonths?: number;
	};

	/** What the backend gets: numbers as numbers, unused choices left out. */
	export function keepPayload(k: Keep): Keep {
		const rolling = k.mode === 'rolling';
		return {
			daily: rolling ? 0 : Number(k.daily) || 0,
			weekly: rolling ? 0 : Number(k.weekly) || 0,
			monthly: rolling ? 0 : Number(k.monthly) || 0,
			mode: rolling ? 'rolling' : '',
			dailyAt: rolling ? '' : (k.dailyAt ?? ''),
			weeklyAt: rolling ? '' : (k.weeklyAt ?? ''),
			weeklyDay: rolling ? 0 : Number(k.weeklyDay) || 0,
			monthlyAt: rolling ? '' : (k.monthlyAt ?? ''),
			monthlyDay: rolling ? 0 : Number(k.monthlyDay) || 0,
			monthlyNth: rolling ? 0 : Number(k.monthlyNth) || 0,
			monthlyWeekday: rolling ? 0 : Number(k.monthlyWeekday) || 0,
			whenFull: rolling ? '' : (k.whenFull ?? ''),
			maxAgeMonths: rolling ? Number(k.maxAgeMonths) || 0 : 0,
		};
	}
</script>

<script lang="ts">
	// Settings › Backups › job: which versions stay (decided 31.10.2026).
	// One line per step that reads as a sentence; below it what comes out.
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';

	let {
		keep = $bindable(),
		times,
		target = 'storage',
		simple = false,
	}: {
		keep: Keep;
		/** The run times of the schedule, to choose from; empty: any time. */
		times: string[];
		/** What "full" refers to: drive, volume, share or storage. */
		target?: 'drive' | 'volume' | 'share' | 'storage';
		/** Only the lines by day, week and month: no rolling mode, no "when full". */
		simple?: boolean;
	} = $props();

	const DAYS = [1, 2, 3, 4, 5, 6, 7];
	const NTH = [1, 2, 3, 4, -1];
	const day = (d: number) => t(`keep.day.${d}` as MessageKey);

	// The month: one list with fixed days and weekdays.
	const monthValue = $derived(
		keep.monthlyNth
			? `n:${keep.monthlyNth}:${keep.monthlyWeekday}`
			: keep.monthlyDay
				? `d:${keep.monthlyDay}`
				: '',
	);
	function setMonth(value: string) {
		const [kind, a, b] = value.split(':');
		keep.monthlyDay = kind === 'd' ? Number(a) : 0;
		keep.monthlyNth = kind === 'n' ? Number(a) : 0;
		keep.monthlyWeekday = kind === 'n' ? Number(b) : 0;
	}
	const monthText = $derived(
		keep.monthlyNth
			? t('keep.nthWeekday', {
					nth: t(`keep.nth.${keep.monthlyNth}` as MessageKey),
					day: day(keep.monthlyWeekday ?? 1),
				})
			: keep.monthlyDay === -1
				? t('keep.lastDay')
				: keep.monthlyDay
					? t('keep.dayOfMonth', { day: keep.monthlyDay })
					: t('keep.newest'),
	);
	const at = (value: string | undefined) => (value ? ` ${value}` : '');
	const total = $derived(
		(Number(keep.daily) || 0) + (Number(keep.weekly) || 0) + (Number(keep.monthly) || 0),
	);
	const result = $derived(
		[
			Number(keep.daily) > 0 &&
				`${keep.daily} × ${keep.dailyAt ? t('keep.dailyAt', { at: keep.dailyAt }) : t('keep.dailyNewest')}`,
			Number(keep.weekly) > 0 &&
				`${keep.weekly} × ${keep.weeklyDay ? day(keep.weeklyDay) : t('keep.weekNewest')}${at(keep.weeklyAt)}`,
			Number(keep.monthly) > 0 && `${keep.monthly} × ${monthText}${at(keep.monthlyAt)}`,
		]
			.filter(Boolean)
			.join(', '),
	);
</script>

{#snippet clock(value: string | undefined, set: (v: string) => void, label: string)}
	{#if times.length}
		<select
			class="field mono"
			aria-label={label}
			value={value ?? ''}
			onchange={(e) => set(e.currentTarget.value)}
		>
			<option value="">{t('keep.anyRun')}</option>
			{#each times as time (time)}<option value={time}>{time}</option>{/each}
		</select>
	{:else}
		<input
			class="field mono"
			type="time"
			aria-label={label}
			value={value ?? ''}
			onchange={(e) => set(e.currentTarget.value)}
		/>
	{/if}
{/snippet}

{#snippet tiers()}
	<p class="muted small">{t('keep.perLine')}</p>
	<div class="line">
		<strong>{t('keep.daily')}</strong>
		<span class="count"
			>{t('keep.keepLast')}
			<input class="field mono" type="number" min="0" max="999" bind:value={keep.daily} />
			{t('keep.days')}</span
		>
		<span class="which"
			>{t('keep.fromRunAt')}
			{@render clock(keep.dailyAt, (v) => (keep.dailyAt = v), t('keep.daily'))}</span
		>
	</div>
	<div class="line">
		<strong>{t('keep.weekly')}</strong>
		<span class="count"
			>{t('keep.keepLast')}
			<input class="field mono" type="number" min="0" max="999" bind:value={keep.weekly} />
			{t('keep.weeks')}</span
		>
		<span class="which"
			>{t('keep.fromRunOn')}
			<select class="field" aria-label={t('keep.weekly')} bind:value={keep.weeklyDay}>
				<option value={0}>{t('keep.newestOfWeek')}</option>
				{#each DAYS as d (d)}<option value={d}>{day(d)}</option>{/each}
			</select>
			{t('keep.at')}
			{@render clock(keep.weeklyAt, (v) => (keep.weeklyAt = v), t('keep.weekly'))}</span
		>
	</div>
	<div class="line">
		<strong>{t('keep.monthly')}</strong>
		<span class="count"
			>{t('keep.keepLast')}
			<input class="field mono" type="number" min="0" max="999" bind:value={keep.monthly} />
			{t('keep.months')}</span
		>
		<span class="which"
			>{t('keep.fromRunOn')}
			<select
				class="field"
				aria-label={t('keep.monthly')}
				value={monthValue}
				onchange={(e) => setMonth(e.currentTarget.value)}
			>
				<option value="">{t('keep.newestOfMonth')}</option>
				<optgroup label={t('keep.fixedDay')}>
					{#each Array.from({ length: 28 }, (_, i) => i + 1) as d (d)}<option value={`d:${d}`}
							>{t('keep.dayOfMonth', { day: d })}</option
						>{/each}
					<option value="d:-1">{t('keep.lastDay')}</option>
				</optgroup>
				<optgroup label={t('keep.weekday')}>
					{#each NTH as n (n)}{#each DAYS as d (d)}<option value={`n:${n}:${d}`}
								>{t('keep.nthWeekday', {
									nth: t(`keep.nth.${n}` as MessageKey),
									day: day(d),
								})}</option
							>{/each}{/each}
				</optgroup>
			</select>
			{t('keep.at')}
			{@render clock(keep.monthlyAt, (v) => (keep.monthlyAt = v), t('keep.monthly'))}</span
		>
	</div>
	<p class="muted small">
		{#if total > 0}{t('keep.result', { total, list: result })}{:else}{t('keep.all')}{/if}
		{t('keep.resultHint')}
	</p>
{/snippet}

{#if simple}
	<div class="mode simple">{@render tiers()}</div>
{:else}
	<div class="modes" role="radiogroup" aria-label={t('backup.keeps')}>
		<div class="mode" class:on={keep.mode !== 'rolling'}>
			<label class="head"
				><input
					type="radio"
					name="keep-mode"
					checked={keep.mode !== 'rolling'}
					onchange={() => (keep.mode = '')}
				/><strong>{t('keep.bySchedule')}</strong></label
			>
			<span class="muted small indent">{t('keep.byScheduleHint')}</span>
			{#if keep.mode !== 'rolling'}
				{@render tiers()}
				<label class="check full"
					><input
						type="checkbox"
						checked={keep.whenFull === 'oldest'}
						onchange={(e) => (keep.whenFull = e.currentTarget.checked ? 'oldest' : '')}
					/><span
						><strong>{t(`keep.whenFull.${target}` as MessageKey)}</strong><br /><span
							class="muted small">{t('keep.whenFullHint')}</span
						></span
					></label
				>
			{/if}
		</div>
		<div class="mode" class:on={keep.mode === 'rolling'}>
			<label class="head"
				><input
					type="radio"
					name="keep-mode"
					checked={keep.mode === 'rolling'}
					onchange={() => (keep.mode = 'rolling')}
				/><strong>{t(`keep.rolling.${target}` as MessageKey)}</strong></label
			>
			<span class="muted small indent">{t('keep.rollingHint')}</span>
			{#if keep.mode === 'rolling'}
				<label class="check indent"
					><input
						type="checkbox"
						checked={(keep.maxAgeMonths ?? 0) > 0}
						onchange={(e) => (keep.maxAgeMonths = e.currentTarget.checked ? 12 : 0)}
					/><span>{t('keep.maxAge')}</span>
					{#if (keep.maxAgeMonths ?? 0) > 0}<input
							class="field mono age"
							type="number"
							min="1"
							max="1200"
							bind:value={keep.maxAgeMonths}
						/>
						{t('keep.months')}{/if}</label
				>
				<p class="muted small indent">{t('keep.newestStays')}</p>
			{/if}
		</div>
	</div>
{/if}

<style>
	.modes {
		display: grid;
		gap: 10px;
	}
	.mode {
		display: grid;
		gap: 6px;
		border: 1px solid var(--border);
		border-radius: 10px;
		padding: 12px 14px;
	}
	.mode.on {
		border-color: var(--accent);
	}
	.head {
		justify-content: flex-start;
		display: flex;
		gap: 8px;
		align-items: center;
	}
	.indent {
		margin-left: 24px;
	}
	.small {
		font-size: var(--text-base);
	}
	p {
		margin: 0;
	}
	.line {
		display: grid;
		grid-template-columns: 6rem auto 1fr;
		gap: 12px;
		align-items: center;
		padding: 8px 0;
		border-top: 1px solid var(--border);
	}
	.count,
	.which {
		display: flex;
		align-items: center;
		gap: 8px;
		flex-wrap: wrap;
	}
	.count {
		white-space: nowrap;
		flex-wrap: nowrap;
	}
	.count input {
		width: 4rem;
		text-align: center;
	}
	.which select,
	.which input {
		width: auto;
	}
	.full {
		align-items: flex-start;
		padding-top: 10px;
		border-top: 1px solid var(--border);
	}
	.age {
		width: 4.5rem;
		text-align: center;
	}
	@media (max-width: 760px) {
		.line {
			grid-template-columns: 1fr;
			gap: 6px;
		}
	}
</style>
