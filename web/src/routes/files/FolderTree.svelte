<script lang="ts">
	import FolderTree from './FolderTree.svelte';
	import { api, type FileEntry } from '#lib/api.ts';
	import { apiErrorText, t, i18n } from '#lib/i18n/index.svelte.ts';
	import { fileList } from '#lib/file-list.ts';
	let {
		root,
		path = '',
		name,
		currentRoot,
		currentPath,
		showHidden,
		onopen,
		depth = 0,
		readOnly = false,
	}: {
		root: string;
		path?: string;
		name: string;
		currentRoot: string;
		currentPath: string;
		showHidden: boolean;
		onopen: (root: string, path: string) => void;
		depth?: number;
		readOnly?: boolean;
	} = $props();
	let expanded = $state(false),
		entries = $state<FileEntry[]>([]),
		busy = $state(false),
		error = $state(''),
		reload = $state(0),
		limit = $state(100);
	$effect(() => {
		if (!expanded) return;
		reload;
		const controller = new AbortController();
		busy = true;
		error = '';
		api
			.get<FileEntry[]>('/files/list?' + new URLSearchParams({ root, path }), controller.signal)
			.then((result) => {
				if (!controller.signal.aborted) entries = result.filter((e) => e.dir);
			})
			.catch((err) => {
				if (!controller.signal.aborted) error = apiErrorText(err);
			})
			.finally(() => {
				if (!controller.signal.aborted) busy = false;
			});
		return () => controller.abort();
	});
	const folders = $derived(
		fileList(
			entries.filter((e) => showHidden || !e.name.startsWith('.')),
			'',
			'name',
			false,
			i18n.locale,
		),
	);
</script>

<div class="node">
	<div class="line">
		<button
			class="toggle"
			aria-expanded={expanded}
			aria-label={t(expanded ? 'files.collapseFolder' : 'files.expandFolder', { name })}
			onclick={() => (expanded = !expanded)}>{expanded ? '▾' : '▸'}</button
		>
		<button
			class="name"
			class:current={root === currentRoot && path === currentPath}
			aria-current={root === currentRoot && path === currentPath ? 'location' : undefined}
			title={name}
			onclick={() => onopen(root, path)}>{name}</button
		>
		{#if readOnly}<span class="chip">{t('files.readOnly')}</span>{/if}
	</div>
	{#if expanded}
		<div class="children">
			{#if busy}<span role="status">{t('files.previewLoading')}</span>{:else if error}<p
					role="alert"
				>
					{error}<button class="btn" onclick={() => reload++}>{t('files.retryPreferences')}</button>
				</p>{:else}
				{#each folders.slice(0, limit) as folder (folder.name)}
					{#if depth < 64}<FolderTree
							{root}
							path={[path, folder.name].filter(Boolean).join('/')}
							name={folder.name}
							{currentRoot}
							{currentPath}
							{showHidden}
							{onopen}
							depth={depth + 1}
						/>{:else}<button
							class="name"
							onclick={() => onopen(root, [path, folder.name].filter(Boolean).join('/'))}
							>{folder.name}</button
						>{/if}
				{:else}<span class="muted">{t('files.noSubfolders')}</span>{/each}
				{#if folders.length > limit}<button class="btn" onclick={() => (limit += 100)}
						>{t('files.moreFolders', { count: folders.length - limit })}</button
					>{/if}
			{/if}
		</div>
	{/if}
</div>

<style>
	.line {
		display: flex;
		align-items: center;
		min-width: 0;
	}
	.line button,
	.name {
		border: 0;
		background: transparent;
		color: var(--text);
		font: inherit;
		cursor: pointer;
		border-radius: var(--radius-ctl);
		min-height: 38px;
		text-align: left;
	}
	.toggle {
		flex: none;
		width: 30px;
	}
	.name {
		flex: 1;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		padding: 4px;
	}
	.current,
	button:hover {
		background: var(--chip) !important;
	}
	.children {
		margin-left: 14px;
		border-left: 1px solid var(--border);
		padding-left: 3px;
	}
	.children > span {
		font-size: var(--text-sm);
	}
	.node {
		min-width: 0;
	}
	p {
		overflow-wrap: anywhere;
	}
</style>
