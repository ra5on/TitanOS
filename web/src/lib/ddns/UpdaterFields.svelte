<script lang="ts">
	// IPv4/IPv6, interval, lookup service and interface of the updater,
	// shared by Settings › Dynamic DNS and Settings › IPv64 › DynDNS.
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { INTERVALS, type UpdaterForm } from './ddns.ts';

	let { form = $bindable(), disabled = false }: { form: UpdaterForm; disabled?: boolean } =
		$props();
	const uid = $props.id();
</script>

<!-- PLAN 78-1: every value is a row of the group the form stands in. -->
<SettingRow icon="globe" title={t('ddns.ipv4')} desc={t('ddns.ipv4Hint')} for="{uid}-ipv4">
	{#snippet control()}
		<input
			id="{uid}-ipv4"
			class="switch"
			type="checkbox"
			role="switch"
			bind:checked={form.ipv4}
			{disabled}
		/>
	{/snippet}
</SettingRow>
<SettingRow icon="globe" title={t('ddns.ipv6')} desc={t('ddns.ipv6Hint')} for="{uid}-ipv6">
	{#snippet control()}
		<input
			id="{uid}-ipv6"
			class="switch"
			type="checkbox"
			role="switch"
			bind:checked={form.ipv6}
			{disabled}
		/>
	{/snippet}
</SettingRow>
<SettingRow icon="clock" title={t('ddns.interval')} for="{uid}-interval">
	{#snippet control()}
		<select id="{uid}-interval" class="field" bind:value={form.interval} {disabled}>
			<option value={0}>{t('ddns.intervalDefault')}</option>
			{#each INTERVALS as minutes (minutes)}<option value={minutes}
					>{t('ddns.minutes', { count: minutes })}</option
				>{/each}
		</select>
	{/snippet}
</SettingRow>
<SettingRow
	icon="search"
	title="{t('ddns.lookup')} ({t('dnsProvider.optional')})"
	desc={t('ddns.lookupHint')}
	for="{uid}-lookup"
>
	{#snippet control()}
		<input
			id="{uid}-lookup"
			class="field mono"
			type="url"
			bind:value={form.lookupURL}
			placeholder="https://api.ipify.org"
			spellcheck="false"
			{disabled}
		/>
	{/snippet}
</SettingRow>
<SettingRow
	icon="columns"
	title="{t('ddns.interface')} ({t('dnsProvider.optional')})"
	for="{uid}-iface"
>
	{#snippet control()}
		<input
			id="{uid}-iface"
			class="field mono"
			type="text"
			bind:value={form.iface}
			placeholder={t('ddns.interfaceAuto')}
			spellcheck="false"
			autocapitalize="off"
			{disabled}
		/>
	{/snippet}
</SettingRow>
