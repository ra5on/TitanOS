<script lang="ts">
	// Storage › Swap a disk (docs/DISK-SWAP.md): step by step from picking the
	// disk to the finished rebuild. Uses the storage actions that exist
	// (add, remove) and the live copy of an mdadm disk (replace).
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { api, type Disk, type Volume } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import SettingGroup from '#lib/components/settings/SettingGroup.svelte';
	import SettingRow from '#lib/components/settings/SettingRow.svelte';
	import {
		needsSwap,
		newlyAttached,
		replacementDisks,
		swapCandidates,
		type SwapCandidate,
	} from '#lib/disk-swap.ts';
	import { formatDiskBytes, formatDuration } from '#lib/format.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { storage, refreshStorage } from '#lib/storage.svelte.ts';
	import { diskType } from '../labels.ts';

	let volumeName = $state(page.url.searchParams.get('volume') ?? '');
	let diskName = $state(page.url.searchParams.get('disk') ?? '');
	let step = $state<'pick' | 'prepare' | 'insert' | 'running' | 'cleanup' | 'done'>('pick');
	let chosenNew = $state('');
	let password = $state('');
	let confirming = $state(false);
	let busy = $state(false);
	let error = $state('');
	let identifyUntil = $state(0);
	let known = $state<string[] | null>(null);
	let now = $state(Date.now());

	// The shell already keeps the storage state current; this page only needs the clock.
	onMount(() => {
		const tick = setInterval(() => (now = Date.now()), 1000);
		return () => clearInterval(tick);
	});

	const status = $derived(storage.status);
	const volumes = $derived(status?.volumes ?? []);
	const volume = $derived(volumes.find((v) => v.name === volumeName));
	const candidates = $derived(volume && status ? swapCandidates(volume, status.disks) : []);
	const candidate = $derived(candidates.find((c) => c.disk === diskName));
	const oldDisk = $derived(status?.disks.find((d) => d.name === diskName));
	const smallestMember = $derived(
		volume && status
			? Math.min(
					...volume.members
						.map((m) => status.disks.find((d) => d.name === m.disk)?.sizeBytes ?? Infinity)
						.filter(Number.isFinite),
				)
			: 0,
	);
	const options = $derived(
		status
			? replacementDisks(
					status.disks,
					oldDisk,
					Number.isFinite(smallestMember) ? smallestMember : 0,
				)
			: [],
	);
	const fresh = $derived(known && status ? newlyAttached(known, options) : []);
	const needsPassword = $derived(!!volume?.encryption && volume.filesystem === 'btrfs');
	const syncing = $derived(
		!!volume?.sync || volume?.state === 'rebuilding' || volume?.state === 'syncing',
	);

	// Offer the volume that needs it most when none was named.
	$effect(() => {
		if (!volumeName && volumes.length) volumeName = (volumes.find(needsSwap) ?? volumes[0]).name;
	});
	$effect(() => {
		if (volume && !candidates.some((c) => c.disk === diskName))
			diskName = candidates.find((c) => c.mode !== 'impossible')?.disk ?? '';
	});
	// While running, move on once the rebuild or copy is over.
	$effect(() => {
		if (step === 'running' && volume && !syncing && volume.state === 'ok')
			step = candidate?.mode === 'live' || candidate?.mode === 'addRemove' ? 'cleanup' : 'done';
	});

	function fail(err: unknown) {
		error = apiErrorText(err, { detail: true });
	}

	async function act(action: () => Promise<unknown>, next?: typeof step) {
		busy = true;
		error = '';
		try {
			await action();
			await refreshStorage();
			if (next) step = next;
		} catch (err) {
			fail(err);
		} finally {
			busy = false;
		}
	}

	function begin() {
		error = '';
		step = candidate?.mode === 'rebuild' && candidate.present ? 'prepare' : 'insert';
		if (step === 'insert') known = (status?.disks ?? []).map((d) => d.identity ?? d.name);
	}

	const identify = (disk: string) =>
		act(async () => {
			await api.post(`/storage/disks/${disk}/identify`, { seconds: 30 });
			identifyUntil = Date.now() + 30_000;
		});

	// A failed disk leaves the volume first, then it can be pulled.
	const takeOut = () =>
		act(async () => {
			await api.delete(`/storage/volumes/${volumeName}/disks/${diskName}`);
			known = (status?.disks ?? [])
				.filter((d) => d.name !== diskName)
				.map((d) => d.identity ?? d.name);
		}, 'insert');

	function start() {
		const target = options.find((d) => d.name === chosenNew);
		if (!target || !candidate) return;
		confirming = false;
		void act(() => {
			if (candidate.mode === 'live')
				return api.post(`/storage/volumes/${volumeName}/replace`, {
					old: diskName,
					new: target.name,
					erase: true,
					identity: target.identity,
				});
			return api.post(`/storage/volumes/${volumeName}/disks`, {
				disk: target.name,
				erase: true,
				identity: target.identity,
				password: needsPassword ? password : undefined,
			});
		}, 'running');
	}

	const removeOld = () =>
		act(() => api.delete(`/storage/volumes/${volumeName}/disks/${diskName}`), 'done');

	function describe(c: SwapCandidate): string {
		const disk = status?.disks.find((d) => d.name === c.disk);
		const what = disk
			? `${diskType(disk)} · ${disk.model || '–'} · ${disk.serial || '–'} · ${formatDiskBytes(disk.sizeBytes)}`
			: t('swap.missingDisk');
		return what;
	}
</script>

<PageHead title={t('swap.title')}>
	<p class="muted">{t('swap.intro')}</p>
</PageHead>

{#if !status}
	<p class="muted loading">{t('common.loading')}</p>
{:else if !volumes.length}
	<p class="muted">{t('swap.noVolumes')}</p>
{:else}
	<ol class="steps" aria-label={t('swap.title')}>
		{#each ['pick', 'insert', 'running', 'done'] as s, i (s)}
			<li
				class:current={step === s ||
					(s === 'insert' && step === 'prepare') ||
					(s === 'running' && step === 'cleanup')}
			>
				{i + 1}. {t(`swap.step.${s}` as never)}
			</li>
		{/each}
	</ol>

	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}

	<!-- PLAN 78-5: every step is a plain heading with flat rows; a disk to
	     choose is a row with its radio button at the right. -->
	{#if step === 'pick'}
		<SettingGroup title={t('swap.pickTitle')} id="swap-pick">
			<SettingRow icon="disk" title={t('swap.volume')} for="swap-volume">
				{#snippet control()}<select id="swap-volume" class="field" bind:value={volumeName}
						>{#each volumes as v (v.name)}<option value={v.name}
								>{v.name} · {t(`state.${v.state}` as never)}</option
							>{/each}</select
					>{/snippet}
			</SettingRow>
			<div class="disks" role="radiogroup" aria-labelledby="swap-disk-label">
				<p class="label" id="swap-disk-label">{t('swap.disk')}</p>
				{#each candidates as c, i (c.disk)}
					<div class="choice" class:off={c.mode === 'impossible'}>
						<SettingRow title={c.disk || '–'} desc={describe(c)} for="swap-old-{i}">
							{#snippet control()}{#if c.mode === 'rebuild'}<span class="chip error"
										>{t(c.present ? 'swap.failed' : 'swap.missing')}</span
									>{:else if c.warn}<span class="chip warn">{t('swap.smartWarn')}</span>{/if}<input
									id="swap-old-{i}"
									type="radio"
									name="disk"
									value={c.disk}
									bind:group={diskName}
									disabled={c.mode === 'impossible'}
								/>{/snippet}
						</SettingRow>
					</div>
				{/each}
			</div>
			{#if candidate}<p class="muted">{t(`swap.mode.${candidate.mode}` as never)}</p>{/if}
			<div class="row-actions">
				<button
					class="btn btn-primary"
					disabled={!candidate || candidate.mode === 'impossible'}
					onclick={begin}>{t('swap.next')}</button
				>
			</div>
		</SettingGroup>
	{:else if step === 'prepare'}
		<SettingGroup title={t('swap.prepareTitle', { disk: diskName })} id="swap-prepare">
			<p>{t('swap.prepareText')}</p>
			<div class="row-actions">
				<button class="btn btn-danger" disabled={busy} onclick={takeOut}>{t('swap.takeOut')}</button
				>
				<button class="btn" onclick={() => (step = 'pick')}>{t('swap.back')}</button>
			</div>
		</SettingGroup>
	{:else if step === 'insert'}
		<SettingGroup title={t('swap.insertTitle')} id="swap-insert">
			{#if oldDisk}
				<SettingRow
					icon="search"
					title={diskName}
					desc={t(candidate?.mode === 'rebuild' ? 'swap.pullOld' : 'swap.keepOld', {
						disk: diskName,
						model: oldDisk.model || '–',
						serial: oldDisk.serial || '–',
					})}
				>
					{#snippet control()}<button
							class="btn"
							disabled={busy || identifyUntil > now}
							onclick={() => identify(diskName)}
							>{identifyUntil > now
								? t('swap.identifying', {
										seconds: String(Math.ceil((identifyUntil - now) / 1000)),
									})
								: t('swap.identify')}</button
						>{/snippet}
				</SettingRow>
			{/if}
			<p>{t('swap.insertText')}</p>
			{#if options.length}
				<div class="disks" role="radiogroup" aria-labelledby="swap-new-label">
					<p class="label" id="swap-new-label">{t('swap.newDisk')}</p>
					{#each options as d, i (d.name)}
						<div class="choice">
							<SettingRow
								title={d.name}
								desc="{diskType(d)} · {d.model || '–'} · {d.serial || '–'} · {formatDiskBytes(
									d.sizeBytes,
								)}"
								for="swap-new-{i}"
							>
								{#snippet control()}{#if fresh.some((f) => f.name === d.name)}<span class="chip ok"
											>{t('swap.justAttached')}</span
										>{/if}{#if d.role === 'used'}<span class="chip warn">{t('swap.hasData')}</span
										>{/if}<input
										id="swap-new-{i}"
										type="radio"
										name="new"
										value={d.name}
										bind:group={chosenNew}
									/>{/snippet}
							</SettingRow>
						</div>
					{/each}
				</div>
			{:else}
				<p class="muted" role="status">
					{t('swap.waiting', {
						size: formatDiskBytes(
							Math.max(
								oldDisk?.sizeBytes ?? 0,
								Number.isFinite(smallestMember) ? smallestMember : 0,
							),
						),
					})}
				</p>
			{/if}
			{#if needsPassword}<SettingRow icon="key" title={t('swap.password')} for="swap-password">
					{#snippet control()}<input
							id="swap-password"
							class="field"
							type="password"
							bind:value={password}
							autocomplete="off"
						/>{/snippet}
				</SettingRow>{/if}
			<div class="row-actions">
				<button
					class="btn btn-primary"
					disabled={busy || !chosenNew || (needsPassword && !password)}
					onclick={() => (confirming = true)}>{t('swap.start')}</button
				>
			</div>
		</SettingGroup>
	{:else if step === 'running'}
		<SettingGroup
			title={t(candidate?.mode === 'live' ? 'swap.copying' : 'swap.rebuilding')}
			id="swap-running"
		>
			{#if volume?.sync}
				<div
					class="bar"
					role="progressbar"
					aria-valuemin="0"
					aria-valuemax="100"
					aria-valuenow={Math.round(volume.sync.percent)}
				>
					<span style:width="{volume.sync.percent}%"></span>
				</div>
				<p class="muted">
					{Math.round(volume.sync.percent)} % · {t('swap.remaining', {
						time: formatDuration(volume.sync.remainingSeconds),
					})}
				</p>
			{:else}
				<p class="muted" role="status">{t('swap.starting')}</p>
			{/if}
			<p class="muted">{t('swap.runningHint')}</p>
		</SettingGroup>
	{:else if step === 'cleanup'}
		<SettingGroup title={t('swap.cleanupTitle')} id="swap-cleanup">
			<p>{t('swap.cleanupText', { disk: diskName })}</p>
			<div class="row-actions">
				<button class="btn btn-primary" disabled={busy} onclick={removeOld}
					>{t('swap.removeOld')}</button
				>
			</div>
		</SettingGroup>
	{:else}
		<SettingGroup title={t('swap.doneTitle')} id="swap-done">
			<p>{t('swap.doneText', { volume: volumeName })}</p>
			{#if oldDisk && !oldDisk.volume}
				<SettingRow
					icon="search"
					title={diskName}
					desc={t('swap.pullNow', { disk: diskName, serial: oldDisk.serial || '–' })}
				>
					{#snippet control()}<button
							class="btn"
							disabled={busy || identifyUntil > now}
							onclick={() => identify(diskName)}>{t('swap.identify')}</button
						>{/snippet}
				</SettingRow>
			{/if}
			<div class="row-actions"><a class="btn" href="/storage">{t('swap.toStorage')}</a></div>
		</SettingGroup>
	{/if}
{/if}

{#if confirming}
	<Dialog title={t('swap.confirmTitle')} onclose={() => (confirming = false)}>
		<p>{t('swap.confirmText', { disk: chosenNew, volume: volumeName })}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (confirming = false)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-danger" disabled={busy} onclick={start}
				>{t('swap.confirm')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.steps {
		display: flex;
		gap: var(--space-2) var(--space-4);
		flex-wrap: wrap;
		list-style: none;
		padding: 0;
		margin: 0 0 var(--space-3);
		color: var(--muted);
		font-size: var(--text-base);
	}
	.steps .current {
		color: var(--text);
		font-weight: 600;
	}
	.disks {
		display: flex;
		flex-direction: column;
		gap: 3px;
		min-width: 0;
	}
	.label {
		margin: var(--space-2) 0 var(--space-1);
		font-weight: 500;
	}
	.choice.off {
		opacity: 0.6;
	}
	.choice input {
		flex: none;
		width: 1.125rem;
		height: 1.125rem;
		accent-color: var(--accent);
	}
	.row-actions {
		margin-top: var(--space-2);
	}
	.bar {
		height: 10px;
		border-radius: var(--radius-bar);
		background: var(--track);
		overflow: hidden;
	}
	.bar span {
		display: block;
		height: 100%;
		background: var(--accent);
	}
</style>
