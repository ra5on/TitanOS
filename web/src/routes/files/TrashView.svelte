<script lang="ts">
	import { untrack } from 'svelte';
	import { api, ApiError, type FileEntry, type FileRoot } from '#lib/api.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import EmptyState from '#lib/components/EmptyState.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import { formatBytes } from '#lib/format.ts';
	import { errorText, i18n, t } from '#lib/i18n/index.svelte.ts';

	// The bin of a share is .recycle/<account>/<where it was>. Paths here
	// start with the account; an account that does not administer only ever
	// sees its own part, an administrator starts with the list of accounts.
	let { root, user, admin, onclose, onchanged }: { root: FileRoot; user: string; admin: boolean; onclose: () => void; onchanged: () => void } = $props();

	const base = $derived(admin ? '' : user);
	let path = $state('');
	type TrashEntry=FileEntry & {path?:string;original?:string;allocated?:number};
 let entries = $state<TrashEntry[] | null>(null);
 let folderFilter=$state(''),beforeDate=$state(''),afterDate=$state('');
 const filtered=$derived((entries??[]).filter(entry=>!flat||((!folderFilter||(entry.original??entry.name).split('/').slice(0,-1).join('/').toLocaleLowerCase(i18n.locale).includes(folderFilter.toLocaleLowerCase(i18n.locale)))&&(!afterDate||Date.parse(entry.modified)>=new Date(afterDate+'T00:00:00').getTime())&&(!beforeDate||Date.parse(entry.modified)<=new Date(beforeDate+'T23:59:59.999').getTime()))));
 const totalSize=$derived((entries??[]).reduce((sum,entry)=>sum+(entry.dir?0:entry.size),0));
 const allocatedSize=$derived(entries?.every(e=>typeof e.allocated==='number')?entries.reduce((sum,e)=>sum+(e.allocated??0),0):null);
 const filteredSize=$derived(filtered.reduce((sum,entry)=>sum+(entry.dir?0:entry.size),0));
 $effect(()=>{const keys=new Set(filtered.map(entryKey));const kept=selected.filter(name=>keys.has(name));if(kept.length!==selected.length)selected=kept;});
 let flat=$state(false),sort=$state('deleted'),descending=$state(true);
 let restorePreview=$state<{path:string;target:string;conflict:boolean}[]|null>(null);
 const entryKey=(entry:TrashEntry)=>entry.path??join(path,entry.name);
 const ordered=$derived([...filtered].sort((a,b)=>{
  const order=sort==='size'?a.size-b.size:sort==='deleted'?Date.parse(a.modified)-Date.parse(b.modified):(sort==='original'?(a.original??a.name):(a.name)).localeCompare(sort==='original'?(b.original??b.name):b.name,i18n.locale,{numeric:true});
  return (order||a.name.localeCompare(b.name))*(descending?-1:1);
 }));
 async function previewRestore(){busy=true;error='';try{restorePreview=await api.post('/files/trash/preview',{root:root.id,paths:selected});}catch(err){error=fail(err);}finally{busy=false;}}

	let selected = $state<string[]>([]);
	let error = $state('');
	let busy = $state(false);
	// Emptying takes the whole bin of the account (or of all accounts at the
	// top), whatever folder is open; the dialog names exactly that.
	let confirm = $state<{ kind: 'delete' } | { kind: 'empty'; paths: string[]; all: boolean } | null>(null);

	const join = (...parts: string[]) => parts.filter(Boolean).join('/');
	const fail = (err: unknown) => errorText(err instanceof ApiError ? err.code : 'internal');
	const date = $derived(new Intl.DateTimeFormat(i18n.locale, { dateStyle: 'medium', timeStyle: 'short' }));
	// Below the base, as shown in the breadcrumbs.
	const crumbs = $derived((base ? path.slice(base.length) : path).split('/').filter(Boolean));
	// The list of accounts is not a place things were deleted from.
	const atAccounts = $derived(!flat && path === '');
	const owner = $derived(path.split('/')[0]);
	const allSelected = $derived(filtered.length>0 && selected.length === filtered.length);

	async function load() {
		const requested = path, mode=flat;
		try {
			const list = await api.get<TrashEntry[]>(`/files/trash?root=${encodeURIComponent(root.id)}&path=${encodeURIComponent(requested)}&flat=${mode?1:0}`);
			if (requested !== path || mode!==flat) return;
			entries = list;
			selected = selected.filter((name) => list.some((entry) => entryKey(entry) === name));
			error = '';
		} catch (err) {
			if (requested !== path || mode!==flat) return;
			// Taking out the last thing removes the folder of the bin.
			if (err instanceof ApiError && err.code === 'file_not_found' && requested !== base) return open(base);
			entries = [];
			error = fail(err);
		}
	}

	function open(next: string) {
		path = next;
		selected = [];
		entries = null;
		load();
	}

	$effect(() => {
		const start = (root.id, base);
		untrack(() => open(start));
	});

	async function act(action: 'restore' | 'delete', paths: string[]) {
		busy = true;
		error = '';
		try {
			await api.post(`/files/trash/${action}`, { root: root.id, paths });
			selected = [];
			onchanged();
		} catch (err) {
			error = fail(err);
		}
		confirm = null;
  restorePreview=null;
		busy = false;
		load();
	}

	function emptyPaths() {
		if (atAccounts) return (entries ?? []).map((entry) => entry.name);
		return [owner];
	}
</script>

<div class="toolbar">
	<button type="button" class="btn" onclick={onclose}>{t('files.trashBack')}</button>
	<div class="crumbs mono">
		<button type="button" onclick={() => open(base)}>{t('files.trashOf', { name: root.name })}</button>
		{#each (flat?[]:crumbs) as crumb, index (index)}
			<span class="muted">/</span>
			<button type="button" onclick={() => open(join(base, crumbs.slice(0, index + 1).join('/')))}>{crumb}</button>
		{/each}
	</div>
	{#if !flat && root.writable && entries && entries.length > 0}
		<button type="button" class="btn btn-danger" disabled={busy} onclick={() => (confirm = { kind: 'empty', paths: emptyPaths(), all: atAccounts })}>{t('files.trashEmpty')}</button>
	{/if}
</div>

<div class="toolbar">
 <label><input type="checkbox" bind:checked={flat} onchange={()=>open(base)} />{t('files.trashFlat')}</label>
 <label>{t('files.sort')}<select class="field" aria-label={t('files.sort')} bind:value={sort}><option value="deleted">{t('files.trashDeleted')}</option><option value="original" disabled={!flat}>{t('files.trashOriginal')}</option><option value="size">{t('files.size')}</option><option value="name">{t('files.name')}</option></select></label>
 <button class="btn" onclick={()=>descending=!descending}>{descending?'↓':'↑'} {t('files.sortDirection')}</button>
 </div>
{#if flat}<div class="toolbar">
 <label>{t('files.trashFolderFilter')}<input class="field" aria-label={t('files.trashFolderFilter')} bind:value={folderFilter}/></label>
 <label>{t('files.trashAfter')}<input class="field" type="date" aria-label={t('files.trashAfter')} bind:value={afterDate}/></label>
 <label>{t('files.trashBefore')}<input class="field" type="date" aria-label={t('files.trashBefore')} bind:value={beforeDate}/></label>
 <button class="btn" onclick={()=>{folderFilter='';beforeDate='';afterDate='';}}>{t('files.clearFilters')}</button>
 {#if allocatedSize!==null}<p>{t('files.trashAllocated',{size:formatBytes(allocatedSize)})}</p>{/if}
 <p>{t('files.trashSizeSummary',{total:formatBytes(totalSize),shown:formatBytes(filteredSize),count:filtered.length})}</p><p class="muted">{t('files.trashSizeHint')}</p>
 </div>{/if}
<p class="hint muted">{t(admin ? 'files.trashHintAdmin' : 'files.trashHint', { days: root.recycleDays ?? 30 })}</p>
{#if error}<p class="notice-error" role="alert">{error}</p>{/if}

<div class="row header">
	<input type="checkbox" aria-label={t('files.selectAll')} checked={allSelected} onchange={(event) => (selected = event.currentTarget.checked ? filtered.map(entryKey) : [])} />
	<span>{t(atAccounts ? 'files.trashAccount' : 'files.name')}</span>
	<span class="num">{t('files.size')}</span>
	<span class="num">{t('files.trashDeleted')}</span>
</div>
{#each ordered as entry (entryKey(entry))}
	<div class="row" class:picked={selected.includes(entryKey(entry))}>
		<input type="checkbox" aria-label={t('files.select', { name: entry.name })} checked={selected.includes(entryKey(entry))}
			onchange={(event) => (selected = event.currentTarget.checked ? [...selected, entryKey(entry)] : selected.filter((name) => name !== entryKey(entry)))} />
		{#if entry.dir}
			<button type="button" class="entry" onclick={() => open(join(path, entry.name))}><Icon name="folder" size={16} /><span>{entry.name}</span></button>
		{:else}
			<span class="entry"><Icon name="file" size={16} /><span>{entry.name}{#if flat}<small class="muted">{root.name}/{entry.original}</small>{/if}</span></span>
		{/if}
		<span class="mono num muted">{entry.dir ? '–' : formatBytes(entry.size)}</span>
		<span class="num muted">{atAccounts ? '' : date.format(new Date(entry.modified))}</span>
	</div>
{:else}
	{#if entries}<EmptyState inline title={t('files.trashNone')} />{/if}
{/each}

{#if selected.length > 0 && root.writable}
	<div class="actions">
		<span class="muted count">{t('files.selected', { count: selected.length })}</span>
		{#if !atAccounts}
			<button type="button" class="btn" disabled={busy} onclick={previewRestore}>{t('files.trashRestore')}</button>
		{/if}
		<button type="button" class="btn btn-danger" disabled={busy} onclick={() => (confirm = { kind: 'delete' })}>{t('files.trashDeleteForever')}</button>
	</div>
{/if}

{#if restorePreview}
 <Dialog title={t('files.trashRestorePreview')} locked={busy} onclose={()=>restorePreview=null}>
 <p>{t('files.trashRestoreHint')}</p>
 <div class="restore-list">{#each restorePreview as item (item.path)}<div><p class="mono">{root.name}/{item.target}</p>{#if item.conflict}<p class="notice-warn">{t('files.trashRestoreConflict')}</p>{/if}</div>{/each}</div>
 <div class="dialog-actions"><button class="btn" disabled={busy} onclick={()=>restorePreview=null}>{t('common.cancel')}</button><button class="btn btn-primary" disabled={busy} onclick={()=>act('restore',restorePreview!.map(item=>item.path))}>{t('files.trashRestore')}</button></div>
 </Dialog>
{/if}
{#if confirm}
	{@const kind = confirm.kind}
	{@const emptying = confirm.kind === 'empty' ? confirm : null}
	<Dialog title={t(kind === 'empty' ? 'files.trashEmptyTitle' : 'files.trashDeleteTitle', { count: selected.length })} locked={busy} onclose={() => (confirm = null)}>
		{#if emptying}
			<p>{emptying.all ? t('files.trashEmptyAll', { share: root.name, count: emptying.paths.length }) : t('files.trashEmptyAccount', { share: root.name, account: emptying.paths[0] ?? '' })}</p>
			{#if emptying.all}<ul class="restore-list">{#each emptying.paths as account (account)}<li class="mono">{account}</li>{/each}</ul>{/if}
		{/if}
		<p class="notice-error">{t('files.trashForeverText')}</p>
        {#if kind==='delete'}<ul class="restore-list">{#each selected as name}<li class="mono">{entries?.find(e=>entryKey(e)===name)?.original??name}</li>{/each}</ul>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (confirm = null)} disabled={busy}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-danger" disabled={busy}
				onclick={() => act('delete', emptying ? emptying.paths : selected)}>
				{t(kind === 'empty' ? 'files.trashEmpty' : 'files.trashDeleteForever')}
			</button>
		</div>
	</Dialog>
{/if}

<style>small{display:block;overflow-wrap:anywhere;white-space:normal;}.restore-list{max-height:50vh;overflow:auto;overflow-wrap:anywhere;}
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
		border-radius: var(--radius-small);
		color: var(--text);
		font: inherit;
		cursor: pointer;
	}
	.crumbs button:hover {
		background: var(--chip);
	}
	.hint {
		margin: 0;
		padding: 10px 14px;
		border-bottom: 1px solid var(--panel-border);
		font-size: 0.8125rem;
	}
	.notice-error {
		margin: 10px 14px;
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
	@media (max-width: 760px) {
		.row {
			grid-template-columns: 22px minmax(0, 1fr) 80px;
		}
		.row > :nth-child(4) {
			display: none;
		}
	}
</style>
