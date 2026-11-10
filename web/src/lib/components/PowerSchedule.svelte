<script lang="ts">
	// Shutting down and switching on at set times, and Wake-on-LAN: two rows
	// of Einstellungen › Energie (PLAN 78-1, draft 78).
	import { api } from '#lib/api.ts';
	import { formatDateTime } from '#lib/format.ts';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import SettingSub from '#lib/components/settings/SettingSub.svelte';

	type Time = { enabled: boolean; days: number[]; time: string };
	type Schedule = { off: Time; on: Time; wakeOnLan: boolean };
	type Iface = { name: string; mac: string; supported: boolean; enabled: boolean };
	type Status = {
		settings: Schedule;
		nextOff?: string;
		nextOn?: string;
		rtc: boolean;
		interfaces: Iface[];
		postponed?: string;
		readOnly: boolean;
		error?: string;
	};

	let status = $state<Status | null>(null);
	let draft = $state<Schedule | null>(null);
	let busy = $state(false);
	let error = $state('');
	let saved = $state(false);
	// Monday first, as calendars here show it; 0 is Sunday.
	const DAYS = [1, 2, 3, 4, 5, 6, 0];
	const fill = (v: Time | undefined, time: string): Time => ({
		enabled: v?.enabled ?? false,
		days: v?.days?.length ? v.days : [0, 1, 2, 3, 4, 5, 6],
		time: v?.time || time,
	});
	function take(res: Status) {
		status = res;
		draft = {
			off: fill(res.settings.off, '23:30'),
			on: fill(res.settings.on, '07:00'),
			wakeOnLan: res.settings.wakeOnLan,
		};
	}
	function fail(err: unknown) {
		error = apiErrorText(err, { detail: true });
	}
	$effect(() => {
		api.get<Status>('/system/power-schedule').then(take, fail);
	});
	// PLAN 78-1: no Save button – every change is stored right away (the same
	// PUT with the whole schedule as before).
	async function store() {
		if (!draft) return;
		busy = true;
		error = '';
		saved = false;
		try {
			take(await api.put<Status>('/system/power-schedule', draft));
			saved = true;
		} catch (err) {
			fail(err);
		} finally {
			busy = false;
		}
	}
	function toggleDay(entry: Time, day: number) {
		entry.days = entry.days.includes(day)
			? entry.days.filter((d) => d !== day)
			: [...entry.days, day];
		void store();
	}
	const anyWake = $derived(status?.interfaces.some((i) => i.supported) ?? false);
	// What the row says at its right: "Aus", or the times that are switched on.
	const summary = $derived.by(() => {
		if (!draft) return '';
		const parts = [
			draft.off.enabled ? t('powerSchedule.sumOff', { time: draft.off.time }) : '',
			draft.on.enabled ? t('powerSchedule.sumOn', { time: draft.on.time }) : '',
		].filter(Boolean);
		return parts.length ? parts.join(' · ') : t('appform.off');
	});
</script>

{#snippet times(entry: Time, which: 'off' | 'on', next: string | undefined)}
	<SettingSub
		title={t(`powerSchedule.${which}` as MessageKey)}
		for="schedule-{which}"
		desc={next && entry.enabled
			? t(`powerSchedule.next${which === 'off' ? 'Off' : 'On'}` as MessageKey, {
					at: formatDateTime(new Date(next)),
				})
			: undefined}
	>
		{#snippet control()}
			<input
				id="schedule-{which}"
				class="switch"
				type="checkbox"
				role="switch"
				bind:checked={entry.enabled}
				disabled={busy || (which === 'on' && !status?.rtc)}
				onchange={store}
			/>
		{/snippet}
	</SettingSub>
	<div class="srow-sub">
		<div class="srow-text">
			<label class="srow-title" for="schedule-{which}-time">{t('powerSchedule.time')}</label>
		</div>
		<div class="srow-ctl">
			<div class="days" role="group" aria-label={t('powerSchedule.days')}>
				{#each DAYS as day}
					<button
						type="button"
						class="btn day"
						class:active={entry.days.includes(day)}
						aria-pressed={entry.days.includes(day)}
						disabled={busy || !entry.enabled}
						onclick={() => toggleDay(entry, day)}
						>{t(`common.day${day}` as MessageKey).slice(0, 2)}</button
					>
				{/each}
			</div>
			<input
				id="schedule-{which}-time"
				class="field mono"
				type="time"
				bind:value={entry.time}
				disabled={busy || !entry.enabled}
				onchange={store}
			/>
		</div>
	</div>
{/snippet}

<SettingRow
	id="power-schedule"
	icon="clock"
	title={t('powerSchedule.schedule')}
	desc={t('powerSchedule.intro')}
	{summary}
>
	{#if error}<div class="srow-block"><p class="notice-error" role="alert">{error}</p></div>{/if}
	{#if status && draft}
		{#if status.readOnly || status.error || status.postponed}<div class="srow-block">
				{#if status.readOnly}<p class="notice-warn">{t('powerSchedule.readOnly')}</p>{/if}
				{#if status.error}<p class="notice-warn">
						{t('powerSchedule.problem')} <span class="mono">{status.error}</span>
					</p>{/if}
				{#if status.postponed}<p class="notice-warn">
						{t('powerSchedule.postponed', { why: status.postponed })}
					</p>{/if}
			</div>{/if}
		{@render times(draft.off, 'off', status.nextOff)}
		{@render times(draft.on, 'on', status.nextOn)}
		<div class="srow-block">
			<p class="muted small">{t(status.rtc ? 'powerSchedule.rtcHint' : 'powerSchedule.noRtc')}</p>
			{#if saved}<p class="muted small" role="status">{t('notify.saved')}</p>{/if}
		</div>
	{:else if !error}<div class="srow-block"><p class="muted">{t('common.loading')}</p></div>{/if}
</SettingRow>
<SettingRow
	id="power-wol"
	icon="globe"
	title={t('powerSchedule.wolTitle')}
	desc={t('powerSchedule.wolDesc')}
	summary={draft ? t(draft.wakeOnLan ? 'appform.on' : 'appform.off') : undefined}
>
	{#snippet control()}
		{#if draft}<input
				class="switch"
				type="checkbox"
				role="switch"
				aria-label={t('powerSchedule.wolTitle')}
				bind:checked={draft.wakeOnLan}
				disabled={busy || !anyWake}
				onchange={store}
			/>{/if}
	{/snippet}
	{#each status?.interfaces ?? [] as iface (iface.name)}
		<SettingSub title={iface.name} desc={iface.mac}>
			{#snippet control()}<span
					class="chip {iface.supported ? (iface.enabled ? 'ok' : 'info') : ''}"
					>{t(
						iface.supported
							? iface.enabled
								? 'powerSchedule.wolOn'
								: 'powerSchedule.wolOff'
							: 'powerSchedule.wolNone',
					)}</span
				>{/snippet}
		</SettingSub>
	{/each}
	<div class="srow-block"><p class="muted small">{t('powerSchedule.wolHint')}</p></div>
</SettingRow>

<style>
	.small {
		font-size: var(--text-base);
	}
	.days {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1);
	}
	.day {
		min-width: 2.5rem;
		padding-inline: 0;
	}
	.day.active {
		background: var(--accent);
		color: var(--on-accent);
		border-color: var(--accent);
	}
</style>
