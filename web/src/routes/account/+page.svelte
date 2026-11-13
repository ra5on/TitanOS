<script lang="ts">
	import PushDevices from '#lib/components/PushDevices.svelte';
	import ProtectEmergency from '#lib/components/agent/features/ProtectEmergency.svelte';
	import { api as protectApi } from '#lib/api.ts';
	// PLAN 54: a person registered for the emergency access asks here, also
	// without the KI-Agent.
	let emergencyMe = $state(false);
	void protectApi
		.get<{ emergency?: boolean }>('/agent/protect/me')
		.then((m) => (emergencyMe = !!m.emergency))
		.catch(() => (emergencyMe = false));
	import PageHead from '#lib/components/PageHead.svelte';
	import SoundSettings from '#lib/components/SoundSettings.svelte';
	import PersonalNotifications from '#lib/components/PersonalNotifications.svelte';
	import BellSettings from '#lib/components/BellSettings.svelte';
	import CameraMailRow from '#lib/nvr/CameraMailRow.svelte';
	// One's own account: password, second factor, logged-in devices.
	import qrcode from 'qrcode-generator';
	import { api } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import PasswordDialog from '#lib/components/PasswordDialog.svelte';
	import { apiErrorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import { refreshSession, session } from '#lib/session.svelte.ts';
	import SshKeys from './SshKeys.svelte';
	import Passkeys from './Passkeys.svelte';
	import WebDav from './WebDav.svelte';
	import Picture from './Picture.svelte';
	import AppInstallCard from '#lib/components/AppInstallCard.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { tick } from 'svelte';

	type Device = {
		id: string;
		created: string;
		lastSeen: string;
		remoteIP: string;
		userAgent: string;
		current: boolean;
		kept?: boolean;
		until?: string;
	};

	const user = $derived(session.data?.user);
	const admin = $derived(user?.role === 'owner' || user?.role === 'admin');
	let devices = $state<Device[] | null>(null);
	let devicesError = $state('');
	let error = $state('');
	let endingOthers = $state(false);
	let changingPassword = $state(false);
	// Setting up the second factor: the secret to scan, then the recovery codes.
	type TotpSetup = { secret: string; uri: string };
	let setup = $state<TotpSetup | null>(null);
	// The row of the second factor is open while its setup form shows.
	let setupOpen = $state(true);
	let code = $state('');
	let recovery = $state<string[] | null>(null);
	let disabling = $state(false);
	// Setting up 2FA asks for the password first (a session alone may not, owner 7.11.2026).
	let beginning = $state(false);
	let password = $state('');
	let dialogError = $state('');
	let busy = $state(false);
	// Renewing the recovery codes takes the password and a code like switching off.
	let renewing = $state(false);
	let copyNote = $state<'' | 'copied' | 'failed'>('');
	let codesBlock = $state<HTMLPreElement>();

	const time = $derived(
		new Intl.DateTimeFormat(i18n.locale, { dateStyle: 'medium', timeStyle: 'short' }),
	);

	async function loadDevices() {
		devicesError = '';
		try {
			devices = (await api.get<{ sessions: Device[] }>('/account/sessions')).sessions;
		} catch (err) {
			devicesError = apiErrorText(err);
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
		for (let row = 0; row < size; row++)
			for (let column = 0; column < size; column++)
				if (code.isDark(row, column)) path += `M${column},${row}h1v1h-1z`;
		return { size, path };
	});

	async function begin() {
		if (busy) return;
		busy = true;
		error = '';
		dialogError = '';
		try {
			setup = await api.post<TotpSetup>('/account/2fa/begin', { password });
			setupOpen = true;
			beginning = false;
			password = '';
			code = '';
		} catch (err) {
			dialogError = apiErrorText(err);
		}
		busy = false;
	}

	async function enable(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		dialogError = '';
		try {
			recovery = (await api.post<{ recoveryCodes: string[] }>('/account/2fa/enable', { code }))
				.recoveryCodes;
			setup = null;
			await refreshSession();
		} catch (err) {
			dialogError = apiErrorText(err);
		}
		busy = false;
	}

	function cancelSetup() {
		setup = null;
		code = '';
		dialogError = '';
	}

	// Switching off and renewing run only from a click on their buttons, never from Enter.
	async function disable() {
		if (busy) return;
		busy = true;
		dialogError = '';
		try {
			await api.post('/account/2fa/disable', { password, code });
			disabling = false;
			await refreshSession();
		} catch (err) {
			dialogError = apiErrorText(err);
		}
		password = '';
		code = '';
		busy = false;
	}

	async function renew() {
		if (busy) return;
		busy = true;
		dialogError = '';
		try {
			recovery = (
				await api.post<{ recoveryCodes: string[] }>('/account/2fa/recovery', { password, code })
			).recoveryCodes;
			renewing = false;
		} catch (err) {
			dialogError = apiErrorText(err);
		}
		password = '';
		code = '';
		busy = false;
	}

	function openConfirm(kind: 'disable' | 'renew' | 'begin') {
		dialogError = '';
		code = '';
		password = '';
		disabling = kind === 'disable';
		renewing = kind === 'renew';
		beginning = kind === 'begin';
	}
	function closeConfirm() {
		disabling = false;
		renewing = false;
		beginning = false;
	}

	async function end(device: Device) {
		try {
			devices = (await api.delete<{ sessions: Device[] }>(`/account/sessions/${device.id}`))
				.sessions;
		} catch (err) {
			error = apiErrorText(err);
		}
	}

	async function endOthers() {
		if (busy) return;
		busy = true;
		dialogError = '';
		try {
			devices = (await api.delete<{ sessions: Device[] }>('/account/sessions')).sessions;
			endingOthers = false;
		} catch (err) {
			dialogError = apiErrorText(err);
		}
		busy = false;
	}

	async function copyCodes() {
		if (!recovery) return;
		try {
			await navigator.clipboard.writeText(recovery.join('\n'));
			copyNote = 'copied';
		} catch {
			// No clipboard (plain http, refused): select the codes for copying by hand.
			copyNote = 'failed';
			if (codesBlock) getSelection()?.selectAllChildren(codesBlock);
		}
	}

	/** The codes as a text file, made in the browser; nothing goes through the NAS. */
	function downloadCodes() {
		if (!recovery || !user) return;
		const url = URL.createObjectURL(new Blob([recovery.join('\n') + '\n'], { type: 'text/plain' }));
		const link = Object.assign(document.createElement('a'), {
			href: url,
			download: `titanos-${user.name}-recovery-codes.txt`,
		});
		link.click();
		setTimeout(() => URL.revokeObjectURL(url), 1000);
	}

	/** What a browser calls itself, cut down to what a person recognizes. */
	function browser(agent: string): string {
		const name = /Firefox\/|Edg\/|Chrome\/|Safari\//.exec(agent)?.[0].replace(/\/$/, '') ?? '';
		const system = /Windows|Android|iPhone|iPad|Mac OS X|Linux/.exec(agent)?.[0] ?? '';
		return (
			[name === 'Edg' ? 'Edge' : name, system].filter(Boolean).join(' · ') ||
			agent.slice(0, 60) ||
			'–'
		);
	}

	// PLAN 67-1: the menu bar of the desktop (Konto): the dialogs, or the section on the page.
	import { registerMenuCommands } from '#lib/menu-commands.svelte.ts';
	// A group (devices) scrolls into view; a row with parts opens first.
	let rows = $state<Record<string, boolean>>({ 'acc-passkeys': false, 'acc-ssh': false });
	const reveal = (id: string) => async () => {
		if (id in rows) rows[id] = true;
		await tick();
		document.getElementById(id)?.scrollIntoView({ behavior: 'smooth', block: 'start' });
	};
	registerMenuCommands({
		'account.password': () => (changingPassword = true),
		'account.twoFactorOn': {
			run: () => openConfirm('begin'),
			enabled: () => !!user && !user.twoFactor && !setup,
		},
		'account.twoFactorOff': {
			run: () => openConfirm('disable'),
			enabled: () => !!user?.twoFactor,
		},
		'account.recoveryCodes': {
			run: () => openConfirm('renew'),
			enabled: () => !!user?.twoFactor,
		},
		'account.passkeys': reveal('acc-passkeys'),
		'account.ssh': reveal('acc-ssh'),
		'account.webdav': reveal('acc-webdav'),
		'account.devices': reveal('acc-devices'),
		'account.logoutAll': {
			run: () => {
				dialogError = '';
				endingOthers = true;
			},
			enabled: () => !!devices?.some((device) => !device.current),
		},
	});
</script>

<PageHead title={t('account.title')} />
{#if error}<p class="notice-error" role="alert">{error}</p>{/if}

{#snippet twoFactorSetup()}
	{#if setup && qr}
		<form class="srow-block setup" onsubmit={enable}>
			<svg
				class="qr"
				viewBox="-2 -2 {qr.size + 4} {qr.size + 4}"
				role="img"
				aria-label={t('account.twoFactor')}
				><rect x="-2" y="-2" width={qr.size + 4} height={qr.size + 4} class="paper" /><path
					d={qr.path}
					class="ink"
				/></svg
			>
			<div class="fields">
				<p class="muted">{t('account.scan')}</p>
				<label
					><span>{t('account.secret')}</span><input
						class="field mono"
						type="text"
						value={setup.secret}
						readonly
						onfocus={(event) => event.currentTarget.select()}
					/></label
				>
				<label
					><span>{t('account.enterCode')}</span><input
						class="field mono"
						type="text"
						bind:value={code}
						inputmode="numeric"
						autocomplete="one-time-code"
						spellcheck="false"
						required
					/></label
				>
				{#if dialogError}<p class="notice-warn" role="alert">{dialogError}</p>{/if}
				<div class="actions">
					<button type="button" class="btn" onclick={cancelSetup}>{t('common.cancel')}</button>
					<button type="submit" class="btn btn-primary" disabled={busy}
						>{t('account.enable')}</button
					>
				</div>
			</div>
		</form>
	{/if}
{/snippet}

<!-- PLAN 78-5: the page in rows like the control panel – a group is a plain
     heading, every setting a row; what has parts opens inside its row. -->
{#if user}
	<SettingGroup title={t('account.group.account')} id="acc-account">
		<SettingRow
			id="acc-who"
			icon="users"
			title={user.name}
			desc="{t('account.role')}: {t(`role.${user.role}`)}"
		/>
		<!-- PLAN 74: the picture of the account on the login page. -->
		<Picture name={user.name} />
		<AppInstallCard />
	</SettingGroup>

	<SettingGroup title={t('account.group.login')} id="acc-login">
		<SettingRow
			id="acc-password"
			icon="lock"
			title={t('account.password')}
			desc={t('account.passwordText')}
		>
			{#snippet control()}
				<button type="button" class="btn" onclick={() => (changingPassword = true)}
					>{t('account.changePassword')}</button
				>
			{/snippet}
		</SettingRow>

		<!-- The form with the QR code opens inside the row while setting up
		     (no id on the row: it must not remember being closed). -->
		<div id="acc-2fa">
			<SettingRow
				icon="shield"
				title={t('account.twoFactor')}
				desc={t(user.twoFactor ? 'account.twoFactorOn' : 'account.twoFactorOff')}
				bind:open={setupOpen}
				children={setup && qr ? twoFactorSetup : undefined}
			>
				{#snippet control()}
					<span class="chip" class:ok={user.twoFactor}
						>{t(user.twoFactor ? 'plugins.active' : 'plugins.off')}</span
					>
					{#if user.twoFactor}
						<button type="button" class="btn" onclick={() => openConfirm('renew')}
							>{t('account.recoveryRenew')}</button
						>
						<button type="button" class="btn" onclick={() => openConfirm('disable')}
							>{t('account.twoFactorDisable')}</button
						>
					{:else if !setup}
						<button type="button" class="btn btn-primary" onclick={() => openConfirm('begin')}
							>{t('account.twoFactorSetup')}</button
						>
					{/if}
				{/snippet}
			</SettingRow>
		</div>

		<Passkeys bind:open={rows['acc-passkeys']} />
		<SshKeys bind:open={rows['acc-ssh']} />
		<WebDav />
	</SettingGroup>

	<SettingGroup title={t('account.devices')} id="acc-devices">
		{#snippet tools()}
			{#if devices && devices.some((device) => !device.current)}
				<button
					type="button"
					class="btn"
					onclick={() => ((endingOthers = true), (dialogError = ''))}
					>{t('account.endOthers')}</button
				>
			{/if}
		{/snippet}
		{#if devicesError}
			<div class="notice-error row" role="alert">
				<span>{devicesError}</span><button type="button" class="btn" onclick={loadDevices}
					>{t('common.retry')}</button
				>
			</div>
		{:else if !devices}
			<p class="muted loading" role="status">{t('common.loading')}</p>
		{:else}
			{#each devices as device (device.id)}
				<SettingRow icon="monitor" title={browser(device.userAgent)}>
					{#snippet badge()}{#if device.current}
							<span class="chip ok">{t('account.thisDevice')}</span>{/if}{/snippet}
					{#snippet description()}
						<span class="mono"
							>{device.remoteIP} · {t('account.since', {
								time: time.format(new Date(device.created)),
							})} · {t('account.lastSeen', {
								time: time.format(new Date(device.lastSeen)),
							})}{#if device.kept}{' '}
								· {device.until
									? t('account.keptUntil', { time: time.format(new Date(device.until)) })
									: t('account.keptForever')}{/if}</span
						>
					{/snippet}
					{#snippet control()}
						{#if !device.current}<button
								type="button"
								class="btn"
								aria-label="{t('account.end')}: {browser(device.userAgent)}, {device.remoteIP}"
								onclick={() => end(device)}>{t('account.end')}</button
							>{/if}
					{/snippet}
				</SettingRow>
			{/each}
		{/if}
		<p class="muted note">{t('account.devicesNote')}</p>
	</SettingGroup>

	<SettingGroup title={t(admin ? 'account.group.bell' : 'account.group.notify')} id="acc-notify">
		{#if !admin}
			<!-- The devices keep their look (the component also serves the control
			     panel and the agent); here they are the parts of a row. -->
			<SettingRow id="push" icon="bell" title={t('push.title')} desc={t('push.introPersonal')} open>
				<div class="srow-block"><PushDevices bare personal /></div>
			</SettingRow>
			<CameraMailRow />
		{/if}
		{#if admin}<BellSettings />{/if}
		<SoundSettings />
	</SettingGroup>
	{#if admin}<PersonalNotifications />{/if}
	{#if emergencyMe}
		<SettingGroup title={t('agent.f.menu.emergency')} id="emergency">
			<div class="emergency"><ProtectEmergency /></div>
		</SettingGroup>
	{/if}
{/if}

{#if changingPassword && user}
	<PasswordDialog
		name={user.name}
		self
		onclose={() => ((changingPassword = false), loadDevices())}
	/>
{/if}

{#if recovery}
	<Dialog
		title={t('account.recoveryTitle')}
		locked
		onclose={() => ((recovery = null), (copyNote = ''))}
	>
		<p class="text">{t('account.recoveryText')}</p>
		<pre class="codes" bind:this={codesBlock}>{recovery.join('\n')}</pre>
		<div class="buttons">
			<button type="button" class="btn" onclick={copyCodes}>{t('account.recoveryCopy')}</button>
			<button type="button" class="btn" onclick={downloadCodes}
				>{t('account.recoveryDownload')}</button
			>
		</div>
		{#if copyNote === 'copied'}<p class="muted" role="status">{t('account.recoveryCopied')}</p>{/if}
		{#if copyNote === 'failed'}<p class="notice-warn" role="alert">
				{t('account.recoveryCopyFailed')}
			</p>{/if}
		<div class="dialog-actions">
			<button
				type="button"
				class="btn btn-primary"
				onclick={() => ((recovery = null), (copyNote = ''))}>{t('account.recoveryDone')}</button
			>
		</div>
	</Dialog>
{/if}

{#if disabling || renewing || beginning}
	<Dialog
		title={t(
			beginning ? 'account.beginTitle' : disabling ? 'account.disableTitle' : 'account.renewTitle',
		)}
		locked={busy}
		onclose={closeConfirm}
	>
		<form
			class="confirm"
			onsubmit={(event) => {
				event.preventDefault();
				// Enter starts the setup (nothing is lost); switching off stays a click.
				if (beginning && password) void begin();
			}}
		>
			<p class="text">
				{t(
					beginning ? 'account.beginText' : disabling ? 'account.disableText' : 'account.renewText',
				)}
			</p>
			<label
				><span>{t('auth.password')}</span><input
					class="field"
					type="password"
					bind:value={password}
					autocomplete="current-password"
					required
				/></label
			>
			{#if !beginning}
				<label
					><span>{t('auth.code')}</span><input
						class="field mono"
						type="text"
						bind:value={code}
						inputmode="numeric"
						autocomplete="one-time-code"
						spellcheck="false"
						required
					/></label
				>
			{/if}
			{#if dialogError}<p class="notice-warn" role="alert">{dialogError}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" disabled={busy} onclick={closeConfirm}
					>{t('common.cancel')}</button
				>
				<button
					type="button"
					class="btn {beginning ? 'btn-primary' : 'btn-warn'}"
					disabled={busy || !password || (!beginning && !code)}
					onclick={beginning ? begin : disabling ? disable : renew}
					>{t(
						beginning
							? 'account.beginSubmit'
							: disabling
								? 'account.disableSubmit'
								: 'account.renewSubmit',
					)}</button
				>
			</div>
		</form>
	</Dialog>
{/if}

{#if endingOthers}
	<Dialog title={t('account.endOthersTitle')} locked={busy} onclose={() => (endingOthers = false)}>
		<p class="text">
			{t('account.endOthersText', {
				count: devices?.filter((device) => !device.current).length ?? 0,
			})}
		</p>
		{#if dialogError}<p class="notice-warn" role="alert">{dialogError}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (endingOthers = false)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-warn" disabled={busy} onclick={endOthers}
				>{t('account.endOthersSubmit')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.row {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-3);
	}
	.setup {
		flex-direction: row;
		flex-wrap: wrap;
		gap: 20px;
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
		gap: var(--space-3);
		min-width: 0;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: var(--space-1-5);
		font-size: var(--text-base);
	}
	label > span {
		color: var(--muted);
	}
	.buttons {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
	}
	.actions {
		display: flex;
		justify-content: flex-end;
		gap: var(--space-2-5);
	}
	.note {
		font-size: var(--text-base);
	}
	/* The emergency access brings its own cards; they stand below the heading
	   with the spacing the tile gave them. */
	.emergency {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		min-width: 0;
		font-size: var(--text-md);
	}
	.text {
		font-size: var(--text-md);
	}
	.codes {
		margin: var(--space-3) 0;
		padding: calc(var(--row-pad) + 2px) var(--tile-pad);
		background: var(--bg);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		font-family: var(--font-mono);
		font-size: var(--text-md);
		line-height: 1.7;
		columns: 2;
	}
	.confirm {
		display: flex;
		flex-direction: column;
		gap: 14px;
	}
</style>
