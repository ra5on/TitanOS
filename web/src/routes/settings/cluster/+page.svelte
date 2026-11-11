<script lang="ts">
	// Settings › Cluster (backend package cluster): several TitanOS that trust
	// each other fully, as in Proxmox. Accounts are the same everywhere, every
	// node can be managed from here, backups go from node to node.
	import { poll } from '#lib/poll.ts';
	import { onMount } from 'svelte';
	import { api, ApiError } from '#lib/api.ts';
	import DangerZone from '#lib/components/DangerZone.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import ReconfirmFields, { reconfirmComplete } from '#lib/components/ReconfirmFields.svelte';
	import { confirmWithPasskey, hasPasskeyHere } from '#lib/passkeys.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import { formatBytes, formatDuration } from '#lib/format.ts';
	import { apiErrorText, errorText, t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { switchNode } from '#lib/cluster-node.ts';
	import { revealTarget } from '#lib/reveal-target.ts';
	import Icon from '#lib/components/Icon.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import ProbeDialog from '#lib/components/cluster/ProbeDialog.svelte';
	import RouteDialog from '#lib/components/cluster/RouteDialog.svelte';
	import UpdateAllDialog from '#lib/components/cluster/UpdateAllDialog.svelte';
	import NodeNotices from '#lib/components/cluster/NodeNotices.svelte';
	import {
		WAYS,
		compareVersions,
		manageable,
		otherVersion,
		wayLabel,
		type ClusterPage,
		type JoinPreview,
		type NodeView,
		type Way,
		type Ways,
	} from '#lib/cluster.svelte.ts';

	let page = $state<ClusterPage | null>(null);
	let loadError = $state('');
	let busy = $state(false);
	let error = $state('');
	let copied = $state(false);

	// The join information lets a node in that sees everything: it is shown
	// only after password (and code) or a passkey, and hidden again after a
	// while, on leaving the page and whenever the key changes.
	const JOIN_INFO_SHOWN = 5 * 60 * 1000;
	let joinInfo = $state('');
	let hideTimer: ReturnType<typeof setTimeout> | undefined;
	let revealing = $state(false);
	let password = $state('');
	let code = $state('');
	let passkeyHere = $state(false);
	let revealError = $state('');

	let newName = $state('');
	let joinText = $state('');
	let preview = $state<JoinPreview | null>(null);
	let confirmJoin = $state(false);
	let confirmLeave = $state(false);
	let confirmKey = $state(false);
	let removing = $state<NodeView | null>(null);

	let clusterName = $state('');
	let internet = $state(false);
	let extra = $state('');
	let ways = $state<Ways>({});
	let tunnel = $state(false);
	let saved = $state(false);
	let probing = $state(false);
	let routing = $state<NodeView | null>(null);
	let updating = $state(false);
	let handing = $state<NodeView | null>(null);
	/** The local account about to become one of the cluster. */
	let sharing = $state<string | null>(null);
	// PLAN 78-1: which node rows are open (a node's chips "N Fehler" open its
	// row, where its warnings and errors are read and deleted).
	let openNodes = $state<Record<string, boolean>>({});
	// The row of the join information opens when it is shown.
	let joinOpen = $state(false);

	const owner = $derived(session.data?.user?.role === 'owner');
	const cluster = $derived(page?.cluster);
	const inCluster = $derived(!!cluster?.name);
	const manages = $derived(!!cluster?.manages);
	const isAdmin = $derived(!!cluster && cluster.admin === cluster.self);
	const version = $derived(cluster?.version ?? '');
	const adminNode = $derived(page?.nodes.find((n) => n.id === cluster?.admin));
	// The VPNs a node can find on itself, with what it found.
	const FOUND_WAYS: Way[] = ['home', 'tailscale', 'netbird', 'wireguard'];
	function foundOf(way: Way): string[] {
		if (!cluster) return [];
		return cluster.found.filter((a) => (cluster.foundWays[a] ?? '') === way);
	}

	function problem(err: unknown): string {
		if (err instanceof ApiError && err.code === 'cluster_version')
			return t('cluster.versionMismatch', { version: err.message.replace(/^.*version /, '') });
		return apiErrorText(err);
	}

	function fill(next: ClusterPage) {
		if (page && next.cluster.nodes.length !== page.cluster.nodes.length) hideJoinInfo();
		page = next;
		clusterName = next.cluster.name ?? '';
		internet = next.cluster.internet;
		extra = next.cluster.extra.join('\n');
		// Missing values are the defaults, so that every list shows a choice.
		const w = next.cluster.ways;
		ways = {
			...w,
			manage: w.manage ?? '',
			backup: w.backup ?? '',
			return: w.return ?? 0,
			alert: w.alert ?? 0,
		};
		tunnel = !!next.cluster.tunnel.enabled;
	}

	async function load() {
		loadError = '';
		try {
			fill(await api.get<ClusterPage>('/cluster'));
		} catch (err) {
			loadError = problem(err);
		}
	}
	onMount(() => {
		const stop = poll(load, { interval: 20000, skip: () => busy || !inCluster });
		return () => {
			stop();
			clearTimeout(hideTimer);
		};
	});

	function hideJoinInfo() {
		joinInfo = '';
		copied = false;
		clearTimeout(hideTimer);
	}

	async function openReveal() {
		password = '';
		code = '';
		revealError = '';
		revealing = true;
		passkeyHere = await hasPasskeyHere();
	}

	async function reveal(proof: Record<string, unknown>) {
		busy = true;
		revealError = '';
		try {
			const answer = await api.post<{ joinInfo: string }>('/cluster/join-info', proof);
			joinInfo = answer.joinInfo;
			joinOpen = true;
			revealing = false;
			clearTimeout(hideTimer);
			hideTimer = setTimeout(hideJoinInfo, JOIN_INFO_SHOWN);
		} catch (err) {
			revealError = problem(err);
		} finally {
			busy = false;
			password = '';
			code = '';
		}
	}

	function revealWithPassword(event: SubmitEvent) {
		event.preventDefault();
		if (reconfirmComplete(password, code)) reveal({ password, code });
	}

	async function revealWithPasskey() {
		revealError = '';
		let passkey;
		try {
			passkey = await confirmWithPasskey();
		} catch (err) {
			if (err instanceof ApiError) revealError = problem(err);
			else
				revealError = t(
					err instanceof DOMException && err.name !== 'NotAllowedError'
						? 'passkeys.browserError'
						: 'passkeys.cancelled',
				);
			return;
		}
		await reveal({ passkey });
	}

	async function act(run: () => Promise<ClusterPage | void>) {
		busy = true;
		error = '';
		saved = false;
		try {
			const next = await run();
			if (next) fill(next);
			return true;
		} catch (err) {
			error = problem(err);
			return false;
		} finally {
			busy = false;
		}
	}

	async function create(event: SubmitEvent) {
		event.preventDefault();
		if (!newName.trim()) return;
		await act(() => api.post<ClusterPage>('/cluster', { name: newName.trim() }));
	}

	async function check(event: SubmitEvent) {
		event.preventDefault();
		preview = null;
		await act(async () => {
			preview = await api.post<JoinPreview>('/cluster/preview', { info: joinText.trim() });
		});
	}

	async function join() {
		if (await act(() => api.post<ClusterPage>('/cluster/join', { info: joinText.trim() }))) {
			confirmJoin = false;
			preview = null;
			joinText = '';
		}
	}

	async function leave() {
		if (await act(() => api.post<ClusterPage>('/cluster/leave'))) {
			confirmLeave = false;
			hideJoinInfo();
		}
	}

	async function newKey() {
		if (await act(() => api.post<ClusterPage>('/cluster/join-key'))) {
			confirmKey = false;
			hideJoinInfo();
		}
	}

	async function setManage(node: NodeView, manage: boolean) {
		await act(() =>
			api.put<ClusterPage>('/cluster/nodes/' + encodeURIComponent(node.id) + '/manage', {
				manage,
			}),
		);
	}

	async function remove(node: NodeView) {
		if (await act(() => api.delete<ClusterPage>('/cluster/nodes/' + encodeURIComponent(node.id))))
			removing = null;
	}

	async function saveSettings(event: SubmitEvent) {
		event.preventDefault();
		const lines = extra
			.split(/\s+/)
			.map((l) => l.trim())
			.filter(Boolean);
		if (
			await act(() =>
				api.put<ClusterPage>('/cluster', {
					name: clusterName.trim(),
					internet,
					extra: lines,
					ways: manages ? { ...ways, only: !!ways.manage && !!ways.only } : undefined,
					tunnel: manages ? tunnel : undefined,
				}),
			)
		)
			saved = true;
	}

	async function copy() {
		if (!joinInfo) return;
		try {
			await navigator.clipboard.writeText(joinInfo);
			copied = true;
			setTimeout(() => (copied = false), 2000);
		} catch {
			// The field stays selectable to copy by hand.
		}
	}

	async function shareAccount(name: string) {
		if (
			await act(() =>
				api.post<ClusterPage>('/cluster/accounts/' + encodeURIComponent(name) + '/share'),
			)
		)
			sharing = null;
	}

	async function handOver(node: NodeView) {
		if (
			await act(() =>
				api.post<ClusterPage>('/cluster/nodes/' + encodeURIComponent(node.id) + '/admin'),
			)
		)
			handing = null;
	}

	function older(n: NodeView): boolean {
		return otherVersion(n, version) && compareVersions(n.status?.version ?? '', version) < 0;
	}

	function used(n: NodeView): string {
		const m = n.status?.memory;
		if (!m || !m.totalBytes) return '';
		return t('cluster.detail.memoryValue', {
			used: formatBytes(m.totalBytes - m.availableBytes),
			total: formatBytes(m.totalBytes),
		});
	}

	function volumeProblems(n: NodeView): number {
		return (n.status?.volumes ?? []).filter(
			(v) => !v.mounted || !['ok', 'clean', 'active', 'healthy'].includes(v.state),
		).length;
	}
</script>

<PageHead title={t('cluster.title')}>
	<p class="muted">{t('cluster.intro')}</p>
</PageHead>

<!-- The warnings and errors of a node: its chips, as a button where this
     node may read them (they open the node's row with the messages). -->
{#snippet noticeChips(node: NodeView)}
	{#if node.status?.errors}<span class="chip error"
			>{t('cluster.errors', { count: node.status.errors })}</span
		>{/if}
	{#if node.status?.warnings}<span class="chip warn"
			>{t('cluster.warnings', { count: node.status.warnings })}</span
		>{/if}
{/snippet}

{#if loadError}
	<div class="notice-error" role="alert">
		{loadError} <button class="btn" onclick={() => load()}>{t('ui.retry')}</button>
	</div>
{:else if !page || !cluster}
	<p class="muted loading">{t('common.loading')}</p>
{:else if !inCluster}
	<!-- PLAN 78-1: the two ways into a cluster are two rows; pasting and
	     checking the join information are the parts of the second one. -->
	{#if !owner}<p class="notice-warn">{t('cluster.ownerOnly')}</p>{/if}
	{#snippet joinParts()}
		<form onsubmit={check}>
			<div class="srow-block">
				<label class="stack"
					><span>{t('cluster.joinInfo')}</span><textarea
						class="field mono"
						rows="4"
						bind:value={joinText}
						spellcheck="false"
						placeholder="titanos-cluster:…"
						disabled={busy}></textarea></label
				>
				<div class="row-actions">
					<button class="btn" disabled={busy || !joinText.trim()}>{t('cluster.check')}</button>
				</div>
			</div>
		</form>
		{#if preview}
			<div class="srow-block preview">
				<p>
					{t('cluster.previewCluster', {
						name: preview.cluster,
						nodes: preview.nodes.join(', '),
					})}
				</p>
				{#if preview.replaced.length}<p class="notice-warn">
						{t('cluster.previewReplaced', { names: preview.replaced.join(', ') })}
					</p>{/if}
				{#if preview.local.length}<p>
						{t('cluster.previewLocal', { names: preview.local.join(', ') })}
					</p>{/if}
				{#if preview.taken.length}<p>
						{t('cluster.previewTaken', { names: preview.taken.join(', ') })}
					</p>{/if}
				<div class="row-actions">
					<button class="btn btn-primary" disabled={busy} onclick={() => (confirmJoin = true)}
						>{t('cluster.joinButton')}</button
					>
				</div>
			</div>
		{/if}
	{/snippet}
	<SettingGroup>
		<SettingRow icon="plus" title={t('cluster.create')} desc={t('cluster.createHint')}>
			{#snippet control()}
				{#if owner}
					<form class="create" onsubmit={create}>
						<input
							class="field"
							type="text"
							bind:value={newName}
							maxlength="63"
							placeholder={t('cluster.namePlaceholder')}
							aria-label={t('cluster.name')}
							disabled={busy}
						/>
						<button class="btn btn-primary" disabled={busy || !newName.trim()}
							>{t('cluster.createButton')}</button
						>
					</form>
				{/if}
			{/snippet}
		</SettingRow>
		<SettingRow
			id="cluster-join"
			icon="tailnet"
			title={t('cluster.join')}
			desc={t('cluster.joinHint')}
			open
			children={owner ? joinParts : undefined}
		/>
	</SettingGroup>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
{:else}
	<!-- PLAN 78-1: every node is a row – name, role and state; it opens to
	     its details, its messages and what can be done with it. -->
	<SettingGroup title={t('cluster.nodesOf', { name: cluster.name ?? '' })} id="cluster-nodes">
		{#snippet tools()}
			{#if manages}
				<button type="button" class="btn small" onclick={() => (probing = true)}
					><Icon name="refresh" size={15} />{t('cluster.probe')}</button
				>
				<button type="button" class="btn small" onclick={() => (updating = true)}
					><Icon name="download" size={15} />{t('cluster.updateAll')}</button
				>
			{/if}
		{/snippet}
		{#each page.nodes as node (node.id)}
			{@const route = cluster.routes[node.id]}
			{@const readable = node.self || (manages && manageable(node))}
			{@const noticed = !!(node.status?.errors || node.status?.warnings)}
			{@const takeOver = owner && !isAdmin && node.id === cluster.admin && !node.online}
			<SettingRow
				id="node-{node.id}"
				icon="tailnet"
				title={node.name}
				bind:open={() => !!openNodes[node.id], (on) => (openNodes[node.id] = on)}
			>
				{#snippet badge()}
					{#if node.self}<span class="chip">{t('cluster.thisNode')}</span>{/if}
					<span class="chip" class:info={node.id === cluster.admin || node.manage}
						>{t(
							node.id === cluster.admin
								? 'cluster.roleAdmin'
								: node.manage
									? 'cluster.roleManage'
									: 'cluster.roleMember',
						)}</span
					>
				{/snippet}
				{#snippet description()}
					<span class="line">
						{#if node.self}
							<span class="mono">{node.addresses.slice(0, 4).join(' · ')}</span>
						{:else if node.online && node.way}
							<span
								>{t('cluster.via', { way: wayLabel(node.way) }) +
									(node.millis ? ` · ${node.millis} ms` : '')}</span
							>
							<span class="mono">{node.address}</span>
							{#if node.fallback}<span class="warn-text"
									>{t((ways.return ?? 0) < 0 ? 'cluster.fallbackStays' : 'cluster.fallback', {
										way: wayLabel(node.preferred),
									})}</span
								>{/if}
						{:else if node.error}
							<span>{errorText(node.error)}</span>
							<span class="mono">{node.addresses.slice(0, 3).join(' · ')}</span>
						{/if}
					</span>
					{#if node.clockSkew}<span class="line warn-text"
							>{t(node.clockSkew > 0 ? 'cluster.clockAhead' : 'cluster.clockBehind', {
								minutes: Math.max(1, Math.round(Math.abs(node.clockSkew) / 60)),
							})}</span
						>{/if}
				{/snippet}
				{#snippet control()}
					<!-- The state in words (it was a coloured dot before). -->
					{#if node.online}<span class="chip ok">{t('cluster.online')}</span>{:else}<span
							class="chip error">{t('cluster.offline')}</span
						>{/if}
					{#if node.status && otherVersion(node, version)}<span class="chip warn"
							>{t(older(node) ? 'cluster.olderVersion' : 'cluster.newerVersion')}</span
						>{/if}
					{#if route?.manage}<span class="chip info"
							>{t('cluster.ownWay', { way: wayLabel(route.manage) })}</span
						>{/if}
					{#if noticed}
						{#if readable}
							<button
								type="button"
								class="chip-button"
								aria-expanded={!!openNodes[node.id]}
								aria-controls="node-{node.id}-parts"
								title={t('cluster.noticesShow')}
								onclick={() => (openNodes[node.id] = !openNodes[node.id])}
							>
								{@render noticeChips(node)}
							</button>
						{:else}
							{@render noticeChips(node)}
						{/if}
					{/if}
					{#if !node.self && manages}
						<button
							class="btn small"
							disabled={!manageable(node)}
							onclick={() => switchNode(node.id)}>{t('cluster.manage')}</button
						>
						{#if older(node) && node.online}<button
								class="btn small btn-primary"
								onclick={() => switchNode(node.id, '/settings/updates')}
								>{t('cluster.update')}</button
							>{/if}
					{/if}
				{/snippet}
				<div class="srow-block">
					<dl>
						<div>
							<dt>{t('cluster.detail.addresses')}</dt>
							<dd class="mono">
								{#each node.addresses as address (address)}<span>{address}</span>{:else}–{/each}
							</dd>
						</div>
						{#if node.status}
							<div>
								<dt>{t('cluster.detail.version')}</dt>
								<dd>
									{node.status.version}{#if older(node)}{' '}
										<span class="warn-text">→ {version}</span>{/if}
								</dd>
							</div>
							<div>
								<dt>{t('cluster.detail.uptime')}</dt>
								<dd>{formatDuration(node.status.uptimeSeconds)}</dd>
							</div>
							{#if node.status.cpuPercent !== null}
								<div>
									<dt>{t('cluster.detail.cpu')}</dt>
									<dd>{Math.round(node.status.cpuPercent)} %</dd>
								</div>
							{/if}
							{#if used(node)}
								<div>
									<dt>{t('cluster.detail.memory')}</dt>
									<dd>{used(node)}</dd>
								</div>
							{/if}
							<div>
								<dt>{t('cluster.detail.volumes')}</dt>
								<dd>
									{node.status.volumes.length}{#if volumeProblems(node)}{' '}
										<span class="warn-text"
											>({t('cluster.volumeProblems', { count: volumeProblems(node) })})</span
										>{/if}
								</dd>
							</div>
						{/if}
					</dl>
				</div>
				{#if noticed && readable}
					<div class="srow-block">
						<NodeNotices node={node.id} self={node.self} name={node.name} onchange={load} />
					</div>
				{/if}
				{#if manages && (!node.self || (owner && !isAdmin && adminNode && !adminNode.online))}
					<div
						class="srow-block actions"
						role="group"
						aria-label={t('cluster.actionsFor', { name: node.name })}
					>
						{#if !node.self}
							<button type="button" class="btn" onclick={() => (routing = node)}
								><Icon name="globe" size={16} />{t('cluster.routeMenu')}</button
							>
						{/if}
						{#if owner && isAdmin && !node.self}
							<button
								type="button"
								class="btn"
								disabled={busy}
								onclick={() => setManage(node, !node.manage)}
								><Icon name="shield" size={16} />{t(
									node.manage ? 'cluster.manageRevoke' : 'cluster.manageAllow',
								)}</button
							>
							<button type="button" class="btn" onclick={() => (handing = node)}
								><Icon name="users" size={16} />{t('cluster.handOver', { name: node.name })}</button
							>
						{/if}
						{#if takeOver}
							<button
								type="button"
								class="btn"
								onclick={() => (handing = page?.nodes.find((n) => n.self) ?? null)}
								><Icon name="users" size={16} />{t('cluster.takeOver')}</button
							>
						{/if}
						{#if owner && node.id !== cluster.admin && !node.self}
							<button type="button" class="btn btn-danger" onclick={() => (removing = node)}
								><Icon name="trash" size={16} />{t('cluster.remove')}</button
							>
						{/if}
					</div>
				{/if}
			</SettingRow>
		{/each}
		<p class="muted small">{t(manages ? 'cluster.trust' : 'cluster.memberHint')}</p>
	</SettingGroup>

	<!-- The settings of the cluster are one form with one Save, as before:
	     every setting a row of its own, grouped under plain headings. -->
	<form class="settings-form" onsubmit={saveSettings}>
		<SettingGroup title={t('cluster.settings')}>
			<SettingRow icon="edit" title={t('cluster.name')} for="cluster-name">
				{#snippet control()}
					<input
						id="cluster-name"
						class="field"
						type="text"
						bind:value={clusterName}
						maxlength="63"
						disabled={busy || !manages}
					/>
				{/snippet}
			</SettingRow>
		</SettingGroup>

		<SettingGroup title={t('cluster.connection')}>
			<SettingRow
				icon="globe"
				title={t('cluster.manageWay')}
				desc={t('cluster.manageWayHint')}
				for="cluster-way"
			>
				{#snippet control()}
					<select
						id="cluster-way"
						class="field"
						bind:value={ways.manage}
						disabled={busy || !manages}
					>
						<option value="">{t('cluster.wayAuto')}</option>
						{#each WAYS as way (way)}<option value={way}
								>{t('cluster.wayFirst', { way: wayLabel(way) })}</option
							>{/each}
					</select>
				{/snippet}
			</SettingRow>
			<SettingRow
				icon="lock"
				title={t('cluster.onlyWay')}
				desc={t(ways.only && ways.manage ? 'cluster.onlyOn' : 'cluster.onlyOff')}
				for="cluster-only"
			>
				{#snippet control()}
					<input
						id="cluster-only"
						class="switch"
						type="checkbox"
						role="switch"
						checked={!!ways.only && !!ways.manage}
						onchange={(e) => (ways.only = e.currentTarget.checked)}
						disabled={busy || !manages || !ways.manage}
					/>
				{/snippet}
			</SettingRow>
			<SettingRow
				icon="undo"
				title={t('cluster.returnWay')}
				desc={t('cluster.returnWayHint')}
				for="cluster-return"
			>
				{#snippet control()}
					<select
						id="cluster-return"
						class="field"
						bind:value={ways.return}
						disabled={busy || !manages}
					>
						<option value={0}>{t('cluster.everyMinute')}</option>
						<option value={5}>{t('cluster.everyMinutes', { count: 5 })}</option>
						<option value={15}>{t('cluster.everyMinutes', { count: 15 })}</option>
						<option value={-1}>{t('cluster.never')}</option>
					</select>
				{/snippet}
			</SettingRow>
			<SettingRow
				icon="archive"
				title={t('cluster.backupWay')}
				desc={t('cluster.backupWayHint')}
				for="cluster-backup"
			>
				{#snippet control()}
					<select
						id="cluster-backup"
						class="field"
						bind:value={ways.backup}
						disabled={busy || !manages}
					>
						<option value="">{t('cluster.backupLikeManage')}</option>
						{#each WAYS as way (way)}<option value={way}
								>{t('cluster.onlyThis', { way: wayLabel(way) })}</option
							>{/each}
					</select>
				{/snippet}
			</SettingRow>
		</SettingGroup>

		<SettingGroup title={t('cluster.waysTitle')}>
			{#each FOUND_WAYS as way (way)}
				{@const found = foundOf(way)}
				<SettingRow
					icon={way === 'home' ? 'home' : 'tailnet'}
					title={wayLabel(way)}
					desc={way === 'tailscale' && found.some((a) => a.includes('.ts.net'))
						? t('cluster.tailscaleNameHint')
						: undefined}
				>
					{#snippet control()}
						{#if found.length}<span class="mono small">{found.join(' · ')}</span>{/if}
						<span class="chip" class:ok={found.length > 0}
							>{t(found.length ? 'cluster.wayFound' : 'cluster.wayNotFound')}</span
						>
					{/snippet}
				</SettingRow>
			{/each}
			<SettingRow icon="shield" title={t('cluster.tunnel')} for="cluster-tunnel">
				{#snippet description()}
					{#if cluster.tunnel.error}<span class="error-text">{cluster.tunnel.error}</span>{:else}{t(
							'cluster.tunnelHint',
							{ port: cluster.tunnel.port ?? 51821 },
						)}{/if}
				{/snippet}
				{#snippet control()}
					{#if tunnel && cluster.tunnel.enabled && cluster.tunnel.address}<span
							>{t('cluster.tunnelOn', { address: cluster.tunnel.address })}</span
						>{/if}
					<input
						id="cluster-tunnel"
						class="switch"
						type="checkbox"
						role="switch"
						bind:checked={tunnel}
						disabled={busy || !manages}
					/>
				{/snippet}
			</SettingRow>
			<SettingRow icon="external" title={t('cluster.internetWay')} for="cluster-internet">
				{#snippet description()}
					{#if internet}<span class="warn-text">{t('cluster.internetWarn')}</span>{:else}{t(
							'cluster.internetHint',
						)}{/if}
				{/snippet}
				{#snippet control()}
					<input
						id="cluster-internet"
						class="switch"
						type="checkbox"
						role="switch"
						bind:checked={internet}
						disabled={busy || !manages}
					/>
				{/snippet}
			</SettingRow>
			<SettingRow
				icon="list"
				title={t('cluster.extra')}
				desc={t('cluster.extraHint')}
				for="cluster-extra"
			>
				{#snippet control()}
					<textarea
						id="cluster-extra"
						class="field mono extra"
						rows="2"
						bind:value={extra}
						spellcheck="false"
						placeholder="nas.example.org"
						disabled={busy}></textarea>
				{/snippet}
			</SettingRow>
		</SettingGroup>

		<SettingGroup title={t('cluster.notices')}>
			<SettingRow
				icon="bell"
				title={t('cluster.alert')}
				desc={t('cluster.alertHint')}
				for="cluster-alert"
			>
				{#snippet control()}
					<select
						id="cluster-alert"
						class="field"
						bind:value={ways.alert}
						disabled={busy || !manages}
					>
						<option value={5}>{t('cluster.alertAfter', { count: 5 })}</option>
						<option value={0}>{t('cluster.alertAfter', { count: 10 })}</option>
						<option value={30}>{t('cluster.alertAfter', { count: 30 })}</option>
						<option value={60}>{t('cluster.alertAfter', { count: 60 })}</option>
						<option value={-1}>{t('cluster.never')}</option>
					</select>
				{/snippet}
			</SettingRow>
			<SettingRow
				icon="clock"
				title={t('cluster.clock')}
				desc={t('cluster.clockHint')}
				for="cluster-clock"
			>
				{#snippet control()}
					<input
						id="cluster-clock"
						class="switch"
						type="checkbox"
						role="switch"
						checked={!ways.clockOff}
						onchange={(e) => (ways.clockOff = !e.currentTarget.checked)}
						disabled={busy || !manages}
					/>
				{/snippet}
			</SettingRow>
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
			<div class="row-actions">
				<button class="btn btn-primary" disabled={busy || !clusterName.trim()}
					>{t('common.save')}</button
				>
				{#if busy || saved}<span class="muted" role="status"
						>{t(busy ? 'ui.saving' : 'ui.saved')}</span
					>{/if}
			</div>
		</SettingGroup>
	</form>

	{#if owner && cluster.localAccounts?.length}
		<SettingGroup
			title={t('cluster.local.title')}
			id="cluster-local"
			desc={t('cluster.local.hint')}
		>
			{#each cluster.localAccounts as name (name)}
				<SettingRow icon="users" title={name}>
					{#snippet control()}
						<button type="button" class="btn" disabled={busy} onclick={() => (sharing = name)}
							>{t('cluster.local.share')}</button
						>
					{/snippet}
				</SettingRow>
			{/each}
		</SettingGroup>
	{/if}

	{#if manages}
		<!-- The join information is a row; shown (after the confirmation), it
		     is the row's part and goes away again by itself. -->
		{#snippet joinInfoParts()}
			<div class="srow-block">
				<textarea
					class="field mono join-info"
					rows="4"
					readonly
					value={joinInfo}
					aria-label={t('cluster.joinInfo')}
					onfocus={(e) => e.currentTarget.select()}></textarea>
				<p class="muted small">{t('cluster.joinInfoHides')}</p>
			</div>
			<div class="srow-block actions">
				<button class="btn btn-primary" onclick={copy}
					>{t(copied ? 'cluster.copied' : 'cluster.copy')}</button
				><button class="btn" onclick={hideJoinInfo}>{t('cluster.hideJoinInfo')}</button>
			</div>
		{/snippet}
		<!-- The anchor of the search; the content comes after the first load. -->
		<div id="cluster-join" use:revealTarget>
			<SettingGroup title={t('cluster.groupJoin')}>
				<SettingRow
					id="cluster-join-info"
					icon="key"
					title={t('cluster.joinInfo')}
					desc={t('cluster.joinInfoHint')}
					bind:open={joinOpen}
					children={joinInfo ? joinInfoParts : undefined}
				>
					{#snippet control()}
						{#if !joinInfo}<button class="btn btn-primary" disabled={busy} onclick={openReveal}
								>{t('cluster.showJoinInfo')}</button
							>{/if}
						<button class="btn" disabled={busy} onclick={() => (confirmKey = true)}
							>{t('cluster.newKey')}</button
						>
					{/snippet}
				</SettingRow>
			</SettingGroup>
		</div>
	{/if}

	{#if owner}
		<!-- Leaving is the destroying action of this page: its own tile at the end. -->
		<DangerZone tile hint={t('cluster.leaveHint')}>
			<button class="btn btn-danger" disabled={busy} onclick={() => (confirmLeave = true)}
				>{t('cluster.leaveButton')}</button
			>
		</DangerZone>
	{/if}
{/if}

{#if confirmJoin && preview}
	<Dialog
		title={t('cluster.joinConfirmTitle', { name: preview.cluster })}
		onclose={() => (confirmJoin = false)}
	>
		<p>{t('cluster.joinConfirm')}</p>
		{#if preview.replaced.length}<p class="notice-warn">
				{t('cluster.previewReplaced', { names: preview.replaced.join(', ') })}
			</p>{/if}
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (confirmJoin = false)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-warn" disabled={busy} onclick={join}
				>{t('cluster.joinButton')}</button
			>
		</div>
	</Dialog>
{/if}

{#if revealing}
	<Dialog title={t('cluster.showJoinInfo')} locked={busy} onclose={() => (revealing = false)}>
		<form class="reveal" onsubmit={revealWithPassword}>
			{#if passkeyHere}<button
					type="button"
					class="btn btn-primary"
					disabled={busy}
					onclick={revealWithPasskey}>{t('cluster.confirmPasskey')}</button
				>
				<p class="muted or">{t('cluster.orPassword')}</p>{/if}
			<ReconfirmFields bind:password bind:code />
			{#if revealError}<p class="notice-error" role="alert">{revealError}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" disabled={busy} onclick={() => (revealing = false)}
					>{t('common.cancel')}</button
				><button
					class={passkeyHere ? 'btn' : 'btn btn-primary'}
					disabled={busy || !reconfirmComplete(password, code)}>{t('cluster.showJoinInfo')}</button
				>
			</div>
		</form>
	</Dialog>
{/if}

{#if sharing}
	<Dialog title={t('cluster.local.share')} locked={busy} onclose={() => (sharing = null)}>
		<p>{t('cluster.local.confirm', { name: sharing })}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (sharing = null)}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-danger"
				disabled={busy}
				onclick={() => sharing && shareAccount(sharing)}>{t('cluster.local.share')}</button
			>
		</div>
	</Dialog>
{/if}

{#if confirmLeave}
	<Dialog title={t('cluster.leave')} onclose={() => (confirmLeave = false)}>
		<p>{t('cluster.leaveConfirm')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (confirmLeave = false)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-danger" disabled={busy} onclick={leave}
				>{t('cluster.leaveButton')}</button
			>
		</div>
	</Dialog>
{/if}

{#if confirmKey}
	<Dialog title={t('cluster.newKey')} onclose={() => (confirmKey = false)}>
		<p>{t('cluster.newKeyConfirm')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (confirmKey = false)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-warn" disabled={busy} onclick={newKey}
				>{t('cluster.newKey')}</button
			>
		</div>
	</Dialog>
{/if}

{#if probing}<ProbeDialog onclose={() => (probing = false)} />{/if}

{#if routing && cluster}
	<RouteDialog
		node={routing}
		route={cluster.routes[routing.id]}
		ways={cluster.ways}
		onclose={() => (routing = null)}
		onsaved={fill}
	/>
{/if}

{#if updating && page}
	<UpdateAllDialog
		nodes={page.nodes.map((n) => ({
			id: n.id,
			name: n.name,
			self: n.self,
			online: n.online,
			version: n.status?.version,
		}))}
		onclose={() => (updating = false)}
		onstarted={() => switchNode(null, '/settings/updates')}
	/>
{/if}

{#if handing && cluster}
	<Dialog
		title={handing.self ? t('cluster.takeOver') : t('cluster.handOver', { name: handing.name })}
		onclose={() => (handing = null)}
	>
		<p>
			{handing.self
				? t('cluster.takeOverConfirm', { name: adminNode?.name ?? '' })
				: t('cluster.handOverConfirm', { name: handing.name })}
		</p>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (handing = null)}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-warn"
				disabled={busy}
				onclick={() => handing && handOver(handing)}
				>{handing.self ? t('cluster.takeOver') : t('cluster.handOverButton')}</button
			>
		</div>
	</Dialog>
{/if}

{#if removing}
	<Dialog title={t('cluster.remove')} onclose={() => (removing = null)}>
		<p>{t('cluster.removeConfirm', { name: removing.name })}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (removing = null)}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-danger"
				disabled={busy}
				onclick={() => removing && remove(removing)}>{t('cluster.remove')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.chip-button {
		display: inline-flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-1);
		padding: 0;
		border: 0;
		background: none;
		color: inherit;
		font: inherit;
		cursor: pointer;
	}
	.chip-button:focus-visible {
		outline: 2px solid var(--focus, var(--accent));
		outline-offset: 2px;
		border-radius: 6px;
	}
	.settings-form {
		display: flex;
		flex-direction: column;
		gap: var(--gap);
	}
	.create {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: flex-end;
		gap: var(--space-2) var(--space-2-5);
	}
	/* The lines under a node's name: how it is reached, what is wrong. */
	.line {
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		gap: 2px var(--space-1-5);
	}
	.small {
		font-size: var(--text-sm);
	}
	dl {
		display: grid;
		grid-template-columns: auto 1fr;
		gap: var(--space-1) var(--space-3);
		margin: 0;
		font-size: var(--text-md);
	}
	dl > div {
		display: contents;
	}
	dt {
		color: var(--muted);
	}
	dd {
		margin: 0;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	dd.mono > span {
		display: block;
	}
	.extra {
		width: 20rem;
		max-width: 100%;
	}
	.row-actions {
		margin-top: var(--space-2);
	}
	.reveal {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
	}
	.join-info {
		width: 100%;
		word-break: break-all;
	}
	.warn-text {
		color: var(--warn-text);
	}
	.error-text {
		color: var(--error-text);
	}
	p {
		margin: 0;
	}
	@media (max-width: 639px) {
		.create {
			justify-content: flex-start;
		}
		.extra {
			width: 100%;
		}
	}
</style>
