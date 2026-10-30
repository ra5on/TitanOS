<script lang="ts">
	import type { FileRoot } from '#lib/api.ts';
	import Dropdown from '#lib/components/Dropdown.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import type { FileView } from '#lib/file-actions.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { views, type FileCommand } from './commands.ts';

	type Props = {
		root: FileRoot | undefined;
		writable: boolean;
		/** Something was copied or cut. */
		clipboard: boolean;
		canPaste: boolean;
		selected: number;
		split: boolean;
		/** The other pane shows a folder to compare with. */
		canCompare: boolean;
		view: FileView;
		preferencesReady: boolean;
		showHidden: boolean;
		folderViewSaved: boolean;
		/** No more folder views can be remembered. */
		folderViewsFull: boolean;
		selectionFilter: string;
		fileInput?: HTMLInputElement;
		folderInput?: HTMLInputElement;
		onpicked: (input: HTMLInputElement) => void;
		oncommand: (command: FileCommand) => void;
	};
	let {
		root,
		writable,
		clipboard,
		canPaste,
		selected,
		split,
		canCompare,
		view,
		preferencesReady,
		showHidden,
		folderViewSaved,
		folderViewsFull,
		selectionFilter = $bindable(),
		fileInput = $bindable(),
		folderInput = $bindable(),
		onpicked,
		oncommand,
	}: Props = $props();

	/** Closes the menu, then runs the command. */
	const run = (close: () => void, command: FileCommand) => {
		close();
		oncommand(command);
	};
</script>

<!-- What: creating, choosing, viewing. -->
<div class="toolbar action-bar">
	{#if writable}
		<button type="button" class="btn btn-primary" onclick={() => oncommand('upload')}
			><Icon name="upload" size={16} />{t('files.upload')}</button
		>
		<Dropdown label={t('files.new')} text={t('files.new')} icon="plus">
			{#snippet children(close)}
				<button type="button" role="menuitem" onclick={() => run(close, 'mkdir')}
					><Icon name="folder" size={16} /><span>{t('files.newFolder')}</span></button
				>
				<button type="button" role="menuitem" onclick={() => run(close, 'newFile')}
					><Icon name="fileText" size={16} /><span>{t('files.newFile')}</span></button
				>
				<hr />
				<button type="button" role="menuitem" onclick={() => run(close, 'uploadFolder')}
					><Icon name="upload" size={16} /><span>{t('files.uploadFolder')}</span></button
				>
				<button type="button" role="menuitem" onclick={() => run(close, 'urlDownload')}
					><Icon name="globe" size={16} /><span>{t('files.urlDownload')}</span></button
				>
			{/snippet}
		</Dropdown>
		<input
			bind:this={fileInput}
			type="file"
			multiple
			hidden
			onchange={(event) => onpicked(event.currentTarget)}
		/>
		<input
			bind:this={folderInput}
			type="file"
			webkitdirectory
			hidden
			onchange={(event) => onpicked(event.currentTarget)}
		/>
	{/if}
	{#if clipboard}
		<button type="button" class="btn" disabled={!canPaste} onclick={() => oncommand('paste')}
			><Icon name="paste" size={16} />{t('files.paste')}</button
		>
	{/if}
	<span class="spacer"></span>
	{#if root}
		<Dropdown
			label={t('files.selection')}
			text={t('files.selection')}
			icon="select"
			kind="panel"
			align="right"
			compact
		>
			{#snippet children()}
				<button class="btn" onclick={() => oncommand('selectAll')}>{t('files.selectAll')}</button>
				<button class="btn" disabled={!selected} onclick={() => oncommand('clearSelection')}
					>{t('files.clearSelection')}</button
				>
				<button class="btn" onclick={() => oncommand('invertSelection')}
					>{t('files.invertSelection')}</button
				>
				<hr />
				<label class="stack"
					>{t('files.selectType')}
					<select class="field" bind:value={selectionFilter}>
						{#each ['images', 'video', 'audio', 'documents', 'folders', 'other'] as type (type)}<option
								value={type}
								>{t(`files.selectionType.${type}` as 'files.selectionType.images')}</option
							>{/each}
					</select>
				</label>
				<button class="btn" onclick={() => oncommand('selectType')}
					>{t('files.selectMatches')}</button
				>
			{/snippet}
		</Dropdown>
		<Dropdown label={t('files.tools')} text={t('files.tools')} icon="tool" align="right" compact>
			{#snippet children(close)}
				<button type="button" role="menuitem" onclick={() => run(close, 'deepSearch')}
					><span>{t('files.deepSearch')}</span></button
				>
				{#if root?.kind !== 'stick'}
					<button type="button" role="menuitem" onclick={() => run(close, 'usage')}
						><span>{t('files.usageTitle')}</span></button
					>
					<button type="button" role="menuitem" onclick={() => run(close, 'duplicates')}
						><span>{t('files.duplicatesTitle')}</span></button
					>
				{/if}
				{#if split}<button
						type="button"
						role="menuitem"
						disabled={!canCompare}
						onclick={() => run(close, 'compare')}><span>{t('files.compareFolders')}</span></button
					>{/if}
			{/snippet}
		</Dropdown>
		{#if root.kind === 'share'}
			<button
				type="button"
				class="btn"
				aria-label={t('files.trash')}
				title={t('files.trash')}
				onclick={() => oncommand('trash')}
				><Icon name="trash" size={16} /><span class="label">{t('files.trash')}</span></button
			>
		{/if}
	{/if}
	<div class="segmented" role="group" aria-label={t('files.view')}>
		{#each views as option (option.mode)}
			<button
				type="button"
				class="btn icon"
				disabled={!preferencesReady}
				aria-label={t(option.label)}
				title={option.mode === 'large'
					? `${t(option.label)} – ${t('files.thumbnailHint')}`
					: t(option.label)}
				aria-pressed={view === option.mode}
				onclick={() => oncommand(option.mode)}><Icon name={option.icon} size={16} /></button
			>
		{/each}
	</div>
	<button
		type="button"
		class="btn icon"
		aria-label={t('files.twoPanes')}
		title={t('files.twoPanes')}
		aria-pressed={split}
		onclick={() => oncommand('twoPanes')}><Icon name="columns" size={16} /></button
	>
	<Dropdown
		label={t('files.view')}
		text={t('files.view')}
		icon="eye"
		kind="panel"
		align="right"
		compact
	>
		{#snippet children(close)}
			<label
				><input type="checkbox" checked={showHidden} onchange={() => oncommand('showHidden')} />{t(
					'files.showHidden',
				)}</label
			>
			{#if root?.kind !== 'stick'}<label
					><input
						type="checkbox"
						checked={folderViewSaved}
						disabled={!preferencesReady || (!folderViewSaved && folderViewsFull)}
						onchange={() => oncommand('folderView')}
					/>{t('files.rememberFolderView')}</label
				>{/if}
			{#if root?.kind !== 'stick'}<button class="btn" onclick={() => run(close, 'photos')}
					><Icon name="grid" size={16} /> {t('files.photoView')}</button
				>{/if}
			<hr />
			<button class="btn" onclick={() => run(close, 'shortcuts')}
				>{t('files.shortcuts')} <kbd aria-hidden="true">F1</kbd></button
			>
		{/snippet}
	</Dropdown>
</div>

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
	.btn.icon {
		width: 2.375rem;
		padding: 0;
		justify-content: center;
	}
	.btn[aria-pressed='true'] {
		border-color: var(--accent);
		color: var(--accent);
	}
	.segmented {
		display: flex;
		gap: 4px;
	}
	.spacer {
		flex: 1 1 auto;
	}

	@media (max-width: 1180px) {
		.action-bar .label {
			display: none;
		}
	}
	@media (max-width: 760px) {
		.toolbar .btn,
		.btn.icon {
			height: 2.75rem;
		}
		.btn.icon {
			width: 2.75rem;
		}
		.spacer {
			display: none;
		}
		/* Phones have their own bar for these. */
		.action-bar {
			display: none;
		}
	}
</style>
