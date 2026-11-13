<script lang="ts">
	import { poll } from '#lib/poll.ts';
	import TerminalSettings from '#lib/components/TerminalSettings.svelte';
	import WebDavSettings from '#lib/components/WebDavSettings.svelte';
	import IdleLockSettings from '#lib/components/IdleLockSettings.svelte';
	import StayLoggedInSettings from '#lib/components/StayLoggedInSettings.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import SettingSub from '#lib/components/settings/SettingSub.svelte';
	// SSH access and the certificate of the web UI.
	import { api, ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { formatDateTime } from '#lib/format.ts';
	import { apiErrorText, errorText, t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { system } from '#lib/system.svelte.ts';
	import { addNetworks, parseNetworks, sameNetworks } from '#lib/ssh-networks.ts';

	type Ssh = {
		enabled: boolean;
		running: boolean;
		fingerprint: string;
		accounts: string[];
		allowedNetworks: string[];
		networksError?: string;
		homeNetworks: string[];
		tailscaleNetworks: string[];
		netbirdNetworks: string[];
		clientAddress: string;
	};
	type Certificate = {
		kind: 'self-signed' | 'custom' | 'titanos-ca';
		names: string[];
		issuer: string;
		notBefore: string;
		notAfter: string;
		fingerprint: string;
		letsEncrypt?: { names: string[]; notAfter: string };
	};

	let ssh = $state<Ssh | null>(null);
	let certificate = $state<Certificate | null>(null);
	let noTls = $state(false);
	let error = $state<{ text: string; detail: string } | null>(null);
	let busy = $state(false);
	let replacing = $state(false);
	let removing = $state(false);
	let certPem = $state('');
	let keyPem = $state('');
	let saved = $state(false);
	let sshError = $state('');
	let sshBusy = $state(false);
	let sshSaved = $state(false);
	$effect(() => {
		if (saved) {
			const timer = setTimeout(() => (saved = false), 5000);
			return () => clearTimeout(timer);
		}
	});
	$effect(() => {
		if (sshSaved) {
			const timer = setTimeout(() => (sshSaved = false), 5000);
			return () => clearTimeout(timer);
		}
	});

	function problem(err: unknown) {
		const code = err instanceof ApiError ? err.code : 'internal';
		error = {
			text: errorText(code),
			detail:
				code === 'invalid_certificate' && err instanceof ApiError
					? err.message.replace(/^tlscert: certificate not usable: /, '')
					: '',
		};
	}

	async function loadSsh() {
		try {
			ssh = await api.get<Ssh>('/system/ssh');
			sshError = '';
		} catch (err) {
			sshError = apiErrorText(err);
		}
	}
	async function loadCertificate() {
		error = null;
		try {
			certificate = await api.get<Certificate>('/system/certificate');
		} catch (err) {
			if (err instanceof ApiError && err.code === 'read_only') noTls = true;
			else problem(err);
		}
	}
	$effect(() => {
		loadSsh();
		loadCertificate();
	});
	// The service follows the switch within a moment. Polling pauses while the
	// page is hidden and gives up after a minute with a visible note.
	const SSH_POLLS = 30;
	let sshPolls = $state(0);
	let sshStuck = $state(false);
	$effect(() => {
		if (!ssh || ssh.enabled === ssh.running || sshStuck) return;
		return poll(
			() => {
				if (++sshPolls >= SSH_POLLS) sshStuck = true;
				else return loadSsh();
			},
			{ interval: 2000, immediate: false },
		);
	});
	function retrySsh() {
		sshPolls = 0;
		sshStuck = false;
		loadSsh();
	}

	async function setSsh(enabled: boolean) {
		if (sshBusy) return;
		sshBusy = true;
		sshSaved = false;
		sshError = '';
		sshPolls = 0;
		sshStuck = false;
		try {
			ssh = await api.put<Ssh>('/system/ssh', { enabled });
			sshSaved = true;
		} catch (err) {
			sshError = apiErrorText(err);
		} finally {
			sshBusy = false;
		}
	}

	// Networks SSH logins may come from. The text is only taken over from the
	// NAS while nobody edits it.
	let netText = $state('');
	let netEdited = $state(false);
	let netBusy = $state(false);
	let netSaved = $state(false);
	let netError = $state('');
	let netConfirm = $state(false);
	$effect(() => {
		if (ssh && !netEdited) netText = ssh.allowedNetworks.join('\n');
	});
	$effect(() => {
		if (netSaved) {
			const timer = setTimeout(() => (netSaved = false), 5000);
			return () => clearTimeout(timer);
		}
	});
	const netChanged = $derived(!!ssh && !sameNetworks(parseNetworks(netText), ssh.allowedNetworks));
	function addPreset(list: string[]) {
		netText = addNetworks(netText, list);
		netEdited = true;
	}
	async function saveNetworks(confirm = false) {
		if (netBusy) return;
		netBusy = true;
		netSaved = false;
		netError = '';
		try {
			ssh = await api.put<Ssh>('/system/ssh/networks', {
				networks: parseNetworks(netText),
				confirm,
			});
			netEdited = false;
			netConfirm = false;
			netSaved = true;
			netText = ssh.allowedNetworks.join('\n');
		} catch (err) {
			if (err instanceof ApiError && err.code === 'ssh_network_excludes_you' && !confirm)
				netConfirm = true;
			else {
				netConfirm = false;
				netError = apiErrorText(err);
			}
		} finally {
			netBusy = false;
		}
	}

	async function readFile(event: Event, into: 'cert' | 'key') {
		const file = (event.currentTarget as HTMLInputElement).files?.[0];
		if (!file) return;
		const text = await file.text();
		if (into === 'cert') certPem = text;
		else keyPem = text;
	}

	async function saveCertificate(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		error = null;
		saved = false;
		try {
			certificate = await api.put<Certificate>('/system/certificate', {
				certificate: certPem,
				key: keyPem,
			});
			certPem = keyPem = '';
			replacing = false;
			saved = true;
		} catch (err) {
			problem(err);
		}
		busy = false;
	}

	async function removeCertificate() {
		if (busy) return;
		busy = true;
		error = null;
		saved = false;
		try {
			certificate = await api.delete<Certificate>('/system/certificate');
			removing = false;
			saved = true;
		} catch (err) {
			problem(err);
		}
		busy = false;
	}

	let certOpen = $state(false);
	const certKind = $derived(
		!certificate
			? ''
			: certificate.kind === 'titanos-ca'
				? t('appaccess.certKindCa')
				: t(`access.certKind.${certificate.kind}`),
	);
	const host = $derived(system.info?.network?.ipv4.split('/')[0] || system.info?.hostname || 'nas');
	const daysLeft = $derived(
		certificate ? Math.floor((Date.parse(certificate.notAfter) - Date.now()) / 86_400_000) : 0,
	);
	// Groups of four are easier to compare with what the browser shows.
	const fingerprint = $derived(certificate?.fingerprint.match(/.{1,2}/g)?.join(':') ?? '');
</script>

<PageHead title={t('access.title')} />

<!-- PLAN 78-1: every access is a row of its own; what belongs to it opens
     inside the row. The search anchors (#access-ssh, #access-cert …) are the
     rows' ids. -->
<SettingGroup title={t('access.groupRemote')}>
	{#if ssh}
		{@const s = ssh}
		<SettingRow
			id="access-ssh"
			icon="key"
			title={t('access.sshEnable')}
			desc={t('access.sshShort')}
			for="access-ssh-switch"
		>
			{#snippet control()}
				{#if sshBusy || sshSaved}<span role="status">{t(sshBusy ? 'ui.saving' : 'ui.saved')}</span
					>{/if}
				<span class="chip {s.running ? 'ok' : ''}"
					>{s.running ? t('access.sshRunning') : t('access.sshOff')}</span
				>
				<input
					id="access-ssh-switch"
					class="switch"
					type="checkbox"
					role="switch"
					checked={s.enabled}
					disabled={sshBusy}
					onchange={async (event) => {
						const field = event.currentTarget;
						await setSsh(field.checked);
						field.checked = ssh?.enabled ?? false;
					}}
				/>
			{/snippet}
			<div class="srow-block"><p class="muted">{t('access.sshText')}</p></div>
			{#if s.enabled}
				<SettingSub title={t('access.sshWho')}>
					{#snippet control()}<span class="value"
							>{s.accounts.length ? s.accounts.join(', ') : t('access.sshNobody')}</span
						>{/snippet}
				</SettingSub>
				<SettingSub title={t('access.sshCommand')}>
					{#snippet control()}<span class="value mono">ssh {session.data?.user?.name}@{host}</span
						>{/snippet}
				</SettingSub>
				<SettingSub title={t('access.sshFingerprint')}>
					{#snippet control()}<span class="value mono">{s.fingerprint || '–'}</span>{/snippet}
				</SettingSub>
				<div class="srow-block">
					{#if s.accounts.length === 0}<p class="notice-warn">
							{t('access.sshNoKeys')} <a href="/account">{t('menu.account')}</a>
						</p>{/if}
					<p class="muted">{t('access.sshNote')}</p>
				</div>
			{/if}
		</SettingRow>
	{/if}
	{#if sshError}<div class="notice-error" role="alert">
			{sshError}<button class="btn" onclick={loadSsh}>{t('ui.retry')}</button>
		</div>{:else if !ssh}<p class="loading" role="status">{t('common.loading')}</p>{/if}
	{#if sshStuck && ssh && ssh.enabled !== ssh.running}<div class="notice-warn" role="alert">
			{t(ssh.enabled ? 'access.sshNotStarted' : 'access.sshNotStopped')}
			<button class="btn" onclick={retrySsh}>{t('ui.retry')}</button>
		</div>{/if}
	{#if ssh}
		{@const s = ssh}
		<!-- The list is typed text: it keeps its own Save. -->
		<SettingRow
			id="access-ssh-networks"
			icon="globe"
			title={t('access.sshNetworks')}
			desc={t('access.sshNetworksShort')}
			summary={s.allowedNetworks.length
				? s.allowedNetworks.join(', ')
				: t('access.sshNetworksAllShort')}
		>
			<div class="srow-block">
				<p class="muted">{t('access.sshNetworksText')}</p>
				<p>
					{s.allowedNetworks.length
						? t('access.sshNetworksActive', { list: s.allowedNetworks.join(', ') })
						: t('access.sshNetworksAll')}
				</p>
				{#if s.networksError}<p class="notice-error" role="alert">
						{t('access.sshNetworksError', { detail: s.networksError })}
					</p>{/if}
				<textarea
					class="field mono networks"
					rows="4"
					aria-label={t('access.sshNetworks')}
					placeholder="192.168.1.0/24"
					spellcheck="false"
					bind:value={netText}
					oninput={() => (netEdited = true)}
					disabled={netBusy}></textarea>
			</div>
			<div class="srow-block actions">
				<button
					type="button"
					class="btn"
					disabled={netBusy || s.homeNetworks.length === 0}
					title={s.homeNetworks.length ? s.homeNetworks.join(', ') : t('access.sshNetworksNoHome')}
					onclick={() => addPreset(ssh?.homeNetworks ?? [])}>{t('access.sshNetworksHome')}</button
				>
				<button
					type="button"
					class="btn"
					disabled={netBusy}
					title={s.tailscaleNetworks.join(', ')}
					onclick={() => addPreset(ssh?.tailscaleNetworks ?? [])}
					>{t('access.sshNetworksTailscale')}</button
				>
				<button
					type="button"
					class="btn"
					disabled={netBusy}
					title={s.netbirdNetworks.join(', ')}
					onclick={() => addPreset(ssh?.netbirdNetworks ?? [])}
					>{t('access.sshNetworksNetbird')}</button
				>
				<button
					type="button"
					class="btn btn-primary"
					disabled={netBusy || !netChanged}
					onclick={() => saveNetworks()}>{t('access.sshNetworksSave')}</button
				>
				{#if netBusy || netSaved}<span role="status">{t(netBusy ? 'ui.saving' : 'ui.saved')}</span
					>{/if}
			</div>
			<div class="srow-block">
				{#if s.homeNetworks.length === 0}<p class="muted">{t('access.sshNetworksNoHome')}</p>{/if}
				{#if s.clientAddress}<p class="muted">
						{t('access.sshNetworksYou', { address: s.clientAddress })}
					</p>{/if}
				{#if netError}<p class="notice-error" role="alert">{netError}</p>{/if}
				<p class="muted">{t('access.sshNetworksRecovery')}</p>
			</div>
		</SettingRow>
	{/if}
	<WebDavSettings />
</SettingGroup>

{#snippet certParts()}
	{#if certificate}
		{@const c = certificate}
		<div class="srow-block">
			{#if c.kind === 'self-signed'}
				<p class="muted">{t('access.certSelf')}</p>
			{:else if c.kind === 'titanos-ca'}
				<p class="muted">
					{t('appaccess.certCa')}
					<a href="/settings/proxy#proxy-ca">{t('appaccess.certCaLink')}</a>
				</p>
			{:else if daysLeft < 14}
				<p class="notice-warn">{t('access.certExpiring', { days: Math.max(daysLeft, 0) })}</p>
			{/if}
			{#if c.letsEncrypt}<p class="notice-ok">
					{t('access.letsEncryptActive', {
						names: c.letsEncrypt.names.join(', '),
						date: formatDateTime(new Date(c.letsEncrypt.notAfter)),
					})}
					<a href="/settings/letsencrypt">{t('letsencrypt.title')}</a>
				</p>{:else}<p class="muted">
					<a href="/settings/letsencrypt">{t('access.letsEncryptLink')}</a>
				</p>{/if}
		</div>
		<SettingSub title={t('access.certKind')}>
			{#snippet control()}<span class="value">{certKind}</span>{/snippet}
		</SettingSub>
		<SettingSub title={t('access.certIssuer')}>
			{#snippet control()}<span class="value">{c.issuer || '–'}</span>{/snippet}
		</SettingSub>
		<SettingSub title={t('access.certUntil')}>
			{#snippet control()}<span class="value">{formatDateTime(new Date(c.notAfter))}</span
				>{/snippet}
		</SettingSub>
		<SettingSub title={t('access.certNames')}>
			{#snippet control()}<span class="value mono">{c.names.join(', ')}</span>{/snippet}
		</SettingSub>
		<SettingSub title={t('access.certFingerprint')}>
			{#snippet control()}<span class="value mono small">{fingerprint}</span>{/snippet}
		</SettingSub>
		{#if replacing}
			<form class="srow-block" onsubmit={saveCertificate}>
				<p class="muted">{t('access.certFormHint')}</p>
				<label class="stack"
					><span>{t('access.certChain')}</span>
					<textarea
						class="field mono"
						bind:value={certPem}
						rows="6"
						placeholder="-----BEGIN CERTIFICATE-----"
						spellcheck="false"
						required></textarea>
					<input
						type="file"
						accept=".pem,.crt,.cer,.txt"
						onchange={(event) => readFile(event, 'cert')}
					/>
				</label>
				<label class="stack"
					><span>{t('access.certKey')}</span>
					<textarea
						class="field mono"
						bind:value={keyPem}
						rows="6"
						placeholder="-----BEGIN PRIVATE KEY-----"
						spellcheck="false"
						autocomplete="off"
						required></textarea>
					<input type="file" accept=".pem,.key,.txt" onchange={(event) => readFile(event, 'key')} />
				</label>
				<p class="muted">{t('access.certKeyNote')}</p>
				<div class="row-actions">
					<button
						type="button"
						class="btn"
						onclick={() => ((replacing = false), (certPem = keyPem = ''), (error = null))}
						disabled={busy}>{t('common.cancel')}</button
					>
					<button type="submit" class="btn btn-primary" disabled={busy}
						>{t('access.certUse')}</button
					>
				</div>
			</form>
		{:else}
			<div class="srow-block actions">
				<button type="button" class="btn" onclick={() => ((replacing = true), (saved = false))}
					>{t(c.kind === 'custom' ? 'access.certReplace' : 'access.certOwn')}</button
				>
				{#if c.kind === 'custom'}<button
						type="button"
						class="btn"
						onclick={() => ((removing = true), (error = null))}
						disabled={busy}>{t('access.certRemove')}</button
					>{/if}
			</div>
		{/if}
	{/if}
{/snippet}

<SettingGroup title={t('access.groupWeb')}>
	{#if error}<div class="notice-error" role="alert">
			{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}<button
				class="btn"
				onclick={loadCertificate}>{t('ui.retry')}</button
			>
		</div>{/if}
	{#if saved}<p class="notice-ok" role="status">{t('access.certSaved')}</p>{/if}
	<SettingRow
		id="access-cert"
		icon="lock"
		title={t('access.cert')}
		desc={noTls ? t('access.certNone') : undefined}
		summary={certificate ? certKind : undefined}
		bind:open={certOpen}
		children={certificate && !noTls ? certParts : undefined}
	>
		{#snippet control()}
			{#if certificate?.kind === 'custom' && daysLeft < 14}<span class="chip warn"
					>{t('access.certExpiringShort')}</span
				>{/if}
		{/snippet}
	</SettingRow>
	<StayLoggedInSettings />
	<IdleLockSettings />
</SettingGroup>

{#if netConfirm && ssh}
	<Dialog
		title={t('access.sshNetworksConfirmTitle')}
		locked={netBusy}
		onclose={() => (netConfirm = false)}
	>
		<p>{t('access.sshNetworksConfirmText', { address: ssh.clientAddress })}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={netBusy} onclick={() => (netConfirm = false)}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-warn"
				disabled={netBusy}
				onclick={() => saveNetworks(true)}>{t('access.sshNetworksConfirmSubmit')}</button
			>
		</div>
	</Dialog>
{/if}

{#if removing}
	<Dialog title={t('access.certRemoveTitle')} locked={busy} onclose={() => (removing = false)}>
		<p>{t('access.certRemoveText')}</p>
		{#if error}<p class="notice-error" role="alert">{error.text}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (removing = false)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-warn" disabled={busy} onclick={removeCertificate}
				>{t('access.certRemoveSubmit')}</button
			>
		</div>
	</Dialog>
{/if}

<TerminalSettings />

<style>
	/* A value at the right of its name: it wraps, never cut. */
	.value {
		color: var(--text);
		overflow-wrap: anywhere;
	}
	.networks {
		max-width: 32rem;
	}
	.small {
		font-size: var(--text-sm);
	}
	textarea {
		height: auto;
		padding: var(--space-2-5) var(--space-3);
		font-size: var(--text-sm);
		resize: vertical;
	}
	input[type='file'] {
		font: inherit;
		font-size: var(--text-base);
		color: var(--muted);
	}
	pre {
		margin: var(--space-1-5) 0 0;
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
</style>
