<script lang="ts">
	// Storage › Swap a disk (docs/DISK-SWAP.md): step by step from picking the
	// disk to the finished rebuild. Uses the storage actions that exist
	// (add, remove) and the live copy of an mdadm disk (replace).
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { api, ApiError, type Disk, type Volume } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { needsSwap, newlyAttached, replacementDisks, swapCandidates, type SwapCandidate } from '#lib/disk-swap.ts';
	import { formatDiskBytes, formatDuration } from '#lib/format.ts';
	import { errorText, t } from '#lib/i18n/index.svelte.ts';
	import { storage, watchStorage, refreshStorage } from '#lib/storage.svelte.ts';
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

	onMount(() => {
		const stop = watchStorage();
		const tick = setInterval(() => (now = Date.now()), 1000);
		return () => {
			stop();
			clearInterval(tick);
		};
	});

	const status = $derived(storage.status);
	const volumes = $derived(status?.volumes ?? []);
	const volume = $derived(volumes.find((v) => v.name === volumeName));
	const candidates = $derived(volume && status ? swapCandidates(volume, status.disks) : []);
	const candidate = $derived(candidates.find((c) => c.disk === diskName));
	const oldDisk = $derived(status?.disks.find((d) => d.name === diskName));
	const smallestMember = $derived(
		volume && status ? Math.min(...volume.members.map((m) => status.disks.find((d) => d.name === m.disk)?.sizeBytes ?? Infinity).filter(Number.isFinite)) : 0,
	);
	const options = $derived(status ? replacementDisks(status.disks, oldDisk, Number.isFinite(smallestMember) ? smallestMember : 0) : []);
	const fresh = $derived(known && status ? newlyAttached(known, options) : []);
	const needsPassword = $derived(!!volume?.encryption && volume.filesystem === 'btrfs');
	const syncing = $derived(!!volume?.sync || volume?.state === 'rebuilding' || volume?.state === 'syncing');

	// Offer the volume that needs it most when none was named.
	$effect(() => {
		if (!volumeName && volumes.length) volumeName = (volumes.find(needsSwap) ?? volumes[0]).name;
	});
	$effect(() => {
		if (volume && !candidates.some((c) => c.disk === diskName)) diskName = candidates.find((c) => c.mode !== 'impossible')?.disk ?? '';
	});
	// While running, move on once the rebuild or copy is over.
	$effect(() => {
		if (step === 'running' && volume && !syncing && volume.state === 'ok') step = candidate?.mode === 'live' || candidate?.mode === 'addRemove' ? 'cleanup' : 'done';
	});

	function fail(err: unknown) {
		error = err instanceof ApiError ? `${errorText(err.code)}${err.message ? ` ${err.message}` : ''}` : errorText('internal');
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
			known = (status?.disks ?? []).filter((d) => d.name !== diskName).map((d) => d.identity ?? d.name);
		}, 'insert');

	function start() {
		const target = options.find((d) => d.name === chosenNew);
		if (!target || !candidate) return;
		confirming = false;
		void act(() => {
			if (candidate.mode === 'live')
				return api.post(`/storage/volumes/${volumeName}/replace`, { old: diskName, new: target.name, erase: true, identity: target.identity });
			return api.post(`/storage/volumes/${volumeName}/disks`, { disk: target.name, erase: true, identity: target.identity, password: needsPassword ? password : undefined });
		}, 'running');
	}

	const removeOld = () => act(() => api.delete(`/storage/volumes/${volumeName}/disks/${diskName}`), 'done');

	function describe(c: SwapCandidate): string {
		const disk = status?.disks.find((d) => d.name === c.disk);
		const what = disk ? `${diskType(disk)} · ${disk.model || '–'} · ${disk.serial || '–'} · ${formatDiskBytes(disk.sizeBytes)}` : t('swap.missingDisk');
		return what;
	}
</script>

<PageHead title={t('swap.title')} parent={{ href: '/storage', label: t('nav.storage') }}>
	<p class="muted">{t('swap.intro')}</p>
</PageHead>

{#if !status}
	<p class="muted loading">{t('common.loading')}</p>
{:else if !volumes.length}
	<p class="muted">{t('swap.noVolumes')}</p>
{:else}
	<ol class="steps" aria-label={t('swap.title')}>
		{#each ['pick', 'insert', 'running', 'done'] as s, i (s)}
			<li class:current={step === s || (s === 'insert' && step === 'prepare') || (s === 'running' && step === 'cleanup')}>{i + 1}. {t(`swap.step.${s}` as never)}</li>
		{/each}
	</ol>

	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}

	{#if step === 'pick'}
		<section class="tile panel">
			<h2 class="tile-title">{t('swap.pickTitle')}</h2>
			<label class="stack"
				><span>{t('swap.volume')}</span><select class="field" bind:value={volumeName}
					>{#each volumes as v (v.name)}<option value={v.name}>{v.name} · {t(`state.${v.state}` as never)}</option>{/each}</select
				></label
			>
			<fieldset class="disks">
				<legend>{t('swap.disk')}</legend>
				{#each candidates as c (c.disk)}
					<label class="choice" class:off={c.mode === 'impossible'}>
						<input type="radio" name="disk" value={c.disk} bind:group={diskName} disabled={c.mode === 'impossible'} />
						<span class="mono">{c.disk || '–'}</span>
						<span class="what">{describe(c)}</span>
						{#if c.mode === 'rebuild'}<span class="chip error">{t(c.present ? 'swap.failed' : 'swap.missing')}</span>{:else if c.warn}<span class="chip warn">{t('swap.smartWarn')}</span>{/if}
					</label>
				{/each}
			</fieldset>
			{#if candidate}<p class="muted">{t(`swap.mode.${candidate.mode}` as never)}</p>{/if}
			<div class="row-actions"><button class="btn btn-primary" disabled={!candidate || candidate.mode === 'impossible'} onclick={begin}>{t('swap.next')}</button></div>
		</section>
	{:else if step === 'prepare'}
		<section class="tile panel">
			<h2 class="tile-title">{t('swap.prepareTitle', { disk: diskName })}</h2>
			<p>{t('swap.prepareText')}</p>
			<div class="row-actions">
				<button class="btn btn-danger" disabled={busy} onclick={takeOut}>{t('swap.takeOut')}</button>
				<button class="btn" onclick={() => (step = 'pick')}>{t('swap.back')}</button>
			</div>
		</section>
	{:else if step === 'insert'}
		<section class="tile panel">
			<h2 class="tile-title">{t('swap.insertTitle')}</h2>
			{#if oldDisk}
				<div class="find">
					<p>{t(candidate?.mode === 'rebuild' ? 'swap.pullOld' : 'swap.keepOld', { disk: diskName, model: oldDisk.model || '–', serial: oldDisk.serial || '–' })}</p>
					<button class="btn" disabled={busy || identifyUntil > now} onclick={() => identify(diskName)}
						>{identifyUntil > now ? t('swap.identifying', { seconds: String(Math.ceil((identifyUntil - now) / 1000)) }) : t('swap.identify')}</button
					>
				</div>
			{/if}
			<p>{t('swap.insertText')}</p>
			{#if options.length}
				<fieldset class="disks">
					<legend>{t('swap.newDisk')}</legend>
					{#each options as d (d.name)}
						<label class="choice">
							<input type="radio" name="new" value={d.name} bind:group={chosenNew} />
							<span class="mono">{d.name}</span>
							<span class="what">{diskType(d)} · {d.model || '–'} · {d.serial || '–'} · {formatDiskBytes(d.sizeBytes)}</span>
							{#if fresh.some((f) => f.name === d.name)}<span class="chip ok">{t('swap.justAttached')}</span>{/if}
							{#if d.role === 'used'}<span class="chip warn">{t('swap.hasData')}</span>{/if}
						</label>
					{/each}
				</fieldset>
			{:else}
				<p class="muted" role="status">{t('swap.waiting', { size: formatDiskBytes(Math.max(oldDisk?.sizeBytes ?? 0, Number.isFinite(smallestMember) ? smallestMember : 0)) })}</p>
			{/if}
			{#if needsPassword}<label class="stack"><span>{t('swap.password')}</span><input class="field" type="password" bind:value={password} autocomplete="off" /></label>{/if}
			<div class="row-actions">
				<button class="btn btn-primary" disabled={busy || !chosenNew || (needsPassword && !password)} onclick={() => (confirming = true)}>{t('swap.start')}</button>
			</div>
		</section>
	{:else if step === 'running'}
		<section class="tile panel">
			<h2 class="tile-title">{t(candidate?.mode === 'live' ? 'swap.copying' : 'swap.rebuilding')}</h2>
			{#if volume?.sync}
				<div class="bar" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-valuenow={Math.round(volume.sync.percent)}>
					<span style:width="{volume.sync.percent}%"></span>
				</div>
				<p class="muted">{Math.round(volume.sync.percent)} % · {t('swap.remaining', { time: formatDuration(volume.sync.remainingSeconds) })}</p>
			{:else}
				<p class="muted" role="status">{t('swap.starting')}</p>
			{/if}
			<p class="muted">{t('swap.runningHint')}</p>
		</section>
	{:else if step === 'cleanup'}
		<section class="tile panel">
			<h2 class="tile-title">{t('swap.cleanupTitle')}</h2>
			<p>{t('swap.cleanupText', { disk: diskName })}</p>
			<div class="row-actions"><button class="btn btn-primary" disabled={busy} onclick={removeOld}>{t('swap.removeOld')}</button></div>
		</section>
	{:else}
		<section class="tile panel">
			<h2 class="tile-title">{t('swap.doneTitle')}</h2>
			<p>{t('swap.doneText', { volume: volumeName })}</p>
			{#if oldDisk && !oldDisk.volume}
				<div class="find">
					<p>{t('swap.pullNow', { disk: diskName, serial: oldDisk.serial || '–' })}</p>
					<button class="btn" disabled={busy || identifyUntil > now} onclick={() => identify(diskName)}>{t('swap.identify')}</button>
				</div>
			{/if}
			<div class="row-actions"><a class="btn" href="/storage">{t('swap.toStorage')}</a></div>
		</section>
	{/if}
{/if}

{#if confirming}
	<Dialog title={t('swap.confirmTitle')} onclose={() => (confirming = false)}>
		<p>{t('swap.confirmText', { disk: chosenNew, volume: volumeName })}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (confirming = false)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-danger" disabled={busy} onclick={start}>{t('swap.confirm')}</button>
		</div>
	</Dialog>
{/if}

<style>
	.steps {
		display: flex;
		gap: 8px 16px;
		flex-wrap: wrap;
		list-style: none;
		padding: 0;
		margin: 0 0 12px;
		color: var(--muted);
		font-size: 0.8125rem;
	}
	.steps .current {
		color: var(--text);
		font-weight: 600;
	}
	.disks {
		border: 0;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: 6px;
	}
	legend {
		font-weight: 500;
		margin-bottom: 4px;
	}
	.choice {
		display: flex;
		align-items: center;
		gap: 10px;
		flex-wrap: wrap;
		padding: 8px 10px;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
	}
	.choice.off {
		opacity: 0.6;
	}
	.what {
		flex: 1 1 14rem;
		font-size: 0.8125rem;
	}
	.find {
		display: flex;
		gap: 12px;
		align-items: center;
		flex-wrap: wrap;
	}
	.find p {
		flex: 1 1 18rem;
		margin: 0;
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
