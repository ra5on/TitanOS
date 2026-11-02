<script lang="ts">
	// Settings › Cluster (backend package cluster): several TitanOS that trust
	// each other fully, as in Proxmox. Accounts are the same everywhere, every
	// node can be managed from here, backups go from node to node.
	import { poll } from '#lib/poll.ts';
	import { onMount } from 'svelte';
	import { api, ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import ReconfirmFields, { reconfirmComplete } from '#lib/components/ReconfirmFields.svelte';
	import { confirmWithPasskey, hasPasskeyHere } from '#lib/passkeys.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import { formatBytes, formatDuration } from '#lib/format.ts';
	import { apiErrorText, errorText, t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { switchNode } from '#lib/cluster-node.ts';
	import Dropdown from '#lib/components/Dropdown.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import FormRow from '#lib/components/appform/FormRow.svelte';
	import FormSection from '#lib/components/appform/FormSection.svelte';
	import Toggle from '#lib/components/appform/Toggle.svelte';
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
	// The node whose warnings and errors are unfolded under it.
	let noticesOf = $state('');

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

	async function handOver(node: NodeView) {
		if (
			await act(() =>
				api.post<ClusterPage>('/cluster/nodes/' + encodeURIComponent(node.id) + '/admin'),
			)
		)
			handing = null;
	}

	/** The state dot of a node: ok, warn (another way, version or clock) or off. */
	function tone(n: NodeView): 'ok' | 'warn' | 'off' {
		if (!n.online) return 'off';
		if (n.fallback || n.clockSkew || otherVersion(n, version) || n.status?.errors) return 'warn';
		return 'ok';
	}

	function older(n: NodeView): boolean {
		return otherVersion(n, version) && compareVersions(n.status?.version ?? '', version) < 0;
	}

	function used(n: NodeView): string {
		const m = n.status?.memory;
		if (!m || !m.totalBytes) return '';
		return t('cluster.memory', {
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

<PageHead title={t('cluster.title')} parent={{ href: '/settings', label: t('nav.settings') }}>
	<p class="muted">{t('cluster.intro')}</p>
</PageHead>

{#if loadError}
	<div class="notice-error" role="alert">
		{loadError} <button class="btn" onclick={() => load()}>{t('ui.retry')}</button>
	</div>
{:else if !page || !cluster}
	<p class="muted loading">{t('common.loading')}</p>
{:else if !inCluster}
	<section class="tile panel">
		<h2 class="tile-title">{t('cluster.create')}</h2>
		<p class="muted">{t('cluster.createHint')}</p>
		{#if owner}
			<form class="row-actions" onsubmit={create}>
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
		{:else}
			<p class="notice-warn">{t('cluster.ownerOnly')}</p>
		{/if}
	</section>
	<section class="tile panel">
		<h2 class="tile-title">{t('cluster.join')}</h2>
		<p class="muted">{t('cluster.joinHint')}</p>
		{#if owner}
			<form onsubmit={check}>
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
			</form>
			{#if preview}
				<div class="preview divided">
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
		{:else}
			<p class="notice-warn">{t('cluster.ownerOnly')}</p>
		{/if}
	</section>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
{:else}
	<section class="tile nodes-tile" id="cluster-nodes" aria-labelledby="cluster-nodes-title">
		<div class="nodes-head">
			<h2 id="cluster-nodes-title">{t('cluster.nodesOf', { name: cluster.name ?? '' })}</h2>
			{#if manages}
				<button type="button" class="btn small" onclick={() => (probing = true)}
					><Icon name="refresh" size={15} />{t('cluster.probe')}</button
				>
				<button type="button" class="btn small" onclick={() => (updating = true)}
					><Icon name="download" size={15} />{t('cluster.updateAll')}</button
				>
			{/if}
		</div>
		<ul class="nodes">
			{#each page.nodes as node (node.id)}
				{@const route = cluster.routes[node.id]}
				<li>
					<span class="dot {tone(node)}" aria-hidden="true"></span>
					<div class="info">
						<div class="node-head">
							<span class="name">{node.name}</span>
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
							{#if !node.online}<span class="chip error">{t('cluster.offline')}</span>{/if}
							{#if node.status && otherVersion(node, version)}<span class="chip warn"
									>{t(older(node) ? 'cluster.olderVersion' : 'cluster.newerVersion')}</span
								>{/if}
							{#if route?.manage}<span class="chip info"
									>{t('cluster.ownWay', { way: wayLabel(route.manage) })}</span
								>{/if}
							{#if node.status?.errors || node.status?.warnings}
								{#if node.self || (manages && manageable(node))}
									<button
										type="button"
										class="chip-button"
										aria-expanded={noticesOf === node.id}
										aria-controls="notices-{node.id}"
										title={t('cluster.noticesShow')}
										onclick={() => (noticesOf = noticesOf === node.id ? '' : node.id)}
									>
										{#if node.status.errors}<span class="chip error"
												>{t('cluster.errors', { count: node.status.errors })}</span
											>{/if}
										{#if node.status.warnings}<span class="chip warn"
												>{t('cluster.warnings', { count: node.status.warnings })}</span
											>{/if}
										<span class="fold" class:open={noticesOf === node.id}
											><Icon name="chevronDown" size={14} /></span
										>
									</button>
								{:else}
									{#if node.status.errors}<span class="chip error"
											>{t('cluster.errors', { count: node.status.errors })}</span
										>{/if}
									{#if node.status.warnings}<span class="chip warn"
											>{t('cluster.warnings', { count: node.status.warnings })}</span
										>{/if}
								{/if}
							{/if}
						</div>
						<p class="line">
							{#if node.self}
								<span class="mono small muted">{node.addresses.slice(0, 4).join(' · ')}</span>
							{:else if node.online && node.way}
								<span
									>{t('cluster.via', { way: wayLabel(node.way) }) +
										(node.millis ? ` · ${node.millis} ms` : '')}</span
								>
								<span class="mono small muted">{node.address}</span>
								{#if node.fallback}<span class="warn-text small"
										>{t((ways.return ?? 0) < 0 ? 'cluster.fallbackStays' : 'cluster.fallback', {
											way: wayLabel(node.preferred),
										})}</span
									>{/if}
							{:else if node.error}
								<span class="muted">{errorText(node.error)}</span>
								<span class="mono small muted">{node.addresses.slice(0, 3).join(' · ')}</span>
							{/if}
						</p>
						{#if node.status}
							<p class="line small muted">
								<span
									>{t('cluster.version', { version: node.status.version }) +
										(older(node) ? ' ' : '')}{#if older(node)}<span class="warn-text"
											>→ {version}</span
										>{/if}</span
								>
								<span
									>· {t('cluster.uptime', {
										time: formatDuration(node.status.uptimeSeconds),
									})}</span
								>
								{#if node.status.cpuPercent !== null}<span
										>· CPU {Math.round(node.status.cpuPercent)} %</span
									>{/if}
								{#if used(node)}<span>· {used(node)}</span>{/if}
								<span
									>· {t('cluster.volumes', {
										count: node.status.volumes.length,
									})}{#if volumeProblems(node)}
										<span class="warn-text"
											>({t('cluster.volumeProblems', { count: volumeProblems(node) })})</span
										>{/if}</span
								>
								{#if node.clockSkew}<span class="warn-text"
										>· {t(node.clockSkew > 0 ? 'cluster.clockAhead' : 'cluster.clockBehind', {
											minutes: Math.max(1, Math.round(Math.abs(node.clockSkew) / 60)),
										})}</span
									>{/if}
							</p>
						{/if}
						{#if noticesOf === node.id}
							<div id="notices-{node.id}">
								<NodeNotices node={node.id} self={node.self} name={node.name} onchange={load} />
							</div>
						{/if}
					</div>
					<div class="actions">
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
						{#if manages && (!node.self || (owner && !isAdmin && adminNode && !adminNode.online))}
							<Dropdown label={t('ui.more')} icon="more" align="right">
								{#snippet children(close)}
									{#if !node.self}
										<button
											type="button"
											role="menuitem"
											onclick={() => {
												close();
												routing = node;
											}}
											><Icon name="globe" size={16} /><span>{t('cluster.routeMenu')}</span></button
										>
									{/if}
									{#if owner && isAdmin && !node.self}
										<button
											type="button"
											role="menuitem"
											disabled={busy}
											onclick={() => {
												close();
												setManage(node, !node.manage);
											}}
											><Icon name="shield" size={16} /><span
												>{t(node.manage ? 'cluster.manageRevoke' : 'cluster.manageAllow')}</span
											></button
										>
										<button
											type="button"
											role="menuitem"
											onclick={() => {
												close();
												handing = node;
											}}
											><Icon name="users" size={16} /><span
												>{t('cluster.handOver', { name: node.name })}</span
											></button
										>
									{/if}
									{#if owner && !isAdmin && node.id === cluster.admin && !node.online}
										<button
											type="button"
											role="menuitem"
											onclick={() => {
												close();
												handing = page?.nodes.find((n) => n.self) ?? null;
											}}><Icon name="users" size={16} /><span>{t('cluster.takeOver')}</span></button
										>
									{/if}
									{#if owner && node.id !== cluster.admin && !node.self}
										<button
											type="button"
											role="menuitem"
											class="danger"
											onclick={() => {
												close();
												removing = node;
											}}><Icon name="trash" size={16} /><span>{t('cluster.remove')}</span></button
										>
									{/if}
								{/snippet}
							</Dropdown>
						{/if}
					</div>
				</li>
			{/each}
		</ul>
		<p class="muted small trust">{t(manages ? 'cluster.trust' : 'cluster.memberHint')}</p>
	</section>

	<form class="tile settings-form" onsubmit={saveSettings}>
		<FormSection title={t('cluster.settings')} />
		<FormRow label={t('cluster.name')} id="cluster-name">
			<div class="line">
				<input
					id="cluster-name"
					class="field"
					type="text"
					bind:value={clusterName}
					maxlength="63"
					disabled={busy || !manages}
				/>
			</div>
		</FormRow>

		<FormSection title={t('cluster.connection')} />
		<FormRow label={t('cluster.manageWay')} id="cluster-way" hint={t('cluster.manageWayHint')}>
			<div class="line">
				<select id="cluster-way" class="field" bind:value={ways.manage} disabled={busy || !manages}>
					<option value="">{t('cluster.wayAuto')}</option>
					{#each WAYS as way (way)}<option value={way}
							>{t('cluster.wayFirst', { way: wayLabel(way) })}</option
						>{/each}
				</select>
			</div>
		</FormRow>
		<FormRow label={t('cluster.onlyWay')}>
			<Toggle
				checked={!!ways.only && !!ways.manage}
				onchange={(on) => (ways.only = on)}
				label={t('cluster.onlyWay')}
				disabled={busy || !manages || !ways.manage}
				text={t(ways.only && ways.manage ? 'cluster.onlyOn' : 'cluster.onlyOff')}
			/>
		</FormRow>
		<FormRow label={t('cluster.returnWay')} id="cluster-return" hint={t('cluster.returnWayHint')}>
			<div class="line">
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
			</div>
		</FormRow>
		<FormRow label={t('cluster.backupWay')} id="cluster-backup" hint={t('cluster.backupWayHint')}>
			<div class="line">
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
			</div>
		</FormRow>

		<FormSection title={t('cluster.waysTitle')} />
		{#each FOUND_WAYS as way (way)}
			{@const found = foundOf(way)}
			<FormRow
				label={wayLabel(way)}
				hint={way === 'tailscale' && found.some((a) => a.includes('.ts.net'))
					? t('cluster.tailscaleNameHint')
					: ''}
			>
				<p class="line found">
					<span class="chip" class:ok={found.length > 0}
						>{t(found.length ? 'cluster.wayFound' : 'cluster.wayNotFound')}</span
					>
					{#if found.length}<span class="mono small">{found.join(' · ')}</span>{/if}
				</p>
			</FormRow>
		{/each}
		<FormRow
			label={t('cluster.tunnel')}
			hint={cluster.tunnel.error
				? cluster.tunnel.error
				: t('cluster.tunnelHint', { port: cluster.tunnel.port ?? 51821 })}
			tone={cluster.tunnel.error ? 'error' : ''}
		>
			<Toggle
				bind:checked={tunnel}
				label={t('cluster.tunnel')}
				disabled={busy || !manages}
				text={tunnel && cluster.tunnel.enabled && cluster.tunnel.address
					? t('cluster.tunnelOn', { address: cluster.tunnel.address })
					: undefined}
			/>
		</FormRow>
		<FormRow
			label={t('cluster.internetWay')}
			hint={internet ? t('cluster.internetWarn') : t('cluster.internetHint')}
			tone={internet ? 'warn' : ''}
		>
			<Toggle
				bind:checked={internet}
				label={t('cluster.internetWay')}
				disabled={busy || !manages}
			/>
		</FormRow>
		<FormRow label={t('cluster.extra')} id="cluster-extra" hint={t('cluster.extraHint')}>
			<textarea
				id="cluster-extra"
				class="field mono extra"
				rows="2"
				bind:value={extra}
				spellcheck="false"
				placeholder="nas.example.org"
				disabled={busy}></textarea>
		</FormRow>

		<FormSection title={t('cluster.notices')} />
		<FormRow label={t('cluster.alert')} id="cluster-alert" hint={t('cluster.alertHint')}>
			<div class="line">
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
			</div>
		</FormRow>
		<FormRow label={t('cluster.clock')} hint={t('cluster.clockHint')}>
			<Toggle
				checked={!ways.clockOff}
				onchange={(on) => (ways.clockOff = !on)}
				label={t('cluster.clock')}
				disabled={busy || !manages}
			/>
		</FormRow>
		{#if error}<p class="notice-error form-error" role="alert">{error}</p>{/if}
		<div class="form-foot">
			<div>
				<button class="btn btn-primary" disabled={busy || !clusterName.trim()}
					>{t('common.save')}</button
				>
				{#if busy || saved}<span class="muted" role="status"
						>{t(busy ? 'ui.saving' : 'ui.saved')}</span
					>{/if}
			</div>
		</div>
	</form>

	{#if manages}<section class="tile panel" id="cluster-join" aria-labelledby="cluster-join-title">
			<h2 class="tile-title" id="cluster-join-title">{t('cluster.joinInfo')}</h2>
			<p class="muted">{t('cluster.joinInfoHint')}</p>
			{#if joinInfo}<textarea
					class="field mono join-info"
					rows="4"
					readonly
					value={joinInfo}
					aria-label={t('cluster.joinInfo')}
					onfocus={(e) => e.currentTarget.select()}></textarea>
				<p class="muted small">{t('cluster.joinInfoHides')}</p>{/if}
			<div class="row-actions">
				{#if joinInfo}<button class="btn btn-primary" onclick={copy}
						>{t(copied ? 'cluster.copied' : 'cluster.copy')}</button
					><button class="btn" onclick={hideJoinInfo}>{t('cluster.hideJoinInfo')}</button
					>{:else}<button class="btn btn-primary" disabled={busy} onclick={openReveal}
						>{t('cluster.showJoinInfo')}</button
					>{/if}
				<button class="btn" disabled={busy} onclick={() => (confirmKey = true)}
					>{t('cluster.newKey')}</button
				>
			</div>
		</section>{/if}

	{#if owner}
		<section class="tile panel">
			<h2 class="tile-title">{t('cluster.leave')}</h2>
			<p class="muted">{t('cluster.leaveHint')}</p>
			<div class="row-actions">
				<button class="btn btn-danger" disabled={busy} onclick={() => (confirmLeave = true)}
					>{t('cluster.leaveButton')}</button
				>
			</div>
		</section>
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
		align-items: center;
		gap: 4px;
		padding: 0;
		border: 0;
		background: none;
		color: inherit;
		font: inherit;
		cursor: pointer;
	}
	.fold {
		display: inline-flex;
		transition: transform 0.15s;
	}
	.fold.open {
		transform: rotate(180deg);
	}
	.chip-button:focus-visible {
		outline: 2px solid var(--focus, var(--accent));
		outline-offset: 2px;
		border-radius: 6px;
	}
	.nodes-tile {
		padding: 0;
	}
	.nodes-head {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px 10px;
		padding: var(--space-3) var(--space-4) 6px;
	}
	.nodes-head h2 {
		margin: 0 auto 0 0;
		font-size: var(--text-lg);
		font-weight: 600;
	}
	.nodes {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.nodes li {
		display: grid;
		grid-template-columns: 12px minmax(0, 1fr) auto;
		gap: 4px 14px;
		align-items: start;
		padding: 12px var(--space-4);
		border-top: 1px solid var(--border);
	}
	.nodes li:first-child {
		border-top: 0;
	}
	.dot {
		width: 10px;
		height: 10px;
		margin-top: 6px;
		border-radius: 50%;
		background: var(--status-ok);
	}
	.dot.warn {
		background: var(--status-warn);
	}
	.dot.off {
		background: var(--status-inactive);
	}
	.info {
		display: flex;
		flex-direction: column;
		gap: 3px;
		min-width: 0;
	}
	.node-head {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 6px 8px;
	}
	.name {
		font-weight: 600;
	}
	.line {
		display: flex;
		flex-wrap: wrap;
		align-items: baseline;
		gap: 2px 6px;
		font-size: var(--text-md);
	}
	.line.small,
	.small {
		font-size: var(--text-sm);
	}
	.actions {
		display: flex;
		flex-wrap: wrap;
		justify-content: flex-end;
		gap: 6px;
	}
	.trust {
		padding: 10px var(--space-4) var(--space-3);
		border-top: 1px solid var(--border);
	}
	.settings-form {
		padding: 0 0 var(--space-2);
	}
	.found {
		padding-top: 0.45rem;
	}
	.extra {
		max-width: 34rem;
	}
	.form-error {
		margin: 8px var(--space-4);
	}
	.form-foot {
		display: grid;
		grid-template-columns: minmax(10rem, 17rem) minmax(0, 1fr);
		gap: 0 var(--space-4);
		margin-top: 8px;
		padding: 14px var(--space-4) 8px;
		border-top: 1px solid var(--border);
	}
	.form-foot > div {
		grid-column: 2;
		display: flex;
		align-items: center;
		gap: 10px;
	}
	.reveal {
		display: flex;
		flex-direction: column;
		gap: 12px;
	}
	.join-info {
		width: 100%;
		word-break: break-all;
	}
	.preview {
		margin-top: 12px;
	}
	.warn-text {
		color: var(--warn-text);
	}
	p {
		margin: 0;
	}
	@media (max-width: 760px) {
		.nodes li {
			grid-template-columns: 12px minmax(0, 1fr);
			padding: 12px var(--space-3);
		}
		.actions {
			grid-column: 2;
			justify-content: flex-start;
		}
		.nodes-head {
			padding-inline: var(--space-3);
		}
		.form-foot {
			grid-template-columns: minmax(0, 1fr);
			padding-inline: var(--space-3);
		}
		.form-foot > div {
			grid-column: 1;
		}
	}
</style>
