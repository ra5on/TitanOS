<script lang="ts">
	// Settings › WireGuard (backend package wgserver): the VPN server of the
	// NAS, as simple as it gets: one switch, devices by name with a QR code.
	import { onMount } from 'svelte';
	import { api, ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import QrCode from '#lib/components/QrCode.svelte';
	import { formatBytes, formatTime } from '#lib/format.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { configFileName, connected, validEndpoint, type WireGuardPeer, type WireGuardStatus } from '#lib/wireguard.ts';

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
			: errorText(err instanceof ApiError ? err.code : 'internal');
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
			fill(await api.put<WireGuardStatus>('/network/wireguard', { enabled, endpoint: endpoint.trim(), port: status.port, network: status.network, fullTunnel, dns: dns.trim() }));
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
			const answer = await api.post<{ name: string; config: string }>('/network/wireguard/peers', { name: newName.trim() });
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
			fill(await api.post<WireGuardStatus>('/network/wireguard/peers/remove', { publicKey: peer.publicKey, name: peer.name }));
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

<PageHead title={t('wg.title')} parent={{ href: '/settings', label: t('nav.settings') }}>
	<p class="muted">{t('wg.intro')}</p>
</PageHead>

{#if loadError}
	<div class="notice-error" role="alert">{loadError} <button class="btn" onclick={() => load()}>{t('ui.retry')}</button></div>
{:else if !status}
	<p class="muted loading">{t('common.loading')}</p>
{:else}
	<form class="tile panel" id="wg-server" onsubmit={save}>
		<h2 class="tile-title">{t('wg.server')}</h2>
		{#if status.disabled}<p class="notice-warn">{t('wg.devMode')}</p>{/if}
		<label class="check"><input type="checkbox" bind:checked={enabled} disabled={busy} /><span>{t('wg.enabled')}</span></label>
		<label class="stack"
			><span>{t('wg.endpoint')}</span><input
				class="field mono"
				type="text"
				bind:value={endpoint}
				placeholder="nas.example.org"
				spellcheck="false"
				autocapitalize="off"
				disabled={busy}
			/><small class="muted">{t('wg.endpointHint')}</small></label
		>
		{#if !endpointOk}<p class="notice-warn" role="alert">{t('wg.endpointInvalid')}</p>{/if}
		<fieldset class="choice">
			<legend>{t('wg.access')}</legend>
			<label class="check"><input type="radio" name="access" value={false} bind:group={fullTunnel} disabled={busy} /><span>{t('wg.accessHome')}</span></label>
			<label class="check"><input type="radio" name="access" value={true} bind:group={fullTunnel} disabled={busy} /><span>{t('wg.accessAll')}</span></label>
		</fieldset>
		<label class="stack"
			><span>{t('wg.dns')}</span><input class="field mono" type="text" bind:value={dns} placeholder="192.168.178.1" spellcheck="false" disabled={busy} /><small class="muted"
				>{t('wg.dnsHint')}</small
			></label
		>
		{#if status.network}
			<p class="muted small">{t('wg.details', { network: status.network, port: String(status.port) })}</p>
		{/if}
		<p class="muted small">{t('wg.router', { port: String(status.port) })}</p>
		{#if status.error}<p class="notice-error" role="alert">{status.error}</p>{/if}
		{#if error && !adding}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="row-actions">
			<button class="btn btn-primary" disabled={!canSave}>{t('common.save')}</button>
			{#if busy || saved}<span class="muted" role="status">{t(busy ? 'ui.saving' : 'ui.saved')}</span>{/if}
			{#if status.enabled}<span class="chip" class:ok={status.running}>{t(status.running ? 'wg.running' : 'wg.notRunning')}</span>{/if}
		</div>
	</form>

	<section class="tile panel" id="wg-devices" aria-labelledby="wg-devices-title">
		<div class="head">
			<h2 class="tile-title" id="wg-devices-title">{t('wg.devices')}</h2>
			<button type="button" class="btn" disabled={busy || !status.enabled || !status.network} onclick={() => ((adding = true), (error = ''))}>{t('wg.add')}</button>
		</div>
		{#if !status.enabled}<p class="muted">{t('wg.enableFirst')}</p>{/if}
		{#if status.peers.length}
			<ul class="peers divided">
				{#each status.peers as peer (peer.publicKey)}
					<li>
						<span class="name">{peer.name}</span>
						<span class="mono small">{peer.address.replace('/32', '')}</span>
						<span class="chip" class:ok={connected(peer, now)}>{t(connected(peer, now) ? 'wg.connected' : 'wg.notConnected')}</span>
						<span class="muted small"
							>{peer.lastHandshake ? t('wg.lastSeen', { time: formatTime(new Date(peer.lastHandshake)) }) : t('wg.never')}{#if peer.received || peer.sent}
								· ↓ {formatBytes(peer.sent)} · ↑ {formatBytes(peer.received)}{/if}</span
						>
						<button type="button" class="btn small" disabled={busy} onclick={() => (removing = peer)}>{t('wg.remove')}</button>
					</li>
				{/each}
			</ul>
		{:else if status.enabled}
			<p class="muted">{t('wg.none')}</p>
		{/if}
		<p class="muted small">{t('wg.apps')}</p>
	</section>
{/if}

{#if adding}
	<Dialog title={t('wg.add')} onclose={() => (adding = false)}>
		<form onsubmit={add}>
			<label class="stack"><span>{t('wg.deviceName')}</span><input class="field" type="text" bind:value={newName} maxlength="40" placeholder={t('wg.deviceNamePlaceholder')} /></label>
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" onclick={() => (adding = false)}>{t('common.cancel')}</button>
				<button class="btn btn-primary" disabled={busy || !newName.trim()}>{t('wg.create')}</button>
			</div>
		</form>
	</Dialog>
{/if}

{#if created}
	<Dialog title={t('wg.createdTitle', { name: created.name })} onclose={() => (created = null)}>
		<p>{t('wg.createdHint')}</p>
		<div class="qr"><QrCode text={created.config} label={t('wg.qrLabel', { name: created.name })} /></div>
		<details>
			<summary>{t('wg.showConfig')}</summary>
			<pre class="mono config">{created.config}</pre>
		</details>
		<p class="notice-warn">{t('wg.onceOnly')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => created && download(created.name, created.config)}>{t('wg.download')}</button>
			<button type="button" class="btn btn-primary" onclick={() => (created = null)}>{t('wg.done')}</button>
		</div>
	</Dialog>
{/if}

{#if removing}
	<Dialog title={t('wg.remove')} onclose={() => (removing = null)}>
		<p>{t('wg.removeText', { name: removing.name })}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (removing = null)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-danger" disabled={busy} onclick={() => removing && remove(removing)}>{t('wg.remove')}</button>
		</div>
	</Dialog>
{/if}

<style>
	.head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
	}
	.choice {
		border: 0;
		padding: 0;
		margin: 0;
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
	legend {
		font-weight: 500;
		margin-bottom: 4px;
	}
	.peers {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.peers li {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 6px 12px;
		padding: 8px 0;
	}
	.peers .name {
		font-weight: 500;
	}
	.peers button {
		margin-left: auto;
	}
	.qr {
		display: flex;
		justify-content: center;
		margin: 8px 0;
	}
	.config {
		white-space: pre-wrap;
		overflow-wrap: anywhere;
		font-size: 0.75rem;
		max-height: 14rem;
		overflow: auto;
	}
</style>
