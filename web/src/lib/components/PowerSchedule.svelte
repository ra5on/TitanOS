<script lang="ts">
	// Shutting down and switching on at set times, and Wake-on-LAN.
	import { api } from '#lib/api.ts';
	import { formatDateTime } from '#lib/format.ts';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';

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
	async function save(event: SubmitEvent) {
		event.preventDefault();
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
	}
	const anyWake = $derived(status?.interfaces.some((i) => i.supported) ?? false);
</script>

{#snippet times(entry: Time, which: 'off' | 'on')}
	<label class="check"
		><input
			type="checkbox"
			bind:checked={entry.enabled}
			disabled={busy || (which === 'on' && !status?.rtc)}
		/><span>{t(`powerSchedule.${which}` as MessageKey)}</span></label
	>
	<div class="row">
		<label class="tiny"
			><span>{t('powerSchedule.time')}</span><input
				class="field mono"
				type="time"
				bind:value={entry.time}
				disabled={busy || !entry.enabled}
			/></label
		>
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
	</div>
{/snippet}

<section class="tile panel schedule" id="power-schedule">
	<h2 class="tile-title">{t('powerSchedule.title')}</h2>
	<p class="muted">{t('powerSchedule.intro')}</p>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if status && draft}
		{#if status.readOnly}<p class="notice-warn">{t('powerSchedule.readOnly')}</p>{/if}
		{#if status.error}<p class="notice-warn">
				{t('powerSchedule.problem')} <span class="mono">{status.error}</span>
			</p>{/if}
		{#if status.postponed}<p class="notice-warn">
				{t('powerSchedule.postponed', { why: status.postponed })}
			</p>{/if}
		<form onsubmit={save}>
			{@render times(draft.off, 'off')}
			{#if status.nextOff && draft.off.enabled}<p class="muted small">
					{t('powerSchedule.nextOff', { at: formatDateTime(new Date(status.nextOff)) })}
				</p>{/if}
			{@render times(draft.on, 'on')}
			{#if !status.rtc}<p class="muted small">{t('powerSchedule.noRtc')}</p>{:else}<p
					class="muted small"
				>
					{t('powerSchedule.rtcHint')}
				</p>{/if}
			{#if status.nextOn && draft.on.enabled}<p class="muted small">
					{t('powerSchedule.nextOn', { at: formatDateTime(new Date(status.nextOn)) })}
				</p>{/if}
			<label class="check"
				><input type="checkbox" bind:checked={draft.wakeOnLan} disabled={busy || !anyWake} /><span
					>{t('powerSchedule.wol')}</span
				></label
			>
			<ul class="ifaces">
				{#each status.interfaces as iface (iface.name)}
					<li>
						<span class="mono">{iface.name}</span><span class="mono muted">{iface.mac}</span><span
							class="chip {iface.supported ? (iface.enabled ? 'ok' : 'info') : ''}"
							>{t(
								iface.supported
									? iface.enabled
										? 'powerSchedule.wolOn'
										: 'powerSchedule.wolOff'
									: 'powerSchedule.wolNone',
							)}</span
						>
					</li>
				{/each}
			</ul>
			<p class="muted small">{t('powerSchedule.wolHint')}</p>
			<div class="actions">
				{#if saved}<span role="status">{t('notify.saved')}</span>{/if}
				<button class="btn btn-primary" disabled={busy}>{t('common.save')}</button>
			</div>
		</form>
	{/if}
</section>

<style>
	.schedule {
		gap: 10px;
	}
	form {
		display: flex;
		flex-direction: column;
		gap: 10px;
	}
	p {
		margin: 0;
	}
	.small {
		font-size: var(--text-base);
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-end;
		gap: 12px 20px;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: var(--text-base);
	}
	label > span {
		color: var(--muted);
	}
	.tiny {
		width: 120px;
	}
	.check {
		flex-direction: row;
		align-items: center;
		gap: 10px;
		font-size: var(--text-md);
		font-weight: 600;
	}
	.check > span {
		color: var(--text);
	}
	input[type='checkbox'] {
		width: 18px;
		height: 18px;
		margin: 0;
	}
	.days {
		display: flex;
		flex-wrap: wrap;
		gap: 4px;
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
	.ifaces {
		margin: 0;
		padding: 0;
		list-style: none;
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
	.ifaces li {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px 16px;
		font-size: var(--text-base);
	}
	.actions {
		display: flex;
		align-items: center;
		justify-content: flex-end;
		gap: 16px;
	}
</style>
