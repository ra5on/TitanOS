<script lang="ts">
	// Certificates from Let's Encrypt (backend package letsencrypt): names,
	// how they are proven (DNS provider or port 80), state of the renewal.
	import { poll } from '#lib/poll.ts';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
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
	import { apiErrorText, errorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';

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
	type CertInfo = {
		names: string[];
		issuer: string;
		notBefore: string;
		notAfter: string;
		fingerprint: string;
	};
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
			loadError = apiErrorText(err);
		}
	}
	// A link to a group of this page (#le-settings …): its content arrives after the
	// browser's own anchor scroll, so bring it into view then.
	const loaded = $derived(!!status);
	$effect(() => {
		const id = page.url.hash.slice(1);
		if (!loaded || !id) return;
		requestAnimationFrame(() => document.getElementById(id)?.scrollIntoView({ block: 'start' }));
	});
	onMount(() => {
		load();
	});
	// While a certificate is being fetched the page follows along.
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

	const domains = $derived(parseNames(names));
	const wildcardAllowed = $derived(challenge === 'dns-01');
	const badNames = $derived(domains.filter((name) => !validHostName(name, wildcardAllowed)));
	const missing = $derived(
		challenge === 'dns-01' && status
			? missingFields(provider, status.settings.provider, status.secrets, cleared)
			: [],
	);
	const propagationValue = $derived(propagation ?? 0);
	const propagationBad = $derived(
		!Number.isInteger(propagationValue) || propagationValue < 0 || propagationValue > 3600,
	);
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
			error =
				err instanceof ApiError && err.code === 'invalid_letsencrypt'
					? `${errorText(err.code)} ${err.message.replace(/^letsencrypt: invalid settings: /, '')}`
					: apiErrorText(err);
		} finally {
			busy = false;
		}
	}

	async function renew() {
		renewError = '';
		try {
			status = await api.post<Status>('/system/letsencrypt/renew', {});
		} catch (err) {
			renewError = apiErrorText(err);
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
		status?.certificate
			? Math.floor((Date.parse(status.certificate.notAfter) - Date.now()) / 86_400_000)
			: 0,
	);
</script>

<PageHead title={t('letsencrypt.title')} />

{#if loadError}
	<div class="notice-error" role="alert">
		{loadError}<button class="btn" onclick={() => load()}>{t('ui.retry')}</button>
	</div>
{:else if !status}
	<p class="muted loading" role="status">{t('common.loading')}</p>
{:else}
	<!-- PLAN 78-1: the state is one row that stands open – its chip at the
	     right, the certificate, the last attempts and the buttons as parts. -->
	<SettingGroup title={t('letsencrypt.statusTitle')} id="le-status" desc={t('letsencrypt.intro')}>
		{#if status.disabled}<p class="notice-warn">{t('letsencrypt.devMode')}</p>{/if}
		<SettingRow id="le-cert" icon="lock" title={t('letsencrypt.certRow')} open>
			{#snippet control()}
				{#if status!.running && status!.phase}<span class="muted" role="status"
						>{t(`letsencrypt.phase.${status!.phase}` as MessageKey)}</span
					>{/if}
				<span class="chip {badge.tone}">{t(badge.text)}</span>
			{/snippet}
			<div class="srow-block">
				{#if status.certificate}
					{#if status.certificateStaging}<p class="notice-warn">
							{t('letsencrypt.stagingCert')}
						</p>{/if}
					{#if daysLeft < 14}<p class="notice-warn">
							{t('letsencrypt.expiring', { days: Math.max(daysLeft, 0) })}
						</p>{/if}
					<dl>
						<div>
							<dt>{t('access.certIssuer')}</dt>
							<dd>{status.certificate.issuer || '–'}</dd>
						</div>
						<div>
							<dt>{t('access.certUntil')}</dt>
							<dd>{formatDateTime(new Date(status.certificate.notAfter))}</dd>
						</div>
						{#if status.renewAt}<div>
								<dt>{t('letsencrypt.renewAt')}</dt>
								<dd>{formatDateTime(new Date(status.renewAt))}</dd>
							</div>{/if}
						<div class="wide">
							<dt>{t('access.certNames')}</dt>
							<dd class="mono">{status.certificate.names.join(', ')}</dd>
						</div>
					</dl>
				{:else if status.settings.enabled}
					<p class="muted">{t('letsencrypt.noCert')}</p>
				{/if}
				{#if status.lastError}
					<div class="notice-error" role="alert">
						{t('letsencrypt.lastError', { count: status.failures })}
						<pre>{status.lastError}</pre>
						{#if status.nextAttempt}<p>
								{t('letsencrypt.nextAttempt', {
									time: formatDateTime(new Date(status.nextAttempt)),
								})}
							</p>{/if}
					</div>
				{/if}
				<dl>
					{#if status.lastAttempt}<div>
							<dt>{t('letsencrypt.lastAttempt')}</dt>
							<dd>{formatDateTime(new Date(status.lastAttempt))}</dd>
						</div>{/if}
					{#if status.lastSuccess}<div>
							<dt>{t('letsencrypt.lastSuccess')}</dt>
							<dd>{formatDateTime(new Date(status.lastSuccess))}</dd>
						</div>{/if}
				</dl>
			</div>
			<div class="srow-block actions">
				<button
					type="button"
					class="btn"
					disabled={!status.settings.enabled || status.running || status.disabled}
					onclick={renew}>{t('letsencrypt.renewNow')}</button
				>
				<a class="btn" href="/settings/access#access-cert">{t('letsencrypt.toAccess')}</a>
			</div>
		</SettingRow>
		{#if renewError}<p class="notice-error" role="alert">{renewError}</p>{/if}
	</SettingGroup>

	<!-- The settings are rows; one Save for all of them (one request, and
	     names and sign-in are typed text). -->
	<form class="rows" onsubmit={save}>
		<SettingGroup title={t('letsencrypt.settingsTitle')} id="le-settings">
			<SettingRow
				icon="power"
				title={t('letsencrypt.enable')}
				desc={t('letsencrypt.enableHint')}
				for="le-enabled"
			>
				{#snippet control()}
					<input
						id="le-enabled"
						class="switch"
						type="checkbox"
						role="switch"
						bind:checked={enabled}
						disabled={busy}
					/>
				{/snippet}
			</SettingRow>
			<SettingRow
				icon="list"
				title={t('letsencrypt.names')}
				desc={t('letsencrypt.namesHint')}
				for="le-names"
			>
				{#snippet control()}
					<textarea
						id="le-names"
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
					{t(wildcardAllowed ? 'letsencrypt.namesInvalid' : 'letsencrypt.namesInvalidHttp', {
						names: badNames.join(', '),
					})}
				</p>{/if}

			<SettingRow
				id="le-challenge"
				icon="shield"
				title={t('letsencrypt.challenge')}
				summary={t(
					challenge === 'dns-01'
						? 'letsencrypt.challengeDnsShort'
						: 'letsencrypt.challengeHttpShort',
				)}
				open
			>
				<div class="srow-block">
					<fieldset class="choice">
						<legend class="sr-only">{t('letsencrypt.challenge')}</legend>
						<label class="check"
							><input
								type="radio"
								name="challenge"
								value="dns-01"
								bind:group={challenge}
								disabled={busy}
							/><span>{t('letsencrypt.challengeDns')}</span></label
						>
						<p class="muted hint">{t('letsencrypt.challengeDnsHint')}</p>
						<label class="check"
							><input
								type="radio"
								name="challenge"
								value="http-01"
								bind:group={challenge}
								disabled={busy}
							/><span>{t('letsencrypt.challengeHttp')}</span></label
						>
						<p class="muted hint">{t('letsencrypt.challengeHttpHint')}</p>
					</fieldset>
					{#if challenge === 'http-01' && !status.httpPort}<p class="notice-warn">
							{t('letsencrypt.noHttpPort')}
						</p>{/if}
					{#if challenge === 'http-01' && status.httpPort && status.firewall && !status.internetHttp}
						<div class="notice-warn" role="note" data-testid="le-firewall-http">
							<p>{t('letsencrypt.firewallHttp')}</p>
							<p>{t('letsencrypt.firewallHttpWays')}</p>
							<div class="row-actions">
								<a class="btn" href="/settings/firewall?tab=policies#firewall-policies"
									>{t('letsencrypt.toFirewall')}</a
								>
								<button
									type="button"
									class="btn"
									disabled={busy}
									onclick={() => (challenge = 'dns-01')}>{t('letsencrypt.useDns')}</button
								>
							</div>
						</div>
					{/if}
				</div>
			</SettingRow>

			{#if challenge === 'dns-01'}
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
				<SettingRow
					icon="clock"
					title={t('letsencrypt.propagation')}
					desc={t('letsencrypt.propagationHint')}
					for="le-propagation"
				>
					{#snippet control()}
						<input
							id="le-propagation"
							class="field mono"
							type="number"
							min="0"
							max="3600"
							bind:value={propagation}
							placeholder={t('letsencrypt.propagationDefault')}
							disabled={busy}
						/>
					{/snippet}
				</SettingRow>
				{#if propagationBad}<p class="notice-warn" role="alert">
						{t('letsencrypt.propagationInvalid')}
					</p>{/if}
				<SettingRow icon="box" title={t('letsencrypt.apps')} for="le-apps">
					{#snippet description()}
						{#if status!.appDomain}
							{t('letsencrypt.appsHint', { wildcard: `*.${status!.appDomain}` })}
						{:else}
							{t('letsencrypt.appsNoDomain')}
							<a href="/settings/proxy">{t('proxy.settingsTitle')}</a>
						{/if}
					{/snippet}
					{#snippet control()}
						<input
							id="le-apps"
							class="switch"
							type="checkbox"
							role="switch"
							bind:checked={apps}
							disabled={busy}
						/>
					{/snippet}
				</SettingRow>
			{/if}

			<SettingRow
				icon="send"
				title="{t('letsencrypt.email')} ({t('dnsProvider.optional')})"
				desc={t('letsencrypt.emailHint')}
				for="le-email"
			>
				{#snippet control()}
					<input
						id="le-email"
						class="field"
						type="email"
						bind:value={email}
						autocomplete="email"
						disabled={busy}
					/>
				{/snippet}
			</SettingRow>
			<SettingRow
				icon="tool"
				title={t('letsencrypt.staging')}
				desc={t('letsencrypt.stagingHint')}
				for="le-staging"
			>
				{#snippet control()}
					<input
						id="le-staging"
						class="switch"
						type="checkbox"
						role="switch"
						bind:checked={staging}
						disabled={busy || !!directory.trim()}
					/>
				{/snippet}
			</SettingRow>
			<SettingRow
				icon="external"
				title="{t('letsencrypt.directory')} ({t('dnsProvider.optional')})"
				desc={t('letsencrypt.directoryHint')}
				for="le-directory"
			>
				{#snippet control()}
					<input
						id="le-directory"
						class="field mono"
						type="url"
						bind:value={directory}
						placeholder="https://acme-v02.api.letsencrypt.org/directory"
						spellcheck="false"
						disabled={busy}
					/>
				{/snippet}
			</SettingRow>

			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
			<div class="row-actions">
				<button class="btn btn-primary" disabled={busy || !valid}>{t('common.save')}</button>
				{#if busy || saved}<span class="muted" role="status"
						>{t(busy ? 'ui.saving' : 'ui.saved')}</span
					>{/if}
			</div>
		</SettingGroup>
	</form>
{/if}

<style>
	form.rows {
		display: contents;
	}
	.row-actions {
		margin-top: var(--space-2);
	}
	.hint {
		margin: 0;
	}
	.choice {
		border: 0;
		padding: 0;
		margin: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		min-width: 0;
	}
	label.check {
		display: flex;
		align-items: flex-start;
		gap: var(--space-2);
		font-size: var(--text-md);
	}
	.check input[type='radio'] {
		width: 18px;
		height: 18px;
		margin: 2px 0 0;
		flex: none;
	}
	.choice .hint {
		padding-left: calc(18px + var(--space-2));
		font-size: var(--text-sm);
	}
	dl {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
		gap: var(--space-3) var(--space-5);
		margin: 0;
		font-size: var(--text-md);
	}
	.wide {
		grid-column: 1 / -1;
	}
	dt {
		color: var(--muted);
		font-size: var(--text-sm);
	}
	dd {
		margin: 2px 0 0;
		overflow-wrap: anywhere;
	}
	textarea {
		height: auto;
		padding: var(--space-2-5) var(--space-3);
		width: 20rem;
		max-width: 100%;
		resize: vertical;
	}
	/* Their placeholders are longer than the usual field of a row. */
	#le-propagation {
		width: 13rem;
	}
	#le-directory {
		width: 26rem;
		max-width: 100%;
	}
	.notice-warn p {
		margin: 0 0 var(--space-1-5);
	}
	pre {
		margin: var(--space-1-5) 0 0;
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
</style>
