// 1.11 (step 16): the requests of Kontakte (BAU-API "Schritt 14b"). Ids
// are file names chosen by devices: always encoded in the path. The one
// address book is "kontakte".
import { api, apiFetch, dispositionName, responseError, saveBlob, ApiError } from '../api.ts';
import { apiPath } from '../cluster-node.ts';
import {
	sanitizeContact,
	sanitizeItems,
	sanitizeSuggestions,
	type Contact,
	type Input,
	type Item,
	type Suggestion,
} from './contacts.ts';

export const BOOK = 'kontakte';
const enc = encodeURIComponent;
const bad = () => new ApiError(0, 'invalid_response', 'unexpected answer');
const one = (id: string) => `/contacts/${BOOK}/${enc(id)}`;

export type Saved = { id: string; book: string; etag: string };
export type PhotoSaved = Saved & { hasPhoto: boolean };

export async function loadContacts(q = ''): Promise<Item[]> {
	const list = sanitizeItems(await api.get<unknown>('/contacts' + (q ? `?q=${enc(q)}` : '')));
	if (!list) throw bad();
	return list;
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
	return api.delete(`${one(id)}?etag=${enc(etag)}`);
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
	failed: { uid?: string; name?: string; reason: string }[];
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
