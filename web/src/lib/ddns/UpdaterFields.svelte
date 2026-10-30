<script lang="ts">
	// IPv4/IPv6, interval, lookup service and interface of the updater,
	// shared by Settings › Dynamic DNS and Settings › IPv64 › DynDNS.
	import { t } from '#lib/i18n/index.svelte.ts';
	import { INTERVALS, type UpdaterForm } from './ddns.ts';

	let { form = $bindable(), disabled = false }: { form: UpdaterForm; disabled?: boolean } =
		$props();
</script>

<div class="divided">
	<label class="check"
		><input type="checkbox" bind:checked={form.ipv4} {disabled} /><span>{t('ddns.ipv4')}</span
		></label
	>
	<p class="muted hint">{t('ddns.ipv4Hint')}</p>
	<label class="check"
		><input type="checkbox" bind:checked={form.ipv6} {disabled} /><span>{t('ddns.ipv6')}</span
		></label
	>
	<p class="muted hint">{t('ddns.ipv6Hint')}</p>
	<label class="stack"
		><span>{t('ddns.interval')}</span><select
			class="field short"
			bind:value={form.interval}
			{disabled}
		>
			<option value={0}>{t('ddns.intervalDefault')}</option>
			{#each INTERVALS as minutes (minutes)}<option value={minutes}
					>{t('ddns.minutes', { count: minutes })}</option
				>{/each}
		</select></label
	>
	<label class="stack"
		><span>{t('ddns.lookup')} <span class="muted">({t('dnsProvider.optional')})</span></span><input
			class="field mono"
			type="url"
			bind:value={form.lookupURL}
			placeholder="https://api.ipify.org"
			spellcheck="false"
			{disabled}
		/></label
	>
	<p class="muted hint">{t('ddns.lookupHint')}</p>
	<label class="stack"
		><span>{t('ddns.interface')} <span class="muted">({t('dnsProvider.optional')})</span></span
		><input
			class="field mono short"
			type="text"
			bind:value={form.iface}
			placeholder={t('ddns.interfaceAuto')}
			spellcheck="false"
			autocapitalize="off"
			{disabled}
		/></label
	>
</div>

<style>
	.divided {
		display: flex;
		flex-direction: column;
		gap: 10px;
	}
	.hint {
		margin: 0;
	}
	input[type='url'] {
		max-width: 32rem;
	}
	.short {
		max-width: 14rem;
	}
</style>
