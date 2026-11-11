<script lang="ts">
	// The places of the file manager: on the page a tile beside the list, in a
	// window of the desktop the window's one side bar (PLAN 69).
	import type { FileRoot } from '#lib/api.ts';
	import Icon from '#lib/components/Icon.svelte';
	import { inWindow } from '#lib/desktop/frame.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import type { FilesPage } from './files-page.svelte.ts';
	import Places from './Places.svelte';

	let { p, roots }: { p: FilesPage; roots: FileRoot[] } = $props();
</script>

<nav
	id="file-places"
	class="places"
	class:tile={!inWindow}
	class:windowed={inWindow}
	class:folded={inWindow && p.sideFolded}
	class:open={p.placesOpen}
	aria-label={t('files.places')}
>
	<div class="side-tree">
		<Places
			windowed={inWindow}
			{roots}
			current={p.trash ? '' : p.rootId}
			dir={p.dir}
			showHidden={p.prefs.showHidden}
			admin={p.canAdminister}
			usbDevices={p.usb.devices}
			usbBusy={p.usb.busy}
			usbLoadError={p.usb.loadError}
			backups={p.usb.backups}
			backupLoadError={p.usb.backupLoadError}
			backupProgressError={p.usb.progressError}
			openingBackup={p.usb.opening}
			favorites={p.visibleFavorites}
			onopen={p.open}
			onusb={(device, action) => p.usb.action(device, action)}
			onbackup={(backup, restore) => p.usb.openBackup(backup, restore)}
			onrefreshbackups={() => {
				p.usb.loadBackups();
				p.usb.load();
			}}
			onmanagefavorites={() => (p.favoriteManager = true)}
			onremovefavorite={(favorite) =>
				p.prefs.saveFavorites(p.prefs.favorites.filter((f) => f !== favorite))}
			onformat={() => (p.formattingDrive = true)}
		/>
	</div>
	{#if inWindow}
		<!-- PLAN 69: the program's other entries and folding, as in every window's side bar. -->
		<div class="side-foot">
			<span class="side-group">{t('desktop.side.settings')}</span>
			<a
				class="side-item"
				href="/settings/files"
				title={p.sideFolded ? t('desktop.side.filesSettings') : undefined}
				aria-label={p.sideFolded ? t('desktop.side.filesSettings') : undefined}
				><Icon name="gear" size={17} /><span>{t('desktop.side.filesSettings')}</span></a
			>
			<button
				type="button"
				class="side-item side-fold"
				onclick={p.foldSide}
				title={p.sideFolded ? t('desktop.window.unfold') : undefined}
				aria-label={p.sideFolded ? t('desktop.window.unfold') : undefined}
				><Icon name="sidebar" size={17} /><span>{t('desktop.window.fold')}</span></button
			>
		</div>
	{/if}
</nav>

<style>
	.places {
		display: flex;
		flex-direction: column;
		gap: 2px;
		padding: var(--space-2);
		position: sticky;
		top: calc(var(--header-height, 0px) + var(--gap));
		max-height: calc(100vh - var(--header-height, 0px) - 2 * var(--gap) - 40px);
		overflow: auto;
	}

	/* PLAN 69: in a window of the desktop the places are the window's one side
	   bar (the window draws none on this page): same width, surface, group
	   headings and highlighted entry as the side bar of every other window. */
	.side-foot {
		display: none;
	}
	.side-group {
		margin: var(--space-3) var(--space-2-5) var(--space-1);
		font-size: var(--text-xs);
		color: var(--muted);
		text-transform: uppercase;
		letter-spacing: 0.05em;
	}
	.side-item {
		display: flex;
		align-items: center;
		gap: var(--space-2-5);
		min-height: 36px;
		padding: var(--space-1-5) var(--space-2-5);
		border: 0;
		border-radius: calc(9px * var(--radius-scale, 1));
		background: none;
		color: var(--text);
		font: inherit;
		text-align: left;
		text-decoration: none;
		cursor: pointer;
		width: 100%;
	}
	.side-item :global(svg) {
		flex: none;
		color: var(--muted);
	}
	.side-item:hover {
		background: color-mix(in srgb, var(--text) 7%, transparent);
	}
	.side-item:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.side-fold {
		color: var(--muted);
		margin-top: var(--space-1-5);
	}
	@media (min-width: 761px) {
		.places.windowed {
			position: sticky;
			z-index: 25;
			top: 0;
			height: 100dvh;
			width: auto;
			max-height: none;
			gap: 0;
			padding: var(--space-3) var(--space-2-5);
			overflow: hidden;
			/* The same opaque pane as the window's side bar (PLAN 71). */
			background:
				linear-gradient(
					180deg,
					color-mix(in srgb, var(--text) 3%, transparent),
					color-mix(in srgb, var(--accent) 3%, transparent)
				),
				color-mix(in srgb, var(--surface) 88%, var(--bg));
			border-right: 1px solid var(--border);
		}
		.places.windowed.folded {
			padding: var(--space-3) var(--space-2);
			align-items: center;
		}
		.side-tree {
			flex: 1;
			min-height: 0;
			overflow-x: hidden;
			overflow-y: auto;
			/* The scroll bar gets a lane of its own instead of lying over the names
			   (owner 9.11.2026); overlay scroll bars keep a small distance too. */
			scrollbar-gutter: stable;
			scrollbar-width: thin;
			padding-right: var(--space-1);
		}
		.folded .side-tree,
		.folded .side-group,
		.folded .side-item span {
			display: none;
		}
		.side-foot {
			display: flex;
			flex-direction: column;
			gap: 2px;
			flex: none;
			width: 100%;
		}
		.folded .side-item {
			justify-content: center;
			padding: 0;
			width: 44px;
		}
		.folded .side-foot {
			align-items: center;
		}
		/* The places look like the entries of a window's side bar. */
		.places.windowed :global(.group) {
			margin: var(--space-3) var(--space-2-5) var(--space-1);
			padding: 0;
			font-size: var(--text-xs);
			text-transform: uppercase;
			letter-spacing: 0.05em;
		}
		.places.windowed :global(:is(.name, .place)) {
			min-height: 36px;
			border-radius: calc(9px * var(--radius-scale, 1));
			color: var(--text);
		}
		.places.windowed :global(:is(.name, .place):hover) {
			background: color-mix(in srgb, var(--text) 7%, transparent) !important;
		}
		.places.windowed :global(:is(.name, .place).current) {
			background: color-mix(in srgb, var(--accent) 18%, transparent) !important;
			/* Toward the text color: 4.5:1 on the tinted glass in every theme (PLAN 65-5). */
			color: color-mix(in srgb, var(--accent) 65%, var(--text));
			font-weight: 600;
		}
		.places.windowed :global(.name.current svg) {
			color: var(--accent);
		}
	}
	@media (max-width: 760px) {
		/* A narrow window: the places unfold under the location like on the phone. */
		.places.windowed {
			background: var(--panel-bg);
			border: 1px solid var(--panel-border);
			border-radius: var(--radius-tile);
		}
		.places.windowed.open {
			flex-direction: column;
		}
		.places.windowed .side-foot {
			display: flex;
			flex-direction: column;
			gap: 2px;
		}
		.places.windowed .side-fold {
			display: none;
		}
	}
	@media (max-width: 760px) {
		.places {
			display: none;
			position: static;
			max-height: none;
		}
		.places.open {
			display: flex;
		}
	}
</style>
