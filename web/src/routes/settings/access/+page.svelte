<script lang="ts">
	// SSH access and the certificate of the web UI.
	import { api, ApiError } from '#lib/api.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import { formatDateTime } from '#lib/format.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { system } from '#lib/system.svelte.ts';

	type Ssh = { enabled: boolean; running: boolean; fingerprint: string; accounts: string[] };
	type Certificate = { kind: 'self-signed' | 'custom'; names: string[]; issuer: string; notBefore: string; notAfter: string; fingerprint: string };

	let ssh = $state<Ssh | null>(null);
	let certificate = $state<Certificate | null>(null);
	let noTls = $state(false);
	let error = $state<{ text: string; detail: string } | null>(null);
	let busy = $state(false);
	let replacing = $state(false);
	let certPem = $state('');
	let keyPem = $state('');
	let saved = $state(false);

	function problem(err: unknown) {
		const code = err instanceof ApiError ? err.code : 'internal';
		error = { text: errorText(code), detail: code === 'invalid_certificate' && err instanceof ApiError ? err.message.replace(/^tlscert: certificate not usable: /, '') : '' };
	}

	async function loadSsh() {
		try {
			ssh = await api.get<Ssh>('/system/ssh');
		} catch (err) {
			if (!ssh) problem(err);
		}
	}
	$effect(() => {
		loadSsh();
		api.get<Certificate>('/system/certificate').then(
			(res) => (certificate = res),
			(err) => (err instanceof ApiError && err.code === 'read_only' ? (noTls = true) : problem(err))
		);
	});
	// The service follows the switch within a moment.
	$effect(() => {
		if (!ssh || ssh.enabled === ssh.running) return;
		const timer = setInterval(loadSsh, 2000);
		return () => clearInterval(timer);
	});

	async function setSsh(enabled: boolean) {
		error = null;
		try {
			ssh = await api.put<Ssh>('/system/ssh', { enabled });
		} catch (err) {
			problem(err);
			loadSsh();
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
			certificate = await api.put<Certificate>('/system/certificate', { certificate: certPem, key: keyPem });
			certPem = keyPem = '';
			replacing = false;
			saved = true;
		} catch (err) {
			problem(err);
		}
		busy = false;
	}

	async function removeCertificate() {
		busy = true;
		error = null;
		saved = false;
		try {
			certificate = await api.delete<Certificate>('/system/certificate');
		} catch (err) {
			problem(err);
		}
		busy = false;
	}

	const host = $derived(system.info?.network?.ipv4.split('/')[0] || system.info?.hostname || 'nas');
	const daysLeft = $derived(certificate ? Math.floor((Date.parse(certificate.notAfter) - Date.now()) / 86_400_000) : 0);
	// Groups of four are easier to compare with what the browser shows.
	const fingerprint = $derived(certificate?.fingerprint.match(/.{1,2}/g)?.join(':') ?? '');
</script>

<PageHead title={t('access.title')} parent={{ href: '/settings', label: t('nav.settings') }} />

{#if error}<div class="notice-error" role="alert">{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}</div>{/if}

{#if ssh}
	<section class="tile" aria-labelledby="access-ssh">
		<div class="head">
			<h2 class="tile-title" id="access-ssh">{t('access.ssh')}</h2>
			<span class="chip {ssh.running ? 'ok' : ''}">{ssh.running ? t('access.sshRunning') : t('access.sshOff')}</span>
		</div>
		<label class="check">
			<input type="checkbox" checked={ssh.enabled} onchange={(event) => setSsh(event.currentTarget.checked)} />
			<span>{t('access.sshEnable')}</span>
		</label>
		<p class="muted">{t('access.sshText')}</p>
		{#if ssh.enabled}
			<dl>
				<div><dt>{t('access.sshWho')}</dt><dd>{ssh.accounts.length ? ssh.accounts.join(', ') : t('access.sshNobody')}</dd></div>
				<div><dt>{t('access.sshCommand')}</dt><dd class="mono">ssh {session.data?.user?.name}@{host}</dd></div>
				<div class="wide"><dt>{t('access.sshFingerprint')}</dt><dd class="mono">{ssh.fingerprint || '–'}</dd></div>
			</dl>
			{#if ssh.accounts.length === 0}<p class="notice-warn">{t('access.sshNoKeys')} <a href="/account">{t('menu.account')}</a></p>{/if}
			<p class="muted">{t('access.sshNote')}</p>
		{/if}
	</section>
{/if}

<section class="tile" aria-labelledby="access-cert">
	<h2 class="tile-title" id="access-cert">{t('access.cert')}</h2>
	{#if noTls}
		<p class="muted">{t('access.certNone')}</p>
	{:else if certificate}
		{#if certificate.kind === 'self-signed'}
			<p class="muted">{t('access.certSelf')}</p>
		{:else if daysLeft < 14}
			<p class="notice-warn">{t('access.certExpiring', { days: Math.max(daysLeft, 0) })}</p>
		{/if}
		{#if saved}<p class="notice-ok" role="status">{t('access.certSaved')}</p>{/if}
		<dl>
			<div><dt>{t('access.certKind')}</dt><dd>{t(`access.certKind.${certificate.kind}`)}</dd></div>
			<div><dt>{t('access.certIssuer')}</dt><dd>{certificate.issuer || '–'}</dd></div>
			<div><dt>{t('access.certUntil')}</dt><dd>{formatDateTime(new Date(certificate.notAfter))}</dd></div>
			<div class="wide"><dt>{t('access.certNames')}</dt><dd class="mono">{certificate.names.join(', ')}</dd></div>
			<div class="wide"><dt>{t('access.certFingerprint')}</dt><dd class="mono small">{fingerprint}</dd></div>
		</dl>
		{#if replacing}
			<form onsubmit={saveCertificate}>
				<p class="muted">{t('access.certFormHint')}</p>
				<label><span>{t('access.certChain')}</span>
					<textarea class="field mono" bind:value={certPem} rows="6" placeholder="-----BEGIN CERTIFICATE-----" spellcheck="false" required></textarea>
					<input type="file" accept=".pem,.crt,.cer,.txt" onchange={(event) => readFile(event, 'cert')} />
				</label>
				<label><span>{t('access.certKey')}</span>
					<textarea class="field mono" bind:value={keyPem} rows="6" placeholder="-----BEGIN PRIVATE KEY-----" spellcheck="false" autocomplete="off" required></textarea>
					<input type="file" accept=".pem,.key,.txt" onchange={(event) => readFile(event, 'key')} />
				</label>
				<p class="muted">{t('access.certKeyNote')}</p>
				<div class="actions">
					<button type="button" class="btn" onclick={() => ((replacing = false), (certPem = keyPem = ''), (error = null))} disabled={busy}>{t('common.cancel')}</button>
					<button type="submit" class="btn btn-primary" disabled={busy}>{t('access.certUse')}</button>
				</div>
			</form>
		{:else}
			<div class="actions start">
				<button type="button" class="btn" onclick={() => ((replacing = true), (saved = false))}>{t(certificate.kind === 'custom' ? 'access.certReplace' : 'access.certOwn')}</button>
				{#if certificate.kind === 'custom'}<button type="button" class="btn" onclick={removeCertificate} disabled={busy}>{t('access.certRemove')}</button>{/if}
			</div>
		{/if}
	{/if}
</section>

<style>
	section {
		display: flex;
		flex-direction: column;
		gap: 14px;
		padding: 18px;
		font-size: 0.875rem;
	}
	p {
		margin: 0;
		max-width: 60rem;
	}
	.head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
	}
	label.check {
		display: flex;
		flex-direction: row;
		align-items: center;
		gap: 10px;
		font-weight: 600;
	}
	.check input {
		width: 18px;
		height: 18px;
		margin: 0;
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
	.small {
		font-size: 0.75rem;
	}
	form,
	label {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	form {
		gap: 14px;
		padding-top: 14px;
		border-top: 1px solid var(--panel-border);
	}
	label > span {
		font-size: 0.8125rem;
		color: var(--muted);
	}
	textarea {
		height: auto;
		padding: 10px 12px;
		font-size: 0.75rem;
		resize: vertical;
	}
	input[type='file'] {
		font: inherit;
		font-size: 0.8125rem;
		color: var(--muted);
	}
	.actions {
		display: flex;
		justify-content: flex-end;
		gap: 10px;
	}
	.actions.start {
		justify-content: flex-start;
	}
	pre {
		margin: 6px 0 0;
		font-family: var(--font-mono);
		font-size: 0.75rem;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
</style>
