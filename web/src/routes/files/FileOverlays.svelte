<script lang="ts">
	// Everything that lies over the list: the dialogs of the file manager and
	// the panels of running jobs, uploads and undo.
	import Dialog from '#lib/components/Dialog.svelte';
	import { previewKind } from '#lib/file-actions.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { isEditableText } from '#lib/text-files.ts';
	import { EDITOR } from '#lib/webapps/protocol.ts';
	import { appForFile } from '#lib/webapps/webapps.svelte.ts';
	import ArchiveDialog from './ArchiveDialog.svelte';
	import CompareDialog from './CompareDialog.svelte';
	import CopyDialog from './CopyDialog.svelte';
	import CreateFileDialog from './CreateFileDialog.svelte';
	import DockerMountDialog from './DockerMountDialog.svelte';
	import DownloadDialog from './DownloadDialog.svelte';
	import DriveFormatDialog from './DriveFormatDialog.svelte';
	import DuplicatesDialog from './DuplicatesDialog.svelte';
	import EntryDialog from './EntryDialog.svelte';
	import FavoritesDialog from './FavoritesDialog.svelte';
	import FilePreview from './FilePreview.svelte';
	import FileProperties from './FileProperties.svelte';
	import type { FilesPage } from './files-page.svelte.ts';
	import { join } from './files-state.svelte.ts';
	import JobsPanel from './JobsPanel.svelte';
	import PhotoView from './PhotoView.svelte';
	import RenameBatchDialog from './RenameBatchDialog.svelte';
	import SearchDialog from './SearchDialog.svelte';
	import ShortcutsDialog from './ShortcutsDialog.svelte';
	import TextEditor from './TextEditor.svelte';
	import UndoPanel from './UndoPanel.svelte';
	import UploadPanel from './UploadPanel.svelte';
	import UploadReview from './UploadReview.svelte';
	import UrlDownloadDialog from './UrlDownloadDialog.svelte';
	import WindowsPath from './WindowsPath.svelte';

	let { p }: { p: FilesPage } = $props();
</script>

{#if p.dockerMount}
	<DockerMountDialog
		app={p.dockerMount.opener.name}
		share={p.dockerMount.opener.share ?? ''}
		file={p.dockerMount.file}
		busy={p.dockerMount.busy}
		error={p.dockerMount.error}
		onconfirm={(readOnly) => void p.confirmMount(readOnly)}
		onclose={() => {
			if (!p.dockerMount?.busy) p.dockerMount = null;
		}}
	/>
{/if}
{#if p.dropReview}
	<Dialog title={t('files.dropTransfer')} onclose={() => (p.dropReview = null)}>
		<p>{t('files.dropTransferHint', { count: p.dropReview.source.paths.length })}</p>
		<p class="mono">
			{p.safeRoots.find((r) => r.id === p.dropReview?.source.root)?.name}: {p.dropReview.source.paths.join(
				', ',
			)}
		</p>
		<p class="mono">
			→ {p.safeRoots.find((r) => r.id === p.dropReview?.target)?.name}/{p.dropReview.path}
		</p>
		<div class="dialog-actions">
			<button class="btn" onclick={() => (p.dropReview = null)}>{t('common.cancel')}</button>
			<button class="btn" onclick={() => p.acceptDrop(false)}>{t('files.copy')}</button>
			<button
				class="btn btn-warn"
				disabled={!p.safeRoots.find((r) => r.id === p.dropReview?.source.root)?.writable}
				onclick={() => p.acceptDrop(true)}>{t('files.move')}</button
			>
		</div>
	</Dialog>
{/if}
{#if p.companion}
	<Dialog title={t('files.macCompanion.title')} onclose={() => (p.companion = null)}>
		<p>{t('files.macCompanion.text', { name: p.companion.file.name })}</p>
		<p>
			{p.companion.real
				? t('files.macCompanion.real', { name: p.companion.real.name })
				: t('files.macCompanion.noReal', { name: p.companion.file.name.slice(2) })}
		</p>
		<div class="dialog-actions">
			<button class="btn" onclick={() => (p.companion = null)}>{t('common.close')}</button>
			{#if p.companion.real}<button
					class="btn btn-primary"
					onclick={() => {
						const real = p.companion?.real;
						p.companion = null;
						if (!real) return;
						p.selected = [real.name];
						p.anchor = real.name;
						if (p.openWithStandard(real)) return;
						if (previewKind(real.name, real.size)) p.preview = real;
						else if (p.editable) p.editing = join(p.dir, real.name);
					}}>{t('files.macCompanion.open', { name: p.companion.real.name })}</button
				>{/if}
		</div>
	</Dialog>
{/if}
<JobsPanel
	canRetry={p.canRetry}
	onopen={(id, path) => p.open(id, path)}
	refresh={p.jobsTick}
	onfinished={() => {
		p.load();
		p.loadRoots();
		p.jobsTick++;
	}}
/>
<UploadPanel />
<UndoPanel
	bind:this={p.undoPanel}
	roots={p.safeRoots}
	refresh={p.jobsTick}
	onchanged={() => {
		p.load();
		p.loadRoots();
		p.jobsTick++;
	}}
/>

{#if p.copying && p.root && p.roots}
	{#key p.copying}<CopyDialog
			roots={p.safeRoots}
			from={p.copying.from ?? p.root}
			paths={p.copying.paths ?? p.selected.map((name) => join(p.dir, name))}
			move={p.copying.move}
			restore={p.copying.restore}
			targetRoot={p.copying.targetRoot}
			targetDir={p.copying.targetDir}
			startDir={p.dir}
			onclose={() => (p.copying = null)}
			ondone={() => {
				if (p.copying?.pasted && p.copying.move) p.clipboard.content = null;
				p.copying = null;
				p.selected = [];
				p.jobsTick++;
			}}
		/>{/key}
{/if}
{#if p.editing && p.root}
	<TextEditor
		root={p.root.id}
		stick={p.root.kind === 'stick'}
		path={p.editing}
		onclose={() => {
			p.editing = null;
			p.load();
		}}
	/>
{/if}
{#if p.windowsPath && p.root}<WindowsPath
		root={p.root}
		dir={p.dir}
		onclose={() => (p.windowsPath = false)}
	/>{/if}

{#if p.dialog}
	{#key p.dialog}
		<EntryDialog
			dialog={p.dialog}
			root={p.root}
			rootId={p.rootId}
			dir={p.dir}
			writable={p.writable}
			selected={p.selected}
			files={p.selectedEntries.length - p.selectedFolders}
			folders={p.selectedFolders}
			bytes={p.selectionBytes}
			uncounted={p.uncounted}
			busy={p.dialogBusy}
			onbusy={(busy) => (p.dialogBusy = busy)}
			onclose={() => (p.dialog = null)}
			ondone={p.dialogDone}
		/>
	{/key}
{/if}

{#if p.downloadPaths && p.root}<DownloadDialog
		root={p.root}
		paths={p.downloadPaths}
		onclose={() => (p.downloadPaths = null)}
		onfailed={(text) => (p.error = text)}
	/>{/if}
{#if p.duplicates && p.root}<DuplicatesDialog
		root={p.safeRoots.find((r) => r.id === p.rootId) ?? p.root}
		dir={p.dir}
		showHidden={p.prefs.showHidden}
		onclose={() => (p.duplicates = false)}
		onopen={(path) => {
			p.duplicates = false;
			p.open(p.rootId, path);
		}}
		onchanged={() => {
			p.load();
			p.loadRoots();
			p.jobsTick++;
		}}
	/>{/if}
{#if p.photos && p.root}<PhotoView
		root={p.rootId}
		dir={p.dir}
		onclose={() => (p.photos = false)}
	/>{/if}
{#if p.deepSearch && p.root}<SearchDialog
		root={p.root}
		dir={p.dir}
		showHidden={p.prefs.showHidden}
		onclose={() => (p.deepSearch = false)}
		onopen={(path) => {
			p.deepSearch = false;
			p.open(p.rootId, path);
		}}
	/>{/if}
{#if p.comparison && p.root && p.rightRoot}<CompareDialog
		left={p.safeRoots.find((r) => r.id === p.rootId) ?? p.root}
		leftDir={p.dir}
		right={p.rightRoot}
		rightDir={p.right.path}
		showHidden={p.prefs.showHidden}
		ondone={() => {
			p.comparison = false;
			p.jobsTick++;
		}}
		onclose={() => (p.comparison = false)}
		onopen={(id, path) => {
			p.comparison = false;
			p.open(id, path);
		}}
	/>{/if}
{#if p.batchRename && p.root}<RenameBatchDialog
		root={p.root}
		dir={p.dir}
		names={p.selected}
		onclose={() => {
			p.batchRename = false;
			p.load();
			p.jobsTick++;
		}}
		ondone={() => {
			p.batchRename = false;
			p.selected = [];
			p.load();
			p.jobsTick++;
		}}
	/>{/if}
{#if p.archive && p.root}<ArchiveDialog
		root={p.root}
		path={p.archive}
		dir={p.dir}
		roots={p.safeRoots}
		onclose={() => (p.archive = null)}
		ondone={() => {
			p.archive = null;
			p.jobsTick++;
		}}
	/>{/if}
{#if p.properties}<FileProperties
		root={p.properties.root}
		path={p.properties.path}
		locked={p.properties.root.kind === 'usb' &&
			!p.safeRoots.find((r) => r.id === p.properties?.root.id)?.writable &&
			p.properties.root.writable}
		onclose={() => (p.properties = null)}
	/>{/if}
{#if p.uploadReview}<UploadReview
		root={p.uploadReview.root}
		dir={p.uploadReview.dir}
		files={p.uploadReview.files}
		onclose={() => (p.uploadReview = null)}
		onconfirm={p.startUpload}
	/>{/if}
{#if p.favoriteManager}<FavoritesDialog
		favorites={p.prefs.favorites}
		onsave={(next) => p.prefs.saveFavorites(next)}
		onclose={() => (p.favoriteManager = false)}
	/>{/if}
{#if p.formattingDrive && p.canAdminister}<DriveFormatDialog
		onclose={() => (p.formattingDrive = false)}
		onformatted={() => {
			p.usb.load();
		}}
	/>{/if}
{#if p.downloadingLink && p.root && p.writable}<UrlDownloadDialog
		root={p.rootId}
		dir={p.dir}
		confined={!p.canAdminister}
		onclose={() => (p.downloadingLink = false)}
		onstarted={() => {
			p.downloadingLink = false;
			p.jobsTick++;
		}}
	/>{/if}
{#if p.creatingFile && p.root && p.writable}<CreateFileDialog
		root={p.rootId}
		dir={p.dir}
		onclose={() => (p.creatingFile = false)}
		oncreated={(path) => {
			p.creatingFile = false;
			p.editing = path;
			p.load();
		}}
	/>{/if}
{#if p.otherPreview}<FilePreview {...p.otherPreview} onclose={() => (p.otherPreview = null)} />{/if}
{#if p.shortcuts}<ShortcutsDialog onclose={() => (p.shortcuts = false)} />{/if}
{#if p.preview && p.root}<FilePreview
		root={p.root.id}
		path={join(p.dir, p.preview.name)}
		size={p.preview.size}
		modified={p.preview.modified}
		onprevious={p.galleryIndex > 0 ? () => p.quickLook(p.gallery[p.galleryIndex - 1]) : undefined}
		onnext={p.galleryIndex >= 0 && p.galleryIndex < p.gallery.length - 1
			? () => p.quickLook(p.gallery[p.galleryIndex + 1])
			: undefined}
		onopen={appForFile(p.preview.name)?.startsWith('webapp:') ||
		(appForFile(p.preview.name) === EDITOR && isEditableText(p.preview.name, p.preview.size))
			? () => {
					const entry = p.preview;
					p.preview = null;
					if (entry) p.openWithStandard(entry);
				}
			: undefined}
		onclose={() => (p.preview = null)}
	/>{/if}

<!-- PLAN 87: a file swiped "Teilen" on the phone. -->
{#if p.sharing}
	<p class="sharing" role="status">{t('phone.files.sharing', { name: p.sharing })}</p>
{/if}
{#if p.shareAgain}
	<Dialog
		title={t('phone.files.shareTitle', { name: p.shareAgain.name })}
		onclose={() => (p.shareAgain = null)}
	>
		<p>{t('phone.files.shareReady')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (p.shareAgain = null)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-primary" onclick={p.shareNow}
				>{t('phone.files.share')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.sharing {
		position: fixed;
		left: 50%;
		bottom: calc(16px + env(safe-area-inset-bottom));
		z-index: 60;
		transform: translateX(-50%);
		margin: 0;
		padding: var(--space-2) var(--space-4);
		border-radius: 999px;
		background: var(--surface);
		border: 1px solid var(--border);
		box-shadow: var(--menu-shadow);
		color: var(--text);
		overflow-wrap: anywhere;
	}
</style>
