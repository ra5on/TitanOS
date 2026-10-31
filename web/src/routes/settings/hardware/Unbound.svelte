<script lang="ts">
	// Geräte ohne Treiber: read once when the page opens (the check reads the
	// kernel log), again only on request.
	import { api } from '#lib/api.ts';
	import UnboundDevices from '#lib/components/UnboundDevices.svelte';
	import type { HardwareReport } from '#lib/hwcheck.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { onMount } from 'svelte';

	let report = $state<HardwareReport | null>(null);
	let error = $state('');
	let loading = $state(false);

	async function load() {
		loading = true;
		error = '';
		try {
			report = await api.get<HardwareReport>('/hardware/report');
		} catch (err) {
			error = apiErrorText(err);
		}
		loading = false;
	}
	onMount(load);
</script>

<section class="tile panel" aria-labelledby="hw-unbound">
	<div class="head">
		<h2 class="tile-title" id="hw-unbound">{t('hwcheck.unbound')}</h2>
		<button type="button" class="btn" disabled={loading} onclick={load}
			>{t(loading ? 'hwcheck.running' : 'hwcheck.again')}</button
		>
	</div>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if report}
		<UnboundDevices unbound={report.unbound} firmware={report.firmwareMissing} />
	{:else if loading}
		<p class="muted">{t('hwcheck.running')}</p>
	{/if}
</section>

<style>
	.head {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 10px;
	}
	.head h2 {
		margin: 0;
	}
</style>
