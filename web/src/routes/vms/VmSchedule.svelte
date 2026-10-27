<script lang="ts">
	// The section "Schedule" of the VM form: times to start the VM and to
	// shut it down on chosen weekdays. It edits a draft owned by the form,
	// so the unsaved-changes guard of the form covers it.
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import {
		MAX_RULES,
		WEEK,
		newRule,
		timeoutChoices,
		type ScheduleDraft,
	} from '#lib/vm-schedule.ts';

	let { draft = $bindable() }: { draft: ScheduleDraft } = $props();

	function add() {
		draft.rules.push(newRule(draft.rules));
		draft.enabled = true;
	}
	function toggleDay(index: number, day: number, on: boolean) {
		const rule = draft.rules[index];
		rule.days = on ? [...new Set([...rule.days, day])] : rule.days.filter((d) => d !== day);
	}
	const minutes = (seconds: number) =>
		seconds % 60 === 0
			? t('vms.sched.minutes', { n: seconds / 60 })
			: t('vms.sched.seconds', { n: seconds });
	const hasStop = $derived(draft.rules.some((rule) => rule.action === 'stop'));
</script>

<div class="schedule">
	<label class="check"
		><input type="checkbox" bind:checked={draft.enabled} /><span>{t('vms.sched.enabled')}</span
		></label
	>
	<small class="muted">{t('vms.sched.hint')}</small>
	{#if draft.rules.length === 0}
		<p class="muted">{t('vms.sched.none')}</p>
	{/if}
	<ul class="rules">
		{#each draft.rules as rule, index (index)}
			<li class:off={!draft.enabled}>
				<select
					class="field"
					bind:value={rule.action}
					aria-label={t('vms.sched.action', { n: index + 1 })}
				>
					<option value="start">{t('vms.sched.start')}</option>
					<option value="stop">{t('vms.sched.stop')}</option>
				</select>
				<input
					class="field mono time"
					type="time"
					step="60"
					bind:value={rule.time}
					aria-label={t('vms.sched.time', { n: index + 1 })}
				/>
				<fieldset class="week">
					<legend class="sr-only">{t('vms.sched.days', { n: index + 1 })}</legend>
					{#each WEEK as day (day)}
						<label class="day"
							><input
								type="checkbox"
								checked={rule.days.includes(day)}
								onchange={(event) => toggleDay(index, day, event.currentTarget.checked)}
							/><span>{t(`backup.dayShort.${day}` as 'backup.dayShort.0')}</span></label
						>
					{/each}
				</fieldset>
				<button
					type="button"
					class="icon"
					aria-label={t('vms.sched.remove', { n: index + 1 })}
					title={t('vms.sched.remove', { n: index + 1 })}
					onclick={() => draft.rules.splice(index, 1)}><Icon name="close" size={14} /></button
				>
			</li>
		{/each}
	</ul>
	<div>
		<button type="button" class="btn" onclick={add} disabled={draft.rules.length >= MAX_RULES}
			><Icon name="plus" size={14} />{t('vms.sched.add')}</button
		>
	</div>
	{#if hasStop}
		<div class="stop">
			<label
				><span>{t('vms.sched.timeout')}</span>
				<select class="field" bind:value={draft.stopTimeout}>
					{#each timeoutChoices(draft.stopTimeout) as seconds (seconds)}
						<option value={seconds}>{minutes(seconds)}</option>
					{/each}
				</select>
				<small class="muted">{t('vms.sched.timeoutHint')}</small>
			</label>
			<label class="check"
				><input type="checkbox" bind:checked={draft.forceOff} /><span
					>{t('vms.sched.forceOff')}</span
				></label
			>
			{#if draft.forceOff}<small class="notice-warn">{t('vms.sched.forceOffHint')}</small>{/if}
		</div>
	{/if}
</div>

<style>
	.schedule {
		display: flex;
		flex-direction: column;
		gap: 10px;
		padding: 16px;
	}
	.check {
		display: flex;
		align-items: center;
		gap: 10px;
		font-size: 0.875rem;
	}
	.check input {
		width: 18px;
		height: 18px;
		margin: 0;
		flex: none;
	}
	.rules {
		display: flex;
		flex-direction: column;
		gap: 8px;
		margin: 0;
		padding: 0;
		list-style: none;
	}
	.rules li {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px;
	}
	.rules li.off {
		opacity: 0.6;
	}
	.rules select {
		width: auto;
		min-width: 10rem;
		flex: none;
	}
	.time {
		width: 7.5rem;
		flex: none;
	}
	.week {
		display: flex;
		flex-wrap: wrap;
		gap: 4px;
		margin: 0;
		padding: 0;
		border: 0;
		min-width: 0;
	}
	.day {
		display: inline-flex;
		align-items: center;
		gap: 3px;
		padding: 3px 7px;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		font-size: 0.8125rem;
		cursor: pointer;
	}
	.day:has(input:checked) {
		border-color: var(--accent);
		background: color-mix(in srgb, var(--accent) 14%, transparent);
	}
	.icon {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 2rem;
		height: 2rem;
		padding: 0;
		background: transparent;
		border: 0;
		color: var(--muted);
		cursor: pointer;
	}
	.stop {
		display: flex;
		flex-direction: column;
		gap: 8px;
		border-top: 1px solid var(--panel-border);
		padding-top: 10px;
	}
	.stop label:not(.check) {
		display: flex;
		flex-direction: column;
		gap: 4px;
		max-width: 22rem;
	}
</style>
