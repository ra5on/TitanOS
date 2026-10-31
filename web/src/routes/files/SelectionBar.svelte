<script lang="ts">
	import Icon from '#lib/components/Icon.svelte';
	import { formatBytes } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import type { FileCommand } from './commands.ts';
	import type { USBBackup } from './usb-backup.ts';

	type Props = {
		count: number;
		bytes: number;
		/** Selected folders; their size is known only after measuring. */
		folders: number;
		/** Folders whose size is not in `bytes`; 0 once measured. */
		uncounted: number;
		measuring: boolean;
		/** The USB backup the folder on screen belongs to. */
		backup: USBBackup | undefined;
		stick: boolean;
		editable: boolean;
		writable: boolean;
		onmeasure: () => void;
		oncommand: (command: FileCommand) => void;
		/** The "more" button on phones opens the menu of the selection. */
		onmore: (button: HTMLElement) => void;
	};
	let {
		count,
		bytes,
		folders,
		uncounted,
		measuring,
		backup,
		stick,
		editable,
		writable,
		onmeasure,
		oncommand,
		onmore,
	}: Props = $props();
</script>

<!-- Stays at the bottom of the screen while the list scrolls. -->
<div class="actions" role="region" aria-label={t('files.selectionActions')}>
	<span class="count"
		>{t('files.selected', { count })} · {formatBytes(bytes)}{#if uncounted}
			· <span class="muted">{t('files.foldersNotCounted', { count: uncounted })}</span>{/if}</span
	>
	{#if folders}<button class="btn secondary" disabled={measuring} onclick={onmeasure}
			>{t(measuring ? 'files.measuring' : 'files.measureFolders')}</button
		>{/if}
	{#if backup}<button
			type="button"
			class="btn"
			disabled={backup.state?.running}
			onclick={() => oncommand('restore')}>{t('files.restoreSelected')}</button
		>{/if}
	<button type="button" class="btn" onclick={() => oncommand('download')}
		><Icon name="download" size={16} />{t('files.download')}</button
	>
	{#if !stick}<button type="button" class="btn secondary" onclick={() => oncommand('copy')}
			>{t('files.copy')}</button
		>{/if}
	{#if editable}<button type="button" class="btn secondary" onclick={() => oncommand('edit')}
			>{t('files.edit')}</button
		>{/if}
	{#if writable}
		<button type="button" class="btn secondary" onclick={() => oncommand('move')}
			>{t('files.move')}</button
		>
		{#if count === 1}<button type="button" class="btn secondary" onclick={() => oncommand('rename')}
				>{t('files.rename')}</button
			>{:else}<button class="btn secondary" onclick={() => oncommand('renameBatch')}
				>{t('files.renameBatch')}</button
			>{/if}
		<button type="button" class="btn btn-danger" onclick={() => oncommand('delete')}
			><Icon name="trash" size={16} />{t('files.delete')}</button
		>
	{/if}
	<!-- On phones the other actions are in the menu, so the bar keeps one line. -->
	<button
		type="button"
		class="btn icon more-actions"
		aria-label={t('files.selectionMore')}
		title={t('files.selectionMore')}
		aria-haspopup="menu"
		onclick={(event) => onmore(event.currentTarget)}><Icon name="more" size={16} /></button
	>
	<button
		type="button"
		class="btn icon clear"
		aria-label={t('files.clearSelection')}
		title={t('files.clearSelection')}
		onclick={() => oncommand('clearSelection')}><Icon name="close" size={16} /></button
	>
</div>

<style>
	.btn.icon {
		width: 2.375rem;
		padding: 0;
		justify-content: center;
	}
	.actions {
		position: sticky;
		bottom: 12px;
		z-index: 5;
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px;
		margin: 0 12px 12px;
		padding: 8px 10px 8px 14px;
		background: var(--surface);
		border: 1px solid var(--accent);
		border-radius: var(--radius-ctl);
		box-shadow: var(--shadow-menu);
	}
	.actions .btn {
		height: 2.25rem;
	}
	.actions .more-actions {
		display: none;
	}
	.count {
		margin-right: auto;
		font-size: var(--text-base);
	}

	@media (max-width: 760px) {
		.btn.icon {
			height: 2.75rem;
			width: 2.75rem;
		}
		.actions .secondary {
			display: none;
		}
		.actions .more-actions {
			display: inline-flex;
		}
		/* Count and "clear" on the first line, the actions below. */
		.actions > * {
			order: 2;
		}
		.actions .count {
			order: 0;
			flex: 1 1 calc(100% - 3.5rem);
		}
		.actions .clear {
			order: 1;
		}
		.actions .btn {
			height: 2.75rem;
		}
		/* Phones have their own bar for the selection above the list. */
		.actions {
			display: none;
		}
	}
</style>
