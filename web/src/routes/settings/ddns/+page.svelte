<script lang="ts">
	// Dynamic DNS (backend package ddns): host names at the public IPv4
	// address of the connection and the global IPv6 address of the NAS.
	import { poll } from '#lib/poll.ts';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError } from '#lib/api.ts';
	import Tabs from '#lib/components/Tabs.svelte';
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
	import { apiErrorText, errorText, t } from '#lib/i18n/index.svelte.ts';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import DdnsStatusPanel from '#lib/ddns/DdnsStatus.svelte';
	import UpdaterFields from '#lib/ddns/UpdaterFields.svelte';
	import {
		MAX_HOSTS,
		updaterForm,
		updaterPayload,
		type DdnsStatus,
		type UpdaterForm,
	} from '#lib/ddns/ddns.ts';
	import Ipv64Panel from '#lib/ipv64/Ipv64Panel.svelte';
	import Ipv64Area from '#lib/ipv64/Ipv64Area.svelte';
	import { DDNS_TABS, ddnsTabFrom, type DdnsTab } from '#lib/ipv64/ipv64.ts';

	// Two tabs (decision of the owner of 29 Oct 2026): dynamic DNS for every
	// provider and the IPv64.net account (?tab=ipv64, sub-tab in ?sub=).
	let tab = $state<DdnsTab>(ddnsTabFrom(page.url.searchParams.get('tab')));
	$effect(() => {
		tab = ddnsTabFrom(page.url.searchParams.get('tab'));
	});
	function chooseTab(next: DdnsTab) {
		tab = next;
		goto(next === 'ipv64' ? '/settings/ddns?tab=ipv64' : '/settings/ddns', { replaceState: true });
	}

	let status = $state<DdnsStatus | null>(null);
	let loadError = $state('');
	let busy = $state(false);
	let error = $state('');
	let saved = $state(false);

	let enabled = $state(false);
	let names = $state('');
	let updater = $state<UpdaterForm>({
		ipv4: true,
		ipv6: true,
		interval: 0,
		lookupURL: '',
		iface: '',
	});
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
			loadError = apiErrorText(err);
		}
	}
	onMount(() => {
		load();
	});
	// A link to a group of this page (#ddns-settings …): its content arrives after the
	// browser's own anchor scroll, so bring it into view then.
	const loaded = $derived(!!status);
	$effect(() => {
		const id = page.url.hash.slice(1);
		if (!loaded || !id) return;
		requestAnimationFrame(() => document.getElementById(id)?.scrollIntoView({ block: 'start' }));
	});
	$effect(() => {
		if (!status?.running) return;
		return poll(() => load(false), { interval: 3000, immediate: false });
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
		status
			? missingFields(provider, status.settings.provider, status.secrets, cleared, 'address')
			: [],
	);
	const valid = $derived(
		badNames.length === 0 &&
			hostnames.length <= MAX_HOSTS &&
			(!enabled ||
				(hostnames.length > 0 &&
					(updater.ipv4 || updater.ipv6) &&
					!!provider.type &&
					missing.length === 0)),
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
					: apiErrorText(err);
		} finally {
			busy = false;
		}
	}

	function addHost(name: string) {
		if (!hostnames.includes(name)) names = [...hostnames, name].join('\n');
	}
</script>

<PageHead title={t('ddns.title')} />

<Tabs
	tabs={DDNS_TABS.map((id) => ({ id, label: t(id === 'ipv64' ? 'ipv64x.title' : 'ddns.tabDdns') }))}
	value={tab}
	label={t('ddns.title')}
	onchange={chooseTab}
/>

{#if tab === 'ipv64'}
	<Ipv64Area />
{:else if loadError}
	<div class="notice-error" role="alert">
		{loadError}<button class="btn" onclick={() => load()}>{t('ui.retry')}</button>
	</div>
{:else if !status}
	<p class="muted loading" role="status">{t('common.loading')}</p>
{:else}
	<DdnsStatusPanel
		{status}
		intro={t('ddns.intro')}
		onstatus={(next) => (status = next)}
		onrefresh={() => load(false)}
	/>

	<!-- PLAN 78-1: the settings are rows; one Save for all of them (one
	     request, and names and sign-in are typed text). -->
	<form class="rows" onsubmit={save}>
		<SettingGroup title={t('ddns.settingsTitle')} id="ddns-settings">
			<SettingRow icon="power" title={t('ddns.enable')} for="ddns-enabled">
				{#snippet control()}
					<input
						id="ddns-enabled"
						class="switch"
						type="checkbox"
						role="switch"
						bind:checked={enabled}
						disabled={busy}
					/>
				{/snippet}
			</SettingRow>
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
			<SettingRow
				icon="list"
				title={t('ddns.hostnames')}
				desc={t('ddns.hostnamesHint')}
				for="ddns-names"
			>
				{#snippet control()}
					<textarea
						id="ddns-names"
						class="field mono"
						rows="3"
						bind:value={names}
						placeholder="nas.example.org"
						spellcheck="false"
						autocapitalize="off"
						disabled={busy}></textarea>
				{/snippet}
			</SettingRow>
			{#if badNames.length}<p class="notice-warn" role="alert">
					{t('ddns.hostnamesInvalid', { names: badNames.join(', ') })}
				</p>{/if}
			{#if hostnames.length > MAX_HOSTS}<p class="notice-warn" role="alert">
					{t('ddns.hostnamesTooMany')}
				</p>{/if}
			<UpdaterFields bind:form={updater} disabled={busy} />
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
			<div class="row-actions">
				<button class="btn btn-primary" disabled={busy || !valid}>{t('common.save')}</button>
				{#if busy || saved}<span class="muted" role="status"
						>{t(busy ? 'ui.saving' : 'ui.saved')}</span
					>{/if}
			</div>
		</SettingGroup>
	</form>

	{#if provider.type === 'ipv64'}
		<Ipv64Panel
			{status}
			apiKey={provider.token ?? ''}
			{hostnames}
			onhost={addHost}
			onstatus={(next) => (status = next)}
		/>
	{/if}
{/if}

<style>
	form.rows {
		display: contents;
	}
	.row-actions {
		margin-top: var(--space-2);
	}
	textarea {
		height: auto;
		padding: var(--space-2-5) var(--space-3);
		width: 20rem;
		max-width: 100%;
		resize: vertical;
	}
</style>
