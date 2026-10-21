<script lang="ts">
	import PageHead from '#lib/components/PageHead.svelte';
	import SoundSettings from '#lib/components/SoundSettings.svelte';
	// One's own account: password, second factor, logged-in devices.
	import qrcode from 'qrcode-generator';
	import { api, ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import PasswordDialog from '#lib/components/PasswordDialog.svelte';
	import { errorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import { refreshSession, session } from '#lib/session.svelte.ts';
	import ApiKeys from './ApiKeys.svelte';
	import SshKeys from './SshKeys.svelte';

	type Device = { id: string; created: string; lastSeen: string; remoteIP: string; userAgent: string; current: boolean };

	const user = $derived(session.data?.user);
	let devices = $state<Device[]>([]);
	let error = $state('');
	let changingPassword = $state(false);
	// Setting up the second factor: the secret to scan, then the recovery codes.
	let setup = $state<{ secret: string; uri: string } | null>(null);
	let code = $state('');
	let recovery = $state<string[] | null>(null);
	let disabling = $state(false);
	let password = $state('');
	let dialogError = $state('');
	let busy = $state(false);

	const problem = (err: unknown) => errorText(err instanceof ApiError ? err.code : 'internal');
	const time = $derived(new Intl.DateTimeFormat(i18n.locale, { dateStyle: 'medium', timeStyle: 'short' }));

	async function loadDevices() {
		try {
			devices = (await api.get<{ sessions: Device[] }>('/account/sessions')).sessions;
		} catch (err) {
			error = problem(err);
		}
	}
	$effect(() => {
		loadDevices();
	});

	/** The address for the authenticator app as a QR code, drawn as one SVG path. */
	const qr = $derived.by(() => {
		if (!setup) return null;
		const code = qrcode(0, 'M');
		code.addData(setup.uri);
		code.make();
		const size = code.getModuleCount();
		let path = '';
		for (let row = 0; row < size; row++) for (let column = 0; column < size; column++) if (code.isDark(row, column)) path += `M${column},${row}h1v1h-1z`;
		return { size, path };
	});

	async function begin() {
		error = '';
		code = '';
		try {
			setup = await api.post('/account/2fa/begin');
		} catch (err) {
			error = problem(err);
		}
	}

	async function enable(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		dialogError = '';
		try {
			recovery = (await api.post<{ recoveryCodes: string[] }>('/account/2fa/enable', { code })).recoveryCodes;
			setup = null;
			await refreshSession();
		} catch (err) {
			dialogError = problem(err);
		}
		busy = false;
	}

	async function disable(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		dialogError = '';
		try {
			await api.post('/account/2fa/disable', { password, code });
			disabling = false;
			await refreshSession();
		} catch (err) {
			dialogError = problem(err);
		}
		password = '';
		code = '';
		busy = false;
	}

	async function end(device: Device) {
		try {
			devices = (await api.delete<{ sessions: Device[] }>(`/account/sessions/${device.id}`)).sessions;
		} catch (err) {
			error = problem(err);
		}
	}

	/** What a browser calls itself, cut down to what a person recognizes. */
	function browser(agent: string): string {
		const name = /Firefox\/|Edg\/|Chrome\/|Safari\//.exec(agent)?.[0].replace(/\/$/, '') ?? '';
		const system = /Windows|Android|iPhone|iPad|Mac OS X|Linux/.exec(agent)?.[0] ?? '';
		return [name === 'Edg' ? 'Edge' : name, system].filter(Boolean).join(' · ') || agent.slice(0, 60) || '–';
	}
</script>


<PageHead title={t('account.title')} />
<SoundSettings />
{#if error}<p class="notice-error" role="alert">{error}</p>{/if}

{#if user}
	<section class="tile">
		<div class="who">
			<span class="initial" aria-hidden="true">{user.name.slice(0, 1).toUpperCase()}</span>
			<div><span class="name mono">{user.name}</span><span class="muted">{t('account.role')}: {t(`role.${user.role}`)}</span></div>
		</div>
	</section>

	<section class="tile" aria-labelledby="acc-password">
		<h2 class="tile-title" id="acc-password">{t('account.password')}</h2>
		<div class="row">
			<p class="muted">{t('account.passwordText')}</p>
			<button type="button" class="btn" onclick={() => (changingPassword = true)}>{t('account.changePassword')}</button>
		</div>
	</section>

	<section class="tile" aria-labelledby="acc-2fa">
		<h2 class="tile-title" id="acc-2fa">{t('account.twoFactor')}</h2>
		<div class="row">
			<p><span class="chip" class:ok={user.twoFactor}>{t(user.twoFactor ? 'plugins.active' : 'plugins.off')}</span> <span class="muted">{t(user.twoFactor ? 'account.twoFactorOn' : 'account.twoFactorOff')}</span></p>
			{#if user.twoFactor}
				<button type="button" class="btn" onclick={() => ((disabling = true), (dialogError = ''), (code = ''))}>{t('account.twoFactorDisable')}</button>
			{:else if !setup}
				<button type="button" class="btn btn-primary" onclick={begin}>{t('account.twoFactorSetup')}</button>
			{/if}
		</div>
		{#if setup && qr}
			<form class="setup" onsubmit={enable}>
				<svg class="qr" viewBox="-2 -2 {qr.size + 4} {qr.size + 4}" role="img" aria-label={t('account.twoFactor')}><rect x="-2" y="-2" width={qr.size + 4} height={qr.size + 4} class="paper" /><path d={qr.path} class="ink" /></svg>
				<div class="fields">
					<p class="muted">{t('account.scan')}</p>
					<label><span>{t('account.secret')}</span><input class="field mono" type="text" value={setup.secret} readonly onfocus={(event) => event.currentTarget.select()} /></label>
					<label><span>{t('account.enterCode')}</span><input class="field mono" type="text" bind:value={code} inputmode="numeric" autocomplete="one-time-code" spellcheck="false" required /></label>
					{#if dialogError}<p class="notice-warn" role="alert">{dialogError}</p>{/if}
					<div class="actions">
						<button type="button" class="btn" onclick={() => (setup = null)}>{t('common.cancel')}</button>
						<button type="submit" class="btn btn-primary" disabled={busy}>{t('account.enable')}</button>
					</div>
				</div>
			</form>
		{/if}
	</section>

	<SshKeys />

	{#if user?.role === 'owner' || user?.role === 'admin'}<ApiKeys />{/if}

	<section class="tile" aria-labelledby="acc-devices">
		<h2 class="tile-title" id="acc-devices">{t('account.devices')}</h2>
		{#each devices as device (device.id)}
			<div class="device">
				<div>
					<span class="name">{browser(device.userAgent)}{#if device.current} <span class="chip ok">{t('account.thisDevice')}</span>{/if}</span>
					<span class="muted mono">{device.remoteIP} · {t('account.since', { time: time.format(new Date(device.created)) })} · {t('account.lastSeen', { time: time.format(new Date(device.lastSeen)) })}</span>
				</div>
				{#if !device.current}<button type="button" class="btn" onclick={() => end(device)}>{t('account.end')}</button>{/if}
			</div>
		{/each}
		<p class="muted note">{t('account.devicesNote')}</p>
	</section>
{/if}

{#if changingPassword && user}
	<PasswordDialog name={user.name} self onclose={() => ((changingPassword = false), loadDevices())} />
{/if}

{#if recovery}
	<Dialog title={t('account.recoveryTitle')} locked onclose={() => (recovery = null)}>
		<p class="text">{t('account.recoveryText')}</p>
		<pre class="codes">{recovery.join('\n')}</pre>
		<div class="dialog-actions"><button type="button" class="btn btn-primary" onclick={() => (recovery = null)}>{t('account.recoveryDone')}</button></div>
	</Dialog>
{/if}

{#if disabling}
	<Dialog title={t('account.disableTitle')} onclose={() => (disabling = false)}>
		<form class="confirm" onsubmit={disable}>
			<p class="text">{t('account.disableText')}</p>
			<label><span>{t('auth.password')}</span><input class="field" type="password" bind:value={password} autocomplete="current-password" required /></label>
			<label><span>{t('auth.code')}</span><input class="field mono" type="text" bind:value={code} inputmode="numeric" autocomplete="one-time-code" spellcheck="false" required /></label>
			{#if dialogError}<p class="notice-warn" role="alert">{dialogError}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" onclick={() => (disabling = false)}>{t('common.cancel')}</button>
				<button type="submit" class="btn btn-warn" disabled={busy}>{t('account.disableSubmit')}</button>
			</div>
		</form>
	</Dialog>
{/if}

<style>
	section {
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding: 18px;
		font-size: 0.875rem;
	}
	.who {
		display: flex;
		align-items: center;
		gap: 14px;
	}
	.who div {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.initial {
		width: 44px;
		height: 44px;
		display: flex;
		align-items: center;
		justify-content: center;
		background: var(--accent);
		border-radius: var(--radius-ctl);
		color: var(--on-accent);
		font-size: 1.125rem;
		font-weight: 600;
	}
	.name {
		font-size: 0.9375rem;
		font-weight: 600;
	}
	.row {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
	}
	.setup {
		display: flex;
		flex-wrap: wrap;
		gap: 20px;
		padding-top: 12px;
		border-top: 1px solid var(--panel-border);
	}
	.qr {
		width: 200px;
		height: 200px;
		flex: none;
		border-radius: var(--radius-ctl);
	}
	/* A QR code is dark on light whatever the theme: scanners expect that. */
	.paper {
		fill: var(--qr-light);
	}
	.ink {
		fill: var(--qr-dark);
	}
	.fields {
		flex: 1 1 320px;
		display: flex;
		flex-direction: column;
		gap: 12px;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: 0.8125rem;
	}
	label > span {
		color: var(--muted);
	}
	.actions {
		display: flex;
		justify-content: flex-end;
		gap: 10px;
	}
	.device {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 10px;
		padding-top: 12px;
		border-top: 1px solid var(--panel-border);
	}
	.device div {
		display: flex;
		flex-direction: column;
		gap: 3px;
		min-width: 0;
	}
	.device .mono {
		font-size: 0.75rem;
		overflow-wrap: anywhere;
	}
	.note {
		font-size: 0.8125rem;
	}
	.text {
		font-size: 0.875rem;
	}
	.codes {
		margin: 12px 0;
		padding: calc(var(--row-pad) + 2px) var(--tile-pad);
		background: var(--bg);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		font-family: var(--font-mono);
		font-size: 0.9375rem;
		line-height: 1.7;
		columns: 2;
	}
	.confirm {
		display: flex;
		flex-direction: column;
		gap: 14px;
	}
</style>
