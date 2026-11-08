<script lang="ts">
	// Settings › Temperatures › Heat notifications: which sensors notify
	// (notify/hardware_watch.go), muted per sensor for the whole NAS.
	import { api } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { system } from '#lib/system.svelte.ts';

	// own: the limit comes from the chip's critical value, else from the
	// default for its kind of chip (notify.AlarmLimitSource).
	type Alarm = { id: string; limit: number | null; own?: boolean; muted: boolean };
	type Alarms = { sensors: Alarm[]; muted: string[] };
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

<section class="tile panel" aria-labelledby="alarms-title">
	<h2 class="tile-title" id="alarms-title">{t('temperatures.alarmTitle')}</h2>
	<p class="muted">{t('temperatures.alarmHint')}</p>
	<div class="sensors">
		{#each sensors as sensor (sensor.id)}
			{@const alarm = alarmOf(sensor.id)}
			{@const limit = alarm?.limit ?? null}
			{@const muted = !!alarms?.muted.includes(sensor.id)}
			<div class="sensor">
				<span class="name"
					><strong>{sensor.label}</strong><span class="muted mono"
						>{sensor.chip} · {sensor.device}</span
					><span class="muted"
						>{!alarms
							? '–'
							: limit === null
								? t('temperatures.alarmNever')
								: muted
									? t('temperatures.alarmMuted')
									: t(alarm?.own ? 'temperatures.alarmAtOwn' : 'temperatures.alarmAtDefault', {
											celsius: limit.toFixed(0),
											chip: sensor.chip,
										})}</span
					></span
				>
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
			</div>
		{/each}
	</div>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if saved}<p role="status">{t('temperatures.alarmSaved')}</p>{/if}
</section>

<style>
	p {
		font-size: var(--text-md);
	}
	.sensors {
		display: flex;
		flex-direction: column;
	}
	.sensor {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		padding: var(--space-3) 0;
		border-bottom: 1px solid var(--panel-border);
	}
	.name {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		overflow-wrap: anywhere;
	}
	.name strong {
		font-weight: 500;
	}
	.name .muted {
		font-size: var(--text-sm);
	}
	.value {
		white-space: nowrap;
	}
	.check {
		display: flex;
		align-items: center;
		gap: var(--space-1-5);
		white-space: nowrap;
	}
</style>
