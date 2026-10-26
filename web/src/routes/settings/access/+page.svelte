<script lang="ts">
	import TerminalSettings from '#lib/components/TerminalSettings.svelte';
	import { revealTarget } from '#lib/reveal-target.ts';
	// SSH access and the certificate of the web UI.
	import { api, ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
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
	let removing = $state(false);
	let certPem = $state('');
	let keyPem = $state('');
	let saved = $state(false);
	let sshError = $state('');
	let sshBusy = $state(false);
	let sshSaved = $state(false);
	$effect(() => { if (saved) { const timer = setTimeout(() => saved = false, 5000); return () => clearTimeout(timer); } });
	$effect(() => { if (sshSaved) { const timer = setTimeout(() => sshSaved = false, 5000); return () => clearTimeout(timer); } });

	function problem(err: unknown) {
		const code = err instanceof ApiError ? err.code : 'internal';
		error = { text: errorText(code), detail: code === 'invalid_certificate' && err instanceof ApiError ? err.message.replace(/^tlscert: certificate not usable: /, '') : '' };
	}

	async function loadSsh() {
		try {
			ssh = await api.get<Ssh>('/system/ssh');
			sshError = '';
		} catch (err) {
			sshError = errorText(err instanceof ApiError ? err.code : 'internal');
		}
	}
	async function loadCertificate() {
		error = null;
		try { certificate = await api.get<Certificate>('/system/certificate'); }
		catch (err) { if (err instanceof ApiError && err.code === 'read_only') noTls = true; else problem(err); }
	}
	$effect(() => { loadSsh(); loadCertificate(); });
	// The service follows the switch within a moment.
	$effect(() => {
		if (!ssh || ssh.enabled === ssh.running) return;
		const timer = setInterval(loadSsh, 2000);
		return () => clearInterval(timer);
	});

	async function setSsh(enabled: boolean) {
		if (sshBusy) return;
		sshBusy = true; sshSaved = false; sshError = '';
		try { ssh = await api.put<Ssh>('/system/ssh', { enabled }); sshSaved = true; }
		catch (err) { sshError = errorText(err instanceof ApiError ? err.code : 'internal'); }
		finally { sshBusy = false; }
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

	const host = $derived(system.info?.network?.ipv4.split('/')[0] || system.info?.hostname || 'nas');
	const daysLeft = $derived(certificate ? Math.floor((Date.parse(certificate.notAfter) - Date.now()) / 86_400_000) : 0);
	// Groups of four are easier to compare with what the browser shows.
	const fingerprint = $derived(certificate?.fingerprint.match(/.{1,2}/g)?.join(':') ?? '');
</script>

<PageHead title={t('access.title')} parent={{ href: '/settings', label: t('nav.settings') }} />



<section class="tile" aria-labelledby="access-ssh">
		<h2 class="tile-title" id="access-ssh" use:revealTarget>{t('access.ssh')}</h2>
		{#if ssh}
		<div class="head">
			<span class="chip {ssh.running ? 'ok' : ''}">{ssh.running ? t('access.sshRunning') : t('access.sshOff')}</span>
		</div>
		<label class="check">
			<input type="checkbox" checked={ssh.enabled} disabled={sshBusy} onchange={async event => { const field = event.currentTarget; await setSsh(field.checked); field.checked = ssh?.enabled ?? false; }} />
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
		{/if}
		{#if sshBusy || sshSaved}<p role="status">{t(sshBusy ? 'ui.saving' : 'ui.saved')}</p>{/if}
		{#if sshError}<div class="notice-error" role="alert">{sshError}<button class="btn" onclick={loadSsh}>{t('ui.retry')}</button></div>{:else if !ssh}<p role="status">{t('common.loading')}</p>{/if}
	</section>

<section class="tile" aria-labelledby="access-cert">
	<h2 class="tile-title" id="access-cert" use:revealTarget>{t('access.cert')}</h2>
	{#if error}<div class="notice-error" role="alert">{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}<button class="btn" onclick={loadCertificate}>{t('ui.retry')}</button></div>{/if}
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
				{#if certificate.kind === 'custom'}<button type="button" class="btn" onclick={() => ((removing = true), (error = null))} disabled={busy}>{t('access.certRemove')}</button>{/if}
			</div>
		{/if}
	{/if}
</section>

{#if removing}
	<Dialog title={t('access.certRemoveTitle')} locked={busy} onclose={() => (removing = false)}>
		<p>{t('access.certRemoveText')}</p>
		{#if error}<p class="notice-error" role="alert">{error.text}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (removing = false)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-warn" disabled={busy} onclick={removeCertificate}>{t('access.certRemoveSubmit')}</button>
		</div>
	</Dialog>
{/if}

<TerminalSettings />

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
