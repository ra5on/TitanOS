<script lang="ts">
	// Settings › Cluster (backend package cluster): several TitanOS that trust
	// each other fully, as in Proxmox. Accounts are the same everywhere, every
	// node can be managed from here, backups go from node to node.
	import { poll } from '#lib/poll.ts';
	import { onMount } from 'svelte';
	import { api, ApiError } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { formatBytes, formatDuration } from '#lib/format.ts';
	import { apiErrorText, errorText, t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { switchNode } from '#lib/cluster-node.ts';
	import {
		manageable,
		type ClusterPage,
		type JoinPreview,
		type NodeView,
	} from '#lib/cluster.svelte.ts';

	let page = $state<ClusterPage | null>(null);
	let loadError = $state('');
	let busy = $state(false);
	let error = $state('');
	let copied = $state(false);

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
	let saved = $state(false);

	const owner = $derived(session.data?.user?.role === 'owner');
	const cluster = $derived(page?.cluster);
	const inCluster = $derived(!!cluster?.name);
	const version = $derived(page?.nodes.find((n) => n.self)?.status?.version ?? '');

	function problem(err: unknown): string {
		if (err instanceof ApiError && err.code === 'cluster_version')
			return t('cluster.versionMismatch', { version: err.message.replace(/^.*version /, '') });
		return apiErrorText(err);
	}

	function fill(next: ClusterPage) {
		page = next;
		clusterName = next.cluster.name ?? '';
		internet = next.cluster.internet;
		extra = next.cluster.extra.join('\n');
	}

	async function load() {
		loadError = '';
		try {
			fill(await api.get<ClusterPage>('/cluster'));
		} catch (err) {
			loadError = problem(err);
		}
	}
	onMount(() => poll(load, { interval: 20000, skip: () => busy || !inCluster }));

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
		if (await act(() => api.post<ClusterPage>('/cluster/leave'))) confirmLeave = false;
	}

	async function newKey() {
		if (await act(() => api.post<ClusterPage>('/cluster/join-key'))) confirmKey = false;
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
				api.put<ClusterPage>('/cluster', { name: clusterName.trim(), internet, extra: lines }),
			)
		)
			saved = true;
	}

	async function copy() {
		if (!cluster?.joinInfo) return;
		try {
			await navigator.clipboard.writeText(cluster.joinInfo);
			copied = true;
			setTimeout(() => (copied = false), 2000);
		} catch {
			// The field stays selectable to copy by hand.
		}
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
					{#if preview.added.length}<p>
							{t('cluster.previewAdded', { names: preview.added.join(', ') })}
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
	<section class="tile panel" id="cluster-nodes" aria-labelledby="cluster-nodes-title">
		<h2 class="tile-title" id="cluster-nodes-title">
			{t('cluster.nodesOf', { name: cluster.name ?? '' })}
		</h2>
		<ul class="nodes divided">
			{#each page.nodes as node (node.id)}
				<li>
					<div class="node-head">
						<span class="name">{node.name}</span>
						{#if node.self}<span class="chip">{t('cluster.thisNode')}</span>{/if}
						<span class="chip" class:ok={node.online} class:error={!node.online}
							>{t(node.online ? 'cluster.online' : 'cluster.offline')}</span
						>
						{#if node.status && node.status.version !== version}<span class="chip warn"
								>{t('cluster.otherVersion', { version: node.status.version })}</span
							>{/if}
						{#if node.status?.errors}<span class="chip error"
								>{t('cluster.errors', { count: node.status.errors })}</span
							>{/if}
						{#if node.status?.warnings}<span class="chip warn"
								>{t('cluster.warnings', { count: node.status.warnings })}</span
							>{/if}
					</div>
					{#if node.status}
						<p class="muted small">
							{t('cluster.version', { version: node.status.version })}
							· {t('cluster.uptime', { time: formatDuration(node.status.uptimeSeconds) })}
							{#if node.status.cpuPercent !== null}· CPU {Math.round(node.status.cpuPercent)} %{/if}
							{#if used(node)}· {used(node)}{/if}
							· {t('cluster.volumes', {
								count: node.status.volumes.length,
							})}{#if volumeProblems(node)}
								<span class="warn-text"
									>({t('cluster.volumeProblems', { count: volumeProblems(node) })})</span
								>{/if}
						</p>
					{:else if node.error}
						<p class="muted small">{errorText(node.error)}</p>
					{/if}
					<p class="muted small mono">
						{(node.address ? [node.address] : node.addresses.slice(0, 3)).join(' · ')}
					</p>
					<div class="row-actions">
						{#if !node.self}
							<button
								class="btn small"
								disabled={!manageable(node, version)}
								onclick={() => switchNode(node.id)}>{t('cluster.manage')}</button
							>
							{#if owner}<button class="btn small" disabled={busy} onclick={() => (removing = node)}
									>{t('cluster.remove')}</button
								>{/if}
						{/if}
					</div>
				</li>
			{/each}
		</ul>
		<p class="muted small">{t('cluster.trust')}</p>
	</section>

	<section class="tile panel" id="cluster-join" aria-labelledby="cluster-join-title">
		<h2 class="tile-title" id="cluster-join-title">{t('cluster.joinInfo')}</h2>
		<p class="muted">{t('cluster.joinInfoHint')}</p>
		<textarea
			class="field mono join-info"
			rows="4"
			readonly
			value={cluster.joinInfo ?? ''}
			aria-label={t('cluster.joinInfo')}
			onfocus={(e) => e.currentTarget.select()}></textarea>
		<div class="row-actions">
			<button class="btn btn-primary" onclick={copy}
				>{t(copied ? 'cluster.copied' : 'cluster.copy')}</button
			>
			<button class="btn" disabled={busy} onclick={() => (confirmKey = true)}
				>{t('cluster.newKey')}</button
			>
		</div>
	</section>

	<form class="tile panel" onsubmit={saveSettings}>
		<h2 class="tile-title">{t('cluster.settings')}</h2>
		<label class="stack"
			><span>{t('cluster.name')}</span><input
				class="field"
				type="text"
				bind:value={clusterName}
				maxlength="63"
				disabled={busy}
			/></label
		>
		<div class="stack">
			<span>{t('cluster.found')}</span>
			<p class="mono small">
				{cluster.found.length ? cluster.found.join(' · ') : t('cluster.noneFound')}
			</p>
		</div>
		<label class="stack"
			><span>{t('cluster.extra')}</span><textarea
				class="field mono"
				rows="3"
				bind:value={extra}
				spellcheck="false"
				placeholder="nas.example.org"
				disabled={busy}></textarea><small class="muted">{t('cluster.extraHint')}</small></label
		>
		<label class="check"
			><input type="checkbox" bind:checked={internet} disabled={busy} /><span
				>{t('cluster.internet')}</span
			></label
		>
		{#if internet}<p class="notice-warn">{t('cluster.internetWarn')}</p>{/if}
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		<div class="row-actions">
			<button class="btn btn-primary" disabled={busy || !clusterName.trim()}
				>{t('common.save')}</button
			>
			{#if busy || saved}<span class="muted" role="status"
					>{t(busy ? 'ui.saving' : 'ui.saved')}</span
				>{/if}
		</div>
	</form>

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
	.nodes {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.nodes li {
		display: flex;
		flex-direction: column;
		gap: 4px;
		padding: 10px 0;
	}
	.node-head {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 6px 10px;
	}
	.name {
		font-weight: 600;
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
</style>
