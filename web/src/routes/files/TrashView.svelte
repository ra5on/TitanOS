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
	let entries = $state<FileEntry[] | null>(null);
	let selected = $state<string[]>([]);
	let error = $state('');
	let busy = $state(false);
	let confirm = $state<{ kind: 'delete' | 'empty' } | null>(null);

	const join = (...parts: string[]) => parts.filter(Boolean).join('/');
	const fail = (err: unknown) => errorText(err instanceof ApiError ? err.code : 'internal');
	const date = $derived(new Intl.DateTimeFormat(i18n.locale, { dateStyle: 'medium', timeStyle: 'short' }));
	// Below the base, as shown in the breadcrumbs.
	const crumbs = $derived((base ? path.slice(base.length) : path).split('/').filter(Boolean));
	// The list of accounts is not a place things were deleted from.
	const atAccounts = $derived(path === '');
	const owner = $derived(path.split('/')[0]);
	const allSelected = $derived(!!entries?.length && selected.length === entries.length);

	async function load() {
		const requested = path;
		try {
			const list = await api.get<FileEntry[]>(`/files/trash?root=${encodeURIComponent(root.id)}&path=${encodeURIComponent(requested)}`);
			if (requested !== path) return;
			entries = list;
			selected = selected.filter((name) => list.some((entry) => entry.name === name));
			error = '';
		} catch (err) {
			if (requested !== path) return;
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
		{#each crumbs as crumb, index (index)}
			<span class="muted">/</span>
			<button type="button" onclick={() => open(join(base, crumbs.slice(0, index + 1).join('/')))}>{crumb}</button>
		{/each}
	</div>
	{#if root.writable && entries && entries.length > 0}
		<button type="button" class="btn btn-danger" disabled={busy} onclick={() => (confirm = { kind: 'empty' })}>{t('files.trashEmpty')}</button>
	{/if}
</div>

<p class="hint muted">{t(admin ? 'files.trashHintAdmin' : 'files.trashHint', { days: root.recycleDays ?? 30 })}</p>
{#if error}<p class="notice-error" role="alert">{error}</p>{/if}

<div class="row header">
	<input type="checkbox" aria-label={t('files.selectAll')} checked={allSelected} onchange={(event) => (selected = event.currentTarget.checked ? (entries ?? []).map((entry) => entry.name) : [])} />
	<span>{t(atAccounts ? 'files.trashAccount' : 'files.name')}</span>
	<span class="num">{t('files.size')}</span>
	<span class="num">{t('files.trashDeleted')}</span>
</div>
{#each entries ?? [] as entry (entry.name)}
	<div class="row" class:picked={selected.includes(entry.name)}>
		<input type="checkbox" aria-label={t('files.select', { name: entry.name })} checked={selected.includes(entry.name)}
			onchange={(event) => (selected = event.currentTarget.checked ? [...selected, entry.name] : selected.filter((name) => name !== entry.name))} />
		{#if entry.dir}
			<button type="button" class="entry" onclick={() => open(join(path, entry.name))}><Icon name="folder" size={16} /><span>{entry.name}</span></button>
		{:else}
			<span class="entry"><Icon name="file" size={16} /><span>{entry.name}</span></span>
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
			<button type="button" class="btn" disabled={busy} onclick={() => act('restore', selected.map((name) => join(path, name)))}>{t('files.trashRestore')}</button>
		{/if}
		<button type="button" class="btn btn-danger" disabled={busy} onclick={() => (confirm = { kind: 'delete' })}>{t('files.trashDeleteForever')}</button>
	</div>
{/if}

{#if confirm}
	{@const kind = confirm.kind}
	<Dialog title={t(kind === 'empty' ? 'files.trashEmptyTitle' : 'files.trashDeleteTitle', { count: selected.length })} onclose={() => (confirm = null)}>
		<p class="notice-error">{t('files.trashForeverText')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (confirm = null)}>{t('common.cancel')}</button>
			<button type="button" class="btn btn-danger" disabled={busy}
				onclick={() => act('delete', kind === 'empty' ? emptyPaths() : selected.map((name) => join(path, name)))}>
				{t(kind === 'empty' ? 'files.trashEmpty' : 'files.trashDeleteForever')}
			</button>
		</div>
	</Dialog>
{/if}

<style>
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
