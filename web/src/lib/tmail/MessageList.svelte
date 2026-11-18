<script lang="ts" module>
	export type MailItem = { folder: string; uid: number };
</script>

<script lang="ts">
	// The list of a folder (1.05), overhauled in 1.12 (owner 18 Nov 2026):
	// sender pictures, the preview titand cut at a word (wrapped, never
	// clamped), marks for attachment, flagged, answered and forwarded;
	// groups Heute, Gestern, Diese Woche, Älter; conversations as one entry
	// with their count (off per device); quick actions on hover and focus,
	// on the phone a swipe; selecting several (box, Shift, Ctrl/Cmd) with a
	// bar for all of them; dragging onto a folder of the same mailbox; the
	// search in this folder or in all folders with sender, attachment and
	// period. Unread looks as before. More pages load when the end comes
	// into sight.
	import { untrack } from 'svelte';
	import Dialog from '../components/Dialog.svelte';
	import Icon from '../components/Icon.svelte';
	import { apiErrorText, i18n, t } from '../i18n/index.svelte.ts';
	import Confirm from '../nvr/Confirm.svelte';
	import { mailApi, type Filters } from './api.ts';
	import { groupByDay, type DayGroup } from './groups.ts';
	import MailEntry, { type EntryAction } from './MailEntry.svelte';
	import { isBool, rangeOf, readPref, writePref } from './prefs.ts';
	import { findFolder, folderName, mail, mailDrag, sortFolders } from './store.svelte.ts';
	import { rowKey, threadRows, type Row, type Thread } from './threads.ts';
	import type { Flags } from './types.ts';

	type Filter = 'all' | 'unread' | 'flagged';
	type Scope = 'folder' | 'all';

	let {
		accountId,
		folder,
		selected = null,
		selectedFolder = '',
		heading,
		phone = false,
		drag = true,
		onopen,
		oncompose,
		onchanged,
		onremoved,
		onloaded,
	}: {
		accountId: string;
		folder: string;
		selected?: number | null;
		/** The folder of the open message ('' = this folder). */
		selectedFolder?: string;
		heading: string;
		phone?: boolean;
		/** Rows can be dragged onto folders (the desktop's columns). */
		drag?: boolean;
		onopen: (uid: number, folder: string) => void;
		/** A button "Neue E-Mail" in the list (narrow windows and the phone). */
		oncompose?: () => void;
		/** Something changed the folders' counts. */
		onchanged?: () => void;
		/** Mails left the list; `next` is the one shown after them. */
		onremoved?: (items: MailItem[], next: MailItem | null) => void;
		/** A list arrived from titand (the overview names the time). */
		onloaded?: (at: Date) => void;
	} = $props();

	let rows = $state<Row[]>([]);
	let next = $state<number | null>(null);
	let total = $state(0);
	let loading = $state(false);
	let error = $state('');
	let loaded = $state(false);
	let search = $state('');
	let q = $state('');
	let filter = $state<Filter>('all');
	let scope = $state<Scope>('folder');
	let moreOpen = $state(false);
	let fromText = $state('');
	let from = $state('');
	let attachment = $state(false);
	let since = $state('');
	let until = $state('');
	let searched = $state<{ folders: number; truncated: boolean } | null>(null);
	let now = $state(new Date());
	let threadsOn = $state(readPref('threads', true, isBool));
	let expanded = $state<Set<string>>(new Set());
	let picked = $state<Set<string>>(new Set());
	let anchor = '';
	let notice = $state('');
	let actionError = $state('');
	let busy = $state(false);
	let confirmItems = $state<Row[] | null>(null);
	let moveItems = $state<Row[] | null>(null);
	let asked = 0;
	let controller: AbortController | null = null;
	let more = $state<HTMLElement>();
	let searchField = $state<HTMLInputElement>();

	const filters = $derived<Filters>({
		q,
		unread: filter === 'unread',
		flagged: filter === 'flagged',
		from,
		attachment,
		since,
		until,
	});
	const hasCriteria = $derived(!!(q || from || attachment || since || until || filter !== 'all'));
	const needCriteria = $derived(scope === 'all' && !hasCriteria);
	const role = (path: string) => findFolder(accountId, path)?.role ?? '';
	const nameOf = (path: string) => {
		const f = findFolder(accountId, path);
		return f ? folderName(f) : path;
	};

	async function load(append: boolean, quiet = false) {
		const n = ++asked;
		controller?.abort();
		const own = (controller = new AbortController());
		if (!quiet) loading = true;
		error = '';
		try {
			if (scope === 'all') {
				if (!hasCriteria) {
					rows = [];
					total = 0;
					next = null;
					searched = null;
					loaded = true;
					return;
				}
				const res = await mailApi.search(accountId, filters, own.signal);
				if (n !== asked) return;
				rows = res.messages.map((m) => ({ ...m, folder: m.folder }));
				total = res.total;
				next = null;
				searched = { folders: res.folders, truncated: res.truncated };
			} else {
				const page = await mailApi.messages(
					accountId,
					{ folder, before: append ? next : null, ...filters },
					own.signal,
				);
				if (n !== asked) return;
				const got: Row[] = page.messages.map((m) => ({ ...m, folder }));
				searched = null;
				if (append) {
					const have = new Set(rows.map((r) => r.uid));
					rows = [...rows, ...got.filter((m) => !have.has(m.uid))];
					next = page.next || null;
				} else {
					// Read again: new mail on top; older rows loaded further down
					// stay, and with them the place of the next page.
					const oldest = got.at(-1)?.uid ?? 0;
					const older = quiet && page.next ? rows.filter((r) => r.uid < oldest) : [];
					rows = [...got, ...older];
					if (!older.length) next = page.next || null;
				}
				total = page.total;
			}
			loaded = true;
			now = new Date();
			onloaded?.(now);
			// Picks of mails that went away go with them.
			const keys = new Set(rows.map(rowKey));
			if ([...picked].some((k) => !keys.has(k)))
				picked = new Set([...picked].filter((k) => keys.has(k)));
		} catch (err) {
			if (n === asked && !own.signal.aborted) error = apiErrorText(err);
		} finally {
			if (n === asked) loading = false;
		}
	}

	// A new folder, search, filter or scope starts from the top.
	$effect(() => {
		void accountId;
		void folder;
		void filters;
		void scope;
		untrack(() => {
			rows = [];
			next = null;
			loaded = false;
			picked = new Set();
			notice = '';
			actionError = '';
			void load(false);
		});
	});
	// Another folder forgets the search in all folders.
	$effect(() => {
		void accountId;
		void folder;
		untrack(() => {
			if (scope === 'all') scope = 'folder';
		});
	});

	// The end of the list in sight loads the next page.
	$effect(() => {
		const el = more;
		if (!el || typeof IntersectionObserver === 'undefined') return;
		const observer = new IntersectionObserver((entries) => {
			if (entries.some((e) => e.isIntersecting) && next !== null && !loading) void load(true);
		});
		observer.observe(el);
		return () => observer.disconnect();
	});

	let typing: ReturnType<typeof setTimeout> | undefined;
	function typed() {
		clearTimeout(typing);
		typing = setTimeout(() => {
			q = search.trim();
			from = fromText.trim();
		}, 400);
	}
	function submit(e: SubmitEvent) {
		e.preventDefault();
		clearTimeout(typing);
		q = search.trim();
		from = fromText.trim();
	}
	function resetFilters() {
		fromText = from = '';
		attachment = false;
		since = until = '';
	}

	// What the list shows: conversations in the groups, opened ones with
	// their other mails below; the keys in this order (keys j/k, Shift).
	type Shown = { row: Row; thread: Thread<Row>; child: boolean };
	const threads = $derived(threadRows(rows, threadsOn));
	const groups = $derived(groupByDay(threads, (th) => th.rows[0].date, now, i18n.locale));
	const isOpen = (th: Thread<Row>) => th.rows.some((r) => expanded.has(rowKey(r)));
	const shown = $derived.by(() => {
		const out: Shown[] = [];
		for (const g of groups)
			for (const th of g.items) {
				out.push({ row: th.rows[0], thread: th, child: false });
				if (th.rows.length > 1 && isOpen(th))
					for (const r of th.rows.slice(1)) out.push({ row: r, thread: th, child: true });
			}
		return out;
	});
	const shownKeys = $derived(shown.map((s) => rowKey(s.row)));
	const groupNames: Record<DayGroup, string> = $derived({
		today: t('mail.group.today'),
		yesterday: t('mail.group.yesterday'),
		week: t('mail.group.week'),
		older: t('mail.group.older'),
	});

	/** The mails an entry stands for: a closed conversation all of them. */
	const rowsOf = (s: Shown): Row[] => (s.child || isOpen(s.thread) ? [s.row] : s.thread.rows);
	const isPicked = (s: Shown) => rowsOf(s).every((r) => picked.has(rowKey(r)));
	const isCurrent = (r: Row) =>
		r.uid === selected && (r.folder ?? folder) === (selectedFolder || folder);

	function pick(s: Shown, e: MouseEvent | KeyboardEvent) {
		const key = rowKey(s.row);
		const nextSet = new Set(picked);
		if (e.shiftKey && anchor) {
			for (const k of rangeOf(shownKeys, anchor, key)) {
				const entry = shown.find((x) => rowKey(x.row) === k);
				if (entry) for (const r of rowsOf(entry)) nextSet.add(rowKey(r));
			}
		} else {
			const on = !isPicked(s);
			for (const r of rowsOf(s)) {
				if (on) nextSet.add(rowKey(r));
				else nextSet.delete(rowKey(r));
			}
			anchor = key;
		}
		picked = nextSet;
	}
	function clicked(s: Shown, e: MouseEvent) {
		if (e.ctrlKey || e.metaKey || e.shiftKey) {
			e.preventDefault();
			pick(s, e);
			return;
		}
		anchor = rowKey(s.row);
		onopen(s.row.uid, s.row.folder ?? folder);
	}
	function toggleThread(s: Shown) {
		const keys = s.thread.rows.map(rowKey);
		const set = new Set(expanded);
		if (isOpen(s.thread)) for (const k of keys) set.delete(k);
		else set.add(keys[0]);
		expanded = set;
	}
	function setThreads(on: boolean) {
		threadsOn = on;
		writePref('threads', on, true);
	}

	const pickedRows = $derived(rows.filter((r) => picked.has(rowKey(r))));
	function selectAll() {
		picked = new Set(rows.map(rowKey));
	}

	/** The rows by folder, in parts titand takes (at most 1000 uids). */
	function byFolder(items: Row[]): [string, number[]][] {
		const map = new Map<string, number[]>();
		for (const r of items) {
			const f = r.folder ?? folder;
			map.set(f, [...(map.get(f) ?? []), r.uid]);
		}
		const out: [string, number[]][] = [];
		for (const [f, uids] of map)
			for (let i = 0; i < uids.length; i += 1000) out.push([f, uids.slice(i, i + 1000)]);
		return out;
	}

	/** The row shown after some leave the list (for the open message). */
	function after(items: Row[]): MailItem | null {
		const gone = new Set(items.map(rowKey));
		const at = shown.findIndex((s) => gone.has(rowKey(s.row)));
		if (at < 0) return null;
		const rest = (list: Shown[]) => list.find((s) => !gone.has(rowKey(s.row)));
		const found = rest(shown.slice(at + 1)) ?? rest(shown.slice(0, at).reverse());
		return found ? { uid: found.row.uid, folder: found.row.folder ?? folder } : null;
	}
	function dropRows(items: Row[]) {
		const gone = new Set(items.map(rowKey));
		const nextItem = after(items);
		rows = rows.filter((r) => !gone.has(rowKey(r)));
		total = Math.max(0, total - items.length);
		picked = new Set([...picked].filter((k) => !gone.has(k)));
		onremoved?.(
			items.map((r) => ({ folder: r.folder ?? folder, uid: r.uid })),
			nextItem,
		);
	}

	async function run(kind: EntryAction | 'move', items: Row[], to = '') {
		if (!items.length) return;
		busy = true;
		actionError = '';
		notice = '';
		const done: Row[] = [];
		try {
			for (const [f, uids] of byFolder(items)) {
				if (kind === 'move' && f === to) continue;
				if (kind === 'archive') await mailApi.archive(accountId, { folder: f, uids });
				else if (kind === 'delete') await mailApi.remove(accountId, { folder: f, uids });
				else if (kind === 'move') await mailApi.move(accountId, { folder: f, uids, to });
				else {
					const flag: keyof Flags = kind === 'seen' || kind === 'unseen' ? 'seen' : 'flagged';
					const on = kind === 'seen' || kind === 'flag';
					await mailApi.flags(accountId, {
						folder: f,
						uids,
						add: on ? [flag] : [],
						remove: on ? [] : [flag],
					});
					for (const r of rows)
						if ((r.folder ?? folder) === f && uids.includes(r.uid))
							r.flags = { ...r.flags, [flag]: on };
				}
				done.push(...items.filter((r) => (r.folder ?? folder) === f && uids.includes(r.uid)));
			}
		} catch (err) {
			actionError = apiErrorText(err, { detail: true });
		} finally {
			busy = false;
		}
		if (!done.length) return;
		if (kind === 'archive' || kind === 'delete' || kind === 'move') {
			dropRows(done);
			notice =
				kind === 'archive'
					? t('mail.done.archived', { count: done.length })
					: kind === 'delete'
						? t('mail.done.deleted', { count: done.length })
						: t('mail.done.moved', { count: done.length, folder: nameOf(to) });
		}
		onchanged?.();
	}

	/** Deleting in the trash is for good: asked first, on every way. */
	function act(kind: EntryAction, items: Row[]) {
		if (kind === 'delete' && items.some((r) => role(r.folder ?? folder) === 'trash')) {
			confirmItems = items;
			return;
		}
		void run(kind, items);
	}
	function confirmed() {
		const items = confirmItems ?? [];
		confirmItems = null;
		void run('delete', items);
	}

	function dragStart(s: Shown, e: DragEvent) {
		const items = picked.has(rowKey(s.row)) ? pickedRows : rowsOf(s);
		mailDrag.account = accountId;
		mailDrag.items = items.map((r) => ({ folder: r.folder ?? folder, uid: r.uid }));
		if (e.dataTransfer) {
			e.dataTransfer.effectAllowed = 'move';
			e.dataTransfer.setData('application/x-titanos-mail', String(items.length));
		}
	}
	function dragEnd() {
		// The drop (if any) has read it by now.
		setTimeout(() => {
			mailDrag.account = '';
			mailDrag.items = [];
		}, 0);
	}

	const inArchive = $derived(role(folder) === 'archive');
	const pickedInTrash = $derived(pickedRows.some((r) => role(r.folder ?? folder) === 'trash'));
	const moveTargets = $derived(sortFolders(mail.folders[accountId] ?? []));

	/** Reads the first page again (new mail, after an action); `quiet` keeps the rows shown meanwhile. */
	export function refresh(quiet = true) {
		void load(false, quiet);
	}
	/** Marks changed elsewhere (the open message, the live stream). */
	export function setFlags(uids: number[], change: Partial<Flags>, inFolder = folder) {
		for (const r of rows)
			if ((r.folder ?? folder) === inFolder && uids.includes(r.uid))
				r.flags = { ...r.flags, ...change };
	}
	/** Messages moved or deleted elsewhere leave the list. */
	export function drop(uids: number[], inFolder = folder) {
		const gone = new Set(uids.map((uid) => rowKey({ uid, folder: inFolder })));
		rows = rows.filter((r) => !gone.has(rowKey(r)));
		total = Math.max(0, total - uids.length);
	}
	/** The mail shown after (or before) one, for going on after a delete. */
	export function neighbour(uid: number, inFolder = folder): MailItem | null {
		const row = rows.find((r) => r.uid === uid && (r.folder ?? folder) === inFolder);
		return row ? after([row]) : null;
	}
	/** j/k: the next or previous entry from the open one. */
	export function step(by: 1 | -1): MailItem | null {
		if (!shown.length) return null;
		const at = shown.findIndex((s) => isCurrent(s.row));
		const to = at < 0 ? (by > 0 ? 0 : shown.length - 1) : at + by;
		const s = shown[Math.max(0, Math.min(shown.length - 1, to))];
		return { uid: s.row.uid, folder: s.row.folder ?? folder };
	}
	/** e and Delete: the selection, else the open mail. */
	export function actOnCurrent(kind: 'archive' | 'delete') {
		let items = pickedRows;
		if (!items.length) {
			const current = rows.find(isCurrent);
			items = current ? [current] : [];
		}
		if (kind === 'archive' && items.every((r) => role(r.folder ?? folder) === 'archive')) return;
		if (items.length) act(kind, items);
	}
	/** /: the search field. */
	export function focusSearch() {
		searchField?.focus();
		searchField?.select();
	}
	/** The overview's links: a filter of this folder. */
	export function showFilter(f: Filter) {
		scope = 'folder';
		filter = f;
	}
	/** A drop onto a folder of this mailbox. */
	export function moveDropped(items: MailItem[], to: string) {
		const keys = new Set(items.map((i) => rowKey(i)));
		const list = rows.filter((r) => keys.has(rowKey(r)));
		if (list.length) void run('move', list, to);
	}

	const filterList: {
		id: Filter;
		key: 'mail.filter.all' | 'mail.filter.unread' | 'mail.filter.flagged';
	}[] = [
		{ id: 'all', key: 'mail.filter.all' },
		{ id: 'unread', key: 'mail.filter.unread' },
		{ id: 'flagged', key: 'mail.filter.flagged' },
	];
</script>

<section class="list" aria-labelledby="mail-list-head">
	<div class="tools">
		<div class="headline">
			<h2 id="mail-list-head">{heading}</h2>
			{#if oncompose}
				<button type="button" class="btn btn-primary compose" onclick={oncompose}
					><Icon name="edit" size={16} />{t('mail.action.compose')}</button
				>
			{/if}
		</div>
		<form role="search" onsubmit={submit}>
			<label class="sr-only" for="mail-search"
				>{scope === 'all' ? t('mail.search.labelAll') : t('mail.search.label')}</label
			>
			<input
				id="mail-search"
				class="field"
				type="search"
				bind:this={searchField}
				placeholder={scope === 'all'
					? t('mail.search.placeholderAll')
					: t('mail.search.placeholder')}
				bind:value={search}
				oninput={typed}
			/>
			<div class="scope" role="group" aria-label={t('mail.scope.label')}>
				<button
					type="button"
					class="btn seg"
					aria-pressed={scope === 'folder'}
					onclick={() => (scope = 'folder')}>{t('mail.scope.folder')}</button
				>
				<button
					type="button"
					class="btn seg"
					aria-pressed={scope === 'all'}
					onclick={() => (scope = 'all')}>{t('mail.scope.all')}</button
				>
				<button
					type="button"
					class="btn seg"
					aria-expanded={moreOpen}
					aria-controls="mail-more-filters"
					onclick={() => (moreOpen = !moreOpen)}
					><Icon name={moreOpen ? 'chevronDown' : 'chevronRight'} size={14} />{t(
						'mail.search.more',
					)}</button
				>
			</div>
			{#if moreOpen}
				<div id="mail-more-filters" class="more-filters">
					<label class="fl">
						<span>{t('mail.search.from')}</span>
						<input
							class="field"
							type="search"
							bind:value={fromText}
							oninput={typed}
							placeholder={t('mail.search.fromHint')}
						/>
					</label>
					<label class="check">
						<input type="checkbox" bind:checked={attachment} />
						<span>{t('mail.search.attachment')}</span>
					</label>
					<p class="muted hint">{t('mail.search.attachmentHint')}</p>
					<fieldset class="period">
						<legend>{t('mail.search.period')}</legend>
						<label class="fl">
							<span>{t('mail.search.since')}</span>
							<input class="field" type="date" bind:value={since} max={until || undefined} />
						</label>
						<label class="fl">
							<span>{t('mail.search.until')}</span>
							<input class="field" type="date" bind:value={until} min={since || undefined} />
						</label>
					</fieldset>
					{#if from || attachment || since || until}
						<button type="button" class="btn" onclick={resetFilters}
							>{t('mail.search.reset')}</button
						>
					{/if}
				</div>
			{/if}
		</form>
		<div class="filters" role="group" aria-label={t('mail.filter.label')}>
			{#each filterList as f (f.id)}
				<button
					type="button"
					class="btn filter"
					aria-pressed={filter === f.id}
					onclick={() => (filter = f.id)}>{t(f.key)}</button
				>
			{/each}
			<label class="check threads">
				<input
					type="checkbox"
					checked={threadsOn}
					onchange={(e) => setThreads((e.currentTarget as HTMLInputElement).checked)}
				/>
				<span>{t('mail.thread.toggle')}</span>
			</label>
		</div>
		{#if loaded && !needCriteria}
			<p class="muted count" role="status">
				{#if searched}
					{t('mail.search.foundAll', {
						count: total,
						folders: searched.folders,
					})}{#if total > rows.length}
						· {t('mail.search.newest', { shown: rows.length })}{/if}
				{:else}
					{q || hasCriteria
						? t('mail.list.found', { count: total })
						: t('mail.list.total', { count: total })}
				{/if}
			</p>
		{/if}
	</div>

	{#if pickedRows.length}
		<div class="bulk" role="toolbar" aria-label={t('mail.bulk.label')}>
			<span class="picked-count" role="status"
				>{t('mail.pick.count', { count: pickedRows.length })}</span
			>
			{#if pickedRows.length < rows.length}
				<button type="button" class="btn" onclick={selectAll}>{t('mail.pick.all')}</button>
			{/if}
			{#if !inArchive || scope === 'all'}
				<button type="button" class="btn" disabled={busy} onclick={() => act('archive', pickedRows)}
					><Icon name="archive" size={16} />{t('mail.action.archive')}</button
				>
			{/if}
			<button type="button" class="btn" disabled={busy} onclick={() => act('delete', pickedRows)}
				><Icon name="trash" size={16} />{pickedInTrash
					? t('mail.action.deleteForever')
					: t('mail.action.delete')}</button
			>
			<button type="button" class="btn" disabled={busy} onclick={() => (moveItems = pickedRows)}
				><Icon name="move" size={16} />{t('mail.action.move')}</button
			>
			<button type="button" class="btn" disabled={busy} onclick={() => act('seen', pickedRows)}
				>{t('mail.action.markRead')}</button
			>
			<button type="button" class="btn" disabled={busy} onclick={() => act('unseen', pickedRows)}
				>{t('mail.action.markUnread')}</button
			>
			<button type="button" class="btn" disabled={busy} onclick={() => act('flag', pickedRows)}
				><Icon name="flag" size={16} />{t('mail.action.flag')}</button
			>
			<button type="button" class="btn" disabled={busy} onclick={() => act('unflag', pickedRows)}
				>{t('mail.action.unflag')}</button
			>
			<button type="button" class="btn" onclick={() => (picked = new Set())}
				><Icon name="close" size={16} />{t('mail.pick.clear')}</button
			>
		</div>
	{/if}

	{#if error}<p class="notice-error" role="alert">{error}</p>{/if}
	{#if actionError}<p class="notice-error" role="alert">{actionError}</p>{/if}
	{#if notice}<p class="notice-ok done" role="status">{notice}</p>{/if}
	{#if searched?.truncated}<p class="notice-warn done" role="note">
			{t('mail.search.truncated')}
		</p>{/if}
	{#if needCriteria}
		<p class="muted empty">{t('mail.search.needCriteria')}</p>
	{:else if loaded && rows.length === 0 && !error}
		<p class="muted empty">
			{hasCriteria ? t('mail.list.noneFound') : t('mail.list.empty')}
		</p>
	{/if}
	{#each groups as g (g.group)}
		<section class="group" aria-labelledby="mail-group-{g.group}">
			<h3 id="mail-group-{g.group}">{groupNames[g.group]}</h3>
			<ul class="rows">
				{#each shown.filter((s) => g.items.includes(s.thread)) as s (rowKey(s.row))}
					<li>
						<MailEntry
							row={s.row}
							rows={s.thread.rows}
							child={s.child}
							expanded={isOpen(s.thread)}
							current={isCurrent(s.row)}
							picked={isPicked(s)}
							{phone}
							inTrash={role(s.row.folder ?? folder) === 'trash'}
							canArchive={role(s.row.folder ?? folder) !== 'archive'}
							folderName={scope === 'all' ? nameOf(s.row.folder ?? folder) : ''}
							draggable={drag && !phone}
							onopen={(e) => clicked(s, e)}
							onpick={(e) => pick(s, e)}
							ontoggle={s.child ? undefined : () => toggleThread(s)}
							onaction={(kind) => act(kind, rowsOf(s))}
							ondragstart={(e) => dragStart(s, e)}
							ondragend={dragEnd}
						/>
					</li>
				{/each}
			</ul>
		</section>
	{/each}
	{#if loading}<p class="muted loading" role="status">{t('common.loading')}</p>{/if}
	{#if next !== null}
		<button
			type="button"
			class="btn more"
			bind:this={more}
			disabled={loading}
			onclick={() => void load(true)}>{t('mail.list.more')}</button
		>
	{/if}
</section>

{#if confirmItems}
	<Confirm
		title={confirmItems.length === 1
			? t('mail.delete.title')
			: t('mail.bulk.deleteTitle', { count: confirmItems.length })}
		text={confirmItems.length === 1
			? [
					t('mail.delete.text', { subject: confirmItems[0].subject || t('mail.noSubject') }),
					t('mail.delete.forever'),
				]
			: t('mail.bulk.deleteText')}
		confirm={t('mail.action.deleteForever')}
		danger
		{busy}
		onconfirm={confirmed}
		onclose={() => (confirmItems = null)}
	/>
{/if}

{#if moveItems}
	<Dialog title={t('mail.move.title')} onclose={() => (moveItems = null)}>
		<ul class="targets">
			{#each moveTargets.filter((f) => scope === 'all' || f.path !== folder) as f (f.path)}
				<li>
					<button
						type="button"
						class="btn target"
						onclick={() => {
							const items = moveItems ?? [];
							moveItems = null;
							void run('move', items, f.path);
						}}>{folderName(f)}</button
					>
				</li>
			{:else}
				<li class="muted">{t('mail.move.none')}</li>
			{/each}
		</ul>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (moveItems = null)}
				>{t('common.cancel')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.list {
		display: flex;
		flex-direction: column;
		min-width: 0;
	}
	.tools {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		padding: var(--space-2-5) var(--space-3);
		border-bottom: 1px solid var(--border);
	}
	.headline {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
	}
	h2 {
		flex: 1 1 10em;
		margin: 0;
		font-size: var(--text-lg);
		overflow-wrap: anywhere;
	}
	.compose {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
	}
	form {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		min-width: 0;
	}
	form > input {
		width: 100%;
	}
	.scope,
	.filters {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-1);
	}
	.seg,
	.filter {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
	}
	.seg[aria-pressed='true'],
	.filter[aria-pressed='true'] {
		border-color: var(--accent);
		background: color-mix(in srgb, var(--accent) 16%, transparent);
		font-weight: 600;
	}
	.more-filters {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		padding: var(--space-2);
		border: 1px solid var(--border);
		border-radius: var(--radius-small);
	}
	.fl {
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-width: 0;
		font-size: var(--text-sm);
	}
	.fl input {
		width: 100%;
		min-width: 0;
	}
	.check {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1-5);
		min-height: 32px;
		overflow-wrap: break-word;
	}
	.threads {
		margin-left: auto;
		font-size: var(--text-sm);
	}
	.hint {
		margin: 0;
		font-size: var(--text-sm);
		overflow-wrap: break-word;
	}
	.period {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
		margin: 0;
		padding: 0;
		border: 0;
		min-width: 0;
	}
	.period legend {
		padding: 0;
		margin-bottom: var(--space-1);
		font-size: var(--text-sm);
		font-weight: 600;
	}
	.period .fl {
		flex: 1 1 9em;
	}
	.count {
		margin: 0;
		font-size: var(--text-sm);
		overflow-wrap: break-word;
	}
	.bulk {
		position: sticky;
		top: 0;
		z-index: 4;
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-1);
		padding: var(--space-2) var(--space-3);
		border-bottom: 1px solid var(--border);
		background: color-mix(in srgb, var(--accent) 10%, var(--surface));
	}
	.bulk .btn {
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
	}
	.picked-count {
		font-weight: 600;
		margin-right: var(--space-2);
	}
	.group h3 {
		margin: 0;
		padding: var(--space-1-5) var(--space-3);
		border-bottom: 1px solid var(--border);
		background: var(--bg-dim, var(--chip));
		color: var(--muted);
		font-size: var(--text-sm);
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.04em;
	}
	.rows {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.rows li {
		border-bottom: 1px solid var(--border);
	}
	.empty,
	.loading,
	.done {
		margin: var(--space-3);
	}
	.more {
		margin: var(--space-3);
		align-self: center;
	}
	.targets {
		list-style: none;
		margin: 0 0 var(--space-3);
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}
	.target {
		width: 100%;
		text-align: left;
		overflow-wrap: anywhere;
	}
	@media (max-width: 899px) {
		.btn {
			min-height: 44px;
		}
		.check {
			min-height: 44px;
		}
	}
</style>
