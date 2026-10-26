<script lang="ts">
	import { beforeNavigate, goto } from '$app/navigation';
	import { api, ApiError } from '#lib/api.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import LegacyNetwork from './LegacyNetwork.svelte';
	import {
		topologyDraft,
		networkPayload,
		type NASNetwork,
		type NetworkBond,
		type NetworkConfig,
		type NetworkStatus,
	} from '#lib/network.ts';
	let status = $state<NetworkStatus | null>(null),
		draft = $state<NetworkConfig | null>(null),
		modes = $state<string[]>([]),
		error = $state(''),
		busy = $state(false),
		migrating = $state(false);
	let tab = $state<'overview' | 'hardware' | 'diagnostics' | 'remote'>('overview'),
		editor = $state<NASNetwork | null>(null),
		newNetwork = $state(false),
		bondEditor = $state<NetworkBond | null>(null),
		newBond = $state(false),
		remove = $state<NASNetwork | null>(null);
	let dns = $state(''),
		diagnosticNet = $state(''),
		checks = $state<{ name: string; ok: boolean; detail: string }[]>([]),
		tailnet = $state<{ active: boolean; state: string; name: string; ips: string[] } | null>(null);
	let discard = $state(false),
		destination = '';
	const current = $derived(status?.pending ?? status?.config);
	const topology = $derived(migrating || !!current?.networks?.length);
	const dirty = $derived(!!draft && JSON.stringify(draft) !== JSON.stringify(current));
	beforeNavigate((n) => {
		if (dirty && !status?.pending) {
			n.cancel();
			if (!n.willUnload) {
				destination = n.to?.url.href ?? '';
				discard = true;
			}
		}
	});
	async function load() {
		try {
			const r = await api.get<{ network: NetworkStatus; bondModes: string[] }>('/network');
			const preserve = dirty;
			status = r.network;
			modes = r.bondModes;
			if (!preserve && !editor && !bondEditor && !migrating)
				draft = structuredClone(r.network.pending ?? r.network.config);
		} catch (e) {
			error =
				errorText(e instanceof ApiError ? e.code : 'internal') +
				(e instanceof ApiError && e.message ? ' · ' + e.message : '');
		}
	}
	$effect(() => {
		void load();
		const timer = setInterval(() => {
			if (!document.hidden && !busy) void load();
		}, 3000);
		return () => clearInterval(timer);
	});
	function enable() {
		if (!status) return;
		draft = topologyDraft(
			$state.snapshot(status.config),
			status.interfaces.map((p) => p.name),
		);
		migrating = true;
	}
	function edit(n?: NASNetwork) {
		if (!draft) return;
		newNetwork = !n;
		editor = n
			? structuredClone($state.snapshot(n))
			: {
					id: 'net' + Date.now().toString(36).slice(-6),
					name: '',
					purpose: 'custom',
					parent: status?.interfaces[0]?.name ?? '',
					vlan: 0,
					bridge: true,
					management: false,
					ipv4: 'dhcp',
					address: '',
					gateway: '',
					ipv6: 'auto',
					address6: '',
					gateway6: '',
					dns: [],
					routes: [],
				};
		dns = (editor.dns ?? []).join(', ');
	}
	function saveNetwork(e: SubmitEvent) {
		e.preventDefault();
		if (!editor || !draft) return;
		const n = structuredClone($state.snapshot(editor));
		n.dns = dns
			.split(',')
			.map((s) => s.trim())
			.filter(Boolean);
		if (n.management) for (const old of draft.networks ?? []) old.management = false;
		draft.networks = newNetwork
			? [...(draft.networks ?? []), n]
			: (draft.networks ?? []).map((old) => (old.id === n.id ? n : old));
		editor = null;
	}
	function editBond(b?: NetworkBond) {
		newBond = !b;
		bondEditor = b
			? structuredClone($state.snapshot(b))
			: { id: 'bond' + Date.now().toString(36).slice(-5), ports: [], mode: 'active-backup' };
	}
	function saveBond(e: SubmitEvent) {
		e.preventDefault();
		if (!draft || !bondEditor) return;
		const b = structuredClone($state.snapshot(bondEditor));
		draft.bonds = newBond
			? [...(draft.bonds ?? []), b]
			: (draft.bonds ?? []).map((old) => (old.id === b.id ? b : old));
		bondEditor = null;
	}
	async function action(kind: 'apply' | 'confirm' | 'revert') {
		if (!draft) return;
		busy = true;
		error = '';
		try {
			if (kind === 'apply') {
				draft = networkPayload($state.snapshot(draft));
				await api.put('/network', $state.snapshot(draft));
				migrating = false;
			} else await api.post(`/network/${kind}`, {});
			await load();
			if (status) draft = $state.snapshot(status.pending ?? status.config);
		} catch (e) {
			error =
				errorText(e instanceof ApiError ? e.code : 'internal') +
				(e instanceof ApiError && e.message ? ' · ' + e.message : '');
		} finally {
			busy = false;
		}
	}
	function managementURL(n: NASNetwork) {
		const address = (n.address || n.address6).split('/')[0];
		if (!address) return '';
		const url = new URL(location.href);
		url.hostname = address.includes(':') ? `[${address}]` : address;
		return url.href;
	}
	async function diagnose() {
		busy = true;
		error = '';
		try {
			const res = await api.post<{ checks: typeof checks }>('/network/diagnostics', {
				network: diagnosticNet,
			});
			checks = res.checks;
		} catch (e) {
			error =
				errorText(e instanceof ApiError ? e.code : 'internal') +
				(e instanceof ApiError && e.message ? ' · ' + e.message : '');
		} finally {
			busy = false;
		}
	}
	async function remote() {
		tab = 'remote';
		try {
			tailnet = await api.get('/plugins/tailscale');
		} catch {
			tailnet = null;
		}
	}
	function labelParent(parent: string) {
		return parent.startsWith('bond:') ? parent.slice(5) : parent;
	}
</script>

{#if !topology}
	<LegacyNetwork />
	{#if status}<section class="tile setup">
			<h2 class="tile-title">{t('netx.multiple')}</h2>
			<p class="muted">{t('netx.migrationHint')}</p>
			<button class="btn btn-primary" onclick={enable}>{t('netx.setup')}</button>
		</section>{/if}
{:else if draft}
	<PageHead title={t('netcfg.title')} parent={{ href: '/settings', label: t('nav.settings') }}
		><p class="muted">{t('netx.intro')}</p>
		{#snippet actions()}<button
				class="btn btn-primary"
				disabled={busy || !!status?.pending}
				onclick={() => edit()}><Icon name="plus" />{t('netx.add')}</button
			>{/snippet}</PageHead
	>
	<div class="tabs" role="tablist" aria-label={t('netcfg.title')}>
		{#each ['overview', 'hardware', 'diagnostics', 'remote'] as value}<button
				class="btn"
				role="tab"
				aria-selected={tab === value}
				onclick={() => (value === 'remote' ? remote() : (tab = value as typeof tab))}
				>{t(`netx.tab.${value}` as 'netx.tab.overview')}</button
			>{/each}
	</div>
	{#if status?.pending}<section class="notice-warn" role="status">
			<strong>{t('netx.trial', { seconds: status.pendingSeconds })}</strong>
			<p>{t('netx.confirmHint')}</p>
			{#each status.pending.networks ?? [] as n}{#if n.management && managementURL(n)}<a
						href={managementURL(n)}>{t('netx.openManagement')}</a
					>{/if}{/each}
			<div class="actions">
				<button class="btn" disabled={busy} onclick={() => action('revert')}
					>{t('netx.revert')}</button
				><button class="btn btn-primary" disabled={busy} onclick={() => action('confirm')}
					>{t('netx.confirm')}</button
				>
			</div>
		</section>{:else if dirty}<div class="notice-warn">
			<p>{t('netx.unsaved')}</p>
			<div class="actions">
				<button
					class="btn"
					disabled={busy}
					onclick={() => {
						migrating = false;
						draft = current ? $state.snapshot(current) : null;
					}}>{t('common.cancel')}</button
				><button class="btn btn-primary" disabled={busy} onclick={() => action('apply')}
					>{t('netx.test')}</button
				>
			</div>
		</div>{/if}
	{#if error}<div class="notice-error" role="alert">{error}</div>{/if}
	{#if tab === 'overview'}<section class="tile setup">
			<h2 class="tile-title">{t('netx.networks')}</h2>
			{#each draft.networks ?? [] as n (n.id)}<article class="net">
					<div class="net-head">
						<div>
							<strong>{n.name}</strong>
							<p class="muted">
								{t(`netx.purpose.${n.purpose}`)} · {labelParent(n.parent)}{n.vlan
									? ` · VLAN ${n.vlan}`
									: ''}
							</p>
						</div>
						<div class="actions">
							{#if n.management}<span class="chip">{t('netx.management')}</span>{/if}<button
								class="btn"
								disabled={!!status?.pending}
								onclick={() => edit(n)}>{t('netx.edit')}</button
							>
						</div>
					</div>
					<p class="mono">
						{n.ipv4 === 'static'
							? n.address
							: n.ipv4 === 'dhcp'
								? 'IPv4 · DHCP'
								: t('netx.noIPv4')}{n.ipv6 !== 'off' ? ` · IPv6 ${n.address6 || n.ipv6}` : ''}
					</p>
					{#if n.management && status?.addresses.some((a) => a !== n.address && a !== n.address6)}<p
							class="mono muted"
						>
							{status.addresses.filter((a) => a !== n.address && a !== n.address6).join(' · ')}
						</p>{/if}
					<details>
						<summary>{t('netx.details')}</summary>
						<p class="muted">
							{t('netx.gateway')}: {n.gateway || n.gateway6 || 'DHCP / RA'} · DNS: {n.dns.join(
								', ',
							) || t('netx.automatic')}
						</p>
						<p class="muted">{n.bridge ? t('netx.bridgeEnabled') : t('netx.bridgeDisabled')}</p>
						{#if !n.management}<button
								class="btn btn-danger"
								disabled={!!status?.pending}
								onclick={() => (remove = n)}>{t('netx.remove')}</button
							>{/if}
					</details>
				</article>{/each}
		</section>
		<p class="muted">{t('netx.external')}</p>
	{:else if tab === 'hardware'}<section class="tile setup">
			<h2 class="tile-title">{t('netx.ports')}</h2>
			{#each status?.interfaces ?? [] as port}<div class="net-head">
					<div>
						<strong class="mono">{port.name}</strong>
						<p class="muted">
							{port.carrier ? t('netx.connected') : t('netx.disconnected')}{port.speedMbps
								? ` · ${port.speedMbps} Mbit/s`
								: ''}
						</p>
					</div>
					<span class="mono muted">{port.mac}</span>
				</div>{/each}
		</section>
		<section class="tile setup">
			<h2 class="tile-title">{t('netx.bonds')}</h2>
			{#each draft.bonds ?? [] as bond}<div class="net-head">
					<div>
						<strong>{bond.id}</strong>
						<p class="muted">{bond.ports.join(' + ')} · {bond.mode}</p>
					</div>
					<button class="btn" disabled={!!status?.pending} onclick={() => editBond(bond)}
						>{t('netx.edit')}</button
					>
				</div>{/each}<button class="btn" disabled={!!status?.pending} onclick={() => editBond()}
				>{t('netx.addBond')}</button
			>
			<p class="muted">{t('netx.switchHint')}</p>
		</section>
	{:else if tab === 'diagnostics'}<section class="tile setup">
			<h2 class="tile-title">{t('netx.tab.diagnostics')}</h2>
			<label class="field-row"
				><span>{t('netx.network')}</span><select class="field" bind:value={diagnosticNet}
					><option value="">{t('netx.choose')}</option>{#each current?.networks ?? [] as n}<option
							value={n.id}>{n.name}</option
						>{/each}</select
				></label
			><button class="btn" disabled={busy || !diagnosticNet || !!status?.pending} onclick={diagnose}
				>{t('netx.check')}</button
			>{#each checks as check}<div class="net-head">
					<div>
						<strong>{check.name}</strong>
						<p class="muted">{check.detail}</p>
					</div>
					<span class="chip">{check.ok ? t('netx.ok') : t('netx.warning')}</span>
				</div>{/each}
		</section>
	{:else}<section class="tile setup">
			<h2 class="tile-title">Tailscale</h2>
			<p>{tailnet?.name ?? t('netx.notAvailable')}</p>
			<p class="mono muted">{tailnet?.ips?.join(' · ') ?? ''}</p>
			<p class="muted">{t('netx.tailscaleHint')}</p>
			<a class="btn" href="/plugins">{t('nav.plugins')}</a>
		</section>
		<section class="tile setup">
			<h2 class="tile-title">Cloudflare / Reverse-Proxy</h2>
			<p class="muted">{t('netx.proxyHint')}</p>
			<a class="btn" href="/docker">{t('netx.appLinks')}</a>
		</section>
		<section class="tile setup">
			<h2 class="tile-title">{t('netx.dnsServices')}</h2>
			<p class="muted">{t('netx.dnsHint')}</p>
		</section>{/if}
{/if}

{#if editor}<Dialog
		title={newNetwork ? t('netx.add') : t('netx.edit')}
		wide
		locked={busy}
		onclose={() => (editor = null)}
		><form onsubmit={saveNetwork}>
			<div class="two">
				<label class="field-row"
					><span>{t('netx.name')}</span><input
						class="field"
						bind:value={editor.name}
						required
						maxlength="80"
					/></label
				><label class="field-row"
					><span>{t('netx.purpose')}</span><select class="field" bind:value={editor.purpose}
						>{#each ['management', 'storage', 'apps', 'custom'] as purpose}<option value={purpose}
								>{t(`netx.purpose.${purpose}` as 'netx.purpose.management')}</option
							>{/each}</select
					></label
				>
			</div>
			<div class="two">
				<label class="field-row"
					><span>{t('netx.connection')}</span><select class="field" bind:value={editor.parent}
						>{#each status?.interfaces ?? [] as p}<option value={p.name}>{p.name}</option
							>{/each}{#each draft?.bonds ?? [] as b}<option value={'bond:' + b.id}
								>Bond {b.id}</option
							>{/each}</select
					></label
				><label class="field-row"
					><span>VLAN ID · {t('netx.optional')}</span><input
						class="field"
						type="number"
						min="0"
						max="4094"
						bind:value={editor.vlan}
					/></label
				>
			</div>
			<label class="check"
				><input type="checkbox" bind:checked={editor.management} /><span>{t('netx.preferred')}</span
				></label
			><label class="check"
				><input type="checkbox" bind:checked={editor.bridge} /><span>{t('netx.bridge')}</span
				></label
			>
			<div class="two">
				<fieldset>
					<legend>IPv4</legend><label class="field-row"
						><span>{t('netx.method')}</span><select class="field" bind:value={editor.ipv4}
							><option value="dhcp">DHCP</option><option value="static">{t('netx.static')}</option
							><option value="off">{t('netx.off')}</option></select
						></label
					>{#if editor.ipv4 === 'static'}<label class="field-row"
							><span>{t('netx.address')}</span><input
								class="field mono"
								bind:value={editor.address}
								required
							/></label
						><label class="field-row"
							><span>{t('netx.gateway')}</span><input
								class="field mono"
								bind:value={editor.gateway}
							/></label
						>{/if}
				</fieldset>
				<fieldset>
					<legend>IPv6</legend><label class="field-row"
						><span>{t('netx.method')}</span><select class="field" bind:value={editor.ipv6}
							><option value="auto">SLAAC / RA</option><option value="dhcp">DHCPv6 / RA</option
							><option value="static">{t('netx.static')}</option><option value="off"
								>{t('netx.off')}</option
							></select
						></label
					>{#if editor.ipv6 === 'static'}<label class="field-row"
							><span>{t('netx.address')}</span><input
								class="field mono"
								bind:value={editor.address6}
								required
							/></label
						><label class="field-row"
							><span>{t('netx.gateway')}</span><input
								class="field mono"
								bind:value={editor.gateway6}
							/></label
						>{/if}
				</fieldset>
			</div>
			<label class="field-row"
				><span>{t('netx.dns')}</span><input class="field mono" bind:value={dns} /></label
			>
			<details>
				<summary>{t('netx.routes')}</summary>{#each editor.routes ?? [] as route, i}<div
						class="route"
					>
						<label class="field-row"
							><span>{t('netx.destination')}</span><input
								class="field mono"
								bind:value={route.destination}
								required
							/></label
						><label class="field-row"
							><span>{t('netx.gateway')}</span><input
								class="field mono"
								bind:value={route.gateway}
								required
							/></label
						><label class="field-row"
							><span>{t('netx.metric')}</span><input
								class="field"
								type="number"
								min="0"
								max="65535"
								bind:value={route.metric}
							/></label
						><button
							class="btn"
							type="button"
							onclick={() => {
								if (editor) editor.routes = editor.routes?.filter((_, j) => i !== j);
							}}>{t('netx.remove')}</button
						>
					</div>{/each}<button
					class="btn"
					type="button"
					onclick={() => {
						if (editor)
							editor.routes = [
								...(editor.routes ?? []),
								{ destination: '', gateway: '', metric: 100 },
							];
					}}>{t('netx.addRoute')}</button
				>
			</details>
			<p class="muted">{t('netx.addressHint')}</p>
			<div class="dialog-actions">
				<button type="button" class="btn" onclick={() => (editor = null)}
					>{t('common.cancel')}</button
				><button class="btn btn-primary">{t('common.save')}</button>
			</div>
		</form></Dialog
	>{/if}
{#if bondEditor}<Dialog title={t('netx.bonds')} onclose={() => (bondEditor = null)}
		><form onsubmit={saveBond}>
			<label class="field-row"
				><span>{t('netx.bondMode')}</span><select class="field" bind:value={bondEditor.mode}
					>{#each modes as mode}<option>{mode}</option>{/each}</select
				></label
			>
			<fieldset>
				<legend>{t('netx.ports')}</legend>{#each status?.interfaces ?? [] as p}<label class="check"
						><input type="checkbox" bind:group={bondEditor.ports} value={p.name} /><span
							class="mono">{p.name}</span
						></label
					>{/each}
			</fieldset>
			<p class="muted">{t('netx.switchHint')}</p>
			<div class="dialog-actions">
				<button type="button" class="btn" onclick={() => (bondEditor = null)}
					>{t('common.cancel')}</button
				><button class="btn btn-primary" disabled={bondEditor.ports.length < 2}
					>{t('common.save')}</button
				>
			</div>
		</form></Dialog
	>{/if}
{#if remove}<Dialog title={t('netx.remove')} onclose={() => (remove = null)}
		><p>{t('netx.removeConfirm', { name: remove.name })}</p>
		<div class="dialog-actions">
			<button class="btn" onclick={() => (remove = null)}>{t('common.cancel')}</button><button
				class="btn btn-danger"
				onclick={() => {
					if (draft && remove) draft.networks = draft.networks?.filter((n) => n.id !== remove?.id);
					remove = null;
				}}>{t('netx.remove')}</button
			>
		</div></Dialog
	>{/if}
{#if discard}<Dialog title={t('netx.unsaved')} onclose={() => (discard = false)}
		><div class="dialog-actions">
			<button class="btn" onclick={() => (discard = false)}>{t('common.cancel')}</button><button
				class="btn btn-warn"
				onclick={() => {
					draft = current ? $state.snapshot(current) : null;
					migrating = false;
					discard = false;
					void goto(destination);
				}}>{t('shares.discard')}</button
			>
		</div></Dialog
	>{/if}

<style>
	.setup {
		padding: var(--tile-pad);
		display: grid;
		gap: var(--gap);
	}
	.net {
		padding: var(--tile-pad) 0;
		border-bottom: 1px solid var(--border);
	}
	.net:last-child {
		border: 0;
	}
	.net-head {
		display: flex;
		justify-content: space-between;
		align-items: center;
		gap: var(--gap);
		padding: var(--row-pad) 0;
	}
	.net-head > div {
		min-width: 0;
	}
	.net p,
	.net-head p {
		overflow-wrap: anywhere;
	}
	.actions,
	.tabs,
	.dialog-actions {
		display: flex;
		align-items: center;
		gap: 8px;
		flex-wrap: wrap;
	}
	.tabs {
		border-bottom: 1px solid var(--border);
	}
	.tabs button {
		border: 0;
		border-radius: 0;
		min-height: 44px;
	}
	.tabs [aria-selected='true'] {
		color: var(--accent);
		border-bottom: 2px solid var(--accent);
	}
	.two {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: var(--gap);
	}
	.field-row,
	form {
		display: grid;
		gap: var(--gap);
	}
	.check {
		display: flex;
		gap: 10px;
		align-items: center;
		min-height: 44px;
	}
	.check input {
		width: 18px;
		height: 18px;
	}
	fieldset {
		padding: var(--tile-pad);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		display: grid;
		gap: var(--gap);
		min-width: 0;
	}
	.route {
		display: grid;
		grid-template-columns: 1fr 1fr 80px auto;
		gap: 8px;
		padding: var(--row-pad) 0;
	}
	.dialog-actions {
		justify-content: flex-end;
		margin-top: var(--gap);
	}
	details > summary {
		cursor: pointer;
		min-height: 44px;
		display: flex;
		align-items: center;
	}
	details > p {
		margin-bottom: var(--gap);
	}
	@media (max-width: 760px) {
		.two,
		.route {
			grid-template-columns: 1fr;
		}
		.net-head {
			align-items: flex-start;
			flex-wrap: wrap;
		}
		.actions {
			gap: 6px;
		}
		.tabs {
			gap: 0;
			flex-wrap: nowrap;
			overflow: auto;
		}
		.tabs button {
			padding: 0 8px;
			white-space: nowrap;
		}
		.net-head > .mono {
			font-size: 0.75rem;
		}
	}
</style>
