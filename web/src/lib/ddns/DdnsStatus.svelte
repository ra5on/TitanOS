<script lang="ts">
	// The state of dynamic DNS: found and published addresses, the last and
	// next look, the last error and "Update now" (Settings › Dynamic DNS).
	import { formatDateTime } from '#lib/format.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
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
			updateError = apiErrorText(err);
		}
	}
</script>

<!-- PLAN 78-1: the state is one row that stands open – its chip at the
     right, the addresses and times and the two buttons as its parts. -->
<SettingGroup title={t('ddns.statusTitle')} {id} desc={intro}>
	{#if status.disabled}<p class="notice-warn">{t('ddns.devMode')}</p>{/if}
	<SettingRow id="{id}-now" icon="activity" title={t('ddns.addresses')} open>
		{#snippet control()}<span class="chip {badge.tone}">{t(badge.text)}</span>{/snippet}
		<div class="srow-block">
			<dl>
				<div>
					<dt>{t('ddns.currentIPv4')}</dt>
					<dd class="mono">{status.currentIPv4 || '–'}</dd>
				</div>
				<div>
					<dt>{t('ddns.currentIPv6')}</dt>
					<dd class="mono">{status.currentIPv6 || '–'}</dd>
				</div>
				<div>
					<dt>{t('ddns.publishedIPv4')}</dt>
					<dd class="mono">{status.publishedIPv4 || '–'}</dd>
				</div>
				<div>
					<dt>{t('ddns.publishedIPv6')}</dt>
					<dd class="mono">{status.publishedIPv6 || '–'}</dd>
				</div>
				<div>
					<dt>{t('ddns.lastCheck')}</dt>
					<dd>{when(status.lastCheck)}</dd>
				</div>
				<div>
					<dt>{t('ddns.lastUpdate')}</dt>
					<dd>{when(status.lastUpdate)}</dd>
				</div>
				{#if status.nextCheck}<div>
						<dt>{t('ddns.nextCheck')}</dt>
						<dd>{when(status.nextCheck)}</dd>
					</div>{/if}
			</dl>
			{#if status.lastError}<div class="notice-error" role="alert">
					{t('ddns.lastError')}
					<pre>{status.lastError}</pre>
				</div>{/if}
		</div>
		<div class="srow-block actions">
			<button
				type="button"
				class="btn"
				disabled={!status.settings.enabled || status.running || status.disabled}
				onclick={update}>{t('ddns.updateNow')}</button
			>
			<button type="button" class="btn" onclick={onrefresh}>{t('ddns.refresh')}</button>
		</div>
	</SettingRow>
	{#if updateError}<p class="notice-error" role="alert">{updateError}</p>{/if}
</SettingGroup>

<style>
	dl {
		font-size: var(--text-md);
	}
	dl {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
		gap: var(--space-3) var(--space-5);
		margin: 0;
	}
	dt {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	dd {
		margin: 2px 0 0;
		overflow-wrap: anywhere;
	}
	pre {
		margin: var(--space-1-5) 0 0;
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
</style>
