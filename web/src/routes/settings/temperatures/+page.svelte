<script lang="ts">
	import { api, ApiError } from '#lib/api.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { system } from '#lib/system.svelte.ts';
	import { sanitizeLayout } from '#lib/dashboard/tiles.ts';
	import { temperatureSelection, withTemperatures, type SelectedTemperature } from '#lib/dashboard/temperatures.ts';

	let show = $state(sanitizeLayout(session.dashboard).some((tile) => tile.id === 'temperatures'));
	let selected = $state(temperatureSelection(session.dashboard));
	let saving = $state(false);
	let error = $state('');
	let saved = $state(false);
	const sensors = $derived(system.info?.temperatures ?? []);
	const missing = $derived(selected.filter((choice) => !sensors.some((sensor) => sensor.id === choice.id)));

	function toggle(choice: SelectedTemperature, checked: boolean) {
		selected = checked ? [...selected, { id: choice.id, chip: choice.chip, label: choice.label }] : selected.filter((sensor) => sensor.id !== choice.id);
		saved = false;
	}
	async function save() {
		saving = true; error = ''; saved = false;
		const preferences = withTemperatures(session.dashboard, show, $state.snapshot(selected));
		try {
			await api.put('/preferences/dashboard', preferences);
			session.dashboard = preferences;
			saved = true;
		} catch (err) { error = errorText(err instanceof ApiError ? err.code : 'internal'); }
		finally { saving = false; }
	}
</script>

<PageHead title={t('temperatures.title')} parent={{ href: '/settings', label: t('nav.settings') }}>
	<p class="muted">{t('temperatures.description')}</p>
</PageHead>
<section class="tile options">
	<label class="check"><input type="checkbox" bind:checked={show} disabled={saving} onchange={() => saved = false} /><strong>{t('temperatures.show')}</strong></label>
	<p class="muted">{t('temperatures.perAccount')}</p>
	{#if !system.info}<p>{t('accelerator.loading')}</p>
	{:else if sensors.length === 0}<p class="muted">{t('temperatures.none')}</p>{/if}
	<div class="sensors">
		{#each sensors as sensor (sensor.id)}
			<label class="sensor">
				<input type="checkbox" checked={selected.some((choice) => choice.id === sensor.id)} disabled={saving} onchange={(event) => toggle(sensor, event.currentTarget.checked)} />
				<span class="name"><strong>{sensor.label}</strong><span class="muted mono">{sensor.chip} · {sensor.device}</span></span>
				<span class="mono value">{sensor.celsius === null ? '–' : `${sensor.celsius.toFixed(1)} °C`}</span>
			</label>
		{/each}
		{#each missing as choice (choice.id)}
			<label class="sensor"><input type="checkbox" checked disabled={saving} onchange={(event) => toggle(choice, event.currentTarget.checked)} /><span class="name"><strong>{choice.chip} · {choice.label}</strong><span class="muted">{t('temperatures.missing')}</span></span><span class="mono">–</span></label>
		{/each}
	</div>
	<p class="muted">{t('temperatures.sensorHint')}</p>
	{#if show && selected.length === 0}<p class="notice-warn">{t('temperatures.choose')}</p>{/if}
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if saved}<p role="status">{t('temperatures.saved')}</p>{/if}
	<div class="actions"><button class="btn btn-primary" disabled={saving || (show && selected.length === 0)} onclick={save}>{t('common.save')}</button><a class="btn" href="/">{t('dashboard.title')}</a></div>
</section>

<style>
	.options { padding: var(--tile-pad); display: flex; flex-direction: column; gap: 16px; }
	p { margin: 0; font-size: 0.875rem; }
	.check, .sensor, .actions { display: flex; align-items: center; gap: 12px; }
	input { width: 18px; height: 18px; flex: none; }
	.sensors { display: flex; flex-direction: column; }
	.sensor { padding: 12px 0; border-bottom: 1px solid var(--panel-border); }
	.name { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 4px; overflow-wrap: anywhere; }
	.name strong { font-weight: 500; }
	.name .muted { font-size: 0.75rem; }
	.value { white-space: nowrap; }
	.actions { flex-wrap: wrap; }
</style>
