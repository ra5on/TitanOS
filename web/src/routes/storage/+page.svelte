<script lang="ts">
	import { api, ApiError, type DiskSleepStatus, type StorageStatus, type Volume } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import { formatDiskBytes } from '#lib/format.ts';
	import { errorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { refreshStorage, storage } from '#lib/storage.svelte.ts';
	import AddDiskDialog from './AddDiskDialog.svelte';
	import CreateVolumeDialog from './CreateVolumeDialog.svelte';
	import DeleteVolumeDialog from './DeleteVolumeDialog.svelte';
	import { diskStatus, diskType, smartText } from './labels.ts';
	import VolumeCard from './VolumeCard.svelte';

	const canAdminister = $derived(session.data?.user?.role === 'owner' || session.data?.user?.role === 'admin');
	const disks = $derived(storage.status?.disks ?? []);
	const volumes = $derived(storage.status?.volumes ?? []);
	const free = $derived(disks.filter((disk) => disk.role === 'free' && !disk.testing));

	let dialog = $state<{ kind: 'create' } | { kind: 'add' | 'delete'; volume: string } | { kind: 'remove'; volume: string; disk: string } | null>(null);
	let busy = $state(false);
	let error = $state<{ text: string; detail: string } | null>(null);
	let sleepChoice = $state<number | null>(null);
	const diskSleep = $derived(storage.status?.diskSleep);
	const idleMinutes = [0, 5, 10, 15, 30, 60, 120, 180];

	// Dialogs work on the live volume, so that they follow its state.
	const dialogVolume = $derived.by(() => {
		const current = dialog;
		if (!current || !('volume' in current)) return undefined;
		return volumes.find((volume) => volume.name === current.volume);
	});

	const summary = $derived.by(() => {
		const parts = [
			volumes.length === 1 ? t('storage.summaryVolume') : t('storage.summaryVolumes', { count: volumes.length }),
			disks.length === 1 ? t('storage.summaryDisk') : t('storage.summaryDisks', { count: disks.length })
		];
		if (free.length > 0) parts.push(t('storage.summaryFree', { count: free.length }));
		return parts.join(' · ');
	});

	const hours = $derived(new Intl.NumberFormat(i18n.locale));

	/** Runs an action that needs no dialog and shows what went wrong. */
	async function run(action: () => Promise<unknown>) {
		busy = true;
		error = null;
		try {
			await action();
		} catch (err) {
			const code = err instanceof ApiError ? err.code : 'internal';
			error = { text: errorText(code), detail: code === 'storage_failed' && err instanceof ApiError ? err.message : '' };
		}
		await refreshStorage();
		busy = false;
	}

	const rescan = () => run(async () => (storage.status = await api.post<StorageStatus>('/storage/rescan')));
	const check = (volume: Volume, start: boolean) => run(() => api.post(`/storage/volumes/${volume.name}/check`, { action: start ? 'start' : 'stop' }));
	const saveDiskSleep = () => run(async () => {
		if (sleepChoice === null) return;
		const updated = await api.put<DiskSleepStatus>('/storage/disk-sleep', { idleMinutes: sleepChoice });
		if (storage.status) storage.status.diskSleep = updated;
		sleepChoice = null;
	});

	function removeDisk() {
		if (dialog?.kind !== 'remove') return;
		const { volume, disk } = dialog;
		dialog = null;
		run(() => api.delete(`/storage/volumes/${volume}/disks/${disk}`));
	}

	function done() {
		dialog = null;
		refreshStorage();
	}
</script>

<div class="head">
	<div class="title">
		<h1>{t('storage.title')}</h1>
		{#if storage.status}<p class="muted">{summary}</p>{/if}
	</div>
	{#if canAdminister}
		<div class="buttons">
			<button type="button" class="btn" onclick={rescan} disabled={busy}>{t('storage.rescan')}</button>
			<button type="button" class="btn btn-primary" onclick={() => (dialog = { kind: 'create' })}>{t('storage.newVolume')}</button>
		</div>
	{/if}
</div>

{#if error}
	<div class="notice-error" role="alert">{error.text}{#if error.detail}<pre>{error.detail}</pre>{/if}</div>
{/if}

<section aria-labelledby="volumes-title">
	<h2 class="tile-title" id="volumes-title">{t('storage.volumes')}</h2>
	<div class="volumes">
		{#each volumes as volume (volume.name)}
			<VolumeCard
				{volume}
				{canAdminister}
				{busy}
				oncheck={(start) => check(volume, start)}
				onadd={() => (dialog = { kind: 'add', volume: volume.name })}
				ondelete={() => (dialog = { kind: 'delete', volume: volume.name })}
				onremove={(disk) => (dialog = { kind: 'remove', volume: volume.name, disk })}
			/>
		{:else}
			{#if storage.status}
				<div class="tile empty">
					<p>{t('storage.noVolumes')}</p>
					<p class="muted">{t('storage.noVolumesHint')}</p>
				</div>
			{/if}
		{/each}
	</div>
</section>

{#if canAdminister}
	<section class="tile disk-sleep" aria-labelledby="disk-sleep-title">
		<div class="sleep-copy">
			<h2 class="tile-title" id="disk-sleep-title">{t('storage.sleepTitle')}</h2>
			<p class="muted">{t('storage.sleepHint')}</p>
		</div>
		<div class="sleep-controls">
			<label for="disk-sleep-time">{t('storage.sleepAfter')}</label>
			<select class="field" id="disk-sleep-time" value={sleepChoice ?? diskSleep?.idleMinutes ?? 0} onchange={(event) => (sleepChoice = Number(event.currentTarget.value))} disabled={busy || !diskSleep?.available}>
				{#each idleMinutes as minutes}
					<option value={minutes}>{minutes === 0 ? t('storage.sleepOff') : minutes < 60 ? t('storage.sleepMinutes', { count: minutes }) : minutes === 60 ? t('storage.sleepHour') : t('storage.sleepHours', { count: minutes / 60 })}</option>
				{/each}
			</select>
			<button type="button" class="btn btn-primary" onclick={saveDiskSleep} disabled={busy || !diskSleep?.available || sleepChoice === null || sleepChoice === diskSleep.idleMinutes}>{t('common.save')}</button>
		</div>
		<p class="muted sleep-detail">{t('storage.sleepDetails')}</p>
		{#if diskSleep && !diskSleep.available}<p class="notice-error" role="alert">{t('storage.sleepUnavailable')}</p>{/if}
		{#if diskSleep?.errors.length}
			<div class="notice-error" role="alert">
				<p>{t('storage.sleepFailed')}</p>
				{#each diskSleep.errors as problem}<p><strong class="mono">{problem.disk}</strong>: {problem.message}</p>{/each}
			</div>
		{/if}
	</section>
{/if}

<section aria-labelledby="disks-title">
	<h2 class="tile-title" id="disks-title">{t('storage.disks')}</h2>
	<div class="tile table">
		<table>
			<thead>
				<tr>
					<th scope="col">{t('disk.status')}</th>
					<th scope="col">{t('disk.device')}</th>
					<th scope="col">{t('disk.type')}</th>
					<th scope="col" class="num">{t('disk.size')}</th>
					<th scope="col" class="num">{t('disk.temp')}</th>
					<th scope="col">{t('disk.volume')}</th>
					<th scope="col">{t('disk.smart')}</th>
					<th scope="col" class="num">{t('disk.errors')}</th>
					<th scope="col" class="num">{t('disk.hours')}</th>
				</tr>
			</thead>
			<tbody>
				{#each disks as disk (disk.name)}
					{@const status = diskStatus(disk)}
					<tr class:building={disk.memberState === 'rebuilding'} class:faulty={disk.memberState === 'faulty'}>
						<td><span class="status"><span class="dot {status.dot}" aria-hidden="true"></span><span class="status-text">{status.text}</span></span></td>
						<td>
							<span class="mono device">{disk.name}</span>
							<span class="sub">{[disk.model, disk.serial].filter(Boolean).join(' · ')}</span>
						</td>
						<td class="muted">{diskType(disk)}</td>
						<td class="mono num">{formatDiskBytes(disk.sizeBytes)}</td>
						<td class="mono num">{disk.smart && disk.smart.temperatureC > 0 ? `${disk.smart.temperatureC} °C` : '–'}</td>
						<td>
							{#if disk.volume}<span class="mono">{disk.volume}</span>
							{:else if disk.role === 'used' && disk.contents?.length}<span class="muted">{t('disk.contents', { contents: disk.contents.join(', ') })}</span>
							{:else if disk.role === 'stick'}<span class="muted">–</span>
							{:else}<span class="muted">{t('disk.unassigned')}</span>{/if}
						</td>
						<td class:bad={disk.smart && !disk.smart.healthy}>{smartText(disk)}</td>
						<td class="mono num">{disk.smart ? disk.smart.errors : '–'}</td>
						<td class="mono num">{disk.smart ? hours.format(disk.smart.powerOnHours) : '–'}</td>
					</tr>
				{:else}
					{#if storage.status}<tr><td colspan="9" class="muted">{t('storage.noDisks')}</td></tr>{/if}
				{/each}
			</tbody>
		</table>
	</div>
</section>

{#if free.length > 0}
	<section class="tile free" aria-labelledby="free-title">
		<div class="free-text">
			<h2 id="free-title">
				{free.length === 1 ? t('storage.freeOne', { disk: free[0].name, size: formatDiskBytes(free[0].sizeBytes) }) : t('storage.freeMany', { count: free.length })}
			</h2>
			<p class="muted">{t('storage.freeHint')}</p>
		</div>
		{#if canAdminister}
			<button type="button" class="btn btn-primary" onclick={() => (dialog = { kind: 'create' })}>{t('storage.createVolume')}</button>
		{/if}
	</section>
{/if}

{#if dialog?.kind === 'create'}
	<CreateVolumeDialog {disks} taken={volumes.map((volume) => volume.name)} onclose={() => (dialog = null)} ondone={done} />
{:else if dialog?.kind === 'add' && dialogVolume}
	<AddDiskDialog volume={dialogVolume} {disks} onclose={() => (dialog = null)} ondone={done} />
{:else if dialog?.kind === 'delete' && dialogVolume}
	<DeleteVolumeDialog volume={dialogVolume} onclose={() => (dialog = null)} ondone={done} />
{:else if dialog?.kind === 'remove'}
	<Dialog title={t('storage.removeDisk', { disk: dialog.disk })} onclose={() => (dialog = null)}>
		<p class="remove-text">{t('storage.removeDiskConfirm', { disk: dialog.disk, volume: dialog.volume })}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (dialog = null)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-warn" onclick={removeDisk}>{t('storage.remove')}</button>
		</div>
	</Dialog>
{/if}

<style>
	.head {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-end;
		justify-content: space-between;
		gap: 12px;
	}
	.title {
		display: flex;
		flex-direction: column;
		gap: 4px;
		font-size: 0.875rem;
	}
	h1 {
		font-size: 1.5rem;
		font-weight: 600;
	}
	.buttons {
		display: flex;
		flex-wrap: wrap;
		gap: 10px;
	}
	section {
		display: flex;
		flex-direction: column;
		gap: 12px;
	}
	.volumes {
		display: flex;
		flex-wrap: wrap;
		gap: var(--gap);
	}
	.empty {
		flex: 1 1 auto;
		padding: 28px 22px;
		display: flex;
		flex-direction: column;
		gap: 4px;
		font-size: 0.875rem;
	}
	.table {
		overflow-x: auto;
	}
	table {
		width: 100%;
		min-width: 980px;
		border-collapse: collapse;
		font-size: 0.875rem;
	}
	th {
		padding: 14px 16px;
		border-bottom: 1px solid var(--panel-border);
		text-align: left;
		font-size: 0.75rem;
		font-weight: 500;
		color: var(--muted);
		white-space: nowrap;
	}
	td {
		padding: 12px 16px;
		border-bottom: 1px solid var(--panel-border);
		vertical-align: top;
	}
	tbody tr:last-child td {
		border-bottom: 0;
	}
	.num {
		text-align: right;
	}
	tr.building {
		background: var(--warn-bg);
	}
	tr.building td:first-child {
		color: var(--warn-text);
	}
	tr.faulty {
		background: var(--error-bg);
	}
	tr.faulty td:first-child,
	.bad {
		color: var(--error-text);
	}
	.status {
		display: inline-flex;
		align-items: center;
		gap: 8px;
		white-space: nowrap;
	}
	.status-text {
		display: inline-block;
	}
	.status-text::first-letter {
		text-transform: uppercase;
	}
	.device {
		display: block;
		font-weight: 500;
	}
	.sub {
		display: block;
		font-size: 0.75rem;
		color: var(--muted);
	}
	.free {
		flex-direction: row;
		flex-wrap: wrap;
		align-items: center;
		gap: 16px;
		padding: 18px 20px;
		border-style: dashed;
	}
	.disk-sleep { padding: 18px 20px; }
	.sleep-copy { display: flex; flex-direction: column; gap: 6px; font-size: 0.875rem; }
	.sleep-controls { display: flex; flex-wrap: wrap; align-items: center; gap: 10px; font-size: 0.875rem; }
	.sleep-controls select { width: auto; min-width: 150px; }
	.sleep-detail { font-size: 0.8125rem; }
	.free-text {
		flex: 1 1 360px;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 4px;
		font-size: 0.875rem;
	}
	.free h2 {
		font-size: 0.9375rem;
		font-weight: 600;
	}
	.remove-text {
		font-size: 0.875rem;
	}
	pre {
		margin: 8px 0 0;
		font-family: var(--font-mono);
		font-size: 0.75rem;
		white-space: pre-wrap;
		word-break: break-word;
	}
</style>
