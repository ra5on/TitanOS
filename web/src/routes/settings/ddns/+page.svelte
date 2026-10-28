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
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { revealTarget } from '#lib/reveal-target.ts';
	import DdnsStatusPanel from '#lib/ddns/DdnsStatus.svelte';
	import UpdaterFields from '#lib/ddns/UpdaterFields.svelte';
	import { MAX_HOSTS, updaterForm, updaterPayload, type DdnsStatus, type UpdaterForm } from '#lib/ddns/ddns.ts';

	let status = $state<DdnsStatus | null>(null);
	let loadError = $state('');
	let busy = $state(false);
	let error = $state('');
	let saved = $state(false);

	let enabled = $state(false);
	let names = $state('');
	let updater = $state<UpdaterForm>({ ipv4: true, ipv6: true, interval: 0, lookupURL: '', iface: '' });
	let provider = $state<DnsProvider>({ type: '' });
	let cleared = $state<SecretName[]>([]);

	function fill(s: DdnsStatus) {
		const set = s.settings;
		enabled = set.enabled;
		names = set.hostnames.join('\n');
		updater = updaterForm(set);
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
			hostnames.length <= MAX_HOSTS &&
			(!enabled || (hostnames.length > 0 && (updater.ipv4 || updater.ipv6) && !!provider.type && missing.length === 0)),
	);

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
				...updaterPayload(updater),
				provider: providerPayload(provider, 'address'),
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
</script>

<PageHead title={t('ddns.title')} parent={{ href: '/settings', label: t('nav.settings') }} />

{#if loadError}
	<div class="notice-error" role="alert">{loadError}<button class="btn" onclick={() => load()}>{t('ui.retry')}</button></div>
{:else if !status}
	<p class="muted loading" role="status">{t('common.loading')}</p>
{:else}
	<DdnsStatusPanel {status} intro={t('ddns.intro')} onstatus={(next) => (status = next)} onrefresh={() => load(false)} />

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
		{#if hostnames.length > MAX_HOSTS}<p class="notice-warn" role="alert">{t('ddns.hostnamesTooMany')}</p>{/if}

		<UpdaterFields bind:form={updater} disabled={busy} />

		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="row-actions">
			<button class="btn btn-primary" disabled={busy || !valid}>{t('common.save')}</button>
			{#if busy || saved}<span class="muted" role="status">{t(busy ? 'ui.saving' : 'ui.saved')}</span>{/if}
		</div>
	</form>
{/if}

<style>
	form {
		font-size: 0.875rem;
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
	textarea {
		height: auto;
		padding: 10px 12px;
		max-width: 32rem;
		resize: vertical;
	}
</style>
