<script lang="ts">
	// When a backup job runs: on or off, how often, on which days, at which
	// times, and optionally only inside a time window.
	import { t } from '#lib/i18n/index.svelte.ts';

	type Schedule = {
		enabled: boolean;
		every: 'hourly' | 'daily' | 'weekly' | 'monthly';
		interval?: number;
		weekdays?: number[];
		monthDay?: number;
		times?: string[];
		windowFrom?: string;
		windowTo?: string;
	};
	let { schedule = $bindable() }: { schedule: Schedule } = $props();

	const WEEKDAYS = [1, 2, 3, 4, 5, 6, 0];
	function toggleDay(day: number) {
		const days = schedule.weekdays ?? [];
		// At least one day stays.
		schedule.weekdays = days.includes(day)
			? days.length > 1
				? days.filter((d) => d !== day)
				: days
			: [...days, day];
	}
	const needsTimes = (every: string) => every !== 'hourly';
	const windowOn = $derived(!!schedule.windowFrom);
	function toggleWindow(on: boolean) {
		[schedule.windowFrom, schedule.windowTo] = on ? ['01:00', '06:00'] : ['', ''];
	}
</script>

<fieldset class="divided">
	<legend>{t('backup.schedule')}</legend>
	<label class="check"
		><input type="checkbox" bind:checked={schedule.enabled} /><span>{t('backup.enabled')}</span
		></label
	>
	{#if schedule.enabled}
		<div class="row">
			<label class="narrow"
				><span>{t('backup.every')}</span>
				<select class="field" bind:value={schedule.every}>
					<option value="hourly">{t('backup.every.hourly')}</option>
					<option value="daily">{t('backup.every.daily')}</option>
					<option value="weekly">{t('backup.every.weekly')}</option>
					<option value="monthly">{t('backup.every.monthly')}</option>
				</select>
			</label>
			{#if schedule.every === 'hourly'}
				<label class="narrow"
					><span>{t('backup.interval')}</span>
					<select class="field" bind:value={schedule.interval}>
						{#each [1, 2, 3, 4, 6, 12] as hours (hours)}<option value={hours}
								>{t('backup.intervalHours', { hours })}</option
							>{/each}
					</select>
				</label>
			{/if}
			{#if schedule.every === 'monthly'}
				<label class="tiny"
					><span>{t('backup.monthDay')}</span>
					<select class="field mono" bind:value={schedule.monthDay}>
						{#each Array.from({ length: 28 }, (_, i) => i + 1) as day (day)}<option value={day}
								>{t('backup.monthDayOption', { day })}</option
							>{/each}
					</select>
				</label>
			{/if}
		</div>
		{#if schedule.every === 'weekly'}
			<div class="days" role="group" aria-label={t('backup.weekdays')}>
				{#each WEEKDAYS as day (day)}
					<label class="check"
						><input
							type="checkbox"
							checked={schedule.weekdays?.includes(day)}
							onchange={() => toggleDay(day)}
						/><span>{t(`backup.dayShort.${day}` as 'backup.dayShort.0')}</span></label
					>
				{/each}
			</div>
		{/if}
		{#if needsTimes(schedule.every)}
			<div class="row">
				{#each schedule.times ?? [] as _, i (i)}
					<label class="tiny"
						><span>{i === 0 ? t('update.autoTime') : ''}&nbsp;</span>
						<span class="inline"
							><input class="field mono" type="time" bind:value={schedule.times![i]} required />
							{#if (schedule.times?.length ?? 0) > 1}<button
									type="button"
									class="btn small"
									aria-label={t('backup.removeTime')}
									onclick={() => (schedule.times = schedule.times!.filter((_, n) => n !== i))}
									>✕</button
								>{/if}
						</span>
					</label>
				{/each}
				{#if (schedule.times?.length ?? 0) < 8}<button
						type="button"
						class="btn small"
						onclick={() => (schedule.times = [...(schedule.times ?? []), '12:00'])}
						>{t('backup.addTime')}</button
					>{/if}
			</div>
		{:else}
			<label class="tiny"
				><span>{t('backup.minute')}</span><input
					class="field mono"
					type="time"
					bind:value={schedule.times![0]}
					required
				/></label
			>
		{/if}
		<label class="check"
			><input
				type="checkbox"
				checked={windowOn}
				onchange={(e) => toggleWindow(e.currentTarget.checked)}
			/><span>{t('backup.window')}</span></label
		>
		{#if windowOn}
			<div class="row">
				<label class="tiny"
					><span>{t('backup.windowFrom')}</span><input
						class="field mono"
						type="time"
						bind:value={schedule.windowFrom}
						required
					/></label
				>
				<label class="tiny"
					><span>{t('backup.windowTo')}</span><input
						class="field mono"
						type="time"
						bind:value={schedule.windowTo}
						required
					/></label
				>
			</div>
			<p class="muted">{t('backup.windowHint')}</p>
		{/if}
		<p class="muted">{t('backup.catchUp')}</p>
	{/if}
</fieldset>

<style>
	p {
		margin: 0;
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-3) 20px;
	}
	fieldset {
		gap: var(--space-3);
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: 0.8125rem;
	}
	label > span {
		color: var(--muted);
	}
	label.check {
		flex-direction: row;
		align-items: center;
		gap: var(--space-2);
		font-size: 0.875rem;
	}
	.check > span {
		color: var(--text);
	}
	.check input {
		width: 18px;
		height: 18px;
		margin: 0;
	}
	.narrow {
		flex: 0 1 260px;
		max-width: 320px;
	}
	.tiny {
		flex: 0 1 120px;
		max-width: 140px;
	}
	.small {
		font-size: 0.8125rem;
	}
	.days {
		display: flex;
		flex-wrap: wrap;
		gap: 0.25rem 1rem;
	}
	.inline {
		display: flex;
		align-items: center;
		gap: 0.4rem;
	}
</style>
