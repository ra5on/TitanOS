// 1.05: what the pages of T-Mail share – the account's mailboxes with their
// state and the folders of each. Read from titand; nothing is kept in the
// browser beyond the open page.
import { apiErrorText, tOr } from '../i18n/index.svelte.ts';
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

export async function loadFolders(id: string): Promise<void> {
	try {
		mail.folders[id] = await mailApi.folders(id);
		mail.folderErrors[id] = '';
	} catch (err) {
		mail.folderErrors[id] = apiErrorText(err);
		mail.folders[id] ??= [];
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
	avatars.photos = {};
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
		const photos: Record<string, string> = {};
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
