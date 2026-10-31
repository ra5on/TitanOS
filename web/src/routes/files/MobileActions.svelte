<script lang="ts">
	import { tick } from 'svelte';
	import type { FileRoot } from '#lib/api.ts';
	import Dropdown from '#lib/components/Dropdown.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { views, type FileCommand } from './commands.ts';

	type Props = {
		root: FileRoot | undefined;
		dir: string;
		writable: boolean;
		canForward: boolean;
		isFavorite: boolean;
		/** Something was copied or cut. */
		clipboard: boolean;
		canPaste: boolean;
		split: boolean;
		canCompare: boolean;
		preferencesReady: boolean;
		folderViewSaved: boolean;
		folderViewsFull: boolean;
		selected: number;
		search: string;
		/** The search field is shown. */
		searchOpen: boolean;
		oncommand: (command: FileCommand) => void;
		/** The search text changed. */
		onsearch: () => void;
		/** The "more" button opens the menu of the selection. */
		onmore: (button: HTMLElement) => void;
	};
	let {
		root,
		dir,
		writable,
		canForward,
		isFavorite,
		clipboard,
		canPaste,
		split,
		canCompare,
		preferencesReady,
		folderViewSaved,
		folderViewsFull,
		selected,
		search = $bindable(),
		searchOpen = $bindable(),
		oncommand,
		onsearch,
		onmore,
	}: Props = $props();

	let mobileSearchField: HTMLInputElement | undefined = $state();
	async function toggleMobileSearch() {
		searchOpen = !searchOpen;
		if (searchOpen) {
			await tick();
			mobileSearchField?.focus();
		} else search = '';
	}
	/** Closes the menu, then runs the command. */
	const run = (close: () => void, command: FileCommand) => {
		close();
		oncommand(command);
	};
</script>

<!-- Phones: the bars of the list are hidden, these take their place. -->
<div class="mobile-file-actions" role="toolbar" aria-label={t('files.moreActions')}>
	<button
		type="button"
		class="btn btn-primary"
		disabled={!writable}
		onclick={() => oncommand('upload')}
		><Icon name="upload" size={16} /><span>{t('files.mobileUpload')}</span></button
	>
	<Dropdown label={t('files.new')} text={t('files.new')} icon="plus" disabled={!writable}
		>{#snippet children(close)}<button
				type="button"
				role="menuitem"
				onclick={() => run(close, 'mkdir')}
				><Icon name="folder" size={16} />{t('files.newFolder')}</button
			><button type="button" role="menuitem" onclick={() => run(close, 'newFile')}
				><Icon name="fileText" size={16} />{t('files.newFile')}</button
			><button type="button" role="menuitem" onclick={() => run(close, 'uploadFolder')}
				><Icon name="upload" size={16} />{t('files.uploadFolder')}</button
			><button type="button" role="menuitem" onclick={() => run(close, 'urlDownload')}
				><Icon name="globe" size={16} />{t('files.urlDownload')}</button
			>{/snippet}</Dropdown
	>
	<button
		type="button"
		class="btn"
		aria-expanded={searchOpen}
		aria-controls="mobile-file-search"
		onclick={toggleMobileSearch}
		><Icon name="search" size={16} /><span>{t('files.mobileSearch')}</span></button
	>
	<Dropdown label={t('files.moreActions')} text={t('files.mobileMore')} icon="more" align="right"
		>{#snippet children(close)}
			<button type="button" role="menuitem" disabled={!dir} onclick={() => run(close, 'up')}
				><Icon name="arrowUp" size={16} />{t('files.up')}</button
			>
			<button
				type="button"
				role="menuitem"
				disabled={!canForward}
				onclick={() => run(close, 'forward')}
				><Icon name="arrowRight" size={16} />{t('files.forward')}</button
			>
			<button type="button" role="menuitem" onclick={() => run(close, 'editPath')}
				><Icon name="edit" size={16} />{t('files.editPath')}</button
			>
			{#if root?.kind !== 'stick'}<button
					type="button"
					role="menuitem"
					onclick={() => run(close, 'favorite')}
					><Icon name="star" size={16} />{t(
						isFavorite ? 'files.unfavorite' : 'files.favorite',
					)}</button
				>{/if}
			{#if clipboard}<button
					type="button"
					role="menuitem"
					disabled={!canPaste}
					onclick={() => run(close, 'paste')}
					><Icon name="paste" size={16} />{t('files.paste')}</button
				>{/if}
			<hr />
			<button type="button" role="menuitem" onclick={() => run(close, 'selectAll')}
				><Icon name="select" size={16} />{t('files.selectAll')}</button
			>
			<button type="button" role="menuitem" onclick={() => run(close, 'deepSearch')}
				><Icon name="search" size={16} />{t('files.deepSearch')}</button
			>
			{#if root?.kind !== 'stick'}<button
					type="button"
					role="menuitem"
					onclick={() => run(close, 'usage')}>{t('files.usageTitle')}</button
				><button type="button" role="menuitem" onclick={() => run(close, 'duplicates')}
					>{t('files.duplicatesTitle')}</button
				>{/if}
			{#if root?.kind === 'share'}<button
					type="button"
					role="menuitem"
					onclick={() => run(close, 'trash')}
					><Icon name="trash" size={16} />{t('files.trash')}</button
				>{/if}
			<hr />
			{#each views as option}<button
					type="button"
					role="menuitem"
					disabled={!preferencesReady}
					onclick={() => run(close, option.mode)}
					><Icon name={option.icon} size={16} />{t(option.label)}</button
				>{/each}
			<button type="button" role="menuitem" onclick={() => run(close, 'twoPanes')}
				><Icon name="columns" size={16} />{t('files.twoPanes')}</button
			>
			{#if split}<button
					type="button"
					role="menuitem"
					disabled={!canCompare}
					onclick={() => run(close, 'compare')}>{t('files.compareFolders')}</button
				>{/if}
			<button type="button" role="menuitem" onclick={() => run(close, 'showHidden')}
				><Icon name="eye" size={16} />{t('files.showHidden')}</button
			>
			{#if root?.kind !== 'stick'}<button
					type="button"
					role="menuitem"
					disabled={!preferencesReady || (!folderViewSaved && folderViewsFull)}
					onclick={() => run(close, 'folderView')}>{t('files.rememberFolderView')}</button
				>{/if}
			<button type="button" role="menuitem" onclick={() => run(close, 'shortcuts')}
				><Icon name="keyboard" size={16} />{t('files.shortcuts')}</button
			>
		{/snippet}</Dropdown
	>
</div>
{#if searchOpen}<label class="mobile-file-search" id="mobile-file-search"
		><span class="sr-only">{t('files.searchFolder')}</span><Icon name="search" size={16} /><input
			class="field"
			type="search"
			bind:this={mobileSearchField}
			bind:value={search}
			placeholder={t('files.searchFolder')}
			oninput={onsearch}
		/><button
			class="btn icon"
			type="button"
			aria-label={t('common.close')}
			onclick={toggleMobileSearch}><Icon name="close" size={16} /></button
		></label
	>{/if}
{#if selected}<div class="mobile-selection" role="region" aria-label={t('files.selectionActions')}>
		<span>{t('files.selected', { count: selected })}</span><button
			type="button"
			class="btn icon"
			aria-label={t('files.copy')}
			onclick={() => oncommand('copy')}><Icon name="copy" size={16} /></button
		><button
			type="button"
			class="btn icon"
			aria-label={t('files.selectionMore')}
			onclick={(event) => onmore(event.currentTarget)}><Icon name="more" size={16} /></button
		><button
			type="button"
			class="btn icon"
			aria-label={t('files.clearSelection')}
			onclick={() => oncommand('clearSelection')}><Icon name="close" size={16} /></button
		>
	</div>{/if}

<style>
	.btn.icon {
		width: 2.375rem;
		padding: 0;
		justify-content: center;
	}
	.mobile-file-actions,
	.mobile-file-search,
	.mobile-selection {
		display: none;
	}
	@media (max-width: 760px) {
		.btn.icon {
			height: 2.75rem;
			width: 2.75rem;
		}
		.mobile-file-actions {
			display: grid;
			grid-template-columns: repeat(4, minmax(0, 1fr));
			gap: 6px;
			min-width: 0;
		}
		.mobile-file-actions > .btn {
			min-width: 0;
			padding: 0 6px;
			font-size: var(--text-base);
			gap: 5px;
			height: 44px;
		}
		.mobile-file-actions :global(.dropdown) {
			min-width: 0;
			width: 100%;
		}
		.mobile-file-actions :global(.dropdown > button) {
			width: 100%;
			height: 44px;
			padding: 0 5px;
			gap: 4px;
			font-size: var(--text-base);
		}
		.mobile-file-actions :global(.chevron) {
			display: none;
		}
		.mobile-file-actions :global(.dropdown-panel) {
			max-width: calc(100vw - 32px);
			max-height: 65vh;
			overflow-y: auto;
		}
		.mobile-file-actions :global(.dropdown-panel button) {
			min-height: 44px;
		}
		.mobile-file-search {
			display: flex;
			gap: 8px;
			align-items: center;
			min-width: 0;
		}
		.mobile-file-search .field {
			flex: 1;
			min-width: 0;
			height: 44px;
		}
		.mobile-selection {
			display: flex;
			align-items: center;
			gap: 6px;
		}
		.mobile-selection > span {
			flex: 1;
			font-size: var(--text-base);
		}
		.mobile-selection .btn {
			width: 44px;
			height: 44px;
			padding: 0;
		}
	}
</style>
