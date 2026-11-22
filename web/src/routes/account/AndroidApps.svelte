<script lang="ts">
	// PLAN 116: Mein Konto › Geräte – the paired Android apps and the pairing
	// of a new one (docs/ANDROID-APP.md, Entwürfe 02, 05 and 92). The app
	// claims the code; the person confirms here with the password (and a
	// code), as for a device password.
	import { onDestroy } from 'svelte';
	import { api, ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import QrCode from '#lib/components/QrCode.svelte';
	import ReconfirmFields, { reconfirmComplete } from '#lib/components/ReconfirmFields.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import { formatDateTime } from '#lib/format.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';

	let { onchange }: { onchange?: () => void } = $props();

	type App = { id: string; name: string; app: string; created: string; expires?: string };
	type Address = { server: string; kind: string; certificate: string; spki: string };
	type Start = {
		id: string;
		code: string;
		manualCode: string;
		expires: string;
		link: string;
		addresses: Address[];
	};
	type Status = { state: string; name?: string; app?: string };

	let apps = $state<App[] | null>(null);
	let error = $state('');
	let pairing = $state<Start | null>(null);
	let status = $state<Status | null>(null);
	let pairError = $state('');
	let manual = $state(false);
	let password = $state('');
	let code = $state('');
	let busy = $state(false);
	let ending = $state<App | null>(null);
	// The row opens by itself once apps are paired (unless the browser
	// remembers it closed).
	let open = $state(false);
	let timer: ReturnType<typeof setInterval> | undefined;

	async function load() {
		try {
			apps = (await api.get<{ apps: App[] }>('/account/apps')).apps;
			if (apps.length) open = true;
			error = '';
		} catch (err) {
			error = apiErrorText(err);
		}
	}
	void load();

	function stopPolling() {
		if (timer) clearInterval(timer);
		timer = undefined;
	}
	onDestroy(stopPolling);

	async function poll() {
		if (!pairing) return;
		try {
			status = await api.get<Status>(`/account/apps/pair/${pairing.id}`);
			if (status.state !== 'waiting' && status.state !== 'claimed') stopPolling();
		} catch (err) {
			stopPolling();
			status = { state: 'expired' };
			if (!(err instanceof ApiError && err.status === 404)) pairError = apiErrorText(err);
		}
	}

	async function start() {
		pairError = '';
		status = null;
		manual = false;
		password = '';
		code = '';
		try {
			pairing = await api.post<Start>('/account/apps/pair', {});
			status = { state: 'waiting' };
			stopPolling();
			timer = setInterval(poll, 2000);
		} catch (err) {
			error = apiErrorText(err);
		}
	}

	async function close() {
		stopPolling();
		const open = pairing && (status?.state === 'waiting' || status?.state === 'claimed');
		const id = pairing?.id;
		pairing = null;
		status = null;
		if (open && id) await api.delete(`/account/apps/pair/${id}`).catch(() => {});
	}

	async function confirm(event: SubmitEvent) {
		event.preventDefault();
		if (!pairing) return;
		busy = true;
		pairError = '';
		try {
			await api.post(`/account/apps/pair/${pairing.id}/confirm`, { password, code: code.trim() });
			stopPolling();
			status = { ...status, state: 'confirmed' };
			password = '';
			code = '';
			await load();
			onchange?.();
		} catch (err) {
			pairError = apiErrorText(err);
			if (err instanceof ApiError && err.code === 'invalid_code') code = '';
		} finally {
			busy = false;
		}
	}

	async function reject() {
		if (!pairing) return;
		busy = true;
		try {
			await api.post(`/account/apps/pair/${pairing.id}/reject`, {});
			stopPolling();
			status = { ...status, state: 'rejected' };
		} catch (err) {
			pairError = apiErrorText(err);
		} finally {
			busy = false;
		}
	}

	async function end() {
		if (!ending) return;
		busy = true;
		try {
			await api.delete(`/account/apps/${ending.id}`);
			ending = null;
			await load();
			onchange?.();
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}

	/** The fingerprint in groups of four, as the app shows it (Entwurf 05). */
	function groups(spki: string): string {
		const bin = atob(spki.replace(/-/g, '+').replace(/_/g, '/'));
		let hex = '';
		for (let i = 0; i < 16 && i < bin.length; i++)
			hex += bin.charCodeAt(i).toString(16).padStart(2, '0');
		return (hex.toUpperCase().match(/.{4}/g) ?? []).join(' ');
	}

	const appName = (app: string) =>
		t(
			app === 'agent'
				? 'androidApps.appAgent'
				: app === 'watch'
					? 'androidApps.appWatch'
					: 'androidApps.appTitanos',
		);
</script>

<SettingRow
	bind:open
	id="acc-apps"
	icon="monitor"
	title={t('androidApps.title')}
	desc={t('androidApps.intro')}
>
	{#snippet control()}
		<button type="button" class="btn btn-primary" onclick={start}>{t('androidApps.pair')}</button>
	{/snippet}
	<div class="srow-block">
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		{#if apps && apps.length === 0}
			<p class="muted">{t('androidApps.none')}</p>
		{:else if apps}
			<ul class="list">
				{#each apps as app (app.id)}
					<li>
						<span class="text">
							<b>{app.name}</b>
							<span class="muted"
								>{appName(app.app)} · {t('androidApps.since', {
									time: formatDateTime(new Date(app.created)),
								})} · {app.expires
									? t('androidApps.until', { time: formatDateTime(new Date(app.expires)) })
									: t('androidApps.forever')}</span
							>
						</span>
						<button
							type="button"
							class="btn"
							aria-label="{t('androidApps.end')}: {app.name}"
							onclick={() => (ending = app)}>{t('androidApps.end')}</button
						>
					</li>
				{/each}
			</ul>
		{/if}
	</div>
</SettingRow>

{#if pairing}
	<Dialog title={t('androidApps.pairTitle')} onclose={close}>
		{#if status?.state === 'claimed'}
			<form class="ask" onsubmit={confirm}>
				<p>
					<b>{t('androidApps.asks', { name: status.name ?? '' })}</b>
					{t('androidApps.asksHint', { app: appName(status.app ?? '') })}
				</p>
				<ReconfirmFields bind:password bind:code />
				{#if pairError}<p class="notice-error" role="alert">{pairError}</p>{/if}
				<div class="dialog-actions">
					<button type="button" class="btn" disabled={busy} onclick={reject}
						>{t('androidApps.reject')}</button
					>
					<button
						type="submit"
						class="btn btn-primary"
						disabled={busy || !reconfirmComplete(password, code)}>{t('androidApps.confirm')}</button
					>
				</div>
			</form>
		{:else if status?.state === 'confirmed'}
			<p>{t('androidApps.confirmed', { name: status.name ?? '' })}</p>
			<div class="dialog-actions">
				<button type="button" class="btn btn-primary" onclick={close}>{t('common.done')}</button>
			</div>
		{:else if status?.state === 'rejected' || status?.state === 'expired'}
			<p>{t(status.state === 'rejected' ? 'androidApps.rejected' : 'androidApps.expired')}</p>
			{#if pairError}<p class="notice-error" role="alert">{pairError}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" onclick={close}>{t('common.cancel')}</button>
				<button type="button" class="btn btn-primary" onclick={start}
					>{t('androidApps.again')}</button
				>
			</div>
		{:else}
			<div class="pair">
				<a class="btn btn-primary wide" href={pairing.link}>{t('androidApps.openHere')}</a>
				<p class="muted">{t('androidApps.orScan')}</p>
				<QrCode text={pairing.link} label={t('androidApps.qrLabel')} />
				<p class="muted">{t('androidApps.validity')}</p>
				<button type="button" class="btn" aria-expanded={manual} onclick={() => (manual = !manual)}
					>{t('androidApps.withoutQr')}</button
				>
				{#if manual}
					<div class="manual">
						<p>{t('androidApps.manualHint')}</p>
						<p>
							{t('androidApps.manualCode')} <span class="mono big">{pairing.manualCode}</span>
						</p>
						<dl>
							{#each pairing.addresses as address (address.server)}
								<dt class="mono">{address.server}</dt>
								<dd class="mono">
									{address.certificate === 'valid'
										? t('androidApps.publicCert')
										: address.spki
											? groups(address.spki)
											: '–'}
								</dd>
							{/each}
						</dl>
					</div>
				{/if}
			</div>
			{#if pairError}<p class="notice-error" role="alert">{pairError}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" onclick={close}>{t('common.cancel')}</button>
			</div>
		{/if}
	</Dialog>
{/if}

{#if ending}
	<Dialog title={t('androidApps.endTitle', { name: ending.name })} onclose={() => (ending = null)}>
		<p>{t('androidApps.endText')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (ending = null)}>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-danger" disabled={busy} onclick={end}
				>{t('androidApps.end')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.list {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}
	.list li {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		flex-wrap: wrap;
	}
	.text {
		flex: 1;
		min-width: 12rem;
		display: flex;
		flex-direction: column;
		overflow-wrap: anywhere;
	}
	.pair {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: var(--space-3);
		text-align: center;
	}
	.wide {
		align-self: stretch;
		justify-content: center;
	}
	.manual {
		align-self: stretch;
		text-align: left;
	}
	.manual dl {
		display: grid;
		grid-template-columns: auto 1fr;
		gap: var(--space-1) var(--space-3);
	}
	.manual dd {
		margin: 0;
		overflow-wrap: anywhere;
	}
	.big {
		font-size: var(--text-lg);
		font-weight: 600;
		letter-spacing: 0.1em;
	}
	.ask {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}
</style>
