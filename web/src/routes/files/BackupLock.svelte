<script lang="ts">
	import Dialog from '#lib/components/Dialog.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import type { UsbDrives } from './usb.svelte.ts';

	type Props = {
		usb: UsbDrives;
		/** The USB drive on screen. */
		rootId: string;
		locked: boolean;
		/** The drive was locked again. */
		onlock: () => void;
	};
	let { usb, rootId, locked, onlock }: Props = $props();

	let unlocking = $state(false);
</script>

<div class="toolbar">
	<span>{t(locked ? 'files.backupLocked' : 'files.backupUnlocked')}</span>
	<button
		class="btn"
		onclick={() => {
			if (locked) unlocking = true;
			else {
				usb.unlockedRoot = '';
				onlock();
			}
		}}>{t(locked ? 'files.unlockBackup' : 'files.lockBackup')}</button
	>
</div>

{#if unlocking}
	<Dialog title={t('files.unlockBackup')} onclose={() => (unlocking = false)}>
		<p>{t('files.unlockBackupHelp')}</p>
		<div class="dialog-actions">
			<button class="btn" onclick={() => (unlocking = false)}>{t('common.cancel')}</button>
			<button
				class="btn btn-warn"
				onclick={() => {
					usb.unlockedRoot = rootId;
					unlocking = false;
				}}>{t('files.unlockBackup')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.toolbar {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px;
		padding: 10px 12px;
		border-bottom: 1px solid var(--panel-border);
	}
	.toolbar .btn {
		height: 2.375rem;
	}
	@media (max-width: 760px) {
		.toolbar .btn {
			height: 2.75rem;
		}
	}
</style>
