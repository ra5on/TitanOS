<script lang="ts">
	// "Use VLANs": off by default, because VLANs need a router or switch that
	// understands them. Switching on asks once; switching off is refused
	// while a network still uses a VLAN.
	import { api } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
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

<section class="tile panel" aria-labelledby="net-vlans" id="vlans">
	<h2 class="tile-title" id="net-vlans">{t('netx.vlans.title')}</h2>
	<label class="check">
		<input
			type="checkbox"
			checked={enabled}
			disabled={disabled || busy || (enabled && inUse)}
			onchange={(e) => {
				const on = e.currentTarget.checked;
				e.currentTarget.checked = enabled;
				if (on) asking = true;
				else void save(false);
			}}
		/><span>{t('netx.vlans.use')}</span>
	</label>
	<p class="muted hint">{t(enabled && inUse ? 'netx.vlans.inUse' : 'netx.vlans.hint')}</p>
	{#if error && !asking}<p class="notice-error" role="alert">{error}</p>{/if}
</section>

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

<style>
	.hint {
		font-size: var(--text-base);
	}
</style>
