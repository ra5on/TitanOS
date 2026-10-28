<script lang="ts">
	// Settings › IPv64: the account at IPv64.net managed from TitanOS. The
	// key is the one of this page or of dynamic DNS / Let's Encrypt; it never
	// comes back from titand. Nothing is created at IPv64 without a click.
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api } from '#lib/api.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import Tabs from '#lib/components/Tabs.svelte';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { TABS, tabFrom, type Status, type Tab } from './ipv64.ts';
	import { failure } from './ui.ts';
	import Overview from './Overview.svelte';
	import Domains from './Domains.svelte';
	import DynDns from './DynDns.svelte';
	import Assignments from './Assignments.svelte';
	import Monitoring from './Monitoring.svelte';
	import Failover from './Failover.svelte';
	import PublicAccess from './PublicAccess.svelte';
	import CloudRouter from './CloudRouter.svelte';
	import Blocklist from './Blocklist.svelte';

	let status = $state<Status | null>(null);
	let loadError = $state('');
	let tab = $state<Tab>(tabFrom(page.url.searchParams.get('tab')));
	let key = $state('');
	let keyBusy = $state(false);
	let keyError = $state('');

	async function load() {
		try {
			status = await api.get<Status>('/ipv64');
			loadError = '';
		} catch (err) {
			loadError = failure(err);
		}
	}
	onMount(load);
	// Links inside the page (e.g. "in DDNS" → ?tab=dyndns) change the tab too.
	$effect(() => {
		tab = tabFrom(page.url.searchParams.get('tab'));
	});

	function choose(next: Tab) {
		tab = next;
		goto(`/settings/ipv64?tab=${next}`, { replaceState: true });
	}

	async function saveKey(clear = false) {
		keyBusy = true;
		keyError = '';
		try {
			status = await api.put<Status>('/ipv64/key', clear ? { clear: true } : { apiKey: key.trim() });
			key = '';
		} catch (err) {
			keyError = failure(err);
		} finally {
			keyBusy = false;
		}
	}
	const hasKey = $derived(!!status?.status.keySource);
</script>

<PageHead title={t('ipv64x.title')} parent={{ href: '/settings', label: t('nav.settings') }}>
	<p class="muted">{t('ipv64x.intro')}</p>
</PageHead>

{#if loadError}
	<div class="notice-error" role="alert">{loadError}<button class="btn" onclick={load}>{t('ui.retry')}</button></div>
{:else if status}
	<section class="tile panel" aria-labelledby="ipv64-key">
		<h2 class="tile-title" id="ipv64-key">{t('ipv64x.key')}</h2>
		{#if hasKey}
			<p>{t(`ipv64x.keySource.${status.status.keySource}` as MessageKey)}</p>
		{:else}
			<p class="notice-warn">{t('ipv64x.noKey')}</p>
		{/if}
		<p class="muted hint">{t('ipv64x.keyHint')}</p>
		<form
			class="row-actions"
			onsubmit={(event) => {
				event.preventDefault();
				if (key.trim()) saveKey();
			}}
		>
			<label class="stack grow"
				><span>{t(status.status.keySet ? 'ipv64x.keyReplace' : 'ipv64x.keyOwn')}</span><input
					class="field mono"
					type="password"
					autocomplete="off"
					bind:value={key}
					placeholder={status.status.keySet ? t('ipv64x.isSet') : ''}
					disabled={keyBusy}
				/></label
			>
			<button class="btn btn-primary" disabled={keyBusy || !key.trim()}>{t('common.save')}</button>
			{#if status.status.keySet}<button type="button" class="btn" disabled={keyBusy} onclick={() => saveKey(true)}>{t('ipv64x.keyRemove')}</button>{/if}
		</form>
		{#if keyError}<p class="notice-error" role="alert">{keyError}</p>{/if}
		{#if status.status.readOnly}<p class="notice-warn">{t('ipv64x.devMode')}</p>{/if}
	</section>

	<Tabs tabs={TABS.map((id) => ({ id, label: t(`ipv64x.tab.${id}` as MessageKey) }))} value={tab} label={t('ipv64x.title')} onchange={choose} />

	{#if !hasKey}
		<p class="muted">{t('ipv64x.needKeyForTabs')}</p>
	{:else if tab === 'overview'}
		<Overview {status} />
	{:else if tab === 'domains'}
		<Domains {status} />
	{:else if tab === 'dyndns'}
		<DynDns {status} />
	{:else if tab === 'assignments'}
		<Assignments {status} />
	{:else if tab === 'monitoring'}
		<Monitoring {status} />
	{:else if tab === 'failover'}
		<Failover {status} onchange={load} />
	{:else if tab === 'public'}
		<PublicAccess {status} />
	{:else if tab === 'router'}
		<CloudRouter {status} onchange={load} />
	{:else}
		<Blocklist {status} onchange={(next) => (status = next)} />
	{/if}
{/if}

<style>
	.hint {
		margin: 0;
	}
	.grow {
		flex: 1 1 18rem;
	}
	form.row-actions {
		align-items: flex-end;
	}
	@media (max-width: 760px) {
		:global(main .ipv64-tab table td),
		:global(main .ipv64-tab table td.actions),
		:global(main .ipv64-tab table td .btn) {
			white-space: nowrap;
			overflow-wrap: normal;
		}
	}
	:global(.ipv64-tab) {
		display: flex;
		flex-direction: column;
		gap: var(--gap, 16px);
		font-size: 0.875rem;
	}
	:global(.ipv64-tab .table-wrap) {
		overflow-x: auto;
	}
	:global(.ipv64-tab table) {
		width: 100%;
		border-collapse: collapse;
	}
	:global(.ipv64-tab th),
	:global(.ipv64-tab td) {
		text-align: left;
		padding: 6px 8px;
		border-bottom: 1px solid var(--border);
		vertical-align: middle;
	}
	:global(.ipv64-tab th) {
		color: var(--muted);
		font-weight: 500;
		font-size: 0.75rem;
	}
	:global(.ipv64-tab td) {
		white-space: nowrap;
	}
	:global(.ipv64-tab td.wrap) {
		white-space: normal;
	}
	:global(.ipv64-tab td.actions) {
		text-align: right;
		white-space: normal;
	}
	:global(.ipv64-tab td.actions .btn) {
		margin: 2px 0 2px 6px;
	}
	:global(.ipv64-tab .hint) {
		margin: 0;
	}
	:global(.ipv64-tab h3) {
		margin: 0;
		font-size: 0.9375rem;
	}
	:global(.ipv64-tab dl) {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(190px, 1fr));
		gap: 12px 24px;
		margin: 0;
	}
	:global(.ipv64-tab dt) {
		color: var(--muted);
		font-size: 0.75rem;
	}
	:global(.ipv64-tab dd) {
		margin: 2px 0 0;
		overflow-wrap: anywhere;
	}
	:global(.ipv64-tab .inline) {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-end;
		gap: 10px;
	}
	:global(.ipv64-tab .grow) {
		flex: 1 1 14rem;
	}
	:global(.ipv64-tab .short) {
		max-width: 9rem;
	}
	:global(.ipv64-tab .wrap) {
		overflow-wrap: anywhere;
		max-width: 28rem;
	}
	:global(.ipv64-tab fieldset) {
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		padding: 8px 12px;
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
	:global(.ipv64-tab .meter) {
		height: 6px;
		border-radius: var(--radius-bar);
		background: var(--track);
		overflow: hidden;
		margin-top: 4px;
	}
	:global(.ipv64-tab .meter > span) {
		display: block;
		height: 100%;
		background: var(--accent);
	}
</style>
