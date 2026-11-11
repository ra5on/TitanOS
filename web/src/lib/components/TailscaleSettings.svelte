<script lang="ts">
	import { poll } from '#lib/poll.ts';
	import { api } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { formatTime } from '#lib/format.ts';
	import Dialog from './Dialog.svelte';
	import SettingGroup from './settings/SettingGroup.svelte';
	import SettingSub from './settings/SettingSub.svelte';
	import TailscaleHttps, { type HttpsStatus } from './TailscaleHttps.svelte';
	let {
		onpending = (_pending: boolean) => {},
		row = false,
	}: {
		onpending?: (pending: boolean) => void;
		/** Inside an open row of the control panel: only the parts, no heading. */
		row?: boolean;
	} = $props();
	type Settings = {
		routes: string[];
		advertiseExit: boolean;
		exitNode: string;
		acceptRoutes: boolean;
		acceptDNS: boolean;
	};
	type Status = {
		settings: Settings;
		active: boolean;
		readOnly: boolean;
		state: string;
		version: string;
		name: string;
		ips: string[];
		authUrl?: string;
		exitNodes: { id: string; name: string; ip: string; online: boolean }[];
		health: string[];
		pendingUntil?: string;
		error?: string;
		https?: HttpsStatus;
	};
	let status = $state<Status | null>(null),
		busy = $state(false),
		error = $state(''),
		dirty = $state(false),
		loggingOut = $state(false);
	let routes = $state(''),
		mode = $state('none'),
		exitNode = $state(''),
		acceptRoutes = $state(false),
		acceptDNS = $state(false);
	const stateKeys = {
		inactive: 'tailscale.states.inactive',
		Running: 'tailscale.states.Running',
		Stopped: 'tailscale.states.Stopped',
		NeedsLogin: 'tailscale.states.NeedsLogin',
		NeedsMachineAuth: 'tailscale.states.NeedsMachineAuth',
		Starting: 'tailscale.states.Starting',
		NoState: 'tailscale.states.NoState',
		InUseOtherUser: 'tailscale.states.InUseOtherUser',
	} as const;
	const locked = $derived(busy || !status?.active || status.readOnly || !!status.pendingUntil);
	function take(value: Status, reset = false) {
		status = value;
		onpending(!!value.pendingUntil);
		if (reset || !dirty) {
			const s = value.settings;
			routes = s.routes.join('\n');
			mode = s.advertiseExit ? 'provide' : s.exitNode ? 'use' : 'none';
			exitNode = s.exitNode;
			acceptRoutes = s.acceptRoutes;
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
			const value = await api.get<Status>('/plugins/tailscale');
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
	async function action(name: string) {
		generation++;
		busy = true;
		error = '';
		try {
			take(await api.post<Status>(`/plugins/tailscale/control/${name}`, {}), true);
			loggingOut = false;
		} catch (err) {
			fail(err);
		} finally {
			busy = false;
		}
	}
	async function save(event: SubmitEvent) {
		event.preventDefault();
		generation++;
		busy = true;
		error = '';
		try {
			take(
				await api.put<Status>('/plugins/tailscale', {
					routes: routes.split(/[\s,]+/).filter(Boolean),
					advertiseExit: mode === 'provide',
					exitNode: mode === 'use' ? exitNode : '',
					acceptRoutes,
					acceptDNS,
				}),
				true,
			);
		} catch (err) {
			fail(err);
		} finally {
			busy = false;
		}
	}
</script>

<!-- PLAN 78-5: the settings are the parts of a row (name left, control
     right). `row`: inside an open row of Settings › Network; otherwise
     (Plugins) the parts stand in a row of their own under a plain heading. -->
{#snippet parts()}
	<section class="sparts" aria-label="Tailscale">
		{#if error}<div class="srow-block"><p class="notice-error" role="alert">{error}</p></div>{/if}
		{#if status}
			<SettingSub title={status.name || t('tailscale.connection')}>
				{#snippet description()}
					{#each status?.ips ?? [] as ip (ip)}<span class="mono address">{ip}</span>{/each}
				{/snippet}
				{#snippet control()}
					<span class="chip" class:ok={status?.state === 'Running'}
						>{status && status.state in stateKeys
							? t(stateKeys[status.state as keyof typeof stateKeys])
							: status?.state}</span
					><span class="mono">{status?.version}</span>
				{/snippet}
			</SettingSub>
			<div class="srow-block">
				{#if status.error}<p class="notice-warn">{status.error}</p>{/if}
				{#each status.health as warning (warning)}<p class="notice-warn">{warning}</p>{/each}
				{#if !status.active}<p class="muted">{t('tailscale.activateHint')}</p>{/if}
				<div class="actions">
					{#if status.authUrl}<a
							class="btn btn-primary"
							href={status.authUrl}
							target="_blank"
							rel="noopener noreferrer">{t('tailscale.login')}</a
						>{/if}
					{#if status.state !== 'Running'}<button
							class="btn"
							disabled={locked}
							onclick={() => action('connect')}>{t('tailscale.connect')}</button
						>
					{:else}<button class="btn" disabled={locked} onclick={() => action('disconnect')}
							>{t('tailscale.disconnect')}</button
						>{/if}
					{#if status.state !== 'NeedsLogin' && status.active}<button
							class="btn"
							disabled={locked}
							onclick={() => (loggingOut = true)}>{t('tailscale.logout')}</button
						>{/if}
				</div>
				{#if status.pendingUntil}
					<div class="notice-warn" role="status">
						<p>{t('tailscale.trial', { time: formatTime(new Date(status.pendingUntil)) })}</p>
						<div class="actions">
							<button class="btn btn-primary" disabled={busy} onclick={() => action('confirm')}
								>{t('tailscale.confirm')}</button
							><button class="btn" disabled={busy} onclick={() => action('undo')}
								>{t('tailscale.undo')}</button
							>
						</div>
					</div>
				{/if}
			</div>
			<TailscaleHttps
				https={status.https}
				disabled={busy || status.readOnly || !!status.pendingUntil}
				onupdate={(value) => take(value as Status)}
			/>
			<form onsubmit={save} oninput={() => (dirty = true)} onchange={() => (dirty = true)}>
				<fieldset class="sparts" disabled={locked}>
					<SettingSub
						title={t('tailscale.routes')}
						desc={t('tailscale.routesHint')}
						for="ts-routes"
					>
						<textarea
							id="ts-routes"
							class="field mono"
							rows="3"
							bind:value={routes}
							placeholder="192.168.10.0/24"></textarea>
					</SettingSub>
					<SettingSub title={t('tailscale.exitMode')} for="ts-exit-mode">
						{#snippet control()}
							<select id="ts-exit-mode" class="field" bind:value={mode}
								><option value="none">{t('tailscale.exitNone')}</option><option value="provide"
									>{t('tailscale.exitProvide')}</option
								><option value="use">{t('tailscale.exitUse')}</option></select
							>
						{/snippet}
					</SettingSub>
					{#if mode === 'use'}
						<SettingSub
							title={t('tailscale.exitNode')}
							desc={t('tailscale.lanAccess')}
							for="ts-exit"
						>
							{#snippet control()}
								<select id="ts-exit" class="field" bind:value={exitNode} required>
									<option value="">{t('tailscale.chooseExit')}</option>
									{#if exitNode && !status?.exitNodes.some((peer) => peer.ip === exitNode)}<option
											value={exitNode}>{exitNode} · {t('tailscale.unavailable')}</option
										>{/if}
									{#each status?.exitNodes ?? [] as peer (peer.id)}<option value={peer.ip}
											>{peer.name} · {peer.ip}{peer.online
												? ''
												: ` · ${t('tailscale.offline')}`}</option
										>{/each}
								</select>
							{/snippet}
						</SettingSub>
					{/if}
					<SettingSub title={t('tailscale.acceptRoutes')} for="ts-accept-routes">
						{#snippet control()}<input
								id="ts-accept-routes"
								class="switch"
								type="checkbox"
								role="switch"
								bind:checked={acceptRoutes}
							/>{/snippet}
					</SettingSub>
					<SettingSub title={t('tailscale.acceptDNS')} for="ts-accept-dns">
						{#snippet control()}<input
								id="ts-accept-dns"
								class="switch"
								type="checkbox"
								role="switch"
								bind:checked={acceptDNS}
							/>{/snippet}
					</SettingSub>
					<div class="srow-block">
						<p class="muted">{t('tailscale.approvalHint')}</p>
						<div class="actions">
							<button class="btn btn-primary" disabled={!dirty || (mode === 'use' && !exitNode)}
								>{t('tailscale.apply')}</button
							>
						</div>
					</div>
				</fieldset>
			</form>
			<SettingSub title={t('tailscale.firewall')} desc={t('tailscale.firewallHint')} />
		{/if}
	</section>
{/snippet}

{#if row}{@render parts()}{:else}
	<SettingGroup title={t('tailscale.connection')} id="tailscale-connection">
		<div class="srow plain">{@render parts()}</div>
	</SettingGroup>
{/if}
{#if loggingOut}<Dialog title={t('tailscale.logout')} onclose={() => (loggingOut = false)}
		><p>{t('tailscale.logoutHint')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (loggingOut = false)}
				>{t('common.cancel')}</button
			><button type="button" class="btn btn-danger" disabled={busy} onclick={() => action('logout')}
				>{t('tailscale.logout')}</button
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
	.address {
		display: block;
		overflow-wrap: anywhere;
	}
	p {
		margin: 0;
	}
	textarea {
		resize: vertical;
	}
	.notice-error {
		white-space: pre-wrap;
	}
</style>
