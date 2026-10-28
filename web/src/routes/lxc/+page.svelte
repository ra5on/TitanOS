<script lang="ts">
	// System containers (LXC): the list on the left, the chosen container on
	// the right with its actions, settings and snapshots.
	import Dialog from '#lib/components/Dialog.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { api, ApiError } from '#lib/api.ts';
	import { formatBytes, formatDateTime } from '#lib/format.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { containerLabel, type LXCList, type LXCSnapshot, type LXCStatus } from '#lib/lxc.ts';
	import { addLXCConsole } from '#lib/terminal.svelte.ts';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { onMount } from 'svelte';
	import LxcForm from './LxcForm.svelte';

	let list = $state<LXCList | null>(null);
	let loadError = $state('');
	let actionError = $state('');
	let busy = $state('');
	let form = $state<{ existing?: LXCStatus } | null>(null);
	let removing = $state<LXCStatus | null>(null);
	let deleteData = $state(false);
	// A simple confirmation that names the action and its target.
	let confirming = $state<{ title: string; text: string; label: string; run: () => void } | null>(
		null,
	);
	let snapshots = $state<LXCSnapshot[] | null>(null);
	let snapshotFor = '';
	let config = $state('');

	const selectedName = $derived(page.url.searchParams.get('ct') ?? '');
	const selected = $derived(
		list?.containers.find((c) => c.name === selectedName) ?? list?.containers[0] ?? null,
	);
	const readOnly = $derived(!!list?.dev || !list?.installed);

	async function load() {
		if (document.hidden) return;
		try {
			list = await api.get<LXCList>('/lxc');
			loadError = '';
		} catch (err) {
			loadError = errorText(err instanceof ApiError ? err.code : 'internal');
		}
	}

	onMount(() => {
		load();
		const timer = setInterval(load, 5000);
		return () => clearInterval(timer);
	});

	// Snapshots are read from the volume only when a Btrfs container is opened.
	$effect(() => {
		const c = selected;
		if (!c || !c.btrfs || c.job) {
			snapshots = null;
			snapshotFor = '';
			return;
		}
		if (snapshotFor !== c.name) {
			snapshotFor = c.name;
			config = '';
			loadSnapshots(c.name);
		}
	});

	async function loadSnapshots(name: string) {
		try {
			const res = await api.get<{ snapshots: LXCSnapshot[] }>(
				`/lxc/${encodeURIComponent(name)}/snapshots`,
			);
			if (snapshotFor === name) snapshots = res.snapshots;
		} catch (err) {
			actionError = errorText(err instanceof ApiError ? err.code : 'internal');
		}
	}

	function problem(err: unknown): string {
		const code = err instanceof ApiError ? err.code : 'internal';
		return code === 'lxc_failed' && err instanceof ApiError ? err.message : errorText(code);
	}

	async function run(name: string, work: () => Promise<unknown>) {
		busy = name;
		actionError = '';
		try {
			await work();
		} catch (err) {
			actionError = problem(err);
		} finally {
			busy = '';
			await load();
		}
	}

	const action = (c: LXCStatus, what: 'start' | 'stop' | 'restart' | 'kill') =>
		run(c.name, () => api.post(`/lxc/${encodeURIComponent(c.name)}/${what}`));

	function choose(c: LXCStatus) {
		void goto(`/lxc?${new URLSearchParams({ ct: c.name })}`, { replaceState: true });
	}

	async function remove() {
		const c = removing;
		if (!c) return;
		removing = null;
		// The API wants the name once more for deleting the data; the click on the button is the confirmation.
		await run(c.name, () =>
			api.delete(
				`/lxc/${encodeURIComponent(c.name)}`,
				deleteData ? { deleteData: true, confirm: c.name } : undefined,
			),
		);
		deleteData = false;
	}

	async function showConfig(c: LXCStatus) {
		try {
			config = (await api.get<{ config: string }>(`/lxc/${encodeURIComponent(c.name)}/config`))
				.config;
		} catch (err) {
			actionError = problem(err);
		}
	}

	async function snapshotAction(c: LXCStatus, work: () => Promise<unknown>) {
		await run(c.name, work);
		await loadSnapshots(c.name);
	}

	function networkText(c: LXCStatus): string {
		if (c.network === 'bridge') return t('lxc.net.bridge');
		if (c.network.startsWith('network:'))
			return list?.networks?.find((n) => `network:${n.id}` === c.network)?.name ?? c.network;
		return t('lxc.net.none');
	}

	function stateText(c: LXCStatus): string {
		if (c.job) return t(c.job.state === 'failed' ? 'lxc.state.failed' : 'lxc.state.creating');
		if (!c.available) return t('lxc.unavailable');
		return t(c.state === 'running' ? 'lxc.state.running' : 'lxc.state.stopped');
	}
</script>

<PageHead title={t('lxc.title')}>
	<p class="muted">{t('lxc.intro')}</p>
	{#snippet actions()}
		<button class="btn btn-primary" disabled={!list || readOnly} onclick={() => (form = {})}
			>{t('lxc.create')}</button
		>
	{/snippet}
</PageHead>

{#if list && !list.installed}<p class="notice-warn" role="status">{t('lxc.notInstalled')}</p>{/if}
{#if list?.dev}<p class="notice-warn" role="status">{t('lxc.dev')}</p>{/if}
{#if loadError}<p class="notice-error" role="alert">
		{loadError} <button class="btn" onclick={load}>{t('common.retry')}</button>
	</p>{/if}
{#if actionError}<p class="notice-error" role="alert">{actionError}</p>{/if}

{#if !list}
	{#if !loadError}<p class="muted loading" role="status">{t('common.loading')}</p>{/if}
{:else if list.containers.length === 0}
	<EmptyState title={t('lxc.empty')} text={t('lxc.emptyText')} />
{:else}
	<div class="layout">
		<ul class="tile list" aria-label={t('lxc.title')}>
			{#each list.containers as c (c.name)}
				<li>
					<button
						type="button"
						class:current={selected?.name === c.name}
						aria-current={selected?.name === c.name}
						onclick={() => choose(c)}
					>
						<span
							class="dot"
							class:ok={c.state === 'running'}
							class:warn={!!c.job && c.job.state === 'running'}
							class:error={c.job?.state === 'failed'}
							aria-hidden="true"
						></span>
						<span class="name">{containerLabel(c)}</span>
						<small class="muted">{stateText(c)}</small>
					</button>
				</li>
			{/each}
		</ul>

		{#if selected}
			{@const c = selected}
			<section class="tile detail" aria-label={containerLabel(c)}>
				<header>
					<div>
						<h2>{containerLabel(c)}</h2>
						<p class="muted mono">{c.name} · {c.distribution} {c.release}</p>
					</div>
					<span
						class="chip"
						class:ok={c.state === 'running'}
						class:error={c.job?.state === 'failed'}>{stateText(c)}</span
					>
				</header>

				{#if c.job}
					{#if c.job.state === 'running'}
						<p class="notice-ok" role="status">
							{t('lxc.creatingText', { phase: t(`lxc.phase.${c.job.phase}`) })}
						</p>
					{:else}
						<div class="notice-error" role="alert">
							<p>{t('lxc.failedHint')}</p>
							{#if c.job.text}<pre class="mono">{c.job.text}</pre>{/if}
						</div>
					{/if}
				{/if}
				{#if c.privileged}<p class="notice-warn">{t('lxc.form.privilegedWarning')}</p>{/if}

				<div class="actions">
					{#if c.state === 'running'}
						<button
							class="btn"
							disabled={readOnly || busy === c.name}
							onclick={() => addLXCConsole(c.name, containerLabel(c))}>{t('lxc.shell')}</button
						>
						<button
							class="btn"
							disabled={readOnly || busy === c.name}
							onclick={() => addLXCConsole(c.name, containerLabel(c), 'console')}
							>{t('lxc.console')}</button
						>
						<button
							class="btn"
							disabled={readOnly || busy === c.name}
							onclick={() => action(c, 'stop')}>{t('lxc.stop')}</button
						>
						<button
							class="btn"
							disabled={readOnly || busy === c.name}
							onclick={() => action(c, 'restart')}>{t('lxc.restart')}</button
						>
						<button
							class="btn"
							disabled={readOnly || busy === c.name}
							onclick={() =>
								(confirming = {
									title: t('lxc.killTitle'),
									text: t('lxc.killText', { name: containerLabel(c) }),
									label: t('lxc.kill'),
									run: () => action(c, 'kill'),
								})}>{t('lxc.kill')}</button
						>
					{:else}
						<button
							class="btn btn-primary"
							disabled={readOnly || busy === c.name || !!c.job || !c.available}
							onclick={() => action(c, 'start')}>{t('lxc.start')}</button
						>
					{/if}
					<button
						class="btn"
						disabled={busy === c.name || c.job?.state === 'running'}
						onclick={() => (form = { existing: c })}>{t('lxc.edit')}</button
					>
					<button
						class="btn"
						disabled={readOnly ||
							busy === c.name ||
							c.state === 'running' ||
							c.job?.state === 'running'}
						onclick={() => ((deleteData = false), (removing = c))}>{t('lxc.remove')}</button
					>
				</div>

				<dl>
					<dt>{t('lxc.detail.mode')}</dt>
					<dd>{t(c.privileged ? 'lxc.privileged' : 'lxc.unprivileged')}</dd>
					<dt>{t('lxc.detail.path')}</dt>
					<dd class="mono">/mnt/{c.volume}/lxc/{c.name}</dd>
					<dt>{t('lxc.detail.network')}</dt>
					<dd>
						{networkText(c)}{#if c.network !== 'none'}
							· <span class="mono">{c.mac}</span>{/if}
					</dd>
					<dt>{t('lxc.detail.limits')}</dt>
					<dd>
						{c.cpus ? t('lxc.cpus', { count: c.cpus }) : t('lxc.unlimited')} · {c.memoryMiB
							? formatBytes(c.memoryMiB * 1048576)
							: t('lxc.unlimited')}
					</dd>
					{#if c.state === 'running'}
						<dt>{t('lxc.detail.usage')}</dt>
						<dd>{Math.round(c.cpuPercent)} % · {formatBytes(c.memoryBytes)}</dd>
					{/if}
					<dt>{t('lxc.autostart')}</dt>
					<dd>{t(c.autostart ? 'lxc.on' : 'lxc.off')}</dd>
					<dt>{t('lxc.detail.created')}</dt>
					<dd>{formatDateTime(new Date(c.created))}</dd>
					{#if c.description}
						<dt>{t('lxc.form.description')}</dt>
						<dd>{c.description}</dd>
					{/if}
					<dt>{t('lxc.detail.mounts')}</dt>
					<dd>
						{#each c.mounts ?? [] as mount (mount.target)}
							<span class="mono block"
								>{mount.path} → {mount.target} ({t(
									mount.readOnly ? 'lxc.readOnly' : 'lxc.readWrite',
								)})</span
							>
						{:else}—{/each}
					</dd>
				</dl>

				<section class="snapshots" aria-label={t('lxc.snap.title')}>
					<h3>{t('lxc.snap.title')}</h3>
					{#if !c.btrfs}
						<p class="muted">{t('lxc.snap.notBtrfs')}</p>
					{:else if snapshots}
						<div>
							<button
								class="btn"
								disabled={readOnly || busy === c.name || !!c.job}
								onclick={() =>
									snapshotAction(c, () => api.post(`/lxc/${encodeURIComponent(c.name)}/snapshots`))}
								>{t('lxc.snap.create')}</button
							>
						</div>
						{#each snapshots as snapshot (snapshot.id)}
							<div class="snapshot">
								<span>{formatDateTime(new Date(snapshot.created))}</span>
								<button
									class="btn"
									disabled={readOnly || busy === c.name || c.state === 'running'}
									title={c.state === 'running' ? t('lxc.snap.stoppedOnly') : undefined}
									onclick={() =>
										(confirming = {
											title: t('lxc.snap.restoreTitle'),
											text: t('lxc.snap.restoreText', {
												name: containerLabel(c),
												time: formatDateTime(new Date(snapshot.created)),
											}),
											label: t('lxc.snap.restore'),
											run: () =>
												snapshotAction(c, () =>
													api.post(
														`/lxc/${encodeURIComponent(c.name)}/snapshots/${snapshot.id}/restore`,
													),
												),
										})}>{t('lxc.snap.restore')}</button
								>
								<button
									class="btn"
									disabled={readOnly || busy === c.name}
									onclick={() =>
										(confirming = {
											title: t('lxc.snap.deleteTitle'),
											text: t('lxc.snap.deleteText', {
												time: formatDateTime(new Date(snapshot.created)),
											}),
											label: t('lxc.snap.delete'),
											run: () =>
												snapshotAction(c, () =>
													api.delete(`/lxc/${encodeURIComponent(c.name)}/snapshots/${snapshot.id}`),
												),
										})}>{t('lxc.snap.delete')}</button
								>
							</div>
						{:else}
							<p class="muted">{t('lxc.snap.empty')}</p>
						{/each}
					{/if}
				</section>

				<details
					ontoggle={(event) => (event.currentTarget as HTMLDetailsElement).open && showConfig(c)}
				>
					<summary>{t('lxc.showConfig')}</summary>
					{#if config}<pre class="mono config">{config}</pre>{/if}
				</details>
			</section>
		{/if}
	</div>
{/if}

{#if form && list}
	<LxcForm
		{list}
		existing={form.existing}
		onclose={() => (form = null)}
		ondone={(name) => {
			form = null;
			void load().then(() =>
				goto(`/lxc?${new URLSearchParams({ ct: name })}`, { replaceState: true }),
			);
		}}
	/>
{/if}

{#if removing}
	{@const c = removing}
	<Dialog title={t('lxc.delete.title')} onclose={() => (removing = null)}>
		<p>
			{t('lxc.delete.text', { name: containerLabel(c), path: `/mnt/${c.volume}/lxc/${c.name}` })}
		</p>
		<label class="check"
			><input type="checkbox" bind:checked={deleteData} />{t('lxc.delete.data')}</label
		>
		{#if deleteData}<p class="notice-warn" role="alert">
				{t('lxc.delete.dataWarning', { name: containerLabel(c) })}
			</p>{/if}
		<div class="dialog-actions">
			<button class="btn" onclick={() => (removing = null)}>{t('common.cancel')}</button>
			<button class="btn btn-danger" onclick={remove}>{t('lxc.delete.confirm')}</button>
		</div>
	</Dialog>
{/if}

{#if confirming}
	{@const request = confirming}
	<Dialog title={request.title} onclose={() => (confirming = null)}>
		<p>{request.text}</p>
		<div class="dialog-actions">
			<button class="btn" onclick={() => (confirming = null)}>{t('common.cancel')}</button>
			<button
				class="btn btn-danger"
				onclick={() => {
					confirming = null;
					request.run();
				}}>{request.label}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.layout {
		display: grid;
		grid-template-columns: minmax(200px, 280px) 1fr;
		gap: 16px;
		align-items: start;
	}
	@media (max-width: 760px) {
		.layout {
			grid-template-columns: 1fr;
		}
	}
	.list {
		display: flex;
		flex-direction: column;
		gap: 2px;
		margin: 0;
		padding: 6px;
		list-style: none;
	}
	.list button {
		display: grid;
		grid-template-columns: auto 1fr;
		align-items: center;
		column-gap: 10px;
		width: 100%;
		min-height: 44px;
		padding: 6px 10px;
		background: none;
		border: 0;
		border-radius: var(--radius-ctl);
		color: var(--text);
		font: inherit;
		text-align: left;
		cursor: pointer;
	}
	.list button small {
		grid-column: 2;
		font-size: 0.75rem;
	}
	.list button.current,
	.list button:hover {
		background: var(--chip);
	}
	.name {
		overflow-wrap: anywhere;
	}
	.detail {
		display: flex;
		flex-direction: column;
		gap: 14px;
		padding: var(--tile-pad);
		min-width: 0;
	}
	header {
		display: flex;
		justify-content: space-between;
		align-items: flex-start;
		gap: 12px;
	}
	h2 {
		margin: 0;
		font-size: 1.125rem;
		overflow-wrap: anywhere;
	}
	h3 {
		margin: 0;
		font-size: 0.9375rem;
	}
	header p {
		margin: 2px 0 0;
		font-size: 0.8125rem;
	}
	.actions {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
	}
	dl {
		display: grid;
		grid-template-columns: max-content 1fr;
		gap: 6px 16px;
		margin: 0;
		font-size: 0.875rem;
	}
	dt {
		color: var(--muted);
	}
	dd {
		margin: 0;
		min-width: 0;
		overflow-wrap: anywhere;
	}
	.block {
		display: block;
	}
	.snapshots {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
	.snapshot {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px;
	}
	.snapshot span {
		flex: 1;
		font-size: 0.875rem;
	}
	pre {
		margin: 6px 0 0;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
		font-size: 0.75rem;
	}
	.config {
		padding: 10px;
		background: var(--bg);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
	}
	summary {
		cursor: pointer;
		font-size: 0.875rem;
	}
	label.check {
		display: flex;
		align-items: center;
		gap: 8px;
		font-size: 0.875rem;
	}
	p {
		margin: 0;
	}
</style>
