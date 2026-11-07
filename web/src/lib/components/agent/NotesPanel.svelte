<script lang="ts">
	// Notes of the KI-Agent (PLAN 57): Markdown files in folders below
	// KI-Agent/<konto>/Notizen, the same files the file manager and SMB show.
	// A list with folders, search and pinned notes, and an editor with a
	// format bar; it saves by itself and never over a change made elsewhere.
	import Icon from '#lib/components/Icon.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import { api } from '#lib/api.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import { formatDateTime } from '#lib/format.ts';
	import { fly } from 'svelte/transition';
	import { cubicOut } from 'svelte/easing';
	import { onDestroy, onMount } from 'svelte';
	import { noteMove, notePath, applyFormat, type Format, type NoteInfo } from '#lib/agent-notes.ts';

	let { calm = false, onclose }: { calm?: boolean; onclose: () => void } = $props();

	let folders = $state<string[]>([]);
	let notes = $state<NoteInfo[]>([]);
	let loaded = $state(false);
	let error = $state('');
	let query = $state('');
	/** '' all notes, '\u0000pinned' the pinned ones, else a folder. */
	let folder = $state('');
	const PINNED = '\u0000pinned';
	let open = $state<{ path: string; title: string; content: string; modified: string } | null>(
		null,
	);
	let saving = $state<'idle' | 'saving' | 'saved' | 'conflict'>('idle');
	let menuFor = $state<string | null>(null);
	let dialog = $state<
		| { kind: 'folder' }
		| { kind: 'rename'; path: string; isFolder: boolean; name: string }
		| { kind: 'move'; path: string }
		| { kind: 'delete'; path: string; isFolder: boolean }
		| null
	>(null);
	let input = $state('');
	let editor = $state<HTMLTextAreaElement>();
	let timer: ReturnType<typeof setTimeout> | undefined;

	async function load() {
		try {
			const r = await api.get<{ folders: string[]; notes: NoteInfo[] }>('/agent/notes');
			folders = r.folders;
			notes = r.notes;
			error = '';
		} catch (e) {
			error = apiErrorText(e);
		}
		loaded = true;
	}
	onMount(load);
	onDestroy(() => {
		if (timer) {
			clearTimeout(timer);
			void save();
		}
	});

	const shown = $derived.by(() => {
		const q = query.trim().toLowerCase();
		let list = notes;
		if (folder === PINNED) list = list.filter((n) => n.pinned);
		else if (folder)
			list = list.filter((n) => n.folder === folder || n.folder.startsWith(folder + '/'));
		if (q) list = list.filter((n) => (n.title + ' ' + (n.preview ?? '')).toLowerCase().includes(q));
		return [...list].sort((a, b) => Number(!!b.pinned) - Number(!!a.pinned));
	});
	const count = (f: string) =>
		notes.filter((n) => n.folder === f || n.folder.startsWith(f + '/')).length;

	async function openNote(path: string) {
		await flush();
		try {
			const r = await api.get<{ path: string; content: string; modified: string }>(
				`/agent/notes/file?path=${encodeURIComponent(path)}`,
			);
			open = { ...r, title: notes.find((n) => n.path === path)?.title ?? path };
			saving = 'idle';
		} catch (e) {
			error = apiErrorText(e);
		}
	}

	async function newNote() {
		await flush();
		const base = t('agent.notes.untitled');
		const inFolder = folder && folder !== PINNED ? folder : '';
		let path = notePath(inFolder, base);
		for (let i = 2; notes.some((n) => n.path === path); i++)
			path = notePath(inFolder, `${base} ${i}`);
		try {
			const r = await api.put<{ path: string; modified: string }>('/agent/notes/file', {
				path,
				content: '',
			});
			await load();
			open = {
				path: r.path,
				title: notes.find((n) => n.path === r.path)?.title ?? base,
				content: '',
				modified: r.modified,
			};
			saving = 'idle';
			queueMicrotask(() => editor?.focus());
		} catch (e) {
			error = apiErrorText(e);
		}
	}

	function changed() {
		saving = 'saving';
		if (timer) clearTimeout(timer);
		timer = setTimeout(() => void save(), 700);
	}
	async function flush() {
		if (timer) {
			clearTimeout(timer);
			timer = undefined;
			await save();
		}
	}
	async function save() {
		timer = undefined;
		const note = open;
		if (!note) return;
		try {
			const r = await api.put<{ modified: string }>('/agent/notes/file', {
				path: note.path,
				content: note.content,
				modified: note.modified,
			});
			note.modified = r.modified;
			saving = 'saved';
			const listed = notes.find((n) => n.path === note.path);
			if (listed) {
				listed.preview = note.content
					.replace(/[#>*`-]|\[[ x]\]/g, ' ')
					.replace(/\s+/g, ' ')
					.trim()
					.slice(0, 140);
				listed.modified = r.modified;
			}
		} catch (e) {
			saving = (e as { status?: number })?.status === 409 ? 'conflict' : 'idle';
			if (saving !== 'conflict') error = apiErrorText(e);
		}
	}
	async function reload() {
		if (open) await openNote(open.path);
	}

	function format(f: Format) {
		if (!editor || !open) return;
		const r = applyFormat(open.content, editor.selectionStart, editor.selectionEnd, f);
		open.content = r.text;
		changed();
		queueMicrotask(() => {
			editor?.focus();
			editor?.setSelectionRange(r.start, r.end);
		});
	}

	async function pin(n: NoteInfo) {
		menuFor = null;
		try {
			await api.put('/agent/notes/pin', { path: n.path, pinned: !n.pinned });
			n.pinned = !n.pinned;
		} catch (e) {
			error = apiErrorText(e);
		}
	}

	async function run(action: () => Promise<unknown>) {
		try {
			await action();
			dialog = null;
			await load();
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	const makeFolder = () =>
		run(() =>
			api.post('/agent/notes/folders', {
				path: noteMove('', folder && folder !== PINNED ? folder : '', input.trim(), true),
			}),
		);
	function rename(d: { path: string; isFolder: boolean }) {
		const to = noteMove(
			d.path,
			d.path.includes('/') ? d.path.slice(0, d.path.lastIndexOf('/')) : '',
			input.trim(),
			d.isFolder,
		);
		return run(async () => {
			await flush();
			await api.post('/agent/notes/move', { from: d.path, to });
			if (open?.path === d.path) open = { ...open, path: to, title: input.trim() };
			else if (d.isFolder && open?.path.startsWith(d.path + '/'))
				open = { ...open, path: to + open.path.slice(d.path.length) };
			if (folder === d.path) folder = to;
		});
	}
	function move(path: string, into: string) {
		const name = path.slice(path.lastIndexOf('/') + 1);
		const to = into ? `${into}/${name}` : name;
		return run(async () => {
			await flush();
			await api.post('/agent/notes/move', { from: path, to });
			if (open?.path === path) open = { ...open, path: to };
		});
	}
	function remove(d: { path: string; isFolder: boolean }) {
		return run(async () => {
			if (timer) clearTimeout(timer);
			timer = undefined;
			await api.delete(`/agent/notes?path=${encodeURIComponent(d.path)}`);
			if (open && (open.path === d.path || open.path.startsWith(d.path + '/'))) open = null;
			if (folder === d.path || folder.startsWith(d.path + '/')) folder = '';
		});
	}
	const depth = (f: string) => f.split('/').length - 1;
	const leaf = (f: string) => f.slice(f.lastIndexOf('/') + 1);
</script>

<svelte:window
	onkeydown={(e) => {
		if (e.key === 'Escape' && !dialog) {
			if (menuFor) menuFor = null;
			else if (open) void flush().then(() => (open = null));
			else onclose();
		}
	}}
/>

<div class="scrim" role="presentation" onclick={onclose}></div>
<div
	class="notes"
	class:editing={!!open}
	role="dialog"
	aria-modal="true"
	aria-label={t('agent.notes.title')}
	transition:fly={{ x: 420, duration: calm ? 0 : 280, opacity: 1, easing: cubicOut }}
>
	<section class="list" aria-label={t('agent.notes.title')}>
		<header>
			<button type="button" class="ib" aria-label={t('common.close')} onclick={onclose}
				><Icon name="close" size={20} /></button
			>
			<h2>{t('agent.notes.title')}</h2>
			<button
				type="button"
				class="ib"
				aria-label={t('agent.notes.newFolder')}
				title={t('agent.notes.newFolder')}
				onclick={() => ((input = ''), (dialog = { kind: 'folder' }))}
				><Icon name="folder" size={18} /></button
			>
			<button
				type="button"
				class="ib accent"
				aria-label={t('agent.notes.new')}
				title={t('agent.notes.new')}
				onclick={newNote}><Icon name="edit" size={18} /></button
			>
		</header>
		<label class="search">
			<Icon name="search" size={16} />
			<input
				type="search"
				placeholder={t('agent.notes.search')}
				aria-label={t('agent.notes.search')}
				bind:value={query}
			/>
		</label>
		<div class="scroll">
			{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
			<div class="folders" role="list">
				<button type="button" class="folder" class:on={folder === ''} onclick={() => (folder = '')}
					><Icon name="fileText" size={16} /><span>{t('agent.notes.all')}</span><small
						>{notes.length}</small
					></button
				>
				{#if notes.some((n) => n.pinned)}
					<button
						type="button"
						class="folder"
						class:on={folder === PINNED}
						onclick={() => (folder = PINNED)}
						><Icon name="pin" size={16} /><span>{t('agent.notes.pinned')}</span><small
							>{notes.filter((n) => n.pinned).length}</small
						></button
					>
				{/if}
				{#each folders as f (f)}
					<div class="frow" style:--d={depth(f)}>
						<button
							type="button"
							class="folder"
							class:on={folder === f}
							onclick={() => (folder = f)}
							><Icon name="folder" size={16} /><span>{leaf(f)}</span><small>{count(f)}</small
							></button
						>
						<button
							type="button"
							class="more"
							aria-label={t('agent.notes.folderMenu', { name: leaf(f) })}
							onclick={() => (menuFor = menuFor === 'f:' + f ? null : 'f:' + f)}
							><Icon name="more" size={16} /></button
						>
						{#if menuFor === 'f:' + f}
							<div class="pop" role="menu">
								<button
									role="menuitem"
									type="button"
									onclick={() => (
										(menuFor = null),
										(input = leaf(f)),
										(dialog = { kind: 'rename', path: f, isFolder: true, name: leaf(f) })
									)}>{t('agent.notes.rename')}</button
								>
								<button
									role="menuitem"
									type="button"
									class="danger"
									onclick={() => (
										(menuFor = null),
										(dialog = { kind: 'delete', path: f, isFolder: true })
									)}>{t('common.delete')}</button
								>
							</div>
						{/if}
					</div>
				{/each}
			</div>
			<div class="items">
				{#each shown as n (n.path)}
					<div class="nrow">
						<button
							type="button"
							class="note"
							class:on={open?.path === n.path}
							onclick={() => openNote(n.path)}
						>
							<span class="nt"
								>{#if n.pinned}<Icon name="pin" size={13} />{/if}<strong>{n.title}</strong></span
							>
							<small
								><span>{formatDateTime(new Date(n.modified))}</span
								>{#if n.folder && folder !== n.folder}<span class="in">
										· {n.folder}</span
									>{/if}</small
							>
							{#if n.preview}<span class="pv">{n.preview}</span>{/if}
						</button>
						<button
							type="button"
							class="more"
							aria-label={t('agent.notes.noteMenu', { name: n.title })}
							onclick={() => (menuFor = menuFor === n.path ? null : n.path)}
							><Icon name="more" size={16} /></button
						>
						{#if menuFor === n.path}
							<div class="pop" role="menu">
								<button role="menuitem" type="button" onclick={() => pin(n)}
									>{n.pinned ? t('agent.notes.unpin') : t('agent.notes.pin')}</button
								>
								<button
									role="menuitem"
									type="button"
									onclick={() => ((menuFor = null), (dialog = { kind: 'move', path: n.path }))}
									>{t('agent.notes.move')}</button
								>
								<button
									role="menuitem"
									type="button"
									onclick={() => (
										(menuFor = null),
										(input = n.title),
										(dialog = { kind: 'rename', path: n.path, isFolder: false, name: n.title })
									)}>{t('agent.notes.rename')}</button
								>
								<button
									role="menuitem"
									type="button"
									class="danger"
									onclick={() => (
										(menuFor = null),
										(dialog = { kind: 'delete', path: n.path, isFolder: false })
									)}>{t('common.delete')}</button
								>
							</div>
						{/if}
					</div>
				{:else}
					{#if loaded}<p class="empty">
							{query ? t('agent.notes.none') : t('agent.notes.empty')}
						</p>{/if}
				{/each}
			</div>
		</div>
	</section>

	{#if open}
		{#key open.path}
			<section
				class="editor"
				aria-label={open.title}
				in:fly={{ x: 140, duration: calm ? 0 : 280, opacity: 0, easing: cubicOut }}
			>
				<header>
					<button
						type="button"
						class="ib back"
						aria-label={t('agent.set.back')}
						onclick={() => void flush().then(() => (open = null))}
						><Icon name="chevronLeft" size={20} /></button
					>
					<h2>{open.title}</h2>
					<span class="state" role="status"
						>{saving === 'saving'
							? t('agent.notes.saving')
							: saving === 'saved'
								? t('agent.notes.saved')
								: ''}</span
					>
				</header>
				{#if saving === 'conflict'}
					<p class="conflict" role="alert">
						{t('agent.notes.conflict')}
						<button type="button" class="btn" onclick={reload}>{t('agent.notes.reload')}</button>
					</p>
				{/if}
				<div class="fmt" role="toolbar" aria-label={t('agent.notes.format')}>
					<button
						type="button"
						aria-label={t('agent.notes.heading')}
						title={t('agent.notes.heading')}
						onclick={() => format('heading')}><b>H</b></button
					>
					<button
						type="button"
						aria-label={t('agent.notes.bold')}
						title={t('agent.notes.bold')}
						onclick={() => format('bold')}><b>B</b></button
					>
					<button
						type="button"
						aria-label={t('agent.notes.italic')}
						title={t('agent.notes.italic')}
						onclick={() => format('italic')}><i>I</i></button
					>
					<button
						type="button"
						aria-label={t('agent.notes.list')}
						title={t('agent.notes.list')}
						onclick={() => format('list')}><Icon name="list" size={17} /></button
					>
					<button
						type="button"
						aria-label={t('agent.notes.check')}
						title={t('agent.notes.check')}
						onclick={() => format('check')}><Icon name="check" size={17} /></button
					>
					<button
						type="button"
						aria-label={t('agent.notes.rule')}
						title={t('agent.notes.rule')}
						onclick={() => format('rule')}>—</button
					>
				</div>
				<textarea
					bind:this={editor}
					bind:value={open.content}
					oninput={changed}
					onblur={() => void flush()}
					aria-label={t('agent.notes.text')}
					placeholder={t('agent.notes.placeholder')}
					spellcheck="true"></textarea>
			</section>
		{/key}
	{:else}
		<section class="editor blank" aria-hidden="true">
			<Icon name="fileText" size={40} />
			<p>{t('agent.notes.pick')}</p>
		</section>
	{/if}
</div>

{#if dialog?.kind === 'folder'}
	<Dialog title={t('agent.notes.newFolder')} onclose={() => (dialog = null)}>
		<form onsubmit={(e) => (e.preventDefault(), input.trim() && void makeFolder())}>
			<label class="stack"
				><span>{t('agent.notes.folderName')}</span><input
					class="field"
					bind:value={input}
					maxlength="120"
				/></label
			>
			<div class="dialog-actions">
				<button type="submit" class="btn btn-primary" disabled={!input.trim()}
					>{t('agent.notes.create')}</button
				>
				<button type="button" class="btn" onclick={() => (dialog = null)}
					>{t('common.cancel')}</button
				>
			</div>
		</form>
	</Dialog>
{:else if dialog?.kind === 'rename'}
	{@const d = dialog}
	<Dialog title={t('agent.notes.rename')} onclose={() => (dialog = null)}>
		<form
			onsubmit={(e) => (
				e.preventDefault(),
				input.trim() && input.trim() !== d.name && void rename(d)
			)}
		>
			<label class="stack"
				><span>{t('agent.notes.name')}</span><input
					class="field"
					bind:value={input}
					maxlength="117"
				/></label
			>
			<div class="dialog-actions">
				<button type="submit" class="btn btn-primary" disabled={!input.trim()}
					>{t('common.save')}</button
				>
				<button type="button" class="btn" onclick={() => (dialog = null)}
					>{t('common.cancel')}</button
				>
			</div>
		</form>
	</Dialog>
{:else if dialog?.kind === 'move'}
	{@const d = dialog}
	{@const from = d.path.includes('/') ? d.path.slice(0, d.path.lastIndexOf('/')) : ''}
	<Dialog title={t('agent.notes.moveTitle')} onclose={() => (dialog = null)}>
		<div class="choose">
			<button type="button" class="btn" disabled={from === ''} onclick={() => move(d.path, '')}
				><Icon name="fileText" size={15} /> {t('agent.notes.top')}</button
			>
			{#each folders as f (f)}
				<button
					type="button"
					class="btn"
					style:padding-left={`${12 + depth(f) * 16}px`}
					disabled={from === f}
					onclick={() => move(d.path, f)}><Icon name="folder" size={15} /> {leaf(f)}</button
				>
			{/each}
		</div>
	</Dialog>
{:else if dialog?.kind === 'delete'}
	{@const d = dialog}
	<Dialog
		title={d.isFolder ? t('agent.notes.deleteFolderTitle') : t('agent.notes.deleteTitle')}
		onclose={() => (dialog = null)}
	>
		<p>
			{d.isFolder
				? t('agent.notes.deleteFolderText', { name: leaf(d.path), count: count(d.path) })
				: t('agent.notes.deleteText', {
						name: notes.find((n) => n.path === d.path)?.title ?? d.path,
					})}
		</p>
		<div class="dialog-actions">
			<button type="button" class="btn btn-danger" onclick={() => remove(d)}
				>{t('common.delete')}</button
			>
			<button type="button" class="btn" onclick={() => (dialog = null)}>{t('common.cancel')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.scrim {
		position: fixed;
		inset: 0;
		z-index: 70;
		background: rgb(0 0 0 / 0.45);
	}
	.notes {
		position: fixed;
		z-index: 71;
		top: 0;
		right: 0;
		bottom: 0;
		width: min(980px, 100%);
		display: grid;
		grid-template-columns: minmax(260px, 340px) minmax(0, 1fr);
		color: var(--text);
		background: var(--bg);
		border-left: 1px solid var(--border);
		box-shadow: var(--shadow-menu, -18px 0 44px rgb(0 0 0 / 0.5));
	}
	section {
		display: flex;
		flex-direction: column;
		min-height: 0;
		min-width: 0;
	}
	section > :not(textarea, .scroll) {
		flex-shrink: 0;
	}
	.list {
		border-right: 1px solid color-mix(in srgb, var(--text) 9%, transparent);
	}
	header {
		display: flex;
		align-items: center;
		gap: 8px;
		padding: calc(env(safe-area-inset-top, 0px) + 12px) 12px 8px;
	}
	h2 {
		flex: 1;
		min-width: 0;
		margin: 0;
		font-size: var(--text-md);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.ib {
		width: 40px;
		height: 40px;
		flex: none;
		border-radius: 13px;
		display: grid;
		place-items: center;
		border: 1px solid color-mix(in srgb, var(--text) 10%, transparent);
		background: color-mix(in srgb, var(--text) 6%, transparent);
		color: var(--text);
		cursor: pointer;
	}
	.ib.accent {
		background: var(--accent);
		color: var(--on-accent, #fff);
		border-color: transparent;
	}
	.editor .back {
		display: none;
	}
	.search {
		display: flex;
		align-items: center;
		gap: 8px;
		margin: 0 12px 8px;
		padding: 0 12px;
		min-height: 40px;
		border-radius: 12px;
		background: color-mix(in srgb, var(--text) 6%, transparent);
		color: var(--muted);
	}
	.search input {
		flex: 1;
		min-width: 0;
		border: 0;
		outline: none;
		background: transparent;
		color: var(--text);
		font: inherit;
		font-size: max(16px, var(--text-base));
	}
	.scroll {
		flex: 1;
		min-height: 0;
		overflow-y: auto;
		padding: 0 8px calc(env(safe-area-inset-bottom, 0px) + 16px);
	}
	.folders,
	.items {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.folders {
		padding-bottom: 8px;
		margin-bottom: 8px;
		border-bottom: 1px solid color-mix(in srgb, var(--text) 9%, transparent);
	}
	.folders > *,
	.items > * {
		flex-shrink: 0;
	}
	.frow,
	.nrow {
		position: relative;
		display: flex;
		align-items: stretch;
	}
	.frow {
		padding-left: calc(var(--d, 0) * 16px);
	}
	.folder,
	.note {
		flex: 1;
		min-width: 0;
		border: 0;
		background: none;
		color: var(--text);
		font: inherit;
		text-align: left;
		border-radius: 10px;
		cursor: pointer;
	}
	.folder {
		display: flex;
		align-items: center;
		gap: 10px;
		min-height: 40px;
		padding: 6px 10px;
	}
	.folder :global(svg) {
		color: var(--muted);
		flex: none;
	}
	.folder span {
		flex: 1;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.folder small {
		color: var(--muted);
	}
	.note {
		display: grid;
		gap: 2px;
		padding: 9px 10px;
	}
	.nt {
		display: flex;
		align-items: center;
		gap: 6px;
		min-width: 0;
	}
	.nt strong {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.note small {
		color: var(--muted);
		font-size: var(--text-xs);
	}
	.pv {
		color: var(--muted);
		font-size: var(--text-sm);
		display: -webkit-box;
		-webkit-line-clamp: 2;
		line-clamp: 2;
		-webkit-box-orient: vertical;
		overflow: hidden;
	}
	.folder:hover,
	.note:hover {
		background: color-mix(in srgb, var(--text) 6%, transparent);
	}
	.folder.on,
	.note.on {
		background: color-mix(in srgb, var(--accent) 18%, transparent);
		color: color-mix(in srgb, var(--accent) 65%, var(--text));
		font-weight: 600;
	}
	.more {
		flex: none;
		width: 34px;
		border: 0;
		background: none;
		color: var(--muted);
		border-radius: 10px;
		cursor: pointer;
		display: grid;
		place-items: center;
	}
	.pop {
		position: absolute;
		z-index: 5;
		right: 6px;
		top: calc(100% - 4px);
		min-width: 170px;
		display: grid;
		padding: 4px;
		border-radius: 12px;
		background: var(--menu-bg, var(--surface));
		border: 1px solid var(--menu-border, var(--border));
		box-shadow: var(--menu-shadow, 0 12px 30px rgb(0 0 0 / 0.4));
	}
	.pop button {
		text-align: left;
		padding: 9px 12px;
		border: 0;
		border-radius: 8px;
		background: none;
		color: var(--text);
		font: inherit;
		cursor: pointer;
	}
	.pop button:hover {
		background: color-mix(in srgb, var(--text) 8%, transparent);
	}
	.pop .danger {
		color: var(--status-error, #e5484d);
	}
	.empty {
		color: var(--muted);
		text-align: center;
		padding: 24px 8px;
	}
	.editor .state {
		color: var(--muted);
		font-size: var(--text-xs);
		min-width: 70px;
		text-align: right;
	}
	.fmt {
		display: flex;
		gap: 4px;
		padding: 0 12px 8px;
		flex-wrap: wrap;
	}
	.fmt button {
		min-width: 38px;
		height: 36px;
		border-radius: 10px;
		border: 1px solid color-mix(in srgb, var(--text) 10%, transparent);
		background: color-mix(in srgb, var(--text) 5%, transparent);
		color: var(--text);
		font: inherit;
		cursor: pointer;
		display: grid;
		place-items: center;
	}
	textarea {
		flex: 1;
		min-height: 0;
		resize: none;
		border: 0;
		outline: none;
		padding: 8px 18px calc(env(safe-area-inset-bottom, 0px) + 18px);
		background: transparent;
		color: var(--text);
		font: inherit;
		font-size: max(16px, var(--text-base));
		line-height: 1.6;
	}
	.conflict {
		margin: 0 12px 8px;
		padding: 8px 12px;
		border-radius: 10px;
		background: var(--warn-bg);
		color: var(--warn-text);
		display: flex;
		gap: 10px;
		align-items: center;
		flex-wrap: wrap;
	}
	.blank {
		align-items: center;
		justify-content: center;
		gap: 10px;
		color: var(--muted);
	}
	.choose {
		display: grid;
		gap: var(--space-2);
	}
	.choose .btn {
		justify-content: flex-start;
		gap: 8px;
	}
	.stack {
		display: grid;
		gap: 6px;
		margin-bottom: var(--space-3);
	}
	/* The phone: the list, and the note slides in over it. */
	@media (max-width: 760px) {
		.notes {
			grid-template-columns: minmax(0, 1fr);
			border-left: 0;
		}
		.list {
			border-right: 0;
		}
		.editor {
			position: absolute;
			inset: 0;
			background:
				linear-gradient(
					color-mix(in srgb, var(--surface) 92%, transparent),
					color-mix(in srgb, var(--surface) 92%, transparent)
				),
				var(--bg, #0b1622);
		}
		.editor .back {
			display: grid;
		}
		.blank {
			display: none;
		}
	}
</style>
