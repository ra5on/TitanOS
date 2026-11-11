<script lang="ts">
	// Settings › WireGuard (backend package wgserver): the VPN server of the
	// NAS, as simple as it gets: one switch, devices by name with a QR code.
	import { onMount } from 'svelte';
	import { api, ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import QrCode from '#lib/components/QrCode.svelte';
	import { formatBytes, formatTime } from '#lib/format.ts';
	import { apiErrorText, errorText, t } from '#lib/i18n/index.svelte.ts';
	import {
		configFileName,
		connected,
		validEndpoint,
		type WireGuardPeer,
		type WireGuardStatus,
	} from '#lib/wireguard.ts';

	let status = $state<WireGuardStatus | null>(null);
	let loadError = $state('');
	let busy = $state(false);
	let error = $state('');
	let saved = $state(false);
	let now = $state(Date.now());

	let enabled = $state(false);
	let endpoint = $state('');
	let fullTunnel = $state(false);
	let dns = $state('');

	let adding = $state(false);
	let newName = $state('');
	let created = $state<{ name: string; config: string } | null>(null);
	let removing = $state<WireGuardPeer | null>(null);

	function fill(next: WireGuardStatus) {
		status = next;
		enabled = next.enabled;
		endpoint = next.endpoint;
		fullTunnel = next.fullTunnel;
		dns = next.dns;
	}

	function problem(err: unknown): string {
		return err instanceof ApiError && err.code === 'invalid_wireguard'
			? `${errorText(err.code)} ${err.message.replace(/^wireguard: invalid settings: ?/, '')}`
			: apiErrorText(err);
	}

	async function load() {
		loadError = '';
		try {
			fill(await api.get<WireGuardStatus>('/network/wireguard'));
			now = Date.now();
		} catch (err) {
			loadError = problem(err);
		}
	}
	onMount(() => {
		void load();
	});

	const endpointOk = $derived(validEndpoint(endpoint));
	const canSave = $derived(!busy && endpointOk && (!enabled || endpoint.trim() !== ''));

	async function save(event: SubmitEvent) {
		event.preventDefault();
		if (!canSave || !status) return;
		busy = true;
		error = '';
		saved = false;
		try {
			fill(
				await api.put<WireGuardStatus>('/network/wireguard', {
					enabled,
					endpoint: endpoint.trim(),
					port: status.port,
					network: status.network,
					fullTunnel,
					dns: dns.trim(),
				}),
			);
			saved = true;
		} catch (err) {
			error = problem(err);
		} finally {
			busy = false;
		}
	}

	async function add(event: SubmitEvent) {
		event.preventDefault();
		if (busy || !newName.trim()) return;
		busy = true;
		error = '';
		try {
			const answer = await api.post<{ name: string; config: string }>('/network/wireguard/peers', {
				name: newName.trim(),
			});
			created = answer;
			adding = false;
			newName = '';
			await load();
		} catch (err) {
			error = problem(err);
		} finally {
			busy = false;
		}
	}

	async function remove(peer: WireGuardPeer) {
		busy = true;
		error = '';
		try {
			fill(
				await api.post<WireGuardStatus>('/network/wireguard/peers/remove', {
					publicKey: peer.publicKey,
					name: peer.name,
				}),
			);
			removing = null;
		} catch (err) {
			error = problem(err);
		} finally {
			busy = false;
		}
	}

	function download(name: string, text: string) {
		const url = URL.createObjectURL(new Blob([text], { type: 'text/plain' }));
		const a = document.createElement('a');
		a.href = url;
		a.download = configFileName(name);
		a.click();
		URL.revokeObjectURL(url);
	}
</script>

<PageHead title={t('wg.title')}>
	<p class="muted">{t('wg.intro')}</p>
</PageHead>

{#if loadError}
	<div class="notice-error" role="alert">
		{loadError} <button class="btn" onclick={() => load()}>{t('ui.retry')}</button>
	</div>
{:else if !status}
	<p class="muted loading">{t('common.loading')}</p>
{:else}
	<!-- PLAN 78-1: the server's settings are rows; one Save for all of them
	     (one request, and address and DNS are typed text). -->
	<form class="rows" onsubmit={save}>
		<SettingGroup title={t('wg.server')} id="wg-server">
			{#if status.disabled}<p class="notice-warn">{t('wg.devMode')}</p>{/if}
			<SettingRow icon="shield" title={t('wg.enabled')} for="wg-enabled">
				{#snippet control()}
					{#if status!.enabled}<span class="chip" class:ok={status!.running}
							>{t(status!.running ? 'wg.running' : 'wg.notRunning')}</span
						>{/if}
					<input
						id="wg-enabled"
						class="switch"
						type="checkbox"
						role="switch"
						bind:checked={enabled}
						disabled={busy}
					/>
				{/snippet}
			</SettingRow>
			<SettingRow
				icon="globe"
				title={t('wg.endpoint')}
				desc={t('wg.endpointHint')}
				for="wg-endpoint"
			>
				{#snippet control()}
					<input
						id="wg-endpoint"
						class="field mono"
						type="text"
						bind:value={endpoint}
						placeholder="nas.example.org"
						spellcheck="false"
						autocapitalize="off"
						disabled={busy}
					/>
				{/snippet}
			</SettingRow>
			{#if !endpointOk}<p class="notice-warn" role="alert">{t('wg.endpointInvalid')}</p>{/if}
			<SettingRow
				id="wg-access"
				icon="tailnet"
				title={t('wg.access')}
				summary={t(fullTunnel ? 'wg.accessAllShort' : 'wg.accessHomeShort')}
				open
			>
				<div class="srow-block">
					<fieldset class="choice">
						<legend class="sr-only">{t('wg.access')}</legend>
						<label class="check"
							><input
								type="radio"
								name="access"
								value={false}
								bind:group={fullTunnel}
								disabled={busy}
							/><span>{t('wg.accessHome')}</span></label
						>
						<label class="check"
							><input
								type="radio"
								name="access"
								value={true}
								bind:group={fullTunnel}
								disabled={busy}
							/><span>{t('wg.accessAll')}</span></label
						>
					</fieldset>
				</div>
			</SettingRow>
			<SettingRow icon="search" title={t('wg.dns')} desc={t('wg.dnsHint')} for="wg-dns">
				{#snippet control()}
					<input
						id="wg-dns"
						class="field mono"
						type="text"
						bind:value={dns}
						placeholder="192.168.178.1"
						spellcheck="false"
						disabled={busy}
					/>
				{/snippet}
			</SettingRow>
			{#if status.network}
				<p class="muted small">
					{t('wg.details', { network: status.network, port: String(status.port) })}
				</p>
			{/if}
			<p class="muted small">{t('wg.router', { port: String(status.port) })}</p>
			{#if status.error}<p class="notice-error" role="alert">{status.error}</p>{/if}
			{#if error && !adding}<p class="notice-error" role="alert">{error}</p>{/if}
			<div class="row-actions">
				<button class="btn btn-primary" disabled={!canSave}>{t('common.save')}</button>
				{#if busy || saved}<span class="muted" role="status"
						>{t(busy ? 'ui.saving' : 'ui.saved')}</span
					>{/if}
			</div>
		</SettingGroup>
	</form>

	<!-- Every device is a row: name, when it was last seen, its address and
	     state, and "Entfernen". -->
	<SettingGroup title={t('wg.devices')} id="wg-devices">
		{#snippet tools()}
			<button
				type="button"
				class="btn"
				disabled={busy || !status!.enabled || !status!.network}
				onclick={() => ((adding = true), (error = ''))}>{t('wg.add')}</button
			>
		{/snippet}
		{#if !status.enabled}<p class="muted">{t('wg.enableFirst')}</p>{/if}
		{#each status.peers as peer (peer.publicKey)}
			<SettingRow
				icon="monitor"
				title={peer.name}
				desc={(peer.lastHandshake
					? t('wg.lastSeen', { time: formatTime(new Date(peer.lastHandshake)) })
					: t('wg.never')) +
					(peer.received || peer.sent
						? ` · ↓ ${formatBytes(peer.sent)} · ↑ ${formatBytes(peer.received)}`
						: '')}
			>
				{#snippet control()}
					<span class="mono">{peer.address.replace('/32', '')}</span>
					<span class="chip" class:ok={connected(peer, now)}
						>{t(connected(peer, now) ? 'wg.connected' : 'wg.notConnected')}</span
					>
					<button type="button" class="btn" disabled={busy} onclick={() => (removing = peer)}
						>{t('wg.remove')}</button
					>
				{/snippet}
			</SettingRow>
		{:else}
			{#if status.enabled}<p class="muted">{t('wg.none')}</p>{/if}
		{/each}
		<p class="muted small">{t('wg.apps')}</p>
	</SettingGroup>
{/if}

{#if adding}
	<Dialog title={t('wg.add')} onclose={() => (adding = false)}>
		<form onsubmit={add}>
			<label class="stack"
				><span>{t('wg.deviceName')}</span><input
					class="field"
					type="text"
					bind:value={newName}
					maxlength="40"
					placeholder={t('wg.deviceNamePlaceholder')}
				/></label
			>
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" onclick={() => (adding = false)}
					>{t('common.cancel')}</button
				>
				<button class="btn btn-primary" disabled={busy || !newName.trim()}>{t('wg.create')}</button>
			</div>
		</form>
	</Dialog>
{/if}

{#if created}
	<Dialog title={t('wg.createdTitle', { name: created.name })} onclose={() => (created = null)}>
		<p>{t('wg.createdHint')}</p>
		<div class="qr">
			<QrCode text={created.config} label={t('wg.qrLabel', { name: created.name })} />
		</div>
		<details>
			<summary>{t('wg.showConfig')}</summary>
			<pre class="mono config">{created.config}</pre>
		</details>
		<p class="notice-warn">{t('wg.onceOnly')}</p>
		<div class="dialog-actions">
			<button
				type="button"
				class="btn"
				onclick={() => created && download(created.name, created.config)}>{t('wg.download')}</button
			>
			<button type="button" class="btn btn-primary" onclick={() => (created = null)}
				>{t('wg.done')}</button
			>
		</div>
	</Dialog>
{/if}

{#if removing}
	<Dialog title={t('wg.remove')} onclose={() => (removing = null)}>
		<p>{t('wg.removeText', { name: removing.name })}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (removing = null)}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-danger"
				disabled={busy}
				onclick={() => removing && remove(removing)}>{t('wg.remove')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	form.rows {
		display: contents;
	}
	.small {
		font-size: var(--text-base);
	}
	.choice {
		border: 0;
		padding: 0;
		margin: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-2-5);
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
	.row-actions {
		margin-top: var(--space-2);
	}
	.qr {
		display: flex;
		justify-content: center;
		margin: var(--space-2) 0;
	}
	.config {
		white-space: pre-wrap;
		overflow-wrap: anywhere;
		font-size: var(--text-sm);
		max-height: 14rem;
		overflow: auto;
	}
</style>
