<script lang="ts">
	// PLAN 65: the sensors as the kernel reports them, plus alarms and fans.
	// The choice for the old dashboard's tile is gone with the dashboard.
	import PageHead from '#lib/components/PageHead.svelte';
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
<section class="tile panel">
	{#if !system.info}<p>{t('accelerator.loading')}</p>
	{:else if sensors.length === 0}<p class="muted">{t('temperatures.none')}</p>{/if}
	<div class="sensors">
		{#each sensors as sensor (sensor.id)}
			<div class="sensor">
				<span class="name"
					><strong>{sensor.label}</strong><span class="muted mono"
						>{sensor.chip} · {sensor.device}</span
					></span
				>
				<span class="mono value"
					>{sensor.celsius === null ? '–' : `${sensor.celsius.toFixed(1)} °C`}</span
				>
			</div>
		{/each}
	</div>
	<p class="muted">{t('temperatures.sensorHint')}</p>
</section>
{#if session.data?.user?.role === 'owner' || session.data?.user?.role === 'admin'}<TempAlarms
	/><FanControl />{/if}

<style>
	p {
		font-size: var(--text-md);
	}
	.sensor {
		display: flex;
		align-items: center;
		gap: var(--space-3);
	}
	.sensors {
		display: flex;
		flex-direction: column;
	}
	.sensor {
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
</style>
