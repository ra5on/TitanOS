<script lang="ts">
	import type { FileEntry, FileRoot } from '#lib/api.ts';
	import { assistant } from '#lib/assistant.svelte.ts';
	import ContextMenu from '#lib/components/ContextMenu.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { previewKind, type FileView } from '#lib/file-actions.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { views, type FileCommand } from './commands.ts';

	type Props = {
		/** The menu of entries, opened on one of them. */
		menu: { entry: FileEntry; x: number; y: number } | null;
		/** The menu of the empty part of the folder. */
		background: { x: number; y: number } | null;
		root: FileRoot | undefined;
		selected: string[];
		editable: boolean;
		writable: boolean;
		canPaste: boolean;
		view: FileView;
		/** Runs a command; the entry is the one the menu was opened on. */
		oncommand: (command: FileCommand, entry?: FileEntry) => void;
		onclose: () => void;
	};
	let {
		menu,
		background,
		root,
		selected,
		editable,
		writable,
		canPaste,
		view,
		oncommand,
		onclose,
	}: Props = $props();
</script>

{#if menu && root}
	{@const entry = menu.entry}
	{@const one = selected.length === 1}
	<ContextMenu
		label={one
			? t('files.actionsFor', { name: entry.name })
			: t('files.selected', { count: selected.length })}
		x={menu.x}
		y={menu.y}
		{onclose}
	>
		<div class="menu-title" role="presentation">
			{one ? entry.name : t('files.selected', { count: selected.length })}
		</div>
		{#if one && entry.dir}<button
				type="button"
				role="menuitem"
				onclick={() => oncommand('openFolder', entry)}
				><Icon name="folder" size={16} /><span>{t('files.openFolder')}</span><kbd aria-hidden="true"
					>Enter</kbd
				></button
			>
		{:else if one && previewKind(entry.name, entry.size)}<button
				type="button"
				role="menuitem"
				onclick={() => oncommand('preview', entry)}
				><Icon name="search" size={16} /><span>{t('files.preview')}</span><kbd aria-hidden="true"
					>Space</kbd
				></button
			>{/if}
		{#if editable}<button type="button" role="menuitem" onclick={() => oncommand('edit', entry)}
				><Icon name="edit" size={16} /><span>{t('files.edit')}</span></button
			>{/if}
		{#if one && !entry.dir && root.kind !== 'stick' && assistant.status?.available && ((previewKind(entry.name, entry.size) === 'text' && entry.size <= 256 << 10) || (previewKind(entry.name, entry.size) === 'image' && entry.size <= 4 << 20))}
			<button type="button" role="menuitem" onclick={() => oncommand('assistant', entry)}
				><Icon name="chat" size={16} /><span>{t('assistant.askFileMenu')}</span></button
			>
		{/if}
		<button type="button" role="menuitem" onclick={() => oncommand('download', entry)}
			><Icon name="download" size={16} /><span>{t('files.download')}</span></button
		>
		{#if one && !entry.dir && /\.zip$/i.test(entry.name)}<button
				type="button"
				role="menuitem"
				onclick={() => oncommand('archive', entry)}
				><Icon name="archive" size={16} /><span>{t('files.archiveTitle')}</span></button
			>{/if}
		{#if root.kind !== 'stick'}
			<hr />
			<button type="button" role="menuitem" onclick={() => oncommand('clipboardCopy', entry)}
				><Icon name="copy" size={16} /><span>{t('files.clipboardCopy')}</span><kbd
					aria-hidden="true">Ctrl+C</kbd
				></button
			>
			{#if writable}<button type="button" role="menuitem" onclick={() => oncommand('cut', entry)}
					><i class="no-icon" aria-hidden="true"></i><span>{t('files.cut')}</span><kbd
						aria-hidden="true">Ctrl+X</kbd
					></button
				>{/if}
			<button type="button" role="menuitem" onclick={() => oncommand('copy', entry)}
				><i class="no-icon" aria-hidden="true"></i><span>{t('files.copy')}</span></button
			>
			{#if writable}<button type="button" role="menuitem" onclick={() => oncommand('move', entry)}
					><i class="no-icon" aria-hidden="true"></i><span>{t('files.move')}</span></button
				>{/if}
		{/if}
		{#if writable}
			{#if one}<button type="button" role="menuitem" onclick={() => oncommand('rename', entry)}
					><i class="no-icon" aria-hidden="true"></i><span>{t('files.rename')}</span><kbd
						aria-hidden="true">F2</kbd
					></button
				>
			{:else}<button type="button" role="menuitem" onclick={() => oncommand('renameBatch', entry)}
					><i class="no-icon" aria-hidden="true"></i><span>{t('files.renameBatch')}</span></button
				>{/if}
		{/if}
		{#if one}<hr />
			<button type="button" role="menuitem" onclick={() => oncommand('properties', entry)}
				><i class="no-icon" aria-hidden="true"></i><span>{t('files.properties')}</span></button
			>{/if}
		{#if writable}<hr />
			<button
				type="button"
				role="menuitem"
				class="danger"
				onclick={() => oncommand('delete', entry)}
				><Icon name="trash" size={16} /><span>{t('files.delete')}</span><kbd aria-hidden="true"
					>Entf</kbd
				></button
			>{/if}
	</ContextMenu>
{/if}

{#if background && root}
	<ContextMenu label={t('files.folderActions')} x={background.x} y={background.y} {onclose}>
		{#if writable}
			<button type="button" role="menuitem" onclick={() => oncommand('mkdir')}
				><Icon name="folder" size={16} /><span>{t('files.newFolder')}</span></button
			>
			<button type="button" role="menuitem" onclick={() => oncommand('newFile')}
				><Icon name="fileText" size={16} /><span>{t('files.newFile')}</span></button
			>
			<button type="button" role="menuitem" onclick={() => oncommand('upload')}
				><Icon name="upload" size={16} /><span>{t('files.upload')}</span></button
			>
			<button type="button" role="menuitem" onclick={() => oncommand('urlDownload')}
				><Icon name="globe" size={16} /><span>{t('files.urlDownload')}</span></button
			>
			<button type="button" role="menuitem" disabled={!canPaste} onclick={() => oncommand('paste')}
				><Icon name="paste" size={16} /><span>{t('files.paste')}</span><kbd aria-hidden="true"
					>Ctrl+V</kbd
				></button
			>
			<hr />
		{/if}
		{#each views as option (option.mode)}
			<button
				type="button"
				role="menuitemradio"
				aria-checked={view === option.mode}
				onclick={() => oncommand(option.mode)}
				><Icon name={option.icon} size={16} /><span>{t(option.label)}</span></button
			>
		{/each}
	</ContextMenu>
{/if}

<style>
	.no-icon {
		width: 16px;
		flex: none;
	}
</style>
