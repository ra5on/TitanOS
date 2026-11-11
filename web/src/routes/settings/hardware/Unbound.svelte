<script lang="ts">
	// Geräte ohne Treiber: read once when the page opens (the check reads the
	// kernel log), again only on request.
	import { api } from '#lib/api.ts';
	import UnboundDevices from '#lib/components/UnboundDevices.svelte';
	import type { HardwareReport } from '#lib/hwcheck.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { onMount } from 'svelte';
	import { attention } from '#lib/hwcheck.ts';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';

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
	// What needs a look: devices a driver should have taken (not the system parts).
	const shown = $derived(report ? attention(report.unbound) : []);
</script>

<!-- PLAN 78-1, draft 78: two rows; one opens when it has something to list. -->
<SettingGroup title={t('hardware.drivers')} id="hw-unbound">
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	<SettingRow
		id="unbound"
		icon="tool"
		title={t('hwcheck.unbound')}
		desc={report
			? shown.length
				? t('hwcheck.unboundCount', { count: shown.length })
				: t('hwcheck.unboundNone')
			: t('hwcheck.running')}
	>
		{#snippet control()}
			{#if report}<span class="chip {shown.length ? 'warn' : 'ok'}"
					>{t(shown.length ? 'hwcheck.look' : 'hwcheck.fine')}</span
				>{/if}
			<button type="button" class="btn" disabled={loading} onclick={load}
				>{t(loading ? 'hwcheck.running' : 'hwcheck.again')}</button
			>
		{/snippet}
		{#if report?.unbound.length}
			<div class="srow-block">
				<UnboundDevices unbound={report.unbound} firmware={[]} part="unbound" />
			</div>
		{/if}
	</SettingRow>
	<SettingRow
		id="firmware"
		icon="chip"
		title={t('hwcheck.firmware')}
		desc={report
			? report.firmwareMissing.length
				? t('hwcheck.firmwareCount', { count: report.firmwareMissing.length })
				: t('hwcheck.firmwareNone')
			: t('hwcheck.running')}
	>
		{#snippet control()}
			{#if report}<span class="chip {report.firmwareMissing.length ? 'warn' : 'ok'}"
					>{t(report.firmwareMissing.length ? 'hwcheck.look' : 'hwcheck.fine')}</span
				>{/if}
		{/snippet}
		{#if report?.firmwareMissing.length}
			<div class="srow-block">
				<UnboundDevices unbound={[]} firmware={report.firmwareMissing} part="firmware" />
			</div>
		{/if}
	</SettingRow>
</SettingGroup>
