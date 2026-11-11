<script lang="ts">
	import { poll } from '#lib/poll.ts';
	import UsageBreakdown from '#lib/operations/UsageBreakdown.svelte';
	import SnapshotFolderRestore from '#lib/operations/SnapshotFolderRestore.svelte';
	import SnapshotRestoreJobs from '#lib/operations/SnapshotRestoreJobs.svelte';
	import type { FolderRestoreJob } from '#lib/snapshot-restore.ts';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { api, type StorageStatus } from '#lib/api.ts';
	import { session } from '#lib/session.svelte.ts';
	import { t, tOr } from '#lib/i18n/index.svelte.ts';
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import { formatBytes } from '#lib/format.ts';
	import { operationError, type Growth, type GrowthStatus } from '#lib/operations.ts';
	type Usage = {
		at: string;
		entries: {
			id?: string;
			kind?: string;
			name: string;
			bytes: number;
			allocated: number;
			files: number;
			incomplete: boolean;
			error: string;
		}[];
		incomplete: boolean;
		scanned: number;
	};
	type SnapshotEntry = { path: string; directory: boolean; bytes: number; modified: string };
	const admin = $derived(['owner', 'admin'].includes(session.data?.user?.role ?? ''));
	let storage = $state<StorageStatus | null>(null),
		volume = $state(''),
		mode = $state('usage'),
		busy = $state(false),
		error = $state(''),
		usage = $state<Usage | null>(null),
		growth = $state<GrowthStatus | null>(null),
		preview = $state<Growth | null>(null),
		disk = $state(''),
		recoveryVolume = $state(''),
		erase = $state(false),
		growthAction = $state<'recover' | 'cancel' | null>(null);
	let snapshots = $state<{ name: string }[]>([]),
		snapshot = $state(''),
		folder = $state(''),
		entries = $state<SnapshotEntry[]>([]),
		next = $state(0),
		selected = $state<string[]>([]),
		restoreVolume = $state(''),
		restored = $state(''),
		// The snapshot folder whose restore dialog is open.
		folderRestore = $state(''),
		restoreJobs = $state<FolderRestoreJob[]>([]);
	let jobList = $state<{ load: () => Promise<void> } | undefined>();
	let restorePreview = $state<{
		uuid: string;
		files: number;
		bytes: number;
		paths: string[];
		targetVolume: string;
	} | null>(null);
	const selectedVolume = $derived(storage?.volumes.find((v) => v.name === volume));
	// Why the chosen volume cannot take another disk, or '' when it can.
	const growBlocked = $derived.by(() => {
		const v = selectedVolume;
		if (!v) return '';
		if (v.filesystem === 'btrfs') return t('ops.growNoBtrfs');
		if (!['raid5', 'raid6'].includes(v.level))
			return t('ops.growNoLevel', { level: t(`level.${v.level}`) });
		if (v.state !== 'ok') return t('ops.growNotHealthy', { state: t(`state.${v.state}`) });
		return '';
	});
	const files = $derived(entries.filter((entry) => !entry.directory).map((entry) => entry.path));
	const phase = (value: string) => tOr(`ops.growthPhase.${value}`, value);
	// The tab and volume live in the address, so reloading or sharing keeps them.
	function remember() {
		const url = new URL(page.url.href);
		url.searchParams.set('mode', mode);
		if (volume) url.searchParams.set('volume', volume);
		// Only the address changes; the page, its scroll position and focus stay.
		void goto(url, { replace: true, shallow: true });
	}
	const candidates = $derived(
		storage?.disks.filter(
			(d) =>
				!['stick', 'member'].includes(d.role) && !d.mounted && !d.testing && d.identity && d.serial,
		) ?? [],
	);
	onMount(() => {
		if (!admin) return;
		volume = page.url.searchParams.get('volume') ?? '';
		mode = page.url.searchParams.get('mode') ?? 'usage';
		load();
		return poll(loadGrowth, {
			interval: 5000,
			immediate: false,
			skip: () => mode !== 'grow' || busy,
		});
	});
	async function load() {
		busy = true;
		error = '';
		try {
			storage = await api.get<StorageStatus>('/storage');
			if (!storage!.volumes.some((v) => v.name === volume))
				volume = storage!.volumes[0]?.name ?? '';
			await loadGrowth();
			if (mode === 'snapshots') await loadSnapshots();
		} catch (e) {
			error = operationError(e, t);
		} finally {
			busy = false;
		}
	}
	async function loadGrowth() {
		try {
			growth = await api.get<GrowthStatus>('/storage/growth');
		} catch (e) {
			error = operationError(e, t);
		}
	}
	function reset() {
		usage = null;
		preview = null;
		entries = [];
		snapshot = '';
		snapshots = [];
		folder = '';
		selected = [];
		error = '';
		disk = '';
		erase = false;
		growthAction = null;
		restored = '';
		restorePreview = null;
		folderRestore = '';
		remember();
		if (mode === 'snapshots') run(loadSnapshots);
	}
	async function run(work: () => Promise<unknown>) {
		busy = true;
		error = '';
		try {
			await work();
		} catch (e) {
			error = operationError(e, t);
		} finally {
			busy = false;
		}
	}
	const base = () => `/storage/volumes/${encodeURIComponent(volume)}`;
	async function analyze() {
		await run(async () => {
			usage = await api.get<Usage>(`${base()}/usage`);
		});
	}
	// The API still wants the volume name as confirmation; the page sends it
	// once the danger button in the dialog was clicked.
	function growthRequest(confirmed = false) {
		const chosen = candidates.find((d) => d.name === disk);
		return {
			disk,
			identity: chosen?.identity ?? '',
			recoveryVolume,
			erase,
			confirm: confirmed ? volume : '',
		};
	}
	async function growPreview() {
		await run(async () => {
			preview = await api.post<Growth>(`${base()}/growth/preview`, growthRequest());
			erase = false;
		});
	}
	async function grow() {
		if (!erase || busy) return;
		await run(async () => {
			await api.post(`${base()}/growth`, growthRequest(true));
			preview = null;
			await loadGrowth();
		});
	}
	async function cancelGrowth() {
		const confirm = growth?.operation.volume ?? '';
		await run(async () => {
			await api.post('/storage/growth/cancel', { confirm });
			growthAction = null;
			await loadGrowth();
		});
	}
	async function recover() {
		const confirm = growth?.operation.volume ?? '';
		await run(async () => {
			await api.post('/storage/growth/recover', { confirm });
			growthAction = null;
			await loadGrowth();
		});
	}
	async function loadSnapshots() {
		snapshots = volume ? await api.get<{ name: string }[]>(`${base()}/snapshots`) : [];
	}
	// Another snapshot starts empty: nothing listed or picked in the old one
	// may be sent to the new one.
	function pickSnapshot() {
		entries = [];
		folder = '';
		next = 0;
		selected = [];
		restorePreview = null;
		browse('');
	}
	async function browse(path = '', offset = 0) {
		if (!snapshot) return;
		const shown = snapshot,
			from = volume;
		await run(async () => {
			const result = await api.get<{ entries: SnapshotEntry[]; next: number }>(
				`${base()}/snapshots/${encodeURIComponent(shown)}/files?` +
					new URLSearchParams({ path, offset: String(offset) }),
			);
			if (shown !== snapshot || from !== volume) return;
			entries = offset ? [...entries, ...result.entries] : result.entries;
			folder = path;
			next = result.next;
			if (!offset) selected = [];
		});
	}
	async function previewRestore() {
		const shown = snapshot;
		await run(async () => {
			const selection = { paths: [...selected], targetVolume: restoreVolume };
			const info = await api.post<{ uuid: string; files: number; bytes: number }>(
				`${base()}/snapshots/${encodeURIComponent(shown)}/files/preview`,
				selection,
			);
			if (shown !== snapshot) return;
			restorePreview = { ...selection, ...info };
		});
	}
	async function restore() {
		if (!restorePreview) return;
		const request = restorePreview;
		await run(async () => {
			const result = await api.post<{ folder: string; files: number; bytes: number }>(
				`${base()}/snapshots/${encodeURIComponent(snapshot)}/files/restore`,
				request,
			);
			restorePreview = null;
			restored = `${restoreVolume}/${result.folder}`;
			selected = [];
		});
	}
</script>

{#if admin}<PageHead
		title={t(mode === 'grow' ? 'ops.grow' : mode === 'snapshots' ? 'snapshot.title' : 'ops.usage')}
		>{#snippet actions()}<a class="btn" href="/tools/maintenance">{t('ops.maintenance')}</a><button
				class="btn"
				disabled={busy}
				onclick={load}>{t('ops.refresh')}</button
			>{/snippet}</PageHead
	>
	<nav class="tabs">
		{#each ['usage', 'grow', 'snapshots'] as tab (tab)}<button
				class="btn"
				class:btn-primary={mode === tab}
				onclick={() => {
					mode = tab;
					reset();
				}}
				>{t(
					tab === 'grow' ? 'ops.grow' : tab === 'snapshots' ? 'snapshot.title' : 'ops.usage',
				)}</button
			>{/each}
	</nav>
	<!-- PLAN 78-5: the choices of a tab are flat rows, results stand under a
	     plain heading; tables keep their own surface. -->
	<div class="groups">
		<SettingGroup>
			<SettingRow icon="disk" title={t('ops.volume')} for="manage-volume">
				{#snippet control()}<select
						id="manage-volume"
						class="field"
						bind:value={volume}
						disabled={busy}
						onchange={reset}
						>{#each storage?.volumes ?? [] as v (v.name)}<option value={v.name}
								>{v.name} · {t(`level.${v.level}`)} · {t(`fs.${v.filesystem}`)}</option
							>{/each}</select
					>{/snippet}
			</SettingRow>
			{#if mode === 'usage'}<SettingRow
					icon="search"
					title={t('ops.usage')}
					desc={t('ops.usageHint')}
				>
					{#snippet control()}<button
							class="btn btn-primary"
							disabled={busy || !volume}
							onclick={analyze}>{t(busy ? 'ops.loading' : 'ops.analyze')}</button
						>{/snippet}
				</SettingRow>{/if}
		</SettingGroup>
		{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
		{#if mode === 'usage'}
			{#if usage}<SettingGroup title={t('ops.usageResult')} id="usage-result">
					<UsageBreakdown entries={usage.entries} />{#if usage.incomplete}<p class="notice-warn">
							{t('ops.incomplete')}
						</p>{/if}
					<div class="tile table-wrap">
						<table>
							<thead
								><tr
									><th>{t('ops.name')}</th><th>{t('ops.category')}</th><th>{t('ops.logical')}</th
									><th>{t('ops.allocated')}</th><th>{t('ops.files')}</th></tr
								></thead
							><tbody
								>{#each usage.entries as entry (entry.id ?? entry.name)}<tr
										><td class="mono"
											>{entry.name}{#if entry.error}<p class="notice-warn">
													{entry.error}
												</p>{/if}</td
										><td>{t(`ops.usage.${entry.kind ?? 'other'}` as 'ops.usage.other')}</td><td
											>{formatBytes(entry.bytes)}</td
										><td>{formatBytes(entry.allocated)}</td><td>{entry.files}</td></tr
									>{/each}</tbody
							>
						</table>
					</div>
				</SettingGroup>{/if}
		{:else if mode === 'grow'}<p class="muted">{t('ops.growHint')}</p>
			{#if growth?.pending}<SettingGroup title={t('ops.growthOpen')} id="growth-pending">
					<p class="notice-warn">{t('ops.growthPending')}</p>
					<SettingRow
						icon="disk"
						title={growth.operation.volume}
						desc="{phase(growth.operation.phase)} · {t('ops.members', {
							before: growth.operation.originalDisks,
							after: growth.operation.targetDisks,
						})}"
					>
						{#snippet control()}<button
								type="button"
								class="btn btn-warn"
								disabled={busy || growth!.active}
								onclick={() => (growthAction = 'recover')}>{t('ops.resume')}</button
							>{/snippet}
					</SettingRow>
					{#if growth.error || growth.operation.error}<p class="notice-warn">
							{growth.error || growth.operation.error}
						</p>{/if}
					{#if ['preparing', 'adding', 'starting', 'aborting'].includes(growth.operation.phase) && !growth.active}<SettingRow
							icon="stop"
							title={t('ops.cancelGrowth')}
							desc={t('ops.cancelGrowthHint')}
						>
							{#snippet control()}<button
									type="button"
									class="btn btn-warn"
									disabled={busy}
									onclick={() => (growthAction = 'cancel')}>{t('ops.cancelGrowth')}</button
								>{/snippet}
						</SettingRow>{/if}
				</SettingGroup>
			{:else if growBlocked}<p class="notice-warn" role="status">{growBlocked}</p>
			{:else}<form
					onsubmit={(e) => {
						e.preventDefault();
						growPreview();
					}}
				>
					<SettingGroup title={t('ops.grow')} id="growth-form">
						<SettingRow icon="disk" title={t('ops.disk')} for="grow-disk">
							{#snippet control()}<select id="grow-disk" class="field" bind:value={disk} required
									><option value="">{t('ops.choose')}</option
									>{#each candidates as d (d.name)}<option value={d.name}
											>{d.model} · {d.serial} · {formatBytes(d.sizeBytes)}</option
										>{/each}</select
								>{/snippet}
						</SettingRow>
						<SettingRow icon="shield" title={t('ops.recoveryVolume')} for="grow-recovery">
							{#snippet control()}<select
									id="grow-recovery"
									class="field"
									bind:value={recoveryVolume}
									required
									><option value="">{t('ops.choose')}</option
									>{#each storage?.volumes.filter((v) => v.name !== volume && v.mounted && v.state === 'ok') ?? [] as v (v.name)}<option
											value={v.name}>{v.name}</option
										>{/each}</select
								>{/snippet}
						</SettingRow>
						<div class="row-actions">
							<button
								class="btn btn-primary"
								disabled={busy ||
									!selectedVolume ||
									!['raid5', 'raid6'].includes(selectedVolume.level)}>{t('ops.preview')}</button
							>
						</div>
					</SettingGroup>
				</form>{/if}
		{:else}<p class="muted">{t('ops.snapshotHint')}</p>
			{#if selectedVolume?.filesystem === 'btrfs'}<SettingGroup>
					<SettingRow icon="camera" title={t('snapshot.title')} for="manage-snapshot">
						{#snippet control()}<select
								id="manage-snapshot"
								class="field"
								bind:value={snapshot}
								disabled={busy}
								onchange={pickSnapshot}
								><option value="">{t('ops.choose')}</option>{#each snapshots as s (s.name)}<option
										value={s.name}>{s.name}</option
									>{/each}</select
							>{/snippet}
					</SettingRow>
				</SettingGroup>
				{#if snapshot}<SettingGroup title={t('ops.snapshotContent')} id="snapshot-files">
						{#snippet tools()}<button
								class="btn"
								disabled={busy || !folder}
								onclick={() => browse(folder.split('/').slice(0, -1).join('/'))}
								>{t('ops.up')}</button
							>{#if folder}<button
									class="btn"
									disabled={busy}
									onclick={() => (folderRestore = folder)}>{t('snapshotRestore.thisFolder')}</button
								>{/if}{/snippet}
						<p class="mono where">/{folder}</p>
						<div class="tile table-wrap">
							<table>
								<thead><tr><th>{t('ops.name')}</th><th>{t('ops.logical')}</th></tr></thead><tbody
									>{#each entries as entry (entry.path)}<tr
											><td
												>{#if entry.directory}<span class="folder"
														><button class="btn" disabled={busy} onclick={() => browse(entry.path)}
															>{entry.path.split('/').at(-1)} /</button
														><button
															class="btn"
															disabled={busy}
															aria-label={t('snapshotRestore.folderLabel', {
																name: entry.path.split('/').at(-1) ?? '',
															})}
															onclick={() => (folderRestore = entry.path)}
															>{t('snapshotRestore.restore')}</button
														></span
													>{:else}<label class="check"
														><input
															type="checkbox"
															checked={selected.includes(entry.path)}
															disabled={busy}
															onchange={(e) => {
																selected = e.currentTarget.checked
																	? [...selected, entry.path]
																	: selected.filter((p) => p !== entry.path);
															}}
														/><span class="mono">{entry.path.split('/').at(-1)}</span></label
													>{/if}</td
											><td>{entry.directory ? '—' : formatBytes(entry.bytes)}</td></tr
										>{/each}</tbody
								>
							</table>
						</div>
						{#if next}<div class="row-actions">
								<button class="btn" disabled={busy} onclick={() => browse(folder, next)}
									>{t('ops.more')}</button
								>
							</div>{/if}
						{#if files.length}<div class="row-actions">
								<button
									class="btn"
									disabled={busy || files.every((f) => selected.includes(f))}
									onclick={() => (selected = [...new Set([...selected, ...files])])}
									>{t('ops.selectFolderFiles', { count: files.length })}</button
								>{#if selected.length}<button
										class="btn"
										disabled={busy}
										onclick={() => (selected = [])}>{t('ops.selectNone')}</button
									>{/if}
							</div>
							<p class="muted">{t('ops.foldersHint')}</p>
							{#if selected.length > 200}<p class="notice-warn" role="alert">
									{t('ops.selectTooMany', { count: selected.length })}
								</p>{/if}{/if}
					</SettingGroup>
					<form
						onsubmit={(e) => {
							e.preventDefault();
							previewRestore();
						}}
					>
						<SettingGroup title={t('ops.restoreSelected')} id="snapshot-restore">
							<SettingRow icon="folder" title={t('ops.target')} for="restore-target">
								{#snippet control()}<select
										id="restore-target"
										class="field"
										bind:value={restoreVolume}
										required
										><option value="">{t('ops.choose')}</option
										>{#each storage?.volumes.filter((v) => v.mounted) ?? [] as v (v.name)}<option
												value={v.name}>{v.name}</option
											>{/each}</select
									>{/snippet}
							</SettingRow>
							<div class="row-actions">
								<button
									class="btn btn-primary"
									disabled={busy || !selected.length || selected.length > 200}
									>{t('ops.restoreSelected')} ({selected.length})</button
								>
							</div>
						</SettingGroup>
					</form>{/if}{/if}
			{#if selectedVolume?.filesystem === 'btrfs'}<SnapshotRestoreJobs
					bind:this={jobList}
					bind:jobs={restoreJobs}
				/>{/if}
			{#if restored}<p class="notice" role="status">
					{t('ops.restored', { path: restored })}
					<a
						href={'/files?' +
							new URLSearchParams({
								root: `volume/${restored.split('/')[0]}`,
								path: restored.split('/').slice(1).join('/'),
							})}>{t('nav.files')} →</a
					>
				</p>{/if}
		{/if}
	</div>
	{#if preview}<Dialog title={t('ops.grow')} locked={busy} onclose={() => (preview = null)}
			><p>{t('ops.growHint')}</p>
			<p>{t('ops.members', { before: preview.originalDisks, after: preview.targetDisks })}</p>
			{#if preview.currentBytes && preview.plannedBytes}<p>
					{t('tasks.capacityBefore')}: <strong>{formatBytes(preview.currentBytes)}</strong>
				</p>
				<p>{t('tasks.capacityAfter')}: <strong>{formatBytes(preview.plannedBytes)}</strong></p>
				<p class="muted">{t('tasks.capacityEstimate')}</p>{/if}
			<p class="mono">{preview.volume} · {preview.diskSerial} · {preview.recoveryVolume}</p>
			<p class="notice-warn">
				{t('ops.growEraseWarning', { disk: preview.diskSerial, volume: preview.volume })}
			</p>
			<label class="check"
				><input type="checkbox" bind:checked={erase} />{t('ops.eraseConsent')}</label
			>
			{#if error}<p class="notice-error">{error}</p>{/if}
			<div class="dialog-actions">
				<button class="btn" disabled={busy} onclick={() => (preview = null)}
					>{t('common.cancel')}</button
				><button type="button" class="btn btn-warn" disabled={busy || !erase} onclick={grow}
					>{t('ops.grow')}</button
				>
			</div></Dialog
		>{/if}
	{#if growthAction && growth?.pending}<Dialog
			title={t(growthAction === 'recover' ? 'ops.resume' : 'ops.cancelGrowth')}
			locked={busy}
			onclose={() => (growthAction = null)}
		>
			<p class="notice-warn">
				{t(growthAction === 'recover' ? 'ops.resumeConfirm' : 'ops.cancelGrowthConfirm', {
					volume: growth.operation.volume,
				})}
			</p>
			{#if error}<p class="notice-error">{error}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" disabled={busy} onclick={() => (growthAction = null)}
					>{t('common.cancel')}</button
				><button
					type="button"
					class="btn btn-warn"
					disabled={busy || growth.active}
					onclick={growthAction === 'recover' ? recover : cancelGrowth}
					>{t(growthAction === 'recover' ? 'ops.resume' : 'ops.cancelGrowth')}</button
				>
			</div>
		</Dialog>{/if}
	{#if folderRestore && snapshot}<SnapshotFolderRestore
			{volume}
			{snapshot}
			path={folderRestore}
			onclose={() => (folderRestore = '')}
			onstarted={(job) => {
				folderRestore = '';
				restoreJobs = [job, ...restoreJobs.filter((j) => j.id !== job.id)];
				jobList?.load();
			}}
		/>{/if}
	{#if restorePreview}<Dialog
			title={t('tasks.restorePreview')}
			locked={busy}
			onclose={() => (restorePreview = null)}
		>
			<p class="mono">{volume} / {snapshot}</p>
			<p>
				{t('tasks.restoreAmount', {
					count: restorePreview.files,
					size: formatBytes(restorePreview.bytes),
				})}
			</p>
			<ul>
				{#each restorePreview.paths as path (path)}<li class="mono">{path}</li>{/each}
			</ul>
			<p>{t('tasks.restoreDestination', { volume: restorePreview.targetVolume })}</p>
			<p class="muted">{t('tasks.restorePreserves')}</p>
			{#if error}<p class="notice-error">{error}</p>{/if}
			<div class="dialog-actions">
				<button class="btn" disabled={busy} onclick={() => (restorePreview = null)}
					>{t('common.cancel')}</button
				><button class="btn btn-primary" disabled={busy} onclick={restore}
					>{t('ops.restoreSelected')}</button
				>
			</div>
		</Dialog>{/if}
{:else}<p class="notice-error">{t('error.forbidden')}</p>{/if}

<style>
	.tabs {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
		margin-bottom: var(--space-4);
	}
	/* The groups of a tab stand apart like the groups of a settings page. */
	.groups {
		display: flex;
		flex-direction: column;
		gap: var(--gap);
		min-width: 0;
	}
	.groups > p {
		margin: 0;
	}
	.row-actions {
		margin-top: var(--space-2);
	}
	.where {
		margin: 0 0 var(--space-1);
	}
	.check {
		display: flex;
		align-items: center;
		gap: var(--space-1-5);
		font-size: var(--text-base);
		min-width: 0;
	}
	.folder {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
	}
	.check span {
		overflow-wrap: anywhere;
	}
	.table-wrap {
		overflow-x: auto;
	}
	table {
		border-collapse: collapse;
		width: 100%;
		font-size: var(--text-base);
	}
	th,
	td {
		text-align: left;
		padding: var(--space-2-5);
		border-bottom: 1px solid var(--border);
		vertical-align: top;
	}
	p,
	.mono {
		overflow-wrap: anywhere;
	}
</style>
