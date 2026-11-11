<script lang="ts">
	// PLAN 65: the sensors as the kernel reports them, plus alarms and fans.
	// The choice for the old dashboard's tile is gone with the dashboard.
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import FanControl from '#lib/components/FanControl.svelte';
	import TempAlarms from './TempAlarms.svelte';
	import { system } from '#lib/system.svelte.ts';

	const sensors = $derived(system.info?.temperatures ?? []);
</script>

<PageHead title={t('temperatures.title')}>
	<p class="muted">{t('temperatures.description')}</p>
</PageHead>
<!-- PLAN 78-1: every sensor is a row with its value at the right. -->
<SettingGroup title={t('temperatures.sensors')}>
	{#if !system.info}<p class="muted">{t('accelerator.loading')}</p>
	{:else if sensors.length === 0}<p class="muted">{t('temperatures.none')}</p>{/if}
	{#each sensors as sensor (sensor.id)}
		<SettingRow icon="chip" title={sensor.label}>
			{#snippet description()}<span class="mono">{sensor.chip} · {sensor.device}</span>{/snippet}
			{#snippet control()}<span class="mono value"
					>{sensor.celsius === null ? '–' : `${sensor.celsius.toFixed(1)} °C`}</span
				>{/snippet}
		</SettingRow>
	{/each}
	<p class="muted hint">{t('temperatures.sensorHint')}</p>
</SettingGroup>
{#if session.data?.user?.role === 'owner' || session.data?.user?.role === 'admin'}<TempAlarms
	/><FanControl />{/if}

<style>
	p {
		margin: 0;
		font-size: var(--text-md);
	}
	.hint {
		margin-top: var(--space-1-5);
		font-size: var(--text-sm);
	}
	.value {
		color: var(--text);
		white-space: nowrap;
	}
</style>
