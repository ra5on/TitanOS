<script lang="ts">
	// PLAN 73 (drafts 16–18, owner 8.11.2026): the command bar of the file
	// manager in a window, like Explorer. Left: Neu ▾, Hochladen, then only
	// Kopieren, Einfügen, Löschen as symbols and "…" for the rest; right:
	// Auswahl, Sortieren, Ansicht and the second pane. The bar measures
	// itself and never scrolls or cuts anything off: first the labels go
	// (the symbols keep their tooltip), then Auswahl and Sortieren move into
	// "…". No fixed widths. The number of selected entries stands only in the
	// status line.
	import { tick } from 'svelte';
	import type { FileRoot } from '#lib/api.ts';
	import Dropdown from '#lib/components/Dropdown.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import type { FileView } from '#lib/file-actions.ts';
	import type { FileSort } from '#lib/file-list.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { hasTrash, views, type FileCommand } from './commands.ts';

	type Props = {
		root: FileRoot | undefined;
		writable: boolean;
		canPaste: boolean;
		selected: number;
		split: boolean;
		canCompare: boolean;
		view: FileView;
		sort: FileSort;
		descending: boolean;
		preferencesReady: boolean;
		showHidden: boolean;
		folderViewSaved: boolean;
		folderViewsFull: boolean;
		selectionFilter: string;
		fileInput?: HTMLInputElement;
		folderInput?: HTMLInputElement;
		onpicked: (input: HTMLInputElement) => void;
		oncommand: (command: FileCommand) => void;
		onsort: (column: FileSort) => void;
	};
	let {
		root,
		writable,
		canPaste,
		selected,
		split,
		canCompare,
		view,
		sort,
		descending,
		preferencesReady,
		showHidden,
		folderViewSaved,
		folderViewsFull,
		selectionFilter = $bindable(),
		fileInput = $bindable(),
		folderInput = $bindable(),
		onpicked,
		oncommand,
		onsort,
	}: Props = $props();

	const run = (close: () => void, command: FileCommand) => {
		close();
		oncommand(command);
	};
	const SORTS: { column: FileSort; label: 'files.name' | 'files.size' | 'files.modified' }[] = [
		{ column: 'name', label: 'files.name' },
		{ column: 'modified', label: 'files.modified' },
		{ column: 'size', label: 'files.size' },
	];

	// 0: everything labelled; 1: symbols only; 2: also Auswahl and Sortieren in "…".
	let stage = $state(0);
	let bar = $state<HTMLElement>();
	// Measured after each step is drawn (tick): labels and the two menus come and go with the stage.
	let fitting = false;
	async function fit() {
		if (!bar || fitting) return;
		fitting = true;
		try {
			for (const next of [0, 1, 2]) {
				stage = next;
				await tick();
				if (!bar || bar.scrollWidth <= bar.clientWidth + 1) break;
			}
		} finally {
			fitting = false;
		}
	}
	$effect(() => {
		if (!bar || typeof ResizeObserver === 'undefined') return;
		let frame = 0;
		const later = () => {
			cancelAnimationFrame(frame);
			frame = requestAnimationFrame(() => void fit());
		};
		const observer = new ResizeObserver(later);
		observer.observe(bar);
		later();
		return () => {
			cancelAnimationFrame(frame);
			observer.disconnect();
		};
	});
	// What the bar shows changes with the folder and the rights: measure again.
	$effect(() => {
		void writable;
		void root;
		void split;
		requestAnimationFrame(() => void fit());
	});
	const one = $derived(selected === 1);
</script>

<div
	class="command-bar"
	bind:this={bar}
	data-stage={stage}
	role="toolbar"
	aria-label={t('files.commands')}
>
	{#if writable}
		<Dropdown label={t('files.new')} text={stage === 0 ? t('files.new') : undefined} icon="plus">
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
		<button
			type="button"
			class="cmd"
			title={t('files.upload')}
			aria-label={t('files.upload')}
			onclick={() => oncommand('upload')}
			><Icon name="upload" size={16} /><span class="label">{t('files.upload')}</span></button
		>
		<input
			bind:this={fileInput}
			type="file"
			multiple
			hidden
			onchange={(e) => onpicked(e.currentTarget)}
		/>
		<input
			bind:this={folderInput}
			type="file"
			webkitdirectory
			hidden
			onchange={(e) => onpicked(e.currentTarget)}
		/>
		<span class="sep" aria-hidden="true"></span>
	{/if}
	<button
		type="button"
		class="cmd icon"
		title={t('files.clipboardCopy')}
		aria-label={t('files.clipboardCopy')}
		disabled={!selected}
		onclick={() => oncommand('clipboardCopy')}><Icon name="copy" size={16} /></button
	>
	<button
		type="button"
		class="cmd icon"
		title={t('files.paste')}
		aria-label={t('files.paste')}
		disabled={!canPaste}
		onclick={() => oncommand('paste')}><Icon name="paste" size={16} /></button
	>
	<button
		type="button"
		class="cmd icon"
		title={t('files.delete')}
		aria-label={t('files.delete')}
		disabled={!selected || !writable}
		onclick={() => oncommand('delete')}><Icon name="trash" size={16} /></button
	>
	<span class="sep" aria-hidden="true"></span>
	<Dropdown label={t('ui.more')} icon="more">
		{#snippet children(close)}
			<button
				type="button"
				role="menuitem"
				disabled={!selected || !writable}
				onclick={() => run(close, 'cut')}
				><Icon name="cut" size={16} /><span>{t('files.cut')}</span><kbd>Strg+X</kbd></button
			>
			<button
				type="button"
				role="menuitem"
				disabled={!one || !writable}
				onclick={() => run(close, 'rename')}
				><Icon name="edit" size={16} /><span>{t('files.rename')}</span><kbd>F2</kbd></button
			>
			<hr />
			{#if stage >= 2}
				<button type="button" role="menuitem" onclick={() => run(close, 'selectAll')}
					><Icon name="select" size={16} /><span>{t('files.selectAll')}</span></button
				>
				<button
					type="button"
					role="menuitem"
					disabled={!selected}
					onclick={() => run(close, 'clearSelection')}
					><span>{t('files.clearSelection')}</span></button
				>
				{#each SORTS as s (s.column)}
					<button
						type="button"
						role="menuitemradio"
						aria-checked={sort === s.column}
						disabled={!preferencesReady}
						onclick={() => {
							close();
							onsort(s.column);
						}}
						><Icon name="sort" size={16} /><span>{t('files.sortBy', { column: t(s.label) })}</span
						></button
					>
				{/each}
				<hr />
			{/if}
			<button
				type="button"
				role="menuitem"
				disabled={!selected}
				onclick={() => run(close, 'archive')}><span>{t('files.archiveTitle')}</span></button
			>
			<button type="button" role="menuitem" disabled={!selected} onclick={() => run(close, 'copy')}
				><span>{t('files.copy')}</span></button
			>
			<button
				type="button"
				role="menuitem"
				disabled={!selected || !writable}
				onclick={() => run(close, 'move')}><span>{t('files.move')}</span></button
			>
			<button type="button" role="menuitem" disabled={!one} onclick={() => run(close, 'properties')}
				><span>{t('files.properties')}</span></button
			>
			<hr />
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
			{#if hasTrash(root)}<button type="button" role="menuitem" onclick={() => run(close, 'trash')}
					><Icon name="trash" size={16} /><span>{t('files.trash')}</span></button
				>{/if}
		{/snippet}
	</Dropdown>

	<span class="spacer"></span>

	{#if root && stage < 2}
		<Dropdown
			label={t('files.selection')}
			text={stage === 0 ? t('files.selection') : undefined}
			icon="select"
			kind="panel"
			align="right"
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
		<Dropdown
			label={t('files.sort')}
			text={stage === 0 ? t('files.sort') : undefined}
			icon="sort"
			align="right"
		>
			{#snippet children(close)}
				{#each SORTS as s (s.column)}
					<button
						type="button"
						role="menuitemradio"
						aria-checked={sort === s.column}
						disabled={!preferencesReady}
						onclick={() => {
							close();
							onsort(s.column);
						}}
						><span>{t(s.label)}</span>{#if sort === s.column}<span class="muted"
								>{descending ? '↓' : '↑'}</span
							>{/if}</button
					>
				{/each}
			{/snippet}
		</Dropdown>
	{/if}
	<Dropdown
		label={t('files.view')}
		text={stage === 0 ? t('files.view') : undefined}
		icon="list"
		kind="panel"
		align="right"
	>
		{#snippet children(close)}
			<div class="segmented" role="group" aria-label={t('files.view')}>
				{#each views as option (option.mode)}
					<button
						type="button"
						class="btn"
						disabled={!preferencesReady}
						aria-pressed={view === option.mode}
						onclick={() => oncommand(option.mode)}
						><Icon name={option.icon} size={16} />{t(option.label)}</button
					>
				{/each}
			</div>
			<hr />
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
				>
				<button class="btn" onclick={() => run(close, 'photos')}
					><Icon name="grid" size={16} /> {t('files.photoView')}</button
				>{/if}
			<hr />
			<button class="btn" onclick={() => run(close, 'shortcuts')}
				>{t('files.shortcuts')} <kbd aria-hidden="true">F1</kbd></button
			>
		{/snippet}
	</Dropdown>
	<button
		type="button"
		class="cmd icon"
		aria-label={t('files.twoPanes')}
		title={t('files.twoPanes')}
		aria-pressed={split}
		onclick={() => oncommand('twoPanes')}><Icon name="columns" size={16} /></button
	>
</div>

<style>
	.command-bar {
		display: flex;
		flex-wrap: nowrap;
		align-items: center;
		gap: 4px;
		padding: 6px 12px;
		border-bottom: 1px solid var(--panel-border);
		overflow: hidden;
		min-width: 0;
	}
	/* Quiet buttons as in Explorer: no frame until hovered. */
	.command-bar :global(:is(.cmd, .dropdown > button, .trigger)) {
		height: 2rem;
		border-color: transparent;
		background: transparent;
	}
	.cmd {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		padding: 0 10px;
		border: 1px solid transparent;
		border-radius: var(--radius-ctl);
		background: transparent;
		color: var(--text);
		font: inherit;
		font-size: var(--text-base);
		cursor: pointer;
		white-space: nowrap;
		flex: none;
	}
	.cmd.icon {
		width: 2rem;
		padding: 0;
		justify-content: center;
	}
	.cmd:hover:not(:disabled) {
		background: var(--chip);
	}
	.cmd:disabled {
		opacity: 0.4;
		cursor: default;
	}
	.cmd[aria-pressed='true'] {
		color: var(--accent);
	}
	.command-bar > :global(*) {
		flex: none;
	}
	.command-bar[data-stage='1'] .label,
	.command-bar[data-stage='2'] .label {
		display: none;
	}
	.sep {
		width: 1px;
		height: 20px;
		margin: 0 4px;
		background: var(--border);
	}
	.spacer {
		flex: 1 1 auto !important;
		min-width: 8px;
	}
	.segmented {
		display: flex;
		gap: 4px;
	}
	/* Phones (and narrow windows) have their own bar for these. */
	@media (max-width: 760px) {
		.command-bar {
			display: none;
		}
	}
	kbd {
		margin-left: auto;
		color: var(--muted);
		font: inherit;
		font-size: var(--text-sm);
	}
</style>
