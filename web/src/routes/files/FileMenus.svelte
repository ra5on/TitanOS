<script lang="ts">
	import type { FileEntry, FileRoot } from '#lib/api.ts';
	import ContextMenu from '#lib/components/ContextMenu.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { previewKind, type FileView } from '#lib/file-actions.ts';
	import { t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { views, type FileCommand } from './commands.ts';
	import { isEditableText } from '#lib/text-files.ts';
	import { appsFor, EDITOR, extOf, webAppKey } from '#lib/webapps/protocol.ts';
	import { appForFile, webapps } from '#lib/webapps/webapps.svelte.ts';
	import { dockerAppForExt, fileOpeners, type FileOpener } from '#lib/docker-opens.ts';

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
		/** The text editor may save in this folder. */
		writableText?: boolean;
		/** The folder of the entries, relative to the root (for the Docker apps, PLAN 72). */
		dir?: string;
		/** PLAN 66: opens the entry in an app ("webapp:<id>" or the text editor); always = make it the standard. */
		onopenwith?: (entry: FileEntry, app: string, always: boolean) => void;
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
		writableText = false,
		dir = '',
		onopenwith,
		oncommand,
		onclose,
	}: Props = $props();

	// PLAN 66, draft 03: "Öffnen mit ›" lists the web apps for the type and the
	// text editor; "Immer mit … öffnen" makes the chosen one the standard app.
	let withOpen = $state(false);
	let always = $state(false);
	let pointed = $state('');
	$effect(() => {
		void menu;
		withOpen = false;
		always = false;
		pointed = '';
	});
	// PLAN 72: the Docker apps that open the file come from titand (which
	// knows the app's folders); they follow the web apps under a heading.
	let dockerApps = $state<FileOpener[]>([]);
	$effect(() => {
		const entry = menu?.entry;
		const rootId = root?.id;
		dockerApps = [];
		if (!entry || entry.dir || selected.length !== 1 || !rootId || root?.kind === 'stick') return;
		const path = dir ? `${dir}/${entry.name}` : entry.name;
		let current = true;
		void fileOpeners(rootId, path).then((list) => {
			if (current) dockerApps = list;
		});
		return () => {
			current = false;
		};
	});
	type Choice = { key: string; name: string; note: string; hint?: string; docker?: boolean };
	const choices = $derived.by((): Choice[] => {
		const entry = menu?.entry;
		if (!entry || entry.dir || selected.length !== 1) return [];
		const ext = extOf(entry.name);
		const standard = appForFile(entry.name);
		const list: Choice[] = appsFor(ext, webapps.apps).map((a) => ({
			key: webAppKey(a.id),
			name: a.name,
			note: webAppKey(a.id) === standard ? t('files.openWith.standard') : '',
		}));
		if (writableText && isEditableText(entry.name, entry.size))
			list.push({
				key: EDITOR,
				name: t('files.openWith.editor'),
				note: standard === EDITOR ? t('files.openWith.standard') : t('files.openWith.titanos'),
			});
		const dockerStandard = dockerAppForExt(ext);
		for (const o of dockerApps)
			list.push({
				key: 'docker:' + o.app,
				name: o.name,
				docker: true,
				note: o.app === dockerStandard ? t('files.openWith.standard') : '',
				hint: o.mounted
					? undefined
					: o.share
						? t('files.openWith.notMounted', { share: o.share })
						: t('files.openWith.outside'),
			});
		return list;
	});
	const standardApp = $derived.by(() => {
		const entry = menu?.entry;
		if (!entry || entry.dir || selected.length !== 1) return null;
		const docker = dockerAppForExt(extOf(entry.name));
		if (docker && dockerApps.some((o) => o.app === docker)) return 'docker:' + docker;
		const s = appForFile(entry.name);
		return s?.startsWith('webapp:') ? s : null;
	});
	const pointedName = $derived(
		choices.find((c) => c.key === pointed)?.name ??
			choices.find((c) => c.note === t('files.openWith.standard'))?.name ??
			choices[0]?.name ??
			'',
	);
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
		{#if standardApp && onopenwith}<button
				type="button"
				role="menuitem"
				onclick={() => onopenwith(entry, standardApp, false)}
				><Icon name="external" size={16} /><span>{t('files.openWith.open')}</span></button
			>{/if}
		{#if choices.length && onopenwith}
			<button
				type="button"
				role="menuitem"
				aria-haspopup="true"
				aria-expanded={withOpen}
				onclick={(e) => {
					e.stopPropagation();
					withOpen = !withOpen;
				}}
				><i class="no-icon" aria-hidden="true"></i><span>{t('files.openWith.title')}</span><span
					class="chev"
					class:open={withOpen}
					aria-hidden="true">›</span
				></button
			>
			{#if withOpen}
				<div class="with" role="group" aria-label={t('files.openWith.title')}>
					{#each choices as c, i (c.key)}
						{#if c.docker && !choices[i - 1]?.docker}<div class="group" role="presentation">
								{t('files.openWith.docker')}
							</div>{/if}
						<button
							type="button"
							role="menuitem"
							class="choice"
							onpointerenter={() => (pointed = c.key)}
							onfocus={() => (pointed = c.key)}
							onclick={() => onopenwith(entry, c.key, always)}
							><span>{c.name}</span>{#if c.note}<small class="note">{c.note}</small
								>{/if}{#if c.hint}<small class="hint">{c.hint}</small>{/if}</button
						>
					{/each}
					<label class="always"
						><input type="checkbox" bind:checked={always} /><span
							>{t('files.openWith.always', { app: pointedName })}</span
						></label
					>
				</div>
			{/if}
		{/if}
		{#if one && entry.dir}<button
				type="button"
				role="menuitem"
				onclick={() => oncommand('openFolder', entry)}
				><Icon name="folder" size={16} /><span>{t('files.openFolder')}</span><kbd aria-hidden="true"
					>{t('shortcuts.enter')}</kbd
				></button
			>
		{:else if one && previewKind(entry.name, entry.size)}<button
				type="button"
				role="menuitem"
				onclick={() => oncommand('preview', entry)}
				><Icon name="search" size={16} /><span>{t('files.preview')}</span><kbd aria-hidden="true"
					>{t('shortcuts.space')}</kbd
				></button
			>{/if}
		{#if editable}<button type="button" role="menuitem" onclick={() => oncommand('edit', entry)}
				><Icon name="edit" size={16} /><span>{t('files.edit')}</span></button
			>{/if}
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
					aria-hidden="true">{t('shortcuts.ctrl')}+C</kbd
				></button
			>
			{#if writable}<button type="button" role="menuitem" onclick={() => oncommand('cut', entry)}
					><i class="no-icon" aria-hidden="true"></i><span>{t('files.cut')}</span><kbd
						aria-hidden="true">{t('shortcuts.ctrl')}+X</kbd
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
		{#if session.services.agent && root.kind !== 'stick'}<hr />
			<button type="button" role="menuitem" onclick={() => oncommand('agent', entry)}
				><Icon name="sparkle" size={16} /><span>{t('files.withAgent')}</span></button
			>{/if}
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
					>{t('shortcuts.delete')}</kbd
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
					>{t('shortcuts.ctrl')}+V</kbd
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
	.chev {
		flex: none !important;
		color: var(--muted);
		transition: transform 0.15s;
	}
	.chev.open {
		transform: rotate(90deg);
	}
	.with {
		display: flex;
		flex-direction: column;
		margin: 2px 0 4px 26px;
		padding-left: 6px;
		border-left: 2px solid var(--border);
	}
	.group {
		padding: 6px 10px 2px;
		font-size: var(--text-xs);
		letter-spacing: 0.06em;
		text-transform: uppercase;
		color: var(--muted);
	}
	.choice:has(.hint) {
		flex-wrap: wrap;
	}
	.choice .hint {
		flex-basis: 100%;
		color: var(--muted);
		font-size: var(--text-xs);
		white-space: normal;
		overflow-wrap: break-word;
	}
	.choice .note {
		color: var(--muted);
		font-size: var(--text-xs);
	}
	.always {
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 6px 10px;
		font-size: var(--text-sm);
		color: var(--muted);
		cursor: pointer;
	}
	.always span {
		overflow-wrap: break-word;
		white-space: normal;
	}
	@media (prefers-reduced-motion: reduce) {
		.chev {
			transition: none;
		}
	}
	.no-icon {
		width: 16px;
		flex: none;
	}
</style>
