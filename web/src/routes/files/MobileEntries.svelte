<script lang="ts">
	import type { FileEntry } from '#lib/api.ts';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import SwipeRow from '#lib/components/SwipeRow.svelte';
	import { thumbnailKind } from '#lib/file-actions.ts';
	import { formatBytes } from '#lib/format.ts';
	import { i18n, t } from '#lib/i18n/index.svelte.ts';
	import { currentPlace } from './mobile-views.svelte.ts';
	import FileIcon from './FileIcon.svelte';
	import FileThumbnail from './FileThumbnail.svelte';
	import type { FilesPage } from './files-page.svelte.ts';
	import { join } from './files-state.svelte.ts';
	import VirtualEntries from './VirtualEntries.svelte';
	let { p }: { p: FilesPage } = $props();
	const place = $derived(currentPlace(p.rootId, p.dir));
	const layout = $derived(
		p.prefs.mobile.folder(place)?.view ?? p.prefs.mobile.data?.default ?? 'list',
	);
	const rail = $derived(layout === 'rail');
	const visual = $derived(layout === 'grid' || layout === 'cards');
	const folders = $derived(rail ? p.visibleEntries.filter((e) => e.dir) : []);
	const items = $derived(rail ? p.visibleEntries.filter((e) => !e.dir) : p.visibleEntries);
	const reset = $derived(
		JSON.stringify([p.rootId, p.dir, p.search, layout, p.prefs.sort, p.prefs.descending]),
	);
	const date = $derived(
		new Intl.DateTimeFormat(i18n.locale, { year: '2-digit', month: '2-digit', day: '2-digit' }),
	);
	const type = (entry: FileEntry) =>
		entry.dir
			? t('files.mobile.folder')
			: entry.name.includes('.')
				? entry.name.split('.').at(-1)?.toLocaleUpperCase(i18n.locale)
				: t('files.mobile.file');
	function open(entry: FileEntry) {
		if (p.choosingFiles) {
			p.toggle(entry.name, !p.selectedNames.has(entry.name));
			return;
		}
		if (entry.dir) {
			p.open(p.rootId, join(p.dir, entry.name));
			return;
		}
		p.selected = [entry.name];
		p.anchor = entry.name;
		if (!p.openWithStandard(entry)) p.quickLook(entry);
	}
	let fileList: VirtualEntries<FileEntry> | undefined = $state();
	let folderRail: VirtualEntries<FileEntry> | undefined = $state();
	// The keyboard moves through the same folder-first order as the underlying file list.
	export function getColumns() {
		return fileList?.getColumns() ?? 1;
	}
	export function reveal(i: number) {
		if (rail && i < folders.length) folderRail?.reveal(i);
		else fileList?.reveal(i - folders.length);
	}
	export function getScroll() {
		return fileList?.getScroll() ?? 0;
	}
	export function setScroll(n: number) {
		fileList?.setScroll(n);
	}
</script>

{#snippet art(entry: FileEntry, big = false)}
	<span class="art" class:folder={entry.dir} class:big>
		{#if entry.dir}<span class="folder-shape" aria-hidden="true"></span>
		{:else if visual && p.root?.kind !== 'stick' && thumbnailKind(entry.name)}<FileThumbnail
				root={p.rootId}
				path={join(p.dir, entry.name)}
				modified={entry.modified}
				height={layout === 'cards' ? 98 : 72}
			/>
		{:else}<FileIcon name={entry.name} dir={false} size={big ? 34 : 23} />{/if}
	</span>
{/snippet}

{#snippet entryRow(entry: FileEntry, strip = false)}
	<div
		class="record"
		class:divided={!visual && !strip}
		class:strip
		class:picked={p.selectedNames.has(entry.name)}
		data-file-name={entry.name}
	>
		<button
			type="button"
			class="entry"
			aria-label={entry.name}
			aria-pressed={p.choosingFiles ? p.selectedNames.has(entry.name) : undefined}
			oncontextmenu={(e) => {
				e.preventDefault();
				e.stopPropagation();
				p.entryMenu(entry, e.clientX, e.clientY);
			}}
			onclick={() => open(entry)}
		>
			{#if p.choosingFiles}<span class="selection" class:checked={p.selectedNames.has(entry.name)}
					>{#if p.selectedNames.has(entry.name)}<Icon name="check" size={12} />{/if}</span
				>{/if}
			{@render art(entry, visual || strip)}
			<span class="copy"
				><strong>{entry.name}</strong>
				{#if layout !== 'details'}<small
						>{type(entry)}{#if !entry.dir}
							· {formatBytes(entry.size)}{/if}</small
					>{/if}
			</span>
			{#if layout === 'details'}<span class="changed">{date.format(new Date(entry.modified))}</span
				><span class="size">{entry.dir ? '—' : formatBytes(entry.size)}</span>{/if}
		</button>
		<button
			type="button"
			class="more"
			aria-label={t('files.actionsFor', { name: entry.name })}
			aria-haspopup="menu"
			aria-expanded={p.menu?.entry.name === entry.name}
			onclick={(e) => p.menuFromButton(entry, e.currentTarget)}
			><Icon name="more" size={16} /></button
		>
	</div>
{/snippet}

<div class="mobile-entries" data-layout={layout}>
	{#if rail && folders.length}
		<VirtualEntries
			bind:this={folderRail}
			horizontal
			items={folders}
			key={(e) => e.name}
			minWidth={154}
			label={t('files.mobile.folders')}
			{reset}
		>
			{#snippet children(entry)}{@render entryRow(entry, true)}{/snippet}
		</VirtualEntries>
		{#if items.length}<h4>{t('files.title')}</h4>{/if}
	{/if}
	{#if layout === 'details' && items.length}
		<div class="columns">
			{#each ['name', 'modified', 'size'] as const as column}
				<button
					disabled={!p.prefs.ready}
					aria-label={t('files.sort') + ' · ' + t(`files.${column}`)}
					onclick={() => p.prefs.sortBy(column)}
				>
					{t(`files.${column}`)}{#if p.prefs.sort === column}<span
							>{p.prefs.descending ? '↓' : '↑'}</span
						>{/if}</button
				>
			{/each}
		</div>
	{/if}
	{#if items.length}
		<VirtualEntries
			bind:this={fileList}
			{items}
			key={(e) => e.name}
			label={t('files.title')}
			{reset}
			minWidth={layout === 'grid' ? 92 : layout === 'cards' ? 140 : 0}
			rowHeight={layout === 'cards' ? 199 : layout === 'grid' ? 152 : 64}
		>
			{#snippet children(entry)}
				{#if visual}{@render entryRow(entry)}
				{:else}<SwipeRow
						actions={[
							...(!entry.dir
								? [
										{
											id: 'share',
											icon: 'external' as const,
											label: t('phone.files.share'),
											run: () => p.shareEntry(entry),
										},
									]
								: []),
							...(p.writable
								? [
										{
											id: 'delete',
											icon: 'trash' as const,
											label: t('common.delete'),
											danger: true,
											run: () => p.deleteEntry(entry),
										},
									]
								: []),
						]}>{@render entryRow(entry)}</SwipeRow
					>{/if}
			{/snippet}
		</VirtualEntries>
	{/if}
	{#if !p.visibleEntries.length}
		{#if !p.root}<EmptyState inline title={t('files.chooseRoot')} />
		{:else if p.listError}<p class="notice-error" role="alert">
				{p.listError}
				<button class="btn" disabled={p.listLoading} onclick={p.load}>{t('common.retry')}</button>
			</p>
		{:else if p.entries}<EmptyState
				inline
				title={t(p.search && p.entries.length ? 'files.noMatches' : 'files.empty')}
			/>
		{:else}<p role="status">{t('common.loading')}</p>{/if}
	{/if}
</div>

<style>
	.mobile-entries {
		min-width: 0;
		--line: var(--nord-line, var(--panel-border));
		--glass: var(--nord-surface, var(--panel-bg));
		--swipe-bg: var(--glass);
	}
	.mobile-entries :global(.viewport:not(.horizontal)) {
		border: 1px solid var(--line);
		background: var(--glass);
		-webkit-backdrop-filter: var(--nord-blur, var(--panel-backdrop));
		backdrop-filter: var(--nord-blur, var(--panel-backdrop));
		border-radius: 18px;
		overflow: hidden;
	}
	.mobile-entries :global(.viewport.virtual:not(.horizontal)) {
		overflow: auto;
	}
	.record {
		position: relative;
		display: flex;
		align-items: center;
		min-height: 58px;
		color: var(--text);
		border-bottom: 1px solid var(--line);
	}
	:global(.cell:last-child) .divided {
		border-bottom-color: transparent;
	}
	.entry,
	.more,
	.columns button {
		border: 0;
		background: transparent;
		color: inherit;
		font: inherit;
	}
	.entry {
		position: relative;
		display: flex;
		gap: 0.65rem;
		align-items: center;
		flex: 1;
		min-width: 0;
		align-self: stretch;
		text-align: start;
		padding: 0.45rem 0.55rem 0.45rem 0.8rem;
	}
	.more {
		display: grid;
		place-items: center;
		flex: none;
		width: 44px;
		height: 44px;
		color: var(--muted);
	}
	.copy {
		min-width: 0;
	}
	strong,
	small {
		display: block;
	}
	strong {
		font-size: 0.93rem;
		font-weight: 600;
		overflow-wrap: anywhere;
		line-height: 1.22;
	}
	small {
		margin-top: 0.16rem;
		font-size: 0.75rem;
		color: var(--muted);
	}
	.art {
		display: grid;
		place-items: center;
		flex: none;
		width: 32px;
		min-height: 32px;
		color: var(--accent);
	}
	.folder-shape {
		position: relative;
		display: block;
		width: 25px;
		height: 17px;
		margin-top: 3px;
		border-radius: 3px;
		background: linear-gradient(#ffd876, #edac36);
		box-shadow: inset 0 1px 0 #ffe9a1;
	}
	.folder-shape::before {
		content: '';
		position: absolute;
		inset: -3px auto auto 0;
		width: 44%;
		height: 3px;
		border-radius: 3px 3px 0 0;
		background: #dda031;
	}
	.big .folder-shape {
		width: 47px;
		height: 31px;
		border-radius: 5px;
	}
	.big .folder-shape::before {
		top: -5px;
		height: 5px;
	}
	.picked {
		background: color-mix(in srgb, var(--accent) 15%, transparent);
	}
	.selection {
		flex: none;
		width: 16px;
		height: 16px;
		display: grid;
		place-items: center;
		border: 1px solid var(--muted);
		border-radius: 50%;
	}
	.checked {
		background: var(--accent);
		color: var(--on-accent);
	}
	.columns,
	.mobile-entries[data-layout='details'] .entry {
		display: grid;
		grid-template-columns: minmax(0, 1fr) 4rem 3.3rem;
		column-gap: 0.2rem;
	}
	.columns {
		padding: 0 44px 0 0.7rem;
		color: var(--muted);
	}
	.columns button {
		min-height: 44px;
		padding: 0;
		text-align: end;
		font-size: 0.7rem;
	}
	.columns button:first-child {
		text-align: start;
	}
	.changed,
	.size {
		font-size: 0.72rem;
		text-align: end;
		color: var(--muted);
	}
	[data-layout='details'] .art {
		display: none;
	}
	[data-layout='details'] .selection {
		position: absolute;
		top: 2px;
		left: 2px;
		width: 12px;
		height: 12px;
	}
	[data-layout='grid'] .record,
	[data-layout='cards'] .record {
		display: block;
		border: 0;
		height: 100%;
	}
	[data-layout='grid'] .entry,
	[data-layout='cards'] .entry {
		display: flex;
		flex-direction: column;
		width: 100%;
		gap: 0.35rem;
		padding: 0.65rem 0.3rem;
		align-items: center;
		text-align: center;
	}
	[data-layout='grid'] .copy,
	[data-layout='cards'] .copy {
		width: 100%;
	}
	[data-layout='grid'] .art,
	[data-layout='cards'] .art {
		width: 100%;
		height: 72px;
	}
	[data-layout='grid'] .more,
	[data-layout='cards'] .more {
		z-index: 1;
		color: var(--text);
		position: absolute;
		top: 0;
		right: 0;
	}
	[data-layout='grid'] .selection,
	[data-layout='cards'] .selection {
		position: absolute;
		top: 8px;
		left: 6px;
	}
	[data-layout='grid'] strong,
	[data-layout='cards'] strong,
	.strip strong {
		display: -webkit-box;
		-webkit-line-clamp: 2;
		line-clamp: 2;
		-webkit-box-orient: vertical;
		overflow: hidden;
	}
	[data-layout='grid'] :global(.viewport:not(.horizontal)) {
		padding: 0.3rem;
	}
	[data-layout='cards'] :global(.viewport:not(.horizontal)) {
		border: 0;
		background: none;
		border-radius: 0;
	}
	[data-layout='cards'] .record,
	.strip {
		background: var(--glass);
		border: 1px solid var(--line);
		border-radius: 16px;
	}
	[data-layout='cards'] .entry {
		text-align: start;
		align-items: stretch;
		padding: 0.45rem 0.65rem 0.75rem;
	}
	[data-layout='cards'] .art {
		height: 100px;
	}
	.strip {
		min-height: 76px;
	}
	.more :global(svg) {
		pointer-events: none;
	}
	[data-layout='grid'] .more :global(svg),
	[data-layout='cards'] .more :global(svg) {
		box-sizing: content-box;
		padding: 0.12rem 0.22rem;
		border-radius: 6px;
		background: var(--glass);
		backdrop-filter: var(--nord-blur, none);
	}
	.strip .entry {
		padding: 0.45rem;
	}
	.strip .more {
		position: absolute;
		right: 0;
		top: 0;
		height: 44px;
	}
	.strip .entry {
		padding-top: 18px;
		align-items: center;
	}
	.strip .art {
		width: 35px;
	}
	.strip .folder-shape {
		width: 31px;
		height: 22px;
	}
	h4 {
		margin: 0.8rem 0 0.5rem;
	}
</style>
