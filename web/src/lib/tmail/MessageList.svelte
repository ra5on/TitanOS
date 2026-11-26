<script lang="ts" module>
	export type MailItem = { folder: string; uid: number; account?: string };
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
	import { listDate } from './text.ts';
	import { isBool, rangeOf, readPref } from './prefs.ts';
	import {
		ALL,
		findFolder,
		folderName,
		folderOf,
		mail,
		mailDrag,
		sortFolders,
		type FolderDelta,
	} from './store.svelte.ts';
	import { rowKey, threadRows, type Row, type Thread } from './threads.ts';
	import type { Flags } from './types.ts';

	type Filter = 'all' | 'unread' | 'flagged';
	type ListAction = EntryAction | 'junk';
	type Scope = 'folder' | 'all';

	let {
		accountId,
		folder,
		selected = null,
		selectedFolder = '',
		selectedAccount = '',
		heading,
		subheading = '',
		phone = false,
		compact = false,
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
		/** 1.15: the mailbox of the open message in a list over all mailboxes. */
		selectedAccount?: string;
		heading: string;
		/** 1.15: the line under the big title (the mailbox). */
		subheading?: string;
		phone?: boolean;
		/** 1.15: the phone and narrow windows (arrow after the date). */
		compact?: boolean;
		/** Rows can be dragged onto folders (the desktop's columns). */
		drag?: boolean;
		onopen: (uid: number, folder: string, account?: string) => void;
		/** "Neue E-Mail" in the list's bar at the bottom (1.15: always). */
		oncompose?: () => void;
		/** Something changed the folders' counts; `deltas` as far as known here (1.13). */
		onchanged?: (deltas: FolderDelta[]) => void;
		/** Mails left the list; `next` is the one shown after them. */
		onremoved?: (items: MailItem[], next: MailItem | null) => void;
		/** A list arrived from titand (the overview names the time). */
		onloaded?: (at: Date) => void;
	} = $props();

	let rows = $state<Row[]>([]);
	// A uid below which the next page starts; over all mailboxes a cursor.
	let next = $state<number | string | null>(null);
	let total = $state(0);
	let loading = $state(false);
	let error = $state('');
	let loaded = $state(false);
	let search = $state('');
	let q = $state('');
	let unreadOnly = $state(false);
	let flaggedOnly = $state(false);
	let scope = $state<Scope>('folder');
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
	// 1.15 (115-2, drafts 11–19): selecting is a mode ("Bearbeiten"), the
	// filters are a sheet behind the bar's filter button, "Markieren" a
	// menu of the selected mails (or of one mail: the swipe's "Mehr").
	let editing = $state(false);
	let filterOpen = $state(false);
	let actionsFor = $state<Row[] | null>(null);
	let searchFocus = $state(false);
	let unavailable = $state<string[]>([]);
	const unified = $derived(accountId === ALL);
	/** The search is in use: the field stays, "Abbrechen" and the scope show. */
	const searchActive = $derived(searchFocus || !!search || scope === 'all');
	const accOf = (r: Pick<Row, 'account'>) => r.account ?? accountId;
	const roleIn = (a: string, path: string) => findFolder(a, path)?.role ?? '';
	const roleOf = (r: Row) => roleIn(accOf(r), r.folder ?? folder);

	const filters = $derived<Filters>({
		q,
		unread: unreadOnly,
		flagged: flaggedOnly,
		from,
		attachment,
		since,
		until,
	});
	const hasCriteria = $derived(
		!!(q || from || attachment || since || until || unreadOnly || flaggedOnly),
	);
	/** A filter of the sheet is on (the search field counts apart). */
	const filterActive = $derived(
		!!(from || attachment || since || until || unreadOnly || flaggedOnly || scope === 'all'),
	);
	const filterWords = $derived(
		[
			unreadOnly && t('mail.filter.unread'),
			flaggedOnly && t('mail.filter.flagged'),
			attachment && t('mail.filter.attachmentWord'),
			from && t('mail.filter.fromWord', { from }),
			(since || until) && t('mail.search.period'),
			scope === 'all' && t('mail.filter.allFolders'),
		]
			.filter(Boolean)
			.join(', '),
	);
	const unreadCount = $derived.by(() => {
		if (unified)
			return (mail.accounts ?? []).reduce(
				(n, a) => n + (mail.folders[a.id]?.find((f) => f.role === 'inbox')?.unread ?? 0),
				0,
			);
		const f = findFolder(accountId, folder);
		return f && f.role !== 'junk' && f.role !== 'trash' ? f.unread : 0;
	});
	const stateLine = $derived(
		!loaded
			? t('common.loading')
			: Date.now() - now.getTime() < 60_000
				? t('mail.bar.justNow')
				: t('mail.bar.updatedAt', { time: listDate(now.toISOString(), i18n.locale) }),
	);
	const accountLabel = (r: Row) => {
		const a = mail.accounts?.find((x) => x.id === r.account);
		const name = a ? a.name || a.email : '';
		return r.folder && r.folder !== 'INBOX' ? `${name} · ${nameOf(r.folder, r.account)}` : name;
	};
	const needCriteria = $derived(scope === 'all' && !hasCriteria);
	const role = (path: string) => (unified ? '' : (findFolder(accountId, path)?.role ?? ''));
	const nameOf = (path: string, a = accountId) => {
		const f = findFolder(a, path);
		return f ? folderName(f) : path;
	};

	async function load(append: boolean, quiet = false) {
		const n = ++asked;
		controller?.abort();
		const own = (controller = new AbortController());
		if (!quiet) loading = true;
		error = '';
		try {
			if (unified) {
				const page = await mailApi.unified(
					folder === 'flagged' ? 'flagged' : 'inbox',
					{ ...filters, cursor: append && typeof next === 'string' ? next : null },
					own.signal,
				);
				if (n !== asked) return;
				const got: Row[] = page.messages.map((m) => ({ ...m }));
				searched = null;
				unavailable = page.failed.map((f) => f.account);
				if (append) {
					const have = new Set(rows.map(rowKey));
					rows = [...rows, ...got.filter((m) => !have.has(rowKey(m)))];
				} else rows = got;
				next = page.next || null;
				total = page.total;
			} else if (scope === 'all') {
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
					{ folder, before: append && typeof next === 'number' ? next : null, ...filters },
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

	// Keys of what is listed: a derived passes a change on only when its
	// value changes, so that a new address of the page (a message opened)
	// does not read the list again.
	const where = $derived(`${accountId}\u0000${folder}`);
	const asking = $derived(`${scope}\u0000${JSON.stringify(filters)}`);

	// A new folder, search, filter or scope starts from the top.
	$effect(() => {
		void where;
		void asking;
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
	// Another folder forgets a search in all folders with its words and
	// filters (a search in one folder stays, as before 1.12).
	$effect(() => {
		void where;
		untrack(() => {
			if (scope !== 'all') return;
			scope = 'folder';
			search = q = '';
			resetFilters();
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
		r.uid === selected &&
		(r.folder ?? folder) === (selectedFolder || folder) &&
		(!unified || r.account === selectedAccount);

	function pick(s: Shown, e: MouseEvent | KeyboardEvent) {
		// Ctrl/Cmd or Shift on a row selects: the bar of the selection shows.
		editing = true;
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
		if (s.row.account) onopen(s.row.uid, s.row.folder ?? folder, s.row.account);
		else onopen(s.row.uid, s.row.folder ?? folder);
	}
	function toggleThread(s: Shown) {
		const keys = s.thread.rows.map(rowKey);
		const set = new Set(expanded);
		if (isOpen(s.thread)) for (const k of keys) set.delete(k);
		else set.add(keys[0]);
		expanded = set;
	}

	const pickedRows = $derived(rows.filter((r) => picked.has(rowKey(r))));
	function selectAll() {
		picked = new Set(rows.map(rowKey));
	}

	/** The rows by mailbox and folder, in parts titand takes (at most 1000 uids). */
	function byFolder(items: Row[]): [string, string, number[]][] {
		const map = new Map<string, { a: string; f: string; uids: number[] }>();
		for (const r of items) {
			const a = accOf(r);
			const f = r.folder ?? folder;
			const k = a + '\u0000' + f;
			const e = map.get(k) ?? { a, f, uids: [] };
			e.uids.push(r.uid);
			map.set(k, e);
		}
		const out: [string, string, number[]][] = [];
		for (const { a, f, uids } of map.values())
			for (let i = 0; i < uids.length; i += 1000) out.push([a, f, uids.slice(i, i + 1000)]);
		return out;
	}

	/** The row shown after some leave the list (for the open message). */
	function after(items: Row[]): MailItem | null {
		const gone = new Set(items.map(rowKey));
		const at = shown.findIndex((s) => gone.has(rowKey(s.row)));
		if (at < 0) return null;
		const rest = (list: Shown[]) => list.find((s) => !gone.has(rowKey(s.row)));
		const found = rest(shown.slice(at + 1)) ?? rest(shown.slice(0, at).reverse());
		return found
			? { uid: found.row.uid, folder: found.row.folder ?? folder, account: found.row.account }
			: null;
	}
	function dropRows(items: Row[]) {
		const gone = new Set(items.map(rowKey));
		const nextItem = after(items);
		rows = rows.filter((r) => !gone.has(rowKey(r)));
		total = Math.max(0, total - items.length);
		picked = new Set([...picked].filter((k) => !gone.has(k)));
		onremoved?.(
			items.map((r) => ({ folder: r.folder ?? folder, uid: r.uid, account: r.account })),
			nextItem,
		);
	}

	async function run(kind: ListAction | 'move', items: Row[], to = '') {
		if (!items.length) return;
		busy = true;
		actionError = '';
		notice = '';
		const done: Row[] = [];
		const deltas: FolderDelta[] = [];
		/** Where archive and delete put mails (none: deleted for good, or not known). */
		const target = (a: string, from: string): string =>
			kind === 'move'
				? to
				: kind === 'junk'
					? (folderOf(a, 'junk')?.path ?? '')
					: kind === 'archive'
						? (folderOf(a, 'archive')?.path ?? '')
						: kind === 'delete' && roleIn(a, from) !== 'trash'
							? (folderOf(a, 'trash')?.path ?? '')
							: '';
		try {
			for (const [a, f, uids] of byFolder(items)) {
				if (kind === 'move' && f === to) continue;
				if (kind === 'archive') await mailApi.archive(a, { folder: f, uids });
				else if (kind === 'delete') await mailApi.remove(a, { folder: f, uids });
				else if (kind === 'move') await mailApi.move(a, { folder: f, uids, to });
				else if (kind === 'junk') {
					const junk = target(a, f);
					if (!junk || junk === f) continue;
					await mailApi.move(a, { folder: f, uids, to: junk });
				} else {
					const flag: keyof Flags = kind === 'seen' || kind === 'unseen' ? 'seen' : 'flagged';
					const on = kind === 'seen' || kind === 'flag';
					const changing = items.filter(
						(r) =>
							accOf(r) === a &&
							(r.folder ?? folder) === f &&
							uids.includes(r.uid) &&
							!!r.flags[flag] !== on,
					).length;
					await mailApi.flags(a, {
						folder: f,
						uids,
						add: on ? [flag] : [],
						remove: on ? [] : [flag],
					});
					for (const r of rows)
						if (accOf(r) === a && (r.folder ?? folder) === f && uids.includes(r.uid))
							r.flags = { ...r.flags, [flag]: on };
					if (flag === 'seen' && changing)
						deltas.push({ account: a, folder: f, unread: on ? -changing : changing });
				}
				const these = items.filter(
					(r) => accOf(r) === a && (r.folder ?? folder) === f && uids.includes(r.uid),
				);
				if (kind === 'archive' || kind === 'delete' || kind === 'move' || kind === 'junk') {
					const unread = these.filter((r) => !r.flags.seen).length;
					deltas.push({ account: a, folder: f, total: -these.length, unread: -unread });
					const into = target(a, f);
					if (into) deltas.push({ account: a, folder: into, total: these.length, unread });
				}
				done.push(...these);
			}
		} catch (err) {
			actionError = apiErrorText(err, { detail: true });
		} finally {
			busy = false;
		}
		if (!done.length) return;
		if (kind === 'archive' || kind === 'delete' || kind === 'move' || kind === 'junk') {
			dropRows(done);
			notice =
				kind === 'archive'
					? t('mail.done.archived', { count: done.length })
					: kind === 'delete'
						? t('mail.done.deleted', { count: done.length })
						: kind === 'junk'
							? t('mail.done.junk', { count: done.length })
							: t('mail.done.moved', { count: done.length, folder: nameOf(to) });
			if (editing && !rows.some((r) => picked.has(rowKey(r)))) picked = new Set();
		}
		onchanged?.(deltas);
	}

	/** The menu "Markieren": its mails, taken before the menu closes. */
	function menuAct(kind: ListAction) {
		const items = actionsFor ?? [];
		actionsFor = null;
		act(kind, items);
	}
	function menuMove() {
		const items = actionsFor ?? [];
		actionsFor = null;
		moveItems = items;
	}

	/** Deleting in the trash is for good: asked first, on every way. */
	function act(kind: ListAction, items: Row[]) {
		if (kind === 'delete' && items.some((r) => roleOf(r) === 'trash')) {
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
		if (unified) return;
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

	const pickedInTrash = $derived(pickedRows.some((r) => roleOf(r) === 'trash'));
	/** "Bewegen" moves within one mailbox: the mailbox of the mails, if it is one. */
	const moveAccount = $derived.by(() => {
		const list = moveItems ?? [];
		const ids = new Set(list.map(accOf));
		return ids.size === 1 ? [...ids][0] : '';
	});
	const moveTargets = $derived(moveAccount ? sortFolders(mail.folders[moveAccount] ?? []) : []);

	/** Reads the first page again (new mail, after an action); `quiet` keeps the rows shown meanwhile. */
	export function refresh(quiet = true) {
		void load(false, quiet);
	}
	/** Marks changed elsewhere (the open message, the live stream). */
	export function setFlags(
		uids: number[],
		change: Partial<Flags>,
		inFolder = folder,
		inAccount?: string,
	) {
		for (const r of rows)
			if (
				(r.folder ?? folder) === inFolder &&
				uids.includes(r.uid) &&
				(!inAccount || r.account === inAccount)
			)
				r.flags = { ...r.flags, ...change };
	}
	/** Messages moved or deleted elsewhere leave the list. */
	export function drop(uids: number[], inFolder = folder, inAccount?: string) {
		const gone = new Set(uids.map((uid) => rowKey({ uid, folder: inFolder, account: inAccount })));
		rows = rows.filter((r) => !gone.has(rowKey(r)));
		total = Math.max(0, total - uids.length);
	}
	/** The mail shown after (or before) one, for going on after a delete. */
	export function neighbour(uid: number, inFolder = folder, inAccount?: string): MailItem | null {
		const row = rows.find(
			(r) =>
				r.uid === uid &&
				(r.folder ?? folder) === inFolder &&
				(!inAccount || r.account === inAccount),
		);
		return row ? after([row]) : null;
	}
	/** j/k: the next or previous entry from the open one. */
	export function step(by: 1 | -1): MailItem | null {
		if (!shown.length) return null;
		const at = shown.findIndex((s) => isCurrent(s.row));
		const to = at < 0 ? (by > 0 ? 0 : shown.length - 1) : at + by;
		const s = shown[Math.max(0, Math.min(shown.length - 1, to))];
		return { uid: s.row.uid, folder: s.row.folder ?? folder, account: s.row.account };
	}
	/** e and Delete: the selection, else the open mail. */
	export function actOnCurrent(kind: 'archive' | 'delete') {
		let items = pickedRows;
		if (!items.length) {
			const current = rows.find(isCurrent);
			items = current ? [current] : [];
		}
		if (kind === 'archive' && items.every((r) => roleOf(r) === 'archive')) return;
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
		unreadOnly = f === 'unread';
		flaggedOnly = f === 'flagged';
	}
	/** A drop onto a folder of this mailbox. */
	export function moveDropped(items: MailItem[], to: string) {
		const keys = new Set(items.map((i) => rowKey(i)));
		const list = rows.filter((r) => keys.has(rowKey(r)));
		if (list.length) void run('move', list, to);
	}
</script>

<section class="list" class:phone class:compact aria-labelledby="mail-list-head">
	<div class="tools">
		{#if !searchActive}
			<div class="nav">
				{#if editing}
					<button type="button" class="link" onclick={selectAll}>{t('mail.pick.all')}</button>
				{:else}<span></span>{/if}
				<button
					type="button"
					class="link"
					class:strong={editing}
					aria-pressed={editing}
					onclick={() => {
						editing = !editing;
						if (!editing) picked = new Set();
					}}>{editing ? t('mail.box.done') : t('mail.box.edit')}</button
				>
			</div>
			<h2 id="mail-list-head" class="big">
				{editing && pickedRows.length
					? t('mail.pick.count', { count: pickedRows.length })
					: heading}
			</h2>
			{#if subheading && !editing}<p class="sub">{subheading}</p>{/if}
		{:else}
			<h2 id="mail-list-head" class="sr-only">{heading}</h2>
		{/if}
		{#if !editing}
			<form role="search" class="search" class:on={searchActive} onsubmit={submit}>
				<label class="sr-only" for="mail-search"
					>{scope === 'all' ? t('mail.search.labelAll') : t('mail.search.label')}</label
				>
				<span class="field-wrap"
					><Icon name="search" size={18} /><input
						id="mail-search"
						class="field"
						type="search"
						bind:this={searchField}
						placeholder={t('mail.search.placeholderShort')}
						bind:value={search}
						oninput={typed}
						onfocus={() => (searchFocus = true)}
					/></span
				>
				{#if searchActive}
					<button
						type="button"
						class="link"
						onclick={() => {
							search = q = '';
							searchFocus = false;
							if (scope === 'all') scope = 'folder';
						}}>{t('common.cancel')}</button
					>
					{#if !unified}
						<div class="seg" role="group" aria-label={t('mail.scope.label')}>
							<button type="button" aria-pressed={scope === 'all'} onclick={() => (scope = 'all')}
								>{t('mail.scope.allShort')}</button
							>
							<button
								type="button"
								aria-pressed={scope === 'folder'}
								onclick={() => (scope = 'folder')}>{heading}</button
							>
						</div>
					{/if}
				{/if}
			</form>
		{/if}
		{#if loaded && !needCriteria && (searched || q || hasCriteria)}
			<p class="muted count" role="status">
				{#if searched}
					{t('mail.search.foundAll', {
						count: total,
						folders: searched.folders,
					})}{#if total > rows.length}
						· {t('mail.search.newest', { shown: rows.length })}{/if}
				{:else}
					{t('mail.list.found', { count: total })}
				{/if}
			</p>
		{/if}
	</div>

	{#if unified && unavailable.length}
		<p class="notice-warn done" role="status">
			{t('mail.all.failed', {
				names: unavailable
					.map((id) => mail.accounts?.find((a) => a.id === id)?.name || id)
					.join(', '),
			})}
		</p>
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
			<h3 id="mail-group-{g.group}" class:sr-only={!phone} class="group-heading">
				{groupNames[g.group]}
			</h3>
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
							{editing}
							arrow={compact && !editing}
							inTrash={roleOf(s.row) === 'trash'}
							canArchive={roleOf(s.row) !== 'archive'}
							folderName={unified
								? accountLabel(s.row)
								: scope === 'all'
									? nameOf(s.row.folder ?? folder)
									: ''}
							draggable={drag && !phone && !unified && !editing}
							onlongpress={() => {
								editing = true;
								picked = new Set(rowsOf(s).map(rowKey));
							}}
							onmore={() => (actionsFor = rowsOf(s))}
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
	<!-- 1.15 (drafts 11–17): the bar at the bottom of the list – filter,
	     the state and "Neue E-Mail"; while selecting Markieren · Bewegen ·
	     Löschen. It stays in view at the list's bottom edge. -->
	<div class="foot" role="toolbar" aria-label={t('mail.listActions')}>
		{#if editing}
			<button
				type="button"
				class="link"
				disabled={!pickedRows.length || busy}
				onclick={() => (actionsFor = pickedRows)}>{t('mail.bar.mark')}</button
			>
			<button
				type="button"
				class="link"
				disabled={!pickedRows.length || busy || new Set(pickedRows.map(accOf)).size > 1}
				title={new Set(pickedRows.map(accOf)).size > 1 ? t('mail.bar.moveOne') : undefined}
				onclick={() => (moveItems = pickedRows)}>{t('mail.bar.move')}</button
			>
			<button
				type="button"
				class="link danger"
				disabled={!pickedRows.length || busy}
				onclick={() => act('delete', pickedRows)}
				>{pickedInTrash ? t('mail.action.deleteForever') : t('mail.action.delete')}</button
			>
		{:else}
			<button
				type="button"
				class="round"
				class:on={filterActive}
				aria-pressed={filterActive}
				aria-label={t('mail.bar.filter')}
				title={t('mail.bar.filter')}
				onclick={() => (filterOpen = true)}><Icon name="filter" size={22} /></button
			>
			<button type="button" class="state" onclick={() => (filterOpen = true)}>
				{#if filterActive}
					<span>{t('mail.bar.filteredBy')}</span><strong>{filterWords}</strong>
				{:else}
					<span>{stateLine}</span>
					{#if unreadCount > 0}<small>{t('mail.box.unread', { count: unreadCount })}</small>{/if}
				{/if}
			</button>
			{#if oncompose}
				<button
					type="button"
					class="round"
					aria-label={t('mail.action.compose')}
					title={t('mail.action.compose')}
					onclick={oncompose}><Icon name="edit" size={22} /></button
				>
			{:else}<span class="round-gap"></span>{/if}
		{/if}
	</div>
</section>

{#if filterOpen}
	<Dialog title={t('mail.bar.filter')} onclose={() => (filterOpen = false)}>
		{#if !unified}
			<h3 class="fh">{t('mail.filter.include')}</h3>
			<div class="fgroup" role="radiogroup" aria-label={t('mail.filter.include')}>
				<button
					type="button"
					role="radio"
					aria-checked={scope === 'folder'}
					onclick={() => (scope = 'folder')}
					><span>{t('mail.filter.thisFolder')}</span>{#if scope === 'folder'}<Icon
							name="check"
							size={18}
						/>{/if}</button
				>
				<button
					type="button"
					role="radio"
					aria-checked={scope === 'all'}
					onclick={() => (scope = 'all')}
					><span>{t('mail.filter.allFolders')}</span>{#if scope === 'all'}<Icon
							name="check"
							size={18}
						/>{/if}</button
				>
			</div>
		{/if}
		<h3 class="fh">{t('mail.filter.mailsThat')}</h3>
		<div class="fgroup">
			<button
				type="button"
				role="checkbox"
				aria-checked={unreadOnly}
				onclick={() => (unreadOnly = !unreadOnly)}
				><span>{t('mail.filter.areUnread')}</span>{#if unreadOnly}<Icon
						name="check"
						size={18}
					/>{/if}</button
			>
			{#if !(unified && folder === 'flagged')}
				<button
					type="button"
					role="checkbox"
					aria-checked={flaggedOnly}
					onclick={() => (flaggedOnly = !flaggedOnly)}
					><span>{t('mail.filter.areFlagged')}</span>{#if flaggedOnly}<Icon
							name="check"
							size={18}
						/>{/if}</button
				>
			{/if}
			<button
				type="button"
				role="checkbox"
				aria-checked={attachment}
				onclick={() => (attachment = !attachment)}
				><span>{t('mail.filter.haveAttachment')}</span>{#if attachment}<Icon
						name="check"
						size={18}
					/>{/if}</button
			>
		</div>
		<p class="muted fnote">{t('mail.search.attachmentHint')}</p>
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
		<div class="dialog-actions">
			<button
				type="button"
				class="btn"
				disabled={!filterActive && scope === 'folder'}
				onclick={() => {
					unreadOnly = flaggedOnly = false;
					resetFilters();
					scope = 'folder';
				}}>{t('mail.search.reset')}</button
			>
			<button type="button" class="btn btn-primary" onclick={() => (filterOpen = false)}
				>{t('mail.box.done')}</button
			>
		</div>
	</Dialog>
{/if}

{#if actionsFor}
	{@const items = actionsFor}
	{@const allFlagged = items.every((r) => r.flags.flagged)}
	{@const allSeen = items.every((r) => r.flags.seen)}
	<Dialog
		title={items.length === 1
			? t('mail.bar.markOne')
			: t('mail.bar.markMany', { count: items.length })}
		onclose={() => (actionsFor = null)}
	>
		<div class="fgroup acts">
			<button type="button" onclick={() => menuAct(allFlagged ? 'unflag' : 'flag')}
				><Icon name="flag" size={18} /><span
					>{allFlagged ? t('mail.action.unflag') : t('mail.action.flag')}</span
				></button
			>
			<button type="button" onclick={() => menuAct(allSeen ? 'unseen' : 'seen')}
				><Icon name="mail" size={18} /><span
					>{allSeen ? t('mail.action.markUnread') : t('mail.action.markRead')}</span
				></button
			>
			{#if items.some((r) => roleOf(r) !== 'archive')}
				<button type="button" onclick={() => menuAct('archive')}
					><Icon name="archive" size={18} /><span>{t('mail.action.archive')}</span></button
				>
			{/if}
			{#if items.some((r) => roleOf(r) !== 'junk')}
				<button type="button" onclick={() => menuAct('junk')}
					><Icon name="shield" size={18} /><span>{t('mail.action.junk')}</span></button
				>
			{/if}
			<button type="button" onclick={() => menuMove()} disabled={new Set(items.map(accOf)).size > 1}
				><Icon name="move" size={18} /><span>{t('mail.bar.move')}</span></button
			>
		</div>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (actionsFor = null)}
				>{t('common.cancel')}</button
			>
		</div>
	</Dialog>
{/if}

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
	/* 1.15 (115-2, drafts 11–19): the head like Apple Mail – "Bearbeiten",
	   a big title, the mailbox below it, the search field; the bar at the
	   bottom with the filter, the state and "Neue E-Mail". Sizes follow the
	   column (container units): the same look in a narrow column, a wide
	   window and on the phone. */
	.list {
		container-type: inline-size;
		/* The headings of the days are for screen readers only: they stay
		   inside the list's own scrolling column. */
		position: relative;
	}
	.tools {
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
		padding: 0 0 var(--space-2-5);
		border-bottom: 1px solid var(--border);
	}
	.nav {
		display: flex;
		justify-content: space-between;
		align-items: center;
		min-height: 44px;
		padding: 0 var(--space-1);
	}
	.link {
		min-height: 44px;
		padding: 0 var(--space-2);
		border: 0;
		background: none;
		color: var(--accent);
		font: inherit;
		font-size: clamp(15px, 4.4cqi, 17px);
		cursor: pointer;
		overflow-wrap: anywhere;
	}
	.link.strong {
		font-weight: 600;
	}
	.link.danger {
		color: var(--status-error);
	}
	.link:disabled {
		color: var(--muted);
		cursor: default;
	}
	.link:focus-visible,
	.round:focus-visible,
	.state:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
		border-radius: var(--radius-small);
	}
	.big {
		margin: 0;
		padding: 0 var(--space-3);
		font-size: clamp(24px, 8cqi, 32px);
		font-weight: 700;
		line-height: 1.15;
		overflow-wrap: anywhere;
	}
	.sub {
		margin: 0;
		padding: 0 var(--space-3);
		color: var(--muted);
		font-size: var(--text-md);
		overflow-wrap: anywhere;
	}
	.search {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
		padding: var(--space-2) var(--space-3) 0;
		min-width: 0;
	}
	.search.on {
		padding-top: var(--space-3);
	}
	.field-wrap {
		flex: 1 1 12em;
		min-width: 0;
		display: flex;
		align-items: center;
		gap: var(--space-1-5);
		min-height: 44px;
		padding: 0 var(--space-2-5);
		border-radius: 10px;
		background: var(--chip);
		color: var(--muted);
	}
	.field-wrap :global(svg) {
		flex: none;
	}
	.field-wrap .field {
		flex: 1;
		min-width: 0;
		border: 0;
		background: none;
		padding: 0;
		min-height: 44px;
		font-size: var(--text-md);
		color: var(--text);
		box-shadow: none;
	}
	.field-wrap .field:focus {
		outline: none;
	}
	.field-wrap:focus-within {
		outline: 2px solid var(--accent);
		outline-offset: 0;
	}
	.seg {
		flex: 1 1 100%;
		display: flex;
		padding: 2px;
		border-radius: 9px;
		background: var(--chip);
	}
	.seg button {
		flex: 1;
		min-height: 36px;
		padding: var(--space-1) var(--space-2);
		border: 0;
		border-radius: 7px;
		background: none;
		color: var(--text);
		font: inherit;
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
		cursor: pointer;
	}
	.seg button[aria-pressed='true'] {
		background: var(--surface);
		font-weight: 600;
		box-shadow: 0 1px 2px rgb(0 0 0 / 0.25);
	}
	.count {
		margin: 0;
		padding: var(--space-1) var(--space-3) 0;
		font-size: var(--text-sm);
		overflow-wrap: break-word;
	}
	.foot {
		position: sticky;
		bottom: 0;
		z-index: 5;
		margin-top: auto;
		display: grid;
		grid-template-columns: 56px minmax(0, 1fr) 56px;
		align-items: center;
		min-height: 56px;
		padding: var(--space-1) var(--space-1) calc(var(--space-1) + env(safe-area-inset-bottom));
		border-top: 1px solid var(--border);
		background: var(--surface);
	}
	.foot:has(.link) {
		grid-template-columns: repeat(3, minmax(0, 1fr));
	}
	.round {
		display: grid;
		place-items: center;
		width: 48px;
		height: 48px;
		justify-self: center;
		border: 0;
		border-radius: 50%;
		background: none;
		color: var(--accent);
		cursor: pointer;
	}
	.round.on {
		background: var(--accent);
		color: var(--on-accent);
	}
	.round-gap {
		width: 48px;
	}
	.state {
		display: flex;
		flex-direction: column;
		align-items: center;
		min-height: 44px;
		justify-content: center;
		border: 0;
		background: none;
		color: var(--text);
		font: inherit;
		font-size: var(--text-sm);
		text-align: center;
		overflow-wrap: anywhere;
		cursor: pointer;
	}
	.state small {
		color: var(--muted);
		font-size: var(--text-xs, 12px);
	}
	.state strong {
		color: var(--accent);
	}
	/* The sheet of filters and the menu "Markieren". */
	.fh {
		margin: var(--space-3) 0 var(--space-1);
		color: var(--muted);
		font-size: var(--text-sm);
		font-weight: 600;
	}
	.fgroup {
		display: flex;
		flex-direction: column;
		border-radius: 12px;
		background: var(--chip);
	}
	.fgroup button:first-child {
		border-radius: 12px 12px 0 0;
	}
	.fgroup button:last-child {
		border-radius: 0 0 12px 12px;
	}
	.fgroup button:only-child {
		border-radius: 12px;
	}
	.fgroup button {
		display: flex;
		align-items: center;
		gap: var(--space-2-5);
		min-height: 48px;
		padding: var(--space-2) var(--space-3);
		border: 0;
		border-bottom: 1px solid var(--border);
		background: none;
		color: var(--text);
		font: inherit;
		font-size: var(--text-md);
		text-align: left;
		cursor: pointer;
	}
	.fgroup button:last-child {
		border-bottom: 0;
	}
	.fgroup button span {
		flex: 1;
		overflow-wrap: anywhere;
	}
	.fgroup button :global(svg) {
		flex: none;
		color: var(--accent);
	}
	.fgroup button:disabled {
		color: var(--muted);
		cursor: default;
	}
	.fgroup button:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	.fnote {
		margin: var(--space-1) 0 var(--space-2);
		font-size: var(--text-sm);
	}
	.fl {
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-width: 0;
		margin-top: var(--space-2);
		font-size: var(--text-sm);
	}
	.fl input {
		width: 100%;
		min-width: 0;
	}
	.period {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
		margin: var(--space-2) 0 0;
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
		margin-top: 0;
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
	}

	.phone .group {
		margin: 0 12px 8px;
	}
	.phone .group-heading {
		margin: 12px 5px 6px;
		font-size: var(--text-sm);
		color: var(--muted);
	}
	.phone .rows {
		overflow: hidden;
		border: 1px solid var(--border);
		border-radius: var(--radius-tile);
		background: color-mix(in srgb, var(--surface) 89%, transparent);
	}
	.phone .group :global(.unread .from) {
		font-weight: 700;
	}
</style>
