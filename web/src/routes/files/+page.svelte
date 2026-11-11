<script lang="ts">
	import EmptyState from '#lib/components/EmptyState.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import { inWindow } from '#lib/desktop/frame.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { uploads } from '#lib/uploads.svelte.ts';
	import BrowserPane from './BrowserPane.svelte';
	import FileMenus from './FileMenus.svelte';
	import FileOverlays from './FileOverlays.svelte';
	import { registerFilesMenu } from './files-menu.ts';
	import { FilesPage } from './files-page.svelte.ts';
	import FolderPane from './FolderPane.svelte';
	import { fileKeys } from './keyboard.ts';
	import MobileActions from './MobileActions.svelte';
	import MobileLocation from './MobileLocation.svelte';
	import PlacesSide from './PlacesSide.svelte';

	// State and actions live in files-state.svelte.ts and files-page.svelte.ts;
	// the parts of the page get this one object.
	const p = new FilesPage();
	registerFilesMenu(p);
</script>

<svelte:window onkeydown={(event) => fileKeys(event, p.keys)} />
<div class="files-pagehead"><PageHead title={t('files.title')} /></div>
{#if p.prefs.error}<p class="notice-error" role="alert">
		{p.prefs.error}
		<button class="btn" onclick={() => (p.prefs.ready ? p.prefs.save() : p.prefs.load())}
			>{t('files.retryPreferences')}</button
		>
	</p>{/if}
{#if p.error}<p class="notice-error" role="alert">{p.error}</p>{/if}
<!-- Fixed, so that saving never moves the page. -->
{#if p.prefs.saving}<p class="saving" class:windowed={inWindow} role="status">
		{t('files.savingPreferences')}
	</p>{/if}

{#if p.roots && p.roots.length === 0}
	<EmptyState
		title={t('files.noRoots')}
		text={p.canAdminister ? t('files.noRootsAdmin') : undefined}
	/>
{:else if p.roots}
	<div class="layout" class:windowed={inWindow} class:folded={inWindow && p.sideFolded}>
		<MobileLocation
			root={p.root}
			dir={p.dir}
			bind:placesOpen={p.placesOpen}
			canBack={!!p.steps.back.length}
			onback={() => p.historyGo('back')}
		/>
		<PlacesSide {p} roots={p.roots} />
		{#if !p.trash}
			<MobileActions
				root={p.root}
				dir={p.dir}
				writable={p.writable}
				canForward={!!p.steps.forward.length}
				isFavorite={p.prefs.isFavorite}
				clipboard={!!p.clipboard.content}
				canPaste={p.canPaste}
				split={p.prefs.split}
				canCompare={!!p.rightRoot}
				preferencesReady={p.prefs.ready}
				folderViewSaved={p.prefs.folderViewSaved}
				folderViewsFull={p.prefs.folderViews.length >= 200}
				selected={p.selected.length}
				bind:search={p.search}
				bind:searchOpen={p.mobileSearch}
				oncommand={p.command}
				onsearch={() => {
					p.selected = [];
					p.anchor = '';
				}}
				onmore={(button) => {
					const entry = p.selectedEntries.at(-1);
					if (entry) p.menuFromButton(entry, button);
				}}
			/>
		{/if}

		<div class="workspace" class:split={p.prefs.split}>
			{#if p.prefs.split && !p.trash && (p.selected.length || p.right.selected.length) && p.root?.kind !== 'stick'}
				<div class="pane-actions">
					{#if p.selected.length}
						<button
							class="btn"
							disabled={!p.rightRoot?.writable}
							onclick={() => p.transferPane('right', false)}>{t('files.copyRight')} →</button
						>
						<button
							class="btn"
							disabled={!p.writable || !p.rightRoot?.writable}
							onclick={() => p.transferPane('right', true)}>{t('files.moveRight')} →</button
						>
					{/if}
					{#if p.right.selected.length}
						<button class="btn" disabled={!p.writable} onclick={() => p.transferPane('left', false)}
							>← {t('files.copyLeft')}</button
						>
						<button
							class="btn"
							disabled={!p.rightRoot?.writable || !p.writable}
							onclick={() => p.transferPane('left', true)}>← {t('files.moveLeft')}</button
						>
					{/if}
				</div>
			{/if}
			<BrowserPane {p} />
			{#if p.prefs.split}<FolderPane
					onpreview={(id, path, entry) => (p.otherPreview = { root: id, path, size: entry.size })}
					ondropfiles={p.reviewDrop}
					roots={p.safeRoots}
					refresh={p.jobsTick + uploads.finished}
					showHidden={p.prefs.showHidden}
					onchange={(id, path, names) => (p.right = { root: id, path, selected: names })}
				/>{/if}
		</div>
	</div>
{/if}

<FileMenus
	menu={p.menu}
	background={p.backgroundMenu}
	root={p.root}
	selected={p.selected}
	editable={p.editable}
	writable={p.writable}
	canPaste={p.canPaste}
	view={p.prefs.view}
	writableText={p.writableText}
	dir={p.dir}
	onopenwith={(entry, app, always) => p.choose(() => void p.openWith(entry, app, always))}
	oncommand={(action, entry) => p.choose(() => p.command(action, entry))}
	onclose={() => {
		p.menu = null;
		p.backgroundMenu = null;
	}}
/>

<FileOverlays {p} />

<style>
	.layout {
		display: grid;
		grid-template-columns: 240px minmax(0, 1fr);
		gap: var(--gap);
		align-items: start;
	}
	.workspace {
		min-width: 0;
		display: grid;
		grid-template-columns: minmax(0, 1fr);
		gap: var(--gap);
		align-items: start;
	}
	.workspace.split {
		grid-template-columns: repeat(2, minmax(0, 1fr));
	}
	.pane-actions {
		grid-column: 1 / -1;
		display: flex;
		gap: var(--space-2);
		flex-wrap: wrap;
	}
	.saving {
		position: fixed;
		left: 16px;
		bottom: 44px;
		z-index: 50;
		margin: 0;
		padding: var(--space-1-5) var(--space-3);
		background: var(--surface);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		box-shadow: var(--shadow-menu);
		color: var(--muted);
		font-size: var(--text-base);
	}
	/* The page head is a heading for screen readers only: it takes no room
	   (in a window the flex gap above the content came from it, PLAN 73). */
	.files-pagehead {
		display: contents;
	}
	@media (min-width: 761px) {
		/* PLAN 73: edge to edge in the window – the places are a column of the
		   page's own grid (no fixed bar laid over the list), the list reaches
		   the window's edges, the bars keep their inner spacing. */
		.layout.windowed {
			grid-template-columns: 220px minmax(0, 1fr);
			gap: 0;
			align-items: stretch;
			margin: calc(-1 * var(--page-pad));
			min-height: 100dvh;
		}
		.layout.windowed.folded {
			grid-template-columns: 60px minmax(0, 1fr);
		}
		.layout.windowed > .workspace {
			align-content: start;
		}
		.saving.windowed {
			left: calc(220px + 16px);
		}
	}
	@media (max-width: 1100px) {
		.workspace.split {
			grid-template-columns: minmax(0, 1fr);
		}
	}
	@media (max-width: 760px) {
		.layout {
			grid-template-columns: minmax(0, 1fr);
			gap: var(--space-2);
		}
	}
</style>
