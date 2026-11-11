<script lang="ts">
	// The section "Schedule" of the VM form: times to start the VM and to
	// shut it down on chosen weekdays. It edits a draft owned by the form,
	// so the unsaved-changes guard of the form covers it.
	import FormRow from '#lib/components/appform/FormRow.svelte';
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

<!-- PLAN 78-5: the switch, the times and what a stop does are rows of the form. -->
<FormRow label={t('vms.sched.enabled')} id="vm-sched-enabled" hint={t('vms.sched.hint')}>
	<input
		id="vm-sched-enabled"
		class="switch"
		type="checkbox"
		role="switch"
		bind:checked={draft.enabled}
	/>
</FormRow>
<div class="srow plain">
	<div class="srow-block">
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
	</div>
</div>
{#if hasStop}
	<FormRow label={t('vms.sched.timeout')} id="vm-sched-timeout" hint={t('vms.sched.timeoutHint')}>
		<select id="vm-sched-timeout" class="field" bind:value={draft.stopTimeout}>
			{#each timeoutChoices(draft.stopTimeout) as seconds (seconds)}
				<option value={seconds}>{minutes(seconds)}</option>
			{/each}
		</select>
	</FormRow>
	<FormRow
		label={t('vms.sched.forceOff')}
		id="vm-sched-force"
		hint={draft.forceOff ? t('vms.sched.forceOffHint') : ''}
		tone="warn"
	>
		<input
			id="vm-sched-force"
			class="switch"
			type="checkbox"
			role="switch"
			bind:checked={draft.forceOff}
		/>
	</FormRow>
{/if}

<style>
	p {
		margin: 0;
	}
	.rules {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		margin: 0;
		padding: 0;
		list-style: none;
	}
	.rules li {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
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
		gap: var(--space-1);
		flex: 1 1 14rem;
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
		font-size: var(--text-base);
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
</style>
