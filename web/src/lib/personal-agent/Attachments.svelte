<script lang="ts">
	import { onMount } from 'svelte';
	import { api, apiFetch, responseError, type FileRoot, type FileEntry } from '#lib/api.ts';
	import { activeNode } from '#lib/cluster-node.ts';
	import { apiErrorText } from '#lib/i18n/index.svelte.ts';
	import Icon from '#lib/components/Icon.svelte';
	import { base, type Source } from './api.ts';
	import { msg, sourceTitle } from './labels.ts';
	import FileDialog from '#lib/webapps/FileDialog.svelte';
	let {
		sources,
		allowed,
		filesAllowed = false,
		onselect,
	}: {
		sources: Source[];
		allowed: boolean;
		filesAllowed?: boolean;
		onselect: (s: Source) => void;
	} = $props();
	let items = $state<Source[]>([]),
		error = $state(''),
		busy = $state(false),
		confirm = $state(''),
		file = $state<HTMLInputElement>(),
		pick = $state<'' | 'open' | 'folder'>(''),
		query = $state(''),
		root = $state(''),
		roots = $state<FileRoot[]>([]),
		hits = $state<(FileEntry & { path: string })[]>([]);
	let live = false,
		searchVersion = 0;
	async function list() {
		const reply = await api.get<{ sources: Source[] }>(base + '/attachments');
		if (live) items = reply.sources ?? [];
	}
	onMount(() => {
		live = true;
		if (allowed) void list().catch((e) => (error = apiErrorText(e)));
		if (filesAllowed && !activeNode())
			void api
				.get<FileRoot[]>('/files/roots')
				.then((items) => {
					if (live) {
						roots = items;
						root = items[0]?.id ?? '';
					}
				})
				.catch((e) => (error = apiErrorText(e)));
		return () => {
			live = false;
			searchVersion++;
		};
	});
	async function upload(pdf: File | undefined) {
		if (!pdf || busy) return;
		busy = true;
		error = '';
		try {
			if (pdf.size > 10 * 1024 * 1024) throw Error(msg('tooLarge'));
			const reply = await apiFetch(base + '/attachments?name=' + encodeURIComponent(pdf.name), {
				method: 'POST',
				headers: { 'Content-Type': 'application/pdf' },
				body: pdf,
			});
			if (!reply.ok) throw await responseError(reply);
			const src = (await reply.json()) as Source;
			if (live) {
				onselect(src);
				await list();
			}
		} catch (e) {
			if (live) error = apiErrorText(e);
		} finally {
			if (live) busy = false;
		}
	}
	async function remove(id: string) {
		busy = true;
		error = '';
		try {
			await api.post(base + '/attachments/' + encodeURIComponent(id) + '/delete', { confirm: id });
			await list();
			confirm = '';
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			busy = false;
		}
	}
	function choose(folder: string, path: string, kind = 'file') {
		onselect({ kind, id: path || '.', label: path.split('/').at(-1), ref: { root: folder } });
		pick = '';
	}
	async function search() {
		const seq = ++searchVersion,
			folder = root;
		error = '';
		hits = [];
		if (!folder || !query.trim()) return;
		try {
			const result = await api.post<{ entries: (FileEntry & { path: string })[] }>(
				'/files/search',
				{ root: folder, path: '', query: query.trim(), showHidden: false },
			);
			if (live && seq === searchVersion)
				hits = (result.entries ?? []).filter((entry) => !entry.dir);
		} catch (e) {
			if (live && seq === searchVersion) error = apiErrorText(e);
		}
	}
</script>

{#if allowed}<section class="pa-attachments">
		<h3>{msg('attachments')}</h3>
		<input
			type="file"
			accept="application/pdf,.pdf"
			hidden
			bind:this={file}
			onchange={(e) => {
				void upload(e.currentTarget.files?.[0]);
				e.currentTarget.value = '';
			}}
		/>
		<button class="pa-button" disabled={busy} onclick={() => file?.click()}
			><Icon name="clip" />{msg('uploadPDF')}</button
		>
		{#each items as src (src.id)}<div class="pa-title-row">
				<button
					class="pa-link"
					disabled={sources.some((s) => s.kind === src.kind && s.id === src.id)}
					onclick={() => onselect(src)}>{sourceTitle(src)}</button
				><button
					class="pa-icon"
					aria-label={msg('delete') + ' ' + sourceTitle(src)}
					onclick={() => (confirm = src.id)}><Icon name="trash" /></button
				>
			</div>
			{#if confirm === src.id}<div class="pa-approval">
					<strong>{msg('deleteAttachment')} · {sourceTitle(src)}</strong>
					<div class="pa-actions">
						<button class="pa-button" onclick={() => (confirm = '')}>{msg('back')}</button><button
							class="pa-button"
							disabled={busy}
							onclick={() => remove(src.id)}>{msg('delete')}</button
						>
					</div>
				</div>{/if}{/each}
	</section>{/if}
{#if filesAllowed && !activeNode()}<div class="pa-context">
		<button class="pa-button" onclick={() => (pick = 'open')}
			><Icon name="folder" />{msg('selectFile')}</button
		>
		<button class="pa-button" onclick={() => (pick = 'folder')}
			><Icon name="folder" />{msg('outputFolder')}</button
		>
		{#if roots.length}<details>
				<summary>{msg('searchFiles')}</summary>
				<form
					class="pa-field"
					onsubmit={(e) => {
						e.preventDefault();
						void search();
					}}
				>
					<select
						aria-label={msg('source')}
						bind:value={root}
						onchange={() => {
							hits = [];
							searchVersion++;
						}}
						>{#each roots as r}<option value={r.id}>{r.name}</option>{/each}</select
					>
					<input type="search" aria-label={msg('searchFiles')} bind:value={query} required /><button
						class="pa-button">{msg('searchFiles')}</button
					>
				</form>
				{#each hits as entry (entry.path)}<button
						class="pa-link"
						onclick={() => choose(root, entry.path)}>{entry.path}</button
					>{/each}
			</details>{/if}
	</div>{/if}
{#if pick}<FileDialog
		mode={pick}
		title={msg('selectFile')}
		onpick={(entry) => choose(entry.root, entry.path, pick === 'folder' ? 'folder' : 'file')}
		onclose={() => (pick = '')}
	/>{/if}
{#if error}<p role="alert" class="pa-warning">{error}</p>{/if}
