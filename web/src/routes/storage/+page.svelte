<script lang="ts">
	import Dropdown from '#lib/components/Dropdown.svelte';
	import type { DiskSleepStatus } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { formatDiskBytes } from '#lib/format.ts';
	import StatusBar from '#lib/components/explorer/StatusBar.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { storage } from '#lib/storage.svelte.ts';
	import AddDiskDialog from './AddDiskDialog.svelte';
	import ProfileDialog from './ProfileDialog.svelte';
	import { takesDisk } from './btrfs.ts';
	import CreateVolumeDialog from './CreateVolumeDialog.svelte';
	import DeleteVolumeDialog from './DeleteVolumeDialog.svelte';
	import DiskSleepDialog from './DiskSleepDialog.svelte';
	import StorageDetail from './StorageDetail.svelte';
	import { StoragePage } from './storage-page.svelte.ts';
	import StorageTables from './StorageTables.svelte';

	// State and actions live in storage-page.svelte.ts.
	const sp = new StoragePage();

	// PLAN 67-1: the menu bar of the desktop runs the same actions as the page.
	import { registerMenuCommands } from '#lib/menu-commands.svelte.ts';
	import { lockedBlocks } from './crypt.ts';
	registerMenuCommands({
		'storage.newVolume': {
			run: () => (sp.dialog = { kind: 'create' }),
			enabled: () => sp.canAdminister,
		},
		'storage.addDisk': {
			run: () => sp.detail && (sp.dialog = { kind: 'add', volume: sp.detail.name }),
			enabled: () =>
				sp.canAdminister && !!sp.detail && takesDisk(sp.detail) && !lockedBlocks(sp.detail),
		},
		'storage.scrub': {
			run: () => sp.detail && sp.check(sp.detail, sp.detail.state !== 'checking'),
			enabled: () =>
				sp.canAdminister &&
				!sp.busy &&
				!!sp.detail &&
				sp.detail.level !== 'raid0' &&
				(sp.detail.state === 'ok' || sp.detail.state === 'checking') &&
				!lockedBlocks(sp.detail),
			checked: () => sp.detail?.state === 'checking',
		},
		'storage.rescan': { run: sp.rescan, enabled: () => sp.canAdminister && !sp.busy },
		'storage.sleep': { run: () => (sp.sleepOpen = true), enabled: () => sp.canAdminister },
		...Object.fromEntries(
			(['volumes', 'disks'] as const).map((value) => [
				'storage.tab.' + value,
				{ run: () => sp.switchTab(value), checked: () => sp.tab === value },
			]),
		),
	});
</script>

<PageHead title={t('storage.title')}>
	{#if storage.status}<p class="muted">{sp.summary}</p>{/if}
	{#snippet actions()}{#if sp.canAdminister}
			<Dropdown label={t('storage.options')} text={t('storage.options')} icon="more" align="right">
				{#snippet children(close)}
					<a role="menuitem" href="/storage/import">{t('ops.import')}</a>
					<a role="menuitem" href="/tools/maintenance">{t('ops.maintenance')}</a>
					<a role="menuitem" href="/storage/smart">{t('smart.open')}</a>
					<button
						role="menuitem"
						disabled={sp.busy}
						onclick={() => {
							close();
							sp.rescan();
						}}>{t('storage.rescan')}</button
					>
					<button
						role="menuitem"
						onclick={() => {
							close();
							sp.sleepOpen = true;
						}}>{t('storage.sleepTitle')}</button
					>
				{/snippet}
			</Dropdown>
			<button class="btn btn-primary" onclick={() => (sp.dialog = { kind: 'create' })}
				>{t('storage.newVolume')}</button
			>
		{/if}{/snippet}
</PageHead>
{#if sp.error}<div class="notice-error" role="alert">
		{sp.error.text}{#if sp.error.detail}<pre>{sp.error.detail}</pre>{/if}
	</div>{/if}
{#if sp.checksHint.length > 0}<section
		class="notice-warn checks-hint"
		aria-labelledby="checks-hint-title"
	>
		<strong id="checks-hint-title">{t('storage.checksHint.title')}</strong>
		<p>
			{t(sp.checksHint.length === 1 ? 'storage.checksHint.one' : 'storage.checksHint.many', {
				volumes: sp.checksHint.join(', '),
			})}
		</p>
		{#if sp.hintError}<p role="alert">{sp.hintError}</p>{/if}
		<div class="row-actions">
			<a class="btn btn-primary" href="/tools/maintenance">{t('storage.checksHint.setup')}</a>
			<button class="btn" disabled={sp.hintBusy} onclick={sp.dismissChecksHint}
				>{t('storage.checksHint.dismiss')}</button
			>
		</div>
	</section>{/if}
<div class="storage-toolbar">
	<div class="tabs" role="group" aria-label={t('storage.title')}>
		<button
			class:chosen={sp.tab === 'volumes'}
			aria-pressed={sp.tab === 'volumes'}
			onclick={() => sp.switchTab('volumes')}
			>{t('storage.tabVolumes')} <span>{sp.volumes.length}</span></button
		>
		<button
			class:chosen={sp.tab === 'disks'}
			aria-pressed={sp.tab === 'disks'}
			onclick={() => sp.switchTab('disks')}
			>{t('storage.tabDisks')} <span>{sp.disks.length}</span></button
		>
	</div>
	<input
		class="field search"
		type="search"
		aria-label={t('storage.search')}
		placeholder={t('storage.search')}
		bind:value={sp.query}
	/>
	<select class="field" aria-label={t('disk.status')} bind:value={sp.filter}>
		<option value="all">{t('storage.all')}</option><option value="problems"
			>{t('storage.problems')}</option
		>
		{#if sp.tab === 'disks'}<option value="free">{t('storage.unassigned')}</option><option
				value="assigned">{t('storage.assigned')}</option
			><option value="hdd">HDD</option><option value="ssd">SSD</option><option value="nvme"
				>NVMe</option
			>{/if}
	</select>
	{#if sp.tab === 'disks'}<select
			class="field"
			aria-label={t('disk.volume')}
			bind:value={sp.volumeFilter}
			><option value="">{t('storage.all')}</option>{#each sp.volumes as v}<option value={v.name}
					>{v.name}</option
				>{/each}</select
		>{/if}
	<select class="field" aria-label={t('storage.sort')} bind:value={sp.sort}
		><option value="name">{t('disk.device')}</option><option value="size">{t('disk.size')}</option
		>{#if sp.tab === 'disks'}<option value="temperature">{t('disk.temp')}</option>{/if}</select
	>
</div>
<div
	class="storage-layout"
	class:with-detail={sp.tab === 'volumes' ? !!sp.detail : !!sp.diskDetail}
>
	<StorageTables {sp} />
	<StorageDetail {sp} />
</div>
{#if storage.status}
	{@const size = sp.volumes.reduce((sum, v) => sum + v.sizeBytes, 0)}
	{@const used = sp.volumes.reduce((sum, v) => sum + v.usedBytes, 0)}
	{@const problems = sp.volumes.filter((v) => !['ok', 'syncing'].includes(v.state)).length}
	<!-- PLAN 75-19, style W4: the figures of the page in one line at the bottom. -->
	<StatusBar label={t('storage.title')}>
		<span>{t('storage.tabVolumes')} <b>{sp.volumes.length}</b></span>
		<span>{t('storage.tabDisks')} <b>{sp.disks.length}</b></span>
		{#if size > 0}<span
				>{t('storage.used')}
				<b>{t('storage.usedOf', { used: formatDiskBytes(used), size: formatDiskBytes(size) })}</b
				></span
			>{/if}
		{#if problems}<span class="warn">{t('storage.problemCount', { count: problems })}</span>{/if}
	</StatusBar>
{/if}
{#if sp.sleepOpen}<DiskSleepDialog
		status={sp.diskSleep}
		focus={sp.sleepFocus}
		onclose={() => ((sp.sleepOpen = false), (sp.sleepFocus = undefined))}
		onsaved={(updated: DiskSleepStatus) => {
			if (storage.status) storage.status.diskSleep = updated;
		}}
	/>{/if}

{#if sp.dialog?.kind === 'create'}
	<CreateVolumeDialog
		disks={sp.disks}
		taken={sp.volumes.map((volume) => volume.name)}
		onclose={() => (sp.dialog = null)}
		ondone={sp.done}
	/>
{:else if sp.dialog?.kind === 'add' && sp.dialogVolume}
	{#key sp.dialog}<AddDiskDialog
			volume={sp.dialogVolume}
			disks={sp.disks}
			preselect={sp.dialog.disk}
			onclose={() => (sp.dialog = null)}
			ondone={sp.done}
		/>{/key}
{:else if sp.dialog?.kind === 'profile' && sp.dialogVolume}
	{#key sp.dialog}<ProfileDialog
			volume={sp.dialogVolume}
			onclose={() => (sp.dialog = null)}
			ondone={sp.done}
		/>{/key}
{:else if sp.dialog?.kind === 'delete' && sp.dialogVolume}
	<DeleteVolumeDialog
		volume={sp.dialogVolume}
		usage={sp.usageFor(sp.dialogVolume.name)}
		onclose={() => (sp.dialog = null)}
		ondone={sp.done}
	/>
{:else if sp.dialog?.kind === 'remove'}
	<Dialog
		title={t('storage.removeDisk', { disk: sp.dialog.disk })}
		locked={sp.busy}
		onclose={() => (sp.dialog = null)}
	>
		<p class="remove-text notice-warn">
			{t(
				sp.dialogVolume?.filesystem === 'btrfs'
					? 'btrfs.removeConfirm'
					: 'storage.removeDiskConfirm',
				{ disk: sp.dialog.disk, volume: sp.dialog.volume },
			)}
		</p>
		{#if sp.error}<div class="notice-error" role="alert">
				{sp.error.text}{#if sp.error.detail}<pre>{sp.error.detail}</pre>{/if}
			</div>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={sp.busy} onclick={() => (sp.dialog = null)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-danger" disabled={sp.busy} onclick={sp.removeDisk}
				>{t('storage.remove')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.checks-hint {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
	}
	.checks-hint p {
		margin: 0;
	}
	.checks-hint .row-actions {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2-5);
	}
	.storage-toolbar,
	.tabs {
		display: flex;
		align-items: center;
		gap: var(--space-2-5);
		flex-wrap: wrap;
	}
	.storage-toolbar {
		gap: var(--space-3);
	}
	.tabs {
		gap: 3px;
		border-bottom: 1px solid var(--border);
	}
	.tabs button {
		padding: var(--space-3);
		border: 0;
		background: transparent;
		color: var(--muted);
		border-bottom: 2px solid transparent;
		cursor: pointer;
	}
	.tabs .chosen {
		color: var(--text);
		border-color: var(--accent);
	}
	.tabs span {
		margin-left: 7px;
		font-family: var(--font-mono);
		font-size: var(--text-sm);
	}
	.search {
		flex: 1;
		min-width: 180px;
	}
	.storage-toolbar select {
		width: auto;
		max-width: 200px;
	}
	.storage-layout {
		display: grid;
		gap: 20px;
		align-items: start;
	}
	.storage-layout.with-detail {
		grid-template-columns: minmax(0, 1.4fr) minmax(310px, 1fr);
	}
	@media (max-width: 1100px) {
		.storage-layout.with-detail {
			grid-template-columns: 1fr;
		}
	}
	@media (max-width: 760px) {
		.storage-toolbar {
			align-items: stretch;
		}
		.search {
			flex-basis: 100%;
		}
		.tabs {
			width: 100%;
		}
	}
</style>
