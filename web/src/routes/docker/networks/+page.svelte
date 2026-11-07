<script lang="ts">
	import { api } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import type { NASNetwork } from '#lib/network.ts';
	import type { ContainerLAN } from '#lib/compose/lan.ts';
	import Icon from '#lib/components/Icon.svelte';
	import {
		deletedCount,
		loadNetworks,
		unusedNetworks,
		usedByText,
		validSubnet,
		type DockerNetwork,
		type PruneResult,
	} from '#lib/docker-resources.ts';

	// PLAN 67: all Docker networks with what uses them; own bridge networks can
	// be made and removed (others are kept and say why). The LAN networks of
	// apps (own addresses in the home network) keep their card below.
	let all = $state<DockerNetwork[]>([]);
	let allError = $state('');
	let message = $state('');
	let bridge = $state<{ name: string; subnet: string; internal: boolean } | null>(null);
	let dropBridge = $state<DockerNetwork | null>(null);
	let pruning = $state(false);
	async function loadAll() {
		try {
			all = await loadNetworks();
			allError = '';
		} catch (e) {
			allError = apiErrorText(e, { detail: true });
		}
	}
	$effect(() => {
		void loadAll();
	});
	const unused = $derived(unusedNetworks(all));
	function reason(n: DockerNetwork): string {
		if (n.kind === 'app') return t('dnet.kind.app', { app: n.app || '–' });
		if (n.kind === 'lan') return t('dnet.kind.lan');
		if (n.kind === 'titanos') return t('dnet.kind.titanos');
		if (n.kind === 'system') return t('dnet.kind.system');
		return '';
	}
	const subnetOk = $derived(!bridge || validSubnet(bridge.subnet));
	async function createBridge(e: SubmitEvent) {
		e.preventDefault();
		if (!bridge || !subnetOk) return;
		busy = true;
		error = '';
		try {
			const body: Record<string, unknown> = { name: bridge.name.trim() };
			if (bridge.subnet.trim()) body.subnet = bridge.subnet.trim();
			if (bridge.internal) body.internal = true;
			await api.post('/docker/networks/bridge', body);
			message = t('dnet.created', { name: bridge.name.trim() });
			bridge = null;
		} catch (err) {
			error = apiErrorText(err, { detail: true });
		} finally {
			busy = false;
			await loadAll();
		}
	}
	async function eraseBridge() {
		if (!dropBridge) return;
		busy = true;
		error = '';
		const name = dropBridge.name;
		try {
			await api.delete(`/docker/networks/bridge/${encodeURIComponent(name)}`);
			message = t('dnet.removed', { name });
		} catch (err) {
			error = apiErrorText(err, { detail: true });
		} finally {
			dropBridge = null;
			busy = false;
			await loadAll();
		}
	}
	async function prune() {
		busy = true;
		error = '';
		try {
			const res = await api.post<PruneResult>('/docker/networks/prune', {});
			message = t('dnet.pruned', { count: deletedCount(res) ?? unused.length });
			if (res?.failed?.length)
				error = t('dres.failed', { names: res.failed.map((f) => f.id).join(', ') });
		} catch (err) {
			error = apiErrorText(err, { detail: true });
		} finally {
			pruning = false;
			busy = false;
			await loadAll();
		}
	}
	let networks = $state<ContainerLAN[]>([]),
		parents = $state<NASNetwork[]>([]),
		loading = $state(true),
		busy = $state(false),
		error = $state(''),
		draft = $state<ContainerLAN | null>(null),
		remove = $state<ContainerLAN | null>(null);
	async function load() {
		try {
			const res = await api.get<{ networks: ContainerLAN[]; parents: NASNetwork[] }>(
				'/docker/networks',
			);
			networks = res.networks ?? [];
			parents = res.parents ?? [];
		} catch (e) {
			error = apiErrorText(e, { detail: true });
		} finally {
			loading = false;
		}
	}
	$effect(() => {
		void load();
	});
	function add() {
		draft = {
			name: '',
			parent: parents[0]?.id ?? '',
			driver: 'ipvlan',
			subnet: '',
			gateway: '',
			range: '',
			hostAccess: false,
			hostAddress: '',
		};
		error = '';
	}
	async function create(e: SubmitEvent) {
		e.preventDefault();
		if (!draft) return;
		busy = true;
		error = '';
		try {
			const n = $state.snapshot(draft);
			if (!n.hostAccess) n.hostAddress = '';
			await api.post('/docker/networks', n);
			draft = null;
			await load();
		} catch (e) {
			error = apiErrorText(e, { detail: true });
			await load();
		} finally {
			busy = false;
		}
	}
	async function erase() {
		if (!remove) return;
		busy = true;
		error = '';
		try {
			await api.delete(`/docker/networks/${encodeURIComponent(remove.name)}`);
			remove = null;
			await load();
		} catch (e) {
			error = apiErrorText(e, { detail: true });
		} finally {
			busy = false;
		}
	}
</script>

<PageHead title={t('dnet.title')}
	><p>{t('dnet.intro')}</p>
	{#snippet actions()}
		<button class="btn" disabled={!parents.length || busy} onclick={add}>{t('lan.add')}</button>
		<button
			class="btn btn-primary"
			disabled={busy}
			onclick={() => {
				error = '';
				bridge = { name: '', subnet: '', internal: false };
			}}><Icon name="plus" size={15} />{t('dnet.new')}</button
		>
	{/snippet}</PageHead
>
{#if error && !draft && !bridge}<div class="notice-error" role="alert">{error}</div>{/if}
{#if message}<p class="notice-ok" role="status">{message}</p>{/if}

<section class="tile ui-card" aria-labelledby="dnet-list">
	<div class="card-head">
		<h2 id="dnet-list">{t('dnet.list')}</h2>
		<span class="card-meta">{all.length}</span>
		<div class="card-tools">
			<button class="btn" disabled={busy || !unused.length} onclick={() => (pruning = true)}
				>{t('dnet.prune', { count: unused.length })}</button
			>
		</div>
	</div>
	{#if allError}<p class="notice-error inside" role="alert">{allError}</p>{/if}
	{#if all.length}
		<div class="table-wrap">
			<table class="data-table nets">
				<thead>
					<tr>
						<th scope="col">{t('dres.col.name')}</th>
						<th scope="col" class="driver">{t('dres.col.driver')}</th>
						<th scope="col">{t('dnet.subnet')}</th>
						<th scope="col" class="used">{t('dres.col.usedBy')}</th>
						<th scope="col"><span class="sr-only">{t('ui.more')}</span></th>
					</tr>
				</thead>
				<tbody>
					{#each all as n (n.id || n.name)}
						{@const used = usedByText(n.usedBy)}
						<tr>
							<td class="name">
								<span class="mono nname">{n.name}</span>
								{#if n.protected || n.kind !== 'user'}<span class="chip" title={t('dnet.kept')}
										><Icon name="lock" size={11} />{reason(n)}</span
									>{/if}
								{#if n.internal}<span class="chip">{t('dnet.internal')}</span>{/if}
								<span class="sub narrow">{used || t('dres.unused')}</span>
							</td>
							<td class="driver muted">{n.driver}</td>
							<td class="mono subnet">{n.subnets.join(', ') || '–'}</td>
							<td class="used" class:muted={!used}>{used || t('dres.unused')}</td>
							<td class="actions">
								{#if n.kind === 'user' && !n.protected && !used}<button
										type="button"
										class="btn btn-danger-outline small"
										disabled={busy}
										aria-label={t('dres.deleteNamed', { name: n.name })}
										onclick={() => (dropBridge = n)}>{t('dnet.remove')}</button
									>{/if}
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{:else if !allError}
		<p class="muted none">{t('common.loading')}</p>
	{/if}
</section>

<section class="tile ui-card" aria-labelledby="dnet-lan">
	<div class="card-head">
		<h2 id="dnet-lan">{t('lan.title')}</h2>
		<span class="card-meta">{t('lan.intro')}</span>
	</div>
	{#if !parents.length && !loading}<p class="notice-warn inside">
			{t('lan.needParent')} <a href="/settings/network">{t('netcfg.title')}</a>
		</p>{/if}
	{#if loading}<p class="muted none" role="status">
			{t('common.loading')}
		</p>{:else if !networks.length}<p class="muted none">{t('lan.empty')}</p>{:else}<div
			class="content"
		>
			{#each networks as n (n.name)}<article>
					<div class="row">
						<div>
							<strong>{n.name}</strong>
							<p class="muted">
								{parents.find((p) => p.id === n.parent)?.name ?? n.parent} · {n.driver}
							</p>
						</div>
						<button
							class="btn btn-danger-outline small"
							disabled={busy}
							onclick={() => (remove = n)}>{t('netx.remove')}</button
						>
					</div>
					<p class="mono">{n.subnet} · {n.range}</p>
					<p class="muted">
						{t('netx.gateway')}: {n.gateway}{n.hostAccess
							? ` · ${t('lan.hostAddress')}: ${n.hostAddress}`
							: ''}
					</p>
					{#if n.error}<div class="notice-error" role="alert">{n.error}</div>{/if}
				</article>{/each}
		</div>{/if}
	<p class="muted none foot">{t('lan.external')}</p>
</section>
{#if draft}<Dialog title={t('lan.add')} wide locked={busy} onclose={() => (draft = null)}
		><form onsubmit={create}>
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
			<div class="two">
				<fieldset>
					<legend>{t('netx.connection')}</legend><label
						><span>{t('netx.name')}</span><input
							class="field"
							required
							pattern="[a-z0-9][a-z0-9_-]*"
							maxlength="24"
							bind:value={draft.name}
						/></label
					><label
						><span>{t('lan.parent')}</span><select class="field" bind:value={draft.parent}
							>{#each parents as p}<option value={p.id}
									>{p.name}{p.vlan ? ` · VLAN ${p.vlan}` : ''}</option
								>{/each}</select
						></label
					><label
						><span>{t('lan.driver')}</span><select class="field" bind:value={draft.driver}
							><option value="ipvlan">ipvlan L2</option><option value="macvlan">macvlan</option
							></select
						><small class="muted">{t('lan.driverHint')}</small></label
					>
				</fieldset>
				<fieldset>
					<legend>{t('lan.addresses')}</legend><label
						><span>{t('lan.subnet')}</span><input
							class="field mono"
							bind:value={draft.subnet}
							required
						/></label
					><label
						><span>{t('netx.gateway')}</span><input
							class="field mono"
							bind:value={draft.gateway}
							required
						/></label
					><label
						><span>{t('lan.range')}</span><input
							class="field mono"
							bind:value={draft.range}
							required
						/><small class="muted">{t('lan.rangeHint')}</small></label
					>
				</fieldset>
			</div>
			<label class="check"
				><input type="checkbox" bind:checked={draft.hostAccess} /><span>{t('lan.hostAccess')}</span
				></label
			>{#if draft.hostAccess}<label
					><span>{t('lan.hostAddress')}</span><input
						class="field mono"
						bind:value={draft.hostAddress}
						required
					/><small class="muted">{t('lan.hostHint')}</small></label
				>{/if}
			<p class="notice-warn">{t('lan.external')}</p>
			<div class="dialog-actions">
				<button class="btn" type="button" disabled={busy} onclick={() => (draft = null)}
					>{t('common.cancel')}</button
				><button class="btn btn-primary" disabled={busy}>{t('lan.add')}</button>
			</div>
		</form></Dialog
	>{/if}
{#if remove}<Dialog title={t('netx.remove')} locked={busy} onclose={() => (remove = null)}
		>{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<p>{t('lan.removeHint', { name: remove.name })}</p>
		<div class="dialog-actions">
			<button class="btn" disabled={busy} onclick={() => (remove = null)}
				>{t('common.cancel')}</button
			><button class="btn btn-danger" disabled={busy} onclick={erase}>{t('netx.remove')}</button>
		</div></Dialog
	>{/if}

{#if bridge}<Dialog title={t('dnet.new')} locked={busy} onclose={() => (bridge = null)}
		><form onsubmit={createBridge}>
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
			<label
				><span>{t('netx.name')}</span><input
					class="field"
					required
					pattern="[a-zA-Z0-9][a-zA-Z0-9_.\-]*"
					maxlength="64"
					bind:value={bridge.name}
				/></label
			>
			<label
				><span>{t('dnet.subnetOptional')}</span><input
					class="field mono"
					placeholder="172.30.0.0/24"
					aria-invalid={!subnetOk}
					bind:value={bridge.subnet}
				/><small class="muted">{t('dnet.subnetHint')}</small>
				{#if !subnetOk}<small class="warn-text" role="alert">{t('dnet.subnetBad')}</small
					>{/if}</label
			>
			<label class="check"
				><input type="checkbox" bind:checked={bridge.internal} /><span
					>{t('dnet.internalLong')}</span
				></label
			>
			<div class="dialog-actions">
				<button class="btn" type="button" disabled={busy} onclick={() => (bridge = null)}
					>{t('common.cancel')}</button
				><button class="btn btn-primary" disabled={busy || !subnetOk}>{t('dnet.create')}</button>
			</div>
		</form></Dialog
	>{/if}
{#if dropBridge}<Dialog title={t('dnet.remove')} locked={busy} onclose={() => (dropBridge = null)}
		><p>{t('dnet.removeText', { name: dropBridge.name })}</p>
		<div class="dialog-actions">
			<button class="btn" disabled={busy} onclick={() => (dropBridge = null)}
				>{t('common.cancel')}</button
			><button class="btn btn-danger" disabled={busy} onclick={eraseBridge}
				>{t('dnet.remove')}</button
			>
		</div></Dialog
	>{/if}
{#if pruning}<Dialog title={t('dnet.pruneTitle')} locked={busy} onclose={() => (pruning = false)}
		><p>{t('dnet.pruneText', { count: unused.length })}</p>
		<ul class="names">
			{#each unused as n (n.name)}<li class="mono">{n.name}</li>{/each}
		</ul>
		<div class="dialog-actions">
			<button class="btn" disabled={busy} onclick={() => (pruning = false)}
				>{t('common.cancel')}</button
			><button class="btn btn-danger" disabled={busy} onclick={prune}>{t('dnet.remove')}</button>
		</div></Dialog
	>{/if}

<style>
	.content {
		padding: 0 16px;
	}
	fieldset {
		padding: var(--tile-pad);
	}
	.inside {
		margin: 12px 16px 0;
	}
	.none {
		margin: 0;
		padding: 12px 16px;
	}
	.foot {
		border-top: 1px solid var(--panel-border, var(--border));
		font-size: var(--text-sm);
	}
	.nname {
		overflow-wrap: anywhere;
	}
	.subnet {
		white-space: nowrap;
	}
	.name .chip {
		margin-left: 6px;
	}
	.actions {
		width: 1%;
		white-space: nowrap;
		text-align: right;
	}
	.small {
		min-height: 30px;
		padding: 0 10px;
		font-size: var(--text-base);
	}
	.narrow {
		display: none;
	}
	.names {
		margin: 8px 0;
		padding: 0;
		list-style: none;
		max-height: 200px;
		overflow: auto;
		font-size: var(--text-base);
	}
	.warn-text {
		color: var(--warn-text);
	}
	@media (max-width: 639px) {
		.nets .driver {
			display: none;
		}
	}
	@media (max-width: 479px) {
		.nets .used {
			display: none;
		}
		.narrow {
			display: block;
		}
	}
	article {
		padding: var(--tile-pad) 0;
		border-bottom: 1px solid var(--border);
	}
	article:last-child {
		border: 0;
	}
	.row,
	.dialog-actions {
		display: flex;
		gap: var(--gap);
		justify-content: space-between;
		align-items: center;
	}
	.row > div {
		min-width: 0;
	}
	.row p,
	.mono {
		overflow-wrap: anywhere;
	}
	form,
	label,
	fieldset {
		display: grid;
		gap: var(--gap);
	}
	fieldset {
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		min-width: 0;
	}
	.two {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: var(--gap);
	}
	.check {
		display: flex;
		align-items: center;
		min-height: 44px;
	}
	.check input {
		width: 18px;
		height: 18px;
	}
	.dialog-actions {
		justify-content: flex-end;
		margin-top: var(--gap);
	}
	@media (max-width: 760px) {
		.two {
			grid-template-columns: 1fr;
		}
	}
</style>
