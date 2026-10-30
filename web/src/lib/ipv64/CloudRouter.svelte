<script lang="ts">
	// Cloud Router: routers, WireGuard gateways and peers, routes, firewall
	// rules, the public IPv6, and optionally the tunnel of this NAS.
	import { onMount } from 'svelte';
	import { api } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import {
		RULE_ACTIONS,
		RULE_CHAINS,
		size,
		tone,
		validCIDR,
		type PublicIP,
		type Route,
		type Router,
		type RouterRule,
		type Status,
	} from './ipv64.ts';
	import { failure, when } from './ui.ts';

	let { status, onchange }: { status: Status; onchange: () => void } = $props();

	let routers = $state<Router[]>([]);
	let error = $state('');
	let notice = $state('');
	let busy = $state(false);
	let newRouter = $state('');

	async function load() {
		try {
			routers = (await api.get<{ routers: Router[] }>('/ipv64/routers')).routers;
			error = '';
		} catch (err) {
			error = failure(err);
		}
	}
	onMount(load);

	async function run(action: () => Promise<unknown>, done = '') {
		busy = true;
		error = notice = '';
		try {
			await action();
			if (done) notice = done;
			return true;
		} catch (err) {
			error = failure(err);
			return false;
		} finally {
			busy = false;
		}
	}

	type RouterDetail = {
		router: Router;
		routes: Route[];
		rules: RouterRule[];
		publicIPs: PublicIP[];
	};
	let detail = $state<RouterDetail | null>(null);
	async function open(id: string) {
		await run(async () => (detail = await api.get<RouterDetail>(`/ipv64/routers/${id}`)));
	}
	const base = $derived(detail ? `/ipv64/routers/${detail.router.id}` : '');
	async function again() {
		if (detail) await open(detail.router.id);
	}

	let route = $state<{
		destination: string;
		gateway: string;
		peer: string;
		distance: number;
	} | null>(null);
	let rule = $state<{ chain: string; action: string; source: string; destination: string } | null>(
		null,
	);
	let gatewayForm = $state<{ id: string; name: string; ipv4: string; ipv6: string } | null>(null);
	let peerName = $state<{ gateway: string; name: string } | null>(null);
	let confirm = $state<{
		title: MessageKey;
		text: string;
		button: MessageKey;
		danger: boolean;
		run: () => Promise<unknown>;
	} | null>(null);
	async function confirmed() {
		if (!confirm) return;
		const what = confirm;
		if (
			await run(async () => {
				await what.run();
				await load();
				await again();
				onchange();
			}, t('ipv64x.accepted'))
		)
			confirm = null;
	}

	// --- this NAS ---
	const wg = $derived(status.status.settings.wireguard);
	let connect = $state<{ router: string; gateway: string; name: string } | null>(null);
	let disconnect = $state(false);
	let deletePeer = $state(true);
	async function doConnect() {
		if (!connect) return;
		const c = connect;
		if (
			await run(async () => {
				await api.post('/ipv64/tunnel', c);
				onchange();
			}, t('ipv64x.tunnelUp'))
		)
			connect = null;
	}
	async function doDisconnect() {
		if (
			await run(async () => {
				await api.delete('/ipv64/tunnel', { deletePeer });
				onchange();
			})
		)
			disconnect = false;
	}
	const routeOK = $derived(
		!!route &&
			validCIDR(route.destination) &&
			route.destination.includes('/') &&
			validCIDR(route.gateway) &&
			!!route.peer,
	);
	const ruleOK = $derived(
		!!rule &&
			(rule.source || rule.destination) !== '' &&
			(!rule.source || validCIDR(rule.source)) &&
			(!rule.destination || validCIDR(rule.destination)),
	);
</script>

<div class="ipv64-tab">
	<section class="tile panel" aria-labelledby="ipv64-nas-tunnel">
		<h2 class="tile-title" id="ipv64-nas-tunnel">{t('ipv64x.thisNasTunnel')}</h2>
		<p class="muted hint">{t('ipv64x.tunnelHint')}</p>
		{#if wg.enabled}
			<dl>
				<div>
					<dt>{t('ipv64x.state')}</dt>
					<dd>
						<span class="chip {status.status.tunnel.up ? 'ok' : 'error'}"
							>{t(status.status.tunnel.up ? 'ipv64x.connected' : 'ipv64x.notConnected')}</span
						>
					</dd>
				</div>
				<div>
					<dt>{t('ipv64x.addresses')}</dt>
					<dd class="mono">{(wg.addresses ?? []).join(', ')}</dd>
				</div>
				<div>
					<dt>{t('ipv64x.endpoint')}</dt>
					<dd class="mono">{wg.endpoint}</dd>
				</div>
				<div>
					<dt>{t('ipv64x.routes')}</dt>
					<dd class="mono">{status.status.tunnel.routes.join(', ') || '–'}</dd>
				</div>
				<div>
					<dt>{t('ipv64x.publicKey')}</dt>
					<dd class="mono">{wg.publicKey}</dd>
				</div>
			</dl>
			{#if status.status.tunnel.error}<p class="notice-error" role="alert">
					{status.status.tunnel.error}
				</p>{/if}
			<p class="muted hint">{t('ipv64x.tunnelZone')}</p>
			<div class="row-actions">
				<button
					type="button"
					class="btn btn-danger"
					disabled={busy}
					onclick={() => ((disconnect = true), (deletePeer = true))}
					>{t('ipv64x.disconnect')}</button
				>
			</div>
		{:else}
			<p>{t('ipv64x.tunnelOff')}</p>
		{/if}
	</section>

	<section class="tile panel" aria-labelledby="ipv64-routers">
		<h2 class="tile-title" id="ipv64-routers">{t('ipv64x.routers')}</h2>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		{#if notice}<p class="notice-ok" role="status">{notice}</p>{/if}
		<div class="table-wrap">
			<table>
				<thead><tr><th>{t('ipv64x.name')}</th><th>{t('ipv64x.gateways')}</th><th></th></tr></thead>
				<tbody>
					{#each routers as r (r.id)}
						<tr>
							<td
								>{r.name}{#if r.traffic_limited}<span class="chip error">{t('ipv64x.limited')}</span
									>{/if}</td
							>
							<td>{r.gateways.length}</td>
							<td class="actions"
								><button type="button" class="btn" onclick={() => open(r.id)}
									>{t('ipv64x.manage')}</button
								></td
							>
						</tr>
					{:else}
						<tr><td colspan="3" class="muted">{t('ipv64x.none')}</td></tr>
					{/each}
				</tbody>
			</table>
		</div>
		<div class="inline">
			<label class="stack"
				><span>{t('ipv64x.name')}</span><input
					class="field"
					type="text"
					maxlength="100"
					bind:value={newRouter}
				/></label
			>
			<button
				type="button"
				class="btn"
				disabled={busy || !newRouter.trim()}
				onclick={() =>
					(confirm = {
						title: 'ipv64x.routerCreate',
						text: t('ipv64x.routerCreateText', { name: newRouter.trim() }),
						button: 'ipv64x.routerCreate',
						danger: false,
						run: () => api.post('/ipv64/routers', { name: newRouter.trim() }),
					})}>{t('ipv64x.routerCreate')}</button
			>
		</div>
	</section>

	{#if detail}
		<section class="tile panel" aria-labelledby="ipv64-router">
			<h2 class="tile-title" id="ipv64-router">{detail.router.name}</h2>
			<h3>{t('ipv64x.gateways')}</h3>
			{#each detail.router.gateways as g (g.id)}
				<div class="gateway">
					<div class="inline">
						<strong class="grow">{g.name}</strong>
						<span class="chip {tone(g.state)}">{g.state}</span>
						{#if g.disabled}<span class="chip">{t('ipv64x.disabled')}</span>{/if}
						{#if g.traffic_limited}<span class="chip error">{t('ipv64x.limited')}</span>{/if}
					</div>
					<dl>
						<div>
							<dt>{t('ipv64x.server')}</dt>
							<dd>{g.server ? `${g.server.hostname} · ${g.server.location}` : '–'}</dd>
						</div>
						<div>
							<dt>{t('ipv64x.listenPort')}</dt>
							<dd class="mono">{g.listen_port ?? '–'}</dd>
						</div>
						<div>
							<dt>{t('ipv64x.addresses')}</dt>
							<dd class="mono">{[g.ipv4, g.ipv6].filter(Boolean).join(', ') || '–'}</dd>
						</div>
						<div>
							<dt>{t('ipv64x.traffic')}</dt>
							<dd>↓ {size(g.traffic_bytes.in)} · ↑ {size(g.traffic_bytes.out)}</dd>
						</div>
					</dl>
					<p class="muted hint">{t('ipv64x.listenPortHint')}</p>
					<div class="row-actions">
						<button
							type="button"
							class="btn"
							disabled={busy}
							onclick={() =>
								(gatewayForm = { id: g.id, name: g.name, ipv4: g.ipv4 ?? '', ipv6: g.ipv6 ?? '' })}
							>{t('ipv64x.edit')}</button
						>
						<button
							type="button"
							class="btn"
							disabled={busy}
							onclick={() => (peerName = { gateway: g.id, name: '' })}>{t('ipv64x.peerAdd')}</button
						>
						{#if !wg.enabled}<button
								type="button"
								class="btn btn-primary"
								disabled={busy || status.status.readOnly}
								onclick={() =>
									(connect = {
										router: detail?.router.id ?? '',
										gateway: g.id,
										name: `titanos-${status.hostname}`,
									})}>{t('ipv64x.connectNas')}</button
							>{/if}
						<button
							type="button"
							class="btn btn-warn"
							disabled={busy}
							onclick={() =>
								(confirm = {
									title: 'ipv64x.rotateKeys',
									text: t('ipv64x.rotateKeysText', { name: g.name }),
									button: 'ipv64x.rotateKeys',
									danger: false,
									run: () => api.post(`/ipv64/gateways/${g.id}/do/rotate-keys`),
								})}>{t('ipv64x.rotateKeys')}</button
						>
						<button
							type="button"
							class="btn"
							disabled={busy}
							onclick={() =>
								run(async () => {
									await api.post(`/ipv64/gateways/${g.id}/do/reset-traffic`);
									await again();
								})}>{t('ipv64x.resetTraffic')}</button
						>
						<button
							type="button"
							class="btn"
							disabled={busy || (wg.enabled && wg.gateway === g.id)}
							onclick={() =>
								(confirm = {
									title: 'ipv64x.gatewayDelete',
									text: t('ipv64x.gatewayDeleteText', { name: g.name }),
									button: 'ipv64x.delete',
									danger: true,
									run: () => api.delete(`/ipv64/gateways/${g.id}`),
								})}>{t('ipv64x.delete')}</button
						>
					</div>
					{#if g.peers?.length}
						<div class="table-wrap">
							<table>
								<thead
									><tr
										><th>{t('ipv64x.peer')}</th><th>{t('ipv64x.addresses')}</th><th
											>{t('ipv64x.handshake')}</th
										><th></th></tr
									></thead
								>
								<tbody>
									{#each g.peers as p (p.id)}
										<tr>
											<td
												>{p.name}{#if wg.peer === p.id}<span class="chip ok"
														>{t('ipv64x.thisNas')}</span
													>{/if}</td
											>
											<td class="mono">{[p.ipv4, p.ipv6].filter(Boolean).join(', ')}</td>
											<td>{p.last_handshake ?? '–'}</td>
											<td class="actions"
												>{#if wg.peer !== p.id}<button
														type="button"
														class="btn"
														disabled={busy}
														onclick={() =>
															(confirm = {
																title: 'ipv64x.peerDelete',
																text: t('ipv64x.peerDeleteText', { name: p.name }),
																button: 'ipv64x.delete',
																danger: true,
																run: () => api.delete(`/ipv64/peers/${p.id}`),
															})}>{t('ipv64x.delete')}</button
													>{/if}</td
											>
										</tr>
									{/each}
								</tbody>
							</table>
						</div>
					{/if}
				</div>
			{/each}
			<div class="row-actions">
				<button
					type="button"
					class="btn"
					disabled={busy}
					onclick={() =>
						run(async () => {
							await api.post(`${base}/gateways`, { name: 'gateway' });
							await again();
						}, t('ipv64x.accepted'))}>{t('ipv64x.gatewayAdd')}</button
				>
			</div>

			<h3>{t('ipv64x.routes')}</h3>
			<div class="table-wrap">
				<table>
					<tbody>
						{#each detail.routes as r (r.id)}
							<tr>
								<td class="mono">{r.destination} via {r.gateway}</td>
								<td
									>{r.system ? t('ipv64x.system') : ''}
									<span class="chip {tone(r.state)}">{r.state}</span>{#if r.disabled}<span
											class="chip">{t('ipv64x.disabled')}</span
										>{/if}</td
								>
								<td class="actions">
									<button
										type="button"
										class="btn"
										disabled={busy}
										onclick={() =>
											run(async () => {
												await api.patch(`${base}/routes/${r.id}`, { disabled: !r.disabled });
												await again();
											})}>{t(r.disabled ? 'ipv64x.switchOn' : 'ipv64x.switchOff')}</button
									>
									{#if !r.system}<button
											type="button"
											class="btn"
											disabled={busy}
											onclick={() =>
												run(async () => {
													await api.delete(`${base}/routes/${r.id}`);
													await again();
												})}>{t('ipv64x.delete')}</button
										>{/if}
								</td>
							</tr>
						{:else}
							<tr><td class="muted">{t('ipv64x.none')}</td></tr>
						{/each}
					</tbody>
				</table>
			</div>
			<div class="row-actions">
				<button
					type="button"
					class="btn"
					onclick={() =>
						(route = {
							destination: '',
							gateway: '',
							peer: detail?.router.gateways[0]?.peers?.[0]?.id ?? '',
							distance: 1,
						})}>{t('ipv64x.routeAdd')}</button
				>
			</div>

			<h3>{t('ipv64x.routerRules')}</h3>
			<div class="table-wrap">
				<table>
					<tbody>
						{#each detail.rules as r (r.id)}
							<tr>
								<td class="mono">{r.chain} {r.action} {r.source ?? '*'} → {r.destination ?? '*'}</td
								>
								<td
									><span class="chip {tone(r.state)}">{r.state}</span>{#if r.disabled}<span
											class="chip">{t('ipv64x.disabled')}</span
										>{/if}</td
								>
								<td class="actions">
									<button
										type="button"
										class="btn"
										disabled={busy}
										onclick={() =>
											run(async () => {
												await api.patch(`${base}/firewall/${r.id}`, { disabled: !r.disabled });
												await again();
											})}>{t(r.disabled ? 'ipv64x.switchOn' : 'ipv64x.switchOff')}</button
									>
									<button
										type="button"
										class="btn"
										disabled={busy}
										onclick={() =>
											run(async () => {
												await api.delete(`${base}/firewall/${r.id}`);
												await again();
											})}>{t('ipv64x.delete')}</button
									>
								</td>
							</tr>
						{:else}
							<tr><td class="muted">{t('ipv64x.none')}</td></tr>
						{/each}
					</tbody>
				</table>
			</div>
			<div class="row-actions">
				<button
					type="button"
					class="btn"
					onclick={() =>
						(rule = { chain: 'forward', action: 'accept', source: '', destination: '' })}
					>{t('ipv64x.ruleAdd')}</button
				>
			</div>

			<h3>{t('ipv64x.publicIPs')}</h3>
			<ul class="plain">
				{#each detail.publicIPs as ip (ip.id)}
					<li class="mono">
						{ip.ip} <span class="chip {tone(ip.state)}">{ip.state}</span>
						<button
							type="button"
							class="btn"
							disabled={busy}
							onclick={() =>
								run(async () => {
									await api.patch(`${base}/public-ips/${ip.id}`, { disabled: !ip.disabled });
									await again();
								})}>{t(ip.disabled ? 'ipv64x.switchOn' : 'ipv64x.switchOff')}</button
						>
						<button
							type="button"
							class="btn"
							disabled={busy}
							onclick={() =>
								(confirm = {
									title: 'ipv64x.ipDelete',
									text: t('ipv64x.ipDeleteText', { ip: ip.ip }),
									button: 'ipv64x.delete',
									danger: true,
									run: () => api.delete(`${base}/public-ips/${ip.id}`),
								})}>{t('ipv64x.delete')}</button
						>
					</li>
				{:else}
					<li class="muted">{t('ipv64x.none')}</li>
				{/each}
			</ul>
			{#if !detail.publicIPs.some((ip) => ip.type === 'ipv6')}
				<div class="row-actions">
					<button
						type="button"
						class="btn"
						disabled={busy}
						onclick={() =>
							(confirm = {
								title: 'ipv64x.ipAdd',
								text: t('ipv64x.ipAddText'),
								button: 'ipv64x.ipAdd',
								danger: false,
								run: () => api.post(`${base}/public-ips`),
							})}>{t('ipv64x.ipAdd')}</button
					>
				</div>
			{/if}
		</section>
	{/if}
</div>

{#if connect}
	<Dialog title={t('ipv64x.connectNas')} locked={busy} onclose={() => (connect = null)}>
		<p>{t('ipv64x.connectText')}</p>
		<ul>
			<li>{t('ipv64x.connectKeys')}</li>
			<li>{t('ipv64x.connectRoutes')}</li>
			<li>{t('ipv64x.connectFirewall')}</li>
			<li>{t('ipv64x.connectRelease')}</li>
		</ul>
		<label class="stack"
			><span>{t('ipv64x.peerName')}</span><input
				class="field"
				type="text"
				maxlength="100"
				bind:value={connect.name}
			/></label
		>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (connect = null)}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-primary"
				disabled={busy || !connect.name.trim()}
				onclick={doConnect}>{t('ipv64x.connectNas')}</button
			>
		</div>
	</Dialog>
{/if}

{#if disconnect}
	<Dialog title={t('ipv64x.disconnect')} locked={busy} onclose={() => (disconnect = false)}>
		<p>{t('ipv64x.disconnectText')}</p>
		<label class="check"
			><input type="checkbox" bind:checked={deletePeer} /><span>{t('ipv64x.deletePeer')}</span
			></label
		>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (disconnect = false)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-danger" disabled={busy} onclick={doDisconnect}
				>{t('ipv64x.disconnect')}</button
			>
		</div>
	</Dialog>
{/if}

{#if gatewayForm}
	<Dialog title={t('ipv64x.gatewayEdit')} locked={busy} onclose={() => (gatewayForm = null)}>
		<label class="stack"
			><span>{t('ipv64x.name')}</span><input
				class="field"
				type="text"
				maxlength="100"
				bind:value={gatewayForm.name}
			/></label
		>
		<label class="stack"
			><span>IPv4 (CIDR)</span><input
				class="field mono"
				type="text"
				bind:value={gatewayForm.ipv4}
				placeholder="10.64.0.1/24"
			/></label
		>
		<label class="stack"
			><span>IPv6 (CIDR)</span><input
				class="field mono"
				type="text"
				bind:value={gatewayForm.ipv6}
				placeholder="fd64::1/64"
			/></label
		>
		<p class="muted hint">{t('ipv64x.gatewayAddrHint')}</p>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (gatewayForm = null)}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-primary"
				disabled={busy || !gatewayForm.name.trim()}
				onclick={async () => {
					const g = gatewayForm;
					if (
						g &&
						(await run(async () => {
							await api.patch(`/ipv64/gateways/${g.id}`, {
								name: g.name.trim(),
								ipv4: g.ipv4.trim(),
								ipv6: g.ipv6.trim(),
							});
							await again();
						}))
					)
						gatewayForm = null;
				}}>{t('common.save')}</button
			>
		</div>
	</Dialog>
{/if}

{#if peerName}
	<Dialog title={t('ipv64x.peerAdd')} locked={busy} onclose={() => (peerName = null)}>
		<label class="stack"
			><span>{t('ipv64x.name')}</span><input
				class="field"
				type="text"
				maxlength="100"
				bind:value={peerName.name}
			/></label
		>
		<p class="muted hint">{t('ipv64x.peerHint')}</p>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (peerName = null)}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-primary"
				disabled={busy || !peerName.name.trim()}
				onclick={async () => {
					const p = peerName;
					if (
						p &&
						(await run(async () => {
							await api.post(`/ipv64/gateways/${p.gateway}/peers`, { name: p.name.trim() });
							await again();
						}, t('ipv64x.accepted')))
					)
						peerName = null;
				}}>{t('ipv64x.add')}</button
			>
		</div>
	</Dialog>
{/if}

{#if route}
	<Dialog title={t('ipv64x.routeAdd')} locked={busy} onclose={() => (route = null)}>
		<label class="stack"
			><span>{t('ipv64x.destination')}</span><input
				class="field mono"
				type="text"
				bind:value={route.destination}
				placeholder="192.168.50.0/24"
			/></label
		>
		<label class="stack"
			><span>{t('ipv64x.nextHop')}</span><input
				class="field mono"
				type="text"
				bind:value={route.gateway}
				placeholder="10.64.0.5"
			/></label
		>
		<label class="stack"
			><span>{t('ipv64x.peer')}</span><select class="field" bind:value={route.peer}
				>{#each detail?.router.gateways ?? [] as g (g.id)}{#each g.peers ?? [] as p (p.id)}<option
							value={p.id}>{g.name} / {p.name}</option
						>{/each}{/each}</select
			></label
		>
		<label class="stack"
			><span>{t('ipv64x.distance')}</span><input
				class="field short"
				type="number"
				min="1"
				max="255"
				bind:value={route.distance}
			/></label
		>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (route = null)}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-primary"
				disabled={busy || !routeOK}
				onclick={async () => {
					const r = route;
					if (
						r &&
						(await run(async () => {
							await api.post(`${base}/routes`, r);
							await again();
						}, t('ipv64x.accepted')))
					)
						route = null;
				}}>{t('ipv64x.add')}</button
			>
		</div>
	</Dialog>
{/if}

{#if rule}
	<Dialog title={t('ipv64x.ruleAdd')} locked={busy} onclose={() => (rule = null)}>
		<div class="inline">
			<label class="stack"
				><span>{t('ipv64x.chain')}</span><select class="field" bind:value={rule.chain}
					>{#each RULE_CHAINS as c (c)}<option value={c}>{c}</option>{/each}</select
				></label
			>
			<label class="stack"
				><span>{t('ipv64x.action')}</span><select class="field" bind:value={rule.action}
					>{#each RULE_ACTIONS as a (a)}<option value={a}>{a}</option>{/each}</select
				></label
			>
		</div>
		<label class="stack"
			><span>{t('ipv64x.source')}</span><input
				class="field mono"
				type="text"
				bind:value={rule.source}
				placeholder="10.0.0.0/8"
			/></label
		>
		<label class="stack"
			><span>{t('ipv64x.destination')}</span><input
				class="field mono"
				type="text"
				bind:value={rule.destination}
			/></label
		>
		<p class="muted hint">{t('ipv64x.ruleHint')}</p>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (rule = null)}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-primary"
				disabled={busy || !ruleOK}
				onclick={async () => {
					const r = rule;
					if (
						r &&
						(await run(async () => {
							await api.post(`${base}/firewall`, {
								chain: r.chain,
								action: r.action,
								...(r.source ? { source: r.source.trim() } : {}),
								...(r.destination ? { destination: r.destination.trim() } : {}),
							});
							await again();
						}, t('ipv64x.accepted')))
					)
						rule = null;
				}}>{t('ipv64x.add')}</button
			>
		</div>
	</Dialog>
{/if}

{#if confirm}
	<Dialog title={t(confirm.title)} locked={busy} onclose={() => (confirm = null)}>
		<p>{confirm.text}</p>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={busy} onclick={() => (confirm = null)}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class={confirm.danger ? 'btn btn-danger' : 'btn btn-warn'}
				disabled={busy}
				onclick={confirmed}>{t(confirm.button)}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.chip {
		margin-left: 6px;
	}
	.gateway {
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		padding: 8px 12px;
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	.plain {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: 4px;
	}
</style>
