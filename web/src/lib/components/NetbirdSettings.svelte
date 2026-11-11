<script lang="ts">
	import { poll } from '#lib/poll.ts';
	import { api } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { formatTime } from '#lib/format.ts';
	import Dialog from './Dialog.svelte';
	import SettingGroup from './settings/SettingGroup.svelte';
	import SettingSub from './settings/SettingSub.svelte';
	let {
		onpending = (_pending: boolean) => {},
		row = false,
	}: {
		onpending?: (pending: boolean) => void;
		/** Inside an open row of the control panel: only the parts, no heading. */
		row?: boolean;
	} = $props();
	type Settings = {
		managementUrl: string;
		acceptRoutes: boolean;
		serveRoutes: boolean;
		acceptDNS: boolean;
	};
	type Status = {
		settings: Settings;
		setupKeySet: boolean;
		connect: boolean;
		active: boolean;
		readOnly: boolean;
		state: string;
		version: string;
		fqdn: string;
		ips: string[];
		management: string;
		managementConnected: boolean;
		signalConnected: boolean;
		peersConnected: number;
		peersTotal: number;
		health: string[];
		pendingUntil?: string;
		error?: string;
	};
	let status = $state<Status | null>(null),
		busy = $state(false),
		error = $state(''),
		dirty = $state(false),
		loggingOut = $state(false);
	let managementUrl = $state(''),
		acceptRoutes = $state(false),
		serveRoutes = $state(false),
		acceptDNS = $state(false),
		setupKey = $state('');
	const stateKeys = {
		inactive: 'netbird.states.inactive',
		Idle: 'netbird.states.Idle',
		Connecting: 'netbird.states.Connecting',
		Connected: 'netbird.states.Connected',
		NeedsLogin: 'netbird.states.NeedsLogin',
		LoginFailed: 'netbird.states.LoginFailed',
		SessionExpired: 'netbird.states.SessionExpired',
	} as const;
	const locked = $derived(busy || !status?.active || status.readOnly || !!status.pendingUntil);
	const online = $derived(status?.state === 'Connected' || status?.state === 'Connecting');
	const needsLogin = $derived(
		status?.state === 'NeedsLogin' ||
			status?.state === 'LoginFailed' ||
			status?.state === 'SessionExpired',
	);
	function take(value: Status, reset = false) {
		status = value;
		onpending(!!value.pendingUntil);
		if (reset || !dirty) {
			const s = value.settings;
			managementUrl = s.managementUrl;
			acceptRoutes = s.acceptRoutes;
			serveRoutes = s.serveRoutes;
			acceptDNS = s.acceptDNS;
			dirty = false;
		}
	}
	function fail(err: unknown) {
		error = apiErrorText(err, { detail: true });
	}
	// Bumped by every save/control: a poll that started before it answers with the old state and is dropped.
	let generation = 0;
	async function load() {
		if (busy) return;
		const asked = generation;
		try {
			const value = await api.get<Status>('/plugins/netbird');
			if (asked === generation && !busy) take(value);
		} catch (err) {
			if (!status && asked === generation) fail(err);
		}
	}
	$effect(() => {
		const stop = poll(load, { interval: 5000 });
		return () => {
			stop();
			onpending(false);
		};
	});
	async function run(call: () => Promise<Status>, reset = true) {
		generation++;
		busy = true;
		error = '';
		try {
			take(await call(), reset);
			loggingOut = false;
			return true;
		} catch (err) {
			fail(err);
			return false;
		} finally {
			busy = false;
		}
	}
	const action = (name: string) =>
		run(() => api.post<Status>(`/plugins/netbird/control/${name}`, {}));
	// The key leaves the browser once; the field is emptied whatever happens.
	async function saveKey(event: SubmitEvent) {
		event.preventDefault();
		const value = setupKey;
		setupKey = '';
		await run(() => api.put<Status>('/plugins/netbird/setup-key', { setupKey: value }), false);
	}
	const removeKey = () =>
		run(() => api.put<Status>('/plugins/netbird/setup-key', { setupKey: '' }), false);
	async function save(event: SubmitEvent) {
		event.preventDefault();
		await run(() =>
			api.put<Status>('/plugins/netbird', {
				managementUrl: managementUrl.trim(),
				acceptRoutes,
				serveRoutes,
				acceptDNS,
			}),
		);
	}
</script>

<!-- PLAN 78-5: the settings are the parts of a row (name left, control
     right). `row`: inside an open row of Settings › Network; otherwise
     (Plugins) the parts stand in a row of their own under a plain heading. -->
{#snippet parts()}
	<section class="sparts" aria-label="NetBird">
		{#if error}<div class="srow-block"><p class="notice-error" role="alert">{error}</p></div>{/if}
		{#if status}
			<SettingSub title={status.fqdn || t('netbird.connection')}>
				{#snippet description()}
					{#each status?.ips ?? [] as ip (ip)}<span class="mono address">{ip}</span>{/each}
					{#if status?.active && status.management}<span class="line">
							{t('netbird.managementState', {
								url: status.management,
								state: t(status.managementConnected ? 'netbird.reachable' : 'netbird.unreachable'),
							})}
						</span>{/if}
					{#if status?.state === 'Connected'}<span class="line">
							{t('netbird.peerCount', {
								connected: status.peersConnected,
								total: status.peersTotal,
							})}
						</span>{/if}
				{/snippet}
				{#snippet control()}
					<span class="chip" class:ok={status?.state === 'Connected'}
						>{status && status.state in stateKeys
							? t(stateKeys[status.state as keyof typeof stateKeys])
							: status?.state}</span
					><span class="mono">{status?.version}</span>
				{/snippet}
			</SettingSub>
			<div class="srow-block">
				{#if status.error}<p class="notice-warn">{status.error}</p>{/if}
				{#each status.health as warning (warning)}<p class="notice-warn">{warning}</p>{/each}
				{#if !status.active}<p class="muted">{t('netbird.activateHint')}</p>{/if}
				{#if needsLogin && !status.setupKeySet}<p class="muted">{t('netbird.keyNeeded')}</p>{/if}
				<div class="actions">
					{#if !online}<button
							class="btn"
							disabled={locked || (needsLogin && !status.setupKeySet)}
							onclick={() => action('connect')}>{t('netbird.connect')}</button
						>
					{:else}<button class="btn" disabled={locked} onclick={() => action('disconnect')}
							>{t('netbird.disconnect')}</button
						>{/if}
					{#if !needsLogin && status.active}<button
							class="btn"
							disabled={locked}
							onclick={() => (loggingOut = true)}>{t('netbird.logout')}</button
						>{/if}
				</div>
				{#if status.pendingUntil}
					<div class="notice-warn" role="status">
						<p>{t('netbird.trial', { time: formatTime(new Date(status.pendingUntil)) })}</p>
						<div class="actions">
							<button class="btn btn-primary" disabled={busy} onclick={() => action('confirm')}
								>{t('netbird.confirm')}</button
							><button class="btn" disabled={busy} onclick={() => action('undo')}
								>{t('netbird.undo')}</button
							>
						</div>
					</div>
				{/if}
			</div>
			<form onsubmit={saveKey}>
				<fieldset class="sparts" disabled={busy || status.readOnly || !!status.pendingUntil}>
					<SettingSub title={t('netbird.setupKey')} for="nb-key">
						{#snippet description()}
							{status?.setupKeySet ? t('netbird.keySet') : t('netbird.keyUnset')}
							<span class="line">{t('netbird.keyHint')}</span>
						{/snippet}
						{#snippet control()}
							<input
								id="nb-key"
								class="field mono"
								type="password"
								autocomplete="off"
								spellcheck="false"
								bind:value={setupKey}
								placeholder={t('netbird.keyPlaceholder')}
							/>
						{/snippet}
						<div class="actions">
							<button class="btn btn-primary" disabled={!setupKey.trim()}
								>{t('netbird.saveKey')}</button
							>{#if status.setupKeySet}<button type="button" class="btn" onclick={removeKey}
									>{t('netbird.removeKey')}</button
								>{/if}
						</div>
					</SettingSub>
				</fieldset>
			</form>
			<form onsubmit={save} oninput={() => (dirty = true)} onchange={() => (dirty = true)}>
				<fieldset class="sparts" disabled={locked}>
					<SettingSub
						title={t('netbird.management')}
						desc={t('netbird.managementHint')}
						for="nb-management"
					>
						{#snippet control()}
							<input
								id="nb-management"
								class="field mono"
								type="url"
								bind:value={managementUrl}
								placeholder="https://api.netbird.io:443"
							/>
						{/snippet}
					</SettingSub>
					<SettingSub title={t('netbird.acceptRoutes')} for="nb-accept-routes">
						{#snippet control()}<input
								id="nb-accept-routes"
								class="switch"
								type="checkbox"
								role="switch"
								bind:checked={acceptRoutes}
							/>{/snippet}
					</SettingSub>
					<SettingSub title={t('netbird.serveRoutes')} for="nb-serve-routes">
						{#snippet control()}<input
								id="nb-serve-routes"
								class="switch"
								type="checkbox"
								role="switch"
								bind:checked={serveRoutes}
							/>{/snippet}
					</SettingSub>
					<SettingSub title={t('netbird.acceptDNS')} for="nb-accept-dns">
						{#snippet control()}<input
								id="nb-accept-dns"
								class="switch"
								type="checkbox"
								role="switch"
								bind:checked={acceptDNS}
							/>{/snippet}
					</SettingSub>
					<div class="srow-block">
						<p class="muted">{t('netbird.policyHint')}</p>
						<div class="actions">
							<button class="btn btn-primary" disabled={!dirty}>{t('netbird.apply')}</button>
						</div>
					</div>
				</fieldset>
			</form>
			<SettingSub title={t('netbird.firewall')} desc={t('netbird.firewallHint')} />
		{/if}
	</section>
{/snippet}

{#if row}{@render parts()}{:else}
	<SettingGroup title={t('netbird.connection')} id="netbird-connection">
		<div class="srow plain">{@render parts()}</div>
	</SettingGroup>
{/if}
{#if loggingOut}<Dialog title={t('netbird.logout')} onclose={() => (loggingOut = false)}
		><p>{t('netbird.logoutHint')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (loggingOut = false)}
				>{t('common.cancel')}</button
			><button type="button" class="btn btn-danger" disabled={busy} onclick={() => action('logout')}
				>{t('netbird.logout')}</button
			>
		</div></Dialog
	>{/if}

<style>
	.actions {
		display: flex;
		gap: var(--space-2-5);
		flex-wrap: wrap;
		align-items: center;
	}
	.mono {
		font-family: var(--font-mono);
	}
	.address,
	.line {
		display: block;
		overflow-wrap: anywhere;
	}
	p {
		margin: 0;
	}
	.notice-error {
		white-space: pre-wrap;
	}
</style>
