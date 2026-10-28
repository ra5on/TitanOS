<script lang="ts">
	// Dynamic DNS (backend package ddns): host names at the public IPv4
	// address of the connection and the global IPv6 address of the NAS.
	import { onMount } from 'svelte';
	import { api, ApiError } from '#lib/api.ts';
	import DnsProviderFields from '#lib/components/DnsProviderFields.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import {
		missingFields,
		parseNames,
		providerForm,
		providerPayload,
		validHostName,
		type DnsProvider,
		type SecretName,
	} from '#lib/dns-providers.ts';
	import { formatDateTime } from '#lib/format.ts';
	import { errorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { revealTarget } from '#lib/reveal-target.ts';
	import Ipv64Panel from './Ipv64Panel.svelte';
	import type { DdnsStatus } from './ddns.ts';

	let status = $state<DdnsStatus | null>(null);
	let loadError = $state('');
	let busy = $state(false);
	let error = $state('');
	let saved = $state(false);
	let updateError = $state('');

	let enabled = $state(false);
	let names = $state('');
	let ipv4 = $state(true);
	let ipv6 = $state(true);
	let interval = $state(0);
	let lookupURL = $state('');
	let iface = $state('');
	let provider = $state<DnsProvider>({ type: '' });
	let cleared = $state<SecretName[]>([]);

	function fill(s: DdnsStatus) {
		const set = s.settings;
		enabled = set.enabled;
		names = set.hostnames.join('\n');
		ipv4 = set.ipv4;
		ipv6 = set.ipv6;
		interval = set.interval ?? 0;
		lookupURL = set.lookupURL ?? '';
		iface = set.interface ?? '';
		provider = providerForm(set.provider);
		cleared = [];
	}

	async function load(refill = true) {
		try {
			const next = await api.get<DdnsStatus>('/network/ddns');
			if (refill || !status) fill(next);
			status = next;
			loadError = '';
		} catch (err) {
			loadError = errorText(err instanceof ApiError ? err.code : 'internal');
		}
	}
	onMount(() => {
		load();
	});
	$effect(() => {
		if (!status?.running) return;
		const timer = setInterval(() => {
			if (!document.hidden) load(false);
		}, 3000);
		return () => clearInterval(timer);
	});
	$effect(() => {
		if (saved) {
			const timer = setTimeout(() => (saved = false), 5000);
			return () => clearTimeout(timer);
		}
	});

	const hostnames = $derived(parseNames(names));
	const badNames = $derived(hostnames.filter((name) => !validHostName(name)));
	const missing = $derived(
		status ? missingFields(provider, status.settings.provider, status.secrets, cleared, 'address') : [],
	);
	const valid = $derived(
		badNames.length === 0 &&
			hostnames.length <= 8 &&
			(!enabled || (hostnames.length > 0 && (ipv4 || ipv6) && !!provider.type && missing.length === 0)),
	);
	const intervals = [5, 10, 15, 30, 60, 360, 1440];

	async function save(event: SubmitEvent) {
		event.preventDefault();
		if (busy || !valid || !status) return;
		busy = true;
		error = '';
		saved = false;
		try {
			const next = await api.put<DdnsStatus>('/network/ddns', {
				enabled,
				hostnames,
				ipv4,
				ipv6,
				interval,
				lookupURL: lookupURL.trim(),
				interface: iface.trim(),
				provider: providerPayload(provider, 'address'),
				// The healthcheck is switched on and off in its own section.
				heartbeat: { ...status.settings.heartbeat },
				clearSecrets: cleared,
			});
			status = next;
			fill(next);
			saved = true;
		} catch (err) {
			error =
				err instanceof ApiError && err.code === 'invalid_ddns'
					? `${errorText(err.code)} ${err.message.replace(/^ddns: invalid settings: /, '')}`
					: errorText(err instanceof ApiError ? err.code : 'internal');
		} finally {
			busy = false;
		}
	}

	async function updateNow() {
		updateError = '';
		try {
			status = await api.post<DdnsStatus>('/network/ddns/update', {});
			setTimeout(() => load(false), 3000);
		} catch (err) {
			updateError = errorText(err instanceof ApiError ? err.code : 'internal');
		}
	}

	function addHost(name: string) {
		if (!hostnames.includes(name)) names = [...hostnames, name].join('\n');
	}

	const badge = $derived.by((): { text: MessageKey; tone: string } => {
		if (!status?.settings.enabled) return { text: 'ddns.state.off', tone: '' };
		if (status.lastError) return { text: 'ddns.state.error', tone: 'warn' };
		if (status.lastUpdate || status.publishedIPv4 || status.publishedIPv6) return { text: 'ddns.state.ok', tone: 'ok' };
		return { text: 'ddns.state.waiting', tone: '' };
	});
	const when = (value?: string) => (value ? formatDateTime(new Date(value)) : '–');
</script>

<PageHead title={t('ddns.title')} parent={{ href: '/settings', label: t('nav.settings') }} />

{#if loadError}
	<div class="notice-error" role="alert">{loadError}<button class="btn" onclick={() => load()}>{t('ui.retry')}</button></div>
{:else if !status}
	<p class="muted" role="status">{t('common.loading')}</p>
{:else}
	<section class="tile panel" aria-labelledby="ddns-status">
		<h2 class="tile-title" id="ddns-status" use:revealTarget>{t('ddns.statusTitle')}</h2>
		<div class="head"><span class="chip {badge.tone}">{t(badge.text)}</span></div>
		{#if status.disabled}<p class="notice-warn">{t('ddns.devMode')}</p>{/if}
		<p class="muted">{t('ddns.intro')}</p>
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
			<button type="button" class="btn" disabled={!status.settings.enabled || status.running || status.disabled} onclick={updateNow}
				>{t('ddns.updateNow')}</button
			>
			<button type="button" class="btn" onclick={() => load(false)}>{t('ddns.refresh')}</button>
		</div>
		{#if updateError}<p class="notice-error" role="alert">{updateError}</p>{/if}
	</section>

	<form class="tile panel" aria-labelledby="ddns-settings" onsubmit={save}>
		<h2 class="tile-title" id="ddns-settings" use:revealTarget>{t('ddns.settingsTitle')}</h2>
		<label class="check"><input type="checkbox" bind:checked={enabled} disabled={busy} /><span>{t('ddns.enable')}</span></label>

		<div class="divided">
			<h3>{t('dnsProvider.title')}</h3>
			<DnsProviderFields
				bind:form={provider}
				bind:cleared
				stored={status.settings.provider}
				secrets={status.secrets}
				purpose="address"
				{missing}
				disabled={busy}
				id="ddns-provider"
			/>
		</div>

		<label class="stack"
			><span>{t('ddns.hostnames')}</span><textarea
				class="field mono"
				rows="3"
				bind:value={names}
				placeholder="nas.example.org"
				spellcheck="false"
				autocapitalize="off"
				disabled={busy}
			></textarea></label
		>
		<p class="muted hint">{t('ddns.hostnamesHint')}</p>
		{#if badNames.length}<p class="notice-warn" role="alert">{t('ddns.hostnamesInvalid', { names: badNames.join(', ') })}</p>{/if}
		{#if hostnames.length > 8}<p class="notice-warn" role="alert">{t('ddns.hostnamesTooMany')}</p>{/if}

		<div class="divided">
			<label class="check"><input type="checkbox" bind:checked={ipv4} disabled={busy} /><span>{t('ddns.ipv4')}</span></label>
			<p class="muted hint">{t('ddns.ipv4Hint')}</p>
			<label class="check"><input type="checkbox" bind:checked={ipv6} disabled={busy} /><span>{t('ddns.ipv6')}</span></label>
			<p class="muted hint">{t('ddns.ipv6Hint')}</p>
			<label class="stack"
				><span>{t('ddns.interval')}</span><select class="field short" bind:value={interval} disabled={busy}>
					<option value={0}>{t('ddns.intervalDefault')}</option>
					{#each intervals as minutes (minutes)}<option value={minutes}>{t('ddns.minutes', { count: minutes })}</option>{/each}
				</select></label
			>
			<label class="stack"
				><span>{t('ddns.lookup')} <span class="muted">({t('dnsProvider.optional')})</span></span><input
					class="field mono"
					type="url"
					bind:value={lookupURL}
					placeholder="https://api.ipify.org"
					spellcheck="false"
					disabled={busy}
				/></label
			>
			<p class="muted hint">{t('ddns.lookupHint')}</p>
			<label class="stack"
				><span>{t('ddns.interface')} <span class="muted">({t('dnsProvider.optional')})</span></span><input
					class="field mono short"
					type="text"
					bind:value={iface}
					placeholder={t('ddns.interfaceAuto')}
					spellcheck="false"
					autocapitalize="off"
					disabled={busy}
				/></label
			>
		</div>

		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="row-actions">
			<button class="btn btn-primary" disabled={busy || !valid}>{t('common.save')}</button>
			{#if busy || saved}<span class="muted" role="status">{t(busy ? 'ui.saving' : 'ui.saved')}</span>{/if}
		</div>
	</form>

	{#if provider.type === 'ipv64'}
		<Ipv64Panel {status} apiKey={provider.token ?? ''} hostnames={hostnames} onhost={addHost} onstatus={(next) => (status = next)} />
	{/if}
{/if}

<style>
	section,
	form {
		font-size: 0.875rem;
	}
	.head {
		display: flex;
		align-items: center;
		gap: 12px;
	}
	.hint {
		margin: 0;
	}
	h3 {
		margin: 0;
		font-size: 0.9375rem;
	}
	.divided {
		display: flex;
		flex-direction: column;
		gap: 10px;
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
	textarea {
		height: auto;
		padding: 10px 12px;
		max-width: 32rem;
		resize: vertical;
	}
	input[type='url'] {
		max-width: 32rem;
	}
	.short {
		max-width: 14rem;
	}
	pre {
		margin: 6px 0 0;
		font-family: var(--font-mono);
		font-size: 0.75rem;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
</style>
