// 1.05: what the pages of T-Mail share – the account's mailboxes with their
// state and the folders of each. Read from titand; nothing is kept in the
// browser beyond the open page.
import { apiErrorText, tOr } from '../i18n/index.svelte.ts';
import { applyOrder, sideOrder } from '../side-order.svelte.ts';
import { mailApi } from './api.ts';
import type { FolderRole, MailAccount, MailFolder, MailStatus } from './types.ts';

export const mail = $state<{
	accounts: MailAccount[] | null;
	error: string;
	folders: Record<string, MailFolder[] | undefined>;
	folderErrors: Record<string, string>;
}>({ accounts: null, error: '', folders: {}, folderErrors: {} });

export async function loadAccounts(): Promise<void> {
	try {
		mail.accounts = await mailApi.accounts();
		mail.error = '';
	} catch (err) {
		mail.error = apiErrorText(err);
		mail.accounts ??= [];
	}
}

// 1.13: one request per mailbox at a time; asked again while one runs, it
// runs once more afterwards (not once per asking).
const folderLoads = new Map<string, Promise<void>>();
const folderAgain = new Set<string>();

/** Reads a mailbox's folders with their counts (IMAP STATUS of each: not cheap). */
export function loadFolders(id: string): Promise<void> {
	const running = folderLoads.get(id);
	if (running) {
		folderAgain.add(id);
		return running;
	}
	const run = (async () => {
		try {
			mail.folders[id] = await mailApi.folders(id);
			mail.folderErrors[id] = '';
		} catch (err) {
			mail.folderErrors[id] = apiErrorText(err);
			mail.folders[id] ??= [];
		} finally {
			folderLoads.delete(id);
		}
		if (folderAgain.delete(id)) await loadFolders(id);
	})();
	folderLoads.set(id, run);
	return run;
}

/** Reads a mailbox's folders once: not when they are there or on their way. */
export function ensureFolders(id: string): void {
	if (!mail.folders[id] && !folderLoads.has(id)) void loadFolders(id);
}

/**
 * 1.13 (Baustand step 5): the folders' state after something changed – a
 * mail read, moved or deleted, an event of the live stream. The counts
 * change on the page at once (adjustFolders); the NAS is asked once the
 * changes rest for QUIET, at the latest after MOST, so that ten mails read
 * one after the other cost one request, not ten.
 */
const QUIET = 3000;
const MOST = 10_000;
const folderTimers = new Map<string, { timer: ReturnType<typeof setTimeout>; first: number }>();
export function refreshFolders(id: string, now = false) {
	const held = folderTimers.get(id);
	if (held) clearTimeout(held.timer);
	const first = held?.first ?? Date.now();
	const wait = now ? 0 : Math.max(0, Math.min(QUIET, first + MOST - Date.now()));
	folderTimers.set(id, {
		first,
		timer: setTimeout(() => {
			folderTimers.delete(id);
			void loadFolders(id);
		}, wait),
	});
}

/** A change of a folder's counts, known before the NAS says it. */
/** `account` (1.15): set by the lists over all mailboxes; else the open one. */
export type FolderDelta = { folder: string; unread?: number; total?: number; account?: string };

/** Changes the counts on the page at once (never below 0); the NAS's answer replaces them. */
export function adjustFolders(id: string, deltas: FolderDelta[]) {
	const list = mail.folders[id];
	if (!list) return;
	for (const d of deltas) {
		const f = list.find((x) => x.path === d.folder);
		if (!f) continue;
		if (d.unread) f.unread = Math.max(0, f.unread + d.unread);
		if (d.total) f.total = Math.max(0, f.total + d.total);
		if (d.unread && f.role === 'inbox') {
			const a = mail.accounts?.find((x) => x.id === id);
			if (a) a.unread = Math.max(0, a.unread + d.unread);
		}
	}
}

/** A state from the live stream. */
export function applyStatus(account: string, status: MailStatus) {
	const a = mail.accounts?.find((x) => x.id === account);
	if (a) a.status = status;
}

const ORDER: FolderRole[] = ['inbox', 'drafts', 'sent', 'archive', 'junk', 'trash'];

/** Folders with a role first (Posteingang, Entwürfe, Gesendet …), then the others by name. */
export function sortFolders(list: MailFolder[]): MailFolder[] {
	const rank = (f: MailFolder) => (f.role ? ORDER.indexOf(f.role) : ORDER.length);
	return [...list].sort((a, b) => rank(a) - rank(b) || a.path.localeCompare(b.path));
}

/** The group of the folders with a role. */
export const ROLES = '\u0001';

/** A folder as the side bar shows it: its group of siblings for sorting. */
export type ArrangedFolder = { f: MailFolder; group: string };

/**
 * 1.13: the folders in the order of the side bar – first the folders with
 * a role (Posteingang, Entwürfe …), then the others, each followed by its
 * subfolders; within each group the account's own order (`order`, folder
 * paths, side-order.svelte.ts), the default order after it. A folder's
 * group is its nearest shown parent without a role ("" at the top, "\u0001"
 * for the roles); below the inbox counts as the top (servers with the
 * namespace "INBOX.").
 */
export function arrangeFolders(list: MailFolder[], order: string[]): ArrangedFolder[] {
	const sorted = sortFolders(list);
	const parentOf = (f: MailFolder): string => {
		if (f.role) return ROLES;
		let best: MailFolder | undefined;
		for (const p of sorted)
			if (!p.role && isBelow(f, p) && (!best || p.path.length > best.path.length)) best = p;
		return best?.path ?? '';
	};
	const children = new Map<string, MailFolder[]>();
	for (const f of sorted) {
		const g = parentOf(f);
		children.set(g, [...(children.get(g) ?? []), f]);
	}
	const out: ArrangedFolder[] = [];
	const seen = new Set<string>();
	const walk = (group: string) => {
		for (const f of applyOrder(children.get(group) ?? [], order, (x) => x.path)) {
			if (seen.has(f.path)) continue;
			seen.add(f.path);
			out.push({ f, group });
			if (!f.role) walk(f.path);
		}
	};
	walk(ROLES);
	walk('');
	return out;
}

/** The folder of a role in a mailbox (drafts, trash …). */
export function folderOf(id: string, role: FolderRole): MailFolder | undefined {
	return mail.folders[id]?.find((f) => f.role === role);
}

/** The inbox of a mailbox; "INBOX" is its name in IMAP when the list is not there yet. */
export function inboxOf(id: string): string {
	return folderOf(id, 'inbox')?.path ?? 'INBOX';
}

/** The name of a folder: its role in the interface's language, else its own name. */
export function folderName(f: Pick<MailFolder, 'name' | 'role'>): string {
	return f.role ? tOr(`mail.role.${f.role}`, f.name) : f.name;
}

/** A folder of a mailbox by its path. */
export function findFolder(id: string, path: string): MailFolder | undefined {
	return mail.folders[id]?.find((f) => f.path === path);
}

/** Forgets everything (another account logged in). */
export function resetMail() {
	mail.accounts = null;
	mail.error = '';
	mail.folders = {};
	mail.folderErrors = {};
	avatars.photos = Object.create(null);
	avatars.loaded = false;
}

/**
 * 1.12: rows being dragged from the list onto a folder. Only folders of
 * the same mailbox take them (a move never crosses mailboxes); the data of
 * a drag cannot be read before the drop, so the page keeps it here.
 */
export const mailDrag = $state<{ account: string; items: { folder: string; uid: number }[] }>({
	account: '',
	items: [],
});

/** 1.12: the own contacts with a picture by lower-case address (GET /mail/avatars). */
export const avatars = $state<{ book: string; photos: Record<string, string>; loaded: boolean }>({
	book: 'kontakte',
	photos: {},
	loaded: false,
});

/** Reads the pictures once per page; without contacts or volume the list shows initials. */
export async function loadAvatars(): Promise<void> {
	if (avatars.loaded) return;
	avatars.loaded = true;
	try {
		const got = await mailApi.avatars();
		// Addresses come from cards a device wrote: no prototype, so that
		// "__proto__" or "constructor" are plain keys (1.12 review).
		const photos: Record<string, string> = Object.create(null);
		if (got && typeof got.photos === 'object' && got.photos)
			for (const [addr, id] of Object.entries(got.photos))
				if (typeof id === 'string' && id) photos[addr.toLowerCase()] = id;
		avatars.photos = photos;
		if (typeof got?.book === 'string' && got.book) avatars.book = got.book;
	} catch {
		// An older NAS or no contacts: initials only.
	}
}

/** The folders a folder holds below it: its path and the delimiter. */
export function isBelow(child: MailFolder, parent: MailFolder): boolean {
	const delim = child.delim || parent.delim || (child.path.includes('/') ? '/' : '.');
	return child.path !== parent.path && child.path.startsWith(parent.path + delim);
}

/**
 * 1.15 (115-4): the place of the lists over all mailboxes – "Alle
 * Posteingänge" (folder "inbox") and "Markiert" (folder "flagged").
 */
export const ALL = '*';
export type AllKind = 'inbox' | 'flagged';

/** The mailboxes in the account's order (the first is the main one). */
export function orderedAccounts(): MailAccount[] {
	return applyOrder(mail.accounts ?? [], sideOrder().mailAccounts, (a) => a.id);
}

/** The mailbox "Neue E-Mail" writes from: the open one, else the main one. */
export function mainAccount(open = ''): MailAccount | undefined {
	return (
		(open && open !== ALL ? mail.accounts?.find((a) => a.id === open) : undefined) ??
		orderedAccounts()[0]
	);
}

/** Folders of a mailbox taken out of the side bar (115-4). */
export function hiddenFolders(id: string): string[] {
	return sideOrder().mailHidden[id] ?? [];
}
