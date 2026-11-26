<script lang="ts">
	import Dropdown from '#lib/components/Dropdown.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { hasTrash, type FileCommand } from './commands.ts';
	import type { FilesPage } from './files-page.svelte.ts';
	import FolderLayoutOptions from './FolderLayoutOptions.svelte';
	import { currentPlace } from './mobile-views.svelte.ts';
	let { p }: { p: FilesPage } = $props();
	const place = $derived(currentPlace(p.rootId, p.dir));
	const views = $derived(p.prefs.mobile);
	function run(close: () => void, action: FileCommand) {
		close();
		p.command(action);
	}
</script>

<div class="folder-head">
	<div class="top">
		<button
			type="button"
			class="round"
			aria-label={t('files.back')}
			disabled={!p.steps.back.length && !p.dir}
			onclick={() => (p.steps.back.length ? p.historyGo('back') : p.command('up'))}
			><Icon name="chevronLeft" /></button
		>
		<button
			type="button"
			class="location"
			aria-expanded={p.placesOpen}
			aria-controls="file-places"
			onclick={() => (p.placesOpen = !p.placesOpen)}
		>
			<Icon name="folder" size={16} /><span
				>{[p.root?.name, ...p.dir.split('/').slice(0, -1)].filter(Boolean).join(' / ') ||
					t('files.places')}</span
			><Icon name="chevronDown" size={15} />
		</button>
		<Dropdown label={t('files.moreActions')} icon="menu" align="right">
			{#snippet children(close)}
				<a role="menuitem" href="/settings/files#folder-views" onclick={close}
					><Icon name="gear" size={18} />{t('files.mobile.settings')}</a
				>
				<button
					role="menuitem"
					onclick={() => {
						close();
						p.choosingFiles = !p.choosingFiles;
						p.selected = [];
					}}><Icon name="select" size={18} />{t('files.mobile.select')}</button
				>
				<button role="menuitem" onclick={() => run(close, 'selectAll')}
					>{t('files.selectAll')}</button
				>
				<button role="menuitem" disabled={!p.dir} onclick={() => run(close, 'up')}
					>{t('files.up')}</button
				>
				<button
					role="menuitem"
					disabled={!p.steps.forward.length}
					onclick={() => run(close, 'forward')}>{t('files.forward')}</button
				>
				<button role="menuitem" onclick={() => run(close, 'editPath')}>{t('files.editPath')}</button
				>
				{#if p.root?.kind !== 'stick'}<button role="menuitem" onclick={() => run(close, 'favorite')}
						>{t(p.prefs.isFavorite ? 'files.unfavorite' : 'files.favorite')}</button
					>{/if}
				{#if p.clipboard.content}<button
						role="menuitem"
						disabled={!p.canPaste}
						onclick={() => run(close, 'paste')}>{t('files.paste')}</button
					>{/if}
				<button role="menuitem" onclick={() => run(close, 'photos')}>{t('files.photoView')}</button>
				{#each ['name', 'modified', 'size'] as const as column}
					<button
						role="menuitem"
						disabled={!p.prefs.ready}
						onclick={() => {
							close();
							p.prefs.sortBy(column);
						}}
					>
						<Icon name="sort" size={16} />{t('files.sort')} · {t(`files.${column}`)}
						{#if p.prefs.sort === column}<span aria-hidden="true"
								>{p.prefs.descending ? '↓' : '↑'}</span
							>{/if}
					</button>
				{/each}
				<button role="menuitem" onclick={() => run(close, 'deepSearch')}
					>{t('files.deepSearch')}</button
				>
				{#if hasTrash(p.root)}<button role="menuitem" onclick={() => run(close, 'trash')}
						>{t('files.trash')}</button
					>{/if}
				{#if p.root?.kind !== 'stick'}<button role="menuitem" onclick={() => run(close, 'usage')}
						>{t('files.usageTitle')}</button
					>
					<button role="menuitem" onclick={() => run(close, 'duplicates')}
						>{t('files.duplicatesTitle')}</button
					>{/if}
				<button
					role="menuitemcheckbox"
					aria-checked={p.prefs.split}
					onclick={() => run(close, 'twoPanes')}>{t('files.twoPanes')}</button
				>
				{#if p.prefs.split}<button
						role="menuitem"
						disabled={!p.rightRoot}
						onclick={() => run(close, 'compare')}>{t('files.compareFolders')}</button
					>{/if}
				<button
					role="menuitemcheckbox"
					aria-checked={p.prefs.showHidden}
					onclick={() => run(close, 'showHidden')}>{t('files.showHidden')}</button
				>
				<button role="menuitem" onclick={() => run(close, 'shortcuts')}
					>{t('files.shortcuts')}</button
				>
			{/snippet}
		</Dropdown>
	</div>
	{#if !p.trash && !p.usage}
		<h2>{p.dir.split('/').at(-1) || p.root?.name || t('files.places')}</h2>
		<label class="search"
			><Icon name="search" size={18} /><input
				type="search"
				bind:value={p.search}
				aria-label={t('files.searchFolder')}
				placeholder={t('files.searchFolder')}
				oninput={() => {
					p.selected = [];
					p.anchor = '';
				}}
			/></label
		>
		<div class="content-bar">
			<h3>{t('files.mobile.content')}</h3>
			{#if p.choosingFiles || p.selected.length}<button
					class="btn"
					onclick={() => {
						p.choosingFiles = false;
						p.selected = [];
					}}>{t('common.done')}</button
				>{/if}
			{#if p.writable}<Dropdown label={t('files.new')} icon="plus" align="right">
					{#snippet children(close)}
						<button role="menuitem" onclick={() => run(close, 'mkdir')}
							><Icon name="folder" size={18} />{t('files.newFolder')}</button
						>
						<button role="menuitem" onclick={() => run(close, 'newFile')}
							><Icon name="fileText" size={18} />{t('files.newFile')}</button
						>
						<button role="menuitem" onclick={() => run(close, 'upload')}
							><Icon name="upload" size={18} />{t('files.mobileUpload')}</button
						>
						<button role="menuitem" onclick={() => run(close, 'uploadFolder')}
							>{t('files.uploadFolder')}</button
						>
						<button role="menuitem" onclick={() => run(close, 'urlDownload')}
							>{t('files.urlDownload')}</button
						>
					{/snippet}
				</Dropdown>{/if}
			<Dropdown label={t('files.mobile.folderOptions')} icon="more" align="right">
				{#snippet children(close)}<FolderLayoutOptions {views} {place} {close} />{/snippet}
			</Dropdown>
		</div>
		{#if p.selected.length}
			<div class="selection-bar" aria-label={t('files.selectionActions')} role="region">
				<span>{t('files.selected', { count: p.selected.length })}</span>
				<button class="btn" aria-label={t('files.copy')} onclick={() => p.command('copy')}
					><Icon name="copy" size={17} /></button
				>
				<button
					class="btn"
					aria-label={t('files.selectionMore')}
					onclick={(event) => {
						const last = p.selectedEntries.at(-1);
						if (last) p.menuFromButton(last, event.currentTarget);
					}}><Icon name="more" size={17} /></button
				>
			</div>
		{/if}
		{#if views.error}<p class="notice-error" role="alert">{views.error}</p>{/if}
		{#if views.success}<p class="notice-ok" role="status">{views.success}</p>{/if}
	{/if}
</div>

<style>
	.selection-bar {
		display: flex;
		gap: 0.35rem;
		align-items: center;
		margin-top: 0.45rem;
	}
	.selection-bar span {
		flex: 1;
		font-size: 0.9rem;
	}
	.selection-bar button {
		min-width: 44px;
		min-height: 44px;
	}
	.folder-head {
		min-width: 0;
	}
	.top,
	.content-bar,
	.location,
	.search {
		display: flex;
		align-items: center;
		gap: 0.5rem;
	}
	.location {
		min-height: 44px;
		flex: 1;
		min-width: 0;
		padding: 0 0.35rem;
		justify-content: center;
		color: var(--muted);
		background: none;
		border: 0;
	}
	.location span {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.round,
	.folder-head :global(.dropdown > .btn) {
		display: grid;
		place-items: center;
		width: 44px;
		height: 44px;
		padding: 0;
		border-radius: 50%;
		color: var(--text);
		border: 1px solid var(--nord-line, var(--panel-border));
		background: var(--nord-surface, var(--panel-bg));
		-webkit-backdrop-filter: var(--nord-blur, var(--panel-backdrop));
		backdrop-filter: var(--nord-blur, var(--panel-backdrop));
	}
	.round:disabled {
		opacity: 0.45;
	}
	h2 {
		margin: 0.8rem 0 0.7rem;
		font-size: clamp(1.65rem, 7.5vw, 2.35rem);
		line-height: 1.12;
		letter-spacing: -0.04em;
		overflow-wrap: anywhere;
	}
	.search {
		min-height: 44px;
		padding: 0 0.8rem;
		border: 1px solid var(--nord-line, var(--panel-border));
		border-radius: 14px;
		background: var(--nord-surface, var(--panel-bg));
		color: var(--muted);
		-webkit-backdrop-filter: var(--nord-blur, var(--panel-backdrop));
		backdrop-filter: var(--nord-blur, var(--panel-backdrop));
	}
	input {
		flex: 1;
		min-width: 0;
		background: transparent;
		border: 0;
		color: var(--text);
		font: inherit;
	}
	.search:focus-within {
		outline: 2px solid var(--accent);
	}
	input:focus {
		outline: 0;
	}
	.content-bar {
		margin-top: 0.8rem;
	}
	h3 {
		flex: 1;
		margin: 0;
		font-size: 1.15rem;
	}
	.folder-head :global(.dropdown-panel :is(a, button)) {
		min-height: 44px;
	}
</style>
