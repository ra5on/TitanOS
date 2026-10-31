<script lang="ts">
	import type { FileEntry, FileRoot } from '#lib/api.ts';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { thumbnailKind, type FileView } from '#lib/file-actions.ts';
	import { endFileDrag, hasFileDrag } from '#lib/file-drag.ts';
	import type { FileSort } from '#lib/file-list.ts';
	import { formatBytes } from '#lib/format.ts';
	import { i18n, t } from '#lib/i18n/index.svelte.ts';
	import FileIcon from './FileIcon.svelte';
	import FileThumbnail from './FileThumbnail.svelte';
	import VirtualEntries from './VirtualEntries.svelte';

	type VirtualList = {
		getScroll: () => number;
		setScroll: (top: number) => void;
		reveal: (index: number) => void;
		getColumns: () => number;
	};
	type Props = {
		/** The entries on screen: filtered and sorted. */
		items: FileEntry[];
		/** Everything in the folder; null while it loads or failed. */
		entries: FileEntry[] | null;
		root: FileRoot | undefined;
		rootId: string;
		dir: string;
		search: string;
		listError: string;
		listLoading: boolean;
		view: FileView;
		sort: FileSort;
		descending: boolean;
		preferencesReady: boolean;
		selectedNames: Set<string>;
		allSelected: boolean;
		writable: boolean;
		/** Entry whose menu is open. */
		menuName: string | undefined;
		virtualList?: VirtualList;
		onload: () => void;
		onselectall: (checked: boolean) => void;
		onsort: (column: FileSort) => void;
		ontoggle: (name: string, checked: boolean) => void;
		onrange: (name: string, add: boolean) => void;
		onrowclick: (entry: FileEntry, event: MouseEvent) => void;
		onrowdblclick: (entry: FileEntry, event: MouseEvent) => void;
		onfolderclick: (entry: FileEntry, event: MouseEvent) => void;
		onmenu: (entry: FileEntry, x: number, y: number) => void;
		onmenubutton: (entry: FileEntry, button: HTMLElement) => void;
		ondragentry: (event: DragEvent, entry: FileEntry) => void;
		/** Something was dropped onto a folder of the list; `path` is that folder. */
		ondropon: (event: DragEvent, path: string) => void;
	};
	let {
		items,
		entries,
		root,
		rootId,
		dir,
		search,
		listError,
		listLoading,
		view,
		sort,
		descending,
		preferencesReady,
		selectedNames,
		allSelected,
		writable,
		menuName,
		virtualList = $bindable(),
		onload,
		onselectall,
		onsort,
		ontoggle,
		onrange,
		onrowclick,
		onrowdblclick,
		onfolderclick,
		onmenu,
		onmenubutton,
		ondragentry,
		ondropon,
	}: Props = $props();

	const join = (...parts: string[]) => parts.filter(Boolean).join('/');
	const date = $derived(
		new Intl.DateTimeFormat(i18n.locale, { dateStyle: 'medium', timeStyle: 'short' }),
	);

	function sortLabel(column: FileSort) {
		const name = t(
			column === 'name' ? 'files.name' : column === 'size' ? 'files.size' : 'files.modified',
		);
		return sort === column
			? `${name}, ${t(descending ? 'files.sortedDescending' : 'files.sortedAscending')}`
			: name;
	}
</script>

<div class="file-entries" class:small={view === 'small'} class:large={view === 'large'}>
	<div class="row header">
		<input
			type="checkbox"
			aria-label={t('files.selectAll')}
			checked={allSelected}
			onchange={(event) => onselectall(event.currentTarget.checked)}
		/>
		{#each ['name', 'size', 'modified'] as const as column (column)}
			<button
				type="button"
				class="sort"
				class:num={column !== 'name'}
				class:active={sort === column}
				disabled={!preferencesReady}
				aria-label={sortLabel(column)}
				title={t('files.sort')}
				onclick={() => onsort(column)}
			>
				<span
					>{t(
						column === 'name' ? 'files.name' : column === 'size' ? 'files.size' : 'files.modified',
					)}</span
				>
				{#if sort === column}<span aria-hidden="true">{descending ? '↓' : '↑'}</span>{/if}
			</button>
		{/each}
	</div>
	<VirtualEntries
		bind:this={virtualList}
		{items}
		key={(e) => e.name}
		minWidth={view === 'list' ? 0 : view === 'small' ? 220 : 160}
		rowHeight={view === 'large' ? 260 : 72}
		label={t('files.title')}
		reset={`${rootId}/${dir}/${search}/${view}/${sort}/${descending}`}
	>
		{#snippet children(entry)}
			<!-- The whole row is a larger target for the mouse. Keyboards and screen
			     readers use the checkbox and the buttons inside it, so the row
			     itself is no control: one nested in another is announced badly. -->
			<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
			<div
				class="row"
				tabindex="-1"
				draggable={root?.kind !== 'stick'}
				ondragstart={(event) => ondragentry(event, entry)}
				ondragend={endFileDrag}
				ondragover={(event) => {
					if (entry.dir && writable && hasFileDrag(event)) {
						event.preventDefault();
						event.stopPropagation();
					}
				}}
				ondrop={(event) => {
					if (entry.dir) ondropon(event, join(dir, entry.name));
				}}
				data-file-name={entry.name}
				class:picked={selectedNames.has(entry.name)}
				onclick={(event) => onrowclick(entry, event)}
				ondblclick={(event) => onrowdblclick(entry, event)}
				oncontextmenu={(event) => {
					event.preventDefault();
					event.stopPropagation();
					onmenu(entry, event.clientX, event.clientY);
				}}
			>
				<input
					type="checkbox"
					aria-label={t('files.select', { name: entry.name })}
					checked={selectedNames.has(entry.name)}
					onclick={(event) => {
						if (event.shiftKey) onrange(entry.name, event.ctrlKey || event.metaKey);
						else ontoggle(entry.name, event.currentTarget.checked);
					}}
				/>
				{#if entry.dir}
					<button type="button" class="entry" onclick={(event) => onfolderclick(entry, event)}
						><FileIcon
							name={entry.name}
							dir
							size={view === 'large' ? 48 : view === 'small' ? 24 : 18}
						/><span>{entry.name}</span></button
					>
				{:else}
					<span class="entry"
						>{#if view === 'large' && thumbnailKind(entry.name) && root?.kind !== 'stick'}<FileThumbnail
								root={rootId}
								path={join(dir, entry.name)}
								modified={entry.modified}
							/>{:else}<FileIcon
								name={entry.name}
								dir={false}
								size={view === 'large' ? 48 : view === 'small' ? 24 : 18}
							/>{/if}<span>{entry.name}</span></span
					>
				{/if}
				<span class="mono num muted">{entry.dir ? '–' : formatBytes(entry.size)}</span>
				<span class="num muted">{date.format(new Date(entry.modified))}</span>
				<button
					type="button"
					class="entry-more"
					aria-label={t('files.actionsFor', { name: entry.name })}
					title={t('files.actionsFor', { name: entry.name })}
					aria-haspopup="menu"
					aria-expanded={menuName === entry.name}
					onclick={(event) => onmenubutton(entry, event.currentTarget)}
					><Icon name="more" size={16} /></button
				>
			</div>
		{/snippet}
	</VirtualEntries>
	{#if !items.length}
		{#if !root}<EmptyState inline title={t('files.chooseRoot')} />{:else if listError}<div
				class="notice-error list-state"
				role="alert"
			>
				<span>{listError}</span><button
					type="button"
					class="btn"
					disabled={listLoading}
					onclick={onload}>{t('common.retry')}</button
				>
			</div>{:else if entries}<EmptyState
				inline
				title={t(search && entries.length > 0 ? 'files.noMatches' : 'files.empty')}
			/>{:else}<p class="muted list-state" role="status">{t('common.loading')}</p>{/if}
	{/if}
</div>

<style>
	.list-state {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
		margin: 12px;
	}
	.file-entries {
		min-height: 120px;
	}
	.row {
		display: grid;
		grid-template-columns: 22px minmax(0, 1fr) 96px 170px 36px;
		align-items: center;
		gap: 12px;
		cursor: pointer;
		padding: 7px 14px;
		border-bottom: 1px solid var(--panel-border);
		font-size: var(--text-md);
		user-select: none;
	}
	.row:not(.header):hover {
		background: color-mix(in srgb, var(--chip) 60%, transparent);
	}
	.row:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.row.header {
		position: sticky;
		top: var(--header-height, 60px);
		z-index: 2;
		padding-top: 4px;
		padding-bottom: 4px;
		background: var(--surface);
		font-size: var(--text-sm);
		color: var(--muted);
		cursor: default;
	}
	.row.picked {
		background: color-mix(in srgb, var(--accent) 14%, transparent);
	}
	.row input {
		width: 18px;
		height: 18px;
		margin: 0;
	}
	.sort {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		min-width: 0;
		padding: 4px 6px;
		margin: 0 -6px;
		background: transparent;
		border: 0;
		border-radius: var(--radius-small);
		color: inherit;
		font: inherit;
		cursor: pointer;
	}
	.sort.num {
		justify-content: flex-end;
	}
	.sort:hover:not(:disabled) {
		background: var(--chip);
		color: var(--text);
	}
	.sort.active {
		color: var(--text);
		font-weight: 500;
	}
	.num {
		text-align: right;
		font-size: var(--text-base);
		white-space: nowrap;
	}
	.entry {
		display: flex;
		align-items: center;
		gap: 10px;
		min-width: 0;
		padding: 0;
		background: transparent;
		border: 0;
		color: var(--text);
		font: inherit;
		text-align: left;
	}
	button.entry {
		cursor: pointer;
	}
	button.entry:hover span {
		text-decoration: underline;
	}
	.entry span {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.entry-more {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 32px;
		height: 32px;
		padding: 0;
		background: transparent;
		border: 1px solid transparent;
		border-radius: var(--radius-ctl);
		color: var(--muted);
		cursor: pointer;
	}
	.entry-more:hover,
	.entry-more[aria-expanded='true'] {
		background: var(--chip);
		border-color: var(--border);
		color: var(--text);
	}

	/* Symbol views: the header keeps the checkbox and the sort buttons in one line. */
	.file-entries.small,
	.file-entries.large {
		padding: 0 12px 12px;
	}
	.small .row.header,
	.large .row.header {
		display: flex;
		gap: 16px;
		margin: 0 -12px 8px;
		padding-left: 26px;
	}
	.small .row:not(.header),
	.large .row:not(.header) {
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		padding: 10px;
		grid-template-columns: 18px minmax(0, 1fr) 32px;
		gap: 8px;
	}
	.small .row:not(.header) > .num,
	.large .row:not(.header) > .num {
		display: none;
	}
	.large .row:not(.header) {
		align-content: start;
	}
	.large .entry {
		grid-column: 1 / -1;
		grid-row: 2;
		flex-direction: column;
		text-align: center;
		min-height: 130px;
		justify-content: center;
	}
	.large .entry span {
		white-space: normal;
		overflow-wrap: anywhere;
		width: 100%;
	}
	.large .entry-more {
		grid-column: 3;
		grid-row: 1;
	}

	@media (max-width: 760px) {
		.row {
			grid-template-columns: 22px minmax(0, 1fr) 76px 36px;
		}
		.row > :nth-child(4) {
			display: none;
		}
		.row.header {
			top: 0;
		}
		.entry-more {
			width: 2.75rem;
			height: 2.75rem;
		}
	}
</style>
