<script lang="ts">
	// Settings › Temperatures › Heat notifications: which sensors notify
	// (notify/hardware_watch.go), muted per sensor for the whole NAS.
	import { api } from '#lib/api.ts';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import SettingSub from '#lib/components/settings/SettingSub.svelte';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { system } from '#lib/system.svelte.ts';

	// own: the limit comes from the chip's critical value, else from the
	// default for its kind of chip (notify.AlarmLimitSource).
	type Alarm = {
		id: string;
		limit: number | null;
		own?: boolean;
		muted: boolean;
		shutdown?: boolean;
	};
	// heatShutdown: PLAN 75-9, shut down when a reliable sensor stays too hot.
	type Alarms = { sensors: Alarm[]; muted: string[]; heatShutdown?: boolean };
	const sensors = $derived(system.info?.temperatures ?? []);
	let alarms = $state<Alarms | null>(null);
	let busy = $state(false);
	let error = $state('');
	let saved = $state(false);
	const alarmOf = (id: string) => alarms?.sensors.find((alarm) => alarm.id === id);

	async function load() {
		try {
			alarms = await api.get<Alarms>('/notifications/temperatures');
		} catch (err) {
			error = apiErrorText(err);
		}
	}
	$effect(() => {
		load();
	});

	// The sensors that can switch the NAS off (reliable chip, own critical
	// value, not muted): notify.ShutsDownOnHeat.
	const guards = $derived(
		sensors.filter((s) => alarmOf(s.id)?.shutdown && !alarms?.muted.includes(s.id)),
	);

	// How many sensors notify right now (the closed row says it).
	const reporting = $derived(
		sensors.filter((s) => (alarmOf(s.id)?.limit ?? null) !== null && !alarms?.muted.includes(s.id))
			.length,
	);

	async function setHeat(on: boolean) {
		if (!alarms) return;
		busy = true;
		error = '';
		saved = false;
		try {
			alarms = await api.put<Alarms>('/notifications/temperatures', {
				muted: alarms.muted,
				heatShutdown: on,
			});
			saved = true;
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}

	async function toggle(id: string, report: boolean) {
		if (!alarms) return;
		const muted = report ? alarms.muted.filter((m) => m !== id) : [...alarms.muted, id];
		busy = true;
		error = '';
		saved = false;
		try {
			alarms = await api.put<Alarms>('/notifications/temperatures', { muted });
			saved = true;
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
</script>

<!-- PLAN 78-1: the sensors that notify are the parts of one row; the heat
     shutdown is a row of its own. Both store on change, as before. -->
<SettingGroup title={t('temperatures.alarmTitle')} id="alarms">
	<SettingRow
		id="alarm-sensors"
		icon="bell"
		title={t('temperatures.alarmSensors')}
		desc={t('temperatures.alarmHint')}
		summary={alarms ? t('temperatures.alarmCount', { on: reporting, total: sensors.length }) : ''}
	>
		{#each sensors as sensor (sensor.id)}
			{@const alarm = alarmOf(sensor.id)}
			{@const limit = alarm?.limit ?? null}
			{@const muted = !!alarms?.muted.includes(sensor.id)}
			<SettingSub title={sensor.label}>
				{#snippet description()}<span class="mono">{sensor.chip} · {sensor.device}</span><br
					/>{!alarms
						? '–'
						: limit === null
							? t('temperatures.alarmNever')
							: muted
								? t('temperatures.alarmMuted')
								: t(alarm?.own ? 'temperatures.alarmAtOwn' : 'temperatures.alarmAtDefault', {
										celsius: limit.toFixed(0),
										chip: sensor.chip,
									})}{/snippet}
				{#snippet control()}
					<span class="mono value"
						>{sensor.celsius === null ? '–' : `${sensor.celsius.toFixed(1)} °C`}</span
					>
					<label class="check"
						><input
							type="checkbox"
							checked={limit !== null && !muted}
							disabled={!alarms || busy || limit === null}
							aria-label={t('temperatures.alarmReportFor', {
								sensor: `${sensor.chip} ${sensor.label}`,
							})}
							onchange={(e) => toggle(sensor.id, e.currentTarget.checked)}
						/><span aria-hidden="true">{t('temperatures.alarmReport')}</span></label
					>
				{/snippet}
			</SettingSub>
		{/each}
		{#if sensors.length === 0}<div class="srow-block">
				<p class="muted">{t('temperatures.none')}</p>
			</div>{/if}
	</SettingRow>
	<SettingRow
		icon="power"
		title={t('temperatures.heatTitle')}
		for="temp-heat-shutdown"
		desc={guards.length
			? t('temperatures.heatHint', {
					sensors: guards.map((s) => `${s.chip} ${s.label}`).join(', '),
				})
			: t('temperatures.heatNone')}
	>
		{#snippet control()}
			<input
				id="temp-heat-shutdown"
				type="checkbox"
				class="switch"
				role="switch"
				checked={!!alarms?.heatShutdown}
				disabled={!alarms || busy}
				onchange={(e) => setHeat(e.currentTarget.checked)}
			/>
		{/snippet}
	</SettingRow>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if saved}<p class="muted" role="status">{t('temperatures.alarmSaved')}</p>{/if}
</SettingGroup>

<style>
	p {
		margin: 0;
		font-size: var(--text-md);
	}
	.value {
		color: var(--text);
		white-space: nowrap;
	}
	.check {
		display: flex;
		align-items: center;
		gap: var(--space-1-5);
		white-space: nowrap;
		color: var(--text);
	}
</style>
