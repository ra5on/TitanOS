<script lang="ts">
	import type { FileRoot, UsbDevice } from '#lib/api.ts';
	import type { Favorite } from '#lib/file-actions.ts';
	import Icon from '#lib/components/Icon.svelte';
	import { formatBytes, formatDiskBytes, formatDateTime } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import FolderTree from './FolderTree.svelte';
	import { backupFolder, type USBBackup } from './usb-backup.ts';

	type Props = {
		roots: FileRoot[];
		/** The place on screen; "" while the trash is open. */
		current: string;
		dir: string;
		showHidden: boolean;
		admin: boolean;
		usbDevices: UsbDevice[];
		usbBusy: string;
		usbLoadError: boolean;
		backups: USBBackup[];
		backupLoadError: boolean;
		backupProgressError: boolean;
		openingBackup: string;
		favorites: Favorite[];
		onopen: (root: string, path: string) => void;
		onusb: (device: UsbDevice, action: 'mount' | 'eject') => void;
		onbackup: (backup: USBBackup, restore: boolean) => void;
		onrefreshbackups: () => void;
		onmanagefavorites: () => void;
		onremovefavorite: (favorite: Favorite) => void;
		onformat: () => void;
	};
	let {
		roots,
		current,
		dir,
		showHidden,
		admin,
		usbDevices,
		usbBusy,
		usbLoadError,
		backups,
		backupLoadError,
		backupProgressError,
		openingBackup,
		favorites,
		onopen,
		onusb,
		onbackup,
		onrefreshbackups,
		onmanagefavorites,
		onremovefavorite,
		onformat,
	}: Props = $props();

	const kinds = ['share', 'volume', 'usb', 'stick'] as const;
	const unmounted = $derived(usbDevices.filter((device) => !device.mounted));
	const groupName = (kind: (typeof kinds)[number]) =>
		t(
			kind === 'share'
				? 'files.shares'
				: kind === 'volume'
					? 'files.volumes'
					: kind === 'usb'
						? 'files.usb'
						: 'files.stick',
		);
	const favoriteName = (favorite: Favorite) =>
		favorite.label ||
		favorite.path.split('/').at(-1) ||
		roots.find((r) => r.id === favorite.root)?.name ||
		favorite.root;
</script>

{#if favorites.length}
	<div class="group-head">
		<span class="group">{t('files.favorites')}</span>
		<button
			type="button"
			class="icon"
			aria-label={t('files.manageFavorites')}
			title={t('files.manageFavorites')}
			onclick={onmanagefavorites}><Icon name="edit" size={14} /></button
		>
	</div>
	{#each favorites as favorite (`${favorite.root}/${favorite.path}`)}
		<div class="favorite-row">
			<button
				type="button"
				class="place"
				class:current={current === favorite.root && dir === favorite.path}
				title={`${roots.find((r) => r.id === favorite.root)?.name}/${favorite.path}`}
				onclick={() => onopen(favorite.root, favorite.path)}
			>
				<Icon name="star" size={16} /><span class="place-name">{favoriteName(favorite)}</span>
			</button>
			<button
				type="button"
				class="icon remove"
				aria-label={t('files.removeFavorite', { name: favorite.path || favorite.root })}
				onclick={() => onremovefavorite(favorite)}><Icon name="close" size={14} /></button
			>
		</div>
	{/each}
{/if}

{#each kinds as kind (kind)}
	{@const list = roots.filter((candidate) => candidate.kind === kind)}
	{#if list.length > 0}
		{#snippet placesList()}
			{#each list as place (place.id)}
				<FolderTree
					readOnly={!place.writable && kind !== 'stick'}
					root={place.id}
					name={place.name}
					currentRoot={current}
					currentPath={dir}
					{showHidden}
					{onopen}
				/>
				{#if place.id === current && kind !== 'stick'}
					{@const used =
						place.totalBytes > 0
							? Math.min(100, Math.max(0, (1 - place.freeBytes / place.totalBytes) * 100))
							: 0}
					<div class="space" class:full={used >= 90}>
						<div class="bar" aria-hidden="true"><span style:width="{used}%"></span></div>
						<span class="mono"
							>{t('files.free', {
								free: formatDiskBytes(place.freeBytes),
								total: formatDiskBytes(place.totalBytes),
							})}</span
						>
					</div>
				{/if}
				{#if kind === 'usb'}
					{@const device = usbDevices.find((d) => d.root === place.id)}
					{#if device?.readOnly}<span class="note">{t('files.driveReadOnly')}</span>{/if}
					{#if device}<button
							type="button"
							class="chip-btn eject"
							disabled={usbBusy === device.id}
							onclick={() => onusb(device, 'eject')}>{t('files.eject')}</button
						>{/if}
				{/if}
			{/each}
		{/snippet}
		{#if kind === 'volume'}
			<details class="advanced" open={roots.find((r) => r.id === current)?.kind === 'volume'}>
				<summary
					>{t('files.advancedAccess')}<span class="summary-note"
						>{t('files.volumes')} · {t('files.adminOnly')}</span
					></summary
				>
				{@render placesList()}
			</details>
		{:else}
			<span class="group"
				>{groupName(kind)}{kind === 'share' ? '' : ` · ${t('files.adminOnly')}`}</span
			>
			{@render placesList()}
		{/if}
	{/if}
{/each}

{#if admin && unmounted.length > 0}
	<span class="group">{t('files.usbPlugged')}</span>
	{#each unmounted as device (device.id)}
		<div class="usb">
			<Icon name="disk" size={16} />
			<span class="drive-description"
				><span class="place-name" title={device.name}>{device.name}</span><span
					class="drive-details mono"
					>{device.fsType}{#if device.transport}
						· {device.transport.toUpperCase()}{/if}{#if device.size > 0}
						· {formatDiskBytes(device.size)}{/if}</span
				></span
			>
			{#if device.busy}<span class="chip">{t('files.driveBusy')}</span>
			{:else if device.supported}
				<button
					type="button"
					class="chip-btn"
					disabled={usbBusy === device.id}
					onclick={() => onusb(device, 'mount')}>{t('files.mount')}</button
				>
			{:else}
				<span class="chip">{t('files.usbUnsupported')}</span>
			{/if}
		</div>
	{/each}
{/if}

{#if admin}
	<!-- Also for drives without a filesystem, which are not listed above. -->
	<button type="button" class="note link format-link" onclick={onformat}
		><Icon name="disk" size={14} />{t('files.formatDrive')}</button
	>
{/if}

<!-- USB backups only take room when there are some; setting them up happens under Settings. -->
{#if admin && (backups.length > 0 || backupLoadError)}
	<div class="group-head">
		<span class="group">{t('files.usbBackups')}</span>
		<button
			type="button"
			class="icon"
			aria-label={t('files.usbBackupRefresh')}
			title={t('files.usbBackupRefresh')}
			onclick={onrefreshbackups}><Icon name="refresh" size={14} /></button
		>
	</div>
	{#each backups as backup (backup.job.id)}
		{@const drive = usbDevices.find((device) => device.id === backup.job.destination.deviceId)}
		<div class="backup">
			<button
				type="button"
				class="place"
				class:current={drive?.root === current && dir === backupFolder(backup)}
				disabled={!drive?.supported || openingBackup !== ''}
				onclick={() => onbackup(backup, false)}
			>
				<Icon name="disk" size={16} /><span class="place-name">{backup.job.id}</span>
			</button>
			<div class="backup-details">
				<span class="mono backup-path">/{backupFolder(backup)}</span>
				<span>
					{drive
						? drive.supported
							? drive.mounted
								? drive.name
								: t('files.usbBackupMount', { name: drive.name })
							: t('files.usbUnsupported')
						: t(
								usbLoadError ? 'files.usbDeviceLoadFailed' : 'files.usbBackupOffline',
							)}{#if drive?.mounted && Number.isFinite(drive.freeBytes)}
						· <span>{t('files.backupFree', { size: formatDiskBytes(drive.freeBytes) })}</span>{/if}
				</span>
				{#if backupProgressError}<span>{t('files.backupProgressStale')}</span>
				{:else if backup.state?.running}
					<span role="status">{t('files.backupRunning')}</span>
					<progress
						max="100"
						value={backup.state.bytesTotal > 0
							? Math.min(100, Math.max(0, backup.state.percent))
							: undefined}
						aria-label={t('files.backupRunning')}
					></progress>
					<span class="mono"
						>{backup.state.bytesTotal > 0
							? t('files.backupBytes', {
									done: formatBytes(backup.state.bytesDone),
									total: formatBytes(backup.state.bytesTotal),
								})
							: t('files.backupCounting')}</span
					>
				{:else if backup.state?.lastError}<span class="backup-error"
						>{t('files.backupFailed')}: {backup.state.lastError}</span
					>
				{:else}<span
						>{t('files.backupLastOK', {
							time: backup.state?.lastOK
								? formatDateTime(new Date(backup.state.lastOK))
								: t('files.backupNever'),
						})}</span
					>{/if}
				{#if backup.state?.warning}<span>{backup.state.warning}</span>{/if}
				<button
					type="button"
					class="chip-btn"
					disabled={!drive?.supported || openingBackup !== '' || backup.state?.running}
					onclick={() => onbackup(backup, true)}>{t('files.restoreBackup')}</button
				>
			</div>
		</div>
	{/each}
	{#if backupLoadError}<span class="note" role="alert">{t('files.usbBackupLoadFailed')}</span>{/if}
	<a class="note link" href="/settings/backups/data">{t('files.usbBackupSettings')}</a>
{/if}

<style>
	.group {
		display: block;
		padding: 10px 10px 4px;
		font-size: 0.75rem;
		color: var(--muted);
	}
	.group-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding-right: 4px;
	}
	.icon {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 28px;
		height: 28px;
		margin-top: 6px;
		padding: 0;
		background: transparent;
		border: 0;
		border-radius: var(--radius-ctl);
		color: var(--muted);
		cursor: pointer;
	}
	.icon:hover {
		background: var(--chip);
		color: var(--text);
	}
	.place {
		display: flex;
		align-items: center;
		gap: 10px;
		width: 100%;
		min-height: 38px;
		padding: 0 10px;
		background: transparent;
		border: 0;
		border-radius: var(--radius-ctl);
		color: var(--muted);
		font: inherit;
		font-size: 0.875rem;
		text-align: left;
		cursor: pointer;
	}
	.place:hover,
	.place.current {
		background: var(--chip);
		color: var(--text);
	}
	.place:disabled {
		opacity: 0.6;
		cursor: default;
	}
	.place-name {
		flex: 1 1 auto;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.favorite-row {
		display: flex;
		align-items: center;
		min-width: 0;
	}
	.favorite-row .remove {
		margin-top: 0;
		flex: none;
	}
	.advanced summary {
		padding: 10px;
		cursor: pointer;
		font-size: 0.8125rem;
		color: var(--text);
	}
	.summary-note {
		display: block;
		margin-left: 14px;
		color: var(--muted);
		font-size: 0.75rem;
	}
	.space {
		display: flex;
		flex-direction: column;
		gap: 4px;
		margin: 2px 10px 8px 34px;
		font-size: 0.75rem;
		color: var(--muted);
	}
	.bar {
		height: 6px;
		overflow: hidden;
		background: var(--track);
		border-radius: var(--radius-bar);
	}
	.bar span {
		display: block;
		height: 100%;
		background: var(--accent);
		border-radius: inherit;
	}
	.space.full .bar span {
		background: var(--warn-text);
	}
	.chip-btn {
		align-self: flex-start;
		background: var(--chip);
		border: 0;
		border-radius: var(--radius-ctl);
		color: var(--text);
		cursor: pointer;
		font: inherit;
		font-size: 0.8rem;
		padding: 0.25rem 0.6rem;
	}
	.chip-btn:disabled {
		opacity: 0.6;
		cursor: default;
	}
	.eject {
		align-self: flex-end;
		margin: -4px 10px 4px;
	}
	.drive-description {
		flex: 1;
		min-width: 0;
	}
	.drive-description .place-name {
		display: block;
	}
	.drive-details {
		display: block;
		font-size: 0.7rem;
		overflow-wrap: anywhere;
	}
	.usb {
		display: flex;
		align-items: center;
		gap: 10px;
		min-height: 38px;
		padding: 0 10px;
		color: var(--muted);
		font-size: 0.875rem;
	}
	.backup-details {
		display: flex;
		flex-direction: column;
		gap: 3px;
		padding: 0 10px 8px 36px;
		font-size: 0.75rem;
		color: var(--muted);
	}
	.backup-details progress {
		width: 100%;
		height: 6px;
		accent-color: var(--accent);
	}
	.backup-path {
		overflow-wrap: anywhere;
	}
	.backup-error {
		color: var(--error-text);
		overflow-wrap: anywhere;
	}
	.note {
		padding: 4px 10px;
		font-size: 0.8125rem;
	}
	.link {
		color: var(--accent);
	}
	.format-link {
		display: flex;
		align-items: center;
		gap: 6px;
		border: 0;
		background: none;
		text-align: left;
		cursor: pointer;
		font: inherit;
		font-size: 0.8125rem;
	}
</style>
