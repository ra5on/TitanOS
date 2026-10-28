<script lang="ts">
	// Certificates from Let's Encrypt (backend package letsencrypt): names,
	// how they are proven (DNS provider or port 80), state of the renewal.
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

	type Settings = {
		enabled: boolean;
		email?: string;
		staging?: boolean;
		domains: string[];
		apps?: boolean;
		challenge: 'dns-01' | 'http-01';
		provider: DnsProvider;
		propagation?: number;
		directory?: string;
	};
	type CertInfo = { names: string[]; issuer: string; notBefore: string; notAfter: string; fingerprint: string };
	type Status = {
		settings: Settings;
		secrets: Partial<Record<SecretName, boolean>>;
		names: string[];
		appDomain: string;
		certificate?: CertInfo;
		certificateStaging?: boolean;
		running: boolean;
		phase?: string;
		lastAttempt?: string;
		lastSuccess?: string;
		lastError?: string;
		failures: number;
		nextAttempt?: string;
		renewAt?: string;
		httpPort: boolean;
		/** The firewall is on, and whether it lets anyone reach port 80 from the internet. */
		firewall?: boolean;
		internetHttp?: boolean;
		disabled?: boolean;
	};

	let status = $state<Status | null>(null);
	let loadError = $state('');
	let busy = $state(false);
	let error = $state('');
	let saved = $state(false);
	let renewError = $state('');

	let enabled = $state(false);
	let names = $state('');
	let apps = $state(false);
	let challenge = $state<'dns-01' | 'http-01'>('dns-01');
	let provider = $state<DnsProvider>({ type: '' });
	let cleared = $state<SecretName[]>([]);
	let email = $state('');
	let staging = $state(false);
	let propagation = $state<number | null>(null);
	let directory = $state('');

	function fill(s: Status) {
		const set = s.settings;
		enabled = set.enabled;
		names = set.domains.join('\n');
		apps = !!set.apps;
		challenge = set.challenge || 'dns-01';
		provider = providerForm(set.provider);
		cleared = [];
		email = set.email ?? '';
		staging = !!set.staging;
		propagation = set.propagation || null;
		directory = set.directory ?? '';
	}

	async function load(refill = true) {
		try {
			const next = await api.get<Status>('/system/letsencrypt');
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
	// While a certificate is being fetched the page follows along.
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

	const domains = $derived(parseNames(names));
	const wildcardAllowed = $derived(challenge === 'dns-01');
	const badNames = $derived(domains.filter((name) => !validHostName(name, wildcardAllowed)));
	const missing = $derived(
		challenge === 'dns-01' && status
			? missingFields(provider, status.settings.provider, status.secrets, cleared)
			: [],
	);
	const propagationValue = $derived(propagation ?? 0);
	const propagationBad = $derived(!Number.isInteger(propagationValue) || propagationValue < 0 || propagationValue > 3600);
	const valid = $derived(
		badNames.length === 0 &&
			(!enabled || domains.length > 0) &&
			(challenge !== 'dns-01' || (!!provider.type && missing.length === 0)) &&
			!propagationBad,
	);

	async function save(event: SubmitEvent) {
		event.preventDefault();
		if (busy || !valid) return;
		busy = true;
		error = '';
		saved = false;
		try {
			const body = {
				enabled,
				email: email.trim(),
				staging,
				domains,
				apps: challenge === 'dns-01' && apps,
				challenge,
				provider: challenge === 'dns-01' ? providerPayload(provider) : { type: '' },
				propagation: propagationValue,
				directory: directory.trim(),
				clearSecrets: cleared,
			};
			const next = await api.put<Status>('/system/letsencrypt', body);
			status = next;
			fill(next);
			saved = true;
		} catch (err) {
			error = err instanceof ApiError && err.code === 'invalid_letsencrypt' ? `${errorText(err.code)} ${err.message.replace(/^letsencrypt: invalid settings: /, '')}` : errorText(err instanceof ApiError ? err.code : 'internal');
		} finally {
			busy = false;
		}
	}

	async function renew() {
		renewError = '';
		try {
			status = await api.post<Status>('/system/letsencrypt/renew', {});
		} catch (err) {
			renewError = errorText(err instanceof ApiError ? err.code : 'internal');
		}
	}

	const badge = $derived.by((): { text: MessageKey; tone: string } => {
		if (!status?.settings.enabled) return { text: 'letsencrypt.state.off', tone: '' };
		if (status.running) return { text: 'letsencrypt.state.running', tone: 'info' };
		if (status.lastError) return { text: 'letsencrypt.state.error', tone: 'warn' };
		if (status.certificate) return { text: 'letsencrypt.state.ok', tone: 'ok' };
		return { text: 'letsencrypt.state.waiting', tone: '' };
	});
	const daysLeft = $derived(
		status?.certificate ? Math.floor((Date.parse(status.certificate.notAfter) - Date.now()) / 86_400_000) : 0,
	);
</script>

<PageHead title={t('letsencrypt.title')} parent={{ href: '/settings', label: t('nav.settings') }} />

{#if loadError}
	<div class="notice-error" role="alert">{loadError}<button class="btn" onclick={() => load()}>{t('ui.retry')}</button></div>
{:else if !status}
	<p class="muted loading" role="status">{t('common.loading')}</p>
{:else}
	<section class="tile panel" aria-labelledby="le-status">
		<h2 class="tile-title" id="le-status" use:revealTarget>{t('letsencrypt.statusTitle')}</h2>
		<div class="head">
			<span class="chip {badge.tone}">{t(badge.text)}</span>
			{#if status.running && status.phase}<span class="muted" role="status"
					>{t(`letsencrypt.phase.${status.phase}` as MessageKey)}</span
				>{/if}
		</div>
		{#if status.disabled}<p class="notice-warn">{t('letsencrypt.devMode')}</p>{/if}
		<p class="muted">{t('letsencrypt.intro')}</p>
		{#if status.certificate}
			{#if status.certificateStaging}<p class="notice-warn">{t('letsencrypt.stagingCert')}</p>{/if}
			{#if daysLeft < 14}<p class="notice-warn">{t('letsencrypt.expiring', { days: Math.max(daysLeft, 0) })}</p>{/if}
			<dl>
				<div><dt>{t('access.certIssuer')}</dt><dd>{status.certificate.issuer || '–'}</dd></div>
				<div><dt>{t('access.certUntil')}</dt><dd>{formatDateTime(new Date(status.certificate.notAfter))}</dd></div>
				{#if status.renewAt}<div><dt>{t('letsencrypt.renewAt')}</dt><dd>{formatDateTime(new Date(status.renewAt))}</dd></div>{/if}
				<div class="wide"><dt>{t('access.certNames')}</dt><dd class="mono">{status.certificate.names.join(', ')}</dd></div>
			</dl>
		{:else if status.settings.enabled}
			<p class="muted">{t('letsencrypt.noCert')}</p>
		{/if}
		{#if status.lastError}
			<div class="notice-error" role="alert">
				{t('letsencrypt.lastError', { count: status.failures })}
				<pre>{status.lastError}</pre>
				{#if status.nextAttempt}<p>{t('letsencrypt.nextAttempt', { time: formatDateTime(new Date(status.nextAttempt)) })}</p>{/if}
			</div>
		{/if}
		<dl>
			{#if status.lastAttempt}<div><dt>{t('letsencrypt.lastAttempt')}</dt><dd>{formatDateTime(new Date(status.lastAttempt))}</dd></div>{/if}
			{#if status.lastSuccess}<div><dt>{t('letsencrypt.lastSuccess')}</dt><dd>{formatDateTime(new Date(status.lastSuccess))}</dd></div>{/if}
		</dl>
		<div class="row-actions">
			<button type="button" class="btn" disabled={!status.settings.enabled || status.running || status.disabled} onclick={renew}
				>{t('letsencrypt.renewNow')}</button
			>
			<a class="btn" href="/settings/access#access-cert">{t('letsencrypt.toAccess')}</a>
		</div>
		{#if renewError}<p class="notice-error" role="alert">{renewError}</p>{/if}
	</section>

	<form class="tile panel" aria-labelledby="le-settings" onsubmit={save}>
		<h2 class="tile-title" id="le-settings" use:revealTarget>{t('letsencrypt.settingsTitle')}</h2>
		<label class="check"><input type="checkbox" bind:checked={enabled} disabled={busy} /><span>{t('letsencrypt.enable')}</span></label>
		<p class="muted hint">{t('letsencrypt.enableHint')}</p>

		<label class="stack"
			><span>{t('letsencrypt.names')}</span><textarea
				class="field mono"
				rows="3"
				bind:value={names}
				placeholder="nas.example.org"
				spellcheck="false"
				autocapitalize="off"
				disabled={busy}
			></textarea></label
		>
		<p class="muted hint">{t('letsencrypt.namesHint')}</p>
		{#if badNames.length}<p class="notice-warn" role="alert">
				{t(wildcardAllowed ? 'letsencrypt.namesInvalid' : 'letsencrypt.namesInvalidHttp', { names: badNames.join(', ') })}
			</p>{/if}

		<div class="divided">
			<h3 id="le-challenge">{t('letsencrypt.challenge')}</h3>
			<label class="check"
				><input type="radio" name="challenge" value="dns-01" bind:group={challenge} disabled={busy} /><span
					>{t('letsencrypt.challengeDns')}</span
				></label
			>
			<p class="muted hint">{t('letsencrypt.challengeDnsHint')}</p>
			<label class="check"
				><input type="radio" name="challenge" value="http-01" bind:group={challenge} disabled={busy} /><span
					>{t('letsencrypt.challengeHttp')}</span
				></label
			>
			<p class="muted hint">{t('letsencrypt.challengeHttpHint')}</p>
			{#if challenge === 'http-01' && !status.httpPort}<p class="notice-warn">{t('letsencrypt.noHttpPort')}</p>{/if}
			{#if challenge === 'http-01' && status.httpPort && status.firewall && !status.internetHttp}
				<div class="notice-warn" role="note" data-testid="le-firewall-http">
					<p>{t('letsencrypt.firewallHttp')}</p>
					<p>{t('letsencrypt.firewallHttpWays')}</p>
					<div class="row-actions">
						<a class="btn" href="/settings/firewall?tab=policies#firewall-policies">{t('letsencrypt.toFirewall')}</a>
						<button type="button" class="btn" disabled={busy} onclick={() => (challenge = 'dns-01')}
							>{t('letsencrypt.useDns')}</button
						>
					</div>
				</div>
			{/if}
		</div>

		{#if challenge === 'dns-01'}
			<div class="divided">
				<h3 id="le-provider">{t('dnsProvider.title')}</h3>
				<DnsProviderFields
					bind:form={provider}
					bind:cleared
					stored={status.settings.provider}
					secrets={status.secrets}
					purpose="challenge"
					{missing}
					disabled={busy}
					id="le-provider"
				/>
				<label class="stack"
					><span>{t('letsencrypt.propagation')}</span><input
						class="field mono short"
						type="number"
						min="0"
						max="3600"
						bind:value={propagation}
						placeholder={t('letsencrypt.propagationDefault')}
						disabled={busy}
					/></label
				>
				<p class="muted hint">{t('letsencrypt.propagationHint')}</p>
				{#if propagationBad}<p class="notice-warn" role="alert">{t('letsencrypt.propagationInvalid')}</p>{/if}
			</div>
			<div class="divided">
				<label class="check"><input type="checkbox" bind:checked={apps} disabled={busy} /><span>{t('letsencrypt.apps')}</span></label>
				{#if status.appDomain}
					<p class="muted hint">{t('letsencrypt.appsHint', { wildcard: `*.${status.appDomain}` })}</p>
				{:else}
					<p class="muted hint">{t('letsencrypt.appsNoDomain')} <a href="/settings/proxy">{t('proxy.settingsTitle')}</a></p>
				{/if}
			</div>
		{/if}

		<div class="divided">
			<label class="stack"
				><span>{t('letsencrypt.email')} <span class="muted">({t('dnsProvider.optional')})</span></span><input
					class="field"
					type="email"
					bind:value={email}
					autocomplete="email"
					disabled={busy}
				/></label
			>
			<p class="muted hint">{t('letsencrypt.emailHint')}</p>
			<label class="check"><input type="checkbox" bind:checked={staging} disabled={busy || !!directory.trim()} /><span>{t('letsencrypt.staging')}</span></label>
			<p class="muted hint">{t('letsencrypt.stagingHint')}</p>
			<label class="stack"
				><span>{t('letsencrypt.directory')} <span class="muted">({t('dnsProvider.optional')})</span></span><input
					class="field mono"
					type="url"
					bind:value={directory}
					placeholder="https://acme-v02.api.letsencrypt.org/directory"
					spellcheck="false"
					disabled={busy}
				/></label
			>
			<p class="muted hint">{t('letsencrypt.directoryHint')}</p>
		</div>

		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="row-actions">
			<button class="btn btn-primary" disabled={busy || !valid}>{t('common.save')}</button>
			{#if busy || saved}<span class="muted" role="status">{t(busy ? 'ui.saving' : 'ui.saved')}</span>{/if}
		</div>
	</form>
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
		grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
		gap: 12px 24px;
		margin: 0;
	}
	.wide {
		grid-column: 1 / -1;
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
	input.short {
		max-width: 12rem;
	}
	input[type='email'],
	input[type='url'] {
		max-width: 32rem;
	}
	.notice-warn p {
		margin: 0 0 6px;
	}
	pre {
		margin: 6px 0 0;
		font-family: var(--font-mono);
		font-size: 0.75rem;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
</style>
