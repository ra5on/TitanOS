<script lang="ts">
	// "Use VLANs": off by default, because VLANs need a router or switch that
	// understands them. Switching on asks once; switching off is refused
	// while a network still uses a VLAN.
	import HelpTip from '#lib/components/HelpTip.svelte';
	import { api } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import type { NetworkStatus } from '#lib/network.ts';

	let {
		enabled,
		inUse,
		disabled,
		onchanged,
	}: {
		enabled: boolean;
		inUse: boolean;
		disabled: boolean;
		onchanged: (status: NetworkStatus) => void;
	} = $props();

	let asking = $state(false);
	let busy = $state(false);
	let error = $state('');

	async function save(vlans: boolean) {
		busy = true;
		error = '';
		try {
			const res = await api.put<{ network: NetworkStatus }>('/network/options', { vlans });
			onchanged(res.network);
			asking = false;
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}
</script>

<!-- PLAN 78-1: one switch, one row. -->
<SettingGroup title={t('netx.vlans.title')} id="vlans">
	<SettingRow
		icon="columns"
		title={t('netx.vlans.use')}
		desc={t(enabled && inUse ? 'netx.vlans.inUse' : 'netx.vlans.hint')}
		for="net-vlans-switch"
	>
		{#snippet badge()}<HelpTip term="vlan" />{/snippet}
		{#snippet control()}
			<input
				id="net-vlans-switch"
				class="switch"
				type="checkbox"
				role="switch"
				checked={enabled}
				disabled={disabled || busy || (enabled && inUse)}
				onchange={(e) => {
					const on = e.currentTarget.checked;
					e.currentTarget.checked = enabled;
					if (on) asking = true;
					else void save(false);
				}}
			/>
		{/snippet}
	</SettingRow>
	{#if error && !asking}<p class="notice-error" role="alert">{error}</p>{/if}
</SettingGroup>

{#if asking}
	<Dialog title={t('netx.vlans.confirmTitle')} locked={busy} onclose={() => (asking = false)}>
		<p>{t('netx.vlans.confirmText')}</p>
		<p class="muted">{t('netx.vlans.confirmLocal')}</p>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (asking = false)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-primary" disabled={busy} onclick={() => save(true)}
				>{t('netx.vlans.confirm')}</button
			>
		</div>
	</Dialog>
{/if}
