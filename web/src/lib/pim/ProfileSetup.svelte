<script lang="ts">
	// 1.13: set up the iPhone with one profile (.mobileconfig) – calendars
	// and contacts at once, optionally the holidays (decision of the owner of
	// 18 Nov 2026). The NAS makes a one-time link after the account password
	// (and code); the iPhone opens it from the QR code, and only then is the
	// device password made, inside the profile. The link and its token live
	// only in this component's state, never in the browser's storage; a link
	// left behind is withdrawn when the page closes. "Herunterladen" fetches
	// the same link here (for AirDrop).
	import { onDestroy } from 'svelte';
	import { api, ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import QrCode from '#lib/components/QrCode.svelte';
	import ReconfirmFields, { reconfirmComplete } from '#lib/components/ReconfirmFields.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { phoneView } from '#lib/desktop/mode.svelte.ts';
	import { formatTime } from '#lib/format.ts';
	import { apiErrorText, i18n, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import {
		HOLIDAY_STATES,
		type DeviceSetup,
		type ProfileInfo,
		type ProfileLink,
		type ProfileMade,
		type SetupAddress,
	} from '#lib/pim/devices.ts';
	import { session } from '#lib/session.svelte.ts';

	let {
		setup,
		describe,
		onfetched,
	}: {
		setup: DeviceSetup | null;
		describe: (a: SetupAddress) => string;
		onfetched: () => void;
	} = $props();

	const phone = $derived(phoneView());
	const user = $derived(session.data?.user?.name ?? '');

	let info = $state<ProfileInfo | null>(null);
	let infoError = $state('');
	let name = $state('');
	let named = $state(false);
	let server = $state('');
	let holidays = $state('');
	let asking = $state(false);
	let password = $state('');
	let code = $state('');
	let askError = $state('');
	let busy = $state(false);
	let made = $state<ProfileMade | null>(null);
	let link = $state<ProfileLink | null>(null);
	let error = $state('');
	let timer: ReturnType<typeof setInterval> | undefined;

	$effect(() => {
		api
			.get<ProfileInfo>('/account/devices/profile')
			.then((r) => {
				info = r;
				infoError = '';
			})
			.catch((err) => (infoError = apiErrorText(err)));
	});

	// "iPhone von Alex" until the person types a name of their own.
	$effect(() => {
		if (!named && user) name = t('iphone.profile.defaultName', { user });
	});

	const addresses = $derived(setup?.addresses ?? []);
	// The first server iOS trusts, else the first one.
	$effect(() => {
		if (!server && addresses.length)
			server = (addresses.find((a) => a.certificate === 'valid') ?? addresses[0]).server;
	});
	const chosen = $derived(addresses.find((a) => a.server === server));

	const STATE_TEXT: Record<string, MessageKey> = {
		bw: 'iphone.state.bw',
		by: 'iphone.state.by',
		be: 'iphone.state.be',
		bb: 'iphone.state.bb',
		hb: 'iphone.state.hb',
		hh: 'iphone.state.hh',
		he: 'iphone.state.he',
		mv: 'iphone.state.mv',
		ni: 'iphone.state.ni',
		nw: 'iphone.state.nw',
		rp: 'iphone.state.rp',
		sl: 'iphone.state.sl',
		sn: 'iphone.state.sn',
		st: 'iphone.state.st',
		sh: 'iphone.state.sh',
		th: 'iphone.state.th',
	};

	function stop() {
		if (timer) clearInterval(timer);
		timer = undefined;
	}

	async function poll() {
		if (!link) return;
		try {
			link = await api.get<ProfileLink>(`/account/devices/profile/${encodeURIComponent(link.id)}`);
		} catch (err) {
			// Gone from the table (much later, or titand restarted): expired.
			if (err instanceof ApiError && err.code === 'profile_link_not_found' && link)
				link = { ...link, state: 'expired' };
		}
		if (link?.state !== 'pending') {
			stop();
			if (link?.state === 'fetched') onfetched();
		}
	}

	function ask(event?: SubmitEvent) {
		event?.preventDefault();
		if (!name.trim()) return;
		password = '';
		code = '';
		askError = '';
		asking = true;
	}

	async function create(event: SubmitEvent) {
		event.preventDefault();
		busy = true;
		askError = '';
		try {
			await withdraw();
			made = await api.post<ProfileMade>('/account/devices/profile', {
				name: name.trim(),
				password,
				code: code.trim(),
				server: chosen ? server : '',
				holidays,
				lang: i18n.locale === 'en' ? 'en' : 'de',
			});
			link = made.link;
			error = '';
			asking = false;
			password = '';
			code = '';
			stop();
			timer = setInterval(poll, 2000);
		} catch (err) {
			askError = apiErrorText(err);
			if (err instanceof ApiError && err.code === 'invalid_code') code = '';
		} finally {
			busy = false;
		}
	}

	async function download() {
		if (!made) return;
		error = '';
		try {
			await api.download(made.path.replace(/^\/api\/v1/, ''), 'TitanOS.mobileconfig', {
				serverName: true,
			});
		} catch (err) {
			error = apiErrorText(err);
		}
		await poll();
	}

	// Withdraws a link still waiting (a new one, "Verwerfen", leaving).
	async function withdraw() {
		const waiting = link?.state === 'pending' ? link : null;
		stop();
		if (!waiting) return;
		link = { ...waiting, state: 'cancelled' };
		try {
			await api.delete(`/account/devices/profile/${encodeURIComponent(waiting.id)}`);
		} catch {
			// It runs out by itself after 10 minutes.
		}
	}

	function reset() {
		made = null;
		link = null;
		error = '';
	}

	onDestroy(() => {
		stop();
		if (link?.state === 'pending')
			void api.delete(`/account/devices/profile/${encodeURIComponent(link.id)}`).catch(() => {});
	});

	function failText(l: ProfileLink): string {
		return t('iphone.profile.failed', {
			reason: apiErrorText(new ApiError(0, l.code ?? '', '')),
		});
	}
</script>

<SettingGroup title={t('iphone.profile.title')} desc={t('iphone.profile.desc')} id="profile">
	{#if infoError}<p class="notice-error" role="alert">{infoError}</p>{/if}
	{#if !made}
		<SettingRow
			icon="device"
			title={t('iphone.profile.name')}
			desc={t('iphone.profile.nameDesc')}
			for="profile-name"
		>
			{#snippet control()}
				<input
					id="profile-name"
					class="field name"
					class:phone
					value={name}
					oninput={(e) => {
						name = e.currentTarget.value;
						named = true;
					}}
					maxlength="64"
					autocomplete="off"
				/>
			{/snippet}
		</SettingRow>
		{#if addresses.length > 1}
			<SettingRow icon="globe" title={t('iphone.profile.server')} for="profile-server">
				{#snippet description()}
					{t('iphone.profile.serverDesc')}
					{#if chosen}<span class="line">{describe(chosen)}</span>{/if}
				{/snippet}
				{#snippet control()}
					<select id="profile-server" class="field" class:phone bind:value={server}>
						{#each addresses as a (a.server)}
							<option value={a.server}
								>{a.server}{a.recommended ? ` (${t('iphone.recommended')})` : ''}</option
							>
						{/each}
					</select>
				{/snippet}
			</SettingRow>
		{/if}
		<SettingRow
			icon="calendar"
			title={t('iphone.profile.holidays')}
			desc={t('iphone.profile.holidaysDesc')}
			for="profile-holidays"
		>
			{#snippet control()}
				<select id="profile-holidays" class="field" class:phone bind:value={holidays}>
					<option value="">{t('iphone.profile.holidaysNone')}</option>
					<option value="de">{t('iphone.profile.holidaysDe')}</option>
					{#each HOLIDAY_STATES as s (s)}
						<option value={`de-${s}`}>{t(STATE_TEXT[s])}</option>
					{/each}
				</select>
			{/snippet}
		</SettingRow>
		{#if info}
			<SettingRow
				icon="shield"
				title={info.signed ? t('iphone.profile.signedTitle') : t('iphone.profile.unsignedTitle')}
				desc={info.signed
					? t('iphone.profile.signed', { signer: info.signer })
					: t('iphone.profile.unsigned')}
			/>
		{/if}
		<div class="srow plain go">
			<button
				type="button"
				class="btn btn-primary"
				disabled={!name.trim() || busy}
				onclick={() => ask()}>{t('iphone.profile.create')}</button
			>
		</div>
	{:else if link}
		<div class="srow plain made" class:phone>
			{#if link.state === 'pending'}
				<QrCode
					text={made.url}
					label={t('iphone.profile.qr', { name: link.device })}
					size={phone ? 220 : 200}
				/>
				<div class="what">
					<b>{t('iphone.profile.scan')}</b>
					<span class="muted">{t('iphone.profile.link')}</span>
					<code class="url">{made.url}</code>
					<span class="muted"
						>{t('iphone.profile.until', { time: formatTime(new Date(link.expires)) })}</span
					>
					<span role="status" class="muted">{t('iphone.profile.waiting')}</span>
					{#if !made.signed}<span class="muted">{t('iphone.profile.unsignedShort')}</span>{/if}
					<span class="acts">
						<button type="button" class="btn" onclick={download}
							><Icon name="download" size={15} />{t('iphone.profile.download')}</button
						>
						<button type="button" class="btn" onclick={withdraw}
							>{t('iphone.profile.cancel')}</button
						>
					</span>
					<small class="muted">{t('iphone.profile.downloadHint')}</small>
				</div>
			{:else}
				<div class="what" role="status">
					{#if link.state === 'fetched'}
						<b class="ok"
							><Icon name="check" size={16} />{t('iphone.profile.fetched', {
								name: link.device,
							})}</b
						>
					{:else if link.state === 'failed'}
						<b class="bad">{failText(link)}</b>
					{:else if link.state === 'expired'}
						<b>{t('iphone.profile.expired')}</b>
					{:else}
						<b>{t('iphone.profile.cancelled')}</b>
					{/if}
					<span class="acts">
						<button type="button" class="btn" onclick={reset}>{t('iphone.profile.again')}</button>
					</span>
				</div>
			{/if}
		</div>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{/if}
	<div class="srow plain steps">
		<b>{t('iphone.profile.steps.title')}</b>
		<ol>
			<li>{t('iphone.profile.step1')}</li>
			<li>{t('iphone.profile.step2')}</li>
			<li>{t('iphone.profile.step3')}</li>
		</ol>
		<p class="note">{t('iphone.profile.sync')}</p>
		<p class="muted">{t('iphone.profile.reminders')}</p>
		<p class="muted">{t('iphone.profile.replaceHint')}</p>
	</div>
</SettingGroup>

{#if asking}
	<Dialog
		title={t('iphone.profile.createTitle', { name: name.trim() })}
		onclose={() => (asking = false)}
	>
		<form class="ask" onsubmit={create}>
			<ReconfirmFields bind:password bind:code />
			{#if askError}<p class="notice-error" role="alert">{askError}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" disabled={busy} onclick={() => (asking = false)}
					>{t('common.cancel')}</button
				>
				<button
					type="submit"
					class="btn btn-primary"
					disabled={busy || !reconfirmComplete(password, code)}
					>{t('iphone.profile.createConfirm')}</button
				>
			</div>
		</form>
	</Dialog>
{/if}

<style>
	.name {
		width: 16rem;
		max-width: 100%;
	}
	.field.phone {
		width: 100%;
		min-height: 44px;
	}
	select.field {
		max-width: 100%;
	}
	.line {
		display: block;
		margin-top: var(--space-1);
	}
	.go {
		display: flex;
		justify-content: flex-end;
		padding: var(--space-3);
	}
	.made {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-start;
		gap: var(--space-4);
		padding: var(--space-3);
	}
	.made.phone {
		flex-direction: column;
		align-items: stretch;
	}
	.what {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		flex: 1 1 18rem;
		min-width: 0;
		overflow-wrap: break-word;
	}
	.url {
		font-family: var(--font-mono, ui-monospace, monospace);
		font-size: 0.92em;
		padding: 2px 6px;
		border-radius: var(--radius-small);
		background: var(--chip);
		overflow-wrap: anywhere;
	}
	.acts {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
	}
	.acts .btn {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
	}
	.made.phone .btn,
	.phone .btn {
		min-height: 44px;
	}
	.ok,
	.bad {
		display: inline-flex;
		align-items: flex-start;
		gap: var(--space-1-5);
	}
	.ok {
		color: var(--ok-text);
	}
	.bad {
		color: var(--error-text);
	}
	.steps {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		padding: var(--space-3);
		overflow-wrap: break-word;
	}
	.steps ol {
		margin: 0;
		padding-left: 1.4em;
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}
	.steps p {
		margin: 0;
	}
	.note {
		font-weight: 600;
	}
	.ask {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}
	small {
		font-size: var(--text-sm);
	}
</style>
