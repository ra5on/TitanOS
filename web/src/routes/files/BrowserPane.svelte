<script lang="ts">
	// The open folder: its bars, notices, the list and what stands below it;
	// in its place the recycle bin or the storage analysis.
	import StatusBar from '#lib/components/explorer/StatusBar.svelte';
	import { inWindow } from '#lib/desktop/frame.ts';
	import { formatDiskBytes } from '#lib/format.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import ActionBar from './ActionBar.svelte';
	import BackupLock from './BackupLock.svelte';
	import CommandBar from './CommandBar.svelte';
	import DropHint from './DropHint.svelte';
	import { pickedFiles } from './drop-files.ts';
	import FileList from './FileList.svelte';
	import type { FilesPage } from './files-page.svelte.ts';
	import { join } from './files-state.svelte.ts';
	import NavBar from './NavBar.svelte';
	import SelectionBar from './SelectionBar.svelte';
	import StorageAnalysis from './StorageAnalysis.svelte';
	import TrashArea from './TrashArea.svelte';
	import UnfinishedUploads from './UnfinishedUploads.svelte';

	let { p }: { p: FilesPage } = $props();
</script>

<section
	oncontextmenu={p.blankMenu}
	class="tile browser"
	class:dragging={p.dragging}
	aria-label={p.root?.name}
	ondragover={(event) => {
		if (p.writable) {
			event.preventDefault();
			p.dragging = true;
		}
	}}
	ondragleave={(event) => {
		if (!event.currentTarget.contains(event.relatedTarget as Node)) p.dragging = false;
	}}
	ondrop={p.dropped}
>
	{#if p.trash && p.root}
		<TrashArea
			root={p.root}
			binRoot={p.binRoot}
			bins={p.bins}
			openBin={p.openBin}
			bind:trashBin={p.trashBin}
			user={session.data?.user?.name ?? ''}
			admin={p.canAdminister}
			onback={() => {
				p.trash = false;
				p.load();
			}}
			onchanged={() => {
				p.load();
				p.loadRoots();
			}}
		/>
	{:else if p.usage && p.root}
		<StorageAnalysis
			root={p.safeRoots.find((r) => r.id === p.rootId) ?? p.root}
			dir={p.dir}
			showHidden={p.prefs.showHidden}
			onback={() => (p.usage = false)}
			onopen={(path) => {
				p.usage = false;
				p.open(p.rootId, path);
			}}
			onduplicates={() => (p.duplicates = true)}
			onchanged={() => {
				p.load();
				p.loadRoots();
				p.jobsTick++;
			}}
		/>
	{:else}
		<NavBar
			bind:this={p.navBar}
			windowed={inWindow}
			root={p.root}
			rootId={p.rootId}
			dir={p.dir}
			canBack={!!p.steps.back.length}
			canForward={!!p.steps.forward.length}
			isFavorite={p.prefs.isFavorite}
			favoritesFull={p.prefs.favorites.length >= 50}
			bind:search={p.search}
			bind:editing={p.pathEditing}
			bind:message={p.pathMessage}
			bind:input={p.pathInput}
			onopen={p.open}
			oncommand={p.command}
			onwindowspath={() => (p.windowsPath = true)}
			onreload={() => {
				p.load();
				p.loadRoots();
			}}
			onsearch={() => {
				p.selected = [];
				p.anchor = '';
			}}
		/>
		{#if inWindow}
			<!-- PLAN 73: the command bar like Explorer (drafts 16–18). -->
			<CommandBar
				root={p.root}
				writable={p.writable}
				canPaste={p.canPaste}
				selected={p.selected.length}
				split={p.prefs.split}
				canCompare={!!p.rightRoot}
				view={p.prefs.view}
				sort={p.prefs.sort}
				descending={p.prefs.descending}
				preferencesReady={p.prefs.ready}
				showHidden={p.prefs.showHidden}
				folderViewSaved={p.prefs.folderViewSaved}
				folderViewsFull={p.prefs.folderViews.length >= 200}
				bind:selectionFilter={p.selectionFilter}
				bind:fileInput={p.fileInput}
				bind:folderInput={p.folderInput}
				onpicked={(input) => p.upload(pickedFiles(input))}
				oncommand={p.command}
				onsort={(column) => p.prefs.sortBy(column)}
			/>
		{:else}
			<ActionBar
				root={p.root}
				writable={p.writable}
				clipboard={!!p.clipboard.content}
				canPaste={p.canPaste}
				selected={p.selected.length}
				split={p.prefs.split}
				canCompare={!!p.rightRoot}
				view={p.prefs.view}
				preferencesReady={p.prefs.ready}
				showHidden={p.prefs.showHidden}
				folderViewSaved={p.prefs.folderViewSaved}
				folderViewsFull={p.prefs.folderViews.length >= 200}
				bind:selectionFilter={p.selectionFilter}
				bind:fileInput={p.fileInput}
				bind:folderInput={p.folderInput}
				onpicked={(input) => p.upload(pickedFiles(input))}
				oncommand={p.command}
			/>
		{/if}

		<UnfinishedUploads
			pending={p.pending}
			root={p.root}
			dir={p.dir}
			bind:recovering={p.recovering}
			onchanged={p.load}
			onerror={(message) => (p.error = message)}
		/>
		{#if p.root?.kind === 'usb' && p.usb.mayHoldBackup(p.rootId)}
			<BackupLock
				usb={p.usb}
				rootId={p.rootId}
				locked={p.backupLocked}
				onlock={() => p.clipboard.dropCut()}
			/>
		{/if}
		{#if p.root && !p.writable}<p class="notice-warn">
				{t(
					p.backupLocked
						? 'files.rightReason.usb'
						: p.root.kind === 'share'
							? 'files.rightReason.share'
							: p.root.kind === 'stick'
								? 'files.rightReason.stick'
								: 'files.rightReason.readOnly',
				)}
			</p>{/if}
		{#if p.refreshError}<p class="notice-warn" role="status">
				{t('files.autoRefreshFailed')}
			</p>{/if}

		<FileList
			bind:virtualList={p.virtualList}
			checks={!inWindow || p.selected.length > 1}
			items={p.visibleEntries}
			entries={p.entries}
			root={p.root}
			rootId={p.rootId}
			dir={p.dir}
			search={p.search}
			listError={p.listError}
			listLoading={p.listLoading}
			view={p.prefs.view}
			sort={p.prefs.sort}
			descending={p.prefs.descending}
			preferencesReady={p.prefs.ready}
			selectedNames={p.selectedNames}
			allSelected={p.allSelected}
			writable={p.writable}
			menuName={p.menu?.entry.name}
			onload={p.load}
			onselectall={(checked) =>
				(p.selected = checked ? p.visibleEntries.map((entry) => entry.name) : [])}
			onsort={(column) => p.prefs.sortBy(column)}
			ontoggle={p.toggle}
			onrange={p.selectRange}
			onrowclick={p.rowClicked}
			onrowdblclick={p.rowDoubleClicked}
			onfolderclick={p.folderClick}
			onmenu={p.entryMenu}
			onmenubutton={p.menuFromButton}
			ondragentry={p.dragEntry}
			ondropon={(event, path) => p.reviewDrop(event, p.rootId, path)}
			onshare={p.phone ? p.shareEntry : undefined}
			ondelete={p.phone ? p.deleteEntry : undefined}
		/>

		{#if p.writable}<DropHint dragging={p.dragging} path={join(p.root?.name ?? '', p.dir)} />{/if}
		{#if inWindow}
			<!-- PLAN 73: the status line at the bottom (Explorer): entries, selection, free space. -->
			<StatusBar label={t('files.statusBar')}>
				<span>{t('files.statusEntries', { count: p.visibleEntries.length })}</span>
				{#if p.selected.length}<span
						>{t('files.statusSelected', {
							count: p.selected.length,
						})}{#if p.selectionBytes}{' · '}{formatDiskBytes(p.selectionBytes)}{/if}</span
					>{/if}
				{#if p.clipboard.content}<span
						>{t(p.clipboard.content.move ? 'files.cutCount' : 'files.copiedCount', {
							count: p.clipboard.content.paths.length,
						})}</span
					>{/if}
				{#if p.root && p.root.totalBytes > 0}<span class="end"
						>{t('files.statusFree', {
							name: p.root.name,
							free: formatDiskBytes(p.root.freeBytes),
						})}</span
					>{/if}
			</StatusBar>
		{:else}
			<p class="list-footer muted" role="status">
				{t('files.resultCount', {
					shown: p.visibleEntries.length,
					total: p.entries?.length ?? 0,
				})}{#if p.clipboard.content}
					· {t(p.clipboard.content.move ? 'files.cutCount' : 'files.copiedCount', {
						count: p.clipboard.content.paths.length,
					})}{/if}
			</p>
		{/if}
		{#if p.root?.kind === 'stick'}<p class="muted none">{t('files.stickNote')}</p>{/if}

		{#if p.selected.length > 0 && !inWindow}
			<SelectionBar
				count={p.selected.length}
				bytes={p.selectionBytes}
				folders={p.selectedFolders}
				uncounted={p.uncounted}
				measuring={p.measuring}
				backup={p.currentUSBBackup}
				stick={p.root?.kind === 'stick'}
				editable={p.editable}
				writable={p.writable}
				onmeasure={p.measureSelection}
				oncommand={p.command}
				onmore={(button) => {
					const entry = p.selectedEntries.at(-1);
					if (entry) p.menuFromButton(entry, button);
				}}
			/>
		{/if}
	{/if}
</section>

<style>
	.browser {
		display: flex;
		flex-direction: column;
		min-width: 0;
	}
	.browser.dragging {
		border-color: var(--accent);
	}
	.list-footer {
		margin: 0;
		padding: var(--space-2-5) 14px 0;
		font-size: var(--text-base);
	}
	.none {
		padding: 20px 14px;
		font-size: var(--text-md);
	}
	@media (min-width: 761px) {
		:global(.layout.windowed) .browser {
			border: 0;
			border-radius: 0;
			background: none;
			box-shadow: none;
		}
	}
	/* The pinned status bar (style W4) sits at the window's lower edge: a glass
	   tile (backdrop-filter) would become its frame and hold it below the list. */
	:global(body.has-statusbar) .browser {
		-webkit-backdrop-filter: none;
		backdrop-filter: none;
	}
</style>
