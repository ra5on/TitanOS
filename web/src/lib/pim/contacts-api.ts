// 1.11 (step 16): the requests of Kontakte (BAU-API "Schritt 14b"). Ids
// are file names chosen by devices: always encoded in the path. The one
// address book is "kontakte".
import { api, apiFetch, dispositionName, responseError, saveBlob, ApiError } from '../api.ts';
import { apiPath } from '../cluster-node.ts';
import {
	sanitizeContact,
	sanitizeDuplicates,
	sanitizeGroup,
	sanitizeItem,
	sanitizeItems,
	sanitizeOverview,
	sanitizeSuggestions,
	type Contact,
	type DuplicateSet,
	type Group,
	type Input,
	type Item,
	type Overview,
	type Suggestion,
} from './contacts.ts';

export const BOOK = 'kontakte';
const enc = encodeURIComponent;
const bad = () => new ApiError(0, 'invalid_response', 'unexpected answer');
const one = (id: string) => `/contacts/${BOOK}/${enc(id)}`;

/** A write's answer; `item` (1.13) is the contact's row of the list now. */
export type Saved = {
	id: string;
	book: string;
	etag: string;
	item?: unknown;
	listETag?: string;
	ctag?: string;
};
export type PhotoSaved = Saved & { hasPhoto: boolean };

/** The row of a write's answer, checked (null when titand sent none). */
export const rowOf = (saved: Saved): Item | null => sanitizeItem(saved.item);

/** A search in one request (1.13): the contacts and, with Mail's release, its suggestions. */
export async function searchContacts(
	q: string,
	signal?: AbortSignal,
): Promise<{ contacts: Item[]; suggestions: Suggestion[] }> {
	const raw = await api.get<{ contacts?: unknown; suggestions?: unknown }>(
		`/contacts/search?q=${enc(q)}`,
		signal,
	);
	const contacts = sanitizeItems(raw?.contacts);
	const suggestions = sanitizeSuggestions(raw?.suggestions ?? []);
	if (!contacts || !suggestions) throw bad();
	return { contacts, suggestions };
}

/**
 * The whole list. 1.13: titand names it with an ETag; asked again with it,
 * an unchanged list answers 304 without a body and this gives null – the
 * caller keeps the list it has. The ETag lives here, per page load; a new
 * page of Kontakte starts with `startContactList()` (115-7).
 */
let listTag = '';
export async function loadContacts(): Promise<Item[] | null> {
	let res: Response;
	try {
		res = await apiFetch('/contacts', listTag ? { headers: { 'If-None-Match': listTag } } : {});
	} catch {
		throw new ApiError(0, 'unreachable', 'titand is not reachable');
	}
	if (res.status === 304) return null;
	if (!res.ok) throw await responseError(res);
	const list = sanitizeItems(await res.json().catch(() => null));
	if (!list) throw bad();
	listTag = res.headers.get('ETag') ?? '';
	return list;
}
/** Forgets the list's ETag (the next load reads the body). */
export function forgetListTag() {
	listTag = '';
}
/**
 * 115-7: this module outlives the page – a page of Kontakte opened again
 * in the app (Einstellungen and back) has an empty list, but the old tag
 * made its first load a 304 and it stayed empty ("0 Kontakte"). A page
 * calls this when it starts: its first load reads the whole list and the
 * generation it has seen is unknown again.
 */
export function startContactList() {
	listTag = '';
	known = '';
}
/**
 * The book's ctag the shown list has seen (the live stream's "generation"
 * of contacts/kontakte); '' when unknown.
 */
let known = '';
export function knowGeneration(generation: string) {
	known = generation;
}
export function knownGeneration(): string {
	return known;
}
/** The ctag one write after `ctag` ("<epoch>-<seq>"). */
export function nextCTag(ctag: string): string {
	const at = ctag.lastIndexOf('-');
	const seq = Number(ctag.slice(at + 1));
	return at > 0 && Number.isInteger(seq) ? `${ctag.slice(0, at)}-${seq + 1}` : '';
}
/**
 * After a write of this page (its row put in place): the list's ETag
 * titand named with the write is kept only when the book's ctag is the
 * known one plus this write – then nothing else changed and the live
 * event of the write is a 304 (or skipped). Otherwise the next read of
 * the list is a whole one.
 */
export function keepListTag(tag: string | undefined | null, ctag: string | undefined | null) {
	if (tag && ctag && known && ctag === nextCTag(known)) {
		listTag = tag;
		known = ctag;
	} else {
		listTag = '';
		known = '';
	}
}

/** A DELETE answering 204 with the list's ETag in X-List-ETag. */
async function deleteKeeping(path: string): Promise<void> {
	let res: Response;
	try {
		res = await apiFetch(path, { method: 'DELETE' });
	} catch {
		throw new ApiError(0, 'unreachable', 'titand is not reachable');
	}
	if (!res.ok) throw await responseError(res);
	keepListTag(res.headers.get('X-List-ETag'), res.headers.get('X-CTag'));
}

/** Address books, groups and the number of possible duplicates: the side bar in one request. */
export async function loadOverview(): Promise<Overview> {
	const o = sanitizeOverview(await api.get<unknown>('/contacts/overview'));
	if (!o) throw bad();
	return o;
}

export async function loadDuplicates(): Promise<DuplicateSet[]> {
	const d = sanitizeDuplicates(await api.get<unknown>('/contacts/duplicates'));
	if (!d) throw bad();
	return d;
}

const groupPath = (id: string) => `/contacts/groups/${enc(id)}?book=${BOOK}`;
async function groupOf(p: Promise<unknown>): Promise<Group> {
	const raw = await p;
	const g = sanitizeGroup(raw);
	if (!g) throw bad();
	const r = raw as { listETag?: string; ctag?: string };
	keepListTag(r.listETag, r.ctag);
	return g;
}
export function createGroup(name: string, members: string[] = []) {
	return groupOf(api.post('/contacts/groups', { book: BOOK, name, members }));
}
/** Renames a group or adds and takes out members (contact ids); `contacts_conflict` when it changed. */
export function updateGroup(
	g: Pick<Group, 'id' | 'etag'>,
	change: { name?: string; add?: string[]; remove?: string[] },
) {
	return groupOf(api.put(groupPath(g.id), { etag: g.etag, ...change }));
}
/** Deletes a group; its contacts stay. */
export function removeGroup(g: Pick<Group, 'id' | 'etag'>) {
	return deleteKeeping(`${groupPath(g.id)}&etag=${enc(g.etag)}`);
}

export type Merged = Saved & { groups: number };
/** Takes `drop` into `keep` with the fields chosen; titand deletes `drop` last. */
export function mergeContacts(
	keep: { id: string; etag: string },
	drop: { id: string; etag: string },
	contact: Input,
	photo: 'keep' | 'drop' | 'none',
) {
	return api.post<Merged>('/contacts/merge', { book: BOOK, keep, drop, contact, photo });
}

export async function loadContact(id: string): Promise<Contact> {
	const c = sanitizeContact(await api.get<unknown>(one(id)));
	if (!c) throw bad();
	return c;
}

export function createContact(contact: Input) {
	return api.post<Saved>('/contacts', { book: BOOK, contact });
}

/** Changes a contact; `contacts_conflict` (409) when it changed meanwhile. */
export function updateContact(id: string, etag: string, contact: Input) {
	return api.put<Saved>(one(id), { etag, contact });
}

export function removeContact(id: string, etag: string) {
	return deleteKeeping(`${one(id)}?etag=${enc(etag)}`);
}

/** The address of a contact's picture; `v` (the etag) loads a new one after a change. */
export function photoURL(id: string, v = ''): string {
	return apiPath(`${one(id)}/photo` + (v ? `?v=${enc(v)}` : ''));
}

export function putPhotoFile(id: string, etag: string, file: File) {
	const form = new FormData();
	form.append('file', file);
	return api.put<PhotoSaved>(`${one(id)}/photo?etag=${enc(etag)}`, form);
}

/** A picture from the files of the NAS, with the account's rights. */
export function putPhotoNas(id: string, etag: string, root: string, path: string) {
	return api.put<PhotoSaved>(`${one(id)}/photo?etag=${enc(etag)}`, { root, path });
}

export function removePhoto(id: string, etag: string) {
	return api.delete<PhotoSaved>(`${one(id)}/photo?etag=${enc(etag)}`);
}

/** Learned addresses of T-Mail not in a contact ([] without the release of T-Mail). */
export async function loadSuggestions(q = ''): Promise<Suggestion[]> {
	const list = sanitizeSuggestions(
		await api.get<unknown>('/contacts/suggestions' + (q ? `?q=${enc(q)}` : '')),
	);
	if (!list) throw bad();
	return list;
}

/** "Als Kontakt übernehmen": a contact of a learned address. */
export function fromSuggestion(addr: string) {
	return api.post<Saved>('/contacts/from-suggestion', { addr, book: BOOK });
}

export type ImportResult = {
	added: number;
	updated: number;
	skipped: number;
	/** `index` (115-7, when titand names it): the card's place in the file, from 1. */
	failed: { uid?: string; name?: string; index?: number; reason: string }[];
};
export function importVCF(file: File, onDuplicate: 'skip' | 'replace') {
	const form = new FormData();
	form.append('file', file);
	return api.post<ImportResult>(`/contacts/books/${BOOK}/import?onDuplicate=${onDuplicate}`, form);
}

/** Saves the address book as .vcf under the name titand gives. */
export async function exportVCF(): Promise<void> {
	let res: Response;
	try {
		res = await apiFetch(`/contacts/books/${BOOK}/export`);
	} catch {
		throw new ApiError(0, 'unreachable', 'titand is not reachable');
	}
	if (!res.ok) throw await responseError(res);
	saveBlob(
		await res.blob(),
		dispositionName(res.headers.get('Content-Disposition')) || `${BOOK}.vcf`,
	);
}
