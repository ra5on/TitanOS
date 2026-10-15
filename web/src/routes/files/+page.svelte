<script lang="ts">
	import { untrack } from 'svelte';
	import { api, ApiError, type FileEntry, type FileRoot, type UploadStatus } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { formatBytes, formatDiskBytes } from '#lib/format.ts';
	import { errorText, i18n, t } from '#lib/i18n/index.svelte.ts';
	import { session } from '#lib/session.svelte.ts';
	import { enqueue, recover, uploads } from '#lib/uploads.svelte.ts';
	import UploadPanel from './UploadPanel.svelte';

	const canAdminister = $derived(session.data?.user?.role === 'owner' || session.data?.user?.role === 'admin');

	let roots = $state<FileRoot[] | null>(null);
	let rootId = $state('');
	let dir = $state(''); // folder inside the root, "" for its top
	let entries = $state<FileEntry[] | null>(null);
	let selected = $state<string[]>([]);
	let error = $state('');
	let dragging = $state(false);
	let dialog = $state<{ kind: 'mkdir' } | { kind: 'rename'; name: string } | { kind: 'delete' } | null>(null);
	let typed = $state('');
	let confirmed = $state(false);
	let dialogError = $state('');
	let fileInput = $state<HTMLInputElement>();
	let folderInput = $state<HTMLInputElement>();
    let pending = $state<UploadStatus[]>([]);
    let recoveryInput = $state<HTMLInputElement>();
    let recovery = $state<{ status: UploadStatus; root: FileRoot; dir: string } | null>(null);
    let recovering = $state(false);
    const unfinished = $derived(pending.filter(part => !uploads.items.some(item => item.id === part.id)));
    async function recoverPicked(input: HTMLInputElement) {
        const file = input.files?.[0], selected = recovery;
        input.value = '';
        if (!file || !selected) return;
        recovering = true;
        try { await recover(file, selected.root, selected.dir, selected.status); await load(); }
        catch (err) { error = fail(err); }
        finally { recovering = false; recovery = null; }
    }
    async function removePending(part: UploadStatus) {
        try { await api.delete(`/files/uploads/${part.id}`); await load(); }
        catch (err) { error = fail(err); }
    }


	const root = $derived(roots?.find((candidate) => candidate.id === rootId));
	const crumbs = $derived(dir ? dir.split('/') : []);
	const date = $derived(new Intl.DateTimeFormat(i18n.locale, { dateStyle: 'medium', timeStyle: 'short' }));
	const allSelected = $derived(!!entries?.length && selected.length === entries.length);

	const join = (...parts: string[]) => parts.filter(Boolean).join('/');
	const fail = (err: unknown) => errorText(err instanceof ApiError ? err.code : 'internal');

	async function loadRoots() {
		try {
			roots = await api.get<FileRoot[]>('/files/roots');
			if (!roots.some((candidate) => candidate.id === rootId)) open(roots[0]?.id ?? '', '');
		} catch (err) {
			error = fail(err);
		}
	}

	async function load() {
		if (!rootId) { entries = null; pending = []; return; }
        const requestedRoot = rootId, requestedDir = dir;
		try {
			const query = `root=${encodeURIComponent(requestedRoot)}&path=${encodeURIComponent(requestedDir)}`;
            const [listing, parts] = await Promise.all([
                api.get<FileEntry[]>(`/files/list?${query}`),
                root?.writable ? api.get<UploadStatus[]>(`/files/uploads?${query}`) : Promise.resolve([])
            ]);
            if (rootId !== requestedRoot || dir !== requestedDir) return;
            entries = listing; pending = parts;
			selected = selected.filter((name) => entries!.some((entry) => entry.name === name));
			error = '';
		} catch (err) {
			entries = [];
			error = fail(err);
		}
	}

	function open(id: string, path: string) {
		rootId = id;
		dir = path;
		selected = [];
		entries = null; pending = [];
		load();
	}

	// At the start, and whenever an upload finishes: it may belong to the
	// folder on screen, and it changes the free space.
	$effect(() => {
		uploads.finished;
		untrack(() => {
			loadRoots();
			load();
		});
	});

	function toggle(name: string, checked: boolean) {
		selected = checked ? [...selected, name] : selected.filter((other) => other !== name);
	}

	function download() {
		if (!entries || selected.length === 0) return;
		const single = selected.length === 1 ? entries.find((entry) => entry.name === selected[0]) : undefined;
		const query = new URLSearchParams({ root: rootId });
		for (const name of selected) query.append('path', join(dir, name));
		// The browser saves the answer; nothing passes through this page's memory.
		location.href = `/api/v1/files/${single && !single.dir ? 'download' : 'zip'}?${query}`;
	}

	function show(kind: 'mkdir' | 'rename' | 'delete') {
		dialogError = '';
		confirmed = false;
		if (kind === 'rename') {
			typed = selected[0];
			dialog = { kind, name: selected[0] };
		} else {
			typed = '';
			dialog = { kind };
		}
	}

	async function submitDialog(event: SubmitEvent) {
		event.preventDefault();
		if (!dialog) return;
		dialogError = '';
		try {
			if (dialog.kind === 'mkdir') {
				await api.post('/files/mkdir', { root: rootId, path: join(dir, typed.trim()) });
			} else if (dialog.kind === 'rename') {
				await api.post('/files/rename', { root: rootId, from: join(dir, dialog.name), to: join(dir, typed.trim()) });
			} else {
				await api.post('/files/delete', { root: rootId, paths: selected.map((name) => join(dir, name)), permanent: !root?.recycle });
			}
			dialog = null;
			selected = [];
			await load();
			loadRoots();
		} catch (err) {
			dialogError = fail(err);
		}
	}

	/** Creates the folders of an uploaded tree, parents first, then queues the files. */
	async function upload(files: { file: File; path: string }[]) {
		if (!root || files.length === 0) return;
		const target = dir;
		const folders = new Set<string>();
		for (const { path } of files) {
			const parts = path.split('/').slice(0, -1);
			for (let depth = 1; depth <= parts.length; depth++) folders.add(parts.slice(0, depth).join('/'));
		}
		for (const folder of [...folders].sort()) {
			try {
				await api.post('/files/mkdir', { root: root.id, path: join(target, folder) });
			} catch (err) {
				// The folder being there already is what we want.
				if (!(err instanceof ApiError && err.code === 'file_exists')) return void (error = fail(err));
			}
		}
		enqueue(
			files.map(({ file, path }) => ({ file, dir: join(target, path.split('/').slice(0, -1).join('/')) })),
			root
		);
		load();
	}

	function picked(input: HTMLInputElement) {
		const files = [...(input.files ?? [])].map((file) => ({ file, path: file.webkitRelativePath || file.name }));
		input.value = '';
		upload(files);
	}

	/** Reads a dropped folder with everything below it. */
	async function readEntry(entry: FileSystemEntry, prefix: string, out: { file: File; path: string }[]) {
		if (entry.isFile) {
			const file = await new Promise<File>((resolve, reject) => (entry as FileSystemFileEntry).file(resolve, reject));
			out.push({ file, path: prefix + entry.name });
		} else if (entry.isDirectory) {
			const reader = (entry as FileSystemDirectoryEntry).createReader();
			for (;;) {
				const batch = await new Promise<FileSystemEntry[]>((resolve, reject) => reader.readEntries(resolve, reject));
				if (batch.length === 0) break;
				for (const child of batch) await readEntry(child, prefix + entry.name + '/', out);
			}
		}
	}

	async function dropped(event: DragEvent) {
		event.preventDefault();
		dragging = false;
		if (!root?.writable || !event.dataTransfer) return;
		// The entries have to be taken before the first await; the list is gone afterwards.
		const items = [...event.dataTransfer.items].map((item) => item.webkitGetAsEntry?.()).filter((entry): entry is FileSystemEntry => !!entry);
		const plain = [...event.dataTransfer.files];
		const files: { file: File; path: string }[] = [];
		if (items.length > 0) for (const entry of items) await readEntry(entry, '', files);
		else files.push(...plain.map((file) => ({ file, path: file.name })));
		upload(files);
	}
</script>

<div class="head">
	<h1>{t('files.title')}</h1>
</div>

{#if error}<p class="notice-error" role="alert">{error}</p>{/if}

{#if roots && roots.length === 0}
	<div class="tile empty">
		<p>{t('files.noRoots')}</p>
		{#if canAdminister}<p class="muted">{t('files.noRootsAdmin')}</p>{/if}
	</div>
{:else if roots}
	<div class="layout">
		<nav class="tile places" aria-label={t('files.shares')}>
			{#each ['share', 'volume'] as kind (kind)}
				{@const list = roots.filter((candidate) => candidate.kind === kind)}
				{#if list.length > 0}
					<span class="group">{t(kind === 'share' ? 'files.shares' : 'files.volumes')}{kind === 'volume' ? ` · ${t('files.adminOnly')}` : ''}</span>
					{#each list as place (place.id)}
						<button type="button" class:current={place.id === rootId} aria-current={place.id === rootId ? 'true' : undefined} onclick={() => open(place.id, '')}>
							<Icon name={kind === 'share' ? 'folder' : 'disk'} size={16} />
							<span class="place-name">{place.name}</span>
							{#if !place.writable}<span class="chip">{t('files.readOnly')}</span>{/if}
						</button>
					{/each}
				{/if}
			{/each}
			{#if root}
				<span class="usage muted">{root.name}<br /><span class="mono">{t('files.free', { free: formatDiskBytes(root.freeBytes), total: formatDiskBytes(root.totalBytes) })}</span></span>
			{/if}
		</nav>

		<section
			class="tile browser"
			class:dragging
			aria-label={root?.name}
			ondragover={(event) => {
				if (root?.writable) {
					event.preventDefault();
					dragging = true;
				}
			}}
			ondragleave={(event) => {
				if (!event.currentTarget.contains(event.relatedTarget as Node)) dragging = false;
			}}
			ondrop={dropped}
		>
			<div class="toolbar">
				<div class="crumbs mono">
					<button type="button" onclick={() => open(rootId, '')}>{root?.name}</button>
					{#each crumbs as crumb, index (index)}
						<span class="muted">/</span>
						<button type="button" onclick={() => open(rootId, crumbs.slice(0, index + 1).join('/'))}>{crumb}</button>
					{/each}
				</div>
				{#if root?.writable}
					<button type="button" class="btn" onclick={() => fileInput?.click()}><Icon name="upload" size={16} />{t('files.upload')}</button>
					<button type="button" class="btn" onclick={() => folderInput?.click()}>{t('files.uploadFolder')}</button>
					<button type="button" class="btn" onclick={() => show('mkdir')}>{t('files.newFolder')}</button>
					<input bind:this={fileInput} type="file" multiple hidden onchange={(event) => picked(event.currentTarget)} />
					<input bind:this={folderInput} type="file" webkitdirectory hidden onchange={(event) => picked(event.currentTarget)} />
				{/if}
			</div>

			{#if unfinished.length > 0}
                <section class="unfinished" aria-label={t('uploads.unfinished')}>
                    <strong>{t('uploads.unfinished')}</strong>
                    <p class="muted">{recovering ? t('uploads.verifying') : t('uploads.recoverHint')}</p>
                    <input bind:this={recoveryInput} type="file" hidden onchange={event => recoverPicked(event.currentTarget)} />
                    {#each unfinished as part (part.id)}
                        <div class="unfinished-row">
                            <span>{part.name} · <span class="mono">{formatBytes(part.offset)} / {formatBytes(part.size)}</span></span>
                            <button class="btn" disabled={recovering || part.busy} onclick={() => { if (root) { recovery = { status: part, root, dir }; recoveryInput?.click(); } }}>{t('uploads.resume')}</button>
                            <button class="btn" disabled={recovering || part.busy} onclick={() => removePending(part)}>{t('uploads.removePartial')}</button>
                        </div>
                    {/each}
                </section>
            {/if}
            <div class="row header">
				<input type="checkbox" aria-label={t('files.selectAll')} checked={allSelected} onchange={(event) => (selected = event.currentTarget.checked ? (entries ?? []).map((entry) => entry.name) : [])} />
				<span>{t('files.name')}</span>
				<span class="num">{t('files.size')}</span>
				<span class="num">{t('files.modified')}</span>
			</div>
			{#each entries ?? [] as entry (entry.name)}
				<div class="row" class:picked={selected.includes(entry.name)}>
					<input type="checkbox" aria-label={t('files.select', { name: entry.name })} checked={selected.includes(entry.name)} onchange={(event) => toggle(entry.name, event.currentTarget.checked)} />
					{#if entry.dir}
						<button type="button" class="entry" onclick={() => open(rootId, join(dir, entry.name))}><Icon name="folder" size={16} /><span>{entry.name}</span></button>
					{:else}
						<span class="entry"><Icon name="file" size={16} /><span>{entry.name}</span></span>
					{/if}
					<span class="mono num muted">{entry.dir ? '–' : formatBytes(entry.size)}</span>
					<span class="num muted">{date.format(new Date(entry.modified))}</span>
				</div>
			{:else}
				{#if entries}<p class="muted none">{t('files.empty')}</p>{/if}
			{/each}

			{#if selected.length > 0}
				<div class="actions">
					<span class="muted count">{t('files.selected', { count: selected.length })}</span>
					<button type="button" class="btn" onclick={download}><Icon name="download" size={16} />{t('files.download')}</button>
					{#if root?.writable}
						{#if selected.length === 1}<button type="button" class="btn" onclick={() => show('rename')}>{t('files.rename')}</button>{/if}
						<button type="button" class="btn danger" onclick={() => show('delete')}>{t('files.delete')}</button>
					{/if}
				</div>
			{/if}

			{#if root?.writable}
				<p class="drop" class:active={dragging}>
					<Icon name="upload" size={18} />{dragging ? t('files.dropNow', { path: join(root.name, dir) }) : t('files.dropHint')}
				</p>
			{/if}
		</section>
	</div>
{/if}

<UploadPanel />

{#if dialog}
	{@const kind = dialog.kind}
	<Dialog
		title={kind === 'mkdir'
			? t('files.newFolderTitle')
			: kind === 'rename'
				? t('files.renameTitle', { name: dialog.name })
				: selected.length === 1
					? t('files.deleteTitleOne', { name: selected[0] })
					: t('files.deleteTitleMany', { count: selected.length })}
		onclose={() => (dialog = null)}
	>
		<form onsubmit={submitDialog}>
			{#if kind === 'delete'}
				<p class={root?.recycle ? 'text' : 'notice-error'}>{t(root?.recycle ? 'files.deleteRecycle' : 'files.deletePermanent')}</p>
				{#if !root?.recycle}
					<label class="confirm"><input type="checkbox" bind:checked={confirmed} /><span>{t('files.deleteConfirm')}</span></label>
				{/if}
			{:else}
				<label class="field-row">
					<span>{t(kind === 'mkdir' ? 'files.name' : 'files.newName')}</span>
					<!-- svelte-ignore a11y_autofocus -->
					<input class="field" type="text" bind:value={typed} maxlength="255" autocomplete="off" spellcheck="false" required autofocus />
				</label>
			{/if}
			{#if dialogError}<p class="notice-warn" role="alert">{dialogError}</p>{/if}
			<div class="dialog-actions">
				<button type="button" class="btn" onclick={() => (dialog = null)}>{t('common.cancel')}</button>
				{#if kind === 'delete'}
					<button type="submit" class="btn {root?.recycle ? 'btn-warn' : 'btn-danger'}" disabled={!root?.recycle && !confirmed}>{t('files.delete')}</button>
				{:else}
					<button type="submit" class="btn btn-primary">{t(kind === 'mkdir' ? 'files.create' : 'files.rename')}</button>
				{/if}
			</div>
		</form>
	</Dialog>
{/if}

<style>
 .unfinished { padding: 14px 16px; border-bottom: 1px solid var(--border); }
 .unfinished-row { display: flex; align-items: center; flex-wrap: wrap; gap: 10px; margin-top: 10px; }
 .unfinished-row > span { flex: 1; overflow-wrap: anywhere; }
	h1 {
		font-size: 1.5rem;
		font-weight: 600;
	}
	.layout {
		display: grid;
		grid-template-columns: 230px minmax(0, 1fr);
		gap: var(--gap);
		align-items: start;
	}
	.places {
		display: flex;
		flex-direction: column;
		gap: 2px;
		padding: 12px 8px;
	}
	.group {
		padding: 8px 10px 4px;
		font-size: 0.75rem;
		color: var(--muted);
	}
	.places button {
		display: flex;
		align-items: center;
		gap: 10px;
		min-height: 38px;
		padding: 0 10px;
		background: transparent;
		border: 0;
		border-radius: var(--radius-ctl);
		color: var(--muted);
		font: inherit;
		font-size: 0.875rem;
		text-align: left;
		cursor: pointer;
	}
	.places button:hover,
	.places button.current {
		background: var(--chip);
		color: var(--text);
	}
	.places button.current {
		font-weight: 500;
	}
	.place-name {
		flex: 1 1 auto;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.usage {
		margin: 10px 10px 2px;
		padding-top: 10px;
		border-top: 1px solid var(--panel-border);
		font-size: 0.75rem;
	}
	.browser {
		display: flex;
		flex-direction: column;
		min-width: 0;
	}
	.browser.dragging {
		border-color: var(--accent);
	}
	.toolbar {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px;
		padding: 12px 14px;
		border-bottom: 1px solid var(--panel-border);
	}
	.toolbar .btn {
		height: 2.375rem;
	}
	.crumbs {
		flex: 1 1 200px;
		min-width: 0;
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 4px;
		font-size: 0.875rem;
	}
	.crumbs button {
		padding: 4px 6px;
		background: transparent;
		border: 0;
		border-radius: 6px;
		color: var(--text);
		font: inherit;
		cursor: pointer;
	}
	.crumbs button:hover {
		background: var(--chip);
	}
	.row {
		display: grid;
		grid-template-columns: 22px minmax(0, 1fr) 90px 170px;
		align-items: center;
		gap: 12px;
		padding: 9px 14px;
		border-bottom: 1px solid var(--panel-border);
		font-size: 0.875rem;
	}
	.row.header {
		font-size: 0.75rem;
		color: var(--muted);
	}
	.row.picked {
		background: color-mix(in srgb, var(--accent) 12%, transparent);
	}
	.row input {
		width: 18px;
		height: 18px;
		margin: 0;
	}
	.num {
		text-align: right;
		font-size: 0.8125rem;
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
	.none {
		padding: 28px 14px;
		font-size: 0.875rem;
	}
	.actions {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 8px;
		padding: 10px 14px;
		background: var(--chip);
		border-bottom: 1px solid var(--panel-border);
	}
	.actions .btn {
		height: 2.25rem;
	}
	.count {
		margin-right: auto;
		font-size: 0.8125rem;
	}
	.danger {
		color: var(--error-text);
	}
	.drop {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 8px;
		margin: 14px;
		padding: 16px;
		border: 1px dashed var(--outline-border);
		border-radius: var(--radius-ctl);
		font-size: 0.8125rem;
		color: var(--muted);
	}
	.drop.active {
		border-color: var(--accent);
		background: color-mix(in srgb, var(--accent) 8%, transparent);
		color: var(--text);
	}
	.empty {
		padding: 28px 22px;
		display: flex;
		flex-direction: column;
		gap: 4px;
		font-size: 0.875rem;
	}
	form {
		display: flex;
		flex-direction: column;
		gap: 16px;
	}
	.field-row {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: 0.875rem;
	}
	.text {
		font-size: 0.875rem;
	}
	@media (max-width: 760px) {
		.layout {
			grid-template-columns: 1fr;
		}
		.row {
			grid-template-columns: 22px minmax(0, 1fr) 80px;
		}
		.row > :nth-child(4) {
			display: none;
		}
	}
</style>
