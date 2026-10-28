<script lang="ts">
	// The state of dynamic DNS: found and published addresses, the last and
	// next look, the last error and "Update now". Shared by Settings ›
	// Dynamic DNS and Settings › IPv64 › DynDNS.
	import { ApiError } from '#lib/api.ts';
	import { formatDateTime } from '#lib/format.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { revealTarget } from '#lib/reveal-target.ts';
	import { ddnsBadge, updateNow, type DdnsStatus } from './ddns.ts';

	let {
		status,
		intro,
		id = 'ddns-status',
		onstatus,
		onrefresh,
	}: {
		status: DdnsStatus;
		intro: string;
		id?: string;
		onstatus: (next: DdnsStatus) => void;
		onrefresh: () => void;
	} = $props();

	let updateError = $state('');
	const badge = $derived(ddnsBadge(status));
	const when = (value?: string) => (value ? formatDateTime(new Date(value)) : '–');

	async function update() {
		updateError = '';
		try {
			onstatus(await updateNow());
			setTimeout(onrefresh, 3000);
		} catch (err) {
			updateError = errorText(err instanceof ApiError ? err.code : 'internal');
		}
	}
</script>

<section class="tile panel" aria-labelledby={id}>
	<h2 class="tile-title" {id} use:revealTarget>{t('ddns.statusTitle')}</h2>
	<div class="head"><span class="chip {badge.tone}">{t(badge.text)}</span></div>
	{#if status.disabled}<p class="notice-warn">{t('ddns.devMode')}</p>{/if}
	<p class="muted">{intro}</p>
	<dl>
		<div><dt>{t('ddns.currentIPv4')}</dt><dd class="mono">{status.currentIPv4 || '–'}</dd></div>
		<div><dt>{t('ddns.currentIPv6')}</dt><dd class="mono">{status.currentIPv6 || '–'}</dd></div>
		<div><dt>{t('ddns.publishedIPv4')}</dt><dd class="mono">{status.publishedIPv4 || '–'}</dd></div>
		<div><dt>{t('ddns.publishedIPv6')}</dt><dd class="mono">{status.publishedIPv6 || '–'}</dd></div>
		<div><dt>{t('ddns.lastCheck')}</dt><dd>{when(status.lastCheck)}</dd></div>
		<div><dt>{t('ddns.lastUpdate')}</dt><dd>{when(status.lastUpdate)}</dd></div>
		{#if status.nextCheck}<div><dt>{t('ddns.nextCheck')}</dt><dd>{when(status.nextCheck)}</dd></div>{/if}
	</dl>
	{#if status.lastError}<div class="notice-error" role="alert">{t('ddns.lastError')}<pre>{status.lastError}</pre></div>{/if}
	<div class="row-actions">
		<button type="button" class="btn" disabled={!status.settings.enabled || status.running || status.disabled} onclick={update}
			>{t('ddns.updateNow')}</button
		>
		<button type="button" class="btn" onclick={onrefresh}>{t('ddns.refresh')}</button>
	</div>
	{#if updateError}<p class="notice-error" role="alert">{updateError}</p>{/if}
</section>

<style>
	section {
		font-size: 0.875rem;
	}
	.head {
		display: flex;
		align-items: center;
		gap: 12px;
	}
	dl {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
		gap: 12px 24px;
		margin: 0;
	}
	dt {
		color: var(--muted);
		font-size: 0.75rem;
	}
	dd {
		margin: 2px 0 0;
		overflow-wrap: anywhere;
	}
	pre {
		margin: 6px 0 0;
		font-family: var(--font-mono);
		font-size: 0.75rem;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
</style>
