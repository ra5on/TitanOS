<script lang="ts">
	import UsageBreakdown from '#lib/operations/UsageBreakdown.svelte';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { api, type StorageStatus } from '#lib/api.ts';
	import { session } from '#lib/session.svelte.ts';
	import { t, tOr } from '#lib/i18n/index.svelte.ts';
	import PageHead from '#lib/components/PageHead.svelte';
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
		restored = $state('');
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
		const timer = setInterval(() => {
			if (!document.hidden && mode === 'grow' && !busy) loadGrowth();
		}, 5000);
		return () => clearInterval(timer);
	});
	async function load() {
		busy = true;
		error = '';
		try {
			storage = await api.get('/storage');
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
			growth = await api.get('/storage/growth');
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
			usage = await api.get(`${base()}/usage`);
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
			preview = await api.post(`${base()}/growth/preview`, growthRequest());
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
		snapshots = volume ? await api.get(`${base()}/snapshots`) : [];
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
		parent={{ href: '/storage', label: t('nav.storage') }}
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
	<label class="volume"
		>{t('ops.volume')}<select class="field" bind:value={volume} disabled={busy} onchange={reset}
			>{#each storage?.volumes ?? [] as v (v.name)}<option value={v.name}
					>{v.name} · {t(`level.${v.level}`)} · {t(`fs.${v.filesystem}`)}</option
				>{/each}</select
		></label
	>
	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if mode === 'usage'}<p class="muted">{t('ops.usageHint')}</p>
		<button class="btn btn-primary" disabled={busy || !volume} onclick={analyze}
			>{t(busy ? 'ops.loading' : 'ops.analyze')}</button
		>
		{#if usage}<section class="tile results">
				<UsageBreakdown entries={usage.entries} />{#if usage.incomplete}<p class="notice-warn">
						{t('ops.incomplete')}
					</p>{/if}
				<div class="table-wrap">
					<table>
						<thead
							><tr
								><th>{t('ops.name')}</th><th>{t('ops.category')}</th><th>{t('ops.logical')}</th><th
									>{t('ops.allocated')}</th
								><th>{t('ops.files')}</th></tr
							></thead
						><tbody
							>{#each usage.entries as entry (entry.id ?? entry.name)}<tr
									><td class="mono"
										>{entry.name}{#if entry.error}<p class="notice-warn">{entry.error}</p>{/if}</td
									><td>{t(`ops.usage.${entry.kind ?? 'other'}` as 'ops.usage.other')}</td><td
										>{formatBytes(entry.bytes)}</td
									><td>{formatBytes(entry.allocated)}</td><td>{entry.files}</td></tr
								>{/each}</tbody
						>
					</table>
				</div>
			</section>{/if}
	{:else if mode === 'grow'}<p class="muted">{t('ops.growHint')}</p>
		{#if growth?.pending}<section class="tile results">
				<p class="notice-warn">{t('ops.growthPending')}</p>
				<p>
					<span class="mono">{growth.operation.volume}</span> · {phase(growth.operation.phase)}
				</p>
				<p>{growth.error || growth.operation.error}</p>
				<p>
					{t('ops.members', {
						before: growth.operation.originalDisks,
						after: growth.operation.targetDisks,
					})}
				</p>
				<button
					type="button"
					class="btn btn-warn"
					disabled={busy || growth.active}
					onclick={() => (growthAction = 'recover')}>{t('ops.resume')}</button
				>{#if ['preparing', 'adding', 'starting', 'aborting'].includes(growth.operation.phase) && !growth.active}<p
						class="muted"
					>
						{t('ops.cancelGrowthHint')}
					</p>
					<button
						type="button"
						class="btn btn-warn"
						disabled={busy}
						onclick={() => (growthAction = 'cancel')}>{t('ops.cancelGrowth')}</button
					>{/if}
			</section>
		{:else if growBlocked}<p class="notice-warn" role="status">{growBlocked}</p>
		{:else}<form
				class="tile results"
				onsubmit={(e) => {
					e.preventDefault();
					growPreview();
				}}
			>
				<label
					>{t('ops.disk')}<select class="field" bind:value={disk} required
						><option value="">{t('ops.choose')}</option>{#each candidates as d (d.name)}<option
								value={d.name}>{d.model} · {d.serial} · {formatBytes(d.sizeBytes)}</option
							>{/each}</select
					></label
				><label
					>{t('ops.recoveryVolume')}<select class="field" bind:value={recoveryVolume} required
						><option value="">{t('ops.choose')}</option
						>{#each storage?.volumes.filter((v) => v.name !== volume && v.mounted && v.state === 'ok') ?? [] as v (v.name)}<option
								value={v.name}>{v.name}</option
							>{/each}</select
					></label
				><button
					class="btn btn-primary"
					disabled={busy || !selectedVolume || !['raid5', 'raid6'].includes(selectedVolume.level)}
					>{t('ops.preview')}</button
				>
			</form>{/if}
	{:else}<p class="muted">{t('ops.snapshotHint')}</p>
		{#if selectedVolume?.filesystem === 'btrfs'}<div class="tile results">
				<label
					>{t('snapshot.title')}<select
						class="field"
						bind:value={snapshot}
						disabled={busy}
						onchange={pickSnapshot}
						><option value="">{t('ops.choose')}</option>{#each snapshots as s (s.name)}<option
								value={s.name}>{s.name}</option
							>{/each}</select
					></label
				>{#if snapshot}<div class="toolbar">
						<button
							class="btn"
							disabled={busy || !folder}
							onclick={() => browse(folder.split('/').slice(0, -1).join('/'))}>{t('ops.up')}</button
						><span class="mono">/{folder}</span>
					</div>
					<div class="table-wrap">
						<table>
							<thead><tr><th>{t('ops.name')}</th><th>{t('ops.logical')}</th></tr></thead><tbody
								>{#each entries as entry (entry.path)}<tr
										><td
											>{#if entry.directory}<button
													class="btn"
													disabled={busy}
													onclick={() => browse(entry.path)}
													>{entry.path.split('/').at(-1)} /</button
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
					{#if next}<button class="btn" disabled={busy} onclick={() => browse(folder, next)}
							>{t('ops.more')}</button
						>{/if}
					{#if files.length}<div class="toolbar">
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
					<form
						onsubmit={(e) => {
							e.preventDefault();
							previewRestore();
						}}
					>
						<label
							>{t('ops.target')}<select class="field" bind:value={restoreVolume} required
								><option value="">{t('ops.choose')}</option
								>{#each storage?.volumes.filter((v) => v.mounted) ?? [] as v (v.name)}<option
										value={v.name}>{v.name}</option
									>{/each}</select
							></label
						><button
							class="btn btn-primary"
							disabled={busy || !selected.length || selected.length > 200}
							>{t('ops.restoreSelected')} ({selected.length})</button
						>
					</form>{/if}
			</div>{/if}
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
	.tabs,
	.toolbar {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
		margin-bottom: 16px;
	}
	.volume {
		max-width: 500px;
		margin-bottom: 20px;
	}
	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: 0.85rem;
		min-width: 0;
	}
	.results {
		padding: var(--tile-pad);
		margin-top: var(--gap);
		display: flex;
		flex-direction: column;
		gap: 16px;
	}
	.results form,
	form.results {
		display: flex;
		flex-wrap: wrap;
		align-items: end;
		gap: 16px;
	}
	.results form label,
	form.results label {
		flex: 1 1 220px;
	}
	.check {
		flex-direction: row;
		align-items: center;
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
		font-size: 0.85rem;
	}
	th,
	td {
		text-align: left;
		padding: 10px;
		border-bottom: 1px solid var(--border);
		vertical-align: top;
	}
	p,
	.mono {
		overflow-wrap: anywhere;
	}
	.toolbar {
		align-items: center;
		margin: 0;
	}
</style>
